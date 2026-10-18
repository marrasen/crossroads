package midittt

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/geom"
)

// A Rebuilt is a midittt rig as a crossroads show, and what of it the
// show does not do as midittt did, each a line to show the user. A rig
// with no notes is rebuilt whole.
type Rebuilt struct {
	Show  show.Show
	Notes []string
}

// The tempo sources midittt ranks, by its IDs for them.
const (
	sourceLink   = "link"
	sourceSerato = "serato"
	sourceClock  = "midi-clock"
	sourceManual = "manual"
	sourceSong   = "song"
)

// Rebuild builds the show that plays rig c, called name: its lights,
// their types and groups; its patterns, effect programmes and timers; a
// tempo from the source it ranks first of those it has; and every route
// in one MIDI routes module, into a mixer that sends to an ENTTEC box
// and shows on the stage.
func Rebuild(c Config, name string) Rebuilt {
	var out Rebuilt
	note := func(format string, args ...any) { out.Notes = append(out.Notes, fmt.Sprintf(format, args...)) }
	s := show.Show{Name: name, Stage: show.Stage{Width: 8, Depth: 5, Spots: map[string]show.Spot{}}}
	s.Rig.Lights = slices.Clone(c.Fixtures)
	s.Rig.Types = slices.Clone(c.FixtureTypes)
	s.Rig.Groups = slices.Clone(c.Groups)
	s.Patterns = slices.Clone(c.Patterns)
	s.Programs = slices.Clone(c.Programs)
	// The lights in a row across the stage, as midittt kept no places.
	for i, p := range s.Rig.Lights {
		s.Stage.Spots[p.ID] = show.Spot{X: s.Stage.Width * (float32(i) + 0.5) / float32(len(s.Rig.Lights)), Y: s.Stage.Depth / 2}
	}
	used := map[string]bool{}
	for _, m := range c.Fixtures {
		used[m.ID] = true
	}
	id := func(prefix string) string {
		for {
			n := s.NewID(prefix)
			if !used[n] {
				used[n] = true
				return n
			}
		}
	}
	add := func(m show.Module) string {
		if m.ID == "" {
			m.ID = id("m")
		}
		used[m.ID] = true
		if m.Name == "" {
			m.Name = m.Kind.Title()
		}
		s.Modules = append(s.Modules, m)
		return m.ID
	}
	wire := func(from, fp, to, tp string) {
		s.Wires = append(s.Wires, show.Wire{ID: id("w"), From: show.End{Module: from, Port: fp}, To: show.End{Module: to, Port: tp}})
	}

	// The tempo: midittt drove the lights from the best source it had,
	// ranked; the Tempo follows the first of them the rig has, and
	// plays its own where none plays.
	tempo := add(show.Module{Kind: show.Tempo, BPM: 120, At: geom.Pt(300, 0)})
	sources := map[string]string{}
	if c.Link.Enabled {
		sources[sourceLink] = add(show.Module{Kind: show.Link, At: geom.Pt(0, 0)})
		if c.Link.Drive {
			note("Ableton Link: midittt proposed its tempo to the session; crossroads follows the session, and proposes nothing.")
		}
		if c.Link.FollowStartStop {
			note("Ableton Link: midittt followed the session's start and stop; crossroads plays the session as always playing.")
		}
	}
	if c.MidiClockIn.Enabled && c.MidiClockIn.DeviceName != "" {
		sources[sourceClock] = add(show.Module{Kind: show.ClockIn, Device: c.MidiClockIn.DeviceName, At: geom.Pt(0, 120)})
	}
	priority := c.TempoPriority
	if len(priority) == 0 {
		priority = []string{sourceLink, sourceSerato, sourceClock, sourceManual}
	}
	if slices.Contains(priority, sourceSerato) {
		sources[sourceSerato] = add(show.Module{Kind: show.Serato, At: geom.Pt(0, 240)})
	}
	var following []string
	for _, p := range priority {
		if src := sources[p]; src != "" {
			following = append(following, p)
		}
	}
	if len(following) > 0 {
		wire(sources[following[0]], "out", tempo, "in")
		if len(following) > 1 {
			note("Tempo: midittt moved down its ranking (%s) as a source went quiet; crossroads' Tempo follows %s alone.",
				strings.Join(following, ", "), following[0])
		}
	}
	if slices.Contains(priority, sourceSong) || c.Song != "" || len(c.Songs) > 0 {
		note("Songs: midittt's song player and its corrected grids do not come over; crossroads' Player tab scans its songs afresh.")
	}

	// The light: the routes and the stage through one mixer.
	mixer := add(show.Module{Kind: show.Mixer, Value: 1, At: geom.Pt(900, 200)})
	box := add(show.Module{Kind: show.DMXOut, Name: "ENTTEC", Out: show.Output{Device: "enttec"}, At: geom.Pt(1200, 140)})
	view := add(show.Module{Kind: show.StageView, Name: "Stage", At: geom.Pt(1200, 300)})
	wire(mixer, "out", box, "in")
	wire(mixer, "out", view, "in")

	for _, t := range c.Timers {
		add(show.Module{ID: t.ID, Kind: show.Timer, Name: t.Name, Seconds: t.DurationMs / 1000, Stopwatch: t.Mode == Stopwatch,
			At: geom.Pt(600, 400+float32(len(s.Modules))*10)})
	}
	timers := map[string]bool{}
	for _, t := range c.Timers {
		timers[t.ID] = true
	}

	routes := slices.Clone(c.Routes)
	// Each route an ID of its own, as a rig edited by hand may share
	// one, and a route is turned on and off, and holds what it holds, by
	// its ID.
	seen := map[string]bool{}
	for i := range routes {
		r := &routes[i]
		base := r.ID
		for n := 2; r.ID == "" || seen[r.ID]; n++ {
			r.ID = base + "-" + strconv.Itoa(n)
		}
		seen[r.ID] = true
	}
	if len(routes) > 0 {
		r := add(show.Module{Kind: show.Routes, Name: "MIDI routes", Routes: routes, At: geom.Pt(600, 140)})
		wire(tempo, "out", r, "clock")
		wire(r, "out", mixer, "in1")
	}
	patterns := map[string]bool{}
	for _, p := range c.Patterns {
		patterns[p.ID] = true
	}
	programs := map[string]bool{}
	for _, p := range c.Programs {
		programs[p.ID] = true
	}
	lights := map[string]bool{}
	for _, p := range c.Fixtures {
		lights[p.ID] = true
	}
	groups := map[string]bool{}
	for _, g := range c.Groups {
		groups[g.ID] = true
	}
	for _, r := range routes {
		name := r.DisplayName()
		switch {
		case r.Cue != nil:
			note("Route %q fires on a song cue, which crossroads does not hear: it never fires.", name)
		case r.Link != "" && !c.Link.Enabled:
			note("Route %q fires as Link starts or stops, but the rig had Link off: it fires once Link is added.", name)
		}
		o := r.Output
		switch o.Kind {
		case routing.ToTimer:
			if !timers[o.TimerID] {
				note("Route %q drives a timer the rig no longer has.", name)
			}
		case routing.ToPattern:
			if !patterns[o.PatternID] {
				note("Route %q plays a pattern the rig no longer has.", name)
			}
		case routing.ToFx:
			if !programs[o.ProgramID] {
				note("Route %q fires an effect programme the rig no longer has.", name)
			}
		case routing.ToFixture:
			if !lights[o.FixtureID] {
				note("Route %q sets a light the rig no longer has.", name)
			}
		case routing.ToGroup:
			if !groups[o.GroupID] {
				note("Route %q sets a group the rig no longer has.", name)
			}
		case routing.ToMidi:
			if o.DeviceName == "" {
				note("Route %q sends MIDI to no device: midittt's oldest files named devices it cannot be told of.", name)
			}
		case routing.ToLink:
			if !c.Link.Enabled {
				note("Route %q moves Link's transport, but the rig had Link off: it does nothing until Link is added.", name)
			}
		case routing.ToDmx:
		}
	}

	if c.MidiClock.Enabled && c.MidiClock.DeviceName != "" {
		out := add(show.Module{Kind: show.ClockOut, Device: c.MidiClock.DeviceName, At: geom.Pt(600, 0)})
		wire(tempo, "out", out, "in")
	}

	s.Layout = show.DefaultLayout()
	s.Layout.Show(show.TabMIDI)
	s.Layout.Show(show.TabLive)
	s.Mend()
	out.Show = s
	return out
}

// NameOf is the name a show rebuilt from the rig at path is given: its
// file's name, without the ending.
func NameOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
