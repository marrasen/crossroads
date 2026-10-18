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
- **Controllers** is each MIDI controller and MIDI out: its port, its
  controls as they move, and learning new ones.
- **Patterns** draws light patterns on the beat, as midittt's editor
  did: a lane for each group or light, clips of colour, level, strobe
  or a function dragged along the bars, and Try to play one on the
  stage while held.
- **Live** is the page to play the show from: each grand master,
  blackout, flash and hold, a pad for each look, GO and Back for each
  cue list, and the buttons and toggles.
- **Monitor** says why a light does what it does: each channel's value
  and the module it comes from, and a log of MIDI, beats and songs.

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

A **Controller** module is a MIDI controller. A Behringer X-Touch Mini
is built in: set its knobs to relative in the X-Touch editor, and its
rings and button lights follow what crossroads plays. Any other
controller learns its controls: press Learn a control on the
**Controllers** tab, then move one. A fader takes over its value only
once it passes it. **Map** scales a value, **Every** fires every so
many beats, **Toggle** turns on and off, and **MIDI out** sends notes
and controller moves.

A **Look** holds a level for each thing its lights do, brought up and
down by one level. A **Chaser** steps through the looks wired into it
on the beat; a **Cue list** fades to the next as GO is pressed. Each
mixer can black out, flash every light full white for a drop, and
hold what plays.

Clocks come from elsewhere too, as midittt's did: **Ableton Link**
follows the Link session on the network, **Serato** follows the deck
Serato DJ Pro's crossfader favours, and **MIDI clock in** follows a
DAW or a drum machine. **MIDI clock out** sends any clock on to gear
that only speaks MIDI clock.

A **Timer** counts down to zero, firing as it gets there, or up as a
stopwatch. A **Pattern** plays one of midittt's light patterns, drawn
on the beat, locked to the clock while it is held or on.

An **Effect** plays one of midittt's effect programmes as it is
triggered: a short move, timed in milliseconds or beats, scaled by how
hard the pad was hit. **MIDI routes** plays midittt's routes as they
were, programmes included.

Tabs drag into any arrangement, and out into windows of their own.
Ctrl+S saves the show, Ctrl+O opens one. See
[plans/plan.md](plans/plan.md) for what comes next.

```sh
go run github.com/marrasen/crossroads@latest
```

## Moving from midittt

**Show → Import a midittt rig…** opens a midittt rig, rebuilt as a
show: its lights, their types and groups, its patterns, effect
programmes and timers, its tempo sources and MIDI clock, and all its
routes in one MIDI routes module, which plays them as midittt did. A
dialog then says what, if anything, plays differently.

To check every rig at once, without opening them:

```sh
crossroads -check-midittt rigs/*.json
```

Each rig is said to come over whole, or what of it plays differently:
a route on a song cue, say, which crossroads does not hear.

## Licence

GPL-3.0-or-later. See [LICENSE](LICENSE). crossroads carries code from
midittt, whose Ableton Link support is derived from Ableton's GPL
library.
