package engine

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

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
	// On are the pads whose scenes are on, from 0, in order, and Levels
	// how far each scene that plays has faded in, 0 to 1, by pad: one
	// fading out is among them, and is not on. Record says Record waits
	// for a pad.
	On     []int
	Levels map[int]float64
	Record bool
	// Making says a chase is being made, and Steps are its steps so far,
	// each a pad, from 0.
	Making bool
	Steps  []int
	// Chase is the chase that runs, by ID, 0 for none; Step the step it
	// is at, by its place in the chase's steps, from 0, -1 for none it
	// can play; and Into how far it is into that step, 0 to 1. Last is
	// the chase last run, 0 for none.
	Chase, Step int
	Into        float64
	Last        int
	// Clock is what the clock wired into the Board counts, nothing for
	// none, and Following says the chase steps on its beats, as Follow
	// the beat is on and the clock has a tempo.
	Clock     Count
	Following bool
	// Speed is how many steps a minute its chase runs at with no beat
	// to follow: the show's, or one tapped that the show does not hold
	// yet.
	Speed float64
	// Remote is the Controller module that plays it, by ID, "" for none,
	// and Page the page that controller is on, from 0: its knobs set
	// channel faders, and its top row presses pads, Page times eight on.
	Remote string
	Page   int
}

// A Recorded is a scene a Board recorded, for the show to keep on
// Board module Module.
type Recorded struct {
	Module string
	Scene  show.Scene
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
	// plays is what the scenes that play set this tick, a slot and a
	// scene at a time. any says a fader holds a slot or a scene plays.
	plays []played
	any   bool
	// record says Record waits for a pad. fades are the scenes that
	// play, by pad. kept are the scenes recorded that the show does not
	// hold yet, by pad, which the board plays in place of the show's.
	record bool
	fades  map[int]*fade
	kept   map[int]kept
	// making says a chase is being made, and steps are its steps so far.
	// chases are the chases made that the show does not hold yet, in
	// the order they were made, which the board plays in place of the
	// show's.
	making bool
	steps  []int
	chases []keptChase
	// run is the chase that runs, by ID, 0 for none, and last the chase
	// last run. at is the step it is at, by its place in the chase's
	// steps, -1 for none, and into how far it is into its step, in
	// steps, below 0 just after a tap.
	run, last int
	at        int
	into      float64
	// fading says the chase fades to its step, and faded how far it is
	// into that, in steps. was is what it played as it took the step,
	// which it fades from, and cur what it plays this tick.
	fading   bool
	faded    float64
	was, cur chaseLook
	// beat is the beat the clock it follows counted last and beats where
	// that clock stood, where heard says it follows one.
	beat  int64
	beats float64
	heard bool
	// taps are when the chase was last tapped, the latest last. speed is
	// a speed tapped that the show does not hold yet, 0 for none; queued
	// says the application was handed it; and showSpeed is the show's
	// speed as last seen, by which a change to it is known.
	taps      []time.Time
	speed     float64
	queued    bool
	showSpeed float64
	// took counts the steps the chase took, for a controller's Tap light
	// to flash on.
	took int
}

// A chaseLook is what a chase plays: for each slot, from 0, a value
// and the level it plays it at, 0 where it plays nothing.
type chaseLook struct {
	v     [dmx.Channels]byte
	level [dmx.Channels]float32
}

// played is one channel of a scene that plays: its universe slot, from
// 0, the value the scene holds for it, and the scene's level.
type played struct {
	slot  int
	v     byte
	level float64
}

// A fade is a scene of a Board as it plays: whether it is on, and how
// far it has faded in, 0 to 1.
type fade struct {
	on    bool
	level float64
}

// kept is a scene a Board recorded. handed says the application has it,
// so the next show set holds it.
type kept struct {
	scene  show.Scene
	handed bool
}

// Pick picks lights on Board module: those alone, or, with toggle,
// adds them to what is picked, or takes them out where all of them are
// picked already. A light the rig does not have is left out.
func (e *Engine) Pick(module string, lights []string, toggle bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pickLights(module, lights, toggle)
}

// pickLights is [Engine.Pick], with the engine held.
func (e *Engine) pickLights(module string, lights []string, toggle bool) {
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
	e.holdAt(module, chans, v)
}

// holdAt is [Engine.Hold], with the engine held.
func (e *Engine) holdAt(module string, chans []Channel, v byte) {
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
	e.letGoAll(module)
}

// letGoAll is [Engine.ClearBoard], with the engine held.
func (e *Engine) letGoAll(module string) {
	if b := e.boards[module]; b != nil {
		b.held = nil
	}
}

