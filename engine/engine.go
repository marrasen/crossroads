// Package engine runs a show's modules: forty times a second, each in
// the order its wires make, passing what each output carries along the
// wires to the inputs they join. The universes a mixer makes go out to
// the DMX boxes and to the stage view.
package engine

import (
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/artnet"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/enttec"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim-dmx/mix"
	"github.com/marrasen/gunim-dmx/sacn"
)

// A Signal is what a port carries. A wire joins an output and an input
// of one signal.
type Signal string

// The signals.
const (
	// Value is a number from 0 to 1, as a fader's.
	Value Signal = "value"
	// Light is what a set of lights should do: a layer of channel
	// values, which a mixer merges.
	Light Signal = "light"
	// DMX is a finished universe.
	DMX Signal = "dmx"
	// Audio is sound, as a [Sound].
	Audio Signal = "audio"
	// Clock is a beat counted, as a [Count].
	Clock Signal = "clock"
	// Trigger is a moment, as a button pressed or a song starting: true
	// for the tick it fires in.
	Trigger Signal = "trigger"
)

// A Port is one of a module's inputs or outputs.
type Port struct {
	ID, Name string
	Signal   Signal
}

// lightPrefix starts the ID of a controller's light's input.
const lightPrefix = "light:"

// Back reports whether p is an input that feeds back, as a
// controller's light: it may close a loop, and hears what is wired
// into it a tick late.
func (p Port) Back() bool { return strings.HasPrefix(p.ID, lightPrefix) }

// mixerIns is how many light inputs a mixer has.
const mixerIns = 4

// Ports are module m's inputs and outputs, in s: a Lights module's
// inputs are the things its lights do, in the order a desk lists them.
func Ports(s *show.Show, m *show.Module) (ins, outs []Port) {
	switch m.Kind {
	case show.Fader:
		return nil, []Port{{"out", "Value", Value}}
	case show.Lights:
		cat := s.Catalog()
		var members []fixtures.Patch
		for _, id := range m.Lights {
			if p := s.Light(id); p != nil {
				members = append(members, *p)
			}
		}
		for _, r := range cat.RolesInGroup(members) {
			ins = append(ins, Port{string(r), r.Label(), Value})
		}
		return ins, []Port{{"out", "Light", Light}}
	case show.Mixer:
		for i := 1; i <= mixerIns; i++ {
			n := string(rune('0' + i))
			ins = append(ins, Port{"in" + n, "Light " + n, Light})
		}
		return ins, []Port{{"out", "DMX", DMX}}
	case show.DMXOut, show.StageView:
		return []Port{{"in", "DMX", DMX}}, nil
	case show.AudioIn:
		return nil, []Port{{"out", "Audio", Audio}}
	case show.Beat:
		return []Port{{"in", "Audio", Audio}}, []Port{{"out", "Clock", Clock}}
	case show.Tempo:
		return []Port{{"in", "Clock", Clock}}, []Port{{"out", "Clock", Clock}}
	case show.Level:
		return []Port{{"in", "Audio", Audio}}, []Port{{"out", "Value", Value}}
	case show.Pulse:
		return []Port{{"in", "Clock", Clock}}, []Port{{"out", "Value", Value}}
	case show.Player:
		return []Port{{"play", "Play or pause", Trigger}, {"stop", "Stop", Trigger}, {"next", "Next song", Trigger}, {"back", "Song before", Trigger}},
			[]Port{{"audio", "Audio", Audio}, {"clock", "Clock", Clock}, {"start", "Song starts", Trigger}, {"end", "Song ends", Trigger}, {"bar", "Each bar", Trigger}}
	case show.Button:
		return nil, []Port{{"out", "Pressed", Trigger}}
	case show.Controller:
		for _, c := range controller.ControlsOf(m) {
			if c.Kind == show.Press {
				outs = append(outs, Port{c.ID, c.Name, Trigger})
				if c.Lit {
					ins = append(ins, Port{lightPrefix + c.ID, "Light " + c.Name, Value})
				}
				continue
			}
			outs = append(outs, Port{c.ID, c.Name, Value})
		}
		return ins, outs
	case show.MIDIOut:
		return []Port{{"note", "Note", Trigger}, {"value", "Controller", Value}}, nil
	case show.Map:
		return []Port{{"in", "Value", Value}}, []Port{{"out", "Value", Value}}
	case show.Every:
		return []Port{{"in", "Clock", Clock}}, []Port{{"out", "Every", Trigger}}
	case show.Toggle:
		return []Port{{"in", "Flip", Trigger}}, []Port{{"out", "On or off", Value}}
	}
	return nil, nil
}

