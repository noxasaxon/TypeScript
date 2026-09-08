package checked

import (
	"context"
	"fmt"
	"github.com/microsoft/typescript-go/internal/ast"
)

type CallableBodyTarget struct {
	Cell     int
	Instance int
	Template string
	Node     *ast.Node
}
type CallableBodyInput struct {
	Program  *Program
	Instance int
	Template string
	Node     *ast.Node
	Targets  map[*ast.Symbol]CallableBodyTarget
}

// CallableBodyInputs is a provisional source-model handoff, not entry admission.
// It retains actual node/symbol identity in the same immutable compiler Program.
func CallableBodyInputs(p *Program, name string) ([]CallableBodyInput, error) {
	plan, err := discoverCallableEntry(p, name)
	if err != nil {
		return nil, err
	}
	templates := map[string]*ast.Node{}
	symbols := map[string]*ast.Symbol{}
	for _, t := range plan.Templates {
		templates[t.Source.ID] = t.node
	}
	// Resolve declaration and reference identities with the same actual checker.
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	for _, t := range plan.Templates {
		for _, param := range t.node.Parameters() {
			symbols[fmt.Sprintf("%s@%d", ast.GetSourceFileOfNode(param).FileName(), param.Pos())] = c.GetSymbolAtLocation(param.Name())
		}
	}
	var out []CallableBodyInput
	seen := map[int]bool{}
	var visit func(int)
	visit = func(id int) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true
		instance := plan.Instances[id-1]
		node := templates[instance.Template]
		input := CallableBodyInput{Program: p, Instance: id, Template: instance.Template, Node: node, Targets: map[*ast.Symbol]CallableBodyTarget{}}
		// Actual source-modeled module cells remain candidates. This retains
		// their lexical identity without certifying startup or immutability.
		for symbol, cell := range plan.moduleCells {
			if cell.Value == 0 {
				continue
			}
			value := plan.Values[cell.Value-1]
			if value.Kind != "callable-instance" {
				continue
			}
			target := plan.Instances[value.Instance-1]
			for _, template := range plan.Templates {
				if template.Source.ID == target.Template && template.Async {
					input.Targets[symbol] = CallableBodyTarget{Cell: cell.ID, Instance: target.ID, Template: target.Template, Node: template.node}
				}
			}
		}
		for _, capture := range instance.Captures {
			if capture.Value == 0 {
				continue
			}
			value := plan.Values[capture.Value-1]
			if value.Kind != "callable-instance" {
				continue
			}
			target := plan.Instances[value.Instance-1]
			tn := templates[target.Template]
			async := false
			for _, t := range plan.Templates {
				if t.Source.ID == target.Template {
					async = t.Async
				}
			}
			if async && symbols[capture.Binding] != nil {
				input.Targets[symbols[capture.Binding]] = CallableBodyTarget{Cell: capture.ID, Instance: target.ID, Template: target.Template, Node: tn}
			}
		}
		out = append(out, input)
		for _, edge := range instance.Returns {
			visit(edge.TargetInstance)
		}
	}
	value := plan.Values[plan.EntryValue-1]
	if value.Kind != "callable-instance" {
		return nil, fmt.Errorf("computed source entry is unresolved")
	}
	visit(value.Instance)
	// Retain complete actual async module bodies, including calls which do not
	// occur in a return expression. Invocation reachability remains unproved.
	for _, instance := range plan.Instances {
		for _, template := range plan.Templates {
			if template.Source.ID == instance.Template && template.Async && template.node.Kind == ast.KindFunctionDeclaration {
				visit(instance.ID)
			}
		}
	}
	return out, nil
}
