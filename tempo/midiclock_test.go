package tempo

import (
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// pulsePeriod is how long one clock pulse lasts at a tempo.
func pulsePeriod(bpm float64) time.Duration {
	return time.Duration(float64(time.Minute) / bpm / pulsesPerBeat)
}

func clockFollower(t *testing.T) (*MidiClockFollower, *Registry, time.Time) {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	registry := NewRegistry()
	follower := NewMidiClockFollower(registry)
	follower.Enable()
	return follower, registry, start
}

// feed sends a run of evenly spaced pulses and returns when the last one
// arrived. The first pulse lands one period after from, the way a real run
// continues.
func feed(f *MidiClockFollower, from time.Time, bpm float64, pulses int) time.Time {
	period := pulsePeriod(bpm)
	at := from
	for range pulses {
		at = at.Add(period)
		f.Pulse(at)
	}
	return at
}

func TestAShortRunIsNotEnoughToReadATempo(t *testing.T) {
	follower, registry, start := clockFollower(t)
	feed(follower, start, 120, minFitPulses-1)

	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Errorf("a tempo was published from fewer than %d pulses", minFitPulses)
	}
}

func TestHalfABeatOfPulsesGivesTheTempo(t *testing.T) {
	follower, registry, start := clockFollower(t)
	feed(follower, start, 120, minFitPulses)

	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("the MIDI clock did not appear as a source")
	}
	if math.Abs(clock.BPM-120) > 0.001 {
		t.Errorf("bpm = %v, want 120", clock.BPM)
	}
	if !clock.Playing {
		t.Errorf("a running clock published as stopped")
	}
}

// The beat is what the pulses have counted, so a whole beat of them has to move
// it by exactly one. It is read at the instant the last pulse landed, because
// the published clock is a snapshot the consumer projects from.
func TestTwentyFourPulsesAdvanceTheBeatByOne(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat-1) > 1e-6 {
		t.Errorf("beat = %v after one beat of pulses, want 1", beat)
	}
}

func TestAnUnevenlyDeliveredClockStillReadsTheRightTempo(t *testing.T) {
	follower, registry, start := clockFollower(t)

	// A millisecond either side of where each pulse belongs, which is about
	// what a Windows MIDI port delivers.
	jitter := rand.New(rand.NewSource(7))
	period := pulsePeriod(128)
	for i := range fitPulses {
		at := start.Add(time.Duration(i+1) * period)
		follower.Pulse(at.Add(time.Duration(jitter.NormFloat64() * float64(time.Millisecond))))
	}

	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("the MIDI clock did not appear as a source")
	}
	// A straight line through two beats of arrivals leaves well under a tenth
	// of a BPM. Timing the last gap alone would be out by several.
	if math.Abs(clock.BPM-128) > 0.1 {
		t.Errorf("bpm = %v, want 128 within 0.1", clock.BPM)
	}
}

// Every consumer projects the beat from the last publish, so a steady clock
// must not wake the listeners once a pulse.
func TestASteadyClockPublishesAtMostTenTimesASecond(t *testing.T) {
	follower, registry, start := clockFollower(t)
	publishes := 0
	registry.Subscribe(func() { publishes++ })

	// Four seconds at 120 BPM is 192 pulses.
	feed(follower, start, 120, 4*2*pulsesPerBeat)

	if publishes > 41 {
		t.Errorf("published %d times in four seconds, want at most ten a second", publishes)
	}
	if publishes == 0 {
		t.Errorf("a running clock published nothing")
	}
	clock, _ := registry.ClockFor(MidiClockSourceID)
	if math.Abs(clock.BPM-120) > 0.001 {
		t.Errorf("bpm = %v, want 120", clock.BPM)
	}
}

func TestATempoChangeIsFollowed(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, 2*fitPulses)
	at = feed(follower, at, 140, 2*fitPulses)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if math.Abs(clock.BPM-140) > 0.001 {
		t.Errorf("bpm = %v after the tempo changed, want 140", clock.BPM)
	}
	_ = at
}

