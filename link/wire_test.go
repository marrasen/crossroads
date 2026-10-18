package link

import (
	"bytes"
	"encoding/binary"
	"math"
	"net/netip"
	"testing"
)

func sampleState() NodeState {
	return NodeState{
		NodeID:      NodeID{1, 2, 3, 4, 5, 6, 7, 8},
		SessionID:   NodeID{9, 10, 11, 12, 13, 14, 15, 16},
		Timeline:    Timeline{BPM: 128, BeatOrigin: 12.5, TimeOrigin: 987654321},
		StartStop:   StartStop{Playing: true, Beats: 4, TimeOrigin: 123456789},
		Measurement: netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 168, 1, 40}), 40123),
	}
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

func TestDiscoveryRoundTrip(t *testing.T) {
	want := sampleState()
	m, err := DecodeDiscovery(EncodeDiscovery(MsgAlive, 5, want))
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != MsgAlive || m.TTLSeconds != 5 {
		t.Errorf("header = %+v", m)
	}
	got := m.State
	if got.NodeID != want.NodeID || got.SessionID != want.SessionID {
		t.Errorf("ids = %v, %v", got.NodeID, got.SessionID)
	}
	if !near(got.Timeline.BPM, want.Timeline.BPM) || !near(got.Timeline.BeatOrigin, want.Timeline.BeatOrigin) ||
		got.Timeline.TimeOrigin != want.Timeline.TimeOrigin {
		t.Errorf("timeline = %+v, want %+v", got.Timeline, want.Timeline)
	}
	if got.StartStop != want.StartStop {
		t.Errorf("transport = %+v, want %+v", got.StartStop, want.StartStop)
	}
	if got.Measurement != want.Measurement {
		t.Errorf("endpoint = %v, want %v", got.Measurement, want.Measurement)
	}
}

func TestDiscoveryHeaderIsTheOneLinkSends(t *testing.T) {
	data := EncodeDiscovery(MsgAlive, 5, sampleState())
	if !bytes.Equal(data[:8], []byte{'_', 'a', 's', 'd', 'p', '_', 'v', 1}) {
		t.Errorf("header = %q", data[:8])
	}
	// The byte after the header is the message type, then the time to
	// live, then a group id Link always leaves at zero.
	if data[8] != MsgAlive || data[9] != 5 || binary.BigEndian.Uint16(data[10:]) != 0 {
		t.Errorf("header fields = %x", data[8:12])
	}
	// Then the node id, unchanged.
	if !bytes.Equal(data[12:20], []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("node id = %x", data[12:20])
	}
}

func TestByeByeCarriesNoPayload(t *testing.T) {
	data := EncodeDiscovery(MsgByeBye, 0, sampleState())
	if len(data) != 20 {
		t.Errorf("a bye-bye is %d bytes, want 20", len(data))
	}
	m, err := DecodeDiscovery(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != MsgByeBye || m.State.NodeID != sampleState().NodeID {
		t.Errorf("message = %+v", m)
	}
}

func TestTempoSurvivesTheWire(t *testing.T) {
	// Tempo travels as whole microseconds per beat, so it comes back
	// within a rounding of itself rather than exactly.
	for _, bpm := range []float64{20, 60, 120, 128.5, 174, 300, 999} {
		state := sampleState()
		state.Timeline.BPM = bpm
		m, err := DecodeDiscovery(EncodeDiscovery(MsgAlive, 5, state))
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(m.State.Timeline.BPM-bpm) > 0.002 {
			t.Errorf("%v BPM came back as %v", bpm, m.State.Timeline.BPM)
		}
	}
}

func TestDecodeRejectsAnotherProtocol(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		[]byte("not a link message at all"),
		append([]byte("_asdp_v"), 1), // the header alone, with no fields
	} {
		if _, err := DecodeDiscovery(data); err == nil {
			t.Errorf("%q was taken for a discovery message", data)
		}
	}
}

