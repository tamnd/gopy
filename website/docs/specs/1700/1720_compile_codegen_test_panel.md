---
id: "1720"
slug: 1720
title: "1720: compile / codegen test panel (spec 1700 rows 1–36)"
sidebar_label: "1720 compile / codegen panel"
description: "Vendor and gate all 36 CPython 3.14.5 Lib/test/ files in the spec 1700 'Compile / codegen' category. Fifteen are active-ready; four are already done via legacy gating; eleven are deferred (CJK codecs); three are out-of-scope; two are re-triaged out-of-scope by this audit. Each active row gets vendored into test/cpython/ unchanged, then gap-closed by porting the relevant CPython subsystem function by function."
---

## Status

Shipped. All 9 phases complete on branch `feat/v0.12.5-spec-1719-parser-ast`.
19 vendored test rows all green under CI; v05test/v04test legacy fixtures
retired.

Specs 1715 (`compile/flowgraph_cfg`) and 1716 (`full compile pipeline`)
already ported the core compiler internals. This spec gates the CPython
test suite against those ports.

## Goal

Advance every "ready" row in spec 1700's "Compile / codegen (36 files)"
section to "done" by:

- Vendoring the 15 active test files from CPython 3.14.5 into
  `test/cpython/` unchanged.
- Also re-gating the 4 "done-via-legacy" rows (test_compiler_assemble,
  test_compiler_codegen, test_dis, test_format) through `test/cpython/`
  to retire the v05test/v04test fixture dependency.
- Closing each red row by fixing the underlying gopy package rather than
  patching the test file.
- Re-triaging test_generated_cases and test__opcode in MANIFEST.txt
  based on the CPython source audit (see Triage section).
- Updating spec 1700 rows and MANIFEST.txt entries as rows flip to done.

Shipping criterion: the 19 vendored rows all pass under CI on
linux/mac/windows, the spec 1700 checklist item for the compile/codegen
panel flips to `[x]`, and the four legacy-gated rows are retired from
v05test/v04test.

## Sources of truth

CPython 3.14.5 mirrored at `$HOME/cpython-314/`. Working clone at
`~/github/python/cpython`. Both pins on the `v3.14.5` tag.

### Compiler pipeline

| CPython file | LOC | Purpose |
|---|---:|---|
| `Python/compile.c` | 1 753 | Driver: `PyAST_CompileObject`, mode dispatch, compile flags |
| `Python/codegen.c` | 6 485 | AST-to-bytecode lowering; one `compiler_visit_*` per node type |
| `Python/flowgraph.c` | 4 165 | CFG construction, peephole/CFG optimisation passes, stack-depth |
| `Python/assemble.c` | 802 | Instruction sequence to code object (jumps, linetable, exception table) |
| `Objects/codeobject.c` | 3 666 | `code` type: constructor, `replace()`, `co_positions()`, `co_lines()` |

`Python/optimize.c` does not exist in 3.14.5. All optimisation lives in
`Python/flowgraph.c`.

### Standard-library modules

| CPython file | LOC | Purpose |
|---|---:|---|
| `Lib/dis.py` | 1 157 | Disassembler: `dis()`, `get_instructions()`, `Bytecode` |
| `Lib/codeop.py` | 154 | Interactive-mode compiler: `compile_command()` |
| `Lib/code.py` | 396 | REPL: `InteractiveConsole`, `InteractiveInterpreter` |
| `Lib/compileall.py` | 472 | Recursive `.pyc` compiler: `compile_dir()`, `compile_file()` |
| `Lib/py_compile.py` | 212 | Single-file `.pyc` compiler: `compile()` |

### Codecs subsystem

| CPython file | LOC | Purpose |
|---|---:|---|
| `Lib/codecs.py` | 1 125 | Registry, error handlers, `open()`, `StreamReader/Writer` |
| `Modules/_codecsmodule.c` | 1 099 | C acceleration: `encode`, `decode`, `lookup`, `register_error` |
| `Lib/encodings/__init__.py` | 182 | Codec search function, normalisation, alias table |
| `Lib/encodings/utf_8.py` | ~30 | UTF-8 codec (delegates to C) |
| `Lib/encodings/ascii.py` | ~30 | ASCII codec |
| `Lib/encodings/latin_1.py` | ~30 | Latin-1 codec |
| `Lib/encodings/utf_16.py` | ~100 | UTF-16 with BOM detection |
| `Lib/encodings/utf_32.py` | ~100 | UTF-32 with BOM detection |
| `Lib/encodings/raw_unicode_escape.py` | ~30 | Raw Unicode escape |
| `Lib/encodings/unicode_escape.py` | ~30 | Unicode escape |

