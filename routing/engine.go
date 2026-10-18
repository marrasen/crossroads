package routing

import (
	"math"
	"slices"
	"strconv"
	"sync"

	"github.com/marrasen/crossroads/tempo"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// maxBeatCatchup is the most beats HandleBeat replays for one late
// tick.
//
// Beyond this the jump is treated as a discontinuity — a source switch,
// a clock seek — and the grid starts again rather than firing a burst.
// It is generous next to a 25 ms sample at any sane tempo.
const maxBeatCatchup = 16

// DefaultRectifyCenter is the centre a rectifying route folds around
// when it names none.
const DefaultRectifyCenter = 64

// Matches reports whether an incoming message satisfies a route's
// matcher. A disabled route never matches, and neither does a
// beat-triggered one, which fires from the grid instead.
func Matches(r Route, m Message, deviceName string) bool {
	if !r.Enabled || r.Triggered() {
		return false
	}
	in := r.Input
	return in.DeviceName == deviceName &&
		in.Type == m.Type &&
		(in.Channel == AnyChannel || in.Channel == m.Channel) &&
		in.Data1.Matches(m.Data1)
}

// scale remaps value from one range onto another.
func scale(value float64, in, out [2]int) float64 {
	if in[1] == in[0] {
		return float64(out[0])
	}
	t := (value - float64(in[0])) / float64(in[1]-in[0])
	return float64(out[0]) + t*float64(out[1]-out[0])
}

// MidiValue is the second data byte a MIDI output sends for a message.
func MidiValue(v Value, m Message) int {
	switch v.Mode {
	case Fixed:
		return Clamp7(v.Fixed)
	case Scaled:
		return Clamp7f(scale(float64(m.Data2), v.ScaleIn, v.ScaleOut))
	default:
		return Clamp7(m.Data2)
	}
}

// Rectify is the distance of value from center, stretched back over the
// full 0 to 127 input range so a full swing either way still reads
// full.
//
// The two halves are rarely the same width — with the usual centre of
// 64 there are 64 steps below it and 63 above — so the wider half sets
// the scale. That makes the centre exactly zero at the cost of the
// shorter half falling a couple of steps shy of full, which is the
// right way round: a light that refuses to go black is far more
// noticeable than one that stops at 251.
func Rectify(value, center int) int {
	span := max(center, 127-center)
	if span <= 0 {
		return 0
	}
	return Clamp7f(math.Round(float64(abs(value-center)) * 127 / float64(span)))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// DmxValue is the eight-bit value a DMX output sends for a message.
//
// Passthrough maps 0 to 127 onto 0 to 255; scaled remaps the incoming
// range onto the output range. A rectifying output folds the input
// around its centre first, so both modes see the folded value.
func DmxValue(v Value, m Message) int {
	in := m.Data2
	if v.Rectify {
		center := v.RectifyCenter
		if center == 0 {
			center = DefaultRectifyCenter
		}
		in = Rectify(in, center)
	}
	switch v.Mode {
	case Fixed:
		return int(dmx.Clamp8(v.Fixed))
	case Scaled:
		return int(dmx.Clamp8f(scale(float64(in), v.ScaleIn, v.ScaleOut)))
	default:
		return int(dmx.MidiToDmx(in))
	}
}

// FixtureOffset is the channel of a fixture an incoming message drives.
//
// A spreading route walks forward from its start channel by the
// message's position within the matcher's span, so the span's first
// number is the start channel itself. Any other route drives its start
// channel alone.
func FixtureOffset(o Output, in Input, m Message) int {
	if !o.Spread {
		return o.Target
	}
	if in.Data1.Kind == Data1Range {
		return o.Target + m.Data1 - in.Data1.From
	}
	return o.Target
}

// A Write is one universe slot to set, and what to set it to.
type Write struct {
	Slot  int
	Value int
}

// A FixtureResult is what a fixture route works out for one message.
type FixtureResult struct {
	Slot  int
	Value int
	// Label names the fixture and channel, such as "Dome 1 · Intensity
	// Green", for the activity log.
	Label string
}

// TransformFixture works out the slot and value for a fixture route.
//
// It reports false when the fixture is no longer patched or the message
// lands outside its channels, so a stale route quietly does nothing
// rather than writing to an unrelated slot.
func TransformFixture(r Route, m Message, patches []fixtures.Patch, cat fixtures.Catalog) (FixtureResult, bool) {
	patch, ok := fixtures.FindPatch(patches, r.Output.FixtureID)
	if !ok {
		return FixtureResult{}, false
	}
	offset := FixtureOffset(r.Output, r.Input, m)
	slot, ok := cat.ResolveChannel(patch, offset)
	if !ok {
		return FixtureResult{}, false
	}
	return FixtureResult{
		Slot: slot,
		// The fixture's own dimmer curve is the last thing applied, so
		// it shapes whatever the route worked out rather than the other
		// way round.
		Value: cat.CurveValueAt(patch, offset, DmxValue(r.Output.Value, m)),
		Label: cat.ChannelLabel(patch, offset),
	}, true
}

// GroupMemberIndex is which member of a group an incoming message
// drives.
//
// A spreading route walks the membership list by the message's position
// within the matcher's span, so the span's first number is the first
// member. Any other route drives the whole group together, which it
// reports as false.
func GroupMemberIndex(o Output, in Input, m Message) (int, bool) {
	if !o.Spread {
		return 0, false
	}
	if in.Data1.Kind == Data1Range {
		return m.Data1 - in.Data1.From, true
	}
	return 0, true
}

// A GroupResult is what a group route works out for one message.
type GroupResult struct {
	// Label names the group and function, such as "Back wash · Red", or
	// adds the member when spreading.
	Label string
	// Value is the value before each member's dimmer curve, for the log.
	Value int
	// Writes is one entry per member that has the role, already curved.
	Writes []Write
}

// Slots is just the universe slots of the writes, for the log.
func (g GroupResult) Slots() []int {
	out := make([]int, len(g.Writes))
	for i, w := range g.Writes {
		out[i] = w.Slot
	}
	return out
}

// TransformGroup works out the slots and value for a group route, and
// reports false when the group no longer exists.
//
// Members are addressed by position, so a spreading route's second
// number always means the second fixture on the bar. A member that
// lacks the role, or whose address puts the channel outside the
// universe, contributes nothing — which is the whole point of targeting
// by role: the route stays valid on a rig of mixed fixtures instead of
// writing to an unrelated slot.
func TransformGroup(r Route, m Message, patches []fixtures.Patch, groups []fixtures.Group, cat fixtures.Catalog) (GroupResult, bool) {
	group, ok := fixtures.FindGroup(groups, r.Output.GroupID)
	if !ok {
		return GroupResult{}, false
	}
	members := fixtures.GroupMembers(group, patches)
	index, spreading := GroupMemberIndex(r.Output, r.Input, m)
	targeted := members
	if spreading {
		if index >= 0 && index < len(members) {
			targeted = members[index : index+1]
		} else {
			targeted = nil
		}
	}

	// Each member curves the value with its own settings, so a group
	// can mix fixtures that need different correction and still read
	// evenly.
	value := DmxValue(r.Output.Value, m)
	var writes []Write
	for _, patch := range targeted {
		resolved, ok := cat.ChannelForRole(patch, r.Output.Role)
		if !ok || !resolved.InUniverse {
			continue
		}
		writes = append(writes, Write{
			Slot:  resolved.Slot,
			Value: fixtures.CurveValue(patch, resolved.Channel, value),
		})
	}

	label := fixtures.GroupRoleLabel(group, r.Output.Role)
	if spreading {
		name := "member " + strconv.Itoa(index+1)
		if len(targeted) > 0 && targeted[0].Name != "" {
			name = targeted[0].Name
		}
		label += " · " + name
	}
	return GroupResult{Label: label, Value: value, Writes: writes}, true
}

// TransformDmx works out the slot and value for a raw DMX route.
func TransformDmx(r Route, m Message) Write {
	return Write{Slot: r.Output.Slot, Value: DmxValue(r.Output.Value, m)}
}

// Transform works out the bytes a MIDI route sends.
//
// The output's note or CC number comes from the action; the value comes
// from the value mode, and is left off for a type that carries none.
func Transform(r Route, m Message) (deviceName string, bytes []byte) {
	o := r.Output
	value := 0
	if o.Type.HasData2() {
		value = MidiValue(o.Value, m)
	}
	return o.DeviceName, Build(o.Type, o.Channel, o.Data1, value)
}

// TriggerMessage builds the message that fires a route by hand, with no
// hardware input. value, 127 for full, becomes the incoming value, so
// fixed modes emit their fixed value while passthrough and scaled emit
// from it.
func TriggerMessage(r Route, value int) Message {
	channel := r.Input.Channel
	if channel == AnyChannel {
		channel = 1
	}
	// A span triggers at its first number, which is a whole-fixture
	// route's first channel.
	number, ok := r.Input.Data1.First()
	if !ok && r.Output.Kind == ToMidi {
		number = r.Output.Data1
	}
	return Message{Type: r.Input.Type, Channel: channel, Data1: number, Data2: value}
}

// A FiredKind says what a route did when it fired.
type FiredKind string

// The kinds of thing a fired route reports.
const (
	FiredMidi    FiredKind = "midi"
	FiredTimer   FiredKind = "timer"
	FiredFx      FiredKind = "fx"
	FiredDmx     FiredKind = "dmx"
	FiredGroup   FiredKind = "group"
	FiredLink    FiredKind = "link"
	FiredPattern FiredKind = "pattern"
)

// A Fired says what one route did, for the activity log and for the
// flash on its card.
type Fired struct {
	Kind    FiredKind
	RouteID string

	// DeviceName and Bytes are what a MIDI route sent.
	DeviceName string
	Bytes      []byte

	// TimerID and Command are what a timer route drove.
	TimerID string
	Command TimerCommand

	// ProgramID and Velocity are what an FX route fired.
	ProgramID string
	Velocity  int

	// Slot and Value are what a DMX or fixture route wrote, and Label
	// names the fixture channel when there was one.
	Slot  int
	Value int
	Label string

	// Slots are the universe slots a group route wrote, one per member
	// that had the role.
	Slots []int
}

// The callbacks an Engine drives when a route fires. Keeping them
// injected is what lets the engine be tested without a MIDI port, a
// timer or a DMX widget anywhere near it.
type (
	// SendFn emits bytes on an output port.
	SendFn func(deviceName string, bytes []byte)
	// RunTimerFn applies a control to a timer.
	RunTimerFn func(timerID string, cmd TimerCommand)
	// SetDmxFn sets a universe slot.
	SetDmxFn func(slot, value int)
	// RunFxFn plays an FX programme at a velocity.
	RunFxFn func(programID string, velocity int)
	// RunLinkFn moves the Link session's transport, and reports false
	// when there is no session to move.
	RunLinkFn func(cmd LinkCommand) bool
	// RunPatternFn plays or lets go of a pattern under a key that ties a
	// note on to its note off, and reports false for a pattern not there
	// or a key not playing.
	RunPatternFn func(action PatternAction, patternID, key string) bool
)

// An Engine holds the active routes and dispatches messages to them.
// One input message may fan out to several routes.
//
// It has no side effects of its own: it calls the functions it was
// given rather than touching a MIDI port, the timer engine or the DMX
// widget directly.
type Engine struct {
	mu       sync.Mutex
	routes   []Route
	patches  []fixtures.Patch
	groups   []fixtures.Group
	cat      fixtures.Catalog
	lastBeat int
	seeded   bool

	send     SendFn
	runTimer RunTimerFn
	setDmx   SetDmxFn
	runFx    RunFxFn
	runLink  RunLinkFn
	runPat   RunPatternFn
}

// NewEngine makes an engine that drives the given outputs. A nil
// function means that kind of output does nothing.
func NewEngine(send SendFn, runTimer RunTimerFn, setDmx SetDmxFn, runFx RunFxFn) *Engine {
	if send == nil {
		send = func(string, []byte) {}
	}
	if runTimer == nil {
		runTimer = func(string, TimerCommand) {}
	}
	if setDmx == nil {
		setDmx = func(int, int) {}
	}
	if runFx == nil {
		runFx = func(string, int) {}
	}
	return &Engine{send: send, runTimer: runTimer, setDmx: setDmx, runFx: runFx,
		runLink: func(LinkCommand) bool { return false },
		runPat:  func(PatternAction, string, string) bool { return false }}
}

// SetRunPattern sets what a ToPattern route calls. Until it is set, such
// a route fires nothing.
func (e *Engine) SetRunPattern(fn RunPatternFn) {
	if fn == nil {
		fn = func(PatternAction, string, string) bool { return false }
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runPat = fn
}

// HoldKey ties a held pattern to the note that holds it: the route, and
// the channel and number the note came on, so two pads on one route are
// held and let go of apart.
func HoldKey(r Route, m Message) string {
	return r.ID + "|" + strconv.Itoa(m.Channel) + "|" + strconv.Itoa(m.Data1)
}

// holds reports whether a route holds its pattern while its note is
// down.
func holds(r Route) bool {
	return r.Output.Kind == ToPattern && r.Output.Pattern == PatternHold
}

// SetRunLink sets what a ToLink route calls. Until it is set, such a
// route fires nothing.
func (e *Engine) SetRunLink(fn RunLinkFn) {
	if fn == nil {
		fn = func(LinkCommand) bool { return false }
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runLink = fn
}

// SetRoutes replaces the rules the engine dispatches to.
func (e *Engine) SetRoutes(routes []Route) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// A copy: the caller edits its own slice in place, and the
	// engine reads this one on other goroutines.
	e.routes = slices.Clone(routes)
}

// SetFixtures replaces the patched fixtures, used to resolve fixture
// routes to universe slots.
func (e *Engine) SetFixtures(patches []fixtures.Patch) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.patches = slices.Clone(patches)
}

// SetCatalog replaces the types of light the fixtures are of.
func (e *Engine) SetCatalog(cat fixtures.Catalog) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cat = cat
}

// SetGroups replaces the fixture groups, used to fan a group route out
// over its members.
func (e *Engine) SetGroups(groups []fixtures.Group) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.groups = slices.Clone(groups)
}

