// Package player plays crossroads' playlist: it scans each song once
// for its beats and how loud it is, plays the songs through the
// speakers, each brought to one loudness, and hands on the sound it
// plays, for the modules that listen.
package player

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/audio/beat"
)

// Kinds are the file name endings of the songs the player plays.
var Kinds = []string{".wav", ".mp3", ".flac", ".ogg"}

// Plays reports whether the player plays the file at path, by its
// name.
func Plays(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, k := range Kinds {
		if ext == k {
			return true
		}
	}
	return false
}

// TitleOf is the title a song at path is given: its file's name,
// without the ending.
func TitleOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Scan reads the song at path through, and finds how long it is, its
// beats and how loud it is.
func Scan(path string) (show.Scan, error) {
	f, err := os.Open(path)
	if err != nil {
		return show.Scan{}, err
	}
	defer func() { _ = f.Close() }()
	src, err := audio.Decode(f)
	if err != nil {
		return show.Scan{}, err
	}
	return ScanSource(src, audio.SampleRate)
}

// ScanSource scans the sound src gives, at rate, as [Scan] does a file.
func ScanSource(src audio.Source, rate int) (show.Scan, error) {
	beats := beat.NewScan(rate)
	loud := audio.NewLoudnessMeter(rate)
	var peak audio.TruePeakMeter
	buf := make([]float32, 2*8192)
	var frames int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			b := buf[:2*n]
			beats.Write(b)
			loud.Write(b)
			peak.Write(b)
			frames += int64(n)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return show.Scan{}, err
		}
		if n == 0 {
			return show.Scan{}, errors.New("the song gave no sound")
		}
	}
	r := beats.Done()
	sc := show.Scan{Length: float64(frames) / float64(rate), BPM: math.Round(r.BPM*10) / 10, Downbeat: r.Downbeat, PerBar: r.PerBar}
	if len(r.Beats) >= 2 {
		sc.Beats = make([]float64, len(r.Beats))
		for i, b := range r.Beats {
			// To the millisecond, which is all a light can show.
			sc.Beats[i] = math.Round(b.Seconds()*1000) / 1000
		}
	}
	sc.LUFS, sc.Loud = loud.Integrated()
	if !sc.Loud || math.IsInf(sc.LUFS, 0) || math.IsNaN(sc.LUFS) {
		sc.LUFS, sc.Loud = 0, false
	}
	sc.LUFS = math.Round(sc.LUFS*10) / 10
	sc.TruePeak = -120
	if p := peak.Peak(); p > 0 {
		sc.TruePeak = math.Round(20*math.Log10(p)*10) / 10
	}
	return sc, nil
}
