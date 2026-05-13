// vm-side wiring for ContextVar.get / .set / .reset. The methods need
// to read the running thread's context, but module/contextvars cannot
// import vm without cycling. The hook is installed from the vm side
// so the dependency arrow points vm -> module/contextvars.

package vm

import (
	"github.com/tamnd/gopy/module/contextvars"
)

func init() {
	contextvars.CurrentThreadHook = currentThread
}
