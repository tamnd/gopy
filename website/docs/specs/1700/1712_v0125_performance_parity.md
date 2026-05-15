---
format: md
id: 1712_v0125_performance_parity
title: "1712. v0.12.5 performance parity"
sidebar_label: "1712. v0.12.5 performance"
sidebar_position: 1712
slug: /specs/1712-v0125-performance
description: "Port the CPython 3.14 performance-critical subsystems (specializer, inline caches, PyLong fast path, dict/str/frame layout, tier-2 optimizer body) so gopy runs within parity of cpython on the regrtest corpus, with a goal of 5x on hot loops."
---

## Why this spec exists

A 10-line `pyperformance` smoke ran on the v0.12.4 branch shows gopy
between 8x and 40x slower than `python3.14` on the same .py source.
That gap is not Go vs C cost. CPython 3.14 ships four pieces of
infrastructure gopy has only partially ported:

1. Specializer + inline caches (`Python/specialize.c`)
2. Tier-2 micro-op interpreter (`Python/executor_cases.c.h`,
   ~285 uops; gopy has 14)
3. Compact PyLong with small-int caching (`Objects/longobject.c`)
4. Compact PyUnicode with kind tags (`Objects/unicodeobject.c`)

Plus several hot-path data structures whose Go translations went for
clarity over throughput:

- `objects/dict.go`: linear-probe + Go `map[any]any` hybrid. CPython
  uses a single open-addressing table with split-keys for instance
  dicts.
- `vm/frame.go`: per-call allocation of `[]Object` locals + `*Frame`.
  CPython recycles frame objects via a per-thread free list.
- `objects/object.go`: every attribute dispatch walks the MRO via Go
  interface assertions. CPython caches the slot pointer at type
  creation.

This spec is the umbrella that drives those ports to the point where
gopy clears `pyperformance` within 1.5x of cpython on geometric mean,
and within 5x on every individual benchmark.

## Goal

| Bench                  | cpython 3.14 | gopy target | gopy today (est.) |
| ---------------------- | -----------: | ----------: | ----------------: |
| `pyperformance` geomean| 1.0x         | <=1.5x      | ~12x              |
| `nbody`                | 1.0x         | <=2.0x      | ~18x              |
| `fannkuch`             | 1.0x         | <=2.0x      | ~25x              |
| `richards`             | 1.0x         | <=2.0x      | ~30x              |
| `unpack_sequence`      | 1.0x         | <=2.0x      | ~8x               |
| `call_method`          | 1.0x         | <=1.5x      | ~15x              |

"5x faster than cpython" on a tight loop is reachable for code paths
where Go's static dispatch + escape-analysis stack allocation can
beat the cpython object allocator, but only after the specializer
lands. Without it, every `LOAD_ATTR` walks the MRO.

## Subsystems to port (in cost order)

### P1. Specializer + inline caches  --- `Python/specialize.c` (~3500 LOC)

Wires the adaptive instruction set: `LOAD_ATTR_INSTANCE_VALUE`,
`LOAD_ATTR_SLOT`, `CALL_PY_EXACT_ARGS`, `BINARY_OP_ADD_INT`, and the
counter-driven specialization machinery that promotes generic opcodes
to type-specialized variants after N hits.

Without this every attribute load is a dict lookup. Estimated 4-6x
geomean win.

Ports needed:
- `_Py_Specialize_LoadAttr` (and the 12 specialized variants)
- `_Py_Specialize_BinaryOp` (and the 9 numeric/sequence variants)
- `_Py_Specialize_Call` (PY_EXACT_ARGS, BUILTIN_O, BUILTIN_FAST, etc.)
- `_Py_Specialize_ToBool`, `_Py_Specialize_StoreAttr`, `_Py_Specialize_StoreSubscr`
- Inline cache layout in `vm/cache.go` mirroring the per-op cache
  struct in `Include/internal/pycore_code.h`

