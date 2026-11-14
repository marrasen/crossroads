package engine

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/geom"
)

// chase puts a chase of steps, by pad, on r's Board, as id, and plays
// the show with it.
func (r *boardRig) chase(id int, steps ...int) {
	m := r.s.Module(r.board)
	m.Chases = slices.DeleteFunc(m.Chases, func(c show.Chase) bool { return c.ID == id })
	m.Chases = append(m.Chases, show.Chase{ID: id, Name: "Run " + string(rune('A'+id-1)), Steps: steps})
	r.e.Set(r.s)
}

// board is what r's Board shows after a tick.
func (r *boardRig) shows() BoardState { return r.e.Tick().Boards[r.board] }

// TestMakingAChaseTakesThePadsInOrderAndHandsItOver: New chase, then
// pads with scenes, are its steps, the pads' scenes left as they were;
// an empty pad is no step, and Record waits not. Done hands the chase
// over as Chase 1; given up, or done with no steps, nothing is.
func TestMakingAChaseTakesThePadsInOrderAndHandsItOver(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.scene(3, map[int]byte{2: 200})
	r.e.PressRecord(r.board)
	r.e.NewChase(r.board)
	for _, pad := range []int{3, 0, 7, 3} {
		r.e.PressPad(r.board, pad)
	}
	r.e.PressRecord(r.board)
	st := r.shows()
	if !st.Making || !slices.Equal(st.Steps, []int{3, 0, 3}) || st.Record || len(st.On) != 0 {
		t.Fatalf("making a chase, the board shows %+v", st)
	}
	r.e.DoneChase(r.board)
	got := r.e.Made()
	if len(got) != 1 || got[0].Module != r.board || got[0].Chase == nil {
		t.Fatalf("done, the board made %+v", got)
	}
	if c := *got[0].Chase; c.ID != 1 || c.Name != "Chase 1" || !slices.Equal(c.Steps, []int{3, 0, 3}) {
		t.Errorf("the chase made is %+v", c)
	}
	if st := r.shows(); st.Making || len(st.Steps) != 0 {
		t.Errorf("done, the board shows %+v", st)
	}
	// Until the show holds it, the board has it: it runs, and the next
	// one made is Chase 2.
	r.e.PressChase(r.board, 1)
	if st := r.shows(); st.Chase != 1 || st.Step != 0 {
		t.Errorf("the chase not yet in the show runs %d at %d", st.Chase, st.Step)
	}
	r.e.NewChase(r.board)
	r.e.PressPad(r.board, 0)
	r.e.DoneChase(r.board)
	if got := r.e.Made(); len(got) != 1 || got[0].Chase.Name != "Chase 2" {
		t.Errorf("the next chase made is %+v", got)
	}
	r.e.NewChase(r.board)
	r.e.PressPad(r.board, 0)
	r.e.GiveUpChase(r.board)
	r.e.NewChase(r.board)
	r.e.DoneChase(r.board)
	if got, st := r.e.Made(), r.shows(); len(got) != 0 || st.Making {
		t.Errorf("given up, the board made %+v and shows %+v", got, st)
	}
}

// TestAChaseMadeRunsOnThroughAShowSetWithoutIt, and goes from the
// board's keeping once the show holds it.
func TestAChaseMadeRunsOnThroughAShowSetWithoutIt(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.e.NewChase(r.board)
	r.e.PressPad(r.board, 0)
	r.e.DoneChase(r.board)
	r.e.PressChase(r.board, 1)
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Boards[r.board].Chase != 1 || live.Stage[1] != 100 {
		t.Fatalf("a show set without the chase, it runs %d and green plays %d", live.Boards[r.board].Chase, live.Stage[1])
	}
	got := r.e.Made()
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Boards[r.board].Chase != 1 {
		t.Fatal("handed over, a show set without the chase stopped it")
	}
	r.s.Module(r.board).Chases = append(r.s.Module(r.board).Chases, *got[0].Chase)
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Boards[r.board].Chase != 1 || len(r.e.boards[r.board].chases) != 0 {
		t.Errorf("held by the show, the chase runs %d and the board keeps %d", live.Boards[r.board].Chase, len(r.e.boards[r.board].chases))
	}
}

