package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/marrasen/crossroads/show"
)

// fakeDeck plays songs of the lengths a test gives, moved on by hand.
type fakeDeck struct {
	lens   map[string]time.Duration
	path   string
	gain   float64
	st     DeckState
	loads  []string
	closed bool
	sound  []float32
}

func (d *fakeDeck) Load(path string, gain float64) error {
	l, ok := d.lens[path]
	if !ok {
		return errors.New("no such file")
	}
	d.path, d.gain = path, gain
	d.st = DeckState{Loaded: true, Len: l}
	d.loads = append(d.loads, path)
	return nil
}
func (d *fakeDeck) Play()                 { d.st.Playing = true }
func (d *fakeDeck) Pause()                { d.st.Playing = false }
func (d *fakeDeck) Stop()                 { d.st = DeckState{} }
func (d *fakeDeck) Seek(at time.Duration) { d.st.Pos = at }
func (d *fakeDeck) SetGain(db float64)    { d.gain = db }
func (d *fakeDeck) State() DeckState      { return d.st }
func (d *fakeDeck) Close() error          { d.closed = true; return nil }
func (d *fakeDeck) Hear(dst []float32) (sound []float32, rate int) {
	dst = append(dst, d.sound...)
	d.sound = d.sound[:0]
	return dst, 48000
}

// move plays the deck on by dt.
func (d *fakeDeck) move(dt time.Duration) {
	if !d.st.Playing {
		return
	}
	d.st.Pos += dt
	if d.st.Pos >= d.st.Len {
		d.st.Pos, d.st.Playing, d.st.Ended = d.st.Len, false, true
	}
}

// deckRig is a show of three songs, a Player, and Buttons into its
// play and next, on an engine with a fake deck.
type deckRig struct {
	s                  show.Show
	e                  *Engine
	d                  *fakeDeck
	player, play, next string
}

func newDeckRig(t *testing.T) *deckRig {
	t.Helper()
	r := &deckRig{d: &fakeDeck{lens: map[string]time.Duration{"a.wav": 4 * time.Second, "b.wav": 3 * time.Second, "c.wav": 5 * time.Second}}}
	for _, p := range []string{"a.wav", "b.wav", "c.wav"} {
		r.s.Songs = append(r.s.Songs, show.Song{ID: r.s.NewID("song"), Path: p, Title: p,
			Scan: &show.Scan{Length: 4, BPM: 120, Beats: []float64{0.5, 1, 1.5, 2, 2.5, 3, 3.5}, PerBar: 4}})
	}
	add := func(k show.Kind) string {
		id := r.s.NewID("m")
		r.s.Modules = append(r.s.Modules, show.Module{ID: id, Kind: k})
		return id
	}
	r.player, r.play, r.next = add(show.Player), add(show.Button), add(show.Button)
	for _, w := range [][2]string{{r.play, "play"}, {r.next, "next"}} {
		r.s.Wires = append(r.s.Wires, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: w[0], Port: "out"}, To: show.End{Module: r.player, Port: w[1]}})
	}
	r.e = New(nil)
	r.e.Deck = r.d
	r.e.Set(r.s)
	t.Cleanup(r.e.Close)
	return r
}

// tick moves the deck on by a tick and runs one, and returns what the
// player's trigger outputs fired.
func (r *deckRig) tick() (live Live, fired map[string]bool) {
	r.d.move(time.Second / 40)
	live = r.e.Tick()
	fired = map[string]bool{}
	for _, p := range []string{"start", "end", "bar"} {
		fired[p], _ = r.e.values[portKey{r.player, p}].(bool)
	}
	return live, fired
}

