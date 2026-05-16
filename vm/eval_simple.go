// Hand-written arms for the smallest core opcodes: stack hygiene,
// constant/fast load, name lookup. The codegen pipeline (1621) will
// eventually replace these with generated arms in opcodes_gen.go.
// Until then, this file is the source of truth for these ops so that
// trivial codes (RESUME / LOAD_CONST / RETURN_VALUE) actually run.
//
// CPython: Python/bytecodes.c per-op definitions

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 5: tier-1 dispatch switch is generated into vm/eval_dispatch_gen.go from Python/bytecodes.c via tools/cases_generator. This file shrinks to evalLoop scaffolding.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/intrinsics"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

// liftNestedCode mirrors pythonrun.liftCode for inner code objects
// reached through a parent's Consts slot. Nested defs / lambdas /
// class bodies all surface here.
func liftNestedCode(c *compile.Code) *objects.Code {
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
	specialize.Enable(out)
	return out
}

// wrapConst converts a raw compile-time constant value into an Object.
// The compiler stores constants as Go scalars in code.Consts; the VM
// has to lift them on first observation.
//
// CPython: Python/bytecodes.c:LOAD_CONST (CPython stores PyObject*
// directly so this conversion is a no-op there).
func wrapConst(v any) (objects.Object, error) {
	switch x := v.(type) {
	case nil:
		return objects.None(), nil
	case bool:
		return objects.NewBool(x), nil
	case int64:
		return objects.NewInt(x), nil
	case int:
		return objects.NewInt(int64(x)), nil
	case *big.Int:
		return objects.NewIntFromBig(x), nil
	case float64:
		return objects.NewFloat(x), nil
	case string:
		return objects.NewStr(x), nil
	case *compile.ConstTuple:
		items := make([]objects.Object, len(x.Values))
		for i, raw := range x.Values {
			item, err := wrapConst(raw)
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return objects.NewTuple(items), nil
	case *compile.Code:
		return liftNestedCode(x), nil
	case []byte:
		return objects.NewBytes(x), nil
	case complex128:
		return objects.NewComplex(real(x), imag(x)), nil
	case ast.EllipsisType:
		return objects.Ellipsis(), nil
	case objects.Object:
		return x, nil
	}
	return nil, fmt.Errorf("vm: unsupported const type %T", v)
}

// trySimple is consulted by dispatch before falling back to
// notImplemented. Returns ok=false if op isn't in the hand-written
// panel. The retDone/err return shape matches dispatch.
//
//nolint:gocognit,gocyclo,gocritic // hand-written opcode switch; the wide return tuple matches dispatch's contract and the arm count shrinks as 1621 codegen replaces these.
func (e *evalState) trySimple(op compile.Opcode, oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	switch op {
	case compile.CACHE, compile.RESERVED:
		// CACHE words are inline-specialization slots; the dispatcher
		// only sees them when fetch() runs past the end of an
		// instruction word, which the action translator does not do.
		// Treat them as NOP so a hand-rolled bytecode that includes
		// padding stays valid. RESERVED is the parity-pin opcode in
		// CPython's table; behavior matches NOP in unspecialized form.
		// NOP itself routes through dispatchGen (spec 1714 phase 5.2).
		return e.advance(), nil, nil, false, true, nil

	case compile.RESUME:
		next, rerr := e.handleResume(op, oparg)
		if rerr != nil {
			return 0, nil, nil, false, true, rerr
		}
		return next, nil, nil, false, true, nil

	case compile.JUMP_BACKWARD, compile.JUMP_BACKWARD_NO_INTERRUPT:
		// Backward jumps poll the eval breaker (CPython: CHECK_EVAL_BREAKER
		// fires here so signal handlers and pending calls can run mid-loop).
		// JUMP_BACKWARD_NO_INTERRUPT skips the poll.
		if op == compile.JUMP_BACKWARD && e.breaker != nil && e.breaker.Load() != 0 {
			if berr := e.handleEvalBreaker(); berr != nil {
				return 0, nil, nil, false, true, berr
			}
		}
		target := e.jumpBy(-int(oparg) + 1)
		if op == compile.JUMP_BACKWARD && target >= 0 {
			e.tryWarmupTier2(target / 2)
		}
		return target, nil, nil, false, true, nil

	case compile.ENTER_EXECUTOR:
		next, retVal, retErr, retDone, eerr := e.enterExecutor(oparg)
		return next, retVal, retErr, retDone, true, eerr

	case compile.BINARY_OP:
		b := e.popObject()
		a := e.popObject()
		out, berr := binaryOp(int32(oparg), a, b)
		if berr != nil {
			return 0, nil, nil, false, true, berr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.BINARY_OP), nil, nil, false, true, nil

	case compile.UNARY_NEGATIVE:
		a := e.popObject()
		neg := a.Type().Number
		if neg == nil || neg.Negative == nil {
			return 0, nil, nil, false, true, fmt.Errorf("vm: bad operand type for unary -: %s", a.Type().Name)
		}
		out, nerr := neg.Negative(a)
		if nerr != nil {
			return 0, nil, nil, false, true, nerr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

	case compile.UNARY_NOT:
		a := e.popObject()
		truthy, terr := objects.IsTruthy(a)
		if terr != nil {
			return 0, nil, nil, false, true, terr
		}
		e.pushObject(objects.NewBool(!truthy))
		return e.advance(), nil, nil, false, true, nil

	case compile.UNARY_INVERT:
		a := e.popObject()
		nm := a.Type().Number
		if nm == nil || nm.Invert == nil {
			return 0, nil, nil, false, true, fmt.Errorf("TypeError: bad operand type for unary ~: '%s'", a.Type().Name)
		}
		out, ierr := nm.Invert(a)
		if ierr != nil {
			return 0, nil, nil, false, true, ierr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

	case compile.COMPARE_OP:
		b := e.popObject()
		a := e.popObject()
		// CPython packs the CompareOp into the high 4 bits of oparg
		// and a "convert-to-bool" flag in bit 4. The low bits are
		// reserved for adaptive specialization.
		cmpOp := objects.CompareOp((oparg >> 5) & 0xf)
		out, cerr := objects.RichCmp(a, b, cmpOp)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.COMPARE_OP), nil, nil, false, true, nil

	case compile.BUILD_MAP:
		n := int(oparg)
		d := objects.NewDict()
		// Stack layout: ..., k0, v0, k1, v1, ..., kn-1, vn-1 (top)
		// We pop pairs in reverse and insert; insertion order is then
		// reversed but Dict iteration order isn't pinned in v0.6.
		pairs := make([][2]objects.Object, n)
		for i := n - 1; i >= 0; i-- {
			v := e.popObject()
			k := e.popObject()
			pairs[i] = [2]objects.Object{k, v}
		}
		for _, p := range pairs {
			if serr := d.SetItem(p[0], p[1]); serr != nil {
				return 0, nil, nil, false, true, serr
			}
		}
		e.pushObject(d)
		return e.advance(), nil, nil, false, true, nil

	case compile.FOR_ITER:
		// Stack: [iter]. Pop iter, peek by re-pushing. CPython peeks
		// directly to avoid the dup but the FOR_ITER arm in 3.14 keeps
		// the iterator on the stack across iterations.
		it := e.peek(0).AsObject()
		if it == nil {
			return 0, nil, nil, false, true, fmt.Errorf("vm: TypeError: FOR_ITER on nil object")
		}
		t := it.Type()
		if t.IterNext == nil {
			return 0, nil, nil, false, true, fmt.Errorf("vm: TypeError: '%s' object is not an iterator", t.Name)
		}
		v, nerr := t.IterNext(it)
		if nerr != nil {
			if errors.Is(nerr, objects.ErrStopIteration) {
				// CPython iter_iternext clears the IndexError /
				// StopIteration on the thread state when it absorbs it
				// (Objects/iterobject.c:62). Gopy's seqIterNext folds
				// the IndexError absorb into ErrStopIteration before
				// returning, so we mirror the clear here.
				pyerrors.Clear(e.ts)
				// Skip past the loop body; oparg is the jump distance to
				// END_FOR (in code units, not instruction words).
				return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
			}
			return 0, nil, nil, false, true, nerr
		}
		e.pushObject(v)
		return e.cacheAdvance(compile.FOR_ITER), nil, nil, false, true, nil

	case compile.END_FOR:
		// END_FOR is a no-op for ordinary iterators in CPython 3.14:
		// the codegen pairs it with POP_ITER (POP_TOP in gopy today),
		// and FOR_ITER's exhaustion path leaves the iterator on the
		// stack for that following pop. Generator finalization is the
		// only case where END_FOR has a real effect, and gopy doesn't
		// land that path yet.
		//
		// CPython: Python/bytecodes.c END_FOR
		return e.advance(), nil, nil, false, true, nil

	case compile.CALL:
		// Stack layout (3.14): [callable, NULL_or_self, arg0, ..., argN].
		// oparg is N (positional arg count).
		argc := int(oparg)
		args := make([]objects.Object, argc)
		for i := argc - 1; i >= 0; i-- {
			args[i] = e.popObject()
		}
		selfOrNull := e.popObject()
		callable := e.popObject()
		if selfOrNull != nil {
			// Method-style call: self_or_null is the bound instance.
			// Prepend it to the positional args.
			args = append([]objects.Object{selfOrNull}, args...)
		}
		out, cerr := objects.Vectorcall(callable, args, uint(len(args)), nil)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.CALL), nil, nil, false, true, nil

	case compile.UNPACK_SEQUENCE:
		seq := e.popObject()
		n := int(oparg)
		items, uerr := unpackSeq(seq, n)
		if uerr != nil {
			return 0, nil, nil, false, true, uerr
		}
		// Push in reverse so items[0] ends up at the top, matching
		// the assignment order CPython documents.
		for i := n - 1; i >= 0; i-- {
			e.pushObject(items[i])
		}
		return e.cacheAdvance(compile.UNPACK_SEQUENCE), nil, nil, false, true, nil

	case compile.STORE_SUBSCR:
		key := e.popObject()
		container := e.popObject()
		v := e.popObject()
		if serr := setItem(container, key, v); serr != nil {
			return 0, nil, nil, false, true, serr
		}
		return e.cacheAdvance(compile.STORE_SUBSCR), nil, nil, false, true, nil

	case compile.DELETE_SUBSCR:
		key := e.popObject()
		container := e.popObject()
		if derr := delItem(container, key); derr != nil {
			return 0, nil, nil, false, true, derr
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.CONTAINS_OP:
		// Stack layout: [..., left, right]. CPython pops right (haystack)
		// then left (needle); oparg low bit toggles `not in`.
		haystack := e.popObject()
		needle := e.popObject()
		hit, cerr := containsItem(haystack, needle)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		if oparg&1 == 1 {
			hit = !hit
		}
		e.pushObject(objects.NewBool(hit))
		return e.cacheAdvance(compile.CONTAINS_OP), nil, nil, false, true, nil

	case compile.RAISE_VARARGS:
		// oparg: 0 = re-raise, 1 = raise exc, 2 = raise exc from cause.
		// Sets the exception on the thread state via errors.Raise so
		// the unwind path, PrintEx, and HandleSystemExit can all read
		// it. The Go error returned is just the sentinel that drives
		// handleException; the real data lives on ts.
		//
		// CPython: Python/bytecodes.c:1648 RAISE_VARARGS
		switch oparg {
		case 0:
			// Bare `raise` re-raises the currently-handled exception
			// (PUSH_EXC_INFO stashed it on entry to the except block).
			// CPython: Python/bytecodes.c:1648 RAISE_VARARGS oparg==0
			// reads tstate->exc_info->exc_value via _PyErr_GetRaisedException.
			handled := e.ts.HandledException()
			if handled == nil {
				return 0, nil, nil, false, true, errors.New("RuntimeError: No active exception to re-raise")
			}
			exc, ok := handled.(*pyerrors.Exception)
			if !ok {
				return 0, nil, nil, false, true, errors.New("RuntimeError: No active exception to re-raise")
			}
			pyerrors.Raise(e.ts, exc)
			return 0, nil, nil, false, true, excSentinel(exc)
		case 1:
			val := e.popObject()
			exc := raiseValue(e.ts, val, nil)
			return 0, nil, nil, false, true, exc
		case 2:
			cause := e.popObject()
			val := e.popObject()
			exc := raiseValue(e.ts, val, cause)
			return 0, nil, nil, false, true, exc
		}
		return 0, nil, nil, false, true, fmt.Errorf("vm: RAISE_VARARGS: invalid oparg %d", oparg)

	case compile.PUSH_EXC_INFO:
		// Save the previously-handled exception under the new top, then
		// install the new exception as the current handled exception
		// so sys.exc_info() reads it from inside the except handler.
		// POP_EXCEPT restores the saved value. The handled slot is
		// distinct from the raised slot used by the unwind path, so
		// installing it here does not perturb getOptionalAttr et al.
		//
		// CPython: Python/bytecodes.c PUSH_EXC_INFO (push exc_info->exc_value;
		// exc_info->exc_value = new_exc)
		top := e.pop()
		prev := pyerrors.Handled(e.ts)
		if prev != nil {
			e.pushObject(prev)
		} else {
			e.pushObject(objects.None())
		}
		e.push(top)
		if exc, ok := top.AsObject().(*pyerrors.Exception); ok {
			pyerrors.SetHandled(e.ts, exc)
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.POP_EXCEPT:
		// Restore the previously-handled exception saved under the
		// handled-exception ref by PUSH_EXC_INFO. None signals "no
		// outer handler", which clears the slot. The with-statement
		// codegen also emits POP_EXCEPT via COPY 3 / POP_EXCEPT /
		// RERAISE 1 where the popped value is a fresh copy of the
		// active exception, in which case we install that exception
		// as handled, which is harmless (the next handler entry will
		// overwrite, and the RERAISE that follows triggers unwind).
		//
		// CPython: Python/bytecodes.c POP_EXCEPT (exc_info->exc_value = saved)
		ref := e.pop()
		if exc, ok := ref.AsObject().(*pyerrors.Exception); ok {
			pyerrors.SetHandled(e.ts, exc)
		} else {
			pyerrors.SetHandled(e.ts, nil)
		}
		ref.Close()
		return e.advance(), nil, nil, false, true, nil

	case compile.CHECK_EXC_MATCH:
		// Stack: [exc, type]. Push True if isinstance(exc, type), else
		// False. v0.6 doesn't have exception class hierarchy, so use a
		// best-effort string compare.
		typeObj := e.popObject()
		exc := e.peek(0).AsObject()
		match := exceptionMatches(exc, typeObj)
		e.pushObject(objects.NewBool(match))
		return e.advance(), nil, nil, false, true, nil

	case compile.FORMAT_SIMPLE:
		v := e.popObject()
		s, serr := objects.Str(v)
		if serr != nil {
			return 0, nil, nil, false, true, serr
		}
		e.pushObject(objects.NewStr(s))
		return e.advance(), nil, nil, false, true, nil

	case compile.FORMAT_WITH_SPEC:
		// Stack: [value, spec]. Dispatch through objects.Format
		// (PyObject_Format), which routes to the value's __format__
		// slot. An empty spec falls back to Str inside Format.
		//
		// CPython: Python/bytecodes.c FORMAT_WITH_SPEC
		// CPython: Objects/object.c:803 PyObject_Format
		spec := e.popObject()
		v := e.popObject()
		specStr, serr := objects.Str(spec)
		if serr != nil {
			return 0, nil, nil, false, true, serr
		}
		s, ferr := objects.Format(v, specStr)
		if ferr != nil {
			return 0, nil, nil, false, true, ferr
		}
		e.pushObject(objects.NewStr(s))
		return e.advance(), nil, nil, false, true, nil

	case compile.CONVERT_VALUE:
		v := e.popObject()
		out, cerr := convertValue(v, oparg)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

	case compile.BUILD_STRING:
		n := int(oparg)
		pieces := make([]string, n)
		for i := n - 1; i >= 0; i-- {
			v := e.popObject()
			s, serr := objects.Str(v)
			if serr != nil {
				return 0, nil, nil, false, true, serr
			}
			pieces[i] = s
		}
		e.pushObject(objects.NewStr(strings.Join(pieces, "")))
		return e.advance(), nil, nil, false, true, nil

	case compile.MAKE_FUNCTION:
		// TOS is a code object; build a Function bound to the current
		// frame's globals. Defaults, kwdefaults, annotations, and the
		// closure tuple are wired separately by SET_FUNCTION_ATTRIBUTE.
		//
		// CPython: Python/bytecodes.c MAKE_FUNCTION
		v := e.popObject()
		code, ok := v.(*objects.Code)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("MAKE_FUNCTION: TOS not a code object, got %T", v)
		}
		fn := objects.NewFunction(code.Name, code, e.f.Globals)
		e.pushObject(fn)
		return e.advance(), nil, nil, false, true, nil

	case compile.SET_FUNCTION_ATTRIBUTE:
		// Stack: [func, attr]. oparg's bit identifies the attribute:
		// 0x01 = defaults tuple, 0x02 = kwdefaults dict, 0x04 = annotations,
		// 0x08 = closure tuple. v0.6 stores the ones we know about and
		// ignores the rest.
		//
		// CPython: Python/bytecodes.c SET_FUNCTION_ATTRIBUTE
		fnObj := e.popObject()
		attr := e.popObject()
		fn, ok := fnObj.(*objects.Function)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("SET_FUNCTION_ATTRIBUTE: TOS not a function, got %T", fnObj)
		}
		switch oparg {
		case 0x01:
			if t, ok := attr.(*objects.Tuple); ok {
				fn.Defaults = t
			}
		case 0x02:
			if d, ok := attr.(*objects.Dict); ok {
				fn.KwDefaults = d
			}
		case 0x08:
			if t, ok := attr.(*objects.Tuple); ok {
				fn.Closure = t
			}
		}
		e.pushObject(fn)
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_CLOSURE:
		// Same as LOAD_DEREF but pushes the cell itself, not its
		// contents. Used by MAKE_FUNCTION when constructing the
		// closure tuple. CPython: Python/bytecodes.c LOAD_CLOSURE.
		ref := e.f.LocalsPlus[frame.NLocalsOf(e.f.Code)+int(oparg)]
		e.push(ref.Dup())
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_DEREF:
		// oparg indexes the cell+free slots (cells first, then frees).
		//
		// CPython: Python/bytecodes.c LOAD_DEREF
		ref := e.f.LocalsPlus[frame.NLocalsOf(e.f.Code)+int(oparg)]
		cellObj := ref.AsObject()
		cell, ok := cellObj.(*objects.Cell)
		if !ok || cell == nil {
			name := derefName(e.f.Code, int(oparg))
			return 0, nil, nil, false, true, fmt.Errorf("LOAD_DEREF: %s slot %d not a cell in %s:%s, got %T", name, oparg, e.f.Code.Filename, e.f.Code.Name, cellObj)
		}
		if cell.Contents == nil {
			name := derefName(e.f.Code, int(oparg))
			return 0, nil, nil, false, true, fmt.Errorf("NameError: free variable %q referenced before assignment in %s:%s", name, e.f.Code.Filename, e.f.Code.Name)
		}
		e.pushObject(cell.Contents)
		return e.advance(), nil, nil, false, true, nil

	case compile.STORE_DEREF:
		v := e.popObject()
		ref := e.f.LocalsPlus[frame.NLocalsOf(e.f.Code)+int(oparg)]
		cellObj := ref.AsObject()
		cell, ok := cellObj.(*objects.Cell)
		if !ok {
			cell = objects.NewCell(nil)
			e.f.LocalsPlus[frame.NLocalsOf(e.f.Code)+int(oparg)] = stackref.FromObject(cell)
		}
		cell.Contents = v
		return e.advance(), nil, nil, false, true, nil

	case compile.DELETE_DEREF:
		ref := e.f.LocalsPlus[frame.NLocalsOf(e.f.Code)+int(oparg)]
		cell, ok := ref.AsObject().(*objects.Cell)
		if !ok || cell.Contents == nil {
			return 0, nil, nil, false, true, fmt.Errorf("NameError: free variable referenced before assignment")
		}
		cell.Contents = nil
		return e.advance(), nil, nil, false, true, nil

	case compile.MAKE_CELL:
		// Wrap the named local in a fresh cell. oparg is the cellvars
		// index (in gopy's split layout); the cell var name is looked
		// up against varnames so that a parameter that's also a cell
		// var transfers its value into the cell. The cell is stored at
		// the cell-region slot for LOAD_DEREF, and mirrored back to
		// the local slot so the LOAD_FAST that emitClosure threads
		// into the closure tuple picks up the cell rather than the
		// raw parameter value (CPython 3.14 achieves the same effect
		// via overlapped localsplus slots).
		//
		// CPython: Python/bytecodes.c:1863 MAKE_CELL
		co := e.f.Code
		base := frame.NLocalsOf(co)
		cellIdx := int(oparg)
		var name string
		if cellIdx < len(co.Cellvars) {
			name = co.Cellvars[cellIdx]
		}
		localIdx := -1
		for i, vn := range co.Varnames {
			if vn == name {
				localIdx = i
				break
			}
		}
		var contents objects.Object
		if localIdx >= 0 {
			ref := e.f.LocalsPlus[localIdx]
			if !ref.IsNull() {
				contents = ref.AsObject()
			}
		}
		cell := objects.NewCell(contents)
		cellRef := stackref.FromObject(cell)
		e.f.LocalsPlus[base+cellIdx] = cellRef
		if localIdx >= 0 {
			e.f.LocalsPlus[localIdx] = cellRef
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.COPY_FREE_VARS:
		// oparg = number of free vars. Source: f.Func's Closure tuple.
		// Target: free-var slots, which start at NLocals + NCells.
		//
		// CPython: Python/bytecodes.c COPY_FREE_VARS
		n := int(oparg)
		fn, ok := e.f.Func.(*objects.Function)
		if !ok || fn.Closure == nil {
			return 0, nil, nil, false, true, fmt.Errorf("COPY_FREE_VARS: frame has no closure")
		}
		dst := frame.FreesStart(e.f.Code)
		for i := 0; i < n; i++ {
			cell := fn.Closure.Item(i)
			e.f.LocalsPlus[dst+i] = stackref.FromObject(cell)
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.LIST_APPEND:
		// Stack: ..., list, ..., value (oparg slots above list).
		// Pops value, appends to the list at depth oparg.
		//
		// CPython: Python/bytecodes.c LIST_APPEND
		v := e.popObject()
		l, ok := e.peek(int(oparg) - 1).AsObject().(*objects.List)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("LIST_APPEND: target not a list")
		}
		l.Append(v)
		return e.advance(), nil, nil, false, true, nil

	case compile.LIST_EXTEND:
		// Pops iter, extends list at depth oparg with all its items.
		v := e.popObject()
		l, ok := e.peek(int(oparg) - 1).AsObject().(*objects.List)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("LIST_EXTEND: target not a list")
		}
		items, eerr := iterToSlice(v)
		if eerr != nil {
			return 0, nil, nil, false, true, eerr
		}
		for _, it := range items {
			l.Append(it)
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.MAP_ADD:
		// Stack: ..., dict, ..., key, value. Pops key+value, sets in
		// the dict at depth oparg.
		//
		// CPython: Python/bytecodes.c MAP_ADD
		val := e.popObject()
		key := e.popObject()
		d, ok := e.peek(int(oparg) - 1).AsObject().(*objects.Dict)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("MAP_ADD: target not a dict")
		}
		if serr := d.SetItem(key, val); serr != nil {
			return 0, nil, nil, false, true, serr
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.DICT_UPDATE, compile.DICT_MERGE:
		// Pop dict-like and merge into the dict at depth oparg.
		// DICT_MERGE additionally errors on duplicate keys; v0.6 treats
		// both as "last write wins".
		src := e.popObject()
		d, ok := e.peek(int(oparg) - 1).AsObject().(*objects.Dict)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("%s: target not a dict", opcodeName(op))
		}
		srcDict, ok := src.(*objects.Dict)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("%s: source not a dict", opcodeName(op))
		}
		for _, k := range srcDict.Keys() {
			v, gerr := srcDict.GetItem(k)
			if gerr != nil {
				return 0, nil, nil, false, true, gerr
			}
			if serr := d.SetItem(k, v); serr != nil {
				return 0, nil, nil, false, true, serr
			}
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.BUILD_SET:
		// Build a set from n stack items. CPython: Objects/setobject.c BUILD_SET.
		//
		// CPython: Python/bytecodes.c BUILD_SET
		n := int(oparg)
		s := objects.NewSet()
		items := make([]objects.Object, n)
		for i := n - 1; i >= 0; i-- {
			items[i] = e.popObject()
		}
		for _, it := range items {
			if aerr := s.Add(it); aerr != nil {
				return 0, nil, nil, false, true, aerr
			}
		}
		e.pushObject(s)
		return e.advance(), nil, nil, false, true, nil

	case compile.BUILD_TEMPLATE:
		// PEP 750 t-string literal. Stack: [strings_tuple, interpolations_tuple].
		// CPython: Python/bytecodes.c:1977 BUILD_TEMPLATE
		interpolations := e.popObject()
		strings := e.popObject()
		e.pushObject(objects.NewTemplateStr(strings, interpolations))
		return e.advance(), nil, nil, false, true, nil

	case compile.SET_ADD:
		// Stack: ..., set, ..., value (oparg slots above set). Pops value,
		// adds to the set at depth oparg.
		//
		// CPython: Python/bytecodes.c SET_ADD
		v := e.popObject()
		s, ok := e.peek(int(oparg) - 1).AsObject().(*objects.Set)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("SET_ADD: target not a set")
		}
		if aerr := s.Add(v); aerr != nil {
			return 0, nil, nil, false, true, aerr
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.SET_UPDATE:
		// Pop iterable, extend the set at depth oparg.
		//
		// CPython: Python/bytecodes.c SET_UPDATE
		v := e.popObject()
		s, ok := e.peek(int(oparg) - 1).AsObject().(*objects.Set)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("SET_UPDATE: target not a set")
		}
		items, ierr := iterToSlice(v)
		if ierr != nil {
			return 0, nil, nil, false, true, ierr
		}
		for _, it := range items {
			if aerr := s.Add(it); aerr != nil {
				return 0, nil, nil, false, true, aerr
			}
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.SETUP_ANNOTATIONS:
		// SETUP_ANNOTATIONS: ensure __annotations__ is a dict in the
		// current local namespace. CPython looks up "__annotations__"
		// in f_locals, creates it if missing, and leaves the stack
		// untouched. At module scope f_locals == f_globals; in gopy
		// f.Locals is nil for modules and the module dict lives in
		// f.Globals, so route the store there. Class-body frames carry
		// an explicit Locals dict.
		//
		// CPython: Python/bytecodes.c SETUP_ANNOTATIONS
		ns := e.f.Locals
		if ns == nil {
			ns = e.f.Globals
		}
		if d, ok := ns.(*objects.Dict); ok {
			key := objects.NewStr("__annotations__")
			if v, _ := d.GetItem(key); v == nil {
				if serr := d.SetItem(key, objects.NewDict()); serr != nil {
					return 0, nil, nil, false, true, serr
				}
			}
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.CALL_INTRINSIC_1:
		v := e.popObject()
		if int(oparg) >= len(intrinsicsUnary) {
			return 0, nil, nil, false, true, fmt.Errorf("CALL_INTRINSIC_1: oparg %d out of range", oparg)
		}
		// IMPORT_STAR needs the current frame's locals, which the generic
		// intrinsic signature doesn't carry. Route it directly.
		if oparg == intrinsics.UnaryImportStarID {
			if ierr := e.importStar(v); ierr != nil {
				return 0, nil, nil, false, true, ierr
			}
			e.pushObject(objects.None())
			return e.advance(), nil, nil, false, true, nil
		}
		fn := intrinsicsUnary[oparg]
		if fn == nil {
			return 0, nil, nil, false, true, fmt.Errorf("CALL_INTRINSIC_1: id %d unbound", oparg)
		}
		out, cerr := fn(e.ts, v)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

	case compile.CALL_INTRINSIC_2:
		rhs := e.popObject()
		lhs := e.popObject()
		if int(oparg) >= len(intrinsicsBinary) {
			return 0, nil, nil, false, true, fmt.Errorf("CALL_INTRINSIC_2: oparg %d out of range", oparg)
		}
		fn := intrinsicsBinary[oparg]
		if fn == nil {
			return 0, nil, nil, false, true, fmt.Errorf("CALL_INTRINSIC_2: id %d unbound", oparg)
		}
		out, cerr := fn(e.ts, lhs, rhs)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

	case compile.RERAISE:
		// Stack: [values[oparg], exc]. Pop exc and reraise it through the
		// thread state so its real type survives propagation. For
		// oparg >= 1 the bytecode emitter stashed the with-block's
		// lasti at values[0]; restore the frame's instruction pointer
		// from it so the eventual traceback reports the original
		// raising offset, not the cleanup site. values[oparg] stay on
		// stack.
		//
		// CPython: Python/bytecodes.c:1429 RERAISE
		if oparg > 2 {
			return 0, nil, nil, false, true, fmt.Errorf("vm: RERAISE: invalid oparg %d", oparg)
		}
		exc := e.popObject()
		if oparg >= 1 {
			lasti := e.peek(int(oparg) - 1).AsObject()
			if li, ok := lasti.(*objects.Int); ok {
				v, _ := li.Int64()
				// Record the original raising site for traceback only.
				// We must NOT touch InstrPtr: handleException looks the
				// exception table up by InstrPtr, and rewinding it to the
				// raise site would re-enter the same SETUP_WITH handler
				// that just ran __exit__, looping forever.
				//
				// CPython: Python/bytecodes.c RERAISE (SET_LASTI for
				// frame->prev_instr) - we mirror that by stamping
				// PrevInstr, not InstrPtr.
				e.f.PrevInstr = int(v) * 2
			}
		}
		if pyExc, ok := exc.(*pyerrors.Exception); ok {
			pyerrors.Raise(e.ts, pyExc)
			return 0, nil, nil, false, true, excSentinel(pyExc)
		}
		return 0, nil, nil, false, true, fmt.Errorf("%s", objectRepr(exc))

	case compile.LOAD_BUILD_CLASS:
		// Push the __build_class__ builtin so the codegen sequence
		// (PUSH_NULL, body fn, name, *bases, CALL) lands on the right
		// callable. The builtin is registered into the dict by
		// builtins.Init via the BuildClass hook the vm package wires.
		// Frames built without an explicit Builtins dict (the v0.7
		// pattern that uses globals as both) fall through to globals.
		key := objects.NewStr("__build_class__")
		bc, ok := lookupIn(e.f.Builtins, key)
		if !ok {
			bc, ok = lookupIn(e.f.Globals, key)
		}
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("NameError: __build_class__ not found")
		}
		e.pushObject(bc)
		return e.advance(), nil, nil, false, true, nil

	case compile.UNPACK_EX:
		// oparg low byte: items before *rest. high byte: items after.
		// Stack pre: [seq]. Stack post: [item_after_n], ..., [rest_list],
		// ..., [item_before_0]. v0.6 surfaces only the simple case.
		before := int(oparg & 0xFF)
		after := int(oparg >> 8)
		seq := e.popObject()
		items, ierr := iterToSlice(seq)
		if ierr != nil {
			return 0, nil, nil, false, true, ierr
		}
		if len(items) < before+after {
			return 0, nil, nil, false, true, fmt.Errorf("ValueError: not enough values to unpack (expected at least %d, got %d)", before+after, len(items))
		}
		rest := items[before : len(items)-after]
		// Push: tail items, then rest, then head items, in reverse so
		// head[0] ends up at TOS.
		for i := len(items) - 1; i >= len(items)-after; i-- {
			e.pushObject(items[i])
		}
		e.pushObject(objects.NewList(rest))
		for i := before - 1; i >= 0; i-- {
			e.pushObject(items[i])
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.CALL_KW:
		// Stack: [callable, NULL_or_self, arg0, ..., argN, kwnames_tuple].
		// oparg is total positional+keyword count; kwnames tuple length is
		// the keyword count.
		//
		// CPython: Python/bytecodes.c CALL_KW
		kwnamesObj := e.popObject()
		kwnames, ok := kwnamesObj.(*objects.Tuple)
		if !ok {
			return 0, nil, nil, false, true, fmt.Errorf("CALL_KW: kwnames not a tuple")
		}
		nkw := kwnames.Len()
		total := int(oparg)
		npos := total - nkw
		// Lay out args[] flat: positionals first, then keyword values
		// in the same order as kwnames, matching the vectorcall ABI.
		all := make([]objects.Object, total)
		for i := total - 1; i >= 0; i-- {
			all[i] = e.popObject()
		}
		selfOrNull := e.popObject()
		callable := e.popObject()
		if selfOrNull != nil {
			all = append([]objects.Object{selfOrNull}, all...)
			npos++
		}
		out, cerr := objects.Vectorcall(callable, all, uint(npos), kwnames)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.CALL_KW), nil, nil, false, true, nil

	case compile.CALL_FUNCTION_EX:
		// Stack: [callable, NULL, args_iterable, kwargs_dict_or_NULL].
		// oparg bit 0: kwargs present.
		//
		// CPython: Python/bytecodes.c CALL_FUNCTION_EX
		var kwargs *objects.Dict
		if oparg&1 != 0 {
			kwObj := e.popObject()
			d, ok := kwObj.(*objects.Dict)
			if !ok {
				return 0, nil, nil, false, true, fmt.Errorf("CALL_FUNCTION_EX: kwargs not a dict")
			}
			kwargs = d
		}
		argsObj := e.popObject()
		argsSlice, ierr := iterToSlice(argsObj)
		if ierr != nil {
			return 0, nil, nil, false, true, ierr
		}
		_ = e.popObject() // NULL_or_self placeholder
		callable := e.popObject()
		out, cerr := objects.Call(callable, objects.NewTuple(argsSlice), kwargs)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

	case compile.BINARY_SLICE:
		// Stack: [container, start, stop]. Push container[start:stop].
		stop := e.popObject()
		start := e.popObject()
		container := e.popObject()
		out, serr := sliceContainer(container, start, stop)
		if serr != nil {
			return 0, nil, nil, false, true, serr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

	case compile.STORE_SLICE:
		// Stack: [value, container, start, stop]. Replace container[start:stop] with value.
		stop := e.popObject()
		start := e.popObject()
		container := e.popObject()
		value := e.popObject()
		if serr := storeSlice(container, start, stop, value); serr != nil {
			return 0, nil, nil, false, true, serr
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.GET_LEN:
		// Push len(TOS) without consuming TOS.
		v := e.peek(0).AsObject()
		t := v.Type()
		var n int
		switch {
		case t.Sequence != nil && t.Sequence.Length != nil:
			x, lerr := t.Sequence.Length(v)
			if lerr != nil {
				return 0, nil, nil, false, true, lerr
			}
			n = x
		case t.Mapping != nil && t.Mapping.Length != nil:
			x, lerr := t.Mapping.Length(v)
			if lerr != nil {
				return 0, nil, nil, false, true, lerr
			}
			n = x
		default:
			return 0, nil, nil, false, true, fmt.Errorf("TypeError: object of type '%s' has no len()", t.Name)
		}
		e.pushObject(objects.NewInt(int64(n)))
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_FAST_LOAD_FAST, compile.LOAD_FAST_BORROW_LOAD_FAST_BORROW:
		// Two local indexes packed: high nibble first, low nibble second.
		// The BORROW variant is identical under Go GC.
		hi := int(oparg >> 4)
		lo := int(oparg & 0xF)
		r1 := e.localAt(hi)
		r2 := e.localAt(lo)
		if r1.IsNull() || r2.IsNull() {
			return 0, nil, nil, false, true, fmt.Errorf("LOAD_FAST_LOAD_FAST: unbound local")
		}
		e.push(r1.Dup())
		e.push(r2.Dup())
		return e.advance(), nil, nil, false, true, nil

	case compile.STORE_FAST_LOAD_FAST:
		// Pop TOS into local hi, then load local lo onto the stack.
		hi := int(oparg >> 4)
		lo := int(oparg & 0xF)
		ref := e.pop()
		old := e.localAt(hi)
		old.Close()
		e.setLocal(hi, ref)
		r2 := e.localAt(lo)
		if r2.IsNull() {
			return 0, nil, nil, false, true, fmt.Errorf("STORE_FAST_LOAD_FAST: local %d unbound", lo)
		}
		e.push(r2.Dup())
		return e.advance(), nil, nil, false, true, nil

	case compile.STORE_FAST_STORE_FAST:
		// Pop TOS into local hi, then pop the new TOS into local lo.
		hi := int(oparg >> 4)
		lo := int(oparg & 0xF)
		r1 := e.pop()
		oldHi := e.localAt(hi)
		oldHi.Close()
		e.setLocal(hi, r1)
		r2 := e.pop()
		oldLo := e.localAt(lo)
		oldLo.Close()
		e.setLocal(lo, r2)
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_SMALL_INT:
		// 3.14 fast path: oparg is the literal int value (0..255). No
		// const lookup, no indirection.
		e.pushObject(objects.NewInt(int64(oparg)))
		return e.advance(), nil, nil, false, true, nil

	case compile.TO_BOOL:
		// Replace TOS with bool(TOS).
		v := e.popObject()
		truthy, terr := objects.IsTruthy(v)
		if terr != nil {
			return 0, nil, nil, false, true, terr
		}
		e.pushObject(objects.NewBool(truthy))
		return e.cacheAdvance(compile.TO_BOOL), nil, nil, false, true, nil

	case compile.NOT_TAKEN:
		// 3.14 marker that flags the not-taken branch of a conditional
		// jump. Has no runtime effect; CPython uses it for monitoring.
		return e.advance(), nil, nil, false, true, nil

	case compile.POP_BLOCK:
		// 3.14 keeps POP_BLOCK as a pseudo for old codegen paths. No
		// block stack in our frame model, so it's a no-op.
		return e.advance(), nil, nil, false, true, nil

	case compile.POP_ITER:
		// Pop the exhausted iterator left on the stack by FOR_ITER's
		// fall-through. CPython 3.14 made this an opcode of its own.
		ref := e.pop()
		ref.Close()
		return e.advance(), nil, nil, false, true, nil

	case compile.JUMP, compile.JUMP_NO_INTERRUPT:
		// Unconditional pseudo-jumps emitted by 3.14 codegen for
		// optimized forms. We treat them as forward jumps; the
		// NO_INTERRUPT variant skips the eval breaker poll.
		return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil

	case compile.END_SEND:
		// `yield from` cleanup. Stack is (receiver, value); we want
		// the value on top and the receiver dropped.
		// CPython: Python/bytecodes.c END_SEND
		val := e.pop()
		recv := e.pop()
		recv.Close()
		e.push(val)
		return e.advance(), nil, nil, false, true, nil

	case compile.EXIT_INIT_CHECK:
		// `__init__` must return None. Pop the return value and raise
		// TypeError if it's something else.
		// CPython: Python/bytecodes.c EXIT_INIT_CHECK
		v := e.popObject()
		if v != objects.None() {
			return 0, nil, nil, false, true, fmt.Errorf("TypeError: __init__() should return None, not '%s'", v.Type().Name)
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_FROM_DICT_OR_GLOBALS:
		// PEP 695 helper: lookup name in the dict at TOS first; if
		// absent, fall back to LOAD_GLOBAL semantics (globals -> builtins).
		// CPython: Python/bytecodes.c LOAD_FROM_DICT_OR_GLOBALS.
		dictTOS := e.popObject()
		nameObj := e.f.Code.Names[oparg]
		nameKey := objects.NewStr(nameObj)
		if d, ok := dictTOS.(*objects.Dict); ok {
			if v, derr := d.GetItem(nameKey); derr == nil && v != nil {
				e.pushObject(v)
				return e.advance(), nil, nil, false, true, nil
			}
		}
		v, perr := e.execNameOp(compile.LOAD_GLOBAL, oparg)
		if perr != nil {
			return 0, nil, nil, false, true, perr
		}
		if v != nil {
			e.pushObject(v)
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_COMMON_CONSTANT:
		// 3.14 fast load for a small set of compiler-emitted constants.
		// Index 0 is AssertionError, used by `assert`. Without an
		// exception class hierarchy yet (1686), we surface a Type object
		// whose name RAISE_VARARGS can format into a runtime error.
		// CPython: Python/bytecodes.c LOAD_COMMON_CONSTANT.
		switch oparg {
		case 0:
			e.pushObject(commonConstant("AssertionError"))
		case 1:
			e.pushObject(commonConstant("NotImplementedError"))
		default:
			return 0, nil, nil, false, true, fmt.Errorf("LOAD_COMMON_CONSTANT: unknown index %d", oparg)
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_LOCALS:
		// Push the f_locals dict. Class-body frames keep an explicit
		// Locals dict; fast-locals frames synthesize one from the
		// current LocalsPlus values keyed by Code.Varnames.
		if e.f.Locals != nil {
			e.pushObject(e.f.Locals)
			return e.advance(), nil, nil, false, true, nil
		}
		d := objects.NewDict()
		for i, name := range e.f.Code.Varnames {
			ref := e.localAt(i)
			if ref.IsNull() {
				continue
			}
			if serr := d.SetItem(objects.NewStr(name), ref.AsObject()); serr != nil {
				return 0, nil, nil, false, true, serr
			}
		}
		e.pushObject(d)
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_FROM_DICT_OR_DEREF:
		// PEP 695 helper: look up name in the dict at TOS first; if absent,
		// fall back to LOAD_DEREF semantics. v0.6 doesn't have class
		// namespace machinery, so the dict path stays a stub and we
		// dispatch through to LOAD_DEREF.
		_ = e.popObject() // discard the class dict TOS
		ref := e.f.LocalsPlus[frame.NLocalsOf(e.f.Code)+int(oparg)]
		cell, ok := ref.AsObject().(*objects.Cell)
		if !ok || cell.Contents == nil {
			return 0, nil, nil, false, true, fmt.Errorf("NameError: free variable referenced before assignment")
		}
		e.pushObject(cell.Contents)
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_NAME, compile.LOAD_GLOBAL, compile.STORE_NAME,
		compile.STORE_GLOBAL, compile.DELETE_NAME, compile.DELETE_GLOBAL:
		v, perr := e.execNameOp(op, oparg)
		if perr != nil {
			return 0, nil, nil, false, true, perr
		}
		_ = v
		return e.cacheAdvance(op), nil, nil, false, true, nil

	case compile.LOAD_ATTR:
		if perr := e.execLoadAttr(oparg); perr != nil {
			return 0, nil, nil, false, true, perr
		}
		return e.cacheAdvance(compile.LOAD_ATTR), nil, nil, false, true, nil

	case compile.STORE_ATTR:
		if perr := e.execStoreAttr(oparg); perr != nil {
			return 0, nil, nil, false, true, perr
		}
		return e.cacheAdvance(compile.STORE_ATTR), nil, nil, false, true, nil

	case compile.DELETE_ATTR:
		if perr := e.execDeleteAttr(oparg); perr != nil {
			return 0, nil, nil, false, true, perr
		}
		return e.advance(), nil, nil, false, true, nil
	}
	return 0, nil, nil, false, false, nil
}

// execLoadAttr implements the LOAD_ATTR macro: pop owner, look up
// co.Names[oparg>>1] via PyObject_GetAttr, push attr (and a NULL
// self slot when oparg&1 is set so a following CALL sees the
// (callable, NULL) shape that matches the unbound-method specialize
// path in CPython).
//
// CPython: Python/bytecodes.c:2296 _LOAD_ATTR
func (e *evalState) execLoadAttr(oparg uint32) error {
	co := e.f.Code
	idx := int(oparg >> 1)
	if idx < 0 || idx >= len(co.Names) {
		return fmt.Errorf("vm: LOAD_ATTR: name index %d out of range", idx)
	}
	owner := e.popObject()
	name := objects.NewStr(co.Names[idx])
	attr, err := objects.GetAttr(owner, name)
	if err != nil {
		return err
	}
	if oparg&1 != 0 {
		// Unbound-method shape: push attr first, then the NULL self
		// slot. CPython's CALL trampoline reads SELF below CALLABLE,
		// so the second push goes on top.
		e.pushObject(attr)
		e.push(stackref.Null)
		return nil
	}
	e.pushObject(attr)
	return nil
}

// execStoreAttr implements STORE_ATTR: pop owner, then value, write
// owner.name = value via PyObject_SetAttr.
//
// CPython: Python/bytecodes.c:1652 _STORE_ATTR
func (e *evalState) execStoreAttr(oparg uint32) error {
	co := e.f.Code
	idx := int(oparg)
	if idx < 0 || idx >= len(co.Names) {
		return fmt.Errorf("vm: STORE_ATTR: name index %d out of range", idx)
	}
	owner := e.popObject()
	value := e.popObject()
	name := objects.NewStr(co.Names[idx])
	return objects.SetAttr(owner, name, value)
}

// execDeleteAttr implements DELETE_ATTR: pop owner, delete
// owner.name via PyObject_DelAttr.
//
// CPython: Python/bytecodes.c:1662 DELETE_ATTR
func (e *evalState) execDeleteAttr(oparg uint32) error {
	co := e.f.Code
	idx := int(oparg)
	if idx < 0 || idx >= len(co.Names) {
		return fmt.Errorf("vm: DELETE_ATTR: name index %d out of range", idx)
	}
	owner := e.popObject()
	name := objects.NewStr(co.Names[idx])
	return objects.DelAttr(owner, name)
}

// execNameOp handles LOAD_NAME / LOAD_GLOBAL / STORE_NAME etc. Looks
// the name up in (in order) f.Locals, f.Globals, f.Builtins. Stores
// land in f.Locals (or f.Globals for STORE_GLOBAL).
//
// CPython: Python/bytecodes.c LOAD_NAME / LOAD_GLOBAL_NAME panel
func (e *evalState) execNameOp(op compile.Opcode, oparg uint32) (objects.Object, error) {
	co := e.f.Code
	// LOAD_GLOBAL packs a "push null" flag into bit 0 of oparg, with
	// the real name index in bits 1+.
	idx := int(oparg)
	pushNull := false
	if op == compile.LOAD_GLOBAL {
		pushNull = (oparg & 1) != 0
		idx = int(oparg >> 1)
	}
	if idx < 0 || idx >= len(co.Names) {
		return nil, fmt.Errorf("vm: %s: name index %d out of range", op.Name(), idx)
	}
	name := co.Names[idx]
	keyObj := objects.NewStr(name)

	switch op {
	case compile.LOAD_NAME:
		if v, ok := lookupIn(e.f.Locals, keyObj); ok {
			e.pushObject(v)
			return v, nil
		}
		fallthrough
	case compile.LOAD_GLOBAL:
		if pushNull {
			e.push(stackref.Null)
		}
		if v, ok := lookupIn(e.f.Globals, keyObj); ok {
			e.pushObject(v)
			return v, nil
		}
		if v, ok := lookupIn(e.f.Builtins, keyObj); ok {
			e.pushObject(v)
			return v, nil
		}
		return nil, fmt.Errorf("vm: NameError: name '%s' is not defined", name)

	case compile.STORE_NAME:
		v := e.popObject()
		dst := e.f.Locals
		if dst == nil {
			dst = e.f.Globals
		}
		return nil, storeIn(dst, keyObj, v)
	case compile.STORE_GLOBAL:
		v := e.popObject()
		return nil, storeIn(e.f.Globals, keyObj, v)
	case compile.DELETE_NAME:
		dst := e.f.Locals
		if dst == nil {
			dst = e.f.Globals
		}
		return nil, deleteIn(dst, keyObj, name)
	case compile.DELETE_GLOBAL:
		return nil, deleteIn(e.f.Globals, keyObj, name)
	}
	return nil, fmt.Errorf("vm: unhandled name op %s", op.Name())
}

// binaryOp dispatches one BINARY_OP suboperator. Mirrors the NB_*
// constants the compiler emits in compile/codegen_expr_op.go. The
// inplace variants share the non-inplace slot for ints because Int is
// immutable; once mutable container ops land they take their own
// slots.
//
// CPython: Python/bytecodes.c BINARY_OP_GENERIC
func binaryOp(sub int32, a, b objects.Object) (objects.Object, error) {
	const (
		nbAdd         = 0
		nbAnd         = 1
		nbFloorDivide = 2
		nbLshift      = 3
		nbMult        = 5
		nbRemainder   = 6
		nbOr          = 7
		nbPower       = 8
		nbRshift      = 9
		nbSubtract    = 10
		nbTrueDivide  = 11
		nbXor         = 12
		// Inplace forms (13..25) re-use the non-inplace slot for
		// immutable types; the mapping mirrors CPython's NB_INPLACE_*
		// alphabetical numbering.
		nbInplaceAdd         = 13
		nbInplaceAnd         = 14
		nbInplaceFloorDivide = 15
		nbInplaceLshift      = 16
		nbInplaceMult        = 18
		nbInplaceRemainder   = 19
		nbInplaceOr          = 20
		nbInplacePower       = 21
		nbInplaceRshift      = 22
		nbInplaceSubtract    = 23
		nbInplaceTrueDivide  = 24
		nbInplaceXor         = 25
		nbSubscr             = 26
	)
	switch sub {
	case nbAdd:
		return objects.NumberAdd(a, b)
	case nbInplaceAdd:
		return objects.NumberInPlaceAdd(a, b)
	case nbSubtract, nbInplaceSubtract:
		return numericForward(a, b, "-", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Subtract
		})
	case nbMult:
		return objects.NumberMultiply(a, b)
	case nbInplaceMult:
		return objects.NumberInPlaceMultiply(a, b)
	case nbTrueDivide, nbInplaceTrueDivide:
		return numericForward(a, b, "/", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.TrueDivide
		})
	case nbFloorDivide, nbInplaceFloorDivide:
		return numericForward(a, b, "//", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.FloorDivide
		})
	case nbRemainder, nbInplaceRemainder:
		return numericForward(a, b, "%", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Remainder
		})
	case nbAnd, nbInplaceAnd:
		return numericForward(a, b, "&", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.And
		})
	case nbOr, nbInplaceOr:
		return numericForward(a, b, "|", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Or
		})
	case nbXor, nbInplaceXor:
		return numericForward(a, b, "^", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Xor
		})
	case nbLshift, nbInplaceLshift:
		return numericForward(a, b, "<<", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Lshift
		})
	case nbRshift, nbInplaceRshift:
		return numericForward(a, b, ">>", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Rshift
		})
	case nbPower, nbInplacePower:
		return powerOp(a, b, nil)
	case nbSubscr:
		return getItem(a, b)
	}
	return nil, fmt.Errorf("vm: BINARY_OP suboperator %d not implemented in v0.6", sub)
}

