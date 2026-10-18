package link

import (
	"math"
	"net/netip"
	"testing"
	"time"
)

func testSession(t *testing.T) *Session {
	t.Helper()
	s := New(4, nil)
	var at int64
	s.now = func() int64 { return at }
	return s
}

func TestAloneItIsItsOwnSession(t *testing.T) {
	s := testSession(t)
	snap := s.Snapshot()
	if snap.BPM != 120 || snap.Peers != 0 || snap.Playing {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestSnapshotReadsTheTimelineThroughTheOffset(t *testing.T) {
	s := New(4, nil)
	at := int64(0)
	s.now = func() int64 { return at }
	s.current = session{id: NodeID{1}, measured: true, intercept: 1_000_000,
		timeline: Timeline{BPM: 120, BeatOrigin: 0, TimeOrigin: 1_000_000}}
	// The host clock reads zero, the offset puts ghost time at the
	// origin, so the session is on beat zero.
	if got := s.Snapshot().Beat; got != 0 {
		t.Errorf("beat = %v, want 0", got)
	}
	// A second later, at 120 BPM, is two beats on, and half a bar.
	at = 1_000_000
	snap := s.Snapshot()
	if math.Abs(snap.Beat-2) > 1e-6 || math.Abs(snap.Phase-2) > 1e-6 {
		t.Errorf("snapshot = %+v, want beat 2 and phase 2", snap)
	}
	// Three seconds in is beat six, which is phase two of the next bar.
	at = 3_000_000
	if got := s.Snapshot().Phase; math.Abs(got-2) > 1e-6 {
		t.Errorf("phase = %v, want 2", got)
	}
}

func TestAdoptTimelinePrefersTheFurtherOrigin(t *testing.T) {
	sess := &session{timeline: Timeline{BPM: 120, BeatOrigin: 10}}
	// An older timeline is left alone.
	adoptTimeline(sess, Timeline{BPM: 140, BeatOrigin: 5})
	if sess.timeline.BPM != 120 {
		t.Errorf("an older timeline was adopted: %+v", sess.timeline)
	}
	// A newer one is taken.
	adoptTimeline(sess, Timeline{BPM: 140, BeatOrigin: 20})
	if sess.timeline.BPM != 140 || sess.timeline.BeatOrigin != 20 {
		t.Errorf("a newer timeline was rejected: %+v", sess.timeline)
	}
	// One with no tempo at all is never taken.
	adoptTimeline(sess, Timeline{BeatOrigin: 100})
	if sess.timeline.BPM != 140 {
		t.Errorf("a timeline with no tempo was adopted: %+v", sess.timeline)
	}
}

func TestSawTimelineJoinsANewSessionOnlyOnceMeasured(t *testing.T) {
	s := testSession(t)
	peerSession := NodeID{9}
	state := NodeState{NodeID: NodeID{2}, SessionID: peerSession,
		Timeline:    Timeline{BPM: 140, BeatOrigin: 8},
		Measurement: netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 9999)}

	s.mu.Lock()
	s.sawTimelineLocked(state)
	s.mu.Unlock()

	// Until the measurement lands, the session driving is unchanged:
	// joining on an unmeasured clock would jump the beat.
	if got := s.Snapshot().BPM; got != 120 {
		t.Errorf("the tempo changed before the measurement: %v", got)
	}
	s.mu.Lock()
	if _, known := s.others[peerSession]; !known {
		t.Error("the new session was not remembered")
	}
	// A clock a whole second ahead wins outright.
	s.finishMeasurementLocked(peerSession, 1_000_000)
	s.mu.Unlock()
	if got := s.Snapshot().BPM; got != 140 {
		t.Errorf("the winning session was not joined: %v BPM", got)
	}
}

func TestASessionBehindDoesNotWin(t *testing.T) {
	s := testSession(t)
	behind := NodeID{9}
	s.mu.Lock()
	s.current = session{id: NodeID{5}, timeline: Timeline{BPM: 120}, intercept: 0, measured: true}
	s.others[behind] = &session{id: behind, timeline: Timeline{BPM: 140}}
	// A clock a whole second behind loses.
	s.finishMeasurementLocked(behind, -1_000_000)
	s.mu.Unlock()
	if got := s.Snapshot().BPM; got != 120 {
		t.Errorf("a session behind won: %v BPM", got)
	}
}

func TestClocksTooCloseAreDecidedByID(t *testing.T) {
	low, high := NodeID{1}, NodeID{2}
	// The lower id wins when the two clocks are within half a second.
	s := testSession(t)
	s.mu.Lock()
	s.current = session{id: high, timeline: Timeline{BPM: 120}, measured: true}
	s.others[low] = &session{id: low, timeline: Timeline{BPM: 140}}
	s.finishMeasurementLocked(low, 1000) // a millisecond apart
	s.mu.Unlock()
	if got := s.Snapshot().BPM; got != 140 {
		t.Errorf("the lower id did not win: %v BPM", got)
	}

	// And the higher id does not, for the same closeness.
	s = testSession(t)
	s.mu.Lock()
	s.current = session{id: low, timeline: Timeline{BPM: 120}, measured: true}
	s.others[high] = &session{id: high, timeline: Timeline{BPM: 140}}
	s.finishMeasurementLocked(high, 1000)
	s.mu.Unlock()
	if got := s.Snapshot().BPM; got != 120 {
		t.Errorf("the higher id won: %v BPM", got)
	}
}

func TestPruneForgetsSilentPeers(t *testing.T) {
	s := testSession(t)
	id := NodeID{7}
	s.mu.Lock()
	s.peers[id] = &peer{state: NodeState{NodeID: id, SessionID: NodeID{9}},
		expires: time.Now().Add(-time.Second)}
	s.current = session{id: NodeID{9}, timeline: Timeline{BPM: 140}, measured: true}
	s.mu.Unlock()

	s.prune()
	if got := s.Snapshot().Peers; got != 0 {
		t.Errorf("%d peers left after they went quiet", got)
	}
	// With nobody left in it, the session it was following is dropped
	// and this node is its own again, keeping the tempo it had.
	s.mu.Lock()
	own := s.current.id == s.nodeID
	s.mu.Unlock()
	if !own {
		t.Error("the empty session was still being followed")
	}
	if got := s.Snapshot().BPM; got != 140 {
		t.Errorf("the tempo jumped when the peer went: %v", got)
	}
}

func TestTransportComesFromTheNewestPeerOfTheSession(t *testing.T) {
	s := testSession(t)
	sessionID := NodeID{9}
	s.mu.Lock()
	s.current = session{id: sessionID, timeline: Timeline{BPM: 120}, measured: true}
	s.peers[NodeID{1}] = &peer{state: NodeState{SessionID: sessionID,
		StartStop: StartStop{Playing: true, TimeOrigin: 100}}, expires: time.Now().Add(time.Minute)}
	s.peers[NodeID{2}] = &peer{state: NodeState{SessionID: sessionID,
		StartStop: StartStop{Playing: false, TimeOrigin: 200}}, expires: time.Now().Add(time.Minute)}
	// A peer of another session has no say.
	s.peers[NodeID{3}] = &peer{state: NodeState{SessionID: NodeID{8},
		StartStop: StartStop{Playing: true, TimeOrigin: 300}}, expires: time.Now().Add(time.Minute)}
	s.mu.Unlock()

	if s.Snapshot().Playing {
		t.Error("the older transport won")
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{nil, 0},
		{[]float64{5}, 5},
		{[]float64{3, 1, 2}, 2},
		{[]float64{4, 1, 3, 2}, 2.5},
		// One wildly late round trip does not move the answer.
		{[]float64{10, 11, 12, 13, 9999}, 12},
	}
	for _, c := range cases {
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMedianLeavesTheCallersSliceAlone(t *testing.T) {
	in := []float64{3, 1, 2}
	median(in)
	if in[0] != 3 {
		t.Errorf("the samples were sorted under the caller: %v", in)
	}
}

func TestSetQuantum(t *testing.T) {
	s := testSession(t)
	s.SetQuantum(8)
	if got := s.Snapshot().Quantum; got != 8 {
		t.Errorf("quantum = %v, want 8", got)
	}
	// A quantum of nothing would divide the bar by nothing.
	s.SetQuantum(0)
	if got := s.Snapshot().Quantum; got != 8 {
		t.Errorf("quantum = %v, want it unchanged at 8", got)
	}
}

// TestSettingTheTempoKeepsTheBeat: a tempo proposed mid-bar carries the
// beat on from where it was rather than jumping the grid.
func TestSettingTheTempoKeepsTheBeat(t *testing.T) {
	s := New(4, nil)
	at := int64(0)
	s.now = func() int64 { return at }
	s.current = session{id: s.nodeID, measured: true,
		timeline: Timeline{BPM: 120, BeatOrigin: 0, TimeOrigin: 0}}

	// A second in at 120 is beat two.
	at = 1_000_000
	if got := s.Snapshot().Beat; math.Abs(got-2) > 1e-6 {
		t.Fatalf("beat = %v before the change, want 2", got)
	}

	s.SetTempo(60)
	snap := s.Snapshot()
	if snap.BPM != 60 {
		t.Errorf("bpm = %v, want 60", snap.BPM)
	}
	if math.Abs(snap.Beat-2) > 1e-6 {
		t.Errorf("beat = %v at the moment of the change, want it kept at 2", snap.Beat)
	}
	// A second later at 60 is one beat on.
	at = 2_000_000
	if got := s.Snapshot().Beat; math.Abs(got-3) > 1e-6 {
		t.Errorf("beat = %v a second after the change, want 3", got)
	}

	// The proposal outranks the timeline it replaced, which is what a
	// peer judges it by.
	var sess session
	adoptTimeline(&sess, s.current.timeline)
	if sess.timeline.BPM != 60 {
		t.Error("a peer would not take the proposed timeline")
	}
	// A tempo of nothing is not a proposal.
	s.SetTempo(0)
	if s.Snapshot().BPM != 60 {
		t.Error("a tempo of zero was taken")
	}
}

// TestSettingTheTransportIsPublishedLikeAPeersIs: a transport set here
// goes out as a peer's would.
func TestSettingTheTransportIsPublishedLikeAPeersIs(t *testing.T) {
	s := New(4, nil)
	at := int64(1_000_000)
	s.now = func() int64 { return at }
	s.current = session{id: s.nodeID, measured: true, timeline: Timeline{BPM: 120}}

	if s.Snapshot().Playing {
		t.Fatal("a session nobody started reads as playing")
	}
	s.SetPlaying(true)
	if !s.Snapshot().Playing {
		t.Error("the session did not start")
	}
	if got := s.state().StartStop; !got.Playing || got.TimeOrigin != at {
		t.Errorf("this node publishes %+v, want it playing from now", got)
	}

	// A peer that says otherwise more recently wins, as Link ranks them.
	at = 2_000_000
	s.peers[NodeID{9}] = &peer{state: NodeState{NodeID: NodeID{9}, SessionID: s.current.id,
		StartStop: StartStop{Playing: false, TimeOrigin: at}}}
	if s.Snapshot().Playing {
		t.Error("a peer's later stop was not taken")
	}

	// And this node saying so again later wins it back.
	at = 3_000_000
	s.SetPlaying(true)
	if !s.Snapshot().Playing {
		t.Error("this node's later start was not taken")
	}
}

// TestASessionWhoseMeasurementRanOutIsMeasuredAgain: a measurement
// that times out is dropped. The session must be measured again the
// next time a peer in it is heard, or it could never be joined.
func TestASessionWhoseMeasurementRanOutIsMeasuredAgain(t *testing.T) {
	s := testSession(t)
	peerSession := NodeID{9}
	state := NodeState{NodeID: NodeID{2}, SessionID: peerSession,
		Timeline:    Timeline{BPM: 140, BeatOrigin: 8},
		Measurement: netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 9999)}

	s.mu.Lock()
	s.sawTimelineLocked(state)
	s.pending[peerSession].deadline = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.nudgeMeasurements()

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, running := s.pending[peerSession]; running {
		t.Fatal("a measurement past its deadline is still running")
	}
	s.sawTimelineLocked(state)
	if _, running := s.pending[peerSession]; !running {
		t.Error("the session was heard again and not measured")
	}
}

// TestTwoProgramsShareTheDiscoveryPort: Ableton Live and crossroads on the
// one machine both listen on the discovery port. The second to start
// must still be able to.
func TestTwoProgramsShareTheDiscoveryPort(t *testing.T) {
	first, _, err := joinDiscoveryGroup()
	if err != nil {
		t.Skip("no discovery socket here: " + err.Error())
	}
	defer func() { _ = first.Close() }()
	second, _, err := joinDiscoveryGroup()
	if err != nil {
		t.Fatal("the second listener on the discovery port failed: ", err)
	}
	_ = second.Close()
}
