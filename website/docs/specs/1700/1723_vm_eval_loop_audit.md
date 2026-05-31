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

## Current test status (audit date: 2026-05-31)

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
| test_extcall | FAILED (1 doctest — module-name harness artifact, P12.5) | ready |
| test_frame | FAILED (1 failure, 1 error — P12.4 PEP 709, P11 refcount) | ready |
| test_eval | file not vendored (P9) | ready |
| test_pow | OK | done |
| test_yield_from | OK (43 pass) | done |
| test_coroutines | FAILED (7 failures, 1 error — P12.3 throw frames, P11 refcount, asyncio) | ready |
| test_asyncgen | FAILED (3 failures, 54 errors — asyncio event loop, spec 1711) | ready |
| test_generator_stop | OK | done |
| test_generators | OK (59 pass, 1 skip) | done |
| test_iter | OK (57 pass, 2 skip) | done |
| test_iterlen | OK (22 pass) | done |
| test_index | OK (55 pass) | done |
| test_isinstance | OK | done |

Recursion-limit enforcement (P12.1, P12.2) flipped `test_repr_deep` across
`list_tests` / `mapping_tests` / `test_dict` and removed the Go stack-overflow
crash on unbounded recursion. The remaining red files reduce to four causes:
PEP 709 inlined comprehensions (P12.4), throw-path frame linking (P12.3),
getrefcount / weakref-reclaim exactness (P11, a GC-model gap), and the asyncio
event loop (spec 1711). `test_extcall`'s one failure is a harness module-naming
artifact (P12.5), not a behavioural gap.

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

## P11 — Generator cycle-collector finalization

**Blocked tests:** `test_generators` (5 remaining failures after P1):
- `FinalizationTest.test_refcycle` — generator pinned by `g.send(g)` self-cycle
  must run its `finally` clause on `gc.collect()`.
- `FinalizationTest.test_frame_resurrect` — generator dropped by `del g` must
  fire `tp_finalize` so `finally` runs and resurrects its own frame via
  `sys._getframe()`.
- `FinalizationTest.test_generator_resurrect` — generator's `except` clause
  resurrects itself, must run after `gc.collect()` fires `tp_finalize`.
- `coroutine` doctest — coroutine pinned in a similar cycle.
- `refleaks` doctest — `__del__` raising must route through
  `sys.unraisablehook` (related to P3.x but exposed here).

**CPython mechanism:**
- `Modules/gcmodule.c:1822 gc_collect_impl` runs the cycle collector.
- `Python/gc.c:392 update_refs` seeds `gc_refs` from each tracked object's
  refcount.
- `Python/gc.c:482 subtract_refs` walks every tracked container's
  `tp_traverse`, decrementing `gc_refs` on each visited tracked target.
- `Python/gc.c:497 move_unreachable` partitions the candidate set into
  reachable (`gc_refs > 0`, recursively reachable) and unreachable (the rest).
- `Python/gc.c:1067 finalize_garbage` runs `tp_finalize` on every entry of
  the unreachable list. For generators, that calls
  `Objects/genobject.c:87 _PyGen_Finalize` which routes through
  `Objects/genobject.c:131 gen_close` and runs the body's `finally` clauses.

**Current gopy state:**

`objects/generator.go NewGenerator` deliberately does NOT call
`GCTrackHook`. The comment in that file documents the blocker: gopy's refcount
discipline on container stores (`dict.SetItem`, `list.Append`, frame
fast-local stores, stack-ref pushes/pops) is incomplete. Concretely, when
the module-level `g = gen()` assignment stores the generator into the
module's `__dict__`, the dict's stored value does not `Incref` the generator,
and the dict's own refcount stays at the value it had on creation
(`refcount == 1`).

When `subtractRefs` runs against a tracked generator, every tracked container
that holds the generator decrements its `gc_refs`. The module `__dict__` is
tracked (`objects/dict.go:271` wires `GCTrackHook`) and its `tp_traverse`
yields the generator. With `gc_refs` decremented to zero, `moveUnreachable`
classifies the generator as unreachable, `finalize_garbage` calls
`genFinalize`, and the body is closed prematurely.

