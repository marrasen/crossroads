package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// playlist is the Player tab: the transport and the song playing over
// the playlist, each song with the tempo it plays its lights at, how
// much louder it plays, and a button that takes it out. A song
// double-clicked plays; the bar under the song playing moves it where
// it is clicked.
type playlist struct {
	st     *app.State
	played engine.Playing
	add    *widget.Button
	// back, play, stop and next are the transport.
	back, play, stop, next *widget.IconButton
	list                   *songList
	scroll                 *widget.Scroll
	size                   geom.Size
}

// songList is the playlist's songs, a row each.
type songList struct {
	p    *playlist
	ids  []string
	rows map[string]*songRow
}

// songRow is a song's row: its tempo, its gain, and its remove button.
type songRow struct {
	bpm, gain *widget.NumberField
	remove    *widget.IconButton
}

func newPlaylist() *playlist {
	p := &playlist{add: widget.NewButton("Add songs")}
	p.add.Icon = icon.ListMusic
	p.add.OnClick = widget.Sends(app.AddSongs{})
	button := func(ic *icon.Icon, tip string, c engine.Command) *widget.IconButton {
		b := widget.NewIconButton(ic, tip)
		b.OnClick = widget.Sends(app.Transport{Command: c})
		return b
	}
	p.back = button(icon.SkipBack, "The song before", engine.Back)
	p.play = button(icon.Play, "Play or pause", engine.Toggle)
	p.stop = button(icon.Square, "Stop", engine.Stop)
	p.next = button(icon.SkipForward, "The next song", engine.Next)
	p.list = &songList{p: p, rows: map[string]*songRow{}}
	p.scroll = widget.NewScroll(p.list)
	return p
}

func (p *playlist) show(st *app.State, u *gunim.UI) {
	p.st = st
	p.list.show(st, u)
	u.Invalidate()
}

func (p *playlist) live(l engine.Live, u *gunim.UI) {
	if l.Playing == p.played {
		return
	}
	was := p.played
	p.played = l.Playing
	if was.Playing != l.Playing.Playing {
		p.play.Icon = icon.Play
		if l.Playing.Playing {
			p.play.Icon = icon.Pause
		}
	}
	u.Invalidate()
	if u != nil && was.Song != l.Playing.Song {
		p.list.reveal(l.Playing.Song, u)
	}
}

// Children implements [gunim.Composite]: the transport, the add button
// and the songs.
func (p *playlist) Children() []gunim.Node {
	return []gunim.Node{p.back, p.play, p.stop, p.next, p.add, p.scroll}
}

// The playlist's measures: how tall the transport is, where its song
// bar runs, and how tall a song's row is.
const (
	headH    = 76
	barY     = 52
	songBarH = 6
	songH    = 52
	fieldW   = 112
)

// Layout implements [gunim.Node]: the transport along the top, the add
// button at its right, the songs under them.
func (p *playlist) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	p.size = size
	x := float32(16)
	for i := range 4 {
		k := kids.At(i)
		s := k.Layout(gunim.Constraints{Max: geom.Sz(36, 36)})
		k.Place(geom.Pt(x, 12))
		x += s.W + 6
	}
	add := kids.At(4)
	as := add.Layout(gunim.Constraints{Max: geom.Sz(200, 36)})
	add.Place(geom.Pt(size.W-16-as.W, 12))
	sc := kids.At(5)
	sc.Layout(gunim.Tight(geom.Sz(size.W, max(size.H-headH, 0))))
	sc.Place(geom.Pt(0, headH))
	return size
}

// bar is where the song bar runs.
func (p *playlist) bar() geom.Rect {
	return geom.Rc(16, barY, max(p.size.W-32, 0), songBarH)
}

// Paint implements [gunim.Node].
func (p *playlist) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	pt.RRect(geom.Rect{Max: box.Point()}, 12, paint.Solid(widget.CardFill.Get(th)))
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	accent := songInk.Get(th)
	title, sub := "Nothing chosen", ""
	if p.st != nil {
		if g := p.st.Show.Song(p.played.Song); g != nil {
			title = g.Title
			sub = clock(p.played.Pos) + " / " + clock(p.played.Len)
		}
	}
	if p.played.Err != "" {
		sub = p.played.Err
	}
	x := float32(16 + 4*42 + 12)
	room := max(box.W-x-16-200, 40)
	t := bold.Shape(title, 14)
	t.PaintFaded(pt, geom.Pt(x, 14), ink, room, widget.TextFade.Get(th))
	if sub != "" {
		c := quiet
		if p.played.Err != "" {
			c = widget.DialogDangerInk.Get(th)
		}
		s := face.Shape(sub, 11)
		s.PaintFaded(pt, geom.Pt(x, 33), c, room, widget.TextFade.Get(th))
	}
	b := p.bar()
	pt.RRect(b, songBarH/2, paint.Solid(faded(ink, 0.1)))
	if p.played.Len > 0 {
		at := min(max(float32(p.played.Pos)/float32(p.played.Len), 0), 1)
		done := b
		done.Max.X = b.Min.X + b.Size().W*at
		pt.RRect(done, songBarH/2, paint.Solid(accent))
	}
	for k := range kids.All {
		k.Paint(pt)
	}
}

