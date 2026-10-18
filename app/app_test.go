package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget/dock"
	"github.com/marrasen/gunim/widget/graph"
)

// named returns the module of a's show named name.
func named(t *testing.T, a *App, name string) *show.Module {
	t.Helper()
	for i := range a.st.Show.Modules {
		if a.st.Show.Modules[i].Name == name {
			return &a.st.Show.Modules[i]
		}
	}
	t.Fatalf("no module %s", name)
	return nil
}

// TestAFaderMovedPlaysAtOnceAndIsKeptWhenLetGo: the engine plays it
// while it moves, and the show keeps it once let go.
func TestAFaderMovedPlaysAtOnceAndIsKeptWhenLetGo(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	green := named(t, a, "Green").ID
	a.Handle(0, SetValue{Module: green, Value: 1})
	if live := a.eng.Tick(); live.Stage[1] != 255 {
		t.Errorf("green moved plays %d", live.Stage[1])
	}
	if named(t, a, "Green").Value != 0 || a.st.Unsaved {
		t.Error("a fader still moving changed the show")
	}
	a.Handle(0, SetValue{Module: green, Value: 1, Done: true})
	if named(t, a, "Green").Value != 1 || !a.st.Unsaved {
		t.Error("a fader let go was not kept")
	}
}

// TestALightAddedTakesTheNextAddressAndJoinsTheLights: it hangs in the
// middle of the stage, played by the first Lights module.
func TestALightAddedTakesTheNextAddressAndJoinsTheLights(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.Handle(0, AddLight{Def: "fun-generation-led-diamond-dome-mk2"})
	s := a.st.Show
	if len(s.Rig.Lights) != 2 {
		t.Fatalf("%d lights", len(s.Rig.Lights))
	}
	l := s.Rig.Lights[1]
	if l.StartAddress != 10 {
		t.Errorf("the second dome starts at %d, want 10", l.StartAddress)
	}
	if _, ok := s.Stage.Spots[l.ID]; !ok {
		t.Error("the light hangs nowhere")
	}
	if dome := named(t, a, "Dome"); len(dome.Lights) != 2 {
		t.Errorf("the Lights module plays %v", dome.Lights)
	}
	a.Handle(0, RemoveLight{Light: l.ID})
	if len(a.st.Show.Rig.Lights) != 1 || len(named(t, a, "Dome").Lights) != 1 {
		t.Error("the light removed stayed somewhere")
	}
}

// TestTheRoutesChangeTheShow: a box moved, a wire refused for a fed
// input, a wire joined, replugged and taken out, and a module deleted
// with its wires.
func TestTheRoutesChangeTheShow(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	red, dome := named(t, a, "Red").ID, named(t, a, "Dome").ID
	a.Handle(0, graph.Move{To: map[string]geom.Point{red: geom.Pt(-40, -40)}})
	if named(t, a, "Red").At != geom.Pt(-40, -40) {
		t.Error("the box did not move")
	}
	wires := len(a.st.Show.Wires)
	a.Handle(0, graph.Connect{From: graph.End{Node: red, Port: "out"}, To: graph.End{Node: dome, Port: "blue"}})
	if len(a.st.Show.Wires) != wires {
		t.Error("a second wire into a fed input was made")
	}
	a.Handle(0, graph.Connect{From: graph.End{Node: red, Port: "out"}, To: graph.End{Node: dome, Port: "strobe"}})
	if len(a.st.Show.Wires) != wires+1 {
		t.Fatal("the wire was not made")
	}
	w := a.st.Show.Wires[len(a.st.Show.Wires)-1]
	a.Handle(0, graph.Replug{Wire: w.ID, From: graph.End{Node: red, Port: "out"}, To: graph.End{Node: dome, Port: "speed"}})
	if got := a.st.Show.Wires[len(a.st.Show.Wires)-1].To.Port; got != "speed" {
		t.Errorf("replugged to %s", got)
	}
	a.Handle(0, graph.Delete{Nodes: []string{dome}})
	for _, w := range a.st.Show.Wires {
		if w.To.Module == dome || w.From.Module == dome {
			t.Fatalf("a wire of the module deleted stayed: %+v", w)
		}
	}
}

// TestATabMovesInTheLayout: and the layout stays whole.
func TestATabMovesInTheLayout(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.Handle(0, dock.Move{ID: show.TabFaders, To: dock.Target{At: show.TabRoutes, Index: -1}})
	if g := a.st.Show.Layout.GroupOf(show.TabFaders); g == nil || g != a.st.Show.Layout.GroupOf(show.TabRoutes) {
		t.Error("the Faders tab did not join the Routes")
	}
}

