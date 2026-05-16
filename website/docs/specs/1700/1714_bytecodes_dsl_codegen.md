---
format: md
id: 1714_bytecodes_dsl_codegen
title: "1714. Port CPython's Tools/cases_generator into gopy"
sidebar_label: "1714. bytecodes DSL codegen"
sidebar_position: 1714
slug: /specs/1714-bytecodes-dsl-codegen
description: "Vendor CPython 3.14's Tools/cases_generator pipeline and teach it to emit Go. Drive every opcode, every cache layout, every stack effect, every family/deopt relation, every tier-1 dispatch arm, and every tier-2 uop body in gopy off the same single source of truth CPython uses (Python/bytecodes.c, Python/optimizer_bytecodes.c, Include/internal/pycore_code.h). Eliminates the entire class of bugs where two sides of the same opcode (specializer write vs VM read, compile-side cache reservation vs runtime cache access, tier-1 body vs tier-2 uop body) drift apart by hand."
---

## Ground rule

Same rule as 1704 / 1705 / 1708 / 1712 / 1713. Vendor full
subsystems, file by file. No partial slices, no name-only shims.
This spec is the most aggressive application of that rule yet:
every opcode in gopy, every cache layout, every stack effect,
every dispatch arm, every uop body gets re-rooted onto CPython's
own DSL. After this spec lands, `vm/eval_*.go`,
`specialize/*.go` cache-access calls, `compile/opcode_caches.go`,
and `compile/opcodes_gen.go` are all *generated* from the same
inputs CPython compiles itself from. The hand-rolled dispatch
loop, the hand-counted cache cells, and the hand-mirrored stack
effect tables go away.

This spec **pauses spec 1713**. Byte-equality work resumes once
1714 lands, because every remaining 1713 row (codegen audit,
flowgraph audit, marshal port, .pyc parity) is downstream of the
same single-source-of-truth question: gopy can only match
CPython's compiled output if both sides agree on opcode numbering,
cache widths, and stack effects, and gopy's current copies of
those tables are hand-maintained and divergent. The LOAD_GLOBAL
cache cell-4 vs cell-1 bug that surfaced during 1713 P2 work is
the canonical example: the specializer wrote past the cache and
overwrote the next opcode's first byte; the VM read from the same
out-of-bounds slot, so both sides "agreed" on a wrong layout for
months. The bug only surfaced when an unrelated change exposed a
codepath where the next instruction happened to be PUSH_NULL
(opcode 33), got rewritten as RESERVED (17), and CALL popped a
tuple iterator from the wrong stack slot. There is no test that
catches this class of bug today. There is no design pattern in
gopy that prevents it. There is in CPython, and it is
Tools/cases_generator.

## Goal

```
tools/regen-cases.sh
git diff --exit-code
# CI fails if any generated file is out of date
```

Concretely, the deliverable is a generator under
`tools/cases_generator/` that:

1. Reads CPython 3.14.5's `Python/bytecodes.c`,
   `Python/optimizer_bytecodes.c`, and
   `Include/internal/pycore_code.h` from the vendored copy under
   `tools/cases_generator/inputs/`.
2. Emits a fixed set of Go files under `compile/`, `specialize/`,
   and `vm/` named `*_gen.go`.
3. Is byte-for-byte reproducible: running `tools/regen-cases.sh`
   on a clean checkout leaves the tree unchanged.

Spec done = every opcode dispatch arm in gopy is generated, every
cache access goes through a typed accessor whose layout is
generated, every stack effect comes from the generated metadata
table, every family / deopt relation is generated, and the
hand-rolled equivalents are deleted. A reproducibility gate
(`test/gate/cases_generator_reproducibility_test.go`) keeps it
that way.

## Why this spec exists

`vm/eval_*.go` is hand-rolled. `specialize/*.go` is hand-rolled.
`compile/opcode_caches.go` is hand-rolled. They were ported one
opcode at a time from CPython 3.14's `Python/bytecodes.c` and
related headers, with each port translating both sides
(specializer write, VM read, compile-side cache size, stack
effect, family membership) separately. Five places per opcode
that all have to agree.

CPython does not have this problem. Every opcode in CPython is
defined exactly once in `Python/bytecodes.c` using a small DSL
(`inst`, `op`, `macro`, `family`, `pseudo`, `tier1`, `tier2`,
`label`). A pipeline of generators under `Tools/cases_generator/`
then emits:

| CPython output file | What it drives |
|---------------------|----------------|
| `Python/generated_cases.c.h` | Tier-1 dispatch bodies inside `_PyEval_EvalFrameDefault` |
| `Python/executor_cases.c.h` | Tier-2 uop bodies inside the executor loop |
| `Include/internal/pycore_opcode_metadata.h` | Stack effects, cache sizes, names, family table, deopt map |
| `Include/internal/pycore_uop_metadata.h` | Uop names, flags, output effects |
| `Include/opcode_ids.h` | Opcode numeric IDs |
| `Python/opcode_targets.h` | Computed-goto dispatch table |
| `Lib/_opcode_metadata.py` | Python-side mirror used by `dis` |

The cache layout (`_PyLoadGlobalCache { uint16_t counter; uint16_t
index; uint16_t module_keys_version; uint16_t
builtin_keys_version; }` in `Include/internal/pycore_code.h:117`)
is *paired* with the `inst(LOAD_GLOBAL_MODULE, ...)` body in
`Python/bytecodes.c` via a `_PyLoadGlobalCache *cache = ...`
declaration the generator parses out of the body. The generator
enforces the pairing. Drift is a build error.

Hand-porting throws this out. We have, today, in gopy:

- `compile/opcode_caches.go` declaring `LOAD_GLOBAL: 4` codeunits.
- `specialize/load_global.go` writing cells {2,3,4} (until 1714 P0
  prework fixed it to {1,2,3}).
- `vm/eval_specialized_load_global.go` reading cell 4 (until the
  same fix moved it to cell 1).
- `objects/dict.go` exposing `GetKeysVersion()`.
- `compile/opcodes_gen.go` numbering LOAD_GLOBAL_MODULE
  independently of CPython's opcode_ids.h.

Five files. Five sources of truth. The same bug class is latent in
LOAD_ATTR_INSTANCE_VALUE, LOAD_ATTR_SLOT, STORE_ATTR_INSTANCE_VALUE,
CALL_PY_EXACT_ARGS, BINARY_SUBSCR_LIST_INT, and every other
specialized arm. We have not seen the bugs yet because the inputs
that trigger them have not been exercised. The 1712 specializer
audit and the 1713 byte-equality gate are both going to surface
them, one by one, the slow way. Or we can port the generator and
delete the class.

The same argument applies to tier-2 uops. Spec 1712 hand-ported 14
of ~285 uops. Each one is a fresh translation from
`Python/optimizer_bytecodes.c`. Without the generator, every uop
is a fresh chance to drift from the tier-1 body of the same name.
CPython's generator emits both sides from the same source.

## CPython architecture

CPython's pipeline, with line counts taken from a fresh
`$HOME/cpython-314` clone at v3.14.5:

```
Python/bytecodes.c                                            5549 lines
  ├── 92 inst()                          tier-1 + tier-2 fused body
  ├── 145 op()                           tier-2-only or composable
  ├── 109 macro()                        composition of op + cache cells
  ├── 17 family()                        specialization families
  ├── 11 pseudo()                        compiler-only synthetics
  └── 3 label()                          shared error-handler labels

Python/optimizer_bytecodes.c                                  1107 lines
  ├── op() bodies that override the tier-1 op() of the same name
  └── used for tier-2 abstract interpretation (sym values, guards)

Include/internal/pycore_code.h           cache structs per opcode family
Include/opcode_ids.h                     opcode numeric IDs (generated)
Include/internal/pycore_opcode_metadata.h  per-opcode metadata (generated)

Tools/cases_generator/                                        5811 lines
  ├── lexer.py            395 lines     tokenizer for the C-with-DSL input
  ├── plexer.py           124 lines     peekable lexer wrapper
  ├── parser.py            78 lines     thin entry point on top of parsing.py
  ├── parsing.py          743 lines     real parser; produces inst/op/macro/family AST
  ├── analyzer.py        1207 lines     resolves macros, computes effects, walks bodies
  ├── stack.py            737 lines     stack-effect tracker; emits push/pop sequences
  ├── cwriter.py          179 lines     C output sink with indent tracking
  ├── generators_common.py 708 lines    body emission shared between tier-1 / tier-2
  ├── tier1_generator.py  306 lines     emits generated_cases.c.h
  ├── tier2_generator.py  228 lines     emits executor_cases.c.h
  ├── optimizer_generator.py 244 lines  emits abstract interpreter cases
  ├── opcode_id_generator.py 63 lines   emits opcode_ids.h
  ├── opcode_metadata_generator.py 418 lines  emits pycore_opcode_metadata.h
  ├── py_metadata_generator.py 95 lines emits Lib/_opcode_metadata.py
  ├── target_generator.py  94 lines     emits opcode_targets.h (computed-goto)
  ├── uop_id_generator.py  79 lines     emits pycore_uop_ids.h
  └── uop_metadata_generator.py 98 lines emits pycore_uop_metadata.h
```

