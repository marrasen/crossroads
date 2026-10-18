// Package midittt reads midittt's saved rigs, every version of them, as
// midittt itself does, so crossroads can bring a rig over. It came from
// midittt's own config package, which crossroads takes over from.
package midittt

import (
	"fmt"
	"os"

	"github.com/marrasen/crossroads/fx"
	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/gunim-dmx/fixtures"
	gtempo "github.com/marrasen/gunim/audio/tempo"
)

// FileVersion is the version stamped on a file written now.
const FileVersion = 7

// appName is the marker that says a file is one of ours.
const appName = "midittt"

// LinkSettings are the saved Ableton Link preferences, applied to the
// session on startup.
type LinkSettings struct {
	// Enabled says whether to join the session.
	Enabled bool
	// Quantum is the beats per bar Link reports its beat and phase
	// against.
	Quantum float64
	// FollowStartStop says whether Link's transport gates the beat
	// clock. Off, the default, treats the session as always playing.
	// Start and stop sync is optional in Link and plenty of peers never
	// publish it — a session on its own reports stopped forever — so
	// following it out of the box would leave the lights dark for most
	// users.
	FollowStartStop bool
	// Drive lets midittt propose the session's tempo and transport,
	// rather than only following them. Off by default: a rig where
	// Live or Serato holds the timeline should keep it.
	Drive bool
}

// MidiClockSettings say whether and where to re-emit the selected tempo
// source as MIDI clock.
type MidiClockSettings struct {
	Enabled bool
	// DeviceName is the output to send the clock to. Empty means none
	// has been chosen.
	DeviceName string
}

// MidiClockInSettings say whether and where midittt takes its tempo
// from MIDI clock, which is how it follows a drum machine or a DAW
// that has no Link.
type MidiClockInSettings struct {
	Enabled bool
	// DeviceName is the input to read the clock from. Empty means none
	// has been chosen.
	DeviceName string
}

// DefaultLink and DefaultMidiClock are what a file that names neither
// gets.
var (
	DefaultLink      = LinkSettings{Quantum: 4}
	DefaultMidiClock = MidiClockSettings{}
)

// A Config is everything midittt saves: the routes and everything they
// can target, plus the two settings groups.
type Config struct {
	Routes   []routing.Route
	Timers   []Timer
	Fixtures []fixtures.Patch
	// Patterns are loops of light drawn on the beat grid, played while a
	// pad is held.
	Patterns []pattern.Pattern
	// FixtureTypes are the types the rig made for itself, beside the
	// built-in library: a generic RGBW par, say.
	FixtureTypes []fixtures.Definition
	Groups       []fixtures.Group
	Programs     []fx.Program
	Link         LinkSettings
	MidiClock    MidiClockSettings
	MidiClockIn  MidiClockInSettings
	// TempoPriority ranks the tempo sources, best first. The lights run
	// on the highest ranked one that is connected.
	TempoPriority []string
	// Song is the recording the rig was last playing along to, which
	// opens again with the rig, and Songs the grids of every song whose
	// grid was corrected or cued against.
	Song  string
	Songs []Song
}

// A Song is what a rig keeps of one recording: its musical time, as it
// was corrected, so the bars its cues name stay where they were.
type Song struct {
	// Name is the recording's file name, without the folder, so a rig
	// moved to another machine still finds it.
	Name string
	Grid *gtempo.Map
}

// A TimerMode says which way a midittt timer counts.
type TimerMode string

// The two modes: a countdown counts down from a duration, a stopwatch
// counts up.
const (
	Countdown TimerMode = "timer"
	Stopwatch TimerMode = "stopwatch"
)

// A Timer is one of a rig's timers.
type Timer struct {
	ID         string
	Name       string
	Mode       TimerMode
	DurationMs float64
}

// New is an empty configuration with the default settings.
func New() Config {
	return Config{Link: DefaultLink, MidiClock: DefaultMidiClock}
}

// Load reads the rig at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	c, err := Unmarshal(data)
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return c, nil
}
