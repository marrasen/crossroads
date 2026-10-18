package show

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
)

// TestAShowComesBackAsItWasSaved, and one written badly is mended.
func TestAShowComesBackAsItWasSaved(t *testing.T) {
	s := Start()
	path := filepath.Join(t.TempDir(), "Gig.crossroads")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Modules, s.Modules) || !reflect.DeepEqual(back.Wires, s.Wires) ||
		!reflect.DeepEqual(back.Rig, s.Rig) || !reflect.DeepEqual(back.Stage, s.Stage) || back.Next != s.Next {
		t.Errorf("came back different:\n%+v\nwant\n%+v", back, s)
	}
	if len(back.Layout.Items()) != len(Tabs) {
		t.Errorf("the layout came back with %v", back.Layout.Items())
	}
}

// TestRemovingTakesWhatHangsOnIt: a module's wires go with it, and a
// light leaves the stage and the modules that played it.
func TestRemovingTakesWhatHangsOnIt(t *testing.T) {
	s := Start()
	dome := s.Rig.Lights[0].ID
	var lights string
	for _, m := range s.Modules {
		if m.Kind == Lights {
			lights = m.ID
		}
	}
	s.RemoveModule(lights)
	for _, w := range s.Wires {
		if w.From.Module == lights || w.To.Module == lights {
			t.Fatalf("a wire of the module stayed: %+v", w)
		}
	}
	s.RemoveLight(dome)
	if s.Light(dome) != nil || len(s.Stage.Spots) != 0 {
		t.Error("the light stayed in the rig or on the stage")
	}
}

// TestASongsClockFollowsItsBeats: counted from its first bar, between
// the beats found and on past them; a tempo set runs steady from the
// first bar.
func TestASongsClockFollowsItsBeats(t *testing.T) {
	g := Song{Scan: &Scan{BPM: 120, Beats: []float64{0.2, 0.7, 1.2, 1.7, 2.2, 2.8}, Downbeat: 1, PerBar: 4}}
	for _, c := range []struct{ t, want float64 }{
		{0.7, 0}, {0.95, 0.5}, {2.5, 3.5}, {3.4, 5}, {0.45, -0.5},
	} {
		if _, got := g.Clock(c.t); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("at %v the count is %v, want %v", c.t, got, c.want)
		}
	}
	g.BPM = 60
	if bpm, got := g.Clock(2.7); bpm != 60 || math.Abs(got-2) > 1e-9 {
		t.Errorf("set to 60, at 2.7 s the clock is %v at %v, want 2 at 60", got, bpm)
	}
	if bpm, _ := (&Song{}).Clock(5); bpm != 0 {
		t.Errorf("a song not scanned has a tempo of %v", bpm)
	}
}

// TestASongIsBroughtToTheTargetUnderTheCeiling: a quiet song is raised,
// no further than its true peak allows, and its own gain goes on top.
func TestASongIsBroughtToTheTargetUnderTheCeiling(t *testing.T) {
	g := Song{Scan: &Scan{LUFS: -20, Loud: true, TruePeak: -10}, Gain: 1.5}
	if got := g.Loudness(); math.Abs(got-7.5) > 1e-9 {
		t.Errorf("a song at -20 LUFS plays %+v dB, want +7.5", got)
	}
	g.Scan.TruePeak = -4
	if got := g.Loudness(); math.Abs(got-4.5) > 1e-9 {
		t.Errorf("held by its peak at -4, it plays %+v dB, want +4.5", got)
	}
	if got := (&Song{Gain: -2}).Loudness(); got != -2 {
		t.Errorf("a song not scanned plays %+v dB, want its own -2", got)
	}
}

// TestMendPutsEveryTabSomewhere: a show whose layout lost a tab gets it
// back.
func TestMendPutsEveryTabSomewhere(t *testing.T) {
	s := Start()
	s.Layout.Remove(TabPlayer)
	s.Mend()
	if _, ok := s.Layout.Where(TabPlayer); !ok {
		t.Error("the Player tab is nowhere")
	}
}
