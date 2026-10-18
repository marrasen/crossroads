package fx

import (
	"slices"
	"strconv"
	"testing"

	"github.com/marrasen/gunim-dmx/fixtures"
)

var (
	// rgbPar is a three-channel colour mixer with no master dimmer.
	rgbPar = fixtures.Patch{ID: "rgb", Name: "Par 1", DefinitionID: "generic-rgb", ModeID: "3ch", StartAddress: 1}
	// rgbwPar is four colours behind a master dimmer, so role lookups
	// have a choice.
	rgbwPar = fixtures.Patch{ID: "rgbw", Name: "Par 2", DefinitionID: "generic-rgbw-dimmer", ModeID: "5ch", StartAddress: 10}
	// house is one level and nothing else, the sparsest thing a
	// template can land on.
	house = fixtures.Patch{ID: "dim", Name: "House", DefinitionID: "generic-dimmer-1ch", ModeID: "1ch", StartAddress: 20}
)

func mustTemplate(t *testing.T, id string) Template {
	t.Helper()
	found, ok := FindTemplate(id)
	if !ok {
		t.Fatalf("no template %q", id)
	}
	return found
}

// counter hands out step ids without a source of randomness.
func counter() func() string {
	n := 0
	return func() string {
		n++
		return "s" + strconv.Itoa(n)
	}
}

func targets(steps []Step) []int {
	out := make([]int, len(steps))
	for i, s := range steps {
		out[i] = s.Target
	}
	return out
}

func forFixture(steps []Step, id string) []Step {
	var out []Step
	for _, s := range steps {
		if s.FixtureID == id {
			out = append(out, s)
		}
	}
	return out
}

func TestInstantiateDrivesEveryColourOfAnRgbPar(t *testing.T) {
	steps := Instantiate(mustTemplate(t, "white-flash"), []fixtures.Patch{rgbPar}, fixtures.Catalog{}, counter())
	// No dimmer to snap, so only the three colours fade.
	if !slices.Equal(targets(steps), []int{1, 2, 3}) {
		t.Errorf("targets = %v", targets(steps))
	}
	for _, s := range steps {
		if s.FixtureID != "rgb" || s.From != 255 || s.To != 0 {
			t.Errorf("step = %+v", s)
		}
	}
	if got := Duration(Program{ID: "p", Steps: steps}); got != 400 {
		t.Errorf("the flash lasts %v, want 400", got)
	}
}

func TestInstantiateAddsTheMasterDimmer(t *testing.T) {
	steps := Instantiate(mustTemplate(t, "white-flash"), []fixtures.Patch{rgbwPar}, fixtures.Catalog{}, counter())
	// The dimmer snaps up first, then white, red, green and blue fade out.
	if steps[0].Target != 1 || steps[0].To != 255 || steps[0].DurationMs != 0 {
		t.Errorf("first step = %+v", steps[0])
	}
	rest := targets(steps[1:])
	slices.Sort(rest)
	if !slices.Equal(rest, []int{2, 3, 4, 5}) {
		t.Errorf("the rest = %v", rest)
	}
}

func TestInstantiateDropsRolesAFixtureLacks(t *testing.T) {
	// A white flash needs no particular channel, so a bare dimmer gets
	// the one step it can make and none of the colour fades.
	steps := Instantiate(mustTemplate(t, "white-flash"), []fixtures.Patch{house}, fixtures.Catalog{}, counter())
	if len(steps) != 1 || steps[0].FixtureID != "dim" || steps[0].Target != 1 || steps[0].To != 255 {
		t.Errorf("steps = %+v", steps)
	}
}

func TestInstantiateLeavesOutAFixtureThatCannotMove(t *testing.T) {
	// The dimmer has no colours to cycle, so it contributes nothing —
	// not even the dimmer snap the template opens with.
	if steps := Instantiate(mustTemplate(t, "colour-cycle"), []fixtures.Patch{house}, fixtures.Catalog{}, counter()); len(steps) != 0 {
		t.Errorf("steps = %+v, want none", steps)
	}
}

func TestInstantiateClosesChaseGaps(t *testing.T) {
	redChase := Template{
		ID: "test-red-chase", Name: "Red chase",
		Requires: []fixtures.Role{fixtures.RoleRed}, ChaseOffsetMs: 100,
		Steps: []TemplateStep{
			firstOf([]fixtures.Role{fixtures.RoleDimmer}, move{To: 255, ScaleByVelocity: true}),
			firstOf([]fixtures.Role{fixtures.RoleRed}, fade(255, 0, 200, 0)),
		},
	}
	// The bare dimmer sits between the two colour fixtures and has no
	// red, so it drops out and the chase stays evenly spaced instead of
	// stuttering.
	steps := Instantiate(redChase, []fixtures.Patch{rgbPar, house, rgbwPar}, fixtures.Catalog{}, counter())
	if len(forFixture(steps, "dim")) != 0 {
		t.Error("the fixture that cannot chase was included")
	}
	for _, s := range forFixture(steps, "rgb") {
		if s.DelayMs != 0 {
			t.Errorf("the first fixture was delayed by %v", s.DelayMs)
		}
	}
	for _, s := range forFixture(steps, "rgbw") {
		if s.DelayMs != 100 {
			t.Errorf("the second fixture was delayed by %v, want 100", s.DelayMs)
		}
	}
}