// TestAChaseStepsAtItsSpeedAndGoesRound: at 120 steps a minute it
// moves on every half second, from its first step, at once, round to
// it again after the last.
func TestAChaseStepsAtItsSpeedAndGoesRound(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 10})
	r.scene(1, map[int]byte{2: 20})
	r.scene(2, map[int]byte{2: 30})
	r.chase(1, 0, 1, 2)
	step := r.clocked()
	r.e.PressChase(r.board, 1)
	if live := r.e.Tick(); live.Stage[1] != 10 || live.Boards[r.board].Step != 0 {
		t.Fatalf("started, green plays %d at step %d", live.Stage[1], live.Boards[r.board].Step)
	}
	for i, want := range []byte{10, 20, 20, 30, 30, 10, 10, 20} {
		live := step()
		if live.Stage[1] != want {
			t.Fatalf("at %v s green plays %d, want %d", float64(i+1)/4, live.Stage[1], want)
		}
	}
	r.e.PressChase(r.board, 1)
	if live := step(); live.Stage[1] != 0 || live.Boards[r.board].Chase != 0 || live.Boards[r.board].Last != 1 {
		t.Errorf("stopped, green plays %d and the board shows %+v", live.Stage[1], live.Boards[r.board])
	}
}

// TestAChaseFadesEachKindOfChannelAsItShould: from a step to the next,
// over a fade of none, half and the whole step. Green, which both set,
// crosses from one to the other; white, only the first sets, fades out;
// UV, only the next sets, fades in; and the strobe and the motor, which
// the grand master does not dim, take the next step's value, or are let
// go, at once.
func TestAChaseFadesEachKindOfChannelAsItShould(t *testing.T) {
	type look struct{ green, white, uv, strobe, motor byte }
	for _, c := range []struct {
		fade float64
		// at are what plays as the step moves on, a quarter and half a
		// step on.
		at [3]look
	}{
		{0, [3]look{{200, 0, 200, 150, 0}, {200, 0, 200, 150, 0}, {200, 0, 200, 150, 0}}},
		{0.5, [3]look{{100, 200, 0, 150, 0}, {150, 100, 100, 150, 0}, {200, 0, 200, 150, 0}}},
		{1, [3]look{{100, 200, 0, 150, 0}, {125, 150, 50, 150, 0}, {150, 100, 100, 150, 0}}},
	} {
		r := newBoardRig(t)
		r.s.Module(r.board).Fade, r.s.Module(r.board).Speed = c.fade, 60
		r.scene(0, map[int]byte{2: 100, 4: 200, 7: 50, 8: 90})
		r.scene(1, map[int]byte{2: 200, 6: 200, 7: 150})
		r.chase(1, 0, 1)
		step := r.clocked()
		r.e.PressChase(r.board, 1)
		if u := r.e.Tick().Stage; u[1] != 100 || u[3] != 200 || u[5] != 0 || u[6] != 50 || u[7] != 90 {
			t.Fatalf("fade %v: started, the first step plays %v", c.fade, u[:8])
		}
		// A second a step: the fourth quarter moves it on.
		for range 3 {
			step()
		}
		for i, want := range c.at {
			u := step().Stage
			if got := (look{u[1], u[3], u[5], u[6], u[7]}); got != want {
				t.Errorf("fade %v, %v of a step on: green, white, UV, strobe and motor play %v, want %v", c.fade, float64(i)/4, got, want)
			}
		}
	}
}

// TestAChaseDoesNotDipBetweenTwoStepsOfOneValue, though it fades over
// the whole step.
func TestAChaseDoesNotDipBetweenTwoStepsOfOneValue(t *testing.T) {
	r := newBoardRig(t)
	r.s.Module(r.board).Fade, r.s.Module(r.board).Speed = 1, 60
	r.scene(0, map[int]byte{2: 200, 4: 10})
	r.scene(1, map[int]byte{2: 200, 4: 90})
	r.chase(1, 0, 1)
	step := r.clocked()
	r.e.PressChase(r.board, 1)
	for i := range 12 {
		if live := step(); live.Stage[1] != 200 {
			t.Fatalf("at %v s green plays %d", float64(i+1)/4, live.Stage[1])
		}
	}
}

