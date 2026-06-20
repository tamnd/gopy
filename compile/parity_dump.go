// Compile-side parity dumpers. Render the same L1 / L3 / L4 text
// the patched CPython oracle scripts emit, so test/gate/ can diff
// the two trees byte-for-byte.
//
// L1: codegen output. Mirrors _PyCompile_CodeGen + ApplyLabelMap.
// L3: cfg-pipeline output. Mirrors _PyCompile_OptimizeCfg (nparams=0,
//     firstlineno=1, like the optimize_cfg test entry).
// L4: assembled code object. Mirrors compile() builtin + every nested
//     code object reachable via co_consts.
//
// The format strings here are byte-identical with their Python
// counterparts in test/cpython/patches/oracle_common.py +
// test/cpython/patches/assemble_oracle.py. Drift between the two
// sides is a real gopy divergence and must be tracked via the
// skip-list, never papered over by changing one side.
//
// CPython: Python/compile.c:1607 _PyCompile_CodeGen
// CPython: Python/flowgraph.c:4126 _PyCompile_OptimizeCfg
// CPython: Python/compile.c:353 _PyAST_Compile

package compile

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/symtable"
)

// CompileAndDumpL1 parses + codegens mod and returns the L1 dump
// text matching codegen_oracle.py's output (top-level unit only).
// The returned string is the byte-equality target for the
// test/gate/codegen_parity_test.go diff.
//
// CPython: Modules/_testinternalcapi.c:728 compiler_codegen +
// test/cpython/patches/codegen_oracle.py
func CompileAndDumpL1(mod ast.Mod, filename string, optimize int) (string, error) {
	unit, err := codegenOnly(mod, filename, optimize)
	if err != nil {
		return "", err
	}
	unit.Seq.ApplyLabelMap(hasJumpTarget)
	return DumpUnitL1(unit), nil
}

// CompileAndDumpL3 parses + codegens mod, runs the cfg optimize
// pipeline with nparams=0, firstlineno=1 (the parameters
// _PyCompile_OptimizeCfg passes), and returns the L3 dump text
// matching optimize_oracle.py's output.
//
// CPython: Python/flowgraph.c:4126 _PyCompile_OptimizeCfg +
// test/cpython/patches/optimize_oracle.py
func CompileAndDumpL3(mod ast.Mod, filename string, optimize, nlocals int) (string, error) {
	unit, err := codegenOnly(mod, filename, optimize)
	if err != nil {
		return "", err
	}
	unit.Seq.ApplyLabelMap(hasJumpTarget)
	consts := append([]any(nil), unit.Consts...)
	g := cfgFromSequence(unit.Seq)
	if err := cfgOptimizeCodeUnit(g, &consts, nlocals, 0, 1); err != nil {
		return "", fmt.Errorf("compile: dump L3: cfg optimize: %w", err)
	}
	optimized := &Sequence{}
	if _, _, err := cfgOptimizedCfgToInstructionSequence(g, unit, finalizeFlags(unit), optimized); err != nil {
		return "", fmt.Errorf("compile: dump L3: cfg to sequence: %w", err)
	}
	dumpUnit := *unit
	dumpUnit.Seq = optimized
	dumpUnit.Consts = consts
	optimized.ApplyLabelMap(hasJumpTarget)
	return DumpUnitL1(&dumpUnit), nil
}

// CompileAndDumpL4 runs the full compile pipeline on mod and returns
// one L4 dump string per nested code object, in walk_code_objects
// order: top-level first, then each nested code reachable via
// co_consts, depth-first.
//
// CPython: Python/compile.c:353 _PyAST_Compile +
// test/cpython/patches/assemble_oracle.py
func CompileAndDumpL4(mod ast.Mod, filename string, optimize int) ([]string, error) {
	co, err := Compile(mod, filename, optimize)
	if err != nil {
		return nil, err
	}
	codes := walkCodeObjects(co)
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = DumpCodeL4(c)
	}
	return out, nil
}

