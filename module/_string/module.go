// Package _string is the gopy port of CPython's Modules/_string.c.
// It exposes the two helpers that Lib/string.py's Formatter class
// delegates to at runtime: formatter_parser iterates over the literal
// and replacement-field segments of a format string, and
// formatter_field_name_split splits a field-name like "0.attr[key]"
// into its first component plus an attribute/index iterator.
//
// CPython: Modules/_string.c

package _string

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_string", buildModule)
}

// buildModule materializes the _string module dict. Mirrors the
// PyInit__string entry point.
//
// CPython: Modules/_string.c:368 PyInit__string
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_string")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"formatter_parser", objects.NewBuiltinFunction("formatter_parser", formatterParser)},
		{"formatter_field_name_split", objects.NewBuiltinFunction("formatter_field_name_split", formatterFieldNameSplit)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// formatter_parser
// ---------------------------------------------------------------------------

// formatterParser implements _string.formatter_parser(format_string).
// It iterates over the format string, yielding one 4-tuple per
// replacement field:
//
//	(literal_text, field_name, format_spec, conversion)
//
// literal_text is the text before the field; the other three
// components come from "{field_name!conversion:format_spec}". The
// last tuple may have field_name==None when the string ends with a
// literal tail and no trailing "{".
//
// CPython: Modules/_string.c:238 _string_formatter_parser_impl
func formatterParser(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: formatter_parser() takes exactly 1 argument (%d given)", len(args))
	}
	s, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: formatter_parser() argument must be str, not %s", args[0].Type().Name)
	}

	segments, err := parseFormatString(s.Value())
	if err != nil {
		return nil, err
	}

	// Materialise all tuples up front and wrap them in a list iterator.
	tuples := make([]objects.Object, 0, len(segments))
	for _, seg := range segments {
		tup, err := formatSegmentTuple(seg)
		if err != nil {
			return nil, err
		}
		tuples = append(tuples, tup)
	}
	return objects.Iter(objects.NewList(tuples))
}

// formatSegment holds one element produced by parseFormatString.
type formatSegment struct {
	literal    string
	fieldName  string // "" when there is no replacement field
	hasField   bool
	formatSpec string
	conversion string // "" or single char
}

// parseFormatString walks s and collects formatSegments. Each
// segment has a literal prefix plus an optional replacement field.
// It mirrors the logic in MarkupIterator_next in _string.c.
//
// CPython: Modules/_string.c:64 MarkupIterator_next
func parseFormatString(s string) ([]formatSegment, error) {
	var segments []formatSegment
	i := 0
	n := len(s)

	for i <= n {
		// Collect literal_text up to the next '{' or '}'.
		litStart := i
		for i < n && s[i] != '{' && s[i] != '}' {
			i++
		}
		literal := s[litStart:i]

		if i == n {
			// End of string. Emit a trailing literal-only segment.
			if literal != "" || len(segments) == 0 {
				segments = append(segments, formatSegment{literal: literal})
			}
			break
		}

		ch := s[i]

		// Escaped braces: "{{" -> "{", "}}" -> "}".
		if ch == '{' && i+1 < n && s[i+1] == '{' {
			literal += "{"
			i += 2
			// Accumulate more literal text before deciding to emit.
			// Re-enter the outer loop to continue accumulating.
			for i < n && s[i] != '{' && s[i] != '}' {
				literal += string(s[i])
				i++
			}
			if i == n {
				segments = append(segments, formatSegment{literal: literal})
				break
			}
			if s[i] == '}' && i+1 < n && s[i+1] == '}' {
				literal += "}"
				i += 2
				continue
			}
			if s[i] == '{' && (i+1 >= n || s[i+1] != '{') {
				// fall through to field parsing below
				ch = s[i]
			} else {
				continue
			}
		}
		if ch == '}' {
			if i+1 < n && s[i+1] == '}' {
				literal += "}"
				i += 2
				continue
			}
			return nil, fmt.Errorf("ValueError: Single '}' encountered in format string")
		}

		// ch == '{' and it is a real replacement field.
		i++ // skip '{'
		// Find matching '}', respecting nested '{...}'.
		fieldStart := i
		depth := 1
		for i < n && depth > 0 {
			if s[i] == '{' {
				depth++
			} else if s[i] == '}' {
				depth--
			}
			if depth > 0 {
				i++
			}
		}
		if depth != 0 {
			return nil, fmt.Errorf("ValueError: Single '{' encountered in format string")
		}
		inner := s[fieldStart:i]
		i++ // skip '}'

		// Split inner into field_name, conversion, format_spec.
		fieldName, conversion, formatSpec, err := splitFieldExpr(inner)
		if err != nil {
			return nil, err
		}
		segments = append(segments, formatSegment{
			literal:    literal,
			fieldName:  fieldName,
			hasField:   true,
			formatSpec: formatSpec,
			conversion: conversion,
		})
	}
	return segments, nil
}

// splitFieldExpr splits "{field_name!conv:spec}" inner content into
// its three components. All three may be empty strings.
//
// CPython: Modules/_string.c:120 parse_field
func splitFieldExpr(inner string) (fieldName, conversion, formatSpec string, err error) {
	// Scan for '!' and ':' at depth 0, respecting nested braces.
	depth := 0
	bangPos := -1
	colonPos := -1
	for idx := 0; idx < len(inner); idx++ {
		switch inner[idx] {
		case '{':
			depth++
		case '}':
			depth--
		case '!':
			if depth == 0 && bangPos < 0 {
				bangPos = idx
			}
		case ':':
			if depth == 0 && colonPos < 0 {
				colonPos = idx
			}
		}
	}

	end := len(inner)
	if colonPos >= 0 {
		formatSpec = inner[colonPos+1 : end]
		end = colonPos
	}
	if bangPos >= 0 && (colonPos < 0 || bangPos < colonPos) {
		convField := inner[bangPos+1 : end]
		if len(convField) != 1 {
			return "", "", "", fmt.Errorf("ValueError: end of string in conversion specifier or too long")
		}
		conversion = convField
		end = bangPos
	}
	fieldName = inner[:end]
	return fieldName, conversion, formatSpec, nil
}

