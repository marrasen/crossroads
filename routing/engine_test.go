package routing

import (
	"bytes"
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/marrasen/crossroads/tempo"
	"github.com/marrasen/gunim-dmx/fixtures"
)

func noteOn(channel, note, velocity int) Message {
	return Message{Type: NoteOn, Channel: channel, Data1: note, Data2: velocity}
}

func noteOff(channel, note int) Message {
	return Message{Type: NoteOff, Channel: channel, Data1: note}
}

func ccMsg(channel, number, value int) Message {
	return Message{Type: CC, Channel: channel, Data1: number, Data2: value}
}

// The driving example: Note ON on channel 10 of XYZ, any note, becomes
// CC 111 = 127 on ABC.
var noteOnToCC = Route{
	ID: "r1", Name: "Note ON → CC 111 = 127", Enabled: true,
	Input: Input{DeviceName: "XYZ", Channel: 10, Type: NoteOn, Data1: Any()},
	Output: Output{Kind: ToMidi, DeviceName: "ABC", Channel: 1, Type: CC, Data1: 111,
		Value: Value{Mode: Fixed, Fixed: 127}},
}

var noteOffToCC = Route{
	ID: "r2", Name: "Note OFF → CC 111 = 0", Enabled: true,
	Input: Input{DeviceName: "XYZ", Channel: 10, Type: NoteOff, Data1: Any()},
	Output: Output{Kind: ToMidi, DeviceName: "ABC", Channel: 1, Type: CC, Data1: 111,
		Value: Value{Mode: Fixed, Fixed: 0}},
}

func TestMatches(t *testing.T) {
	cases := []struct {
		name   string
		route  Route
		msg    Message
		device string
		want   bool
	}{
		{"the driving example", noteOnToCC, noteOn(10, 64, 100), "XYZ", true},
		{"any note, low", noteOnToCC, noteOn(10, 7, 100), "XYZ", true},
		{"any note, high", noteOnToCC, noteOn(10, 120, 1), "XYZ", true},
		{"the wrong device", noteOnToCC, noteOn(10, 64, 100), "OTHER", false},
		{"the wrong channel", noteOnToCC, noteOn(9, 64, 100), "XYZ", false},
		{"the wrong type", noteOnToCC, noteOff(10, 64), "XYZ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Matches(c.route, c.msg, c.device); got != c.want {
				t.Errorf("Matches = %v, want %v", got, c.want)
			}
		})
	}
	disabled := noteOnToCC
	disabled.Enabled = false
	if Matches(disabled, noteOn(10, 64, 100), "XYZ") {
		t.Error("a disabled route matched")
	}
}