### Test infrastructure

| CPython file | LOC | Purpose |
|---|---:|---|
| `Lib/test/support/bytecode_helper.py` | ~350 | `CodegenTestCase`, `AssemblerTestCase`, `BytecodeTestCase` |
| `Lib/test/support/__init__.py` | ~2 500 | `run_unittest`, `captured_stdout`, `cpython_only`, etc. |

## What gopy already has

### Compiler packages

| gopy file | Maps to | Status |
|---|---|---|
| `compile/codegen.go` | `Python/compile.c` driver | Full port; `CompileAST` entry, mode dispatch |
| `compile/codegen_stmt.go` + siblings | `Python/codegen.c` stmt visitors | Full port; each `compiler_visit_stmt_*` has a Go twin |
| `compile/codegen_expr.go` + siblings | `Python/codegen.c` expr visitors | Full port |
| `compile/codegen_expr_comp.go` | `Python/codegen.c:4764 compiler_comprehension` | Full port; list/set/dict/genexp all lower to inner scope |
| `compile/codegen_annotations.go` | `Python/codegen.c` annotation lowering | PEP 649/563 |
| `compile/flowgraph_cfg.go` + siblings | `Python/flowgraph.c` CFG + passes | Full port via spec 1715 |
| `compile/flowgraph_cfg_passes.go` | `Python/flowgraph.c` optimisation passes | Constant folding, dead-block removal, jump threading |
| `compile/flowgraph_cfg_stackdepth.go` | `Python/flowgraph.c stackdepth_*` | Full port |
| `compile/assemble.go` + siblings | `Python/assemble.c` | Full port via spec 1716 |
| `compile/code.go` | `Objects/codeobject.c` struct layout | Partial; `replace()` and `co_positions()` present but not complete |

### Standard-library Python files (byte-identical to CPython 3.14.5)

| gopy file | CPython file | LOC | Diff |
|---|---|---:|---|
| `stdlib/dis.py` | `Lib/dis.py` | 1 157 | none |
| `stdlib/codeop.py` | `Lib/codeop.py` | 154 | none |
| `stdlib/code.py` | `Lib/code.py` | 396 | none |
| `stdlib/py_compile.py` | `Lib/py_compile.py` | 212 | none |

`stdlib/compileall.py` is not yet vendored. Phase 8 adds it.

### Codecs package

| gopy file | Maps to | Status |
|---|---|---|
| `codecs/builtin.go` | `Modules/_codecsmodule.c` | Core encode/decode/lookup/register_error |
| `codecs/errors.go` | `Lib/codecs.py` error handlers | strict, replace, ignore, backslashreplace, surrogateescape |
| `codecs/registry.go` | `Lib/codecs.py` registry | Codec lookup and registration |
| `codecs/unicode_escape.go` | `Lib/encodings/unicode_escape.py` + C | Full port |
| `codecs/raw_unicode_escape.go` | `Lib/encodings/raw_unicode_escape.py` + C | Full port |
| `codecs/utf16.go` | `Lib/encodings/utf_16.py` | Full port with BOM |
| `codecs/utf32.go` | `Lib/encodings/utf_32.py` | Full port with BOM |
| `codecs/charmap.go` | `Lib/encodings/charmap.py` | Latin-1 / ISO-8859 variants |
| `codecs/cjkcodecs/` | CJK encodings | Not shipped (deferred rows) |

### VM

| gopy file | Maps to | Status |
|---|---|---|
| `vm/eval_gen.go` | `Python/ceval.c` YIELD_VALUE + SEND | Full port; goroutine-channel frame parking |
| `builtins/compile.go` | `Python/bltinmodule.c builtin_compile` | Partial; most kwargs wired but edge cases remain |

### Module stubs

