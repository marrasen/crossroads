package fx

import "github.com/marrasen/gunim-dmx/dmx"

// VelocityFactor reads a note velocity as a factor from zero to one.
func VelocityFactor(velocity int) float64 {
	return min(1, max(0, float64(velocity)/127))
}

// scaled applies velocity scaling to one of a step's endpoint values.
func scaled(value int, s Step, velocity int) float64 {
	if s.ScaleByVelocity {
		return float64(value) * VelocityFactor(velocity)
	}
	return float64(value)
}

// timing is a step's delay and duration in whichever unit it is timed in.
func timing(s Step) (delay, duration float64) {
	if s.TimeBase == InBeats {
		return s.DelayBeats, s.DurationBeats
	}
	return s.DelayMs, s.DurationMs
}

// StepValueAt is the value a step asserts t into the programme, and
// false while it is still waiting out its delay.
//
// t is in the step's own unit — milliseconds for a plain step, beats
// for a beat-timed one — which the engine supplies from the matching
// elapsed measure.
//
// Once a step has finished it keeps asserting its end value for the
// rest of the run. That is what lets a later step on the same channel
// override an earlier one: the engine applies steps in list order, so
// the last one to speak wins.
func StepValueAt(s Step, t float64, velocity int) (int, bool) {
	delay, duration := timing(s)
	if t < delay {
		return 0, false
	}
	local := t - delay
	if duration <= 0 || local >= duration {
		return int(dmx.Clamp8f(scaled(s.To, s, velocity))), true
	}
	from, to := scaled(s.From, s, velocity), scaled(s.To, s, velocity)
	return int(dmx.Clamp8f(from + (to-from)*(local/duration))), true
}

// Duration is how long the millisecond-timed part of a programme runs:
// the latest moment at which any such step is still moving.
//
// Beat-timed steps do not count, since their length depends on tempo;
// DurationBeats measures those. A programme of instant steps lasts no
// time at all, which still produces one frame of output.
func Duration(p Program) float64 {
	var end float64
	for _, s := range p.Steps {
		if s.TimeBase == InBeats {
			continue
		}
		end = max(end, max(0, s.DelayMs)+max(0, s.DurationMs))
	}
	return end
}

// DurationBeats is how long the beat-timed part of a programme runs, in
// beats, and zero when it has none.
func DurationBeats(p Program) float64 {
	var end float64
	for _, s := range p.Steps {
		if s.TimeBase != InBeats {
			continue
		}
		end = max(end, max(0, s.DelayBeats)+max(0, s.DurationBeats))
	}
	return end
}

// ValuesAt is every channel write a programme wants, in step order.
//
// elapsedMs drives plain steps and elapsedBeats the beat-timed ones, so
// the two clocks can live in one programme. Channels may repeat: the
// caller applies them in order, so later steps win.
func ValuesAt(p Program, elapsedMs float64, velocity int, elapsedBeats float64) []Write {
	var writes []Write
	for _, s := range p.Steps {
		t := elapsedMs
		if s.TimeBase == InBeats {
			t = elapsedBeats
		}
		value, ok := StepValueAt(s, t, velocity)
		if !ok {
			continue
		}
		writes = append(writes, Write{FixtureID: s.FixtureID, Target: s.Target, Value: value})
	}
	return writes
}

// MergeWrites collapses repeated channels so each is written once,
// keeping the last, and holds the order the channels first appeared in.
func MergeWrites(writes []Write) []Write {
	type key struct {
		fixtureID string
		target    int
	}
	index := map[key]int{}
	var out []Write
	for _, w := range writes {
		k := key{w.FixtureID, w.Target}
		if i, seen := index[k]; seen {
			out[i] = w
			continue
		}
		index[k] = len(out)
		out = append(out, w)
	}
	return out
}
