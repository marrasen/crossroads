package ui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image/color"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// patternsTab is the Patterns tab, midittt's pattern editor: the show's
// patterns down the left, and the one picked on the right, its lanes a
// row each with clips drawn along the beats. Each change goes into the
// show as it is made.
type patternsTab struct {
	st     *app.State
	ids    []string
	sel    string
	add    *widget.Button
	remove *widget.IconButton
	editor *swap
	scroll *widget.Scroll
	body   *patternBody
	hover  int
}

// The tab's measures: the list's width, a row of it, and its head.
const (
	listW    = 220
	listRowH = 36
	listHead = 52
)

func newPatternsTab() *patternsTab {
	t := &patternsTab{editor: &swap{}, hover: -1}
	t.scroll = widget.NewScroll(t.editor)
	t.add = widget.NewButton("New pattern")
	t.add.Icon = icon.Plus
	t.add.OnClick = func(*gunim.UI) gunim.Intent {
		id := "pattern-" + newID()
		t.sel = id
		return app.SavePattern{Pattern: pattern.Pattern{ID: id, Name: "New pattern", Bars: 1, Release: pattern.Release{Rest: true}}}
	}
	t.remove = widget.NewIconButton(icon.Trash2, "Take the pattern out of the show")
	t.remove.OnClick = func(*gunim.UI) gunim.Intent {
		if t.sel == "" {
			return nil
		}
		return app.RemovePattern{ID: t.sel}
	}
	return t
}

func (t *patternsTab) show(st *app.State, u *gunim.UI) {
	t.st = st
	t.ids = t.ids[:0]
	for _, p := range st.Show.Patterns {
		t.ids = append(t.ids, p.ID)
	}
	if !slices.Contains(t.ids, t.sel) {
		t.sel = ""
		if len(t.ids) > 0 {
			t.sel = t.ids[0]
		}
	}
	t.remove.Disabled = t.sel == ""
	t.follow(u)
	u.Invalidate()
}

// follow shows the pattern picked in the editor: a new editor for
// another pattern, or for this one changed elsewhere; the editor as it
// is where the show holds what it holds.
func (t *patternsTab) follow(u *gunim.UI) {
	i := slices.IndexFunc(t.st.Show.Patterns, func(p pattern.Pattern) bool { return p.ID == t.sel })
	if i < 0 {
		t.body = nil
		t.editor.show(faint("No patterns yet. Make one with New pattern, or import a midittt rig."), u)
		return
	}
	p := t.st.Show.Patterns[i]
	if t.body != nil && t.body.p.ID == p.ID && (t.body.drawing() || reflect.DeepEqual(t.body.result(), p)) {
		t.body.lights, t.body.groups, t.body.cat = t.st.Show.Rig.Lights, t.st.Show.Rig.Groups, t.st.Show.Catalog()
		return
	}
	t.body = newPatternBody(p, t.st.Show.Rig.Lights, t.st.Show.Rig.Groups, t.st.Show.Catalog())
	t.editor.show(t.body.root, u)
}

func (t *patternsTab) live(engine.Live, *gunim.UI) {}

// Children implements [gunim.Composite].
func (t *patternsTab) Children() []gunim.Node {
	return []gunim.Node{t.add, t.remove, t.scroll}
}

// Layout implements [gunim.Node]: the list's buttons over it at the
// left, the editor, scrolled, at the right.
func (t *patternsTab) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	add, remove, sc := kids.At(0), kids.At(1), kids.At(2)
	rs := remove.Layout(gunim.Constraints{Max: geom.Sz(36, 36)})
	remove.Place(geom.Pt(listW-12-rs.W, (listHead-rs.H)/2))
	as := add.Layout(gunim.Constraints{Max: geom.Sz(listW-rs.W-36, 36)})
	add.Place(geom.Pt(12, (listHead-as.H)/2))
	sc.Layout(gunim.Tight(geom.Sz(max(size.W-listW-16, 0), max(size.H-24, 0))))
	sc.Place(geom.Pt(listW+8, 12))
	return size
}