// letGo lets go of chans on Board module, the other channels its faders
// hold held on.
func (e *Engine) letGo(module string, chans []Channel) {
	b := e.boards[module]
	if b == nil || !slices.ContainsFunc(chans, func(c Channel) bool { _, ok := b.held[c]; return ok }) {
		return
	}
	held := maps.Clone(b.held)
	for _, c := range chans {
		delete(held, c)
	}
	b.held = held
}

// PressRecord has Board module wait for a pad to record onto, or, where
// it waits, give up. While a chase is being made it does nothing: the
// pads are the chase's.
func (e *Engine) PressRecord(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recordDown(module)
}

// recordDown is [Engine.PressRecord], with the engine held.
func (e *Engine) recordDown(module string) {
	if b := e.boards[module]; b != nil && !b.making {
		b.record = !b.record
	}
}

// GiveUpRecord has Board module wait for no pad to record onto.
func (e *Engine) GiveUpRecord(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if b := e.boards[module]; b != nil {
		b.record = false
	}
}

// PressPad presses pad of Board module, from 0: while a chase is being
// made, a pad with a scene is its next step; where Record waits, it
// records onto the pad; otherwise it turns the pad's scene on, or off
// where it is on, fading over the scene fade.
func (e *Engine) PressPad(module string, pad int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.padDown(module, pad)
}

// padDown is [Engine.PressPad], with the engine held.
func (e *Engine) padDown(module string, pad int) {
	b, m := e.boards[module], e.s.Module(module)
	if b == nil || m == nil || pad < 0 || pad >= show.Pads {
		return
	}
	if b.record {
		e.record(m, b, pad)
		return
	}
	if _, ok := b.scene(m, pad); !ok {
		return
	}
	if b.making {
		// A new slice, as the last tick reported the one there was.
		b.steps = append(slices.Clip(b.steps), pad)
		return
	}
	at := fadeOf(m) == 0
	if f := b.fades[pad]; f != nil && f.on {
		f.on = false
		if at {
			delete(b.fades, pad)
		}
		return
	}
	if b.fades == nil {
		b.fades = map[int]*fade{}
	}
	f := b.fades[pad]
	if f == nil {
		f = &fade{}
		b.fades[pad] = f
	}
	f.on = true
	if at {
		f.level = 1
	}
}

// record stores on pad of Board m, b its state, what it plays now: the
// faders over the scenes that play. That scene is then on, alone, and
// the faders let go, so the stage plays on as it was. With nothing to
// store, it gives up. A scene recorded over another keeps its name.
func (e *Engine) record(m *show.Module, b *board, pad int) {
	b.record = false
	cat := e.s.Catalog()
	values := map[string]map[int]byte{}
	put := func(c Channel, v byte) {
		if values[c.Light] == nil {
			values[c.Light] = map[int]byte{}
		}
		values[c.Light][c.Offset] = v
	}
	// The faders hold their slots whole: a scene's channel at a slot a
	// fader holds, as another light's at the same address, is stored at
	// what the fader holds, so it plays no higher.
	heldAt := map[int]byte{}
	for c, v := range b.held {
		if slot, ok := e.slotOf(cat, c); ok {
			put(c, v)
			heldAt[slot] = max(heldAt[slot], v)
		}
	}
	e.scenesPlay(m, b, cat, func(c Channel, slot int, v byte, level float64) {
		if _, held := b.held[c]; held {
			return
		}
		v = e.atLevel(slot, v, level)
		if h, ok := heldAt[slot]; ok {
			v = h
		}
		if was, ok := values[c.Light][c.Offset]; !ok || v > was {
			put(c, v)
		}
	})
	// The chase that runs, as it plays now: the scene has it, and the
	// chase stops, so that the stage plays on as it was.
	if c, ok := b.chase(m, b.run); ok {
		for _, step := range c.Steps {
			sc, ok := b.scene(m, step)
			if !ok {
				continue
			}
			for id, chans := range sc.Values {
				for off := range chans {
					ch := Channel{Light: id, Offset: off}
					slot, ok := e.slotOf(cat, ch)
					if _, held := b.held[ch]; !ok || held || b.cur.level[slot-1] <= 0 {
						continue
					}
					v := e.atLevel(slot, b.cur.v[slot-1], float64(b.cur.level[slot-1]))
					if h, ok := heldAt[slot]; ok {
						v = h
					}
					if was, ok := values[id][off]; !ok || v > was {
						put(ch, v)
					}
				}
			}
		}
	}
	if len(values) == 0 {
		return
	}
	b.run = 0
	sc := show.Scene{Pad: pad, Name: "Scene " + strconv.Itoa(pad+1), Values: values}
	if was, ok := b.scene(m, pad); ok {
		sc.Name = was.Name
	}
	if b.kept == nil {
		b.kept = map[int]kept{}
	}
	b.kept[pad] = kept{scene: sc}
	e.recorded = append(e.recorded, Recorded{Module: m.ID, Scene: sc.Clone()})
	b.fades = map[int]*fade{pad: {on: true, level: 1}}
	b.held = nil
	if e.changed != nil {
		e.changed()
	}
}

