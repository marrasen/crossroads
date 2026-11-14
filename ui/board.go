package ui

import (
	"image/color"
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// boardTab is the Board tab, which plays the show's first Board: its
// page, scrolled.
type boardTab struct {
	page   *boardPage
	scroll *widget.Scroll
}

func newBoardTab() *boardTab {
	b := newBoardPage()
	return &boardTab{page: b, scroll: widget.NewScroll(b)}
}

func (t *boardTab) show(st *app.State, u *gunim.UI) { t.page.show(st, u) }
func (t *boardTab) live(l engine.Live, u *gunim.UI) { t.page.live(l, u) }

// Children implements [gunim.Composite].
func (t *boardTab) Children() []gunim.Node { return []gunim.Node{t.scroll} }

// Layout implements [gunim.Node]: the page fills the tab, and grows
// past it, to scroll, only where what it holds needs more.
func (t *boardTab) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	t.page.view = c.Max
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (t *boardTab) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}

// boardPage is the Board tab's page, as a lighting desk: a pad for
// each light of the rig and each group, to pick with; a fader for each
// channel of what is picked, and Clear; the grand master, Blackout and
// Flash of the mixer the Board is wired into; and the DMX out that
// mixer feeds. A show with no Board offers to add one.
type boardPage struct {
	st     *app.State
	played engine.Live
	// view is how much of the page the tab shows.
	view geom.Size
	// board is the Board the page plays, mixer the Mixer it is wired
	// into and out the DMX out that feeds; "" for none.
	board, mixer, out string
	add, clear        *widget.Button
	master            *widget.Slider
	black             *widget.Button
	flash             *holdButton
	output            *outputFields
	// picked are the lights picked, as the engine last said, and faders
	// a fader for each channel of them.
	picked []string
	faders []*boardFader
	kids   []gunim.Node
	pads   []lightPad
	// lightsY, headY and outY are where the lights, the channels and
	// the output start, area is the faders' room, and column the
	// master's.
	lightsY, headY, outY float32
	area, column         geom.Rect
	per                  float32
	placed               map[gunim.Node]geom.Rect
}

// A boardFader is one of the page's channel faders: what it sets, its
// slider, the slot of its first channel, 0 for none, and the value it
// shows, which a fader holds or the light plays.
type boardFader struct {
	engine.Fader
	s     *widget.Slider
	slot  int
	held  bool
	value byte
}

// A lightPad is a pad that picks a light, or a group's lights.
type lightPad struct {
	label  string
	lights []string
	group  bool
	r      geom.Rect
}

func newBoardPage() *boardPage {
	b := &boardPage{placed: map[gunim.Node]geom.Rect{}}
	b.add = widget.NewButton("Add a board")
	b.add.Kind, b.add.Icon = widget.ButtonPrimary, icon.Plus
	b.add.OnClick = widget.Sends(app.AddBoard{})
	b.clear = widget.NewButton("Clear")
	b.clear.Icon = icon.Eraser
	b.clear.Tooltip = "Lets go of every fader"
	b.clear.OnClick = func(*gunim.UI) gunim.Intent { return app.ClearBoard{Module: b.board} }
	b.master = widget.NewVerticalSlider(0, 1)
	b.master.Label = "Grand master"
	b.master.OnChange = func(v float32, _ *gunim.UI) gunim.Intent { return app.SetValue{Module: b.mixer, Value: float64(v)} }
	b.master.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
		return app.SetValue{Module: b.mixer, Value: float64(v), Done: true}
	}
	b.black = widget.NewButton("Blackout")
	b.black.OnClick = func(*gunim.UI) gunim.Intent {
		return app.Override{Module: b.mixer, Which: engine.Blackout, On: !b.played.Overrides[b.mixer][0]}
	}
	b.flash = newHoldButton("Flash", func(down bool, _ *gunim.UI) gunim.Intent {
		return app.Override{Module: b.mixer, Which: engine.FlashAll, On: down}
	})
	b.output = newOutputFields("", false)
	return b
}

