package checked

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSourceMetadataMapCanonicalOrder(t *testing.T) {
	// Literal encodings witness the existing digest order, including lexical
	// numeric order and empty string keys. Sorting must not change seal bytes.
	cases := []struct {
		value any
		text  string
	}{
		{map[string]int{"z": 3, "a": 2, "": 1}, `map[string]int:3;string:"";int:1;string:"a";int:2;string:"z";int:3;`},
		{map[int]string{2: "two", 10: "ten", -1: "negative"}, `map[int]string:3;int:-1;string:"negative";int:10;string:"ten";int:2;string:"two";`},
		{map[uint8]bool{2: false, 10: true}, `map[uint8]bool:2;uint8:10;bool:true;uint8:2;bool:false;`},
		{map[string]int(nil), `map[string]int:nil;0;`},
		{map[string]int{}, `map[string]int:0;`},
	}
	for _, c := range cases {
		for range 10 {
			got, err := sourceMetadataDigest(reflect.ValueOf(c.value))
			if err != nil || got != sha256.Sum256([]byte(c.text)) {
				t.Fatalf("%T canonical digest: %x, %v", c.value, got, err)
			}
		}
	}
	if _, err := sourceMetadataDigest(reflect.ValueOf(map[bool]int{true: 1})); err == nil {
		t.Fatal("unsupported map key accepted")
	}
}

func TestSourceMetadataMapMutationAndPointerIdentity(t *testing.T) {
	left, right := 7, 7
	values := map[*int]string{&left: "original"}
	digest := func() [32]byte {
		t.Helper()
		value, err := sourceMetadataDigest(reflect.ValueOf(values))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	original := digest()
	values[&left] = "changed"
	if digest() == original {
		t.Fatal("changed map value accepted")
	}
	values[&left] = "original"
	if digest() != original {
		t.Fatal("restored map changed digest")
	}
	delete(values, &left)
	values[&right] = "original"
	if digest() == original {
		t.Fatal("replacement pointer with identical contents accepted")
	}
}

func BenchmarkSourceMetadataMapDigest(b *testing.B) {
	values := map[string]int{}
	for i := range 256 {
		values[fmt.Sprintf("%s/%04d", strings.Repeat("source", 16), i)] = i
	}
	value := reflect.ValueOf(values)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := sourceMetadataDigest(value); err != nil {
			b.Fatal(err)
		}
	}
}