// codegenOnly runs the front-end (future + preprocess + symtable +
// Codegen) without the cfg / assemble passes. Used by L1 / L3 dumps
// so the seq matches what _PyCompile_CodeGen returns: post-codegen
// but pre-assemble.
//
// CPython: Python/compile.c:1607 _PyCompile_CodeGen
func codegenOnly(mod ast.Mod, filename string, optimize int) (*Unit, error) {
	ff, err := future.FromAST(mod, filename)
	if err != nil {
		return nil, err
	}
	ast.Preprocess(mod, ast.PreprocessOptions{
		Filename:       filename,
		OptimizeLevel:  optimize,
		FFFeatures:     ff.Bits,
		EnableWarnings: true,
	})
	st, err := symtable.Build(mod, filename, ff)
	if err != nil {
		return nil, err
	}
	c := NewCompiler(filename, optimize, ff, st)
	scope := c.Symtable.Top
	if scope == nil {
		return nil, fmt.Errorf("compile: symtable has no top entry")
	}
	return c.Codegen(scope, mod)
}

// DumpUnitL1 renders unit in the format produced by
// oracle_common.dump_instruction_sequence: argcount, posonlyargcount,
// kwonlyargcount, consts list, instructions list.
//
// CPython: test/cpython/patches/oracle_common.py
// dump_instruction_sequence
func DumpUnitL1(unit *Unit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "argcount: %d\n", unit.Argcount)
	fmt.Fprintf(&b, "posonlyargcount: %d\n", unit.PosOnlyArgCount)
	fmt.Fprintf(&b, "kwonlyargcount: %d\n", unit.KwOnlyArgCount)
	fmt.Fprintf(&b, "consts (%d):\n", len(unit.Consts))
	for i, c := range unit.Consts {
		fmt.Fprintf(&b, "  [%d] %s\n", i, formatConstRepr(c))
	}
	fmt.Fprintf(&b, "instructions (%d):\n", len(unit.Seq.Instrs))
	for i, ins := range unit.Seq.Instrs {
		fmt.Fprintf(&b, "  %s\n", formatInstr(i, ins))
	}
	return b.String()
}

// formatInstr renders one Instr in the oracle_common format. The
// arg field shows "None" for opcodes that don't carry an oparg
// (mirrors Py_None vs int decisions in
// InstructionSequenceType_get_instructions_impl).
//
// CPython: test/cpython/patches/oracle_common.py format_instruction
func formatInstr(idx int, ins Instr) string {
	loc := ins.Loc
	name := ins.Op.Name()
	if name == "" {
		name = fmt.Sprintf("OP%d", ins.Op)
	}
	var argStr string
	if ins.Op.HasArg() {
		argStr = strconv.FormatInt(int64(ins.Oparg), 10)
	} else {
		argStr = "None"
	}
	return fmt.Sprintf("[%d] %s (%d) arg: %s loc: %d:%d:%d:%d",
		idx, name, int(ins.Op), argStr,
		loc.Lineno, loc.EndLineno, loc.ColOffset, loc.EndColOffset)
}

