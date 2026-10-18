// Package app is crossroads' application half: it keeps the show and
// runs it, carries out what the windows ask for, and opens and closes
// the windows as the tabs move between them. The windows' half is
// package ui.
package app

import (
	"context"
	"log"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget/dock"
	"github.com/marrasen/gunim/widget/graph"
)

// The topics the windows follow: the show, as it changes, and what the
// engine plays, many times a second.
const (
	ShowTopic = "show"
	LiveTopic = "live"
)

// State is what every window shows: the show, the file it is in, and
// whether it has changes not saved.
type State struct {
	Show    show.Show
	Path    string
	Unsaved bool
}

// Title is the windows' title.
func (s State) Title() string {
	name := s.Show.Name
	if s.Path != "" {
		name = filepath.Base(s.Path)
	}
	if s.Unsaved {
		name += " •"
	}
	return name + " — crossroads"
}

// Intents the windows send, besides the dock's and the graph's changes.
type (
	// SetValue sets a fader's value, or a mixer's grand master: at
	// once while the fader moves, and kept in the show once Done.
	SetValue struct {
		Module string
		Value  float64
		Done   bool
	}
	// AddModule adds a module of Kind with its box at At.
	AddModule struct {
		Kind show.Kind
		At   geom.Point
	}
	// AddLight adds a light of the library's type Def at the first free
	// address, to the first Lights module too.
	AddLight struct{ Def string }
	// SetAddress moves light Light to Address.
	SetAddress struct {
		Light   string
		Address int
	}
	// RemoveLight takes light Light out of the show.
	RemoveLight struct{ Light string }
	// PlaceLight hangs light Light at Spot on the stage.
	PlaceLight struct {
		Light string
		Spot  show.Spot
	}
	// SetOutput has DMX out Module send to Out.
	SetOutput struct {
		Module string
		Out    show.Output
	}
	// Save saves the show, asking where for one never saved.
	Save struct{}
	// Open asks for a show file and opens it.
	Open struct{}
	// New starts a new show.
	New struct{}
)

// Opener opens a window for window win of the layout, at the point at
// in parent's space, size large, or where place says, and mounts the windows' view
// in it.
type Opener func(win int, parent *gunim.Window, at geom.Point, size geom.Size, place *driver.Placement) (gunim.Client, *gunim.Window, error)

// App is the application.
type App struct {
	st      State
	eng     *engine.Engine
	open    Opener
	wins    map[int]*win
	in      chan any
	changed chan struct{}
	// moving are the values of the faders held, not kept in the show
	// yet, which the engine goes on playing as the show changes.
	moving map[string]float64
}

// win is one of the application's windows.
type win struct {
	c  gunim.Client
	gw *gunim.Window
}

// savePath and openPath are the files the dialogs chose, coming back
// to the application's goroutine.
type (
	savePath string
	openPath string
)

// fromWin is an intent from window id, or word that it closed.
type fromWin struct {
	id     int
	in     gunim.Intent
	closed bool
}

// NewApp returns the application, the show s in it, read from path or
// new.
func NewApp(s show.Show, path string, open Opener) *App {
	s.Mend()
	if s.Layout.Window(0) == nil {
		s.Layout = show.Start().Layout
	}
	prune(&s)
	a := &App{st: State{Show: s, Path: path}, open: open, wins: map[int]*win{}, moving: map[string]float64{},
		in: make(chan any, 64), changed: make(chan struct{}, 1)}
	a.eng = engine.New(func() {
		select {
		case a.changed <- struct{}{}:
		default:
		}
	})
	a.eng.Set(a.st.Show)
	return a
}

// State returns the application's state.
func (a *App) State() State { return a.st }

