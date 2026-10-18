package tempo

import (
	"math"
	"sync"
	"time"
)

// MidiClockSourceID is the tempo source that follows MIDI clock on an input
// port.
const MidiClockSourceID = "midi-clock"

// MidiClockSourceLabel is what the source picker calls it.
const MidiClockSourceLabel = "MIDI clock"

// pulsesPerBeat is the MIDI clock's pulses per quarter note, fixed by the MIDI
// spec. It is spelled out here rather than taken from internal/midi, which
// imports this package.
const pulsesPerBeat = 24

// fitPulses is how many arrivals the tempo is fitted over: two beats. A longer
// window reads the tempo more steadily and follows a change more slowly, and
// two beats is where a tempo automation lane still tracks without the BPM
// readout twitching.
const fitPulses = 2 * pulsesPerBeat

// minFitPulses is the shortest run that gets the source registered at all, so
// it appears within half a beat of the clock starting and does not appear at a
// tempo read off a pulse or two.
const minFitPulses = pulsesPerBeat / 2

// sixteenthsPerBeat is how many of the steps a song position counts make one
// beat. A song position is in sixteenth notes and a beat is a quarter note. It
// is spelled out here rather than taken from internal/midi, which imports this
// package.
const sixteenthsPerBeat = 4

// resumeFitPulses is the shortest run that gets a tempo once the source is
// registered. Two arrivals is one gap, which is noisy, but a clock coming back
// after a pause is often coming back at a new tempo, and a noisy reading of the
// tempo now beats a clean reading of one that has gone.
const resumeFitPulses = 2

// PulseGap is how long a silence has to be before the clock counts as not
// running. Half a second is over ten missed pulses at any tempo anyone plays
// at. The beat then freezes where it stopped rather than running on.
const PulseGap = 500 * time.Millisecond

// SourceGone is how long a silence has to be before the source is dropped
// altogether, for a cable pulled out or a DAW closed. The ranking then falls to
// whatever is next.
const SourceGone = 10 * time.Second

// minMidiClockBPM and maxMidiClockBPM bound a tempo read off the wire. A fit
// outside them came from a stutter in delivery rather than from a tempo, and
// publishing it would throw the lights off a beat.
const (
	minMidiClockBPM = 20
	maxMidiClockBPM = 400
)

// bpmEpsilon is how far the fitted tempo has to move to be worth publishing. It
// sits above the noise a two-beat fit leaves on a jittery Windows port, so a
// steady clock publishes on drift alone.
const bpmEpsilon = 0.1

// bpmJump is a tempo change big enough to be a tempo change rather than noise.
// One of those does not wait for the rate floor.
const bpmJump = 2

// minPublishGap holds the publish rate to ten a second, which is what the Link
// follower settles at. A transport change and a jump in tempo ignore it.
const minPublishGap = 100 * time.Millisecond

// snapshotOpts is how one publish differs from the ordinary per-pulse one.
type snapshotOpts struct {
	// anchor replaces the fitted reference instant, which is what a rewind
	// needs: a Start puts the beat back at the Start, not at the pulse before.
	anchor time.Time
	// force publishes whatever the last snapshot said, for a transport edge,
	// which is the one thing a light cue must not wait for.
	force bool
	// remembered lets the tempo this port was last measured at stand in for a
	// fit there is not enough of a run for, once the source has been dropped.
	// Only a transport byte may: re-registering a dropped source off a pulse or
	// two at a stale tempo is exactly what the fit floor is there to stop.
	remembered bool
}

// arrival is one clock byte: when it came in, and which pulse of the run it is.
//
// The number is not the position in the window. A pulse the driver dropped
// still counted, and numbering by position reads the gap it left as one slow
// pulse, which bends the fitted tempo and shifts the beat for good.
type arrival struct {
	at    time.Time
	pulse float64
}

