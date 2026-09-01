package extract

import (
	"context"
	"slices"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/scanner"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
)

const testMainPath = "/src/main.ts"

func TestForEachChildSupportsDepthFirstTraversal(t *testing.T) {
	t.Parallel()

	file := checkedSourceFile(t, `const total = 1 + 2;`)

	var directChildren []ast.Kind
	stopped := file.ForEachChild(func(child *ast.Node) bool {
		directChildren = append(directChildren, child.Kind)
		return false
	})
	if stopped {
		t.Fatal("ForEachChild reported an early stop when every visitor call returned false")
	}
	if want := []ast.Kind{ast.KindVariableStatement, ast.KindEndOfFile}; !slices.Equal(directChildren, want) {
		t.Fatalf("direct source-file children: got %v, want %v", directChildren, want)
	}

	var depthFirst []ast.Kind
	var walk func(*ast.Node)
	walk = func(node *ast.Node) {
		depthFirst = append(depthFirst, node.Kind)
		node.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	file.ForEachChild(func(child *ast.Node) bool {
		walk(child)
		return false
	})

	wantInOrder := []ast.Kind{
		ast.KindVariableStatement,
		ast.KindVariableDeclarationList,
		ast.KindVariableDeclaration,
		ast.KindIdentifier,
		ast.KindBinaryExpression,
		ast.KindNumericLiteral,
		ast.KindPlusToken,
		ast.KindNumericLiteral,
		ast.KindEndOfFile,
	}
	if !slices.Equal(depthFirst, wantInOrder) {
		t.Fatalf("depth-first traversal: got %v, want %v", depthFirst, wantInOrder)
	}

	var visited int
	if stopped := file.ForEachChild(func(*ast.Node) bool {
		visited++
		return true
	}); !stopped {
		t.Fatal("ForEachChild did not report the visitor's early stop")
	}
	if visited != 1 {
		t.Fatalf("early-stop visitor calls: got %d, want 1", visited)
	}
}

func TestNodeTokenPositionMapsToECMALineAndUTF16Column(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		source        string
		statement     int
		wantLine      int
		wantCharacter core.UTF16Offset
		wantTrivia    bool
	}{
		{
			name:          "UTF-16 column",
			source:        `"😀"; if (true) {}`,
			statement:     1,
			wantLine:      0,
			wantCharacter: 6,
		},
		{
			name:          "leading trivia",
			source:        "const value = 1;\n// comment before construct\n  if (value) {}\n",
			statement:     1,
			wantLine:      2,
			wantCharacter: 2,
			wantTrivia:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := checkedSourceFile(t, tt.source)
			node := file.Statements.Nodes[tt.statement]
			tokenPos := scanner.GetTokenPosOfNode(node, file, false)
			line, character := scanner.GetECMALineAndUTF16CharacterOfPosition(file, tokenPos)
			if line != tt.wantLine || character != tt.wantCharacter {
				t.Fatalf("node position: got %d:%d, want %d:%d", line, character, tt.wantLine, tt.wantCharacter)
			}
			if tt.wantTrivia && tokenPos <= node.Pos() {
				t.Fatalf("token position %d did not skip trivia from node position %d", tokenPos, node.Pos())
			}
		})
	}
}

