package wam

import (
	"encoding/binary"
	"math"
)

// Wire-format constants from WAWebWamLibProtocol, ported from the zapo-js
// @zapo-js/wam encoder. Marker bytes split into the bottom 4 bits (marker type
// + last/extendedId flags) and the top 4 bits (value type + size class). These
// are stable protocol constants; TestWireConstantsMatchRegistry asserts they
// still agree with the embedded registry's wireFormat.
const (
	markGlobalAttribute = 0
	markEvent           = 1
	markField           = 2
	markLastFlag        = 4
	markExtendedIDFlag  = 8

	encNull        = 0
	encIntZero     = 16
	encIntOne      = 32
	encInt8        = 48
	encInt16       = 64
	encInt32       = 80
	encInt64       = 96
	encFloat64     = 112
	encStringShort = 128
	encStringMed   = 144
	encStringLong  = 160
)

// int32UpperExclusive mirrors WA Web's `r < 2147483648` int32 check in WAWamBuffer.
const int32UpperExclusive = 2147483648

// valueKind is how a value is encoded on the wire (from the registry field/global type).
type valueKind uint8

const (
	kindInvalid valueKind = iota
	kindInt
	kindFloat
	kindString
	kindBool
)

// resolvedField is a single event field resolved to its wire id, kind, and value.
type resolvedField struct {
	id    int
	kind  valueKind
	value any // int64 | float64 | string | bool
}

// writeTag writes a TLV tag: marker/encoding byte + id (uint8, or extended-flag + uint16 for id >= 256).
func writeTag(buf *writer, id int, markerWithEncoding int) {
	if id < 256 {
		buf.u8(byte(markerWithEncoding))
		buf.u8(byte(id))
	} else {
		buf.u8(byte(markerWithEncoding | markExtendedIDFlag))
		buf.u16(uint16(id))
	}
}

// writeInt writes an integer value with the smallest matching width class.
func writeInt(buf *writer, id, marker int, value int64) {
	switch {
	case value == 0:
		writeTag(buf, id, encIntZero|marker)
	case value == 1:
		writeTag(buf, id, encIntOne|marker)
	case value >= -128 && value < 128:
		writeTag(buf, id, encInt8|marker)
		buf.u8(byte(int8(value)))
	case value >= -32768 && value < 32768:
		writeTag(buf, id, encInt16|marker)
		buf.u16(uint16(int16(value)))
	case value >= math.MinInt32 && value < int32UpperExclusive:
		writeTag(buf, id, encInt32|marker)
		buf.u32(uint32(int32(value)))
	default:
		writeTag(buf, id, encInt64|marker)
		buf.u64(uint64(value))
	}
}

func writeFloat(buf *writer, id, marker int, value float64) {
	writeTag(buf, id, encFloat64|marker)
	buf.u64(math.Float64bits(value))
}

func writeStringValue(buf *writer, id, marker int, value string) {
	b := []byte(value)
	n := len(b)
	switch {
	case n < 256:
		writeTag(buf, id, encStringShort|marker)
		buf.u8(byte(n))
	case n < 65536:
		writeTag(buf, id, encStringMed|marker)
		buf.u16(uint16(n))
	default:
		writeTag(buf, id, encStringLong|marker)
		buf.u32(uint32(n))
	}
	buf.raw(b)
}

// writeGlobalAttribute writes a global-attribute TLV (marker 0): numbers as int,
// booleans as int 0/1, nil as tag-only.
func writeGlobalAttribute(buf *writer, id int, value any) {
	marker := markGlobalAttribute
	switch v := value.(type) {
	case nil:
		writeTag(buf, id, encNull|marker)
	case string:
		writeStringValue(buf, id, marker, v)
	case bool:
		writeInt(buf, id, marker, boolToInt(v))
	case int:
		writeInt(buf, id, marker, int64(v))
	case int64:
		writeInt(buf, id, marker, v)
	case float64:
		writeInt(buf, id, marker, int64(v))
	default:
		writeTag(buf, id, encNull|marker)
	}
}

// writeEventHeader writes the event-header TLV (marker 1), value = sampling weight;
// sets the last-flag when it has no fields.
func writeEventHeader(buf *writer, id int, weight int64, hasFields bool) {
	marker := markEvent
	if !hasFields {
		marker = markEvent | markLastFlag
	}
	writeInt(buf, id, marker, weight)
}

// writeField writes a field TLV (marker 2), kind-encoded; the last field in a group sets the last-flag.
func writeField(buf *writer, f resolvedField, isLast bool) {
	marker := markField
	if isLast {
		marker = markField | markLastFlag
	}
	switch f.kind {
	case kindString:
		writeStringValue(buf, f.id, marker, f.value.(string))
	case kindFloat:
		writeFloat(buf, f.id, marker, f.value.(float64))
	case kindBool:
		writeInt(buf, f.id, marker, boolToInt(f.value.(bool)))
	default: // kindInt
		writeInt(buf, f.id, marker, f.value.(int64))
	}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// writer is a minimal big-endian byte writer. bytes.Buffer would work, but a tiny
// slice-backed writer keeps allocations down and mirrors the TS BinaryWriter.
type writer struct {
	b       []byte
	scratch [8]byte
}

func newWriter(capacity int) *writer { return &writer{b: make([]byte, 0, capacity)} }

func (w *writer) u8(v byte) { w.b = append(w.b, v) }
func (w *writer) u16(v uint16) {
	binary.BigEndian.PutUint16(w.scratch[:2], v)
	w.b = append(w.b, w.scratch[:2]...)
}
func (w *writer) u32(v uint32) {
	binary.BigEndian.PutUint32(w.scratch[:4], v)
	w.b = append(w.b, w.scratch[:4]...)
}
func (w *writer) u64(v uint64) {
	binary.BigEndian.PutUint64(w.scratch[:8], v)
	w.b = append(w.b, w.scratch[:8]...)
}
func (w *writer) raw(p []byte) { w.b = append(w.b, p...) }
func (w *writer) str(s string) { w.b = append(w.b, s...) }
func (w *writer) size() int    { return len(w.b) }
func (w *writer) bytes() []byte {
	out := make([]byte, len(w.b))
	copy(out, w.b)
	return out
}
