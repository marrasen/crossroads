package engine

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// A remote is a Controller as it plays a Board: the Board, by ID; the
// page it is on, 0 or 1; and what it heard this tick, in order.
type remote struct {
	board string
	page  int
	acts  []act
	// pos is where the fader stood when last heard, and moved says it
	// was heard since the remote began, or the controller came back.
	// caught says the fader has picked up the grand master, which it set
	// last to set.
	pos           float64
	moved, caught bool
	set           float64
	// mixer is the Mixer whose grand master the fader was last heard
	// for, "" for none.
	mixer string
	// took is how many steps the Board's chase had taken as the Tap
	// light last looked, stepped when it took the last, and flash when
	// that light goes out.
	took    int
	stepped time.Time
	flash   time.Time
}

// An act is a control a remote heard: a knob turned ticks steps, the
// fader moved to at, 0 to 1, or a button pressed down.
type act struct {
	control string
	ticks   int
	at      float64
}

// boardLights are what a Board shows on the controller that plays it:
// each knob's ring, 0 to 1, and each button's light, as the velocity
// that lights it, by control.
type boardLights struct {
	rings  map[string]float64
	lights map[string]int
}

// A Master is a grand master a controller moved, for the show to keep:
// the Mixer module's, and its value, 0 to 1.
type Master struct {
	Module string
	Value  float64
}

// A master is a grand master a controller set that the show does not
// hold yet: the value it plays; the show's own, as last seen; the value
// handed to the application, -1 for none; when it was last set; and
// whether that value was handed over.
type master struct {
	v, shown, handed float64
	at               time.Time
	queued           bool
}

const (
	// pageSize is how many channel faders, and how many pads, a page
	// holds: one for each knob, and each button of the top row.
	pageSize = 8
	// knobDMX is how far a knob moves its fader's DMX value a step.
	knobDMX = 3
	// tapFlash is how long the Tap light flashes on each step.
	tapFlash = 80 * time.Millisecond
	// masterRest is how long the fader rests before the grand master it
	// set is handed to the application.
	masterRest = time.Second
)

// The bottom row's buttons, as a Board has them.
const (
	lightBefore    = "button9"
	lightAfter     = "button10"
	recordButton   = "button11"
	clearButton    = "button12"
	chaseButton    = "button13"
	tapButton      = "button14"
	flashButton    = "button15"
	blackoutButton = "button16"
)

// keepRemotes gives each Controller that plays a Board a remote: the
// first Board, in the show's order, played from it, where it has the
// controls. A controller that begins or stops playing a Board, or plays
// another, starts afresh on the first page, and its lights are all sent
// again, as the Board, or its wires, now have them.
func (e *Engine) keepRemotes() {
	want := map[string]string{}
	for i := range e.s.Modules {
		m := &e.s.Modules[i]
		if m.Kind != show.Board || m.PlayedFrom == "" {
			continue
		}
		if _, taken := want[m.PlayedFrom]; taken || !controller.PlaysBoard(e.s.Module(m.PlayedFrom)) {
			continue
		}
		want[m.PlayedFrom] = m.ID
	}
	for id, rm := range e.remotes {
		if want[id] != rm.board {
			delete(e.remotes, id)
			e.relight(id)
		}
	}
	for id, board := range want {
		if e.remotes[id] != nil {
			continue
		}
		rm := &remote{board: board}
		if b := e.boards[board]; b != nil {
			rm.took = b.took
		}
		e.remotes[id] = rm
		e.relight(id)
	}
}

// relight has Controller module id's lights all sent again.
func (e *Engine) relight(id string) {
	if d := e.desks[id]; d != nil {
		clear(d.sent)
	}
}