The full toolchain is 5811 lines of Python. The inputs are 6656
lines of C (bytecodes.c + optimizer_bytecodes.c). The C-side
generated output across all targets is roughly 18000 lines.

Five concepts carry most of the weight, and the gopy port must
preserve all of them:

**Stack effects.** Every `inst()` declaration looks like
`inst(LOAD_FAST, (-- value))` or
`inst(BINARY_OP, (lhs, rhs -- res))`. The names and order on each
side declare the stack inputs (popped) and outputs (pushed). The
generator uses these to emit `PEEK`, `STACK_SHRINK`, `STACK_GROW`,
and the local-variable initialization that gives the C body a
typed binding for each input/output. `stack.py` tracks the running
effect across a `macro()` composition and emits the minimum-cost
push/pop sequence.

**Cache cells.** A `macro(LOAD_GLOBAL) = unused/1 + counter/1 +
globals_version/2 + builtins_version/1 + _LOAD_GLOBAL +
_PUSH_NULL_CONDITIONAL` declares the cache layout inline. The
`/N` suffix declares how many codeunits each cell occupies. The
generator computes the offset of each named cell, threads a
`_PyLoadGlobalCache *cache = (_PyLoadGlobalCache *)next_instr;` at
the top of the body, and rewrites `cache->index` references to
the right offset in the codeunit stream. The struct in
`pycore_code.h` matches by name and order. Drift is a compile
error in CPython.

**Families and deopt.** `family(LOAD_GLOBAL, INLINE_CACHE_ENTRIES_LOAD_GLOBAL)
= { LOAD_GLOBAL_MODULE, LOAD_GLOBAL_BUILTIN };` declares that
LOAD_GLOBAL's specialized arms are LOAD_GLOBAL_MODULE and
LOAD_GLOBAL_BUILTIN, all sharing the same cache size. The
generator emits the deopt map: a specialized arm that hits
`DEOPT_IF(cond)` jumps back to the family parent without changing
the cache layout. This is the mechanism gopy implements ad-hoc in
`specialize/deopt.go` today.

**`DEOPT_IF` / `EXIT_IF` / `ERROR_IF`.** These three macros in the
body desugar into the three exit paths the generator must emit:
deopt back to the parent opcode, exit the tier-2 trace, or jump
to the per-opcode error label. The generator knows which macro is
legal in which context (DEOPT_IF only inside specialized arms,
EXIT_IF only in tier-2, ERROR_IF anywhere) and emits the right
restoration code (stack rollback, refcount adjustment) for each.

**Tier-1 / tier-2 fusion.** `inst(NAME, ...)` declares a body that
serves both tiers. `op(NAME, ...)` declares a tier-2-only body or
a tier-1-only body, depending on whether it's referenced by a
`macro()`. `optimizer_bytecodes.c` overrides selected `op()`
bodies with abstract-interpretation versions used during
optimization. The generator emits the tier-1 case, the tier-2 uop
body, and the optimizer-case body from the same source. Spec
1712's hand-rolled approach has all three drift independently.

## gopy current state (2026-05-16)

| Layer | gopy file(s) | What it should generate from | Status |
|-------|--------------|------------------------------|--------|
| Opcode IDs | `compile/opcodes_gen.go` | `Include/opcode_ids.h` | hand-rolled, generated-name notwithstanding. Source-of-truth: a Python script we run by hand, not the DSL. |
| Cache widths | `compile/opcode_caches.go` | `pycore_opcode_metadata.h` `_PyOpcode_Caches[]` | hand-rolled. 4 codeunits for LOAD_GLOBAL, etc. |
| Stack effects | inline in each `eval_*.go` arm | `pycore_opcode_metadata.h` `_PyOpcode_num_popped`/`num_pushed` | hand-rolled, no cross-check. |
| Cache layouts (struct) | `specialize/cache.go` `SetCacheCell` / `CacheCell` | `pycore_code.h` `_Py<Op>Cache` structs | hand-rolled offsets. Cell index is a magic number at every call site. |
| Tier-1 dispatch | `vm/eval_simple.go` | `Python/generated_cases.c.h` | hand-rolled switch statement. |
| Tier-1 specialized arms | `vm/eval_specialized_*.go` | `Python/generated_cases.c.h` (same cases) | hand-rolled. |
| Tier-2 uops | `vm/uops/*.go` (per 1712) | `Python/executor_cases.c.h` | 14 of ~285 hand-rolled. |
| Family table | `specialize/quicken.go` | `pycore_opcode_metadata.h` `_PyOpcode_Caches`+family arrays | hand-rolled. |
| Deopt map | `specialize/deopt.go` | `pycore_opcode_metadata.h` `_PyOpcode_Deopt[]` | hand-rolled. |
| Specializer skeletons | `specialize/*.go` per-family | DSL family declarations | hand-rolled. |

Every row above is in scope. By the end of this spec, every "hand-rolled"
becomes "generated", or the row gets explicitly carved out with a
documented reason.

## Files in scope

Sources of truth live under `/Users/apple/cpython-314/` and are
mirrored into `tools/cases_generator/inputs/` so the generator
runs hermetically. Every file below is ported in full, with
`// CPython: <file>:<line> <function>` citations on the Go
emitters and on any non-trivial bridging glue.

| # | CPython source | gopy target | Why |
|---|----------------|-------------|-----|
| A | `Tools/cases_generator/lexer.py` | `tools/cases_generator/lexer.py` (vendored verbatim) | DSL tokenizer. No Go port. Run under host Python 3.14. |
| B | `Tools/cases_generator/plexer.py` | `tools/cases_generator/plexer.py` (vendored) | Peekable wrapper. |
| C | `Tools/cases_generator/parser.py` + `parsing.py` | vendored | DSL parser. Produces `Inst`, `Op`, `Macro`, `Family`, `Pseudo` AST nodes. |
| D | `Tools/cases_generator/analyzer.py` | vendored | Resolves macros, computes stack effects, walks bodies for `DEOPT_IF`/`ERROR_IF`/`EXIT_IF`. |
| E | `Tools/cases_generator/stack.py` | vendored | Stack-effect tracker; emits push/pop sequences. |
| F | `Tools/cases_generator/cwriter.py` | vendored *and* re-implemented as `gowriter.py` | C writer + a Go writer sharing the same indent-tracking and emit API. |
| G | `Tools/cases_generator/generators_common.py` | vendored, plus a `go_generators_common.py` companion | Body emission. The companion handles Go-specific macro expansion (`DEOPT_IF` → `return 0, false`, `ERROR_IF` → `return e.raise(err)`, etc). |
| H | `Tools/cases_generator/opcode_id_generator.py` | + `gopy_opcode_id_generator.py` | New emitter targeting `compile/opcode_ids_gen.go`. |
| I | `Tools/cases_generator/opcode_metadata_generator.py` | + `gopy_opcode_metadata_generator.py` | New emitter targeting `compile/opcode_metadata_gen.go` (replaces `compile/opcode_caches.go`). |
| J | `Tools/cases_generator/uop_id_generator.py` + `uop_metadata_generator.py` | + Go companions | Tier-2 uop tables. |
| K | `Tools/cases_generator/tier1_generator.py` | + `gopy_tier1_generator.py` | Emits `vm/eval_dispatch_gen.go`: the dispatch switch + per-opcode body harness. |
| L | `Tools/cases_generator/tier2_generator.py` | + `gopy_tier2_generator.py` | Emits `vm/eval_uops_gen.go`: uop dispatch + body harness. |
| M | `Tools/cases_generator/optimizer_generator.py` | + `gopy_optimizer_generator.py` | Emits `compile/optimizer_cases_gen.go` for spec 1712's abstract interpreter. |
| N | `Tools/cases_generator/target_generator.py` | (not ported) | CPython-specific computed-goto. Go's `switch` is fine. Documented carve-out. |
| O | `Tools/cases_generator/py_metadata_generator.py` | (vendored only) | Emits `Lib/_opcode_metadata.py`; gopy already vendors that file via 1710 T5.1. No regeneration needed; we ship CPython's. |
| P | `Python/bytecodes.c` (v3.14.5) | `tools/cases_generator/inputs/bytecodes.c` | The single source. Frozen per CPython tag; bumped together with 1707 sync. |
| Q | `Python/optimizer_bytecodes.c` (v3.14.5) | `tools/cases_generator/inputs/optimizer_bytecodes.c` | Tier-2 source. |
| R | `Include/internal/pycore_code.h` | `tools/cases_generator/inputs/pycore_code.h` | Cache struct definitions. Parsed by a new `cache_struct_parser.py` to emit `specialize/cache_layouts_gen.go`. |

