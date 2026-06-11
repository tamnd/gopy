---
id: "1727"
slug: 1727
title: "1727: List item ownership + stack-ref discipline (synchronous container reclaim)"
sidebar_label: "1727 List stack-ref ownership"
description: "Make Python lists own a strong reference to every stored item, so a list's last decref reclaims its contents synchronously and del l fires each item's __del__, matching CPython. Requires a consistent steal/borrow convention across all ~145 list-construction call sites plus owned-push discipline in the eval loop's value producers."
---

## Status

Active. Branch `feat/v0.13.2-vm-zero-skip-conformance`, stacked under spec 1726.
This is the subsystem the spec 1726 P11 gate
(`test_iter.test_ref_counting_behavior`) and `test_frame.test_clear_refcycles`
both block on.

## Why this exists

CPython reclaims a container's contents the moment the container's refcount
hits zero: `list_dealloc` walks every slot and `Py_DECREF`s it, so

```python
l = [C(), C(), C()]
del l            # each C().__del__ fires here, synchronously
```

gopy does not: a list holds its items as plain Go slice entries with no
dedicated reference, `list_dealloc` has no content-release sweep, and the eval
loop has no single ownership convention (BUILD_LIST *steals* its stack refs
while `list_concat` / `list_slice` *borrow* their source items). So `del l`
leaks the contents to the Go GC and `__del__` never fires at the Python-visible
moment. The two gate tests assert the CPython timing and fail.

### Why a partial port corrupts (measured 2026-06-10)

A first attempt made the list side faithful (incref on every store, decref on
every removal + `list_dealloc`) but left the surrounding VM unchanged. It built
clean and satisfied the narrow gate, but `import textwrap` / `re` raised
`IndexError: index out of range` from a prematurely-freed sub-list. Root cause,
proven by bisection:

- gopy's VM has **no consistent list-item ownership convention**: BUILD_LIST /
  LIST_APPEND transfer (steal) their stack references into the list without an
  incref, while `list_concat` / `list_slice` / `list_repeat` copy *borrowed*
  source items. One `list_dealloc` decref cannot balance both.
- The VM's value producers are under-owned. With an underflow detector enabled
  on an otherwise-clean tree, the `CALL` arm over-decrefs
  `builtin_function_or_method` (the popped callable is pushed borrowed by the
  preceding LOAD, then `CALL` decrefs it). So `LOAD_*` / `BINARY_SUBSCR` /
  `BUILD_TUPLE` push references that any added `DECREF_INPUTS` then drives
  negative.

The conclusion: list ownership cannot land in isolation. Every path that *puts*
a reference into a list must own it, every path that *takes* one out must
release it, and every eval-loop op that *pushes* a value must push an owned
reference so the consumer's `DECREF_INPUTS` is balanced. That is the stack-ref
discipline this spec ports.

## Invariant

> Every reference stored in a `List` slot is owned by the list. Every store
> increfs; every removal and `list_dealloc` decrefs. Every value pushed onto the
> eval stack is owned by its slot; every consume decrefs.

This is corruption-proof by construction: with a dedicated incref per slot, a
`list_dealloc` decref can never drive a still-referenced item negative.

CPython references: `Objects/listobject.c` (`PyList_New`,
`list_dealloc`/`list_clear_impl`, `list_concat`, `list_ass_slice_lock_held`,
`app1`/`list_append`, `ins1`, `PyList_SetItem` Py_XSETREF); `Python/bytecodes.c`
(BUILD_LIST, LIST_APPEND, UNPACK_SEQUENCE, BINARY_SUBSCR DECREF_INPUTS).

## API (objects/list.go)

- `NewList(items)` — BORROW: increfs each stored item. For Go callers that pass
  a slice they keep using. The safe default; over-use leaks, never corrupts.
- `NewListSteal(items)` — STEAL: adopts the caller's references with no incref.
  For builders that produce fresh owned items they do not retain (eval loop
  after closing stack refs, internal single-allocation builders).
- `Append(v)` — BORROW (incref), mirrors `app1`.
- `AppendSteal(v)` — STEAL, for LIST_APPEND-style transfer.
- mutators (`SetItem`, `SetSlice`, `Insert`, `Remove`, `Pop`, `Clear`,
  `listSetItem`, `listDelIndex`, `listAssSliceItems`, `listSetSlice`,
  `listDelSlice`, `listInPlaceRepeat`): incref newly-placed, decref
  removed/recycled (recycle-then-XDECREF-last order per `list_ass_slice`).
- `listDealloc`: untrack, empty-first, decref each slot in reverse.

## Phases

- **P1 — Convention + API.** Add `NewListSteal` / `AppendSteal`; make `NewList`
  / `Append` borrow (incref). No dealloc decref yet, so the tree only leaks
  (green, but refcount-sensitive tests deferred to P6 flip).
- **P2 — Mutators.** Balance every list mutator (incref placed, decref removed),
  porting `list_ass_slice`'s recycle/memmove/XNewRef/XDECREF-last shape.
- **P3 — Caller audit.** Classify all 145 `NewList` + 4 `newListAdopt` sites as
  steal vs borrow; switch the steal sites to `NewListSteal`. Table below.