// TestALongPauseMovesAChaseOnOneStep: ten seconds between two ticks,
// as of a laptop asleep, at ten steps a second, move it on one step,
// not a hundred.
func TestALongPauseMovesAChaseOnOneStep(t *testing.T) {
	r := newBoardRig(t)
	r.s.Module(r.board).Speed = show.MaxSpeed
	for pad := range 4 {
		r.scene(pad, map[int]byte{2: byte(10 * (pad + 1))})
	}
	r.chase(1, 0, 1, 2, 3)
	now := time.Unix(1000, 0)
	r.e.Now = func() time.Time { return now }
	r.e.PressChase(r.board, 1)
	if st := r.shows(); st.Step != 0 {
		t.Fatalf("started at step %d", st.Step)
	}
	now = now.Add(10 * time.Second)
	if st := r.shows(); st.Step != 1 {
		t.Errorf("after ten seconds asleep the chase is at step %d, want 1", st.Step)
	}
}

// TestOneChaseRunsAtATime: another pressed takes the place of the one
// that runs; the one that runs pressed stops; and the chase last run
// starts and stops from one button.
func TestOneChaseRunsAtATime(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.scene(1, map[int]byte{4: 100})
	r.chase(1, 0)
	r.chase(2, 1)
	r.e.PressChase(r.board, 1)
	r.e.PressChase(r.board, 2)
	live := r.e.Tick()
	if st := live.Boards[r.board]; st.Chase != 2 || st.Last != 2 || live.Stage[1] != 0 || live.Stage[3] != 100 {
		t.Fatalf("the second pressed, the board shows %+v, green %d and white %d", st, live.Stage[1], live.Stage[3])
	}
	r.e.PressChase(r.board, 2)
	if st := r.shows(); st.Chase != 0 || st.Last != 2 {
		t.Errorf("pressed again, the board shows %+v", st)
	}
	r.e.PressLastChase(r.board)
	if st := r.shows(); st.Chase != 2 {
		t.Errorf("the last run started again runs %d", st.Chase)
	}
	r.e.PressLastChase(r.board)
	if st := r.shows(); st.Chase != 0 {
		t.Errorf("the last run stopped runs %d", st.Chase)
	}
	r.e.PressChase(r.board, 9)
	if st := r.shows(); st.Chase != 0 || st.Last != 2 {
		t.Errorf("a chase the board has not pressed, it shows %+v", st)
	}
}

