package link

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/netip"
	"strconv"

	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// Timing constants taken from Ableton's implementation.
const (
	// pingEvery is how often a measurement sends another ping while it
	// gathers samples.
	pingEvery = 50 * time.Millisecond
	// pingsPerMeasurement is how many pings one measurement sends
	// before giving up on a peer that is not answering.
	pingsPerMeasurement = 5
	// samplesWanted is how many round-trip samples a measurement
	// gathers before it settles on an answer.
	samplesWanted = 100
	// remeasureEvery is how often the session that is driving is
	// measured again, so the two clocks stay together as they drift.
	remeasureEvery = 30 * time.Second
	// sessionEps is how close two sessions' clocks have to be before
	// the tie is broken by session id instead.
	sessionEps = 500 * time.Millisecond
)

// A Snapshot is one reading of the session.
type Snapshot struct {
	// BPM is the session tempo.
	BPM float64
	// Beat is the continuous beat count on the session timeline.
	Beat float64
	// Phase is the position within the quantum, from zero up to it.
	Phase float64
	// Peers is how many other nodes are in the session.
	Peers int
	// Playing is the session transport. It only means anything with
	// start and stop sync, which many peers never publish.
	Playing bool
	// Quantum is the beats per bar Beat and Phase were read with.
	Quantum float64
	// Measured says the session's clock has been tied to this
	// machine's. Until it has, Beat is as good as the two clocks happen
	// to agree, which is not very.
	Measured bool
}

// A peer is another node heard on the network.
type peer struct {
	state   NodeState
	expires time.Time
}

// A session is one Link session this node knows about.
type session struct {
	id       NodeID
	timeline Timeline
	// intercept is the ghost transform's offset in microseconds. Link's
	// slope is always one, so the transform is just an offset.
	intercept  int64
	measured   bool
	measuredAt time.Time
}

// A Session is crossroads' place in an Ableton Link session.
//
// It joins the discovery group, follows whichever session wins, ties
// that session's clock to this machine's with ping and pong
// measurements, and answers other nodes' pings so they can do the same.
//
// It never proposes a tempo. A rig where Live or Serato holds the
// timeline keeps it, and crossroads falls in behind.
type Session struct {
	mu       sync.Mutex
	nodeID   NodeID
	current  session
	others   map[NodeID]*session
	peers    map[NodeID]*peer
	quantum  float64
	pending  map[NodeID]*measurement
	measured netip.AddrPort
	// startStop is this node's own transport, published like any
	// peer's. It stays at its zero value until something drives it, so
	// a session crossroads only follows hears nothing from it.
	startStop StartStop

	discovery *net.UDPConn
	packet    *ipv4.PacketConn
	ping      *net.UDPConn

	cancel context.CancelFunc
	wg     sync.WaitGroup
	// now is the machine's monotonic clock in microseconds. Tests
	// replace it.
	now func() int64
	// onChange is called whenever the tempo, transport or peer count
	// moves, so the application can push a reading on without polling.
	onChange func()
}

var epoch = time.Now()

func nowMicros() int64 { return int64(time.Since(epoch) / time.Microsecond) }

// New makes a session that has not joined the network yet.
//
// quantum is the beats per bar the reported beat and phase are
// measured against. onChange, when set, is called on the session's own
// goroutine whenever something worth redrawing moves.
func New(quantum float64, onChange func()) *Session {
	var id NodeID
	rand.Read(id[:])
	if quantum <= 0 {
		quantum = 4
	}
	s := &Session{
		nodeID:   id,
		others:   map[NodeID]*session{},
		peers:    map[NodeID]*peer{},
		pending:  map[NodeID]*measurement{},
		quantum:  quantum,
		now:      nowMicros,
		onChange: onChange,
	}
	// Alone, this node is its own session at a resting tempo. The
	// moment a real one is heard, that one wins.
	s.current = session{id: id, timeline: Timeline{BPM: 120}, measured: true}
	return s
}

