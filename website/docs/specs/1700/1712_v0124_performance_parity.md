---
format: md
id: 1712_v0124_performance_parity
title: "1712. v0.12.4 performance parity"
sidebar_label: "1712. v0.12.4 performance"
sidebar_position: 1712
slug: /specs/1712-v0124-performance
description: "Stand up the pyperformance harness across cpython 3.14, PyPy 3.11, and gopy; audit every performance-critical subsystem already in tree (specializer, tier-2 uops, PyLong cache, dict split-keys, frame, slot dispatch); wire the ones that are idle; finish the ones that are stubs; add the subsystems still missing (float fast path, GC tracking, generator fast path, pickle/xml/sqlite native paths, string builder) so gopy clears pyperformance within 1.5x of cpython on geomean."
---

## Ground rule

Port full subsystems / files one by one. No partial slices, no
name-only shims, no "patch the gate and move on". When a phase here
touches a CPython source file, every function in that file lands in
the corresponding gopy package with a `// CPython:` citation before
the phase flips to DONE. The cost of revisiting a half-ported
subsystem is always higher than the cost of finishing it the first
time. This rule overrides any pressure to ship a row green early.

## Why this spec exists

A 10-line `pyperformance` smoke ran on the v0.12.4 branch shows gopy
between 8x and 40x slower than `python3.14` on the same .py source.
The first warm-up run (see "Current benchmark results" below) puts
geomean at ~283x cpython, with three benchmarks failing outright.

That gap is **not** Go vs C cost. The gap is structural: gopy has
shipped most of the performance machinery (specializer at
~3500 LOC under `specialize/`, tier-2 uops at ~23k LOC under
`optimizer/`, small-int cache, dict split-keys, generator,
float, slot tables) but the machinery is either not wired into
the eval loop, gated behind a flag nothing flips, or stops short
of the dispatch paths the benchmarks actually take.

This spec is the umbrella that drives the audit + wire-up + the
remaining ports to the point where gopy clears `pyperformance`
within 1.5x of cpython on geomean, and within 5x on every
individual benchmark in the small-subset gate.

The single biggest blocker is wiring, not porting: `Code.Quickened`
is declared in `objects/code.go:76`, read by `vm/adaptive.go:41/54/73`
and `monitor/install.go:126,177`, and **never assigned `true`
anywhere in the runtime**. The specializer is fully written and
tested; it just doesn't fire. That alone is most of the geomean
gap.

## Goal

| Bench                  | cpython 3.14 | gopy target | gopy 2026-05-16 |
| ---------------------- | -----------: | ----------: | --------------: |
| `pyperformance` geomean| 1.0x         | <=1.5x      |          283x   |
| `nbody`                | 1.0x         | <=2.0x      |        N/A (P8) |
| `fannkuch`             | 1.0x         | <=2.0x      |        N/A (P8) |
| `richards`             | 1.0x         | <=2.0x      |         1899x   |
| `unpack_sequence`      | 1.0x         | <=2.0x      |          254x   |
| `call_method`          | 1.0x         | <=1.5x      |         2407x   |
| `regex_compile`        | 1.0x         | <=2.0x      |         1952x   |
| `pidigits`             | 1.0x         | <=2.0x      |         7.83x   |
| `json_dumps`           | 1.0x         | <=2.0x      |        N/A (P9) |

## Benchmark coverage matrix

Each benchmark is unlocked by one or more subsystems below. A bench
"unlocked" by P_n means P_n is the principal contributor to closing
the gap on that bench; PRs targeting P_n must show the corresponding
column in "Current benchmark results" moves.

| Benchmark         | Primary             | Secondary           | Tertiary  |
|-------------------|---------------------|---------------------|-----------|
| `nbody`           | P8 (fix)            | P10 (float)         | P1, P2    |
| `fannkuch`        | P8 (fix)            | P1                  | P5        |
| `richards`        | P1 (specializer)    | P7 (slot cache)     | P6        |
| `call_method`     | P1                  | P7                  | P6        |
| `unpack_sequence` | P2 (tier-2 uops)    | P6 (frame)          | P1        |
| `regex_compile`   | P1                  | P4 (kind strings)   | P15 (str builder) |
| `json_dumps`      | P9 (fix)            | P15 (str builder)   | P3        |
| `pidigits`        | P3 (long fast path) | P1                  | -         |
| `pyflate`         | P3                  | P10                 | P1        |
| `raytrace`        | P10 (float fast)    | P1                  | P7        |
| `scimark_*`       | P10                 | P1                  | P2        |
| `spectral_norm`   | P10                 | P1                  | -         |
| `float`           | P10                 | -                   | -         |
| `generators`      | P12 (gen fast path) | P6                  | -         |
| `async_tree_*`    | P12                 | P6                  | -         |
| `gc_collect`      | P13 (GC)            | P6                  | -         |
| `pickle`          | P14 (_pickle)       | P3                  | P5        |
| `unpickle`        | P14                 | P3                  | P5        |
| `xml_etree`       | P14 (_elementtree)  | P4                  | P15       |
| `tomli_loads`     | P15                 | P4                  | -         |
| `logging`         | P15 (str builder)   | P1                  | -         |
| `django_template` | P15                 | P1                  | P7        |
| `mako`            | P15                 | P1                  | P7        |
| `chaos`           | P10                 | P1                  | P2        |
| `deltablue`       | P1                  | P7                  | P6        |
| `go`              | P1                  | P5 (dict)           | P3        |
| `hexiom`          | P1                  | P5                  | P3        |
| `nqueens`         | P2                  | P1                  | P5        |
| `meteor_contest`  | P5                  | P4                  | P1        |
| `comprehensions`  | P2                  | P6                  | P5        |
| `deepcopy`        | P13                 | P5                  | P7        |
| `pprint`          | P15                 | P1                  | P5        |
| `sqlite_synth`    | P14 (_sqlite)       | -                   | -         |
| `tornado_http`    | P12                 | P15                 | P5        |
| `typing_runtime`  | P7                  | P5                  | P1        |

## Subsystems (audit + ports)

Each subsystem below lists, in order:

1. **Audit** — what's already in tree (files + LOC) and what's idle
2. **Gap** — concrete missing piece(s)
3. **Phases** — shippable chunks, in PR-sized increments
4. **Gate** — the test/bench signal that proves the phase landed
5. **Estimated win** — geomean impact when the phase ships

### P0. pyperformance harness — three-way baseline gate

**Audit.** `bench/` shipped 2026-05-16. `install_cpython.sh`,
`install_pypy.sh` (pinned to PyPy 3.11 v7.3.22 outside the working
tree at `$HOME/pypy3.11/`), `run_one.sh`, `run_small.sh`,
`run_full.sh`, `cmd/compare/main.go`. Eight standalone benches under
`bench_sources/`. First end-to-end run on M4 + macOS 15.7.7 produced
the table in "Current benchmark results" below.

**Gap.**

- `run_full.sh` is a placeholder; pyperformance's full corpus has
  not been driven through `run_one.sh` against gopy yet.
- No CI gate. `baseline_v0124.json` not frozen.
- Bench-source iteration counts are tuned for cpython ~30-300 ms;
  PyPy is now warm (geomean 0.80x cpython, matching published 7.3
  numbers) but gopy times balloon to 80 s on the dispatch benches.
  Need an automatic gopy-only iteration shrink for benches where
  gopy is >100x cpython, so the small subset stays under 10 min.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P0.1 | Automatic iteration scaler in `run_one.sh`: probe cpython wall time, then scale bench `iter_count` for gopy via `GOPY_BENCH_SCALE` env var so wall time stays under 30 s. Shipped: `BASELINE_JSON` + `TARGET_WALL_MS` + `EST_SLOWDOWN` drive `bench_scale()`, which sets `GOPY_BENCH_SCALE` per bench and scales measured wall time back up. | DONE | ca0bef1 |
