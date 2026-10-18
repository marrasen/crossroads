package tempo

import (
	"math"
	"slices"
	"testing"
)

func beat(over func(*Clock)) Clock {
	c := Clock{SourceID: "link", BPM: 120, RefTimeMs: 1000, Playing: true}
	if over != nil {
		over(&c)
	}
	return c
}

func closeTo(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

func TestPulsesInWindowCountsTwentyFourPerBeat(t *testing.T) {
	// One beat at 120 BPM is 500 ms.
	if got := len(PulsesInWindow(beat(nil), 1000, 1500)); got != PPQN {
		t.Errorf("got %d pulses, want %d", got, PPQN)
	}
}

func TestPulsesInWindowSpacesThemEvenly(t *testing.T) {
	times := PulsesInWindow(beat(nil), 1000, 1500)
	for i, want := range map[int]float64{0: 1000, 1: 1000 + 500.0/24, 23: 1500 - 500.0/24} {
		if !closeTo(times[i], want) {
			t.Errorf("pulse %d at %v, want %v", i, times[i], want)
		}
	}
}

func TestPulsesInWindowScalesWithTempo(t *testing.T) {
	// 240 BPM is a beat every 250 ms, so twice the pulses in one window.
	c := beat(func(c *Clock) { c.BPM = 240 })
	if got := len(PulsesInWindow(c, 1000, 1500)); got != PPQN*2 {
		t.Errorf("got %d pulses, want %d", got, PPQN*2)
	}
}

func TestPulsesInWindowNeverRepeatsOrDrops(t *testing.T) {
	c := beat(func(c *Clock) { c.BPM = 137.3 })
	var all []float64
	for from := 1000.0; from < 3000; from += 125 {
		all = append(all, PulsesInWindow(c, from, from+125)...)
	}
	direct := PulsesInWindow(c, 1000, 3000)
	if !slices.Equal(all, direct) {
		t.Errorf("%d pulses over small windows, %d over one", len(all), len(direct))
	}
	if len(slices.Compact(slices.Clone(all))) != len(all) {
		t.Error("a pulse was repeated")
	}
}

func TestPulsesInWindowIsHalfOpen(t *testing.T) {
	// 1000 is exactly a pulse, and so is 1500.
	if !slices.Contains(PulsesInWindow(beat(nil), 1000, 1500), 1000) {
		t.Error("the pulse on the opening boundary was dropped")
	}
	if slices.Contains(PulsesInWindow(beat(nil), 1000, 1500), 1500) {
		t.Error("the pulse on the closing boundary was taken twice")
	}
	if !slices.Contains(PulsesInWindow(beat(nil), 1500, 2000), 1500) {
		t.Error("the later window did not take the boundary pulse")
	}
}

func TestPulsesInWindowBeforeTheReference(t *testing.T) {
	c := beat(func(c *Clock) { c.BeatAtRef, c.RefTimeMs = 8, 5000 })
	times := PulsesInWindow(c, 1000, 1500)
	if len(times) != PPQN || !closeTo(times[0], 1000) {
		t.Errorf("got %d pulses starting at %v", len(times), times[0])
	}
}

func TestPulsesInWindowEmptyCases(t *testing.T) {
	cases := []struct {
		name     string
		c        Clock
		from, to float64
	}{
		{"stopped", beat(func(c *Clock) { c.Playing = false }), 1000, 1500},
		{"empty window", beat(nil), 1500, 1500},
		{"inverted window", beat(nil), 1500, 1000},
		{"no tempo", beat(func(c *Clock) { c.BPM = 0 }), 1000, 1500},
		{"negative tempo", beat(func(c *Clock) { c.BPM = -120 }), 1000, 1500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PulsesInWindow(c.c, c.from, c.to); len(got) != 0 {
				t.Errorf("got %d pulses, want none", len(got))
			}
		})
	}
}

func TestPulsesInWindowCapsAPathologicalWindow(t *testing.T) {
	if got := len(PulsesInWindow(beat(nil), 1000, 1e9)); got != maxPulses {
		t.Errorf("got %d pulses, want the cap of %d", got, maxPulses)
	}
}

// A harness drives a ClockOut on a clock the test moves by hand.
type harness struct {
	out     *ClockOut
	at      float64
	current Clock
	have    bool
	// deliver stands in for "no output device matched", which the port
	// list reports by returning false rather than failing.
	deliver bool
	sent    []struct {
		b      byte
		timeMs float64
	}
}

func newHarness(initial Clock, have bool) *harness {
	h := &harness{current: initial, have: have, deliver: true}
	h.out = NewClockOut(
		func() (Clock, bool) { return h.current, h.have },
		func(bytes []byte, timeMs float64) bool {
			if !h.deliver {
				return false
			}
			h.sent = append(h.sent, struct {
				b      byte
				timeMs float64
			}{bytes[0], timeMs})
			return true
		})
	h.out.now = func() float64 { return h.at }
	return h
}

// advance moves the clock on in scheduler ticks.
func (h *harness) advance(ms float64) {
	for range int(ms / 25) {
		h.at += 25
		h.out.Tick()
	}
}

// transport is the start and stop bytes, which most assertions care about.
func (h *harness) transport() []byte {
	var out []byte
	for _, s := range h.sent {
		if s.b != ClockByte {
			out = append(out, s.b)
		}
	}
	return out
}

func (h *harness) pulses() []float64 {
	var out []float64
	for _, s := range h.sent {
		if s.b == ClockByte {
			out = append(out, s.timeMs)
		}
	}
	return out
}

// anchored is a clock starting at the harness's time origin, so pulse
// times read easily.
func anchored(over func(*Clock)) Clock {
	return beat(func(c *Clock) {
		c.RefTimeMs, c.BeatAtRef = 0, 0
		if over != nil {
			over(c)
		}
	})
}

