package engine

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/audio/midi"
)

// playRig is the start show, a par beside its dome, and an X-Touch Mini
// on fake ports that plays its Board, lit once already.
type playRig struct {
	*midiRig
	board, mixer, dome, par string
}

func newPlayRig(t *testing.T) *playRig {
	t.Helper()
	r := &playRig{midiRig: &midiRig{s: show.Start(), d: &fakeMIDI{}, now: time.Unix(1000, 0)}}
	r.board = moduleNamed(t, &r.s, "Board").ID
	r.mixer = moduleNamed(t, &r.s, "Mixer").ID
	r.dome = r.s.Rig.Lights[0].ID
	par := fixtures.Patch{ID: r.s.NewID("light"), Name: "Par", DefinitionID: "generic-rgbw-dimmer", ModeID: "5ch", StartAddress: 10}
	r.s.Rig.Lights = append(r.s.Rig.Lights, par)
	r.par = par.ID
	r.ctl = r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: r.ctl, Kind: show.Controller, Name: "X-Touch Mini", Device: "X-TOUCH MINI",
		Layout: controller.XTouchMiniID})
	r.s.Module(r.board).PlayedFrom = r.ctl
	r.e = New(nil)
	r.e.MIDI = r.d
	r.e.Now = func() time.Time { return r.now }
	r.e.Set(r.s)
	waitPorts(t, r.e)
	t.Cleanup(r.e.Close)
	r.tick()
	r.d.took()
	return r
}

// press presses the button of note down and lets it go, within a tick.
func (r *playRig) press(note int) Live {
	r.d.play(midi.NoteOn(0, note, 127))
	r.d.play(midi.NoteOn(0, note, 0))
	return r.tick()
}

// set plays the rig's show as it now is.
func (r *playRig) set() {
	r.e.Set(r.s)
}

// The X-Touch Mini's notes and controllers, as a Board plays them.
const (
	padNote1     = 89
	beforeNote   = 87
	afterNote    = 88
	recordNote   = 91
	clearNote    = 92
	chaseNote    = 86
	tapNote      = 93
	flashNote    = 94
	blackoutNote = 95
	layerANote   = 84
	layerBNote   = 85
)

// topNotes are the top row's notes, left to right.
var topNotes = []int{89, 90, 40, 41, 42, 43, 44, 45}

// allLights are every ring and button light of the X-Touch Mini, in its
// layout's order: the rings at ring, the buttons at what button says
// for each note.
func allLights(ring func(cc int) int, button func(note int) int) []midi.Message {
	var w []midi.Message
	for cc := 48; cc <= 55; cc++ {
		w = append(w, midi.Control(0, cc, ring(cc)))
	}
	for _, n := range []int{89, 90, 40, 41, 42, 43, 44, 45, 87, 88, 91, 92, 86, 93, 94, 95, 84, 85} {
		w = append(w, midi.NoteOn(0, n, button(n)))
	}
	return w
}

// dark is a ring dark, whichever.
func dark(int) int { return 32 }

// TestAControllerPlayingTheBoardIsLitAsTheBoardHasIt: as it starts
// playing the Board, every light is sent as the Board has it, Layer A
// lit; as it stops, every light again as its wires have them; again as
// the Board's as it starts once more, and after it comes back. Nothing
// is sent twice.
func TestAControllerPlayingTheBoardIsLitAsTheBoardHasIt(t *testing.T) {
	r := newPlayRig(t)
	board := func(n int) int {
		if n == layerANote {
			return 127
		}
		return 0
	}
	routes := func(int) int { return 0 }
	check := func(want []midi.Message, what string) {
		t.Helper()
		if sent := r.d.took(); !sameBytes(sent, want) {
			t.Errorf("%s, the X-Touch Mini was sent\n%v\nwant\n%v", what, sent, want)
		}
	}
	r.s.Module(r.board).PlayedFrom = ""
	r.set()
	r.tick()
	check(allLights(dark, routes), "the board played from none")
	r.tick()
	check(nil, "a tick on")
	r.s.Module(r.board).PlayedFrom = r.ctl
	r.set()
	r.tick()
	check(allLights(dark, board), "the board played from it again")
	r.replug(t)
	hello := make([]midi.Message, 0, 16)
	for ch := range 16 {
		hello = append(hello, midi.Message{Status: byte(0xB0 + ch), Data1: 127, Data2: 1})
	}
	check(append(hello, allLights(dark, board)...), "back again")
	r.tick()
	check(nil, "a tick on")
}

