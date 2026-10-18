// Package serato reads live deck state from Serato DJ over External
// Control.
//
// The protocol was reverse-engineered; see
// docs/soundswitch-micro-research.md.
//
// It came from midittt, which crossroads takes over from.
package serato

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
)

// This file is a small OSC 1.0 codec for Serato's stream, plus the
// conventions Serato adds on top:
//
//   - No length prefix and no SLIP framing. Packets sit back to back,
//     so a reader parses one to find the next.
//   - A 16-byte trailer after every packet, constant for the life of a
//     connection — a session id — that replies must echo.
//   - Asymmetric bundling. Serato sends bare messages; a client replies
//     with #bundle packets whose single trailer sits after the whole
//     bundle.

// TrailerLen is the length of the session id that follows every packet.
const TrailerLen = 16

const bundleTag = "#bundle\x00"

func pad4(n int) int { return (n + 3) &^ 3 }

// An Arg is one OSC argument: an int32, a float32, a string, a bool, a
// blob, or nil.
type Arg any

// A Message is one decoded OSC message: its address, its type tags
// without the leading comma, and its arguments.
type Message struct {
	Address string
	Types   string
	Args    []Arg
}

// Int is the argument at i as an int, and zero when it is not a number.
func (m Message) Int(i int) int {
	switch v := m.arg(i).(type) {
	case int32:
		return int(v)
	case float32:
		return int(v)
	}
	return 0
}

// Float is the argument at i as a float, and zero when it is not a
// number.
func (m Message) Float(i int) float64 {
	switch v := m.arg(i).(type) {
	case int32:
		return float64(v)
	case float32:
		return float64(v)
	}
	return 0
}

// Str is the argument at i as a string, and empty when it is not one.
func (m Message) Str(i int) string {
	if s, ok := m.arg(i).(string); ok {
		return s
	}
	return ""
}

// Blob is the first argument that is a blob of n bytes.
func (m Message) Blob(n int) ([]byte, bool) {
	for _, a := range m.Args {
		if b, ok := a.([]byte); ok && len(b) == n {
			return b, true
		}
	}
	return nil, false
}

// IsNumber reports whether the argument at i is a number at all, which
// tells a missing argument from one that is really zero.
func (m Message) IsNumber(i int) bool {
	switch m.arg(i).(type) {
	case int32, float32:
		return true
	}
	return false
}

func (m Message) arg(i int) Arg {
	if i < 0 || i >= len(m.Args) {
		return nil
	}
	return m.Args[i]
}

// A Packet is one decoded packet: its messages, one or several in a
// bundle, and its trailer.
type Packet struct {
	Bundle   bool
	Messages []Message
	Trailer  []byte
	// End is the offset in the source buffer just past this packet.
	End int
}

// ErrShort says the buffer holds only part of a packet, which is the
// normal mid-stream case: a /Ping has been seen split across two reads.
var ErrShort = errors.New("serato: the packet is not all here yet")

// errMalformed says the bytes are not a packet this codec can read.
var errMalformed = errors.New("serato: malformed packet")

func readString(buf []byte, off int) (s string, next int, err error) {
	end := off
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	if end >= len(buf) {
		return "", 0, ErrShort
	}
	next = off + pad4(end-off+1)
	if next > len(buf) {
		return "", 0, ErrShort
	}
	return string(buf[off:end]), next, nil
}

func writeString(s string) []byte {
	out := make([]byte, pad4(len(s)+1))
	copy(out, s)
	return out
}

// short reports whether a field running to limit has all its bytes, and
// tells a truncated packet from a malformed one: past the end of the
// buffer is truncation, within it but past the element's own limit is
// malformed.
func short(buf []byte, need, limit int) error {
	if need > len(buf) {
		return ErrShort
	}
	if need > limit {
		return errMalformed
	}
	return nil
}

func decodeMessage(buf []byte, off, limit int) (Message, int, error) {
	address, next, err := readString(buf, off)
	if err != nil {
		return Message{}, 0, err
	}
	if err := short(buf, next, limit); err != nil {
		return Message{}, 0, err
	}
	tags, next, err := readString(buf, next)
	if err != nil {
		return Message{}, 0, err
	}
	if !strings.HasPrefix(tags, ",") {
		return Message{}, 0, errMalformed
	}
	if err := short(buf, next, limit); err != nil {
		return Message{}, 0, err
	}

	var args []Arg
	p := next
	for _, t := range tags[1:] {
		switch t {
		case 'i', 'f':
			if err := short(buf, p+4, limit); err != nil {
				return Message{}, 0, err
			}
			bits := binary.BigEndian.Uint32(buf[p:])
			if t == 'i' {
				args = append(args, int32(bits))
			} else {
				args = append(args, math.Float32frombits(bits))
			}
			p += 4
		case 's':
			s, after, err := readString(buf, p)
			if err != nil {
				return Message{}, 0, err
			}
			if err := short(buf, after, limit); err != nil {
				return Message{}, 0, err
			}
			args = append(args, s)
			p = after
		case 'b':
			if err := short(buf, p+4, limit); err != nil {
				return Message{}, 0, err
			}
			n := int(int32(binary.BigEndian.Uint32(buf[p:])))
			// Reject a negative length. It is signed, and a negative
			// value passes the upper-bound check yet makes pad4
			// negative, moving the parse pointer backwards. This data
			// comes straight off a socket on the network.
			if n < 0 {
				return Message{}, 0, errMalformed
			}
			if err := short(buf, p+4+n, limit); err != nil {
				return Message{}, 0, err
			}
			args = append(args, buf[p+4:p+4+n])
			p += 4 + pad4(n)
		case 'T':
			args = append(args, true)
		case 'F':
			args = append(args, false)
		case 'N':
			args = append(args, nil)
		default:
			return Message{}, 0, errMalformed
		}
	}
	return Message{Address: address, Types: tags[1:], Args: args}, p, nil
}

