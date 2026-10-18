package serato

import (
	"encoding/hex"
	"testing"
)

// TestAuthBlobAnswersTheRecordedChallenges, which is the whole of what
// says crossroads can pair with Serato by itself. The four pairs were
// recorded from SoundSwitch answering real nonces, and are written
// down in docs/soundswitch-micro-research.md.
func TestAuthBlobAnswersTheRecordedChallenges(t *testing.T) {
	for _, c := range []struct{ nonce, blob string }{
		{"00000000000000000000000000000000", "6d5c3b6ddb2cdeb7683cc88142b84254"},
		{"000102030405060708090a0b0c0d0e0f", "29ffcd7fb39208dcb0b6b83b6541ea11"},
		{"ffffffffffffffffffffffffffffffff", "b02e88c7c859b8cacb3d545b23de34e6"},
		{"0123456789abcdef0123456789abcdef", "087cea9e817747798011a7b340867b44"},
	} {
		nonce, err := hex.DecodeString(c.nonce)
		if err != nil {
			t.Fatal(err)
		}
		if got := hex.EncodeToString(AuthBlob(nonce)); got != c.blob {
			t.Errorf("nonce %s answered %s, want %s", c.nonce, got, c.blob)
		}
	}
}

// TestAuthBlobIsTheRightLength, since Serato reads a fixed blob.
func TestAuthBlobIsTheRightLength(t *testing.T) {
	if n := len(AuthBlob(make([]byte, NonceLen))); n != NonceLen {
		t.Errorf("the blob is %d bytes, want %d", n, NonceLen)
	}
}