// TestTheKnobsSetThePickedLightsFadersFromWhereTheyStand: with nothing
// picked a knob does nothing and the rings stay dark; the dome picked,
// each ring shows its channel as it plays; a knob turned holds its
// channel from where it stands, 3 of 255 a step times its speed, and
// within 0 and 255; pressed, it lets go of its channel alone.
func TestTheKnobsSetThePickedLightsFadersFromWhereTheyStand(t *testing.T) {
	r := newPlayRig(t)
	r.d.play(midi.Control(0, 17, 1))
	if st := r.tick().Boards[r.board]; len(st.Held) != 0 {
		t.Errorf("with nothing picked, a knob holds %v", st.Held)
	}
	if sent := r.d.took(); len(sent) != 0 {
		t.Errorf("with nothing picked, a knob turned sent %v", sent)
	}
	stage := r.press(afterNote).Stage
	if st := r.e.Tick(); !slices.Equal(st.Boards[r.board].Picked, []string{r.dome}) {
		t.Fatalf("Bottom 2 with nothing picked picks %v", st.Boards[r.board].Picked)
	}
	var want []midi.Message
	for k := range 8 {
		if ring := controller.Ring(float64(stage[k]) / 255); ring != 32 {
			want = append(want, midi.Control(0, 48+k, ring))
		}
	}
	if len(want) == 0 {
		t.Fatal("the dome plays nothing, so this test checks nothing")
	}
	if sent := r.d.took(); !sameBytes(sent, want) {
		t.Errorf("the dome picked, the rings were sent %v, want %v", sent, want)
	}
	red := Channel{Light: r.dome, Offset: 1}
	green := Channel{Light: r.dome, Offset: 2}
	r.d.play(midi.Control(0, 17, 5))
	st := r.tick().Boards[r.board]
	if st.Held[green] != 15 {
		t.Errorf("green, at 0, turned up at speed 5 holds %d, want 15", st.Held[green])
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.Control(0, 49, 33), midi.NoteOn(0, clearNote, 127)}) {
		t.Errorf("green held at 15, the X-Touch Mini was sent %v", sent)
	}
	r.d.play(midi.Control(0, 16, 65))
	if st := r.tick().Boards[r.board]; int(st.Held[red]) != int(stage[0])-3 {
		t.Errorf("red, at %d, turned down a step holds %d", stage[0], st.Held[red])
	}
	r.d.play(midi.Control(0, 17, 71))
	r.d.play(midi.Control(0, 17, 71))
	if st := r.tick().Boards[r.board]; st.Held[green] != 0 {
		t.Errorf("green turned down past 0 holds %d", st.Held[green])
	}
	r.d.play(midi.Control(0, 16, 7))
	for range 20 {
		r.d.play(midi.Control(0, 16, 7))
	}
	if st := r.tick().Boards[r.board]; st.Held[red] != 255 {
		t.Errorf("red turned up past 255 holds %d", st.Held[red])
	}
	r.press(33)
	st = r.tick().Boards[r.board]
	if _, ok := st.Held[green]; ok || st.Held[red] != 255 {
		t.Errorf("knob 2 pressed, the board holds %v", st.Held)
	}
}