// Paint implements [gunim.Node]: the list, the pattern picked lit.
func (t *patternsTab) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	p.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(th)))
	ink := widget.Ink.Get(th)
	face := widget.Font.Get(th)
	p.RRect(geom.Rc(listW, 12, 1, max(box.H-24, 0)), 0, paint.Solid(faded(ink, 0.1)))
	if t.st != nil {
		for i, p2 := range t.st.Show.Patterns {
			r := geom.Rc(8, listHead+float32(i)*listRowH, listW-16, listRowH-4)
			switch {
			case p2.ID == t.sel:
				p.RRect(r, 8, paint.Solid(faded(lookInk.Get(th), 0.3)))
			case i == t.hover:
				p.RRect(r, 8, paint.Solid(faded(ink, 0.06)))
			}
			s := face.Shape(p2.DisplayName(), 13)
			s.PaintFaded(p, geom.Pt(r.Min.X+10, r.Min.Y+9), ink, r.Size().W-20, widget.TextFade.Get(th))
		}
	}
	for k := range kids.All {
		k.Paint(p)
	}
}

// Handle implements [gunim.Handler]: a pattern of the list clicked is
// picked.
func (t *patternsTab) Handle(e input.Event, u *gunim.UI) bool {
	rowAt := func(pos geom.Point) int {
		if pos.X > listW || pos.Y < listHead {
			return -1
		}
		i := int((pos.Y - listHead) / listRowH)
		if i >= len(t.ids) {
			return -1
		}
		return i
	}
	switch e := e.(type) {
	case input.PointerMove:
		if i := rowAt(e.Pos); i != t.hover {
			t.hover = i
			u.Invalidate()
		}
	case input.PointerLeave:
		t.hover = -1
		u.Invalidate()
	case input.PointerDown:
		if i := rowAt(e.Pos); i >= 0 && e.Button == input.ButtonPrimary {
			t.sel = t.ids[i]
			t.remove.Disabled = false
			t.follow(u)
			u.Invalidate()
			return true
		}
	}
	return false
}

// newID is an ID no clip, lane or pattern has.
func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// A patternBody edits one pattern: its name, length and what letting go
// does; the grid of its lanes; and what the clip picked does. Each
// change is sent to the show.
type patternBody struct {
	p      pattern.Pattern
	lights []fixtures.Patch
	groups []fixtures.Group
	cat    fixtures.Catalog

	snap    float64
	brush   pattern.Kind
	selLane int
	selClip string
	// lastColour is what a new colour or strobe clip starts in: the
	// colour last set, so a run of clips in one colour is quick.
	lastColour pattern.Colour

	root     gunim.Node
	name     *widget.TextField
	lanes    *swap
	ruler    *swap
	timeline *timeline
	target   *picker[string]
	detail   *swap
}

func kindLabel(k pattern.Kind) string {
	return map[pattern.Kind]string{pattern.KindColour: "Colour", pattern.KindLevel: "Level",
		pattern.KindStrobe: "Strobe", pattern.KindChannel: "Channel"}[k]
}

var (
	barChoices  = []int{1, 2, 4, 8, 16}
	snapChoices = []float64{1, 0.5, 0.25}
	rateChoices = []float64{1, 2, 4, 8}
)

func snapLabel(s float64) string {
	return map[float64]string{1: "Beats", 0.5: "8ths", 0.25: "16ths"}[s]
}

func rateLabel(r float64) string {
	return map[float64]string{1: "Every beat", 2: "Eighths", 4: "Sixteenths", 8: "Thirty-seconds"}[r]
}

// The pattern editor's measures: the column of lane names, the row of
// bar numbers, a lane, and how near a clip's end a press takes the end.
const (
	laneNameW = 150
	rulerH    = 22
	laneH     = 40
	edgeGrab  = 7
)

