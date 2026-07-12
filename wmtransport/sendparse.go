package wmtransport

import (
	"strings"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// Ported from zapo-js @zapo-js/wam send-parse.ts. Turns whatsmeow binary nodes
// and message protos into the WAM enum keys the registry expects.

// findFirstEncNode does a depth-first search for the first <enc> node (direct,
// group skmsg, or nested under <participants>).
func findFirstEncNode(node *waBinary.Node) *waBinary.Node {
	children, ok := node.Content.([]waBinary.Node)
	if !ok {
		return nil
	}
	for i := range children {
		if children[i].Tag == "enc" {
			return &children[i]
		}
		if nested := findFirstEncNode(&children[i]); nested != nil {
			return nested
		}
	}
	return nil
}

// ciphertextTypeKey maps an <enc type> attr to the E2E_CIPHERTEXT_TYPE enum key.
func ciphertextTypeKey(encType string) string {
	switch encType {
	case "msg":
		return "MESSAGE"
	case "pkmsg":
		return "PREKEY_MESSAGE"
	case "skmsg":
		return "SENDER_KEY_MESSAGE"
	case "msmsg":
		return "MESSAGE_SECRET_MESSAGE"
	default:
		return ""
	}
}

// mediaTypeKey maps an <enc mediatype> attr to the MEDIA_TYPE enum key ("" for
// text / unknown).
func mediaTypeKey(mediatype string) string {
	switch mediatype {
	case "image":
		return "PHOTO"
	case "video":
		return "VIDEO"
	case "audio":
		return "AUDIO"
	case "ptt":
		return "PTT"
	case "document":
		return "DOCUMENT"
	case "sticker":
		return "STICKER"
	case "gif":
		return "GIF"
	case "contact":
		return "CONTACT"
	case "location":
		return "LOCATION"
	default:
		return ""
	}
}

// editTypeKey maps a <message edit> attr to the EDIT_TYPE enum key ("" when the
// message is not an edit/revoke).
func editTypeKey(edit string) string {
	switch types.EditAttribute(edit) {
	case types.EditAttributeMessageEdit, types.EditAttributeAdminEdit:
		return "EDITED"
	case types.EditAttributePinInChat:
		return "PIN"
	case types.EditAttributeSenderRevoke:
		return "SENDER_REVOKE"
	case types.EditAttributeAdminRevoke:
		return "ADMIN_REVOKE"
	default:
		return ""
	}
}

// pinInChatTypeKey maps a PinInChatMessage.Type to its PIN_IN_CHAT_TYPE key.
func pinInChatTypeKey(t waE2E.PinInChatMessage_Type) string {
	switch t {
	case waE2E.PinInChatMessage_PIN_FOR_ALL:
		return "PIN_FOR_ALL"
	case waE2E.PinInChatMessage_UNPIN_FOR_ALL:
		return "UNPIN_FOR_ALL"
	default:
		return ""
	}
}

// fileExtension returns the lowercase extension of a document fileName, or "".
func fileExtension(fileName string) string {
	dot := strings.LastIndex(fileName, ".")
	if dot <= 0 || dot == len(fileName)-1 {
		return ""
	}
	return strings.ToLower(fileName[dot+1:])
}

// documentTypeFor maps a document mimetype to the DOCUMENT_TYPE enum key.
func documentTypeFor(mimetype string) string {
	mime := strings.ToLower(mimetype)
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "IMAGE"
	case strings.HasPrefix(mime, "video/"):
		return "VIDEO"
	case strings.HasPrefix(mime, "audio/"):
		return "AUDIO"
	case strings.Contains(mime, "vcard"):
		return "VCARD"
	case strings.Contains(mime, "zip"), strings.Contains(mime, "rar"),
		strings.Contains(mime, "7z"), strings.Contains(mime, "tar"):
		return "COMPRESSED_FILE"
	case strings.Contains(mime, "msdownload"), strings.Contains(mime, "executable"),
		strings.Contains(mime, "x-msdos"):
		return "EXECUTABLE"
	case strings.Contains(mime, "pdf"), strings.Contains(mime, "word"),
		strings.Contains(mime, "document"), strings.Contains(mime, "sheet"),
		strings.Contains(mime, "presentation"), strings.HasPrefix(mime, "text/"):
		return "DOCUMENT"
	default:
		return "OTHER"
	}
}