// powerOp routes BINARY_OP NB_POWER through NumberMethods.Power on
// either operand, mirroring numericForward's NotImplemented walk.
// The optional `mod` argument is reserved for the three-arg pow()
// builtin; the bytecode form always passes nil.
//
// CPython: Objects/abstract.c PyNumber_Power
func powerOp(a, b, mod objects.Object) (objects.Object, error) {
	if n := a.Type().Number; n != nil && n.Power != nil {
		out, err := n.Power(a, b, mod)
		if err != nil {
			return nil, err
		}
		if !objects.IsNotImplemented(out) {
			return out, nil
		}
	}
	if n := b.Type().Number; n != nil && n.Power != nil {
		out, err := n.Power(a, b, mod)
		if err != nil {
			return nil, err
		}
		if !objects.IsNotImplemented(out) {
			return out, nil
		}
	}
	return nil, fmt.Errorf("TypeError: unsupported operand type(s) for ** or pow(): '%s' and '%s'", a.Type().Name, b.Type().Name)
}

// numericForward walks a's number slot first, then b's, returning
// the first concrete result. A slot that returns the NotImplemented
// singleton signals "try the other operand" and is treated as a
// fall-through, matching how CPython's abstract layer steps through
// the forward / reflected pair. The lookup walks each operand's MRO
// so subclasses (e.g. bool, which leaves NumberMethods nil) inherit
// the parent's slot table.
//
// CPython: Objects/abstract.c binary_op1
func numericForward(a, b objects.Object, sym string, pick func(*objects.NumberMethods) func(a, b objects.Object) (objects.Object, error)) (objects.Object, error) {
	if fn := mroNumberSlot(a, pick); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !objects.IsNotImplemented(out) {
			return out, nil
		}
	}
	if fn := mroNumberSlot(b, pick); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !objects.IsNotImplemented(out) {
			return out, nil
		}
	}
	return nil, fmt.Errorf("TypeError: unsupported operand type(s) for %s: '%s' and '%s'", sym, a.Type().Name, b.Type().Name)
}

