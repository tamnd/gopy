// Command asdl_go generates ast/nodes_gen.go from
// cpython/Parser/Python.asdl.
//
// The grammar is the original Zephyr-ASDL subset CPython uses:
//
//	module Name { def* }
//	def    = name '=' sum | name '=' product
//	sum    = ctor ('|' ctor)* ('attributes' '(' field* ')')?
//	ctor   = name | name '(' field* ')'
//	product= '(' field* ')' ('attributes' '(' field* ')')?
//	field  = type ('?' | '*' | '?*')? name
//
// `--` introduces a line comment.
//
// Built-in types (identifier, int, string, constant) map to Go strings,
// ints, and `any` for `constant`. Asdl `?` becomes a pointer for
// non-pointer Go types and stays interface-typed for sums. Asdl `*`
// becomes a `Seq[T]`. Asdl `?*` is a `Seq[T]` whose elements may be
// nil (only used for Dict.keys to mark `**` unpack slots).
//
// Run with:
//
//	go run ./tools/asdl_go \
//	  -input=$HOME/github/python/cpython/Parser/Python.asdl \
//	  -output=ast/nodes_gen.go
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"strings"
	"unicode"
)

// Built-in ASDL primitive types and their Go target.
var primTypes = map[string]string{
	"identifier": "string",
	"int":        "int",
	"string":     "string",
	"constant":   "any",
}

func main() {
	in := flag.String("input", "", "path to Python.asdl")
	out := flag.String("output", "", "path to output .go file")
	flag.Parse()
	if *in == "" || *out == "" {
		log.Fatal("both -input and -output required")
	}
	src, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	mod, err := parse(string(src))
	if err != nil {
		log.Fatal(err)
	}
	code, err := emit(mod)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, code, 0o644); err != nil {
		log.Fatal(err)
	}
}

// AST of the asdl source.

type module struct {
	Name string
	Defs []*def
}

type def struct {
	Name    string
	Sum     []*ctor // non-empty for sums
	Product []*field
	Attrs   []*field
}

type ctor struct {
	Name   string
	Fields []*field
}

type field struct {
	Type string
	Quan rune // 0, '?', '*', or 'O' for ?*
	Name string
}

// Lexer.

type token struct {
	kind string // ident, punct, eof
	val  string
}

type lexer struct {
	src string
	pos int
}

func (l *lexer) next() token {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case c == '-' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '-':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		default:
			goto tok
		}
	}
	return token{kind: "eof"}
tok:
	c := l.src[l.pos]
	if isIdentStart(c) {
		start := l.pos
		for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
			l.pos++
		}
		return token{kind: "ident", val: l.src[start:l.pos]}
	}
	// Punctuation: { } ( ) , = | ? *
	if l.pos+1 < len(l.src) && c == '?' && l.src[l.pos+1] == '*' {
		l.pos += 2
		return token{kind: "punct", val: "?*"}
	}
	l.pos++
	return token{kind: "punct", val: string(c)}
}

func (l *lexer) peek() token {
	save := l.pos
	t := l.next()
	l.pos = save
	return t
}

func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentPart(c byte) bool  { return isIdentStart(c) || (c >= '0' && c <= '9') }

// Parser.

func parse(src string) (*module, error) {
	l := &lexer{src: src}
	if t := l.next(); t.val != "module" {
		return nil, fmt.Errorf("expected 'module', got %q", t.val)
	}
	name := l.next()
	if name.kind != "ident" {
		return nil, fmt.Errorf("expected module name, got %q", name.val)
	}
	if t := l.next(); t.val != "{" {
		return nil, fmt.Errorf("expected '{', got %q", t.val)
	}
	mod := &module{Name: name.val}
	for {
		t := l.peek()
		if t.val == "}" {
			l.next()
			break
		}
		if t.kind == "eof" {
			return nil, fmt.Errorf("unexpected eof inside module")
		}
		d, err := parseDef(l)
		if err != nil {
			return nil, err
		}
		mod.Defs = append(mod.Defs, d)
	}
	return mod, nil
}

func parseDef(l *lexer) (*def, error) {
	name := l.next()
	if name.kind != "ident" {
		return nil, fmt.Errorf("expected def name, got %q", name.val)
	}
	if t := l.next(); t.val != "=" {
		return nil, fmt.Errorf("expected '=' after %s, got %q", name.val, t.val)
	}
	d := &def{Name: name.val}
	// Product types start with '('.
	if l.peek().val == "(" {
		fs, err := parseFields(l)
		if err != nil {
			return nil, err
		}
		d.Product = fs
		if l.peek().val == "attributes" {
			l.next()
			fs, err := parseFields(l)
			if err != nil {
				return nil, err
			}
			d.Attrs = fs
		}
		return d, nil
	}
	// Sum: ctor ('|' ctor)*
	for {
		c, err := parseCtor(l)
		if err != nil {
			return nil, err
		}
		d.Sum = append(d.Sum, c)
		if l.peek().val == "|" {
			l.next()
			continue
		}
		break
	}
	if l.peek().val == "attributes" {
		l.next()
		fs, err := parseFields(l)
		if err != nil {
			return nil, err
		}
		d.Attrs = fs
	}
	return d, nil
}