// SetTempo proposes a tempo to the session, as any Link peer may.
//
// The beat is kept where it is and carried on from there at the new
// tempo, so a change mid-bar does not jump the grid. The proposal is
// ranked by its beat origin, exactly as a peer's would be, so it wins
// by being the newest rather than by coming from here.
//
// crossroads follows unless something calls this: a rig where Live or
// Serato holds the timeline keeps it.
func (s *Session) SetTempo(bpm float64) {
	if bpm <= 0 {
		return
	}
	s.mu.Lock()
	ghost := s.now() + s.current.intercept
	s.current.timeline = Timeline{
		BPM:        bpm,
		BeatOrigin: s.current.timeline.BeatsAt(ghost),
		TimeOrigin: ghost,
	}
	s.mu.Unlock()
	s.broadcast(MsgAlive)
	s.changed()
}

// SetPlaying starts or stops the session's transport for every peer
// that follows it, as a Link peer with start and stop sync does.
//
// The beat it changed at goes with it, which is what lets a peer that
// joins late know where the transport began.
func (s *Session) SetPlaying(playing bool) {
	s.mu.Lock()
	ghost := s.now() + s.current.intercept
	s.startStop = StartStop{
		Playing:    playing,
		Beats:      s.current.timeline.BeatsAt(ghost),
		TimeOrigin: ghost,
	}
	s.mu.Unlock()
	s.broadcast(MsgAlive)
	s.changed()
}

// SetQuantum sets the beats per bar the reported beat and phase are
// measured against.
func (s *Session) SetQuantum(quantum float64) {
	if quantum <= 0 {
		return
	}
	s.mu.Lock()
	s.quantum = quantum
	s.mu.Unlock()
}

// Start joins the network. It returns once the sockets are up;
// everything after that runs on goroutines of its own.
func (s *Session) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.discovery != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	local, err := localIPv4()
	if err != nil {
		return err
	}
	ping, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
	if err != nil {
		return fmt.Errorf("link: opening the measurement socket: %w", err)
	}
	discovery, packet, err := joinDiscoveryGroup()
	if err != nil {
		_ = ping.Close()
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.discovery, s.packet, s.ping, s.cancel = discovery, packet, ping, cancel
	addr, _ := netip.AddrFromSlice(local.To4())
	if at, ok := ping.LocalAddr().(*net.UDPAddr); ok {
		s.measured = netip.AddrPortFrom(addr, uint16(at.Port))
	}
	s.mu.Unlock()

	s.wg.Go(func() { s.readDiscovery(ctx) })
	s.wg.Go(func() { s.readMeasurement(ctx) })
	s.wg.Go(func() { s.heartbeat(ctx) })
	s.wg.Go(func() { s.housekeeping(ctx) })

	// Announce at once, and ask everyone else to say who they are.
	s.broadcast(MsgAlive)
	return nil
}

// Stop leaves the session, telling peers so they drop this node at once
// rather than waiting out its time to live.
func (s *Session) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	discovery, ping := s.discovery, s.ping
	s.cancel = nil
	s.mu.Unlock()
	if discovery == nil {
		return
	}
	s.broadcast(MsgByeBye)
	if cancel != nil {
		cancel()
	}
	_ = discovery.Close()
	_ = ping.Close()
	s.wg.Wait()

	s.mu.Lock()
	s.discovery, s.packet, s.ping = nil, nil, nil
	s.peers = map[NodeID]*peer{}
	s.others = map[NodeID]*session{}
	s.pending = map[NodeID]*measurement{}
	s.current = session{id: s.nodeID, timeline: Timeline{BPM: 120}, measured: true}
	s.mu.Unlock()
}

// Snapshot is the session as it reads now.
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(s.now())
}

