package app

import (
	"errors"
	"testing"
	"time"

	"github.com/marrasen/crossroads/engine"
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
