package serato

import "crypto/md5"

// secret is the 32 bytes of key material embedded in SoundSwitch.exe,
// recovered by reverse engineering (see
// docs/soundswitch-micro-research.md, where it is the context struct's
// X and Y). It is fixed and universal rather than per-machine, which
// is what lets crossroads answer Serato for itself.
var secret = mustHex("5e0ac1b120684ff3b35c4532b86bda9aa4e0cd4b59094305b724567924aad7c5")

// NonceLen is the length of Serato's challenge nonce, and of the reply
// blob.
const NonceLen = 16

// AuthBlob answers Serato's challenge: MD5 over the nonce followed by
// the secret.
//
// The transform was worked out from four nonce-and-blob pairs recorded
// off SoundSwitch itself; the test beside this checks all four, which
// is as near proof as there is without Serato in front of us.
func AuthBlob(nonce []byte) []byte {
	sum := md5.Sum(append(append(make([]byte, 0, len(nonce)+len(secret)), nonce...), secret...))
	return sum[:]
}
