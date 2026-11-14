package engine

import (
	"errors"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/crossroads/controller"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// fakeMIDI is a MIDI device a test plays: what it sends comes in, and
// what is sent to it is kept. plugs counts its unplugs: a port opened
// before the last is gone.
type fakeMIDI struct {
	mu       sync.Mutex
	f        func(midi.Event)
	sent     []midi.Message
	opens    int
	outOpens int
	down     bool
	plugs    int
}

type fakeIn struct {
	d    *fakeMIDI
	plug int
}

func (i fakeIn) Err() error {
	i.d.mu.Lock()
	defer i.d.mu.Unlock()
	if i.plug != i.d.plugs {
		return midi.ErrGone
	}
	return nil
}
func (fakeIn) Close() error { return nil }

type fakeOut fakeIn

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
	d.f = f
	d.opens++
	return fakeIn{d, d.plugs}, nil
}

func (d *fakeMIDI) OpenOut(string) (MIDIOut, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.outOpens++
	return fakeOut{d, d.plugs}, nil
}

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
	r.e = New(nil)
	r.e.MIDI = r.d
	r.e.Now = func() time.Time { return r.now }
	r.e.Set(r.s)
	waitPorts(t, r.e)
	t.Cleanup(r.e.Close)
	return r
}

// tick runs a tick a fortieth of a second on.
func (r *midiRig) tick() Live {
	r.now = r.now.Add(time.Second / 40)
	return r.e.Tick()
}

// TestAKnobTurnedEitherWayMovesItsValueAndRing: in Mackie Control
// mode, 1 to 7 up and 65 to 71 down, each step a hundredth times the
// speed, within 0 and 1; its ring dark at 0, one LED for a little, all
// 11 at full.
func TestAKnobTurnedEitherWayMovesItsValueAndRing(t *testing.T) {
	r := newMIDIRig(t)
	r.tick()
	r.d.took()
	turn := func(v, times int, want float64, what string) {
		t.Helper()
		for range times {
			r.d.play(midi.Control(0, 16, v))
		}
		got := r.tick().Desks[r.ctl].Values["knob1"]
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s, knob 1 is at %v, want %v", what, got, want)
		}
	}
	ring := func(want int, what string) {
		t.Helper()
		if sent := r.d.took(); !sameBytes(sent, []midi.Message{midi.Control(0, 48, want)}) {
			t.Errorf("%s, ring 1 was sent %v, want %d", what, sent, want)
		}
	}
	turn(1, 1, 0.01, "turned up one step")
	ring(33, "a little up")
	turn(5, 3, 0.16, "turned up three times at speed 5")
	if v, _ := r.e.values[portKey{r.ctl, "knob1"}].(float64); math.Abs(v-0.16) > 1e-9 {
		t.Errorf("knob 1's output carries %v", v)
	}
	r.d.took()
	turn(65, 1, 0.15, "turned down one step")
	turn(70, 1, 0.09, "turned down at speed 6")
	r.d.took()
	turn(71, 3, 0, "turned down past its end")
	ring(32, "at 0")
	turn(7, 20, 1, "turned up past its end")
	ring(43, "full")
}

// TestAButtonFlipsAToggleThatLightsIt: pressed, it fires once, the
// Toggle turns on, and its light is sent on, a note on of its own note
// at 127; pressed again, off at 0.
func TestAButtonFlipsAToggleThatLightsIt(t *testing.T) {
	r := newMIDIRig(t)
	r.tick()
	r.d.took()
	r.d.play(midi.NoteOn(0, 89, 127))
	live := r.tick()
	if live.Values[r.toggle] != 1 {
		t.Fatalf("pressed, the toggle is at %v", live.Values[r.toggle])
	}
	// The toggle runs after the controller, so its light goes out at
	// the next tick.
	r.tick()
	if sent := r.d.took(); !slices.ContainsFunc(sent, sameAs(midi.NoteOn(0, 89, 127))) {
		t.Errorf("the button's light was sent %v", sent)
	}
	r.d.play(midi.NoteOn(0, 89, 0))
	r.tick()
	if r.tick().Values[r.toggle] != 1 {
		t.Error("let go, the button flipped the toggle again")
	}
	r.d.play(midi.NoteOn(0, 89, 127))
	r.tick()
	r.tick()
	if sent := r.d.took(); !slices.ContainsFunc(sent, sameAs(midi.NoteOn(0, 89, 0))) {
		t.Errorf("pressed again, the button's light was sent %v", sent)
	}
}

// fader is the X-Touch Mini's fader at v, 0 to 127: a pitch bend on
// channel 9, the first data byte 0.
func fader(v byte) midi.Message { return midi.Message{Status: 0xE8, Data2: v} }

