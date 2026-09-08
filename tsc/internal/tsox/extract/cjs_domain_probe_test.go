package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestCJSActualRouteDomainBoundary(t *testing.T) {
	root := sourcefixture.Get(t, "cjs-domain-output")
	portfolio := sourcefixture.Get(t, "portfolio")
	if root == "" || portfolio == "" {
		t.Fatal("explicit source/output required")
	}
	p, _, roots, e := checked.ReadCJSBodySourceProject(filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts", cjsBodyRealm())
	if e != nil {
		t.Fatal(e)
	}
	if roots != 10 {
		t.Fatal(roots)
	}
	result := ExtractAsyncChecked(p.Entry.FileName(), p, "handle", "")
	data, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "route-extraction.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log(string(data))
}