func TestData1Matches(t *testing.T) {
	if !Any().Matches(0) || !Any().Matches(127) {
		t.Error("any did not match every number")
	}
	if !One(7).Matches(7) || One(7).Matches(8) {
		t.Error("a single number matched the wrong values")
	}
	span := Span(10, 18)
	for _, c := range []struct {
		value int
		want  bool
	}{{10, true}, {14, true}, {18, true}, {9, false}, {19, false}} {
		if got := span.Matches(c.value); got != c.want {
			t.Errorf("Span(10, 18).Matches(%d) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestMidiValue(t *testing.T) {
	if got := MidiValue(noteOnToCC.Output.Value, noteOn(10, 64, 5)); got != 127 {
		t.Errorf("fixed = %d, want 127", got)
	}
	if got := MidiValue(noteOffToCC.Output.Value, noteOff(10, 64)); got != 0 {
		t.Errorf("fixed zero = %d, want 0", got)
	}
	if got := MidiValue(Value{Mode: Passthrough}, noteOn(10, 64, 88)); got != 88 {
		t.Errorf("passthrough = %d, want 88", got)
	}
	scaled := Value{Mode: Scaled, ScaleIn: [2]int{0, 127}, ScaleOut: [2]int{0, 10}}
	if got := MidiValue(scaled, noteOn(10, 64, 127)); got != 10 {
		t.Errorf("scaled full = %d, want 10", got)
	}
	if got := MidiValue(scaled, noteOn(10, 64, 0)); got != 0 {
		t.Errorf("scaled zero = %d, want 0", got)
	}
}

func TestTransform(t *testing.T) {
	device, b := Transform(noteOnToCC, noteOn(10, 64, 100))
	if device != "ABC" || !bytes.Equal(b, []byte{0xb0, 111, 127}) {
		t.Errorf("note on gave %q %x", device, b)
	}
	device, b = Transform(noteOffToCC, noteOff(10, 64))
	if device != "ABC" || !bytes.Equal(b, []byte{0xb0, 111, 0}) {
		t.Errorf("note off gave %q %x", device, b)
	}
}

func TestTriggerMessage(t *testing.T) {
	_, b := Transform(noteOnToCC, TriggerMessage(noteOnToCC, 127))
	if !bytes.Equal(b, []byte{0xb0, 111, 127}) {
		t.Errorf("a fixed route triggered to %x", b)
	}
	pass := noteOnToCC
	pass.Output.Value = Value{Mode: Passthrough}
	if _, b := Transform(pass, TriggerMessage(pass, 127)); !bytes.Equal(b, []byte{0xb0, 111, 127}) {
		t.Errorf("full trigger gave %x", b)
	}
	if _, b := Transform(pass, TriggerMessage(pass, 40)); !bytes.Equal(b, []byte{0xb0, 111, 40}) {
		t.Errorf("a trigger at 40 gave %x", b)
	}
}

// a recorder collects what an engine drove.
type recorder struct {
	sent []struct {
		device string
		bytes  []byte
	}
	timers []struct {
		id  string
		cmd TimerCommand
	}
	writes [][2]int
	fx     []struct {
		id       string
		velocity int
	}
}

func newTestEngine(r *recorder) *Engine {
	return NewEngine(
		func(device string, b []byte) {
			r.sent = append(r.sent, struct {
				device string
				bytes  []byte
			}{device, b})
		},
		func(id string, cmd TimerCommand) {
			r.timers = append(r.timers, struct {
				id  string
				cmd TimerCommand
			}{id, cmd})
		},
		func(slot, value int) { r.writes = append(r.writes, [2]int{slot, value}) },
		func(id string, velocity int) {
			r.fx = append(r.fx, struct {
				id       string
				velocity int
			}{id, velocity})
		})
}

func TestEngineFansOutToEveryMatchingRoute(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	e.SetRoutes([]Route{noteOnToCC, noteOffToCC})

	e.HandleMessage("XYZ", noteOn(10, 64, 100))
	if len(r.sent) != 1 || r.sent[0].device != "ABC" || !bytes.Equal(r.sent[0].bytes, []byte{0xb0, 111, 127}) {
		t.Fatalf("note on sent %+v", r.sent)
	}
	e.HandleMessage("XYZ", noteOff(10, 64))
	if len(r.sent) != 2 || !bytes.Equal(r.sent[1].bytes, []byte{0xb0, 111, 0}) {
		t.Errorf("note off sent %+v", r.sent)
	}
}

func TestEngineIgnoresUnrelatedDevices(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	e.SetRoutes([]Route{noteOnToCC})
	e.HandleMessage("OTHER", noteOn(10, 64, 100))
	if len(r.sent) != 0 {
		t.Errorf("sent %d messages from an unrelated device", len(r.sent))
	}
}

func TestEngineDrivesATimer(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	start := Route{
		ID: "t-route", Name: "Pad → start timer", Enabled: true,
		Input:  Input{DeviceName: "XYZ", Channel: 10, Type: NoteOn, Data1: Any()},
		Output: Output{Kind: ToTimer, TimerID: "timer-1", Command: TimerStart},
	}
	e.SetRoutes([]Route{start})

	fired := e.HandleMessage("XYZ", noteOn(10, 64, 100))
	if len(r.timers) != 1 || r.timers[0].id != "timer-1" || r.timers[0].cmd != TimerStart {
		t.Errorf("timer calls = %+v", r.timers)
	}
	if len(r.sent) != 0 {
		t.Errorf("a timer route also sent MIDI: %+v", r.sent)
	}
	want := Fired{Kind: FiredTimer, RouteID: "t-route", TimerID: "timer-1", Command: TimerStart}
	if len(fired) != 1 || !firedEqual(fired[0], want) {
		t.Errorf("fired = %+v, want %+v", fired, want)
	}
}

func firedEqual(a, b Fired) bool {
	return a.Kind == b.Kind && a.RouteID == b.RouteID && a.DeviceName == b.DeviceName &&
		bytes.Equal(a.Bytes, b.Bytes) && a.TimerID == b.TimerID && a.Command == b.Command &&
		a.ProgramID == b.ProgramID && a.Velocity == b.Velocity && a.Slot == b.Slot &&
		a.Value == b.Value && a.Label == b.Label && slices.Equal(a.Slots, b.Slots)
}

// ---- DMX output routes ----

var rawDimmer = Route{
	ID: "d1", Name: "Fader → DMX 12", Enabled: true,
	Input:  Input{DeviceName: "XYZ", Channel: 1, Type: CC, Data1: One(7)},
	Output: Output{Kind: ToDmx, Slot: 12, Value: Value{Mode: Fixed, Fixed: 255}},
}

func cc7(value int) Message { return ccMsg(1, 7, value) }

func TestDmxValueFixedClamps(t *testing.T) {
	for _, c := range []struct{ fixed, want int }{{255, 255}, {999, 255}, {-1, 0}} {
		if got := DmxValue(Value{Mode: Fixed, Fixed: c.fixed}, cc7(3)); got != c.want {
			t.Errorf("fixed %d = %d, want %d", c.fixed, got, c.want)
		}
	}
}

func TestDmxValuePassthroughStretches(t *testing.T) {
	for _, c := range []struct{ in, want int }{{127, 255}, {0, 0}, {64, 129}} {
		if got := DmxValue(Value{Mode: Passthrough}, cc7(c.in)); got != c.want {
			t.Errorf("passthrough %d = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestDmxValueScaled(t *testing.T) {
	full := Value{Mode: Scaled, ScaleIn: [2]int{0, 127}, ScaleOut: [2]int{0, 255}}
	if got := DmxValue(full, cc7(127)); got != 255 {
		t.Errorf("scaled to the full range = %d, want 255", got)
	}
	dim := Value{Mode: Scaled, ScaleIn: [2]int{0, 127}, ScaleOut: [2]int{10, 20}}
	if got := DmxValue(dim, cc7(0)); got != 10 {
		t.Errorf("scaled bottom = %d, want 10", got)
	}
	if got := DmxValue(dim, cc7(127)); got != 20 {
		t.Errorf("scaled top = %d, want 20", got)
	}
}

func TestTransformDmx(t *testing.T) {
	if got := (TransformDmx(rawDimmer, cc7(0))); got != (Write{Slot: 12, Value: 255}) {
		t.Errorf("TransformDmx = %+v", got)
	}
	pass := rawDimmer
	pass.Output.Value = Value{Mode: Passthrough}
	if got := TransformDmx(pass, TriggerMessage(pass, 127)).Value; got != 255 {
		t.Errorf("a manual trigger gave %d, want full", got)
	}
}

func TestEngineSetsADmxSlot(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	e.SetRoutes([]Route{rawDimmer})
	fired := e.HandleMessage("XYZ", cc7(100))
	if !slices.Equal(r.writes, [][2]int{{12, 255}}) {
		t.Errorf("writes = %v", r.writes)
	}
	if len(r.sent) != 0 {
		t.Errorf("a DMX route also sent MIDI: %+v", r.sent)
	}
	want := Fired{Kind: FiredDmx, RouteID: "d1", Slot: 12, Value: 255}
	if len(fired) != 1 || !firedEqual(fired[0], want) {
		t.Errorf("fired = %+v, want %+v", fired, want)
	}
}

// ---- fixture routes ----

var dome = fixtures.Patch{ID: "fx1", Name: "Dome 1",
	DefinitionID: "fun-generation-led-diamond-dome-mk2", ModeID: "9ch", StartAddress: 10}

func fixtureRoute(id string, data1 Data1, target int, spread bool) Route {
	return Route{
		ID: id, Name: "fixture route", Enabled: true,
		Input: Input{DeviceName: "XYZ", Channel: 6, Type: CC, Data1: data1},
		Output: Output{Kind: ToFixture, FixtureID: "fx1", Target: target, Spread: spread,
			Value: Value{Mode: Passthrough}},
	}
}

func TestTransformFixtureResolvesThroughTheStartAddress(t *testing.T) {
	green := fixtureRoute("g1", One(30), 2, false)
	got, ok := TransformFixture(green, ccMsg(6, 30, 127), []fixtures.Patch{dome}, fixtures.Catalog{})
	want := FixtureResult{Slot: 11, Value: 255, Label: "Dome 1 · Intensity Green"}
	if !ok || got != want {
		t.Errorf("= %+v, %v; want %+v", got, ok, want)
	}
	moved := dome
	moved.StartAddress = 100
	got, _ = TransformFixture(green, ccMsg(6, 30, 127), []fixtures.Patch{moved}, fixtures.Catalog{})
	if got.Slot != 101 {
		t.Errorf("re-addressed fixture resolved to %d, want 101", got.Slot)
	}
}

func TestTransformFixtureSpreadsFromAnyStartChannel(t *testing.T) {
	// The "just the last four channels" case: a short span starting
	// partway into the fixture.
	tail := fixtureRoute("t1", Span(50, 53), 6, true)
	cases := []struct {
		number int
		label  string
		slot   int
	}{
		{50, "Dome 1 · Intensity Purple / UV", 15},
		{51, "Dome 1 · Stroboscope", 16},
		{52, "Dome 1 · Motor", 17},
		{53, "Dome 1 · Built-in programmes", 18},
	}
	for _, c := range cases {
		got, ok := TransformFixture(tail, ccMsg(6, c.number, 127), []fixtures.Patch{dome}, fixtures.Catalog{})
		if !ok || got.Label != c.label || got.Slot != c.slot {
			t.Errorf("CC %d gave %+v, want %q at %d", c.number, got, c.label, c.slot)
		}
	}
}

func TestTransformFixtureWithoutSpreadPinsTheStartChannel(t *testing.T) {
	// Every number in the span lands on channel one, which is the trap
	// the editor warns about.
	pinned := fixtureRoute("w1", Span(10, 18), 1, false)
	for _, number := range []int{10, 14, 18} {
		got, _ := TransformFixture(pinned, ccMsg(6, number, 127), []fixtures.Patch{dome}, fixtures.Catalog{})
		if got.Slot != 10 {
			t.Errorf("CC %d resolved to %d, want 10", number, got.Slot)
		}
	}
}

func TestTransformFixtureSpreadsAWholeFixture(t *testing.T) {
	whole := fixtureRoute("w1", Span(10, 18), 1, true)
	cases := []struct {
		number, slot int
		label        string
	}{
		{10, 10, "Dome 1 · Intensity Red"},
		{12, 12, "Dome 1 · Intensity Blue"},
		{18, 18, "Dome 1 · Built-in programmes"},
	}
	for _, c := range cases {
		got, _ := TransformFixture(whole, ccMsg(6, c.number, 127), []fixtures.Patch{dome}, fixtures.Catalog{})
		if got.Slot != c.slot || got.Label != c.label {
			t.Errorf("CC %d gave %+v, want %q at %d", c.number, got, c.label, c.slot)
		}
	}
}

func TestTransformFixtureIgnoresMessagesPastTheLastChannel(t *testing.T) {
	wide := fixtureRoute("w1", Span(10, 30), 1, true)
	if _, ok := TransformFixture(wide, ccMsg(6, 19, 127), []fixtures.Patch{dome}, fixtures.Catalog{}); ok {
		t.Error("a message past the fixture's last channel resolved")
	}
	late := fixtureRoute("t1", Span(50, 59), 6, true)
	if got, ok := TransformFixture(late, ccMsg(6, 53, 127), []fixtures.Patch{dome}, fixtures.Catalog{}); !ok || got.Slot != 18 {
		t.Errorf("the last channel gave %+v, %v", got, ok)
	}
	if _, ok := TransformFixture(late, ccMsg(6, 54, 127), []fixtures.Patch{dome}, fixtures.Catalog{}); ok {
		t.Error("a spread running off the end resolved")
	}
}

func TestTransformFixtureIgnoresAnUnpatchedFixture(t *testing.T) {
	green := fixtureRoute("g1", One(30), 2, false)
	if _, ok := TransformFixture(green, ccMsg(6, 30, 127), nil, fixtures.Catalog{}); ok {
		t.Error("an unpatched fixture resolved")
	}
}

func TestTransformFixtureTriggersAtTheStartChannel(t *testing.T) {
	whole := fixtureRoute("w1", Span(10, 18), 1, true)
	got, ok := TransformFixture(whole, TriggerMessage(whole, 127), []fixtures.Patch{dome}, fixtures.Catalog{})
	want := FixtureResult{Slot: 10, Value: 255, Label: "Dome 1 · Intensity Red"}
	if !ok || got != want {
		t.Errorf("= %+v, %v; want %+v", got, ok, want)
	}
}

func TestEngineDrivesAResolvedFixtureSlot(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	e.SetFixtures([]fixtures.Patch{dome})
	whole := fixtureRoute("w1", Span(10, 18), 1, true)
	e.SetRoutes([]Route{whole})

	fired := e.HandleMessage("XYZ", ccMsg(6, 11, 127))
	if !slices.Equal(r.writes, [][2]int{{11, 255}}) {
		t.Errorf("writes = %v", r.writes)
	}
	want := Fired{Kind: FiredDmx, RouteID: "w1", Slot: 11, Value: 255, Label: "Dome 1 · Intensity Green"}
	if len(fired) != 1 || !firedEqual(fired[0], want) {
		t.Errorf("fired = %+v, want %+v", fired, want)
	}
}

func TestEngineStaysSilentWhenTheFixtureIsUnpatched(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	e.SetRoutes([]Route{fixtureRoute("g1", One(30), 2, false)})
	if fired := e.HandleMessage("XYZ", ccMsg(6, 30, 127)); len(fired) != 0 {
		t.Errorf("fired = %+v, want nothing", fired)
	}
	if len(r.writes) != 0 {
		t.Errorf("writes = %v, want none", r.writes)
	}
}

// ---- FX routes ----

var flash = Route{
	ID: "fx-route", Name: "Pad → white flash", Enabled: true,
	Input:  Input{DeviceName: "XYZ", Channel: 1, Type: NoteOn, Data1: One(36)},
	Output: Output{Kind: ToFx, ProgramID: "p1"},
}

func TestFxRoutes(t *testing.T) {
	hit := func(velocity int) Message { return noteOn(1, 36, velocity) }

	t.Run("passes the velocity to the programme", func(t *testing.T) {
		var r recorder
		e := newTestEngine(&r)
		e.SetRoutes([]Route{flash})
		e.HandleMessage("XYZ", hit(64))
		if len(r.fx) != 1 || r.fx[0].id != "p1" || r.fx[0].velocity != 64 {
			t.Errorf("fx = %+v", r.fx)
		}
	})

	t.Run("writes no DMX itself", func(t *testing.T) {
		var r recorder
		e := newTestEngine(&r)
		e.SetRoutes([]Route{flash})
		e.HandleMessage("XYZ", hit(127))
		if len(r.writes) != 0 {
			t.Errorf("writes = %v, want none; the FX player does that", r.writes)
		}
	})

	t.Run("reports the firing", func(t *testing.T) {
		var r recorder
		e := newTestEngine(&r)
		e.SetRoutes([]Route{flash})
		fired := e.HandleMessage("XYZ", hit(100))
		want := Fired{Kind: FiredFx, RouteID: "fx-route", ProgramID: "p1", Velocity: 100}
		if len(fired) != 1 || !firedEqual(fired[0], want) {
			t.Errorf("fired = %+v, want %+v", fired, want)
		}
	})

	t.Run("fires again on every hit", func(t *testing.T) {
		var r recorder
		e := newTestEngine(&r)
		e.SetRoutes([]Route{flash})
		e.HandleMessage("XYZ", hit(10))
		e.HandleMessage("XYZ", hit(20))
		if len(r.fx) != 2 || r.fx[0].velocity != 10 || r.fx[1].velocity != 20 {
			t.Errorf("fx = %+v", r.fx)
		}
	})
}

// ---- fixture groups ----

// Two Domes and an RGB par: the mixed rig a group has to cope with.
var (
	domeA = fixtures.Patch{ID: "a", Name: "Dome 1",
		DefinitionID: "fun-generation-led-diamond-dome-mk2", ModeID: "9ch", StartAddress: 1}
	domeB = fixtures.Patch{ID: "b", Name: "Dome 2",
		DefinitionID: "fun-generation-led-diamond-dome-mk2", ModeID: "9ch", StartAddress: 20}
	par = fixtures.Patch{ID: "c", Name: "Par 1",
		DefinitionID: "generic-rgb", ModeID: "3ch", StartAddress: 100}
	rig  = []fixtures.Patch{domeA, domeB, par}
	back = fixtures.Group{ID: "g1", Name: "Back", MemberIDs: []string{"a", "b", "c"}}
)

func groupRoute(role fixtures.Role, spread bool, data1 Data1, value Value) Route {
	return Route{
		ID: "gr", Name: "group route", Enabled: true,
		Input:  Input{DeviceName: "XYZ", Channel: 1, Type: CC, Data1: data1},
		Output: Output{Kind: ToGroup, GroupID: "g1", Role: role, Spread: spread, Value: value},
	}
}

func plainGroup() Route {
	return groupRoute(fixtures.RoleRed, false, One(20), Value{Mode: Passthrough})
}

func TestTransformGroup(t *testing.T) {
	t.Run("drives the function on every member at once", func(t *testing.T) {
		got, ok := TransformGroup(plainGroup(), ccMsg(1, 20, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
		// Red is offset one on all three, so each fixture's own start address.
		if !ok || !slices.Equal(got.Slots(), []int{1, 20, 100}) || got.Value != 255 || got.Label != "Back · Red" {
			t.Errorf("= %+v, %v", got, ok)
		}
	})

	t.Run("resolves the function to each fixture's own offset", func(t *testing.T) {
		// UV is the Dome's sixth channel; the par has none and drops out.
		r := groupRoute(fixtures.RoleUV, false, One(20), Value{Mode: Passthrough})
		got, _ := TransformGroup(r, ccMsg(1, 20, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
		if !slices.Equal(got.Slots(), []int{6, 25}) {
			t.Errorf("slots = %v", got.Slots())
		}
	})

	t.Run("skips members lacking the function", func(t *testing.T) {
		r := groupRoute(fixtures.RoleProgram, false, One(20), Value{Mode: Passthrough})
		got, _ := TransformGroup(r, ccMsg(1, 20, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
		if !slices.Equal(got.Slots(), []int{9, 28}) {
			t.Errorf("slots = %v", got.Slots())
		}
	})

	t.Run("writes nothing when no member has the function", func(t *testing.T) {
		r := groupRoute(fixtures.RolePan, false, One(20), Value{Mode: Passthrough})
		got, ok := TransformGroup(r, ccMsg(1, 20, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
		if !ok || len(got.Slots()) != 0 {
			t.Errorf("= %+v, %v", got, ok)
		}
	})

	t.Run("reports false for a deleted group", func(t *testing.T) {
		if _, ok := TransformGroup(plainGroup(), ccMsg(1, 20, 127), rig, nil, fixtures.Catalog{}); ok {
			t.Error("a deleted group resolved")
		}
	})

	t.Run("survives a member being unpatched", func(t *testing.T) {
		got, _ := TransformGroup(plainGroup(), ccMsg(1, 20, 127), []fixtures.Patch{domeA, par}, []fixtures.Group{back}, fixtures.Catalog{})
		if !slices.Equal(got.Slots(), []int{1, 100}) {
			t.Errorf("slots = %v", got.Slots())
		}
	})

	t.Run("chases one fixture per number when spreading", func(t *testing.T) {
		r := groupRoute(fixtures.RoleRed, true, Span(20, 22), Value{Mode: Passthrough})
		for _, c := range []struct {
			number int
			want   []int
		}{{20, []int{1}}, {21, []int{20}}, {22, []int{100}}} {
			got, _ := TransformGroup(r, ccMsg(1, c.number, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
			if !slices.Equal(got.Slots(), c.want) {
				t.Errorf("CC %d wrote %v, want %v", c.number, got.Slots(), c.want)
			}
		}
	})

	t.Run("names the member it hit when chasing", func(t *testing.T) {
		r := groupRoute(fixtures.RoleRed, true, Span(20, 22), Value{Mode: Passthrough})
		got, _ := TransformGroup(r, ccMsg(1, 21, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
		if got.Label != "Back · Red · Dome 2" {
			t.Errorf("label = %q", got.Label)
		}
	})

	t.Run("addresses members by position, so a gap stays a gap", func(t *testing.T) {
		// The par is third in the group and has no UV, so the chase's
		// third number does nothing rather than sliding onto a fixture
		// that does.
		r := groupRoute(fixtures.RoleUV, true, Span(20, 22), Value{Mode: Passthrough})
		got, _ := TransformGroup(r, ccMsg(1, 22, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
		if len(got.Slots()) != 0 {
			t.Errorf("slots = %v, want none", got.Slots())
		}
	})

	t.Run("ignores numbers past the end of the group", func(t *testing.T) {
		r := groupRoute(fixtures.RoleRed, true, Span(20, 29), Value{Mode: Passthrough})
		got, _ := TransformGroup(r, ccMsg(1, 25, 127), rig, []fixtures.Group{back}, fixtures.Catalog{})
		if len(got.Slots()) != 0 {
			t.Errorf("slots = %v, want none", got.Slots())
		}
	})

	t.Run("applies the value mode once, to every member", func(t *testing.T) {
		r := groupRoute(fixtures.RoleRed, false, One(20), Value{Mode: Fixed, Fixed: 77})
		got, _ := TransformGroup(r, ccMsg(1, 20, 0), rig, []fixtures.Group{back}, fixtures.Catalog{})
		if got.Value != 77 || len(got.Slots()) != 3 {
			t.Errorf("= %+v", got)
		}
	})
}

func TestEngineGroupRoutes(t *testing.T) {
	t.Run("writes every slot and reports one row", func(t *testing.T) {
		var r recorder
		e := newTestEngine(&r)
		e.SetFixtures(rig)
		e.SetGroups([]fixtures.Group{back})
		e.SetRoutes([]Route{plainGroup()})

		fired := e.HandleMessage("XYZ", ccMsg(1, 20, 127))
		if !slices.Equal(r.writes, [][2]int{{1, 255}, {20, 255}, {100, 255}}) {
			t.Errorf("writes = %v", r.writes)
		}
		// One row per route, not one per fixture, so a group of twelve
		// does not bury the activity log.
		if len(fired) != 1 || fired[0].Kind != FiredGroup || !slices.Equal(fired[0].Slots, []int{1, 20, 100}) {
			t.Errorf("fired = %+v", fired)
		}
	})

	t.Run("does nothing when the group has been deleted", func(t *testing.T) {
		var r recorder
		e := newTestEngine(&r)
		e.SetFixtures(rig)
		e.SetRoutes([]Route{plainGroup()})
		if fired := e.HandleMessage("XYZ", ccMsg(1, 20, 127)); len(fired) != 0 {
			t.Errorf("fired = %+v", fired)
		}
		if len(r.writes) != 0 {
			t.Errorf("writes = %v", r.writes)
		}
	})
}

// ---- rectifying bipolar modulation ----

func TestRectify(t *testing.T) {
	// The whole point: an LFO at zero depth parks at the centre and must
	// go dark.
	if got := Rectify(64, 64); got != 0 {
		t.Errorf("the centre read %d, want 0", got)
	}
	if got := Rectify(0, 64); got != 127 {
		t.Errorf("the bottom read %d, want 127", got)
	}
	// 0 to 127 has no exact centre: 64 leaves 64 steps below and only 63
	// above, so the shorter half stops just shy of full rather than the
	// centre drifting off zero.
	if got := Rectify(127, 64); got != 125 {
		t.Errorf("the top read %d, want 125", got)
	}
	if Rectify(64-20, 64) != Rectify(64+20, 64) {
		t.Error("equal deflections either side read differently")
	}
	previous := -1
	for d := range 61 {
		v := Rectify(64+d, 64)
		if v < previous {
			t.Fatalf("%d off centre read %d, below the %d before it", d, v, previous)
		}
		previous = v
	}
	if got := Rectify(100, 100); got != 0 {
		t.Errorf("a centre of 100 read %d at 100, want 0", got)
	}
	if got := Rectify(0, 100); got != 127 {
		t.Errorf("a centre of 100 read %d at 0, want 127", got)
	}
}

func TestDmxValueWithRectify(t *testing.T) {
	at := func(data2 int, v Value) int {
		v.Rectify = true
		return DmxValue(v, cc7(data2))
	}
	pass := Value{Mode: Passthrough}

	t.Run("black at the centre, full at both extremes", func(t *testing.T) {
		for _, c := range []struct{ in, want int }{{64, 0}, {0, 255}, {127, 251}} {
			if got := at(c.in, pass); got != c.want {
				t.Errorf("%d became %d, want %d", c.in, got, c.want)
			}
		}
	})

	t.Run("follows the abs(cv - 64) * 4 shape asked for", func(t *testing.T) {
		// Within a step or two of the arithmetic, across the range.
		for _, cv := range []int{0, 16, 32, 48, 64, 80, 96, 112, 127} {
			want := min(255, abs(cv-64)*4)
			if got := at(cv, pass); math.Abs(float64(got-want)) > 2 {
				t.Errorf("%d became %d, want about %d", cv, got, want)
			}
		}
	})

	t.Run("composes with a scaled output range", func(t *testing.T) {
		// Rectify folds the input; the scale still maps it onto the range.
		scaled := Value{Mode: Scaled, ScaleIn: [2]int{0, 127}, ScaleOut: [2]int{0, 100}}
		if got := at(64, scaled); got != 0 {
			t.Errorf("the centre became %d, want 0", got)
		}
		if got := at(0, scaled); got != 100 {
			t.Errorf("the bottom became %d, want 100", got)
		}
	})

	t.Run("leaves a fixed value alone", func(t *testing.T) {
		if got := at(64, Value{Mode: Fixed, Fixed: 200}); got != 200 {
			t.Errorf("a fixed value became %d, want 200", got)
		}
	})

	t.Run("changes nothing when switched off", func(t *testing.T) {
		if got := DmxValue(pass, cc7(64)); got != 129 {
			t.Errorf("64 became %d, want 129", got)
		}
	})
}

// ---- the fixture's dimmer curve reaches the wire ----

func TestDimmerCurveThroughRoutes(t *testing.T) {
	curved := domeA
	curved.Gamma = 2.2

	route := func(fixtureID string, target int) Route {
		return Route{
			ID: "fr", Name: "fader to colour", Enabled: true,
			Input:  Input{DeviceName: "XYZ", Channel: 1, Type: CC, Data1: One(7)},
			Output: Output{Kind: ToFixture, FixtureID: fixtureID, Target: target, Value: Value{Mode: Passthrough}},
		}
	}

	t.Run("curves a fixture route's value", func(t *testing.T) {
		// Passthrough turns CC 64 into DMX 129; the curve pulls it to 57.
		plain, _ := TransformFixture(route("a", 1), ccMsg(1, 7, 64), []fixtures.Patch{domeA}, fixtures.Catalog{})
		if plain.Value != 129 {
			t.Errorf("uncurved = %d, want 129", plain.Value)
		}
		shaped, _ := TransformFixture(route("a", 1), ccMsg(1, 7, 64), []fixtures.Patch{curved}, fixtures.Catalog{})
		if shaped.Value != 57 {
			t.Errorf("curved = %d, want 57", shaped.Value)
		}
	})

	t.Run("leaves a behaviour channel alone", func(t *testing.T) {
		// Channel eight is the motor: 129 there picks a rotation speed.
		shaped, _ := TransformFixture(route("a", 8), ccMsg(1, 7, 64), []fixtures.Patch{curved}, fixtures.Catalog{})
		if shaped.Value != 129 {
			t.Errorf("the motor got %d, want 129", shaped.Value)
		}
	})

	t.Run("curves each group member with its own setting", func(t *testing.T) {
		// Dome 1 corrected, Dome 2 not: one group, two different outputs.
		got, _ := TransformGroup(plainGroup(), ccMsg(1, 20, 64),
			[]fixtures.Patch{curved, domeB, par}, []fixtures.Group{back}, fixtures.Catalog{})
		want := []Write{{1, 57}, {20, 129}, {100, 129}}
		if !slices.Equal(got.Writes, want) {
			t.Errorf("writes = %+v, want %+v", got.Writes, want)
		}
		// The value before the curve is what the activity log reports.
		if got.Value != 129 {
			t.Errorf("reported value = %d, want 129", got.Value)
		}
	})

	t.Run("writes the curved values through the engine", func(t *testing.T) {
		var r recorder
		e := newTestEngine(&r)
		e.SetFixtures([]fixtures.Patch{curved, domeB, par})
		e.SetGroups([]fixtures.Group{back})
		e.SetRoutes([]Route{plainGroup()})
		e.HandleMessage("XYZ", ccMsg(1, 20, 64))
		if !slices.Equal(r.writes, [][2]int{{1, 57}, {20, 129}, {100, 129}}) {
			t.Errorf("writes = %v", r.writes)
		}
	})
}

// ---- beat-triggered routes ----

func beatRoute(every, offset int) Route {
	return Route{
		ID: "b1", Name: "Every beat → CC", Enabled: true,
		Input:   Input{DeviceName: "XYZ", Channel: 1, Type: NoteOn, Data1: Any()},
		Beat:    Beat{EveryBeats: every, OffsetBeats: offset},
		HasBeat: true,
		Output: Output{Kind: ToMidi, DeviceName: "OUT", Channel: 1, Type: CC, Data1: 20,
			Value: Value{Mode: Fixed, Fixed: 127}},
	}
}

func TestBeatRouteNeverFiresOnMidi(t *testing.T) {
	r := beatRoute(1, 0)
	if Matches(r, noteOn(1, 60, 100), "XYZ") {
		t.Error("a beat route matched a MIDI message")
	}
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{r})
	if fired := e.HandleMessage("XYZ", noteOn(1, 60, 100)); len(fired) != 0 {
		t.Errorf("fired = %+v", fired)
	}
}

func TestBeatRouteSeedsThenFiresPerBeat(t *testing.T) {
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(1, 0)})
	if fired := e.HandleBeat(4.2); len(fired) != 0 {
		t.Errorf("the seeding sample fired %+v", fired)
	}
	if fired := e.HandleBeat(4.9); len(fired) != 0 {
		t.Errorf("a part beat fired %+v", fired)
	}
	if fired := e.HandleBeat(6.1); len(fired) != 2 { // crosses beats 5 and 6
		t.Errorf("crossing two beats fired %d times", len(fired))
	}
	if len(rec.sent) != 2 {
		t.Errorf("sent %d messages, want 2", len(rec.sent))
	}
}

func TestBeatRouteHonoursIntervalAndOffset(t *testing.T) {
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(4, 0)}) // a downbeat
	e.HandleBeat(0)
	// Crossing beats 1 to 8, only 4 and 8 are multiples of four.
	if fired := e.HandleBeat(8.5); len(fired) != 2 {
		t.Errorf("fired %d times, want 2", len(fired))
	}
}

func TestBeatRouteCrossesNothingWhileTheClockHolds(t *testing.T) {
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(1, 0)})
	e.HandleBeat(2)
	for range 2 {
		if fired := e.HandleBeat(2); len(fired) != 0 {
			t.Errorf("a held clock fired %+v", fired)
		}
	}
}

func TestBeatRouteSurvivesADriftCorrection(t *testing.T) {
	// A network tempo source re-anchors both ways as it corrects drift,
	// so the sampled position can step slightly backwards. Lowering the
	// grid to match would fire the beat again on the way back.
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(1, 0)})
	e.HandleBeat(3.99) // seed just short of beat four
	if fired := e.HandleBeat(4.01); len(fired) != 1 {
		t.Fatalf("beat four fired %d times", len(fired))
	}
	if fired := e.HandleBeat(3.98); len(fired) != 0 {
		t.Errorf("the correction fired %+v", fired)
	}
	if fired := e.HandleBeat(4.02); len(fired) != 0 {
		t.Errorf("beat four fired twice: %+v", fired)
	}
	if fired := e.HandleBeat(5.01); len(fired) != 1 {
		t.Errorf("beat five fired %d times", len(fired))
	}
}

func TestBeatRouteStartsAgainOnARealRewind(t *testing.T) {
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(1, 0)})
	e.HandleBeat(20)
	if fired := e.HandleBeat(4); len(fired) != 0 { // a seek, not a correction
		t.Errorf("a seek fired %+v", fired)
	}
	if fired := e.HandleBeat(5); len(fired) != 1 {
		t.Errorf("the grid did not follow the new position: %+v", fired)
	}
}

func TestResetBeatSeedsAgain(t *testing.T) {
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(1, 0)})
	e.HandleBeat(10)
	e.ResetBeat()
	if fired := e.HandleBeat(20); len(fired) != 0 {
		t.Errorf("re-seeding fired %+v", fired)
	}
	if fired := e.HandleBeat(21); len(fired) != 1 {
		t.Errorf("the next beat fired %d times", len(fired))
	}
}

func TestBeatCatchupIsCapped(t *testing.T) {
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(1, 0)})
	e.HandleBeat(0)
	// A source seek jumps far ahead, and must not fire a burst.
	if fired := e.HandleBeat(600); len(fired) != 0 {
		t.Errorf("a large jump fired %d times", len(fired))
	}
	// It carries on normally from the new baseline.
	if fired := e.HandleBeat(601); len(fired) != 1 {
		t.Errorf("the next beat fired %d times", len(fired))
	}
}

func TestBeatCatchupStillReplaysASmallGap(t *testing.T) {
	var rec recorder
	e := newTestEngine(&rec)
	e.SetRoutes([]Route{beatRoute(1, 0)})
	e.HandleBeat(0)
	if fired := e.HandleBeat(3); len(fired) != 3 { // beats 1, 2 and 3
		t.Errorf("fired %d times, want 3", len(fired))
	}
}

// TestTheEngineKeepsItsOwnFixtures: the application edits its fixture
// list in place as one is saved. The engine reads its list on the MIDI
// goroutine, so it must hold a copy, or the race detector finds the two
// meeting.
func TestTheEngineKeepsItsOwnFixtures(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	patches := []fixtures.Patch{domeA}
	e.SetFixtures(patches)
	e.SetRoutes([]Route{{
		ID: "fr", Name: "fader to colour", Enabled: true,
		Input:  Input{DeviceName: "XYZ", Channel: 1, Type: CC, Data1: One(7)},
		Output: Output{Kind: ToFixture, FixtureID: domeA.ID, Target: 1, Value: Value{Mode: Passthrough}},
	}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			e.HandleMessage("XYZ", ccMsg(1, 7, 64))
		}
	}()
	for i := range 200 {
		edited := domeA
		edited.Name = "Dome " + strconv.Itoa(i)
		patches[0] = edited
	}
	<-done
	if len(r.writes) == 0 {
		t.Fatal("the route never reached the fixture")
	}
}

// TestATransportRouteFiresOnItsEdgeAndNotOnMidi: a route on the
// transport fires as the transport moves its way, Play on every way it
// begins, and never on a MIDI message its matcher would otherwise take.
func TestATransportRouteFiresOnItsEdgeAndNotOnMidi(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	cue := Route{ID: "go", Enabled: true, Transport: tempo.TransportPlay,
		Input:  Input{DeviceName: "XYZ", Channel: 1, Type: CC, Data1: One(7)},
		Output: Output{Kind: ToTimer, TimerID: "t1", Command: TimerStart}}
	stop := Route{ID: "end", Enabled: true, Transport: tempo.TransportStop,
		Output: Output{Kind: ToTimer, TimerID: "t1", Command: TimerPause}}
	e.SetRoutes([]Route{cue, stop})

	if fired := e.HandleMessage("XYZ", ccMsg(1, 7, 64)); len(fired) != 0 {
		t.Errorf("a transport route fired on MIDI: %+v", fired)
	}
	for _, edge := range []tempo.TransportEdge{tempo.TransportStart, tempo.TransportContinue, tempo.TransportPlay} {
		fired := e.HandleTransport(edge)
		if len(fired) != 1 || fired[0].RouteID != "go" {
			t.Errorf("on %q the routes fired were %+v, want the Play cue", edge, fired)
		}
	}
	if fired := e.HandleTransport(tempo.TransportStop); len(fired) != 1 || fired[0].RouteID != "end" {
		t.Errorf("on a stop the routes fired were %+v, want the Stop cue", fired)
	}
}

// TestALinkRouteFiresOnTheSessionsTransport: a route on the Link
// session fires as the session starts or stops.
func TestALinkRouteFiresOnTheSessionsTransport(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	e.SetRoutes([]Route{
		{ID: "on", Enabled: true, Link: LinkOnStart, Output: Output{Kind: ToTimer, TimerID: "t1", Command: TimerStart}},
		{ID: "off", Enabled: true, Link: LinkOnStop, Output: Output{Kind: ToTimer, TimerID: "t1", Command: TimerPause}},
		{ID: "asleep", Link: LinkOnStart, Output: Output{Kind: ToTimer, TimerID: "t1"}},
	})
	if fired := e.HandleLinkTransport(true); len(fired) != 1 || fired[0].RouteID != "on" {
		t.Errorf("as the session started the routes fired were %+v, want the one on start", fired)
	}
	if fired := e.HandleLinkTransport(false); len(fired) != 1 || fired[0].RouteID != "off" {
		t.Errorf("as the session stopped the routes fired were %+v, want the one on stop", fired)
	}
}

// TestALinkOutputMovesTheSession: a Link output passes its command on,
// and fires nothing while there is no session to move.
func TestALinkOutputMovesTheSession(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	route := Route{ID: "pad", Enabled: true, Output: Output{Kind: ToLink, Link: LinkToggle}}
	if _, ok := e.Fire(route, TriggerMessage(route, 127)); ok {
		t.Error("a Link output fired with no session to move")
	}
	var sent []LinkCommand
	e.SetRunLink(func(c LinkCommand) bool { sent = append(sent, c); return true })
	f, ok := e.Fire(route, TriggerMessage(route, 127))
	if !ok || f.Kind != FiredLink || !slices.Equal(sent, []LinkCommand{LinkToggle}) {
		t.Errorf("fired %+v, %v, and the session was sent %q", f, ok, sent)
	}
}

// TestAPatternIsHeldWhileItsNoteIs: a note on starts the pattern under a
// key, the note off for the same note lets go of that key, and a CC
// holds it as a sustain pedal does.
func TestAPatternIsHeldWhileItsNoteIs(t *testing.T) {
	var r recorder
	e := newTestEngine(&r)
	type call struct {
		action PatternAction
		key    string
	}
	var calls []call
	e.SetRunPattern(func(a PatternAction, id, key string) bool {
		calls = append(calls, call{a, key})
		return id == "p1"
	})
	pad := Route{ID: "pad", Enabled: true,
		Input:  Input{DeviceName: "XYZ", Channel: AnyChannel, Type: NoteOn, Data1: Span(36, 39)},
		Output: Output{Kind: ToPattern, PatternID: "p1", Pattern: PatternHold}}
	pedal := Route{ID: "pedal", Enabled: true,
		Input:  Input{DeviceName: "XYZ", Channel: 1, Type: CC, Data1: One(64)},
		Output: Output{Kind: ToPattern, PatternID: "p1", Pattern: PatternHold}}
	e.SetRoutes([]Route{pad, pedal})

	on := Message{Type: NoteOn, Channel: 10, Data1: 37, Data2: 100}
	off := Message{Type: NoteOff, Channel: 10, Data1: 37}
	other := Message{Type: NoteOff, Channel: 10, Data1: 38}
	e.HandleMessage("XYZ", on)
	e.HandleMessage("XYZ", other) // another pad of the span, never pressed
	e.HandleMessage("XYZ", off)
	e.HandleMessage("XYZ", ccMsg(1, 64, 127))
	e.HandleMessage("XYZ", ccMsg(1, 64, 0))

	want := []call{
		{PatternHold, "pad|10|37"},
		{PatternRelease, "pad|10|38"},
		{PatternRelease, "pad|10|37"},
		{PatternHold, "pedal|1|64"},
		{PatternRelease, "pedal|1|64"},
	}
	if !slices.Equal(calls, want) {
		t.Errorf("the pattern was asked\n%v\nwant\n%v", calls, want)
	}
}
