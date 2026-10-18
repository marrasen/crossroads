package engine

import (
	"strings"
	"time"

	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/crossroads/tempo"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim-dmx/mix"
	"github.com/marrasen/gunim/audio/midi"
)

// A router is a MIDI routes module's state: midittt's routing engine
// with its routes, the light its routes set, the patterns its routes
// hold, by pattern and by the note holding each, and how its clock's
// transport and the Link session's stood at the last tick.
type router struct {
	eng      *routing.Engine
	layer    *mix.Layer
	holds    map[string]map[string]bool
	sure     bool
	linkWas  bool
	linkSeen bool
	// fired are the routes that fired this tick, for the log; fx the
	// effect programmes its routes play, and clock the clock wired in,
	// as this tick read it.
	fired []routing.Fired
	fx    fxRunner
	clock Count
}

// newRouter makes module m's router, whose routes act on e.
func (e *Engine) newRouter(m *show.Module) *router {
	r := &router{layer: mix.NewLayer(), holds: map[string]map[string]bool{}}
	r.eng = routing.NewEngine(e.routeSend, e.routeTimer, func(slot, v int) { r.layer.Set(slot, byte(min(max(v, 0), 255))) },
		func(program string, velocity int) {
			if p, ok := e.programs[program]; ok {
				r.fx.trigger(p, velocity, e.last, r.clock)
			}
		})
	r.eng.SetRunPattern(func(action routing.PatternAction, id, key string) bool { return r.runPattern(action, id, key) })
	r.eng.SetRunLink(e.routeLink)
	e.layerOf[r.layer] = m.ID
	return r
}

// routeSend sends a route's MIDI bytes to the output of device.
func (e *Engine) routeSend(device string, b []byte) {
	out, _ := e.hub.out(device)
	if out == nil || len(b) == 0 {
		return
	}
	m := midi.Message{Status: b[0]}
	if len(b) > 1 {
		m.Data1 = b[1]
	}
	if len(b) > 2 {
		m.Data2 = b[2]
	}
	_ = out.Send(time.Time{}, m)
}

// routeTimer tells the Timer of id what a route says, at once: the
// routes run within a tick, as the timers do.
func (e *Engine) routeTimer(id string, c routing.TimerCommand) {
	if t := e.timers[id]; t != nil {
		t.asked = append(t.asked, TimerCommand(c))
	}
}

// routeLink moves the Link session's transport, where a Link module
// runs it.
func (e *Engine) routeLink(c routing.LinkCommand) bool {
	f := &e.sync.link
	if !f.on || f.err != nil {
		return false
	}
	playing := f.f.Snapshot().Playing
	switch c {
	case routing.LinkStart:
		playing = true
	case routing.LinkStop:
		playing = false
	case routing.LinkToggle:
		playing = !playing
	}
	f.f.SetPlaying(playing)
	return true
}

// runPattern holds, or lets go of, pattern id under key, as a route
// says; the Pattern modules playing it play while any key holds it.
func (r *router) runPattern(action routing.PatternAction, id, key string) bool {
	held := r.holds[id]
	if held == nil {
		held = map[string]bool{}
		r.holds[id] = held
	}
	switch action {
	case routing.PatternHold, routing.PatternStart:
		held[key] = true
	case routing.PatternRelease:
		if !held[key] {
			return false
		}
		delete(held, key)
	case routing.PatternStop:
		clear(held)
	case routing.PatternToggle:
		if len(held) > 0 {
			clear(held)
		} else {
			held[key] = true
		}
	}
	return true
}

// patternHeld reports whether any MIDI routes module holds pattern id.
func (e *Engine) patternHeld(id string) bool {
	for _, r := range e.routers {
		if len(r.holds[id]) > 0 {
			return true
		}
	}
	return false
}

// route runs MIDI routes module m: each message heard on its routes'
// devices, and each beat, transport move and Link start or stop of the
// clock wired in, through its routes.
func (e *Engine) route(m *show.Module, cat fixtures.Catalog, now time.Time, live *Live) {
	r := e.routers[m.ID]
	if r == nil {
		return
	}
	r.eng.SetCatalog(cat)
	r.fired = r.fired[:0]
	r.clock, _ = e.input(m.ID, "clock").(Count)
	for device, heard := range e.hub.heard {
		for _, ev := range heard {
			msg, ok := routing.Parse(ev.AppendTo(nil))
			if ok {
				r.fired = append(r.fired, r.eng.HandleMessage(device, msg)...)
			}
		}
	}
	if c, ok := e.input(m.ID, "clock").(Count); ok && c.BPM > 0 {
		if c.Sure {
			r.fired = append(r.fired, r.eng.HandleBeat(c.Beats)...)
		}
		if c.Sure != r.sure {
			edge := tempo.TransportStop
			if c.Sure {
				edge = tempo.TransportPlay
			}
			r.fired = append(r.fired, r.eng.HandleTransport(edge)...)
			r.sure = c.Sure
		}
	}
	if f := &e.sync.link; f.on && f.err == nil {
		playing := f.f.Snapshot().Playing
		if r.linkSeen && playing != r.linkWas {
			r.fired = append(r.fired, r.eng.HandleLinkTransport(playing)...)
		}
		r.linkWas, r.linkSeen = playing, true
	}
	names := map[string]string{}
	for _, rt := range m.Routes {
		names[rt.ID] = rt.DisplayName()
	}
	for _, f := range r.fired {
		e.note(now, m.Name+": "+names[f.RouteID]+" "+firedText(f))
	}
	r.fx.tick(now, r.clock, e.s.Rig.Lights, cat, r.layer)
	e.values[portKey{m.ID, "out"}] = r.layer
	e.values[portKey{m.ID, "fired"}] = len(r.fired) > 0
	if len(r.fired) > 0 {
		live.Values[m.ID] = 1
	}
}

// firedText says what a fired route did.
func firedText(f routing.Fired) string {
	switch f.Kind {
	case routing.FiredMidi:
		return "sent MIDI to " + f.DeviceName
	case routing.FiredTimer:
		return string(f.Command) + " a timer"
	case routing.FiredDmx:
		if f.Label != "" {
			return "set " + f.Label
		}
		return "set a channel"
	case routing.FiredGroup:
		return "set " + f.Label
	case routing.FiredPattern:
		return strings.ToLower(f.Label) + " a pattern"
	case routing.FiredLink:
		return f.Label + " Link"
	case routing.FiredFx:
		return "fired an effect"
	}
	return "fired"
}
