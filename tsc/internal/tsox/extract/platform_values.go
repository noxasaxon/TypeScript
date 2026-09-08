package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	"path"
	"strings"
	"unicode/utf8"
)

func (b *builder) platformValueType(value *checker.Type) (graph.Type, bool) {
	if !b.standardEntry || value == nil {
		return graph.Type{}, false
	}
	for _, item := range []struct {
		name string
		kind graph.TypeKind
	}{{"Headers", graph.TypeHeaders}, {"URL", graph.TypeURL}, {"URLSearchParams", graph.TypeURLSearchParams}} {
		symbol := b.checker.GetGlobalSymbol(item.name, ast.SymbolFlagsType, nil)
		if symbol != nil && value.Symbol() == symbol && b.platformLibrarySymbol(symbol) {
			return graph.Type{Kind: item.kind}, true
		}
	}
	return graph.Type{}, false
}
func (b *builder) platformLibrarySymbol(symbol *ast.Symbol) bool {
	if symbol == nil || len(symbol.Declarations) == 0 {
		return false
	}
	for _, declaration := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(declaration)
		if file == nil {
			return false
		}
		name := file.FileName()
		library := path.Dir(name) == path.Clean(bundled.LibPath()) && strings.HasPrefix(path.Base(name), "lib.") && strings.HasSuffix(name, ".d.ts")
		if b.ownsFile(file) || (!library && name != checked.StandardNodeGlobalsPath) {
			return false
		}
	}
	return true
}
func (b *builder) platformGlobal(node *ast.Node, name string) bool {
	if node == nil || node.Kind != ast.KindIdentifier || node.Text() != name {
		return false
	}
	symbol := b.checker.GetGlobalSymbol(name, ast.SymbolFlagsValue, nil)
	return b.platformLibrarySymbol(symbol) && b.checker.GetSymbolAtLocation(node) == symbol
}
func (b *builder) platformMember(node *ast.Node, owner, name string) bool {
	symbol := b.checker.GetGlobalSymbol(owner, ast.SymbolFlagsType, nil)
	if !b.platformLibrarySymbol(symbol) {
		return false
	}
	member := b.checker.GetPropertyOfType(b.checker.GetDeclaredTypeOfSymbol(symbol), name)
	return b.platformLibrarySymbol(member) && b.checker.GetSymbolAtLocation(node) == member
}
func (b *builder) platformValueExpression(node *ast.Node) (*graph.Expression, *fenceError, bool) {
	if !b.standardEntry {
		return nil, nil, false
	}
	makeValue := func(kind graph.PlatformValueKind, typ graph.TypeKind) *graph.Expression {
		return &graph.Expression{Kind: graph.ExpressionPlatformValue, Position: b.position(node), Type: graph.Type{Kind: typ}, Platform: &graph.PlatformValue{Kind: kind, Contract: graph.PlatformNode24Collected}}
	}
	if node.Kind == ast.KindBinaryExpression && node.AsBinaryExpression().OperatorToken.Kind == ast.KindQuestionQuestionToken {
		data := node.AsBinaryExpression()
		if platformNullableStringType(b.checker.GetTypeAtLocation(data.Left)) {
			left, fence := b.expression(data.Left)
			if fence != nil {
				return nil, fence, true
			}
			right, fence := b.expressionForSlot(data.Right, graph.Type{Kind: graph.TypeString})
			if fence != nil {
				return nil, fence, true
			}
			if left.Type.Kind != graph.TypeNullableString || right.Type.Kind != graph.TypeString || right.Type.Optional {
				return nil, b.fenceDiagnostic(node, "NullableString", "nullable string coalescing requires a string fallback"), true
			}
			return &graph.Expression{Kind: graph.ExpressionNullish, Position: b.position(node), Type: graph.Type{Kind: graph.TypeString}, Left: left, Right: right}, nil, true
		}
	}
	if node.Kind == ast.KindCallExpression {
		call := node.AsCallExpression()
		if call.Expression.Kind == ast.KindPropertyAccessExpression {
			member := call.Expression.AsPropertyAccessExpression()
			if member.Name().Text() == "json" && b.platformGlobal(member.Expression, "Response") {
				actual := b.checker.GetPropertyOfType(b.checker.GetTypeAtLocation(member.Expression), "json")
				if member.QuestionDotToken != nil || call.QuestionDotToken != nil || call.TypeArguments != nil || !b.platformLibrarySymbol(actual) || b.checker.GetSymbolAtLocation(member.Name()) != actual {
					return nil, b.fenceDiagnostic(node, "PlatformMember", "Response.json requires its unmodified intrinsic member"), true
				}
				if call.Arguments == nil || len(call.Arguments.Nodes) < 1 || len(call.Arguments.Nodes) > 2 {
					return nil, b.fenceDiagnostic(node, "PlatformMember", "Response.json requires data and optional init"), true
				}
				argument := call.Arguments.Nodes[0]
				value, fence := b.platformJSONData(argument)
				if fence != nil {
					return nil, fence, true
				}
				result := makeValue(graph.PlatformResponseJSON, graph.TypeResponse)
				result.Arguments = []*graph.Expression{value}
				if len(call.Arguments.Nodes) == 2 {
					if fence := b.platformResponseInit(call.Arguments.Nodes[1], result); fence != nil {
						return nil, fence, true
					}
				}
				return result, nil, true
			}
			typ, ok := b.platformValueType(b.checker.GetTypeAtLocation(member.Expression))
			if ok && member.Name().Text() == "get" && (typ.Kind == graph.TypeHeaders || typ.Kind == graph.TypeURLSearchParams) {
				owner, kind := "Headers", graph.PlatformHeadersGet
				if typ.Kind == graph.TypeURLSearchParams {
					owner, kind = "URLSearchParams", graph.PlatformSearchParamsGet
				}
				if call.QuestionDotToken != nil || call.TypeArguments != nil || member.QuestionDotToken != nil || !b.platformMember(member.Name(), owner, "get") {
					return nil, b.fenceDiagnostic(node, "PlatformMember", "get requires an unmodified intrinsic member"), true
				}
				if len(call.Arguments.Nodes) != 1 {
					return nil, b.fenceDiagnostic(node, "PlatformMember", "get requires one evaluated string argument"), true
				}
				receiver, fence := b.expression(member.Expression)
				if fence != nil {
					return nil, fence, true
				}
				argument, fence := b.expression(call.Arguments.Nodes[0])
				if fence != nil {
					return nil, fence, true
				}
				if argument.Type.Kind != graph.TypeString || argument.Type.Optional {
					return nil, b.fenceDiagnostic(node, "PlatformMember", "get key requires an actual string"), true
				}
				result := makeValue(kind, graph.TypeNullableString)
				result.Receiver = receiver
				result.Arguments = []*graph.Expression{argument}
				return result, nil, true
			}
		}
	}
	if node.Kind == ast.KindNewExpression {
		call := node.AsNewExpression()
		kind, typ := graph.PlatformValueKind(""), graph.TypeKind("")
		switch {
		case b.platformGlobal(call.Expression, "Response"):
			kind, typ = graph.PlatformResponseNew, graph.TypeResponse
		case b.platformGlobal(call.Expression, "URL"):
			kind, typ = graph.PlatformURLNew, graph.TypeURL
		default:
			return nil, nil, false
		}
		if call.TypeArguments != nil {
			return nil, b.fenceDiagnostic(node, "PlatformConstructor", "generic intrinsic constructor is unsupported"), true
		}
		result := makeValue(kind, typ)
		if call.Arguments != nil {
			for index, argument := range call.Arguments.Nodes {
				if kind == graph.PlatformResponseNew && index == 1 {
					if fence := b.platformResponseInit(argument, result); fence != nil {
						return nil, fence, true
					}
					continue
				}
				if kind == graph.PlatformResponseNew && index > 1 {
					return nil, b.fenceDiagnostic(argument, "PlatformConstructor", "Response extra arguments require ordered evaluation"), true
				}
				if argument.Kind == ast.KindSpreadElement {
					return nil, b.fenceDiagnostic(argument, "PlatformConstructor", "spread intrinsic operands need ordered expansion"), true
				}
				var value *graph.Expression
				var fence *fenceError
				if argument.Kind == ast.KindNullKeyword {
					value = &graph.Expression{Kind: graph.ExpressionNull, Type: graph.Type{Kind: graph.TypeNull}, Position: b.position(argument)}
				} else {
					value, fence = b.expression(argument)
				}
				if fence != nil {
					return nil, fence, true
				}
				result.Arguments = append(result.Arguments, value)
			}
		}
		if kind == graph.PlatformURLNew {
			if len(result.Arguments) == 0 || len(result.Arguments) > 2 {
				return nil, b.fenceDiagnostic(node, "PlatformConstructor", "URL currently requires one or two string arguments"), true
			}
			for _, arg := range result.Arguments {
				if arg.Type.Kind != graph.TypeString || arg.Type.Optional {
					return nil, b.fenceDiagnostic(node, "PlatformConstructor", "URL coercion currently requires actual string operands"), true
				}
			}
		} else {
			if len(result.Arguments) >= 1 {
				arg := result.Arguments[0]
				if (arg.Type.Kind != graph.TypeString || arg.Type.Optional) && arg.Kind != graph.ExpressionNull && arg.Kind != graph.ExpressionUndefined {
					return nil, b.fenceDiagnostic(node, "PlatformConstructor", "Response body currently requires string, null or undefined"), true
				}
			}
		}
		return result, nil, true
	}
	if node.Kind == ast.KindPropertyAccessExpression {
		property := node.AsPropertyAccessExpression()
		if property.Expression.Kind == ast.KindPropertyAccessExpression {
			env := property.Expression.AsPropertyAccessExpression()
			if env.Name().Text() == "env" && b.platformGlobal(env.Expression, "process") {
				symbol := b.checker.GetSymbolAtLocation(env.Expression)
				valid := len(symbol.Declarations) == 1 && ast.GetSourceFileOfNode(symbol.Declarations[0]).FileName() == checked.StandardNodeGlobalsPath
				actualEnv := b.checker.GetPropertyOfType(b.checker.GetTypeAtLocation(env.Expression), "env")
				if !valid || env.QuestionDotToken != nil || property.QuestionDotToken != nil || b.checker.GetSymbolAtLocation(env.Name()) != actualEnv {
					return nil, b.fenceDiagnostic(node, "PlatformEnv", "environment access requires the unmodified compiler-owned process.env"), true
				}
				switch property.Name().Text() {
				case "constructor", "__defineGetter__", "__defineSetter__", "hasOwnProperty", "__lookupGetter__", "__lookupSetter__", "isPrototypeOf", "propertyIsEnumerable", "toString", "valueOf", "__proto__", "toLocaleString":
					return nil, b.fenceDiagnostic(node, "PlatformEnv", "environment prototype properties require non-string fallback semantics"), true
				}
				result := makeValue(graph.PlatformEnvGet, graph.TypeString)
				result.Type.Optional = true
				result.Arguments = []*graph.Expression{{Kind: graph.ExpressionString, Type: graph.Type{Kind: graph.TypeString}, String: property.Name().Text(), Position: b.position(property.Name())}}
				return result, nil, true
			}
		}
		ownerType := b.standardType(b.checker.GetTypeAtLocation(property.Expression))
		if ownerType == "" {
			if typ, ok := b.platformValueType(b.checker.GetTypeAtLocation(property.Expression)); ok {
				ownerType = typ.Kind
			}
		}
		kind := graph.PlatformValueKind("")
		owner := ""
		switch ownerType {
		case graph.TypeRequest:
			owner = "Request"
			switch property.Name().Text() {
			case "method":
				kind = graph.PlatformRequestMethod
			case "url":
				kind = graph.PlatformRequestURL
			case "headers":
				kind = graph.PlatformRequestHeaders
			}
		case graph.TypeURL:
			owner = "URL"
			if property.Name().Text() == "pathname" {
				kind = graph.PlatformURLPathname
			} else if property.Name().Text() == "searchParams" {
				kind = graph.PlatformURLSearchParams
			}
		}
		if kind == "" {
			return nil, nil, false
		}
		if property.QuestionDotToken != nil || !b.platformMember(property.Name(), owner, property.Name().Text()) {
			return nil, b.fenceDiagnostic(node, "PlatformMember", "member lacks unmodified intrinsic identity"), true
		}
		receiver, fence := b.expression(property.Expression)
		if fence != nil {
			return nil, fence, true
		}
		result := makeValue(kind, graph.TypeString)
		if kind == graph.PlatformRequestHeaders {
			result.Type.Kind = graph.TypeHeaders
		}
		if kind == graph.PlatformURLSearchParams {
			result.Type.Kind = graph.TypeURLSearchParams
		}
		result.Receiver = receiver
		return result, nil, true
	}
	return nil, nil, false
}

