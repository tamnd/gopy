// Package symtable is the gopy port of CPython's Modules/symtablemodule.c.
// It exposes the compiler's internal symbol tables to Python so that the
// Lib/symtable.py wrapper can introspect name binding and scope.
//
// CPython: Modules/symtablemodule.c:1 _symtable module
//
// The single entry point _symtable.symtable(source, filename, startstr)
// parses the source, runs the gopy symtable builder (the same one the
// compiler uses), and returns the top "symtable entry" object. Each entry
// mirrors PySTEntryObject: it carries id / name / symbols / varnames /
// children / nested / type / lineno, exactly the surface Lib/symtable.py
// reads.
package symtable

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/symtable"
)

func init() {
	_ = imp.AppendInittab("_symtable", buildModule)
}

// stEntry is the Python-visible wrapper around a symtable.Entry. The whole
// tree is materialized eagerly at symtable() time so that repeated access to
// .children and .symbols returns the same objects, matching CPython where
// every PySTEntryObject already exists once the build completes.
//
// CPython: Include/internal/pycore_symtable.h:88 PySTEntryObject
type stEntry struct {
	objects.Header
	id       int
	name     string
	typ      int
	nested   int
	lineno   int
	symbols  *objects.Dict
	varnames *objects.List
	children *objects.List
}

// stEntryType is the type for symtable entry objects. The member surface
// (id, name, symbols, varnames, children, nested, type, lineno) and the repr
// are a direct port of PySTEntry_Type's ste_memberlist plus ste_repr.
//
// CPython: Python/symtable.c:210 PySTEntry_Type
var stEntryType = func() *objects.Type {
	t := objects.NewType("symtable entry", []*objects.Type{objects.ObjectType()})
	// PySTEntryObject keeps the default identity hash so Lib/symtable.py can use
	// (raw_entry, filename) tuples as WeakValueDictionary keys.
	// CPython: Python/symtable.c:210 PySTEntry_Type (no tp_hash override)
	t.Hash = objects.IdentityHash
	t.Repr = func(o objects.Object) (string, error) {
		e := o.(*stEntry)
		// CPython: Python/symtable.c ste_repr "<symtable entry %U(%R), line %d>"
		return fmt.Sprintf("<symtable entry %s(%d), line %d>", e.name, e.id, e.lineno), nil
	}
	getInt := func(f func(*stEntry) int64) func(objects.Object) (objects.Object, error) {
		return func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(f(o.(*stEntry))), nil
		}
	}
	// CPython: Python/symtable.c:200 ste_memberlist
	objects.SetTypeDescr(t, "id", objects.NewGetSetDescr("id", getInt(func(e *stEntry) int64 { return int64(e.id) }), nil))
	objects.SetTypeDescr(t, "name", objects.NewGetSetDescr("name", func(o objects.Object) (objects.Object, error) {
		return objects.NewStr(o.(*stEntry).name), nil
	}, nil))
	objects.SetTypeDescr(t, "symbols", objects.NewGetSetDescr("symbols", func(o objects.Object) (objects.Object, error) {
		return o.(*stEntry).symbols, nil
	}, nil))
	objects.SetTypeDescr(t, "varnames", objects.NewGetSetDescr("varnames", func(o objects.Object) (objects.Object, error) {
		return o.(*stEntry).varnames, nil
	}, nil))
	objects.SetTypeDescr(t, "children", objects.NewGetSetDescr("children", func(o objects.Object) (objects.Object, error) {
		return o.(*stEntry).children, nil
	}, nil))
	objects.SetTypeDescr(t, "nested", objects.NewGetSetDescr("nested", getInt(func(e *stEntry) int64 { return int64(e.nested) }), nil))
	objects.SetTypeDescr(t, "type", objects.NewGetSetDescr("type", getInt(func(e *stEntry) int64 { return int64(e.typ) }), nil))
	objects.SetTypeDescr(t, "lineno", objects.NewGetSetDescr("lineno", getInt(func(e *stEntry) int64 { return int64(e.lineno) }), nil))
	return t
}()

// wrapEntry builds the Python entry object for a symtable.Entry, recursing
// into its children. Each Entry is wrapped exactly once.
//
// CPython: Python/symtable.c:104 ste_new (one PySTEntryObject per block)
func wrapEntry(e *symtable.Entry) (*stEntry, error) {
	symbols := objects.NewDict()
	for _, name := range e.OrderedSymbols() {
		if err := symbols.SetItem(objects.NewStr(name), objects.NewInt(int64(e.Symbols[name]))); err != nil {
			return nil, err
		}
	}
	varnames := objects.NewList(nil)
	for _, v := range e.Varnames {
		varnames.Append(objects.NewStr(v))
	}
	children := objects.NewList(nil)
	for _, c := range e.Children {
		cw, err := wrapEntry(c)
		if err != nil {
			return nil, err
		}
		children.Append(cw)
	}
	nested := 0
	if e.Nested {
		nested = 1
	}
	w := &stEntry{
		id:       e.ID,
		name:     e.Name,
		typ:      int(e.Type),
		nested:   nested,
		lineno:   e.Loc.Lineno,
		symbols:  symbols,
		varnames: varnames,
		children: children,
	}
	w.Init(stEntryType)
	return w, nil
}

