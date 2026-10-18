package tempo

import (
	"math"
	"slices"
	"testing"
)

func testClock(over func(*Clock)) Clock {
	c := Clock{SourceID: "s", BPM: 120, RefTimeMs: 1000, Playing: true}
	if over != nil {
		over(&c)
	}
	return c
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

func TestMsPerBeat(t *testing.T) {
	if got := MsPerBeat(120); got != 500 {
		t.Errorf("MsPerBeat(120) = %v, want 500", got)
	}
	if got := MsPerBeat(0); !math.IsInf(got, 1) {
		t.Errorf("MsPerBeat(0) = %v, want +Inf", got)
	}
}

func TestBeatsAt(t *testing.T) {
	// 120 BPM is two beats a second, so 2000 ms past the reference is
	// four beats on.
	c := testClock(func(c *Clock) { c.BeatAtRef = 1 })
	if got := BeatsAt(c, 3000); !near(got, 5) {
		t.Errorf("BeatsAt = %v, want 5", got)
	}
	stopped := testClock(func(c *Clock) { c.Playing, c.BeatAtRef = false, 7 })
	if got := BeatsAt(stopped, 9999); got != 7 {
		t.Errorf("a stopped clock read %v, want 7", got)
	}
}

func TestPhase(t *testing.T) {
	cases := []struct{ beats, quantum, want float64 }{
		{4, 4, 0},
		{5.5, 4, 1.5},
		{-0.5, 4, 3.5},
	}
	for _, c := range cases {
		if got := Phase(c.beats, c.quantum); !near(got, c.want) {
			t.Errorf("Phase(%v, %v) = %v, want %v", c.beats, c.quantum, got, c.want)
		}
	}
}

func TestBeatsUntilBoundary(t *testing.T) {
	if got := BeatsUntilBoundary(8, 4); got != 0 {
		t.Errorf("on a boundary = %v, want 0", got)
	}
	if got := BeatsUntilBoundary(5, 4); !near(got, 3) {
		t.Errorf("BeatsUntilBoundary(5, 4) = %v, want 3", got)
	}
}

func TestRegistryHasAManualSource(t *testing.T) {
	r := NewRegistry()
	ids := func() []string {
		out := make([]string, 0, len(r.Sources()))
		for _, s := range r.Sources() {
			out = append(out, s.ID)
		}
		return out
	}
	if !slices.Contains(ids(), ManualSourceID) {
		t.Fatalf("sources = %v, want the manual one among them", ids())
	}
	c, ok := r.Active()
	if !ok || c.Playing {
		t.Errorf("the manual source starts as %+v, want it stopped", c)
	}
}

func TestRegistryAdvancesTheManualSource(t *testing.T) {
	r := NewRegistry()
	var at float64
	r.now = func() float64 { return at }
	r.SetManual(120, true) // two beats a second
	at = 1000
	got, ok := r.BeatsNow(at)
	if !ok || !near(got, 2) {
		t.Errorf("BeatsNow = %v, %v; want 2", got, ok)
	}
}

func TestRegistryPublishAndSelect(t *testing.T) {
	r := NewRegistry()
	r.Publish(Clock{SourceID: "link", BPM: 128, BeatAtRef: 10, RefTimeMs: 0}, "Ableton Link")
	r.SetPriority([]string{"link"})
	if got := r.Selected(); got != "link" {
		t.Errorf("selected = %q, want link", got)
	}
	// The published clock is stopped, so it holds its beat.
	if got, _ := r.BeatsNow(9999); got != 10 {
		t.Errorf("BeatsNow = %v, want 10", got)
	}
	i := slices.IndexFunc(r.Sources(), func(s Source) bool { return s.ID == "link" })
	if i < 0 || r.Sources()[i].Label != "Ableton Link" {
		t.Errorf("sources = %+v", r.Sources())
	}
}

func TestRegistryFallsBackToManual(t *testing.T) {
	r := NewRegistry()
	r.Publish(Clock{SourceID: "deck0", BPM: 100, Playing: true}, "")
	r.SetPriority([]string{"deck0"})
	r.Remove("deck0")
	if got := r.Selected(); got != ManualSourceID {
		t.Errorf("selected = %q after the source went, want the manual one", got)
	}
}

func TestRegistryNotifiesListeners(t *testing.T) {
	r := NewRegistry()
	called := 0
	r.Subscribe(func() { called++ })
	r.SetManual(140, true)
	if called == 0 {
		t.Error("the listener was not called")
	}
}

// A source nobody ranked still drives where nothing above it is there,
// so a device plugged into a rig whose ranking was never edited works.
func TestAnUnrankedSourceStillDrives(t *testing.T) {
	r := NewRegistry()
	r.Publish(Clock{SourceID: "midi-clock", BPM: 128, Playing: true}, "MIDI clock")
	if got := r.Selected(); got != "midi-clock" {
		t.Errorf("selected = %q, want the source that turned up", got)
	}
}

// The ranking decides between two that are there, and a source going
// away falls to the next one down rather than all the way to manual.
func TestTheRankingDecidesAndFallsBack(t *testing.T) {
	r := NewRegistry()
	r.SetPriority([]string{"link", "midi-clock"})
	r.Publish(Clock{SourceID: "midi-clock", BPM: 128, Playing: true}, "MIDI clock")
	if got := r.Selected(); got != "midi-clock" {
		t.Fatalf("selected = %q with only the clock there, want it", got)
	}
	// Link outranks it, so it takes over the moment it appears.
	r.Publish(Clock{SourceID: "link", BPM: 120, Playing: true}, "Ableton Link")
	if got := r.Selected(); got != "link" {
		t.Errorf("selected = %q once Link appeared, want link", got)
	}
	// And going away hands it back rather than dropping to manual.
	r.Remove("link")
	if got := r.Selected(); got != "midi-clock" {
		t.Errorf("selected = %q once Link went, want the clock", got)
	}
	r.Remove("midi-clock")
	if got := r.Selected(); got != ManualSourceID {
		t.Errorf("selected = %q with nothing left, want the manual one", got)
	}
}

// Every source known is listed in the order they are tried, with those
// that are there marked.
func TestSourcesAreListedInRankOrder(t *testing.T) {
	r := NewRegistry()
	r.Publish(Clock{SourceID: "song", BPM: 124}, "Song")
	r.SetPriority([]string{"link", "song"})
	ids := make([]string, 0, len(r.Sources()))
	connected := map[string]bool{}
	for _, s := range r.Sources() {
		ids = append(ids, s.ID)
		connected[s.ID] = s.Connected
	}
	want := []string{"link", "song", ManualSourceID}
	if !slices.Equal(ids, want) {
		t.Errorf("sources = %v, want %v", ids, want)
	}
	if connected["link"] {
		t.Error("a source nothing has published to reads as connected")
	}
	if !connected["song"] || !connected[ManualSourceID] {
		t.Error("a registered source reads as not connected")
	}
}
