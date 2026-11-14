package engine

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/fixtures"
)

// boardRig is the start show, its Board wired into the mixer's second
// input, with a second light of another type, an RGBW par with a
// dimmer, at address 10.
type boardRig struct {
	s                show.Show
	e                *Engine
	board, dome, par string
}

func newBoardRig(t *testing.T) *boardRig {
	t.Helper()
	r := &boardRig{s: show.Start()}
	r.board = moduleNamed(t, &r.s, "Board").ID
	r.dome = r.s.Rig.Lights[0].ID
	par := fixtures.Patch{ID: r.s.NewID("light"), Name: "Par", DefinitionID: "generic-rgbw-dimmer", ModeID: "5ch", StartAddress: 10}
	r.s.Rig.Lights = append(r.s.Rig.Lights, par)
	r.par = par.ID
	r.e = New(nil)
	t.Cleanup(r.e.Close)
	r.e.Set(r.s)
	return r
}

// TestABoardHoldsWhatItsFadersHoldAndNothingElse: a channel held is in
// its layer at its DMX value, with no curve; one not held is left out,
// and the Monitor names the Board as the owner of what it sets.
func TestABoardHoldsWhatItsFadersHoldAndNothingElse(t *testing.T) {
	r := newBoardRig(t)
	r.e.Pick(r.board, []string{r.dome}, false)
	// The dome's green, which its fader holds at 0.
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 100)
	live := r.e.Tick()
	l := r.e.layers[r.board]
	if v, ok := l.Get(2); !ok || v != 100 {
		t.Errorf("green held at 100 is %d in the layer, set %v", v, ok)
	}
	for _, ch := range []int{1, 3, 10} {
		if _, ok := l.Get(ch); ok {
			t.Errorf("channel %d, which nothing holds, is in the layer", ch)
		}
	}
	if live.Stage[1] != 100 || live.Owners[2] != r.board {
		t.Errorf("the stage plays green %d, owned by %q", live.Stage[1], live.Owners[2])
	}
	st := live.Boards[r.board]
	if !slices.Equal(st.Picked, []string{r.dome}) || st.Held[Channel{Light: r.dome, Offset: 2}] != 100 || st.Plays != live.Stage {
		t.Errorf("the board shows %+v", st)
	}
}

// TestAFaderOfMixedLightsSetsEachLightsChannel: a dome and a par picked
// have a fader for each thing they do between them; red's sets the
// red of each, at each one's own offset.
func TestAFaderOfMixedLightsSetsEachLightsChannel(t *testing.T) {
	r := newBoardRig(t)
	fs := Faders(&r.s, []string{r.dome, r.par})
	i := slices.IndexFunc(fs, func(f Fader) bool { return f.Role == fixtures.RoleRed })
	if i < 0 || !slices.ContainsFunc(fs, func(f Fader) bool { return f.Role == fixtures.RoleDimmer }) {
		t.Fatalf("the faders are %+v", fs)
	}
	red := fs[i]
	if len(red.Channels) != 2 || red.Channels[0] != (Channel{Light: r.dome, Offset: 1}) || red.Channels[1].Light != r.par {
		t.Fatalf("red sets %+v", red.Channels)
	}
	r.e.Hold(r.board, red.Channels, 255)
	r.e.Tick()
	l := r.e.layers[r.board]
	parRed := 10 + red.Channels[1].Offset - 1
	if v, _ := l.Get(1); v != 255 {
		t.Errorf("the dome's red is %d", v)
	}
	if v, _ := l.Get(parRed); v != 255 {
		t.Errorf("the par's red, at %d, is %d", parRed, v)
	}
	// One type alone: a fader for each of its nine channels, in order.
	fs = Faders(&r.s, []string{r.dome})
	if len(fs) != 9 || fs[0].Name != "Red" || fs[6].Role != fixtures.RoleStrobe || fs[8].Channels[0].Offset != 9 {
		t.Errorf("the dome alone has %+v", fs)
	}
	if Faders(&r.s, nil) != nil {
		t.Error("nothing picked has faders")
	}
}

