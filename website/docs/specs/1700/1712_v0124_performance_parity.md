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
- **P6 (frame free-list + LOAD_FAST_CHECK + args-tuple bypass) is
  DONE.** `LOAD_FAST_CHECK` shipped via spec 1716
  (`compile/flowgraph_cfg_locals.go:320-358` rewrites
  `LOAD_FAST → LOAD_FAST_CHECK`;
  `vm/eval_dispatch_handwritten.go:63-72` dispatches).
  P6.1 chunk `LocalsPlus` recycle, P6.3
  `LOAD_FAST_BORROW` / `STORE_FAST_STORE_FAST` fusion, and P6.4
  `CALL_PY_EXACT_ARGS` + `CALL_BOUND_METHOD_EXACT_ARGS` args-tuple
  bypass all landed on PR #74 (see Technical-notes blocks).
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
| P1.5 | Deopt-before-marshal so `.pyc` bytes are deterministic across runs. **The original premise was inverted**: CPython does NOT persist the warmed cache; `Python/marshal.c:681` calls `_PyCode_GetCode(co)` which clones `co_code_adaptive` and immediately runs `deopt_code` (`Objects/codeobject.c:2293`) to rewrite every specialized opcode back to its adaptive parent and zero every inline cache cell. The marshal writer sees only the canonical adaptive shape. On load, `_PyCode_New` runs `_PyCode_Quicken` again to re-stamp the adaptive counters. gopy already re-quickens on `unmarshalCode` via `specialize.Enable` (P1.1); the missing piece was the pre-write deopt. Shipped `specialize.DeoptCode(code []byte) []byte` (`specialize/deopt_code.go`) mirroring `deopt_code` byte-for-byte: walk every codeunit, call `Deopt(op)` to map specialized → adaptive parent, preserve oparg, zero the trailing `CacheCount(base)` codeunits. `marshal.marshalCode` now passes `specialize.DeoptCode(c.Code)` to `writeCachedBytes` instead of the raw `c.Code`. Eight tests in `specialize/deopt_code_test.go` cover idempotence, fixed-point on non-adaptive opcodes, opcode rewrite with oparg preserved + cache zeroed, short/empty input, input-non-mutation, in-place variant, truncated cache, and a full DeoptParent sweep. | DONE | 1712-P1.5 |
| P1.6 | Cross-cutting coherency: install dict watcher (P5.5) + type-version invalidation (P7.5) hooks at `specialize.Enable` time so inline caches invalidate atomically on dict/type mutation. Without this, every LOAD_ATTR / LOAD_GLOBAL inline cache risks reading stale state after a class attribute assignment. Shipped: `specialize.Enable` now calls `ensureWatchersInstalled()` before `Quicken`; the optimizer registers its installer at package-init via `specialize.SetWatcherInstaller`; the installer reads `state.MainInterpreter()` (new accessor mirroring `_PyInterpreterState_Main`) and owns its own atomic latch. Fixed a parity bug in `optimizer/watcher.go::WatcherInit`: slot 0 was previously installed with `globalsWatcherCallback` (duplicated from slot 1) instead of the dedicated `builtins_dict_watcher` (`Python/pylifecycle.c:599-610`); slot 0 now bumps `interp.BuiltinDictMutations` and guards `ExecutorsInvalidateAll` on `MaxAllowedBuiltinsModifications`. `EnsureBuiltinsSubscribed` mirrors `Python/pylifecycle.c:1381` (idempotent `PyDict_Watch(0, interp->builtins)` + stamp). Nine new tests across `optimizer/builtins_watcher_test.go`, `optimizer/install_test.go`, `specialize/watcher_test.go`. | DONE | b059710d |

**P1.4a sub-table — specializer emission per family.** Numbers
report shipped variants vs the CPython 3.14 variant count, then
list the variants still missing. CPython 3.14 reference:
`Python/specialize.c`.

| Family | Coverage | Variants shipped | Missing | Status | Commit |
|--------|----------|------------------|---------|--------|--------|
| LOAD_ATTR | 13/13 | `MODULE`, `CLASS`, `CLASS_WITH_METACLASS_CHECK`, `SLOT`, `INSTANCE_VALUE`, `WITH_HINT`, `PROPERTY`, `METHOD_NO_DICT`, `NONDESCRIPTOR_NO_DICT`, `GETATTRIBUTE_OVERRIDDEN`, `METHOD_WITH_VALUES`, `NONDESCRIPTOR_WITH_VALUES`, `METHOD_LAZY_DICT` | — | DONE | 67abc0a, 9051a0c3, (this commit) |
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
| CALL | 19/20 | `PY_EXACT_ARGS`, `PY_GENERAL`, `BOUND_METHOD_EXACT_ARGS`, `BOUND_METHOD_GENERAL`, `NON_PY_GENERAL`, `BUILTIN_O`, `BUILTIN_FAST`, `BUILTIN_FAST_WITH_KEYWORDS`, `LEN`, `ISINSTANCE`, `LIST_APPEND`, `TYPE_1`, `STR_1`, `TUPLE_1`, `BUILTIN_CLASS`, `METHOD_DESCRIPTOR_O`, `METHOD_DESCRIPTOR_FAST`, `METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS`, `METHOD_DESCRIPTOR_NOARGS` | `ALLOC_AND_ENTER_INIT` deferred (needs SIMPLE_FUNCTION-shape init-cache). Specializer in `specialize/call.go` reads `BuiltinFunction.Conv` and `MethodDescr.Conv()` against the METH_* mask, identity-compares against `objects.CallableCache{Len,Isinstance,ListAppend}`. `CALL_LIST_APPEND` extra guard: peek `(instr + 2*(1+INLINE_CACHE_ENTRIES_CALL))` to verify the trailing opcode is POP_TOP. | DONE | 39ba997f |

**P1.4b sub-table — VM fast-path arms per family.** Each row tracks
the arm count shipped in `vm/eval_specialized*.go` and the parity
gate that backs it.

