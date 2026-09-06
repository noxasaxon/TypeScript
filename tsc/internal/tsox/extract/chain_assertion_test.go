package extract

import "testing"

func TestExtractChainAssertionScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, expression string
		chain, link      bool
	}{
		{"in chain", "g?.items!.includes(1)", true, true},
		{"new optional link", "g?.items!?.includes(1)", true, true},
		{"grouped asserted link", "(g?.items!)?.includes(1)", true, true},
		{"grouped assertion", "(g?.items)!.includes(1)", false, false},
		{"grouped assertion before optional link", "(g?.items)!?.includes(1)", true, false},
		{"repeated link assertion", "g?.items!!?.includes(1)", true, true},
		{"repeated grouped assertion", "(g?.items)!!?.includes(1)", true, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := Extract(extractSourcePath, `interface G { items?: number[]; }
function present(): G | undefined { return {items:[1]}; }
const g = present();
const answer: boolean | undefined = `+test.expression+`;`)
			if result.Program == nil {
				t.Fatal(result.Diagnostics)
			}
			value := variableStatement(t, result.Program, "answer").Value
			if value.OptionalChain != test.chain {
				t.Fatalf("method chain = %t, want %t", value.OptionalChain, test.chain)
			}
			receiver := value.Receiver
			if receiver == nil || !receiver.NonNullAssertion || receiver.ChainResultAsserted != test.link || receiver.Type.Optional != test.link || receiver.UnwrapOptional == test.link {
				t.Fatalf("receiver assertion scope: %#v, want link=%t", receiver, test.link)
			}
		})
	}
}
