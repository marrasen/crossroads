package ui

import (
	"image/color"
	"math"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// The stage's looks: the floor, its metre grid, and the lights' bodies.
var (
	floorFill = theme.Color("crossroads.stage.floor", color.NRGBA{R: 0x14, G: 0x16, B: 0x1c, A: 0xff})
	floorGrid = theme.Color("crossroads.stage.grid", color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x0c})
	lampBody  = theme.Color("crossroads.stage.lamp", color.NRGBA{R: 0x3a, G: 0x40, B: 0x50, A: 0xff})
)

// stage is the Stage tab: the room seen from above, each light where it
// hangs, glowing in the colour it plays now. A light dragged hangs where
// it is let go. With no DMX box at hand, a show can be worked out here.
type stage struct {
	st    *app.State
	u     dmx.Universe
	size  geom.Size
	hover string
	// held is the light the pointer drags, at where it is held now on
	// the stage, in metres.
	held   string
	heldAt show.Spot
	grab   geom.Point
	moved  bool
}

func newStage() *stage { return &stage{} }

func (s *stage) show(st *app.State, u *gunim.UI) {
	s.st = st
	u.Invalidate()
}

func (s *stage) live(l engine.Live, u *gunim.UI) {
	if l.Stage != s.u {
		s.u = l.Stage
		u.Invalidate()
	}
}

// room is where the stage stands in the tab, and how many pixels a
// metre is.
func (s *stage) room() (r geom.Rect, perMetre float32) {
	if s.st == nil {
		return geom.Rect{}, 1
	}
	st := s.st.Show.Stage
	const margin = 40
	k := min((s.size.W-2*margin)/max(st.Width, 0.1), (s.size.H-2*margin-24)/max(st.Depth, 0.1))
	k = max(k, 1)
	w, h := st.Width*k, st.Depth*k
	return geom.Rc((s.size.W-w)/2, (s.size.H-24-h)/2, w, h), k
}

// at is where spot sp stands in the tab.
func (s *stage) at(sp show.Spot) geom.Point {
	r, k := s.room()
	return geom.Pt(r.Min.X+sp.X*k, r.Max.Y-sp.Y*k)
}

// spotOf is where tab point p stands on the stage.
func (s *stage) spotOf(p geom.Point) show.Spot {
	r, k := s.room()
	return show.Spot{X: (p.X - r.Min.X) / k, Y: (r.Max.Y - p.Y) / k}
}

// spot is where light id hangs now, as it is dragged too.
func (s *stage) spot(id string) (show.Spot, bool) {
	if id == s.held && s.moved {
		return s.heldAt, true
	}
	sp, ok := s.st.Show.Stage.Spots[id]
	return sp, ok
}

// lampR is a light's radius, in pixels.
const lampR = 14

// lightAt is the light at tab point p, or "".
func (s *stage) lightAt(p geom.Point) string {
	if s.st == nil {
		return ""
	}
	for i := len(s.st.Show.Rig.Lights) - 1; i >= 0; i-- {
		id := s.st.Show.Rig.Lights[i].ID
		if sp, ok := s.spot(id); ok {
			d := s.at(sp).Sub(p)
			if math.Hypot(float64(d.X), float64(d.Y)) <= lampR+4 {
				return id
			}
		}
	}
	return ""
}

// Layout implements [gunim.Node].
func (s *stage) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	s.size = c.Max
	return c.Max
}

// Cursor implements [gunim.CursorShaper].
func (s *stage) Cursor(p geom.Point) input.Cursor {
	if s.held != "" || s.lightAt(p) != "" {
		return input.CursorMove
	}
	return input.CursorArrow
}

// Handle implements [gunim.Handler]: a light dragged moves, and hangs
// where it is let go.
func (s *stage) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerMove:
		if s.held != "" {
			if !s.moved && dist(e.Pos, s.grab) < 4 {
				return true
			}
			s.moved = true
			s.heldAt = s.spotOf(e.Pos)
			st := s.st.Show.Stage
			s.heldAt.X = min(max(s.heldAt.X, 0), st.Width)
			s.heldAt.Y = min(max(s.heldAt.Y, 0), st.Depth)
			u.Invalidate()
			return true
		}
		if h := s.lightAt(e.Pos); h != s.hover {
			s.hover = h
			u.Invalidate()
		}
		return s.hover != ""
	case input.PointerLeave:
		s.hover = ""
		u.Invalidate()
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		id := s.lightAt(e.Pos)
		if id == "" {
			return false
		}
		s.held, s.grab, s.moved = id, e.Pos, false
		return true
	case input.PointerUp:
		if s.held == "" {
			return false
		}
		if s.moved {
			u.Send(s, app.PlaceLight{Light: s.held, Spot: s.heldAt})
		}
		s.held, s.moved = "", false
		u.Invalidate()
		return true
	}
	return false
}