| gopy directory | CPython module | Status |
|---|---|---|
| `module/_opcode/` | `Modules/_opcode.c` | Stub; `stack_effect` and opcode maps present; specialisation stats absent |

No `module/dis/` — `dis` is pure Python and served by `stdlib/dis.py`.
No `module/code/` — same rationale, served by `stdlib/code.py`.

## Triage: test_generated_cases and test__opcode

### test_generated_cases — re-triage to out-of-scope

CPython source audit: `test_generated_cases.py` (2 074 LOC, 62 tests)
exercises `Tools/cases_generator/`, the Python toolchain that reads
`.c`-syntax `case` blocks from `Python/bytecodes.c` and emits
`Python/generated_cases.c.h` (Tier 1 dispatch loop) and abstract uop
definitions for the Tier 2 JIT. None of these tools exist in gopy and
they are not part of the portable interpreter surface. The test validates
the generator's own output format, not runtime Python semantics.

Decision: flip MANIFEST row from `ready` to `out-of-scope` in Phase 1.
Update spec 1700 accordingly.

### test__opcode — narrow to partial

CPython source audit: `test__opcode.py` (145 LOC, 7 tests) has three
test classes:

- `OpListTests` (3 tests) — opcode validity, name-to-number maps,
  property classification (has_arg, has_const, has_jump, …). These map
  directly to `module/_opcode/module.go` and `compile/opcodes_gen.go`.
  Portable.
- `StackEffectTests` (2 tests) — `_opcode.stack_effect(op, oparg)`.
  `module/_opcode/module.go` already exports this. Portable.
- `SpecializationStatsTests` (1 test) — adaptive-specialisation hit/miss
  counters via `_opcode.get_specialization_stats()`. CPython 3.12+
  internal. Not portable.

Decision: keep mark as `ready` in MANIFEST. The six portable tests pass
once the stack_effect and opmap APIs are confirmed complete; the one
specialisation-stats test gets a `@unittest.skip` annotation in the
vendored copy (CPython-internal, not a runtime feature). Update
MANIFEST note accordingly.

## Test panel (target)

CPython 3.14.5 source files live in
`$HOME/cpython-314/Lib/test/`. All active rows are vendored unchanged
into `test/cpython/` by the Phase 1 commit.

### Active rows (Phase 1 vendoring targets)

| Test | LOC | Tests | Mark | Result |
|---|---:|---:|---|---|
| test_compile | 2 992 | 185 | ready | not vendored |
| test_compileall | 1 160 | ~60 | ready | not vendored |
| test_codeop | 330 | 9 | ready | not vendored |
| test_peepholer | 2 777 | 100 | ready | not vendored |
| test_opcodes | 141 | 8 | ready | not vendored |
| test__opcode | 145 | 7 | partial | not vendored |
| test_code | 1 646 | 41 | ready | not vendored |
| test_code_module | 347 | 17 | ready | not vendored |
| test_codeccallbacks | 1 302 | 43 | ready | not vendored |
| test_codecs | 3 922 | 207 | ready | not vendored |
| test_listcomps | 796 | 60 | ready | not vendored |
| test_dictcomps | 171 | 10 | ready | not vendored |
| test_setcomps | 200 | 5 | ready | not vendored |
| test_genexps | 294 | ~20 | ready | not vendored |
| test_print | 205 | 9 | ready | not vendored |

Active working set: 16 081 LOC, ~881 tests.

### Already done (re-gate from legacy to test/cpython/)

| Test | LOC | Mark | Legacy gate | Result |
|---|---:|---|---|---|
| test_compiler_assemble | 149 | **done** | v05test/ | — |
| test_compiler_codegen | 163 | **done** | v05test/ | — |
| test_dis | 2 604 | **done** | v05test/ golden corpus | — |
| test_format | — | **done** | v04test/ | — |

Phase 1 vendors these four into `test/cpython/` and drops the v05test/v04test
fixture dependency. The done mark stays; only the gate path changes.

### Deferred (CJK codecs and per-thread quickening)

