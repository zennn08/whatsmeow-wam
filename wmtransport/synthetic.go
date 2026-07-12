package wmtransport

import (
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// Synthetic UI telemetry (Phase 3), ported from zapo-js's WaWamSyntheticUi.
// Fabricates the plausible ambient UiAction-style events a human WA Web session
// emits, so a headless client's event profile isn't conspicuously protocol-only.
// Everything is jittered, rate-limited, and confined to optional active hours —
// a badly-timed or skeleton event is a worse tell than none.
//
// Stage 3.1: the orchestrator (scheduling, active hours, session identity, the
// three periodic loops, message/node-out driven fabrications) and the base
// ambient stream. The larger AMBIENT_FABS weighted table (fabrications.ts) is
// Stage 3.2.

const (
	messageOpenMinGapMs = 20_000
	infoOpenMinGapMs    = 180_000
	aboutMinGapMs       = 120_000
	recentChats         = 12
	activitySliceMs     = 60_000
	activityFlushSlices = 5
	activityMaxSlices   = 64
)

var emojiTabs = []string{"EMOJI", "GIF", "STICKER"}

// SyntheticOptions tune the fabrication rates and gating. Zero values fall back to
// the same defaults zapo uses.
type SyntheticOptions struct {
	ChatOpenProbability         float64 // default 0.25
	ImageOpenProbability        float64 // default 0.3
	AudioLoadProbability        float64 // default 0.3
	InfoOpenProbability         float64 // default 0.05
	AttachmentTrayProbability   float64 // default 0.4
	AboutConsumptionProbability float64 // default 0.06
	AmbientIntervalMinMs        float64 // default 5min
	AmbientIntervalMaxMs        float64 // default 25min
	MemoryIntervalMinMs         float64 // default 2min
	MemoryIntervalMaxMs         float64 // default 5min
	// ActiveHours: outside [start,end) nothing is fabricated. Both must be set
	// (non-nil) to take effect; start>end spans midnight.
	ActiveHoursStartHour *int
	ActiveHoursEndHour   *int
	// Capability gates for surface-specific ambient events; enable only when the
	// account genuinely has that surface (firing one otherwise is a tell).
	Channels    bool
	Communities bool
	Business    bool
}

type ambientSpec struct {
	w    int
	emit func()
}

// SyntheticUI is the fabrication engine. Create with NewSyntheticUI, Start it, and
// feed it OnMessage / OnNodeOut; Stop cancels all timers.
type SyntheticUI struct {
	coord committer
	opts  SyntheticOptions

	mu       sync.Mutex
	timers   map[*time.Timer]struct{}
	disposed bool

	// immutable session identity
	windowHeightFloat int
	sessionStartMs    int64
	unifiedSessionID  string
	appSessionID      string
	gifProvider       string

	ambientSpecs []ambientSpec

	// mutable state (guarded by mu)
	recentChatIsLid   []bool
	activitySessionID string
	tsSessionID       int
	activityStartMs   int64
	lastOpenMs        int64
	lastInfoOpenMs    int64
	lastAboutMs       int64
	memCurrentKb      int
	memPeakKb         int
	messagesSeen      int
	activitySlice     int
	activitySeq       int
	activeSliceCount  int
	bitmapLow         uint32
	bitmapHigh        uint32
	sliceActive       bool
}

// NewSyntheticUI builds a fabrication engine writing to coord.
func NewSyntheticUI(coord committer, opts SyntheticOptions) *SyntheticUI {
	opts.ChatOpenProbability = probOr(opts.ChatOpenProbability, 0.25)
	opts.ImageOpenProbability = probOr(opts.ImageOpenProbability, 0.3)
	opts.AudioLoadProbability = probOr(opts.AudioLoadProbability, 0.3)
	opts.InfoOpenProbability = probOr(opts.InfoOpenProbability, 0.05)
	opts.AttachmentTrayProbability = probOr(opts.AttachmentTrayProbability, 0.4)
	opts.AboutConsumptionProbability = probOr(opts.AboutConsumptionProbability, 0.06)
	opts.AmbientIntervalMinMs = intervalOr(opts.AmbientIntervalMinMs, 5*60_000)
	opts.AmbientIntervalMaxMs = intervalOr(opts.AmbientIntervalMaxMs, 25*60_000)
	opts.MemoryIntervalMinMs = intervalOr(opts.MemoryIntervalMinMs, 2*60_000)
	opts.MemoryIntervalMaxMs = intervalOr(opts.MemoryIntervalMaxMs, 5*60_000)

	now := nowMilli()
	gif := "TENOR"
	if rand.Float64() >= 0.75 {
		gif = "GIPHY"
	}
	s := &SyntheticUI{
		coord:             coord,
		opts:              opts,
		timers:            make(map[*time.Timer]struct{}),
		windowHeightFloat: randInt(680, 1040),
		sessionStartMs:    now,
		unifiedSessionID:  strconv.Itoa(randInt(1, 2_000_000_000)),
		appSessionID:      randUUID(),
		gifProvider:       gif,
		activitySessionID: randBase36(6),
		tsSessionID:       randInt(1, 2_000_000_000),
		activityStartMs:   now,
		memCurrentKb:      randInt(50_000, 90_000),
	}
	s.registerAmbientSpecs()
	return s
}

// Start kicks off the three periodic loops.
func (s *SyntheticUI) Start() {
	s.scheduleAmbient()
	s.scheduleMemory()
	s.scheduleActivitySlice()
}

// Stop cancels all pending timers; the engine emits nothing further.
func (s *SyntheticUI) Stop() {
	s.mu.Lock()
	s.disposed = true
	for t := range s.timers {
		t.Stop()
		delete(s.timers, t)
	}
	s.mu.Unlock()
}

// OnMessage fabricates chat-open / media / info-drawer activity anchored to an
// inbound message.
func (s *SyntheticUI) OnMessage(info types.MessageInfo, msg *waE2E.Message) {
	if !s.canEmit() {
		return
	}
	s.markActivity()
	isLid := isLID(info.MessageSource)
	isNewsletter := info.Chat.Server == types.NewsletterServer
	s.rememberChat(isLid)

	now := nowMilli()
	s.mu.Lock()
	openOK := rand.Float64() <= s.opts.ChatOpenProbability && now-s.lastOpenMs >= messageOpenMinGapMs
	if openOK {
		s.lastOpenMs = now
	}
	s.mu.Unlock()
	if openOK {
		s.schedule(jitter(2000, 60_000), func() { s.emitChatOpen(isLid) })
		if msg.GetImageMessage() != nil && rand.Float64() < s.opts.ImageOpenProbability {
			s.schedule(jitter(4000, 90_000), func() { s.emitImageOpen(isLid) })
		}
	}

	if (info.IsGroup || isNewsletter) && s.infoOpenAllowed() {
		payload := map[string]any{"uiActionType": "GROUP_INFO_OPEN", "uiActionPreloaded": true, "isLid": isLid, "uiActionT": randInt(40, 400)}
		if isNewsletter {
			payload = map[string]any{"uiActionType": "CHANNEL_INFO_OPEN", "uiActionPreloaded": true, "uiActionT": randInt(40, 400)}
		}
		s.schedule(jitter(3000, 120_000), func() { s.emitUiAction(payload) })
	}

	if msg.GetAudioMessage() != nil && rand.Float64() < s.opts.AudioLoadProbability {
		s.schedule(jitter(1000, 8000), func() { s.emitMediaLoad() })
	}

	if !info.IsGroup && !isNewsletter && rand.Float64() < s.opts.AboutConsumptionProbability {
		s.mu.Lock()
		aboutOK := now-s.lastAboutMs >= aboutMinGapMs
		if aboutOK {
			s.lastAboutMs = now
		}
		s.mu.Unlock()
		if aboutOK {
			s.schedule(jitter(2000, 40_000), func() { s.emitAboutConsumption() })
		}
	}

	if msg.GetVideoMessage() != nil && rand.Float64() < 0.3 {
		s.schedule(jitter(1500, 20_000), func() { s.emitMediaStreamPlayback() })
	}
	if info.IsGroup && rand.Float64() < 0.05 {
		s.schedule(jitter(2000, 30_000), func() { s.emitGroupCatchUp() })
	}
	if link := msg.GetExtendedTextMessage().GetMatchedText(); link != "" && strings.Contains(strings.ToLower(link), "youtu") && rand.Float64() < 0.4 {
		s.schedule(jitter(3000, 60_000), func() { s.emitInlineVideoClosed(info.IsGroup) })
	}
	if s.opts.Business && (msg.GetButtonsMessage() != nil || msg.GetInteractiveMessage() != nil || msg.GetTemplateMessage() != nil) && rand.Float64() < 0.25 {
		s.schedule(jitter(2000, 30_000), func() { s.emitStructuredMessageBuyerInteraction() })
	}
}

// OnNodeOut fabricates attachment-tray / media-picker / mention activity anchored
// to an outbound message stanza.
func (s *SyntheticUI) OnNodeOut(node *waBinary.Node) {
	if !s.canEmit() || node.Tag != "message" {
		return
	}
	s.markActivity()
	ag := node.AttrGetter()
	to := ag.OptionalJIDOrEmpty("to")
	isLid := to.Server == types.HiddenUserServer || ag.OptionalString("addressing_mode") == string(types.AddressingModeLID)
	enc := findFirstEncNode(node)
	media := ""
	if enc != nil {
		media = mediaTypeKey(enc.AttrGetter().OptionalString("mediatype"))
	}
	isGroup := to.Server == types.GroupServer

	if media != "" && rand.Float64() < s.opts.AttachmentTrayProbability {
		s.schedule(jitter(1000, 12_000), func() { s.emitAttachmentTray(media, isGroup) })
	}
	if media != "" {
		isPhotoVideo := media == "PHOTO" || media == "VIDEO"
		if (isPhotoVideo || media == "DOCUMENT") && rand.Float64() < 0.35 {
			s.schedule(jitter(1500, 15_000), func() { s.emitMediaPicker(media) })
		}
		if isPhotoVideo && rand.Float64() < 0.3 {
			s.schedule(jitter(1000, 12_000), func() { s.emitMediaEditorSend() })
		}
		if isPhotoVideo && rand.Float64() < 0.12 {
			s.schedule(jitter(800, 8000), func() { s.emitHdMediaAwareness() })
		}
	} else if enc != nil && rand.Float64() < 0.04 {
		s.schedule(jitter(2000, 30_000), func() { s.emitTextMessageUserJourney(isGroup) })
	}
	if isGroup && rand.Float64() < 0.07 {
		s.schedule(jitter(1500, 20_000), func() { s.emitMentionPickerAction() })
	}
	if s.infoOpenAllowed() {
		s.schedule(jitter(3000, 120_000), func() {
			s.emitUiAction(map[string]any{"uiActionType": "MSG_INFO_OPEN", "uiActionPreloaded": true, "isLid": isLid, "uiActionT": randInt(40, 400)})
		})
	}
}

// --- periodic loops ---

func (s *SyntheticUI) scheduleAmbient() {
	s.schedule(jitter(s.opts.AmbientIntervalMinMs, s.opts.AmbientIntervalMaxMs), func() {
		s.pickAmbient()
		s.scheduleAmbient()
	})
}

func (s *SyntheticUI) pickAmbient() {
	if len(s.ambientSpecs) == 0 {
		return
	}
	total := 0
	for _, sp := range s.ambientSpecs {
		total += sp.w
	}
	r := rand.Intn(total)
	for _, sp := range s.ambientSpecs {
		r -= sp.w
		if r < 0 {
			sp.emit()
			return
		}
	}
}

func (s *SyntheticUI) scheduleMemory() {
	s.schedule(jitter(s.opts.MemoryIntervalMinMs, s.opts.MemoryIntervalMaxMs), func() {
		s.emitMemoryStat()
		s.scheduleMemory()
	})
}

func (s *SyntheticUI) scheduleActivitySlice() {
	s.schedule(activitySliceMs*time.Millisecond, func() {
		s.recordActivitySlice()
		s.scheduleActivitySlice()
	})
}

func (s *SyntheticUI) registerAmbientSpecs() {
	add := func(w int, emit func()) { s.ambientSpecs = append(s.ambientSpecs, ambientSpec{w, emit}) }
	add(40, s.emitChatOpenAmbient)
	add(5, s.emitEmojiOpen)
	add(3, s.emitStickerPickerOpened)
	add(4, s.emitContactSearch)
	add(3, s.emitTsNavigation)
	add(1, s.emitDisappearingModeSetting)
	add(3, s.emitGifSearchSession)
	add(8, s.emitMessageContextMenu)
	// The AMBIENT_FABS weighted table (Stage 3.2). Faithful to zapo, these commit
	// directly (no active-hours guard). Gated entries only when their flag is on.
	for _, f := range ambientFabTable() {
		if f.gate != "" && !s.gateEnabled(f.gate) {
			continue
		}
		fab := f
		add(fab.weight, func() { fab.emit(s) })
	}
}

func (s *SyntheticUI) gateEnabled(gate string) bool {
	switch gate {
	case "channels":
		return s.opts.Channels
	case "communities":
		return s.opts.Communities
	case "business":
		return s.opts.Business
	default:
		return true
	}
}

// --- gating / state ---

func (s *SyntheticUI) canEmit() bool {
	s.mu.Lock()
	d := s.disposed
	s.mu.Unlock()
	return !d && s.withinActiveHours()
}

func (s *SyntheticUI) withinActiveHours() bool {
	if s.opts.ActiveHoursStartHour == nil || s.opts.ActiveHoursEndHour == nil {
		return true
	}
	start, end := *s.opts.ActiveHoursStartHour, *s.opts.ActiveHoursEndHour
	hour := time.Now().Hour()
	if start <= end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

func (s *SyntheticUI) markActivity() {
	if !s.canEmit() {
		return
	}
	s.mu.Lock()
	s.sliceActive = true
	s.messagesSeen++
	s.mu.Unlock()
}

func (s *SyntheticUI) infoOpenAllowed() bool {
	if rand.Float64() >= s.opts.InfoOpenProbability {
		return false
	}
	now := nowMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	if now-s.lastInfoOpenMs < infoOpenMinGapMs {
		return false
	}
	s.lastInfoOpenMs = now
	return true
}

func (s *SyntheticUI) rememberChat(isLid bool) {
	s.mu.Lock()
	s.recentChatIsLid = append(s.recentChatIsLid, isLid)
	if len(s.recentChatIsLid) > recentChats {
		s.recentChatIsLid = s.recentChatIsLid[1:]
	}
	s.mu.Unlock()
}

func (s *SyntheticUI) schedule(delay time.Duration, fn func()) {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	var t *time.Timer
	t = time.AfterFunc(delay, func() {
		s.mu.Lock()
		delete(s.timers, t)
		done := s.disposed
		s.mu.Unlock()
		if !done {
			fn()
		}
	})
	s.timers[t] = struct{}{}
	s.mu.Unlock()
}