// formatSegmentTuple converts a formatSegment into the 4-tuple that
// formatter_parser yields.
//
// CPython: Modules/_string.c:238 _string_formatter_parser_impl (yield step)
func formatSegmentTuple(seg formatSegment) (objects.Object, error) {
	var fieldNameObj, formatSpecObj, conversionObj objects.Object

	if seg.hasField {
		fieldNameObj = objects.NewStr(seg.fieldName)
		formatSpecObj = objects.NewStr(seg.formatSpec)
		if seg.conversion != "" {
			conversionObj = objects.NewStr(seg.conversion)
		} else {
			conversionObj = objects.None()
		}
	} else {
		fieldNameObj = objects.None()
		formatSpecObj = objects.None()
		conversionObj = objects.None()
	}

	return objects.NewTuple([]objects.Object{
		objects.NewStr(seg.literal),
		fieldNameObj,
		formatSpecObj,
		conversionObj,
	}), nil
}

// ---------------------------------------------------------------------------
// formatter_field_name_split
// ---------------------------------------------------------------------------

// formatterFieldNameSplit implements
// _string.formatter_field_name_split(field_name).
//
// Returns a 2-tuple: (first, iterator). first is an int when the
// leading component is a decimal integer, otherwise a str. iterator
// yields (is_attr, key) 2-tuples for each subsequent component:
// is_attr==True and key==str for ".name" components; is_attr==False
// and key==int-or-str for "[key]" components.
//
// CPython: Modules/_string.c:297 _string_formatter_field_name_split_impl
func formatterFieldNameSplit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: formatter_field_name_split() takes exactly 1 argument (%d given)", len(args))
	}
	s, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: formatter_field_name_split() argument must be str, not %s", args[0].Type().Name)
	}

	first, rest, err := parseFieldName(s.Value())
	if err != nil {
		return nil, err
	}

	// Build the rest iterator from a pre-collected slice of tuples.
	tuples := make([]objects.Object, 0, len(rest))
	for _, comp := range rest {
		tup := fieldNameComponentTuple(comp)
		tuples = append(tuples, tup)
	}
	it, err := objects.Iter(objects.NewList(tuples))
	if err != nil {
		return nil, err
	}

	return objects.NewTuple([]objects.Object{first, it}), nil
}

// fieldNameComponent is one element of the field-name chain after the
// initial component.
type fieldNameComponent struct {
	isAttr bool
	key    objects.Object // str for attributes, int or str for subscripts
}

// parseFieldName splits a field name string into the leading first
// component and the remaining chain. The logic mirrors
// field_name_iterator_init in _string.c.
//
// CPython: Modules/_string.c:177 field_name_iterator_init
func parseFieldName(s string) (first objects.Object, rest []fieldNameComponent, err error) {
	if s == "" {
		return objects.NewStr(""), nil, nil
	}

	// The first component runs until the first '.' or '['.
	i := 0
	for i < len(s) && s[i] != '.' && s[i] != '[' {
		i++
	}

	firstStr := s[:i]
	// If firstStr is a non-negative integer, return int; else str.
	if isDigitString(firstStr) {
		n, _ := strconv.ParseInt(firstStr, 10, 64)
		first = objects.NewInt(n)
	} else {
		first = objects.NewStr(firstStr)
	}

	s = s[i:]
	for len(s) > 0 {
		switch s[0] {
		case '.':
			// Attribute access: ".name"
			s = s[1:]
			end := strings.IndexAny(s, ".[")
			var name string
			if end < 0 {
				name = s
				s = ""
			} else {
				name = s[:end]
				s = s[end:]
			}
			rest = append(rest, fieldNameComponent{
				isAttr: true,
				key:    objects.NewStr(name),
			})
		case '[':
			// Subscript access: "[key]"
			end := strings.IndexByte(s, ']')
			if end < 0 {
				return nil, nil, fmt.Errorf("ValueError: Missing ']' in format string")
			}
			keyStr := s[1:end]
			s = s[end+1:]
			var keyObj objects.Object
			if isDigitString(keyStr) {
				n, _ := strconv.ParseInt(keyStr, 10, 64)
				keyObj = objects.NewInt(n)
			} else {
				keyObj = objects.NewStr(keyStr)
			}
			rest = append(rest, fieldNameComponent{
				isAttr: false,
				key:    keyObj,
			})
		default:
			return nil, nil, fmt.Errorf("ValueError: Only '.' or '[' may follow ']' in format field specifier")
		}
	}
	return first, rest, nil
}

// fieldNameComponentTuple builds the (is_attr, key) 2-tuple that the
// rest iterator yields.
//
// CPython: Modules/_string.c:259 formatter_field_name_split_iter
func fieldNameComponentTuple(c fieldNameComponent) objects.Object {
	var isAttr objects.Object
	if c.isAttr {
		isAttr = objects.True()
	} else {
		isAttr = objects.False()
	}
	return objects.NewTuple([]objects.Object{isAttr, c.key})
}

// isDigitString reports whether s is a non-empty string of ASCII
// digits with no leading zero (unless s == "0"). This mirrors the
// check CPython uses to decide whether to return an int or a str for
// the first field-name component.
//
// CPython: Modules/_string.c:151 is_integer
func isDigitString(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}