// MidiClockFollower turns a run of MIDI clock arrivals into one tempo source.
//
// Tempo comes from a straight line fitted through the recent arrivals rather
// than from the last gap, because a Windows MIDI port delivers a pulse a
// millisecond or so either side of where it belongs and one gap carries two of
// those errors. The line also gives the reference instant, so the published
// beat grid is smoother than the wire it came off.
//
// Every method is safe to call from any goroutine.
type MidiClockFollower struct {
	registry *Registry

	// publishing orders the writes to the registry and is taken before mu.
	//
	// It is held across the registry call while mu is not. So two goroutines
	// cannot land their snapshots in the other order and leave the registry
	// disagreeing with this follower, and a tempo subscriber can still read
	// this follower back, which is what the registry promises one may do.
	publishing sync.Mutex

	mu sync.Mutex
	// enabled says the feature is on. A pulse arriving after Disable is
	// dropped rather than putting the source back.
	enabled bool
	// arrivals is the fit window, oldest first.
	arrivals []arrival
	// moved is when the clock last did anything, a pulse or a transport byte.
	// A Start with no pulses behind it still has to go quiet eventually.
	moved time.Time
	// pending is a transport that began and has not been published yet. A
	// Start arrives before there is a run long enough to read a tempo off,
	// so there is nothing to publish it on until the pulses behind it
	// have come in.
	pending TransportEdge
	// pulse is how many pulses the run has counted, dropped ones included.
	pulse float64
	// period is the last measured seconds per pulse, which is what says how
	// many pulses a gap swallowed. Zero until the first fit.
	period float64
	// lastBPM is the tempo this port was last measured at. It outlives the
	// source being dropped, so a clock that comes back registers on its first
	// transport byte rather than half a beat later, and a cue on that byte
	// fires when it happened.
	lastBPM float64
	// beat is the position the pulses have counted out so far, in beats.
	beat float64
	// transport is what Start, Continue and Stop last said. It begins true,
	// because plenty of gear sends clock and no transport at all.
	transport bool
	// running says pulses are arriving now. Tick clears it after a silence.
	running bool
	// published is the last clock published, and nil when the source is not
	// registered, so a reading that says nothing new is dropped.
	published *Clock
	// publishedAt is when that happened, for the rate floor.
	publishedAt time.Time
	// registered says the source is in the registry, so Disable only removes
	// one that is there.
	registered bool
}

// NewMidiClockFollower returns a follower that publishes into registry.
func NewMidiClockFollower(registry *Registry) *MidiClockFollower {
	return &MidiClockFollower{registry: registry}
}

// Enable lets pulses through, for the user having picked an input port.
func (f *MidiClockFollower) Enable() {
	f.publishing.Lock()
	defer f.publishing.Unlock()

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enabled {
		return
	}
	f.resetLocked()
	f.enabled = true
}

// Disable drops the source and refuses further pulses, for the feature being
// switched off or the port being changed.
func (f *MidiClockFollower) Disable() {
	f.publishing.Lock()
	defer f.publishing.Unlock()

	f.mu.Lock()
	registered := f.registered
	f.resetLocked()
	f.mu.Unlock()

	if registered {
		f.registry.Remove(MidiClockSourceID)
	}
}

// Enabled reports whether pulses are being followed.
func (f *MidiClockFollower) Enabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled
}

// resetLocked forgets everything about the run and switches the feature off.
// The caller holds f.mu and takes the source out of the registry itself.
func (f *MidiClockFollower) resetLocked() {
	f.enabled = false
	f.arrivals = nil
	f.moved = time.Time{}
	f.pending = ""
	f.pulse = 0
	f.period = 0
	f.lastBPM = 0
	f.beat = 0
	f.transport = true
	f.running = false
	f.published = nil
	f.publishedAt = time.Time{}
	f.registered = false
}

// Pulse folds one clock byte into the source, reporting whether it published.
func (f *MidiClockFollower) Pulse(at time.Time) bool {
	f.publishing.Lock()
	defer f.publishing.Unlock()

	f.mu.Lock()
	if !f.enabled {
		f.mu.Unlock()
		return false
	}
	f.countLocked(at)
	next, ok := f.snapshotLocked(at, snapshotOpts{})
	f.mu.Unlock()

	if !ok {
		return false
	}
	return f.publish(next, at)
}

// forgetRunLocked drops the run measured so far, for a silence or a Start. How
// long a pulse lasts goes with it: the clock may well come back at another
// tempo, and reading the first gap against the old one would count it as two
// pulses. The caller holds f.mu.
func (f *MidiClockFollower) forgetRunLocked() {
	f.arrivals = f.arrivals[:0]
	f.period = 0
}

