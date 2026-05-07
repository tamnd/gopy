// Port of builtin_eval_impl and builtin_exec_impl. Both share the same
// shape: an optional source (str or code object) plus optional globals
// and locals; eval drives the parser in expression mode and returns
// the value, exec drives it in file mode and returns None.
//
// CPython: Python/bltinmodule.c:956 builtin_eval_impl
// CPython: Python/bltinmodule.c:1081 builtin_exec_impl

package builtins

import (
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
)

// Evaluator runs a code object against the given globals and locals
// and returns the value the frame produced. vm.init installs this
// during startup so the dependency stays builtins -> objects, vm ->
// builtins without a cycle.
//
// CPython: equivalent of PyEval_EvalCode (Python/ceval.c)
type Evaluator func(code *objects.Code, globals, locals objects.Object) (objects.Object, error)

var currentEvaluator Evaluator

// SetEvaluator installs the eval hook used by eval() and exec().
//
// CPython: vm-side glue, no direct counterpart.
func SetEvaluator(p Evaluator) {
	currentEvaluator = p
}

// Eval implements builtins.eval(source, globals=None, locals=None).
// source may be a str (parsed in expression mode and compiled) or an
// already-compiled code object; the returned value is whatever the
// expression yielded.
//
// CPython: Python/bltinmodule.c:956 builtin_eval_impl
func Eval(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	source, globals, locals, err := parseEvalExecArgs("eval", args, kwargs)
	if err != nil {
		return nil, err
	}
	code, err := codeForSource(source, "eval", parser.ModeEval)
	if err != nil {
		return nil, err
	}
	return runCode(code, globals, locals)
}

// Exec implements builtins.exec(source, globals=None, locals=None).
// source may be a str (parsed in file mode) or a code object. The
// return value is always None when the call succeeds; failures bubble
// up as the actual Python error.
//
// CPython: Python/bltinmodule.c:1081 builtin_exec_impl
func Exec(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	source, globals, locals, err := parseEvalExecArgs("exec", args, kwargs)
	if err != nil {
		return nil, err
	}
	code, err := codeForSource(source, "exec", parser.ModeFile)
	if err != nil {
		return nil, err
	}
	if _, err := runCode(code, globals, locals); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// parseEvalExecArgs binds the (source, globals, locals) trio shared by
// eval() and exec(), filling globals and locals from the running frame
// when the caller did not pass them. fnName is the builtin's own name
// for error reporting ("eval" or "exec"), since CPython's wording
// includes it.
func parseEvalExecArgs(fnName string, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, objects.Object, objects.Object, error) {
	if len(args) > 3 {
		return nil, nil, nil, fmt.Errorf("TypeError: %s() takes at most 3 arguments (%d given)", fnName, len(args))
	}
	names := []string{"source", "globals", "locals"}
	bound := make([]objects.Object, 3)
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
			return nil, nil, nil, fmt.Errorf("TypeError: %s() got an unexpected keyword argument %q", fnName, k)
		}
		if bound[idx] != nil {
			return nil, nil, nil, fmt.Errorf("TypeError: %s() got multiple values for argument %q", fnName, k)
		}
		bound[idx] = v
	}
	if bound[0] == nil {
		return nil, nil, nil, fmt.Errorf("TypeError: %s() missing required argument: 'source'", fnName)
	}
	globals, locals, err := resolveScope(fnName, bound[1], bound[2])
	if err != nil {
		return nil, nil, nil, err
	}
	return bound[0], globals, locals, nil
}

// resolveScope picks the globals + locals for the eval/exec call. When
// either is None / nil we substitute the running frame's scope (via
// the currentScope hook), and locals defaults to globals when only
// globals is given. globals must be a real dict; locals is allowed to
// be any object today (CPython requires PyMapping_Check, which gopy
// does not yet expose generically).
func resolveScope(fnName string, globals, locals objects.Object) (objects.Object, objects.Object, error) {
	fromFrame := false
	if globals == nil || objects.IsNone(globals) {
		if currentScope != nil {
			g, _ := currentScope()
			if g != nil {
				globals = g
				fromFrame = true
			}
		}
		if globals == nil {
			return nil, nil, fmt.Errorf("TypeError: %s() must be given globals and locals when called without a frame", fnName)
		}
	}
	if _, ok := globals.(*objects.Dict); !ok {
		return nil, nil, fmt.Errorf("TypeError: %s() globals must be a dict, not %s", fnName, globals.Type().Name)
	}
	if locals == nil || objects.IsNone(locals) {
		if fromFrame && currentScope != nil {
			_, l := currentScope()
			if l != nil {
				locals = l
			}
		}
		if locals == nil {
			locals = globals
		}
	}
	return globals, locals, nil
}

// codeForSource turns the source argument into an *objects.Code. A
// code object is returned as-is; a str is parsed under mode and
// compiled. fnName / mode pair the call to its CPython sibling
// ("eval"/ModeEval, "exec"/ModeFile).
func codeForSource(source objects.Object, fnName string, mode parser.Mode) (*objects.Code, error) {
	if c, ok := source.(*objects.Code); ok {
		return c, nil
	}
	str, ok := source.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: %s() arg 1 must be a string, bytes or code object", fnName)
	}
	src := str.Value()
	if src == "" || src[len(src)-1] != '\n' {
		src += "\n"
	}
	mod, err := parser.ParseString(src, "<string>", mode)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, "<string>", 0)
	if err != nil {
		return nil, err
	}
	return liftCompileCode(cco), nil
}

// runCode dispatches a compiled code object through the vm via the
// installed evaluator hook.
func runCode(code *objects.Code, globals, locals objects.Object) (objects.Object, error) {
	if currentEvaluator == nil {
		return nil, fmt.Errorf("SystemError: eval/exec evaluator not installed")
	}
	return currentEvaluator(code, globals, locals)
}
