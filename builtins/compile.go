// Port of builtin_compile_impl. compile() drives the parser + compiler
// pipeline from Python: take a source string plus a filename and mode,
// return a code object the user can hand to exec() / eval() (those land
// alongside this in spec 1651).
//
// CPython: Python/bltinmodule.c:771 builtin_compile_impl

package builtins

import (
	"fmt"

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
	mod, err := parser.ParseString(parsed.source, parsed.filename, parsed.mode)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, parsed.filename, parsed.optimize)
	if err != nil {
		return nil, err
	}
	return liftCompileCode(cco), nil
}

type compileArgs struct {
	source   string
	filename string
	mode     parser.Mode
	flags    int
	optimize int
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
	source, err := stringArg(bound[0], "source")
	if err != nil {
		return compileArgs{}, fmt.Errorf("TypeError: compile() arg 1 must be a string, bytes or AST object")
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
	if err := checkDontInherit(bound[4]); err != nil {
		return compileArgs{}, err
	}
	optimize, err := parseCompileOptimize(bound[5])
	if err != nil {
		return compileArgs{}, err
	}
	return compileArgs{
		source:   source,
		filename: filename,
		mode:     mode,
		flags:    flags,
		optimize: optimize,
	}, nil
}

// bindCompileArgs maps the positional + keyword args onto the
// fixed-position bound slice, enforcing the required-arg trio and
// rejecting dupes / unknown kwargs.
func bindCompileArgs(args []objects.Object, kwargs map[string]objects.Object) ([]objects.Object, error) {
	const maxArgs = 6
	if len(args) > maxArgs {
		return nil, fmt.Errorf("TypeError: compile() takes at most 6 arguments (%d given)", len(args))
	}
	names := []string{"source", "filename", "mode", "flags", "dont_inherit", "optimize"}
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

// parseCompileMode maps the mode string to the parser mode constant.
// func_type is recognized but rejected because gopy has no
// PyCF_ONLY_AST path yet.
func parseCompileMode(modeStr string) (parser.Mode, error) {
	switch modeStr {
	case "exec":
		return parser.ModeFile, nil
	case "eval":
		return parser.ModeEval, nil
	case "single":
		return parser.ModeSingle, nil
	case "func_type":
		return 0, fmt.Errorf("ValueError: compile() mode 'func_type' requires flag PyCF_ONLY_AST")
	}
	return 0, fmt.Errorf("ValueError: compile() mode must be 'exec', 'eval' or 'single'")
}

// parseCompileFlags reads the optional flags arg. Only the flag bits
// CPython actually carries today are PyCF_ONLY_AST / PyCF_OPTIMIZED_AST
// / PyCF_DONT_IMPLY_DEDENT / PyCF_ALLOW_TOP_LEVEL_AWAIT plus the
// future-statement bits; none alter codegen in gopy yet, so non-zero
// values are silently accepted except for the AST bits which need
// AST-as-Python support.
func parseCompileFlags(o objects.Object) (int, error) {
	if o == nil {
		return 0, nil
	}
	flags, err := intArg(o, "flags")
	if err != nil {
		return 0, err
	}
	const cfOnlyAST = 0x0400
	const cfOptimizedAST = 0x2400
	if flags&(cfOnlyAST|cfOptimizedAST) != 0 {
		return 0, fmt.Errorf("ValueError: compile() PyCF_ONLY_AST / PyCF_OPTIMIZED_AST not supported yet")
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
// is meaningful so we need a permissive sibling.
func signedIntArg(o objects.Object, label string) (int, error) {
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
		Consts:          c.Consts,
		Names:           c.Names,
		Varnames:        c.VarNames,
		Freevars:        c.FreeVars,
		Cellvars:        c.CellVars,
		Filename:        c.Filename,
		Name:            c.Name,
		Qualname:        c.Qualname,
		Firstlineno:     c.Firstlineno,
		Linetable:       c.Linetable,
		ExceptionTable:  c.ExceptionTable,
	}
	out.Init(objects.CodeType)
	return out
}
