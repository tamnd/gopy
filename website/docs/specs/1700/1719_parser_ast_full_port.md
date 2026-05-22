---
id: "1719"
slug: 1719
title: "1719: parser / grammar / AST full port (panel #476)"
sidebar_label: "1719 parser / AST"
description: "Move the v0.10.2 parser panel (27 top-level test_*.py + test_ast/, test_future_stmt/, test_peg_generator/) from `ready` to `done` by porting CPython 3.14.5 Parser/, Python/ast.c, Python/ast_unparse.c, Parser/Python.asdl, and Lib/ast.py one file at a time and pinning the gate runs in test/cpython/."
---

## Status

Active. Branch `feat/v0.12.5-spec-1719-parser-ast`. PR #78. Spec scaffold
landed; vendoring + gap closers proceed under follow-up commits.

Spec 1710 + 1718 closed the lexer panel (PR #76, squash `bd1e9cce`),
which clears the five `Lexer / tokenizer` rows in spec 1700. The next
panel up is `Parser / grammar / AST`. 27 top-level test files plus the
three `test_ast/`, `test_future_stmt/`, `test_peg_generator/` packages
share the same parser + ast subsystem, so they go through together.

## Goal

Make the entire spec 1700 `Parser / grammar / AST` panel pass under
`gopy test/cpython/test_<name>.py`. Concretely:

- Vendor all 27 + 3 test files from CPython 3.14.5 into `test/cpython/`
  unchanged (the same protocol used for the lexer panel).
- Port every supporting file from CPython 1:1 with `// CPython:`
  citations at function granularity.
- Close each red row by fixing the underlying gopy package rather than
  patching the test.
- Flip each row in spec 1700 from `ready` to `done` with the
  closing commit hash.

The shipping criterion mirrors spec 1718: panel rows go green under CI
on linux/mac/windows for the entire matrix, and the spec 1700 checklist
item `v0.10.2 parser panel (#476)` flips to `[x]`.

## Sources of truth

CPython 3.14.5 mirrored at `/Users/apple/cpython-314/`. Working clone
at `~/github/python/cpython`. Keep both on the same `v3.14.5` tag.

### Parser/ (non-lexer, non-tokenizer)

| CPython file | LOC | Purpose |
|-------------|----:|---------|
| `Parser/Python.asdl` | 154 | ASDL grammar definition (source of truth for every node type) |
| `Parser/parser.c` | 38073 | Generated PEG parser (built from `python.gram`, do not edit by hand) |
| `Parser/pegen.c` | 1083 | PEG runtime: memoization, token feeding, rule invocation |
| `Parser/pegen.h` | 388 | PEG runtime header (`Parser_t`, rule types) |
| `Parser/peg_api.c` | 39 | Public entry `_PyPegen_run_parser` + friends |
| `Parser/action_helpers.c` | 1953 | AST-build helpers called from generated rule actions |
| `Parser/pegen_errors.c` | 462 | SyntaxError message construction at parse time |
| `Parser/string_parser.c` | 339 | F-string / triple-quoted string parser (call sites in pegen) |
| `Parser/string_parser.h` | 11 | String parser header |
| `Parser/token.c` | 250 | Token type names + metadata |
| `Parser/myreadline.c` | 437 | Interactive readline (out of scope for this spec) |

### Python/ast*.c + asdl.c

| CPython file | LOC | Purpose |
|-------------|----:|---------|
| `Python/ast.c` | 1091 | AST validator (`_PyAST_Validate`) + scope/parent checks |
| `Python/ast_unparse.c` | 1029 | C-side AST -> source (deprecated; Python uses `Lib/_ast_unparse.py`) |
| `Python/asdl.c` | 6 | ASDL sequence allocators (stub, mostly inline in headers) |

`Python/ast_opt.c` is not present in 3.14.5 (folded into the compiler).
We do not port it.

### Lib/

| CPython file | LOC | Purpose |
|-------------|----:|---------|
| `Lib/ast.py` | 680 | `NodeVisitor`, `NodeTransformer`, `literal_eval`, `dump`, `parse`, `unparse` |
| `Lib/_ast_unparse.py` | 1161 | `_Unparser` class (the engine behind `ast.unparse`) |

### Grammar

| CPython file | LOC | Purpose |
|-------------|----:|---------|
| `Grammar/python.gram` | ~1900 | PEG grammar source. The generator emits `Parser/parser.c` from this. |
| `Tools/peg_generator/pegen/` | ~5000 | The generator itself (drives `make regen-pegen`). Owned by spec 1719 only because `test_peg_generator/` exercises it. |

## What gopy already has

| gopy file | Maps to | Notes |
|-----------|---------|-------|
| `parser/parser.go` | `Parser/peg_api.c` + `Parser/pegen.c` entry | Calls into `parser/pegen` runtime; mode dispatch matches `_PyPegen_run_parser`. |
| `parser/pegen/parser.go` | `Parser/pegen.c` runtime | Memoization + rule dispatch. |
| `parser/pegen/parser_gen.go` | `Parser/parser.c` (generated) | Hand-translated generated parser. Drift here is the largest risk surface. |
| `parser/pegen/actions.go` | `Parser/action_helpers.c` | AST-build helpers. |
| `parser/pegen/errors.go` | `Parser/pegen_errors.c` | Syntax error generation. |
| `parser/pegen/fstring.go` | `Parser/string_parser.c` (f-string section) | F-string parse-time decoding (spec 1718 P25 hardened this). |
| `parser/string/fstring.go` + siblings | `Parser/string_parser.c` | String prefix + escape + N{name} resolution. |
| `parser/errors/messages.go` | `Parser/pegen_errors.c` templates | `SyntaxError` message templates. |
| `ast/nodes_gen.go` | `Parser/Python.asdl` | Generated AST node struct definitions. |
| `ast/asdl.go` | `Include/internal/pycore_asdl.h` | ASDL sequence runtime. |
| `ast/validate.go` | `Python/ast.c` | AST validator. Partial port; gap audit below. |
| `ast/unparse.go` | `Lib/_ast_unparse.py` | AST -> source. Partial port; gap audit below. |
| `ast/dump.go` | `Lib/ast.py` `dump` | Pretty-print. |
| `ast/walk.go`, `ast/visitor.go` | `Lib/ast.py` `walk` / `NodeVisitor` | Traversal. |
| `ast/literal_eval.go` | `Lib/ast.py` `literal_eval` | Constant folding. |
| `ast/preprocess.go` | `Python/ast.c` rewrites + post-parse normalization | The "fix locations, parent links" pass. |
| `stdlib/ast.py` | `Lib/ast.py` (680 LOC) | Currently vendored shim. Spec 1719 makes it byte-identical to upstream. |
| `stdlib/_ast.py` | `_ast` C extension shim | Stub bridging Python-level `_ast` to gopy's Go-side `ast` package. |

Notable gaps the audit surfaces:

- No `module/ast/` or `module/_ast/`. `ast` is currently pure-Python on
  top of `_ast`. Once the validator + unparse ports land we may add a
  C-fast-path module, but it is not required for the gate.
- `parser/pegen/parser_gen.go` is hand-translated. CPython regenerates
  `Parser/parser.c` from `Grammar/python.gram` via the PEG generator.
  Spec 1719 does NOT regenerate; it pins parity with the 3.14.5 frozen
  output and notes any divergence per-rule in the per-row work items.
- `Python/ast.c` validator coverage is partial. Each test row that
  exercises an unhandled construct adds the missing validator branch.

## Test panel (target)

All 27 top-level tests live in `/Users/apple/cpython-314/Lib/test/` and
are vendored unchanged into `test/cpython/` by the Phase 1 commit.
Baseline column captures the post-spec-1718 starting point on commit
`60ad2e42` (this branch, no porting work yet).

| Test | LOC | Mark | Baseline | Blocker |
|------|----:|------|----------|---------|
| test_int_literal          |  143 | ready | **6/6 OK** | none — already green |
| test_grammar              | 2063 | done  | parse error: `compile: cannot delete target *ast.Tuple` | codegen drift on `del (a, b)` lowering |
| test_decorators           |  341 | ready | Ran 16, FAILED (failures=2) | top-level run works; 2 row failures to diagnose |
| test_eof                  |  171 | ready | Ran 6, FAILED (failures=6) | EOF/continuation handling in lexer or parser |
| test_keywordonlyarg       |  178 | ready | Ran 11, FAILED (failures=1, errors=2) | `SyntaxError: invalid syntax (<test>, line 1)` inside `assertRaisesRegex` body |
| test_named_expressions    |  767 | ready | Ran 74, FAILED (failures=8, errors=77) | `NameError: name 'range' is not defined` inside class bodies + walrus codegen rows |
| test_positional_only_arg  |  452 | ready | Ran 28, FAILED (failures=16, errors=7) | `f() got multiple values for argument` + parse errors on pos-only test sources |
| test_string_literals      |  356 | ready | Ran 20, FAILED (failures=2, errors=10) | `AttributeError: module 'unittest' has no attribute '__warningregistry__'` (warning-state plumbing) |
| test_type_comments        |  447 | ready | Ran 18, FAILED (errors=18) | `_feature_version` kwarg now accepted; remaining errors track PyCF_ONLY_AST + ast.Mod -> _ast bridge |
| test_unicode_identifiers  |   32 | ready | OK | Was 3 failures; NFKC fold + char-based SyntaxError column close the panel |
| test_annotationlib        | 2375 | ready | parse error: `compile: ClassDef with PEP 695 type params not yet supported` | PEP 695 generic-class lowering |
| test_asdl_parser          |  131 | ready | Traceback (likely module gap) | needs deeper trace |
| test_fstring              | 1871 | ready | parser farthest-token mis-points to `import ast` line 10 | parser drops mid-file but reports wrong location; root cause around f-string assertAllRaise corpus near line 880-900 |
| test_global               |  214 | 2 errors | Ran 20, FAILED (errors=2). test_caught_exception_group needs CHECK_EG_MATCH (PEP 654); test_type_alias needs CALL_INTRINSIC_1 oparg 12 (PEP 695). 18/20 pass after match-seq + frame back-pointer + symtable offset fixes. | spec 1719 D-test_global |
| test_metaclass            |  302 | ready | `ModuleNotFoundError: doctest` | doctest module not implemented |
| test_patma                | 3559 | ready | parse error reporting `import array` line 1 | farthest-token misreport; real failure is array module missing OR a patma rule |
| test_pep646_syntax        |  329 | ready | `ModuleNotFoundError: doctest` | doctest module not implemented |
| test_scope                |  839 | 1 fail | testLeaks (refcount/finalizer issue, not scope-resolution) | finalizer-count cycle |
| test_subclassinit         |  281 | ready | gopy main.go runtime crash | infinite recursion or panic to fix |
| test_syntax               | 3323 | ready | `ModuleNotFoundError: doctest` | doctest module not implemented |
| test_tstring              |  291 | ready | `ModuleNotFoundError: test.test_string._support` | needs `test/test_string/_support.py` helper or t-string lib |
| test_type_aliases         |  415 | ready | parse error: PEP 695 type params | same as test_annotationlib |
| test_type_annotations     |  891 | ready | `ModuleNotFoundError: test.test_inspect` | inspect-test helper missing |
| test_type_params          | 1469 | ready | parse error: PEP 695 type params | same blocker as test_annotationlib |
| test_unpack               |  222 | ready | `ModuleNotFoundError: doctest` | doctest module not implemented |
| test_unpack_ex            |  411 | ready | `ModuleNotFoundError: doctest` | doctest module not implemented |
| test_unparse              | 1066 | ready | `ModuleNotFoundError: pathlib` | pathlib module not implemented |

Three packages:

| Package | Files | Mark | Baseline | Blocker |
|---------|-------|------|----------|---------|
| `test_ast/`           | `__init__.py` (empty), `test_ast.py` (4267 LOC), `snippets.py`, `utils.py`, `data/ast_repr.txt` | ready | `import _ast_unparse` SyntaxError on stdlib load | `_ast_unparse` not vendored from `Lib/_ast_unparse.py`; once vendored the parser drops on a construct inside it |
| `test_future_stmt/`   | 9 files | ready | **4/4 OK + 1 partial** | `test_future_flags`, `test_future_multiple_features`, `test_future_multiple_imports`, `test_future_single_import` all green; `test_future` shows `EEE.EEEEEEEEEEEFFEEEEFEEEEEEEEEE` pattern (mostly errors) |
| `test_peg_generator/` | 6 files | deferred | gopy errors on package directory (`is a directory`) | parent block: PEG generator not shipped, follow-up spec |

### Blocker buckets (initial triage)

1. **Missing stdlib modules** (high impact, low difficulty):
   - `doctest` blocks `test_metaclass`, `test_pep646_syntax`, `test_syntax`, `test_unpack`, `test_unpack_ex` (5 tests)
   - `pathlib` blocks `test_unparse` (1 test)
   - `test.test_string._support` blocks `test_tstring` (1 test)
   - `test.test_inspect` blocks `test_type_annotations` (1 test)
   - `_ast_unparse` (the Python `Lib/_ast_unparse.py`) blocks `test_ast/test_ast.py` (the largest single test in the panel)

2. **PEP 695 type parameter grammar** (3 tests):
   - `compile: ClassDef with PEP 695 type params not yet supported` blocks `test_annotationlib`, `test_type_aliases`, `test_type_params`. Phase 6 closer.

3. **Compile / codegen drift** (4 tests):
   - `cannot delete target *ast.Tuple` (test_grammar)
   - `mapping pattern keys/patterns length mismatch` (test_global)
   - `free var ... has scope LOCAL in outer ...` (test_scope)
   - `compile() got an unexpected keyword argument "_feature_version"` (test_type_comments)

4. **Parser farthest-token misreport** (2 tests):
   - `test_fstring`, `test_patma` both report `import X` line 10 / 1 as the error location while CPython parses the file cleanly. Real failure is later in the file; the parser fallback in `parser/parser.go runParse` synthesizes a SyntaxError at the farthest token, which here is wrong. Needs `parser/pegen` audit against `Parser/pegen.c:1136 _PyPegen_run_parser` farthest_pos handling.

5. **Real row failures inside passing runs** (7 tests):
   `test_decorators`, `test_eof`, `test_keywordonlyarg`, `test_named_expressions`, `test_positional_only_arg`, `test_string_literals`, `test_type_comments`, `test_unicode_identifiers`. Each is a per-row gap to diagnose; some share root causes (e.g. `_feature_version` and `compile()` kwargs).

6. **Runtime crash** (1 test):
   `test_subclassinit` panics in `cmd/gopy/main.go`. Stack trace pointed to `main.mainWithProfile`; needs reproducer + fix.

7. **Already green** (1 test): `test_int_literal` (6/6 OK).

8. **Deferred** (1 package): `test_peg_generator/` — PEG generator port is its own follow-up spec, not blocking the panel.

LOC column drives sizing: the panel ships 17,580 lines of test code
plus 4,418 in `test_ast/`, 677 in `test_future_stmt/`, 2,034 in
`test_peg_generator/`. Working set ~24,700 lines for the whole panel.

## Phases

Spec 1719 reuses the spec-1718 cadence: one CPython subsystem at a
time, full-file port (per `feedback_full_subsystem_port`), test gate
flips after the port lands. Phases are sequential because later phases
build on the validator + unparse landings.

### Phase 1: vendor the panel

Vendor all 27 top-level tests + `test_ast/`, `test_future_stmt/`,
`test_peg_generator/` into `test/cpython/`. No code changes. The PR
documents the baseline pass/fail count per row so the closer phases
have a target.

Acceptance: every test file present under `test/cpython/`, MANIFEST
entry per row, baseline pass/fail recorded in spec 1719 panel table.

### Phase 2: ASDL grammar + node-type parity

Port `Parser/Python.asdl` byte-for-byte into the gopy mirror and run
the generated comparison against `ast/nodes_gen.go`. Any node-type
drift (new field, dropped optional, etc.) blocks the rest of the
panel. Phase output: zero diff between regenerated nodes and the
hand-maintained `nodes_gen.go`.

Acceptance: `tools/asdl_gen` (existing) regenerates `nodes_gen.go`
byte-identical to current; `Parser/Python.asdl` copy passes `diff`
against upstream.

### Phase 3: ast validator + ast.py byte-identical port

Port `Python/ast.c` to `ast/validate.go` completely (every
`_PyAST_Validate*` function gets a Go twin with line-number citation).
Then re-sync `stdlib/ast.py` against `Lib/ast.py` so the Python
surface (`NodeVisitor`, `NodeTransformer`, `literal_eval`, `dump`,
`parse`, `unparse`) matches CPython exactly.

Acceptance: `test_ast/test_ast.py` runs to completion with pass count
matching CPython on the same source. Diff between
`stdlib/ast.py` and `Lib/ast.py` is empty.

### Phase 4: unparse port

Port `Lib/_ast_unparse.py` to either `stdlib/_ast_unparse.py` (Python
implementation) or `ast/unparse.go` (Go fast path). The decision per
class is in the closer PR.

Acceptance: `test_unparse.py` goes green; `ast.unparse(ast.parse(src))`
round-trips on the CPython unparse corpus.

### Phase 5: pegen runtime + action_helpers parity

Re-audit `parser/pegen/parser.go` against `Parser/pegen.c` and
`parser/pegen/actions.go` against `Parser/action_helpers.c`. Each
function in the upstream files must be present in the gopy port with
a `// CPython: Parser/<file>.c:<line> <funcname>` citation. Functions
that already shipped get their citation line updated if the upstream
moved.

Acceptance: every public function in `pegen.c` and `action_helpers.c`
has a Go twin with citation; `test_syntax`, `test_eof`, `test_fstring`,
`test_tstring`, `test_named_expressions`, `test_string_literals`,
`test_unicode_identifiers` all pass.

### Phase 6: grammar feature panel

Port + close the language-feature rows that exercise specific PEP
implementations: `test_pep646_syntax`, `test_type_params`,
`test_type_aliases`, `test_type_annotations`, `test_type_comments`,
`test_annotationlib`, `test_patma`, `test_decorators`,
`test_keywordonlyarg`, `test_positional_only_arg`, `test_unpack`,
`test_unpack_ex`, `test_named_expressions`. Each closer is a separate
commit citing the relevant `Grammar/python.gram` rule.

Acceptance: every row above goes green and flips to `done` in
spec 1700.

### Phase 7: symtable + class-creation

`test_global`, `test_scope`, `test_metaclass`, `test_subclassinit`,
plus the `__future__` package. These rows do not exercise the parser
directly but live under the same panel in spec 1700 because they ride
on parse-time scope analysis.

Acceptance: rows flip to `done` with the closing commit hash.

### Phase 8: integer/decimal literal parsing

`test_int_literal`. Already partly covered by the v0.4 number panel
audit; spec 1719 re-runs it on the v0.12.5 build and pins the green.

Acceptance: row flips to `done`.

### Phase 9: PEG generator (deferred)

`test_peg_generator/` exercises `Tools/peg_generator/`. gopy does not
yet ship the generator; this phase opens a follow-up spec rather than
blocking 1719. The row stays `deferred` in spec 1700.

Acceptance: spec 1700 `test_peg_generator/` row keeps its `deferred`
mark with a pointer to the follow-up spec id.

### Phase 10: panel flip

Re-run every row, update spec 1700 marks, flip the
`v0.10.2 parser panel (#476)` checklist item.

Acceptance: spec 1700 line 35 checkbox is `[x]` and the panel table
shows `done` for every non-deferred row.

## Checklist

- [x] P0: write spec 1719 (this file) and mark lexer panel done in spec 1700
- [x] P1: vendor 27 top-level tests + 3 packages, record baseline counts (`60ad2e42` + this commit)
- [ ] P2: ASDL grammar + node-type parity (diff Python.asdl, regen nodes_gen.go)
- [ ] P3: `Python/ast.c` validator port + `Lib/ast.py` byte-identical sync
- [ ] P4: `Lib/_ast_unparse.py` port; `test_unparse` green
- [ ] P5: pegen.c + action_helpers.c citation audit + close `test_syntax / test_eof / test_fstring / test_tstring / test_named_expressions / test_string_literals / test_unicode_identifiers`
- [ ] P6: grammar feature panel (PEP 646 / 695 / 634 / 657 etc.)
- [ ] P7: symtable + class-creation rows (`test_global / test_scope / test_metaclass / test_subclassinit / test_future_stmt`)
- [ ] P8: `test_int_literal` re-run
- [ ] P9: PEG generator follow-up spec (deferred)
- [ ] P10: flip spec 1700 panel + #476 checklist

## Working notes

- The lexer panel taught us that one closer PR per subsystem keeps CI
  noise low. Spec 1719 follows the same pattern: each P closer is its
  own commit with citations and at least one new regression test.
- F-string and string-parser citations may overlap with spec 1718; do
  not re-port what 1718 already shipped, but re-cite if the upstream
  line numbers drifted under 3.14.5.
- `test_grammar` is already pinned green per the v0.10.2 release
  notes. Run it on the post-1718 build and confirm; if it regressed,
  treat the regression as a Phase-5 closer rather than re-opening
  spec 1710.
- The PEG-generated `parser/pegen/parser_gen.go` is the largest single
  diff surface (38k lines upstream). Spec 1719 does not regenerate;
  it audits per-rule when a panel row goes red.

### P2 closer 1: mapping/class pattern action-helper slice-type loss

`match x: case {"a": str()}` and `case Foo(value=Bar())` both failed
to compile with `keys/patterns length mismatch` and
`kwd attrs/patterns length mismatch`. Root cause was in
`parser/pegen/action_helpers_gen.go`:

- `actionPgenGetPatterns` filtered out entries whose pattern slot
  could not be coerced via `patternOf`, while the matching
  `actionPgenGetPatternKeys` never filters its key column. CPython
  `Parser/action_helpers.c:415 _PyPegen_get_patterns` is a 1:1 copy
  of `_PyPegen_get_pattern_keys` and never filters either, so the
  filter was pure drift. Removed.
- `patternSeqOf` walked `[]any` recursively but had no case for the
  concrete `ast.Seq[ast.Pattern]` / `[]ast.Pattern` slice types its
  callers actually produce, so it silently dropped every typed
  pattern fed through it. Added the missing cases.
- `actionPgenMapNamesToIDs` had the same slice-type blindness for
  `ast.Seq[ast.Expr]` keys (and missed `ast.Expr` -> `*ast.Name`
  unwrap), which is why class keyword patterns ended up with empty
  `KwdAttrs`. Added typed cases plus the `ast.Expr` -> `*ast.Name`
  branch so `Foo(value=Bar())` keeps its `value` attribute.

Parity regressions seeded in `parser/parity_test.go`:
`match_mapping_class_values` and `match_class_kwd` differential-dump
against CPython 3.14.5.

### P2 closer 2: PEP 3131 NFKC identifier normalisation

`test_unicode_identifiers.test_valid` and `test_non_bmp_normalized`
expected `µ` (U+00B5 MICRO SIGN) and `𝔘𝔫𝔦𝔠𝔬𝔡𝔢` to fold to their
NFKC forms (`μ` U+03BC and `Unicode`) at the parser level. gopy's
`parser/pegen/token_helpers.go:nameFromToken` was building Name
nodes from the raw UTF-8 bytes, so attribute lookups against the
folded form found nothing.

Ported the NFKC step from `Parser/pegen.c:502 _PyPegen_new_identifier`
into `normalizeIdentifier`: ASCII bytes short-circuit, anything else
runs through `unicodedata.NFKC`. Wired into `nameFromToken` before
the Name node is built.

### P2 closer 3: unicodedata <- parser/pegen import cycle

Adding the NFKC call introduced a test-time import cycle:

```
compile (test)
 -> parser
 -> parser/pegen
 -> module/unicodedata
 -> imp -> marshal -> specialize -> compile
```

`module/unicodedata` itself only needs `objects`; the `imp` import
came from a single `init()` block that called `imp.AppendInittab`.
Moved that registration into `stdlibinit/registry.go`'s init() so
`module/unicodedata` stays a leaf package and the cycle is gone.
`BuildModule` (was `buildModule`) is now exported; everything else
in the package is unchanged. The flat-package constraint is
respected because no new sub-packages were introduced.

### P2 closer 4: sys.monitoring + compile(_feature_version=...)

`bdb` imports `sys.monitoring` (PEP 669) and `ast.parse` calls
`compile()` with a keyword-only `_feature_version`. Both were
missing.

- `module/sys/monitoring.go`: stub namespace matching the public
  surface from `Python/instrumentation.c`. Constants are bit-
  identical so user code doing `events.LINE | events.CALL` gets
  the same integer CPython would. Callable hooks
  (`register_callback`, `set_events`, ...) are no-ops returning
  `None`; the interpreter does not emit instrumentation events
  yet.
- `builtins/compile.go`: added `_feature_version` to the accepted
  keyword list. Value is recognised but ignored (gopy always
  parses against the bundled grammar).

These unblock `ast.parse(source)` and `import bdb` along the
`pdb` / `doctest` import chain.

### P2 closer 5: lexer SyntaxError column was byte-based

`test_unicode_identifiers.test_invalid` expected `err.offset == 1`
for `€ = 2`, gopy reported 3. Root cause: `parser/lexer/state.go`
`recordError` was reading `s.col`, which is incremented per byte
in the lexer's `nextC`. CPython's `_syntaxerror_range` instead
decodes `[tok->line_start, tok->cur)` as UTF-8 and uses the code-
point count for `col_offset`.

Added `charColAt(pos)` that mirrors the CPython decode (lone
continuation bytes become one replacement code point, matching
`errors='replace'`), and routed both `recordError` and
`recordErrorWithText` through it. The byte-based `s.col` field is
kept as is; it still drives token start/end positions, which are
byte offsets in CPython's tokenizer surface as well.

This affects every lexer-emitted SyntaxError on non-ASCII lines.
Spot-checked against `parser/lexer` and the full `./...` suite,
nothing regressed.

### P7 closer 1: DEF_FREE_CLASS class-local cells

`test_scope.testFreeVarInMethod` exercises the case where a class
body binds a name (`def method_and_var(self): ...`) AND a sibling
method body references the same name as a free variable. CPython
resolves this by analysing the class scope with the
`DEF_FREE_CLASS` flag on the symbol: scope stays `LOCAL` (the
class-body binding wins for the class's own STORE_NAME) but the
class is added to the freevar pass-through so the sibling method
can still close over the enclosing function's cell.

`Python/compile.c:641 compiler_enter_scope` runs
`dictbytype(symbols, FREE, DEF_FREE_CLASS, ...)` which folds both
the FREE-scoped and the DEF_FREE_CLASS-flagged names into
`u_freevars`. gopy was filtering by `Scope == Free` only, so the
class's u_freevars came up empty, the nested method's
`emitClosure` bailed with `compile: free var %q has scope LOCAL
in outer %q`, and the class body had no cell to pass through.

Fix: `enterScope`, `freeVarsOf`, and `emitClosure` now all OR in
the `DEF_FREE_CLASS` predicate. The class's `u_freevars` list
mirrors CPython, the runtime's `COPY_FREE_VARS` lands the cell at
the right slot, and `LOAD_CLOSURE` reads from it when the nested
method's MAKE_FUNCTION is built.

### P7 closer 2: UnboundLocalError vs NameError at empty cells

`test_scope.testUnboundLocal*` checks that reading an unbound
local raises `UnboundLocalError` while reading an unbound free
variable raises `NameError`. The split lives in
`Python/ceval.c:3482 _PyEval_FormatExcUnbound`: an oparg below
`PyUnstable_Code_GetFirstFree(co)` (i.e. inside the cellvar /
fastlocals span) is `UnboundLocalError`; otherwise it's
`NameError`.

gopy returned a single `NameError: free variable referenced
before assignment` string from `LOAD_DEREF`, `DELETE_DEREF`,
`LOAD_FROM_DICT_OR_DEREF`, `LOAD_FAST_CHECK`, and `DELETE_FAST`.
Replaced each with a call to `formatExcUnbound(co, idx)` which
computes the freevar boundary as `nlocalsplus - len(freevars)`
and picks the right `Pyerrors` prefix. Added
`"UnboundLocalError:"` to `errorPrefixToType` so the unwinder
synthesises the correct PyExc_UnboundLocalError class.

### P7 closer 3: exec()/eval() auto-inject __builtins__

`builtin_exec_impl` inserts `__builtins__` into a user-supplied
globals dict that lacks it, so `exec(src, {"fail": fail})` still
resolves name lookups for builtin exception classes. The gopy
hook (`vm.currentEvaluator`) skipped that step, so
`test_scope.testUnboundLocal_AugAssign` raised `NameError: name
'UnboundLocalError' is not defined` from inside the exec'd
except clause. Added the same auto-injection from the running
frame's builtins before calling `EvalCode`.

### P7 closer 4: FrameFastToLocals walks LocalsplusKinds

`test_scope.testLocalsFunction` builds a closure where the
enclosing function binds `y = 1`, the inner function turns `y`
into a cell (via `y = 7` after a `nonlocal`-style rebinding),
and the test inspects `locals()` to verify both the original
fast-local snapshot AND the cell-bound value land in the dict.

CPython's `frame_get_var` iterates
`co_localsplusnames`/`co_localspluskinds` together: for each
slot it reads `frame->localsplus[i]` once, then dispatches on
the kind byte (`CO_FAST_FREE` / `CO_FAST_CELL` / plain). After
`fix_cell_offsets` collapses cell slots into the fast-local
span the same slot is tagged with `CO_FAST_CELL`, so the kinds
walk picks up the unwrapped cell value without double-writing.

gopy's `FrameFastToLocals` was using the legacy three-pass
walk (varnames, then cellvars, then freevars) and missing the
fix_cell_offsets rewrite: the cell value got overwritten by
the original arg-slot value, so `locals()["y"]` came back as
the pre-cell binding.

Fix: port `frame_get_var` directly. Added
`FrameLocalsPlusItem(i)` to the `InterpreterFrame` interface
(plus the `*Frame`, `FrameSnapshot`, and test-fixture stubs)
so the absolute-slot read works without recomputing the cell
offset. The pre-3.11 split is kept as
`frameFastToLocalsLegacy` for fixtures that build code
objects without `LocalsplusNames` / `LocalsplusKinds`.

### P7 closer 5: class body uses LOAD_LOCALS + LOAD_FROM_DICT_OR_DEREF

`test_scope.testClassNamespaceOverridesClosure`:

```python
x = 42
class X:
    locals()["x"] = 43
    y = x
assert X.y == 43
```

A class body that reads a name resolved as `FREE` should emit
`LOAD_LOCALS` + `LOAD_FROM_DICT_OR_DEREF`, not `LOAD_DEREF`:
the dict-first lookup is what makes `locals()["x"] = 43`
actually shadow the cell. CPython's
`codegen_nameop` (`Python/codegen.c:3215`) switches on the
active scope being `ClassBlock` and emits the prefix; gopy's
`emitDeref` always emitted `LOAD_DEREF`.

`LOAD_FROM_DICT_OR_DEREF` also needed the same fix on the VM
side: the previous arm popped the dict TOS and went straight to
the cell, ignoring the dict altogether. Now it pulls the name
out of `LocalsplusNames`, attempts `PyMapping_GetOptionalItem`
on the dict (via `objects.GetItem`), and only falls back to the
cell when the dict misses.

### P7 closer 6: symtable SyntaxError carries lineno / offset

`test_scope.testUnoptimizedNamespaces` runs
`compile("def f():\n    from sys import *\n    return 1\n", ...)`
expecting a `SyntaxError` with non-None `.lineno`. gopy raised a
plain `Exception` because:

1. `symtable.SyntaxError.Error()` returned only `Msg` with no
   `"SyntaxError:"` prefix, so the unwind path's prefix table
   could not promote it to the right exception class.
2. Even after typing, the alias built by
   `_PyPegen_alias_for_star` is positioned at `ast.NoPos`, so
   no lineno data made it onto the eventual SyntaxError
   instance.

Fix: prefix `SyntaxError:` in `(*symtable.SyntaxError).Error()`,
add `errors.SyntaxFromSymtable` to lift the structured record
into the canonical `(msg, (filename, lineno, offset, ...))`
2-arg form, and have `symtable.visitImportFrom` fall back the
alias position to the `ImportFrom` statement's `Pos` when the
alias was built without location info.


### P7 closer 7: match-sequence stackdepth + frame back-pointer + symtable offset

`test_global.test_match_seq` failed at compile time with
`compile: invalid CFG, inconsistent stackdepth`. Root cause was
`patternSequence` in `compile/codegen_stmt_match.go`: the helper
emitted `MATCH_SEQUENCE` + `GET_LEN`/`COMPARE_OP`/`TO_BOOL`
gates without bracketing them with `pc.onTop++` / `pc.onTop--`.
That made `jumpToFailPop` compute `pops = stores + onTop = 0`,
so the fail label was `fail_pop[0]` (a bare NOP). The successful
fall-through path consumed the subject via the unpack, leaving
the post-match join at depth 0; the failing branch jumped to
`fail_pop[0]` with the subject still on the stack at depth 1.
`cfgCalculateStackdepth` flagged the join.

Mirroring `Python/codegen.c:6280 codegen_pattern_sequence`, the
fix wraps the gate sequence with `pc.onTop++` before
`MATCH_SEQUENCE` and `pc.onTop--` right before the unpack /
`POP_TOP`. CPython's comments call this out explicitly:
"We need to keep the subject on top during the sequence and
length checks" and "Whatever comes next should consume the
subject".

A second blocker surfaced once `test_match_seq` compiled: every
test in the file errored with `ValueError: call stack is not
deep enough` because `check_warnings` walks back via
`sys._getframe(1)`. All five production `FrameStack.Push`
callsites passed `prev=nil`, so `f.Previous` was always nil and
`FrameBack()` always returned nil. Mirroring CPython's
`_PyThreadState_PushFrame` (which wires `tstate->current_frame`
as prev), `FrameStack.Push` now reads `Top()` itself; the
explicit `prev` parameter was redundant and has been dropped.

Third fix: `errors.SyntaxFromSymtable` exposed `Pos.ColOffset`
directly, but `Pos.ColOffset` is 0-indexed (matching
`ast.col_offset`) while `SyntaxError.offset` is 1-indexed.
CPython's `Python/symtable.c` symtable_error_set passes
`col_offset + 1` to `PyErr_RangedSyntaxLocationObject`; gopy
now does the same for both `offset` and `end_offset`. The four
`check_syntax_error` cases in `test_global` that compare against
`offset=5` now pass.

Fourth fix (drive-by): `contextlib.nullcontext.__new__` called
`inst.Dict().SetItem(...)` on a fresh instance whose `__dict__`
is lazily allocated, panicking on the nil dict. Swapped to
`EnsureDict()`. Surfaced because `test_global.test_enter_result`
uses `contextlib.nullcontext(value)`.

After all four, `test_global` runs 20 tests with two remaining
errors (PEP 654 `CHECK_EG_MATCH` for exception groups, PEP 695
`CALL_INTRINSIC_1` oparg 12 for type aliases). Both are tracked
under separate panel rows / tasks.