// Handle implements [gunim.Handler]: a click on the song bar moves the
// song there.
func (p *playlist) Handle(e input.Event, u *gunim.UI) bool {
	d, ok := e.(input.PointerDown)
	if !ok || d.Button != input.ButtonPrimary || p.played.Len <= 0 {
		return false
	}
	b := p.bar()
	b.Min.Y -= 8
	b.Max.Y += 8
	if !b.Contains(d.Pos) {
		return false
	}
	at := min(max((d.Pos.X-b.Min.X)/b.Size().W, 0), 1)
	u.Send(p, app.SeekSong{At: time.Duration(float64(at) * float64(p.played.Len))})
	return true
}

// clock writes d as minutes and seconds.
func clock(d time.Duration) string {
	s := int(max(d, 0).Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// show follows the playlist: a row for each song, in its order.
func (l *songList) show(st *app.State, u *gunim.UI) {
	ids := make([]string, 0, len(st.Show.Songs))
	for i := range st.Show.Songs {
		g := &st.Show.Songs[i]
		ids = append(ids, g.ID)
		row := l.rows[g.ID]
		if row == nil {
			id := g.ID
			row = &songRow{bpm: widget.NewNumberField(0, app.MaxBPM), gain: widget.NewNumberField(-app.MaxGain, app.MaxGain),
				remove: widget.NewIconButton(icon.Trash2, "Take the song out")}
			row.bpm.Decimals, row.bpm.Suffix = 1, " BPM"
			row.gain.Decimals, row.gain.Suffix, row.gain.Increment = 1, " dB", 0.5
			row.bpm.OnCommit = func(v float64, _ *gunim.UI) gunim.Intent {
				// The tempo found, typed back, sets none of its own.
				if g := l.p.st.Show.Song(id); g != nil && g.Scan != nil && math.Abs(v-g.Scan.BPM) < 0.05 {
					v = 0
				}
				return app.SetSongBPM{Song: id, BPM: v}
			}
			row.gain.OnCommit = func(v float64, _ *gunim.UI) gunim.Intent { return app.SetSongGain{Song: id, Gain: v} }
			row.remove.OnClick = widget.Sends(app.RemoveSong{Song: id})
			l.rows[g.ID] = row
		}
		bpm := g.BPM
		if bpm <= 0 && g.Scan != nil {
			bpm = g.Scan.BPM
		}
		if u == nil || !u.HasFocus(row.bpm) {
			row.bpm.SetValue(bpm, u)
		}
		if u == nil || !u.HasFocus(row.gain) {
			row.gain.SetValue(g.Gain, u)
		}
	}
	if !slices.Equal(ids, l.ids) {
		for _, id := range l.ids {
			if !slices.Contains(ids, id) {
				if row := l.rows[id]; row != nil && u != nil {
					u.Remove(row.bpm)
					u.Remove(row.gain)
					u.Remove(row.remove)
				}
				delete(l.rows, id)
			}
		}
		for _, id := range ids {
			if !slices.Contains(l.ids, id) && u != nil {
				row := l.rows[id]
				u.Insert(l, row.bpm)
				u.Insert(l, row.gain)
				u.Insert(l, row.remove)
			}
		}
		l.ids = ids
	}
	u.Invalidate()
}

// reveal scrolls song id's row into view.
func (l *songList) reveal(id string, u *gunim.UI) {
	if i := slices.Index(l.ids, id); i >= 0 {
		l.p.scroll.Reveal(geom.Rc(0, float32(i)*songH, 1, songH), u)
	}
}

// Children implements [gunim.Composite].
func (l *songList) Children() []gunim.Node {
	out := make([]gunim.Node, 0, 3*len(l.ids))
	for _, id := range l.ids {
		r := l.rows[id]
		out = append(out, r.bpm, r.gain, r.remove)
	}
	return out
}

// rowOf is the row of the node n holds, and which of its parts it is:
// 0 the tempo, 1 the gain, 2 the remove button; -1 for none.
func (l *songList) rowOf(n gunim.Node) (row, part int) {
	for i, id := range l.ids {
		r := l.rows[id]
		switch n {
		case r.bpm:
			return i, 0
		case r.gain:
			return i, 1
		case r.remove:
			return i, 2
		}
	}
	return -1, -1
}

// Layout implements [gunim.Node]: a row for each song, its fields and
// button at its right.
func (l *songList) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	w := c.Max.W
	size := geom.Sz(w, max(float32(len(l.ids))*songH+8, songH))
	for k := range kids.All {
		i, part := l.rowOf(k.Node())
		if i < 0 {
			k.Layout(gunim.Tight(k.Size()))
			continue
		}
		y := float32(i) * songH
		switch part {
		case 0:
			s := k.Layout(gunim.Tight(geom.Sz(fieldW, 32)))
			k.Place(geom.Pt(w-16-36-12-fieldW-8-fieldW, y+(songH-s.H)/2))
		case 1:
			s := k.Layout(gunim.Tight(geom.Sz(fieldW, 32)))
			k.Place(geom.Pt(w-16-36-12-fieldW, y+(songH-s.H)/2))
		default:
			s := k.Layout(gunim.Constraints{Max: geom.Sz(36, 36)})
			k.Place(geom.Pt(w-16-s.W, y+(songH-s.H)/2))
		}
	}
	return size
}

// Paint implements [gunim.Node]: each song's number, title, and what
// its scan found, the song playing lit.
func (l *songList) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	ink := widget.Ink.Get(th)
	quiet := faded(ink, 0.55)
	face, bold := widget.Font.Get(th), widget.BoldFont.Get(th)
	st := l.p.st
	if st == nil {
		return
	}
	if len(st.Show.Songs) == 0 {
		msg := face.Shape("No songs yet. Add some with Add songs.", 12)
		msg.Paint(p, geom.Pt(20, 16), quiet)
		return
	}
	room := max(box.W-20-2*fieldW-100, 40)
	for i := range st.Show.Songs {
		g := &st.Show.Songs[i]
		y := float32(i) * songH
		r := geom.Rc(8, y+2, box.W-16, songH-4)
		switch {
		case g.ID == l.p.played.Song:
			p.RRect(r, 8, paint.Solid(faded(songInk.Get(th), 0.18)))
		case i%2 == 0:
			p.RRect(r, 8, paint.Solid(faded(ink, 0.03)))
		}
		n := face.Shape(strconv.Itoa(i+1), 12)
		n.Paint(p, geom.Pt(20, y+18), quiet)
		t := bold.Shape(g.Title, 13)
		t.PaintFaded(p, geom.Pt(48, y+9), ink, room, widget.TextFade.Get(th))
		c, sub := quiet, songNote(g, st.Songs[g.ID])
		if note := st.Songs[g.ID]; note != "" && note != app.Scanning {
			c = widget.DialogDangerInk.Get(th)
		}
		s := face.Shape(sub, 11)
		s.PaintFaded(p, geom.Pt(48, y+28), c, room, widget.TextFade.Get(th))
	}
	for k := range kids.All {
		k.Paint(p)
	}
}

// Handle implements [gunim.Handler]: a song double-clicked plays.
func (l *songList) Handle(e input.Event, u *gunim.UI) bool {
	d, ok := e.(input.PointerDown)
	if !ok || d.Button != input.ButtonPrimary || d.Clicks != 2 {
		return false
	}
	i := int(d.Pos.Y / songH)
	if i < 0 || i >= len(l.ids) {
		return false
	}
	u.Send(l, app.PlaySong{Song: l.ids[i]})
	return true
}

// songNote is what a song's row says under its title: how long it is,
// the tempo found, and how loud it is and plays; or what is wrong.
func songNote(g *show.Song, note string) string {
	if note != "" {
		return note
	}
	sc := g.Scan
	if sc == nil {
		return "Not scanned"
	}
	out := clock(time.Duration(sc.Length * float64(time.Second)))
	if sc.BPM > 0 {
		out += fmt.Sprintf(" · %.1f BPM found", sc.BPM)
	} else {
		out += " · no beat found"
	}
	if sc.Loud {
		out += fmt.Sprintf(" · %.1f LUFS, plays %+.1f dB", sc.LUFS, g.Loudness())
	}
	return out
}
