// Package link speaks Ableton Link, so crossroads can take its tempo from
// whatever else on the network is playing.
//
// Link is two protocols over UDP. Discovery runs on the multicast group
// 224.76.78.75, port 20808: every node broadcasts what it is and what
// timeline it holds, several times a second. Measurement runs between
// two nodes on unicast ports: pings and pongs whose round trips say how
// far apart the two machines' clocks are, which is what lets a beat
// mean the same instant on both.
//
// The wire format here follows Ableton's own implementation, at
// github.com/Ableton/link, which is the only specification there is.
// Everything is big-endian.
//
// crossroads only ever follows a session. It never proposes a tempo and
// never founds a session of its own while another exists, so a rig
// running Live or Serato keeps its own timeline and crossroads falls in
// behind it.
//
// It came from midittt, which crossroads takes over from.
package link

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"time"
)

// The discovery group crossroads joins. The last three octets of the
// address spell LNK.
var (
	DiscoveryAddr = netip.MustParseAddr("224.76.78.75")
	DiscoveryPort = 20808
)

// MaxMessageSize is the largest message either protocol sends.
const MaxMessageSize = 512

// TTL is how long a node's broadcast stands before peers drop it, and
// TTLRatio is how many times within that a node re-broadcasts. Together
// they set the 250 ms heartbeat Link uses.
const (
	TTL      = 5 * time.Second
	TTLRatio = 20
)

// Heartbeat is how often a node broadcasts that it is alive.
const Heartbeat = TTL / TTLRatio

// The discovery protocol's header and message types.
var discoveryHeader = [8]byte{'_', 'a', 's', 'd', 'p', '_', 'v', 1}

// The kinds of discovery message.
const (
	MsgInvalid  byte = 0
	MsgAlive    byte = 1
	MsgResponse byte = 2
	MsgByeBye   byte = 3
)

// The measurement protocol's header and message types.
var measurementHeader = [8]byte{'_', 'l', 'i', 'n', 'k', '_', 'v', 1}

// The kinds of measurement message.
const (
	MsgPing byte = 1
	MsgPong byte = 2
)

// The payload entry keys, each four ASCII bytes read as a big-endian
// number.
const (
	keyTimeline      uint32 = 0x746d6c6e // "tmln"
	keySession       uint32 = 0x73657373 // "sess"
	keyStartStop     uint32 = 0x73747374 // "stst"
	keyMeasurementV4 uint32 = 0x6d657034 // "mep4"
	keyHostTime      uint32 = 0x5f5f6874 // "__ht"
	keyGhostTime     uint32 = 0x5f5f6774 // "__gt"
	keyPrevGhostTime uint32 = 0x5f706774 // "_pgt"
)

// A NodeID names one node, and a session by the node that founded it.
type NodeID [8]byte

// String writes a node id as Link's logs do.
func (n NodeID) String() string { return fmt.Sprintf("0x%x", n[:]) }

// Less orders node ids, which is how two sessions whose clocks are too
// close to tell apart are decided between.
func (n NodeID) Less(other NodeID) bool {
	for i := range n {
		if n[i] != other[i] {
			return n[i] < other[i]
		}
	}
	return false
}

// A Timeline maps beats to time: at timeOrigin the session was at
// beatOrigin, and it has run at tempo since.
//
// Tempo travels as microseconds per beat and beats as millionths of a
// beat, both as 64-bit signed numbers, so the whole timeline is exact
// on the wire.
type Timeline struct {
	// BPM is the tempo in beats per minute.
	BPM float64
	// BeatOrigin is the beat the session was at, in beats.
	BeatOrigin float64
	// TimeOrigin is when that was, in ghost microseconds.
	TimeOrigin int64
}

// BeatsAt is the beat the timeline reads at a ghost time.
func (t Timeline) BeatsAt(ghostMicros int64) float64 {
	if t.BPM <= 0 {
		return t.BeatOrigin
	}
	return t.BeatOrigin + float64(ghostMicros-t.TimeOrigin)/microsPerBeat(t.BPM)
}

func microsPerBeat(bpm float64) float64 { return 60 * 1e6 / bpm }

// A StartStop is the session's transport, with the beat and ghost time
// it changed at. Start and stop sync is optional in Link, and a peer
// that never publishes it leaves this at its zero value.
type StartStop struct {
	Playing    bool
	Beats      float64
	TimeOrigin int64
}

// A NodeState is everything a node says about itself in a discovery
// broadcast.
type NodeState struct {
	NodeID    NodeID
	SessionID NodeID
	Timeline  Timeline
	StartStop StartStop
	// Measurement is where to reach the node's ping responder. It is
	// zero for a node that published no IPv4 endpoint.
	Measurement netip.AddrPort
}