Output Go files (all `*_gen.go`, all under generator control,
none hand-edited):

| Output file | Lines (estimated) | Replaces |
|-------------|-------------------|----------|
| `compile/opcode_ids_gen.go` | ~600 | `compile/opcodes_gen.go` |
| `compile/opcode_metadata_gen.go` | ~1500 | `compile/opcode_caches.go` |
| `compile/optimizer_cases_gen.go` | ~2500 | parts of `vm/uops/*.go` (abstract interp) |
| `specialize/cache_layouts_gen.go` | ~400 | implicit layout knowledge across `specialize/*.go` |
| `specialize/family_gen.go` | ~200 | `specialize/quicken.go` family table + `specialize/deopt.go` map |
| `vm/eval_dispatch_gen.go` | ~4000 | core of `vm/eval_simple.go` + `vm/eval_specialized*.go` |
| `vm/eval_uops_gen.go` | ~3000 | core of `vm/uops/*.go` |

## Phase index

Each phase ports one block end to end. Status lives on the
Checklist at the bottom of this spec, mirrored per row here. The
phase order is chosen so that every phase ends with a green CI,
including phases where the generator is partially wired: each
emitter ships with a parity test that diffs its output against the
hand-rolled file it will eventually replace, and only flips the
hand-rolled file's role to "fallback" when the parity test goes
green.

