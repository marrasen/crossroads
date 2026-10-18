package engine

import (
	"testing"
	"time"

	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// TestACountdownReachesZeroOnceAndRunsIntoOvertime: started, it counts
// down; paused, it holds; at zero it fires done once and goes on below
// zero; reset, it starts over.
func TestACountdownReachesZeroOnceAndRunsIntoOvertime(t *testing.T) {
	var s show.Show
	id := s.NewID("m")
	s.Modules = []show.Module{{ID: id, Kind: show.Timer, Name: "Set", Seconds: 2}}
	now := time.Unix(1000, 0)
	e := New(nil)
	e.Now = func() time.Time { return now }
	defer e.Close()
	e.Set(s)
	tick := func(d time.Duration) (Live, bool) {
		now = now.Add(d)
		live := e.Tick()
		done, _ := e.values[portKey{id, "done"}].(bool)
		return live, done
	}
	e.TimerDo(id, TimerStart)
	tick(0)
	live, _ := tick(time.Second)
	if st := live.Timers[id]; st.Shown != time.Second || !st.Running || live.Values[id] != 0.5 {
		t.Errorf("a second into two, the timer shows %+v, left %v", st, live.Values[id])
	}
	e.TimerDo(id, TimerPause)
	tick(0)
	if live, _ = tick(5 * time.Second); live.Timers[id].Shown != time.Second {
		t.Errorf("paused, the timer moved to %v", live.Timers[id].Shown)
	}
	e.TimerDo(id, TimerStart)
	tick(0)
	var fired int
	for range 30 {
		if _, done := tick(100 * time.Millisecond); done {
			fired++
		}
	}
	live, _ = tick(0)
	if st := live.Timers[id]; fired != 1 || !st.Finished || st.Shown != -2*time.Second {
		t.Errorf("past zero the timer fired %d times and shows %+v", fired, st)
	}
	e.TimerDo(id, TimerReset)
	if live, _ = tick(0); live.Timers[id].Shown != 2*time.Second || live.Timers[id].Finished {
		t.Errorf("reset, running on, the timer shows %+v", live.Timers[id])
	}
}

// TestAPatternPlaysLockedToTheBeatWhileHeld: red on its third beat,
// whatever beat it was held from, and its lights given back as it is
// let go.
func TestAPatternPlaysLockedToTheBeatWhileHeld(t *testing.T) {
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
	s.Rig.Groups = []fixtures.Group{{ID: "all", MemberIDs: []string{s.Rig.Lights[0].ID}}}
	s.Patterns = []pattern.Pattern{{ID: "p", Name: "Red on three", Bars: 1, Release: pattern.Release{Rest: true},
		Lanes: []pattern.Lane{{ID: "l", GroupID: "all", Clips: []pattern.Clip{
			{ID: "c", Start: 2, Length: 1, Kind: pattern.KindColour, Colour: pattern.Colour{R: 255}},
		}}}}}
	tp, pt := s.NewID("m"), s.NewID("m")
	s.Modules = append(s.Modules, show.Module{ID: tp, Kind: show.Tempo, BPM: 60, Manual: true}, show.Module{ID: pt, Kind: show.Pattern, Pattern: "p"})
	s.Wires = append(s.Wires,
		show.Wire{ID: s.NewID("w"), From: show.End{Module: tp, Port: "out"}, To: show.End{Module: pt, Port: "clock"}},
		show.Wire{ID: s.NewID("w"), From: show.End{Module: pt, Port: "out"}, To: show.End{Module: mixer, Port: "in1"}})
	now := time.Unix(1000, 0)
	e := New(nil)
	e.Now = func() time.Time { return now }
	defer e.Close()
	e.Set(s)
	// Held from beat 1.5, a beat a second.
	for range 15 {
		now = now.Add(100 * time.Millisecond)
		e.Tick()
	}
	e.Override(pt, "play", true)
	red := map[int]byte{}
	for i := range 40 {
		now = now.Add(100 * time.Millisecond)
		live := e.Tick()
		red[i] = live.Stage[0]
	}
	// Beats 2 to 3 of each four light red: from half a second after the
	// hold for a second, and again four beats on.
	if red[2] != 0 || red[5] != 255 || red[12] != 255 || red[16] != 0 || red[36] != 0 || red[38] != 0 {
		t.Errorf("held, the red over four seconds is %v", red)
	}
	e.Override(pt, "play", false)
	now = now.Add(2700 * time.Millisecond)
	if live := e.Tick(); live.Stage[0] != 0 {
		t.Errorf("let go on its third beat, the pattern still plays red %d", live.Stage[0])
	}
}

// TestAPatternAuditionedPlaysOnTheStage, over all else, until stopped.
func TestAPatternAuditionedPlaysOnTheStage(t *testing.T) {
	s := show.Start()
	light := s.Rig.Lights[0].ID
	p := pattern.Pattern{ID: "p", Bars: 1, Lanes: []pattern.Lane{{ID: "l", FixtureID: light, Clips: []pattern.Clip{
		{ID: "c", Start: 0, Length: 4, Kind: pattern.KindColour, Colour: pattern.Colour{G: 255}}}}}}
	e := New(nil)
	defer e.Close()
	e.Set(s)
	e.Audition(&p)
	if live := e.Tick(); live.Stage[1] != 255 {
		t.Errorf("auditioned, the pattern plays green %d", live.Stage[1])
	}
	e.Audition(nil)
	if live := e.Tick(); live.Stage[1] != 0 {
		t.Errorf("stopped, the pattern still plays green %d", live.Stage[1])
	}
}