// TestClearLetsGoOfEveryFader, and the mixer's home shows again.
func TestClearLetsGoOfEveryFader(t *testing.T) {
	r := newBoardRig(t)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}, {Light: r.par, Offset: 1}}, 200)
	r.e.Tick()
	r.e.ClearBoard(r.board)
	live := r.e.Tick()
	if _, ok := r.e.layers[r.board].Get(2); ok || len(live.Boards[r.board].Held) != 0 {
		t.Error("cleared, the board still holds a channel")
	}
	if live.Stage[1] != 0 {
		t.Errorf("cleared, green plays %d", live.Stage[1])
	}
}

// TestPickingAddsTakesOutAndReplaces: a click picks a light alone,
// Ctrl+click adds or takes one out, and a group picked whole with Ctrl
// is taken out whole.
func TestPickingAddsTakesOutAndReplaces(t *testing.T) {
	r := newBoardRig(t)
	picked := func() []string { return r.e.Tick().Boards[r.board].Picked }
	r.e.Pick(r.board, []string{r.dome}, false)
	r.e.Pick(r.board, []string{r.par}, true)
	if got := picked(); !slices.Equal(got, []string{r.dome, r.par}) {
		t.Errorf("Ctrl+click added: %v", got)
	}
	r.e.Pick(r.board, []string{r.dome}, true)
	if got := picked(); !slices.Equal(got, []string{r.par}) {
		t.Errorf("Ctrl+click took out: %v", got)
	}
	r.e.Pick(r.board, []string{r.dome, r.par, "nowhere"}, false)
	if got := picked(); !slices.Equal(got, []string{r.dome, r.par}) {
		t.Errorf("a group picked: %v", got)
	}
	r.e.Pick(r.board, []string{r.dome, r.par}, true)
	if got := picked(); len(got) != 0 {
		t.Errorf("a group picked whole, Ctrl+clicked, left %v", got)
	}
}

// TestWhatABoardHoldsOutlivesAShowChangeButNotItsLight: picked lights
// and held channels stay as the show changes; a light taken out of the
// rig drops out of both.
func TestWhatABoardHoldsOutlivesAShowChangeButNotItsLight(t *testing.T) {
	r := newBoardRig(t)
	r.e.Pick(r.board, []string{r.dome, r.par}, false)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}, {Light: r.par, Offset: 1}}, 90)
	r.s.Module(r.board).Name = "Desk"
	r.e.Set(r.s)
	st := r.e.Tick().Boards[r.board]
	if len(st.Picked) != 2 || len(st.Held) != 2 {
		t.Fatalf("after a show change the board shows %+v", st)
	}
	r.s.RemoveLight(r.par)
	r.e.Set(r.s)
	st = r.e.Tick().Boards[r.board]
	if !slices.Equal(st.Picked, []string{r.dome}) || len(st.Held) != 1 || st.Held[Channel{Light: r.dome, Offset: 2}] != 90 {
		t.Errorf("the par taken out, the board shows %+v", st)
	}
	r.s.RemoveModule(r.board)
	r.e.Set(r.s)
	if _, ok := r.e.Tick().Boards[r.board]; ok || r.e.boards[r.board] != nil {
		t.Error("the board taken out is still played")
	}
}

// TestTheBoardsMixerAndOutput: the start show's Board is wired into its
// mixer, which feeds its DMX out.
func TestTheBoardsMixerAndOutput(t *testing.T) {
	s := show.Start()
	b, m := moduleNamed(t, &s, "Board").ID, moduleNamed(t, &s, "Mixer").ID
	if MixerOf(&s, b) != m || OutOf(&s, m) != moduleNamed(t, &s, "DMX out").ID {
		t.Errorf("the board's mixer is %q, its output %q", MixerOf(&s, b), OutOf(&s, m))
	}
	if MixerOf(&s, "nothing") != "" || OutOf(&s, "nothing") != "" {
		t.Error("a module wired to nothing has a mixer or an output")
	}
}

