package tempo

import (
	"math"
	"sync"
	"time"
)

// PPQN is the MIDI clock's pulses per quarter note, fixed by the spec.
const PPQN = 24

// The system real-time bytes. None of these needs SysEx permission.
const (
	ClockByte = 0xf8
	StartByte = 0xfa
	StopByte  = 0xfc
	// ContinueByte plays on from where a stop left off, and
	// SongPositionByte says where in the song that is, in sixteenth
	// notes. crossroads reads both and sends neither.
	ContinueByte     = 0xfb
	SongPositionByte = 0xf2
)

// tickEvery is how often the scheduler wakes, and lookahead is how far
// ahead it schedules.
//
// The lookahead is the bridge's tempo latency: pulses already handed
// over cannot be recalled, so a tempo change reaches the device up to
// this late. Keep it comfortably above the tick, and no more.
const (
	tickEvery = 25 * time.Millisecond
	lookahead = 60 // milliseconds
)

// pulseEpsilon nudges past the last queued pulse when scheduling again,
// so a replacement grid cannot land a duplicate on a timestamp already
// sent.
const pulseEpsilon = 1e-6

// maxPulses caps how many pulses one window may hold. A window normally
// holds about five; this only bites if the clock is handed an absurd
// tempo, and stops a bad value locking up the tick.
const maxPulses = 1000

// PulsesInWindow is the times, on the tempo package's monotonic scale,
// of every clock pulse due in the half-open window from fromMs to toMs.
//
// Half-open is what lets consecutive windows abut without repeating a
// pulse or dropping one: a pulse landing exactly on the boundary
// belongs to the later window alone. Pulses sit at whole
// twenty-fourths of a beat, so they come from the beat grid rather
// than accumulating, and never drift.
func PulsesInWindow(c Clock, fromMs, toMs float64) []float64 {
	perBeat := MsPerBeat(c.BPM)
	if !c.Playing || math.IsInf(perBeat, 0) {
		return nil
	}
	var times []float64
	endPulse := BeatsAt(c, toMs) * PPQN
	for pulse := math.Ceil(BeatsAt(c, fromMs) * PPQN); pulse < endPulse && len(times) < maxPulses; pulse++ {
		times = append(times, c.RefTimeMs+(pulse/PPQN-c.BeatAtRef)*perBeat)
	}
	return times
}

// freeRunning is the clock a stopped source still produces pulses from.
//
// Slaves use the pulse stream to hold tempo and treat start and stop
// purely as a transport gate, and several fall back to their internal
// clock the moment pulses stop arriving. Letting the held snapshot run
// on from where it stopped keeps them locked.
func freeRunning(c Clock) Clock {
	c.Playing = true
	return c
}

// A SendAt sends raw bytes, at a future time on the tempo package's
// monotonic scale when timeMs is not zero. It reports false when the
// bytes went nowhere — no port matched, or none was chosen — so the
// caller can tell an unheard message from a delivered one.
type SendAt func(bytes []byte, timeMs float64) bool

// A ClockOut re-emits the selected tempo source as MIDI clock, turning
// crossroads into a Link-to-MIDI-clock bridge, and a tap-to-MIDI-clock one.
//
// Pulses are scheduled, not fired: each tick hands the port every pulse
// due in the next lookahead, each with its own timestamp. A ticker
// firing every 20.8 ms would carry every scrap of its own jitter into
// the clock, which is audible.
//
// Start and stop are sent at once on the transport edge. Continue and
// song position are deliberately absent: crossroads' beat clock is a
// free-running counter with no song origin, so it has no position to
// continue from.
type ClockOut struct {
	mu sync.Mutex
	// getClock reads the selected tempo source, and reports false when
	// none is driving.
	getClock func() (Clock, bool)
	send     SendAt
	now      func() float64

	stop chan struct{}
	// scheduledToMs is the end of the window already handed over, so
	// windows stay contiguous.
	scheduledToMs float64
	// lastPulseMs is the timestamp of the last pulse handed over.
	// Nothing queued can be taken back, so this is the earliest a
	// replacement grid may start — never rewind behind it, or the
	// device hears the same span twice at double density.
	lastPulseMs float64
	// startSent says the device has been sent a Start it really received.
	startSent bool
}

// NewClockOut makes a bridge reading getClock and sending through send.
func NewClockOut(getClock func() (Clock, bool), send SendAt) *ClockOut {
	return &ClockOut{getClock: getClock, send: send, now: Now}
}

// Start begins bridging. Calling it again while it runs does nothing,
// so a settings change can call it freely.
func (c *ClockOut) Start() {
	c.mu.Lock()
	if c.stop != nil {
		c.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	c.stop = stop
	c.mu.Unlock()
	go func() {
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.Tick()
			}
		}
	}()
	c.Tick()
}

// Stop ends bridging, telling the device to stop if it was ever told to
// start.
func (c *ClockOut) Stop() {
	c.mu.Lock()
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
	sendStop := c.startSent
	c.startSent = false
	c.mu.Unlock()
	if sendStop {
		c.send([]byte{StopByte}, 0)
	}
}

// Tick fills one scheduling window. Start drives it; tests call it
// themselves.
func (c *ClockOut) Tick() {
	clock, ok := c.getClock()
	if !ok {
		return
	}
	at := c.now()

	c.mu.Lock()
	if clock.Playing != c.startSent {
		b := byte(StopByte)
		if clock.Playing {
			b = StartByte
		}
		c.mu.Unlock()
		// Only claim the device knows once the byte really reached a
		// port. A device unplugged when the transport started would
		// otherwise be left receiving clock forever with no Start to
		// act on.
		delivered := c.send([]byte{b}, 0)
		c.mu.Lock()
		if delivered {
			c.startSent = clock.Playing
		}
		// The beat grid moves with the transport, so schedule against
		// the new one as early as the pulses already queued allow.
		c.scheduledToMs = max(at, c.lastPulseMs+pulseEpsilon)
	}

	// Never schedule into the past: after a long stall, skip the missed
	// pulses instead of dumping a burst of back-dated ones.
	from := max(c.scheduledToMs, at)
	to := at + lookahead
	if to <= from {
		c.mu.Unlock()
		return
	}
	c.scheduledToMs = to
	pulses := PulsesInWindow(freeRunning(clock), from, to)
	c.mu.Unlock()

	for _, timeMs := range pulses {
		if c.send([]byte{ClockByte}, timeMs) {
			c.mu.Lock()
			c.lastPulseMs = timeMs
			c.mu.Unlock()
		}
	}
}
