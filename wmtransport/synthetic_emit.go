package wmtransport

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// --- anchored fabrications ---

func (s *SyntheticUI) emitChatOpen(isLid bool) {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("UiAction", map[string]any{
		"uiActionType": "CHAT_OPEN", "uiActionPreloaded": true, "isLid": isLid, "uiActionT": randInt(40, 400),
	})
	rendered := randInt(8, 30)
	beforePaint := randInt(20, 80)
	painted := beforePaint + randInt(20, 120)
	s.coord.Commit("WebcChatOpen", map[string]any{
		"webcUnreadCount":               randInt(0, 4),
		"webcWindowHeightFloat":         s.windowHeightFloat,
		"webcChatOpenBeforePaintT":      beforePaint,
		"webcChatOpenPaintedT":          painted,
		"webcChatOpenT":                 painted + randInt(10, 200),
		"webcRenderedMessageCount":      rendered,
		"webcFinalRenderedMessageCount": rendered,
	})
}

func (s *SyntheticUI) emitImageOpen(isLid bool) {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("UiAction", map[string]any{
		"uiActionType": "IMAGE_OPEN", "uiActionPreloaded": true, "isLid": isLid, "uiActionT": randInt(60, 600),
	})
}

func (s *SyntheticUI) emitUiAction(payload map[string]any) {
	if s.canEmit() {
		s.coord.Commit("UiAction", payload)
	}
}

func (s *SyntheticUI) emitAboutConsumption() {
	if !s.canEmit() {
		return
	}
	surface := "PROFILE_INFO"
	if rand.Float64() < 0.5 {
		surface = "ONE_ON_ONE_CHAT"
	}
	s.coord.Commit("AboutConsumption", map[string]any{"aboutConsumptionSurface": surface})
	if rand.Float64() < 0.35 {
		s.coord.Commit("AboutInteraction", map[string]any{"aboutConsumptionSurface": surface})
	}
}

func (s *SyntheticUI) emitMediaLoad() {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("WebcMediaLoad", map[string]any{"webcMediaLoadResult": "SUCCESS", "webcMediaLoadT": randInt(30, 800)})
}

func (s *SyntheticUI) emitAttachmentTray(media string, isGroup bool) {
	if !s.canEmit() {
		return
	}
	target := attachmentTargetFor(media)
	if target == "" {
		return
	}
	p := map[string]any{
		"attachmentTrayAction":       "SEND",
		"attachmentTrayActionTarget": target,
		"actionThreadType":           pick(isGroup, "GROUP_CHAT", "P2P_THREAD"),
		"isAGroup":                   isGroup,
		"isSuccessful":               true,
		"actionDurationMs":           randInt(1500, 20_000),
		"sendTime":                   randInt(200, 4000),
	}
	if media == "PHOTO" || media == "VIDEO" {
		p["sendMediaType"] = media
	}
	s.coord.Commit("AttachmentTrayActions", p)
}

func (s *SyntheticUI) emitMediaPicker(media string) {
	if !s.canEmit() {
		return
	}
	isDoc := media == "DOCUMENT"
	mt := "PHOTO"
	if isDoc {
		mt = "DOCUMENT"
	} else if media == "VIDEO" {
		mt = "VIDEO"
	}
	origin := "CHAT_PHOTO_LIBRARY"
	if isDoc {
		origin = "DOCUMENT_PICKER"
	}
	s.coord.Commit("MediaPicker", map[string]any{
		"mediaPickerSent": 1, "mediaPickerSentUnchanged": 1, "mediaPickerT": randInt(1500, 15_000),
		"mediaType": mt, "mediaPickerOrigin": origin, "mediaPickerChanged": 0, "mediaPickerCroppedRotated": 0,
		"mediaPickerDrawing": 0, "mediaPickerStickers": 0, "mediaPickerText": 0, "mediaPickerLikeDoc": 0,
		"mediaPickerNotLikeDoc": 0, "mediaPickerDeleted": 0, "chatRecipients": 1, "isViewOnce": false,
	})
}

