---
id: "1723"
slug: 1723
title: "1723: VM / eval loop audit — 22-file gap analysis and CPython parity fixes"
sidebar_label: "1723 VM eval loop audit"
description: "Full audit of the 22 VM/eval-loop test files from spec 1700 against CPython 3.14. Nine tests are green and flipped to done. Thirteen tests have documented CPython-source-of-truth gaps. Every gap is traced to the exact CPython file and line."
---

## Status

Active. Branch `feat/v0.12.7-vm-audit`.

## Goal

Run every test in the spec 1700 VM/eval-loop panel (22 files), confirm which
pass end-to-end today, flip those to `done` in spec 1700, and document every
remaining gap with a CPython citation and a concrete fix plan.

Sources of truth: `$HOME/cpython-314/`. Every cited function was read from
that tree.

---

## Current test status (audit date: 2026-05-29)

| Test | Result | Spec-1700 mark |
|------|--------|----------------|
| test_call | OK (4 pass, 182 skipped — _testcapi) | done |
| test_dynamic | OK | done |
| test_richcmp | OK | done |
| test_compare | OK | done |
| test_unary | OK | done |
| test_augassign | OK | done |
| test_with | OK | done |
| test_contains | OK | done |
| test_typechecks | OK | done |
| test_extcall | FAILED (1 failure) | ready |
| test_frame | FAILED (17 failures, 9 errors) | ready |
| test_eval | file not vendored | ready |
| test_pow | OK | done |
| test_yield_from | FAILED (15 failures, 30 errors) | ready |
| test_coroutines | PANIC (goroutine deadlock) | ready |
| test_asyncgen | FAILED (4 failures, 79 errors) | ready |
| test_generator_stop | FAILED (2 errors) | ready |
| test_generators | FAILED (20 failures, 19 errors) | ready |
| test_iter | FAILED (1 failure) — tracked in spec 1722 P5 | ready |
| test_iterlen | FAILED (18 failures) | ready |
| test_index | FAILED (1 failure, 20 errors) | ready |
| test_isinstance | OK | done |

---

## P1 — Generator / coroutine / async-generator attribute gaps

**Blocked tests:** test_generators (20 fail, 19 errors), test_coroutines (panic),
test_asyncgen (4 fail, 79 errors), test_yield_from (partial).

**CPython file:** `Objects/genobject.c`

### P1.1 — `GeneratorType` missing attributes

CPython's `generator_getsetlist` at `Objects/genobject.c:813`:

```c
{"__name__",    gen_get_name,     gen_set_name,     ...},
{"__qualname__",gen_get_qualname, gen_set_qualname, ...},
{"gi_yieldfrom",gen_getyieldfrom, NULL,             ...},
{"gi_suspended",gen_getsuspended, NULL,             NULL},
{"gi_code",     gen_getcode,      NULL,             NULL},
```

`gi_running` and `gi_frame` are present in gopy. The five above are absent.

**Status in gopy:** `objects/generator.go` only wires `gi_running` and `gi_frame`.

**Failing tests:**
- `g.__name__` / `g.__qualname__` raise `AttributeError`.
- `g.gi_code` raises `AttributeError`.
- `g.gi_suspended` raises `AttributeError`.
- `g.gi_yieldfrom` raises `AttributeError`.

**Fix:** add `GetSetDescr` entries on `GeneratorType` for each missing attribute.
`gen_get_name` returns `gen->gi_name`, `gen_get_qualname` returns
`gen->gi_qualname`, both are settable. `gi_code` returns the code object from the
frame. `gi_suspended` returns a bool (frame state is FRAME_SUSPENDED).
`gi_yieldfrom` returns the object the generator is delegating to via `yield from`.
CPython citations:
- `Objects/genobject.c:793 gen_getframe`
- `Objects/genobject.c:809 gen_getcode`
- `Objects/genobject.c:813 gen_getsetlist`
- `Objects/genobject.c:817 gen_getyieldfrom`
- `Objects/genobject.c:821 gen_getsuspended`

