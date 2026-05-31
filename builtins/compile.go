// Port of builtin_compile_impl. compile() drives the parser + compiler
// pipeline from Python: take a source string plus a filename and mode,
// return a code object the user can hand to exec() / eval() (those land
// alongside this in spec 1651).
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl

package builtins

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
)

// Compile implements builtins.compile(source, filename, mode,
// flags=0, dont_inherit=False, optimize=-1). The gopy v0.10.1 cut
// accepts source as str (bytes/AST input land with spec 1685 once AST
// objects exist on the Python side); the flags / dont_inherit /
// feature_version arguments are recognized for signature parity but
// only optimize is plumbed into compile.Compile today.
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl
func Compile(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	parsed, err := parseCompileArgs(args, kwargs)
	if err != nil {
		return nil, err
	}
	var mod ast.Mod
	switch {
	case parsed.astMod != nil:
		// Source was a Python _ast object; use the reverse-bridged Go AST directly.
		// CPython: Python/bltinmodule.c:813 builtin_compile_impl PyAST_obj2mod path
		mod = parsed.astMod
	case parsed.sourceBytes != nil:
		mod, err = parser.ParseBytesFlagsVersion(parsed.sourceBytes, parsed.filename, parsed.mode, parsed.flags, parsed.featureVersion)
	default:
		mod, err = parser.ParseStringFlagsVersion(parsed.source, parsed.filename, parsed.mode, parsed.flags, parsed.featureVersion)
	}
	if err != nil {
		// Parser-incomplete sentinel surfaces as SyntaxError to Python
		// so callers like dis._try_compile that fall back from eval to
		// exec can catch and retry. CPython only ever raises
		// SyntaxError out of compile()'s parser stage.
		//
		// CPython: Python/bltinmodule.c:771 builtin_compile_impl
		if errors.Is(err, parser.ErrParserNotImplemented) {
			return nil, fmt.Errorf("SyntaxError: invalid syntax")
		}
		return nil, err
	}
	// PyCF_ONLY_AST: parse-only mode. Return a sentinel Module object
	// so callers like codeop.compile_command and _find_keyword_typos can
	// check for SyntaxError without needing full codegen. A proper
	// Python-side AST object tree lands with the _ast_unparse spec.
	//
	// CPython: Python/bltinmodule.c:813 builtin_compile_impl PyAST_obj2mod
	if parsed.flags&(cfOnlyAST|cfOptimizedAST) != 0 {
		return parseOnlyResult(mod, &parsed)
	}
	cco, err := compile.CompileFlags(mod, parsed.filename, parsed.optimize, parsed.flags)
	if err != nil {
		return nil, err
	}
	return liftCompileCode(cco), nil
}

type compileArgs struct {
	source         string
	sourceBytes    []byte
	astMod         ast.Mod // non-nil when source is a Python _ast object
	filename       string
	mode           parser.Mode
	flags          int
	optimize       int
	featureVersion int // minor-only, e.g. 4 for (3, 4)
}

// parseCompileArgs binds the positional and keyword arguments to the
// compile() signature, validates mode and flags, and reports the same
// errors CPython does (ValueError on bad mode / flags / optimize,
// TypeError on wrong arg types).
//
// CPython: Python/clinic/bltinmodule.c.h builtin_compile clinic block
//
//	plus the validation in builtin_compile_impl
func parseCompileArgs(args []objects.Object, kwargs map[string]objects.Object) (compileArgs, error) {
	bound, err := bindCompileArgs(args, kwargs)
	if err != nil {
		return compileArgs{}, err
	}
	// Check for Python _ast object before falling through to text source.
	// CPython: Python/bltinmodule.c:813 builtin_compile_impl PyAST_Check
	var astMod ast.Mod
	if mod, ok, convErr := PyASTObjectToMod(bound[0]); convErr != nil {
		return compileArgs{}, convErr
	} else if ok {
		astMod = mod
	}
	var source string
	var sourceBytes []byte
	if astMod == nil {
		source, sourceBytes, err = compileSourceArg(bound[0])
		if err != nil {
			return compileArgs{}, err
		}
	}
	filename, err := stringArg(bound[1], "filename")
	if err != nil {
		return compileArgs{}, err
	}
	modeStr, err := stringArg(bound[2], "mode")
	if err != nil {
		return compileArgs{}, err
	}
	mode, err := parseCompileMode(modeStr)
	if err != nil {
		return compileArgs{}, err
	}
	flags, err := parseCompileFlags(bound[3])
	if err != nil {
		return compileArgs{}, err
	}
	// func_type requires PyCF_ONLY_AST; validate now that flags are known.
	// CPython: Python/bltinmodule.c:771 builtin_compile_impl (mode == func_type check)
	if mode == parser.ModeFunc && flags&(cfOnlyAST|cfOptimizedAST) == 0 {
		return compileArgs{}, fmt.Errorf("ValueError: compile() mode 'func_type' requires flag PyCF_ONLY_AST")
	}
	if err := checkDontInherit(bound[4]); err != nil {
		return compileArgs{}, err
	}
	optimize, err := parseCompileOptimize(bound[5])
	if err != nil {
		return compileArgs{}, err
	}
	featureVersion := parseFeatureVersion(bound[6])
	return compileArgs{
		source:         source,
		sourceBytes:    sourceBytes,
		astMod:         astMod,
		filename:       filename,
		mode:           mode,
		flags:          flags,
		optimize:       optimize,
		featureVersion: featureVersion,
	}, nil
}