func TestDecodeRejectsAnOversizedEntry(t *testing.T) {
	// A payload entry claiming more bytes than the message holds must
	// not be read past the end.
	data := EncodeDiscovery(MsgAlive, 5, sampleState())
	binary.BigEndian.PutUint32(data[24:], 0xffffffff) // the first entry's size
	if _, err := DecodeDiscovery(data); err == nil {
		t.Error("an entry claiming the whole address space was accepted")
	}
}

func TestDecodeIgnoresAnEntryItDoesNotKnow(t *testing.T) {
	// Link adds entries over time — audio endpoints, IPv6 — and a node
	// that does not know one has to walk past it.
	state := sampleState()
	data := EncodeDiscovery(MsgAlive, 5, state)
	var extra writer
	extra.entry(0x61657034, []byte{1, 2, 3, 4, 5, 6}) // "aep4"
	m, err := DecodeDiscovery(append(data, extra.buf...))
	if err != nil {
		t.Fatal(err)
	}
	if m.State.SessionID != state.SessionID {
		t.Errorf("the entries before an unknown one were lost: %+v", m.State)
	}
}

func TestNoEndpointEntryWithoutAnAddress(t *testing.T) {
	state := sampleState()
	state.Measurement = netip.AddrPort{}
	m, err := DecodeDiscovery(EncodeDiscovery(MsgAlive, 5, state))
	if err != nil {
		t.Fatal(err)
	}
	if m.State.Measurement.IsValid() {
		t.Errorf("an endpoint appeared from nowhere: %v", m.State.Measurement)
	}
}

func TestPingPongRoundTrip(t *testing.T) {
	ping := EncodePing(111111, 222222, true)
	msgType, got, _, err := DecodeMeasurement(ping)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != MsgPing || got.HostMicros != 111111 || got.PrevGhostMicros != 222222 {
		t.Errorf("ping = %d, %+v", msgType, got)
	}

	session := NodeID{9, 9, 9, 9, 9, 9, 9, 9}
	pong := EncodePong(session, 333333, got.Payload)
	msgType, _, reply, err := DecodeMeasurement(pong)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != MsgPong || reply.SessionID != session || reply.GhostMicros != 333333 {
		t.Errorf("pong = %d, %+v", msgType, reply)
	}
	// The ping's own payload comes back, which is what ties the reply to
	// the ping that caused it.
	if reply.HostMicros != 111111 || reply.PrevGhostMicros != 222222 {
		t.Errorf("the echoed payload = %+v", reply)
	}
}

func TestPingWithoutAPreviousGhostTime(t *testing.T) {
	_, got, _, err := DecodeMeasurement(EncodePing(111111, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	if got.HostMicros != 111111 || got.PrevGhostMicros != 0 {
		t.Errorf("ping = %+v", got)
	}
}

func TestMeasurementHeaderIsTheOneLinkSends(t *testing.T) {
	data := EncodePing(1, 0, false)
	if !bytes.Equal(data[:8], []byte{'_', 'l', 'i', 'n', 'k', '_', 'v', 1}) {
		t.Errorf("header = %q", data[:8])
	}
	if data[8] != MsgPing {
		t.Errorf("type = %d, want %d", data[8], MsgPing)
	}
}

func TestTimelineBeatsAt(t *testing.T) {
	// 120 BPM is half a second a beat, so a second past the origin is
	// two beats on.
	tl := Timeline{BPM: 120, BeatOrigin: 4, TimeOrigin: 1_000_000}
	if got := tl.BeatsAt(2_000_000); !near(got, 6) {
		t.Errorf("BeatsAt = %v, want 6", got)
	}
	// A timeline with no tempo holds its origin rather than dividing by
	// nothing.
	if got := (Timeline{BeatOrigin: 3}).BeatsAt(9e9); got != 3 {
		t.Errorf("a tempo-less timeline read %v, want 3", got)
	}
}

func TestNodeIDLess(t *testing.T) {
	a := NodeID{1, 0, 0, 0, 0, 0, 0, 0}
	b := NodeID{1, 0, 0, 0, 0, 0, 0, 1}
	if !a.Less(b) || b.Less(a) || a.Less(a) {
		t.Error("node ids do not order consistently")
	}
}