func (s *SyntheticUI) emitMediaStreamPlayback() {
	if !s.canEmit() {
		return
	}
	state := "ENDED"
	if rand.Float64() < 0.5 {
		state = "READY_PAUSE"
	}
	s.coord.Commit("MediaStreamPlayback", map[string]any{
		"playbackOrigin": "CONVERSATION", "mediaType": "VIDEO", "didPlay": true,
		"playbackState": state, "videoDuration": randInt(5, 180), "initialBufferingT": randInt(50, 900),
	})
}

func (s *SyntheticUI) emitMediaEditorSend() {
	if !s.canEmit() {
		return
	}
	imageCount := 1
	if rand.Float64() >= 0.85 {
		imageCount = 2
	}
	editedImageCount := 0
	if rand.Float64() < 0.25 {
		editedImageCount = 1
	}
	textLayerCount := 0
	if editedImageCount != 0 && rand.Float64() < 0.6 {
		textLayerCount = 1
	}
	emojiLayerCount := 0
	if editedImageCount != 0 && textLayerCount == 0 {
		emojiLayerCount = 1
	}
	s.coord.Commit("WebcMediaEditorSend", map[string]any{
		"imageCount": imageCount, "editedImageCount": editedImageCount, "paintedImageCount": 0,
		"blurImageCount": 0, "emojiLayerCount": emojiLayerCount, "stickerLayerCount": 0, "textLayerCount": textLayerCount,
	})
}

func (s *SyntheticUI) emitHdMediaAwareness() {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("WebHdMediaAwarenessInteraction", map[string]any{"hdMediaSelected": rand.Float64() < 0.5})
}

func (s *SyntheticUI) emitMentionPickerAction() {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("MentionPickerAction", map[string]any{"isAGroup": true, "mentionType": "REGULAR_USER", "threadId": randB64(32)})
}

func (s *SyntheticUI) emitGroupCatchUp() {
	if !s.canEmit() {
		return
	}
	pct := 0
	if rand.Float64() >= 0.8 {
		pct = randInt(1, 6) * 10
	}
	s.coord.Commit("GroupCatchUp", map[string]any{"mentionsCountPendingPercentage": pct})
}

func (s *SyntheticUI) emitInlineVideoClosed(isGroup bool) {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("InlineVideoPlaybackClosed", map[string]any{
		"inlineVideoType": "YOUTUBE", "inlineVideoPlayed": true,
		"messageType": pick(isGroup, "GROUP", "INDIVIDUAL"), "inlineVideoHasRcat": false,
		"inlineVideoPlayStartT": randInt(300, 3000), "inlineVideoDurationT": randInt(45, 600),
	})
}

func (s *SyntheticUI) emitTextMessageUserJourney(isGroup bool) {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("TextMessageUserJourney", map[string]any{
		"appSessionId": s.appSessionID, "unifiedSessionId": s.unifiedSessionID,
		"userJourneyFunnelId": randUUID(), "uiSurface": pick(isGroup, "GROUP_CHAT", "CHAT_THREAD"),
		"textMessageUserJourneyAction": "SENT", "userJourneyChatType": pick(isGroup, "GROUP", "INDIVIDUAL"),
		"userJourneyEventMs": nowMilli(), "chatbarInitialState": "EMPTY",
	})
}

func (s *SyntheticUI) emitStructuredMessageBuyerInteraction() {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("StructuredMessageBuyerInteraction", map[string]any{
		"bizPlatform": "SMB", "messageClass": "BUTTON_NFM", "messageClassAttributes": "{}",
		"messageInteraction": "USER_VIEW", "messageMediaType": "NONE",
	})
}

// --- ambient (idle) fabrications ---

func (s *SyntheticUI) emitChatOpenAmbient() {
	s.mu.Lock()
	n := len(s.recentChatIsLid)
	if n == 0 {
		s.mu.Unlock()
		return
	}
	isLid := s.recentChatIsLid[rand.Intn(n)]
	s.mu.Unlock()
	s.emitChatOpen(isLid)
}

func (s *SyntheticUI) emitEmojiOpen() {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("WebcEmojiOpen", map[string]any{"webcEmojiOpenTab": emojiTabs[rand.Intn(len(emojiTabs))]})
}

func (s *SyntheticUI) emitStickerPickerOpened() {
	if s.canEmit() {
		s.coord.Commit("StickerPickerOpened", nil)
	}
}

