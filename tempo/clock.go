// Package tempo holds the musical clock, independent of where the tempo
// comes from.
//
// crossroads takes tempo from several producers — Ableton Link, a Serato
// deck, or a manual tap — and the lighting side should not care which.
// Every producer publishes the same Clock, and the FX and routing
// engines read it.
//
// A Clock is a snapshot rather than a running value: it records where
// the beat was at one instant and how fast it is moving, so any
// consumer can project the beat forward from it with BeatsAt. That
// keeps producers cheap, since they only push when tempo or transport
// changes, and lets a 25 ms FX tick and a routing check read a
// consistent position without everyone sharing a heartbeat.
//
// It came from midittt, which crossroads takes over from.
package tempo

import (
	"math"
	"time"
)

// A Clock is one reading of a tempo source.
type Clock struct {
	// SourceID says which producer this came from.
	SourceID string
	// BPM is the tempo in beats per minute.
	BPM float64
	// BeatAtRef is the continuous beat count, not wrapped to a bar, at
	// RefTimeMs.
	BeatAtRef float64
	// RefTimeMs is the local monotonic time, in milliseconds, at which
	// BeatAtRef held.
	RefTimeMs float64
	// Playing says whether the beat is advancing. A stopped clock holds
	// BeatAtRef.
	Playing bool
	// Transport is how the transport moved to reach this snapshot, for
	// a source that knows more than Playing shows: a MIDI Start goes
	// back to the top and a Continue plays on, and both end with the
	// transport running.
	//
	// It is empty for a source that cannot say, and the registry then
	// works the move out from Playing changing. It describes one move
	// rather than a state, so nothing projects from it.
	Transport TransportEdge
}

// A Source is a tempo producer the user can rank.
type Source struct {
	ID    string
	Label string
	// Connected says the source is registered now, so it can drive.
	Connected bool
}

// ManualSourceID names the built-in manual tap, which is always there
// so the rest works with no producer connected.
const ManualSourceID = "manual"

var start = time.Now()

// Now is the monotonic millisecond clock the tempo maths runs on.
func Now() float64 { return float64(time.Since(start)) / float64(time.Millisecond) }

// AtMs is where an instant falls on that clock, for a producer handed a
// [time.Time] — a MIDI driver stamping a message as it arrives.
func AtMs(t time.Time) float64 { return float64(t.Sub(start)) / float64(time.Millisecond) }

// TimeAt is the instant ms falls at on that clock, for handing a moment
// to something that keeps time in [time.Time]: a MIDI port that holds a
// message until then.
func TimeAt(ms float64) time.Time { return start.Add(time.Duration(ms * float64(time.Millisecond))) }

// MsPerBeat is how many milliseconds a beat lasts at a tempo. A tempo
// of zero or less gives infinity, which every caller treats as "no
// tempo" rather than dividing by it.
func MsPerBeat(bpm float64) float64 {
	if bpm > 0 {
		return 60000 / bpm
	}
	return math.Inf(1)
}

// BeatsAt is the continuous beat count the clock reads at local time
// atMs.
//
// A playing clock advances from its snapshot at its tempo; a stopped
// one holds. Projecting from a snapshot, rather than storing a live
// value, is what lets a producer stay quiet between tempo changes.
func BeatsAt(c Clock, atMs float64) float64 {
	if !c.Playing {
		return c.BeatAtRef
	}
	return c.BeatAtRef + (atMs-c.RefTimeMs)/MsPerBeat(c.BPM)
}

// Phase is the position within a repeating grid of quantum beats, in
// the half-open range from zero to quantum. With a quantum of 4, a bar
// of 4/4, this is the beat within the bar: zero on the downbeat, 3.5 on
// the "and" of four.
func Phase(beats, quantum float64) float64 {
	if quantum <= 0 {
		return 0
	}
	return math.Mod(math.Mod(beats, quantum)+quantum, quantum)
}

// BeatsUntilBoundary is how many beats from beats to the next multiple
// of quantum. It is zero exactly on a boundary, so a caller quantising
// a start fires at once when it is already aligned rather than waiting
// a whole grid.
func BeatsUntilBoundary(beats, quantum float64) float64 {
	if quantum <= 0 {
		return 0
	}
	p := Phase(beats, quantum)
	if p == 0 {
		return 0
	}
	return quantum - p
}
