// vm-side wiring for _warnings.getWarningsContext. The function needs
// to read the running thread's ContextVar, but module/_warnings cannot
// import vm without cycling. The hook is installed from the vm side so
// the dependency arrow points vm -> module/_warnings.
//
// CPython: Python/_warnings.c:267 get_warnings_context (PyContextVar_Get)

package vm

import (
	warnings "github.com/tamnd/gopy/module/_warnings"
)

func init() {
	warnings.CurrentThreadHook = currentThread
}