// Recorded returns the scenes the Boards recorded since it was last
// asked, for the show to keep. A Board plays each until the next show
// set, which should hold it.
func (e *Engine) Recorded() []Recorded {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.recorded
	e.recorded = nil
	for _, r := range out {
		if b := e.boards[r.Module]; b != nil {
			if k, ok := b.kept[r.Scene.Pad]; ok {
				k.handed = true
				b.kept[r.Scene.Pad] = k
			}
		}
	}
	return out
}

// scene is the scene on pad of Board m, b its state: one recorded that
// the show does not hold yet, or the show's.
func (b *board) scene(m *show.Module, pad int) (show.Scene, bool) {
	if k, ok := b.kept[pad]; ok {
		return k.scene, true
	}
	if sc := m.Scene(pad); sc != nil {
		return *sc, true
	}
	return show.Scene{}, false
}

// fadeOf is how many seconds Board m's scenes take to fade.
func fadeOf(m *show.Module) float64 { return min(max(m.SceneFade, 0), show.MaxSceneFade) }

// slotOf is the universe slot channel c is at, from 1, and false for a
// light the rig does not have or a channel it does not.
func (e *Engine) slotOf(cat fixtures.Catalog, c Channel) (int, bool) {
	p := e.s.Light(c.Light)
	if p == nil || c.Offset < 1 || c.Offset > cat.ChannelCount(*p) {
		return 0, false
	}
	slot, ok := cat.ResolveChannel(*p, c.Offset)
	return slot, ok && slot >= 1 && slot <= dmx.Channels
}

// atLevel is what a scene at level plays for value v at slot, from 1,
// over a dark stage: a channel the grand master dims at the scene's
// level, the others whole while it plays.
func (e *Engine) atLevel(slot int, v byte, level float64) byte {
	if e.dims[slot-1] {
		return byte(math.Round(float64(v) * level))
	}
	return v
}

// scenesPlay calls f with each channel each scene of Board m that plays
// sets: its slot, from 1, the value the scene holds for it, and the
// scene's level. Two scenes may call f for one channel.
func (e *Engine) scenesPlay(m *show.Module, b *board, cat fixtures.Catalog, f func(c Channel, slot int, v byte, level float64)) {
	for pad, fd := range b.fades {
		sc, ok := b.scene(m, pad)
		if !ok {
			continue
		}
		for id, chans := range sc.Values {
			for off, v := range chans {
				c := Channel{Light: id, Offset: off}
				slot, ok := e.slotOf(cat, c)
				if !ok {
					continue
				}
				f(c, slot, v, fd.level)
			}
		}
	}
}

// keepBoards gives each Board of the show its state, kept by module
// from the show before; lights gone from the rig, and channels gone
// from their lights, drop out of it, and so do the scenes recorded, the
// chases made and the speed tapped that the show now holds. A chase
// deleted stops, and is the last run no more.
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
		// A scene recorded goes once the show holds it: the same scene,
		// or, handed over, whatever the show then made of its pad. A
		// show set meanwhile that has none on the pad leaves it playing.
		maps.DeleteFunc(b.kept, func(pad int, k kept) bool {
			sc := m.Scene(pad)
			return sc != nil && (k.handed || sameValues(sc.Values, k.scene.Values))
		})
		e.keepChases(&m, b)
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

// sameValues reports whether two scenes hold the same values.
func sameValues(a, b map[string]map[int]byte) bool {
	return maps.EqualFunc(a, b, maps.Equal[map[int]byte, map[int]byte])
}