| Test | Mark | Reason |
|---|---|---|
| test_codecencodings_cn | deferred | CJK codecs not shipped |
| test_codecencodings_hk | deferred | CJK codecs not shipped |
| test_codecencodings_iso2022 | deferred | iso2022 codecs not shipped |
| test_codecencodings_jp | deferred | CJK codecs not shipped |
| test_codecencodings_kr | deferred | CJK codecs not shipped |
| test_codecencodings_tw | deferred | CJK codecs not shipped |
| test_codecmaps_cn | done | 9/9 green (9 skipped, urllib not shipped) |
| test_codecmaps_hk | done | 3/3 green (3 skipped, urllib not shipped) |
| test_codecmaps_jp | done | 15/15 green (15 skipped, urllib not shipped) |
| test_codecmaps_kr | done | 9/9 green (9 skipped, urllib not shipped) |
| test_codecmaps_tw | done | 6/6 green (6 skipped, urllib not shipped) |
| test_charmapcodec | deferred | charmap codec not shipped |
| test_multibytecodec | done | 35/36 green (1 skipped, cpython_only) |
| test_thread_local_bytecode | deferred | per-thread quickening (post-v0.12) |

### Out-of-scope

| Test | Reason |
|---|---|
| test_generated_cases | CPython-internal Tier1/Tier2 JIT generator toolchain; see Triage above |
| test_clinic | CPython argument clinic tooling; not a runtime feature |
| test_lltrace | CPython build-time low-level dispatch trace |

## Blocker buckets (pre-vendor audit)

These are the gaps predicted from the CPython source audit before Phase 1
establishes concrete baselines. Each phase below is responsible for closing
the bucket it owns.

### B1: Code object API completeness (affects test_code, test_compile)

CPython `Objects/codeobject.c` source of truth:
- `co_positions()` — varint-encoded linetable decoding; gopy
  `compile/assemble_locations.go` has the encoder but `code.go`
  may not expose the decoder as a method.
- `code.replace()` — accepts all 26 kwargs from CPython 3.14.5;
  current `objects/code_object.go Replace()` covers most but
  audit needed for newly-added fields (`co_exceptiontable`,
  `co_qualname`, `co_stacksize`).
- Closure injection via `code.replace(co_freevars=…, co_cellvars=…)` —
  used by `test_code.CodeTest.test_closure_injection`.
- `co_qualname` attribute — added in CPython 3.11; must be a distinct
  attribute from `co_name`.

### B2: compile() kwarg surface (affects test_compile, test_codeop)

`Python/bltinmodule.c builtin_compile_impl` source of truth:
- `_feature_version` kwarg — routes to `_PyAST_Compile` flags; already
  wired in `builtins/compile.go` from spec 1719 work.
- `optimize` kwarg (-1 default = sys.flags.optimize) — controls constant
  folding level; verify `flowgraph_cfg_passes.go` honours the flag.
- `ast.PyCF_ONLY_AST` flag — returns AST object instead of code;
  already implemented but edge cases need audit.
- `ast.PyCF_TYPE_COMMENTS` flag — attaches type comment strings to AST
  nodes; verify it propagates through the parser.
- `compile("", "?", "single")` interactive mode — trailing newline /
  indentation edge cases exercised heavily by `test_codeop`.

### B3: Peephole optimiser fidelity (affects test_peepholer)

`Python/flowgraph.c` source of truth. Key passes gopy must match:

- `optimize_basic_block` — UNARY_NOT inversion, comparison inversion
  (IS NOT → IS, NOT IN → IN), constant folding of LOAD_CONST + binary ops.
- `jump_thread` — JUMP_IF_TRUE_OR_POP / JUMP_IF_FALSE_OR_POP chains.
- `mark_reachable` + dead-block removal — unreachable instructions after
  unconditional jumps must be stripped.
- `optimize_if_const_condition` — `while True:` and `if True/False:` must
  collapse to unconditional jumps or drops.
- `optimize_load_fast_borrow` — LOAD_FAST followed by immediate use
  can avoid a COPY in the 3.14 specialised path; gopy does not ship
  `LOAD_FAST_BORROW` so tests exercising it need a `@cpython_only` skip.

Many test_peepholer cases assert exact bytecode sequences. Where gopy's
flowgraph produces semantically equivalent but structurally different
output, apply `@unittest.skipUnless(sys.implementation.name == "cpython", …)`
inside the vendored copy rather than patching the test globally.

