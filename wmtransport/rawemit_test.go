package wmtransport

import (
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func msgOut(to types.JID, id, edit string, enc waBinary.Node) *waBinary.Node {
	attrs := waBinary.Attrs{"to": to, "id": id}
	if edit != "" {
		attrs["edit"] = edit
	}
	return &waBinary.Node{Tag: "message", Attrs: attrs, Content: []waBinary.Node{enc}}
}

func encNode(t, v, mediatype string) waBinary.Node {
	a := waBinary.Attrs{"type": t}
	if v != "" {
		a["v"] = v
	}
	if mediatype != "" {
		a["mediatype"] = mediatype
	}
	return waBinary.Node{Tag: "enc", Attrs: a}
}

func TestOutboundMessageAndAck(t *testing.T) {
	ae, r := newAE(&store.Device{})
	group := types.NewJID("123", types.GroupServer)
	ae.onNodeOut(msgOut(group, "M1", "", encNode("pkmsg", "2", "image")))

	e2e := r.find("E2eMessageSend")
	if e2e == nil || e2e["e2eDestination"] != "GROUP" || e2e["typeOfGroup"] != "GROUP" ||
		e2e["e2eCiphertextType"] != "PREKEY_MESSAGE" || e2e["e2eCiphertextVersion"] != 2 ||
		e2e["messageMediaType"] != "PHOTO" || e2e["editType"] != "NOT_EDITED" {
		t.Fatalf("E2eMessageSend = %v", e2e)
	}
	if wm := r.find("WebcMessageSend"); wm == nil || wm["messageType"] != "GROUP" || wm["messageMediaType"] != "PHOTO" {
		t.Fatalf("WebcMessageSend = %v", wm)
	}

	// The <ack> completes the send → MessageSend using tracked context.
	r.commits = nil
	ae.onNodeIn(&waBinary.Node{Tag: "ack", Attrs: waBinary.Attrs{"class": "message", "id": "M1"}}, true)
	ms := r.find("MessageSend")
	if ms == nil || ms["messageSendResult"] != "OK" || ms["messageType"] != "GROUP" ||
		ms["e2eCiphertextType"] != "PREKEY_MESSAGE" || ms["messageMediaType"] != "PHOTO" {
		t.Fatalf("MessageSend = %v", ms)
	}
	if r.has("EditMessageSend") {
		t.Error("non-edit message should not emit EditMessageSend")
	}
}

func TestRevokeAckChain(t *testing.T) {
	ae, r := newAE(&store.Device{})
	dm := types.NewJID("123", types.DefaultUserServer)
	ae.onNodeOut(msgOut(dm, "R1", "7", encNode("msg", "", ""))) // edit=7 → SENDER_REVOKE
	r.commits = nil
	ae.onNodeIn(&waBinary.Node{Tag: "ack", Attrs: waBinary.Attrs{"class": "message", "id": "R1"}}, true)

	for _, ev := range []string{"MessageSend", "EditMessageSend", "RevokeMessageSend", "MessageDeleteActions", "SendRevokeMessage"} {
		if !r.has(ev) {
			t.Errorf("revoke ack should emit %s; commits=%v", ev, r.names())
		}
	}
	if r.find("MessageSend")["messageIsRevoke"] != true {
		t.Error("MessageSend.messageIsRevoke should be true for a revoke")
	}
	if r.find("RevokeMessageSend")["revokeType"] != "SENDER" {
		t.Errorf("revokeType = %v, want SENDER", r.find("RevokeMessageSend")["revokeType"])
	}
}

func TestReceiptRetryHighCount(t *testing.T) {
	ae, r := newAE(&store.Device{})
	from := types.NewJID("123", types.DefaultUserServer)
	node := &waBinary.Node{Tag: "receipt", Attrs: waBinary.Attrs{"type": "retry", "from": from},
		Content: []waBinary.Node{{Tag: "retry", Attrs: waBinary.Attrs{"count": "5"}}}}
	ae.onNodeIn(node, true)
	if hr := r.find("MessageHighRetryCount"); hr == nil || hr["retryCount"] != 5 || hr["messageType"] != "INDIVIDUAL" {
		t.Fatalf("MessageHighRetryCount = %v", hr)
	}

	// Below threshold → nothing.
	r.commits = nil
	node.Content = []waBinary.Node{{Tag: "retry", Attrs: waBinary.Attrs{"count": "4"}}}
	ae.onNodeIn(node, true)
	if r.has("MessageHighRetryCount") {
		t.Error("retry count 4 should not fire MessageHighRetryCount")
	}
}

func TestWaOldCode(t *testing.T) {
	ae, r := newAE(&store.Device{})
	node := &waBinary.Node{Tag: "notification", Attrs: waBinary.Attrs{"type": "account_sync"},
		Content: []waBinary.Node{{Tag: "wa_old_registration", Attrs: waBinary.Attrs{"device_id": "7"}}}}
	ae.onNodeIn(node, true)
	if oc := r.find("WaOldCode"); oc == nil || oc["deviceId"] != "7" {
		t.Fatalf("WaOldCode = %v", oc)
	}
}

func TestClockSkewOnce(t *testing.T) {
	orig := nowMilli
	nowMilli = func() int64 { return 2_000_000_000 * 1000 } // fixed "now" in ms
	defer func() { nowMilli = orig }()

	ae, r := newAE(&store.Device{})
	// server t two hours behind now → skew ~ +2h.
	ae.onNodeIn(&waBinary.Node{Tag: "ib", Attrs: waBinary.Attrs{"t": "1999992800"}}, true)
	cs := r.find("ClockSkewDifferenceT")
	if cs == nil || cs["clockSkewHourly"] != 2 {
		t.Fatalf("ClockSkewDifferenceT = %v (want hourly 2)", cs)
	}
	// Second node must not re-report.
	r.commits = nil
	ae.onNodeIn(&waBinary.Node{Tag: "ib", Attrs: waBinary.Attrs{"t": "1999992800"}}, true)
	if r.has("ClockSkewDifferenceT") {
		t.Error("clock skew should only report once")
	}
}

func TestUnknownStanza(t *testing.T) {
	ae, r := newAE(&store.Device{})
	ae.onNodeIn(&waBinary.Node{Tag: "weird", Attrs: waBinary.Attrs{"type": "x"}}, false)
	us := r.find("UnknownStanza")
	if us == nil || us["unknownStanzaTag"] != "weird" || us["unknownStanzaType"] != "x" {
		t.Fatalf("UnknownStanza = %v", us)
	}
	// A handled node must not emit UnknownStanza.
	r.commits = nil
	ae.onNodeIn(&waBinary.Node{Tag: "weird"}, true)
	if r.has("UnknownStanza") {
		t.Error("handled node should not emit UnknownStanza")
	}
}

func TestIqGroupCreateRoundTrip(t *testing.T) {
	ae, r := newAE(&store.Device{})
	group := types.NewJID("", types.GroupServer)
	iqOut := &waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"id": "IQ1", "type": "set", "xmlns": "w:g2", "to": group},
		Content: []waBinary.Node{{Tag: "create", Attrs: waBinary.Attrs{"subject": "My Group"}}}}
	ae.onNodeOut(iqOut)
	ae.onNodeIn(&waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"id": "IQ1", "type": "result"}}, true)

	if gc := r.find("GroupCreate"); gc == nil || gc["hasGroupName"] != true {
		t.Fatalf("GroupCreate = %v", gc)
	}
	if !r.has("GroupCreateC") {
		t.Error("GroupCreate should be followed by GroupCreateC")
	}
}

