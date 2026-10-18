package engine

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// fakeMIDI is a MIDI device a test plays: what it sends comes in, and
// what is sent to it is kept.
type fakeMIDI struct {
	mu    sync.Mutex
	f     func(midi.Event)
	sent  []midi.Message
	opens int
	down  bool
	gone  error
}

type fakeIn struct{ d *fakeMIDI }

func (i fakeIn) Err() error {
	i.d.mu.Lock()
	defer i.d.mu.Unlock()
	return i.d.gone
}
func (fakeIn) Close() error { return nil }

type fakeOut struct{ d *fakeMIDI }

func (o fakeOut) Send(_ time.Time, m midi.Message) error {
	o.d.mu.Lock()
	defer o.d.mu.Unlock()
	o.d.sent = append(o.d.sent, m)
	return nil
}
func (o fakeOut) Err() error { return fakeIn(o).Err() }
func (fakeOut) Close() error { return nil }

func (d *fakeMIDI) OpenIn(_ string, f func(midi.Event)) (MIDIIn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.down {
		return nil, errors.New("not plugged in")
	}
	d.f, d.gone = f, nil
	d.opens++
	return fakeIn{d}, nil
}

func (d *fakeMIDI) OpenOut(string) (MIDIOut, error) { return fakeOut{d}, nil }

// play sends m from the device.
func (d *fakeMIDI) play(m midi.Message) {
	d.mu.Lock()
	f := d.f
	d.mu.Unlock()
	f(midi.Event{Time: time.Now(), Message: m})
}

// took returns and forgets what was sent to the device.
func (d *fakeMIDI) took() []midi.Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.sent
	d.sent = nil
	return out
}

// midiRig is an X-Touch Mini, its first button lit by a Toggle it
// flips, on an engine whose clock a test moves.
type midiRig struct {
	s           show.Show
	e           *Engine
	d           *fakeMIDI
	now         time.Time
	ctl, toggle string
}

func newMIDIRig(t *testing.T) *midiRig {
	t.Helper()
	r := &midiRig{d: &fakeMIDI{}, now: time.Unix(1000, 0)}
	r.ctl, r.toggle = r.s.NewID("m"), r.s.NewID("m")
	r.s.Modules = []show.Module{
		{ID: r.ctl, Kind: show.Controller, Device: "X-TOUCH MINI", Layout: controller.XTouchMiniID},
		{ID: r.toggle, Kind: show.Toggle},
	}
	r.s.Wires = []show.Wire{
		{ID: "w1", From: show.End{Module: r.ctl, Port: "button1"}, To: show.End{Module: r.toggle, Port: "in"}},
		{ID: "w2", From: show.End{Module: r.toggle, Port: "out"}, To: show.End{Module: r.ctl, Port: "light:button1"}},
	}
	opened := make(chan struct{}, 4)
	r.e = New(func() { opened <- struct{}{} })
	r.e.MIDI = r.d
	r.e.Now = func() time.Time { return r.now }
	r.e.Set(r.s)
	r.e.Tick()
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the controller never opened")
	}
	t.Cleanup(r.e.Close)
	return r
}

// tick runs a tick a fortieth of a second on.
func (r *midiRig) tick() Live {
	r.now = r.now.Add(time.Second / 40)
	return r.e.Tick()
}

// TestAKnobTurnsItsValueAndLightsItsRing: relative steps move it, it
// stays within 0 and 1, and its ring is sent where it is.
func TestAKnobTurnsItsValueAndLightsItsRing(t *testing.T) {
	r := newMIDIRig(t)
	r.tick()
	r.d.took()
	for range 3 {
		r.d.play(midi.Control(10, 1, 5))
	}
	live := r.tick()
	if v := live.Desks[r.ctl].Values["knob1"]; v < 0.149 || v > 0.151 {
		t.Errorf("three turns of five steps put knob 1 at %v, want 0.15", v)
	}
	if v, _ := r.e.values[portKey{r.ctl, "knob1"}].(float64); v < 0.149 {
		t.Errorf("knob 1's output carries %v", v)
	}
	sent := r.d.took()
	if len(sent) != 1 || !same(sent[0], midi.Control(10, 1, 19)) {
		t.Errorf("knob 1's ring was sent %v, want one move to 19", sent)
	}
	r.d.play(midi.Control(10, 1, 100)) // 28 steps down
	if v := r.tick().Desks[r.ctl].Values["knob1"]; v != 0 {
		t.Errorf("turned down past its end, knob 1 is at %v", v)
	}
}