// TestTapsSetTheSpeedAndPutAStepOnTheTap: a tap says where a step
// falls. Just after the chase took a step by itself, it takes no other;
// two taps set the speed, played at once, and the second, a step's
// length after the first by that speed, takes a step. The speed is
// handed over once, when the tapping has stopped.
func TestTapsSetTheSpeedAndPutAStepOnTheTap(t *testing.T) {
	r := newBoardRig(t)
	r.s.Module(r.board).Speed = 30
	for pad := range 3 {
		r.scene(pad, map[int]byte{2: byte(10 * (pad + 1))})
	}
	r.chase(1, 0, 1, 2)
	now := time.Unix(1000, 0)
	r.e.Now = func() time.Time { return now }
	r.e.Tick()
	r.e.PressChase(r.board, 1)
	run := func(d time.Duration) BoardState {
		st := r.shows()
		for ; d > 0; d -= 25 * time.Millisecond {
			now = now.Add(25 * time.Millisecond)
			st = r.shows()
		}
		return st
	}
	run(100 * time.Millisecond)
	r.e.TapChase(r.board)
	if st := run(0); st.Step != 0 || st.Speed != 30 {
		t.Fatalf("a first tap, just after the chase began, leaves it at step %d and %v a minute", st.Step, st.Speed)
	}
	run(500 * time.Millisecond)
	r.e.TapChase(r.board)
	if st := run(0); st.Step != 1 || st.Speed != 120 {
		t.Fatalf("a second tap half a second on leaves it at step %d and %v a minute", st.Step, st.Speed)
	}
	// In time with the chase, which steps by itself about now: the tap
	// takes the step, or leaves the one just taken, and never two.
	run(500 * time.Millisecond)
	r.e.TapChase(r.board)
	if st := run(0); st.Step != 2 {
		t.Fatalf("a tap in time with the chase leaves it at step %d", st.Step)
	}
	// A show set meanwhile leaves the speed tapped playing.
	r.e.Set(r.s)
	if st := run(250 * time.Millisecond); st.Step != 2 || st.Speed != 120 {
		t.Errorf("a quarter of a second on, the chase is at step %d and %v a minute", st.Step, st.Speed)
	}
	if st := run(300 * time.Millisecond); st.Step != 0 {
		t.Errorf("a little over half a second on, the chase is at step %d, not round to the first", st.Step)
	}
	if got := r.e.Made(); len(got) != 0 {
		t.Fatalf("while the tapping may go on, the engine hands over %+v", got)
	}
	run(3 * time.Second)
	got := r.e.Made()
	if len(got) != 1 || got[0].Module != r.board || got[0].Chase != nil || got[0].Speed != 120 {
		t.Fatalf("taps half a second apart made %+v", got)
	}
	r.s.Module(r.board).Speed = 120
	r.e.Set(r.s)
	if r.e.boards[r.board].speed != 0 {
		t.Error("the board still keeps the speed the show holds")
	}
	// Taps too fast set the fastest speed there is.
	run(time.Second)
	r.e.TapChase(r.board)
	now = now.Add(10 * time.Millisecond)
	r.e.TapChase(r.board)
	run(4 * time.Second)
	if got := r.e.Made(); len(got) != 1 || got[0].Speed != show.MaxSpeed {
		t.Errorf("taps too fast made %+v", got)
	}
	// The speed's fader moved has the last word over one tapped.
	r.e.TapChase(r.board)
	now = now.Add(time.Second)
	r.e.TapChase(r.board)
	if st := run(0); st.Speed != 60 {
		t.Fatalf("taps a second apart play %v a minute", st.Speed)
	}
	r.s.Module(r.board).Speed = 200
	r.e.Set(r.s)
	if st := run(4 * time.Second); st.Speed != 200 {
		t.Errorf("the fader moved to 200, the chase plays %v a minute", st.Speed)
	}
	if got := r.e.Made(); len(got) != 0 {
		t.Errorf("the fader moved, the engine still hands over %+v", got)
	}
}

// TestAChaseFollowsTheBeatOfItsClock: with a tempo of 120 wired into
// the board and Follow the beat on, it steps each half second, whatever
// its speed; with the clock taken out, it runs at its own speed.
func TestAChaseFollowsTheBeatOfItsClock(t *testing.T) {
	r := newBoardRig(t)
	tempo := show.Module{ID: r.s.NewID("m"), Kind: show.Tempo, Name: "Tempo", BPM: 120, Manual: true, At: geom.Pt(0, 0)}
	r.s.Modules = append(r.s.Modules, tempo)
	wire := show.Wire{ID: r.s.NewID("w"), From: show.End{Module: tempo.ID, Port: "out"}, To: show.End{Module: r.board, Port: "clock"}}
	r.s.Wires = append(r.s.Wires, wire)
	m := r.s.Module(r.board)
	m.FollowBeat, m.Speed = true, show.MaxSpeed
	for pad := range 4 {
		r.scene(pad, map[int]byte{2: byte(10 * (pad + 1))})
	}
	r.chase(1, 0, 1, 2, 3)
	step := r.clocked()
	r.e.PressChase(r.board, 1)
	at := make([]int, 0, 8)
	for range 8 {
		st := step().Boards[r.board]
		if !st.Following || st.Clock.BPM != 120 {
			t.Fatalf("following, the board shows %+v", st)
		}
		at = append(at, st.Step)
	}
	if !slices.Equal(at, []int{0, 1, 1, 2, 2, 3, 3, 0}) {
		t.Errorf("on a beat each half second the chase stepped %v", at)
	}
	r.s.Wires = slices.DeleteFunc(r.s.Wires, func(w show.Wire) bool { return w.ID == wire.ID })
	r.e.Set(r.s)
	was := r.shows().Step
	st := step().Boards[r.board]
	if st.Following || st.Step == was {
		t.Errorf("with no clock, at its own speed, the chase stayed at %d, following %v", st.Step, st.Following)
	}
}

