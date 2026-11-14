# Plan: crossroads as a stand-alone DMX board

**Status:** a plan, decided on 2026-11-14. Nothing of it is built yet.

crossroads should be usable in place of a hardware DMX board: plug in
an ENTTEC box, open the **Board** tab, pick lights, push faders, store
scenes, run chases. Nobody should need the Routes tab for it.

It is built in five steps, each one pull request that can be tried on
its own:

1. The X-Touch Mini in Mackie Control mode.
2. The Board: lights, channel faders, master, blackout, flash, output.
3. Scenes.
4. Chases.
5. The X-Touch Mini plays the Board.

## What the Board is

The Board is a **module**, like everything else in crossroads, and a
**tab** that plays the show's first Board module.

- **Module `board`.** One input, `clock` (Clock). One output, `out`
  (Light). Its output is a layer of channel values, as a Look's is, and
  it is wired into a Mixer.
- **The starting show** gets a Board wired into the Mixer's second
  input, and the Board tab in front on the left, in the larger half of
  the window. The Dome, its Lights module and its six faders stay as
  they are.
- **A show without a Board** (one saved before, or a midittt rig) gets
  the Board tab too. The tab then says the show has no board, with one
  button, "Add a board", which adds the module and wires it into the
  first Mixer's first free input.
- **Master, blackout and flash** on the Board tab are the Mixer's own:
  the Mixer the Board's output is wired into. The Board adds no second
  master.
- **The output** on the Board tab is the first DMX out that Mixer feeds.

### What is saved, and what is not

Saved in the show, on the Board module:

- the scenes and the chases;
- the scene fade time, the chase speed, the chase fade, and whether the
  chase follows the beat;
- the controller that plays the Board (step 5).

Not saved, kept by the engine while the show is open, as a hardware
board keeps its faders:

- which lights are picked;
- the values the channel faders hold;
- which scenes are on, and whether a chase runs.

The engine reports them in `engine.Live`, so every window and the
X-Touch Mini show the same thing.

### How the Board makes its light

Three sources, merged per channel:

1. **Scenes that are on.** Where two scenes set the same channel, the
   higher value wins.
2. **The running chase**, merged with the scenes the same way.
3. **The faders.** A channel a fader holds wins over scenes and chase.
   **Clear** lets go of every fader, so the scenes show again.

**The Board has the channels it sets.** What it plays on a channel wins
over whatever else is wired into the Mixer, under the grand master and
blackout. So a fader pulled down really goes down, and recording a
scene never changes the stage. A scene or a chase step fading in or out
crosses to another scene of the Board's that holds the channel, or,
where none does, to what plays under the Board. A channel the Board does not set shows what
the other modules play, or the Mixer's home value.

A fader's value is the channel's DMX value, 0 to 255, as on a hardware
board. No curve is applied.

## Step 1: the X-Touch Mini in Mackie Control mode

crossroads' built-in layout was written from memory and never tried on
the device. Hej DAW!'s research (`plans/midi-input.md` in marrdaw,
section "The X-Touch Mini") checked every message on the real device on
2026-11-01. This step moves crossroads onto it. MC mode needs no setup
in Behringer's editor, and its lights work.

- **Switching.** On connect, and on every reconnect, send CC 127 value 1
  on all 16 channels (`B0 7F 01` to `BF 7F 01`), then every light.
- **Knobs 1–8.** CC 16–23 on channel 1. 1–7 is clockwise, 65–71
  anticlockwise, the number being the speed. This is not two's
  complement: 65 is one step down. A step moves the value 1/100 of its
  range, times the speed.
- **Knobs pressed.** Notes 32–39, 127 down, 0 up.
- **Fader.** Pitch bend on channel 9. The first data byte is always 0;
  the second is the value, 0–127.
- **Top row.** Notes 89, 90, 40, 41, 42, 43, 44, 45, left to right.
- **Bottom row.** Notes 87, 88, 91, 92, 86, 93, 94, 95, left to right.
- **Layer A and B.** Notes 84 and 85. They become two more buttons with
  lights.
- **Button lights.** Note on, same note: 0 off, 127 on, 1 blinking.
- **Rings.** CC 48–55, value = style + position. Style fan is +32.
  Position 0 is dark, 1–11 fills from the left. A knob's ring shows its
  value: dark at 0, else 1 to 11.
- **Still in Standard mode.** If a knob sends CC 1–8 on channel 11, the
  switch did not reach the device. The Controllers tab then says it
  wants MC mode, and how: hold MC while plugging it in.

The controls keep their IDs (`knob1`…, `push1`…, `button1`…, `fader`),
so wires in saved shows still fit. `layerA` and `layerB` are new.

## Step 2: the Board

The Board module, as above, and the Board tab:

- **Lights.** A button for each light of the rig, in rig order, and one
  for each group. A click picks that light alone; Ctrl+click adds or
  removes one; a group's button picks its lights. Picked buttons are
  lit. Each button shows the light's colour and level as it plays now.
