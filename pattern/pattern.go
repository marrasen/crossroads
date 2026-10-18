// Package pattern plays light patterns drawn on the beat grid.
//
// A Pattern is a few bars of lanes, one for each group or fixture it
// drives, with clips drawn along them: a colour for two beats, a fade
// over a bar, a strobe on the eighths, a motor held at a speed. While a
// pad is held the pattern loops, locked to the beat grid, so beat three
// of the pattern lands on beat three of the bar wherever it was pressed.
// Let go, and it returns its lights to rest or leaves them as they are,
// whichever the pattern says.
//
// Nothing here knows about MIDI, nor the engine: this package works
// out what each lane says at each moment. It came from midittt, which
// crossroads takes over from.
package pattern

import (
	"math"

	"github.com/marrasen/gunim-dmx/fixtures"
)

// BeatsPerBar is the bar a pattern counts in. The beat grid's bars are
// four beats long, and a pattern locked to it is too.
const BeatsPerBar = 4

// MaxBars is the longest a pattern runs before it loops.
const MaxBars = 16

// A Kind is what a clip does.
type Kind string

// The kinds of clip.
const (
	// KindColour lights the lane in a colour, with a fade in and out.
	KindColour Kind = "colour"
	// KindLevel sets the lane's brightness, flat or ramping.
	KindLevel Kind = "level"
	// KindStrobe flashes a colour at a rate tied to the beat.
	KindStrobe Kind = "strobe"
	// KindChannel holds one function at a value, or ramps it: a motor's
	// speed, a gobo, a built-in programme.
	KindChannel Kind = "channel"
)

// Kinds are the kinds in the order the editor offers them.
var Kinds = []Kind{KindColour, KindLevel, KindStrobe, KindChannel}

// A Colour is what a colour or strobe clip lights: red, green and blue
// for the fixtures that mix, and white, amber and UV for those that have
// them, each 0 to 255.
type Colour struct {
	R  uint8 `json:"r,omitempty"`
	G  uint8 `json:"g,omitempty"`
	B  uint8 `json:"b,omitempty"`
	W  uint8 `json:"w,omitempty"`
	A  uint8 `json:"a,omitempty"`
	UV uint8 `json:"uv,omitempty"`
}

// Brightness is the colour's strongest part, which is what a fixture
// with only a dimmer can show of it.
func (c Colour) Brightness() int {
	return int(max(c.R, c.G, c.B, c.W, c.A, c.UV))
}

// A Clip is one thing drawn on a lane: where it starts and how long it
// lasts, in beats from the start of the pattern, and what it does.
type Clip struct {
	ID     string  `json:"id"`
	Start  float64 `json:"start"`
	Length float64 `json:"length"`
	Kind   Kind    `json:"kind"`
	// Colour is what a colour or strobe clip lights.
	Colour Colour `json:"colour,omitzero"`
	// FadeIn and FadeOut, in beats, bring a colour clip up from nothing
	// and back down to it.
	FadeIn  float64 `json:"fadeIn,omitempty"`
	FadeOut float64 `json:"fadeOut,omitempty"`
	// Rate is how many flashes a strobe clip makes a beat: 1, 2, 4.
	Rate float64 `json:"rate,omitempty"`
	// From and To are where a level or channel clip starts and ends,
	// 0 to 255. The same number twice holds it.
	From int `json:"from,omitempty"`
	To   int `json:"to,omitempty"`
	// Role is the function a channel clip drives. On a fixture lane a
	// clip may name the channel by number instead, in Target, for one
	// whose function has no name.
	Role   fixtures.Role `json:"role,omitempty"`
	Target int           `json:"target,omitempty"`
}

// End is the beat the clip stops at.
func (c Clip) End() float64 { return c.Start + c.Length }

// A Lane is one row of a pattern: the group or the fixture its clips
// drive.
type Lane struct {
	ID string `json:"id"`
	// GroupID or FixtureID names what the lane drives, one of the two.
	GroupID   string `json:"groupId,omitempty"`
	FixtureID string `json:"fixtureId,omitempty"`
	Clips     []Clip `json:"clips"`
}