// PortOf returns the port of module m that e names, an output where
// out says.
func PortOf(s *show.Show, e show.End, out bool) (Port, bool) {
	m := s.Module(e.Module)
	if m == nil {
		return Port{}, false
	}
	ins, outs := Ports(s, m)
	ps := ins
	if out {
		ps = outs
	}
	i := slices.IndexFunc(ps, func(p Port) bool { return p.ID == e.Port })
	if i < 0 {
		return Port{}, false
	}
	return ps[i], true
}

// CanJoin says whether a wire may join output from to input to in s,
// and, where not, why: the same signal, an input no wire feeds yet, and
// no loop back to where it started.
func CanJoin(s *show.Show, from, to show.End) (ok bool, why string) {
	a, okA := PortOf(s, from, true)
	b, okB := PortOf(s, to, false)
	switch {
	case !okA || !okB:
		return false, "There is no such port."
	case a.Signal != b.Signal:
		return false, "A " + string(a.Signal) + " output joins " + string(a.Signal) + " inputs only."
	case slices.ContainsFunc(s.Wires, func(w show.Wire) bool { return w.To == to }):
		return false, "That input is fed already. Pull its wire out first."
	case !b.Back() && feeds(s, to.Module, from.Module):
		return false, "That would feed the module its own output."
	}
	return true, ""
}

// feeds reports whether module a feeds module b, through any wires, or
// is it.
func feeds(s *show.Show, a, b string) bool {
	seen := map[string]bool{}
	var walk func(m string) bool
	walk = func(m string) bool {
		if m == b {
			return true
		}
		if seen[m] {
			return false
		}
		seen[m] = true
		for _, w := range s.Wires {
			if w.From.Module == m && walk(w.To.Module) {
				return true
			}
		}
		return false
	}
	return walk(a)
}

// Live is what the engine has made, for the windows to show.
type Live struct {
	// Stage is the universe the first stage view, in the modules' order,
	// shows.
	Stage dmx.Universe
	// Values are what each module's Value output carries, by module.
	Values map[string]float64
	// Outs are each DMX out's box, by module.
	Outs map[string]dmx.State
	// Clocks are what each Beat and Tempo counts, by module.
	Clocks map[string]Count
	// Says are what each Audio in listens to, and each MIDI out sends
	// to, or why not, by module.
	Says map[string]string
	// Playing is what the playlist is doing.
	Playing Playing
	// Desks are what each Controller shows, by module.
	Desks map[string]Desk
}

