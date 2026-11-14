package engine

import (
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// A Made is what a Board made, for the show to keep on Board module
// Module: a chase, or, where Chase is nil, the speed its chase was
// tapped to, in steps a minute.
type Made struct {
	Module string
	Chase  *show.Chase
	Speed  float64
}

// keptChase is a chase a Board made. handed says the application has
// it, so the next show set holds it.
type keptChase struct {
	chase  show.Chase
	handed bool
}

// The chase's taps: the longest gap between two taps of one speed, and
// how many taps it counts.
const (
	chaseTapGap = 3 * time.Second
	chaseTaps   = 4
)

// ClampSpeed is v, in steps a minute, within the speeds a chase runs
// at, to a whole step a minute.
func ClampSpeed(v float64) float64 { return math.Round(min(max(v, show.MinSpeed), show.MaxSpeed)) }

// NewChase has Board module start making a chase: the pads pressed from
// now on are its steps, in order. Record waits no more.
func (e *Engine) NewChase(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.beginChase(module)
}

// beginChase is [Engine.NewChase], with the engine held.
func (e *Engine) beginChase(module string) {
	b := e.boards[module]
	if b == nil || b.making {
		return
	}
	b.making, b.steps, b.record = true, nil, false
}

// DoneChase ends the chase Board module is making: with a step or more,
// it is a chase, named "Chase" and its ID, which the show is to keep;
// with none, it is given up.
func (e *Engine) DoneChase(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.endChase(module)
}

// endChase is [Engine.DoneChase], with the engine held.
func (e *Engine) endChase(module string) {
	b, m := e.boards[module], e.s.Module(module)
	if b == nil || m == nil || !b.making {
		return
	}
	steps := b.steps
	b.making, b.steps = false, nil
	if len(steps) == 0 {
		return
	}
	id := m.NextChase()
	for _, k := range b.chases {
		id = max(id, k.chase.ID+1)
	}
	c := show.Chase{ID: id, Name: "Chase " + strconv.Itoa(id), Steps: slices.Clone(steps)}
	b.chases = append(b.chases, keptChase{chase: c})
	c = c.Clone()
	e.made = append(e.made, Made{Module: m.ID, Chase: &c})
	if e.changed != nil {
		e.changed()
	}
}

// GiveUpChase has Board module make no chase: the steps pressed so far
// are let go.
func (e *Engine) GiveUpChase(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dropChase(module)
}

// dropChase is [Engine.GiveUpChase], with the engine held.
func (e *Engine) dropChase(module string) {
	if b := e.boards[module]; b != nil {
		b.making, b.steps = false, nil
	}
}

// PressChase presses Board module's chase id: it runs, from its first
// step, in place of any that ran; where it runs, it stops.
func (e *Engine) PressChase(module string, id int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.chaseDown(module, id)
}

// chaseDown is [Engine.PressChase], with the engine held.
func (e *Engine) chaseDown(module string, id int) {
	b, m := e.boards[module], e.s.Module(module)
	if b == nil || m == nil {
		return
	}
	if b.run == id {
		b.run = 0
		return
	}
	if c, ok := b.chase(m, id); ok {
		b.start(m, c)
	}
}

// PressLastChase stops the chase Board module runs, or, with none
// running, runs the chase it ran last again.
func (e *Engine) PressLastChase(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastChaseDown(module)
}

// lastChaseDown is [Engine.PressLastChase], with the engine held.
func (e *Engine) lastChaseDown(module string) {
	b, m := e.boards[module], e.s.Module(module)
	if b == nil || m == nil {
		return
	}
	if b.run != 0 {
		b.run = 0
		return
	}
	if c, ok := b.chase(m, b.last); ok {
		b.start(m, c)
	}
}

// TapChase taps Board module's chase: two taps or more a steady gap
// apart set its speed, which the show is to keep, and the chase that
// runs moves on a step on the tap. A tap long after the last starts
// counting afresh.
func (e *Engine) TapChase(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tapDown(module)
}

// tapDown is [Engine.TapChase], with the engine held.
func (e *Engine) tapDown(module string) {
	b, m := e.boards[module], e.s.Module(module)
	if b == nil || m == nil {
		return
	}
	now := e.now()
	if n := len(b.taps); n > 0 {
		if gap := now.Sub(b.taps[n-1]); gap > chaseTapGap || gap < 0 {
			b.taps = b.taps[:0]
		}
	}
	b.taps = append(b.taps, now)
	if n := len(b.taps); n > chaseTaps {
		b.taps = slices.Delete(b.taps, 0, n-chaseTaps)
	}
	if n := len(b.taps); n >= 2 {
		gap := b.taps[n-1].Sub(b.taps[0]).Seconds() / float64(n-1)
		speed := show.MaxSpeed
		if gap > 0 {
			speed = ClampSpeed(60 / gap)
		}
		if speed != b.speedOf(m) {
			b.speed, b.handed = speed, false
			e.made = append(e.made, Made{Module: m.ID, Speed: speed})
			if e.changed != nil {
				e.changed()
			}
		}
	}
	if b.run == 0 {
		return
	}
	c, ok := b.chase(m, b.run)
	if !ok {
		return
	}
	b.stepOn(m, c)
	// The step falls on the tap: the next tick counts on from it, not
	// from the tick before.
	var since float64
	if !e.last.IsZero() {
		since = min(max(now.Sub(e.last).Seconds(), 0), 0.25)
	}
	b.into = -since * b.speedOf(m) / 60
}

// Made returns the chases the Boards made and the speeds tapped since
// it was last asked, for the show to keep. A Board plays each until the
// next show set, which should hold it.
func (e *Engine) Made() []Made {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.made
	e.made = nil
	for _, d := range out {
		b := e.boards[d.Module]
		if b == nil {
			continue
		}
		if d.Chase == nil {
			b.handed = b.handed || b.speed == d.Speed
			continue
		}
		for i := range b.chases {
			if b.chases[i].chase.ID == d.Chase.ID {
				b.chases[i].handed = true
			}
		}
	}
	return out
}

// chase is chase id of Board m, b its state: one made that the show
// does not hold yet, or the show's.
func (b *board) chase(m *show.Module, id int) (show.Chase, bool) {
	if id == 0 {
		return show.Chase{}, false
	}
	for _, k := range b.chases {
		if k.chase.ID == id {
			return k.chase, true
		}
	}
	if c := m.Chase(id); c != nil {
		return *c, true
	}
	return show.Chase{}, false
}

// speedOf is how many steps a minute Board m's chase runs at, b its
// state: as tapped, where the show does not hold that yet.
func (b *board) speedOf(m *show.Module) float64 {
	if b.speed > 0 {
		return b.speed
	}
	if m.Speed <= 0 {
		return show.HomeSpeed
	}
	return ClampSpeed(m.Speed)
}

// start runs chase c of Board m, at once at its first step it can play.
func (b *board) start(m *show.Module, c show.Chase) {
	b.run, b.last = c.ID, c.ID
	b.at, b.from, b.into, b.heard = -1, -1, 0, false
	b.at = b.next(m, c, -1)
}

// stepOn moves the chase c of Board m that runs on to its next step it
// can play, fading from the one it was at.
func (b *board) stepOn(m *show.Module, c show.Chase) {
	next := b.next(m, c, b.at)
	b.from = -1
	if b.at >= 0 && b.at < len(c.Steps) && next >= 0 {
		b.from = c.Steps[b.at]
	}
	b.at = next
}

// next is the place in chase c of Board m of the first step after the
// one at after whose pad has a scene, round to the first after the
// last; -1 for none.
func (b *board) next(m *show.Module, c show.Chase, after int) int {
	for k := 1; k <= len(c.Steps); k++ {
		i := (after + k) % len(c.Steps)
		if i < 0 {
			i += len(c.Steps)
		}
		if _, ok := b.scene(m, c.Steps[i]); ok {
			return i
		}
	}
	return -1
}

// keepChases drops from Board m's state, b, the chases made and the
// speed tapped that the show now holds, and stops the chase that runs,
// and forgets the last run, where the show has it no more.
func (e *Engine) keepChases(m *show.Module, b *board) {
	b.chases = slices.DeleteFunc(b.chases, func(k keptChase) bool {
		c := m.Chase(k.chase.ID)
		return c != nil && (k.handed || slices.Equal(c.Steps, k.chase.Steps))
	})
	if b.speed > 0 && (b.handed || m.Speed == b.speed) {
		b.speed, b.handed = 0, false
	}
	if _, ok := b.chase(m, b.run); !ok {
		b.run = 0
	}
	if _, ok := b.chase(m, b.last); !ok {
		b.last = 0
	}
}

// chaseOn runs the chase Board m runs, b its state, dt seconds on from
// the last tick, and calls put with each channel it plays: its slot,
// from 0, its value and its level. Following the beat of clock, it
// steps on each beat the clock is sure of, and holds its step through a
// break; with no clock, or one with no tempo, it runs at its own speed.
// However long the tick, it moves on no more than a step, so a long
// pause does not race it through its steps. It reports whether the
// chase follows the beat.
//
// From one step to the next it fades over the share of each step the
// fade says: a channel the grand master dims that both set crosses from
// one value to the other; one only the step before sets fades out to
// what plays under it, and one only the next sets fades in from it. A
// channel the master does not dim takes the next step's value at once.
func (e *Engine) chaseOn(m *show.Module, b *board, cat fixtures.Catalog, dt float64, clock Count,
	put func(slot int, v byte, level float64)) (following bool) {
	following = m.FollowBeat && clock.BPM > 0
	if b.run == 0 {
		return following
	}
	c, ok := b.chase(m, b.run)
	if !ok {
		b.run = 0
		return following
	}
	// A step gone from under it, as a scene deleted or the chase's last
	// step taken out, gives way to the next it can play.
	if b.at < 0 || b.at >= len(c.Steps) {
		b.at = b.next(m, c, -1)
	} else if _, ok := b.scene(m, c.Steps[b.at]); !ok {
		b.at = b.next(m, c, b.at-1)
	}
	if following {
		n := int64(math.Floor(clock.Beats))
		if b.heard && n != b.beat {
			if clock.Sure {
				b.stepOn(m, c)
			} else {
				// A break: the step holds, whole.
				b.from = -1
			}
		}
		b.beat, b.heard = n, true
		_, b.into = math.Modf(clock.Beats)
		if b.into < 0 {
			b.into++
		}
	} else {
		b.heard = false
		b.into += dt * b.speedOf(m) / 60
		if b.into >= 1 {
			b.into -= math.Floor(b.into)
			b.stepOn(m, c)
		}
	}
	if b.at < 0 {
		return following
	}
	t := 1.0
	if share := min(m.Fade, 1); b.from >= 0 && share > 0 {
		t = min(max(b.into/share, 0), 1)
	}
	// What each step sets, by slot, each value one up, so that 0 says it
	// sets nothing there.
	var from, to [dmx.Channels]uint16
	e.sceneSlots(m, b, cat, c.Steps[b.at], &to)
	if t < 1 {
		e.sceneSlots(m, b, cat, b.from, &from)
	}
	for i := range dmx.Channels {
		a, z := from[i], to[i]
		switch {
		case a == 0 && z == 0:
		case !e.dims[i]:
			if z != 0 {
				put(i, byte(z-1), 1)
			}
		case a != 0 && z != 0:
			put(i, byte(math.Round(float64(a-1)+t*(float64(z)-float64(a)))), 1)
		case a != 0:
			if t < 1 {
				put(i, byte(a-1), 1-t)
			}
		default:
			if t > 0 {
				put(i, byte(z-1), t)
			}
		}
	}
	return following
}

// sceneSlots sets in dst each slot the scene on pad of Board m, b its
// state, sets, from 0, to its value one up; the higher where two of its
// lights share a slot. A pad with no scene sets none.
func (e *Engine) sceneSlots(m *show.Module, b *board, cat fixtures.Catalog, pad int, dst *[dmx.Channels]uint16) {
	sc, ok := b.scene(m, pad)
	if !ok {
		return
	}
	for id, chans := range sc.Values {
		for off, v := range chans {
			if slot, ok := e.slotOf(cat, Channel{Light: id, Offset: off}); ok {
				dst[slot-1] = max(dst[slot-1], uint16(v)+1)
			}
		}
	}
}
