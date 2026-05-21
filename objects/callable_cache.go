// Callable cache: a small set of canonical built-in callables the
// CALL specializer identity-checks to fire its hottest fast arms
// (CALL_LEN for `len(o)`, CALL_ISINSTANCE for `isinstance(o, t)`,
// CALL_LIST_APPEND for `L.append(x)`). CPython stashes the same
// pointers on the interpreter struct as
// interp->callable_cache.{len, isinstance, list_append} and the
// specializer reads them through PyInterpreterState_GET().
//
// gopy doesn't carry a per-interpreter struct, so we keep the cache
// as package-level pointers populated lazily at builtin / list type
// init time. specialize/call.go reads them through the accessors
// below.
//
// CPython: Include/internal/pycore_interp.h _PyCallableCache
// CPython: Python/specialize.c:2143 specialize_c_call (interp->callable_cache.len)
// CPython: Python/specialize.c:2162 specialize_c_call (interp->callable_cache.isinstance)
// CPython: Python/specialize.c:2039 specialize_method_descriptor (interp->callable_cache.list_append)

package objects

var (
	callableCacheLen        *BuiltinFunction
	callableCacheIsinstance *BuiltinFunction
	callableCacheListAppend *MethodDescr
)

// RegisterCallableCacheLen stores the canonical `len` BuiltinFunction
// the specializer compares against.
//
// CPython: Python/pylifecycle.c init_interp_main (assigns interp->callable_cache.len)
func RegisterCallableCacheLen(bf *BuiltinFunction) { callableCacheLen = bf }

// RegisterCallableCacheIsinstance stores the canonical `isinstance`
// BuiltinFunction the specializer compares against.
//
// CPython: Python/pylifecycle.c init_interp_main (assigns interp->callable_cache.isinstance)
func RegisterCallableCacheIsinstance(bf *BuiltinFunction) { callableCacheIsinstance = bf }

// RegisterCallableCacheListAppend stores the canonical list.append
// MethodDescr the specializer compares against. CPython picks
// CALL_LIST_APPEND when the resolved descriptor is identical to this
// pointer AND the following bytecode is POP_TOP AND oparg == 1.
//
// CPython: Python/pylifecycle.c init_interp_main (assigns interp->callable_cache.list_append)
func RegisterCallableCacheListAppend(d *MethodDescr) { callableCacheListAppend = d }

// CallableCacheLen returns the registered `len` callable.
func CallableCacheLen() *BuiltinFunction { return callableCacheLen }

// CallableCacheIsinstance returns the registered `isinstance` callable.
func CallableCacheIsinstance() *BuiltinFunction { return callableCacheIsinstance }

// CallableCacheListAppend returns the registered list.append descriptor.
func CallableCacheListAppend() *MethodDescr { return callableCacheListAppend }
