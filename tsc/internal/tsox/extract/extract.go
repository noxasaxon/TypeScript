// Package extract constructs the checked, plain-data semantic graph for the
// first TSOX slice.
package extract

import (
	"context"
	"fmt"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/jsnum"
	"github.com/microsoft/typescript-go/internal/scanner"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
	"github.com/microsoft/typescript-go/tsox/graph"
)

const virtualMainPath = "/src/main.ts"

// Extract checks source with one TypeScript checker and either returns the
// first-slice semantic graph or one source-order fence diagnostic.
func Extract(sourcePath string, source string) graph.Result {
	if sourcePath == "" {
		sourcePath = "main.ts"
	}

	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		virtualMainPath: source,
	}, true))
	host := compiler.NewCompilerHost("/src", fs, bundled.LibPath(), nil, nil)
	config := tsoptions.NewParsedCommandLine(
		&core.CompilerOptions{Strict: core.TSTrue},
		[]string{virtualMainPath},
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

	file := program.GetSourceFile(virtualMainPath)
	if file == nil {
		return diagnosticResult(graph.Diagnostic{
			SourcePath: sourcePath,
			Position:   graph.Position{Line: 1, Column: 1},
			Construct:  "SourceFile",
			Message:    "TypeScript program did not load the input source file",
		})
	}

	ctx := context.Background()
	if diagnostics := program.GetSyntacticDiagnostics(ctx, file); len(diagnostics) != 0 {
		return diagnosticResult(typescriptDiagnostic(sourcePath, file, diagnostics[0]))
	}
	if diagnostics := program.GetSemanticDiagnostics(ctx, file); len(diagnostics) != 0 {
		return diagnosticResult(typescriptDiagnostic(sourcePath, file, diagnostics[0]))
	}

	typeChecker, done := program.GetTypeChecker(ctx)
	defer done()

	b := &builder{
		sourcePath:  sourcePath,
		file:        file,
		checker:     typeChecker,
		bindings:    make(map[*ast.Symbol]graph.BindingID),
		nextBinding: 1,
	}
	statements, fence := b.statements(file.Statements.Nodes, true)
	if fence != nil {
		return diagnosticResult(fence.diagnostic)
	}
	return graph.Result{Program: &graph.Program{
		SourcePath: sourcePath,
		Statements: statements,
	}}
}

func diagnosticResult(diagnostic graph.Diagnostic) graph.Result {
	return graph.Result{Diagnostics: []graph.Diagnostic{diagnostic}}
}

func typescriptDiagnostic(sourcePath string, file *ast.SourceFile, diagnostic *ast.Diagnostic) graph.Diagnostic {
	position := graph.Position{Line: 1, Column: 1}
	if diagnostic.File() != nil && diagnostic.Pos() >= 0 {
		line, column := scanner.GetECMALineAndUTF16CharacterOfPosition(file, diagnostic.Pos())
		position = graph.Position{Line: line + 1, Column: int(column) + 1}
	}
	return graph.Diagnostic{
		SourcePath: sourcePath,
		Position:   position,
		Construct:  "TypeScriptDiagnostic",
		Message:    "TypeScript diagnostic: " + diagnostic.String(),
	}
}

type builder struct {
	sourcePath  string
	file        *ast.SourceFile
	checker     *checker.Checker
	bindings    map[*ast.Symbol]graph.BindingID
	nextBinding graph.BindingID
}

type fenceError struct {
	diagnostic graph.Diagnostic
}

func (b *builder) statements(nodes []*ast.Node, topLevel bool) ([]*graph.Statement, *fenceError) {
	var result []*graph.Statement
	for _, node := range nodes {
		statements, fence := b.statement(node, topLevel)
		if fence != nil {
			return nil, fence
		}
		result = append(result, statements...)
	}
	return result, nil
}

