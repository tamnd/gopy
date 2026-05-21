// Action body translator (B6). Walks the raw token slice that survived
// from the parser and tries to render a Go body for the dispatch arm.
//
// The translator is opportunistic, mirroring parser_gen's approach: it
// understands a fixed set of control macros and a small calling
// convention for `_Py*` helpers, and bails out to a panic-stub for
// anything it cannot type. As the typed object surface lands, more
// shapes flip from panic-stub to real translation without touching the
// generator's other stages.
//
// CPython: Tools/cases_generator/generators_common.py.

package main

import (
	"fmt"
	"strings"
)

// TranslateBody renders the action body for one analyzed instruction.
// Returns ok=false (with a non-nil note) when a shape is not yet
// supported, so the emitter can fall back to a panic-stub arm.
//
// terminates=true means the rendered body already emits its own
// dispatch return (e.g. JUMPBY); the emitter must not append the
// default `return e.advance()` tail in that case.
//
// The output is a Go statement block (no surrounding braces); the
// caller indents and wraps it.
func TranslateBody(body []dslTok, sig *SignatureAnalysis) (goSrc string, terminates, ok bool, note string) {
	// Outputs with a Sized variadic shape or an explicit :type
	// annotation need the typed helper layer; we only translate the
	// simple stack-ref outputs today.
	for _, o := range sig.Outputs {
		// A sized output that aliases a sized input (same name, same
		// SizeExpr, same index) is handled by the emitter: the slot
		// rides through on the input's backing slice and the body
		// never names it. A non-passthrough sized output (e.g.
		// UNPACK_EX's `unused[oparg & 0xFF]`) still needs a typed
		// helper that we have not written yet.
		if o.Sized && !o.Passthrough {
			return "", false, false, "non-passthrough sized output not yet handled by action translator"
		}
		if o.Type != "" {
			return "", false, false, fmt.Sprintf("typed output %q not yet handled by action translator", o.Type)
		}
	}
	t := &actionTranslator{
		sig:          sig,
		bound:        bindNames(sig),
		toks:         stripWhitespace(body),
		writer:       &strings.Builder{},
		assigned:     map[string]bool{},
		locals:       map[string]bool{},
		intLocals:    map[string]bool{},
		boolLocals:   map[string]bool{},
		excInfoAlias: map[string]bool{},
	}
	// Passthrough outputs: when an output reuses an input name, CPython's
	// analyzer treats the slot as already populated (the input ref flows
	// to the output position). Mark those names as pre-assigned so the
	// "never assigned" guard below doesn't fire on idiomatic shapes like
	// `inst(TO_BOOL_BOOL, (unused/1, unused/2, value -- value))`.
	//
	// CPython: Tools/cases_generator/analyzer.py Instruction.is_passthrough.
	inputNames := map[string]bool{}
	for _, in := range sig.Inputs {
		if in.Name != "" && in.Name != "unused" {
			inputNames[in.Name] = true
		}
	}
	for _, o := range sig.Outputs {
		if inputNames[o.Name] {
			t.assigned[o.Name] = true
		}
	}
	if err := t.run(); err != nil {
		return "", false, false, err.Error()
	}
	// Every output must have been assigned by the body; otherwise the
	// epilogue would push an undefined slot.
	for _, o := range sig.Outputs {
		if o.Name == "" || o.Name == "unused" {
			continue
		}
		if !t.assigned[o.Name] {
			return "", false, false, fmt.Sprintf("output %q never assigned", o.Name)
		}
	}
	return t.writer.String(), t.terminates, true, ""
}

type actionTranslator struct {
	sig        *SignatureAnalysis
	bound      map[string]string // name -> "in"/"out"
	toks       []dslTok
	pos        int
	writer     *strings.Builder
	terminates bool            // body emits its own dispatch return
	assigned   map[string]bool // output names that got assigned
	locals     map[string]bool // C-locals declared in the body
	intLocals  map[string]bool // subset of locals whose C type was `int`
	boolLocals map[string]bool // subset of locals whose Go type is bool (helper-typed)
	nestDepth  int             // brace-block nesting; >0 means inside an if/else body
	// excInfoAlias names a local that was bound to `tstate->exc_info`,
	// the per-thread handled-exception slot. POP_EXCEPT and PUSH_EXC_INFO
	// declare such a local then read or write `<alias>->exc_value`; we
	// translate those references to e.handledException() / setHandledException
	// rather than emitting a real struct alias.
	//
	// CPython: Include/internal/pycore_runtime.h _PyErr_StackItem
	excInfoAlias map[string]bool
}

func (t *actionTranslator) run() error {
	if len(t.toks) == 0 {
		return nil
	}
	// Statement-level walker. We only recognize a handful of
	// statement shapes today; anything else trips the fallback.
	for t.pos < len(t.toks) {
		if err := t.translateStmt(); err != nil {
			return err
		}
	}
	return nil
}

func (t *actionTranslator) translateStmt() error {
	tk := t.toks[t.pos]
	switch tk.Text {
	case "DEOPT_IF":
		return t.translateControlIf("return e.deoptHere()")
	case "ERROR_IF":
		return t.translateErrorIf()
	case "ERROR_NO_POP":
		// CPython macro: same effect as ERROR_IF(true) — unconditional
		// jump to the per-instruction error label. Appears inside if
		// blocks that have already prepared tstate->current_exception.
		//
		// CPython: Python/ceval_macros.h ERROR_NO_POP.
		return t.translateErrorNoPop()
	case "if":
		return t.translateIfStmt()
	case "EXIT_IF":
		return t.translateControlIf("return e.exitTrace()")
	case "DECREF_INPUTS":
		return t.translateNullary(fmt.Sprintf("e.decrefInputs(%d)", len(t.sig.Inputs)))
	case "INPUTS_DEAD":
		return t.translateNullary("// INPUTS_DEAD: no-op in refcount-only path")
	case "STAT_INC", "STAT_DEC":
		return t.skipParenthesised()
	case "DEAD":
		// DEAD(name) marks a name as no longer referenced after this
		// point. The refcount-only path has nothing to do; the stack
		// slot for the input has already been popped by the prologue.
		return t.skipParenthesised()
	case "INSTRUMENTED_JUMP":
		// Monitoring callback emit. v0.6 has no monitoring path, so the
		// macro collapses to a no-op; the surrounding arm still needs
		// to commit its real stack effect.
		//
		// CPython: Python/ceval_macros.h INSTRUMENTED_JUMP.
		return t.skipParenthesised()
	case "SYNC_SP", "DISPATCH", "DISPATCH_GOTO", "DISPATCH_SAME_OPARG",
		"ADVANCE_ADAPTIVE_COUNTER", "PAUSE_ADAPTIVE_COUNTER",
		"RECORD_BRANCH_TAKEN":
		// Cache/dispatch macros that are no-ops for v0.6 (no
		// specializer, no computed-goto). The eval loop drives every
		// dispatch through the main switch, so DISPATCH() is implicit.
		return t.skipParenthesised()
	case "assert":
		// CPython sprinkles assert(invariant) through bytecodes; Go has
		// no compiled-out assert. The body's behavior holds regardless,
		// so consume `assert(...)` and any trailing `;` and continue.
		return t.skipParenthesised()
	case "JUMPBY":
		return t.translateJumpBy()
	case "Py_XSETREF":
		// Py_XSETREF(slot, value): set slot to value, dropping the
		// previous content. The only slot we model is the per-thread
		// handled-exception slot (exc_info->exc_value); the alias was
		// recorded by translateTypedDecl when CPython introduced the
		// `_PyErr_StackItem *NAME = tstate->exc_info` local. Translate
		// to e.setHandledException(<value>), peeling off the conditional
		// `IsNone(x) ? NULL : Steal(x)` shape that POP_EXCEPT uses.
		//
		// CPython: Include/refcount.h Py_XSETREF
		// CPython: Python/bytecodes.c POP_EXCEPT
		return t.translateExcInfoXSetRef()
	case "STACKREFS_TO_PYOBJECTS":
		// CPython macro that materializes a borrowed PyObject* array
		// alongside a stack-ref array. Under gopy stackref.Ref already
		// wraps an Object, so the materialization is a simple slice
		// build through a helper. Declares the output name as a local
		// of type []objects.Object.
		//
		// CPython: Python/ceval_macros.h STACKREFS_TO_PYOBJECTS
		return t.translateStackrefsToPyObjects()
	case "STACKREFS_TO_PYOBJECTS_CLEANUP":
		// Cleanup macro: drops a refcount on every PyObject* the
		// materialization step would have added. gopy has GC, so the
		// cleanup is a no-op. Consume the call and emit a comment.
		//
		// CPython: Python/ceval_macros.h STACKREFS_TO_PYOBJECTS_CLEANUP
		return t.translateStmtNoop("STACKREFS_TO_PYOBJECTS_CLEANUP")
	case "PyStackRef_CLOSE", "PyStackRef_XCLOSE":
		// Stack-ref close: drop the runtime ref. CLOSE asserts non-null;
		// XCLOSE tolerates null. Both map to .Close() on the popped
		// local, since stackref.Ref's Close already null-checks.
		return t.translateUnaryCall(".Close()")
	case "GETLOCAL":
		// `GETLOCAL(oparg) = expr;` — CPython uses GETLOCAL as both
		// rvalue (handled in expression parsing) and lvalue (handled
		// here). The lvalue case translates to e.setLocal.
		return t.translateGetlocalAssign()
	case "(":
		// `(void)expr;` cast-to-void noop. CPython sprinkles these to
		// silence unused-variable warnings (e.g. `(void)this_instr;`
		// in INSTRUMENTED_NOT_TAKEN). In Go we already mark every
		// declared local with `_ = name`, so the cast has no analogue.
		return t.translateVoidCast()
	}
	// Statement-level error setters. CPython spells these as
	// `PyErr_Format(tstate, type, "fmt", args...);` or similar; their
	// only effect is to stash an exception on the thread state. Under
	// the refcount-only path the message text is informational, so we
	// translate to a generic pendingErr stash and let the surrounding
	// `ERROR_NO_POP()` / `ERROR_IF(...)` carry the failure.
	//
	// CPython: Python/errors.c PyErr_Format and friends.
	if stmtErrSetters[tk.Text] {
		return t.translateStmtErrSetter(tk.Text)
	}
	if stmtNoopCalls[tk.Text] {
		return t.translateStmtNoop(tk.Text)
	}
	if h, ok := stmtCallHelpers[tk.Text]; ok {
		return t.translateStmtCall(tk.Text, h)
	}
	// Generic C-local declaration: `<type-prefix> name = expr;` or
	// `<type-prefix> * name = expr;`. The set of accepted prefixes lives
	// in cTypeDecls below. Go infers the type from the rhs so we drop
	// the prefix entirely; the only thing we need is the name and the
	// expression.
	if cTypeDecls[tk.Text] {
		return t.translateTypedDecl()
	}
	// `<exc_info_alias>->exc_value = EXPR;` writes the per-thread
	// handled-exception slot. The alias was bound earlier by
	// translateTypedDecl. PUSH_EXC_INFO uses this lvalue form (rather
	// than Py_XSETREF) once it's already saved the previous value.
	//
	// CPython: Python/bytecodes.c PUSH_EXC_INFO
	if t.pos+4 < len(t.toks) &&
		t.excInfoAlias[t.toks[t.pos].Text] &&
		t.toks[t.pos+1].Text == "->" &&
		t.toks[t.pos+2].Text == "exc_value" &&
		t.toks[t.pos+3].Text == "=" {
		return t.translateExcInfoAssign()
	}
	// Assignment to a bound slot. Outputs need this for the obvious
	// `result = something;` pattern; passthrough inputs need it for
	// in-place rewrites like SWAP's `bottom = top;`. The Go-level
	// write-back is the same in both cases (`name = expr`); the arm
	// epilogue handles the setPeek commit for passthroughs.
	if isBareIdent(tk.Text) && t.bound[tk.Text] != "" {
		return t.translateOutputAssign(tk.Text)
	}
	// Assignment to a previously declared local. CPython routinely
	// reuses `int err;` followed by branchy `err = helper(...)` legs;
	// the Go translation is just `err = ...` since the local already
	// exists from the typed-decl prologue.
	if isBareIdent(tk.Text) && t.locals[tk.Text] {
		return t.translateLocalAssign(tk.Text)
	}
	return fmt.Errorf("unrecognized token at action body start: %q", tk.Text)
}