// countLocked folds one pulse into the run: where it sits on the grid, and what
// the beat has reached. The caller holds f.mu.
func (f *MidiClockFollower) countLocked(at time.Time) {
	step := 1.0
	if n := len(f.arrivals); n > 0 {
		since := at.Sub(f.arrivals[n-1].at)
		switch {
		case since > PulseGap || since < 0:
			// The run this window was fitted to has ended. The tempo is
			// measured afresh, and the beat carries on from where it had
			// counted to rather than guessing what the silence swallowed.
			f.forgetRunLocked()
		case f.period > 0:
			step = math.Max(1, math.Round(since.Seconds()/f.period))
		}
	}
	f.pulse += step
	f.arrivals = append(f.arrivals, arrival{at: at, pulse: f.pulse})
	f.moved = at
	if len(f.arrivals) > fitPulses {
		f.arrivals = append(f.arrivals[:0], f.arrivals[len(f.arrivals)-fitPulses:]...)
	}
	f.running = true
	if f.transport {
		f.beat += step / pulsesPerBeat
	}
}

// SongPosition moves the beat to where in the song the sender says it is, in
// sixteenth notes, and reports whether it published.
//
// This is what lines the grid up with the sending app's bars. Without it the
// beat is a free-running count from the last Start, so bar one is wherever that
// happened to be, and dragging the playhead leaves the two disagreeing for the
// rest of the set.
//
// It is a seek and not a transport moving, so it fires no cue. A reposition
// while playing arrives as a Stop, this, and a Continue, and those two fire.
func (f *MidiClockFollower) SongPosition(sixteenths int, at time.Time) bool {
	f.publishing.Lock()
	defer f.publishing.Unlock()

	f.mu.Lock()
	if !f.enabled || sixteenths < 0 {
		f.mu.Unlock()
		return false
	}
	f.moved = at
	beat := float64(sixteenths) / sixteenthsPerBeat
	// The position is where the sender is now, and the beat is kept as of the
	// last pulse, because that is what the pulses go on adding to and what they
	// publish against. So a position that lands part way through a pulse is
	// carried back to it. A stopped transport has not moved since that pulse,
	// so there is nothing to carry.
	if bpm, ref, ok := f.fitLocked(resumeFitPulses); ok && f.transport && f.running {
		beat -= at.Sub(ref).Seconds() * bpm / 60
	}
	f.beat = beat
	// Published at once: the position jumped, and every consumer projects from
	// the last snapshot rather than being told again.
	next, ok := f.snapshotLocked(at, snapshotOpts{force: true})
	f.mu.Unlock()

	if !ok {
		return false
	}
	return f.publish(next, at)
}

// TransportStart runs a MIDI Start: the beat goes back to nothing and the clock
// plays.
func (f *MidiClockFollower) TransportStart(at time.Time) bool {
	return f.transportTo(at, TransportStart)
}

// TransportContinue runs a MIDI Continue: the clock plays on from where it
// stopped.
func (f *MidiClockFollower) TransportContinue(at time.Time) bool {
	return f.transportTo(at, TransportContinue)
}

// TransportStop runs a MIDI Stop: the beat holds where it had reached.
func (f *MidiClockFollower) TransportStop(at time.Time) bool {
	return f.transportTo(at, TransportStop)
}

// transportTo moves the transport and publishes the change straight away,
// because a transport edge is the one thing a light cue must not wait for.
//
// The beat is left where the pulses counted it to. Projecting it on to this
// instant would invent beats out of a stretch in which no pulse arrived, and a
// Stop can land most of a second after the last one.
func (f *MidiClockFollower) transportTo(at time.Time, edge TransportEdge) bool {
	f.publishing.Lock()
	defer f.publishing.Unlock()

	f.mu.Lock()
	if !f.enabled {
		f.mu.Unlock()
		return false
	}
	f.moved = at
	// A DAW sends Stop on a stop press whether or not it was already stopped,
	// and some send a second for return to zero. Only a move is reported, or
	// the cue runs twice. A Start is always a move: it goes back to the top
	// wherever the transport was.
	wasPlaying := f.published != nil && f.published.Playing
	moved := edge == TransportStart || edge.Began() != wasPlaying

	// A transport that begins places the beat at this instant rather than at the
	// pulse before it: a Start puts it back to nothing now, and a Continue picks
	// the held beat up now. A Stop is left on the fit, because the beat it holds
	// is the one the pulses counted to.
	anchor := time.Time{}
	if edge.Began() {
		anchor = at
	}
	if edge == TransportStart {
		// A Start is the transport going back to the top, and it is where an
		// operator who has just changed the tempo presses play. The run measured
		// before it describes neither the beat nor the tempo now, so it goes.
		// The beat is anchored on the Start itself rather than on the pulse
		// before it.
		f.beat = 0
		f.forgetRunLocked()
	}
	f.transport = edge.Began()
	// A stop with no source to stop is nothing at all, so only a
	// transport that began waits for somewhere to be published.
	f.pending = ""
	if f.transport && moved {
		f.pending = edge
	}
	if f.transport {
		// Most DAWs stop the clock with the transport, so a Start arrives with
		// the pulses already quiet. Waiting for the first pulse to call the
		// source playing would report the transport beginning twice: once for
		// the Start, and again a pulse later.
		f.running = true
	}
	// A stop that stops nothing says nothing: a DAW sends one on every
	// press, and some send a second for the return to zero.
	next, ok := f.snapshotLocked(at, snapshotOpts{anchor: anchor, force: moved, remembered: true})
	if ok && moved {
		// Said outright, because a Start and a Continue both end with the
		// transport running and nothing else in the snapshot tells them
		// apart.
		next.Transport = edge
	}
	f.mu.Unlock()

	if !ok {
		return false
	}
	return f.publish(next, at)
}

