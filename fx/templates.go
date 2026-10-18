package fx

import (
	"slices"

	"github.com/marrasen/gunim-dmx/fixtures"
)

// Starter FX programmes.
//
// A template describes a move in terms of fixture roles — the dimmer,
// the red — rather than channel offsets, so the same template can be
// dropped onto whatever is patched. Choosing one produces ordinary
// Steps bound to real fixtures, which the user is then free to edit;
// nothing about a programme remembers it came from a template.

// A move is what a template step does, without saying which channel
// does it.
type move struct {
	DelayMs         float64
	From, To        int
	DurationMs      float64
	ScaleByVelocity bool
}

// A TemplateStep is one move within a template, targeting a fixture by
// function.
type TemplateStep struct {
	// Roles are candidates in order of preference. The first one the
	// fixture really has is driven; a fixture with none of them skips
	// the step. That is what lets one template cover a dimmer-plus-
	// colour mover and a bare RGB par.
	Roles []fixtures.Role
	move
}

// A Template is one starter move.
type Template struct {
	ID string
	// Name shows on the card, and becomes the new programme's name.
	Name string
	// Description is one line on what the move looks like on stage.
	Description string
	// Requires are the roles without which the move is not itself. A
	// fixture offering none of them is left out entirely, so "Pan
	// sweep" on a fixed wash does nothing rather than quietly snapping
	// its dimmer up and calling that a sweep. Empty means every role
	// the template touches.
	Requires []fixtures.Role
	// ChaseOffsetMs pushes each successive fixture later, turning one
	// move into a chase along the selection. Zero fires them together.
	ChaseOffsetMs float64
	Steps         []TemplateStep
}

// firstOf is a step driving the first of roles the fixture has.
func firstOf(roles []fixtures.Role, m move) TemplateStep {
	return TemplateStep{Roles: roles, move: m}
}

// each is one step per role, so a fixture makes the same move on all of
// them.
func each(roles []fixtures.Role, m move) []TemplateStep {
	out := make([]TemplateStep, len(roles))
	for i, r := range roles {
		out[i] = TemplateStep{Roles: []fixtures.Role{r}, move: m}
	}
	return out
}

// whiteMix are the colour channels that add up to white. Driving all of
// them together is how a template stays white on an RGB par and on an
// RGBW one alike.
var whiteMix = []fixtures.Role{fixtures.RoleWhite, fixtures.RoleRed, fixtures.RoleGreen, fixtures.RoleBlue}

// intensity is anything usable as a master level, best choice first.
var intensity = []fixtures.Role{fixtures.RoleDimmer, fixtures.RoleWhite, fixtures.RoleRed,
	fixtures.RoleGreen, fixtures.RoleBlue, fixtures.RoleAmber}

// emitters is every channel that makes light, for killing output.
var emitters = []fixtures.Role{fixtures.RoleDimmer, fixtures.RoleWhite, fixtures.RoleRed,
	fixtures.RoleGreen, fixtures.RoleBlue, fixtures.RoleAmber, fixtures.RoleUV, fixtures.RoleStrobe}

// full snaps a channel to full, as hard as the pad was hit.
func full() move { return move{To: 255, ScaleByVelocity: true} }

func snapAt(to int, delayMs float64) move {
	return move{To: to, DelayMs: delayMs, ScaleByVelocity: true}
}

func fade(from, to int, durationMs, delayMs float64) move {
	return move{From: from, To: to, DurationMs: durationMs, DelayMs: delayMs, ScaleByVelocity: true}
}

// flashes is count on-and-off pairs, one every periodMs, each lasting
// onMs.
func flashes(count int, periodMs, onMs float64) []move {
	out := make([]move, 0, 2*count)
	for i := range count {
		out = append(out, snapAt(255, float64(i)*periodMs), move{To: 0, DelayMs: float64(i)*periodMs + onMs})
	}
	return out
}