// Decode reads one packet — a bare message or a bundle — and its
// trailer from the front of buf.
//
// It reports ErrShort when the buffer holds only part of one. A bundle
// carries no overall length, so its end is found by peeking: a real
// element starts with a slash or a hash.
func Decode(buf []byte) (Packet, error) {
	if len(buf) < 4 {
		return Packet{}, ErrShort
	}

	if len(buf) >= 16 && string(buf[:8]) == bundleTag {
		var messages []Message
		p := 16 // the tag and the time tag
		for {
			if p+TrailerLen > len(buf) {
				return Packet{}, ErrShort
			}
			looksLikeElement := p+4 < len(buf) &&
				int32(binary.BigEndian.Uint32(buf[p:])) > 0 &&
				(buf[p+4] == '/' || buf[p+4] == '#')
			if !looksLikeElement {
				break
			}
			size := int(int32(binary.BigEndian.Uint32(buf[p:])))
			if p+4+size+TrailerLen > len(buf) {
				return Packet{}, ErrShort
			}
			m, _, err := decodeMessage(buf, p+4, p+4+size)
			if err != nil {
				return Packet{}, err
			}
			messages = append(messages, m)
			p += 4 + size
		}
		if p+TrailerLen > len(buf) {
			return Packet{}, ErrShort
		}
		return Packet{Bundle: true, Messages: messages,
			Trailer: buf[p : p+TrailerLen], End: p + TrailerLen}, nil
	}

	m, end, err := decodeMessage(buf, 0, len(buf))
	if err != nil {
		return Packet{}, err
	}
	if end+TrailerLen > len(buf) {
		return Packet{}, ErrShort
	}
	return Packet{Messages: []Message{m}, Trailer: buf[end : end+TrailerLen], End: end + TrailerLen}, nil
}

// EncodeMessage builds a bare message body, with no trailer, as used
// inside a bundle or alone.
func EncodeMessage(address, types string, args []Arg) []byte {
	out := append(writeString(address), writeString(","+types)...)
	i := 0
	for _, t := range types {
		var v Arg
		if i < len(args) {
			v = args[i]
		}
		i++
		switch t {
		case 'i':
			out = binary.BigEndian.AppendUint32(out, uint32(toInt32(v)))
		case 'f':
			out = binary.BigEndian.AppendUint32(out, math.Float32bits(toFloat32(v)))
		case 's':
			out = append(out, writeString(toString(v))...)
		case 'b':
			data, _ := v.([]byte)
			out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
			out = append(out, data...)
			out = append(out, make([]byte, pad4(len(data))-len(data))...)
		case 'T', 'F', 'N':
			// These carry no bytes.
		}
	}
	return out
}

// EncodeBundle wraps messages in a #bundle packet with the trailer, as
// Serato expects a client's replies.
func EncodeBundle(messages []Message, trailer []byte) []byte {
	out := make([]byte, 0, 64)
	out = append(out, bundleTag...)
	out = binary.BigEndian.AppendUint64(out, 1) // immediate
	for _, m := range messages {
		body := EncodeMessage(m.Address, m.Types, m.Args)
		out = binary.BigEndian.AppendUint32(out, uint32(len(body)))
		out = append(out, body...)
	}
	if len(trailer) == TrailerLen {
		out = append(out, trailer...)
	} else {
		out = append(out, make([]byte, TrailerLen)...)
	}
	return out
}

func toInt32(v Arg) int32 {
	switch n := v.(type) {
	case int32:
		return n
	case int:
		return int32(n)
	case float32:
		return int32(n)
	case float64:
		return int32(n)
	}
	return 0
}

func toFloat32(v Arg) float32 {
	switch n := v.(type) {
	case float32:
		return n
	case float64:
		return float32(n)
	case int32:
		return float32(n)
	case int:
		return float32(n)
	}
	return 0
}

func toString(v Arg) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
