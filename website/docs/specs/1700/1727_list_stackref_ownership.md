---
id: "1727"
slug: 1727
title: "1727: List item ownership + stack-ref discipline (synchronous container reclaim)"
sidebar_label: "1727 List stack-ref ownership"
description: "Make Python lists own a strong reference to every stored item, so a list's last decref reclaims its contents synchronously and del l fires each item's __del__, matching CPython. Requires a consistent steal/borrow convention across all ~145 list-construction call sites plus owned-push discipline in the eval loop's value producers."
---

## Status

Done. Branch `feat/v0.13.2-vm-zero-skip-conformance`, stacked under spec 1726.
This is the subsystem the spec 1726 P11 gate
(`test_iter.test_ref_counting_behavior`) and `test_frame.test_clear_refcycles`
both blocked on; both gates now pass.

**Landed (2026-06-12).** The conversion the dry-runs below charted is now in the
tree and verified green. The list owns a strong reference to every stored item
(`Incref` on `PyList_New` / `Append` / `SetItem` / `list_ass_slice` /
`list_inplace_repeat`), `listDealloc` walks the slots tail-first and `Decref`s
each one under a resurrection guard, and every iterator that holds a list source
(`listIter`, `zip`, `map`, `SeqIter`, `Filter`, `Enumerate`, `Reversed`, the
dict/set iterators) increfs its source in the constructor. The owned-store path
needed concurrent dict access to be race-safe first, which spec 1728 supplied
(per-dict critical sections). Both gates
(`test_iter.test_ref_counting_behavior`, `test_frame.test_clear_refcycles`) now
pass, the corpus repros the dry-runs flagged (`import re` / `textwrap`,
`list(zip(...))`, `dict(zip(...))`, `list(map(...))`, `list(iter([...]))`) are
all clean, the at-risk sweep shows no regression vs base, and `go vet` /
`TestCfgPhaseParity` / the package unit tests are green. The dry-run
archaeology below is kept as the record of why the partial slices failed.

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

### Producer attribution via per-object history

The Decref stack alone names the *consumer*, never the producer that pushed the
under-owned reference. To close that gap the probe was extended to record a
short INCREF/DECREF history (with a trimmed stack per event) keyed by object
pointer, and to dump the whole life of the first object that goes negative.
Replaying one object's history end to end shows exactly which op pushed a
borrowed reference that a later op then over-released.

The first textwrap underflow attributed this way is a `dict.setdefault` result:
`setdefault` returned the stored value borrowed, the eval loop treated the call
result as owned, and a following `LOAD_ATTR` (`DECREF_INPUTS`) dropped it below
zero. That is one instance of a broader bug class.

### Bug class: builtin returns borrowed where CPython returns Py_NewRef

A vectorcall result is owned by contract (`CALL` pushes it owned, the eventual
consumer decrefs). Several gopy builtins that *return a value they looked up in
a container* hand it back borrowed, while the matching CPython impl wraps it in
`Py_NewRef`. Each such builtin is an isolated, faithful fix at the method-call
boundary, and each strictly reduces under-ownership (turns a negative into a
balanced count). Fixed so far:

| Method | gopy bug | CPython |
| --- | --- | --- |
| `dict.setdefault` | returned the stored value borrowed (the `incref_result=0` helper) | `Objects/dictobject.c:4542` dict_setdefault_impl (incref_result=1, Py_NewRef) |
| `dict.get` | returned the found value and the default borrowed | `Objects/dictobject.c:4387` dict_get_impl (Py_NewRef) |
| `dict.pop` | `DelItem` dropped the slot's reference, then the now-borrowed value was returned; the discarded result then under-decreffed | `Objects/dictobject.c:3144` _PyDict_Pop_KnownHash (`delitem_common(..., Py_NewRef(old_value)); *result = old_value`); default arm `Py_NewRef(default_value)` |