// TestAFaderHeldPlaysWhatItSaysOverWhatElseIsWiredIn: the dome's red
// plays at 0.8 from its Lights module; the Board's fader held lower
// still has the channel, under the grand master, and the Monitor names
// the Board.
func TestAFaderHeldPlaysWhatItSaysOverWhatElseIsWiredIn(t *testing.T) {
	r := newBoardRig(t)
	if live := r.e.Tick(); live.Stage[0] <= 10 {
		t.Fatalf("the dome's red plays %d before the board holds it", live.Stage[0])
	}
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 1}}, 10)
	live := r.e.Tick()
	if live.Stage[0] != 10 || live.Owners[1] != r.board {
		t.Errorf("red held at 10 plays %d, owned by %q", live.Stage[0], live.Owners[1])
	}
	mixer := moduleNamed(t, &r.s, "Mixer")
	mixer.Value = 0.5
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Stage[0] != 5 {
		t.Errorf("under a grand master at half, red held at 10 plays %d", live.Stage[0])
	}
	r.e.ClearBoard(r.board)
	if live := r.e.Tick(); live.Stage[0] <= 10 || live.Owners[1] == r.board {
		t.Errorf("cleared, red plays %d, owned by %q", live.Stage[0], live.Owners[1])
	}
}

// TestTwoLightsAtOneAddressPlayTheHigherValueSteadily: a slot two
// lights share has the higher of what is held for it, tick after tick.
func TestTwoLightsAtOneAddressPlayTheHigherValueSteadily(t *testing.T) {
	r := newBoardRig(t)
	for i := range r.s.Rig.Lights {
		if r.s.Rig.Lights[i].ID == r.par {
			r.s.Rig.Lights[i].StartAddress = 2
		}
	}
	r.e.Set(r.s)
	// The dome's green and the par's dimmer are both channel 2.
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 200)
	r.e.Hold(r.board, []Channel{{Light: r.par, Offset: 1}}, 50)
	for i := range 100 {
		if live := r.e.Tick(); live.Stage[1] != 200 {
			t.Fatalf("tick %d: the shared channel plays %d, not 200", i, live.Stage[1])
		}
	}
}

// TestForgetLetsGoOfWhatAShowDoesNotKeep: what a Board picked and
// holds, and an override held, go as another show takes the place.
func TestForgetLetsGoOfWhatAShowDoesNotKeep(t *testing.T) {
	r := newBoardRig(t)
	mixer := moduleNamed(t, &r.s, "Mixer").ID
	r.e.Pick(r.board, []string{r.dome}, false)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 200)
	r.e.Override(mixer, Blackout, true)
	r.e.Forget()
	r.e.Set(r.s)
	live := r.e.Tick()
	if st := live.Boards[r.board]; len(st.Picked) != 0 || len(st.Held) != 0 || live.Stage[1] != 0 {
		t.Errorf("forgotten, the board shows %+v and green plays %d", st, live.Stage[1])
	}
	if live.Overrides[mixer][0] || live.Stage[0] == 0 {
		t.Errorf("forgotten, blackout is %v and red plays %d", live.Overrides[mixer][0], live.Stage[0])
	}
}

// scene puts a scene on pad of r's Board, of the dome's channels at the
// values given, by offset, and plays the show with it.
func (r *boardRig) scene(pad int, values map[int]byte) {
	r.s.Module(r.board).SetScene(show.Scene{Pad: pad, Name: "Look " + string(rune('A'+pad)), Values: map[string]map[int]byte{r.dome: values}})
	r.e.Set(r.s)
}

// clocked has r's engine tell the time from now, which step moves on a
// quarter of a second, the longest a tick runs a clock on by.
func (r *boardRig) clocked() (step func() Live) {
	now := time.Unix(1000, 0)
	r.e.Now = func() time.Time { return now }
	r.e.Tick()
	return func() Live {
		now = now.Add(250 * time.Millisecond)
		return r.e.Tick()
	}
}