- **Channel faders.** For what is picked:
  - one light, or several of one type and mode: a fader for each
    channel, in the light's own order, named by the channel, in the
    colour of what it does;
  - lights of different types: a fader for each thing they do between
    them (dimmer, red, strobe…), which sets that channel on each light
    that has it.
  A fader shows the first picked light's value, and its number, 0–255.
  A fader not yet touched stands at what the light plays now and is
  drawn dim.
- **Clear.** Lets go of every fader.
- **Master, Blackout, Flash.** The Mixer's grand master as a fader,
  blackout as a toggle, flash while held. They use the intents the Live
  tab already sends.
- **Output.** A row with:
  - the box: Nowhere, ENTTEC USB Pro, ENTTEC Open DMX, Art-Net, sACN;
  - for a USB box, the serial port: "Find it", or one of the ports
    found;
  - for Art-Net and sACN, the address and the universe;
  - how it stands: connected and to what, connecting, or the error.
  The same port, address and universe fields go on the DMX out's card
  in Routes, which today only picks the box.
- **Monitor.** The Monitor tab names the Board as the owner of the
  channels it sets.

## Step 3: scenes

- **A scene** is a name and a value for each channel it holds, by light.
  Scenes are saved on the Board module, in order.
- **Pads.** A grid of pads, 8 a row, at least 16. A pad with a scene
  shows its name and a swatch of its colour; an empty one is dim.
- **Record.** Press Record, then a pad. The pad stores what the Board
  plays now: the faders over the scenes that are on. The scene is then
  on and the faders are let go, so the stage does not change. Recording
  onto a pad that has a scene replaces it. Record pressed again, or
  Escape, gives up. A chase that runs is stored as it plays, and stops.
- **Playing.** A pad pressed turns its scene on, pressed again off. It
  fades in and out over the **scene fade** time, a fader from 0 to 10
  seconds.
- **Rename and delete.** From a pad's menu.

A Look stays what it is, one level per role. Scenes are the Board's own.

## Step 4: chases

- **A chase** is a name and a list of scenes, in order. Chases are
  saved on the Board module.
- **Making one.** "New chase", then press the pads in the order they
  should play; "Done" ends it. A chase's steps can be removed and the
  chase renamed or deleted from its menu.
- **Running.** A button for each chase. One chase runs at a time;
  pressing the running one stops it.
- **Speed.** A fader, 20 to 600 steps a minute. It needs no clock.
  **Tap** sets it from the gap between taps, and puts a step on the
  tap: the chase takes its next step, unless it has just taken one by
  itself. The speed is kept in the show once the tapping stops.
- **Fade.** A fader, 0 to 1: how much of each step is spent fading to
  it, as the Chaser module's.
- **Follow the beat.** A switch. On, the chase steps on each beat of
  the clock wired into the Board's `clock` input, and the speed fader
  is dim. With nothing wired there, the switch offers "Listen to the
  sound card", which adds an Audio in and a Beat module and wires them
  in. The starting show does not open the sound card unasked.

## Step 5: the X-Touch Mini plays the Board

The Board has a setting, **Played from**: none, or one of the show's
Controller modules. "Add an X-Touch Mini" on the Board tab adds a
Controller with the built-in layout, on the first port whose name
starts with "X-TOUCH MINI", and picks it.

While a controller plays the Board, the Board owns all its lights. The
controller's outputs still work in Routes.

| Control | Does | Light |
|---|---|---|
| Knobs 1–8 | The first eight channel faders | Ring shows the value |
| Knob pressed | Lets go of that fader | |
| Top row | Scene pads 1–8 | Lit while on; all blink while Record waits |
| Bottom 1, 2 | Pick the light before, the next light | |
| Bottom 3 | Record | Blinks while it waits for a pad |
| Bottom 4 | Clear | Lit while any fader holds a value |
| Bottom 5 | Start or stop the chase last run | Lit while it runs |
| Bottom 6 | Tap | Flashes on each step |
| Bottom 7 | Flash, while held | Lit while held |
| Bottom 8 | Blackout | Lit while on |
| Layer A, B | Page 1 or 2: channels 1–8 or 9–16, pads 1–8 or 9–16 | The page's is lit |
| Fader | Grand master, picked up as it passes the value | |

The Board tab shows the page, and marks the eight faders and pads the
controller holds.

## How each step is checked

- Engine tests with the fake MIDI ports and no DMX box: what the Board's
  layer holds, what a scene stores, when a chase steps, which bytes go
  to the X-Touch Mini.
- UI tests with an offscreen window: the tab builds, a fader dragged
  sends its intent, a pad pressed plays.
- `crossroads -shot` to look at the tab.
- `go vet`, `go test -race`, `golangci-lint`, and a build with
  `CGO_ENABLED=0` for Linux and Windows.

Not checked by this work: a real ENTTEC box and a real X-Touch Mini.
Neither is on the machine it is built on. The ENTTEC output is known to
work on Windows.

## Left out

- Pan and tilt as a pad, and moving-head shapes.
- A scene level per pad; a scene is on or off.
- More than one chase at a time.
- More than one Board per show on the tab; the tab plays the first.
- The Akai APC mini and Novation Launchpad as Board controllers.
