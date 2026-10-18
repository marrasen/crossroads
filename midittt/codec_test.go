package midittt

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	gtempo "github.com/marrasen/gunim/audio/tempo"

	"github.com/marrasen/crossroads/fx"
	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/tempo"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// full is a configuration touching every field the file can hold, so a
// round trip proves the whole shape.
func full() Config {
	c := New()
	c.Routes = []routing.Route{
		{
			ID: "r1", Name: "Note ON → CC", Enabled: true,
			Input: routing.Input{DeviceName: "XYZ", Channel: 10, Type: routing.NoteOn, Data1: routing.Any()},
			Output: routing.Output{Kind: routing.ToMidi, DeviceName: "ABC", Channel: 1,
				Type: routing.CC, Data1: 111,
				Value: routing.Value{Mode: routing.Fixed, Fixed: 127,
					ScaleIn: [2]int{0, 127}, ScaleOut: [2]int{0, 127},
					RectifyCenter: routing.DefaultRectifyCenter}},
		},
		{
			ID: "r2", Name: "Bank → whole dome", Enabled: true,
			Input: routing.Input{DeviceName: "XYZ", Channel: routing.AnyChannel,
				Type: routing.CC, Data1: routing.Span(10, 18)},
			Output: routing.Output{Kind: routing.ToFixture, FixtureID: "f1", Target: 1, Spread: true,
				Value: routing.Value{Mode: routing.Scaled, ScaleIn: [2]int{0, 100},
					ScaleOut: [2]int{10, 200}, RectifyCenter: routing.DefaultRectifyCenter}},
		},
		{
			ID: "r3", Name: "Every bar → group", Enabled: false,
			Input:   routing.Input{DeviceName: "XYZ", Channel: 1, Type: routing.CC, Data1: routing.One(7)},
			Beat:    routing.Beat{EveryBeats: 4, OffsetBeats: 2},
			HasBeat: true,
			Output: routing.Output{Kind: routing.ToGroup, GroupID: "g1", Role: fixtures.RoleRed, Spread: true,
				Value: routing.Value{Mode: routing.Passthrough, ScaleIn: [2]int{0, 127},
					ScaleOut: [2]int{0, 255}, Rectify: true, RectifyCenter: 100}},
		},
		{
			ID: "r5", Name: "Play → start Link", Enabled: true,
			Input:     routing.Input{Channel: routing.AnyChannel, Type: routing.NoteOn, Data1: routing.Any()},
			Transport: tempo.TransportPlay,
			Output:    routing.Output{Kind: routing.ToLink, Link: routing.LinkStart},
		},
		{
			ID: "r6", Name: "Link stops → timer", Enabled: true,
			Input:  routing.Input{Channel: routing.AnyChannel, Type: routing.NoteOn, Data1: routing.Any()},
			Link:   routing.LinkOnStop,
			Output: routing.Output{Kind: routing.ToTimer, TimerID: "t1", Command: routing.TimerPause},
		},
		{
			ID: "r7", Name: "Drop → FX", Enabled: true,
			Input:  routing.Input{Channel: routing.AnyChannel, Type: routing.NoteOn, Data1: routing.Any()},
			Cue:    &routing.Cue{Song: "track.mp3", Bar: 33, Beat: 3},
			Output: routing.Output{Kind: routing.ToFx, ProgramID: "p1"},
		},
		{
			ID: "r4", Name: "Pad → timer", Enabled: true,
			Input:  routing.Input{DeviceName: "XYZ", Channel: 1, Type: routing.NoteOn, Data1: routing.One(36)},
			Output: routing.Output{Kind: routing.ToTimer, TimerID: "t1", Command: routing.TimerStart},
		},
		{
			ID: "r5", Name: "Pad → FX", Enabled: true,
			Input:  routing.Input{DeviceName: "XYZ", Channel: 1, Type: routing.NoteOn, Data1: routing.One(37)},
			Output: routing.Output{Kind: routing.ToFx, ProgramID: "p1"},
		},
		{
			ID: "r6", Name: "Fader → slot 12", Enabled: true,
			Input: routing.Input{DeviceName: "XYZ", Channel: 1, Type: routing.CC, Data1: routing.One(7)},
			Output: routing.Output{Kind: routing.ToDmx, Slot: 12,
				Value: routing.Value{Mode: routing.Fixed, Fixed: 255, ScaleIn: [2]int{0, 127},
					ScaleOut: [2]int{0, 255}, RectifyCenter: routing.DefaultRectifyCenter}},
		},
	}
	c.Timers = []Timer{{ID: "t1", Name: "Break", Mode: Countdown, DurationMs: 600000}}
	c.Fixtures = []fixtures.Patch{
		{ID: "f1", Name: "Dome 1", DefinitionID: "fun-generation-led-diamond-dome-mk2",
			ModeID: "9ch", StartAddress: 10, Gamma: 2.2, MaxLevel: 200},
		{ID: "f2", Name: "Par 1", DefinitionID: "generic-rgb", ModeID: "3ch",
			StartAddress: 100, Gamma: 1, MaxLevel: 255},
	}
	c.Groups = []fixtures.Group{{ID: "g1", Name: "Back", MemberIDs: []string{"f1", "f2"}}}
	c.Programs = []fx.Program{{ID: "p1", Name: "Flash", Quantize: fx.QuantizeBar, Steps: []fx.Step{
		{ID: "s1", FixtureID: "f1", Target: 1, TimeBase: fx.InMilliseconds,
			DelayMs: 0, From: 255, To: 0, DurationMs: 400, ScaleByVelocity: true},
		{ID: "s2", FixtureID: "f2", Target: 2, TimeBase: fx.InBeats,
			DelayBeats: 1, DurationBeats: 4, From: 0, To: 255},
	}}}
	c.Link = LinkSettings{Enabled: true, Quantum: 8, FollowStartStop: true}
	c.MidiClock = MidiClockSettings{Enabled: true, DeviceName: "Digitakt"}
	grid := gtempo.New(124, 4, 4)
	grid.SetTempo(gtempo.Tempo{At: 64 * gtempo.PPQ, BPM: 126})
	grid.SetStart(1234 * time.Millisecond)
	c.FixtureTypes = []fixtures.Definition{{ID: fixtures.CustomPrefix + "par", Manufacturer: "Generic",
		Model: "RGBW par", Modes: []fixtures.Mode{{ID: "default", Name: "5-channel", Channels: []fixtures.Channel{
			fixtures.ChannelFor(1, fixtures.Function(fixtures.RoleDimmer), "", 0),
			fixtures.ChannelFor(2, fixtures.Function(fixtures.RoleRed), "", 0),
			fixtures.ChannelFor(3, fixtures.FunctionUnused, "", 0),
			fixtures.ChannelFor(4, fixtures.FunctionUnknown, "Macro", 12),
			fixtures.ChannelFor(5, fixtures.Function(fixtures.RoleStrobe), "", 0),
		}}}}}
	c.Patterns = []pattern.Pattern{{ID: "pat", Name: "Chase", Bars: 2,
		Release: pattern.Release{Rest: true, FadeMs: 250},
		Lanes: []pattern.Lane{
			{ID: "l1", GroupID: "g1", Clips: []pattern.Clip{
				{ID: "c1", Start: 0, Length: 2, Kind: pattern.KindColour, Colour: pattern.Colour{R: 255, UV: 80}, FadeOut: 0.5},
				{ID: "c2", Start: 2, Length: 2, Kind: pattern.KindStrobe, Colour: pattern.Colour{W: 255}, Rate: 4},
			}},
			{ID: "l2", FixtureID: "f1", Clips: []pattern.Clip{
				{ID: "c3", Start: 0, Length: 8, Kind: pattern.KindChannel, Role: fixtures.RoleSpeed, From: 180, To: 180},
				{ID: "c4", Start: 4, Length: 4, Kind: pattern.KindChannel, Target: 9, From: 0, To: 200},
				{ID: "c5", Start: 0, Length: 4, Kind: pattern.KindLevel, From: 255, To: 40},
			}},
		}}}
	c.Song = "/music/track.mp3"
	c.Songs = []Song{{Name: "track.mp3", Grid: grid}}
	return c
}