// TestTheTopRowPressesThePadsAndShowsThem: a pad pressed turns its
// scene on and lights, again off; Record blinks with every pad while it
// waits, and a pad then records; while a chase is made, a pad that is a
// step is lit.
func TestTheTopRowPressesThePadsAndShowsThem(t *testing.T) {
	r := newPlayRig(t)
	r.s.Module(r.board).SetScene(show.Scene{Pad: 0, Name: "Red", Values: map[string]map[int]byte{r.dome: {1: 255}}})
	r.set()
	r.tick()
	r.d.took()
	if st := r.press(padNote1).Boards[r.board]; !slices.Equal(st.On, []int{0}) {
		t.Errorf("pad 1 pressed, the scenes on are %v", st.On)
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, padNote1, 127)}) {
		t.Errorf("pad 1 on, the X-Touch Mini was sent %v", sent)
	}
	r.press(padNote1)
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, padNote1, 0)}) {
		t.Errorf("pad 1 off, the X-Touch Mini was sent %v", sent)
	}
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}}, 100)
	r.tick()
	r.d.took()
	if st := r.press(recordNote).Boards[r.board]; !st.Record {
		t.Fatal("Bottom 3 pressed, Record does not wait")
	}
	var want []midi.Message
	for _, n := range topNotes {
		want = append(want, midi.NoteOn(0, n, controller.LightBlink))
	}
	want = append(want, midi.NoteOn(0, recordNote, controller.LightBlink))
	if sent := r.d.took(); !sameBytes(sent, want) {
		t.Errorf("Record waiting, the X-Touch Mini was sent\n%v\nwant\n%v", sent, want)
	}
	r.tick()
	if sent := r.d.took(); len(sent) != 0 {
		t.Errorf("Record still waiting, the X-Touch Mini was sent %v again", sent)
	}
	if st := r.press(90).Boards[r.board]; st.Record || !slices.Equal(st.On, []int{1}) || len(st.Held) != 0 {
		t.Errorf("pad 2 pressed as Record waits, the board shows %+v", st)
	}
	if got := r.e.Recorded(); len(got) != 1 || got[0].Scene.Pad != 1 {
		t.Errorf("recorded %+v", got)
	}
	want = want[:0]
	for _, n := range topNotes {
		v := 0
		if n == 90 {
			v = 127
		}
		want = append(want, midi.NoteOn(0, n, v))
	}
	want = append(want, midi.NoteOn(0, recordNote, 0), midi.NoteOn(0, clearNote, 0))
	if sent := r.d.took(); !sameBytes(sent, want) {
		t.Errorf("recorded onto pad 2, the X-Touch Mini was sent\n%v\nwant\n%v", sent, want)
	}
	r.e.NewChase(r.board)
	r.tick()
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, 90, 0)}) {
		t.Errorf("a chase begun, the X-Touch Mini was sent %v", sent)
	}
	if st := r.press(padNote1).Boards[r.board]; !slices.Equal(st.Steps, []int{0}) {
		t.Errorf("pad 1 pressed as a chase is made, its steps are %v", st.Steps)
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, padNote1, 127)}) {
		t.Errorf("pad 1 a step, the X-Touch Mini was sent %v", sent)
	}
}

// TestBottomOneAndTwoPickTheLightBeforeAndAfter, alone, round the rig;
// with nothing picked, or several, Bottom 2 picks the first and Bottom
// 1 the last.
func TestBottomOneAndTwoPickTheLightBeforeAndAfter(t *testing.T) {
	r := newPlayRig(t)
	picked := func(note int) []string { return r.press(note).Boards[r.board].Picked }
	for i, c := range []struct {
		note int
		want string
	}{{beforeNote, r.par}, {beforeNote, r.dome}, {beforeNote, r.par}, {afterNote, r.dome}, {afterNote, r.par}, {afterNote, r.dome}} {
		if got := picked(c.note); !slices.Equal(got, []string{c.want}) {
			t.Errorf("press %d, of note %d, picks %v, want %s", i+1, c.note, got, c.want)
		}
	}
	r.e.Pick(r.board, []string{r.dome, r.par}, false)
	if got := picked(beforeNote); !slices.Equal(got, []string{r.par}) {
		t.Errorf("two picked, Bottom 1 picks %v, not the last", got)
	}
	r.e.Pick(r.board, []string{r.par, r.dome}, false)
	if got := picked(afterNote); !slices.Equal(got, []string{r.dome}) {
		t.Errorf("two picked, Bottom 2 picks %v, not the first", got)
	}
}

