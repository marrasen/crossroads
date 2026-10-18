package engine

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/crossroads/link"
	"github.com/marrasen/crossroads/serato"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// fakeLink is a Link session a test sets the reading of.
type fakeLink struct {
	mu      sync.Mutex
	snap    link.Snapshot
	started bool
	stopped bool
}

func (f *fakeLink) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = true
	return nil
}

func (f *fakeLink) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
}

func (f *fakeLink) Snapshot() link.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

// fakeSerato is Serato's feed as a test sets it.
type fakeSerato struct {
	mu   sync.Mutex
	snap serato.Snapshot
}

func (f *fakeSerato) Start(context.Context) error { return nil }
func (f *fakeSerato) Stop()                       {}
func (f *fakeSerato) Snapshot() serato.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap.Clone()
}

// TestLinkIsAClockWhileAModuleWantsIt: the session starts as a Link
// module comes, its tempo and beat come out, and it stops as the
// module goes.
func TestLinkIsAClockWhileAModuleWantsIt(t *testing.T) {
	fl := &fakeLink{snap: link.Snapshot{BPM: 126, Beat: 8, Peers: 2, Playing: true, Quantum: 4, Measured: true}}
	var s show.Show
	id := s.NewID("m")
	s.Modules = []show.Module{{ID: id, Kind: show.Link}}
	e := New(nil)
	e.Link = func() Linker { return fl }
	defer e.Close()
	e.Set(s)
	live := e.Tick()
	c := live.Clocks[id]
	if c.BPM != 126 || math.Abs(c.Beats-8) > 0.1 || !c.Sure {
		t.Errorf("the Link module counts %+v", c)
	}
	if live.Says[id] != "2 others · 126.0 BPM" {
		t.Errorf("its box says %q", live.Says[id])
	}
	e.Set(show.Show{})
	if !fl.started || !fl.stopped {
		t.Errorf("the session started %v, stopped %v", fl.started, fl.stopped)
	}
}

// TestSeratoFollowsTheDeckTheCrossfaderFavours, counting its beat from
// its playhead.
func TestSeratoFollowsTheDeckTheCrossfaderFavours(t *testing.T) {
	snap := serato.EmptySnapshot(true)
	snap.Decks[0] = serato.Deck{Index: 0, Title: "Left", Loaded: true, Playing: true, BPM: 120, PositionSec: 30}
	snap.Decks[1] = serato.Deck{Index: 1, Title: "Right", Loaded: true, Playing: true, BPM: 128, PositionSec: 60}
	snap.Crossfader = 1
	fs := &fakeSerato{snap: snap}
	var s show.Show
	id := s.NewID("m")
	s.Modules = []show.Module{{ID: id, Kind: show.Serato}}
	e := New(nil)
	e.Serato = func() SeratoFeed { return fs }
	defer e.Close()
	e.Set(s)
	live := e.Tick()
	c := live.Clocks[id]
	if c.BPM != 128 || math.Abs(c.Beats-128) > 0.1 || live.Says[id] != "Right · 128.0 BPM" {
		t.Errorf("crossfaded right, Serato counts %+v and says %q", c, live.Says[id])
	}
}

// TestMIDIClockInReadsTheTempoOfItsPulses: 24 a beat at 120, and the
// transport's start.
func TestMIDIClockInReadsTheTempoOfItsPulses(t *testing.T) {
	d := &fakeMIDI{}
	var s show.Show
	id := s.NewID("m")
	s.Modules = []show.Module{{ID: id, Kind: show.ClockIn, Device: "DAW"}}
	opened := make(chan struct{}, 4)
	e := New(func() { opened <- struct{}{} })
	e.MIDI = d
	defer e.Close()
	e.Set(s)
	e.Tick()
	<-opened
	start := time.Now().Add(-2 * time.Second)
	d.mu.Lock()
	f := d.f
	d.mu.Unlock()
	f(midi.Event{Time: start, Message: midi.Start()})
	for i := range 4 * 24 {
		at := start.Add(time.Duration(i) * time.Second / 48)
		f(midi.Event{Time: at, Message: midi.Clock()})
	}
	live := e.Tick()
	c := live.Clocks[id]
	if math.Abs(c.BPM-120) > 0.5 || !c.Sure {
		t.Errorf("pulses at 120 read as %+v", c)
	}
}

// TestMIDIClockOutSendsStartAndPulses for the clock wired into it.
func TestMIDIClockOutSendsStartAndPulses(t *testing.T) {
	d := &fakeMIDI{}
	var s show.Show
	tp, out := s.NewID("m"), s.NewID("m")
	s.Modules = []show.Module{{ID: tp, Kind: show.Tempo, BPM: 120, Manual: true}, {ID: out, Kind: show.ClockOut, Device: "Drum machine"}}
	s.Wires = []show.Wire{{ID: "w", From: show.End{Module: tp, Port: "out"}, To: show.End{Module: out, Port: "in"}}}
	opened := make(chan struct{}, 4)
	e := New(func() { opened <- struct{}{} })
	e.MIDI = d
	defer e.Close()
	e.Set(s)
	e.Tick()
	<-opened
	for range 12 {
		e.Tick()
		time.Sleep(25 * time.Millisecond)
	}
	sent := d.took()
	var starts, pulses int
	for _, m := range sent {
		switch m.Kind() {
		case midi.KindStart:
			starts++
		case midi.KindClock:
			pulses++
		case midi.KindNoteOff, midi.KindNoteOn, midi.KindKeyPressure, midi.KindControl, midi.KindProgram, midi.KindPressure,
			midi.KindBend, midi.KindSysex, midi.KindTimeCode, midi.KindSongPosition, midi.KindSongSelect, midi.KindTuneRequest,
			midi.KindContinue, midi.KindStop, midi.KindActiveSensing, midi.KindReset, midi.KindUndefined:
		}
	}
	// A third of a second at 120 is two thirds of a beat: 16 pulses,
	// give or take what the bridge sends ahead.
	if starts != 1 || pulses < 8 || pulses > 30 {
		t.Errorf("a third of a second at 120 sent %d starts and %d pulses", starts, pulses)
	}
}
