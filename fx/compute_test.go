package fx

import (
	"math"
	"slices"
	"testing"
)

func step(over func(*Step)) Step {
	s := Step{ID: "s1", FixtureID: "dome", Target: 1, From: 255}
	if over != nil {
		over(&s)
	}
	return s
}

// whiteFlash is the driving example: a white flash that fades out over
// half a second while the motor spins, then stops.
var whiteFlash = Program{
	ID: "p1", Name: "White flash",
	Steps: []Step{
		// White intensity: full, scaled by velocity, fading to nothing.
		step(func(s *Step) {
			s.ID, s.Target, s.From, s.To, s.DurationMs, s.ScaleByVelocity = "a", 4, 255, 0, 500, true
		}),
		// Motor: spin now...
		step(func(s *Step) { s.ID, s.Target, s.To = "b", 8, 222 }),
		// ...and stop half a second later.
		step(func(s *Step) { s.ID, s.Target, s.To, s.DelayMs = "c", 8, 0, 500 }),
	},
}

func TestVelocityFactor(t *testing.T) {
	if got := VelocityFactor(127); got != 1 {
		t.Errorf("full velocity = %v, want 1", got)
	}
	if got := VelocityFactor(0); got != 0 {
		t.Errorf("no velocity = %v, want 0", got)
	}
	if got := VelocityFactor(64); math.Abs(got-0.504) > 0.001 {
		t.Errorf("half velocity = %v, want about 0.504", got)
	}
	if got := VelocityFactor(200); got != 1 {
		t.Errorf("an impossible velocity = %v, want 1", got)
	}
	if got := VelocityFactor(-5); got != 0 {
		t.Errorf("a negative velocity = %v, want 0", got)
	}
}

func TestStepValueAt(t *testing.T) {
	t.Run("stays silent until its delay has passed", func(t *testing.T) {
		s := step(func(s *Step) { s.DelayMs, s.To = 500, 99 })
		for _, at := range []float64{0, 499} {
			if _, ok := StepValueAt(s, at, 127); ok {
				t.Errorf("the step spoke at %v", at)
			}
		}
		if got, ok := StepValueAt(s, 500, 127); !ok || got != 99 {
			t.Errorf("at its delay = %d, %v; want 99", got, ok)
		}
	})

	t.Run("snaps to its end value with no duration", func(t *testing.T) {
		s := step(func(s *Step) { s.To = 222 })
		for _, at := range []float64{0, 10000} {
			if got, _ := StepValueAt(s, at, 127); got != 222 {
				t.Errorf("at %v = %d, want 222", at, got)
			}
		}
	})

	t.Run("interpolates across its duration", func(t *testing.T) {
		fade := step(func(s *Step) { s.From, s.To, s.DurationMs = 200, 0, 500 })
		for _, c := range []struct {
			at   float64
			want int
		}{{0, 200}, {250, 100}, {500, 0}, {900, 0}} {
			if got, _ := StepValueAt(fade, c.at, 127); got != c.want {
				t.Errorf("at %v = %d, want %d", c.at, got, c.want)
			}
		}
	})

	t.Run("holds its end value once finished", func(t *testing.T) {
		fade := step(func(s *Step) { s.From, s.To, s.DurationMs = 200, 10, 500 })
		if got, _ := StepValueAt(fade, 900, 127); got != 10 {
			t.Errorf("past the end = %d, want 10", got)
		}
	})

	t.Run("scales both endpoints by velocity when asked", func(t *testing.T) {
		fade := step(func(s *Step) { s.From, s.To, s.DurationMs, s.ScaleByVelocity = 255, 0, 500, true })
		// A half-force hit starts at half and still fades to nothing.
		for _, c := range []struct {
			at       float64
			velocity int
			want     int
		}{{0, 64, 129}, {500, 64, 0}, {0, 127, 255}} {
			if got, _ := StepValueAt(fade, c.at, c.velocity); got != c.want {
				t.Errorf("at %v, velocity %d = %d, want %d", c.at, c.velocity, got, c.want)
			}
		}
	})

	t.Run("ignores velocity when scaling is off", func(t *testing.T) {
		if got, _ := StepValueAt(step(func(s *Step) { s.To = 222 }), 0, 1); got != 222 {
			t.Errorf("= %d, want 222", got)
		}
	})
}

func TestDuration(t *testing.T) {
	if got := Duration(whiteFlash); got != 500 {
		t.Errorf("the white flash lasts %v, want 500", got)
	}
	instant := Program{ID: "x", Name: "set", Steps: []Step{step(func(s *Step) { s.To = 5 })}}
	if got := Duration(instant); got != 0 {
		t.Errorf("a programme of instant steps lasts %v, want 0", got)
	}
	late := Program{ID: "x", Name: "late", Steps: []Step{
		step(func(s *Step) { s.DelayMs, s.DurationMs = 1000, 250 })}}
	if got := Duration(late); got != 1250 {
		t.Errorf("a delayed fade lasts %v, want 1250", got)
	}
}