func TestStopHoldsTheBeatAndStartPutsItBack(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	at = at.Add(pulsePeriod(120))
	follower.TransportStop(at)
	stopped, _ := registry.ClockFor(MidiClockSourceID)
	if stopped.Playing {
		t.Errorf("the clock is playing after a MIDI Stop")
	}

	// Pulses that keep arriving while stopped must not move the beat.
	held := stopped.BeatAtRef
	at = feed(follower, at, 120, pulsesPerBeat)
	stillStopped, _ := registry.ClockFor(MidiClockSourceID)
	if math.Abs(stillStopped.BeatAtRef-held) > 1e-9 {
		t.Errorf("the beat moved from %v to %v while stopped", held, stillStopped.BeatAtRef)
	}

	follower.TransportStart(at)
	restarted, _ := registry.ClockFor(MidiClockSourceID)
	if !restarted.Playing {
		t.Errorf("the clock is stopped after a MIDI Start")
	}
	if restarted.BeatAtRef != 0 {
		t.Errorf("beat = %v after a MIDI Start, want 0", restarted.BeatAtRef)
	}
}

func TestContinuePlaysOnFromWhereItStopped(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	at = at.Add(pulsePeriod(120))
	follower.TransportStop(at)
	stopped, _ := registry.ClockFor(MidiClockSourceID)

	at = at.Add(2 * time.Second)
	follower.TransportContinue(at)
	resumed, _ := registry.ClockFor(MidiClockSourceID)
	if !resumed.Playing {
		t.Errorf("the clock is stopped after a MIDI Continue")
	}
	if math.Abs(resumed.BeatAtRef-stopped.BeatAtRef) > 1e-9 {
		t.Errorf("beat = %v after a Continue, want the %v it stopped at", resumed.BeatAtRef, stopped.BeatAtRef)
	}
}

// Gear that sends clock and no transport at all is common, so a bare run of
// pulses has to count as playing.
func TestAClockWithNoTransportMessageCountsAsPlaying(t *testing.T) {
	follower, registry, start := clockFollower(t)
	feed(follower, start, 120, pulsesPerBeat)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if !clock.Playing {
		t.Errorf("a clock that sent no Start published as stopped")
	}
}

func TestTheBeatFreezesWhenThePulsesStop(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	if follower.Tick(at.Add(PulseGap / 2)) {
		t.Errorf("a gap shorter than %v was taken for the clock stopping", PulseGap)
	}
	if !follower.Tick(at.Add(PulseGap + time.Millisecond)) {
		t.Errorf("a gap longer than %v was not taken for the clock stopping", PulseGap)
	}
	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("the source was dropped when it only went quiet")
	}
	if clock.Playing {
		t.Errorf("the clock is still playing %v after the last pulse", PulseGap)
	}
	// A stopped clock holds its position, so the beat must read the same a
	// minute later as it does now.
	held := BeatsAt(clock, AtMs(at))
	if later := BeatsAt(clock, AtMs(at.Add(time.Minute))); later != held {
		t.Errorf("the beat ran on from %v to %v while the clock was stopped", held, later)
	}
}

// A gap of a beat or two is a DAW being paused, not a device going away. The
// lights have to start again on the very next pulse rather than wait for a new
// tempo to be measured.
func TestAResumingClockPlaysAgainOnTheFirstPulse(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)
	follower.Tick(at.Add(PulseGap + time.Millisecond))

	feed(follower, at.Add(2*time.Second), 120, 1)
	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("the source went away while the clock was only paused")
	}
	if !clock.Playing {
		t.Errorf("the clock is stopped one pulse after it came back")
	}
	if math.Abs(clock.BPM-120) > 0.001 {
		t.Errorf("bpm = %v on the first pulse back, want the 120 it was measured at", clock.BPM)
	}
}

func TestTheSourceIsDroppedWhenTheDeviceGoesAway(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	follower.Tick(at.Add(PulseGap + time.Millisecond))
	follower.Tick(at.Add(SourceGone))
	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Errorf("the source is still registered %v after the last pulse", SourceGone)
	}
	// The ranking falls to whatever is next, which with nothing else connected
	// is the manual source.
	if registry.Selected() != ManualSourceID {
		t.Errorf("selected = %q after the clock went away, want %q", registry.Selected(), ManualSourceID)
	}
}