// TestAButtonFlipsAToggleThatLightsIt: pressed, it fires once, the
// Toggle turns on, and its light is sent on; pressed again, off.
func TestAButtonFlipsAToggleThatLightsIt(t *testing.T) {
	r := newMIDIRig(t)
	r.tick()
	r.d.took()
	r.d.play(midi.NoteOn(10, 8, 127))
	live := r.tick()
	if live.Values[r.toggle] != 1 {
		t.Fatalf("pressed, the toggle is at %v", live.Values[r.toggle])
	}
	// The toggle runs after the controller, so its light goes out at
	// the next tick.
	r.tick()
	if sent := r.d.took(); !contains(sent, midi.NoteOn(10, 8, 127)) {
		t.Errorf("the button's light was sent %v", sent)
	}
	r.d.play(midi.NoteOn(10, 8, 0))
	r.tick()
	if r.tick().Values[r.toggle] != 1 {
		t.Error("let go, the button flipped the toggle again")
	}
	r.d.play(midi.NoteOn(10, 8, 127))
	r.tick()
	r.tick()
	if sent := r.d.took(); !contains(sent, midi.NoteOn(10, 8, 0)) {
		t.Errorf("pressed again, the button's light was sent %v", sent)
	}
}

// same reports whether two messages without sysex are the same.
func same(a, b midi.Message) bool { return a.String() == b.String() }

func contains(ms []midi.Message, m midi.Message) bool {
	for _, x := range ms {
		if same(x, m) {
			return true
		}
	}
	return false
}