// cTypeDecls is the set of C type-name tokens we accept as the start of
// a `<type> [*] name = expr;` declaration. Go's `:=` infers the actual
// type from the rhs so we drop the prefix; the only thing we need is
// the name. Buckets A2 (int / bool), A5 (uint32_t), A7 (typed pointers
// like PyFunctionObject *) all funnel through here.
var cTypeDecls = map[string]bool{
	"_PyStackRef":        true,
	"PyObject":           true,
	"int":                true,
	"uint8_t":            true,
	"uint32_t":           true,
	"size_t":             true,
	"Py_ssize_t":         true,
	"Py_hash_t":          true,
	"PyTypeObject":       true,
	"PyFunctionObject":   true,
	"PyCodeObject":       true,
	"PyCellObject":       true,
	"PyListObject":       true,
	"PyDictObject":       true,
	"PySetObject":        true,
	"PyGenObject":        true,
	"PyCoroObject":       true,
	"PyLongObject":       true,
	"PyUnicodeObject":    true,
	"PyTupleObject":      true,
	"PyInterpreterState": true,
	"_PyErr_StackItem":   true,
	"conversion_func":    true,
	"unaryfunc":          true,
}

// translateTypedDecl handles `<C-type> [*] NAME = EXPR;`. Go infers the
// resulting variable's type from the rhs, so we drop the prefix and emit
// `NAME := <go-expr>`. The name is recorded as a declared local so later
// references resolve.
func (t *actionTranslator) translateTypedDecl() error {
	prefix := t.toks[t.pos].Text
	t.pos++
	// Optional pointer star: `PyObject *name` and friends.
	if t.pos < len(t.toks) && t.toks[t.pos].Text == "*" {
		t.pos++
	}
	if t.pos >= len(t.toks) {
		return fmt.Errorf("expected name after %s", prefix)
	}
	name := t.toks[t.pos].Text
	if !isBareIdent(name) {
		return fmt.Errorf("expected identifier after %s, got %q", prefix, name)
	}
	t.pos++
	// `<type> [*] name ;` — uninitialized declaration. CPython relies
	// on these for "I will assign later (often via & out-param)"
	// patterns. Translate to a zero-valued Go var so subsequent
	// statements can read or write the slot.
	if t.pos < len(t.toks) && t.toks[t.pos].Text == ";" {
		t.acceptSemi()
		t.locals[name] = true
		goType := goTypeForPrefix(prefix)
		if goType == "int32" {
			t.intLocals[name] = true
		}
		fmt.Fprintf(t.writer, "var %s %s\n", goLocalName(name), goType)
		fmt.Fprintf(t.writer, "_ = %s\n", goLocalName(name))
		return nil
	}
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "=" {
		return fmt.Errorf("expected '=' after %s %s", prefix, name)
	}
	t.pos++
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	// `_PyErr_StackItem *NAME = tstate->exc_info;` binds NAME to the
	// per-thread handled-exception slot. CPython then reads or writes
	// `NAME->exc_value` to peek at / install the active handler. We do
	// not model the C struct in Go; instead we record the alias so
	// later reads/writes route through the e.handledException /
	// setHandledException helpers in eval_helpers.go.
	//
	// CPython: Python/bytecodes.c POP_EXCEPT, PUSH_EXC_INFO
	if prefix == "_PyErr_StackItem" && len(rhs) == 3 &&
		rhs[0] == "tstate" && rhs[1] == "->" && rhs[2] == "exc_info" {
		t.excInfoAlias[name] = true
		// Mark the alias as a local so parsePrimary accepts bare
		// references to it; the postfix `->` loop intercepts the
		// only legal use (NAME->exc_value) before any Go code is
		// emitted for the bare name itself.
		t.locals[name] = true
		return nil
	}
	// Out-param helper call: `int err = HELPER(args..., &out);`. CPython
	// uses this shape for PyMapping_GetOptionalItem and friends — a
	// helper that returns an int status (-1 / 0 / 1 in CPython, error /
	// not-found / found here) and writes the looked-up value into a
	// previously declared PyObject* slot. The Go signature returns the
	// status and the value as a pair, so the emission is a multi-assign
	// against an already-declared local.
	//
	// CPython: Objects/abstract.c PyMapping_GetOptionalItem
	if prefix == "int" && len(rhs) >= 5 {
		if h, ok := outParamHelpers[rhs[0]]; ok && rhs[1] == "(" && rhs[len(rhs)-1] == ")" {
			// Trailing `, & IDENT` is the out-param. Strip it before
			// translating the remaining args.
			if rhs[len(rhs)-3] == "&" && isBareIdent(rhs[len(rhs)-2]) && rhs[len(rhs)-4] == "," {
				outName := rhs[len(rhs)-2]
				if t.locals[outName] {
					inner := rhs[2 : len(rhs)-4]
					args, err := splitTopLevelArgs(inner)
					if err != nil {
						return fmt.Errorf("%s %s out-param helper: %w", prefix, name, err)
					}
					if len(args) != h.arity {
						return fmt.Errorf("%s expects %d args, got %d", rhs[0], h.arity, len(args))
					}
					argExprs := make([]string, len(args))
					for i, a := range args {
						ex, err := t.translateExpr(a)
						if err != nil {
							return fmt.Errorf("%s arg %d: %w", rhs[0], i, err)
						}
						argExprs[i] = ex
					}
					t.locals[name] = true
					t.intLocals[name] = true
					// In a nested block (if/else body) Go's `:=` would
					// shadow the outer outName instead of writing through
					// it. Emit a separate `var err int32` declaration and
					// a plain `=` multi-assign so the outer slot is the
					// one updated. CPython: a fresh `int err` inside a
					// braced block in C declares a new err but still
					// writes the outer PyObject *out via `&out`.
					if t.nestDepth > 0 {
						fmt.Fprintf(t.writer, "var %s int32\n", goLocalName(name))
						fmt.Fprintf(t.writer, "%s, %s = %s(%s)\n",
							goLocalName(outName), goLocalName(name),
							h.goExpr, strings.Join(argExprs, ", "))
					} else {
						fmt.Fprintf(t.writer, "%s, %s := %s(%s)\n",
							goLocalName(outName), goLocalName(name),
							h.goExpr, strings.Join(argExprs, ", "))
					}
					fmt.Fprintf(t.writer, "_ = %s\n", goLocalName(name))
					fmt.Fprintf(t.writer, "_ = %s\n", goLocalName(outName))
					return nil
				}
			}
		}
	}
	// Ternary RHS expands the same way as in translateOutputAssign: Go
	// has no ?: operator, so emit a zero-valued declaration and an
	// if/else write-back. CPython sprinkles ternaries in typed decls
	// (BUILD_SLICE's `oparg == 3 ? ... : NULL`, RAISE_VARARGS's
	// `oparg == 2 ? ... : NULL`).
	if condToks, aToks, bToks, isTern := splitTopLevelTernary(rhs); isTern {
		condExpr, err := t.translateExpr(condToks)
		if err != nil {
			return fmt.Errorf("%s %s ternary cond: %w", prefix, name, err)
		}
		if len(condToks) == 1 && t.intLocals[condToks[0]] {
			condExpr = condExpr + " != 0"
		}
		aExpr, err := t.translateExpr(aToks)
		if err != nil {
			return fmt.Errorf("%s %s ternary then: %w", prefix, name, err)
		}
		bExpr, err := t.translateExpr(bToks)
		if err != nil {
			return fmt.Errorf("%s %s ternary else: %w", prefix, name, err)
		}
		t.locals[name] = true
		goType := goTypeForPrefix(prefix)
		fmt.Fprintf(t.writer, "var %s %s\n", goLocalName(name), goType)
		fmt.Fprintf(t.writer, "if %s { %s = %s } else { %s = %s }\n",
			condExpr, goLocalName(name), aExpr, goLocalName(name), bExpr)
		fmt.Fprintf(t.writer, "_ = %s\n", goLocalName(name))
		return nil
	}
	rhsExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("%s %s rhs: %w", prefix, name, err)
	}
	t.locals[name] = true
	if prefix == "int" || prefix == "uint8_t" || prefix == "uint32_t" || prefix == "size_t" || prefix == "Py_ssize_t" || prefix == "Py_hash_t" {
		// `int jump = PyStackRef_IsFalse(cond);` is idiomatic CPython
		// for "store a 0/1 from a predicate". The Go translation
		// returns a real bool, so dont mark the local as int (which
		// would otherwise append `!= 0` to later boolean uses).
		if !rhsIsBool(rhsExpr) {
			t.intLocals[name] = true
		} else {
			t.boolLocals[name] = true
		}
	}
	fmt.Fprintf(t.writer, "%s := %s\n", goLocalName(name), rhsExpr)
	fmt.Fprintf(t.writer, "_ = %s\n", goLocalName(name))
	return nil
}

// stmtErrSetters is the set of CPython helpers that only set the
// thread exception state. Translated as a generic pendingErr stash
// since the actual format string is informational and the surrounding
// ERROR_NO_POP / ERROR_IF carries the bail.
var stmtErrSetters = map[string]bool{
	"PyErr_Format":              true,
	"_PyErr_Format":             true,
	"_PyErr_SetString":          true,
	"_PyErr_SetKeyError":        true,
	"_PyEval_FormatExcUnbound":  true,
	"_PyEval_FormatExcCheckArg": true,
	"_PyEval_FormatKwargsError": true,
	"Py_FatalError":             true,
}

// translateStmtErrSetter consumes `NAME(args...);` and emits a
// pendingErr stash. The args themselves are discarded since the
// translated wrapper synthesises a generic error.
func (t *actionTranslator) translateStmtErrSetter(name string) error {
	t.pos++ // helper name
	inner, err := t.takeParenthesised()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	t.acceptSemi()
	// Py_FatalError is the "abort the interpreter" macro; treat it as
	// a hard panic so the surrounding epilogue can't reach.
	if name == "Py_FatalError" {
		fmt.Fprintf(t.writer, "panic(%q)\n", "vm: Py_FatalError")
		if t.nestDepth == 0 {
			t.terminates = true
		}
		return nil
	}
	// Carry the string literal through to pendingErr when the helper
	// has the standard shape `NAME(tstate, PyExc_Foo, "literal")`. The
	// formatted message is informational, but the surrounding code
	// (and gate-skip logic in tests) reads it to decide whether the
	// failure is a known gap or a real regression.
	exc := extractExceptionName(inner)
	if lit, ok := extractLastStringLiteral(inner); ok {
		if exc != "" {
			fmt.Fprintf(t.writer, "e.setPendingErr(%q)\n", exc+": "+lit)
		} else {
			fmt.Fprintf(t.writer, "e.setPendingErr(%q)\n", lit)
		}
		return nil
	}
	// No string literal found, but a PyExc_<Name> token in the args is
	// enough to label the failure so the test gate-skip logic can
	// classify it (e.g. NameError vs a real regression).
	if exc != "" {
		fmt.Fprintf(t.writer, "e.setPendingErr(%q)\n", exc)
		return nil
	}
	fmt.Fprintf(t.writer, "e.setPendingErr(%q)\n", name)
	return nil
}