// Run opens the windows and serves them until the main window closes
// or ctx ends, playing the show forty times a second.
func (a *App) Run(ctx context.Context) error {
	defer a.eng.Close()
	for _, w := range a.st.Show.Layout.Windows {
		if err := a.openWindow(w.ID, nil, geom.Pt(80, 80), geom.Sz(1280, 800), w.Place); err != nil {
			if w.ID == 0 {
				return err
			}
			log.Printf("crossroads: opening a window: %v", err)
		}
	}
	tick := time.NewTicker(time.Second / dmx.Rate)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.changed:
		case <-tick.C:
			live := a.eng.Tick()
			n++
			if n%2 == 0 {
				for _, w := range a.wins {
					_ = w.c.Patch(LiveTopic, live)
				}
			}
		case m := <-a.in:
			switch m := m.(type) {
			case savePath:
				a.saveTo(string(m))
				continue
			case openPath:
				a.openFrom(string(m))
				continue
			}
			fw, ok := m.(fromWin)
			if !ok {
				continue
			}
			if fw.closed {
				if a.windowClosed(fw.id) {
					return nil
				}
				continue
			}
			a.Handle(fw.id, fw.in)
		}
	}
}

// openWindow opens window id of the layout.
func (a *App) openWindow(id int, parent *gunim.Window, at geom.Point, size geom.Size, place *driver.Placement) error {
	if a.open == nil {
		return nil
	}
	c, gw, err := a.open(id, parent, at, size, place)
	if err != nil {
		return err
	}
	a.wins[id] = &win{c: c, gw: gw}
	_ = c.Publish(ShowTopic, a.st)
	go func() {
		for env := range c.Intents() {
			a.in <- fromWin{id: id, in: env.Intent}
		}
		a.in <- fromWin{id: id, closed: true}
	}()
	return nil
}

// windowClosed puts the tabs of a window the user closed back in the
// main window, and reports whether it was the main window, which ends
// the application.
func (a *App) windowClosed(id int) bool {
	if _, ok := a.wins[id]; !ok {
		return false
	}
	delete(a.wins, id)
	if id == 0 {
		for _, w := range a.wins {
			w.c.Close()
		}
		return true
	}
	l := a.st.Show.Layout.Clone()
	for _, t := range l.CloseWindow(id) {
		l.Add(t, dock.Target{Index: -1})
	}
	a.st.Show.Layout = l
	a.publish()
	return false
}

// Handle carries out intent in, from window win.
func (a *App) Handle(win int, in gunim.Intent) {
	s := a.st.Show.Clone()
	switch in := in.(type) {
	case SetValue:
		a.eng.SetValue(in.Module, in.Value)
		if !in.Done {
			a.moving[in.Module] = in.Value
			return
		}
		delete(a.moving, in.Module)
		if m := s.Module(in.Module); m != nil {
			m.Value = min(max(in.Value, 0), 1)
		}
	case AddModule:
		m := show.Module{ID: s.NewID("m"), Kind: in.Kind, Name: in.Kind.Title(), At: in.At}
		switch in.Kind {
		case show.Mixer:
			m.Value = 1
		case show.Lights:
			for _, p := range s.Rig.Lights {
				m.Lights = append(m.Lights, p.ID)
			}
		case show.Fader, show.DMXOut, show.StageView:
		}
		s.Modules = append(s.Modules, m)
	case AddLight:
		a.addLight(&s, in.Def)
	case SetAddress:
		if p := s.Light(in.Light); p != nil {
			p.StartAddress = min(max(in.Address, 1), dmx.Channels)
		}
	case RemoveLight:
		s.RemoveLight(in.Light)
	case PlaceLight:
		if s.Light(in.Light) == nil {
			return
		}
		if s.Stage.Spots == nil {
			s.Stage.Spots = map[string]show.Spot{}
		}
		s.Stage.Spots[in.Light] = show.Spot{X: min(max(in.Spot.X, 0), s.Stage.Width), Y: min(max(in.Spot.Y, 0), s.Stage.Depth)}
	case SetOutput:
		if m := s.Module(in.Module); m != nil {
			m.Out = in.Out
		}
	case graph.Change:
		if !a.graphChange(&s, in) {
			return
		}
	case dock.Change:
		a.dockChange(in)
		return
	case Save:
		a.save(win)
		return
	case Open:
		a.openShow(win)
		return
	case New:
		a.replace(show.Start(), "")
		return
	default:
		return
	}
	a.setShow(s)
}

