---
format: md
id: 1713_pyc_byte_equality
title: "1713. .pyc byte-equality with CPython 3.14"
sidebar_label: "1713. pyc byte-equality"
sidebar_position: 1713
slug: /specs/1713-pyc-byte-equality
description: "Drive every compile-side subsystem (parser, codegen, flowgraph, instruction sequence, assemble, marshal, importlib pyc writer) to the point where `gopy -m py_compile foo.py` produces a byte-equal .pyc to `python3.14 -m py_compile foo.py` for the full CPython test corpus. Starts from disassembly-stream parity so each gap is debuggable before the marshal layer rolls it into opaque bytes."
---

## Ground rule

Same rule as 1704 / 1705 / 1708 / 1712 / 1716. Port full subsystems,
file by file. No partial slices, no name-only shims, no "tweak the
output until the bytes match for this one fixture". Every CPython
source file the spec touches lands in gopy with a 1:1 function list
and `// CPython:` citations carrying file + line. The cost of a
half-ported subsystem here is paid back many times over: every
divergence on the byte-equality gate becomes a forensic dig through
marshal output, and that is exactly the work the spec is built to
make easy.

This spec **pauses spec 1712**. Performance-parity work resumes once
1713 lands, because every remaining 1712 row (specializer emission
audits, cache-persistence via marshal, P1.5) depends on a trustworthy
compile pipeline. If the .pyc gopy writes today does not round-trip
through CPython byte for byte, the specializer's inline-cache slots,
exception table, location table, and co_consts ordering are all
suspect.

## Goal

```
python3.14 -m py_compile foo.py
gopy        -m py_compile foo.py
cmp __pycache__/foo.cpython-314.pyc __pycache__/foo.cpython-314.gopy.pyc
# exit 0 for every foo.py in Lib/ and Lib/test/
```

The final deliverable is `test/gate/pyc_parity_test.go`. It walks the
vendored CPython corpus under `test/cpython/Lib/`, compiles each file
with both interpreters, and asserts byte-equal .pyc output. Spec done
= gate green across the full corpus.

Two intermediate gates land before the final byte-equality gate,
because byte-equality is the *worst* signal to debug from. In order:

1. **Instruction-stream parity** (L1 codegen + L3 cfg-optimize). Every
   instruction, oparg, jump target, and location matches CPython for
   the same Python source. Any diff here is a codegen or flowgraph
   bug, named in human terms.
2. **Code-object field parity** (L4). Every field on the assembled
   code object (co_consts ordering and types, co_names ordering,
   co_varnames, co_freevars, co_cellvars, co_flags, co_stacksize,
   co_firstlineno, co_linetable, co_exceptiontable, co_qualname)
   matches CPython's for the same fixture. This catches the bugs the
   disassembler smooths over.
3. **.pyc byte-equality**. Marshal + importlib header writer produces
   identical bytes. By construction, this gate cannot fail unless the
   marshal layer or the pyc header writer itself diverges, because
   both inputs are now byte-equal code objects.