func newPatternBody(p pattern.Pattern, lights []fixtures.Patch, groups []fixtures.Group, cat fixtures.Catalog) *patternBody {
	b := &patternBody{p: p, lights: lights, groups: groups, cat: cat, snap: 0.5, brush: pattern.KindColour, selLane: -1,
		lastColour: pattern.Colour{R: 255}, lanes: &swap{}, ruler: &swap{}, detail: &swap{}}
	b.p.Bars = min(max(b.p.Bars, 1), pattern.MaxBars)
	b.timeline = &timeline{body: b}

	b.name = widget.NewTextField()
	b.name.Placeholder = "e.g. Red and blue chase"
	b.name.SetText(b.p.Name, nil)
	b.name.OnCommit = func(string, *gunim.UI) gunim.Intent { return b.saved() }
	bars := newPicker(barChoices, func(n int) string {
		if n == 1 {
			return "1 bar"
		}
		return strconv.Itoa(n) + " bars"
	}, func(n int, u *gunim.UI) {
		b.setBars(n)
		b.ruler.show(b.rulerRow(), u)
		b.picked(u)
		b.save(u)
	})
	bars.pick(b.p.Bars)
	snap := newSwitcher(snapChoices, snapLabel, func(v float64, u *gunim.UI) {
		b.snap = v
		u.Invalidate()
	})
	snap.pick(b.snap)
	brush := newSwitcher(pattern.Kinds, kindLabel, func(k pattern.Kind, _ *gunim.UI) { b.brush = k })
	brush.pick(b.brush)
	release := newPicker([]bool{true, false}, func(rest bool) string {
		if rest {
			return "Give its lights back"
		}
		return "Leave the lights as they are"
	}, func(rest bool, u *gunim.UI) {
		b.p.Release.Rest = rest
		b.save(u)
	})
	release.pick(b.p.Release.Rest)
	test := newHoldButton("Try", func(down bool, _ *gunim.UI) gunim.Intent {
		return app.TestPattern{Pattern: b.result(), On: down}
	})
	test.Tooltip = "Plays the pattern on the stage while held, as a pad would"

	// Lanes are added by what they drive: a group, or one light.
	targets := make([]string, 0, len(groups)+len(lights))
	for _, g := range groups {
		targets = append(targets, "g:"+g.ID)
	}
	for _, l := range lights {
		targets = append(targets, "f:"+l.ID)
	}
	b.target = newPicker(targets, b.targetName, nil)
	add := widget.NewButton("+ Add lane")
	add.Disabled = len(targets) == 0
	add.OnClick = func(u *gunim.UI) gunim.Intent {
		t, ok := b.target.value()
		if !ok {
			return nil
		}
		l := pattern.Lane{ID: newID()}
		if id, isGroup := strings.CutPrefix(t, "g:"); isGroup {
			l.GroupID = id
		} else {
			l.FixtureID = strings.TrimPrefix(t, "f:")
		}
		b.p.Lanes = append(slices.Clone(b.p.Lanes), l)
		b.lanesChanged(u)
		return b.saved()
	}

	form := widget.NewForm().
		Add("Name", b.name).
		Add("Length", line(narrow(bars, 160))).
		Add("Let go", line(narrow(release, 260)))
	gap := widget.NewSpacer()
	draw := line(faint("Draw"), brush)
	tools := line(faint("Snap to"), snap, gap, test)
	tools.Grow(gap, 1)

	left := widget.Column(widget.NewSized(faint(""), laneNameW, rulerH), b.lanes)
	left.Gap = noGap
	right := widget.Column(widget.NewSized(b.ruler, 0, rulerH), b.timeline)
	right.Gap, right.Cross = noGap, widget.CrossStretch
	grid := line(left, right)
	grid.Cross = widget.CrossStart
	grid.Grow(right, 1)

	b.lanes.shown = b.laneColumn()
	b.ruler.shown = b.rulerRow()
	b.detail.shown = b.hint()
	b.root = widget.NewPad(stack(form, draw, tools, grid, line(narrow(b.target, 220), add), b.detail))
	return b
}

// result is the pattern as the editor holds it.
func (b *patternBody) result() pattern.Pattern {
	out := b.p
	out.Name = strings.TrimSpace(b.name.Text())
	out.Lanes = slices.Clone(out.Lanes)
	return out
}

// saved is the intent that puts the pattern in the show.
func (b *patternBody) saved() gunim.Intent { return app.SavePattern{Pattern: b.result()} }

// save puts the pattern in the show.
func (b *patternBody) save(u *gunim.UI) {
	if u != nil {
		u.Send(b.timeline, b.saved())
	}
}

// drawing reports whether a drag on the grid holds a clip, which the
// show is not told of until it lets go.
func (b *patternBody) drawing() bool { return b.timeline.drag.on }

// targetName names what a lane drives.
func (b *patternBody) targetName(t string) string {
	if id, ok := strings.CutPrefix(t, "g:"); ok {
		if g, ok := fixtures.FindGroup(b.groups, id); ok {
			name := g.Name
			if name == "" {
				name = "Untitled group"
			}
			return name + " (group)"
		}
		return "(a group not there)"
	}
	if p, ok := fixtures.FindPatch(b.lights, strings.TrimPrefix(t, "f:")); ok {
		if p.Name == "" {
			return "Untitled light"
		}
		return p.Name
	}
	return "(a light not there)"
}

func laneTarget(l pattern.Lane) string {
	if l.GroupID != "" {
		return "g:" + l.GroupID
	}
	return "f:" + l.FixtureID
}

