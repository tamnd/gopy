package vm

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

// stampLoadSuperAttr rewrites the LOAD_SUPER_ATTR at instr to op (an
// ATTR / METHOD variant) and parks the adaptive counter on cooldown so
// trySpecialized routes through the fast arm instead of the adaptive
// tick. Mirrors _Py_Specialize_LoadSuperAttr.
//
// CPython: Python/specialize.c _Py_Specialize_LoadSuperAttr
func stampLoadSuperAttr(co *objects.Code, instr int, op compile.Opcode) {
	specialize.SetOpcode(co.Code, instr, op)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
}

// superAttrProgram lays out
//
//	idx 0  LOAD_CONST 0       (super)
//	idx 1  LOAD_CONST 1       (class)
//	idx 2  LOAD_CONST 2       (self)
//	idx 3  LOAD_SUPER_ATTR_X  (fast arm under test)
//	idx 4+ RETURN_VALUE
//
// The LOAD_SUPER_ATTR opcode has cache cells, so idx 4 in codeunits is
// past the CACHE words. The tests stamp the opcode at index 3 and assert
// either the e2e result (ATTR arm) or the post-arm stack state (METHOD
// arm) drives the right shape.
func superAttrProgram(op compile.Opcode, oparg byte, names []string, class, self objects.Object) *objects.Code {
	return &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.LOAD_CONST, 2),
			instr(op, oparg),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{objects.SuperType, class, self},
		Names:     names,
		Stacksize: 8,
	}
}

// makeSuperHierarchy builds a (Base, Sub) pair where Base defines
// methodName and Sub inherits it. SuperLookup walks Sub.MRO past Sub
// (the explicit class) and finds methodName on Base. isMethodLike on
// *Function is true, so SuperLookup's method_found probe trips on it.
//
// This mirrors the canonical `class Sub(Base): ... super(Sub, x).m()`
// shape the specializer is built for.
func makeSuperHierarchy(t *testing.T, baseName, subName, methodName string) (*objects.Type, *objects.Type, *objects.Function) {
	t.Helper()
	base := objects.NewType(baseName, []*objects.Type{objects.ObjectType()})
	sub := objects.NewType(subName, []*objects.Type{base})
	body := concat(
		instr(compile.RESUME, 0),
		instr(compile.LOAD_FAST, 0),
		instr(compile.RETURN_VALUE, 0),
	)
	inner := objects.NewCode()
	inner.Code = body
	inner.Argcount = 1
	inner.Varnames = []string{"self"}
	inner.Stacksize = 4
	inner.Name = methodName
	inner.Flags = int(compile.CoOptimized)
	fn := objects.NewFunction(methodName, inner, objects.NewDict())
	fn.Builtins = objects.NewDict()
	objects.SetTypeDescr(base, methodName, fn)
	return base, sub, fn
}

// makeSuperHierarchyWithAttr installs a non-method-like class attribute
// (a plain int) on the base type so SuperLookup's method_found probe
// stays false on lookups against that name.
func makeSuperHierarchyWithAttr(t *testing.T, baseName, subName, attrName string, value objects.Object) (*objects.Type, *objects.Type) {
	t.Helper()
	base := objects.NewType(baseName, []*objects.Type{objects.ObjectType()})
	sub := objects.NewType(subName, []*objects.Type{base})
	objects.SetTypeDescr(base, attrName, value)
	return base, sub
}

// newEvalStateForFastArm builds an evalState wrapped around a fresh
// frame for co, ready for a fast-arm function to be invoked directly.
// Tests that need to inspect both items the METHOD arm pushes use this
// to bypass the dispatch loop. Mirrors the work Eval() does on entry.
func newEvalStateForFastArm(t *testing.T, ts *state.Thread, co *objects.Code) *evalState {
	t.Helper()
	stack := frameStackFor(ts)
	f := stack.Push(co, nil, nil, nil)
	t.Cleanup(stack.Pop)
	v := vmFor(ts)
	return &evalState{ts: ts, f: f, breaker: v.breaker, gilTimer: &v.gilTimer, gil: v.gil, code: f.Code.Code}
}

var _ *frame.Frame // satisfy import linter when frame indirections are used below

