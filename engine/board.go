package engine

import (
	"maps"
	"slices"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// A Channel is one channel of one light of the rig: the light, by its
// ID, and the channel's offset in it, from 1.
type Channel struct {
	Light  string
	Offset int
}

// A BoardState is what a Board shows: the lights picked, by ID, in the
// order they were picked; the value each channel its faders hold, 0 to
// 255; and the universe the Mixer it is wired into plays now.
type BoardState struct {
	Picked []string
	Held   map[Channel]byte
	Plays  dmx.Universe
}

// A Fader is one of a Board's channel faders: its name, the role of
// what it sets, "" for a channel with none, and the channels it sets,
// the first picked light's first.
type Fader struct {
	Name     string
	Role     fixtures.Role
	Channels []Channel
}

// Faders are the channel faders for the lights picked in s. One light,
// or several of one type and mode, have a fader for each channel, in
// the light's own order. Lights of different types have a fader for
// each thing they do between them, which sets that channel on each
// light that has it.
func Faders(s *show.Show, picked []string) []Fader {
	cat := s.Catalog()
	var lights []fixtures.Patch
	for _, id := range picked {
		if p := s.Light(id); p != nil {
			lights = append(lights, *p)
		}
	}
	if len(lights) == 0 {
		return nil
	}
	same := !slices.ContainsFunc(lights, func(p fixtures.Patch) bool {
		return p.DefinitionID != lights[0].DefinitionID || p.ModeID != lights[0].ModeID
	})
	var out []Fader
	if same {
		for _, c := range cat.ResolvedChannels(lights[0]) {
			f := Fader{Name: c.Channel.Name, Role: c.Channel.Role}
			if f.Role != "" {
				f.Name = f.Role.Label()
			}
			for _, p := range lights {
				f.Channels = append(f.Channels, Channel{Light: p.ID, Offset: c.Offset})
			}
			out = append(out, f)
		}
		return out
	}
	for _, r := range cat.RolesInGroup(lights) {
		f := Fader{Name: r.Label(), Role: r}
		for _, p := range lights {
			if c, ok := cat.ChannelForRole(p, r); ok {
				f.Channels = append(f.Channels, Channel{Light: p.ID, Offset: c.Offset})
			}
		}
		out = append(out, f)
	}
	return out
}

// board is a Board module's state, which the show does not keep: the
// lights picked and the channels held. A change makes a new slice or
// map, never changes one, so what a tick reported stays as it was.
type board struct {
	picked []string
	held   map[Channel]byte
	// over is what the faders hold, by universe slot from 0, each value
	// one up, so that 0 says the slot is not held; any says one is.
	over [dmx.Channels]uint16
	any  bool
}

// Pick picks lights on Board module: those alone, or, with toggle,
// adds them to what is picked, or takes them out where all of them are
// picked already. A light the rig does not have is left out.
func (e *Engine) Pick(module string, lights []string, toggle bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b := e.boards[module]
	if b == nil {
		return
	}
	lights = slices.DeleteFunc(slices.Clone(lights), func(id string) bool { return e.s.Light(id) == nil })
	if !toggle {
		b.picked = lights
		return
	}
	all := len(lights) > 0
	for _, id := range lights {
		all = all && slices.Contains(b.picked, id)
	}
	if all {
		b.picked = slices.DeleteFunc(slices.Clone(b.picked), func(id string) bool { return slices.Contains(lights, id) })
		return
	}
	picked := slices.Clone(b.picked)
	for _, id := range lights {
		if !slices.Contains(picked, id) {
			picked = append(picked, id)
		}
	}
	b.picked = picked
}

// Hold has Board module's faders hold each of chans at v.
func (e *Engine) Hold(module string, chans []Channel, v byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b := e.boards[module]
	if b == nil {
		return
	}
	held := maps.Clone(b.held)
	if held == nil {
		held = map[Channel]byte{}
	}
	for _, c := range chans {
		if p := e.s.Light(c.Light); p != nil && c.Offset >= 1 && c.Offset <= e.s.Catalog().ChannelCount(*p) {
			held[c] = v
		}
	}
	b.held = held
}

// ClearBoard lets go of every channel Board module's faders hold.
func (e *Engine) ClearBoard(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if b := e.boards[module]; b != nil {
		b.held = nil
	}
}

// keepBoards gives each Board of the show its state, kept by module
// from the show before; lights gone from the rig, and channels gone
// from their lights, drop out of it.
func (e *Engine) keepBoards(cat fixtures.Catalog) {
	keep := map[string]bool{}
	for _, m := range e.s.Modules {
		if m.Kind != show.Board {
			continue
		}
		keep[m.ID] = true
		b := e.boards[m.ID]
		if b == nil {
			e.boards[m.ID] = &board{}
			continue
		}
		if slices.ContainsFunc(b.picked, func(id string) bool { return e.s.Light(id) == nil }) {
			b.picked = slices.DeleteFunc(slices.Clone(b.picked), func(id string) bool { return e.s.Light(id) == nil })
		}
		gone := func(c Channel, _ byte) bool {
			p := e.s.Light(c.Light)
			return p == nil || c.Offset > cat.ChannelCount(*p)
		}
		for c, v := range b.held {
			if gone(c, v) {
				held := maps.Clone(b.held)
				maps.DeleteFunc(held, gone)
				b.held = held
				break
			}
		}
	}
	maps.DeleteFunc(e.boards, func(id string, _ *board) bool { return !keep[id] })
}

// playBoard runs Board module m: its layer holds each channel its
// faders hold, as it is, with no curve; a channel nothing holds is left
// out, so the mixer's home value shows.
func (e *Engine) playBoard(m *show.Module, cat fixtures.Catalog, live *Live) {
	l, b := e.layers[m.ID], e.boards[m.ID]
	if l == nil || b == nil {
		return
	}
	l.Clear()
	b.over, b.any = [dmx.Channels]uint16{}, false
	for c, v := range b.held {
		p := e.s.Light(c.Light)
		if p == nil {
			continue
		}
		slot, ok := cat.ResolveChannel(*p, c.Offset)
		if !ok || slot < 1 || slot > dmx.Channels {
			continue
		}
		// Two lights at one address share the slot: the higher value
		// has it, whichever is come to first.
		if uint16(v)+1 > b.over[slot-1] {
			b.over[slot-1], b.any = uint16(v)+1, true
			l.Set(slot, v)
		}
	}
	e.values[portKey{m.ID, "out"}] = l
	live.Boards[m.ID] = BoardState{Picked: b.picked, Held: b.held}
}

// boardsInto calls f with each Board wired into Mixer module mixer
// whose faders hold a channel.
func (e *Engine) boardsInto(mixer string, f func(id string, b *board)) {
	if len(e.boards) == 0 {
		return
	}
	for _, w := range e.s.Wires {
		if w.To.Module != mixer {
			continue
		}
		if b := e.boards[w.From.Module]; b != nil && b.any {
			f(w.From.Module, b)
		}
	}
}

// fadersOver puts what the faders of the Boards wired into Mixer module
// mixer hold over u, the universe it merged: a fader held plays what it
// says, whatever else is wired into the mixer, under the grand master.
func (e *Engine) fadersOver(mixer string, master float64, u *dmx.Universe) {
	e.boardsInto(mixer, func(_ string, b *board) {
		for i, v := range &b.over {
			if v == 0 {
				continue
			}
			u[i] = byte(v - 1)
			if e.dims[i] && master < 1 {
				u[i] = dmx.Clamp8f(float64(u[i]) * master)
			}
		}
	})
}

// Forget lets go of what a show does not keep, as another show takes
// its place: what each Board has picked and holds, and the overrides
// held.
func (e *Engine) Forget() {
	e.mu.Lock()
	defer e.mu.Unlock()
	clear(e.boards)
	clear(e.held)
}

// boardsPlay puts in each Board's state what the Mixer it is wired into
// played this tick.
func (e *Engine) boardsPlay(live *Live) {
	for id, st := range live.Boards {
		if u, ok := e.values[portKey{MixerOf(&e.s, id), "out"}].(dmx.Universe); ok {
			st.Plays = u
			live.Boards[id] = st
		}
	}
}

// MixerOf is the Mixer Board module board's output is wired into, the
// first in the show's order, "" for none.
func MixerOf(s *show.Show, board string) string {
	for _, m := range s.Modules {
		if m.Kind != show.Mixer {
			continue
		}
		for _, w := range s.Wires {
			if w.From.Module == board && w.From.Port == "out" && w.To.Module == m.ID {
				return m.ID
			}
		}
	}
	return ""
}

// OutOf is the first DMX out, in the show's order, that Mixer module
// mixer feeds, "" for none.
func OutOf(s *show.Show, mixer string) string {
	for _, m := range s.Modules {
		if m.Kind != show.DMXOut {
			continue
		}
		for _, w := range s.Wires {
			if w.From.Module == mixer && w.To.Module == m.ID {
				return m.ID
			}
		}
	}
	return ""
}