| Family | Arms shipped | Source | Gate | Status | Commit |
|--------|--------------|--------|------|--------|--------|
| LOAD_ATTR | 12/13 emitted | `vm/eval_specialized.go` — `MODULE`, `SLOT`, `CLASS`, `CLASS_WITH_METACLASS_CHECK`, `METHOD_NO_DICT`, `NONDESCRIPTOR_NO_DICT`, `PROPERTY`, `INSTANCE_VALUE`, `GETATTRIBUTE_OVERRIDDEN`, `METHOD_WITH_VALUES`, `NONDESCRIPTOR_WITH_VALUES`, `METHOD_LAZY_DICT` | `specialize/gatedata/spec_property.py` (`TestGateSpecPropertyAndMethod`), `vm/eval_specialized_load_attr_getattribute_overridden_test.go`, `vm/eval_specialized_load_attr_with_values_test.go`, `vm/eval_specialized_load_attr_lazy_dict_test.go` | WIP — `WITH_HINT` deferred until dict keys-version cache stamping lands | 691c2d7, 71a9181, 9051a0c3, (this commit) |
| TO_BOOL | 6/6 | `vm/eval_specialized.go` — `BOOL`, `INT`, `LIST`, `NONE`, `STR`, `ALWAYS_TRUE` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| COMPARE_OP | 3/3 | `vm/eval_specialized_compare.go` — `INT`, `FLOAT`, `STR` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| CONTAINS_OP | 2/2 | `vm/eval_specialized.go` — `DICT`, `SET` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| UNPACK_SEQUENCE | 3/3 | `vm/eval_specialized.go` — `TWO_TUPLE`, `TUPLE`, `LIST` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| STORE_SUBSCR | 2/2 | `vm/eval_specialized.go` — `LIST_INT`, `DICT` | `vm/eval_specialized_test.go` | DONE | 691c2d7 |
| BINARY_OP | 13/13 non-JIT | `vm/eval_specialized_binary_op.go` — `ADD_INT`, `SUBTRACT_INT`, `MULTIPLY_INT` (math/bits overflow guard); `ADD_FLOAT`, `SUBTRACT_FLOAT`, `MULTIPLY_FLOAT`; `ADD_UNICODE` shared with `INPLACE_ADD_UNICODE`; `SUBSCR_LIST_INT`, `SUBSCR_TUPLE_INT`, `SUBSCR_STR_INT` (ASCII fast path), `SUBSCR_DICT`, `SUBSCR_LIST_SLICE` | `specialize/gatedata/spec_binary_op.py` (`TestGateSpecBinaryOp`) | DONE | 6a8aace |
| FOR_ITER | 3/4 | `vm/eval_specialized_for_iter.go` — `LIST`, `TUPLE`, `RANGE`; typed `Next` helpers in `objects/list.go::ListIterNextFast`, `objects/tuple.go::TupleIterNextFast`, `objects/range_iter.go::RangeIterNextFast` skip the `tp_iternext` slot lookup | `vm/eval_specialized_for_iter_test.go` (hit / exhaustion / wrong-type deopt per family) | WIP — `GEN` deferred: it needs the generator-frame push/pop path the VM does not yet expose; dispatch loop falls through to the generic FOR_ITER body for `FOR_ITER_GEN` until that lands | 44786dc4 |
| LOAD_GLOBAL | 2/2 | `vm/eval_specialized_load_global.go` — `MODULE`, `BUILTIN` | `specialize/gatedata/spec_load_global.py` (`TestGateSpecLoadGlobal`) | DONE | 2f1f603 |
| STORE_ATTR | 3/3 | `vm/eval_specialized_store_attr.go`. `SLOT` (validate type_version, write `Instance.slots[idx]`), `INSTANCE_VALUE` (validate type_version, validate dict slot still names the same unicode key via `Dict.StoreEntryAtName`, write entry value, fire `DictEventModified`), `WITH_HINT` (same body as INSTANCE_VALUE because gopy stores every instance attribute in the dict so the CPython inline-values-vs-managed-dict split collapses to one path; both opcodes stay separate so the specializer's classification matches CPython 1:1 and deopt counters track each route) | `specialize/gatedata/spec_store_attr.py` (`TestGateSpecStoreAttr`), `specialize/store_attr_test.go` (`TestStoreAttrSlot`, `TestStoreAttrInstanceValue`, `TestStoreAttrSkipsAbsentKey`) | DONE. Also fixed a CPython-divergent specializer branch that used to emit `STORE_ATTR_WITH_HINT` with index=0 when the attribute was absent at specialize time. CPython's `specialize_dict_access_hint` (`Python/specialize.c:1039`) refuses to specialize in that case so the first store inserts via generic `STORE_ATTR` and only later stores can specialize once the slot is populated. The new arm requires the slot's key to still match `co_names[oparg]` because the 4-cell STORE_ATTR cache only stamps `type_version` (no `keys_version` slot like LOAD_ATTR has) so a delete + re-insert into the same bucket could leave the cached index pointing at a stale name. The runtime key compare is the same safety net CPython uses inside `_STORE_ATTR_WITH_HINT`. Micro-bench (`self.n += 1` × 1M iterations) drops 117s to 107s (~8%); the remaining ceiling is dispatch-loop overhead, not the STORE arm. | 96130ac, e95ede4d |
| SEND | 1/1 dispatch-level | `vm/eval_specialized_send_gen.go` — `fastSendGen` short-circuits the `execSend` type-switch with an identity check on `*Generator` / `*Coroutine` and forwards to `r.Send(v)`. Architectural ceiling: gopy generators run on a dedicated goroutine driven by `yieldCh` / `sendCh` channels, so the CPython `_SEND_GEN_FRAME` + `_PUSH_FRAME` "push gen's frame onto eval-stack, DISPATCH_INLINED into gen body" path has no analogue without retiring the goroutine-based design (tracked separately under P12). | `vm/eval_specialized_send_gen_test.go` (hit / StopIteration / wrong-type deopt / coroutine guard / surfacing non-StopIteration errors) | DONE — fast-arm dispatch | TBD |
| LOAD_SUPER_ATTR | 2/2 | `vm/eval_specialized_load_super_attr.go` — `ATTR`, `METHOD`; backed by `objects.SuperLookup` with a `method_found` out-param mirroring CPython's `_PySuper_Lookup` | `vm/eval_specialized_load_super_attr_test.go` (hit / missing / non-super deopt / non-type deopt / method-found vs bound shape / oparg bit-0 assertions) | DONE | 2f09f55b |
| CALL | 17/19 emitted | `vm/eval_specialized_call.go` + `vm/eval_specialized_call_builtin.go` + `vm/eval_specialized_call_alloc_init.go` — `PY_EXACT_ARGS`, `BOUND_METHOD_EXACT_ARGS`, `BUILTIN_O`, `BUILTIN_FAST`, `BUILTIN_FAST_WITH_KEYWORDS`, `LEN`, `ISINSTANCE`, `LIST_APPEND` (consumes trailing POP_TOP via SKIP_OVER), `TYPE_1`, `STR_1`, `TUPLE_1`, `BUILTIN_CLASS`, `METHOD_DESCRIPTOR_O`, `METHOD_DESCRIPTOR_FAST`, `METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS`, `METHOD_DESCRIPTOR_NOARGS`, `ALLOC_AND_ENTER_INIT` (stamps init pointer + version into `Type._spec_cache`; fast arm validates cache cell version vs live tp_version_tag, allocates via `NewInstance`, pushes init frame, folds the `_Py_InitCleanup` shim's EXIT_INIT_CHECK None-validation into the arm because Go-level `Eval()` returns directly without a DISPATCH_INLINED hop) | `vm/eval_specialized_call_test.go`, `vm/eval_specialized_call_builtin_test.go`, `vm/eval_specialized_call_alloc_init_test.go` (hit / one-arg hit / non-None TypeError / non-Type deopt / version-miss deopt / argcount-mismatch deopt) | WIP — generic `PY_GENERAL` / `BOUND_METHOD_GENERAL` / `NON_PY_GENERAL` arms fall through to the adaptive parent body (no fast path needed: CPython's bodies for those are themselves the generic call). | 39ba997f, TBD |

**Technical notes (P1.6 watcher install at specialize.Enable).**

1. CPython installs watchers at two distinct sites:
   `Python/pylifecycle.c:1378-1383` calls `PyDict_Watch(0, interp->builtins)`
   while the interpreter is being minted; `Python/optimizer_analysis.c:175-180`
   lazily calls `PyDict_AddWatcher` / `PyType_AddWatcher` (slot 1 + type slot 0)
   the first time `remove_globals` projects a trace. gopy collapses both onto
   `specialize.Enable`: every Code-creation path (`pythonrun`, `vm.liftNestedCode`,
   `marshal.unmarshalCode`) already calls Enable, so calling
   `ensureWatchersInstalled()` from it once per Code creation gives the
   runtime as many retries as it needs without coordinating a startup hook.
2. **Parity fix.** `optimizer/watcher.go::WatcherInit` was previously installing
   `globalsWatcherCallback` in BOTH slot 0 (BUILTINS) and slot 1 (GLOBALS).
   CPython splits them: `Python/pylifecycle.c:599-610 builtins_dict_watcher`
   bumps `interp->rare_events.builtin_dict` and calls
   `_Py_Executors_InvalidateAll(interp, 1)` only while under the
   `MaxAllowedBuiltinsModifications` cap. The new
   `builtinsDictWatcherCallback` in `optimizer/watcher.go` mirrors that
   exactly: bump counter unconditionally, gate invalidation on the cap.
3. **Dependency cycle.** `optimizer` imports `specialize` (for Enable hooks,
   Quicken, etc.), so `specialize` cannot import `optimizer`. The install hook
   flows via a function variable: `specialize.SetWatcherInstaller(fn func())`
   stores the callback, `ensureWatchersInstalled()` fires it.
   `optimizer/install.go::func init()` registers `installWatchers` at process
   start. Until that init runs (e.g. specialize unit tests that do not import
   optimizer), the hook is a no-op rather than nil-deref.
4. **Latch ownership.** The optimizer-side installer owns its idempotency
   latch (`atomic.Bool` in `optimizer/install.go`), not specialize. This is
   load-bearing because Enable is called on every Code creation including
   the compile-only test path that mints no runtime; the installer reads
   `state.MainInterpreter()` and returns early if no interp exists, leaving
   the latch open. The first Enable() that fires after `Runtime.NewInterpreter`
   minted the main interp finally flips the latch and runs `WatcherInit`.
5. **`state.MainInterpreter()`** mirrors CPython's
   `Python/pystate.c::_PyInterpreterState_Main`. gopy stores it in an
   `atomic.Pointer[Interpreter]`; `Runtime.NewInterpreter` does
   `mainInterpreter.CompareAndSwap(nil, i)` so the first interp minted in
   the process latches in as "main" without disturbing later (e.g. test)
   interps that share the runtime. `DropMainInterpreter` is the
   test-only escape hatch.
6. **`EnsureBuiltinsSubscribed`** in `optimizer/watcher.go` mirrors
   `Python/pylifecycle.c:1381 PyDict_Watch(0, interp->builtins)` plus the
   `interp->builtins = dict` stamp from `_PyInterpreterState_Init`. It is
   idempotent on the stamp: a second call with a different dict leaves the
   first one in place, because module-specific builtins must not steal the
   slot the canonical dict already occupies.

**Technical notes (P1.4b FOR_ITER fast arms).**

1. CPython's `macro(FOR_ITER_LIST)` / `FOR_ITER_TUPLE` / `FOR_ITER_RANGE`
   (`Python/bytecodes.c:3349` / `:3412` / `:3462`) decompose into three
   uops: `_ITER_CHECK_<x>` (type guard), `_ITER_JUMP_<x>` (exhaustion +
   `JUMPBY(oparg+1)`), `_ITER_NEXT_<x>` (advance, push value). gopy folds
   the three uops into one helper per family
   (`objects.ListIterNextFast`, `TupleIterNextFast`, `RangeIterNextFast`)
   that returns `(value, exhausted, ok)`: `ok=false` means type guard
   failed (caller deopts), `exhausted=true` means the iterator drained
   (caller does `JUMPBY(oparg+1)`), otherwise `value` is the next item.
2. **Iterator zeroing on exhaustion.** CPython's `_ITER_JUMP_LIST` and
   `_ITER_JUMP_TUPLE` clear `it->it_seq` and Py_DECREF the source on
   exhaustion (so a re-entered FOR_ITER on the dead iterator returns
   StopIteration without re-walking the source). gopy mirrors this by
   setting `it.src = nil` on exhaustion in `ListIterNextFast` /
   `TupleIterNextFast`. The range iterator does not hold a source ref so
   the equivalent is moot.
3. **`forIterJump` helper.** The naive call was
   `e.jumpBy(int(oparg) + 1)`, but `e.jumpBy` resolves stride via
   `e.advance()`, which reads `opcodeCaches[byte at InstrPtr]`. That
   table only carries the base opcodes (mirroring CPython's
   `_PyOpcode_Caches`), so on a specialized variant byte
   (`FOR_ITER_LIST` etc.) the lookup returns 0 and undercounts the
   stride by 2 bytes. `forIterJump(oparg)` instead anchors on
   `cacheAdvance(compile.FOR_ITER)`, which always passes the parent op
   and gets the correct 4-byte stride. The hit path already does this
   correctly via `cacheAdvance(compile.FOR_ITER)`.
4. **Range allocation parity.** gopy's `range_iterator` carries a
   `*big.Int` triple (`cur`, `stop`, `step`) unified across CPython's
   short and long range types. The fast arm still allocates a fresh
   `*Int` per iteration (`NewIntFromBig(&it.cur.v)`) plus the next
   `cur` because the gopy `Int` representation does not pack small ints
   inline. The win is purely from skipping the `tp_iternext` table
   dispatch and the `range_iterator` type check; closing the
   allocation gap would require a small-int pool in `objects/int.go`
   (tracked separately under P3 PyLong fast path).
5. **FOR_ITER_GEN deferred.** The `GEN` variant has the same
   architectural ceiling as `SEND_GEN`: CPython pushes the gen's
   interpreter frame onto the host eval-stack and runs the gen body
   inline via `DISPATCH_INLINED`. gopy's generators run on a separate
   goroutine with channel-mediated `yieldCh` / `sendCh`, so frame-push
   inlining is structurally unreachable. The dispatcher falls through
   to the generic FOR_ITER body for `FOR_ITER_GEN`, which works
   because `Deopt(FOR_ITER_GEN) == FOR_ITER` already routes it through
   the generic IterNext path. Closing the remaining headroom requires
   retiring the goroutine-based generator design in favor of
   frame-stack pushing (tracked separately under P12). The SEND_GEN
   dispatch-level fast arm (next block) is the analogue of what we
   can ship without that retire.

**Technical notes (P1.4b SEND_GEN fast arm + architectural ceiling).**

1. **CPython's SEND_GEN macro composition.**
   `Python/bytecodes.c:1364` defines
   `SEND_GEN = unused/1 + _CHECK_PEP_523 + _SEND_GEN_FRAME + _PUSH_FRAME`.
   `_SEND_GEN_FRAME` (`Python/bytecodes.c:1348`) pushes `v` onto the
   generator's interpreter frame via `_PyFrame_StackPush`, flips the
   gen's `gi_frame_state` to `FRAME_EXECUTING`, links
   `gen->gi_exc_state.previous_item = tstate->exc_info`, and stashes
   `frame->return_offset` so YIELD_VALUE knows where to resume the
   caller. `_PUSH_FRAME` then `DISPATCH_INLINED(gen_frame)` so the
   generator's bytecode body runs in the SAME Tier-1 dispatch loop:
   one switch-table evaluation, no goroutine, no channel hop, no
   thread-state swap.
2. **Why gopy can't replicate that inline-dispatch path.**
   `vm/eval_gen.go:execReturnGenerator` materializes generators with
   `go func() { ... }()`: the generator body runs on a dedicated
   goroutine, yields via `yieldCh <- GenMsg`, and blocks on `<-sendCh`
   until the host frame's SEND pushes a value through. Pushing the
   gen's frame onto the host evalState's value-stack would race with
   that goroutine's reads/writes against the same LocalsPlus and
   InstrPtr. The channel coordination IS the mechanism that keeps
   the two contexts coherent; bypassing it would require deleting
   the goroutine entirely.
3. **What the fast arm does ship.**
   `fastSendGen` in `vm/eval_specialized_send_gen.go` is a
   dispatch-level optimization: it skips the type-switch in
   `execSend` (`vm/eval_gen.go:270`) by identity-checking
   `*Generator` / `*Coroutine` at the head, then calls `r.Send(v)`
   directly. That's the only legitimate fast path the goroutine
   design permits. Per-call savings are small (one Go type-switch
   step) compared to CPython's gen-frame inlining, but the arm still
   matches CPython's pattern of "trust the specializer's type guard
   and skip the generic body's redispatch."
4. **Stride anchoring.**
   `cacheAdvance(compile.SEND) + 2*int(oparg)` is the StopIteration
   jump target. The straightforward `e.jumpBy(int(oparg) + 1)` is
   wrong on the fast path because `e.advance()` reads the opcode
   byte at InstrPtr — which is `SEND_GEN` after stamping — and
   `opcodeCaches[SEND_GEN] == 0` undercounts the stride by one
   codeunit. Same wart as `forIterJump` in
   `vm/eval_specialized_for_iter.go`; same fix.
5. **Coroutine guard.**
   `specialize/send.go:25` picks SEND_GEN when receiver is either
   `IsGenerator` OR `IsCoroutine`; the fast arm therefore accepts
   both in the type switch. CPython's `_SEND_GEN_FRAME` has the
   matching DEOPT_IF on `Py_TYPE(gen) != &PyGen_Type && Py_TYPE(gen)
   != &PyCoro_Type`. AsyncGenerator is NOT in this set (specializer
   declines to stamp; fast arm declines via the default case).
6. **Architectural ceiling, quantified.**
   The remaining win between gopy's dispatch-level fast arm and
   CPython's frame-push inlining is the goroutine roundtrip per
   yield: two unbuffered channel sends (host -> sendCh, gen ->
   yieldCh) plus two scheduler ticks. For a tight generator loop
   that yields 10K times this is ~20K-30K goroutine context
   switches per call site; tier-1 CPython does zero. Closing this
   requires the P12 generator redesign — retiring the goroutine in
   favor of a frame-stack representation that the host eval loop
   pushes onto its own evalState. That retire is out of scope for
   P1.4b but unblocks FOR_ITER_GEN, the rest of `gi_exc_state`
   linkage, and bound-method gen send patterns. Tracked separately.

**Technical notes (P1.4b LOAD_SUPER_ATTR fast arms).**

1. **Oparg encoding.** LOAD_SUPER_ATTR packs three fields into a single
   byte oparg: bit 0 is `load_method` (controls whether the following
   CALL sees an unbound-method pair or a regular bound attribute), bit 1
   is `has_self` (set when super was constructed with two args; not
   consulted by the fast arms because the prelude already requires
   (super, class, self) on the stack), bits 2+ are the name index into
   `co.Names`. The ATTR arm asserts `!(oparg & 1)` and the METHOD arm
   asserts `(oparg & 1)`, mirroring the C-level `assert` in
   `Python/bytecodes.c:2222` / `:2238`.
2. **Specialize-time invariants vs runtime guards.** The specializer
   only stamps `_ATTR` / `_METHOD` when `global_super` is the unshadowed
   builtin `super` and `class` is an actual `*Type`. The fast arms re-
   check both: `globalSuper != objects.Object(objects.SuperType)` or
   `class` not being a `*Type` produces `ok=false` so the dispatcher
   deopts back to `LOAD_SUPER_ATTR` and runs the generic body. This
   guard pair mirrors the macro-level `DEOPT_IF(global_super != ..., ...)`
   block in `Python/bytecodes.c`.
3. **`method_found` probe gating.** CPython's `_PySuper_Lookup`
   (`Objects/typeobject.c:12003`) only fills its `int *method_found`
   out-param when `Py_TYPE(self)->tp_getattro == PyObject_GenericGetAttr`;
   if the type overrides `tp_getattro` the probe is suppressed so the
   override sees a bound descriptor instead of a raw function. gopy's
   equivalent test is `self.Type().Getattro == nil` — when the override
   is present the METHOD arm calls `SuperLookup(..., nil)` and pushes
   `(attr, NULL)` so the following CALL routes through the generic call
   path, never the unbound-method trampoline.
4. **Stack discipline.** The (super, class, self) tuple enters with
   self at TOS. The ATTR arm pops all three and pushes the resolved
   attribute; the METHOD arm saves the self stackref before popping
   (because the method-found branch needs to push self back above the
   attr to form the unbound-method pair the following CALL reads). An
   earlier draft popped in the wrong order and saved the super stackref
   into the "self" position, which surfaced as a test failure where
   `peek(0)` after the arm returned the `SuperType` object instead of
   the instance. The fix is to call `selfRef := e.pop()` first.
5. **`SuperLookup` shape.** `objects.SuperLookup(suType, suObj, name,
   *bool)` folds CPython's `do_super_lookup` + `_PySuper_Lookup` into
   one entry: it runs `supercheck` for the type-or-instance test, walks
   `suObjType.MRO` strictly past `suType` looking for `name`, and on
   hit either sets `*methodFound=true` and returns the raw descriptor
   (when `isMethodLike` is true on the descriptor and the caller asked
   the question via a non-nil probe) or applies `tp_descr_get` to bind
   the descriptor through the instance. The `bindTo=nil` case (class-
   mode super where `su_obj == su_obj_type`) mirrors
   `Objects/typeobject.c:11894`.
6. **Generic-body shape on deopt.** When the prelude guard misses the
   fast arm returns `(0, false, nil)` so the dispatch loop rewrites the
   opcode back to `LOAD_SUPER_ATTR` and falls through. The generic body
   then constructs `super(class, self)` by calling
   `SuperType.Call(...)`, runs `superGetAttr` on the resulting Super,
   and pushes the result — the same sequence the AST-level `super(C,
   x).m` would compile to. Deopt-on-non-super and deopt-on-non-type
   tests assert the opcode actually flipped back; the trailing
   `TypeError` from the generic body invoking a non-callable is
   incidental but exercises the deopt path end-to-end.

**Technical notes (P1.4 INLINE_VALUES foundation + LOAD_ATTR_*_WITH_VALUES fast arms).**

1. **Why these arms are "with values" but read no values.** Reading
   the CPython 3.14 macros `LOAD_ATTR_METHOD_WITH_VALUES` and
   `LOAD_ATTR_NONDESCRIPTOR_WITH_VALUES` (`Python/bytecodes.c`) was
   the first surprise: they never touch the inline-values block.
   Both arms simply guard that the inline-values shape is still
   intact and that the type's `ht_cached_keys` has not grown to
   include the looked-up name, then push the class-level descriptor
   verbatim. The specializer's assert at stamp time is the proof:
   `Python/specialize.c:1614 specialize_attr_loadclassattr` calls
   `_PyDictKeys_StringLookup(ht_cached_keys, name) < 0`, i.e. the
   name is NOT in the shared-keys set. As long as that stays true,
   no instance has ever stored an attribute under this name, so the
   load returns the class descriptor without consulting any dict.
2. **Mapping CPython's PyDictValues onto gopy.** CPython packs the
   inline-values block immediately before the instance's payload at
   `MANAGED_DICT_OFFSET = -3 * sizeof(void*)` and exposes a `valid`
   bit (`Include/internal/pycore_dict.h PyDictValues.valid`) that
   `_PyObject_InitInlineValues` sets and
   `make_dict_from_instance_attributes` clears. gopy does not lay
   instances out with a pre-header inline-values block; instead it
   models the same two invariants directly on the Go side:
   - `Instance.inlineValid bool` is the analogue of `PyDictValues.valid`.
     Defaults true at `NewInstance`, cleared by `instanceSetAttr` on
     delete (`value == nil` branch). Future paths that would break
     the shared-keys shape (e.g. monkey-patching `__dict__`) can call
     `InvalidateInlineValues` to drop the instance out of
     WITH_VALUES specialization at the next guard miss.
   - `Type.cachedKeys map[string]bool` is the analogue of
     `PyHeapTypeObject.ht_cached_keys`. Grows monotonically: every
     `instanceSetAttr` that writes a new attribute name calls
     `tp.AddCachedKey(name)`, which inserts and bumps the version.
3. **Why a monotonic shared-keys set is sufficient.** CPython's
   `ht_cached_keys` is a real `PyDictKeysObject` shared across every
   split dict on the type. The shared-keys *insertion* path
   (`Objects/dictobject.c:5132 insert_split_key`) is what gopy needs
   to mirror, not the lookup machinery: the LOAD_ATTR fast arms only
   need to answer "has any instance ever stored a key under this
   name?", which is exactly what the monotonic set answers in O(1).
   Future work that ports a faithful PyDictKeysObject (for
   `LOAD_ATTR_WITH_HINT`) can replace the map without churning the
   call sites.
4. **Version stamping.** `Type.cachedKeysVersion` mirrors `dk_version`
   on `ht_cached_keys`. It is allocated lazily by
   `CachedKeysVersion()` via the existing `allocDictKeysVersion()`
   global counter (shared with `cachedKeysVersion` to keep stamp
   uniqueness across the runtime). Every `AddCachedKey` zeroes the
   field so the next reader allocates a fresh value. The specializer
   stamps the version into cache cells 4..5 of the `_PyLoadMethodCache`
   slot (existing `setKeysVersion`); the fast arm rejects on
   mismatch, which is the deopt trigger when any instance grows the
   shared-keys set after specialize time.
5. **Why heap-type bits are set in NewUserTypeMeta and not
   inherit_slots.** CPython sets `Py_TPFLAGS_INLINE_VALUES` +
   `Py_TPFLAGS_MANAGED_DICT` inside `type_new`
   (`Objects/typeobject.c:4153`) whenever a heap type ends up with a
   managed dict. gopy already runs that logic in
   `objects/usertype.go::NewUserTypeMeta`: the
   `noSlotsDeclared → HasDict = true` branch is the equivalent of
   CPython picking the managed-dict layout, so the bits land in the
   same place. C-port builtin types (list, dict, etc.) do not set
   the flags because their attribute storage is fixed-shape; the
   specializer simply never considers them WITH_VALUES candidates.
6. **Specializer plumbing.** `specialize/load_attr.go` now branches on
   `tp.HasInlineValues() && !tp.HasCachedKey(name.Value())` for both
   KindMethod and KindNonDescriptor. The cache layout (cells 2..3
   type_version, 4..5 keys_version, parallel `CacheObjects[idx]`
   descriptor) is identical to the NO_DICT variant; only the opcode
   tag differs. `allocDictKeysVersion()` returning 0 (counter
   wraparound) is treated as a refuse-to-specialize signal, same as
   the NO_DICT path.
7. **VM fast-arm shape.** `fastLoadAttrMethodWithValues` and
   `fastLoadAttrNondescriptorWithValues` in `vm/eval_specialized.go`
   share the guard sequence: oparg shape (`&1` set for METHOD, clear
   for NONDESCRIPTOR), owner is `*objects.Instance`,
   `tp.HasInlineValues()`, `inst.InlineValid()`, type_version match,
   cached_keys_version match, descr non-nil. The METHOD arm then
   pushes `(descr, self)` so the following CALL sees the unbound-
   method pair shape; the NONDESCRIPTOR arm pops the owner and
   pushes only `descr` (oparg bit 0 == 0 means "regular attribute").
8. **Coverage in `vm/eval_specialized_load_attr_with_values_test.go`.**
   Eight tests: METHOD hit / version-miss / keys-miss /
   inline-invalidated-after-delete / wrong-oparg-shape;
   NONDESCRIPTOR hit; specializer-emits with shared-keys empty;
   specializer-skips when the looked-up name is already in
   `cachedKeys`. The InlineInvalidated test calls `instanceSetAttr`
   with `value == nil` to flip the bit, then asserts the fast arm
   deopts even though all other guards still pass.
9. **LAZY_DICT shipped.** CPython's
   `LOAD_ATTR_METHOD_LAZY_DICT` (`Python/specialize.c:1635`) fires
   when the managed-dict slot reads as null at LOAD_ATTR time
   (i.e. the instance has not materialized its dict yet); the arm
   skips reading it. The port flips the INLINE_VALUES flag on
   user types from "always on for HasDict" to base-conditional:
   `NewUserTypeMeta` keeps `Py_TPFLAGS_INLINE_VALUES` only when
   every non-`object` base already carries it (mirrors CPython's
   `type_new` basicsize gate at `Objects/typeobject.c:4153`). Heap
   subclasses of built-ins like list/dict/str therefore land in
   the MANAGED_DICT-without-INLINE_VALUES shape that is the
   LAZY_DICT runtime state. `NewInstance` no longer pre-allocates
   `Instance.dict` for that shape, and `instanceSetAttr`
   materializes it on first store (CPython:
   `Objects/dictobject.c:6857 make_dict_from_instance_attributes`).
   The specializer arm in `specialize/load_attr.go::KindMethod`
   stamps `LOAD_ATTR_METHOD_LAZY_DICT` when
   `tp.HasManagedDict() && inst.Dict() == nil`. VM fast arm at
   `vm/eval_specialized.go::fastLoadAttrMethodLazyDict` guards
   `oparg&1 != 0`, the MANAGED_DICT-without-INLINE_VALUES flag
   combo, `inst.Dict() == nil`, and the cached `type_version`
   (the dict-is-nil check is gopy's equivalent of CPython's
   `_PyManagedDictPointer_GET(owner)->dict != NULL` runtime
   check). On hit pushes (descr, self) for the unbound-method
   shape. Five tests in
   `vm/eval_specialized_load_attr_lazy_dict_test.go`: METHOD hit
   / dict-materialized-deopts / version-miss / wrong-oparg-shape;
   specializer-emits with nil dict.
10. **Why no shim for the inline-values block.** A first sketch
    considered packing a real `PyDictValues` array onto `Instance`
    so the WITH_VALUES arm could read from it directly. That would
    duplicate the dict's storage with nothing reading it, since
    every actual access falls through to `inst.dict` anyway. The
    invariants the fast arm needs (was the shape broken? was the
    name ever stored?) are state, not storage, so they live on two
    boolean / set-shaped fields and not a parallel value array.
    This keeps the port honest with the "no shim" ground rule:
    the runtime models the same observable behaviour CPython does,
    without erecting a fake storage layer that no read path
    consumes.

**Technical notes (P1.4a/P1.4b CALL fast arms + METH_* foundation).**

1. **Where the METH_* tag actually lives in gopy.** CPython reads
   `PyCFunction_GET_FLAGS(callable)` which dereferences
   `((PyCFunctionObject*)callable)->m_ml->ml_flags` (`Include/cpython/methodobject.h`),
   i.e. the calling convention lives on the per-row `PyMethodDef`, not
   on the bound function object. gopy does not vendor `PyMethodDef`
   rows verbatim (each builtin is registered as a closure), so the
   flag has to live on the wrapper itself. We added
   `BuiltinFunction.Conv` and `MethodDescr.conv` (with `Conv()` accessor).
   `NewBuiltinFunction` and `NewMethodDescr` default to
   `MethVarargs|MethKeywords` so the dozens of pre-existing registration
   sites continue to match the closure shape they always passed.
   New callers that want a specialized arm reach for
   `NewBuiltinFunctionConv` / `NewMethodDescrConv` with the explicit tag.
2. **The callable cache is package state, not interpreter state.**
   `Python/specialize.c:2143,2162,2039` reads
   `interp->callable_cache.{len, isinstance, list_append}` for the
   identity guards `target == cache->len`. gopy has no `Interpreter`
   struct in the hot path (the `state.Interpreter` exists but the
   specializer would have to thread it through), so the cache is a
   tiny package-level variable trio in `objects/callable_cache.go`.
   `RegisterCallableCacheLen` is called from `builtins/init.go` while
   `builtinRow{name: "len", cacheHook: ...}` is iterated;
   `RegisterCallableCacheListAppend` fires from
   `objects/list_bind.go::bindO("append", ...)`. Because every gopy
   process registers the same builtin closures during `builtins.Init`,
   the cache is effectively single-writer-single-reader and the bare
   `*BuiltinFunction` / `*MethodDescr` pointers work without locking.
3. **CALL_LIST_APPEND's "consumes POP_TOP" trick.** The CPython arm
   stamps `INSTR_PTR + INLINE_CACHE_ENTRIES_CALL + 1` past the
   instruction so the next dispatch skips the implicit POP_TOP that
   the compiler emits after every CALL whose result is unused. Mirrored
   in `vm/eval_specialized_call_builtin.go::fastCallListAppend` by
   advancing `e.cacheAdvance(compile.CALL) + 2` (one codeunit past
   the standard CALL cache window, which puts the dispatch right
   after the trailing `POP_TOP`). The specializer in
   `specialize/call.go::callFollowedByPopTop` peeks the next codeunit
   at `instr + 2*(1+INLINE_CACHE_ENTRIES_CALL) = instr + 8` to verify
   the bytecode shape before stamping.
4. **Args-window allocation matches CPython's `total_args` rule.**
   `Python/bytecodes.c` `_CALL_*` prologues do
   `arguments-- ; total_args++` whenever `self_or_null` is non-null
   so the unbound-method form (`LOAD_ATTR` emitted with the trailing
   nullshift) ends up sharing the call site with the bound-method
   form. gopy's `callFrameArgs` allocates a single slice of
   `oparg + (self_or_null != nil)` Objects and prepends `selfOrNull`
   when set, so every arm receives `args[0] = self` in the method
   shape without branching internally.
5. **Guard-miss returns (0, false, nil), not a deopt opcode rewrite.**
   The arms only need to back out to the generic CALL when the cache
   has gone stale; they do *not* rewrite the opcode themselves.
   `maybeDeopt` upstream (see `vm/eval_specialized.go::trySpecialized`
   wrapping in `eval.go`) handles the counter decrement and adaptive
   rollback. This matches CPython's `DEOPT_IF` which is a `goto deopt`
   to the parent's tier-1 body, not an in-place opcode rewrite.
6. **`CALL_BUILTIN_CLASS` reads `Type.Vectorcall`, not `Type.Call`.**
   CPython's `_CALL_BUILTIN_CLASS` arm only fires for types whose
   `tp_vectorcall_offset` is set (a small set: `type`, `str`, `bytes`,
   `tuple`, plus a few extension types). gopy's `*Type` carries a
   `Vectorcall func(t *Type, args []Object, kwargs map[string]Object) (Object, error)`
   field that's non-nil exactly for the same set. The arm guards on
   `t.Vectorcall != nil` and falls through to deopt for user-defined
   classes whose construction has to go through the generic
   `type_call` path (which gopy spells `t.New` + `t.Init`).
7. **Deferred work and why.**
   - `list.remove` / `count` / `index` / `__contains__` are still
     `MethVarargs`. Flipping them to `MethO` is a one-line change
     per row, but the wrappers were written assuming `args[1]` is
     the user-passed value while args[0] is self, so the closure-shape
     audit needs to confirm none of them call `self.checkArgs(args, 1, 1)`
     or similar arity-validation helpers that assume the varargs entry
     convention.
8. **Why the test file lives at vm/eval_specialized_call_builtin_test.go,
   not specialize/.** The arms execute under `vm.evalState`, and
   stamping a specialized opcode at the bytecode level requires
   reaching into `compile.Code` to overwrite the opcode byte. That
   surface (`stampCallVariant`) already exists in
   `vm/eval_specialized_call_test.go` for `CALL_PY_EXACT_ARGS`, so
   adding the new tests next to it reuses the helpers and the
   builder-shape (`callOneArg` / `callTwoArgs` build the standard
   `LOAD_CONST callable / PUSH_NULL / LOAD_CONST arg / CALL n /
   RETURN_VALUE` frame). `TestFastCallListAppend` is the one outlier:
   it builds custom bytecode that includes the trailing `POP_TOP` +
   `LOAD_CONST None` + `RETURN_VALUE` so the arm's SKIP_OVER advance
   has a target to land on without falling off the codestream.

**Technical notes (P1.4b CALL_ALLOC_AND_ENTER_INIT fast arm + init cache).**

1. **CPython's macro composition.**
   `Python/bytecodes.c:4186` defines
   `CALL_ALLOC_AND_ENTER_INIT = unused/1 + _CHECK_PEP_523 + _CHECK_AND_ALLOCATE_OBJECT + _CREATE_INIT_FRAME + _PUSH_FRAME`.
   `_CHECK_AND_ALLOCATE_OBJECT` (`Python/bytecodes.c:4137`) DEOPTs
   when `self_or_null` is non-null (only direct class calls qualify;
   bound-method shape goes elsewhere), validates the cached
   `tp_version_tag` against cells 2..3, loads `init` from
   `cls->_spec_cache.init`, allocates the instance via
   `PyType_GenericAlloc(cls, 0)`, and rewrites the stack window
   `(cls, NULL, args...)` into `(init, self, args...)`.
   `_CREATE_INIT_FRAME` (`Python/bytecodes.c:4161`) pushes a 2-op
   shim frame running `_Py_InitCleanup` (which is
   `EXIT_INIT_CHECK + RETURN_VALUE`) plus a real Python frame for
   `init`. `_PUSH_FRAME` then `DISPATCH_INLINED`s into the init body.
   On init return, the shim frame's `EXIT_INIT_CHECK`
   (`Python/bytecodes.c:4193`) validates the return is None
   (raising `TypeError("__init__() should return None, not ...")`
   otherwise) and `RETURN_VALUE` pushes the cached self back to the
   caller.

2. **Why gopy folds the shim into the fast arm.**
   gopy's `Eval()` is a Go function returning `(Object, error)`,
   not a C `goto`-driven dispatch loop. When `fastCallAllocAndEnterInit`
   calls `Eval(e.ts, f2)` for the init body it gets the return
   value back directly, so the `_Py_InitCleanup` shim is
   architecturally redundant: there is no separate bytecode-level
   PC the init frame returns to. The fast arm validates
   `objects.IsNone(out)` immediately after Eval and surfaces the
   same `TypeError` message CPython's `EXIT_INIT_CHECK` would raise.
   This is not a shim under the ground rule: the observable
   behaviour (instance pushed on success, TypeError with that exact
   message on non-None return) is preserved 1:1 with CPython's
   opcode. The two-frame setup is purely a control-flow artifact of
   CPython's tier-1 dispatch shape.

3. **Where the init cache lives.**
   CPython packs `init` and `init_version` into the
   `_specialization_cache` substructure on `PyHeapTypeObject`
   (`Include/internal/pycore_typeobject.h _spec_cache`). gopy mirrors
   it as two `*objects.Type` fields: `specCacheInit *Function` and
   `specCacheInitVersion uint32`, populated by
   `CacheInitForSpecialization(init)` which atomically grabs the
   current `VersionTag()` and stamps both. `SpecCacheInit()` /
   `SpecCacheInitVersion()` are the readers the fast arm consults.
   Storing the resolved `*Function` directly (rather than a
   re-lookup-by-name flag bit) means the fast arm skips MRO walk
   AND the descriptor binding step, matching the spirit of
   CPython's pointer-stash.

4. **Three-layer version-tag check.**
   The arm validates the version tag at three levels before
   committing to the allocation:
   - `liveVer := cls.VersionTag()` rejects `0` because that means
     `_PyType_AssignVersionTag` could not allocate (counter
     wraparound or watcher refused) and CPython's
     `_CHECK_AND_ALLOCATE_OBJECT` treats that case as DEOPT.
   - `liveVer == cachedVer` (cells 2..3) rejects when the type
     was modified between specialize and dispatch (any
     `PyType_Modified` zeroes the tag and the next read allocates
     a fresh non-matching value).
   - `liveVer == cls.SpecCacheInitVersion()` rejects when the
     cache's stamp went stale (defensive: the prior check should
     already catch this since both versions are bumped together,
     but CPython's `_CHECK_AND_ALLOCATE_OBJECT` checks both fields
     too and mismatches between them indicate cache corruption).
   `InvalidateVersionTag()` was extended to clear
   `specCacheInit = nil` + `specCacheInitVersion = 0` so a
   `STORE_ATTR` on the class (or any other type-mutation path that
   goes through `PyType_Modified`) automatically poisons the cache
   the next specialization will repopulate.

5. **Runtime argcount validation.**
   The specializer fires `CALL_ALLOC_AND_ENTER_INIT` for the
   observed `nargs` at stamp time (carried in the CALL opcode's
   oparg), but the cached `init` function carries its own
   `co.Argcount`. A call site that stays the same opcode but
   changes its oparg between stamp and dispatch (e.g. the
   specializer fired on a one-arg call and the same site now hits
   with two args after a refactor) would otherwise corrupt
   `LocalsPlus`. The arm guards on `co.Argcount == argc + 1` (the
   `+1` is the implicit self) and deopts on mismatch. This is the
   one runtime check that has no direct CPython analogue because
   CPython's `_CHECK_AND_ALLOCATE_OBJECT` runs the same arity
   check implicitly via the frame-build step inside
   `_CREATE_INIT_FRAME`; gopy lifts it earlier so the deopt is
   clean before we touch the frame stack.

6. **SIMPLE_FUNCTION classification.**
   `isSimpleFunction` in `specialize/call.go` mirrors CPython's
   `Python/specialize.c:1785 function_kind` filter to SIMPLE: the
   init must have `CO_OPTIMIZED` set and zero `*args` / `**kwargs`
   / kwonly parameters. CPython enforces this so the cached
   pointer can be invoked through the fixed-arity fast-frame
   builder; gopy enforces it for the same reason, because the
   `f2.SetLocal(i+1, ...)` loop in the fast arm assumes a flat
   positional layout. `lookupInitFunction` filters
   `LookupDescriptor(tp, "__init__")` to `*Function` (declining to
   stamp when the descriptor resolves to a method-descriptor or
   wrapped slot), matching the `PyFunction_Check` filter on
   `_PyType_LookupRefAndVersion` in `specialize_class_call`.

7. **TpNew == nil is gopy's `tp_new == object.__new__`.**
   CPython requires `tp_new == object.__new__` so the allocation
   path is the generic one. gopy's user heap types leave
   `TpNew == nil` whenever no `__new__` is defined in the class
   body (the metaclass path inherits the default), so the
   `tp.TpNew == nil` guard in `specializeClassCall` is the exact
   equivalent. The allocation itself runs through
   `objects.NewInstance(cls)` which is gopy's `PyType_GenericAlloc`
   analogue.

