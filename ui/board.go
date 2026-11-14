package ui

import (
	"image/color"
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// boardTab is the Board tab, which plays the show's first Board: its
// page, with the menu a scene's pad or a chase opens, scrolled.
type boardTab struct {
	page   *boardPage
	scroll *widget.Scroll
}

func newBoardTab() *boardTab {
	b := newBoardPage()
	return &boardTab{page: b, scroll: widget.NewScroll(b.padMenu())}
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

// boardPage is the Board tab's page, as a lighting desk: the pads of
// the scenes, with Record and the scene fade; the chases, with their
// speed, fade and Tap, and Follow the beat; a pad for each light of
// the rig and each group, to pick with; a fader for each channel of
// what is picked, and Clear; the grand master, Blackout and Flash of
// the mixer the Board is wired into; and the DMX out that mixer feeds.
// Along the top, the controller it is played from, or the offer to add
// an X-Touch Mini. A show with no Board offers to add one.
type boardPage struct {
	st     *app.State
	played engine.Live
	// menu is the menu a scene's pad or a chase opens, and menuOpen
	// whether it was open when last looked at: as it opened, and at
	// each tick.
	menu     *widget.ContextMenu
	menuOpen bool
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
	// record is Record, and fade the scene fade; scenes are the pads of
	// the scenes, one for each pad of the Board.
	record *widget.Button
	fade   *widget.Slider
	scenes [show.Pads]scenePad
	// from picks the controller the Board is played from, of remotes,
	// "" first for none; addTouch adds an X-Touch Mini, offered while
	// the show has none; and fromY is where their row stands. names are
	// what the picker offers.
	from     *widget.Dropdown
	remotes  []string
	names    []string
	addTouch *widget.Button
	hasTouch bool
	fromY    float32
	// The chases' controls, and a button for each chase, in order;
	// making is the place of the one being made. clocked says a clock
	// is wired into the Board.
	chaseControls
	chases  []chasePad
	making  geom.Rect
	clocked bool
	// picked are the lights picked, as the engine last said, and faders
	// a fader for each channel of them.
	picked []string
	faders []*boardFader
	kids   []gunim.Node
	pads   []lightPad
	// scenesY, chasesY, tempoY, lightsY, headY and outY are where the
	// scenes, the chases, their speed, the lights, the channels and the
	// output start, area is the faders' room, and column the master's.
	scenesY, chasesY, tempoY, lightsY, headY, outY float32
	area, column                                   geom.Rect
	per                                            float32
	placed                                         map[gunim.Node]geom.Rect
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
	// drag is the channels a drag under way sets, nil for none.
	drag []engine.Channel
}

// A scenePad is a pad for a scene: the scene's name, where it has one,
// the colour it plays and how bright, and its place.
type scenePad struct {
	name   string
	full   bool
	colour color.NRGBA
	level  float32
	r      geom.Rect
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
	b.record = widget.NewButton("Record")
	b.record.Icon = icon.Disc
	b.record.Tooltip = "Then press a pad to store on it what the board plays. Right-click a pad to rename or delete its scene."
	b.record.OnClick = func(*gunim.UI) gunim.Intent { return app.PressRecord{Module: b.board} }
	b.fade = widget.NewSlider(0, show.MaxSceneFade)
	b.fade.Label, b.fade.Snap = "Scene fade", 0.1
	// The seconds beside it follow it as it moves.
	b.fade.OnChange = func(_ float32, u *gunim.UI) gunim.Intent {
		u.Invalidate()
		return nil
	}
	b.fade.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
		return app.SetSceneFade{Module: b.board, Seconds: float64(v)}
	}
	b.from = widget.NewDropdown(nil)
	b.from.OnChange = func(i int, _ *gunim.UI) gunim.Intent {
		if i < 0 || i >= len(b.remotes) {
			return nil
		}
		return app.PlayFrom{Module: b.board, Controller: b.remotes[i]}
	}
	b.addTouch = widget.NewButton("Add an X-Touch Mini")
	b.addTouch.Icon = icon.Plus
	b.addTouch.Tooltip = "Adds a Behringer X-Touch Mini on its MIDI port, which then plays the board: its knobs set the channels, its top row presses the pads"
	b.addTouch.OnClick = func(*gunim.UI) gunim.Intent { return app.AddXTouchMini{Module: b.board} }
	b.newChaseControls()
	return b
}

// padMenu is the page with the menu a scene's pad or a chase opens on
// a right click: Rename and Delete, for the scene on the pad pressed
// then; Rename, Remove the last step and Delete, for the chase.
func (b *boardPage) padMenu() *widget.ContextMenu {
	c := widget.NewContextMenu(b, nil)
	b.menu = c
	var board string
	pad, chase := -1, chasePad{}
	c.Prepare = func(at geom.Point, _ *gunim.UI) bool {
		board, pad, chase = b.board, b.sceneAt(at), chasePad{}
		if board == "" {
			return false
		}
		switch i := b.chaseAt(at); {
		case pad >= 0 && b.scenes[pad].full:
			c.SetItems([]widget.MenuItem{{Label: "Rename…", Icon: icon.Pencil}, {Label: "Delete", Icon: icon.Trash2}})
		case i >= 0:
			pad, chase = -1, b.chases[i]
			c.SetItems([]widget.MenuItem{{Label: "Rename…", Icon: icon.Pencil}, {Label: "Remove the last step", Icon: icon.Undo2},
				{Label: "Delete", Icon: icon.Trash2}})
		default:
			return false
		}
		b.menuOpen = true
		return true
	}
	c.OnPick = func(i int, u *gunim.UI) gunim.Intent {
		if pad >= 0 {
			if i == 1 {
				return app.DeleteScene{Module: board, Pad: pad}
			}
			b.rename("Rename the scene on pad "+strconv.Itoa(pad+1), b.scenes[pad].name, func(name string) gunim.Intent {
				return app.RenameScene{Module: board, Pad: pad, Name: name}
			}, u)
			return nil
		}
		id := chase.id
		switch i {
		case 1:
			return app.RemoveStep{Module: board, Chase: id}
		case 2:
			return app.DeleteChase{Module: board, Chase: id}
		}
		b.rename("Rename the chase", chase.name, func(name string) gunim.Intent {
			return app.RenameChase{Module: board, Chase: id, Name: name}
		}, u)
		return nil
	}
	return c
}

// rename asks, over the window, under title, for a new name in place of
// name, which named makes the intent of.
func (b *boardPage) rename(title, name string, named func(string) gunim.Intent, u *gunim.UI) {
	root := u.Root()
	if root == nil {
		return
	}
	d := widget.NewDialog(title)
	field := widget.NewTextField()
	field.SetText(name, nil)
	field.Select(0, len([]rune(name)))
	d.Body = widget.NewForm().Add("Name", field)
	d.SetButtons("Rename", "Cancel")
	d.OnAccept = func(*gunim.UI) gunim.Intent { return named(field.Text()) }
	u.Insert(root, d)
}

// sceneAt is the pad of a scene at p, -1 for none.
func (b *boardPage) sceneAt(p geom.Point) int {
	for i := range b.scenes {
		if b.scenes[i].r.Contains(p) {
			return i
		}
	}
	return -1
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
	cat := s.Catalog()
	for i := range b.scenes {
		sp := &b.scenes[i]
		sp.name, sp.full, sp.colour, sp.level = "", false, color.NRGBA{}, 0
		if m := s.Module(b.board); m != nil {
			if sc := m.Scene(i); sc != nil {
				sp.name, sp.full = sc.Name, true
				sp.colour, sp.level = sceneLook(s, cat, sc)
			}
		}
	}
	if m := s.Module(b.board); m != nil && !b.fade.Held() {
		b.fade.SetValue(float32(m.SceneFade), u)
	}
	b.showChases(s, s.Module(b.board), u)
	b.showRemotes(s, u)
	b.pads = b.pads[:0]
	for _, p := range s.Rig.Lights {
		b.pads = append(b.pads, lightPad{label: p.Name, lights: []string{p.ID}})
	}
	for _, g := range s.Rig.Groups {
		b.pads = append(b.pads, lightPad{label: g.Name, lights: g.MemberIDs, group: true})
	}
	b.refader(u)
	// Once the output's fields are in the tree, which refader sees to.
	if m := s.Module(b.out); m != nil {
		b.output.set(b.out, m.Out, st.Ports, u)
	}
	u.Invalidate()
}

// live follows what the engine plays: what is picked, what each fader
// holds or its light plays, the master and the overrides.
func (b *boardPage) live(l engine.Live, u *gunim.UI) {
	b.played = l
	b.menuOpen = b.menu != nil && b.menu.Focusable()
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
	// Red while it waits for a pad.
	b.record.Kind = widget.ButtonPlain
	if st.Record {
		b.record.Kind = widget.ButtonDanger
	}
	b.liveChases(&st, u)
	u.Invalidate()
}

// sceneLook is the colour scene sc of show s plays its lights at, and
// how bright, 0 to 1: their colours mixed, each as bright as it plays,
// and the brightest light's level.
func sceneLook(s *show.Show, cat fixtures.Catalog, sc *show.Scene) (c color.NRGBA, lit float32) {
	var u dmx.Universe
	var lights []fixtures.Patch
	for _, p := range s.Rig.Lights {
		chans, ok := sc.Values[p.ID]
		if !ok {
			continue
		}
		lights = append(lights, p)
		for off, v := range chans {
			if slot, ok := cat.ResolveChannel(p, off); ok && slot >= 1 && slot <= dmx.Channels {
				u[slot-1] = v
			}
		}
	}
	var r, g, bl, peak float32
	for _, p := range lights {
		c, level := lampLook(cat, &u, p)
		r += float32(c.R) * level
		g += float32(c.G) * level
		bl += float32(c.B) * level
		peak = max(peak, level)
	}
	top := max(r, g, bl)
	if top <= 0 {
		return color.NRGBA{}, 0
	}
	k := 255 / top
	return color.NRGBA{R: uint8(r * k), G: uint8(g * k), B: uint8(bl * k), A: 0xff}, peak
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
			// A drag sets the channels the fader had as it began, though
			// other lights are picked meanwhile, as from another window.
			bf.s.OnChange = func(v float32, _ *gunim.UI) gunim.Intent {
				chans := bf.Channels
				if bf.s.Held() {
					if bf.drag == nil {
						bf.drag = bf.Channels
					}
					chans = bf.drag
				} else {
					bf.drag = nil
				}
				return app.Hold{Module: b.board, Channels: chans, Value: int(v + 0.5)}
			}
			bf.s.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
				chans := bf.Channels
				if bf.drag != nil {
					chans, bf.drag = bf.drag, nil
				}
				return app.Hold{Module: b.board, Channels: chans, Value: int(v + 0.5)}
			}
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
		want = append(want, b.from)
		if !b.hasTouch {
			want = append(want, b.addTouch)
		}
		want = append(want, b.record, b.fade)
		want = append(want, b.chaseKids()...)
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
	sceneH      = 40
	sceneGap    = 6
	scenesRow   = 8
	fadeW       = 120
)