### P1.2 — `CoroutineType` missing all attributes and frame

CPython's `coro_getsetlist` at `Objects/genobject.c:1170`:

```c
{"__name__",    gen_get_name,      gen_set_name,     ...},
{"__qualname__",gen_get_qualname,  gen_set_qualname, ...},
{"cr_await",    coro_get_cr_await, NULL,             ...},
{"cr_running",  cr_getrunning,     NULL,             NULL},
{"cr_origin",   ..., Py_READONLY},
```

Plus `cr_frame` (returns the frame object) and `cr_code` (code object), both via
`_gen_getframe` / `_gen_getcode` at `Objects/genobject.c:1160–1167`.

**Status in gopy:** `objects/coroutine.go` registers only `send`, `throw`, `close`.
None of the `cr_*` attributes or `__name__`/`__qualname__` exist.

**Fix:** mirror gen_getsetlist approach: add GetSetDescr entries for all
`cr_*` fields and `__name__`/`__qualname__` on `CoroutineType`.

### P1.3 — `AsyncGeneratorType` missing attributes

CPython's `async_gen_getsetlist` at `Objects/genobject.c:1604`:

```c
{"__name__",    gen_get_name,     gen_set_name,     ...},
{"__qualname__",gen_get_qualname, gen_set_qualname, ...},
{"ag_await",    coro_get_cr_await, NULL,            ...},
{"ag_running",  ..., Py_READONLY},
```

Plus `ag_frame` at `Objects/genobject.c:1584` and `ag_code` at `Objects/genobject.c:1590`.

**Status in gopy:** similar gap as CoroutineType.

**Fix:** add all `ag_*` attribute descriptors on `AsyncGeneratorType` the same way.

---

## P2 — `pow(a, -1, m)` modular inverse not implemented

**Blocked test:** test_pow (2 failures, 10000 errors — parameterised subtests).

**CPython function:** `Objects/longobject.c:4956 long_pow`

```c
/* if exponent is negative, negate the exponent and
   replace the base with a modular inverse */
if (_PyLong_IsNegative(b)) {
    _PyLong_Negate(&b);
    temp = long_invmod(a, c);   // modular inverse of a mod |c|
    Py_SETREF(a, temp);
}
```

When the three-argument form `pow(a, b, m)` has a negative `b`, CPython
computes `pow(modinv(a, |m|), -b, |m|)` using the extended Euclidean
algorithm in `long_invmod` (`Objects/longobject.c:4818`).

**Status in gopy:** `objects/long_arith.go:146 intPower` returns `notImplemented()`
when `b < 0 && mod != nil`. Callers see `TypeError: unsupported operand type(s) for pow()`.

**Fix:**
1. When `b.Sign() < 0` and `mod != nil`, negate `b`, then compute the modular
   inverse of `a` mod `|m|` using Go's `new(big.Int).ModInverse(&ai.v, absM)`.
2. If `ModInverse` returns nil (gcd != 1, no inverse exists) raise
   `ValueError: base is not invertible for the given modulus`.
3. Then raise `ValueError` when `m == 0` (already done) and when `|m| == 1` return 0.
4. CPython also normalises a negative modulus to positive at
   `Objects/longobject.c:4916`, replicate that.

---

## P3 — sys asyncgen / coroutine hooks absent

**Blocked tests:** test_asyncgen (many errors), test_coroutines (partial).

**CPython file:** `Python/sysmodule.c`

### P3.1 — `sys.set_asyncgen_hooks` / `sys.get_asyncgen_hooks`

CPython: `Python/sysmodule.c:1436 sys_set_asyncgen_hooks`,
`Python/sysmodule.c:1508 sys_get_asyncgen_hooks_impl`.

The hooks are stored in the interpreter state:
`_PyEval_SetAsyncGenFinalizer` / `_PyEval_GetAsyncGenFinalizer` and
`_PyEval_SetAsyncGenFirstiter` / `_PyEval_GetAsyncGenFirstiter`
(`Python/ceval.c`).