// show follows the show: the first Board, its mixer and its output, and
// a pad for each light and group.
func (b *boardPage) show(st *app.State, u *gunim.UI) {
	b.st = st
	s := &st.Show
	b.board, b.mixer, b.out = "", "", ""
	if i := slices.IndexFunc(s.Modules, func(m show.Module) bool { return m.Kind == show.Board }); i >= 0 {
		b.board = s.Modules[i].ID
		b.mixer = engine.MixerOf(s, b.board)
		b.out = engine.OutOf(s, b.mixer)
	}
	if m := s.Module(b.mixer); m != nil && !b.master.Held() {
		b.master.SetValue(float32(m.Value), u)
	}
	if m := s.Module(b.out); m != nil {
		b.output.set(b.out, m.Out, st.Ports, u)
	}
	b.pads = b.pads[:0]
	for _, p := range s.Rig.Lights {
		b.pads = append(b.pads, lightPad{label: p.Name, lights: []string{p.ID}})
	}
	for _, g := range s.Rig.Groups {
		b.pads = append(b.pads, lightPad{label: g.Name, lights: g.MemberIDs, group: true})
	}
	b.refader(u)
	u.Invalidate()
}

// live follows what the engine plays: what is picked, what each fader
// holds or its light plays, the master and the overrides.
func (b *boardPage) live(l engine.Live, u *gunim.UI) {
	b.played = l
	st := l.Boards[b.board]
	if !slices.Equal(st.Picked, b.picked) {
		b.picked = slices.Clone(st.Picked)
		b.refader(u)
	}
	for _, f := range b.faders {
		f.value, f.held = st.Held[f.Channels[0]]
		if !f.held && f.slot > 0 {
			f.value = st.Plays[f.slot-1]
		}
		if !f.s.Held() && f.s.Value() != float32(f.value) {
			f.s.SetValue(float32(f.value), u)
		}
	}
	if v, ok := l.Values[b.mixer]; ok && !b.master.Held() && float64(b.master.Value()) != v {
		b.master.SetValue(float32(v), u)
	}
	b.black.Active = l.Overrides[b.mixer][0]
	b.clear.Disabled = len(st.Held) == 0
	u.Invalidate()
}

// refader makes a fader for each channel of the lights picked, keeping
// the sliders there were, and the page's children what it now holds.
func (b *boardPage) refader(u *gunim.UI) {
	if b.st == nil {
		return
	}
	s := &b.st.Show
	cat := s.Catalog()
	fs := engine.Faders(s, b.picked)
	for i, f := range fs {
		if i == len(b.faders) {
			bf := &boardFader{s: widget.NewVerticalSlider(0, 255)}
			bf.s.Snap = 1
			hold := func(v float32, _ *gunim.UI) gunim.Intent {
				return app.Hold{Module: b.board, Channels: bf.Channels, Value: int(v + 0.5)}
			}
			bf.s.OnChange, bf.s.OnCommit = hold, hold
			b.faders = append(b.faders, bf)
		}
		bf := b.faders[i]
		bf.Fader, bf.slot = f, 0
		bf.s.Label = f.Name
		bf.s.Fill = color.NRGBA{}
		if c, ok := channelColour(f.Role); ok {
			bf.s.Fill = c
		}
		if p := s.Light(f.Channels[0].Light); p != nil {
			if slot, ok := cat.ResolveChannel(*p, f.Channels[0].Offset); ok {
				bf.slot = slot
			}
		}
	}
	b.faders = b.faders[:len(fs)]
	b.keep(u)
}

