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

Same rule as 1704 / 1705 / 1708 / 1712. Port full subsystems, file
by file. No partial slices, no name-only shims, no "tweak the
output until the bytes match for this one fixture". Every CPython
source file the spec touches lands in gopy with a 1:1 function
list and `// CPython:` citations carrying file + line. The cost of
a half-ported subsystem here is paid back many times over: every
divergence on the byte-equality gate becomes a forensic dig
through marshal output, and that is exactly the work the spec is
built to make easy.

This spec **pauses spec 1712**. Performance-parity work resumes
once 1713 lands, because every remaining 1712 row (specializer
emission audits, cache-persistence via marshal, P1.5) depends on a
trustworthy compile pipeline. If the .pyc gopy writes today does
not round-trip through CPython byte for byte, the specializer's
inline-cache slots, exception table, location table, and
co_consts ordering are all suspect.

## Goal

```
python3.14 -m py_compile foo.py
gopy        -m py_compile foo.py
cmp __pycache__/foo.cpython-314.pyc __pycache__/foo.cpython-314.gopy.pyc
# exit 0 for every foo.py in Lib/ and Lib/test/
```

Concretely, the deliverable is a gate (`test/gate/pyc_parity_test.go`)
that walks the vendored CPython corpus under `test/cpython/Lib/`,
compiles each file with both interpreters, and asserts byte-equal
.pyc output. Spec done = gate green across the full corpus.

Two intermediate gates land before the final byte-equality gate,
because byte-equality is the *worst* signal to debug from. In
order:

1. **Disassembly-stream parity** — `compile.Disassemble(co)` for
   every fixture matches `python3.14 -m dis` byte for byte. Any
   diff here is a codegen or flowgraph bug, named in human terms
   (op name, oparg, jump target, line number). This is the gate
   we land first.
2. **Code-object field parity** — once disassembly matches, every
   field on `objects.Code` (co_consts ordering and types, co_names
   ordering, co_varnames, co_freevars, co_cellvars, co_flags,
   co_stacksize, co_firstlineno, co_linetable, co_exceptiontable,
   co_qualname) matches CPython's for the same fixture. This
   catches the bugs the disassembler smooths over (intern order,
   tuple vs list, hidden None at index 0).
3. **.pyc byte-equality** — marshal + importlib header writer
   produces identical bytes. By construction, this gate cannot
   fail unless the marshal layer itself diverges, because both
   inputs are now byte-equal code objects.

## Why this spec exists

Spec 1712 hit the wall it was always going to hit: porting
specializer fast paths is straightforward, but every arm reads
the inline cache the compiler emitted, and any disagreement
between gopy's cache layout and CPython's burns hours per arm.
The same is true of unwind (1708): the assembler emits one PEP
657 entry per instruction in CPython but coalesces in gopy, and
the symptom only surfaced when traceback rendering broke.

Both gaps are downstream of the same root cause: gopy's compile
pipeline produces code objects that *behave* like CPython's most
of the time but are not byte-identical. The right fix is not a
shim per symptom. The right fix is to drive the compile pipeline
to byte-equality and keep it there with a gate.

Once .pyc files round-trip, the entire performance-parity stack
gets a free property: marshal can ship the specializer's cache
layout to disk, and importlib can read it back. Spec 1712 P1.5
folds into this spec.

## Current state (2026-05-16)

A quick audit before drafting:

| Subsystem | gopy file(s) | CPython source | Status |
|-----------|--------------|----------------|--------|
| .pyc header | `marshal/pyc.go` | `Lib/importlib/_bootstrap_external.py:224` | **MAGIC mismatch**: gopy writes 3620, CPython 3.14 writes 3627. Hard fail on byte one. |
| marshal | `marshal/marshal.go`, `marshal/code.go`, `marshal/long.go` | `Python/marshal.c` | needs 1:1 audit. Unknown which type tags / TYPE_REF flow paths gopy emits vs CPython. |
| code object | `objects/code.go` | `Objects/codeobject.c` | needs field-by-field audit against `_PyCode_New`. |
| assemble | `compile/assemble*.go` | `Python/assemble.c` | 1708 closed location-emission. Still need a full audit for co_consts / co_names ordering, stacksize, exception-table emission. |
| instruction seq | `compile/instrseq.go` | `Python/instruction_sequence.c` | 1708 audit pass landed. Re-audit for byte-equal output (instruction order, jump fixup order). |
| flowgraph | `compile/flowgraph*.go` | `Python/flowgraph.c` | unported passes (peepholer pre-passes, redundant-NOP removal, dead-code elim variants) are suspect. |
| codegen | `compile/codegen*.go` + `compile/compiler.go` | `Python/codegen.c` + `Python/compile.c` | covers most of the language. Edge cases (PEP 695 type aliases, match guards, async comprehensions) need a fixture-driven audit. |
| disassembler | `compile/dis.go` | `Lib/dis.py` (`_disassemble_recursive`) | gopy's is hand-written. Needs alignment with CPython's exact format so the disassembly-parity gate is mechanical. |
| importlib pyc writer | `imp/...` (TBD) | `Lib/importlib/_bootstrap_external.py:_code_to_timestamp_pyc` etc | unknown coverage. Audit needed. |

