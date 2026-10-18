// Package fx holds the short, pre-programmed lighting moves a MIDI note
// can fire. It came from midittt, which crossroads takes over from.
//
// A Program is a list of Steps, each driving one fixture channel over
// time. Steps run on a shared clock that starts when the programme is
// triggered, so a step's delay positions it within the programme rather
// than relative to the step before it.
//
// A step is timed either in milliseconds or in beats of the selected
// tempo source. Beat timing tracks tempo changes mid-run and lets a
// fade span a musical length — a whole bar, a quarter note — whatever
// the tempo. The two are independent per step, so a programme can mix a
// fixed-time strobe with a beat-locked fade.
package fx

// A TimeBase says whether a step's delay and duration are read as
// milliseconds or as beats.
type TimeBase string

// The two time bases.
const (
	InMilliseconds TimeBase = "ms"
	InBeats        TimeBase = "beats"
)

// A Step drives one fixture channel over time.
type Step struct {
	ID string
	// FixtureID names the patched fixture, and Target is the one-based
	// channel offset within it.
	FixtureID string
	Target    int
	// TimeBase is the unit Delay and Duration are read in. Empty means
	// milliseconds, so files written before beat timing keep their
	// behaviour.
	TimeBase TimeBase
	// DelayMs is how long after the programme starts this step begins,
	// and DelayBeats the same in beats, used when TimeBase is InBeats.
	DelayMs, DelayBeats float64
	// From is the value the step starts at, used only when the duration
	// is above zero, and To the value it ends on.
	From, To int
	// DurationMs is how long the step takes to travel from From to To.
	// Zero snaps straight to To, which is how you set a value rather
	// than fade one. DurationBeats is the same in beats.
	DurationMs, DurationBeats float64
	// ScaleByVelocity scales From and To by the triggering note's
	// velocity, so hitting the pad at half force gives half the level.
	// Velocity 127 means full.
	ScaleByVelocity bool
}

// A Quantize aligns a triggered programme's start to the tempo grid.
type Quantize string

// The three alignments. With no tempo clock available, the alignment is
// ignored and the programme starts at once.
const (
	// QuantizeOff starts the programme immediately.
	QuantizeOff Quantize = "off"
	// QuantizeBeat waits for the next beat.
	QuantizeBeat Quantize = "beat"
	// QuantizeBar waits for the next bar, four beats.
	QuantizeBar Quantize = "bar"
)

// Quantizes are the alignments in the order a picker shows them.
var Quantizes = []Quantize{QuantizeOff, QuantizeBeat, QuantizeBar}

// A Program is one named lighting move.
type Program struct {
	ID    string
	Name  string
	Steps []Step
	// Quantize aligns the start to the beat grid. Empty means off.
	Quantize Quantize
}

// DisplayName is the programme's name, or a stand-in when it has none.
func (p Program) DisplayName() string {
	if p.Name == "" {
		return "Untitled FX"
	}
	return p.Name
}

// A Write is one channel a programme wants set at a given moment.
type Write struct {
	FixtureID string
	// Target is the one-based channel offset within the fixture.
	Target int
	// Value is the eight-bit DMX value.
	Value int
}
