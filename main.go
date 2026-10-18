// Command crossroads runs the lights and the music at a gig.
//
//	crossroads                        a new show: the Dome on a stage, played by faders
//	crossroads Gig.crossroads         the show in that file
//	crossroads -import rig.json       a midittt rig, rebuilt as a show
//	crossroads -check-midittt *.json  says, for each midittt rig, whether
//	                                  it comes over whole, and quits
//
// -shot writes the first window to a PNG file once it has drawn, and
// quits, for looking at the pixels.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/midittt"
	"github.com/marrasen/crossroads/player"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/crossroads/ui"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
)

func main() {
	shot := flag.String("shot", "", "write the first window to this PNG file after -after, and quit")
	after := flag.Duration("after", 2*time.Second, "how long -shot waits")
	rig := flag.String("import", "", "open this midittt rig, rebuilt as a show")
	check := flag.Bool("check-midittt", false, "say whether each midittt rig named comes over whole, and quit")
	flag.Parse()
	if *check {
		if !checkRigs(os.Stdout, flag.Args()) {
			os.Exit(1)
		}
		return
	}
	path := flag.Arg(0)
	if err := run(path, *rig, *shot, *after); err != nil {
		log.Fatal(err)
	}
}

// checkRigs says, for each midittt rig at paths, whether it comes over
// whole, and what plays differently where it does not; it reports false
// where one could not be read.
func checkRigs(w io.Writer, paths []string) bool {
	ok := true
	for _, p := range paths {
		c, err := midittt.Load(p)
		if err != nil {
			_, _ = fmt.Fprintf(w, "%s: could not be read: %v\n", p, err)
			ok = false
			continue
		}
		r := midittt.Rebuild(c, midittt.NameOf(p))
		if len(r.Notes) == 0 {
			_, _ = fmt.Fprintf(w, "%s: comes over whole: %d routes, %d lights\n", p, len(c.Routes), len(c.Fixtures))
			continue
		}
		_, _ = fmt.Fprintf(w, "%s: comes over, but for these:\n", p)
		for _, n := range r.Notes {
			_, _ = fmt.Fprintf(w, "  - %s\n", n)
		}
	}
	return ok
}

func run(path, rig, shot string, after time.Duration) error {
	s := show.Start()
	var notes []string
	switch {
	case rig != "":
		c, err := midittt.Load(rig)
		if err != nil {
			return err
		}
		r := midittt.Rebuild(c, midittt.NameOf(rig))
		s, notes, path = r.Show, r.Notes, ""
	case path != "":
		var err error
		if s, err = show.Load(path); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := gunim.Main(ctx, func(a *gunim.App) error {
		var first gunim.Client
		open := func(win int, parent *gunim.Window, at geom.Point, size geom.Size, place *driver.Placement) (gunim.Client, *gunim.Window, error) {
			o := gunim.WindowOptions{Title: "crossroads", Size: size, Parent: parent, Anchor: at, Place: place, ZoomKeys: true}
			w, err := a.NewWindow(o)
			if err != nil {
				return gunim.Client{}, nil, fmt.Errorf("crossroads: %w", err)
			}
			ui.Register(w, win)
			c := w.Client()
			if err := c.Mount(gunim.Root, ui.View, ui.View, app.State{}, app.ShowTopic, app.LiveTopic); err != nil {
				return gunim.Client{}, nil, err
			}
			if win == 0 {
				first = c
			}
			return c, w, nil
		}
		ap := app.NewApp(s, path, open)
		ap.UseDeck(player.NewDeck())
		if rig != "" {
			ap.Imported(s.Name, notes)
		}
		if shot != "" {
			go func() {
				select {
				case <-time.After(after):
				case <-ctx.Done():
					return
				}
				if err := writeShot(ctx, first, shot); err != nil {
					log.Print(err)
				}
				first.Close()
			}()
		}
		return ap.Run(ctx)
	})
	if errors.Is(err, driver.ErrNoDriver) {
		log.Print("gunim has no driver for this operating system yet")
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// writeShot writes what a window shows to a PNG file.
func writeShot(ctx context.Context, c gunim.Client, path string) error {
	img, err := c.Shot(ctx)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