Gate: `vm/specialize_test.go` walks the adaptive transitions and
asserts the right specialized opcode replaces the generic after
warmup.

### P2. Tier-2 micro-op interpreter body  --- `Python/executor_cases.c.h`

Spec 1693 shipped the dispatch loop and 14 uops. There are about 285
uops total. The hot ones are the same as the specialized opcodes
above (`_LOAD_FAST`, `_STORE_FAST`, `_BINARY_OP_ADD_INT`,
`_GUARD_BOTH_INT`, `_LOAD_ATTR_INSTANCE_VALUE`, ...).

Goal: hand-port the 60 hottest uops; they cover ~95% of dynamic
dispatch on the pyperformance corpus per CPython's own profiling.

Gate: `vm/uop_test.go` with one positive + one guard-fail case per
uop. Add a benchmark `BenchmarkTier2Nbody` that compares the tier-1
and tier-2 paths.

### P3. PyLong fast path  --- `Objects/longobject.c`

gopy currently routes every integer through `math/big.Int`, which
allocates per op. CPython stores small ints (|n| < 2^30) inline in
the PyLong header and caches PyLong objects for [-5, 256].

Ports:
- Compact `Long` representation with a single-word `digit` field
- `_PyLong_Add` / `_PyLong_Sub` / `_PyLong_Multiply` fast path that
  bypasses big.Int for single-word operands
- Small-int cache (`_PyLong_GetSmallInt`)

Estimated 3x win on numeric-heavy code.

Gate: `objects/long_arith_test.go` cross-checks the fast path against
the big.Int slow path on randomized inputs; `BenchmarkLongAdd` proves
the fast path is allocation-free.

### P4. PyUnicode kind-tagged strings  --- `Objects/unicodeobject.c`

CPython stores three string flavors (1-byte Latin-1, 2-byte BMP,
4-byte full Unicode) with a single contiguous buffer chosen at
creation. gopy uses Go's UTF-8 `string` everywhere, which means every
indexed access is O(n) byte-walking.

Ports:
- `PyUnicode_Kind` tags on `Unicode`
- Kind-specific fast paths for `find`, `count`, `replace`, slicing
- `PyUnicode_New` / `PyUnicode_FromKindAndData`
- `PyUnicode_READ` macro -> kind-dispatched index op

Estimated 2x win on string-heavy code (text processing benches).

Gate: `objects/unicode_kind_test.go` covers all three kinds across
the indexing/slicing surface; `BenchmarkStrFind` shows kind-1
strings hit the byte-find fast path.

### P5. Dict open-addressing  --- `Objects/dictobject.c`

gopy's `objects/dict.go` uses Go's built-in `map[any]any` plus a
parallel insertion-order slice. Each lookup pays Go map overhead
(hash + interface boxing) plus the slice walk.

CPython uses a single open-addressing table with `dk_indices`
(compact index array) + `dk_entries` (key/value/hash triples).

Ports:
- `PyDictKeysObject` layout: split index array + entries array
- `lookdict` open-addressing probe (Python/dictobject.c:973)
- Split-keys dict (`dictobject.c:543 new_keys_object`) for instance
  __dict__: keys shared with the type, values inline in the object

Estimated 2x win on call-method and attribute-heavy code.

Gate: `objects/dict_oa_test.go` plus `BenchmarkDictLookup` showing
the new layout matches CPython's allocation count.

### P6. Frame free-list + LOAD_FAST_CHECK  --- `Objects/frameobject.c`, `Python/ceval.c`

CPython recycles frames through a per-thread free list (capped at
20) and uses `_PyEval_FrameClearAndPop` to reset rather than free.
gopy allocates a new `*Frame` and `[]Object` on every call.

Ports:
- `Frame` pool in `vm/frame_pool.go`
- `LOAD_FAST_CHECK` opcode (the unbound-local check that the
  specializer relies on)

Estimated 1.5x win on call-heavy code.