// DragsTouch implements [gunim.TouchDragger]: a finger on a light
// drags it.
func (s *stage) DragsTouch() bool { return s.held != "" }

func dist(a, b geom.Point) float32 {
	d := a.Sub(b)
	return float32(math.Hypot(float64(d.X), float64(d.Y)))
}

// Paint implements [gunim.Node]: the room and its metre grid, the front
// marked, and each light where it hangs: its glow on the floor, its
// body, its lens lit in the colour it plays, and its name.
func (s *stage) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	whole := geom.Rect{Max: box.Point()}
	p.RRect(whole, 12, paint.Solid(widget.CardFill.Get(th)))
	if s.st == nil {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: whole, Clip: true, Radius: 12, Opacity: 1})()
	r, k := s.room()
	p.RRect(r, 6, paint.Solid(floorFill.Get(th)))
	grid := floorGrid.Get(th)
	st := s.st.Show.Stage
	for x := float32(1); x < st.Width; x++ {
		p.RRect(geom.Rc(r.Min.X+x*k, r.Min.Y, 1, r.Size().H), 0, paint.Solid(grid))
	}
	for y := float32(1); y < st.Depth; y++ {
		p.RRect(geom.Rc(r.Min.X, r.Max.Y-y*k, r.Size().W, 1), 0, paint.Solid(grid))
	}
	face := widget.Font.Get(th)
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.5)
	front := face.Shape("Front · "+strconv.FormatFloat(float64(st.Width), 'f', -1, 32)+" × "+
		strconv.FormatFloat(float64(st.Depth), 'f', -1, 32)+" m", 11)
	front.Paint(p, geom.Pt(r.Center().X-front.Advance/2, r.Max.Y+8), quiet)
	cat := s.st.Show.Catalog()
	// The glows first, adding light to the floor, then the bodies over
	// them.
	func() {
		defer p.Blend(paint.BlendAdd)()
		for _, l := range s.st.Show.Rig.Lights {
			sp, ok := s.spot(l.ID)
			if !ok {
				continue
			}
			c, lit := lampLook(cat, &s.u, l)
			if lit < 0.01 {
				continue
			}
			at := s.at(sp)
			reach := 1.6 * k
			p.RRect(geom.Rc(at.X-reach, at.Y-reach, 2*reach, 2*reach), reach, paint.Fill{Gradient: &paint.Gradient{
				From: at, To: at.Add(geom.Pt(reach, 0)), Start: faded(c, 0.5*lit), End: faded(c, 0), Radial: true}})
		}
	}()
	body := lampBody.Get(th)
	for _, l := range s.st.Show.Rig.Lights {
		sp, ok := s.spot(l.ID)
		if !ok {
			continue
		}
		at := s.at(sp)
		c, lit := lampLook(cat, &s.u, l)
		disc := geom.Rc(at.X-lampR, at.Y-lampR, 2*lampR, 2*lampR)
		edge := faded(ink, 0.15)
		if l.ID == s.hover || l.ID == s.held {
			edge = widget.Accent.Get(th)
		}
		p.ShadowRRect(disc, lampR, paint.Solid(body), paint.Shadow{Blur: 8, Offset: geom.Pt(0, 3), Color: color.NRGBA{A: 0x80}})
		lens := disc.Inset(geom.Uniform(5))
		if lit > 0.01 {
			p.ShadowRRect(lens, lampR-5, paint.Solid(c), paint.Shadow{Blur: 6 + 14*lit, Color: faded(c, 0.9*lit)})
		} else {
			p.RRect(lens, lampR-5, paint.Solid(floorFill.Get(th)))
		}
		p.RRectStroke(disc, lampR, paint.Fill{}, paint.Stroke{Width: 1.5, Color: edge})
		name := face.Shape(l.Name, 11)
		name.Paint(p, geom.Pt(at.X-name.Advance/2, at.Y+lampR+4), ink)
	}
	if len(s.st.Show.Rig.Lights) == 0 {
		msg := face.Shape("No lights yet. Add them on the Rig tab.", 12)
		msg.Paint(p, geom.Pt((box.W-msg.Advance)/2, box.H/2), quiet)
	}
}
