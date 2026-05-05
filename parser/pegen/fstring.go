// CPython: Parser/action_helpers.c f-string / t-string section.
// Builders the rule actions call to fold middle-pieces and
// {expr} chunks into a JoinedStr or TemplateStr node.

package pegen

import "github.com/tamnd/gopy/ast"

// Conversion characters as the parser sees them. -1 means absent.
const (
	ConvNone  = -1
	ConvStr   = int('s')
	ConvRepr  = int('r')
	ConvASCII = int('a')
)

// JoinedStrFromValues lifts a slice of literal/expression parts
// into a JoinedStr node. Adjacent plain Constant strings are folded
// to keep the AST flat, mirroring _PyPegen_concatenate_strings'
// post-pass.
//
// CPython: Parser/action_helpers.c:1860 _PyPegen_concatenate_strings
func JoinedStrFromValues(values []ast.Expr) *ast.JoinedStr {
	return &ast.JoinedStr{Values: foldAdjacentStrings(values)}
}

// TemplateStrFromValues is the t-string counterpart.
//
// CPython: Parser/action_helpers.c (PEP 750)
func TemplateStrFromValues(values []ast.Expr) *ast.TemplateStr {
	return &ast.TemplateStr{Values: foldAdjacentStrings(values)}
}

// FormattedValueFor builds a FormattedValue from the (expr,
// conversion, format_spec) triple a {expr!c:fmt} rule produces.
//
// CPython: Parser/action_helpers.c:1748 _PyPegen_formatted_value
func FormattedValueFor(expr ast.Expr, conversion int, format ast.Expr) *ast.FormattedValue {
	return &ast.FormattedValue{
		Value:      expr,
		Conversion: conversion,
		FormatSpec: format,
	}
}

// InterpolationFor is the t-string equivalent. The Str field carries
// the source text of the expression, used at runtime by Template.
//
// CPython: Parser/action_helpers.c (PEP 750)
func InterpolationFor(expr ast.Expr, srcText string, conversion int, format ast.Expr) *ast.Interpolation {
	return &ast.Interpolation{
		Value:      expr,
		Str:        srcText,
		Conversion: conversion,
		FormatSpec: format,
	}
}

// DebugLiteral builds the literal "expr=" Constant the {expr=} debug
// form prepends before the formatted value.
//
// CPython: Parser/action_helpers.c:1780 fstring_debug_literal
func DebugLiteral(text string) *ast.Constant {
	return &ast.Constant{Value: text + "="}
}

func foldAdjacentStrings(values []ast.Expr) []ast.Expr {
	if len(values) < 2 {
		return values
	}
	out := make([]ast.Expr, 0, len(values))
	for _, v := range values {
		if c, ok := v.(*ast.Constant); ok {
			if s, ok := c.Value.(string); ok && len(out) > 0 {
				if last, ok := out[len(out)-1].(*ast.Constant); ok {
					if ls, ok := last.Value.(string); ok {
						last.Value = ls + s
						continue
					}
				}
			}
		}
		out = append(out, v)
	}
	return out
}