// mroNumberSlot resolves a NumberMethods slot via MRO walk, mirroring
// CPython's slot inheritance from PyType_Ready.
//
// CPython: Objects/typeobject.c:7895 inherit_slots
func mroNumberSlot(o objects.Object, pick func(*objects.NumberMethods) func(a, b objects.Object) (objects.Object, error)) func(a, b objects.Object) (objects.Object, error) {
	for _, base := range o.Type().MRO {
		if base == nil || base.Number == nil {
			continue
		}
		if fn := pick(base.Number); fn != nil {
			return fn
		}
	}
	return nil
}

func lookupIn(scope objects.Object, key objects.Object) (objects.Object, bool) {
	if scope == nil {
		return nil, false
	}
	// Exact-dict fast path. A dict subclass (e.g. enum.EnumDict, or any
	// class returned by a metaclass __prepare__) has to go through the
	// mapping protocol so an overridden __getitem__ fires.
	//
	// CPython: Python/bytecodes.c LOAD_NAME (PyMapping_GetOptionalItem
	// on locals)
	if d, ok := scope.(*objects.Dict); ok && scope.Type() == objects.DictType {
		v, err := d.GetItem(key)
		if err != nil {
			return nil, false
		}
		return v, true
	}
	v, err := objects.GetItem(scope, key)
	if err != nil {
		return nil, false
	}
	return v, true
}

