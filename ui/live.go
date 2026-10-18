package ui

import (
	"image/color"
	"strconv"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// livePage is the Live tab, the page to play the show from: each
// mixer's grand master, with blackout and flash held down and hold
// latched; a pad for each look, which brings it up and down; GO and
// Back for each cue list, Next for each chaser; and a pad for each
// button and toggle. Each pad lights as what it plays is on.
type livePage struct {
	st      *app.State
	played  engine.Live
	masters map[string]*widget.Slider
	ids     []string
	pads    []pad
	// down is the pad pressed, -1 for none, which hears its letting go.
	down int
	size geom.Size
}

// A pad is one of the page's pads: what it says, what kind of module it
// plays, its place, and what it asks for as it is pressed and let go.
type pad struct {
	label, sub string
	kind       show.Kind
	module     string
	r          geom.Rect
	lit        bool
	press      gunim.Intent
	letGo      gunim.Intent
}

// liveTab is the Live tab: its page, scrolled.
type liveTab struct {
	page   *livePage
	scroll *widget.Scroll
}

func newLiveTab() *liveTab {
	p := &livePage{masters: map[string]*widget.Slider{}, down: -1}
	return &liveTab{page: p, scroll: widget.NewScroll(p)}
}

func (t *liveTab) show(st *app.State, u *gunim.UI) { t.page.show(st, u) }
func (t *liveTab) live(l engine.Live, u *gunim.UI) { t.page.live(l, u) }

// Children implements [gunim.Composite].
func (t *liveTab) Children() []gunim.Node { return []gunim.Node{t.scroll} }

// Layout implements [gunim.Node].
func (t *liveTab) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (t *liveTab) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}

func (l *livePage) show(st *app.State, u *gunim.UI) {
	l.st = st
	var ids []string
	for i := range st.Show.Modules {
		m := &st.Show.Modules[i]
		if m.Kind != show.Mixer {
			continue
		}
		ids = append(ids, m.ID)
		s := l.masters[m.ID]
		if s == nil {
			id := m.ID
			s = widget.NewSlider(0, 1)
			s.OnChange = func(v float32, _ *gunim.UI) gunim.Intent { return app.SetValue{Module: id, Value: float64(v)} }
			s.OnCommit = func(v float32, _ *gunim.UI) gunim.Intent {
				return app.SetValue{Module: id, Value: float64(v), Done: true}
			}
			l.masters[m.ID] = s
			if u != nil {
				u.Insert(l, s)
			}
		}
		if !s.Held() {
			s.SetValue(float32(m.Value), u)
		}
	}
	for id, s := range l.masters {
		if st.Show.Module(id) == nil {
			if u != nil {
				u.Remove(s)
			}
			delete(l.masters, id)
		}
	}
	l.ids = ids
	l.down = -1
	u.Invalidate()
}

func (l *livePage) live(lv engine.Live, u *gunim.UI) {
	l.played = lv
	for id, s := range l.masters {
		if v, ok := lv.Values[id]; ok && !s.Held() && float64(s.Value()) != v {
			s.SetValue(float32(v), u)
		}
	}
	u.Invalidate()
}

// Children implements [gunim.Composite]: the masters.
func (l *livePage) Children() []gunim.Node {
	out := make([]gunim.Node, 0, len(l.ids))
	for _, id := range l.ids {
		out = append(out, l.masters[id])
	}
	return out
}

// The page's measures.
const (
	padW     = 150
	padH     = 84
	padGap   = 10
	liveHead = 30
	liveSide = 16
)

// Layout implements [gunim.Node]: the pads, a section a kind, each in
// rows as wide as the page, and each mixer's master over its pads.
func (l *livePage) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	l.size = c.Max
	place, h := l.build(c.Max.W)
	for k := range kids.All {
		r, ok := place[k.Node()]
		if !ok {
			k.Layout(gunim.Tight(k.Size()))
			continue
		}
		k.Layout(gunim.Tight(r.Size()))
		k.Place(r.Min)
	}
	return geom.Sz(c.Max.W, h)
}

