package ui

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim/audio/capture"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/graph"
)

// routes is the Routes tab: the show's modules joined by wires, and a
// menu over them that adds modules. A fader's box holds its
// fader, and a DMX out's the box it sends to.
type routes struct {
	view    *graph.View
	add     *widget.MenuButton
	st      *app.State
	played  engine.Live
	th      *theme.Live
	faders  map[string]*widget.Slider
	devices map[string]*widget.Dropdown
	// The controls of the modules that hear and count: each Audio in's
	// input, each Tempo's tap button, its tempo and its follow box,
	// and each Pulse's length.
	inputs  map[string]*widget.Dropdown
	taps    map[string]*widget.Button
	bpms    map[string]*widget.NumberField
	follows map[string]*widget.Checkbox
	everys  map[string]*widget.Segmented
	maps    map[string]*mapFields
	// heard are the sound card's inputs, listed once, as an Audio in
	// first asks.
	heard  []capture.Device
	listed bool
	size   geom.Size
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
		inputs: map[string]*widget.Dropdown{}, taps: map[string]*widget.Button{}, bpms: map[string]*widget.NumberField{}, follows: map[string]*widget.Checkbox{},
		everys: map[string]*widget.Segmented{}, maps: map[string]*mapFields{}, ports: map[string][2][]engine.Port{}}
	r.view.Content = r.content
	r.view.Accepts = func(from, to graph.End) (bool, string) {
		if r.st == nil {
			return false, ""
		}
		return engine.CanJoin(&r.st.Show, show.End{Module: from.Node, Port: from.Port}, show.End{Module: to.Node, Port: to.Port})
	}
	var items []widget.MenuItem
	var kinds []show.Kind
	for i, group := range addGroups {
		if i > 0 {
			items, kinds = append(items, widget.MenuItem{Break: true}), append(kinds, "")
		}
		items, kinds = append(items, widget.MenuItem{Label: group.title, Caption: true}), append(kinds, "")
		for _, k := range group.kinds {
			items, kinds = append(items, widget.MenuItem{Label: k.Title(), Icon: kindIcon(k)}), append(kinds, k)
		}
	}
	r.add = widget.NewMenuButton("Add module", items)
	r.add.OnPick = func(i int, _ *gunim.UI) gunim.Intent {
		if i < 0 || i >= len(kinds) || kinds[i] == "" {
			return nil
		}
		at := r.view.ToPlane(geom.Pt(r.size.W/2, r.size.H/2)).Sub(geom.Pt(90, 40))
		return app.AddModule{Kind: kinds[i], At: at}
	}
	return r
}

// addGroups are the kinds of module the add menu offers, in groups.
var addGroups = []struct {
	title string
	kinds []show.Kind
}{
	{"Light", []show.Kind{show.Fader, show.Lights, show.Mixer, show.DMXOut, show.StageView}},
	{"Sound and beat", []show.Kind{show.AudioIn, show.Player, show.Beat, show.Tempo, show.Level, show.Pulse}},
	{"Controllers and logic", []show.Kind{show.Button, show.Controller, show.MIDIOut, show.Map, show.Every, show.Toggle}},
}

