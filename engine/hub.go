package engine

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// retryEvery is how often a MIDI port that would not open, or went
// away, is tried again.
const retryEvery = 2 * time.Second

// A port is a MIDI device's input or output, opened on a goroutine of
// its own and opened again as it goes away.
type port struct {
	device string
	in     bool
	mu     sync.Mutex
	input  MIDIIn
	output MIDIOut
	err    error
	busy   bool
	tried  time.Time
	gone   atomic.Bool
	// heard is what came in since the last tick.
	heard []midi.Event
	// fresh says the port opened since the last tick: an output then
	// sends everything again, and a controller heard afresh takes its
	// sliders' values over afresh.
	fresh bool
}

// open opens the port's device, where it is not open nor opening, and
// was not tried in the last retryEvery.
func (p *port) open(ports MIDIPorts, now time.Time, changed func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.device == "" || p.busy || p.gone.Load() || now.Sub(p.tried) < retryEvery || p.aliveLocked() {
		return
	}
	p.closeLocked()
	p.busy, p.tried = true, now
	go func() {
		var in MIDIIn
		var out MIDIOut
		var err error
		if p.in {
			in, err = ports.OpenIn(p.device, p.hear)
		} else {
			out, err = ports.OpenOut(p.device)
		}
		p.mu.Lock()
		p.input, p.output, p.err, p.busy, p.fresh = in, out, err, false, in != nil || out != nil
		gone := p.gone.Load()
		p.mu.Unlock()
		if gone {
			p.close()
		}
		if changed != nil {
			changed()
		}
	}()
}

// aliveLocked reports whether the port is open and its device there.
func (p *port) aliveLocked() bool {
	if p.in {
		return p.input != nil && p.input.Err() == nil
	}
	return p.output != nil && p.output.Err() == nil
}

// hear keeps a message that came in, for the next tick.
func (p *port) hear(e midi.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.heard) < 1024 {
		p.heard = append(p.heard, e)
	}
}

// take returns what came in since it was last asked, whether the
// output opened meanwhile, and the output, nil where it is not open.
func (p *port) take(dst []midi.Event) (heard []midi.Event, fresh bool, out MIDIOut) {
	p.mu.Lock()
	defer p.mu.Unlock()
	dst = append(dst, p.heard...)
	p.heard = p.heard[:0]
	fresh, p.fresh = p.fresh, false
	if p.output != nil && p.output.Err() != nil {
		return dst, fresh, nil
	}
	return dst, fresh, p.output
}

// says is what the port says of itself: the device it is open to, or
// why not.
func (p *port) says() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.device == "":
		return "No device"
	case p.err != nil:
		return p.err.Error()
	case p.busy:
		return "Opening…"
	case !p.aliveLocked():
		return "Not connected"
	}
	return p.device
}

// close closes the port, now and once one on its way opens.
func (p *port) close() {
	p.gone.Store(true)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeLocked()
}

func (p *port) closeLocked() {
	if p.input != nil {
		_ = p.input.Close()
		p.input = nil
	}
	if p.output != nil {
		_ = p.output.Close()
		p.output = nil
	}
}

// A hub is the MIDI devices the show listens to and sends to, each
// opened once whatever number of modules use it, as a port on Windows
// opens to one user only: what each input heard this tick, and each
// output, and whether it opened since the last.
type hub struct {
	ins, outs map[string]*port
	heard     map[string][]midi.Event
	sendTo    map[string]MIDIOut
	fresh     map[string]bool
	// freshIn says each input opened since the last tick.
	freshIn map[string]bool
}

func newHub() hub {
	return hub{ins: map[string]*port{}, outs: map[string]*port{}, heard: map[string][]midi.Event{},
		sendTo: map[string]MIDIOut{}, fresh: map[string]bool{}, freshIn: map[string]bool{}}
}

// devices are the MIDI devices s's modules listen to and send to.
func devices(s *show.Show) (ins, outs []string) {
	add := func(to *[]string, d string) {
		if d != "" && !slices.Contains(*to, d) {
			*to = append(*to, d)
		}
	}
	for i := range s.Modules {
		m := &s.Modules[i]
		switch m.Kind {
		case show.Controller:
			// Its lights go back to the output of its name, where it has
			// one.
			add(&ins, m.Device)
			add(&outs, m.Device)
		case show.ClockIn:
			add(&ins, m.Device)
		case show.MIDIOut, show.ClockOut:
			add(&outs, m.Device)
		case show.Routes:
			for _, r := range m.Routes {
				if !r.Triggered() {
					add(&ins, r.Input.DeviceName)
				}
				if r.Output.Kind == "" || r.Output.Kind == "midi" {
					add(&outs, r.Output.DeviceName)
				}
			}
		default:
		}
	}
	return ins, outs
}

// want opens a port for each device of s, and closes those no module
// uses now.
func (h *hub) want(s *show.Show) {
	ins, outs := devices(s)
	keep := func(ports map[string]*port, names []string, in bool) {
		for d, p := range ports {
			if !slices.Contains(names, d) {
				p.close()
				delete(ports, d)
			}
		}
		for _, d := range names {
			if ports[d] == nil {
				ports[d] = &port{device: d, in: in}
			}
		}
	}
	keep(h.ins, ins, true)
	keep(h.outs, outs, false)
}

// tick opens what would not open before, and takes what each input
// heard since the last tick.
func (h *hub) tick(ports MIDIPorts, now time.Time, changed func()) {
	clear(h.fresh)
	clear(h.freshIn)
	for d, p := range h.ins {
		p.open(ports, now, changed)
		h.heard[d], h.freshIn[d], _ = p.take(h.heard[d][:0])
	}
	for d, p := range h.outs {
		p.open(ports, now, changed)
		var fresh bool
		_, fresh, h.sendTo[d] = p.take(nil)
		h.fresh[d] = fresh
	}
	for d := range h.heard {
		if h.ins[d] == nil {
			delete(h.heard, d)
		}
	}
	for d := range h.sendTo {
		if h.outs[d] == nil {
			delete(h.sendTo, d)
		}
	}
}

// out is device's output, nil where it is not open, and whether it
// opened since the last tick.
func (h *hub) out(device string) (MIDIOut, bool) {
	return h.sendTo[device], h.fresh[device]
}

// says is what device's input, or output, says of itself.
func (h *hub) says(device string, in bool) string {
	ports := h.outs
	if in {
		ports = h.ins
	}
	if p := ports[device]; p != nil {
		return p.says()
	}
	if device == "" {
		return "No device"
	}
	return "Not connected"
}

// close closes every port.
func (h *hub) close() {
	for d, p := range h.ins {
		p.close()
		delete(h.ins, d)
	}
	for d, p := range h.outs {
		p.close()
		delete(h.outs, d)
	}
}