func TestRoundTrip(t *testing.T) {
	want := full()
	data, err := Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the configuration changed across the file:\n got %+v\nwant %+v", got, want)
	}
}

func TestMarshalWritesTheFileShape(t *testing.T) {
	data, err := Marshal(full())
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]any
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f["app"] != appName {
		t.Errorf("app = %v, want %q", f["app"], appName)
	}
	if f["version"] != float64(FileVersion) {
		t.Errorf("version = %v, want %d", f["version"], FileVersion)
	}
	for _, key := range []string{"routes", "timers", "fixtures", "fixtureGroups", "fxPrograms"} {
		if _, ok := f[key]; !ok {
			t.Errorf("the file has no %q", key)
		}
	}
	// A wildcard channel is the string "any", as the file always had it.
	routes := f["routes"].([]any)
	input := routes[1].(map[string]any)["input"].(map[string]any)
	if input["channel"] != "any" {
		t.Errorf("a wildcard channel was written as %v", input["channel"])
	}
	// A span is an object with both ends.
	span := input["data1"].(map[string]any)
	if span["from"] != float64(10) || span["to"] != float64(18) {
		t.Errorf("a span was written as %v", span)
	}
}

func TestUnmarshalRejectsAnotherApplicationsFile(t *testing.T) {
	for _, data := range []string{`{"app":"something-else","routes":[]}`, `{"app":"midittt"}`} {
		if _, err := Unmarshal([]byte(data)); !errors.Is(err, ErrNotOurs) {
			t.Errorf("%s gave %v, want ErrNotOurs", data, err)
		}
	}
}