// kindIcon is the add menu's icon for a module of kind k.
func kindIcon(k show.Kind) *icon.Icon {
	switch k {
	case show.Fader:
		return icon.SlidersVertical
	case show.Lights:
		return icon.Lightbulb
	case show.Mixer:
		return icon.Blend
	case show.DMXOut:
		return icon.Cable
	case show.StageView:
		return icon.Theater
	case show.AudioIn:
		return icon.Mic
	case show.Beat:
		return icon.Activity
	case show.Tempo:
		return icon.Metronome
	case show.Level:
		return icon.Gauge
	case show.Pulse:
		return icon.Zap
	case show.Player:
		return icon.Music
	case show.Button:
		return icon.CirclePlay
	case show.Controller:
		return icon.Gamepad2
	case show.MIDIOut:
		return icon.Send
	case show.Map:
		return icon.Spline
	case show.Every:
		return icon.Repeat
	case show.Toggle:
		return icon.ToggleRight
	}
	return icon.Plus
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
	case show.AudioIn:
		return r.inputContent(m)
	case show.Tempo:
		return r.tempoContent(m)
	case show.Pulse:
		sg := widget.NewSegmented(everyNames...)
		sg.SetSelected(everyIndex(m.Every), nil)
		sg.OnChange = func(i int, _ *gunim.UI) gunim.Intent { return app.SetEvery{Module: id, Every: everys[i]} }
		r.everys[id] = sg
		return padded(sg)
	case show.Button:
		b := widget.NewButton("Press")
		b.OnClick = widget.Sends(app.Press{Module: id})
		return padded(b)
	case show.Every:
		sg := widget.NewSegmented(beatNames...)
		sg.SetSelected(beatIndex(m.Every), nil)
		sg.OnChange = func(i int, _ *gunim.UI) gunim.Intent { return app.SetEvery{Module: id, Every: beatCounts[i]} }
		r.everys[id] = sg
		return padded(sg)
	case show.Map:
		return r.mapContent(m)
	case show.Lights, show.Mixer, show.StageView, show.Beat, show.Level, show.Player, show.Controller, show.MIDIOut, show.Toggle:
	}
	return nil
}

// The beats an Every offers to fire every, and what its buttons say.
var (
	beatCounts = []float64{0.5, 1, 2, 4, 8, 16}
	beatNames  = []string{"½", "1", "2", "4", "8", "16"}
)

// beatIndex is which of the counts every is, 4 where it is none.
func beatIndex(every float64) int {
	if i := slices.Index(beatCounts, every); i >= 0 {
		return i
	}
	return slices.Index(beatCounts, 4)
}

// mapContent is a Map's box: the range it maps from, the range it maps
// to, and whether it turns the value over.
func (r *routes) mapContent(m *show.Module) gunim.Node {
	id := m.ID
	fields := [4]*widget.NumberField{}
	invert := widget.NewCheckbox("Turn over")
	send := func() gunim.Intent {
		return app.SetMap{Module: id, From: [2]float64{fields[0].Value(), fields[1].Value()},
			To: [2]float64{fields[2].Value(), fields[3].Value()}, Invert: invert.Checked()}
	}
	for i := range fields {
		f := widget.NewNumberField(0, 1)
		f.Decimals, f.Increment = 2, 0.05
		f.OnCommit = func(float64, *gunim.UI) gunim.Intent { return send() }
		fields[i] = f
	}
	invert.OnChange = func(bool, *gunim.UI) gunim.Intent { return send() }
	r.maps[id] = &mapFields{fields: fields, invert: invert}
	r.maps[id].set(m, nil)
	from, to := widget.NewLabel("From"), widget.NewLabel("to")
	return padded(widget.NewWrap(from, widget.NewSized(fields[0], 64, 0), widget.NewSized(fields[1], 64, 0), invert,
		to, widget.NewSized(fields[2], 64, 0), widget.NewSized(fields[3], 64, 0)))
}

// mapFields are a Map box's fields.
type mapFields struct {
	fields [4]*widget.NumberField
	invert *widget.Checkbox
}

// set shows module m's map, but in a field being typed.
func (f *mapFields) set(m *show.Module, u *gunim.UI) {
	from, to := m.From, m.To
	if from == [2]float64{} {
		from = [2]float64{0, 1}
	}
	if to == [2]float64{} {
		to = [2]float64{0, 1}
	}
	for i, v := range []float64{from[0], from[1], to[0], to[1]} {
		if u == nil || !u.HasFocus(f.fields[i]) {
			f.fields[i].SetValue(v, u)
		}
	}
	f.invert.SetChecked(m.Invert, u)
}

// listInputs lists the sound card's inputs. Tests put a list of their
// own in.
var listInputs = capture.Devices

