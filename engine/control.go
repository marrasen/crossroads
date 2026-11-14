package engine

import (
	"fmt"
	"math"
	"time"

	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// MIDIIn is a MIDI input open, as package midi's is.
type MIDIIn interface {
	Err() error
	Close() error
}

// MIDIOut is a MIDI output open, as package midi's is.
type MIDIOut interface {
	Send(at time.Time, m midi.Message) error
	Err() error
	Close() error
}

// MIDIPorts opens MIDI ports by their names, as package midi does.
type MIDIPorts interface {
	OpenIn(name string, f func(midi.Event)) (MIDIIn, error)
	OpenOut(name string) (MIDIOut, error)
}

// systemMIDI is the system's MIDI ports.
type systemMIDI struct{}

// OpenIn implements [MIDIPorts]. Where the port would not open it
// returns a nil interface, not a nil port in one.
func (systemMIDI) OpenIn(name string, f func(midi.Event)) (MIDIIn, error) {
	in, err := midi.OpenIn(name, f)
	if err != nil {
		return nil, err
	}
	return in, nil
}

// OpenOut implements [MIDIPorts], as OpenIn does.
func (systemMIDI) OpenOut(name string) (MIDIOut, error) {
	out, err := midi.OpenOut(name)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// A desk is a Controller's state: its port, each knob's and slider's
// value, which sliders have taken over their value, the buttons held,
// what its lights were last sent, whether it was heard in Standard mode
// where it wants Mackie Control mode, and whether it learns its next
// control.
type desk struct {
	device string
	values map[string]float64
	// pos is where each slider not caught up stood last.
	pos    map[string]float64
	caught map[string]bool
	held   map[string]bool
	fired  map[string]bool
	sent   map[string]int
	// standard says a knob was heard in Standard mode, and nothing in
	// Mackie Control mode since.
	standard bool
	// switched says the switch to Mackie Control mode went out to the
	// port as it stands open, for the layout as it is.
	switched bool
	learn    bool
	last     string
	events   []midi.Event
}

// A Desk is what a Controller shows: what its port says, each
// control's value, the last message heard, whether it learns, and
// whether it is in Standard mode where its layout wants Mackie Control
// mode.
type Desk struct {
	Says     string
	Values   map[string]float64
	Last     string
	Learn    bool
	Standard bool
}

// Learnt is a control a Controller learned from what it heard.
type Learnt struct {
	Module  string
	Control show.Control
}

// knobStep is how far a knob turns its value each step.
const knobStep = 1.0 / 100

// catchWithin is how near a slider has to come to the value it takes
// over, where it did not pass it.
const catchWithin = 2.0 / 127

// control runs Controller module m: what it heard moves its controls'
// values and fires its buttons, and its lights are sent what is wired
// into them.
func (e *Engine) control(m *show.Module, now time.Time, live *Live) {
	d := e.desks[m.ID]
	if d == nil {
		return
	}
	d.events = append(d.events[:0], e.hub.heard[m.Device]...)
	out, fresh := e.hub.out(m.Device)
	clear(d.fired)
	if e.hub.freshIn[m.Device] {
		// Heard again: each slider takes its value over afresh, as it
		// may have moved meanwhile, and so does the fader the grand
		// master of a Board it plays.
		clear(d.caught)
		if rm := e.remotes[m.ID]; rm != nil {
			rm.caught, rm.moved = false, false
		}
	}
	if !e.hub.liveIn[m.Device] {
		// Unplugged, or never there: no button is held, so nothing it
		// holds, as a flash, sticks.
		clear(d.held)
	}
	// rm is the Board the controller plays, nil for none: what it heard
	// plays the Board too.
	rm := e.remotes[m.ID]
	if rm != nil {
		rm.acts = rm.acts[:0]
	}
	controls := controller.ControlsOf(m)
	l, _ := controller.Find(m.Layout)
	ticks := controller.Ticks
	if l.Mackie {
		ticks = controller.MackieTicks
	}
	if fresh {
		clear(d.sent)
		d.switched = false
	}
	if !l.Mackie {
		d.switched, d.standard = false, false
	}
	// justSwitched says the switch goes out this tick: what was heard
	// before it says nothing of the mode the controller is in now.
	justSwitched := l.Mackie && out != nil && !d.switched
	if justSwitched {
		// The switch to Mackie Control mode, before every light: as the
		// port opens, and where it was open already, as for a layout
		// just picked or a port another module holds.
		clear(d.sent)
		d.standard = false
		d.switched = true
		for _, msg := range controller.MackieHello() {
			if out.Send(time.Time{}, msg) != nil {
				d.switched = false
			}
		}
	}
	for _, ev := range d.events {
		kind, ch := ev.Kind(), ev.Channel()
		var number, value int
		switch kind {
		case midi.KindControl:
			number, value = ev.Controller(), ev.Value()
		case midi.KindNoteOn:
			number, value = ev.Key(), ev.Velocity()
		case midi.KindNoteOff:
			number, value = ev.Key(), 0
		case midi.KindBend:
			// Where it stands is the second data byte, as a 7-bit fader
			// sends it.
			value = int(ev.Data2)
		default:
			continue
		}
		d.last = ev.String()
		e.note(now, m.Name+": "+d.last)
		c := find(controls, kind, ch, number)
		if l.Mackie {
			switch {
			case controller.IsStandard(kind, ch, number):
				d.standard = !justSwitched
			case c != nil:
				// One of Mackie Control mode's own messages: it switched.
				d.standard = false
			}
		}
		if c == nil {
			if d.learn && value > 0 {
				id := fmt.Sprintf("c%d", len(m.Controls)+len(e.learnt)+1)
				if lc, ok := controller.Learned(id, byte(kind), ch, number); ok {
					e.learnt = append(e.learnt, Learnt{Module: m.ID, Control: lc})
					d.learn = false
				}
			}
			continue
		}
		switch c.Kind {
		case show.Knob:
			if rm != nil {
				rm.acts = append(rm.acts, act{control: c.ID, ticks: ticks(byte(value))})
			}
			// Kept on a whole step, so that steps up and then down end
			// where they began.
			v := d.values[c.ID] + float64(ticks(byte(value)))*knobStep
			d.values[c.ID] = min(max(math.Round(v/knobStep)*knobStep, 0), 1)
		case show.Slider:
			at := float64(value) / 127
			if rm != nil {
				rm.acts = append(rm.acts, act{control: c.ID, at: at})
			}
			if was, ok := d.values[c.ID]; ok && !d.caught[c.ID] {
				// Soft takeover: the slider takes the value over only as
				// it passes it, or comes near it.
				last, moved := d.pos[c.ID]
				d.pos[c.ID] = at
				if math.Abs(at-was) > catchWithin && (!moved || (last-was)*(at-was) > 0) {
					continue
				}
			}
			d.caught[c.ID] = true
			d.values[c.ID] = at
		case show.Press:
			down := kind == midi.KindNoteOn && value > 0
			if down && !d.held[c.ID] {
				d.fired[c.ID] = true
				if rm != nil {
					rm.acts = append(rm.acts, act{control: c.ID})
				}
			}
			d.held[c.ID] = down
		}
	}
	values := make(map[string]float64, len(controls))
	for _, c := range controls {
		switch c.Kind {
		case show.Knob, show.Slider:
			e.values[portKey{m.ID, c.ID}] = d.values[c.ID]
			values[c.ID] = d.values[c.ID]
		case show.Press:
			e.values[portKey{m.ID, c.ID}] = d.fired[c.ID]
			if d.held[c.ID] {
				values[c.ID] = 1
			}
		}
	}
	if rm != nil {
		// Its lights are the Board's, sent once every module has run.
		e.playRemote(rm)
	} else if out != nil {
		e.light(m, d, controls, out, l.Mackie, nil)
	}
	live.Desks[m.ID] = Desk{Says: e.hub.says(m.Device, true), Values: values, Last: d.last, Learn: d.learn, Standard: d.standard}
}

// light sends the controller's lights what they show, where it
// changed, each on its control's own channel: a knob's ring its value,
// a button's light what is wired into it, or, with nothing wired,
// whether it is held; or, for a controller that plays a Board, what
// board says. mackie says the controller is in Mackie Control mode,
// whose rings fill from the left.
func (e *Engine) light(m *show.Module, d *desk, controls []show.Control, out MIDIOut, mackie bool, board *boardLights) {
	for _, c := range controls {
		if !c.Lit {
			continue
		}
		var v int
		var msg midi.Message
		switch c.Kind {
		case show.Knob, show.Slider:
			value := d.values[c.ID]
			if board != nil {
				value = board.rings[c.ID]
			}
			v = int(math.Round(value * 127))
			msg = midi.Control(c.Channel, c.CC, v)
			if mackie {
				v = controller.Ring(value)
				msg = midi.Control(c.Channel, controller.RingCC(c.CC), v)
			}
		case show.Press:
			lit := d.held[c.ID]
			if in, ok := e.input(m.ID, lightPrefix+c.ID).(float64); ok {
				lit = in >= 0.5
			}
			v = controller.LightOff
			if lit {
				v = controller.LightOn
			}
			if board != nil {
				v = board.lights[c.ID]
			}
			msg = midi.NoteOn(c.Channel, c.Note, v)
		}
		if was, ok := d.sent[c.ID]; ok && was == v {
			continue
		}
		if out.Send(time.Time{}, msg) == nil {
			d.sent[c.ID] = v
		}
	}
}

// find is the control of controls that sends a message of kind on
// channel ch for number, nil for none.
func find(controls []show.Control, kind midi.Kind, ch, number int) *show.Control {
	note := kind == midi.KindNoteOn || kind == midi.KindNoteOff
	for i := range controls {
		c := &controls[i]
		if c.Channel != ch {
			continue
		}
		if kind == midi.KindBend && c.Bend || kind == midi.KindControl && c.CC == number || note && c.Note == number {
			return c
		}
	}
	return nil
}

// midiPorts is the MIDI the engine opens.
func (e *Engine) midiPorts() MIDIPorts {
	if e.MIDI != nil {
		return e.MIDI
	}
	return systemMIDI{}
}

// Learn has Controller module learn the next control moved that it
// does not know, or not.
func (e *Engine) Learn(module string, on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d := e.desks[module]; d != nil {
		d.learn = on
	}
}

// Learnt returns the controls learned since it was last asked.
func (e *Engine) Learnt() []Learnt {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.learnt
	e.learnt = nil
	return out
}

// A sender is a MIDI out's state: the device it sends to, and what it
// sent last.
type sender struct {
	device string
	cc     int
	noted  bool
}

// send runs MIDI out module m: a trigger in plays its note, ended at
// the next tick, and a value in moves its controller where it changed.
func (e *Engine) send(m *show.Module, live *Live) {
	s := e.senders[m.ID]
	if s == nil {
		return
	}
	out, fresh := e.hub.out(m.Device)
	live.Says[m.ID] = e.hub.says(m.Device, false)
	if out == nil {
		return
	}
	if fresh {
		s.cc = -1
	}
	if s.noted {
		_ = out.Send(time.Time{}, midi.NoteOff(m.Channel, m.Note, 0))
		s.noted = false
	}
	if fired, _ := e.input(m.ID, "note").(bool); fired {
		if out.Send(time.Time{}, midi.NoteOn(m.Channel, m.Note, 127)) == nil {
			s.noted = true
		}
	}
	if v, ok := e.input(m.ID, "value").(float64); ok {
		cc := int(math.Round(min(max(v, 0), 1) * 127))
		if cc != s.cc && out.Send(time.Time{}, midi.Control(m.Channel, m.CC, cc)) == nil {
			s.cc = cc
		}
	}
}

// logic runs the modules that work on values, triggers and clocks.
func (e *Engine) logic(m *show.Module, live *Live) {
	key := portKey{m.ID, "out"}
	switch m.Kind {
	case show.Map:
		v, ok := e.input(m.ID, "in").(float64)
		if !ok {
			return
		}
		out := MapValue(v, m.From, m.To, m.Invert)
		e.values[key] = out
		live.Values[m.ID] = out
	case show.Every:
		c, ok := e.input(m.ID, "in").(Count)
		fired := false
		if ok && c.BPM > 0 {
			every := m.Every
			if every <= 0 {
				every = 1
			}
			n := int64(math.Floor(c.Beats / every))
			last, seen := e.everys[m.ID]
			// Each time the count passes into the next so many beats, and
			// as it starts, where it has only just begun them.
			fired = c.Sure && (seen && n != last || !seen && c.Beats-float64(n)*every < barJust)
			e.everys[m.ID] = n
		}
		if fired {
			e.note(e.last, m.Name+" fired")
		}
		e.values[key] = fired
		if fired {
			live.Values[m.ID] = 1
		}
	case show.Toggle:
		on := e.toggles[m.ID]
		if fired, _ := e.input(m.ID, "in").(bool); fired {
			on = !on
		}
		e.toggles[m.ID] = on
		v := 0.0
		if on {
			v = 1
		}
		e.values[key] = v
		live.Values[m.ID] = v
	default:
	}
}

// Flip flips Toggle module, as a trigger into it would.
func (e *Engine) Flip(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.toggles[module] = !e.toggles[module]
}

// MapValue maps v from the range from to the range to, turned over
// where invert says; ranges of zero are 0 to 1.
func MapValue(v float64, from, to [2]float64, invert bool) float64 {
	if from == [2]float64{} {
		from = [2]float64{0, 1}
	}
	if to == [2]float64{} {
		to = [2]float64{0, 1}
	}
	t := 0.0
	if from[1] != from[0] {
		t = min(max((v-from[0])/(from[1]-from[0]), 0), 1)
	}
	if invert {
		t = 1 - t
	}
	return to[0] + (to[1]-to[0])*t
}
