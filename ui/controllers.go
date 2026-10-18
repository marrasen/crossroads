package ui

import (
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio/midi"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// controllers is the Controllers tab: a card for each Controller, with
// the MIDI port it listens to, its layout, a button that learns a
// control, and each control's value as it moves; a card for each MIDI
// out, with the port it sends to and what it sends; and a card for
// each MIDI clock in and out, with its port.
type controllers struct {
	st     *app.State
	played engine.Live
	list   *cardList
	scroll *widget.Scroll
}

// cardList is the cards, one under another.
type cardList struct {
	c     *controllers
	ids   []string
	cards map[string]*card
	// ins and outs are the MIDI ports, as listed when the tab last
	// followed the show.
	ins, outs []string
	// cols is how many controls a card's row holds, as wide as the
	// cards were laid out.
	cols int
}

// card is one module's card: its widgets, which are those its kind
// has, and its learned controls' forget buttons.
type card struct {
	kind show.Kind
	// ons are a MIDI routes card's routes' on boxes, and routes its
	// routes as the show has them.
	ons               map[string]*widget.Checkbox
	routes            []routing.Route
	device, layout    *widget.Dropdown
	learn             *widget.Button
	channel, note, cc *widget.NumberField
	forget            map[string]*widget.IconButton
	devices           []string
	controls          []show.Control
	height            float32
	learning          bool
}

// listMIDI lists the MIDI ports, inputs or outputs. Tests put lists of
// their own in.
var listMIDI = func(outputs bool) []string {
	var names []string
	if outputs {
		names, _ = midi.Outputs()
	} else {
		names, _ = midi.Inputs()
	}
	return names
}

func newControllers() *controllers {
	c := &controllers{}
	c.list = &cardList{c: c, cards: map[string]*card{}}
	c.scroll = widget.NewScroll(c.list)
	return c
}

func (c *controllers) show(st *app.State, u *gunim.UI) {
	c.st = st
	c.list.show(st, u)
	u.Invalidate()
}

func (c *controllers) live(l engine.Live, u *gunim.UI) {
	c.played = l
	for id, cd := range c.list.cards {
		if cd.learn == nil {
			continue
		}
		on := l.Desks[id].Learn
		if on != cd.learning {
			cd.learning = on
			cd.learn.Label = "Learn a control"
			if on {
				cd.learn.Label = "Move a control…"
			}
		}
	}
	u.Invalidate()
}

// Children implements [gunim.Composite].
func (c *controllers) Children() []gunim.Node { return []gunim.Node{c.scroll} }

// Layout implements [gunim.Node].
func (c *controllers) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(cs.Max))
	k.Place(geom.Point{})
	return cs.Max
}

// Paint implements [gunim.Node].
func (c *controllers) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}

// paintRoutes paints a MIDI routes card's routes: each one's name, and
// what fires it and what it does.
func (l *cardList) paintRoutes(p *paint.Painter, f gunim.Frame, cd *card, m *show.Module, y, w float32) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	n := face.Shape(strconv.Itoa(len(m.Routes))+" routes from midittt", 11)
	n.Paint(p, geom.Pt(cardInset*2, y+29), quiet)
	if len(m.Routes) == 0 {
		msg := face.Shape("No routes. Import a midittt rig to bring its routes over.", 11)
		msg.Paint(p, geom.Pt(cardInset*2, y+cardHead+8), quiet)
	}
	room := max(w-4*cardInset-40, 40)
	for i, r := range cd.routes {
		ry := y + cardHead + float32(i)*routeRowH
		c := ink
		if !r.Enabled {
			c = quiet
		}
		t := bold.Shape(r.DisplayName(), 12)
		t.PaintFaded(p, geom.Pt(cardInset*2+34, ry+4), c, room, widget.TextFade.Get(th))
		s := face.Shape(r.Says(), 11)
		s.PaintFaded(p, geom.Pt(cardInset*2+34, ry+21), quiet, room, widget.TextFade.Get(th))
	}
}

// routeRowH is how tall a route's row is.
const routeRowH = 40

// The cards' measures.
const (
	cardHead  = 48
	cardRow   = 44
	cellW     = 180
	cellH     = 30
	cardGap   = 12
	cardInset = 16
)

