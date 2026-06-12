---
id: "1728"
slug: 1728
title: "1728: Per-dict critical sections (free-threaded dict locking)"
sidebar_label: "1728 Dict critical sections"
description: "Port CPython 3.14's free-threaded per-dict critical sections to gopy. Wrap every dict table read and write in a goroutine-reentrant per-dict lock so concurrent dict operations serialize under no-GIL threading, removing the fatal `s.allocCount != s.nelems` crash that the container-ownership port exposed in test_weakref."
---

## Status

Done. Branch `feat/v0.13.2-vm-zero-skip-conformance`, alongside spec 1727.
Lands the lock the container-ownership work (1727) needs before any concurrent
dict access can be made owned-store safe.

## Why this exists

gopy runs `threading.Thread` as real goroutines with no GIL. The `Dict` had no
per-object lock, so a table read on one goroutine (`Keys`, dict-iter, `copy`,
all of which read `d.entries` / `d.order`) could run concurrently with a write
on another (`dictInsert`, `dictDelete`, `dictResize`). Before the container
ownership port this was a benign torn read. Once stores began incref-ing their
values, a torn read during a resize turned into a fatal allocator invariant
violation:

```
fatal error: s.allocCount != s.nelems
```

reproduced reliably by `test_weakref` (the `WeakValueDictionary` tests spawn
threads that mutate a shared dict while another thread copies it).

CPython 3.14's free-threaded build guards every dict operation with a critical
section:

```c
Py_BEGIN_CRITICAL_SECTION(mp);   // locks mp->ob_mutex
...
Py_END_CRITICAL_SECTION();
```

On the default GIL build these compile to no-ops; on the free-threaded build
they lock the per-object `ob_mutex`. gopy has no GIL, so it always needs the
lock. This spec ports the dict half of that subsystem.

## Design

A goroutine-reentrant per-dict lock, modeled on the proven `reentrantMutex` in
`module/_functools/module.go` rather than the full
`_PyCriticalSection_SuspendAll` suspend-on-block protocol.

- **Reentrant by goroutine id.** A dict operation can re-enter the same dict
  through a user callback (`__hash__`, `__eq__`, `__del__` firing on a Decref
  while the lock is held). Same-goroutine re-entry bumps a depth counter
  instead of self-deadlocking; cross-goroutine access fully serializes. This
  matches CPython holding the section across `insertdict` / `delitem_common` /
  `copy_lock_held` / the dict-iter `*_lock_held` paths, including the user code
  those call.
- **Held across user callbacks.** The lock is held for the whole operation,
  including the Decref of a replaced/removed value that may run `__del__`,
  exactly where CPython keeps its critical section open.
- **Zero-value ready.** The lock embeds in `Dict` as a value field with no
  constructor. `cond.L` is wired lazily on first acquire under the internal
  mutex (`if !r.ready { r.cond.L = &r.mu; r.ready = true }`). Safe because a
  `Dict` is only ever used by pointer, never copied by value.
- **Goroutine id.** Reuses the existing `objects.GoidHook` (the VM's fast
  getg-based hook) with a `runtime.Stack` parse fallback when the hook is nil.

Accepted trade-off: a goroutine that locks dict A then dict B while another
locks B then A can deadlock (no lock ordering). CPython's suspend protocol
avoids this; gopy's reentrant lock does not. Chosen deliberately: the suspend
protocol is a much larger port with its own deadlock surface, and the dict
operations gopy holds two dict locks across are confined to `copy` (where the
destination is a fresh `NewDict()` invisible to other goroutines, so no ABBA).

## API surface

New file `objects/dict_lock.go`:

- `dictMutex` struct: `sync.Mutex` + `sync.Cond` + `ready bool` + `owner int64`
  + `depth int`.
- `(r *dictMutex) lock()` / `unlock()`: the reentrant acquire/release.
- `(d *Dict) lock()` / `unlock()`: forward to the embedded `mu dictMutex`.
- `dictGoid() int64`: `GoidHook` with `runtime.Stack` fallback.

`Dict` gains one field: `mu dictMutex` (after `watcherTag`).

