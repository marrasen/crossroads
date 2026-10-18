package engine

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/capture"
	"github.com/marrasen/gunim/geom"
)

// fakeEar is a sound card input that hands on what a test puts in it,
// as stereo at 48 kHz.
type fakeEar struct {
	mu     sync.Mutex
	frames []float32
	closed bool
}

func (f *fakeEar) put(frames []float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, frames...)
}

func (f *fakeEar) Read(dst []float32) capture.Block {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := copy(dst, f.frames)
	n -= n % 2
	f.frames = f.frames[n:]
	return capture.Block{Frames: dst[:n], Channels: 2}
}

func (f *fakeEar) Info() capture.Info {
	return capture.Info{Name: "Fake input", Rate: rate, Channels: 2}
}
func (f *fakeEar) Err() error { return nil }
func (f *fakeEar) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

const rate = 48000

// clicks is secs of a click every beat at bpm, from the first frame, as
// stereo at rate.
func clicks(bpm, secs float64) []float32 {
	n := int(secs * rate)
	out := make([]float32, 2*n)
	per := 60 / bpm * rate
	for at := 0.0; int(at) < n; at += per {
		for k := range int(0.12 * rate) {
			i := int(at) + k
			if i >= n {
				break
			}
			v := float32(0.6 * math.Exp(-float64(k)/rate/0.03) * math.Sin(2*math.Pi*1500*float64(k)/rate))
			out[2*i] += v
			out[2*i+1] += v
		}
	}
	return out
}

// rig is a show of an Audio in heard by a Beat, its clock into a Tempo
// and a Pulse, and the sound into a Level, on an engine whose time a
// test moves.
type rig struct {
	s                             show.Show
	e                             *Engine
	ear                           *fakeEar
	clock                         time.Time
	in, beat, tempo, pulse, level string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{ear: &fakeEar{}, clock: time.Unix(1000, 0)}
	add := func(k show.Kind) string {
		id := r.s.NewID("m")
		r.s.Modules = append(r.s.Modules, show.Module{ID: id, Kind: k, Name: k.Title(), At: geom.Pt(0, 0)})
		return id
	}
	wire := func(from, to string) {
		r.s.Wires = append(r.s.Wires, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: from, Port: "out"}, To: show.End{Module: to, Port: "in"}})
	}
	r.in, r.beat, r.tempo, r.pulse, r.level = add(show.AudioIn), add(show.Beat), add(show.Tempo), add(show.Pulse), add(show.Level)
	wire(r.in, r.beat)
	wire(r.beat, r.tempo)
	wire(r.tempo, r.pulse)
	wire(r.in, r.level)
	opened := make(chan struct{})
	r.e = New(func() {
		select {
		case opened <- struct{}{}:
		default:
		}
	})
	r.e.Listen = func(string) (Listener, error) { return r.ear, nil }
	r.e.Now = func() time.Time { return r.clock }
	r.e.Set(r.s)
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the input never opened")
	}
	t.Cleanup(r.e.Close)
	return r
}

// play plays sound into the rig a tick at a time, as forty ticks a
// second would hear it, and returns what the last tick made.
func (r *rig) play(sound []float32) Live {
	const tick = rate / 40
	var live Live
	for i := 0; i < len(sound); i += 2 * tick {
		r.ear.put(sound[i:min(i+2*tick, len(sound))])
		r.clock = r.clock.Add(time.Second / 40)
		live = r.e.Tick()
	}
	return live
}

// TestTheBeatOfSoundComesThroughToAPulse: an Audio in heard by a Beat
// finds the tempo of a click track, the Tempo after it follows, and
// the Pulse flashes on the clicks.
func TestTheBeatOfSoundComesThroughToAPulse(t *testing.T) {
	r := newRig(t)
	live := r.play(clicks(120, 10))
	if says := live.Ears[r.in]; says != "Fake input" {
		t.Errorf("the Audio in says %q", says)
	}
	c := live.Clocks[r.beat]
	if math.Abs(c.BPM-120) > 1 || !c.Sure {
		t.Fatalf("the Beat counts %+v, want 120, sure", c)
	}
	if got := live.Clocks[r.tempo]; got != c {
		t.Errorf("the Tempo counts %+v, the Beat into it %+v", got, c)
	}
	// Over two more seconds, the pulse is bright on the clicks and dark
	// between them.
	var on, off, loud float64
	sound := clicks(120, 12)[2*10*rate:]
	const tick = rate / 40
	for i := 0; i < len(sound); i += 2 * tick {
		r.ear.put(sound[i:min(i+2*tick, len(sound))])
		r.clock = r.clock.Add(time.Second / 40)
		live = r.e.Tick()
		// Where the tick falls in the beat: the clicks are every 0.5 s
		// from the start.
		_, f := math.Modf(float64(10*rate+i/2+tick) / rate / 0.5)
		v := live.Values[r.pulse]
		loud = max(loud, live.Values[r.level])
		switch {
		case f < 0.15:
			on = max(on, v)
		case f > 0.6:
			off = max(off, v)
		}
	}
	if on < 0.6 || off > 0.3 {
		t.Errorf("the pulse reaches %v on the beat and %v off it", on, off)
	}
	// The level rises with each click, and falls between them.
	if l := live.Values[r.level]; loud < 0.6 || l >= loud {
		t.Errorf("the level of a click track reaches %v, and ends at %v", loud, l)
	}
}