// TestClearLetsGoAndIsLitWhileAFaderHolds: Bottom 4 lets go of every
// fader, and is lit while one holds a value.
func TestClearLetsGoAndIsLitWhileAFaderHolds(t *testing.T) {
	r := newPlayRig(t)
	r.e.Hold(r.board, []Channel{{Light: r.dome, Offset: 2}, {Light: r.par, Offset: 1}}, 200)
	r.tick()
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, clearNote, 127)}) {
		t.Errorf("a fader held, the X-Touch Mini was sent %v", sent)
	}
	if st := r.press(clearNote).Boards[r.board]; len(st.Held) != 0 {
		t.Errorf("Clear pressed, the board holds %v", st.Held)
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, clearNote, 0)}) {
		t.Errorf("cleared, the X-Touch Mini was sent %v", sent)
	}
}

// TestBottomFiveRunsTheLastChaseAndTapFlashesOnItsSteps: it stops the
// chase that runs, lit while it runs, and runs it again; Tap's light
// flashes as the chase takes a step, and taps set the speed.
func TestBottomFiveRunsTheLastChaseAndTapFlashesOnItsSteps(t *testing.T) {
	r := newPlayRig(t)
	m := r.s.Module(r.board)
	m.SetScene(show.Scene{Pad: 0, Name: "Red", Values: map[string]map[int]byte{r.dome: {1: 255}}})
	m.SetScene(show.Scene{Pad: 1, Name: "Blue", Values: map[string]map[int]byte{r.dome: {3: 255}}})
	m.Chases = []show.Chase{{ID: 1, Name: "Round", Steps: []int{0, 1}}}
	m.Speed = 60
	r.set()
	if st := r.press(chaseNote).Boards[r.board]; st.Chase != 0 {
		t.Fatalf("with no chase run before, Bottom 5 runs chase %d", st.Chase)
	}
	r.d.took()
	r.e.PressChase(r.board, 1)
	r.tick()
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, chaseNote, 127)}) {
		t.Errorf("a chase run, the X-Touch Mini was sent %v", sent)
	}
	if st := r.press(chaseNote).Boards[r.board]; st.Chase != 0 {
		t.Errorf("Bottom 5 pressed, chase %d runs on", st.Chase)
	}
	if st := r.press(chaseNote).Boards[r.board]; st.Chase != 1 || st.Step != 0 {
		t.Errorf("Bottom 5 pressed again, chase %d runs at step %d", st.Chase, st.Step)
	}
	r.d.took()
	// A step a second: the light flashes as the chase takes its second
	// step, and is out again a tenth of a second on.
	var on, off int
	for i := range 44 {
		r.tick()
		for _, msg := range r.d.took() {
			switch {
			case sameAs(midi.NoteOn(0, tapNote, 127))(msg):
				on = i
			case sameAs(midi.NoteOn(0, tapNote, 0))(msg):
				off = i
			}
		}
	}
	if on < 36 || on > 40 || off-on < 3 || off-on > 4 {
		t.Errorf("the Tap light went on at tick %d and off at %d", on, off)
	}
	r.press(tapNote)
	// Half a second on, with the tick the press takes.
	r.now = r.now.Add(475 * time.Millisecond)
	if st := r.press(tapNote).Boards[r.board]; st.Speed != 120 {
		t.Errorf("tapped half a second apart, the chase runs at %v a minute", st.Speed)
	}
}

