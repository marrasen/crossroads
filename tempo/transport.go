package tempo

// A TransportEdge is a way a tempo source's transport moves.
//
// Play is a transport that began without saying how. MIDI clock is the
// one source that tells a Start, which goes back to the top, from a
// Continue, which plays on from where it stopped: Ableton Link, Serato,
// the song and the manual tap report a transport that is running or one
// that is not, and nothing more.
type TransportEdge string

// The ways a transport moves.
const (
	TransportPlay     TransportEdge = "play"
	TransportStart    TransportEdge = "start"
	TransportContinue TransportEdge = "continue"
	TransportStop     TransportEdge = "stop"
)

// Valid reports whether an edge is one a transport crosses, and so one
// a route can be set to fire on.
func (e TransportEdge) Valid() bool {
	switch e {
	case TransportPlay, TransportStart, TransportContinue, TransportStop:
		return true
	}
	return false
}

// Began reports whether an edge is the transport beginning rather than
// ending.
func (e TransportEdge) Began() bool {
	return e == TransportPlay || e == TransportStart || e == TransportContinue
}

// Covers reports whether something waiting on this edge fires on one
// that happened.
//
// Play covers every way a transport begins, so a cue on Play runs
// whether the source said start, said continue, or said nothing beyond
// having begun. Start and Continue cover only themselves, which is what
// makes them worth picking.
func (e TransportEdge) Covers(got TransportEdge) bool {
	if e == TransportPlay {
		return got.Began()
	}
	return e == got && e.Valid()
}
