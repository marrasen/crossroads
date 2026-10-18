package serato

import "math"

// Picking which Serato deck the lights run to, so a DJ never has to touch the
// tempo picker mid-set.
//
// Audibility is judged from the crossfader alone. The upfader is deliberately
// ignored: DJs use it to gain-match tracks, so a quiet track turned up would
// otherwise look more audible than a loud one turned down, and a deck's fader
// level says nothing about which track is in front.

// FollowMargin is how much more of the crossfader a rival deck needs before the
// follow moves. Without it the lights would flip back and forth around the
// mid-point of every blend, which is exactly when they should stay put.
const FollowMargin = 0.15

// CrossfaderShare is a deck's share of the crossfader, 0 when faded out and 1
// when fully in.
//
// Serato sends one 0-to-1 crossfader value and never says which side a deck is
// assigned to. The /DeckAssign feed exists in the protocol but is not one we
// subscribe to, so this assumes the stock layout: decks 1 and 3 on the left,
// 2 and 4 on the right, with 0-based indices here.
func CrossfaderShare(deck int, crossfader float64) float64 {
	// A crossfader we have never received reads 0.5, which ties every deck and
	// leaves the choice to play state and hysteresis — the right default.
	c := crossfader
	if math.IsNaN(c) {
		c = 0.5
	}
	c = math.Min(1, math.Max(0, c))
	if deck%2 == 0 {
		return 1 - c
	}
	return c
}

// FollowDeck returns the deck to follow, or -1 when nothing is audible.
// current is the deck being followed now, or -1, and is kept unless a rival is
// clearly louder.
func FollowDeck(snap Snapshot, current int) int {
	if !snap.Connected {
		return -1
	}
	// Only a deck that is loaded, running and analysed can drive a beat clock.
	var candidates []*Deck
	for i := range snap.Decks {
		if d := &snap.Decks[i]; d.Loaded && d.Playing && d.BPM > 0 {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) == 0 {
		return -1
	}

	best := candidates[0]
	bestShare := CrossfaderShare(best.Index, snap.Crossfader)
	for _, d := range candidates {
		if share := CrossfaderShare(d.Index, snap.Crossfader); share > bestShare {
			best, bestShare = d, share
		}
	}

	// Hold the current deck through the middle of a blend, where both decks are
	// audible and the leader flickers.
	for _, d := range candidates {
		if d.Index == current && CrossfaderShare(d.Index, snap.Crossfader) >= bestShare-FollowMargin {
			return d.Index
		}
	}
	return best.Index
}
