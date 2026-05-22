package _warnings

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/parser/lexer"
)

// TestFlushLexerWarningsRoutesToFilter feeds a synthetic
// SyntaxWarning entry through FlushLexerWarnings and confirms the
// "filename:lineno: category: text" line lands on sys.stderr via the
// default filter, the same shape CPython produces from
// _PyTokenizer_parser_warn.
//
// CPython: Parser/tokenizer/helpers.c:152 _PyTokenizer_parser_warn
func TestFlushLexerWarningsRoutesToFilter(t *testing.T) {
	resetState(t)
	warns := []lexer.SyntaxError{{
		Pos:      lexer.Pos{Line: 7},
		Message:  "invalid \\X escape",
		Category: "SyntaxWarning",
	}}
	out := captureStderr(t, func() {
		FlushLexerWarnings("frag.py", warns)
	})
	if !strings.Contains(out, "SyntaxWarning") || !strings.Contains(out, "invalid \\X escape") {
		t.Fatalf("expected SyntaxWarning text in stderr, got %q", out)
	}
	if !strings.Contains(out, "frag.py:7") {
		t.Fatalf("expected filename:lineno in stderr, got %q", out)
	}
}

// TestLexerWarnHookRegistered pins that module/_warnings.init wired
// itself into lexer.WarnHook so callers do not need to register the
// hook manually.
func TestLexerWarnHookRegistered(t *testing.T) {
	if lexer.WarnHook == nil {
		t.Fatalf("lexer.WarnHook is nil; module/_warnings.init did not register it")
	}
}
