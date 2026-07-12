package wam

import (
	"fmt"
	"testing"
)

// TestCrossCheckResolveFields locks in Go/TS parity on enum resolution and field
// order. The `want` strings are the verbatim output of the zapo-js
// resolveWamEventFields for the same payloads (kind int == 1). If the registry or
// resolver drifts from the TS implementation, this fails.
func TestCrossCheckResolveFields(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"UiAction", map[string]any{"uiActionType": "CHAT_OPEN"}, `{"id":1,"kind":1,"value":3}`},
		{"MessageSend", map[string]any{"messageType": "TEXT", "messageSendResult": "OK"}, `{"id":1,"kind":1,"value":1}`},
		{"WebcChatOpen", map[string]any{"webcChatType": "SOLO"}, ``},
	}
	for _, c := range cases {
		def := reg.Events[c.name]
		fields := resolveEventFields(def, c.payload)
		got := ""
		for _, f := range fields {
			got += fmt.Sprintf(`{"id":%d,"kind":%d,"value":%v}`, f.id, f.kind, f.value)
		}
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}
