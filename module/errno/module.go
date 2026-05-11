// Package errno is the gopy port of CPython's built-in errno module.
// It exposes the host platform's POSIX errno integer constants (EPERM,
// ENOENT, ...) as module attributes plus an `errorcode` dict that
// maps each integer value back to its uppercase name string.
//
// The CPython source iterates a long #ifdef-guarded list inside
// errno_exec; gopy mirrors that with per-GOOS files (entries_darwin.go,
// entries_linux.go, entries_other.go) so cross-compile stays clean.
// Each file returns the (name, code) pairs that exist on that platform.
//
// CPython: Modules/errnomodule.c:88 errno_exec
package errno

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("errno", buildModule)
}

// errnoEntry is one (name, code) pair pulled from the host platform's
// syscall package. Mirrors the args to errnomodule.c's add_errcode
// macro (the comment field is dropped: Python only sees name + code).
//
// CPython: Modules/errnomodule.c:105 add_errcode
type errnoEntry struct {
	name string
	code int
}

// buildModule constructs the errno module dict. Mirrors errno_exec on
// the C side: install every platform-available E* constant as a module
// attribute, then publish the reverse `errorcode` dict that maps the
// integer code back to the uppercase name string.
//
// CPython: Modules/errnomodule.c:88 errno_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("errno")
	d := m.Dict()
	errorcode := objects.NewDict()
	if err := d.SetItem(objects.NewStr("errorcode"), errorcode); err != nil {
		return nil, err
	}
	for _, e := range errnoEntries() {
		if err := addErrcode(d, errorcode, e.name, e.code); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// addErrcode inserts one errno name/code pair into the module dict and
// the reverse `errorcode` dict. Mirrors _add_errcode in the C source:
// the module dict gets name -> code, the errorcode dict gets
// code -> name. If a code already maps to a name (some platforms alias
// values, e.g. Linux's EDEADLOCK == EDEADLK), the first entry wins,
// matching CPython where successive PyDict_SetItem calls last-write
// for the integer key. We keep first-write to keep `errorcode[x]`
// stable across runs.
//
// CPython: Modules/errnomodule.c:58 _add_errcode
func addErrcode(moduleDict, errorcode *objects.Dict, name string, code int) error {
	nameObj := objects.NewStr(name)
	codeObj := objects.NewInt(int64(code))
	if err := moduleDict.SetItem(nameObj, codeObj); err != nil {
		return err
	}
	// Preserve the first name assigned to a given code so aliases like
	// EDEADLOCK/EDEADLK on Linux do not flip errorcode[35] back and
	// forth depending on iteration order.
	have, err := errorcode.Contains(codeObj)
	if err != nil {
		return err
	}
	if have {
		return nil
	}
	return errorcode.SetItem(codeObj, nameObj)
}