func TestInstantiateFallsBackThroughTheRoleList(t *testing.T) {
	onDimmer := Instantiate(mustTemplate(t, "strobe-burst"), []fixtures.Patch{rgbwPar}, fixtures.Catalog{}, counter())
	onRgb := Instantiate(mustTemplate(t, "strobe-burst"), []fixtures.Patch{rgbPar}, fixtures.Catalog{}, counter())
	// Six on and off pairs, on the dimmer where there is one and on red
	// where there is not.
	if len(onDimmer) != 12 {
		t.Errorf("got %d steps on the dimmer, want 12", len(onDimmer))
	}
	for _, steps := range [][]Step{onDimmer, onRgb} {
		for _, s := range steps {
			if s.Target != 1 {
				t.Errorf("a burst step landed on channel %d, want 1", s.Target)
			}
		}
	}
	if onRgb[0].FixtureID != "rgb" {
		t.Errorf("the burst landed on %q", onRgb[0].FixtureID)
	}
}

func TestInstantiateStacksChaseOffsets(t *testing.T) {
	chase := mustTemplate(t, "chase")
	steps := Instantiate(chase, []fixtures.Patch{rgbPar, rgbwPar}, fixtures.Catalog{}, counter())
	for _, s := range forFixture(steps, "rgb") {
		if s.DelayMs != 0 {
			t.Errorf("the first fixture was delayed by %v", s.DelayMs)
		}
	}
	for _, s := range forFixture(steps, "rgbw") {
		if s.DelayMs != chase.ChaseOffsetMs {
			t.Errorf("the second fixture was delayed by %v, want %v", s.DelayMs, chase.ChaseOffsetMs)
		}
	}
}

func TestInstantiateProducesNothingForAnImpossibleMove(t *testing.T) {
	if steps := Instantiate(mustTemplate(t, "pan-sweep"), []fixtures.Patch{rgbPar}, fixtures.Catalog{}, counter()); len(steps) != 0 {
		t.Errorf("steps = %+v, want none", steps)
	}
}

func TestInstantiateGivesEveryStepItsOwnID(t *testing.T) {
	steps := Instantiate(mustTemplate(t, "colour-cycle"), []fixtures.Patch{rgbPar, rgbwPar}, fixtures.Catalog{}, counter())
	ids := map[string]bool{}
	for _, s := range steps {
		if ids[s.ID] {
			t.Fatalf("id %q was used twice", s.ID)
		}
		ids[s.ID] = true
	}
}

func TestInstantiateOrdersAChannelsMovesSoTheLatestWins(t *testing.T) {
	// Red is driven twice by the colour cycle: down over the first
	// second, back up over the last. The engine applies steps in list
	// order and a finished step keeps asserting its end value, so the
	// later move has to come second in the list.
	steps := Instantiate(mustTemplate(t, "colour-cycle"), []fixtures.Patch{rgbPar}, fixtures.Catalog{}, counter())
	var red []Step
	for _, s := range steps {
		if s.Target == 1 {
			red = append(red, s)
		}
	}
	if len(red) != 2 {
		t.Fatalf("red is driven %d times, want twice", len(red))
	}
	if red[0].DelayMs >= red[1].DelayMs {
		t.Errorf("the moves are out of order: %v then %v", red[0].DelayMs, red[1].DelayMs)
	}
}

func TestCoverage(t *testing.T) {
	ids := func(ps []fixtures.Patch) []string {
		out := make([]string, 0, len(ps))
		for _, p := range ps {
			out = append(out, p.ID)
		}
		return out
	}
	// Nothing here pans, so the sweep reaches none of them — the house
	// dimmer included, even though the template does touch a dimmer.
	covered, skipped := Coverage(mustTemplate(t, "pan-sweep"), []fixtures.Patch{rgbPar, house}, fixtures.Catalog{})
	if len(covered) != 0 || !slices.Equal(ids(skipped), []string{"rgb", "dim"}) {
		t.Errorf("sweep covered %v, skipped %v", ids(covered), ids(skipped))
	}
	// Without a Requires, any role the template touches is enough.
	covered, skipped = Coverage(mustTemplate(t, "white-flash"), []fixtures.Patch{rgbPar, house}, fixtures.Catalog{})
	if !slices.Equal(ids(covered), []string{"rgb", "dim"}) || len(skipped) != 0 {
		t.Errorf("flash covered %v, skipped %v", ids(covered), ids(skipped))
	}
}

func TestTemplatesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	positional := 0
	for _, tm := range Templates {
		if seen[tm.ID] {
			t.Errorf("template id %q was used twice", tm.ID)
		}
		seen[tm.ID] = true
		for _, s := range tm.Steps {
			if s.From < 0 || s.From > 255 || s.To < 0 || s.To > 255 {
				t.Errorf("%s has a value outside the DMX range: %+v", tm.ID, s)
			}
			if s.DelayMs < 0 || s.DurationMs < 0 {
				t.Errorf("%s has a negative time: %+v", tm.ID, s)
			}
			if len(s.Roles) == 0 {
				t.Errorf("%s has a step targeting no role", tm.ID)
			}
			if slices.Contains(s.Roles, fixtures.RolePan) || slices.Contains(s.Roles, fixtures.RoleTilt) {
				positional++
				if s.ScaleByVelocity {
					// Aiming the head by how hard the pad was hit is
					// never what anyone means.
					t.Errorf("%s velocity-scales a position: %+v", tm.ID, s)
				}
			}
		}
	}
	if positional == 0 {
		t.Error("no template drives a position, so the check above proves nothing")
	}
}
