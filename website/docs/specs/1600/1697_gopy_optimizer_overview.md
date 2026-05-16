---
format: md
id: 1697_gopy_optimizer_overview
title: "1697. gopy Tier-2 optimizer overview"
sidebar_label: "1697. optimizer_overview"
sidebar_position: 1697
slug: /specs/1697-optimizer_overview
description: "v0.12 port of Python/optimizer.c. Trace projection, executor lifecycle, ENTER_EXECUTOR side-table on Code.Executors, and the bloom filter for invalidation."
---


## Goal

Port CPython's Tier-2 trace projection and executor lifecycle into
the gopy VM. v0.11 left specialized opcodes living in the Tier-1
dispatch loop. v0.12 adds the second tier underneath: when a hot
loop crosses its warm-up threshold the runtime projects a linear
trace of micro-ops out of the specialized bytecode, runs the
trace through the abstract interpreter (spec 1699), and stores the
resulting executor on the side. The next time control reaches the
same bytecode offset the dispatch loop hands off to the executor
through `ENTER_EXECUTOR`.

The matching JIT path (`jit.c`) is explicitly out of scope.
gopy's Tier-2 stays interpreter-only. The executor object holds a
buffer of `_PyUOpInstruction` entries and the uop interpreter
(spec 1698) walks them.

## Why Tier-2 at all

Two reasons that cannot be papered over downstream:

1. **`dis.dis` parity**. CPython's behaviour test suite asserts on
   `ENTER_EXECUTOR` rows in disassembly listings of warmed-up
   functions. Without v0.12 the disassembly diverges as soon as a
   loop crosses the warm-up threshold.
2. **Profile observability**. `sys.monitoring` events and
   `sys.settrace` callbacks fire on the Tier-2 path through the
   reverse mapping the executor stores back to the original
   bytecode offsets. Tools that read frame state expect to see
   the original opcodes; the runtime returns them by walking the
   uop trace's `target` field. v0.11 wired these for the Tier-1
   path; v0.12 makes them work when execution is actually inside
   the trace.

We do not need the absolute speed gain. We do need the same shape,
the same hooks, and the same observable state.

## Sources of truth

| CPython file                                | Lines | Target                              |
|---------------------------------------------|------:|-------------------------------------|
| `Python/optimizer.c`                        |  1755 | `optimizer/` package                |
| `Include/internal/pycore_optimizer.h`       |   313 | `optimizer/types.go` shapes         |
| `Include/internal/pycore_jit.h`             |   ~30 | stubbed; `JitState` returns "no executor" |
| `Lib/test/test_capi/test_opt.py`            |  data | gate fixtures for trace shape       |

The two adjacent specs cover the dependent halves:

- 1698 covers `optimizer_bytecodes.c`, `optimizer_cases.c.h`,
  `pycore_uop_ids.h`, `pycore_uop_metadata.h`, the DSL generator,
  and the uop interpreter. v0.12 needs all three pieces to
  ship together.
- 1699 covers `optimizer_analysis.c` and `optimizer_symbols.c`.
  The trace projection in this spec produces the buffer the
  analysis pass consumes.

## Package layout

```
optimizer/
  types.go        _PyExecutorObject, _PyUOpInstruction,
                  _PyExecutorArray, _PyBloomFilter shapes
                  (pycore_optimizer.h:75-104)
  executor.go     allocate_executor, executor_clear,
                  unlink_executor, free_executor,
                  add_to_pending_deletion_list
                  (optimizer.c:1104-1184)
  trace.go        translate_bytecode_to_trace,
                  add_to_trace, count_exits, make_exit
                  (optimizer.c:478-1029)
  optimize.go     _PyOptimizer_Optimize entry point,
                  uop_optimize, prepare_for_execution,
                  effective_trace_length
                  (optimizer.c:114-1273)
  bloom.go        _Py_BloomFilter_Init / Add / MayContain
                  (optimizer.c:1382-1407)
  side_table.go   has_space_for_executor,
                  get_index_for_executor, insert_executor,
                  get_executor_lock_held, _Py_GetExecutor
                  (optimizer.c:34-193)
  pyobject.go     _PyExecutorObject as a real Python object:
                  is_valid, get_opcode, get_oparg, len, item,
                  traverse, dealloc (optimizer.c:193-416)
```

## Executor lifecycle

The executor object is the v0.12 unit of work. CPython's
`_PyExecutorObject` is a Python object that owns:

- `trace`: a `[]UopInstruction` of length up to `UOP_MAX_TRACE_LENGTH`
  (800).
- `dependencies`: a `BloomFilter` over types/dicts/code objects the
  trace's guards key off.
- `vm_data`: a thread-list link plus the back-pointer to the
  `Code` and bytecode offset where the executor was installed.

Lifecycle:

1. **Birth**. The Tier-1 dispatch loop reaches a warmed-up
   `JUMP_BACKWARD` at offset `i` of `Code.Code`. It calls
   `optimize.Optimize(frame, &Code.Code[i])`. That function reads
   the specialized bytecode forward, projects a uop trace, runs
   the analysis pass (spec 1699), and produces an
   `_PyExecutorObject` with the optimized trace.
2. **Installation**. `insert_executor` writes the executor pointer
   into `Code.Executors` (a `_PyExecutorArray` of up to 256
   entries), and rewrites `Code.Code[i]` to `ENTER_EXECUTOR` with
   the side-table index in oparg. The original opcode is preserved
   in the executor's `vm_data` so deopt can restore it.
3. **Use**. Subsequent dispatch hits `ENTER_EXECUTOR` at offset
   `i`, looks up `Code.Executors[oparg]`, and hands off to the
   uop interpreter (spec 1698).