Gate: `vm/frame_pool_test.go`; `BenchmarkCallNop` shows allocations
drop to 0 on the hot path.

### P7. Object slot caching  --- `Objects/typeobject.c`

CPython resolves dunder methods (`__add__`, `__len__`, `__iter__`)
once at type-creation time and caches them in the type's slot table.
gopy currently re-walks the MRO via `Lookup` on every operator.

Ports:
- `_PyType_AssignSpecialMethods` walking the MRO once per type
- Type slot table populated for every special method gopy already
  understands (~40 slots)
- Cache invalidation on `__class__` reassignment / `type` mutation

Estimated 1.5x win on operator-heavy code.

Gate: existing operator tests stay green; new `objects/slots_test.go`
asserts the slot cache is populated correctly and invalidates on
type mutation.

## Checklist

| Subsystem                       | CPython source         | gopy destination          | Estimated win | Status | Commit |
|---------------------------------|------------------------|---------------------------|--------------:|--------|--------|
| P1. Specializer + IC            | `Python/specialize.c`  | `vm/specialize.go`        | 4-6x          | TODO   | -      |
| P2. Tier-2 uop bodies           | `Python/executor_cases.c.h` | `vm/uop_*.go`        | 1.5-2x        | TODO   | -      |
| P3. PyLong fast path            | `Objects/longobject.c` | `objects/long_fast.go`    | 3x            | TODO   | -      |
| P4. PyUnicode kind tags         | `Objects/unicodeobject.c` | `objects/unicode_kind.go` | 2x         | TODO   | -      |
| P5. Dict open-addressing        | `Objects/dictobject.c` | `objects/dict_oa.go`      | 2x            | TODO   | -      |
| P6. Frame free-list             | `Objects/frameobject.c` | `vm/frame_pool.go`       | 1.5x          | TODO   | -      |
| P7. Type slot cache             | `Objects/typeobject.c` | `objects/type_slots.go`   | 1.5x          | TODO   | -      |

## Bench harness

A new `bench/pyperformance_runner.go` wraps `python -m pyperformance
run --python=gopy` (the gopy binary takes the same `script.py` arg).
The CI gate compares against a recorded `bench/baseline_v0124.json`;
regressions over 10% fail the build.

The harness must:

- Run cpython and gopy back-to-back on the same source files
- Capture wall-clock geomean per benchmark
- Emit a side-by-side markdown table the spec page renders

## Sources of truth

| CPython file                       | Lines | What it gives us                       |
|------------------------------------|------:|----------------------------------------|
| `Python/specialize.c`              | 3500  | The full specializer                   |
| `Python/executor_cases.c.h`        | 4200  | The 285 tier-2 uop bodies              |
| `Objects/longobject.c`             | 6400  | Compact small-int + fast path arith    |
| `Objects/unicodeobject.c`          | 16000 | Kind-tagged string layout              |
| `Objects/dictobject.c`             | 4800  | Open-addressing + split keys           |
| `Objects/frameobject.c`            | 1100  | Frame free-list                        |
| `Objects/typeobject.c`             | 11000 | Slot caching                           |
| `Include/internal/pycore_code.h`   | 600   | Inline cache layouts                   |

## Risk + scope notes

- The specializer is the only one of the seven items that *requires*
  the others to ship first. P3 / P4 / P5 / P6 / P7 can land in any
  order. Recommend starting with P3 (Long fast path) since it is the
  smallest LOC count and unblocks the integer-heavy benchmarks.
- pyperformance must run under gopy, which today fails on
  `import pyperf`. Vendor `Lib/pyperf` (or a minimal subset) as a
  prerequisite for the CI gate.
- The 5x-faster-than-CPython aspirational target only holds on tight
  loops where Go's escape analysis stack-allocates frame locals and
  the specializer has already promoted to the type-specialized op.
  Geomean parity (1.5x) is the realistic ship gate.
