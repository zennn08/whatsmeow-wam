package wmtransport

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// OnSendMessage commits the content-derived outbound WAM events WA Web fires when
// you send a message (ForwardSend, ReactionActions, PollsActions, SendDocument,
// StickerSend, PinInChatMessageSend), mirroring zapo-js's onMessageSend.
//
// whatsmeow has no outbound-message event, so call this yourself right before (or
// after) cli.SendMessage(ctx, to, msg) with the same arguments. The stanza-level
// send events (E2eMessageSend / WebcMessageSend / MessageSend) come automatically
// from the raw-node hook.
func (a *AutoEmitter) OnSendMessage(to types.JID, msg *waE2E.Message) {
	if msg == nil {
		return
	}
	msg = unwrapMessage(msg)
	destination := destKey(to)
	isGroup := to.Server == types.GroupServer

	if ci := messageContextInfo(msg); ci != nil && ci.GetIsForwarded() {
		score := ci.GetForwardingScore()
		fwd := map[string]any{
			"messageType":           destination,
			"isFrequentlyForwarded": score >= 4,
			"isForwardedForward":    score > 1,
		}
		if media := mediaTypeOfMessage(msg); media != "" {
			fwd["messageMediaType"] = media
		}
		if isGroup {
			fwd["typeOfGroup"] = "GROUP"
		}
		a.coord.Commit("ForwardSend", fwd)
	}

	if r := msg.GetReactionMessage(); r != nil {
		action := "DELETE"
		if r.GetText() != "" {
			action = "UPDATE"
		}
		a.coord.Commit("ReactionActions", map[string]any{"reactionAction": action, "messageType": destination})
		return
	}

	if poll := firstPoll(msg); poll != nil {
		p := map[string]any{
			"pollAction": "CREATE_POLL",
			"chatType":   pollChatTypeKey(to),
			"isAGroup":   isGroup,
		}
		if opts := poll.GetOptions(); opts != nil {
			p["pollOptionsCount"] = len(opts)
		}
		if isGroup {
			p["typeOfGroup"] = "GROUP"
		}
		a.coord.Commit("PollsActions", p)
		return
	}
	if msg.GetPollUpdateMessage() != nil {
		p := map[string]any{"pollAction": "VOTE", "chatType": pollChatTypeKey(to), "isAGroup": isGroup}
		if isGroup {
			p["typeOfGroup"] = "GROUP"
		}
		a.coord.Commit("PollsActions", p)
		return
	}

	if doc := msg.GetDocumentMessage(); doc != nil {
		d := map[string]any{"documentType": documentTypeFor(doc.GetMimetype())}
		if ext := fileExtension(doc.GetFileName()); ext != "" {
			d["documentExt"] = ext
		}
		if pc := doc.GetPageCount(); pc != 0 {
			d["documentPageSize"] = int(pc)
		}
		if fl := doc.GetFileLength(); fl != 0 {
			d["documentSize"] = int64(fl)
		}
		a.coord.Commit("SendDocument", d)
		return
	}

	if s := msg.GetStickerMessage(); s != nil {
		a.coord.Commit("StickerSend", map[string]any{
			"stickerIsAnimated": s.GetIsAnimated(),
			"stickerIsLottie":   s.GetIsLottie(),
		})
		return
	}

	if pin := msg.GetPinInChatMessage(); pin != nil {
		p := map[string]any{"isAGroup": isGroup, "isSelfPin": true}
		if pt := pinInChatTypeKey(pin.GetType()); pt != "" {
			p["pinInChatType"] = pt
		}
		if pin.GetKey() != nil {
			p["isSelfParentMessage"] = pin.GetKey().GetFromMe()
		}
		a.coord.Commit("PinInChatMessageSend", p)
		return
	}
}

// unwrapMessage peels the wrapper messages WA nests content in, returning the
// innermost payload.
func unwrapMessage(msg *waE2E.Message) *waE2E.Message {
	for range 8 { // bounded: avoid a pathological self-referential loop
		switch {
		case msg.GetEphemeralMessage().GetMessage() != nil:
			msg = msg.GetEphemeralMessage().GetMessage()
		case msg.GetViewOnceMessage().GetMessage() != nil:
			msg = msg.GetViewOnceMessage().GetMessage()
		case msg.GetViewOnceMessageV2().GetMessage() != nil:
			msg = msg.GetViewOnceMessageV2().GetMessage()
		case msg.GetViewOnceMessageV2Extension().GetMessage() != nil:
			msg = msg.GetViewOnceMessageV2Extension().GetMessage()
		case msg.GetDocumentWithCaptionMessage().GetMessage() != nil:
			msg = msg.GetDocumentWithCaptionMessage().GetMessage()
		case msg.GetEditedMessage().GetMessage() != nil:
			msg = msg.GetEditedMessage().GetMessage()
		case msg.GetDeviceSentMessage().GetMessage() != nil:
			msg = msg.GetDeviceSentMessage().GetMessage()
		default:
			return msg
		}
	}
	return msg
}

// messageContextInfo returns the ContextInfo of whichever sub-message carries one
// (enough of the forward-capable types to derive ForwardSend).
func messageContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	case msg.GetContactMessage() != nil:
		return msg.GetContactMessage().GetContextInfo()
	case msg.GetLocationMessage() != nil:
		return msg.GetLocationMessage().GetContextInfo()
	case msg.GetLiveLocationMessage() != nil:
		return msg.GetLiveLocationMessage().GetContextInfo()
	default:
		return nil
	}
}

// mediaTypeOfMessage returns the WAM MEDIA_TYPE key for a message ("" for non-media).
func mediaTypeOfMessage(msg *waE2E.Message) string {
	switch {
	case msg.GetImageMessage() != nil:
		return "PHOTO"
	case msg.GetVideoMessage() != nil:
		if msg.GetVideoMessage().GetGifPlayback() {
			return "GIF"
		}
		return "VIDEO"
	case msg.GetAudioMessage() != nil:
		if msg.GetAudioMessage().GetPTT() {
			return "PTT"
		}
		return "AUDIO"
	case msg.GetDocumentMessage() != nil:
		return "DOCUMENT"
	case msg.GetStickerMessage() != nil:
		return "STICKER"
	case msg.GetContactMessage() != nil:
		return "CONTACT"
	case msg.GetLocationMessage() != nil, msg.GetLiveLocationMessage() != nil:
		return "LOCATION"
	default:
		return ""
	}
}

func firstPoll(msg *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{
		msg.GetPollCreationMessage(),
		msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(),
		msg.GetPollCreationMessageV5(),
	} {
		if p != nil {
			return p
		}
	}
	return nil
}

// pollChatTypeKey maps a chat JID to the POLL chatType key.
func pollChatTypeKey(chat types.JID) string {
	switch {
	case chat.Server == types.GroupServer:
		return "GROUP"
	case chat.Server == types.NewsletterServer:
		return "CHANNEL"
	case chat.Server == types.BroadcastServer && chat.User == types.StatusBroadcastJID.User:
		return "STATUS"
	default:
		return "INDIVIDUAL"
	}
}
