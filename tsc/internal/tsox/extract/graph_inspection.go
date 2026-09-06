package extract

import "github.com/microsoft/typescript-go/tsox/graph"

// walkGraphExpressions visits every expression, including nested function bodies
// and parameter defaults. It does not follow calls into separately bound bodies.
func walkGraphExpressions(statements []*graph.Statement, visit func(*graph.Expression)) {
	var expression func(*graph.Expression)
	var body func([]*graph.Statement)
	expression = func(value *graph.Expression) {
		if value == nil {
			return
		}
		visit(value)
		for _, child := range []*graph.Expression{value.Left, value.Right, value.Operand, value.Callee, value.Receiver, value.Index} {
			expression(child)
		}
		for _, child := range value.Arguments {
			expression(child)
		}
		for _, child := range value.Expressions {
			expression(child)
		}
		for _, property := range value.Properties {
			expression(property.Value)
		}
		for _, parameter := range value.Parameters {
			expression(parameter.Default)
		}
		body(value.Body)
	}
	body = func(values []*graph.Statement) {
		for _, statement := range values {
			expression(statement.Value)
			expression(statement.Condition)
			expression(statement.Increment)
			for _, argument := range statement.Arguments {
				expression(argument)
			}
			for _, parameter := range statement.Parameters {
				expression(parameter.Default)
			}
			body(statement.Init)
			body(statement.Then)
			body(statement.Else)
			body(statement.Body)
		}
	}
	body(statements)
}

// indexReadOnly is a conservative effect check for indexed-read scheduling.
// Calls and writes may replace the receiver before the current emitter reads it.
func indexReadOnly(value *graph.Expression) bool {
	if value == nil {
		return true
	}
	switch value.Kind {
	case graph.ExpressionCall, graph.ExpressionMethodCall, graph.ExpressionAssignment, graph.ExpressionUpdate, graph.ExpressionArrow:
		return false
	}
	for _, child := range []*graph.Expression{value.Left, value.Right, value.Operand, value.Callee, value.Receiver, value.Index} {
		if !indexReadOnly(child) {
			return false
		}
	}
	for _, child := range value.Arguments {
		if !indexReadOnly(child) {
			return false
		}
	}
	for _, child := range value.Expressions {
		if !indexReadOnly(child) {
			return false
		}
	}
	for _, property := range value.Properties {
		if !indexReadOnly(property.Value) {
			return false
		}
	}
	return true
}