// extractExceptionName scans args for a `PyExc_<Name>` token and
// returns "<Name>Error" so a translated err setter still carries the
// exception type into pendingErr (e.g. NameError, SystemError). The
// trailing "Error" is dropped if the token already ends with it
// (PyExc_StopIteration -> "StopIteration"). Returns "" when no
// PyExc_* token is present.
func extractExceptionName(s string) string {
	for tok := range strings.FieldsSeq(s) {
		if !strings.HasPrefix(tok, "PyExc_") {
			continue
		}
		// Strip surrounding punctuation (commas left in by the joined
		// arg string).
		tok = strings.TrimRight(tok, ",")
		name := strings.TrimPrefix(tok, "PyExc_")
		if name == "" {
			continue
		}
		// PyExc_NameError, PyExc_SystemError, PyExc_ValueError, etc.
		// already carry the suffix; pass through unchanged. The bare
		// ones (PyExc_StopIteration, PyExc_GeneratorExit) we leave as
		// is so the test logic that matches on the type name still
		// works.
		return name
	}
	return ""
}

// extractLastStringLiteral returns the contents of the last `"..."`
// run that closes the input. Lexer tokens for C string literals carry
// their surrounding quotes verbatim, so a final `"` in the joined
// argument list pairs with the previous `"` and the bracketed slice
// is the literal payload. Returns ok=false when no string literal
// terminates the input.
func extractLastStringLiteral(s string) (string, bool) {
	s = strings.TrimRight(s, " \t")
	if len(s) < 2 || s[len(s)-1] != '"' {
		return "", false
	}
	end := len(s) - 1
	for i := end - 1; i >= 0; i-- {
		if s[i] == '"' && (i == 0 || s[i-1] != '\\') {
			return s[i+1 : end], true
		}
	}
	return "", false
}

// stmtNoopCalls is the set of CPython statement-form macros that are
// refcount-only side effects. Under the GC-backed Go runtime they have
// no analogue. Consume `NAME(args...);` and emit nothing.
//
// CPython: Include/refcount.h Py_INCREF / Py_DECREF / Py_XINCREF /
// Py_XDECREF.
var stmtNoopCalls = map[string]bool{
	"Py_INCREF":  true,
	"Py_DECREF":  true,
	"Py_XINCREF": true,
	"Py_XDECREF": true,
}

func (t *actionTranslator) translateStmtNoop(name string) error {
	t.pos++ // helper name
	if _, err := t.takeParenthesised(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	t.acceptSemi()
	fmt.Fprintf(t.writer, "// %s: no-op under GC\n", name)
	return nil
}

// stmtCallHelpers wires CPython statement-form helpers that DO have a
// matching Go side effect. The arity check and argument rendering reuses
// the expression helper-call machinery (helperCall struct).
var stmtCallHelpers = map[string]helperCall{
	// PyCell_SetTakeRef(cell, value): write a value into a cell,
	// stealing the caller's reference. Maps to a thin evalState method
	// that handles the cell type-check.
	//
	// CPython: Objects/cellobject.c PyCell_SetTakeRef.
	"PyCell_SetTakeRef": {goExpr: "e.cellSetTakeRef", arity: 2},
}

func (t *actionTranslator) translateStmtCall(name string, h helperCall) error {
	t.pos++ // helper name
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "(" {
		return fmt.Errorf("%s: expected '('", name)
	}
	// Reuse the expression-side parser: collect tokens up to the
	// matching ')', then route through translateExpr by prepending the
	// helper name so parsePrimary picks it up as a helper call.
	argToks, err := t.takeParenTokens()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	t.acceptSemi()
	// Reconstruct `NAME(args...)` and translate as an expression. The
	// helperCall registry resolves it to the Go form; statement context
	// just emits the resulting expression as a bare call.
	reconstructed := []string{name, "("}
	reconstructed = append(reconstructed, argToks...)
	reconstructed = append(reconstructed, ")")
	// Register a temporary expression-side entry so parsePrimary finds
	// it. To avoid mutating the global map mid-parse, build the parser
	// with a per-call helper.
	expr, err := t.translateExprWithHelper(reconstructed, name, h)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Fprintf(t.writer, "%s\n", expr)
	return nil
}

// translateExprWithHelper translates an expression with a single extra
// helper-call entry registered. Used by translateStmtCall to fall through
// the existing expression infrastructure without polluting the global
// helperCalls map.
func (t *actionTranslator) translateExprWithHelper(toks []string, name string, h helperCall) (string, error) {
	// helperCalls is keyed by name, and parsePrimary looks it up
	// directly. Since the map is package-global, temporarily add the
	// entry, parse, then remove it. The translator runs single-threaded
	// per generation pass, so the mutation is safe.
	if _, exists := helperCalls[name]; !exists {
		helperCalls[name] = h
		defer delete(helperCalls, name)
	}
	return t.translateExpr(toks)
}

// rhsIsBool returns true when the translated Go RHS resolves to a bool
// rather than an int. CPython conflates the two in `int err = cond` but
// our generated wrappers return real bools for predicate helpers.
func rhsIsBool(expr string) bool {
	for _, s := range []string{".IsFalse()", ".IsTrue()", ".IsNone()", ".IsNull()"} {
		if strings.Contains(expr, s) {
			return true
		}
	}
	if strings.HasPrefix(expr, "!") {
		return true
	}
	// objects.IsExact* and friends are bool-returning helpers, wired
	// in helperCallRegistry. Any helper whose Go expression starts
	// with `objects.Is` returns bool.
	if strings.HasPrefix(expr, "objects.Is") {
		return true
	}
	// Predicate helpers on the eval state. Each returns Go bool.
	for _, s := range []string{"e.errOccurred(", "e.errExceptionMatches("} {
		if strings.HasPrefix(expr, s) {
			return true
		}
	}
	return false
}

// goTypeForPrefix maps a CPython type-name token to the Go type used
// when emitting an uninitialized declaration. Sized integer prefixes
// all funnel to int32 because that is what helper wrappers return for
// CPython's "int err" / "Py_ssize_t" failure signals; everything else
// falls back to objects.Object since the surface only really cares
// about PyObject*-shaped slots.
func goTypeForPrefix(prefix string) string {
	switch prefix {
	case "int", "uint8_t", "uint32_t", "size_t", "Py_ssize_t", "Py_hash_t":
		return "int32"
	default:
		return "objects.Object"
	}
}

// translateGetlocalAssign handles `GETLOCAL(<idx>) = <expr>;`. The
// LHS macro doubles as an lvalue in CPython; we route it through the
// e.setLocal helper which already handles closing the previous slot.
func (t *actionTranslator) translateGetlocalAssign() error {
	t.pos++ // GETLOCAL
	idxToks, err := t.takeParenTokens()
	if err != nil {
		return err
	}
	idxExpr, err := t.translateExprFromStrings(idxToks)
	if err != nil {
		return fmt.Errorf("GETLOCAL lvalue index: %w", err)
	}
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "=" {
		return fmt.Errorf("expected '=' after GETLOCAL(...) lvalue")
	}
	t.pos++
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	rhsExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("GETLOCAL lvalue rhs: %w", err)
	}
	fmt.Fprintf(t.writer, "e.setLocal(int(%s), %s)\n", idxExpr, rhsExpr)
	return nil
}

// takeParenTokens is the token-slice analogue of takeParenthesised:
// returns the raw inner tokens (still as text) so they can be fed back
// into translateExpr without losing structure.
func (t *actionTranslator) takeParenTokens() ([]string, error) {
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "(" {
		return nil, fmt.Errorf("expected '(' at position %d", t.pos)
	}
	t.pos++
	depth := 1
	out := []string{}
	for t.pos < len(t.toks) && depth > 0 {
		tk := t.toks[t.pos]
		switch tk.Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				t.pos++
				return out, nil
			}
		}
		out = append(out, tk.Text)
		t.pos++
	}
	return nil, fmt.Errorf("unterminated '(' at position %d", t.pos)
}

// translateExprFromStrings is the version of translateExpr that takes
// raw token strings (already split by the caller).
func (t *actionTranslator) translateExprFromStrings(toks []string) (string, error) {
	return t.translateExpr(toks)
}

// translateOutputAssign handles `name = expr ;` where name is a
// declared output of the instruction. The right-hand side is parsed
// from a small vocabulary of CPython expressions; unrecognized shapes
// bail so the emitter can fall back to the panic-stub.
func (t *actionTranslator) translateOutputAssign(name string) error {
	t.pos++ // name
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "=" {
		return fmt.Errorf("expected '=' after output name %q", name)
	}
	t.pos++ // =
	// Collect the RHS up to the semicolon.
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	// Ternary expansion: `name = cond ? a : b;` becomes an if/else
	// pair in Go since Go has no ternary operator. The split is
	// top-level only so nested parens/calls stay intact.
	if condToks, aToks, bToks, isTern := splitTopLevelTernary(rhs); isTern {
		condExpr, err := t.translateExpr(condToks)
		if err != nil {
			return fmt.Errorf("output assign %q ternary cond: %w", name, err)
		}
		// C int doubles as a boolean. When the condition reduces to a
		// single int-typed local, emit the explicit `!= 0` Go needs.
		if len(condToks) == 1 && t.intLocals[condToks[0]] {
			condExpr = condExpr + " != 0"
		}
		aExpr, err := t.translateExpr(aToks)
		if err != nil {
			return fmt.Errorf("output assign %q ternary then: %w", name, err)
		}
		bExpr, err := t.translateExpr(bToks)
		if err != nil {
			return fmt.Errorf("output assign %q ternary else: %w", name, err)
		}
		fmt.Fprintf(t.writer, "if %s { %s = %s } else { %s = %s }\n",
			condExpr, goLocalName(name), aExpr, goLocalName(name), bExpr)
		t.assigned[name] = true
		return nil
	}
	goExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("output assign %q: %w", name, err)
	}
	fmt.Fprintf(t.writer, "%s = %s\n", goLocalName(name), goExpr)
	t.assigned[name] = true
	return nil
}

// translateLocalAssign handles `name = expr;` for a previously declared
// local. Mirrors translateOutputAssign but does not mark the slot as an
// "output" — locals do not participate in the post-body push epilogue.
func (t *actionTranslator) translateLocalAssign(name string) error {
	t.pos++ // name
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "=" {
		return fmt.Errorf("expected '=' after local name %q", name)
	}
	t.pos++ // =
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	goExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("local assign %q: %w", name, err)
	}
	fmt.Fprintf(t.writer, "%s = %s\n", goLocalName(name), goExpr)
	return nil
}

