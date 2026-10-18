package player

import (
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/audio/speaker"
)

// fade is how long a song takes to fade as it pauses or stops, so it
// does not click.
const fade = 40 * time.Millisecond

// A Deck plays one song at a time through the speakers, which it opens
// as the first song loads. It is an [engine.Deck].
type Deck struct {
	mu   sync.Mutex
	mix  *audio.Mixer
	spk  *speaker.Speaker
	v    *audio.Voice
	f    *os.File
	ear  *ring
	err  error
	read int64
	// open opens the speakers; tests put a stand-in in.
	open func(*audio.Mixer) (*speaker.Speaker, error)
}

// NewDeck returns a deck with nothing loaded.
func NewDeck() *Deck {
	return &Deck{mix: audio.NewMixer(), ear: newRing(2 * audio.SampleRate * 2)}
}

// speakers opens the speakers, once.
func (d *Deck) speakers() error {
	if d.spk != nil || d.err != nil {
		return d.err
	}
	open := d.open
	if open == nil {
		open = func(m *audio.Mixer) (*speaker.Speaker, error) { return speaker.Open(m, speaker.Options{}) }
	}
	d.spk, d.err = open(d.mix)
	return d.err
}

// Load implements [engine.Deck].
func (d *Deck) Load(path string, gain float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.drop()
	if err := d.speakers(); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	src, err := audio.Decode(f)
	if err != nil {
		_ = f.Close()
		return err
	}
	d.f = f
	d.v = d.mix.Play(src, audio.Options{Volume: volume(gain), Paused: true, Insert: d.ear})
	return nil
}

// volume is the scale a gain of db plays at.
func volume(db float64) float32 {
	return float32(max(math.Pow(10, db/20), 1e-6))
}

// drop stops what plays, and lets its file go once it has faded.
func (d *Deck) drop() {
	if d.v == nil {
		return
	}
	v, f := d.v, d.f
	d.v, d.f = nil, nil
	v.Stop(fade)
	go func() {
		<-v.Done()
		_ = f.Close()
	}()
}

// Play implements [engine.Deck].
func (d *Deck) Play() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.v != nil {
		d.v.Resume(fade)
	}
}

// Pause implements [engine.Deck].
func (d *Deck) Pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.v != nil {
		d.v.Pause(fade)
	}
}

// Stop implements [engine.Deck].
func (d *Deck) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.drop()
}

// Seek implements [engine.Deck].
func (d *Deck) Seek(at time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.v != nil {
		_ = d.v.Seek(max(at, 0))
	}
}

// SetGain implements [engine.Deck].
func (d *Deck) SetGain(db float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.v != nil {
		d.v.SetVolume(volume(db), anim.Spring{Response: 0.3, Damping: 1})
	}
}

// State implements [engine.Deck].
func (d *Deck) State() engine.DeckState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.v == nil {
		return engine.DeckState{}
	}
	st := engine.DeckState{Loaded: true, Pos: d.v.Position(), Len: d.v.Len(), Playing: !d.v.Paused()}
	select {
	case <-d.v.Done():
		// Played to its end, or to a fault in its file: either way,
		// over.
		st.Playing, st.Ended = false, true
	default:
	}
	return st
}

// Hear implements [engine.Deck].
func (d *Deck) Hear(dst []float32) (sound []float32, rate int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dst, d.read = d.ear.since(dst, d.read)
	return dst, d.mix.Rate()
}

// Close implements [engine.Deck]: the song stops, and the speakers
// close.
func (d *Deck) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.drop()
	if d.spk != nil {
		err := d.spk.Close()
		d.spk = nil
		return err
	}
	return nil
}

// A ring keeps the last of the sound a voice plays, written as the
// mixer mixes and read by the engine as it ticks. The mixer's side
// takes no lock and makes nothing.
type ring struct {
	buf []float32
	// wrote counts the samples ever written.
	wrote atomic.Int64
}

func newRing(n int) *ring { return &ring{buf: make([]float32, n)} }

// Process implements [audio.Insert]: the frames are kept, unchanged.
func (r *ring) Process(frames []float32) {
	w := r.wrote.Load()
	n := int64(len(r.buf))
	for i, s := range frames {
		r.buf[(w+int64(i))%n] = s
	}
	r.wrote.Store(w + int64(len(frames)))
}

// since appends to dst the samples written from read on, no more than
// half the ring, and returns it and where the next read starts.
func (r *ring) since(dst []float32, read int64) (out []float32, next int64) {
	w := r.wrote.Load()
	n := int64(len(r.buf))
	read = max(read, w-n/2)
	read -= read % 2
	for i := read; i < w; i++ {
		dst = append(dst, r.buf[i%n])
	}
	return dst, w
}