func storeIn(scope objects.Object, key, value objects.Object) error {
	if scope == nil {
		return fmt.Errorf("vm: cannot store name: scope is nil")
	}
	// Exact-dict fast path. A dict subclass (e.g. enum.EnumDict) must go
	// through the mapping protocol so its overridden __setitem__ fires.
	//
	// CPython: Python/ceval.c STORE_NAME uses PyObject_SetItem on locals
	if d, ok := scope.(*objects.Dict); ok && scope.Type() == objects.DictType {
		return d.SetItem(key, value)
	}
	return objects.SetItem(scope, key, value)
}

// unpackSeq unpacks seq into exactly n items, mirroring CPython's
// UNPACK_SEQUENCE error texts on length mismatch.
//
// CPython: Python/bytecodes.c UNPACK_SEQUENCE
func unpackSeq(seq objects.Object, n int) ([]objects.Object, error) {
	if t, ok := seq.(*objects.Tuple); ok {
		if t.Len() != n {
			return nil, fmt.Errorf("ValueError: not enough values to unpack (expected %d, got %d)", n, t.Len())
		}
		out := make([]objects.Object, n)
		for i := 0; i < n; i++ {
			out[i] = t.Item(i)
		}
		return out, nil
	}
	if l, ok := seq.(*objects.List); ok {
		if l.Len() != n {
			return nil, fmt.Errorf("ValueError: not enough values to unpack (expected %d, got %d)", n, l.Len())
		}
		out := make([]objects.Object, n)
		seq := l.Type().Sequence
		for i := 0; i < n; i++ {
			v, gerr := seq.GetItem(l, i)
			if gerr != nil {
				return nil, gerr
			}
			out[i] = v
		}
		return out, nil
	}
	// Fall back to the iterator protocol for arbitrary iterables.
	t := seq.Type()
	if t.Iter == nil {
		return nil, fmt.Errorf("TypeError: cannot unpack non-iterable '%s' object", t.Name)
	}
	it, ierr := t.Iter(seq)
	if ierr != nil {
		return nil, ierr
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not an iterator", itType.Name)
	}
	out := make([]objects.Object, 0, n)
	for {
		v, nerr := itType.IterNext(it)
		if errors.Is(nerr, objects.ErrStopIteration) {
			break
		}
		if nerr != nil {
			return nil, nerr
		}
		out = append(out, v)
	}
	if len(out) != n {
		return nil, fmt.Errorf("ValueError: not enough values to unpack (expected %d, got %d)", n, len(out))
	}
	return out, nil
}