// DumpCodeL4 renders one code object in the format produced by
// assemble_oracle.dump_code. Each field gets one line; the byte
// blobs are hex-encoded.
//
// CPython: test/cpython/patches/assemble_oracle.py dump_code
func DumpCodeL4(co *Code) string {
	var b strings.Builder
	fmt.Fprintf(&b, "co_name: %s\n", pyReprStr(co.Name))
	fmt.Fprintf(&b, "co_qualname: %s\n", pyReprStr(co.Qualname))
	fmt.Fprintf(&b, "co_argcount: %d\n", co.Argcount)
	fmt.Fprintf(&b, "co_posonlyargcount: %d\n", co.PosOnlyArgCount)
	fmt.Fprintf(&b, "co_kwonlyargcount: %d\n", co.KwOnlyArgCount)
	fmt.Fprintf(&b, "co_nlocals: %d\n", co.NLocals)
	fmt.Fprintf(&b, "co_stacksize: %d\n", co.Stacksize)
	fmt.Fprintf(&b, "co_flags: 0x%x\n", co.Flags)
	fmt.Fprintf(&b, "co_firstlineno: %d\n", co.Firstlineno)
	writeStringTuple(&b, "co_names", co.Names)
	writeStringTuple(&b, "co_varnames", co.VarNames)
	writeStringTuple(&b, "co_cellvars", co.CellVars)
	writeStringTuple(&b, "co_freevars", co.FreeVars)
	writeConstTuple(&b, "co_consts", co.Consts)
	fmt.Fprintf(&b, "co_code (%d bytes): %s\n", len(co.Code), hex.EncodeToString(co.Code))
	fmt.Fprintf(&b, "co_linetable (%d bytes): %s\n", len(co.Linetable), hex.EncodeToString(co.Linetable))
	fmt.Fprintf(&b, "co_exceptiontable (%d bytes): %s\n", len(co.ExceptionTable), hex.EncodeToString(co.ExceptionTable))
	return b.String()
}

// writeStringTuple emits "name: ()" for empty, otherwise "name (N):"
// followed by one repr per line. Mirrors format_tuple for the four
// string-tuple fields (names, varnames, cellvars, freevars).
//
// CPython: test/cpython/patches/assemble_oracle.py format_tuple
func writeStringTuple(b *strings.Builder, name string, values []string) {
	if len(values) == 0 {
		fmt.Fprintf(b, "%s: ()\n", name)
		return
	}
	fmt.Fprintf(b, "%s (%d):\n", name, len(values))
	for i, v := range values {
		fmt.Fprintf(b, "  [%d] %s\n", i, pyReprStr(v))
	}
}

// writeConstTuple emits the co_consts list. formatConstRepr renders
// each item via Python repr(), with the same code-object marker the
// oracle uses.
//
// CPython: test/cpython/patches/assemble_oracle.py format_tuple +
// oracle_common.format_const
func writeConstTuple(b *strings.Builder, name string, values []any) {
	if len(values) == 0 {
		fmt.Fprintf(b, "%s: ()\n", name)
		return
	}
	fmt.Fprintf(b, "%s (%d):\n", name, len(values))
	for i, v := range values {
		fmt.Fprintf(b, "  [%d] %s\n", i, formatConstRepr(v))
	}
}

// walkCodeObjects yields co and every nested code reachable via
// co_consts, depth-first. Matches assemble_oracle.walk_code_objects.
//
// CPython: test/cpython/patches/assemble_oracle.py walk_code_objects
func walkCodeObjects(co *Code) []*Code {
	var out []*Code
	var walk func(c *Code)
	walk = func(c *Code) {
		out = append(out, c)
		for _, v := range c.Consts {
			if child, ok := v.(*Code); ok {
				walk(child)
			}
		}
	}
	walk(co)
	return out
}

// formatConstRepr renders one raw const value (the []any payload
// codegen stashes on Unit.Consts and the assembler propagates to
// co_consts) in Python repr() form. For code objects we emit the
// short marker "<code 'qualname' firstlineno=N>" matching
// oracle_common.format_const.
//
// CPython: test/cpython/patches/oracle_common.py format_const
func formatConstRepr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return pyReprFloat(x)
	case complex128:
		return pyReprComplex(x)
	case string:
		return pyReprStr(x)
	case []byte:
		return pyReprBytes(x)
	case *ConstTuple:
		return formatTupleRepr(x.Values)
	case *ConstSlice:
		return "slice(" + formatConstRepr(x.Start) + ", " +
			formatConstRepr(x.Stop) + ", " + formatConstRepr(x.Step) + ")"
	case *Unit:
		return fmt.Sprintf("<code '%s' firstlineno=%d>", x.Qualname, x.FirstLineno)
	case *Code:
		return fmt.Sprintf("<code '%s' firstlineno=%d>", x.Qualname, x.Firstlineno)
	default:
		return fmt.Sprintf("<unsupported %T>", v)
	}
}