// setBars changes the length, cutting short a clip that would run past
// the new end and dropping one that would start after it.
func (b *patternBody) setBars(n int) {
	b.p.Bars = n
	end := b.p.Length()
	lanes := slices.Clone(b.p.Lanes)
	for i := range lanes {
		clips := lanes[i].Clips[:0:0]
		for _, c := range lanes[i].Clips {
			if c.Start >= end {
				continue
			}
			c.Length = min(c.Length, end-c.Start)
			clips = append(clips, c)
		}
		lanes[i].Clips = clips
	}
	b.p.Lanes = lanes
}

// laneColumn is the lane names, a row a lane, each with a way to take
// the lane out.
func (b *patternBody) laneColumn() gunim.Node {
	if len(b.p.Lanes) == 0 {
		return widget.NewSized(faint("Add a lane below."), laneNameW, laneH)
	}
	rows := make([]gunim.Node, 0, len(b.p.Lanes))
	for i, l := range b.p.Lanes {
		name := widget.NewLabel(b.targetName(laneTarget(l)))
		name.MaxLines = 1
		remove := widget.NewIconButton(icon.X, "Take this lane out")
		remove.OnClick = func(u *gunim.UI) gunim.Intent {
			b.p.Lanes = slices.Delete(slices.Clone(b.p.Lanes), i, i+1)
			b.selLane, b.selClip = -1, ""
			b.lanesChanged(u)
			return b.saved()
		}
		row := line(name, remove)
		row.Grow(name, 1)
		rows = append(rows, widget.NewSized(row, laneNameW, laneH))
	}
	col := widget.Column(rows...)
	col.Gap = noGap
	return col
}

// rulerRow numbers the bars over the grid, each number at its bar.
func (b *patternBody) rulerRow() gunim.Node {
	nums := make([]gunim.Node, b.p.Bars)
	for i := range nums {
		nums[i] = faint(strconv.Itoa(i + 1))
	}
	row := widget.Row(nums...)
	row.Gap = noGap
	for _, n := range nums {
		row.Grow(n, 1)
	}
	return row
}

func (b *patternBody) lanesChanged(u *gunim.UI) {
	b.lanes.show(b.laneColumn(), u)
	b.picked(u)
}

// newClip is a clip of what the brush is set to, from start for length.
func (b *patternBody) newClip(start, length float64) pattern.Clip {
	c := pattern.Clip{ID: newID(), Start: start, Length: length, Kind: b.brush}
	b.fillKind(&c)
	return c
}

// fillKind gives a clip what its kind needs to start with.
func (b *patternBody) fillKind(c *pattern.Clip) {
	switch c.Kind {
	case pattern.KindColour:
		c.Colour = b.lastColour
	case pattern.KindStrobe:
		c.Colour = b.lastColour
		if c.Rate <= 0 {
			c.Rate = 2
		}
	case pattern.KindLevel:
		if c.From == 0 && c.To == 0 {
			c.From, c.To = 255, 255
		}
	case pattern.KindChannel:
		if c.Role == "" && c.Target == 0 {
			if fs := b.functionsOf(b.selLane); len(fs) > 0 {
				c.Role, c.Target = splitFunction(fs[0])
			}
			c.From, c.To = 128, 128
		}
	}
}

// editClip changes one clip.
func (b *patternBody) editClip(lane int, id string, apply func(*pattern.Clip)) {
	if lane < 0 || lane >= len(b.p.Lanes) {
		return
	}
	lanes := slices.Clone(b.p.Lanes)
	l := &lanes[lane]
	if i := clipIndex(*l, id); i >= 0 {
		clips := slices.Clone(l.Clips)
		apply(&clips[i])
		l.Clips = clips
		b.p.Lanes = lanes
	}
}

// selected is the clip picked, and false with none.
func (b *patternBody) selected() (pattern.Clip, bool) {
	if b.selLane < 0 || b.selLane >= len(b.p.Lanes) {
		return pattern.Clip{}, false
	}
	l := b.p.Lanes[b.selLane]
	if i := clipIndex(l, b.selClip); i >= 0 {
		return l.Clips[i], true
	}
	return pattern.Clip{}, false
}

// picked shows what the clip picked does, for editing, or how to draw
// one where none is.
func (b *patternBody) picked(u *gunim.UI) {
	if _, ok := b.selected(); ok {
		b.detail.show(b.clipEditor(), u)
	} else {
		b.detail.show(b.hint(), u)
	}
	u.Invalidate()
}

