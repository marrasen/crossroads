package show

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestAShowComesBackAsItWasSaved, and one written badly is mended.
func TestAShowComesBackAsItWasSaved(t *testing.T) {
	s := Start()
	path := filepath.Join(t.TempDir(), "Gig.crossroads")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Modules, s.Modules) || !reflect.DeepEqual(back.Wires, s.Wires) ||
		!reflect.DeepEqual(back.Rig, s.Rig) || !reflect.DeepEqual(back.Stage, s.Stage) || back.Next != s.Next {
		t.Errorf("came back different:\n%+v\nwant\n%+v", back, s)
	}
	if len(back.Layout.Items()) != len(Tabs) {
		t.Errorf("the layout came back with %v", back.Layout.Items())
	}
}

// TestRemovingTakesWhatHangsOnIt: a module's wires go with it, and a
// light leaves the stage and the modules that played it.
func TestRemovingTakesWhatHangsOnIt(t *testing.T) {
	s := Start()
	dome := s.Rig.Lights[0].ID
	var lights string
	for _, m := range s.Modules {
		if m.Kind == Lights {
			lights = m.ID
		}
	}
	s.RemoveModule(lights)
	for _, w := range s.Wires {
		if w.From.Module == lights || w.To.Module == lights {
			t.Fatalf("a wire of the module stayed: %+v", w)
		}
	}
	s.RemoveLight(dome)
	if s.Light(dome) != nil || len(s.Stage.Spots) != 0 {
		t.Error("the light stayed in the rig or on the stage")
	}
}
