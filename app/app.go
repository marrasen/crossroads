// Package app is crossroads' application half: it keeps the show and
// runs it, carries out what the windows ask for, and opens and closes
// the windows as the tabs move between them. The windows' half is
// package ui.
package app

import (
	"cmp"
	"context"
	"log"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/midittt"
	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/player"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/enttec"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim-dmx/sacn"
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
	// Songs say, by song, what is wrong with one: "Scanning…" while it
	// is scanned, or why it could not be read.
	Songs map[string]string
	// Imported says the show was brought over from midittt, and Notes
	// what of it plays differently, for the windows to tell, once.
	Imported string
	Notes    []string
	// Ports are the serial ports a USB DMX box may be on, as last
	// looked for; nil before they are.
	Ports []enttec.PortInfo
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
	// FindPorts looks for the serial ports a USB DMX box may be on.
	FindPorts struct{}
	// AddBoard adds a Board, wired into the first Mixer's first free
	// input, to a show that has none.
	AddBoard struct{}
	// Pick picks Lights on Board Module: those alone, or, with Toggle,
	// adds them, or takes them out where all of them are picked.
	Pick struct {
		Module string
		Lights []string
		Toggle bool
	}
	// Hold has Board Module's faders hold Channels at Value, 0 to 255.
	Hold struct {
		Module   string
		Channels []engine.Channel
		Value    int
	}
	// ClearBoard lets go of every channel Board Module's faders hold.
	ClearBoard struct{ Module string }
	// PressRecord has Board Module wait for a pad to record onto, or,
	// where it waits, give up.
	PressRecord struct{ Module string }
	// GiveUpRecord has Board Module wait for no pad to record onto.
	GiveUpRecord struct{ Module string }
	// PressPad presses Board Module's pad Pad, from 0: where Record
	// waits, it records onto it; otherwise it turns its scene on or off.
	PressPad struct {
		Module string
		Pad    int
	}
	// RenameScene names the scene on Board Module's pad Pad Name.
	RenameScene struct {
		Module, Name string
		Pad          int
	}
	// DeleteScene takes the scene on Board Module's pad Pad out.
	DeleteScene struct {
		Module string
		Pad    int
	}
	// SetSceneFade has Board Module's scenes fade in and out over
	// Seconds, 0 to 10.
	SetSceneFade struct {
		Module  string
		Seconds float64
	}
	// NewChase has Board Module start making a chase: the pads pressed
	// from now on are its steps, in order.
	NewChase struct{ Module string }
	// DoneChase ends the chase Board Module is making, which, with a
	// step or more, the show keeps.
	DoneChase struct{ Module string }
	// GiveUpChase has Board Module make no chase.
	GiveUpChase struct{ Module string }
	// PressChase runs Board Module's chase Chase, by ID, in place of any
	// that runs, or stops it where it runs.
	PressChase struct {
		Module string
		Chase  int
	}
	// TapChase taps Board Module's chase: taps a steady gap apart set
	// its speed, and each moves the chase that runs on a step.
	TapChase struct{ Module string }
	// RenameChase names Board Module's chase Chase Name.
	RenameChase struct {
		Module, Name string
		Chase        int
	}
	// RemoveStep takes the last step out of Board Module's chase Chase;
	// a chase left with none goes.
	RemoveStep struct {
		Module string
		Chase  int
	}
	// DeleteChase takes Board Module's chase Chase out.
	DeleteChase struct {
		Module string
		Chase  int
	}
	// SetChaseSpeed has Board Module's chase run at Speed steps a
	// minute, 20 to 600.
	SetChaseSpeed struct {
		Module string
		Speed  float64
	}
	// SetChaseFade has Board Module's chase spend Fade of each step,
	// 0 to 1, fading to it.
	SetChaseFade struct {
		Module string
		Fade   float64
	}
	// FollowBeat has Board Module's chase step on the beats of the clock
	// wired into it, where On says, or at its own speed.
	FollowBeat struct {
		Module string
		On     bool
	}
	// ListenForBeat has Board Module follow the beat the sound card
	// hears: an Audio in and a Beat, wired into its clock, where nothing
	// is wired there yet.
	ListenForBeat struct{ Module string }
	// Tap taps Tempo Module's beat: the beat falls on the tap, and taps
	// a steady gap apart set its tempo, which it then plays.
	Tap struct{ Module string }
	// SetTempo has Tempo Module play BPM, where Manual says, or follow
	// the clock wired into it. Taken over with no BPM, it plays on at
	// the tempo it followed.
	SetTempo struct {
		Module string
		BPM    float64
		Manual bool
	}
	// SetInput has Audio in Module listen to sound card input Input.
	SetInput struct{ Module, Input string }
	// SetEvery has Pulse Module flash every Every beats.
	SetEvery struct {
		Module string
		Every  float64
	}
	// AddSongs asks for songs and adds them to the playlist.
	AddSongs struct{}
	// AddSongPaths adds the songs at Paths to the playlist, in order,
	// before the song Before, at the end where it is "".
	AddSongPaths struct {
		Paths  []string
		Before string
	}
	// RemoveSong takes Song out of the playlist.
	RemoveSong struct{ Song string }
	// MoveSong moves Song to play before the song Before, last where it
	// is "".
	MoveSong struct{ Song, Before string }
	// SetSongBPM sets the tempo Song plays its lights at, or with 0 has
	// it play at the tempo its scan found.
	SetSongBPM struct {
		Song string
		BPM  float64
	}
	// SetSongGain sets how many decibels louder Song plays than the
	// target loudness brings it to.
	SetSongGain struct {
		Song string
		Gain float64
	}
	// Transport has the playlist do Command.
	Transport struct{ Command engine.Command }
	// PlaySong plays Song from its start.
	PlaySong struct{ Song string }
	// SeekSong moves the song playing to At.
	SeekSong struct{ At time.Duration }
	// Press presses Button Module.
	Press struct{ Module string }
	// SetDevice has Controller or MIDI out Module use the MIDI port
	// named Device.
	SetDevice struct{ Module, Device string }
	// SetLayout gives Controller Module the built-in layout Layout, or,
	// with "", the controls it learns.
	SetLayout struct{ Module, Layout string }
	// Learn has Controller Module learn the next control moved, or
	// stop.
	Learn struct {
		Module string
		On     bool
	}
	// RemoveControl has Controller Module forget learned control
	// Control.
	RemoveControl struct{ Module, Control string }
	// SetMap sets how Map Module maps.
	SetMap struct {
		Module   string
		From, To [2]float64
		Invert   bool
	}
	// SetLevel sets the level Look Module holds Role at, 0 to 1; a
	// level below 0 lets go of the role.
	SetLevel struct {
		Module, Role string
		Level        float64
	}
	// SetFade sets how a Chaser or a Cue list Module fades.
	SetFade struct {
		Module string
		Fade   float64
	}
	// Override holds Mixer Module's override Which, or lets it go.
	Override struct {
		Module, Which string
		On            bool
	}
	// Flip flips Toggle Module.
	Flip struct{ Module string }
	// SetTimer has Timer Module count down from Seconds, or up as a
	// stopwatch.
	SetTimer struct {
		Module    string
		Seconds   float64
		Stopwatch bool
	}
	// TimerDo tells Timer Module to start, pause or reset.
	TimerDo struct {
		Module  string
		Command engine.TimerCommand
	}
	// SetPattern has Pattern Module play the show's pattern Pattern.
	SetPattern struct{ Module, Pattern string }
	// SetProgram has Effect Module play the show's effect programme
	// Program.
	SetProgram struct{ Module, Program string }
	// FireEffect fires Effect Module.
	FireEffect struct{ Module string }
	// SavePattern puts Pattern in the show, in place of the pattern of
	// its ID, or as a new one.
	SavePattern struct{ Pattern pattern.Pattern }
	// RemovePattern takes the pattern of ID out of the show.
	RemovePattern struct{ ID string }
	// TestPattern plays Pattern on the stage while On, as the pattern
	// editor's test button is held.
	TestPattern struct {
		Pattern pattern.Pattern
		On      bool
	}
	// SetRouteOn turns MIDI routes Module's route Route on or off.
	SetRouteOn struct {
		Module, Route string
		On            bool
	}
	// Step moves Chaser or Cue list Module on a step, or back with By
	// -1.
	Step struct {
		Module string
		By     int
	}
	// SetMIDI sets the channel, note and controller MIDI out Module
	// sends.
	SetMIDI struct {
		Module            string
		Channel, Note, CC int
	}
	// Save saves the show, asking where for one never saved.
	Save struct{}
	// Open asks for a show file and opens it.
	Open struct{}
	// Import asks for a midittt rig and opens it, rebuilt as a show.
	Import struct{}
	// ImportPath opens the midittt rig at Path, rebuilt as a show.
	ImportPath struct{ Path string }
	// Told says the windows told of the import.
	Told struct{}
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
	// Scan scans a song; [player.Scan] where it is nil. scanning are
	// the songs being scanned, by path, and lanes holds a place for
	// each scan running, so only a few run at once.
	Scan     func(path string) (show.Scan, error)
	scanning map[string]string
	lanes    chan struct{}
	// Serial lists the serial ports; [enttec.ListPorts] where it is nil.
	// finding says it is listing them now.
	Serial  func() ([]enttec.PortInfo, error)
	finding bool
}

