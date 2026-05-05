// Hand-written arms for the smallest core opcodes: stack hygiene,
// constant/fast load, name lookup. The codegen pipeline (1621) will
// eventually replace these with generated arms in opcodes_gen.go.
// Until then, this file is the source of truth for these ops so that
// trivial codes (RESUME / LOAD_CONST / RETURN_VALUE) actually run.
//
// CPython: Python/bytecodes.c per-op definitions

package vm

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

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
	case objects.Object:
		return x, nil
	}
	return nil, fmt.Errorf("vm: unsupported const type %T", v)
}

// trySimple is consulted by dispatch before falling back to
// notImplemented. Returns ok=false if op isn't in the hand-written
// panel. The retDone/err return shape matches dispatch.
//
//nolint:gocognit,gocyclo,gocritic,unparam // hand-written opcode switch; the wide return tuple matches dispatch's contract and the arm count shrinks as 1621 codegen replaces these.
func (e *evalState) trySimple(op compile.Opcode, oparg uint32) (next int, retVal objects.Object, retErr error, retDone, ok bool, err error) {
	switch op {
	case compile.NOP:
		return e.advance(), nil, nil, false, true, nil

	case compile.RESUME:
		// RESUME is the eval-breaker poll point. The breaker check
		// at the top of run() already runs every loop tick, so we
		// just clear any breaker bits the dispatcher knows how to
		// drain and advance.
		if e.breaker != nil && e.breaker.Load()&gil.BreakerEventsMask != 0 {
			if berr := e.handleEvalBreaker(); berr != nil {
				return 0, nil, nil, false, true, berr
			}
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.POP_TOP:
		ref := e.pop()
		ref.Close()
		return e.advance(), nil, nil, false, true, nil

	case compile.PUSH_NULL:
		e.push(stackref.Null)
		return e.advance(), nil, nil, false, true, nil

	case compile.COPY:
		// COPY i pushes a duplicate of stack[-i]. oparg=1 means
		// duplicate the top.
		if oparg < 1 {
			return 0, nil, nil, false, true, errors.New("vm: COPY oparg must be >= 1")
		}
		ref := e.peek(int(oparg) - 1)
		e.push(ref.Dup())
		return e.advance(), nil, nil, false, true, nil

	case compile.SWAP:
		// SWAP i swaps the top with stack[-i]. oparg=2 swaps top two.
		if oparg < 2 {
			return 0, nil, nil, false, true, errors.New("vm: SWAP oparg must be >= 2")
		}
		top := e.f.StackTop - 1
		other := e.f.StackTop - int(oparg)
		nlp := frame.NLocalsPlusOf(e.f.Code)
		e.f.LocalsPlus[nlp+top], e.f.LocalsPlus[nlp+other] = e.f.LocalsPlus[nlp+other], e.f.LocalsPlus[nlp+top]
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_CONST:
		co := e.f.Code
		if int(oparg) >= len(co.Consts) {
			return 0, nil, nil, false, true, fmt.Errorf("vm: LOAD_CONST index %d out of range", oparg)
		}
		obj, werr := wrapConst(co.Consts[oparg])
		if werr != nil {
			return 0, nil, nil, false, true, werr
		}
		e.pushObject(obj)
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_FAST:
		ref := e.localAt(int(oparg))
		if ref.IsNull() {
			return 0, nil, nil, false, true, fmt.Errorf("vm: LOAD_FAST: local %d unbound", oparg)
		}
		e.push(ref.Dup())
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_FAST_CHECK:
		ref := e.localAt(int(oparg))
		if ref.IsNull() {
			return 0, nil, nil, false, true, fmt.Errorf("vm: LOAD_FAST_CHECK: local %d unbound", oparg)
		}
		e.push(ref.Dup())
		return e.advance(), nil, nil, false, true, nil

	case compile.LOAD_FAST_AND_CLEAR:
		ref := e.localAt(int(oparg))
		e.setLocal(int(oparg), stackref.Null)
		e.push(ref)
		return e.advance(), nil, nil, false, true, nil

	case compile.STORE_FAST:
		ref := e.pop()
		old := e.localAt(int(oparg))
		old.Close()
		e.setLocal(int(oparg), ref)
		return e.advance(), nil, nil, false, true, nil

	case compile.DELETE_FAST:
		old := e.localAt(int(oparg))
		if old.IsNull() {
			return 0, nil, nil, false, true, fmt.Errorf("vm: DELETE_FAST: local %d unbound", oparg)
		}
		old.Close()
		e.setLocal(int(oparg), stackref.Null)
		return e.advance(), nil, nil, false, true, nil

	case compile.RETURN_VALUE:
		v := e.popObject()
		return 0, v, nil, true, true, nil

	case compile.JUMP_FORWARD:
		return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
	case compile.JUMP_BACKWARD, compile.JUMP_BACKWARD_NO_INTERRUPT:
		// Backward jumps poll the eval breaker (CPython: CHECK_EVAL_BREAKER
		// fires here so signal handlers and pending calls can run mid-loop).
		// JUMP_BACKWARD_NO_INTERRUPT skips the poll.
		if op == compile.JUMP_BACKWARD && e.breaker != nil && e.breaker.Load() != 0 {
			if berr := e.handleEvalBreaker(); berr != nil {
				return 0, nil, nil, false, true, berr
			}
		}
		return e.jumpBy(-int(oparg) + 1), nil, nil, false, true, nil

	case compile.BINARY_OP:
		b := e.popObject()
		a := e.popObject()
		out, berr := binaryOp(int32(oparg), a, b)
		if berr != nil {
			return 0, nil, nil, false, true, berr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

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
		return e.advance(), nil, nil, false, true, nil

	case compile.BUILD_LIST:
		n := int(oparg)
		items := make([]objects.Object, n)
		for i := n - 1; i >= 0; i-- {
			items[i] = e.popObject()
		}
		e.pushObject(objects.NewList(items))
		return e.advance(), nil, nil, false, true, nil

	case compile.BUILD_TUPLE:
		n := int(oparg)
		items := make([]objects.Object, n)
		for i := n - 1; i >= 0; i-- {
			items[i] = e.popObject()
		}
		e.pushObject(objects.NewTuple(items))
		return e.advance(), nil, nil, false, true, nil

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

	case compile.IS_OP:
		b := e.popObject()
		a := e.popObject()
		eq := (a == b)
		if oparg == 1 {
			eq = !eq
		}
		e.pushObject(objects.NewBool(eq))
		return e.advance(), nil, nil, false, true, nil

	case compile.POP_JUMP_IF_TRUE, compile.POP_JUMP_IF_FALSE,
		compile.POP_JUMP_IF_NONE, compile.POP_JUMP_IF_NOT_NONE:
		v := e.popObject()
		var take bool
		switch op {
		case compile.POP_JUMP_IF_NONE:
			take = (v == objects.None())
		case compile.POP_JUMP_IF_NOT_NONE:
			take = (v != objects.None())
		default:
			truthy, terr := objects.IsTruthy(v)
			if terr != nil {
				return 0, nil, nil, false, true, terr
			}
			if op == compile.POP_JUMP_IF_TRUE {
				take = truthy
			} else {
				take = !truthy
			}
		}
		if take {
			return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.GET_ITER:
		obj := e.popObject()
		t := obj.Type()
		if t.Iter == nil {
			return 0, nil, nil, false, true, fmt.Errorf("vm: TypeError: '%s' object is not iterable", t.Name)
		}
		it, ierr := t.Iter(obj)
		if ierr != nil {
			return 0, nil, nil, false, true, ierr
		}
		e.pushObject(it)
		return e.advance(), nil, nil, false, true, nil

	case compile.FOR_ITER:
		// Stack: [iter]. Pop iter, peek by re-pushing. CPython peeks
		// directly to avoid the dup but the FOR_ITER arm in 3.14 keeps
		// the iterator on the stack across iterations.
		it := e.peek(0).AsObject()
		t := it.Type()
		if t.IterNext == nil {
			return 0, nil, nil, false, true, fmt.Errorf("vm: TypeError: '%s' object is not an iterator", t.Name)
		}
		v, nerr := t.IterNext(it)
		if nerr != nil {
			if errors.Is(nerr, objects.ErrStopIteration) {
				// Skip past the loop body; oparg is the jump distance to
				// END_FOR (in code units, not instruction words).
				return e.jumpBy(int(oparg) + 1), nil, nil, false, true, nil
			}
			return 0, nil, nil, false, true, nerr
		}
		e.pushObject(v)
		return e.advance(), nil, nil, false, true, nil

	case compile.END_FOR:
		// END_FOR pops the exhausted iterator left by FOR_ITER's
		// fallthrough path.
		ref := e.pop()
		ref.Close()
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
		out, cerr := objects.Call(callable, args, nil)
		if cerr != nil {
			return 0, nil, nil, false, true, cerr
		}
		e.pushObject(out)
		return e.advance(), nil, nil, false, true, nil

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
		return e.advance(), nil, nil, false, true, nil

	case compile.BUILD_SLICE:
		// oparg is 2 (start:stop) or 3 (start:stop:step).
		var step objects.Object
		if oparg == 3 {
			step = e.popObject()
		}
		stop := e.popObject()
		start := e.popObject()
		e.pushObject(objects.NewSlice(start, stop, step))
		return e.advance(), nil, nil, false, true, nil

	case compile.STORE_SUBSCR:
		key := e.popObject()
		container := e.popObject()
		v := e.popObject()
		if serr := setItem(container, key, v); serr != nil {
			return 0, nil, nil, false, true, serr
		}
		return e.advance(), nil, nil, false, true, nil

	case compile.RAISE_VARARGS:
		// oparg: 0 = re-raise, 1 = raise exc, 2 = raise exc from cause.
		// v0.6 has no exception object hierarchy yet, so we surface the
		// raised value as a Go error with its repr; the eval loop's
		// exception table walker handles the unwind.
		switch oparg {
		case 0:
			return 0, nil, nil, false, true, errors.New("RuntimeError: No active exception to re-raise")
		case 1:
			exc := e.popObject()
			return 0, nil, nil, false, true, fmt.Errorf("%s", objectRepr(exc))
		case 2:
			cause := e.popObject()
			exc := e.popObject()
			return 0, nil, nil, false, true, fmt.Errorf("%s (from %s)", objectRepr(exc), objectRepr(cause))
		}
		return 0, nil, nil, false, true, fmt.Errorf("vm: RAISE_VARARGS: invalid oparg %d", oparg)

	case compile.PUSH_EXC_INFO:
		// Pushes the current exception under the value just pushed.
		// v0.6 has no per-frame exc info stack yet, so push a None
		// placeholder under the top.
		top := e.pop()
		e.pushObject(objects.None())
		e.push(top)
		return e.advance(), nil, nil, false, true, nil

	case compile.POP_EXCEPT:
		ref := e.pop()
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
		// frame's globals. v0.6 ignores defaults / closure flags.
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

	case compile.LOAD_NAME, compile.LOAD_GLOBAL, compile.STORE_NAME,
		compile.STORE_GLOBAL, compile.DELETE_NAME, compile.DELETE_GLOBAL:
		v, perr := e.execNameOp(op, oparg)
		if perr != nil {
			return 0, nil, nil, false, true, perr
		}
		_ = v
		return e.advance(), nil, nil, false, true, nil
	}
	return 0, nil, nil, false, false, nil
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
		if v, ok, err := lookupIn(e.f.Locals, keyObj); err != nil {
			return nil, err
		} else if ok {
			e.pushObject(v)
			return v, nil
		}
		fallthrough
	case compile.LOAD_GLOBAL:
		if pushNull {
			e.push(stackref.Null)
		}
		if v, ok, err := lookupIn(e.f.Globals, keyObj); err != nil {
			return nil, err
		} else if ok {
			e.pushObject(v)
			return v, nil
		}
		if v, ok, err := lookupIn(e.f.Builtins, keyObj); err != nil {
			return nil, err
		} else if ok {
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
// constants the compiler emits in compile/codegen_expr_op.go. Only the
// arithmetic forms tied to slots in NumberMethods are handled in v0.6;
// the rest return an error until the abstract layer fills in.
//
// CPython: Python/bytecodes.c BINARY_OP_GENERIC
func binaryOp(sub int32, a, b objects.Object) (objects.Object, error) {
	const (
		nbAdd      = 0
		nbMult     = 5
		nbSubtract = 10
	)
	switch sub {
	case nbAdd:
		return numericForward(a, b, "+", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Add
		})
	case nbSubtract:
		return numericForward(a, b, "-", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Subtract
		})
	case nbMult:
		return numericForward(a, b, "*", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
			return n.Multiply
		})
	}
	return nil, fmt.Errorf("vm: BINARY_OP suboperator %d not implemented in v0.6", sub)
}

func numericForward(a, b objects.Object, sym string, pick func(*objects.NumberMethods) func(a, b objects.Object) (objects.Object, error)) (objects.Object, error) {
	if n := a.Type().Number; n != nil {
		if fn := pick(n); fn != nil {
			out, err := fn(a, b)
			if err == nil {
				return out, nil
			}
		}
	}
	if n := b.Type().Number; n != nil {
		if fn := pick(n); fn != nil {
			out, err := fn(a, b)
			if err == nil {
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("vm: unsupported operand types for %s: %s and %s", sym, a.Type().Name, b.Type().Name)
}

func lookupIn(scope objects.Object, key objects.Object) (objects.Object, bool, error) {
	if scope == nil {
		return nil, false, nil
	}
	if d, ok := scope.(*objects.Dict); ok {
		v, err := d.GetItem(key)
		if err != nil {
			//nolint:nilerr // a missing key is the not-found signal for name lookup
			return nil, false, nil
		}
		return v, true, nil
	}
	return nil, false, fmt.Errorf("vm: name lookup against unsupported scope type %T", scope)
}

func storeIn(scope objects.Object, key, value objects.Object) error {
	if scope == nil {
		return fmt.Errorf("vm: cannot store name: scope is nil")
	}
	if d, ok := scope.(*objects.Dict); ok {
		return d.SetItem(key, value)
	}
	return fmt.Errorf("vm: store against unsupported scope type %T", scope)
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

// setItem mirrors PyObject_SetItem against the v0.6 container surface.
//
// CPython: Objects/abstract.c PyObject_SetItem
func setItem(container, key, value objects.Object) error {
	if d, ok := container.(*objects.Dict); ok {
		return d.SetItem(key, value)
	}
	if l, ok := container.(*objects.List); ok {
		idx, ok := key.(*objects.Int)
		if !ok {
			return fmt.Errorf("TypeError: list indices must be integers, not %s", key.Type().Name)
		}
		i, _ := idx.Int64()
		if i < 0 || int(i) >= l.Len() {
			return errors.New("IndexError: list assignment index out of range")
		}
		// objects.List doesn't expose a SetItem; route through SequenceMethods.
		if l.Type().Sequence != nil && l.Type().Sequence.SetItem != nil {
			return l.Type().Sequence.SetItem(l, int(i), value)
		}
	}
	return fmt.Errorf("TypeError: '%s' object does not support item assignment", container.Type().Name)
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
	if d, ok := scope.(*objects.Dict); ok {
		return d.DelItem(key)
	}
	return fmt.Errorf("vm: delete against unsupported scope type %T", scope)
}
