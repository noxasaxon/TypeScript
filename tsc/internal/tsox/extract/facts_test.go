package extract

import (
	"context"
	"slices"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
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

func checkedSourceFile(t *testing.T, source string) *ast.SourceFile {
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

	return file
}
