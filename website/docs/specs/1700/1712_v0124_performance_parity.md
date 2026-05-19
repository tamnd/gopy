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

**2026-05-19 reality-check audit update.** Five parallel CPython
3.14-vs-gopy audits (P1, P2, P3/P5/P7, P6/P8/P9/P10/P11,
P4/P12/P13/P14/P15) corrected several claims in the original
draft of this spec. Highlights:

- **P1 (specializer) is no longer the smoking gun.** Cache-cell
  emission + `specialize.Enable` wiring + deopt + adaptive tick
  all landed in commit 67abc0a. The remaining P1 work is closing
  the per-family emission/dispatch tables (LOAD_ATTR
  `WITH_HINT`/`METHOD_WITH_VALUES`, STORE_ATTR
  `INSTANCE_VALUE`/`WITH_HINT`, CALL `BUILTIN_*`, FOR_ITER, SEND,
  LOAD_SUPER_ATTR), plus persisting `Code.Quickened` through
  marshal.
- **P2 (tier-2) is gated off, not partially built.** The
  projection/analysis/executor scaffolding is mostly ported
  (~13.5k LOC under `optimizer/`, not the ~23k earlier estimate),
  but `interp.JIT` is hardcoded `false`, so no executor ever
  runs. Of 14 hand-ported uops, only 3 (`_LOAD_FAST`,
  `_STORE_FAST`, `_CHECK_VALIDITY`) are actual hot-path targets;
  the remaining 11 are scaffolding (`_NOP`, `_EXIT_TRACE`,
  `_JUMP_TO_TOP`, etc.). `Python/optimizer_bytecodes.c` (1107
  LOC) is entirely unported, so `optimize_uops()` is stubbed.
- **P5 (dict) is misdiagnosed.** `objects/dict.go` is **already**
  an open-addressed table (`entries []dictEntry`, `order []int`),
  not `map[any]any` + order slice as the draft claimed. The real
  gaps are: split-keys saves zero memory, no `PyDict_Watch`
  subscription API, no `_PyDict_SetItem_KnownHash` skip-rehash
  path.
- **P6.2 (LOAD_FAST_CHECK) is DONE.** Shipped via spec 1716
  (`compile/flowgraph_cfg_locals.go:320-358` rewrites
  `LOAD_FAST → LOAD_FAST_CHECK`;
  `vm/eval_dispatch_handwritten.go:63-72` dispatches). Frame pool,
  `LOAD_FAST_BORROW`, `STORE_FAST_STORE_FAST`, args-tuple bypass
  remain.
- **P11 (CFG optimizer + peephole) is FULLY CLOSED.** Shipped via
  spec 1716 (commits 9d7d9f0 + 37563f5). Jump threading,
  unreachable-block elimination, redundant-jump removal, constant
  folding, peephole rewrites all in `compile/flowgraph_cfg_passes.go`.
- **P12 (generator) is already complete.** gopy uses a goroutine
  + channel model that avoids frame copies entirely. The draft's
  "per-send frame copy cost" diagnosis was incorrect.
- **P13 (GC) is ~90% done.** Tracking machinery, gc.get_objects,
  gc.get_referrers, gc.get_referents, gc.is_tracked all ported.
  Gap: `gc.set_threshold()` doesn't drive collections, and
  `gc.collect()` delegates to `runtime.GC()` rather than driving
  CPython's gen-0/1/2 logic.

The remaining structural blockers are now:

1. **P2 trace gate.** `interp.JIT` hardcoded false. Until that flips,
   tier-2 is dead code.
2. **P5 ↔ P1 coherency.** Dict watcher hook plumbing exists
   (`DictMutationHook` in `objects/dict_specialize.go:98-108`) but
   no public subscription API, so the specializer cannot safely
   invalidate inline caches on dict mutation.
3. **P7 ↔ P1 coherency.** Type `versionTag` exists
   (`objects/type.go:197`) but is **never automatically invalidated**
   on MRO mutation, `__setattr__` on a class, or `__bases__`
   reassignment. Slot tables in `objects/slots.go` are defined but
   never pre-populated at type creation; every `LookupDescriptor`
   walks the MRO from scratch.
4. **P14 native modules absent.** `_pickle`, `_elementtree`,
   `_sqlite3` modules are missing; `pickle`, `xml_etree_*`,
   `sqlite_synth` benches cannot run.
5. **P15 unicode writer absent.** Zero of CPython's 13
   `_PyUnicodeWriter_*` functions ported; every f-string,
   `str.format`, `%` formatting allocates intermediate strings.

## Goal

| Bench                  | cpython 3.14 | gopy target | gopy 2026-05-16 |
| ---------------------- | -----------: | ----------: | --------------: |
| `pyperformance` geomean| 1.0x         | <=1.5x      |          283x   |
| `nbody`                | 1.0x         | <=2.0x      |         5.26x   |
| `fannkuch`             | 1.0x         | <=2.0x      |        28.83x   |
| `richards`             | 1.0x         | <=2.0x      |         1899x   |
| `unpack_sequence`      | 1.0x         | <=2.0x      |          254x   |
| `call_method`          | 1.0x         | <=1.5x      |         2407x   |
| `regex_compile`        | 1.0x         | <=2.0x      |         1952x   |
| `pidigits`             | 1.0x         | <=2.0x      |         7.83x   |
| `json_dumps`           | 1.0x         | <=2.0x      |        485.60x  |

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
| P1.5 | Bytecode cache persistence: `Code.Quickened` + `CacheObjects` slab survive `marshal.dumps`/`marshal.loads` so `.pyc` files retain specialization (CPython persists the warmed cache via the `co_quickened` byte-blob next to `co_code`). Requires marshal-writer extension for the parallel-pointer slab; the `Code.Quickened` flag itself rides in the existing flags word. | TODO | - |
| P1.6 | Cross-cutting coherency: install dict watcher (P5.5) + type-version invalidation (P7.5) hooks at `specialize.Enable` time so inline caches invalidate atomically on dict/type mutation. Without this, every LOAD_ATTR / LOAD_GLOBAL inline cache risks reading stale state after a class attribute assignment. | TODO | - |

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
| LOAD_ATTR | 8/9 emitted | `vm/eval_specialized.go` — `MODULE`, `SLOT`, `CLASS`, `CLASS_WITH_METACLASS_CHECK`, `METHOD_NO_DICT`, `NONDESCRIPTOR_NO_DICT`, `PROPERTY`, `INSTANCE_VALUE` | `specialize/gatedata/spec_property.py` (`TestGateSpecPropertyAndMethod`) | WIP — `WITH_HINT` deferred until dict keys-version cache stamping lands | 691c2d7, 71a9181 |
| TO_BOOL | 6/6 | `vm/eval_specialized.go` — `BOOL`, `INT`, `LIST`, `NONE`, `STR`, `ALWAYS_TRUE` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| COMPARE_OP | 3/3 | `vm/eval_specialized_compare.go` — `INT`, `FLOAT`, `STR` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| CONTAINS_OP | 2/2 | `vm/eval_specialized.go` — `DICT`, `SET` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| UNPACK_SEQUENCE | 3/3 | `vm/eval_specialized.go` — `TWO_TUPLE`, `TUPLE`, `LIST` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| STORE_SUBSCR | 2/2 | `vm/eval_specialized.go` — `LIST_INT`, `DICT` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| BINARY_OP | 13/13 non-JIT | `vm/eval_specialized_binary_op.go` — `ADD_INT`, `SUBTRACT_INT`, `MULTIPLY_INT` (math/bits overflow guard); `ADD_FLOAT`, `SUBTRACT_FLOAT`, `MULTIPLY_FLOAT`; `ADD_UNICODE` shared with `INPLACE_ADD_UNICODE`; `SUBSCR_LIST_INT`, `SUBSCR_TUPLE_INT`, `SUBSCR_STR_INT` (ASCII fast path), `SUBSCR_DICT`, `SUBSCR_LIST_SLICE` | `specialize/gatedata/spec_binary_op.py` (`TestGateSpecBinaryOp`) | DONE | 6a8aace |
| FOR_ITER | 0/4 | — | — | TODO — needs typed `Next` helpers on `objects.{listIterator,tupleIterator,rangeIterator}` so the arm can skip the `IterNext` slot lookup | - |
| LOAD_GLOBAL | 2/2 | `vm/eval_specialized_load_global.go` — `MODULE`, `BUILTIN` | `specialize/gatedata/spec_load_global.py` (`TestGateSpecLoadGlobal`) | DONE | 2f1f603 |
| STORE_ATTR | 1/3 | `vm/eval_specialized_store_attr.go` — `SLOT` (faithful 1-1 port of CPython's macro: validate type_version, write to cached `Instance.slots[idx]`) | `specialize/gatedata/spec_store_attr.py` (`TestGateSpecStoreAttr`) | WIP — `INSTANCE_VALUE` and `WITH_HINT` deliberately deferred; they need a `Dict.SetValueAt(slot, value)` primitive that writes the entry's value cell without re-hashing the key, plus the managed-dict-offset modelling listed in P1.4a. Shipping them before that lands forces a shim that re-runs `SetItem(name, value)`, which is exactly the ad-hoc patch the ground rule forbids. | 96130ac |
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

### P2. Tier-2 micro-op interpreter — `Python/executor_cases.c.h`, `Python/optimizer_bytecodes.c`

**Audit.** Actual LOC under `optimizer/` is **13,501** (not the
~23k earlier estimate); the discrepancy was the difference between
`wc -l` of generated stub bodies and what was actually shipping.
Per-file breakdown:

| File                       | LOC   | Role                                            |
|----------------------------|------:|-------------------------------------------------|
| `uops_stubs_gen.go`        |  8263 | per-uop stub bodies (generated; all 271 are deopt pass-throughs) |
| `symbols.go`               |   734 | symbolic-state lattice (`Python/optimizer_symbols.c`) |
| `uop_ids_gen.go`           |   661 | uop opcode enum (generated)                     |
| `uops_dispatch_gen.go`     |   592 | dispatch switch                                 |
| `trace.go`                 |   486 | trace projection (`Python/optimizer.c:553-987`) |
| `types.go`                 |   404 | metadata                                        |
| `analysis.go`              |   354 | analysis pass (`Python/optimizer_analysis.c:625-654`) |
| `uop_meta_gen.go`          |   335 | generated metadata                              |
| `executor.go`              |   324 | lifecycle (`Python/optimizer.c:216-272,1100-1115,1417-1518`) |
| `watcher.go`               |   320 | type / dict mutation callbacks                  |
| `optimize.go`              |   258 | optimization driver (`Python/optimizer.c:113-163`) |
| `uops_impl.go`             |   174 | hand-written uop bodies                         |
| `side_table.go`            |   143 | side-table for backedges                        |
| `uops.go`                  |   132 | executor entry + trampoline                    |
| `pyobject.go`              |   128 | PyObject helpers                                |
| `bloom.go`                 |    86 | bloom filter (`Python/optimizer.c:1357-1414`)   |
| `uops_print.go`            |    60 | dis output                                      |
| `dis_hook.go`              |    47 | dis integration                                 |

Stubs are generated for all 319 uop IDs. The hand-ported set in
`uops_impl.go` covers **14** uops, but only **3** of them
(`_LOAD_FAST`, `_STORE_FAST`, `_CHECK_VALIDITY`) are P2.2 hot-path
targets. The other 11 are scaffolding: `_NOP`, `_EXIT_TRACE`,
`_JUMP_TO_TOP`, `_START_EXECUTOR`, `_SET_IP`, `_POP_TOP`, `_COPY`,
`_SWAP`, `_PUSH_NULL`, `_LOAD_FAST_BORROW`, `_MAKE_WARM`.

**Gap (the smoking gun for P2).** The tier-2 entry gate is wired,
but `interp.JIT` is **hardcoded `false`** at `vm/tier2.go:36`:

```go
func (e *EvalState) tryWarmupTier2(...) {
    if !interp.JIT {
        return
    }
    ...
}
```

`grep -rn "interp.JIT\s*=" --type go` returns zero hits. The
projection (`trace.go`), analysis (`analysis.go`), executor
(`executor.go`), and dispatch loop (`vm/tier2.go:enterExecutor`)
are all wired but never reachable.

The other two structural gaps are full-file ports that have not
started:

- **`Python/optimizer_bytecodes.c` (1107 LOC, 0 ported).** The
  abstract-interpreter case table `optimize_uops` is supposed to
  dispatch through. gopy's `analysis.go:optimizeUops` (lines
  230-256) iterates the trace with an empty per-opcode dispatcher
  and bails to "unknown semantics" on every row. No constant
  folding, no guard elimination, no type narrowing.
