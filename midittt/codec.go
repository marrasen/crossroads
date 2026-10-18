package midittt

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/marrasen/crossroads/fx"
	"github.com/marrasen/crossroads/pattern"
	"github.com/marrasen/crossroads/routing"
	"github.com/marrasen/crossroads/tempo"
	"github.com/marrasen/gunim-dmx/fixtures"
	gtempo "github.com/marrasen/gunim/audio/tempo"
)

// The wire shapes below mirror the JSON the Electron build wrote, field
// for field. They are separate from the domain types on purpose: the
// file is something users keep and share, so its shape is fixed here
// and the Go types are free to read better.

type file struct {
	App           string            `json:"app"`
	Version       int               `json:"version"`
	Routes        []routeJSON       `json:"routes"`
	Timers        []timerJSON       `json:"timers"`
	Fixtures      []patchJSON       `json:"fixtures"`
	FixtureGroups []groupJSON       `json:"fixtureGroups"`
	FxPrograms    []fxJSON          `json:"fxPrograms"`
	LinkSettings  *linkJSON         `json:"linkSettings,omitempty"`
	MidiClock     *clockJSON        `json:"midiClock,omitempty"`
	MidiClockIn   *clockJSON        `json:"midiClockIn,omitempty"`
	TempoPriority []string          `json:"tempoPriority,omitempty"`
	FixtureTypes  []typeJSON        `json:"fixtureTypes,omitempty"`
	Patterns      []pattern.Pattern `json:"patterns,omitempty"`
	Song          string            `json:"song,omitempty"`
	Songs         []songJSON        `json:"songs,omitempty"`
}

type songJSON struct {
	Name string      `json:"name"`
	Grid *gtempo.Map `json:"grid"`
}

// typeJSON is a fixture type a rig made, with every mode it has.
type typeJSON struct {
	ID    string     `json:"id"`
	Maker string     `json:"maker,omitempty"`
	Model string     `json:"model"`
	Modes []modeJSON `json:"modes"`
}

type modeJSON struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Channels []channelJSON `json:"channels"`
}

type channelJSON struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Role string `json:"role,omitempty"`
	Home int    `json:"home,omitempty"`
}

func typeToJSON(d fixtures.Definition) typeJSON {
	out := typeJSON{ID: d.ID, Maker: d.Manufacturer, Model: d.Model}
	for _, m := range d.Modes {
		mj := modeJSON{ID: m.ID, Name: m.Name}
		for _, c := range m.Channels {
			mj.Channels = append(mj.Channels, channelJSON{Name: c.Name, Kind: string(c.Kind),
				Role: string(c.Role), Home: c.Home})
		}
		out.Modes = append(out.Modes, mj)
	}
	return out
}

// typeFromJSON reads a fixture type back. The channels are numbered
// from their order, so a file edited by hand cannot leave a gap.
func typeFromJSON(t typeJSON) fixtures.Definition {
	out := fixtures.Definition{ID: t.ID, Manufacturer: t.Maker, Model: t.Model}
	for _, m := range t.Modes {
		mode := fixtures.Mode{ID: m.ID, Name: m.Name}
		for i, c := range m.Channels {
			mode.Channels = append(mode.Channels, fixtures.Channel{Offset: i + 1, Name: c.Name,
				Kind: fixtures.Kind(c.Kind), Role: fixtures.Role(c.Role), Home: min(max(c.Home, 0), 255)})
		}
		out.Modes = append(out.Modes, mode)
	}
	return out
}

type cueJSON struct {
	Song  string  `json:"song"`
	Bar   int     `json:"bar"`
	Beat  int     `json:"beat"`
	Beats float64 `json:"beats,omitempty"`
}

type routeJSON struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	Input   inputJSON `json:"input"`
	Beat    *beatJSON `json:"beat,omitempty"`
	// Transport and Link are the triggers on a transport, in the shape
	// the Go daemon wrote them, so its rigs read as they are.
	Transport *edgeJSON       `json:"transport,omitempty"`
	Link      *edgeJSON       `json:"link,omitempty"`
	Cue       *cueJSON        `json:"cue,omitempty"`
	Output    json.RawMessage `json:"output"`
}

type edgeJSON struct {
	On string `json:"on"`
}