// inputContent is an Audio in's box: a list of the inputs to listen to.
func (r *routes) inputContent(m *show.Module) gunim.Node {
	if !r.listed {
		r.listed = true
		r.heard, _ = listInputs()
	}
	names := append(make([]string, 0, 1+len(r.heard)), "Default input")
	ids := append(make([]string, 0, 1+len(r.heard)), "")
	for _, d := range r.heard {
		names = append(names, d.Name)
		ids = append(ids, d.ID)
	}
	d := widget.NewDropdown(widget.Labels(names...))
	if i := slices.Index(ids, m.Input); i >= 0 {
		d.SetSelected(i, nil)
	}
	id := m.ID
	d.OnChange = func(i int, _ *gunim.UI) gunim.Intent { return app.SetInput{Module: id, Input: ids[i]} }
	r.inputs[id] = d
	return padded(d)
}

// tempoContent is a Tempo's box: a button to tap the beat on, its own
// tempo to type, and whether it follows the clock wired into it.
func (r *routes) tempoContent(m *show.Module) gunim.Node {
	id := m.ID
	tap := widget.NewButton("Tap")
	tap.OnClick = widget.Sends(app.Tap{Module: id})
	bpm := widget.NewNumberField(app.MinBPM, app.MaxBPM)
	bpm.Decimals, bpm.Suffix = 1, " BPM"
	bpm.SetValue(m.BPM, nil)
	bpm.OnCommit = func(v float64, _ *gunim.UI) gunim.Intent { return app.SetTempo{Module: id, BPM: v, Manual: true} }
	follow := widget.NewCheckbox("Follow the clock in")
	follow.SetChecked(!m.Manual, nil)
	follow.OnChange = func(on bool, _ *gunim.UI) gunim.Intent { return app.SetTempo{Module: id, Manual: !on} }
	r.taps[id], r.bpms[id], r.follows[id] = tap, bpm, follow
	w := widget.NewWrap(tap, widget.NewSized(bpm, 104, 0), follow)
	return padded(w)
}

// The lengths a Pulse offers, in beats, and what its buttons say.
var (
	everys     = []float64{0.25, 0.5, 1, 2, 4}
	everyNames = []string{"¼", "½", "1", "2", "4"}
)

// everyIndex is which of the lengths every is, 1 where it is none.
func everyIndex(every float64) int {
	if i := slices.Index(everys, every); i >= 0 {
		return i
	}
	return slices.Index(everys, 1)
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
	for _, m := range []map[string]*widget.Dropdown{r.devices, r.inputs} {
		maps.DeleteFunc(m, func(id string, _ *widget.Dropdown) bool { return s.Show.Module(id) == nil })
	}
	maps.DeleteFunc(r.faders, func(id string, _ *widget.Slider) bool { return s.Show.Module(id) == nil })
	maps.DeleteFunc(r.taps, func(id string, _ *widget.Button) bool { return s.Show.Module(id) == nil })
	maps.DeleteFunc(r.bpms, func(id string, _ *widget.NumberField) bool { return s.Show.Module(id) == nil })
	maps.DeleteFunc(r.follows, func(id string, _ *widget.Checkbox) bool { return s.Show.Module(id) == nil })
	maps.DeleteFunc(r.everys, func(id string, _ *widget.Segmented) bool { return s.Show.Module(id) == nil })
	for id, f := range r.bpms {
		if m := s.Show.Module(id); m != nil && (u == nil || !u.HasFocus(f)) {
			f.SetValue(m.BPM, u)
			r.follows[id].SetChecked(!m.Manual, u)
		}
	}
	for id, sg := range r.everys {
		if m := s.Show.Module(id); m != nil {
			if m.Kind == show.Every {
				sg.SetSelected(beatIndex(m.Every), u)
			} else {
				sg.SetSelected(everyIndex(m.Every), u)
			}
		}
	}
	maps.DeleteFunc(r.maps, func(id string, _ *mapFields) bool { return s.Show.Module(id) == nil })
	for id, f := range r.maps {
		f.set(s.Show.Module(id), u)
	}
	r.sync(u)
}

