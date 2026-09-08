package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestUnknownEffectSourceFixtures(t *testing.T) {
	for _, key := range []string{"optional-consumer", "carrier"} {
		t.Run(key, func(t *testing.T) {
			dir := filepath.Join(sourcefixture.Get(t, key), "fixtures/unknown-effect")
			source, err := os.ReadFile(filepath.Join(dir, "case.ts"))
			if err != nil {
				t.Fatal(err)
			}
			p, ds := checked.New("case.ts", map[string]string{"case.ts": string(source)})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			actual := sourceHelperBodies(p)
			if len(actual.Diagnostics) > 0 {
				t.Fatal(actual.Diagnostics)
			}
			b, err := os.ReadFile(filepath.Join(dir, "graph.json"))
			if err != nil {
				t.Fatal(err)
			}
			var expected SourceHelperBodies
			if err = json.Unmarshal(b, &expected); err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(actual.Program)
			b, _ = json.Marshal(expected.Program)
			if string(a) != string(b) {
				t.Fatal("actual effectful source differs from frozen graph")
			}
		})
	}
}
