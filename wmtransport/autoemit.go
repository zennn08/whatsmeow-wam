package wmtransport

import (
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	wam "github.com/zennn08/whatsmeow-wam"
)

// committer is the subset of *wam.Coordinator the emitter needs; an interface so
// tests can record commits without a live coordinator.
type committer interface {
	Commit(event string, payload map[string]any)
}

// AutoEmitter observes a whatsmeow client's typed events and commits the WAM
// protocol-lifecycle events WA Web fires at the same points, deriving every field
// honestly from real activity.
//
// whatsmeow exposes only typed events, not raw stanzas, so the raw-stanza and
// outbound events in the zapo-js auto-emitter are NOT reproducible here:
//   - outbound (E2eMessageSend, WebcMessageSend, MessageSend, EditMessageSend):
//     whatsmeow has no send hook.
//   - raw-stanza (MessageHighRetryCount, ClockSkewDifferenceT, WaOldCode,
//     UnknownStanza, OfflineCountTooHigh): no public raw-node observer.
//
// Covered here (9 of the 19 lifecycle events): WebcSocketConnect,
// WebcStreamModeChange, WebcPageResume, WebcRawPlatforms, MessageReceive,
// E2eMessageRecv, ReceiptStanzaReceive, GroupJoinC, MdBootstrapHistoryDataReceived.
// (WebWamForceFlush is emitted by Coordinator.Close.)
type AutoEmitter struct {
	coord committer
	dev   *store.Device

	mu               sync.Mutex
	streamMode       string // "" | MAIN | SYNCING | OFFLINE
	connectedOnce    bool
	resumeCount      int
	platformReported bool
	inOfflineSync    bool

	// raw-node tracking (Tier C: derived from Client.RawNodeHandler)
	clockSkewReported bool
	sentMessages      map[string]sentInfo // outbound msg id -> send context, for the ack
	pendingIqs        map[string]pendingIq
	recvEnc           map[string]encInfo // inbound msg id -> enc info, to enrich E2eMessageRecv

	synth *SyntheticUI // optional Phase 3 synthetic UI engine

	// EmitAppStateActions enables the integrator-action events (ChatMute,
	// ChatAction, StatusMute, MdSyncdDogfoodingFeatureUsage) from app-state
	// events. Off by default: whatsmeow emits app-state events on SYNC (from any
	// device), not on this client's own action, so enabling it can over-report on
	// multi-device accounts. Full-sync events are always skipped.
	EmitAppStateActions bool
}

// AttachAutoEmitter builds an AutoEmitter and registers it on the client. Call
// once, after NewCoordinator. The returned emitter's Handle is already wired.
func AttachAutoEmitter(cli *whatsmeow.Client, coord *wam.Coordinator) *AutoEmitter {
	ae := &AutoEmitter{
		coord:        coord,
		dev:          cli.Store,
		sentMessages: make(map[string]sentInfo),
		pendingIqs:   make(map[string]pendingIq),
		recvEnc:      make(map[string]encInfo),
	}
	cli.AddEventHandler(ae.Handle)
	// RawNodeHandler gives stanza-level visibility for the outbound and
	// raw-stanza events the typed event API can't express (Tier C).
	cli.RawNodeHandler = ae.HandleRawNode
	return ae
}

// StartSynthetic enables Phase 3 synthetic UI telemetry: fabricated ambient
// UiAction-style events (jittered, rate-limited, gated) so the event profile
// resembles a human WA Web session. Call once; returns the engine so you can
// Stop it. Best-effort anti-fingerprinting.
func (a *AutoEmitter) StartSynthetic(opts SyntheticOptions) *SyntheticUI {
	s := NewSyntheticUI(a.coord, opts)
	a.synth = s
	s.Start()
	return s
}

// Stop halts the synthetic UI engine (if started). The typed/raw handlers stay
// registered on the client.
func (a *AutoEmitter) Stop() {
	if a.synth != nil {
		a.synth.Stop()
	}
}