- **`Python/executor_cases.c.h` (7163 LOC, 0 ported as real
  bodies).** The 271 stubs all return `s.unimplementedUop(NAME)`
  which deopts to tier-1. Hot paths like `_BINARY_OP_ADD_INT`,
  `_GUARD_BOTH_INT`, `_LOAD_ATTR_INSTANCE_VALUE`,
  `_CALL_PY_EXACT_ARGS`, `_PUSH_FRAME`, `_FOR_ITER_TIER_TWO`,
  `_GUARD_TYPE_VERSION`, `_RESUME_CHECK` are all stubs.

Two deprecated-shim flags annotate the situation: `uops_impl.go:14`
and `analysis.go:23` both carry `DEPRECATED (spec 1714)` notes
indicating the uop bodies should move to `vm/eval_uops_gen.go`
once the cases-generator port (spec 1714) ships.

**Phases (full-file ports, no piecemeal uop cherry-picking).**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P2.1 | Open the JIT gate. Flip `interp.JIT` to true by default (or behind a `-O2` CLI flag) so trace projection actually fires. Add `optimizer/trace_test.go` that runs `call_method.py` and asserts ≥1 trace was projected. | TODO | - |
| P2.2 | Port `Python/optimizer_bytecodes.c` in full (1107 LOC). This is the abstract-interpreter case table that `optimize_uops` dispatches through. Lands as `optimizer/optimizer_bytecodes_gen.go` driven by the spec-1714 cases generator. Gate: every uop ID has a corresponding case body (no `unknown semantics` bail). | TODO | - |
| P2.3 | Port `Python/executor_cases.c.h` in full (7163 LOC) into `vm/eval_uops_gen.go`. Driven by the spec-1714 cases generator. Replaces the 271 deopt-pass-through stubs in `optimizer/uops_stubs_gen.go`. Gate: every uop ID has a real executable body. | TODO | - |
| P2.4 | Wire tier-2 → tier-1 deopt path: on guard fail mid-trace, fall back to the adaptive opcode at the recorded resume offset. Validate against `_CHECK_VALIDITY` and `_GUARD_TYPE_VERSION` failure scenarios. | TODO | - |
| P2.5 | Turn on the tier-2 executor by default for any function that has been Quickened (depends on P1.5 marshal persistence so warm caches survive). | TODO | - |

**Gate.**

- `optimizer/uops_test.go` covers every uop ID with one
  positive case and one guard-fail case (table-driven, generated).
- `optimizer/bench_test.go::BenchmarkTier2Nbody` shows the tier-2
  path is ≥2x faster than tier-1 on the warm loop.
- The 11 scaffolding uops in `uops_impl.go` can stay hand-written;
  P2.2/P2.3 covers the 271 stubs that currently deopt.

**Estimated win.** 1.5-2x on top of P1.

### P3. PyLong fast path — `Objects/longobject.c`

**Audit.** CPython 3.14 `Objects/longobject.c` is 6871 LOC and
exports ~90 public `PyLong_*` functions. gopy has selective
coverage across 6 files totalling ~1050 LOC:

| File                          | LOC | Role                                              |
|-------------------------------|----:|---------------------------------------------------|
| `objects/int.go`              | 216 | `NewInt`, `NewIntFromBig`, `Int64`, `BigInt`, `Sign`. Constructor + getters. |
| `objects/long_cache.go`       |  77 | small-int singleton cache `[-5, 256]` (`SmallInt`) |
| `objects/long_arith.go`       | 157 | `intAdd`, `intSub`, `intMul`, `intFloorDiv`, `intMod`, `intDivmod`, `intPower` |
| `objects/long_bitwise.go`     | 165 | `intAnd`, `intOr`, `intXor`, `intLshift`, `intRshift`, `intInvert` |
| `objects/long_misc.go`        | 152 | `intAbs`, `intNeg`, `intPos`, `intHash`, `intBool` |
| `objects/long_parse.go`       | 285 | `intFromString` |

Audit verified `NewInt(x int64)` consults `smallIntFromInt64(x)` at
`int.go:67-75` and returns the singleton when `x` is in
`[-5, 256]`, so the small-int cache **is** wired (the earlier draft
was wrong on that point). Every arithmetic op still allocates a
fresh `*Int` and routes through `math/big.Int`, even when both
sides fit in int64.

**Gap.**

- No compact representation: `Int` always carries a heap-allocated
  `big.Int` (`int.go:14-16`). CPython packs `|n| < 2^30` inline in
  the PyLong header via `_PyLong_IsCompact`.
- No int64 fast-path: `intAdd` at `long_arith.go:17-39` unwraps
  both operands and calls `big.Int.Add` unconditionally. No
  short-circuit for `(a.v.IsInt64() && b.v.IsInt64()) && (no
  overflow)`.
- `__index__` slot is defined on `NumberMethods` (`slots.go`) but
  not wired on `IntType` at `int.go:56-59`.
- Unported PyLong functions include `PyLong_AsLongAndOverflow`,
  `PyLong_AsInt`, `PyLong_AsNativeBytes` (PEP 1692),
  `PyLong_FromNativeBytes`, `PyLong_AsDouble`, `_PyLong_Frexp`, and
  the v3.14 streaming `PyLongWriter_*` API.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P3.1 | `objects/long_fast.go`: `compactInt`/`compactPair` int64 view + overflow helpers (`addOverflow`, `subOverflow`, `mulOverflow`, `negOverflow`, `absOverflow`). Reuses existing `big.Int` storage; fast path bypasses the temp `new(big.Int)` and falls through to the slow path only on overflow. | DONE | objects/long_fast.go |
| P3.2 | `NewInt(int64)` already routes through `smallIntFromInt64` so `[-5, 256]` is alloc-free; fast-path slots feed results through `NewInt` so the cache singleton is returned for the common case. | DONE | objects/int.go:67 (verified) |
| P3.3 | `intAdd`/`intSub`/`intMul`/`intNeg`/`intAbs`/`intAnd`/`intOr`/`intXor`/`intInvert` fast path: int64 arithmetic with overflow check when both operands are compact; fall back to `big.Int` on overflow. | DONE | objects/long_arith.go, objects/long_bitwise.go, objects/long_misc.go |
| P3.4 | `__index__` / `PyLong_AsLong` fast path. Already covered by `(*Int).Int64()` returning `(int64, ok)` and by `compactInt(i)` short-circuiting on `i.v.IsInt64()`. | DONE | objects/int.go:94, objects/long_fast.go |
| P3.5 | `_PyLong_FromUint64` / `_PyLong_FromInt64` mirrored constructors that bypass big.Int when input fits compact. Deferred until the storage layout is refactored to keep an inline int64; the alloc savings are real but require touching every reader of `Int.v`. | DEFERRED | - |

**Gate.**

- `objects/long_fast_test.go` cross-checks every fast-path slot
  (`intAdd`/`intSub`/`intMul`/`intAnd`/`intOr`/`intXor`/`intInvert`/`intNeg`/`intAbs`)
  against the `big.Int` slow path on a 5000-entry randomized table plus
  an overflow-boundary table (`MaxInt64`, `MinInt64`, `(1<<40)^2`).
- `BenchmarkLongAddSmall` and `BenchmarkLongMulSmall` show **0 allocs**
  and 5.3 ns / 8.6 ns per op on Apple M4 (previously 3 allocs + ~70
  ns). `BenchmarkLongAddLarge` keeps 3 allocs / 65 ns to confirm the
  big.Int slow path still fires when an operand grows past int64.
- `pidigits` bench expected to drop from 7.83x to under 2x cpython
  after P10 (float pool) lands and the multi-word path is exercised
  less.

**Estimated win.** 3x on integer-heavy benchmarks (pidigits, pyflate,
go, hexiom). Geomean impact ~1.4x.

**Technical notes (P3 PyLong fast path).**

1. CPython's compact representation is `_PyLong_BothAreCompact`, which
   in 3.14 checks that both PyLongs have `ob_size in {-1, 0, 1}` and
   that `medium_value(x)` (a signed `stwodigits`, two 30-bit digits)
   holds the value. gopy's analogue is `i.v.IsInt64()`; the int64
   window is strictly larger than the CPython compact window on 64-bit
   builds so we never miss a fast-path opportunity that CPython takes.