// TestFlashIsHeldWhileItsButtonIsAndLetGoAsTheControllerGoes: the
// mixer's flash is on, and lit, while Bottom 7 is held; let go, it is
// off. Held as the controller is unplugged, or stops playing the Board,
// it is off too.
func TestFlashIsHeldWhileItsButtonIsAndLetGoAsTheControllerGoes(t *testing.T) {
	r := newPlayRig(t)
	flash := func() bool {
		r.tick()
		return r.tick().Overrides[r.mixer][1]
	}
	r.d.play(midi.NoteOn(0, flashNote, 127))
	if !flash() {
		t.Fatal("Bottom 7 held, the mixer does not flash")
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, flashNote, 127)}) {
		t.Errorf("flashing, the X-Touch Mini was sent %v", sent)
	}
	r.d.play(midi.NoteOn(0, flashNote, 0))
	if flash() {
		t.Error("Bottom 7 let go, the mixer flashes on")
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, flashNote, 0)}) {
		t.Errorf("not flashing, the X-Touch Mini was sent %v", sent)
	}
	r.d.play(midi.NoteOn(0, flashNote, 127))
	if !flash() {
		t.Fatal("Bottom 7 held again, the mixer does not flash")
	}
	r.d.mu.Lock()
	r.d.plugs++
	r.d.mu.Unlock()
	if flash() {
		t.Error("held as the controller was unplugged, the mixer flashes on")
	}
	r.replug(t)
	r.d.play(midi.NoteOn(0, flashNote, 127))
	if !flash() {
		t.Fatal("back again and Bottom 7 held, the mixer does not flash")
	}
	r.s.Module(r.board).PlayedFrom = ""
	r.set()
	if flash() {
		t.Error("held as the controller stopped playing the Board, the mixer flashes on")
	}
}

// TestBottomEightTurnsBlackoutOnAndOff, lit while it is on.
func TestBottomEightTurnsBlackoutOnAndOff(t *testing.T) {
	r := newPlayRig(t)
	r.press(blackoutNote)
	if !r.tick().Overrides[r.mixer][0] {
		t.Error("Bottom 8 pressed, the mixer is not blacked out")
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, blackoutNote, 127)}) {
		t.Errorf("blacked out, the X-Touch Mini was sent %v", sent)
	}
	r.press(blackoutNote)
	if r.tick().Overrides[r.mixer][0] {
		t.Error("Bottom 8 pressed again, the mixer is still blacked out")
	}
	if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, blackoutNote, 0)}) {
		t.Errorf("blackout off, the X-Touch Mini was sent %v", sent)
	}
}

// TestLayerBTurnsToTheSecondPage: knob 1 sets the ninth fader and the
// top row the pads from 9; the knobs past the faders there are do
// nothing, dark; Layer B is lit; Layer A turns back.
func TestLayerBTurnsToTheSecondPage(t *testing.T) {
	r := newPlayRig(t)
	r.s.Module(r.board).SetScene(show.Scene{Pad: 8, Name: "Nine", Values: map[string]map[int]byte{r.dome: {1: 255}}})
	r.set()
	r.e.Pick(r.board, []string{r.dome}, false)
	stage := r.tick().Stage
	r.d.took()
	if st := r.press(layerBNote).Boards[r.board]; st.Page != 1 || st.Remote != r.ctl {
		t.Fatalf("Layer B pressed, the board says page %d, played from %q", st.Page, st.Remote)
	}
	var want []midi.Message
	for k := range 8 {
		was, now := controller.Ring(float64(stage[k])/255), 32
		if k == 0 {
			now = controller.Ring(float64(stage[8]) / 255)
		}
		if now != was {
			want = append(want, midi.Control(0, 48+k, now))
		}
	}
	want = append(want, midi.NoteOn(0, layerANote, 0), midi.NoteOn(0, layerBNote, 127))
	if sent := r.d.took(); !sameBytes(sent, want) {
		t.Errorf("turned to page 2, the X-Touch Mini was sent\n%v\nwant\n%v", sent, want)
	}
	r.d.play(midi.Control(0, 16, 1))
	r.d.play(midi.Control(0, 17, 1))
	st := r.tick().Boards[r.board]
	if len(st.Held) != 1 || int(st.Held[Channel{Light: r.dome, Offset: 9}]) != int(stage[8])+3 {
		t.Errorf("knobs 1 and 2 turned on page 2, the board holds %v", st.Held)
	}
	if st := r.press(padNote1).Boards[r.board]; !slices.Equal(st.On, []int{8}) {
		t.Errorf("the first pad pressed on page 2 turns on %v", st.On)
	}
	if st := r.press(layerANote).Boards[r.board]; st.Page != 0 {
		t.Errorf("Layer A pressed, the board says page %d", st.Page)
	}
}