type inputJSON struct {
	DeviceName string          `json:"deviceName"`
	Channel    json.RawMessage `json:"channel"`
	Type       string          `json:"type"`
	Data1      json.RawMessage `json:"data1"`
}

type beatJSON struct {
	EveryBeats  int `json:"everyBeats"`
	OffsetBeats int `json:"offsetBeats,omitempty"`
}

// outputJSON is every output kind's fields in one shape. Only the ones
// the kind uses are written, which is what OmitEmpty gives, and it
// matches the discriminated union the file already held.
type outputJSON struct {
	Kind string `json:"kind"`

	DeviceName string `json:"deviceName,omitempty"`
	// Channel is the outgoing MIDI channel for a midi output and the
	// absolute universe slot for a dmx one.
	Channel int    `json:"channel,omitempty"`
	Type    string `json:"type,omitempty"`
	Data1   int    `json:"data1,omitempty"`

	TimerID string `json:"timerId,omitempty"`
	Command string `json:"command,omitempty"`

	// FixtureTarget is the channel offset within a fixture. The file
	// once allowed the string "all" here, which reading turns into a
	// target of 1 with spread on.
	FixtureID     string          `json:"fixtureId,omitempty"`
	FixtureTarget json.RawMessage `json:"target,omitempty"`

	GroupID string `json:"groupId,omitempty"`

	// Transport is what a link output does to the session.
	Transport string `json:"transport,omitempty"`

	// PatternID and PatternAction are what a pattern output plays.
	PatternID     string `json:"patternId,omitempty"`
	PatternAction string `json:"patternAction,omitempty"`
	Role          string `json:"role,omitempty"`

	Spread bool `json:"spread,omitempty"`

	ProgramID string `json:"programId,omitempty"`

	ValueMode     string  `json:"valueMode,omitempty"`
	FixedValue    *int    `json:"fixedValue,omitempty"`
	ScaleIn       *[2]int `json:"scaleIn,omitempty"`
	ScaleOut      *[2]int `json:"scaleOut,omitempty"`
	Rectify       bool    `json:"rectify,omitempty"`
	RectifyCenter *int    `json:"rectifyCenter,omitempty"`
}

type timerJSON struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Mode       string  `json:"mode"`
	DurationMs float64 `json:"durationMs"`
}

type patchJSON struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	DefinitionID string   `json:"definitionId"`
	ModeID       string   `json:"modeId"`
	StartAddress int      `json:"startAddress"`
	Gamma        *float64 `json:"gamma,omitempty"`
	MaxLevel     *int     `json:"maxLevel,omitempty"`
}

type groupJSON struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	MemberIDs []string `json:"memberIds"`
}

type fxJSON struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Steps    []stepJSON `json:"steps"`
	Quantize string     `json:"quantize,omitempty"`
}

type stepJSON struct {
	ID              string   `json:"id"`
	FixtureID       string   `json:"fixtureId"`
	Target          int      `json:"target"`
	TimeBase        string   `json:"timeBase,omitempty"`
	DelayMs         float64  `json:"delayMs"`
	DelayBeats      *float64 `json:"delayBeats,omitempty"`
	From            int      `json:"from"`
	To              int      `json:"to"`
	DurationMs      float64  `json:"durationMs"`
	DurationBeats   *float64 `json:"durationBeats,omitempty"`
	ScaleByVelocity bool     `json:"scaleByVelocity"`
}

type linkJSON struct {
	Enabled         bool    `json:"enabled"`
	Quantum         float64 `json:"quantum"`
	FollowStartStop bool    `json:"followStartStop"`
	Drive           bool    `json:"drive,omitempty"`
}

type clockJSON struct {
	Enabled    bool   `json:"enabled"`
	DeviceName string `json:"deviceName"`
}

// ErrNotOurs says a file is not a midittt configuration.
var ErrNotOurs = errors.New("config: not a midittt configuration file")