func TestIqEphemeralRoundTrip(t *testing.T) {
	ae, r := newAE(&store.Device{})
	group := types.NewJID("g", types.GroupServer)
	iqOut := &waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"id": "E1", "type": "set", "xmlns": "w:g2", "to": group},
		Content: []waBinary.Node{{Tag: "ephemeral", Attrs: waBinary.Attrs{"expiration": "86400"}}}}
	ae.onNodeOut(iqOut)
	ae.onNodeIn(&waBinary.Node{Tag: "iq", Attrs: waBinary.Attrs{"id": "E1", "type": "result"}}, true)
	esc := r.find("EphemeralSettingChange")
	if esc == nil || esc["chatEphemeralityDuration"] != 86400 || esc["isSuccess"] != true {
		t.Fatalf("EphemeralSettingChange = %v", esc)
	}
}

func TestRecvEncEnrichmentAndOfflineCount(t *testing.T) {
	ae, r := newAE(&store.Device{})
	dm := types.NewJID("123", types.DefaultUserServer)
	// Raw inbound <message> stashes enc + offline count.
	in := &waBinary.Node{Tag: "message", Attrs: waBinary.Attrs{"id": "IN1", "from": dm, "offline": "12"},
		Content: []waBinary.Node{encNode("pkmsg", "2", "image")}}
	ae.onNodeIn(in, true)

	// The typed Message event with the same id enriches E2eMessageRecv.
	ae.Handle(&events.Message{Info: types.MessageInfo{
		ID:            "IN1",
		MessageSource: types.MessageSource{Chat: dm},
	}})

	e2e := r.find("E2eMessageRecv")
	if e2e == nil || e2e["e2eCiphertextType"] != "PREKEY_MESSAGE" || e2e["e2eCiphertextVersion"] != 2 || e2e["messageMediaType"] != "PHOTO" {
		t.Fatalf("E2eMessageRecv enrichment = %v", e2e)
	}
	if oc := r.find("OfflineCountTooHigh"); oc == nil || oc["offlineCount"] != 12 || oc["mediaType"] != "PHOTO" {
		t.Fatalf("OfflineCountTooHigh = %v", oc)
	}
}