// TestTheFaderPicksUpTheGrandMasterAndHandsItOverOnce: it does nothing
// until it comes near the master, then follows, played at once; a show
// set meanwhile plays on what it set; once it rests, the master is
// handed over, once, and the show holding it lets it go. The master
// moved on screen, the fader picks it up afresh as it passes it.
func TestTheFaderPicksUpTheGrandMasterAndHandsItOverOnce(t *testing.T) {
	r := newPlayRig(t)
	master := func() float64 { return r.tick().Values[r.mixer] }
	for _, v := range []byte{0, 64} {
		r.d.play(fader(v))
		if got := master(); got != 1 {
			t.Errorf("the fader at %d, below the master, moved it to %v", v, got)
		}
	}
	r.d.play(fader(127))
	r.d.play(fader(64))
	if got := master(); got != 64.0/127 {
		t.Fatalf("picked up at the top and brought to 64, the master is %v", got)
	}
	if sent := r.d.took(); len(sent) != 0 {
		t.Errorf("the master moved, the X-Touch Mini was sent %v", sent)
	}
	r.e.Set(r.s)
	if got := master(); got != 64.0/127 {
		t.Errorf("a show set meanwhile, the master is %v", got)
	}
	if got := r.e.Masters(); len(got) != 0 {
		t.Fatalf("while the fader may move on, the engine hands over %+v", got)
	}
	for range 40 {
		r.tick()
	}
	got := r.e.Masters()
	if len(got) != 1 || got[0] != (Master{Module: r.mixer, Value: 64.0 / 127}) {
		t.Fatalf("the fader rested, the engine hands over %+v", got)
	}
	for range 80 {
		r.tick()
	}
	if got := r.e.Masters(); len(got) != 0 {
		t.Errorf("handed over once, the engine hands over %+v again", got)
	}
	r.s.Module(r.mixer).Value = 64.0 / 127
	r.e.Set(r.s)
	if len(r.e.masters) != 0 {
		t.Error("the show holds the master, and the engine still keeps it")
	}
	// Moved on screen: the fader picks it up again only as it passes it.
	r.e.SetValue(r.mixer, 0.2)
	r.d.play(fader(70))
	if got := master(); got != 0.2 {
		t.Errorf("the master at 0.2 on screen, the fader above it moved it to %v", got)
	}
	r.d.play(fader(10))
	if got := master(); got != 10.0/127 {
		t.Errorf("the fader passed the master, which is %v", got)
	}
	// Moved on screen again before the fader rests: that has the last
	// word, and nothing is handed over.
	r.e.SetValue(r.mixer, 0.5)
	for range 80 {
		r.tick()
	}
	if got := r.e.Masters(); len(got) != 0 || r.tick().Values[r.mixer] != 0.5 {
		t.Errorf("moved on screen meanwhile, the engine hands over %+v", got)
	}
}