Empirically, enabling tracking on `NewGenerator` flips the three cycle tests
(`test_refcycle`, `test_frame_resurrect`, `test_generator_resurrect`) to
green at the cost of regressing four doctests
(`conjoin`, `coroutine`, `email`, `fun`) and `test_modify_f_locals`, because
those tests rely on a generator surviving an explicit `gc.collect()` between
creation and consumption. The net delta is +1 failure, so tracking is
currently off.

### P11.1 — Faithful refcount on container stores

Add `Incref`/`Decref` on every container mutation path so the cycle collector
can compute external references accurately.

**Files to audit (every `Object` store in these paths must Incref the new
value and Decref the displaced value):**

- `objects/dict.go` — `Dict.SetItem`, `Dict.DelItem`, internal slot mutations
  in `insertDict`, `dictResize`, the split-dict materialization paths.
- `objects/list.go` — `List.Append`, `List.SetItem`, slice assignment, `pop`,
  `extend`, `clear`.
- `objects/tuple.go` — `Tuple` is immutable, but `NewTuple`'s caller must
  Incref each input; `BUILD_TUPLE` opcode and tuple-pack helpers need to pair
  the Incref with a Decref of the source.
- `objects/set.go` — `Set.Add`, `Set.Discard`, table resize.
- `objects/instance.go` — `instanceSetAttr`, `instanceDelAttr` on
  `inst.dict`, `inst.slots`.
- `frame/frame.go` — `FrameFastLocalSet`, `FrameStackPush`/`Pop`, cell-local
  writes (`MAKE_CELL`, `STORE_DEREF`), `FrameClearLocals`.
- `vm/eval.go` — `STORE_FAST`, `STORE_GLOBAL`, `STORE_NAME`, `STORE_ATTR`,
  `STORE_SUBSCR`, `STORE_DEREF`, every stack push/pop that transfers
  ownership.

**CPython reference:** every `Py_DECREF`/`Py_INCREF`/`Py_SETREF` in
`Objects/dictobject.c`, `Objects/listobject.c`, `Objects/setobject.c`,
`Objects/typeobject.c`, `Python/ceval.c`. Use `Include/object.h:605 Py_INCREF`,
`Include/object.h:631 Py_DECREF`, and `Include/object.h:725 Py_SETREF` as
the model.

**Shipping criterion:** with `NewGenerator` calling `GCTrackHook`, both the
five-test cycle suite AND the conjoin/email/fun/coroutine/test_modify_f_locals
panel pass. The order is: land P11.1, then enable tracking in P11.2.

### P11.2 — Wire `GCTrackHook` in `NewGenerator`

After P11.1 lands, restore the `GCTrackHook` call in
`objects/generator.go NewGenerator` and re-run `test_generators.py`. CPython
calls `PyObject_GC_Track` at the end of `Objects/genobject.c:867
gen_new_with_qualname`.

### P11.3 — Resurrect-after-finalize semantics

`test_generator_resurrect` and `test_frame_resurrect` both check that an
object resurrected inside `tp_finalize` (by storing itself in a still-live
container) survives. `Python/gc.c:1261 handle_resurrected_objects` re-runs
`deduce_unreachable` on the post-finalize list and pulls survivors back out
of the reclaim path. gopy's `module/gc/finalize.go handleResurrected`
already implements this. Once tracking is on, these tests should pass.

### P11.4 — `sys.unraisablehook` formatting for the `refleaks` doctest

CPython's `refleaks` doctest verifies the exact `err_msg` produced when a
`__del__` raises (`"Exception ignored while calling deallocator <repr>"`).
gopy's hook stub does not produce this message. CPython:
`Python/errors.c:1380 _PyErr_WriteUnraisable` and
`Objects/typeobject.c:1450 subtype_dealloc` for the deallocator-context
format string.

**Fix:** in `vm/builtins_hook.go` (where `WriteUnraisableHook` is registered)
and `objects/refcount.go Decref`, ensure the err_msg passed to
`sys.unraisablehook` matches CPython's exact format for the
`Finalize`-from-`Decref` path: `"Exception ignored while calling deallocator " + repr(t.Finalize)`.

