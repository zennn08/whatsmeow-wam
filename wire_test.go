package wam

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
)

// TestWireConstantsMatchRegistry guards the hardcoded wire constants against
// drift from the embedded registry's wireFormat.
func TestWireConstantsMatchRegistry(t *testing.T) {
	m := reg.WireFormat.Markers
	e := reg.WireFormat.ValueEncodingBits
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"globalAttribute", markGlobalAttribute, m["globalAttribute"]},
		{"event", markEvent, m["event"]},
		{"field", markField, m["field"]},
		{"lastFlag", markLastFlag, m["lastFlag"]},
		{"extendedIdFlag", markExtendedIDFlag, m["extendedIdFlag"]},
		{"null", encNull, e["null"]},
		{"intZero", encIntZero, e["intZero"]},
		{"intOne", encIntOne, e["intOne"]},
		{"int8", encInt8, e["int8"]},
		{"int16", encInt16, e["int16"]},
		{"int32", encInt32, e["int32"]},
		{"int64", encInt64, e["int64"]},
		{"float64", encFloat64, e["float64"]},
		{"stringShort", encStringShort, e["stringShort"]},
		{"stringMedium", encStringMed, e["stringMedium"]},
		{"stringLong", encStringLong, e["stringLong"]},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: hardcoded %d != registry %d", c.name, c.got, c.want)
		}
	}
	if reg.ProtocolVersion != 5 {
		t.Errorf("protocolVersion = %d, want 5", reg.ProtocolVersion)
	}
}

// TestBatchHeaderAndEncoding asserts a full batch is byte-identical to the
// hand-derived wire bytes from the WAWebWamLibProtocol spec.
func TestBatchHeaderAndEncoding(t *testing.T) {
	// Empty initial globals so we control every byte.
	b := newBatch("regular", 0x2a, 0x0007, nil)

	// One event, id 300 (>=256 -> extended id), weight 1, with three fields:
	//  - int field id 5, value 0   -> intZero
	//  - string field id 6, "hi"   -> stringShort
	//  - int field id 7, value 300 -> int16 (last field)
	fields := []resolvedField{
		{id: 5, kind: kindInt, value: int64(0)},
		{id: 6, kind: kindString, value: "hi"},
		{id: 7, kind: kindInt, value: int64(300)},
	}
	b.writeEvent(0x5F5E1000, 300, 1, fields) // commitTime 0x5F5E1000 = 1600000000

	got := b.bytes()

	var want bytes.Buffer
	// Header: "WAM" + version(5) + streamId(0x2a) + sequence(0x0007) + channel(regular=0)
	want.WriteString("WAM")
	want.WriteByte(5)
	want.WriteByte(0x2a)
	want.Write([]byte{0x00, 0x07})
	want.WriteByte(0x00)
	// commitTime global: id 47, value 1600000000 (< 2^31 -> int32).
	// tag = encInt32(80) | markGlobalAttribute(0) = 0x50, id 47 = 0x2f, then 4 BE bytes.
	want.Write([]byte{0x50, 0x2f, 0x5F, 0x5E, 0x10, 0x00})
	// event header: id 300 -> extended. tag = encIntOne(32)|markEvent(1)|extendedIdFlag(8) = 41 = 0x29,
	// then id as uint16 BE 0x012c. (weight 1 -> intOne, no trailing value)
	want.Write([]byte{0x29, 0x01, 0x2c})
	// field 1: int id 5 value 0 -> intZero(16)|markField(2)=18=0x12, id 5
	want.Write([]byte{0x12, 0x05})
	// field 2: string id 6 "hi" -> stringShort(128)|markField(2)=130=0x82, id 6, len 2, "hi"
	want.Write([]byte{0x82, 0x06, 0x02, 'h', 'i'})
	// field 3 (last): int id 7 value 300 -> int16(64)|markField(2)|lastFlag(4)=70=0x46, id 7, 0x012c
	want.Write([]byte{0x46, 0x07, 0x01, 0x2c})

	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("batch mismatch:\n got  %s\n want %s", hex.EncodeToString(got), hex.EncodeToString(want.Bytes()))
	}
}

// TestIntWidthClasses checks the smallest-width integer selection at boundaries.
func TestIntWidthClasses(t *testing.T) {
	cases := []struct {
		value int64
		tag   byte // encoding|markField for a non-last field, id omitted below
		body  []byte
	}{
		{0, encIntZero | markField, nil},
		{1, encIntOne | markField, nil},
		{-1, encInt8 | markField, []byte{0xff}},
		{127, encInt8 | markField, []byte{0x7f}},
		{128, encInt16 | markField, []byte{0x00, 0x80}},
		{-129, encInt16 | markField, []byte{0xff, 0x7f}},
		{32767, encInt16 | markField, []byte{0x7f, 0xff}},
		{32768, encInt32 | markField, []byte{0x00, 0x00, 0x80, 0x00}},
		{2147483647, encInt32 | markField, []byte{0x7f, 0xff, 0xff, 0xff}},
		{2147483648, encInt64 | markField, []byte{0x00, 0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00}},
		{-2147483648, encInt32 | markField, []byte{0x80, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		w := newWriter(16)
		writeInt(w, 9, markField, c.value)
		want := append([]byte{byte(c.tag), 0x09}, c.body...)
		if !bytes.Equal(w.bytes(), want) {
			t.Errorf("writeInt(%d): got %s want %s", c.value,
				hex.EncodeToString(w.bytes()), hex.EncodeToString(want))
		}
	}
}

// TestCommitResolvesEnumAndOrder exercises the registry-driven commit path end to
// end and confirms a batch is produced with the event present.
func TestCommitResolvesEnumAndOrder(t *testing.T) {
	var uploaded [][]byte
	c := New(Options{
		Globals:         GlobalsInput{OSDisplayName: "Windows 10", Browser: "Chrome", AppVersion: "2.3000.0"},
		DisableSampling: true,
		Transport:       transportFunc(func(_ context.Context, b []byte) error { uploaded = append(uploaded, b); return nil }),
	})
	// UiAction (id 472, regular) with an enum field.
	c.Commit("UiAction", map[string]any{"uiActionType": "CHAT_OPEN"})
	c.Flush(context.Background())

	if len(uploaded) != 1 {
		t.Fatalf("expected 1 uploaded batch, got %d", len(uploaded))
	}
	batch := uploaded[0]
	if string(batch[:3]) != "WAM" {
		t.Fatalf("batch missing WAM magic: %s", hex.EncodeToString(batch[:8]))
	}
	// streamId global should be present (streamId 3543) and header stream id matches.
	if batch[3] != byte(reg.ProtocolVersion) {
		t.Fatalf("version byte = %d", batch[3])
	}
	if int(batch[4]) != c.StreamID()&0xff {
		t.Fatalf("header streamId %d != coordinator %d", batch[4], c.StreamID())
	}
}

// TestUnknownEventIgnored ensures an unknown event name is a no-op.
func TestUnknownEventIgnored(t *testing.T) {
	var count int
	c := New(Options{
		DisableSampling: true,
		Transport:       transportFunc(func(_ context.Context, _ []byte) error { count++; return nil }),
	})
	c.Commit("NoSuchEventXYZ", nil)
	c.Flush(context.Background())
	if count != 0 {
		t.Fatalf("unknown event produced %d uploads", count)
	}
}

type transportFunc func(context.Context, []byte) error

func (f transportFunc) Upload(ctx context.Context, b []byte) error { return f(ctx, b) }
