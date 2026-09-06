package extract

import (
	"github.com/microsoft/typescript-go/tsox/graph"
	"testing"
)

func TestExtractExplicitUndefinedCallbackDefault(t *testing.T) {
	result := Extract(extractSourcePath, `function one(): number { return 1; }
function invoke(callback: () => number = one): number { return callback(); }
console.log(invoke(undefined));
console.log(invoke());`)
	if result.Program == nil {
		t.Fatalf("extraction failed: %v", result.Diagnostics)
	}
	for _, statement := range result.Program.Statements[2:] {
		call := statement.Arguments[0]
		if len(call.Arguments) != 1 || call.Arguments[0].Kind != graph.ExpressionUndefined || !call.Arguments[0].Type.Optional || call.Arguments[0].Type.Kind != graph.TypeFunction {
			t.Fatalf("missing callback must retain the optional callable ABI: %#v", call.Arguments)
		}
	}
}