// Tick notices the clock going quiet: first that it has stopped, so the beat
// freezes rather than running on, and later that the device has gone, so the
// ranking can fall to another source. It reports whether anything changed.
func (f *MidiClockFollower) Tick(now time.Time) bool {
	f.publishing.Lock()
	defer f.publishing.Unlock()

	f.mu.Lock()
	if !f.enabled || f.moved.IsZero() {
		f.mu.Unlock()
		return false
	}
	since := now.Sub(f.moved)
	if since >= SourceGone {
		registered := f.registered
		// What the transport last said outlives the run. A DAW that went away
		// stopped is still stopped when its clock comes back, and it sends no
		// transport byte, because nothing about its transport changed. Gear
		// that never sends transport at all is unaffected.
		transport, bpm, beat := f.transport, f.lastBPM, f.beat
		f.resetLocked()
		// Only Disable switches the feature off. A device that comes back is
		// followed again without the user touching anything, at the tempo it
		// was last seen at and the beat it was stopped at, until a pulse or a
		// song position says otherwise. A Continue means play on from where it
		// stopped, and ten seconds of quiet does not change where that was.
		f.enabled, f.transport, f.lastBPM, f.beat = true, transport, bpm, beat
		f.mu.Unlock()

		if registered {
			f.registry.Remove(MidiClockSourceID)
		}
		return registered
	}
	if since < PulseGap || !f.running {
		f.mu.Unlock()
		return false
	}
	// Stopped. The window is left alone so the fit still has a tempo to publish
	// the held beat against; the next pulse after the gap clears it.
	f.running = false
	// A Start with no clock behind it is not waiting to be reported any
	// more: it is a transport that never began. Held over this, it would
	// fire when the clock came back, which could be seconds later.
	f.pending = ""
	next, ok := f.snapshotLocked(now, snapshotOpts{force: true})
	f.mu.Unlock()

	if !ok {
		return false
	}
	return f.publish(next, now)
}

// snapshotLocked is the clock to publish now, reporting false when there is
// nothing worth publishing. A non-zero anchor is the reference to use instead
// of the fitted one, which is what a rewind needs. The caller holds f.mu.
func (f *MidiClockFollower) snapshotLocked(at time.Time, opts snapshotOpts) (Clock, bool) {
	// Before the source has registered at all, wait for a proper window rather
	// than show a tempo read off a pulse or two. After that, take whatever the
	// run says, because a gap or a Start is often exactly where the tempo
	// changed and the one measured before it is then simply wrong.
	least := minFitPulses
	if f.published != nil {
		least = resumeFitPulses
	}
	bpm, ref, ok := f.fitLocked(least)
	switch {
	case ok:
		f.period = 60 / (bpm * pulsesPerBeat)
		f.lastBPM = bpm
	case f.published != nil:
		// A pulse or two back after a gap, with the source still registered.
		// The tempo it was measured at is the only one there is, and half a
		// beat later the fit replaces it.
		bpm, ref = f.published.BPM, at
	case opts.remembered && f.lastBPM > 0:
		// A transport byte on a port that has been measured before, after the
		// source was dropped for going quiet. Moving the source now is what
		// keeps a cue on that byte from landing half a beat late.
		bpm, ref = f.lastBPM, at
	default:
		return Clock{}, false
	}
	if !opts.anchor.IsZero() {
		ref = opts.anchor
	}
	next := Clock{
		SourceID:  MidiClockSourceID,
		BPM:       bpm,
		BeatAtRef: f.beat,
		RefTimeMs: AtMs(ref),
		Playing:   f.transport && f.running,
		Transport: f.pending,
	}
	if !opts.force && !f.worthPublishingLocked(next, at) {
		return Clock{}, false
	}
	return next, true
}