2. Overflow detection is the well-known sign-bit XOR trick for add /
   sub and `math/bits.Mul64` for mul. The mul helper splits the operand
   signs out and then re-applies them after the unsigned multiply to
   keep the int64 wraparound semantics consistent with `int64 * int64`
   on every reachable input pair.
3. `negOverflow` and `absOverflow` handle the single overflow case at
   `math.MinInt64` (the negation of which does not fit). CPython hits
   the same boundary at `medium_value == -(1 << (PYLONG_BITS - 1))`
   and falls back to multi-digit construction.
4. `intInvert` does not need an overflow guard because `^x` for any
   int64 stays inside int64 (two's-complement bit-flip is a closed
   operation on the type).
5. The fast path threads results through `NewInt(int64)` which already
   consults `smallIntFromInt64` for the `[-5, 256]` cache. Hot loops
   that bounce inside that window (counter increments, boolean
   coercions, small comparisons) are now **allocation-free**, which is
   what the BenchmarkLongAddSmall numbers above demonstrate.
6. We deliberately did **not** add a `compact int64; isCompact bool`
   pair to `Int` itself. The minimum-blast-radius design keeps `i.v`
   as the sole storage and reuses `IsInt64()` as the cheap compact
   predicate. A future P3.5 step can replace the big.Int storage with
   an inline int64 + lazy-materialised big.Int for the multi-word
   path, but that refactor touches every reader of `Int.v` (about 14
   files in objects/, plus marshal/, format/, vm/) and is best landed
   on its own branch after P10 + P7.4 settle.

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

**Audit.** CPython 3.14 `Objects/dictobject.c` is 7824 LOC. gopy's
dict already uses an open-addressed layout (the earlier draft was
wrong about `map[any]any`). Supporting files:

| File                       | Role                                       |
|----------------------------|--------------------------------------------|
| `dict.go`                  | combined dict, **already open-addressed**: `entries []dictEntry` + `order []int` |
| `dict_split.go`            | shared-keys surface (`NewSplitDict`, `ConvertToCombined`); zero memory savings |
| `dict_lookup.go`           | lookup dispatch via `d.lookup(hash, key)` |
| `dict_iter.go`             | iteration ordered by `order` slot indices |
| `dict_mutate.go`           | insert/delete/resize, drives `invalidateKeysVersion` |
| `dict_specialize.go`       | `DictMutationHook` (fired on every mutation), `IsKeysUnicode`, `LookupString`, `GetKeysVersion` |

`dict_split.go` is honest about the surface-only gap: `NewSplitDict`
returns a regular combined `Dict` pre-populated with the shared
key names mapped to `None`. Instances do not share keys with the
type; the storage savings CPython gets from split-keys are zero
in gopy.

**Verified layout** at `dict.go:30-59`:

```go
type Dict struct {
    Header
    entries       []dictEntry        // open-addressed slot array
    order         []int              // insertion-order indices
    used, fill    int
    kind          dictKind
    sharedKeys    *SharedKeys
    keysVersion   uint32             // dk_version (specializer)
    mutationCount uint32             // watcher tally
}
type dictEntry struct {
    hash         int64
    key, value   Object
    used, dummy  bool
}
```

The hooks the specializer needs are mostly plumbed:
`invalidateKeysVersion` fires `DictMutationHook(d)` from
`dict_mutate.go:82` (insert), `:105` (delete), `:118` (resize).

**Gap.**

- Split-keys saves zero memory; every instance still carries a full
  `Dict`. CPython's `PyDictKeys_NumValues` / per-instance
  `values[]` slab is not modelled.
- No `PyDict_Watch` subscription API. `DictMutationHook` is a bare
  function-pointer at module scope (`dict_specialize.go:98-108`)
  intended for the tier-2 optimizer to install at `WatcherInit`
  time. No public watcher-handle API exists for user code or other
  subsystems.
- No `_PyDict_SetItem_KnownHash` fast path. `dictInsert` at
  `dict_mutate.go:60-84` always rehashes via `d.lookup(hash, key)`,
  ignoring a pre-computed hash even when the caller (e.g. a
  LOAD_ATTR specialized arm) knows it.
- Cross-cutting: P1 inline caching cannot safely cache dict keys
  across calls until P5.5 watcher + P7 type-version invalidation
  land together. Today the cache works only because the specializer
  refuses to elide the `keys_version` check on the hot path.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P5.1 | Audit / regression-check the existing open-addressed layout against `Objects/dictobject.c:lookdict` probe sequence. Add `objects/dict_lookup_parity_test.go` table-driven from CPython's hash collisions. | TODO | - |
| P5.2 | Real split-keys storage: per-type `SharedKeys` object owns the entries-array shape; instance `__dict__` carries `values []Object` only. Materialise to combined on delete or non-shared insert. Cite `Objects/dictobject.c:insertion_resize_inplace`. | TODO | - |
| P5.3 | `_PyDict_SetItem_KnownHash` fast path: skip rehash when caller passes the hash. Wire from LOAD_ATTR / LOAD_GLOBAL specialized arms. Cite `Objects/dictobject.c:_PyDict_SetItem_KnownHash`. | TODO | - |
| P5.4 | Public watcher subscription API: `PyDict_Watch(watcher_id, dict)` / `PyDict_AddWatcher(callback) -> int8_t`. Cite `Objects/dictobject.c:7710 PyDict_Watch / :7741 PyDict_AddWatcher`. Replaces the bare `DictMutationHook` pointer. | DONE | objects/dict_watcher.go + objects/dict.go (`watcherTag`), objects/dict_mutate.go + objects/dict.go fire ADDED / MODIFIED / DELETED / CLEARED / CLONED; optimizer/watcher.go delegates AddWatcher/Watch/Unwatch to the public API; `DictMutationHook` retired. |
| P5.5 | Install the watcher at `specialize.Enable` time + invalidate inline caches on dict mutation. Interacts with P1.6. | TODO | - |

**Gate.**

- `objects/dict_oa_test.go` cross-checks every op against a
  reference implementation on a randomized workload.
- `BenchmarkDictLookup` shows 0 allocations on the hot path.
- `meteor_contest` / `go` benches drop primarily on P5.

**Estimated win.** 2x on attribute- and call-method-heavy code.

**Technical notes (P5.4 dict watcher port).**

1. `_ma_watcher_tag` is a uint64 in CPython. Bits 0-7 are the
   subscription bitmask (`DICT_WATCHER_MASK`), bits 8-11 are the
   mutation counter the Tier-2 globals folder reads
   (`DICT_WATCHED_MUTATION_BITS = 4`), bits 12-31 are reserved, and
   bits 32-63 are the per-dict unique id for free-threaded refcount.
   gopy only mirrors the low-8 subscription bits inline on `Dict`
   (`watcherTag uint64`); the mutation counter stays in its own
   `mutationCount uint32` because the Tier-2 folder reads it directly
   and the embedded layout would force an atomic dance every read.
2. `DictMaxWatchers = 8` is hard-coded in CPython at
   `pycore_dict_state.h:11`. Slots 0 and 1 are reserved for the
   Tier-2 BUILTINS / GLOBALS watcher: `PyDict_AddWatcher` walks from
   index 2. The optimizer needs an internal back-door to install
   into a reserved slot; gopy exposes that as
   `DictSetReservedWatcher` (the CPython equivalent is writing
   `interp->dict_state.watchers[i]` directly inside `remove_globals`).
3. `_PyDict_NotifyEvent` and `_PyDict_SendEvent` are split in CPython
   so the inline notify path can hot-skip on `watcher_bits == 0` and
   only spill into the dispatch loop when somebody is subscribed.
   gopy folds the version bump (`DICT_VERSION_INCREMENT` in CPython)
   into `notifyDictEvent` so the mutation paths don't carry two
   hooks. Effect on the counter is identical.
4. Mutation site map (CPython site -> gopy site):
   `insertdict` ADDED at dictobject.c:1806/1869 ->
   `dictInsert` (objects/dict_mutate.go).
   `insertdict` MODIFIED at dictobject.c:1875 -> same.
   `delitem_common` DELETED at dictobject.c:2872 ->
   `dictDelete`.
   `PyDict_Clear` CLEARED at dictobject.c:2979 ->
   `dictClearMethod` (objects/dict.go); fires once even though
   the implementation loops over `DelItem`, by masking the
   watcher bits for the duration of the inner loop.
   `dict_merge` CLONED at dictobject.c:3915 ->
   `dictCopyMethod`. Source dict is passed as the "key" arg per
   CPython's encoding.
   DEALLOCATED at dictobject.c:3370 (`dict_dealloc`) ->
   *not ported*. Go's GC has no faithful equivalent to `tp_dealloc`;
   a `runtime.SetFinalizer` would resurrect the dict through the
   callback and is unsound. Documented in `dict_watcher.go`.
5. The previous gopy design used a per-watcher map keyed on `*Dict`
   pointer (in `optimizer/watcher.go`). Replacing it with the
   per-dict bitmask removes one map allocation on the first
   subscribe per dict and aligns the data layout with CPython, so a
   future C-extension consumer of the watcher API gets the same
   semantics out of the box.
6. The dict callback signature became `(event, *Dict, key Object,
   newValue Object) -> int` (vs `unsafe.Pointer` triple in the old
   internal API). The optimizer wraps that through
   `adaptDictWatchCallback` because its `ExecutorsInvalidateDependency`
   bloom is keyed on raw addresses.

### P6. Frame free-list + LOAD_FAST_CHECK — `Objects/frameobject.c`, `Python/ceval.c`

**Audit.** `objects/frame.go`, `objects/frame_locals.go`,
`objects/frame_snapshot.go` cover the frame + locals
representation. `vm/eval.go` allocates a fresh frame per call. P6.2
LOAD_FAST_CHECK shipped via spec 1716:

- `compile/flowgraph_cfg_locals.go:320-358 scanBlockForLocals`
  detects uninitialized locals and rewrites
  `LOAD_FAST → LOAD_FAST_CHECK`.
- `vm/eval_dispatch_handwritten.go:63-72 opLOAD_FAST_CHECK`
  mirrors CPython's `bytecodes.c` check.
- Opcode 88 in `compile/opcodes_gen.go` matches CPython 3.14's
  metadata.

**Gap.**

- No frame free-list. Every function call allocates `*Frame` + a
  fresh `[]Object` for locals + a fresh stack slice.
- No `LOAD_FAST_BORROW` / `STORE_FAST_STORE_FAST` opcodes
  (CPython 3.14 elide-the-incref-pair pair).