// scanLanes is how many songs are scanned at once.
const scanLanes = 2

// win is one of the application's windows.
type win struct {
	c  gunim.Client
	gw *gunim.Window
}

// savePath and openPath are the files the dialogs chose, coming back
// to the application's goroutine, and songPaths the songs.
type (
	savePath  string
	openPath  string
	songPaths []string
	rigPath   string
)

// scanned is what a scan of song found in the file at path, coming
// back to the application's goroutine.
type scanned struct {
	song, path string
	scan       show.Scan
	err        error
}

// portsFound are the serial ports found, coming back to the
// application's goroutine.
type portsFound []enttec.PortInfo

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
	a := &App{st: State{Show: s, Path: path, Songs: map[string]string{}}, open: open, wins: map[int]*win{}, moving: map[string]float64{},
		in: make(chan any, 64), changed: make(chan struct{}, 1), scanning: map[string]string{}, lanes: make(chan struct{}, scanLanes)}
	a.eng = engine.New(func() {
		select {
		case a.changed <- struct{}{}:
		default:
		}
	})
	a.eng.Set(a.st.Show)
	return a
}

// Imported says the show was brought over from midittt as name, and
// what of it plays differently, for the windows to tell. Call it before
// Run.
func (a *App) Imported(name string, notes []string) {
	a.st.Unsaved, a.st.Imported, a.st.Notes = true, name, notes
}

