// Package show is a crossroads show: the lights of the rig and where
// they hang on the stage, the modules and the wires between them, and
// how the windows and tabs were laid out. It is what a show file
// holds, read and written as JSON.
package show

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget/dock"
)

// A Show is everything a show file holds.
type Show struct {
	// Name is the show's, as "Friday at the Dome".
	Name string `json:"name"`
	Rig  Rig    `json:"rig"`
	// Stage is the room the lights hang in, and where each hangs.
	Stage Stage `json:"stage"`
	// Modules are the show's modules, and Wires what joins them.
	Modules []Module `json:"modules"`
	Wires   []Wire   `json:"wires"`
	// Songs are the playlist, in the order it plays.
	Songs []Song `json:"songs,omitempty"`
	// Layout is where the tabs stood, in which windows.
	Layout dock.Layout `json:"layout"`
	// Next counts the IDs made, to make the next one.
	Next int `json:"next"`
}

// A Rig is the lights of a show: each at its DMX address, and the
// show's own types of light.
type Rig struct {
	Lights []fixtures.Patch      `json:"lights"`
	Types  []fixtures.Definition `json:"types,omitempty"`
}

// A Stage is the room the lights hang in, in metres, and where each
// light hangs, by its ID.
type Stage struct {
	Width float32         `json:"width"`
	Depth float32         `json:"depth"`
	Spots map[string]Spot `json:"spots,omitempty"`
}

// A Spot is where a light hangs on the stage, in metres from the
// stage's front left corner, seen from above.
type Spot struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

// A Kind is what a module is.
type Kind string

// The kinds of module.
const (
	// Fader is a fader on screen, a value from 0 to 1.
	Fader Kind = "fader"
	// Lights plays a set of lights: a value for each thing they do.
	Lights Kind = "lights"
	// Mixer merges light into a universe, under a grand master.
	Mixer Kind = "mixer"
	// DMXOut sends a universe to a DMX box.
	DMXOut Kind = "dmxout"
	// StageView shows a universe on the Stage tab.
	StageView Kind = "stage"
	// AudioIn listens to a sound card's input.
	AudioIn Kind = "audioin"
	// Beat follows the beat of sound, as a clock.
	Beat Kind = "beat"
	// Tempo is a clock of its own, tapped or typed in, or following the
	// clock wired into it.
	Tempo Kind = "tempo"
	// Level is how loud sound is, as a value.
	Level Kind = "level"
	// Pulse is a value that flashes on the beat of a clock and fades.
	Pulse Kind = "pulse"
	// Player plays the playlist: its sound, a clock from each song's
	// beats, and a trigger as each song starts and ends and each bar.
	Player Kind = "player"
	// Button is a button on screen, which fires a trigger.
	Button Kind = "button"
)

// Kinds are the kinds of module, in the order the Routes tab offers
// them.
var Kinds = []Kind{Fader, Lights, Mixer, DMXOut, StageView, AudioIn, Beat, Tempo, Level, Pulse, Player, Button}

// Title is what a module of kind k is called.
func (k Kind) Title() string {
	switch k {
	case Fader:
		return "Fader"
	case Lights:
		return "Lights"
	case Mixer:
		return "Mixer"
	case DMXOut:
		return "DMX out"
	case StageView:
		return "Stage view"
	case AudioIn:
		return "Audio in"
	case Beat:
		return "Beat"
	case Tempo:
		return "Tempo"
	case Level:
		return "Level"
	case Pulse:
		return "Pulse"
	case Player:
		return "Player"
	case Button:
		return "Button"
	}
	return string(k)
}

// A Module is one module of the show: its kind, its name, where its box
// stands in the Routes tab, and its settings.
type Module struct {
	ID   string     `json:"id"`
	Kind Kind       `json:"kind"`
	Name string     `json:"name"`
	At   geom.Point `json:"at"`
	// Value is a fader's value, and a mixer's grand master, from 0 to
	// 1.
	Value float64 `json:"value,omitempty"`
	// Lights are the lights a Lights module plays, by their IDs, in
	// order.
	Lights []string `json:"lights,omitempty"`
	// Out is where a DMX out sends.
	Out Output `json:"out,omitzero"`
	// Input is the sound card input an Audio in listens to, by its
	// ID, "" for the system's default.
	Input string `json:"input,omitempty"`
	// BPM is a Tempo's own tempo, which it plays where Manual is set or
	// no clock is wired into it.
	BPM    float64 `json:"bpm,omitempty"`
	Manual bool    `json:"manual,omitempty"`
	// Every is how many beats a Pulse flashes every, 1 where it is 0.
	Every float64 `json:"every,omitempty"`
}