// TestRecordingStoresTheFadersOverTheScenesAndTheStagePlaysOn: a scene
// on and two faders held, recorded onto a pad, make a scene of both,
// the faders winning; it is then on alone and the faders let go, and
// the stage plays as it did, tick by tick, before and after the show
// holds the scene.
func TestRecordingStoresTheFadersOverTheScenesAndTheStagePlaysOn(t *testing.T) {
	r := newBoardRig(t)
	// Green and the strobe, which the grand master does not dim.
	r.scene(0, map[int]byte{2: 100, 7: 50})
	r.e.PressPad(r.board, 0)
	if live := r.e.Tick(); live.Stage[1] != 100 || live.Stage[6] != 50 {
		t.Fatalf("the scene on plays green %d and strobe %d", live.Stage[1], live.Stage[6])
	}
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}, {Light: r.dome, Offset: 4}}, 200)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 4}}, 80)
	before := r.e.Tick().Stage
	if before[1] != 200 || before[3] != 80 {
		t.Fatalf("held, green plays %d and white %d", before[1], before[3])
	}
	r.e.PressRecord(r.board)
	if !r.e.Tick().Boards[r.board].Record {
		t.Fatal("Record pressed does not wait for a pad")
	}
	r.e.PressPad(r.board, 1)
	steady := func(when string) {
		t.Helper()
		for i := range 5 {
			live := r.e.Tick()
			if live.Stage != before {
				t.Fatalf("%s, tick %d: the stage changed", when, i)
			}
			st := live.Boards[r.board]
			if !slices.Equal(st.On, []int{1}) || len(st.Held) != 0 || st.Record || st.Levels[1] != 1 {
				t.Fatalf("%s, tick %d: the board shows %+v", when, i, st)
			}
		}
	}
	steady("recorded")
	got := r.e.Recorded()
	if len(got) != 1 || got[0].Module != r.board {
		t.Fatalf("recorded %+v", got)
	}
	sc := got[0].Scene
	want := map[string]map[int]byte{r.dome: {2: 200, 4: 80, 7: 50}}
	if sc.Pad != 1 || sc.Name != "Scene 2" || !reflect.DeepEqual(sc.Values, want) {
		t.Errorf("the scene recorded is %+v", sc)
	}
	if again := r.e.Recorded(); len(again) != 0 {
		t.Errorf("asked again, recorded %+v", again)
	}
	steady("handed to the application")
	r.s.Module(r.board).SetScene(sc)
	r.e.Set(r.s)
	steady("held by the show")
	if len(r.e.boards[r.board].kept) != 0 {
		t.Error("the board still keeps a scene the show holds")
	}
}

// TestASceneRecordedPlaysOnThroughAShowSetWithoutIt: a show set before
// the application has the scene, as for another change, does not stop
// it playing.
func TestASceneRecordedPlaysOnThroughAShowSetWithoutIt(t *testing.T) {
	r := newBoardRig(t)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 150)
	r.e.Tick()
	r.e.PressRecord(r.board)
	r.e.PressPad(r.board, 5)
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Stage[1] != 150 || !slices.Equal(live.Boards[r.board].On, []int{5}) {
		t.Errorf("a show set without the scene, green plays %d and %v is on", live.Stage[1], live.Boards[r.board].On)
	}
}

// TestRecordingOntoAPadThatHasASceneReplacesIt, and keeps its name.
func TestRecordingOntoAPadThatHasASceneReplacesIt(t *testing.T) {
	r := newBoardRig(t)
	r.scene(2, map[int]byte{2: 100, 7: 50})
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 4}}, 60)
	r.e.PressRecord(r.board)
	r.e.PressPad(r.board, 2)
	got := r.e.Recorded()
	if len(got) != 1 || got[0].Scene.Name != "Look C" || !reflect.DeepEqual(got[0].Scene.Values, map[string]map[int]byte{r.dome: {4: 60}}) {
		t.Fatalf("recorded over Look C: %+v", got)
	}
	if live := r.e.Tick(); live.Stage[1] != 0 || live.Stage[3] != 60 {
		t.Errorf("the new scene plays green %d and white %d", live.Stage[1], live.Stage[3])
	}
}