// An Engine runs a show's modules.
type Engine struct {
	mu     sync.Mutex
	s      show.Show
	order  []string
	layers map[string]*mix.Layer
	mixers map[string]*mix.Mixer
	// heard are the layers each mixer merged last tick.
	heard   map[string][]*mix.Layer
	dims    [dmx.Channels]bool
	home    dmx.Universe
	outs    map[string]*out
	values  map[portKey]any
	changed func()
	// ears are the Audio ins' inputs, follow the Beats' followers,
	// tempos the Tempos' own clocks and levels the Levels', by module.
	ears   map[string]*ear
	follow map[string]*follower
	tempos map[string]*tempo
	levels map[string]float64
	// last is when the last tick ran.
	last time.Time
	// The playlist: the song chosen, what the deck was doing at the
	// last tick, and why it would not play; the gain it plays at, the
	// bar it is in, the events this tick, and the sound it played.
	song    string
	state   DeckState
	deckErr string
	gain    float64
	bar     int64
	ev      events
	played  []float32
	rate    int
	// pressed are the Buttons pressed since the last tick.
	pressed map[string]bool
	// desks are the Controllers', senders the MIDI outs', everys the
	// beat each Every counted last and toggles each Toggle's state, by
	// module; learnt the controls learned.
	desks   map[string]*desk
	senders map[string]*sender
	everys  map[string]int64
	toggles map[string]bool
	learnt  []Learnt

	// Listen opens an Audio in's input, [Listen] where it is nil, and
	// now tells the time, [time.Now] where it is nil. Tests set them
	// before the first Set.
	Listen func(input string) (Listener, error)
	Now    func() time.Time
	// Deck plays the playlist; with none, nothing plays. Set it before
	// the first Set; Close closes it.
	Deck Deck
	// MIDI opens the MIDI ports, the system's where it is nil. Set it
	// before the first Set.
	MIDI MIDIPorts
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// out is a DMX out's box, and what it was opened for. gone says it
// was let go, which a connection still on its way then lets go too.
type out struct {
	output show.Output
	e      *dmx.Engine
	gone   atomic.Bool
}

// close lets the box go, now and once a connection on its way lands.
func (o *out) close() {
	o.gone.Store(true)
	o.e.Close()
}

type portKey struct {
	module, port string
}

// New returns an engine of no modules. changed, where set, runs from
// any goroutine as a DMX box connects, fails or goes.
func New(changed func()) *Engine {
	return &Engine{layers: map[string]*mix.Layer{}, mixers: map[string]*mix.Mixer{}, heard: map[string][]*mix.Layer{}, outs: map[string]*out{},
		values: map[portKey]any{}, changed: changed, ears: map[string]*ear{}, follow: map[string]*follower{},
		tempos: map[string]*tempo{}, levels: map[string]float64{}, pressed: map[string]bool{}, bar: math.MinInt64,
		desks: map[string]*desk{}, senders: map[string]*sender{}, everys: map[string]int64{}, toggles: map[string]bool{}}
}

// Set runs show s from now on: its modules in its wires' order, with a
// DMX box for each DMX out, opened again where its output changed.
func (e *Engine) Set(s show.Show) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.s = s.Clone()
	e.order = order(&e.s)
	cat := e.s.Catalog()
	e.dims = [dmx.Channels]bool{}
	var home dmx.Universe
	for _, p := range e.s.Rig.Lights {
		for _, c := range cat.ResolvedChannels(p) {
			if !c.InUniverse {
				continue
			}
			e.dims[c.Slot-1] = fixtures.Dimmable(c.Channel)
			home[c.Slot-1] = dmx.Clamp8(c.Channel.Home)
		}
	}
	e.home = home
	keep := map[string]bool{}
	for _, m := range e.s.Modules {
		keep[m.ID] = true
		switch m.Kind {
		case show.Lights:
			if e.layers[m.ID] == nil {
				e.layers[m.ID] = mix.NewLayer()
			}
		case show.Mixer:
			if e.mixers[m.ID] == nil {
				e.mixers[m.ID] = mix.NewMixer()
			}
			e.mixers[m.ID].SetHome(home)
		case show.DMXOut:
			o := e.outs[m.ID]
			if o != nil && o.output == m.Out {
				continue
			}
			if o != nil {
				o.close()
			}
			e.outs[m.ID] = e.open(m.Out)
		case show.AudioIn:
			a := e.ears[m.ID]
			if a != nil && a.input == m.Input {
				continue
			}
			if a != nil {
				a.close()
			}
			a = &ear{input: m.Input}
			listen := e.Listen
			if listen == nil {
				listen = Listen
			}
			a.open(listen, e.changed)
			e.ears[m.ID] = a
		case show.Controller:
			d := e.desks[m.ID]
			if d != nil && d.port.device == m.Device {
				continue
			}
			if d != nil {
				d.port.close()
			} else {
				d = &desk{values: map[string]float64{}, pos: map[string]float64{}, caught: map[string]bool{}, held: map[string]bool{},
					fired: map[string]bool{}, sent: map[string]int{}}
				e.desks[m.ID] = d
			}
			// Another device: its lights and its sliders start afresh.
			d.port, d.ch = &port{device: m.Device, listen: true}, -1
			clear(d.sent)
			clear(d.caught)
		case show.MIDIOut:
			if sd := e.senders[m.ID]; sd != nil && sd.port.device == m.Device {
				continue
			} else if sd != nil {
				sd.port.close()
			}
			e.senders[m.ID] = &sender{port: &port{device: m.Device}, cc: -1}
		case show.Fader, show.StageView, show.Beat, show.Tempo, show.Level, show.Pulse, show.Player, show.Button,
			show.Map, show.Every, show.Toggle:
		}
	}
	for id := range e.layers {
		if !keep[id] {
			delete(e.layers, id)
		}
	}
	for id := range e.mixers {
		if !keep[id] {
			delete(e.mixers, id)
			delete(e.heard, id)
		}
	}
	for id, o := range e.outs {
		if m := e.s.Module(id); m == nil || m.Kind != show.DMXOut {
			o.close()
			delete(e.outs, id)
		}
	}
	for id, a := range e.ears {
		if m := e.s.Module(id); m == nil || m.Kind != show.AudioIn {
			a.close()
			delete(e.ears, id)
		}
	}
	if e.song != "" && e.s.Song(e.song) == nil {
		// The song chosen is out of the playlist: it stops.
		if e.Deck != nil {
			e.Deck.Stop()
		}
		e.song, e.state, e.deckErr = "", DeckState{}, ""
	}
	for id, d := range e.desks {
		if m := e.s.Module(id); m == nil || m.Kind != show.Controller {
			d.port.close()
			delete(e.desks, id)
		}
	}
	for id, sd := range e.senders {
		if m := e.s.Module(id); m == nil || m.Kind != show.MIDIOut {
			sd.port.close()
			delete(e.senders, id)
		}
	}
	for id := range e.everys {
		if !keep[id] {
			delete(e.everys, id)
		}
	}
	for id := range e.toggles {
		if !keep[id] {
			delete(e.toggles, id)
		}
	}
	for id := range e.follow {
		if !keep[id] {
			delete(e.follow, id)
		}
	}
	for id := range e.tempos {
		if !keep[id] {
			delete(e.tempos, id)
		}
	}
	for id := range e.levels {
		if !keep[id] {
			delete(e.levels, id)
		}
	}
}