**Status in gopy:** absent from `module/sys/sys.go`. `sys.set_asyncgen_hooks()`
raises `AttributeError`.

**Fix:** add two callable entries in `module/sys/sys.go`:
- `set_asyncgen_hooks(firstiter=None, finalizer=None)` — store in a global
  in `vm/` that is readable from the asyncgen dispatch path.
- `get_asyncgen_hooks()` — returns `asyncgen_hooks(firstiter=..., finalizer=...)`.
The named-tuple return type can use a simple `*Namespace` for now.

### P3.2 — `sys.set_coroutine_origin_tracking_depth` / `sys.get_coroutine_origin_tracking_depth`

CPython: `Python/sysmodule.c:1392 sys_set_coroutine_origin_tracking_depth_impl`.

Sets an integer depth; the coroutine object records the call stack at creation
time to populate `cr_origin`.

**Status in gopy:** absent. `cr_origin` attribute also absent (see P1.2).

**Fix:** store a thread-local depth counter. When depth > 0, capture
the top-`depth` frames at coroutine creation and store as `cr_origin`.

### P3.3 — `sys.call_tracing`

CPython: `Python/sysmodule.c:2172 sys_call_tracing_impl` calls
`_PyEval_CallTracing(func, args)`.

Saves the tracing state of the current thread, calls `func(*args)` with
tracing enabled, then restores the state.

**Status in gopy:** absent.

**Fix:** add `sys.call_tracing(func, args)` that temporarily sets
`sys.settrace(None)` around the call, or simply calls `func(*args)` in gopy
(since gopy has a stub tracing layer). Mark as partial-stub in the checklist.

---

## P4 — Frame attribute gaps

**Blocked test:** test_frame (17 failures, 9 errors).

**CPython file:** `Objects/frameobject.c`

### P4.1 — `frame.f_generator` attribute missing

CPython: `Objects/frameobject.c frame_getsetlist`, entry `f_generator` at
`Objects/frameobject.c:722`. Returns the generator or coroutine object that
owns this frame, or `None` for non-generator frames.

**Status in gopy:** `frame.f_generator` raises `AttributeError`.

**Fix:** add a `GetSetDescr` for `f_generator` on `FrameType` that returns
`frame.Generator` (or `None()`).

### P4.2 — `frame.f_trace` must be settable

CPython: `Objects/frameobject.c:680 frame_settrace`. Setting `f_trace` to a
callable enables per-line tracing for that frame; setting it to `None` disables.

**Status in gopy:** `f_trace` is declared read-only; attempts to assign raise
`TypeError: 'frame' object has only read-only attributes (assign to .f_trace)`.

**Fix:** implement the setter in the `f_trace` GetSetDescr. Store the callable
on the frame struct. The existing tracing hook infrastructure can wire it in.

### P4.3 — `sys.unraisablehook` not installed

CPython: `Python/sysmodule.c sys_unraisablehook`. When an exception is raised
in a context where it cannot be propagated (e.g., `__del__`), CPython calls
`sys.unraisablehook`.

**Status in gopy:** `sys` module has no `unraisablehook` attribute.
`test_frame` accesses it and raises `AttributeError: module has no attribute 'unraisablehook'`.

**Fix:** expose `sys.unraisablehook` as a settable attribute in `module/sys/sys.go`.
Default value is `sys.__unraisablehook__` (the built-in printer).

### P4.4 — `frame.clear()` while frame is executing should raise `RuntimeError`

CPython: `Objects/frameobject.c:559 frameobj_clear`. When the frame is
currently on the call stack (`frame_state >= FRAME_EXECUTING`), raises
`RuntimeError: cannot clear an executing frame`.

**Status in gopy:** `frame.clear()` does not check this guard.

**Fix:** check `frame.IsExecuting()` in the `clear` method descriptor and raise
`RuntimeError` when true.

