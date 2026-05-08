package main

import (
	"bytes"
	"strings"
	"testing"
)

// newTestWriter returns a CWriter writing into a fresh buffer.
func newTestWriter() (*CWriter, *bytes.Buffer) {
	var buf bytes.Buffer
	return NewCWriter(&buf, 0, false), &buf
}

// runStorageForUop drives a uop through the stack model using the same
// for_uop sequence as the real generator, then renders push_outputs and
// flush so the test can inspect the bracketed C. inputsLive=false
// kills the inputs first to mimic a body that consumed them.
func runStorageForUop(t *testing.T, src, uopName string, inputsLive bool) (string, *Storage) {
	t.Helper()
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	u, ok := a.Uops[uopName]
	if !ok {
		t.Fatalf("missing uop %q", uopName)
	}
	w, buf := newTestWriter()
	stack := NewStack()
	storage, err := StorageForUop(stack, u, w, true)
	if err != nil {
		t.Fatalf("for_uop: %v", err)
	}
	if !inputsLive {
		for _, in := range storage.Inputs {
			in.Kill()
		}
	}
	// Mark every output that needs defining as written by the body.
	for _, o := range storage.Outputs {
		if storageNeedsDefining(o) {
			o.InLocal = true
		}
	}
	if err := storage.PushOutputs(); err != nil {
		t.Fatalf("push_outputs: %v", err)
	}
	if err := storage.Flush(w); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return buf.String(), storage
}

func TestStack_PointerOffset_ToCBasic(t *testing.T) {
	o := ZeroOffset()
	if got := o.ToC(); got != "0" {
		t.Errorf("zero offset: %q != %q", got, "0")
	}
	a := CreateOffset(2, []string{"oparg"}, nil)
	if got := a.ToC(); got != "2 + oparg" {
		t.Errorf("a.ToC: %q != %q", got, "2 + oparg")
	}
	b := a.Sub(CreateOffset(0, []string{"oparg"}, nil))
	if got := b.ToC(); got != "2" {
		t.Errorf("after cancel: %q != %q", got, "2")
	}
	if v, ok := b.AsInt(); !ok || v != 2 {
		t.Errorf("AsInt: %d %v", v, ok)
	}
}

func TestStack_PointerOffset_FromItem(t *testing.T) {
	scalar := &StackItem{Name: "x"}
	if got := OffsetFromItem(scalar).ToC(); got != "1" {
		t.Errorf("scalar item offset: %q", got)
	}
	arr := &StackItem{Name: "args", Size: "oparg"}
	if got := OffsetFromItem(arr).ToC(); got != "oparg" {
		t.Errorf("array item offset: %q", got)
	}
}

func TestStack_PurePop(t *testing.T) {
	src := `
op(_POP_USED, (val -- )) {
    DECREF_INPUTS();
}
`
	out, st := runStorageForUop(t, src, "_POP_USED", false)
	if !strings.Contains(out, "val = stack_pointer[-1];") {
		t.Errorf("missing pop assign in output: %q", out)
	}
	if !strings.Contains(out, "stack_pointer += -1;") {
		t.Errorf("missing stack_pointer decrement in output: %q", out)
	}
	if !st.Stack.IsFlushed() {
		t.Errorf("expected stack flushed; sp=%s logical=%s", st.Stack.PhysicalSp, st.Stack.LogicalSp)
	}
}

func TestStack_PurePush(t *testing.T) {
	src := `
op(_PUSH_ONE, (-- res)) {
    res = 1;
}
`
	out, _ := runStorageForUop(t, src, "_PUSH_ONE", false)
	// The push side bumps stack_pointer by +1 and stores via stack_pointer[-1].
	if !strings.Contains(out, "stack_pointer[0] = res;") {
		t.Errorf("missing store of res: %q", out)
	}
	if !strings.Contains(out, "stack_pointer += 1;") {
		t.Errorf("missing stack_pointer increment: %q", out)
	}
}

func TestStack_PeekAndReplace(t *testing.T) {
	src := `
op(_PEEK_REPLACE, (top -- top)) {
    top = top;
}
`
	out, _ := runStorageForUop(t, src, "_PEEK_REPLACE", true)
	// A peek-and-replace should not bump stack_pointer (logical_sp == base).
	if strings.Contains(out, "stack_pointer +=") {
		t.Errorf("peek should not bump stack_pointer, got %q", out)
	}
	if !strings.Contains(out, "top = stack_pointer[-1];") {
		t.Errorf("missing top peek read: %q", out)
	}
}

