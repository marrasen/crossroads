package app

import (
	"testing"

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