## Files in scope

Sources of truth live under `/Users/apple/cpython-314/`. Every
file below is ported in full, with `// CPython: <file>:<line>
<function>` citations.

| # | CPython file | gopy target | Why |
|---|--------------|-------------|-----|
| A | `Python/marshal.c` | `marshal/marshal.go`, `marshal/code.go`, `marshal/long.go` | The bytes the gate compares. Every TYPE_* tag, every TYPE_REF reuse decision, every short-vs-long-int encoding must match. |
| B | `Lib/importlib/_bootstrap_external.py` (pyc reader / writer slice: `_code_to_timestamp_pyc`, `_code_to_hash_pyc`, `_classify_pyc`, `MAGIC_NUMBER`) | `marshal/pyc.go`, `imp/...` | The 16-byte header is part of the gate; magic must update with CPython. |
| C | `Include/internal/pycore_magic_number.h` | `marshal/pyc.go` `MagicNumber` constant | The single source of the version bump. |
| D | `Objects/codeobject.c` (`_PyCode_New`, `intern_strings`, `intern_constants`, `_PyCode_ConstantKey`) | `objects/code.go`, `objects/code_intern.go` (new if needed) | co_consts / co_names ordering is set here, and it's load-bearing for marshal output. |
| E | `Python/compile.c` (`compute_code_flags`, `dict_keys_inorder`, `consts_dict_keys_inorder`, `compiler_codegen` epilogue, `optimize_and_assemble`) | `compile/compiler.go` | Final stage before the assembler. Decides the iteration order of co_names / co_varnames that marshal then writes. |
| F | `Python/codegen.c` | `compile/codegen*.go` | Already mostly ported; audit pass to close any remaining surprise emit orders (e.g. annotation handling, type-param scope, walrus). |
| G | `Python/flowgraph.c` | `compile/flowgraph*.go` | Optimization passes change the instruction stream. Any pass missing in gopy = bytes diverge. Audit pass list 1:1. |
| H | `Python/instruction_sequence.c` | `compile/instrseq.go` | Already audited for 1708; re-confirm for byte-equality (jump fixup order, label resolution order). |
| I | `Python/assemble.c` | `compile/assemble*.go` | 1708 covered location emission. Audit the rest (exception table, co_lnotab fallback, stacksize compute). |
| J | `Lib/dis.py` (`_disassemble_recursive`, `_get_instructions_bytes`, `Instruction.__str__`) | `compile/dis.go` | The first gate compares output to `python3.14 -m dis`. The format must match CPython's, or every diff is noise. |

## Phase index

Each phase ports one block end to end. Status lives on the
Checklist at the bottom of this spec, mirrored per row here.

| Phase | Block | Gate | Status |
|-------|-------|------|--------|
| 1 | Magic bump + marshal audit prep. Update `MagicNumber` to 3627. Add `marshal/parity_test.go` round-trip: marshal → unmarshal in gopy gives back an equal Code. | round-trip | done (5dbfac9) |
| 2 | Disassembly format alignment. Port `Lib/dis.py` 1:1 into `compile/dis.go`. Output must match `python3.14 -m dis` for a 30-fixture corpus. | dis-stream parity gate | TODO |
| 3 | Codegen / flowgraph audit. Walk every CPython codegen.c + flowgraph.c function; ensure gopy has a 1:1 port with citations. Fix every divergence the dis-stream gate surfaces. | dis-stream parity green on full `test/cpython/Lib/` corpus | TODO |
| 4 | Code-object field audit. Port `_PyCode_New`, `intern_strings`, `intern_constants`, `_PyCode_ConstantKey` from `Objects/codeobject.c` and the `*_inorder` helpers from `Python/compile.c`. Gate: every field on `objects.Code` matches CPython's for the corpus. | field-parity gate | TODO |
| 5 | Assemble audit. 1:1 against `Python/assemble.c` (location emission is already done from 1708; this is exception table, stacksize, co_lnotab fallback). | field-parity green on full corpus | TODO |
| 6 | Marshal port. 1:1 against `Python/marshal.c`. Every TYPE_* tag, the TYPE_REF reuse table, short/long int encoding, float encoding (binary vs string), interned-string handling. | byte-equality gate green on a 30-fixture corpus | TODO |
| 7 | pyc header + importlib writer. Port the relevant slice of `_bootstrap_external.py` for the writer side. | byte-equality gate green on full `test/cpython/Lib/` corpus | TODO |
| Gate | Byte-equality across the full vendored CPython corpus. | gate green | TODO |