// replug unplugs the rig's controller and plugs it in again, and waits
// until its input and output have opened again.
func (r *midiRig) replug(t *testing.T) {
	t.Helper()
	r.d.mu.Lock()
	r.d.plugs++
	ins, outs := r.d.opens, r.d.outOpens
	r.d.mu.Unlock()
	r.now = r.now.Add(3 * time.Second)
	for range 2000 {
		r.tick()
		r.d.mu.Lock()
		reopened := r.d.opens > ins && r.d.outOpens > outs
		r.d.mu.Unlock()
		busy := false
		for _, p := range []*port{r.e.hub.ins["X-TOUCH MINI"], r.e.hub.outs["X-TOUCH MINI"]} {
			p.mu.Lock()
			busy = busy || p.busy
			p.mu.Unlock()
		}
		if reopened && !busy {
			r.tick()
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the controller never opened again")
}

// TestTheXTouchMiniIsSwitchedToMCModeThenLitOnEveryConnect: CC 127 at
// 1 on all 16 channels, then every ring and button light, as it first
// connects and again as it comes back.
func TestTheXTouchMiniIsSwitchedToMCModeThenLitOnEveryConnect(t *testing.T) {
	r := newMIDIRig(t)
	want := func(ring1 int) []midi.Message {
		var w []midi.Message
		for ch := range 16 {
			w = append(w, midi.Message{Status: byte(0xB0 + ch), Data1: 127, Data2: 1})
		}
		w = append(w, midi.Control(0, 48, ring1))
		for cc := 49; cc <= 55; cc++ {
			w = append(w, midi.Control(0, cc, 32))
		}
		for _, n := range []int{89, 90, 40, 41, 42, 43, 44, 45, 87, 88, 91, 92, 86, 93, 94, 95, 84, 85} {
			w = append(w, midi.NoteOn(0, n, 0))
		}
		return w
	}
	r.tick()
	if sent := r.d.took(); !sameBytes(sent, want(32)) {
		t.Errorf("connected, the X-Touch Mini was sent\n%v\nwant\n%v", sent, want(32))
	}
	r.d.play(midi.Control(0, 16, 7))
	r.tick()
	r.d.took()
	r.replug(t)
	if sent := r.d.took(); !sameBytes(sent, want(33)) {
		t.Errorf("back again, the X-Touch Mini was sent\n%v\nwant\n%v", sent, want(33))
	}

	// A controller of learned controls is sent no switch.
	d := &fakeMIDI{}
	var s show.Show
	ctl := s.NewID("m")
	s.Modules = []show.Module{{ID: ctl, Kind: show.Controller, Device: "nanoKONTROL",
		Controls: []show.Control{{ID: "c1", Name: "Note 36", Kind: show.Press, CC: -1, Note: 36, Lit: true}}}}
	e := New(nil)
	e.MIDI = d
	defer e.Close()
	e.Set(s)
	waitPorts(t, e)
	e.Tick()
	if sent := d.took(); !sameBytes(sent, []midi.Message{midi.NoteOn(0, 36, 0)}) {
		t.Errorf("a controller of learned controls was sent %v", sent)
	}
}

// TestAnXTouchMiniInStandardModeSaysItWantsMCMode: a knob heard as CC
// 1 to 8 on channel 11 means the switch did not reach it, until it
// comes back and is switched again.
func TestAnXTouchMiniInStandardModeSaysItWantsMCMode(t *testing.T) {
	r := newMIDIRig(t)
	r.d.play(midi.Control(0, 16, 1))
	if r.tick().Desks[r.ctl].Standard {
		t.Error("in MC mode, the X-Touch Mini says it is in Standard mode")
	}
	r.d.play(midi.Control(10, 3, 64))
	if !r.tick().Desks[r.ctl].Standard {
		t.Error("a Standard mode knob heard, the X-Touch Mini does not say so")
	}
	r.replug(t)
	if r.tick().Desks[r.ctl].Standard {
		t.Error("back again and switched, it still says it is in Standard mode")
	}
}

// same reports whether two messages without sysex are the same.
func same(a, b midi.Message) bool { return a.String() == b.String() }

// sameAs says whether a message without sysex is m, byte for byte: a
// note on at 0 is not a note off.
func sameAs(m midi.Message) func(midi.Message) bool {
	return func(x midi.Message) bool { return x.Status == m.Status && x.Data1 == m.Data1 && x.Data2 == m.Data2 }
}

// sameBytes reports whether two lists of messages without sysex are the
// same bytes.
func sameBytes(a, b []midi.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameAs(b[i])(a[i]) {
			return false
		}
	}
	return true
}

func contains(ms []midi.Message, m midi.Message) bool {
	for _, x := range ms {
		if same(x, m) {
			return true
		}
	}
	return false
}

// TestTheFaderTakesOverOnlyOnceItPassesItsValue: it sends a pitch
// bend, its value in the second data byte; after the controller comes
// back, the fader does nothing until it passes where its value stands.
func TestTheFaderTakesOverOnlyOnceItPassesItsValue(t *testing.T) {
	r := newMIDIRig(t)
	r.d.play(fader(100))
	if v := r.tick().Desks[r.ctl].Values["fader"]; v < 0.78 || v > 0.79 {
		t.Fatalf("the fader first moved to 100 is at %v", v)
	}
	// Unplugged and plugged in again, the fader moved meanwhile.
	r.replug(t)
	r.d.play(fader(20))
	r.d.play(fader(40))
	if v := r.tick().Desks[r.ctl].Values["fader"]; v < 0.78 {
		t.Errorf("below where it stood, the fader moved the value to %v", v)
	}
	r.d.play(fader(110))
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
	e := New(nil)
	e.MIDI = d
	defer e.Close()
	e.Set(s)
	waitPorts(t, e)
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
	p := &port{device: "crossroads: no such port", in: true}
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

// waitPorts ticks e until every MIDI port it uses has tried to open,
// and none is still opening.
func waitPorts(t *testing.T, e *Engine) {
	t.Helper()
	for range 5000 {
		e.Tick()
		e.mu.Lock()
		done := true
		for _, ports := range []map[string]*port{e.hub.ins, e.hub.outs} {
			for _, p := range ports {
				p.mu.Lock()
				if p.busy || p.tried.IsZero() {
					done = false
				}
				p.mu.Unlock()
			}
		}
		e.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the MIDI ports never opened")
}

// TestALayoutPickedOnAnOpenPortIsSwitchedToMCMode: the switch goes out
// as the X-Touch Mini's layout is picked for a controller whose port
// stands open already, before its lights.
func TestALayoutPickedOnAnOpenPortIsSwitchedToMCMode(t *testing.T) {
	d := &fakeMIDI{}
	var s show.Show
	ctl := s.NewID("m")
	s.Modules = []show.Module{{ID: ctl, Kind: show.Controller, Device: "X-TOUCH MINI"}}
	e := New(nil)
	e.MIDI = d
	defer e.Close()
	e.Set(s)
	waitPorts(t, e)
	e.Tick()
	if sent := d.took(); len(sent) != 0 {
		t.Fatalf("a controller with no controls was sent %v", sent)
	}
	s.Modules[0].Layout = controller.XTouchMiniID
	e.Set(s)
	e.Tick()
	sent := d.took()
	if len(sent) < 17 {
		t.Fatalf("its layout picked, the X-Touch Mini was sent %v", sent)
	}
	for ch, m := range sent[:16] {
		if !same(m, midi.Message{Status: byte(0xB0 + ch), Data1: 127, Data2: 1}) {
			t.Fatalf("its layout picked, message %d sent is %v, not the switch", ch, m)
		}
	}
	if !same(sent[16], midi.Control(0, 48, 32)) {
		t.Errorf("after the switch came %v, not the first ring", sent[16])
	}
	e.Tick()
	if sent := d.took(); len(sent) != 0 {
		t.Errorf("switched once, it was sent %v again", sent)
	}
}

// TestTheStandardModeWarningGoesAsMCModeIsHeardOrTheLayoutLeft: one of
// Mackie Control mode's own messages says the controller switched, and
// a controller of learned controls wants no mode.
func TestTheStandardModeWarningGoesAsMCModeIsHeardOrTheLayoutLeft(t *testing.T) {
	r := newMIDIRig(t)
	r.tick()
	r.d.play(midi.Control(10, 3, 64))
	if !r.tick().Desks[r.ctl].Standard {
		t.Fatal("a Standard mode knob heard, the X-Touch Mini does not say so")
	}
	r.d.play(midi.Control(0, 16, 1))
	if r.tick().Desks[r.ctl].Standard {
		t.Error("a Mackie Control knob heard, it still says it is in Standard mode")
	}
	r.d.play(midi.Control(10, 3, 64))
	if !r.tick().Desks[r.ctl].Standard {
		t.Fatal("a Standard mode knob heard again, the X-Touch Mini does not say so")
	}
	r.s.Modules[0].Layout = ""
	r.e.Set(r.s)
	if r.tick().Desks[r.ctl].Standard {
		t.Error("its layout left, the controller still says it is in Standard mode")
	}
}

// TestAKnobTurnedUpAndBackDownEndsDark: steps up and the same steps
// down leave the value at 0 and the ring dark, not a hair above it.
func TestAKnobTurnedUpAndBackDownEndsDark(t *testing.T) {
	r := newMIDIRig(t)
	r.tick()
	r.d.took()
	for _, v := range []int{1, 5, 70} {
		r.d.play(midi.Control(0, 16, v))
	}
	if v := r.tick().Desks[r.ctl].Values["knob1"]; v != 0 {
		t.Errorf("up six steps and down six, the knob stands at %v", v)
	}
	sent := r.d.took()
	if len(sent) != 0 {
		t.Errorf("the ring, dark before and after, was sent %v", sent)
	}
}