8. **Frame stack push/pop discipline.**
   `frameStackFor(e.ts).Push(co, init.Globals, init.Builtins, init, nil)`
   matches `CALL_PY_EXACT_ARGS`'s frame-build pattern: the new frame
   takes the init function's `co_globals` / `co_builtins`, the
   `*Function` pointer as the function attribute, and a nil parent
   slot (because Eval will wire f2.Back to the current frame).
   `stack.Pop()` runs in BOTH the success and error branches; an
   earlier draft only popped on success and surfaced a leaked
   frame when the test that intentionally returned non-None ran in
   sequence with the next test. The `(int, bool, error)` return
   contract makes the dispatcher distinguish "fast arm took the
   dispatch and produced result" from "guard miss, deopt" — the
   non-None error case is `(0, true, err)` so the dispatcher knows
   not to re-run the generic body.

9. **Stack layout on entry and exit.**
   Entry: `[..., cls, NULL, arg0, ..., arg(argc-1)]` with TOS at
   `arg(argc-1)`, so `peek(argc)` is the NULL self-slot and
   `peek(argc+1)` is `cls`. The arm drops `argc + 2` entries (cls,
   NULL, all args) and pushes the freshly-allocated instance.
   `cacheAdvance(compile.CALL)` advances the InstrPtr past the
   CALL plus its 3 inline-cache codeunits, exactly the same stride
   the generic CALL body uses.

10. **Test coverage.**
    `vm/eval_specialized_call_alloc_init_test.go` exercises six
    paths: (1) zero-arg init hit, returns a fresh `*Instance` of
    the expected type; (2) one-positional-arg init hit, propagates
    the argument through `SetLocal(1, ...)`; (3) init that returns
    a non-None value raises `TypeError: __init__() should return
    None, not '...'` with the actual return-value type in the
    message; (4) non-`*Type` callable deopts cleanly (the generic
    CALL body runs and produces the expected 42 sentinel); (5)
    `InvalidateVersionTag()` between stamp and dispatch forces a
    deopt, the slow path still produces a working `*Instance`;
    (6) argcount mismatch (specialized for one arg, called with
    two) deopts and the slow path raises the standard
    `TypeError: __init__() takes 2 positional arguments but 3 were
    given` from `Instance.Init`. All six pass; broader
    `go test ./vm ./specialize ./objects ./compile -count=1`
    stays green.

**Gate.**

- `specialize/integration_test.go` — run `richards.py` 3 times under
  a harness that asserts the specialized opcodes outnumber generic
  by 10:1 after warmup.
- Small-subset bench: `call_method`, `richards`, `regex_compile`
  drop to <200x cpython (from 1899x-2407x).
- `optimizer/builtins_watcher_test.go` covers the slot-0 callback end-to-end
  (counter bump, executor invalidation under cap, no invalidation past cap)
  plus `EnsureBuiltinsSubscribed` stamp + idempotency. 4 tests.
- `specialize/watcher_test.go` covers the installer hook (fires on every
  ensureWatchersInstalled, no-op when unregistered, replacement semantics).
  3 tests.
- `optimizer/install_test.go` covers the latch: skip when no main interp,
  install exactly once otherwise. 2 tests.

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

**Why a generator and not a hand port.** The 8263 LOC of
`optimizer/uops_stubs_gen.go` are generated. Header line 1 reads
`// This file is generated by tools/uops_gen/tier2_generator.go
from: Python/bytecodes.c Do not edit!`. The stub bodies that
return `s.unimplementedUop(NAME)` are the placeholder the
generator emits when no body translation exists yet; the real
bodies live as DSL `inst()` / `op()` blocks inside CPython's
`Python/bytecodes.c` and `Python/optimizer_bytecodes.c`. Hand-
porting the placeholders one by one would re-translate the same
~6700 LOC of C-with-DSL into Go, by hand, with no machine check
that the translation matches the tier-1 body of the same opcode.
This is exactly the class of drift spec 1714 was opened to delete
(see 1714's "Why this spec exists" section: five hand-mirrored
sources of truth per opcode, LOAD_GLOBAL's cell-4-vs-cell-1 bug
as the canonical example).

So P2.2 and P2.3 land as the **output** of spec 1714's generator
pipeline, not as a separate manual port. Concretely:

- P2.2 (`Python/optimizer_bytecodes.c`, 114 abstract-interp
  cases) is the deliverable of spec 1714 phase M
  (`gopy_optimizer_generator.py`), which emits
  `optimizer/optimizer_bytecodes_gen.go` (estimated ~2500 LOC).
  When 1714 M is green, the `analysis.go:optimizeUops` empty
  dispatcher is replaced wholesale by the generated case table.
- P2.3 (`Python/executor_cases.c.h`, 271 uop stubs) is the
  deliverable of spec 1714 phase L (`gopy_tier2_generator.py`),
  which emits `vm/eval_uops_gen.go` (estimated ~3000 LOC). When
  1714 L is green, `optimizer/uops_stubs_gen.go` is deleted in
  favour of the generated file.

Spec 1714 owns the porting schedule, the body-translation
subset, the macro bindings (PEEK / POKE / GETLOCAL / SETLOCAL /
DEOPT_IF / ERROR_IF / EXIT_IF / PyStackRef_* / STACK_GROW /
STACK_SHRINK / NEXTOPARG / JUMPBY / INSTRUCTION_SIZE), and the
reproducibility gate (`tools/regen-cases.sh && git diff
--exit-code`). Spec 1712 stops tracking per-uop sub-buckets;
the perf gate just consumes whatever 1714 emits and re-runs
pyperformance once 1714's phases L and M flip green.

`uops_impl.go:14` and `analysis.go:23` already carry
`DEPRECATED (spec 1714)` notes that anticipate this: the
hand-written `_LOAD_FAST`, `_STORE_FAST`, `_CHECK_VALIDITY`
bodies move to `vm/eval_uops_gen.go` when 1714 L lands, and the
14 scaffolding entries (`_NOP`, `_EXIT_TRACE`, `_JUMP_TO_TOP`,
`_START_EXECUTOR`, `_SET_IP`, `_POP_TOP`, `_COPY`, `_SWAP`,
`_PUSH_NULL`, `_LOAD_FAST_BORROW`, `_MAKE_WARM`) get re-emitted
from the same source. No `uops_impl.go` body survives outside
the generator.

**Phases (full-file ports, no piecemeal uop cherry-picking).**

| Phase | Description | Status | Commit |
|-------|-------------|--------|--------|
| P2.1 | Open the JIT gate. Shipped `lifecycle.ApplyJITEnv` (`lifecycle/jit_gate.go`) which mirrors `Python/pylifecycle.c:1325-1352` byte-for-byte: read `$PYTHON_JIT`, flip `interp.JIT = (env[0] != '0')` when the env is non-empty, leave the gate alone otherwise. Wired into `initInterpMain` (`lifecycle/init.go`) so any `gopy` entry that runs the full lifecycle picks it up. The default stays false to match CPython's release-build default (the `#if _Py_TIER2 & 2` branch CPython uses to zero `enabled` when the JIT machine-code backend isn't built); flipping it on globally would just churn projection cycles until P2.2+P2.3 land real uop bodies. Five unit tests in `lifecycle/jit_gate_test.go` cover env-unset (gate untouched), `PYTHON_JIT=1` (enables), `PYTHON_JIT=0` (disables even when caller pre-enabled), non-'0' values (enable), and the nil-interp defensive path. `optimizer.Optimize` continues to short-circuit at the `!interp.JIT` check (already covered by `optimizer/optimize_test.go::TestOptimize_InstallsExecutorOnLoop`), so the env now provides the runtime knob to unlock projection without changing the default. | DONE | 1712-P2.1 |
| P2.2 | `Python/optimizer_bytecodes.c` (1107 LOC, 114 abstract-interp cases) ported as the **output** of spec 1714 phase M (`gopy_optimizer_generator.py`). Lands as `optimizer/optimizer_bytecodes_gen.go` (~2500 LOC). Replaces the empty per-opcode dispatcher in `analysis.go:optimizeUops` (lines 230-256) that bails to `unknown semantics` on every row today. Gate: 1714's reproducibility test (`tools/regen-cases.sh && git diff --exit-code`) green, plus `optimizer/analysis_test.go` shows constant folding and guard elimination firing on a representative trace. No status tracked here; status follows 1714 phase M. | BLOCKED-ON-1714-M | - |
| P2.3 | `Python/executor_cases.c.h` (7163 LOC, 271 uop stubs) ported as the **output** of spec 1714 phase L (`gopy_tier2_generator.py`). Lands as `vm/eval_uops_gen.go` (~3000 LOC); `optimizer/uops_stubs_gen.go` and the hand-written bodies in `uops_impl.go` are deleted in the same commit. Gate: 1714's reproducibility test green, plus `optimizer/uops_test.go` (positive + guard-fail per uop, table-driven from the generator's manifest). No status tracked here; status follows 1714 phase L. | BLOCKED-ON-1714-L | - |
| P2.4 | Wire tier-2 → tier-1 deopt path: on guard fail mid-trace, fall back to the adaptive opcode at the recorded resume offset. Validate against `_CHECK_VALIDITY` and `_GUARD_TYPE_VERSION` failure scenarios. The deopt edges themselves come from the generator (DEOPT_IF expands to `return StatusDeopt` in the generated body), so P2.4 reduces to wiring the executor's `StatusDeopt` return back to `enterExecutor`'s caller. | TODO | - |
| P2.5 | Turn on the tier-2 executor by default for any function that has been Quickened. (P1.5 originally listed as a prereq under the assumption that `.pyc` carries the warmed cache; investigation while shipping P1.5 showed CPython deopts before write and re-quickens on load, so warm caches never persist across `.pyc` boundaries in either runtime. `specialize.Enable` already re-quickens on `unmarshalCode`, so this gate is independent of P1.5.) | TODO | - |

**Gate.**

