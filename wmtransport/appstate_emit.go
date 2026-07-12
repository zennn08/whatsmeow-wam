package wmtransport

import (
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// App-state integrator actions (Tier A), mirroring zapo-js's onMutationSend.
// Gated by EmitAppStateActions and skipped for full-sync events, since whatsmeow
// emits these on sync (from any device) rather than on this client's own action.

func (a *AutoEmitter) onMute(e *events.Mute) {
	if !a.EmitAppStateActions || e.FromFullSync {
		return
	}
	muted := e.Action.GetMuted()
	action := "UNMUTE"
	if muted {
		action = "MUTE"
	}
	chatMute := map[string]any{
		"actionConducted": action,
		"muteChatType":    muteChatTypeKey(e.JID),
	}
	if muted {
		if end := e.Action.GetMuteEndTimestamp(); end > 0 {
			chatMute["muteDuration"] = max64(0, end-nowMilli())
		}
	}
	a.coord.Commit("ChatMute", chatMute)
	a.commitChatAction(action, e.JID)
}

func (a *AutoEmitter) onPin(e *events.Pin) {
	if !a.EmitAppStateActions || e.FromFullSync || !e.Action.GetPinned() {
		return
	}
	a.commitChatAction("PIN", e.JID)
	a.coord.Commit("MdSyncdDogfoodingFeatureUsage", map[string]any{"mdSyncdDogfoodingFeature": "PIN_MUTATION"})
}

func (a *AutoEmitter) onArchive(e *events.Archive) {
	if !a.EmitAppStateActions || e.FromFullSync || !e.Action.GetArchived() {
		return
	}
	a.commitChatAction("ARCHIVE", e.JID)
}

func (a *AutoEmitter) onMarkChatAsRead(e *events.MarkChatAsRead) {
	if !a.EmitAppStateActions || e.FromFullSync {
		return
	}
	action := "UNREAD"
	if e.Action.GetRead() {
		action = "READ"
	}
	a.commitChatAction(action, e.JID)
}

func (a *AutoEmitter) onDeleteChat(e *events.DeleteChat) {
	if !a.EmitAppStateActions || e.FromFullSync {
		return
	}
	a.coord.Commit("MdSyncdDogfoodingFeatureUsage", map[string]any{"mdSyncdDogfoodingFeature": "DELETE_MUTATION"})
}

func (a *AutoEmitter) onClearChat(e *events.ClearChat) {
	if !a.EmitAppStateActions || e.FromFullSync {
		return
	}
	// whatsmeow's ClearChatAction carries no "delete starred" flag, so we report
	// the keep-starred variant (the common case).
	a.coord.Commit("MdSyncdDogfoodingFeatureUsage", map[string]any{"mdSyncdDogfoodingFeature": "CLEAR_CHAT_KEEP_STARRED_MUTATION"})
}

func (a *AutoEmitter) onUserStatusMute(e *events.UserStatusMute) {
	if !a.EmitAppStateActions || e.FromFullSync {
		return
	}
	action := "UNMUTE"
	if e.Action.GetMuted() {
		action = "MUTE"
	}
	a.coord.Commit("StatusMute", map[string]any{"muteAction": action, "statusCategory": "REGULAR_STATUS"})
}

func (a *AutoEmitter) commitChatAction(chatActionType string, jid types.JID) {
	a.coord.Commit("ChatAction", map[string]any{
		"chatActionType":     chatActionType,
		"chatActionChatType": chatActionChatTypeKey(jid),
	})
}

// muteChatTypeKey maps a chat JID to the MUTE_CHAT_TYPE key.
func muteChatTypeKey(jid types.JID) string {
	switch jid.Server {
	case types.GroupServer:
		return "GROUP"
	case types.NewsletterServer:
		return "CHANNEL"
	default:
		return "ONE_ON_ONE"
	}
}

// chatActionChatTypeKey maps a chat JID to the CHAT_ACTION_CHAT_TYPE key
// (business chat type is not derivable, so only GROUP/INDIVIDUAL).
func chatActionChatTypeKey(jid types.JID) string {
	if jid.Server == types.GroupServer {
		return "GROUP"
	}
	return "INDIVIDUAL"
}