// show follows the show: a card for each Controller and MIDI out, in
// the show's order.
func (l *cardList) show(st *app.State, u *gunim.UI) {
	l.ins, l.outs = listMIDI(false), listMIDI(true)
	var ids []string
	for i := range st.Show.Modules {
		m := &st.Show.Modules[i]
		if m.Kind != show.Controller && m.Kind != show.MIDIOut && m.Kind != show.ClockIn && m.Kind != show.ClockOut && m.Kind != show.Routes {
			continue
		}
		ids = append(ids, m.ID)
		cd := l.cards[m.ID]
		if cd == nil {
			cd = l.newCard(m)
			l.cards[m.ID] = cd
			if u != nil {
				for _, n := range cd.nodes() {
					u.Insert(l, n)
				}
			}
		}
		l.follow(cd, m, u)
	}
	for _, id := range l.ids {
		if !slices.Contains(ids, id) {
			if cd := l.cards[id]; cd != nil && u != nil {
				for _, n := range cd.nodes() {
					u.Remove(n)
				}
			}
			delete(l.cards, id)
		}
	}
	l.ids = ids
	u.Invalidate()
}

// newCard makes module m's card.
func (l *cardList) newCard(m *show.Module) *card {
	id := m.ID
	cd := &card{kind: m.Kind, forget: map[string]*widget.IconButton{}, ons: map[string]*widget.Checkbox{}}
	if m.Kind == show.Routes {
		return cd
	}
	cd.device = widget.NewDropdown(nil)
	cd.device.OnChange = func(i int, _ *gunim.UI) gunim.Intent {
		if i < 0 || i >= len(cd.devices) {
			return nil
		}
		return app.SetDevice{Module: id, Device: cd.devices[i]}
	}
	if m.Kind == show.ClockIn || m.Kind == show.ClockOut {
		return cd
	}
	if m.Kind == show.MIDIOut {
		field := func(lo, hi float64) *widget.NumberField {
			f := widget.NewNumberField(lo, hi)
			f.NoDrag = true
			return f
		}
		cd.channel, cd.note, cd.cc = field(1, 16), field(0, 127), field(0, 127)
		cd.channel.Suffix, cd.note.Suffix, cd.cc.Suffix = " ch", " note", " cc"
		send := func(float64, *gunim.UI) gunim.Intent {
			return app.SetMIDI{Module: id, Channel: int(cd.channel.Value()) - 1, Note: int(cd.note.Value()), CC: int(cd.cc.Value())}
		}
		cd.channel.OnCommit, cd.note.OnCommit, cd.cc.OnCommit = send, send, send
		return cd
	}
	names := []string{"Learned controls"}
	layouts := []string{""}
	for _, lo := range controller.Layouts {
		names = append(names, lo.Name)
		layouts = append(layouts, lo.ID)
	}
	cd.layout = widget.NewDropdown(widget.Labels(names...))
	cd.layout.OnChange = func(i int, _ *gunim.UI) gunim.Intent { return app.SetLayout{Module: id, Layout: layouts[i]} }
	if i := slices.Index(layouts, m.Layout); i >= 0 {
		cd.layout.SetSelected(i, nil)
	}
	cd.learn = widget.NewButton("Learn a control")
	cd.learn.OnClick = func(*gunim.UI) gunim.Intent { return app.Learn{Module: id, On: !cd.learning} }
	return cd
}

// follow brings card cd up to date with module m: the ports offered,
// the settings, and a forget button for each control it learned.
func (l *cardList) follow(cd *card, m *show.Module, u *gunim.UI) {
	if m.Kind == show.Routes {
		l.followRoutes(cd, m, u)
		return
	}
	ports := l.ins
	if m.Kind == show.MIDIOut || m.Kind == show.ClockOut {
		ports = l.outs
	}
	devices := slices.Clone(ports)
	if m.Device != "" && !slices.Contains(devices, m.Device) {
		// Kept, though it is not plugged in.
		devices = append(devices, m.Device)
	}
	if !slices.Equal(devices, cd.devices) {
		cd.devices = devices
		names := make([]string, len(devices))
		for i, d := range devices {
			names[i] = d
			if !slices.Contains(ports, d) {
				names[i] = d + " (not plugged in)"
			}
		}
		cd.device.SetItems(widget.Labels(names...))
	}
	cd.device.SetSelected(slices.Index(devices, m.Device), u)
	if m.Kind == show.ClockIn || m.Kind == show.ClockOut {
		return
	}
	if m.Kind == show.MIDIOut {
		for _, f := range []struct {
			f *widget.NumberField
			v int
		}{{cd.channel, m.Channel + 1}, {cd.note, m.Note}, {cd.cc, m.CC}} {
			if u == nil || !u.HasFocus(f.f) {
				f.f.SetValue(float64(f.v), u)
			}
		}
		return
	}
	_, built := controller.Find(m.Layout)
	cd.controls = controller.ControlsOf(m)
	learned := map[string]bool{}
	if !built {
		for _, c := range m.Controls {
			learned[c.ID] = true
			if cd.forget[c.ID] == nil {
				b := widget.NewIconButton(icon.X, "Forget the control")
				b.OnClick = widget.Sends(app.RemoveControl{Module: m.ID, Control: c.ID})
				cd.forget[c.ID] = b
				if u != nil {
					u.Insert(l, b)
				}
			}
		}
	}
	for id, b := range cd.forget {
		if !learned[id] {
			if u != nil {
				u.Remove(b)
			}
			delete(cd.forget, id)
		}
	}
	// A built-in layout knows its controls: there are none to learn.
	cd.learn.Disabled = built
}