// Marshal writes a configuration as the file format, indented so a
// version control system can read a diff of it.
func Marshal(c Config) ([]byte, error) {
	f := file{App: appName, Version: FileVersion}
	f.Routes = make([]routeJSON, len(c.Routes))
	for i, r := range c.Routes {
		f.Routes[i] = routeToJSON(r)
	}
	f.Timers = make([]timerJSON, len(c.Timers))
	for i, t := range c.Timers {
		f.Timers[i] = timerJSON{ID: t.ID, Name: t.Name, Mode: string(t.Mode), DurationMs: t.DurationMs}
	}
	f.Fixtures = make([]patchJSON, len(c.Fixtures))
	for i, p := range c.Fixtures {
		f.Fixtures[i] = patchToJSON(p)
	}
	f.FixtureGroups = make([]groupJSON, len(c.Groups))
	for i, g := range c.Groups {
		f.FixtureGroups[i] = groupJSON{ID: g.ID, Name: g.Name, MemberIDs: g.MemberIDs}
	}
	f.FxPrograms = make([]fxJSON, len(c.Programs))
	for i, p := range c.Programs {
		f.FxPrograms[i] = programToJSON(p)
	}
	f.LinkSettings = &linkJSON{Enabled: c.Link.Enabled, Quantum: c.Link.Quantum,
		FollowStartStop: c.Link.FollowStartStop, Drive: c.Link.Drive}
	f.MidiClock = &clockJSON{Enabled: c.MidiClock.Enabled, DeviceName: c.MidiClock.DeviceName}
	f.MidiClockIn = &clockJSON{Enabled: c.MidiClockIn.Enabled, DeviceName: c.MidiClockIn.DeviceName}
	f.TempoPriority = c.TempoPriority
	for _, d := range c.FixtureTypes {
		f.FixtureTypes = append(f.FixtureTypes, typeToJSON(d))
	}
	f.Patterns = c.Patterns
	f.Song = c.Song
	for _, s := range c.Songs {
		if s.Grid != nil {
			f.Songs = append(f.Songs, songJSON(s))
		}
	}
	return json.MarshalIndent(f, "", "  ")
}

// Unmarshal reads a configuration file, applying every migration a file
// from an older version needs.
func Unmarshal(data []byte) (Config, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if f.App != appName || f.Routes == nil {
		return Config{}, ErrNotOurs
	}
	c := New()
	for _, r := range f.Routes {
		route, err := routeFromJSON(r, f.Version)
		if err != nil {
			return Config{}, err
		}
		c.Routes = append(c.Routes, route)
	}
	for _, t := range f.Timers {
		c.Timers = append(c.Timers, Timer{ID: t.ID, Name: t.Name,
			Mode: TimerMode(t.Mode), DurationMs: t.DurationMs})
	}
	for _, p := range f.Fixtures {
		c.Fixtures = append(c.Fixtures, patchFromJSON(p))
	}
	for _, g := range f.FixtureGroups {
		c.Groups = append(c.Groups, fixtures.Group{ID: g.ID, Name: g.Name, MemberIDs: g.MemberIDs})
	}
	for _, p := range f.FxPrograms {
		c.Programs = append(c.Programs, programFromJSON(p))
	}
	if f.LinkSettings != nil {
		c.Link = LinkSettings{Enabled: f.LinkSettings.Enabled, Quantum: f.LinkSettings.Quantum,
			FollowStartStop: f.LinkSettings.FollowStartStop, Drive: f.LinkSettings.Drive}
		// A quantum of zero or less would divide the bar by nothing.
		if !(c.Link.Quantum > 0) {
			c.Link.Quantum = DefaultLink.Quantum
		}
	}
	c.TempoPriority = f.TempoPriority
	for _, t := range f.FixtureTypes {
		c.FixtureTypes = append(c.FixtureTypes, typeFromJSON(t))
	}
	c.Patterns = f.Patterns
	c.Song = f.Song
	for _, s := range f.Songs {
		if s.Grid != nil {
			c.Songs = append(c.Songs, Song(s))
		}
	}
	if f.MidiClockIn != nil {
		c.MidiClockIn = MidiClockInSettings{Enabled: f.MidiClockIn.Enabled,
			DeviceName: f.MidiClockIn.DeviceName}
	}
	if f.MidiClock != nil {
		c.MidiClock = MidiClockSettings{Enabled: f.MidiClock.Enabled, DeviceName: f.MidiClock.DeviceName}
	}
	return c, nil
}

// ---- routes ----

