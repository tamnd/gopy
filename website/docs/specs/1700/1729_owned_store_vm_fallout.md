---
id: "1729"
slug: 1729
title: "1729: Owned-store fallout in the VM/eval-loop panel"
sidebar_label: "1729 Owned-store VM fallout"
description: "The container-ownership port (1727) and per-dict critical sections (1728) turned every stored item into a counted reference. That exposed five latent reference-discipline bugs in the generator/coroutine/async-generator paths and one structural deadlock in the dict lock. This spec records each fix against its CPython source."
---

## Status

Done. Branch `feat/v0.13.0-spec-1724-builtins-types-panel`. Closes the
regressions the owned-store work (spec 1727) and the per-dict critical
sections (spec 1728) left in the spec 1723 VM/eval-loop panel. After these
fixes the full 21-file panel re-runs with 20 files fully green and `test_call`
carrying only its documented `_testcapi` whole-port gaps (#243/#245). Both
spec-1723 gates stay green: `test_iter.test_ref_counting_behavior` and
`test_frame.test_clear_refcycles`.

## Why this exists

Before spec 1727, a stored object in a list/tuple/dict was a borrowed pointer:
the Go GC kept it alive and a premature `Decref` of the container was harmless.
Spec 1727 made every container own a counted reference to each stored item and
decref its contents tail-first on dealloc. That is the CPython model, but it
turned a class of pre-existing borrow bugs from silent into fatal: any code
path that handed a stored object out without the matching `Py_INCREF`, or kept
a temporary owning-tuple past its call, now drops a live object's refcount to
zero and frees the items it owned out from under still-live fields.

Five such bugs surfaced in the generator/coroutine/async-generator paths, plus
one structural deadlock the per-dict lock (spec 1728) introduced once the lock
became load-bearing. Each is a CPython-faithful port, not a workaround.

## P1 — `cr_origin` getter handed out a borrowed tuple

`coroutine.cr_origin` stores the captured creation-stack tuple in the coroutine
struct. The getset getter returned that stored tuple borrowed. Pre-1727 the
attribute-access machinery's consume-and-release of one reference was harmless;
post-1727 it dropped the tuple to zero and decreffed its owned inner frames, so
`repr(coro.cr_origin)` showed a populated tuple while `len()` / `list()` saw an
empty one (the warning printer in `_py_warnings._warn_unawaited_coroutine`
rendered a blank traceback as a result).

**Fix.** `Incref(c.CrOrigin)` before returning, mirroring CPython.

CPython: `Objects/genobject.c:1184 coro_get_cr_origin` (`Py_INCREF(coro->cr_origin)`).

File: `objects/coroutine.go` (`initCoroutineDescriptors`, the `cr_origin`
getter).

## P2 — async-gen firstiter / finalizer hooks leaked their args tuple

`async_gen_init_hooks` calls the thread's firstiter hook with the generator as
its single argument, and `_PyGen_Finalize` calls the finalizer hook the same
way. gopy built a temporary `NewTuple([]Object{g})` for each call and never
released it. Post-1727 that tuple owns one reference to the async generator, so
the unreleased tuple pinned the generator for its whole lifetime: under asyncio
the firstiter hook registers the generator in `loop._asyncgens`, and the +1 kept
`del g; gc.collect()` from reaching refcount 0, so the finalizer that schedules
`aclose()` never fired (`test_asyncgen.test_async_gen_asyncio_gc_aclose_09`,
`test_async_gen_asyncio_shutdown_exception_02`).

**Fix.** Release the args tuple after each hook call, matching CPython, which
passes the generator directly via `PyObject_CallOneArg` so nothing outlives the
call.

CPython: `Objects/genobject.c:130 async_gen_init_hooks`,
`Objects/genobject.c:87 _PyGen_Finalize`.

File: `objects/async_gen.go` (`initHooks`, `asyncGenFinalize`).

## P3 — async-gen value wrap / unwrap refcounts

`_PyAsyncGenValueWrapperNew` takes a new reference to the wrapped value
(`Py_NewRef`), the wrapper's dealloc clears it (`Py_CLEAR`), and
`async_gen_unwrap_value` decrefs the wrapper after handing the inner value to
the StopIteration hook (`Py_DECREF`). gopy's wrapper did not own its value and
never released it, so under owned-store the value churned by one count per
yield.

**Fix.** `Incref` in `NewAsyncGenWrappedValue`; add `asyncGenWrappedValueDealloc`
(`Py_CLEAR`) wired through `AsyncGenWrappedValueType.Dealloc`; `Decref` the
wrapper after `AsyncGenStopIterationHook` in the asend/athrow drive paths.

CPython: `Objects/genobject.c:2049 _PyAsyncGenValueWrapperNew`,
`Objects/genobject.c:1980` (wrapper dealloc `Py_CLEAR`),
`Objects/genobject.c:1725 async_gen_unwrap_value`.

File: `objects/async_gen.go`.

## P4 — `END_SEND` closed the wrong stack slot

`END_SEND` keeps the value sent back from a sub-generator and discards the
receiver. gopy's implementation dropped the wrong slot, so under owned-store the
still-live value was the one released.

**Fix.** Pop the value, drop exactly the receiver, push the value back.
Same correction in `INSTRUMENTED_END_SEND`.

CPython: `Python/bytecodes.c END_SEND`.

File: `vm/eval_dispatch_gen.go`.

## P5 — `dict_iter` item caching aliased an owned tuple

A 1727-era optimization had `dictIterNextItem` cache and reuse one
`(key, value)` tuple across iterations. With owned-store every consumer that
held a yielded item kept a counted reference to the shared tuple, so successive
items aliased.

**Fix.** Allocate a fresh tuple per `next()`, as CPython does.

CPython: `Objects/dictobject.c dictiter_iternextitem`.

File: `objects/dict_iter.go`.

## P6 — per-dict critical section deadlocked across a generator drive

Spec 1728 added a goroutine-reentrant per-dict lock. gopy runs each generator,
coroutine, and async-generator body on its own goroutine, but that body shares
its driver's `*state.Thread` (it inherits `savedTS` through the active-thread
map), exactly as CPython runs a generator on the driver's `PyThreadState`. The
lock keyed reentrancy on the raw goroutine id, so when a driver held a dict's
lock and resumed a body that touched the same dict (any globals lookup), the
body goroutine had a different goid and blocked on a lock its own thread already
owned, while the driver blocked waiting for the body to yield. Deadlock
(`test_generators` `coroutine` doctest: `fatal error: all goroutines are asleep`).

