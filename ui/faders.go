package ui

import (
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// faders is the Faders tab: a fader for each Fader module of the show,
// and each mixer's grand master, nothing else. It is the simple view,
// to play a show by hand.
type faders struct {
	st     *app.State
	played engine.Live
	ids    []string
	strips map[string]*strip
}

// strip is one fader: its slider, its name and its value.
type strip struct {
	s      *widget.Slider
	name   string
	master bool
}

func newFaders() *faders { return &faders{strips: map[string]*strip{}} }

// show follows the show: a strip for each fader and master, in the
// show's order.
func (f *faders) show(st *app.State, u *gunim.UI) {
	f.st = st
	var ids []string
	for i := range st.Show.Modules {
		m := &st.Show.Modules[i]
		if m.Kind != show.Fader && m.Kind != show.Mixer {
			continue
		}
		ids = append(ids, m.ID)
		sp := f.strips[m.ID]
		if sp == nil {
			sp = &strip{s: widget.NewVerticalSlider(0, 1), master: m.Kind == show.Mixer}
			id := m.ID
			sp.s.OnChange = func(v float32, _ *gunim.UI) gunim.Intent { return app.SetValue{Module: id, Value: float64(v)} }
			sp.s.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
				return app.SetValue{Module: id, Value: float64(v), Done: true}
			}
			sp.s.SetValue(float32(m.Value), nil)
			f.strips[m.ID] = sp
		}
		sp.name = m.Name
	}
	if !slices.Equal(ids, f.ids) {
		for _, id := range f.ids {
			if !slices.Contains(ids, id) && u != nil {
				u.Remove(f.strips[id].s)
				delete(f.strips, id)
			}
		}
		for _, id := range ids {
			if !slices.Contains(f.ids, id) && u != nil {
				u.Insert(f, f.strips[id].s)
			}
		}
		f.ids = ids
	}
	u.Invalidate()
}

// live moves the faders as the engine plays their values.
func (f *faders) live(l engine.Live, u *gunim.UI) {
	f.played = l
	for id, sp := range f.strips {
		if v, ok := l.Values[id]; ok && !sp.s.Held() && float64(sp.s.Value()) != v {
			sp.s.SetValue(float32(v), u)
		}
	}
	u.Invalidate()
}

// Children implements [gunim.Composite].
func (f *faders) Children() []gunim.Node {
	out := make([]gunim.Node, 0, len(f.ids))
	for _, id := range f.ids {
		out = append(out, f.strips[id].s)
	}
	return out
}

// The strips' measures.
const (
	stripW    = 72
	stripTop  = 40
	stripFoot = 44
)

// Layout implements [gunim.Node]: the strips side by side, as tall as
// the tab.
func (f *faders) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	h := max(size.H-stripTop-stripFoot, 40)
	i := 0
	for k := range kids.All {
		if k.Presence() == gunim.Exiting {
			k.Layout(gunim.Tight(k.Size()))
			continue
		}
		s := k.Layout(gunim.Constraints{Max: geom.Sz(stripW, h), Min: geom.Sz(0, h)})
		k.Place(geom.Pt(16+float32(i)*stripW+(stripW-s.W)/2, stripTop))
		i++
	}
	return size
}

// Paint implements [gunim.Node]: each strip's name over it and its value
// under it.
func (f *faders) Paint(p *paint.Painter, fr gunim.Frame, box geom.Size, kids gunim.Children) {
	th := fr.Theme
	whole := geom.Rect{Max: box.Point()}
	p.RRect(whole, 12, paint.Solid(widget.CardFill.Get(th)))
	face := widget.Font.Get(th)
	ink := widget.Ink.Get(th)
	for i, id := range f.ids {
		sp := f.strips[id]
		x := 16 + float32(i)*stripW
		name := face.Shape(sp.name, 12)
		name.PaintFaded(p, geom.Pt(x+max((stripW-name.Advance)/2, 4), 14), ink, stripW-8, widget.TextFade.Get(th))
		v := face.Shape(strconv.Itoa(int(sp.s.Value()*100+0.5))+"%", 11)
		c := faded(ink, 0.55)
		if sp.master {
			c = mixerInk.Get(th)
		}
		v.Paint(p, geom.Pt(x+(stripW-v.Advance)/2, box.H-stripFoot+12), c)
	}
	for k := range kids.All {
		k.Paint(p)
	}
	if len(f.ids) == 0 {
		msg := face.Shape("No faders yet. Add one on the Routes tab.", 12)
		msg.Paint(p, geom.Pt((box.W-msg.Advance)/2, box.H/2), faded(ink, 0.5))
	}
}