func TestCheckerResolvesNamedShapesAccessesArraysAndForOf(t *testing.T) {
	t.Parallel()

	file, typeChecker := checkedFileAndChecker(t, `interface Point { x: number; y: number; }
type Box = { value: Point };
const point: Point = { x: 1, y: 2 };
const box: Box = { value: point };
const x: number = point.x;
const rows: number[][] = [[1, 2]];
const first: number = rows[0][0];
for (const value of rows[0]) { console.log(value); }`)

	pointDeclaration := file.Statements.Nodes[0]
	pointName := pointDeclaration.AsInterfaceDeclaration().Name()
	pointSymbol := typeChecker.GetSymbolAtLocation(pointName)
	if pointSymbol == nil {
		t.Fatal("interface name has no symbol")
	}
	pointVariable := variableNameAt(t, file, 2)
	pointType := typeChecker.GetTypeAtLocation(pointVariable)
	if pointType.Symbol() != pointSymbol {
		t.Fatalf("Point value type symbol: got %p, want interface symbol %p", pointType.Symbol(), pointSymbol)
	}
	if len(pointSymbol.Declarations) != 1 || pointSymbol.Declarations[0] != pointDeclaration {
		t.Fatalf("Point symbol declarations: got %v, want the interface declaration", pointSymbol.Declarations)
	}

	boxDeclaration := file.Statements.Nodes[1]
	boxSymbol := typeChecker.GetSymbolAtLocation(boxDeclaration.AsTypeAliasDeclaration().Name())
	boxType := typeChecker.GetTypeAtLocation(variableNameAt(t, file, 3))
	if boxType.Alias() == nil || boxType.Alias().Symbol() != boxSymbol {
		t.Fatalf("Box value type alias symbol does not resolve to its declaration")
	}

	xInitializer := variableInitializerAt(t, file, 4)
	property := xInitializer.AsPropertyAccessExpression()
	resolvedProperty := typeChecker.GetSymbolAtLocation(property.Name())
	if resolvedProperty == nil || resolvedProperty != typeChecker.GetPropertyOfType(pointType, "x") {
		t.Fatal("property access name did not resolve to Point.x")
	}

	rowsType := typeChecker.GetTypeAtLocation(variableNameAt(t, file, 5))
	if !typeChecker.IsArrayType(rowsType) {
		t.Fatalf("rows type is not an array: %s", typeChecker.TypeToString(rowsType))
	}
	rowType := typeChecker.GetElementTypeOfArrayType(rowsType)
	if rowType == nil || !typeChecker.IsArrayType(rowType) {
		t.Fatalf("rows element is not an array: %v", rowType)
	}
	elementType := typeChecker.GetElementTypeOfArrayType(rowType)
	if elementType == nil || typeChecker.TypeToString(elementType) != "number" {
		t.Fatalf("nested array element: got %v, want number", elementType)
	}

	firstInitializer := variableInitializerAt(t, file, 6)
	if got := typeChecker.TypeToString(typeChecker.GetTypeAtLocation(firstInitializer)); got != "number" {
		t.Fatalf("element access type: got %q, want number", got)
	}
	forOf := file.Statements.Nodes[7].AsForInOrOfStatement()
	loopName := forOf.Initializer.AsVariableDeclarationList().Declarations.Nodes[0].AsVariableDeclaration().Name()
	if got := typeChecker.TypeToString(typeChecker.GetTypeAtLocation(loopName)); got != "number" {
		t.Fatalf("for-of binding type: got %q, want number", got)
	}
}

func variableNameAt(t *testing.T, file *ast.SourceFile, statement int) *ast.Node {
	t.Helper()
	return variableDeclarationAt(t, file, statement).Name()
}

func variableInitializerAt(t *testing.T, file *ast.SourceFile, statement int) *ast.Node {
	t.Helper()
	declaration := variableDeclarationAt(t, file, statement)
	if declaration.Initializer == nil {
		t.Fatalf("statement %d does not have an initialized declaration", statement)
	}
	return declaration.Initializer
}

func variableDeclarationAt(t *testing.T, file *ast.SourceFile, statement int) *ast.VariableDeclaration {
	t.Helper()
	declarations := file.Statements.Nodes[statement].AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes
	if len(declarations) != 1 {
		t.Fatalf("statement %d has %d declarations, want 1", statement, len(declarations))
	}
	return declarations[0].AsVariableDeclaration()
}

func checkedSourceFile(t *testing.T, source string) *ast.SourceFile {
	t.Helper()
	file, _ := checkedFileAndChecker(t, source)
	return file
}

func checkedFileAndChecker(t *testing.T, source string) (*ast.SourceFile, *checker.Checker) {
	t.Helper()

	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		testMainPath: source,
	}, true))
	host := compiler.NewCompilerHost("/src", fs, bundled.LibPath(), nil, nil)
	config := tsoptions.NewParsedCommandLine(
		&core.CompilerOptions{Strict: core.TSTrue},
		[]string{testMainPath},
		tspath.ComparePathsOptions{
			UseCaseSensitiveFileNames: true,
			CurrentDirectory:          "/src",
		},
	)
	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         config,
		Host:           host,
		SingleThreaded: core.TSTrue,
	})
	program.BindSourceFiles()

	file := program.GetSourceFile(testMainPath)
	if file == nil {
		t.Fatalf("program did not load %s; files: %v", testMainPath, program.GetSourceFiles())
	}
	if diagnostics := program.GetSemanticDiagnostics(context.Background(), file); len(diagnostics) != 0 {
		for _, diagnostic := range diagnostics {
			t.Errorf("unexpected diagnostic: %s (code %d)", diagnostic.String(), diagnostic.Code())
		}
		t.Fatal("expected no semantic diagnostics")
	}

	typeChecker, done := program.GetTypeChecker(context.Background())
	t.Cleanup(done)
	return file, typeChecker
}
