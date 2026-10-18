package show

import (
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget/dock"
)

// The tabs, by the names the layout keeps them under.
const (
	TabRoutes = "routes"
	TabStage  = "stage"
	TabFaders = "faders"
	TabRig    = "rig"
	TabPlayer = "player"
	TabMIDI   = "controllers"
	TabLive   = "live"
	TabWatch  = "monitor"
)

// Tabs are the tabs, in the order a new window holds them.
var Tabs = []string{TabRoutes, TabStage, TabFaders, TabRig, TabPlayer, TabMIDI, TabLive, TabWatch}

// TabTitle is what tab t's tab says.
func TabTitle(t string) string {
	switch t {
	case TabRoutes:
		return "Routes"
	case TabStage:
		return "Stage"
	case TabFaders:
		return "Faders"
	case TabRig:
		return "Rig"
	case TabPlayer:
		return "Player"
	case TabMIDI:
		return "Controllers"
	case TabLive:
		return "Live"
	case TabWatch:
		return "Monitor"
	}
	return t
}

// Start is the show crossroads opens with: a Fun Generation Diamond
// Dome at address 1, in the middle of an eight by five metre stage; a
// Lights module playing it; a fader for each of its colours, wired in;
// a mixer, a DMX out sending nowhere yet, and the stage view. Routes and
// Rig stand on the left, the Stage over the Faders on the right.
func Start() Show {
	s := Show{Name: "New show", Stage: Stage{Width: 8, Depth: 5, Spots: map[string]Spot{}}}
	dome := fixtures.Patch{ID: s.NewID("light"), Name: "Dome", DefinitionID: "fun-generation-led-diamond-dome-mk2",
		ModeID: "9ch", StartAddress: 1, Gamma: 2.2}
	s.Rig.Lights = append(s.Rig.Lights, dome)
	s.Stage.Spots[dome.ID] = Spot{X: 4, Y: 2.5}
	lights := Module{ID: s.NewID("m"), Kind: Lights, Name: "Dome", At: geom.Pt(320, 40), Lights: []string{dome.ID}}
	mixer := Module{ID: s.NewID("m"), Kind: Mixer, Name: "Mixer", At: geom.Pt(620, 120), Value: 1}
	out := Module{ID: s.NewID("m"), Kind: DMXOut, Name: "DMX out", At: geom.Pt(900, 60)}
	view := Module{ID: s.NewID("m"), Kind: StageView, Name: "Stage", At: geom.Pt(900, 220)}
	s.Modules = append(s.Modules, lights, mixer, out, view)
	for i, c := range []struct {
		name string
		role fixtures.Role
		v    float64
	}{
		{"Red", fixtures.RoleRed, 0.8},
		{"Green", fixtures.RoleGreen, 0},
		{"Blue", fixtures.RoleBlue, 0.6},
		{"White", fixtures.RoleWhite, 0},
		{"Amber", fixtures.RoleAmber, 0.3},
		{"UV", fixtures.RoleUV, 0},
	} {
		f := Module{ID: s.NewID("m"), Kind: Fader, Name: c.name, At: geom.Pt(0, float32(i)*90), Value: c.v}
		s.Modules = append(s.Modules, f)
		s.Wires = append(s.Wires, Wire{ID: s.NewID("w"), From: End{f.ID, "out"}, To: End{lights.ID, string(c.role)}})
	}
	s.Wires = append(s.Wires,
		Wire{ID: s.NewID("w"), From: End{lights.ID, "out"}, To: End{mixer.ID, "in1"}},
		Wire{ID: s.NewID("w"), From: End{mixer.ID, "out"}, To: End{out.ID, "in"}},
		Wire{ID: s.NewID("w"), From: End{mixer.ID, "out"}, To: End{view.ID, "in"}},
	)
	s.Layout = DefaultLayout()
	return s
}

// DefaultLayout is the layout a new show opens with: Routes, Rig,
// Controllers and Monitor on the left, the Stage over the Faders,
// Player and Live on the right, Routes and Faders in front.
func DefaultLayout() dock.Layout {
	l := dock.NewLayout(true, TabRoutes, TabRig, TabMIDI, TabWatch, TabStage, TabFaders, TabPlayer, TabLive)
	l.Move(TabStage, dock.Target{Side: dock.Right})
	l.Move(TabFaders, dock.Target{At: TabStage, Side: dock.Bottom})
	l.Move(TabPlayer, dock.Target{At: TabFaders, Index: -1})
	l.Move(TabLive, dock.Target{At: TabFaders, Index: -1})
	l.Show(TabFaders)
	l.Show(TabRoutes)
	return l
}