func (b *patternBody) hint() gunim.Node {
	h := faint("Pick what to draw, then press on a lane and drag out how long it lasts. " +
		"Press a clip to change it; drag it to move it, or by an end to resize it. " +
		"The pattern loops while it plays, in step with the beat.")
	h.MaxLines = 4
	return h
}

// set changes the clip picked, and puts the pattern in the show.
func (b *patternBody) set(apply func(*pattern.Clip), u *gunim.UI) {
	b.editClip(b.selLane, b.selClip, apply)
	if c, ok := b.selected(); ok && (c.Kind == pattern.KindColour || c.Kind == pattern.KindStrobe) {
		b.lastColour = c.Colour
	}
	b.save(u)
	u.Invalidate()
}

// clipEditor is the panel under the grid for the clip picked.
func (b *patternBody) clipEditor() gunim.Node {
	c, _ := b.selected()
	kind := newSwitcher(pattern.Kinds, kindLabel, func(k pattern.Kind, u *gunim.UI) {
		b.set(func(c *pattern.Clip) { c.Kind = k; b.fillKind(c) }, u)
		b.picked(u)
	})
	kind.pick(c.Kind)

	beats := func(v, lo, hi float64, apply func(*pattern.Clip, float64)) *widget.NumberField {
		f := numberField(lo, hi, v)
		f.Decimals, f.Suffix = 2, " beats"
		f.SetValue(v, nil)
		f.OnCommit = func(v float64, u *gunim.UI) gunim.Intent {
			b.set(func(c *pattern.Clip) { apply(c, v) }, u)
			return nil
		}
		return f
	}
	length := b.p.Length()
	when := widget.NewForm().
		Add("Starts", narrow(beats(c.Start, 0, length, func(c *pattern.Clip, v float64) {
			c.Start = min(v, length-c.Length)
		}), 150)).
		Add("Lasts", narrow(beats(c.Length, b.snap, length, func(c *pattern.Clip, v float64) {
			c.Length = min(v, length-c.Start)
		}), 150))

	what := widget.NewForm()
	switch c.Kind {
	case pattern.KindColour, pattern.KindStrobe:
		what.Add("Colour", b.colourRow(c))
		if c.Kind == pattern.KindStrobe {
			rate := newPicker(rateChoices, rateLabel, func(r float64, u *gunim.UI) {
				b.set(func(c *pattern.Clip) { c.Rate = r }, u)
			})
			rate.pick(c.Rate)
			what.Add("Flashes", narrow(rate, 200))
		} else {
			what.Add("Fade in", narrow(beats(c.FadeIn, 0, c.Length, func(c *pattern.Clip, v float64) { c.FadeIn = v }), 150))
			what.Add("Fade out", narrow(beats(c.FadeOut, 0, c.Length, func(c *pattern.Clip, v float64) { c.FadeOut = v }), 150))
		}
	case pattern.KindLevel, pattern.KindChannel:
		if c.Kind == pattern.KindChannel {
			fn := newPicker(b.functionsOf(b.selLane), b.functionName, func(f string, u *gunim.UI) {
				b.set(func(c *pattern.Clip) { c.Role, c.Target = splitFunction(f) }, u)
			})
			fn.pick(joinFunction(c.Role, c.Target))
			what.Add("Drives", fn)
		}
		value := func(v int, apply func(*pattern.Clip, int)) *widget.NumberField {
			f := numberField(0, 255, float64(v))
			f.OnCommit = func(v float64, u *gunim.UI) gunim.Intent {
				b.set(func(c *pattern.Clip) { apply(c, int(v)) }, u)
				return nil
			}
			return f
		}
		what.Add("From", narrow(value(c.From, func(c *pattern.Clip, v int) { c.From = v }), 110))
		what.Add("To", narrow(value(c.To, func(c *pattern.Clip, v int) { c.To = v }), 110))
	}

	remove := widget.NewButton("Delete clip")
	remove.Kind = widget.ButtonDanger
	remove.OnClick = func(u *gunim.UI) gunim.Intent {
		if b.selLane >= 0 && b.selLane < len(b.p.Lanes) {
			lanes := slices.Clone(b.p.Lanes)
			lanes[b.selLane].Clips = slices.DeleteFunc(slices.Clone(lanes[b.selLane].Clips), func(c pattern.Clip) bool { return c.ID == b.selClip })
			b.p.Lanes = lanes
		}
		b.selClip = ""
		b.picked(u)
		return b.saved()
	}
	cols := line(when, what)
	cols.Cross, cols.Gap = widget.CrossStart, sideGap
	cols.Grow(what, 1)
	note := faint(kindHint(c.Kind))
	note.MaxLines = 3
	return stack(line(faint("This clip is"), kind, widget.NewSpacer(), remove), cols, note)
}

