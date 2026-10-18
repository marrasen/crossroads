package tempo

import (
	"slices"
	"sync"
)

// A Registry holds every registered tempo source's latest Clock and
// works out which one drives the lights: the highest ranked source that
// is registered now.
//
// Producers — Link, Serato, MIDI clock, the song, the manual tap — push
// snapshots; consumers — the FX engine, routing, the interface — sample
// the driving clock through BeatsNow.
//
// Ranking rather than picking is what makes a live rig work without
// anyone touching the window: plug a controller in and its clock takes
// over, close the DAW and the tempo falls back to whatever is left. A
// source nobody has ranked sits below those who have been, in the order
// it first appeared, so something new still drives rather than being
// ignored.
//
// A built-in manual source is always there, so the whole tempo feature
// works with no external producer connected, and so there is always a
// winner. Set its tempo and transport with SetManual.
//
// A Registry is safe to use from several goroutines: producers run on
// their own, and the application half reads it from the serve loop.
type Registry struct {
	mu     sync.RWMutex
	clocks map[string]Clock
	labels map[string]string
	order  []string
	// priority is the ranking the user set, best first. Manual is
	// always in it.
	priority  []string
	selected  string
	listeners []func()
	// transports hear the driving source's transport move.
	transports []func(TransportEdge)
	now        func() float64
}

// NewRegistry makes a registry holding a stopped manual source.
func NewRegistry() *Registry {
	r := &Registry{
		clocks:   map[string]Clock{},
		labels:   map[string]string{ManualSourceID: "Manual"},
		order:    []string{ManualSourceID},
		selected: ManualSourceID,
		now:      Now,
	}
	r.clocks[ManualSourceID] = Clock{SourceID: ManualSourceID, BPM: 120, RefTimeMs: r.now()}
	return r
}

// Publish registers or refreshes a producer's snapshot, adding the
// source when it is new. An empty label leaves an existing one alone.
func (r *Registry) Publish(c Clock, label string) {
	r.mu.Lock()
	if _, known := r.labels[c.SourceID]; !known {
		r.order = append(r.order, c.SourceID)
		r.labels[c.SourceID] = c.SourceID
	}
	if label != "" {
		r.labels[c.SourceID] = label
	}
	previous, had := r.clocks[c.SourceID]
	r.clocks[c.SourceID] = c
	r.resolveLocked()
	edge := r.edgeLocked(c.SourceID, had, previous.Playing, c)
	r.mu.Unlock()
	// The beat listeners first and the transport second: one of the
	// first moves the beat grid, and a cue fired before that would run
	// against the grid the transport left behind.
	r.emit()
	r.emitTransport(edge)
}

// edgeLocked is how the transport moved, or empty when it did not move
// or the source is not the one driving the lights.
//
// A source appearing is not a transport moving: it is a device being
// plugged in or an app being opened, and firing a cue on that would
// surprise anyone. Nor is the ranking settling on another source, which
// is why this only reports on the source already selected. The caller
// holds r.mu.
func (r *Registry) edgeLocked(sourceID string, had, wasPlaying bool, next Clock) TransportEdge {
	if sourceID != r.selected {
		return ""
	}
	// A source that says how its transport moved is believed. MIDI clock
	// hears a Start before it has measured enough of a tempo to register
	// at all, and carries the Start to the snapshot that does.
	if next.Transport.Valid() {
		return next.Transport
	}
	if !had || wasPlaying == next.Playing {
		return ""
	}
	if next.Playing {
		return TransportPlay
	}
	return TransportStop
}