func (s *Session) snapshotLocked(hostMicros int64) Snapshot {
	ghost := hostMicros + s.current.intercept
	beat := s.current.timeline.BeatsAt(ghost)
	snap := Snapshot{
		BPM:      s.current.timeline.BPM,
		Beat:     beat,
		Quantum:  s.quantum,
		Peers:    len(s.peers),
		Playing:  s.startStopLocked(),
		Measured: s.current.measured,
	}
	if s.quantum > 0 {
		snap.Phase = math.Mod(math.Mod(beat, s.quantum)+s.quantum, s.quantum)
	}
	return snap
}

// startStopLocked is the session transport, taken from whichever node
// of the session published one most recently — this one included,
// since crossroads may be the one driving.
func (s *Session) startStopLocked() bool {
	best, have := s.startStop, s.startStop.TimeOrigin > 0
	for _, p := range s.peers {
		if p.state.SessionID != s.current.id {
			continue
		}
		if !have || p.state.StartStop.TimeOrigin > best.TimeOrigin {
			best, have = p.state.StartStop, true
		}
	}
	return have && best.Playing
}

// ---- discovery ----

// joinDiscoveryGroup opens the multicast socket and joins the group on
// every interface that can carry it.
func joinDiscoveryGroup() (*net.UDPConn, *ipv4.PacketConn, error) {
	lc := net.ListenConfig{Control: shareAddress}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":"+strconv.Itoa(DiscoveryPort))
	if err != nil {
		return nil, nil, fmt.Errorf("link: opening the discovery socket: %w", err)
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, nil, errors.New("link: the discovery socket is not UDP")
	}
	packet := ipv4.NewPacketConn(conn)
	group := DiscoveryGroup()
	interfaces, err := net.Interfaces()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("link: listing interfaces: %w", err)
	}
	joined := 0
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		if err := packet.JoinGroup(&iface, group); err == nil {
			joined++
		}
	}
	if joined == 0 {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("link: no network interface could join %s", DiscoveryAddr)
	}
	// Peers on this machine are peers too, so keep our own multicast
	// looping back; a message from our own node id is ignored anyway.
	_ = packet.SetMulticastLoopback(true)
	_ = packet.SetMulticastTTL(2)
	return conn, packet, nil
}

// localIPv4 is the address to publish as this node's measurement
// endpoint: the first address of an interface that is up, carries
// multicast, and is not loopback.
func localIPv4() (net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("link: listing interfaces: %w", err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 ||
			iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if ok && n.IP.To4() != nil {
				return n.IP.To4(), nil
			}
		}
	}
	return nil, errors.New("link: this machine has no IPv4 address on a network that carries multicast")
}

// state is what this node says about itself.
func (s *Session) state() NodeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return NodeState{
		NodeID:      s.nodeID,
		SessionID:   s.current.id,
		Timeline:    s.current.timeline,
		StartStop:   s.startStop,
		Measurement: s.measured,
	}
}

func (s *Session) broadcast(msgType byte) {
	s.mu.Lock()
	conn := s.discovery
	s.mu.Unlock()
	if conn == nil {
		return
	}
	_, _ = conn.WriteToUDP(EncodeDiscovery(msgType, int(TTL/time.Second), s.state()), DiscoveryGroup())
}

func (s *Session) heartbeat(ctx context.Context) {
	t := time.NewTicker(Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.broadcast(MsgAlive)
		}
	}
}

func (s *Session) readDiscovery(ctx context.Context) {
	buf := make([]byte, MaxMessageSize)
	for {
		n, from, err := s.discovery.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		m, err := DecodeDiscovery(buf[:n])
		if err != nil || m.State.NodeID == s.nodeID {
			continue
		}
		s.handlePeer(m, from)
	}
}