// playRemote plays on its Board what remote rm heard this tick, in
// order. Knob n turns the nth channel fader of its page from where it
// stands, a step 3 of 255 times the speed it turned at, and holds it
// there; pressed, it lets go of that fader. The top row presses the
// page's pads, as on screen. The bottom row picks the light before and
// the one after, presses Record, Clear, the chase last run, Tap, Flash
// while held, and Blackout. Layer A and B turn to page 1 and 2. The
// fader sets the grand master, once it has picked it up.
func (e *Engine) playRemote(rm *remote) {
	b := e.boards[rm.board]
	if b == nil || len(rm.acts) == 0 {
		return
	}
	cat := e.s.Catalog()
	mixer := MixerOf(&e.s, rm.board)
	for _, a := range rm.acts {
		if n, ok := numbered(a.control, "knob"); ok {
			if f, ok := e.pageFader(rm, b, n); ok {
				v := int(e.standsAt(cat, b, mixer, f)) + knobDMX*a.ticks
				e.holdAt(rm.board, f.Channels, byte(min(max(v, 0), 255)))
			}
			continue
		}
		if n, ok := numbered(a.control, "push"); ok {
			if f, ok := e.pageFader(rm, b, n); ok {
				e.letGo(rm.board, f.Channels)
			}
			continue
		}
		if n, ok := numbered(a.control, "button"); ok && n <= pageSize {
			e.padDown(rm.board, rm.page*pageSize+n-1)
			continue
		}
		switch a.control {
		case lightBefore, lightAfter:
			e.pickBeside(rm.board, b, a.control == lightAfter)
		case recordButton:
			e.recordDown(rm.board)
		case clearButton:
			e.letGoAll(rm.board)
		case chaseButton:
			e.lastChaseDown(rm.board)
		case tapButton:
			e.tapDown(rm.board)
		case blackoutButton:
			// Toggled, as the Board tab's button does.
			if mixer != "" {
				if k := (portKey{mixer, Blackout}); e.overridden(mixer, Blackout) {
					delete(e.held, k)
				} else {
					e.held[k] = true
				}
			}
		case "layerA":
			rm.page = 0
		case "layerB":
			rm.page = 1
		case "fader":
			e.pickUp(rm, mixer, a.at)
		}
	}
}

// numbered is the number control id has after prefix, as 3 for
// "knob3", and false where it has none.
func numbered(id, prefix string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	return n, err == nil && strings.HasPrefix(id, prefix) && n >= 1
}

// pageFader is the channel fader knob n, from 1, of remote rm sets on
// page it is on, of Board state b; false for none, as with nothing
// picked.
func (e *Engine) pageFader(rm *remote, b *board, n int) (Fader, bool) {
	fs := Faders(&e.s, b.picked)
	i := rm.page*pageSize + n - 1
	if i < 0 || i >= len(fs) || len(fs[i].Channels) == 0 {
		return Fader{}, false
	}
	return fs[i], true
}

// standsAt is where fader f of Board state b stands, 0 to 255: what it
// holds, or, not held, what its first channel played last in Mixer
// module mixer, before the grand master and the overrides, so that a
// knob turned a step leaves the light a step from where it was.
func (e *Engine) standsAt(cat fixtures.Catalog, b *board, mixer string, f Fader) byte {
	c := f.Channels[0]
	if v, ok := b.held[c]; ok {
		return v
	}
	u, played := e.raw[mixer]
	slot, ok := e.slotOf(cat, c)
	if !played || !ok {
		return 0
	}
	return u[slot-1]
}

// pickBeside picks on Board module, b its state, the light after the
// one picked, alone, in the rig's order, or, where after is false, the
// one before, round from the last to the first and back. With nothing
// picked, or several, it picks the first light, or the last.
func (e *Engine) pickBeside(module string, b *board, after bool) {
	lights := e.s.Rig.Lights
	n := len(lights)
	if n == 0 {
		return
	}
	i := -1
	if len(b.picked) == 1 {
		i = slices.IndexFunc(lights, func(p fixtures.Patch) bool { return p.ID == b.picked[0] })
	}
	switch {
	case i < 0 && after:
		i = 0
	case i < 0:
		i = n - 1
	case after:
		i = (i + 1) % n
	default:
		i = (i + n - 1) % n
	}
	e.pickLights(module, []string{lights[i].ID}, false)
}

