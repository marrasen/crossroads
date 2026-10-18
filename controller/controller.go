// Package controller knows MIDI controllers: the built-in layouts of
// the controllers crossroads knows by name, and how their knobs say how
// far they turned.
package controller

import (
	"fmt"
	"strconv"

	"github.com/marrasen/crossroads/show"
)

// A Layout is a controller crossroads knows: its controls, as it sends
// them out of the box.
type Layout struct {
	ID, Name string
	Controls []show.Control
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

// xTouchMini is a Behringer X-Touch Mini in its standard mode, layer A,
// on MIDI channel 11, its knobs set to send relative moves in the
// X-Touch editor: knobs 1 to 8 move controllers 1 to 8 and light their
// rings, pressed they play notes 0 to 7; its sixteen buttons play notes
// 8 to 23 and light; its fader moves controller 9.
func xTouchMini() Layout {
	l := Layout{ID: XTouchMiniID, Name: "Behringer X-Touch Mini"}
	const ch = 10
	for i := range 8 {
		n := strconv.Itoa(i + 1)
		l.Controls = append(l.Controls, show.Control{ID: "knob" + n, Name: "Knob " + n, Kind: show.Knob, Channel: ch, CC: i + 1, Note: -1, Lit: true})
	}
	for i := range 8 {
		n := strconv.Itoa(i + 1)
		l.Controls = append(l.Controls, show.Control{ID: "push" + n, Name: "Knob " + n + " pushed", Kind: show.Press, Channel: ch, CC: -1, Note: i})
	}
	for i := range 16 {
		row, col := "top", i+1
		if i >= 8 {
			row, col = "bottom", i-7
		}
		l.Controls = append(l.Controls, show.Control{ID: fmt.Sprintf("button%d", i+1), Name: fmt.Sprintf("Button %s %d", row, col),
			Kind: show.Press, Channel: ch, CC: -1, Note: 8 + i, Lit: true})
	}
	l.Controls = append(l.Controls, show.Control{ID: "fader", Name: "Fader", Kind: show.Slider, Channel: ch, CC: 9, Note: -1})
	return l
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