// build lays the pads out across width w, as what plays now lights
// them, and returns where each master stands and how tall it all is.
func (l *livePage) build(w float32) (place map[gunim.Node]geom.Rect, height float32) {
	l.pads = l.pads[:0]
	place = map[gunim.Node]geom.Rect{}
	if l.st == nil {
		return place, 0
	}
	cols := max(int((w-2*liveSide+padGap)/(padW+padGap)), 1)
	y := float32(liveSide)
	section := func(ps []pad) {
		if len(ps) == 0 {
			return
		}
		y += liveHead
		for i := range ps {
			col, row := i%cols, i/cols
			ps[i].r = geom.Rc(liveSide+float32(col)*(padW+padGap), y+float32(row)*(padH+padGap), padW, padH)
		}
		y += float32((len(ps)+cols-1)/cols)*(padH+padGap) + padGap
		l.pads = append(l.pads, ps...)
	}
	s := &l.st.Show
	for _, id := range l.ids {
		ov := l.played.Overrides[id]
		y += liveHead
		place[l.masters[id]] = geom.Rc(liveSide, y, min(w-2*liveSide, 3*padW+2*padGap), 32)
		y += 32
		section([]pad{
			{label: "Blackout", sub: "hold down", kind: show.Mixer, module: id, lit: ov[0],
				press: app.Override{Module: id, Which: engine.Blackout, On: true}, letGo: app.Override{Module: id, Which: engine.Blackout}},
			{label: "Flash white", sub: "hold down", kind: show.Mixer, module: id, lit: ov[1],
				press: app.Override{Module: id, Which: engine.FlashAll, On: true}, letGo: app.Override{Module: id, Which: engine.FlashAll}},
			{label: "Hold", sub: "freezes what plays", kind: show.Mixer, module: id, lit: ov[2],
				press: app.Override{Module: id, Which: engine.HoldStill, On: !l.heldOn(id)}},
		})
	}
	var looks, steps, triggers []pad
	for i := range s.Modules {
		m := &s.Modules[i]
		v := l.played.Values[m.ID]
		switch m.Kind {
		case show.Look:
			on := v > 0
			level := 1.0
			if on {
				level = 0
			}
			looks = append(looks, pad{label: m.Name, sub: strconv.Itoa(int(v*100+0.5)) + "%", kind: m.Kind, module: m.ID, lit: on,
				press: app.SetValue{Module: m.ID, Value: level, Done: true}})
		case show.Cues:
			at := l.stepText(m.ID)
			steps = append(steps,
				pad{label: "GO", sub: m.Name + " · " + at, kind: m.Kind, module: m.ID, press: app.Step{Module: m.ID, By: 1}},
				pad{label: "Back", sub: m.Name, kind: m.Kind, module: m.ID, press: app.Step{Module: m.ID, By: -1}})
		case show.Chaser:
			steps = append(steps, pad{label: "Next", sub: m.Name + " · " + l.stepText(m.ID), kind: m.Kind, module: m.ID,
				press: app.Step{Module: m.ID, By: 1}})
		case show.Button:
			triggers = append(triggers, pad{label: m.Name, sub: "press", kind: m.Kind, module: m.ID, lit: v > 0,
				press: app.Press{Module: m.ID}})
		case show.Timer:
			st := l.played.Timers[m.ID]
			label, cmd := "Start", engine.TimerStart
			if st.Running {
				label, cmd = "Pause", engine.TimerPause
			}
			triggers = append(triggers,
				pad{label: label, sub: m.Name + " · " + timeText(st.Shown), kind: m.Kind, module: m.ID, lit: st.Running,
					press: app.TimerDo{Module: m.ID, Command: cmd}},
				pad{label: "Reset", sub: m.Name, kind: m.Kind, module: m.ID, press: app.TimerDo{Module: m.ID, Command: engine.TimerReset}})
		case show.Effect:
			looks = append(looks, pad{label: m.Name, sub: "fire", kind: m.Kind, module: m.ID, lit: v > 0,
				press: app.FireEffect{Module: m.ID}})
		case show.Pattern:
			looks = append(looks, pad{label: m.Name, sub: "hold to play", kind: m.Kind, module: m.ID, lit: v > 0,
				press: app.Override{Module: m.ID, Which: "play", On: true}, letGo: app.Override{Module: m.ID, Which: "play"}})
		case show.Toggle:
			sub := "off"
			if v > 0 {
				sub = "on"
			}
			triggers = append(triggers, pad{label: m.Name, sub: sub, kind: m.Kind, module: m.ID, lit: v > 0,
				press: app.Flip{Module: m.ID}})
		default:
		}
	}
	section(looks)
	section(steps)
	section(triggers)
	return place, y
}

