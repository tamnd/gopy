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
	body := tokLine("ERROR_IF(res == NULL, error);")
	got, _, ok, note := TranslateBody(body, &SignatureAnalysis{Name: "X"})
	if !ok {
		t.Fatalf("translate failed: %s", note)
	}
	if !strings.Contains(got, "res") || !strings.Contains(got, "NULL") || !strings.Contains(got, `e.error("error")`) {
		t.Errorf("unexpected output:\n%s", got)
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