func (b *builder) statement(node *ast.Node, topLevel bool) ([]*graph.Statement, *fenceError) {
	switch node.Kind {
	case ast.KindVariableStatement:
		data := node.AsVariableStatement()
		if data.Modifiers() != nil {
			return nil, b.fence(node)
		}
		return b.variableDeclarations(data.DeclarationList)

	case ast.KindExpressionStatement:
		expressionNode := node.AsExpressionStatement().Expression
		if call, ok := b.consoleLogCall(expressionNode); ok {
			if call.QuestionDotToken != nil || call.TypeArguments != nil || len(call.Arguments.Nodes) != 1 {
				return nil, b.fence(expressionNode)
			}
			argument, fence := b.expression(call.Arguments.Nodes[0])
			if fence != nil {
				return nil, fence
			}
			if !isPrintable(argument.Type) {
				return nil, b.fenceWithMessage(call.Arguments.Nodes[0], "unsupported console.log argument type")
			}
			return []*graph.Statement{{
				Kind:      graph.StatementPrint,
				Position:  b.position(node),
				Arguments: []*graph.Expression{argument},
			}}, nil
		}
		expression, fence := b.expression(expressionNode)
		if fence != nil {
			return nil, fence
		}
		return []*graph.Statement{{
			Kind:     graph.StatementExpression,
			Position: b.position(node),
			Value:    expression,
		}}, nil

	case ast.KindIfStatement:
		data := node.AsIfStatement()
		condition, fence := b.booleanExpression(data.Expression)
		if fence != nil {
			return nil, fence
		}
		thenStatements, fence := b.statementBody(data.ThenStatement)
		if fence != nil {
			return nil, fence
		}
		var elseStatements []*graph.Statement
		if data.ElseStatement != nil {
			elseStatements, fence = b.statementBody(data.ElseStatement)
			if fence != nil {
				return nil, fence
			}
		}
		return []*graph.Statement{{
			Kind:      graph.StatementIf,
			Position:  b.position(node),
			Condition: condition,
			Then:      thenStatements,
			Else:      elseStatements,
		}}, nil

	case ast.KindWhileStatement:
		data := node.AsWhileStatement()
		condition, fence := b.booleanExpression(data.Expression)
		if fence != nil {
			return nil, fence
		}
		body, fence := b.statementBody(data.Statement)
		if fence != nil {
			return nil, fence
		}
		return []*graph.Statement{{
			Kind:      graph.StatementWhile,
			Position:  b.position(node),
			Condition: condition,
			Body:      body,
		}}, nil

	case ast.KindForStatement:
		data := node.AsForStatement()
		var init []*graph.Statement
		var fence *fenceError
		if data.Initializer != nil {
			switch data.Initializer.Kind {
			case ast.KindVariableDeclarationList:
				init, fence = b.variableDeclarations(data.Initializer)
			default:
				var expression *graph.Expression
				expression, fence = b.expression(data.Initializer)
				if fence == nil {
					init = []*graph.Statement{{
						Kind:     graph.StatementExpression,
						Position: b.position(data.Initializer),
						Value:    expression,
					}}
				}
			}
			if fence != nil {
				return nil, fence
			}
		}
		var condition *graph.Expression
		if data.Condition != nil {
			condition, fence = b.booleanExpression(data.Condition)
			if fence != nil {
				return nil, fence
			}
		}
		var increment *graph.Expression
		if data.Incrementor != nil {
			increment, fence = b.expression(data.Incrementor)
			if fence != nil {
				return nil, fence
			}
		}
		body, fence := b.statementBody(data.Statement)
		if fence != nil {
			return nil, fence
		}
		return []*graph.Statement{{
			Kind:      graph.StatementFor,
			Position:  b.position(node),
			Init:      init,
			Condition: condition,
			Increment: increment,
			Body:      body,
		}}, nil

	case ast.KindFunctionDeclaration:
		if !topLevel {
			return nil, b.fenceDiagnostic(node, "NestedFunctionDeclaration", "unsupported construct nested function declaration")
		}
		statement, fence := b.functionDeclaration(node)
		if fence != nil {
			return nil, fence
		}
		return []*graph.Statement{statement}, nil

	case ast.KindReturnStatement:
		data := node.AsReturnStatement()
		var value *graph.Expression
		var fence *fenceError
		if data.Expression != nil {
			value, fence = b.expression(data.Expression)
			if fence != nil {
				return nil, fence
			}
		}
		return []*graph.Statement{{
			Kind:     graph.StatementReturn,
			Position: b.position(node),
			Value:    value,
		}}, nil
	}
	return nil, b.fence(node)
}

