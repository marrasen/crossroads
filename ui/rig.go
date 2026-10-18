package ui

import (
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// rig is the Rig tab: the show's lights, each with its type, the
// addresses it takes and a field for where it starts, and a button that
// adds a light of the library's types.
type rig struct {
	st   *app.State
	add  *widget.MenuButton
	defs []fixtures.Definition
	ids  []string
	rows map[string]*rigRow
}

// rigRow is one light's row: its address field and its remove button.
type rigRow struct {
	addr   *widget.NumberField
	remove *widget.IconButton
}

func newRig() *rig {
	r := &rig{rows: map[string]*rigRow{}}
	var cat fixtures.Catalog
	r.defs = cat.All()
	items := make([]widget.MenuItem, len(r.defs))
	for i, d := range r.defs {
		items[i] = widget.MenuItem{Label: d.Label(), Icon: icon.Lightbulb}
	}
	r.add = widget.NewMenuButton("Add light", items)
	r.add.OnPick = func(i int, _ *gunim.UI) gunim.Intent { return app.AddLight{Def: r.defs[i].ID} }
	return r
}

func (r *rig) show(st *app.State, u *gunim.UI) {
	r.st = st
	ids := make([]string, 0, len(st.Show.Rig.Lights))
	for _, p := range st.Show.Rig.Lights {
		ids = append(ids, p.ID)
		row := r.rows[p.ID]
		if row == nil {
			id := p.ID
			row = &rigRow{addr: widget.NewNumberField(1, 512), remove: widget.NewIconButton(icon.Trash2, "Remove the light")}
			row.addr.OnCommit = func(v float64, _ *gunim.UI) gunim.Intent { return app.SetAddress{Light: id, Address: int(v)} }
			row.remove.OnClick = widget.Sends(app.RemoveLight{Light: id})
			r.rows[p.ID] = row
		}
		if u == nil || !u.HasFocus(row.addr) {
			// Not over an address being typed.
			row.addr.SetValue(float64(p.StartAddress), u)
		}
	}
	if !slices.Equal(ids, r.ids) {
		for _, id := range r.ids {
			if !slices.Contains(ids, id) && u != nil {
				u.Remove(r.rows[id].addr)
				u.Remove(r.rows[id].remove)
				delete(r.rows, id)
			}
		}
		for _, id := range ids {
			if !slices.Contains(r.ids, id) && u != nil {
				u.Insert(r, r.rows[id].addr)
				u.Insert(r, r.rows[id].remove)
			}
		}
		r.ids = ids
	}
	u.Invalidate()
}

func (r *rig) live(engine.Live, *gunim.UI) {}

// Children implements [gunim.Composite].
func (r *rig) Children() []gunim.Node {
	out := make([]gunim.Node, 0, 1+2*len(r.ids))
	out = append(out, r.add)
	for _, id := range r.ids {
		out = append(out, r.rows[id].addr, r.rows[id].remove)
	}
	return out
}

// The rig's measures.
const (
	rigHead = 52
	rigRowH = 44
	rigAddr = 96
)

// rowOf is the row of the node n holds, or -1, and whether it is the
// address field.
func (r *rig) rowOf(n gunim.Node) (int, bool) {
	for i, id := range r.ids {
		row := r.rows[id]
		if row.addr == n {
			return i, true
		}
		if row.remove == n {
			return i, false
		}
	}
	return -1, false
}

// Layout implements [gunim.Node]: the add button at the top right, and
// a row for each light under it.
func (r *rig) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	for k := range kids.All {
		if k.Node() == r.add {
			s := k.Layout(gunim.Constraints{Max: geom.Sz(220, 36)})
			k.Place(geom.Pt(size.W-s.W-16, (rigHead-s.H)/2))
			continue
		}
		i, addr := r.rowOf(k.Node())
		if i < 0 {
			k.Layout(gunim.Tight(k.Size()))
			continue
		}
		y := rigHead + float32(i)*rigRowH
		if addr {
			s := k.Layout(gunim.Tight(geom.Sz(rigAddr, 32)))
			k.Place(geom.Pt(size.W-16-36-12-rigAddr, y+(rigRowH-s.H)/2))
		} else {
			s := k.Layout(gunim.Constraints{Max: geom.Sz(36, 36)})
			k.Place(geom.Pt(size.W-16-s.W, y+(rigRowH-s.H)/2))
		}
	}
	return size
}

// Paint implements [gunim.Node]: a row for each light, its name, its
// type and the addresses it takes; lights whose addresses overlap in
// the theme's danger colour.
func (r *rig) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	p.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(th)))
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	head := bold.Shape("Lights", 15)
	head.Paint(p, geom.Pt(16, (rigHead-head.Height())/2), ink)
	if r.st != nil {
		cat := r.st.Show.Catalog()
		over := cat.Overlapping(r.st.Show.Rig.Lights)
		for i, l := range r.st.Show.Rig.Lights {
			y := rigHead + float32(i)*rigRowH
			if i%2 == 0 {
				p.RRect(geom.Rc(8, y, box.W-16, rigRowH), 8, paint.Solid(faded(ink, 0.03)))
			}
			name := bold.Shape(l.Name, 13)
			name.Paint(p, geom.Pt(20, y+8), ink)
			kind := l.DefinitionID
			if d, ok := cat.Find(l.DefinitionID); ok {
				kind = d.Label()
			}
			span := ""
			if fp, ok := cat.FootprintOf(l); ok {
				span = " · channels " + strconv.Itoa(fp.Start) + "–" + strconv.Itoa(fp.End)
			}
			c := quiet
			if over[l.ID] {
				c = widget.DialogDangerInk.Get(th)
				span += " · overlaps another light"
			}
			sub := face.Shape(kind+span, 11)
			sub.PaintFaded(p, geom.Pt(20, y+26), c, max(box.W-20-rigAddr-80, 40), widget.TextFade.Get(th))
		}
		if len(r.st.Show.Rig.Lights) == 0 {
			msg := face.Shape("No lights yet. Add one with Add light.", 12)
			msg.Paint(p, geom.Pt(20, rigHead+12), quiet)
		}
	}
	for k := range kids.All {
		k.Paint(p)
	}
}