// HandleMessage dispatches one parsed message from an input device, and
// reports what fired.
func (e *Engine) HandleMessage(deviceName string, m Message) []Fired {
	e.mu.Lock()
	routes, patches, groups, cat := e.routes, e.patches, e.groups, e.cat
	e.mu.Unlock()

	var fired []Fired
	for _, r := range routes {
		// A note off lets go of what its note on holds. It is not the
		// message the route matches, so it is looked for here.
		if m.Type == NoteOff && holds(r) && r.Input.Type == NoteOn {
			on := m
			on.Type = NoteOn
			if Matches(r, on, deviceName) {
				if f, ok := e.letGo(r, on); ok {
					fired = append(fired, f)
				}
			}
			continue
		}
		if !Matches(r, m, deviceName) {
			continue
		}
		if f, ok := e.fireRoute(r, m, patches, groups, cat); ok {
			fired = append(fired, f)
		}
	}
	return fired
}

// Release lets go of what a route holds, as its trigger ending would:
// the pattern a cue held for its span. A route that holds nothing does
// nothing.
func (e *Engine) Release(r Route) (Fired, bool) {
	if r.Output.Kind != ToPattern {
		return Fired{}, false
	}
	return e.letGo(r, TriggerMessage(r, 127))
}

// letGo lets go of the pattern a route holds under a note.
func (e *Engine) letGo(r Route, m Message) (Fired, bool) {
	e.mu.Lock()
	run := e.runPat
	e.mu.Unlock()
	if !run(PatternRelease, r.Output.PatternID, HoldKey(r, m)) {
		return Fired{}, false
	}
	return Fired{Kind: FiredPattern, RouteID: r.ID, ProgramID: r.Output.PatternID, Label: string(PatternRelease)}, true
}

