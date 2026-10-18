package engine

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/beat"
	"github.com/marrasen/gunim/audio/capture"
)

// A Sound is what an Audio in heard since the last tick: frames of
// interleaved stereo at Rate frames a second.
type Sound struct {
	Rate   int
	Frames []float32
}

// A Count is what a clock carries: the tempo, in beats a minute, and
// the beats counted, the fraction of one the way to the next. Sure says
// the clock hears a beat, or keeps one of its own; a beat follower in a
// break counts on, not sure.
type Count struct {
	BPM, Beats float64
	Sure       bool
}

// A Listener is a sound card input an Audio in listens to, as
// [capture.Stream] is.
type Listener interface {
	Read(dst []float32) capture.Block
	Info() capture.Info
	Err() error
	Close() error
}

// Listen opens a sound card input, by its ID, "" for the system's
// default, and starts it recording.
func Listen(input string) (Listener, error) {
	return capture.Open(capture.Options{Device: input, Name: "crossroads", Hold: time.Second})
}

// ear is an Audio in's input, opened on a goroutine of its own.
type ear struct {
	input string
	mu    sync.Mutex
	l     Listener
	err   error
	gone  atomic.Bool
	// read is what a Read fills, and heard the frames of a tick, as
	// stereo.
	read, heard []float32
}

// open opens the ear's input with listen, and calls changed once it has
// opened or failed.
func (a *ear) open(listen func(string) (Listener, error), changed func()) {
	go func() {
		l, err := listen(a.input)
		a.mu.Lock()
		a.l, a.err = l, err
		a.mu.Unlock()
		if a.gone.Load() && l != nil {
			// Let go while it opened: the input goes too.
			_ = l.Close()
		}
		if changed != nil {
			changed()
		}
	}()
}

// close lets the input go, now and once one on its way opens.
func (a *ear) close() {
	a.gone.Store(true)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.l != nil {
		_ = a.l.Close()
	}
}

// hear is the sound heard since it was last asked, and what the Audio
// in's box says: what it listens to, or why it does not.
func (a *ear) hear() (heard Sound, says string) {
	a.mu.Lock()
	l, err := a.l, a.err
	a.mu.Unlock()
	switch {
	case err != nil:
		return Sound{}, err.Error()
	case l == nil:
		return Sound{}, "Opening…"
	case l.Err() != nil:
		return Sound{}, l.Err().Error()
	}
	info := l.Info()
	if len(a.read) == 0 {
		a.read = make([]float32, 4096*max(info.Channels, 1))
	}
	a.heard = a.heard[:0]
	for {
		b := l.Read(a.read)
		n := b.Len()
		if n == 0 {
			break
		}
		for i := range n {
			left := b.Frames[i*b.Channels]
			right := left
			if b.Channels > 1 {
				right = b.Frames[i*b.Channels+1]
			}
			a.heard = append(a.heard, left, right)
		}
	}
	return Sound{Rate: info.Rate, Frames: a.heard}, info.Name
}

// ownTempo is a Tempo module's own clock, and the taps that set it.
type ownTempo struct {
	beats float64
	taps  []time.Time
	// played is what it counted last tick.
	played Count
}

// The Tempo module's measures: the tempo it plays with none set, the
// longest gap between two taps of one tempo, and how many taps it
// counts.
const (
	homeBPM = 120.0
	tapGap  = 2 * time.Second
	tapsMax = 8
)

// A beat follower is sure of a beat it hears this clearly.
const sure = 0.2

// Level reads sound this many decibels down as silence, and falls this
// much of its range a second.
const (
	levelFloor = 60.0
	levelFall  = 1.5
)