// UseDeck has the playlist play through d, which the application
// closes as it ends. Call it before Run.
func (a *App) UseDeck(d engine.Deck) {
	a.eng.Deck = d
}

// State returns the application's state.
func (a *App) State() State { return a.st }

// Run opens the windows and serves them until the main window closes
// or ctx ends, playing the show forty times a second.
func (a *App) Run(ctx context.Context) error {
	defer a.eng.Close()
	a.scanSongs()
	a.portsWanted()
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
			a.recorded()
		case <-tick.C:
			live := a.eng.Tick()
			a.learnt()
			a.recorded()
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
			case songPaths:
				a.Handle(0, AddSongPaths{Paths: m})
				continue
			case rigPath:
				a.Handle(0, ImportPath{Path: string(m)})
				continue
			case scanned:
				a.scanned(m)
				continue
			case portsFound:
				a.finding = false
				a.st.Ports = m
				a.publish()
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
		case show.Tempo:
			m.BPM = 120
		case show.Pulse:
			m.Every = 1
		case show.Every:
			m.Every = 4
		case show.Controller:
			m.Layout = controller.XTouchMiniID
		case show.Map:
			m.From, m.To = [2]float64{0, 1}, [2]float64{0, 1}
		case show.Look:
			// A warm white, on every light, ready to bring up.
			for _, p := range s.Rig.Lights {
				m.Lights = append(m.Lights, p.ID)
			}
			m.Levels = map[string]float64{"dimmer": 1, "white": 1, "red": 1, "green": 0.55, "blue": 0.2, "amber": 0.6}
		case show.Chaser:
			m.Every, m.Fade = 1, 0.5
		case show.Cues:
			m.Fade = 2
		case show.Timer:
			m.Seconds = 300
		case show.Pattern:
			if len(s.Patterns) > 0 {
				m.Pattern = s.Patterns[0].ID
			}
		case show.Effect:
			if len(s.Programs) > 0 {
				m.Program = s.Programs[0].ID
			}
		default:
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
			m.Out.Universe = clampUniverse(m.Out)
		}
		if usb(in.Out) && a.st.Ports == nil {
			a.findPorts()
		}
	case FindPorts:
		a.findPorts()
		return
	case AddBoard:
		if !addBoard(&s) {
			return
		}
	case Pick:
		a.eng.Pick(in.Module, in.Lights, in.Toggle)
		return
	case Hold:
		a.eng.Hold(in.Module, in.Channels, byte(min(max(in.Value, 0), 255)))
		return
	case ClearBoard:
		a.eng.ClearBoard(in.Module)
		return
	case PressRecord:
		a.eng.PressRecord(in.Module)
		return
	case GiveUpRecord:
		a.eng.GiveUpRecord(in.Module)
		return
	case PressPad:
		a.eng.PressPad(in.Module, in.Pad)
		a.recorded()
		return
	case RenameScene:
		m := s.Module(in.Module)
		name := strings.TrimSpace(in.Name)
		if m == nil || m.Scene(in.Pad) == nil || name == "" || m.Scene(in.Pad).Name == name {
			return
		}
		m.Scene(in.Pad).Name = name
	case DeleteScene:
		m := s.Module(in.Module)
		if m == nil || m.Scene(in.Pad) == nil {
			return
		}
		m.Scenes = slices.DeleteFunc(m.Scenes, func(c show.Scene) bool { return c.Pad == in.Pad })
	case SetSceneFade:
		m := s.Module(in.Module)
		secs := math.Round(min(max(in.Seconds, 0), show.MaxSceneFade)*10) / 10
		if m == nil || m.Kind != show.Board || m.SceneFade == secs {
			return
		}
		m.SceneFade = secs
	case NewChase:
		a.eng.NewChase(in.Module)
		return
	case DoneChase:
		a.eng.DoneChase(in.Module)
		a.recorded()
		return
	case GiveUpChase:
		a.eng.GiveUpChase(in.Module)
		return
	case PressChase:
		a.eng.PressChase(in.Module, in.Chase)
		return
	case TapChase:
		a.eng.TapChase(in.Module)
		a.recorded()
		return
	case RenameChase:
		m := boardOf(&s, in.Module)
		name := strings.TrimSpace(in.Name)
		if m == nil || m.Chase(in.Chase) == nil || name == "" || m.Chase(in.Chase).Name == name {
			return
		}
		m.Chase(in.Chase).Name = name
	case RemoveStep:
		m := boardOf(&s, in.Module)
		if m == nil || m.Chase(in.Chase) == nil {
			return
		}
		c := m.Chase(in.Chase)
		if c.Steps = c.Steps[:max(len(c.Steps)-1, 0)]; len(c.Steps) == 0 {
			// A chase of no steps is no chase.
			m.Chases = slices.DeleteFunc(m.Chases, func(c show.Chase) bool { return c.ID == in.Chase })
		}
	case DeleteChase:
		m := boardOf(&s, in.Module)
		if m == nil || m.Chase(in.Chase) == nil {
			return
		}
		m.Chases = slices.DeleteFunc(m.Chases, func(c show.Chase) bool { return c.ID == in.Chase })
	case SetChaseSpeed:
		m := boardOf(&s, in.Module)
		speed := engine.ClampSpeed(in.Speed)
		if m == nil || engine.ClampSpeed(cmp.Or(m.Speed, show.HomeSpeed)) == speed {
			return
		}
		m.Speed = speed
	case SetChaseFade:
		m := boardOf(&s, in.Module)
		fade := math.Round(min(max(in.Fade, 0), 1)*100) / 100
		if m == nil || m.Fade == fade {
			return
		}
		m.Fade = fade
	case FollowBeat:
		m := boardOf(&s, in.Module)
		if m == nil || m.FollowBeat == in.On {
			return
		}
		m.FollowBeat = in.On
	case ListenForBeat:
		if !listenForBeat(&s, in.Module) {
			return
		}
	case Tap:
		m := s.Module(in.Module)
		if m == nil || m.Kind != show.Tempo {
			return
		}
		bpm, ok := a.eng.Tap(in.Module)
		if !ok {
			// A first tap only puts the beat on it; the show is the
			// same.
			return
		}
		m.BPM, m.Manual = clampBPM(bpm), true
	case SetTempo:
		if m := s.Module(in.Module); m != nil {
			switch c, ok := a.eng.Played(in.Module); {
			case in.BPM > 0:
				m.BPM = clampBPM(in.BPM)
			case in.Manual && !m.Manual && ok:
				// Taken over: it plays on at the tempo it followed.
				m.BPM = clampBPM(c.BPM)
			}
			m.Manual = in.Manual
		}
	case SetInput:
		if m := s.Module(in.Module); m != nil {
			m.Input = in.Input
		}
	case SetEvery:
		if m := s.Module(in.Module); m != nil && in.Every > 0 {
			m.Every = in.Every
		}
	case graph.Change:
		if !a.graphChange(&s, in) {
			return
		}
	case dock.Change:
		a.dockChange(in)
		return
	case AddSongs:
		a.chooseSongs(win)
		return
	case AddSongPaths:
		at := len(s.Songs)
		if i := slices.IndexFunc(s.Songs, func(g show.Song) bool { return g.ID == in.Before }); i >= 0 {
			at = i
		}
		var add []show.Song
		for _, p := range in.Paths {
			if player.Plays(p) {
				add = append(add, show.Song{ID: s.NewID("song"), Path: p, Title: player.TitleOf(p)})
			}
		}
		if len(add) == 0 {
			return
		}
		s.Songs = slices.Insert(s.Songs, at, add...)
	case RemoveSong:
		s.Songs = slices.DeleteFunc(s.Songs, func(g show.Song) bool { return g.ID == in.Song })
	case MoveSong:
		i := slices.IndexFunc(s.Songs, func(g show.Song) bool { return g.ID == in.Song })
		if i < 0 || in.Song == in.Before {
			return
		}
		g := s.Songs[i]
		s.Songs = slices.Delete(s.Songs, i, i+1)
		at := len(s.Songs)
		if j := slices.IndexFunc(s.Songs, func(g show.Song) bool { return g.ID == in.Before }); j >= 0 {
			at = j
		}
		s.Songs = slices.Insert(s.Songs, at, g)
	case SetSongBPM:
		if g := s.Song(in.Song); g != nil {
			g.BPM = 0
			if in.BPM > 0 {
				g.BPM = clampBPM(in.BPM)
			}
		}
	case SetSongGain:
		if g := s.Song(in.Song); g != nil {
			g.Gain = math.Round(min(max(in.Gain, -MaxGain), MaxGain)*10) / 10
		}
	case Transport:
		a.eng.Transport(in.Command)
		return
	case PlaySong:
		a.eng.PlaySong(in.Song)
		return
	case SeekSong:
		a.eng.SeekTo(in.At)
		return
	case Press:
		a.eng.Press(in.Module)
		return
	case SetDevice:
		if m := s.Module(in.Module); m != nil {
			m.Device = in.Device
		}
	case SetLayout:
		if m := s.Module(in.Module); m != nil {
			m.Layout = in.Layout
		}
	case Learn:
		a.eng.Learn(in.Module, in.On)
		return
	case RemoveControl:
		if m := s.Module(in.Module); m != nil {
			m.Controls = slices.DeleteFunc(m.Controls, func(c show.Control) bool { return c.ID == in.Control })
		}
	case SetMap:
		if m := s.Module(in.Module); m != nil {
			m.From, m.To, m.Invert = in.From, in.To, in.Invert
		}
	case SetLevel:
		if m := s.Module(in.Module); m != nil {
			m.Levels = maps.Clone(m.Levels)
			if m.Levels == nil {
				m.Levels = map[string]float64{}
			}
			if in.Level < 0 {
				delete(m.Levels, in.Role)
			} else {
				m.Levels[in.Role] = min(in.Level, 1)
			}
		}
	case SetFade:
		if m := s.Module(in.Module); m != nil {
			m.Fade = max(in.Fade, 0)
			if m.Kind == show.Chaser {
				m.Fade = min(m.Fade, 1)
			}
		}
	case Override:
		a.eng.Override(in.Module, in.Which, in.On)
		return
	case Flip:
		a.eng.Flip(in.Module)
		return
	case SetTimer:
		if m := s.Module(in.Module); m != nil {
			m.Seconds, m.Stopwatch = max(in.Seconds, 0), in.Stopwatch
		}
	case TimerDo:
		a.eng.TimerDo(in.Module, in.Command)
		return
	case SetPattern:
		if m := s.Module(in.Module); m != nil {
			m.Pattern = in.Pattern
		}
	case SetProgram:
		if m := s.Module(in.Module); m != nil {
			m.Program = in.Program
		}
	case FireEffect:
		a.eng.FireEffect(in.Module)
		return
	case SavePattern:
		p := in.Pattern
		p.Lanes = slices.Clone(p.Lanes)
		if i := slices.IndexFunc(s.Patterns, func(o pattern.Pattern) bool { return o.ID == p.ID }); i >= 0 {
			s.Patterns[i] = p
		} else {
			if p.ID == "" {
				p.ID = s.NewID("pattern")
			}
			s.Patterns = append(s.Patterns, p)
		}
	case RemovePattern:
		s.Patterns = slices.DeleteFunc(s.Patterns, func(p pattern.Pattern) bool { return p.ID == in.ID })
	case TestPattern:
		if in.On {
			p := in.Pattern
			a.eng.Audition(&p)
		} else {
			a.eng.Audition(nil)
		}
		return
	case SetRouteOn:
		if m := s.Module(in.Module); m != nil {
			for i := range m.Routes {
				if m.Routes[i].ID == in.Route {
					m.Routes[i].Enabled = in.On
				}
			}
		}
	case Step:
		a.eng.Step(in.Module, in.By)
		return
	case SetMIDI:
		if m := s.Module(in.Module); m != nil {
			m.Channel, m.Note, m.CC = min(max(in.Channel, 0), 15), min(max(in.Note, 0), 127), min(max(in.CC, 0), 127)
		}
	case Save:
		a.save(win)
		return
	case Open:
		a.openShow(win)
		return
	case Import:
		a.chooseRig(win)
		return
	case ImportPath:
		c, err := midittt.Load(in.Path)
		if err != nil {
			a.st.Imported, a.st.Notes = midittt.NameOf(in.Path), []string{"It could not be read: " + err.Error()}
			a.publish()
			return
		}
		r := midittt.Rebuild(c, midittt.NameOf(in.Path))
		a.replace(r.Show, "")
		a.st.Unsaved, a.st.Imported, a.st.Notes = true, r.Show.Name, r.Notes
		a.publish()
		return
	case Told:
		a.st.Imported, a.st.Notes = "", nil
		a.publish()
		return
	case New:
		a.replace(show.Start(), "")
		return
	default:
		return
	}
	a.setShow(s)
}