// setShow makes s the show: the engine plays it, it is marked unsaved,
// and the windows show it.
func (a *App) setShow(s show.Show) {
	prune(&s)
	a.st.Show, a.st.Unsaved = s, true
	a.eng.Set(s)
	for id, v := range a.moving {
		// A fader held plays on where it is held.
		a.eng.SetValue(id, v)
	}
	a.publish()
}

// prune takes out the wires of s into or out of ports that are gone,
// as a Lights module's input for a colour none of its lights has now.
func prune(s *show.Show) {
	s.Wires = slices.DeleteFunc(s.Wires, func(w show.Wire) bool {
		_, from := engine.PortOf(s, w.From, true)
		_, to := engine.PortOf(s, w.To, false)
		return !from || !to
	})
}

// replace opens show s, from path: the windows close and open again as
// its layout has them.
func (a *App) replace(s show.Show, path string) {
	s.Mend()
	if s.Layout.Window(0) == nil {
		s.Layout = show.Start().Layout
	}
	prune(&s)
	clear(a.moving)
	a.st = State{Show: s, Path: path}
	a.eng.Set(s)
	a.syncWindows()
	a.publish()
}

// addLight adds a light of type def at the first address it fits.
func (a *App) addLight(s *show.Show, def string) {
	cat := s.Catalog()
	d, ok := cat.Find(def)
	if !ok || len(d.Modes) == 0 {
		return
	}
	p := fixtures.Patch{ID: s.NewID("light"), DefinitionID: def, ModeID: d.Modes[0].ID, Gamma: 2.2}
	n := 1
	for _, q := range s.Rig.Lights {
		if q.DefinitionID == def {
			n++
		}
	}
	p.Name = d.Model + " " + strconv.Itoa(n)
	at, ok := cat.SuggestStartAddress(s.Rig.Lights, cat.ChannelCount(p))
	if !ok {
		at = 1
	}
	p.StartAddress = at
	s.Rig.Lights = append(s.Rig.Lights, p)
	if s.Stage.Spots == nil {
		s.Stage.Spots = map[string]show.Spot{}
	}
	s.Stage.Spots[p.ID] = show.Spot{X: s.Stage.Width / 2, Y: s.Stage.Depth / 2}
	for i := range s.Modules {
		if s.Modules[i].Kind == show.Lights {
			s.Modules[i].Lights = append(s.Modules[i].Lights, p.ID)
			break
		}
	}
}

// graphChange carries out what the user asked of the Routes tab, and
// reports whether it changed the show.
func (a *App) graphChange(s *show.Show, c graph.Change) bool {
	switch c := c.(type) {
	case graph.Move:
		for id, at := range c.To {
			if m := s.Module(id); m != nil {
				m.At = at
			}
		}
		return true
	case graph.Connect:
		from, to := show.End{Module: c.From.Node, Port: c.From.Port}, show.End{Module: c.To.Node, Port: c.To.Port}
		if ok, _ := engine.CanJoin(s, from, to); !ok {
			return false
		}
		s.Wires = append(s.Wires, show.Wire{ID: s.NewID("w"), From: from, To: to})
		return true
	case graph.Replug:
		i := slices.IndexFunc(s.Wires, func(w show.Wire) bool { return w.ID == c.Wire })
		if i < 0 {
			return false
		}
		was := s.Wires[i]
		s.Wires = slices.Delete(s.Wires, i, i+1)
		from := show.End{Module: c.From.Node, Port: c.From.Port}
		to := show.End{Module: c.To.Node, Port: c.To.Port}
		if ok, _ := engine.CanJoin(s, from, to); !ok {
			return false
		}
		was.From, was.To = from, to
		s.Wires = slices.Insert(s.Wires, i, was)
		return true
	case graph.Delete:
		for _, id := range c.Nodes {
			s.RemoveModule(id)
		}
		s.Wires = slices.DeleteFunc(s.Wires, func(w show.Wire) bool { return slices.Contains(c.Wires, w.ID) })
		return len(c.Nodes) > 0 || len(c.Wires) > 0
	}
	return false
}