// formatTupleRepr emits Python's repr for a tuple of consts. Empty
// tuple is "()", single-element tuple has a trailing comma, others
// are comma-space separated.
//
// CPython: Objects/tupleobject.c:432 tuplerepr
func formatTupleRepr(items []any) string {
	if len(items) == 0 {
		return "()"
	}
	parts := make([]string, len(items))
	for i, v := range items {
		parts[i] = formatConstRepr(v)
	}
	if len(items) == 1 {
		return "(" + parts[0] + ",)"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// pyReprStr produces Python repr() for a string. Uses single quotes
// by default; double quotes if the string contains a single quote
// but no double quote. Escapes non-printable / non-ASCII chars the
// way CPython does (\\, \n, \r, \t, \xNN, \uNNNN, \UNNNNNNNN).
//
// CPython: Objects/unicodeobject.c:13096 unicode_repr
func pyReprStr(s string) string {
	hasSingle := strings.ContainsRune(s, '\'')
	hasDouble := strings.ContainsRune(s, '"')
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case rune(quote):
			b.WriteByte('\\')
			b.WriteByte(quote)
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			switch {
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&b, "\\x%02x", r)
			case r < 0x7f:
				b.WriteRune(r)
			case r < 0x10000:
				if unicode.IsPrint(r) {
					b.WriteRune(r)
				} else {
					fmt.Fprintf(&b, "\\u%04x", r)
				}
			default:
				if unicode.IsPrint(r) {
					b.WriteRune(r)
				} else {
					fmt.Fprintf(&b, "\\U%08x", r)
				}
			}
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// pyReprFloat emits Python repr() for a float64. Matches CPython
// PyFloat_Repr: round-trip shortest decimal, with "inf", "-inf",
// "nan" for the special values.
//
// CPython: Objects/floatobject.c:418 float_repr
func pyReprFloat(x float64) string {
	if math.IsNaN(x) {
		return "nan"
	}
	if math.IsInf(x, 1) {
		return "inf"
	}
	if math.IsInf(x, -1) {
		return "-inf"
	}
	s := strconv.FormatFloat(x, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// pyReprComplex emits Python repr() for a complex128. Matches CPython
// complex_repr: "(re+imj)" with parentheses, or just "imj" for pure
// imaginary literals. Both halves use pyReprFloat.
//
// CPython: Objects/complexobject.c:407 complex_repr
func pyReprComplex(c complex128) string {
	re, im := real(c), imag(c)
	if re == 0 && !math.Signbit(re) {
		return pyReprFloat(im) + "j"
	}
	sign := "+"
	if im < 0 || (im == 0 && math.Signbit(im)) {
		sign = ""
	}
	return "(" + pyReprFloat(re) + sign + pyReprFloat(im) + "j)"
}

// pyReprBytes emits Python repr() for a bytes payload. Same
// quote-selection rules as pyReprStr; non-printable bytes use \xNN.
//
// CPython: Objects/bytesobject.c:1086 bytes_repr
func pyReprBytes(b []byte) string {
	hasSingle := false
	hasDouble := false
	for _, c := range b {
		if c == '\'' {
			hasSingle = true
		}
		if c == '"' {
			hasDouble = true
		}
	}
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	var out strings.Builder
	out.WriteByte('b')
	out.WriteByte(quote)
	for _, c := range b {
		switch {
		case c == quote:
			out.WriteByte('\\')
			out.WriteByte(c)
		case c == '\\':
			out.WriteString("\\\\")
		case c == '\n':
			out.WriteString("\\n")
		case c == '\r':
			out.WriteString("\\r")
		case c == '\t':
			out.WriteString("\\t")
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&out, "\\x%02x", c)
		default:
			out.WriteByte(c)
		}
	}
	out.WriteByte(quote)
	return out.String()
}
