package ui

import (
	"slices"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// chaseControls are the Board page's controls for its chases: New
// chase, which reads Done while one is made; the chase's fade, speed
// and Tap; and Follow the beat, or, with no clock wired into the Board,
// the offer to listen to the sound card.
type chaseControls struct {
	newChase  *widget.Button
	chaseFade *widget.Slider
	speed     *widget.Slider
	tap       *holdButton
	follow    *widget.Switch
	listen    *widget.Button
}

// A chasePad is the button of one of a Board's chases: its ID, its
// name, its steps, by pad, and its place.
type chasePad struct {
	id    int
	name  string
	steps []int
	r     geom.Rect
}

// The chases' measures.
const (
	speedW = 180
	tapW   = 72
	tempoW = 92
)

// newChaseControls makes the page's chase controls.
func (b *boardPage) newChaseControls() {
	c := &b.chaseControls
	c.newChase = widget.NewButton("New chase")
	c.newChase.Icon = icon.ListPlus
	c.newChase.Tooltip = "Then press the pads in the order they should play, and Done. Right-click a chase to rename it, take out its last step, or delete it."
	c.newChase.OnClick = func(*gunim.UI) gunim.Intent {
		if b.played.Boards[b.board].Making {
			return app.DoneChase{Module: b.board}
		}
		return app.NewChase{Module: b.board}
	}
	c.chaseFade = widget.NewSlider(0, 1)
	c.chaseFade.Label, c.chaseFade.Snap = "Chase fade", 0.05
	c.chaseFade.OnChange = func(_ float32, u *gunim.UI) gunim.Intent {
		u.Invalidate()
		return nil
	}
	c.chaseFade.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
		return app.SetChaseFade{Module: b.board, Fade: float64(v)}
	}
	c.speed = widget.NewSlider(show.MinSpeed, show.MaxSpeed)
	c.speed.Label, c.speed.Snap = "Chase speed", 1
	c.speed.OnChange = func(_ float32, u *gunim.UI) gunim.Intent {
		u.Invalidate()
		return nil
	}
	c.speed.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
		return app.SetChaseSpeed{Module: b.board, Speed: float64(v)}
	}
	// A tap counts as it goes down, not as it comes up.
	c.tap = newHoldButton("Tap", func(down bool, _ *gunim.UI) gunim.Intent {
		if !down {
			return nil
		}
		return app.TapChase{Module: b.board}
	})
	c.tap.Tooltip = "Tap twice or more to set the speed. Each tap moves the chase on a step."
	c.follow = widget.NewSwitch("Follow the beat")
	c.follow.OnChange = func(on bool, _ *gunim.UI) gunim.Intent { return app.FollowBeat{Module: b.board, On: on} }
	c.listen = widget.NewButton("Listen to the sound card")
	c.listen.Icon = icon.Mic
	c.listen.Tooltip = "Adds an Audio in and a Beat, wired into the board, so the chase steps on the beat of the music"
	c.listen.OnClick = func(*gunim.UI) gunim.Intent { return app.ListenForBeat{Module: b.board} }
}

// showChases follows Board m's chases and their settings.
func (b *boardPage) showChases(s *show.Show, m *show.Module, u *gunim.UI) {
	b.chases = b.chases[:0]
	b.clocked = false
	if m == nil {
		return
	}
	for _, c := range m.Chases {
		b.chases = append(b.chases, chasePad{id: c.ID, name: c.Name, steps: slices.Clone(c.Steps)})
	}
	b.clocked = slices.ContainsFunc(s.Wires, func(w show.Wire) bool { return w.To.Module == m.ID && w.To.Port == "clock" })
	c := &b.chaseControls
	if !c.chaseFade.Held() {
		c.chaseFade.SetValue(float32(min(max(m.Fade, 0), 1)), u)
	}
	c.follow.SetChecked(m.FollowBeat, u)
}

// liveChases follows what the engine says of the chases: whether one
// is being made.
func (b *boardPage) liveChases(st *engine.BoardState, u *gunim.UI) {
	c := &b.chaseControls
	// The speed as the chase runs at it: the show's, or one tapped.
	if !c.speed.Held() && st.Speed > 0 && c.speed.Value() != float32(st.Speed) {
		c.speed.SetValue(float32(st.Speed), u)
	}
	c.newChase.Label, c.newChase.Icon, c.newChase.Kind = "New chase", icon.ListPlus, widget.ButtonPlain
	if st.Making {
		c.newChase.Label, c.newChase.Icon, c.newChase.Kind = "Done", icon.Check, widget.ButtonPrimary
	}
	// The pads are the chase's while it is made, not Record's.
	b.record.Disabled = st.Making
}

