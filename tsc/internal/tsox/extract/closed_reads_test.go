package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func TestClosedReadKeepsDeclaredReceiver(t *testing.T) {
	cases := map[string]string{
		"tag_recall": `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{return {ok:true,value:3};}function entry(value:unknown):number{if(choose().ok){
// @ts-ignore
return choose().value;}return 0;}`,
		"stored_guard": `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{return {ok:true,value:3};}function entry(value:unknown):number{const r=choose();const yes=r.ok;if(!yes)return 0;return r.value;}`,

		"enum_alias":      `interface P{count:number} type R={ok:true;value:P}|{ok:false;status:number};function choose():R{return {ok:true,value:{count:1}};}function entry(value:unknown):number{const r=choose();const a=r;if(!a.ok||!r.ok)return 0;const p=a.value;r.value.count=2;return p.count;}`,
		"capable_payload": `type R={ok:true;value:string}|{ok:false;status:number};function choose(value:unknown):R{if(typeof value==="string")return {ok:true,value};return {ok:false,status:1};}function entry(value:unknown):string{const r=choose(value);if(!r.ok)return "bad";return r.value;}`,

		"unguarded_call": `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{return {ok:true,value:3};}function entry(value:unknown):number{const r=choose();
// @ts-ignore
return r.value;}`,
		"wrong_edge": `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{return {ok:true,value:3};}function entry(value:unknown):number{const r=choose();if(!r.ok){
// @ts-ignore
return r.value;}return 0;}`,
		"rebind": `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{return {ok:true,value:3};}function entry(value:unknown):number{let r=choose();if(!r.ok)return 0;r=choose();
// @ts-ignore
return r.value;}`,
		"alias_snapshot": `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{return {ok:true,value:3};}function entry(value:unknown):number{let r=choose();const alias=r;if(!alias.ok)return 0;r=choose();return alias.value;}`,
		"shortcircuit":   `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{return {ok:true,value:3};}function entry(value:unknown):number{const r=choose();if(r.ok&&r.value>0)return 1;return 0;}`,
		"tag_effect":     `type R={ok:true;value:number}|{ok:false;status:number};function choose():R{console.log("effect");return {ok:true,value:3};}function entry(value:unknown):boolean{return choose().ok;}`,
		"payload_alias":  `interface P{count:number} type R={ok:true;value:P}|{ok:false;status:number};function choose():R{return {ok:true,value:{count:1}};}function entry(value:unknown):number{const r=choose();if(!r.ok)return 0;const p=r.value;const q=r.value;p.count=2;return q.count;}`,

		"early_return": `type Choice<T>={tag:"yes";payload:T}|{tag:"no";code:number}; function choose():Choice<number>{return {tag:"yes",payload:3};} function entry(valueInput:unknown):number{const value=choose();if(value.tag!=="yes")return value.code;return value.payload;}`,
		"alias":        `type R={ok:true;value:number}|{ok:false;status:number}; function choose():R{return {ok:true,value:3};} function entry(valueInput:unknown):number{const r=choose();const alias=r; if(!alias.ok)return alias.status;return alias.value;}`,
		"unchecked": `type R={ok:true;value:number}|{ok:false;status:number};function entry(r:R):number{ // @ts-ignore
 return r.value;}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			result := extractSource(name+".ts", source, true)
			if result.Program == nil {
				t.Fatal(result.Diagnostics)
			}
			assertClosedReceiverObligations(t, result.Program)
			if directory := os.Getenv("TSOX_CLOSED_FIXTURES"); directory != "" {
				writeClosedFixture(t, directory, name, source, nil, result.Program)
			}
		})
	}
}

func TestClosedReadUnchangedValidatorConsumer(t *testing.T) {
	sources := map[string]string{}
	for _, name := range []string{"model.ts", "validation.ts"} {
		source, err := os.ReadFile(filepath.Join("testdata", "json-layout", name))
		if err != nil {
			t.Fatal(err)
		}
		sources[name] = string(source)
	}
	sources["consumer.ts"] = `import {validateTask} from "./validation.ts";
 export function consume(value:unknown):number {const input=validateTask(value);if(!input.ok)return input.status;return input.value.tags.length;}`
	program, diagnostics := checked.New("consumer.ts", sources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	result := extractChecked("consumer.ts", program, true)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	assertClosedReceiverObligations(t, result.Program)
	if directory := os.Getenv("TSOX_CLOSED_FIXTURES"); directory != "" {
		writeClosedFixture(t, directory, "validator_consumer", sources["consumer.ts"], sources, result.Program)
	}
	if public := ExtractChecked("consumer.ts", program); public.Program != nil {
		t.Fatal("overlay opened public admission")
	}
}

func assertClosedReceiverObligations(t *testing.T, program *graph.Program) {
	t.Helper()
	data, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	var object any
	if err = json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	reads := 0
	var visit func(any)
	visit = func(x any) {
		switch v := x.(type) {
		case []any:
			for _, c := range v {
				visit(c)
			}
		case map[string]any:
			if v["Kind"] == string(graph.ExpressionClosedTag) || v["Kind"] == string(graph.ExpressionClosedProperty) {
				reads++
				receiver, ok := v["Receiver"].(map[string]any)
				if !ok {
					t.Fatal("lost original receiver")
				}
				typ, ok := receiver["Type"].(map[string]any)
				if !ok || typ["Kind"] != string(graph.TypeClosedUnion) {
					t.Fatal("checker narrowing erased declared union")
				}
			}
			for _, c := range v {
				visit(c)
			}
		}
	}
	visit(object)
	if reads == 0 {
		t.Fatal("missing closed read obligation")
	}
}
func writeClosedFixture(t *testing.T, directory, name, source string, sources map[string]string, program *graph.Program) {
	t.Helper()
	data, err := json.MarshalIndent(struct {
		Source  string
		Sources map[string]string
		Program *graph.Program
	}{source, sources, program}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, name+".json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestClosedConstructionDoesNotErasePrototypeSyntax(t *testing.T) {
	source := `interface P { count: number } interface O { __proto__: P } function entry(value: unknown): O { return { __proto__: {count: 1} }; }`
	result := extractSource("prototype-construction.ts", source, true)
	if result.Program != nil || len(result.Diagnostics) == 0 || result.Diagnostics[0].Position.Line == 0 {
		t.Fatal("prototype-setting constructor acquired ordinary field storage")
	}
}