// TestRecordingWithNothingToStoreGivesUp: no fader held and no scene
// on, a pad pressed stores nothing, and Record waits no more.
func TestRecordingWithNothingToStoreGivesUp(t *testing.T) {
	r := newBoardRig(t)
	r.scene(3, map[int]byte{2: 100})
	r.e.PressRecord(r.board)
	r.e.PressPad(r.board, 3)
	st := r.e.Tick().Boards[r.board]
	if got := r.e.Recorded(); len(got) != 0 || st.Record || len(st.On) != 0 {
		t.Errorf("recording nothing stored %+v, and the board shows %+v", got, st)
	}
}

// TestAPadTurnsItsSceneOnAndOff, an empty pad does nothing, and Record
// pressed again, or given up, waits no more.
func TestAPadTurnsItsSceneOnAndOff(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.e.PressPad(r.board, 0)
	if live := r.e.Tick(); !slices.Equal(live.Boards[r.board].On, []int{0}) || live.Stage[1] != 100 {
		t.Errorf("pressed, pad 1 shows %v and green plays %d", live.Boards[r.board].On, live.Stage[1])
	}
	r.e.PressPad(r.board, 0)
	if live := r.e.Tick(); len(live.Boards[r.board].On) != 0 || live.Stage[1] != 0 {
		t.Errorf("pressed again, %v is on and green plays %d", live.Boards[r.board].On, live.Stage[1])
	}
	r.e.PressPad(r.board, 9)
	r.e.PressPad(r.board, show.Pads)
	if st := r.e.Tick().Boards[r.board]; len(st.On) != 0 || len(st.Levels) != 0 {
		t.Errorf("empty pads pressed, the board shows %+v", st)
	}
	r.e.PressRecord(r.board)
	r.e.PressRecord(r.board)
	if r.e.Tick().Boards[r.board].Record {
		t.Error("Record pressed twice still waits")
	}
	r.e.PressRecord(r.board)
	r.e.GiveUpRecord(r.board)
	r.e.GiveUpRecord(r.board)
	if r.e.Tick().Boards[r.board].Record {
		t.Error("given up, Record still waits")
	}
}

// TestASceneFadesWhatTheMasterDimsAndSetsTheRestAtOnce: over a fade of
// two seconds, green comes up and goes down in step, and the strobe is
// set as the scene goes on and let go as it has faded out.
func TestASceneFadesWhatTheMasterDimsAndSetsTheRestAtOnce(t *testing.T) {
	r := newBoardRig(t)
	r.s.Module(r.board).SceneFade = 2
	r.scene(0, map[int]byte{2: 200, 7: 100})
	step := r.clocked()
	home := step().Stage[6]
	r.e.PressPad(r.board, 0)
	for i, want := range []byte{25, 50, 75, 100, 125, 150, 175, 200, 200} {
		live := step()
		if live.Stage[1] != want || live.Stage[6] != 100 {
			t.Fatalf("fading in, at %v s green plays %d, want %d, and strobe %d", float64(i+1)/4, live.Stage[1], want, live.Stage[6])
		}
		if !slices.Equal(live.Boards[r.board].On, []int{0}) {
			t.Fatalf("fading in, %v is on", live.Boards[r.board].On)
		}
	}
	r.e.PressPad(r.board, 0)
	for i, want := range []byte{175, 150, 125, 100, 75, 50, 25} {
		live := step()
		if live.Stage[1] != want || live.Stage[6] != 100 {
			t.Fatalf("fading out, at %v s green plays %d, want %d, and strobe %d", float64(i+1)/4, live.Stage[1], want, live.Stage[6])
		}
		if st := live.Boards[r.board]; len(st.On) != 0 || st.Levels[0] <= 0 {
			t.Fatalf("fading out, the board shows %+v", st)
		}
	}
	live := step()
	if live.Stage[1] != 0 || live.Stage[6] != home || len(live.Boards[r.board].Levels) != 0 {
		t.Errorf("faded out, green plays %d and strobe %d, and the board shows %+v", live.Stage[1], live.Stage[6], live.Boards[r.board])
	}
	// No fade: on and off at once.
	r.s.Module(r.board).SceneFade = 0
	r.e.Set(r.s)
	r.e.PressPad(r.board, 0)
	if live := step(); live.Stage[1] != 200 {
		t.Errorf("with no fade, green comes up to %d", live.Stage[1])
	}
	r.e.PressPad(r.board, 0)
	if live := step(); live.Stage[1] != 0 || live.Stage[6] != home {
		t.Errorf("with no fade, green goes down to %d and strobe to %d", live.Stage[1], live.Stage[6])
	}
}