// translateExcInfoXSetRef handles `Py_XSETREF(ALIAS->exc_value, EXPR);`
// where ALIAS was bound to tstate->exc_info. EXPR is usually a ternary
// `cond ? NULL : steal_expr` (POP_EXCEPT) so we expand to an if/else
// pair around setHandledException. A non-ternary EXPR (or the bare
// NULL value) just funnels to a single setHandledException call.
//
// CPython: Python/bytecodes.c POP_EXCEPT
func (t *actionTranslator) translateExcInfoXSetRef() error {
	t.pos++ // Py_XSETREF
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "(" {
		return fmt.Errorf("Py_XSETREF: expected '('")
	}
	t.pos++
	// First arg: ALIAS -> exc_value.
	if t.pos+2 >= len(t.toks) ||
		!t.excInfoAlias[t.toks[t.pos].Text] ||
		t.toks[t.pos+1].Text != "->" ||
		t.toks[t.pos+2].Text != "exc_value" {
		return fmt.Errorf("Py_XSETREF: only exc_info->exc_value supported")
	}
	t.pos += 3
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "," {
		return fmt.Errorf("Py_XSETREF: expected ',' after slot")
	}
	t.pos++
	// Second arg: collect tokens until the matching ')'.
	depth := 1
	var rhs []string
	for t.pos < len(t.toks) {
		tk := t.toks[t.pos].Text
		if tk == "(" {
			depth++
		} else if tk == ")" {
			depth--
			if depth == 0 {
				break
			}
		}
		rhs = append(rhs, tk)
		t.pos++
	}
	if t.pos >= len(t.toks) {
		return fmt.Errorf("Py_XSETREF: unterminated arglist")
	}
	t.pos++ // consume ')'
	t.acceptSemi()
	// Bare NULL: clear the slot.
	if len(rhs) == 1 && rhs[0] == "NULL" {
		fmt.Fprintln(t.writer, "e.setHandledException(nil)")
		return nil
	}
	if condToks, aToks, bToks, isTern := splitTopLevelTernary(rhs); isTern {
		condExpr, err := t.translateExpr(condToks)
		if err != nil {
			return fmt.Errorf("Py_XSETREF cond: %w", err)
		}
		aExpr, err := excInfoSetExpr(t, aToks)
		if err != nil {
			return fmt.Errorf("Py_XSETREF then: %w", err)
		}
		bExpr, err := excInfoSetExpr(t, bToks)
		if err != nil {
			return fmt.Errorf("Py_XSETREF else: %w", err)
		}
		fmt.Fprintf(t.writer, "if %s {\n%s\n} else {\n%s\n}\n", condExpr, aExpr, bExpr)
		return nil
	}
	expr, err := excInfoSetExpr(t, rhs)
	if err != nil {
		return err
	}
	fmt.Fprintln(t.writer, expr)
	return nil
}

// excInfoSetExpr renders one Py_XSETREF arm. NULL clears the slot; any
// other expression evaluates to objects.Object and routes through
// setHandledException.
func excInfoSetExpr(t *actionTranslator, toks []string) (string, error) {
	if len(toks) == 1 && toks[0] == "NULL" {
		return "e.setHandledException(nil)", nil
	}
	goExpr, err := t.translateExpr(toks)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("e.setHandledException(%s)", goExpr), nil
}

// translateExcInfoAssign handles `ALIAS->exc_value = EXPR;` for an
// alias that was bound to tstate->exc_info. PUSH_EXC_INFO uses this
// form (no Py_XSETREF) once it's already saved the previous slot.
//
// CPython: Python/bytecodes.c PUSH_EXC_INFO
func (t *actionTranslator) translateExcInfoAssign() error {
	t.pos += 4 // ALIAS -> exc_value =
	rhs := []string{}
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		rhs = append(rhs, t.toks[t.pos].Text)
		t.pos++
	}
	t.acceptSemi()
	if len(rhs) == 1 && rhs[0] == "NULL" {
		fmt.Fprintln(t.writer, "e.setHandledException(nil)")
		return nil
	}
	goExpr, err := t.translateExpr(rhs)
	if err != nil {
		return fmt.Errorf("exc_info assign: %w", err)
	}
	fmt.Fprintf(t.writer, "e.setHandledException(%s)\n", goExpr)
	return nil
}