// TestFastLoadSuperAttrAttrHit drives LOAD_SUPER_ATTR_ATTR end-to-end:
// the (SuperType, cls, instance) tuple lives at the bottom of the stack,
// the name index resolves to "greet", and the fast arm should bind the
// underlying function through tp_descr_get and push the bound method.
//
// CPython: Python/bytecodes.c:2221 LOAD_SUPER_ATTR_ATTR
func TestFastLoadSuperAttrAttrHit(t *testing.T) {
	_, cls, fn := makeSuperHierarchy(t, "BaseC", "SubC", "greet")
	self := objects.NewInstance(cls)
	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 0, []string{"greet"}, cls, self)
	specialize.Enable(co)
	stampLoadSuperAttr(co, 3, compile.LOAD_SUPER_ATTR_ATTR)

	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	bm, ok := v.(*objects.BoundMethod)
	if !ok {
		t.Fatalf("got %T, want *objects.BoundMethod", v)
	}
	if bm.Func() != fn {
		t.Errorf("bound function: got %v, want %v", bm.Func(), fn)
	}
	if bm.Self() != objects.Object(self) {
		t.Errorf("bound self: got %v, want instance", bm.Self())
	}
	if op := compile.Opcode(co.Code[2*3]); op != compile.LOAD_SUPER_ATTR_ATTR {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastLoadSuperAttrAttrMissing exercises the AttributeError surface:
// SuperLookup walks the MRO past cls and finds nothing, so the fast arm
// returns the error and the dispatcher unwinds.
func TestFastLoadSuperAttrAttrMissing(t *testing.T) {
	_, cls, _ := makeSuperHierarchy(t, "BaseD", "SubD", "greet")
	self := objects.NewInstance(cls)
	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 0, []string{"missing"}, cls, self)
	specialize.Enable(co)
	stampLoadSuperAttr(co, 3, compile.LOAD_SUPER_ATTR_ATTR)

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected AttributeError")
	}
	if !strings.Contains(err.Error(), "AttributeError") || !strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, want AttributeError mentioning 'missing'", err)
	}
}

// TestFastLoadSuperAttrAttrDeoptsOnNonSuper plants a non-super value in
// the global_super slot. The fast arm's prelude guard fails (ok=false),
// the dispatcher rewrites the opcode back to LOAD_SUPER_ATTR, and the
// generic body tries to call the non-callable sentinel as super(...)
// which surfaces a TypeError. The point of the test is that the opcode
// got rewritten back; the error is incidental.
func TestFastLoadSuperAttrAttrDeoptsOnNonSuper(t *testing.T) {
	_, cls, _ := makeSuperHierarchy(t, "BaseE", "SubE", "greet")
	self := objects.NewInstance(cls)
	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 2, []string{"greet"}, cls, self)
	co.Consts[0] = objects.NewInt(1)
	specialize.Enable(co)
	stampLoadSuperAttr(co, 3, compile.LOAD_SUPER_ATTR_ATTR)

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error from generic body invoking non-callable as super()")
	}
	if op := compile.Opcode(co.Code[2*3]); op == compile.LOAD_SUPER_ATTR_ATTR {
		t.Errorf("guard miss should have deopted, but op stayed LOAD_SUPER_ATTR_ATTR")
	}
}

// TestFastLoadSuperAttrAttrDeoptsOnNonType plants a non-Type in the
// class slot. The fast arm's *Type assertion fails, the dispatcher
// rewrites the opcode back to LOAD_SUPER_ATTR, and the generic body
// surfaces the TypeError the super(non-type, ...) constructor would
// have raised.
func TestFastLoadSuperAttrAttrDeoptsOnNonType(t *testing.T) {
	_, cls, _ := makeSuperHierarchy(t, "BaseF", "SubF", "greet")
	self := objects.NewInstance(cls)
	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 2, []string{"greet"}, cls, self)
	co.Consts[1] = objects.NewInt(1)
	specialize.Enable(co)
	stampLoadSuperAttr(co, 3, compile.LOAD_SUPER_ATTR_ATTR)

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected TypeError from generic body's super(non-type, ...)")
	}
	if op := compile.Opcode(co.Code[2*3]); op == compile.LOAD_SUPER_ATTR_ATTR {
		t.Errorf("guard miss should have deopted, but op stayed LOAD_SUPER_ATTR_ATTR")
	}
}

// TestFastLoadSuperAttrMethodMethodFound drives the METHOD arm against a
// *Function descriptor. SuperLookup's method_found probe must trip, so
// the arm pushes (descr, self) as the unbound-method pair a following
// CALL would consume. The arm is invoked directly because the
// (attr, self) shape can't be unwrapped through RETURN_VALUE in a
// single instruction.
func TestFastLoadSuperAttrMethodMethodFound(t *testing.T) {
	_, cls, fn := makeSuperHierarchy(t, "BaseG", "SubG", "greet")
	self := objects.NewInstance(cls)

	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 1, []string{"greet"}, cls, self)
	specialize.Enable(co)
	stampLoadSuperAttr(co, 3, compile.LOAD_SUPER_ATTR_METHOD)

	ts := state.NewThread()
	e := newEvalStateForFastArm(t, ts, co)
	e.pushObject(objects.SuperType)
	e.pushObject(cls)
	e.pushObject(self)
	// Position the instruction pointer at the stamped opcode (codeunit
	// index 3, byte offset 6).
	e.f.InstrPtr = 6
	oparg := uint32((0 << 2) | 1) // name idx 0, load_method bit set
	_, ok, err := e.fastLoadSuperAttrMethod(oparg)
	if err != nil {
		t.Fatalf("fastLoadSuperAttrMethod: %v", err)
	}
	if !ok {
		t.Fatal("expected fast arm to take the dispatch")
	}
	// After the arm runs the stack has (attr, self) with self at TOS.
	if topSelf := e.peek(0).AsObject(); topSelf != objects.Object(self) {
		t.Errorf("TOS: got %v, want self (unbound-method pair)", topSelf)
	}
	if attr := e.peek(1).AsObject(); attr != objects.Object(fn) {
		t.Errorf("attr below TOS: got %v, want raw function", attr)
	}
}

