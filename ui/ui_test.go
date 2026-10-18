package ui

import (
	"testing"
	"time"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
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
