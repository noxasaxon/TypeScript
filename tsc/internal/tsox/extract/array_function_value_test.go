package extract

import "testing"

func TestArrayMethodFunctionValueIsDiagnosed(t *testing.T) {
	for _, method := range []string{"push", "unshift", "includes", "indexOf"} {
		t.Run(method, func(t *testing.T) {
			result := Extract(extractSourcePath, "const readers: (() => number)[] = []; readers."+method+"((): number => { return 1; });")
			if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != "FunctionValue" {
				t.Fatalf("want FunctionValue diagnostic, got %+v", result)
			}
		})
	}
}