// symtableFunc implements _symtable.symtable(source, filename, startstr).
//
// CPython: Modules/symtablemodule.c:25 _symtable_symtable_impl
func symtableFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: symtable() takes exactly 3 arguments (%d given)", len(args))
	}

	src, isBytes, err := sourceString(args[0])
	if err != nil {
		return nil, err
	}

	filename, ferr := fsDecode(args[1])
	if ferr != nil {
		return nil, ferr
	}

	start, ok := args[2].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: symtable() argument 3 must be str, not %s", args[2].Type().Name)
	}
	var mode parser.Mode
	switch start.Value() {
	case "exec":
		mode = parser.ModeFile
	case "eval":
		mode = parser.ModeEval
	case "single":
		mode = parser.ModeSingle
	default:
		// CPython: Modules/symtablemodule.c:48 ValueError on bad startstr
		return nil, fmt.Errorf("ValueError: symtable() arg 3 must be 'exec' or 'eval' or 'single'")
	}

	// Parse the source. bytes route through ParseBytes, str through
	// ParseString, mirroring _Py_SourceAsString's two paths.
	// CPython: Python/pythonrun.c _Py_SymtableStringObjectFlags
	parsed, perr := parseSource(src, isBytes, filename, mode)
	if perr != nil {
		return nil, perr
	}

	ff, ferr := future.FromAST(parsed, filename)
	if ferr != nil {
		return nil, ferr
	}

	st, berr := symtable.Build(parsed, filename, ff)
	if berr != nil {
		return nil, berr
	}

	return wrapEntry(st.Top)
}

// sourceString extracts the source text from a str / bytes / bytearray
// argument. The bool reports whether the original was a bytes-like object,
// which selects the byte-oriented parser entry.
//
// CPython: Python/pythonrun.c:1380 _Py_SourceAsString
func sourceString(o objects.Object) (string, bool, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), false, nil
	case *objects.Bytes:
		return string(v.Bytes()), true, nil
	case *objects.ByteArray:
		return string(v.Bytes()), true, nil
	}
	return "", false, fmt.Errorf("TypeError: symtable() argument 1 must be string or bytes, not %s", o.Type().Name)
}

// fsDecode mirrors PyUnicode_FSDecoder for the filename argument: a str
// is taken verbatim, a bytes object is decoded through the filesystem
// encoding, and everything else (bytearray, memoryview, list) is rejected
// with a TypeError. CPython runs the clinic converter unicode_fs_decoded
// over arg 2.
//
// CPython: Modules/symtablemodule.c:16 filename: unicode_fs_decoded
// CPython: Objects/unicodeobject.c:4070 PyUnicode_FSDecoder
func fsDecode(o objects.Object) (string, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	}
	return "", fmt.Errorf("TypeError: symtable() argument 2 must be str or bytes, not %s", o.Type().Name)
}

// parseSource routes the source through the byte- or string-oriented parser
// entry under the chosen mode.
//
// CPython: Parser/peg_api.c:8 _PyParser_ASTFromString / _PyParser_ASTFromBytes
func parseSource(src string, isBytes bool, filename string, mode parser.Mode) (ast.Mod, error) {
	if isBytes {
		return parser.ParseBytes([]byte(src), filename, mode)
	}
	return parser.ParseString(src, filename, mode)
}

// buildModule constructs the _symtable module dict and constants.
//
// CPython: Modules/symtablemodule.c:73 symtable_init_constants
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_symtable")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("symtable"), objects.NewBuiltinFunction("symtable", symtableFunc)); err != nil {
		return nil, err
	}

	// CPython: Modules/symtablemodule.c:75 PyModule_AddIntMacro DEF_* / USE
	consts := []struct {
		name string
		val  int64
	}{
		{"USE", int64(symtable.Use)},
		{"DEF_GLOBAL", int64(symtable.DefGlobal)},
		{"DEF_NONLOCAL", int64(symtable.DefNonlocal)},
		{"DEF_LOCAL", int64(symtable.DefLocal)},
		{"DEF_PARAM", int64(symtable.DefParam)},
		{"DEF_TYPE_PARAM", int64(symtable.DefTypeParam)},
		{"DEF_FREE_CLASS", int64(symtable.DefFreeClass)},
		{"DEF_IMPORT", int64(symtable.DefImport)},
		{"DEF_BOUND", int64(symtable.DefBound)},
		{"DEF_ANNOT", int64(symtable.DefAnnot)},
		{"DEF_COMP_ITER", int64(symtable.DefCompIter)},
		{"DEF_COMP_CELL", int64(symtable.DefCompCell)},
		// Block type constants. The numeric order tracks _Py_block_ty.
		// CPython: Modules/symtablemodule.c:88 TYPE_* PyModule_AddIntConstant
		{"TYPE_FUNCTION", int64(symtable.FunctionBlock)},
		{"TYPE_CLASS", int64(symtable.ClassBlock)},
		{"TYPE_MODULE", int64(symtable.ModuleBlock)},
		{"TYPE_ANNOTATION", int64(symtable.AnnotationBlock)},
		{"TYPE_TYPE_ALIAS", int64(symtable.TypeAliasBlock)},
		{"TYPE_TYPE_PARAMETERS", int64(symtable.TypeParametersBlock)},
		{"TYPE_TYPE_VARIABLE", int64(symtable.TypeVariableBlock)},
		// Scope constants.
		// CPython: Modules/symtablemodule.c:107 LOCAL / GLOBAL_* / FREE / CELL
		{"LOCAL", int64(symtable.Local)},
		{"GLOBAL_EXPLICIT", int64(symtable.GlobalExplicit)},
		{"GLOBAL_IMPLICIT", int64(symtable.GlobalImplicit)},
		{"FREE", int64(symtable.Free)},
		{"CELL", int64(symtable.Cell)},
		// Scope packing layout.
		// CPython: Modules/symtablemodule.c:113 SCOPE_OFF / SCOPE_MASK
		{"SCOPE_OFF", int64(symtable.ScopeOffset)},
		{"SCOPE_MASK", int64(symtable.ScopeMask)},
	}
	for _, c := range consts {
		if err := d.SetItem(objects.NewStr(c.name), objects.NewInt(c.val)); err != nil {
			return nil, err
		}
	}
	return m, nil
}
