// Port of the code-object introspection helpers CPython exposes to the
// test suite through _testinternalcapi: get_code_var_counts,
// get_co_localskinds, code_returns_only_none, and verify_stateless_code.
// The heavy lifting (var-count tallying, unbound-name identification,
// statelessness verification, returns-only-None analysis) mirrors the
// matching functions in Objects/codeobject.c one-for-one.
//
// CPython: Modules/_testinternalcapi.c:969 code_returns_only_none
// CPython: Modules/_testinternalcapi.c:992 get_co_localskinds
// CPython: Modules/_testinternalcapi.c:1023 get_code_var_counts
// CPython: Modules/_testinternalcapi.c:1192 verify_stateless_code
// CPython: Objects/codeobject.c:1818 identify_unbound_names
// CPython: Objects/codeobject.c:1913 _PyCode_GetVarCounts
// CPython: Objects/codeobject.c:2020 _PyCode_SetUnboundVarCounts
// CPython: Objects/codeobject.c:2085 _PyCode_CheckNoInternalState
// CPython: Objects/codeobject.c:2104 _PyCode_CheckNoExternalState
// CPython: Objects/codeobject.c:2132 _PyCode_VerifyStateless
// CPython: Objects/codeobject.c:2166 _PyCode_CheckPureFunction
// CPython: Objects/codeobject.c:2259 code_returns_only_none
package testinternalcapi