// live follows what the engine plays: the faders move, and the boxes
// and the wires glow as their values rise. Nothing changed, nothing is
// done.
func (r *routes) live(l engine.Live, u *gunim.UI) {
	same := maps.Equal(l.Values, r.played.Values) && maps.Equal(l.Outs, r.played.Outs) &&
		maps.Equal(l.Clocks, r.played.Clocks) && maps.Equal(l.Says, r.played.Says)
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
		case show.DMXOut, show.AudioIn:
			n.Body = 36
		case show.Tempo:
			n.Body, n.Width = 76, 240
			n.Glow = float32(engine.Flash(r.played.Clocks[m.ID].Beats, 1)) * 0.6
		case show.Beat:
			if c := r.played.Clocks[m.ID]; c.BPM > 0 {
				n.Glow = float32(engine.Flash(c.Beats, 1)) * 0.6
			}
		case show.Pulse:
			n.Body, n.Width = 36, 200
			n.Glow = float32(r.value(m)) * 0.6
		case show.Level:
			n.Glow = float32(r.value(m)) * 0.6
		case show.Button:
			n.Body = 36
			n.Glow = float32(r.value(m)) * 0.8
		case show.Every:
			n.Body, n.Width = 36, 220
			n.Glow = float32(r.value(m)) * 0.8
		case show.Toggle:
			n.Glow = float32(r.value(m)) * 0.6
		case show.Map:
			n.Body, n.Width = 84, 260
			n.Glow = float32(r.value(m)) * 0.4
		case show.Controller, show.MIDIOut:
		case show.Player:
			if r.played.Playing.Playing {
				n.Glow = float32(engine.Flash(r.played.Clocks[m.ID].Beats, 1)) * 0.6
			}
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
			case engine.Clock:
				gw.Thin = true
				if m := s.Module(w.From.Module); m != nil {
					gw.Flow = float32(engine.Flash(r.played.Clocks[m.ID].Beats, 1))
				}
			case engine.Light, engine.DMX, engine.Audio:
				gw.Flow = 0.5
			case engine.Trigger:
				gw.Thin, gw.Dashed = true, true
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
	case show.AudioIn:
		if says, ok := r.played.Says[m.ID]; ok {
			return says
		}
		return "Opening…"
	case show.Beat:
		c := r.played.Clocks[m.ID]
		switch {
		case c.BPM <= 0:
			return "Listening for a beat…"
		case !c.Sure:
			return fmt.Sprintf("%.1f BPM, counting on", c.BPM)
		}
		return fmt.Sprintf("%.1f BPM", c.BPM)
	case show.Tempo:
		c := r.played.Clocks[m.ID]
		if c.BPM <= 0 {
			return ""
		}
		if !m.Manual && c.BPM != m.BPM {
			return fmt.Sprintf("%.1f BPM, following", c.BPM)
		}
		return fmt.Sprintf("%.1f BPM", c.BPM)
	case show.Level:
		return strconv.Itoa(int(r.value(m)*100+0.5)) + "%"
	case show.Player:
		pl := r.played.Playing
		g := r.st.Show.Song(pl.Song)
		switch {
		case pl.Err != "":
			return pl.Err
		case g == nil:
			return strconv.Itoa(len(r.st.Show.Songs)) + " songs"
		case pl.Playing:
			return "▶ " + g.Title
		}
		return g.Title
	case show.Button:
		return "Fires a trigger"
	case show.Controller:
		d := r.played.Desks[m.ID]
		if d.Says == "" {
			return "No device"
		}
		return d.Says
	case show.MIDIOut:
		if says := r.played.Says[m.ID]; says != "" {
			return says
		}
		return "No device"
	case show.Map:
		return strconv.Itoa(int(r.value(m)*100+0.5)) + "%"
	case show.Every:
		if m.Every == 1 {
			return "Every beat"
		}
		return "Every " + strconv.FormatFloat(beatCounts[beatIndex(m.Every)], 'f', -1, 64) + " beats"
	case show.Toggle:
		if r.value(m) > 0 {
			return "On"
		}
		return "Off"
	case show.Pulse:
		every := m.Every
		if every <= 0 {
			every = 1
		}
		if every == 1 {
			return "Every beat"
		}
		return "Every " + strconv.FormatFloat(every, 'f', -1, 64) + " beats"
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

// Children implements [gunim.Composite]: the add menu, then the view.
func (r *routes) Children() []gunim.Node { return []gunim.Node{r.add, r.view} }

// barH is how tall the bar over the view is.
const barH = 44

// Layout implements [gunim.Node]: the add menu along the top, the view
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
