package pattern

import (
	"slices"
	"testing"

	"github.com/marrasen/gunim-dmx/fixtures"
)

// rig patches two fixtures of a made type: a dimmer, red, green, blue,
// white, and a motor's speed, at 1 and at 11.
func rig(t *testing.T) ([]fixtures.Patch, []fixtures.Group, fixtures.Catalog) {
	t.Helper()
	chans := make([]fixtures.Channel, 0, 7)
	for i, f := range []fixtures.Function{fixtures.Function(fixtures.RoleDimmer),
		fixtures.Function(fixtures.RoleRed), fixtures.Function(fixtures.RoleGreen),
		fixtures.Function(fixtures.RoleBlue), fixtures.Function(fixtures.RoleWhite),
		fixtures.Function(fixtures.RoleSpeed), fixtures.FunctionUnknown} {
		chans = append(chans, fixtures.ChannelFor(i+1, f, "", 0))
	}
	chans[5].Home = 40
	cat := fixtures.Catalog{Own: []fixtures.Definition{{ID: fixtures.CustomPrefix + "par", Model: "Par",
		Modes: []fixtures.Mode{{ID: "default", Channels: chans}}}}}
	patches := []fixtures.Patch{
		{ID: "a", DefinitionID: fixtures.CustomPrefix + "par", ModeID: "default", StartAddress: 1, Gamma: 1, MaxLevel: 255},
		{ID: "b", DefinitionID: fixtures.CustomPrefix + "par", ModeID: "default", StartAddress: 11, Gamma: 1, MaxLevel: 255},
	}
	return patches, []fixtures.Group{{ID: "g", MemberIDs: []string{"a", "b"}}}, cat
}

// at is the value one channel of one fixture is written, and -1 for
// none.
func at(ws []Write, id string, offset int) int {
	if i := slices.IndexFunc(ws, func(w Write) bool { return w.FixtureID == id && w.Offset == offset }); i >= 0 {
		return ws[i].Value
	}
	return -1
}

func TestAColourLightsEveryMemberOfTheGroup(t *testing.T) {
	patches, groups, cat := rig(t)
	p := Pattern{Bars: 1, Lanes: []Lane{{GroupID: "g", Clips: []Clip{
		{Start: 0, Length: 2, Kind: KindColour, Colour: Colour{R: 255, B: 100}},
	}}}}
	ws := ValuesAt(p, 1, patches, groups, cat)
	for _, id := range []string{"a", "b"} {
		if at(ws, id, 1) != 255 || at(ws, id, 2) != 255 || at(ws, id, 3) != 0 || at(ws, id, 4) != 100 || at(ws, id, 5) != 0 {
			t.Errorf("fixture %s is lit %v", id, ws)
		}
		if at(ws, id, 6) != -1 {
			t.Errorf("a colour moved fixture %s's motor", id)
		}
	}
	if ws := ValuesAt(p, 2.5, patches, groups, cat); len(ws) != 0 {
		t.Errorf("past the clip, the pattern still writes %v", ws)
	}
}

// A level clip sets how bright a colour over it is: the colour's own
// call for a full dimmer gives way.
func TestALevelUnderAColourSetsItsBrightness(t *testing.T) {
	patches, groups, cat := rig(t)
	p := Pattern{Bars: 1, Lanes: []Lane{{FixtureID: "a", Clips: []Clip{
		{Start: 0, Length: 4, Kind: KindLevel, From: 0, To: 200},
		{Start: 0, Length: 4, Kind: KindColour, Colour: Colour{G: 255}},
	}}}}
	if d := at(ValuesAt(p, 2, patches, groups, cat), "a", 1); d != 100 {
		t.Errorf("half way up a ramp to 200 the dimmer is %d, want 100", d)
	}
}

func TestAColourFadesInAndOut(t *testing.T) {
	patches, groups, cat := rig(t)
	p := Pattern{Bars: 1, Lanes: []Lane{{FixtureID: "a", Clips: []Clip{
		{Start: 0, Length: 4, Kind: KindColour, Colour: Colour{R: 200}, FadeIn: 1, FadeOut: 2},
	}}}}
	for _, c := range []struct {
		pos  float64
		want int
	}{{0.5, 100}, {1.5, 200}, {3, 100}} {
		if got := at(ValuesAt(p, c.pos, patches, groups, cat), "a", 2); got != c.want {
			t.Errorf("at beat %v red is %d, want %d", c.pos, got, c.want)
		}
	}
}

// A strobe flashes on the beat: lit the first half of each flash.
func TestAStrobeFlashesOnTheBeat(t *testing.T) {
	patches, groups, cat := rig(t)
	p := Pattern{Bars: 1, Lanes: []Lane{{FixtureID: "a", Clips: []Clip{
		{Start: 0, Length: 4, Kind: KindStrobe, Rate: 2, Colour: Colour{W: 255}},
	}}}}
	for _, c := range []struct {
		pos  float64
		want int
	}{{0.1, 255}, {0.3, 0}, {0.6, 255}, {0.8, 0}} {
		if got := at(ValuesAt(p, c.pos, patches, groups, cat), "a", 5); got != c.want {
			t.Errorf("at beat %v white is %d, want %d", c.pos, got, c.want)
		}
	}
}

// A channel clip drives a function by name on a group, and by number on
// a fixture whose channel has no name.
func TestAChannelClipDrivesAFunction(t *testing.T) {
	patches, groups, cat := rig(t)
	p := Pattern{Bars: 1, Lanes: []Lane{
		{GroupID: "g", Clips: []Clip{{Start: 0, Length: 4, Kind: KindChannel, Role: fixtures.RoleSpeed, From: 180, To: 180}}},
		{FixtureID: "b", Clips: []Clip{{Start: 0, Length: 4, Kind: KindChannel, Target: 7, From: 0, To: 100}}},
	}}
	ws := ValuesAt(p, 2, patches, groups, cat)
	if at(ws, "a", 6) != 180 || at(ws, "b", 6) != 180 {
		t.Errorf("the motor speed is %d and %d, want 180", at(ws, "a", 6), at(ws, "b", 6))
	}
	if at(ws, "b", 7) != 50 {
		t.Errorf("the unnamed channel is %d half way to 100", at(ws, "b", 7))
	}
}

// ---- the engine ----