import (
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// argsCounts mirrors the args sub-struct of co_locals_counts.
type argsCounts struct {
	total      int
	numposonly int
	numposorkw int
	numkwonly  int
	varargs    int
	varkwargs  int
}

type cellsCounts struct {
	total     int
	numargs   int
	numothers int
}

type hiddenCounts struct {
	total   int
	numpure int
	numcells int
}

type localsCounts struct {
	total   int
	args    argsCounts
	numpure int
	cells   cellsCounts
	hidden  hiddenCounts
}

type globalsCounts struct {
	total      int
	numglobal  int
	numbuiltin int
	numunknown int
}

type unboundCounts struct {
	total      int
	globals    globalsCounts
	numattrs   int
	numunknown int
}

// varCounts mirrors _PyCode_var_counts_t.
//
// CPython: Include/internal/pycore_code.h:572 _PyCode_var_counts_t
type varCounts struct {
	total   int
	locals  localsCounts
	numfree int
	unbound unboundCounts
}

// getVarCounts tallies the locals/cells/free vars from co_localspluskinds
// and seeds the unbound counts from co_names.
//
// CPython: Objects/codeobject.c:1913 _PyCode_GetVarCounts
func getVarCounts(co *objects.Code) varCounts {
	var locals localsCounts
	numfree := 0
	kinds := co.LocalsplusKinds
	for i := 0; i < len(kinds); i++ {
		kind := kinds[i]
		if kind&objects.CoFastFree != 0 {
			numfree++
			continue
		}
		locals.total++
		if kind&objects.CoFastArg != 0 {
			locals.args.total++
			switch {
			case kind&objects.CoFastArgVar != 0:
				if kind&objects.CoFastArgPos != 0 {
					locals.args.varargs = 1
				} else {
					locals.args.varkwargs = 1
				}
			case kind&objects.CoFastArgPos != 0:
				if kind&objects.CoFastArgKw != 0 {
					locals.args.numposorkw++
				} else {
					locals.args.numposonly++
				}
			default:
				locals.args.numkwonly++
			}
			if kind&objects.CoFastCell != 0 {
				locals.cells.total++
				locals.cells.numargs++
			}
		} else {
			if kind&objects.CoFastCell != 0 {
				locals.cells.total++
				locals.cells.numothers++
				if kind&objects.CoFastHidden != 0 {
					locals.hidden.total++
					locals.hidden.numcells++
				}
			} else {
				locals.numpure++
				if kind&objects.CoFastHidden != 0 {
					locals.hidden.total++
					locals.hidden.numpure++
				}
			}
		}
	}

	numunbound := len(co.Names)
	unbound := unboundCounts{
		total:      numunbound,
		numunknown: numunbound,
	}

	return varCounts{
		total:   locals.total + numfree + unbound.total,
		locals:  locals,
		numfree: numfree,
		unbound: unbound,
	}
}

// nameAt returns the interned name object at index idx in co_names.
func nameAt(co *objects.Code, idx int) objects.Object {
	if idx >= 0 && idx < len(co.NameObjs) && co.NameObjs[idx] != nil {
		return co.NameObjs[idx]
	}
	if idx >= 0 && idx < len(co.Names) {
		return objects.NewStr(co.Names[idx])
	}
	return objects.NewStr("")
}

// identifyUnboundNames walks the (deoptimized) bytecode and classifies
// each LOAD_GLOBAL / LOAD_ATTR name against the provided namespaces.
//
// CPython: Objects/codeobject.c:1818 identify_unbound_names
func identifyUnboundNames(co *objects.Code, globalnames, attrnames *objects.Set, globalsns, builtinsns *objects.Dict) (unboundCounts, int, error) {
	var unbound unboundCounts
	numdupes := 0
	code := specialize.DeoptCode(co.Code)
	ncodeunits := len(code) / 2
	for i := 0; i < ncodeunits; {
		op := compile.Opcode(code[i*2])
		arg := int(code[i*2+1])
		switch op {
		case compile.LOAD_ATTR:
			name := nameAt(co, arg>>1)
			if ok, err := attrnames.Contains(name); err != nil {
				return unbound, 0, err
			} else if ok {
				break
			}
			unbound.total++
			unbound.numattrs++
			if err := attrnames.Add(name); err != nil {
				return unbound, 0, err
			}
			if ok, err := globalnames.Contains(name); err != nil {
				return unbound, 0, err
			} else if ok {
				numdupes++
			}
		case compile.LOAD_GLOBAL:
			name := nameAt(co, arg>>1)
			if ok, err := globalnames.Contains(name); err != nil {
				return unbound, 0, err
			} else if ok {
				break
			}
			unbound.total++
			unbound.globals.total++
			switch {
			case globalsns != nil && dictContains(globalsns, name):
				unbound.globals.numglobal++
			case builtinsns != nil && dictContains(builtinsns, name):
				unbound.globals.numbuiltin++
			default:
				unbound.globals.numunknown++
			}
			if err := globalnames.Add(name); err != nil {
				return unbound, 0, err
			}
			if ok, err := attrnames.Contains(name); err != nil {
				return unbound, 0, err
			} else if ok {
				numdupes++
			}
		}
		i += 1 + compile.CacheCount(op)
	}
	return unbound, numdupes, nil
}

func dictContains(d *objects.Dict, key objects.Object) bool {
	ok, err := d.Contains(key)
	return err == nil && ok
}

// setUnboundVarCounts fills in counts.unbound from the bytecode walk,
// reconciling duplicate names that appear as both globals and attrs.
//
// CPython: Objects/codeobject.c:2020 _PyCode_SetUnboundVarCounts
func setUnboundVarCounts(co *objects.Code, counts *varCounts, globalnames, attrnames *objects.Set, globalsns, builtinsns *objects.Dict) error {
	if globalnames == nil {
		globalnames = objects.NewSet()
	}
	if attrnames == nil {
		attrnames = objects.NewSet()
	}
	unbound, numdupes, err := identifyUnboundNames(co, globalnames, attrnames, globalsns, builtinsns)
	if err != nil {
		return err
	}
	totalunbound := counts.unbound.total + numdupes
	unbound.numunknown = totalunbound - unbound.total
	unbound.total = totalunbound
	counts.unbound = unbound
	counts.total += numdupes
	return nil
}

// checkNoExternalState mirrors _PyCode_CheckNoExternalState; returns the
// CPython error message when the code relies on closures or globals.
//
// CPython: Objects/codeobject.c:2104 _PyCode_CheckNoExternalState
func checkNoExternalState(counts *varCounts) string {
	switch {
	case counts.numfree > 0:
		return "closures not supported"
	case counts.unbound.globals.numglobal > 0:
		return "globals not supported"
	case counts.unbound.globals.numbuiltin > 0 && counts.unbound.globals.numunknown > 0:
		return "globals not supported"
	}
	return ""
}

// checkPureFunction mirrors _PyCode_CheckPureFunction.
//
// CPython: Objects/codeobject.c:2166 _PyCode_CheckPureFunction
func checkPureFunction(co *objects.Code) bool {
	flags := uint32(co.Flags)
	if flags&compile.CoGenerator != 0 ||
		flags&compile.CoCoroutine != 0 ||
		flags&compile.CoIterableCoroutine != 0 ||
		flags&compile.CoAsyncGenerator != 0 {
		return false
	}
	return true
}

// returnsOnlyNone mirrors code_returns_only_none: a bare/implicit return
// or an explicit "return None" everywhere (or a function that only raises).
//
// CPython: Objects/codeobject.c:2259 code_returns_only_none
func returnsOnlyNone(co *objects.Code) bool {
	if !checkPureFunction(co) {
		return false
	}
	code := specialize.DeoptCode(co.Code)
	ncodeunits := len(code) / 2
	if ncodeunits == 0 {
		return true
	}

	finalOp := compile.Opcode(code[(ncodeunits-1)*2])

	// Look up None in co_consts.
	co.SyncConstObjs()
	noneIndex := 0
	nconsts := len(co.ConstObjs)
	for ; noneIndex < nconsts; noneIndex++ {
		if co.ConstObjs[noneIndex] == objects.None() {
			break
		}
	}

	isReturn := func(op compile.Opcode) bool { return op == compile.RETURN_VALUE }
	isRaise := func(op compile.Opcode) bool {
		return op == compile.RAISE_VARARGS || op == compile.RERAISE
	}

	if noneIndex == nconsts {
		// None wasn't in co_consts: no implicit return / "return None".
		if isReturn(finalOp) {
			return false
		}
		_ = isRaise // it must end with a raise
		for i := 0; i < ncodeunits; {
			op := compile.Opcode(code[i*2])
			if isReturn(op) {
				return false
			}
			i += 1 + compile.CacheCount(op)
		}
		return true
	}

	// Walk the bytecode, looking for a RETURN_VALUE that does not return
	// the None constant.
	for i := 0; i < ncodeunits; {
		op := compile.Opcode(code[i*2])
		if isReturn(op) && i != 0 {
			prevOp := compile.Opcode(code[(i-1)*2])
			prevArg := int(code[(i-1)*2+1])
			if prevOp == compile.LOAD_CONST && prevArg == noneIndex {
				i += 1 + compile.CacheCount(op)
				continue
			}
			return false
		}
		i += 1 + compile.CacheCount(op)
	}
	return true
}

// codeArg resolves the first positional argument to a *Code, unwrapping a
// function and (optionally) sourcing its globals/builtins namespaces.
func codeArg(arg objects.Object, globalsns, builtinsns **objects.Dict) (*objects.Code, error) {
	switch v := arg.(type) {
	case *objects.Function:
		if *globalsns == nil {
			if d, ok := v.Globals.(*objects.Dict); ok {
				*globalsns = d
			}
		}
		if *builtinsns == nil {
			if d, ok := v.Builtins.(*objects.Dict); ok {
				*builtinsns = d
			}
		}
		return v.Code, nil
	case *objects.Code:
		return v, nil
	default:
		return nil, fmt.Errorf("TypeError: argument must be a code object or a function")
	}
}

// buildVarCountsDict renders a varCounts value into the nested dict the
// test compares against.
func buildVarCountsDict(c *varCounts) (*objects.Dict, error) {
	mk := func(pairs [][2]any) (*objects.Dict, error) {
		d := objects.NewDict()
		for _, p := range pairs {
			var val objects.Object
			switch v := p[1].(type) {
			case int:
				val = objects.NewInt(int64(v))
			case *objects.Dict:
				val = v
			}
			if err := d.SetItem(objects.NewStr(p[0].(string)), val); err != nil {
				return nil, err
			}
		}
		return d, nil
	}

	args, err := mk([][2]any{
		{"total", c.locals.args.total},
		{"numposonly", c.locals.args.numposonly},
		{"numposorkw", c.locals.args.numposorkw},
		{"numkwonly", c.locals.args.numkwonly},
		{"varargs", c.locals.args.varargs},
		{"varkwargs", c.locals.args.varkwargs},
	})
	if err != nil {
		return nil, err
	}
	cells, err := mk([][2]any{
		{"total", c.locals.cells.total},
		{"numargs", c.locals.cells.numargs},
		{"numothers", c.locals.cells.numothers},
	})
	if err != nil {
		return nil, err
	}
	hidden, err := mk([][2]any{
		{"total", c.locals.hidden.total},
		{"numpure", c.locals.hidden.numpure},
		{"numcells", c.locals.hidden.numcells},
	})
	if err != nil {
		return nil, err
	}
	locals, err := mk([][2]any{
		{"total", c.locals.total},
		{"args", args},
		{"numpure", c.locals.numpure},
		{"cells", cells},
		{"hidden", hidden},
	})
	if err != nil {
		return nil, err
	}
	globals, err := mk([][2]any{
		{"total", c.unbound.globals.total},
		{"numglobal", c.unbound.globals.numglobal},
		{"numbuiltin", c.unbound.globals.numbuiltin},
		{"numunknown", c.unbound.globals.numunknown},
	})
	if err != nil {
		return nil, err
	}
	unbound, err := mk([][2]any{
		{"total", c.unbound.total},
		{"globals", globals},
		{"numattrs", c.unbound.numattrs},
		{"numunknown", c.unbound.numunknown},
	})
	if err != nil {
		return nil, err
	}
	return mk([][2]any{
		{"total", c.total},
		{"locals", locals},
		{"numfree", c.numfree},
		{"unbound", unbound},
	})
}

// getCodeVarCounts is _testinternalcapi.get_code_var_counts.
//
// CPython: Modules/_testinternalcapi.c:1023 get_code_var_counts
func getCodeVarCounts(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: get_code_var_counts() missing required argument 'code'")
	}
	var globalnames, attrnames *objects.Set
	var globalsns, builtinsns *objects.Dict
	if v, ok := kwargs["globalnames"]; ok {
		if s, ok := v.(*objects.Set); ok {
			globalnames = s
		}
	}
	if v, ok := kwargs["attrnames"]; ok {
		if s, ok := v.(*objects.Set); ok {
			attrnames = s
		}
	}
	if v, ok := kwargs["globalsns"]; ok {
		d, ok := v.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: globalsns must be a dict")
		}
		globalsns = d
	}
	if v, ok := kwargs["builtinsns"]; ok {
		d, ok := v.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: builtinsns must be a dict")
		}
		builtinsns = d
	}

	code, err := codeArg(args[0], &globalsns, &builtinsns)
	if err != nil {
		return nil, err
	}

	counts := getVarCounts(code)
	if err := setUnboundVarCounts(code, &counts, globalnames, attrnames, globalsns, builtinsns); err != nil {
		return nil, err
	}
	return buildVarCountsDict(&counts)
}

