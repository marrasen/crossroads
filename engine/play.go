package engine

import (
	"math"
	"time"

	"github.com/marrasen/crossroads/show"
)

// A Deck plays the playlist's songs, one at a time, as package player's
// does through the speakers.
type Deck interface {
	// Load stops what plays and readies the song at path, paused at its
	// start, gain decibels louder.
	Load(path string, gain float64) error
	Play()
	Pause()
	// Stop stops what plays, and lets the song go.
	Stop()
	Seek(at time.Duration)
	SetGain(db float64)
	State() DeckState
	// Hear appends to dst the sound played since it was last asked, as
	// interleaved stereo, and gives the rate it plays at.
	Hear(dst []float32) (sound []float32, rate int)
	Close() error
}

// DeckState is what a deck is doing: whether a song is loaded, whether
// it plays, whether it has played to its end, and how far in it is of
// how long.
type DeckState struct {
	Loaded, Playing, Ended bool
	Pos, Len               time.Duration
}

// Playing is what the playlist is doing, for the windows: the song
// chosen, how far in it is of how long, whether it plays, and why it
// would not.
type Playing struct {
	Song     string
	Pos, Len time.Duration
	Playing  bool
	Err      string
}

// A Command is what the transport is told to do.
type Command int

// The commands.
const (
	// Play plays the song chosen, from where it stands.
	Play Command = iota
	Pause
	// Toggle plays a song paused, and pauses one playing.
	Toggle
	// Stop stops, the song going back to its start.
	Stop
	// Next goes to the next song, and Back to the song before, or to
	// the start of this one where it has played a while. Either plays
	// on where a song played.
	Next
	Back
)

// barJust is how far into a bar, in beats, a song started or moved to
// there still counts as starting it.
const barJust = 0.25

// backTo is how far into a song Back goes to its start rather than to
// the song before.
const backTo = 3 * time.Second

// The player's events this tick, which its trigger outputs fire on.
type events struct {
	start, end, bar bool
}

// Transport has the playlist do c.
func (e *Engine) Transport(c Command) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.do(c)
}

// PlaySong plays song id from its start.
func (e *Engine) PlaySong(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.load(id) {
		e.play()
	}
}

// SeekTo moves the song chosen to at.
func (e *Engine) SeekTo(at time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Deck != nil && e.state.Loaded {
		e.Deck.Seek(at)
		e.state.Pos = at
		e.bar = math.MinInt64
	}
}

// do does c, the lock held.
func (e *Engine) do(c Command) {
	if e.Deck == nil {
		return
	}
	switch c {
	case Toggle:
		if e.state.Playing {
			e.Deck.Pause()
			e.state.Playing = false
			return
		}
		e.play()
	case Play:
		e.play()
	case Pause:
		e.Deck.Pause()
		e.state.Playing = false
	case Stop:
		e.Deck.Stop()
		e.state = DeckState{}
	case Next, Back:
		playing := e.state.Playing
		if c == Back && e.state.Loaded && e.state.Pos > backTo {
			e.Deck.Seek(0)
			e.state.Pos = 0
			return
		}
		step := 1
		if c == Back {
			step = -1
		}
		i := e.songIndex(e.song) + step
		if i < 0 || i >= len(e.s.Songs) {
			return
		}
		if e.load(e.s.Songs[i].ID) && playing {
			e.play()
		}
	}
}

// songIndex is where song id is in the playlist, -1 for nowhere.
func (e *Engine) songIndex(id string) int {
	for i := range e.s.Songs {
		if e.s.Songs[i].ID == id {
			return i
		}
	}
	return -1
}

// load readies song id, and reports whether it could.
func (e *Engine) load(id string) bool {
	g := e.s.Song(id)
	if e.Deck == nil || g == nil {
		return false
	}
	e.song, e.deckErr = id, ""
	e.state = DeckState{}
	e.bar = math.MinInt64
	if err := e.Deck.Load(g.Path, g.Loudness()); err != nil {
		e.deckErr = err.Error()
		return false
	}
	e.gain = g.Loudness()
	e.state.Loaded = true
	return true
}

// play plays the song chosen, loading it, or the first song, where none
// is loaded, and from its start where it played to its end.
func (e *Engine) play() {
	if !e.state.Loaded || e.state.Ended {
		id := e.song
		if e.s.Song(id) == nil && len(e.s.Songs) > 0 {
			id = e.s.Songs[0].ID
		}
		if !e.load(id) {
			return
		}
	}
	if e.state.Pos == 0 {
		e.ev.start = true
	}
	e.Deck.Play()
	e.state.Playing = true
}

// playlist follows the deck, once a tick: the song that ends goes on
// to the next, and the bars are counted.
func (e *Engine) playlist() {
	e.played = e.played[:0]
	if e.Deck == nil {
		return
	}
	e.played, e.rate = e.Deck.Hear(e.played)
	if !e.state.Loaded {
		return
	}
	was := e.state
	st := e.Deck.State()
	e.state = st
	if g := e.s.Song(e.song); g != nil && g.Loudness() != e.gain {
		e.gain = g.Loudness()
		e.Deck.SetGain(e.gain)
	}
	if st.Ended && !was.Ended {
		e.ev.end = true
		if i := e.songIndex(e.song); i >= 0 && i+1 < len(e.s.Songs) && e.load(e.s.Songs[i+1].ID) {
			e.play()
		} else {
			e.Deck.Stop()
			e.state = DeckState{}
		}
		return
	}
	if !st.Playing {
		return
	}
	g := e.s.Song(e.song)
	if g == nil {
		return
	}
	per := 4
	if g.Scan != nil && g.Scan.PerBar > 0 {
		per = g.Scan.PerBar
	}
	if _, beats := g.Clock(st.Pos.Seconds()); beats >= 0 {
		// A bar starts where the count passes into a new one, and, just
		// after a start or a seek, where it has only just begun.
		bar := int64(math.Floor(beats / float64(per)))
		if e.bar != math.MinInt64 && bar != e.bar || e.bar == math.MinInt64 && beats-float64(bar*int64(per)) < barJust {
			e.ev.bar = true
		}
		e.bar = bar
	}
}

// player runs Player module m: its triggers in work the transport, and
// its outputs carry the sound, the song's clock and its triggers.
func (e *Engine) player(m *show.Module, live *Live) {
	for port, c := range map[string]Command{"play": Toggle, "stop": Stop, "next": Next, "back": Back} {
		if fired, _ := e.input(m.ID, port).(bool); fired {
			e.do(c)
		}
	}
	e.values[portKey{m.ID, "audio"}] = Sound{Rate: e.rate, Frames: e.played}
	var c Count
	if g := e.s.Song(e.song); g != nil && e.state.Loaded {
		c.BPM, c.Beats = g.Clock(e.state.Pos.Seconds())
		c.Sure = e.state.Playing
	}
	e.values[portKey{m.ID, "clock"}] = c
	live.Clocks[m.ID] = c
	e.values[portKey{m.ID, "start"}] = e.ev.start
	e.values[portKey{m.ID, "end"}] = e.ev.end
	e.values[portKey{m.ID, "bar"}] = e.ev.bar
}

// Press presses Button module, which fires at the next tick.
func (e *Engine) Press(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pressed[module] = true
}