---

## P5 — isinstance / issubclass with `types.UnionType`

**Blocked test:** test_isinstance (1 failure, 16 errors).

**CPython file:** `Objects/unionobject.c`

CPython's `UnionType` implements `__instancecheck__` and `__subclasscheck__`:

```c
// Objects/unionobject.c:234 union_instancecheck
static int
union_nb_bool(PyObject *self) ...
```

`isinstance(x, int | str)` calls `UnionType.__instancecheck__` which iterates
the union's `__args__` tuple and returns true if `isinstance(x, arg)` is true
for any member.

**Status in gopy:** `objects/union_type.go` implements `UnionTypeType` for the `|`
operator and PEP 604 type union syntax, but `isinstance(x, union)` raises
`TypeError: isinstance() arg 2 must be a type, a tuple of types, or a union`
because the `abstract.go isinstance` path doesn't recognise `*UnionType` as a
valid class arg.

**Fix:**
1. In `builtins/funcs.go isinstanceBuiltin`, after the tuple-of-types branch,
   add a `*objects.UnionType` case that checks `isinstance(x, arg)` for each
   member in `ut.Args`.
2. Similarly in `issubclassBuiltin`, handle `*objects.UnionType` by iterating
   members.
3. CPython citations: `Objects/unionobject.c:234 union_instancecheck`,
   `Objects/unionobject.c:257 union_subclasscheck`.

---

## P6 — `__index__` not called in subscript paths

**Blocked test:** test_index (1 failure, 20 errors).

**CPython mechanism:** `Objects/abstract.c:1666 PySequence_GetItem` calls
`PyNumber_AsSsize_t(o, PyExc_IndexError)` which invokes `__index__` on the
subscript before using it. All sequence `tp_as_sequence->sq_item` slots accept
any object with `__index__`.

**Status in gopy:** `bytes.__getitem__`, `bytearray.__getitem__`, `list.__getitem__`,
`tuple.__getitem__` all do a direct `*Int` type assertion. An object with
`__index__` (e.g. a class returning `2` from `__index__`) raises
`TypeError: TYPE indices must be integers or slices, not OTHERTYPE` without
calling `__index__`.

**Failing test:** `class NewStyle: def __index__(self): return 2; b"abc"[NewStyle()]`.

**Fix:** in each `__getitem__` that currently asserts `*Int`, add a fallback
path that calls `objects.Index(subscript)` (the `__index__` protocol helper)
before raising `TypeError`. CPython citations:
- `Objects/bytesobject.c:1330 bytes_subscript` — calls `PySlice_Unpack` or
  `PyNumber_AsSsize_t` (which calls `__index__`).
- `Objects/listobject.c:2853 list_subscript`
- `Objects/tupleobject.c:880 tuplesubscript`

---

## P7 — `__length_hint__` missing on iterator types

**Blocked test:** test_iterlen (18 failures).

**CPython mechanism:** `Objects/abstract.c:473 PyObject_LengthHint` tries
`__len__` then `__length_hint__`. CPython's container iterators expose
`__length_hint__` via a `sq_length` slot on their iterator type.

**Status in gopy:** deque iterators (`_collections.deque_iterator`,
`deque_reverseiterator`) and dict view iterators (`dict_keyiterator`,
`dict_valueiterator`, `dict_itemiterator`) expose no `__length_hint__` method.
`operator.length_hint(iter(deque()))` returns 0 instead of the deque's
remaining length.

**Fix:** for each iterator type listed above:
1. Add a `__length_hint__` method descriptor.
2. Return `len(container) - self.index` (remaining items).
3. CPython citations:
   - `Modules/_collectionsmodule.c:1480 dequeiter_len` (used as `sq_length`)
   - `Objects/dictobject.c:4060 dictiter_len` (used as `sq_length`)
   These are `sq_length` slots that the abstract layer surfaces as `__length_hint__`.

---