// getCoLocalskinds is _testinternalcapi.get_co_localskinds.
//
// CPython: Modules/_testinternalcapi.c:992 get_co_localskinds
func getCoLocalskinds(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: get_co_localskinds() takes exactly one argument")
	}
	co, ok := args[0].(*objects.Code)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument must be a code object")
	}
	d := objects.NewDict()
	for offset := 0; offset < co.Nlocalsplus && offset < len(co.LocalsplusKinds); offset++ {
		name := objects.NewStr(co.LocalsplusNames[offset])
		kind := objects.NewInt(int64(co.LocalsplusKinds[offset]))
		if err := d.SetItem(name, kind); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// codeReturnsOnlyNone is _testinternalcapi.code_returns_only_none.
//
// CPython: Modules/_testinternalcapi.c:969 code_returns_only_none
func codeReturnsOnlyNone(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: code_returns_only_none() takes exactly one argument")
	}
	co, ok := args[0].(*objects.Code)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument must be a code object")
	}
	return objects.NewBool(returnsOnlyNone(co)), nil
}

// verifyStatelessCode is _testinternalcapi.verify_stateless_code.
//
// CPython: Modules/_testinternalcapi.c:1192 verify_stateless_code
// CPython: Objects/codeobject.c:2132 _PyCode_VerifyStateless
func verifyStatelessCode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: verify_stateless_code() missing required argument 'code'")
	}
	var globalnames *objects.Set
	var globalsns, builtinsns *objects.Dict
	if v, ok := kwargs["globalnames"]; ok {
		s, ok := v.(*objects.Set)
		if !ok {
			return nil, fmt.Errorf("TypeError: globalnames must be a set")
		}
		globalnames = s
	}
	if v, ok := kwargs["globalsns"]; ok {
		d, ok := v.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: globalsns must be a dict")
		}
		globalsns = d
	}
	if v, ok := kwargs["builtinsns"]; ok {
		d, ok := v.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: builtinsns must be a dict")
		}
		builtinsns = d
	}

	code, err := codeArg(args[0], &globalsns, &builtinsns)
	if err != nil {
		return nil, err
	}

	counts := getVarCounts(code)
	if err := setUnboundVarCounts(code, &counts, globalnames, nil, globalsns, builtinsns); err != nil {
		return nil, err
	}
	// gopy code objects carry no co_extra, so the no-internal-state check
	// always passes (CPython: _PyCode_CheckNoInternalState).
	if builtinsns != nil {
		// Make sure the external-state check fails for globals even when
		// there are no builtins.
		counts.unbound.globals.numbuiltin++
	}
	if msg := checkNoExternalState(&counts); msg != "" {
		return nil, fmt.Errorf("ValueError: %s", msg)
	}
	return objects.None(), nil
}