// Templates are the starters offered when creating a programme, in the
// order they are shown. Ones needing channels a rig may not have — pan,
// tilt — sit last, since they produce nothing on a fixed wash.
var Templates = []Template{
	{
		ID: "white-flash", Name: "White flash",
		Description: "Snaps to full white and fades away over 400 ms.",
		Steps: append([]TemplateStep{firstOf([]fixtures.Role{fixtures.RoleDimmer}, full())},
			each(whiteMix, fade(255, 0, 400, 0))...),
	},
	{
		ID: "colour-bump", Name: "Colour bump",
		Description: "A short red hit that decays over 300 ms. Edit the values for another colour.",
		Requires:    []fixtures.Role{fixtures.RoleRed},
		Steps: slices.Concat(
			[]TemplateStep{firstOf([]fixtures.Role{fixtures.RoleDimmer}, full())},
			each([]fixtures.Role{fixtures.RoleGreen, fixtures.RoleBlue, fixtures.RoleWhite}, move{}),
			[]TemplateStep{firstOf([]fixtures.Role{fixtures.RoleRed}, fade(255, 0, 300, 0))},
		),
	},
	{
		ID: "fade-up", Name: "Slow fade up",
		Description: "Comes up to full white over two seconds and stays there.",
		Steps: append([]TemplateStep{firstOf([]fixtures.Role{fixtures.RoleDimmer}, fade(0, 255, 2000, 0))},
			each(whiteMix, fade(0, 255, 2000, 0))...),
	},
	{
		ID: "blackout", Name: "Blackout",
		Description: "Kills every light-making channel instantly. Useful on a stop cue.",
		Steps:       each(emitters, move{}),
	},
	{
		ID: "strobe-burst", Name: "Strobe burst",
		Description: "Six hard flashes over half a second, on the dimmer — or the first colour channel when there is no dimmer.",
		Steps: func() []TemplateStep {
			moves := flashes(6, 100, 50)
			out := make([]TemplateStep, 0, len(moves))
			for _, m := range moves {
				out = append(out, firstOf(intensity, m))
			}
			return out
		}(),
	},
	{
		ID: "chase", Name: "Chase",
		Description:   "The same flash walked across the selected fixtures, 120 ms apart.",
		ChaseOffsetMs: 120,
		Steps: append([]TemplateStep{firstOf([]fixtures.Role{fixtures.RoleDimmer}, full())},
			each(whiteMix, fade(255, 0, 250, 0))...),
	},
	{
		ID: "colour-cycle", Name: "Colour cycle",
		Description: "Crossfades red into green into blue and back, over three seconds.",
		Requires:    []fixtures.Role{fixtures.RoleRed, fixtures.RoleGreen, fixtures.RoleBlue},
		Steps: []TemplateStep{
			firstOf([]fixtures.Role{fixtures.RoleDimmer}, full()),
			firstOf([]fixtures.Role{fixtures.RoleWhite}, move{}),
			firstOf([]fixtures.Role{fixtures.RoleRed}, fade(255, 0, 1000, 0)),
			firstOf([]fixtures.Role{fixtures.RoleGreen}, fade(0, 255, 1000, 0)),
			firstOf([]fixtures.Role{fixtures.RoleGreen}, fade(255, 0, 1000, 1000)),
			firstOf([]fixtures.Role{fixtures.RoleBlue}, fade(0, 255, 1000, 1000)),
			firstOf([]fixtures.Role{fixtures.RoleBlue}, fade(255, 0, 1000, 2000)),
			firstOf([]fixtures.Role{fixtures.RoleRed}, fade(0, 255, 1000, 2000)),
		},
	},
	{
		ID: "pan-sweep", Name: "Pan sweep",
		Description: "Swings a mover from one end of its pan to the other over three seconds.",
		Requires:    []fixtures.Role{fixtures.RolePan},
		Steps: []TemplateStep{
			firstOf([]fixtures.Role{fixtures.RoleDimmer}, full()),
			// Position is where the head points, not how hard the pad
			// was hit, so these deliberately ignore velocity.
			firstOf([]fixtures.Role{fixtures.RoleTilt}, move{To: 128}),
			firstOf([]fixtures.Role{fixtures.RolePan}, move{To: 255, DurationMs: 3000}),
		},
	},
}

// FindTemplate looks a template up by id.
func FindTemplate(id string) (Template, bool) {
	i := slices.IndexFunc(Templates, func(t Template) bool { return t.ID == id })
	if i < 0 {
		return Template{}, false
	}
	return Templates[i], true
}

// CanPerform reports whether the fixture has at least one of the roles
// the template needs.
func CanPerform(t Template, p fixtures.Patch, cat fixtures.Catalog) bool {
	required := t.Requires
	if len(required) == 0 {
		for _, s := range t.Steps {
			required = append(required, s.Roles...)
		}
	}
	for _, role := range required {
		if _, ok := cat.ChannelForRole(p, role); ok {
			return true
		}
	}
	return false
}

// Coverage is which of the fixtures a template can really drive.
//
// A fixture missing the roles that define the move contributes nothing,
// and the picker says so rather than quietly producing a shorter
// programme than expected.
func Coverage(t Template, patches []fixtures.Patch, cat fixtures.Catalog) (covered, skipped []fixtures.Patch) {
	for _, p := range patches {
		if CanPerform(t, p, cat) {
			covered = append(covered, p)
		} else {
			skipped = append(skipped, p)
		}
	}
	return covered, skipped
}

// Instantiate is the steps a template produces against the fixtures, in
// fixture order.
//
// Fixtures that cannot perform the move at all are left out; within the
// ones that can, roles they lack are dropped rather than guessed at, so
// a template applied to a mixed selection does as much as each unit can
// manage. The result is an ordinary step list, and later steps still
// win on a shared channel, which is why each fixture's steps stay in
// the order they were written.
//
// A chase counts only the fixtures it actually reaches, so its offsets
// stay even rather than leaving a hole where a skipped fixture sat.
//
// newID names each step; the caller supplies it so the package needs no
// source of randomness of its own.
func Instantiate(t Template, patches []fixtures.Patch, cat fixtures.Catalog, newID func() string) []Step {
	var steps []Step
	covered, _ := Coverage(t, patches, cat)
	for index, patch := range covered {
		shift := float64(index) * t.ChaseOffsetMs
		for _, m := range t.Steps {
			var channel fixtures.Resolved
			found := false
			for _, role := range m.Roles {
				if c, ok := cat.ChannelForRole(patch, role); ok {
					channel, found = c, true
					break
				}
			}
			if !found {
				continue
			}
			steps = append(steps, Step{
				ID:              newID(),
				FixtureID:       patch.ID,
				Target:          channel.Offset,
				DelayMs:         m.DelayMs + shift,
				From:            m.From,
				To:              m.To,
				DurationMs:      m.DurationMs,
				ScaleByVelocity: m.ScaleByVelocity,
			})
		}
	}
	return steps
}