// sound runs module m of the kinds that hear sound or count beats, dt
// after the last tick.
func (e *Engine) sound(m *show.Module, dt float64, live *Live) {
	key := portKey{m.ID, "out"}
	switch m.Kind {
	case show.AudioIn:
		a := e.ears[m.ID]
		if a == nil {
			return
		}
		snd, says := a.hear()
		e.values[key] = snd
		live.Says[m.ID] = says
	case show.Beat:
		snd, ok := e.input(m.ID, "in").(Sound)
		f := e.follow[m.ID]
		if ok && len(snd.Frames) > 0 {
			if f == nil || f.rate != snd.Rate {
				f = &follower{Live: beat.NewLive(snd.Rate), rate: snd.Rate}
				e.follow[m.ID] = f
			}
			f.Write(snd.Frames)
		}
		var c Count
		if f != nil {
			c = Count{BPM: f.BPM(), Beats: f.Beats(), Sure: f.BPM() > 0 && f.Confidence() >= sure}
			if b := int64(c.Beats); c.Sure && b != f.said {
				f.said = b
				e.note(e.last, fmt.Sprintf("%s: beat %d at %.1f BPM", m.Name, b, c.BPM))
			}
		}
		e.values[key] = c
		live.Clocks[m.ID] = c
	case show.Tempo:
		t := e.tempos[m.ID]
		if t == nil {
			t = &ownTempo{}
			e.tempos[m.ID] = t
		}
		c, ok := e.input(m.ID, "in").(Count)
		if ok && c.BPM > 0 && !m.Manual {
			// Following, through a break too, where the clock in counts
			// on unsure: and where the user takes over, the beat goes on
			// from here.
			t.beats = c.Beats
		} else {
			bpm := m.BPM
			if bpm <= 0 {
				bpm = homeBPM
			}
			t.beats += dt * bpm / 60
			c = Count{BPM: bpm, Beats: t.beats, Sure: true}
		}
		t.played = c
		e.values[key] = c
		live.Clocks[m.ID] = c
	case show.Level:
		v := e.levels[m.ID] - dt*levelFall
		if snd, ok := e.input(m.ID, "in").(Sound); ok && len(snd.Frames) > 0 {
			var sq float64
			for _, x := range snd.Frames {
				sq += float64(x) * float64(x)
			}
			db := 10 * math.Log10(max(sq/float64(len(snd.Frames)), 1e-12))
			v = max(v, (db+levelFloor)/levelFloor)
		}
		v = min(max(v, 0), 1)
		e.levels[m.ID] = v
		e.values[key] = v
		live.Values[m.ID] = v
	case show.Pulse:
		var v float64
		if c, ok := e.input(m.ID, "in").(Count); ok && c.BPM > 0 {
			v = Flash(c.Beats, m.Every)
		}
		e.values[key] = v
		live.Values[m.ID] = v
	case show.Fader, show.Lights, show.Mixer, show.DMXOut, show.StageView, show.Player, show.Button,
		show.Controller, show.MIDIOut, show.Map, show.Every, show.Toggle, show.Look, show.Chaser, show.Cues, show.Link, show.Serato, show.ClockIn, show.ClockOut, show.Timer, show.Pattern:
	}
}

// follower is a Beat module's beat follower, and the rate it hears at.
type follower struct {
	*beat.Live
	rate int
	// said is the beat the log was last told of.
	said int64
}

// Flash is how bright a pulse every beats long is at beats counted:
// full on the beat, fading to nothing by the next.
func Flash(beats, every float64) float64 {
	if every <= 0 {
		every = 1
	}
	_, f := math.Modf(beats / every)
	if f < 0 {
		f++
	}
	return (1 - f) * (1 - f)
}

// Played is what Tempo module counted last tick, and false for none.
func (e *Engine) Played(module string) (Count, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.tempos[module]
	if t == nil || t.played.BPM <= 0 {
		return Count{}, false
	}
	return t.played, true
}

// Tap taps Tempo module's beat now: the beat falls on it, and two taps
// or more a steady gap apart set the tempo, which it returns, and
// false for a first tap.
func (e *Engine) Tap(module string) (float64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.tempos[module]
	if t == nil {
		t = &ownTempo{}
		e.tempos[module] = t
	}
	now := e.now()
	if n := len(t.taps); n > 0 && now.Sub(t.taps[n-1]) > tapGap {
		t.taps = t.taps[:0]
	}
	t.taps = append(t.taps, now)
	if len(t.taps) > tapsMax {
		t.taps = t.taps[1:]
	}
	// The beat falls now: the count, as it stands now, goes to the
	// nearest whole beat. The next tick counts on from the last.
	var since float64
	if m := e.s.Module(module); m != nil && !e.last.IsZero() {
		bpm := m.BPM
		if bpm <= 0 {
			bpm = homeBPM
		}
		since = min(max(now.Sub(e.last).Seconds(), 0), 0.25) * bpm / 60
	}
	t.beats = math.Round(t.beats+since) - since
	if len(t.taps) < 2 {
		return 0, false
	}
	gap := t.taps[len(t.taps)-1].Sub(t.taps[0]).Seconds() / float64(len(t.taps)-1)
	return 60 / gap, true
}
