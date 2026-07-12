package wam

// commitTimeGlobalID is the reserved global attribute WAWebWamLibContext re-stamps
// before every event (unix seconds).
const commitTimeGlobalID = 47

// batch is one WAM batch for a single channel: the header, a running
// committed-globals map (unchanged globals are not re-emitted), and the appended
// events. Mirrors WAWamBuffer + WAWebWamLibContext.
type batch struct {
	channel          string
	streamID         int
	sequenceNumber   int
	w                *writer
	committedGlobals map[int]any
	eventsWritten    int
}

func newBatch(channel string, streamID, sequenceNumber int, initialGlobals []globalKV) *batch {
	b := &batch{
		channel:          channel,
		streamID:         streamID,
		sequenceNumber:   sequenceNumber,
		w:                newWriter(512),
		committedGlobals: make(map[int]any),
	}
	b.w.str("WAM")
	b.w.u8(byte(reg.ProtocolVersion))
	b.w.u8(byte(streamID & 0xff))
	b.w.u16(uint16(sequenceNumber & 0xffff))
	b.w.u8(byte(reg.ChannelWireCodes[channel]))
	for _, g := range initialGlobals {
		writeGlobalAttribute(b.w, g.id, g.value)
		b.committedGlobals[g.id] = g.value
	}
	return b
}

// setGlobal delta-writes a global attribute: only emitted when its value changed.
func (b *batch) setGlobal(id int, value any) {
	if prev, ok := b.committedGlobals[id]; ok && prev == value {
		return
	}
	writeGlobalAttribute(b.w, id, value)
	b.committedGlobals[id] = value
}

// writeEvent appends one event: re-stamps commitTime (id 47, unix seconds), writes
// the event header carrying weight, then each present field in order (the last
// field flagged so the decoder can close the event group).
func (b *batch) writeEvent(commitTimeSecs int64, eventID int, weight int64, fields []resolvedField) {
	writeGlobalAttribute(b.w, commitTimeGlobalID, commitTimeSecs)
	last := len(fields) - 1
	writeEventHeader(b.w, eventID, weight, last >= 0)
	for i := range fields {
		writeField(b.w, fields[i], i == last)
	}
	b.eventsWritten++
}

func (b *batch) size() int       { return b.w.size() }
func (b *batch) hasEvents() bool { return b.eventsWritten > 0 }
func (b *batch) bytes() []byte   { return b.w.bytes() }

// globalKV is an id-keyed global attribute value for a batch header.
type globalKV struct {
	id    int
	value any
}
