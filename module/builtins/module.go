// builtins built-in module. Registers the builtins dict as an inittab
// entry so `import builtins` resolves in the stdlib without requiring
// cmd/gopy/main.go initialization. The builtins dict is also what
// every module frame uses as its implicit __builtins__ namespace.
//
// CPython: Python/pylifecycle.c:1413 init_interp_main (builtins in interp->modules)
// CPython: Modules/builtinsmodule.c:1 PyInit_builtins

package builtins

import (
	"os"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("builtins", buildModule)
}

// buildModule creates the builtins module by calling builtins.Init with
// os.Stdout as the print target, then wrapping the resulting dict in a
// module object.
//
// CPython: Modules/builtinsmodule.c:3116 builtin_init
func buildModule() (*objects.Module, error) {
	d, err := builtins.Init(os.Stdout)
	if err != nil {
		return nil, err
	}
	return objects.NewModuleWithDict("builtins", d), nil
}