// HandleBeat advances the beat grid to the continuous position beats
// and fires every beat route whose interval lands on a whole beat
// crossed since the last call.
//
// The caller samples the selected tempo clock on an interval. Passing
// the continuous position, rather than a tick count, lets a late or
// coarse tick still fire every beat it skipped over, and a stopped or
// rewound clock simply crosses nothing.
func (e *Engine) HandleBeat(beats float64) []Fired {
	e.mu.Lock()
	routes, patches, groups, cat := e.routes, e.patches, e.groups, e.cat
	current := int(math.Floor(beats))
	if !e.seeded {
		// Seed on the first sample, so beats before we started do not fire.
		e.lastBeat, e.seeded = current, true
		e.mu.Unlock()
		return nil
	}
	// Replaying a late tick's missed beats is intended, but a large
	// jump — a source switch, or a clock seek — must not fire a burst.
	// Past a small cap, start again and fire nothing.
	if current-e.lastBeat > maxBeatCatchup {
		e.lastBeat = current
		e.mu.Unlock()
		return nil
	}
	if current < e.lastBeat {
		// A fraction of a beat backwards is a drift correction, not a
		// seek: a network tempo source nudges its position both ways as
		// it re-anchors. Hold the grid there, or the beat just fired
		// would fire again on the way back over. A real rewind starts
		// again instead.
		if e.lastBeat-current > 1 {
			e.lastBeat = current
		}
		e.mu.Unlock()
		return nil
	}
	from, to := e.lastBeat+1, current
	e.lastBeat = current
	e.mu.Unlock()

	var fired []Fired
	for b := from; b <= to; b++ {
		for _, r := range routes {
			if !r.Enabled || !r.HasBeat {
				continue
			}
			every := max(1, r.Beat.EveryBeats)
			offset := ((r.Beat.OffsetBeats % every) + every) % every
			if ((b-offset)%every+every)%every != 0 {
				continue
			}
			// A beat trigger has no MIDI input, so fire at full value.
			if f, ok := e.fireRoute(r, TriggerMessage(r, 127), patches, groups, cat); ok {
				fired = append(fired, f)
			}
		}
	}
	return fired
}

