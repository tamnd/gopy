package stdlibinit

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// threadExecutor adapts a *state.Thread to the imp.Executor interface
// so that imp.ImportModule can drive Python source modules via the eval
// loop.
type threadExecutor struct{ ts *state.Thread }

func (e *threadExecutor) ExecCode(code *objects.Code, mod *objects.Module) (objects.Object, error) {
	return vm.EvalCode(e.ts, code, mod.Dict(), nil)
}

func TestImportTraceback(t *testing.T) {
	ts := state.NewThread()
	exec := &threadExecutor{ts: ts}
	_, err := imp.ImportModule(exec, "traceback")
	if err != nil {
		if errors.Is(err, imp.ErrModuleNotFound) || strings.Contains(err.Error(), "No module named") {
			t.Skipf("blocked on missing transitive import: %v", err)
		}
		t.Fatalf("import traceback: %v", err)
	}
}
