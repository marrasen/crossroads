package tempo

import (
	"slices"
	"testing"
	"time"
)

// edges records every transport move a registry reports.
func edges(r *Registry) *[]TransportEdge {
	var got []TransportEdge
	r.SubscribeTransport(func(e TransportEdge) { got = append(got, e) })
	return &got
}

func TestPlayCoversEveryWayATransportBegins(t *testing.T) {
	for _, got := range []TransportEdge{TransportPlay, TransportStart, TransportContinue} {
		if !TransportPlay.Covers(got) {
			t.Errorf("Play does not cover %q", got)
		}
	}
	if TransportPlay.Covers(TransportStop) {
		t.Error("Play covers a stop")
	}
	if TransportStart.Covers(TransportContinue) || TransportContinue.Covers(TransportPlay) {
		t.Error("Start or Continue covers a move other than itself")
	}
	if !TransportStop.Covers(TransportStop) {
		t.Error("Stop does not cover a stop")
	}
}

// The manual source says nothing but whether it plays, so its moves are
// worked out: Play as it starts, Stop as it stops, nothing otherwise.
func TestTheManualTransportMovesArePlayAndStop(t *testing.T) {
	r := NewRegistry()
	got := edges(r)
	r.SetManual(120, true)
	r.SetManual(130, true) // a tempo change, not a move
	r.SetManual(130, false)
	r.SetManual(130, false) // a second stop stops nothing
	if want := []TransportEdge{TransportPlay, TransportStop}; !slices.Equal(*got, want) {
		t.Errorf("the moves were %q, want %q", *got, want)
	}
}

// A source that is not driving the lights moves nothing a route hears,
// and one that appears already playing has not started.
func TestOnlyTheDrivingSourceMovesTheTransport(t *testing.T) {
	r := NewRegistry()
	r.SetPriority([]string{ManualSourceID, LinkSourceID})
	got := edges(r)
	r.Publish(Clock{SourceID: LinkSourceID, BPM: 120, Playing: true}, "Link")
	r.Publish(Clock{SourceID: LinkSourceID, BPM: 120, Playing: false}, "Link")
	if len(*got) != 0 {
		t.Errorf("a source ranked below the one driving moved the transport: %q", *got)
	}

	r.SetPriority([]string{LinkSourceID})
	r.Publish(Clock{SourceID: LinkSourceID, BPM: 120, Playing: false}, "Link")
	r.Publish(Clock{SourceID: LinkSourceID, BPM: 120, Playing: true}, "Link")
	if want := []TransportEdge{TransportPlay}; !slices.Equal(*got, want) {
		t.Errorf("the moves were %q, want %q", *got, want)
	}
}

func TestASourceAppearingPlayingHasNotStarted(t *testing.T) {
	r := NewRegistry()
	got := edges(r)
	r.Publish(Clock{SourceID: LinkSourceID, BPM: 120, Playing: true}, "Link")
	if len(*got) != 0 {
		t.Errorf("joining a session that was already playing moved the transport: %q", *got)
	}
}

// MIDI clock tells a start from a continue, and the registry believes it.
func TestMidiClockSaysStartAndContinue(t *testing.T) {
	follower, registry, start := clockFollower(t)
	got := edges(registry)
	at := feed(follower, start, 120, pulsesPerBeat)
	at = at.Add(pulsePeriod(120))
	follower.TransportStop(at)
	follower.TransportStop(at.Add(time.Millisecond)) // the return to zero
	follower.TransportContinue(at.Add(time.Second))
	follower.TransportStop(at.Add(2 * time.Second))
	follower.TransportStart(at.Add(3 * time.Second))
	want := []TransportEdge{TransportStop, TransportContinue, TransportStop, TransportStart}
	if !slices.Equal(*got, want) {
		t.Errorf("the moves were %q, want %q", *got, want)
	}
}

// A DAW's Start comes before any pulse, so before MIDI clock has a
// tempo to register with. The Start must still be heard, once, when it
// does.
func TestAStartBeforeTheFirstPulseIsHeardOnceTheClockRegisters(t *testing.T) {
	follower, registry, start := clockFollower(t)
	registry.SetPriority([]string{MidiClockSourceID})
	got := edges(registry)
	follower.TransportStart(start)
	if len(*got) != 0 {
		t.Fatalf("a Start with no tempo yet was reported: %q", *got)
	}
	feed(follower, start, 120, 2*pulsesPerBeat)
	if want := []TransportEdge{TransportStart}; !slices.Equal(*got, want) {
		t.Errorf("the moves were %q, want %q", *got, want)
	}
}