Gates (1) and (2) shipped under spec 1716 (PR #71, merged at 9d7d9f0)
as `test/gate/codegen_parity_test.go` (L1), `test/gate/cfg_phase_parity_test.go`
(L2 cfg-input parity), and `test/gate/assemble_parity_test.go` (L3
post-optimize + L4 final code object). Spec 1713 now leans on those
as the upstream sentinels and focuses on Phase 6 (marshal audit) and
Phase 7 (py_compile + pyc writer + byte-equality gate).

## Why this spec exists

Spec 1712 hit the wall it was always going to hit: porting specializer
fast paths is straightforward, but every arm reads the inline cache
the compiler emitted, and any disagreement between gopy's cache layout
and CPython's burns hours per arm. The same is true of unwind (1708):
the assembler emits one PEP 657 entry per instruction in CPython but
coalesces in gopy, and the symptom only surfaced when traceback
rendering broke.

Both gaps are downstream of the same root cause: gopy's compile
pipeline produces code objects that *behave* like CPython's most of
the time but are not byte-identical. The right fix is not a shim per
symptom. The right fix is to drive the compile pipeline to
byte-equality and keep it there with a gate.

Once .pyc files round-trip, the entire performance-parity stack gets a
free property: marshal can ship the specializer's cache layout to
disk, and importlib can read it back. Spec 1712 P1.5 folds into this
spec.

## Current state (2026-05-19, post-1716)

Audit after spec 1716 merged. Subsystem rows are now grouped by
whether 1716 closed them or they remain open work.

### Closed by spec 1716

| Subsystem | gopy file(s) | CPython source | What 1716 closed |
|-----------|--------------|----------------|------------------|
| codegen | `compile/codegen*.go` (21 files) | `Python/codegen.c` (6485 lines) | Every stmt + expr + match-pattern visitor 1:1 with citations. L1 codegen-parity gate covers it. PEP 695 generics and t-string interpolations explicitly deferred (no shim, raises). |
| flowgraph / cfgBuilder | `compile/flowgraph*.go` (10 files) | `Python/flowgraph.c` (4165 lines) | All passes ported onto cfgBuilder: `remove_redundant_nops`, `remove_redundant_jumps`, `remove_unreachable`, `propagate_line_numbers`, `convert_pseudo_ops`, `convert_pseudo_conditional_jumps`, `prepare_localsplus`, `optimize_load_const`, `remove_unused_consts`, `insert_superinstructions`, `mark_warm` / `mark_cold` / `push_cold_blocks_to_end`, `calculate_stackdepth`. L2 cfg-phase-parity gate covers it. |
| instruction sequence | `compile/instrseq.go` | `Python/instruction_sequence.c` | 1716 D retired the flat-sequence pass shim. The Sequence type remains as the codegen → cfg bridge layer, mirroring CPython structurally. |
| compile driver | `compile/compiler.go` | `Python/compile.c` (`_PyCompile_CodeGen`, `optimize_and_assemble_code_unit`) | 1716 C.3 ported `optimize_and_assemble_code_unit` 1:1. `finalizeFlags` absorbs `compute_code_flags`. |
| assemble | `compile/assemble*.go` (6 files) | `Python/assemble.c` (802 lines) | 1716 C.2 split into `assembleEmit`, `assembleExceptionTable`, `assembleLocationInfo`, `makecode`. Varint emission (`compile/assemble_varint.go`) byte-identical with `write_varint`. 1708 closed location-emission. |
| code object | `objects/code*.go` (3 files) | `Objects/codeobject.c` (2980 lines) | Code struct mirrors `_PyCodeObject` fields. All Python-facing `co_*` getsets exposed (#645). `_PyCode_ConstantKey` const-dedup logic ported into `compile/codegen_addop.go` and `compile/flowgraph_cfg_passes.go` (#666). |
| disassembly-stream gate (L0) | `test/gate/dis_parity_test.go`, `test/gate/disdata/` (946 fixtures) | `Lib/dis.py` (vendored at `stdlib/dis.py`) | gate runs `dis.dis(compile(src, ...))` via the vendored dis.py, diffs CPython vs gopy. |

### Still open

| Subsystem | gopy file(s) | CPython source | Status |
|-----------|--------------|----------------|--------|
| marshal | `marshal/marshal.go`, `marshal/code.go`, `marshal/long.go` | `Python/marshal.c` (2163 lines) | w_object / w_ref split, FLAG_REF per-type placement, `_PyObject_IsUniquelyReferenced` approximation, cached-bytes memoization (co_code, co_linetable, co_exceptiontable), TYPE_INT vs TYPE_LONG int32 boundary, and the 6-bucket co_localsplus reconstruction all shipped at 1da09b3. Per-tag fixture matrix (TYPE_BINARY_FLOAT vs TYPE_FLOAT for NaN/inf, TYPE_INTERNED vs TYPE_UNICODE) still pending. |
| .pyc header | `marshal/pyc.go` | `Lib/importlib/_bootstrap_external.py:222 MAGIC_NUMBER`, `Include/internal/pycore_magic_number.h:295 PYC_MAGIC_NUMBER` | MAGIC_NUMBER 3627 bumped in Phase 1 (5dbfac9). Timestamp- and hash-based writers ported in Phase 7 (859df19). |
| `_PyCode_New` interning helpers | not yet ported | `Objects/codeobject.c` `intern_strings`, `intern_constants` | The dedup logic on the codegen side (`_PyCode_ConstantKey`) is ported, but the post-assemble `intern_strings` walk that flips strings to interned at code-object construction time is missing. The byte-equality gate has not surfaced a divergence yet, so this stays a watch item rather than blocking. |
| py_compile module | `stdlib/py_compile.py` | `Lib/py_compile.py` | Vendored 1:1 in Phase 7 (859df19). `gopy -m py_compile foo.py` runs end to end and writes a real `__pycache__/foo.gopy-3140.pyc`. |
| importlib pyc writer | `stdlib/importlib/_bootstrap_external.py` | `Lib/importlib/_bootstrap_external.py` (`_code_to_timestamp_pyc`, `_code_to_hash_pyc`, `_classify_pyc`, `_pack_uint32`, `_unpack_uint*`, `_write_atomic`, `cache_from_source`, `_path_split`, `_path_join`, `_path_abspath`, `source_hash`) | Writer slice + the path helpers `cache_from_source` / `source_from_cache` lean on shipped in Phase 7. Reader / finder / loader scaffolding still future work (spec 1711). |
| byte-equality gate | `test/gate/pyc_parity_test.go` | n/a | Stood up. Walks `test/gate/pyc_parity_corpus.txt`, runs `python3.14 -m py_compile` and `gopy -m py_compile` against a copy of each fixture, diffs the resulting .pyc bytes (header + marshaled body). Self-skips when CPython 3.14 is missing. Currently green on the disdata starter set. |

## Files in scope

Sources of truth live under `/Users/apple/cpython-314/`. Every file
below is ported in full, with `// CPython: <file>:<line> <function>`
citations.

| # | CPython file | gopy target | Why | 1716 status |
|---|--------------|-------------|-----|-------------|
| A | `Python/marshal.c` | `marshal/marshal.go`, `marshal/code.go`, `marshal/long.go` | The bytes the gate compares. Every TYPE_* tag, every TYPE_REF reuse decision, every short-vs-long-int encoding must match. | code present; audit pending (Phase 6) |
| B | `Lib/importlib/_bootstrap_external.py` (pyc writer slice) | `stdlib/importlib/_bootstrap_external.py` | 16-byte header layout + hash-based cache flow. | writer slice ported (859df19); reader/finder slice future work |
| C | `Include/internal/pycore_magic_number.h` | `marshal/pyc.go` MagicNumber const | Single source of the version bump. | done (5dbfac9) |
| D | `Objects/codeobject.c` (`_PyCode_New`, `intern_strings`, `intern_constants`, `_PyCode_ConstantKey`) | `objects/code.go` | co_consts / co_names ordering. | `_PyCode_ConstantKey` ported; `intern_strings` / `intern_constants` watch-item |
| E | `Python/compile.c` (`compute_code_flags`, `dict_keys_inorder`, `consts_dict_keys_inorder`, `optimize_and_assemble_code_unit`) | `compile/compiler.go`, `compile/assemble_makecode.go` | Final stage before the assembler. | done via 1716 C.3 |
| F | `Python/codegen.c` | `compile/codegen*.go` | Statement + expression visitors. | done via L1 gate (1716 E) |
| G | `Python/flowgraph.c` | `compile/flowgraph*.go` | Optimization passes. | done via 1716 C.1 + L2/L3 gates |
| H | `Python/instruction_sequence.c` | `compile/instrseq.go` | Codegen → cfg bridge. | done via 1716 D |
| I | `Python/assemble.c` | `compile/assemble*.go` | Exception table, location table, makecode. | done via 1708 + 1716 C.2 |
| J | `Lib/dis.py` | `stdlib/dis.py` (vendor) | First-line debugging signal for divergence. | vendored; L0 gate green on 946 fixtures |
| K | `Lib/py_compile.py` | `stdlib/py_compile.py` | The `gopy -m py_compile` driver. | vendored 1:1 (859df19) |
| L | `Python/import.c` (`_imp.source_hash`, `_imp.pyc_magic_number_token`) | `module/_imp/module.go` | Backs `_bootstrap_external.source_hash` and `MAGIC_NUMBER`. | done (859df19) |

## Phase index

Each phase ports one block end to end. Status lives on the Checklist
at the bottom of this spec, mirrored per row here.

| Phase | Block | Gate | Status | Commit |
|-------|-------|------|--------|--------|
| 1 | Magic bump + marshal round-trip. Update `MagicNumber` to 3627. `marshal/parity_test.go` round-trip fixture green. | round-trip | done | 5dbfac9 |
| 2 | Disassembly-stream parity. `gopy -m dis` runs against a 946-file disdata corpus; vendor `stdlib/dis.py` + `stdlib/runpy.py`; gate compares to `python3.14 -m dis`. | L0 dis-stream gate | done | 554dfca + 1716 (PR #71) |
| 3 | Codegen + flowgraph + instruction-sequence + assemble + compile-driver port, with citations. Stand up L1 / L2 / L3 / L4 parity gates so per-subsystem divergence is forensic-grade. | L1 / L2 / L3 / L4 parity gates | done | spec 1716 (PR #71, 9d7d9f0) |
| 4 | Code-object field-level parity. L4 gate dumps every observable field on the assembled Code object and diffs CPython vs gopy. | L4 field-parity gate | done via 1716 E | 6004c1c |
| 5 | Assemble audit (exception table, code flags, stacksize). | L4 field-parity gate green | done via 1708 + 1716 C.2 | (multiple) |
| 6 | Marshal port audit. 1:1 against `Python/marshal.c` writer + reader. TYPE_REF reuse table, int short/long encoding, float encoding, interning. Per-tag round-trip fixtures. | `marshal/parity_test.go` extended | writer side audited; per-tag fixtures pending | 1da09b3 |
| 7 | py_compile + importlib pyc writer. Vendor `Lib/py_compile.py`. Port `_code_to_timestamp_pyc` / `_code_to_hash_pyc` / `_classify_pyc` slice of `_bootstrap_external.py`. `gopy -m py_compile foo.py` produces a real `__pycache__/foo.gopy-3140.pyc`. Stand up `test/gate/pyc_parity_test.go`. | `test/gate/pyc_parity_test.go` green on disdata | done | 859df19 + 656f672 |
| Gate | Byte-equality across the full vendored CPython corpus. | gate green on `test/cpython/Lib/` | TODO | - |

## Phase 1 — magic + marshal round-trip (done, 5dbfac9)

Two changes, both small, both load-bearing. First, `marshal/pyc.go`
bumped `MagicNumber` from 3620 to 3627 to match
`Include/internal/pycore_magic_number.h:295`, with a citation on the
constant so future bumps don't drift. Second, `marshal/parity_test.go`
round-trips a curated fixture through Dump/Load and asserts
`reflect.DeepEqual` on every observable field of `objects.Code` (with
a `(*big.Int).Cmp` shim for the big-int slot). The fixture covers
short and long ints, floats, strings with duplicates for TYPE_REF
reuse, bools, None, plus the Linetable and ExceptionTable blobs.

| Step | Status | Commit |
|------|--------|--------|
| `MagicNumber` bumped 3620 → 3627 with CPython citation | done | 5dbfac9 |
| `marshal/parity_test.go` round-trip fixture green | done | 5dbfac9 |

## Phase 2 — disassembly-stream parity (done)

The L0 gate runs `gopy -m dis foo.py` and `python3.14 -m dis foo.py`
for every fixture in `test/gate/disdata/` (946 fixtures, up from the
original 30-fixture estimate) and diffs the two streams. The gate
runs *first* because every diff is human-readable.

The original draft framed this phase as "port `Lib/dis.py` 1:1 into
`compile/dis.go`". That was wrong: `stdlib/dis.py` already vendors
`Lib/dis.py`. The full-file rule says *vendor* CPython sources, not
rewrite them in Go. The actual port work to make `gopy -m dis foo.py`
produce CPython-byte-equal output was to vendor `Lib/runpy.py`, wire
`cmd/gopy -m` to call `runpy._run_module_as_main`, and then fix every
VM / compile bug the dis-stream gate surfaced when the gopy
interpreter runs the vendored dis.py against itself.

| Step | Status | Commit |
|------|--------|--------|
| Vendor `Lib/runpy.py` 1:1 into `stdlib/runpy.py` | done | 554dfca |
| Wire `cmd/gopy -m mod` to call `runpy._run_module_as_main(mod)` | done | 554dfca |
| Expose Python-facing `co_code` / `co_consts` / `co_names` / `co_varnames` / `co_*` attrs on `objects.Code` | done | 08b0d9d (#645) |
| Plumb builtin `compile(src, fn, mode)` through the parser + compiler | done | 83fc368 (#644) |
| Fix `importlib.util` attribute access path so `importlib.util.find_spec` resolves | done | d009fd9 (#643) |
| Port `codegen_enter_scope` RESUME prologue for module / eval / interactive scopes | done | spec 1716 |
| Port `propagate_line_numbers` flowgraph pass onto cfgBuilder | done | spec 1716 |
| Port `maybe_instr_make_load_smallint` rewrite onto cfgBuilder | done | spec 1716 |
| Port `remove_unused_consts` from `Python/flowgraph.c:3174` | done | f679821 |
| Port `optimize_basic_block` peephole pairs onto cfgBuilder | done | spec 1716 |
| `gopy -m dis foo.py` end to end on disdata corpus | done | spec 1716 |
| `test/gate/dis_parity_test.go` on 946-fixture corpus | done | spec 1716 |

## Phase 3 — codegen / flowgraph audit (done, spec 1716)

Driven entirely by the L1 / L2 / L3 / L4 parity gates that spec 1716
stood up. Every CPython codegen.c + flowgraph.c + instruction_sequence.c
+ assemble.c function has a 1:1 gopy port with `// CPython: file:line`
citations. Per-subsystem skip lists at `test/gate/codegen_parity_skip.txt`
and `test/gate/assemble_parity_skip.txt` track the (currently one)
fixture pending follow-up parity work.

| Step | Status | Commit |
|------|--------|--------|
| 1:1 audit of `Python/codegen.c` against `compile/codegen*.go` | done | spec 1716 |
| 1:1 audit of `Python/flowgraph.c` against `compile/flowgraph*.go` | done | spec 1716 C.1 |
| 1:1 audit of `Python/instruction_sequence.c` against `compile/instrseq.go` | done | spec 1716 D |
| 1:1 audit of `Python/assemble.c` against `compile/assemble*.go` | done | spec 1716 C.2 |
| Port `optimize_and_assemble_code_unit` driver into `compile/compiler.go` | done | spec 1716 C.3 |
| Flip `assembleUnit` to cfg driver + delete flat-sequence files | done | spec 1716 D |
| Stand up L1 / L2 / L3 / L4 parity gates with skip lists | done | spec 1716 E (6004c1c) |

## Phase 4 — code-object field-level parity (done, via 1716 E)

The L4 gate (`test/gate/assemble_parity_test.go::TestAssembleParity`)
dumps every observable field of the assembled Code object using the
same format CPython's `dump_code` (in `assemble_oracle.py`) writes,
then diffs byte-for-byte. The dump walks nested code objects in
`assembleOrder`, so closure / classbody / generator code objects are
all covered.

| Step | Status | Commit |
|------|--------|--------|
| Compile-side `_PyCode_ConstantKey` const-dedup (codegen + flowgraph paths) | done | #666 |
| `objects.Code` exposes every `co_*` Python attribute the field dump compares | done | #645 |
| `DumpCodeL4` writes argcount / posonlyargcount / kwonlyargcount / nlocals / stacksize / flags / consts / names / varnames / freevars / cellvars / filename / name / qualname / firstlineno / linetable / exceptiontable / code | done | 6004c1c |
| `test/gate/assemble_parity_test.go::TestAssembleParity` walks nested code objects in dump order | done | 6004c1c |
| Watch-item: port `intern_strings` + `intern_constants` if L4 gate surfaces a divergence | watch | - |

## Phase 5 — assemble audit (done, via 1708 + 1716 C.2)

| Step | Status | Commit |
|------|--------|--------|
| `assemble_exception_table` byte-identical varint emission | done | spec 1708 |
| `assemble_location_table` PEP 657 emission | done | spec 1708 |
| `compute_code_flags` covers every CO_* flag (folded into `finalizeFlags`) | done | (compile/compiler.go) |
| `assemble_emit` stacksize matches `_PyCompile_OptimizeAndAssemble` | done | spec 1716 C.2 |
| `makecode` split out from `assemble_emit` | done | spec 1716 C.2 |

## Phase 6 — marshal audit (writer side done at 1da09b3)

`Python/marshal.c` is one source file, ~2163 lines, very mechanical.
The work here is a function-by-function 1:1 audit with citations. The
load-bearing risks are: TYPE_REF reuse (CPython tracks a `refs` dict
keyed by identity, not value, so gopy must use the same identity
semantics for the reuse positions to match); short vs long int
encoding (TYPE_INT covers `-2^31..2^31-1`, TYPE_LONG covers the rest);
float encoding (TYPE_BINARY_FLOAT, 8 bytes IEEE 754, is what CPython
3.14 uses by default; confirm gopy isn't writing the legacy TYPE_FLOAT
ASCII form); and interned vs non-interned strings (TYPE_INTERNED vs
TYPE_UNICODE, decided by the string's `state.interned` flag).

| Step | Status | Commit |
|------|--------|--------|
| 1:1 audit of `marshal/marshal.go` against `Python/marshal.c` writer side (every `w_*` function carries a citation, w_object / w_ref split, per-type FLAG_REF placement) | done | 1da09b3 |
| `_PyObject_IsUniquelyReferenced` approximation drives the `scanShared` pre-pass | done | 1da09b3 |
| `_PyCompile_ConstCacheMergeOne` style cached-bytes memoization for co_code / co_linetable / co_exceptiontable | done | 1da09b3 |
| TYPE_INT / TYPE_LONG boundary covered (anything outside int32 routes through TYPE_LONG; decoder downcasts back to int64 when magnitude fits) | done | 1da09b3 |
| `co_localsplus` reconstruction from Argcount / PosonlyArgcount / KwonlyArgcount + CO_VARARGS / CO_VARKEYWORDS when flat slabs are absent | done | 1da09b3 |
| 1:1 audit of `marshal/marshal.go` against `Python/marshal.c` reader side (every `r_*` function carries a citation) | TODO | - |
| TYPE_BINARY_FLOAT vs TYPE_FLOAT fixture (NaN, ±inf, denormals) | TODO | - |
| TYPE_INTERNED vs TYPE_UNICODE fixture (interned reused name, non-interned literal) | TODO | - |
| `marshal/parity_test.go` per-tag fixture matrix green | TODO | - |

## Phase 7 — py_compile + importlib pyc writer (active on PR #72)

The user-facing entry point. Drops the `stdlib/py_compile.py` stub,
vendors `Lib/py_compile.py` 1:1, ports the writer/reader slice of
`Lib/importlib/_bootstrap_external.py`, and stands up the
byte-equality gate. The byte-equality gate is what 1713 is named for;
it cannot run until `gopy -m py_compile foo.py` produces a real `.pyc`.

| Step | Status | Commit |
|------|--------|--------|
| Fix `marshal.MagicNumber` byte order to match CPython `PYC_MAGIC_NUMBER_TOKEN` | done | d6eb31e |
| Create `module/_imp` builtin: `source_hash`, `pyc_magic_number_token`, `check_hash_based_pycs` | done | 859df19 |
| Create `stdlib/importlib/_bootstrap_external.py` and port the writer slice (`_pack_uint32`, `_unpack_uint*`, `_code_to_timestamp_pyc`, `_code_to_hash_pyc`, `_classify_pyc`, `_validate_timestamp_pyc`, `_validate_hash_pyc`, `_calc_mode`, `_write_atomic`, `source_hash`, `MAGIC_NUMBER`, `SourceFileLoader`) | done | 859df19 |
| Drop stub `MAGIC_NUMBER` in `stdlib/importlib/util.py`; rewire to `_bootstrap_external` | done | 859df19 |
| Vendor `Lib/py_compile.py` 1:1 into `stdlib/py_compile.py` (replace the stub) | done | 859df19 |
| Port `cache_from_source` + `source_from_cache` + `_path_split` + `_path_join` + `_path_abspath` 1:1 (drop the `<source>c` shim) | done | 656f672 |
| Wire `gopy -m py_compile foo.py` so it writes `__pycache__/foo.gopy-3140.pyc` | done | 656f672 |
| Stand up `test/gate/pyc_parity_test.go` (self-skip when `python3.14` is missing) | done | 656f672 |
| `test/gate/pyc_parity_corpus.txt` + `test/gate/pyc_parity_skip.txt` (start with disdata corpus subset) | done | 656f672 |
| Byte-equality gate green on disdata 946-fixture corpus | 942 / 946 pass; 4 skip-listed (2 marshal FLAG_REF on non-interned const strings, 2 codegen NamedExpr location-table divergences) | 656f672 |
| Byte-equality gate green on `test/cpython/Lib/` corpus | TODO | - |

## Gate

After all seven phases:

1. `test/gate/pyc_parity_test.go` walks every `.py` file under
   `test/cpython/Lib/`, compiles it through both `python3.14 -m
   py_compile` and `gopy -m py_compile`, asserts the two `.pyc`
   files are byte-equal.
2. Re-run `test/gate/dis_parity_test.go` (L0) — disassembly parity
   stays green.
3. Re-run `test/gate/codegen_parity_test.go` (L1),
   `test/gate/cfg_phase_parity_test.go` (L2),
   `test/gate/assemble_parity_test.go` (L3 + L4) — all four parity
   gates from 1716 stay green.
4. Re-run `marshal/parity_test.go` — round-trip parity stays green.
5. Spec 1712 unpaused. Its P1.5 (specializer cache persistence via
   marshal) becomes trivial: the cache slabs marshal through the
   existing `Code.CacheObjects` path and round-trip by definition.

## Checklist

- [x] Phase 1: `MagicNumber` bumped to 3627; `marshal/parity_test.go` round-trip green (5dbfac9)
- [x] Phase 2: L0 dis-stream gate green on disdata 946-fixture corpus (spec 1716)
- [x] Phase 3: codegen + flowgraph + instrseq + assemble + compile-driver ported with citations; L1 / L2 / L3 / L4 parity gates stood up (spec 1716, 9d7d9f0)
- [x] Phase 4: L4 field-parity gate green on the 1716 starter corpus (6004c1c)
- [x] Phase 5: `Python/assemble.c` audit complete (1708 + 1716 C.2)
- [x] Phase 6: `Python/marshal.c` writer side audited at 1da09b3 (w_object / w_ref split, FLAG_REF per-type, cached-bytes memoization, TYPE_INT / TYPE_LONG int32 boundary, 6-bucket co_localsplus reconstruction); reader-side audit + per-tag round-trip fixtures still TODO
- [x] Phase 7: `Lib/py_compile.py` vendored; importlib pyc writer slice ported; `cache_from_source` + path helpers ported 1:1; `test/gate/pyc_parity_test.go` stood up
- [x] Phase 7: byte-equality gate green on full disdata 946-fixture corpus (942 / 946; 4 fixtures pinned in `test/gate/pyc_parity_skip.txt` against follow-up marshal + NamedExpr-location work)
- [ ] Phase 7: byte-equality gate green on full `test/cpython/Lib/` corpus
- [ ] Gate: spec 1712 unpaused with P1.5 folded into the marshal round-trip
