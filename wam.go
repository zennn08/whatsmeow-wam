// Package wam emits the client-side WAM (Falco) telemetry batches WhatsApp Web
// uploads over the `w:stats` channel, for wire parity and anti-fingerprinting.
//
// This is Phase 1 (core): the binary TLV encoder, per-channel batching, globals,
// and a manual Commit API, driven by the embedded @vinikjkkj/wa-wam registry.
// Auto-emit of protocol events and synthetic UI telemetry are later phases.
//
// The core package is dependency-free and produces byte-identical batches to the
// zapo-js @zapo-js/wam encoder. Wiring to a whatsmeow client lives in the
// whatsmeow-wam/wmtransport subpackage.
package wam

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Transport ships a finalized WAM batch as the `<iq type="set" xmlns="w:stats">`
// stanza WA Web sends. Best-effort: implementations should not surface transient
// failures beyond returning an error (the coordinator drops on error).
type Transport interface {
	Upload(ctx context.Context, batch []byte) error
}

// Options configure a Coordinator. Only Globals is really required; the rest have
// registry-derived or sensible defaults.
type Options struct {
	// Globals populate batch-level attributes; keep them consistent with the
	// client's pairing ClientPayload.
	Globals GlobalsInput
	// Transport uploads finalized batches. Nil means batches are built and
	// dropped (useful for tests / offline encoding).
	Transport Transport
	// IsConnected gates uploads; a batch built while disconnected is dropped.
	// Nil defaults to always-connected.
	IsConnected func() bool
	// FlushInterval coalesces events before a non-empty batch flushes.
	// Zero uses the registry default (5s).
	FlushInterval time.Duration
	// MaxBufferSize forces an immediate flush at this byte size.
	// Zero uses the registry default (50000).
	MaxBufferSize int
	// DisableSampling skips the random weight gate so every Commit is written.
	// Off by default (real clients sample); handy for tests and manual commits.
	DisableSampling bool
	// Logf, if set, receives best-effort debug lines.
	Logf func(format string, args ...any)

	// rng is an injectable [0,1) source for the sampling gate; nil uses a
	// package rand. Exposed only within the package for deterministic tests.
	rng func() float64
}

const sequenceMax = 65535

// Coordinator owns the WAM telemetry pipeline for one client: it accumulates
// committed events into per-channel batches and flushes on size/interval/close.
type Coordinator struct {
	opts          Options
	streamID      int
	flushInterval time.Duration
	maxBufferSize int
	isConnected   func() bool
	rng           func() float64

	mu          sync.Mutex
	globals     map[string][]globalKV // channel -> resolved globals (cached)
	openBatches map[string]*batch
	sequence    map[string]int
	flushTimer  *time.Timer
	disposed    bool
}

// New builds a Coordinator. It picks a per-session stream id (1..255) and stamps
// it into the header and the streamId global.
func New(opts Options) *Coordinator {
	streamID := 1 + rand.Intn(255)
	opts.Globals.StreamID = streamID

	flush := opts.FlushInterval
	if flush <= 0 {
		flush = time.Duration(defaultOrN(reg.BufferConstants.InMemoryBufferingDurationSecs, 5)) * time.Second
	}
	maxBuf := opts.MaxBufferSize
	if maxBuf <= 0 {
		maxBuf = defaultOrN(reg.BufferConstants.MaxBufferSize, 50000)
	}
	isConn := opts.IsConnected
	if isConn == nil {
		isConn = func() bool { return true }
	}
	rng := opts.rng
	if rng == nil {
		rng = rand.Float64
	}

	return &Coordinator{
		opts:          opts,
		streamID:      streamID,
		flushInterval: flush,
		maxBufferSize: maxBuf,
		isConnected:   isConn,
		rng:           rng,
		globals:       make(map[string][]globalKV),
		openBatches:   make(map[string]*batch),
		sequence:      make(map[string]int),
	}
}

// StreamID is the per-session stream id stamped into every batch.
func (c *Coordinator) StreamID() int { return c.streamID }