// A Release says what a pattern does when its pad is let go.
type Release struct {
	// Rest returns the channels the pattern drove to their resting
	// values, over FadeMs. Without it they are left as they are, so the
	// pattern that follows takes over with no gap of dark between.
	Rest   bool    `json:"rest,omitempty"`
	FadeMs float64 `json:"fadeMs,omitempty"`
}

// A Pattern is a loop of lanes drawn on the beat grid.
type Pattern struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Bars is how long the pattern runs before it loops.
	Bars    int     `json:"bars"`
	Lanes   []Lane  `json:"lanes"`
	Release Release `json:"release"`
}

// DisplayName is the pattern's name, or a stand-in when it has none.
func (p Pattern) DisplayName() string {
	if p.Name == "" {
		return "Untitled pattern"
	}
	return p.Name
}

// Length is the pattern's loop in beats.
func (p Pattern) Length() float64 {
	return float64(min(max(p.Bars, 1), MaxBars) * BeatsPerBar)
}

// A Write is one channel of one fixture a pattern wants at a value:
// the channel by its offset in the fixture, from 1, and by its slot in
// the universe.
type Write struct {
	FixtureID string
	Offset    int
	Slot      int
	Value     int
}

// Members are the fixtures a lane drives, in order.
func Members(l Lane, patches []fixtures.Patch, groups []fixtures.Group) []fixtures.Patch {
	if l.FixtureID != "" {
		if p, ok := fixtures.FindPatch(patches, l.FixtureID); ok {
			return []fixtures.Patch{p}
		}
		return nil
	}
	if g, ok := fixtures.FindGroup(groups, l.GroupID); ok {
		return fixtures.GroupMembers(g, patches)
	}
	return nil
}

// ValuesAt is what every lane of a pattern says at pos beats into it.
//
// Within a lane a later clip wins on a channel two clips share, and a
// clip that lights a colour asks for the fixture's dimmer at full only
// where nothing else on the lane sets it, so a level clip under a
// colour sets how bright the colour is.
func ValuesAt(p Pattern, pos float64, patches []fixtures.Patch, groups []fixtures.Group, cat fixtures.Catalog) []Write {
	var out []Write
	for _, l := range p.Lanes {
		members := Members(l, patches, groups)
		if len(members) == 0 {
			continue
		}
		var weak, strong []Write
		for _, c := range l.Clips {
			if pos < c.Start || pos >= c.End() {
				continue
			}
			for _, m := range members {
				w, s := clipWrites(c, m, pos, l.FixtureID != "", cat)
				weak, strong = append(weak, w...), append(strong, s...)
			}
		}
		out = append(append(out, weak...), strong...)
	}
	return Merge(out)
}

// Merge keeps the last write to each channel, in the order of first
// appearance, so later writes win.
func Merge(ws []Write) []Write {
	type key struct {
		id     string
		offset int
	}
	at := map[key]int{}
	var out []Write
	for _, w := range ws {
		k := key{w.FixtureID, w.Offset}
		if i, ok := at[k]; ok {
			out[i] = w
			continue
		}
		at[k] = len(out)
		out = append(out, w)
	}
	return out
}

