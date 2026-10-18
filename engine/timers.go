package engine

import (
	"math"
	"time"

	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// A TimerCommand is what a timer is told: to start, to pause, or to go
// back to its start.
type TimerCommand string

// The commands.
const (
	TimerStart TimerCommand = "start"
	TimerPause TimerCommand = "pause"
	TimerReset TimerCommand = "reset"
)

// A timer is a Timer module's state: whether it runs, the time it ran
// before it last started, when that was, and whether it has said it is
// done.
type timer struct {
	running bool
	gone    time.Duration
	since   time.Time
	done    bool
	// asked are the commands from the Live tab since the last tick.
	asked []TimerCommand
}

// TimerState is what a Timer shows: the time left on a countdown, past
// zero into overtime below it, or gone on a stopwatch; whether it runs;
// and whether a countdown has reached zero.
type TimerState struct {
	Shown    time.Duration
	Running  bool
	Finished bool
}

// TimerDo tells Timer module c, at the next tick, as a trigger into it
// would.
func (e *Engine) TimerDo(module string, c TimerCommand) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t := e.timers[module]; t != nil {
		t.asked = append(t.asked, c)
	}
}

// count runs Timer module m: its triggers in start, pause and reset
// it; it fires done as a countdown reaches zero, and its value is how
// much of a countdown is left.
func (e *Engine) count(m *show.Module, now time.Time, live *Live) {
	t := e.timers[m.ID]
	if t == nil {
		t = &timer{}
		e.timers[m.ID] = t
	}
	do := func(c TimerCommand) {
		switch c {
		case TimerStart:
			if !t.running {
				t.running, t.since = true, now
			}
		case TimerPause:
			if t.running {
				t.gone += now.Sub(t.since)
				t.running = false
			}
		case TimerReset:
			t.gone, t.since, t.done = 0, now, false
		}
	}
	for _, c := range t.asked {
		do(c)
	}
	t.asked = t.asked[:0]
	for _, c := range []TimerCommand{TimerStart, TimerPause, TimerReset} {
		if fired, _ := e.input(m.ID, string(c)).(bool); fired {
			do(c)
		}
	}
	gone := t.gone
	if t.running {
		gone += now.Sub(t.since)
	}
	st := TimerState{Shown: gone, Running: t.running}
	left := 0.0
	fired := false
	if !m.Stopwatch {
		total := time.Duration(m.Seconds * float64(time.Second))
		st.Shown = total - gone
		st.Finished = st.Shown <= 0
		if total > 0 {
			left = min(max(float64(st.Shown)/float64(total), 0), 1)
		}
		if st.Finished && !t.done {
			fired = true
			e.note(now, m.Name+" reached zero")
		}
		t.done = st.Finished
	}
	e.values[portKey{m.ID, "done"}] = fired
	e.values[portKey{m.ID, "left"}] = left
	live.Values[m.ID] = left
	live.Timers[m.ID] = st
}

// playPattern runs Pattern module m: while it is held or on, its pattern on
// its lights at where the clock wired in stands in it, the pattern
// looping and locked to the beat, so its third beat falls on the bar's
// third wherever it began. Let go, it leaves its lights as they are, or
// gives them back, as the pattern says.
func (e *Engine) playPattern(m *show.Module, cat fixtures.Catalog, live *Live) {
	l := e.layers[m.ID]
	if l == nil {
		return
	}
	e.values[portKey{m.ID, "out"}] = l
	p, ok := e.patterns[m.Pattern]
	c, clocked := e.input(m.ID, "clock").(Count)
	on := m.Value >= 0.5 || e.held[portKey{m.ID, "play"}] || e.patternHeld(m.Pattern)
	if v, wired := e.input(m.ID, "play").(float64); wired {
		on = on || v >= 0.5
	}
	if !ok || !on || !clocked || c.BPM <= 0 {
		if !ok || p.Release.Rest {
			l.Clear()
		}
		return
	}
	live.Values[m.ID] = 1
	pos := math.Mod(c.Beats, p.Length())
	if pos < 0 {
		pos += p.Length()
	}
	l.Clear()
	for _, w := range pattern.ValuesAt(p, pos, e.s.Rig.Lights, e.s.Rig.Groups, cat) {
		v := w.Value
		if fp, found := fixtures.FindPatch(e.s.Rig.Lights, w.FixtureID); found {
			v = cat.CurveValueAt(fp, w.Offset, v)
		}
		l.Set(w.Slot, byte(min(max(v, 0), 255)))
	}
}

// Audition plays pattern p on the stage, through every mixer, as the
// pattern editor's test button is held, or stops it with nil: on the
// first Tempo's beat, or at 120 where the show has none.
func (e *Engine) Audition(p *pattern.Pattern) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p == nil {
		e.audition = nil
		e.audLayer.Clear()
		return
	}
	c := *p
	e.audition = &c
}

// audit plays the pattern auditioned, dt after the last tick.
func (e *Engine) audit(dt float64, cat fixtures.Catalog) {
	e.audLayer.Clear()
	p := e.audition
	if p == nil {
		return
	}
	beats := -1.0
	for _, id := range e.order {
		if m := e.s.Module(id); m != nil && m.Kind == show.Tempo {
			if c, ok := e.values[portKey{id, "out"}].(Count); ok && c.BPM > 0 {
				beats = c.Beats
			}
			break
		}
	}
	if beats < 0 {
		e.audBeats += dt * 2
		beats = e.audBeats
	}
	pos := math.Mod(beats, p.Length())
	if pos < 0 {
		pos += p.Length()
	}
	for _, w := range pattern.ValuesAt(*p, pos, e.s.Rig.Lights, e.s.Rig.Groups, cat) {
		v := w.Value
		if fp, found := fixtures.FindPatch(e.s.Rig.Lights, w.FixtureID); found {
			v = cat.CurveValueAt(fp, w.Offset, v)
		}
		e.audLayer.Set(w.Slot, byte(min(max(v, 0), 255)))
	}
}
