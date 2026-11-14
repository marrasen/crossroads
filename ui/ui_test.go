package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/enttec"
	"github.com/marrasen/gunim/audio/capture"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"
)

// TestAWindowShowsTheShowAndItsFadersPlay: the four tabs build, the
// stage glows as the engine plays, and a fader dragged on the Faders tab
// asks to set its value.
func TestAWindowShowsTheShowAndItsFadersPlay(t *testing.T) {
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	st := app.State{Show: show.Start()}
	// The Board stands in front of the Faders as a show starts.
	st.Show.Layout.Show(show.TabFaders)
	if err := c.Mount(gunim.Root, View, View, st, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	run := func(n int) {
		for range n {
			w.Frame(time.Second / 60)
		}
	}
	run(3)
	e := engine.New(nil)
	defer e.Close()
	e.Set(st.Show)
	if err := c.Patch(app.LiveTopic, e.Tick()); err != nil {
		t.Fatal(err)
	}
	run(3)
	// The frame is the root; its tabs are what its dock holds.
	f := frameOf(t, w)
	shown := f.d.Arrangement()
	for _, tb := range show.Tabs {
		if f.tabs[tb] == nil && shown.Shows(tb) {
			t.Errorf("tab %s shows and was not built", tb)
		}
	}
	sg, ok := f.tabs[show.TabStage].(*stage)
	if !ok || sg.u[0] == 0 {
		t.Fatal("the stage shows nothing the engine played")
	}
	fd, ok := f.tabs[show.TabFaders].(*faders)
	if !ok || len(fd.ids) == 0 {
		t.Fatal("the Faders tab has no faders")
	}
	// Drag the first fader, the mixer's master, down.
	s := fd.strips[fd.ids[0]].s
	ui := frameUI(t, w)
	r, ok := ui.Bounds(s)
	if !ok {
		t.Fatal("the fader is not drawn")
	}
	from := geom.Pt(r.Center().X, r.Min.Y+4)
	w.Input(input.PointerMove{Pos: from})
	w.Input(input.PointerDown{Pos: from, Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerMove{Pos: from.Add(geom.Pt(0, 100))})
	w.Input(input.PointerUp{Pos: from.Add(geom.Pt(0, 100)), Button: input.ButtonPrimary})
	run(2)
	var done bool
	for len(c.Intents()) > 0 {
		if sv, ok := (<-c.Intents()).Intent.(app.SetValue); ok && sv.Done && sv.Value < 1 {
			done = true
		}
	}
	if !done {
		t.Error("a fader dragged down asked to keep no value")
	}
}

// testWindow is an offscreen window that paints every frame, as these
// tests ask the UI where its nodes were drawn.
func testWindow(t *testing.T, size geom.Size) *gunim.Window {
	t.Helper()
	w := gunimtest.New(t, size, nil)
	w.PaintWhenLooked(false)
	return w
}

// uiCall runs on the window's UI goroutine with the frame.
type uiCall func(f *frame, u *gunim.UI)

// onUI runs fn with w's frame and UI, and a frame.
func onUI(t *testing.T, w *gunim.Window, fn func(f *frame, u *gunim.UI)) {
	t.Helper()
	if err := w.Client().Patch(View, uiCall(fn)); err != nil {
		t.Fatal(err)
	}
	w.Frame(time.Second / 60)
}

// frameOf is w's frame.
func frameOf(t *testing.T, w *gunim.Window) *frame {
	t.Helper()
	var f *frame
	onUI(t, w, func(fr *frame, _ *gunim.UI) { f = fr })
	if f == nil {
		t.Fatal("no frame")
	}
	return f
}

// frameUI is w's UI, for asking where nodes stand.
func frameUI(t *testing.T, w *gunim.Window) *gunim.UI {
	t.Helper()
	var ui *gunim.UI
	onUI(t, w, func(_ *frame, u *gunim.UI) { ui = u })
	return ui
}

// TestLeavingAShowNotSavedAsksFirst: Ctrl+N asks, and only the answer
// to leave it asks the application for a new show.
func TestLeavingAShowNotSavedAsksFirst(t *testing.T) {
	w := testWindow(t, geom.Sz(1000, 700))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: show.Start(), Unsaved: true}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	news := func() int {
		n := 0
		for len(c.Intents()) > 0 {
			if _, ok := (<-c.Intents()).Intent.(app.New); ok {
				n++
			}
		}
		return n
	}
	w.Input(input.KeyPress{Key: input.KeyN, Mods: input.ModControl})
	w.Frame(time.Second / 60)
	f := frameOf(t, w)
	if f.ask == nil || news() != 0 {
		t.Fatal("Ctrl+N left a show not saved without asking")
	}
	var in gunim.Intent
	onUI(t, w, func(f *frame, u *gunim.UI) { in = f.ask.OnAccept(u) })
	if _, ok := in.(app.New); !ok {
		t.Errorf("leaving it asks for %#v, not a new show", in)
	}
}

// TestTheBeatModulesShowTheirBeat: a Tempo's box holds a tap button
// that taps it, a Beat's says the tempo it hears, and an Audio in's
// offers the inputs.
func TestTheBeatModulesShowTheirBeat(t *testing.T) {
	listInputs = func() ([]capture.Device, error) { return []capture.Device{{ID: "mic", Name: "Microphone"}}, nil }
	defer func() { listInputs = capture.Devices }()
	s := show.Start()
	add := func(k show.Kind, at geom.Point) string {
		m := show.Module{ID: s.NewID("m"), Kind: k, Name: k.Title(), At: at, BPM: 128, Every: 2}
		s.Modules = append(s.Modules, m)
		return m.ID
	}
	in, bt, tp, pl := add(show.AudioIn, geom.Pt(300, 260)), add(show.Beat, geom.Pt(300, 380)), add(show.Tempo, geom.Pt(560, 300)), add(show.Pulse, geom.Pt(560, 460))
	s.Layout.Show(show.TabRoutes)
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	live := engine.Live{Values: map[string]float64{pl: 0.5}, Clocks: map[string]engine.Count{bt: {BPM: 124, Beats: 3.2, Sure: true}},
		Says: map[string]string{in: "Microphone"}}
	if err := c.Patch(app.LiveTopic, live); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		w.Frame(time.Second / 60)
	}
	f := frameOf(t, w)
	r, ok := f.tabs[show.TabRoutes].(*routes)
	if !ok {
		t.Fatal("no Routes tab")
	}
	m := func(id string) *show.Module { return r.st.Show.Module(id) }
	for id, want := range map[string]string{in: "Microphone", bt: "124.0 BPM", pl: "Every 2 beats"} {
		if got := r.detail(m(id)); got != want {
			t.Errorf("%s says %q, want %q", m(id).Name, got, want)
		}
	}
	if d := r.inputs[in]; d == nil || len(r.heard) != 1 {
		t.Fatal("the Audio in offers no inputs")
	}
	if fd := r.bpms[tp]; fd == nil || fd.Value() != 128 {
		t.Fatal("the Tempo's box shows no tempo of 128")
	}
	// Tap the tap button.
	ui := frameUI(t, w)
	tap := r.taps[tp]
	if tap == nil {
		t.Fatal("the Tempo's box has no tap button")
	}
	b, ok := ui.Bounds(tap)
	if !ok {
		t.Fatal("the tap button is not drawn")
	}
	w.Input(input.PointerMove{Pos: b.Center()})
	w.Input(input.PointerDown{Pos: b.Center(), Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: b.Center(), Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var tapped bool
	for len(c.Intents()) > 0 {
		if tp2, ok := (<-c.Intents()).Intent.(app.Tap); ok && tp2.Module == tp {
			tapped = true
		}
	}
	if !tapped {
		t.Error("the tap button tapped nothing")
	}
}

// TestThePlayerTabListsTheSongsAndPlaysOneDoubleClicked: a row for each
// song with its tempo, the play button turning to pause as a song
// plays, and the song bar moving the song where it is clicked.
func TestThePlayerTabListsTheSongsAndPlaysOneDoubleClicked(t *testing.T) {
	s := show.Start()
	for i, title := range []string{"Opener", "Closer"} {
		s.Songs = append(s.Songs, show.Song{ID: s.NewID("song"), Path: title + ".wav", Title: title,
			Scan: &show.Scan{Length: 200, BPM: 120 + float64(i)*8, LUFS: -12, Loud: true, TruePeak: -1}})
	}
	s.Songs[1].BPM = 100
	s.Layout.Show(show.TabPlayer)
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s, Songs: map[string]string{}}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	f := frameOf(t, w)
	p, ok := f.tabs[show.TabPlayer].(*playlist)
	if !ok {
		t.Fatal("no Player tab")
	}
	if len(p.list.ids) != 2 || p.list.rows[s.Songs[0].ID].bpm.Value() != 120 || p.list.rows[s.Songs[1].ID].bpm.Value() != 100 {
		t.Fatal("the rows do not show each song's tempo")
	}
	if got := songNote(&s.Songs[0], ""); got != "3:20 · 120.0 BPM found · -12.0 LUFS, plays -2.0 dB" {
		t.Errorf("the first song's row says %q", got)
	}
	ui := frameUI(t, w)
	r, ok := ui.Bounds(p.list)
	if !ok {
		t.Fatal("the songs are not drawn")
	}
	at := geom.Pt(r.Min.X+60, r.Min.Y+songH*1.5)
	w.Input(input.PointerMove{Pos: at})
	w.Input(input.PointerDown{Pos: at, Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: at, Button: input.ButtonPrimary})
	w.Input(input.PointerDown{Pos: at, Button: input.ButtonPrimary, Clicks: 2})
	w.Input(input.PointerUp{Pos: at, Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var played string
	for len(c.Intents()) > 0 {
		if ps, ok := (<-c.Intents()).Intent.(app.PlaySong); ok {
			played = ps.Song
		}
	}
	if played != s.Songs[1].ID {
		t.Errorf("a double click on the second song played %q", played)
	}
	live := engine.Live{Playing: engine.Playing{Song: s.Songs[1].ID, Pos: 50 * time.Second, Len: 200 * time.Second, Playing: true}}
	if err := c.Patch(app.LiveTopic, live); err != nil {
		t.Fatal(err)
	}
	w.Frame(time.Second / 60)
	if p.play.Icon != icon.Pause {
		t.Error("playing, the play button does not offer to pause")
	}
	pr, _ := ui.Bounds(p)
	b := p.bar()
	click := pr.Min.Add(geom.Pt(b.Min.X+b.Size().W*0.75, b.Center().Y))
	w.Input(input.PointerMove{Pos: click})
	w.Input(input.PointerDown{Pos: click, Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: click, Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var seek time.Duration = -1
	for len(c.Intents()) > 0 {
		if sk, ok := (<-c.Intents()).Intent.(app.SeekSong); ok {
			seek = sk.At
		}
	}
	if seek < 149*time.Second || seek > 151*time.Second {
		t.Errorf("a click three quarters along the bar moves the song to %v, want 150 s", seek)
	}
}

// TestTheControllersTabShowsEachControllerAndLearns: a card for a
// Controller and a MIDI out, the ports offered, the controls' values,
// and the learn button asking to learn.
func TestTheControllersTabShowsEachControllerAndLearns(t *testing.T) {
	was := listMIDI
	listMIDI = func(outputs bool) []string {
		if outputs {
			return []string{"Synth"}
		}
		return []string{"X-TOUCH MINI", "nanoKONTROL"}
	}
	defer func() { listMIDI = was }()
	s := show.Start()
	ctl, out := s.NewID("m"), s.NewID("m")
	s.Modules = append(s.Modules,
		show.Module{ID: ctl, Kind: show.Controller, Name: "Desk", Device: "nanoKONTROL",
			Controls: []show.Control{{ID: "c1", Name: "Control 7", Kind: show.Slider, CC: 7, Note: -1}}},
		show.Module{ID: out, Kind: show.MIDIOut, Name: "To the synth", Device: "Gone", Channel: 3, Note: 60, CC: 1})
	s.Layout.Show(show.TabMIDI)
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	live := engine.Live{Desks: map[string]engine.Desk{ctl: {Says: "nanoKONTROL", Values: map[string]float64{"c1": 0.5}}}}
	if err := c.Patch(app.LiveTopic, live); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	f := frameOf(t, w)
	tab, ok := f.tabs[show.TabMIDI].(*controllers)
	if !ok {
		t.Fatal("no Controllers tab")
	}
	desk, synth := tab.list.cards[ctl], tab.list.cards[out]
	if desk == nil || synth == nil {
		t.Fatal("a card is missing")
	}
	if len(desk.devices) != 2 || desk.device.Selected() != 1 {
		t.Errorf("the controller offers %v, chosen %d", desk.devices, desk.device.Selected())
	}
	if len(synth.devices) != 2 || synth.devices[1] != "Gone" || synth.channel.Value() != 4 {
		t.Errorf("the MIDI out offers %v on channel %v", synth.devices, synth.channel.Value())
	}
	if desk.learn.Disabled || desk.forget["c1"] == nil {
		t.Fatal("a controller of learned controls cannot learn, or forget")
	}
	ui := frameUI(t, w)
	b, ok := ui.Bounds(desk.learn)
	if !ok {
		t.Fatal("the learn button is not drawn")
	}
	w.Input(input.PointerMove{Pos: b.Center()})
	w.Input(input.PointerDown{Pos: b.Center(), Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: b.Center(), Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var learn bool
	for len(c.Intents()) > 0 {
		if l, ok := (<-c.Intents()).Intent.(app.Learn); ok && l.Module == ctl && l.On {
			learn = true
		}
	}
	if !learn {
		t.Error("the learn button asked to learn nothing")
	}
}

// TestAControllerInStandardModeSaysHowToSwitchToMCMode: hold MC while
// plugging it in, ahead of what its port says.
func TestAControllerInStandardModeSaysHowToSwitchToMCMode(t *testing.T) {
	d := engine.Desk{Says: "X-TOUCH MINI", Last: "Control ch 11 cc 3 64"}
	if got := deskSays(d); got != "X-TOUCH MINI · last heard Control ch 11 cc 3 64" {
		t.Errorf("in MC mode, the card says %q", got)
	}
	d.Standard = true
	if got := deskSays(d); !strings.HasPrefix(got, "In Standard mode, it wants MC mode: hold MC while plugging it in · X-TOUCH MINI") {
		t.Errorf("in Standard mode, the card says %q", got)
	}
}

// TestTheLivePageHoldsBlackoutWhileItIsHeld: the pad asks for blackout
// as it is pressed and lets go as it is let go; a look's pad brings it
// up; and the Monitor tab builds.
func TestTheLivePageHoldsBlackoutWhileItIsHeld(t *testing.T) {
	s := show.Start()
	look := s.NewID("m")
	s.Modules = append(s.Modules, show.Module{ID: look, Kind: show.Look, Name: "Warm", Lights: []string{s.Rig.Lights[0].ID}})
	s.Layout.Show(show.TabLive)
	s.Layout.Show(show.TabWatch)
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	f := frameOf(t, w)
	lt, ok := f.tabs[show.TabLive].(*liveTab)
	if !ok {
		t.Fatal("no Live tab")
	}
	lp := lt.page
	if _, ok := f.tabs[show.TabWatch].(*monitor); !ok {
		t.Fatal("no Monitor tab")
	}
	ui := frameUI(t, w)
	at, _ := ui.Bounds(lp)
	press := func(label string) (down, up []gunim.Intent) {
		var r geom.Rect
		for _, pd := range lp.pads {
			if pd.label == label {
				r = pd.r
			}
		}
		if r.Empty() {
			t.Fatalf("no %s pad among %d", label, len(lp.pads))
		}
		p := at.Min.Add(r.Center())
		w.Input(input.PointerMove{Pos: p})
		w.Input(input.PointerDown{Pos: p, Button: input.ButtonPrimary, Clicks: 1})
		w.Frame(time.Second / 60)
		for len(c.Intents()) > 0 {
			down = append(down, (<-c.Intents()).Intent)
		}
		w.Input(input.PointerUp{Pos: p, Button: input.ButtonPrimary})
		w.Frame(time.Second / 60)
		for len(c.Intents()) > 0 {
			up = append(up, (<-c.Intents()).Intent)
		}
		return down, up
	}
	down, up := press("Blackout")
	if len(down) != 1 || down[0] != (app.Override{Module: lp.ids[0], Which: engine.Blackout, On: true}) {
		t.Errorf("pressed, blackout asks %v", down)
	}
	if len(up) != 1 || up[0] != (app.Override{Module: lp.ids[0], Which: engine.Blackout}) {
		t.Errorf("let go, blackout asks %v", up)
	}
	down, _ = press("Warm")
	if len(down) != 1 || down[0] != (app.SetValue{Module: look, Value: 1, Done: true}) {
		t.Errorf("the look's pad asks %v", down)
	}
}

// TestARoutesCardListsItsRoutesAndTurnsThemOff: each route's name and
// what it does, and its on box asks to turn it off.
func TestARoutesCardListsItsRoutesAndTurnsThemOff(t *testing.T) {
	s := show.Start()
	rt := s.NewID("m")
	s.Modules = append(s.Modules, show.Module{ID: rt, Kind: show.Routes, Name: "Pads", Routes: []routing.Route{
		{ID: "a", Name: "Red pad", Enabled: true, Input: routing.Input{DeviceName: "Pads", Channel: 1, Type: routing.NoteOn, Data1: routing.One(36)},
			Output: routing.Output{Kind: routing.ToTimer, Command: routing.TimerStart}},
	}})
	s.Layout.Show(show.TabMIDI)
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	tab, _ := frameOf(t, w).tabs[show.TabMIDI].(*controllers)
	cd := tab.list.cards[rt]
	if cd == nil || cd.ons["a"] == nil || !cd.ons["a"].Checked() {
		t.Fatal("the routes card has no on box for its route")
	}
	if got := s.Modules[len(s.Modules)-1].Routes[0].Says(); got != "Pads Note ON 36 on channel 1 → start a timer" {
		t.Errorf("the route says %q", got)
	}
	ui := frameUI(t, w)
	b, ok := ui.Bounds(cd.ons["a"])
	if !ok {
		t.Fatal("the on box is not drawn")
	}
	w.Input(input.PointerMove{Pos: b.Center()})
	w.Input(input.PointerDown{Pos: b.Center(), Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: b.Center(), Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var off bool
	for len(c.Intents()) > 0 {
		if in, ok := (<-c.Intents()).Intent.(app.SetRouteOn); ok && in.Module == rt && in.Route == "a" && !in.On {
			off = true
		}
	}
	if !off {
		t.Error("the on box asked nothing")
	}
}

// TestThePatternEditorDrawsAClipAndTriesThePattern: a drag along an
// empty lane draws a clip a beat and a half long, which goes into the
// show; Try plays the pattern while held.
func TestThePatternEditorDrawsAClipAndTriesThePattern(t *testing.T) {
	s := show.Start()
	light := s.Rig.Lights[0].ID
	s.Patterns = []pattern.Pattern{{ID: "p", Name: "Chase", Bars: 1, Lanes: []pattern.Lane{{ID: "l", FixtureID: light}}}}
	s.Layout.Show(show.TabDraw)
	w := testWindow(t, geom.Sz(1600, 900))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	tab, ok := frameOf(t, w).tabs[show.TabDraw].(*patternsTab)
	if !ok || tab.body == nil || tab.body.p.ID != "p" {
		t.Fatal("the Patterns tab does not edit the pattern")
	}
	ui := frameUI(t, w)
	r, ok := ui.Bounds(tab.body.timeline)
	if !ok {
		t.Fatal("the grid is not drawn")
	}
	bw := r.Size().W / 4
	y := r.Min.Y + laneH/2
	from, to := geom.Pt(r.Min.X+bw*0.1, y), geom.Pt(r.Min.X+bw*1.5, y)
	w.Input(input.PointerMove{Pos: from})
	w.Input(input.PointerDown{Pos: from, Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerMove{Pos: to})
	w.Input(input.PointerUp{Pos: to, Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var saved *pattern.Pattern
	for len(c.Intents()) > 0 {
		if sp, ok := (<-c.Intents()).Intent.(app.SavePattern); ok {
			saved = &sp.Pattern
		}
	}
	if saved == nil || len(saved.Lanes[0].Clips) != 1 {
		t.Fatalf("the drawing saved %+v", saved)
	}
	if cl := saved.Lanes[0].Clips[0]; cl.Start != 0 || cl.Length != 1.5 || cl.Kind != pattern.KindColour {
		t.Errorf("the clip drawn is %+v, want a colour from 0 lasting 1.5 beats", cl)
	}
	try := findButton(tab.body.root, "Try")
	if try == nil {
		t.Fatal("no Try button")
	}
	b, _ := ui.Bounds(try)
	w.Input(input.PointerMove{Pos: b.Center()})
	w.Input(input.PointerDown{Pos: b.Center(), Button: input.ButtonPrimary, Clicks: 1})
	w.Frame(time.Second / 60)
	w.Input(input.PointerUp{Pos: b.Center(), Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var on, off bool
	for len(c.Intents()) > 0 {
		if tp, ok := (<-c.Intents()).Intent.(app.TestPattern); ok {
			if tp.On {
				on = true
			} else {
				off = on
			}
		}
	}
	if !on || !off {
		t.Errorf("Try held and let go tried %v and stopped %v", on, off)
	}
}

// findButton finds the button labelled label among n and what it holds.
func findButton(n gunim.Node, label string) *widget.Button {
	switch b := n.(type) {
	case *holdButton:
		if b.Label == label {
			return b.Button
		}
	case *widget.Button:
		if b.Label == label {
			return b
		}
	}
	if c, ok := n.(gunim.Composite); ok {
		for _, k := range c.Children() {
			if b := findButton(k, label); b != nil {
				return b
			}
		}
	}
	return nil
}

// TestTheBoardTabPicksALightAndHoldsAChannel: the tab builds with a pad
// for the dome and no faders; a click on the pad picks it, Ctrl+click
// adds or takes it out; once picked, a fader for each of its channels,
// and the green one dragged up asks to hold green.
func TestTheBoardTabPicksALightAndHoldsAChannel(t *testing.T) {
	s := show.Start()
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	e := engine.New(nil)
	defer e.Close()
	e.Set(s)
	tick := func() {
		if err := c.Patch(app.LiveTopic, e.Tick()); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			w.Frame(time.Second / 60)
		}
	}
	tick()
	tab, ok := frameOf(t, w).tabs[show.TabBoard].(*boardTab)
	if !ok {
		t.Fatal("no Board tab in front")
	}
	b := tab.page
	dome := s.Rig.Lights[0].ID
	if b.board == "" || b.mixer == "" || b.out == "" || len(b.pads) != 1 || len(b.faders) != 0 {
		t.Fatalf("the tab plays board %q into %q and %q, with %d pads and %d faders", b.board, b.mixer, b.out, len(b.pads), len(b.faders))
	}
	ui := frameUI(t, w)
	at, _ := ui.Bounds(b)
	click := func(mods input.Mods) []gunim.Intent {
		p := at.Min.Add(b.pads[0].r.Center())
		w.Input(input.PointerMove{Pos: p})
		w.Input(input.PointerDown{Pos: p, Button: input.ButtonPrimary, Mods: mods, Clicks: 1})
		w.Input(input.PointerUp{Pos: p, Button: input.ButtonPrimary, Mods: mods})
		w.Frame(time.Second / 60)
		var got []gunim.Intent
		for len(c.Intents()) > 0 {
			got = append(got, (<-c.Intents()).Intent)
		}
		return got
	}
	got := click(0)
	if len(got) != 1 {
		t.Fatalf("a click on the dome asks %v", got)
	}
	if pk, ok := got[0].(app.Pick); !ok || pk.Module != b.board || len(pk.Lights) != 1 || pk.Lights[0] != dome || pk.Toggle {
		t.Errorf("a click on the dome asks %#v", got[0])
	}
	if got := click(input.ModControl); len(got) != 1 || !got[0].(app.Pick).Toggle {
		t.Errorf("Ctrl+click on the dome asks %v", got)
	}
	e.Pick(b.board, []string{dome}, false)
	tick()
	if len(b.faders) != 9 || b.faders[1].Name != "Green" {
		t.Fatalf("the dome picked has %d faders", len(b.faders))
	}
	if !b.dim(b.faders[1].s) {
		t.Error("a fader nothing holds is not drawn dim")
	}
	r, ok := ui.Bounds(b.faders[1].s)
	if !ok {
		t.Fatal("the green fader is not drawn")
	}
	from := geom.Pt(r.Center().X, r.Max.Y-4)
	w.Input(input.PointerMove{Pos: from})
	w.Input(input.PointerDown{Pos: from, Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerMove{Pos: geom.Pt(from.X, r.Min.Y-20)})
	w.Input(input.PointerUp{Pos: geom.Pt(from.X, r.Min.Y-20), Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var held *app.Hold
	for len(c.Intents()) > 0 {
		if h, ok := (<-c.Intents()).Intent.(app.Hold); ok {
			held = &h
		}
	}
	if held == nil || held.Module != b.board || held.Value != 255 || len(held.Channels) != 1 ||
		held.Channels[0] != (engine.Channel{Light: dome, Offset: 2}) {
		t.Fatalf("green dragged to the top asks %+v", held)
	}
	e.Hold(held.Module, held.Channels, byte(held.Value))
	tick()
	if b.dim(b.faders[1].s) || b.faders[1].value != 255 || b.clear.Disabled {
		t.Error("a fader held is drawn dim, or Clear has nothing to let go of")
	}
}

// TestADMXOutOffersItsPortAndAddress: a USB box offers Find it and the
// ports found, one gone kept; Art-Net offers the address, sent as it is
// typed, and the universe.
func TestADMXOutOffersItsPortAndAddress(t *testing.T) {
	s := show.Start()
	out := ""
	for i := range s.Modules {
		if s.Modules[i].Kind == show.DMXOut {
			out = s.Modules[i].ID
			s.Modules[i].Out = show.Output{Device: "enttec", Port: "COM9"}
		}
	}
	// Routes and the Board both in front, each in a group of its own.
	s.Layout.Move(show.TabBoard, dock.Target{At: show.TabFaders, Index: -1})
	s.Layout.Show(show.TabBoard)
	s.Layout.Show(show.TabRoutes)
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	st := app.State{Show: s, Ports: []enttec.PortInfo{{Name: "COM3", Product: "DMX USB PRO"}}}
	if err := c.Mount(gunim.Root, View, View, st, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	f := frameOf(t, w)
	r, ok := f.tabs[show.TabRoutes].(*routes)
	if !ok || r.outputs[out] == nil {
		t.Fatal("the DMX out's box has no fields")
	}
	fs := r.outputs[out]
	labels := make([]string, 0, len(fs.port.Items()))
	for _, it := range fs.port.Items() {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, "|") != "Find it|COM3 — DMX USB PRO|COM9 (not plugged in)" || fs.port.Selected() != 2 || fs.more.shown != fs.usb {
		t.Fatalf("the port list offers %v, chosen %d", labels, fs.port.Selected())
	}
	if in, ok := fs.port.OnChange(1, nil).(app.SetOutput); !ok || in.Out != (show.Output{Device: "enttec", Port: "COM3"}) {
		t.Errorf("COM3 chosen asks %#v", in)
	}
	if in, ok := fs.port.OnChange(0, nil).(app.SetOutput); !ok || in.Out.Port != "" {
		t.Errorf("Find it chosen asks %#v", in)
	}
	s.Module(out).Out = show.Output{Device: "artnet", Universe: 4}
	st.Show = s
	if err := c.Publish(app.ShowTopic, st); err != nil {
		t.Fatal(err)
	}
	w.Frame(time.Second / 60)
	if fs.more.shown != fs.net || fs.uni.Value() != 4 {
		t.Fatalf("Art-Net shows %v, universe %v", fs.more.shown, fs.uni.Value())
	}
	if in, ok := fs.host.OnCommit(" 10.0.0.5 ", nil).(app.SetOutput); !ok || in.Out != (show.Output{Device: "artnet", Host: "10.0.0.5", Universe: 4}) {
		t.Errorf("an address typed asks %#v", in)
	}
	if in, ok := fs.uni.OnCommit(7, nil).(app.SetOutput); !ok || in.Out.Universe != 7 {
		t.Errorf("universe 7 typed asks %#v", in)
	}
	if b := f.tabs[show.TabBoard].(*boardTab).page; b.output.module != out || b.output.more.shown != b.output.net {
		t.Error("the Board tab's output row is not the DMX out's")
	}
}

// TestABoardAddedToAShowWhoseOutputIsSetShowsTheOutput: a show with no
// Board, its DMX out sending to Art-Net, gets a Board; the tab then
// shows the output's fields, which were not in the tree before.
func TestABoardAddedToAShowWhoseOutputIsSetShowsTheOutput(t *testing.T) {
	s := show.Start()
	var board show.Module
	for _, m := range s.Modules {
		if m.Kind == show.Board {
			board = m
		}
	}
	wires := append([]show.Wire(nil), s.Wires...)
	s.RemoveModule(board.ID)
	for i := range s.Modules {
		if s.Modules[i].Kind == show.DMXOut {
			s.Modules[i].Out = show.Output{Device: "artnet", Host: "10.0.0.2", Universe: 1}
		}
	}
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	tab, ok := frameOf(t, w).tabs[show.TabBoard].(*boardTab)
	if !ok || tab.page.board != "" {
		t.Fatal("the Board tab is not in front, or plays a board the show does not have")
	}
	with := s.Clone()
	with.Modules = append(with.Modules, board)
	with.Wires = wires
	if err := c.Publish(app.ShowTopic, app.State{Show: with}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w.Frame(time.Second / 60)
	}
	b := tab.page
	if b.board == "" || b.out == "" {
		t.Fatalf("the board added, the tab plays board %q and output %q", b.board, b.out)
	}
	if _, ok := frameUI(t, w).Bounds(b.output.root); !ok {
		t.Error("the output's fields are not drawn")
	}
}

// TestTheBoardTabRecordsOntoAPadAndPlaysIt: Record and then a pad ask
// the board to wait and to press that pad; Escape, while it waits,
// gives up; a pad's look follows what the engine says is on; and its
// menu deletes its scene.
func TestTheBoardTabRecordsOntoAPadAndPlaysIt(t *testing.T) {
	s := show.Start()
	dome := s.Rig.Lights[0].ID
	board := ""
	for i := range s.Modules {
		if s.Modules[i].Kind == show.Board {
			board = s.Modules[i].ID
			s.Modules[i].SetScene(show.Scene{Pad: 2, Name: "Red wash", Values: map[string]map[int]byte{dome: {1: 255}}})
		}
	}
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	e := engine.New(nil)
	defer e.Close()
	e.Set(s)
	tick := func() {
		if err := c.Patch(app.LiveTopic, e.Tick()); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			w.Frame(time.Second / 60)
		}
	}
	tick()
	tab, ok := frameOf(t, w).tabs[show.TabBoard].(*boardTab)
	if !ok {
		t.Fatal("no Board tab in front")
	}
	b := tab.page
	if !b.scenes[2].full || b.scenes[2].name != "Red wash" || b.scenes[0].full {
		t.Fatalf("the pads show %+v", b.scenes[:3])
	}
	ui := frameUI(t, w)
	at, _ := ui.Bounds(b)
	sent := func() []gunim.Intent {
		w.Frame(time.Second / 60)
		var got []gunim.Intent
		for len(c.Intents()) > 0 {
			got = append(got, (<-c.Intents()).Intent)
		}
		return got
	}
	press := func(p geom.Point, button input.Button) []gunim.Intent {
		w.Input(input.PointerMove{Pos: p})
		w.Input(input.PointerDown{Pos: p, Button: button, Clicks: 1})
		w.Input(input.PointerUp{Pos: p, Button: button})
		return sent()
	}
	r, ok := ui.Bounds(b.record)
	if !ok {
		t.Fatal("Record is not drawn")
	}
	if got := press(r.Center(), input.ButtonPrimary); len(got) != 1 || got[0] != (app.PressRecord{Module: board}) {
		t.Fatalf("Record asks %v", got)
	}
	e.PressRecord(board)
	tick()
	if b.record.Kind != widget.ButtonDanger {
		t.Error("Record waiting is not shown")
	}
	w.Input(input.KeyPress{Key: input.KeyEscape})
	if got := sent(); len(got) != 1 || got[0] != (app.GiveUpRecord{Module: board}) {
		t.Errorf("Escape, while Record waits, asks %v", got)
	}
	pad := at.Min.Add(b.scenes[5].r.Center())
	if got := press(pad, input.ButtonPrimary); len(got) != 1 || got[0] != (app.PressPad{Module: board, Pad: 5}) {
		t.Fatalf("pad 6 asks %v", got)
	}
	if on, level := b.sceneLit(2); on || level != 0 {
		t.Error("a scene the engine has not turned on is lit")
	}
	e.GiveUpRecord(board)
	e.PressPad(board, 2)
	tick()
	if on, level := b.sceneLit(2); !on || level != 1 || b.record.Kind == widget.ButtonDanger {
		t.Errorf("the scene on is lit %v at %v, and Record waits %v", on, level, b.record.Kind == widget.ButtonDanger)
	}
	w.Input(input.KeyPress{Key: input.KeyEscape})
	if got := sent(); len(got) != 0 {
		t.Errorf("Escape, with Record not waiting, asks %v", got)
	}
	// The scene's menu: its first item asks for a name over the window,
	// its second deletes it.
	_ = press(at.Min.Add(b.scenes[2].r.Center()), input.ButtonSecondary)
	w.Input(input.KeyPress{Key: input.KeyDown})
	w.Input(input.KeyPress{Key: input.KeyEnter})
	for range 10 {
		w.Frame(time.Second / 60)
	}
	w.Input(input.TextInput{Text: "Sunset"})
	w.Input(input.KeyPress{Key: input.KeyEnter})
	if got := sent(); len(got) != 1 || got[0] != (app.RenameScene{Module: board, Pad: 2, Name: "Sunset"}) {
		t.Errorf("Rename from the pad's menu asks %v", got)
	}
	for range 30 {
		w.Frame(time.Second / 60)
	}
	_ = press(at.Min.Add(b.scenes[2].r.Center()), input.ButtonSecondary)
	w.Input(input.KeyPress{Key: input.KeyDown})
	w.Input(input.KeyPress{Key: input.KeyDown})
	w.Input(input.KeyPress{Key: input.KeyEnter})
	if got := sent(); len(got) != 1 || got[0] != (app.DeleteScene{Module: board, Pad: 2}) {
		t.Errorf("Delete from the pad's menu asks %v", got)
	}
	// A press on another pad while the menu is open only closes it.
	for range 10 {
		w.Frame(time.Second / 60)
	}
	_ = press(at.Min.Add(b.scenes[2].r.Center()), input.ButtonSecondary)
	if !b.menu.Focusable() {
		t.Fatal("the pad's menu did not open")
	}
	if got := press(at.Min.Add(b.scenes[5].r.Center()), input.ButtonPrimary); len(got) != 0 || b.menu.Focusable() {
		t.Errorf("a press with the menu open asks %v, and the menu is open %v", got, b.menu.Focusable())
	}
	if got := press(at.Min.Add(b.scenes[5].r.Center()), input.ButtonPrimary); len(got) != 1 || got[0] != (app.PressPad{Module: board, Pad: 5}) {
		t.Errorf("the menu closed, pad 6 asks %v", got)
	}
}

// TestTheBoardTabMakesAndRunsAChase: New chase, a pad and Escape ask
// the board to make a chase, take a step and give up; while one is
// made the button reads Done, and Record waits not; a chase pressed
// runs, its step's pad ringed; Tap taps as it goes down; with no clock
// wired, the tab offers to listen to the sound card; and a chase's menu
// renames it, takes out its last step and deletes it, the press that
// closes it doing no more.
func TestTheBoardTabMakesAndRunsAChase(t *testing.T) {
	s := show.Start()
	dome := s.Rig.Lights[0].ID
	board := ""
	for i := range s.Modules {
		if s.Modules[i].Kind == show.Board {
			board = s.Modules[i].ID
			s.Modules[i].SetScene(show.Scene{Pad: 0, Name: "Red", Values: map[string]map[int]byte{dome: {1: 255}}})
			s.Modules[i].SetScene(show.Scene{Pad: 1, Name: "Blue", Values: map[string]map[int]byte{dome: {3: 255}}})
			s.Modules[i].Chases = []show.Chase{{ID: 3, Name: "Round", Steps: []int{1, 0}}}
		}
	}
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	e := engine.New(nil)
	defer e.Close()
	e.Set(s)
	tick := func() {
		if err := c.Patch(app.LiveTopic, e.Tick()); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			w.Frame(time.Second / 60)
		}
	}
	tick()
	tab, ok := frameOf(t, w).tabs[show.TabBoard].(*boardTab)
	if !ok {
		t.Fatal("no Board tab in front")
	}
	b := tab.page
	if len(b.chases) != 1 || b.chases[0].id != 3 || b.chases[0].name != "Round" {
		t.Fatalf("the tab shows the chases %+v", b.chases)
	}
	ui := frameUI(t, w)
	at, _ := ui.Bounds(b)
	sent := func() []gunim.Intent {
		w.Frame(time.Second / 60)
		var got []gunim.Intent
		for len(c.Intents()) > 0 {
			got = append(got, (<-c.Intents()).Intent)
		}
		return got
	}
	press := func(p geom.Point, button input.Button) []gunim.Intent {
		w.Input(input.PointerMove{Pos: p})
		w.Input(input.PointerDown{Pos: p, Button: button, Clicks: 1})
		w.Input(input.PointerUp{Pos: p, Button: button})
		return sent()
	}
	middle := func(n gunim.Node) geom.Point {
		t.Helper()
		r, ok := ui.Bounds(n)
		if !ok {
			t.Fatalf("%T is not drawn", n)
		}
		return r.Center()
	}
	if got := press(middle(b.newChase), input.ButtonPrimary); len(got) != 1 || got[0] != (app.NewChase{Module: board}) {
		t.Fatalf("New chase asks %v", got)
	}
	e.NewChase(board)
	tick()
	if b.newChase.Label != "Done" || !b.record.Disabled {
		t.Errorf("making a chase, the button reads %q and Record is disabled %v", b.newChase.Label, b.record.Disabled)
	}
	if got := press(at.Min.Add(b.scenes[1].r.Center()), input.ButtonPrimary); len(got) != 1 || got[0] != (app.PressPad{Module: board, Pad: 1}) {
		t.Errorf("a pad, while a chase is made, asks %v", got)
	}
	w.Input(input.KeyPress{Key: input.KeyEscape})
	if got := sent(); len(got) != 1 || got[0] != (app.GiveUpChase{Module: board}) {
		t.Errorf("Escape, while a chase is made, asks %v", got)
	}
	if got := press(middle(b.newChase), input.ButtonPrimary); len(got) != 1 || got[0] != (app.DoneChase{Module: board}) {
		t.Errorf("Done asks %v", got)
	}
	e.GiveUpChase(board)
	tick()
	if got := press(at.Min.Add(b.chases[0].r.Center()), input.ButtonPrimary); len(got) != 1 || got[0] != (app.PressChase{Module: board, Chase: 3}) {
		t.Fatalf("the chase pressed asks %v", got)
	}
	e.PressChase(board, 3)
	tick()
	if st := b.played.Boards[board]; b.stepPad(&st) != 1 || b.newChase.Label != "New chase" {
		t.Errorf("the chase run is at pad %d", b.stepPad(&st))
	}
	tp := middle(b.tap)
	w.Input(input.PointerMove{Pos: tp})
	w.Input(input.PointerDown{Pos: tp, Button: input.ButtonPrimary, Clicks: 1})
	if got := sent(); len(got) != 1 || got[0] != (app.TapChase{Module: board}) {
		t.Errorf("Tap going down asks %v", got)
	}
	w.Input(input.PointerUp{Pos: tp, Button: input.ButtonPrimary})
	if got := sent(); len(got) != 0 {
		t.Errorf("Tap coming up asks %v", got)
	}
	if got := press(middle(b.listen), input.ButtonPrimary); len(got) != 1 || got[0] != (app.ListenForBeat{Module: board}) {
		t.Errorf("Listen to the sound card asks %v", got)
	}
	// The chase's menu: Rename, Remove the last step, Delete.
	chase := at.Min.Add(b.chases[0].r.Center())
	pick := func(item int) []gunim.Intent {
		for range 30 {
			w.Frame(time.Second / 60)
		}
		_ = press(chase, input.ButtonSecondary)
		for range item + 1 {
			w.Input(input.KeyPress{Key: input.KeyDown})
		}
		w.Input(input.KeyPress{Key: input.KeyEnter})
		return sent()
	}
	_ = pick(0)
	for range 10 {
		w.Frame(time.Second / 60)
	}
	w.Input(input.TextInput{Text: "Up and down"})
	w.Input(input.KeyPress{Key: input.KeyEnter})
	if got := sent(); len(got) != 1 || got[0] != (app.RenameChase{Module: board, Chase: 3, Name: "Up and down"}) {
		t.Errorf("Rename from the chase's menu asks %v", got)
	}
	if got := pick(1); len(got) != 1 || got[0] != (app.RemoveStep{Module: board, Chase: 3}) {
		t.Errorf("Remove the last step asks %v", got)
	}
	if got := pick(2); len(got) != 1 || got[0] != (app.DeleteChase{Module: board, Chase: 3}) {
		t.Errorf("Delete from the chase's menu asks %v", got)
	}
	for range 10 {
		w.Frame(time.Second / 60)
	}
	_ = press(chase, input.ButtonSecondary)
	if !b.menu.Focusable() {
		t.Fatal("the chase's menu did not open")
	}
	if got := press(chase, input.ButtonPrimary); len(got) != 0 || b.menu.Focusable() {
		t.Errorf("a press with the menu open asks %v, and the menu is open %v", got, b.menu.Focusable())
	}
}

// TestTheBoardTabIsPlayedFromAController: with no controller in the
// show, the tab offers Add an X-Touch Mini, which asks for one; with
// one that plays the board, the picker offers None and it, picked,
// the button is gone, and the eight faders and pads of the page it is
// on are marked, the next eight on page 2; None picked asks to play
// the board from none; and its Controllers card says it plays the
// Board, or, with no port, that it wants one.
func TestTheBoardTabIsPlayedFromAController(t *testing.T) {
	s := show.Start()
	board := ""
	for _, m := range s.Modules {
		if m.Kind == show.Board {
			board = m.ID
		}
	}
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	e := engine.New(nil)
	defer e.Close()
	e.Set(s)
	tick := func() {
		if err := c.Patch(app.LiveTopic, e.Tick()); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			w.Frame(time.Second / 60)
		}
	}
	tick()
	tab, ok := frameOf(t, w).tabs[show.TabBoard].(*boardTab)
	if !ok {
		t.Fatal("no Board tab in front")
	}
	b := tab.page
	if len(b.remotes) != 1 || b.from.Selected() != 0 || b.hasTouch {
		t.Fatalf("with no controller, the picker offers %v, picked %d", b.remotes, b.from.Selected())
	}
	ui := frameUI(t, w)
	r, ok := ui.Bounds(b.addTouch)
	if !ok {
		t.Fatal("Add an X-Touch Mini is not drawn")
	}
	w.Input(input.PointerMove{Pos: r.Center()})
	w.Input(input.PointerDown{Pos: r.Center(), Button: input.ButtonPrimary, Clicks: 1})
	w.Input(input.PointerUp{Pos: r.Center(), Button: input.ButtonPrimary})
	w.Frame(time.Second / 60)
	var got []gunim.Intent
	for len(c.Intents()) > 0 {
		got = append(got, (<-c.Intents()).Intent)
	}
	if len(got) != 1 || got[0] != (app.AddXTouchMini{Module: board}) {
		t.Fatalf("Add an X-Touch Mini asks %v", got)
	}
	ctl := s.NewID("m")
	s.Modules = append(s.Modules, show.Module{ID: ctl, Kind: show.Controller, Name: "X-Touch Mini", Layout: controller.XTouchMiniID})
	s.Module(board).PlayedFrom = ctl
	s.Layout.Show(show.TabMIDI)
	if err := c.Publish(app.ShowTopic, app.State{Show: s}); err != nil {
		t.Fatal(err)
	}
	e.Set(s)
	e.Pick(board, []string{s.Rig.Lights[0].ID}, false)
	tick()
	if len(b.remotes) != 2 || b.remotes[1] != ctl || b.from.Selected() != 1 || slices.Contains(b.kids, gunim.Node(b.addTouch)) {
		t.Fatalf("played from the X-Touch Mini, the picker offers %v, picked %d", b.remotes, b.from.Selected())
	}
	st := b.played.Boards[board]
	if st.Remote != ctl || len(b.faders) != 9 {
		t.Fatalf("the board shows %+v, with %d faders", st, len(b.faders))
	}
	for i := range show.Pads {
		if b.held(&st, i) != (i < 8) {
			t.Errorf("on page 1, fader or pad %d marked %v", i+1, b.held(&st, i))
		}
	}
	st.Page = 1
	if b.held(&st, 7) || !b.held(&st, 8) || !b.held(&st, 15) {
		t.Error("on page 2, the marks are not on 9 to 16")
	}
	if in := b.from.OnChange(0, nil); in != (app.PlayFrom{Module: board}) {
		t.Errorf("None picked asks %v", in)
	}
	if says := controllerSays(&s, s.Module(ctl), engine.Desk{Says: "No device"}); says != "Plays the Board · No MIDI port: plug it in, then pick its port" {
		t.Errorf("the controller's card says %q", says)
	}
	s.Module(ctl).Device = "X-TOUCH MINI"
	s.Module(board).PlayedFrom = ""
	if says := controllerSays(&s, s.Module(ctl), engine.Desk{Says: "X-TOUCH MINI"}); says != "X-TOUCH MINI" {
		t.Errorf("playing no board, the controller's card says %q", says)
	}
}

// TestATabBehindAnotherFollowsTheShow: a tab once shown, then behind
// another tab of its group, is out of the tree. The show changing then
// must not insert under it, which panics, and the tab shows the change
// as it comes back in front.
func TestATabBehindAnotherFollowsTheShow(t *testing.T) {
	s := show.Start()
	out := ""
	for i := range s.Modules {
		if s.Modules[i].Kind == show.DMXOut {
			out = s.Modules[i].ID
		}
	}
	w := testWindow(t, geom.Sz(1280, 800))
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	st := app.State{Show: s}
	if err := c.Mount(gunim.Root, View, View, st, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	// publish shows the show changed by change, and lets a tab on its
	// way out leave.
	publish := func(change func(s *show.Show)) {
		t.Helper()
		s = s.Clone()
		change(&s)
		st.Show = s
		if err := c.Publish(app.ShowTopic, st); err != nil {
			t.Fatal(err)
		}
		for range 30 {
			w.Frame(time.Second / 60)
		}
	}
	// Every tab made, each then behind the last of its group.
	tabs := []string{
		show.TabBoard, show.TabRoutes, show.TabRig, show.TabMIDI, show.TabDraw, show.TabWatch,
		show.TabStage, show.TabFaders, show.TabPlayer, show.TabLive,
	}
	for _, tab := range tabs {
		publish(func(s *show.Show) { s.Layout.Show(tab) })
	}
	f := frameOf(t, w)
	for _, tab := range tabs {
		if f.tabs[tab] == nil {
			t.Fatalf("the %s tab was not made", tab)
		}
	}
	e := engine.New(nil)
	e.Set(s)
	live := func() {
		t.Helper()
		if err := c.Patch(app.LiveTopic, e.Tick()); err != nil {
			t.Fatal(err)
		}
		w.Frame(time.Second / 60)
	}

	// The Board and Routes both behind the Monitor: a USB box chosen.
	publish(func(s *show.Show) { s.Module(out).Out = show.Output{Device: "enttec"} })
	live()
	board := f.tabs[show.TabBoard].(*boardTab).page
	routes := f.tabs[show.TabRoutes].(*routes)
	if board.output.more.shown != board.output.usb || routes.outputs[out].more.shown != routes.outputs[out].usb {
		t.Fatal("a tab behind another did not follow the output to the USB box")
	}
	// A light gone, which takes its faders and its pad with it.
	publish(func(s *show.Show) { s.Rig.Lights = nil })
	live()

	// The Board in front again shows the port's list, in the tree.
	publish(func(s *show.Show) { s.Layout.Show(show.TabBoard) })
	u := frameUI(t, w)
	if _, ok := u.Bounds(board.output.port); !ok {
		t.Error("the Board in front again does not show the USB box's port")
	}
	// Routes, behind it, follows the output to Art-Net, and shows the
	// address as it comes in front.
	publish(func(s *show.Show) { s.Module(out).Out = show.Output{Device: "artnet", Universe: 2} })
	live()
	publish(func(s *show.Show) { s.Layout.Show(show.TabRoutes) })
	u = frameUI(t, w)
	if _, ok := u.Bounds(routes.outputs[out].host); !ok {
		t.Error("Routes in front again does not show the Art-Net address")
	}
}