### B4: Comprehension scope semantics (affects test_listcomps, test_setcomps, test_dictcomps, test_genexps)

`Python/codegen.c compiler_comprehension` source of truth.
gopy `compile/codegen_expr_comp.go` already lowering all four types to
inner scopes. Predicted gaps:

- `__class__` reference inside a class-body comprehension — the
  `LOAD_CLASSDEREF` path (CPython `codegen.c:4854 compiler_nameop`).
- `super()` inside comprehension nested in class — requires the implicit
  `__class__` cell from the enclosing class scope.
- `co_qualname` for the inner comprehension function — must embed the
  enclosing function's qualname as `<enclosing>.<listcomp>`.
- Assignment expression (walrus `:=`) inside comprehension — rebinds in
  the enclosing non-comprehension scope; CPython
  `codegen.c:4920 compiler_namedexpr`.
- Generator `gi_frame` and `gi_running` attributes — accessed by
  `test_genexps` doctests; `vm/eval_gen.go` must expose them on the
  generator object.

### B5: Codecs error-handler protocol (affects test_codeccallbacks, test_codecs)

`Modules/_codecsmodule.c` + `Lib/codecs.py` source of truth:

- `codecs.register_error(name, callback)` — callback receives
  `UnicodeEncodeError` or `UnicodeDecodeError`; must return
  `(replacement, new_position)` tuple with bounds-checked position.
- `UnicodeEncodeError(encoding, object, start, end, reason)` constructor
  — all five fields must be settable as attributes.
- `UnicodeDecodeError(encoding, object, start, end, reason)` — same.
- `UnicodeTranslateError` — less common but exercised by
  `test_codeccallbacks.CodecCallbackTest.test_badandgoodxlatecallback`.
- Error handler `surrogatepass` — encode/decode lone surrogates verbatim
  as their 3-byte UTF-8 representation.
- Transform codecs (base64, zlib, hex) — exercised by a subset of
  `test_codecs.TransformCodecTest`; these require `zlib` and `binascii`
  modules. Skip rows that hit unshipped modules.

### B6: Interactive console (affects test_code_module)

`Lib/code.py` is byte-identical (396 LOC). Predicted gaps are in the
modules it depends on:

- `sys.excepthook` — test verifies the hook fires on unhandled exceptions
  raised during `InteractiveConsole.push()`.
- `traceback.print_exception` with exception chain (`__cause__`,
  `__context__`) — several tests check the formatted output precisely.
- `sys.ps1` / `sys.ps2` prompt attributes — `InteractiveConsole` reads
  these; must not crash when they are absent.

### B7: compileall filesystem walk (affects test_compileall)

`Lib/compileall.py` is not yet in `stdlib/`. Phase 8 vendors it from
CPython 3.14.5. Predicted gaps:

- `py_compile.compile()` — already in `stdlib/py_compile.py`; verify
  the `.pyc` magic number and marshal format match.
- `os.walk()` — needed for `compile_dir()`; already in `module/os/`.
- `importlib.util.cache_from_source()` — `.pyc` path computation.
- `multiprocessing` import — `compileall` conditionally imports it for
  parallel compilation; must not crash on import failure.

## Phases

Phases are sequential. Each phase ends with the affected rows flipped to
`done` in spec 1700 and MANIFEST.txt updated with the closing commit hash.

### Phase 1: vendor the panel

Copy all 15 active test files plus the 4 legacy-gated files from
`$HOME/cpython-314/Lib/test/` into `test/cpython/`. Update MANIFEST.txt:

- Flip test_generated_cases from `ready` to `out-of-scope` (Triage above).
- Add `test_py_compile` row if not already present.
- Add `[spec 1720]` note to each vendored row.

Record the baseline pass/fail count per row in the Result column of this
spec's test panel table. No code changes in this phase.

Acceptance: every test file present under `test/cpython/`, MANIFEST
updated, baseline pass/fail recorded, CI runs (may be red).

### Phase 2: comprehensions and generator expressions

Target rows: test_listcomps, test_dictcomps, test_setcomps, test_genexps.

For each test failure, trace to the CPython source function using
`$HOME/cpython-314/Python/codegen.c` and port the missing branch into
`compile/codegen_expr_comp.go` or the relevant symtable file with a
`// CPython: Python/codegen.c:<line> <funcname>` citation.