// TestAControllerPlayingTheBoardStillPlaysItsWires: a button wired into
// a Toggle flips it, and its knob's output moves, but the Toggle wired
// into its light lights it not: the Board does.
func TestAControllerPlayingTheBoardStillPlaysItsWires(t *testing.T) {
	r := newPlayRig(t)
	tg := r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: tg, Kind: show.Toggle})
	r.s.Wires = append(r.s.Wires,
		show.Wire{ID: r.s.NewID("w"), From: show.End{Module: r.ctl, Port: "button1"}, To: show.End{Module: tg, Port: "in"}},
		show.Wire{ID: r.s.NewID("w"), From: show.End{Module: tg, Port: "out"}, To: show.End{Module: r.ctl, Port: "light:button1"}})
	r.set()
	r.tick()
	r.d.took()
	r.d.play(midi.Control(0, 16, 5))
	if live := r.press(padNote1); live.Values[tg] != 1 {
		t.Errorf("pad 1 pressed, the toggle wired to it is at %v", live.Values[tg])
	}
	if v, _ := r.e.values[portKey{r.ctl, "knob1"}].(float64); math.Abs(v-0.05) > 1e-9 {
		t.Errorf("knob 1 turned, its output carries %v", v)
	}
	r.tick()
	if sent := r.d.took(); len(sent) != 0 {
		t.Errorf("the toggle on, and pad 1 with no scene, the X-Touch Mini was sent %v", sent)
	}
}

// TestForgetLetsGoOfThePageAndTheMasterMoved: a new show starts on
// page 1, at its own master, and the controller is lit afresh.
func TestForgetLetsGoOfThePageAndTheMasterMoved(t *testing.T) {
	r := newPlayRig(t)
	r.press(layerBNote)
	r.d.play(fader(127))
	r.d.play(fader(20))
	r.tick()
	r.d.took()
	r.e.Forget()
	r.e.Set(r.s)
	live := r.tick()
	if st := live.Boards[r.board]; st.Page != 0 || live.Values[r.mixer] != 1 {
		t.Errorf("forgotten, the board is on page %d and the master at %v", st.Page, live.Values[r.mixer])
	}
	board := func(n int) int {
		if n == layerANote {
			return 127
		}
		return 0
	}
	if sent := r.d.took(); !sameBytes(sent, allLights(dark, board)) {
		t.Errorf("forgotten, the X-Touch Mini was sent %v", sent)
	}
	for range 80 {
		r.tick()
	}
	if got := r.e.Masters(); len(got) != 0 {
		t.Errorf("forgotten, the engine hands over %+v", got)
	}
}

// TestAControllerRemovedOrOfAnotherLayoutPlaysTheBoardNoMore, and lets
// go of its flash; a second Board played from the same controller is
// not played from it.
func TestAControllerRemovedOrOfAnotherLayoutPlaysTheBoardNoMore(t *testing.T) {
	r := newPlayRig(t)
	other := r.s.NewID("m")
	r.s.Modules = append(r.s.Modules, show.Module{ID: other, Kind: show.Board, Name: "Second", PlayedFrom: r.ctl})
	r.set()
	r.d.play(midi.NoteOn(0, flashNote, 127))
	r.tick()
	live := r.tick()
	if live.Boards[r.board].Remote != r.ctl || live.Boards[other].Remote != "" || !live.Overrides[r.mixer][1] {
		t.Fatalf("the first board is played from %q, the second from %q", live.Boards[r.board].Remote, live.Boards[other].Remote)
	}
	r.s.Module(r.ctl).Layout = ""
	r.set()
	r.tick()
	live = r.tick()
	if live.Boards[r.board].Remote != "" || live.Overrides[r.mixer][1] {
		t.Error("its layout left, the controller plays the board on, or flashes")
	}
	r.s.Module(r.ctl).Layout = controller.XTouchMiniID
	r.set()
	r.d.play(midi.NoteOn(0, flashNote, 127))
	r.tick()
	if !r.tick().Overrides[r.mixer][1] {
		t.Fatal("its layout back, the controller does not flash")
	}
	r.s.RemoveModule(r.ctl)
	r.set()
	r.tick()
	live = r.tick()
	if live.Boards[r.board].Remote != "" || live.Overrides[r.mixer][1] || len(r.e.remotes) != 0 {
		t.Error("removed, the controller plays the board on, or flashes")
	}
}