// followRoutes brings a MIDI routes card up to date: an on box for each
// route.
func (l *cardList) followRoutes(cd *card, m *show.Module, u *gunim.UI) {
	cd.routes = m.Routes
	have := map[string]bool{}
	for _, r := range m.Routes {
		have[r.ID] = true
		box := cd.ons[r.ID]
		if box == nil {
			box = widget.NewCheckbox("")
			id, route := m.ID, r.ID
			box.OnChange = func(on bool, _ *gunim.UI) gunim.Intent { return app.SetRouteOn{Module: id, Route: route, On: on} }
			cd.ons[r.ID] = box
			if u != nil {
				u.Insert(l, box)
			}
		}
		box.SetChecked(r.Enabled, u)
	}
	for id, box := range cd.ons {
		if !have[id] {
			if u != nil {
				u.Remove(box)
			}
			delete(cd.ons, id)
		}
	}
}

// nodes are the card's widgets, in the order its card lays them out.
func (cd *card) nodes() []gunim.Node {
	if cd.kind == show.Routes {
		out := make([]gunim.Node, 0, len(cd.routes))
		for _, r := range cd.routes {
			if b := cd.ons[r.ID]; b != nil {
				out = append(out, b)
			}
		}
		return out
	}
	out := []gunim.Node{cd.device}
	if cd.layout != nil {
		out = append(out, cd.layout)
	}
	if cd.learn != nil {
		out = append(out, cd.learn)
	}
	if cd.channel != nil {
		out = append(out, cd.channel, cd.note, cd.cc)
	}
	for _, c := range cd.controls {
		if b := cd.forget[c.ID]; b != nil {
			out = append(out, b)
		}
	}
	return out
}

// Children implements [gunim.Composite].
func (l *cardList) Children() []gunim.Node {
	var out []gunim.Node
	for _, id := range l.ids {
		out = append(out, l.cards[id].nodes()...)
	}
	return out
}

// top is where card id starts.
func (l *cardList) top(id string) float32 {
	y := float32(cardGap)
	for _, o := range l.ids {
		if o == id {
			break
		}
		y += l.cards[o].height + cardGap
	}
	return y
}