func (b *builder) statementBody(node *ast.Node) ([]*graph.Statement, *fenceError) {
	if node.Kind == ast.KindBlock {
		return b.statements(node.AsBlock().Statements.Nodes, false)
	}
	return b.statement(node, false)
}

func (b *builder) variableDeclarations(node *ast.Node) ([]*graph.Statement, *fenceError) {
	if node.Kind != ast.KindVariableDeclarationList {
		return nil, b.fence(node)
	}
	data := node.AsVariableDeclarationList()
	flags := node.Flags & ast.NodeFlagsBlockScoped
	if flags != ast.NodeFlagsLet && flags != ast.NodeFlagsConst {
		return nil, b.fence(node)
	}

	result := make([]*graph.Statement, 0, len(data.Declarations.Nodes))
	for _, declarationNode := range data.Declarations.Nodes {
		declaration := declarationNode.AsVariableDeclaration()
		nameNode := declaration.Name()
		if nameNode == nil || nameNode.Kind != ast.KindIdentifier || declaration.Initializer == nil || declaration.ExclamationToken != nil {
			return nil, b.fence(declarationNode)
		}
		if declaration.Type != nil {
			if fence := b.validateTypeNode(declaration.Type); fence != nil {
				return nil, fence
			}
		}
		value, fence := b.expression(declaration.Initializer)
		if fence != nil {
			return nil, fence
		}
		valueType, fence := b.checkedType(nameNode)
		if fence != nil {
			return nil, fence
		}
		binding, fence := b.binding(nameNode)
		if fence != nil {
			return nil, fence
		}
		result = append(result, &graph.Statement{
			Kind:     graph.StatementVariable,
			Position: b.position(declarationNode),
			Binding:  binding,
			Name:     nameNode.Text(),
			Mutable:  flags == ast.NodeFlagsLet,
			Type:     valueType,
			Value:    value,
		})
	}
	return result, nil
}

func (b *builder) functionDeclaration(node *ast.Node) (*graph.Statement, *fenceError) {
	data := node.AsFunctionDeclaration()
	nameNode := data.Name()
	if hasModifier(data.Modifiers(), ast.KindAsyncKeyword) {
		return nil, b.fenceDiagnostic(node, "AsyncFunction", "unsupported construct async function")
	}
	if nameNode == nil || nameNode.Kind != ast.KindIdentifier || data.Modifiers() != nil || data.AsteriskToken != nil || data.TypeParameters != nil || data.Body == nil || data.Body.Kind != ast.KindBlock {
		return nil, b.fence(node)
	}
	if data.Type != nil {
		if fence := b.validateTypeNode(data.Type); fence != nil {
			return nil, fence
		}
	}
	parameters, fence := b.parameters(data.Parameters.Nodes)
	if fence != nil {
		return nil, fence
	}
	functionType, fence := b.checkedType(nameNode)
	if fence != nil {
		return nil, fence
	}
	if functionType.Kind != graph.TypeFunction || functionType.Result == nil {
		return nil, b.fence(node)
	}
	binding, fence := b.binding(nameNode)
	if fence != nil {
		return nil, fence
	}
	body, fence := b.statements(data.Body.AsBlock().Statements.Nodes, false)
	if fence != nil {
		return nil, fence
	}
	return &graph.Statement{
		Kind:       graph.StatementFunction,
		Position:   b.position(node),
		Binding:    binding,
		Name:       nameNode.Text(),
		Type:       functionType,
		Parameters: parameters,
		ReturnType: *functionType.Result,
		Body:       body,
	}, nil
}

