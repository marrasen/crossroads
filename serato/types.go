package serato

// DeckCount is how many decks Serato reports.
const DeckCount = 4

// A Deck is one Serato deck's live state, as far as the subscribed
// feeds report it.
type Deck struct {
	// Index is the deck, counted from zero.
	Index    int
	Title    string
	Artist   string
	Filepath string
	// Loaded says a track is on the deck.
	Loaded bool
	// PositionSec is the playhead, in seconds.
	PositionSec float64
	Playing     bool
	// BPM is the analysed tempo.
	BPM float64
	// LoopBeats is the loop length in beats, 4, 8 and so on, and
	// AutoLoop says an auto-loop is running.
	LoopBeats float64
	AutoLoop  bool
	// Upfader is the channel fader level, zero to one.
	Upfader float64
}

// A Snapshot is the whole of what crossroads knows about Serato now.
type Snapshot struct {
	// Connected says a Serato connection is paired.
	Connected bool
	Decks     []Deck
	// Crossfader is the crossfader position, zero to one.
	Crossfader float64
}

// EmptySnapshot is a blank snapshot with the fixed number of decks.
func EmptySnapshot(connected bool) Snapshot {
	decks := make([]Deck, DeckCount)
	for i := range decks {
		decks[i] = Deck{Index: i}
	}
	return Snapshot{Connected: connected, Decks: decks, Crossfader: 0.5}
}

// Clone is a deep copy, so a snapshot handed to the interface is not
// changed under it by the next message off the socket.
func (s Snapshot) Clone() Snapshot {
	out := s
	out.Decks = append([]Deck(nil), s.Decks...)
	return out
}
