package wmtransport

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestSendSticker(t *testing.T) {
	ae, r := newAE(&store.Device{})
	dm := types.NewJID("1", types.DefaultUserServer)
	ae.OnSendMessage(dm, &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		IsAnimated: proto.Bool(true),
		IsLottie:   proto.Bool(false),
	}})
	s := r.find("StickerSend")
	if s == nil || s["stickerIsAnimated"] != true || s["stickerIsLottie"] != false {
		t.Fatalf("StickerSend = %v", s)
	}
}

func TestSendDocument(t *testing.T) {
	ae, r := newAE(&store.Device{})
	dm := types.NewJID("1", types.DefaultUserServer)
	ae.OnSendMessage(dm, &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		Mimetype:   proto.String("application/pdf"),
		FileName:   proto.String("report.PDF"),
		PageCount:  proto.Uint32(12),
		FileLength: proto.Uint64(2048),
	}})
	d := r.find("SendDocument")
	if d == nil || d["documentType"] != "DOCUMENT" || d["documentExt"] != "pdf" ||
		d["documentPageSize"] != 12 || d["documentSize"] != int64(2048) {
		t.Fatalf("SendDocument = %v", d)
	}
}

func TestSendReaction(t *testing.T) {
	ae, r := newAE(&store.Device{})
	dm := types.NewJID("1", types.DefaultUserServer)
	ae.OnSendMessage(dm, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}})
	if r.find("ReactionActions")["reactionAction"] != "UPDATE" {
		t.Errorf("reaction with text should be UPDATE")
	}
	r.commits = nil
	ae.OnSendMessage(dm, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("")}})
	if r.find("ReactionActions")["reactionAction"] != "DELETE" {
		t.Errorf("reaction with empty text should be DELETE")
	}
}

func TestSendPollCreate(t *testing.T) {
	ae, r := newAE(&store.Device{})
	group := types.NewJID("g", types.GroupServer)
	ae.OnSendMessage(group, &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{
		Options: []*waE2E.PollCreationMessage_Option{{}, {}, {}},
	}})
	p := r.find("PollsActions")
	if p == nil || p["pollAction"] != "CREATE_POLL" || p["chatType"] != "GROUP" ||
		p["isAGroup"] != true || p["pollOptionsCount"] != 3 || p["typeOfGroup"] != "GROUP" {
		t.Fatalf("PollsActions = %v", p)
	}
}

func TestSendForwardedImage(t *testing.T) {
	ae, r := newAE(&store.Device{})
	dm := types.NewJID("1", types.DefaultUserServer)
	ae.OnSendMessage(dm, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		ContextInfo: &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(5)},
	}})
	f := r.find("ForwardSend")
	if f == nil || f["messageType"] != "INDIVIDUAL" || f["isFrequentlyForwarded"] != true ||
		f["isForwardedForward"] != true || f["messageMediaType"] != "PHOTO" {
		t.Fatalf("ForwardSend = %v", f)
	}
}

func TestSendPinInChat(t *testing.T) {
	ae, r := newAE(&store.Device{})
	group := types.NewJID("g", types.GroupServer)
	ae.OnSendMessage(group, &waE2E.Message{PinInChatMessage: &waE2E.PinInChatMessage{
		Type: waE2E.PinInChatMessage_PIN_FOR_ALL.Enum(),
		Key:  &waCommon.MessageKey{FromMe: proto.Bool(true)},
	}})
	p := r.find("PinInChatMessageSend")
	if p == nil || p["pinInChatType"] != "PIN_FOR_ALL" || p["isAGroup"] != true ||
		p["isSelfPin"] != true || p["isSelfParentMessage"] != true {
		t.Fatalf("PinInChatMessageSend = %v", p)
	}
}

func TestUnwrapEphemeral(t *testing.T) {
	ae, r := newAE(&store.Device{})
	dm := types.NewJID("1", types.DefaultUserServer)
	// Sticker wrapped in an ephemeral wrapper still resolves to StickerSend.
	ae.OnSendMessage(dm, &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{IsAnimated: proto.Bool(false)}},
	}})
	if !r.has("StickerSend") {
		t.Error("ephemeral-wrapped sticker should still emit StickerSend")
	}
}