// compileSourceArg accepts the first positional argument to compile().
// str routes through ParseString. bytes / bytearray route through
// ParseBytes so the PEP 263 coding cookie controls the decode. AST
// input is rejected until gopy ships Python-side AST objects.
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl source decode
func compileSourceArg(o objects.Object) (string, []byte, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), nil, nil
	case *objects.Bytes:
		b := v.Bytes()
		dup := make([]byte, len(b))
		copy(dup, b)
		return "", dup, nil
	case *objects.ByteArray:
		b := v.Bytes()
		dup := make([]byte, len(b))
		copy(dup, b)
		return "", dup, nil
	}
	// _Py_SourceAsString falls back to PyObject_GetBuffer for anything
	// that is neither str nor a code/AST object, so a memoryview (or any
	// buffer-protocol exporter) over source bytes compiles like bytes.
	//
	// CPython: Python/pythonrun.c:1572 _Py_SourceAsString (PyObject_GetBuffer)
	if b, ok := objects.AsBytesLike(o); ok {
		dup := make([]byte, len(b))
		copy(dup, b)
		return "", dup, nil
	}
	return "", nil, fmt.Errorf("TypeError: compile() arg 1 must be a string, bytes or AST object")
}

// bindCompileArgs maps the positional + keyword args onto the
// fixed-position bound slice, enforcing the required-arg trio and
// rejecting dupes / unknown kwargs.
//
// _feature_version is the keyword-only private knob ast.parse uses to
// pin a Python feature level when compiling. CPython accepts it as a
// kwarg in Python/clinic/bltinmodule.c.h:289; gopy accepts it but
// ignores the value (we always parse against the bundled grammar).
//
// CPython: Python/clinic/bltinmodule.c.h:241 builtin_compile signature
func bindCompileArgs(args []objects.Object, kwargs map[string]objects.Object) ([]objects.Object, error) {
	const maxPositional = 6
	const maxArgs = 7
	if len(args) > maxPositional {
		return nil, fmt.Errorf("TypeError: compile() takes at most 6 positional arguments (%d given)", len(args))
	}
	names := []string{"source", "filename", "mode", "flags", "dont_inherit", "optimize", "_feature_version"}
	bound := make([]objects.Object, maxArgs)
	copy(bound, args)
	for k, v := range kwargs {
		idx := -1
		for i, n := range names {
			if n == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("TypeError: compile() got an unexpected keyword argument %q", k)
		}
		if bound[idx] != nil {
			return nil, fmt.Errorf("TypeError: compile() got multiple values for argument %q", k)
		}
		bound[idx] = v
	}
	for i, name := range names[:3] {
		if bound[i] == nil {
			return nil, fmt.Errorf("TypeError: compile() missing required argument: %q", name)
		}
	}
	return bound, nil
}

// parseFeatureVersion extracts the minor version from the _feature_version
// kwarg. ast.parse already strips the major and passes only the minor as
// an integer (e.g. 4 for Python 3.4). We also accept -1 (unset sentinel
// used by ast.parse when feature_version=None) as "unset".
//
// CPython: Python/clinic/bltinmodule.c.h:289 builtin_compile (feature_version param)
// CPython: Lib/ast.py:38-44 (feature_version normalization: tuple→minor int)
func parseFeatureVersion(obj objects.Object) int {
	if obj == nil {
		return 0
	}
	if n, ok := obj.(*objects.Int); ok {
		v, exact := n.Int64()
		if !exact || v <= 0 {
			return 0
		}
		return int(v)
	}
	return 0
}

// parseCompileMode maps the mode string to the parser mode constant.
// "func_type" is valid only when PyCF_ONLY_AST is set; that validation
// happens in parseCompileArgs after flags are parsed.
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl (mode check)
func parseCompileMode(modeStr string) (parser.Mode, error) {
	switch modeStr {
	case "exec":
		return parser.ModeFile, nil
	case "eval":
		return parser.ModeEval, nil
	case "single":
		return parser.ModeSingle, nil
	case "func_type":
		return parser.ModeFunc, nil
	}
	return 0, fmt.Errorf("ValueError: compile() mode must be 'exec', 'eval', 'single' or 'func_type'")
}