// getItem mirrors PyObject_GetItem against the v0.6 container surface.
// Mappings (Dict) take a key; sequences (List/Tuple/Str) take an int
// index that may be negative (counted from the end).
//
// CPython: Objects/abstract.c PyObject_GetItem
func getItem(container, key objects.Object) (objects.Object, error) {
	t := container.Type()
	mp, sq := mappingAndSequence(t)
	if mp != nil && mp.GetItem != nil {
		return mp.GetItem(container, key)
	}
	if sq != nil && sq.GetItem != nil {
		if sl, ok := key.(*objects.Slice); ok {
			return sliceSequence(container, sl)
		}
		idx, ok := key.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: '%s' indices must be integers, not %s", t.Name, key.Type().Name)
		}
		i, _ := idx.Int64()
		if sq.Length != nil {
			n, lerr := sq.Length(container)
			if lerr != nil {
				return nil, lerr
			}
			if i < 0 {
				i += int64(n)
			}
			if i < 0 || i >= int64(n) {
				return nil, fmt.Errorf("IndexError: %s index out of range", t.Name)
			}
		}
		return sq.GetItem(container, int(i))
	}
	if cls, ok := container.(*objects.Type); ok {
		return typeSubscript(cls, key)
	}
	return nil, fmt.Errorf("TypeError: '%s' object is not subscriptable", t.Name)
}