func (s *Session) handlePeer(m Message, from *net.UDPAddr) {
	if m.Type == MsgByeBye {
		s.mu.Lock()
		delete(s.peers, m.State.NodeID)
		s.mu.Unlock()
		s.changed()
		return
	}
	if m.Type != MsgAlive && m.Type != MsgResponse {
		return
	}
	// A node that has just arrived asks everyone to say who they are;
	// answering directly keeps the group quiet.
	if m.Type == MsgAlive {
		s.mu.Lock()
		conn := s.discovery
		s.mu.Unlock()
		if conn != nil {
			_, _ = conn.WriteToUDP(EncodeDiscovery(MsgResponse, int(TTL/time.Second), s.state()), from)
		}
	}

	ttl := time.Duration(m.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = TTL
	}
	s.mu.Lock()
	s.peers[m.State.NodeID] = &peer{state: m.State, expires: time.Now().Add(ttl)}
	s.sawTimelineLocked(m.State)
	s.mu.Unlock()
	s.changed()
}

// sawTimelineLocked folds a peer's session and timeline in.
//
// A session already known takes the newer timeline, judged by beat
// origin exactly as Link judges it. A session never seen before is
// measured, and joined once the measurement says its clock is ahead of
// the one driving now.
func (s *Session) sawTimelineLocked(state NodeState) {
	if state.SessionID == s.current.id {
		adoptTimeline(&s.current, state.Timeline)
		return
	}
	known, seen := s.others[state.SessionID]
	if seen {
		adoptTimeline(known, state.Timeline)
		// A measurement that ran out of time, or never had an endpoint
		// to ping, is tried again, or the session could never be
		// joined.
		if !known.measured {
			s.startMeasurementLocked(state)
		}
		return
	}
	s.others[state.SessionID] = &session{id: state.SessionID, timeline: state.Timeline}
	s.startMeasurementLocked(state)
}

// adoptTimeline takes a peer's timeline when it is further on than the
// one held. Beat origin is what Link uses to rank them.
func adoptTimeline(sess *session, timeline Timeline) {
	if timeline.BPM > 0 && timeline.BeatOrigin > sess.timeline.BeatOrigin {
		sess.timeline = timeline
	}
}

// housekeeping drops peers that have stopped broadcasting, and measures
// the driving session again now and then so the clocks stay together.
func (s *Session) housekeeping(ctx context.Context) {
	prune := time.NewTicker(time.Second)
	defer prune.Stop()
	remeasure := time.NewTicker(remeasureEvery)
	defer remeasure.Stop()
	nudge := time.NewTicker(pingEvery)
	defer nudge.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			s.prune()
		case <-remeasure.C:
			s.remeasure()
		case <-nudge.C:
			s.nudgeMeasurements()
		}
	}
}

func (s *Session) prune() {
	now := time.Now()
	s.mu.Lock()
	var gone []NodeID
	for id, p := range s.peers {
		if now.After(p.expires) {
			gone = append(gone, id)
		}
	}
	for _, id := range gone {
		delete(s.peers, id)
	}
	// A session nobody is left in is forgotten, and this node falls
	// back to being its own session.
	for id := range s.others {
		if !s.anyPeerInLocked(id) {
			delete(s.others, id)
		}
	}
	if s.current.id != s.nodeID && !s.anyPeerInLocked(s.current.id) {
		s.current = session{id: s.nodeID, timeline: s.current.timeline, measured: true}
	}
	s.mu.Unlock()
	if len(gone) > 0 {
		s.changed()
	}
}

func (s *Session) anyPeerInLocked(id NodeID) bool {
	for _, p := range s.peers {
		if p.state.SessionID == id {
			return true
		}
	}
	return false
}

func (s *Session) remeasure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current.id == s.nodeID {
		return
	}
	for _, p := range s.peers {
		if p.state.SessionID == s.current.id {
			s.startMeasurementLocked(p.state)
			return
		}
	}
}

func (s *Session) changed() {
	s.mu.Lock()
	fn := s.onChange
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *Session) logf(format string, args ...any) {
	log.Printf("[link] "+format, args...)
}