// The tempos a Tempo plays between.
const (
	MinBPM = 30.0
	MaxBPM = 300.0
)

// clampBPM is bpm, within the tempos a Tempo plays, to a tenth of a
// beat a minute.
// clampUniverse is the universe o sends to, kept within what its box
// takes: an Art-Net port address, or an sACN universe.
func clampUniverse(o show.Output) int {
	switch o.Device {
	case "artnet":
		return min(max(o.Universe, 0), 32767)
	case "sacn":
		return min(max(o.Universe, sacn.MinUniverse), sacn.MaxUniverse)
	}
	return max(o.Universe, 0)
}

func clampBPM(bpm float64) float64 {
	return math.Round(min(max(bpm, MinBPM), MaxBPM)*10) / 10
}

// learnt adds the controls the controllers learned to them.
func (a *App) learnt() { a.learntFrom(a.eng.Learnt()) }

// learntFrom adds the controls got to their controllers.
func (a *App) learntFrom(got []engine.Learnt) {
	if len(got) == 0 {
		return
	}
	s := a.st.Show.Clone()
	for _, l := range got {
		m := s.Module(l.Module)
		if m == nil {
			continue
		}
		c := l.Control
		// An ID none of its controls has.
		for n := len(m.Controls) + 1; slices.ContainsFunc(m.Controls, func(o show.Control) bool { return o.ID == c.ID }); n++ {
			c.ID = "c" + strconv.Itoa(n)
		}
		m.Controls = append(m.Controls, c)
	}
	a.setShow(s)
}

