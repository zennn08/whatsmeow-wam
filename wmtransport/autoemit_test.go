package wmtransport

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	wam "github.com/zennn08/whatsmeow-wam"
)

type transportFunc func(context.Context, []byte) error

func (f transportFunc) Upload(ctx context.Context, b []byte) error { return f(ctx, b) }

type commit struct {
	event   string
	payload map[string]any
}

type recorder struct{ commits []commit }

func (r *recorder) Commit(event string, payload map[string]any) {
	r.commits = append(r.commits, commit{event, payload})
}

func (r *recorder) names() []string {
	out := make([]string, len(r.commits))
	for i, c := range r.commits {
		out[i] = c.event
	}
	return out
}

func (r *recorder) find(event string) map[string]any {
	for _, c := range r.commits {
		if c.event == event {
			return c.payload
		}
	}
	return nil
}

func (r *recorder) has(event string) bool {
	for _, c := range r.commits {
		if c.event == event {
			return true
		}
	}
	return false
}

func newAE(dev *store.Device) (*AutoEmitter, *recorder) {
	r := &recorder{}
	return &AutoEmitter{coord: r, dev: dev}, r
}

func TestConnectSequence(t *testing.T) {
	dev := &store.Device{Platform: "smba"}
	ae, r := newAE(dev)

	ae.Handle(&events.Connected{})
	if got := r.names(); len(got) != 3 || got[0] != "WebcSocketConnect" || got[1] != "WebcStreamModeChange" || got[2] != "WebcRawPlatforms" {
		t.Fatalf("first connect commits = %v", got)
	}
	if reason := r.find("WebcSocketConnect")["webcSocketConnectReason"]; reason != "PAGE_LOAD" {
		t.Errorf("first connect reason = %v, want PAGE_LOAD", reason)
	}
	if r.find("WebcStreamModeChange")["webcStreamMode"] != "SYNCING" {
		t.Errorf("stream mode = %v, want SYNCING", r.find("WebcStreamModeChange")["webcStreamMode"])
	}

	// Second connect: RECONNECT + WebcPageResume(1). Platform already reported (once).
	r.commits = nil
	ae.Handle(&events.Connected{})
	if r.find("WebcSocketConnect")["webcSocketConnectReason"] != "RECONNECT" {
		t.Errorf("second connect reason = %v, want RECONNECT", r.find("WebcSocketConnect")["webcSocketConnectReason"])
	}
	if pr := r.find("WebcPageResume"); pr == nil || pr["webcResumeCount"] != 1 {
		t.Errorf("WebcPageResume = %v, want count 1", pr)
	}
	if r.find("WebcRawPlatforms") != nil {
		t.Error("platform should only be reported once")
	}
	// Stream mode already SYNCING → deduped, no new WebcStreamModeChange.
	if r.find("WebcStreamModeChange") != nil {
		t.Error("stream mode SYNCING should be deduped on reconnect")
	}
}

func TestOfflineWindowMarksMessages(t *testing.T) {
	ae, r := newAE(&store.Device{})
	ae.Handle(&events.Connected{}) // inOfflineSync = true
	r.commits = nil

	dm := events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{
		Chat: types.NewJID("123", types.DefaultUserServer),
	}}}
	ae.Handle(&dm)
	if r.find("MessageReceive")["messageIsOffline"] != true {
		t.Error("message during offline sync should be marked offline")
	}
	if r.find("E2eMessageRecv")["offline"] != true {
		t.Error("e2e recv during offline sync should be marked offline")
	}

	// After offline sync completes: MAIN + subsequent messages not offline.
	r.commits = nil
	ae.Handle(&events.OfflineSyncCompleted{Count: 3})
	if r.find("WebcStreamModeChange")["webcStreamMode"] != "MAIN" {
		t.Error("offline sync completed should switch stream mode to MAIN")
	}
	r.commits = nil
	ae.Handle(&dm)
	if r.find("MessageReceive")["messageIsOffline"] != false {
		t.Error("message after offline sync should not be offline")
	}
}

func TestMessageFieldMapping(t *testing.T) {
	ae, r := newAE(&store.Device{})
	group := events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{
		Chat:           types.NewJID("123", types.GroupServer),
		IsGroup:        true,
		AddressingMode: types.AddressingModeLID,
	}}, RetryCount: 2}
	ae.Handle(&group)

	e2e := r.find("E2eMessageRecv")
	if e2e["e2eDestination"] != "GROUP" || e2e["typeOfGroup"] != "GROUP" || e2e["isLid"] != true {
		t.Errorf("E2eMessageRecv fields = %v", e2e)
	}
	if e2e["retryCount"] != 2 {
		t.Errorf("retryCount = %v, want 2", e2e["retryCount"])
	}
	if e2e["e2eSuccessful"] != true {
		t.Error("decrypted message should be e2eSuccessful")
	}
	if r.find("MessageReceive")["messageType"] != "GROUP" {
		t.Errorf("messageType = %v, want GROUP", r.find("MessageReceive")["messageType"])
	}
}