// colourRow picks a colour: red, green and blue by swatch, and white,
// amber and UV by number, for the lights that have them.
func (b *patternBody) colourRow(c pattern.Clip) gunim.Node {
	swatch := widget.NewColorButton(color.NRGBA{R: c.Colour.R, G: c.Colour.G, B: c.Colour.B, A: 0xff})
	swatch.Opaque = true
	swatch.OnChange = func(v color.NRGBA, u *gunim.UI) gunim.Intent {
		b.set(func(c *pattern.Clip) { c.Colour.R, c.Colour.G, c.Colour.B = v.R, v.G, v.B }, u)
		return nil
	}
	part := func(label string, v uint8, apply func(*pattern.Colour, uint8)) gunim.Node {
		f := numberField(0, 255, float64(v))
		f.OnCommit = func(v float64, u *gunim.UI) gunim.Intent {
			b.set(func(c *pattern.Clip) { apply(&c.Colour, uint8(v)) }, u)
			return nil
		}
		return line(faint(label), narrow(f, 80))
	}
	return line(swatch,
		part("White", c.Colour.W, func(c *pattern.Colour, v uint8) { c.W = v }),
		part("Amber", c.Colour.A, func(c *pattern.Colour, v uint8) { c.A = v }),
		part("UV", c.Colour.UV, func(c *pattern.Colour, v uint8) { c.UV = v }))
}

func kindHint(k pattern.Kind) string {
	switch k {
	case pattern.KindColour:
		return "Lights every light in the lane in this colour. Channels a light lacks are " +
			"left out; one with only a dimmer shows how bright the colour is."
	case pattern.KindStrobe:
		return "Flashes the colour in step with the beat, lit for the first half of each flash."
	case pattern.KindLevel:
		return "Sets the dimmer, flat or ramping. Under a colour it sets how bright the colour " +
			"is. A light with no dimmer shows it as white light."
	case pattern.KindChannel:
	}
	return "Holds one function at a value, or ramps it: a motor's speed, a gobo, a " +
		"built-in programme. Lights without that function are left alone."
}

// functionsOf are what a channel clip on a lane can drive: every
// function its lights have, and on a one-light lane its unnamed channels
// by number.
func (b *patternBody) functionsOf(lane int) []string {
	if lane < 0 || lane >= len(b.p.Lanes) {
		return nil
	}
	l := b.p.Lanes[lane]
	members := pattern.Members(l, b.lights, b.groups)
	var out []string
	for _, r := range b.cat.RolesInGroup(members) {
		out = append(out, joinFunction(r, 0))
	}
	if l.FixtureID != "" && len(members) == 1 {
		for _, c := range b.cat.ResolvedChannels(members[0]) {
			if c.Channel.Role == "" {
				out = append(out, joinFunction("", c.Offset))
			}
		}
	}
	return out
}

func (b *patternBody) functionName(f string) string {
	role, target := splitFunction(f)
	if role != "" {
		return role.Label()
	}
	if b.selLane >= 0 && b.selLane < len(b.p.Lanes) {
		if p, ok := fixtures.FindPatch(b.lights, b.p.Lanes[b.selLane].FixtureID); ok {
			return b.cat.ChannelLabel(p, target)
		}
	}
	return fmt.Sprintf("Channel %d", target)
}

// joinFunction and splitFunction carry a channel clip's target as one
// picker value: a role, or a channel number.
func joinFunction(r fixtures.Role, target int) string {
	if r != "" {
		return "role:" + string(r)
	}
	return "ch:" + strconv.Itoa(target)
}

func splitFunction(f string) (role fixtures.Role, target int) {
	if r, ok := strings.CutPrefix(f, "role:"); ok {
		return fixtures.Role(r), 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(f, "ch:"))
	return "", n
}

// A timeline is the pattern editor's grid: a lane a row, the bars and
// beats across, and the clips drawn on them. A press on empty grid draws
// a clip, dragging out its length; a press on a clip picks it, and a
// drag moves it, or by an end, resizes it. Everything snaps to the grid.
type timeline struct {
	body *patternBody
	size geom.Size

	// drag is what a press has hold of until it lets go.
	drag struct {
		on       bool
		lane     int
		clipID   string
		mode     dragMode
		grab     float64 // beats from the clip's start to the press
		anchor   float64 // where a new clip was started
		original pattern.Clip
	}
}

type dragMode int