// keep makes the page's children the widgets it holds now, inserting
// those new and removing those gone.
func (b *boardPage) keep(u *gunim.UI) {
	var want []gunim.Node
	switch b.board {
	case "":
		want = append(want, b.add)
	default:
		want = append(want, b.clear)
		for _, f := range b.faders {
			want = append(want, f.s)
		}
		if b.mixer != "" {
			want = append(want, b.master, b.black, b.flash)
		}
		if b.out != "" {
			want = append(want, b.output.root)
		}
	}
	if slices.Equal(want, b.kids) {
		return
	}
	if u != nil {
		for _, n := range b.kids {
			if !slices.Contains(want, n) {
				u.Remove(n)
			}
		}
		for _, n := range want {
			if !slices.Contains(b.kids, n) {
				u.Insert(b, n)
			}
		}
	}
	b.kids = want
}

// channelColour is the colour of what a channel of role does: its
// colour's, or warm white for a dimmer; false for the rest.
func channelColour(r fixtures.Role) (color.NRGBA, bool) {
	if c, ok := roleColours[r]; ok {
		return c, true
	}
	if r == fixtures.RoleDimmer {
		return warmWhite, true
	}
	return color.NRGBA{}, false
}

// toward is c brought a share t of the way to to.
func toward(c, to color.NRGBA, t float32) color.NRGBA {
	mix := func(a, b uint8) uint8 { return uint8(float32(a) + (float32(b)-float32(a))*t + 0.5) }
	return color.NRGBA{R: mix(c.R, to.R), G: mix(c.G, to.G), B: mix(c.B, to.B), A: c.A}
}

// warmWhite is the colour of a light of a dimmer alone, and of a
// dimmer's fader.
var warmWhite = color.NRGBA{R: 0xff, G: 0xd9, B: 0xa0, A: 0xff}

// Children implements [gunim.Composite].
func (b *boardPage) Children() []gunim.Node { return b.kids }

// The page's measures.
const (
	boardSide   = 16
	boardHead   = 24
	lightW      = 132
	lightH      = 44
	lightGap    = 8
	boardStripW = 60
	stripName   = 22
	stripNumber = 24
	minStripH   = 150
	minStripW   = 44
	masterW     = 132
	buttonH     = 32
	outRowH     = 34
)