// pickUp has remote rm's fader, moved to at, set the grand master of
// Mixer module mixer once it has picked it up: as it passes the
// master's value, or comes near it. With the master moved since by
// other means, as on screen, it picks it up afresh.
func (e *Engine) pickUp(rm *remote, mixer string, at float64) {
	m := e.s.Module(mixer)
	if mixer != rm.mixer {
		// Another mixer, or none: its master is picked up afresh.
		rm.mixer, rm.caught = mixer, false
	}
	if m == nil {
		rm.pos, rm.moved = at, true
		return
	}
	if rm.caught && m.Value != rm.set {
		rm.caught = false
	}
	if !rm.caught {
		near := math.Abs(at-m.Value) <= catchWithin
		passed := rm.moved && (rm.pos-m.Value)*(at-m.Value) <= 0
		rm.pos, rm.moved = at, true
		if !near && !passed {
			return
		}
		rm.caught = true
	}
	rm.pos = at
	e.setMaster(m, at)
	rm.set = m.Value
}

// setMaster has Mixer module m play grand master v at once, and hands
// it to the application once the fader has rested.
func (e *Engine) setMaster(m *show.Module, v float64) {
	if m.Value == v {
		return
	}
	p := e.masters[m.ID]
	if p == nil {
		p = &master{shown: e.shown[m.ID], handed: -1}
		e.masters[m.ID] = p
	}
	p.v, p.at, p.queued = v, e.now(), false
	m.Value = v
}

// keepMasters plays on, in the show just set, each grand master a
// controller set that the show does not hold yet, where the show's
// own is as it was. One the show now holds is let go; so is one whose
// master the show changed by other means, as its fader on screen let
// go, which then has the last word.
func (e *Engine) keepMasters() {
	shown := make(map[string]float64, len(e.shown))
	for i := range e.s.Modules {
		m := &e.s.Modules[i]
		if m.Kind != show.Mixer {
			continue
		}
		shown[m.ID] = m.Value
		p := e.masters[m.ID]
		switch {
		case p == nil:
		case m.Value == p.shown:
			m.Value = p.v
		case m.Value == p.handed && p.v != p.handed:
			// The show holds what was handed over, and the fader moved on
			// since.
			p.shown = m.Value
			m.Value = p.v
		default:
			delete(e.masters, m.ID)
		}
	}
	maps.DeleteFunc(e.masters, func(id string, _ *master) bool {
		_, ok := shown[id]
		return !ok
	})
	e.shown = shown
}

// settleMasters hands each grand master a controller set to the
// application, once its fader has rested: one change to the show for a
// move, not one a tick. One moved since by other means, as on screen,
// is not handed over: that move has the last word.
func (e *Engine) settleMasters(now time.Time) {
	if len(e.masters) == 0 {
		return
	}
	for i := range e.s.Modules {
		m := &e.s.Modules[i]
		p := e.masters[m.ID]
		if p == nil || p.queued || now.Sub(p.at) < masterRest {
			continue
		}
		if m.Value != p.v {
			delete(e.masters, m.ID)
			continue
		}
		p.queued, p.handed = true, p.v
		e.mastered = append(e.mastered, Master{Module: m.ID, Value: p.v})
		if e.changed != nil {
			e.changed()
		}
	}
}

// Masters returns the grand masters the controllers moved since it was
// last asked, for the show to keep. A Mixer plays each until the next
// show set holds it. One whose master the show changed meanwhile by
// other means is left out.
func (e *Engine) Masters() []Master {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := slices.DeleteFunc(e.mastered, func(g Master) bool {
		p := e.masters[g.Module]
		return p == nil || p.handed != g.Value
	})
	e.mastered = nil
	return out
}