// recorded keeps the scenes the Boards recorded in the show, and the
// chases they made and the speeds tapped.
func (a *App) recorded() {
	a.recordedFrom(a.eng.Recorded())
	a.madeFrom(a.eng.Made())
}

// madeFrom adds each chase got to its Board, and sets each speed got as
// its Board's chase speed.
func (a *App) madeFrom(got []engine.Made) {
	if len(got) == 0 {
		return
	}
	s := a.st.Show.Clone()
	changed := false
	for _, d := range got {
		m := boardOf(&s, d.Module)
		switch {
		case m == nil:
		case d.Chase == nil:
			changed = changed || m.Speed != d.Speed
			m.Speed = d.Speed
		case m.Chase(d.Chase.ID) == nil:
			m.Chases = append(m.Chases, d.Chase.Clone())
			changed = true
		}
	}
	if changed {
		a.setShow(s)
	}
}

// boardOf is Board module id of s, nil where s has none of that ID or
// it is no Board.
func boardOf(s *show.Show, id string) *show.Module {
	if m := s.Module(id); m != nil && m.Kind == show.Board {
		return m
	}
	return nil
}

// listenForBeat has Board module board of s follow the beat the sound
// card hears: it adds an Audio in, listening to the default input, and
// a Beat, wires the one into the other and the Beat into the Board's
// clock, and has its chase follow the beat. It does nothing, and
// reports false, where a clock is wired into the Board already.
func listenForBeat(s *show.Show, board string) bool {
	m := boardOf(s, board)
	if m == nil || slices.ContainsFunc(s.Wires, func(w show.Wire) bool { return w.To == show.End{Module: board, Port: "clock"} }) {
		return false
	}
	m.FollowBeat = true
	at := m.At
	in := show.Module{ID: s.NewID("m"), Kind: show.AudioIn, Name: show.AudioIn.Title(), At: at.Add(geom.Pt(-600, 180))}
	beat := show.Module{ID: s.NewID("m"), Kind: show.Beat, Name: show.Beat.Title(), At: at.Add(geom.Pt(-300, 180))}
	s.Modules = append(s.Modules, in, beat)
	s.Wires = append(s.Wires,
		show.Wire{ID: s.NewID("w"), From: show.End{Module: in.ID, Port: "out"}, To: show.End{Module: beat.ID, Port: "in"}},
		show.Wire{ID: s.NewID("w"), From: show.End{Module: beat.ID, Port: "out"}, To: show.End{Module: board, Port: "clock"}})
	return true
}