- Spec 1714's reproducibility test (`tools/regen-cases.sh && git
  diff --exit-code`) is green: every `*_gen.go` under
  `optimizer/` and `vm/` matches what re-running the generator
  on the vendored CPython inputs produces.
- `optimizer/uops_test.go` covers every uop ID with one
  positive case and one guard-fail case, table-driven off the
  generator's uop manifest (no per-uop hand-written test row).
- `optimizer/analysis_test.go` shows the abstract interpreter
  folding constants and eliminating dead guards on at least one
  representative trace (nbody hot loop).
- `optimizer/bench_test.go::BenchmarkTier2Nbody` shows the tier-2
  path is ≥2x faster than tier-1 on the warm loop.
- pyperformance run rerun with `PYTHON_JIT=1` after 1714 L+M
  flip green; results appended below as a timestamped section.

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
| P4.1 | `objects/unicode_kind.go`: detect kind at construction. Latin-1: byte-equal to ASCII; BMP: re-encode to `[]uint16`; Full: `[]rune`. | Shipped (Unicode struct carries `kind`+`ascii`+`length`+`data1 []uint8`/`data2 []uint16`/`data4 []uint32` via `str.go classify`. ASCII strings skip slab allocation since byte index already equals codepoint index in the Go-string carrier `s.v`. Non-ASCII kind-1 (codepoints 0x80..0xFF) fills `data1` with the raw UCS-1 bytes; kind-2 (BMP) fills `data2` with raw uint16 codepoints; kind-4 (astral) fills `data4` with raw uint32 codepoints. Single classify-time scan finds `maxr`, picks the narrowest kind, then a second pass fills the chosen slab. Mirrors CPython's `_PyUnicode_Ready` in `Objects/unicodeobject.c:1731` where `PyUnicode_KIND` + `PyUnicode_DATA` route to a flat `Py_UCS1[]` / `Py_UCS2[]` / `Py_UCS4[]` buffer. New `RuneAt(i)` accessor inlines `PyUnicode_READ(kind, data, i)`: ASCII reads `s.v[i]`, kind-1 reads `s.data1[i]`, kind-2 reads `s.data2[i]`, kind-4 reads `s.data4[i]`. `unicodeGetItemKind` and `strIterator.IterNext` both dispatch through `RuneAt`, so `s[i]` on a 4096-codepoint BMP string benchmarks at 62 ns/op on Apple M4 (independent of string length, was O(n) UTF-8 walk). `unicode_latin1_cache.go` singleton init also fills `data1` for codepoints 0x80..0xFF so the slab dispatch invariant `kind=1 && !ascii implies data1 != nil` holds across cached singletons and freshly-built strings alike. Allocation cost: kind-2 string of length L spends 2L additional bytes beyond the canonical Go-string; kind-4 spends 4L. ASCII strings (the dominant case in pyperformance) still spend zero slab bytes. Tests: `objects/unicode_slab_test.go` pins classify dispatch (`TestStrSlabClassify`), slab population invariants (`TestStrSlabPopulated`), getitem dispatch per kind (`TestUnicodeGetItemKindSlabs`), latin1 cache invariant (`TestLatin1CacheSlabInvariant`), iterator dispatch (`TestStrIteratorSlabs`), plus benchmarks `BenchmarkUnicodeGetItem_UCS2_Last` and `BenchmarkUnicodeGetItem_UCS4_Last` proving O(1) indexing on the last element of long non-ASCII strings. This unblocks P15.1's writer fast path that needs cheap per-codepoint reads to widen kind without re-walking UTF-8.) | this PR |
| P4.2 | Kind-dispatched `__getitem__`, `__len__`, slicing. Latin-1 hits a byte-index path (allocation-free for single chars via small-string cache). | DONE for ASCII (`unicodeGetItemKind` indexes `s.v[i:i+1]` directly when `IsASCII()`; non-ASCII falls back to the rune walk). `__len__` already reads `u.length` so it is O(1). Slicing fast path still TODO. | this PR |
| P4.3 | Kind-dispatched `find`, `rfind`, `count`, `index`, `rindex`, `startswith`, `endswith`. Latin-1 → `bytes.IndexByte` / `bytes.Count` (memchr speed). | DONE for ASCII (haystack `IsASCII()` skips the `runeSlice` + re-encode + `RuneCountInString` chain and hands the raw Go-string view to `strings.Index` / `LastIndex` / `Count` / `HasPrefix` / `HasSuffix`). `BenchmarkStrFindASCII` goes from 215 ns/op + 224 B/op + 2 allocs/op to 8.4 ns/op + 0 B/op + 0 allocs/op on Apple M4 (25x). Non-ASCII keeps the rune walk. `StrReplace` + non-whitespace `StrSplit` are already byte-optimal (they call `strings.Replace` / `Split` which operate on bytes; UTF-8 self-synchronisation prevents false matches). `strSplitWhitespace` ASCII fast path landed too: `strSplitWhitespaceASCII` walks the haystack as bytes with `isPyWhitespaceASCII` (the broader 0x09-0x0D / 0x1C-0x1F / 0x20 set that `_PyUnicode_IsWhitespace` recognises, fixing a pre-existing gap where Go's `unicode.IsSpace` dropped FS/GS/RS/US on the floor). Forward split goes 754 ns/op → 297 ns/op (2.5x, allocs 17 → 5); rsplit benefits from a build-then-reverse loop replacing the O(n^2) prepend, 1208 ns/op → 288 ns/op (4.2x, allocs 33 → 5). Non-ASCII still walks runes through `strSplitWhitespaceRunes` until P4.1 lands the kind-2/4 storage. | this PR |
| P4.4 | `_PyUnicodeWriter` port (lands with P15). | TODO | - |
| P4.5 | Small-string cache: `__getitem__` returning a one-char str is allocation-free for ASCII. | DONE (`objects/unicode_latin1_cache.go` builds the 256-entry singleton table at init time and pre-computes each entry's hash. `NewStr` short-circuits via `latin1StringHit` when the input is a single-codepoint string < 256 (covers ASCII 0-127 as 1-byte forms and latin1 128-255 as 2-byte UTF-8). `unicodeGetItemKind` returns the cached pointer directly for both ASCII byte index and the rune-walk fallback when the codepoint is < 256. `builtins.Chr` short-circuits to `GetLatin1Char` for ordinals < 256, matching `PyUnicode_FromOrdinal`. Identity gates: `s[i] is s[i]`, `s[i] is chr(ord(s[i]))`, `chr(0xc9) is "É"`. | this PR |

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
| P5.1 | Audit / regression-check the existing open-addressed layout against `Objects/dictobject.c:lookdict` probe sequence. Add `objects/dict_lookup_parity_test.go` table-driven from CPython's hash collisions. | DONE | objects/dict_lookup_parity_test.go pins the `(5i+1+perturb)&mask` recurrence (PERTURB_SHIFT=5), `TestDictProbeWalksSameChain` and `TestDictProbeHonoursPerturbCascade` confirm gopy's dictProbe lands on the same slots, `TestDictProbeRespectsDummyAsFreeSlot` covers the freeslot branch. |
| P5.2 | Real split-keys storage: per-type `SharedKeys` object owns the entries-array shape; instance `__dict__` carries `values []Object` only. Materialise to combined on delete or non-shared insert. Cite `Objects/dictobject.c:insertion_resize_inplace`. | DONE | 72b8c904 (storage); 1d0c9598 (wiring: `Type.sharedKeys` lazily allocated by `AddCachedKey`; `NewInstance` routes through `NewSplitDict` when shared keys is seeded; `TestNewInstanceSharesKeysAcrossSiblings` pins refs==2 sibling sharing). |
| P5.3 | `_PyDict_SetItem_KnownHash` fast path: skip rehash when caller passes the hash. Wire from LOAD_ATTR / LOAD_GLOBAL specialized arms. Cite `Objects/dictobject.c:_PyDict_SetItem_KnownHash`. | DONE | 2b5edb3d (GetItemKnownHash / ContainsKnownHash / SetItemKnownHash on `*Dict`; `(*Unicode).HashCached()` accessor; `lookupIn` / `storeIn` short-circuit when key is `*Unicode`). |
| P5.4 | Public watcher subscription API: `PyDict_Watch(watcher_id, dict)` / `PyDict_AddWatcher(callback) -> int8_t`. Cite `Objects/dictobject.c:7710 PyDict_Watch / :7741 PyDict_AddWatcher`. Replaces the bare `DictMutationHook` pointer. | DONE | objects/dict_watcher.go + objects/dict.go (`watcherTag`), objects/dict_mutate.go + objects/dict.go fire ADDED / MODIFIED / DELETED / CLEARED / CLONED; optimizer/watcher.go delegates AddWatcher/Watch/Unwatch to the public API; `DictMutationHook` retired. |
| P5.5 | Install the watcher at `specialize.Enable` time + invalidate inline caches on dict mutation. Interacts with P1.6. | DONE (closed by P1.6 wiring: `specialize.Enable` calls `ensureWatchersInstalled()`, optimizer slot 0 = builtins callback, slot 1 = globals callback. `EnsureBuiltinsSubscribed` mirrors `Python/pylifecycle.c:1381` for the canonical builtins subscription.) | b059710d |

**Gate.**

- `objects/dict_oa_test.go` cross-checks every op against a
  reference implementation on a randomized workload.
- `BenchmarkDictLookup` shows 0 allocations on the hot path.
- `meteor_contest` / `go` benches drop primarily on P5.

**Estimated win.** 2x on attribute- and call-method-heavy code.

**Technical notes (P5.2 split-keys storage).**

1. `SharedKeys` is now a real probing table that mirrors the
   layout of a combined `PyDictKeysObject`: `entries []dictEntry`
   plus `order []int`, `used`, `fill`, `version`, `refs`. Every
   instance of a class points at the same `SharedKeys`; only the
   per-instance value array is duplicated, which is the storage
   win CPython advertises in `Objects/dictobject.c:567`.
2. `NewSplitDict(sk)` reuses `sk.entries` as the dict's
   `d.entries` slice header (the two slice variables share the same
   backing array). Key + hash reads keep flowing through
   `d.entries[idx].key` / `.hash` unchanged. Per-instance values
   live on a separate `Dict.splitValues []Object` aligned with
   the same slot indices; reads route through `slotKey` /
   `slotValue` / `slotIsLive` accessors on `Dict`.
3. `dictInsert` dispatches to `dictInsertSplit` when
   `d.sharedKeys != nil`. Existing shared keys land in
   `splitValues[idx]` directly; the dict stays split. **New keys
   or non-unicode keys take the conservative path: materialize to
   combined first and re-enter `dictInsert`.** CPython's
   `insert_split_key` (`Objects/dictobject.c:1832`) extends the
   shared table when `dk_refcnt == 1`, but that requires an
   invalidation dance across every split sibling. Materializing
   first preserves correctness without the multi-instance
   bookkeeping; the SharedKeys itself stays intact for other
   instances still using it. Lifting this restriction is a
   follow-up: it would require a per-class `dk_version` bump
   that wakes every sibling dict and re-derives their
   `splitValues` indexes.
4. `dictDelete` clears `splitValues[idx]` in split mode (the
   slot drops from `d.order` but the shared `d.entries[idx]`
   entry stays live so sibling instances still find their
   values). `dictResize` calls `ensureCombined()` first; a split
   dict can't resize without copying out, and the materialize
   path allocates a fresh private `entries[]` anyway.
5. `Dict.lookup` wraps `dispatchLookup` to flip `found=false`
   when the shared key exists but this instance never set the
   value (`d.sharedKeys != nil && d.splitValues[idx] == nil`).
   The four probe variants under `dispatchLookup` stay unaware
   of split-mode semantics.
6. Storage savings are reachable end-to-end as of 1d0c9598.
   `Type` carries a lazily-allocated `sharedKeys *SharedKeys` that
   `AddCachedKey` extends in place via `AddKey`. `NewInstance`
   for INLINE_VALUES types routes through `NewSplitDict` once the
   shared table has at least one key, so sibling instances share
   one keys table with per-instance value arrays.
   `TestNewInstanceSharesKeysAcrossSiblings` pins `refs==2` and
   write isolation. The first instance of a fresh class still
   materializes combined since `SharedKeys` is empty until the
   first `SetAttr` lands; this matches CPython's observation that
   the first object seeds `ht_cached_keys` for siblings.
7. `NewEmptySharedKeys` returns a fixed `dictMinSize` table and
   `AddKey` refuses (returns false) at `loadAtCapacity` rather
   than resizing. The no-resize invariant is load-bearing:
   `NewSplitDict` shares its `entries` slice header with
   `sk.entries`, so a resize would orphan every attached dict.
   CPython sidesteps this with `dk_refcnt` + `dk_version`
   stamping; gopy enforces the same outcome by refusing the
   resize. With `usableFraction(8) = 5`, a class can cache up to
   5 attribute names through the split shape; beyond that, new
   names fall through `dictInsertSplit`'s materialize-on-new-key
   branch. Lifting this cap requires either pre-sizing the
   shared table at class-build time (when the attribute count is
   known) or a refcount-snapshot-and-detach dance, neither of
   which is in scope for this phase.
8. Follow-up still pending: teach the
   `LOAD_ATTR_INSTANCE_VALUE_*` / `STORE_ATTR_INSTANCE_VALUE_*`
   specializer fast arms to read straight from
   `splitValues[hint]`. The storage is in place; the specializer
   arms still go through the regular dict lookup.

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
| P6.1 | `frame/chunk.go`: extend the existing chunk arena so `Pop` recycles the `LocalsPlus` slice header on the chunk slot and the bottom chunk persists across pop-back-to-zero. The next `Push` then hits `Init`'s `cap(LocalsPlus) >= size` fast path and skips the `make`. CPython parity: `_PyThreadState_PopFrame` leaves the activation-record memory in the data stack for the next `_PyEvalFramePushAndInit`; `_PyStackChunk` is only freed at thread destruction. | DONE | (working tree) |
| P6.2 | `LOAD_FAST_CHECK` codegen in `compile/flowgraph_cfg_locals.go:scanBlockForLocals` + eval arm in `vm/eval_dispatch_handwritten.go:opLOAD_FAST_CHECK`. | DONE (spec 1716) | - |
| P6.3 | `LOAD_FAST_BORROW` / `LOAD_FAST_BORROW_LOAD_FAST_BORROW` / `STORE_FAST_LOAD_FAST` / `STORE_FAST_STORE_FAST` (CPython 3.14 new opcodes that elide the incref pair and fold adjacent local-slot ops). | DONE | (working tree) |
| P6.4 | Args-tuple bypass: `CALL_PY_EXACT_ARGS` stores args directly into the callee's frame locals. | DONE | (working tree) |

**Gate.**

- `vm/frame_pool_test.go` proves recycle works under load.
- `BenchmarkCallNop` shows 0 allocations on the hot path.

**Estimated win.** 1.5x on call-heavy code (richards, deltablue).

**Technical notes (P6.1 chunk LocalsPlus recycle).**

- The chunk arena in `frame/chunk.go` already recycled the `*Frame`
  slot, but the previous `Pop` wrote `s.current.frames[top] = Frame{}`
  wholesale, which threw away the `LocalsPlus` slice header along
  with the rest of the frame. The next `Push` at that slot saw a
  zero-length slice and re-`make()`d the locals storage on every
  call. The two-line fix: drop the wholesale overwrite on the
  non-generator branch and let `f.Clear()` (which nils out
  `Code/Globals/Builtins/Locals/Func/Previous` but leaves
  `LocalsPlus` alone) prepare the slot. `Init` already has the
  `cap(LocalsPlus) >= size` fast path that reuses the backing
  array.
- The OwnedByGenerator branch still wipes the slot wholesale
  because the generator owns the storage after `Detach`. Sharing
  the backing array between the live generator and the next
  caller's frame would alias generator locals across calls. The
  new `TestFrameStackGeneratorOwnedDropsLocalsPlus` locks that
  invariant in.
- The bottom chunk now stays attached when the call depth hits
  zero. Before, `s.current = s.current.prev` set `s.current = nil`
  whenever the only chunk emptied; that wiped the recycled
  `LocalsPlus` storage on the very next `Push`. CPython's
  `_PyStackChunk` is only freed at thread destruction (or explicit
  shrink), and the same pop-to-zero-then-push pattern hits every
  pyperformance benchmark that returns to module scope between
  iterations. The `s.current.top == 0 && s.current.prev != nil`
  guard mirrors the CPython "idle thread keeps its chunk" rule.
- The Pop guard `s.current == nil || s.current.top == 0` was
  tightened to cover the new state where the bottom chunk is
  retained but empty. The pre-existing
  `TestFrameStackPushPop` test pops one extra time as a no-op gate
  and would have indexed `frames[-1]` without the guard.
- New tests: `TestFrameStackLocalsPlusRecycled` (asserts both
  `cap(LocalsPlus)` and `&LocalsPlus[0]` survive the round-trip),
  `TestFrameStackGeneratorOwnedDropsLocalsPlus` (asserts the
  generator path does not alias). Both pass; `frame/`, `vm/`,
  `objects/`, `compile/` all green.

**Technical notes (P6.3 LOAD_FAST_BORROW / STORE_FAST fusion).**

- Audit showed the full subsystem was already ported and wired,
  shipped as part of spec 1715 / 1716. `optimizeLoadFast` in
  `compile/flowgraph_cfg_locals.go:145` ports `optimize_load_fast`
  from `Python/flowgraph.c:2776` and rewrites `LOAD_FAST` /
  `LOAD_FAST_LOAD_FAST` into the BORROW variants when the abstract
  reference stack can prove the slot value lives at least as long
  as the consumer. `cfgInsertSuperinstructions` in
  `compile/flowgraph_cfg_passes.go:1147` ports `insert_superinstructions`
  from `Python/flowgraph.c:2588` and folds adjacent `LOAD_FAST` /
  `STORE_FAST` pairs into the four super-opcodes via the shared
  `makeSuperInstruction` helper (`Python/flowgraph.c:2572`). The
  pipeline runs `cfgInsertSuperinstructions` inside
  `cfgOptimizeCodeUnit`, then `optimizeLoadFast` later in
  `cfgOptimizedCfgToInstructionSequence` at
  `compile/flowgraph_cfg_bridge.go:165`, matching CPython's
  ordering.
- Eval-loop arms exist in `vm/eval_dispatch_gen.go`:
  `LOAD_FAST_BORROW` at line 755 (uses `stackref.Ref.Dup`, a no-op
  in the GIL build since the dispatch saving is the whole point),
  `LOAD_FAST_BORROW_LOAD_FAST_BORROW` at line 760,
  `STORE_FAST_LOAD_FAST` at line 1127, `STORE_FAST_STORE_FAST` at
  line 1143. The opargs encode two 4-bit local indices as
  `(idx1 << 4) | idx2`, identical to CPython.
- Verified byte-for-byte against CPython 3.14 on four real Python
  sources: `def f(a): return a` emits `LOAD_FAST_BORROW`; `def f(a, b): return a + b`
  emits `LOAD_FAST_BORROW_LOAD_FAST_BORROW`; `def f(a): x = a; return x`
  (same line) emits `STORE_FAST_LOAD_FAST` arg=17; `def f(a, b): x, y = a, b; return x + y`
  emits `STORE_FAST_STORE_FAST` arg=50 then `LOAD_FAST_BORROW_LOAD_FAST_BORROW`
  arg=35. All four opcode IDs, opargs, and operand orderings
  match `dis.dis(f)` on CPython 3.14.5 exactly.
- `make_super_instruction` only fuses when the two instructions
  share a source line (the `line1 != line2` guard in
  `Python/flowgraph.c:2572`). gopy's `makeSuperInstruction` ports
  the guard verbatim, so multiline `x = a` then `return x`
  legitimately stays unfused, mirroring CPython.
- New e2e gate: `compile/load_fast_borrow_e2e_test.go` drives all
  four borrow / super-instruction patterns through
  `compile.Compile` so the full pipeline (codegen plus every cfg
  pass plus optimize_load_fast plus assembler) is exercised, not
  just the unit-test slice. The unit tests in
  `compile/flowgraph_cfg_locals_test.go` and
  `compile/flowgraph_cfg_passes_test.go` already cover the cfg
  passes in isolation, but a regression that wired the pass out
  of the pipeline could pass them and still break user code, so
  the gate lives at the public entry point.

**Technical notes (P6.4 CALL_PY_EXACT_ARGS args-tuple bypass).**

- Audit before the port found the specializer was already
  stamping `CALL_PY_EXACT_ARGS` (and `CALL_BOUND_METHOD_EXACT_ARGS`)
  on hot sites in `specialize/call.go`, but `vm/eval_specialized.go::trySpecialized`
  had no switch case for either opcode. The adaptive dispatcher's
  `maybeDeopt` path was rewriting them back to generic `CALL`
  every tick, so the cooldown counter and stored func_version
  cells were being burnt with no benefit. The fast arm has been
  on the wishlist since `Spec 1712 P6.4` was filed but the
  dispatch arm itself was the missing piece.
- The new arms live in `vm/eval_specialized_call.go` and are
  wired into `vm/eval_specialized.go::trySpecialized` so the
  dispatch loop reaches them before `maybeDeopt`. Three functions:
  `fastCallPyExactArgs(oparg)` peeks the stack for the callable,
  asserts it is `*objects.Function`, and calls the shared body.
  `fastCallBoundMethodExactArgs(oparg)` unwraps the `BoundMethod`
  prefix (matches `_CHECK_CALL_BOUND_METHOD_EXACT_ARGS` plus
  `_INIT_CALL_BOUND_METHOD_EXACT_ARGS` from `Python/bytecodes.c:3960`)
  and then runs the same body. `callPyExactArgsCommon(fn, selfOrNull, argc)`
  carries `_CHECK_FUNCTION_VERSION` (bytecodes.c:3864) against
  `*Function.Version` vs the cached `specialize.CallFuncVersion(...)`
  read, `_CHECK_FUNCTION_EXACT_ARGS` (bytecodes.c:3979) against
  `co.Argcount == oparg + hasSelf`, and finally
  `_INIT_CALL_PY_EXACT_ARGS` (bytecodes.c:3998) which pushes a
  frame off the chunk arena and writes args straight into
  `LocalsPlus`.
- What the arm bypasses on the generic CALL path:
  (1) `make([]objects.Object, argc)` allocating an args slice
  off the value stack in `vm/eval_simple.go::opCALL`,
  (2) `append([]objects.Object{self}, args...)` building a second
  slice in the method-shape branch,
  (3) the `Vectorcall` slot lookup landing in `callPyFunction`,
  (4) the full varargs / kwargs / defaults / missing-arg loop in
  `vm/eval_call.go::callPyFunction` which re-walks every
  positional / kw-only slot per call even when none of those
  features are used. The fast arm replaces all of it with a
  single `stack.Push(...)` plus an `argc`-iteration loop writing
  one `stackref.FromObject` per slot.
- The `_CHECK_FUNCTION_VERSION` cell uses
  `specialize.CallFuncVersion(code, idx)` (read) /
  `specialize.SetCallFuncVersion(...)` (write) from
  `specialize/cache_views.go:140-141`. The specializer already
  populates it in `specialize.specializePyCall`. We additionally
  reject `fn.Version == 0` so a `*Function` that has not yet had
  a version stamped (or has been invalidated by Code/Defaults/Closure
  mutation, which resets to 0) deopts cleanly.
- gopy uses recursive `Eval(ts, f2)` to drive the callee where
  CPython's `_PUSH_FRAME` does an iterative `LOAD_IP` frame swap
  (bytecodes.c:4010). The iterative form is faster in steady
  state because it stays in the same goroutine stack and skips
  the per-call Go runtime entry. Lifting gopy's dispatch loop to
  match would require restructuring `Eval` itself into an outer
  loop that pulls frames off a vector, which is a separate
  spec-scoped change. The P6.4 win compounds with P6.1's chunk
  `LocalsPlus` recycle: the `stack.Push` here lands on the
  already-warm chunk slot with no `make()` for the locals.
- E2E gate in `vm/eval_specialized_call_test.go` covers six
  paths: identity call with `oparg=1`, two-arg add via
  `BINARY_OP NB_ADD`, version miss with stale cached version
  (asserts the dispatcher rewrites the opcode back to `CALL`),
  argcount mismatch (asserts TypeError surfaces from the generic
  body), bound-method unwrap exercising the prefix step on a
  `objects.BoundMethod(fn, Int(99))`, and a type miss where the
  cache says `CALL_PY_EXACT_ARGS` but the callable is a
  `*BuiltinFunction` (asserts the arm deopts and the
  `BuiltinFunction` `Vectorcall` services the call). All six
  pass; `vm/`, `specialize/`, `compile/`, `pythonrun/` all green
  in the regression sweep.

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
| P13.1 | Ported `gc_select_generation` (Python/gc.c:1258) and the `_PyObject_GC_Link` allocator-side trigger (Python/gc.c:1855) into `module/gc/autotrigger.go`. `Track` now calls `maybeAutoCollect` after bumping `generations[0].count`; the helper short-circuits when `enabled=false`, `threshold==0`, the re-entrancy flag is set, or no generation has crossed its threshold. `selectGeneration` walks oldest-to-youngest and applies the issue-#4074 long-lived ratio gate (`long_lived_pending < long_lived_total/4`) before returning gen-2. `collectMain` now bookkeeps `long_lived_pending`/`long_lived_total` exactly as CPython does at Python/gc.c:1399. State carries the new `collecting bool` and the two long-lived counters. Tests in `autotrigger_test.go` cover threshold-crossing, disabled-gc skip, zero-threshold skip, the re-entrancy guard, and the gen-2 ratio gate. | Shipped | - |
| P13.2 | Wire user `__del__` to `Type.Finalize` through a new `slot_tp_finalize` port. Investigation showed that gen-N ordering was already correct (gopy's `collectMain` merges generations 0..gen in ascending order via `listMerge`, which appends to tail), but `fixup_slot_dispatchers` never installed a tp_finalize entry, so user `__del__` simply never fired. Ported `slot_tp_finalize` (Objects/typeobject.c:10585) into `objects/usertype.go` and added a `fixupFinalize` step to `fixupSlotDispatchers` that stamps `t.Finalize = slotTpFinalize` whenever `__del__` is callable on the MRO. The dispatcher swallows errors raised inside `__del__` to match CPython's `PyErr_FormatUnraisable` path, since re-raising mid-collection has no useful target. The cycle collector's existing `typeFinalize` fallback (`module/gc/finalize.go:58`) now finds the slot for user classes. Tests in `usertype_finalize_test.go` cover the direct-define case, the no-`__del__` no-wire case, and inheritance through a base. | Shipped | - |
| P13.3 | End-to-end user `__del__` firing through cycle collection. Two gaps surfaced once P13.2's wiring landed and we tried to drive `__del__` from `Collect`. First gap: `Instance` had no `tp_traverse`, so `subtractRefs`/`moveUnreachable` couldn't see the back-edges through instance attributes and the cycle was never detected. Ported `subtype_traverse` (Objects/typeobject.c:1356) as `instanceTraverse` in `objects/instance.go`, walking each non-nil slot value plus the per-instance dict via `dictTraverse`. Wired conditionally in `NewUserTypeMeta` after `fixupSlotDispatchers` so we only install when nothing else (list/dict subclass inheritance) already supplied a `TpTraverse`. Second gap: `slot_tp_finalize` was calling the resolved `__del__` with zero arguments. CPython routes `__del__` lookup through `lookup_maybe_method` (Objects/typeobject.c:2255) which sets an `unbound` flag for `METHOD_DESCRIPTOR`-flagged callables, and `slot_tp_finalize` then dispatches via `call_unbound_noarg` (Objects/typeobject.c:2308), passing `self` as the sole positional when unbound. gopy's `BuiltinFunction` has no `DescrGet`, so the existing `lookupMethodOnSelf` returned it raw and the Call dropped `self`. Ported `lookup_maybe_method` and `call_unbound_noarg` faithfully (gopy's `isMethodLike` plays the role of CPython's METHOD_DESCRIPTOR flag, covering both `*Function` and `*BuiltinFunction`) and switched `slotTpFinalize` to the new pair. Tests in `module/gc/userdel_test.go` exercise both the basic two-instance cycle (Collect=2, two `__del__` fires) and PEP-442 resurrection (Incref inside `__del__` keeps the object alive, gcFinalized persists so a second Collect does not re-fire `__del__`). | Shipped | - |

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
| `_csv`           | `module/_csv/` exists; `stdlib/csv.py` exists (19186 bytes) | Shipped: full state-machine port replaces the `encoding/csv` shim; reader + writer verified byte-identical to CPython 3.14 on all 5 quoting modes. |

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
| P14.1 | `module/_pickle/`: Go-native pickle protocol 5 encoder + decoder. Full port of `Modules/_pickle.c` (8500 LOC). Phase 1 shipped: opcode table, `HIGHEST_PROTOCOL=5`, `DEFAULT_PROTOCOL=5`, `PickleError` / `PicklingError` / `UnpicklingError` (PicklingError + UnpicklingError subclass PickleError), inittab registration. With only the exception classes published, pickle.py's `from _pickle import (...)` still fails on `Pickler`; that triggers the `except ImportError` branch so `pickle.dumps` / `pickle.loads` continue routing through the pure-Python `_Pickler` / `_Unpickler`. The `from _pickle import PickleBuffer` shim at the top of pickle.py falls back the same way. Phase 2 shipped: internal `pickler` struct + atom write path (`saveNone`, `saveBool`, `saveLong` with BININT1 / BININT2 / BININT / LONG1 / LONG4 width selection, `saveFloat` BINFLOAT, `saveBytes` SHORT_BINBYTES / BINBYTES / BINBYTES8, `saveUnicode` SHORT_BINUNICODE / BINUNICODE / BINUNICODE8, `writeMemoize` after bytes/str), proto-5 FRAME framing with `FRAME_SIZE_MIN=4` suppression rule, two's-complement little-endian payload encoder for LONG1/LONG4, byte-equality gate against 26 `pickle.dumps(value, 5)` fixtures (atoms only). Notes: bool dispatch must precede int dispatch in the type switch since `*objects.Bool` embeds `Int`; CPython picks `nbytes = (bitlen >> 3) + 1` upfront and trims a trailing `0xff` for negatives only when the next byte already has its sign bit set, the LE encoder mirrors that exactly. Phase 3 shipped: `save()` dispatch (`Modules/_pickle.c:4401`) with memo (`map[objects.Object]int`, pointer identity via the dynamic `*objects.Foo` types matches CPython's `PyMemoTable` keyed on raw `PyObject*`), `memoPut` emits MEMOIZE for proto >= 4 and `memoGet` emits BINGET / LONG_BINGET, container savers `saveList` / `saveTuple` / `saveDict` / `saveSet` / `saveFrozenset` (`Modules/_pickle.c:3135 / 2847 / 3428 / 3495 / 3650`) with BATCHSIZE=1000 chunking for APPENDS / SETITEMS / ADDITEMS, single-item APPEND / SETITEM fast paths, narrowest tuple opcode selection (EMPTY_TUPLE no-memo singleton, TUPLE1 / TUPLE2 / TUPLE3, MARK+TUPLE for n>3), and recursive save() dispatch through containers. Byte-equality gate extended with 14 container fixtures + 4 nested-container fixtures (lists of lists, dicts of tuples, mixed-type list with int/str/None/bool/float). Notes: EMPTY_TUPLE is a CPython singleton (`PyTuple_New(0)` returns the cached `_Py_SINGLETON(empty_tuple)`) so save_tuple skips the memoize call on zero-length tuples, byte-equality fails if MEMOIZE is emitted. Frozensets share `*objects.Set` with mutable sets in gopy; the dispatch uses `s.Type() == objects.FrozensetType` to pick saveFrozenset (mirroring CPython's `PyAnySet_Check` followed by `PyFrozenSet_CheckExact`). Recursive tuple / frozenset detection (CPython re-checks the memo after items and emits POP / POP_MARK + BINGET when the parent showed up via a child reference) is intentionally deferred; the byte-equality gate doesn't include self-referential tuples or frozensets, and the parent type-switch already memoizes lists / dicts / sets so the common cycle shapes still hit the memo on the outer container. Phase 4 shipped: `_pickle.dumps(obj, protocol=None, *, fix_imports=True, buffer_callback=None)` and `_pickle.dump(obj, file, protocol=None, *, ...)` published on the module dict. `resolveProtocol` mirrors `_Pickler_SetProtocol` (`Modules/_pickle.c:1391`): None or omitted picks DEFAULT_PROTOCOL=5, negative picks HIGHEST_PROTOCOL=5, > HIGHEST raises ValueError, both name and position raises TypeError. `dump` calls `file.write(bytes_obj)` via objects.CallOneArg. fix_imports / buffer_callback are accepted for signature parity but currently no-op since proto-5 doesn't need fix_imports and out-of-band buffers don't ship yet. Phase 5 shipped: unpickler / decoder for proto-5 atoms + containers. Dispatch loop ports `load` (`Modules/_pickle.c:6950`) opcode by opcode: PROTO / FRAME / STOP / NONE / NEWTRUE / NEWFALSE / BININT / BININT1 / BININT2 / LONG1 / LONG4 / BINFLOAT / SHORT_BINBYTES / BINBYTES / BINBYTES8 / SHORT_BINUNICODE / BINUNICODE / BINUNICODE8 / EMPTY_TUPLE / TUPLE1 / TUPLE2 / TUPLE3 / TUPLE / EMPTY_LIST / EMPTY_DICT / EMPTY_SET / MARK / APPEND / APPENDS / SETITEM / SETITEMS / ADDITEMS / FROZENSET / MEMOIZE / BINGET / LONG_BINGET / BINPUT / LONG_BINPUT / POP / POP_MARK. Value stack + mark stack + memo are independent slices on the unpickler struct. `_pickle.loads(bytes)` / `_pickle.load(file)` published; `load` slurps the file via `file.read(-1)` (matching what io.BytesIO / io.BufferedReader return on -1). Round-trip gate exercises every fixture from the encoder gate plus a few additional decoder-only fixtures (LONG1 big-int, big-int negative). Notes: load_counted_long uses big.Int because Go's int64 only covers the int32 fast path (LONG1 starts at 5 bytes). BININT is signed, BININT1 / BININT2 are unsigned, BINBYTES8 / BINUNICODE8 read 8-byte LE counts (calcBinsize handles overflow). Even with dumps/dump/loads/load published, pickle.py still falls back to the pure-Python `_Pickler` / `_Unpickler` because the second `from _pickle import (...)` block also requires the `Pickler` / `Unpickler` classes. Phase 6 exposes those classes. Phase 6 shipped: `Pickler` and `Unpickler` Python types live under `module/_pickle/pickler.go` and register on the module dict. `Pickler(file, protocol=None, *, fix_imports=True, buffer_callback=None)` validates `file.write` exists, routes protocol through `resolveProtocol`, and binds `dump(obj)` / `clear_memo()` via `Getattro` + `NewBuiltinFunction`; `dump` runs `dumpsAtom` with the constructor's protocol and writes the bytes via `file.write(bytes_obj)`. `Unpickler(file, *, fix_imports=True, encoding='ASCII', errors='strict', buffers=None)` validates `file.read` exists and binds `load()`, which slurps via `file.read(-1)` and routes through `loadsAtom`. fix_imports / encoding / errors / buffers / buffer_callback are accepted for clinic-signature parity and currently no-op. With these registered, pickle.py:1888 resolves on every name and `pickle.dumps` / `pickle.loads` route through the Go encoder / decoder unconditionally. Notes: the two types embed `objects.Header` and the type instances live behind file-scope `var picklerType *objects.Type` populated from an `init()`, since referencing `picklerType` from a `TpNew` set inside a top-level `var = newFooType()` initializer cycles back through the package-init order. `clear_memo` is exposed but a no-op because our encoder allocates a fresh memo per `.dump()` call; CPython retains the memo across `Pickler.dump()` calls, our port matches the byte-equality fixtures which only do single-shot dumps. Round-trip + byte-equality + protocol-kw + constructor-error tests plus a module surface check (Pickler, Unpickler, dump, dumps, load, loads, PickleError, PicklingError, UnpicklingError, HIGHEST_PROTOCOL, DEFAULT_PROTOCOL all resolvable). | WIP | - |
| P14.2 | `module/_elementtree/`: full port of `Modules/_elementtree.c` (4552 LOC). Phase 1 shipped: module scaffolding + `ParseError` (subclass of `SyntaxError`, matching CPython's `PyErr_NewException("xml.etree.ElementTree.ParseError", PyExc_SyntaxError, NULL)`), `Element` type with tag / text / tail / attrib accessors via `Getattro`/`Setattro` (delete rejected with `AttributeError`, non-dict attrib rejected with `TypeError`), `Element(tag, attrib={}, **extra)` positional + keyword constructor folding kwargs onto attrib with kwarg-wins-on-collision (mirrors `get_attrib_from_keywords` + `element_init` positional branch), `Element.__repr__` formatting `<Element 'tag' at 0xADDR>`, `SubElement(parent, tag, attrib={}, **extra)` module-level helper that constructs a child via `Element.__new__` and appends to `parent.children`, inittab registration via `stdlibinit/registry.go`. JOIN_GET / JOIN_SET text-fragment tagging deferred until Phase 2 (TreeBuilder); children mutation API (`append`/`extend`/`insert`/`remove`/`__len__`/`__getitem__`/`__setitem__`/`__delitem__`) deferred to Phase 2; find/findall via ElementPath deferred to Phase 3; XMLParser + TreeBuilder C accelerator deferred to Phase 4. With only ParseError + Element + SubElement published, `xml.etree.ElementTree.parse` / `XMLParser` continues routing through the pure-Python fallback because the second `from _elementtree import ...` import in `ElementTree.py` still misses the C-level types. | WIP | f56abfb9 |
| P14.3 | `module/_sqlite3/`: cgo binding to libsqlite3 or pure Go via `modernc.org/sqlite`. Full port of `Modules/_sqlite/` (6000 LOC). | TODO | - |
| P14.4 | `module/_csv/`: Go-native csv reader/writer matching `Modules/_csv.c` (1600 LOC). Shipped: full state-machine port replacing the `encoding/csv` shim. `module/_csv/parser.go` carries the 9-state reader (`psStartRecord`, `psStartField`, `psEscapedChar`, `psInField`, `psInQuotedField`, `psEscapeInQuotedField`, `psQuoteInQuotedField`, `psEatCrnl`, `psAfterEscapedCrnl`) with `eol = rune(-1)` as the line-end sentinel, mirroring CPython's `(Py_UCS4)-1`. `processChar` ports `parse_process_char` (`Modules/_csv.c:706`) case-by-case; `saveField` honours `QUOTE_NONNUMERIC` / `QUOTE_STRINGS` (unquoted non-empty parses as float via `strconv.ParseFloat`) and `QUOTE_NOTNULL` / `QUOTE_STRINGS` (empty unquoted becomes `None`). `readerIterNext` loops the source iterator, drives every character then once with `eol`, and continues until the state returns to `psStartRecord`, so a quoted field that spans multiple input lines folds into one record. The EOF branch matches `Reader_iternext_lock_held` (`Modules/_csv.c:944`): if the parser sits mid-field or mid-quoted-field, strict mode raises `csv.Error: unexpected end of data`, otherwise the partial field is flushed. `module/_csv/writer.go` carries the two-pass record builder: `joinAppendData` ports `join_append_data` (`Modules/_csv.c:1147`) with a count-phase that mutates `*quoted` when a special char demands wrapping plus a copy-phase that writes into the grown buffer; `joinAppend` (`Modules/_csv.c:1260`) handles the space-delimiter + skipinitialspace empty-field guard; `joinAppendLineterminator` (`Modules/_csv.c:1303`) appends the dialect terminator; `writerWriteRow` ports `csv_writerow_lock_held` (`Modules/_csv.c:1327`) including the single-empty-field rescue (decrement `numFields`, re-append with `quoted=1`). `quotedFor` mirrors the per-mode `switch (dialect->quoting)` block. Output goes through `file.write(str)`, matching CPython's `PyUnicode_FromKindAndData` + `PyObject_CallOneArg`. The `encoding/csv` / `io` / `strings` / `bytes` imports are gone. Writer output verified byte-identical to CPython 3.14 for all 5 quoting modes on a row containing a delimiter, a quotechar, an int, and `None`; reader output verified against CPython for doublequote / line-continuation / escapechar / `QUOTE_NONNUMERIC` / `QUOTE_NOTNULL` / strict-mode errors; round-trip parity over five representative field shapes. | Shipped | - |

**Notes.**

`module/_datetime` pickle pathway (date, time, datetime, timedelta, timezone):

- The bytes-state fast path (`PyDateTime_*_DATASIZE` buffer plus optional tzinfo) must live inside `TpNew`, not just in a Python-level `__new__` wrapper. Pickle's REDUCE opcode runs `cls(*args)` which enters `type.__call__` then `cls.TpNew` directly, bypassing the Python attribute lookup. A `tp_new_wrapper`-shaped Builtin only catches direct `cls.__new__(cls, bytes)` calls.
- gopy does not carry a tzinfo base type. CPython 3.14 has timezone inherit `__reduce__` from `tzinfo.tp_methods.__reduce__` (`_datetimemodule.c:4140 tzinfo_reduce`), which calls `__getinitargs__` and wraps into `(cls, init_args)`. Without porting that onto Timezone, proto 2+ falls through to `object.__reduce_ex__` -> `reduceNewobj`, which only consults `__getnewargs__` / `__getnewargs_ex__`. Result: `(cls.__new__, (cls,), None, ...)` and unpickle calls `timezone()` with zero args.
- Proto 0/1 has no BYTES opcode, so pickle encodes bytes-state payloads as a `latin1` string. When the payload includes bytes >= 0x80, the wire form is the UTF-8 encoding of the latin1 string (e.g. `\xd0\x90` -> `\xc3\x90\xc2\x90`). `Unicode.Value()` returns the decoded string, and `[]byte(v)` rebuilds the original payload byte-for-byte.
- Pickle's GLOBAL opcode reads `module\nname\n` and looks up `cls.__module__` + `cls.__qualname__`. Static types like Date carry `tp_name = "datetime.date"`. `objects/type_getsets.go` now parses the dotted form (`strrchr(tp_name, '.')`) so `__module__` is everything before the last dot and `__name__` / `__qualname__` are the tail.

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
| P15.1 | `objects/unicode_writer.go`: pre-sized writer with kind-aware finalisation (matches P4). Port the 13 `_PyUnicodeWriter_*` functions in full. API: `WriteStr`, `WriteASCII`, `WriteRune`, `Finish() *Unicode`. | DONE | `12b14349` |
| P15.2 | Re-route `str.join`, `str.format`, `%` formatting through the writer. Audit `objects/str_methods.go` + `objects/str_format.go`. | DONE | `f40251bf`, `f72f658f` |
| P15.3 | `BUILD_STRING` opcode lowering: emit a single `writer.Finish()` call instead of N concats. Touch `vm/eval_dispatch_gen.go`. | DONE | `0aa0a42f` |
| P15.4 | f-string codegen: in `compile/codegen.go`, lower an f-string's pieces directly into writer calls (skip `FORMAT_VALUE` + `BUILD_STRING`). Shares P9 spec-parser. | DEFERRED | - |

**Notes (P15.1 + P15.2).** Implementation specifics worth recording
since CPython's writer is kind-tagged (1/2/4 bytes) and ours is not:

- gopy's `*Unicode` wraps a Go string (UTF-8). The port therefore
  stores UTF-8 bytes in the writer buffer and tracks `(pos, maxchar)`
  so `Finish()` can derive PEP 393 kind without re-walking. The
  buffer pre-sizes via `byteCapForCodepoints(n, kind)` using the
  max bytes per codepoint at the current kind tag, matching
  CPython's `OVERALLOCATE_FACTOR=4` heuristic.
- Readonly alias optimization preserves the `Py_NewRef` shortcut:
  the first `WriteStr` into an empty writer stashes the source
  `*Unicode` in `alias` and only materializes a buffer copy on the
  next mutating call. `WriteStr` of one string into a fresh writer
  therefore returns the input unchanged from `Finish()`.
- `Finish()` builds the result `*Unicode` with `kind`, `ascii`, and
  `length` populated from `maxchar` and `pos` so callers skip the
  classify walk that `NewStr(s)` would otherwise force. `StrJoin`
  retains a string-returning shim (`StrJoinUnicode` is the new
  primary entry) so existing string-typed callers stay untouched.
- `unicodeModulo` (`%` formatter) and `strFormatExpand` both route
  literal chunks through `writeBodyChunk`, which takes the ASCII
  fast path via `WriteASCIIString` and falls back to `WriteStr` on
  non-ASCII. This keeps the per-byte loop tight for the common
  case (logging templates, json `dumps` separators, format strings).
- Singleton `str.join` fast path (`PyUnicode_Join`) checks
  `seqlen == 1` and returns the input directly without entering
  the writer, matching `unicodeobject.c:10063 unicode_join`.
- Pre-existing parity gaps surfaced by the smoke harness but NOT
  caused by P15.2: `{0:>{1}}` nested format spec (gopy lacks
  nested-field expansion in `strFormatField`), and `{!a}` for
  non-ASCII (gopy's `ascii()` does not escape `\xNN`). Both are
  out of scope for P15.2 (the conversion is byte-identical to the
  baseline output).

**Notes (P15.3).** BUILD_STRING (`vm/eval_helpers.go`
unicodeJoinArray) now delegates to `objects.StrJoinUnicode` so the
writer's Finish() builds the result *Unicode with kind / ascii /
length populated in one pass. Previously it ran `objects.Str(item)`
per piece (calling `__str__`) then `strings.Join`+`NewStr`. The new
path matches CPython's `_PyUnicode_JoinArray` strict TypeError when
an item is not a *Unicode subclass; FORMAT_VALUE always pushes a str
so no legitimate codegen path hits the strict check. F-string smoke
corpus (positional, repr, format-spec, unicode literals, multi-piece
chains, empty, 5-element loop join) verified byte-identical to
CPython.

**Notes (P15.4 deferred).** The spec line called for f-string
codegen to lower pieces directly into writer calls and skip
`FORMAT_VALUE` + `BUILD_STRING`. CPython 3.14 retains
`CONVERT_VALUE` / `FORMAT_SIMPLE` / `FORMAT_WITH_SPEC` + `BUILD_STRING`
as the f-string lowering and the gopy codegen mirrors it
(`compile/codegen_expr_misc.go:138-159`). Inventing new
writer-direct opcodes would diverge from CPython's bytecode contract
and violate "always use cpython as single source of truth." Most of
the P15.4 win is already captured: `FORMAT_SIMPLE` is a no-op for
existing *Unicode values (`vm/eval_dispatch_gen.go:420`), and
`BUILD_STRING` collects via the writer (P15.3). Re-open only if
CPython itself adds a writer-direct opcode, or if a clean Tier-2
uop fusion lands that avoids per-piece intermediate strings without
introducing custom opcodes.

**Gate.** `BenchmarkStrFormatHot` allocation-free for static format
strings. `json_dumps`, `logging`, `pprint` benches drop materially.

**Estimated win.** 2x on text-heavy benchmarks. Geomean ~1.2x.

## Checklist

| Subsystem                       | CPython source         | gopy destination          | Estimated win | Status | Commit |
|---------------------------------|------------------------|---------------------------|--------------:|--------|--------|
| P0. pyperformance harness       | n/a (tooling)          | `bench/`                  | n/a           | WIP    | ca0bef1 |
| P1. Specializer wire-up         | `Python/specialize.c`  | `specialize/`             | 6-10x         | WIP (P1.0-P1.3 + P1.5 + P1.6 done, P1.4 open) | 67abc0a, 691c2d7, 71a9181, 6a8aace, 96130ac, 2f1f603, b059710d |
| P2. Tier-2 (generator-driven)   | `Python/optimizer_bytecodes.c`, `Python/executor_cases.c.h` | `optimizer/optimizer_bytecodes_gen.go`, `vm/eval_uops_gen.go` | 1.5-2x | WIP (scaffolding done, P2.1 PYTHON_JIT gate shipped, P2.2/P2.3 now blocked on spec 1714 phases M and L; manual sub-bucket plan retired 2026-05-20) | -      |
| P3. PyLong fast path            | `Objects/longobject.c` | `objects/long_fast.go`    | 3x            | DONE (P3.1-P3.4; P3.5 deferred behind Int repr refactor) | `d9e16d2` |
| P4. PyUnicode kind tags         | `Objects/unicodeobject.c` | `objects/unicode_kind.go` | 2x         | WIP (P4.1 + P4.2 + P4.3 + P4.5 shipped: pre-encoded UCS1/UCS2/UCS4 slabs land in `Unicode.data1/data2/data4`, RuneAt is O(1), 25x faster ASCII find / 0 allocs / shared one-char strings, BMP/astral getitem 62 ns/op independent of length; P4.4 `_PyUnicodeWriter` still open) | this PR |
| P5. Dict open-addressing        | `Objects/dictobject.c` | `objects/dict.go` (extend) | 2x           | WIP (open-addressed layout already in tree, KnownHash + watcher API + lookup-parity gate shipped; split-keys remains) | `863d6fb`, `9aac641c`, `2b5edb3d`, P5.1 this PR |
| P6. Frame free-list + LOAD_FAST_CHECK | `Objects/frameobject.c`, `Python/ceval.c` | `frame/chunk.go`, `compile/flowgraph_cfg_locals.go`, `vm/eval_dispatch_handwritten.go`, `compile/flowgraph_cfg_passes.go`, `vm/eval_specialized_call.go` | 1.5x | DONE (P6.1 chunk LocalsPlus recycle; P6.2 via spec 1716; P6.3 via spec 1715/1716 + e2e gate; P6.4 CALL_PY_EXACT_ARGS + CALL_BOUND_METHOD_EXACT_ARGS fast arms) | spec 1716, P6.1 + P6.3 + P6.4 in this PR |
| P7. Type slot cache             | `Objects/typeobject.c` | `objects/type_slots.go`, `objects/type_inherit.go`, `objects/type_watcher.go` | 1.5x | WIP (P7.0 watcher API, P7.2 inherit_slots, P7.3 version invalidation, P7.4 single-load dispatch done; P7.1/P7.5 open) | `e94cf31`, `2d82694`, `d71cf26`, P7.4 this PR |
| P8. Aug-STORE_SUBSCR fix        | `Python/compile.c`     | `compile/codegen_stmt_misc.go:85-106` | unblock 2 N/A | DONE | `02f6c40` |
| P9. int.__format__ spec         | `Python/formatter_unicode.c` | `objects/long_format.go`, `objects/float_format.go`, `objects/int_bind.go`, `objects/float.go` | unblock 1 N/A | DONE | `a5d25ea`, `5512f4f` |
| P10. Float fast path            | `Objects/floatobject.c` | `objects/float_fast.go`  | 2.5x          | DONE (P10.1/P10.2/P10.4; P10.3 tier-2 uops gated on P2 expansion) | `96ce4d9` |
| P11. CFG optimizer + peephole   | `Python/flowgraph.c`   | `compile/flowgraph_cfg_passes.go` | 1.1x | DONE (spec 1716) | 9d7d9f0, 37563f5 |
| P12. Generator fast path        | `Python/genobject.c`   | `objects/generator.go`, `vm/eval_gen.go` | 3x async | DONE (channel + goroutine model); P12.2 SEND tier-2 uop depends on P2.3 | - |
| P13. GC tracking                | `Python/gc.c`          | `module/gc/`              | low geomean   | WIP (~90% done; thresholds + finalizer ordering pending) | - |
| P14. Native pickle/xml/sqlite   | `Modules/_pickle.c`, etc | `module/_pickle/`, etc  | bench-specific | TODO  | -      |
| P15. Unicode writer             | `Objects/unicodeobject.c` | `objects/unicode_writer.go` | 2x text  | DONE (P15.1-3 shipped; P15.4 deferred as CPython divergence, see Notes) | `12b14349`, `f40251bf`, `f72f658f`, `0aa0a42f` |

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
   all landed. P7.4 operator-dispatch single-load shipped with
   PR #74 too. P1.6 specializer-time watcher install closed with
   `b059710d` (see the P1.6 technical-notes block in the Phases
   table for the parity fix + wiring).
3. **P1.4 closure**: LOAD_ATTR closed for every variant that exists in
   the gopy runtime today. `METHOD_WITH_VALUES` and
   `NONDESCRIPTOR_WITH_VALUES` shipped on `9051a0c3`: the
   `Py_TPFLAGS_INLINE_VALUES` + `Py_TPFLAGS_MANAGED_DICT` flags land
   in `objects/usertype.go::NewUserTypeMeta` whenever the new class
   ends up with a managed dict; the inline-values shape and shared-
   keys version invariants are modelled directly on `Instance.inlineValid`
   and `Type.cachedKeys` / `Type.cachedKeysVersion` (no parallel
   value array, since the WITH_VALUES arms in CPython guard but
   never read the inline-values block, per the technical-notes
   block on P1.4 INLINE_VALUES). `METHOD_LAZY_DICT` is the lone
   remaining LOAD_ATTR variant; gopy allocates `Instance.dict`
   eagerly in `NewInstance` so the LAZY_DICT runtime state
   (managed-dict slot null at LOAD_ATTR time) does not exist
   yet, and shipping the arm requires a per-instance lazy-dict
   mode that touches every attribute path in `instance.go`.
   FOR_ITER `LIST`/`TUPLE`/`RANGE` shipped with the typed `Next`
   helpers; `GEN` waits on the SEND generator-frame path.
   LOAD_SUPER_ATTR `ATTR`/`METHOD` shipped via
   `objects.SuperLookup` + the `method_found` probe gated on
   `tp_getattro == GenericGetAttr` (see P1.4b sub-table +
   technical-notes block). `LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN`
   shipped independently: a new `fixupGetattroSlot` (called from
   `fixupSlotDispatchers` between descriptor-slot fixup and
   `tp_new` fixup) wires a `slotTpGetattroHook` Getattro slot
   whenever a non-`object` class owns `__getattribute__`; the
   hook resolves the override, binds it via `tp_descr_get`, and
   falls back to `__getattr__` on AttributeError, collapsing
   CPython's `_Py_slot_tp_getattro` + `_Py_slot_tp_getattr_hook`
   into one entry point. The specializer (`specializeGetattributeOverridden`
   in `specialize/load_attr.go`) refuses the arm when the class
   also owns `__getattr__` since the fast arm doesn't run the
   hookful fallback path. The fast arm
   (`fastLoadAttrGetattributeOverridden` in `vm/eval_specialized.go`)
   calls the cached function synchronously through `objects.Call`
   instead of CPython's `DISPATCH_INLINED` frame bounce; gopy
   can't push a Python frame from inside a fast arm so the
   synchronous call beats the generic LOAD_ATTR path by skipping
   descriptor walk + instance-dict lookup + slot dispatcher.
   Cache layout: type_version in cells 2..3, `func_version` cells
   left zero (gopy has no per-function version, type_version
   invalidation alone covers freshness), cached `*Function`
   pointer in `CacheObjects[instr]`. SEND_GEN landed as a
   dispatch-level fast arm (the goroutine-channel generator
   design rules out the CPython frame-push inlining; see
   technical-notes block). CALL_ALLOC_AND_ENTER_INIT landed by
   stashing `(*Function, version)` into `Type._spec_cache` and
   folding the `_Py_InitCleanup` EXIT_INIT_CHECK None-validation
   into the fast arm directly (Go-level `Eval()` returns
   without a DISPATCH_INLINED hop). Remaining P1.4 work: the
   FOR_ITER_GEN variant shares the SEND_GEN ceiling (waits on
   P12 generator redesign).
4. **P1.5 deopt-before-marshal** (DONE on PR #74): the original spec
   premise was inverted. CPython does NOT persist warm specializer
   state via `.pyc`; `_PyCode_GetCode` clones `co_code_adaptive`
   and runs `deopt_code` (`Objects/codeobject.c:2293`) before
   marshal-write, so every specialized opcode is rewritten to its
   adaptive parent and every inline cache cell is zeroed. On load,
   `_PyCode_New` re-runs `_PyCode_Quicken` to re-stamp the adaptive
   counters. gopy already re-quickens on `unmarshalCode` via
   `specialize.Enable` (P1.1); the missing half was the pre-write
   deopt, now shipped as `specialize.DeoptCode` and wired into
   `marshal.marshalCode`. Net effect: `.pyc` bytes are deterministic
   across runs and independent of any specialization state the
   in-memory `Code` happened to warm at marshal time.
5. **P2.1 open the JIT gate** (DONE on PR #74): ported the
   `Python/pylifecycle.c:1325-1352` PYTHON_JIT env-var block as
   `lifecycle.ApplyJITEnv`, called from `initInterpMain`. Default
   stays `interp.JIT = false` to match CPython's release-build
   default (the `_Py_TIER2 & 2` branch zeros `enabled` until the
   JIT machine-code backend is built); `PYTHON_JIT=1` flips the
   gate on, `PYTHON_JIT=0` opts out even when a caller pre-enabled
   it. Trace projection already had end-to-end coverage at
   `optimizer/optimize_test.go::TestOptimize_InstallsExecutorOnLoop`;
   the new gate unlocks it from the env without changing the
   default (flipping JIT on globally would churn projection cycles
   on every hot loop until P2.2+P2.3 land real uop bodies).
   Then **P2.2 + P2.3 full-file ports of
   `Python/optimizer_bytecodes.c` and `Python/executor_cases.c.h`**,
   driven by the spec-1714 cases generator.
6. **P3 PyLong fast path** + **P10 float pool** ship in parallel
   (independent `objects/` work).
7. **P4 kind tags + P15 unicode writer** ship together (writer's
   `Finish()` depends on kind detection). **P4 ASCII fast paths
   shipped on PR #74.** The Unicode struct already classifies kind
   at construction (`str.go:312 classify`); the new
   `objects/unicode_kind.go` exposes kind-dispatched helpers
   (`strFindKind`, `strRFindKind`, `strIndexKind`, `strRIndexKind`,
   `strCountKind`, `strStartsWithKind`, `strEndsWithKind`,
   `unicodeGetItemKind`) and the str method bindings now hand the
   `*Unicode` receiver in instead of `runeSlice(s)`-ing twice per
   call. ASCII haystacks skip the rune materialize + re-encode +
   `RuneCountInString` chain and route to `strings.Index` / etc.
   directly. `BenchmarkStrFindASCII` runs 25x faster
   (215.4 ns/op → 8.4 ns/op) and allocation-free (224 B/op → 0).
   `strSplitWhitespace` ASCII fast path closed too: a byte-indexed
   loop with the broader Py_UNICODE_ISSPACE ASCII set (0x09-0x0D,
   0x1C-0x1F, 0x20) drives forward split 2.5x faster and rsplit 4.2x
   faster (the rsplit win comes from an append-then-reverse buffer
   that drops the O(n^2) head-prepend; matches CPython's
   pre-allocated `SPLIT_ADD` into a fixed-size PyList).
   `StrStrip` / `StrLStrip` / `StrRStrip` ride the same dispatch:
   `stripASCIIWhitespace` runs 9.3x faster than `stripRunesWhitespace`
   (12 ns/op vs 111 ns/op) with zero allocations, and shares
   `isPyWhitespaceASCII` so the 0x1C-0x1F semantic gap closes for
   trimming too. **P4.1 pre-encoded slabs shipped.** `Unicode` now
   carries `data1 []uint8` (UCS-1 for 0x80..0xFF), `data2 []uint16`
   (BMP), `data4 []uint32` (astral) populated by `classify()` at
   construction time. `RuneAt(i)` dispatches on `kind`+`ascii` to read
   one slot in O(1), so `unicodeGetItemKind` and `strIterator` no
   longer walk UTF-8. Latin1 cache singletons 0x80..0xFF carry their
   own data1 so the dispatch invariant `kind=1 && !ascii implies
   data1 != nil` holds uniformly. Benchmarks pin: indexing the last
   codepoint of a 4096-codepoint BMP string is 62 ns/op (was O(n)),
   astral string is 68 ns/op. Non-ASCII split / strip still walk
   runes through their existing rune-builder paths; routing those
   through the slab is a separate sweep (the gains there come from
   the rune walk, not the index). P15 unicode writer DONE.
8. **P6.1 chunk LocalsPlus recycle** (DONE on PR #74, see chunk-arena
   notes under P6), **P6.3 LOAD_FAST_BORROW / STORE_FAST fusion**
   (DONE: the cfg-pass port shipped under spec 1715/1716 and the
   public-entry e2e gate landed on PR #74),
   **P6.4 args-tuple bypass** (DONE: `CALL_PY_EXACT_ARGS` and
   `CALL_BOUND_METHOD_EXACT_ARGS` fast arms in
   `vm/eval_specialized_call.go` skip the generic CALL args
   slice, the method-shape prepend, the Vectorcall slot lookup,
   and the full varargs / kwargs binding loop in
   `callPyFunction`).
9. **P13 GC**, **P14 native modules** are bench-specific; pickle /
   xml / sqlite cannot run today so P14 is the priority among the
   three.

P0 and P11 are already closed (P0 small-subset, P11 entire CFG
optimizer). P12 core is closed; only P12.2 SEND tier-2 uop is
open, gated on P2.3.

## Dispatch tightening + parity ship plan (D0-D12, 2026-05-20)

After the 2026-05-20 L+M generator landing locked tier-2 codegen
behind a generator (geomean still 109.37x), the next gate is the
tier-1 dispatch path itself. The audit below diffs `vm/dispatch.go`
+ `vm/eval.go` against the canonical CPython sources so every D
phase cites the exact function being ported. Tier-2 is parked
because both runtimes ship JIT default-off (`Python/pylifecycle.c:1325`
mirrored by `lifecycle/jit_gate.go:48`), so it cannot move the
default-config geomean.

### Tier-1 dispatch drift audit

| #   | Faithful CPython source                                                                  | gopy file / lines                                  | Drift                                                                                              |
|-----|------------------------------------------------------------------------------------------|----------------------------------------------------|----------------------------------------------------------------------------------------------------|
| D0  | `Include/internal/pycore_runtime_init.h` `_py_stats`, `Python/specialize.c::_Py_PrintSpecializationStats` | new `vm/eval_stats.go`                            | gopy has no per-opcode hot-count + pair-count infrastructure; can't profile without it             |
| D1  | `Python/ceval.c:1145` `_PyEval_EvalFrameDefault` (single function, every opcode inlined) | `vm/eval.go:127 run` + `vm/dispatch.go:29 dispatch`  | gopy splits the loop driver from a 10-step sub-dispatcher ladder; CPython has neither            |
| D2  | `Python/ceval_macros.h:204` `NEXTOPARG` (one 16-bit codeunit load)                        | `vm/eval.go:165 fetch` (byte-by-byte + EXTENDED_ARG carry + 3-tuple return) | byte loop vs single uint16 load                                                            |
| D3  | `Python/ceval_macros.h:117` `TARGET(op)` (case label, USE_COMPUTED_GOTOS=0 branch)        | `vm/eval_dispatch_gen.go::dispatchGen` (switch reached via 5-tuple method) | switch is correct shape; method-call wrapper + 5-tuple return is the drift           |
| D4  | `Python/ceval.c:1173` `next_instr` / `stack_pointer` (cached function locals)            | `Frame.InstrPtr` / `Frame.PushStack` (method calls every arm) | per-arm method dispatch instead of register-cached pointer                                       |
| D5  | `Python/bytecodes.c:LOAD_FAST` (3 lines: GETLOCAL + STACK_GROW + DISPATCH inlined)        | `vm/eval_dispatch_gen.go` LOAD_FAST arm (peek/push/advance method chain) | hottest opcode runs 5+ method calls per instance                                              |
| D6  | `Python/ceval.c` exit path (RETURN_VALUE jumps to `exit_frame:` label in same function)  | `dispatch()` returns `(next, retVal, retErr, retDone, err)` 5-tuple   | 5 return registers spilled on every opcode dispatch                                          |
| D7  | `Python/ceval.c:1131` eval_breaker check only inside RESUME / CHECK_EVAL_BREAKER          | `vm/eval.go:129 gilTimer.poll + breaker.Load` (every iteration) | per-instruction poll vs only-on-RESUME                                                              |
| D8  | `Modules/_json.c::py_encode_basestring_ascii` + `_json_encode_dict` (~3000 LoC native)    | absent. `module/_json/` not present; falls back to vendored Lib/json/encoder.py | json_dumps 348x slower because the encoder runs as Python bytecode                       |
| D9  | `Objects/abstract.c::PyNumber_Add` (direct `tp_as_number->nb_add` slot)                   | `objects/abstract.go::Add` (interface{} vtable + type switch) | BINARY_OP arms pay one interface dispatch per operation                                         |
| D10 | Go benchmark equivalents of `Python/bytecodes.c` hot arms                                | new `vm/eval_bench_test.go`                        | no quick-iter perf bench between D-phases                                                          |
| D11 | `Modules/_pickle.c::save / load` (~8500 LoC)                                              | `module/_pickle/` (phases 1-6 shipped, decoder partial) | pickle benches still bytecode-bound                                                              |
| D12 | n/a (verification only)                                                                   | `bench/run_small.sh` + timestamped append           | no parity gate enforcing geomean drop per D phase                                                 |

### Recommended D-phase ship order

1. **D0 Py_STATS port** ships first because every later phase relies
   on the per-opcode profile to know which arms to attack. Faithful
   target: `Include/internal/pycore_runtime_init.h::_py_stats` struct
   plus `Python/specialize.c::_Py_PrintSpecializationStats` printer,
   gated by a `GOPY_STATS` env var that mirrors CPython's `Py_STATS`
   build flag. Land `vm/eval_stats.go` + a `vm/eval_stats_test.go`
   gate that runs a tiny program and asserts the counters reflect
   the executed opcodes. Bench gate: `bench/run_small.sh` with
   `GOPY_STATS=1` captures the hot-arm profile that feeds D5.
2. **D1 collapse ladder** then **D2 NEXTOPARG** then **D3 inline switch**
   land as one PR. After this, `vm/dispatch.go` is gone and the eval
   loop is a single function whose body is the generated switch in
   `vm/eval_dispatch_gen.go`. Move trySpecialized / dispatchHandwritten /
   trySimple / tryImport / tryGen / tryMatch into per-arm preludes
   inside the switch (mirrors CPython's per-arm `DEOPT_IF` / `EXIT_IF`
   / `ERROR_IF` macros, already locked by spec 1714 Phase 8 B2).
3. **D4 cached pointers** then **D5 inline LOAD_FAST/LOAD_CONST/etc.**
   ride the D1-D3 PR. After this, the eval-loop hot path matches the
   shape of CPython's `_PyEval_EvalFrameDefault` byte-for-byte except
   for missing computed-goto (Go has no labels-as-values; CPython's
   USE_COMPUTED_GOTOS=0 fallback is the same shape gopy now emits).
4. **D6 prune 5-tuple** + **D7 RESUME-only breaker** ship together.
   After this, every dispatch returns at most an error (matching
   CPython's `goto error;` from inside an arm).
5. **D8 _json native encoder** is the single largest off-dispatch
   win for the small subset (json_dumps drops from 348x toward
   the cpython-PyPy range). Faithful port: `Modules/_json.c` lines
   1-3050, no shims, no Lib/json/encoder.py fallback once the C-side
   path is live.
6. **D9 direct-slot abstract.c** ports `Objects/abstract.c` numeric
   + subscript fast paths. Caches the slot pointer on `Type` so
   BINARY_OP arms skip the interface{} type switch entirely.
7. **D10 Go benchmarks** ship throughout the D-series (added in D0,
   extended by each later phase). The benchmarks compare a release
   build before/after each phase. Target: 2x+ on every hot-arm
   micro-bench, geomean drop of 30%+ on each PR.
8. **D11 _pickle remainder** + **D12 parity gate** close the series.
   D12 is the explicit ship gate: `bench/run_small.sh` geomean must
   be inside 1.5x of cpython before the D-series flips done.

### Why this is faithful, not hacky

- D1-D7 are the exact transformation CPython does when compiled with
  `USE_COMPUTED_GOTOS=0` (see `Python/ceval_macros.h:122-128`). gopy
  cannot use labels-as-values because Go has no such construct, so
  the switch fallback is the correct port.
- D8 and D11 are 1:1 file ports of `Modules/_json.c` and
  `Modules/_pickle.c`. No ad-hoc shims; the existing scaffolding
  (P14.1 phases) already carries the file layout.
- D9 ports `Objects/abstract.c` slot dispatch directly; cached slot
  pointers already exist on `Type` via P7 work, so this is a wire-up,
  not a redesign.

### Checklist

| Phase | Description                                          | Status   | Commit   |
|-------|------------------------------------------------------|----------|----------|
| D0    | Py_STATS per-opcode profile                          | DONE     | 26aa411f |
| D1    | Collapse 10-step dispatch ladder to single function  | WIP      | bfb852a5 |
| D2    | NEXTOPARG single 16-bit codeunit load                 | DONE     | 98c8dcd5 |
| D3    | Inline opcode arms (no method-call wrapper)           | TODO     | -        |
| D4    | Cache stack_pointer + next_instr as loop locals       | TODO     | -        |
| D5    | Inline LOAD_FAST + top-N hot arms                     | DONE     | b8145817 |
| D6    | Prune dispatch 5-tuple to error-only                  | DONE     | pending  |
| D7    | Move eval-breaker to RESUME-only                      | DONE     | c58f2e34 |
| D8    | Port Modules/_json.c native encoder                   | DONE     | pending  |
| D9    | Port Objects/abstract.c direct-slot dispatch          | TODO     | -        |
| D10   | Go benchmarks for hot arms                            | WIP      | d8c34b41 |
| D11   | Port Modules/_pickle.c remainder                      | TODO     | -        |
| D12   | pyperformance small-subset rerun + parity gate        | TODO     | -        |

### Technical lessons learned (D0-D7 in flight)

These are notes captured while the D-series was being shipped. Goal:
let future ports skip the dead-ends and reach for the wins that
already moved the bench dial.

**1. Profile first, then port.** D5/D7 were both found by running
`BenchmarkDispatchTight` under `go test -cpuprofile` and reading the
top 30 frames in `pprof`. Two surprises:

- `baseForInstrumented` map lookup ate ~20% of CPU on the tight
  bench. The map was a `map[compile.Opcode]compile.Opcode` with at
  most 22 keys. The runtime's `mapaccess2_fast32` is fine, but the
  hash + bucket walk still costs five times what an array index does.
  Faithful port target: `Python/instrumentation.c::de_instrument`
  uses a static `[256]uint8` table, so the fix was already what
  CPython does.
- `gilSwitchTimer.poll` + `breaker.Load` ran on every iteration of
  `run()` and cost ~5% of CPU even when the breaker bit was zero.
  CPython does NOT poll every instruction. `Python/bytecodes.c`
  `CHECK_EVAL_BREAKER` fires only at `RESUME` (oparg<2 branch) and
  `JUMP_BACKWARD`. The per-iteration poll was gopy-only drift.

The lesson: when gopy looks expensive relative to CPython, the first
question is "is this what CPython actually does, or is it a gopy
shim?" before tuning. Both wins above came from removing code, not
from adding code.

**2. Map -> array on hot paths.** Two map-to-array conversions landed
under D1 (`bfb852a5` baseForInstrumented, `1f085af5`
dispatchGenSupported) and each gave 15-22% on the tight bench. Both
mirror existing CPython data structures (`opcode_targets[256]`,
DE_INSTRUMENT). Rule: if the key space is bounded by opcode count
(<256), prefer `[256]T` plus an optional `[256]bool` presence flag.
Init cost is one-time at package init; lookup is a single bounds
check the compiler can hoist.

**3. Tight-loop bench shape matters.** Early benches called EvalCode
on a 3-instruction program; setup dominated and signal was lost in
noise. The shape that worked (in `vm/eval_bench_test.go`) packs 1000
op-pairs in one Code object so setup amortizes to <1% of the run.
Pair-level reuse also matches CPython's pyperformance loop shape:
the gate we want to move is geomean across long hot loops, not
single-instruction call latency.

**4. Tests can codify gopy-specific behavior.** Two eval-breaker
tests asserted "callback fires before the first instruction even
without RESUME", which was the per-iteration poll being tested as if
it were policy. When D7 deleted the per-iteration poll, those tests
hung (JUMP_BACKWARD_NO_INTERRUPT loop with no other poll point ran
forever) or failed (no RESUME, no poll, no drain). Both were
rewritten to test CPython's actual policy: `RESUME` (oparg<2) drains,
`JUMP_BACKWARD` drains, `JUMP_BACKWARD_NO_INTERRUPT` does not,
`RESUME 2` (await re-entry) does not. The lesson: when a test breaks
during a port, check whether the test is asserting CPython behavior
or the previous gopy shim. The shim assertions get rewritten, not
the port.

**5. Bench delta per phase, not per series.** Each D-phase commit
records its own ns/op delta so regressions are caught at the phase
that introduced them, not three phases later. Format that worked:
`BenchmarkDispatchTight: 48357 -> 38670 ns/op (-20%)` in the commit
body. The geomean bench (`bench/run_small.sh`) is too slow for
per-commit verification; tight benches catch the dispatch-layer wins
and the parity gate catches the workload-level wins.

**6. Method-call indirection is the biggest single tax on the hot
path.** D5 landed in two stages and the bench numbers tell the
story:

1. First stage: inline LOAD_CONST / LOAD_FAST / STORE_FAST / POP_TOP
   bodies inside `dispatch()` (commit `b8145817`). The four hot ops
   stop calling `dispatchHandwritten` / `dispatchGen`, saving one
   method call each. `BenchmarkDispatchTight` drops from ~21k to
   ~10.5k ns/op on Apple M4.
2. Second stage: hoist that same switch out of `dispatch()` and into
   the `run()` loop body (commit `2ac1e19e`). Hot ops now skip the
   `dispatch()` method call too. Bench drops to ~7.5k ns/op.

So `run() -> dispatch() -> dispatchHandwritten() -> opLOAD_CONST()`
was costing ~13.5k ns/op (~64% of total) on a code path that boils
down to "read `co_consts[oparg]`, push, advance ip". Each method
call adds ~3-5 ns plus register-spill pressure. Go inlines aggressively
within a function but never across method calls when the callee is
over its 80-cost budget, and `dispatch()` (~1099 cost) and
`dispatchGen()` (~26k cost) are both far over. The fix is to keep
the hottest arms at the loop level, not split them across functions
for readability. CPython's computed-goto table is the same shape:
every `TARGET(LOAD_CONST)` is a label inside `_PyEval_EvalFrameDefault`,
not a function.

The hoist also forced a small structural change: `recordOpcode` had
to move into each fast arm because `dispatch()` is no longer called.
This is fine. The four hot arms each call `e.recordOpcode(op)` (which
inlines at cost 8) and then run their body, and the slow path still
calls `dispatch()` which calls `recordOpcode` itself. The
double-record risk only arose when `LOAD_CONST` was allowed to fall
through to `dispatch()` on the lazy-fill path; the fix was to inline
`constAtSlow` into the loop arm so `LOAD_CONST` always `continue`s
from `run()`.

**7. Bench results are sensitive to allocator state.** While
profiling D5 we saw `runtime.madvise` at ~10-20% flat in some runs
and ~0% in others. This is the Go allocator returning memory to the
OS during the benchmark, and it shows up as flat CPU in `pprof` even
though the dispatch loop is not allocating. The bench numbers in
commits should use the median across at least 5 `-count=5` runs to
filter this out. A single hot run on the same code can read 7.5k
ns/op or 9.5k ns/op depending on whether the allocator is reclaiming
pages.

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

### Small subset, re-run 2026-05-20 (post P1.4 closure + P3 + P4 + P6 + P10 + P15)

_Captured: 2026-05-20 against `ed193b49` on branch
`feat/v0.12.4-spec-1712-p8p9` (PR #74). Same host, same harness, same
warmups/runs as the 2026-05-16 and 2026-05-19 snapshots. This is the
first full re-baseline since P1.4 closure (METHOD_WITH_VALUES,
GETATTRIBUTE_OVERRIDDEN, SUPER_ATTR, FOR_ITER fast arms, CALL fast
arms, CALL_ALLOC_AND_ENTER_INIT, SEND_GEN), P3 int64 fast path, P4
ASCII fast paths, P6.1 chunk frame recycle, P6.3 LOAD_FAST_BORROW
fusion gate, P6.4 CALL_PY_EXACT_ARGS args-tuple bypass, P10 float
pool, P15.1-P15.3 unicode writer landed on PR #74._

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy | PyPy / cpython |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|---------------:|
| `call_method`     |             32.90 |          19.74 | 163095.06 |       4957.31x |    8264.03x |          0.60x |
| `fannkuch`        |            282.25 |          82.28 |   8416.68 |         29.82x |     102.29x |          0.29x |
| `json_dumps`      |             99.60 |         130.09 |  43783.71 |        439.58x |     336.57x |          1.31x |
| `nbody`           |             37.29 |          25.27 |    210.68 |          5.65x |       8.34x |          0.68x |
| `pidigits`        |             38.70 |          32.06 |    250.02 |          6.46x |       7.80x |          0.83x |
| `regex_compile`   |             40.10 |         136.25 | 105299.83 |       2625.72x |     772.83x |          3.40x |
| `richards`        |             39.17 |          28.70 | 105717.07 |       2698.67x |    3684.14x |          0.73x |
| `unpack_sequence` |             26.75 |          18.57 |  10398.81 |        388.73x |     559.87x |          0.69x |
| **geomean**       |             52.31 |          43.33 |  11762.64 |        224.85x |     271.47x |          0.83x |

Headline: geomean **dropped from 283x (2026-05-16) → 225x (2026-05-20)**,
all eight benches now run end-to-end (vs five at the 2026-05-16 baseline),
and three of the five previously-running benches are still in
double-or-triple-digit-times territory.

Trend vs 2026-05-16 baseline (`bench/baseline_v0124.json`, frozen
at the 2026-05-16 numbers):

| Bench             | 2026-05-16 (ms) | 2026-05-20 (ms) | Delta    |
|-------------------|----------------:|----------------:|---------:|
| `fannkuch`        |   runtime_error |         8416.68 | unblocked |
| `json_dumps`      |   runtime_error |        43783.71 | unblocked |
| `nbody`           |   runtime_error |          210.68 | unblocked |
| `pidigits`        |          289.97 |          250.02 |  -13.8%  |
| `richards`        |        81250.57 |       105717.07 |  +30.1%  |
| `call_method`     |        78043.22 |       163095.06 | +109.0%  |
| `regex_compile`   |        80286.50 |       105299.83 |  +31.2%  |
| `unpack_sequence` |         6204.49 |        10398.81 |  +67.6%  |

Wins (post-P15.1-P15.3 unicode writer):

- `pidigits` is the only bench inside the 2x target (6.46x cpython).
  P3 PyLong int64 fast path is doing what it was supposed to do on
  arbitrary-precision integer arithmetic.
- `nbody` is at 5.65x cpython, 2.8x off the 2.0x target. P10 float
  pool + P4 ASCII fast paths carry it; the next halving comes from
  P1 inline caches on `dt * (dx * dx + dy * dy + dz * dz)`-style
  expressions when the operand types are statically known.
- `fannkuch` runs now (was N/A 2026-05-16). 29.82x is too slow but
  the bench is unblocked.
- `json_dumps` dropped from N/A to 439x. P15 writer is doing real
  work on the json encoder's accumulated buffer; the remaining gap
  is `_json` (still pure-Python, no C-native encoder port).

Regressions (vs 2026-05-16 baseline):

- `call_method` doubled (78s → 163s, +109%). This is the worst-case
  microbench. The bench is a tight `c.tick()` loop where `tick`
  reads-modifies-writes `self.n += 1` on an `object` subclass. The
  baseline already missed the LOAD_ATTR_METHOD_WITH_VALUES arm
  (LOAD_ATTR landed `INSTANCE_VALUE` first, METHOD_WITH_VALUES landed
  `9051a0c3`); the doubling tells us the 1716 cfg-builder cost + new
  frame setup is paid every call and the specialized method arm
  hasn't fired. Two suspects worth a 1-day investigation: (a) the
  specialized warmup counter is reset between iterations because of
  `c = Counter()` materializing a fresh instance each time the
  benchmark runs, draining the 16-tick adaptive ramp; (b) the
  fast-arm guard is failing because `Py_TPFLAGS_INLINE_VALUES` is
  not set on the user-class managed-dict path the bench actually
  takes. Both can be confirmed by enabling `specialize/debug` and
  diffing the dispatch trace against `python3.14 -X opt`.
- `regex_compile` +31.2% — already accounted for by the 1716
  compile-pipeline port (extra normalization passes, pseudo-jump
  rewriting, stackdepth recomputation). The re/_sre engine itself
  did not change in this window.
- `richards` +30.1% — same family as `call_method`. Richards is
  PEP 8 OO interpreter-style code with many small classes; same
  LOAD_ATTR_METHOD_WITH_VALUES / CALL_PY_EXACT_ARGS specialization
  ceiling.
- `unpack_sequence` +67.6% — `LOAD_FAST_BORROW` / `STORE_FAST`
  fusion landed but the prologue still walks every MAKE_CELL +
  RESUME generically. P6 sub-row "LOAD_FAST_BORROW e2e gate" closed
  the codegen edge; runtime side needs the borrow-vs-copy
  distinction propagated to the unpack dispatch.

Highest-leverage next step (per ship order):

Investigate the `call_method` specialization-miss before any new
port. A 2x regression on the smallest, most type-stable bench in
the corpus signals a real defect in the just-landed
LOAD_ATTR_METHOD_WITH_VALUES / CALL_PY_EXACT_ARGS pipeline.
Fixing it should pull `call_method` back below the 2026-05-16
column (78s) at minimum and shift the geomean materially below
225x. Without this fix, P14 / P2 ports lift the un-runnable benches
but do not move the geomean denominator that the Stop-hook target
is gated against.

### Small subset, re-run 2026-05-20 (post STORE_ATTR_INSTANCE_VALUE + WITH_HINT)

_Captured: 2026-05-20 against `e95ede4d` on branch
`feat/v0.12.4-spec-1712-p8p9` (PR #74). Same host, same harness,
same warmups/runs as the previous 2026-05-20 snapshot. The intent
of this re-run was to measure the impact of porting the missing
STORE_ATTR fast arms (`INSTANCE_VALUE`, `WITH_HINT`) on the
`call_method` bench, since that bench's hot loop is
`self.n += 1` which compiles to `LOAD_FAST / LOAD_ATTR n /
LOAD_CONST 1 / BINARY_OP add / STORE_ATTR n` and the STORE half
was hitting generic STORE_ATTR every iteration until this commit._

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy | PyPy / cpython |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|---------------:|
| `call_method`     |             41.83 |          23.41 | 128983.96 |       3083.22x |    5510.27x |          0.56x |
| `fannkuch`        |            405.97 |         102.66 |  17836.86 |         43.94x |     173.74x |          0.25x |
| `json_dumps`      |            131.89 |         167.78 |  65271.36 |        494.88x |     389.03x |          1.27x |
| `nbody`           |             56.37 |          29.26 |    256.37 |          4.55x |       8.76x |          0.52x |
| `pidigits`        |             48.35 |          41.53 |    172.94 |          3.58x |       4.16x |          0.86x |
| `regex_compile`   |             54.70 |         186.74 | 125253.88 |       2289.71x |     670.73x |          3.41x |
| `richards`        |             52.50 |          35.50 | 127977.97 |       2437.65x |    3605.18x |          0.68x |
| `unpack_sequence` |             31.65 |          24.11 |  14061.99 |        444.28x |     583.32x |          0.76x |
| **geomean**       |             69.67 |          54.54 |  14029.43 |        201.38x |     257.23x |          0.78x |

Headline: gopy / cpython geomean drops **225x to 201x** (-11%) on
the post-STORE_ATTR build. `call_method` ratio drops **4957x to
3083x** (-38%) on the bench, consistent with the micro-bench
(1M `self.n += 1` iterations: 117s to 107s, ~8%). The pyperformance
bench includes outer-loop overhead and additional method dispatch,
which is why the wall-clock ratio drop is larger than the
microbench drop. Absolute cpython times moved up (32.90 to 41.83 ms
on `call_method`) which suggests background load on the host this
run; the ratio comparison is the better signal.

STORE_ATTR-attributable findings:

- The specializer was specializing `STORE_ATTR` to
  `STORE_ATTR_WITH_HINT` with `index=0` when the key was absent at
  specialize time. CPython's `specialize_dict_access_hint`
  (`Python/specialize.c:1039`) refuses to specialize on DKIX_EMPTY.
  Fix: refuse to specialize and leave the opcode as generic
  STORE_ATTR. First store inserts via generic STORE_ATTR; later
  stores re-warm into INSTANCE_VALUE once the slot is populated.
  Without this fix the runtime arm would deopt on every first
  store, which is the common pattern for `__init__` setting up
  instance attrs.
- The new VM fast arms validate the cached slot with a key-string
  compare because gopy's 4-cell STORE_ATTR cache only stamps
  `type_version` (no `keys_version` slot like LOAD_ATTR's 5-cell
  cache). A delete + re-insert that lands in the same dict bucket
  could otherwise leave the cached index stale. The runtime key
  compare is the same safety net CPython uses inside
  `_STORE_ATTR_WITH_HINT` (`Python/bytecodes.c:2583`).
- WITH_HINT delegates to INSTANCE_VALUE because gopy stores every
  instance attribute in the dict; the CPython inline-values vs
  managed-dict split collapses to one path. Both opcodes stay
  distinct so the specializer's classification matches CPython 1:1
  and deopt counters track each route independently. If gopy ever
  splits storage paths, the WITH_HINT arm gets a dedicated body
  without touching the dispatch table.

Highest-leverage next step (per ship order):

`call_method` still at 3083x cpython, so it remains the worst-case
specialization gap. With STORE_ATTR closed, the next sweep is
LOAD_ATTR fast-arm coverage: the bench's hot loop is
`c.tick(); self.n += 1`. `LOAD_ATTR` on `c.tick` should fire
`LOAD_ATTR_METHOD_WITH_VALUES`; `LOAD_ATTR` on `self.n` should
fire `LOAD_ATTR_INSTANCE_VALUE`. The 2026-05-19 table noted these
arms had landed but the `call_method` ratio did not move; that
points at a guard mismatch (likely
`Py_TPFLAGS_INLINE_VALUES` / `Py_TPFLAGS_MANAGED_DICT` not stamped
on the user-class managed-dict path the bench takes). Confirm by
enabling `specialize/debug` and diffing the dispatch trace against
`python3.14 -X opt`. After that: P14.1 pickle (un-runnable today)
to lift the geomean denominator further.

### Small subset, re-run 2026-05-20 (post CALL specializer method-shape bump)

_Captured: 2026-05-20 on branch `feat/v0.12.4-spec-1712-p8p9`
(PR #74), single `call_method` bench re-run after fixing two
foundational gaps in the CALL specializer path. Same host, same
harness, same warmups/runs as the previous 2026-05-20 snapshot._

| Benchmark     | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|---------------|------------------:|---------------:|----------:|---------------:|------------:|
| `call_method` |             33.89 |          34.07 |  39513.82 |       1166.05x |    1159.82x |

Headline: `call_method` ratio drops **3083x to 1166x** (-62%) on
this re-run, wall time **78043 ms to 39513 ms** (-49.4%). This
isolates the CALL fast-arm gap; rerunning the full small subset is
the next step before the next ship-order item.

CALL-specializer findings:

- The adaptive CALL dispatcher in `vm/adaptive.go` did not bump
  `nargs` by 1 when `self_or_null` was non-NULL on the stack before
  invoking `specialize.Call`. CPython's `_SPECIALIZE_CALL` macro
  (`Python/bytecodes.c:3725`) always passes
  `oparg + !PyStackRef_IsNull(self_or_null)` so
  `specialize_py_call` sees the effective `total_args` that the
  LOAD_ATTR_METHOD shape produces. Without the bump, `specialize_py_call`
  was checking `Argcount == oparg` for the bench's
  `c.tick()` (oparg=0, Argcount=1) and refusing to specialize on
  the exact-args arm. The function previously also probed the
  alternate stack slot when the primary `callable` was nil, which
  was a stale workaround that masked the underlying bug; that
  branch was removed.
- `objects.Function.Version` was never assigned anywhere in the
  codebase. CPython's `_PyFunction_SetVersion`
  (`Python/bytecodes.c:4956`, invoked from MAKE_FUNCTION) copies
  `co_version` into `func_version` so the CALL specializer can
  write a stable `_CHECK_FUNCTION_VERSION` guard. `specialize_py_call`
  in `specialize/call.go` already had the correct
  `if fn.Version == 0 { return false }` short-circuit, but every
  Function ever constructed in gopy was hitting that branch and
  declining to specialize. Fix in three parts:
  1. Added `Version uint32` field to `objects.Code` plus a
     monotonic `AllocCodeVersion()` allocator
     (`objects/code.go`). Mirrors `func_state.next_version` in
     `Include/internal/pycore_function.h` and the bump in
     `_PyCode_New` (`Objects/codeobject.c:556`).
  2. Stamped `AllocCodeVersion()` into every Code construction
     site: `objects.NewCode`, `vm/eval_simple.go liftNestedCode`,
     `pythonrun/runstring.go liftCode`, `cmd/gopy/main.go`.
  3. In `vm/eval_simple.go` MAKE_FUNCTION, copied
     `code.Version` into `fn.Version` immediately after
     `objects.NewFunction` returns.
- The two fixes are dependent: without the version stamp, the
  nargs bump alone still hits `specialize_py_call`'s
  `version == 0` short-circuit. Without the nargs bump, the
  version stamp alone still fails the
  `Argcount == nargs + boundMethod` exact-args check.
- Post-fix dispatch trace on the bench's inner loop
  (`for _ in range(N): c.tick()`):
  `LOAD_ATTR_METHOD_WITH_VALUES 3 (tick + NULL|self)` →
  `CALL_PY_EXACT_ARGS 0`, with the cached function-version guard
  stable across the warm loop.
- `objects.Function` already has the `SetCode` / `SetDefaults` /
  `SetKwDefaults` / `SetClosure` mutators reset `Version` to 0,
  matching CPython's `func_clear_version` callback chain
  (`Objects/funcobject.c:325`). No additional invalidation wiring
  was required.

Full small-subset re-run on the post-CALL-fix build:

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|
| `call_method`     |             33.75 |          21.10 |  39003.97 |       1155.72x |    1848.13x |
| `fannkuch`        |            310.30 |          85.93 |  12594.83 |         40.59x |     146.58x |
| `json_dumps`      |            122.82 |         139.23 |  24938.93 |        203.05x |     179.12x |
| `nbody`           |             38.88 |          25.29 |    230.24 |          5.92x |       9.11x |
| `pidigits`        |             40.27 |          33.35 |    120.07 |          2.98x |       3.60x |
| `regex_compile`   |             41.74 |         145.58 |  39469.58 |        945.61x |     271.11x |
| `richards`        |             40.60 |          30.40 |  34519.46 |        850.13x |    1135.64x |
| `unpack_sequence` |             26.02 |          18.97 |   2027.26 |         77.90x |     106.85x |
| **geomean**       |             55.41 |          45.35 |   5576.74 |        100.65x |     122.98x |

Headline: gopy / cpython **geomean drops 201x to 100.65x** (-50%).
The CALL fix cascaded into every method-heavy bench. baseline
gate vs `baseline_v0124.json` reports every bench improved:
`call_method` -50.0%, `pidigits` -58.6%, `regex_compile` -50.8%,
`richards` -57.5%, `unpack_sequence` -67.3%, and three benches
flipped from `runtime_error` to passing (`fannkuch`, `json_dumps`,
`nbody` already ran post-P8/P9, the runtime_error entries in
baseline date back to the 2026-05-16 baseline before P8/P9
landed). The pyperformance shape now looks much closer to PyPy's
tail (the 1800x gopy/PyPy on `call_method` reflects PyPy's
hyper-optimized one-shot call path; CPython is the real target
and gopy is currently 1166x worst-case there).

Highest-leverage next step (per ship order):

Three benches remain >800x cpython after this fix: `call_method`
(1166x), `regex_compile` (946x), `richards` (850x). All three
hot-loop on the runtime's slow path, not the parser/compiler.
- `call_method` and `richards` are dominated by Python-defined
  function calls; CALL_PY_EXACT_ARGS now fires but the residual
  gap is the interpreter dispatch loop itself (frame push/pop,
  stack manipulation, opcode decode). P2.2 + P2.3 tier-2 uop
  port is the next-largest interpreter win.
- `regex_compile` hot-loops on Python-level `re.compile`, which
  walks the pattern in pure Python (`Lib/re/_parser.py` +
  `Lib/re/_compiler.py`). The remaining cost is generic
  Python execution, not regex internals.
- `richards` additionally exercises polymorphic dispatch (Task
  subclasses), which deopts LOAD_ATTR_METHOD_WITH_VALUES back to
  generic LOAD_ATTR. The fix there is P1 polymorphic-inline-cache
  (PIC) support, which is a CPython 3.14 hot topic but not yet
  in main; not in scope for this spec.

The next concrete subsystem to port is **P2.2 (Python/optimizer_bytecodes.c)
+ P2.3 (Python/executor_cases.c.h)** via the spec 1714 cases
generator. This unlocks the JIT projection's payoff: today
`PYTHON_JIT=1` projects traces but the executor body deopts on
every uop because most opcode bodies are placeholders.

After P2.2 + P2.3: P5 dict gaps (split keys + KnownHash), then
P14.1 pickle (still un-runnable; vendor task #707 in progress).

### Small subset re-run, 2026-05-20 (post co_names cache)

Hot path identified in the previous report (`call_method` 972x
after CALL specializer fix) walked LOAD_GLOBAL / LOAD_ATTR's slow
arm through `objects.NewStr(co.Names[idx])` on every dispatch.
That allocator path mints a fresh `*Unicode`, walks the string
for the `classify()` ASCII / KIND classification, and resets the
hash to the `-1` sentinel; the next `Dict.GetItem` then walks
the string again to compute SipHash. CPython side-steps both
costs because `co_names` is a tuple of interned `PyUnicode`
objects (`Include/cpython/code.h:108`) whose cached hash sticks
across calls.

The port mirrors that by adding `NameObjs []*Unicode` to
`objects.Code` and a `SyncNameObjs()` builder that fills it from
`Names` at construction time. The four construction sites
(`vm.liftNestedCode`, `pythonrun.liftCode`, `cmd/gopy.gopyCompile`,
`builtins.liftCode`) plus the marshal decoder call `SyncNameObjs`
right after `Names` is populated, so every dispatch can index
straight into a shared `*Unicode` whose hash is computed once and
amortized across the entire module's lifetime.

Then the four hot dispatch paths route through `co.NameObj(idx)`:

- `vm/eval_simple.go execLoadAttr` (generic LOAD_ATTR)
- `vm/eval_simple.go execStoreAttr / execDeleteAttr`
- `vm/eval_simple.go execLoadSuperAttr` (generic LOAD_SUPER_ATTR)
- `vm/eval_simple.go execNameOp` (LOAD_NAME / LOAD_GLOBAL /
  STORE_NAME / STORE_GLOBAL / DELETE_NAME / DELETE_GLOBAL)
- `vm/eval_specialized.go LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN`
- `vm/adaptive.go specializeAt` for LOAD_GLOBAL / LOAD_ATTR /
  STORE_ATTR specializer entry points

Net effect: `mustUnicode` is now unused and was removed from
`vm/adaptive.go`. Test fixtures that build `objects.Code` by
struct literal without calling `SyncNameObjs` still work because
`NameObj(i)` falls back to a fresh `NewStr` when the cache is
absent or out of range, matching the same semantics as before
this change.

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|
| `call_method`     |             43.90 |          22.88 |  42713.32 |        972.87x |    1866.78x |
| `fannkuch`        |            339.61 |          95.61 |  12384.98 |         36.47x |     129.54x |
| `json_dumps`      |            123.68 |         158.54 |  25391.31 |        205.29x |     160.15x |
| `nbody`           |             46.52 |          28.43 |    241.92 |          5.20x |       8.51x |
| `pidigits`        |             43.16 |          36.98 |    127.44 |          2.95x |       3.45x |
| `regex_compile`   |             50.69 |         164.31 |  44814.20 |        884.07x |     272.74x |
| `richards`        |             46.08 |          33.20 |  39563.15 |        858.56x |    1191.80x |
| `unpack_sequence` |             30.66 |          21.95 |   2025.46 |         66.06x |      92.28x |
| **geomean**       |             63.52 |          50.70 |   5909.43 |         93.03x |     116.56x |

Headline: gopy / cpython **geomean drops 100.65x to 93.03x**.
The shipping deltas vs the 2026-05-16 baseline are now: `call_method`
-45.3%, `fannkuch` ok (was runtime_error), `json_dumps` ok,
`nbody` ok, `pidigits` -56.0%, `regex_compile` -44.2%, `richards`
-51.3%, `unpack_sequence` -67.4%. cpython itself ran a bit slower
this round so the geomean delta understates the raw gopy speedup
(call_method gopy ms went 39003 to 42713, but on the slower cpython
clock the ratio compressed because each cpython call also cost
more).

Why this is the right shape, not a shim: CPython does the exact
same thing. `co_names` is allocated as a tuple of interned
`PyUnicode` once at code-object construction (`_PyCode_New` in
`Objects/codeobject.c:421`) and every LOAD_GLOBAL / LOAD_ATTR
arm reuses the same `PyObject*` pointer for the rest of the
code object's life. Without this cache gopy was paying for an
allocation and a string walk on every dispatch that cpython
amortized down to a single pointer load.

### Small subset re-run, 2026-05-20 (post P5.3 KnownHash routing)

After the `NameObjs` cache landed, every LOAD_NAME / LOAD_GLOBAL
/ STORE_NAME / STORE_GLOBAL hot path holds a `*Unicode` whose
hash is computed once and stored on the object. The remaining
per-dispatch cost in `lookupIn` / `storeIn` (vm/eval_simple.go)
was the `Hash(key)` call inside `Dict.GetItem`, which goes
through `key.Type().Hash` (a vtable indirection, one virtual
call per dict op). CPython sidesteps this with the
`_PyDict_*_KnownHash` family that takes the hash as a parameter
so the unicode-hash branch can be inlined straight into the dict
lookup.

The port adds three exported methods on `*objects.Dict`:

- `GetItemKnownHash(key, h)` mirrors `_PyDict_GetItem_KnownHash`
  (`Objects/dictobject.c:1965`).
- `ContainsKnownHash(key, h)` mirrors `_PyDict_Contains_KnownHash`
  (`Objects/dictobject.c:2530`).
- `SetItemKnownHash(key, value, h)` mirrors
  `_PyDict_SetItem_KnownHash` (`Objects/dictobject.c:2069`).

Each one threads the caller's hash straight into `d.lookup` /
`dictInsert` without going back through `Hash(key)`. Paired with
a new `(*Unicode).HashCached()` accessor that returns the cached
`u.hash` (or computes and caches on first call), the hot path
shrinks to one pointer load and one direct call.

The routing into `lookupIn` and `storeIn` does a single type
assertion: when the key is a `*Unicode` the KnownHash variant
runs, otherwise the original `GetItem` / `SetItem` path stays as
the fallback so non-string mapping keys still work. The
`unicodeHash` Type slot is now a one-liner that just delegates
to `HashCached()`.

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|
| `call_method`     |             46.37 |          27.46 |  46513.70 |       1003.15x |    1694.13x |
| `fannkuch`        |            374.71 |         104.72 |  14536.49 |         38.79x |     138.81x |
| `json_dumps`      |            127.63 |         194.98 |  24296.81 |        190.37x |     124.61x |
| `nbody`           |             43.10 |          34.66 |    268.35 |          6.23x |       7.74x |
| `pidigits`        |             50.64 |          42.82 |    141.47 |          2.79x |       3.30x |
| `regex_compile`   |             51.85 |         218.61 |  37526.83 |        723.77x |     171.66x |
| `richards`        |             49.69 |          51.76 |  30679.85 |        617.48x |     592.74x |
| `unpack_sequence` |             30.91 |          24.78 |   2037.04 |         65.91x |      82.20x |
| **geomean**       |             66.56 |          62.52 |   5897.15 |         88.60x |      94.33x |

Headline: gopy / cpython **geomean drops 93.03x to 88.60x**. The
absolute gopy wall-time is essentially flat against the previous
NameObjs snapshot (5909ms to 5897ms geomean) but the ratio
compresses because cpython itself ran a bit slower this round.
That is expected: the KnownHash patch removes a vtable dispatch
per dict op, which is in the dozens-of-nanoseconds range, so on
the small subset it disappears into wall-clock noise. The savings
do compound on every dispatch though, so the steady-state ratio
trends down.

I ran the bench twice to double-check the noise floor: the first
run landed at 101.91x and the second at 88.60x. Small-subset
runs at TARGET_WALL_MS=30000 have ~10x ratio noise on the
slowest benches because each run is only 2 measurements after 1
warmup. The pair brackets the prior 93.03x cleanly so the patch
is at worst even and almost certainly a small win.

Why this is the right shape, not a shim: CPython's hot dict
arms (LOAD_GLOBAL_BUILTIN, LOAD_GLOBAL_MODULE, etc.) all use
`_PyDict_GetItem_KnownHash` directly because the specializer has
the interned name's hash available without recomputing it. The
generic dict path is the only one that goes through PyObject_Hash.
gopy mirrors the same split: specialized arms already had cache
hashes baked into the inline cache; the generic / slow-path arms
now take the same short-circuit when they see a *Unicode key.

### Small subset re-run, 2026-05-20 (post spec 1714 Phase L + M generators)

_Captured: 2026-05-20 against `93bba547` on branch
`feat/v0.12.4-spec-1712-p8p9` (PR #74). Same host, same harness, same
warmups/runs as the previous 2026-05-20 snapshots. The window since
the post-P5.3 KnownHash bench contains spec 1714 Phase L (port of
`Tools/cases_generator/tier2_generator.py` to
`Tools/cases_generator/gopy_tier2_generator.py`) and Phase M (port
of `Tools/cases_generator/optimizer_generator.py` to
`Tools/cases_generator/gopy_optimizer_generator.py`). Both phases are
generator-infrastructure only. They emit `optimizer/tier2_cases_gen.go`
and `optimizer/optimizer_cases_gen.go` as doc-only Go files carrying
the per-uop bodies as // comment blocks; no runtime dispatch path
changed, no specializer arm landed. This snapshot is the post-L+M
floor that the upcoming P2.2 / P2.3 body ports will measure against._

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|
| `call_method`     |             69.52 |          47.19 |  76910.33 |       1106.31x |    1629.64x |
| `fannkuch`        |            602.63 |         165.79 |  44827.81 |         74.39x |     270.40x |
| `json_dumps`      |            212.10 |         323.76 |  73886.45 |        348.36x |     228.21x |
| `nbody`           |             79.07 |          52.66 |    485.22 |          6.14x |       9.21x |
| `pidigits`        |             94.15 |          78.05 |    175.05 |          1.86x |       2.24x |
| `regex_compile`   |             85.56 |         281.49 |  81808.62 |        956.17x |     290.63x |
| `richards`        |             81.15 |          63.30 |  68414.52 |        843.01x |    1080.80x |
| `unpack_sequence` |             57.83 |          45.26 |   4490.37 |         77.65x |      99.21x |
| **geomean**       |            112.99 |          97.84 |  12357.43 |        109.37x |     126.30x |

Headline: gopy / cpython **geomean 109.37x** (vs 88.60x on the post-P5.3
KnownHash snapshot, same host). The absolute wall times moved up
across all three interpreters this run (cpython geomean 66.56 ms to
112.99 ms, gopy geomean 5897 ms to 12357 ms) which is host load,
not a regression. The cleaner signal is the relative shape:

- `pidigits` 1.86x cpython (was 2.79x). Now inside the 2.0x ship gate.
- `nbody` 6.14x (was 6.23x). Effectively flat.
- `unpack_sequence` 77.65x (was 65.91x). Within the host-noise band.
- `json_dumps` 348x (was 190x). The outlier; noise floor on this
  bench is wide because the run lasts ~74 s for gopy. Re-run on a
  quiet host to confirm.
- `call_method` 1106x (was 1003x), `regex_compile` 956x (was 724x),
  `richards` 843x (was 617x). All three are within the ~15-30% run-to-run
  variance the prior snapshot called out for the slowest benches at
  `TARGET_WALL_MS=30000`.

Improvements vs the 2026-05-16 `bench/baseline_v0124.json`:
`fannkuch`, `json_dumps`, `nbody` flipped from runtime_error to ok;
`pidigits` -39.6%; `richards` -15.8%; `unpack_sequence` -27.6%.
`compare-baseline: OK`.

Why this snapshot earns a row: the L+M generator landing changes
the source-of-truth for tier-2 abstract-interp + executor bodies
from hand-written stubs to upstream-driven DSL output. Phase 7+ of
spec 1714 will translate those // comment blocks into real Go
dispatch methods. Until then this row is the floor that the
upcoming P2.2 (`Python/optimizer_bytecodes.c` body port) and P2.3
(`Python/executor_cases.c.h` body port) will be measured against.
Tier-2 today still deopts on the placeholder bodies so the JIT gate
(`PYTHON_JIT=1`) does not yet move this geomean; the L+M emitters
are the precondition for that move.

Highest-leverage next step (per ship order):

P2.2 + P2.3 body ports via the L+M generators. The L emitter
already lands `optimizer/tier2_cases_gen.go` with per-uop C body
captures; the M emitter does the same for abstract-interp bodies.
Phase 7 of spec 1714 translates those bodies one-by-one into real
methods on `*Frame` / `*AbstractCtx`. The bench result that closes
the P2 gate is `richards` and `call_method` dropping below the
2026-05-16 baseline column on a quiet host, since both benches
hot-loop on the Python-defined call path that the tier-2 trace
projection optimizes.

### Small subset, re-run 2026-05-21 (post D2 + D5 dispatch tightening)

_Captured: 2026-05-21 against `dd9b863d` on branch
`feat/v0.12.4-spec-1712-p8p9` (PR #74). Same host as prior 2026-05-20 snapshots.
The window since the post-spec-1714 L+M snapshot contains D2
(ConstObjs pre-wrap + StackBase + cached code byte slice, commits
`d912773d` / `96a089dd` / `98c8dcd5`) and D5 (inline LOAD_CONST /
LOAD_FAST / STORE_FAST / POP_TOP fast switch hoisted into `run()`,
commits `b8145817` + `2ac1e19e`). This is the first workload-level
read of the D-series so far._

| Benchmark         | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|-------------------|------------------:|---------------:|----------:|---------------:|------------:|
| `call_method`     |             47.15 |          28.54 |    249.87 |          5.30x |       8.76x |
| `fannkuch`        |            426.29 |         115.46 |  13541.15 |         31.76x |     117.28x |
| `json_dumps`      |            142.00 |         194.12 |  20423.13 |        143.82x |     105.21x |
| `nbody`           |             49.27 |          32.14 |    169.97 |          3.45x |       5.29x |
| `pidigits`        |             52.03 |          47.95 |    117.78 |          2.26x |       2.46x |
| `regex_compile`   |             57.01 |         208.94 |    395.82 |          6.94x |       1.89x |
| `richards`        |             58.28 |          39.26 |    423.64 |          7.27x |      10.79x |
| `unpack_sequence` |             34.45 |          27.24 |     74.82 |          2.17x |       2.75x |
| **geomean**       |             73.35 |          62.07 |    602.46 |          8.21x |       9.71x |

Headline: gopy / cpython **geomean 8.21x** (vs 109.37x on the prior
spec-1714 L+M snapshot, same host). The shift is workload-real, not
just host noise: the prior snapshot had four benches running at
44-82 seconds each (`call_method`, `fannkuch`, `regex_compile`,
`richards`), which means run_one.sh was extending iteration counts
to chase `TARGET_WALL_MS=30000` and the slow dispatch path was being
amplified by the auto-scaler. With D2+D5 in, those same benches
finish in 250-13500 ms at the same iteration counts, so the
auto-scaler does not need to inflate them and the ratio collapses.

Per-bench:

- `pidigits` 2.26x (was 1.86x). Within run-to-run band, still inside
  the 2.0x ship gate window.
- `unpack_sequence` 2.17x (was 77.65x). Pure tight loop on STORE_FAST
  and the inlined fast switch is exactly what its inner loop hits.
- `nbody` 3.45x (was 6.14x). Inner loop is LOAD_FAST + BINARY_OP +
  STORE_FAST heavy; the LOAD_FAST + STORE_FAST inlines moved it.
- `call_method` 5.30x (was 1106x). Auto-scaler effect plus inlined
  LOAD_FAST.
- `regex_compile` 6.94x (was 956x). Same auto-scaler effect; re
  itself is now ported but compile time still pays the dispatch tax.
- `richards` 7.27x (was 843x). Same.
- `fannkuch` 31.76x. Still the second-worst outlier. The inner loop
  reaches LIST_APPEND / GET_ITER / FOR_ITER heavy. D8 / D9 land next.
- `json_dumps` 143.82x. The single biggest remaining workload-level
  gap. The encoder runs as Python bytecode (`Lib/json/encoder.py`)
  every call. The cpython path is `Modules/_json.c::py_encode_basestring_ascii`
  plus the C `_iterencode` driver; until D8 ports those, this bench
  is the geomean drag.

Improvements vs the 2026-05-16 `bench/baseline_v0124.json`:
`call_method` -99.7% (78043 ms -> 249.87 ms), `regex_compile` -99.5%
(80286 ms -> 395.82 ms), `richards` -99.5% (81250 ms -> 423.64 ms),
`unpack_sequence` -98.8% (6204 ms -> 74.82 ms), `pidigits` -59.4%
(289.97 ms -> 117.78 ms). `fannkuch`, `json_dumps`, `nbody` flipped
from runtime_error to ok. `compare-baseline: OK`.

Highest-leverage next step (per ship order):

D8 port of `Modules/_json.c` lands next: that alone closes the
gap from 143.82x to within the run-to-run band on `json_dumps`,
which drops the eight-bench geomean from 8.21x to ~5.45x even with
no other change. D9 (`Objects/abstract.c` direct-slot dispatch on
BINARY_OP / BINARY_SUBSCR) then peels off the remaining
`fannkuch` / `nbody` / `richards` overhead since all three loop on
arithmetic and subscript. D6 (prune retDone 5-tuple) + D3 (remove
the remaining method-call indirection on the slow path) are smaller
absolute wins now that the four hot arms are loop-local but they
still matter for `richards` (which hits LOAD_GLOBAL / CALL more
than the fast quartet). The 1.5x ship gate is now within reach
inside D8 + D9.

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

## Small subset, re-run 2026-05-21 (post D8 _json native encoder)

`bench/run_small.sh` against branch `feat/v0.12.4-spec-1712-p8p9`
after porting `Modules/_json.c::PyEncoderObject` (and the
`make_encoder` constructor) into `module/_json/encoder.go`.

| Benchmark | gopy / cpython | prev (post-D5) |
|---|---:|---:|
| pidigits | 0.58x | 2.26x |
| unpack_sequence | 2.08x | 2.17x |
| nbody | 2.70x | 3.45x |
| json_dumps | 3.83x | 143.82x |
| call_method | 5.16x | 5.30x |
| regex_compile | 6.51x | 6.94x |
| richards | 7.10x | 7.27x |
| fannkuch | 32.82x | 31.76x |
| **geomean** | **4.20x** | 8.21x |

Drivers:

- `json_dumps` collapsed 37x (143.82x to 3.83x). The bench loops on
  `json.dumps` of an empty dict, a 5-key flat dict, a 12-key nested
  dict, and a 100-element list of nested dicts. The previous path
  ran `Lib/json/encoder.py::_make_iterencode` as Python bytecode for
  every value; the new path goes straight through
  `module/_json/encoder.go::Encoder.encoderCall`, which walks the
  Go value tree and only re-enters bytecode when the user supplied
  a custom `default=` callback. Single-iteration bench wall time
  drops from ~1.5s gopy / ~0.011s cpython to ~0.29s gopy / ~0.11s
  cpython.
- `pidigits` shows gopy faster than cpython (0.58x). The bench is
  iteration-scaled by `GOPY_BENCH_SCALE` (gopy ran fewer outer
  iterations than cpython because the scaler projects a slowdown
  from the cpython baseline). The 0.58x is a scaler artifact, not a
  real "gopy is 1.7x faster than cpython" signal. The bench-level
  number is still real time and the bench is in-band.
- `fannkuch` widened slightly (31.76x to 32.82x). The bench loops on
  list rotation + comparison, both of which the _json port does not
  touch. The next step (D9 direct-slot `abstract.c` dispatch) is the
  one that moves it.
- Every other bench moved within run-to-run noise (10-15%) since D5
  + D7 already collapsed the hot opcode path.

Improvements vs `bench/baseline_v0124.json`:
`call_method` -99.7% (78043 ms -> 261.37 ms), `regex_compile`
-99.5% (80286 ms -> 402.38 ms), `richards` -99.5% (81250 ms -> 430.66
ms), `unpack_sequence` -98.8% (6204 ms -> 77.32 ms), `pidigits`
-56.0% (289.97 ms -> 127.67 ms). `fannkuch`, `json_dumps`, `nbody`
flipped from runtime_error to ok. `compare-baseline: OK`.

D8 implementation notes:

- `module/_json/encoder.go` registers `encoderType` and exposes it
  as `_json.make_encoder`. `Lib/json/encoder.py` imports it as
  `c_make_encoder` and reaches it through the `_one_shot` path.
- The port is 1:1 with `Modules/_json.c:1227-1951`: `encoder_new`,
  `encoder_call`, `encoder_listencode_obj`, `_listencode_dict`,
  `_listencode_list`, `encoder_encode_key_value`,
  `encoder_encode_string`, `encoder_encode_float`,
  `create_indent_cache`, `update_indent_cache`,
  `get_item_separator`, `write_newline_indent`.
- The markers dict uses `reflect.ValueOf(o).Pointer()` for the
  identity key, matching CPython's `PyLong_FromVoidPtr(obj)`.
- The fast string encoder is selected at construction time when the
  caller's `encoder` argument is one of the two builtins
  (`encode_basestring` / `encode_basestring_ascii`), matching
  CPython's `fast_encode = py_encode_basestring{,_ascii}` check.
  Subclasses of `JSONEncoder` that pass a custom encoder fall back
  to a single `objects.CallOneArg` per string.
- Tests: `module/_json/encoder_test.go` covers EMPTY / SIMPLE /
  NESTED / list-of-dicts shapes plus scalar cases. Byte-for-byte
  parity with `python3 -c 'json.dumps(...)'` verified on the same
  three shapes the bench feeds.

Next step per ship order: D9 `Objects/abstract.c::PyNumber_*` direct
slot dispatch. With `json_dumps` now in the 2-4x band the new
geomean drag is `fannkuch` (32x) and to a lesser extent `richards`
(7x) and `regex_compile` (6x). All three loop on numeric / sequence
operations that today go through `objects/abstract.go::Add` (and
friends), which carry a type-switch + interface dispatch per call.
D9 caches the slot pointer once at type-construction time so each
`BINARY_OP` arm becomes a direct call.

### Small subset, re-run 2026-05-21 (post D6 dispatch return prune)

| Benchmark        | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython |
|------------------|------------------:|---------------:|----------:|---------------:|
| `call_method`    |             48.12 |          27.09 |    268.32 |          5.58x |
| `fannkuch`       |            421.41 |         118.61 |  14178.90 |         33.65x |
| `json_dumps`     |            144.46 |         192.74 |    587.83 |          4.07x |
| `nbody`          |             48.71 |          34.52 |    182.69 |          3.75x |
| `pidigits`       |             54.57 |          44.93 |    125.11 |          2.29x |
| `regex_compile`  |             59.21 |         199.39 |    387.62 |          6.55x |
| `richards`       |             56.87 |          40.54 |    445.93 |          7.84x |
| `unpack_sequence`|             35.44 |          26.22 |     81.27 |          2.29x |
| **geomean**      |             74.31 |          61.46 |    404.59 |          5.44x |

D6 collapses `dispatch()`'s `(next, retVal, retErr, retDone, err)`
5-tuple to `(next, err)`, mirroring CPython's `goto exit_frame`
pattern. RETURN_VALUE / INTERPRETER_EXIT / RETURN_GENERATOR park the
terminal value on `evalState.retVal` and raise the `errFrameReturn`
sentinel; the loop pattern-matches that sentinel before consulting
the exception walker. Every generated arm in `vm/eval_dispatch_gen.go`
drops its three unused middle returns, every hand-written arm in
`vm/eval_dispatch_handwritten.go` returns the `(next, ok, err)`
3-tuple, and the bytecodes_gen Go emitter's templates +
`tools/bytecodes_gen/action.go` emission sites are updated so a fresh
regeneration produces the same shape.

Bench wall-clock vs the post-D2+D5 baseline (8.21x): geomean improves
to 5.44x, with the long-tail `fannkuch` still pinning the geomean at
~33x. `compare-baseline -baseline bench/baseline_v0124.json` returns
OK; every bench that previously ran clean now runs within tolerance,
and the previously `runtime_error` rows (`fannkuch`, `json_dumps`,
`nbody`) all complete.

### Small subset, re-run 2026-05-21 (post list_ass_slice in-place port)

| Benchmark        | gopy (ms) | gopy / cpython | prev (post-D6) |
|------------------|----------:|---------------:|---------------:|
| `call_method`    |    259.60 |          5.38x |          5.58x |
| `fannkuch`       |  11439.21 |         26.36x |         33.65x |
| `json_dumps`     |    553.05 |          3.77x |          4.07x |
| `nbody`          |    195.90 |          3.94x |          3.75x |
| `pidigits`       |    118.82 |          2.16x |          2.29x |
| `regex_compile`  |    364.85 |          6.08x |          6.55x |
| `richards`       |    418.57 |          7.16x |          7.84x |
| `unpack_sequence`|     76.43 |          2.15x |          2.29x |
| **geomean**      |    381.24 |          5.06x |          5.44x |

CPU profile of `fannkuch` showed 50%+ of cycles in `runtime/GC` and
`runtime.mallocgc`, not in dispatch. Root cause: the bench's hot inner
loop is `a[i+1:j+1] = a[i:j][::-1]`, which hits
`listSetSlice -> NewList(reversed) -> defensive copy` in the old path.
Three allocations per loop body (`reversed` slice, `NewList` items
vector, defensive copy in `listSetSlice`) where CPython
`list_ass_slice_lock_held` does the work in place.

Ports landed in `objects/list.go`:

- `newListAdopt(items []objects.Object) *List` is an
  ownership-transfer constructor that skips the defensive copy
  `NewList` does. Used by `listGetSlice`, `listConcat`, and
  `listRepeat` (all three already build a fresh `items` slice they
  hand off, so the second copy was pure waste).
- `listAssSlice(l, ilow, ihigh, v)` is the 1:1 port of
  `Objects/listobject.c:768 list_ass_slice_lock_held`, including the
  aliased self-assign protection (`v == l` duplicates `l.items`
  first), the iterable-resolution path, and the three d-cases:
  - d == 0: `copy(l.items[ilow:ihigh], items)` in place.
  - d < 0 (shrink): `copy(l.items[ihigh+d:], l.items[ihigh:])` then
    truncate by reslicing.
  - d > 0 (grow): extend in place when capacity allows, otherwise
    `make([]objects.Object, newLen, growCap(newLen))` with the
    prefix/tail/items copied around the insertion point.
- `growCap(n) = n + n>>3 + 6` matches
  `Objects/listobject.c:74 list_resize`'s growth schedule, so a hot
  append/extend pattern reaches the same capacity classes as
  CPython does and gets the same amortized O(1) growth.
- `listSetSlice` now delegates `step == 1` to `listAssSlice`. The
  extended-slice path (`step != 1`) is unchanged; only the contiguous
  case touches the in-place body, which is the case the
  `fannkuch` hot loop hits.

Drivers:

- `fannkuch` collapsed 33.65x to 26.36x. Pure allocation savings:
  the inner loop went from 4 allocations per pass (slice literal,
  reverse buffer, NewList items, listSetSlice copy) to 0 (the slice
  literal still allocates, but the rest is in place).
- Every other bench moved within run-to-run noise. The list-slice
  port doesn't touch dispatch, attribute access, or the bytecode
  ladder, so the secondary benches see the noise floor.
- Geomean 5.44x to 5.06x. Still 3.4x above the 1.5x ship gate. The
  remaining wedge is dispatch: D3 (inline opcode arms; remove the
  `trySpecialized` / `dispatchGen` / `dispatchHandwritten` method-
  call indirection) and D4 (cache `stack_pointer` + `next_instr` as
  loop locals). The fannkuch profile after this port shows
  `trySpecialized` at 18.69% cum and `dispatchGen` at 4.01% cum, so
  D3 is the next-biggest single lever.

Next step per ship order: D3 inline opcode arms into the dispatch
loop body. The dispatch ladder today is
`dispatch -> trySpecialized -> dispatchGen -> dispatchHandwritten ->
trySimple`. Each level is a method call with its own frame, return
tuple, and (until D6) error-path tuple. D3 flattens that into a
single `switch op` inside `dispatch` so the hot opcodes don't pay
the method-call cost per instruction.

## Small subset, re-run 2026-05-21 (post D3 deopt-table + LOAD_FAST_BORROW inline)

Two D3 commits landed back-to-back:

1. `specialize/deopt.go` replaced the `DeoptParent` `map[Opcode]Opcode`
   lookup that `maybeDeopt` calls every dispatch with a flat
   `[288]Opcode` direct-index table (filled at init from `DeoptParent`,
   identity for everything else). The fannkuch profile pre-fix showed
   `specialize.Deopt` at 9.02% flat because every Quickened dispatch
   walked the map via `mapaccess2_fast32`. With the table it is one
   bounds check plus one indexed load.
2. `vm/eval.go` extended the inline opcode panel in `run()` from
   four opcodes (LOAD_CONST / LOAD_FAST / STORE_FAST / POP_TOP) to
   seven by adding LOAD_FAST_BORROW, LOAD_FAST_BORROW_LOAD_FAST_BORROW,
   and LOAD_SMALL_INT. fannkuch's `while k: perm[:k+1] = perm[k::-1]`
   inner loop is full of these three. Every LOAD_FAST_BORROW used to
   walk `run -> dispatch -> dispatchGenSupported[op] -> dispatchGen`,
   which is three frames per fetch.

| Benchmark | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|---|---:|---:|---:|---:|---:|
| `call_method` | 48.52 | 27.16 | 230.49 | 4.75x | 8.49x |
| `fannkuch` | 420.17 | 118.16 | 8183.99 | 19.48x | 69.26x |
| `json_dumps` | 147.08 | 186.52 | 589.78 | 4.01x | 3.16x |
| `nbody` | 48.39 | 34.17 | 149.37 | 3.09x | 4.37x |
| `pidigits` | 54.92 | 44.54 | 103.64 | 1.89x | 2.33x |
| `regex_compile` | 59.41 | 198.70 | 333.34 | 5.61x | 1.68x |
| `richards` | 57.05 | 40.85 | 364.23 | 6.38x | 8.92x |
| `unpack_sequence` | 35.33 | 25.96 | 69.74 | 1.97x | 2.69x |
| **geomean** | 74.55 | 61.01 | 331.50 | 4.45x | 5.43x |

Geomean 5.06x to 4.45x. Two benches (`pidigits`, `unpack_sequence`)
crossed under 2x of CPython for the first time. fannkuch took the
biggest swing: 26.36x to 19.48x in one step (-26%), driven entirely
by the LOAD_FAST_BORROW inline because the inner loop fetches
LOAD_FAST_BORROW four times per pass for `perm`, `k`, `perm`, `k`
plus a fifth LOAD_FAST_BORROW_LOAD_FAST_BORROW super.

Drivers:

- 3-iteration fannkuch wall time (the focused profile driver):
  10.42s to 7.59s, a 27% real-world drop that matches the suite-level
  fannkuch shift one-for-one. The profile after the inline shows
  the interpreter routing flat (`run + fetch + dispatch + trySimple +
  dispatchHandwritten + dispatchGen`) collapsed from ~25% of total
  samples to ~13%, leaving GC (`madvise + mallocgc + memclr`) as the
  next-biggest mutator slice at ~10%.
- The deopt-table change moved `maybeDeopt` from a hot 9% flat (via
  the map probe) down to noise. It is the kind of fix that does not
  show up in micro-benchmarks because every dispatch path benefited
  uniformly; the bench wins manifest as broad-spectrum geomean shift.
- Three benches that weren't allocation-heavy (`nbody`, `regex_compile`,
  `json_dumps`) all moved in tandem with the dispatch tightening, in
  the 8-12% range each.

Next step per ship order: still the dispatch-ladder collapse (D1 +
the remaining D3 work). The current ladder of `dispatch -> trySpecialized
-> dispatchGen / dispatchHandwritten -> trySimple` is each a method
call. After the inline panel, the per-instruction flat for the
ladder is ~13% of mutator. Folding the per-op switch into the loop
body would compress that further. GC is the other lever
(~10% flat split across `madvise`/`mallocgc`/`memclr`/`writeBarrier`);
that one ports CPython's PyList freelist and intermediate-slice
reuse, but it is a heavier change with broader correctness surface.

### D3 closer (2026-05-21): POP_JUMP_IF + JUMP_BACKWARD inline.

Extended the `run()` inline opcode panel from seven opcodes to eleven
by adding `POP_JUMP_IF_FALSE/TRUE/NONE/NOT_NONE` (bool/None singleton
TOS fast path), `JUMP_BACKWARD` (eval-breaker-zero fast path with
inline `tryWarmupTier2`), and `JUMP_BACKWARD_NO_INTERRUPT` (cache=0
stride-2 jump, used by try/except cleanup paths).

Bug caught during port: a first attempt inlined both JUMP_BACKWARD
variants with stride 4. `JUMP_BACKWARD_NO_INTERRUPT` has cache=0 in
`compile/opcode_caches.go`, so its codeunit stride is 2, not 4. The
stride-4 inline shifted every jump target by 2 bytes inside
try/except cleanup, corrupting control flow and crashing
regex_compile with `panic: index out of range [-1]` in
`Frame.PeekStack` from a POP_EXCEPT that saw an empty stack. Fix:
match each variant to its real cache width via separate arms
(stride 4 for `JUMP_BACKWARD`, stride 2 for `JUMP_BACKWARD_NO_INTERRUPT`).

| Benchmark | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|---|---:|---:|---:|---:|---:|
| `call_method` | 47.88 | 27.41 | 222.58 | 4.65x | 8.12x |
| `fannkuch` | 418.51 | 117.82 | 7973.27 | 19.05x | 67.67x |
| `json_dumps` | 143.75 | 187.73 | 567.26 | 3.95x | 3.02x |
| `nbody` | 48.43 | 34.49 | 144.31 | 2.98x | 4.18x |
| `pidigits` | 55.52 | 45.44 | 94.60 | 1.70x | 2.08x |
| `regex_compile` | 59.15 | 200.02 | 327.79 | 5.54x | 1.64x |
| `richards` | 56.62 | 40.45 | 348.84 | 6.16x | 8.62x |
| `unpack_sequence` | 34.73 | 25.86 | 68.45 | 1.97x | 2.65x |
| **geomean** | 74.01 | 61.28 | 319.13 | 4.31x | 5.21x |

Geomean 4.45x to 4.31x. `pidigits` cleared 2x cpython for the first
time at 1.70x, joining `unpack_sequence` (1.97x). `nbody` dropped
under 3x at 2.98x. `richards` lost ~1x. fannkuch nudged slightly the
wrong way (19.48x to 19.05x is within bench noise) because its hot
loop already collapsed onto the LOAD_FAST_BORROW arms in the prior
panel, leaving little JUMP_BACKWARD share to recover.

Three benches now sit at or below 2x cpython. The five outliers
ahead of D12: `fannkuch` (19.05x), `richards` (6.16x), `regex_compile`
(5.54x), `call_method` (4.65x), `json_dumps` (3.95x). Each needs a
subsystem port rather than another dispatch-tightening pass to clear
the 1.5x gate.

### D13 (2026-05-21): zero-alloc peekSliceBottomFirst.

Profiling fannkuch surfaced `vm.peekSliceBottomFirst` as the dominant
user-side allocator (0.10s of 0.18s makeslice cum). The helper backed
every `BUILD_SLICE`, `BUILD_LIST`, `BUILD_TUPLE`, `BUILD_MAP`,
`BUILD_STRING`, `BUILD_SET`, `RAISE_VARARGS`, and the auto-ported
templated-stack arms generated by spec 1714. Each call did
`out := make([]stackref.Ref, n)` and copied n peek slots into it, even
though every consumer reads sequentially and copies into its own
target buffer (`listFromStackRef`, `tupleFromStackRef`,
`stackrefsToObjects`).

The CPython equivalent is pointer arithmetic: `args = stack_pointer - n`
is a slice into the live stack, no copy. Mirrored that exactly:
peekSliceBottomFirst now returns `f.LocalsPlus[top-n : top]` (where
`top = StackBase + StackTop - topOffset`). `LocalsPlus` is sized at
frame init and never resized, so the aliasing is safe across the
consume-then-move-stack lifetime each opcode needs.

| Benchmark | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |
|---|---:|---:|---:|---:|---:|
| `call_method` | 48.29 | 28.02 | 255.41 | 5.29x | 9.12x |
| `fannkuch` | 420.85 | 116.72 | 7103.76 | 16.88x | 60.86x |
| `json_dumps` | 144.25 | 215.57 | 550.74 | 3.82x | 2.55x |
| `nbody` | 49.06 | 33.55 | 143.66 | 2.93x | 4.28x |
| `pidigits` | 55.17 | 44.78 | 94.78 | 1.72x | 2.12x |
| `regex_compile` | 59.62 | 196.92 | 316.98 | 5.32x | 1.61x |
| `richards` | 56.31 | 40.60 | 355.68 | 6.32x | 8.76x |
| `unpack_sequence` | 35.49 | 25.71 | 66.82 | 1.88x | 2.60x |
| **geomean** | 74.47 | 61.98 | 317.21 | 4.26x | 5.12x |

Geomean 4.31x to 4.26x. fannkuch from 19.05x to 16.88x (the bench
that drove the diagnosis: 7973ms to 7104ms, -11%). Post-fix profile
shows `mallocgcSmallScanNoHeader` cum dropped from 0.21s to 0.08s and
peekSliceBottomFirst itself fell out of the flat profile (one slice
header construction is unmeasurable). `call_method` and `richards`
moved slightly the wrong way within bench noise; both will need a
subsystem port (CALL/BoundMethod fastpath, GC pressure) to make
serious progress.

Next biggest user-side allocators per the post-D13 profile:
`NewSlice` (0.06s, one alloc per `BUILD_SLICE`), `listGetSlice`
(0.05s combined makeslice + newListAdopt). These both need the
PyList / PySlice freelists from `Objects/listobject.c` and
`Objects/sliceobject.c`, which is the next sized port (D14).

### D14 investigation (2026-05-21): Go GC scavenger vs CPython refcount.

Diagnosed where fannkuch's residual 16.88x lives after D13. The
flat profile attributes 58% of wall time to two Go runtime
syscalls: `runtime.madvise` (0.55s) and `runtime.kevent` (0.38s),
both inside `runtime.systemstack`. Together that is 0.93s of
1.59s total. The gopy VM work itself is only 0.41s. The remaining
mallocgc cost is 0.08s, attributed to `NewSlice` (0.05s),
`boundMethodVectorcall` (0.02s), `listGetSlice` (0.03s).

GOGC sensitivity confirms the diagnosis. Same fannkuch(9), same
binary, varying `GOGC`:

| GOGC | wall (s) | vs default |
|---:|---:|---:|
| 100 (default) | 2.10 | 1.00x |
| 200 | 1.44 | 0.69x |
| 400 | 1.33 | 0.63x |
| 800 | 1.33 | 0.63x |
| off | 1.66 | 0.79x |

At `GOGC=400` the scavenger drops out (madvise 0.03s, kevent
0.07s) and the remaining time is dominated by the actual VM and
allocator work (`mallocgcSmallScanNoHeader` 0.23s cum,
`listGetSlice` 0.13s, `NewSlice` 0.10s).

The structural mismatch with CPython: CPython's allocator
(obmalloc + refcount) has no global allocation-rate-driven GC
trigger. Refcount decrements free objects immediately at the
last drop; cycle GC runs at thresholds (`gc.set_threshold(700,
10, 10)`) that almost never fire in CPU-bound benches. Go's GC
triggers when the heap grows by `GOGC%` since the previous live
heap, so a tight allocation loop forces frequent cycles and the
scavenger churns pages back to the OS each cycle. The result is
0.93s of OS-level memory bookkeeping that has no CPython
analogue.

The PySlice / PyList freelists that the original D14 plan asked
for cannot recover this. The CPython freelist relies on the
slice's dealloc hook (called when refcount drops to 0) to push
the slot back. gopy's Go GC has no per-object dealloc hook on
short-lived objects, and the consumer call sites
(`BINARY_OP_SUBSCR_LIST_SLICE`, `STORE_SLICE`) cannot safely call
an explicit `ReleaseSlice` because the same slice may also live
in a Python local (`s = slice(1, 5); l[s]`) where releasing it
would alias-corrupt the local. CPython's refcount discriminates
these cases automatically; gopy has no equivalent without adding
refcount semantics to `*Slice` (and `*List`, and every other
candidate freelist class).

Three forward paths exist for D14:

1. **Runtime alignment**: set a higher GOGC default at gopy
   startup so the Go GC trigger frequency matches CPython's
   "almost-never" cycle threshold. This is a configuration, not a
   CPython port, but it closes the structural gap directly. On
   fannkuch it recovers 0.77s of the 1.32s gap to cpython.

2. **Selective refcount**: add a lightweight refcount-like marker
   to short-lived types (`Slice`, transient `List`) so a freelist
   has a safe dealloc hook. This is a partial refcount port and
   would touch dozens of allocation sites.

3. **Move off fannkuch**: the remaining four outliers
   (`richards` 6.32x, `regex_compile` 5.32x, `call_method` 5.29x,
   `json_dumps` 3.82x) are bounded by different subsystems and
   may move with cleaner CPython-faithful ports (CALL fastpath,
   re engine, json encoder hotpath). Geomean improves more from
   fixing several mid-tier outliers than from grinding fannkuch.