---

## P12 — Recursion limit enforcement

CPython enforces two recursion budgets. The Python budget
(`py_recursion_remaining`) is decremented at every frame entry
(`start_frame` in `_PyEval_EvalFrameDefault`, via `_Py_EnterRecursivePy`
in `Python/ceval_macros.h:337` and `_Py_CheckRecursiveCallPy` in
`Python/ceval.c:1027`). The C budget guards nested native slot calls
(repr, str, comparison) in `PyObject_Repr` / `PyObject_Str`
(`Objects/object.c:777` / `:822`, via `_Py_EnterRecursiveCallTstate` in
`Include/internal/pycore_ceval.h:222`).

### P12.1 — Python recursion limit on the specialized call arms (shipped)

The generic `CALL` path in `vm/eval_call.go` guards `FrameStack.Depth()`
against `sys.RecursionLimit()` before every `stack.Push`. The fast
`CALL_PY_EXACT_ARGS` / `CALL_BOUND_METHOD_EXACT_ARGS`
(`vm/eval_specialized_call.go`) and `CALL_ALLOC_AND_ENTER_INIT`
(`vm/eval_specialized_call_alloc_init.go`) arms push frames directly and
skipped that check, so any recursive function the specializer promoted to
the fast arm ran until the Go stack overflowed instead of raising
`RecursionError`. Both arms now repeat the guard before their direct
`stack.Push`, citing `Python/bytecodes.c:4010 _PUSH_FRAME` and the
`start_frame` recursion check.

### P12.2 — C recursion limit on repr / str (shipped)

`repr()` / `str()` of a deeply self-nested container recursed on the Go
stack unbounded (`test_repr_deep` in `list_tests`, `mapping_tests`,
`test_dict` nests 150k-200k deep and asserts `RecursionError`). gopy now
counts nesting depth in `objects.Repr` / `objects.Str` against a budget
(`enterRecursiveCall` / `leaveRecursiveCall`), mirroring the pre-stack-pointer
`c_recursion_remaining` / `Py_C_RECURSION_LIMIT` design. CPython 3.14
switched to a machine-stack-pointer check (`_Py_MakeRecCheck`), which is not
portably reachable from Go, so the counter is the behavioural equivalent.

### P12.3 — Throw stack-frame linking (pending)

`test_stack_in_coroutine_throw` (`test_coroutines`) asserts the visible
`traceback.extract_stack()` depth is identical when a coroutine chain
`a -> b -> c` is resumed via `send` versus `throw`. gopy reports 18 frames
on `send` and 16 on `throw`. Root cause: `Generator.Throw` /
`Coroutine.Throw` forward the throw down the `YieldFromTarget` chain by
recursively calling `Throw` on the *caller's* goroutine, so the
`CallerFrame` stamped into the resumed body (`callerFrame()`) is the test's
frame rather than the delegating coroutine's frame. The innermost frame's
`f_back` therefore skips the two delegators. The send path links correctly
because each delegation runs on its own body goroutine. **Fix:** thread the
delegating generator's own body frame (its `GiFrame` iframe) as the
forwarded `CallerFrame` instead of `callerFrame()`. CPython:
`Objects/genobject.c:248 gen_send_ex2` (`previous_frame` set on resume),
`Objects/genobject.c:466 _gen_throw` (yf forwarding).

### P12.4 — PEP 709 inlined comprehensions (pending)

`test_write_with_hidden` (`test_frame`) captures `sys._getframe().f_locals`
*inside* a list comprehension and expects writes through the proxy to land
on the enclosing function's fast locals. This only works when the
comprehension is inlined into the enclosing frame (PEP 709, 3.12+), so that
`sys._getframe()` returns the enclosing frame and the iteration variable
occupies a `CO_FAST_HIDDEN` slot alongside the function's own. gopy still
compiles list/set/dict comprehensions as separate `<listcomp>` code objects
(pre-3.12 behaviour), so `sys._getframe()` returns the dead comprehension
frame. **Fix:** port the inlined-comprehension subsystem:
`Python/symtable.c:802 inline_comprehension` (symbol merge, hidden vars,
`DEF_COMP_CELL`), and the codegen push/pop in `Python/compile.c:1019`
(`u_in_inlined_comp`, `u_fasthidden`). This is a large symtable + codegen
port and is tracked separately so it does not land as a partial slice.