// open opens a DMX box for o, connecting on a goroutine of its own, as
// a USB box may take a moment; one sending nowhere has no box.
func (e *Engine) open(o show.Output) *out {
	eng := dmx.NewEngine()
	if e.changed != nil {
		eng.OnState(func(dmx.State) { e.changed() })
	}
	var opener func() (dmx.Device, error)
	switch o.Device {
	case "enttec":
		if o.Port == "" {
			opener = func() (dmx.Device, error) { return enttec.FindPro() }
		} else {
			opener = func() (dmx.Device, error) { return enttec.OpenPro(o.Port) }
		}
	case "opendmx":
		opener = func() (dmx.Device, error) { return enttec.OpenOpenDMX(o.Port) }
	case "artnet":
		opener = func() (dmx.Device, error) {
			if o.Host == "" {
				return nil, errors.New("art-net needs the node's IP address, as 192.168.1.40")
			}
			return artnet.Dial(o.Host, artnet.PortAddress(o.Universe))
		}
	case "sacn":
		opener = func() (dmx.Device, error) {
			var opts []sacn.Option
			if o.Host != "" {
				opts = append(opts, sacn.WithHost(o.Host))
			}
			return sacn.Dial(max(o.Universe, sacn.MinUniverse), "crossroads", opts...)
		}
	}
	ob := &out{output: o, e: eng}
	if opener != nil {
		go func() {
			_ = eng.Connect(opener)
			if ob.gone.Load() {
				// Let go while it connected: the box it opened goes too.
				eng.Close()
			}
		}()
	}
	return ob
}

// order is the modules of s, each after every module that feeds it.
func order(s *show.Show) []string {
	var out []string
	done := map[string]bool{}
	busy := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		if done[id] || busy[id] {
			return
		}
		busy[id] = true
		for _, w := range s.Wires {
			if w.To.Module == id && !back(s, w.To) {
				visit(w.From.Module)
			}
		}
		busy[id] = false
		done[id] = true
		out = append(out, id)
	}
	for _, m := range s.Modules {
		visit(m.ID)
	}
	return out
}

// back reports whether input e of s feeds back, and so does not put
// what feeds it first.
func back(s *show.Show, e show.End) bool {
	p, ok := PortOf(s, e, false)
	return ok && p.Back()
}

// SetValue sets a fader's value, or a mixer's grand master, at once,
// for the next tick, as its fader moves.
func (e *Engine) SetValue(module string, v float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if m := e.s.Module(module); m != nil {
		m.Value = min(max(v, 0), 1)
	}
}

// input is what the wire into port of module carries, nil for none.
func (e *Engine) input(module, port string) any {
	for _, w := range e.s.Wires {
		if w.To.Module == module && w.To.Port == port {
			return e.values[portKey{w.From.Module, w.From.Port}]
		}
	}
	return nil
}