func routeToJSON(r routing.Route) routeJSON {
	out := routeJSON{ID: r.ID, Name: r.Name, Enabled: r.Enabled, Input: inputJSON{
		DeviceName: r.Input.DeviceName,
		Channel:    channelToJSON(r.Input.Channel),
		Type:       string(r.Input.Type),
		Data1:      data1ToJSON(r.Input.Data1),
	}}
	if r.HasBeat {
		out.Beat = &beatJSON{EveryBeats: r.Beat.EveryBeats, OffsetBeats: r.Beat.OffsetBeats}
	}
	if r.Transport != "" {
		out.Transport = &edgeJSON{On: string(r.Transport)}
	}
	if r.Link != "" {
		out.Link = &edgeJSON{On: string(r.Link)}
	}
	if r.Cue != nil {
		out.Cue = &cueJSON{Song: r.Cue.Song, Bar: r.Cue.Bar, Beat: r.Cue.Beat, Beats: r.Cue.Beats}
	}
	out.Output, _ = json.Marshal(outputToJSON(r.Output))
	return out
}

func routeFromJSON(r routeJSON, version int) (routing.Route, error) {
	out := routing.Route{ID: r.ID, Name: r.Name, Enabled: r.Enabled}
	channel, err := channelFromJSON(r.Input.Channel)
	if err != nil {
		return routing.Route{}, fmt.Errorf("config: route %q: %w", r.ID, err)
	}
	data1, err := data1FromJSON(r.Input.Data1)
	if err != nil {
		return routing.Route{}, fmt.Errorf("config: route %q: %w", r.ID, err)
	}
	out.Input = routing.Input{
		DeviceName: r.Input.DeviceName,
		Channel:    channel,
		Type:       routing.Type(r.Input.Type),
		Data1:      data1,
	}
	if r.Beat != nil {
		out.HasBeat = true
		out.Beat = routing.Beat{EveryBeats: r.Beat.EveryBeats, OffsetBeats: r.Beat.OffsetBeats}
	}
	// A trigger on an edge this version does not know is an error rather
	// than nothing: read as nothing, the route would fall back to its MIDI
	// matcher and fire on whatever that matches.
	if r.Transport != nil {
		out.Transport = tempo.TransportEdge(r.Transport.On)
		if !out.Transport.Valid() {
			return routing.Route{}, fmt.Errorf("config: route %q: unknown transport edge %q", r.ID, r.Transport.On)
		}
	}
	if r.Link != nil {
		out.Link = routing.LinkEdge(r.Link.On)
		if !out.Link.Valid() {
			return routing.Route{}, fmt.Errorf("config: route %q: unknown Link edge %q", r.ID, r.Link.On)
		}
	}
	if r.Cue != nil {
		out.Cue = &routing.Cue{Song: r.Cue.Song, Bar: r.Cue.Bar, Beat: max(r.Cue.Beat, 1), Beats: max(r.Cue.Beats, 0)}
	}
	out.Output, err = outputFromJSON(r.Output, version)
	if err != nil {
		return routing.Route{}, fmt.Errorf("config: route %q: %w", r.ID, err)
	}
	return out, nil
}

// channelToJSON writes a MIDI channel, or the string "any".
func channelToJSON(channel int) json.RawMessage {
	if channel == routing.AnyChannel {
		return json.RawMessage(`"any"`)
	}
	b, _ := json.Marshal(channel)
	return b
}

func channelFromJSON(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return routing.AnyChannel, nil
	}
	var channel int
	if err := json.Unmarshal(raw, &channel); err == nil {
		return channel, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s == "any" {
		return routing.AnyChannel, nil
	}
	return 0, fmt.Errorf("channel %s is neither a number nor \"any\"", raw)
}

// data1ToJSON writes a note or CC matcher: a number, the string "any",
// or a span.
func data1ToJSON(d routing.Data1) json.RawMessage {
	switch d.Kind {
	case routing.Data1Any:
		return json.RawMessage(`"any"`)
	case routing.Data1Range:
		b, _ := json.Marshal(struct {
			From int `json:"from"`
			To   int `json:"to"`
		}{d.From, d.To})
		return b
	default:
		b, _ := json.Marshal(d.Number)
		return b
	}
}