// chaseKids are the chase controls the page holds now.
func (b *boardPage) chaseKids() []gunim.Node {
	c := &b.chaseControls
	kids := []gunim.Node{c.chaseFade, c.newChase, c.speed, c.tap}
	if b.clocked {
		return append(kids, c.follow)
	}
	return append(kids, c.listen)
}

// layChases lays out the chases from y down, full wide: their head,
// with the fade and New chase at its right, as the scenes' has; a
// button for each chase, eight a row, as wide as the scenes' pads, and
// one for the chase being made; and the speed and Tap, with Follow the
// beat, or Listen to the sound card, at the right. It returns where the
// chases end.
func (b *boardPage) layChases(y, full, cw float32, making bool, lay func(gunim.Node, gunim.Constraints) geom.Size) float32 {
	c := &b.chaseControls
	b.chasesY = y
	ns := lay(c.newChase, gunim.Constraints{Max: geom.Sz(160, buttonH)})
	b.placed[c.newChase] = geom.Rc(full-ns.W, y-4, ns.W, ns.H)
	fs := lay(c.chaseFade, gunim.Constraints{Max: geom.Sz(fadeW, buttonH)})
	b.placed[c.chaseFade] = geom.Rc(full-ns.W-16-fadeW, y-4+(ns.H-fs.H)/2, fs.W, fs.H)
	y += buttonH + 2
	n := len(b.chases)
	if making {
		n++
	}
	for i := range b.chases {
		col, row := i%scenesRow, i/scenesRow
		b.chases[i].r = geom.Rc(boardSide+float32(col)*(cw+sceneGap), y+float32(row)*(sceneH+sceneGap), cw, sceneH)
	}
	b.making = geom.Rect{}
	if making {
		i := len(b.chases)
		col, row := i%scenesRow, i/scenesRow
		b.making = geom.Rc(boardSide+float32(col)*(cw+sceneGap), y+float32(row)*(sceneH+sceneGap), cw, sceneH)
	}
	y += float32((max(n, 1)+scenesRow-1)/scenesRow)*(sceneH+sceneGap) + 2
	b.tempoY = y
	// The speed's words stand left of its slider.
	x := float32(boardSide + 96)
	other := gunim.Node(c.listen)
	if b.clocked {
		other = c.follow
	}
	os := lay(other, gunim.Constraints{Max: geom.Sz(260, buttonH)})
	b.placed[other] = geom.Rc(full-os.W, y+(buttonH-os.H)/2, os.W, os.H)
	// The slider gives way in a narrow tab, so that Tap stands clear of
	// what is at the right: the switch and, before it, the tempo.
	right := full - os.W - 12
	if b.clocked {
		right -= tempoW + 12
	}
	sw := min(max(right-x-12-tapW, 48), speedW)
	ss := lay(c.speed, gunim.Constraints{Max: geom.Sz(sw, buttonH)})
	b.placed[c.speed] = geom.Rc(x, y+(buttonH-ss.H)/2, ss.W, ss.H)
	x += sw + 12
	ts := lay(c.tap, gunim.Constraints{Max: geom.Sz(tapW, buttonH), Min: geom.Sz(tapW, 0)})
	b.placed[c.tap] = geom.Rc(x, y, ts.W, ts.H)
	return y + buttonH + 8
}