// A Message is one decoded discovery message.
type Message struct {
	Type byte
	// TTLSeconds is how long the sender's state stands.
	TTLSeconds int
	State      NodeState
}

// errShort says a message ran out before its fields did.
var errShort = errors.New("link: the message is too short")

// ---- writing ----

type writer struct{ buf []byte }

func (w *writer) u8(v byte)     { w.buf = append(w.buf, v) }
func (w *writer) u16(v uint16)  { w.buf = binary.BigEndian.AppendUint16(w.buf, v) }
func (w *writer) u32(v uint32)  { w.buf = binary.BigEndian.AppendUint32(w.buf, v) }
func (w *writer) i64(v int64)   { w.buf = binary.BigEndian.AppendUint64(w.buf, uint64(v)) }
func (w *writer) raw(v []byte)  { w.buf = append(w.buf, v...) }
func (w *writer) node(v NodeID) { w.buf = append(w.buf, v[:]...) }
func (w *writer) boolean(v bool) {
	if v {
		w.u8(1)
	} else {
		w.u8(0)
	}
}

// entry writes one payload entry: its key, the size of its value, then
// the value.
func (w *writer) entry(key uint32, value []byte) {
	w.u32(key)
	w.u32(uint32(len(value)))
	w.raw(value)
}

// EncodeDiscovery builds an alive, response or bye-bye message.
//
// A bye-bye carries no payload: it only says the node has gone, so
// peers drop it at once rather than waiting out its time to live.
func EncodeDiscovery(msgType byte, ttlSeconds int, state NodeState) []byte {
	var w writer
	w.raw(discoveryHeader[:])
	w.u8(msgType)
	w.u8(byte(ttlSeconds))
	w.u16(0) // the group id, which Link always leaves at zero
	w.node(state.NodeID)
	if msgType != MsgByeBye {
		w.raw(encodePayload(state))
	}
	return w.buf
}

func encodePayload(state NodeState) []byte {
	var w writer

	var tl writer
	tl.i64(int64(math.Round(microsPerBeat(state.Timeline.BPM))))
	tl.i64(int64(math.Round(state.Timeline.BeatOrigin * 1e6)))
	tl.i64(state.Timeline.TimeOrigin)
	w.entry(keyTimeline, tl.buf)

	w.entry(keySession, state.SessionID[:])

	var ss writer
	ss.boolean(state.StartStop.Playing)
	ss.i64(int64(math.Round(state.StartStop.Beats * 1e6)))
	ss.i64(state.StartStop.TimeOrigin)
	w.entry(keyStartStop, ss.buf)

	// The endpoint entry is left out entirely when there is no IPv4
	// address to publish, exactly as Link leaves out an entry whose
	// value would be empty.
	if state.Measurement.IsValid() && state.Measurement.Addr().Is4() {
		var ep writer
		addr := state.Measurement.Addr().As4()
		ep.u32(binary.BigEndian.Uint32(addr[:]))
		ep.u16(state.Measurement.Port())
		w.entry(keyMeasurementV4, ep.buf)
	}
	return w.buf
}

// EncodePing builds a ping: the host time it left at, and the ghost
// time of the pong before it where there was one.
func EncodePing(hostMicros, prevGhostMicros int64, havePrev bool) []byte {
	var w writer
	w.raw(measurementHeader[:])
	w.u8(MsgPing)
	w.entry(keyHostTime, int64Bytes(hostMicros))
	if havePrev {
		w.entry(keyPrevGhostTime, int64Bytes(prevGhostMicros))
	}
	return w.buf
}

// EncodePong builds a reply to a ping: this node's session and the
// ghost time now, then the ping's own payload echoed back so the asker
// can match the reply to what it sent.
func EncodePong(sessionID NodeID, ghostMicros int64, pingPayload []byte) []byte {
	var w writer
	w.raw(measurementHeader[:])
	w.u8(MsgPong)
	w.entry(keySession, sessionID[:])
	w.entry(keyGhostTime, int64Bytes(ghostMicros))
	w.raw(pingPayload)
	return w.buf
}

func int64Bytes(v int64) []byte {
	var w writer
	w.i64(v)
	return w.buf
}

// ---- reading ----

type reader struct {
	buf []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if len(r.buf) < n {
		r.err = errShort
		return nil
	}
	out := r.buf[:n]
	r.buf = r.buf[n:]
	return out
}

func (r *reader) u8() byte {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *reader) u16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (r *reader) u32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *reader) i64() int64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(b))
}

func (r *reader) node() NodeID {
	var id NodeID
	if b := r.take(8); b != nil {
		copy(id[:], b)
	}
	return id
}