// recordedFrom puts each scene got on its Board's pad, in place of the
// scene there.
func (a *App) recordedFrom(got []engine.Recorded) {
	if len(got) == 0 {
		return
	}
	s := a.st.Show.Clone()
	for _, r := range got {
		if m := s.Module(r.Module); m != nil && m.Kind == show.Board {
			m.SetScene(r.Scene)
		}
	}
	a.setShow(s)
}

// MaxGain is the most decibels a song's own gain moves it by, up or
// down.
const MaxGain = 12.0

// setShow makes s the show: the engine plays it, it is marked unsaved,
// and the windows show it. Songs not scanned yet are scanned.
func (a *App) setShow(s show.Show) {
	prune(&s)
	a.st.Show, a.st.Unsaved = s, true
	a.eng.Set(s)
	for id, v := range a.moving {
		// A fader held plays on where it is held.
		a.eng.SetValue(id, v)
	}
	a.scanSongs()
	a.publish()
}

// scanSongs scans each song of the show not scanned, nor being
// scanned, nor that failed, a few at a time, each on a goroutine of
// its own.
func (a *App) scanSongs() {
	scan := a.Scan
	if scan == nil {
		scan = player.Scan
	}
	var songs map[string]string
	for _, g := range a.st.Show.Songs {
		if g.Scan != nil || a.st.Songs[g.ID] != "" || a.scanning[g.ID] == g.Path {
			continue
		}
		if songs == nil {
			// The windows hold the map the state had: a new one, not it
			// changed.
			songs = maps.Clone(a.st.Songs)
			a.st.Songs = songs
		}
		a.scanning[g.ID] = g.Path
		songs[g.ID] = Scanning
		go func(id, path string) {
			a.lanes <- struct{}{}
			sc, err := scan(path)
			<-a.lanes
			a.in <- scanned{song: id, path: path, scan: sc, err: err}
		}(g.ID, g.Path)
	}
}