// flashHeld reports whether a controller that plays a Board wired into
// Mixer module mixer holds its Flash down.
func (e *Engine) flashHeld(mixer string) bool {
	for id, rm := range e.remotes {
		if d := e.desks[id]; d != nil && d.held[flashButton] && MixerOf(&e.s, rm.board) == mixer {
			return true
		}
	}
	return false
}

// lightRemotes sends each Controller that plays a Board, in the order
// the modules ran, its lights as the Board has them once every module
// has run.
func (e *Engine) lightRemotes(now time.Time) {
	if len(e.remotes) == 0 {
		return
	}
	for _, id := range e.order {
		rm, m, d := e.remotes[id], e.s.Module(id), e.desks[id]
		if rm == nil || m == nil || d == nil {
			continue
		}
		b := e.boards[rm.board]
		if b == nil {
			continue
		}
		if b.took != rm.took {
			// Lit for no more than half the step, so that a fast chase
			// still flashes.
			lit := tapFlash
			if gap := now.Sub(rm.stepped); !rm.stepped.IsZero() && gap > 0 && gap/2 < lit {
				lit = gap / 2
			}
			rm.took, rm.stepped, rm.flash = b.took, now, now.Add(lit)
		}
		out, _ := e.hub.out(m.Device)
		if out == nil {
			continue
		}
		l, _ := controller.Find(m.Layout)
		e.light(m, d, controller.ControlsOf(m), out, l.Mackie, e.boardLights(rm, d, b, now))
	}
}

// boardLights is what Board state b shows on the controller that plays
// it, rm its remote and d its desk. Each knob's ring shows its fader,
// held or as it plays; a knob with no fader is dark. The top row is lit
// while its pad's scene is on, or, while a chase is made, where its pad
// is a step already; all blink while Record waits. The bottom row: the
// light before and after lit while held, Record blinking while it
// waits, Clear lit while a fader holds a value, the chase's while it
// runs, Tap a flash on each step, Flash and Blackout while the mixer's
// are on. The page's Layer button is lit.
func (e *Engine) boardLights(rm *remote, d *desk, b *board, now time.Time) *boardLights {
	bl := &boardLights{rings: map[string]float64{}, lights: map[string]int{}}
	lit := func(on bool) int {
		if on {
			return controller.LightOn
		}
		return controller.LightOff
	}
	cat := e.s.Catalog()
	mixer := MixerOf(&e.s, rm.board)
	fs := Faders(&e.s, b.picked)
	for i := range pageSize {
		n := strconv.Itoa(i + 1)
		if k := rm.page*pageSize + i; k < len(fs) && len(fs[k].Channels) > 0 {
			bl.rings["knob"+n] = float64(e.standsAt(cat, b, mixer, fs[k])) / 255
		}
		pad := rm.page*pageSize + i
		var v int
		switch {
		case b.record:
			v = controller.LightBlink
		case b.making:
			v = lit(slices.Contains(b.steps, pad))
		default:
			f := b.fades[pad]
			v = lit(f != nil && f.on)
		}
		bl.lights["button"+n] = v
	}
	bl.lights[lightBefore] = lit(d.held[lightBefore])
	bl.lights[lightAfter] = lit(d.held[lightAfter])
	bl.lights[recordButton] = controller.LightOff
	if b.record {
		bl.lights[recordButton] = controller.LightBlink
	}
	bl.lights[clearButton] = lit(len(b.held) > 0)
	bl.lights[chaseButton] = lit(b.run != 0)
	bl.lights[tapButton] = lit(b.run != 0 && now.Before(rm.flash))
	bl.lights[flashButton] = lit(mixer != "" && e.overridden(mixer, FlashAll))
	bl.lights[blackoutButton] = lit(mixer != "" && e.overridden(mixer, Blackout))
	bl.lights["layerA"] = lit(rm.page == 0)
	bl.lights["layerB"] = lit(rm.page == 1)
	return bl
}