// Layout implements [gunim.Node]: the scenes' pads across the top,
// eight a row, under Record and the scene fade; under them the lights'
// pads in rows, then the faders, as wide as they have room for, the
// master's column at the right of both; and the output along the
// bottom.
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
	full := w - boardSide
	right := full
	if b.mixer != "" {
		right -= masterW + boardSide
	}
	y := float32(boardSide)
	y = b.layRemote(y, full, lay)
	b.scenesY = y
	rs := lay(b.record, gunim.Constraints{Max: geom.Sz(160, buttonH)})
	b.placed[b.record] = geom.Rc(full-rs.W, y-4, rs.W, rs.H)
	fs := lay(b.fade, gunim.Constraints{Max: geom.Sz(fadeW, buttonH)})
	b.placed[b.fade] = geom.Rc(full-rs.W-16-fadeW, y-4+(rs.H-fs.H)/2, fs.W, fs.H)
	y += buttonH + 2
	cw := (full - boardSide - (scenesRow-1)*sceneGap) / scenesRow
	for i := range b.scenes {
		col, row := i%scenesRow, i/scenesRow
		b.scenes[i].r = geom.Rc(boardSide+float32(col)*(cw+sceneGap), y+float32(row)*(sceneH+sceneGap), cw, sceneH)
	}
	y += float32((len(b.scenes)+scenesRow-1)/scenesRow)*(sceneH+sceneGap) + 8
	y = b.layChases(y, full, cw, b.played.Boards[b.board].Making, lay)
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
	// Room under the faders: for the output, or, with no mixer, for the
	// line that says so.
	outH := float32(36)
	if b.mixer != "" {
		outH = boardHead + outRowH + 4
	}
	// The faders fill what is left, less the gap over the output and
	// the page's foot.
	h := max(viewH-y-12-outH-12, minStripH)
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
		// The master's column, from the lights down to the faders' foot.
		x, top := w-boardSide-masterW, b.lightsY
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
	y += outH
	b.place(kids, byNode)
	return geom.Sz(w, max(y+12, viewH))
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
	accent := widget.Accent.Get(th)
	b.paintRemote(p, f, box, &st)
	b.paintScenes(p, f, &st)
	b.paintChases(p, f, &st)
	h := bold.Shape("Lights", 12)
	h.Paint(p, geom.Pt(boardSide, b.lightsY), quiet)
	if len(b.pads) == 0 {
		msg := face.Shape("No lights in the rig. Add them on the Rig tab.", 12)
		msg.Paint(p, geom.Pt(boardSide, b.lightsY+boardHead+8), quiet)
	}
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
	for i, bf := range b.faders {
		r, ok := b.placed[bf.s]
		if !ok {
			continue
		}
		cell := b.per
		if b.held(&st, i) {
			// One of the eight the controller's knobs set.
			mark := geom.Rc(r.Center().X-cell/2+2, r.Min.Y-stripName, cell-4, r.Size().H+stripName+stripNumber)
			p.RRect(mark, 8, paint.Solid(faded(midiInk.Get(th), 0.12)))
			p.RRect(geom.Rc(mark.Min.X+6, mark.Min.Y, mark.Size().W-12, 2), 1, paint.Solid(midiInk.Get(th)))
		}
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

// paintScenes paints the scenes' head, the scene fade, and the pads:
// one with a scene shows its name and a swatch of its colour, lit as it
// is on and as far as it has faded in; an empty one is dim. While Record
// waits, the head says so and every pad is ringed in its colour. While
// a chase is made, each pad says where it comes in it; while one runs,
// the pad of its step is ringed.
func (b *boardPage) paintScenes(p *paint.Painter, f gunim.Frame, st *engine.BoardState) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	fade := widget.TextFade.Get(th)
	accent, red := widget.Accent.Get(th), recordInk.Get(th)
	fr := b.placed[b.fade]
	secs := strconv.FormatFloat(float64(b.fade.Value()), 'f', 1, 32)
	ft := face.Shape("Fade "+secs+" s", 12)
	fx := fr.Min.X - 8 - ft.Advance
	ft.Paint(p, geom.Pt(fx, b.scenesY+4), quiet)
	head, hc := "Scenes", quiet
	if st.Record {
		head, hc = "Recording · press a pad to store what plays, or Esc", red
	} else if !slices.ContainsFunc(b.scenes[:], func(sp scenePad) bool { return sp.full }) {
		head = "Scenes · Record, then a pad, stores what plays"
	}
	h := bold.Shape(head, 12)
	h.PaintFaded(p, geom.Pt(boardSide, b.scenesY+4), hc, fx-boardSide-12, fade)
	for i := range b.scenes {
		sp := &b.scenes[i]
		r := sp.r
		on, level := b.sceneLit(i)
		fill := faded(ink, 0.03)
		if sp.full {
			fill = faded(ink, 0.07)
		}
		p.RRect(r, 10, paint.Solid(fill))
		if level > 0 {
			p.RRect(r, 10, paint.Solid(faded(accent, 0.35*level)))
		}
		if on {
			p.RRect(geom.Rc(r.Min.X, r.Min.Y, 4, r.Size().H), 2, paint.Solid(accent))
		}
		if b.held(st, i) {
			// One of the eight the controller's top row presses.
			p.RRect(geom.Rc(r.Min.X+10, r.Min.Y+3, max(r.Size().W-40, 8), 3), 1.5, paint.Solid(midiInk.Get(th)))
		}
		switch {
		case st.Record:
			p.RRectStroke(r, 10, paint.Fill{}, paint.Stroke{Width: 1.5, Color: faded(red, 0.8)})
		case st.Making && sp.full:
			p.RRectStroke(r, 10, paint.Fill{}, paint.Stroke{Width: 1.5, Color: faded(accent, 0.8)})
		case b.stepPad(st) == i:
			p.RRectStroke(r, 10, paint.Fill{}, paint.Stroke{Width: 2, Color: accent})
		}
		// The pad's number, or, while a chase is made, where the pad
		// comes in it.
		num := face.Shape(strconv.Itoa(i+1), 10)
		nc := faded(ink, 0.3)
		if sp.full {
			nc = quiet
		}
		if order := stepsOn(st.Steps, i); st.Making && order != "" {
			num, nc = bold.Shape(order, 11), accent
		}
		room := r.Size().W / 2
		num.PaintFaded(p, geom.Pt(r.Max.X-8-min(num.Advance, room), r.Min.Y+5), nc, room, fade)
		if !sp.full {
			continue
		}
		sw := geom.Rc(r.Min.X+10, r.Min.Y+8, 12, 12)
		p.RRect(sw, 6, paint.Solid(faded(ink, 0.12)))
		p.RRect(sw, 6, paint.Solid(faded(sp.colour, max(sp.level, 0.15))))
		t := bold.Shape(sp.name, 12)
		t.PaintFaded(p, geom.Pt(r.Min.X+10, r.Min.Y+24), ink, r.Size().W-18, fade)
	}
}

