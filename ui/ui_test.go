package ui

import (
	"testing"
	"time"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio/capture"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
)

// TestAWindowShowsTheShowAndItsFadersPlay: the four tabs build, the
// stage glows as the engine plays, and a fader dragged on the Faders tab
// asks to set its value.
func TestAWindowShowsTheShowAndItsFadersPlay(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(1280, 800), nil)
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	st := app.State{Show: show.Start()}
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
	w := gunimtest.New(t, geom.Sz(1000, 700), nil)
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
	w := gunimtest.New(t, geom.Sz(1280, 800), nil)
	Register(w, 0)
	gunim.RegisterPatch(w, View, func(f *frame, call uiCall, u *gunim.UI) { call(f, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, View, View, app.State{Show: s}, app.ShowTopic, app.LiveTopic); err != nil {
		t.Fatal(err)
	}
	live := engine.Live{Values: map[string]float64{pl: 0.5}, Clocks: map[string]engine.Count{bt: {BPM: 124, Beats: 3.2, Sure: true}},
		Ears: map[string]string{in: "Microphone"}}
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
	w := gunimtest.New(t, geom.Sz(1280, 800), nil)
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
