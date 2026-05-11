// vm-side wiring for sys.exc_info. The builtin needs to read the
// running thread's handled-exception slot, but module/sys cannot
// import vm without cycling back through the import machinery.
// Installing the hook from the vm side keeps the dependency arrow
// pointing vm -> module/sys.

package vm

import (
	"github.com/tamnd/gopy/module/sys"
)

func init() {
	sys.CurrentThreadHook = currentThread
}