// TestAFaderHeldPlaysOnAsTheShowChanges: and a light's colours gone,
// their wires go too.
func TestAFaderHeldPlaysOnAsTheShowChanges(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	green := named(t, a, "Green").ID
	a.Handle(0, SetValue{Module: green, Value: 1})
	a.Handle(0, graph.Move{To: map[string]geom.Point{green: geom.Pt(5, 5)}})
	if live := a.eng.Tick(); live.Stage[1] != 255 {
		t.Errorf("another edit put the held fader back: green plays %d", live.Stage[1])
	}
	a.Handle(0, RemoveLight{Light: a.st.Show.Rig.Lights[0].ID})
	dome := named(t, a, "Dome").ID
	for _, w := range a.st.Show.Wires {
		if w.To.Module == dome {
			t.Fatalf("a wire into a colour no light has stayed: %+v", w)
		}
	}
}

// TestAWiresOutputEndMoves: a replug may move either end.
func TestAWiresOutputEndMoves(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	red, green, dome := named(t, a, "Red").ID, named(t, a, "Green").ID, named(t, a, "Dome").ID
	var id string
	for _, w := range a.st.Show.Wires {
		if w.From.Module == red {
			id = w.ID
		}
	}
	a.Handle(0, graph.Delete{Wires: []string{"x"}})
	for _, w := range a.st.Show.Wires {
		if w.From.Module == green {
			a.Handle(0, graph.Delete{Wires: []string{w.ID}})
		}
	}
	a.Handle(0, graph.Replug{Wire: id, From: graph.End{Node: green, Port: "out"}, To: graph.End{Node: dome, Port: "red"}})
	for _, w := range a.st.Show.Wires {
		if w.ID == id && w.From.Module != green {
			t.Errorf("the wire still runs from %s", w.From.Module)
		}
	}
}

// TestTapsSetATemposTempo: a first tap changes nothing kept; taps a
// steady gap apart set its tempo and hold it there, which the follow
// box lets go of again.
func TestTapsSetATemposTempo(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.Handle(0, AddModule{Kind: show.Tempo})
	m := named(t, a, "Tempo")
	if m.BPM != 120 || m.Manual {
		t.Fatalf("a new Tempo plays %v, manual %v", m.BPM, m.Manual)
	}
	id := m.ID
	now := time.Unix(1000, 0)
	a.eng.Now = func() time.Time { return now }
	a.st.Unsaved = false
	a.Handle(0, Tap{Module: id})
	if a.st.Unsaved {
		t.Error("a first tap changed the show")
	}
	now = now.Add(600 * time.Millisecond)
	a.Handle(0, Tap{Module: id})
	if m := named(t, a, "Tempo"); m.BPM != 100 || !m.Manual {
		t.Errorf("taps 0.6 s apart set %v, manual %v; want 100, manual", m.BPM, m.Manual)
	}
	a.Handle(0, SetTempo{Module: id, Manual: false})
	if m := named(t, a, "Tempo"); m.BPM != 100 || m.Manual {
		t.Errorf("let go, the Tempo plays %v, manual %v", m.BPM, m.Manual)
	}
	a.Handle(0, SetTempo{Module: id, BPM: 999, Manual: true})
	if m := named(t, a, "Tempo"); m.BPM != MaxBPM {
		t.Errorf("typed 999, the Tempo plays %v", m.BPM)
	}
}

// TestAPulseAndAnAudioInKeepTheirSettings: what a Pulse flashes every,
// and the input an Audio in hears, are kept; a length of no beats is
// not.
func TestAPulseAndAnAudioInKeepTheirSettings(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.eng.Listen = func(string) (engine.Listener, error) { return nil, errors.New("no sound card in a test") }
	a.Handle(0, AddModule{Kind: show.Pulse})
	a.Handle(0, AddModule{Kind: show.AudioIn})
	p, in := named(t, a, "Pulse").ID, named(t, a, "Audio in").ID
	if named(t, a, "Pulse").Every != 1 {
		t.Errorf("a new Pulse flashes every %v beats", named(t, a, "Pulse").Every)
	}
	a.Handle(0, SetEvery{Module: p, Every: 0.5})
	a.Handle(0, SetEvery{Module: p, Every: -1})
	a.Handle(0, SetInput{Module: in, Input: "mic"})
	if named(t, a, "Pulse").Every != 0.5 || named(t, a, "Audio in").Input != "mic" {
		t.Errorf("the Pulse flashes every %v, the Audio in hears %q", named(t, a, "Pulse").Every, named(t, a, "Audio in").Input)
	}
}

