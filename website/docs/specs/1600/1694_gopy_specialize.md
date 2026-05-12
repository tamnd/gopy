---
format: md
id: 1694_gopy_specialize
title: "1694. gopy adaptive specialization"
sidebar_label: "1694. specialize"
sidebar_position: 1694
slug: /specs/1694-specialize
description: "v0.11 port of Python/specialize.c. PEP 659 quickening, the 16-bit backoff counter, the inline-cache layouts, the _Py_Specialize_* family, and the de-specialize / unquicken paths."
---


## Goal

Port CPython's adaptive specializer (PEP 659) into the gopy VM so
that hot bytecode rewrites itself into specialized variants on
warm-up and falls back cleanly on shape mismatch. v0.6 stopped at
the unspecialized Tier-1 interpreter and wired
`_Py_call_instrumentation` / specialize entry points to no-ops at
`vm/dispatch.go`. v0.11 turns those no-ops into the real machinery.

## Why specialize at all

`gopy` runs on Go's runtime, which already has a JIT-grade compiler
under the user code. A pure tree-walking interpreter would still be
fast enough for stdlib bringup. We port specialization for three
reasons that cannot be papered over downstream:

1. **`dis` parity**. `dis.dis(f)` on a warmed-up function shows
   `LOAD_ATTR_INSTANCE_VALUE`, `BINARY_OP_ADD_INT`,
   `CALL_PY_EXACT_ARGS`, etc. CPython's behaviour test suite asserts
   on these names. If we never specialize, every disassembly diverges.
2. **Inline-cache observability**. `sys._getframe().f_code.co_code`
   exposes the cache cells. Tooling (`coverage`, `line_profiler`,
   profilers using `sys.monitoring` / `sys.settrace`) reads them.
3. **Tier-2 hand-off**. The trace projection optimizer (v0.12) keys
   off specialized opcodes to start a trace. Without v0.11 the
   v0.12 entry point never fires.

We do not need the speed for its own sake. We do need the surface
shape.

## Sources of truth

| CPython file                                | Lines | Target                              |
|---------------------------------------------|------:|-------------------------------------|
| `Python/specialize.c`                       |  3232 | `specialize/` package               |
| `Include/internal/pycore_code.h`            |   ~70 | `specialize/cache.go` cache structs |
| `Include/internal/pycore_backoff.h`         |   133 | `specialize/backoff.go`             |
| `Include/internal/pycore_opcode_metadata.h` |  gen  | `vm/opcodes_gen.go` `_PyOpcode_Deopt[]` table |
| `Lib/opcode.py`                             |  data | parser-fed table for cache widths   |

`Python/bytecodes.c` (already ported) carries the per-family
inline-cache sizes (`INLINE_CACHE_ENTRIES_*`) inline in opcode
definitions. `pycore_code.h` is the canonical layout. The DSL
generator regenerates `vm/opcodes_gen.go` to add the deopt table.

## Package layout

```
specialize/
  backoff.go     16-bit counter helpers (pycore_backoff.h)
  cache.go       Inline cache struct types (pycore_code.h:67-161)
  quicken.go     _PyCode_Quicken: stamp initial counters at first call
  deopt.go       _PyOpcode_Deopt[] driven unquicken / unspecialize helpers
  stats.go       _Py_GatherStats: optional, behind a build tag
  opcodes/       One file per family. Each file ports the matching
                 _Py_Specialize_<Family> from specialize.c:
    attr.go        LOAD_ATTR     (specialize.c:1345)
    storeattr.go   STORE_ATTR    (specialize.c:1376)
    super.go       LOAD_SUPER_ATTR (specialize.c:828)
    binop.go       BINARY_OP     (specialize.c:2578)
    cmpop.go       COMPARE_OP    (specialize.c:2740)
    contains.go    CONTAINS_OP   (specialize.c:3109)
    storesubscr.go STORE_SUBSCR  (specialize.c:1895)
    foriter.go     FOR_ITER      (specialize.c:2910)
    send.go        SEND          (specialize.c:2965)
    tobool.go      TO_BOOL       (specialize.c:3035)
    call.go        CALL          (specialize.c:2183)
    callkw.go      CALL_KW       (specialize.c:2223)
    unpack.go      UNPACK_SEQUENCE (specialize.c:2803)
    loadglobal.go  LOAD_GLOBAL   (specialize.c:1775)
```