// A device that is unplugged and put back must be followed again without the
// user touching the settings.
func TestAClockThatComesBackIsFollowedAgain(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)
	follower.Tick(at.Add(SourceGone))

	feed(follower, at.Add(SourceGone), 140, pulsesPerBeat)
	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("a clock that came back did not register again")
	}
	if math.Abs(clock.BPM-140) > 0.001 {
		t.Errorf("bpm = %v, want the 140 it came back at", clock.BPM)
	}
}

// The tempo has to be measured afresh after a gap, or the silence reads as one
// very long pulse and the tempo collapses.
func TestAGapDoesNotDragTheTempoDown(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)
	at = feed(follower, at.Add(5*time.Second), 120, pulsesPerBeat)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if math.Abs(clock.BPM-120) > 0.001 {
		t.Errorf("bpm = %v after a five second gap, want 120", clock.BPM)
	}
	_ = at
}

func TestAnAbsurdTempoIsNotPublished(t *testing.T) {
	follower, registry, start := clockFollower(t)
	// A pulse every microsecond is 2.5 million BPM.
	at := start
	for range fitPulses {
		at = at.Add(time.Microsecond)
		follower.Pulse(at)
	}
	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Errorf("a tempo far outside %v to %v was published", minMidiClockBPM, maxMidiClockBPM)
	}
}

func TestPulsesAreIgnoredUntilTheSourceIsEnabled(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	registry := NewRegistry()
	follower := NewMidiClockFollower(registry)

	feed(follower, start, 120, fitPulses)
	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Errorf("a follower that was never enabled published a tempo")
	}
}

func TestDisableDropsTheSource(t *testing.T) {
	follower, registry, start := clockFollower(t)
	feed(follower, start, 120, pulsesPerBeat)

	follower.Disable()
	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Errorf("the source is still registered after Disable")
	}
	if follower.Enabled() {
		t.Errorf("the follower still reports itself enabled after Disable")
	}
}

// The daemon re-sends the settings on every reconnection, so enabling a
// follower that is already enabled must not throw away the run it has measured.
func TestEnablingTwiceKeepsTheRun(t *testing.T) {
	follower, registry, start := clockFollower(t)
	feed(follower, start, 120, pulsesPerBeat)
	before, _ := registry.ClockFor(MidiClockSourceID)

	follower.Enable()
	after, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("enabling twice dropped the source")
	}
	if after.BPM != before.BPM || after.BeatAtRef != before.BeatAtRef {
		t.Errorf("the run was reset by a second Enable")
	}
}

// Pulses arrive on the driver's goroutine while the staleness ticker runs on
// another and the user switches the source off from a third. Switching it off
// has to win: a source left in the registry with nothing updating it freezes
// the lights at whatever it last said.
func TestSwitchingOffWhileThePulsesFlowLeavesNothingBehind(t *testing.T) {
	for range 50 {
		follower, registry, start := clockFollower(t)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			feed(follower, start, 120, 4*fitPulses)
		}()
		go func() {
			defer wg.Done()
			for i := range 4 * fitPulses {
				follower.Tick(start.Add(time.Duration(i) * time.Millisecond))
			}
		}()
		follower.Disable()
		wg.Wait()

		follower.Disable()
		if _, ok := registry.ClockFor(MidiClockSourceID); ok {
			t.Fatalf("the source was left in the registry after being switched off")
		}
	}
}

// An operator who stops, changes the tempo and presses play has to get the new
// tempo, not the one measured before the stop. Gear that keeps the clock
// running while stopped gives no gap to notice, so the Start has to be what
// clears the run.
func TestPressingPlayAfterATempoChangeFollowsTheNewTempo(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 174, 2*fitPulses)

	at = at.Add(pulsePeriod(174))
	follower.TransportStop(at)
	at = at.Add(time.Second)
	follower.TransportStart(at)

	// Two pulses is one gap, which is all there is to measure at this point.
	feed(follower, at, 90, 2)
	clock, _ := registry.ClockFor(MidiClockSourceID)
	if math.Abs(clock.BPM-90) > 1 {
		t.Errorf("bpm = %v two pulses after pressing play at 90, want 90", clock.BPM)
	}
}

