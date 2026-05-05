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

func deleteIn(scope objects.Object, key objects.Object, name string) error {
	if scope == nil {
		return fmt.Errorf("vm: NameError: name '%s' is not defined", name)
	}
	if d, ok := scope.(*objects.Dict); ok {
		return d.DelItem(key)
	}
	return fmt.Errorf("vm: delete against unsupported scope type %T", scope)
}
