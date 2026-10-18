package ui

import (
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// monitor is the Monitor tab, which says why a light does what it
// does: on the left each light's channels, each with its value now and
// the module its light comes from; on the right what happened lately,
// MIDI heard, beats, songs and buttons, the latest first.
type monitor struct {
	st     *app.State
	played engine.Live
}

func newMonitor() *monitor { return &monitor{} }

func (m *monitor) show(st *app.State, u *gunim.UI) {
	m.st = st
	u.Invalidate()
}

func (m *monitor) live(l engine.Live, u *gunim.UI) {
	m.played = l
	u.Invalidate()
}

// Layout implements [gunim.Node].
func (m *monitor) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Max
}

// The monitor's measures.
const (
	monRow  = 22
	monHead = 40
	monSide = 16
)

// Paint implements [gunim.Node].
func (m *monitor) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	p.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(th)))
	if m.st == nil {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Clip: true, Radius: 12, Opacity: 1})()
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	split := max(box.W*0.58, 240)
	h := bold.Shape("Channels", 14)
	h.Paint(p, geom.Pt(monSide, 14), ink)
	h = bold.Shape("What happened", 14)
	h.Paint(p, geom.Pt(split+monSide, 14), ink)
	p.RRect(geom.Rc(split, 12, 1, box.H-24), 0, paint.Solid(faded(ink, 0.12)))

	s := &m.st.Show
	cat := s.Catalog()
	y := float32(monHead)
	for _, l := range s.Rig.Lights {
		if y > box.H {
			break
		}
		t := bold.Shape(l.Name, 12)
		t.Paint(p, geom.Pt(monSide, y+3), ink)
		y += monRow
		for _, c := range cat.ResolvedChannels(l) {
			if !c.InUniverse || y > box.H {
				continue
			}
			v := m.played.Stage[c.Slot-1]
			slot := face.Shape(strconv.Itoa(c.Slot), 11)
			slot.Paint(p, geom.Pt(monSide+8, y+4), quiet)
			name := c.Channel.Name
			if c.Channel.Role != "" {
				name = c.Channel.Role.Label()
			}
			n := face.Shape(name, 11)
			n.PaintFaded(p, geom.Pt(monSide+44, y+4), ink, 110, widget.TextFade.Get(th))
			bar := geom.Rc(monSide+160, y+6, 90, 8)
			p.RRect(bar, 4, paint.Solid(faded(ink, 0.08)))
			fill := bar
			fill.Max.X = bar.Min.X + bar.Size().W*float32(v)/255
			owner := m.played.Owners[c.Slot]
			accent := quiet
			if o := s.Module(owner); o != nil {
				accent = kindInk(th, o.Kind)
			}
			p.RRect(fill, 4, paint.Solid(accent))
			val := face.Shape(strconv.Itoa(int(v)), 11)
			val.Paint(p, geom.Pt(monSide+258, y+4), quiet)
			who := "at rest"
			if o := s.Module(owner); o != nil {
				who = o.Name
			}
			w := face.Shape(who, 11)
			w.PaintFaded(p, geom.Pt(monSide+292, y+4), accent, max(split-monSide-300, 30), widget.TextFade.Get(th))
			y += monRow
		}
		y += 6
	}
	if len(s.Rig.Lights) == 0 {
		msg := face.Shape("No lights in the rig.", 12)
		msg.Paint(p, geom.Pt(monSide, monHead), quiet)
	}

	y = monHead
	log := m.played.Log
	if len(log) == 0 {
		msg := face.Shape("Nothing yet: MIDI heard, beats, songs and buttons show here.", 11)
		msg.PaintFaded(p, geom.Pt(split+monSide, y), quiet, max(box.W-split-2*monSide, 30), widget.TextFade.Get(th))
	}
	for i := len(log) - 1; i >= 0 && y < box.H; i-- {
		at := face.Shape(log[i].At.Format("15:04:05.0"), 11)
		at.Paint(p, geom.Pt(split+monSide, y), quiet)
		t := face.Shape(log[i].Text, 11)
		t.PaintFaded(p, geom.Pt(split+monSide+84, y), ink, max(box.W-split-monSide-100, 30), widget.TextFade.Get(th))
		y += monRow - 4
	}
}