// splitTopLevelArgs splits a comma-separated argument list (already
// stripped of the enclosing parens) into per-arg token slices, honoring
// nested parens/brackets so a comma inside a sub-call does not split
// the outer arg.
func splitTopLevelArgs(toks []string) ([][]string, error) {
	var out [][]string
	depth := 0
	start := 0
	for i, tk := range toks {
		switch tk {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced %q", tk)
			}
		case ",":
			if depth == 0 {
				out = append(out, toks[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced parens in arg list")
	}
	if start < len(toks) {
		out = append(out, toks[start:])
	}
	return out, nil
}

// outParamHelpers is the registry of CPython helpers whose call shape is
// `int err = HELPER(in1, ..., inN, &out);` — they return a status int
// and write the value through an out-pointer. The Go wrappers return
// (value, status) so the translator can emit a multi-assign:
//
//	out, err := e.helper(in1, ..., inN)
//
// CPython: Objects/abstract.c PyMapping_GetOptionalItem (and similar)
var outParamHelpers = map[string]helperCall{
	// PyMapping_GetOptionalItem(obj, key, &out) -> int (1 found / 0
	// missing / -1 error). gopy stashes the error on pendingErr; the
	// wrapper returns the value and an int matching CPython's contract.
	//
	// CPython: Objects/abstract.c:207 PyMapping_GetOptionalItem
	"PyMapping_GetOptionalItem": {goExpr: "e.mappingGetOptionalItem", arity: 2},
}

// splitTopLevelTernary scans `cond ? a : b` and returns the three
// token slices. Operators inside parens/brackets are ignored. Returns
// ok=false when no top-level `?` is present.
func splitTopLevelTernary(toks []string) (cond, a, b []string, ok bool) {
	depth := 0
	qPos := -1
	for i, tk := range toks {
		switch tk {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case "?":
			if depth == 0 && qPos < 0 {
				qPos = i
			}
		}
	}
	if qPos < 0 {
		return nil, nil, nil, false
	}
	depth = 0
	cPos := -1
	for i := qPos + 1; i < len(toks); i++ {
		switch toks[i] {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ":":
			if depth == 0 {
				cPos = i
			}
		}
		if cPos >= 0 {
			break
		}
	}
	if cPos < 0 {
		return nil, nil, nil, false
	}
	return toks[:qPos], toks[qPos+1 : cPos], toks[cPos+1:], true
}

// translateExpr renders a small CPython expression vocabulary into Go.
// The vocabulary grows as more opcode bodies migrate. The parser is a
// tiny recursive-descent walker over the token stream.
func (t *actionTranslator) translateExpr(toks []string) (string, error) {
	p := &exprParser{toks: toks, bound: t.bound, locals: t.locals, excInfoAlias: t.excInfoAlias}
	out, err := p.parse()
	if err != nil {
		return "", err
	}
	if p.pos != len(toks) {
		return "", fmt.Errorf("trailing tokens after expression: %q", strings.Join(toks[p.pos:], " "))
	}
	return out, nil
}

// exprParser walks a flat token slice and produces a Go expression.
// It only understands the CPython idioms that have been ported so
// far; unknown shapes bail with an error so the emitter falls back to
// a panic-stub.
type exprParser struct {
	toks   []string
	pos    int
	bound  map[string]string
	locals map[string]bool
	// excInfoAlias mirrors actionTranslator.excInfoAlias so the
	// expression layer can rewrite reads of `<alias>->exc_value` into
	// e.handledException() without re-deriving the alias map.
	excInfoAlias map[string]bool
}

func (p *exprParser) parse() (string, error) {
	return p.parseExpr(0)
}

// parseExpr is a Pratt-style binary operator parser. precedence levels
// follow C's: || (1) || (2) && (3) | (4) ^ (5) & (6) == != (7)
// < <= > >= (8) << >> (9) + - (10) * / % (11). Unary - and ! are
// handled inside parsePrimary; precedence 0 is the entry point.
func (p *exprParser) parseExpr(minPrec int) (string, error) {
	lhs, err := p.parsePrimary()
	if err != nil {
		return "", err
	}
	// Postfix `->field` chains. CPython uses `tstate->interp->common_consts`,
	// `frame->stackpointer`, etc., to reach struct fields the opcode body
	// reads or writes. We translate them via a per-segment table: each
	// (tag, field) entry produces a fresh Go expression and a new tag for
	// the next link. Unknown chains bail so the coverage test reports
	// them as a single grouped reason.
	tag := lhs
	for p.pos+1 < len(p.toks) && p.toks[p.pos] == "->" {
		field := p.toks[p.pos+1]
		// `<alias>->exc_value` read where <alias> was bound to
		// `tstate->exc_info` earlier in the body. Route through the
		// runtime accessor; from here the value behaves like any
		// other objects.Object so a following `!= NULL` or
		// `PyStackRef_FromPyObjectSteal(...)` resumes the normal path.
		if field == "exc_value" && p.excInfoAlias[lhs] {
			p.pos += 2
			lhs = "e.handledException()"
			tag = "objects.Object"
			continue
		}
		key := tag + "." + field
		mapping, ok := structArrow[key]
		if !ok {
			return "", fmt.Errorf("unsupported struct arrow %s", key)
		}
		p.pos += 2
		lhs = mapping.goExpr
		tag = mapping.nextTag
	}
	// Postfix `[index]` subscripts. CPython uses these to read elements
	// of sized inputs (`args[0]`) or fixed arrays; in Go the same
	// syntax indexes a slice, so we pass the index through as a Go
	// expression of its own.
	for p.pos < len(p.toks) && p.toks[p.pos] == "[" {
		p.pos++
		idx, err := p.parseExpr(0)
		if err != nil {
			return "", err
		}
		if p.pos >= len(p.toks) || p.toks[p.pos] != "]" {
			return "", fmt.Errorf("expected ']' to close subscript")
		}
		p.pos++
		lhs = lhs + "[" + idx + "]"
	}
	for p.pos < len(p.toks) {
		op := p.toks[p.pos]
		prec, ok := binOpPrec[op]
		if !ok || prec < minPrec {
			break
		}
		p.pos++
		rhs, err := p.parseExpr(prec + 1)
		if err != nil {
			return "", err
		}
		lhs = lhs + " " + op + " " + rhs
	}
	return lhs, nil
}

// binOpPrec lists the C binary operators we accept verbatim. All of
// these spell the same in Go, so the renderer just sandwiches them
// between the rendered operands.
var binOpPrec = map[string]int{
	"||": 1,
	"&&": 3,
	"|":  4,
	"^":  5,
	"&":  6,
	"==": 7, "!=": 7,
	"<": 8, "<=": 8, ">": 8, ">=": 8,
	"<<": 9, ">>": 9,
	"+": 10, "-": 10,
	"*": 11, "/": 11, "%": 11,
}

func (p *exprParser) parsePrimary() (string, error) {
	if p.pos >= len(p.toks) {
		return "", fmt.Errorf("unexpected end of expression")
	}
	tk := p.toks[p.pos]
	p.pos++
	switch tk {
	case "PyStackRef_NULL":
		return "stackref.Null", nil
	case "PyStackRef_True":
		return "stackref.True", nil
	case "PyStackRef_False":
		return "stackref.False", nil
	case "PyStackRef_None":
		return "stackref.None", nil
	case "PyStackRef_TYPE":
		// CPython macro: returns the PyTypeObject* of the boxed object.
		// In bytecodes.c the only call shape we see is
		// `PyStackRef_TYPE(x) -> tp_flags`, used for tp_flags bit-tests
		// in MATCH_MAPPING / MATCH_SEQUENCE. Translate that compound
		// into a single helper that returns the flag bitset as uint64.
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		if p.pos+1 < len(p.toks) && p.toks[p.pos] == "->" && p.toks[p.pos+1] == "tp_flags" {
			p.pos += 2
			return fmt.Sprintf("e.stackrefTypeFlags(%s)", arg), nil
		}
		return "", fmt.Errorf("PyStackRef_TYPE only supported with -> tp_flags")
	case "PyStackRef_DUP", "PyStackRef_Borrow":
		// CPython distinguishes owned (DUP) from borrowed (Borrow) refs;
		// under Go's GC the distinction collapses, so both render the
		// same way.
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".Dup()", nil
	case "PyStackRef_IsNull":
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".IsNull()", nil
	case "PyStackRef_IsTrue":
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".IsTrue()", nil
	case "PyStackRef_IsFalse":
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".IsFalse()", nil
	case "PyStackRef_IsNone":
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".IsNone()", nil
	case "PyStackRef_GenCheck":
		// CPython: Include/internal/pycore_stackref.h PyStackRef_GenCheck
		// wraps PyGen_Check around the stack-ref unwrap.
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return "objects.IsGenerator(" + arg + ".AsObject())", nil
	case "PyStackRef_AsPyObjectBorrow", "PyStackRef_AsPyObjectSteal", "PyStackRef_AsPyObjectNew":
		// CPython distinguishes borrowed from owning extraction. Under
		// Go's GC both collapse to `Ref.AsObject()`.
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return arg + ".AsObject()", nil
	case "PyStackRef_FromPyObjectSteal",
		"PyStackRef_FromPyObjectStealMortal",
		"PyStackRef_FromPyObjectNew",
		"PyStackRef_FromPyObjectNewMortal",
		"PyStackRef_FromPyObjectImmortal",
		"PyStackRef_FromPyObjectBorrow":
		// CPython distinguishes ownership transfer modes; under Go's
		// GC they all collapse to `stackref.FromObject(obj)`.
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return "stackref.FromObject(" + arg + ")", nil
	case "GETLOCAL":
		arg, err := p.parseCallArg()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("e.localAt(int(%s))", arg), nil
	case "GETITEM":
		// CPython macro: GETITEM(FRAME_CO_CONSTS, i) -> co_consts[i],
		// GETITEM(FRAME_CO_NAMES, i) -> co_names[i]. We route them
		// through e.constAt / e.nameAt; the resulting Go expression
		// has type objects.Object, matching the PyObject* lhs.
		//
		// CPython: Python/ceval_macros.h GETITEM.
		return p.parseGetItemCall()
	case "GLOBALS", "BUILTINS", "LOCALS":
		// Zero-arg frame-namespace macros from Python/ceval_macros.h.
		// They expand to f_globals / f_builtins / f_locals; gopy mirrors
		// each as a one-line evalState accessor.
		if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
			return "", fmt.Errorf("expected '(' after %s", tk)
		}
		p.pos++
		if p.pos >= len(p.toks) || p.toks[p.pos] != ")" {
			return "", fmt.Errorf("expected ')' to close %s()", tk)
		}
		p.pos++
		switch tk {
		case "GLOBALS":
			return "e.globals()", nil
		case "BUILTINS":
			return "e.builtinsDict()", nil
		default:
			return "e.localsDict()", nil
		}
	}
	// Helper-call vocabulary. Each entry maps a CPython helper name to
	// a Go expression template and an argument arity. The translator
	// renders the call as a method on the eval state so the helper
	// owns the failure mode (pendingErr) instead of inventing a
	// per-call error path.
	if h, ok := helperCalls[tk]; ok {
		return p.parseHelperCall(h)
	}
	// Parenthesised subexpression. Also recognise a C-style cast of the
	// form `(TypeName *)operand` and discard it: under Go's interface
	// representation every object already presents as objects.Object,
	// so the cast has no Go-side analogue.
	if tk == "(" {
		if p.pos+2 < len(p.toks) && cTypeDecls[p.toks[p.pos]] && p.toks[p.pos+1] == "*" && p.toks[p.pos+2] == ")" {
			p.pos += 3
			return p.parsePrimary()
		}
		inner, err := p.parseExpr(0)
		if err != nil {
			return "", err
		}
		if p.pos >= len(p.toks) || p.toks[p.pos] != ")" {
			return "", fmt.Errorf("expected ')' to close subexpression")
		}
		p.pos++
		return "(" + inner + ")", nil
	}
	// Unary operators. ! and ~ keep the same Go spelling; unary - too.
	if tk == "-" || tk == "!" || tk == "~" {
		operand, err := p.parsePrimary()
		if err != nil {
			return "", err
		}
		return tk + operand, nil
	}
	// Unary `&` is C's address-of. Under Go's interface dispatch every
	// objects.Object already presents as a pointer, so the operator has
	// no Go-side analogue. Drop it and parse the operand directly.
	if tk == "&" {
		return p.parsePrimary()
	}
	if tk == "oparg" {
		return "oparg", nil
	}
	if tk == "NULL" {
		return "nil", nil
	}
	// CONVERSION_FAILED(arr) tests the result of a STACKREFS_TO_PYOBJECTS
	// materialization. gopy's stack-refs always wrap a real Object so
	// the materialization cannot fail; render the macro as the constant
	// `false` and consume the argument list.
	//
	// CPython: Python/ceval_macros.h CONVERSION_FAILED
	if tk == "CONVERSION_FAILED" {
		if _, err := p.parseCallArg(); err != nil {
			return "", err
		}
		return "false", nil
	}
	// _Py_STR(name) returns one of CPython's preallocated interned
	// string singletons. The only call shape in bytecodes.c today is
	// _Py_STR(empty), used by BUILD_STRING as the join separator;
	// render it directly as the empty string literal.
	//
	// CPython: Include/internal/pycore_global_strings.h _Py_STR
	if tk == "_Py_STR" {
		// _Py_STR takes a bare identifier (a key in CPython's interned
		// strings table), not an expression. Consume the parens and the
		// token directly so unknown-identifier expression parsing never
		// sees the inner name.
		if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
			return "", fmt.Errorf("_Py_STR: expected '('")
		}
		p.pos++
		if p.pos >= len(p.toks) {
			return "", fmt.Errorf("_Py_STR: missing singleton name")
		}
		arg := p.toks[p.pos]
		p.pos++
		if p.pos >= len(p.toks) || p.toks[p.pos] != ")" {
			return "", fmt.Errorf("_Py_STR: expected ')'")
		}
		p.pos++
		if arg == "empty" {
			return `objects.NewStr("")`, nil
		}
		return "", fmt.Errorf("_Py_STR: unsupported singleton %q", arg)
	}
	// _Py_ID(name) returns CPython's preallocated interned identifier
	// for `name` (a bare identifier, not an expression). Used in
	// bytecodes.c via `&_Py_ID(__build_class__)` and friends; the `&`
	// is dropped earlier by the unary-prefix path, so the call surface
	// is the bare macro form. Render as a fresh objects.NewStr — string
	// equality in gopy already collapses across identical contents, so
	// the interning is an optimization rather than a semantic.
	//
	// CPython: Include/internal/pycore_runtime_init_generated.h _Py_ID
	if tk == "_Py_ID" {
		if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
			return "", fmt.Errorf("_Py_ID: expected '('")
		}
		p.pos++
		if p.pos >= len(p.toks) {
			return "", fmt.Errorf("_Py_ID: missing identifier")
		}
		arg := p.toks[p.pos]
		p.pos++
		if p.pos >= len(p.toks) || p.toks[p.pos] != ")" {
			return "", fmt.Errorf("_Py_ID: expected ')'")
		}
		p.pos++
		return fmt.Sprintf("objects.NewStr(%q)", arg), nil
	}
	// _PyDict_FromItems builds a dict from an interleaved key/value
	// array. The only call shape in bytecodes.c (BUILD_MAP) is
	// `_PyDict_FromItems(values_o, 2, values_o+1, 2, oparg)`: same
	// array twice with stride 2, second pointer offset by one. gopy's
	// dict helper takes the values slice and count directly. Consume
	// the five args literally, then emit the simpler helper.
	//
	// CPython: Objects/dictobject.c _PyDict_FromItems
	if tk == "_PyDict_FromItems" {
		if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
			return "", fmt.Errorf("_PyDict_FromItems: expected '('")
		}
		p.pos++
		args := make([]string, 0, 5)
		for {
			a, err := p.parseExpr(0)
			if err != nil {
				return "", err
			}
			args = append(args, a)
			if p.pos < len(p.toks) && p.toks[p.pos] == "," {
				p.pos++
				continue
			}
			if p.pos < len(p.toks) && p.toks[p.pos] == ")" {
				p.pos++
				break
			}
			return "", fmt.Errorf("_PyDict_FromItems: expected ',' or ')'")
		}
		if len(args) != 5 {
			return "", fmt.Errorf("_PyDict_FromItems: expected 5 args, got %d", len(args))
		}
		return fmt.Sprintf("e.dictFromItems(%s, %s)", args[0], args[4]), nil
	}
	// _PyIntrinsics_UnaryFunctions / _PyIntrinsics_BinaryFunctions: dispatch
	// tables of function pointers indexed by oparg. The opcode bodies
	// consume them as `_PyIntrinsics_UnaryFunctions[oparg].func(tstate, x)`
	// and `_PyIntrinsics_BinaryFunctions[oparg].func(tstate, v2, v1)`. Both
	// route through dedicated evalState helpers that own the actual
	// dispatch via the intrinsics package tables.
	//
	// CPython: Python/intrinsics.c _PyIntrinsics_UnaryFunctions /
	// _PyIntrinsics_BinaryFunctions
	if tk == "_PyIntrinsics_UnaryFunctions" || tk == "_PyIntrinsics_BinaryFunctions" {
		// Expect `[<idx>].func(tstate, arg [, arg])`.
		if p.pos >= len(p.toks) || p.toks[p.pos] != "[" {
			return "", fmt.Errorf("%s: expected '['", tk)
		}
		p.pos++
		idx, err := p.parseExpr(0)
		if err != nil {
			return "", err
		}
		if p.pos+4 >= len(p.toks) || p.toks[p.pos] != "]" || p.toks[p.pos+1] != "." || p.toks[p.pos+2] != "func" || p.toks[p.pos+3] != "(" {
			return "", fmt.Errorf("%s: expected '].func('", tk)
		}
		p.pos += 4
		// First arg is `tstate`; the helper carries it implicitly via e.ts.
		if p.pos < len(p.toks) && p.toks[p.pos] == "tstate" {
			p.pos++
			if p.pos < len(p.toks) && p.toks[p.pos] == "," {
				p.pos++
			}
		}
		var args []string
		for {
			if p.pos < len(p.toks) && p.toks[p.pos] == ")" {
				p.pos++
				break
			}
			a, err := p.parseExpr(0)
			if err != nil {
				return "", err
			}
			args = append(args, a)
			if p.pos < len(p.toks) && p.toks[p.pos] == "," {
				p.pos++
				continue
			}
			if p.pos < len(p.toks) && p.toks[p.pos] == ")" {
				p.pos++
				break
			}
			return "", fmt.Errorf("%s: expected ',' or ')'", tk)
		}
		helper := "e.callIntrinsic1"
		if tk == "_PyIntrinsics_BinaryFunctions" {
			helper = "e.callIntrinsic2"
		}
		return fmt.Sprintf("%s(%s, %s)", helper, idx, strings.Join(args, ", ")), nil
	}
	// CPython's small-int singleton table. Used by LOAD_SMALL_INT as
	// `(PyObject *)&_PyLong_SMALL_INTS[_PY_NSMALLNEGINTS + oparg]`.
	// Render the subscript as objects.SmallInt(offset).
	//
	// CPython: Objects/longobject.c:19 _PyLong_SMALL_INTS
	if tk == "_PyLong_SMALL_INTS" {
		if p.pos >= len(p.toks) || p.toks[p.pos] != "[" {
			return "", fmt.Errorf("expected '[' after _PyLong_SMALL_INTS")
		}
		p.pos++
		idx, err := p.parseExpr(0)
		if err != nil {
			return "", err
		}
		if p.pos >= len(p.toks) || p.toks[p.pos] != "]" {
			return "", fmt.Errorf("expected ']' to close _PyLong_SMALL_INTS subscript")
		}
		p.pos++
		return fmt.Sprintf("objects.SmallInt(int(%s))", idx), nil
	}
	// CPython global-objects constant for the negative half of the
	// small-int table. Hard-coded to 5 in pycore_global_objects.h.
	//
	// CPython: Include/internal/pycore_global_objects.h:35 _PY_NSMALLNEGINTS
	if tk == "_PY_NSMALLNEGINTS" {
		return "5", nil
	}
	if tk == "true" || tk == "false" {
		return tk, nil
	}
	// CPython helpers carry tstate / frame as the first one or two args.
	// The helperCall translation drops them via dropFirst, but the parse
	// still needs to consume the tokens, so pass them through verbatim.
	if tk == "tstate" || tk == "frame" || tk == "this_instr" {
		return tk, nil
	}
	if isNumericLiteral(tk) {
		return stripIntSuffix(tk), nil
	}
	if isBareIdent(tk) {
		if _, ok := p.bound[tk]; ok {
			return goLocalName(tk), nil
		}
		if p.locals[tk] {
			return goLocalName(tk), nil
		}
		// CPython exception-type constants: PyExc_TypeError, PyExc_AttributeError,
		// etc. The gopy errors package mirrors each as a *objects.Type
		// singleton, so the surface reference resolves directly without a
		// sentinel. _PyErr_ExceptionMatches and friends consume the
		// *objects.Type to compare against the pending exception's class.
		if strings.HasPrefix(tk, "PyExc_") {
			return "pyerrors." + tk, nil
		}
		// Py_TPFLAGS_* constants. Used as the RHS of `tp_flags & FLAG`
		// bit tests in MATCH_MAPPING / MATCH_SEQUENCE. The objects
		// package exposes the same values as TpFlagMapping etc.
		if rest, ok := strings.CutPrefix(tk, "Py_TPFLAGS_"); ok {
			return "objects.TpFlag" + camelizeFlag(rest), nil
		}
	}
	return "", fmt.Errorf("unexpected token %q in expression", tk)
}