func parseCtor(l *lexer) (*ctor, error) {
	name := l.next()
	if name.kind != "ident" {
		return nil, fmt.Errorf("expected constructor name, got %q", name.val)
	}
	c := &ctor{Name: name.val}
	if l.peek().val == "(" {
		fs, err := parseFields(l)
		if err != nil {
			return nil, err
		}
		c.Fields = fs
	}
	return c, nil
}

func parseFields(l *lexer) ([]*field, error) {
	if t := l.next(); t.val != "(" {
		return nil, fmt.Errorf("expected '(', got %q", t.val)
	}
	var out []*field
	for {
		if l.peek().val == ")" {
			l.next()
			return out, nil
		}
		f, err := parseField(l)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
		if l.peek().val == "," {
			l.next()
		}
	}
}

func parseField(l *lexer) (*field, error) {
	t := l.next()
	if t.kind != "ident" {
		return nil, fmt.Errorf("expected field type, got %q", t.val)
	}
	f := &field{Type: t.val}
	switch l.peek().val {
	case "?*":
		l.next()
		f.Quan = 'O'
	case "?":
		l.next()
		f.Quan = '?'
	case "*":
		l.next()
		f.Quan = '*'
	}
	n := l.next()
	if n.kind != "ident" {
		return nil, fmt.Errorf("expected field name, got %q", n.val)
	}
	f.Name = n.val
	return f, nil
}

// Emitter.

func emit(mod *module) ([]byte, error) {
	// Index sum-type membership: ctor name -> sum name. Lets us know
	// the interface a ctor satisfies (e.g. FunctionDef -> stmt).
	sumOf := map[string]string{}
	sumKind := map[string]bool{} // sum names
	allTypes := map[string]bool{}
	for _, d := range mod.Defs {
		allTypes[d.Name] = true
		if len(d.Sum) > 0 {
			sumKind[d.Name] = true
			for _, c := range d.Sum {
				sumOf[c.Name] = d.Name
			}
		}
	}
	// Resolve ctor-vs-other-type Go-name collisions. The asdl has two
	// kinds of clash:
	//   1. stmt.Expr collides with the expr sum: rename to ExprStmt.
	//   2. type_ignore.TypeIgnore collides with its own sum interface
	//      (single-ctor sum): rename ctor to TypeIgnoreNode.
	goNames := map[string]string{} // goName -> origin ("sum"/"ctor:<sumname>")
	for _, d := range mod.Defs {
		goNames[goName(d.Name)] = "sum"
	}
	for _, d := range mod.Defs {
		for _, c := range d.Sum {
			gn := goName(c.Name)
			if owner, ok := goNames[gn]; ok && owner == "sum" {
				if goName(d.Name) == gn {
					c.Name += "Node"
				} else {
					c.Name += titleASCII(d.Name)
				}
			}
		}
	}

	var buf bytes.Buffer
	fmt.Fprintln(&buf, "// Code generated by tools/asdl_go. DO NOT EDIT.")
	fmt.Fprintln(&buf, "// Source: cpython/Parser/Python.asdl")
	fmt.Fprintln(&buf, "")
	fmt.Fprintln(&buf, "package ast")
	fmt.Fprintln(&buf, "")

	// Emit defs in source order.
	for _, d := range mod.Defs {
		if len(d.Sum) > 0 {
			emitSum(&buf, d, sumKind)
		} else {
			emitProduct(&buf, d, sumKind)
		}
	}

	// Stable order helper: also emit a constant table for enum-like
	// sums (sums whose ctors take no fields) as Go iota constants.
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("gofmt: %w", err)
	}
	return src, nil
}