## Phase 1 — magic + marshal round-trip

Two changes, both small, both load-bearing. First, `marshal/pyc.go`
bumps `MagicNumber` from 3620 to 3627 to match
`Include/internal/pycore_magic_number.h:295`, with a citation on
the constant so future bumps don't drift. Second,
`marshal/parity_test.go` round-trips a curated fixture through
Dump/Load and asserts `reflect.DeepEqual` on every observable
field of `objects.Code` (with a `(*big.Int).Cmp` shim for the
big-int slot). The fixture covers short and long ints, floats,
strings with duplicates for TYPE_REF reuse, bools, None, plus the
Linetable and ExceptionTable blobs. Phase 1 is the safety net:
every later phase reuses this round-trip to catch regressions
within gopy before any cross-interpreter compare.

| Step | Status | Commit |
|------|--------|--------|
| `MagicNumber` bumped 3620 → 3627 with CPython citation | done | 5dbfac9 |
| `marshal/parity_test.go` round-trip fixture green | done | 5dbfac9 |

## Phase 2 — disassembly-stream parity

The gate runs `gopy -m dis foo.py` and `python3.14 -m dis foo.py`
for every fixture in `test/gate/disdata/` and diffs the two
streams. Initial corpus is 30 small fixtures (one per language
feature: defs, classes, generators, async, comprehensions, match,
exception groups, PEP 695, walrus, f-strings, etc); once each is
green the gate scales to the full vendored corpus. This gate runs
*first* because every diff is human-readable. A missing peephole
pass surfaces as "CPython dropped the redundant JUMP, gopy
didn't"; an interning order bug surfaces as "co_consts[3] is 'foo'
in CPython, () in gopy"; a flowgraph bug surfaces as "block
ordering swapped, jump targets differ by N bytes". None of that is
visible in marshal output.

The original draft framed this phase as "port `Lib/dis.py` 1:1 into
`compile/dis.go`". That was wrong: `stdlib/dis.py` already vendors
all 1157 lines of `Lib/dis.py`. The full-file rule says *vendor*
CPython sources, not rewrite them in Go. The actual port work to
make `gopy -m dis foo.py` produce CPython-byte-equal output is to
vendor `Lib/runpy.py`, wire `cmd/gopy -m` to call
`runpy._run_module_as_main` (exactly what
`Modules/main.c:pymain_run_module` does in CPython), and then fix
every VM / compile bug the dis-stream gate surfaces when the gopy
interpreter runs the vendored dis.py against itself. The Go-side
`compile/dis.go` stays as a debug helper for Go callers; the gate
runs the Python module.