### Lock placement

Every table read and write acquires the lock. Internal helpers
(`dictResize`, `dictReinsert`, `dictInsertSplit`, `d.lookup`) are reached only
through an already-locked outer path; reentrancy covers the same-goroutine
nesting. `clearContents` takes no lock: it runs from dead-object teardown at
refcount 0, where there is no contention.

| Site | File | CPython |
| --- | --- | --- |
| `dictInsert` | dict_mutate.go | `dictobject.c:2707 PyDict_SetItem` |
| `dictSetDefault` | dict_mutate.go | `dictobject.c:4512 dict_setdefault_ref` |
| `dictDelete` | dict_mutate.go | `dictobject.c:2881 PyDict_DelItem` |
| `Keys` | dict.go | `dictobject.c:3193 keys_lock_held` |
| `ForEachWithHash` | dict.go | `dictobject.c:3492 _PyDict_Next` |
| `GetItem` | dict.go | `dictobject.c:1576 PyDict_GetItemRef` |
| `GetItemKnownHash` | dict.go | `dictobject.c:1576 PyDict_GetItemRef` |
| `Contains` | dict.go | `dictobject.c:2706 PyDict_Contains` |
| `ContainsKnownHash` | dict.go | `dictobject.c:2706 _PyDict_Contains_KnownHash` |
| `dictClearMethod` | dict.go | `dictobject.c:2938 PyDict_Clear` |
| `dictCopyMethod` | dict.go | `dictobject.c:4147 copy_lock_held` |
| `advance` (dict-iter) | dict_iter.go | `dictobject.c:5227 dictiter_iternextkey_lock_held` |

`dictCopyMethod` holds `src.lock()` across the entire snapshot-and-read loop
(`Keys` + `GetItem`) so the copy sees a consistent table; the destination is a
fresh `NewDict()`, so no ABBA. `advance` captures `d := it.src` before locking
so the deferred unlock survives `release()` nilling `it.src`.

## CPython citations

- `Python/critical_section.c:21 _PyCriticalSection_BeginSlow`
- `Python/critical_section.c:173 PyCriticalSection_End`
- `Include/object.h ob_mutex` (free-threaded `PyObject`)
- `Include/internal/pycore_pystate.h _PyThreadState_GET`
- per-site citations in the table above

## Verification

Build clean (`go build ./...`), `go vet ./objects/` clean,
`go test ./test/gate -run TestCfgPhaseParity` green.

Both MUST-PASS gates green:

- `test_iter.TestCase.test_ref_counting_behavior`: OK
- `test_frame.ClearTest.test_clear_refcycles`: OK

At-risk sweep, current binary vs base (`/tmp/gopy_base`, committed HEAD before
this work):

| Test | Current | Base | Verdict |
| --- | --- | --- | --- |
| `test_weakref` (137 tests) | 30 fail / 15 err, **no crash** | 31 fail / 18 err, fatal crash | fixed |
| `test_gc` | 29 fail / 3 err | 29 fail / 3 err | identical, no regression |
| `test_dict` | 1 out-of-scope fail | same | parity |
| `test_tuple` | 3 out-of-scope fail | same | parity |
| `test_list` / `test_set` / `test_iter` / `test_frame` | green | green | parity |
| `test_dict.test_setdefault` | OK | OK | parity |

The single `test_dict` failure (`test_splittable_popitem`) and the three
`test_tuple` track_* failures fail on base too; they are out of scope for this
spec. Net effect: the fatal `test_weakref` crash is removed with zero new
regressions.

## Checklist

- [x] `objects/dict_lock.go`: reentrant per-dict lock + `dictGoid`
- [x] `Dict.mu dictMutex` field
- [x] Lock all 12 read/write sites (table above)
- [x] `dictCopyMethod` holds src lock across snapshot loop
- [x] `advance` captures `it.src` before locking
- [x] Both MUST-PASS gates green
- [x] At-risk sweep: no regression vs base; `test_weakref` crash removed
- [x] `go vet` + `TestCfgPhaseParity` green
- [ ] Land the owned-store half on top (tracked in spec 1727)
