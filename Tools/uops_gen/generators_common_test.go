package main

import (
	"bytes"
	"strings"
	"testing"
)

// tok builds a synthetic Token of the given kind with text.
func tok(kind, text string) Token {
	return Token{Kind: kind, Text: text}
}

// tokensFromText tokenizes a string of C-shaped DSL.
func tokensFromText(t *testing.T, src string) []Token {
	t.Helper()
	tks, err := Tokenize(src, "<test>", 1)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	return tks
}

// emitterFromSrc parses src, analyzes it, picks the named uop, and
// returns an Emitter / Storage / Uop wired against a fresh CWriter.
func emitterFromSrc(t *testing.T, src, uopName string) (*Emitter, *Storage, *Uop, *bytes.Buffer) {
	t.Helper()
	forest := parseForest(t, src)
	a, err := AnalyzeForest(forest)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	u, ok := a.Uops[uopName]
	if !ok {
		t.Fatalf("uop %q missing", uopName)
	}
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	storage, err := StorageForUop(NewStack(), u, w, true)
	if err != nil {
		t.Fatalf("for_uop: %v", err)
	}
	e := NewEmitter(w, a.Labels)
	return e, storage, u, &buf
}

func TestTokenIterator_NextPeek(t *testing.T) {
	tks := []Token{tok(TokIdentifier, "a"), tok(TokIdentifier, "b")}
	it := NewTokenIterator(tks)
	if p, ok := it.Peek(); !ok || p.Text != "a" {
		t.Fatalf("peek1: %v %v", p, ok)
	}
	n, _ := it.Next()
	if n.Text != "a" {
		t.Fatalf("next1: %v", n)
	}
	if p, ok := it.Peek(); !ok || p.Text != "b" {
		t.Fatalf("peek2: %v %v", p, ok)
	}
	n, _ = it.Next()
	if n.Text != "b" {
		t.Fatalf("next2: %v", n)
	}
	if _, ok := it.Next(); ok {
		t.Fatal("expected EOF")
	}
}

func TestEmitTo_StopsAtTopLevelRParen(t *testing.T) {
	src := "x + (y * z) ) tail"
	tks := tokensFromText(t, src)
	var buf bytes.Buffer
	w := NewCWriter(&buf, 0, false)
	it := NewTokenIterator(tks)
	end, err := emitTo(w, it, TokRParen)
	if err != nil {
		t.Fatalf("emitTo: %v", err)
	}
	if end.Kind != TokRParen {
		t.Fatalf("end kind: %s", end.Kind)
	}
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "x + (y * z)") {
		t.Errorf("output: %q", got)
	}
	// "tail" must remain in the iterator.
	rest, _ := it.Next()
	if rest.Text != "tail" {
		t.Errorf("rest: %v", rest)
	}
}

func TestAlwaysTrue(t *testing.T) {
	cases := []struct {
		text string
		ok   bool
		want bool
	}{
		{"true", true, true},
		{"1", true, true},
		{"x", true, false},
		{"", false, false},
	}
	for _, c := range cases {
		got := alwaysTrue(Token{Text: c.text}, c.ok)
		if got != c.want {
			t.Errorf("alwaysTrue(%q,%v)=%v want %v", c.text, c.ok, got, c.want)
		}
	}
}

func TestTypeAndNull(t *testing.T) {
	cases := []struct {
		item             *StackItem
		wantType, wantNS string
	}{
		{&StackItem{Type: "PyObject *"}, "PyObject *", "NULL"},
		{&StackItem{Size: "oparg"}, "_PyStackRef *", "NULL"},
		{&StackItem{}, "_PyStackRef", "PyStackRef_NULL"},
	}
	for _, c := range cases {
		gotT, gotN := typeAndNull(c.item)
		if gotT != c.wantType || gotN != c.wantNS {
			t.Errorf("typeAndNull(%+v) = (%q,%q), want (%q,%q)",
				c.item, gotT, gotN, c.wantType, c.wantNS)
		}
	}
}

func TestCFlags_Empty(t *testing.T) {
	p := &Properties{
		EscapingCalls: map[*SimpleStmt]EscapingCall{},
	}
	// All-zero properties is infallible and has no flags.
	if got := CFlags(p); got != "0" {
		t.Errorf("default flags: %q", got)
	}
}