// Remove drops a producer, as when Link is switched off or a Serato
// deck is unloaded. The manual source stays.
func (r *Registry) Remove(sourceID string) {
	if sourceID == ManualSourceID {
		return
	}
	r.mu.Lock()
	delete(r.clocks, sourceID)
	delete(r.labels, sourceID)
	for i, id := range r.order {
		if id == sourceID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.resolveLocked()
	r.mu.Unlock()
	r.emit()
}

// SetManual drives the built-in manual source. Beats advance from the
// moment it plays, and the beat reached so far is frozen first, so
// changing the tempo or stopping does not jump the position.
func (r *Registry) SetManual(bpm float64, playing bool) {
	r.mu.Lock()
	prev := r.clocks[ManualSourceID]
	at := r.now()
	if bpm <= 0 {
		bpm = prev.BPM
	}
	next := Clock{
		SourceID:  ManualSourceID,
		BPM:       bpm,
		BeatAtRef: BeatsAt(prev, at),
		RefTimeMs: at,
		Playing:   playing,
	}
	r.clocks[ManualSourceID] = next
	edge := r.edgeLocked(ManualSourceID, true, prev.Playing, next)
	r.mu.Unlock()
	r.emit()
	r.emitTransport(edge)
}

// SetPriority sets the ranking, best first. A source left out of it
// still drives where nothing above it is registered, since a ranking
// nobody has edited should not silence a device somebody plugged in.
func (r *Registry) SetPriority(ids []string) {
	r.mu.Lock()
	r.priority = slices.Clone(ids)
	r.resolveLocked()
	r.mu.Unlock()
	r.emit()
}

// Priority is the ranking as it stands, best first, every known source
// in it: those the user ranked, then the rest in the order they first
// appeared.
func (r *Registry) Priority() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rankedLocked()
}

// rankedLocked is every source known, in the order they are tried: the
// ranking the user set, then anything else in the order it first
// appeared, and the manual source last of all unless the user put it
// higher. Manual is last because it is always registered, and a source
// somebody connected should beat a number somebody typed.
//
// The caller holds r.mu.
func (r *Registry) rankedLocked() []string {
	out := slices.Clone(r.priority)
	for _, id := range r.order {
		if id == ManualSourceID || slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	if !slices.Contains(out, ManualSourceID) {
		out = append(out, ManualSourceID)
	}
	return out
}

// resolveLocked picks the source driving the lights: the highest
// ranked one that is registered. The caller holds r.mu.
func (r *Registry) resolveLocked() {
	for _, id := range r.rankedLocked() {
		if _, ok := r.clocks[id]; ok {
			r.selected = id
			return
		}
	}
	r.selected = ManualSourceID
}

// Selected is the id of the source driving the lights.
func (r *Registry) Selected() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.selected
}

// Active is the selected snapshot, and false if it somehow went away.
func (r *Registry) Active() (Clock, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clocks[r.selected]
	return c, ok
}

// ClockFor is one named producer's snapshot, whether or not it is
// selected. It lets a producer's own panel show its beat moving while
// the lights follow another.
func (r *Registry) ClockFor(sourceID string) (Clock, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clocks[sourceID]
	return c, ok
}

// BeatsNow is the selected clock's beat position at atMs, and false
// with nothing selected.
func (r *Registry) BeatsNow(atMs float64) (float64, bool) {
	c, ok := r.Active()
	if !ok {
		return 0, false
	}
	return BeatsAt(c, atMs), true
}

// Sources is every source known, in the order they are tried, with
// Connected saying which of them are there now.
func (r *Registry) Sources() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ranked := r.rankedLocked()
	out := make([]Source, len(ranked))
	for i, id := range ranked {
		_, connected := r.clocks[id]
		label := r.labels[id]
		if label == "" {
			label = id
		}
		out[i] = Source{ID: id, Label: label, Connected: connected}
	}
	return out
}

// Subscribe adds a listener called whenever the selection, the source
// list or the active tempo moves. Listeners run on the goroutine that
// made the change, so keep them short.
func (r *Registry) Subscribe(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, fn)
}

// SubscribeTransport adds a listener called when the transport of the
// source driving the lights moves. It runs on the goroutine that made
// the change, after the Subscribe listeners have heard of it.
func (r *Registry) SubscribeTransport(fn func(TransportEdge)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transports = append(r.transports, fn)
}

func (r *Registry) emitTransport(edge TransportEdge) {
	if edge == "" {
		return
	}
	r.mu.RLock()
	listeners := slices.Clone(r.transports)
	r.mu.RUnlock()
	for _, fn := range listeners {
		fn(edge)
	}
}

func (r *Registry) emit() {
	r.mu.RLock()
	listeners := append([]func(){}, r.listeners...)
	r.mu.RUnlock()
	for _, fn := range listeners {
		fn()
	}
}
