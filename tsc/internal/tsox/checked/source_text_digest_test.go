package checked

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestSourceTextDigestExactBytes(t *testing.T) {
	for _, length := range []int{0, 1, 55, 56, 63, 64, 65, 4095, 4096, 4097, 8193, 1048577} {
		input := make([]byte, length)
		for i := range input {
			// Include NUL and invalid UTF-8; source fingerprints hash bytes.
			input[i] = byte(i * 37)
		}
		if got, want := sourceTextDigest(string(input)), sha256.Sum256(input); got != want {
			t.Fatalf("length %d: %x != %x", length, got, want)
		}
	}
}

func BenchmarkSourceTextDigest(b *testing.B) {
	text := strings.Repeat("source text\x00\xff", 100000)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sourceTextDigest(text)
	}
}
