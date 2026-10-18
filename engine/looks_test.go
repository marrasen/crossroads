package engine

import (
	"testing"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/mix"
)

// lookRig is the start show, its faders' wire into the mixer pulled,
// with two looks, Red and Blue, into a Chaser and a Cue list, each of
// those into the mixer's free inputs.
type lookRig struct {
	s                              show.Show
	e                              *Engine
	now                            time.Time
	red, blue, chaser, cues, mixer string
}

func newLookRig(t *testing.T) *lookRig {
	t.Helper()
	r := &lookRig{s: show.Start(), now: time.Unix(1000, 0)}
	r.mixer = moduleNamed(t, &r.s, "Mixer").ID
	dome := moduleNamed(t, &r.s, "Dome").ID
	// The faders play nothing: the looks alone light the dome.
	var keep []show.Wire
	for _, w := range r.s.Wires {
		if w.From.Module != dome {
			keep = append(keep, w)
		}
	}
	r.s.Wires = keep
	add := func(m show.Module) string {
		m.ID = r.s.NewID("m")
		r.s.Modules = append(r.s.Modules, m)
		return m.ID
	}
	light := r.s.Rig.Lights[0].ID
	r.red = add(show.Module{Kind: show.Look, Name: "Red", Lights: []string{light}, Levels: map[string]float64{"red": 1}})
	r.blue = add(show.Module{Kind: show.Look, Name: "Blue", Lights: []string{light}, Levels: map[string]float64{"blue": 1}})
	r.chaser = add(show.Module{Kind: show.Chaser, Name: "Chase"})
	r.cues = add(show.Module{Kind: show.Cues, Name: "Cues", Fade: 1})
	wire := func(from, fp, to, tp string) {
		r.s.Wires = append(r.s.Wires, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: from, Port: fp}, To: show.End{Module: to, Port: tp}})
	}
	wire(r.red, "out", r.chaser, "step1")
	wire(r.blue, "out", r.chaser, "step2")
	wire(r.red, "out", r.cues, "cue1")
	wire(r.blue, "out", r.cues, "cue2")
	r.e = New(nil)
	r.e.Now = func() time.Time { return r.now }
	t.Cleanup(r.e.Close)
	r.e.Set(r.s)
	return r
}

// up brings looks up, in the show and so in the engine.
func (r *lookRig) up(looks ...string) {
	for _, id := range looks {
		r.s.Module(id).Value = 1
	}
	r.e.Set(r.s)
}

// into wires module from into the mixer's first input, alone.
func (r *lookRig) into(from string) {
	var keep []show.Wire
	for _, w := range r.s.Wires {
		if w.To.Module != r.mixer || w.To.Port != "in1" {
			keep = append(keep, w)
		}
	}
	keep = append(keep, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: from, Port: "out"}, To: show.End{Module: r.mixer, Port: "in1"}})
	r.s.Wires = keep
	r.e.Set(r.s)
}

// tick runs a tick a fortieth of a second on.
func (r *lookRig) tick() Live {
	r.now = r.now.Add(time.Second / 40)
	return r.e.Tick()
}

// TestALookComesUpWithItsLevel: off at 0, and its red full at 1.
func TestALookComesUpWithItsLevel(t *testing.T) {
	r := newLookRig(t)
	r.into(r.red)
	if live := r.tick(); live.Stage[0] != 0 {
		t.Errorf("a look at 0 plays red %d", live.Stage[0])
	}
	r.e.SetValue(r.red, 1)
	if live := r.tick(); live.Stage[0] != 255 || live.Owners[1] != r.red {
		t.Errorf("a look at 1 plays red %d, owned by %q", live.Stage[0], live.Owners[1])
	}
}