// entries walks a payload, calling fn for each entry it holds. An entry
// whose size runs past the end of the payload ends the walk with an
// error, since nothing after it can be trusted either.
func entries(payload []byte, fn func(key uint32, value []byte)) error {
	for len(payload) > 0 {
		if len(payload) < 8 {
			return errShort
		}
		key := binary.BigEndian.Uint32(payload)
		size := int(binary.BigEndian.Uint32(payload[4:]))
		payload = payload[8:]
		if size < 0 || size > len(payload) {
			return fmt.Errorf("link: a payload entry claims %d bytes, and %d are left", size, len(payload))
		}
		fn(key, payload[:size])
		payload = payload[size:]
	}
	return nil
}

// DecodeDiscovery reads an alive, response or bye-bye message. It
// reports an error for anything that is not one, which is what a packet
// from another protocol on the same group looks like.
func DecodeDiscovery(data []byte) (Message, error) {
	if len(data) < len(discoveryHeader)+12 || !bytes.Equal(data[:8], discoveryHeader[:]) {
		return Message{}, errors.New("link: not a discovery message")
	}
	r := &reader{buf: data[len(discoveryHeader):]}
	var m Message
	m.Type = r.u8()
	m.TTLSeconds = int(r.u8())
	r.u16() // the group id
	m.State.NodeID = r.node()
	if r.err != nil {
		return Message{}, r.err
	}
	if m.Type == MsgByeBye {
		return m, nil
	}
	if err := decodeNodePayload(r.buf, &m.State); err != nil {
		return Message{}, err
	}
	return m, nil
}

func decodeNodePayload(payload []byte, state *NodeState) error {
	return entries(payload, func(key uint32, value []byte) {
		v := &reader{buf: value}
		switch key {
		case keyTimeline:
			micros, beats, origin := v.i64(), v.i64(), v.i64()
			if v.err != nil || micros <= 0 {
				return
			}
			state.Timeline = Timeline{
				BPM:        60 * 1e6 / float64(micros),
				BeatOrigin: float64(beats) / 1e6,
				TimeOrigin: origin,
			}
		case keySession:
			state.SessionID = v.node()
		case keyStartStop:
			playing, beats, origin := v.u8() != 0, v.i64(), v.i64()
			if v.err != nil {
				return
			}
			state.StartStop = StartStop{Playing: playing, Beats: float64(beats) / 1e6, TimeOrigin: origin}
		case keyMeasurementV4:
			raw, port := v.u32(), v.u16()
			if v.err != nil {
				return
			}
			var addr [4]byte
			binary.BigEndian.PutUint32(addr[:], raw)
			state.Measurement = netip.AddrPortFrom(netip.AddrFrom4(addr), port)
		}
	})
}

// A Pong is a decoded reply to a ping.
type Pong struct {
	SessionID NodeID
	// GhostMicros is the responder's ghost time when it replied.
	GhostMicros int64
	// HostMicros and PrevGhostMicros are the ping's own payload, echoed
	// back, which is what ties the reply to the ping that caused it.
	HostMicros      int64
	PrevGhostMicros int64
}

// A Ping is a decoded ping, as the responder reads it. Payload is the
// bytes to echo back in the pong.
type Ping struct {
	HostMicros      int64
	PrevGhostMicros int64
	Payload         []byte
}

// DecodeMeasurement reads a ping or a pong.
func DecodeMeasurement(data []byte) (msgType byte, ping Ping, pong Pong, err error) {
	if len(data) <= len(measurementHeader) || !bytes.Equal(data[:8], measurementHeader[:]) {
		return 0, Ping{}, Pong{}, errors.New("link: not a measurement message")
	}
	msgType = data[len(measurementHeader)]
	payload := data[len(measurementHeader)+1:]
	ping.Payload = payload
	err = entries(payload, func(key uint32, value []byte) {
		v := &reader{buf: value}
		switch key {
		case keySession:
			pong.SessionID = v.node()
		case keyGhostTime:
			pong.GhostMicros = v.i64()
		case keyHostTime:
			t := v.i64()
			pong.HostMicros, ping.HostMicros = t, t
		case keyPrevGhostTime:
			t := v.i64()
			pong.PrevGhostMicros, ping.PrevGhostMicros = t, t
		}
	})
	if err != nil {
		return 0, Ping{}, Pong{}, err
	}
	return msgType, ping, pong, nil
}

// DiscoveryGroup is the multicast address and port every Link node
// joins.
func DiscoveryGroup() *net.UDPAddr {
	return &net.UDPAddr{IP: DiscoveryAddr.AsSlice(), Port: DiscoveryPort}
}