// Tick runs every module once, sends the universes to the DMX boxes,
// and returns what the windows show.
func (e *Engine) Tick() Live {
	e.mu.Lock()
	defer e.mu.Unlock()
	cat := e.s.Catalog()
	live := Live{Values: map[string]float64{}, Outs: map[string]dmx.State{}, Clocks: map[string]Count{}, Says: map[string]string{},
		Desks: map[string]Desk{}}
	staged := false
	now := e.now()
	var dt float64
	if !e.last.IsZero() {
		// A tick held up long, as by a laptop asleep, does not run a
		// clock on by all of it.
		dt = min(max(now.Sub(e.last).Seconds(), 0), 0.25)
	}
	e.last = now
	e.playlist()
	defer func() {
		e.ev = events{}
		clear(e.pressed)
	}()
	for _, id := range e.order {
		m := e.s.Module(id)
		if m == nil {
			continue
		}
		switch m.Kind {
		case show.Fader:
			e.values[portKey{id, "out"}] = m.Value
			live.Values[id] = m.Value
		case show.Lights:
			e.lights(m, cat)
		case show.Mixer:
			mx := e.mixers[id]
			for _, l := range e.heard[id] {
				mx.Remove(l)
			}
			e.heard[id] = e.heard[id][:0]
			for i := 1; i <= mixerIns; i++ {
				if l, ok := e.input(id, "in"+string(rune('0'+i))).(*mix.Layer); ok {
					mx.Add(l)
					e.heard[id] = append(e.heard[id], l)
				}
			}
			mx.SetMaster(m.Value)
			e.values[portKey{id, "out"}] = mx.Merge(&e.dims)
			live.Values[id] = m.Value
		case show.DMXOut:
			u, ok := e.input(id, "in").(dmx.Universe)
			if !ok {
				u = e.home
			}
			if o := e.outs[id]; o != nil {
				o.e.SetUniverse(u)
				live.Outs[id] = o.e.State()
			}
		case show.StageView:
			if staged {
				continue
			}
			// The first stage view shows what is wired into it, and with
			// nothing wired, the lights at rest.
			staged = true
			if u, ok := e.input(id, "in").(dmx.Universe); ok {
				live.Stage = u
			} else {
				live.Stage = e.home
			}
		case show.AudioIn, show.Beat, show.Tempo, show.Level, show.Pulse:
			e.sound(m, dt, &live)
		case show.Player:
			e.player(m, &live)
		case show.Controller:
			e.control(m, now, &live)
		case show.MIDIOut:
			e.send(m, now, &live)
		case show.Map, show.Every, show.Toggle:
			e.logic(m, &live)
		case show.Button:
			fired := e.pressed[id]
			e.values[portKey{id, "out"}] = fired
			if fired {
				live.Values[id] = 1
			}
		}
	}
	live.Playing = Playing{Song: e.song, Pos: e.state.Pos, Len: e.state.Len, Playing: e.state.Playing, Err: e.deckErr}
	return live
}

// lights makes Lights module m's layer: each thing its lights do set
// from the value wired into its input, curved as each light asks; an
// input with no wire leaves its channels at rest.
func (e *Engine) lights(m *show.Module, cat fixtures.Catalog) {
	l := e.layers[m.ID]
	l.Clear()
	for _, id := range m.Lights {
		p := e.s.Light(id)
		if p == nil {
			continue
		}
		for _, r := range cat.RolesOf(*p) {
			v, ok := e.input(m.ID, string(r)).(float64)
			if !ok {
				continue
			}
			c, ok := cat.ChannelForRole(*p, r)
			if !ok || !c.InUniverse {
				continue
			}
			l.Set(c.Slot, byte(fixtures.CurveValue(*p, c.Channel, int(dmx.Level(v)))))
		}
	}
	e.values[portKey{m.ID, "out"}] = l
}

// Close darkens every DMX box and lets it go.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, o := range e.outs {
		o.close()
		delete(e.outs, id)
	}
	for id, a := range e.ears {
		a.close()
		delete(e.ears, id)
	}
	if e.Deck != nil {
		_ = e.Deck.Close()
		e.Deck = nil
	}
	for id, d := range e.desks {
		d.port.close()
		delete(e.desks, id)
	}
	for id, sd := range e.senders {
		sd.port.close()
		delete(e.senders, id)
	}
}