- **P4 — Eval-loop producers.** Make BUILD_LIST / LIST_APPEND close (decref)
  their stack inputs after the list increfs; make `BINARY_SUBSCR` (and the other
  UNPACK sources) push owned refs; add `DECREF_INPUTS` to UNPACK_SEQUENCE.
- **P5 — list_dealloc.** Enable the content-release sweep.
- **P6 — Validation + flip.** Run the full affected panel
  (`test_iter` / `test_frame` / `test_list` / `test_descr` / `test_gc` /
  `test_weakref`) + `import textwrap/re/json` smoke under the underflow
  detector; flip the dealloc flag; confirm both gates pass; commit.

## Underflow detector (the audit tool)

P4 is driven by a temporary diagnostic, not by reading code blind. A hook in
`Decref` logs (does not panic) the type, name, and Go stack whenever a mortal
refcount drops below zero, capped at a few hundred lines so a whole program runs
to completion and surfaces every under-ownership site in one pass:

```go
// objects/refcount.go, temporary
newrc := atomic.AddInt64(&h.refcnt, -1)
if newrc < 0 { probeUnderflow(o, newrc) } // log type/name/stack, capped
```

Run `import textwrap` (pulls in `re`, the f-string machinery, and BUILD_STRING)
under the probe, read the stacks, fix the producer at the top of each, rebuild,
repeat. The probe and its `objects/refcount_probe.go` companion are removed
before every commit; the tree never ships with the hook.

## P4 progress (2026-06-10)

Three eval-loop value producers were pushing borrowed references that the
following consumer then decref'd, driving the object negative. Each is now fixed
to push an owned reference exactly where the CPython bytecode increfs. Commit
`621d84c7`.

| Producer | Bug | Fix | CPython |
| --- | --- | --- | --- |
| `LOAD_CONST` (eval.go fast path + eval_dispatch_gen.go) | stole the borrowed `co_consts` entry; gopy consts are not immortal so the push must own | `FromObject` → `FromObjectNew` | `Python/bytecodes.c` LOAD_CONST (Py_INCREF on non-immortal const) |
| `LOAD_BUILD_CLASS` (eval_dispatch_gen.go) | stole the borrowed `__build_class__` slot returned by `MappingGetOptionalItem` (which, unlike CPython, returns borrowed) | `FromObject` → `FromObjectNew` | `Python/bytecodes.c:1556` LOAD_BUILD_CLASS (`Py_INCREF(bc)`) |
| `FORMAT_SIMPLE` exact-str passthrough (eval_dispatch_gen.go) | aliased the borrowed input, then `drop(1)` decref'd it while `push` re-pushed the same object uncounted | `res = value` → `res = value.Dup()` | `Python/bytecodes.c` FORMAT_SIMPLE (str branch keeps the input live) |

Measured state after these fixes: the noisiest monotonic over-decrefs are gone,
but ~430 under-ownership sites remain across multiple producers. Bucket counts
from the textwrap run (top of stack, deduped by type):

| Type | Underflows | Likely producer family |
| --- | --- | --- |
| builtin_function_or_method | 108 | LOAD_GLOBAL / LOAD_METHOD borrowed push |
| tuple | 96 | BUILD_TUPLE / BINARY_SUBSCR |
| int | 53 | LOAD_FAST / arithmetic results |
| list | 52 | BUILD_LIST / list_concat / list_slice |
| str | 44 | LOAD_ATTR / BUILD_STRING |
| dict | 10 | BUILD_MAP / LOAD_GLOBAL globals |
| module | 8 | IMPORT_NAME / LOAD_ATTR on module |

This is the P4 worklist: each family needs its push site audited against the
matching `Python/bytecodes.c` op so the reference it pushes is owned. The count
is too large to drive to zero in one session, which is the multi-session scope
this spec was opened for. `list_dealloc` (P5) stays disabled until P4 reaches
zero, because enabling it now would convert these latent under-counts (harmless
negative refcounts that never free) into active corruption (premature free of
live contents), exactly the `IndexError` failure mode measured above.

## Caller audit table (P3)

Filled in as each package is classified. Format: `package` — steal / borrow /
unchanged.

(pending)

## Gates

- `test/cpython/test_iter.py::test_ref_counting_behavior` — `del l` drives
  `C.count` to 0.
- `test/cpython/test_frame.py::test_clear_refcycles`.
- No regression: `import textwrap`, `re`, `json` smoke; full VM panel green.

## Checklist

- [ ] P1 convention + API (NewList/Append borrow, NewListSteal/AppendSteal)
- [ ] P2 list mutators balanced
- [ ] P3 145 NewList + 4 newListAdopt caller audit and reclassification
- [~] P4 eval-loop producers push owned + UNPACK DECREF_INPUTS (3 producers fixed: LOAD_CONST, LOAD_BUILD_CLASS, FORMAT_SIMPLE; ~430 sites remain, see bucket table)
- [ ] P5 list_dealloc content-release sweep
- [ ] P6 full panel + smoke under underflow detector, flip, both gates pass
- [ ] spec 1726 P11 + spec 1723 status updated, human PR comment on #91
