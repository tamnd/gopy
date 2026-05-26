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

// init wires the const-wrap hook so objects.wrapConstAttr (the path
// dis.py and friends take when they read co_consts) can convert
// compile-pipeline value types into Objects without dragging the
// compile package into objects/.
func init() {
	objects.ConstWrapHook = func(v any) (objects.Object, bool) {
		switch x := v.(type) {
		case *compile.Code:
			return liftNestedCode(x), true
		case *compile.ConstTuple:
			items := make([]objects.Object, len(x.Values))
			for i, raw := range x.Values {
				item, err := wrapConst(raw)
				if err != nil {
					return nil, false
				}
				items[i] = item
			}
			return objects.NewTuple(items), true
		case ast.EllipsisType:
			return objects.Ellipsis(), true
		}
		return nil, false
	}
}

// liftNestedCode mirrors pythonrun.liftCode for inner code objects
// reached through a parent's Consts slot. Nested defs / lambdas /
// class bodies all surface here.
func liftNestedCode(c *compile.Code) *objects.Code {
	if cached, ok := c.Lifted.(*objects.Code); ok && cached != nil {
		return cached
	}
	out := &objects.Code{
		Version:         objects.AllocCodeVersion(),
		Argcount:        c.Argcount,
		PosonlyArgcount: c.PosOnlyArgCount,
		KwonlyArgcount:  c.KwOnlyArgCount,
		Stacksize:       c.Stacksize,
		Flags:           int(c.Flags),
		Code:            c.Code,
		Consts:          liftConsts(c.Consts),
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
	specialize.Enable(out)
	c.Lifted = out
	return out
}

// liftConsts walks a Code.Consts slice and converts compile-pipeline
// value types into marshal-friendly forms: *compile.Code becomes
// *objects.Code (recursively lifted) and *compile.ConstTuple becomes
// []any (recursively lifted). Scalars pass through unchanged.
//
// Without this lift, nested code objects keep CPython's pre-lift type
// in the parent's Consts slot and marshal.Dump refuses them.
//
// CPython: Python/compile.c assemble_consts paths (each child code
// is already a PyObject* so this conversion is implicit there).
func liftConsts(consts []any) []any {
	out := make([]any, len(consts))
	for i, v := range consts {
		out[i] = liftConst(v)
	}
	return out
}

func liftConst(v any) any {
	switch x := v.(type) {
	case *compile.Code:
		return liftNestedCode(x)
	case *compile.ConstTuple:
		items := make([]any, len(x.Values))
		for i, raw := range x.Values {
			items[i] = liftConst(raw)
		}
		return items
	}
	return v
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
	case []any:
		// Tuple constants that have been pre-lifted by liftConsts: the
		// codegen-side *compile.ConstTuple was flattened to []any so
		// marshal can encode it, and LOAD_CONST now sees the bare slice.
		items := make([]objects.Object, len(x))
		for i, raw := range x {
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
// panel. Frame termination uses the errFrameReturn sentinel via
// e.retVal; arms set those fields and return errFrameReturn just like
// CPython's goto exit_frame.
//
//nolint:gocognit,gocyclo // hand-written opcode switch; the arm count shrinks as 1621 codegen replaces these.
func (e *evalState) trySimple(op compile.Opcode, oparg uint32) (next int, ok bool, err error) {
	switch op {
	case compile.CACHE, compile.RESERVED:
		// CACHE words are inline-specialization slots; the dispatcher
		// only sees them when fetch() runs past the end of an
		// instruction word, which the action translator does not do.
		// Treat them as NOP so a hand-rolled bytecode that includes
		// padding stays valid. RESERVED is the parity-pin opcode in
		// CPython's table; behavior matches NOP in unspecialized form.
		// NOP itself routes through dispatchGen (spec 1714 phase 5.2).
		return e.advance(), true, nil

	case compile.RESUME:
		next, rerr := e.handleResume(op, oparg)
		if rerr != nil {
			return 0, true, rerr
		}
		return next, true, nil

	case compile.JUMP_BACKWARD, compile.JUMP_BACKWARD_NO_INTERRUPT:
		// Backward jumps poll the eval breaker (CPython: CHECK_EVAL_BREAKER
		// fires here so signal handlers and pending calls can run mid-loop).
		// JUMP_BACKWARD_NO_INTERRUPT skips the poll. The gilTimer tick
		// rides along here too because backward branches are the
		// preemption points CPython arms the GIL drop request at.
		if op == compile.JUMP_BACKWARD {
			if e.gilTimer != nil {
				e.gilTimer.poll(e.gil, e.breaker)
			}
			if e.breaker != nil && e.breaker.Load() != 0 {
				if berr := e.handleEvalBreaker(); berr != nil {
					return 0, true, berr
				}
			}
		}
		target := e.jumpBy(-int(oparg) + 1)
		if op == compile.JUMP_BACKWARD && target >= 0 {
			e.tryWarmupTier2(target / 2)
		}
		return target, true, nil

	case compile.ENTER_EXECUTOR:
		next, eerr := e.enterExecutor(oparg)
		return next, true, eerr

	case compile.BINARY_OP:
		b := e.popObject()
		a := e.popObject()
		out, berr := binaryOp(int32(oparg), a, b)
		if berr != nil {
			return 0, true, berr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.BINARY_OP), true, nil

	case compile.COMPARE_OP:
		b := e.popObject()
		a := e.popObject()
		// CPython packs the CompareOp into the high 4 bits of oparg
		// and a "convert-to-bool" flag in bit 4. The low bits are
		// reserved for adaptive specialization.
		cmpOp := objects.CompareOp((oparg >> 5) & 0xf)
		out, cerr := objects.RichCmp(a, b, cmpOp)
		if cerr != nil {
			return 0, true, cerr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.COMPARE_OP), true, nil

	case compile.FOR_ITER:
		// Stack: [iter]. Pop iter, peek by re-pushing. CPython peeks
		// directly to avoid the dup but the FOR_ITER arm in 3.14 keeps
		// the iterator on the stack across iterations.
		it := e.peek(0).AsObject()
		if it == nil {
			return 0, true, fmt.Errorf("vm: TypeError: FOR_ITER on nil object")
		}
		t := it.Type()
		if t.IterNext == nil {
			return 0, true, fmt.Errorf("vm: TypeError: '%s' object is not an iterator", t.Name)
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
				return e.jumpBy(int(oparg) + 1), true, nil
			}
			return 0, true, nerr
		}
		e.pushObject(v)
		return e.cacheAdvance(compile.FOR_ITER), true, nil

	case compile.END_FOR:
		// END_FOR is a no-op for ordinary iterators in CPython 3.14:
		// the codegen pairs it with POP_ITER, and FOR_ITER's exhaustion
		// path leaves the iterator on the stack for that following pop.
		// Generator finalization is the only case where END_FOR has a
		// real effect, and gopy doesn't land that path yet.
		//
		// CPython: Python/bytecodes.c END_FOR
		return e.advance(), true, nil

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
			return 0, true, cerr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.CALL), true, nil

	case compile.UNPACK_SEQUENCE:
		seq := e.popObject()
		n := int(oparg)
		items, uerr := unpackSeq(seq, n)
		if uerr != nil {
			return 0, true, uerr
		}
		// Push in reverse so items[0] ends up at the top, matching
		// the assignment order CPython documents.
		for i := n - 1; i >= 0; i-- {
			e.pushObject(items[i])
		}
		return e.cacheAdvance(compile.UNPACK_SEQUENCE), true, nil

	case compile.STORE_SUBSCR:
		key := e.popObject()
		container := e.popObject()
		v := e.popObject()
		if serr := setItem(container, key, v); serr != nil {
			return 0, true, serr
		}
		return e.cacheAdvance(compile.STORE_SUBSCR), true, nil

	case compile.CONTAINS_OP:
		// Stack layout: [..., left, right]. CPython pops right (haystack)
		// then left (needle); oparg low bit toggles `not in`.
		haystack := e.popObject()
		needle := e.popObject()
		hit, cerr := containsItem(haystack, needle)
		if cerr != nil {
			return 0, true, cerr
		}
		if oparg&1 == 1 {
			hit = !hit
		}
		e.pushObject(objects.NewBool(hit))
		return e.cacheAdvance(compile.CONTAINS_OP), true, nil

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
				return 0, true, errors.New("RuntimeError: No active exception to re-raise")
			}
			exc, ok := handled.(*pyerrors.Exception)
			if !ok {
				return 0, true, errors.New("RuntimeError: No active exception to re-raise")
			}
			pyerrors.Raise(e.ts, exc)
			return 0, true, excSentinel(exc)
		case 1:
			val := e.popObject()
			exc := raiseValue(e.ts, val, nil)
			return 0, true, exc
		case 2:
			cause := e.popObject()
			val := e.popObject()
			exc := raiseValue(e.ts, val, cause)
			return 0, true, exc
		}
		return 0, true, fmt.Errorf("vm: RAISE_VARARGS: invalid oparg %d", oparg)

	case compile.CHECK_EXC_MATCH:
		// Stack: [exc, type]. Push True if isinstance(exc, type), else
		// False. v0.6 doesn't have exception class hierarchy, so use a
		// best-effort string compare.
		typeObj := e.popObject()
		exc := e.peek(0).AsObject()
		match := exceptionMatches(exc, typeObj)
		e.pushObject(objects.NewBool(match))
		return e.advance(), true, nil

	case compile.CONVERT_VALUE:
		v := e.popObject()
		out, cerr := convertValue(v, oparg)
		if cerr != nil {
			return 0, true, cerr
		}
		e.pushObject(out)
		return e.advance(), true, nil

	case compile.MAKE_FUNCTION:
		// TOS is a code object; build a Function bound to the current
		// frame's globals. Defaults, kwdefaults, annotations, and the
		// closure tuple are wired separately by SET_FUNCTION_ATTRIBUTE.
		//
		// CPython: Python/bytecodes.c MAKE_FUNCTION
		v := e.popObject()
		code, ok := v.(*objects.Code)
		if !ok {
			return 0, true, fmt.Errorf("MAKE_FUNCTION: TOS not a code object, got %T", v)
		}
		fn := objects.NewFunction(code.Name, code, e.f.Globals)
		// Stamp the cached co_version so CALL_PY_EXACT_ARGS can write a
		// stable _CHECK_FUNCTION_VERSION guard. Without this the call
		// specializer always sees Version==0 and falls back to the
		// generic CALL arm.
		//
		// CPython: Python/bytecodes.c:4956 _PyFunction_SetVersion
		fn.Version = code.Version
		e.pushObject(fn)
		return e.advance(), true, nil

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
			return 0, true, fmt.Errorf("SET_FUNCTION_ATTRIBUTE: TOS not a function, got %T", fnObj)
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
		case 0x04:
			// CPython: Python/bytecodes.c SET_FUNCTION_ATTRIBUTE 0x04
			fn.Annotate = attr
			fn.Annotations = nil
			// gh-137814: fix the qualname of the annotation function to
			// "enclosing_func.__qualname__ + .__annotate__" so that
			// f.__annotate__.__qualname__ == "f.__annotate__".
			//
			// CPython: Python/bytecodes.c:4975
			// SET_FUNCTION_ATTRIBUTE MAKE_FUNCTION_ANNOTATE branch
			if af, ok := attr.(*objects.Function); ok {
				af.Qualname = fn.Qualname + ".__annotate__"
				af.Annotations = nil
			}
		case 0x08:
			if t, ok := attr.(*objects.Tuple); ok {
				fn.Closure = t
			}
		}
		e.pushObject(fn)
		return e.advance(), true, nil

	case compile.LOAD_CLOSURE:
		// Same as LOAD_DEREF but pushes the cell itself, not its
		// contents. Used by MAKE_FUNCTION when constructing the
		// closure tuple. After cfgFixCellOffsets, oparg is the final
		// localsplus offset of the cell slot.
		//
		// CPython: Python/bytecodes.c:261 pseudo(LOAD_CLOSURE)
		ref := e.f.LocalsPlus[int(oparg)]
		e.push(ref.Dup())
		return e.advance(), true, nil

	case compile.LOAD_DEREF:
		// oparg is the localsplus offset of the cell, as rewritten by
		// fix_cell_offsets during prepare_localsplus.
		//
		// CPython: Python/bytecodes.c:1911 LOAD_DEREF
		ref := e.f.LocalsPlus[int(oparg)]
		cellObj := ref.AsObject()
		cell, ok := cellObj.(*objects.Cell)
		if !ok || cell == nil {
			name := derefName(e.f.Code, int(oparg))
			return 0, true, fmt.Errorf("LOAD_DEREF: %s slot %d not a cell in %s:%s, got %T", name, oparg, e.f.Code.Filename, e.f.Code.Name, cellObj)
		}
		if cell.Contents == nil {
			return 0, true, formatExcUnbound(e.f.Code, int(oparg))
		}
		e.pushObject(cell.Contents)
		return e.advance(), true, nil

	case compile.STORE_DEREF:
		// CPython: Python/bytecodes.c:1920 STORE_DEREF
		v := e.popObject()
		ref := e.f.LocalsPlus[int(oparg)]
		cellObj := ref.AsObject()
		cell, ok := cellObj.(*objects.Cell)
		if !ok {
			cell = objects.NewCell(nil)
			e.f.LocalsPlus[int(oparg)] = stackref.FromObject(cell)
		}
		cell.Contents = v
		return e.advance(), true, nil

	case compile.DELETE_DEREF:
		// CPython: Python/bytecodes.c:1875 DELETE_DEREF
		ref := e.f.LocalsPlus[int(oparg)]
		cell, ok := ref.AsObject().(*objects.Cell)
		if !ok || cell.Contents == nil {
			return 0, true, formatExcUnbound(e.f.Code, int(oparg))
		}
		cell.Contents = nil
		return e.advance(), true, nil

	case compile.COPY_FREE_VARS:
		// oparg = co_nfreevars. CPython computes the destination as
		// co_nlocalsplus - oparg so free vars always land at the end
		// of the compacted localsplus table, regardless of how many
		// arg-cells fix_cell_offsets merged into the locals span.
		//
		// CPython: Python/bytecodes.c:1925 COPY_FREE_VARS
		n := int(oparg)
		fn, ok := e.f.Func.(*objects.Function)
		if !ok || fn.Closure == nil {
			return 0, true, fmt.Errorf("COPY_FREE_VARS: frame has no closure")
		}
		dst := frame.NLocalsPlusOf(e.f.Code) - n
		for i := 0; i < n; i++ {
			cell := fn.Closure.Item(i)
			e.f.LocalsPlus[dst+i] = stackref.FromObject(cell)
		}
		return e.advance(), true, nil

	case compile.LIST_EXTEND:
		// Pops iter, extends list at depth oparg with all its items.
		v := e.popObject()
		l, ok := e.peek(int(oparg) - 1).AsObject().(*objects.List)
		if !ok {
			return 0, true, fmt.Errorf("LIST_EXTEND: target not a list")
		}
		items, eerr := iterToSlice(v)
		if eerr != nil {
			return 0, true, eerr
		}
		for _, it := range items {
			l.Append(it)
		}
		return e.advance(), true, nil

	case compile.DICT_MERGE:
		// Pop the source mapping and merge into the kwargs dict at
		// depth oparg. The callable sits three slots below the dict
		// (NULL + args tuple between), and is used to dress the error
		// with the function's qualified name. Mirrors CPython's
		// DICT_MERGE + _PyEval_FormatKwargsError pair.
		//
		// CPython: Python/bytecodes.c:2122 DICT_MERGE
		// CPython: Python/ceval.c:3410 _PyEval_FormatKwargsError
		src := e.popObject()
		d, ok := e.peek(int(oparg) - 1).AsObject().(*objects.Dict)
		if !ok {
			return 0, true, fmt.Errorf("DICT_MERGE: target not a dict")
		}
		callable := e.peek(int(oparg) + 2).AsObject()
		if merr := dictMergeKwargs(d, src); merr != nil {
			return 0, true, formatKwargsError(callable, merr)
		}
		return e.advance(), true, nil

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
				return 0, true, aerr
			}
		}
		e.pushObject(s)
		return e.advance(), true, nil

	case compile.CALL_INTRINSIC_1:
		v := e.popObject()
		if int(oparg) >= len(intrinsicsUnary) {
			return 0, true, fmt.Errorf("CALL_INTRINSIC_1: oparg %d out of range", oparg)
		}
		// IMPORT_STAR needs the current frame's locals, which the generic
		// intrinsic signature doesn't carry. Route it directly.
		if oparg == intrinsics.UnaryImportStarID {
			if ierr := e.importStar(v); ierr != nil {
				return 0, true, ierr
			}
			e.pushObject(objects.None())
			return e.advance(), true, nil
		}
		fn := intrinsicsUnary[oparg]
		if fn == nil {
			return 0, true, fmt.Errorf("CALL_INTRINSIC_1: id %d unbound", oparg)
		}
		out, cerr := fn(e.ts, v)
		if cerr != nil {
			return 0, true, cerr
		}
		e.pushObject(out)
		return e.advance(), true, nil

	case compile.CALL_INTRINSIC_2:
		rhs := e.popObject()
		lhs := e.popObject()
		if int(oparg) >= len(intrinsicsBinary) {
			return 0, true, fmt.Errorf("CALL_INTRINSIC_2: oparg %d out of range", oparg)
		}
		fn := intrinsicsBinary[oparg]
		if fn == nil {
			return 0, true, fmt.Errorf("CALL_INTRINSIC_2: id %d unbound", oparg)
		}
		out, cerr := fn(e.ts, lhs, rhs)
		if cerr != nil {
			return 0, true, cerr
		}
		e.pushObject(out)
		return e.advance(), true, nil

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
			return 0, true, fmt.Errorf("vm: RERAISE: invalid oparg %d", oparg)
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
			return 0, true, excSentinel(pyExc)
		}
		return 0, true, fmt.Errorf("%s", objectRepr(exc))

	case compile.UNPACK_EX:
		// oparg low byte: items before *rest. high byte: items after.
		// Stack pre: [seq]. Stack post: [item_after_n], ..., [rest_list],
		// ..., [item_before_0]. v0.6 surfaces only the simple case.
		before := int(oparg & 0xFF)
		after := int(oparg >> 8)
		seq := e.popObject()
		items, ierr := iterToSlice(seq)
		if ierr != nil {
			t := seq.Type()
			if t.Iter == nil && (t.Sequence == nil || t.Sequence.GetItem == nil) {
				return 0, true, fmt.Errorf("TypeError: cannot unpack non-iterable %s object", t.Name)
			}
			return 0, true, ierr
		}
		if len(items) < before+after {
			return 0, true, fmt.Errorf("ValueError: not enough values to unpack (expected at least %d, got %d)", before+after, len(items))
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
		return e.advance(), true, nil

	case compile.CALL_KW:
		// Stack: [callable, NULL_or_self, arg0, ..., argN, kwnames_tuple].
		// oparg is total positional+keyword count; kwnames tuple length is
		// the keyword count.
		//
		// CPython: Python/bytecodes.c CALL_KW
		kwnamesObj := e.popObject()
		kwnames, ok := kwnamesObj.(*objects.Tuple)
		if !ok {
			return 0, true, fmt.Errorf("CALL_KW: kwnames not a tuple")
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
			return 0, true, cerr
		}
		e.pushObject(out)
		return e.cacheAdvance(compile.CALL_KW), true, nil

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
				return 0, true, fmt.Errorf("CALL_FUNCTION_EX: kwargs not a dict")
			}
			kwargs = d
		}
		argsObj := e.popObject()
		argsSlice, ierr := iterToSlice(argsObj)
		if ierr != nil {
			return 0, true, ierr
		}
		_ = e.popObject() // NULL_or_self placeholder
		callable := e.popObject()
		out, cerr := objects.Call(callable, objects.NewTuple(argsSlice), kwargs)
		if cerr != nil {
			return 0, true, cerr
		}
		e.pushObject(out)
		return e.advance(), true, nil

	case compile.BINARY_SLICE:
		// Stack: [container, start, stop]. Push container[start:stop].
		stop := e.popObject()
		start := e.popObject()
		container := e.popObject()
		out, serr := sliceContainer(container, start, stop)
		if serr != nil {
			return 0, true, serr
		}
		e.pushObject(out)
		return e.advance(), true, nil

	case compile.STORE_SLICE:
		// Stack: [value, container, start, stop]. Replace container[start:stop] with value.
		stop := e.popObject()
		start := e.popObject()
		container := e.popObject()
		value := e.popObject()
		if serr := storeSlice(container, start, stop, value); serr != nil {
			return 0, true, serr
		}
		return e.advance(), true, nil

	case compile.LOAD_FAST_LOAD_FAST, compile.LOAD_FAST_BORROW_LOAD_FAST_BORROW:
		// Two local indexes packed: high nibble first, low nibble second.
		// The BORROW variant is identical under Go GC.
		hi := int(oparg >> 4)
		lo := int(oparg & 0xF)
		r1 := e.localAt(hi)
		r2 := e.localAt(lo)
		if r1.IsNull() || r2.IsNull() {
			return 0, true, fmt.Errorf("LOAD_FAST_LOAD_FAST: unbound local")
		}
		e.push(r1.Dup())
		e.push(r2.Dup())
		return e.advance(), true, nil

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
			return 0, true, fmt.Errorf("STORE_FAST_LOAD_FAST: local %d unbound", lo)
		}
		e.push(r2.Dup())
		return e.advance(), true, nil

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
		return e.advance(), true, nil

	case compile.TO_BOOL:
		// Replace TOS with bool(TOS).
		v := e.popObject()
		truthy, terr := objects.IsTruthy(v)
		if terr != nil {
			return 0, true, terr
		}
		e.pushObject(objects.NewBool(truthy))
		return e.cacheAdvance(compile.TO_BOOL), true, nil

	case compile.NOT_TAKEN:
		// 3.14 marker that flags the not-taken branch of a conditional
		// jump. Has no runtime effect; CPython uses it for monitoring.
		return e.advance(), true, nil

	case compile.POP_BLOCK:
		// 3.14 keeps POP_BLOCK as a pseudo for old codegen paths. No
		// block stack in our frame model, so it's a no-op.
		return e.advance(), true, nil

	case compile.POP_ITER:
		// Pop the exhausted iterator left on the stack by FOR_ITER's
		// fall-through. CPython 3.14 made this an opcode of its own.
		ref := e.pop()
		ref.Close()
		return e.advance(), true, nil

	case compile.JUMP, compile.JUMP_NO_INTERRUPT:
		// Unconditional pseudo-jumps emitted by 3.14 codegen for
		// optimized forms. We treat them as forward jumps; the
		// NO_INTERRUPT variant skips the eval breaker poll.
		return e.jumpBy(int(oparg) + 1), true, nil

	case compile.EXIT_INIT_CHECK:
		// `__init__` must return None. Pop the return value and raise
		// TypeError if it's something else.
		// CPython: Python/bytecodes.c EXIT_INIT_CHECK
		v := e.popObject()
		if v != objects.None() {
			return 0, true, fmt.Errorf("TypeError: __init__() should return None, not '%s'", v.Type().Name)
		}
		return e.advance(), true, nil

	case compile.LOAD_FROM_DICT_OR_DEREF:
		// Look up co_localsplusnames[oparg] in the mapping at TOS first
		// (the class namespace at class-body load time, the type-params
		// dict for PEP 695); if absent, fall back to the cell at the
		// same localsplus slot. This is what makes a class-body
		// `locals()["x"] = 43` override an enclosing closure x.
		//
		// CPython: Python/bytecodes.c:1887 LOAD_FROM_DICT_OR_DEREF
		classDict := e.popObject()
		co := e.f.Code
		idx := int(oparg)
		if idx < 0 || idx >= len(co.LocalsplusNames) {
			return 0, true, fmt.Errorf("SystemError: LOAD_FROM_DICT_OR_DEREF oparg %d out of range", idx)
		}
		name := objects.NewStr(co.LocalsplusNames[idx])
		if v, gerr := objects.GetItem(classDict, name); gerr == nil && v != nil {
			e.pushObject(v)
			return e.advance(), true, nil
		}
		ref := e.f.LocalsPlus[idx]
		cell, ok := ref.AsObject().(*objects.Cell)
		if !ok || cell.Contents == nil {
			return 0, true, formatExcUnbound(co, idx)
		}
		e.pushObject(cell.Contents)
		return e.advance(), true, nil

	case compile.LOAD_NAME, compile.LOAD_GLOBAL, compile.STORE_NAME,
		compile.DELETE_NAME:
		v, perr := e.execNameOp(op, oparg)
		if perr != nil {
			return 0, true, perr
		}
		_ = v
		return e.cacheAdvance(op), true, nil

	case compile.LOAD_ATTR:
		if perr := e.execLoadAttr(oparg); perr != nil {
			return 0, true, perr
		}
		return e.cacheAdvance(compile.LOAD_ATTR), true, nil

	case compile.STORE_ATTR:
		if perr := e.execStoreAttr(oparg); perr != nil {
			return 0, true, perr
		}
		return e.cacheAdvance(compile.STORE_ATTR), true, nil

	case compile.DELETE_ATTR:
		if perr := e.execDeleteAttr(oparg); perr != nil {
			return 0, true, perr
		}
		return e.advance(), true, nil

	case compile.LOAD_SUPER_ATTR:
		if perr := e.execLoadSuperAttr(oparg); perr != nil {
			return 0, true, perr
		}
		return e.cacheAdvance(compile.LOAD_SUPER_ATTR), true, nil
	}
	return 0, false, nil
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
	name := co.NameObj(idx)
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
	name := co.NameObj(idx)
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
	name := co.NameObj(idx)
	return objects.DelAttr(owner, name)
}