// Commit buffers one WAM event until the next flush. Unknown events are ignored.
// Unless DisableSampling is set, the event is dropped by the same
// `rand()*weight > 1` gate WA applies.
func (c *Coordinator) Commit(event string, payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disposed {
		return
	}
	def, ok := reg.Events[event]
	if !ok {
		return
	}
	weight := def.Weight
	if weight <= 0 {
		weight = 1
	}
	if !c.opts.DisableSampling && c.rng()*weight > 1 {
		return
	}
	fields := resolveEventFields(def, payload)
	b := c.openBatchLocked(def.Channel)
	b.writeEvent(time.Now().Unix(), def.ID, int64(weight), fields)
	if b.size() >= c.maxBufferSize {
		delete(c.openBatches, def.Channel)
		go c.upload(b)
	} else {
		c.scheduleFlushLocked()
	}
}

// Flush uploads all open non-empty batches now.
func (c *Coordinator) Flush(ctx context.Context) {
	c.mu.Lock()
	c.clearTimerLocked()
	batches := make([]*batch, 0, len(c.openBatches))
	for ch, b := range c.openBatches {
		batches = append(batches, b)
		delete(c.openBatches, ch)
	}
	c.mu.Unlock()
	for _, b := range batches {
		c.uploadCtx(ctx, b)
	}
}

// Close commits WebWamForceFlush, stops accepting events, and flushes.
func (c *Coordinator) Close(ctx context.Context) {
	c.Commit("WebWamForceFlush", nil)
	c.mu.Lock()
	c.disposed = true
	c.mu.Unlock()
	c.Flush(ctx)
}

func (c *Coordinator) openBatchLocked(channel string) *batch {
	if b, ok := c.openBatches[channel]; ok && b != nil {
		return b
	}
	b := newBatch(channel, c.streamID, c.nextSequenceLocked(channel), c.globalsForLocked(channel))
	c.openBatches[channel] = b
	return b
}

func (c *Coordinator) globalsForLocked(channel string) []globalKV {
	if g, ok := c.globals[channel]; ok {
		return g
	}
	g := resolveGlobals(c.opts.Globals, channel)
	c.globals[channel] = g
	return g
}

func (c *Coordinator) nextSequenceLocked(channel string) int {
	cur, ok := c.sequence[channel]
	var next int
	if !ok || cur >= sequenceMax {
		next = 1
	} else {
		next = cur + 1
	}
	c.sequence[channel] = next
	return next
}

func (c *Coordinator) scheduleFlushLocked() {
	if c.flushTimer != nil {
		return
	}
	c.flushTimer = time.AfterFunc(c.flushInterval, func() {
		c.mu.Lock()
		c.flushTimer = nil
		c.mu.Unlock()
		c.Flush(context.Background())
	})
}

func (c *Coordinator) clearTimerLocked() {
	if c.flushTimer != nil {
		c.flushTimer.Stop()
		c.flushTimer = nil
	}
}

// upload uploads a batch with a fresh background context (used from Commit's
// size-triggered path, which holds no caller context).
func (c *Coordinator) upload(b *batch) { c.uploadCtx(context.Background(), b) }

func (c *Coordinator) uploadCtx(ctx context.Context, b *batch) {
	if b == nil || !b.hasEvents() {
		return
	}
	if !c.isConnected() {
		c.logf("wam batch dropped: not connected (channel=%s size=%d)", b.channel, b.size())
		return
	}
	if c.opts.Transport == nil {
		return
	}
	// ponytail: single attempt, drop on error. Retry-with-backoff is a Transport
	// concern; add there if telemetry loss on transient 5xx matters.
	if err := c.opts.Transport.Upload(ctx, b.bytes()); err != nil {
		c.logf("wam batch upload failed (channel=%s): %v", b.channel, err)
	}
}

func (c *Coordinator) logf(format string, args ...any) {
	if c.opts.Logf != nil {
		c.opts.Logf(format, args...)
	}
}

func defaultOrN(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}