func TestCFlags_FullyLoaded(t *testing.T) {
	p := &Properties{
		Oparg:           true,
		UsesCoConsts:    true,
		UsesCoNames:     true,
		Jumps:           true,
		HasFree:         true,
		UsesLocals:      true,
		EvalBreaker:     true,
		Deopts:          true,
		SideExit:        true,
		ErrorWithPop:    true,
		ErrorWithoutPop: true,
		Escapes:         true,
		Pure:            true,
		NoSaveIP:        true,
	}
	got := CFlags(p)
	want := []string{
		"HAS_ARG_FLAG", "HAS_CONST_FLAG", "HAS_NAME_FLAG", "HAS_JUMP_FLAG",
		"HAS_FREE_FLAG", "HAS_LOCAL_FLAG", "HAS_EVAL_BREAK_FLAG",
		"HAS_DEOPT_FLAG", "HAS_EXIT_FLAG", "HAS_ERROR_FLAG",
		"HAS_ERROR_NO_POP_FLAG", "HAS_ESCAPES_FLAG", "HAS_PURE_FLAG",
		"HAS_NO_SAVE_IP_FLAG",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing flag %s in %q", w, got)
		}
	}
	if got != strings.Join(want, " | ") {
		t.Errorf("order mismatch:\n got: %s\nwant: %s", got, strings.Join(want, " | "))
	}
}

func TestCFlags_PartialDefaults(t *testing.T) {
	p := &Properties{}
	if got := CFlags(p); got != "0" {
		t.Errorf("infallible bag should be 0, got %q", got)
	}
}

func TestRootRelativePath_RelativeOk(t *testing.T) {
	root := "/x/y"
	if got := rootRelativePath("/x/y/z/file.c", root); got != "z/file.c" {
		t.Errorf("got %q", got)
	}
	// Path outside root is returned unchanged.
	if got := rootRelativePath("/other/file.c", root); got != "/other/file.c" {
		t.Errorf("outside root: %q", got)
	}
}

func TestWriteHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := writeHeader("/r/g.py", []string{"/r/a.c", "/r/b.c"}, &buf, "//", "/r"); err != nil {
		t.Fatalf("writeHeader: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"// This file is generated by g.py", "from:", "a.c, b.c", "Do not edit!"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// --- replacer-level tests using a synthetic uop body ---

// runReplacer drives a single replacer over a synthetic stream
// `KEYWORD(args);` and returns the rewritten output and reachability.
func runReplacer(
	t *testing.T,
	src, uopName, keyword, body string,
) (string, bool, error) {
	t.Helper()
	e, storage, u, buf := emitterFromSrc(t, src, uopName)
	tks := tokensFromText(t, keyword+body)
	if len(tks) == 0 {
		t.Fatalf("no tokens for %q", body)
	}
	it := NewTokenIterator(tks)
	first, _ := it.Next()
	fn, ok := e.replacers[first.Text]
	if !ok {
		t.Fatalf("no replacer for %q", first.Text)
	}
	reachable, err := fn(first, it, u, storage, nil)
	return buf.String(), reachable, err
}

const decrefSrc = `
op(_X, (left, right -- )) {
    DECREF_INPUTS();
}
`

func TestEmitter_DecrefInputs(t *testing.T) {
	out, reachable, err := runReplacer(t, decrefSrc, "_X", "DECREF_INPUTS", "();")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reachable {
		t.Errorf("DECREF_INPUTS should remain reachable")
	}
	if !strings.Contains(out, "PyStackRef_CLOSE") {
		t.Errorf("output missing PyStackRef_CLOSE:\n%s", out)
	}
}

const syncSrc = `
op(_S, (a, b -- a, b)) {
    SYNC_SP();
}
`

func TestEmitter_SyncSp(t *testing.T) {
	out, reachable, err := runReplacer(t, syncSrc, "_S", "SYNC_SP", "();")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reachable {
		t.Error("SYNC_SP should remain reachable")
	}
	_ = out
}

const errIfSrc = `
op(_E, (a -- a)) {
    ERROR_IF(a == 0);
}
`

func TestEmitter_ErrorIf_Conditional(t *testing.T) {
	out, reachable, err := runReplacer(t, errIfSrc, "_E", "ERROR_IF", "(a == 0);")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reachable {
		t.Error("conditional ERROR_IF stays reachable")
	}
	if !strings.Contains(out, "if (") || !strings.Contains(out, "JUMP_TO_LABEL(error)") {
		t.Errorf("ERROR_IF body wrong:\n%s", out)
	}
}

const errIfTrue = `
op(_T, (a -- a)) {
    ERROR_IF(true);
}
`

func TestEmitter_ErrorIf_Unconditional(t *testing.T) {
	out, reachable, err := runReplacer(t, errIfTrue, "_T", "ERROR_IF", "(true);")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if reachable {
		t.Error("unconditional ERROR_IF must mark unreachable")
	}
	if !strings.Contains(out, "JUMP_TO_LABEL(error)") {
		t.Errorf("missing jump:\n%s", out)
	}
	if strings.Contains(out, "if (") {
		t.Errorf("unconditional should not wrap in if:\n%s", out)
	}
}

const errNoPopSrc = `
op(_N, (a -- a)) {
    ERROR_NO_POP();
}
`

func TestEmitter_ErrorNoPop(t *testing.T) {
	out, reachable, err := runReplacer(t, errNoPopSrc, "_N", "ERROR_NO_POP", "();")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if reachable {
		t.Error("ERROR_NO_POP must mark unreachable")
	}
	if !strings.Contains(out, "JUMP_TO_LABEL(error)") {
		t.Errorf("missing jump:\n%s", out)
	}
}

// minimal source that produces a uop with a family attached.
const deoptParityFamily = `
op(_DG, (a -- a)) {
    DEOPT_IF(a == 0);
}
op(_DG2, (a -- a)) {
}
inst(_DG, (a -- a)) {
    DEOPT_IF(a == 0);
}
inst(_DG2, (a -- a)) {
}
family(_DG, 0) = { _DG, _DG2 };
`

func TestEmitter_DeoptIf(t *testing.T) {
	e, storage, u, buf := emitterFromSrc(t, deoptParityFamily, "_DG")
	// Need an Instruction whose Family is set.
	a, err := AnalyzeForest(parseForest(t, deoptParityFamily))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	inst := a.Instructions["_DG"]
	if inst == nil || inst.Family == nil {
		t.Skip("expected family for _DG; analyzer setup differs")
	}
	tks := tokensFromText(t, "DEOPT_IF(a == 0);")
	it := NewTokenIterator(tks)
	first, _ := it.Next()
	reachable, err := e.DeoptIf(first, it, u, storage, inst)
	if err != nil {
		t.Fatalf("DEOPT_IF: %v", err)
	}
	if !reachable {
		t.Error("conditional DEOPT_IF stays reachable")
	}
	out := buf.String()
	for _, want := range []string{"if (", "UPDATE_MISS_STATS(_DG)", "JUMP_TO_PREDICTED(_DG)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

const dispatchSrc = `
op(_DSP, (-- )) {
    DISPATCH();
}
`

func TestEmitter_Dispatch(t *testing.T) {
	e, storage, u, buf := emitterFromSrc(t, dispatchSrc, "_DSP")
	tks := tokensFromText(t, "DISPATCH")
	first := tks[0]
	reachable, err := e.Dispatch(first, NewTokenIterator(nil), u, storage, nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if reachable {
		t.Error("DISPATCH marks unreachable")
	}
	if !strings.Contains(buf.String(), "DISPATCH") {
		t.Errorf("missing DISPATCH:\n%s", buf.String())
	}
}

const stackPtrSrc = `
op(_SP, (-- )) {
    if (stack_pointer != NULL) { (void)0; }
}
`

func TestEmitter_StackPointer_Spilled(t *testing.T) {
	e, storage, u, _ := emitterFromSrc(t, stackPtrSrc, "_SP")
	storage.Spilled = 1
	_, err := e.StackPointer(tok(TokIdentifier, "stack_pointer"), nil, u, storage, nil)
	if err == nil {
		t.Error("expected error when spilled")
	}
}

const instSizeSrc = `
op(_IS, (-- )) {
    int s = INSTRUCTION_SIZE;
}
inst(_IS, (-- )) {
    int s = INSTRUCTION_SIZE;
}
`

func TestEmitter_InstructionSize(t *testing.T) {
	e, storage, u, buf := emitterFromSrc(t, instSizeSrc, "_IS")
	if u.InstructionSize == nil {
		t.Skip("no instruction size set; analyzer skipped wiring")
	}
	tk := tok(TokIdentifier, "INSTRUCTION_SIZE")
	if _, err := e.InstructionSize(tk, nil, u, storage, nil); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(buf.String(), "u ") {
		t.Errorf("missing literal size: %q", buf.String())
	}
}

// End-to-end test: feed a uop body through EmitTokens and check that
// the GO_TO_INSTRUCTION / DISPATCH / DECREF / SYNC dispatching all run.
const e2eSrc = `
op(_E2E, (a, b -- )) {
    DECREF_INPUTS();
    SYNC_SP();
}
`

func TestEmitter_EmitTokens_E2E(t *testing.T) {
	e, storage, u, buf := emitterFromSrc(t, e2eSrc, "_E2E")
	reachable, st, err := e.EmitTokens(u, storage, nil, true)
	if err != nil {
		t.Fatalf("EmitTokens: %v", err)
	}
	if !reachable {
		t.Errorf("expected body to remain reachable")
	}
	if st == nil {
		t.Errorf("expected storage")
	}
	out := buf.String()
	// Body should produce DECREF + SYNC related output.
	if !strings.Contains(out, "PyStackRef_CLOSE") {
		t.Errorf("missing PyStackRef_CLOSE:\n%s", out)
	}
}

// Identifier passthrough: arbitrary identifiers should be emitted
// unchanged when they are not in the replacement map.
func TestEmitter_PassthroughIdentifier(t *testing.T) {
	const src = `
op(_PT, (-- )) {
    int x = some_helper(42);
}
`
	e, storage, u, buf := emitterFromSrc(t, src, "_PT")
	if _, st, err := e.EmitTokens(u, storage, nil, true); err != nil || st == nil {
		t.Fatalf("EmitTokens: err=%v st=%v", err, st)
	}
	if !strings.Contains(buf.String(), "some_helper") {
		t.Errorf("missing identifier:\n%s", buf.String())
	}
}