func (s *SyntheticUI) emitContactSearch() {
	if !s.canEmit() {
		return
	}
	action := "CLICK_ON_CONTACT"
	if rand.Float64() < 0.6 {
		action = "SEARCH_START"
	}
	s.coord.Commit("ContactSearchExperience", map[string]any{
		"contactSearchEntrypoint": "CHATS_LIST_GLOBAL_SEARCH", "searchActionName": action,
		"isUsernameSearch": false, "searchStartsWithAt": false,
	})
}

func (s *SyntheticUI) emitTsNavigation() {
	if !s.canEmit() {
		return
	}
	now := nowMilli()
	s.coord.Commit("TsNavigation", map[string]any{
		"tsSessionId": s.tsSessionID, "relativeTimestampMs": max64(0, now-s.sessionStartMs),
		"navigationSource": "CHAT_LIST", "navigationDestination": "CHAT_THREAD",
		"navigationDestinationViewName": "", "isCanonicalEntPresent": true,
		"tsTimestampMs": now, "unifiedSessionId": s.unifiedSessionID,
	})
}

func (s *SyntheticUI) emitDisappearingModeSetting() {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("DisappearingModeSettingEvents", map[string]any{
		"disappearingModeSettingEventName": "DEFAULT_MESSAGE_TIMER_OPEN",
		"disappearingModeEntryPoint":       "ACCOUNT_SETTINGS", "isAfterRead": false,
	})
	s.schedule(jitter(2000, 30_000), func() {
		if !s.canEmit() {
			return
		}
		s.coord.Commit("DisappearingModeSettingEvents", map[string]any{
			"disappearingModeSettingEventName": "DEFAULT_MESSAGE_TIMER_EXIT",
			"disappearingModeEntryPoint":       "ACCOUNT_SETTINGS", "isAfterRead": false,
		})
	})
}

func (s *SyntheticUI) emitGifSearchSession() {
	if !s.canEmit() {
		return
	}
	s.coord.Commit("GifSearchSessionStarted", map[string]any{"gifSearchProvider": s.gifProvider})
	if rand.Float64() < 0.2 {
		s.schedule(jitter(1500, 6000), func() {
			if s.canEmit() {
				s.coord.Commit("GifSearchNoResults", map[string]any{"gifSearchProvider": s.gifProvider})
			}
		})
	}
	s.schedule(jitter(3000, 20_000), func() {
		if s.canEmit() {
			s.coord.Commit("GifSearchCancelled", map[string]any{"gifSearchProvider": s.gifProvider})
		}
	})
}

func (s *SyntheticUI) emitMessageContextMenu() {
	if !s.canEmit() {
		return
	}
	isAGroup := rand.Float64() < 0.4
	isOriginalSender := rand.Float64() < 0.35
	s.coord.Commit("MessageContextMenuActions", map[string]any{
		"isAGroup": isAGroup, "isMultiAction": false, "isOriginalSender": isOriginalSender, "messageContextMenuAction": "OPEN",
	})
	if rand.Float64() < 0.5 {
		options := []string{"REACT", "REPLY", "COPY", "FORWARD", "STAR_OR_UNSTAR"}
		opt := options[rand.Intn(len(options))]
		s.schedule(jitter(400, 3000), func() {
			if !s.canEmit() {
				return
			}
			s.coord.Commit("MessageContextMenuActions", map[string]any{
				"isAGroup": isAGroup, "isMultiAction": false, "isOriginalSender": isOriginalSender,
				"messageContextMenuAction": "CLICK", "messageContextMenuOption": opt,
			})
		})
	}
}

// --- periodic stats ---

func (s *SyntheticUI) emitMemoryStat() {
	if !s.canEmit() {
		return
	}
	s.mu.Lock()
	s.memCurrentKb = clampInt(s.memCurrentKb+randInt(-4000, 6000), 40_000, 180_000)
	if s.memCurrentKb > s.memPeakKb {
		s.memPeakKb = s.memCurrentKb
	}
	cur, peak, seen, start := s.memCurrentKb, s.memPeakKb, s.messagesSeen, s.sessionStartMs
	s.mu.Unlock()
	s.coord.Commit("MemoryStat", map[string]any{
		"workingSetSize": cur, "workingSetPeakSize": peak,
		"uptime": int((nowMilli() - start) / 1000), "numMessages": seen, "processType": "main",
	})
}