// An Output is a DMX box a DMX out sends to.
type Output struct {
	// Device is "", sending nowhere, "enttec" for an ENTTEC DMX USB Pro
	// or one like it, "opendmx" for an ENTTEC Open DMX, "artnet" or
	// "sacn".
	Device string `json:"device,omitempty"`
	// Port is the serial port of a USB box, "" to find a Pro on its
	// own; Host the IP address of an Art-Net node, or of an sACN
	// receiver, "" to send sACN to its universe's group.
	Port string `json:"port,omitempty"`
	Host string `json:"host,omitempty"`
	// Universe is the Art-Net port address, or the sACN universe.
	Universe int `json:"universe,omitempty"`
}

// A Song is one song of the playlist: the file it plays, and what its
// scan found in it.
type Song struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	// BPM, where it is set, is the tempo the song plays its lights at
	// over the one its scan found, from the first beat it found.
	BPM float64 `json:"bpm,omitempty"`
	// Gain is decibels the song plays louder by, over what brings it
	// to the target loudness.
	Gain float64 `json:"gain,omitempty"`
	// Scan is what a scan of the file found, nil before one.
	Scan *Scan `json:"scan,omitempty"`
}

// A Scan is what a scan of a song found: how long it is, its beats,
// and how loud it is.
type Scan struct {
	// Length is in seconds.
	Length float64 `json:"length"`
	// BPM is the tempo found, and Beats where each beat falls, in
	// seconds; Downbeat is the beat of Beats the first bar starts on,
	// of PerBar beats a bar. A song with no beat found has none.
	BPM      float64   `json:"bpm,omitempty"`
	Beats    []float64 `json:"beats,omitempty"`
	Downbeat int       `json:"downbeat,omitempty"`
	PerBar   int       `json:"perBar,omitempty"`
	// LUFS is the song's integrated loudness, and Loud false for a
	// song too short or quiet to measure. TruePeak is its loudest
	// point, in dBTP.
	LUFS     float64 `json:"lufs"`
	Loud     bool    `json:"loud"`
	TruePeak float64 `json:"truePeak"`
}

// Target is the loudness each song is brought to, in LUFS, and Ceiling
// the loudest its true peak may then be, in dBTP.
const (
	Target  = -14.0
	Ceiling = -1.0
)

// Loudness is how many decibels song g plays louder by: what brings it
// to the target loudness, no further than keeps its true peak under the
// ceiling, and the song's own gain on top.
func (g *Song) Loudness() float64 {
	var db float64
	if g.Scan != nil && g.Scan.Loud {
		db = min(Target-g.Scan.LUFS, Ceiling-g.Scan.TruePeak)
	}
	return db + g.Gain
}

// Clock is the tempo song g plays its lights at, and how many beats
// into it t seconds are, counted from its first bar: its beats as
// found, or, where its BPM is set or none were found, a steady beat
// from the first. Before the first beat the count is less than 0.
func (g *Song) Clock(t float64) (bpm, beats float64) {
	var b []float64
	start := 0
	if g.Scan != nil {
		b, start = g.Scan.Beats, g.Scan.Downbeat
		bpm = g.Scan.BPM
	}
	if g.BPM > 0 || len(b) < 2 {
		if g.BPM > 0 {
			bpm = g.BPM
		}
		if bpm <= 0 {
			return 0, 0
		}
		first := 0.0
		if len(b) > start {
			first = b[start]
		}
		return bpm, (t - first) * bpm / 60
	}
	start = min(start, len(b)-2)
	// The beat t falls after, and the one after it; before the first
	// and after the last, the nearest two, the beat going on.
	i, _ := slices.BinarySearch(b, t)
	i = min(max(i-1, 0), len(b)-2)
	return bpm, float64(i-start) + (t-b[i])/(b[i+1]-b[i])
}

// Song returns song id of the playlist, or nil.
func (s *Show) Song(id string) *Song {
	if i := slices.IndexFunc(s.Songs, func(g Song) bool { return g.ID == id }); i >= 0 {
		return &s.Songs[i]
	}
	return nil
}