// isNumericLiteral matches C integer literals (decimal only; we strip
// trailing u/U/l/L). Float literals don't appear in opcode bodies.
func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case 'u', 'U', 'l', 'L', 'x', 'X', 'a', 'b', 'c', 'd', 'e', 'f', 'A', 'B', 'C', 'D', 'E', 'F':
			continue
		}
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

func stripIntSuffix(s string) string {
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == 'u' || last == 'U' || last == 'l' || last == 'L' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

// parseGetItemCall consumes `( FRAME_CO_CONSTS | FRAME_CO_NAMES , <idx> )`
// and emits the matching evalState helper. CPython's GETITEM is a
// PyTuple_GET_ITEM macro under the hood; for the two co_consts / co_names
// table arguments we generate dedicated helpers that handle the wrap from
// the raw compile-time storage to objects.Object.
func (p *exprParser) parseGetItemCall() (string, error) {
	if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
		return "", fmt.Errorf("expected '(' after GETITEM")
	}
	p.pos++
	if p.pos >= len(p.toks) {
		return "", fmt.Errorf("unexpected end of GETITEM call")
	}
	table := p.toks[p.pos]
	p.pos++
	var helper string
	switch table {
	case "FRAME_CO_CONSTS":
		helper = "e.constAt"
	case "FRAME_CO_NAMES":
		helper = "e.nameAt"
	default:
		return "", fmt.Errorf("unsupported GETITEM table %q", table)
	}
	if p.pos >= len(p.toks) || p.toks[p.pos] != "," {
		return "", fmt.Errorf("expected ',' after GETITEM table")
	}
	p.pos++
	// The index is often `oparg`, but CPython also writes
	// `oparg >> 2` (LOAD_SUPER_ATTR) and similar. Collect every
	// token up to the matching ')', then render them verbatim:
	// C's bitshift / mask operators all spell the same in Go.
	depth := 1
	start := p.pos
	for p.pos < len(p.toks) && depth > 0 {
		switch p.toks[p.pos] {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				idx := strings.Join(p.toks[start:p.pos], " ")
				p.pos++
				return fmt.Sprintf("%s(int(%s))", helper, idx), nil
			}
		}
		p.pos++
	}
	return "", fmt.Errorf("unterminated GETITEM call")
}

// helperCall describes how to render a CPython helper as a Go call on
// the eval state. Arity must match the number of comma-separated args
// in the source; tstate / frame are dropped because they are implicit
// receivers in the evalState method.
type helperCall struct {
	goExpr    string // e.g. "e.pyNumberNegative" — applied as Func(args...)
	arity     int    // expected non-implicit arg count
	dropFirst int    // strip this many leading args (tstate, frame)
}

// structArrow is the per-segment translation table for postfix
// `->field` chains in CPython opcode bodies. Each key is
// `currentTag.field`; the value is the Go expression that replaces the
// chain up to and including that segment, plus the tag the next link
// uses. Tags start as the leading identifier (`tstate`, `frame`) and
// thread through each link.
//
// Replacements are full-substitution, not suffixes: `tstate->interp`
// renders as `e.ts.Interp()`, dropping the literal `tstate` because
// the gopy receiver carries it implicitly.
var structArrow = map[string]struct {
	goExpr  string
	nextTag string
}{
	// `tstate->interp->common_consts` is the LOAD_COMMON_CONSTANT input.
	// The chain bottoms out at a helper because state.Interpreter stores
	// the array as `[5]any` (state stays free of objects imports); the
	// helper asserts each slot to objects.Object on the way out.
	//
	// CPython: Include/internal/pycore_interp_structs.h common_consts
	"tstate.interp":        {goExpr: "e.ts.Interp()", nextTag: "interp"},
	"interp.common_consts": {goExpr: "e.commonConsts()", nextTag: "common_consts"},
}

// helperCalls is the registry of CPython helpers mapped to gopy
// evalState methods. The Go wrappers live in vm/eval_helpers.go and
// own the failure mode (pendingErr) so the translator can emit them
// uniformly inside the action body.
var helperCalls = map[string]helperCall{
	"Py_Is": {goExpr: "e.pyIs", arity: 2},
	// PyObject_GetIter(obj) calls obj.__iter__() and returns the
	// resulting iterator or NULL on error. Mirrors abstract.c
	// PyObject_GetIter.
	//
	// CPython: Objects/abstract.c PyObject_GetIter
	"PyObject_GetIter":  {goExpr: "e.objectGetIter", arity: 1},
	"Py_TYPE":           {goExpr: "e.pyType", arity: 1},
	"PyNumber_Negative": {goExpr: "e.pyNumberNegative", arity: 1},
	"PyNumber_Invert":   {goExpr: "e.pyNumberInvert", arity: 1},
	"PyNumber_Positive": {goExpr: "e.pyNumberPositive", arity: 1},
	"PyNumber_Absolute": {goExpr: "e.pyNumberAbsolute", arity: 1},
	// Name / import family. The first one or two C args carry the
	// implicit tstate / frame receivers; drop those so the call lines
	// up with the gopy evalState method.
	"_PyEval_LoadName":   {goExpr: "e.loadName", arity: 1, dropFirst: 2},
	"_PyEval_ImportName": {goExpr: "e.importName", arity: 3, dropFirst: 2},
	"_PyEval_ImportFrom": {goExpr: "e.importFrom", arity: 2, dropFirst: 1},
	// Dict / object mutation. These return a C int err (0 on success,
	// nonzero on failure); the surrounding `int err = ...; ERROR_IF(err);`
	// pattern handles the dispatch.
	"PyDict_SetItem":   {goExpr: "e.dictSetItem", arity: 3},
	"PyObject_DelItem": {goExpr: "e.objectDelItem", arity: 2},
	"PyObject_SetItem": {goExpr: "e.objectSetItem", arity: 3},
	"PyObject_DelAttr": {goExpr: "e.objectDelAttr", arity: 2},
	// Pattern-matching helpers from Python/ceval.c.
	"_PyEval_MatchKeys":  {goExpr: "e.matchKeys", arity: 2, dropFirst: 1},
	"_PyEval_MatchClass": {goExpr: "e.matchClass", arity: 4, dropFirst: 1},
	// Exception-type validation. Both return int err (0 ok, -1 bad
	// type with an exception set); pendingErr carries the cause.
	"_PyEval_CheckExceptTypeValid":     {goExpr: "e.checkExceptTypeValid", arity: 1, dropFirst: 1},
	"_PyEval_CheckExceptStarTypeValid": {goExpr: "e.checkExceptStarTypeValid", arity: 1, dropFirst: 1},
	// PyErr_GivenExceptionMatches: 1 if exc is an instance of (or
	// matches) the target type, else 0.
	"PyErr_GivenExceptionMatches": {goExpr: "e.exceptionMatches", arity: 2},
	// _PyErr_Occurred(tstate) returns whether the running thread state
	// holds a pending exception; gopy stashes pending failures on the
	// evalState so the wrapper just inspects e.pendingErr.
	//
	// CPython: Python/errors.c _PyErr_Occurred.
	"_PyErr_Occurred": {goExpr: "e.errOccurred", arity: 0, dropFirst: 1},
	// _PyErr_ExceptionMatches(tstate, PyExc_X) reports whether the
	// running exception is of the named type. gopy holds the pending
	// exception as a Go error string, so the wrapper does a substring
	// match against the type-name placeholder.
	//
	// CPython: Python/errors.c _PyErr_ExceptionMatches.
	"_PyErr_ExceptionMatches": {goExpr: "e.errExceptionMatches", arity: 1, dropFirst: 1},
	// Dict / list / set mutation helpers. These all return a C int err
	// (0 ok, nonzero error) and stash the cause on pendingErr.
	"PyDict_Pop":            {goExpr: "e.dictPop", arity: 3},
	"_PyDict_MergeEx":       {goExpr: "e.dictMergeEx", arity: 3},
	"_PyList_Extend":        {goExpr: "e.listExtend", arity: 2},
	"_PyList_AppendTakeRef": {goExpr: "e.listAppendTakeRef", arity: 2},
	"_PySet_AddTakeRef":     {goExpr: "e.setAddTakeRef", arity: 2},
	"_PySet_Update":         {goExpr: "e.setUpdate", arity: 2},
	// PyObject_Length returns Py_ssize_t (-1 on error).
	"PyObject_Length": {goExpr: "e.objectLength", arity: 1},
	// PySet_New(iterable) builds a fresh set; iterable may be NULL.
	"PySet_New": {goExpr: "e.setNew", arity: 1},
	// _PyDict_LoadGlobal(globals, builtins, name) walks globals then
	// builtins for a name without raising on miss. Returns the found
	// value (Object) or nil when neither dict carries the key; on a
	// real lookup failure pendingErr is set.
	//
	// CPython: Objects/dictobject.c _PyDict_LoadGlobal
	"_PyDict_LoadGlobal": {goExpr: "e.dictLoadGlobal", arity: 3},
	// PyDict_New() returns a fresh empty dict. CPython can fail with
	// MemoryError; gopy's NewDict is infallible under Go's GC.
	//
	// CPython: Objects/dictobject.c PyDict_New
	"PyDict_New": {goExpr: "e.dictNew", arity: 0},
	// Cell helpers.
	"PyCell_New":         {goExpr: "e.cellNew", arity: 1},
	"PyCell_SwapTakeRef": {goExpr: "e.cellSwapTakeRef", arity: 2},
	// Async-iter next.
	"_PyEval_GetANext": {goExpr: "e.getANext", arity: 1},
	// Stack-ref tuple/list builders (used by BUILD_TUPLE / BUILD_LIST).
	// The first arg is the input scratch array name, second is its length.
	"_PyTuple_FromStackRefStealOnSuccess": {goExpr: "e.tupleFromStackRef", arity: 2},
	"_PyList_FromStackRefStealOnSuccess":  {goExpr: "e.listFromStackRef", arity: 2},
	// PyObject_Format(obj, spec) used by FORMAT_WITH_SPEC. spec may be NULL.
	"PyObject_Format": {goExpr: "e.objectFormat", arity: 2},
	// Long constructor from a Go ssize_t (int). Boxes a Py_ssize_t as a
	// Python int. Used by GET_LEN.
	"PyLong_FromSsize_t": {goExpr: "e.longFromSsizeT", arity: 1},
	// _PyCell_GetStackRef(cell) returns the cell's contents as a stack
	// ref; in gopy we surface it as an Object.
	"_PyCell_GetStackRef": {goExpr: "e.cellGetStackRef", arity: 1},
	// _PyEval_GetAwaitable(iter, opcode) wraps GET_AWAITABLE; the second
	// arg is a hint about the opcode triggering the call (BEFORE_ASYNC_WITH
	// vs ordinary). gopy ignores the hint.
	"_PyEval_GetAwaitable": {goExpr: "e.getAwaitable", arity: 2},
	// PyDict_Update(a, b) returns int err; merges b into a without
	// duplicate-key checking.
	"PyDict_Update": {goExpr: "e.dictUpdate", arity: 2},
	// _PyDict_SetItem_Take2(dict, key, value) returns int err; gopy
	// treats it the same as PyDict_SetItem since no refcount transfer
	// matters under Go's GC.
	"_PyDict_SetItem_Take2": {goExpr: "e.dictSetItem", arity: 3},
	// _PyTemplate_Build(strings, interpolations) returns a t-string.
	"_PyTemplate_Build": {goExpr: "e.templateBuild", arity: 2},
	// Exact-type predicates. Each takes a borrowed PyObject* and returns
	// a C int (0 or 1); gopy mirrors them as bool-returning Go calls and
	// the int-vs-bool detection in translateTypedDecl keeps `int x = ...`
	// from getting an `!= 0` fixup against a bool RHS.
	"PyUnicode_CheckExact": {goExpr: "objects.IsExactStr", arity: 1},
	"PyLong_CheckExact":    {goExpr: "objects.IsExactInt", arity: 1},
	"PyFloat_CheckExact":   {goExpr: "objects.IsExactFloat", arity: 1},
	"PyList_CheckExact":    {goExpr: "objects.IsExactList", arity: 1},
	"PyTuple_CheckExact":   {goExpr: "objects.IsExactTuple", arity: 1},
	// Borrowed-ref tuple subscript. CPython exposes it as a macro; gopy
	// goes through a helper that bounds-checks and reports a typed
	// IndexError via pendingErr instead of segfaulting.
	//
	// CPython: Include/cpython/tupleobject.h PyTuple_GET_ITEM
	"PyTuple_GET_ITEM":  {goExpr: "e.tupleGetItem", arity: 2},
	"PyDict_CheckExact": {goExpr: "objects.IsExactDict", arity: 1},
	"PySet_CheckExact":  {goExpr: "objects.IsExactSet", arity: 1},
	"PyBool_Check":      {goExpr: "objects.IsExactBool", arity: 1},
	"PySlice_Check":     {goExpr: "objects.IsExactSlice", arity: 1},
	"PyCoro_CheckExact": {goExpr: "objects.IsCoroutine", arity: 1},
	"PyGen_Check":       {goExpr: "objects.IsGenerator", arity: 1},
	"PyGen_CheckExact":  {goExpr: "objects.IsGenerator", arity: 1},
	// monitor_stop_iteration fires PEP 669 STOP_ITERATION for the
	// running frame. The first three args (tstate, frame, this_instr)
	// are implicit on the evalState; the wrapper takes only the value.
	//
	// CPython: Python/ceval.c:2550 monitor_stop_iteration
	"monitor_stop_iteration": {goExpr: "e.monitorStopIteration", arity: 1, dropFirst: 3},
	// Long predicate. Returns bool, matching the C int 0/1 convention.
	"_PyLong_IsZero": {goExpr: "e.longIsZero", arity: 1},
	// Slice constructor. CPython exposes a 3-arg PyObject* factory; the
	// Go side maps to a thin e.sliceNew helper that wraps the runtime
	// PySlice equivalent and reflects "NULL step" as Python None.
	//
	// CPython: Objects/sliceobject.c PySlice_New.
	"PySlice_New": {goExpr: "e.sliceNew", arity: 3},
	// _PyUnicode_JoinArray(sep, items, n) returns the joined string or
	// NULL on error. Maps to e.unicodeJoinArray which handles the type
	// check on the separator and surfaces the failure on pendingErr.
	//
	// CPython: Objects/unicodeobject.c _PyUnicode_JoinArray
	"_PyUnicode_JoinArray": {goExpr: "e.unicodeJoinArray", arity: 3},
}