// clipWrites is what one clip does to one fixture at pos: the dimmer a
// colour asks for, which anything else may override, and the rest.
func clipWrites(c Clip, p fixtures.Patch, pos float64, fixtureLane bool, cat fixtures.Catalog) (weak, strong []Write) {
	t := 0.0
	if c.Length > 0 {
		t = min(max((pos-c.Start)/c.Length, 0), 1)
	}
	write := func(to *[]Write, r fixtures.Resolved, value int) {
		if !r.InUniverse {
			return
		}
		*to = append(*to, Write{FixtureID: p.ID, Offset: r.Offset, Slot: r.Slot, Value: min(max(value, 0), 255)})
	}
	switch c.Kind {
	case KindColour, KindStrobe:
		factor := envelope(c, pos)
		if c.Kind == KindStrobe && !flashOn(c, pos) {
			factor = 0
		}
		lit := false
		for _, ch := range cat.ResolvedChannels(p) {
			part, ok := colourPart(ch.Channel.Role, c.Colour)
			if !ok {
				continue
			}
			lit = true
			write(&strong, ch, int(math.Round(float64(part)*factor)))
		}
		if d, ok := cat.ChannelForRole(p, fixtures.RoleDimmer); ok {
			if lit {
				write(&weak, d, 255)
			} else {
				// A fixture with no colour to mix shows the colour as
				// brightness.
				write(&strong, d, int(math.Round(float64(c.Colour.Brightness())*factor)))
			}
		}
	case KindLevel:
		v := lerp(c.From, c.To, t)
		if d, ok := cat.ChannelForRole(p, fixtures.RoleDimmer); ok {
			write(&strong, d, v)
			break
		}
		// No dimmer: the level is white light, in the white channel if
		// there is one and in red, green and blue together if not.
		if w, ok := cat.ChannelForRole(p, fixtures.RoleWhite); ok {
			write(&strong, w, v)
			break
		}
		for _, r := range []fixtures.Role{fixtures.RoleRed, fixtures.RoleGreen, fixtures.RoleBlue} {
			if ch, ok := cat.ChannelForRole(p, r); ok {
				write(&strong, ch, v)
			}
		}
	case KindChannel:
		v := lerp(c.From, c.To, t)
		switch {
		case c.Role != "":
			if ch, ok := cat.ChannelForRole(p, c.Role); ok {
				write(&strong, ch, v)
			}
		case fixtureLane && c.Target > 0:
			for _, r := range cat.ResolvedChannels(p) {
				if r.Offset == c.Target {
					write(&strong, r, v)
				}
			}
		}
	}
	return weak, strong
}

// colourPart is what a channel of a role takes of a colour, and false
// for a channel that makes no colour. A colour channel the clip's colour
// has no part for goes dark, or it would tint the colour picked.
func colourPart(r fixtures.Role, c Colour) (uint8, bool) {
	switch r {
	case fixtures.RoleRed:
		return c.R, true
	case fixtures.RoleGreen:
		return c.G, true
	case fixtures.RoleBlue:
		return c.B, true
	case fixtures.RoleWhite:
		return c.W, true
	case fixtures.RoleAmber:
		return c.A, true
	case fixtures.RoleUV:
		return c.UV, true
	case fixtures.RoleLime, fixtures.RoleCyan, fixtures.RoleMagenta, fixtures.RoleYellow:
		return 0, true
	case fixtures.RoleDimmer, fixtures.RoleColorWheel, fixtures.RoleStrobe, fixtures.RolePan, fixtures.RolePanFine,
		fixtures.RoleTilt, fixtures.RoleTiltFine, fixtures.RoleZoom, fixtures.RoleFocus, fixtures.RoleIris, fixtures.RoleGobo,
		fixtures.RoleGoboRotation, fixtures.RolePrism, fixtures.RoleSpeed, fixtures.RoleProgram, fixtures.RoleControl:
	}
	return 0, false
}

// envelope is how far up a colour clip is at pos: rising over its fade
// in, falling over its fade out, full between.
func envelope(c Clip, pos float64) float64 {
	f := 1.0
	if c.FadeIn > 0 {
		f = min(f, (pos-c.Start)/c.FadeIn)
	}
	if c.FadeOut > 0 {
		f = min(f, (c.End()-pos)/c.FadeOut)
	}
	return min(max(f, 0), 1)
}

// flashOn reports whether a strobe is lit at pos: the first half of
// every flash, counted from the clip's start, so flashes land on the
// beat.
func flashOn(c Clip, pos float64) bool {
	rate := c.Rate
	if rate <= 0 {
		rate = 1
	}
	phase := (pos - c.Start) * rate
	return phase-math.Floor(phase) < 0.5
}

func lerp(from, to int, t float64) int {
	return int(math.Round(float64(from) + float64(to-from)*t))
}