func (b *builder) parameters(nodes []*ast.Node) ([]graph.Parameter, *fenceError) {
	parameters := make([]graph.Parameter, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind != ast.KindParameter {
			return nil, b.fence(node)
		}
		data := node.AsParameterDeclaration()
		nameNode := data.Name()
		if nameNode == nil || nameNode.Kind != ast.KindIdentifier || data.Modifiers() != nil || data.DotDotDotToken != nil || data.QuestionToken != nil || data.Initializer != nil {
			return nil, b.fence(node)
		}
		if data.Type != nil {
			if fence := b.validateTypeNode(data.Type); fence != nil {
				return nil, fence
			}
		}
		parameterType, fence := b.checkedType(nameNode)
		if fence != nil {
			return nil, fence
		}
		binding, fence := b.binding(nameNode)
		if fence != nil {
			return nil, fence
		}
		parameters = append(parameters, graph.Parameter{
			Position: b.position(node),
			Binding:  binding,
			Name:     nameNode.Text(),
			Type:     parameterType,
		})
	}
	return parameters, nil
}

func (b *builder) expression(node *ast.Node) (*graph.Expression, *fenceError) {
	switch node.Kind {
	case ast.KindNumericLiteral:
		return &graph.Expression{
			Kind:     graph.ExpressionNumber,
			Position: b.position(node),
			Type:     graph.Type{Kind: graph.TypeNumber},
			Number:   float64(jsnum.FromString(node.Text())),
		}, nil

	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return &graph.Expression{
			Kind:     graph.ExpressionString,
			Position: b.position(node),
			Type:     graph.Type{Kind: graph.TypeString},
			String:   node.Text(),
		}, nil

	case ast.KindTrueKeyword, ast.KindFalseKeyword:
		return &graph.Expression{
			Kind:     graph.ExpressionBoolean,
			Position: b.position(node),
			Type:     graph.Type{Kind: graph.TypeBoolean},
			Boolean:  node.Kind == ast.KindTrueKeyword,
		}, nil

	case ast.KindIdentifier:
		valueType, fence := b.checkedType(node)
		if fence != nil {
			return nil, fence
		}
		binding, fence := b.binding(node)
		if fence != nil {
			return nil, fence
		}
		return &graph.Expression{
			Kind:     graph.ExpressionIdentifier,
			Position: b.position(node),
			Binding:  binding,
			Type:     valueType,
			Name:     node.Text(),
		}, nil

	case ast.KindBinaryExpression:
		return b.binaryExpression(node)

	case ast.KindPrefixUnaryExpression:
		data := node.AsPrefixUnaryExpression()
		if data.Operator == ast.KindPlusPlusToken || data.Operator == ast.KindMinusMinusToken {
			return b.updateExpression(node, data.Operand, data.Operator, true)
		}
		operator, ok := unaryOperator(data.Operator)
		if !ok {
			return nil, b.fence(node)
		}
		operand, fence := b.expression(data.Operand)
		if fence != nil {
			return nil, fence
		}
		valueType, fence := b.checkedType(node)
		if fence != nil {
			return nil, fence
		}
		if operator == "!" && (operand.Type.Kind != graph.TypeBoolean || valueType.Kind != graph.TypeBoolean) {
			return nil, b.fence(node)
		}
		if operator != "!" && (operand.Type.Kind != graph.TypeNumber || valueType.Kind != graph.TypeNumber) {
			return nil, b.fence(node)
		}
		return &graph.Expression{
			Kind:     graph.ExpressionUnary,
			Position: b.position(node),
			Type:     valueType,
			Operator: operator,
			Operand:  operand,
		}, nil

	case ast.KindPostfixUnaryExpression:
		data := node.AsPostfixUnaryExpression()
		return b.updateExpression(node, data.Operand, data.Operator, false)

	case ast.KindCallExpression:
		if _, ok := b.consoleLogCall(node); ok {
			return nil, b.fence(node)
		}
		data := node.AsCallExpression()
		if data.QuestionDotToken != nil || data.TypeArguments != nil {
			return nil, b.fence(node)
		}
		callee, fence := b.expression(data.Expression)
		if fence != nil {
			return nil, fence
		}
		if !b.supportedCallTarget(data.Expression) {
			if data.Expression.Kind == ast.KindIdentifier {
				return nil, b.fenceWithMessage(data.Expression, "unsupported call target "+data.Expression.Text())
			}
			return nil, b.fence(node)
		}
		if callee.Type.Kind != graph.TypeFunction {
			return nil, b.fence(node)
		}
		arguments := make([]*graph.Expression, 0, len(data.Arguments.Nodes))
		for _, argumentNode := range data.Arguments.Nodes {
			argument, fence := b.expression(argumentNode)
			if fence != nil {
				return nil, fence
			}
			arguments = append(arguments, argument)
		}
		valueType, fence := b.checkedType(node)
		if fence != nil {
			return nil, fence
		}
		return &graph.Expression{
			Kind:      graph.ExpressionCall,
			Position:  b.position(node),
			Type:      valueType,
			Callee:    callee,
			Arguments: arguments,
		}, nil

	case ast.KindTemplateExpression:
		data := node.AsTemplateExpression()
		chunks := []string{data.Head.Text()}
		expressions := make([]*graph.Expression, 0, len(data.TemplateSpans.Nodes))
		for _, spanNode := range data.TemplateSpans.Nodes {
			span := spanNode.AsTemplateSpan()
			expression, fence := b.expression(span.Expression)
			if fence != nil {
				return nil, fence
			}
			if !isPrintable(expression.Type) {
				return nil, b.fenceWithMessage(span.Expression, "unsupported template interpolation type")
			}
			expressions = append(expressions, expression)
			chunks = append(chunks, span.Literal.Text())
		}
		return &graph.Expression{
			Kind:        graph.ExpressionTemplate,
			Position:    b.position(node),
			Type:        graph.Type{Kind: graph.TypeString},
			Chunks:      chunks,
			Expressions: expressions,
		}, nil

	case ast.KindArrowFunction:
		return b.arrowExpression(node)

	case ast.KindParenthesizedExpression:
		return b.expression(node.AsParenthesizedExpression().Expression)
	}
	return nil, b.fence(node)
}

