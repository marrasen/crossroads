package engine

import (
	"testing"
	"time"

	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/audio/midi"
)

// TestMIDIRoutesPlayAsInMidittt: a pad's note sets the dome's red from
// its velocity, a held pad holds a pattern, a note goes on to a synth
// as a controller's move, and each beat starts a timer.
func TestMIDIRoutesPlayAsInMidittt(t *testing.T) {
	d := &fakeMIDI{}
	s := show.Start()
	mixer := moduleNamed(t, &s, "Mixer").ID
	dome := moduleNamed(t, &s, "Dome").ID
	var keep []show.Wire
	for _, w := range s.Wires {
		if w.From.Module != dome {
			keep = append(keep, w)
		}
	}
	s.Wires = keep
	light := s.Rig.Lights[0].ID
	s.Rig.Groups = []fixtures.Group{{ID: "all", MemberIDs: []string{light}}}
	s.Patterns = []pattern.Pattern{{ID: "blue", Bars: 1, Release: pattern.Release{Rest: true}, Lanes: []pattern.Lane{
		{ID: "l", GroupID: "all", Clips: []pattern.Clip{{ID: "c", Start: 0, Length: 4, Kind: pattern.KindColour, Colour: pattern.Colour{B: 255}}}}}}}
	tm, tp, pt, rt := s.NewID("m"), s.NewID("m"), s.NewID("m"), s.NewID("m")
	s.Modules = append(s.Modules,
		show.Module{ID: tm, Kind: show.Timer, Name: "Set", Seconds: 60},
		show.Module{ID: tp, Kind: show.Tempo, BPM: 120, Manual: true},
		show.Module{ID: pt, Kind: show.Pattern, Pattern: "blue"},
		show.Module{ID: rt, Kind: show.Routes, Name: "Pads", Routes: []routing.Route{
			{ID: "red", Name: "Red pad", Enabled: true, Input: routing.Input{DeviceName: "Pads", Channel: 1, Type: routing.NoteOn, Data1: routing.One(36)},
				Output: routing.Output{Kind: routing.ToFixture, FixtureID: light, Target: 1, Value: routing.Value{Mode: routing.Passthrough}}},
			{ID: "hold", Name: "Blue pad", Enabled: true, Input: routing.Input{DeviceName: "Pads", Channel: 1, Type: routing.NoteOn, Data1: routing.One(37)},
				Output: routing.Output{Kind: routing.ToPattern, PatternID: "blue", Pattern: routing.PatternHold}},
			{ID: "synth", Name: "To the synth", Enabled: true, Input: routing.Input{DeviceName: "Pads", Channel: routing.AnyChannel, Type: routing.NoteOn, Data1: routing.One(38)},
				Output: routing.Output{Kind: routing.ToMidi, DeviceName: "Pads", Channel: 2, Type: routing.CC, Data1: 20, Value: routing.Value{Mode: routing.Fixed, Fixed: 99}}},
			{ID: "beat", Name: "Every beat", Enabled: true, HasBeat: true, Beat: routing.Beat{EveryBeats: 1},
				Output: routing.Output{Kind: routing.ToTimer, TimerID: tm, Command: routing.TimerStart}},
		}})
	wire := func(from, fp, to, tp string) {
		s.Wires = append(s.Wires, show.Wire{ID: s.NewID("w"), From: show.End{Module: from, Port: fp}, To: show.End{Module: to, Port: tp}})
	}
	wire(tp, "out", rt, "clock")
	wire(tp, "out", pt, "clock")
	wire(rt, "out", mixer, "in1")
	wire(pt, "out", mixer, "in2")
	now := time.Unix(1000, 0)
	e := New(nil)
	e.MIDI = d
	e.Now = func() time.Time { return now }
	defer e.Close()
	e.Set(s)
	waitPorts(t, e)
	tick := func() Live {
		now = now.Add(time.Second / 40)
		return e.Tick()
	}
	d.took()
	d.play(midi.NoteOn(0, 36, 127))
	if live := tick(); live.Stage[0] != 255 || live.Owners[1] != rt {
		t.Errorf("the red pad hit full plays red %d, owned by %q", live.Stage[0], live.Owners[1])
	}
	d.play(midi.NoteOn(0, 37, 100))
	tick()
	if live := tick(); live.Stage[2] != 255 {
		t.Errorf("the blue pad held plays blue %d", live.Stage[2])
	}
	d.play(midi.NoteOff(0, 37, 0))
	tick()
	if live := tick(); live.Stage[2] != 0 {
		t.Errorf("the blue pad let go still plays blue %d", live.Stage[2])
	}
	d.play(midi.NoteOn(5, 38, 64))
	tick()
	if sent := d.took(); !contains(sent, midi.Control(1, 20, 99)) {
		t.Errorf("the synth pad sent %v", sent)
	}
	for range 40 {
		tick()
	}
	if live := tick(); !live.Timers[tm].Running {
		t.Error("a second of beats did not start the timer")
	}
}
