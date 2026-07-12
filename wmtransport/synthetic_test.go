package wmtransport

import (
	"testing"
)

func newSynth(opts SyntheticOptions) (*SyntheticUI, *recorder) {
	r := &recorder{}
	return NewSyntheticUI(r, opts), r
}

func TestSynthChatOpen(t *testing.T) {
	s, r := newSynth(SyntheticOptions{})
	s.emitChatOpen(true)
	ua := r.find("UiAction")
	if ua == nil || ua["uiActionType"] != "CHAT_OPEN" || ua["isLid"] != true || ua["uiActionPreloaded"] != true {
		t.Fatalf("UiAction = %v", ua)
	}
	co := r.find("WebcChatOpen")
	if co == nil || co["webcRenderedMessageCount"] != co["webcFinalRenderedMessageCount"] {
		t.Fatalf("WebcChatOpen = %v", co)
	}
}

func TestSynthAttachmentTrayTargets(t *testing.T) {
	cases := []struct {
		media, target string
		hasSendMedia  bool
	}{
		{"PHOTO", "GALLERY", true},
		{"VIDEO", "GALLERY", true},
		{"DOCUMENT", "DOCUMENT", false},
		{"AUDIO", "AUDIO", false},
		{"CONTACT", "CONTACT", false},
		{"LOCATION", "LOCATION", false},
	}
	for _, c := range cases {
		s, r := newSynth(SyntheticOptions{})
		s.emitAttachmentTray(c.media, false)
		at := r.find("AttachmentTrayActions")
		if at == nil || at["attachmentTrayActionTarget"] != c.target || at["actionThreadType"] != "P2P_THREAD" {
			t.Fatalf("media %s: AttachmentTrayActions = %v", c.media, at)
		}
		_, has := at["sendMediaType"]
		if has != c.hasSendMedia {
			t.Errorf("media %s: sendMediaType present=%v want %v", c.media, has, c.hasSendMedia)
		}
	}
	// Sticker/gif have no tray target → no commit.
	s, r := newSynth(SyntheticOptions{})
	s.emitAttachmentTray("STICKER", false)
	if r.has("AttachmentTrayActions") {
		t.Error("sticker should not emit an attachment-tray action")
	}
}

func TestSynthActiveHoursGate(t *testing.T) {
	zero := 0
	s, r := newSynth(SyntheticOptions{ActiveHoursStartHour: &zero, ActiveHoursEndHour: &zero}) // empty window
	s.emitChatOpen(false)
	s.emitEmojiOpen()
	s.emitMemoryStat()
	if len(r.commits) != 0 {
		t.Fatalf("nothing should fabricate outside active hours, got %v", r.names())
	}
}

func TestSynthStopDisables(t *testing.T) {
	s, r := newSynth(SyntheticOptions{})
	s.Start()
	s.Stop()
	s.emitChatOpen(false)
	if len(r.commits) != 0 {
		t.Fatalf("stopped engine should emit nothing, got %v", r.names())
	}
}

func TestSynthActivityBitmap(t *testing.T) {
	s, r := newSynth(SyntheticOptions{})
	s.markActivity()                // marks slice 0 active
	for range activityFlushSlices { // 5 slices → flush at slice 5
		s.recordActivitySlice()
	}
	ua := r.find("UserActivity")
	if ua == nil || ua["userActivityBitmapLen"] != activityFlushSlices ||
		ua["userActivitySessionSeq"] != 1 || ua["userActivityBitmapLow"] != int64(1) {
		t.Fatalf("UserActivity = %v", ua)
	}
	if !r.has("TsBitArray") {
		t.Error("UserActivity should be paired with TsBitArray")
	}
}

func TestSynthAmbientEmitters(t *testing.T) {
	s, r := newSynth(SyntheticOptions{})

	s.emitEmojiOpen()
	if tab := r.find("WebcEmojiOpen")["webcEmojiOpenTab"]; tab != "EMOJI" && tab != "GIF" && tab != "STICKER" {
		t.Errorf("emoji tab = %v", tab)
	}
	r.commits = nil
	s.emitContactSearch()
	cs := r.find("ContactSearchExperience")
	if cs == nil || cs["contactSearchEntrypoint"] != "CHATS_LIST_GLOBAL_SEARCH" {
		t.Fatalf("ContactSearchExperience = %v", cs)
	}
	if a := cs["searchActionName"]; a != "SEARCH_START" && a != "CLICK_ON_CONTACT" {
		t.Errorf("searchActionName = %v", a)
	}
	r.commits = nil
	s.emitMemoryStat()
	if r.find("MemoryStat")["processType"] != "main" {
		t.Errorf("MemoryStat processType wrong: %v", r.find("MemoryStat"))
	}
}

func TestSynthPickAmbientFires(t *testing.T) {
	s, r := newSynth(SyntheticOptions{})
	s.rememberChat(false) // so the high-weight chat-open ambient can fire
	for range 60 {
		s.pickAmbient()
	}
	if len(r.commits) == 0 {
		t.Fatal("pickAmbient over 60 iterations should have fabricated something")
	}
}
