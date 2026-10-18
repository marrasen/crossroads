package routing

import "math"

// A Type is one kind of channel-voice message.
//
// A MIDI channel-voice message is one status byte and up to two data
// bytes. The status byte's high nibble is the type and its low nibble
// is the channel, counted from zero.
type Type string

// The channel-voice messages midittt routes.
const (
	NoteOn          Type = "noteon"
	NoteOff         Type = "noteoff"
	CC              Type = "cc"
	ProgramChange   Type = "programchange"
	PitchBend       Type = "pitchbend"
	Aftertouch      Type = "aftertouch" // polyphonic key pressure
	ChannelPressure Type = "channelpressure"
)

// Types are every message type, in the order the pickers show them.
var Types = []Type{NoteOn, NoteOff, CC, ProgramChange, PitchBend, Aftertouch, ChannelPressure}

// TypeLabels name each type for a picker.
var TypeLabels = map[Type]string{
	NoteOn:          "Note ON",
	NoteOff:         "Note OFF",
	CC:              "Control Change (CC)",
	ProgramChange:   "Program Change",
	PitchBend:       "Pitch Bend",
	Aftertouch:      "Aftertouch (poly)",
	ChannelPressure: "Channel Pressure",
}

// Label names t for a picker, falling back to the bare type.
func (t Type) Label() string {
	if l, ok := TypeLabels[t]; ok {
		return l
	}
	return string(t)
}

// HasData2 reports whether a type carries a second data byte.
func (t Type) HasData2() bool {
	return t != ProgramChange && t != ChannelPressure
}

// status holds the high-nibble status code of each type.
var status = map[Type]byte{
	NoteOff:         0x8,
	NoteOn:          0x9,
	Aftertouch:      0xa,
	CC:              0xb,
	ProgramChange:   0xc,
	ChannelPressure: 0xd,
	PitchBend:       0xe,
}

var typeOf = map[byte]Type{
	0x8: NoteOff,
	0x9: NoteOn,
	0xa: Aftertouch,
	0xb: CC,
	0xc: ProgramChange,
	0xd: ChannelPressure,
	0xe: PitchBend,
}

// A Message is one parsed channel-voice message.
type Message struct {
	Type Type
	// Channel is the MIDI channel, 1 to 16.
	Channel int
	// Data1 is the first data byte: the note, CC or programme number.
	Data1 int
	// Data2 is the second: the velocity, CC value or pressure. Zero
	// where the type has none.
	Data2 int
	// Raw is the bytes the message was parsed from, empty for one built
	// to fire a route by hand.
	Raw []byte
}

// Parse reads raw bytes as a channel-voice message.
//
// It reports false for anything else, such as SysEx, clock or another
// system message, which routing ignores. A Note ON of velocity zero
// means Note OFF, so it comes back as one and routing has a single
// case to handle.
func Parse(data []byte) (Message, bool) {
	if len(data) == 0 {
		return Message{}, false
	}
	st := data[0]
	// System messages, 0xF0 to 0xFF, are not channel-voice.
	if st < 0x80 || st >= 0xf0 {
		return Message{}, false
	}
	t, ok := typeOf[st>>4]
	if !ok {
		return Message{}, false
	}
	m := Message{Type: t, Channel: int(st&0x0f) + 1, Raw: data}
	if len(data) > 1 {
		m.Data1 = int(data[1])
	}
	if len(data) > 2 {
		m.Data2 = int(data[2])
	}
	if m.Type == NoteOn && m.Data2 == 0 {
		m.Type = NoteOff
	}
	return m, true
}

// Build serializes a message back into bytes ready to send. channel is
// counted from one, and the values are clamped to what MIDI allows.
func Build(t Type, channel, data1, data2 int) []byte {
	ch := clamp(channel, 1, 16) - 1
	out := []byte{status[t]<<4 | byte(ch), byte(Clamp7(data1))}
	if t.HasData2() {
		out = append(out, byte(Clamp7(data2)))
	}
	return out
}

// Clamp7 clamps a value to a seven-bit MIDI data byte, 0 to 127.
func Clamp7(v int) int { return clamp(v, 0, 127) }

func clamp(v, lo, hi int) int { return min(hi, max(lo, v)) }

// Clamp7f rounds and clamps a computed value to a seven-bit data byte.
func Clamp7f(v float64) int {
	if math.IsNaN(v) {
		return 0
	}
	return Clamp7(int(math.Round(v)))
}