func data1FromJSON(raw json.RawMessage) (routing.Data1, error) {
	if len(raw) == 0 {
		return routing.Any(), nil
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return routing.One(number), nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s == "any" {
		return routing.Any(), nil
	}
	var span struct {
		From int `json:"from"`
		To   int `json:"to"`
	}
	if err := json.Unmarshal(raw, &span); err == nil {
		return routing.Span(span.From, span.To), nil
	}
	return routing.Data1{}, fmt.Errorf("note or CC matcher %s is not a number, \"any\" or a span", raw)
}

// ---- outputs ----

func outputToJSON(o routing.Output) outputJSON {
	out := outputJSON{Kind: string(o.Kind)}
	switch o.Kind {
	case routing.ToTimer:
		out.TimerID, out.Command = o.TimerID, string(o.Command)
		return out
	case routing.ToFx:
		out.ProgramID = o.ProgramID
		return out
	case routing.ToLink:
		out.Transport = string(o.Link)
		return out
	case routing.ToPattern:
		out.PatternID, out.PatternAction = o.PatternID, string(o.Pattern)
		return out
	case routing.ToMidi:
		out.DeviceName, out.Channel = o.DeviceName, o.Channel
		out.Type, out.Data1 = string(o.Type), o.Data1
		writeValue(&out, o.Value, false)
		return out
	case routing.ToDmx:
		out.Channel = o.Slot
	case routing.ToFixture:
		out.FixtureID = o.FixtureID
		out.FixtureTarget, _ = json.Marshal(o.Target)
		out.Spread = o.Spread
	case routing.ToGroup:
		out.GroupID, out.Role, out.Spread = o.GroupID, string(o.Role), o.Spread
	}
	writeValue(&out, o.Value, true)
	return out
}

// writeValue fills the value fields, leaving out the ones that carry
// the default for this kind of output, as the file always did.
func writeValue(out *outputJSON, v routing.Value, isDmx bool) {
	out.ValueMode = string(v.Mode)
	switch v.Mode {
	case routing.Fixed:
		fixed := v.Fixed
		out.FixedValue = &fixed
	case routing.Scaled:
		in, outRange := v.ScaleIn, v.ScaleOut
		out.ScaleIn, out.ScaleOut = &in, &outRange
	case routing.Passthrough:
	}
	if isDmx && v.Rectify {
		out.Rectify = true
		if v.RectifyCenter != 0 && v.RectifyCenter != routing.DefaultRectifyCenter {
			center := v.RectifyCenter
			out.RectifyCenter = &center
		}
	}
}

func outputFromJSON(raw json.RawMessage, version int) (routing.Output, error) {
	var in outputJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return routing.Output{}, err
	}
	// Version 0 had no kind: every output was a MIDI message.
	if in.Kind == "" {
		in.Kind = string(routing.ToMidi)
	}
	out := routing.Output{Kind: routing.OutputKind(in.Kind)}
	switch out.Kind {
	case routing.ToTimer:
		out.TimerID, out.Command = in.TimerID, routing.TimerCommand(in.Command)
		return out, nil
	case routing.ToFx:
		out.ProgramID = in.ProgramID
		return out, nil
	case routing.ToPattern:
		out.PatternID, out.Pattern = in.PatternID, routing.PatternAction(in.PatternAction)
		if !out.Pattern.Valid() {
			return routing.Output{}, fmt.Errorf("unknown pattern action %q", in.PatternAction)
		}
		return out, nil
	case routing.ToLink:
		out.Link = routing.LinkCommand(in.Transport)
		if !out.Link.Valid() {
			return routing.Output{}, fmt.Errorf("unknown Link command %q", in.Transport)
		}
		return out, nil
	case routing.ToMidi:
		// Version 1 keyed devices by the browser's port id, which was
		// specific to a machine and cannot be mapped to a name. Those
		// files come through with no device, to be picked again once.
		if version >= 2 {
			out.DeviceName = in.DeviceName
		}
		out.Channel, out.Type, out.Data1 = in.Channel, routing.Type(in.Type), in.Data1
		out.Value = readValue(in, [2]int{0, 127})
		return out, nil
	case routing.ToDmx:
		out.Slot = in.Channel
	case routing.ToFixture:
		out.FixtureID = in.FixtureID
		out.Target, out.Spread = fixtureTarget(in)
	case routing.ToGroup:
		out.GroupID, out.Role, out.Spread = in.GroupID, fixtures.Role(in.Role), in.Spread
	default:
		return routing.Output{}, fmt.Errorf("unknown output kind %q", in.Kind)
	}
	out.Value = readValue(in, [2]int{0, 255})
	return out, nil
}

