package main

import (
	"strings"
	"testing"
)

func tokLine(text string) []dslTok {
	toks, err := tokenize(text)
	if err != nil {
		panic(err)
	}
	return toks
}

func TestTranslateBodyDeoptIf(t *testing.T) {
	body := tokLine("DEOPT_IF(!check(left));")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "if ") || !strings.Contains(got, "left") || !strings.Contains(got, "e.deoptHere()") {
		t.Errorf("unexpected output:\n%s", got)
	}
}

func TestTranslateBodyErrorIf(t *testing.T) {
	// CPython 3.14 ERROR_IF takes only the condition; the label is
	// implicitly `error`.
	body := tokLine("ERROR_IF(res == NULL);")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "res") || !strings.Contains(got, "NULL") || !strings.Contains(got, `e.error("error")`) {
		t.Errorf("unexpected output:\n%s", got)
	}
}

func TestTranslateBodyErrorIfTrueIsUnconditional(t *testing.T) {
	body := tokLine("ERROR_IF(true);")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if strings.Contains(got, "if ") {
		t.Errorf("ERROR_IF(true) must be unconditional, got:\n%s", got)
	}
	if !strings.Contains(got, `e.error("error")`) {
		t.Errorf("missing return, got:\n%s", got)
	}
}

func TestTranslateBodyDecrefInputs(t *testing.T) {
	body := tokLine("DECREF_INPUTS();")
	sig := &SignatureAnalysis{Name: "X", Inputs: []StackBinding{{Name: "a"}, {Name: "b"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "e.decrefInputs(2)") {
		t.Errorf("expected e.decrefInputs(2), got:\n%s", got)
	}
}

func TestTranslateBodyStatIncSkipped(t *testing.T) {
	body := tokLine("STAT_INC(BINARY_OP, hit);")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if got != "" {
		t.Errorf("expected empty body for STAT_INC, got:\n%s", got)
	}
}

func TestTranslateBodyUnrecognizedFalls(t *testing.T) {
	body := tokLine("res = some_helper(left, right);")
	_, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if ok {
		t.Errorf("expected fallback for unrecognized body")
	}
	if note == "" {
		t.Errorf("expected note explaining fallback")
	}
}

func TestTranslateBodyStackRefClose(t *testing.T) {
	body := tokLine("PyStackRef_CLOSE(value);")
	sig := &SignatureAnalysis{Name: "POP_TOP", Inputs: []StackBinding{{Name: "value"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "value.Close()") {
		t.Errorf("expected value.Close(), got:\n%s", got)
	}
}

func TestTranslateBodyStackRefCloseKeywordSlot(t *testing.T) {
	body := tokLine("PyStackRef_CLOSE(type);")
	sig := &SignatureAnalysis{Name: "X", Inputs: []StackBinding{{Name: "type"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "type_v.Close()") {
		t.Errorf("expected type_v.Close() (keyword rename), got:\n%s", got)
	}
}

func TestTranslateBodyDeadIsNoop(t *testing.T) {
	body := tokLine("DEAD(value);")
	sig := &SignatureAnalysis{Name: "X", Inputs: []StackBinding{{Name: "value"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if got != "" {
		t.Errorf("expected empty body for DEAD, got:\n%s", got)
	}
}

func TestTranslateBodyOutputsBail(t *testing.T) {
	body := tokLine("DEAD(value);")
	sig := &SignatureAnalysis{Name: "X", Outputs: []StackBinding{{Name: "out"}}}
	_, _, ok, note := TranslateBody(body, sig)
	if ok {
		t.Errorf("expected bail when sig has outputs")
	}
	if note == "" {
		t.Errorf("expected explanatory note")
	}
}

func TestTranslateBodyJumpBy(t *testing.T) {
	body := tokLine("JUMPBY(oparg);")
	got, terminates, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "JUMP_FORWARD"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !terminates {
		t.Errorf("expected terminates=true for JUMPBY")
	}
	if !strings.Contains(got, "e.jumpBy(int(oparg) + 1)") {
		t.Errorf("expected jumpBy return, got:\n%s", got)
	}
}

func TestTranslateBodyOutputAssignPyStackRefNull(t *testing.T) {
	body := tokLine("res = PyStackRef_NULL;")
	sig := &SignatureAnalysis{Name: "PUSH_NULL", Outputs: []StackBinding{{Name: "res"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "res = stackref.Null") {
		t.Errorf("expected res = stackref.Null, got:\n%s", got)
	}
}

func TestTranslateBodyOutputAssignLoadFast(t *testing.T) {
	body := tokLine("value = PyStackRef_DUP(GETLOCAL(oparg));")
	sig := &SignatureAnalysis{Name: "LOAD_FAST", Outputs: []StackBinding{{Name: "value"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "value = e.localAt(int(oparg)).Dup()") {
		t.Errorf("expected dup-of-local, got:\n%s", got)
	}
}

func TestTranslateBodyOutputUnassignedBails(t *testing.T) {
	// Output declared but body never assigns it; translator must bail.
	body := tokLine("assert(true);")
	sig := &SignatureAnalysis{Name: "X", Outputs: []StackBinding{{Name: "out"}}}
	_, _, ok, note := TranslateBody(body, sig)
	if ok {
		t.Errorf("expected bail when output left unassigned")
	}
	if note == "" {
		t.Errorf("expected explanatory note")
	}
}

func TestTranslateBodyOutputAssignTrueFalse(t *testing.T) {
	body := tokLine("b = PyStackRef_True;")
	sig := &SignatureAnalysis{Name: "X", Outputs: []StackBinding{{Name: "b"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "b = stackref.True") {
		t.Errorf("expected stackref.True, got:\n%s", got)
	}
}

func TestTranslateBodyGetlocalLvalue(t *testing.T) {
	body := tokLine("GETLOCAL(oparg) = PyStackRef_NULL;")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "e.setLocal(int(oparg), stackref.Null)") {
		t.Errorf("expected setLocal, got:\n%s", got)
	}
}

func TestTranslateBodyLoadFastAndClear(t *testing.T) {
	body := tokLine("value = GETLOCAL(oparg); GETLOCAL(oparg) = PyStackRef_NULL;")
	sig := &SignatureAnalysis{Name: "LOAD_FAST_AND_CLEAR", Outputs: []StackBinding{{Name: "value"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "value = e.localAt(int(oparg))") {
		t.Errorf("missing value = local, got:\n%s", got)
	}
	if !strings.Contains(got, "e.setLocal(int(oparg), stackref.Null)") {
		t.Errorf("missing setLocal, got:\n%s", got)
	}
}

func TestTranslateBodyStoreFast(t *testing.T) {
	body := tokLine(`_PyStackRef tmp = GETLOCAL(oparg); GETLOCAL(oparg) = value; DEAD(value); PyStackRef_XCLOSE(tmp);`)
	sig := &SignatureAnalysis{Name: "STORE_FAST", Inputs: []StackBinding{{Name: "value"}}}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	for _, want := range []string{
		"tmp := e.localAt(int(oparg))",
		"e.setLocal(int(oparg), value)",
		"tmp.Close()",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestTranslateBodyOutputAssignTernary(t *testing.T) {
	body := tokLine("res = PyStackRef_IsFalse(value) ? PyStackRef_True : PyStackRef_False;")
	sig := &SignatureAnalysis{
		Name:    "UNARY_NOT",
		Inputs:  []StackBinding{{Name: "value"}},
		Outputs: []StackBinding{{Name: "res"}},
	}
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	for _, want := range []string{
		"if value.IsFalse()",
		"res = stackref.True",
		"res = stackref.False",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestTranslateBodyJumpByNegativeOparg(t *testing.T) {
	body := tokLine("JUMPBY(-oparg);")
	got, terminates, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "JUMP_BACKWARD"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !terminates {
		t.Errorf("expected terminates=true for JUMPBY")
	}
	if !strings.Contains(got, "e.jumpBy(-int(oparg) + 1)") {
		t.Errorf("expected negative jumpBy return, got:\n%s", got)
	}
}

func TestTranslateBodyVoidCast(t *testing.T) {
	// `(void)this_instr;` is a no-op cast CPython uses to silence
	// unused-variable warnings (e.g. INSTRUMENTED_NOT_TAKEN). The
	// translator should consume it and emit nothing.
	body := tokLine("(void)this_instr;")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if got != "" {
		t.Errorf("expected empty body for (void)cast, got:\n%s", got)
	}
}

func TestTranslateBodyVoidCastFollowedByCall(t *testing.T) {
	// After the cast the rest of the body should still translate.
	// Use ERROR_IF since it has a known shape.
	body := tokLine("(void)x; ERROR_IF(cond);")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "if cond") || !strings.Contains(got, `e.error("error")`) {
		t.Errorf("expected ERROR_IF body after cast, got:\n%s", got)
	}
}

func TestTranslateBodyPyObjectDecl(t *testing.T) {
	// `PyObject *obj = PyStackRef_AsPyObjectBorrow(value);` is the
	// idiomatic prologue for bytecodes that need a borrowed view of a
	// stack input. The decl should render as `obj := value.AsObject()`
	// and the name should be reachable for later references.
	sig := &SignatureAnalysis{
		Name:   "X",
		Inputs: []StackBinding{{Name: "value"}},
	}
	body := tokLine("PyObject *obj = PyStackRef_AsPyObjectBorrow(value);")
	got, _, ok, note := TranslateBody(body, sig)
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "obj := value.AsObject()") {
		t.Errorf("unexpected output:\n%s", got)
	}
}

func TestTranslateBodyFromPyObjectExpressions(t *testing.T) {
	// The Steal / New / Immortal / Borrow variants all collapse to
	// stackref.FromObject under Go's GC.
	sig := &SignatureAnalysis{
		Name:    "X",
		Inputs:  []StackBinding{{Name: "value"}},
		Outputs: []StackBinding{{Name: "res"}},
	}
	for _, variant := range []string{
		"PyStackRef_FromPyObjectSteal",
		"PyStackRef_FromPyObjectNew",
		"PyStackRef_FromPyObjectImmortal",
		"PyStackRef_FromPyObjectBorrow",
	} {
		body := tokLine("PyObject *o = PyStackRef_AsPyObjectBorrow(value); res = " + variant + "(o);")
		got, _, ok, note := TranslateBody(body, sig)
		if !ok {
			t.Fatalf("%s: translate failed: %s", variant, note)
		}
		if !strings.Contains(got, "stackref.FromObject(o)") {
			t.Errorf("%s: missing FromObject wrap:\n%s", variant, got)
		}
	}
}

func TestSplitTopLevelComma(t *testing.T) {
	a, b, ok := splitTopLevelComma("res == NULL, error")
	if !ok || strings.TrimSpace(a) != "res == NULL" || strings.TrimSpace(b) != "error" {
		t.Errorf("got %q / %q ok=%v", a, b, ok)
	}
	a, b, ok = splitTopLevelComma("f(x, y), error")
	if !ok || strings.TrimSpace(a) != "f(x, y)" || strings.TrimSpace(b) != "error" {
		t.Errorf("nested: got %q / %q ok=%v", a, b, ok)
	}
}