// TestAButtonPlaysThePlaylistAndItGoesOnToTheNextSong: the first song
// plays, starts, counts its bars, ends, and the next plays on.
func TestAButtonPlaysThePlaylistAndItGoesOnToTheNextSong(t *testing.T) {
	r := newDeckRig(t)
	r.e.Press(r.play)
	live, fired := r.tick()
	if !live.Playing.Playing || live.Playing.Song != r.s.Songs[0].ID || !fired["start"] {
		t.Fatalf("pressed, the player plays %+v, fired %v", live.Playing, fired)
	}
	if live.Values[r.play] != 1 {
		t.Error("the button pressed does not light")
	}
	var bars, ends int
	for range 4 * 40 {
		live, fired = r.tick()
		if fired["bar"] {
			bars++
		}
		if fired["end"] {
			ends++
		}
	}
	// Beats from 0.5 s, four a bar, at 120: bars at 0.5 s and 2.5 s.
	if bars != 2 || ends != 1 {
		t.Errorf("over the first song %d bars and %d ends fired, want 2 and 1", bars, ends)
	}
	if live.Playing.Song != r.s.Songs[1].ID || !live.Playing.Playing {
		t.Errorf("after the first song the player plays %+v", live.Playing)
	}
	c := live.Clocks[r.player]
	if c.BPM != 120 || !c.Sure {
		t.Errorf("the player's clock is %+v", c)
	}
}

// TestTheTransportGoesBackAndOn: Back goes to the start of a song well
// under way, or to the song before; Next past the last does nothing,
// and the last song ending stops the player.
func TestTheTransportGoesBackAndOn(t *testing.T) {
	r := newDeckRig(t)
	// Four seconds into c, five long: Back goes to its start.
	r.e.PlaySong(r.s.Songs[2].ID)
	for range 4 * 40 {
		r.tick()
	}
	r.e.Transport(Back)
	if r.d.path != "c.wav" || r.d.st.Pos != 0 {
		t.Errorf("Back well into c went to %s at %v", r.d.path, r.d.st.Pos)
	}
	// At its start, Back goes to b, and plays on.
	r.e.Transport(Back)
	if r.d.path != "b.wav" || !r.d.st.Playing {
		t.Errorf("Back at c's start went to %s, playing %v", r.d.path, r.d.st.Playing)
	}
	r.e.PlaySong(r.s.Songs[2].ID)
	r.e.Transport(Next)
	if r.d.path != "c.wav" {
		t.Errorf("Next past the last song went to %s", r.d.path)
	}
	for range 6 * 40 {
		r.tick()
	}
	if live, _ := r.tick(); live.Playing.Playing || r.d.st.Loaded {
		t.Errorf("after the last song the player plays %+v", live.Playing)
	}
}

// TestASongThatWillNotLoadSaysWhy, and a gain changed plays at once.
func TestASongThatWillNotLoadSaysWhy(t *testing.T) {
	r := newDeckRig(t)
	r.s.Songs[0].Path = "gone.wav"
	r.e.Set(r.s)
	r.e.Transport(Play)
	if live, _ := r.tick(); live.Playing.Err != "no such file" || live.Playing.Playing {
		t.Errorf("a song not there plays %+v", live.Playing)
	}
	r.e.PlaySong(r.s.Songs[1].ID)
	r.tick()
	r.s.Songs[1].Gain = 3
	r.e.Set(r.s)
	r.tick()
	if r.d.gain != 3 {
		t.Errorf("a song turned up 3 dB plays at %v", r.d.gain)
	}
}

// TestASongTakenOutStops, the playlist going on with nothing chosen.
func TestASongTakenOutStops(t *testing.T) {
	r := newDeckRig(t)
	r.e.PlaySong(r.s.Songs[1].ID)
	r.tick()
	r.s.Songs = r.s.Songs[:1]
	r.e.Set(r.s)
	if live, _ := r.tick(); live.Playing.Song != "" || live.Playing.Playing || r.d.st.Loaded {
		t.Errorf("its song taken out, the player plays %+v", live.Playing)
	}
}

// TestThePlayerHandsOnItsSound to the modules that listen.
func TestThePlayerHandsOnItsSound(t *testing.T) {
	r := newDeckRig(t)
	lv := r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: lv, Kind: show.Level})
	r.s.Wires = append(r.s.Wires, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: r.player, Port: "audio"}, To: show.End{Module: lv, Port: "in"}})
	r.e.Set(r.s)
	r.d.sound = make([]float32, 2400)
	for i := range r.d.sound {
		r.d.sound[i] = 0.5
	}
	if live, _ := r.tick(); live.Values[lv] < 0.8 {
		t.Errorf("a level of the player's sound at half scale reads %v", live.Values[lv])
	}
}