// TestATempoPlaysItsOwnUntilTold: unwired, a Tempo plays its own tempo;
// taps a steady gap apart give the tempo, and put the beat on the tap.
func TestATempoPlaysItsOwnUntilTold(t *testing.T) {
	var s show.Show
	id := s.NewID("m")
	s.Modules = []show.Module{{ID: id, Kind: show.Tempo, BPM: 90}}
	now := time.Unix(1000, 0)
	e := New(nil)
	e.Now = func() time.Time { return now }
	defer e.Close()
	e.Set(s)
	e.Tick()
	for range 40 {
		now = now.Add(time.Second / 40)
		e.Tick()
	}
	if c := e.Tick().Clocks[id]; math.Abs(c.Beats-1.5) > 0.01 || c.BPM != 90 {
		t.Errorf("a second at 90 counts %+v, want 1.5 beats", c)
	}
	now = now.Add(100 * time.Millisecond)
	if _, ok := e.Tap(id); ok {
		t.Error("a first tap gave a tempo")
	}
	if c := e.Tick().Clocks[id]; math.Abs(c.Beats-math.Round(c.Beats)) > 1e-9 {
		t.Errorf("after a tap the count is %v, want a whole beat", c.Beats)
	}
	var bpm float64
	for range 3 {
		now = now.Add(400 * time.Millisecond)
		bpm, _ = e.Tap(id)
	}
	if math.Abs(bpm-150) > 0.01 {
		t.Errorf("taps 0.4 s apart give %v, want 150", bpm)
	}
	// A tap long after starts again.
	now = now.Add(5 * time.Second)
	if _, ok := e.Tap(id); ok {
		t.Error("a tap five seconds on gave a tempo")
	}
}

// TestATempoFollowsThroughABreak: the music stops, the Beat counts on
// unsure, and the Tempo after it goes on counting with it rather than
// jumping to a tempo of its own.
func TestATempoFollowsThroughABreak(t *testing.T) {
	r := newRig(t)
	r.s.Module(r.tempo).BPM = 90
	r.e.Set(r.s)
	r.play(clicks(120, 8))
	live := r.play(make([]float32, 2*8*rate))
	if live.Clocks[r.beat].Sure {
		t.Fatal("eight seconds into a break the Beat is still sure")
	}
	c := live.Clocks[r.tempo]
	if math.Abs(c.BPM-120) > 1 || c != live.Clocks[r.beat] {
		t.Errorf("eight seconds into a break the Tempo counts %+v, the Beat %+v", c, live.Clocks[r.beat])
	}
	if p, ok := r.e.Played(r.tempo); !ok || p != c {
		t.Errorf("the Tempo is said to have played %+v", p)
	}
}

// TestATempoHeldManualIgnoresItsClock: a Tempo set to its own tempo
// plays that, whatever comes in.
func TestATempoHeldManualIgnoresItsClock(t *testing.T) {
	r := newRig(t)
	r.play(clicks(120, 8))
	r.s.Module(r.tempo).Manual = true
	r.s.Module(r.tempo).BPM = 100
	r.e.Set(r.s)
	live := r.play(clicks(120, 1))
	if c := live.Clocks[r.tempo]; c.BPM != 100 {
		t.Errorf("held at 100, the Tempo counts %+v", c)
	}
}

// TestAnAudioInThatFailsSaysWhy, in its box.
func TestAnAudioInThatFailsSaysWhy(t *testing.T) {
	var s show.Show
	id := s.NewID("m")
	s.Modules = []show.Module{{ID: id, Kind: show.AudioIn}}
	done := make(chan struct{}, 1)
	e := New(func() { done <- struct{}{} })
	e.Listen = func(string) (Listener, error) { return nil, errors.New("no such input") }
	defer e.Close()
	e.Set(s)
	<-done
	if says := e.Tick().Ears[id]; says != "no such input" {
		t.Errorf("the Audio in says %q", says)
	}
}

// TestAnAudioInGoneLetsItsInputGo: taken out of the show, its input is
// closed.
func TestAnAudioInGoneLetsItsInputGo(t *testing.T) {
	r := newRig(t)
	r.s.RemoveModule(r.in)
	r.e.Set(r.s)
	r.ear.mu.Lock()
	defer r.ear.mu.Unlock()
	if !r.ear.closed {
		t.Error("the input is still open")
	}
}

// TestFlashFadesOverItsBeats: full on the beat, half way down a
// quarter of the way, and dark by the next.
func TestFlashFadesOverItsBeats(t *testing.T) {
	for _, c := range []struct{ beats, every, want float64 }{
		{4, 1, 1}, {4.5, 1, 0.25}, {4.999, 1, 0}, {5, 2, 0.25}, {6, 2, 1}, {0.125, 0.25, 0.25}, {3, 0, 1},
	} {
		if got := Flash(c.beats, c.every); math.Abs(got-c.want) > 0.01 {
			t.Errorf("Flash(%v, %v) = %v, want %v", c.beats, c.every, got, c.want)
		}
	}
}