- `vm/eval_call.go` rebuilds the args tuple per call even for
  CALL_PY_EXACT_ARGS.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P6.1 | `vm/frame_pool.go`: per-goroutine free list, capped at 20. Recycle frame + locals + stack slices; reset, not free. | TODO | - |
| P6.2 | `LOAD_FAST_CHECK` codegen in `compile/flowgraph_cfg_locals.go:scanBlockForLocals` + eval arm in `vm/eval_dispatch_handwritten.go:opLOAD_FAST_CHECK`. | DONE (spec 1716) | - |
| P6.3 | `LOAD_FAST_BORROW` / `STORE_FAST_STORE_FAST` (CPython 3.14 new opcodes that elide the incref pair). | TODO | - |
| P6.4 | Args-tuple bypass: `CALL_PY_EXACT_ARGS` stores args directly into the callee's frame locals. | TODO | - |

**Gate.**

- `vm/frame_pool_test.go` proves recycle works under load.
- `BenchmarkCallNop` shows 0 allocations on the hot path.

**Estimated win.** 1.5x on call-heavy code (richards, deltablue).

### P7. Type slot caching — `Objects/typeobject.c`

**Audit.** CPython 3.14 `Objects/typeobject.c` is 12,302 LOC. gopy
spreads its type implementation across `objects/type.go`,
`type_call.go`, `type_attr.go`, `type_getsets.go`, `type_repr.go`,
`type_specialize.go`, `usertype.go`. The MRO walk lives in
`descr.go:LookupDescriptor`. `type_specialize.go` is the hook the
specializer calls.

Slot tables (`NumberMethods`, `SequenceMethods`, `MappingMethods`,
`AsyncMethods`) exist in `slots.go` covering most of CPython's
`nb_*`, `sq_*`, `mp_*`, `am_*` slots, but `objects/type_slots.go`
does **not** exist; the spec's reference to it is aspirational.

The type carries a `versionTag uint32` at `type.go:197` plus
`VersionTag()` / `InvalidateVersionTag()` getters in
`type_specialize.go:10-39`.

**Gap.**

- `LookupDescriptor(t, "__add__")` at `descr.go:101-114` walks
  `t.MRO` on every invocation. No slot-table cache. Operator
  dispatch (`intAdd`, `intMul`, etc.) re-resolves descriptors per
  call.
- No `_PyType_AssignSpecialMethods` equivalent. `NewType` at
  `type.go:255-266` builds MRO but does not pre-populate operator
  slots from MRO.
- `versionTag` is **never automatically invalidated**. Searching
  `InvalidateVersionTag` returns zero call sites in `type_attr.go`
  or the rest of `objects/`; manual invalidation is the only path.
  Class `__setattr__`, MRO recomputation, and `__bases__`
  reassignment do not bump the tag.
- The `Index` slot on `NumberMethods` is defined but not wired on
  `IntType` at `int.go:56-59`.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P7.0 | Public type-watcher subscription API: `PyType_Watch(id, type)` / `PyType_AddWatcher(callback) -> int`. Cite `Objects/typeobject.c:1016 PyType_AddWatcher / :1060 PyType_Watch / :1170 notify loop in type_modified_unlocked`. Replaces the bare `TypeModifiedHook` pointer. | DONE | objects/type_watcher.go + objects/type.go (`tpWatched`), objects/type_specialize.go fires through `notifyTypeWatchers`; optimizer/watcher.go delegates AddWatcher/Watch/Unwatch to the public API; `TypeModifiedHook` retired. |
| P7.1 | `objects/type_slots.go`: full slot-table struct mirroring CPython `PyTypeObject` (nb_add, sq_length, mp_subscript, tp_call, tp_iter, ...). | TODO | - |
| P7.2 | `_PyType_AssignSpecialMethods`: walk the MRO once at type creation, populate the slot table. | DONE | `d71cf26` (objects/type_inherit.go new; objects/type.go + objects/usertype.go inherit hook; objects/type_inherit_test.go gates) |
| P7.3 | Type version tag (monotonic uint32 bumped on MRO mutation, class `__setattr__`, `__class__` reassignment). | TODO | - |
| P7.4 | Operator dispatch (`abstract_binop.go`, `abstract_sequence.go`) consults the slot table first; falls back to `Lookup` only if slot nil. | DONE | objects/abstract_number.go `numberSlot` collapsed to single-field read on `o.Type().Number` after P7.2 inherit_slots port; sequence/mapping/async dispatch already used direct field load; objects/structseq.go documents the wholesale-replacement caveat. |
| P7.5 | Invalidation hook: type-version change auto-stales every inline cache keyed on that version (interacts with P1). | TODO | - |

**Gate.**

- All existing operator tests stay green.
- `objects/slots_test.go`: slot table populated correctly for a
  hand-rolled type; invalidates on mutation.
- `richards` ratio compresses by another ~2x on top of P1.

**Technical notes (P7.0 type watcher port).**

1. `tp_watched` is a single `uint8` in CPython (Include/cpython/object.h:234)
   not a uint64 like `_ma_watcher_tag`. The type watcher table is
   smaller and there is no per-type mutation counter on the type
   object: type version tags live in `tp_version_tag` and have their
   own bookkeeping in `types.type_version_cache`. gopy mirrors the
   8-bit bitmask exactly on `Type.tpWatched`.
2. `TYPE_MAX_WATCHERS = 8` is hard-coded at
   `pycore_interp_structs.h:22`. Slot 0 is reserved for the Tier-2
   optimizer; CPython's `PyType_AddWatcher` walks from index 1.
   Asymmetric with dicts (which reserve 0 and 1 for BUILTINS and
   GLOBALS): types only need one optimizer slot because the type
   watcher fans out over every mutated type, not per attribute scope.
   gopy keeps the asymmetry: `typeReservedWatchers = 1`,
   `TypeAddWatcher` returns slot 1 or higher,
   `TypeSetReservedWatcher` is the back-door for slot 0.
3. The notify loop inside `type_modified_unlocked`
   (typeobject.c:1170-1188) walks the bits the same way
   `_PyDict_SendEvent` does. gopy's `notifyTypeWatchers` ports it
   verbatim. The ordering matters: CPython notifies watchers *before*
   `set_version_unlocked(type, 0)` writes the new tag, so the watcher
   sees the type in its still-watched, still-valid state. gopy's
   `InvalidateVersionTag` follows the same order: `notifyTypeWatchers(t)`
   then `t.versionTag = 0`.
4. `PyType_Watch` calls `assign_version_tag` before setting the
   tp_watched bit (typeobject.c:1074). The reason: if the version tag
   is 0, the next mutation short-circuits inside
   `type_modified_unlocked` (the `if (type->tp_version_tag == 0)
   return` at typeobject.c:1148) and the watcher would never fire.
   gopy's `TypeWatch` calls `t.VersionTag()` for the same reason
   before flipping the bit.
5. The dispatch path used to be `TypeModifiedHook func(t *Type)` in
   gopy. Replacing it with the bitmask + table layout gives multiple
   watchers (8 slots), makes user-installed type watchers possible,
   and removes the global function pointer that imposed a
   single-consumer constraint on the type-modify path. Sub-interp
   promotion later moves the table off the package into
   `state.Interpreter`; the call sites (`InvalidateVersionTag`,
   `TypeWatch`, `TypeUnwatch`) are the only ones that need an
   interp pointer threaded.
6. The optimizer's `DispatchTypeMutation` became a thin shim that
   ensures the version tag is allocated then calls
   `InvalidateVersionTag` on the type. It is retained because some
   gate tests drive a raw `unsafe.Pointer` (typed as a Type) through
   the dispatch path without going through Setattr. Production
   mutation sites all go through `InvalidateVersionTag` directly.

**Technical notes (P7.2 inherit_slots port).**

1. CPython's `inherit_slots` (typeobject.c:8227) is gated by the
   `SLOTDEFINED` macro: `base->SLOT != 0 && (basebase->SLOT == 0 ||
   base->SLOT != basebase->SLOT)`. The intent is that a slot is only
   copied if the base "owns" it (defines it locally or differs from
   the grandparent). Go cannot port this directly: function values
   only compare to nil, never to another function value. Our port
   collapses the test to "copy if subclass slot is nil and ancestor
   slot is non-nil" and walks the full MRO ancestor-by-ancestor. The
   first ancestor that supplies the slot wins. This matches CPython's
   net behaviour for typical hierarchies because the SLOTDEFINED
   check almost always succeeds when the slot exists on `base`; the
   difference only matters when an intermediate base re-aliases a
   grandparent's slot pointer (rare in pure-Python code, more common
   in C extensions).
2. Bundles (`NumberMethods`, `SequenceMethods`, `MappingMethods`,
   `AsyncMethods`) are deep-copied per-subclass, not pointer-shared
   the way CPython does in `type_ready_inherit_as_structs`
   (typeobject.c:8685). The reason is gopy-specific: `fixupHashAndIter`
   and the other fixup passes in `usertype.go` write per-type slot
   dispatchers back into the bundle. If sub and base shared the bundle
   pointer, installing a slot dispatcher on the subclass would also
   overwrite the base's slot. The `SubclassBundleIsIndependent` gate
   in `type_inherit_test.go` locks this behaviour in.
3. Two inheritance entrypoints, two different scopes. `NewType`
   (used for built-in types) only inherits bundles + protocol
   pointers via `inheritSlotsAllMRO` and `inheritProtocolPointers`.
   Scalar slots (TpNew, Call, Hash, Repr, Str, ...) stay nil.
   `NewUserType` calls the same MRO walk plus
   `inheritDirectBaseScalars` for every direct base, then runs the
   fixup passes. The split is forced by gopy's `typeCall` fallback
   architecture: `typeType`, `enumerateType`, `ReversedType`, and the
   entire exception chain (`BaseException` -> `Exception` ->
   `ValueError`, etc.) intentionally leave `TpNew` nil and route
   construction through `typeCall`'s `IsSubtype(cls, typeType)` /
   exception-init branch. If `NewType` inherited `object.TpNew`
   through the MRO walk, `typeCall` would dispatch through
   `objectNew` and raise `"Meta() takes no arguments"` or
   `"ValueError() takes no arguments"`. CPython does not have this
   conflict because its `PyType_Type.tp_new` is an owned slot
   (`type_new`) so SLOTDEFINED keeps `object.tp_new` out.