// paintChases paints the chases' head, the fade and the speed, and a
// button for each chase: its name and how many steps it has, lit while
// it runs, with the step it is at and how far into it. While a chase
// is made, the head says so, and a button for it shows its steps so
// far. With the beat followed, it says the clock's tempo.
func (b *boardPage) paintChases(p *paint.Painter, f gunim.Frame, st *engine.BoardState) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	fade := widget.TextFade.Get(th)
	accent := widget.Accent.Get(th)
	c := &b.chaseControls
	fr := b.placed[c.chaseFade]
	ft := face.Shape("Fade "+strconv.Itoa(int(c.chaseFade.Value()*100+0.5))+"%", 12)
	fx := fr.Min.X - 8 - ft.Advance
	ft.Paint(p, geom.Pt(fx, b.chasesY+4), quiet)
	head, hc := "Chases", quiet
	if st.Making {
		head, hc = "New chase · press the pads in the order they play, then Done, or Esc", accent
	}
	h := bold.Shape(head, 12)
	h.PaintFaded(p, geom.Pt(boardSide, b.chasesY+4), hc, fx-boardSide-12, fade)
	for _, cp := range b.chases {
		r := cp.r
		running := st.Chase == cp.id
		fill := faded(ink, 0.07)
		if running {
			fill = faded(accent, 0.35)
		}
		p.RRect(r, 10, paint.Solid(fill))
		sub := countOf(len(cp.steps), "step")
		if running {
			p.RRect(geom.Rc(r.Min.X, r.Min.Y, 4, r.Size().H), 2, paint.Solid(accent))
			sub = "No scenes to play"
			if st.Step >= 0 {
				sub = "Step " + strconv.Itoa(st.Step+1) + " of " + strconv.Itoa(len(cp.steps))
				// How far into its step, along its foot.
				x, w := r.Min.X+10, r.Size().W-20
				p.RRect(geom.Rc(x, r.Max.Y-5, w, 2), 1, paint.Solid(faded(ink, 0.15)))
				p.RRect(geom.Rc(x, r.Max.Y-5, w*float32(st.Into), 2), 1, paint.Solid(accent))
			}
		} else if st.Last == cp.id && st.Chase == 0 {
			p.RRectStroke(r, 10, paint.Fill{}, paint.Stroke{Width: 1, Color: faded(accent, 0.5)})
		}
		t := bold.Shape(cp.name, 12)
		t.PaintFaded(p, geom.Pt(r.Min.X+10, r.Min.Y+6), ink, r.Size().W-18, fade)
		s := face.Shape(sub, 10)
		s.PaintFaded(p, geom.Pt(r.Min.X+10, r.Min.Y+22), quiet, r.Size().W-18, fade)
	}
	if st.Making {
		r := b.making
		p.RRectStroke(r, 10, paint.Solid(faded(accent, 0.08)), paint.Stroke{Width: 1.5, Color: accent})
		t := bold.Shape("New chase", 12)
		t.PaintFaded(p, geom.Pt(r.Min.X+10, r.Min.Y+6), ink, r.Size().W-18, fade)
		sub := "No steps yet"
		if n := len(st.Steps); n > 0 {
			sub = countOf(n, "step")
		}
		s := face.Shape(sub, 10)
		s.PaintFaded(p, geom.Pt(r.Min.X+10, r.Min.Y+22), accent, r.Size().W-18, fade)
	} else if len(b.chases) == 0 {
		msg := face.Shape("No chases yet. New chase, then the pads in the order they should play, makes one.", 12)
		msg.Paint(p, geom.Pt(boardSide, b.chasesY+buttonH+2+(sceneH-msg.Height())/2), faded(ink, 0.35))
	}
	// The speed, dim while the beat sets it, and the tempo followed.
	sr := b.placed[c.speed]
	sc := quiet
	if st.Following {
		sc = faded(ink, 0.3)
	}
	speed := strconv.Itoa(int(c.speed.Value() + 0.5))
	sw := face.Shape("Speed", 12)
	sw.Paint(p, geom.Pt(boardSide, b.tempoY+9), sc)
	sv := bold.Shape(speed+"/min", 12)
	sv.Paint(p, geom.Pt(sr.Min.X-8-sv.Advance, b.tempoY+9), sc)
	if !b.clocked {
		return
	}
	or := b.placed[c.follow]
	tempo, tc := "No beat yet", quiet
	if st.Clock.BPM > 0 {
		tempo = strconv.FormatFloat(st.Clock.BPM, 'f', 1, 64) + " BPM"
		if st.Following {
			tc = clockInk.Get(th)
		}
	}
	tt := face.Shape(tempo, 12)
	tt.PaintFaded(p, geom.Pt(or.Min.X-12-min(tt.Advance, tempoW), b.tempoY+9), tc, tempoW, fade)
}

// chaseAt is the place among the page's chases of the one at p, -1 for
// none.
func (b *boardPage) chaseAt(p geom.Point) int {
	for i := range b.chases {
		if b.chases[i].r.Contains(p) {
			return i
		}
	}
	return -1
}

// stepsOn is where pad comes in the chase being made, from 1, as the
// pad says it: "1·3" for a pad first and third.
func stepsOn(steps []int, pad int) string {
	var s string
	for i, p := range steps {
		if p != pad {
			continue
		}
		if s != "" {
			s += "·"
		}
		s += strconv.Itoa(i + 1)
	}
	return s
}