func emitSum(buf *bytes.Buffer, d *def, sumKind map[string]bool) {
	// Enum sum: all ctors are field-less. Emit as a Go int type.
	enum := true
	for _, c := range d.Sum {
		if len(c.Fields) > 0 {
			enum = false
			break
		}
	}
	if enum {
		fmt.Fprintf(buf, "// %s is the asdl `%s` enum.\n", goName(d.Name), d.Name)
		fmt.Fprintf(buf, "type %s int\n\n", goName(d.Name))
		fmt.Fprintln(buf, "const (")
		for i, c := range d.Sum {
			if i == 0 {
				fmt.Fprintf(buf, "\t%s %s = iota + 1\n", goName(c.Name), goName(d.Name))
			} else {
				fmt.Fprintf(buf, "\t%s\n", goName(c.Name))
			}
		}
		fmt.Fprintln(buf, ")")
		fmt.Fprintln(buf, "")
		// String() method.
		fmt.Fprintf(buf, "// String returns the asdl name.\n")
		fmt.Fprintf(buf, "func (v %s) String() string {\n", goName(d.Name))
		fmt.Fprintln(buf, "\tswitch v {")
		for _, c := range d.Sum {
			fmt.Fprintf(buf, "\tcase %s: return %q\n", goName(c.Name), c.Name)
		}
		fmt.Fprintln(buf, "\t}")
		fmt.Fprintln(buf, "\treturn \"\"")
		fmt.Fprintln(buf, "}")
		fmt.Fprintln(buf, "")
		return
	}
	// Tagged sum: emit interface and per-ctor struct.
	iface := goName(d.Name)
	hasPos := hasPosition(d.Attrs)
	fmt.Fprintf(buf, "// %s is the asdl `%s` sum.\n", iface, d.Name)
	fmt.Fprintf(buf, "type %s interface {\n", iface)
	fmt.Fprintf(buf, "\tis%s()\n", iface)
	if hasPos {
		fmt.Fprintln(buf, "\tPosition() Pos")
		fmt.Fprintln(buf, "\tSetPos(Pos)")
	}
	fmt.Fprintln(buf, "}")
	fmt.Fprintln(buf, "")
	for _, c := range d.Sum {
		emitStruct(buf, c.Name, c.Fields, d.Attrs, sumKind)
		fmt.Fprintf(buf, "func (*%s) is%s() {}\n", goName(c.Name), iface)
		if hasPos {
			fmt.Fprintf(buf, "// Position returns the source location.\n")
			fmt.Fprintf(buf, "func (n *%s) Position() Pos { return n.Pos }\n", goName(c.Name))
			fmt.Fprintf(buf, "// SetPos installs the source location.\n")
			fmt.Fprintf(buf, "func (n *%s) SetPos(p Pos) { n.Pos = p }\n", goName(c.Name))
		}
		fmt.Fprintln(buf, "")
	}
}

func emitProduct(buf *bytes.Buffer, d *def, sumKind map[string]bool) {
	emitStruct(buf, d.Name, d.Product, d.Attrs, sumKind)
	if hasPosition(d.Attrs) {
		fmt.Fprintf(buf, "// Position returns the source location.\n")
		fmt.Fprintf(buf, "func (n *%s) Position() Pos { return n.Pos }\n", goName(d.Name))
		fmt.Fprintf(buf, "// SetPos installs the source location.\n")
		fmt.Fprintf(buf, "func (n *%s) SetPos(p Pos) { n.Pos = p }\n", goName(d.Name))
	}
	fmt.Fprintln(buf, "")
}

func emitStruct(buf *bytes.Buffer, name string, fields, attrs []*field, sumKind map[string]bool) {
	fmt.Fprintf(buf, "// %s is the asdl `%s` node.\n", goName(name), name)
	fmt.Fprintf(buf, "type %s struct {\n", goName(name))
	for _, f := range fields {
		fmt.Fprintf(buf, "\t%s %s\n", goFieldName(f.Name), goFieldType(f, sumKind))
	}
	if hasPosition(attrs) {
		fmt.Fprintln(buf, "\tPos Pos")
	} else {
		// Emit other attribute fields verbatim (rare).
		for _, a := range attrs {
			fmt.Fprintf(buf, "\t%s %s\n", goFieldName(a.Name), goFieldType(a, sumKind))
		}
	}
	fmt.Fprintln(buf, "}")
	fmt.Fprintln(buf, "")
}

func hasPosition(attrs []*field) bool {
	// CPython attaches lineno/col_offset/end_lineno/end_col_offset to
	// node kinds with source positions. We collapse them into a Pos.
	want := map[string]bool{"lineno": true, "col_offset": true, "end_lineno": true, "end_col_offset": true}
	hits := 0
	for _, a := range attrs {
		if want[a.Name] {
			hits++
		}
	}
	return hits >= 2
}

func goFieldType(f *field, sumKind map[string]bool) string {
	base := primGo(f.Type)
	if base == "" {
		base = goName(f.Type)
	}
	isSum := sumKind[f.Type]
	switch f.Quan {
	case '*':
		if isSum {
			return "Seq[" + base + "]"
		}
		if _, prim := primTypes[f.Type]; prim {
			return "Seq[" + base + "]"
		}
		return "Seq[*" + base + "]"
	case 'O': // ?*
		if isSum {
			return "Seq[" + base + "]"
		}
		return "Seq[*" + base + "]"
	case '?':
		if isSum {
			return base
		}
		if _, prim := primTypes[f.Type]; prim {
			return "*" + base
		}
		return "*" + base
	default:
		if isSum {
			return base
		}
		if _, prim := primTypes[f.Type]; prim {
			return base
		}
		return "*" + base
	}
}

func primGo(name string) string { return primTypes[name] }

// readIfExists is a small helper used by the test suite.
func readIfExists(path string) ([]byte, error) { return os.ReadFile(path) }

func titleASCII(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func goName(s string) string {
	// asdl uses CamelCase for ctors and snake_case for sums and
	// products. Convert any underscores to title-case joins so
	// `type_ignore` -> `TypeIgnore`.
	if s == "" {
		return s
	}
	if !strings.ContainsRune(s, '_') {
		r := []rune(s)
		r[0] = unicode.ToUpper(r[0])
		return string(r)
	}
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, "")
}

func goFieldName(s string) string {
	// asdl field names are snake_case. Convert to UpperCamel.
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, "")
}