func (b *builder) binaryExpression(node *ast.Node) (*graph.Expression, *fenceError) {
	data := node.AsBinaryExpression()
	operatorKind := data.OperatorToken.Kind
	if operator, ok := assignmentOperator(operatorKind); ok {
		if data.Left.Kind != ast.KindIdentifier {
			return nil, b.fence(data.Left)
		}
		left, fence := b.expression(data.Left)
		if fence != nil {
			return nil, fence
		}
		right, fence := b.expression(data.Right)
		if fence != nil {
			return nil, fence
		}
		valueType, fence := b.checkedType(node)
		if fence != nil {
			return nil, fence
		}
		if !validAssignment(operator, left.Type, right.Type) {
			return nil, b.fence(node)
		}
		return &graph.Expression{
			Kind:     graph.ExpressionAssignment,
			Position: b.position(node),
			Type:     valueType,
			Operator: operator,
			Left:     left,
			Right:    right,
		}, nil
	}

	operator, ok := binaryOperator(operatorKind)
	if !ok {
		return nil, b.fence(node)
	}
	left, fence := b.expression(data.Left)
	if fence != nil {
		return nil, fence
	}
	right, fence := b.expression(data.Right)
	if fence != nil {
		return nil, fence
	}
	valueType, fence := b.checkedType(node)
	if fence != nil {
		return nil, fence
	}
	if !validBinary(operator, left.Type, right.Type, valueType) {
		return nil, b.fence(node)
	}
	return &graph.Expression{
		Kind:     graph.ExpressionBinary,
		Position: b.position(node),
		Type:     valueType,
		Operator: operator,
		Left:     left,
		Right:    right,
	}, nil
}

func (b *builder) updateExpression(node *ast.Node, operandNode *ast.Node, operatorKind ast.Kind, prefix bool) (*graph.Expression, *fenceError) {
	operator, ok := updateOperator(operatorKind)
	if !ok || operandNode.Kind != ast.KindIdentifier {
		return nil, b.fence(node)
	}
	operand, fence := b.expression(operandNode)
	if fence != nil {
		return nil, fence
	}
	valueType, fence := b.checkedType(node)
	if fence != nil {
		return nil, fence
	}
	if operand.Type.Kind != graph.TypeNumber || valueType.Kind != graph.TypeNumber {
		return nil, b.fence(node)
	}
	return &graph.Expression{
		Kind:     graph.ExpressionUpdate,
		Position: b.position(node),
		Type:     valueType,
		Operator: operator,
		Prefix:   prefix,
		Operand:  operand,
	}, nil
}