// Handle is the whatsmeow event handler. Safe to register via cli.AddEventHandler.
func (a *AutoEmitter) Handle(evt any) {
	switch e := evt.(type) {
	case *events.Connected:
		a.onConnected()
	case *events.Disconnected:
		a.onDisconnected()
	case *events.OfflineSyncPreview:
		a.setOfflineSync(true)
	case *events.OfflineSyncCompleted:
		a.setOfflineSync(false)
		a.setStreamMode("MAIN")
	case *events.Message:
		a.onMessage(e.Info, e.RetryCount, true)
		if a.synth != nil {
			a.synth.OnMessage(e.Info, e.Message)
		}
	case *events.UndecryptableMessage:
		a.onMessage(e.Info, 0, false)
	case *events.Receipt:
		a.onReceipt(e)
	case *events.JoinedGroup:
		a.onJoinedGroup(e)
	case *events.HistorySync:
		a.onHistorySync(e)
	case *events.Mute:
		a.onMute(e)
	case *events.Pin:
		a.onPin(e)
	case *events.Archive:
		a.onArchive(e)
	case *events.MarkChatAsRead:
		a.onMarkChatAsRead(e)
	case *events.DeleteChat:
		a.onDeleteChat(e)
	case *events.ClearChat:
		a.onClearChat(e)
	case *events.UserStatusMute:
		a.onUserStatusMute(e)
	}
}

func (a *AutoEmitter) onConnected() {
	a.mu.Lock()
	reason := "PAGE_LOAD"
	if a.connectedOnce {
		reason = "RECONNECT"
	}
	resume := 0
	emitResume := false
	if a.connectedOnce {
		a.resumeCount++
		resume = a.resumeCount
		emitResume = true
	}
	a.connectedOnce = true
	a.inOfflineSync = true
	platform := ""
	if !a.platformReported && a.dev != nil && a.dev.Platform != "" {
		platform = a.dev.Platform
		a.platformReported = true
	}
	a.mu.Unlock()

	a.coord.Commit("WebcSocketConnect", map[string]any{"webcSocketConnectReason": reason})
	a.setStreamMode("SYNCING")
	if emitResume {
		a.coord.Commit("WebcPageResume", map[string]any{"webcResumeCount": resume})
	}
	if platform != "" {
		a.coord.Commit("WebcRawPlatforms", map[string]any{"webcRawPlatform": platform})
	}
}

func (a *AutoEmitter) onDisconnected() {
	a.mu.Lock()
	has := a.streamMode != ""
	a.mu.Unlock()
	if has {
		a.setStreamMode("OFFLINE")
	}
}

func (a *AutoEmitter) setOfflineSync(v bool) {
	a.mu.Lock()
	a.inOfflineSync = v
	a.mu.Unlock()
}

// setStreamMode mirrors WA Web's stream model: emit on each real transition, deduped.
func (a *AutoEmitter) setStreamMode(mode string) {
	a.mu.Lock()
	if a.streamMode == mode {
		a.mu.Unlock()
		return
	}
	a.streamMode = mode
	a.mu.Unlock()
	a.coord.Commit("WebcStreamModeChange", map[string]any{"webcStreamMode": mode})
}

