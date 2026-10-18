package serato

import "encoding/hex"

// This file holds the External Control specifics decoded in
// docs/soundswitch-micro-research.md: the mDNS service, the handshake
// messages, the feeds crossroads subscribes to, and how a /Status message
// updates a deck.

// ServiceType is the Bonjour service Serato browses for, which a client
// advertises. The full name adds the underscore and the transport.
const ServiceType = "_SeratoIOSRemote._tcp"

// Trailer is Serato's per-connection session id, which replies echo. It
// is constant.
var Trailer = mustHex("4caac2ae35b1c476db5a644403bd4170")

// Subscriptions are the feeds crossroads registers for after pairing.
var Subscriptions = []string{
	"/Register/Status/Deck/Song/Title",
	"/Register/Status/Deck/Playhead",
	"/Register/Status/Deck/Song/Artist",
	"/Register/Status/Deck/Song/Valid",
	"/Register/Status/Deck/Song/Filepath",
	"/Register/Status/Video/Deck/Mixer/Upfader",
	"/Register/Status/Video/Mixer/Crossfader",
	"/Register/Status/Deck/Loop/BeatLength",
	"/Register/Status/Deck/Loop/AutoLoopOn",
	"/Register/Status/Deck/Loop/LoopRollOn",
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// deckOf is the deck a message names, clamped to one that exists.
func deckOf(m Message) (int, bool) {
	if !m.IsNumber(0) {
		return 0, false
	}
	d := m.Int(0)
	if d < 0 || d >= DeckCount {
		return 0, false
	}
	return d, true
}

// ApplyStatus folds one /Status message into a snapshot and reports
// whether it changed anything. An address it does not know is ignored,
// so a new Serato feed does nothing rather than failing.
func ApplyStatus(s *Snapshot, m Message) bool {
	// The crossfader is the one deck-less feed.
	if m.Address == "/Status/Video/Mixer/Crossfader" {
		if !m.IsNumber(0) {
			return false
		}
		s.Crossfader = m.Float(0)
		return true
	}

	index, ok := deckOf(m)
	if !ok {
		return false
	}
	d := &s.Decks[index]
	switch m.Address {
	case "/Status/Deck/Song/Title":
		d.Title = m.Str(1)
	case "/Status/Deck/Song/Artist":
		d.Artist = m.Str(1)
	case "/Status/Deck/Song/Filepath":
		d.Filepath = m.Str(1)
	case "/Status/Deck/Song/Valid":
		d.Loaded = m.Int(1) != 0
	case "/Status/Deck/Playhead":
		// The arguments are the deck, the position, whether it plays,
		// and the tempo.
		d.PositionSec = m.Float(1)
		d.Playing = m.Float(2) != 0
		d.BPM = m.Float(3)
	case "/Status/Deck/Loop/BeatLength":
		d.LoopBeats = m.Float(1)
	case "/Status/Deck/Loop/AutoLoopOn":
		d.AutoLoop = m.Int(1) != 0
	case "/Status/Video/Deck/Mixer/Upfader":
		d.Upfader = m.Float(1)
	default:
		return false // LoopRollOn and the like: subscribed, not modelled
	}
	return true
}
