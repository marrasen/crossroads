package tempo

import "math"

// LinkSourceID names the Ableton Link session as a tempo source, and
// LinkSourceLabel is what the picker calls it.
const (
	LinkSourceID    = "link"
	LinkSourceLabel = "Ableton Link"
)

// A LinkSnapshot is one reading of the Link session.
type LinkSnapshot struct {
	// BPM is the session tempo.
	BPM float64
	// Beat is the continuous beat count on the session timeline,
	// relative to the quantum.
	Beat float64
	// Phase is the position within the quantum, from zero up to it.
	Phase float64
	// Peers is how many other Link applications are in the session.
	Peers int
	// Playing is the session transport, which only means anything with
	// start and stop sync.
	Playing bool
	// Quantum is the beats per bar the beat and phase were read with.
	Quantum float64
	// ReadAtMs is the monotonic time the session was read at.
	ReadAtMs float64
}

// LinkState is everything the interface knows about Link.
type LinkState struct {
	// Available is false when no session can run at all, so the panel
	// says so instead of lying.
	Available bool
	Enabled   bool
	Quantum   float64
	// Snapshot is the latest reading, and Reading is false while Link
	// is off or has not reported yet.
	Snapshot LinkSnapshot
	Reading  bool
	// Err carries the reason a session failed, when one did.
	Err string
}

// ReanchorBeats is how far the local projection may drift from Link
// before the clock is anchored again, in beats. 0.02 beats is 10 ms at
// 120 BPM — below anything visible in a light cue, and well above the
// jitter of a ten-times-a-second reading.
const ReanchorBeats = 0.02

// LinkClock turns a Link reading into a Clock, or reports false to keep
// the one already held.
//
// atMs is the monotonic time the snapshot belongs at. Taking that
// anchor on every snapshot would push the reader's jitter straight into
// the beat position, so instead the existing anchor keeps projecting,
// and only a tempo change, a transport change or real drift past
// ReanchorBeats moves it. Reporting false for the common case also
// keeps the Registry quiet, so the interface is not redrawn ten times a
// second for nothing.
//
// followStartStop off treats the session as always playing. Start and
// stop sync is optional in Link and plenty of peers, Serato among them,
// never send it, so a user with a running session would otherwise see
// nothing move.
func LinkClock(prev Clock, havePrev bool, s LinkSnapshot, atMs float64, followStartStop bool) (Clock, bool) {
	// A malformed reading would poison every consumer projecting from it.
	if !(s.BPM > 0) || math.IsNaN(s.Beat) || math.IsInf(s.Beat, 0) {
		return Clock{}, false
	}
	playing := s.Playing || !followStartStop
	next := Clock{
		SourceID:  LinkSourceID,
		BPM:       s.BPM,
		BeatAtRef: s.Beat,
		RefTimeMs: atMs,
		Playing:   playing,
	}
	if !havePrev {
		return next, true
	}
	if prev.BPM != s.BPM || prev.Playing != playing {
		return next, true
	}
	// A stopped clock holds its position, so there is nothing to drift.
	if !playing {
		return Clock{}, false
	}
	if math.Abs(BeatsAt(prev, atMs)-s.Beat) > ReanchorBeats {
		return next, true
	}
	return Clock{}, false
}