// HandleTransport fires every route waiting on the driving tempo
// source's transport moving this way, and reports what fired. The
// caller reports the move, so it is called only when one happened.
func (e *Engine) HandleTransport(edge tempo.TransportEdge) []Fired {
	return e.fireEach(func(r Route) bool { return r.Transport.Covers(edge) })
}

// HandleLinkTransport fires every route waiting on the Ableton Link
// session's transport starting or stopping, and reports what fired. The
// caller spots the move, so it is called only when one happened.
func (e *Engine) HandleLinkTransport(playing bool) []Fired {
	edge := LinkOnStop
	if playing {
		edge = LinkOnStart
	}
	return e.fireEach(func(r Route) bool { return r.Link == edge })
}

// HandleCues fires every route cued in song whose cue crossed reports
// the playhead has just passed, and reports what fired. The caller
// knows the song's grid, so it is the one that says where a cue falls.
func (e *Engine) HandleCues(song string, crossed func(Cue) bool) []Fired {
	return e.fireEach(func(r Route) bool { return r.Cue != nil && r.Cue.Song == song && crossed(*r.Cue) })
}

// fireEach fires every enabled route want picks, at full value, since
// nothing about a transport says how hard.
func (e *Engine) fireEach(want func(Route) bool) []Fired {
	e.mu.Lock()
	routes, patches, groups, cat := e.routes, e.patches, e.groups, e.cat
	e.mu.Unlock()
	var fired []Fired
	for _, r := range routes {
		if !r.Enabled || !want(r) {
			continue
		}
		if f, ok := e.fireRoute(r, TriggerMessage(r, 127), patches, groups, cat); ok {
			fired = append(fired, f)
		}
	}
	return fired
}

