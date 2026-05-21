package sys

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// auditHooks holds the list of audit hooks registered through
// sys.addaudithook. The slice is the Python-visible mirror of
// CPython's interpreter-state audit_hooks list. Hooks are appended
// in registration order; sys.audit walks them left-to-right.
//
// CPython: Python/sysmodule.c:537 sys_addaudithook_impl
var auditHooks []objects.Object

// sysAudit ports sys.audit(event, *args). When no audit hooks are
// registered the call is effectively a no-op that returns None,
// which matches CPython's should_audit short-circuit at the top of
// sys_audit_impl. When hooks are registered each one is invoked
// with (event, args) and any raised exception aborts the dispatch
// the same way CPython propagates the first hook error.
//
// CPython: Python/sysmodule.c:565 sys_audit_impl
func sysAudit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: audit() takes at least 1 argument (0 given)")
	}
	if args[0].Type() != objects.StrType() {
		return nil, fmt.Errorf("TypeError: audit() argument 'event' must be str, not %s", args[0].Type().Name)
	}
	if len(auditHooks) == 0 {
		return objects.None(), nil
	}
	event := args[0]
	hookArgs := objects.NewTuple(args[1:])
	callArgs := objects.NewTuple([]objects.Object{event, hookArgs})
	for _, hook := range auditHooks {
		if _, err := objects.Call(hook, callArgs, nil); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// sysAddAuditHook ports sys.addaudithook(hook). CPython invokes the
// existing hooks first with "sys.addaudithook" so they can veto
// installation; the audit dispatch in sysAudit handles that
// recursively when entries already exist.
//
// CPython: Python/sysmodule.c:521 sys_addaudithook_impl
func sysAddAuditHook(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: addaudithook() takes exactly one argument (%d given)", len(args))
	}
	if _, err := sysAudit([]objects.Object{objects.NewStr("sys.addaudithook")}, nil); err != nil {
		return nil, err
	}
	auditHooks = append(auditHooks, args[0])
	return objects.None(), nil
}
