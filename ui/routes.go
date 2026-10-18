package ui

import (
	"fmt"
	"maps"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/graph"
)

// routes is the Routes tab: the show's modules joined by wires, and a
// row of buttons over them that add modules. A fader's box holds its
// fader, and a DMX out's the box it sends to.
type routes struct {
	view    *graph.View
	add     []*widget.Button
	st      *app.State
	played  engine.Live
	th      *theme.Live
	faders  map[string]*widget.Slider
	devices map[string]*widget.Dropdown
	size    geom.Size
	// ports are each module's inputs and outputs, as the show last
	// gave them.
	ports map[string][2][]engine.Port
}

// The DMX boxes a DMX out offers, by the show's names for them.
var devices = []struct{ id, name string }{
	{"", "Nowhere"},
	{"enttec", "ENTTEC USB Pro"},
	{"opendmx", "ENTTEC Open DMX"},
	{"artnet", "Art-Net"},
	{"sacn", "sACN"},
}

func newRoutes() *routes {
	r := &routes{view: graph.NewView(), faders: map[string]*widget.Slider{}, devices: map[string]*widget.Dropdown{},
		ports: map[string][2][]engine.Port{}}
	r.view.Content = r.content
	r.view.Accepts = func(from, to graph.End) (bool, string) {
		if r.st == nil {
			return false, ""
		}
		return engine.CanJoin(&r.st.Show, show.End{Module: from.Node, Port: from.Port}, show.End{Module: to.Node, Port: to.Port})
	}
	for _, k := range show.Kinds {
		b := widget.NewButton("+ " + k.Title())
		b.OnClick = func(*gunim.UI) gunim.Intent {
			at := r.view.ToPlane(geom.Pt(r.size.W/2, r.size.H/2)).Sub(geom.Pt(90, 40))
			return app.AddModule{Kind: k, At: at}
		}
		r.add = append(r.add, b)
	}
	return r
}

// content makes what module m's box holds: a fader's fader, a DMX out's
// box to send to.
func (r *routes) content(id string) gunim.Node {
	if r.st == nil {
		return nil
	}
	m := r.st.Show.Module(id)
	if m == nil {
		return nil
	}
	switch m.Kind {
	case show.Fader:
		s := widget.NewSlider(0, 1)
		s.SetValue(float32(m.Value), nil)
		s.OnChange = func(v float32, _ *gunim.UI) gunim.Intent { return app.SetValue{Module: id, Value: float64(v)} }
		s.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
			return app.SetValue{Module: id, Value: float64(v), Done: true}
		}
		r.faders[id] = s
		return padded(s)
	case show.DMXOut:
		names := make([]string, len(devices))
		for i, d := range devices {
			names[i] = d.name
		}
		d := widget.NewDropdown(widget.Labels(names...))
		for i, dv := range devices {
			if dv.id == m.Out.Device {
				d.SetSelected(i, nil)
			}
		}
		d.OnChange = func(i int, _ *gunim.UI) gunim.Intent {
			out := m.Out
			if mm := r.st.Show.Module(id); mm != nil {
				out = mm.Out
			}
			out.Device = devices[i].id
			return app.SetOutput{Module: id, Out: out}
		}
		r.devices[id] = d
		return padded(d)
	case show.Lights, show.Mixer, show.StageView:
	}
	return nil
}

// padding is the room round what a box holds.
var padding = theme.Insets("crossroads.box.padding", geom.Insets{Left: 12, Right: 12, Top: 2, Bottom: 2})

func padded(n gunim.Node) gunim.Node {
	p := widget.NewPad(n)
	p.Padding = padding
	return p
}

// show follows the show, working out each module's ports once.
func (r *routes) show(s *app.State, u *gunim.UI) {
	r.st = s
	clear(r.ports)
	for i := range s.Show.Modules {
		m := &s.Show.Modules[i]
		ins, outs := engine.Ports(&s.Show, m)
		r.ports[m.ID] = [2][]engine.Port{ins, outs}
	}
	for id, d := range r.devices {
		if m := s.Show.Module(id); m != nil {
			for i, dv := range devices {
				if dv.id == m.Out.Device {
					d.SetSelected(i, u)
				}
			}
		}
	}
	r.sync(u)
}

// live follows what the engine plays: the faders move, and the boxes
// and the wires glow as their values rise. Nothing changed, nothing is
// done.
func (r *routes) live(l engine.Live, u *gunim.UI) {
	same := maps.Equal(l.Values, r.played.Values) && maps.Equal(l.Outs, r.played.Outs)
	r.played = l
	if same {
		return
	}
	for id, s := range r.faders {
		if v, ok := l.Values[id]; ok && !s.Held() && float64(s.Value()) != v {
			s.SetValue(float32(v), u)
		}
	}
	if r.st != nil && u != nil {
		r.sync(u)
	}
}