func TestStack_Macro_GrowsStack(t *testing.T) {
	// A macro composed of two uops: one that consumes `a` and pushes `b`,
	// then one that pushes another `c`. Net effect: pop one, push two.
	src := `
op(_FIRST, (a -- b)) {
    b = a;
}
op(_SECOND, (-- c)) {
    c = 0;
}
macro(_GROWS) = _FIRST + _SECOND;
`
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	inst, ok := a.Instructions["_GROWS"]
	if !ok {
		t.Fatalf("missing macro instruction _GROWS")
	}
	stack, err := GetStackEffect(inst)
	if err != nil {
		t.Fatalf("get_stack_effect: %v", err)
	}
	// inputs = [a]; outputs = [b, c]; net = +1 slot
	// Net stack delta from the start of the macro is logical_sp itself
	// (we started at zero). pop a, push b, push c => +1.
	delta := stack.LogicalSp
	if v, ok := delta.AsInt(); !ok || v != 1 {
		t.Errorf("expected logical_sp delta 1, got %s (ok=%v)", delta, ok)
	}
	if len(stack.Variables) != 2 {
		t.Errorf("expected 2 stash vars; got %d", len(stack.Variables))
	}
}

func TestStack_StorageForUop_SpillReload(t *testing.T) {
	// One uop, exercise save/reload bracketing through Storage.
	src := `
op(_USE, (a -- a)) {
    a = a;
}
`
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	u := a.Uops["_USE"]
	if u == nil {
		t.Fatalf("missing _USE")
	}
	w, buf := newTestWriter()
	stack := NewStack()
	storage, err := StorageForUop(stack, u, w, true)
	if err != nil {
		t.Fatalf("for_uop: %v", err)
	}
	storage.Save(w)
	w.Emit("foo();\n")
	if err := storage.Reload(w); err != nil {
		t.Fatalf("reload: %v", err)
	}
	w.MaybeWriteSpill()
	out := buf.String()
	if !strings.Contains(out, "_PyFrame_SetStackPointer(frame, stack_pointer);") {
		t.Errorf("missing spill save in %q", out)
	}
	if !strings.Contains(out, "stack_pointer = _PyFrame_GetStackPointer(frame);") {
		t.Errorf("missing reload in %q", out)
	}
	if !strings.Contains(out, "foo();") {
		t.Errorf("missing inner call in %q", out)
	}
}

func TestStack_RoundTrip_PreambleBracket(t *testing.T) {
	// Round-trip through parser+analyzer and assert the stack model
	// brackets the case body with the expected pointer adjustment.
	src := `
op(_BRACKET, (lhs, rhs -- res)) {
    res = lhs;
    use(rhs);
}
`
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	u := a.Uops["_BRACKET"]
	if u == nil {
		t.Fatal("missing _BRACKET")
	}
	w, buf := newTestWriter()
	stack := NewStack()
	storage, err := StorageForUop(stack, u, w, true)
	if err != nil {
		t.Fatalf("for_uop: %v", err)
	}
	for _, in := range storage.Inputs {
		in.Kill()
	}
	for _, o := range storage.Outputs {
		if storageNeedsDefining(o) {
			o.InLocal = true
		}
	}
	if err := storage.PushOutputs(); err != nil {
		t.Fatalf("push_outputs: %v", err)
	}
	if err := storage.Flush(w); err != nil {
		t.Fatalf("flush: %v", err)
	}
	out := buf.String()
	// We pop two and push one => net -1. The flush should bump physical sp by -1.
	if !strings.Contains(out, "stack_pointer += -1;") {
		t.Errorf("missing closing stack_pointer adjustment: %q", out)
	}
	// Both inputs should be assigned from the stack.
	if !strings.Contains(out, "rhs = stack_pointer[-1];") {
		t.Errorf("missing rhs assign: %q", out)
	}
	if !strings.Contains(out, "lhs = stack_pointer[-2];") {
		t.Errorf("missing lhs assign: %q", out)
	}
}