// execLoadSuperAttr implements the generic _LOAD_SUPER_ATTR uop. The
// bytecode pushed (global_super, class, self); we pop the trio, call
// global_super(class, self) (or super(class) when bit 1 of oparg is
// clear) to mint a super proxy, then look up co.Names[oparg>>2] on
// it. The trailing _PUSH_NULL_CONDITIONAL contributes the NULL self
// slot when bit 0 of oparg is set so a following CALL sees the
// (callable, NULL) shape.
//
// CPython: Python/bytecodes.c:2172 _LOAD_SUPER_ATTR
func (e *evalState) execLoadSuperAttr(oparg uint32) error {
	co := e.f.Code
	nameIdx := int(oparg >> 2)
	if nameIdx < 0 || nameIdx >= len(co.Names) {
		return fmt.Errorf("vm: LOAD_SUPER_ATTR: name index %d out of range", nameIdx)
	}
	self := e.popObject()
	cls := e.popObject()
	globalSuper := e.popObject()
	var argv []objects.Object
	if oparg&2 != 0 {
		argv = []objects.Object{cls, self}
	} else {
		argv = []objects.Object{cls}
	}
	su, err := objects.Call(globalSuper, objects.NewTuple(argv), nil)
	if err != nil {
		return err
	}
	name := co.NameObj(nameIdx)
	attr, err := objects.GetAttr(su, name)
	if err != nil {
		return err
	}
	e.pushObject(attr)
	if oparg&1 != 0 {
		e.push(stackref.Null)
	}
	return nil
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
	keyObj := co.NameObj(idx)

	switch op {
	case compile.LOAD_NAME:
		if v, ok := lookupIn(e.f.Locals, keyObj); ok {
			e.pushObject(v)
			return v, nil
		}
		fallthrough
	case compile.LOAD_GLOBAL:
		// Stack effect (CPython 3.14, bytecodes.c:1769):
		//     ( -- res, null if oparg & 1 )
		// res is pushed first (deeper slot), the NULL marker on top.
		// That matches the codegen pair LOAD_GLOBAL + PUSH_NULL: the
		// callable lands below, NULL above, so an insert_superinstructions
		// fold to LOAD_GLOBAL with bit 0 set is a no-op for the stack.
		var v objects.Object
		if w, ok := lookupIn(e.f.Globals, keyObj); ok {
			v = w
		} else if w, ok := lookupIn(e.f.Builtins, keyObj); ok {
			v = w
		} else {
			return nil, fmt.Errorf("vm: NameError: name '%s' is not defined", name)
		}
		e.pushObject(v)
		if pushNull {
			e.push(stackref.Null)
		}
		return v, nil

	case compile.STORE_NAME:
		v := e.popObject()
		dst := e.f.Locals
		if dst == nil {
			if uint32(e.f.Code.Flags)&compile.CoOptimized != 0 {
				// TypeParametersBlock and similar synthetic optimized scopes
				// emit STORE_NAME for TypeVar names but have no Locals dict
				// yet. Create one on demand so the name lives in the
				// function's local scope and does not escape to globals.
				// CPython: Python/frameobject.c:306 _PyFrameGetLocals (lazy)
				ns := objects.NewDict()
				e.f.Locals = ns
				dst = ns
			} else {
				dst = e.f.Globals
			}
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
		nbAdd            = 0
		nbAnd            = 1
		nbFloorDivide    = 2
		nbLshift         = 3
		nbMatrixMultiply = 4
		nbMult           = 5
		nbRemainder      = 6
		nbOr             = 7
		nbPower          = 8
		nbRshift         = 9
		nbSubtract       = 10
		nbTrueDivide     = 11
		nbXor            = 12
		// Inplace forms (13..25) re-use the non-inplace slot for
		// immutable types; the mapping mirrors CPython's NB_INPLACE_*
		// alphabetical numbering.
		nbInplaceAdd            = 13
		nbInplaceAnd            = 14
		nbInplaceFloorDivide    = 15
		nbInplaceLshift         = 16
		nbInplaceMatrixMultiply = 17
		nbInplaceMult           = 18
		nbInplaceRemainder      = 19
		nbInplaceOr             = 20
		nbInplacePower          = 21
		nbInplaceRshift         = 22
		nbInplaceSubtract       = 23
		nbInplaceTrueDivide     = 24
		nbInplaceXor            = 25
		nbSubscr                = 26
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
	case nbMatrixMultiply:
		return objects.NumberMatrixMultiply(a, b)
	case nbInplaceMatrixMultiply:
		return objects.NumberInPlaceMatrixMultiply(a, b)
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
	// CPython: Objects/typeobject.c:8195 SLOT1BIN (__pow__ / __rpow__)
	if out, ok, err := objects.DunderBinary(a, b, "**"); ok {
		return out, err
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
	// __dunder__ / __rdunder__ fallback so Python classes that define
	// __add__ / __mul__ / __truediv__ etc. (Decimal, Fraction, ...) get
	// dispatched. Lands until the slot-wrapper port wires update_one_slot.
	//
	// CPython: Objects/typeobject.c:8195 SLOT1BIN
	if out, ok, err := objects.DunderBinary(a, b, sym); ok {
		return out, err
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
		// KnownHash short-circuit: the LOAD_NAME / LOAD_GLOBAL key
		// is a *Unicode pulled from co.NameObj, whose hash is cached
		// after the first dispatch on this code object. Threading the
		// cached hash straight through avoids the PyObject_Hash vtable
		// dispatch (one virtual call) per lookup.
		//
		// CPython: Objects/dictobject.c:1965 _PyDict_GetItem_KnownHash
		if u, ok := key.(*objects.Unicode); ok {
			v, err := d.GetItemKnownHash(u, u.HashCached())
			if err != nil {
				return nil, false
			}
			return v, true
		}
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
		if u, ok := key.(*objects.Unicode); ok {
			// CPython: Objects/dictobject.c:2069 _PyDict_SetItem_KnownHash
			return d.SetItemKnownHash(u, value, u.HashCached())
		}
		return d.SetItem(key, value)
	}
	return objects.SetItem(scope, key, value)
}

// unpackSeq unpacks seq into exactly n items, mirroring CPython's
// UNPACK_SEQUENCE error texts on length mismatch.
//
// CPython: Python/bytecodes.c UNPACK_SEQUENCE
func unpackSeq(seq objects.Object, n int) ([]objects.Object, error) {
	t := seq.Type()
	it, ierr := objects.Iter(seq)
	if ierr != nil {
		if t.Iter == nil && (t.Sequence == nil || t.Sequence.GetItem == nil) {
			return nil, fmt.Errorf("TypeError: cannot unpack non-iterable %s object", t.Name)
		}
		return nil, ierr
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not an iterator", itType.Name)
	}
	out := make([]objects.Object, 0, n)
	for i := 0; i < n; i++ {
		v, nerr := itType.IterNext(it)
		if errors.Is(nerr, objects.ErrStopIteration) {
			return nil, fmt.Errorf("ValueError: not enough values to unpack (expected %d, got %d)", n, len(out))
		}
		if nerr != nil {
			return nil, nerr
		}
		out = append(out, v)
	}
	extra, nerr := itType.IterNext(it)
	if nerr != nil && !errors.Is(nerr, objects.ErrStopIteration) {
		return nil, nerr
	}
	if nerr == nil && extra != nil {
		if ll, ok := exactBuiltinLen(seq); ok && ll > n {
			return nil, fmt.Errorf("ValueError: too many values to unpack (expected %d, got %d)", n, ll)
		}
		return nil, fmt.Errorf("ValueError: too many values to unpack (expected %d)", n)
	}
	return out, nil
}

// exactBuiltinLen reports the length of v when v is an exact builtin
// list, tuple, or dict (not a subclass). Mirrors CPython's
// PyList_CheckExact/PyTuple_CheckExact/PyDict_CheckExact branch in
// _PyEval_UnpackIterableStackRef.
//
// CPython: Python/ceval.c:2443 _PyEval_UnpackIterableStackRef
func exactBuiltinLen(v objects.Object) (int, bool) {
	switch x := v.(type) {
	case *objects.Tuple:
		if v.Type() == objects.TupleType {
			return x.Len(), true
		}
	case *objects.List:
		if v.Type() == objects.ListType {
			return x.Len(), true
		}
	case *objects.Dict:
		if v.Type() == objects.DictType {
			return x.Len(), true
		}
	}
	return 0, false
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

// derefName returns the localsplus name at idx, post fix_cell_offsets
// compaction. After C.1, every deref oparg is a final localsplus
// offset, so LocalsplusNames is the canonical source. Falls back to
// the legacy cell/free walk when LocalsplusNames is empty (test
// fixtures only).
//
// CPython: Objects/codeobject.c:423 get_localsplus_names
func derefName(co *objects.Code, idx int) string {
	if idx >= 0 && idx < len(co.LocalsplusNames) {
		return co.LocalsplusNames[idx]
	}
	if idx < len(co.Cellvars) {
		return co.Cellvars[idx]
	}
	if i := idx - len(co.Cellvars); i >= 0 && i < len(co.Freevars) {
		return co.Freevars[i]
	}
	return "<unknown>"
}

// formatExcUnbound mirrors CPython's _PyEval_FormatExcUnbound: an
// empty cell at a localsplus slot below the first freevar raises
// UnboundLocalError; at or above that boundary it raises NameError
// with the "in enclosing scope" suffix. The boundary is
// nlocalsplus - nfreevars (PyUnstable_Code_GetFirstFree).
//
// CPython: Python/ceval.c:3482 _PyEval_FormatExcUnbound
func formatExcUnbound(co *objects.Code, idx int) error {
	name := derefName(co, idx)
	firstFree := frame.NLocalsPlusOf(co) - len(co.Freevars)
	if idx < firstFree {
		return fmt.Errorf("UnboundLocalError: cannot access local variable '%s' where it is not associated with a value", name)
	}
	return fmt.Errorf("NameError: cannot access free variable '%s' where it is not associated with a value in enclosing scope", name)
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
