---
id: "1723"
slug: 1723
title: "1723: VM / eval loop audit — 22-file gap analysis and CPython parity fixes"
sidebar_label: "1723 VM eval loop audit"
description: "Full audit of the 22 VM/eval-loop test files from spec 1700 against CPython 3.14. Nine tests are green and flipped to done. Thirteen tests have documented CPython-source-of-truth gaps. Every gap is traced to the exact CPython file and line."
---

## Status

Reopened 2026-06-10 under the zero-skip conformance standard (spec 1726).
The 2026-06-09 "complete" sign-off counted the `@cpython_only` and
`requires _testcapi` skips as acceptable. They are not: gopy must reproduce
CPython's own run/skip decisions exactly, and CPython does not skip those.
Running the panel under CPython 3.14 itself (same vendored files) skips only
the FrameLocalsProxy design tests in `test_frame` (8 unconditional
`@unittest.skip`s like "Unlike a mapping: no proxy.update"). Everything else
CPython runs. So every other gopy skip is a real gap.

The earlier source-level audit still holds for the eval loop itself: a fresh
sweep found no behavioural defers (the residual "not implemented" strings are
defensive switch defaults that no real bytecode reaches, e.g. the `BINARY_OP`
suboperator default in `vm/eval_simple.go` which already covers all 27 NB_
codes, plus the `FOR_ITER_GEN` specialization that intentionally falls through
to the correct generic `FOR_ITER` body). The gaps are object-layer and harness
gaps the skips were hiding, now tracked in spec 1726:

- `@cpython_only` tests now run (gopy is treated as cpython for impl-detail
  gating, since gopy commits to CPython implementation-detail parity). This
  exposed real arg-count message drift the funcstr sweep (spec 1725) had not
  reached: `test_call` alone has ~29 such failures.
- `requires _testcapi` / `_testinternalcapi` tests need those test C-extension
  modules ported (vectorcall.c heap types, etc.).
- `test_frame` needs `ctypes` for one C-API test.
- P11 (container refcount discipline) is no longer a free pass: it is exactly
  what `test_iter.test_ref_counting_behavior` and the `_testcapi` getrefcount
  tests measure.

Fixed so far on this pass: `dict.__contains__` rebind to METH_O (kwargs are
rejected before arity, matching `Objects/clinic/dictobject.c.h:66`), the
`PySeqIter` index overflow guard (`Objects/iterobject.c:64`, clears
`test_iter.test_iter_overflow`), and the `test_call` `@cpython_only` arg-count
message drift (getattr/hasattr/setattr/delattr rebound to METH_FASTCALL,
dict.get/pop/setdefault/fromkeys + classmethod/staticmethod routed through
`CheckPositional`, ImportError keyword `__init__`, module-not-callable
suggestion, and a faithful `Python/suggestions.c` port for the "Did you mean"
tails).

## Full-panel re-run (2026-06-10)

Reran all 21 in-scope files (test_eval stays out-of-scope, no standalone 3.14
module). 18 are fully green. Three carry a combined five failures, and every
one traces to a subsystem the spec already tracks as a whole-port, not a
partial slice:

| File | Result | Remaining failures | Root cause |
|------|--------|--------------------|------------|
| test_call | 2 fail, 1 error | test_margin_is_sufficient (error) | needs `_testinternalcapi.get_stack_margin` (spec 1726 #245) |
| | | test_varargs18_kw | clinic `_PyArg_UnpackKeywords` with real BadStr key objects + ordered kwargs (spec 1726 #243) |
| | | test_unexpected_keyword_suggestion_via_getargs | same clinic path: `str.split` suggestion text + ordered ImportError kwargs (#243) |
| test_frame | 1 fail | test_clear_refcycles | `frame.clear()` must decref locals so a pure-refcount (gc disabled) cycle breaks: P11 container refcount discipline |
| test_iter | 1 fail | test_ref_counting_behavior | failed unpack `a, b = iter(l)` plus `del l` must deterministically run `__del__` at refcount 0: P11 (spec 1726 #246) |

These are not skips: gopy runs every one of these tests (the `@cpython_only`
bridge is live). They fail because gopy does not yet reproduce CPython's exact
deterministic destruction (P11) or carry the test C-extension modules. P11 is
deliberately a whole-subsystem port (`objects/dict.go`, `list.go`, `tuple.go`,
`set.go`, `instance.go`, `frame/frame.go`, `vm/eval.go` all need faithful
Incref/Decref on every store) because enabling generator tracking or
half-wiring the refcounts regresses the conjoin/email/fun/coroutine doctests,
as recorded under P11.1 below. So the panel does not yet meet the zero-skip
"every gate green" bar; the gap is fully enumerated, cited, and tracked rather
than papered over.

**P11 sharpened (2026-06-11).** A `list_dealloc` dry-run (spec 1727, "P5 dealloc
dry-run") pinned the remaining work precisely. The dealloc body itself is correct
and makes `test_iter.test_ref_counting_behavior`'s `del l` drive `C.count` to 0;
the blocker is that the list content-insert paths (`LIST_EXTEND`, `list.extend`,
slice assignment, `list()` from an iterator, comprehension `LIST_APPEND`, and the
borrow-returning `IterNext` convention) hand items to the list without the
`Py_INCREF` that `Objects/listobject.c` does, so flipping dealloc frees still-held
items (concrete repro: `_collections_abc` import loses `Coroutine.register`; then
`re`/`_sre` loses `CATEGORY_NOT_WORD`). These increfs are coupled to the dealloc
flip: task #137 made `Append` not-incref on purpose to fire a weakref reclaim, so
they cannot land individually while dealloc is off without regressing weakref. The
two P11 gates therefore stay red until the content-borrow sites and the dealloc
flip land as one verified-green step. Reported red, not skipped.

**P11 blast radius confirmed (2026-06-11, second dry-run).** A fuller dealloc
attempt (incref on every list insert/mutator plus the `STORE_SUBSCR_LIST_INT`
stack-ref close, dealloc flipped on) still passes the gate scenario and is
race-clean, but corrupts the corpus one level deeper than the insert sites. The
minimal repro is `list(zip(...))` / `list(map(...))` / `list(iter([...]))`
returning `[]`: every iterator that holds a list source (`listIter`, `zip`,
`map`, `SeqIter`, `Filter`, `Enumerate`, `Reversed`, the dict/set iterators)
keeps it *borrowed*, where CPython does `Py_INCREF(seq)` in the iterator
constructor. With dealloc live the borrowed source reaches refcount 0 while the
iterator still walks it, `list_dealloc` wipes the slice, and the iterator yields
nothing (which is what breaks `dict(zip(...))` and the `re` `CH_NEGATE` table).
The decisive conclusion: the insert convention is necessary but not sufficient.
Every holder that keeps a list past one op must incref, which is a conversion of
the whole runtime to strict CPython refcounting (it currently under-counts
everywhere and leans on the Go GC). That is multi-session scope; this dry-run was
reverted to baseline so the tree stays green and the two gates stay honestly red.
Full diagnosis and the next-pass plan (port `Py_INCREF(seq)` into every iterator
constructor first, flip dealloc last) is in spec 1727, "P5 second dry-run".

## Goal

Run every test in the spec 1700 VM/eval-loop panel (22 files), confirm which
pass end-to-end today, flip those to `done` in spec 1700, and document every
remaining gap with a CPython citation and a concrete fix plan.

Sources of truth: `$HOME/cpython-314/`. Every cited function was read from
that tree.

---

## Current test status (audit date: 2026-06-09)

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
| test_extcall | OK (P10 fixed: module naming, star-unpack funcstr, iter-drain StopIteration) | done |
| test_frame | OK (59 pass, 12 skip) | done |
| test_eval | no standalone module in 3.14 (moved to test_capi/, needs _testcapi) | out-of-scope |
| test_pow | OK | done |
| test_yield_from | OK (43 pass) | done |
| test_coroutines | OK (99 pass, 3 skip) | done |
| test_asyncgen | OK (85 pass) | done |
| test_generator_stop | OK | done |
| test_generators | OK (59 pass, 1 skip) | done |
| test_iter | OK (57 pass, 2 skip) | done |
| test_iterlen | OK (22 pass) | done |
| test_index | OK (55 pass) | done |
| test_isinstance | OK | done |

Recursion-limit enforcement (P12.1, P12.2) flipped `test_repr_deep` across
`list_tests` / `mapping_tests` / `test_dict` and removed the Go stack-overflow
crash on unbounded recursion. As of the 2026-06-01 pass `test_frame` and
`test_generators` are fully green (P12.4 PEP 709 inlining shipped, plus the
generator gi_exc_state nesting-depth fix so `sys.exception()` is correct across
a yield inside an except block). As of the 2026-06-09 re-audit every in-scope
file is green, including the two that previously waited on the asyncio event
loop: `test_coroutines` (99 pass, 3 skip) and `test_asyncgen` (85 pass). Those
flipped once the event-loop port landed (Context.run, thread identity, hashable
coroutines) and the executing-frame GC roots (P14 below) stopped the collector
from reclaiming a live coroutine/generator held only by an interpreter frame.
The container refcount-discipline gap (P11) remains open but no longer blocks
any panel file; it is now a correctness/exactness item rather than a gate.
`test_extcall` is now green (P10): vendored tests run under
`test.<name>` so the doctest module prefix matches CPython, the star-unpack
TypeError formatters were ported faithfully, and iterator draining no longer
leaks a user `__next__` raising `StopIteration`. `test_eval` has no standalone module in CPython 3.14 (it moved
under `Lib/test/test_capi/test_eval.py` and needs `_testcapi`), so P9 is
out of scope.

A coroutine that GET_ITER rejected with TypeError (`for _ in coro()`,
bpo-32703) used to stay pinned on the eval stack after the unwind, so its
never-awaited finalizer never ran; the exception-unwind path now decrefs the
abandoned stack temporaries the way CPython's exception_unwind does, which
recovered `test_coroutines.test_func_9` (7 failures down to 5).

`test_asyncgen` emitted spurious `Task was destroyed but it is pending!`
lines on stderr after the aclose cases. The async-generator port now mirrors
CPython's `ag_closed` flag (set the instant aclose begins driving the body,
distinct from the frame-finished state) and gates `_PyGen_Finalize` on it, so
a body that ignores GeneratorExit and raises RuntimeError no longer leaves the
generator looking open for a later GC to re-finalize. The one remaining stderr
line comes from `test_async_gen_aclose_compatible_with_get_stack`, which
creates an aclose task it never awaits: gopy collects the parent async
generator while that task is still queued in the loop's ready deque, so the
finalizer fires once more. That reachability gap is the same container
refcount discipline tracked under P11, not the asyncgen logic; the test itself
passes.

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

## P10 — `test_extcall` doctest module-name mismatch (shipped)

**Blocked test:** test_extcall. Now green.

The doctests check the exact text of `TypeError` messages, which CPython
prefixes with the module (`test.test_extcall.f()`). Three separate gaps
kept this red; all are fixed.

1. **Module naming.** Vendored CPython tests are imported by regrtest as
   `test.<name>`, so `__name__` (and every doctest glob `__name__`) is the
   dotted package name, and `_PyObject_FunctionStr` bakes that into the
   message. gopy ran the file as `__main__`. `runFile` now runs a
   `test_*.py` file under `test.<name>` (`mainModuleName`), registers it
   under that name, and aliases `__main__` to the same module so
   `__import__('__main__')` and the appended unittest runner still resolve.
   CPython: `Lib/test/libregrtest/runtest.py` (imports `test.<name>`).

2. **Star-unpack funcstr.** A lone `f(*x)` reaches `CALL_FUNCTION_EX` and
   must report `f() argument after * must be an iterable, not X`
   (`Python/ceval.c` check_args_iterable); a mixed `f(1, *x)` goes through
   `LIST_EXTEND` and must report `Value after * must be an iterable, not X`
   (`Python/bytecodes.c:2023 LIST_EXTEND`). gopy emitted a generic message
   and, because codegen always wrapped the single star arg in a list, the
   two messages were swapped. `codegen_call_helper_impl`'s single-star fast
   path (visit `star.Value` directly) is now mirrored, and both opcodes
   reformat only when the object is genuinely not iterable (an `__iter__`
   that raises still propagates its own error). New helper
   `objects.Iterable` mirrors `tp_iter != NULL || PySequence_Check`.

3. **Iterator-drain StopIteration.** `tuple(it)`, `[*it]`, and `f(*it)`
   drain through `iterToSlice` / `DrainIterable`, which called the raw
   `tp_iternext` slot rather than the `PyIter_Next` wrapper, so a user
   `__next__` raising Python-level `StopIteration` was returned as a plain
   error instead of ending the loop. Both now route through
   `objects.IterNext` (`Objects/abstract.c:2852 PyIter_Next`), matching the
   for-loop and `list()` paths.

Appended unittest runners compile under the file's real path rather than
`"<string>"` so frame repr and traceback source-line lookup keep working
(`pythonrun.RunSimpleStringWithName`).

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

**Finding (2026-06-10): a container-only refcount port is unsafe here.**
Implementing the full list refcount in isolation (`NewList` increfs each stored
item under a borrow convention, `listDealloc` decrefs the contents, every
mutation path in `objects/list.go` balanced) compiles cleanly and passes every
simple repro, but corrupts real workloads: `import textwrap` dies with
`IndexError: index out of range` inside `re._parser` because a live list reaches
refcount 0 and `listDealloc` frees its backing slice while the interpreter still
holds it (the list reports `len == 1` but `list[0]` is out of range, a
length/contents desync). Disabling only `ListType.Dealloc = listDealloc` makes
the import pass again, which pins the cause precisely.

The root cause is the eval loop, not the container. `decrefInputs`
(`vm/eval_helpers.go`, CPython `Python/ceval_macros.h DECREF_INPUTS`) is a
deliberate no-op, and the load opcodes (`LOAD_FAST`, `LOAD_ATTR`, returns,
iterator results) do not consistently `Incref` the value they push. So the
Python refcount is not a coherent liveness signal: any container `Dealloc` that
frees contents at refcount 0 will free still-live objects. The container-level
port therefore CANNOT ship on its own. The gate tests
(`test_iter.test_ref_counting_behavior`, `test_frame.test_clear_refcycles`)
require the full eval-loop stack-ref discipline below as a prerequisite.

**The discipline is interlocked, not uniformly loose.** A closer audit shows
gopy's eval loop is not "no refcounting" but an inconsistent mix, and that is
why no single piece can be made faithful in isolation:

- Loads DO incref. `LOAD_FAST` is `e.localAt(oparg).Dup()`
  (`vm/eval_dispatch_gen.go:744`), and `Dup` increfs (`stackref/stackref.go:83`).
- Frame teardown DOES decref. `Frame.DropStack` and `Frame.Clear`
  (`frame/frame.go:353`, `:369`) `Close()` every slot.
- But `decrefInputs` is a no-op (`vm/eval_helpers.go:27`), so the per-opcode
  consumed inputs never get released (they leak upward, which is safe but wrong).
- And iteration BORROWS instead of owning. `listIterType.IterNext` returns
  `it.src.items[it.pos]` with no incref (`objects/list.go:791`), diverging from
  CPython `listiter_next`, which returns an owned reference via
  `list_get_item_ref` (`Objects/listobject.c:4018`, `:4026`).

So the net per-object count is neither a clean borrow model nor a clean owned
model. That mix is what makes a targeted fix unsafe: e.g. adding the faithful
`Py_DECREF(it)` / item-close discipline to the failed-unpack path
(`unpackSeq`, `vm/eval_simple.go:1744`, porting
`Python/ceval.c:2387 _PyEval_UnpackIterableStackRef`) would over-decref, because
the items `IterNext` handed back were borrowed from the list, not owned.

**Prerequisite P11.0 — eval-loop stack-ref discipline.** Before any container
`Dealloc` can fire at refcount 0, the stack must own its references the way
CPython's stackref machinery does: each load pushes an owned reference (increfs
its source), iterators return owned references (`listiter_next` increfs),
every consumed input is closed (`decrefInputs` becomes a real `Decref` over the
popped slots), and stores into locals/cells/containers/attrs incref. CPython
model: `Python/ceval_macros.h DECREF_INPUTS`,
`Include/internal/pycore_stackref.h PyStackRef_DUP`/`PyStackRef_CLOSE`,
`Python/bytecodes.c` (every `LOAD_*`/`STORE_*`/`FOR_ITER` uop),
`Objects/listobject.c:4018 listiter_next`. The change is coherent-whole: making
`listiter_next` own its result forces every `IterNext` consumer (FOR_ITER,
`list()`, `unpackSeq`, comprehensions) to close it, and making `decrefInputs`
real forces every opcode's inputs to be genuinely owned at entry. Only once the
refcount is a true liveness signal do the failed-unpack decref, P11.1 (container
deallocs), and P11.2 (generator tracking) become safe to land.

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

## P14 — Executing-frame objects as cycle-collector roots (shipped)

**Blocked test:** `test_frame.test_clear_executing_generator`. A suspended
generator referenced only by a *running* test frame's fast local was reclaimed
mid-test, so `frame.clear()` saw a dead generator.

**CPython mechanism.** The cycle collector roots everything reachable from the
live call stack because each executing frame is anchored at
`tstate->current_frame`. `Python/gc.c:1430 gc_collect_main` never adds those
frames to the candidate set, so `subtract_refs` cannot zero out an object the
running interpreter still holds on its value stack or in a fast local.

**gopy gap.** gopy runs each generator / coroutine / async-generator body on
its own goroutine and keeps interpreter activation records in a per-thread
`frame.Frame` arena that the refcount collector cannot see. `subtract_refs`
walks tracked containers via `tp_traverse`, but the arena is not a tracked
container, so an object held only by an executing frame's local collapses to
`gc_refs == 0` and `move_unreachable` reclaims it. The pre-existing
`objects.GCRoot` pin (running gen/coro/asyncgen, task #199) covered the body
object itself but not the arbitrary objects that body's frame holds.

**Fix.** A new hook lets the collector enumerate everything an executing
interpreter frame holds and re-float any candidate that landed on the
candidate list, mirroring the `tstate->current_frame` rooting:

- `objects/type.go` declares `GCExecutingRootsHook func(pin func(Object))`,
  nil until the vm wires it (keeps `module/gc` free of a vm import).
- `vm/gc_roots.go` (new) sets the hook in `init()`. `pinExecutingFrameRoots`
  walks every live thread's `FrameStack` plus the `activeEvalFrames` registry
  of generator-body frames, and for each frame visits its fast locals, cell
  locals, free vars, and value-stack slots, the same surface as
  `Objects/frameobject.c:1163 frame_traverse`.
- `module/gc/refs.go` `pinRoots` invokes the hook and re-floats `gc_refs` to 1
  for any held object still carrying the COLLECTING bit at `gc_refs == 0`.
  `move_unreachable`'s `visit_reachable` then pulls in whatever the rooted
  object itself references (a suspended generator's frame, its sub-generators).
- `module/gc/collector.go` passes `state.tracked` to `pinRoots` so the hook can
  map a held object back to its `gcHead`.

This is the faithful complement to P11: P11 chases exact refcounts on container
stores, while P14 reproduces the call-stack rooting CPython gets for free. P14
unblocks the executing-generator case without enabling generator tracking, so
none of the conjoin/email/fun/coroutine doctests regress.

CPython: `Python/gc.c:1430 gc_collect_main`,
`Objects/frameobject.c:1163 frame_traverse`,
`Python/pystate.c:2099 PyThreadState_GetFrame`.

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

- [x] P3.1 `sys.set_asyncgen_hooks` / `sys.get_asyncgen_hooks`
- [x] P3.2 `sys.set_coroutine_origin_tracking_depth` / `sys.get_coroutine_origin_tracking_depth`
- [x] P3.3 `sys.call_tracing(func, args)` ports `_PyEval_CallTracing`: saves and
  zeroes `tstate->tracing` around the call so a debugger can recursively trace
  code from a checkpoint, then restores the prior depth. Arg-count and non-tuple
  errors match CPython's clinic wording.

### P13 — Async generator finalization through asyncio shutdown

- [x] P13.1 `async_generator.__aiter__` (am_aiter) returns a new strong reference
  via `PyObject_SelfIter`. The missing incref let `GET_AITER` drop the generator
  to refcount zero, firing `tp_finalize` on a still-referenced generator and
  emptying `loop._asyncgens` before `shutdown_asyncgens` ran.
- [x] P13.2 `async_generator_asend` / `async_generator_athrow` are hashable by
  identity (`tp_hash` inherits object's `_Py_HashPointer`). `shutdown_asyncgens`
  gathers the `aclose()` awaitables and keys a dict on each.

### P4 — Frame attributes

- [x] P4.1 `frame.f_generator`
- [x] P4.2 `frame.f_trace` settable
- [x] P4.3 `sys.unraisablehook` settable attribute
- [x] P4.4 `frame.clear()` executing-frame guard (raises `RuntimeError: cannot clear an executing frame`)

### P5 — isinstance / issubclass with UnionType and abstract classes

- [x] P5.1 `isinstance(x, int | str)` via `*UnionType` case in `objectIsInstance`
- [x] P5.2 `issubclass(T, int | str)` via `*UnionType` case in `objectIsSubclassObj`
- [x] P5.3 abstract class protocol (`checkClass` + `abstractIsSubclass` + `ClearCurrentExceptionHook`)
- [x] P5.4 AttributeError masking: clear thread-state exception to avoid stale exception on synthesize path

### P6 — __index__ in subscript paths

- [x] P6.1 `bytes.__getitem__` calls `objects.Index` on non-int subscript
- [x] P6.2 `bytearray.__getitem__` calls `objects.Index`
- [x] P6.3 `list.__getitem__` calls `objects.Index`
- [x] P6.4 `tuple.__getitem__` calls `objects.Index`

### P7 — __length_hint__ on iterators

- [x] P7.1 deque iterator `__length_hint__`
- [x] P7.2 deque reverse iterator `__length_hint__`
- [x] P7.3 dict key/value/item iterator `__length_hint__`

### P8 — Generator exception handling

- [x] P8.1 `gen.throw()` preserves exception object identity
- [x] P8.2 `gen.close()` does not re-wrap GeneratorExit
- [x] P8.3 PEP 479 StopIteration → RuntimeError conversion in generator body (`generator raised StopIteration`)

### P9 — Missing test file

- [x] P9.1 diagnosed: no standalone `test_eval.py` in CPython 3.14 (moved under `Lib/test/test_capi/test_eval.py`, needs `_testcapi`); out of scope.

### P10 — Error message module prefix

- [x] P10.1 TypeError messages use `module.funcname()` not `__main__.funcname()` for non-main modules (vendored tests run under `test.<name>`; star-unpack funcstr; iterator-drain routes through `objects.IterNext`)

### P11 — Generator cycle-collector finalization

- [ ] P11.1 Faithful refcount on container stores (dict/list/tuple/set/instance/frame/eval).
- [ ] P11.2 Restore `GCTrackHook` call in `NewGenerator` once P11.1 lands.
- [ ] P11.3 Verify `handle_resurrected_objects` flow on generator resurrect.
- [ ] P11.4 `sys.unraisablehook` deallocator-context format string matches CPython.

### P12 — Recursion limit enforcement

- [x] P12.1 Python recursion limit on `CALL_PY_EXACT_ARGS` / `CALL_BOUND_METHOD_EXACT_ARGS` / `CALL_ALLOC_AND_ENTER_INIT` fast arms.
- [x] P12.2 C recursion limit on `objects.Repr` / `objects.Str` (`test_repr_deep`).
- [x] P12.3 Throw-path frame linking so `traceback.extract_stack()` matches `send` (`test_stack_in_coroutine_throw`): a coroutine driven by `throw()` reports the same visible stack depth as one driven by `send()` (both 4).
- [x] P12.4 PEP 709 inlined comprehensions (`test_write_with_hidden`).
- [x] P12.5 `test_extcall` module-name diagnosed as harness artifact (no code change).

### P13 — Async generator finalization through asyncio shutdown

- [x] P13.1 `async_generator.__aiter__` (am_aiter) returns a new strong reference.
- [x] P13.2 `async_generator_asend` / `async_generator_athrow` hashable by identity.

### P14 — Executing-frame objects as cycle-collector roots

- [x] P14.1 `GCExecutingRootsHook` declared in `objects/type.go` (nil until vm wires it).
- [x] P14.2 `vm/gc_roots.go` enumerates live thread frames + `activeEvalFrames` and pins held objects (mirrors `frame_traverse`).
- [x] P14.3 `module/gc/refs.go` `pinRoots` re-floats `gc_refs` for held candidates; `collector.go` passes `state.tracked` through.
- [x] P14.4 `test_frame.test_clear_executing_generator` green; no conjoin/email/fun/coroutine doctest regression.

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
| test_coroutines | ready | done | P1 + P12.3 + P14 + asyncio event loop shipped |
| test_asyncgen | ready | done | P1 + P13 + P14 + asyncio event loop shipped |
| test_yield_from | ready | done | P1 + P8 shipped |
| test_generator_stop | ready | done | existing pass |
| test_pow | ready | done | P2 shipped |
| test_frame | ready | done | P12.4 (PEP 709) shipped |
| test_isinstance | ready | done | P5 shipped |
| test_index | ready | done | P6 shipped |
| test_iterlen | ready | done | P7 shipped |
| test_extcall | ready | done | P10 shipped (module naming, star-unpack, iter-drain) |
| test_eval | ready | out-of-scope | P9 (no standalone module in 3.14) |