// TestTheFaderTakesOverOnlyOnceItPassesItsValue: after the controller
// comes back, the fader does nothing until it passes where its value
// stands.
func TestTheFaderTakesOverOnlyOnceItPassesItsValue(t *testing.T) {
	r := newMIDIRig(t)
	r.d.play(midi.Control(10, 9, 100))
	if v := r.tick().Desks[r.ctl].Values["fader"]; v < 0.78 || v > 0.79 {
		t.Fatalf("the fader first moved to 100 is at %v", v)
	}
	// Unplugged and plugged in again, the fader moved meanwhile.
	r.d.mu.Lock()
	r.d.gone = midi.ErrGone
	r.d.mu.Unlock()
	r.now = r.now.Add(3 * time.Second)
	r.tick()
	for range 200 {
		r.tick()
		r.d.mu.Lock()
		n := r.d.opens
		r.d.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.tick()
	r.d.play(midi.Control(10, 9, 20))
	r.d.play(midi.Control(10, 9, 40))
	if v := r.tick().Desks[r.ctl].Values["fader"]; v < 0.78 {
		t.Errorf("below where it stood, the fader moved the value to %v", v)
	}
	r.d.play(midi.Control(10, 9, 110))
	if v := r.tick().Desks[r.ctl].Values["fader"]; v < 0.86 || v > 0.87 {
		t.Errorf("past where it stood, the fader is at %v, want 110/127", v)
	}
}

// TestAControllerLearnsTheNextControlMoved, once.
func TestAControllerLearnsTheNextControlMoved(t *testing.T) {
	r := newMIDIRig(t)
	r.s.Module(r.ctl).Layout = ""
	r.e.Set(r.s)
	r.tick()
	r.e.Learn(r.ctl, true)
	if !r.tick().Desks[r.ctl].Learn {
		t.Error("the controller does not say it learns")
	}
	r.d.play(midi.NoteOn(3, 60, 100))
	r.d.play(midi.Control(3, 7, 64))
	r.tick()
	got := r.e.Learnt()
	if len(got) != 1 || got[0].Module != r.ctl || got[0].Control.Note != 60 || got[0].Control.Channel != 3 {
		t.Errorf("learned %+v", got)
	}
	if r.tick().Desks[r.ctl].Learn {
		t.Error("having learned one, it learns on")
	}
}

// TestAMIDIOutPlaysANoteForATriggerAndMovesForAValue: the note ends at
// the next tick, and the controller moves only where the value did.
func TestAMIDIOutPlaysANoteForATriggerAndMovesForAValue(t *testing.T) {
	d := &fakeMIDI{}
	var s show.Show
	btn, f, out := s.NewID("m"), s.NewID("m"), s.NewID("m")
	s.Modules = []show.Module{{ID: btn, Kind: show.Button}, {ID: f, Kind: show.Fader, Value: 0.5},
		{ID: out, Kind: show.MIDIOut, Device: "Synth", Channel: 2, Note: 64, CC: 20}}
	s.Wires = []show.Wire{{ID: "w1", From: show.End{Module: btn, Port: "out"}, To: show.End{Module: out, Port: "note"}},
		{ID: "w2", From: show.End{Module: f, Port: "out"}, To: show.End{Module: out, Port: "value"}}}
	opened := make(chan struct{}, 4)
	e := New(func() { opened <- struct{}{} })
	e.MIDI = d
	defer e.Close()
	e.Set(s)
	e.Tick()
	<-opened
	e.Tick()
	if sent := d.took(); len(sent) != 1 || !same(sent[0], midi.Control(2, 20, 64)) {
		t.Errorf("a fader at half sent %v", sent)
	}
	e.Press(btn)
	e.Tick()
	e.Tick()
	if sent := d.took(); len(sent) != 2 || !same(sent[0], midi.NoteOn(2, 64, 127)) || !same(sent[1], midi.NoteOff(2, 64, 0)) {
		t.Errorf("a button pressed sent %v", sent)
	}
}

// TestLogicMapsCountsAndToggles: a Map scales and turns over, and an
// Every fires as its clock passes each so many beats.
func TestLogicMapsCountsAndToggles(t *testing.T) {
	if got := MapValue(0.25, [2]float64{0, 0.5}, [2]float64{0.2, 0.4}, false); got < 0.2999 || got > 0.3001 {
		t.Errorf("0.25 of 0 to 0.5 into 0.2 to 0.4 is %v", got)
	}
	if got := MapValue(0.9, [2]float64{}, [2]float64{}, true); got < 0.0999 || got > 0.1001 {
		t.Errorf("0.9 turned over is %v", got)
	}
	var s show.Show
	tp, ev := s.NewID("m"), s.NewID("m")
	s.Modules = []show.Module{{ID: tp, Kind: show.Tempo, BPM: 120, Manual: true}, {ID: ev, Kind: show.Every, Every: 2}}
	s.Wires = []show.Wire{{ID: "w", From: show.End{Module: tp, Port: "out"}, To: show.End{Module: ev, Port: "in"}}}
	now := time.Unix(1000, 0)
	e := New(nil)
	e.Now = func() time.Time { return now }
	defer e.Close()
	e.Set(s)
	fired := 0
	for range 150 {
		e.Tick()
		if f, _ := e.values[portKey{ev, "out"}].(bool); f {
			fired++
		}
		now = now.Add(time.Second / 40)
	}
	// 150 ticks at 120 is seven and a half beats: on 0, 2, 4 and 6.
	if fired != 4 {
		t.Errorf("every 2 beats fired %d times in seven and a half beats", fired)
	}
}

// TestALightMayCloseALoop: a button into a Toggle into the button's own
// light joins, though a value back into the controller would not.
func TestALightMayCloseALoop(t *testing.T) {
	var s show.Show
	ctl, tg := s.NewID("m"), s.NewID("m")
	s.Modules = []show.Module{{ID: ctl, Kind: show.Controller, Layout: controller.XTouchMiniID}, {ID: tg, Kind: show.Toggle}}
	s.Wires = []show.Wire{{ID: "w", From: show.End{Module: ctl, Port: "button1"}, To: show.End{Module: tg, Port: "in"}}}
	if ok, why := CanJoin(&s, show.End{Module: tg, Port: "out"}, show.End{Module: ctl, Port: "light:button1"}); !ok {
		t.Errorf("a toggle may not light the button that flips it: %s", why)
	}
	if got := order(&s); got[0] != ctl {
		t.Errorf("the order is %v, the controller not first", got)
	}
}

// TestAPortThatWillNotOpenIsTriedAgainQuietly: the system's ports give
// no port in an interface where none opened, and the engine closes
// nothing it did not open.
func TestAPortThatWillNotOpenIsTriedAgainQuietly(t *testing.T) {
	in, err := systemMIDI{}.OpenIn("crossroads: no such port", func(midi.Event) {})
	if err == nil || in != nil {
		t.Fatalf("a port not there opened as %v, %v", in, err)
	}
	if out, err := (systemMIDI{}).OpenOut("crossroads: no such port"); err == nil || out != nil {
		t.Fatalf("a port not there opened as %v, %v", out, err)
	}
	p := &port{device: "crossroads: no such port", listen: true}
	p.open(systemMIDI{}, time.Unix(1000, 0), nil)
	for range 500 {
		p.mu.Lock()
		busy := p.busy
		p.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	p.open(systemMIDI{}, time.Unix(1010, 0), nil)
	p.close()
}