// TestSongsAddedAreScannedAndKeptInOrder: only songs the player plays
// are added, each is scanned, and the playlist moves and loses songs
// as asked.
func TestSongsAddedAreScannedAndKeptInOrder(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.Scan = func(path string) (show.Scan, error) {
		if path == "bad.mp3" {
			return show.Scan{}, errors.New("not an MP3")
		}
		return show.Scan{Length: 60, BPM: 128}, nil
	}
	a.Handle(0, AddSongPaths{Paths: []string{"a.wav", "notes.txt", "bad.mp3", "c.flac"}})
	songs := a.st.Show.Songs
	if len(songs) != 3 || songs[0].Title != "a" || songs[2].Title != "c" {
		t.Fatalf("the playlist is %+v", songs)
	}
	for _, g := range songs {
		if a.st.Songs[g.ID] != Scanning {
			t.Errorf("%s says %q, not that it is scanned", g.Title, a.st.Songs[g.ID])
		}
	}
	for range 3 {
		a.scanned((<-a.in).(scanned))
	}
	a0, bad, c := a.st.Show.Songs[0], a.st.Show.Songs[1], a.st.Show.Songs[2]
	if a0.Scan == nil || a0.Scan.BPM != 128 || a.st.Songs[a0.ID] != "" {
		t.Errorf("a scanned is %+v, saying %q", a0.Scan, a.st.Songs[a0.ID])
	}
	if bad.Scan != nil || a.st.Songs[bad.ID] != "not an MP3" {
		t.Errorf("a song that will not scan says %q", a.st.Songs[bad.ID])
	}
	a.Handle(0, MoveSong{Song: c.ID, Before: a0.ID})
	a.Handle(0, RemoveSong{Song: bad.ID})
	got := make([]string, 0, len(a.st.Show.Songs))
	for _, g := range a.st.Show.Songs {
		got = append(got, g.Title)
	}
	if len(got) != 2 || got[0] != "c" || got[1] != "a" {
		t.Errorf("moved and taken out, the playlist is %v", got)
	}
	a.Handle(0, AddSongPaths{Paths: []string{"b.ogg"}, Before: a0.ID})
	if g := a.st.Show.Songs[1]; g.Title != "b" {
		t.Errorf("a song added before a is %s", g.Title)
	}
}

// TestASongsTempoAndGainAreKeptWithinBounds: a tempo to a tenth of a
// beat a minute, a gain within MaxGain, and a tempo of 0 sets none.
func TestASongsTempoAndGainAreKeptWithinBounds(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.Scan = func(string) (show.Scan, error) { return show.Scan{}, nil }
	a.Handle(0, AddSongPaths{Paths: []string{"a.wav"}})
	id := a.st.Show.Songs[0].ID
	a.Handle(0, SetSongBPM{Song: id, BPM: 127.04})
	a.Handle(0, SetSongGain{Song: id, Gain: 40})
	if g := a.st.Show.Song(id); g.BPM != 127 || g.Gain != MaxGain {
		t.Errorf("set to 127.04 BPM and 40 dB, the song plays %v BPM and %v dB", g.BPM, g.Gain)
	}
	a.Handle(0, SetSongBPM{Song: id, BPM: 0})
	if g := a.st.Show.Song(id); g.BPM != 0 {
		t.Errorf("set back to its scan, the song's tempo is %v", g.BPM)
	}
}

// TestAControlLearnedJoinsItsController with an ID of its own, and is
// forgotten as asked.
func TestAControlLearnedJoinsItsController(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.Handle(0, AddModule{Kind: show.Controller})
	id := named(t, a, "Controller").ID
	a.Handle(0, SetLayout{Module: id, Layout: ""})
	a.Handle(0, SetDevice{Module: id, Device: "nanoKONTROL"})
	a.st.Show.Module(id).Controls = []show.Control{{ID: "c1"}}
	a.eng.Set(a.st.Show)
	a.eng.Learn(id, true)
	learnt := []engine.Learnt{{Module: id, Control: show.Control{ID: "c1", Name: "Note 36", Kind: show.Press, CC: -1, Note: 36}}}
	a.learntFrom(learnt)
	m := named(t, a, "Controller")
	if m.Device != "nanoKONTROL" || m.Layout != "" || len(m.Controls) != 2 || m.Controls[1].ID == "c1" {
		t.Fatalf("the controller is %+v", m)
	}
	a.Handle(0, RemoveControl{Module: id, Control: "c1"})
	if m := named(t, a, "Controller"); len(m.Controls) != 1 || m.Controls[0].Name != "Note 36" {
		t.Errorf("having forgotten c1, the controls are %+v", m.Controls)
	}
}

