// Package ui is crossroads' windows' half: each window is a dock of
// tabs, Routes, Stage, Faders and Rig, that the user drags into the
// arrangement they want, across as many windows as they like. The
// application's half is package app.
package ui

import (
	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"
)

// View is the name the windows' view is registered under.
const View = "crossroads"

// Register registers the view of window win of the layout in w: Mount
// it with the state, following app.ShowTopic and app.LiveTopic.
func Register(w *gunim.Window, win int) {
	gunim.RegisterView(w, View, func(app.State) *frame { return newFrame(win) },
		func(f *frame, s app.State, u *gunim.UI) { f.show(s, u) })
	gunim.RegisterPatch(w, View, func(f *frame, l engine.Live, u *gunim.UI) { f.live(l, u) })
}

// A tab is what a tab of the dock shows.
type tab interface {
	gunim.Node
	show(s *app.State, u *gunim.UI)
	live(l engine.Live, u *gunim.UI)
}

// frame is a window: its title row, and the dock of its tabs.
type frame struct {
	win      int
	d        *dock.Dock
	title    *widget.WindowTitle
	menu     *widget.MenuButton
	controls *widget.WindowControls
	tabs     map[string]tab
	st       app.State
	last     engine.Live
	named    string
}

func newFrame(win int) *frame {
	f := &frame{win: win, d: dock.NewDock(win), title: widget.NewWindowTitle("crossroads"), controls: widget.NewWindowControls(),
		tabs: map[string]tab{}}
	f.menu = widget.NewMenuButton("Show", []widget.MenuItem{
		{Label: "New show", Icon: icon.FilePlus, Hint: "Ctrl+N"},
		{Label: "Open…", Icon: icon.FolderOpen, Hint: "Ctrl+O"},
		{Label: "Save", Icon: icon.Save, Hint: "Ctrl+S"},
	})
	f.menu.OnPick = func(i int, _ *gunim.UI) gunim.Intent {
		switch i {
		case 0:
			return app.New{}
		case 1:
			return app.Open{}
		}
		return app.Save{}
	}
	f.d.Content = f.content
	f.d.Face = func(n *dock.Node) dock.Face {
		return dock.Face{Title: show.TabTitle(n.ID), Icon: tabIcon(n.ID)}
	}
	f.d.Keys, f.d.Caption = true, win != 0
	return f
}

// tabIcon is the icon tab t's tab shows.
func tabIcon(t string) *icon.Icon {
	switch t {
	case show.TabRoutes:
		return icon.Workflow
	case show.TabStage:
		return icon.Lightbulb
	case show.TabFaders:
		return icon.SlidersVertical
	case show.TabRig:
		return icon.Cable
	}
	return icon.SquareTerminal
}

// content makes what tab t shows.
func (f *frame) content(t string) gunim.Node {
	var n tab
	switch t {
	case show.TabRoutes:
		n = newRoutes()
	case show.TabStage:
		n = newStage()
	case show.TabFaders:
		n = newFaders()
	case show.TabRig:
		n = newRig()
	default:
		return nil
	}
	f.tabs[t] = n
	n.show(&f.st, nil)
	n.live(f.last, nil)
	return n
}

// show follows the application's state.
func (f *frame) show(s app.State, u *gunim.UI) {
	f.st = s
	f.d.SetLayout(s.Show.Layout, u)
	for _, t := range f.tabs {
		t.show(&f.st, u)
	}
	if title := s.Title(); title != f.named {
		f.named = title
		f.title.Text = title
		if u != nil {
			u.SetTitle(title)
		}
	}
	u.Invalidate()
}

// live follows what the engine plays.
func (f *frame) live(l engine.Live, u *gunim.UI) {
	f.last = l
	for _, t := range f.tabs {
		t.live(l, u)
	}
}

// Children implements [gunim.Composite].
func (f *frame) Children() []gunim.Node {
	return []gunim.Node{f.d, f.menu, f.title, f.controls}
}

// titleH is how tall the title row is.
const titleH = 40

// Layout implements [gunim.Node]: the title row, the Show menu, the
// title and the window's buttons, over the dock.
func (f *frame) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	d, menu, title, controls := kids.At(0), kids.At(1), kids.At(2), kids.At(3)
	cs := controls.Layout(gunim.Constraints{Max: geom.Sz(size.W, titleH)})
	controls.Place(geom.Pt(size.W-cs.W, 0))
	ms := menu.Layout(gunim.Constraints{Max: geom.Sz(160, titleH-8)})
	menu.Place(geom.Pt(8, (titleH-ms.H)/2))
	title.Layout(gunim.Tight(geom.Sz(max(size.W-cs.W-ms.W-24, 0), titleH)))
	title.Place(geom.Pt(ms.W+16, 0))
	d.Layout(gunim.Tight(geom.Sz(max(size.W-16, 0), max(size.H-titleH-8, 0))))
	d.Place(geom.Pt(8, titleH))
	return size
}

// Paint implements [gunim.Node].
func (f *frame) Paint(p *paint.Painter, fr gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenubarFill.Get(fr.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}

// CatchKey implements [gunim.KeyCatcher]: Ctrl+S saves the show,
// Ctrl+O opens one, and Ctrl+N starts a new one.
func (f *frame) CatchKey(e input.Event, u *gunim.UI) bool {
	k, ok := e.(input.KeyPress)
	if !ok || !k.Mods.Has(input.ModControl) || k.Mods.Has(input.ModAlt) || k.Mods.Has(input.ModShift) {
		return false
	}
	switch k.Key {
	case input.KeyS:
		u.Send(f, app.Save{})
	case input.KeyO:
		u.Send(f, app.Open{})
	case input.KeyN:
		u.Send(f, app.New{})
	default:
		return false
	}
	return true
}