// playBoard runs Board module m, dt seconds on from the last tick: its
// layer holds each channel its faders hold, as it is, with no curve,
// and under them the scenes that play and the chase that runs, the
// higher where two set one channel; a channel nothing sets is left out,
// so the mixer's home value shows. The scenes fading go on fading, and
// the chase steps on.
func (e *Engine) playBoard(m *show.Module, cat fixtures.Catalog, dt float64, live *Live) {
	l, b := e.layers[m.ID], e.boards[m.ID]
	if l == nil || b == nil {
		return
	}
	l.Clear()
	b.over, b.any = [dmx.Channels]uint16{}, false
	for c, v := range b.held {
		slot, ok := e.slotOf(cat, c)
		if !ok {
			continue
		}
		// Two lights at one address share the slot: the higher value
		// has it, whichever is come to first.
		if uint16(v)+1 > b.over[slot-1] {
			b.over[slot-1], b.any = uint16(v)+1, true
			l.Set(slot, v)
		}
	}
	secs := fadeOf(m)
	for pad, f := range b.fades {
		step := 1.0
		if secs > 0 {
			step = dt / secs
		}
		if f.on {
			f.level = min(f.level+step, 1)
		} else {
			f.level -= step
		}
		if _, ok := b.scene(m, pad); !ok || !f.on && f.level <= 0 {
			delete(b.fades, pad)
		}
	}
	// The scenes, by slot, each value one up, so that 0 says no scene
	// sets it.
	var scenes [dmx.Channels]uint16
	b.plays = b.plays[:0]
	e.scenesPlay(m, b, cat, func(_ Channel, slot int, v byte, level float64) {
		b.plays = append(b.plays, played{slot: slot - 1, v: v, level: level})
		scenes[slot-1] = max(scenes[slot-1], uint16(e.atLevel(slot, v, level))+1)
	})
	// The chase that runs, beside them.
	clock, _ := e.input(m.ID, "clock").(Count)
	following := e.chaseOn(m, b, cat, dt, clock, func(slot int, v byte, level float64) {
		b.plays = append(b.plays, played{slot: slot, v: v, level: level})
		scenes[slot] = max(scenes[slot], uint16(e.atLevel(slot+1, v, level))+1)
	})
	b.any = b.any || len(b.plays) > 0
	for i, v := range &scenes {
		if v != 0 && b.over[i] == 0 {
			l.Set(i+1, byte(v-1))
		}
	}
	e.values[portKey{m.ID, "out"}] = l
	st := BoardState{Picked: b.picked, Held: b.held, Record: b.record, Making: b.making, Steps: b.steps,
		Chase: b.run, Step: -1, Last: b.last, Clock: clock, Following: following, Speed: b.speedOf(m)}
	if b.run != 0 {
		st.Step, st.Into = b.at, min(max(b.into, 0), 1)
	}
	for pad, f := range b.fades {
		if st.Levels == nil {
			st.Levels = map[int]float64{}
		}
		st.Levels[pad] = f.level
		if f.on {
			st.On = append(st.On, pad)
		}
	}
	slices.Sort(st.On)
	if rm := e.remotes[m.PlayedFrom]; rm != nil && rm.board == m.ID {
		st.Remote, st.Page = m.PlayedFrom, rm.page
	}
	live.Boards[m.ID] = st
}

// boardsInto calls f with each Board wired into Mixer module mixer
// that plays a scene or whose faders hold a channel.
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

// boardsOver puts what the Boards wired into Mixer module mixer play
// over u, the universe it merged from its other inputs: a Board has
// the channels it sets, whatever else is wired into the mixer; the
// grand master comes after. Of what a Board plays on a channel the master
// dims, each at its level, the most shows, and what plays under shows
// through by as much as the Board's fullest level there leaves: so a
// scene fading crosses to another of the Board's that holds the channel,
// or to what the mixer merged where none does. A fader held has its
// channel over all of that.
func (e *Engine) boardsOver(mixer string, u *dmx.Universe) {
	e.boardsInto(mixer, func(_ string, b *board) {
		var cover, top [dmx.Channels]float32
		for _, p := range b.plays {
			l := float32(p.level)
			if !e.dims[p.slot] {
				l = 1
			}
			cover[p.slot] = max(cover[p.slot], l)
			top[p.slot] = max(top[p.slot], float32(p.v)*l)
		}
		for i, c := range &cover {
			if c <= 0 {
				continue
			}
			u[i] = dmx.Clamp8f(float64(u[i])*(1-float64(c)) + float64(top[i]))
		}
		for i, v := range &b.over {
			if v != 0 {
				u[i] = byte(v - 1)
			}
		}
	})
}

// Forget lets go of what a show does not keep, as another show takes
// its place: what each Board has picked and holds, its scenes on and
// those recorded, and whether Record waits; the chase being made, those
// made, the one that runs and the last run, and the taps; the overrides
// held; the page each controller that plays a Board is on, and the
// grand masters they set that the show does not hold.
func (e *Engine) Forget() {
	e.mu.Lock()
	defer e.mu.Unlock()
	clear(e.boards)
	clear(e.raw)
	clear(e.held)
	e.recorded = nil
	e.made = nil
	clear(e.remotes)
	clear(e.masters)
	e.mastered = nil
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
