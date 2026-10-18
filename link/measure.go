package link

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"time"
)

// A measurement is one run of pings against a peer, gathering samples
// of how far its clock sits from this machine's.
//
// The exchange comes from Ableton's implementation. Each ping carries
// the host time it left at, and the ghost time of the reply before it.
// Each pong carries the responder's session, its ghost time now, and
// the ping's own payload echoed back. Two samples fall out of each
// round trip:
//
//	ghost - (host + prevHost) / 2
//	(ghost + prevGhost) / 2 - prevHost
//
// Both estimate the same offset from different ends of the trip, so
// taking the median of a hundred of them shrugs off the ones that were
// delayed.
type measurement struct {
	sessionID NodeID
	endpoint  netip.AddrPort
	samples   []float64
	// prevGhost and prevHost are the previous reply's ghost time and
	// the host time of the ping that drew it.
	prevGhost, prevHost int64
	sent                int
	deadline            time.Time
	done                bool
}

// startMeasurementLocked begins measuring the session a peer belongs
// to, if it is not already being measured.
func (s *Session) startMeasurementLocked(state NodeState) {
	if !state.Measurement.IsValid() || state.SessionID == s.nodeID {
		return
	}
	if m, running := s.pending[state.SessionID]; running && !m.done {
		return
	}
	m := &measurement{
		sessionID: state.SessionID,
		endpoint:  state.Measurement,
		deadline:  time.Now().Add(pingEvery * (pingsPerMeasurement + 1)),
	}
	s.pending[state.SessionID] = m
	s.sendPingLocked(m)
}

// sendPingLocked puts one ping on the wire.
func (s *Session) sendPingLocked(m *measurement) {
	if s.ping == nil || m.sent >= pingsPerMeasurement+samplesWanted {
		return
	}
	host := s.now()
	data := EncodePing(host, m.prevGhost, m.prevGhost != 0)
	_, _ = s.ping.WriteToUDP(data, &net.UDPAddr{
		IP:   m.endpoint.Addr().AsSlice(),
		Port: int(m.endpoint.Port()),
	})
	m.sent++
	m.prevHost = host
}

// readMeasurement answers other nodes' pings and takes in the replies
// to this node's own.
func (s *Session) readMeasurement(ctx context.Context) {
	buf := make([]byte, MaxMessageSize)
	for {
		n, from, err := s.ping.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		msgType, ping, pong, err := DecodeMeasurement(buf[:n])
		if err != nil {
			continue
		}
		switch msgType {
		case MsgPing:
			s.reply(ping, from)
		case MsgPong:
			s.takeSample(pong)
		}
	}
}

// reply answers another node's ping with this node's ghost time, so it
// can work out where this machine's clock sits.
func (s *Session) reply(ping Ping, to *net.UDPAddr) {
	s.mu.Lock()
	sessionID := s.current.id
	ghost := s.now() + s.current.intercept
	conn := s.ping
	s.mu.Unlock()
	if conn == nil {
		return
	}
	_, _ = conn.WriteToUDP(EncodePong(sessionID, ghost, ping.Payload), to)
}

// takeSample folds one reply into the measurement that asked for it,
// and settles the measurement once it has enough.
func (s *Session) takeSample(pong Pong) {
	s.mu.Lock()
	m, running := s.pending[pong.SessionID]
	if !running || m.done {
		s.mu.Unlock()
		return
	}
	host := s.now()
	if pong.GhostMicros != 0 && pong.HostMicros != 0 {
		m.samples = append(m.samples,
			float64(pong.GhostMicros)-float64(host+pong.HostMicros)*0.5)
		if pong.PrevGhostMicros != 0 {
			m.samples = append(m.samples,
				float64(pong.GhostMicros+pong.PrevGhostMicros)*0.5-float64(pong.HostMicros))
		}
	}
	m.prevGhost = pong.GhostMicros

	if len(m.samples) > samplesWanted {
		m.done = true
		intercept := int64(median(m.samples))
		s.finishMeasurementLocked(pong.SessionID, intercept)
		s.mu.Unlock()
		s.changed()
		return
	}
	s.sendPingLocked(m)
	s.mu.Unlock()
}

// finishMeasurementLocked records a session's offset and decides
// whether to switch to it.
//
// Link's rule: the session whose clock reads later wins, and where the
// two are within half a second of each other the lower session id wins
// instead, so every node reaches the same answer.
func (s *Session) finishMeasurementLocked(id NodeID, intercept int64) {
	delete(s.pending, id)
	if id == s.current.id {
		s.current.intercept = intercept
		s.current.measured = true
		s.current.measuredAt = time.Now()
		return
	}
	candidate, known := s.others[id]
	if !known {
		return
	}
	candidate.intercept = intercept
	candidate.measured = true
	candidate.measuredAt = time.Now()

	// The slope is always one, so the difference between two sessions'
	// clocks is just the difference between their offsets.
	diff := candidate.intercept - s.current.intercept
	eps := int64(sessionEps / time.Microsecond)
	wins := diff > eps || (abs64(diff) < eps && id.Less(s.current.id))
	if !wins {
		return
	}
	s.logf("joining session %v at %.2f BPM", id, candidate.timeline.BPM)
	previous := s.current
	s.current = *candidate
	delete(s.others, id)
	// Keep the session we left, so it is not measured again from scratch.
	if previous.id != s.nodeID {
		s.others[previous.id] = &previous
	}
}

// nudgeMeasurements sends another ping to every run still waiting, and
// drops the ones whose peer has stopped answering, so a peer that
// appears again is measured afresh.
//
// A measurement is normally driven by its own replies; this is what
// keeps one going when a reply is lost.
func (s *Session) nudgeMeasurements() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, m := range s.pending {
		if m.done || now.After(m.deadline) {
			delete(s.pending, id)
			continue
		}
		s.sendPingLocked(m)
	}
}

// median is the middle of the samples. It sorts a copy, so the caller's
// slice keeps the order the samples arrived in.
func median(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
