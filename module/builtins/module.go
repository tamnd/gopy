// builtins built-in module. Registers the builtins dict as an inittab
// entry so `import builtins` resolves in the stdlib without requiring
// cmd/gopy/main.go initialization. The builtins dict is also what
// every module frame uses as its implicit __builtins__ namespace.
//
// CPython: Python/pylifecycle.c:1413 init_interp_main (builtins in interp->modules)
// CPython: Modules/builtinsmodule.c:1 PyInit_builtins

package builtins

import (
	"errors"
	"os"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("builtins", buildModule)
}

// buildModule creates the builtins module by calling builtins.Init with
// os.Stdout as the print target. Init stamps the canonical module into
// sys.modules (interp->modules) and records it as the m_self of every
// module-level builtin, so this returns that exact object rather than
// wrapping the dict in a fresh module. A second wrapper would make
// `len.__self__ is builtins` false even though both share one dict.
//
// CPython: Modules/builtinsmodule.c:3116 builtin_init
func buildModule() (*objects.Module, error) {
	if _, err := builtins.Init(os.Stdout); err != nil {
		return nil, err
	}
	if m, ok := imp.GetModule("builtins"); ok {
		return m, nil
	}
	return nil, errors.New("builtins: Init did not register the builtins module")
}
