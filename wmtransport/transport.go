// Package wmtransport wires the whatsmeow-wam core to a whatsmeow client: it
// uploads WAM batches as the `<iq type="set" xmlns="w:stats"><add t>` stanza WA
// Web sends, and builds a Coordinator whose globals track the client's identity.
package wmtransport

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	wam "github.com/zennn08/whatsmeow-wam"
)

// iqSender is the subset of the client this package needs. Satisfied by
// cli.DangerousInternals(). Kept as an interface so tests can stub it.
type iqSender interface {
	SendIQ(ctx context.Context, query whatsmeow.InfoQuery) (*waBinary.Node, error)
}

const (
	defaultMaxAttempts = 4
	defaultTimeout     = 15 * time.Second
	maxBackoff         = 30 * time.Second
)

// Transport uploads WAM batches over a whatsmeow client's socket as w:stats IQs,
// waiting for the ack and retrying transient (5xx) failures with backoff.
type Transport struct {
	sender      iqSender
	maxAttempts int
	timeout     time.Duration
	backoffFn   func(attempt int) time.Duration // injectable for tests; nil uses backoff
}

// New builds a Transport from anything exposing SendIQ (i.e.
// cli.DangerousInternals()).
func New(sender iqSender) *Transport {
	return &Transport{sender: sender, maxAttempts: defaultMaxAttempts, timeout: defaultTimeout, backoffFn: backoff}
}

// Upload sends one finalized batch as the `<iq type="set" xmlns="w:stats"><add t>`
// stanza and waits for the server ack. Transient failures (a 5xx iq error) retry
// with exponential backoff + jitter; a permanent rejection returns its error and
// the batch is dropped upstream. sendIQ already handles disconnect retries.
func (t *Transport) Upload(ctx context.Context, batch []byte) error {
	query := whatsmeow.InfoQuery{
		Namespace: "w:stats",
		Type:      whatsmeow.IQSet,
		To:        types.ServerJID,
		Timeout:   t.timeout,
		Content: []waBinary.Node{{
			Tag:     "add",
			Attrs:   waBinary.Attrs{"t": strconv.FormatInt(time.Now().Unix(), 10)},
			Content: batch,
		}},
	}

	var lastErr error
	for attempt := 0; attempt < t.maxAttempts; attempt++ {
		_, err := t.sender.SendIQ(ctx, query)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			return err
		}
		if attempt < t.maxAttempts-1 {
			if err := sleep(ctx, t.backoffFn(attempt)); err != nil {
				return err
			}
		}
	}
	return lastErr
}

// isRetryable reports whether an IQ error is worth retrying: WA replies with a
// 5xx code on transient server failures. sendIQ already retries disconnects, so
// everything else (4xx, timeout, malformed) is treated as terminal.
func isRetryable(err error) bool {
	var iqErr *whatsmeow.IQError
	if errors.As(err, &iqErr) {
		return iqErr.Code >= 500
	}
	return false
}

// backoff is exponential (1s, 2s, 4s, …) capped at maxBackoff, with up to 10% jitter.
func backoff(attempt int) time.Duration {
	base := min(time.Second<<attempt, maxBackoff)
	return base + time.Duration(rand.Int63n(int64(base)/10+1))
}

// sleep waits for d or ctx cancellation, whichever comes first.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewCoordinator builds a wam.Coordinator wired to a whatsmeow client: uploads go
// over its socket via cli.DangerousInternals().SendIQ, uploads are gated on
// cli.IsConnected, and globals derive from the advertised device identity
// (store.DeviceProps + the active WA version).
//
// Browser is not something whatsmeow advertises (it pairs as a desktop companion,
// not a browser), so it defaults to "Chrome"; override opts.Globals.Browser to
// match whatever you present.
func NewCoordinator(cli *whatsmeow.Client, opts wam.Options) *wam.Coordinator {
	if opts.Globals.OSDisplayName == "" {
		opts.Globals.OSDisplayName = store.DeviceProps.GetOs()
	}
	if opts.Globals.AppVersion == "" {
		opts.Globals.AppVersion = store.GetWAVersion().String()
	}
	if opts.Globals.Browser == "" {
		opts.Globals.Browser = "Chrome"
	}
	if opts.Transport == nil {
		opts.Transport = New(cli.DangerousInternals())
	}
	if opts.IsConnected == nil {
		opts.IsConnected = cli.IsConnected
	}
	return wam.New(opts)
}