func (b *builder) arrowExpression(node *ast.Node) (*graph.Expression, *fenceError) {
	data := node.AsArrowFunction()
	if data.Modifiers() != nil || data.TypeParameters != nil || data.Body == nil || data.Body.Kind != ast.KindBlock {
		return nil, b.fence(node)
	}
	if data.Type != nil {
		if fence := b.validateTypeNode(data.Type); fence != nil {
			return nil, fence
		}
	}
	parameters, fence := b.parameters(data.Parameters.Nodes)
	if fence != nil {
		return nil, fence
	}
	functionType, fence := b.checkedType(node)
	if fence != nil {
		return nil, fence
	}
	if functionType.Kind != graph.TypeFunction || functionType.Result == nil {
		return nil, b.fence(node)
	}
	body, fence := b.statements(data.Body.AsBlock().Statements.Nodes, false)
	if fence != nil {
		return nil, fence
	}
	return &graph.Expression{
		Kind:       graph.ExpressionArrow,
		Position:   b.position(node),
		Type:       functionType,
		Parameters: parameters,
		ReturnType: *functionType.Result,
		Body:       body,
	}, nil
}

func (b *builder) booleanExpression(node *ast.Node) (*graph.Expression, *fenceError) {
	expression, fence := b.expression(node)
	if fence != nil {
		return nil, fence
	}
	if expression.Type.Kind != graph.TypeBoolean {
		return nil, b.fenceWithMessage(node, "unsupported non-boolean condition")
	}
	return expression, nil
}

func (b *builder) checkedType(node *ast.Node) (graph.Type, *fenceError) {
	return b.graphType(b.checker.GetTypeAtLocation(node), node)
}

func (b *builder) validateTypeNode(node *ast.Node) *fenceError {
	switch node.Kind {
	case ast.KindNumberKeyword, ast.KindStringKeyword, ast.KindBooleanKeyword, ast.KindVoidKeyword:
		return nil
	case ast.KindFunctionType:
		data := node.AsFunctionTypeNode()
		if data.TypeParameters != nil || data.Type == nil {
			return b.fence(node)
		}
		for _, parameterNode := range data.Parameters.Nodes {
			if parameterNode.Kind != ast.KindParameter {
				return b.fence(parameterNode)
			}
			parameter := parameterNode.AsParameterDeclaration()
			name := parameter.Name()
			if name == nil || name.Kind != ast.KindIdentifier || parameter.Modifiers() != nil || parameter.DotDotDotToken != nil || parameter.QuestionToken != nil || parameter.Initializer != nil || parameter.Type == nil {
				return b.fence(parameterNode)
			}
			if fence := b.validateTypeNode(parameter.Type); fence != nil {
				return fence
			}
		}
		return b.validateTypeNode(data.Type)
	default:
		return b.fence(node)
	}
}

func (b *builder) graphType(value *checker.Type, node *ast.Node) (graph.Type, *fenceError) {
	flags := value.Flags()
	switch {
	case flags&checker.TypeFlagsNumberLike != 0:
		return graph.Type{Kind: graph.TypeNumber}, nil
	case flags&checker.TypeFlagsStringLike != 0:
		return graph.Type{Kind: graph.TypeString}, nil
	case flags&checker.TypeFlagsBooleanLike != 0:
		return graph.Type{Kind: graph.TypeBoolean}, nil
	case flags&checker.TypeFlagsVoidLike != 0:
		return graph.Type{Kind: graph.TypeVoid}, nil
	}

	signatures := b.checker.GetSignaturesOfType(value, checker.SignatureKindCall)
	if len(signatures) != 1 {
		return graph.Type{}, b.fenceWithMessage(node, "unsupported checked type "+b.checker.TypeToString(value))
	}
	signature := signatures[0]
	parameterTypes := make([]graph.Type, 0, len(signature.Parameters()))
	for _, parameter := range signature.Parameters() {
		parameterType, fence := b.graphType(b.checker.GetTypeOfSymbol(parameter), node)
		if fence != nil {
			return graph.Type{}, fence
		}
		parameterTypes = append(parameterTypes, parameterType)
	}
	resultType, fence := b.graphType(b.checker.GetReturnTypeOfSignature(signature), node)
	if fence != nil {
		return graph.Type{}, fence
	}
	return graph.FunctionType(parameterTypes, resultType), nil
}