`vm/dispatch.go` already has `tryAdaptive(...)` placeholder hooks.
v0.11 swaps each placeholder for a real `specialize.<Family>` call.

## The 16-bit backoff counter

`specialize/backoff.go` ports `Include/internal/pycore_backoff.h`
verbatim:

```go
// CPython: Include/internal/pycore_backoff.h:34
const (
    BackoffBits        = 4
    MaxBackoff         = 12
    UnreachableBackoff = 15
)

// BackoffCounter packs a 12-bit value above a 4-bit backoff.
// CPython: Include/internal/pycore_structs.h _Py_BackoffCounter
type BackoffCounter struct {
    ValueAndBackoff uint16
}

func MakeBackoffCounter(value, backoff uint16) BackoffCounter
func RestartBackoffCounter(c BackoffCounter) BackoffCounter
func PauseBackoffCounter(c BackoffCounter) BackoffCounter
func AdvanceBackoffCounter(c BackoffCounter) BackoffCounter
func BackoffCounterTriggers(c BackoffCounter) bool
func IsUnreachable(c BackoffCounter) bool
```

The eval loop ticks via `AdvanceBackoffCounter` once per execution
of an adaptive opcode. When `BackoffCounterTriggers` returns true
the dispatch arm calls into the specializer for the matching
family. On specialize success the counter is reset via
`AdaptiveCounterCooldown` (a 6-bit value of 52, backoff 0); on
specialize miss the counter is reset via `RestartBackoffCounter` so
the next attempt waits exponentially longer.

CPython initial values (specialize.c top of file):

| Helper                          | value | backoff |
|---------------------------------|------:|--------:|
| `adaptive_counter_warmup`       |     1 |       1 |
| `adaptive_counter_cooldown`     |    52 |       0 |

`_PyCode_Quicken` stamps `adaptive_counter_warmup` into every
adaptive cache slot the first time a code object enters the
interpreter (specialize.c:459).

## Inline cache layouts

`specialize/cache.go` mirrors `pycore_code.h:67-161`. Each Go
struct's field order and width must match the C layout byte-for-byte
because the cache cells live inside the bytecode stream as
`_Py_CODEUNIT` (uint16) entries:

```go
// CPython: Include/internal/pycore_code.h:67 _PyLoadGlobalCache
type LoadGlobalCache struct {
    Counter            BackoffCounter
    ModuleKeysVersion  uint16
    BuiltinKeysVersion uint16
    Index              uint16
}

// CPython: Include/internal/pycore_code.h:108 _PyLoadMethodCache
type LoadMethodCache struct {
    Counter     BackoffCounter
    TypeVersion [2]uint16
    Keys        [2]uint16  // or DictOffset, union in C
    Descr       [4]uint16
}

// ... and the rest, one per family ...
```

`vm/codeunit.go` already exposes the bytecode buffer as a
`[]uint16`. The cache helpers cast a `*uint16` slice over the
adaptive instruction's tail. We keep the Go layout deterministic by
disabling field padding (all `uint16`); `unsafe.Sizeof` matches
`CACHE_ENTRIES(...)` exactly.

The corresponding `INLINE_CACHE_ENTRIES_*` constants come out of
the cache widths via Go's `unsafe.Sizeof / 2`. We pin them in
`specialize/cache_test.go` against the CPython numbers from
`_PyOpcode_Caches[]` in `pycore_opcode_metadata.h`:
`LOAD_ATTR=9`, `LOAD_GLOBAL=4`, `BINARY_OP=5`, `STORE_ATTR=4`,
`TO_BOOL=3`, `CALL=3`, `CALL_KW=3`, `FOR_ITER=1`, `SEND=1`,
`COMPARE_OP=1`, `STORE_SUBSCR=1`, `LOAD_SUPER_ATTR=1`,
`UNPACK_SEQUENCE=1`, `CONTAINS_OP=1`.

## The deopt table

