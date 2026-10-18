package engine

import (
	"math"
	"strconv"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim-dmx/mix"
)

// steps is how many light inputs a Chaser or a Cue list has.
const steps = 8

// stepPrefix starts a Chaser's or a Cue list's light inputs, by kind.
func stepPrefix(k show.Kind) (id, name string) {
	if k == show.Cues {
		return "cue", "Cue "
	}
	return "step", "Step "
}

// stepPorts are a Chaser's or a Cue list's light inputs.
func stepPorts(k show.Kind) []Port {
	id, name := stepPrefix(k)
	out := make([]Port, steps)
	for i := range steps {
		n := strconv.Itoa(i + 1)
		out[i] = Port{id + n, name + n, Light}
	}
	return out
}

// A stepper is a Chaser's or a Cue list's place: the step it is at and
// the one it fades from, by input, -1 for none, how far the fade has
// come, from 0 to 1, and the beat its clock counted last.
type stepper struct {
	at, from int
	t        float64
	beat     int64
	clocked  bool
	// moves are the steps on, or back, asked for from the Live tab
	// since the last tick.
	moves int
}

// Step moves Chaser or Cue list module on a step, or back with by -1,
// at the next tick, as a trigger into its next, go or back would.
func (e *Engine) Step(module string, by int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st := e.steppers[module]; st != nil {
		st.moves += by
	}
}

// Stepping is what a Chaser or a Cue list shows: the step it is at,
// from 0, and -1 before the first, and the steps wired into it, by
// input number.
type Stepping struct {
	At    int
	Steps []int
}

// look runs Look module m: its levels on its lights, at the level wired
// in, or its own.
func (e *Engine) look(m *show.Module, cat fixtures.Catalog, live *Live) {
	l := e.layers[m.ID]
	if l == nil {
		return
	}
	l.Clear()
	level := m.Value
	if v, ok := e.input(m.ID, "level").(float64); ok {
		level = v
	}
	level = min(max(level, 0), 1)
	if level > 0 {
		for _, id := range m.Lights {
			p := e.s.Light(id)
			if p == nil {
				continue
			}
			for role, v := range m.Levels {
				c, ok := cat.ChannelForRole(*p, fixtures.Role(role))
				if !ok || !c.InUniverse {
					continue
				}
				l.Set(c.Slot, byte(fixtures.CurveValue(*p, c.Channel, int(dmx.Level(v*level)))))
			}
		}
	}
	e.values[portKey{m.ID, "out"}] = l
	live.Values[m.ID] = level
}

// stepOn runs Chaser or Cue list m, dt after the last tick: it moves on
// as it is told, or with its clock, fades from step to step, and its
// output carries where it is.
func (e *Engine) stepOn(m *show.Module, dt float64, live *Live) {
	l := e.layers[m.ID]
	st := e.steppers[m.ID]
	if l == nil || st == nil {
		return
	}
	prefix, _ := stepPrefix(m.Kind)
	var wired []int
	layers := map[int]*mix.Layer{}
	for i := range steps {
		if in, ok := e.input(m.ID, prefix+strconv.Itoa(i+1)).(*mix.Layer); ok {
			wired = append(wired, i)
			layers[i] = in
		}
	}
	move := func(by int) {
		if len(wired) == 0 {
			return
		}
		k := indexOf(wired, st.at)
		switch {
		case m.Kind == show.Chaser:
			k = (k + by + len(wired)) % len(wired)
		case k+by < 0, k+by >= len(wired):
			// A cue list stops at its ends.
			return
		default:
			k += by
		}
		st.from, st.at, st.t = st.at, wired[k], 0
		if by < 0 {
			// Back cuts, rather than fades.
			st.t = 1
		}
	}
	for ; st.moves > 0; st.moves-- {
		move(1)
	}
	for ; st.moves < 0; st.moves++ {
		move(-1)
	}
	switch m.Kind {
	case show.Chaser:
		if fired, _ := e.input(m.ID, "next").(bool); fired {
			move(1)
		}
		c, ok := e.input(m.ID, "clock").(Count)
		st.clocked = ok && c.BPM > 0
		if st.clocked {
			every := m.Every
			if every <= 0 {
				every = 1
			}
			n := int64(math.Floor(c.Beats / every))
			if n != st.beat && c.Sure {
				move(1)
			}
			st.beat = n
			// The fade runs over the share of the step Fade says.
			_, f := math.Modf(c.Beats / every)
			if f < 0 {
				f++
			}
			st.t = 1
			if m.Fade > 0 {
				st.t = min(f/m.Fade, 1)
			}
		} else {
			st.t = 1
		}
		if st.at < 0 && len(wired) > 0 {
			st.at, st.t = wired[0], 1
		}
	case show.Cues:
		if fired, _ := e.input(m.ID, "go").(bool); fired {
			move(1)
		}
		if fired, _ := e.input(m.ID, "back").(bool); fired {
			move(-1)
		}
		if m.Fade > 0 {
			st.t = min(st.t+dt/m.Fade, 1)
		} else {
			st.t = 1
		}
	case show.Fader, show.Lights, show.Mixer, show.DMXOut, show.StageView, show.AudioIn, show.Beat, show.Tempo,
		show.Level, show.Pulse, show.Player, show.Button, show.Controller, show.MIDIOut, show.Map, show.Every,
		show.Toggle, show.Look:
	}
	blend(l, layers[st.from], layers[st.at], st.t)
	e.values[portKey{m.ID, "out"}] = l
	live.Steps[m.ID] = Stepping{At: indexOf(wired, st.at), Steps: wired}
}

