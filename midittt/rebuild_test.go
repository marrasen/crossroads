package midittt

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio/midi"
)

// fakePorts is the MIDI devices a test plays: whatever is opened hears
// what the test sends it, and what is sent out is dropped.
type fakePorts struct {
	mu sync.Mutex
	in map[string]func(midi.Event)
}

type fakeIn struct{}

func (fakeIn) Err() error   { return nil }
func (fakeIn) Close() error { return nil }

type fakeOut struct{}

func (fakeOut) Send(time.Time, midi.Message) error { return nil }
func (fakeOut) Err() error                         { return nil }
func (fakeOut) Close() error                       { return nil }

func (p *fakePorts) OpenIn(name string, f func(midi.Event)) (engine.MIDIIn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.in[name] = f
	return fakeIn{}, nil
}

func (p *fakePorts) OpenOut(string) (engine.MIDIOut, error) { return fakeOut{}, nil }

// play sends m from device.
func (p *fakePorts) play(t *testing.T, device string, m midi.Message) {
	t.Helper()
	p.mu.Lock()
	f := p.in[device]
	p.mu.Unlock()
	if f == nil {
		t.Fatalf("%s is not open", device)
	}
	f(midi.Event{Time: time.Now(), Message: m})
}

// TestARigIsRebuiltAsAShow: midittt's own test rig comes over whole, and
// what the show does differently is said.
func TestARigIsRebuiltAsAShow(t *testing.T) {
	r := Rebuild(full(), "Gig")
	s := r.Show
	kinds := map[show.Kind]int{}
	for _, m := range s.Modules {
		kinds[m.Kind]++
	}
	for k, n := range map[show.Kind]int{show.Tempo: 1, show.Link: 1, show.ClockOut: 1, show.Routes: 1, show.Timer: 1,
		show.Mixer: 1, show.DMXOut: 1, show.StageView: 1} {
		if kinds[k] != n {
			t.Errorf("%d %s modules, want %d", kinds[k], k, n)
		}
	}
	if len(s.Rig.Lights) != 2 || len(s.Rig.Groups) != 1 || len(s.Rig.Types) != 1 || len(s.Programs) != 1 {
		t.Errorf("the rig is %+v, programmes %d", s.Rig, len(s.Programs))
	}
	ids := map[string]bool{}
	for _, m := range s.Modules {
		for _, rt := range m.Routes {
			if ids[rt.ID] {
				t.Errorf("two routes are %s", rt.ID)
			}
			ids[rt.ID] = true
		}
	}
	if s.Module("t1") == nil || s.Module("t1").Seconds != 600 {
		t.Error("the timer did not keep its ID and time")
	}
	for _, w := range s.Wires {
		from, okFrom := engine.PortOf(&s, w.From, true)
		to, okTo := engine.PortOf(&s, w.To, false)
		if !okFrom || !okTo || from.Signal != to.Signal {
			t.Errorf("wire %v → %v joins %v to %v", w.From, w.To, from, to)
		}
	}
	notes := strings.Join(r.Notes, "\n")
	for _, want := range []string{"start and stop", `"Drop → FX" fires on a song cue`} {
		if !strings.Contains(notes, want) {
			t.Errorf("the notes say nothing of %q:\n%s", want, notes)
		}
	}
}

// TestARebuiltRigPlays: its pad starts its timer, and its fader's
// route sets slot 12, through the mixer to the stage.
func TestARebuiltRigPlays(t *testing.T) {
	s := Rebuild(full(), "Gig").Show
	ports := &fakePorts{in: map[string]func(midi.Event){}}
	e := engine.New(nil)
	e.MIDI = ports
	s.Modules = slices.DeleteFunc(s.Modules, func(m show.Module) bool { return m.Kind == show.Link })
	defer e.Close()
	e.Set(s)
	for range 2000 {
		e.Tick()
		ports.mu.Lock()
		open := ports.in["XYZ"] != nil
		ports.mu.Unlock()
		if open {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ports.play(t, "XYZ", midi.NoteOn(0, 36, 100))
	ports.play(t, "XYZ", midi.Control(0, 7, 90))
	live := e.Tick()
	if !live.Timers["t1"].Running {
		t.Error("the pad did not start the timer")
	}
	if live.Stage[11] != 255 {
		t.Errorf("the fader's route set slot 12 to %d", live.Stage[11])
	}
}