// dockChange carries out a tab moved, picked, resized, or let go
// outside every window.
func (a *App) dockChange(c dock.Change) {
	l := a.st.Show.Layout.Clone()
	switch c := c.(type) {
	case dock.Detach:
		w := a.wins[c.From]
		id, ok := l.ToWindow(c.ID, nil)
		if !ok {
			return
		}
		a.st.Show.Layout = l
		var parent *gunim.Window
		if w != nil {
			parent = w.gw
		}
		if err := a.openWindow(id, parent, c.At, c.Size, nil); err != nil {
			log.Printf("crossroads: opening a window: %v", err)
		}
	default:
		if !l.Apply(c) {
			return
		}
		a.st.Show.Layout = l
	}
	a.syncWindows()
	a.publish()
}

// syncWindows closes the windows the layout holds no more, and opens
// those it holds that have none.
func (a *App) syncWindows() {
	for id, w := range a.wins {
		if a.st.Show.Layout.Window(id) == nil {
			delete(a.wins, id)
			w.c.Close()
		}
	}
	for _, w := range a.st.Show.Layout.Windows {
		if a.wins[w.ID] == nil {
			if err := a.openWindow(w.ID, nil, geom.Pt(80, 80), geom.Sz(1100, 700), w.Place); err != nil {
				log.Printf("crossroads: opening a window: %v", err)
			}
		}
	}
}

// publish sends the state to every window.
func (a *App) publish() {
	for _, w := range a.wins {
		_ = w.c.Publish(ShowTopic, a.st)
	}
}

// save saves the show, asking window win where for one never saved:
// the dialog waits on a goroutine of its own, as the show plays on.
func (a *App) save(win int) {
	if a.st.Path != "" {
		a.saveTo(a.st.Path)
		return
	}
	w := a.wins[win]
	if w == nil {
		return
	}
	name := a.st.Show.Name + ".crossroads"
	go func() {
		p, err := w.c.SaveFile(context.Background(), driver.SaveOptions{Title: "Save the show", Name: name})
		if err == nil && p != "" {
			a.in <- savePath(p)
		}
	}()
}

// saveTo saves the show to path.
func (a *App) saveTo(path string) {
	a.keepPlaces()
	if err := a.st.Show.Save(path); err != nil {
		log.Printf("crossroads: saving %s: %v", path, err)
		return
	}
	a.st.Path, a.st.Unsaved = path, false
	a.publish()
}

// keepPlaces notes where each window stands, for the show to keep.
func (a *App) keepPlaces() {
	for id, w := range a.wins {
		if w.gw == nil {
			continue
		}
		if p, ok := w.gw.Placement(); ok {
			if lw := a.st.Show.Layout.Window(id); lw != nil {
				lw.Place = &p
			}
		}
	}
}

// openShow asks window win for a show file, on a goroutine of its own,
// and opens it.
func (a *App) openShow(win int) {
	w := a.wins[win]
	if w == nil {
		return
	}
	go func() {
		paths, err := w.c.ChooseFiles(context.Background(), driver.ChooseOptions{Title: "Open a show"})
		if err == nil && len(paths) > 0 {
			a.in <- openPath(paths[0])
		}
	}()
}

// openFrom opens the show at path.
func (a *App) openFrom(path string) {
	s, err := show.Load(path)
	if err != nil {
		log.Printf("crossroads: %v", err)
		return
	}
	a.replace(s, path)
}