// Scanning is what State.Songs says of a song being scanned.
const Scanning = "Scanning…"

// scanned keeps what a scan found, where the song is still the one it
// scanned.
func (a *App) scanned(m scanned) {
	if a.scanning[m.song] == m.path {
		delete(a.scanning, m.song)
	}
	g := a.st.Show.Song(m.song)
	if g == nil || g.Path != m.path {
		return
	}
	songs := maps.Clone(a.st.Songs)
	delete(songs, m.song)
	if m.err != nil {
		songs[m.song] = m.err.Error()
		a.st.Songs = songs
		a.publish()
		return
	}
	a.st.Songs = songs
	s := a.st.Show.Clone()
	sc := m.scan
	s.Song(m.song).Scan = &sc
	a.setShow(s)
}

// chooseRig asks in window win for a midittt rig to bring over.
func (a *App) chooseRig(win int) {
	w := a.wins[win]
	if w == nil {
		return
	}
	go func() {
		paths, err := w.c.ChooseFiles(context.Background(), driver.ChooseOptions{Title: "Import a midittt rig",
			Filters: []driver.FileFilter{{Name: "midittt rigs", Patterns: []string{"*.json"}}}})
		if err == nil && len(paths) > 0 {
			a.in <- rigPath(paths[0])
		}
	}()
}