func TestWhiteFlash(t *testing.T) {
	at := func(t float64, velocity int) []Write { return ValuesAt(whiteFlash, t, velocity, 0) }

	t.Run("opens with white at half and the motor spinning", func(t *testing.T) {
		want := []Write{{"dome", 4, 129}, {"dome", 8, 222}}
		if got := at(0, 64); !slices.Equal(got, want) {
			t.Errorf("= %+v, want %+v", got, want)
		}
	})

	t.Run("fades the white while the motor keeps spinning", func(t *testing.T) {
		mid := at(250, 64)
		// Endpoints are scaled before interpolating and rounded once at
		// the end, so this is 128.5 halved, not 129 halved.
		if mid[0].Value != 64 {
			t.Errorf("white at halfway = %d, want 64", mid[0].Value)
		}
		if mid[1] != (Write{"dome", 8, 222}) {
			t.Errorf("motor at halfway = %+v", mid[1])
		}
	})

	t.Run("ends dark with the motor stopped", func(t *testing.T) {
		end := at(500, 64)
		if end[0] != (Write{"dome", 4, 0}) {
			t.Errorf("white at the end = %+v", end[0])
		}
		// Both motor steps speak; the later one wins after merging.
		want := []Write{{"dome", 4, 0}, {"dome", 8, 0}}
		if got := MergeWrites(end); !slices.Equal(got, want) {
			t.Errorf("merged = %+v, want %+v", got, want)
		}
	})

	t.Run("scales the whole flash with the velocity", func(t *testing.T) {
		if got := at(0, 127)[0].Value; got != 255 {
			t.Errorf("full force = %d, want 255", got)
		}
		if got := at(0, 32)[0].Value; got != 64 {
			t.Errorf("a quarter force = %d, want 64", got)
		}
		// The motor is not velocity-scaled, so it spins the same either way.
		if got := at(0, 32)[1].Value; got != 222 {
			t.Errorf("the motor = %d, want 222", got)
		}
	})
}

func TestMergeWrites(t *testing.T) {
	got := MergeWrites([]Write{{"a", 1, 10}, {"a", 1, 20}, {"b", 1, 30}})
	want := []Write{{"a", 1, 20}, {"b", 1, 30}}
	if !slices.Equal(got, want) {
		t.Errorf("= %+v, want %+v", got, want)
	}
}

func TestBeatTimedSteps(t *testing.T) {
	beatStep := func(over func(*Step)) Step {
		return step(func(s *Step) {
			s.TimeBase = InBeats
			if over != nil {
				over(s)
			}
		})
	}

	t.Run("reads a beat step against beats, not milliseconds", func(t *testing.T) {
		s := beatStep(func(s *Step) { s.DelayBeats, s.To = 2, 99 })
		if _, ok := StepValueAt(s, 1.9, 127); ok {
			t.Error("the step spoke before its beat")
		}
		if got, ok := StepValueAt(s, 2, 127); !ok || got != 99 {
			t.Errorf("on its beat = %d, %v; want 99", got, ok)
		}
	})

	t.Run("fades across a duration in beats", func(t *testing.T) {
		fade := beatStep(func(s *Step) { s.From, s.To, s.DurationBeats = 200, 0, 4 })
		for _, c := range []struct {
			at   float64
			want int
		}{{0, 200}, {2, 100}, {4, 0}} {
			if got, _ := StepValueAt(fade, c.at, 127); got != c.want {
				t.Errorf("at beat %v = %d, want %d", c.at, got, c.want)
			}
		}
	})

	t.Run("drives each clock from its own measure", func(t *testing.T) {
		p := Program{ID: "mix", Name: "mixed", Steps: []Step{
			step(func(s *Step) { s.ID, s.Target, s.To, s.DelayMs = "ms", 1, 255, 100 }),
			beatStep(func(s *Step) { s.ID, s.Target, s.To, s.DelayBeats = "bt", 2, 128, 1 }),
		}}
		// 50 ms in and half a beat in: neither has reached its delay.
		if got := ValuesAt(p, 50, 127, 0.5); len(got) != 0 {
			t.Errorf("= %+v, want nothing", got)
		}
		// 150 ms, still half a beat: the ms step fires and the beat step waits.
		if got := ValuesAt(p, 150, 127, 0.5); !slices.Equal(got, []Write{{"dome", 1, 255}}) {
			t.Errorf("= %+v", got)
		}
		// 150 ms and a whole beat: both fire.
		want := []Write{{"dome", 1, 255}, {"dome", 2, 128}}
		if got := ValuesAt(p, 150, 127, 1); !slices.Equal(got, want) {
			t.Errorf("= %+v, want %+v", got, want)
		}
	})

	t.Run("measures the two durations separately", func(t *testing.T) {
		p := Program{ID: "d", Name: "durations", Steps: []Step{
			step(func(s *Step) { s.DelayMs, s.DurationMs = 200, 300 }),
			beatStep(func(s *Step) { s.DelayBeats, s.DurationBeats = 1, 3 }),
		}}
		if got := Duration(p); got != 500 {
			t.Errorf("the millisecond part lasts %v, want 500", got)
		}
		if got := DurationBeats(p); got != 4 {
			t.Errorf("the beat part lasts %v, want 4", got)
		}
	})
}
