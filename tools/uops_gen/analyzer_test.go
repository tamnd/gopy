package main

import (
	"strings"
	"testing"
)

// parseForest runs the parser to EOF and returns the node list.
func parseForest(t *testing.T, src string) []Node {
	t.Helper()
	p, err := NewParser(src, "<analyzer-test>")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	var out []Node
	for {
		n, err := p.Definition()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if n == nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func TestAnalyzer_SyntheticProperties(t *testing.T) {
	src := `
op(_LOAD, (-- res)) {
    res = 1;
}
op(_DEOPT, (left, right -- left, right)) {
    DEOPT_IF(left == right);
}
op(_ESCAPES, (-- res)) {
    res = PyDict_GetItemWithError(globals, name);
}
op(_PURE, (a -- b)) {
    b = a;
}
pure op(_REALLY_PURE, (a -- b)) {
    b = a;
}
op(_HAS_ERR, (a -- )) {
    if (a < 0) { ERROR_IF(1); }
}
`
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	cases := map[string]func(*Properties) bool{
		"_DEOPT":       func(p *Properties) bool { return p.Deopts && !p.Escapes },
		"_ESCAPES":     func(p *Properties) bool { return p.Escapes },
		"_REALLY_PURE": func(p *Properties) bool { return p.Pure && !p.Escapes },
		"_PURE":        func(p *Properties) bool { return !p.Pure }, // not annotated pure
		"_HAS_ERR":     func(p *Properties) bool { return p.ErrorWithPop },
	}
	for name, check := range cases {
		u, ok := a.Uops[name]
		if !ok {
			t.Errorf("missing uop %q", name)
			continue
		}
		if !check(u.Properties) {
			t.Errorf("uop %q properties failed: %+v", name, u.Properties)
		}
	}
}

func TestAnalyzer_StackPeekAndUsed(t *testing.T) {
	src := `
op(_PEEK, (top -- top, extra)) {
    extra = top;
}
`
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	u := a.Uops["_PEEK"]
	if u == nil {
		t.Fatal("missing uop _PEEK")
	}
	if !u.Stack.Inputs[0].Peek || !u.Stack.Outputs[0].Peek {
		t.Errorf("expected matching top to be marked peek; inputs=%+v outputs=%+v",
			u.Stack.Inputs[0], u.Stack.Outputs[0])
	}
}

func TestAnalyzer_RealBytecodes(t *testing.T) {
	const path = "/Users/apple/cpython-314/Python/bytecodes.c"
	src, err := readBytecodesSection(path)
	if err != nil {
		t.Skipf("upstream not available: %v", err)
	}
	p, err := NewParser(src, path)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	var forest []Node
	for {
		n, err := p.Definition()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if n == nil {
			break
		}
		forest = append(forest, n)
	}
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(a.Uops) < 50 {
		t.Errorf("only %d uops analyzed", len(a.Uops))
	}
	if len(a.Instructions) < 50 {
		t.Errorf("only %d instructions analyzed", len(a.Instructions))
	}

	// Spot-check representative uops. We sanity check that they exist
	// and that some properties are sensible. We don't pin every bool
	// because the upstream source evolves; we pick stable invariants.
	checks := map[string]func(*Uop) string{
		"LOAD_FAST": func(u *Uop) string {
			if u.Properties.Escapes {
				return "LOAD_FAST should not escape"
			}
			return ""
		},
		"_CHECK_VALIDITY": func(u *Uop) string {
			if !u.Properties.Deopts && !u.Properties.SideExit {
				return "_CHECK_VALIDITY should DEOPT_IF or EXIT_IF"
			}
			return ""
		},
		"_BINARY_OP_ADD_INT": func(u *Uop) string {
			// Upstream may or may not annotate pure; just check parse.
			if u.Name == "" {
				return "name unset"
			}
			return ""
		},
		"RESUME_CHECK": func(u *Uop) string {
			if u.Properties.Escapes {
				return "RESUME_CHECK should not escape"
			}
			return ""
		},
	}
	for name, check := range checks {
		u, ok := a.Uops[name]
		if !ok {
			t.Errorf("missing uop %q in analyzed forest", name)
			continue
		}
		if msg := check(u); msg != "" {
			t.Errorf("uop %q: %s", name, msg)
		}
	}
}

func TestAnalyzer_EscapeClassifiesPyDecref(t *testing.T) {
	// Py_DECREF is NOT in the non-escaping carve-out, so a body
	// that calls it should produce one EscapingCall pinned to that
	// identifier token. Py_INCREF, by contrast, is in the carve-out
	// and must not produce an EscapingCall.
	src := `
op(_DEC, (a -- )) {
    Py_DECREF(a);
}
op(_INC, (a -- a)) {
    Py_INCREF(a);
}
`
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	dec := a.Uops["_DEC"]
	if dec == nil {
		t.Fatal("missing _DEC")
	}
	if !dec.Properties.Escapes {
		t.Errorf("_DEC should escape via Py_DECREF; calls=%v", dec.Properties.EscapingCalls)
	}
	foundDecref := false
	for _, ec := range dec.Properties.EscapingCalls {
		if ec.Call.Text == "Py_DECREF" {
			foundDecref = true
		}
	}
	if !foundDecref {
		t.Errorf("Py_DECREF not flagged as escaping; got %+v", dec.Properties.EscapingCalls)
	}

	inc := a.Uops["_INC"]
	if inc == nil {
		t.Fatal("missing _INC")
	}
	if inc.Properties.Escapes {
		t.Errorf("_INC should not escape (Py_INCREF is in the non-escaping list); got %+v", inc.Properties.EscapingCalls)
	}
}

func TestAnalyzer_RejectsBadStack(t *testing.T) {
	// Output 'a' lives at a different stack slot than input 'a', so
	// after writing 'b' to slot 0 we cannot reuse 'a' at slot 1.
	src := `op(_BAD, (a, b -- b, a)) { }`
	forest := parseForest(t, src)
	_, err := AnalyzeForest(forest)
	if err == nil {
		t.Fatal("expected error from analyzeStack reuse")
	}
	if !strings.Contains(err.Error(), "Reuse of variable") {
		t.Logf("error message: %v", err)
	}
}