The dict.pop case was found by adding a per-event opcode label to the probe.
The dominant remaining textwrap underflow was a list stored via
`d.setdefault(k, []).append(...)`, read back, iterated, then removed by
`d.pop(k)` as a statement. The opcode label pinned the over-decref to the
`POP_TOP` that discards `pop`'s result: `pop` removed the entry (decref) and
returned the value borrowed, so the discard drove it negative. CPython's
`_PyDict_Pop` transfers the entry's own reference to the caller instead, so the
fix increfs the value before `DelItem` and increfs the default.

A separate, latent instance lives in the specialized UNPACK arms
(`UNPACK_SEQUENCE_TWO_TUPLE` / `_TUPLE` / `_LIST`): each read `seq.Item(i)`
(a borrowed slot) and pushed it stolen, where CPython does
`PyStackRef_FromPyObjectNew` on every element before closing the seq input
(`Python/bytecodes.c:1585/1599/1614`). It is masked today because `list_dealloc`
is disabled, but it would corrupt once P5 flips dealloc on, so it is fixed now.

Measured: textwrap smoke 430 → 422 (setdefault, get) → 241 (dict.pop, unpack)
under-zero decrefs, no test regression (core objects/vm/stackref/frame green;
test_dict's pre-existing `test_splittable_popitem` failure is unchanged). The
dict.pop reduction is large because it fires once per loop iteration. The
reduction is real (the counts drop, they do not shift), which validates the
attribution method.

**Shared-helper trap.** The fix must land at the method-call boundary, not in a
shared helper. `dict.__getitem__` routes through `dictMappingGet`, which returns
the stored slot *borrowed* on a hit but the `__missing__` *call result* owned on
a subclass miss. The same helper also backs the `mp_subscript` slot, and the
generic `BINARY_SUBSCR` arm already increfs its borrowed return. Blanket-increfing
inside the helper would double-incref the `BINARY_SUBSCR` path and leak the
`__missing__` result, so `__getitem__` needs its own boundary incref that skips
the `__missing__` branch. Deferred to a careful pass rather than rushed.

## P5 dealloc dry-run: the content-borrow coupling (2026-06-11)

Wired a faithful `list_dealloc` (Finalize dance, `GCUntrackHook`, `ClearWeakRefs`,
then `Py_XDECREF` each item from the tail backwards, per
`Objects/listobject.c:2813`) and flipped `ListType.Dealloc` on to measure what
the content-release sweep actually surfaces. Findings, all reverted to keep the
tree green, recorded here so the next pass starts from the diagnosis instead of
re-deriving it:

1. **The gate scenario works.** With `list_dealloc` live, the exact body of
   `test_ref_counting_behavior` drives `C.count` to 0 on `del l` (was stuck at 3
   with dealloc off). So the mechanism is correct; the only blocker is making
   every list-content path refcount-clean first.

2. **`BUILD_LIST` is already balanced; the content-insert paths are not.**
   `listFromStackRef` increfs each element and `BUILD_LIST` then drops the stack
   refs, so a BUILD_LIST list owns a real reference per item and `list_dealloc`
   balances it. `LIST_EXTEND` does not: `iterToSlice` returns the source's items
   *borrowed* (the List/Tuple fast path just reads `Item(i)`; the iterator path
   returns the borrowed `IterNext` result), `l.Append` steals (no incref), and
   then the arm closes the source iterable. With dealloc off the source list
   leaks and the borrow stays alive; with dealloc on the source's `list_dealloc`
   decrefs those shared items and the target is left holding freed slots. The
   first concrete corruption is `_collections_abc` import: `Coroutine.register`
   vanishes because an ABC-machinery list extended from a borrowed source is
   dealloc'd mid-build. CPython's `list_extend` does `Py_INCREF` per copied item
   (`Objects/listobject.c:1023`); the fix is the matching incref in the
   `LIST_EXTEND` arm, and it makes the ABC import clean.

3. **It is whack-a-mole across every content-borrow site, and the sites are
   coupled to dealloc.** Fixing `LIST_EXTEND` surfaced the next premature free
   (`KeyError: CATEGORY_NOT_WORD` from the `re`/`_sre` constant tables). Each
   borrow-without-incref site (LIST_EXTEND, the `list.extend`/`+=` method, slice
   assignment, `list()` from an iterator, comprehension `LIST_APPEND`, and the
   `IterNext`-returns-borrowed convention behind FOR_ITER) is a separate fix.
   They cannot be banked one at a time while dealloc is off: task #137
   deliberately made `Append` *not* incref to make a weakref self-cycle reclaim
   fire, so adding a content-path incref *without* dealloc reintroduces that leak
   and regresses the weakref reclaim tests. The increfs and the dealloc flip are
   one atomic landing (or a sequence verified green only at the end), not
   independent green increments.

4. **A real latent dict under-count rides along.** `buildClass`'s final
   `DecrefThrowawayKwargs(ns)` (`vm/build_class.go:196`) takes the class
   namespace dict below zero: the metaclass `Call` path already released `ns` to
   0, so the documented "last owner" decref underflows. Harmless today (dict has
   no dealloc) but it is a genuine over-decref to chase in the P4 sweep
   (`Python/bltinmodule.c:246` does a single `Py_DECREF(ns)` and type_new only
   borrows the namespace).

Plan crystallized by this dry-run: enumerate the content-insert borrow sites
(item 3), convert each to the BUILD_LIST discipline (the target owns a real
incref per element, matching the CPython `Py_INCREF`), land them together with
the `list_dealloc` flip, and verify the full corpus green in one step rather
than per-site. Until that atomic landing the two gates stay red, and this spec
reports them red rather than skipped.

## P5 second dry-run: the blast radius is every borrowing holder, not just inserts (2026-06-11)

A second, fuller pass took the content-insert fixes further (incref on `NewList`,
`newListAdopt`, `Append`, `SetItem`, `SetSlice`, `Insert`, `Remove`, `Clear`,
`listInPlaceRepeat`, the slice/del paths) plus the matching stack-ref close in
the `STORE_SUBSCR_LIST_INT` arm, and flipped `list_dealloc` on. The gate scenario
passes exactly (`del l` → `C.count` 0; clean tree leaves it at 3). The objects
and vm packages are race-clean. But the corpus still corrupts, and bisecting it
pinned the failure one level deeper than the content-insert sites:

- Minimal repro: `list(zip([1,2],[3,4]))` → `[]`, `list(map(str,[1,2,3]))` → `[]`,
  `list(iter([1,2,3]))` → `[]`. `list(g())` for a generator, `list(range(3))`,
  `list([1,2,3])`, `list((1,2,3))` all stay correct. The empty results are what
  break `dict(zip(...))` (the `re`/`_sre` `CH_NEGATE` table builds this way, hence
  `KeyError: CATEGORY_NOT_WORD`).
- Root cause: the broken cases all wrap a **list source held borrowed by an
  iterator**. `listIter` stores `src: o.(*List)` with no incref (CPython's
  `list___iter___` does `Py_INCREF(seq)`); `zip` / `map` / `SeqIter` / `Filter` /
  `Enumerate` / `Reversed` / `dict_iter` do the same. With `list_dealloc` off this
  is harmless (Go's GC keeps the borrowed source alive). With `list_dealloc` on,
  the temporary source list reaches refcount 0 *while the iterator still holds it*
  (the iterator's hold does not count), `list_dealloc` fires, nils the slice, and
  the iterator then walks an empty source.

This is the decisive finding: **the content-insert convention is necessary but
not sufficient.** gopy's object model deliberately under-counts everywhere and
leans on the Go GC for liveness; refcount 0 does *not* mean unreachable. A
`list_dealloc` that destroys on refcount 0 is only safe once *every* holder that
keeps a list past a single op increfs it: not just the eval-loop inserts, but
`listIter` / `listRevIter` / `SeqIter` / `CallIter` / `Map` / `Filter` /
`Enumerate` / `Reversed` / `zip` / the dict and set iterators, every generator
frame that pins a list local, and the C-level temporaries throughout the
modules. That is a conversion of the whole runtime from "Go-GC-backed,
refcount-for-finalizers" to strict CPython refcounting, which is the multi-session
scope this spec was opened for and is not landable in one pass without shipping
intermediate corruption.

Per the project's standing rule (never commit corrupting code; revert and report
when a port cannot be made green in budget), this dry-run was reverted to the
06b35097 baseline. The two gates stay red and honest. The next pass should treat
the holder-incref audit as the gating work: port `Py_INCREF(seq)` into every
iterator constructor first (smallest faithful CPython-shaped units, each green on
its own because it only adds a leak while dealloc is off), and only flip
`list_dealloc` once the holder set and the insert set are both complete.

## GC cycle-collector test reconciliation (downstream of P5)

Flipping `list_dealloc` and making `Append` own its stored ref changed the
refcount inputs the `module/gc` cycle-collector unit tests feed `Collect`. Those
tests pre-date list ownership: they built a cycle with bare `Append`, `Track`ed
it, and called `Collect` expecting reclaim, without ever dropping the creating
scope's reference. That only worked while `Append` was a no-op on refcounts. The
`TestParityDict` case already modeled the right shape (`d.SetItem(1, d); Track(d);
Decref(d) // simulate del d`) because dicts always owned their stored refs; the
list and tuple cases simply omitted the `del`.

CPython agrees: `l = []; l.append(l); gc.collect()` does **not** reclaim `l`
while the local still holds it. `subtractRefs` (`refs.go`) computes the
external-only count, so a freshly built self-cycle with a live creator reference
is correctly read as reachable. The fix is to make each list/tuple cycle test
drop the creator reference after wiring the cycle (`objects.Decref(x) // simulate
del`), matching `TestParityDict` and the literal `del l` / `del t,l` in the
CPython citations. Sites updated: `cycle_test.go`, `garbage_test.go`,
`finalize_type_slot_test.go`, `module_debug_test.go`, `test_gc_parity_test.go`,
`weakref_test.go`, `weakproxy_test.go`. The `linkInto` / `linkContainer` helpers
keep their extra `Incref` (now a stand-in for an external root, not a
compensation for a non-owning `Append`); their doc comments were corrected to say
so. No collector code changed; all of `module/gc` is green.

While the full lint ran over the audited eval loop it flagged `storeSubscr`'s
`keepKey` return as dead (`unparam`): no container path adopts the key's stack
reference (an exact dict increfs its own copy, an exact list stores the value
under an integer index). The return was dropped to `(keepValue, err)` and the
STORE_SUBSCR arm always closes the key, with the stale comment claiming the dict
"steals the key" corrected to match `insertdict`.

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

- [x] P1 convention + API (list owns every stored item, Incref on store)
- [x] P2 list mutators balanced (SetItem, ass_slice, inplace_repeat Incref/Decref)
- [x] P3 NewList + Append call sites incref on store
- [x] P4 eval-loop producers push owned + UNPACK DECREF_INPUTS
- [x] P5 list_dealloc content-release sweep (tail-first Decref under resurrection guard) + every list-holding iterator increfs its source in the constructor
- [x] P6 panel + corpus smoke clean, dealloc flipped, both gates pass
- [x] dict critical sections (spec 1728) land first so the owned store is race-safe
- [x] spec 1726 P11 + spec 1723 status updated
- [x] module/gc cycle-collector tests reconciled to the owned-store refcount model (drop the creating reference, `del`-style, in every list/tuple cycle test) + `storeSubscr` dead `keepKey` return removed
- [x] human PR comment on #91 (after merge #90 / rebase)
