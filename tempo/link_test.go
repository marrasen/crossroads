package tempo

import (
	"math"
	"testing"
)

func snap(over func(*LinkSnapshot)) LinkSnapshot {
	s := LinkSnapshot{BPM: 120, Peers: 1, Playing: true, Quantum: 4}
	if over != nil {
		over(&s)
	}
	return s
}

// first is the clock a snapshot makes with nothing held yet.
func first(t *testing.T, s LinkSnapshot, atMs float64, follow bool) Clock {
	t.Helper()
	c, ok := LinkClock(Clock{}, false, s, atMs, follow)
	if !ok {
		t.Fatalf("the first snapshot made no clock")
	}
	return c
}

func TestLinkClockAnchorsTheFirstSnapshot(t *testing.T) {
	got := first(t, snap(func(s *LinkSnapshot) { s.Beat = 8.5 }), 1000, true)
	want := Clock{SourceID: LinkSourceID, BPM: 120, BeatAtRef: 8.5, RefTimeMs: 1000, Playing: true}
	if got != want {
		t.Errorf("clock = %+v, want %+v", got, want)
	}
}

func TestLinkClockKeepsAMatchingAnchor(t *testing.T) {
	prev := first(t, snap(nil), 1000, true)
	// 500 ms later at 120 BPM is exactly one beat on.
	if _, ok := LinkClock(prev, true, snap(func(s *LinkSnapshot) { s.Beat = 1 }), 1500, true); ok {
		t.Error("a matching projection was re-anchored")
	}
	// 5 ms of lateness is 0.01 beats at 120 BPM, under the threshold.
	if _, ok := LinkClock(prev, true, snap(func(s *LinkSnapshot) { s.Beat = 1.01 }), 1500, true); ok {
		t.Error("jitter below the threshold was re-anchored")
	}
}

func TestLinkClockReanchorsOnRealDrift(t *testing.T) {
	prev := first(t, snap(nil), 1000, true)
	got, ok := LinkClock(prev, true, snap(func(s *LinkSnapshot) { s.Beat = 1.5 }), 1500, true)
	if !ok || got.BeatAtRef != 1.5 || got.RefTimeMs != 1500 {
		t.Errorf("clock = %+v, %v", got, ok)
	}
}

func TestLinkClockReanchorsOnATempoChange(t *testing.T) {
	prev := first(t, snap(nil), 1000, true)
	got, ok := LinkClock(prev, true, snap(func(s *LinkSnapshot) { s.BPM, s.Beat = 128, 1 }), 1500, true)
	if !ok || got.BPM != 128 || got.BeatAtRef != 1 || got.RefTimeMs != 1500 {
		t.Errorf("clock = %+v, %v", got, ok)
	}
}

func TestLinkClockReanchorsOnTransport(t *testing.T) {
	prev := first(t, snap(nil), 1000, true)
	stopped, ok := LinkClock(prev, true, snap(func(s *LinkSnapshot) { s.Beat, s.Playing = 1, false }), 1500, true)
	if !ok || stopped.Playing || stopped.BeatAtRef != 1 {
		t.Fatalf("stopping gave %+v, %v", stopped, ok)
	}
	// Link's beat keeps running while stopped, so resuming has to anchor
	// to it rather than carry on from where the held clock was left.
	resumed, ok := LinkClock(stopped, true, snap(func(s *LinkSnapshot) { s.Beat = 9 }), 2000, true)
	if !ok || !resumed.Playing || resumed.BeatAtRef != 9 || resumed.RefTimeMs != 2000 {
		t.Errorf("resuming gave %+v, %v", resumed, ok)
	}
}

func TestLinkClockHoldsWhileStopped(t *testing.T) {
	prev := Clock{SourceID: LinkSourceID, BPM: 120, BeatAtRef: 4, RefTimeMs: 1000}
	// Link's beat has moved on, but a stopped clock has nothing to correct.
	if _, ok := LinkClock(prev, true, snap(func(s *LinkSnapshot) { s.Beat, s.Playing = 12, false }), 5000, true); ok {
		t.Error("a stopped clock was re-anchored")
	}
	if got := BeatsAt(prev, 5000); got != 4 {
		t.Errorf("a stopped clock read %v, want 4", got)
	}
}

func TestLinkClockIgnoresTransportWhenNotFollowing(t *testing.T) {
	got := first(t, snap(func(s *LinkSnapshot) { s.Playing = false }), 1000, false)
	if !got.Playing {
		t.Error("the session was treated as stopped without follow-start/stop")
	}
}

func TestLinkClockReanchorsWhenFollowingIsTurnedOn(t *testing.T) {
	prev := first(t, snap(func(s *LinkSnapshot) { s.Playing = false }), 1000, false)
	if !prev.Playing {
		t.Fatal("the held clock should have been playing")
	}
	got, ok := LinkClock(prev, true, snap(func(s *LinkSnapshot) { s.Beat, s.Playing = 1, false }), 1500, true)
	if !ok || got.Playing {
		t.Errorf("clock = %+v, %v; want it stopped", got, ok)
	}
}

func TestLinkClockRejectsNonsense(t *testing.T) {
	prev := first(t, snap(nil), 1000, true)
	bad := []LinkSnapshot{
		snap(func(s *LinkSnapshot) { s.BPM = 0 }),
		snap(func(s *LinkSnapshot) { s.Beat = math.NaN() }),
		snap(func(s *LinkSnapshot) { s.BPM = -1 }),
	}
	for _, s := range bad {
		if _, ok := LinkClock(prev, true, s, 1500, true); ok {
			t.Errorf("a nonsensical reading %+v was taken", s)
		}
		if _, ok := LinkClock(Clock{}, false, s, 1500, true); ok {
			t.Errorf("a nonsensical first reading %+v was taken", s)
		}
	}
}
