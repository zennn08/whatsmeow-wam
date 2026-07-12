package wmtransport

import (
	"fmt"
	"math/rand"
)

// The ambient fabrication table, ported from zapo-js's fabrications.ts. Each entry
// replicates the field subset WA Web's own emit sets, with plausible values. The
// engine picks one per ambient tick, weighted by cadence; gated entries are only
// eligible when their capability flag is on.
//
// Faithful to zapo: these commit directly (no active-hours guard — only the
// base inline ambient specs are hour-gated), and only stop when the engine is
// disposed (the ambient timer is cancelled).

type ambientFab struct {
	event  string
	weight int
	gate   string // "" | channels | communities | business
	emit   func(s *SyntheticUI)
}

func randChoice(opts []string) string { return opts[rand.Intn(len(opts))] }

func cid120363(lo, hi int) string { return fmt.Sprintf("120363%d", randInt(lo, hi)) }

// ambientFabTable returns the full weighted table. Built lazily (closures capture
// no shared state; each takes the engine).
func ambientFabTable() []ambientFab {
	return []ambientFab{
		{"AboutConsumptionDaily", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("AboutConsumptionDaily", map[string]any{
				"aboutChatConsumptionCount": randInt(0, 4), "aboutChatBubbleTapCount": randInt(0, 3), "aboutMessageSendCount": randInt(0, 2),
			})
		}},
		{"ChannelOpen", 1, "channels", func(s *SyntheticUI) {
			s.coord.Commit("ChannelOpen", map[string]any{
				"channelSessionId": randInt(0, 1_000_000_000), "channelEntryPoint": "UPDATES_TAB", "channelUserType": "FOLLOWER",
				"cid": cid120363(100_000_000_000, 1_000_000_000_000), "unreadMessages": randInt(0, 6), "discoverySurface": "CHANNEL_UPDATES_HOME",
			})
		}},
		{"ChatFilterEvent", 3, "", func(s *SyntheticUI) {
			s.coord.Commit("ChatFilterEvent", map[string]any{
				"actionType": "OPEN", "filterType": "NONE", "sessionId": randInt(1, 2_000_000_000), "targetScreen": "CHAT_LIST",
			})
		}},
		{"ChatFolderOpen", 3, "", func(s *SyntheticUI) {
			p := map[string]any{"folderType": "Archive"}
			if rand.Float64() < 0.4 {
				p["activityIndicatorCount"] = randInt(1, 8)
			}
			s.coord.Commit("ChatFolderOpen", p)
		}},
		{"ChatThemeScreen", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("ChatThemeScreen", map[string]any{
				"appearanceType": pick(rand.Float64() < 0.45, "DARK", "LIGHT"), "chatThemeChangeApplied": false,
				"chatThemeId": "", "chatThemeSource": "APP_WIDE", "chatWallpaperType": "DEFAULT",
			})
		}},
		{"ChatThreadWallpaper", 3, "", func(s *SyntheticUI) {
			s.coord.Commit("ChatThreadWallpaper", map[string]any{
				"appearanceType": pick(rand.Float64() < 0.45, "DARK", "LIGHT"), "belongsToCommunity": false,
				"chatThemeId": "doodle@whatsapp-green#tonal", "chatThemeSource": "APP_WIDE",
				"chatType": pick(rand.Float64() < 0.25, "GROUP", "INDIVIDUAL"), "threadId": randB64(32), "wallpaperApplied": false,
			})
		}},
		{"ChatWallpaper", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("ChatWallpaper", map[string]any{
				"appearanceType": pick(rand.Float64() < 0.45, "DARK", "LIGHT"), "chatWallpaperChangeApplied": false,
				"chatWallpaperSource": "APP_WIDE", "chatWallpaperType": "DEFAULT", "chatWallpaperVisit": true,
			})
		}},
		{"CommunityCreation", 1, "communities", func(s *SyntheticUI) {
			s.coord.Commit("CommunityCreation", map[string]any{
				"communityCreationSessionId": randUUID(), "communityCreationActionTaken": "ENTER",
				"communityCreationCurrentScreen": "COMMUNITIES_TAB", "communityCreationEntrypoint": "COMMUNITIES_TAB",
			})
		}},
		{"CommunityFeatureUsage", 1, "communities", func(s *SyntheticUI) {
			s.coord.Commit("CommunityFeatureUsage", map[string]any{
				"communityId": cid120363(100_000_000_000, 1_000_000_000_000), "communityUiAction": "ENTRY", "communityUiFeature": "SUBGROUP_SWITCH",
			})
		}},
		{"CommunityHomeAction", 1, "communities", func(s *SyntheticUI) {
			s.coord.Commit("CommunityHomeAction", map[string]any{
				"communityHomeId": cid120363(100_000_000_000, 1_000_000_000_000), "communityHomeViews": randInt(1, 4),
				"communityHomeGroupNavigations": randInt(0, 3), "communityHomeGroupDiscoveries": randInt(0, 2), "communityHomeGroupJoins": 0,
			})
		}},
		{"CommunityTabAction", 1, "communities", func(s *SyntheticUI) {
			s.coord.Commit("CommunityTabAction", map[string]any{
				"communityTabViews": randInt(1, 4), "communityTabGroupNavigations": randInt(0, 2),
				"communityTabToHomeViews": randInt(0, 2), "communityTabViewsViaContextMenu": 0,
			})
		}},
		{"ContactNotificationSettingUserJourney", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("ContactNotificationSettingUserJourney", map[string]any{
				"appSessionId":                         s.appSessionID,
				"contactNotificationSettingActionType": pick(rand.Float64() < 0.7, "MUTE_MENTION_EVERYONE_ON", "MUTE_MENTION_EVERYONE_OFF"),
				"uiSurface":                            "CONTACT_NOTIFICATION_SETTING_PAGE", "groupSize": randInt(3, 60),
			})
		}},
		{"DialogEvent", 1, "", func(s *SyntheticUI) {
			if rand.Float64() < 0.6 {
				s.coord.Commit("DialogEvent", map[string]any{"dialogEventSource": "dismiss", "dialogEventType": "CLICK", "dialogName": "HARD_REFRESH"})
			} else {
				s.coord.Commit("DialogEvent", map[string]any{"dialogEventSource": "cancel", "dialogEventType": "CLICK", "dialogName": "LOGOUT"})
			}
		}},
		{"DisappearingMessageChatPicker", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("DisappearingMessageChatPicker", map[string]any{
				"dmChatPickerEntryPoint": "DEFAULT_MODE_SETTING", "dmChatPickerEventName": "CHAT_PICKER_TRAY_OPEN",
				"ephemeralityDuration": pickInt(rand.Float64() < 0.8, 7_776_000, 604_800),
			})
		}},
		{"ForwardActionUserJourney", 1, "", func(s *SyntheticUI) {
			isGroup := rand.Float64() < 0.35
			s.coord.Commit("ForwardActionUserJourney", map[string]any{
				"appSessionId": s.appSessionID, "unifiedSessionId": s.unifiedSessionID, "userJourneyFunnelId": randUUID(),
				"forwardUserJourneyFunnelId": randUUID(), "forwardActionUserJourneyAction": "CONTEXT_MENU_SHOWN_WITHOUT_FORWARD",
				"uiSurface": pick(isGroup, "GROUP_CHAT", "CHAT_THREAD"), "userJourneyChatType": pick(isGroup, "GROUP", "INDIVIDUAL"),
				"messageType": pick(isGroup, "GROUP", "INDIVIDUAL"), "messageMediaType": "TEXT", "messageIsFromMe": false,
			})
		}},
		{"GroupJourney", 1, "communities", func(s *SyntheticUI) {
			s.coord.Commit("GroupJourney", map[string]any{
				"actionType": "GROUP_NAVIGATION", "appSessionId": s.appSessionID, "surface": "COMMUNITY_TAB",
				"groupSize": randInt(6, 180), "threadType": "SUB_GROUP", "userRole": "MEMBER",
			})
		}},
		{"GroupMemberUpdates", 3, "", func(s *SyntheticUI) {
			sess := randUUID()
			s.coord.Commit("GroupMemberUpdates", map[string]any{
				"groupMemberUpdatesActionName": "VIEW", "groupMemberUpdatesCurrentScreen": "GROUP_MEMBER_UPDATES_SCREEN", "groupMemberUpdatesSessionId": sess,
			})
			s.coord.Commit("GroupMemberUpdates", map[string]any{
				"groupMemberUpdatesActionName": "FETCH_MEMBER_UPDATES_SUCCESS", "groupMemberUpdatesCurrentScreen": "GROUP_MEMBER_UPDATES_SCREEN",
				"groupMemberUpdatesSessionId": sess, "fetchedMessageCount": randInt(1, 8), "fetchedMessageLatency": randInt(40, 400),
			})
		}},
		{"HfmTextSearchComplete", 1, "", func(s *SyntheticUI) { s.coord.Commit("HfmTextSearchComplete", nil) }},
		{"KeepInChatErrors", 1, "", func(s *SyntheticUI) {
			isAGroup := rand.Float64() < 0.35
			isAdmin := isAGroup && rand.Float64() < 0.5
			canEdit := true
			if isAGroup {
				canEdit = isAdmin
			}
			s.coord.Commit("KeepInChatErrors", map[string]any{
				"kicAction": "KEEP_MESSAGE", "isAGroup": isAGroup, "isAdmin": isAdmin, "canEditDmSettings": canEdit,
				"kicMessageEphemeralityDuration": 604_800, "kicErrorCode": "OFFLINE",
			})
		}},
		{"KeepInChatNux", 1, "", func(s *SyntheticUI) {
			durations := []int{86_400, 604_800, 7_776_000}
			s.coord.Commit("KeepInChatNux", map[string]any{
				"kicNuxActionName": "KIC_NUX_IMPRESSION", "trigger": "CHAT_ENTRY", "chatEphemeralityDuration": durations[rand.Intn(len(durations))],
			})
		}},
		{"LimitSharingSettingUpdate", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("LimitSharingSettingUpdate", map[string]any{"toggleUpdateAction": pick(rand.Float64() < 0.7, "TURN_ON", "TURN_OFF")})
		}},
		{"ListUpdateUserJourney", 3, "", func(s *SyntheticUI) {
			s.coord.Commit("ListUpdateUserJourney", map[string]any{"listAction": "CREATE", "listUpdateUserJourneyAction": "START", "updateEntryPoint": "ADD_LIST_FILTER"})
		}},
		{"LockFolderUnlock", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("LockFolderUnlock", map[string]any{"landingSurface": "FOLDER", "totalChatCount": randInt(1, 4), "unlockEntryPoint": "CHAT_LIST"})
		}},
		{"MdChatAssignmentSecondaryAction", 1, "business", func(s *SyntheticUI) {
			s.coord.Commit("MdChatAssignmentSecondaryAction", map[string]any{
				"mdChatAssignmentSecondaryActionAgentId": "", "mdChatAssignmentSecondaryActionBrowserId": randHex(20),
				"mdChatAssignmentSecondaryActionChatType": "INDIVIDUAL", "mdChatAssignmentSecondaryActionMdId": randInt(0, 30),
				"mdChatAssignmentSecondaryActionSource": "NONE", "mdChatAssignmentSecondaryActionType": "ACTION_TOOLTIP_SHOWN",
			})
		}},
		{"MediaHubUserJourney", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("MediaHubUserJourney", map[string]any{
				"mediaHubEntryPoint": "MAIN_SCREEN", "mediaHubAction": "OPEN_MEDIA_HUB", "unifiedSessionId": s.unifiedSessionID,
				"mediaHubSurface": "MEDIA", "mediaHubSequenceNumber": 1, "mediaHubSessionId": randUUID(), "customFields": `{"search_results":false}`,
			})
		}},
		{"MessagingUserJourney", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("MessagingUserJourney", map[string]any{
				"appSessionId": s.appSessionID, "userJourneyFunnelId": randHex(16), "threadType": "INDIVIDUAL",
				"uiSurface": "MESSAGE_MENU", "messagingActionType": "CLICK_PIN", "mediaType": "TEXT",
			})
		}},
		{"PinInChatInteraction", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("PinInChatInteraction", map[string]any{
				"pinInChatInteractionType": "TAP_ON_BANNER", "isAGroup": false, "mediaType": "TEXT",
				"pinCount": 1, "pinIndex": 0, "isSelfPin": rand.Float64() < 0.5,
			})
		}},
		{"PrivacyHighlightDaily", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("PrivacyHighlightDaily", map[string]any{
				"privacyHighlightCategory": "E2EE", "privacyHighlightSurface": "GOLDEN_BOX_CONTACT",
				"narrativeAppearCount": randInt(1, 8), "dialogAppearCount": 0, "dialogSelectCount": 0,
			})
		}},
		{"PrivacySettingsClick", 1, "", func(s *SyntheticUI) {
			items := []string{"LAST_SEEN_AND_ONLINE", "PROFILE_PHOTO", "ABOUT", "GROUPS", "READ_RECEIPT", "BLOCKED"}
			s.coord.Commit("PrivacySettingsClick", map[string]any{"privacyControlEntryPoint": "PRIVACY_SETTINGS", "privacyControlItem": randChoice(items)})
		}},
		{"PrivacyTipAction", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("PrivacyTipAction", map[string]any{"privacyTipActionType": pick(rand.Float64() < 0.8, "VIEW", "CLICK_OK")})
		}},
		{"QuotedMessageUserJourney", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("QuotedMessageUserJourney", map[string]any{
				"appSessionId": s.appSessionID, "unifiedSessionId": s.unifiedSessionID, "userJourneyFunnelId": randUUID(),
				"uiSurface": "CHAT_THREAD", "userJourneyChatType": "INDIVIDUAL", "quotedMediaType": "TEXT",
				"quotedMessageTypeEnum": "INDIVIDUAL", "quotedMessageUserJourneyAction": "QUOTED_MESSAGE_ADDED",
				"quotedMessageUserJourneyEntryPoint": "CONTEXT_MENU_REPLY_BUTTON",
			})
		}},
		{"ReactionUserJourney", 3, "", func(s *SyntheticUI) {
			s.coord.Commit("ReactionUserJourney", map[string]any{
				"appSessionId": s.appSessionID, "unifiedSessionId": s.unifiedSessionID, "userJourneyFunnelId": randUUID(),
				"userJourneyEventMs": nowMilli(), "reactionUserJourneyAction": "TRAY_OPEN", "reactionUserJourneyEntryPoint": "MACOS_MESSAGE_REACTION_BUTTON",
				"uiSurface": "CHAT_THREAD", "userJourneyChatType": "INDIVIDUAL", "messageType": "INDIVIDUAL", "messageMediaType": "TEXT",
				"messageHasReaction": false, "messageHasOwnReaction": false,
			})
		}},
		{"ReportToAdminEvents", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("ReportToAdminEvents", map[string]any{
				"reportToAdminInteraction": "CLICK_SEND_FOR_ADMIN_REVIEW", "rtaGroupId": fmt.Sprintf("120363%d@g.us", randInt(100_000_000_000, 999_999_999_999)),
			})
		}},
		{"RingtoneScreen", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("RingtoneScreen", map[string]any{
				"ringtoneChangeApplied": false, "ringtoneId": "__default__", "ringtoneReset": false,
				"ringtoneSelectionCancelled": true, "ringtoneSource": "APP_WIDE", "premiumRingtonesDownloadedCount": randInt(0, 5),
			})
		}},
		{"ScreenLockSettingsData", 1, "", func(s *SyntheticUI) { s.coord.Commit("ScreenLockSettingsData", nil) }},
		{"SearchActionEvent", 3, "", func(s *SyntheticUI) {
			s.coord.Commit("SearchActionEvent", map[string]any{
				"searchAction": "TYPEAHEAD_SHOW", "searchActionEntryPoint": "CHATS_LIST", "searchAiSuggestionCount": randInt(0, 2),
				"searchChatsCount": randInt(1, 6), "searchContactsCount": randInt(0, 4), "searchGroupsCount": randInt(0, 3), "searchMessagesCount": randInt(0, 5),
			})
		}},
		{"SearchTheWebFunnel", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("SearchTheWebFunnel", map[string]any{
				"stwInteraction": "ENTRY_POINT_SURFACED", "stwEntryPoint": "HIGHLY_FORWARDED_MESSAGE", "stwFormat": "SINGLE_TEXT", "messageType": "GROUP",
			})
		}},
		{"SettingsChange", 1, "", func(s *SyntheticUI) {
			toggles := []string{"IS_ENTER_TO_SEND_ENABLED", "IS_SPELL_CHECK_ENABLED", "DISABLE_LINK_PREVIEWS", "REPLACE_TEXT_WITH_EMOJI"}
			s.coord.Commit("SettingsChange", map[string]any{"settingType": randChoice(toggles), "currentSettingValue": pick(rand.Float64() < 0.5, "true", "false")})
		}},
		{"SettingsClick", 3, "", func(s *SyntheticUI) {
			items := []string{"CHATS", "NOTIFICATIONS", "PRIVACY", "ACCOUNT", "STARRED_MESSAGES"}
			s.coord.Commit("SettingsClick", map[string]any{"settingsItem": randChoice(items), "settingsClickEntryPoint": "SETTINGS_SCREEN"})
		}},
		{"SettingsSearchInitiate", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("SettingsSearchInitiate", map[string]any{"settingsPageType": "SETTINGS"})
		}},
		{"SettingsSearchTap", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("SettingsSearchTap", map[string]any{"tapItemName": "chat-wallpaper", "topLevelParentSetting": "CHATS"})
		}},
		{"ShareContentUserJourney", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("ShareContentUserJourney", map[string]any{
				"appSessionId": s.appSessionID, "unifiedSessionId": s.unifiedSessionID, "userJourneyFunnelId": randUUID(),
				"userJourneyEventMs": nowMilli(), "shareContentUserJourneyAction": "FUNNEL_START", "shareContentUserJourneyEntryPoint": "CONTEXT_MENU",
				"uiSurface": "CHAT_THREAD", "mediaCount": 0, "hasFiles": false,
			})
		}},
		{"SnackbarDeleteUndo", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("SnackbarDeleteUndo", map[string]any{
				"snackbarActionType": "SNACKBAR_SHOWN", "isAGroup": false, "messagesUndeleted": 1, "threadId": randB64(32), "mediaType": "TEXT",
			})
		}},
		{"StatusItemView", 3, "", func(s *SyntheticUI) {
			viewTime := randInt(2500, 8000)
			s.coord.Commit("StatusItemView", map[string]any{
				"statusItemViewResult": "OK", "statusItemViewTime": viewTime, "statusItemLoadTime": randInt(40, 600),
				"statusItem3sViewCount": boolToInt01(viewTime >= 3000), "statusItemViewCount": 1, "statusItemImpressionCount": 1,
				"statusItemReplied": 0, "statusCategory": "REGULAR_STATUS", "statusViewerSessionId": randInt(1, 1_000_000_000),
				"statusRowSection": "RECENT_STORIES", "statusRowIndex": randInt(0, 5), "mediaType": "PHOTO", "statusItemUnread": true,
			})
		}},
		{"StatusPosterActions", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("StatusPosterActions", map[string]any{
				"statusEventType": "STATUS_ENTRYPOINT_TAP", "statusCreationEntryPoint": pick(rand.Float64() < 0.5, "STATUS_TAB_CAMERA", "STATUS_TAB_PEN"),
				"statusPostingSessionId": randInt(1, 2_000_000_000),
			})
		}},
		{"StatusPostImpression", 1, "", func(s *SyntheticUI) {
			viewTime := randInt(2500, 8000)
			s.coord.Commit("StatusPostImpression", map[string]any{
				"statusId": randHex(20), "statusContentType": "PHOTO", "statusMediaType": "PHOTO", "isSelfView": false, "isSubImpression": false,
				"statusViewEntrypoint": "CHAT_LIST", "statusViewTime": viewTime, "unifiedSessionId": s.unifiedSessionID,
				"updatesTabSessionId": randInt(1, 1_000_000_000), "statusViewerSessionId": randInt(1, 1_000_000_000), "statusPogIndex": 0, "statusPostIndex": 0,
				"isFirstView": true, "isCloseSharingPost": false, "isPosterBiz": false, "isViewedInLandscape": false, "psaLinkAvailable": false,
				"statusCategory": "REGULAR_STATUS", "statusPostPlaybackDuration": viewTime, "statusContainsMusic": false, "musicBlocked": false,
				"statusContainsQuestion": false, "isSuccessfulView": true, "statusItemViewResult": "OK", "entryMethod": "DIRECT_POG_TAP",
				"viewSequenceIndex": 0, "isResharable": false, "isReshare": false,
			})
		}},
		{"StatusReportingEvents", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("StatusReportingEvents", map[string]any{"statusReportInteraction": "CLICK_REPORT"})
		}},
		{"StatusRowView", 3, "", func(s *SyntheticUI) {
			s.coord.Commit("StatusRowView", map[string]any{
				"statusRowEntryMethod": "DIRECT_ROW_TAP", "statusRowIndex": randInt(0, 5), "statusRowSection": "RECENT_STORIES",
				"statusRowUnreadItemCount": randInt(1, 4), "statusRowViewCount": 1, "statusSessionId": randInt(1, 1_000_000_000), "statusViewerSessionId": randInt(1, 1_000_000_000),
			})
		}},
		{"StatusViewerAction", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("StatusViewerAction", map[string]any{"viewerActionType": "ATTRIBUTION_TAPPED", "attributionType": "MUSIC", "statusCategory": "REGULAR_STATUS"})
		}},
		{"StickerAddToFavorite", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("StickerAddToFavorite", map[string]any{
				"stickerIsAnimated": rand.Float64() < 0.5, "stickerIsFirstParty": false, "stickerIsFromStickerMaker": false, "stickerIsPremium": false,
			})
		}},
		{"StickerStoreOpened", 1, "", func(s *SyntheticUI) { s.coord.Commit("StickerStoreOpened", nil) }},
		{"SystemMessageClick", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("SystemMessageClick", map[string]any{
				"isAGroup": false, "isANewThread": true, "systemMessageCategory": "PRIVACY", "systemMessageType": "E2E_ENCRYPTED_MESSAGES",
			})
		}},
		{"UiMessageYourselfAction", 1, "", func(s *SyntheticUI) {
			useSearch := rand.Float64() < 0.4
			funnel := pick(useSearch, "CONTACT_AND_GLOBAL_SEARCH", "NEW_CHAT")
			action := "EXISTING_NTS_OPENED"
			if rand.Float64() < 0.5 {
				action = pick(useSearch, "SEARCH_BAR_PRESSED", "NEW_CHAT_PRESSED")
			}
			s.coord.Commit("UiMessageYourselfAction", map[string]any{
				"uiMessageYourselfActionSessionId": randHex(16), "uiMessageYourselfActionType": action, "uiMessageYourselfFunnelName": funnel,
			})
		}},
		{"UiRevokeAction", 1, "", func(s *SyntheticUI) {
			trash := rand.Float64() < 0.45
			dur := 0
			if trash {
				dur = randInt(600, 5000)
			}
			s.coord.Commit("UiRevokeAction", map[string]any{
				"messageAction": pick(trash, "TRASH_CAN_SELECTED", "MESSAGE_SELECTED"), "uiRevokeActionDuration": dur, "uiRevokeActionSessionId": randHex(16),
			})
		}},
		{"UpdatesTabSearch", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("UpdatesTabSearch", map[string]any{"updateTabSearchEventType": "SEARCH_TAP", "channelsFollowedCount": randInt(0, 5), "channelsAdminCount": 0})
		}},
		{"UsernameExposed", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("UsernameExposed", map[string]any{"usernameExposureContext": "contact_info_subtitle"})
		}},
		{"ViewBusinessProfile", 1, "", func(s *SyntheticUI) {
			entryPoints := []string{"CHAT_HEADER", "CONTACT_CARD", "CHATS_HOME"}
			s.coord.Commit("ViewBusinessProfile", map[string]any{
				"viewBusinessProfileAction": "ACTION_IMPRESSION", "catalogSessionId": randHex(16), "profileEntryPoint": randChoice(entryPoints),
				"isProfileLinked": false, "hasCoverPhoto": rand.Float64() < 0.5,
			})
		}},
		{"ViewOnceScreenshotActions", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("ViewOnceScreenshotActions", map[string]any{
				"voSsAction": "PLACEHOLDER_MESSAGE_LEARN_MORE_TAP", "voMessageType": pick(rand.Float64() < 0.75, "PHOTO", "VIDEO"), "isAGroup": false, "threadId": randB64(32),
			})
		}},
		{"WaShopsManagement", 1, "business", func(s *SyntheticUI) {
			s.coord.Commit("WaShopsManagement", map[string]any{"shopsManagementAction": "ACTION_CLICK_SHOPS_SETTING", "isShopsProductPreviewVisible": false})
		}},
		{"WebcButterbarEvent", 3, "", func(s *SyntheticUI) {
			s.coord.Commit("WebcButterbarEvent", map[string]any{
				"webcButterbarType": pick(rand.Float64() < 0.6, "OFFLINE", "RESUME_CONNECTING"), "webcButterbarAction": pick(rand.Float64() < 0.7, "IMPRESSION", "AUTO_DISMISS"),
			})
		}},
		{"WebcLinkPreviewDisplay", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("WebcLinkPreviewDisplay", map[string]any{
				"webcDisplayStatus": "SHOWED_PREVIEW_TO_USER", "didRequestHq": true, "didRespondHqPreview": true, "didFallbackNonHq": false,
			})
		}},
		{"WebcMenu", 1, "", func(s *SyntheticUI) {
			labels := []string{"NEW_GROUP", "STARRED", "ARCHIVED", "NEW_GROUP", "STARRED"}
			s.coord.Commit("WebcMenu", map[string]any{"webcMenuAction": "THREADS_SCREEN_CLICK", "webcMenuItemLabel": randChoice(labels)})
		}},
		{"WebcNativeUpsellCta", 1, "", func(s *SyntheticUI) {
			sources := []string{"CHATLIST_DROPDOWN", "SETTINGS", "BUTTERBAR"}
			s.coord.Commit("WebcNativeUpsellCta", map[string]any{
				"webcNativeUpsellCtaEventType": "IMPRESSION", "webcNativeUpsellCtaSource": randChoice(sources),
				"webcNativeUpsellCtaQrScreenExperimentGroup": "CONTROL", "webcNativeUpsellCtaReleaseChannel": "PRODUCTION", "webcNativeUpsellCtaIsBetaUser": false,
			})
		}},
		{"WebcNavbar", 3, "", func(s *SyntheticUI) {
			r := rand.Float64()
			label := "COMMUNITIES"
			switch {
			case r < 0.7:
				label = "CHATS"
			case r < 0.85:
				label = "STATUS"
			case r < 0.95:
				label = "SETTINGS"
			}
			s.coord.Commit("WebcNavbar", map[string]any{"webcNavbarItemLabel": label})
		}},
		{"WebContactListStartNewChat", 3, "", func(s *SyntheticUI) {
			isGroup := rand.Float64() < 0.15
			search := isGroup || rand.Float64() < 0.55
			s.coord.Commit("WebContactListStartNewChat", map[string]any{
				"webContactListStartNewChatType": pick(isGroup, "GROUP", "CONTACT"), "webContactListStartNewChatSearch": search,
			})
		}},
		{"WebcStickerMakerEvents", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("WebcStickerMakerEvents", map[string]any{"stickerMakerEventName": "STICKER_MAKER_BUTTON_TAP"})
		}},
		{"WebcWhatsNewImpression", 1, "", func(s *SyntheticUI) {
			s.coord.Commit("WebcWhatsNewImpression", map[string]any{"webcWhatsNewSurface": "BANNER", "webcWhatsNewAction": "IMPRESSION", "webcWhatsNewVariant": randInt(1, 6)})
		}},
	}
}

func pickInt(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}

func boolToInt01(b bool) int {
	if b {
		return 1
	}
	return 0
}