// typeSubscript implements `cls[args]` by reaching for
// __class_getitem__ on cls's MRO. Mirrors the trailing PyType_Check
// branch of PyObject_GetItem. The type singleton itself short-circuits
// to Py_GenericAlias(type, key) so `type[int]` matches CPython.
//
// CPython: Objects/abstract.c:181 PyObject_GetItem (type branch)
func typeSubscript(cls *objects.Type, key objects.Object) (objects.Object, error) {
	if cls == objects.TypeType() {
		return objects.NewGenericAlias(cls, key), nil
	}
	descr, _ := objects.LookupDescriptor(cls, "__class_getitem__")
	if descr != nil {
		// Bind the descriptor against cls so a classmethod (or a plain
		// callable installed via SetTypeDescr) sees the class as its
		// implicit first argument.
		dt := descr.Type()
		bound := descr
		if dt.DescrGet != nil {
			v, err := dt.DescrGet(descr, cls, cls)
			if err != nil {
				return nil, err
			}
			bound = v
		}
		return objects.Call(bound, objects.NewTuple([]objects.Object{key}), nil)
	}
	return nil, fmt.Errorf("TypeError: type '%s' is not subscriptable", cls.Name)
}

// sliceSequence walks a sequence's GetItem slot once per index in the
// slice. CPython lists / tuples / strings each have their own
// PySequence_GetSlice fast path; gopy uses a single generic loop
// against the int-indexed Sequence.GetItem, then rewraps the result
// in the source container's type. Negative or zero-length slices
// produce an empty container.
//
// CPython: Objects/abstract.c PyObject_GetItem slice branch +
// per-type sq_slice routines (list_subscript, tuple_subscript,
// unicode_subscript)
func sliceSequence(container objects.Object, sl *objects.Slice) (objects.Object, error) {
	t := container.Type()
	if t.Sequence == nil || t.Sequence.Length == nil || t.Sequence.GetItem == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not subscriptable", t.Name)
	}
	n, err := t.Sequence.Length(container)
	if err != nil {
		return nil, err
	}
	start, _, step, count, err := sl.GetIndices(n)
	if err != nil {
		return nil, err
	}
	items := make([]objects.Object, 0, count)
	for i := 0; i < count; i++ {
		v, gerr := t.Sequence.GetItem(container, start+i*step)
		if gerr != nil {
			return nil, gerr
		}
		items = append(items, v)
	}
	switch container.(type) {
	case *objects.List:
		return objects.NewList(items), nil
	case *objects.Tuple:
		return objects.NewTuple(items), nil
	case *objects.Unicode:
		var sb strings.Builder
		for _, it := range items {
			s, _ := objects.Str(it)
			sb.WriteString(s)
		}
		return objects.NewStr(sb.String()), nil
	}
	return objects.NewList(items), nil
}