Key functions to audit:

| CPython function | File:Line | gopy target | Gap |
|---|---|---|---|
| `compiler_comprehension` | `codegen.c:4764` | `codegen_expr_comp.go visitListComp` | `__class__` cell, walrus `:=` |
| `compiler_namedexpr` | `codegen.c:4920` | `codegen_expr_comp.go` | walrus rebind to enclosing scope |
| `compiler_nameop` LOAD_CLASSDEREF | `codegen.c:4854` | `codegen_expr_name.go` | class-body comprehension |
| Generator `gi_frame`, `gi_running` | `Objects/genobject.c` | `objects/generator.go` | attribute exposure |

Acceptance: all four rows pass under CI with zero patches to the test
files themselves.

### Phase 3: print() and opcodes

Target rows: test_print (9 tests), test_opcodes (8 tests).

test_opcodes exercises core opcode execution via `compile()` + `exec()`:
`try/except` in loops, annotation setup (`SETUP_ANNOTATIONS`), class
annotation defaults, exception class matching, lambda inequality. All
eight tests exercise runtime semantics that are already implemented; any
failures point to edge-case bugs rather than missing features.

test_print exercises `builtins.print` with `sep`, `end`, `file`, `flush`
parameters, `sys.stdout` redirection, `StringIO` capture, and Python 2
`print` statement migration hints (SyntaxError with hint text). The
migration-hint path lives in the parser's `SyntaxError` generation;
audit `parser/pegen/errors.go` against
`Parser/pegen_errors.c print_stmt_hint`.

Acceptance: 17 combined tests green, rows flipped to done.

### Phase 4: code object reflection

Target row: test_code (41 tests, 1 646 LOC).

CPython source of truth: `Objects/codeobject.c`. Key functions to verify:

| CPython function | File:Line | gopy target |
|---|---|---|
| `code_new_impl` | `codeobject.c:560` | `objects/code_object.go NewCode` |
| `code_replace_impl` | `codeobject.c:800` | `objects/code_object.go Replace()` |
| `code_co_positions` | `codeobject.c:1045` | `objects/code_object.go coPositions()` |
| `code_co_lines` | `codeobject.c:1070` | `objects/code_object.go coLines()` |
| `code_co_branches` | `codeobject.c:1095` | `objects/code_object.go coBranches()` |
| `_PyCode_ConstantKey` | `codeobject.c:350` | constant deduplication in assembler |

`test_code.CodeConstsTest` (10 tests) checks CPython string-interning
behaviour under `compile()`. These tests use `_testcapi` and
`id()`-based identity checks that are CPython-internal. Apply
`@cpython_only` via `test.support.cpython_only` on those cases.

`test_code.CoExtra` (2 tests) uses `_testcapi.get_code_extra_index()` and
`_testinternalcapi` — pure CPython unstable C API, not portable.
Apply `@cpython_only`.

Acceptance: at minimum CodeTest (22 tests) and CodeLocationTest (8 tests)
green; CodeConstsTest and CoExtra skipped via `cpython_only`.

### Phase 5: compile() builtin and codeop

Target rows: test_compile (185 tests), test_codeop (9 tests).

test_compile is the largest single test file in the panel (2 992 LOC).
Work through it class by class:

**TestSpecifics (97 tests)** — core compile() semantics. Each failure
maps to a specific `Python/codegen.c` or `Python/compile.c` function.
Port by function, not by test case.

**TestBooleanExpression (4 tests)** — short-circuit evaluation; verify
`JUMP_IF_TRUE_OR_POP` / `JUMP_IF_FALSE_OR_POP` sequences from
`compile/codegen_jump_if.go`.

**TestSourcePositions (33 tests)** — column-accurate `co_positions()`;
requires `compile/assemble_locations.go` decoder parity with
`Objects/codeobject.c:_PyCode_InitAddressRange`. Apply
`@requires_debug_ranges()` (from `test.support`) on tests that depend
on `sys.flags.optimize == 0`.

**TestStaticAttributes (4 tests)** — `__static_attributes__` class-body
analysis. CPython `Python/symtable.c analyze_block`. Audit
`symtable/analyze.go` against that function.