// Layout implements [gunim.Node]: the cards one under another, each
// its port and settings in a row under its head, its controls in a
// grid under them, as many to a row as fit.
func (l *cardList) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	w := c.Max.W
	l.cols = max(int((w-4*cardInset+8)/(cellW+8)), 1)
	byNode := map[gunim.Node]gunim.Child{}
	for k := range kids.All {
		byNode[k.Node()] = k
	}
	done := map[gunim.Node]bool{}
	y := float32(cardGap)
	for _, id := range l.ids {
		cd := l.cards[id]
		row := y + cardHead
		if cd.kind == show.Routes {
			for i, r := range cd.routes {
				if k, ok := byNode[cd.ons[r.ID]]; ok {
					k.Layout(gunim.Tight(geom.Sz(24, 24)))
					k.Place(geom.Pt(cardInset*2, y+cardHead+float32(i)*routeRowH+(routeRowH-24)/2))
					done[k.Node()] = true
				}
			}
			cd.height = cardHead + float32(max(len(cd.routes), 1))*routeRowH + 8
			y += cd.height + cardGap
			continue
		}
		x := float32(cardInset * 2)
		put := func(n gunim.Node, most float32) {
			k, ok := byNode[n]
			if !ok {
				return
			}
			s := k.Layout(gunim.Constraints{Max: geom.Sz(most, 34)})
			k.Place(geom.Pt(x, row+4+(34-s.H)/2))
			x += s.W + 8
			done[n] = true
		}
		put(cd.device, 280)
		if cd.layout != nil {
			put(cd.layout, 240)
		}
		if cd.learn != nil {
			put(cd.learn, 200)
		}
		if cd.channel != nil {
			put(cd.channel, 96)
			put(cd.note, 110)
			put(cd.cc, 96)
		}
		for i, ct := range cd.controls {
			if k, ok := byNode[cd.forget[ct.ID]]; ok {
				cx, cy := l.cellAt(i, row+cardRow)
				k.Layout(gunim.Tight(geom.Sz(24, 24)))
				k.Place(geom.Pt(cx+cellW-26, cy+3))
				done[k.Node()] = true
			}
		}
		cd.height = cardHead + cardRow
		if cd.kind == show.Controller {
			cd.height += float32((len(cd.controls)+l.cols-1)/l.cols)*cellH + 8
		}
		y += cd.height + cardGap
	}
	for k := range kids.All {
		if !done[k.Node()] {
			k.Layout(gunim.Tight(k.Size()))
		}
	}
	return geom.Sz(w, max(y, 80))
}

// cellAt is where control i's cell stands, its grid starting at y.
func (l *cardList) cellAt(i int, y float32) (x, top float32) {
	cols := max(l.cols, 1)
	return cardInset*2 + float32(i%cols)*(cellW+8), y + float32(i/cols)*cellH
}

// Paint implements [gunim.Node]: each card, its name and what its port
// says, and its controls, each with its value or lit as it is held.
func (l *cardList) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	st := l.c.st
	if st == nil {
		return
	}
	if len(l.ids) == 0 {
		msg := face.Shape("No controllers yet. Add a Controller, a MIDI out or a MIDI clock on the Routes tab.", 12)
		msg.Paint(p, geom.Pt(cardInset+8, cardGap+8), quiet)
	}
	for _, id := range l.ids {
		cd, m := l.cards[id], st.Show.Module(id)
		if m == nil {
			continue
		}
		y := l.top(id)
		p.RRect(geom.Rc(cardInset, y, box.W-2*cardInset, cd.height), 10, paint.Solid(faded(ink, 0.04)))
		accent := kindInk(th, m.Kind)
		p.RRect(geom.Rc(cardInset, y, 4, cd.height), 2, paint.Solid(accent))
		t := bold.Shape(m.Name, 14)
		t.Paint(p, geom.Pt(cardInset*2, y+10), ink)
		if m.Kind == show.Routes {
			l.paintRoutes(p, f, cd, m, y, box.W)
			continue
		}
		says := l.c.played.Says[id]
		if m.Kind == show.Controller {
			d := l.c.played.Desks[id]
			says = d.Says
			if d.Last != "" {
				says += " · last heard " + d.Last
			}
		}
		if says != "" {
			s := face.Shape(says, 11)
			s.PaintFaded(p, geom.Pt(cardInset*2, y+29), quiet, max(box.W-4*cardInset, 40), widget.TextFade.Get(th))
		}
		if m.Kind != show.Controller {
			continue
		}
		vals := l.c.played.Desks[id].Values
		for i, c := range cd.controls {
			x, cy := l.cellAt(i, y+cardHead+cardRow)
			r := geom.Rc(x, cy+2, cellW, cellH-4)
			p.RRect(r, 6, paint.Solid(faded(ink, 0.05)))
			v := vals[c.ID]
			if c.Kind == show.Press {
				if v > 0 {
					p.RRect(r, 6, paint.Solid(faded(accent, 0.45)))
				}
			} else {
				fill := r
				fill.Max.X = r.Min.X + r.Size().W*float32(v)
				p.RRect(fill, 6, paint.Solid(faded(accent, 0.35)))
			}
			label := c.Name
			if c.Kind != show.Press {
				label += " · " + strconv.Itoa(int(v*100+0.5)) + "%"
			}
			n := face.Shape(label, 11)
			n.PaintFaded(p, geom.Pt(x+8, cy+8), ink, cellW-40, widget.TextFade.Get(th))
		}
	}
	for k := range kids.All {
		k.Paint(p)
	}
}
