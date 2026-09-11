package checked

import "crypto/sha256"

// sourceTextDigest hashes every current byte without copying the entire source
// string into a temporary byte slice on each source-coverage validation.
func sourceTextDigest(text string) [32]byte {
	h := sha256.New()
	var block [4096]byte
	for len(text) > 0 {
		n := copy(block[:], text)
		_, _ = h.Write(block[:n])
		text = text[n:]
	}
	var sum [32]byte
	copy(sum[:], h.Sum(sum[:0]))
	return sum
}
