// Package intrinsics is the dispatch surface for CALL_INTRINSIC_1
// and CALL_INTRINSIC_2. The compiler emits these opcodes for runtime
// helpers that don't deserve a dedicated bytecode (print expr,
// list-to-tuple finalization, PEP 695 type-param construction,
// import-star, exception-group re-raise prep). Oparg selects an
// entry in UnaryTable / BinaryTable.
//
// In v0.6 most helpers are stubs that return notImplemented errors:
// the table shape and ID numbering land here so the eval loop in
// 1636 can be generated against real symbols. Helper bodies fill in
// as cross-block prereqs land (PEP 695 in 1689, exceptions in 1686,
// sys.displayhook in 1651, import in 1683, function/method slots in
// 1685).
//
// CPython: Python/intrinsics.c
// CPython: Include/internal/pycore_intrinsics.h

package intrinsics

import "fmt"

// notImplementedError signals a deferred helper. The eval loop
// surfaces it as a runtime error pointing at the spec block that
// owns the missing piece.
type notImplementedError struct {
	helper string
	reason string
}

func (e *notImplementedError) Error() string {
	return fmt.Sprintf("intrinsics: %s not implemented in v0.6: %s", e.helper, e.reason)
}

func notImplemented(helper, reason string) error {
	return &notImplementedError{helper: helper, reason: reason}
}