func TestClockOutSendsStartPulsesStop(t *testing.T) {
	h := newHarness(anchored(nil), true)
	h.out.Tick()
	if got := h.transport(); !slices.Equal(got, []byte{StartByte}) {
		t.Errorf("transport = %x, want a start", got)
	}
	if len(h.pulses()) == 0 {
		t.Error("no pulses were scheduled")
	}
	h.out.Stop()
	if got := h.transport(); !slices.Equal(got, []byte{StartByte, StopByte}) {
		t.Errorf("transport = %x, want a start then a stop", got)
	}
}

func TestClockOutSchedulesAheadWithoutRepeating(t *testing.T) {
	h := newHarness(anchored(nil), true)
	h.out.Tick()
	h.advance(1000)
	pulses := h.pulses()
	// A second at 120 BPM is two beats, so 48 pulses, plus whatever the
	// last window reached past the second.
	if len(pulses) < 48 {
		t.Errorf("got %d pulses in a second, want at least 48", len(pulses))
	}
	for i := 1; i < len(pulses); i++ {
		if pulses[i] <= pulses[i-1] {
			t.Fatalf("pulse %d at %v does not follow %v", i, pulses[i], pulses[i-1])
		}
	}
}

func TestClockOutNeverSchedulesInThePast(t *testing.T) {
	h := newHarness(anchored(nil), true)
	h.out.Tick()
	seen := len(h.pulses())
	for range 20 {
		at := h.at
		h.advance(25)
		for _, time := range h.pulses()[seen:] {
			if time < at {
				t.Fatalf("a pulse was scheduled at %v, before %v", time, at)
			}
		}
		seen = len(h.pulses())
	}
}

func TestClockOutKeepsRunningWhileTheSourceIsStopped(t *testing.T) {
	h := newHarness(anchored(func(c *Clock) { c.Playing = false }), true)
	h.out.Tick()
	h.advance(500)
	// No start message, since the transport never ran, but pulses keep
	// the slave locked to the
	if got := h.transport(); len(got) != 0 {
		t.Errorf("transport = %x, want nothing", got)
	}
	if len(h.pulses()) == 0 {
		t.Error("no pulses were scheduled")
	}
}

func TestClockOutFollowsTheSourceTransport(t *testing.T) {
	h := newHarness(anchored(func(c *Clock) { c.Playing = false }), true)
	h.out.Tick()
	h.advance(100)
	h.current = anchored(func(c *Clock) { c.Playing, c.RefTimeMs = true, h.at })
	h.advance(50)
	if got := h.transport(); !slices.Equal(got, []byte{StartByte}) {
		t.Fatalf("transport = %x, want a start", got)
	}
	h.current = anchored(func(c *Clock) { c.Playing, c.RefTimeMs = false, h.at })
	h.advance(50)
	if got := h.transport(); !slices.Equal(got, []byte{StartByte, StopByte}) {
		t.Errorf("transport = %x, want a start then a stop", got)
	}
}

func TestClockOutStopsCleanlyWithNoSource(t *testing.T) {
	h := newHarness(Clock{}, false)
	h.out.Tick()
	h.advance(100)
	h.out.Stop()
	if len(h.sent) != 0 {
		t.Errorf("sent %d messages with no source, want none", len(h.sent))
	}
}

func TestClockOutNeverResendsAQueuedPulse(t *testing.T) {
	// A pulse handed over cannot be recalled, so the scheduling horizon
	// must never rewind behind what has been queued. A restart or a
	// transport edge that re-filled that span would double the pulse
	// density and leave the device permanently ahead of the grid.
	h := newHarness(anchored(nil), true)
	h.out.Tick()
	h.advance(200)
	h.out.Stop()
	h.out.Tick()
	h.advance(200)
	assertDistinctAndRising(t, h.pulses())
}

func TestClockOutDoesNotRepeatOnATransportChange(t *testing.T) {
	h := newHarness(anchored(func(c *Clock) { c.Playing = false }), true)
	h.out.Tick()
	h.advance(200)
	// Resume on a grid anchored somewhere else entirely, as Link does.
	h.current = anchored(func(c *Clock) {
		c.Playing, c.BeatAtRef, c.RefTimeMs = true, 12.37, h.at
	})
	h.advance(200)
	assertDistinctAndRising(t, h.pulses())
}

func TestClockOutKeepsTryingToStart(t *testing.T) {
	// The chosen output is unplugged when the transport starts. Claiming
	// the start landed would leave the device on an unbroken clock
	// stream with no Start to act on, and nothing would ever retry.
	h := newHarness(anchored(nil), true)
	h.deliver = false
	h.out.Tick()
	h.advance(200)
	if got := h.transport(); len(got) != 0 {
		t.Fatalf("transport = %x, want nothing to have been delivered", got)
	}
	h.deliver = true
	h.advance(50)
	if got := h.transport(); !slices.Equal(got, []byte{StartByte}) {
		t.Errorf("transport = %x, want the start to have been retried", got)
	}
}

func TestClockOutIsSafeToStopTwice(t *testing.T) {
	h := newHarness(anchored(nil), true)
	h.out.Tick()
	h.advance(100)
	h.out.Stop()
	h.out.Stop()
	if got := h.transport(); !slices.Equal(got, []byte{StartByte, StopByte}) {
		t.Errorf("transport = %x, want one start and one stop", got)
	}
}

func assertDistinctAndRising(t *testing.T, pulses []float64) {
	t.Helper()
	for i := 1; i < len(pulses); i++ {
		if pulses[i] <= pulses[i-1] {
			t.Fatalf("pulse %d at %v does not follow %v", i, pulses[i], pulses[i-1])
		}
	}
}