4. **Invalidation**. A type-watcher / dict-watcher fires when a
   dependency the trace assumes mutates. The runtime walks the
   per-interpreter executor list and asks each executor's
   `dependencies.MayContain(...)` whether the mutation is in
   scope; matches are added to the deletion list and unlinked.
5. **Death**. `unlink_executor` removes the executor from the
   per-interpreter list, restores the original opcode at
   `Code.Code[i]`, and the Python `tp_dealloc` runs once the GC
   walks the deletion queue. CPython routes deletion through a
   pending list because `tp_dealloc` cannot run while another
   thread is inside the executor; gopy mirrors that with the
   same two-phase approach via `Code.Executors` plus the
   `pending_deletion_list` on `InterpState`.

## Trace projection

The projection walks specialized bytecode forward from the
warm-up `JUMP_BACKWARD` and emits one or more uops per source
opcode. Each opcode's expansion is recorded by the
`_PyOpcode_macro_expansion` table (spec 1698 covers how that
table is generated from `optimizer_bytecodes.c`).

```
LOAD_FAST       i           ──▶  _LOAD_FAST(i)
BINARY_OP_ADD_INT  arg=add  ──▶  _GUARD_BOTH_INT
                                _BINARY_OP_ADD_INT
                                _CHECK_VALIDITY
JUMP_BACKWARD   delta       ──▶  _JUMP_TO_TOP                (loop close)
```

Each emitted `_PyUOpInstruction` carries:

- `opcode`: the uop ID (an entry in `pycore_uop_ids.h`).
- `oparg`: 32-bit value sized to the uop family.
- `target`: the original bytecode offset of the source opcode,
  used by `sys.monitoring` / `sys.settrace` to resolve back to
  the user's code.
- `operand`: 64-bit operand (constant, type pointer, ...).

The walk stops on:

- `JUMP_BACKWARD` whose delta lands on the trace's start (the
  expected loop close; emit `_JUMP_TO_TOP`).
- A non-projectable opcode (emit `_DEOPT` and bail).
- The trace reaches `UOP_MAX_TRACE_LENGTH`.
- A second branch out of a guard (we project one straight-line
  trace; deopt arms become exits).

CPython's `translate_bytecode_to_trace` is the canonical reference
([Python/optimizer.c:554-1001](https://github.com/python/cpython/blob/v3.14.5/Python/optimizer.c#L554-L1001)). The Go port mirrors its control
flow byte for byte, including the projection of inlined Python
calls (`PUSH_FRAME`).

## Trace shape

```go
type UopInstruction struct {
    Opcode  uint16
    Oparg   uint32
    Target  uint32
    Operand uint64
}
```

`UopInstruction` matches `_PyUOpInstruction` field for field. The
analysis pass writes back into the same buffer; the executor
copies the post-analysis buffer when it allocates.

## Bloom filter for invalidation

`_Py_BloomFilter` packs four 64-bit hashes per entry. Trace
projection adds every type pointer, dict-keys pointer, and code
object pointer it guards against to the executor's filter via
`_Py_BloomFilter_Add`. When a watcher fires the runtime hashes
the mutated object once and walks the executor list checking
`bloom_filter_may_contain`. False positives are fine; false
negatives are not. The filter's role is to keep the invalidation
walk O(1) per executor in the common case.

## Wiring

`vm/dispatch.go` grows two new arms:

- `JUMP_BACKWARD`. The existing arm decrements a per-instruction
  warmup counter on the bytecode side table. When it triggers,
  call `optimizer.Optimize(frame, code, offset)`. On success
  the in-place rewrite to `ENTER_EXECUTOR` happens before the
  next dispatch tick.
- `ENTER_EXECUTOR`. New arm. Looks up `code.Executors[oparg]`,
  reads the executor's trace, and runs the uop interpreter
  (spec 1698). On uop interpreter return the dispatch loop reads
  the executor's exit info to find the next bytecode offset to
  resume Tier-1 at.

`objects/code.go` grows the side table:

```go
type Code struct {
    // ... existing fields
    Executors *ExecutorArray
}

type ExecutorArray struct {
    Capacity int
    Size     int
    Entries  []*ExecutorObject
}
```

`ExecutorObject` lives in the `optimizer` package and implements
`objects.Object` so it round-trips through `dis.dis` and the
debugger surfaces.

## Gate

`v012test/optimizer_gate_test.go` covers four end-to-end scenarios:

1. **Trace projection**. Build a tight `for i in range(N): s += i`
   loop, compile it, run it past the warm-up threshold, and
   assert that `Code.Executors` has one entry whose trace
   contains `_GUARD_BOTH_INT`, `_BINARY_OP_ADD_INT`, and
   `_JUMP_TO_TOP`.
2. **`ENTER_EXECUTOR` dispatch**. Run the same loop a second time
   and assert the second iteration goes through the executor's
   trace, not the bytecode side. Drive with a counter on a
   uop-interpreter hook.
3. **Invalidation**. Mutate a type-attr the trace guards on, run
   the loop again, and assert the executor was unlinked and the
   bytecode at the warm-up offset is back to `JUMP_BACKWARD`.
4. **Deopt / exit**. Force a guard miss inside the trace, assert
   the uop interpreter returns to Tier-1 at the right bytecode
   offset, and assert subsequent execution still produces the
   right result.

A separate `dis.dis` parity row is added to the v0.10.2 parity
fixture set: a warmed-up function whose disassembly contains
`ENTER_EXECUTOR` must dump byte-equal to CPython 3.14's output
on the same source.
