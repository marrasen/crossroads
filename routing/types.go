// Package routing matches incoming MIDI messages against the rules the
// user set up, and works out what each one should emit: midittt's
// routes, which crossroads plays as they were, in a MIDI routes module.
// It came from midittt, which crossroads takes over from.
package routing

import (
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/tempo"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// AnyChannel matches every MIDI channel. Channels are numbered from
// one, so zero is free to mean any.
const AnyChannel = 0

// A Data1Kind says how a matcher reads the note or CC number.
type Data1Kind uint8

// The three ways to match a number.
const (
	// Data1One matches one number.
	Data1One Data1Kind = iota
	// Data1Any matches every number.
	Data1Any
	// Data1Range matches an inclusive span, such as CC 10 through 18.
	Data1Range
)

// A Data1 is what a matcher accepts for the note or CC number.
type Data1 struct {
	Kind Data1Kind
	// Number is the one number Data1One matches.
	Number int
	// From and To are the inclusive bounds Data1Range matches.
	From, To int
}

// One matches a single number.
func One(n int) Data1 { return Data1{Kind: Data1One, Number: n} }

// Any matches every number.
func Any() Data1 { return Data1{Kind: Data1Any} }

// Span matches an inclusive range of numbers.
func Span(from, to int) Data1 { return Data1{Kind: Data1Range, From: from, To: to} }

// Matches reports whether an incoming number satisfies the matcher.
func (d Data1) Matches(value int) bool {
	switch d.Kind {
	case Data1Any:
		return true
	case Data1Range:
		return value >= d.From && value <= d.To
	default:
		return d.Number == value
	}
}

// First is the number a manual trigger uses: the single number, or the
// first of a span. It reports false for a matcher that names none.
func (d Data1) First() (int, bool) {
	switch d.Kind {
	case Data1Any:
		return 0, false
	case Data1Range:
		return d.From, true
	default:
		return d.Number, true
	}
}

// An Input says which incoming messages a route reacts to.
//
// DeviceName is the input device's name, so a configuration is portable
// across machines. Channel is counted from one, or AnyChannel. Data1 is
// the note or CC number, which may be one number, any, or a span that a
// whole-fixture route spreads across a fixture's channels.
type Input struct {
	DeviceName string
	Channel    int
	Type       Type
	Data1      Data1
}

// A ValueMode says how an output's value is worked out from the input.
type ValueMode string

// The three value modes.
const (
	// Fixed always emits the same value.
	Fixed ValueMode = "fixed"
	// Passthrough emits the incoming value, stretched to the output's
	// range where they differ.
	Passthrough ValueMode = "passthrough"
	// Scaled remaps the incoming range onto the output range.
	Scaled ValueMode = "scaled"
)

// ValueModes are the modes in the order a picker shows them.
var ValueModes = []ValueMode{Fixed, Passthrough, Scaled}

// A Value says how an output derives the value it sends.
//
// ScaleIn and ScaleOut always hold a range; the file reader fills in
// the defaults for an output that names none, so nothing downstream has
// to know what those defaults were.
type Value struct {
	Mode ValueMode
	// Fixed is the value Fixed mode emits.
	Fixed int
	// ScaleIn is the incoming range Scaled mode reads.
	ScaleIn [2]int
	// ScaleOut is the range Scaled mode writes.
	ScaleOut [2]int
	// Rectify folds the incoming value around RectifyCenter and
	// measures the distance from there, so both extremes read full and
	// the centre reads zero.
	//
	// This is for modulation sources that idle at their midpoint. An
	// LFO sent as control voltage is bipolar, so at zero depth it parks
	// at the centre — which drives an intensity channel to half rather
	// than to black. Rectifying makes zero depth mean dark, and any
	// deflection, either way, mean light.
	//
	// It applies to the input before the mode runs, so it works with
	// both passthrough and a scaled output range. DMX outputs only.
	Rectify bool
	// RectifyCenter is the input value treated as zero when rectifying.
	// Zero means DefaultRectifyCenter.
	RectifyCenter int
}

// A TimerCommand is what a route tells a timer.
type TimerCommand string

// The things a route tells a timer.
const (
	TimerStart TimerCommand = "start"
	TimerPause TimerCommand = "pause"
	TimerReset TimerCommand = "reset"
)

// An OutputKind says what a route does when it fires.
type OutputKind string

// The kinds of output.
const (
	// ToMidi emits a MIDI message.
	ToMidi OutputKind = "midi"
	// ToTimer drives a timer.
	ToTimer OutputKind = "timer"
	// ToDmx sets one raw slot of the universe, addressed absolutely.
	// Use it for a device that is not patched as a fixture; otherwise
	// prefer ToFixture, which survives re-addressing.
	ToDmx OutputKind = "dmx"
	// ToFixture sets channels of a patched fixture. The slot is worked
	// out from the fixture's start address at send time, so
	// re-addressing the fixture moves every route with it.
	ToFixture OutputKind = "fixture"
	// ToGroup drives one function across a whole fixture group,
	// addressed by role rather than by channel number. Each member
	// resolves the role to its own offset, so the same route works on
	// any rig whose fixtures declare that role, and members lacking it
	// are skipped.
	//
	// This is the portable form of ToFixture: nothing in it names a
	// fixture or a slot, so re-pointing the group at a venue's rig is
	// the only thing that has to change.
	ToGroup OutputKind = "group"
	// ToFx fires a pre-programmed lighting move. The triggering
	// message's value, a note velocity in practice, is passed to the
	// programme, so steps marked velocity-scaled follow how hard the
	// pad was hit.
	ToFx OutputKind = "fx"
	// ToLink starts, stops or toggles the Ableton Link session's
	// transport, which every peer with start/stop sync follows: a pad
	// that starts Live.
	ToLink OutputKind = "link"
	// ToPattern plays a pattern drawn on the beat grid: while a note is
	// held, or from one cue to another.
	ToPattern OutputKind = "pattern"
)

// OutputKinds are the kinds in the order a picker shows them.
var OutputKinds = []OutputKind{ToMidi, ToTimer, ToDmx, ToFixture, ToGroup, ToFx, ToLink, ToPattern}

// A PatternAction is what a ToPattern route does to its pattern.
type PatternAction string

// The things a ToPattern route can do.
const (
	// PatternHold plays the pattern while the note is held, and lets go
	// on its note off. A CC holds it at 64 and up, as a sustain pedal
	// does. Fired by anything else it starts the pattern.
	PatternHold PatternAction = "hold"
	// PatternStart, PatternStop and PatternToggle are for triggers that
	// have no release, such as a cue: start it at one, stop it at
	// another.
	PatternStart  PatternAction = "start"
	PatternStop   PatternAction = "stop"
	PatternToggle PatternAction = "toggle"
	// PatternRelease is what a note off sends; no route is set to it.
	PatternRelease PatternAction = "release"
)

// PatternActions are the actions a route can be set to, in the order a
// picker shows them.
var PatternActions = []PatternAction{PatternHold, PatternStart, PatternStop, PatternToggle}

// Valid reports whether a route can be set to the action.
func (a PatternAction) Valid() bool { return slices.Contains(PatternActions, a) }

// A LinkCommand is what a ToLink route does to the session's transport.
type LinkCommand string

// The commands a ToLink route sends.
const (
	LinkStart  LinkCommand = "start"
	LinkStop   LinkCommand = "stop"
	LinkToggle LinkCommand = "toggle"
)

// LinkCommands are the commands in the order a picker shows them.
var LinkCommands = []LinkCommand{LinkStart, LinkStop, LinkToggle}

// Valid reports whether a command is one the Link transport takes.
func (c LinkCommand) Valid() bool { return c == LinkStart || c == LinkStop || c == LinkToggle }

// A LinkEdge is which way the Ableton Link session's transport has to
// move for a route to fire.
type LinkEdge string

// The two ways the session's transport moves.
const (
	LinkOnStart LinkEdge = "start"
	LinkOnStop  LinkEdge = "stop"
)

// Valid reports whether an edge is one the session's transport crosses.
func (e LinkEdge) Valid() bool { return e == LinkOnStart || e == LinkOnStop }

// An Output is what a route does when it fires. Kind says which fields
// apply.
type Output struct {
	Kind OutputKind

	// DeviceName is the output device's name, for ToMidi. It is the
	// name rather than a port id, so a configuration is portable.
	DeviceName string
	// Channel is the outgoing MIDI channel, counted from one, and Type
	// and Data1 the message it sends, for ToMidi.
	Channel int
	Type    Type
	Data1   int

	// TimerID and Command name the timer and the control, for ToTimer.
	TimerID string
	Command TimerCommand

	// Slot is the absolute universe slot, 1 to 512, for ToDmx.
	Slot int

	// FixtureID names the patched fixture, and Target is the one-based
	// channel offset within it that is driven first, for ToFixture.
	FixtureID string
	Target    int

	// GroupID and Role name the group and the function, for ToGroup.
	GroupID string
	Role    fixtures.Role

	// Spread walks a matcher's span outwards from the first target.
	//
	// For ToFixture that is consecutive channels, so the span's second
	// number drives Target + 1 and so on, covering a whole fixture from
	// channel one, or any run within it. For ToGroup it is the group's
	// members in order, so the span's first number drives only the
	// first fixture — a chase along the bar.
	//
	// Off, every number in the span drives the one target: one channel,
	// or the role on every member at once as a single wash.
	Spread bool

	// ProgramID names the FX programme, for ToFx.
	ProgramID string

	// Link is what a ToLink route does to the session's transport.
	Link LinkCommand

	// PatternID and Pattern name the pattern and what is done to it, for
	// ToPattern.
	PatternID string
	Pattern   PatternAction

	// Value says how the value sent is worked out, for every kind that
	// sends one.
	Value Value
}

// A Beat fires a route from the tempo grid instead of a MIDI message:
// once every EveryBeats beats, on the beat picked by OffsetBeats.
//
// This is how a route runs to the selected tempo source — Link, a
// Serato deck, or the manual tap. A strobe on every beat, a scene
// change once a bar.
//
// The output fires at full value, as if a note were hit at velocity
// 127, so fixed and scaled outputs behave as they do on a manual
// trigger.
type Beat struct {
	// EveryBeats fires once every this many beats. One is every beat,
	// four is once a bar.
	EveryBeats int
	// OffsetBeats picks which beat of the interval fires, counted from
	// zero. Zero is the downbeat.
	OffsetBeats int
}

// A Route is one rule: what to listen for, and what to do about it.
type Route struct {
	ID      string
	Name    string
	Enabled bool
	// Input is the MIDI matcher. It is always there so the shape stays
	// portable, but it is ignored for matching when Beat is set — a
	// beat route never fires on MIDI.
	Input Input
	// Beat, when set, drives the route from the tempo grid rather than
	// from MIDI input.
	Beat    Beat
	HasBeat bool
	// Transport, when set, fires the route as the transport of the tempo
	// source driving the lights moves that way. It follows whichever
	// source is driving, so one route works on MIDI clock, Link, Serato,
	// the song or the manual tap. Only MIDI clock tells a start from a
	// continue, so a route on either of those fires only while it
	// drives; Play covers both, and every source.
	Transport tempo.TransportEdge
	// Link, when set, fires the route as the Ableton Link session's
	// transport starts or stops, whichever source drives the lights.
	Link LinkEdge
	// Cue, when set, fires the route as a song's playhead crosses a bar
	// and beat of it.
	Cue    *Cue
	Output Output
}

// A Cue is a place in a song: the song, named by its file, and the bar
// and beat of it, counted from one as a musician counts them.
//
// It is a bar and a beat rather than a time, so it stays on its bar
// when the song's grid is corrected.
type Cue struct {
	Song      string
	Bar, Beat int
	// Beats is how long the cue lasts, in beats, and zero for one that
	// is only a moment. A pattern cued for a span plays from its start
	// to its end, as a pad held that long would; anything else fires at
	// the start.
	Beats float64
}

// Triggered reports whether something other than MIDI fires the route:
// the beat grid, a transport, the Link session or a song cue.
func (r Route) Triggered() bool {
	return r.HasBeat || r.Transport != "" || r.Link != "" || r.Cue != nil
}

// DisplayName is the route's name, or a stand-in when it has none.
func (r Route) DisplayName() string {
	if r.Name == "" {
		return "Untitled route"
	}
	return r.Name
}

// Says is the route in a line: what fires it, and what it does.
func (r Route) Says() string {
	return r.When() + " → " + r.Does()
}

// When is what fires the route.
func (r Route) When() string {
	switch {
	case r.HasBeat:
		every := max(r.Beat.EveryBeats, 1)
		if every == 1 {
			return "every beat"
		}
		return "every " + strconv.Itoa(every) + " beats"
	case r.Transport != "":
		return "the transport's " + string(r.Transport)
	case r.Link != "":
		return "Link's " + string(r.Link)
	case r.Cue != nil:
		return r.Cue.Song + " bar " + strconv.Itoa(r.Cue.Bar) + " beat " + strconv.Itoa(r.Cue.Beat)
	}
	in := r.Input
	out := in.DeviceName + " " + in.Type.Label()
	switch in.Data1.Kind {
	case Data1One:
		out += " " + strconv.Itoa(in.Data1.Number)
	case Data1Range:
		out += " " + strconv.Itoa(in.Data1.From) + "–" + strconv.Itoa(in.Data1.To)
	case Data1Any:
	}
	if in.Channel != AnyChannel {
		out += " on channel " + strconv.Itoa(in.Channel)
	}
	return out
}

// Does is what the route does as it fires.
func (r Route) Does() string {
	o := r.Output
	switch o.Kind {
	case ToTimer:
		return string(o.Command) + " a timer"
	case ToDmx:
		return "channel " + strconv.Itoa(o.Slot)
	case ToFixture:
		return "a light's channel " + strconv.Itoa(o.Target)
	case ToGroup:
		return "a group's " + o.Role.Label()
	case ToFx:
		return "an effect programme"
	case ToLink:
		return string(o.Link) + " Link"
	case ToPattern:
		return string(o.Pattern) + " a pattern"
	case ToMidi:
	}
	return o.DeviceName + " " + o.Type.Label() + " " + strconv.Itoa(o.Data1)
}