// This source domain has a single missing value: null, never undefined.
func platformNullableStringType(value *checker.Type) bool {
	if value == nil || value.Flags()&checker.TypeFlagsUnion == 0 {
		return false
	}
	stringValue, nullValue := false, false
	for _, part := range value.Types() {
		switch {
		case part.Flags()&checker.TypeFlagsNull != 0:
			nullValue = true
		case part.Flags()&checker.TypeFlagsStringLike != 0:
			stringValue = true
		default:
			return false
		}
	}
	return stringValue && nullValue
}

// Fresh data-only init literals cannot escape. Flatten their operand evaluations
// in source order, then perform the WebIDL dictionary conversion in the backend.
func (b *builder) platformResponseInit(node *ast.Node, result *graph.Expression) *fenceError {
	if node.Kind == ast.KindNullKeyword || (node.Kind == ast.KindIdentifier && node.Text() == "undefined" && b.checker.GetTypeAtLocation(node).Flags()&checker.TypeFlagsUndefined != 0) {
		return nil
	}
	if node.Kind != ast.KindObjectLiteralExpression {
		return b.fenceDiagnostic(node, "ResponseInit", "Response init requires a fresh data-only dictionary")
	}
	seen := map[string]bool{}
	for _, item := range node.AsObjectLiteralExpression().Properties.Nodes {
		if item.Kind != ast.KindPropertyAssignment {
			return b.fenceDiagnostic(item, "ResponseInit", "Response init requires explicit data properties")
		}
		property := item.AsPropertyAssignment()
		name := property.Name()
		if name == nil || (name.Kind != ast.KindIdentifier && name.Kind != ast.KindStringLiteral) || seen[name.Text()] {
			return b.fenceDiagnostic(item, "ResponseInit", "Response init requires unique static keys")
		}
		seen[name.Text()] = true
		switch name.Text() {
		case "status":
			value, fence := b.expression(property.Initializer)
			if fence != nil {
				return fence
			}
			if (value.Type.Kind != graph.TypeNumber || value.Type.Optional) && value.Kind != graph.ExpressionUndefined {
				return b.fenceDiagnostic(item, "ResponseInit", "Response status requires a number or undefined")
			}
			result.Arguments = append(result.Arguments, value)
			result.Platform.ResponseInit = append(result.Platform.ResponseInit, "status")
		case "headers":
			headers := property.Initializer
			if headers.Kind != ast.KindObjectLiteralExpression {
				return b.fenceDiagnostic(headers, "ResponseInit", "Response headers currently require a fresh string record")
			}
			keys := map[string]bool{}
			for _, entry := range headers.AsObjectLiteralExpression().Properties.Nodes {
				if entry.Kind != ast.KindPropertyAssignment {
					return b.fenceDiagnostic(entry, "ResponseInit", "header record requires explicit data properties")
				}
				pair := entry.AsPropertyAssignment()
				key := pair.Name()
				if key == nil || (key.Kind != ast.KindIdentifier && key.Kind != ast.KindStringLiteral) || !utf8.ValidString(key.Text()) || key.Text() == "__proto__" || keys[key.Text()] {
					return b.fenceDiagnostic(entry, "ResponseInit", "header record requires unique static own keys")
				}
				keys[key.Text()] = true
				value, fence := b.expression(pair.Initializer)
				if fence != nil {
					return fence
				}
				if value.Type.Kind != graph.TypeString || value.Type.Optional {
					return b.fenceDiagnostic(entry, "ResponseInit", "header record values require actual strings")
				}
				result.Arguments = append(result.Arguments, value)
				result.Platform.ResponseInit = append(result.Platform.ResponseInit, "header:"+key.Text())
			}
		default:
			return b.fenceDiagnostic(item, "ResponseInit", "Response init member requires its dictionary conversion")
		}
	}
	return nil
}
