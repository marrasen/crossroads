package controller

import (
	"testing"

	"github.com/marrasen/crossroads/show"
)

// TestARelativeKnobSaysHowFarItTurned, up from 1 and down from 127.
func TestARelativeKnobSaysHowFarItTurned(t *testing.T) {
	for v, want := range map[byte]int{0: 0, 1: 1, 3: 3, 63: 63, 64: 0, 65: -63, 126: -2, 127: -1} {
		if got := Ticks(v); got != want {
			t.Errorf("Ticks(%d) = %d, want %d", v, got, want)
		}
	}
}

// TestAMackieKnobSaysHowFarItTurned: 1 to 63 up, and 65 one step
// down, not 63.
func TestAMackieKnobSaysHowFarItTurned(t *testing.T) {
	for v, want := range map[byte]int{0: 0, 1: 1, 7: 7, 63: 63, 64: 0, 65: -1, 71: -7, 127: -63} {
		if got := MackieTicks(v); got != want {
			t.Errorf("MackieTicks(%d) = %d, want %d", v, got, want)
		}
	}
}

// TestARingFillsFromTheLeftAsItsValueGrows: dark at 0, one LED for a
// little, all 11 at full, fan style each time.
func TestARingFillsFromTheLeftAsItsValueGrows(t *testing.T) {
	for v, want := range map[float64]int{0: 32, 0.01: 33, 0.5: 38, 1: 43, 2: 43} {
		if got := Ring(v); got != want {
			t.Errorf("Ring(%v) = %d, want %d", v, got, want)
		}
	}
}

// TestTheXTouchMiniIsSwitchedToMackieControlOnEveryChannel: CC 127 at
// 1, B0 7F 01 to BF 7F 01.
func TestTheXTouchMiniIsSwitchedToMackieControlOnEveryChannel(t *testing.T) {
	got := MackieHello()
	if len(got) != 16 {
		t.Fatalf("the switch is %d messages", len(got))
	}
	for ch, m := range got {
		if m.Status != byte(0xB0+ch) || m.Data1 != 127 || m.Data2 != 1 {
			t.Errorf("the switch on channel %d is %X %X %X", ch+1, m.Status, m.Data1, m.Data2)
		}
	}
}

// TestTheXTouchMiniHasEachControlOnce: 8 knobs and their pushes, 16
// buttons, layer A and B and a fader, no ID nor MIDI message twice.
func TestTheXTouchMiniHasEachControlOnce(t *testing.T) {
	l, ok := Find(XTouchMiniID)
	if !ok || len(l.Controls) != 35 {
		t.Fatalf("the X-Touch Mini has %d controls", len(l.Controls))
	}
	ids, ccs, notes := map[string]bool{}, map[int]bool{}, map[int]bool{}
	for _, c := range l.Controls {
		if ids[c.ID] || c.CC >= 0 && ccs[c.CC] || c.Note >= 0 && notes[c.Note] {
			t.Errorf("%+v is there twice", c)
		}
		ids[c.ID], ccs[c.CC], notes[c.Note] = true, true, true
	}
	m := show.Module{Layout: XTouchMiniID, Controls: []show.Control{{ID: "mine"}}}
	if len(ControlsOf(&m)) != 35 {
		t.Error("a layout's own controls give way to the ones learned")
	}
	m.Layout = ""
	if got := ControlsOf(&m); len(got) != 1 {
		t.Errorf("with no layout, the controls are %v", got)
	}
}

// TestAControlIsLearnedFromANoteOrAControllersMove: a button from a
// note, a slider from a controller, and nothing from anything else.
func TestAControlIsLearnedFromANoteOrAControllersMove(t *testing.T) {
	if c, ok := Learned("c1", 0x90, 2, 36); !ok || c.Kind != show.Press || c.Note != 36 || c.CC != -1 || c.Channel != 2 {
		t.Errorf("a note learns %+v", c)
	}
	if c, ok := Learned("c2", 0xB0, 0, 7); !ok || c.Kind != show.Slider || c.CC != 7 || c.Name != "Control 7" {
		t.Errorf("a controller's move learns %+v", c)
	}
	if _, ok := Learned("c3", 0xE0, 0, 0); ok {
		t.Error("a pitch bend learned a control")
	}
}

// TestTheXTouchMinisControlsSendWhatTheDeviceSends in Mackie Control
// mode, the IDs kept from before so saved wires still fit.
func TestTheXTouchMinisControlsSendWhatTheDeviceSends(t *testing.T) {
	l, _ := Find(XTouchMiniID)
	byID := map[string]show.Control{}
	for _, c := range l.Controls {
		byID[c.ID] = c
	}
	want := map[string][3]int{ // channel, CC, note
		"knob1": {0, 16, -1}, "knob8": {0, 23, -1}, "push1": {0, -1, 32}, "push8": {0, -1, 39},
		"button1": {0, -1, 89}, "button2": {0, -1, 90}, "button3": {0, -1, 40}, "button8": {0, -1, 45},
		"button9": {0, -1, 87}, "button10": {0, -1, 88}, "button11": {0, -1, 91}, "button12": {0, -1, 92},
		"button13": {0, -1, 86}, "button14": {0, -1, 93}, "button16": {0, -1, 95},
		"layerA": {0, -1, 84}, "layerB": {0, -1, 85}, "fader": {8, -1, -1},
	}
	for id, w := range want {
		c, ok := byID[id]
		if !ok || [3]int{c.Channel, c.CC, c.Note} != w {
			t.Errorf("%s is %+v, want channel, CC and note %v", id, c, w)
		}
	}
	if !byID["fader"].Bend || !byID["layerA"].Lit || !byID["layerB"].Lit || !byID["knob1"].Lit {
		t.Error("the fader does not bend, or a layer button or a ring does not light")
	}
}

// TestTheXTouchMiniPlaysABoardAndIsFoundByItsPort: its layout has the
// controls a Board is played with, one of learned controls has not; its
// port is the first named X-TOUCH MINI, in any case.
func TestTheXTouchMiniPlaysABoardAndIsFoundByItsPort(t *testing.T) {
	if !PlaysBoard(&show.Module{Kind: show.Controller, Layout: XTouchMiniID}) {
		t.Error("the X-Touch Mini cannot play a board")
	}
	learned := &show.Module{Kind: show.Controller, Controls: xTouchMini().Controls[:20]}
	if PlaysBoard(learned) || PlaysBoard(&show.Module{Kind: show.Board, Layout: XTouchMiniID}) || PlaysBoard(nil) {
		t.Error("a controller without the controls, or a board, plays a board")
	}
	if got := XTouchMiniPort([]string{"Synth", "X-Touch Mini MIDI 1", "X-TOUCH MINI"}); got != "X-Touch Mini MIDI 1" {
		t.Errorf("the X-Touch Mini's port is %q", got)
	}
	if got := XTouchMiniPort([]string{"Synth", "My X-TOUCH MINI"}); got != "" {
		t.Errorf("with none, the X-Touch Mini's port is %q", got)
	}
}