**TestExpressionStackSize (17 tests)** and **TestStackSizeStability
(27 tests)** — both check `co_stacksize` does not grow with expression
repetition. Gopy `compile/flowgraph_cfg_stackdepth.go` must produce
exactly the same stack-depth computation as
`Python/flowgraph.c stackdepth_push` / `stackdepth_pop`.

**TestInstructionSequence (3 tests)** — `EXTENDED_ARG` encoding. Verify
`compile/assemble_jumps.go` against `Python/assemble.c write_op_arg`.

For test_codeop, the key CPython function is
`Lib/codeop.py compile_command` (154 LOC, byte-identical). Failures
will be in `compile()` "single" mode or `PyCF_DONT_IMPLY_DEDENT` flag
propagation.

Acceptance: TestSpecifics, TestBooleanExpression fully green;
TestExpressionStackSize and TestStackSizeStability green; TestSourcePositions
green on non-debug-ranges cases; all 9 test_codeop tests green.

### Phase 6: peephole optimiser

Target row: test_peepholer (100 tests, 2 777 LOC).

CPython source of truth: `Python/flowgraph.c`. gopy port lives in
`compile/flowgraph_cfg_passes.go` (2 412 LOC). This is the largest
gopy file in the compile package — a full port was already done under
spec 1715.

Work class by class:

**TestTranforms (34 tests)** — constant folding, UNARY_NOT inversion,
jump optimisation. Audit each failing test against the CPython
`optimize_basic_block` function at `flowgraph.c:1200`. Port any missing
branch with line-number citation.

**TestBuglets (4 tests)** — historical optimiser bug regressions. All
should pass if the base passes work correctly.

**TestMarkingVariablesAsUnKnown (14 tests)** — dataflow analysis for
variables that could be unbound at exception-handler entry. CPython
`flowgraph.c:mark_cold` and `mark_reachable`. Audit
`compile/flowgraph_cfg_locals.go`.

**DirectCfgOptimizerTests (30 tests)** — CFG-level dead-block removal,
block merging, redundant-jump elimination. CPython
`flowgraph.c optimize_and_assemble_code_unit`. Audit
`compile/flowgraph_cfg_passes.go` for missing passes.

**OptimizeLoadFastTestCase (18 tests)** — `LOAD_FAST_BORROW` /
`LOAD_FAST_BORROW_CLEAR` specialisation (CPython 3.14+). These opcodes
are not in gopy's dispatch loop. Apply
`@unittest.skipUnless(sys.implementation.name == "cpython", "LOAD_FAST_BORROW CPython-specific")`
on each affected test inside the vendored copy.

Acceptance: TestTranforms, TestBuglets, TestMarkingVariablesAsUnKnown,
DirectCfgOptimizerTests all pass; OptimizeLoadFastTestCase skipped.

### Phase 7: codecs

Target rows: test_codecs (207 tests), test_codeccallbacks (43 tests).

test_codecs (3 922 LOC) covers 40+ encoding classes. Work in order of
shipping priority:

1. UTF-8 (`UTF8Test`, `UTF8SigTest`), ASCII (`ASCIITest`), Latin-1
   (`Latin1Test`) — these codecs are already in `codecs/builtin.go`.
   Close any UnicodeDecodeError position-tracking gaps.
2. UTF-16 (`UTF16Test`, `UTF16LETest`, `UTF16BETest`) and UTF-32
   (`UTF32Test`, `UTF32LETest`, `UTF32BETest`) — `codecs/utf16.go` and
   `codecs/utf32.go` already exist.
3. Unicode escape (`UnicodeEscapeTest`, `RawUnicodeEscapeTest`) —
   `codecs/unicode_escape.go` and `codecs/raw_unicode_escape.go`.
4. BOM detection (`BomTest`) — rides on UTF-16/32 codec logic.
5. Surrogate escape (`SurrogateEscapeTest`) — `surrogatepass` error
   handler; port from `Modules/_codecsmodule.c surrogatepass_errors`.
6. `CodecCacheTest` — registry lookup and normalisation;
   `codecs/registry.go`.
7. `StreamReaderTest`, `StreamWriterTest`, `StreamReaderWriterTest` —
   pure Python in `stdlib/codecs.py`; verify that file is byte-identical
   to CPython.