// TestACueListFadesFromCueToCueAndStopsAtItsEnds: GO fades over its
// second, Back cuts, and past the last cue GO does nothing.
func TestACueListFadesFromCueToCueAndStopsAtItsEnds(t *testing.T) {
	r := newLookRig(t)
	r.up(r.red, r.blue)
	r.into(r.cues)
	if live := r.tick(); live.Stage[0] != 0 || live.Steps[r.cues].At != -1 {
		t.Fatalf("before GO the cue list plays red %d, at %d", live.Stage[0], live.Steps[r.cues].At)
	}
	r.e.Step(r.cues, 1)
	for range 40 {
		r.tick()
	}
	live := r.tick()
	if live.Stage[0] != 255 || live.Steps[r.cues].At != 0 {
		t.Fatalf("a second after GO, red is %d at cue %d", live.Stage[0], live.Steps[r.cues].At)
	}
	r.e.Step(r.cues, 1)
	for range 20 {
		live = r.tick()
	}
	if live.Stage[0] < 100 || live.Stage[0] > 155 || live.Stage[2] < 100 || live.Stage[2] > 155 {
		t.Errorf("half way to cue 2, red is %d and blue %d", live.Stage[0], live.Stage[2])
	}
	r.e.Step(r.cues, 1)
	for range 40 {
		live = r.tick()
	}
	if live.Steps[r.cues].At != 1 || live.Stage[2] != 255 {
		t.Errorf("GO past the last cue left it at %d, blue %d", live.Steps[r.cues].At, live.Stage[2])
	}
	r.e.Step(r.cues, -1)
	if live = r.tick(); live.Stage[0] != 255 || live.Stage[2] != 0 {
		t.Errorf("Back cuts to red %d, blue %d", live.Stage[0], live.Stage[2])
	}
}

// TestAChaserStepsOnItsClock, every beat, round and round.
func TestAChaserStepsOnItsClock(t *testing.T) {
	r := newLookRig(t)
	r.up(r.red, r.blue)
	tp := r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: tp, Kind: show.Tempo, BPM: 120, Manual: true})
	r.s.Wires = append(r.s.Wires, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: tp, Port: "out"}, To: show.End{Module: r.chaser, Port: "clock"}})
	r.into(r.chaser)
	seen := map[int]int{}
	for range 80 {
		live := r.tick()
		seen[live.Steps[r.chaser].At]++
	}
	// Two seconds at 120 is four beats: two on each step.
	if seen[0] < 30 || seen[1] < 30 {
		t.Errorf("over four beats the chaser stood at %v", seen)
	}
}

// TestTheOverridesWinAndLetGo: flash brings the dome full white,
// blackout darkens it over the flash, and hold keeps what played.
func TestTheOverridesWinAndLetGo(t *testing.T) {
	r := newLookRig(t)
	r.up(r.red)
	r.into(r.red)
	r.e.Override(r.mixer, FlashAll, true)
	live := r.tick()
	if live.Stage[0] != 255 || live.Stage[1] != 255 || live.Stage[2] != 255 || live.Stage[3] != 255 || !live.Overrides[r.mixer][1] {
		t.Errorf("flashed, the dome plays %v", live.Stage[:4])
	}
	r.e.Override(r.mixer, Blackout, true)
	if live = r.tick(); live.Stage[0] != 0 || live.Stage[3] != 0 {
		t.Errorf("blacked out over a flash, the dome plays %v", live.Stage[:4])
	}
	r.e.Override(r.mixer, Blackout, false)
	r.e.Override(r.mixer, FlashAll, false)
	r.e.Override(r.mixer, HoldStill, true)
	r.tick()
	r.e.SetValue(r.red, 0)
	if live = r.tick(); live.Stage[0] != 255 {
		t.Errorf("held, the look taken down plays red %d", live.Stage[0])
	}
	r.e.Override(r.mixer, HoldStill, false)
	if live = r.tick(); live.Stage[0] != 0 {
		t.Errorf("let go, the look taken down plays red %d", live.Stage[0])
	}
}

// TestBlendFadesBetweenLayers, channels either leaves from or to 0.
func TestBlendFadesBetweenLayers(t *testing.T) {
	a, b, dst := mix.NewLayer(), mix.NewLayer(), mix.NewLayer()
	a.Set(1, 200)
	b.Set(2, 100)
	blend(dst, a, b, 0.25)
	if v, _ := dst.Get(1); v != 150 {
		t.Errorf("a quarter on, a's 200 is %d", v)
	}
	if v, _ := dst.Get(2); v != 25 {
		t.Errorf("a quarter on, b's 100 is %d", v)
	}
	if _, ok := dst.Get(3); ok {
		t.Error("a channel neither sets is set")
	}
}

// TestTheLogSaysWhatHappened: a button pressed is noted.
func TestTheLogSaysWhatHappened(t *testing.T) {
	r := newLookRig(t)
	b := r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: b, Kind: show.Button, Name: "Drop"})
	r.e.Set(r.s)
	r.e.Press(b)
	live := r.tick()
	if n := len(live.Log); n == 0 || live.Log[n-1].Text != "Drop pressed" {
		t.Errorf("the log says %v", live.Log)
	}
}
