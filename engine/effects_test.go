package engine

import (
	"testing"
	"time"

	"github.com/marrasen/crossroads/fx"
	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// effectRig is the start show, the faders' wire into the dome pulled,
// with a programme that snaps the dome's red up and fades it out over
// a second, and another that waits for the next bar to flash its blue.
type effectRig struct {
	s     show.Show
	e     *Engine
	now   time.Time
	mixer string
	light string
}

func newEffectRig(t *testing.T) *effectRig {
	t.Helper()
	r := &effectRig{s: show.Start(), now: time.Unix(1000, 0)}
	r.mixer = moduleNamed(t, &r.s, "Mixer").ID
	dome := moduleNamed(t, &r.s, "Dome").ID
	var keep []show.Wire
	for _, w := range r.s.Wires {
		if w.From.Module != dome {
			keep = append(keep, w)
		}
	}
	r.s.Wires = keep
	r.light = r.s.Rig.Lights[0].ID
	// A straight curve, so a level reads as set.
	r.s.Rig.Lights[0].Gamma = 1
	r.s.Programs = []fx.Program{
		{ID: "hit", Name: "Red hit", Steps: []fx.Step{{ID: "s", FixtureID: r.light, Target: 1, From: 255, To: 0, DurationMs: 1000, ScaleByVelocity: true}}},
		{ID: "bar", Name: "Blue on the bar", Quantize: fx.QuantizeBar, Steps: []fx.Step{{ID: "s", FixtureID: r.light, Target: 3, To: 255}}},
	}
	r.e = New(nil)
	r.e.Now = func() time.Time { return r.now }
	t.Cleanup(r.e.Close)
	return r
}

func (r *effectRig) wire(from, fp, to, tp string) {
	r.s.Wires = append(r.s.Wires, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: from, Port: fp}, To: show.End{Module: to, Port: tp}})
}

func (r *effectRig) tick(d time.Duration) Live {
	r.now = r.now.Add(d)
	return r.e.Tick()
}

// TestAnEffectPlaysItsProgrammeAsItIsFired: snapped up, fading over its
// second, and left where it ended.
func TestAnEffectPlaysItsProgrammeAsItIsFired(t *testing.T) {
	r := newEffectRig(t)
	btn, ef := r.s.NewID("m"), r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: btn, Kind: show.Button}, show.Module{ID: ef, Kind: show.Effect, Name: "Hit", Program: "hit"})
	r.wire(btn, "out", ef, "go")
	r.wire(ef, "out", r.mixer, "in1")
	r.e.Set(r.s)
	r.tick(0)
	r.e.Press(btn)
	if live := r.tick(0); live.Stage[0] != 255 {
		t.Errorf("fired, the hit plays red %d", live.Stage[0])
	}
	if live := r.tick(500 * time.Millisecond); live.Stage[0] < 120 || live.Stage[0] > 135 {
		t.Errorf("half way, the hit plays red %d", live.Stage[0])
	}
	r.tick(600 * time.Millisecond)
	if live := r.tick(time.Second); live.Stage[0] != 0 || live.Values[ef] != 0 {
		t.Errorf("ended, the hit plays red %d, and says it plays %v", live.Stage[0], live.Values[ef])
	}
}

// TestARouteFiresAProgrammeAtItsPadsVelocity, and one waiting for the
// bar waits for it.
func TestARouteFiresAProgrammeAtItsPadsVelocity(t *testing.T) {
	r := newEffectRig(t)
	d := &fakeMIDI{}
	tp, rt := r.s.NewID("m"), r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: tp, Kind: show.Tempo, BPM: 120, Manual: true},
		show.Module{ID: rt, Kind: show.Routes, Name: "Pads", Routes: []routing.Route{
			{ID: "hit", Enabled: true, Input: routing.Input{DeviceName: "Pads", Type: routing.NoteOn, Data1: routing.One(36)},
				Output: routing.Output{Kind: routing.ToFx, ProgramID: "hit"}},
			{ID: "bar", Enabled: true, Input: routing.Input{DeviceName: "Pads", Type: routing.NoteOn, Data1: routing.One(37)},
				Output: routing.Output{Kind: routing.ToFx, ProgramID: "bar"}},
		}})
	r.wire(tp, "out", rt, "clock")
	r.wire(rt, "out", r.mixer, "in1")
	r.e.MIDI = d
	r.e.Set(r.s)
	waitPorts(t, r.e)
	d.play(midi.NoteOn(0, 36, 64))
	if live := r.tick(time.Millisecond); live.Stage[0] < 125 || live.Stage[0] > 131 {
		t.Errorf("hit at half velocity, the pad plays red %d", live.Stage[0])
	}
	// Half a beat into a bar of 120: the blue waits three and a half
	// beats, 1.75 s.
	for range 9 {
		r.tick(25 * time.Millisecond)
	}
	d.play(midi.NoteOn(0, 37, 127))
	if live := r.tick(25 * time.Millisecond); live.Stage[2] != 0 {
		t.Errorf("before the bar, the blue plays %d", live.Stage[2])
	}
	var blue byte
	for range 80 {
		blue = r.tick(25 * time.Millisecond).Stage[2]
	}
	if blue != 255 {
		t.Errorf("on the bar, the blue plays %d", blue)
	}
}
