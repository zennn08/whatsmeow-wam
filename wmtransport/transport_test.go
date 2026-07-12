package wmtransport

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
)

// stubSender records queries and replays a scripted sequence of errors (nil == ack).
type stubSender struct {
	queries []whatsmeow.InfoQuery
	errs    []error // one per attempt; runs off the end -> nil
}

func (s *stubSender) SendIQ(_ context.Context, q whatsmeow.InfoQuery) (*waBinary.Node, error) {
	s.queries = append(s.queries, q)
	i := len(s.queries) - 1
	if i < len(s.errs) {
		return nil, s.errs[i]
	}
	return &waBinary.Node{Tag: "iq"}, nil
}

func newTestTransport(s *stubSender) *Transport {
	tr := New(s)
	tr.backoffFn = func(int) time.Duration { return 0 } // no real sleeping in tests
	return tr
}

// TestUploadStanzaShape asserts the w:stats IQ is built as WA Web sends it:
// <iq type=set xmlns=w:stats to=s.whatsapp.net><add t=...>BATCH</add></iq>.
func TestUploadStanzaShape(t *testing.T) {
	s := &stubSender{}
	batch := []byte{0x57, 0x41, 0x4d, 0x05}
	if err := newTestTransport(s).Upload(context.Background(), batch); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if len(s.queries) != 1 {
		t.Fatalf("expected 1 send, got %d", len(s.queries))
	}
	q := s.queries[0]
	if q.Namespace != "w:stats" {
		t.Errorf("namespace = %q, want w:stats", q.Namespace)
	}
	if q.Type != whatsmeow.IQSet {
		t.Errorf("type = %q, want set", q.Type)
	}
	if q.To.String() != "s.whatsapp.net" {
		t.Errorf("to = %q, want s.whatsapp.net", q.To.String())
	}
	children, ok := q.Content.([]waBinary.Node)
	if !ok || len(children) != 1 {
		t.Fatalf("expected 1 child node, got %#v", q.Content)
	}
	add := children[0]
	if add.Tag != "add" {
		t.Errorf("child tag = %q, want add", add.Tag)
	}
	if add.Attrs["t"] == nil || add.Attrs["t"] == "" {
		t.Error("missing add timestamp")
	}
	body, ok := add.Content.([]byte)
	if !ok || string(body) != string(batch) {
		t.Errorf("child content = %#v, want the batch bytes", add.Content)
	}
}

// TestUploadRetriesOn5xx retries transient server errors then succeeds.
func TestUploadRetriesOn5xx(t *testing.T) {
	s := &stubSender{errs: []error{
		&whatsmeow.IQError{Code: 503, Text: "service-unavailable"},
		&whatsmeow.IQError{Code: 500, Text: "internal"},
	}}
	if err := newTestTransport(s).Upload(context.Background(), []byte{1}); err != nil {
		t.Fatalf("Upload should have succeeded on retry, got %v", err)
	}
	if len(s.queries) != 3 {
		t.Fatalf("expected 3 attempts (2 fail + 1 ok), got %d", len(s.queries))
	}
}

// TestUploadDoesNotRetry4xx returns a permanent error immediately.
func TestUploadDoesNotRetry4xx(t *testing.T) {
	s := &stubSender{errs: []error{&whatsmeow.IQError{Code: 400, Text: "bad-request"}}}
	err := newTestTransport(s).Upload(context.Background(), []byte{1})
	if err == nil {
		t.Fatal("expected error for 4xx")
	}
	if len(s.queries) != 1 {
		t.Fatalf("4xx should not retry; got %d attempts", len(s.queries))
	}
}

// TestUploadExhaustsRetries returns the last error after maxAttempts 5xx failures.
func TestUploadExhaustsRetries(t *testing.T) {
	s := &stubSender{errs: []error{
		&whatsmeow.IQError{Code: 500},
		&whatsmeow.IQError{Code: 500},
		&whatsmeow.IQError{Code: 500},
		&whatsmeow.IQError{Code: 500},
	}}
	err := newTestTransport(s).Upload(context.Background(), []byte{1})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if len(s.queries) != defaultMaxAttempts {
		t.Fatalf("expected %d attempts, got %d", defaultMaxAttempts, len(s.queries))
	}
}