func TestUndecryptableMessage(t *testing.T) {
	ae, r := newAE(&store.Device{})
	ae.Handle(&events.UndecryptableMessage{Info: types.MessageInfo{MessageSource: types.MessageSource{
		Chat: types.NewJID("123", types.DefaultUserServer),
	}}})
	if r.find("E2eMessageRecv")["e2eSuccessful"] != false {
		t.Error("undecryptable message should be e2eSuccessful=false")
	}
}

func TestReceiptMapping(t *testing.T) {
	ae, r := newAE(&store.Device{})
	ae.Handle(&events.Receipt{Type: types.ReceiptTypeRead, MessageIDs: []types.MessageID{"a", "b", "c"}})
	rc := r.find("ReceiptStanzaReceive")
	if rc["receiptStanzaType"] != "read" || rc["receiptStanzaTotalCount"] != 3 {
		t.Errorf("receipt = %v", rc)
	}
	// Delivered receipt has empty type → normalized to "delivery".
	r.commits = nil
	ae.Handle(&events.Receipt{Type: types.ReceiptTypeDelivered, MessageIDs: []types.MessageID{"a"}})
	if r.find("ReceiptStanzaReceive")["receiptStanzaType"] != "delivery" {
		t.Errorf("delivered type = %v, want delivery", r.find("ReceiptStanzaReceive")["receiptStanzaType"])
	}
}

func TestJoinedGroupSkipsSelfCreated(t *testing.T) {
	self := types.NewJID("999", types.DefaultUserServer)
	ae, r := newAE(&store.Device{ID: &self})

	// Created by self → skip.
	ae.Handle(&events.JoinedGroup{Sender: &self})
	if r.has("GroupJoinC") {
		t.Error("self-created group should not emit GroupJoinC")
	}
	// Added by someone else → emit.
	other := types.NewJID("111", types.DefaultUserServer)
	ae.Handle(&events.JoinedGroup{Sender: &other})
	if !r.has("GroupJoinC") {
		t.Error("group joined via other should emit GroupJoinC")
	}
}

func TestHistorySyncMapping(t *testing.T) {
	ae, r := newAE(&store.Device{})
	ae.Handle(&events.HistorySync{Data: &waHistorySync.HistorySync{
		ChunkOrder: proto.Uint32(4),
		Progress:   proto.Uint32(55),
	}})
	hs := r.find("MdBootstrapHistoryDataReceived")
	if hs["historySyncChunkOrder"] != 4 || hs["historySyncStageProgress"] != 55 {
		t.Errorf("history sync = %v", hs)
	}
}

func TestDestAndMsgTypeKeys(t *testing.T) {
	status := types.StatusBroadcastJID
	cases := []struct {
		jid     types.JID
		dest    string
		msgType string
	}{
		{types.NewJID("1", types.DefaultUserServer), "INDIVIDUAL", "INDIVIDUAL"},
		{types.NewJID("1", types.GroupServer), "GROUP", "GROUP"},
		{types.NewJID("1", types.NewsletterServer), "CHANNEL", "CHANNEL"},
		{status, "STATUS", "STATUS"},
		{types.NewJID("1", types.BroadcastServer), "INDIVIDUAL", "BROADCAST"},
	}
	for _, c := range cases {
		if got := destKey(c.jid); got != c.dest {
			t.Errorf("destKey(%s) = %s, want %s", c.jid, got, c.dest)
		}
		if got := msgTypeKey(types.MessageSource{Chat: c.jid}); got != c.msgType {
			t.Errorf("msgTypeKey(%s) = %s, want %s", c.jid, got, c.msgType)
		}
	}
}

// TestAutoEmitterThroughRealCoordinator drives events through the concrete
// wam.Coordinator (encoder + batching) and asserts a valid WAM batch uploads.
func TestAutoEmitterThroughRealCoordinator(t *testing.T) {
	var uploaded [][]byte
	coord := wam.New(wam.Options{
		DisableSampling: true,
		Transport:       transportFunc(func(_ context.Context, b []byte) error { uploaded = append(uploaded, b); return nil }),
	})
	ae := &AutoEmitter{coord: coord, dev: &store.Device{Platform: "smba"}}

	ae.Handle(&events.Connected{})
	ae.Handle(&events.Receipt{Type: types.ReceiptTypeRead, MessageIDs: []types.MessageID{"a"}})
	coord.Flush(context.Background())

	if len(uploaded) == 0 {
		t.Fatal("no batch uploaded")
	}
	if string(uploaded[0][:3]) != "WAM" {
		t.Fatalf("batch missing WAM magic: %x", uploaded[0][:8])
	}
}
