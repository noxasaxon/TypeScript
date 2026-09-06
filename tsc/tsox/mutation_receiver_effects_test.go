package tsox_test

import (
	"reflect"
	"testing"

	"github.com/microsoft/typescript-go/tsox"
)

func TestReceiverEffectSitesPreserveCoordinatesAndLexicalDonors(t *testing.T) {
	source := `// café 😀
interface Group { items?: number[]; }
interface Owner { group: Group; }
const donor: number[] = [2];
function sibling(): void { const hidden: number[] = [3]; }
let owner: Owner | undefined = { group: { items: [1] } };
console.log(owner?.group.items?.includes(1));
console.log(owner?.group.items?.[0]);
`
	result := tsox.MutationSites("main.ts", source)
	if len(result.Diagnostics) != 0 || len(result.ReceiverEffects) != 2 {
		t.Fatalf("sites: %+v", result)
	}
	for index, site := range result.ReceiverEffects {
		assertText(t, source, site.Operand, []string{"1", "0"}[index])
		if len(site.Targets) != 3 {
			t.Fatalf("targets: %+v", site.Targets)
		}
		if site.Targets[0].Place != "owner" || !site.Targets[0].Clearable || site.Targets[1].Place != "owner!.group" || site.Targets[1].Clearable || site.Targets[2].Place != "owner!.group.items" || !site.Targets[2].Clearable {
			t.Fatalf("owner prefixes: %+v", site.Targets)
		}
		if !reflect.DeepEqual(site.Targets[2].Donors, []string{"donor"}) {
			t.Fatalf("out-of-scope donor leaked: %+v", site.Targets[2])
		}
	}
}

func TestReceiverEffectSitesExcludeUnstablePathsAndReadonlyWrites(t *testing.T) {
	source := `interface Holder { readonly items: number[]; }
const holder: Holder = { items: [1] };
const fixed: number[] = [1];
const donor: number[] = [2];
function receiver(): number[] { return fixed; }
function index(): number { return 0; }
console.log(holder.items[0]);
console.log(fixed[0]);
console.log(receiver()[0]);
const rows: number[][] = [[1]];
console.log(rows[index()][0]);
rows[0] = donor;
if (true) console.log(rows[0][0]);
for (let i = 0; i < rows[0][0]; i++) { }
function withDefault(n: number = rows[0][0]): number { return n; }
`
	result := tsox.MutationSites("main.ts", source)
	if len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
	if len(result.ReceiverEffects) != 0 {
		t.Fatalf("ineligible paths: %+v", result.ReceiverEffects)
	}
}