// An End is one end of a wire: a module, and a port of it.
type End struct {
	Module string `json:"module"`
	Port   string `json:"port"`
}

// A Wire joins an output of one module, From, to an input of another,
// To.
type Wire struct {
	ID   string `json:"id"`
	From End    `json:"from"`
	To   End    `json:"to"`
}

// NewID returns an ID no part of s has yet, with prefix.
func (s *Show) NewID(prefix string) string {
	s.Next++
	return prefix + strconv.Itoa(s.Next)
}

// Module returns module id, or nil.
func (s *Show) Module(id string) *Module {
	if i := slices.IndexFunc(s.Modules, func(m Module) bool { return m.ID == id }); i >= 0 {
		return &s.Modules[i]
	}
	return nil
}

// Light returns light id of the rig, or nil.
func (s *Show) Light(id string) *fixtures.Patch {
	if i := slices.IndexFunc(s.Rig.Lights, func(p fixtures.Patch) bool { return p.ID == id }); i >= 0 {
		return &s.Rig.Lights[i]
	}
	return nil
}

// Catalog is the types of light the show knows: the library's, and
// its own.
func (s *Show) Catalog() fixtures.Catalog { return fixtures.Catalog{Own: s.Rig.Types} }

// Clone returns a deep copy of s.
func (s *Show) Clone() Show {
	c := *s
	c.Rig.Lights = slices.Clone(s.Rig.Lights)
	c.Rig.Types = slices.Clone(s.Rig.Types)
	if s.Stage.Spots != nil {
		c.Stage.Spots = make(map[string]Spot, len(s.Stage.Spots))
		for k, v := range s.Stage.Spots {
			c.Stage.Spots[k] = v
		}
	}
	c.Modules = slices.Clone(s.Modules)
	for i := range c.Modules {
		c.Modules[i].Lights = slices.Clone(c.Modules[i].Lights)
	}
	c.Wires = slices.Clone(s.Wires)
	c.Songs = slices.Clone(s.Songs)
	c.Layout = s.Layout.Clone()
	return c
}

// RemoveModule takes module id out, with the wires to and from it.
func (s *Show) RemoveModule(id string) {
	s.Modules = slices.DeleteFunc(s.Modules, func(m Module) bool { return m.ID == id })
	s.Wires = slices.DeleteFunc(s.Wires, func(w Wire) bool { return w.From.Module == id || w.To.Module == id })
}

// RemoveLight takes light id out of the rig, off the stage and out of
// every Lights module.
func (s *Show) RemoveLight(id string) {
	s.Rig.Lights = slices.DeleteFunc(s.Rig.Lights, func(p fixtures.Patch) bool { return p.ID == id })
	delete(s.Stage.Spots, id)
	for i := range s.Modules {
		s.Modules[i].Lights = slices.DeleteFunc(s.Modules[i].Lights, func(l string) bool { return l == id })
	}
}

// Load reads the show at path.
func Load(path string) (Show, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Show{}, err
	}
	var s Show
	if err := json.Unmarshal(b, &s); err != nil {
		return Show{}, fmt.Errorf("reading %s: %w", path, err)
	}
	s.Mend()
	return s, nil
}

// Save writes s to path, whole or not at all: to a file beside it
// first, which then takes its place.
func (s *Show) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "\t")
	if err != nil {
		return err
	}
	tmp := path + ".saving"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}

// Mend makes s whole, as one read from a file another build wrote: the
// stage has a size, wires join modules that are there, and every tab
// is in the layout.
func (s *Show) Mend() {
	if s.Stage.Width <= 0 || s.Stage.Depth <= 0 {
		s.Stage.Width, s.Stage.Depth = 8, 5
	}
	s.Wires = slices.DeleteFunc(s.Wires, func(w Wire) bool { return s.Module(w.From.Module) == nil || s.Module(w.To.Module) == nil })
	for i := range s.Modules {
		s.Modules[i].Lights = slices.DeleteFunc(s.Modules[i].Lights, func(id string) bool { return s.Light(id) == nil })
	}
	if s.Layout.Window(0) != nil {
		// Every tab is somewhere, in the main window where it was not.
		for _, t := range Tabs {
			if _, ok := s.Layout.Where(t); !ok {
				s.Layout.Add(t, dock.Target{Index: -1})
			}
		}
	}
}