// sync shows the show's modules as boxes, and its wires.
func (r *routes) sync(u *gunim.UI) {
	if r.st == nil {
		return
	}
	th := r.th
	if u != nil {
		th = u.Theme()
	}
	s := &r.st.Show
	var g graph.Graph
	for i := range s.Modules {
		m := &s.Modules[i]
		ps := r.ports[m.ID]
		ins, outs := ps[0], ps[1]
		n := graph.Node{ID: m.ID, Title: m.Name, Sub: r.detail(m), At: m.At, Ins: ports(th, ins), Outs: ports(th, outs)}
		if th != nil {
			n.Color = kindInk(th, m.Kind)
		}
		switch m.Kind {
		case show.Fader:
			n.Body = 30
			n.Glow = float32(r.value(m)) * 0.6
		case show.DMXOut:
			n.Body = 36
		case show.Lights, show.Mixer, show.StageView:
		}
		g.Nodes = append(g.Nodes, n)
	}
	for _, w := range s.Wires {
		gw := graph.Wire{ID: w.ID, From: graph.End{Node: w.From.Module, Port: w.From.Port}, To: graph.End{Node: w.To.Module, Port: w.To.Port}}
		if p, ok := engine.PortOf(s, w.From, true); ok {
			switch p.Signal {
			case engine.Value:
				gw.Thin, gw.Dashed = true, true
				if m := s.Module(w.From.Module); m != nil {
					gw.Flow = float32(r.value(m))
				}
			case engine.Light, engine.DMX:
				gw.Flow = 0.5
			}
		}
		g.Wires = append(g.Wires, gw)
	}
	r.view.SetGraph(g, u)
}

// value is module m's value as the engine plays it now.
func (r *routes) value(m *show.Module) float64 {
	if v, ok := r.played.Values[m.ID]; ok {
		return v
	}
	return m.Value
}

// detail is what module m's box says under its name.
func (r *routes) detail(m *show.Module) string {
	switch m.Kind {
	case show.Fader:
		return strconv.Itoa(int(r.value(m)*100+0.5)) + "%"
	case show.Lights:
		if len(m.Lights) == 1 {
			return "1 light"
		}
		return strconv.Itoa(len(m.Lights)) + " lights"
	case show.Mixer:
		return fmt.Sprintf("Master %d%%", int(r.value(m)*100+0.5))
	case show.DMXOut:
		st, ok := r.played.Outs[m.ID]
		switch {
		case m.Out.Device == "":
			return "Sends nowhere"
		case !ok || st.Status == dmx.Disconnected:
			return "Not connected"
		case st.Status == dmx.Failed:
			return st.Err
		case st.Status == dmx.Connecting:
			return "Connecting…"
		}
		return st.Info.Label()
	case show.StageView:
		return "On the Stage tab"
	}
	return ""
}

// ports are a module's ports, as the graph draws them.
func ports(th *theme.Live, ps []engine.Port) []graph.Port {
	out := make([]graph.Port, len(ps))
	for i, p := range ps {
		gp := graph.Port{ID: p.ID, Name: p.Name, Kind: string(p.Signal), Shape: graph.Ring}
		if p.Signal == engine.Value {
			gp.Shape = graph.Diamond
		}
		if th != nil {
			gp.Color = signalInk(th, p.Signal)
		}
		out[i] = gp
	}
	return out
}

// Children implements [gunim.Composite]: the buttons, then the view.
func (r *routes) Children() []gunim.Node {
	out := make([]gunim.Node, 0, len(r.add)+1)
	for _, b := range r.add {
		out = append(out, b)
	}
	return append(out, r.view)
}

// barH is how tall the row of buttons is.
const barH = 44

// Layout implements [gunim.Node]: the buttons along the top, the view
// under them.
func (r *routes) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	if r.th == nil {
		// The first layout: the boxes take the theme's colours.
		r.th = f.Theme
		defer r.sync(nil)
	}
	x := float32(8)
	n := kids.Len()
	for i := range n - 1 {
		k := kids.At(i)
		s := k.Layout(gunim.Constraints{Max: geom.Sz(size.W, barH-8)})
		k.Place(geom.Pt(x, (barH-s.H)/2))
		x += s.W + 8
	}
	v := kids.At(n - 1)
	r.size = geom.Sz(size.W, max(size.H-barH, 0))
	v.Layout(gunim.Tight(r.size))
	v.Place(geom.Pt(0, barH))
	return size
}

// Paint implements [gunim.Node].
func (r *routes) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(f.Theme)))
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Clip: true, Radius: 12, Opacity: 1})()
	for k := range kids.All {
		k.Paint(p)
	}
}