// ResetBeat forgets the last beat position, so the next HandleBeat
// seeds again.
func (e *Engine) ResetBeat() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seeded = false
}

// Fire runs one route's output by hand and reports what it did. It is
// what the "fire now" button drives.
func (e *Engine) Fire(r Route, m Message) (Fired, bool) {
	e.mu.Lock()
	patches, groups, cat := e.patches, e.groups, e.cat
	e.mu.Unlock()
	return e.fireRoute(r, m, patches, groups, cat)
}

// fireRoute runs one route's output for a message and reports what it did.
// It reports false when the route targets a fixture or group that no
// longer resolves. MIDI and beat triggers share it, so both drive
// outputs identically.
func (e *Engine) fireRoute(r Route, m Message, patches []fixtures.Patch, groups []fixtures.Group, cat fixtures.Catalog) (Fired, bool) {
	switch r.Output.Kind {
	case ToTimer:
		e.runTimer(r.Output.TimerID, r.Output.Command)
		return Fired{Kind: FiredTimer, RouteID: r.ID, TimerID: r.Output.TimerID, Command: r.Output.Command}, true

	case ToDmx:
		w := TransformDmx(r, m)
		e.setDmx(w.Slot, w.Value)
		return Fired{Kind: FiredDmx, RouteID: r.ID, Slot: w.Slot, Value: w.Value}, true

	case ToPattern:
		action := r.Output.Pattern
		// A CC holds as a sustain pedal does: down at 64 and up.
		if action == PatternHold && m.Type == CC && m.Data2 < 64 {
			return e.letGo(r, m)
		}
		e.mu.Lock()
		run := e.runPat
		e.mu.Unlock()
		if !run(action, r.Output.PatternID, HoldKey(r, m)) {
			return Fired{}, false
		}
		return Fired{Kind: FiredPattern, RouteID: r.ID, ProgramID: r.Output.PatternID, Label: string(action)}, true

	case ToLink:
		e.mu.Lock()
		run := e.runLink
		e.mu.Unlock()
		if !run(r.Output.Link) {
			return Fired{}, false // Link is off, so there is nothing to move
		}
		return Fired{Kind: FiredLink, RouteID: r.ID, Label: string(r.Output.Link)}, true

	case ToFx:
		e.runFx(r.Output.ProgramID, m.Data2)
		return Fired{Kind: FiredFx, RouteID: r.ID, ProgramID: r.Output.ProgramID, Velocity: m.Data2}, true

	case ToFixture:
		res, ok := TransformFixture(r, m, patches, cat)
		if !ok { // an unpatched fixture, or a number outside its span
			return Fired{}, false
		}
		e.setDmx(res.Slot, res.Value)
		return Fired{Kind: FiredDmx, RouteID: r.ID, Slot: res.Slot, Value: res.Value, Label: res.Label}, true

	case ToGroup:
		res, ok := TransformGroup(r, m, patches, groups, cat)
		if !ok { // a deleted group
			return Fired{}, false
		}
		for _, w := range res.Writes {
			e.setDmx(w.Slot, w.Value)
		}
		return Fired{Kind: FiredGroup, RouteID: r.ID, Label: res.Label, Value: res.Value, Slots: res.Slots()}, true

	default:
		device, bytes := Transform(r, m)
		e.send(device, bytes)
		return Fired{Kind: FiredMidi, RouteID: r.ID, DeviceName: device, Bytes: bytes}, true
	}
}