// PyCF_ONLY_AST and PyCF_OPTIMIZED_AST flag constants.
//
// CPython: Include/cpython/code.h PyCF_ONLY_AST / PyCF_OPTIMIZED_AST
const (
	cfOnlyAST      = 0x0400
	cfOptimizedAST = 0x8400
)

// cfValidMask is the union of flag bits compile() accepts:
// PyCF_MASK (the CO_FUTURE_* bits) | PyCF_MASK_OBSOLETE (CO_NESTED) |
// PyCF_COMPILE_MASK (the parse-control bits). Any bit outside this
// union is a ValueError("compile(): unrecognised flags").
//
// CPython: Include/cpython/compile.h PyCF_MASK / PyCF_MASK_OBSOLETE / PyCF_COMPILE_MASK
const (
	// CO_FUTURE_* future-statement bits (Include/cpython/code.h).
	pycfMask = 0x20000 | 0x40000 | 0x80000 | 0x100000 |
		0x200000 | 0x400000 | 0x800000 | 0x1000000
	// CO_NESTED (Include/cpython/code.h).
	pycfMaskObsolete = 0x0010
	// PyCF_DONT_IMPLY_DEDENT | PyCF_ONLY_AST | PyCF_TYPE_COMMENTS |
	// PyCF_ALLOW_TOP_LEVEL_AWAIT | PyCF_ALLOW_INCOMPLETE_INPUT |
	// PyCF_OPTIMIZED_AST (Include/cpython/compile.h).
	pycfCompileMask = 0x0200 | 0x0400 | 0x1000 | 0x2000 | 0x4000 | 0x8400
	cfValidMask     = pycfMask | pycfMaskObsolete | pycfCompileMask
)

// parseCompileFlags reads the optional flags arg. PyCF_ONLY_AST is now
// accepted and triggers parse-only mode (no codegen). Other flag bits are
// silently accepted for signature parity. Non-int types are coerced via
// __index__ matching CPython's PyArg_ParseTuple "i" format behavior.
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl flags check
func parseCompileFlags(o objects.Object) (int, error) {
	if o == nil {
		return 0, nil
	}
	if _, ok := o.(*objects.Int); !ok {
		// Try __index__ coercion (CPython: PyNumber_Index path in PyArg_ParseTuple "i").
		idxFn, err := objects.GetAttr(o, objects.NewStr("__index__"))
		if err != nil || idxFn == nil {
			return 0, fmt.Errorf("TypeError: flags must be int, not %s", o.Type().Name)
		}
		res, err := objects.CallNoArgs(idxFn)
		if err != nil {
			return 0, err
		}
		if _, ok := res.(*objects.Int); !ok {
			return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", res.Type().Name)
		}
		o = res
	}
	flags, err := intArg(o, "flags")
	if err != nil {
		return 0, err
	}
	// Reject any bit outside the recognised flag union before it can
	// reach the parser / codegen.
	//
	// CPython: Python/bltinmodule.c:771 builtin_compile_impl (unrecognised flags)
	if flags&^cfValidMask != 0 {
		return 0, fmt.Errorf("ValueError: compile(): unrecognised flags")
	}
	return flags, nil
}

// checkDontInherit accepts dont_inherit for signature parity. gopy has
// no surrounding compiler-flags context to inherit, so the value is a
// no-op either way; only the type is validated.
func checkDontInherit(o objects.Object) error {
	if o == nil {
		return nil
	}
	if _, ok := o.(*objects.Int); ok {
		return nil
	}
	if _, ok := o.(*objects.Bool); ok {
		return nil
	}
	return fmt.Errorf("TypeError: compile() arg 5 (dont_inherit) must be int or bool")
}

// parseCompileOptimize reads the optional optimize arg. -1 is the
// "use the runtime default" sentinel; 0/1/2 select the requested
// optimisation level. Anything else is a ValueError.
func parseCompileOptimize(o objects.Object) (int, error) {
	if o == nil {
		return -1, nil
	}
	v, err := signedIntArg(o, "optimize")
	if err != nil {
		return 0, err
	}
	if v < -1 || v > 2 {
		return 0, fmt.Errorf("ValueError: compile(): invalid optimize value")
	}
	return v, nil
}