// A Stop lands between two pulses, so the beat it freezes at is the one the
// pulses counted. Projecting it forward would invent beats out of a stretch in
// which none arrived, and a Stop can land most of a second after the last one.
func TestStopFreezesTheBeatThePulsesCounted(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	// Just inside the gap that would have counted as the clock stopping.
	follower.TransportStop(at.Add(PulseGap - time.Millisecond))

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if math.Abs(clock.BeatAtRef-1) > 1.0/pulsesPerBeat {
		t.Errorf("beat = %v after a late Stop, want the 1 the pulses counted", clock.BeatAtRef)
	}
}

// A Start is the transport beginning again at that instant, not at whenever the
// last pulse happened to land.
func TestStartPutsTheBeatBackAtTheStartItself(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	at = at.Add(pulsePeriod(120) / 2)
	follower.TransportStart(at)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat) > 1e-9 {
		t.Errorf("the grid reads %v beats at the Start instant, want 0", beat)
	}
}

// A DAW that went away stopped is still stopped when its clock comes back, and
// it sends no transport byte, because nothing about its transport changed.
func TestAClockThatWentAwayStoppedComesBackStopped(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, pulsesPerBeat)

	at = at.Add(pulsePeriod(120))
	follower.TransportStop(at)
	follower.Tick(at.Add(SourceGone))

	feed(follower, at.Add(SourceGone), 120, pulsesPerBeat)
	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("the clock did not register again when it came back")
	}
	if clock.Playing {
		t.Errorf("a clock that was stopped before it went away came back playing")
	}
}

// Most DAWs stop the clock with the transport, so a DAW left stopped for ten
// seconds is dropped as though it had gone away. Pressing play then has to move
// the source at once, on the tempo it was last measured at, or the cue on that
// press lands half a beat late.
func TestAStartAfterTheSourceWasDroppedRegistersAtOnce(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 128, fitPulses)
	follower.Tick(at.Add(SourceGone))
	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Fatalf("the source was not dropped after %v of silence", SourceGone)
	}

	at = at.Add(SourceGone)
	follower.TransportStart(at)

	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("pressing play did not put the source back")
	}
	if !clock.Playing {
		t.Error("a start left the source stopped")
	}
	if !clock.Playing {
		t.Errorf("the source is not playing after a Start")
	}
	if math.Abs(clock.BPM-128) > 0.001 {
		t.Errorf("bpm = %v, want the 128 it was last measured at", clock.BPM)
	}
}

// The tempo a dropped port was last measured at is there for a transport byte
// to move the source on. A bare pulse may not use it: putting a dropped source
// back off one pulse at a stale tempo is what the fit floor exists to stop.
func TestABarePulseDoesNotPutADroppedSourceBackAtAStaleTempo(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 128, fitPulses)
	follower.Tick(at.Add(SourceGone))

	at = feed(follower, at.Add(SourceGone), 90, minFitPulses-1)
	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Fatalf("a dropped source was put back before a tempo had been measured")
	}

	// Half a beat of clock, and it registers at the tempo it is really running
	// at rather than the one it used to.
	feed(follower, at, 90, 1)
	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("the source did not register after half a beat of clock")
	}
	if math.Abs(clock.BPM-90) > 0.5 {
		t.Errorf("bpm = %v, want the 90 it came back at", clock.BPM)
	}
}

// Switching the feature off and on is a different port as far as anyone knows,
// so the tempo it remembered goes with it.
func TestSwitchingTheSourceOffForgetsTheTempoItRemembered(t *testing.T) {
	follower, registry, start := clockFollower(t)
	feed(follower, start, 128, fitPulses)

	follower.Disable()
	follower.Enable()
	follower.TransportStart(start.Add(time.Minute))

	if _, ok := registry.ClockFor(MidiClockSourceID); ok {
		t.Errorf("a Start on a source with no tempo behind it registered one")
	}
}

// A song position is in sixteenth notes, so four of them make one beat. This is
// what lines the grid up with the sending app's bars.
func TestASongPositionSetsTheBeat(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, fitPulses)

	// Bar five of four four, counting from one, is beat sixteen.
	follower.SongPosition(16*sixteenthsPerBeat, at)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat-16) > 1e-9 {
		t.Errorf("beat = %v after a song position of bar five, want 16", beat)
	}
}

