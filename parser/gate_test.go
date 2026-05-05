// Gate tests pin the cases the generated parser can handle today.
// As the action surface fills in, more sources will land here and
// the catch-all sentinel test will shrink.

package parser

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestParseEmptyModule is the M7 gate: an empty source must come
// back as a real *ast.Module with no Body, not the
// ErrParserNotImplemented sentinel.
func TestParseEmptyModule(t *testing.T) {
	mod, err := ParseString("", "empty.py", ModeFile)
	if err != nil {
		t.Fatalf("ParseString(\"\"): %v", err)
	}
	m, ok := mod.(*ast.Module)
	if !ok {
		t.Fatalf("got %T, want *ast.Module", mod)
	}
	if len(m.Body) != 0 {
		t.Errorf("Body len = %d, want 0", len(m.Body))
	}
}
