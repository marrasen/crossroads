package engine

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/marrasen/crossroads/link"
	"github.com/marrasen/crossroads/serato"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/crossroads/tempo"
	"github.com/marrasen/gunim/audio/midi"
)

// A Linker is an Ableton Link session, as package link's is.
type Linker interface {
	Start(ctx context.Context) error
	Stop()
	Snapshot() link.Snapshot
	SetPlaying(playing bool)
}

// A SeratoFeed is Serato DJ Pro's decks, as package serato's service
// hears them.
type SeratoFeed interface {
	Start(ctx context.Context) error
	Stop()
	Snapshot() serato.Snapshot
}

// linkQuantum is the beats in a bar a Link session is read in.
const linkQuantum = 4

// A feed is a session the engine runs while a module wants it: Link,
// or Serato. It starts as the first module wanting it comes, and stops
// as the last goes.
type feed[T interface {
	Start(ctx context.Context) error
	Stop()
}] struct {
	f      T
	on     bool
	err    error
	cancel context.CancelFunc
}

// want starts the feed where it is wanted and not running, making it
// with newFeed, and stops it where it is running and not wanted.
func (f *feed[T]) want(wanted bool, newFeed func() T) {
	switch {
	case wanted && !f.on:
		ctx, cancel := context.WithCancel(context.Background())
		f.f, f.cancel, f.on = newFeed(), cancel, true
		f.err = f.f.Start(ctx)
	case !wanted && f.on:
		f.f.Stop()
		f.cancel()
		f.on, f.err = false, nil
	}
}

// syncs are the engine's clocks from elsewhere: the Link session and
// the clock read off it, and Serato's feed, the deck it follows and
// the clock read off that.
type syncs struct {
	link      feed[Linker]
	linkClock tempo.Clock
	linkHave  bool
	serato    feed[SeratoFeed]
	deck      int
	deckClock tempo.Clock
}

// wantFeeds starts and stops Link and Serato as the show's modules want
// them.
func (e *Engine) wantFeeds() {
	var wantLink, wantSerato bool
	for _, m := range e.s.Modules {
		wantLink = wantLink || m.Kind == show.Link
		wantSerato = wantSerato || m.Kind == show.Serato
	}
	e.sync.link.want(wantLink, func() Linker {
		if e.Link != nil {
			return e.Link()
		}
		return link.New(linkQuantum, nil)
	})
	if !wantLink {
		e.sync.linkHave = false
	}
	e.sync.serato.want(wantSerato, func() SeratoFeed {
		if e.Serato != nil {
			return e.Serato()
		}
		return serato.NewService(func(serato.Snapshot) {})
	})
	if !wantSerato {
		e.sync.deck = -1
	}
}

// linked runs Link module m: the session's clock, and what the box
// says of it.
func (e *Engine) linked(m *show.Module, live *Live) {
	f := &e.sync.link
	var c Count
	switch {
	case !f.on:
	case f.err != nil:
		live.Says[m.ID] = f.err.Error()
	default:
		s := f.f.Snapshot()
		at := tempo.Now()
		snap := tempo.LinkSnapshot{BPM: s.BPM, Beat: s.Beat, Phase: s.Phase, Peers: s.Peers, Playing: s.Playing, Quantum: s.Quantum, ReadAtMs: at}
		if next, ok := tempo.LinkClock(e.sync.linkClock, e.sync.linkHave, snap, at, false); ok {
			e.sync.linkClock, e.sync.linkHave = next, true
		}
		if e.sync.linkHave {
			c = Count{BPM: e.sync.linkClock.BPM, Beats: tempo.BeatsAt(e.sync.linkClock, at), Sure: s.Measured || s.Peers == 0}
		}
		switch s.Peers {
		case 0:
			live.Says[m.ID] = fmt.Sprintf("No one else in the session · %.1f BPM", s.BPM)
		case 1:
			live.Says[m.ID] = fmt.Sprintf("1 other · %.1f BPM", s.BPM)
		default:
			live.Says[m.ID] = fmt.Sprintf("%d others · %.1f BPM", s.Peers, s.BPM)
		}
	}
	e.values[portKey{m.ID, "out"}] = c
	live.Clocks[m.ID] = c
}

// seratoReanchor is how often the clock read off a Serato deck is
// anchored again where nothing about the deck changed: its playhead
// comes many times a second, and the clock runs on between.
const seratoReanchor = 500 * time.Millisecond

// followSerato runs Serato module m: the clock of the deck the
// crossfader favours, its beat counted from its playhead.
func (e *Engine) followSerato(m *show.Module, live *Live) {
	f := &e.sync.serato
	var c Count
	switch {
	case !f.on:
	case f.err != nil:
		live.Says[m.ID] = f.err.Error()
	default:
		s := f.f.Snapshot()
		at := tempo.Now()
		deck := serato.FollowDeck(s, e.sync.deck)
		switch {
		case !s.Connected:
			live.Says[m.ID] = "Waiting for Serato DJ Pro"
		case deck < 0 || deck >= len(s.Decks):
			live.Says[m.ID] = "No deck playing"
		default:
			d := s.Decks[deck]
			prev := e.sync.deckClock
			if deck != e.sync.deck || prev.BPM != d.BPM || prev.Playing != d.Playing || at-prev.RefTimeMs > float64(seratoReanchor/time.Millisecond) {
				e.sync.deckClock = tempo.Clock{SourceID: tempo.SeratoSourceID, BPM: d.BPM, BeatAtRef: d.PositionSec * d.BPM / 60,
					RefTimeMs: at, Playing: d.Playing}
			}
			c = Count{BPM: d.BPM, Beats: tempo.BeatsAt(e.sync.deckClock, at), Sure: d.Playing}
			title := d.Title
			if title == "" {
				title = "Deck " + strconv.Itoa(deck+1)
			}
			live.Says[m.ID] = fmt.Sprintf("%s · %.1f BPM", title, d.BPM)
		}
		e.sync.deck = deck
	}
	e.values[portKey{m.ID, "out"}] = c
	live.Clocks[m.ID] = c
}