// TestTwoScenesOnOneChannelPlayTheHigher, steadily, and the lower
// shows again as the higher goes.
func TestTwoScenesOnOneChannelPlayTheHigher(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.scene(1, map[int]byte{2: 180, 4: 20})
	r.e.PressPad(r.board, 0)
	r.e.PressPad(r.board, 1)
	for i := range 50 {
		if live := r.e.Tick(); live.Stage[1] != 180 || live.Stage[3] != 20 {
			t.Fatalf("tick %d: green plays %d and white %d", i, live.Stage[1], live.Stage[3])
		}
	}
	r.e.PressPad(r.board, 1)
	if live := r.e.Tick(); live.Stage[1] != 100 || live.Stage[3] != 0 {
		t.Errorf("the higher gone, green plays %d and white %d", live.Stage[1], live.Stage[3])
	}
}

// TestAFaderHeldWinsOverASceneAndClearShowsTheSceneAgain: green held
// below the scene's plays what the fader says, and once cleared, what
// the scene says.
func TestAFaderHeldWinsOverASceneAndClearShowsTheSceneAgain(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 200})
	r.e.PressPad(r.board, 0)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 30)
	if live := r.e.Tick(); live.Stage[1] != 30 || live.Owners[2] != r.board {
		t.Errorf("held under the scene, green plays %d", live.Stage[1])
	}
	r.e.ClearBoard(r.board)
	if live := r.e.Tick(); live.Stage[1] != 200 {
		t.Errorf("cleared, green plays %d, not the scene's 200", live.Stage[1])
	}
}

// TestASceneLeavesOutWhatTheRigDoesNotHave: a light taken out of the
// rig leaves every scene, and a channel its light does not have is
// passed over as the scene plays.
func TestASceneLeavesOutWhatTheRigDoesNotHave(t *testing.T) {
	r := newBoardRig(t)
	r.s.Module(r.board).SetScene(show.Scene{Pad: 0, Name: "Both", Values: map[string]map[int]byte{
		r.dome: {2: 90, 12: 255}, r.par: {1: 70}}})
	r.e.Set(r.s)
	r.e.PressPad(r.board, 0)
	live := r.e.Tick()
	if live.Stage[1] != 90 || live.Stage[9] != 70 || live.Stage[11] != 0 {
		t.Errorf("the scene plays green %d, the par's dimmer %d and channel 12 %d", live.Stage[1], live.Stage[9], live.Stage[11])
	}
	r.s.RemoveLight(r.par)
	if _, ok := r.s.Module(r.board).Scene(0).Values[r.par]; ok {
		t.Fatal("the par taken out of the rig stayed in the scene")
	}
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Stage[1] != 90 || !slices.Equal(live.Boards[r.board].On, []int{0}) {
		t.Errorf("the par taken out, the scene plays green %d, on %v", live.Stage[1], live.Boards[r.board].On)
	}
	r.s.Module(r.board).Scenes = nil
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Stage[1] != 0 || len(live.Boards[r.board].On) != 0 {
		t.Errorf("the scene deleted, green plays %d, on %v", live.Stage[1], live.Boards[r.board].On)
	}
}

// TestForgetLetsGoOfTheScenesOn, Record waiting, and the scenes
// recorded that the application has not taken.
func TestForgetLetsGoOfTheScenesOn(t *testing.T) {
	r := newBoardRig(t)
	r.scene(0, map[int]byte{2: 100})
	r.e.PressPad(r.board, 0)
	r.e.PressRecord(r.board)
	r.e.PressPad(r.board, 1)
	r.e.PressRecord(r.board)
	r.e.Forget()
	r.e.Set(r.s)
	live := r.e.Tick()
	if st := live.Boards[r.board]; len(st.On) != 0 || st.Record || live.Stage[1] != 0 {
		t.Errorf("forgotten, the board shows %+v and green plays %d", st, live.Stage[1])
	}
	if got := r.e.Recorded(); len(got) != 0 {
		t.Errorf("forgotten, the engine still hands over %+v", got)
	}
}