| P0.2 | Freeze `bench/baseline_v0124.json`. Add `bench/compare-baseline` subcommand: a >10% regression on the same host fails CI. Shipped: `bench/baseline_v0124.json` + `bench/cmd/compare-baseline/main.go` (tolerance flag, status-drop + regression gates, exits non-zero on either). | DONE | ca0bef1 |
| P0.3 | Wire `bench/run_small.sh` into `.github/workflows/`. Run nightly + on every PR that touches `compile/`, `vm/`, `specialize/`, `optimizer/`, `objects/`. Shipped: `.github/workflows/bench.yml` (schedule + path-filtered pull_request + workflow_dispatch), uploads `results_small.md` and the raw JSONs as artifacts. | DONE | ca0bef1 |
| P0.4 | Extend `bench_sources/` to cover every primary-column bench in the coverage matrix that gopy can currently run. Target: 20 benches. Shipped: 20 standalone scripts under `bench/bench_sources/` (call_method, chaos, comprehensions, deepcopy, fannkuch, float, go_bench, hexiom, json_dumps, logging_bench, nbody, nqueens, pidigits, pprint_bench, raytrace, regex_compile, richards, spectral_norm, typing_runtime, unpack_sequence). | DONE | ca0bef1 |
| P0.5 | `run_full.sh` against pyperformance's vendored sources via the existing shim; mark unsupported benches as `module_missing` rather than `N/A`. Current `run_full.sh` walks `bench_sources/` only; vendored pyperformance corpus + `module_missing` classification still pending. | WIP | - |

**Gate.** `bench/run_small.sh` exit 0 + table written to
`bench/results_small.md`; CI re-runs and the regression check passes.

**Estimated win.** n/a (tooling).

### P1. Specializer + inline caches — `Python/specialize.c`

**Audit.** Already in tree at ~3500 LOC under `specialize/`:

| File                       | Role                                         |
|----------------------------|----------------------------------------------|
| `backoff.go`               | 16-bit warmup/cooldown counter machinery     |
| `cache.go`                 | Per-op cache cell layouts                    |
| `core.go` + `quicken.go`   | `Specialize()` rewriter + `Quicken()` seeder |
| `load_attr.go`             | 12 LOAD_ATTR specialized variants            |
| `binary_op.go`             | 9 BINARY_OP variants (INT/FLOAT/STR x +,-,*)  |
| `call.go` + `call_kw.go`   | CALL_PY_EXACT_ARGS, BUILTIN_O/FAST, BOUND_METHOD |
| `compare_op.go`            | COMPARE_OP_INT/FLOAT/STR                     |
| `contains_op.go`           | CONTAINS_OP_DICT/SET                         |
| `for_iter.go`              | FOR_ITER_LIST/TUPLE/RANGE/GEN                |
| `load_global.go`           | LOAD_GLOBAL_MODULE/BUILTIN                   |
| `load_super_attr.go`       | LOAD_SUPER_ATTR_ATTR/METHOD                  |
| `send.go`                  | SEND_GEN                                     |
| `store_attr.go`            | STORE_ATTR_INSTANCE_VALUE/SLOT/WITH_HINT     |
| `store_subscr.go`          | STORE_SUBSCR_LIST_INT/DICT                   |
| `to_bool.go`               | TO_BOOL_INT/FLOAT/STR/NONE/BOOL/LIST         |
| `unpack_sequence.go`       | UNPACK_SEQUENCE_TUPLE/LIST/TWO_TUPLE         |
| `deopt.go`                 | Specialized → adaptive parent table          |

Tests cover the table extensively.

**Gap (the smoking gun — two-part).**

1. `Code.Quickened` is never set true at runtime:

   ```
   $ rg "Quickened\s*=\s*true" --type go        # zero hits in runtime
   $ rg "Quickened" --type go | rg -v _test     # all reads, no writes
     objects/code.go:76        Quickened bool       (declaration)
     vm/adaptive.go:41,54,73   if !e.f.Code.Quickened { return }
     monitor/install.go:126,177 same gate
   ```

2. The compiler emits **no inline `CACHE` cells**. Confirmed
   experimentally on 2026-05-16: setting `Quickened = true` from
   `liftCode` / `liftNestedCode` / `unmarshalCode` corrupts every
   non-trivial program (the IP walks off the end at len=8 for
   `1 == 1`) because `specialize.Quicken` writes seed counters into
   what it expects to be `CACHE` codeunits but are actually real
   opcodes. CPython's `Python/compile.c:write_instr` emits a `CACHE`
   pseudo-op block sized by `_PyOpcode_Caches[op]` after every
   adaptive instruction; the assembler serializes them as zero
   codeunits; `_PyCode_Quicken` is what fills them in.

Until both gaps are closed, every adaptive opcode's "attempt to
specialize" path is short-circuited. `call_method` (2407x cpython)
is the most visible victim — every method call rebuilds the bound
method, walks the MRO, allocates a tuple of args, even though
`LOAD_ATTR_METHOD_WITH_VALUES` and `CALL_PY_EXACT_ARGS` are both
written and tested.

Adjacent gaps surface once the above are closed:

- The eval loop's `LOAD_ATTR_*` dispatch table needs an entry point
  for every specialized variant declared in `specialize/load_attr.go`.
  Spot-check `vm/eval.go` and `vm/eval_call.go` for missing case
  arms.
