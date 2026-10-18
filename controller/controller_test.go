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

// TestTheXTouchMiniHasEachControlOnce: 8 knobs and their pushes, 16
// buttons and a fader, no ID nor MIDI message twice.
func TestTheXTouchMiniHasEachControlOnce(t *testing.T) {
	l, ok := Find(XTouchMiniID)
	if !ok || len(l.Controls) != 33 {
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
	if len(ControlsOf(&m)) != 33 {
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