`specialize/deopt.go` ports `_PyOpcode_Deopt[]` from
`pycore_opcode_metadata.h`. The table maps every specialized opcode
to its adaptive parent (e.g. `LOAD_ATTR_INSTANCE_VALUE` -> `LOAD_ATTR`,
`BINARY_OP_ADD_INT` -> `BINARY_OP`). The DSL generator that built
`vm/opcodes_gen.go` already has access to the metadata; v0.11
extends `tools/bytecodes_gen/` to emit the deopt entries alongside
the per-arm dispatch case.

The de-specialize path runs inside `vm/dispatch.go` whenever a
specialized arm hits a shape mismatch. It overwrites the in-stream
opcode with `_PyOpcode_Deopt[opcode]` (the adaptive parent) and
calls `PauseBackoffCounter` so the next execution falls into the
generic arm. On the *next* counter trip the generic arm will try to
re-specialize.

## Quicken

`specialize/quicken.go:Quicken` is the v0.11 port of
`_PyCode_Quicken` (specialize.c:459). It walks the bytecode array
once and, for every adaptive opcode in `_PyOpcode_Caches`, stamps
`adaptive_counter_warmup()` into the counter slot. CPython runs
this on first execution; gopy wires the call into
`vm/eval.go:Run` immediately after the frame's code object lands
on the dispatch loop and before the first `Tick`.

`Quicken` is idempotent. Re-running it is a no-op because the
counters are already warmup-shaped.

## Family-by-family port plan

Each file under `specialize/opcodes/` ports the matching
`_Py_Specialize_<Family>` function plus its private helpers from
`specialize.c`. The contract for every entry point is the same:

```go
// CPython: Python/specialize.c:1345 _Py_Specialize_LoadAttr
func LoadAttr(owner objects.Object, instr *vm.CodeUnit, name objects.Object)
```

It rewrites `*instr` to a specialized opcode (or to the adaptive
parent on a miss) and stamps the matching cache slot. It does not
allocate. It does not raise. It always leaves the counter in a
valid state.

### v0.11 must-port families (10)

These are the families the Python stdlib hits at warmup. Skipping
any of them leaves measurable disassembly drift on `dis.dis(...)`
fixtures that the `Lib/test/test_dis.py` panel asserts on.

1. **LOAD_ATTR** (specialize.c:1345). Five common variants
   (`INSTANCE_VALUE`, `MODULE`, `WITH_HINT`, `SLOT`, `CLASS`), method
   variants (`METHOD_*`), property variants. Reads
   `tp_version_tag`, `dk_version`. The largest family.
2. **BINARY_OP** (specialize.c:2578). `ADD_INT`, `SUBTRACT_INT`,
   `MULTIPLY_INT`, `ADD_FLOAT`, `SUBTRACT_FLOAT`, `MULTIPLY_FLOAT`,
   `ADD_UNICODE`, `INPLACE_ADD_UNICODE`. Pure type-tag dispatch.
3. **COMPARE_OP** (specialize.c:2740). `INT`, `FLOAT`, `STR`. The
   only family that interacts with the cmpop oparg encoding.
4. **LOAD_GLOBAL** (specialize.c:1775). `MODULE`, `BUILTIN`. Reads
   the dict keys version on globals/builtins.
5. **TO_BOOL** (specialize.c:3035). `BOOL`, `INT`, `LIST`, `NONE`,
   `STR`, `ALWAYS_TRUE`. Cheap, hit on every truthy test.
6. **STORE_SUBSCR** (specialize.c:1895). `LIST_INT`, `DICT`. Dict
   subscripts dominate stdlib.
7. **FOR_ITER** (specialize.c:2910). `LIST`, `TUPLE`, `RANGE`,
   `GEN`. Loop hot-path.
8. **STORE_ATTR** (specialize.c:1376). `INSTANCE_VALUE`,
   `WITH_HINT`, `SLOT`. Mirror of `LOAD_ATTR`.
9. **SEND** (specialize.c:2965). `GEN`. Generator/coroutine warmup.
   Generators on goroutines (1693) already expose
   `gi_frame_state`; the specialized arm reads it instead of
   round-tripping through the channel.
10. **CALL** (specialize.c:2183). `PY_EXACT_ARGS`, `PY_GENERAL`,
    `BOUND_METHOD_GENERAL`, `LIST_APPEND`, `BUILTIN_O`,
    `BUILTIN_FAST`, `METHOD_DESCRIPTOR_*`, `TYPE_1`, `STR_1`,
    `TUPLE_1`, etc. Second-largest family after LOAD_ATTR.

