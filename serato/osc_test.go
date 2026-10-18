package serato

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"testing"
)

func fromHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ping is a real /Ping packet captured from Serato: the address, the
// empty type tags, then the 16-byte trailer.
const pingHex = "2f50696e670000002c0000004caac2ae35b1c476db5a644403bd4170"

// authReq is a real /StreamMgmt/Authorize/Request carrying a 16-byte
// blob and two ints, plus the trailer.
const authReqHex = "2f53747265616d4d676d742f417574686f72697a652f526571756573740000002c626969000000" +
	"00000000102480f5846fa0dbefda7a4f6ee389429900000001000000014caac2ae35b1c476db5a644403bd4170"

func TestDecodeBarePing(t *testing.T) {
	ping := fromHex(t, pingHex)
	pkt, err := Decode(ping)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Bundle || pkt.Messages[0].Address != "/Ping" {
		t.Errorf("packet = %+v", pkt)
	}
	if got := hex.EncodeToString(pkt.Trailer); got != "4caac2ae35b1c476db5a644403bd4170" {
		t.Errorf("trailer = %s", got)
	}
	if pkt.End != len(ping) {
		t.Errorf("end = %d, want %d", pkt.End, len(ping))
	}
}

func TestDecodeAuthorizeRequest(t *testing.T) {
	pkt, err := Decode(fromHex(t, authReqHex))
	if err != nil {
		t.Fatal(err)
	}
	m := pkt.Messages[0]
	if m.Address != "/StreamMgmt/Authorize/Request" || m.Types != "bii" {
		t.Fatalf("message = %+v", m)
	}
	if blob, ok := m.Blob(16); !ok || len(blob) != 16 {
		t.Errorf("no 16-byte blob: %v", ok)
	}
	if m.Int(1) != 1 || m.Int(2) != 1 {
		t.Errorf("ints = %d, %d; want 1, 1", m.Int(1), m.Int(2))
	}
}

func TestDecodeReportsAPartialPacket(t *testing.T) {
	ping := fromHex(t, pingHex)
	if _, err := Decode(ping[:10]); !errors.Is(err, ErrShort) {
		t.Errorf("a part packet gave %v, want ErrShort", err)
	}
	if _, err := Decode(ping); err != nil {
		t.Errorf("the whole packet gave %v", err)
	}
}

func TestDecodeWalksBackToBackPackets(t *testing.T) {
	ping := fromHex(t, pingHex)
	two := slices.Concat(ping, ping)
	first, err := Decode(two)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Decode(two[first.End:])
	if err != nil {
		t.Fatal(err)
	}
	if first.Messages[0].Address != "/Ping" || second.Messages[0].Address != "/Ping" {
		t.Errorf("addresses = %q, %q", first.Messages[0].Address, second.Messages[0].Address)
	}
}

func TestEncodeBundleMatchesSoundSwitch(t *testing.T) {
	// A captured, known-good Authorize/Response bundle.
	want := "2362756e646c65000000000000000001000000802f53747265616d4d676d742f417574686f72697a" +
		"652f526573706f6e736500002c737362000000007b31343666653935392d313661332d343935372d6232" +
		"35392d3665346139643438343635347d0000536f756e645377697463682040204d61726375735850533" +
		"235000000000000108628b97e639cb83d32ec399ff7ef71bf4caac2ae35b1c476db5a644403bd4170"
	got := EncodeBundle([]Message{{
		Address: "/StreamMgmt/Authorize/Response",
		Types:   "ssb",
		Args: []Arg{
			"{146fe959-16a3-4957-b259-6e4a9d484654}",
			"SoundSwitch @ MarcusXPS25",
			fromHex(t, "8628b97e639cb83d32ec399ff7ef71bf"),
		},
	}}, fromHex(t, "4caac2ae35b1c476db5a644403bd4170"))
	if hex.EncodeToString(got) != want {
		t.Errorf("bundle = %s\nwant    %s", hex.EncodeToString(got), want)
	}
}

func TestEncodeMessagePadsToFourBytes(t *testing.T) {
	if b := EncodeMessage("/Ping", "", nil); len(b)%4 != 0 {
		t.Errorf("a body of %d bytes is not padded", len(b))
	}
	if TrailerLen != 16 {
		t.Errorf("TrailerLen = %d, want 16", TrailerLen)
	}
}

func TestDecodeRejectsANegativeBlobLength(t *testing.T) {
	// "/x" and ",b", then a negative int32 length, then the trailer. It
	// must not walk the parse pointer backwards.
	buf := slices.Concat(
		fromHex(t, "2f780000"), // "/x\0\0"
		fromHex(t, "2c620000"), // ",b\0\0"
		fromHex(t, "fffffff8"), // -8
		fromHex(t, "4caac2ae35b1c476db5a644403bd4170"),
	)
	if pkt, err := Decode(buf); err == nil {
		t.Errorf("a negative blob length decoded to %+v", pkt)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	want := Message{Address: "/Status/Deck/Playhead", Types: "ifff",
		Args: []Arg{int32(0), float32(16.9), float32(1), float32(126.84)}}
	body := EncodeMessage(want.Address, want.Types, want.Args)
	pkt, err := Decode(slices.Concat(body, make([]byte, TrailerLen)))
	if err != nil {
		t.Fatal(err)
	}
	got := pkt.Messages[0]
	if got.Address != want.Address || got.Types != want.Types {
		t.Fatalf("message = %+v", got)
	}
	if got.Int(0) != 0 || math.Abs(got.Float(1)-16.9) > 1e-4 || math.Abs(got.Float(3)-126.84) > 1e-4 {
		t.Errorf("arguments = %+v", got.Args)
	}
}

func TestEncodeBlobArgument(t *testing.T) {
	blob := []byte{1, 2, 3}
	body := EncodeMessage("/x", "b", []Arg{blob})
	pkt, err := Decode(slices.Concat(body, make([]byte, TrailerLen)))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := pkt.Messages[0].Blob(3)
	if !ok || !bytes.Equal(got, blob) {
		t.Errorf("blob = %v, %v", got, ok)
	}
}