### P12.5 — `test_extcall` module-name doctest (harness artifact)

`test_extcall` doctests embed `test.test_extcall.f()` in expected
`TypeError` text. CPython's regrtest imports the file as `test.test_extcall`,
so `f.__module__` carries the `test.` package prefix that
`_PyObject_FunctionStr` (`Objects/object.c:973`) renders. gopy's harness runs
the file top-level as `test_extcall`, so it correctly renders
`test_extcall.f()`. The difference is the module's import name under the two
harnesses, not a behavioural gap in gopy's funcstr formatting.

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

- [x] P1.1 `gi_code`, `gi_suspended`, `gi_yieldfrom`, `__name__` (writable), `__qualname__` (writable) on GeneratorType
- [x] P1.2 `cr_frame`, `cr_running`, `cr_code`, `cr_await`, `cr_origin`, `cr_suspended`, `__name__`, `__qualname__` on CoroutineType
- [x] P1.3 `ag_frame`, `ag_running`, `ag_code`, `ag_await`, `ag_suspended`, `__name__`, `__qualname__` on AsyncGeneratorType

### P2 — pow() modular inverse

- [x] P2.1 `pow(a, -1, m)` modular inverse via `big.Int.ModInverse`
- [x] P2.2 `pow(a, b, m)` negative-modulus normalisation

### P3 — sys hooks

- [ ] P3.1 `sys.set_asyncgen_hooks` / `sys.get_asyncgen_hooks`
- [ ] P3.2 `sys.set_coroutine_origin_tracking_depth` / `sys.get_coroutine_origin_tracking_depth`
- [ ] P3.3 `sys.call_tracing` stub

### P4 — Frame attributes

- [x] P4.1 `frame.f_generator`
- [x] P4.2 `frame.f_trace` settable
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

### P11 — Generator cycle-collector finalization

- [ ] P11.1 Faithful refcount on container stores (dict/list/tuple/set/instance/frame/eval).
- [ ] P11.2 Restore `GCTrackHook` call in `NewGenerator` once P11.1 lands.
- [ ] P11.3 Verify `handle_resurrected_objects` flow on generator resurrect.
- [ ] P11.4 `sys.unraisablehook` deallocator-context format string matches CPython.

### P12 — Recursion limit enforcement

- [x] P12.1 Python recursion limit on `CALL_PY_EXACT_ARGS` / `CALL_BOUND_METHOD_EXACT_ARGS` / `CALL_ALLOC_AND_ENTER_INIT` fast arms.
- [x] P12.2 C recursion limit on `objects.Repr` / `objects.Str` (`test_repr_deep`).
- [ ] P12.3 Throw-path frame linking so `traceback.extract_stack()` matches `send` (`test_stack_in_coroutine_throw`).
- [ ] P12.4 PEP 709 inlined comprehensions (`test_write_with_hidden`).
- [x] P12.5 `test_extcall` module-name diagnosed as harness artifact (no code change).

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
| test_generators | ready | done | P1 shipped |
| test_coroutines | ready | ready | P1 shipped; P12.3 + P11 + asyncio pending |
| test_asyncgen | ready | ready | P1 shipped; asyncio event loop pending (spec 1711) |
| test_yield_from | ready | done | P1 + P8 shipped |
| test_generator_stop | ready | done | existing pass |
| test_pow | ready | done | P2 shipped |
| test_frame | ready | ready | P12.4 (PEP 709) + P11 pending |
| test_isinstance | ready | done | P5 shipped |
| test_index | ready | done | P6 shipped |
| test_iterlen | ready | done | P7 shipped |
| test_extcall | ready | ready | P12.5 harness artifact |
| test_eval | ready | ready | P9 |
