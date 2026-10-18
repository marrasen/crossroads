package ui

import (
	"image/color"

	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/fixtures"
	"github.com/marrasen/gunim/theme"
)

// The colours of the signals, and of the modules' kinds.
var (
	valueInk = theme.Color("crossroads.signal.value", color.NRGBA{R: 0xc7, G: 0x7d, B: 0xff, A: 0xff})
	lightInk = theme.Color("crossroads.signal.light", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff})
	dmxInk   = theme.Color("crossroads.signal.dmx", color.NRGBA{R: 0x4f, G: 0xd6, B: 0xc0, A: 0xff})
	mixerInk = theme.Color("crossroads.kind.mixer", color.NRGBA{R: 0xff, G: 0xc8, B: 0x4a, A: 0xff})
	stageInk = theme.Color("crossroads.kind.stage", color.NRGBA{R: 0xff, G: 0x8a, B: 0x5e, A: 0xff})
	audioInk = theme.Color("crossroads.signal.audio", color.NRGBA{R: 0x7c, G: 0xd9, B: 0x5a, A: 0xff})
	clockInk = theme.Color("crossroads.signal.clock", color.NRGBA{R: 0xff, G: 0x6a, B: 0xb0, A: 0xff})
	trigInk  = theme.Color("crossroads.signal.trigger", color.NRGBA{R: 0xff, G: 0xe0, B: 0x66, A: 0xff})
	songInk  = theme.Color("crossroads.kind.player", color.NRGBA{R: 0x9b, G: 0xe8, B: 0xff, A: 0xff})
	midiInk  = theme.Color("crossroads.kind.midi", color.NRGBA{R: 0xb4, G: 0xa7, B: 0xff, A: 0xff})
	lookInk  = theme.Color("crossroads.kind.look", color.NRGBA{R: 0xff, G: 0x9f, B: 0x43, A: 0xff})
)

// signalInk is signal s's colour.
func signalInk(th *theme.Live, s engine.Signal) color.NRGBA {
	switch s {
	case engine.Value:
		return valueInk.Get(th)
	case engine.Light:
		return lightInk.Get(th)
	case engine.Audio:
		return audioInk.Get(th)
	case engine.Clock:
		return clockInk.Get(th)
	case engine.Trigger:
		return trigInk.Get(th)
	case engine.DMX:
	}
	return dmxInk.Get(th)
}

// kindInk is the colour of a module of kind k.
func kindInk(th *theme.Live, k show.Kind) color.NRGBA {
	switch k {
	case show.Fader, show.Level, show.Pulse:
		return valueInk.Get(th)
	case show.Look, show.Chaser, show.Cues:
		return lookInk.Get(th)
	case show.AudioIn:
		return audioInk.Get(th)
	case show.Beat, show.Tempo, show.Link, show.Serato, show.ClockIn, show.ClockOut:
		return clockInk.Get(th)
	case show.Player:
		return songInk.Get(th)
	case show.Button, show.Every, show.Toggle:
		return trigInk.Get(th)
	case show.Controller, show.MIDIOut:
		return midiInk.Get(th)
	case show.Map:
		return valueInk.Get(th)
	case show.Lights:
		return lightInk.Get(th)
	case show.Mixer:
		return mixerInk.Get(th)
	case show.DMXOut:
		return dmxInk.Get(th)
	case show.StageView:
	}
	return stageInk.Get(th)
}

// roleColours are the colours of a light's colour channels at full.
var roleColours = map[fixtures.Role]color.NRGBA{
	fixtures.RoleRed:   {R: 0xff, G: 0x20, B: 0x20, A: 0xff},
	fixtures.RoleGreen: {R: 0x20, G: 0xff, B: 0x40, A: 0xff},
	fixtures.RoleBlue:  {R: 0x30, G: 0x50, B: 0xff, A: 0xff},
	fixtures.RoleWhite: {R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	fixtures.RoleAmber: {R: 0xff, G: 0xa8, B: 0x20, A: 0xff},
	fixtures.RoleUV:    {R: 0x90, G: 0x30, B: 0xff, A: 0xff},
	fixtures.RoleLime:  {R: 0xb0, G: 0xff, B: 0x30, A: 0xff},
	fixtures.RoleCyan:  {R: 0x20, G: 0xe0, B: 0xff, A: 0xff},
}

// lampLook is the colour light p gives out as u plays it, at full, and
// how bright: its colour channels mixed, its dimmer over them; a light
// of a dimmer alone gives warm white.
func lampLook(cat fixtures.Catalog, u *dmx.Universe, p fixtures.Patch) (c color.NRGBA, lit float32) {
	var r, g, b float32
	dim, dims, colour := float32(1), false, false
	for _, ch := range cat.ResolvedChannels(p) {
		if !ch.InUniverse {
			continue
		}
		v := float32(u[ch.Slot-1]) / 255
		if ch.Channel.Role == fixtures.RoleDimmer {
			dim, dims = v, true
			continue
		}
		rc, ok := roleColours[ch.Channel.Role]
		if !ok {
			continue
		}
		colour = true
		r += float32(rc.R) * v
		g += float32(rc.G) * v
		b += float32(rc.B) * v
	}
	if !colour {
		if !dims {
			return color.NRGBA{}, 0
		}
		return color.NRGBA{R: 0xff, G: 0xd9, B: 0xa0, A: 0xff}, dim
	}
	peak := max(r, g, b)
	if peak <= 0 {
		return color.NRGBA{}, 0
	}
	s := 255 / peak
	return color.NRGBA{R: uint8(r * s), G: uint8(g * s), B: uint8(b * s), A: 0xff}, min(peak/255, 1) * dim
}

// faded is c at a share a of its alpha.
func faded(c color.NRGBA, a float32) color.NRGBA {
	c.A = uint8(float32(c.A)*min(max(a, 0), 1) + 0.5)
	return c
}
