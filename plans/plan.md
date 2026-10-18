# Plan: gunim-dmx and crossroads

Two new projects:

- **gunim-dmx**: a public Go library for driving lights. It holds light types, rigs, the effects engine, and drivers for USB and network DMX boxes. It knows nothing about any app.
- **crossroads**: a gunim desktop app for running the lights and music at a gig. Everything in it is a module with inputs and outputs, wired together in a Routes view like Hej DAW!'s. It replaces midittt.

Hej DAW! moves onto gunim-dmx too. Its Light groups become a thin layer on top, so both apps share one engine.

## 1. What lives where

### gunim (existing, Apache-2.0): three things move in from Hej DAW!

These belong in the framework, because both apps need them and they aren't about lights.

1. **`widget/graph`: the Routes view as a general widget.** Today it's about 8,000 lines in Hej DAW!'s `plane*.go`, tied to the song. It splits in two:
   - **The widget:** a neutral model of boxes, typed ports (kind, group, colour), wires, and a "can these two connect?" callback. It also does the painting, zoom, orbs, focus, group rails, replugging and selection.
   - **What stays in Hej DAW!:** the code that turns a song into a graph, plus the song edits.
2. **`widget/dock`: tabs you can detach, dock and move.** These are now `layout.go`, `dock.go` and `windows.go` in Hej DAW!. Views are already named by plain strings, so this is mostly a move.
3. **`audio/beat`: a live beat tracker.** gunim's `audio/beat` only scans whole files now. The live version would:
   - find onsets (spectral flux, split into kick, snare and hats bands)
   - estimate tempo from the last 4–8 s
   - predict the next beat, BTrack-style, so lights fire on time, not late
   - hold the tempo through silence.

   It needs no new dependencies. Hej DAW! could use it too, to follow a live band.

### gunim-dmx (new, public, Apache-2.0 to match gunim)

| Package | What it holds | Comes from |
|---|---|---|
| `dmx` | A 512-channel universe and a fixed 40-per-second send loop | marrdaw `lights/dmx` |
| `dmx/enttec` | ENTTEC DMX USB Pro. This also covers the Eurolite USB-DMX512 PRO and DMXking ultraDMX, which copy its protocol | marrdaw `lights/dmx` |
| `dmx/opendmx` | ENTTEC Open DMX. The computer has to make the timing, so it gets its own thread | new |
| `dmx/artnet`, `dmx/sacn` | DMX over a network cable: send, find nodes, keep alive | new, small enough to write ourselves |
| `fixtures` | Light types: channels, roles, modes, wheels, fine channels, pixels; custom types | marrdaw `lights/fixtures` + light-types branch |
| `fixtures/ofl` | Imports the Open Fixture Library, about 1,000 makers' lights (MIT licence), into an embedded set with `go generate` | new |
| `rig` | Lights placed at addresses on universes, groups in order, positions on a stage plan | marrdaw `song/rig.go` |
| `fx` | Shapes (sine, pulse, ramp, random), spread and phase across a group, chasers, fades and delays | marrdaw `lights` + midittt `fx`, `pattern` |
| `mix` | Merges every source into the final universe: strongest wins for brightness, latest wins for colour and position, plus grand master, blackout and overrides | marrdaw `lights/output.go` |
| `view` | gunim widgets: drawing a light with its beam, a stage plan, a 512-address map | marrdaw `lightdesk.go` painting |

The `view` package is the only one that needs gunim. Everything else is plain Go, so anything can use it.

### crossroads (new): the app

- **Module runtime:** modules, ports, wires, and one clock tick that runs the graph.
- **The modules:** see section 2.
- **The windows and tabs:** see section 3.
- **The show file:** see the open questions.
- **Ported from midittt:** Ableton Link, Serato, MIDI clock in and out, timers, and the tempo-source ranking.

## 2. Modules and the signals they pass

### Signals

Each signal kind is a port kind with its own colour. A wire can only join ports of the same kind. That keeps the Routes view logical: you can see what plugs into what.

| Signal | What flows | Example |
|---|---|---|
| Audio | Sound | Line in → Beat detector |
| Clock | Tempo, beat and bar position, playing or stopped | Beat detector → Tempo → Chaser |
| Trigger | A moment: a beat, a button press, the next song | X-Touch button → Look "Drop" |
| Value | A number from 0 to 1 | X-Touch fader → Spot dimmer |
| Light | What a group of lights should do: brightness, colour, position, strobe | Effect → Mixer |
| MIDI | Raw MIDI messages | MIDI in → Filter → MIDI out |
| DMX | A finished universe | Mixer → ENTTEC out, Mixer → Stage view |

### Modules for the first version

- **Inputs:**
  - Audio in
  - MIDI in
  - Controller: a MIDI device with a known layout, such as the X-Touch Mini, with lit rings and buttons
  - Tap tempo
  - Button and fader (on-screen)
- **Sound:**
  - Music player
  - Beat detector
  - Tempo: picks the strongest source and holds your BPM override
  - Level follower: how loud right now, per band
- **Light:**
  - Group: a set of lights in order
  - Look: saved colours and levels
  - Effect: shapes spread across a group
  - Chaser
  - Cue list with GO
  - Mixer, including grand master and blackout
- **Logic:**
  - Trigger: when X happens, do Y. This is midittt's routes, one per module.
  - Map: scale or invert a value
  - Every N beats
  - Timer
- **Outputs:**
  - DMX out (ENTTEC, Open DMX, Art-Net, sACN)
  - MIDI out
  - Stage view: the simulator

A real DMX out and the Stage view are both just outputs. You can wire both at once, or only the Stage view when preparing at home.

### A typical gig in the Routes view

