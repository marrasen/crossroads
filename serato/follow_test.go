package serato

import "testing"

// playing returns a snapshot with the named decks loaded, analysed and
// running, at the given crossfader.
func playing(crossfader float64, decks ...int) Snapshot {
	s := EmptySnapshot(true)
	s.Crossfader = crossfader
	for _, i := range decks {
		s.Decks[i].Loaded, s.Decks[i].Playing, s.Decks[i].BPM = true, true, 128
	}
	return s
}

func TestCrossfaderShare(t *testing.T) {
	for _, c := range []struct {
		deck       int
		crossfader float64
		want       float64
	}{
		{0, 0, 1}, {0, 1, 0}, {0, 0.5, 0.5},
		{1, 0, 0}, {1, 1, 1}, {1, 0.5, 0.5},
		// Decks 1 and 3 share the left, 2 and 4 the right.
		{2, 0, 1}, {3, 0, 0},
		// Out of range is brought back in.
		{0, -1, 1}, {0, 2, 0},
	} {
		if got := CrossfaderShare(c.deck, c.crossfader); got != c.want {
			t.Errorf("deck %d at crossfader %v has %v of it, want %v",
				c.deck, c.crossfader, got, c.want)
		}
	}
}

// TestFollowDeckTakesTheAudibleOne, which is what saves a DJ from the
// tempo picker mid-set.
func TestFollowDeckTakesTheAudibleOne(t *testing.T) {
	if got := FollowDeck(EmptySnapshot(false), -1); got != -1 {
		t.Errorf("a disconnected Serato follows deck %d, want none", got)
	}
	if got := FollowDeck(EmptySnapshot(true), -1); got != -1 {
		t.Errorf("an empty Serato follows deck %d, want none", got)
	}

	// One deck playing is the one, wherever the crossfader is.
	if got := FollowDeck(playing(1, 0), -1); got != 0 {
		t.Errorf("the only deck playing is %d, want 0", got)
	}

	// Two playing: the one the crossfader is on.
	if got := FollowDeck(playing(0, 0, 1), -1); got != 0 {
		t.Errorf("followed deck %d with the crossfader hard left, want 0", got)
	}
	if got := FollowDeck(playing(1, 0, 1), -1); got != 1 {
		t.Errorf("followed deck %d with the crossfader hard right, want 1", got)
	}

	// A deck that is not analysed cannot drive a beat clock.
	s := playing(1, 0, 1)
	s.Decks[1].BPM = 0
	if got := FollowDeck(s, -1); got != 0 {
		t.Errorf("followed deck %d, want the analysed one", got)
	}

	// Nor can one that is paused.
	s = playing(1, 0, 1)
	s.Decks[1].Playing = false
	if got := FollowDeck(s, -1); got != 0 {
		t.Errorf("followed deck %d, want the one that is running", got)
	}
}

// TestFollowDeckHoldsThroughABlend: in the middle of a blend both decks
// are audible, and that is exactly when the lights should stay put.
func TestFollowDeckHoldsThroughABlend(t *testing.T) {
	// Deck 1 is slightly louder, but not by enough to pull the follow
	// off deck 0.
	if got := FollowDeck(playing(0.55, 0, 1), 0); got != 0 {
		t.Errorf("followed deck %d part way through a blend, want to hold deck 0", got)
	}
	// Past the margin it moves.
	if got := FollowDeck(playing(0.75, 0, 1), 0); got != 1 {
		t.Errorf("followed deck %d once the blend was over, want deck 1", got)
	}
	// A deck that stops holds nothing.
	s := playing(0.55, 0, 1)
	s.Decks[0].Playing = false
	if got := FollowDeck(s, 0); got != 1 {
		t.Errorf("followed deck %d after the one it held stopped, want deck 1", got)
	}
}