// signedIntArg accepts negative ints (intArg in import.go forbids them
// because the only caller wanted level >= 0). compile()'s optimize=-1
// is meaningful so we need a permissive sibling. Non-int types are
// coerced via __index__ to match CPython's PyArg_ParseTuple "i" format.
//
// CPython: Python/bltinmodule.c:775 builtin_compile_impl optimize check
func signedIntArg(o objects.Object, label string) (int, error) {
	if _, ok := o.(*objects.Int); !ok {
		idxFn, err := objects.GetAttr(o, objects.NewStr("__index__"))
		if err != nil || idxFn == nil {
			return 0, fmt.Errorf("TypeError: %s must be int, not %s", label, o.Type().Name)
		}
		res, err := objects.CallNoArgs(idxFn)
		if err != nil {
			return 0, err
		}
		if _, ok := res.(*objects.Int); !ok {
			return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", res.Type().Name)
		}
		o = res
	}
	iv, ok := o.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s must be int, not %s", label, o.Type().Name)
	}
	v, exact := iv.Int64()
	if !exact {
		return 0, fmt.Errorf("OverflowError: %s does not fit in a Go int", label)
	}
	return int(v), nil
}

// parseOnlyResult converts a Go ast.Mod into a Python _ast object tree
// for PyCF_ONLY_AST parse-only mode. The _ast module must already be
// loaded in sys.modules (ast.parse() imports it before calling compile()).
// Falls back to a sentinel int(0) if the bridge cannot find _ast, so
// callers that only check for SyntaxError still see a successful parse.
//
// When PyCF_OPTIMIZED_AST is set (i.e. both 0x8000 and 0x0400 bits),
// the Preprocess pass runs constant-folding and other optimizations before
// converting to Python objects. When only PyCF_ONLY_AST (0x0400) is set,
// only syntax-check mode is used (no mutations).
//
// CPython: Python/bltinmodule.c:840 builtin_compile_impl (PyCF_ONLY_AST branch)
func parseOnlyResult(mod ast.Mod, parsed *compileArgs) (objects.Object, error) {
	// CPython: Python/bltinmodule.c:843 _PyAST_Validate
	if err := ast.Validate(mod); err != nil {
		return nil, fmt.Errorf("ValueError: %w", err)
	}
	// CPython: Python/bltinmodule.c:846
	// syntax_check_only = ((flags & PyCF_OPTIMIZED_AST) == PyCF_ONLY_AST)
	syntaxCheckOnly := (parsed.flags & cfOptimizedAST) == cfOnlyAST
	ast.Preprocess(mod, ast.PreprocessOptions{
		Filename:        parsed.filename,
		OptimizeLevel:   parsed.optimize,
		SyntaxCheckOnly: syntaxCheckOnly,
	})
	return astModToObject(mod), nil
}

// liftCompileCode adapts compile.Code into objects.Code. Mirrors the
// helper pythonrun keeps for the same purpose; both go away once spec
// 1687 retires compile.Code in favor of objects.Code directly.
func liftCompileCode(c *compile.Code) *objects.Code {
	out := &objects.Code{
		Argcount:        c.Argcount,
		PosonlyArgcount: c.PosOnlyArgCount,
		KwonlyArgcount:  c.KwOnlyArgCount,
		Stacksize:       c.Stacksize,
		Flags:           int(c.Flags),
		Code:            c.Code,
		Consts:          liftCompileConsts(c.Consts),
		Names:           c.Names,
		Varnames:        c.VarNames,
		Freevars:        c.FreeVars,
		Cellvars:        c.CellVars,
		LocalsplusNames: c.LocalsPlusNames,
		LocalsplusKinds: c.LocalsPlusKinds,
		Filename:        c.Filename,
		Name:            c.Name,
		Qualname:        c.Qualname,
		Firstlineno:     c.Firstlineno,
		Linetable:       c.Linetable,
		ExceptionTable:  c.ExceptionTable,
	}
	out.Init(objects.CodeType)
	out.SyncNameObjs()
	out.SyncConstObjs()
	out.SyncLocalsplusCounts()
	return out
}

// liftCompileConsts walks a Code.Consts slice and converts
// compile-pipeline value types into marshal-friendly forms:
// *compile.Code becomes *objects.Code (recursively lifted) and
// *compile.ConstTuple becomes []any (recursively lifted). Scalars
// pass through unchanged. Without this lift the .pyc marshal layer
// refuses nested code objects.
//
// CPython: Python/compile.c assemble_consts paths (each child code
// is already a PyObject* so this conversion is implicit there).
func liftCompileConsts(consts []any) []any {
	out := make([]any, len(consts))
	for i, v := range consts {
		out[i] = liftCompileConst(v)
	}
	return out
}

func liftCompileConst(v any) any {
	switch x := v.(type) {
	case *compile.Code:
		return liftCompileCode(x)
	case *compile.ConstTuple:
		items := make([]any, len(x.Values))
		for i, raw := range x.Values {
			items[i] = liftCompileConst(raw)
		}
		return items
	}
	return v
}