// publish writes a prepared snapshot to the registry and records it.
//
// The caller holds f.publishing and must not hold f.mu, so a tempo
// subscriber may read this follower back as the registry reports the
// change.
func (f *MidiClockFollower) publish(next Clock, at time.Time) bool {
	f.registry.Publish(next, MidiClockSourceLabel)
	f.mu.Lock()
	f.published, f.publishedAt, f.registered = &next, at, true
	// Reported, so it is no longer waiting for somewhere to be reported.
	if f.pending == next.Transport {
		f.pending = ""
	}
	f.mu.Unlock()
	return true
}

// worthPublishingLocked reports whether a reading says anything the last one
// did not. The caller holds f.mu.
func (f *MidiClockFollower) worthPublishingLocked(next Clock, at time.Time) bool {
	if f.published == nil {
		return true
	}
	if f.published.Playing != next.Playing {
		return true
	}
	moved := math.Abs(f.published.BPM - next.BPM)
	// A tempo that has moved this far is a change of tempo rather than the
	// noise the floor is there to absorb, so it does not wait for the floor.
	if moved >= bpmJump {
		return true
	}
	// Ten a second at most, so a clock costs a renderer no more than Link does.
	// A transport change comes through force and never gets here.
	if at.Sub(f.publishedAt) < minPublishGap {
		return false
	}
	if moved >= bpmEpsilon {
		return true
	}
	// A stopped clock holds its position, so there is nothing to drift.
	if !next.Playing {
		return false
	}
	return math.Abs(BeatsAt(*f.published, next.RefTimeMs)-next.BeatAtRef) > ReanchorBeats
}

// fitLocked is the tempo and the reference instant a straight line through the
// window gives, reporting false when there is less than least of a run to
// measure or the answer is not a tempo. The caller holds f.mu.
//
// The line is fitted by least squares over every arrival in the window, so the
// jitter on each averages out instead of landing wholly on the answer, which is
// what timing the last gap would do. Its value at the newest pulse is the
// reference, which is why the published grid is smoother than the wire.
//
// Time is fitted against each arrival's pulse number rather than its position
// in the window, so a pulse the driver dropped leaves a hole in the numbering
// instead of a slow beat in the answer.
func (f *MidiClockFollower) fitLocked(least int) (bpm float64, ref time.Time, ok bool) {
	n := len(f.arrivals)
	if n < least || n < 2 {
		return 0, time.Time{}, false
	}
	// Measured from the oldest arrival, so the sums stay small and exact.
	origin := f.arrivals[0]
	var meanPulse, meanTime float64
	for _, a := range f.arrivals {
		meanPulse += a.pulse - origin.pulse
		meanTime += a.at.Sub(origin.at).Seconds()
	}
	meanPulse /= float64(n)
	meanTime /= float64(n)

	var covariance, spread float64
	for _, a := range f.arrivals {
		p := a.pulse - origin.pulse - meanPulse
		covariance += p * (a.at.Sub(origin.at).Seconds() - meanTime)
		spread += p * p
	}
	// Every arrival on the same pulse number, which no run produces.
	if spread <= 0 {
		return 0, time.Time{}, false
	}
	// Seconds per pulse.
	period := covariance / spread
	if !(period > 0) || math.IsInf(period, 0) {
		return 0, time.Time{}, false
	}
	bpm = 60 / (period * pulsesPerBeat)
	if bpm < minMidiClockBPM || bpm > maxMidiClockBPM {
		return 0, time.Time{}, false
	}
	// The fitted line at the newest pulse, rather than the arrival itself.
	newest := meanTime + period*(f.arrivals[n-1].pulse-origin.pulse-meanPulse)
	return bpm, origin.at.Add(time.Duration(newest * float64(time.Second))), true
}