// TestRecordingAFaderHeldUnderAnotherModulesLightLeavesTheStage: the
// dome's red plays at 0.8 from its Lights module, and the Board holds
// it at 10. Recorded, the scene has the channel as the fader had, so
// the stage plays on as it was, and the Monitor names the Board.
func TestRecordingAFaderHeldUnderAnotherModulesLightLeavesTheStage(t *testing.T) {
	r := newBoardRig(t)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 1}}, 10)
	before := r.e.Tick().Stage
	if before[0] != 10 {
		t.Fatalf("red held at 10 plays %d", before[0])
	}
	r.e.PressRecord(r.board)
	r.e.PressPad(r.board, 0)
	for i := range 5 {
		live := r.e.Tick()
		if live.Stage != before {
			t.Fatalf("recorded, tick %d: red plays %d, not %d", i, live.Stage[0], before[0])
		}
		if live.Owners[1] != r.board {
			t.Fatalf("recorded, tick %d: red is owned by %q", i, live.Owners[1])
		}
	}
}

// TestASceneFadingCrossesFromWhatPlaysUnderIt: over the dome's red at
// 0.8 from its Lights module, a scene with red at 10 fades down from
// what played to 10, and back up to it as it fades out, never dipping
// to dark.
func TestASceneFadingCrossesFromWhatPlaysUnderIt(t *testing.T) {
	r := newBoardRig(t)
	r.s.Module(r.board).SceneFade = 1
	r.scene(0, map[int]byte{1: 10})
	step := r.clocked()
	under := r.e.Tick().Stage[0]
	if under <= 100 {
		t.Fatalf("the dome's red plays %d under the board", under)
	}
	r.e.PressPad(r.board, 0)
	was := under
	for i := range 4 {
		got := step().Stage[0]
		if got >= was || got < 10 {
			t.Fatalf("fading in, step %d: red went from %d to %d", i, was, got)
		}
		was = got
	}
	if was != 10 {
		t.Fatalf("faded in, red plays %d, not 10", was)
	}
	r.e.PressPad(r.board, 0)
	for i := range 4 {
		got := step().Stage[0]
		if got <= was || got > under {
			t.Fatalf("fading out, step %d: red went from %d to %d", i, was, got)
		}
		was = got
	}
	if was != under {
		t.Errorf("faded out, red plays %d, not %d", was, under)
	}
}

// TestASceneHandedOverPlaysOnThroughAShowSetWithoutIt: handed to the
// application, a scene recorded still plays through a show set that has
// none on its pad, and goes from the board's keeping once one has.
func TestASceneHandedOverPlaysOnThroughAShowSetWithoutIt(t *testing.T) {
	r := newBoardRig(t)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 150)
	r.e.Tick()
	r.e.PressRecord(r.board)
	r.e.PressPad(r.board, 5)
	got := r.e.Recorded()
	if len(got) != 1 {
		t.Fatalf("recorded %+v", got)
	}
	r.e.Set(r.s)
	for i := range 3 {
		if live := r.e.Tick(); live.Stage[1] != 150 || !slices.Equal(live.Boards[r.board].On, []int{5}) {
			t.Fatalf("a show set without the scene, tick %d: green plays %d and %v is on", i, live.Stage[1], live.Boards[r.board].On)
		}
	}
	r.s.Module(r.board).SetScene(got[0].Scene)
	r.e.Set(r.s)
	if live := r.e.Tick(); live.Stage[1] != 150 || len(r.e.boards[r.board].kept) != 0 {
		t.Errorf("held by the show, green plays %d and the board keeps %d scenes", live.Stage[1], len(r.e.boards[r.board].kept))
	}
}