// delItem mirrors PyObject_DelItem.
//
// CPython: Objects/abstract.c PyObject_DelItem
func delItem(container, key objects.Object) error {
	t := container.Type()
	mp, _ := mappingAndSequence(t)
	if mp != nil && mp.DelItem != nil {
		return mp.DelItem(container, key)
	}
	return fmt.Errorf("TypeError: '%s' object does not support item deletion", t.Name)
}

// containsItem mirrors PySequence_Contains. Falls back to walking the
// iterator when the type provides no Contains slot.
//
// CPython: Objects/abstract.c PySequence_Contains
func containsItem(haystack, needle objects.Object) (bool, error) {
	t := haystack.Type()
	if t.Sequence != nil && t.Sequence.Contains != nil {
		return t.Sequence.Contains(haystack, needle)
	}
	if d, ok := haystack.(*objects.Dict); ok {
		v, _ := d.GetItem(needle)
		return v != nil, nil
	}
	if t.Iter == nil {
		return false, fmt.Errorf("TypeError: argument of type '%s' is not iterable", t.Name)
	}
	items, ierr := iterToSlice(haystack)
	if ierr != nil {
		return false, ierr
	}
	for _, item := range items {
		eq, eerr := objects.RichCmp(item, needle, objects.CompareEQ)
		if eerr != nil {
			return false, eerr
		}
		truthy, terr := objects.IsTruthy(eq)
		if terr != nil {
			return false, terr
		}
		if truthy {
			return true, nil
		}
	}
	return false, nil
}