func (b *builder) binding(node *ast.Node) (graph.BindingID, *fenceError) {
	symbol := b.checker.GetSymbolAtLocation(node)
	if symbol == nil {
		return 0, b.fenceWithMessage(node, "identifier has no checker symbol")
	}
	if binding, ok := b.bindings[symbol]; ok {
		return binding, nil
	}
	binding := b.nextBinding
	b.nextBinding++
	b.bindings[symbol] = binding
	return binding, nil
}

func (b *builder) position(node *ast.Node) graph.Position {
	pos := scanner.GetTokenPosOfNode(node, b.file, false)
	line, column := scanner.GetECMALineAndUTF16CharacterOfPosition(b.file, pos)
	return graph.Position{Line: line + 1, Column: int(column) + 1}
}

func (b *builder) fence(node *ast.Node) *fenceError {
	construct := strings.TrimPrefix(node.Kind.String(), "Kind")
	return b.fenceDiagnostic(node, construct, "unsupported construct "+construct)
}

func (b *builder) fenceWithMessage(node *ast.Node, message string) *fenceError {
	construct := strings.TrimPrefix(node.Kind.String(), "Kind")
	return b.fenceDiagnostic(node, construct, message)
}

func (b *builder) fenceDiagnostic(node *ast.Node, construct string, message string) *fenceError {
	return &fenceError{diagnostic: graph.Diagnostic{
		SourcePath: b.sourcePath,
		Position:   b.position(node),
		Construct:  construct,
		Message:    message,
	}}
}

func hasModifier(modifiers *ast.ModifierList, kind ast.Kind) bool {
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == kind {
			return true
		}
	}
	return false
}

func (b *builder) consoleLogCall(node *ast.Node) (*ast.CallExpression, bool) {
	if node.Kind != ast.KindCallExpression {
		return nil, false
	}
	call := node.AsCallExpression()
	if call.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	property := call.Expression.AsPropertyAccessExpression()
	name := property.Name()
	if property.QuestionDotToken != nil || property.Expression.Kind != ast.KindIdentifier || name == nil || name.Kind != ast.KindIdentifier {
		return nil, false
	}
	if property.Expression.Text() != "console" || name.Text() != "log" {
		return nil, false
	}
	globalConsole := b.checker.GetGlobalSymbol("console", ast.SymbolFlagsValue, nil)
	return call, globalConsole != nil && b.checker.GetSymbolAtLocation(property.Expression) == globalConsole
}

// supportedCallTarget keeps calls inside the first slice tied to source-owned
// functions. A checker type alone is not enough here: bundled declarations such
// as parseFloat have perfectly usable function types, but their implementations
// are outside the graph/emitter contract. Source function declarations,
// parameters, arrows, and variables initialized from those values are safe to
// lower. In particular, this also rejects an alias such as
// `const parse = parseFloat` while accepting function-valued closures.
func (b *builder) supportedCallTarget(node *ast.Node) bool {
	return b.supportedCallValue(node, make(map[*ast.Symbol]bool))
}

func (b *builder) supportedCallValue(node *ast.Node, seen map[*ast.Symbol]bool) bool {
	switch node.Kind {
	case ast.KindIdentifier:
		return b.supportedSourceSymbol(b.checker.GetSymbolAtLocation(node), seen)
	case ast.KindParenthesizedExpression:
		return b.supportedCallValue(node.AsParenthesizedExpression().Expression, seen)
	case ast.KindArrowFunction:
		return true
	case ast.KindCallExpression:
		return b.supportedCallValue(node.AsCallExpression().Expression, seen)
	default:
		return false
	}
}

