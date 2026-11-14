package engine

import (
	"slices"
	"testing"

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
