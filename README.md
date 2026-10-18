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

**Status:** being planned. See [plans/plan.md](plans/plan.md).

## Licence

GPL-3.0-or-later. See [LICENSE](LICENSE). crossroads carries code from
midittt, whose Ableton Link support is derived from Ableton's GPL
library.
