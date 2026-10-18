package player

import (
	"io"
	"math"
	"testing"
)

// clicks is a source of secs of a click every beat at bpm, as stereo
// at 48 kHz.
type clicks struct {
	bpm  float64
	n, i int
}

func newClicks(bpm, secs float64) *clicks { return &clicks{bpm: bpm, n: int(secs * 48000)} }

func (c *clicks) Read(dst []float32) (int, error) {
	per := 60 / c.bpm * 48000
	k := 0
	for ; k < len(dst)/2 && c.i < c.n; k++ {
		since := math.Mod(float64(c.i), per)
		v := float32(0.5 * math.Exp(-since/48000/0.03) * math.Sin(2*math.Pi*1500*since/48000))
		dst[2*k], dst[2*k+1] = v, v
		c.i++
	}
	if c.i >= c.n {
		return k, io.EOF
	}
	return k, nil
}

// TestAScanFindsTheBeatsAndTheLoudness of a click track.
func TestAScanFindsTheBeatsAndTheLoudness(t *testing.T) {
	sc, err := ScanSource(newClicks(124, 20), 48000)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(sc.Length-20) > 0.01 {
		t.Errorf("20 s scans as %v s", sc.Length)
	}
	if math.Abs(sc.BPM-124) > 0.5 || len(sc.Beats) < 35 {
		t.Errorf("a track at 124 scans at %v, with %d beats", sc.BPM, len(sc.Beats))
	}
	if !sc.Loud || sc.LUFS > -5 || sc.LUFS < -40 {
		t.Errorf("its loudness is %v LUFS, measured %v", sc.LUFS, sc.Loud)
	}
	if sc.TruePeak > -5 || sc.TruePeak < -7.5 {
		t.Errorf("a click at half scale peaks at %v dBTP, want about -6", sc.TruePeak)
	}
}

// TestTheRingHandsOnWhatWasPlayedOnce: each sample once, and no more
// than half the ring of what was missed.
func TestTheRingHandsOnWhatWasPlayedOnce(t *testing.T) {
	r := newRing(16)
	r.Process([]float32{1, 2, 3, 4})
	got, read := r.since(nil, 0)
	if len(got) != 4 || got[3] != 4 {
		t.Fatalf("first read %v", got)
	}
	if got, _ = r.since(nil, read); len(got) != 0 {
		t.Errorf("read again, %v", got)
	}
	for range 10 {
		r.Process([]float32{5, 6})
	}
	if got, _ = r.since(nil, read); len(got) != 8 {
		t.Errorf("after 20 samples missed, %d read, want the last 8", len(got))
	}
}

// TestThePlayerPlaysWhatItCanDecode, by name.
func TestThePlayerPlaysWhatItCanDecode(t *testing.T) {
	for p, want := range map[string]bool{"a/Song.MP3": true, "b.flac": true, "c.ogg": true, "d.wav": true, "e.aac": false, "f": false} {
		if Plays(p) != want {
			t.Errorf("Plays(%q) = %v", p, !want)
		}
	}
	if got := TitleOf("/music/Friday Mix.flac"); got != "Friday Mix" {
		t.Errorf("the title is %q", got)
	}
}
