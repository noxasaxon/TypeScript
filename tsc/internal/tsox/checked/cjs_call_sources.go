package checked

import (
	"context"
	"fmt"
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
)

type CJSArgumentSource struct {
	Source                   SourceNodeID
	Node                     *ast.Node `json:"-"`
	CheckedType              string
	ScalarOnNormalEvaluation string
	ConstructorOwnFields     []CJSArgumentField
	Dependencies             []SourceNodeID
	Pending                  []string
}
type CJSArgumentField struct {
	Name  string
	Value CJSArgumentSource
}
type CJSCaptureSource struct {
	Cell               CJSBodyCell
	InitialSourceValue CJSStateValue
	BindingImmutable   bool
	Pending            []string
}
type CJSCallControl struct {
	Source SourceNodeID
	Edge   string
}
type CJSCallSourcePlan struct {
	Source                 SourceNodeID
	Call                   *ast.Node `json:"-"`
	Import                 RuntimeImportBinding
	Callable               *CJSCallableBodyInput
	CalleeFirst            SourceNodeID
	ThisArgument           string
	Arguments              []CJSArgumentSource
	MissingFormalPositions []int
	ArgumentMappingPending bool
	Captures               []CJSCaptureSource
	Controls               []CJSCallControl
	EnclosingFunction      *SourceNodeID
	Pending                []string
	// Remains false: source linkage is not a same-flow evaluated-call proof.
	EvaluatedCertificate bool
}

