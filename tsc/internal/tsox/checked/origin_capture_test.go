package checked

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestOriginCaptureBeforeExposure(t *testing.T) {
	snapshot, e := ReadCJSBodySourceSnapshot(filepath.Join(sourcefixture.Get(t, "portfolio"), "tsconfig.json"), "web-api/route.ts", CJSExportBoundary{})
	if e != nil {
		t.Fatal(e)
	}
	reject := func() {
		t.Helper()
		if _, e := NewSourceRecoveryScope(snapshot); e == nil {
			t.Fatal("edited original capture accepted")
		}
	}
	restore := func() {
		t.Helper()
		if e := snapshot.validateSeal(); e != nil {
			t.Fatal("restored capture rejected", e)
		}
	}
	t.Run("parsed-directive-before-registration", func(t *testing.T) {
		f := snapshot.Program.Entry
		old := f.CheckJsDirective
		f.CheckJsDirective = &ast.CheckJsDirective{Enabled: false}
		reject()
		f.CheckJsDirective = old
		restore()
	})
	t.Run("original-statement-detached", func(t *testing.T) {
		f := snapshot.Program.Entry
		old := f.Statements.Nodes
		f.Statements.Nodes = append([]*ast.Node(nil), old...)
		reject()
		f.Statements.Nodes = old
		restore()
	})
	t.Run("ordered-startup-before-registration", func(t *testing.T) {
		for _, plan := range snapshot.Plans.Modules {
			if len(plan.Initialization) > 1 {
				plan.Initialization[0], plan.Initialization[1] = plan.Initialization[1], plan.Initialization[0]
				reject()
				plan.Initialization[0], plan.Initialization[1] = plan.Initialization[1], plan.Initialization[0]
				restore()
				return
			}
		}
		t.Fatal("actual startup missing")
	})
	t.Run("map-key-content-before-registration", func(t *testing.T) {
		f := snapshot.Program.Entry
		for k, v := range f.Identifiers {
			f.Identifiers[k] = v + "changed"
			reject()
			f.Identifiers[k] = v
			restore()
			return
		}
		t.Fatal("actual identifiers missing")
	})
	t.Run("foreign-snapshot-copy", func(t *testing.T) {
		copy := *snapshot
		if _, e := NewSourceRecoveryScope(&copy); e == nil {
			t.Fatal("copied public snapshot became issuer")
		}
	})
	t.Run("seal-cannot-rebaseline", func(t *testing.T) {
		if e := sealSourceSnapshot(snapshot, snapshot.syntax); e == nil {
			t.Fatal("existing seal was replaced")
		}
	})
	if _, e := NewSourceRecoveryScope(snapshot); e != nil {
		t.Fatal(e)
	}
}
func TestOriginSchemaExclusionsFailClosed(t *testing.T) {
	for name := range sourceFileCache {
		if _, ok := reflect.TypeFor[ast.SourceFile]().FieldByName(name); !ok {
			t.Fatal("stale excluded field", name)
		}
	}
	if e := requireSourceSyntaxSchema(reflect.StructOf([]reflect.StructField{{Name: "FutureSyntax", Type: reflect.TypeFor[string]()}})); e == nil {
		t.Fatal("unknown schema accepted")
	}
	// Parsed syntax maps/pragma arguments cannot use an unclassified dynamic kind.
	out := &sourceSyntaxIntegrity{}
	value := func() {}
	if out.capture(reflect.ValueOf(value)) == nil {
		t.Fatal("unclassified payload accepted")
	}
}