// setItem mirrors PyObject_SetItem against the v0.6 container surface.
// Walks the MRO so a subclass of a built-in container picks up the
// base's mapping/sequence slot when the subclass itself does not
// override it. Mirrors CPython's inherit_slots running at class
// readiness; gopy does the walk on each call instead.
//
// CPython: Objects/abstract.c PyObject_SetItem
func setItem(container, key, value objects.Object) error {
	mp, sq := mappingAndSequence(container.Type())
	if mp != nil && mp.SetItem != nil {
		return mp.SetItem(container, key, value)
	}
	if sq != nil && sq.SetItem != nil {
		idx, ok := key.(*objects.Int)
		if !ok {
			return fmt.Errorf("TypeError: %s indices must be integers, not %s", container.Type().Name, key.Type().Name)
		}
		i, _ := idx.Int64()
		return sq.SetItem(container, int(i), value)
	}
	return fmt.Errorf("TypeError: '%s' object does not support item assignment", container.Type().Name)
}

// mappingAndSequence walks t's MRO and returns the first Mapping and
// Sequence bundles found. Either return value may be nil. Lets a
// subclass inherit container behavior without copying slot tables at
// class-creation time.
func mappingAndSequence(t *objects.Type) (*objects.MappingMethods, *objects.SequenceMethods) {
	var mp *objects.MappingMethods
	var sq *objects.SequenceMethods
	for _, b := range t.MRO {
		if mp == nil && b.Mapping != nil {
			mp = b.Mapping
		}
		if sq == nil && b.Sequence != nil {
			sq = b.Sequence
		}
		if mp != nil && sq != nil {
			break
		}
	}
	return mp, sq
}

// commonConstantsCache memoises the placeholder Type objects
// LOAD_COMMON_CONSTANT pushes. Real exception classes arrive with the
// exceptions module port (1686).
var commonConstantsCache = map[string]*objects.Type{}

func commonConstant(name string) *objects.Type {
	if t, ok := commonConstantsCache[name]; ok {
		return t
	}
	t := objects.NewType(name, nil)
	commonConstantsCache[name] = t
	return t
}

// raiseValue is the do-raise body of RAISE_VARARGS. val is whatever
// the bytecode pushed for the raise statement: an exception instance,
// an exception class (CPython instantiates it with no args), or some
// other object (TypeError). cause is the `from <cause>` value or nil.
// raiseValue installs the resulting exception on the thread state and
// returns the Go sentinel that drives the unwind loop.
//
// CPython: Python/ceval.c:L3105 do_raise
func raiseValue(ts *state.Thread, val, cause objects.Object) error {
	exc, err := normalizeRaise(val)
	if err != nil {
		return err
	}
	if cause != nil {
		causeExc, cerr := normalizeCause(cause)
		if cerr != nil {
			return cerr
		}
		pyerrors.RaiseFrom(ts, exc, causeExc)
	} else {
		pyerrors.Raise(ts, exc)
	}
	return excSentinel(exc)
}

// normalizeRaise mirrors the head of do_raise: if val is already an
// exception instance, keep it; if it is an exception class, call it
// with no args; otherwise raise TypeError.
//
// CPython: Python/ceval.c:L3140 do_raise (the PyExceptionInstance_Check
// / PyExceptionClass_Check branches)
func normalizeRaise(val objects.Object) (*pyerrors.Exception, error) {
	if exc, ok := val.(*pyerrors.Exception); ok {
		return exc, nil
	}
	if t, ok := val.(*objects.Type); ok && pyerrors.IsSubtype(t, pyerrors.PyExc_BaseException) {
		return pyerrors.New(t, objects.NewTuple(nil)), nil
	}
	return nil, fmt.Errorf("TypeError: exceptions must derive from BaseException")
}

// normalizeCause handles the `from <cause>` operand. None clears the
// cause; otherwise the same exception-instance-or-class normalization
// runs.
//
// CPython: Python/ceval.c:L3119 do_raise (cause branch)
func normalizeCause(cause objects.Object) (*pyerrors.Exception, error) {
	if cause == nil || cause == objects.None() {
		return nil, nil
	}
	return normalizeRaise(cause)
}

// excSentinel returns the Go error the unwind loop sees once the
// exception is on the thread state. The text mirrors
// `repr(exc)`-style output so any test that pins err.Error() before
// the proper traceback printer lands keeps working.
func excSentinel(exc *pyerrors.Exception) error {
	if exc == nil {
		return errors.New("Exception")
	}
	msg := exc.Message()
	if msg == "" {
		return fmt.Errorf("%s", exc.TypeName())
	}
	return fmt.Errorf("%s: %s", exc.TypeName(), msg)
}

// derefName returns the cell or free variable name at index idx in
// the cell+free layout. Used purely for error messages so a failing
// LOAD_DEREF tells the operator which name is unbound.
func derefName(co *objects.Code, idx int) string {
	if idx < len(co.Cellvars) {
		return co.Cellvars[idx]
	}
	if i := idx - len(co.Cellvars); i >= 0 && i < len(co.Freevars) {
		return co.Freevars[i]
	}
	return "<unknown>"
}

// objectRepr returns repr(o), falling back to a placeholder so error
// messages from RAISE_VARARGS don't double-fault.
func objectRepr(o objects.Object) string {
	s, err := objects.Repr(o)
	if err != nil {
		return "<unrepresentable>"
	}
	return s
}

// exceptionMatches tests whether exc matches the type-or-tuple-of-types
// in match. v0.6 has no exception class hierarchy, so the test is a
// best-effort comparison: same type identity, or same type name when
// the match value is a Type.
func exceptionMatches(exc, match objects.Object) bool {
	if t, ok := match.(*objects.Type); ok {
		// PEP 3134 / CPython give_exception_matches: an exception
		// matches when its type is a (non-strict) subclass of the
		// handler type. Plain equality drops ModuleNotFoundError when
		// the source wrote `except ImportError:`, which breaks every
		// stdlib module that tries an optional accelerator import.
		//
		// CPython: Python/errors.c:218 PyErr_GivenExceptionMatches
		if pyerrors.IsSubtype(exc.Type(), t) {
			return true
		}
		return exc.Type() == t || exc.Type().Name == t.Name
	}
	if tup, ok := match.(*objects.Tuple); ok {
		for i := 0; i < tup.Len(); i++ {
			if exceptionMatches(exc, tup.Item(i)) {
				return true
			}
		}
	}
	return false
}

// convertValue mirrors CPython's CONVERT_VALUE oparg encoding:
// 1=str, 2=repr, 3=ascii. v0.6 maps str/repr to objects.Str/Repr; ascii
// is treated as repr until the abstract layer ports ascii() proper.
//
// CPython: Python/bytecodes.c CONVERT_VALUE
func convertValue(v objects.Object, oparg uint32) (objects.Object, error) {
	switch oparg {
	case 1: // FVC_STR
		s, err := objects.Str(v)
		if err != nil {
			return nil, err
		}
		return objects.NewStr(s), nil
	case 2, 3: // FVC_REPR / FVC_ASCII
		s, err := objects.Repr(v)
		if err != nil {
			return nil, err
		}
		return objects.NewStr(s), nil
	}
	return nil, fmt.Errorf("vm: CONVERT_VALUE: unknown oparg %d", oparg)
}

func deleteIn(scope objects.Object, key objects.Object, name string) error {
	if scope == nil {
		return fmt.Errorf("vm: NameError: name '%s' is not defined", name)
	}
	// Exact-dict fast path. Dict subclasses (and any non-Dict mapping
	// returned by a metaclass __prepare__) need to route through the
	// mapping protocol so an overridden __delitem__ fires.
	//
	// CPython: Python/bytecodes.c DELETE_NAME (PyObject_DelItem on
	// locals)
	if d, ok := scope.(*objects.Dict); ok && scope.Type() == objects.DictType {
		return d.DelItem(key)
	}
	return objects.DelItem(scope, key)
}