- `monitor/install.go:177` only Quickens when monitoring is off; the
  default path on import skips it. Wiring belongs in
  `pythonrun/run.go` (after parse → compile → marshal load) and
  `imp/` (after `marshal.loads(.pyc body)`).

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P1.0 | Port `Python/compile.c:write_instr` and `Python/instruction_sequence.c` cache-cell emission. After every adaptive opcode, the assembler emits `_PyOpcode_Caches[op]` zero codeunits so the bytecode layout matches CPython. `instr_size`, dis CACHE-skipping, vm `advance()` / `jumpBy()` all updated. Goldens and the marshal roundtrip test refreshed. Shipped: `compile/opcode_caches.go` is the single source of truth (`CacheCount(op)`); `compile/assemble.go`, `assemble_locations.go`, `dis.go`, `marshal/code.go`, `vm/eval.go` all consult it; v05test goldens (`class_pass`, `def_add_one`, `if_pass`, `while_pass`) refreshed for the wider bytecode. | DONE | 67abc0a |
| P1.1 | Wire `specialize.Enable` into `pythonrun.liftCode`, `vm.liftNestedCode`, and `marshal.unmarshalCode`. Shipped: `pythonrun/runstring.go:122`, `vm/eval_simple.go:52`, `marshal/code.go:239` all call `specialize.Enable(out)`. `Quickened = true` + `CacheObjects []Object` slab (gopy's stand-in for CPython's pointer cache cells; Go can't pack GC pointers in `[]byte`). Full `go test ./...` green. | DONE | 67abc0a |
| P1.2 | Audit `vm/eval.go` for missing specialized-opcode dispatch arms. Coverage achieved via `vm/adaptive.go:maybeDeopt`: every specialized variant rewrites back to its adaptive parent before dispatch, and the parent body runs. The full deopt table in `specialize/deopt.go` enumerates every CPython 3.14 specialized opcode. Correctness complete; per-variant fast paths land under P1.4. | DONE | 67abc0a |
| P1.3 | Wire de-opt. `vm/adaptive.go:53 maybeDeopt` calls `specialize.Deopt` + `specialize.Unspecialize`, and `vm/adaptive.go:72 adaptiveTick` drives the counter and routes triggers into the per-family specializers. No panics, no re-walks. | DONE | 67abc0a |
| P1.4a | Extend specializer emission coverage. CPython 3.14 ships specialized opcode variants across 13 families; gopy's emission state per family is broken out in the P1.4a sub-table below. Faithful port of `classify_descriptor` lives at `specialize/descr_classify.go`. | WIP | 67abc0a |
| P1.4b | VM fast-path arms for each specialized opcode. Framework landed at `vm/eval_specialized.go:trySpecialized`, wired into `vm/dispatch.go` before `maybeDeopt` so hot sites take the fast path first and fall through to deopt on guard miss. Prerequisite: `Code.CacheObjects []Object` parallel slab is gopy's stand-in for CPython's in-cache pointer slots (Go cannot stash GC-tracked pointers in a `[]byte`); `specialize.{Set,}CacheObject` stamp / read by codeunit index, validity gated by the same version cells. Per-family arm state in the P1.4b sub-table below. | WIP | 691c2d7, 71a9181, 6a8aace |
| P1.5 | Bytecode cache persistence: `Code.Quickened` should survive `marshal.dumps`/`marshal.loads` so `.pyc` files retain specialization (CPython persists the warmed cache). | TODO | - |

**P1.4a sub-table — specializer emission per family.** Numbers
report shipped variants vs the CPython 3.14 variant count, then
list the variants still missing. CPython 3.14 reference:
`Python/specialize.c`.

| Family | Coverage | Variants shipped | Missing | Status | Commit |
|--------|----------|------------------|---------|--------|--------|
| LOAD_ATTR | 9/13 | `MODULE`, `CLASS`, `CLASS_WITH_METACLASS_CHECK`, `SLOT`, `INSTANCE_VALUE`, `WITH_HINT`, `PROPERTY`, `METHOD_NO_DICT`, `NONDESCRIPTOR_NO_DICT` | `METHOD_WITH_VALUES`, `NONDESCRIPTOR_WITH_VALUES`, `METHOD_LAZY_DICT`, `GETATTRIBUTE_OVERRIDDEN` — need `Py_TPFLAGS_INLINE_VALUES` / managed-dict-offset / `__getattribute__`-override modelling in `objects/type.go` | WIP | 67abc0a |
| STORE_ATTR | 3/3 | `INSTANCE_VALUE`, `SLOT`, `WITH_HINT` | — | DONE | 67abc0a |
| LOAD_GLOBAL | 2/2 | `MODULE`, `BUILTIN` | — | DONE | 67abc0a |
| COMPARE_OP | 3/3 | `INT`, `FLOAT`, `STR` | — | DONE | 67abc0a |
| CONTAINS_OP | 2/2 | `DICT`, `SET` | — | DONE | 67abc0a |
| FOR_ITER | 4/4 | `LIST`, `TUPLE`, `RANGE`, `GEN` | — | DONE | 67abc0a |
| LOAD_SUPER_ATTR | 2/2 | `ATTR`, `METHOD` | — | DONE | 67abc0a |
| SEND | 1/1 | `GEN` | — | DONE | 67abc0a |
| STORE_SUBSCR | 2/2 | `LIST_INT`, `DICT` | — | DONE | 67abc0a |
| TO_BOOL | 6/6 | `BOOL`, `INT`, `LIST`, `NONE`, `STR`, `ALWAYS_TRUE` | — | DONE | 67abc0a |
| UNPACK_SEQUENCE | 3/3 | `TWO_TUPLE`, `TUPLE`, `LIST` | — | DONE | 67abc0a |
| BINARY_OP | 13/14 | `ADD_INT`, `SUBTRACT_INT`, `MULTIPLY_INT`, `ADD_FLOAT`, `SUBTRACT_FLOAT`, `MULTIPLY_FLOAT`, `ADD_UNICODE`, `INPLACE_ADD_UNICODE`, `SUBSCR_LIST_INT`, `SUBSCR_TUPLE_INT`, `SUBSCR_STR_INT`, `SUBSCR_DICT`, `SUBSCR_LIST_SLICE` | `BINARY_OP_EXTEND` is JIT-only and intentionally skipped | DONE | 67abc0a |
| CALL | 5/16 | `PY_EXACT_ARGS`, `PY_GENERAL`, `BOUND_METHOD_EXACT_ARGS`, `BOUND_METHOD_GENERAL`, `NON_PY_GENERAL` | 8 builtin variants (`CALL_BUILTIN_FAST`, `CALL_BUILTIN_O`, `CALL_METHOD_DESCRIPTOR_*`, `CALL_ISINSTANCE`, `CALL_LEN`, `CALL_LIST_APPEND`, `CALL_ALLOC_AND_ENTER_INIT`) collapse into `CALL_NON_PY_GENERAL` — needs METH_* calling-convention flags on `BuiltinFunction`. `CALL_TYPE_1`, `CALL_STR_1`, `CALL_TUPLE_1` also pending. | WIP | 67abc0a |

**P1.4b sub-table — VM fast-path arms per family.** Each row tracks
the arm count shipped in `vm/eval_specialized*.go` and the parity
gate that backs it.

| Family | Arms shipped | Source | Gate | Status | Commit |
|--------|--------------|--------|------|--------|--------|
| LOAD_ATTR | 7/9 emitted | `vm/eval_specialized.go` — `MODULE`, `SLOT`, `CLASS`, `CLASS_WITH_METACLASS_CHECK`, `METHOD_NO_DICT`, `NONDESCRIPTOR_NO_DICT`, `PROPERTY` | `specialize/gatedata/spec_property.py` (`TestGateSpecPropertyAndMethod`) | WIP — `INSTANCE_VALUE` and `WITH_HINT` need emission to stamp `keys_version` first | 691c2d7, 71a9181 |
| TO_BOOL | 6/6 | `vm/eval_specialized.go` — `BOOL`, `INT`, `LIST`, `NONE`, `STR`, `ALWAYS_TRUE` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| COMPARE_OP | 3/3 | `vm/eval_specialized_compare.go` — `INT`, `FLOAT`, `STR` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| CONTAINS_OP | 2/2 | `vm/eval_specialized.go` — `DICT`, `SET` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| UNPACK_SEQUENCE | 3/3 | `vm/eval_specialized.go` — `TWO_TUPLE`, `TUPLE`, `LIST` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| STORE_SUBSCR | 2/2 | `vm/eval_specialized.go` — `LIST_INT`, `DICT` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| BINARY_OP | 13/13 non-JIT | `vm/eval_specialized_binary_op.go` — `ADD_INT`, `SUBTRACT_INT`, `MULTIPLY_INT` (math/bits overflow guard); `ADD_FLOAT`, `SUBTRACT_FLOAT`, `MULTIPLY_FLOAT`; `ADD_UNICODE` shared with `INPLACE_ADD_UNICODE`; `SUBSCR_LIST_INT`, `SUBSCR_TUPLE_INT`, `SUBSCR_STR_INT` (ASCII fast path), `SUBSCR_DICT`, `SUBSCR_LIST_SLICE` | `specialize/gatedata/spec_binary_op.py` (`TestGateSpecBinaryOp`) | DONE | 6a8aace |
| FOR_ITER | 0/4 | — | — | TODO — needs typed `Next` helpers on `objects.{listIterator,tupleIterator,rangeIterator}` so the arm can skip the `IterNext` slot lookup | - |
| LOAD_GLOBAL | 2/2 | `vm/eval_specialized_load_global.go` — `MODULE`, `BUILTIN` | `specialize/gatedata/spec_load_global.py` (`TestGateSpecLoadGlobal`) | DONE | 2f1f603 |
| STORE_ATTR | 1/3 | `vm/eval_specialized_store_attr.go` — `SLOT` (faithful 1-1 port of CPython's macro: validate type_version, write to cached `Instance.slots[idx]`) | `specialize/gatedata/spec_store_attr.py` (`TestGateSpecStoreAttr`) | WIP — `INSTANCE_VALUE` and `WITH_HINT` deliberately deferred; they need a `Dict.SetValueAt(slot, value)` primitive that writes the entry's value cell without re-hashing the key, plus the managed-dict-offset modelling listed in P1.4a. Shipping them before that lands forces a shim that re-runs `SetItem(name, value)`, which is exactly the ad-hoc patch the ground rule forbids. | 2f1f603 |
| SEND | 0/1 | — | — | TODO — depends on generator-frame plumbing | - |
| LOAD_SUPER_ATTR | 0/2 | — | — | TODO | - |
| CALL | 0/5 emitted | — | — | TODO — gated on closing P1.4a CALL gap first | - |

**Gate.**

- `specialize/integration_test.go` — run `richards.py` 3 times under
  a harness that asserts the specialized opcodes outnumber generic
  by 10:1 after warmup.
- Small-subset bench: `call_method`, `richards`, `regex_compile`
  drop to <200x cpython (from 1899x-2407x).

**Estimated win.** 6-10x geomean improvement. Single biggest lever.

### P2. Tier-2 micro-op interpreter — `Python/executor_cases.c.h`

**Audit.** Already in tree at ~23k LOC under `optimizer/`:

| File                    | LOC   | Role                                       |
|-------------------------|------:|--------------------------------------------|
| `uop_ids_gen.go`        |   ?   | uop opcode enum (generated)                |
| `uop_meta_gen.go`       |   ?   | uop metadata (stack effect, name)          |
| `uops_dispatch_gen.go`  |   589 | dispatch switch                            |
| `uops_stubs_gen.go`     |  8260 | per-uop stub bodies (generated)            |
| `uops_impl.go`          |   171 | hand-written hot uops                      |
| `uops.go`               |   129 | executor entry + trampoline                |
| `executor.go`           |   ?   | tier-2 entry point                         |
| `trace.go`              |   ?   | trace projection from tier-1               |
| `analysis.go`           |   ?   | type/escape analysis pre-pass              |
| `optimize.go`           |   ?   | tier-2 IR optimization                     |
| `dis_hook.go`           |   ?   | dis integration                            |
| `watcher.go`            |   320 | type / dict version watcher                |
| `bloom.go`              |   ?   | bloom filter for redundancy elimination    |

Stubs are generated for all ~285 uops. The hand-ported set
(`uops_impl.go`) covers ~14 of the hot ones per the v0.12 ship note.

**Gap.** The tier-2 executor only runs when a trace projection
succeeds and the trace executor is enabled. Two pieces are missing:

- The threshold for trace projection is not tuned. CPython promotes
  to tier-2 after ~16 specialized hits on the same code object;
  gopy's `optimizer/executor.go` threshold is unverified.
- ~270 of the 285 stubs are still no-op pass-through to tier-1 (they
  exist as generated stubs to keep dispatch dense, but fall back).
  The hand-ported 14 do not include `_BINARY_OP_ADD_INT`,
  `_GUARD_BOTH_INT`, `_LOAD_ATTR_INSTANCE_VALUE`, which are exactly
  the uops `richards` would generate.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P2.1 | Confirm trace projection fires. Add `optimizer/trace_test.go` that runs `call_method.py` under the executor and asserts ≥1 trace was projected. Fix the trigger in `optimizer/executor.go` if zero. | TODO | - |
| P2.2 | Hand-port the top 30 hottest uops: `_LOAD_FAST`, `_STORE_FAST`, `_LOAD_CONST`, `_BINARY_OP_ADD/SUBTRACT/MULTIPLY_INT`, `_GUARD_BOTH_INT`, `_LOAD_ATTR_INSTANCE_VALUE`, `_STORE_ATTR_INSTANCE_VALUE`, `_CALL_PY_EXACT_ARGS`, `_PUSH/POP_FRAME`, `_FOR_ITER_TIER_TWO`, `_GUARD_TYPE_VERSION`, `_GUARD_NOT_EXHAUSTED_RANGE`, `_BINARY_OP_ADD/MULTIPLY_FLOAT`, `_LOAD_ATTR_METHOD_WITH_VALUES`, `_RESUME_CHECK`, `_CHECK_VALIDITY`, `_INIT_CALL_PY_EXACT_ARGS`, `_SAVE_RETURN_OFFSET`, `_CHECK_STACK_SPACE`, `_TO_BOOL_INT`, `_COMPARE_OP_INT`, `_BINARY/STORE_SUBSCR_LIST_INT`, `_BUILD_TUPLE`, `_UNPACK_SEQUENCE_TUPLE/TWO_TUPLE`. | TODO | - |
| P2.3 | Wire the tier-2 → tier-1 deopt path: on guard fail mid-trace, fall back to the adaptive opcode at the recorded resume offset. | TODO | - |
| P2.4 | Hand-port the next 30 uops (cpython's long-tail profile). | TODO | - |
| P2.5 | Turn on the tier-2 executor by default for any function that has been Quickened (P1.1). | TODO | - |

**Gate.**

- `optimizer/uops_test.go` covers each hand-ported uop with one
  positive case and one guard-fail case.
- New `optimizer/bench_test.go::BenchmarkTier2Nbody` shows the tier-2
  path is ≥2x faster than tier-1 on the warm loop.

**Estimated win.** 1.5-2x on top of P1.

### P3. PyLong fast path — `Objects/longobject.c`

**Audit.** In tree:

- `objects/long_cache.go` — small-int cache `[-5, 256]`, initialised
  from `objects/int.go`'s `init()`.
- `objects/long_arith.go` — `Add`, `Sub`, `Mul`, etc.
- `objects/long_bitwise.go` — bit ops
- `objects/long_misc.go` — divmod, pow, gcd helpers
- `objects/long_parse.go` — int(str, base) parser

Every arithmetic op currently allocates a fresh `*Int` and routes
through `math/big.Int`, even when both sides are in `[-5, 256]` or
fit in a single machine word.

**Gap.**

- No compact representation: gopy `Int` is always a heap-allocated
  `big.Int`. CPython packs |n| < 2^30 inline in the PyLong header.
- Small-int cache is populated but `New(int64)` doesn't consult it;
  every `Int{}` construction skips the cache.
- `Add`/`Sub`/`Mul` don't short-circuit on the
  "both operands fit in int64" case. Every op pays a `big.Int.Add`
  plus an allocation.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P3.1 | `objects/long_fast.go`: detect inline-representable values, store unboxed int64 alongside `big.Int`. Add `compactValue int64; isCompact bool` (or single int64 with sentinel bit). | TODO | - |
| P3.2 | Route `New(int64)` and `FromString` through `long_cache.go` for `[-5, 256]`. Allocation-free. | TODO | - |
| P3.3 | `Add`/`Sub`/`Mul`/`Neg`/`Abs` fast-path: int64 arithmetic with overflow check when both compact; fall back to big.Int on overflow. | TODO | - |
| P3.4 | `__index__` / `PyLong_AsLong` fast path. | TODO | - |
| P3.5 | `_PyLong_FromUint64` / `_PyLong_FromInt64` mirrored constructors that bypass big.Int when input fits compact. | TODO | - |

**Gate.**

- `objects/long_arith_test.go` adds a cross-check: every fast-path
  result equals the big.Int slow-path result on a 10k-entry random
  table.
- `BenchmarkLongAddSmall`/`BenchmarkLongMulSmall` show 0 allocs and
  ≥5x speedup vs the current path.
- `pidigits` bench drops from 7.83x to under 2x cpython.

**Estimated win.** 3x on integer-heavy benchmarks (pidigits, pyflate,
go, hexiom). Geomean impact ~1.4x.

### P4. PyUnicode kind tags — `Objects/unicodeobject.c`

**Audit.** `objects/unicode*.go` uses Go's UTF-8 `string` as backing
storage, plus `unicode_ctype.go` for category lookups. Indexing,
slicing, find/count/replace all walk bytes.

**Gap.**

- No kind tag (Latin-1/BMP/full Unicode).
- Indexing is O(n) for any non-ASCII string. `find`, `count`,
  `replace` likewise walk by rune.
- `str.encode`/`bytes.decode` round-trips through the rune iterator.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P4.1 | `objects/unicode_kind.go`: detect kind at construction. Latin-1: byte-equal to ASCII; BMP: re-encode to `[]uint16`; Full: `[]rune`. | TODO | - |
| P4.2 | Kind-dispatched `__getitem__`, `__len__`, slicing. Latin-1 hits a byte-index path (allocation-free for single chars via small-string cache). | TODO | - |
| P4.3 | Kind-dispatched `find`, `rfind`, `count`, `index`, `replace`, `split`. Latin-1 → `bytes.IndexByte` / `bytes.Count` (memchr speed). | TODO | - |
| P4.4 | `_PyUnicodeWriter` port (lands with P15). | TODO | - |
| P4.5 | Small-string cache: `__getitem__` returning a one-char str is allocation-free for ASCII. | TODO | - |

**Gate.**

- `objects/unicode_kind_test.go` covers indexing/slicing/find/count
  for all three kinds against the cpython-reference behavior.
- `BenchmarkStrFindAscii` shows kind-1 strings hit the byte-find
  fast path (alloc count = 0).
- `regex_compile` ratio compresses (P1 is primary; P4 is secondary).

**Estimated win.** 2x on string-heavy benchmarks (regex_compile,
html5lib, mako, django_template).

### P5. Dict open-addressing + split keys — `Objects/dictobject.c`

**Audit.** `objects/dict.go` plus a suite of supporting files:

| File                       | Role                                       |
|----------------------------|--------------------------------------------|
| `dict.go`                  | combined dict (Go `map[any]any` + order slice) |
| `dict_split.go`            | shared-keys layout placeholder             |
| `dict_lookup.go`           | lookup dispatch                            |
| `dict_iter.go`             | iteration order                            |
| `dict_mutate.go`           | insert/delete/resize                       |
| `dict_specialize.go`       | specializer hooks                          |

`dict_split.go` is honest about the gap: "ports the surface but
not yet the storage savings. NewSplitDict returns a regular combined
Dict pre-populated with the shared key names mapped to None".

**Gap.**

- Combined-dict layout is still `map[any]any` + order slice. Two
  allocations per insert, hash + interface boxing per lookup.
- Split-keys is surface only. Instance `__dict__` should share keys
  with the type and store only values inline — that win is currently
  zero.
- No compact entries array (CPython's `PyDictKeyEntry`).

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P5.1 | `objects/dict_oa.go`: open-addressing table with `dk_indices` (i1/i2/i4/i8) + `dk_entries` (hash/key/value). Probe mirrors CPython `lookdict` (`dictobject.c:973`). | TODO | - |
| P5.2 | Switch combined-dict storage from `map[any]any` to the new layout. Maintain insertion order via entries-array order. | TODO | - |
| P5.3 | Real split-keys: shared `PyDictKeysObject` per type; instance carries only `values []Object`. Materialise to combined on delete / non-shared insert. | TODO | - |
| P5.4 | `_PyDict_SetItem_KnownHash` fast path; LOAD_ATTR with cached hash bypasses `key.__hash__`. | TODO | - |
| P5.5 | Keys-watcher invalidation hook for specializer cache coherency (interacts with P1). | TODO | - |

**Gate.**

- `objects/dict_oa_test.go` cross-checks every op against the
  current `Dict` reference on a randomized workload.
- `BenchmarkDictLookup` shows 0 allocations on the hot path.
- `meteor_contest` / `go` benches drop primarily on P5.

**Estimated win.** 2x on attribute- and call-method-heavy code.

### P6. Frame free-list + LOAD_FAST_CHECK — `Objects/frameobject.c`, `Python/ceval.c`

**Audit.** `objects/frame.go`, `objects/frame_locals.go`,
`objects/frame_snapshot.go` — frame + locals representation.
`vm/eval.go` allocates a fresh frame per call.

**Gap.**

- No frame free-list. Every function call allocates `*Frame` + a
  fresh `[]Object` for locals + a fresh stack slice.
- No `LOAD_FAST_CHECK` opcode — the unbound-local check the
  specializer relies on for `LOAD_FAST_BORROW`.
- `vm/eval_call.go` rebuilds the args tuple per call even for
  CALL_PY_EXACT_ARGS.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P6.1 | `vm/frame_pool.go`: per-goroutine free list, capped at 20. Recycle frame + locals + stack slices; reset, not free. | TODO | - |
| P6.2 | `LOAD_FAST_CHECK` codegen in `compile/codegen.go` + eval arm. | TODO | - |
| P6.3 | `LOAD_FAST_BORROW` / `STORE_FAST_STORE_FAST` (CPython 3.14 new opcodes that elide the incref pair). | TODO | - |
| P6.4 | Args-tuple bypass: `CALL_PY_EXACT_ARGS` stores args directly into the callee's frame locals. | TODO | - |

**Gate.**

- `vm/frame_pool_test.go` proves recycle works under load.
- `BenchmarkCallNop` shows 0 allocations on the hot path.

**Estimated win.** 1.5x on call-heavy code (richards, deltablue).

### P7. Type slot caching — `Objects/typeobject.c`

**Audit.** `objects/type.go`, `type_call.go`, `type_attr.go`,
`type_getsets.go`, `type_repr.go`, `type_specialize.go`,
`usertype.go`. The MRO walk lives in `type_attr.go`'s `Lookup`.
`type_specialize.go` is the hook the specializer calls.

**Gap.**

- `Lookup(__add__)`, `Lookup(__len__)`, `Lookup(__iter__)` etc. walk
  the MRO on every dispatch. CPython resolves these once at
  type-creation time and stores function pointers in the type's
  slot table (~40 slots).
- No version tag on types, so the specializer can't cache a slot
  pointer across hits.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P7.1 | `objects/type_slots.go`: full slot-table struct mirroring CPython `PyTypeObject` (nb_add, sq_length, mp_subscript, tp_call, tp_iter, ...). | TODO | - |
| P7.2 | `_PyType_AssignSpecialMethods`: walk the MRO once at type creation, populate the slot table. | TODO | - |
| P7.3 | Type version tag (monotonic uint32 bumped on MRO mutation, class `__setattr__`, `__class__` reassignment). | TODO | - |
| P7.4 | Operator dispatch (`abstract_binop.go`, `abstract_sequence.go`) consults the slot table first; falls back to `Lookup` only if slot nil. | TODO | - |
| P7.5 | Invalidation hook: type-version change auto-stales every inline cache keyed on that version (interacts with P1). | TODO | - |

**Gate.**

- All existing operator tests stay green.
- `objects/slots_test.go`: slot table populated correctly for a
  hand-rolled type; invalidates on mutation.
- `richards` ratio compresses by another ~2x on top of P1.

**Estimated win.** 1.5x on operator-heavy code (richards, deltablue,
typing_runtime_protocols).

### P8. Augmented STORE_SUBSCR codegen — `Python/compile.c`

**Symptom.** `target[idx] -= rhs` raises `TypeError: 'int' object
does not support item assignment` whenever `target` is bound through
a nested unpack in a for-loop. Confirmed reproducer:

```python
pairs = [(([1,2,3], [4,5,6], 7), ([10,20,30], [40,50,60], 70))]
for ((p1, v1, m1), (p2, v2, m2)) in pairs:
    v1[0] -= 100   # raises, even though v1 is correctly a list
```

`v1[0] = 99` works on the same binding; `v1[0] -= 100` does not.

**Gap.** gopy's compiler lowers `v[0] -= rhs` into an opcode sequence
that misroutes STORE_SUBSCR's container target after `BINARY_OP`. The
SET_ITEM dispatches against the loaded value (an int) instead of the
list. cpython's correct sequence is:

```
LOAD_FAST v
LOAD_CONST 0          ; index
COPY 2                ; dup container
COPY 2                ; dup index
BINARY_SUBSCR         ; loads v[0]
LOAD_CONST 100
BINARY_OP -=
SWAP 3                ; restore stack: ..., new_val, container, index
STORE_SUBSCR
```

gopy is likely missing the `COPY 2` / `SWAP 3` pair, so the second
operand on STORE_SUBSCR's stack-effect slot is the loaded int, not
the saved container.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P8.1 | Capture gopy `dis` output for the reproducer; diff against cpython 3.14. Land the diff in `compile/augassign_test.go::TestStoreSubscrSequence`. | TODO | - |
| P8.2 | Fix the lowering in `compile/codegen.go` (Subscript LHS in augmented context). | TODO | - |
| P8.3 | Extend the test matrix: augmented STORE_SUBSCR with all bound-context flavors (nested unpack, dict.get returns, comprehension target). | TODO | - |
| P8.4 | Same audit for augmented `STORE_ATTR` (`obj.attr -= rhs`). | TODO | - |

**Gate.** `nbody`, `fannkuch` run to completion under `bin/gopy`;
both show up with real numbers in the small-subset table.

**Estimated win.** Unblocks 2 N/A benches.

### P9. `int.__format__` format-spec parser — `Python/formatter_unicode.c`

**Symptom.** `'{0:04x}'.format(255)` raises `TypeError: unsupported
format string passed to int.__format__`. `stdlib/json/encoder.py:31`
(`'\\u{0:04x}'.format(i)` in `ESCAPE_DCT` initialisation) hits this on
`import json`, blocking `json_dumps`.

**Gap.** gopy's int formatter parses bare type codes (`x`, `o`, `b`,
`d`) only. It rejects any prefix carrying fill/align/sign/alt/width/
grouping/precision.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P9.1 | `objects/long_format.go`: port `Python/formatter_unicode.c:parse_internal_render_format_spec` into an `InternalFormatSpec` struct (fill, align, sign, alt, width, grouping, precision, type). | TODO | - |
| P9.2 | Wire `int.__format__` to the parsed spec; route through the existing decimal/hex/octal/binary renderers, applying padding + alignment + sign + grouping. | TODO | - |
| P9.3 | Float-spec coercion: `'{:.2g}'.format(255)` promotes the int to float and dispatches to `float.__format__`. Mirror cpython. | TODO | - |
| P9.4 | Table-driven test pulled from CPython `Lib/test/test_format.py`. | TODO | - |

**Gate.** `objects/long_format_test.go` matches cpython output on
every spec from `test_format.py`. `json_dumps` runs to completion
under `bin/gopy`.

**Estimated win.** Unblocks 1 N/A bench plus removes a class of
silent-format failures hiding in other stdlib paths.

### P10. Float fast path — `Objects/floatobject.c`

**Audit.** `objects/float.go`, `objects/float_parse.go`. Stored as
boxed `*Float` wrapping a Go `float64`. Every `Float{v: x}` is a
heap allocation.

**Gap.**

- No free list / small-float cache.
- `_BINARY_OP_ADD_FLOAT` is in the specializer's vocabulary but the
  eval arm allocates a fresh `*Float` per op. CPython has the same
  per-op cost but its tier-2 executor can elide it; gopy's tier-2
  executor doesn't see floats yet.
- `float.__format__` may share P9's spec-parser gap; audit before
  P9 ships.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P10.1 | `objects/float_pool.go`: per-goroutine free list for `*Float`. Lookback list of N=128 recently-freed Float pointers. Reset, don't re-allocate. | TODO | - |
| P10.2 | `BINARY_OP_ADD_FLOAT` / `SUBTRACT_FLOAT` / `MULTIPLY_FLOAT` / `TRUE_DIVIDE_FLOAT` fast path: if the LHS is a temporary (refcount=1, recycled from the pool), mutate in place. | TODO | - |
| P10.3 | `_BINARY_OP_*_FLOAT` tier-2 uops hand-ported (depends on P2.2). | TODO | - |
| P10.4 | `float.__format__` audit + spec-parser share with P9. | TODO | - |

**Gate.** `BenchmarkFloatAddHot` shows allocation-free path. `nbody`
ratio compresses (P8 must land first).

**Estimated win.** 2.5x on float-heavy benchmarks (nbody, raytrace,
spectral_norm, scimark_*). Geomean ~1.3x.

### P11. Compiler CFG optimizer + peephole — `Python/flowgraph.c`, `Python/compile.c`

**Audit.** `compile/`, `optimizer/analysis.go`. The compiler does
constant-folding for arithmetic on integer literals but no CFG
simplification beyond that.

**Gap.**

- CPython 3.14 ships `Python/flowgraph.c` with: jump threading,
  dead code elimination after `raise`/`return`, redundant LOAD_CONST
  folding, peephole rewrites (LOAD_FAST + RETURN_VALUE →
  RETURN_FAST), unreachable-block pruning.
- Without these, every gopy-compiled function is ~10-15% larger and
  has correspondingly more dispatch overhead.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P11.1 | `compile/flowgraph.go`: basic-block graph construction. Cite `Python/flowgraph.c:_PyCfg_FromInstructionSequence`. | TODO | - |
| P11.2 | Port the four big passes: jump threading, eliminate-after-terminator, fold-constant-jumps, prune-unreachable. | TODO | - |
| P11.3 | Port the peephole table from `Python/flowgraph.c:optimize_basic_block`. | TODO | - |
| P11.4 | `dis.dis` integration: the optimizer pass must run before `dis` so what users see matches what runs. | TODO | - |

**Gate.** `compile/flowgraph_test.go` table-driven against cpython
`Lib/test/test_peepholer.py` cases. Every cpython test passes.

**Estimated win.** 1.1-1.15x geomean (small but uniform).

### P12. Generator + coroutine fast path — `Python/genobject.c`

**Audit.** `objects/generator.go`, `objects/async_gen.go`,
`vm/eval_gen.go`, `vm/eval_resume.go`. Generators work
correctly today; the cost is per-`send` overhead.

**Gap.**

- Each `gen.send(v)` saves and restores the full frame via
  `frame_snapshot.go`. CPython steals the frame pointer + revives
  it without copy.
- `GET_AITER` / `GET_ANEXT` / async-gen-wrapped opcodes go through
  the same heavy path.
- Async benches (`async_tree_*`) are blocked first on `asyncio`
  module port (spec 1711); but once that lands, generator dispatch
  cost dominates.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P12.1 | Frame stealing: `Generator` owns its `Frame` directly, no copy on `send`. Cite `Python/genobject.c:gen_send_ex2`. | TODO | - |
| P12.2 | `SEND` opcode tier-2 uop (P2.2 list). | TODO | - |
| P12.3 | `GET_AITER` / `GET_ANEXT` / `END_ASYNC_FOR` fast path for the common case (async iterator implemented as plain generator with `__aiter__ = lambda self: self`). | TODO | - |
| P12.4 | Coroutine suspend/resume via `goroutine + channel` swap (Go runtime advantage over CPython's per-platform asm). | TODO | - |

**Gate.** `objects/generator_test.go` adds a benchmark
`BenchmarkGenSendHot` showing ≤2 allocations per send. `generators`
bench drops to under 5x cpython.

**Estimated win.** Blocked on asyncio port; once unblocked, ~3x on
async benches.

### P13. GC tracking + generational collector — `Python/gc.c`

**Audit.** Go has a GC; gopy relies on it. There is no Python-level
generational collector and no `gc.collect()` semantics beyond
"yield to the runtime".

**Gap.**

- `gc_collect` bench measures cycle-collection wall time. gopy gives
  the Go GC's wall time, which is correct but not what the
  benchmark expects. CPython's gen-0/gen-1/gen-2 threshold logic is
  not reproduced.
- `gc.get_count()`, `gc.get_threshold()`, `gc.set_threshold()` return
  fixed defaults; the bench tweaks them, gopy ignores it.
- `__del__` order is best-effort under Go GC, not deterministic.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P13.1 | `module/gc/`: real implementation of `gc.collect()`, `gc.get_count`, `gc.get_threshold`, `gc.set_threshold`. Drive a Python-level generation counter (objects-since-last-collect) that triggers `runtime.GC()` at the gen-0 threshold, then bumps gen-1 etc. | TODO | - |
| P13.2 | Python-level finalizer queue: order `__del__` calls by gc-generation, not by Go GC traversal order. | TODO | - |
| P13.3 | Cycle detection for `__del__` resurrected objects. | TODO | - |

**Gate.** `module/gc/gc_test.go` mirrors cpython
`Lib/test/test_gc.py`. The `gc_collect` bench returns plausible
numbers (within 10x cpython; we can't beat Go's GC).

**Estimated win.** Low geomean impact (gc_collect alone). Mostly
unblocks the cpython test suite gc tests.

### P14. Native C-extension paths — `_pickle`, `_elementtree`, `_sqlite3`

**Audit.** `module/_pickle/`, `module/xml/etree/`, `module/_sqlite3/`
status varies; some are pure-Python falls-back, some are stubs.

**Gap.**

- `pickle` / `unpickle` benches run on the pure-Python fallback,
  which is ~20x slower than the C `_pickle` module CPython uses by
  default. gopy needs Go-native equivalents.
- `xml_etree_*` benches similarly fall back to `xml.etree.ElementTree`
  pure-Python; cpython uses `_elementtree` (C).
- `sqlite_synth` requires `_sqlite3` bindings.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P14.1 | `module/_pickle/`: Go-native pickle protocol 5 encoder + decoder. Cite `Modules/_pickle.c`. | TODO | - |
| P14.2 | `module/_elementtree/`: thin wrapper over `encoding/xml` with the cpython `_elementtree` API surface. | TODO | - |
| P14.3 | `module/_sqlite3/`: cgo binding to libsqlite3 (or pure Go via `modernc.org/sqlite` if cgo is undesirable). | TODO | - |
| P14.4 | `module/_csv/`: Go-native csv reader/writer. | TODO | - |

**Gate.** `pickle` / `unpickle` benches drop to under 3x cpython.
`xml_etree_*` benches drop to under 5x.

**Estimated win.** Targeted; only the named benches.

### P15. Unicode writer + string concat — `Objects/unicodeobject.c`

**Audit.** gopy concatenates strings via `string + string` which
allocates per op (immutable Go strings). Format/join paths build
intermediate strings.

**Gap.**

- No `_PyUnicodeWriter` equivalent. CPython's writer pre-sizes a
  buffer, writes append-style, finalises once. `json_dumps`,
  `logging`, `mako`, `django_template` all hit this.
- `str.join` allocates the join separator slice per call.
- `%` formatting and `str.format` go through immutable concat.
- f-string codegen produces `FORMAT_VALUE` + `BUILD_STRING` which
  does N concats for an N-piece f-string.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P15.1 | `objects/unicode_writer.go`: pre-sized `strings.Builder`-backed writer with kind-aware finalisation (matches P4). API: `WriteStr`, `WriteASCII`, `WriteRune`, `Finish() *Unicode`. | TODO | - |
| P15.2 | Re-route `str.join`, `str.format`, `%` formatting through the writer. | TODO | - |
| P15.3 | `BUILD_STRING` opcode lowering: emit a single writer.Finish call instead of N concats. | TODO | - |
| P15.4 | f-string codegen: in `compile/codegen.go`, lower an f-string's pieces directly into writer calls. | TODO | - |

**Gate.** `BenchmarkStrFormatHot` allocation-free for static format
strings. `json_dumps`, `logging`, `pprint` benches drop materially.

**Estimated win.** 2x on text-heavy benchmarks. Geomean ~1.2x.

## Checklist

| Subsystem                       | CPython source         | gopy destination          | Estimated win | Status | Commit |
|---------------------------------|------------------------|---------------------------|--------------:|--------|--------|
| P0. pyperformance harness       | n/a (tooling)          | `bench/`                  | n/a           | WIP    | -      |
| P1. Specializer wire-up         | `Python/specialize.c`  | `specialize/` (in tree); wire in `pythonrun/`, `imp/` | 6-10x | TODO | - |
| P2. Tier-2 uop hot bodies       | `Python/executor_cases.c.h` | `optimizer/uops_impl.go` | 1.5-2x  | WIP   | -      |
| P3. PyLong fast path            | `Objects/longobject.c` | `objects/long_fast.go`    | 3x            | TODO   | -      |
| P4. PyUnicode kind tags         | `Objects/unicodeobject.c` | `objects/unicode_kind.go` | 2x         | TODO   | -      |
| P5. Dict open-addressing        | `Objects/dictobject.c` | `objects/dict_oa.go`      | 2x            | TODO   | -      |
| P6. Frame free-list             | `Objects/frameobject.c` | `vm/frame_pool.go`        | 1.5x          | TODO   | -      |
| P7. Type slot cache             | `Objects/typeobject.c` | `objects/type_slots.go`   | 1.5x          | TODO   | -      |
| P8. Aug-STORE_SUBSCR fix        | `Python/compile.c`     | `compile/codegen.go`      | unblock 2 N/A | TODO   | -      |
| P9. int.__format__ spec         | `Python/formatter_unicode.c` | `objects/long_format.go` | unblock 1 N/A | TODO | - |
| P10. Float fast path            | `Objects/floatobject.c` | `objects/float_pool.go`  | 2.5x          | TODO   | -      |
| P11. CFG optimizer + peephole   | `Python/flowgraph.c`   | `compile/flowgraph.go`    | 1.1x          | TODO   | -      |
| P12. Generator fast path        | `Python/genobject.c`   | `objects/generator.go` (extend) | 3x async | TODO  | -      |
| P13. GC tracking                | `Python/gc.c`          | `module/gc/`              | low geomean   | TODO   | -      |
| P14. Native pickle/xml/sqlite   | `Modules/_pickle.c`, etc | `module/_pickle/`, etc  | bench-specific | TODO  | -      |
| P15. Unicode writer             | `Objects/unicodeobject.c` | `objects/unicode_writer.go` | 2x text  | TODO   | -      |

## Recommended ship order

P0 (CI gate) → **P1 wire-up + P8 fix + P9 fix** ship as one PR;
they're independent but together remove the three N/A cells and the
biggest geomean blocker. Then:

- P3, P5, P7, P10 ship in parallel (independent objects/ work).
- P2 follows P1 (needs Quickened code to project traces from).
- P6 follows P7 (frame pool wants the version-tagged slot table).
- P4 + P15 ship together (kind tags + writer share the resize path).
- P11 lands last among core ports (cheap geomean win, no
  dependencies).
- P12, P13, P14 are bench-specific and can ship out-of-order.

## Current benchmark results

_Captured: 2026-05-16. First end-to-end P0 small-subset run with
warmed-up PyPy. Each P1-P15 PR refreshes the gopy column._

Host:

- CPU: Apple M4
- macOS: 15.7.7
- Go: 1.26.3 (darwin/arm64)
- cpython: 3.14.5 (brew)
- PyPy: 3.11.15 v7.3.22 (`$HOME/pypy3.11/`)
- gopy: `v0.12.0-425-gea07e20` (branch `feat/v0.12.4-lexer-tokenizer`)

Method:

- Each interpreter runs the same standalone `.py` files under
  `bench/bench_sources/` via `bench/run_one.sh`.
- Iteration counts tuned so cpython is in the ~30-300 ms range, so
  PyPy gets a JIT warmup window. The earlier draft of this table
  (trimmed iteration counts) showed PyPy ~ cpython, which was the
  JIT-compile-time artifact, not steady state.
- cpython + PyPy: 2 warmup runs + 3 timed runs per bench.
- gopy: 1 warmup + 2 timed runs (it is ~283x slower today; full
  3+2 pushes wall time past 15 min on the slow benches).

### Small subset (the day-to-day gate)

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy | PyPy / cpython |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|---------------:|
| `call_method`     |             32.42 |          20.50 |  78043.22 |       2407.02x |    3806.80x |          0.63x |
| `fannkuch`        |            292.52 |          82.56 |       N/A |            N/A |         N/A |          0.28x |
| `json_dumps`      |             97.35 |         128.47 |       N/A |            N/A |         N/A |          1.32x |
| `nbody`           |             57.87 |          23.90 |       N/A |            N/A |         N/A |          0.41x |
| `pidigits`        |             37.05 |          33.34 |    289.97 |          7.83x |       8.70x |          0.90x |
| `regex_compile`   |             41.14 |         140.11 |  80286.50 |       1951.54x |     573.03x |          3.41x |
| `richards`        |             42.79 |          29.30 |  81250.57 |       1898.87x |    2772.59x |          0.68x |
| `unpack_sequence` |             24.43 |          20.65 |   6204.49 |        253.94x |     300.53x |          0.84x |
| **geomean**       |             55.11 |          44.24 |  15573.05 |        282.56x |     351.98x |          0.80x |

PyPy is ~1.25x faster than cpython on geomean (5/8 benches faster,
3/8 slower) — that matches the published PyPy 7.3 numbers and
confirms the JIT is doing its job.

gopy is at **283x cpython on geomean** across the five benches that
complete. That ratio compresses dramatically with P1 (specializer
wire-up) alone — without P1 every adaptive opcode short-circuits
in `vm/adaptive.go:41/54/73`.

### Full corpus (release-tag and nightly only)

_Populated when `bench/run_full.sh` lands its first end-to-end run.
Until then, only the small subset above is the ship gate._

Caveats:

- P8 and P9 are prerequisites for a complete table. The "N/A" cells
  become real numbers once those land.
- The 5 ok benches above gate the P1-P7 ports: each PR must shrink
  the `gopy / cpython` column or document why a regression is
  acceptable.
- The `call_method` ratio widened from earlier preliminary runs
  (487x → 2407x) when iteration counts increased. That is cpython's
  specializer kicking in on the warm loop while gopy stays at the
  generic dispatch path. After P1 ships, this ratio should compress
  by an order of magnitude.

## Sources of truth

| CPython file                       | Lines | What it gives us                       |
|------------------------------------|------:|----------------------------------------|
| `Python/specialize.c`              | 3500  | Specializer (mostly already ported)    |
| `Python/executor_cases.c.h`        | 4200  | The 285 tier-2 uop bodies              |
| `Python/optimizer.c`               | 2000  | Trace projection + tier-2 entry        |
| `Python/flowgraph.c`               | 3000  | CFG optimizer + peephole               |
| `Python/compile.c`                 | 7000  | Codegen incl. aug-assign lowering      |
| `Python/genobject.c`               | 1500  | Generator + coroutine machinery        |
| `Python/gc.c`                      | 3000  | Generational GC                        |
| `Python/formatter_unicode.c`       | 1600  | Format-spec grammar                    |
| `Objects/longobject.c`             | 6400  | Compact small-int + fast-path arith    |
| `Objects/floatobject.c`            | 2000  | Float + free list                      |
| `Objects/unicodeobject.c`          | 16000 | Kind-tagged strings + writer           |
| `Objects/dictobject.c`             | 4800  | Open-addressing + split keys           |
| `Objects/frameobject.c`            | 1100  | Frame free-list                        |
| `Objects/typeobject.c`             | 11000 | Slot caching                           |
| `Include/internal/pycore_code.h`   | 600   | Inline cache layouts                   |
| `Modules/_pickle.c`                | 8500  | Native pickle                          |
| `Modules/_elementtree.c`           | 4000  | Native XML                             |
| `Modules/_sqlite/`                 | 6000  | sqlite3 bindings                       |

## Risk + scope notes

- **P1 wire-up is the single highest-leverage change.** The
  specializer is already written and tested; flipping the Quickened
  flag in `pythonrun/`/`imp/` should be a one-day change with
  6-10x geomean impact.
- P3 / P5 / P7 / P10 can ship in any order; pick by who has bandwidth.
- The 5x-faster-than-CPython aspirational target only holds on tight
  loops where Go's escape analysis stack-allocates frame locals and
  the specializer has already promoted to the type-specialized op.
  Geomean parity (1.5x) is the realistic ship gate.
- P13 + P14 are bench-specific. They don't move the geomean much but
  unblock named benchmarks that are part of the full corpus.
- The PyPy column is a sanity check, not a target. gopy's parity
  goal is against cpython; beating PyPy on specific shapes (e.g.
  `regex_compile`, where PyPy's JIT loses to cpython's C re) is a
  bonus, not a requirement.