// Dragging the playhead while playing arrives as a Stop, a song position and a
// Continue. The beat has to follow the drag rather than carry on from where it
// froze, or the grid is wrong for the rest of the set.
func TestARepositionMovesTheBeatToWhereItWasDraggedTo(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, 2*fitPulses)

	at = at.Add(time.Millisecond)
	follower.TransportStop(at)
	at = at.Add(time.Millisecond)
	follower.SongPosition(64*sixteenthsPerBeat, at)
	at = at.Add(time.Millisecond)
	follower.TransportContinue(at)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if !clock.Playing {
		t.Fatalf("the clock is stopped after a reposition and a Continue")
	}
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat-64) > 1e-9 {
		t.Errorf("beat = %v after being dragged to beat 64, want 64", beat)
	}

	// And it counts on from there, rather than from where it had got to. The
	// three milliseconds the transport bytes took sit inside the fit window, so
	// the line through it is out by a fraction of a pulse.
	at = feed(follower, at, 120, pulsesPerBeat)
	clock, _ = registry.ClockFor(MidiClockSourceID)
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat-65) > ReanchorBeats {
		t.Errorf("beat = %v a beat after the reposition, want 65", beat)
	}
}

// A seek is not the transport moving. The Stop and the Continue around it fire
// the cues; the position itself must not fire a third.
func TestASongPositionIsNotATransportMove(t *testing.T) {
	follower, registry, start := clockFollower(t)
	// Without this the source never drives the lights.
	registry.SetPriority([]string{MidiClockSourceID})
	at := feed(follower, start, 120, fitPulses)

	// The stop stops it and the continue starts it again; the position
	// between them leaves the transport where it found it.
	at = at.Add(time.Millisecond)
	follower.TransportStop(at)
	if clock, _ := registry.ClockFor(MidiClockSourceID); clock.Playing {
		t.Fatal("a stop left the clock playing")
	}
	at = at.Add(time.Millisecond)
	follower.SongPosition(32*sixteenthsPerBeat, at)
	if clock, _ := registry.ClockFor(MidiClockSourceID); clock.Playing {
		t.Error("a song position started a stopped transport")
	}
	at = at.Add(time.Millisecond)
	follower.TransportContinue(at)
	if clock, _ := registry.ClockFor(MidiClockSourceID); !clock.Playing {
		t.Error("a continue left the clock stopped")
	}
}

// Reason sends a bare position with the transport still running when Send
// Stop/Continue when Repositioning is off, which is the setup the README
// recommends. The beat has to land where it was dragged to, with no transport
// bytes around it to put it right.
func TestASeekWhilePlayingMovesTheBeat(t *testing.T) {
	follower, registry, start := clockFollower(t)
	registry.SetPriority([]string{MidiClockSourceID})
	last := feed(follower, start, 120, 2*fitPulses)

	// Part way between two pulses, which is where a drag really lands.
	at := last.Add(pulsePeriod(120) * 7 / 10)
	follower.SongPosition(48*sixteenthsPerBeat, at)

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if !clock.Playing {
		t.Fatalf("a seek stopped the transport")
	}
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat-48) > 1e-6 {
		t.Errorf("beat = %v at the instant of the seek, want 48", beat)
	}

	// The clock carries on at its own cadence, which a drag does not disturb.
	// The pulses add to the same beat the seek set, so the grid does not step:
	// a beat later it still puts beat 48 at the instant of the seek.
	feed(follower, last, 120, pulsesPerBeat)
	clock, _ = registry.ClockFor(MidiClockSourceID)
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat-48) > 0.01 {
		t.Errorf("the grid stepped: %v at the instant of the seek, want 48", beat)
	}
	if !clock.Playing {
		t.Error("a seek while playing stopped the transport")
	}
}

// The rate floor is checked before the drift, so a seek arriving just after
// another publish would be dropped on its merits. A drag lands exactly there:
// the Stop in front of it published a moment before.
func TestASeekRightAfterAStopIsStillPublished(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, 2*fitPulses)

	at = at.Add(time.Millisecond)
	follower.TransportStop(at)
	at = at.Add(time.Millisecond)
	if !follower.SongPosition(96*sixteenthsPerBeat, at) {
		t.Fatalf("a seek inside the publish rate floor was dropped")
	}

	clock, _ := registry.ClockFor(MidiClockSourceID)
	if math.Abs(clock.BeatAtRef-96) > 0.01 {
		t.Errorf("beat = %v, want the 96 it was dragged to", clock.BeatAtRef)
	}
}