// A clockIn is a MIDI clock in's port, and the follower it feeds.
type clockIn struct {
	device string
	reg    *tempo.Registry
	follow *tempo.MidiClockFollower
	events []midi.Event
}

func newClockIn(device string) *clockIn {
	reg := tempo.NewRegistry()
	c := &clockIn{device: device, reg: reg, follow: tempo.NewMidiClockFollower(reg)}
	c.follow.Enable()
	return c
}

// clockedIn runs MIDI clock in module m: the clock its port's pulses
// make.
func (e *Engine) clockedIn(m *show.Module, live *Live) {
	ci := e.clockIns[m.ID]
	if ci == nil {
		return
	}
	ci.events = append(ci.events[:0], e.hub.heard[m.Device]...)
	for _, ev := range ci.events {
		switch ev.Kind() {
		case midi.KindClock:
			ci.follow.Pulse(ev.Time)
		case midi.KindStart:
			ci.follow.TransportStart(ev.Time)
		case midi.KindContinue:
			ci.follow.TransportContinue(ev.Time)
		case midi.KindStop:
			ci.follow.TransportStop(ev.Time)
		case midi.KindSongPosition:
			ci.follow.SongPosition(ev.Beats(), ev.Time)
		case midi.KindNoteOff, midi.KindNoteOn, midi.KindKeyPressure, midi.KindControl, midi.KindProgram, midi.KindPressure,
			midi.KindBend, midi.KindSysex, midi.KindTimeCode, midi.KindSongSelect, midi.KindTuneRequest, midi.KindActiveSensing,
			midi.KindReset, midi.KindUndefined:
		}
	}
	ci.follow.Tick(time.Now())
	var c Count
	says := e.hub.says(m.Device, true)
	if clock, ok := ci.reg.ClockFor(tempo.MidiClockSourceID); ok && clock.BPM > 0 {
		c = Count{BPM: clock.BPM, Beats: tempo.BeatsAt(clock, tempo.Now()), Sure: clock.Playing}
		state := "stopped"
		if clock.Playing {
			state = "playing"
		}
		says = fmt.Sprintf("%s · %.1f BPM, %s", says, clock.BPM, state)
	}
	e.values[portKey{m.ID, "out"}] = c
	live.Clocks[m.ID] = c
	live.Says[m.ID] = says
}

// A clockOut is a MIDI clock out's port, the bridge sending it clock,
// and the clock the bridge reads, anchored afresh only as it moves.
type clockOut struct {
	device string
	out    *tempo.ClockOut
	mu     sync.Mutex
	at     tempo.Clock
	have   bool
	// sendTo is the port's output, as the last tick found it.
	sendTo MIDIOut
}

func newClockOut(device string) *clockOut {
	c := &clockOut{device: device}
	c.out = tempo.NewClockOut(c.clock, c.send)
	return c
}

// clock is what the bridge sends: the clock wired in, as the last tick
// read it.
func (c *clockOut) clock() (tempo.Clock, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at, c.have
}

// send hands the bridge's bytes to the port, at the time it asks.
func (c *clockOut) send(b []byte, timeMs float64) bool {
	c.mu.Lock()
	out := c.sendTo
	c.mu.Unlock()
	if out == nil || len(b) == 0 {
		return false
	}
	var at time.Time
	if timeMs != 0 {
		at = tempo.TimeAt(timeMs)
	}
	return out.Send(at, midi.Message{Status: b[0]}) == nil
}

// follow takes the clock wired in, anchoring it again only where its
// tempo or transport changed or it drifted, so the pulses keep an even
// grid.
func (c *clockOut) follow(in Count, ok bool, out MIDIOut) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sendTo = out
	if !ok || in.BPM <= 0 {
		c.have = false
		return
	}
	at := tempo.Now()
	next := tempo.Clock{SourceID: "wire", BPM: in.BPM, BeatAtRef: in.Beats, RefTimeMs: at, Playing: in.Sure}
	if c.have && c.at.BPM == next.BPM && c.at.Playing == next.Playing && math.Abs(tempo.BeatsAt(c.at, at)-in.Beats) <= tempo.ReanchorBeats {
		return
	}
	c.at, c.have = next, true
}

// clockedOut runs MIDI clock out module m: the clock wired in goes out
// as MIDI clock, its pulses scheduled ahead on the bridge's goroutine.
func (e *Engine) clockedOut(m *show.Module, live *Live) {
	co := e.clockOuts[m.ID]
	if co == nil {
		return
	}
	out, _ := e.hub.out(m.Device)
	in, ok := e.input(m.ID, "in").(Count)
	co.follow(in, ok, out)
	live.Says[m.ID] = e.hub.says(m.Device, false)
}

// stop stops the clock out's bridge.
func (c *clockOut) stop() { c.out.Stop() }