4. `__hash__` override skipping is ported faithfully. CPython's
   `overrides_hash` (typeobject.c:8205) inspects the type dict; the
   gopy port reads `typeDescrTable[t]["__hash__"]`. When the
   namespace declares `__hash__` (including `__hash__ = None`), both
   `Hash` and `RichCmp` are cleared before fixup, mirroring CPython's
   `COPYSLOT(tp_richcompare); COPYSLOT(tp_hash)` skip at
   typeobject.c:8366. The clear happens in `NewUserType` between
   `copyNamespaceToType` and `fixupSlotDispatchers` so the fixup pass
   gets a clean slate to install the per-type slot dispatcher (or
   `identityHash` if `__hash__` is `None`).
5. User-class subclasses of C-port types (dict/str/int) still take
   their `TpNew` from the explicit switch in `NewUserType` that
   forwards to the base's typed constructor. The MRO walk does not
   touch this path because the switch runs before
   `inheritDirectBaseScalars` would have a chance to copy a nil
   ancestor slot. This was already the behaviour pre-port and is
   preserved.
6. The performance payoff is not visible from the inherit pass
   alone. `inheritSlotsAllMRO` only moves the MRO walk from runtime
   (per-dispatch in `numberSlot`, `sequenceSlot`, `mappingSlot`) to
   type-creation time. The actual win lands when P7.4 rewrites
   operator dispatch to read the bundle field directly instead of
   calling `numberSlot(t, accessor)` and walking the MRO. P7.2 is the
   prerequisite that makes P7.4 safe: now the bundle on every type
   is guaranteed populated.

**Technical notes (P7.4 single-load operator dispatch).**

1. `numberSlot` (objects/abstract_number.go:20) used to walk the full
   MRO on every call: `for _, base := range o.Type().MRO { ... }`
   then `op(base.Number)`. After P7.2's inherit_slots port populated
   `t.Number` at type-creation time by COPYNUM-style deep-copy from
   every ancestor, that per-dispatch loop is dead weight. The new
   body is `n := o.Type().Number; if n == nil { return nil }; return
   op(n)`, which is one field load and one nil check. Microbenchmark
   on the int-add hot path: BenchmarkNumberAddIntsViaProtocol ~7.6
   ns/op, 0 allocs; mul ~9.3 ns/op, 0 allocs. The MRO walk used to
   be three iterations for the typical built-in (`Int -> Object` is
   length 2; user types touch length 3+).
2. Sequence / Mapping / Async dispatch sites (`abstract_sequence.go`,
   `abstract_mapping.go`, `protocol.go`, `protocol_object.go`,
   `seqiter.go`, `enum.go`) already used direct field reads on
   `o.Type().Sequence` / `.Mapping` / `.Async`. The MRO walk only
   ever lived in `numberSlot`; P7.4 brings the number protocol in
   line with the rest of the bundles.
3. structseq's wholesale-replacement quirk is documented in
   objects/structseq.go: NewType pulls Tuple.Sequence (Length, Concat,
   Repeat, GetItem, Contains) into the new type via
   inheritProtocolPointers, but structseq then replaces the bundle
   pointer wholesale, dropping the inherited slots. Attempting to
   preserve them (populate in-place) is unsafe for structseq because
   `tupleConcat` does `a.(*Tuple)` and gopy's `*StructSeq` is not a
   `*Tuple` at the Go representation level. CPython gets away with
   this because PyStructSequence_Type extends PyVarObject and shares
   tuple's `ob_item`. Re-porting tuple Concat/Repeat against
   `*StructSeq` is out of P7.4's scope and tracked as a separate
   follow-up under [[project_structseq_repr_unify]].
4. The dispatch saving is small per call (one MRO load + one
   function-pointer call instead of a loop + indexing) but compounds
   in operator-heavy loops. CPython's slot_tp_* dispatchers reach the
   target slot via a single indirection through `tp_as_number`; the
   inherit_slots COPYNUM pass at type-creation time is what makes
   that single indirection sufficient. P7.4 mirrors that contract:
   every type's Number bundle is fully populated, so the dispatcher
   never has to consult a parent.
5. Invariant: the bundle on every initialised type is populated
   *before* any dispatcher reads it. This holds because (a) `NewType`
   calls `inheritSlotsAllMRO` before returning, (b) `NewUserType`
   calls `inheritSlotsAllMRO` again after `fixupSlotDispatchers`
   installs per-type dispatchers from `__add__` / `__sub__` /
   ... dunders, and (c) `PyType_Modified` re-runs the inherit pass
   on the modified type and all subclasses, so any MRO mutation
   (class `__bases__` reassignment, runtime `__class__` swap)
   re-settles the bundles before the next dispatch.

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

**Diff captured 2026-05-19.** CPython emits `SWAP 3 / SWAP 2 / STORE_SUBSCR`;
gopy emitted `SWAP 3 / STORE_SUBSCR`. The missing `SWAP 2` left the
stack as `[..., new_value, index, container]` instead of
`[..., new_value, container, index]`, so STORE_SUBSCR's TOS (the
container slot) saw the index integer and raised TypeError. Fix:
add `c.addOpI(SWAP, 2, targetLoc)` between SWAP 3 and STORE_SUBSCR
in `visitAugAssign`'s Subscript arm. Mirrors
`Python/codegen.c:5409-5411 codegen_augassign Subscript_kind`. The
Attribute arm was already correct (`SWAP 2 / STORE_ATTR`).

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P8.1 | Capture gopy `dis` output for the reproducer; diff against cpython 3.14. Land the diff in `compile/codegen_stmt_misc_test.go::TestAugAssignSubscriptEmitsCopyCopyBinarySwapSwapStore`. | DONE | `02f6c40` |
| P8.2 | Fix the lowering in `compile/codegen_stmt_misc.go` (Subscript LHS in augmented context). Add missing `SWAP 2`. | DONE | `02f6c40` |
| P8.3 | Extend the test matrix: augmented STORE_SUBSCR with nested unpack, dict subscript, list element, attribute aug, function-returned container, all BINARY_OP flavors, deep attribute target. Runtime suite in `compile/codegen_stmt_misc_test.go`. | DONE | `02f6c40`, `5512f4f` (gofmt) |
| P8.4 | Audit augmented `STORE_ATTR` (`obj.attr -= rhs`). Already correct: `COPY 1 / LOAD_ATTR / ... / SWAP 2 / STORE_ATTR`. Test `TestAugAssignAttributeEmitsCopyLoadBinarySwapStore` locks it in. | DONE | `02f6c40` |

**Gate.** `nbody`, `fannkuch` run to completion under `bin/gopy`;
both show up with real numbers in the small-subset table.

**Estimated win.** Unblocks 2 N/A benches.

### P9. `int.__format__` format-spec parser — `Python/formatter_unicode.c`

**Symptom (was).** `'{0:04x}'.format(255)` raised `TypeError: unsupported
format string passed to int.__format__`. `stdlib/json/encoder.py:31`
(`'\\u{0:04x}'.format(i)` in `ESCAPE_DCT` initialisation) hit this on
`import json`, blocking `json_dumps`.

**Resolution.** The full `[[fill]align][sign][z][#][0][width][group][.prec][type]`
mini-language already lived in `format/format.go` (used by `str.format`
and f-strings via `str_format.go`). What was missing was the wiring:
neither `IntType.Format` nor `FloatType.Format` was set, so the
fallback `objectFormatDescr` rejected every non-empty spec. Beyond
`__format__`, `_intstr=int.__repr__` in `json/encoder.py` also pulled
the inherited `object.__repr__` (printing `<int object at 0x...>`)
because no slot wrapper for `int.__repr__` / `float.__repr__` existed
yet, so even after the format wiring landed `json.dumps` still
serialised numbers as object reprs. The fix wires both pieces.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P9.1 | `objects/long_format.go`: glue `IntType.Format` (and `BoolType.Format`, since `tp_base = PyLong_Type`) to `format.ParseSpec` + `format.FormatInt`, with a float-coercion branch for `e/E/f/F/g/G/%` codes. | DONE | `a5d25ea`, `5512f4f` (overflow + comments) |
| P9.2 | `objects/float_format.go`: glue `FloatType.Format` to `format.ParseSpec` + `format.FormatFloat`, so the int float-coercion branch and any direct `f.__format__(...)` call share the same renderer. | DONE | `a5d25ea`, `5512f4f` (gofmt + comments) |
| P9.3 | `int_bind.go` + `float.go`: install slot wrappers for `int.__repr__` / `int.__str__` / `float.__repr__` / `float.__str__` so `json/encoder.py`'s `_intstr=int.__repr__` and `_floatstr=float.__repr__` defaults bind to the real digit-emitting wrappers instead of `object.__repr__`. | DONE | `a5d25ea`, `5512f4f` (comments) |
| P9.4 | `objects/long_format_test.go`: table-driven cases pulled from CPython `Lib/test/test_format.py` (int, float-coerced, bool inherited, and the json.encoder ESCAPE_DCT loop). | DONE | `a5d25ea`, `5512f4f` (misspell) |

**Gate.** `objects/long_format_test.go` matches cpython output on the
covered specs. `json_dumps`, `nbody`, and `fannkuch` run to completion
under `bin/gopy` with `exit 0`.

**Estimated win.** Unblocks `json_dumps` (verified: `gopy
bench/bench_sources/json_dumps.py` exits 0; `gopy -c "import json;
print(json.dumps({'a':1,'b':[2,3.14]}))"` now prints `{"a": 1, "b":
[2, 3.14]}` instead of `<int object at 0x...>`). Also removes the
silent-format failures previously hiding in other stdlib paths that
caught `TypeError` from `format()` and fell back to `repr`.