const (
	dragMove dragMode = iota
	dragStart
	dragEnd
	dragDraw
)

// beatWidth is how wide a beat is drawn.
func (t *timeline) beatWidth() float32 {
	return t.size.W / float32(max(t.body.p.Length(), 1))
}

// Layout implements [gunim.Node]: as wide as given, a lane a row.
func (t *timeline) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	t.size = c.Constrain(geom.Sz(c.Max.W, float32(max(len(t.body.p.Lanes), 1))*laneH))
	return t.size
}

// Paint implements [gunim.Node].
func (t *timeline) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	p.RRect(geom.Rect{Max: box.Point()}, 6, paint.Solid(faded(ink, 0.05)))
	bw := t.beatWidth()
	length := t.body.p.Length()
	// Alternate lanes are shaded, so a clip reads against its own row.
	for i := range t.body.p.Lanes {
		if i%2 == 1 {
			p.RRect(geom.Rc(0, float32(i)*laneH, box.W, laneH), 0, paint.Solid(faded(ink, 0.04)))
		}
	}
	// The grid: the snap faint, the beats plainer, the bars plain.
	bar, beat, snapLine := faded(ink, 0.3), faded(ink, 0.15), faded(ink, 0.06)
	snap := t.body.snap
	for s := snap; s < length; s += snap {
		x := float32(s) * bw
		switch {
		case math.Mod(s, pattern.BeatsPerBar) == 0:
			p.RRect(geom.Rc(x-1, 0, 2, box.H), 0, paint.Solid(bar))
		case math.Mod(s, 1) == 0:
			p.RRect(geom.Rc(x, 0, 1, box.H), 0, paint.Solid(beat))
		default:
			p.RRect(geom.Rc(x, 0, 1, box.H), 0, paint.Solid(snapLine))
		}
	}
	for li, l := range t.body.p.Lanes {
		y := float32(li)*laneH + 4
		for _, c := range l.Clips {
			r := geom.Rc(float32(c.Start)*bw+1, y, max(float32(c.Length)*bw-2, 3), laneH-8)
			paintClip(p, c, r)
			if c.ID == t.body.selClip {
				p.RRectStroke(r, 4, paint.Solid(color.NRGBA{}), paint.Stroke{Width: 2, Color: ink})
			}
		}
	}
}

// paintClip draws one clip in what it does: a colour in its colour, a
// strobe striped by its flashes, a level as grey as it ends, a channel
// in teal.
func paintClip(p *paint.Painter, c pattern.Clip, r geom.Rect) {
	switch c.Kind {
	case pattern.KindColour:
		p.RRect(r, 4, paint.Solid(clipColour(c.Colour)))
	case pattern.KindStrobe:
		p.RRect(r, 4, paint.Solid(dim(clipColour(c.Colour))))
		rate := c.Rate
		if rate <= 0 {
			rate = 1
		}
		flash := r.Size().W / float32(math.Max(c.Length*rate, 1))
		for x := r.Min.X; x < r.Max.X; x += flash {
			p.RRect(geom.Rc(x, r.Min.Y, min(flash/2, r.Max.X-x), r.Size().H), 2, paint.Solid(clipColour(c.Colour)))
		}
	case pattern.KindLevel:
		v := uint8(max(c.From, c.To)/2 + 40)
		p.RRect(r, 4, paint.Solid(color.NRGBA{R: v, G: v, B: v, A: 0xff}))
		// The ramp, as a line from where it starts to where it ends.
		y0 := r.Max.Y - float32(c.From)/255*r.Size().H
		y1 := r.Max.Y - float32(c.To)/255*r.Size().H
		const steps = 12
		for i := range steps {
			x := r.Min.X + r.Size().W*float32(i)/steps
			y := y0 + (y1-y0)*float32(i)/steps
			p.RRect(geom.Rc(x, y-1, r.Size().W/steps+1, 2), 0, paint.Solid(color.NRGBA{A: 0xc0}))
		}
	case pattern.KindChannel:
		p.RRect(r, 4, paint.Solid(color.NRGBA{R: 0x3c, G: 0xb4, B: 0xa8, A: 0xff}))
	}
}

