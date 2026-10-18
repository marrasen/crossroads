package serato

import (
	"math"
	"testing"
)

func msg(address, types string, args ...Arg) Message {
	return Message{Address: address, Types: types, Args: args}
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-4 }

func TestApplyStatusFillsADeck(t *testing.T) {
	s := EmptySnapshot(true)
	ApplyStatus(&s, msg("/Status/Deck/Song/Title", "is", int32(0), "The Winner Takes It All"))
	ApplyStatus(&s, msg("/Status/Deck/Song/Artist", "is", int32(0), "ABBA"))
	ApplyStatus(&s, msg("/Status/Deck/Song/Valid", "if", int32(0), float32(1)))
	ApplyStatus(&s, msg("/Status/Deck/Playhead", "ifff", int32(0), float32(16.9), float32(1), float32(126.84)))
	d := s.Decks[0]
	if d.Title != "The Winner Takes It All" || d.Artist != "ABBA" || !d.Loaded {
		t.Errorf("deck = %+v", d)
	}
	if !near(d.PositionSec, 16.9) || !d.Playing || !near(d.BPM, 126.84) {
		t.Errorf("deck = %+v", d)
	}
}

func TestApplyStatusReadsTheStopFlag(t *testing.T) {
	s := EmptySnapshot(true)
	ApplyStatus(&s, msg("/Status/Deck/Playhead", "ifff", int32(1), float32(30), float32(1), float32(120)))
	ApplyStatus(&s, msg("/Status/Deck/Playhead", "ifff", int32(1), float32(30), float32(0), float32(120)))
	if s.Decks[1].Playing || !near(s.Decks[1].PositionSec, 30) {
		t.Errorf("deck = %+v", s.Decks[1])
	}
}

func TestApplyStatusTracksLoopAndFaders(t *testing.T) {
	s := EmptySnapshot(true)
	ApplyStatus(&s, msg("/Status/Deck/Loop/BeatLength", "if", int32(0), float32(8)))
	ApplyStatus(&s, msg("/Status/Deck/Loop/AutoLoopOn", "if", int32(0), float32(1)))
	ApplyStatus(&s, msg("/Status/Video/Deck/Mixer/Upfader", "if", int32(0), float32(0.75)))
	ApplyStatus(&s, msg("/Status/Video/Mixer/Crossfader", "f", float32(0.09)))
	d := s.Decks[0]
	if d.LoopBeats != 8 || !d.AutoLoop || !near(d.Upfader, 0.75) || !near(s.Crossfader, 0.09) {
		t.Errorf("deck = %+v, crossfader = %v", d, s.Crossfader)
	}
}

func TestApplyStatusIgnoresWhatItDoesNotKnow(t *testing.T) {
	s := EmptySnapshot(true)
	if ApplyStatus(&s, msg("/Status/Deck/Loop/LoopRollOn", "if", int32(0), float32(1))) {
		t.Error("an unmodelled feed reported a change")
	}
	if ApplyStatus(&s, msg("/Status/Deck/Song/Title", "is", int32(9), "x")) {
		t.Error("a deck that does not exist reported a change")
	}
}