**Fix.** Key the critical section on the Python thread, not the goroutine. The
lock owner is now the active `*state.Thread` id (tagged out of the goroutine
number space, with a goid fallback for goroutines running dict operations
outside the eval loop). A generator body and its driver share that id and pass
reentrantly; two distinct real Python threads still get distinct ids and
serialize, so the concurrency safety 1728 added is preserved (the threaded
`WeakValueDictionary` tests still pass and the `s.allocCount != s.nelems` crash
stays gone).

CPython: `Python/critical_section.c` keys on `PyThreadState`;
`Objects/genobject.c:248 gen_send_ex2` (a generator shares the driver's tstate).

Files: `objects/dict_lock.go` (`CriticalSectionOwnerHook`, `dictGoid`),
`vm/thread_hook.go` (hook wiring to `currentThread().ID()`).

## Verification

Full spec-1723 panel re-run (2026-06-13), built from this branch, run from
`test/cpython/`:

| File | Result |
|------|--------|
| test_dynamic, test_richcmp, test_compare, test_unary, test_augassign | OK |
| test_with, test_contains, test_typechecks, test_extcall | OK |
| test_frame | OK (10 skip, the FrameLocalsProxy design skips CPython also takes) |
| test_pow, test_yield_from, test_generator_stop | OK |
| test_coroutines | OK (3 skip) |
| test_asyncgen | OK (85) |
| test_generators | OK (1 skip) |
| test_iter, test_iterlen, test_index, test_isinstance | OK |
| test_call | 2 fail + 1 error: `_testcapi` / `_testinternalcapi` whole-port gaps (#243/#245) |

Gates: `test_iter.test_ref_counting_behavior` OK,
`test_frame.test_clear_refcycles` OK. `test_gc` byte-identical to the
pre-fix build (29 fail / 3 error / 8 skip). `test_weakref` threaded
`MappingTestCase` tests all pass; its remaining hang in
`test_weak_keyed_cascading_deletes` predates this work (task #226) and is out of
the 1723 panel. `gofmt`, `go vet`, and `golangci-lint` clean on `objects/` and
`vm/`.

## Checklist

- [x] P1 `cr_origin` getter increfs the stored tuple
- [x] P2 async-gen firstiter / finalizer hooks release their args tuple
- [x] P3 async-gen value wrap/unwrap refcounts (`Py_NewRef` / `Py_CLEAR` / `Py_DECREF`)
- [x] P4 `END_SEND` / `INSTRUMENTED_END_SEND` close the receiver, keep the value
- [x] P5 `dict_iter` allocates a fresh tuple per `next()`
- [x] P6 per-dict critical section keyed on the Python thread, not the goroutine
- [x] Full 21-file panel re-run green except `test_call`'s documented `_testcapi` gaps
- [x] Both spec-1723 gates green; `test_gc` unregressed; lint clean