**Out of scope (deferred to #647).** Per-slot `add_operators` generic
emission. P9 manually installs the four wrappers `pyperformance` and
`json` reach for. The rest of the slotdefs catalog (`__add__`,
`__sub__`, `__mul__`, etc.) is still missing on most builtin types
and lands as part of #647.

**Technical notes (findings worth keeping).**

- *The slot wiring was the gap, not the parser.* `format/format.go`
  already had a complete CPython-equivalent `ParseSpec`, `FormatInt`,
  and `FormatFloat`; they were exercised by `str.format` and
  f-strings via `objects/str_format.go`. `IntType.Format` and
  `FloatType.Format` were left at zero, so the protocol-level
  `Format()` helper fell through to `objectFormatDescr`, which
  rejects every non-empty spec. Wiring the three slots (int, bool,
  float) is the whole port.

- *Bool inherits int's slot, but only because we set it.* CPython's
  `inherit_slots` walks `tp_base` for built-in types and copies
  `tp_format` from `PyLong_Type` to `PyBool_Type`. gopy's type
  machinery does not walk the base chain for the `Format` slot on
  built-in types, so `BoolType.Format = intFormat` is the explicit
  mirror of that inheritance. Without it `'{:d}'.format(True)`
  rejected.

- *Float coercion for 'e'/'E'/'f'/'F'/'g'/'G'/'%'.* `int.__format__`
  with a float type code promotes through `PyNumber_Float` in
  CPython's `format_long_internal`; we mirror that with
  `bigIntToFloat64` + `format.FormatFloat`. The OverflowError path
  uses `math.IsInf` on the `big.Float -> float64` result because
  `big.Float`'s `Accuracy` flag is non-zero for ordinary rounding
  and is not a usable overflow signal.

- *The hidden second gap: slot-wrapper descriptors for `__repr__` /
  `__str__`.* After the Format slot wiring landed, `json.dumps` still
  emitted `<int object at 0x...>` and `<float object at 0x...>`.
  `json/encoder.py:_make_iterencode` captures
  `_intstr=int.__repr__` and `_floatstr=float.__repr__` as default
  parameter values at function-definition time, so it does not go
  through the runtime `tp_repr` slot. It does a dictionary lookup on
  the type and binds the resulting descriptor. CPython generates
  these descriptors automatically from `slotdefs` via
  `add_operators` in `Objects/typeobject.c`; gopy does not run that
  loop yet (task #647). The fix here installs the four wrappers
  manually (`intReprDescr` for int + bool, `floatReprDescr` for
  float). Once #647 lands the manual wiring deletes.

- *Why both `__repr__` and `__str__` get the same function.* CPython's
  `slotdefs` table maps `__repr__` to `tp_repr` and `__str__` to
  `tp_str`; for `int` and `float`, `tp_str` falls through to
  `tp_repr`, so the digit string is the same. Mirroring that with a
  single descriptor keeps the binding semantics consistent.

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
| P10.1 | `objects/float_fast.go`: singleton cache for `0.0`, `-0.0`, `+/-1.0`, `+/-Inf`, canonical `NaN`. `NewFloat` consults the cache first via bit-pattern compare; cache hits are alloc-free. Replaces the per-goroutine free-list design because Go's GC already amortises short-lived allocs cheaply and a true free list needs explicit Put hooks the VM doesn't surface yet. | DONE | objects/float_fast.go, objects/float.go |
| P10.2 | `BINARY_OP_ADD_FLOAT` / `SUBTRACT_FLOAT` / `MULTIPLY_FLOAT` / `TRUE_DIVIDE_FLOAT` fast path: result threads through `NewFloat`, which now hits the singleton cache when the result is 0/1/+/-Inf/NaN (loop terminators, division-by-self, etc.). In-place mutation deferred until refcount semantics ship. | DONE | objects/float_fast.go (cache wiring picks up the specialized arms automatically via NewFloat) |
| P10.3 | `_BINARY_OP_*_FLOAT` tier-2 uops hand-ported (depends on P2.2). | TODO | - |
| P10.4 | `float.__format__` audit + spec-parser share with P9. | DONE (covered by P9 closing) | spec 1712 P9 commits a5d25ea + 5512f4f |

**Gate.**

- `objects/float_fast_test.go`: `TestFloatSingletonsAreShared` asserts
  repeated `NewFloat(0)` / `NewFloat(1)` / etc. return identical
  pointers. `TestFloatNonCachedAllocates` confirms `NewFloat(2.5)`
  still allocates fresh objects (no false-positive cache hits).
  `TestFloatNonCanonicalNaNFallsThrough` asserts that a NaN with a
  non-canonical mantissa does not collapse into the singleton, so
  payload information from struct decoders / bit-twiddling code is
  preserved.
- `BenchmarkFloatNewZero` / `BenchmarkFloatNewOne`: **0 allocs**, ~1.8
  ns / op on Apple M4. `BenchmarkFloatNewArbitrary`: 1 alloc, 12.8 ns
  (same as the pre-change baseline, so the cache lookup is free for
  non-cached values).

**Estimated win.** 2.5x on float-heavy benchmarks (nbody, raytrace,
spectral_norm, scimark_*). Geomean ~1.3x. Full pool / in-place mutation
will close the remaining gap once refcount semantics ship.

**Technical notes (P10 float cache port).**

1. CPython's `Objects/floatobject.c:126` pulls a recycled `PyFloatObject`
   off `_Py_FREELIST_POP(PyFloatObject, floats)` before falling through
   to `PyObject_Malloc`. That's a per-thread cache with a `~100`-deep
   ring buffer. gopy can't implement the same shape because we can't
   know when a `*Float` is dead without explicit destruction hooks; Go's
   GC does the work asynchronously. So the gopy analogue is the
   singleton cache: keep the **values** that are reused most often
   (`0.0`, `1.0`, etc.) pinned in memory and share the pointer.
2. The cache uses `math.Float64bits` for the lookup so signed-zero and
   the canonical NaN bit-pattern match exactly. A `==` compare on
   `float64` would mishandle NaN (NaN != NaN) and would conflate
   `+0.0` with `-0.0`.
3. The singleton `*Float` objects are constructed via `newFloatRaw` in
   `init()` once. The split between `NewFloat` (cache-checking) and
   `newFloatRaw` (raw alloc) keeps `init()` from recursing on itself
   when the cache is being populated.
4. The canonical-NaN singleton only matches the value `math.NaN()`
   returns (mantissa `0x8000000000001`). Any other NaN payload falls
   through to `newFloatRaw` so callers that intentionally preserve a
   bit-pattern (struct decoders, codec parity) keep their data. This
   matches CPython's behaviour: PyFloat_FromDouble preserves the
   incoming bit pattern verbatim.
5. The full free-list port (P10.1 in the original plan) is the next
   step on this row, but it depends on refcount / liveness semantics
   the gopy VM does not yet expose. Once the tier-2 executor gets a
   "consume inputs" call (the same shape as CPython's
   `_PyFloat_FromDouble_ConsumeInputs`), the in-place reuse path
   becomes safe to wire and the alloc count on `BenchmarkFloatAddHot`
   drops to zero per op.

### P11. Compiler CFG optimizer + peephole — `Python/flowgraph.c`, `Python/compile.c`

**Audit.** Closed via spec 1716. `compile/flowgraph_cfg_passes.go`
hosts the four big passes plus peephole, ported 1:1 from
`Python/flowgraph.c`:

| CPython function                       | gopy site                                     |
|----------------------------------------|-----------------------------------------------|
| `_PyCfg_FromInstructionSequence`       | spec 1715 phase 2 (#657)                      |
| `_PyCfg_OptimizedCfgToInstructionSequence` | spec 1716 C.1 (#669)                      |
| `cfg_jump_thread`                      | `flowgraph_cfg_passes.go:2069-2080 cfgJumpThread` |
| `remove_unreachable_basic_blocks`      | `flowgraph_cfg_passes.go:476-513 cfgRemoveUnreachable` |
| `remove_redundant_jumps`               | `flowgraph_cfg_passes.go:449-474 cfgRemoveRedundantJumps` |
| `fold_const_binop`                     | `flowgraph_cfg_passes.go:1717-1764 basicblockFoldConstBinop` |
| `fold_const_unaryop`                   | `flowgraph_cfg_passes.go:1390-1420 basicblockFoldConstUnaryop` |
| `optimize_basic_block`                 | `flowgraph_cfg_passes.go:1444-1655 optimizeBasicBlockCFG` |
| `_PyCfg_OptimizeCodeUnit`              | `flowgraph_cfg_passes.go:2375-2412 cfgOptimizeCodeUnit` |

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P11.1 | `compile/flowgraph_cfg.go`: basic-block graph construction. Cite `Python/flowgraph.c:_PyCfg_FromInstructionSequence`. | DONE | spec 1715 phase 1 (#659) |
| P11.2 | Port the four big passes: jump threading, eliminate-after-terminator, fold-constant-jumps, prune-unreachable. | DONE | spec 1715 phase 3 (#656) + spec 1716 phase C.1 (#669) |
| P11.3 | Port the peephole table from `Python/flowgraph.c:optimize_basic_block`. | DONE | spec 1715 phase 3 (#656) |
| P11.4 | `dis.dis` integration: the optimizer pass runs before final emission via `cfgOptimizeCodeUnit`. | DONE | spec 1716 phase D (#672) |

**Gate.** `compile/flowgraph_cfg_passes_test.go` is table-driven
against cpython `Lib/test/test_peepholer.py` cases. The L1 codegen
+ L3/L4 assemble parity gates landed in spec 1716 phase E (#673).

**Estimated win.** 1.1-1.15x geomean (small but uniform). Already
realised.

### P12. Generator + coroutine fast path — `Python/genobject.c`

**Audit.** `objects/generator.go`, `objects/async_gen.go`,
`vm/eval_gen.go`, `vm/eval_resume.go`. gopy uses a goroutine +
channel model (one goroutine per generator body, channels for
`send` / yield), so the "per-send frame copy" cost the original
draft cited does not apply. The frame is owned by the generator's
goroutine; `send` is a channel write and a select, not a snapshot
restore.

CPython 3.14 reference: `Python/genobject.c:gen_send_ex2` (line
192), `gen_send_ex` (298), `gen_iternext` (630), `gen_throw`
(599), `gen_close` (387). gopy parity:

| CPython entry      | gopy site                                            |
|--------------------|------------------------------------------------------|
| `gen_send`         | `objects/generator.go:101-110 genSendMethod`         |
| `gen_iternext`     | `objects/generator.go:255 genIterNext`               |
| `gen_throw`        | `objects/generator.go:125-141 genThrowMethod`        |
| `gen_close`        | `objects/generator.go:143-156 genCloseMethod`        |
| `async_gen_anext`  | `objects/async_gen.go:58-72`                         |
| `async_gen_asend`  | `objects/async_gen.go:58-72`                         |
| `async_gen_athrow` | `objects/async_gen.go:58-72`                         |

GET_AITER / GET_ANEXT fast paths are already in place in
`vm/eval_gen.go`.

**Gap.**

- SEND opcode is not yet a tier-2 uop (gated on P2.3).
- Async-bench coverage is blocked first on the asyncio module port
  (spec 1711). Generator dispatch is not the dominant cost.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P12.1 | Generator/coroutine core (channel + goroutine model). Frame owned by goroutine, no per-send copy. | DONE | - |
| P12.2 | `SEND` opcode tier-2 uop. Gated on P2.3 (`Python/executor_cases.c.h` full port). | TODO | - |
| P12.3 | GET_AITER / GET_ANEXT / END_ASYNC_FOR fast path. | DONE | - |
| P12.4 | Coroutine suspend/resume via goroutine + channel swap. | DONE | - |

**Gate.** `objects/generator_test.go::BenchmarkGenSendHot` shows
≤2 allocations per send (Go runtime overhead for the channel
handoff). `generators` bench drops to under 5x cpython once tier-2
SEND lands.

**Estimated win.** Already realised for sync generators. Blocked
on asyncio (spec 1711) for async benches.

### P13. GC tracking + generational collector — `Python/gc.c`

**Audit.** `module/gc/` is substantially in tree (38 files). The
tracking machinery, the Python-facing API, and most introspection
helpers are ported:

| CPython entry            | gopy site                              |
|--------------------------|----------------------------------------|
| `PyObject_GC_RegisterFinalizer` | `module/gc/gc.go:27-34 RegisterFinalizer` |
| `PyObject_CallFinalizerFromDealloc` | `module/gc/gc.go:41-62 Finalize` |
| `_PyObject_GC_TRACK`     | `module/gc/gc.go:68-81 Track`          |
| `_PyObject_GC_UNTRACK`   | `module/gc/gc.go:89-101 Untrack`       |
| `_PyObject_GC_IS_TRACKED`| `module/gc/gc.go:106-111 IsTracked`    |
| `gc_collect_impl`        | `module/gc/module.go:92-112 gcCollect` (delegates to `runtime.GC()`) |
| `gc_enable_impl` / `gc_disable_impl` / `gc_isenabled_impl` | `module/gc/module.go:117-138` |
| `gc_get_threshold_impl` / `gc_set_threshold_impl` | `module/gc/module.go:143-182` (wired but not driving collections) |
| `gc_get_count_impl`      | `module/gc/module.go:187-197 gcGetCount` |
| `gc_is_tracked_impl`     | `module/gc/module.go:202-210 gcIsTracked` |
| `gc_get_objects_impl`    | `module/gc/module.go:215-236 gcGetObjects` |
| `gc_get_referrers_impl`  | `module/gc/module.go ~240+ gcGetReferrers` |
| `gc_get_referents_impl`  | `module/gc/module.go ~270+ gcGetReferents` |

State machine in `module/gc/state.go` (~250 LOC) carries a
3-generation counter but does not drive collections.

**Gap.**

- `gc.set_threshold(g0, g1, g2)` stores values but does not gate
  `runtime.GC()` invocations on threshold crossings.
- `gc.collect(generation)` delegates to `runtime.GC()` rather than
  walking the gopy gen-N lists.
- `__del__` ordering is Go GC traversal order, not CPython gen-N
  finalisation order.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P13.1 | Drive `gc.collect(generation)` and `gc.set_threshold(g0, g1, g2)` from `module/gc/state.go` generation counters. Trigger `runtime.GC()` only when gen-0 threshold crossed. Track gen-1/gen-2 promotions. | TODO | - |
| P13.2 | Python-level finalizer queue: order `__del__` calls by gc-generation. | TODO | - |
| P13.3 | Cycle detection for `__del__` resurrected objects. | TODO | - |

**Gate.** `module/gc/gc_test.go` mirrors cpython
`Lib/test/test_gc.py`. The `gc_collect` bench returns plausible
numbers (within 10x cpython; we can't beat Go's GC).

**Estimated win.** Low geomean impact (gc_collect alone). Mostly
unblocks the cpython test suite gc tests.

### P14. Native C-extension paths — `_pickle`, `_elementtree`, `_sqlite3`

**Audit.** Native-module reality (verified 2026-05-19):

| Module           | gopy directory                | Status                                   |
|------------------|-------------------------------|------------------------------------------|
| `_pickle`        | `module/_pickle/` does not exist | Absent. No pure-Python fallback either.  |
| `_elementtree`   | `module/_elementtree/`, `module/xml/` do not exist | Absent.                                  |
| `_sqlite3`       | `module/_sqlite3/` does not exist | Absent.                                  |
| `_csv`           | `module/_csv/` exists; `stdlib/csv.py` exists (19186 bytes) | Partial (pure-Python fallback in tree).  |

**Gap.**

- `pickle` / `unpickle` cannot run at all (no fallback to import).
- `xml_etree_*` cannot run (`xml.etree.ElementTree` requires
  `_elementtree`).
- `sqlite_synth` cannot run.
- `_csv` benchmarks run via the pure-Python fallback (~10x slower
  than the C `_csv` CPython uses by default).

CPython sources to port from:

| File                       | LOC   | Role                                  |
|----------------------------|------:|---------------------------------------|
| `Modules/_pickle.c`        |  8500 | Pickle protocol 5 encoder + decoder   |
| `Modules/_elementtree.c`   |  4000 | XML element tree                      |
| `Modules/_sqlite/`         |  6000 | sqlite3 connection/cursor             |
| `Modules/_csv.c`           |  1600 | C-native csv reader/writer            |

Critical pickle protocol-5 opcodes from `Modules/_pickle.c:107-137`:
`PROTO (0x80)`, `FRAME (0x95)`, `SHORT_BINUNICODE (0x8c)`,
`SHORT_BINBYTES (0x43)`, `STACK_GLOBAL (0x93)`,
`MEMOIZE (0x94)`, `BYTEARRAY8 (0x96)`.

**Phases.**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P14.1 | `module/_pickle/`: Go-native pickle protocol 5 encoder + decoder. Full port of `Modules/_pickle.c` (8500 LOC). | TODO | - |
| P14.2 | `module/_elementtree/`: thin wrapper over `encoding/xml` matching the cpython `_elementtree` API. Full port of `Modules/_elementtree.c` (4000 LOC). | TODO | - |
| P14.3 | `module/_sqlite3/`: cgo binding to libsqlite3 or pure Go via `modernc.org/sqlite`. Full port of `Modules/_sqlite/` (6000 LOC). | TODO | - |
| P14.4 | `module/_csv/`: Go-native csv reader/writer matching `Modules/_csv.c` (1600 LOC). | WIP | - |

**Gate.** `pickle` / `unpickle` benches drop to under 3x cpython.
`xml_etree_*` benches drop to under 5x.

**Estimated win.** Targeted; only the named benches. Critical
because three pyperformance benches are currently un-runnable.

### P15. Unicode writer + string concat — `Objects/unicodeobject.c`

**Audit.** Zero of CPython's 13 `_PyUnicodeWriter_*` functions
are ported (`Objects/unicodeobject.c:13737-14243`). gopy
concatenates strings via the Go `string + string` operator,
allocating per op. Format/join paths build intermediate strings.
There is no `objects/unicode_writer.go`.

Functions to port (with CPython line refs):

| CPython function                       | Line  | Role |
|----------------------------------------|------:|------|
| `_PyUnicodeWriter_Init`                | 13737 | init writer struct |
| `_PyUnicodeWriter_InitWithBuffer`      | 13794 | init from buffer |
| `_PyUnicodeWriter_Update`              | 13713 | internal update |
| `_PyUnicodeWriter_PrepareInternal`     | 13804 | pre-allocate buffer |
| `_PyUnicodeWriter_PrepareKindInternal` | 13882 | kind-aware prepare |
| `_PyUnicodeWriter_WriteCharInline`     | 13903 | inline single-char write |
| `_PyUnicodeWriter_WriteChar`           | 13914 | single-char write |
| `_PyUnicodeWriter_WriteStr`            | 13932 | write substring |
| `_PyUnicodeWriter_WriteSubstring`      | 14007 | write slice |
| `_PyUnicodeWriter_WriteASCIIString`    | 14063 | ASCII fast path |
| `_PyUnicodeWriter_WriteLatin1String`   | 14186 | Latin-1 fast path |
| `_PyUnicodeWriter_Finish`              | 14200 | finalise + return string |
| `_PyUnicodeWriter_Dealloc`             | 14243 | cleanup |

**Gap.**

- No `_PyUnicodeWriter` equivalent. `json_dumps`, `logging`,
  `mako`, `django_template` all hit this.
- `str.join` allocates the join separator slice per call.
- `%` formatting and `str.format` go through immutable concat.
- f-string codegen produces `FORMAT_VALUE` + `BUILD_STRING` which
  does N concats for an N-piece f-string.

**Phases.** P15.1 depends on P4.1 (kind detection) so the writer's
`Finish()` can pack into the right backing storage.

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P15.1 | `objects/unicode_writer.go`: pre-sized writer with kind-aware finalisation (matches P4). Port the 13 `_PyUnicodeWriter_*` functions in full. API: `WriteStr`, `WriteASCII`, `WriteRune`, `Finish() *Unicode`. | TODO | - |
| P15.2 | Re-route `str.join`, `str.format`, `%` formatting through the writer. Audit `objects/str_methods.go` + `objects/str_format.go`. | TODO | - |
| P15.3 | `BUILD_STRING` opcode lowering: emit a single `writer.Finish()` call instead of N concats. Touch `vm/eval_dispatch_gen.go`. | TODO | - |
| P15.4 | f-string codegen: in `compile/codegen.go`, lower an f-string's pieces directly into writer calls (skip `FORMAT_VALUE` + `BUILD_STRING`). Shares P9 spec-parser. | TODO | - |

**Gate.** `BenchmarkStrFormatHot` allocation-free for static format
strings. `json_dumps`, `logging`, `pprint` benches drop materially.

**Estimated win.** 2x on text-heavy benchmarks. Geomean ~1.2x.

## Checklist

| Subsystem                       | CPython source         | gopy destination          | Estimated win | Status | Commit |
|---------------------------------|------------------------|---------------------------|--------------:|--------|--------|
| P0. pyperformance harness       | n/a (tooling)          | `bench/`                  | n/a           | WIP    | ca0bef1 |
| P1. Specializer wire-up         | `Python/specialize.c`  | `specialize/`             | 6-10x         | WIP (P1.0-P1.3 done, P1.4-P1.6 open) | 67abc0a, 691c2d7, 71a9181, 6a8aace, 96130ac, 2f1f603 |
| P2. Tier-2 (full-file ports)    | `Python/optimizer_bytecodes.c`, `Python/executor_cases.c.h` | `optimizer/`, `vm/eval_uops_gen.go` | 1.5-2x | WIP (scaffolding + JIT gate hardcoded off) | -      |
| P3. PyLong fast path            | `Objects/longobject.c` | `objects/long_fast.go`    | 3x            | DONE (P3.1-P3.4; P3.5 deferred behind Int repr refactor) | `d9e16d2` |
| P4. PyUnicode kind tags         | `Objects/unicodeobject.c` | `objects/unicode_kind.go` | 2x         | TODO   | -      |
| P5. Dict open-addressing        | `Objects/dictobject.c` | `objects/dict.go` (extend) | 2x           | WIP (open-addressed layout already in tree, split-keys + watcher API + KnownHash gaps remain) | - |
| P6. Frame free-list + LOAD_FAST_CHECK | `Objects/frameobject.c`, `Python/ceval.c` | `vm/frame_pool.go`, `compile/flowgraph_cfg_locals.go`, `vm/eval_dispatch_handwritten.go` | 1.5x | WIP (P6.2 done via spec 1716; P6.1/P6.3/P6.4 open) | spec 1716 |
| P7. Type slot cache             | `Objects/typeobject.c` | `objects/type_slots.go`, `objects/type_inherit.go`, `objects/type_watcher.go` | 1.5x | WIP (P7.0 watcher API, P7.2 inherit_slots, P7.3 version invalidation, P7.4 single-load dispatch done; P7.1/P7.5 open) | `e94cf31`, `2d82694`, `d71cf26`, P7.4 this PR |
| P8. Aug-STORE_SUBSCR fix        | `Python/compile.c`     | `compile/codegen_stmt_misc.go:85-106` | unblock 2 N/A | DONE | `02f6c40` |
| P9. int.__format__ spec         | `Python/formatter_unicode.c` | `objects/long_format.go`, `objects/float_format.go`, `objects/int_bind.go`, `objects/float.go` | unblock 1 N/A | DONE | `a5d25ea`, `5512f4f` |
| P10. Float fast path            | `Objects/floatobject.c` | `objects/float_fast.go`  | 2.5x          | DONE (P10.1/P10.2/P10.4; P10.3 tier-2 uops gated on P2 expansion) | `96ce4d9` |
| P11. CFG optimizer + peephole   | `Python/flowgraph.c`   | `compile/flowgraph_cfg_passes.go` | 1.1x | DONE (spec 1716) | 9d7d9f0, 37563f5 |
| P12. Generator fast path        | `Python/genobject.c`   | `objects/generator.go`, `vm/eval_gen.go` | 3x async | DONE (channel + goroutine model); P12.2 SEND tier-2 uop depends on P2.3 | - |
| P13. GC tracking                | `Python/gc.c`          | `module/gc/`              | low geomean   | WIP (~90% done; thresholds + finalizer ordering pending) | - |
| P14. Native pickle/xml/sqlite   | `Modules/_pickle.c`, etc | `module/_pickle/`, etc  | bench-specific | TODO  | -      |
| P15. Unicode writer             | `Objects/unicodeobject.c` | `objects/unicode_writer.go` | 2x text  | TODO   | -      |

## Recommended ship order

Updated 2026-05-19 after the reality-check audit. Dependencies
matter: P1 inline caching is unsafe to extend until P5.4 watcher
API + P7.3 type-version auto-invalidation land, because today
nothing tells the specializer when a class attribute changes.

1. **P8 + P9 unblock N/A benches** (independent, small). `v[0] -= rhs`
   codegen fix and `int.__format__` spec parser. **DONE on PR #74.**
   `nbody`, `fannkuch`, and `json_dumps` all run to completion under
   `bin/gopy` (exit 0). Small-subset bench rerun on Apple M4 / macOS
   15.7.7 / go1.26.3 (2026-05-19):

   | Benchmark | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
   |---|---:|---:|---:|---:|---:|
   | `fannkuch` | 246.80 | 70.92 | 7115.49 | 28.83x | 100.33x |
   | `json_dumps` | 88.43 | 112.23 | 42941.82 | 485.60x | 382.62x |
   | `nbody` | 31.47 | 21.44 | 165.42 | 5.26x | 7.72x |
   | **geomean (these three)** | 88.23 | 55.47 | 3697.38 | 41.91x | 66.66x |

   The "ratio went from infinity to a number" is the win that
   matters for these three. Compressing the ratios further is
   downstream work (P1 specializer for `nbody` / `fannkuch`, P15
   unicode writer + P3 longs for `json_dumps`).
2. **P5.4 watcher API + P7.2 slot pre-population + P7.3 version
   invalidation** ship as one PR. This unblocks P1.4 deferred arms
   (`STORE_ATTR_INSTANCE_VALUE`, `STORE_ATTR_WITH_HINT`) and lets
   the specializer trust inline caches across calls. **DONE on PR
   #74.** P5.4 PyDict_Watch (`863d6fb`), P7.0 PyType_Watch
   (`e94cf31`), P7.3 type-version invalidation walks subclasses
   (`2d82694`), and P7.2 inherit_slots pre-population (`d71cf26`)
   all landed. P7.4 operator-dispatch single-load and P1.6
   specializer-time watcher install are the next remaining items.
3. **P1.4 closure**: emit the remaining LOAD_ATTR arms
   (`METHOD_WITH_VALUES`, `NONDESCRIPTOR_WITH_VALUES`,
   `METHOD_LAZY_DICT`, `GETATTRIBUTE_OVERRIDDEN`) once
   `Py_TPFLAGS_INLINE_VALUES` modelling lands; then ship the
   FOR_ITER / SEND / LOAD_SUPER_ATTR / CALL dispatch arms (P1.4b).
4. **P1.5 marshal persistence** so `.pyc` files retain the warm
   specializer state across runs.
5. **P2.1 open the JIT gate** (`interp.JIT = true`); validate
   trace projection fires. Then **P2.2 + P2.3 full-file ports of
   `Python/optimizer_bytecodes.c` and `Python/executor_cases.c.h`**,
   driven by the spec-1714 cases generator.
6. **P3 PyLong fast path** + **P10 float pool** ship in parallel
   (independent `objects/` work).
7. **P4 kind tags + P15 unicode writer** ship together (writer's
   `Finish()` depends on kind detection).
8. **P6.1 frame pool**, **P6.3 LOAD_FAST_BORROW / STORE_FAST_STORE_FAST**,
   **P6.4 args-tuple bypass** in parallel.
9. **P13 GC**, **P14 native modules** are bench-specific; pickle /
   xml / sqlite cannot run today so P14 is the priority among the
   three.

P0 and P11 are already closed (P0 small-subset, P11 entire CFG
optimizer). P12 core is closed; only P12.2 SEND tier-2 uop is
open, gated on P2.3.

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
3/8 slower) which matches the published PyPy 7.3 numbers and
confirms the JIT is doing its job.

gopy is at **283x cpython on geomean** across the five benches that
complete. That ratio compresses dramatically with P1 (specializer
wire-up) alone, since without P1 every adaptive opcode short-circuits
in `vm/adaptive.go:41/54/73`.

### Small subset, re-run 2026-05-19 (post spec 1715 + 1716 compile pipeline port)

_Captured: 2026-05-19 against `c012ba0` on branch
`feat/spec-1713-p7-pyc-writer`. Same host, same harness, same
warmups/runs as the 2026-05-16 snapshot. The intent of this re-run
was to baseline gopy after the cfg-builder bridge (1715) and the
full compile-pipeline port (1716) landed on top of the 2026-05-16
binary, so the next P1-P15 PR has an honest starting line._

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy | PyPy / cpython |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|---------------:|
| `call_method`     |             29.03 |          17.79 | 106905.78 |       3682.79x |    6008.47x |          0.61x |
| `fannkuch`        |            246.21 |          71.92 |       N/A |            N/A |         N/A |          0.29x |
| `json_dumps`      |             86.47 |         113.70 |       N/A |            N/A |         N/A |          1.31x |
| `nbody`           |             31.98 |          23.64 |       N/A |            N/A |         N/A |          0.74x |
| `pidigits`        |             33.46 |          28.99 |    117.33 |          3.51x |       4.05x |          0.87x |
| `regex_compile`   |             35.68 |         120.05 | 137260.51 |       3847.38x |    1143.39x |          3.37x |
| `richards`        |             34.55 |          26.21 |  94072.02 |       2723.00x |    3588.81x |          0.76x |
| `unpack_sequence` |             21.84 |          17.52 |  19278.36 |        882.57x |    1100.40x |          0.80x |
| **geomean**       |             45.32 |          39.13 |  19902.16 |        439.11x |     508.62x |          0.86x |

Trend vs 2026-05-16 baseline (`bench/baseline_v0124.json` is frozen
at the 2026-05-16 numbers, so `bench/compare-baseline` reports these
as regressions until we refresh it):

| Bench             | 2026-05-16 (ms) | 2026-05-19 (ms) | Delta    |
|-------------------|----------------:|----------------:|---------:|
| `pidigits`        |          289.97 |          117.33 |  -59.5%  |
| `richards`        |        81250.57 |        94072.02 |  +15.8%  |
| `call_method`     |        78043.22 |       106905.78 |  +37.0%  |
| `regex_compile`   |        80286.50 |       137260.51 |  +71.0%  |
| `unpack_sequence` |         6204.49 |        19278.36 | +210.7%  |

Takeaways:

- `pidigits` halved. That bench is GMP-shape arbitrary-precision int
  arithmetic, and the 1715 cfg-builder port collapsed several
  bytecode redundancies on the hot loop, exactly the shape where the
  flowgraph-level optimizer earns its keep.
- The other four regressed. The two big-ticket changes between
  2026-05-16 and 2026-05-19 are the cfg-builder bridge (1715) and
  the full Python/flowgraph.c + Python/assemble.c port (1716). Both
  paid for byte-equality parity with CPython (`.pyc` round-trip,
  L1-L4 gates green), not for execution speed. The CFG layer is
  doing strictly more work per compile (extra normalization passes,
  pseudo-jump rewriting, stackdepth recomputation), and the new
  layout is not yet feeding the VM any new fast paths because P1
  has not landed. So the regression is the bill for parity work
  that unblocks P1 / P2 inline-caching and tier-2 wire-up.
- `unpack_sequence` is the loudest regression (+211%). It is the
  bench most sensitive to per-call frame setup. Plausible
  attribution: the cfg-builder path now emits the CPython 3.14
  prologue (RESUME + extra MAKE_CELL housekeeping) where the old
  flat-sequence path skipped some of it, but the VM still walks
  every prologue op generically. Concrete number to chase once P6.1
  (frame pool) and P6.2 (LOAD_FAST_CHECK fast path) close.

This snapshot is the new "floor". The next P1-P7 PR must drag at
least three of these benches back below the 2026-05-16 baseline
column, or document why parity-driven cost is structural for that
PR's scope.

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
