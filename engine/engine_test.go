package engine

import (
	"slices"
	"testing"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// moduleNamed returns the module of s named name.
func moduleNamed(t *testing.T, s *show.Show, name string) *show.Module {
	t.Helper()
	for i := range s.Modules {
		if s.Modules[i].Name == name {
			return &s.Modules[i]
		}
	}
	t.Fatalf("no module %s", name)
	return nil
}

// TestTheFadersPlayTheDome: each colour fader reaches its channel,
// curved as the dome asks, through the mixer to the stage; a fader
// moved plays at the next tick.
func TestTheFadersPlayTheDome(t *testing.T) {
	s := show.Start()
	e := New(nil)
	defer e.Close()
	e.Set(s)
	live := e.Tick()
	dome := s.Rig.Lights[0]
	want := func(v float64) byte {
		return byte(fixtures.CurveValue(dome, fixtures.Channel{Kind: fixtures.KindColor}, int(dmx.Level(v))))
	}
	if live.Stage[0] != want(0.8) || live.Stage[2] != want(0.6) || live.Stage[1] != 0 {
		t.Fatalf("red, green, blue are %d, %d, %d; want %d, 0, %d", live.Stage[0], live.Stage[1], live.Stage[2], want(0.8), want(0.6))
	}
	e.SetValue(moduleNamed(t, &s, "Green").ID, 1)
	if live := e.Tick(); live.Stage[1] != 255 {
		t.Errorf("green full plays %d", live.Stage[1])
	}
	// The grand master halves what dims.
	e.SetValue(moduleNamed(t, &s, "Mixer").ID, 0.5)
	if live := e.Tick(); live.Stage[1] != 127 && live.Stage[1] != 128 {
		t.Errorf("at half master, green full plays %d", live.Stage[1])
	}
}

// TestALightsModuleOffersWhatItsLightsDo: the dome's colours, strobe,
// motor and programmes, each a value in.
func TestALightsModuleOffersWhatItsLightsDo(t *testing.T) {
	s := show.Start()
	ins, outs := Ports(&s, moduleNamed(t, &s, "Dome"))
	got := map[string]bool{}
	for _, p := range ins {
		got[p.ID] = p.Signal == Value
	}
	for _, r := range []fixtures.Role{fixtures.RoleRed, fixtures.RoleUV, fixtures.RoleStrobe} {
		if !got[string(r)] {
			t.Errorf("no value input for %s among %v", r, ins)
		}
	}
	if len(outs) != 1 || outs[0].Signal != Light {
		t.Errorf("outputs %v", outs)
	}
}

// TestWiresJoinOneSignalOnceAndNeverInALoop: a value into a value
// input no wire feeds, never into light or a fed input, nor in a loop.
func TestWiresJoinOneSignalOnceAndNeverInALoop(t *testing.T) {
	s := show.Start()
	fader, dome, mixer := moduleNamed(t, &s, "Red"), moduleNamed(t, &s, "Dome"), moduleNamed(t, &s, "Mixer")
	for _, c := range []struct {
		from, to show.End
		ok       bool
	}{
		{show.End{Module: fader.ID, Port: "out"}, show.End{Module: dome.ID, Port: "strobe"}, true},
		{show.End{Module: fader.ID, Port: "out"}, show.End{Module: mixer.ID, Port: "in2"}, false},
		{show.End{Module: fader.ID, Port: "out"}, show.End{Module: dome.ID, Port: "red"}, false},
		{show.End{Module: dome.ID, Port: "out"}, show.End{Module: mixer.ID, Port: "in2"}, true},
	} {
		if ok, why := CanJoin(&s, c.from, c.to); ok != c.ok {
			t.Errorf("%v to %v: %v (%s), want %v", c.from, c.to, ok, why, c.ok)
		}
	}
	// A second mixer fed by the first may not feed it back.
	m2 := show.Module{ID: s.NewID("m"), Kind: show.Lights, Name: "Back"}
	s.Modules = append(s.Modules, m2)
	s.Wires = append(s.Wires, show.Wire{ID: "x", From: show.End{Module: dome.ID, Port: "out"}, To: show.End{Module: mixer.ID, Port: "in2"}})
	if ok, _ := CanJoin(&s, show.End{Module: mixer.ID, Port: "out"}, show.End{Module: mixer.ID, Port: "in3"}); ok {
		t.Error("a mixer fed its own output")
	}
}

// TestALightsModuleGoneGoesDark: the mixer hears it no more, and the
// stage, its wire pulled, shows the lights at rest.
func TestALightsModuleGoneGoesDark(t *testing.T) {
	s := show.Start()
	e := New(nil)
	defer e.Close()
	e.Set(s)
	if live := e.Tick(); live.Stage[0] == 0 {
		t.Fatal("the dome is dark to begin with")
	}
	s.RemoveModule(moduleNamed(t, &s, "Dome").ID)
	e.Set(s)
	if live := e.Tick(); live.Stage[0] != 0 {
		t.Errorf("the module gone, red still plays %d", live.Stage[0])
	}
	s = show.Start()
	e.Set(s)
	e.Tick()
	view := moduleNamed(t, &s, "Stage").ID
	s.Wires = slices.DeleteFunc(s.Wires, func(w show.Wire) bool { return w.To.Module == view })
	e.Set(s)
	if live := e.Tick(); live.Stage[0] != 0 {
		t.Errorf("its wire pulled, the stage still shows red at %d", live.Stage[0])
	}
}
