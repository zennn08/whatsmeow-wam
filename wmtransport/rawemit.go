package wmtransport

import (
	"math"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// Raw-node auto-emit (Tier C): mirrors zapo-js's onNodeOut / onNodeIn /
// resolveOutgoingIq, driven by Client.RawNodeHandler. Reproduces the outbound
// and raw-stanza WAM events the typed event API can't express.

const (
	highRetryThreshold           = 5
	offlineCountTooHighThreshold = 11
	secondsPerHour               = 3600
	maxTrackedSends              = 256
	maxTrackedIQs                = 64
)

// nowMilli is unix millis; a package var so tests can pin it.
var nowMilli = func() int64 { return time.Now().UnixMilli() }

// sentInfo is retained per outbound message id so the later <ack> can emit MessageSend.
type sentInfo struct {
	destination    string
	isLid          bool
	isGroup        bool
	ciphertextType string
	mediaType      string
	editType       string
}

// encInfo is retained per inbound message id so the typed Message event can
// enrich E2eMessageRecv with the raw <enc> fields.
type encInfo struct {
	ciphertextType string
	version        int
	mediaType      string
	offlineCount   int
}

type pendingIq struct {
	kind              string // joinRequest | groupCreate | ephemeral | disappearingMode
	startMs           int64
	groupJid          string
	joinRequestAction string
	hasGroupName      bool
	duration          int
}

// HandleRawNode is the Client.RawNodeHandler. Fast, non-blocking.
func (a *AutoEmitter) HandleRawNode(evt whatsmeow.RawNodeEvent) {
	if evt.Node == nil {
		return
	}
	if evt.Outgoing {
		a.onNodeOut(evt.Node)
		if a.synth != nil {
			a.synth.OnNodeOut(evt.Node)
		}
	} else {
		a.onNodeIn(evt.Node, evt.Handled)
	}
}

func (a *AutoEmitter) onNodeOut(node *waBinary.Node) {
	ag := node.AttrGetter()
	if node.Tag == "iq" {
		a.trackOutgoingIq(node)
		return
	}
	if node.Tag != "message" {
		return
	}
	enc := findFirstEncNode(node)
	if enc == nil {
		return
	}
	to := ag.OptionalJIDOrEmpty("to")
	destination := destKey(to)
	isGroup := to.Server == types.GroupServer
	isLid := to.Server == types.HiddenUserServer ||
		ag.OptionalString("addressing_mode") == string(types.AddressingModeLID)
	encAg := enc.AttrGetter()
	ciphertextType := ciphertextTypeKey(encAg.OptionalString("type"))
	media := mediaTypeKey(encAg.OptionalString("mediatype"))
	version := encAg.OptionalInt("v")
	count := encAg.OptionalInt("count")
	editType := editTypeKey(ag.OptionalString("edit"))

	e2e := map[string]any{
		"e2eSuccessful":  true,
		"e2eDestination": destination,
		"isLid":          isLid,
		"botType":        "UNKNOWN",
		"editType":       firstNonEmpty(editType, "NOT_EDITED"),
		"retryCount":     count,
	}
	if ciphertextType != "" {
		e2e["e2eCiphertextType"] = ciphertextType
	}
	if version > 0 {
		e2e["e2eCiphertextVersion"] = version
	}
	if media != "" {
		e2e["messageMediaType"] = media
	}
	if isGroup {
		e2e["typeOfGroup"] = "GROUP"
	}
	a.coord.Commit("E2eMessageSend", e2e)

	wm := map[string]any{"messageType": destination}
	if media != "" {
		wm["messageMediaType"] = media
	}
	a.coord.Commit("WebcMessageSend", wm)

	if id := ag.OptionalString("id"); id != "" {
		a.trackSend(id, sentInfo{destination, isLid, isGroup, ciphertextType, media, editType})
	}
}

func (a *AutoEmitter) onNodeIn(node *waBinary.Node, handled bool) {
	ag := node.AttrGetter()
	a.maybeClockSkew(ag)

	switch node.Tag {
	case "message":
		a.stashRecvEnc(node)
	case "notification":
		if oldReg := node.GetChildByTag("wa_old_registration"); oldReg.Tag == "wa_old_registration" {
			if did := oldReg.AttrGetter().OptionalString("device_id"); did != "" {
				a.coord.Commit("WaOldCode", map[string]any{"deviceId": did})
			}
		}
	case "iq":
		if t := ag.OptionalString("type"); t == "result" || t == "error" {
			a.resolveOutgoingIq(node, t == "result")
		}
		return
	case "receipt":
		if ag.OptionalString("type") == "retry" {
			retry := node.GetChildByTag("retry")
			if count := retry.AttrGetter().OptionalInt("count"); count >= highRetryThreshold {
				a.coord.Commit("MessageHighRetryCount", map[string]any{
					"retryCount":       count,
					"messageType":      destKey(ag.OptionalJIDOrEmpty("from")),
					"isSenderLidBased": ag.OptionalString("is_lid") == "true",
				})
			}
		}
		return
	case "ack":
		if ag.OptionalString("class") == "message" {
			a.onAck(node)
		}
		return
	}

	if !handled {
		payload := map[string]any{"unknownStanzaTag": node.Tag}
		if t := ag.OptionalString("type"); t != "" {
			payload["unknownStanzaType"] = t
		}
		a.coord.Commit("UnknownStanza", payload)
	}
}

// maybeClockSkew fires ClockSkewDifferenceT once, from the first inbound node
// carrying a server `t` attr more than an hour off local time.
func (a *AutoEmitter) maybeClockSkew(ag *waBinary.AttrUtility) {
	a.mu.Lock()
	if a.clockSkewReported {
		a.mu.Unlock()
		return
	}
	t, ok := ag.GetInt64("t", false)
	if !ok {
		a.mu.Unlock()
		return
	}
	a.clockSkewReported = true
	a.mu.Unlock()

	skewSeconds := float64(nowMilli())/1000 - float64(t)
	if math.Abs(skewSeconds) >= secondsPerHour {
		a.coord.Commit("ClockSkewDifferenceT", map[string]any{
			"clockSkewHourly": int(math.Round(skewSeconds / secondsPerHour)),
		})
	}
}

func (a *AutoEmitter) onAck(node *waBinary.Node) {
	id := node.AttrGetter().OptionalString("id")
	if id == "" {
		return
	}
	info, ok := a.popSend(id)
	if !ok {
		return
	}
	isRevoke := info.editType == "SENDER_REVOKE" || info.editType == "ADMIN_REVOKE"
	send := map[string]any{
		"messageSendResult":           "OK",
		"messageSendResultIsTerminal": false,
		"messageType":                 info.destination,
		"isLid":                       info.isLid,
		"botType":                     "UNKNOWN",
		"editType":                    firstNonEmpty(info.editType, "NOT_EDITED"),
		"messageIsRevoke":             isRevoke,
		"e2eBackfill":                 false,
	}
	if info.ciphertextType != "" {
		send["e2eCiphertextType"] = info.ciphertextType
	}
	if info.mediaType != "" {
		send["messageMediaType"] = info.mediaType
	}
	if info.isGroup {
		send["typeOfGroup"] = "GROUP"
	}
	a.coord.Commit("MessageSend", send)

	if info.editType != "" {
		edit := map[string]any{
			"editType":                    info.editType,
			"messageType":                 info.destination,
			"messageSendResultIsTerminal": false,
		}
		if info.mediaType != "" {
			edit["mediaType"] = info.mediaType
		}
		if info.isGroup {
			edit["typeOfGroup"] = "GROUP"
		}
		a.coord.Commit("EditMessageSend", edit)
	}
	if isRevoke {
		revokeType := "SENDER"
		if info.editType == "ADMIN_REVOKE" {
			revokeType = "ADMIN"
		}
		a.coord.Commit("RevokeMessageSend", map[string]any{
			"revokeType":                  revokeType,
			"messageType":                 info.destination,
			"messageSendResultIsTerminal": false,
		})
		a.coord.Commit("MessageDeleteActions", map[string]any{
			"deleteActionType": "DELETE_FOR_EVERYONE",
			"isAGroup":         info.isGroup,
			"messagesDeleted":  1,
		})
		a.coord.Commit("SendRevokeMessage", map[string]any{"messageType": info.destination})
	}
}

func (a *AutoEmitter) trackOutgoingIq(node *waBinary.Node) {
	ag := node.AttrGetter()
	id := ag.OptionalString("id")
	if id == "" || ag.OptionalString("type") != "set" {
		return
	}
	switch ag.OptionalString("xmlns") {
	case "w:g2":
		if membership := node.GetChildByTag("membership_requests_action"); membership.Tag == "membership_requests_action" {
			action := ""
			if membership.GetChildByTag("approve").Tag == "approve" {
				action = "MEMBERSHIP_REQUEST_APPROVE"
			} else if membership.GetChildByTag("reject").Tag == "reject" {
				action = "MEMBERSHIP_REQUEST_REJECT"
			}
			groupJid := ag.OptionalString("to")
			if groupJid != "" && action != "" {
				a.trackIq(id, pendingIq{kind: "joinRequest", startMs: nowMilli(), groupJid: groupJid, joinRequestAction: action})
			}
			return
		}
		if create := node.GetChildByTag("create"); create.Tag == "create" {
			a.trackIq(id, pendingIq{kind: "groupCreate", hasGroupName: create.AttrGetter().OptionalString("subject") != ""})
			return
		}
		if eph := node.GetChildByTag("ephemeral"); eph.Tag == "ephemeral" {
			if _, ok := eph.AttrGetter().GetString("expiration", false); ok {
				a.trackIq(id, pendingIq{kind: "ephemeral", duration: eph.AttrGetter().OptionalInt("expiration")})
			}
		}
	case "disappearing_mode":
		if dm := node.GetChildByTag("disappearing_mode"); dm.Tag == "disappearing_mode" {
			if _, ok := dm.AttrGetter().GetString("duration", false); ok {
				a.trackIq(id, pendingIq{kind: "disappearingMode", duration: dm.AttrGetter().OptionalInt("duration")})
			}
		}
	}
}

func (a *AutoEmitter) resolveOutgoingIq(node *waBinary.Node, ok bool) {
	id := node.AttrGetter().OptionalString("id")
	if id == "" {
		return
	}
	p, found := a.popIq(id)
	if !found {
		return
	}
	switch p.kind {
	case "joinRequest":
		a.coord.Commit("WaFsGroupJoinRequestAction", map[string]any{
			"groupJid":               p.groupJid,
			"groupJoinRequestAction": p.joinRequestAction,
			"isSuccessful":           ok,
			"serverResponseTime":     max64(0, nowMilli()-p.startMs),
		})
	case "groupCreate":
		if !ok {
			return
		}
		a.coord.Commit("GroupCreate", map[string]any{"hasGroupName": p.hasGroupName})
		a.coord.Commit("GroupCreateC", nil)
	case "ephemeral":
		a.coord.Commit("EphemeralSettingChange", map[string]any{"chatEphemeralityDuration": p.duration, "isSuccess": ok})
	case "disappearingMode":
		a.coord.Commit("DisappearingModeSettingChange", map[string]any{"newEphemeralityDuration": p.duration, "isSuccess": ok})
	}
}

func (a *AutoEmitter) stashRecvEnc(node *waBinary.Node) {
	id := node.AttrGetter().OptionalString("id")
	if id == "" {
		return
	}
	enc := findFirstEncNode(node)
	if enc == nil {
		return
	}
	encAg := enc.AttrGetter()
	info := encInfo{
		ciphertextType: ciphertextTypeKey(encAg.OptionalString("type")),
		version:        encAg.OptionalInt("v"),
		mediaType:      mediaTypeKey(encAg.OptionalString("mediatype")),
		offlineCount:   node.AttrGetter().OptionalInt("offline"),
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.recvEnc == nil {
		a.recvEnc = make(map[string]encInfo)
	}
	evictOne(a.recvEnc, maxTrackedSends)
	a.recvEnc[id] = info
}

func (a *AutoEmitter) popRecvEnc(id string) (encInfo, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.recvEnc[id]
	if ok {
		delete(a.recvEnc, id)
	}
	return e, ok
}

func (a *AutoEmitter) trackSend(id string, info sentInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sentMessages == nil {
		a.sentMessages = make(map[string]sentInfo)
	}
	evictOne(a.sentMessages, maxTrackedSends)
	a.sentMessages[id] = info
}

func (a *AutoEmitter) popSend(id string) (sentInfo, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sentMessages[id]
	if ok {
		delete(a.sentMessages, id)
	}
	return s, ok
}

func (a *AutoEmitter) trackIq(id string, p pendingIq) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingIqs == nil {
		a.pendingIqs = make(map[string]pendingIq)
	}
	evictOne(a.pendingIqs, maxTrackedIQs)
	a.pendingIqs[id] = p
}

func (a *AutoEmitter) popIq(id string) (pendingIq, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pendingIqs[id]
	if ok {
		delete(a.pendingIqs, id)
	}
	return p, ok
}

// evictOne drops one arbitrary entry when the map is at capacity.
// ponytail: arbitrary eviction (not strict LRU) — a bound to cap memory; acks
// arrive fast so the tracked window stays small in practice.
func evictOne[V any](m map[string]V, max int) {
	if len(m) < max {
		return
	}
	for k := range m {
		delete(m, k)
		return
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