// TestAChaseSkipsTheStepsWithNoScene, and one with none plays nothing.
func TestAChaseSkipsTheStepsWithNoScene(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 10})
	r.scene(1, map[int]byte{2: 20})
	r.chase(1, 5, 0, 9, 1, 12)
	step := r.clocked()
	r.e.PressChase(r.board, 1)
	at := make([]int, 0, 6)
	for range 6 {
		at = append(at, step().Boards[r.board].Step)
	}
	if !slices.Equal(at, []int{1, 3, 3, 1, 1, 3}) {
		t.Errorf("stepping over the empty pads, the chase was at %v", at)
	}
	r.chase(2, 5, 9)
	r.e.PressChase(r.board, 2)
	live := step()
	if st := live.Boards[r.board]; st.Chase != 2 || st.Step != -1 || live.Stage[1] != 0 {
		t.Errorf("a chase of empty pads shows %+v, green %d", st, live.Stage[1])
	}
}

// TestAChaseMergesWithTheScenesAndTheFadersWin: a scene on and the
// chase play the higher of the two; a fader held has its channel.
func TestAChaseMergesWithTheScenesAndTheFadersWin(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.scene(1, map[int]byte{2: 200})
	r.scene(3, map[int]byte{2: 150, 4: 40})
	r.chase(1, 0, 1)
	step := r.clocked()
	r.e.PressPad(r.board, 3)
	r.e.PressChase(r.board, 1)
	for i, want := range []byte{150, 200, 200, 150, 150} {
		live := step()
		if live.Stage[1] != want || live.Stage[3] != 40 || live.Owners[2] != r.board {
			t.Fatalf("at %v s green plays %d, want %d, and white %d", float64(i+1)/4, live.Stage[1], want, live.Stage[3])
		}
	}
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 30)
	for range 4 {
		if live := step(); live.Stage[1] != 30 {
			t.Fatalf("held at 30, green plays %d", live.Stage[1])
		}
	}
}

// TestAChaseChangedOrDeletedAsItRuns: its last step taken out from
// under it, it goes round to its first; a scene deleted leaves its
// steps, skipped; and deleted, it stops and is the last run no more.
func TestAChaseChangedOrDeletedAsItRuns(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 10})
	r.scene(1, map[int]byte{2: 20})
	r.chase(1, 0, 1)
	step := r.clocked()
	r.e.PressChase(r.board, 1)
	step()
	if st := step().Boards[r.board]; st.Step != 1 {
		t.Fatalf("the chase is at %d", st.Step)
	}
	r.chase(1, 0)
	if live := r.e.Tick(); live.Boards[r.board].Step != 0 || live.Stage[1] != 10 {
		t.Errorf("its last step taken out, the chase is at %d, green %d", live.Boards[r.board].Step, live.Stage[1])
	}
	r.chase(1, 0, 1)
	r.s.Module(r.board).Scenes = slices.DeleteFunc(r.s.Module(r.board).Scenes, func(c show.Scene) bool { return c.Pad == 0 })
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Boards[r.board].Step != 1 || live.Stage[1] != 20 || len(r.s.Module(r.board).Chase(1).Steps) != 2 {
		t.Errorf("a scene deleted, the chase is at %d, green %d", live.Boards[r.board].Step, live.Stage[1])
	}
	r.s.Module(r.board).Chases = nil
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Boards[r.board].Chase != 0 || live.Boards[r.board].Last != 0 || live.Stage[1] != 0 {
		t.Errorf("deleted, the chase shows %+v and green plays %d", live.Boards[r.board], live.Stage[1])
	}
}