// fixtureTarget reads a fixture output's target. Up to version 3 it
// could be the string "all", which meant the whole fixture; that became
// a start channel of one with spreading on.
func fixtureTarget(in outputJSON) (target int, spread bool) {
	var n int
	if err := json.Unmarshal(in.FixtureTarget, &n); err == nil {
		return n, in.Spread
	}
	var s string
	if err := json.Unmarshal(in.FixtureTarget, &s); err == nil && s == "all" {
		return 1, true
	}
	return 1, in.Spread
}

// readValue fills in the ranges an output left out, so nothing
// downstream has to know what the defaults were.
func readValue(in outputJSON, defaultOut [2]int) routing.Value {
	v := routing.Value{Mode: routing.ValueMode(in.ValueMode),
		ScaleIn: [2]int{0, 127}, ScaleOut: defaultOut,
		Rectify: in.Rectify, RectifyCenter: routing.DefaultRectifyCenter}
	if v.Mode == "" {
		v.Mode = routing.Fixed
	}
	if in.FixedValue != nil {
		v.Fixed = *in.FixedValue
	}
	if in.ScaleIn != nil {
		v.ScaleIn = *in.ScaleIn
	}
	if in.ScaleOut != nil {
		v.ScaleOut = *in.ScaleOut
	}
	if in.RectifyCenter != nil {
		v.RectifyCenter = *in.RectifyCenter
	}
	return v
}

// ---- fixtures and programmes ----

func patchToJSON(p fixtures.Patch) patchJSON {
	out := patchJSON{ID: p.ID, Name: p.Name, DefinitionID: p.DefinitionID,
		ModeID: p.ModeID, StartAddress: p.StartAddress}
	if p.Gamma != 0 && p.Gamma != 1 {
		gamma := p.Gamma
		out.Gamma = &gamma
	}
	if p.MaxLevel != 0 && p.MaxLevel != 255 {
		level := p.MaxLevel
		out.MaxLevel = &level
	}
	return out
}

func patchFromJSON(p patchJSON) fixtures.Patch {
	out := fixtures.Patch{ID: p.ID, Name: p.Name, DefinitionID: p.DefinitionID,
		ModeID: p.ModeID, StartAddress: p.StartAddress, Gamma: 1, MaxLevel: 255}
	if p.Gamma != nil {
		out.Gamma = *p.Gamma
	}
	if p.MaxLevel != nil {
		out.MaxLevel = *p.MaxLevel
	}
	return out
}

func programToJSON(p fx.Program) fxJSON {
	out := fxJSON{ID: p.ID, Name: p.Name, Quantize: string(p.Quantize)}
	if out.Quantize == string(fx.QuantizeOff) {
		out.Quantize = ""
	}
	out.Steps = make([]stepJSON, len(p.Steps))
	for i, s := range p.Steps {
		step := stepJSON{ID: s.ID, FixtureID: s.FixtureID, Target: s.Target,
			DelayMs: s.DelayMs, From: s.From, To: s.To, DurationMs: s.DurationMs,
			ScaleByVelocity: s.ScaleByVelocity}
		if s.TimeBase == fx.InBeats {
			step.TimeBase = string(fx.InBeats)
			delay, duration := s.DelayBeats, s.DurationBeats
			step.DelayBeats, step.DurationBeats = &delay, &duration
		}
		out.Steps[i] = step
	}
	return out
}

func programFromJSON(p fxJSON) fx.Program {
	out := fx.Program{ID: p.ID, Name: p.Name, Quantize: fx.Quantize(p.Quantize)}
	if out.Quantize == "" {
		out.Quantize = fx.QuantizeOff
	}
	out.Steps = make([]fx.Step, len(p.Steps))
	for i, s := range p.Steps {
		step := fx.Step{ID: s.ID, FixtureID: s.FixtureID, Target: s.Target,
			TimeBase: fx.TimeBase(s.TimeBase), DelayMs: s.DelayMs, From: s.From, To: s.To,
			DurationMs: s.DurationMs, ScaleByVelocity: s.ScaleByVelocity}
		if step.TimeBase == "" {
			step.TimeBase = fx.InMilliseconds
		}
		if s.DelayBeats != nil {
			step.DelayBeats = *s.DelayBeats
		}
		if s.DurationBeats != nil {
			step.DurationBeats = *s.DurationBeats
		}
		out.Steps[i] = step
	}
	return out
}