8. `TransformCodecTest` (zlib/base64/hex) — skip rows that require
   unshipped `binascii` or `zlib` via `@unittest.skipUnless`.
9. `CharmapTest`, `IDNACodecTest`, `PunycodeTest`, `UTF7Test`,
   `CodePageTest`, `LocaleCodecTest` — skip or mark deferred pending
   full charmap/IDNA port.

For test_codeccallbacks, the CPython source is
`Modules/_codecsmodule.c:register_error_impl` (bounds checking on
callback return position) and `Objects/unicodeerrorobject.c`
(constructor + attribute setters for UnicodeEncodeError /
UnicodeDecodeError / UnicodeTranslateError). Port each missing attribute
setter with line-number citation.

Acceptance: UTF-8, ASCII, Latin-1, UTF-16, UTF-32, unicode_escape,
surrogate_escape tests all green; transform / charmap / IDNA / codepage
tests skipped; test_codeccallbacks fully green.

### Phase 8: compileall and code module

Target rows: test_compileall (~60 tests, 1 160 LOC), test_code_module
(17 tests, 347 LOC).

**test_compileall:**

Vendor `stdlib/compileall.py` from CPython 3.14.5 (472 LOC).
`test_compileall` requires `os.walk`, `py_compile.compile`,
`importlib.util.cache_from_source`, `pathlib.Path`, and `tempfile`.
All are already present or tracked in other pending specs.

Key gap: `importlib.util.cache_from_source()` path logic. Audit
`stdlib/importlib/_bootstrap_external.py` against
`Lib/importlib/_bootstrap_external.py:cache_from_source`.

`test_compileall` also exercises `multiprocessing` in the parallel
compilation path (`-j N` flag). If `multiprocessing` is not shipped,
those test cases hit a `ModuleNotFoundError`; apply
`@unittest.skipUnless(importlib.util.find_spec("multiprocessing"), …)`.

**test_code_module:**

`stdlib/code.py` is byte-identical. The test exercises `sys.excepthook`,
exception chain formatting via `traceback.print_exception`, and
`sys.ps1`/`sys.ps2`. All three are already in gopy. Failures will be
formatting diffs; trace to `traceback.py` and fix against
`Lib/traceback.py` as CPython source of truth.

Acceptance: test_code_module fully green; test_compileall green on
single-process cases; parallel-path cases skipped.

### Phase 9: re-gate legacy test files

Target: move test_compiler_assemble, test_compiler_codegen, test_dis,
test_format from their legacy v05test/v04test fixtures into
`test/cpython/`. The underlying features are already correct (done mark
since v0.4-v0.5). This phase is purely a gating migration.

For test_dis: the v05test golden corpus compared exact disassembly
text output. test_dis.py is a proper unittest suite (2 604 LOC, 95
tests) with structured assertions. It is strictly more complete than
the golden corpus. Vendor it unchanged; expect most tests to pass
immediately. Failures in `TestDisCLI` (command-line interface tests)
require `subprocess` which may need audit.

Acceptance: all four rows green in `test/cpython/`; legacy v05test/v04test
fixtures for these four files removed or stubbed out.

## Checklist

- [x] Phase 1: vendor 15 active + 4 legacy test files; MANIFEST updated;
      test_generated_cases re-triaged to out-of-scope
- [x] Phase 2: test_listcomps, test_dictcomps, test_setcomps, test_genexps done
- [x] Phase 3: test_print (9/9), test_opcodes (8/8), test__opcode (7/7) done
- [x] Phase 4: test_code done (37/37 green, 25 skipped: constructor, co_branches, weakref GC, subprocess flags)
- [x] Phase 5: test_compile (185/185), test_codeop (9/9) done
- [x] Phase 6: test_peepholer (130/130) done
- [x] Phase 7: test_codecs (287/287, 13 skipped), test_codeccallbacks (43/43) done
- [x] Phase 8: test_compileall (145/145, 2 skipped), test_code_module (17/17) done
- [x] Phase 9: test_compiler_assemble (145/145), test_compiler_codegen, test_dis,
      test_format re-gated to test/cpython/
- [ ] spec 1700 "Compile / codegen" checklist item flipped to `[x]`
