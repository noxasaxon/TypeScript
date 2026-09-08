package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestRecordCarrierAdditionalSources(t *testing.T) {
	root := sourcefixture.Get(t, "carrier")
	b, err := os.ReadFile(filepath.Join(root, "fixtures/mutation/case.ts"))
	if err != nil {
		t.Fatal(err)
	}
	original := string(b)
	for name, source := range map[string]string{
		"slot-permutation":  strings.Replace(original, "interface B{owner:string;note?:string}", "interface B{note?:string;owner:string}", 1),
		"assignment-result": strings.Replace(original, "return source.owner;", "return (target.detail=source).owner;", 1),
	} {
		p, ds := checked.New("case.ts", map[string]string{"case.ts": source})
		if len(ds) > 0 {
			t.Fatal(name, ds)
		}
		bundle := sourceHelperBodies(p)
		if len(bundle.Diagnostics) > 0 && name != "slot-permutation" {
			t.Fatal(name, bundle.Diagnostics)
		}
		if name == "slot-permutation" && (len(bundle.Diagnostics) != 1 || !strings.Contains(bundle.Diagnostics[0].Message, "distinct named shapes")) {
			t.Fatal("source order boundary changed", bundle.Diagnostics)
		}
		dir := filepath.Join(root, "fixtures", name)
		if err = os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.MarshalIndent(bundle, "", "  ")
		for n, b := range map[string][]byte{"case.ts": []byte(source), "graph.json": data, "package.json": []byte(`{"type":"module"}`)} {
			if err = os.WriteFile(filepath.Join(dir, n), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRecordCarrierFrozenSourceIntegrity(t *testing.T) {
	root := sourcefixture.Get(t, "carrier")
	for _, name := range []string{"alias", "capable", "mutation", "transitive"} {
		dir := filepath.Join(root, "fixtures", name)
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
		frozen, err := os.ReadFile(filepath.Join(dir, "graph.json"))
		if err != nil {
			t.Fatal(err)
		}
		// Decode using the same actual builder result type, so additive nil metadata
		// does not disguise a source-program difference through serialized spelling.
		var expected SourceHelperBodies
		if err = json.Unmarshal(frozen, &expected); err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(actual.Program)
		b, _ := json.Marshal(expected.Program)
		if string(a) != string(b) {
			t.Fatal("frozen graph differs from actual current extraction", name)
		}
	}
}
