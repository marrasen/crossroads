package engine

import (
	"math"
	"slices"
	"time"

	"github.com/marrasen/crossroads/fx"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim-dmx/mix"
)

// An fxRun is an effect programme playing: from when, counted in
// milliseconds, from which beat once it began, and at what velocity.
type fxRun struct {
	program         fx.Program
	velocity        int
	startMs         float64
	startBeat       float64
	haveBeat        bool
	durMs, durBeats float64
}

// fxRunner plays effect programmes into a layer: what midittt's effect
// engine did, run by the engine's tick rather than a ticker of its own.
// What a programme last set stays set once it ends, as it did.
type fxRunner struct {
	runs []*fxRun
}

// msOf is t on the effect runner's clock, in milliseconds.
func msOf(t time.Time) float64 { return float64(t.UnixNano()) / float64(time.Millisecond) }

// quantum is the beats a programme's start waits for the next of.
var quantum = map[fx.Quantize]float64{fx.QuantizeBeat: 1, fx.QuantizeBar: 4}

// trigger plays p at velocity, at now, or at the next beat or bar of c
// where p waits for one and c plays. A programme already playing
// starts over.
func (f *fxRunner) trigger(p fx.Program, velocity int, now time.Time, c Count) {
	f.runs = slices.DeleteFunc(f.runs, func(r *fxRun) bool { return r.program.ID == p.ID })
	start := msOf(now)
	if q, ok := quantum[p.Quantize]; ok && c.Sure && c.BPM > 0 {
		until := q - math.Mod(c.Beats, q)
		if until >= q {
			until = 0
		}
		start += until * 60000 / c.BPM
	}
	f.runs = append(f.runs, &fxRun{program: p, velocity: velocity, startMs: start, durMs: fx.Duration(p), durBeats: fx.DurationBeats(p)})
}

// tick writes what each programme playing sets now into l, the oldest
// first, so the newest wins a channel two share.
func (f *fxRunner) tick(now time.Time, c Count, lights []fixtures.Patch, cat fixtures.Catalog, l *mix.Layer) {
	t := msOf(now)
	clocked := c.BPM > 0
	var writes []fx.Write
	for _, r := range f.runs {
		if t < r.startMs {
			continue
		}
		if !r.haveBeat && clocked {
			r.startBeat, r.haveBeat = c.Beats, true
		}
		local := min(t-r.startMs, r.durMs)
		var beats float64
		if r.haveBeat {
			beats = min(c.Beats-r.startBeat, r.durBeats)
		}
		writes = append(writes, fx.ValuesAt(r.program, local, r.velocity, beats)...)
		msDone := t-r.startMs >= r.durMs
		beatsDone := r.durBeats <= 0 || !clocked || !c.Sure || !r.haveBeat || c.Beats-r.startBeat >= r.durBeats
		if msDone && beatsDone {
			r.durMs = -1
		}
	}
	f.runs = slices.DeleteFunc(f.runs, func(r *fxRun) bool { return r.durMs < 0 })
	for _, w := range fx.MergeWrites(writes) {
		p, ok := fixtures.FindPatch(lights, w.FixtureID)
		if !ok {
			continue
		}
		slot, ok := cat.ResolveChannel(p, w.Target)
		if !ok {
			continue
		}
		l.Set(slot, byte(min(max(cat.CurveValueAt(p, w.Target, w.Value), 0), 255)))
	}
}

// playing reports whether any programme plays, or waits to.
func (f *fxRunner) playing() bool { return len(f.runs) > 0 }

// FireEffect fires Effect module at the next tick, as a trigger into it
// would.
func (e *Engine) FireEffect(module string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pressed[module] = true
}

// effect runs Effect module m: a trigger in, or a press on the Live
// tab, plays its programme at full velocity over its lights.
func (e *Engine) effect(m *show.Module, cat fixtures.Catalog, now time.Time, live *Live) {
	l := e.layers[m.ID]
	f := e.effects[m.ID]
	if l == nil || f == nil {
		return
	}
	c, _ := e.input(m.ID, "clock").(Count)
	fired, _ := e.input(m.ID, "go").(bool)
	if fired || e.pressed[m.ID] {
		if p, ok := e.programs[m.Program]; ok {
			f.trigger(p, 127, now, c)
			e.note(now, m.Name+" fired "+p.DisplayName())
		}
	}
	f.tick(now, c, e.s.Rig.Lights, cat, l)
	e.values[portKey{m.ID, "out"}] = l
	if f.playing() {
		live.Values[m.ID] = 1
	}
}
