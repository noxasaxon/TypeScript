package extract

import (
	"fmt"
	"github.com/microsoft/typescript-go/tsox/graph"
	"strings"
	"testing"
)

func TestAsyncConditionalSuccessors(t *testing.T) {
	source := strings.Replace(asyncSource, " const text=await hostRead(input.key);", `let text=""; if(input.key==="hit"){const loaded="cached";text=loaded;}else{const loaded=await hostRead(input.key);text=loaded;}`, 1)
	r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead")
	if r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
	a := r.Program
	if a.Flow == nil || len(a.Stages) != 1 || len(a.Flow.Blocks) != 5 {
		t.Fatalf("expected one shared join, hit, await miss, miss continuation and branch: %+v", a.Flow)
	}
	entry := a.Flow.Blocks[a.Flow.Entry]
	if entry.Condition == nil || entry.Then == entry.Else {
		t.Fatal("lost conditional successors")
	}
	var shadows []graph.BindingID
	var collect func([]*graph.Statement)
	collect = func(ss []*graph.Statement) {
		for _, s := range ss {
			if s.Name == "loaded" {
				shadows = append(shadows, s.Binding)
			}
			collect(s.Then)
			collect(s.Else)
		}
	}
	collect(a.Flow.Body)
	if len(shadows) != 2 || shadows[0] == shadows[1] {
		t.Fatal("branch shadow bindings merged")
	}
	await := a.Flow.Blocks[entry.Else]
	if await.Await != 0 || a.Flow.Blocks[entry.Then].Next != a.Flow.Blocks[await.Next].Next {
		t.Fatal("branch suffix was not shared")
	}
}

func TestAsyncConditionalPrunesUnreachableOperations(t *testing.T) {
	source := strings.Replace(asyncSource, " const text=await hostRead(input.key);\n return {text:text};", `if(input.key==="hit"){return {text:"hit"};const text=await hostRead(input.key);}else{return {text:"miss"};const text=await hostRead(input.key);}const later=await hostRead(input.key);return {text:later};`, 1)
	r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead")
	if r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
	if r.Program.Flow == nil || len(r.Program.Stages) != 0 || len(r.Program.Flow.Blocks) != 3 {
		t.Fatalf("unreachable continuations survived: %+v", r.Program)
	}
}

func TestAsyncConditionalAwaitLoopsRemainFenced(t *testing.T) {
	source := strings.Replace(asyncSource, " const text=await hostRead(input.key);", `let text="";while(input.key!==text){const value=await hostRead(input.key);text=value;}`, 1)
	if r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead"); r.Program != nil || len(r.Diagnostics) == 0 {
		t.Fatal("await loop unexpectedly admitted")
	}
}

func TestAsyncConditionalSharedSuffixGrowth(t *testing.T) {
	var body strings.Builder
	const branches = 24
	for i := 0; i < branches; i++ {
		fmt.Fprintf(&body, "if(input.key===%q){const value=await hostRead(input.key);console.log(value);}", fmt.Sprint(i))
	}
	source := strings.Replace(asyncSource, " const text=await hostRead(input.key);\n return {text:text};", body.String()+`return{text:"done"};`, 1)
	r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead")
	if r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
	if len(r.Program.Stages) != branches || len(r.Program.Flow.Blocks) != 3*branches+1 {
		t.Fatalf("conditional suffix was duplicated: %d blocks / %d operations", len(r.Program.Flow.Blocks), len(r.Program.Stages))
	}
}