// A Continue means play on from where it stopped, and ten seconds of quiet does
// not change where that was.
func TestAContinueAfterALongSilenceResumesWhereItStopped(t *testing.T) {
	follower, registry, start := clockFollower(t)
	at := feed(follower, start, 120, 8*pulsesPerBeat)
	at = at.Add(time.Millisecond)
	follower.TransportStop(at)
	stopped, _ := registry.ClockFor(MidiClockSourceID)

	follower.Tick(at.Add(SourceGone))
	at = at.Add(SourceGone + time.Second)
	follower.TransportContinue(at)

	clock, ok := registry.ClockFor(MidiClockSourceID)
	if !ok {
		t.Fatalf("a Continue after the source was dropped put nothing back")
	}
	if math.Abs(clock.BeatAtRef-stopped.BeatAtRef) > 1e-9 {
		t.Errorf("resumed at beat %v, want the %v it stopped at", clock.BeatAtRef, stopped.BeatAtRef)
	}
}

// A Windows MIDI port drops a message when the routes have been busy. The gap
// it leaves is one missing pulse, not one slow one: read as a slow one it bends
// the fitted tempo and shifts the beat for good.
func TestADroppedPulseDoesNotBendTheTempoOrTheBeat(t *testing.T) {
	follower, registry, start := clockFollower(t)
	period := pulsePeriod(120)

	at := start
	sent := 0
	for i := range 3 * fitPulses {
		at = at.Add(period)
		// Well past the half-beat the first fit needs, so the follower knows
		// how long a pulse lasts before the hole appears.
		if i == 2*fitPulses {
			continue
		}
		follower.Pulse(at)
		sent++

		clock, ok := registry.ClockFor(MidiClockSourceID)
		if ok && math.Abs(clock.BPM-120) > 0.5 {
			t.Fatalf("bpm = %v around a dropped pulse, want 120", clock.BPM)
		}
	}

	// The beat counts pulses that were sent, not pulses that arrived.
	clock, _ := registry.ClockFor(MidiClockSourceID)
	want := float64(3*fitPulses) / pulsesPerBeat
	if beat := BeatsAt(clock, AtMs(at)); math.Abs(beat-want) > 2.0/pulsesPerBeat {
		t.Errorf("beat = %v after %d of %d pulses arrived, want %v", beat, sent, 3*fitPulses, want)
	}
}

// The registry promises a subscriber may read it back, and a subscriber that
// reaches this follower has to work too. Holding the follower's lock across the
// registry call would wedge the whole daemon here.
func TestASubscriberMayReadTheFollowerBack(t *testing.T) {
	follower, registry, start := clockFollower(t)
	registry.Subscribe(func() { follower.Enabled() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		feed(follower, start, 120, fitPulses)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a subscriber that read the follower back deadlocked the publish")
	}
}

// The published grid has to be smoother than the wire, or every light cue lands
// wherever the last pulse happened to.
func TestThePublishedGridIsSmootherThanTheArrivals(t *testing.T) {
	follower, registry, start := clockFollower(t)

	jitter := rand.New(rand.NewSource(11))
	period := pulsePeriod(120)
	var worst float64
	for i := range 4 * fitPulses {
		at := start.Add(time.Duration(i+1) * period)
		follower.Pulse(at.Add(time.Duration(jitter.NormFloat64() * float64(time.Millisecond))))
		clock, ok := registry.ClockFor(MidiClockSourceID)
		if !ok {
			continue
		}
		// Where the published grid says the beat is, against where the pulses
		// have actually counted to.
		want := float64(i+1) / pulsesPerBeat
		worst = math.Max(worst, math.Abs(BeatsAt(clock, AtMs(at))-want))
	}
	// A millisecond of jitter is 0.002 beats at 120 BPM. Anchoring on the raw
	// arrival would leave that on every publish.
	if worst > 0.01 {
		t.Errorf("the published grid was out by %v beats, want under 0.01", worst)
	}
}