// parseHelperCall consumes `(arg1, arg2, ...)` and renders the call.
func (p *exprParser) parseHelperCall(h helperCall) (string, error) {
	if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
		return "", fmt.Errorf("expected '(' after helper")
	}
	p.pos++
	args := []string{}
	if p.pos < len(p.toks) && p.toks[p.pos] == ")" {
		p.pos++
	} else {
		for {
			a, err := p.parseExpr(0)
			if err != nil {
				return "", err
			}
			args = append(args, a)
			if p.pos >= len(p.toks) {
				return "", fmt.Errorf("unterminated helper call")
			}
			if p.toks[p.pos] == "," {
				p.pos++
				continue
			}
			if p.toks[p.pos] == ")" {
				p.pos++
				break
			}
			return "", fmt.Errorf("expected ',' or ')' in helper call, got %q", p.toks[p.pos])
		}
	}
	if h.dropFirst > 0 {
		if len(args) < h.dropFirst {
			return "", fmt.Errorf("helper call missing %d implicit args", h.dropFirst)
		}
		args = args[h.dropFirst:]
	}
	if len(args) != h.arity {
		return "", fmt.Errorf("helper call arity: want %d, got %d", h.arity, len(args))
	}
	return h.goExpr + "(" + strings.Join(args, ", ") + ")", nil
}

// parseCallArg consumes ( <expr> ) and returns the rendered inner
// expression. Single-arg callees only for now; multi-arg can layer on
// when an opcode needs it.
func (p *exprParser) parseCallArg() (string, error) {
	if p.pos >= len(p.toks) || p.toks[p.pos] != "(" {
		return "", fmt.Errorf("expected '(' after function name")
	}
	p.pos++
	// parseExpr (not parsePrimary) so postfix subscripts on the inner
	// expression participate: callers like PyStackRef_AsPyObjectBorrow
	// often wrap a sized-input index such as `args[0]`.
	inner, err := p.parseExpr(0)
	if err != nil {
		return "", err
	}
	if p.pos >= len(p.toks) || p.toks[p.pos] != ")" {
		return "", fmt.Errorf("expected ')' to close call")
	}
	p.pos++
	return inner, nil
}

// translateStackrefsToPyObjects handles
// `STACKREFS_TO_PYOBJECTS(src, n, dst);`. Reads src as a sized-input
// array name (a Go []stackref.Ref local), n as a count expression,
// and declares dst as a new []objects.Object local that the
// subsequent body uses for PyObject*-array consumers like
// _PyUnicode_JoinArray.
//
// CPython: Python/ceval_macros.h STACKREFS_TO_PYOBJECTS
func (t *actionTranslator) translateStackrefsToPyObjects() error {
	t.pos++ // STACKREFS_TO_PYOBJECTS
	args, err := t.takeParenTokens()
	if err != nil {
		return fmt.Errorf("STACKREFS_TO_PYOBJECTS: %w", err)
	}
	t.acceptSemi()
	// Three comma-separated args at top level.
	parts, err := splitTopLevelCommas(args)
	if err != nil {
		return fmt.Errorf("STACKREFS_TO_PYOBJECTS: %w", err)
	}
	if len(parts) != 3 {
		return fmt.Errorf("STACKREFS_TO_PYOBJECTS: want 3 args, got %d", len(parts))
	}
	srcExpr, err := t.translateExpr(parts[0])
	if err != nil {
		return fmt.Errorf("STACKREFS_TO_PYOBJECTS src: %w", err)
	}
	nExpr, err := t.translateExpr(parts[1])
	if err != nil {
		return fmt.Errorf("STACKREFS_TO_PYOBJECTS count: %w", err)
	}
	if len(parts[2]) != 1 || !isBareIdent(parts[2][0]) {
		return fmt.Errorf("STACKREFS_TO_PYOBJECTS dst: not a bare identifier")
	}
	dstName := parts[2][0]
	t.locals[dstName] = true
	fmt.Fprintf(t.writer, "%s := e.stackrefsToObjects(%s, %s)\n",
		goLocalName(dstName), srcExpr, nExpr)
	fmt.Fprintf(t.writer, "_ = %s\n", goLocalName(dstName))
	return nil
}

// splitTopLevelCommas splits a token slice on top-level commas (commas
// not inside nested parens or brackets). Returns one slice of tokens
// per argument.
func splitTopLevelCommas(toks []string) ([][]string, error) {
	if len(toks) == 0 {
		return nil, nil
	}
	out := [][]string{}
	cur := []string{}
	depth := 0
	for _, tk := range toks {
		switch tk {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced bracket")
			}
		}
		if tk == "," && depth == 0 {
			out = append(out, cur)
			cur = []string{}
			continue
		}
		cur = append(cur, tk)
	}
	out = append(out, cur)
	return out, nil
}

// translateJumpBy emits the dispatch return for `JUMPBY(<expr>);`.
// CPython's JUMPBY adjusts next_instr by oparg codeunits *relative to
// the instruction following the current one*, so our jumpBy helper
// adds 1 to land on the same place.
func (t *actionTranslator) translateJumpBy() error {
	t.pos++ // JUMPBY
	arg, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	arg = strings.TrimSpace(arg)
	// JUMPBY(oparg) — forward jump (JUMP_FORWARD).
	// JUMPBY(-oparg) — backward jump (JUMP_BACKWARD family). The
	// space appears because takeParenthesised joins tokens with a
	// single space.
	switch arg {
	case "oparg":
		fmt.Fprintln(t.writer, "return e.jumpBy(int(oparg) + 1), nil")
	case "- oparg":
		fmt.Fprintln(t.writer, "return e.jumpBy(-int(oparg) + 1), nil")
	default:
		return fmt.Errorf("JUMPBY arg %q is not the bare 'oparg' identifier", arg)
	}
	t.terminates = true
	return nil
}

// translateControlIf emits:
//
//	if <cond> { <action> }
//
// for `MACRO(cond);` shapes where the action is a fixed string.
func (t *actionTranslator) translateControlIf(action string) error {
	t.pos++ // macro name
	cond, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	fmt.Fprintf(t.writer, "if %s { %s }\n", cond, action)
	return nil
}