// TestForgetLetsGoOfTheChases: the chase being made, the one that runs,
// the last run, a speed tapped and a chase made not yet taken go as
// another show takes the place.
func TestForgetLetsGoOfTheChases(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.chase(1, 0)
	r.e.PressChase(r.board, 1)
	r.e.TapChase(r.board)
	r.e.TapChase(r.board)
	r.e.NewChase(r.board)
	r.e.PressPad(r.board, 0)
	r.e.DoneChase(r.board)
	r.e.NewChase(r.board)
	r.e.PressPad(r.board, 0)
	r.e.Forget()
	r.e.Set(r.s)
	live := r.e.Tick()
	if st := live.Boards[r.board]; st.Making || len(st.Steps) != 0 || st.Chase != 0 || st.Last != 0 || live.Stage[1] != 0 {
		t.Errorf("forgotten, the board shows %+v and green plays %d", st, live.Stage[1])
	}
	if got := r.e.Made(); len(got) != 0 {
		t.Errorf("forgotten, the engine still hands over %+v", got)
	}
	if b := r.e.boards[r.board]; len(b.taps) != 0 || b.speed != 0 || len(b.chases) != 0 {
		t.Errorf("forgotten, the board keeps taps %v, speed %v and %d chases", b.taps, b.speed, len(b.chases))
	}
}

// ticked has r's engine tell the time from now, which run moves on a
// fortieth of a second at a time, calling each with what plays.
func (r *boardRig) ticked() (run func(d time.Duration, each func(Live))) {
	now := time.Unix(1000, 0)
	r.e.Now = func() time.Time { return now }
	r.e.Tick()
	return func(d time.Duration, each func(Live)) {
		for ; d > 0; d -= 25 * time.Millisecond {
			now = now.Add(25 * time.Millisecond)
			if live := r.e.Tick(); each != nil {
				each(live)
			}
		}
	}
}

// TestAChasesChannelFadesToASceneOfTheBoardsNotToWhatPlaysUnder: the
// dome's red plays at 0.8 from its Lights module, a scene on holds it
// at 50, and a chase's first step at 100, its second not at all. Red
// goes between 100 and 50, never to what the Lights module plays.
func TestAChasesChannelFadesToASceneOfTheBoardsNotToWhatPlaysUnder(t *testing.T) {
	r := newBoardRig(t)
	m := r.s.Module(r.board)
	m.Speed, m.Fade = 60, 0.5
	r.scene(0, map[int]byte{1: 50})
	r.scene(1, map[int]byte{1: 100})
	r.scene(2, map[int]byte{2: 100})
	r.chase(1, 1, 2)
	run := r.ticked()
	r.e.PressPad(r.board, 0)
	r.e.PressChase(r.board, 1)
	n := 0
	run(4*time.Second, func(live Live) {
		n++
		if red := live.Stage[0]; red < 50 || red > 100 {
			t.Fatalf("tick %d: red plays %d, outside what the board's scene and chase hold", n, red)
		}
	})
}

// TestAStepTakenMidFadeFadesFromWhatPlayed: a tap half way through a
// fade from green 0 to 250 fades on from where green was, not from 250.
func TestAStepTakenMidFadeFadesFromWhatPlayed(t *testing.T) {
	r := newBoardRig(t)
	m := r.s.Module(r.board)
	m.Speed, m.Fade = 60, 1
	r.scene(0, map[int]byte{2: 0})
	r.scene(1, map[int]byte{2: 250})
	r.chase(1, 0, 1, 0)
	run := r.ticked()
	r.e.PressChase(r.board, 1)
	var green byte
	see := func(live Live) { green = live.Stage[1] }
	run(1500*time.Millisecond, see)
	if green < 100 || green > 150 {
		t.Fatalf("half way through the fade, green plays %d", green)
	}
	// Past the middle of the step, the tap takes the next.
	run(100*time.Millisecond, see)
	was := green
	r.e.TapChase(r.board)
	n := 0
	run(time.Second, func(live Live) {
		n++
		got := live.Stage[1]
		if got > was {
			t.Fatalf("tick %d after the tap: green rose from %d to %d", n, was, got)
		}
		if int(was)-int(got) > 12 {
			t.Fatalf("tick %d after the tap: green fell from %d to %d in one tick", n, was, got)
		}
		was = got
	})
	if was > 10 {
		t.Errorf("the fade all but done, green plays %d", was)
	}
}