// indexOf is where v is in s, -1 for nowhere.
func indexOf(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// blend sets dst to a faded into b by t, from 0, all a, to 1, all b; a
// channel one of them leaves fades from or to 0.
func blend(dst, a, b *mix.Layer, t float64) {
	dst.Clear()
	for ch := 1; ch <= dmx.Channels; ch++ {
		var va, vb byte
		var oka, okb bool
		if a != nil && t < 1 {
			va, oka = a.Get(ch)
		}
		if b != nil && t > 0 {
			vb, okb = b.Get(ch)
		}
		if !oka && !okb {
			continue
		}
		// A layer that leaves a channel alone says nothing of it.
		if !oka {
			va = 0
		}
		if !okb {
			vb = 0
		}
		dst.Set(ch, byte(math.Round(float64(va)*(1-t)+float64(vb)*t)))
	}
}

// The overrides a Mixer has, by their inputs' IDs.
const (
	Blackout  = "blackout"
	FlashAll  = "flash"
	HoldStill = "hold"
)

// overridePorts are a Mixer's override inputs.
var overridePorts = []Port{{Blackout, "Blackout", Value}, {FlashAll, "Flash white", Value}, {HoldStill, "Hold", Value}}

// Override holds Mixer module's override which on, or lets it go, as
// the Live tab's buttons do; a value wired into it holds it too.
func (e *Engine) Override(module, which string, on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := portKey{module, which}
	if on {
		e.held[k] = true
	} else {
		delete(e.held, k)
	}
}

// overridden reports whether Mixer m's override which is on.
func (e *Engine) overridden(m, which string) bool {
	if e.held[portKey{m, which}] {
		return true
	}
	v, _ := e.input(m, which).(float64)
	return v >= 0.5
}

// overrideAll applies Mixer m's overrides to what it merged: held, it
// plays what it played as the hold began; flashed, every light full
// and white; blacked out, every light dark. It reports which are on.
func (e *Engine) overrideAll(m string, u dmx.Universe) (out dmx.Universe, on [3]bool) {
	on = [3]bool{e.overridden(m, Blackout), e.overridden(m, FlashAll), e.overridden(m, HoldStill)}
	if on[2] {
		if was, ok := e.frozen[m]; ok {
			u = was
		} else {
			e.frozen[m] = u
		}
	} else {
		delete(e.frozen, m)
	}
	if on[1] {
		for _, slot := range e.flashSlots {
			u[slot-1] = 255
		}
	}
	if on[0] {
		for i := range u {
			if e.dims[i] {
				u[i] = 0
			}
		}
	}
	return u, on
}

// flashRoles are what a flash brings to full: every light's dimmer, and
// its white, or its red, green and blue, which make white.
var flashRoles = []fixtures.Role{fixtures.RoleDimmer, fixtures.RoleWhite, fixtures.RoleRed, fixtures.RoleGreen, fixtures.RoleBlue}

// flashSlotsOf are the slots a flash brings to full in s.
func flashSlotsOf(s *show.Show, cat fixtures.Catalog) []int {
	var out []int
	for _, p := range s.Rig.Lights {
		for _, r := range flashRoles {
			if c, ok := cat.ChannelForRole(p, r); ok && c.InUniverse {
				out = append(out, c.Slot)
			}
		}
	}
	return out
}

// owners notes, for Mixer m, which module's light each slot it merges
// comes from: the one setting it highest, as the mixer merges, or the
// mixer itself where an override sets it.
func (e *Engine) owners(m string, on [3]bool, live *Live) {
	if live.Owners == nil {
		live.Owners = map[int]string{}
	}
	if on[0] || on[1] || on[2] {
		for i := range dmx.Channels {
			if e.dims[i] {
				live.Owners[i+1] = m
			}
		}
		if on[0] || on[2] {
			return
		}
	}
	best := map[int]byte{}
	for _, l := range e.heard[m] {
		owner := e.layerOf[l]
		for ch := 1; ch <= dmx.Channels; ch++ {
			v, ok := l.Get(ch)
			if !ok {
				continue
			}
			if was, seen := best[ch]; !seen || v > was {
				best[ch] = v
				if !on[1] || !e.dims[ch-1] {
					live.Owners[ch] = owner
				}
			}
		}
	}
}

// A Note is a line of the Monitor's log: when, and what happened.
type Note struct {
	At   time.Time
	Text string
}

// logKeep is how many lines the log keeps.
const logKeep = 200

// note adds a line to the log.
func (e *Engine) note(now time.Time, text string) {
	e.log = append(e.log, Note{At: now, Text: text})
	if len(e.log) > logKeep {
		e.log = e.log[len(e.log)-logKeep:]
	}
}
