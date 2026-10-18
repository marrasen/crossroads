# crossroads

Run the lights and the music at a gig, from one app.

Everything in crossroads is a module with inputs and outputs: an audio
input, a beat detector, a music player, a light effect, a MIDI
controller, a trigger, a DMX output. You wire them together in the
Routes view, the way Hej DAW! wires its sound. A whole gig can be built
and tested in software, on a simulated stage, before any light is
plugged in.

It is built with [gunim](https://github.com/marrasen/gunim) and drives
lights through [gunim-dmx](https://github.com/marrasen/gunim-dmx). It
takes over from midittt.

**Status:** a first skeleton. It opens with a Fun Generation Diamond
Dome on an eight by five metre stage, a fader for each of its colours,
a mixer, a DMX out and the stage view, all wired up:

- **Routes** shows the modules and their wires. Drag from a port to
  join it to another of the same kind; Add module adds one.
- **Stage** shows the room from above, each light glowing as it plays.
  Drag a light to hang it elsewhere.
- **Faders** is a fader for each fader module, and the grand master.
- **Rig** is the lights, their types and their addresses.
- **Player** is the playlist and the transport.

Lights can follow the music. **Audio in** listens to a sound card
input. **Beat** finds the beat in it, and keeps counting through a
break. **Tempo** follows that beat, or plays a tempo you tap or type.
**Pulse** flashes on every beat, or every few, and fades. **Level** is
how loud the sound is. A Pulse or a Level wires into anything a fader
can.

The **Player** tab is the playlist. Each song is scanned once for its
beats and loudness, and plays brought to −14 LUFS, with a gain of its
own on top. The **Player** module sends out the song's sound, a clock
from its beats, and triggers as each song starts and ends and on each
bar; triggers in, as from a **Button**, play, stop, skip and go back.

Tabs drag into any arrangement, and out into windows of their own.
Ctrl+S saves the show, Ctrl+O opens one. See
[plans/plan.md](plans/plan.md) for what comes next.

```sh
go run github.com/marrasen/crossroads@latest
```

## Licence

GPL-3.0-or-later. See [LICENSE](LICENSE). crossroads carries code from
midittt, whose Ableton Link support is derived from Ableton's GPL
library.