// clipColour is roughly what a colour clip looks like: its red, green
// and blue, with white, amber and UV mixed in as they would look.
func clipColour(c pattern.Colour) color.NRGBA {
	r := float64(c.R) + float64(c.W) + float64(c.A) + 0.55*float64(c.UV)
	g := float64(c.G) + float64(c.W) + 0.75*float64(c.A)
	b := float64(c.B) + float64(c.W) + float64(c.UV)
	clamp := func(v float64) uint8 { return uint8(math.Min(v, 255)) }
	out := color.NRGBA{R: clamp(r), G: clamp(g), B: clamp(b), A: 0xff}
	if out.R < 30 && out.G < 30 && out.B < 30 {
		out = color.NRGBA{R: 0x30, G: 0x30, B: 0x38, A: 0xff} // black, but seen
	}
	return out
}

func dim(c color.NRGBA) color.NRGBA {
	c.R, c.G, c.B = c.R/3, c.G/3, c.B/3
	return c
}

// at is the beat and lane under a point.
func (t *timeline) at(pt geom.Point) (beat float64, lane int) {
	bw := t.beatWidth()
	if bw <= 0 {
		return 0, -1
	}
	return float64(pt.X / bw), int(pt.Y / laneH)
}

func (t *timeline) snapTo(beat float64) float64 {
	s := t.body.snap
	return math.Round(beat/s) * s
}

// Handle implements [gunim.Handler]: a press takes hold, a move carries
// what it holds, and letting go puts the pattern in the show.
func (t *timeline) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		beat, lane := t.at(e.Pos)
		if lane < 0 || lane >= len(t.body.p.Lanes) {
			return false
		}
		t.press(beat, lane, e.Pos.X)
		u.Invalidate()
		return true
	case input.PointerMove:
		if !t.drag.on {
			return false
		}
		beat, _ := t.at(e.Pos)
		t.move(beat)
		u.Invalidate()
		return true
	case input.PointerUp:
		if !t.drag.on {
			return false
		}
		t.drag.on = false
		t.body.picked(u)
		t.body.save(u)
		return true
	}
	return false
}

// press takes hold of the clip under the pointer, by an end or as a
// whole, or starts drawing a new one.
func (t *timeline) press(beat float64, lane int, x float32) {
	d := &t.drag
	d.on, d.lane = true, lane
	t.body.selLane = lane
	l := t.body.p.Lanes[lane]
	bw := t.beatWidth()
	// The topmost clip, which is the one drawn last.
	for i := len(l.Clips) - 1; i >= 0; i-- {
		c := l.Clips[i]
		if beat < c.Start || beat >= c.End() {
			continue
		}
		d.clipID, d.original, d.grab = c.ID, c, beat-c.Start
		left, right := float32(c.Start)*bw, float32(c.End())*bw
		switch {
		case x-left < edgeGrab && c.Length > t.body.snap:
			d.mode = dragStart
		case right-x < edgeGrab:
			d.mode = dragEnd
		default:
			d.mode = dragMove
		}
		t.body.selClip = c.ID
		return
	}
	// Empty grid: draw a clip of what the brush is set to, a snap long,
	// which the drag stretches.
	start := math.Floor(beat/t.body.snap) * t.body.snap
	c := t.body.newClip(start, t.body.snap)
	lanes := slices.Clone(t.body.p.Lanes)
	lanes[lane].Clips = append(slices.Clone(lanes[lane].Clips), c)
	t.body.p.Lanes = lanes
	d.clipID, d.original, d.anchor, d.mode = c.ID, c, start, dragDraw
	t.body.selClip = c.ID
}

// move carries the clip held to beat.
func (t *timeline) move(beat float64) {
	d := &t.drag
	length := t.body.p.Length()
	snap := t.body.snap
	t.body.editClip(d.lane, d.clipID, func(c *pattern.Clip) {
		o := d.original
		switch d.mode {
		case dragMove:
			c.Start = min(max(t.snapTo(beat-d.grab), 0), length-o.Length)
		case dragStart:
			s := min(max(t.snapTo(beat), 0), o.End()-snap)
			c.Start, c.Length = s, o.End()-s
		case dragEnd:
			e := min(max(t.snapTo(beat), o.Start+snap), length)
			c.Length = e - o.Start
		case dragDraw:
			// Drawn either way from where the press began.
			b := min(max(t.snapTo(beat), 0), length)
			lo, hi := math.Min(d.anchor, b), math.Max(d.anchor, b)
			if hi-lo < snap {
				hi = math.Min(lo+snap, length)
				lo = hi - snap
			}
			c.Start, c.Length = lo, hi-lo
		}
	})
}

// clipIndex finds a clip in a lane by its ID.
func clipIndex(l pattern.Lane, id string) int {
	return slices.IndexFunc(l.Clips, func(c pattern.Clip) bool { return c.ID == id })
}