| Step | Status | Commit |
|------|--------|--------|
| Vendor `Lib/runpy.py` 1:1 into `stdlib/runpy.py` | done | 554dfca |
| Wire `cmd/gopy -m mod` to call `runpy._run_module_as_main(mod)` | done | 554dfca |
| Expose Python-facing `co_code` / `co_consts` / `co_names` / `co_varnames` / `co_*` attrs on `objects.Code` so `hasattr(co, 'co_code')` succeeds | done | 08b0d9d |
| Plumb builtin `compile(src, fn, mode)` through the parser + compiler (`_try_compile` in `Lib/dis.py:80` calls this) | done | 83fc368 |
| Fix `importlib.util` attribute access path so `importlib.util.find_spec` resolves | done | d009fd9 |
| Port `codegen_enter_scope` RESUME prologue for module / eval / interactive scopes | done | (current branch) |
| Port `propagate_line_numbers` flowgraph pass so NO_LOCATION instructions inherit prior valid location | done | (current branch) |
| Port `maybe_instr_make_load_smallint` rewrite so 0..255 int constants emit `LOAD_SMALL_INT` | done | (current branch) |
| Port `remove_unused_consts` from `Python/flowgraph.c:3174` so orphaned const slots vanish from `co_consts` after small-int promotion | done | f679821 |
| Port `optimize_basic_block` peephole pairs from `Python/flowgraph.c:2449` (COMPARE_OP/CONTAINS_OP/IS_OP fused with TO_BOOL/UNARY_NOT, TO_BOOL+TO_BOOL, UNARY_NOT+TO_BOOL) onto the flat sequence with a jump-target pin set; also fix `visitBoolOp` to emit the post-`convert_pseudo_conditional_jumps` COPY + TO_BOOL + POP_JUMP_IF_X form | done | b7c2d26 |
| `gopy -m dis foo.py` runs end to end on a hello-world fixture | done for the seed corpus (empty / string-assign / int-assign / if_simple / and_or) | (current branch) |
| `test/gate/disdata/` corpus (30 small files, one per feature) | 5 of 30 seeded (empty.py, hello.py, assign_int.py, if_simple.py, and_or.py); rest grows as gaps close | (current branch) |
| `test/gate/dis_parity_test.go` green on the 30-fixture corpus | 5 of 30 green | (current branch) |

## Phase 3 — codegen / flowgraph audit

Driven entirely by Phase 2 diffs. For each diff, identify the
CPython function that produced the CPython side and confirm gopy
has a 1:1 port with citations. Add the port if missing; fix the
behavior if present. The expected long tail is flowgraph
optimization passes: gopy has the structural framework
(`compile/flowgraph*.go`) but the audit will find missing passes
(likely candidates: redundant-NOP removal, more aggressive peephole
rewrites, conditional jump inversion when the fall-through is
shorter). Phase 3 ends when the dis-stream gate is green across the
full `test/cpython/Lib/` corpus, not just the 30-fixture starter.

| Step | Status | Commit |
|------|--------|--------|
| 1:1 audit of `Python/codegen.c` against `compile/codegen*.go` (citation pass) | TODO | - |
| 1:1 audit of `Python/flowgraph.c` against `compile/flowgraph*.go` (citation pass) | TODO | - |
| Port every missing flowgraph optimization pass surfaced by Phase 2 diffs | TODO | - |
| Vendor first `test/cpython/Lib/` slice + add `test/gate/dis_parity_lib_test.go` skeleton with per-file known-failing map | done (keyword.py + __future__.py vendored; both currently skip with `task #662` / `task #664` reasons) | (current branch) |
| Dis-stream gate green on full `test/cpython/Lib/` corpus | TODO | - |

## Phase 4 — code-object field audit

With dis-stream parity, the instruction stream matches. What's left
is the *container*. `co_consts`, `co_names`, `co_varnames`,
`co_freevars`, `co_cellvars`, and `co_qualname` are populated by
`_PyCode_New` (`Objects/codeobject.c`) and the `dict_keys_inorder`
/ `consts_dict_keys_inorder` helpers in `Python/compile.c`. Order
matters: marshal writes these as tuples and `TYPE_REF` reuses
earlier positions, so any ordering drift cascades into marshal
output. The field-parity gate compiles each fixture through both
interpreters, writes each field via `repr` plus a stable type-tag,
and diffs.

| Step | Status | Commit |
|------|--------|--------|
| Port `_PyCode_New` 1:1 into `objects/code.go` with citations | TODO | - |
| Port `intern_strings` + `intern_constants` + `_PyCode_ConstantKey` | TODO | - |
| Port `dict_keys_inorder` + `consts_dict_keys_inorder` from `Python/compile.c` | TODO | - |
| `test/gate/field_parity_test.go` green on the 30-fixture corpus | TODO | - |
| Field-parity gate green on full `test/cpython/Lib/` corpus | TODO | - |

## Phase 5 — assemble audit (non-location)

1708 covered location emission. Phase 5 finishes the rest of
`Python/assemble.c`. `assemble_exception_table` is already
structurally ported in `compile/assemble_exceptions.go` but needs
a byte-identical varint emission audit against
`_PyCompile_AssembleExceptionTable`. `compute_code_flags` has to
set every flag (CO_OPTIMIZED, CO_NEWLOCALS, CO_VARARGS,
CO_VARKEYWORDS, CO_NESTED, CO_GENERATOR, CO_COROUTINE,
CO_ITERABLE_COROUTINE, CO_ASYNC_GENERATOR, CO_FUTURE_*) under the
same conditions as CPython. `assemble_emit`'s stacksize computation
must match `_PyCompile_OptimizeAndAssemble`'s output exactly.