// Layout implements [gunim.Node]: the lights' pads in rows across the
// top; under them the faders, as wide as they have room for, the
// master's column at their right; and the output along the bottom.
func (b *boardPage) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	w := c.Max.W
	viewH := b.view.H
	if viewH <= 0 {
		viewH = 600
	}
	clear(b.placed)
	byNode := map[gunim.Node]gunim.Child{}
	for k := range kids.All {
		if k.Presence() != gunim.Exiting {
			byNode[k.Node()] = k
		}
	}
	lay := func(n gunim.Node, c gunim.Constraints) geom.Size {
		k, ok := byNode[n]
		if !ok {
			return geom.Size{}
		}
		delete(byNode, n)
		return k.Layout(c)
	}
	if b.board == "" {
		s := lay(b.add, gunim.Constraints{Max: geom.Sz(w, buttonH)})
		b.placed[b.add] = geom.Rc((w-s.W)/2, viewH/2+12, s.W, s.H)
		b.place(kids, byNode)
		return geom.Sz(w, viewH)
	}
	b.master.Fill = mixerInk.Get(f.Theme)
	right := w - boardSide
	if b.mixer != "" {
		right -= masterW + boardSide
	}
	y := float32(boardSide)
	b.lightsY = y
	y += boardHead
	cols := max(int((right-boardSide+lightGap)/(lightW+lightGap)), 1)
	for i := range b.pads {
		col, row := i%cols, i/cols
		b.pads[i].r = geom.Rc(boardSide+float32(col)*(lightW+lightGap), y+float32(row)*(lightH+lightGap), lightW, lightH)
	}
	y += float32((max(len(b.pads), 1)+cols-1)/cols)*(lightH+lightGap) + 8
	b.headY = y
	cs := lay(b.clear, gunim.Constraints{Max: geom.Sz(160, buttonH)})
	b.placed[b.clear] = geom.Rc(right-cs.W, y-4, cs.W, cs.H)
	y += buttonH + 8
	outH := float32(0)
	if b.mixer != "" {
		outH = boardHead + outRowH + 28
	}
	h := max(viewH-boardSide-outH-y, minStripH)
	n := len(b.faders)
	per := float32(boardStripW)
	if n > 0 {
		per = min(max((right-boardSide)/float32(n), minStripW), boardStripW)
		perRow := max(int((right-boardSide)/per), 1)
		rows := (n + perRow - 1) / perRow
		h = max(h, float32(rows)*minStripH)
		rowH := h / float32(rows)
		for i, bf := range b.faders {
			col, row := i%perRow, i/perRow
			sh := rowH - stripName - stripNumber - 8
			s := lay(bf.s, gunim.Constraints{Max: geom.Sz(per, sh), Min: geom.Sz(0, sh)})
			b.placed[bf.s] = geom.Rc(boardSide+float32(col)*per+(per-s.W)/2, y+float32(row)*rowH+stripName, s.W, s.H)
		}
	}
	b.area, b.per = geom.Rc(boardSide, y, right-boardSide, h), per
	if b.mixer != "" {
		// The master's column, from the top down to the faders' foot.
		x, top := w-boardSide-masterW, float32(boardSide)
		b.column = geom.Rc(x, top, masterW, y+h-top)
		sh := max(y+h-top-stripName-2*(buttonH+8), 60)
		s := lay(b.master, gunim.Constraints{Max: geom.Sz(masterW, sh), Min: geom.Sz(0, sh)})
		b.placed[b.master] = geom.Rc(x+(masterW-s.W)/2, top+stripName, s.W, s.H)
		by := top + stripName + sh + 8
		lay(b.black, gunim.Tight(geom.Sz(masterW, buttonH)))
		lay(b.flash, gunim.Tight(geom.Sz(masterW, buttonH)))
		b.placed[b.black] = geom.Rc(x, by, masterW, buttonH)
		b.placed[b.flash] = geom.Rc(x, by+buttonH+8, masterW, buttonH)
	}
	y += h + 12
	b.outY = y
	if b.out != "" {
		root := b.output.root
		s := lay(root, gunim.Constraints{Max: geom.Sz(w-2*boardSide, outRowH)})
		b.placed[root] = geom.Rc(boardSide, y+boardHead, s.W, s.H)
	}
	if b.mixer != "" {
		y += outH
	}
	b.place(kids, byNode)
	return geom.Sz(w, max(y+boardSide, viewH))
}

// place places each child where Layout put it. One on its way out
// keeps its size, and one Layout left out, as a fader beyond those that
// fit, has none.
func (b *boardPage) place(kids gunim.Children, left map[gunim.Node]gunim.Child) {
	for k := range kids.All {
		switch _, unlaid := left[k.Node()]; {
		case k.Presence() == gunim.Exiting:
			k.Layout(gunim.Tight(k.Size()))
		case unlaid:
			k.Layout(gunim.Tight(geom.Size{}))
		default:
			if r, ok := b.placed[k.Node()]; ok {
				k.Place(r.Min)
			}
		}
	}
}