// TestRecordingOverARunningChaseKeepsTheStageAndStopsTheChase: a fader
// holds green under what the chase plays; recorded, the scene has what
// the board played, the chase with it, and the chase stops, so the
// stage plays on as it was.
func TestRecordingOverARunningChaseKeepsTheStageAndStopsTheChase(t *testing.T) {
	r := newBoardRig(t)
	// Green, white and the strobe.
	r.scene(0, map[int]byte{2: 200, 4: 90, 7: 40})
	r.chase(1, 0)
	run := r.ticked()
	r.e.PressChase(r.board, 1)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 30)
	var before Live
	run(100*time.Millisecond, func(live Live) { before = live })
	if before.Stage[1] != 30 || before.Stage[3] != 90 || before.Stage[6] != 40 {
		t.Fatalf("the chase under a fader plays green %d, white %d, strobe %d", before.Stage[1], before.Stage[3], before.Stage[6])
	}
	r.e.PressRecord(r.board)
	r.e.PressPad(r.board, 5)
	n := 0
	run(500*time.Millisecond, func(live Live) {
		n++
		if live.Stage != before.Stage {
			t.Fatalf("tick %d after recording: green %d, white %d, strobe %d", n, live.Stage[1], live.Stage[3], live.Stage[6])
		}
		if st := live.Boards[r.board]; st.Chase != 0 || st.Last != 1 || !slices.Equal(st.On, []int{5}) {
			t.Fatalf("tick %d after recording: the board shows chase %d, last %d, on %v", n, st.Chase, st.Last, st.On)
		}
	})
	got := r.e.Recorded()
	if len(got) != 1 || len(got[0].Scene.Values[r.dome]) != 3 || got[0].Scene.Values[r.dome][2] != 30 {
		t.Errorf("recorded %+v", got)
	}
}

// TestAChaseBeginningToFollowABeatFadesOn: its fade done, a chase told
// to follow the beat part way through its step plays on whole, and does
// not start the fade again.
func TestAChaseBeginningToFollowABeatFadesOn(t *testing.T) {
	r := newBoardRig(t)
	tempo := show.Module{ID: r.s.NewID("m"), Kind: show.Tempo, Name: "Tempo", BPM: 60, Manual: true, At: geom.Pt(0, 0)}
	r.s.Modules = append(r.s.Modules, tempo)
	r.s.Wires = append(r.s.Wires, show.Wire{ID: r.s.NewID("w"), From: show.End{Module: tempo.ID, Port: "out"}, To: show.End{Module: r.board, Port: "clock"}})
	m := r.s.Module(r.board)
	m.Speed, m.Fade = 60, 0.5
	r.scene(0, map[int]byte{2: 50})
	r.scene(1, map[int]byte{2: 250})
	r.chase(1, 0, 1)
	run := r.ticked()
	r.e.PressChase(r.board, 1)
	var green byte
	see := func(live Live) { green = live.Stage[1] }
	// Into the second step, its fade done.
	run(1800*time.Millisecond, see)
	if green != 250 {
		t.Fatalf("its fade done, green plays %d", green)
	}
	r.s.Module(r.board).FollowBeat = true
	r.e.Set(r.s)
	n := 0
	run(150*time.Millisecond, func(live Live) {
		n++
		if st := live.Boards[r.board]; !st.Following || (st.Step == 1 && live.Stage[1] != 250) {
			t.Fatalf("tick %d of following: following %v, step %d, green %d", n, st.Following, st.Step, live.Stage[1])
		}
	})
}