```
Audio in ─► Beat detector ─► Tempo ◄─ Tap tempo
                               │  ◄─ Music player (its scanned beat grid)
                               ▼
X-Touch ──► Trigger ──► Effect "Pulse" ──► Mixer ──► ENTTEC out
   │                    Look "Warm"   ──►   │   ──► Stage view
   └─ fader 1 ─────────► Spot dimmer  ──►   │
   └─ buttons ─► Music player (play, stop, next, back)
```

## 3. Windows and tabs

These are dockable tabs, as in Hej DAW!. Any tab can be torn out into its own window, docked at an edge or stacked with others. The layout is saved with the show.

| Tab | What it's for |
|---|---|
| **Routes** | Wire modules together |
| **Stage** | The simulator. Draw the venue's floor plan, place the lights, and watch beams, colours and strobes. Prepare a gig with no lights at hand. |
| **Live** | The performance page: big buttons for looks and effects, the grand master, blackout, and hold |
| **Faders** | The simple view: one fader per output or group, nothing else |
| **Player** | The playlist. Each song shows its found BPM (you can override it), its loudness and its gain knob. A big transport. |
| **Rig** | Patch lights onto addresses, pick or make light types, test each channel live |
| **Controllers** | MIDI devices, learn mode, and what each knob and button does |
| **Monitor** | Why is this light doing that? It shows which module owns each channel right now, plus a log of incoming MIDI and beats. |

Animations matter everywhere: wires that pulse with the beat, beams that fade, buttons that glow while their effect runs. gunim is built for this. It's also how the app beats the old desks, which give poor feedback.

## 4. Music player details

- **Formats:** WAV, MP3, FLAC and Ogg Vorbis, all decoded by gunim already. AAC is left out for now: the only pure-Go decoder is LGPL.
- **Scanning each song:**
  - **BPM and beat grid:** `audio/beat` does this already. You can type an override.
  - **Loudness:** `audio/loudness.go` and `audio/truepeak.go` do this already.
- **Auto-gain:** each song is brought to a target loudness, −14 LUFS by default (LUFS is the standard loudness unit). Its true peak is kept at or below −1 dBTP. A gain knob per song lets you adjust on top.
- **Clock:** the player outputs a Clock from its beat grid. Lights lock to the music without listening to it.
- **Triggers in:** play, stop, next and previous. **Triggers out:** song start, song end, each bar.

## 5. Controllers (X-Touch Mini first)

- **Mode:** the X-Touch Mini is used in standard mode. Encoders are set to relative, so values never jump.
- **Learn:** click any control in the app, then move a knob.
- **Feedback:** the app always sends values back, so the knob rings and button lights match it. It resends everything when the device reconnects.
- **Soft takeover for faders:** a fader does nothing until it passes the current value.
- **Built-in layouts:** X-Touch Mini, Akai APC mini, Novation Launchpad.
- **What a control can do:** a knob or button can do anything a module input can. That includes starting and stopping an effect, running a dimmer channel for a spot, and controlling the music transport.

## 6. Order of work

Each step ends in something you can try.

1. **Repos.** Create both GitHub repos with licences, a README and lean CI (one Linux job, with Windows run locally before every push).
2. **gunim-dmx core.**
   - Move `dmx`, `fixtures` and the custom light types out of Hej DAW!.
   - Add Open DMX, Art-Net, sACN, `mix` and the Open Fixture Library import.
   - Switch Hej DAW! over and delete its copies.
3. **gunim widgets.** Extract `widget/dock` and `widget/graph` from Hej DAW!. Hej DAW! must look and behave the same afterwards. This is the riskiest step, so it gets screenshot comparisons before and after.
4. **crossroads skeleton.** Includes:
   - the module runtime and Routes
   - the Rig, Stage simulator and Faders tabs
   - DMX out and Stage view modules
   - save and load of the show file.

   You can patch the Dome, see it on the Stage, and drive it from faders.
5. **Beat.** The live beat tracker in gunim, plus the Beat detector, Tempo (with tap and override) and Level modules. Effects and chasers lock to the beat.
6. **Player.** Includes:
   - the playlist
   - BPM and loudness scans
   - auto-gain and gain knobs
   - transport triggers
   - the Clock out.
7. **Controllers.** MIDI in and out, learn, the X-Touch Mini layout with lit rings, and Trigger and Map modules.
8. **Looks and the Live page.** Looks, chasers, cue lists, overrides ("all white", for drops), and the Live and Monitor tabs.
9. **Retire midittt.** Bring over Link, Serato, MIDI clock, timers and patterns, and check every midittt rig can be rebuilt in crossroads.
10. **Later.** In no fixed order:
    - pixel mapping for LED bars
    - moving-head shapes
    - a 3D stage
    - OSC
    - Denon StageLinQ and the Pioneer beat listener
    - RDM (setting a light's address from the computer)
    - timecode.

Steps 2 and 3 can run in parallel worktrees. So can 5 and 6, once 4 is done.

## 7. Decisions (2026-10-18)

1. **Licences.** crossroads is public under GPL-3.0, because it carries midittt's Ableton Link code, which is derived from Ableton's GPL library. gunim-dmx is public under Apache-2.0, to match gunim.
2. **Relicensing.** The `dmx` and `fixtures` code began in GPL midittt. Its author releases it as Apache-2.0 in gunim-dmx, and gunim-dmx's NOTICE says so.
3. **Music in show files.** The show file refers to the music files. A "Pack for the gig" command copies the show and its music into one folder.
4. **Hej DAW! moves onto the shared code.** In this order: the lights engine onto gunim-dmx, then the tabs onto `widget/dock`, then the Routes view onto `widget/graph`. Screenshots are compared before and after each.