// Paint implements [gunim.Node]: the heads, the pads, lit as they are
// picked and each light's swatch its colour as it plays, each fader's
// name and number, a fader no one holds drawn dim, and how the output
// stands.
func (b *boardPage) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if b.st == nil {
		return
	}
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	fade := widget.TextFade.Get(th)
	centred := func(t string, size, y float32, c color.NRGBA) {
		s := face.Shape(t, size)
		s.PaintFaded(p, geom.Pt(max((box.W-s.Advance)/2, boardSide), y), c, box.W-2*boardSide, fade)
	}
	if b.board == "" {
		h := bold.Shape("This show has no board.", 16)
		h.Paint(p, geom.Pt((box.W-h.Advance)/2, b.view.H/2-40), ink)
		centred("A board picks lights and sets their channels by hand, as a lighting desk does.", 12, b.view.H/2-14, quiet)
		for k := range kids.All {
			k.Paint(p)
		}
		return
	}
	s := &b.st.Show
	cat := s.Catalog()
	st := b.played.Boards[b.board]
	h := bold.Shape("Lights", 12)
	h.Paint(p, geom.Pt(boardSide, b.lightsY), quiet)
	if len(b.pads) == 0 {
		msg := face.Shape("No lights in the rig. Add them on the Rig tab.", 12)
		msg.Paint(p, geom.Pt(boardSide, b.lightsY+boardHead+8), quiet)
	}
	accent := widget.Accent.Get(th)
	for _, pd := range b.pads {
		lit := !slices.ContainsFunc(pd.lights, func(id string) bool { return !slices.Contains(st.Picked, id) }) && len(pd.lights) > 0
		fill := faded(ink, 0.06)
		if lit {
			fill = faded(accent, 0.35)
		}
		p.RRect(pd.r, 10, paint.Solid(fill))
		if lit {
			p.RRect(geom.Rc(pd.r.Min.X, pd.r.Min.Y, 4, pd.r.Size().H), 2, paint.Solid(accent))
		}
		x := pd.r.Min.X + 12
		sub := countOf(len(pd.lights), "light")
		if !pd.group {
			if l := s.Light(pd.lights[0]); l != nil {
				c, level := lampLook(cat, &st.Plays, *l)
				sw := geom.Rc(x, pd.r.Min.Y+(lightH-18)/2, 18, 18)
				p.RRect(sw, 9, paint.Solid(faded(ink, 0.12)))
				p.RRect(sw, 9, paint.Solid(faded(c, level)))
				sub = strconv.Itoa(int(level*100+0.5)) + "%"
			}
			x += 28
		}
		t := bold.Shape(pd.label, 13)
		t.PaintFaded(p, geom.Pt(x, pd.r.Min.Y+7), ink, pd.r.Max.X-x-8, fade)
		u := face.Shape(sub, 11)
		u.PaintFaded(p, geom.Pt(x, pd.r.Min.Y+25), quiet, pd.r.Max.X-x-8, fade)
	}
	h = bold.Shape("Channels · "+b.pickedText(), 12)
	h.PaintFaded(p, geom.Pt(boardSide, b.headY+4), quiet, b.area.Size().W-120, fade)
	if len(b.faders) == 0 {
		msg := face.Shape("Pick a light to set its channels. Ctrl+click picks more than one.", 12)
		at := b.area.Center()
		msg.PaintFaded(p, geom.Pt(max(at.X-msg.Advance/2, b.area.Min.X), at.Y-6), quiet, b.area.Size().W, fade)
	}
	for _, bf := range b.faders {
		r, ok := b.placed[bf.s]
		if !ok {
			continue
		}
		cell := b.per
		c := ink
		if cc, ok := channelColour(bf.Role); ok {
			// Its colour, brought toward the ink to read on the card.
			c = toward(cc, ink, 0.35)
		}
		name := face.Shape(bf.Name, 11)
		name.PaintFaded(p, geom.Pt(r.Center().X-min(name.Advance, cell-8)/2, r.Min.Y-stripName+4), c, cell-8, fade)
		held := bf.held || bf.s.Held()
		v := int(bf.value)
		if bf.s.Held() {
			v = int(bf.s.Value() + 0.5)
		}
		nc, nf := quiet, face
		if held {
			nc, nf = ink, bold
		}
		num := nf.Shape(strconv.Itoa(v), 12)
		num.Paint(p, geom.Pt(r.Center().X-num.Advance/2, r.Max.Y+6), nc)
	}
	if m := s.Module(b.mixer); m != nil {
		col := b.column
		mh := bold.Shape("Master "+strconv.Itoa(int(float64(b.master.Value())*100+0.5))+"%", 12)
		mh.PaintFaded(p, geom.Pt(col.Center().X-min(mh.Advance, masterW)/2, col.Min.Y+4), mixerInk.Get(th), masterW, fade)
	}
	b.paintOutput(p, f, box)
	for k := range kids.All {
		r, dim := b.placed[k.Node()], b.dim(k.Node())
		if !dim {
			k.Paint(p)
			continue
		}
		end := p.Layer(paint.LayerOpts{Bounds: geom.Rect{Min: r.Min.Sub(geom.Pt(4, 4)), Max: r.Max.Add(geom.Pt(4, 4))}, Opacity: 0.4})
		k.Paint(p)
		end()
	}
}