// sceneLit reports, as the engine last said, whether pad's scene is on,
// and how far it has faded in, 0 to 1.
func (b *boardPage) sceneLit(pad int) (on bool, level float32) {
	st := b.played.Boards[b.board]
	return slices.Contains(st.On, pad), float32(st.Levels[pad])
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
	hw := min(h.Advance, (box.W-2*boardSide)/2)
	h.PaintFaded(p, geom.Pt(boardSide, y), quiet, hw, fade)
	says, connected := outSays(m, b.played)
	dot := quiet
	switch {
	case connected:
		says = "Connected to " + says
		dot = audioInk.Get(th)
	case m.Out.Device != "" && b.played.Outs[m.ID].Err != "":
		dot = stageInk.Get(th)
	}
	// How it stands, on the head's line.
	x := boardSide + hw + 16
	p.RRect(geom.Rc(x, y+4, 8, 8), 4, paint.Solid(dot))
	t := face.Shape(says, 11)
	t.PaintFaded(p, geom.Pt(x+14, y+1), ink, box.W-boardSide-x-14, fade)
}

// dim reports whether n is a channel fader nothing holds, or the
// chase's speed while the beat sets it, drawn dim.
func (b *boardPage) dim(n gunim.Node) bool {
	if n == b.speed {
		return b.played.Boards[b.board].Following
	}
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

// Handle implements [gunim.Handler]: a scene's pad pressed presses it
// on the Board, which adds it to the chase being made, records onto it
// where Record waits, or turns its scene on or off; a chase pressed
// runs or stops; a light's pad picks its light, or its group's, alone,
// and with Ctrl, adds them, or takes them out.
func (b *boardPage) Handle(e input.Event, u *gunim.UI) bool {
	d, ok := e.(input.PointerDown)
	if !ok || d.Button != input.ButtonPrimary || b.board == "" {
		return false
	}
	if b.menuOpen {
		// A pad's menu was open, and this press closed it, as it took
		// the focus: it does no more than that.
		b.menuOpen = false
		if b.menu.Focusable() {
			b.menu.Toggle(d.Pos, u)
		}
		return true
	}
	if i := b.sceneAt(d.Pos); i >= 0 {
		u.Send(b, app.PressPad{Module: b.board, Pad: i})
		return true
	}
	if i := b.chaseAt(d.Pos); i >= 0 {
		u.Send(b, app.PressChase{Module: b.board, Chase: b.chases[i].id})
		return true
	}
	for _, pd := range b.pads {
		if pd.r.Contains(d.Pos) {
			u.Send(b, app.Pick{Module: b.board, Lights: pd.lights, Toggle: d.Mods.Has(input.ModControl)})
			return true
		}
	}
	return false
}

// CatchKey implements [gunim.KeyCatcher]: Escape, while a chase is
// made, gives it up, and while Record waits, gives that up.
func (b *boardPage) CatchKey(e input.Event, u *gunim.UI) bool {
	k, ok := e.(input.KeyPress)
	if !ok || k.Key != input.KeyEscape || b.board == "" {
		return false
	}
	switch st := b.played.Boards[b.board]; {
	case st.Making:
		u.Send(b, app.GiveUpChase{Module: b.board})
	case st.Record:
		u.Send(b, app.GiveUpRecord{Module: b.board})
	default:
		return false
	}
	return true
}

// stepPad is the pad of the step the chase that runs is at, -1 for
// none.
func (b *boardPage) stepPad(st *engine.BoardState) int {
	i := slices.IndexFunc(b.chases, func(c chasePad) bool { return c.id == st.Chase })
	if st.Chase == 0 || i < 0 || st.Step < 0 || st.Step >= len(b.chases[i].steps) {
		return -1
	}
	return b.chases[i].steps[st.Step]
}

// countOf is n things, as "1 light" or "3 lights".
func countOf(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// pageSize is how many channel faders, and how many pads, a page of
// the controller that plays the Board holds.
const pageSize = 8

// held reports whether the controller that plays the Board, as st
// says, holds channel fader, or pad, i, from 0: one of the eight of the
// page it is on.
func (b *boardPage) held(st *engine.BoardState, i int) bool {
	return st.Remote != "" && i/pageSize == st.Page
}

// showRemotes follows the controllers that can play the Board: the
// picker offers None and them, by name, and picks the one the Board
// is played from; Add an X-Touch Mini is offered while the show has no
// controller of its layout.
func (b *boardPage) showRemotes(s *show.Show, u *gunim.UI) {
	b.remotes = []string{""}
	names := []string{"None"}
	b.hasTouch = false
	for i := range s.Modules {
		m := &s.Modules[i]
		if m.Kind != show.Controller {
			continue
		}
		b.hasTouch = b.hasTouch || m.Layout == controller.XTouchMiniID
		if controller.PlaysBoard(m) {
			b.remotes = append(b.remotes, m.ID)
			names = append(names, m.Name)
		}
	}
	if !slices.Equal(names, b.names) {
		// Not set afresh as it stands, so an open list stays open.
		b.names = names
		b.from.SetItems(widget.Labels(names...))
	}
	at := 0
	if m := s.Module(b.board); m != nil {
		at = max(slices.Index(b.remotes, m.PlayedFrom), 0)
	}
	b.from.SetSelected(at, u)
}

// The controller's row's measures: the words before the picker, and
// the picker at its widest.
const (
	fromW   = 84
	remoteW = 200
)

// layRemote lays out, from y down, full wide, the row of the controller
// the Board is played from: the picker, after its words, and Add an
// X-Touch Mini. It returns where the row ends.
func (b *boardPage) layRemote(y, full float32, lay func(gunim.Node, gunim.Constraints) geom.Size) float32 {
	b.fromY = y
	x := float32(boardSide + fromW)
	fs := lay(b.from, gunim.Constraints{Max: geom.Sz(min(remoteW, max(full-x, 80)), buttonH)})
	b.placed[b.from] = geom.Rc(x, y-4+(buttonH-fs.H)/2, fs.W, fs.H)
	x += fs.W + 12
	if !b.hasTouch {
		as := lay(b.addTouch, gunim.Constraints{Max: geom.Sz(max(full-x, 80), buttonH)})
		b.placed[b.addTouch] = geom.Rc(x, y-4, as.W, as.H)
	}
	return y + buttonH + 6
}

// paintRemote paints the words before the controller picker, and, while
// a controller plays the Board, the page it is on, in the colour of the
// marks on the faders and pads it holds, and how its port stands.
func (b *boardPage) paintRemote(p *paint.Painter, f gunim.Frame, box geom.Size, st *engine.BoardState) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	fade := widget.TextFade.Get(th)
	h := face.Shape("Played from", 12)
	h.Paint(p, geom.Pt(boardSide, b.fromY+4), quiet)
	if st.Remote == "" {
		return
	}
	x := b.placed[b.from].Max.X + 12
	if !b.hasTouch {
		x = b.placed[b.addTouch].Max.X + 12
	}
	first := st.Page*pageSize + 1
	t := bold.Shape("Page "+strconv.Itoa(st.Page+1)+" · "+strconv.Itoa(first)+"–"+strconv.Itoa(first+pageSize-1), 12)
	room := max(box.W-boardSide-x, 0)
	t.PaintFaded(p, geom.Pt(x, b.fromY+4), midiInk.Get(th), room, fade)
	says := b.played.Desks[st.Remote].Says
	if m := b.st.Show.Module(st.Remote); m != nil && m.Device == "" {
		says = "No MIDI port: pick it on the Controllers tab"
	}
	if x += t.Advance + 10; says != "" && x < box.W-boardSide {
		s := face.Shape(says, 11)
		s.PaintFaded(p, geom.Pt(x, b.fromY+5), quiet, box.W-boardSide-x, fade)
	}
}
