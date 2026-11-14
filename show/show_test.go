package show

import (
	"math"
	"path/filepath"
	"reflect"
	"slices"
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

// TestABoardsScenesComeBackAsTheyWereSaved, and Mend keeps one scene a
// pad, on the pads there are, of the lights the rig has.
func TestABoardsScenesComeBackAsTheyWereSaved(t *testing.T) {
	s := Start()
	dome := s.Rig.Lights[0].ID
	var b *Module
	for i := range s.Modules {
		if s.Modules[i].Kind == Board {
			b = &s.Modules[i]
		}
	}
	b.SceneFade = 2.5
	b.SetScene(Scene{Pad: 9, Name: "Blue", Values: map[string]map[int]byte{dome: {3: 255, 7: 0}}})
	b.SetScene(Scene{Pad: 1, Name: "Red", Values: map[string]map[int]byte{dome: {1: 200}}})
	path := filepath.Join(t.TempDir(), "Gig.crossroads")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Modules, s.Modules) {
		t.Fatalf("the scenes came back as %+v", back.Module(b.ID).Scenes)
	}
	if got := back.Module(b.ID).Scenes; got[0].Name != "Red" || got[1].Name != "Blue" {
		t.Errorf("the scenes are not in their pads' order: %+v", got)
	}
	m := back.Module(b.ID)
	m.Scenes = append(m.Scenes, Scene{Pad: 1, Name: "Again"}, Scene{Pad: Pads, Name: "Off the pads"},
		Scene{Pad: 0, Name: "Gone", Values: map[string]map[int]byte{"nowhere": {1: 1}, dome: {2: 2}}})
	back.Mend()
	got := back.Module(b.ID).Scenes
	if len(got) != 3 || got[0].Name != "Gone" || got[1].Name != "Red" || len(got[0].Values) != 1 {
		t.Errorf("mended, the scenes are %+v", got)
	}
}

// TestASceneWhoseLastLightIsRemovedLeavesItsPad: a scene holding
// nothing is no scene.
func TestASceneWhoseLastLightIsRemovedLeavesItsPad(t *testing.T) {
	s := Start()
	dome := s.Rig.Lights[0].ID
	var b *Module
	for i := range s.Modules {
		if s.Modules[i].Kind == Board {
			b = &s.Modules[i]
		}
	}
	b.SetScene(Scene{Pad: 3, Name: "Red", Values: map[string]map[int]byte{dome: {1: 255}}})
	before := s.Clone()
	s.RemoveLight(dome)
	if b.Scene(3) != nil {
		t.Errorf("its only light removed, pad 3 still has %+v", *b.Scene(3))
	}
	for _, m := range before.Modules {
		if m.Kind == Board && (m.Scene(3) == nil || len(m.Scene(3).Values) != 1) {
			t.Error("removing the light changed the show it was cloned from")
		}
	}
}

// TestABoardsChasesComeBackAsTheyWereSaved, and Mend keeps the steps on
// the pads there are, an ID to each chase, and no chase of no steps; a
// clone's chases are its own.
func TestABoardsChasesComeBackAsTheyWereSaved(t *testing.T) {
	s := Start()
	var b *Module
	for i := range s.Modules {
		if s.Modules[i].Kind == Board {
			b = &s.Modules[i]
		}
	}
	b.Chases = []Chase{{ID: 2, Name: "Round", Steps: []int{0, 3, 3, 15}}, {ID: 5, Name: "Back", Steps: []int{1}}}
	b.Speed, b.Fade, b.FollowBeat = 96, 0.4, true
	path := filepath.Join(t.TempDir(), "Gig.crossroads")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Modules, s.Modules) {
		t.Fatalf("the chases came back as %+v", back.Module(b.ID))
	}
	if b.NextChase() != 6 {
		t.Errorf("the next chase is %d", b.NextChase())
	}
	c := back.Clone()
	c.Module(b.ID).Chases[0].Steps[0] = 9
	if back.Module(b.ID).Chases[0].Steps[0] != 0 {
		t.Error("a clone's chase changed the show it was cloned from")
	}
	m := back.Module(b.ID)
	m.Chases = append(m.Chases, Chase{ID: 2, Name: "Twin", Steps: []int{2}}, Chase{Name: "No ID", Steps: []int{-1, 4, Pads}},
		Chase{ID: 7, Name: "Off the pads", Steps: []int{Pads}})
	back.Mend()
	got := back.Module(b.ID).Chases
	if len(got) != 4 || got[2].Name != "Twin" || got[2].ID != 8 || got[3].ID != 9 || !slices.Equal(got[3].Steps, []int{4}) {
		t.Errorf("mended, the chases are %+v", got)
	}
}

// TestTheControllerABoardIsPlayedFromComesBack, and Mend has a Board
// played from a module not there, or no Controller, played from none;
// so does the controller removed.
func TestTheControllerABoardIsPlayedFromComesBack(t *testing.T) {
	s := Start()
	ctl := s.NewID("m")
	s.Modules = append(s.Modules, Module{ID: ctl, Kind: Controller, Name: "X-Touch Mini", Layout: "xtouch-mini"})
	var b *Module
	for i := range s.Modules {
		if s.Modules[i].Kind == Board {
			b = &s.Modules[i]
		}
	}
	b.PlayedFrom = ctl
	path := filepath.Join(t.TempDir(), "Gig.crossroads")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Module(b.ID).PlayedFrom != ctl {
		t.Fatalf("the board came back played from %q", back.Module(b.ID).PlayedFrom)
	}
	for _, from := range []string{"gone", back.Modules[0].ID} {
		c := back.Clone()
		c.Module(b.ID).PlayedFrom = from
		c.Mend()
		if got := c.Module(b.ID).PlayedFrom; got != "" {
			t.Errorf("played from %q, mended, the board is played from %q", from, got)
		}
	}
	back.RemoveModule(ctl)
	if got := back.Module(b.ID).PlayedFrom; got != "" {
		t.Errorf("its controller removed, the board is played from %q", got)
	}
}