## P8 — Generator exception-handling edge cases

**Blocked tests:** test_yield_from (15 failures, 30 errors), test_generator_stop
(2 errors), test_generators (partial).

**CPython file:** `Objects/genobject.c`

### P8.1 — `gen.throw()` exception identity on close

`test_yield_from.test_close_and_throw_raise_base_exception` and related tests
call `gen.close()` or `gen.throw(SomeException)` and check that the raised
exception is the exact same object (using `assertIs`). The test fails with
messages like `Exception('GeneratorExit') is not BaseException()`, which means
gopy wraps or re-creates exceptions instead of propagating the original object.

CPython: `Objects/genobject.c:378 gen_throw` passes the exception through
without re-raising it as a new instance.

**Fix:** audit `vm/eval_gen.go genThrow` and `objects/generator.go` to ensure
the exception object identity is preserved through throw/close dispatch.
CPython citation: `Objects/genobject.c:378 gen_close` + `gen_throw`.

### P8.2 — `generator.close()` wraps `GeneratorExit` incorrectly

Several tests in test_yield_from assert that after `gen.close()`, the
exception seen at the call site is exactly `GeneratorExit()`, not a
re-wrapped instance. gopy's `close()` may create a fresh `GeneratorExit`
instead of throwing the original.

**Fix:** in `genCloseMethod`, pass a single shared `GeneratorExit` sentinel
through to the throw path, and return it directly if the generator raises it.
CPython citation: `Objects/genobject.c:436 gen_close`.

### P8.3 — PEP 479 `StopIteration` escapes a generator

`test_generator_stop` tests that `StopIteration` raised inside a generator
body is converted to `RuntimeError: generator raised StopIteration`.

**Status in gopy:** the conversion may be missing in the `YIELD_VALUE` unwind
path.

**Fix:** in `vm/eval_gen.go` at the point where a generator body raises
`StopIteration`, check if the frame is a generator frame and raise
`RuntimeError` instead. CPython citation:
`Objects/genobject.c:198 _PyGen_SetStopIterationValue` path and
`Python/bytecodes.c YIELD_VALUE` PEP 479 check.

---

## P9 — `test_eval` not vendored

`test/cpython/test_eval.py` does not exist. The test_eval row in spec 1700
is marked `ready` but the file was never vendored.

**Fix:** vendor `$HOME/cpython-314/Lib/test/test_eval.py` into
`test/cpython/test_eval.py` and run it to confirm pass/fail count before
updating spec 1700.

---

## P10 — `test_extcall` doctest module-name mismatch

**Blocked test:** test_extcall (1 failure).

The single failure is in a `doctest` block that checks the exact text of
`TypeError` messages. CPython prefixes the function name with the module
(`test.test_extcall.f()`), but gopy's error messages use `__main__.f()`.

**CPython function:** `Python/ceval.c` error message formatting for CALL
opcodes uses `PyObject_GetQualName` on the callable.

**Status in gopy:** `objects/call.go` and the error-formatting helpers in
`vm/` build the TypeError message using `fn.Name` without a module prefix.

**Fix:** in `objects.UnexpectedKeywordError`, `objects.TooManyPositionalError`,
and related formatters, look up `fn.__module__` and prepend it when not
`"__main__"`. CPython citation: `Python/ceval.c:1546 positional_only_passed_as_keyword`
uses `PyObject_GetQualName` on the callable for the error prefix.

---

## Checklist

### Tests to flip to done

- [x] test_call — all non-_testcapi tests pass
- [x] test_dynamic — OK
- [x] test_richcmp — OK
- [x] test_compare — OK
- [x] test_unary — OK
- [x] test_augassign — OK
- [x] test_with — OK
- [x] test_contains — OK
- [x] test_typechecks — OK

### P1 — Generator / coroutine / asyncgen attributes