// TestFastLoadSuperAttrMethodBound drives the METHOD arm against a
// non-method-like descriptor (a plain int). SuperLookup's method_found
// probe stays false, so the arm binds the descriptor through
// tp_descr_get (a no-op for int) and pushes (attr, NULL).
func TestFastLoadSuperAttrMethodBound(t *testing.T) {
	_, cls := makeSuperHierarchyWithAttr(t, "BaseH", "SubH", "magic", objects.NewInt(99))
	self := objects.NewInstance(cls)

	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 1, []string{"magic"}, cls, self)
	specialize.Enable(co)
	stampLoadSuperAttr(co, 3, compile.LOAD_SUPER_ATTR_METHOD)

	ts := state.NewThread()
	e := newEvalStateForFastArm(t, ts, co)
	e.pushObject(objects.SuperType)
	e.pushObject(cls)
	e.pushObject(self)
	e.f.InstrPtr = 6
	oparg := uint32((0 << 2) | 1)
	_, ok, err := e.fastLoadSuperAttrMethod(oparg)
	if err != nil {
		t.Fatalf("fastLoadSuperAttrMethod: %v", err)
	}
	if !ok {
		t.Fatal("expected fast arm to take the dispatch")
	}
	if top := e.peek(0); top != stackref.Null {
		t.Errorf("TOS: got %v, want Null (bound-method shape)", top.AsObject())
	}
	attr := e.peek(1).AsObject()
	if got, _ := attr.(*objects.Int).Int64(); got != 99 {
		t.Errorf("attr: got %v, want 99", attr)
	}
}

// TestFastLoadSuperAttrAttrRejectsLoadMethodBit verifies the
// `assert(!(oparg & 1))` invariant: when bit 0 is set the ATTR arm must
// refuse, matching CPython's assertion in Python/bytecodes.c:2222.
func TestFastLoadSuperAttrAttrRejectsLoadMethodBit(t *testing.T) {
	_, cls, _ := makeSuperHierarchy(t, "BaseI", "SubI", "greet")
	self := objects.NewInstance(cls)
	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 0, []string{"greet"}, cls, self)
	specialize.Enable(co)

	ts := state.NewThread()
	e := newEvalStateForFastArm(t, ts, co)
	e.pushObject(objects.SuperType)
	e.pushObject(cls)
	e.pushObject(self)
	e.f.InstrPtr = 6
	oparg := uint32((0 << 2) | 1)
	_, ok, err := e.fastLoadSuperAttrAttr(oparg)
	if err != nil {
		t.Fatalf("fastLoadSuperAttrAttr: %v", err)
	}
	if ok {
		t.Error("ATTR arm must refuse load_method bit (oparg & 1)")
	}
}

// TestFastLoadSuperAttrMethodRejectsAttrBit is the dual assertion: the
// METHOD arm must refuse when bit 0 is clear, matching
// Python/bytecodes.c:2237's `assert(oparg & 1)`.
func TestFastLoadSuperAttrMethodRejectsAttrBit(t *testing.T) {
	_, cls, _ := makeSuperHierarchy(t, "BaseJ", "SubJ", "greet")
	self := objects.NewInstance(cls)
	co := superAttrProgram(compile.LOAD_SUPER_ATTR, 0, []string{"greet"}, cls, self)
	specialize.Enable(co)

	ts := state.NewThread()
	e := newEvalStateForFastArm(t, ts, co)
	e.pushObject(objects.SuperType)
	e.pushObject(cls)
	e.pushObject(self)
	e.f.InstrPtr = 6
	oparg := uint32(0 << 2) // load_method clear
	_, ok, err := e.fastLoadSuperAttrMethod(oparg)
	if err != nil {
		t.Fatalf("fastLoadSuperAttrMethod: %v", err)
	}
	if ok {
		t.Error("METHOD arm must refuse when load_method bit is clear")
	}
}