func (a *AutoEmitter) onMessage(info types.MessageInfo, retryCount int, decrypted bool) {
	a.mu.Lock()
	offline := a.inOfflineSync
	a.mu.Unlock()

	isLid := isLID(info.MessageSource)
	isGroup := info.IsGroup
	enc, hasEnc := a.popRecvEnc(info.ID)

	recv := map[string]any{
		"e2eSuccessful":  decrypted,
		"e2eDestination": destKey(info.Chat),
		"isLid":          isLid,
		"offline":        offline,
	}
	if retryCount > 0 {
		recv["retryCount"] = retryCount
	}
	// Ciphertext type/version + media come from the raw <enc> node (Tier C).
	if hasEnc {
		if enc.ciphertextType != "" {
			recv["e2eCiphertextType"] = enc.ciphertextType
		}
		if enc.version > 0 {
			recv["e2eCiphertextVersion"] = enc.version
		}
		if enc.mediaType != "" {
			recv["messageMediaType"] = enc.mediaType
		}
	} else if info.MediaType != "" {
		if mt := mediaTypeKey(info.MediaType); mt != "" {
			recv["messageMediaType"] = mt
		}
	}
	if isGroup {
		recv["typeOfGroup"] = "GROUP"
	}
	a.coord.Commit("E2eMessageRecv", recv)

	mr := map[string]any{
		"messageType":      msgTypeKey(info.MessageSource),
		"isLid":            isLid,
		"messageIsOffline": offline,
	}
	if isGroup {
		mr["typeOfGroup"] = "GROUP"
	}
	a.coord.Commit("MessageReceive", mr)

	// OfflineCountTooHigh (WA's s=11 threshold), from the raw stanza's offline attr.
	if hasEnc && enc.offlineCount >= offlineCountTooHighThreshold {
		payload := map[string]any{
			"offlineCount": enc.offlineCount,
			"stanzaType":   "MESSAGE",
			"messageType":  msgTypeKey(info.MessageSource),
			"mediaType":    "NONE",
		}
		if enc.mediaType != "" {
			payload["mediaType"] = enc.mediaType
		}
		a.coord.Commit("OfflineCountTooHigh", payload)
	}
}

func (a *AutoEmitter) onReceipt(e *events.Receipt) {
	stanzaType := string(e.Type)
	if stanzaType == "" {
		stanzaType = "delivery" // whatsmeow maps the delivered receipt (no type attr) to ""
	}
	a.coord.Commit("ReceiptStanzaReceive", map[string]any{
		"receiptStanzaType":       stanzaType,
		"receiptStanzaTotalCount": len(e.MessageIDs),
	})
}

func (a *AutoEmitter) onJoinedGroup(e *events.JoinedGroup) {
	// Skip groups this account created (WA Web fires GroupJoinC only when someone
	// else added you / you joined via invite).
	if e.Sender != nil && a.isSelf(*e.Sender) {
		return
	}
	a.coord.Commit("GroupJoinC", nil)
}

func (a *AutoEmitter) onHistorySync(e *events.HistorySync) {
	if e.Data == nil {
		return
	}
	a.coord.Commit("MdBootstrapHistoryDataReceived", map[string]any{
		"historySyncChunkOrder":    int(e.Data.GetChunkOrder()),
		"historySyncStageProgress": int(e.Data.GetProgress()),
	})
}

func (a *AutoEmitter) isSelf(jid types.JID) bool {
	s := a.dev
	if s == nil {
		return false
	}
	if s.ID != nil && jid.ToNonAD() == s.ID.ToNonAD() {
		return true
	}
	return !s.LID.IsEmpty() && jid.ToNonAD() == s.LID.ToNonAD()
}

// destKey maps a chat JID to the E2E_DESTINATION enum key (matching zapo's
// e2eDestinationKey: group/status/channel/individual).
func destKey(chat types.JID) string {
	switch {
	case chat.Server == types.GroupServer:
		return "GROUP"
	case chat.Server == types.BroadcastServer && chat.User == types.StatusBroadcastJID.User:
		return "STATUS"
	case chat.Server == types.NewsletterServer:
		return "CHANNEL"
	default:
		return "INDIVIDUAL"
	}
}

// msgTypeKey maps a message source to the MESSAGE_TYPE enum key.
func msgTypeKey(src types.MessageSource) string {
	switch {
	case src.Chat.Server == types.NewsletterServer:
		return "CHANNEL"
	case src.Chat.Server == types.BroadcastServer && src.Chat.User == types.StatusBroadcastJID.User:
		return "STATUS"
	case src.Chat.Server == types.BroadcastServer:
		return "BROADCAST"
	case src.IsGroup || src.Chat.Server == types.GroupServer:
		return "GROUP"
	default:
		return "INDIVIDUAL"
	}
}

func isLID(src types.MessageSource) bool {
	return src.AddressingMode == types.AddressingModeLID ||
		src.Sender.Server == types.HiddenUserServer ||
		src.Chat.Server == types.HiddenUserServer
}