// chooseSongs asks in window win for songs to add.
func (a *App) chooseSongs(win int) {
	w := a.wins[win]
	if w == nil {
		return
	}
	patterns := make([]string, len(player.Kinds))
	for i, k := range player.Kinds {
		patterns[i] = "*" + k
	}
	go func() {
		paths, err := w.c.ChooseFiles(context.Background(), driver.ChooseOptions{Title: "Add songs", Multiple: true,
			Filters: []driver.FileFilter{{Name: "Songs", Patterns: patterns}}})
		if err == nil && len(paths) > 0 {
			a.in <- songPaths(paths)
		}
	}()
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
	a.st = State{Show: s, Path: path, Songs: map[string]string{}, Ports: a.st.Ports}
	clear(a.scanning)
	// Another show: what the Boards held and the overrides held go
	// with the one before, whose modules' IDs this one may share.
	a.eng.Forget()
	a.eng.Set(s)
	a.scanSongs()
	a.portsWanted()
	a.syncWindows()
	a.publish()
}

// addBoard adds a Board to s, where it has none, wired into the first
// Mixer's first free input, and reports whether it did.
func addBoard(s *show.Show) bool {
	if slices.ContainsFunc(s.Modules, func(m show.Module) bool { return m.Kind == show.Board }) {
		return false
	}
	b := show.Module{ID: s.NewID("m"), Kind: show.Board, Name: show.Board.Title()}
	i := slices.IndexFunc(s.Modules, func(m show.Module) bool { return m.Kind == show.Mixer })
	if i < 0 {
		s.Modules = append(s.Modules, b)
		return true
	}
	mixer := s.Modules[i]
	b.At = mixer.At.Add(geom.Pt(-300, 340))
	s.Modules = append(s.Modules, b)
	from := show.End{Module: b.ID, Port: "out"}
	ins, _ := engine.Ports(s, &mixer)
	for _, p := range ins {
		to := show.End{Module: mixer.ID, Port: p.ID}
		if ok, _ := engine.CanJoin(s, from, to); ok {
			s.Wires = append(s.Wires, show.Wire{ID: s.NewID("w"), From: from, To: to})
			break
		}
	}
	return true
}

// usb reports whether o is a USB box, on a serial port.
func usb(o show.Output) bool { return o.Device == "enttec" || o.Device == "opendmx" }

// portsWanted looks for the serial ports where a DMX out of the show
// sends to a USB box and they were never looked for.
func (a *App) portsWanted() {
	if a.st.Ports != nil {
		return
	}
	for _, m := range a.st.Show.Modules {
		if m.Kind == show.DMXOut && usb(m.Out) {
			a.findPorts()
			return
		}
	}
}

// findPorts looks for the serial ports on a goroutine of its own, as
// the system may take a moment, and the show plays on meanwhile.
func (a *App) findPorts() {
	if a.finding {
		return
	}
	a.finding = true
	list := a.Serial
	if list == nil {
		list = enttec.ListPorts
	}
	go func() {
		ports, err := list()
		if err != nil {
			log.Printf("crossroads: %v", err)
		}
		if ports == nil {
			ports = []enttec.PortInfo{}
		}
		a.in <- portsFound(ports)
	}()
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