// heldOn reports whether Mixer id holds.
func (l *livePage) heldOn(id string) bool { return l.played.Overrides[id][2] }

// stepText is where Chaser or Cue list id is, as its pad says it.
func (l *livePage) stepText(id string) string {
	st, ok := l.played.Steps[id]
	switch {
	case !ok || len(st.Steps) == 0:
		return "nothing wired in"
	case st.At < 0:
		return "ready"
	}
	return strconv.Itoa(st.At+1) + " of " + strconv.Itoa(len(st.Steps))
}

// Paint implements [gunim.Node]: the sections' heads, and the pads, lit
// in their kind's colour as what they play is on.
func (l *livePage) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	if l.st == nil {
		return
	}
	l.build(box.W)
	if len(l.pads) == 0 && len(l.ids) == 0 {
		msg := face.Shape("Nothing to play yet. Add looks, cue lists, buttons or a mixer on the Routes tab.", 12)
		msg.Paint(p, geom.Pt(liveSide+4, liveSide+8), quiet)
	}
	var lastKind show.Kind
	for i, pd := range l.pads {
		group := padGroup(pd.kind)
		if i == 0 || group != padGroup(lastKind) {
			h := bold.Shape(group, 12)
			h.Paint(p, geom.Pt(pd.r.Min.X, pd.r.Min.Y-liveHead+8), quiet)
		}
		lastKind = pd.kind
		accent := kindInk(th, pd.kind)
		fill := faded(ink, 0.06)
		if pd.lit {
			fill = faded(accent, 0.75)
		}
		if i == l.down {
			fill = faded(accent, 0.5)
		}
		p.RRect(pd.r, 12, paint.Solid(fill))
		p.RRect(geom.Rc(pd.r.Min.X, pd.r.Min.Y, 4, pd.r.Size().H), 2, paint.Solid(accent))
		text := ink
		if pd.lit {
			text = color.NRGBA{R: 0x10, G: 0x10, B: 0x14, A: 0xff}
		}
		t := bold.Shape(pd.label, 16)
		t.PaintFaded(p, geom.Pt(pd.r.Min.X+14, pd.r.Min.Y+18), text, padW-24, widget.TextFade.Get(th))
		s := face.Shape(pd.sub, 11)
		s.PaintFaded(p, geom.Pt(pd.r.Min.X+14, pd.r.Min.Y+46), faded(text, 0.7), padW-24, widget.TextFade.Get(th))
	}
	for _, id := range l.ids {
		if m := l.st.Show.Module(id); m != nil {
			if k, ok := l.masterRect(id); ok {
				h := bold.Shape(m.Name+" · grand master "+strconv.Itoa(int(m.Value*100+0.5))+"%", 12)
				h.Paint(p, geom.Pt(k.Min.X, k.Min.Y-22), quiet)
			}
		}
	}
	for k := range kids.All {
		k.Paint(p)
	}
}

// masterRect is where Mixer id's master stands: over its first pad.
func (l *livePage) masterRect(id string) (geom.Rect, bool) {
	for _, pd := range l.pads {
		if pd.module == id {
			return geom.Rc(pd.r.Min.X, pd.r.Min.Y-liveHead-32, padW, 32), true
		}
	}
	return geom.Rect{}, false
}

// padGroup is the section a pad of kind k stands in.
func padGroup(k show.Kind) string {
	switch k {
	case show.Mixer:
		return "Overrides"
	case show.Look, show.Pattern, show.Effect:
		return "Looks, patterns and effects"
	case show.Cues, show.Chaser:
		return "Cues and chasers"
	default:
	}
	return "Buttons, toggles and timers"
}

// Handle implements [gunim.Handler]: a pad pressed asks for what it
// plays, and one held down for as long as it is held, let go.
func (l *livePage) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		for i, pd := range l.pads {
			if pd.r.Contains(e.Pos) {
				l.down = i
				if pd.press != nil {
					u.Send(l, pd.press)
				}
				u.Invalidate()
				return true
			}
		}
	case input.PointerUp:
		if l.down < 0 {
			return false
		}
		if l.down < len(l.pads) && l.pads[l.down].letGo != nil {
			u.Send(l, l.pads[l.down].letGo)
		}
		l.down = -1
		u.Invalidate()
		return true
	}
	return false
}