// translateErrorIf handles `ERROR_IF(cond);` by routing through the
// eval loop's error helper. CPython 3.14 dropped the label argument;
// the macro always jumps to the per-instruction `error` label, so we
// always emit the same `e.error("error")` return.
//
// CPython: Tools/cases_generator/generators_common.py error_if.
func (t *actionTranslator) translateErrorIf() error {
	t.pos++ // ERROR_IF
	condToks, err := t.takeParenTokens()
	if err != nil {
		return err
	}
	t.acceptSemi()
	if len(condToks) == 1 && condToks[0] == "true" {
		fmt.Fprintln(t.writer, `return 0, e.error("error")`)
		if t.nestDepth == 0 {
			t.terminates = true
		}
		return nil
	}
	// Route through translateExpr so helper-call vocabulary (NULL → nil,
	// _PyErr_Occurred → e.errOccurred(), etc.) gets the same treatment
	// as in any other expression context.
	condExpr, err := t.translateExpr(condToks)
	if err != nil {
		// Legacy passthrough for synthetic fixtures that name a bare
		// identifier not declared anywhere in the body. CPython sources
		// always declare the cond, so this only matters for unit tests.
		condExpr = strings.Join(condToks, " ")
		condExpr = rewriteCCondToGo(condExpr, t.intLocals)
	}
	if len(condToks) == 1 && t.intLocals[condToks[0]] {
		condExpr = condExpr + " != 0"
	} else if len(condToks) == 1 && t.locals[condToks[0]] && !t.intLocals[condToks[0]] && !t.boolLocals[condToks[0]] {
		condExpr = condExpr + " != nil"
	}
	fmt.Fprintf(t.writer, "if %s { return 0, e.error(\"error\") }\n", condExpr)
	return nil
}

// rewriteCCondToGo was the legacy ERROR_IF condition rewriter; it has
// been superseded by routing through translateExpr.
func rewriteCCondToGo(s string, intLocals map[string]bool) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "NULL" {
			parts[i] = "nil"
		}
	}
	out := strings.Join(parts, " ")
	if len(parts) == 1 && intLocals[parts[0]] {
		out = out + " != 0"
	}
	return out
}

// translateErrorNoPop emits the unconditional `goto error` equivalent.
// CPython's ERROR_NO_POP jumps to the per-instruction error label
// without first popping inputs; under the gopy refcount-only path
// that distinction collapses to the same `e.error("error")` return.
//
// CPython: Python/ceval_macros.h ERROR_NO_POP.
func (t *actionTranslator) translateErrorNoPop() error {
	t.pos++ // ERROR_NO_POP
	// CPython spells it ERROR_NO_POP() — consume the empty parens.
	if t.pos < len(t.toks) && t.toks[t.pos].Text == "(" {
		if _, err := t.takeParenthesised(); err != nil {
			return err
		}
	}
	t.acceptSemi()
	fmt.Fprintln(t.writer, `return 0, e.error("error")`)
	return nil
}

// translateIfStmt translates a C-style `if (cond) { body }` plus an
// optional `else { body }` clause. The condition is parsed via the
// expression vocabulary so helpers like PyStackRef_IsNull resolve to
// their Go method counterparts. The body is recursively translated
// using the same statement walker; this means nested if blocks and
// ERROR_NO_POP / ERROR_IF / output assignments inside the branch all
// work without extra plumbing.
//
// CPython: bodies in Python/bytecodes.c routinely guard error paths
// with `if (cond) { _PyErr_*(...); ERROR_NO_POP(); }` and dispatch
// branches with `if (cond) { ... } else { ... }`.
func (t *actionTranslator) translateIfStmt() error {
	t.pos++ // if
	condToks, err := t.takeParenTokens()
	if err != nil {
		return fmt.Errorf("if: %w", err)
	}
	condExpr, err := t.translateExpr(condToks)
	if err != nil {
		return fmt.Errorf("if cond: %w", err)
	}
	// Bare int identifiers need explicit `!= 0` to satisfy Go's
	// boolean typing; mirrors the ERROR_IF condition fixup. Bare
	// object-typed locals (PyObject *attrs_o = ...; if (attrs_o) ...)
	// likewise need `!= nil` because Go has no implicit nil truthiness.
	if len(condToks) == 1 && t.intLocals[condToks[0]] {
		condExpr = condExpr + " != 0"
	} else if len(condToks) == 1 && t.locals[condToks[0]] && !t.intLocals[condToks[0]] && !t.boolLocals[condToks[0]] {
		condExpr = condExpr + " != nil"
	}
	thenBody, err := t.translateBraceBlock()
	if err != nil {
		return fmt.Errorf("if then: %w", err)
	}
	fmt.Fprintf(t.writer, "if %s {\n%s}\n", condExpr, thenBody)
	if t.pos < len(t.toks) && t.toks[t.pos].Text == "else" {
		t.pos++ // else
		if t.pos < len(t.toks) && t.toks[t.pos].Text == "if" {
			// `else if (cond) { ... }` — render as `else { if ... }` to
			// avoid having to special-case it. Go is happy with either.
			fmt.Fprintln(t.writer, "{")
			if err := t.translateIfStmt(); err != nil {
				return fmt.Errorf("else if: %w", err)
			}
			fmt.Fprintln(t.writer, "}")
			return nil
		}
		elseBody, err := t.translateBraceBlock()
		if err != nil {
			return fmt.Errorf("if else: %w", err)
		}
		// Rewrite the trailing newline of the printed `}` so the `else`
		// keyword sits on the same line; Go's parser does not allow a
		// standalone `else` after a line break.
		w := t.writer.String()
		if strings.HasSuffix(w, "}\n") {
			t.writer.Reset()
			t.writer.WriteString(strings.TrimSuffix(w, "}\n"))
			fmt.Fprintf(t.writer, "} else {\n%s}\n", elseBody)
		} else {
			fmt.Fprintf(t.writer, "else {\n%s}\n", elseBody)
		}
	}
	return nil
}

// translateBraceBlock consumes `{ stmt; stmt; ... }` and returns the
// translated Go body as a string. The inner statements share locals
// and assigned-output state with the outer translator so a value
// assigned inside the branch is still considered "assigned" after the
// block closes.
func (t *actionTranslator) translateBraceBlock() (string, error) {
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "{" {
		return "", fmt.Errorf("expected '{', got %q", t.toks[t.pos].Text)
	}
	t.pos++
	saved := t.writer
	sub := &strings.Builder{}
	t.writer = sub
	t.nestDepth++
	defer func() { t.writer = saved; t.nestDepth-- }()
	for t.pos < len(t.toks) && t.toks[t.pos].Text != "}" {
		if err := t.translateStmt(); err != nil {
			return "", err
		}
	}
	if t.pos >= len(t.toks) {
		return "", fmt.Errorf("unterminated '{'")
	}
	t.pos++ // }
	return sub.String(), nil
}

// translateUnaryCall handles `MACRO(arg);` where arg is a single bound
// stack-slot name. Emits `arg<suffix>` (e.g. `.Close()`). Bails when
// the argument is anything more interesting than a bare identifier,
// since those need typed-helper translation that we do not have yet.
func (t *actionTranslator) translateUnaryCall(suffix string) error {
	t.pos++ // macro name
	arg, err := t.takeParenthesised()
	if err != nil {
		return err
	}
	t.acceptSemi()
	arg = strings.TrimSpace(arg)
	if !isBareIdent(arg) {
		return fmt.Errorf("unary call arg %q is not a bare identifier", arg)
	}
	if _, isBound := t.bound[arg]; !isBound {
		if !t.locals[arg] {
			return fmt.Errorf("unary call arg %q is not a bound stack slot or local", arg)
		}
	}
	fmt.Fprintf(t.writer, "%s%s\n", goLocalName(arg), suffix)
	return nil
}

// goLocalName mirrors bindName for the case where the slot has a real
// source name. Keyword slots get a `_v` suffix so the body references
// match the prologue's locals.
// camelizeFlag turns a SCREAMING_SNAKE flag name (MAPPING, SEQUENCE)
// into the CamelCase suffix used by objects.TpFlagXxx constants.
func camelizeFlag(s string) string {
	parts := strings.Split(strings.ToLower(s), "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func goLocalName(name string) string {
	if goKeywords[name] {
		return name + "_v"
	}
	return name
}

// isBareIdent returns true if s is a single C identifier with no
// operators, casts, or member access.
func isBareIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			// always allowed
		case (r >= '0' && r <= '9') && i > 0:
			// allowed after first char
		default:
			return false
		}
	}
	return true
}

// translateNullary emits a single statement and consumes
// `MACRO();` from the token stream.
func (t *actionTranslator) translateNullary(stmt string) error {
	t.pos++
	if _, err := t.takeParenthesised(); err != nil {
		return err
	}
	t.acceptSemi()
	fmt.Fprintln(t.writer, stmt)
	return nil
}

// skipParenthesised drops a `MACRO(...)` plus optional `;`. Used for
// statistics macros that have no runtime effect.
// translateVoidCast consumes `(void)expr;` and emits nothing. The
// rest of the body still has to translate; this just keeps the
// statement walker moving past a CPython C-ism that has no Go
// counterpart.
func (t *actionTranslator) translateVoidCast() error {
	// Match `( void ) <ident-or-expr> ;` exactly. If the shape
	// doesn't fit we bail rather than silently swallow a real
	// expression statement.
	if t.pos+2 >= len(t.toks) ||
		t.toks[t.pos].Text != "(" ||
		t.toks[t.pos+1].Text != "void" ||
		t.toks[t.pos+2].Text != ")" {
		return fmt.Errorf("unexpected token %q in expression", t.toks[t.pos].Text)
	}
	t.pos += 3 // consume `( void )`
	for t.pos < len(t.toks) && t.toks[t.pos].Text != ";" {
		t.pos++
	}
	t.acceptSemi()
	return nil
}

func (t *actionTranslator) skipParenthesised() error {
	t.pos++
	if _, err := t.takeParenthesised(); err != nil {
		return err
	}
	t.acceptSemi()
	return nil
}

// takeParenthesised consumes `(...)` starting at t.pos and returns the
// raw text inside the parens (joined with single spaces).
func (t *actionTranslator) takeParenthesised() (string, error) {
	if t.pos >= len(t.toks) || t.toks[t.pos].Text != "(" {
		return "", fmt.Errorf("expected '(' at position %d", t.pos)
	}
	t.pos++
	depth := 1
	parts := []string{}
	for t.pos < len(t.toks) && depth > 0 {
		tk := t.toks[t.pos]
		switch tk.Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				t.pos++
				return strings.Join(parts, " "), nil
			}
		}
		parts = append(parts, tk.Text)
		t.pos++
	}
	return "", fmt.Errorf("unterminated '(' at position %d", t.pos)
}

func (t *actionTranslator) acceptSemi() {
	if t.pos < len(t.toks) && t.toks[t.pos].Text == ";" {
		t.pos++
	}
}

// bindNames returns a name->direction map for the signature.
// Synthetic / unused slots don't appear.
func bindNames(sig *SignatureAnalysis) map[string]string {
	out := map[string]string{}
	for _, b := range sig.Inputs {
		if b.Name == "" || b.Name == "unused" {
			continue
		}
		out[b.Name] = "in"
	}
	for _, b := range sig.Outputs {
		if b.Name == "" || b.Name == "unused" {
			continue
		}
		// Slots that appear in both inputs and outputs (e.g. IMPORT_FROM's
		// `from` which is preserved and re-pushed) keep their "in"
		// direction so the body's reads still resolve.
		if _, ok := out[b.Name]; ok {
			continue
		}
		out[b.Name] = "out"
	}
	return out
}

// stripWhitespace drops newline / comment tokens so the statement
// walker doesn't have to skip them at every step. CMacro tokens are
// already filtered (and their conditional bodies are elided) by
// tokenize().
func stripWhitespace(in []dslTok) []dslTok {
	out := make([]dslTok, 0, len(in))
	for _, t := range in {
		switch t.Kind {
		case tokNewline, tokComment, tokCMacro:
			continue
		}
		out = append(out, t)
	}
	return out
}

// splitTopLevelComma splits "a, b" into ("a", "b", true). Commas
// inside nested parens or brackets are ignored.
func splitTopLevelComma(s string) (head, tail string, ok bool) {
	depth := 0
	for i, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				return s[:i], s[i+1:], true
			}
		}
	}
	return s, "", false
}
