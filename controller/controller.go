// Package controller knows MIDI controllers: the built-in layouts of
// the controllers crossroads knows by name, and how their knobs say how
// far they turned.
package controller

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// A Layout is a controller crossroads knows: its controls, as it sends
// them out of the box.
type Layout struct {
	ID, Name string
	Controls []show.Control
	// Mackie says the controller runs in Mackie Control mode: it is
	// sent [MackieHello] as it connects, its knobs turn as
	// [MackieTicks] says, and its rings light as [Ring] and [RingCC]
	// say.
	Mackie bool
}

// XTouchMiniID is the X-Touch Mini's layout's ID.
const XTouchMiniID = "xtouch-mini"

// Layouts are the built-in layouts, in the order they are offered.
var Layouts = []Layout{xTouchMini()}

// Find returns the layout id, and false for none.
func Find(id string) (Layout, bool) {
	for _, l := range Layouts {
		if l.ID == id {
			return l, true
		}
	}
	return Layout{}, false
}

// ControlsOf are module m's controls: its layout's, or the ones it
// learned.
func ControlsOf(m *show.Module) []show.Control {
	if l, ok := Find(m.Layout); ok {
		return l.Controls
	}
	return m.Controls
}

// xTouchMini is a Behringer X-Touch Mini in Mackie Control mode, as
// checked on the device: knobs 1 to 8 turn controllers 16 to 23 and
// light rings 48 to 55, pressed they play notes 32 to 39; its sixteen
// buttons and Layer A and B play their notes and light; its fader bends
// the pitch on channel 9. All but the fader are on channel 1.
func xTouchMini() Layout {
	l := Layout{ID: XTouchMiniID, Name: "Behringer X-Touch Mini", Mackie: true}
	for i := range 8 {
		n := strconv.Itoa(i + 1)
		l.Controls = append(l.Controls, show.Control{ID: "knob" + n, Name: "Knob " + n, Kind: show.Knob, CC: 16 + i, Note: -1, Lit: true})
	}
	for i := range 8 {
		n := strconv.Itoa(i + 1)
		l.Controls = append(l.Controls, show.Control{ID: "push" + n, Name: "Knob " + n + " pushed", Kind: show.Press, CC: -1, Note: 32 + i})
	}
	notes := []int{89, 90, 40, 41, 42, 43, 44, 45, 87, 88, 91, 92, 86, 93, 94, 95}
	for i, note := range notes {
		row, col := "top", i+1
		if i >= 8 {
			row, col = "bottom", i-7
		}
		l.Controls = append(l.Controls, show.Control{ID: fmt.Sprintf("button%d", i+1), Name: fmt.Sprintf("Button %s %d", row, col),
			Kind: show.Press, CC: -1, Note: note, Lit: true})
	}
	l.Controls = append(l.Controls,
		show.Control{ID: "layerA", Name: "Layer A", Kind: show.Press, CC: -1, Note: 84, Lit: true},
		show.Control{ID: "layerB", Name: "Layer B", Kind: show.Press, CC: -1, Note: 85, Lit: true},
		show.Control{ID: "fader", Name: "Fader", Kind: show.Slider, Channel: 8, CC: -1, Note: -1, Bend: true})
	return l
}

// boardControls are the controls a controller plays a Board with, by
// ID and kind: eight knobs, each pressed too, sixteen buttons, Layer A
// and B, and a fader.
func boardControls() map[string]show.ControlKind {
	want := map[string]show.ControlKind{"layerA": show.Press, "layerB": show.Press, "fader": show.Slider}
	for i := 1; i <= 8; i++ {
		n := strconv.Itoa(i)
		want["knob"+n], want["push"+n] = show.Knob, show.Press
	}
	for i := 1; i <= 16; i++ {
		want["button"+strconv.Itoa(i)] = show.Press
	}
	return want
}

// PlaysBoard reports whether Controller module m has the controls a
// Board is played with, as the X-Touch Mini's layout has.
func PlaysBoard(m *show.Module) bool {
	if m == nil || m.Kind != show.Controller {
		return false
	}
	want := boardControls()
	for _, c := range ControlsOf(m) {
		if k, ok := want[c.ID]; ok && k == c.Kind {
			delete(want, c.ID)
		}
	}
	return len(want) == 0
}

// XTouchMiniPort is the first of the MIDI ports names whose name starts
// with "X-TOUCH MINI", in any case, as an X-Touch Mini's does; "" for
// none.
func XTouchMiniPort(names []string) string {
	for _, n := range names {
		if strings.HasPrefix(strings.ToUpper(n), "X-TOUCH MINI") {
			return n
		}
	}
	return ""
}

// Ticks is how far a knob turned, in steps, from the value it sent: 1
// to 63 up, 65 to 127 down, as a relative knob sends in two's
// complement.
func Ticks(v byte) int {
	switch {
	case v == 0 || v == 64:
		return 0
	case v < 64:
		return int(v)
	}
	return int(v) - 128
}

// MackieTicks is how far a knob in Mackie Control mode turned, in
// steps, from the value it sent: 1 to 63 up, 65 to 127 down, the steps
// being the value less 64. This is not two's complement: 65 is one step
// down.
func MackieTicks(v byte) int {
	switch {
	case v == 0 || v == 64:
		return 0
	case v < 64:
		return int(v)
	}
	return 64 - int(v)
}

// MackieHello is what a controller in Mackie Control mode is sent as it
// connects, before its lights: the switch to Mackie Control mode, CC
// 127 at 1, on all 16 channels. The X-Touch Mini takes it on its global
// channel, which its editor can change, and ignores it on the others.
func MackieHello() []midi.Message {
	out := make([]midi.Message, 16)
	for ch := range out {
		out[ch] = midi.Control(ch, 127, 1)
	}
	return out
}

// The button lights in Mackie Control mode: a note on, the button's own
// note, at one of these velocities.
const (
	LightOff   = 0
	LightOn    = 127
	LightBlink = 1
)

// ringFan is what a ring in Mackie Control mode adds to its position to
// fill from the left.
const ringFan = 32

// Ring is what a knob's ring in Mackie Control mode is sent for value
// v, 0 to 1: dark at 0, and otherwise lit from the left, 1 to 11 of its
// LEDs.
func Ring(v float64) int {
	if v <= 0 {
		return ringFan
	}
	return ringFan + min(max(int(math.Round(v*11)), 1), 11)
}

// RingCC is the controller number that lights the ring of a knob in
// Mackie Control mode that turns controller cc: 48 to 55 for knobs 16
// to 23.
func RingCC(cc int) int { return cc + 32 }

// IsStandard reports whether a message of kind on channel ch for number
// is an X-Touch Mini's knob in its Standard mode, not Mackie Control
// mode: CC 1 to 8 on channel 11.
func IsStandard(kind midi.Kind, ch, number int) bool {
	return kind == midi.KindControl && ch == 10 && number >= 1 && number <= 8
}

// Learned is the control a controller learns from a message: a button
// from a note, a slider from a controller's move, named for what it
// sends; ok is false for a message that is neither.
func Learned(id string, kind byte, channel, number int) (c show.Control, ok bool) {
	switch kind {
	case 0x90:
		return show.Control{ID: id, Name: fmt.Sprintf("Note %d", number), Kind: show.Press, Channel: channel, CC: -1, Note: number}, true
	case 0xB0:
		return show.Control{ID: id, Name: fmt.Sprintf("Control %d", number), Kind: show.Slider, Channel: channel, CC: number, Note: -1}, true
	}
	return show.Control{}, false
}