func (b *builder) supportedSourceSymbol(symbol *ast.Symbol, seen map[*ast.Symbol]bool) bool {
	if symbol == nil || len(symbol.Declarations) == 0 || seen[symbol] {
		return false
	}
	seen[symbol] = true
	for _, declaration := range symbol.Declarations {
		if ast.GetSourceFileOfNode(declaration) != b.file {
			continue
		}
		switch declaration.Kind {
		case ast.KindFunctionDeclaration, ast.KindParameter:
			return true
		case ast.KindVariableDeclaration:
			initializer := declaration.AsVariableDeclaration().Initializer
			if initializer != nil && b.supportedCallValue(initializer, seen) {
				return true
			}
		}
	}
	return false
}

func unaryOperator(kind ast.Kind) (string, bool) {
	switch kind {
	case ast.KindPlusToken:
		return "+", true
	case ast.KindMinusToken:
		return "-", true
	case ast.KindExclamationToken:
		return "!", true
	default:
		return "", false
	}
}

func updateOperator(kind ast.Kind) (string, bool) {
	switch kind {
	case ast.KindPlusPlusToken:
		return "++", true
	case ast.KindMinusMinusToken:
		return "--", true
	default:
		return "", false
	}
}

func assignmentOperator(kind ast.Kind) (string, bool) {
	switch kind {
	case ast.KindEqualsToken:
		return "=", true
	case ast.KindPlusEqualsToken:
		return "+=", true
	case ast.KindMinusEqualsToken:
		return "-=", true
	case ast.KindAsteriskEqualsToken:
		return "*=", true
	case ast.KindSlashEqualsToken:
		return "/=", true
	case ast.KindPercentEqualsToken:
		return "%=", true
	default:
		return "", false
	}
}

func binaryOperator(kind ast.Kind) (string, bool) {
	switch kind {
	case ast.KindPlusToken:
		return "+", true
	case ast.KindMinusToken:
		return "-", true
	case ast.KindAsteriskToken:
		return "*", true
	case ast.KindSlashToken:
		return "/", true
	case ast.KindPercentToken:
		return "%", true
	case ast.KindLessThanToken:
		return "<", true
	case ast.KindLessThanEqualsToken:
		return "<=", true
	case ast.KindGreaterThanToken:
		return ">", true
	case ast.KindGreaterThanEqualsToken:
		return ">=", true
	case ast.KindEqualsEqualsEqualsToken:
		return "===", true
	case ast.KindExclamationEqualsEqualsToken:
		return "!==", true
	case ast.KindAmpersandAmpersandToken:
		return "&&", true
	case ast.KindBarBarToken:
		return "||", true
	default:
		return "", false
	}
}

func validAssignment(operator string, left graph.Type, right graph.Type) bool {
	if operator == "=" {
		return left.Kind == right.Kind
	}
	if operator == "+=" && left.Kind == graph.TypeString {
		return right.Kind == graph.TypeString || right.Kind == graph.TypeNumber
	}
	return left.Kind == graph.TypeNumber && right.Kind == graph.TypeNumber
}

func isPrintable(valueType graph.Type) bool {
	return valueType.Kind == graph.TypeNumber || valueType.Kind == graph.TypeString || valueType.Kind == graph.TypeBoolean
}

func validBinary(operator string, left graph.Type, right graph.Type, result graph.Type) bool {
	switch operator {
	case "+":
		if result.Kind == graph.TypeString {
			return left.Kind == graph.TypeString && (right.Kind == graph.TypeString || right.Kind == graph.TypeNumber)
		}
		return left.Kind == graph.TypeNumber && right.Kind == graph.TypeNumber && result.Kind == graph.TypeNumber
	case "-", "*", "/", "%":
		return left.Kind == graph.TypeNumber && right.Kind == graph.TypeNumber && result.Kind == graph.TypeNumber
	case "<", "<=", ">", ">=":
		return left.Kind == graph.TypeNumber && right.Kind == graph.TypeNumber && result.Kind == graph.TypeBoolean
	case "===", "!==":
		return left.Kind == right.Kind && (left.Kind == graph.TypeNumber || left.Kind == graph.TypeString || left.Kind == graph.TypeBoolean) && result.Kind == graph.TypeBoolean
	case "&&", "||":
		return left.Kind == graph.TypeBoolean && right.Kind == graph.TypeBoolean && result.Kind == graph.TypeBoolean
	default:
		panic(fmt.Sprintf("validBinary called with unknown operator %q", operator))
	}
}