- [ ] P1.1 `gi_code`, `gi_suspended`, `gi_yieldfrom`, `__name__` (writable), `__qualname__` (writable) on GeneratorType
- [ ] P1.2 `cr_frame`, `cr_running`, `cr_code`, `cr_await`, `cr_origin`, `__name__`, `__qualname__` on CoroutineType
- [ ] P1.3 `ag_frame`, `ag_running`, `ag_code`, `ag_await`, `__name__`, `__qualname__` on AsyncGeneratorType

### P2 — pow() modular inverse

- [x] P2.1 `pow(a, -1, m)` modular inverse via `big.Int.ModInverse`
- [x] P2.2 `pow(a, b, m)` negative-modulus normalisation

### P3 — sys hooks

- [ ] P3.1 `sys.set_asyncgen_hooks` / `sys.get_asyncgen_hooks`
- [ ] P3.2 `sys.set_coroutine_origin_tracking_depth` / `sys.get_coroutine_origin_tracking_depth`
- [ ] P3.3 `sys.call_tracing` stub

### P4 — Frame attributes

- [ ] P4.1 `frame.f_generator`
- [ ] P4.2 `frame.f_trace` settable
- [ ] P4.3 `sys.unraisablehook` settable attribute
- [ ] P4.4 `frame.clear()` executing-frame guard

### P5 — isinstance / issubclass with UnionType and abstract classes

- [x] P5.1 `isinstance(x, int | str)` via `*UnionType` case in `objectIsInstance`
- [x] P5.2 `issubclass(T, int | str)` via `*UnionType` case in `objectIsSubclassObj`
- [x] P5.3 abstract class protocol (`checkClass` + `abstractIsSubclass` + `ClearCurrentExceptionHook`)
- [x] P5.4 AttributeError masking: clear thread-state exception to avoid stale exception on synthesize path

### P6 — __index__ in subscript paths

- [ ] P6.1 `bytes.__getitem__` calls `objects.Index` on non-int subscript
- [ ] P6.2 `bytearray.__getitem__` calls `objects.Index`
- [ ] P6.3 `list.__getitem__` calls `objects.Index`
- [ ] P6.4 `tuple.__getitem__` calls `objects.Index`

### P7 — __length_hint__ on iterators

- [ ] P7.1 deque iterator `__length_hint__`
- [ ] P7.2 deque reverse iterator `__length_hint__`
- [ ] P7.3 dict key/value/item iterator `__length_hint__`

### P8 — Generator exception handling

- [ ] P8.1 `gen.throw()` preserves exception object identity
- [ ] P8.2 `gen.close()` does not re-wrap GeneratorExit
- [ ] P8.3 PEP 479 StopIteration → RuntimeError conversion in generator body

### P9 — Missing test file

- [ ] P9.1 vendor `test_eval.py` from CPython 3.14 Lib/test/

### P10 — Error message module prefix

- [ ] P10.1 TypeError messages use `module.funcname()` not `__main__.funcname()` for non-main modules

### Spec 1700 rows advanced

| Test | Before | After | Unblocked by |
|------|--------|-------|--------------|
| test_call | ready | done | existing pass |
| test_dynamic | ready | done | existing pass |
| test_richcmp | ready | done | existing pass |
| test_compare | ready | done | existing pass |
| test_unary | ready | done | existing pass |
| test_augassign | ready | done | existing pass |
| test_with | ready | done | existing pass |
| test_contains | ready | done | existing pass |
| test_typechecks | ready | done | existing pass |
| test_generators | ready | ready | P1 |
| test_coroutines | ready | ready | P1, P3 |
| test_asyncgen | ready | ready | P1, P3 |
| test_yield_from | ready | ready | P1, P8 |
| test_generator_stop | ready | ready | P8 |
| test_pow | ready | done | P2 shipped |
| test_frame | ready | ready | P4 |
| test_isinstance | ready | done | P5 shipped |
| test_index | ready | ready | P6 |
| test_iterlen | ready | ready | P7 |
| test_extcall | ready | ready | P10 |
| test_eval | ready | ready | P9 |