### Deferred families (3)

* **LOAD_SUPER_ATTR** (specialize.c:828). Used only by
  `super().method()`. Limited stdlib coverage. Defer to v0.11.1
  cleanup.
* **CONTAINS_OP** (specialize.c:3109). `IN_DICT`, `IN_LIST`,
  `IN_SET`, `IN_TUPLE`. Cheap; defer for cleanup pass.
* **CALL_KW** (specialize.c:2223). Mirror of CALL with kwargs.

These ship as pass-through specialize entry points (do nothing,
leave counter alone) until the cleanup pass.

### UNPACK_SEQUENCE (specialize.c:2803)

Specializes to `UNPACK_SEQUENCE_LIST`, `UNPACK_SEQUENCE_TUPLE`,
`UNPACK_SEQUENCE_TWO_TUPLE`. Trivial; ports in v0.11 alongside
FOR_ITER.

## Dependencies on other subsystems

The specializer reads several pieces of object/runtime state. Each
must already be wired up before the matching family ports:

| Specializer needs                         | Provided by    | Status |
|-------------------------------------------|----------------|--------|
| `tp_version_tag` on every type            | `objects/type` | shipped (v0.4) |
| `dk_version` on every dict keys           | `objects/dict` | shipped (v0.4) |
| `func_version` on every function          | `objects/function` | shipped (v0.7) |
| `co_version`, `co_quickened` flag on code | `objects/code` | shipped (v0.5) |
| `_Py_CODEUNIT` slice over bytecode        | `vm/codeunit`  | shipped (v0.6) |
| Frame `instr_ptr` advance                 | `vm/frame`     | shipped (v0.6) |
| `interp.callable_cache.object__getattribute__` | `state/callable_cache` | NEW in v0.11; tracked here |

The callable cache is a per-interpreter struct that caches
`object.__getattribute__` and a couple of other hot dunders so the
LOAD_ATTR specializer can identify them by pointer comparison
without doing a name lookup. We add it to `state/interp.go`.

## Generator hook into specialize entry

`vm/eval.go` runs each specialized opcode in three phases:

1. **Try specialized arm**. If the in-stream opcode matches a
   specialized variant, run it. On shape mismatch, deopt
   (overwrite opcode with adaptive parent, pause counter) and fall
   through.
2. **Tick adaptive counter**. `AdvanceBackoffCounter`. If
   triggers, call `specialize.<Family>(...)` then re-fetch the
   opcode and goto step 1.
3. **Run generic arm**. The original v0.6 implementation, kept
   intact.

This is a direct port of the dispatch loop in `Python/ceval.c`
under the `Py_TIER_2_DISABLED` arm.

## Tests

* **`specialize/backoff_test.go`**. Pin every helper from
  `pycore_backoff.h` against hand-computed bit patterns.
* **`specialize/cache_test.go`**. `unsafe.Sizeof` of every cache
  struct equals `CACHE_ENTRIES_<FAMILY> * 2`.
* **`specialize/quicken_test.go`**. After `Quicken(code)`, every
  adaptive cache slot's counter is `make_backoff_counter(1, 1)`.
* **`specialize/opcodes/<family>_test.go`**. Per-family golden
  panel: build a frame, run it once, assert the in-stream opcode
  rewrote to the expected specialized variant.
* **`vm/dis_specialize_test.go`**. End-to-end: parse, compile,
  warm a function for ~64 calls, `dis.dis` it, and pin the output
  against `python3 -m dis` byte-for-byte.

The end-to-end test is the v0.11 equivalent of the parser parity
gate (parser/parity_test.go from v0.10.2): same shape (run CPython
side by side, dump, byte-equal).

## Out of scope for v0.11

* JIT (`jit.c`). Stays a stub.
* Tier-2 trace projection (`optimizer.c`). v0.12.
* Adaptive counter tuning. We use CPython 3.14 values verbatim.
* Stats collection (`_Py_GatherStats`). Behind a build tag, not
  part of the v0.11 gate.

## Gate

`dis.dis(f)` on a warmed-up sample function from
`Lib/test/test_dis.py` matches CPython byte-for-byte across the
ten must-port families.
