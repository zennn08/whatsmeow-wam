package wmtransport

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func newAEAppState() (*AutoEmitter, *recorder) {
	ae, r := newAE(&store.Device{})
	ae.EmitAppStateActions = true
	return ae, r
}

func TestAppStateDisabledByDefault(t *testing.T) {
	ae, r := newAE(&store.Device{}) // EmitAppStateActions false
	ae.Handle(&events.Mute{JID: types.NewJID("1", types.DefaultUserServer),
		Action: &waSyncAction.MuteAction{Muted: proto.Bool(true)}})
	if len(r.commits) != 0 {
		t.Fatalf("app-state actions should be off by default, got %v", r.names())
	}
}

func TestFullSyncSkipped(t *testing.T) {
	ae, r := newAEAppState()
	ae.Handle(&events.Mute{JID: types.NewJID("1", types.DefaultUserServer), FromFullSync: true,
		Action: &waSyncAction.MuteAction{Muted: proto.Bool(true)}})
	if len(r.commits) != 0 {
		t.Fatalf("full-sync app-state should be skipped, got %v", r.names())
	}
}

func TestMute(t *testing.T) {
	ae, r := newAEAppState()
	group := types.NewJID("g", types.GroupServer)
	ae.Handle(&events.Mute{JID: group, Action: &waSyncAction.MuteAction{Muted: proto.Bool(true)}})
	cm := r.find("ChatMute")
	if cm == nil || cm["actionConducted"] != "MUTE" || cm["muteChatType"] != "GROUP" {
		t.Fatalf("ChatMute = %v", cm)
	}
	ca := r.find("ChatAction")
	if ca == nil || ca["chatActionType"] != "MUTE" || ca["chatActionChatType"] != "GROUP" {
		t.Fatalf("ChatAction = %v", ca)
	}

	r.commits = nil
	ae.Handle(&events.Mute{JID: types.NewJID("1", types.DefaultUserServer),
		Action: &waSyncAction.MuteAction{Muted: proto.Bool(false)}})
	if r.find("ChatMute")["actionConducted"] != "UNMUTE" {
		t.Errorf("unmute should be UNMUTE")
	}
	if r.find("ChatMute")["muteChatType"] != "ONE_ON_ONE" {
		t.Errorf("dm mute chat type should be ONE_ON_ONE")
	}
}

func TestPinAndArchive(t *testing.T) {
	ae, r := newAEAppState()
	jid := types.NewJID("1", types.DefaultUserServer)
	ae.Handle(&events.Pin{JID: jid, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}})
	if r.find("ChatAction")["chatActionType"] != "PIN" || !r.has("MdSyncdDogfoodingFeatureUsage") {
		t.Errorf("pin should emit ChatAction(PIN) + dogfooding; got %v", r.names())
	}
	if r.find("MdSyncdDogfoodingFeatureUsage")["mdSyncdDogfoodingFeature"] != "PIN_MUTATION" {
		t.Errorf("pin dogfooding feature wrong")
	}

	// Unpin → nothing.
	r.commits = nil
	ae.Handle(&events.Pin{JID: jid, Action: &waSyncAction.PinAction{Pinned: proto.Bool(false)}})
	if len(r.commits) != 0 {
		t.Errorf("unpin should emit nothing, got %v", r.names())
	}

	ae.Handle(&events.Archive{JID: jid, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}})
	if r.find("ChatAction")["chatActionType"] != "ARCHIVE" {
		t.Errorf("archive should emit ChatAction(ARCHIVE)")
	}
}

func TestMarkReadAndStatusMute(t *testing.T) {
	ae, r := newAEAppState()
	jid := types.NewJID("1", types.DefaultUserServer)
	ae.Handle(&events.MarkChatAsRead{JID: jid, Action: &waSyncAction.MarkChatAsReadAction{Read: proto.Bool(true)}})
	if r.find("ChatAction")["chatActionType"] != "READ" {
		t.Errorf("mark read should be READ")
	}
	r.commits = nil
	ae.Handle(&events.UserStatusMute{JID: jid, Action: &waSyncAction.UserStatusMuteAction{Muted: proto.Bool(true)}})
	sm := r.find("StatusMute")
	if sm == nil || sm["muteAction"] != "MUTE" || sm["statusCategory"] != "REGULAR_STATUS" {
		t.Fatalf("StatusMute = %v", sm)
	}
}

func TestDeleteAndClearChat(t *testing.T) {
	ae, r := newAEAppState()
	jid := types.NewJID("1", types.DefaultUserServer)
	ae.Handle(&events.DeleteChat{JID: jid, Action: &waSyncAction.DeleteChatAction{}})
	if r.find("MdSyncdDogfoodingFeatureUsage")["mdSyncdDogfoodingFeature"] != "DELETE_MUTATION" {
		t.Errorf("delete chat feature wrong: %v", r.names())
	}
	r.commits = nil
	ae.Handle(&events.ClearChat{JID: jid, Action: &waSyncAction.ClearChatAction{}})
	if r.find("MdSyncdDogfoodingFeatureUsage")["mdSyncdDogfoodingFeature"] != "CLEAR_CHAT_KEEP_STARRED_MUTATION" {
		t.Errorf("clear chat feature wrong")
	}
}