// TestALookIsAddedWarmAndItsLevelsChange: a new look holds a warm
// white on every light, at 0; its levels change and go, and a fade is
// kept within its bounds.
func TestALookIsAddedWarmAndItsLevelsChange(t *testing.T) {
	a := NewApp(show.Start(), "", nil)
	a.Handle(0, AddModule{Kind: show.Look})
	m := named(t, a, "Look")
	if len(m.Lights) != 1 || m.Levels["dimmer"] != 1 || m.Value != 0 {
		t.Fatalf("a new look is %+v", m)
	}
	a.Handle(0, SetLevel{Module: m.ID, Role: "uv", Level: 2})
	a.Handle(0, SetLevel{Module: m.ID, Role: "blue", Level: -1})
	if m := named(t, a, "Look"); m.Levels["uv"] != 1 {
		t.Errorf("uv set to 2 is %v", m.Levels["uv"])
	} else if _, ok := m.Levels["blue"]; ok {
		t.Error("blue let go is held still")
	}
	a.Handle(0, AddModule{Kind: show.Chaser})
	c := named(t, a, "Chaser")
	a.Handle(0, SetFade{Module: c.ID, Fade: 3})
	if c := named(t, a, "Chaser"); c.Fade != 1 {
		t.Errorf("a chaser's fade set to 3 is %v", c.Fade)
	}
}

// TestATimerAndAPatternKeepTheirSettings: a new timer counts down five
// minutes, set as asked; a new pattern plays the show's first.
func TestATimerAndAPatternKeepTheirSettings(t *testing.T) {
	s := show.Start()
	s.Patterns = []pattern.Pattern{{ID: "p1", Name: "Chase"}, {ID: "p2", Name: "Wash"}}
	a := NewApp(s, "", nil)
	a.Handle(0, AddModule{Kind: show.Timer})
	a.Handle(0, AddModule{Kind: show.Pattern})
	tm, pt := named(t, a, "Timer"), named(t, a, "Pattern")
	if tm.Seconds != 300 || tm.Stopwatch || pt.Pattern != "p1" {
		t.Fatalf("a new timer is %+v and pattern %+v", tm, pt)
	}
	a.Handle(0, SetTimer{Module: tm.ID, Seconds: -5, Stopwatch: true})
	a.Handle(0, SetPattern{Module: pt.ID, Pattern: "p2"})
	if tm, pt := named(t, a, "Timer"), named(t, a, "Pattern"); tm.Seconds != 0 || !tm.Stopwatch || pt.Pattern != "p2" {
		t.Errorf("set, the timer is %+v and pattern %+v", tm, pt)
	}
}

// TestARigIsImportedAndToldOfOnce: a midittt rig opens rebuilt, unsaved,
// with what plays differently to tell; told, that goes; a rig that
// cannot be read says why and leaves the show alone.
func TestARigIsImportedAndToldOfOnce(t *testing.T) {
	dir := t.TempDir()
	rig := filepath.Join(dir, "Friday.json")
	// One route on a song cue, which crossroads does not hear; the tempo
	// tapped by hand, so nothing goes out on the network.
	if err := os.WriteFile(rig, []byte(`{"app":"midittt","version":7,"tempoPriority":["manual"],
		"timers":[{"id":"t","name":"Break","mode":"timer","durationMs":1000}],"fixtures":[],
		"fixtureGroups":[],"fxPrograms":[],"routes":[{"id":"r","name":"Drop","enabled":true,
		"input":{"deviceName":"","channel":"any","type":"noteon","data1":"any"},"cue":{"song":"a.mp3","bar":9,"beat":1},
		"output":{"kind":"timer","timerId":"t","command":"start"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewApp(show.Start(), "", nil)
	a.Handle(0, ImportPath{Path: rig})
	if a.st.Show.Name != "Friday" || !a.st.Unsaved || a.st.Imported != "Friday" || len(a.st.Notes) != 1 {
		t.Fatalf("imported, the state is %q, unsaved %v, %q, %v", a.st.Show.Name, a.st.Unsaved, a.st.Imported, a.st.Notes)
	}
	a.Handle(0, Told{})
	if a.st.Imported != "" || a.st.Notes != nil {
		t.Error("told, the import is still to tell")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"app":"other"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Handle(0, ImportPath{Path: bad})
	if a.st.Show.Name != "Friday" || len(a.st.Notes) != 1 || !strings.Contains(a.st.Notes[0], "could not be read") {
		t.Errorf("a bad rig left the show %q, saying %v", a.st.Show.Name, a.st.Notes)
	}
}