func (s *SyntheticUI) recordActivitySlice() {
	s.mu.Lock()
	if s.sliceActive {
		i := s.activitySlice
		if i < 32 {
			s.bitmapLow |= 1 << uint(i)
		} else {
			s.bitmapHigh |= 1 << uint(i-32)
		}
		s.activeSliceCount++
	}
	s.activitySlice++
	s.sliceActive = false
	slice := s.activitySlice
	s.mu.Unlock()

	if slice >= activityMaxSlices {
		s.emitUserActivity()
		s.resetActivityWindow()
	} else if slice%activityFlushSlices == 0 {
		s.emitUserActivity()
	}
}

func (s *SyntheticUI) emitUserActivity() {
	if !s.canEmit() {
		return
	}
	s.mu.Lock()
	if s.activitySlice == 0 {
		s.mu.Unlock()
		return
	}
	s.activitySeq++
	length := min(s.activitySlice, activityMaxSlices)
	low, high := s.bitmapLow, s.bitmapHigh
	seq, cum := s.activitySeq, s.activeSliceCount
	sessID, tsID := s.activitySessionID, s.tsSessionID
	startMs, uniID, sessionStart := s.activityStartMs, s.unifiedSessionID, s.sessionStartMs
	s.mu.Unlock()

	ua := map[string]any{
		"userActivitySessionId":  sessID,
		"userActivityStartTime":  startMs / 1000,
		"userActivityBitmapLen":  length,
		"userActivityBitmapLow":  int64(low),
		"userActivitySessionSeq": seq,
		"userActivitySessionCum": cum,
	}
	if length > 32 {
		ua["userActivityBitmapHigh"] = int64(high)
	}
	s.coord.Commit("UserActivity", ua)

	now := nowMilli()
	ts := map[string]any{
		"tsSessionId": tsID, "bitarrayLength": length, "bitarrayLow": int64(low),
		"cumulativeBits": cum, "sessionSeq": seq, "relativeTimestampMs": max64(0, now-sessionStart),
		"tsTimestampMs": now, "unifiedSessionId": uniID,
	}
	if length > 32 {
		ts["bitarrayHigh"] = int64(high)
	}
	s.coord.Commit("TsBitArray", ts)
}

func (s *SyntheticUI) resetActivityWindow() {
	s.mu.Lock()
	s.bitmapLow, s.bitmapHigh = 0, 0
	s.activitySlice, s.activeSliceCount, s.activitySeq = 0, 0, 0
	s.activitySessionID = randBase36(6)
	s.tsSessionID = randInt(1, 2_000_000_000)
	s.activityStartMs = nowMilli()
	s.mu.Unlock()
}

// --- helpers ---

func attachmentTargetFor(media string) string {
	switch media {
	case "PHOTO", "VIDEO":
		return "GALLERY"
	case "DOCUMENT":
		return "DOCUMENT"
	case "AUDIO", "PTT":
		return "AUDIO"
	case "CONTACT":
		return "CONTACT"
	case "LOCATION":
		return "LOCATION"
	default:
		return ""
	}
}

func probOr(v, fallback float64) float64 {
	if v > 0 && v <= 1 {
		return v
	}
	return fallback
}

func intervalOr(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// randInt returns a value in [min, max) (matching zapo's floor(rand)).
func randInt(min, max int) int {
	if max <= min {
		return min
	}
	return min + rand.Intn(max-min)
}

func randf(min, max float64) float64 { return min + rand.Float64()*(max-min) }

func jitter(minMs, maxMs float64) time.Duration {
	return time.Duration(randf(minMs, maxMs)) * time.Millisecond
}

func randHex(n int) string {
	b := make([]byte, (n+1)/2)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	return hex.EncodeToString(b)[:n]
}

func randBase36(n int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(36)]
	}
	return string(b)
}

func randUUID() string {
	variant := 8 + rand.Intn(4)
	return fmt.Sprintf("%s-%s-4%s-%x%s-%s", randHex(8), randHex(4), randHex(3), variant, randHex(3), randHex(12))
}

func randB64(nBytes int) string {
	b := make([]byte, nBytes)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	return strings.ReplaceAll(base64.StdEncoding.EncodeToString(b), "/", "-")
}
