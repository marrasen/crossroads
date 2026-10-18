package ui

import (
	"image/color"
	"slices"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// This file holds the small pieces the pattern editor is built from,
// from midittt's editors: a swap, a picker and a switcher that know what
// each of their items stands for, a button held down, and the rows and
// columns they stand in.

// The kit's text and spacing.
var (
	smallText = theme.Length("crossroads.text.small", 12)
	faintInk  = theme.Color("crossroads.text.faint", color.NRGBA{R: 0x9a, G: 0xa0, B: 0xab, A: 0xff})
	rowGap    = theme.Length("crossroads.rowgap", 8)
	colGap    = theme.Length("crossroads.colgap", 10)
	sideGap   = theme.Length("crossroads.sidegap", 24)
	noGap     = theme.Length("crossroads.nogap", 0)
)

// faint is small, faint writing: a hint, or what a list says when it is
// empty.
func faint(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Size, l.Color = smallText, faintInk
	return l
}

// stack is a column with the kit's spacing between its rows.
func stack(kids ...gunim.Node) *widget.Flex {
	c := widget.Column(kids...)
	c.Gap = colGap
	c.Cross = widget.CrossStretch
	return c
}

// line is a row of controls with the kit's spacing, lined up on their
// middles.
func line(kids ...gunim.Node) *widget.Flex {
	r := widget.Row(kids...)
	r.Gap = rowGap
	r.Cross = widget.CrossCenter
	return r
}

// narrow holds a field to a width that leaves room beside it.
func narrow(n gunim.Node, width float32) gunim.Node { return widget.NewSized(n, width, 0) }

// numberField is a number held between lo and hi.
func numberField(lo, hi, value float64) *widget.NumberField {
	f := widget.NewNumberField(lo, hi)
	f.SetValue(value, nil)
	return f
}

// A swap shows one of several nodes at a time, the others taken out of
// the tree, so a field that does not apply cannot be typed into.
type swap struct {
	shown gunim.Node
}

// show puts n in place of whatever was there.
func (s *swap) show(n gunim.Node, u *gunim.UI) {
	if s.shown == n {
		return
	}
	if u == nil {
		s.shown = n
		return
	}
	if s.shown != nil {
		u.Remove(s.shown)
	}
	s.shown = n
	if n != nil {
		u.Insert(s, n)
	}
	u.Invalidate()
}

// Children implements [gunim.Composite].
func (s *swap) Children() []gunim.Node {
	if s.shown == nil {
		return nil
	}
	return []gunim.Node{s.shown}
}

// Layout implements [gunim.Node]: what shows sets the size; anything on
// its way out has no room.
func (s *swap) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	var size geom.Size
	for kid := range kids.All {
		if kid.Node() == s.shown {
			size = kid.Layout(c)
		} else {
			kid.Layout(gunim.Tight(geom.Size{}))
		}
		kid.Place(geom.Point{})
	}
	return size
}

// Paint implements [gunim.Node].
func (s *swap) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for kid := range kids.All {
		kid.Paint(p)
	}
}

// A picker chooses one of a list of values from a drop-down.
type picker[T comparable] struct {
	*widget.Dropdown
	values []T
	label  func(T) string
}

// newPicker returns a picker over values, labelled by label, calling
// chose each time the choice changes.
func newPicker[T comparable](values []T, label func(T) string, chose func(T, *gunim.UI)) *picker[T] {
	p := &picker[T]{Dropdown: widget.NewDropdown(nil), label: label}
	p.setValues(values)
	p.OnChange = func(i int, u *gunim.UI) gunim.Intent {
		if chose != nil && i >= 0 && i < len(p.values) {
			chose(p.values[i], u)
		}
		return nil
	}
	return p
}

// setValues replaces what the picker offers, keeping the choice where it
// is still on offer.
func (p *picker[T]) setValues(values []T) {
	was, had := p.value()
	p.values = values
	items := make([]widget.MenuItem, len(values))
	for i, v := range values {
		items[i] = widget.MenuItem{Label: p.label(v)}
	}
	p.SetItems(items)
	p.SetSelected(0, nil)
	if had {
		p.pick(was)
	}
}

// pick selects a value without telling anyone.
func (p *picker[T]) pick(v T) {
	if i := slices.Index(p.values, v); i >= 0 {
		p.SetSelected(i, nil)
	}
}

// value is what is chosen now, and false when nothing is.
func (p *picker[T]) value() (T, bool) {
	var zero T
	i := p.Selected()
	if i < 0 || i >= len(p.values) {
		return zero, false
	}
	return p.values[i], true
}

// A switcher chooses one of a few values, all shown at once.
type switcher[T comparable] struct {
	*widget.Segmented
	values []T
}

// newSwitcher returns a switcher over values, labelled by label,
// calling chose each time the choice changes.
func newSwitcher[T comparable](values []T, label func(T) string, chose func(T, *gunim.UI)) *switcher[T] {
	labels := make([]string, len(values))
	for i, v := range values {
		labels[i] = label(v)
	}
	s := &switcher[T]{Segmented: widget.NewSegmented(labels...), values: values}
	s.OnChange = func(i int, u *gunim.UI) gunim.Intent {
		if chose != nil && i >= 0 && i < len(s.values) {
			chose(s.values[i], u)
		}
		return nil
	}
	return s
}

// pick selects a value without telling anyone.
func (s *switcher[T]) pick(v T) {
	if i := slices.Index(s.values, v); i >= 0 {
		s.SetSelected(i, nil)
	}
}

// A holdButton is a button that says when it goes down and when it comes
// up, for something that plays while held, as a pad does.
type holdButton struct {
	*widget.Button
	held bool
	// hold makes what is sent as the button goes down, true, and comes
	// up, false.
	hold func(down bool, u *gunim.UI) gunim.Intent
}

func newHoldButton(label string, hold func(down bool, u *gunim.UI) gunim.Intent) *holdButton {
	return &holdButton{Button: widget.NewButton(label), hold: hold}
}

// Handle implements [gunim.Handler]: the press and the release each send
// what hold makes, and the button draws itself pressed as ever.
func (b *holdButton) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerDown:
		if e.Button == input.ButtonPrimary && !b.Disabled && !b.held {
			b.held = true
			b.send(true, u)
		}
	case input.PointerUp:
		if b.held {
			b.held = false
			b.send(false, u)
		}
	}
	return b.Button.Handle(e, u)
}

func (b *holdButton) send(down bool, u *gunim.UI) {
	if in := b.hold(down, u); in != nil {
		u.Send(b, in)
	}
}