func CJSCallSources(snapshot *CJSBodySourceSnapshot) ([]CJSCallSourcePlan, error) {
	if snapshot == nil || snapshot.Program == nil || snapshot.Plans == nil {
		return nil, fmt.Errorf("actual source snapshot required")
	}
	p := snapshot.Program
	inputs := map[*ast.Symbol]*CJSCallableBodyInput{}
	inputFailures := map[*ast.Symbol]string{}
	for _, binding := range snapshot.Plans.Imports {
		if binding.Format != "commonjs" || binding.Form == "namespace" {
			continue
		}
		input, err := CJSBodyInput(p, snapshot.States[binding.Module], binding.Requested)
		if err != nil {
			inputFailures[binding.CheckerLocal] = err.Error()
			continue
		}
		inputs[binding.CheckerLocal] = input
	}
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	var out []CJSCallSourcePlan
	for _, binding := range snapshot.Plans.Imports {
		if binding.Format != "commonjs" || binding.Form == "namespace" {
			continue
		}
		file := p.Compiler.GetSourceFile(binding.Importer)
		if file == nil || p.Files[file] == "" {
			return nil, fmt.Errorf("import belongs to different Program")
		}
		state := snapshot.States[binding.Module]
		input := inputs[binding.CheckerLocal]
		var visit func(*ast.Node) bool
		visit = func(n *ast.Node) bool {
			if n.Kind == ast.KindCallExpression {
				call := n.AsCallExpression()
				if call.Expression.Kind == ast.KindIdentifier && c.GetSymbolAtLocation(call.Expression) == binding.CheckerLocal {
					site := CJSCallSourcePlan{Source: sourceNodeID(file, n), Call: n, Import: binding, Callable: input, CalleeFirst: sourceNodeID(file, call.Expression), ThisArgument: "undefined-direct-import", Pending: []string{"actual caller SemanticFlow operation and evaluated ValueIDs required", "CJS import snapshot/default and native startup dependency unfulfilled", "actual body argument/default/receiver/result and all-path effect proof required", "ProgramInstance cells and persistent mutation/escape invariant required"}}
					if call.QuestionDotToken != nil {
						site.Pending = append(site.Pending, "optional imported call requires selected-edge invocation contract")
					}
					if call.Arguments != nil {
						for _, arg := range call.Arguments.Nodes {
							site.Arguments = append(site.Arguments, cjsArgumentSource(file, arg, c))
							if arg.Kind == ast.KindSpreadElement {
								site.ArgumentMappingPending = true
								site.Pending = append(site.Pending, "spread iterator evaluation and actual argument sequence required")
							}
						}
					}
					if input == nil {
						site.Pending = append(site.Pending, "actual callable selection unresolved: "+inputFailures[binding.CheckerLocal])
					} else {
						if !site.ArgumentMappingPending {
							for i := len(site.Arguments); i < len(input.Callable.Template.Node.Parameters()); i++ {
								parameter := input.Callable.Template.Node.Parameters()[i].AsParameterDeclaration()
								if parameter.DotDotDotToken != nil {
									site.Pending = append(site.Pending, "actual rest array construction required")
									break
								}
								site.MissingFormalPositions = append(site.MissingFormalPositions, i)
							}
						}
						for _, cell := range input.Cells {
							current := state.Cells[cell.Cell]
							site.Captures = append(site.Captures, CJSCaptureSource{cell, current.Value, current.Immutable, []string{"initial source-state observation is not current pre-call value", "binding constness does not prove captured object immutability or nonescape"}})
						}
					}
					child := n
					for parent := n.Parent; parent != nil; parent = parent.Parent {
						if ast.IsFunctionLike(parent) {
							id := sourceNodeID(file, parent)
							site.EnclosingFunction = &id
							break
						}
						edge := ""
						switch parent.Kind {
						case ast.KindConditionalExpression:
							x := parent.AsConditionalExpression()
							if child == x.WhenTrue {
								edge = "truthy"
							} else if child == x.WhenFalse {
								edge = "falsy"
							} else {
								edge = "condition"
							}
						case ast.KindIfStatement:
							x := parent.AsIfStatement()
							if child == x.ThenStatement {
								edge = "then"
							} else if child == x.ElseStatement {
								edge = "else"
							} else {
								edge = "condition"
							}
						case ast.KindWhileStatement, ast.KindDoStatement, ast.KindForStatement, ast.KindTryStatement:
							edge = "source-region"
						}
						if edge != "" {
							site.Controls = append(site.Controls, CJSCallControl{sourceNodeID(file, parent), edge})
						}
						child = parent
					}
					slices.Reverse(site.Controls)
					out = append(out, site)
				}
			}
			return n.ForEachChild(visit)
		}
		file.AsNode().ForEachChild(visit)
	}
	return out, nil
}
func cjsArgumentSource(file *ast.SourceFile, n *ast.Node, c *checker.Checker) CJSArgumentSource {
	out := CJSArgumentSource{Source: sourceNodeID(file, n), Node: n, CheckedType: c.TypeToString(c.GetTypeAtLocation(n)), Pending: []string{"actual evaluated caller ValueID and source order required"}}
	switch n.Kind {
	case ast.KindStringLiteral:
		out.ScalarOnNormalEvaluation = "string"
	case ast.KindNumericLiteral:
		out.ScalarOnNormalEvaluation = "number"
	case ast.KindTrueKeyword, ast.KindFalseKeyword:
		out.ScalarOnNormalEvaluation = "boolean"
	case ast.KindNullKeyword:
		out.ScalarOnNormalEvaluation = "null"
	case ast.KindParenthesizedExpression:
		out.ScalarOnNormalEvaluation = cjsArgumentSource(file, n.AsParenthesizedExpression().Expression, c).ScalarOnNormalEvaluation
	case ast.KindBinaryExpression:
		x := n.AsBinaryExpression()
		a, z := cjsArgumentSource(file, x.Left, c), cjsArgumentSource(file, x.Right, c)
		switch x.OperatorToken.Kind {
		case ast.KindPlusToken, ast.KindMinusToken, ast.KindAsteriskToken, ast.KindSlashToken, ast.KindPercentToken, ast.KindAsteriskAsteriskToken:
			if a.ScalarOnNormalEvaluation == "number" && z.ScalarOnNormalEvaluation == "number" {
				out.ScalarOnNormalEvaluation = "number"
			}
		}
	case ast.KindObjectLiteralExpression:
		out.Pending = append(out.Pending, "original constructor identity, prototype, own-field layout, alias/escape/effect proof required")
		for _, member := range n.AsObjectLiteralExpression().Properties.Nodes {
			if member.Kind != ast.KindPropertyAssignment {
				out.Pending = append(out.Pending, "ordered spread/accessor/method semantics required")
				continue
			}
			x := member.AsPropertyAssignment()
			if x.Name().Kind != ast.KindIdentifier && x.Name().Kind != ast.KindStringLiteral {
				out.Pending = append(out.Pending, "computed property key evaluation required")
				continue
			}
			if x.Name().Text() == "__proto__" {
				out.Pending = append(out.Pending, "object literal prototype setter semantics required")
				continue
			}
			out.ConstructorOwnFields = append(out.ConstructorOwnFields, CJSArgumentField{x.Name().Text(), cjsArgumentSource(file, x.Initializer, c)})
		}
	}
	if out.ScalarOnNormalEvaluation == "" && n.Kind != ast.KindObjectLiteralExpression {
		out.Pending = append(out.Pending, "actual source operation/guard/call-result domain proof unresolved")
	}
	var visit func(*ast.Node) bool
	visit = func(x *ast.Node) bool {
		if x.Kind == ast.KindIdentifier {
			if symbol := c.GetSymbolAtLocation(x); symbol != nil {
				for _, decl := range symbol.Declarations {
					source := ast.GetSourceFileOfNode(decl)
					if source != nil {
						out.Dependencies = append(out.Dependencies, sourceNodeID(source, decl))
					}
				}
			}
		}
		return x.ForEachChild(visit)
	}
	visit(n)
	return out
}