| Phase | Block | Gate | Status |
|-------|-------|------|--------|
| 0 | Vendor `Tools/cases_generator/` verbatim. Mirror inputs (`bytecodes.c`, `optimizer_bytecodes.c`, `pycore_code.h`) under `Tools/cases_generator/inputs/` at the 3.14.5 hash. Add `Tools/regen-cases/` (Go driver) that invokes the generators against our vendored inputs and reproduces CPython's own outputs into a scratch dir for diffing. | upstream reproducibility: regenerating CPython's 9 generator outputs from our vendored inputs matches the files in `$HOME/cpython-314` byte for byte (header lines excluded) | DONE (CI pending) |
| 1 | Output abstraction. Port `cwriter.py` into a `gowriter.py` sibling that shares the indent/scope API but emits Go syntax. Implement `go_generators_common.py` with macro→Go bindings for the constant macros (`PyStackRef_FromPyObject*`, `PyStackRef_AsPyObject*`, `STACK_SHRINK`, `STACK_GROW`, `PEEK`, `POKE`, etc). | unit-test corpus: 30 hand-written macro snippets emit known-good Go | DONE (corpus at 20/30; remaining 10 stage with Phase 5 op signatures) |
| 2 | Metadata + opcode-id emitters. Ship `gopy_opcode_id_generator.py` and `gopy_opcode_metadata_generator.py`. Output `compile/opcode_ids_gen.go` + `compile/opcode_metadata_gen.go`. **Parity test:** the generated tables equal `compile/opcodes_gen.go` + `compile/opcode_caches.go` for every opcode currently in gopy. Once green, the hand-rolled files get deleted. | `go test ./compile -run TestOpcodeMetadataParity` green; deletion lands | DONE (2.1-2.3; 2.4 deletion pending) |
| 3 | Cache-layout emitter. New `cache_struct_parser.py` reads `pycore_code.h`, emits `specialize/cache_layouts_gen.go` with typed accessors (`(*LoadGlobalCache).Index`, `.ModuleKeysVersion`, etc) backed by the codeunit slice. Migrate every `SetCacheCell` / `CacheCell` call site in `specialize/*.go` and `vm/eval_specialized_*.go` to typed accessors. The LOAD_GLOBAL cell-1 vs cell-4 bug class becomes a compile error. | every specialize/* + eval_specialized_* file builds; existing tests green; one new test (`TestCacheLayoutTypedAccess`) asserts the struct sizes match `_PyOpcode_Caches[]` | TODO |
| 4 | Family + deopt emitter. Generator emits `specialize/family_gen.go` carrying the family table (`map[Opcode][]Opcode`), deopt map (`Opcode→Opcode` parent), and per-family cache-size guard. Replace `specialize/quicken.go` family literal + `specialize/deopt.go` map. **Parity test:** generated tables equal current hand-rolled ones for every opcode we specialize today. | parity test green; deletion lands | DONE (8abf069) |
| 5 | Tier-1 harness emitter. Generator emits `vm/eval_dispatch_gen.go`: the switch statement, per-opcode prologue (stack peek to typed locals), epilogue (stack push of typed outputs, cache advance), deopt path, error path. The body itself remains in hand-written Go: each opcode has a function `op<NAME>(e *evalState, oparg uint32, in <inputs>) (out <outputs>, err error)` whose signature is **derived from the DSL** and enforced by the generator. Any hand-written function whose signature diverges from the DSL stack effect is a build error. | every opcode in gopy currently routed through `vm/eval_simple.go` is now routed through `vm/eval_dispatch_gen.go`; full `go test ./...` green | TODO |
| 6 | Specialized arms. Same harness as Phase 5 but generates the specialized cases (`LOAD_GLOBAL_MODULE` etc), wires `DEOPT_IF` to a generated `return deoptTo<PARENT>(e)` shim. Hand-written specializer fast paths in `vm/eval_specialized_*.go` are reduced to per-opcode body functions; the cache decode, deopt branch, and cache advance live in the generator output. | every `vm/eval_specialized_*.go` file shrinks to opcode bodies only; the LOAD_GLOBAL bug's regression test (a fixture that stresses cache cell boundaries) is added | TODO |
| 7 | Tier-2 uop harness. `gopy_tier2_generator.py` emits `vm/eval_uops_gen.go`. The 14 uops 1712 hand-ported are re-rooted onto generated harness; remaining ~270 uops become trivial Go-body inserts. **Parity test:** for every uop name shared with tier-1 (e.g. `_LOAD_FAST`), the tier-1 body and the tier-2 uop body call into the same opcode-body function. | tier-2 trace executor runs the same 1712 P2 microbenchmark with hand-rolled and generated uops side by side; perf within 2%, results identical | TODO |
| 8 | Body translation pilot. Pick 10 trivial opcodes (`NOP`, `POP_TOP`, `POP_TOP_LOAD_CONST_INLINE_BORROW`, `LOAD_FAST`, `LOAD_FAST_BORROW`, `STORE_FAST`, `LOAD_CONST`, `RETURN_VALUE`, `RESUME_CHECK`, `END_FOR`). Add a C-body→Go-body translator under `tools/cases_generator/body_translator.py` that handles the constrained subset of C these bodies use. Generated Go bodies replace the hand-written ones; CI must stay green. | 10 opcodes have zero hand-written Go; CI green; size of the body-translator subset documented in `tools/cases_generator/SUBSET.md` | TODO |
| 9 | Body translation scale-up. Translate every remaining opcode body. Each opcode that survives translation has its hand-written Go body deleted. Opcodes that the translator cannot handle (calls into runtime helpers gopy spells differently, refcount idioms gopy doesn't have because Go is GC'd) stay hand-written but with a generator-emitted stub asserting the signature. | hand-written opcode bodies count drops below 30 (escape hatches only, documented per opcode); reproducibility gate green | TODO |
| Gate | `tools/regen-cases.sh && git diff --exit-code` runs in CI. Any drift between source DSL and emitted Go fails the build. | gate green | TODO |

## Phase 0 — vendor the generator

Bring `Tools/cases_generator/` into the gopy repo under
`tools/cases_generator/` verbatim. The directory layout mirrors
CPython's: `lexer.py`, `plexer.py`, `parser.py`, `parsing.py`,
`analyzer.py`, `stack.py`, `cwriter.py`, `generators_common.py`,
and the eight per-target emitters. No edits. The vendored copy
carries a header comment naming the CPython commit it was pulled
from, identical to how `stdlib/` files name their source.

Inputs: `Python/bytecodes.c`, `Python/optimizer_bytecodes.c`,
`Include/internal/pycore_code.h` get mirrored under
`tools/cases_generator/inputs/`. Same pin: CPython 3.14.5. Spec
1707 (CPython 3.14.x sync) is the upstream rollup; this spec adds
three rows to 1707's checklist for these inputs.

Driver: `tools/regen-cases.sh` is a thin bash wrapper that exports
`PYTHONPATH=tools/cases_generator` and invokes each generator with
the right input files and output paths. In Phase 0 the script
emits only into a scratch directory under `/tmp` and runs `diff`
against CPython's actual output in `$HOME/cpython-314/Python/` and
`$HOME/cpython-314/Include/internal/`. A green diff proves the
toolchain is wired correctly before any Go work starts.

The Phase 0 gate runs in CI: `tools/regen-cases.sh --check-upstream`
regenerates the C files into `/tmp`, diffs against the vendored
CPython tree, fails on any divergence. This catches accidental
edits to the vendored generator and the case where a CPython 3.14
patch release moves the inputs without touching the generator.

| Step | Status | Commit |
|------|--------|--------|
| Vendor `Tools/cases_generator/` under `Tools/cases_generator/` | DONE | - |
| Mirror `bytecodes.c`, `optimizer_bytecodes.c`, `pycore_code.h` under `Tools/cases_generator/inputs/` | DONE | - |
| `Tools/regen-cases/` (Go driver) invokes each upstream generator into a scratch dir | DONE | - |
| `go run ./Tools/regen-cases --check-upstream` diff-clean against CPython 3.14.5 generated files | DONE | - |
| CI job `cases-generator-upstream-parity` green | TODO | - |

## Phase 1 — output abstraction

CPython's `cwriter.py` is a 179-line indent-tracking C-syntax sink.
It exposes `emit(text)`, `start_line()`, `block()` (context manager
for `{...}`), `set_position()`, `set_lineno()`. Generators write to
a `CWriter` and get well-formatted C with `#line` directives.

Phase 1 introduces `tools/cases_generator/gowriter.py` with the
same surface area, emitting Go. The `block()` context emits `{` /
`}` the same way; `start_line()` honors `gofmt`-friendly
indentation; `set_lineno()` emits `//line` directives keyed to the
DSL source location so that runtime panics in generated code point
back to `Python/bytecodes.c`.

A second file, `go_generators_common.py`, mirrors
`generators_common.py` but binds DSL macros to Go expressions. The
constant macros (the ones whose expansion does not depend on the
surrounding stack effect) are the Phase 1 deliverable:

| DSL macro | Go expansion |
|-----------|--------------|
| `PyStackRef_AsPyObjectBorrow(r)` | `r.AsObject()` |
| `PyStackRef_FromPyObjectNew(o)` | `stackref.FromObject(o)` |
| `PyStackRef_FromPyObjectImmortal(o)` | `stackref.FromObjectImmortal(o)` |
| `PyStackRef_IsNull(r)` | `r.IsNull()` |
| `STACK_SHRINK(n)` | (handled by the harness, not the body) |
| `STACK_GROW(n)` | (handled by the harness) |
| `PEEK(i)` | (handled by the harness) |
| `POKE(i, v)` | (handled by the harness) |
| `Py_INCREF(o)` / `Py_DECREF(o)` | (no-op; Go is GC'd) |
| `Py_XDECREF(o)` | (no-op) |
| `JUMPBY(n)` | `e.jumpBy(n)` |
| `next_instr` | `e.f.NextInstr` |
| `_PyFrame_GetCode(frame)` | `e.f.Code` |
| `frame->localsplus[i]` | `e.f.Locals[i]` |
| `tstate` | `e` (the evalState) |
| `oparg` | `oparg` (passed as parameter) |
| `DEOPT_IF(cond)` | `if cond { return 0, false }` (specialized arms) |
| `EXIT_IF(cond)` | `if cond { return e.tier2Exit() }` (tier-2 only) |
| `ERROR_IF(cond, label)` | `if cond { return e.<label>() }` |
| `DECREF_INPUTS()` | (no-op; harness clears stack refs) |
| `DEAD(name)` | (no-op; informational) |

The Phase 1 corpus is 30 hand-written macro snippets covering each
binding above. The test is mechanical: `gowriter.py` emits the
binding, the result compiles, the result matches a checked-in
golden file. No real opcode bodies translated yet.

| Step | Status | Commit |
|------|--------|--------|
| `Tools/cases_generator/gowriter.py` lands | DONE | - |
| `Tools/cases_generator/go_generators_common.py` lands with the binding table above | DONE | - |
| 30-snippet golden-file corpus under `Tools/cases_generator/testdata/snippets/` | PARTIAL (20/30) | - |
| `go test ./Tools/regen-cases -run TestSnippetParity` (Go harness shelling out to Python) green | DONE | - |

## Phase 2 — opcode IDs + metadata

Two Go emitters land. `gopy_opcode_id_generator.py` walks the
analyzer's opcode list and emits `compile/opcode_ids_gen.go`:

```go
// Code generated by tools/cases_generator. DO NOT EDIT.
// Source: Python/bytecodes.c (CPython 3.14.5)
package compile

const (
    NOP                          Opcode = 0
    RESERVED                     Opcode = 17
    LOAD_FAST                    Opcode = 85
    LOAD_GLOBAL                  Opcode = 91
    LOAD_GLOBAL_MODULE           Opcode = 158
    LOAD_GLOBAL_BUILTIN          Opcode = 159
    // ...
)

var OpcodeName = map[Opcode]string{ ... }
```

`gopy_opcode_metadata_generator.py` emits
`compile/opcode_metadata_gen.go`:

```go
package compile

// CacheSize is the number of codeunits the inline cache occupies
// for this opcode. Generated from family() declarations and the
// macro() cache-cell list in Python/bytecodes.c.
var CacheSize = map[Opcode]int{
    LOAD_GLOBAL:         4,
    LOAD_GLOBAL_MODULE:  4,
    LOAD_GLOBAL_BUILTIN: 4,
    LOAD_ATTR:           9,
    // ...
}

// StackEffect carries the popped/pushed counts derived from the
// DSL stack effect declaration. Both counts may depend on oparg
// for variadic opcodes (BUILD_TUPLE, CALL, etc); in that case
// StackEffect.PoppedFn / PushedFn is non-nil.
var StackEffect = map[Opcode]Effect{ ... }

// Family lists the specialized arms of each adaptive opcode.
var Family = map[Opcode][]Opcode{
    LOAD_GLOBAL: {LOAD_GLOBAL_MODULE, LOAD_GLOBAL_BUILTIN},
    // ...
}

// Deopt is the inverse: every specialized arm maps to its family
// parent.
var Deopt = map[Opcode]Opcode{
    LOAD_GLOBAL_MODULE:  LOAD_GLOBAL,
    LOAD_GLOBAL_BUILTIN: LOAD_GLOBAL,
    // ...
}
```

The parity test (`compile/opcode_metadata_parity_test.go`) asserts
that the generated maps cover every key currently in
`compile/opcode_caches.go` with the same value. When green, the
hand-rolled file gets deleted in the same commit; the generated
file becomes the only source.

| Step | Status | Commit |
|------|--------|--------|
| `tools/cases_generator/gopy_opcode_id_generator.py` | DONE | 78e434b |
| `tools/cases_generator/gopy_opcode_metadata_generator.py` | DONE | this commit |
| `compile/opcode_ids_gen.go` + `compile/opcode_metadata_gen.go` checked in | DONE | this commit |
| Parity test green | DONE (3 tests; YIELD_VALUE escapes-flag delta logged, generator wins) | this commit |
| `compile/opcode_caches.go` deleted; references redirected | TODO | - |

## Phase 3 — typed cache layouts

The biggest single bug-class reduction in this spec. Today,
`specialize.SetCacheCell(code, instr, 1, idx)` writes a magic
number to a magic offset. The relationship between cell number
and the meaning of that cell lives in a comment, sometimes wrong.

Phase 3 ships `tools/cases_generator/cache_struct_parser.py`. It
reads `Include/internal/pycore_code.h`, parses each
`_Py<Op>Cache` struct, emits `specialize/cache_layouts_gen.go`:

```go
// Code generated by tools/cases_generator. DO NOT EDIT.
// Source: Include/internal/pycore_code.h (CPython 3.14.5)
package specialize

import "github.com/tamnd/gopy/compile"

// LoadGlobalCache mirrors Include/internal/pycore_code.h:117
// struct _PyLoadGlobalCache. Each codeunit cell maps to one
// uint16 field. Field offsets are checked at init time against
// compile.CacheSize[compile.LOAD_GLOBAL].
type LoadGlobalCache struct {
    code  []byte
    instr int
}

func LoadGlobalCacheAt(code []byte, instr int) LoadGlobalCache { ... }
func (c LoadGlobalCache) Counter() uint16            { ... }
func (c LoadGlobalCache) SetCounter(v uint16)        { ... }
func (c LoadGlobalCache) Index() uint16              { ... }
func (c LoadGlobalCache) SetIndex(v uint16)          { ... }
func (c LoadGlobalCache) ModuleKeysVersion() uint16  { ... }
func (c LoadGlobalCache) SetModuleKeysVersion(v uint16) { ... }
func (c LoadGlobalCache) BuiltinKeysVersion() uint16 { ... }
func (c LoadGlobalCache) SetBuiltinKeysVersion(v uint16) { ... }
```

Every call site in `specialize/*.go` and
`vm/eval_specialized_*.go` migrates to typed access. The
specializer writes `cache.SetIndex(uint16(idx))`. The VM reads
`cache.Index()`. Cell-4 vs cell-1 cannot happen: there is no cell
4, the struct has four fields and the type system enforces the
mapping.

A new test (`specialize/cache_layout_size_test.go`) asserts that
the codeunit size implied by each typed struct matches
`compile.CacheSize[op]` for the family parent of that struct. A
struct that overflows the family's reserved size is a test
failure.

| Step | Status | Commit |
|------|--------|--------|
| `tools/cases_generator/cache_struct_parser.py` | DONE | this commit |
| `specialize/cache_layouts_gen.go` lands with every `_Py<Op>Cache` typed | DONE | this commit |
| Migrate `specialize/load_global.go` + `vm/eval_specialized_load_global.go` to typed access | TODO | - |
| Migrate every other `specialize/*.go` + `vm/eval_specialized_*.go` | TODO | - |
| `specialize/cache.go` `SetCacheCell` / `CacheCell` deleted (no callers) | TODO | - |
| Cache-layout size test green | DONE | this commit |

## Phase 4 — family + deopt

`specialize/family_gen.go` carries:

```go
// Code generated by tools/cases_generator. DO NOT EDIT.
// Source: family() declarations in Python/bytecodes.c.
package specialize

import "github.com/tamnd/gopy/compile"

var Family = map[compile.Opcode][]compile.Opcode{
    compile.LOAD_GLOBAL: {
        compile.LOAD_GLOBAL_MODULE,
        compile.LOAD_GLOBAL_BUILTIN,
    },
    // ... 16 more
}

var DeoptParent = map[compile.Opcode]compile.Opcode{
    compile.LOAD_GLOBAL_MODULE:  compile.LOAD_GLOBAL,
    compile.LOAD_GLOBAL_BUILTIN: compile.LOAD_GLOBAL,
    // ...
}
```

Existing hand-rolled equivalents in `specialize/quicken.go` and
`specialize/deopt.go` shrink to *consumers* of these tables. The
parity test asserts that for every opcode in either side, the
hand-rolled and generated versions agree. Once green, the literal
tables are deleted; the consumers stay.

| Step | Status | Commit |
|------|--------|--------|
| `gopy_family_generator.py` (or fold into Phase 2's metadata emitter) | DONE | 8abf069 |
| `specialize/family_gen.go` lands | DONE | 8abf069 |
| `specialize/quicken.go` + `specialize/deopt.go` consume generated tables | DONE | 8abf069 |
| Parity test green; old literals deleted | DONE | 8abf069 |

## Phase 5 — tier-1 dispatch harness

The harness owns:

1. The dispatch `switch` keyed on opcode.
2. Per-opcode prologue: peek stack inputs into typed locals named
   per the DSL declaration.
3. Per-opcode epilogue: push outputs, advance `next_instr` by
   `1 + CacheSize[op]`.
4. Error path: every `ERROR_IF(cond, label)` becomes a generated
   helper call.
5. Deopt path (for specialized arms): every `DEOPT_IF(cond)`
   becomes a return that signals fallback to the unspecialized
   arm.

The body is *not* generated in this phase. Each opcode has a
hand-written Go function whose signature is fully determined by
the DSL:

```go
// Generator-derived signature:
//   inst(LOAD_FAST, (-- value))
// produces:
func opLOAD_FAST(e *evalState, oparg uint32) (value stackref.Ref, err error) {
    return e.f.Locals[oparg], nil
}

// inst(BINARY_OP, (lhs, rhs -- res))
// produces:
func opBINARY_OP(e *evalState, oparg uint32, lhs, rhs stackref.Ref) (res stackref.Ref, err error) {
    ...
}
```

The generator emits a *stub* per opcode at the bottom of
`vm/eval_dispatch_gen.go`:

```go
//go:linkname op<NAME> github.com/tamnd/gopy/vm.op<NAME>
var _ = op<NAME> // build error if op<NAME> is missing or signature drifts
```

A missing body or a wrong signature is a build error, not a
runtime error. This is the entire point of the spec: the same
class of bug that took down 1713 P2 work for half a day cannot
exist by the end of Phase 5.

The harness also drives the cache: a specialized arm gets
`cache := LoadGlobalCacheAt(code, instr)` injected before the
body, so the body never reaches for raw codeunits.

| Step | Status | Commit |
|------|--------|--------|
| `gopy_tier1_generator.py` lands (as `Tools/bytecodes_gen` Go emitter) | DONE | (prior phase) |
| `vm/eval_dispatch_gen.go` covers every unspecialized opcode (skeleton; bodies pending Phase 8) | PARTIAL (107 arms, bodies stubbed) | this commit |
| `vm/eval_simple.go` shrinks to evalLoop scaffolding only (frame setup, exit handling) | PARTIAL (NOP, POP_TOP routed) | this commit |
| `go test ./vm` green | DONE | this commit |
| CPython-parity harness (`Tools/bytecodes_gen/cpython_parity_test.go`) lifts `Lib/test/test_generated_cases.py` fixtures and prints rolling coverage | DONE (5 / 10 fixtures translate today) | `f97a926` |
| Bytecodes.c coverage gauge (`Tools/bytecodes_gen/cpython_coverage_test.go`) walks every `inst()` in CPython 3.14.5's `Python/bytecodes.c` and reports the bail histogram | DONE (14 / 118 inst() bodies translate today) | `a93336d` |

### CPython-parity gate

`Tools/bytecodes_gen/cpython_parity_test.go` is the spec's authority
on action-translator faithfulness. Each fixture is a verbatim copy
of an `(input, output)` pair from
`Lib/test/test_generated_cases.py`, wrapped in BEGIN/END markers and
fed through `ParseBytecodes → AnalyzeInst → TranslateBody`. Each
row carries:

- `bail=true` while the translator falls back to a panic-stub. The
  harness asserts the fallback note's prefix stays stable so a
  drift in error wording shows up as a test failure rather than a
  silent regression.
- `want=[...substr...]` once the translator handles the shape. The
  harness asserts the rendered Go body contains every substring.

`TestCPythonParityFixtures` logs `coverage: PASS / N fixtures
translate (bail=B)` so the porting auto-flow can read progress
without parsing test output structure. Coverage growth is
monotonic: a fixture never moves from `bail=false` back to
`bail=true`. Rows are never removed; when CPython retires a test
we mirror the deletion in a separate commit so blame stays
honest.

### Bytecodes.c coverage gauge

`Tools/bytecodes_gen/cpython_coverage_test.go` is the
complementary, exhaustive gauge: it walks every `inst()` in CPython
3.14.5's `Python/bytecodes.c`, runs each through the full pipeline
(`ParseBytecodes → AnalyzeInst → TranslateBody`), and groups the
bail reasons. The headline number is `N / total inst() bodies
translate`; the bail histogram (`bail (count) reason  names...`)
exposes which translator extension yields the most leverage.

A hard floor (`const minTranslates`) in the test refuses to let the
count regress. Bump it (never down) when a translator change flips
more bodies; the porting auto-flow reads that constant to know
progress without scraping logs.

Caveat: the gauge tracks *parser coverage* — a body "translates"
when no stage rejects it. Compile-correctness of the emitted Go is
verified separately by the strict `dispatchGenSupported`
whitelist, which gates which opcodes route through `dispatchGen` in
the live eval loop. The two layers measure different things on
purpose: the gauge tells us "how much of bytecodes.c does the
translator parse without bailing", the whitelist tells us "which
opcodes have we audited end-to-end and run in production".

### Migration progress

The harness routes every opcode through one of three layers, in
order: generated (`dispatchGen`, gated by `dispatchGenSupported`),
hand-written staging (`dispatchHandwritten`), and the legacy
`trySimple` panel. The goal is to flip every opcode into the
generated column. Each row migrates exactly once. Update these
tables whenever an opcode moves.

#### Generated (`dispatchGen`)

These arms come straight out of `Tools/bytecodes_gen` and the
action translator. The whitelist in
`vm/dispatch_gen_whitelist.go` controls which ones the dispatcher
actually consults; an opcode lands here only after its generated
body has been audited byte-equivalent to the prior arm.

Each row carries a **Status** (`generated` once it lands in
`dispatchGen` and the whitelist; `staging` while still in
`dispatchHandwritten`; `legacy` while still in `trySimple` and
friends) and a **Commit** stamp so the porting auto-flow can
diff the tables against the tree without re-reading every
panel.

| Opcode | Status | Commit | Translator shape | Notes |
|--------|--------|--------|------------------|-------|
| `NOP` | generated | `71b1fd1` | empty body | trivial |
| `POP_TOP` | generated | `71b1fd1` | `PyStackRef_CLOSE(value)` | exercises stack-ref close |
| `JUMP_FORWARD` | generated | `0d54073` | `JUMPBY(oparg)` | body-driven terminator |
| `PUSH_NULL` | generated | `0d47064` | output `= PyStackRef_NULL` | output-assignment statement |
| `LOAD_FAST` | generated | `b842a4a` | output `= PyStackRef_DUP(GETLOCAL(oparg))` | GETLOCAL rvalue + Dup |
| `LOAD_FAST_BORROW` | generated | `b842a4a` | same body as LOAD_FAST | borrow collapses under Go GC |
| `LOAD_FAST_AND_CLEAR` | generated | `b842a4a` | LOAD_FAST plus `GETLOCAL(oparg) = PyStackRef_NULL` | GETLOCAL lvalue |
| `STORE_FAST` | generated | `b842a4a` | `_PyStackRef tmp = GETLOCAL(oparg); GETLOCAL(oparg) = value; PyStackRef_XCLOSE(tmp)` | C-local decl + lvalue |
| `JUMP_BACKWARD_NO_INTERRUPT` | generated | `337d126` | `JUMPBY(-oparg)` | shares JUMPBY body with JUMP_FORWARD; `JUMP_BACKWARD` proper stays handwritten for breaker poll |
| `END_SEND` | generated | `8ac6d1c` | `val = value; DEAD(value); PyStackRef_CLOSE(receiver)` | bit-equivalent to handwritten body in eval_simple.go |

#### Hand-written staging (`dispatchHandwritten`)

Bodies live in `vm/eval_dispatch_handwritten.go` under the
typed `op<NAME>(oparg) -> (next, retVal, retErr, retDone, ok,
err)` signature. Each row leaves this table once the action
translator can render an equivalent generated body and the
whitelist accepts it. **Status** flips to `generated` and the
row migrates up to the previous table once that happens; the
**Commit** column stamps the flip (or the staging landing
commit, while still here) so we can audit the chain.

| Opcode | Status | Commit | Blocker | Notes |
|--------|--------|--------|---------|-------|
| `LOAD_CONST` | staging | - | needs `co_consts[oparg]` indexing in translator | hand-written body wraps via `wrapConst` |
| `LOAD_FAST_CHECK` | staging | - | `_PyEval_FormatExcCheckArg` helper port | adds null-guard error path |
| `DELETE_FAST` | staging | - | same as LOAD_FAST_CHECK | STORE_FAST plus unbound error |
| `RETURN_VALUE` | staging | - | frame unlinking / `SAVE_STACK` | terminator that returns TOS |
| `INTERPRETER_EXIT` | staging | - | `tstate->current_frame` machinery | same body as RETURN_VALUE today |
| `COPY` | staging | - | sized-input `unused[oparg-1]` peek | `stack[-oparg]` peek + dup |
| `SWAP` | staging | - | sized-input `unused[oparg-2]` peek | `stack[-oparg] <-> stack[-1]` |
| `IS_OP` | staging | - | `Py_Is` + `PyStackRef_AsPyObjectBorrow` + ternary + C-int local | identity compare + bool push |
| `BUILD_LIST` | staging | - | sized inputs + `objects.NewList` constructor | sized pop + list constructor |
| `BUILD_TUPLE` | staging | - | sized inputs + `objects.NewTuple` constructor | sized pop + tuple constructor |
| `BUILD_SLICE` | staging | - | conditional 2-vs-3 arg constructor | 2/3-arg slice constructor |
| `GET_ITER` | staging | - | `PyObject_GetIter` → `objects.Iter` with `ERROR_IF(NULL)` | hand-written body checks nil return |
| `POP_JUMP_IF_TRUE` | staging | - | `JUMPBY(flag ? oparg : NOT_TAKEN)` ternary + `PyStackRef_IsFalse` | shared body with the other three |
| `POP_JUMP_IF_FALSE` | staging | - | same as POP_JUMP_IF_TRUE | shared body |
| `POP_JUMP_IF_NONE` | staging | - | macro composition (`_IS_NONE + _POP_JUMP_IF_TRUE`) | shared body, identity check |
| `POP_JUMP_IF_NOT_NONE` | staging | - | macro composition (`_IS_NONE + _POP_JUMP_IF_FALSE`) | shared body, identity check |

#### Legacy (`trySimple` and friends)

Every remaining opcode in `vm/eval_simple.go`,
`vm/eval_import.go`, `vm/eval_match.go`, and `vm/eval_gen.go`.
These flip into staging (or straight into the generated column
once the translator covers their shape) as the migration
progresses. Tracking individual rows here would duplicate the
switch statements themselves, so instead we track the *count*
that still has to move.

| Panel | Approx. remaining arms |
|-------|------------------------|
| `trySimple` (vm/eval_simple.go) | ~70 |
| `tryImport` (vm/eval_import.go) | ~6 |
| `tryGen` (vm/eval_gen.go) | ~6 |
| `tryMatch` (vm/eval_match.go) | ~9 |

Phase 5.3 closes when the legacy panels are empty and the
staging panel is empty too. Phase 5.4 closes when `go test ./vm`
stays green across that final flip.

## Phase 6 — specialized arms

Same harness; the specialized switch arms live in
`vm/eval_dispatch_gen.go` alongside the parent's case. The body
functions for specialized opcodes have the same calling convention
as Phase 5; the harness handles cache decode, the deopt branch
(returns a flag the harness interprets as "fall through to the
parent's body"), and the cache advance.

The LOAD_GLOBAL regression test lands here: a fixture that
specializes LOAD_GLOBAL, then introspects the generated bytecode
to assert the next instruction's first byte is unchanged. The same
fixture catches every future opcode whose cache crosses the
boundary into the next instruction.

| Step | Status | Commit |
|------|--------|--------|
| Specialized cases emitted in `vm/eval_dispatch_gen.go` | TODO | - |
| `vm/eval_specialized_*.go` shrinks to bodies only | TODO | - |
| LOAD_GLOBAL cache-boundary regression test | TODO | - |
| Per-family boundary tests for the other ~10 specialized families | TODO | - |

## Phase 7 — tier-2 uops

Same harness pattern against
`Python/optimizer_bytecodes.c` for the override side and the same
`bytecodes.c` for the shared `op()` bodies. The win is twofold:

1. Tier-1 LOAD_FAST and tier-2 `_LOAD_FAST` route through the same
   `opLOAD_FAST` body function. They cannot disagree.
2. The remaining ~270 uops 1712 has not hand-ported land for free:
   the generator emits their bodies (or, in the body-translation
   phases, generates them entirely).

A perf check runs the 1712 P2 microbenchmark suite before/after
the cutover. The acceptance band is ±2%; a wider gap means the
generated dispatch path is missing an inlining opportunity that
the hand-rolled version had, and that's a generator fix.

| Step | Status | Commit |
|------|--------|--------|
| `gopy_tier2_generator.py` lands | TODO | - |
| `vm/eval_uops_gen.go` covers every uop currently hand-rolled | TODO | - |
| Shared-body parity test (tier-1 LOAD_FAST ≡ tier-2 _LOAD_FAST) | TODO | - |
| Remaining ~270 uops emitted; tier-2 trace coverage on micro-bench corpus jumps | TODO | - |
| 1712 microbench ±2% before/after | TODO | - |

## Phase 8 — body translation pilot

CPython's opcode bodies are C, but they use a tightly constrained
subset of C. A first pass through every body in `bytecodes.c`
shows the subset is:

- Local variable declarations + assignments
- `if` / `else if` / `else`
- `while` (rare; mostly for stack juggling helpers)
- `switch` (extremely rare; mostly oparg-dispatch in CALL family)
- Calls to a fixed set of runtime helpers (`PyObject_GetAttr`,
  `_PyLong_Add`, etc) each of which gopy has a Go-side equivalent
- The DSL-specific macros listed in Phase 1
- `goto error` / `goto exit_unwind` (handled by harness)

What does *not* appear: pointer arithmetic, manual struct casts,
`#ifdef` (a few of these exist, gated on debug builds; we ignore
them), inline assembly, `setjmp`/`longjmp`.

The pilot translator handles the subset above for 10 opcodes:

| Opcode | Why this one |
|--------|--------------|
| `NOP` | Empty body. Smoke test. |
| `POP_TOP` | One-line stack effect. |
| `LOAD_FAST` | Local-variable read. |
| `LOAD_FAST_BORROW` | Same but borrow-flavored stack ref. |
| `STORE_FAST` | Local-variable write. |
| `LOAD_CONST` | Constant table read. |
| `RETURN_VALUE` | Frame exit; tests the harness's exit path. |
| `RESUME_CHECK` | Tests `DEOPT_IF` translation. |
| `END_FOR` | Tests stack-shrink + jump. |
| `POP_TOP_LOAD_CONST_INLINE_BORROW` | Tests `macro()` composition. |

Each translated body lands as a generator output; the hand-written
Go body is deleted. CI must stay green at the end of each opcode's
migration.

The subset is documented in
`tools/cases_generator/SUBSET.md`. Anything outside it is a
"keep hand-rolled" escape hatch with a single-line justification.

| Step | Status | Commit |
|------|--------|--------|
| `tools/cases_generator/body_translator.py` covers the Phase 8 subset | TODO | - |
| 10 opcode bodies translated; hand-written deleted; CI green per-opcode | TODO | - |
| `SUBSET.md` describes covered + uncovered constructs | TODO | - |

## Phase 9 — body translation scale-up

Run the translator against every opcode. Expectations after
Phase 9:

- ~270 of ~285 uops fully generated (body + harness).
- ~110 of ~140 tier-1 opcodes fully generated.
- ~30 opcodes flagged as "manual body required" with a comment
  citing the construct (typically: a CPython helper gopy spells
  differently, or a CPython-specific refcount idiom).
- Reproducibility gate green: `tools/regen-cases.sh` produces no
  diff against the committed `*_gen.go` files.

| Step | Status | Commit |
|------|--------|--------|
| All remaining bodies translated or explicitly opted out | TODO | - |
| Hand-written body count below 30 with per-opcode justification | TODO | - |
| Reproducibility gate `tools/cases_generator_reproducibility_test.go` green in CI | TODO | - |

## Risks and carve-outs

**Body translation is the hard part.** Phase 8 is the de-risking
step: if the translator subset turns out to be too narrow, the
spec lands at Phase 7 (harness only, hand-written bodies) and
Phase 8/9 become a follow-on spec. Phase 7 still removes the
entire dispatch / cache-layout / family / deopt class of bugs;
that is the load-bearing win.

**CPython version pinning.** The generator runs against one
CPython version's `bytecodes.c`. Bumping CPython (spec 1707)
becomes a more involved process: vendor the new inputs, run the
generator, fix the diffs in any opcode bodies the translator
flagged as manual. The win is that the diff is now mechanical:
look at the DSL declarations, look at the generated harness diff,
fix the bodies. Today's CPython bumps require manually walking
every opcode for cache-layout and stack-effect changes; this is
much worse.

**Python at build time.** The generator is Python. Builds need a
working `python3.14`. CI already has one (spec 1700 / regrtest
gate); developer machines need it. We document it in
`tools/cases_generator/README.md`. The generator runs only on
explicit `tools/regen-cases.sh` invocation; ordinary `go build`
does not invoke it. Generated files are checked in.

**Two-source-of-truth windows.** During Phases 2-6 each emitter
ships with a parity test against the hand-rolled file it
replaces. The hand-rolled file is deleted only when the parity
test is green. There is no window where two files claim to
own the same table; the parity test mediates the switchover.

**Debuggability of generated Go.** Generated dispatch code is
harder to step through than hand-written Go. Mitigation:
`//line` directives in the generator output point each emitted
line back to `Python/bytecodes.c`, so a panic stack trace lands
the developer at the DSL declaration, not the generated Go. This
mirrors what CPython does for `generated_cases.c.h` against
`bytecodes.c`.

**Performance regressions in dispatch.** Generated Go may pessimize
inlining vs the hand-written switch. The Phase 7 microbench check
catches this; if it fails, fix is generator-side (emit different
shapes) not body-side. The hand-written baseline is preserved in
git for A/B comparison until Phase 9 closes.

## Deprecated source files

Every file below carries a `// DEPRECATED (spec 1714): ...` banner
the day this spec lands. The banner names the phase that deletes
the file and the generated file that replaces it. Editing a
deprecated file is strongly discouraged once the replacing phase
is in flight: any change there has to be reflected in the
generator output too, and the deletion sweep at end-of-phase
rolls back manual edits anyway.

A "fully deleted" entry means the file disappears from the tree.
A "shrinks to" entry means the file survives but its hand-rolled
sections (cache writes, family literals, dispatch switches) are
removed; what remains is glue too small to be worth generating
(typically: a specializer policy function or a frame-setup
helper).

| File | Status today | Replaced by | Phase | Disposition |
|------|--------------|-------------|-------|-------------|
| `compile/opcodes_gen.go` | Hand-curated despite the `_gen` suffix | `compile/opcode_ids_gen.go` | 2 | Fully deleted |
| `compile/opcode_caches.go` | Hand-rolled cache-size table | `compile/opcode_metadata_gen.go` | 2 | Fully deleted |
| `specialize/cache.go` | `SetCacheCell` / `CacheCell` raw codeunit access | `specialize/cache_layouts_gen.go` typed accessors | 3 | Fully deleted (no callers post-migration) |
| `specialize/quicken.go` | Hand-rolled family literal | `specialize/family_gen.go` | 4 | Shrinks to policy helpers |
| `specialize/deopt.go` | Hand-rolled deopt map | `specialize/family_gen.go` | 4 | Shrinks to deopt-action helpers |
| `specialize/binary_op.go` | Hand-rolled specializer; raw cache writes | typed cache access (Phase 3) + generated metadata (Phase 4) | 3+4 | Shrinks to specialize policy |
| `specialize/call.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/call_kw.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/compare_op.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/contains_op.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/for_iter.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/load_attr.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/load_global.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/load_super_attr.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/send.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/store_attr.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/store_subscr.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/to_bool.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `specialize/unpack_sequence.go` | Same | Same | 3+4 | Shrinks to specialize policy |
| `vm/eval_simple.go` | Hand-rolled tier-1 dispatch switch | `vm/eval_dispatch_gen.go` | 5 | Shrinks to evalLoop scaffolding |
| `vm/eval_call.go` | Hand-rolled CALL/CALL_KW family bodies | `vm/eval_dispatch_gen.go` + per-opcode `op<NAME>` bodies | 5+6 | Shrinks to body helpers |
| `vm/eval_resume.go` | Hand-rolled RESUME / RESUME_CHECK | generator output | 5+6 | Shrinks to body helpers |
| `vm/eval_match.go` | Hand-rolled MATCH_* family | generator output | 5 | Shrinks to body helpers |
| `vm/eval_import.go` | Hand-rolled IMPORT_NAME / IMPORT_FROM | generator output | 5 | Shrinks to body helpers |
| `vm/eval_unwind.go` | Hand-rolled error-path label dispatch | generator output (error labels) | 5 | Shrinks to error helpers |
| `vm/eval_gen.go` | Hand-rolled generator-related opcode bodies (SEND, YIELD_VALUE, etc) | generator output | 5+6 | Shrinks to body helpers |
| `vm/eval_specialized.go` | Hand-rolled dispatch for specialized arms | `vm/eval_dispatch_gen.go` | 6 | Fully deleted |
| `vm/eval_specialized_binary_op.go` | Hand-rolled BINARY_OP_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/eval_specialized_compare.go` | Hand-rolled COMPARE_OP_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/eval_specialized_contains.go` | Hand-rolled CONTAINS_OP_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/eval_specialized_load_global.go` | Hand-rolled LOAD_GLOBAL_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/eval_specialized_store_attr.go` | Hand-rolled STORE_ATTR_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/eval_specialized_store_subscr.go` | Hand-rolled STORE_SUBSCR_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/eval_specialized_tobool.go` | Hand-rolled TO_BOOL_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/eval_specialized_unpack.go` | Hand-rolled UNPACK_SEQUENCE_* arms | generator + per-opcode bodies | 6 | Shrinks to body helpers |
| `vm/tier2.go` | Hand-rolled tier-2 trace dispatcher | `vm/eval_uops_gen.go` | 7 | Shrinks to trace-loop scaffolding |
| `optimizer/uops.go` | Hand-rolled uop definitions | `optimizer/uop_meta_gen.go` (regenerated) | 7 | Fully deleted |
| `optimizer/uops_impl.go` | Hand-rolled uop body implementations | `vm/eval_uops_gen.go` | 7+8+9 | Fully deleted |
| `optimizer/uops_dispatch_gen.go` | Generated by a hand-rolled script | `vm/eval_uops_gen.go` | 7 | Fully deleted |
| `optimizer/uops_stubs_gen.go` | Generated by a hand-rolled script | generator output | 7 | Fully deleted |
| `optimizer/uop_ids_gen.go` | Generated by a hand-rolled script | `optimizer/uop_ids_gen.go` (regenerated through cases_generator) | 7 | File survives, content fully regenerated |
| `optimizer/uop_meta_gen.go` | Same | Same | 7 | File survives, content fully regenerated |
| `optimizer/analysis.go` | Hand-rolled abstract interpreter cases | `compile/optimizer_cases_gen.go` | 7 | Shrinks to analysis-driver scaffolding |

## Out of scope

- **Computed-goto dispatch (`target_generator.py` analogue).** Go
  does not support computed goto. The dispatch switch is the
  best we can do; CPython's `opcode_targets.h` has no gopy
  analogue. This is a documented carve-out, not a TODO.
- **Python-side metadata (`py_metadata_generator.py` →
  `Lib/_opcode_metadata.py`).** gopy already vendors that file
  via 1710 T5.1. We ship CPython's directly; no regeneration.
- **PEP 659 specializer skeletons.** Each `specialize/<family>.go`
  has a hand-written specialize/unspecialize policy that decides
  *when* to upgrade an opcode. The decision logic stays
  hand-written; only the cache layout and dispatch harness
  become generated. CPython's generator does not emit the policy
  either; it emits the *plumbing* the policy uses.

## Checklist

- [x] Phase 0.1 — vendor `Tools/cases_generator/` under `Tools/cases_generator/`
- [x] Phase 0.2 — mirror `bytecodes.c`, `optimizer_bytecodes.c`, `pycore_code.h` under `Tools/cases_generator/inputs/`
- [x] Phase 0.3 — `Tools/regen-cases/` (Go driver) invokes upstream generators into a scratch dir
- [x] Phase 0.4 — `go run ./Tools/regen-cases --check-upstream` diff-clean vs CPython 3.14.5
- [ ] Phase 0.5 — CI job `cases-generator-upstream-parity` green
- [x] Phase 1.1 — `gowriter.py` mirrors `cwriter.py` API for Go output
- [x] Phase 1.2 — `go_generators_common.py` binds the constant DSL macros to Go
- [ ] Phase 1.3 — 30-snippet golden corpus under `Tools/cases_generator/testdata/snippets/` (20/30 landed; remaining 10 stage with Phase 5 op signatures)
- [x] Phase 1.4 — `TestSnippetParity` green
- [x] Phase 2.1 — `gopy_opcode_id_generator.py` emits `compile/opcode_ids_gen.go`
- [x] Phase 2.2 — `gopy_opcode_metadata_generator.py` emits `compile/opcode_metadata_gen.go`
- [x] Phase 2.3 — parity test vs `compile/opcodes_gen.go` + `compile/opcode_caches.go`
- [ ] Phase 2.4 — delete `compile/opcode_caches.go`; redirect references
- [x] Phase 3.1 — `cache_struct_parser.py` parses `pycore_code.h` struct definitions
- [x] Phase 3.2 — `specialize/cache_layouts_gen.go` covers every `_Py<Op>Cache`
- [ ] Phase 3.3 — migrate `specialize/load_global.go` + `vm/eval_specialized_load_global.go` to typed accessors
- [ ] Phase 3.4 — migrate every other `specialize/*.go` + `vm/eval_specialized_*.go`
- [ ] Phase 3.5 — delete `specialize.SetCacheCell` / `CacheCell` (no callers)
- [ ] Phase 3.6 — `TestCacheLayoutSize` green
- [ ] Phase 4.1 — generator emits `specialize/family_gen.go` (family + deopt tables)
- [ ] Phase 4.2 — `specialize/quicken.go` + `specialize/deopt.go` consume the generated tables
- [ ] Phase 4.3 — parity test green; literal tables deleted
- [x] Phase 5.1 — tier-1 emitter (Go-side `Tools/bytecodes_gen` in lieu of `gopy_tier1_generator.py`) emits `vm/eval_dispatch_gen.go` for unspecialized opcodes (107 arms, bodies stubbed pending Phase 8 action translator)
- [ ] Phase 5.2 — every opcode body in `vm/eval_simple.go` migrated to a typed `op<NAME>` function (NOP, POP_TOP routed through `dispatchGen` via the `dispatchGenSupported` whitelist)
- [ ] Phase 5.3 — `vm/eval_simple.go` shrinks to evalLoop scaffolding only
- [ ] Phase 5.4 — `go test ./vm` green
- [ ] Phase 6.1 — specialized cases emitted in `vm/eval_dispatch_gen.go`
- [ ] Phase 6.2 — each `vm/eval_specialized_*.go` shrinks to body functions
- [ ] Phase 6.3 — LOAD_GLOBAL cache-boundary regression test
- [ ] Phase 6.4 — per-family boundary tests for ~10 other specialized families
- [ ] Phase 7.1 — `gopy_tier2_generator.py` emits `vm/eval_uops_gen.go`
- [ ] Phase 7.2 — shared-body parity test (tier-1 LOAD_FAST ≡ tier-2 _LOAD_FAST)
- [ ] Phase 7.3 — remaining ~270 uops emitted
- [ ] Phase 7.4 — 1712 microbench ±2% before/after
- [ ] Phase 8.1 — `body_translator.py` covers the Phase 8 subset
- [ ] Phase 8.2 — 10 pilot opcodes have zero hand-written Go body
- [ ] Phase 8.3 — `SUBSET.md` lists covered + uncovered constructs
- [ ] Phase 9.1 — every remaining body translated or explicitly opted out
- [ ] Phase 9.2 — hand-written body count below 30 with per-opcode justification
- [ ] Phase 9.3 — `test/gate/cases_generator_reproducibility_test.go` runs `tools/regen-cases.sh && git diff --exit-code` in CI
- [ ] Spec 1714 final gate — generator reproducibility green; spec 1713 resumed