| Step | Status | Commit |
|------|--------|--------|
| `assemble_exception_table` byte-identical varint audit | TODO | - |
| `compute_code_flags` covers every CO_* flag under the same conditions as CPython | TODO | - |
| `assemble_emit` stacksize matches `_PyCompile_OptimizeAndAssemble` byte-for-byte | TODO | - |
| Field-parity gate green on full corpus | TODO | - |

## Phase 6 — marshal port

`Python/marshal.c` is one source file, ~1800 lines, very mechanical.
The port is a function-by-function transcription with citations. The
load-bearing risks are: TYPE_REF reuse (CPython tracks a `refs` dict
keyed by identity, not value, so gopy must use the same identity
semantics for the reuse positions to match); short vs long int
encoding (TYPE_INT covers `-2^31..2^31-1`, TYPE_LONG covers the
rest); float encoding (TYPE_BINARY_FLOAT, 8 bytes IEEE 754, is what
CPython 3.14 uses by default; confirm gopy isn't writing the legacy
TYPE_FLOAT ASCII form); and interned vs non-interned strings
(TYPE_INTERNED vs TYPE_UNICODE, decided by the string's
`state.interned` flag that Phase 4 sets via `intern_strings`).

| Step | Status | Commit |
|------|--------|--------|
| 1:1 audit of `marshal/marshal.go` against `Python/marshal.c` writer side | TODO | - |
| 1:1 audit of `marshal/marshal.go` against `Python/marshal.c` reader side | TODO | - |
| TYPE_REF identity-keyed reuse table matches CPython exactly | TODO | - |
| Int / float / string encoding paths confirmed byte-identical | TODO | - |
| Byte-equality gate green on Phase 2's 30-fixture corpus | TODO | - |

## Phase 7 — pyc header + importlib writer

The 16-byte header is small but still part of the gate.
`MAGIC_NUMBER` itself is already covered in Phase 1; Phase 7 ports
the writer / reader slice of `Lib/importlib/_bootstrap_external.py`
that drives the header layout: `_code_to_timestamp_pyc` (default
timestamp-based cache), `_code_to_hash_pyc` (hash-based caches used
by `--check-hash-based-pycs`), and `_classify_pyc` (reader,
needed for round-trip). After Phase 7 the byte-equality gate runs
across the full `test/cpython/Lib/` corpus.

| Step | Status | Commit |
|------|--------|--------|
| Port `_code_to_timestamp_pyc` writer | TODO | - |
| Port `_code_to_hash_pyc` writer | TODO | - |
| Port `_classify_pyc` reader | TODO | - |
| `test/gate/pyc_parity_test.go` green on full `test/cpython/Lib/` corpus | TODO | - |

## Gate

After all seven phases:

1. `test/gate/pyc_parity_test.go` walks every `.py` file under
   `test/cpython/Lib/`, compiles it through both `python3.14 -m
   py_compile` and `gopy -m py_compile`, asserts the two `.pyc`
   files are byte-equal.
2. Re-run `test/gate/disdata` — disassembly parity stays green.
3. Re-run `marshal/parity_test.go` — round-trip parity stays green.
4. Spec 1712 unpaused. Its P1.5 (specializer cache persistence
   via marshal) becomes trivial: the cache slabs marshal through
   the existing `Code.CacheObjects` path and round-trip by
   definition.

## Checklist

- [x] Phase 1: `MagicNumber` bumped to 3627; `marshal/parity_test.go` round-trip green (5dbfac9)
- [ ] Phase 2: `compile/dis.go` 1:1 against `Lib/dis.py`; dis-stream gate green on 30-fixture corpus
- [ ] Phase 3: dis-stream gate green on full `test/cpython/Lib/` corpus; every codegen.c + flowgraph.c function has a 1:1 gopy port with citations
- [ ] Phase 4: `_PyCode_New` + interning helpers ported; field-parity gate green on 30-fixture corpus
- [ ] Phase 5: `Python/assemble.c` audit complete (exception table, code flags, stacksize); field-parity gate green on full corpus
- [ ] Phase 6: `Python/marshal.c` 1:1 ported; byte-equality gate green on 30-fixture corpus
- [ ] Phase 7: importlib pyc writer slice ported; byte-equality gate green on full `test/cpython/Lib/` corpus
- [ ] Gate: full corpus byte-equal; spec 1712 unpaused with P1.5 folded into the marshal round-trip