// paintOutput paints the output's head and how it stands, or why there
// is none.
func (b *boardPage) paintOutput(p *paint.Painter, f gunim.Frame, box geom.Size) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	fade := widget.TextFade.Get(th)
	s := &b.st.Show
	y := b.outY
	if b.mixer == "" {
		msg := face.Shape("The board is wired into no mixer, so nothing it holds goes out. Wire it into one on the Routes tab.", 12)
		msg.PaintFaded(p, geom.Pt(boardSide, y+4), quiet, box.W-2*boardSide, fade)
		return
	}
	m := s.Module(b.out)
	if m == nil {
		h := bold.Shape("Output", 12)
		h.Paint(p, geom.Pt(boardSide, y), quiet)
		msg := face.Shape("The mixer feeds no DMX out. Add one on the Routes tab.", 12)
		msg.PaintFaded(p, geom.Pt(boardSide, y+boardHead+8), quiet, box.W-2*boardSide, fade)
		return
	}
	h := bold.Shape("Output · "+m.Name, 12)
	h.PaintFaded(p, geom.Pt(boardSide, y), quiet, box.W-2*boardSide, fade)
	says, connected := outSays(m, b.played)
	dot := quiet
	switch {
	case connected:
		says = "Connected to " + says
		dot = audioInk.Get(th)
	case m.Out.Device != "" && b.played.Outs[m.ID].Err != "":
		dot = stageInk.Get(th)
	}
	sy := y + boardHead + outRowH + 8
	p.RRect(geom.Rc(boardSide, sy+3, 8, 8), 4, paint.Solid(dot))
	t := face.Shape(says, 11)
	t.PaintFaded(p, geom.Pt(boardSide+14, sy), ink, box.W-2*boardSide-14, fade)
}

// dim reports whether n is a channel fader nothing holds, drawn dim.
func (b *boardPage) dim(n gunim.Node) bool {
	for _, bf := range b.faders {
		if bf.s == n {
			return !bf.held && !bf.s.Held()
		}
	}
	return false
}

// pickedText is what is picked, as the channels' head says it.
func (b *boardPage) pickedText() string {
	if len(b.picked) == 1 {
		if l := b.st.Show.Light(b.picked[0]); l != nil {
			return l.Name
		}
	}
	if len(b.picked) == 0 {
		return "nothing picked"
	}
	return countOf(len(b.picked), "light")
}

// Handle implements [gunim.Handler]: a pad pressed picks its light, or
// its group's, alone; with Ctrl, it adds them, or takes them out.
func (b *boardPage) Handle(e input.Event, u *gunim.UI) bool {
	d, ok := e.(input.PointerDown)
	if !ok || d.Button != input.ButtonPrimary || b.board == "" {
		return false
	}
	for _, pd := range b.pads {
		if pd.r.Contains(d.Pos) {
			u.Send(b, app.Pick{Module: b.board, Lights: pd.lights, Toggle: d.Mods.Has(input.ModControl)})
			return true
		}
	}
	return false
}

// countOf is n things, as "1 light" or "3 lights".
func countOf(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}