func TestUnmarshalV1FileWithNoOutputKind(t *testing.T) {
	// Version 0 had no kind on an output: every one was a MIDI message.
	// Version 1 keyed devices by a browser port id, which cannot be
	// mapped to a name, so those come through with no device.
	data := `{"app":"midittt","version":1,"routes":[{
		"id":"r1","name":"old","enabled":true,
		"input":{"deviceId":"abc","deviceName":"XYZ","channel":10,"type":"noteon","data1":"any"},
		"output":{"deviceId":"def","deviceName":"ABC","channel":1,"type":"cc","data1":111,
			"valueMode":"fixed","fixedValue":127}}]}`
	c, err := Unmarshal([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Routes[0]
	if r.Output.Kind != routing.ToMidi {
		t.Errorf("the output kind came out as %q, want midi", r.Output.Kind)
	}
	if r.Output.DeviceName != "" {
		t.Errorf("a version 1 device came through as %q, want it cleared", r.Output.DeviceName)
	}
	if r.Input.Channel != 10 || r.Input.Data1.Kind != routing.Data1Any {
		t.Errorf("the matcher came out as %+v", r.Input)
	}
}

func TestUnmarshalV3FixtureTargetAll(t *testing.T) {
	// Up to version 3 a fixture output could target the string "all",
	// which meant the whole fixture. That is now a start channel of one
	// with spreading on.
	data := `{"app":"midittt","version":3,"routes":[{
		"id":"r1","name":"whole dome","enabled":true,
		"input":{"deviceName":"XYZ","channel":1,"type":"cc","data1":{"from":10,"to":18}},
		"output":{"kind":"fixture","fixtureId":"f1","target":"all","valueMode":"passthrough"}}]}`
	c, err := Unmarshal([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	o := c.Routes[0].Output
	if o.Target != 1 || !o.Spread {
		t.Errorf("target = %d, spread = %v; want 1 and true", o.Target, o.Spread)
	}
}

func TestUnmarshalFillsInMissingSections(t *testing.T) {
	// Version 1 was routes alone; the rest arrived later, and an older
	// file simply has none of them.
	data := `{"app":"midittt","version":1,"routes":[]}`
	c, err := Unmarshal([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Timers)+len(c.Fixtures)+len(c.Groups)+len(c.Programs) != 0 {
		t.Errorf("an old file gained content: %+v", c)
	}
	if c.Link != DefaultLink || c.MidiClock != DefaultMidiClock {
		t.Errorf("settings = %+v, %+v; want the defaults", c.Link, c.MidiClock)
	}
}

func TestUnmarshalFillsInValueDefaults(t *testing.T) {
	// A scaled output that named no ranges used to fall back to 0-127 in
	// and 0-127 out for MIDI, 0-255 out for DMX.
	data := `{"app":"midittt","version":5,"routes":[
		{"id":"a","name":"","enabled":true,
		 "input":{"deviceName":"X","channel":1,"type":"cc","data1":7},
		 "output":{"kind":"midi","deviceName":"Y","channel":1,"type":"cc","data1":1,"valueMode":"scaled"}},
		{"id":"b","name":"","enabled":true,
		 "input":{"deviceName":"X","channel":1,"type":"cc","data1":7},
		 "output":{"kind":"dmx","channel":5,"valueMode":"scaled"}}]}`
	c, err := Unmarshal([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Routes[0].Output.Value; got.ScaleIn != ([2]int{0, 127}) || got.ScaleOut != ([2]int{0, 127}) {
		t.Errorf("MIDI ranges = %+v", got)
	}
	if got := c.Routes[1].Output.Value; got.ScaleIn != ([2]int{0, 127}) || got.ScaleOut != ([2]int{0, 255}) {
		t.Errorf("DMX ranges = %+v", got)
	}
}

func TestUnmarshalRejectsANonsensicalQuantum(t *testing.T) {
	data := `{"app":"midittt","version":7,"routes":[],"linkSettings":{"enabled":true,"quantum":0}}`
	c, err := Unmarshal([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if c.Link.Quantum != DefaultLink.Quantum {
		t.Errorf("quantum = %v, want the default %v", c.Link.Quantum, DefaultLink.Quantum)
	}
}

// TestARigIsLoadedFromItsFile, and a file not there is an error, not
// an empty rig.
func TestARigIsLoadedFromItsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rig.json")
	if _, err := Load(path); err == nil {
		t.Error("a rig not there loaded")
	}
	want := full()
	data, err := Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, string(data)); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the rig changed across the disk")
	}
}

func TestLoadReportsABrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeFile(path, "not json at all"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("a broken file loaded without complaint")
	}
}

// TestUnmarshalTheDaemonsTransportRoutes: the Go daemon wrote version 6
// rigs with routes on a transport and outputs that move Link. They read
// as those routes, not as MIDI routes on the matcher left behind them.
func TestUnmarshalTheDaemonsTransportRoutes(t *testing.T) {
	data := `{"app":"midittt","version":6,"routes":[
		{"id":"a","name":"go","enabled":true,
		 "input":{"deviceName":"","channel":"any","type":"noteon","data1":"any"},
		 "transport":{"on":"continue"},
		 "output":{"kind":"timer","timerId":"t1","command":"start"}},
		{"id":"b","name":"session","enabled":true,
		 "input":{"deviceName":"","channel":"any","type":"noteon","data1":"any"},
		 "link":{"on":"start"}, "transport":null,
		 "output":{"kind":"link","transport":"toggle"}}]}`
	c, err := Unmarshal([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if r := c.Routes[0]; r.Transport != tempo.TransportContinue || r.Link != "" {
		t.Errorf("the transport route came out as %+v", r)
	}
	if r := c.Routes[1]; r.Link != routing.LinkOnStart || r.Transport != "" ||
		r.Output.Kind != routing.ToLink || r.Output.Link != routing.LinkToggle {
		t.Errorf("the Link route came out as %+v", r)
	}
}

// TestUnmarshalRejectsAnEdgeItDoesNotKnow: a trigger read as nothing
// would leave the route on its MIDI matcher, firing on whatever that
// matches, so an edge from a later version is an error.
func TestUnmarshalRejectsAnEdgeItDoesNotKnow(t *testing.T) {
	for _, route := range []string{
		`"transport":{"on":"rewind"},"output":{"kind":"timer"}`,
		`"link":{"on":"pause"},"output":{"kind":"timer"}`,
		`"output":{"kind":"link","transport":"rewind"}`,
	} {
		data := `{"app":"midittt","version":7,"routes":[{"id":"x","input":{},` + route + `}]}`
		if _, err := Unmarshal([]byte(data)); err == nil {
			t.Errorf("%s read without an error", route)
		}
	}
}
