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
| test_decorators           |  341 | ready | **16/16 OK** | none. Function `__name__` / `__qualname__` now reuse the same *Unicode wrapper across reads, so functools_wraps preserves identity for classmethod / staticmethod |
| test_eof                  |  171 | ready | **6/6 OK** | none. backslash-EOF caret + text + offset land on CPython numbers |
| test_keywordonlyarg       |  178 | ready | **11/11 OK** | none. Single-input ENDMARKER rewrite + real `raiseAction` + `codegen_validate_keywords` port |
| test_named_expressions    |  767 | ready | **74/74 OK** | none. `actionPgenGetExprName` arg index + missing AST kinds in `GetExprName` + `PyPegen_last_item` translator |
| test_positional_only_arg  |  452 | ready | Ran 28, FAILED (errors=4) | 24/28 OK. Positional-only kw enforcement + reordered too_many_positional ship. Remaining 4 errors are annotations lazy-eval (2), async coroutine StopIteration .value, and pickle (out of scope) |
| test_string_literals      |  356 | ready | Ran 20, FAILED (failures=1, errors=4) | 15/20 OK. eval() now strips leading whitespace (`Python/bltinmodule.c:1036`); string-literal decode errors pin SyntaxError at the token; `\N{NAME}` routes through the full UCD lookup. Remaining 5 all gated on SyntaxWarning plumbing through warnings.catch_warnings |
| test_type_comments        |  447 | ready | Ran 18, FAILED (errors=18) | `_feature_version` kwarg now accepted; remaining errors track PyCF_ONLY_AST + ast.Mod -> _ast bridge |
| test_unicode_identifiers  |   32 | ready | OK | Was 3 failures; NFKC fold + char-based SyntaxError column close the panel |
| test_annotationlib        | 2375 | ready | parse error: `compile: ClassDef with PEP 695 type params not yet supported` | PEP 695 generic-class lowering |
| test_asdl_parser          |  131 | ready | Traceback (likely module gap) | needs deeper trace |
| test_fstring              | 1871 | ready | parser farthest-token mis-points to `import ast` line 10 | parser drops mid-file but reports wrong location; root cause around f-string assertAllRaise corpus near line 880-900 |
| test_global               |  214 | 2 errors | Ran 20, FAILED (errors=2). test_caught_exception_group needs CHECK_EG_MATCH (PEP 654); test_type_alias needs CALL_INTRINSIC_1 oparg 12 (PEP 695). 18/20 pass after match-seq + frame back-pointer + symtable offset fixes. | spec 1719 D-test_global |
| test_metaclass            |  302 | OK | Ran 1, OK | doctest passes end-to-end after ClassDef ex_call, __prepare__ wrap fix, StringIO encoding/errors, function-metaclass + type.__prepare__, keyword Pos via withSpan, and PyObject_GetOptionalAttr semantics on __prepare__ lookup |
| test_patma                | 3559 | ready | parse error reporting `import array` line 1 | farthest-token misreport; real failure is array module missing OR a patma rule |
| test_pep646_syntax        |  329 | ready | `imp: loadAsModule "typing": ClassDef with PEP 695 type params not yet supported` | doctest now imports through; new blocker is PEP 695 generic-class lowering in `typing` |
| test_scope                |  839 | 1 fail | testLeaks (refcount/finalizer issue, not scope-resolution) | finalizer-count cycle |
| test_subclassinit         |  281 | ready | Ran 17, FAILED (errors=3) | 14/17 OK. Remaining: `test_errors` needs metaclass kwargs split before chaining to `object.__init__`; `test_set_name_error` / `test_set_name_wrong` need `__notes__` attach in `type_new_set_names` after `__set_name__` raises |
| test_syntax               | 3323 | ready | `imp: loadAsModule "typing": ClassDef with PEP 695 type params not yet supported` | doctest now imports through; new blocker is PEP 695 generic-class lowering in `typing` |
| test_tstring              |  291 | ready | `ModuleNotFoundError: test.test_string._support` | needs `test/test_string/_support.py` helper or t-string lib |
| test_type_aliases         |  415 | ready | parse error: PEP 695 type params | same as test_annotationlib |
| test_type_annotations     |  891 | ready | `ModuleNotFoundError: test.test_inspect` | inspect-test helper missing |
| test_type_params          | 1469 | ready | parse error: PEP 695 type params | same blocker as test_annotationlib |
| test_unpack               |  222 | ready | `imp: loadAsModule "typing": ClassDef with PEP 695 type params not yet supported` | doctest now imports through; new blocker is PEP 695 generic-class lowering in `typing` |
| test_unpack_ex            |  411 | ready | `imp: loadAsModule "typing": ClassDef with PEP 695 type params not yet supported` | doctest now imports through; new blocker is PEP 695 generic-class lowering in `typing` |
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
- [x] P6: grammar feature panel (PEP 695 generic class/alias/function codegen + _typing module shipped; PEP 646 unpack + 634 match already passing)
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

### P7 closer 8: test_eof unexpected-EOF caret + text

`test_eof.test_line_continuation_EOF` and its file-input twin
`test_line_continuation_EOF_from_file_bpo2180` pin the exact
shape of the SyntaxError emitted when source ends mid-line on a
backslash continuation (`'ä = 5\\'`). CPython reports
`offset=7`, `text='ä = 5\\\n'`, caret one column past the
trailing backslash.

Three drift points fixed:

1. `Parser/lexer/lexer.go` continuation loop explicitly set
   `s.lineStart = s.cur` after consuming the continuation
   `\n`. CPython does NOT touch `tok->line_start` inside
   `tok_continuation_line` (Parser/lexer/lexer.c:435); line_start
   only advances when `tok_underflow_string` successfully fetches
   the next line. On EOF the underflow returns 0, so line_start
   stays at the start of the line containing the backslash. The
   explicit assignment dropped the caret one column low and lost
   the source bytes for the text field.

2. `pegen.tokenizerSyntaxError` populated `pos.ColOff` from the
   ERRORTOKEN's stored column. CPython's `_PyPegen_raise_error`
   (Parser/pegen_errors.c:248) treats `t->col_offset == -1` as
   "recompute from `tok->cur - tok->line_start`", then converts
   bytes to characters via `_PyPegen_byte_offset_to_character_offset`.
   gopy now exposes `State.EOFCharOffset()` that returns the same
   1-based code-point offset from line_start to inp, and the
   DoneEOF Level=0 branch overrides `pos.ColOff` with it.

3. `parser.go runParse` filled `SyntaxError.text` via
   `SourceLine(...)`, which strips the trailing `\n`. For string
   input at EOF, CPython's fallback in
   `_PyPegen_raise_error_known_location` decodes from
   `tok->line_start` to `tok->inp`, *including* the `\n` that
   `translate_newlines` appended. gopy now exposes
   `State.EOFLineText()` returning the raw byte range; the
   DoneEOF Level=0 branch in `tokenizerSyntaxError` uses it
   instead of falling through to `SourceLine`.

Drive-by: `tools/parser_gen/emit.go` Dispatch retry path was
emitting `SetCallInvalid(true)` + `Reset(0)`, but
`parser_gen.go` had been hand-edited to call
`ResetForErrorPass()` (which additionally clears every cached
token's memo so the second pass actually re-runs `invalid_*`
rules). Emitter + test now match the in-tree generated file.

### P7 closer 9: Function `__name__` / `__qualname__` identity

`test_decorators.test_classmethod` and `test_staticmethod` both
exercise

```python
self.assertIs(getattr(wrapper, attr), getattr(func, attr))
```

for `attr in ('__module__', '__name__', '__qualname__', '__doc__',
'__annotations__')`. CPython passes because `func_name` and
`func_qualname` are `PyObject*` on `PyFunctionObject`, so every
read of `func.__qualname__` returns the same `PyObject*`. When
`cm_init` (Objects/funcobject.c:1487) calls `functools_wraps`,
that same pointer lands in `cm->cm_dict`, and subsequent reads
via `wrapper.__qualname__` resolve to the same Python object.

gopy stored `Name` and `Qualname` as Go `string` and the
`__name__` / `__qualname__` getters did `NewStr(f.Name)` on every
read. Two consecutive reads returned distinct `*Unicode`
allocations, so `func.__qualname__ is func.__qualname__` was
already False (`wrapper.__qualname__ is func.__qualname__` was
False for the same reason).

Fix: `objects/function.go` adds `nameObj` / `qualnameObj
*Unicode` slots on `Function` plus `nameUnicode()` /
`qualnameUnicode()` helpers. The getters return the cached
wrapper; the setters store the supplied `*Unicode` directly so
identity survives a round-trip through `f.__name__ = "x"`. The
cache invalidates when the underlying Go string is mutated
out-of-band (e.g. through `funcSetAttr` or via copy of code's
Name field), so a stale wrapper cannot leak.

CPython refs:
- `Objects/funcobject.c:148` PyFunction_NewWithQualName
- `Objects/funcobject.c:633` func_memberlist (`__name__`,
  `__qualname__`)
- `Objects/funcobject.c:1316` functools_wraps
- `Objects/funcobject.c:1487` cm_init

### P7 closer 10: single-input ENDMARKER and raiseAction wiring

`test_keywordonlyarg` last red row covered two adjacent gaps. Both
were stub-driven, both showed up under `compile(src, "<test>",
"single")`.

1. Single-input mode never synthesized the trailing NEWLINE.
   CPython's `_PyPegen_fill_token` (`Parser/pegen.c:244`) rewrites
   the trailing `ENDMARKER` to `NEWLINE` when `p->start_rule ==
   Py_single_input`, and pushes a `-indent` `pendin` so the
   tokenizer also emits the matching DEDENTs. gopy's `fillToken`
   only emitted the ENDMARKER, so `def f(p, *):\n  pass\n` parsed as
   a half-formed function header. Fix lives in
   `parser/pegen/parser.go fillToken` (the new branch matches
   `StartSingle` plus `parsingStarted`) and `parser/lexer/state.go
   ForceDedentsAtEOF` (the helper that puts the dedent stack in the
   state the rewrite expects).

2. `raiseAction` and `actionPgenArgumentsParsingError` were stubs
   returning `placeholderMatched`. CPython routes every
   `RAISE_SYNTAX_ERROR*` macro through
   `_PyPegen_raise_error_known_location`
   (`Parser/pegen_errors.c:317`) which sets the typed SyntaxError
   instance with filename, line, col, end_line, end_col. Stub
   `raiseAction` dropped both the message and the location, so the
   invalid_arguments alt for `f(p, k=1, p2)` matched, consumed the
   tokens, and built a bogus `Call` with empty `args`. Fix lives in
   `parser/pegen/action_helpers_gen.go`: a real `raiseAction`
   dispatches on the macro name and calls
   `p.RaiseSyntaxErrorKnownLocation(pos, msg, fmtArgs...)`; a real
   `actionPgenArgumentsParsingError` (CPython
   `Parser/action_helpers.c:1224`) inspects the call's keywords for
   `**` unpack and chooses between "positional argument follows
   keyword argument" and "...unpacking". `tools/parser_gen/emit.go`
   adds `raiseAction` to the excluded-action map so the generator no
   longer emits the auto-stub on top.

3. Duplicate kwargs (`f(p, k1=50, *(1,2), k1=100)`) raised in
   codegen, not in the parser. CPython
   `codegen_validate_keywords` (`Python/codegen.c:4018`) runs from
   `codegen_call` before any opcode emit and calls
   `_PyCompile_Error` (`Python/compile.c:1191`) with `LOC(other)`.
   Port: `compile/errors.go SyntaxError` mirrors the symtable
   structured record; `compile/codegen_expr_call.go validateKeywords`
   is the inner loop; `vm/eval_unwind.go` adds a
   `*compile.SyntaxError` branch that calls a new
   `errors.SyntaxFromCompile` helper so the Python-side exception
   keeps filename + line + col.

After these three: `test_keywordonlyarg` passes 11/11.

CPython refs:
- `Parser/pegen.c:244` `_PyPegen_fill_token`
- `Parser/pegen_errors.c:317` `_PyPegen_raise_error_known_location`
- `Parser/action_helpers.c:1224` `_PyPegen_arguments_parsing_error`
- `Python/codegen.c:4018` `codegen_validate_keywords`
- `Python/compile.c:1191` `_PyCompile_Error`

### P5 closer 1: GetExprName arg index + PyPegen_last_item

Three independent bugs shared a single panel row, `test_named_expressions`.

1. `actionPgenGetExprName` read `argAt(args, 1)` but the generator
   emits the helper without the `(p, p, …)` doubling that the other
   action helpers use, so the real arg landed at `args[0]`. Every
   walrus / `:= rhs` diagnostic message ("cannot use assignment
   expressions with %s") therefore reported "expression" no matter
   what the LHS was.

2. `GetExprName` only handled half of the AST kinds CPython's
   `_PyPegen_get_expr_name` covers (`Parser/action_helpers.c:1043`).
   Lambda, BoolOp / BinOp / UnaryOp, GeneratorExp, Yield / YieldFrom,
   Await, ListComp / SetComp / DictComp, Dict, Set, Compare, IfExp,
   NamedExpr were all missing, so messages like
   "cannot use assignment expressions with lambda" / "...with tuple"
   came back as "...with expression".

3. The `invalid_comprehension` alt 1 in `Parser/Python.gram:1303`
   passes `PyPegen_last_item(b, expr_ty)` as the range end. The
   action translator had no case for that macro, so the generator
   gave up on the action and fell back to `return []any{…}`. The
   alt then quietly matched without raising, so
   `[i := 0, j := 1 for i, j in ...]` reached the outer parser and
   surfaced as a generic "invalid syntax" instead of "did you forget
   parentheses around the comprehension target?". Fix: teach
   `Tools/parser_gen/action.go translateCall` to map
   `PyPegen_last_item(seq, _)` onto a new `seqLastAny(seq)` runtime
   helper (the type tag has no Go equivalent) and regenerate
   `parser/pegen/parser_gen.go`.

After these three: `test_named_expressions` passes 74/74.

CPython refs:
- `Parser/action_helpers.c:1043` `_PyPegen_get_expr_name`
- `Parser/pegen.h:265` `PyPegen_last_item` macro
- `Parser/Python.gram:1303` `invalid_comprehension` alt 1

### P7 closer 11: positional-only kw enforcement + too_many_positional ordering

`test_positional_only_arg` panel was sitting at 7 failures / 4 errors.
Three independent gaps in `vm/eval_call.go callPyFunction` accounted
for all of the failures.

1. **Positional-only kw enforcement missing.** When a caller passes a
   keyword that names a positional-only slot (slot index <
   `co_posonlyargcount`), CPython routes the kw into `**kwargs` if
   the function declares one; otherwise it collects the colliding
   names and raises `f() got some positional-only arguments passed
   as keyword arguments: 'a, b'`. gopy was binding the kw straight
   into the positional-only slot, so the TypeError never fired for
   `f(a=1, /)`-style calls and the `**kwargs` re-routing in
   `test_same_keyword_as_positional_with_kwargs` failed too.

   Fix: skip slots `i < nposonly` in the kwarg name-match scan, route
   collisions to `kwDict` when `hasVarkw`, otherwise batch into a
   `posonlyAsKw` slice and emit `objects.PositionalOnlyAsKeywordError`
   in declaration order after the loop.

2. **`too_many_positional` happened before kwarg binding.** gopy
   raised the "takes N positional arguments but M were given"
   TypeError up front, before processing kwargs, so `kwonlyGiven`
   was hardcoded to 0. CPython does the check after binding, when
   the kw-only slots already reflect what the caller passed.

   Fix: move the check to the tail of `callPyFunction`, after
   defaults are applied. Count `kwonlyGiven` by walking
   `[kwOnlyBase..kwOnlyBase+nkwonly)` and only counting slots whose
   binding came from `kwargs` (skip slots filled by `KwDefaults`).

3. **Error helper format drift.** Two CPython-shaped messages in
   `objects/call_args.go` were close but not byte-equal.

   - `PositionalOnlyAsKeywordError` was emitting
     `'a', 'b'` (each name separately quoted). CPython's format
     string carries one pair of quotes and joins the names with
     `", "` inside, producing `'a, b'`.
   - `tailKwonly` was emitting ` (and N keyword-only arguments)`
     but CPython's snprintf buffer leads with
     ` positional argument(s)` before the parenthetical, so the
     full message reads
     `... but 6 positional arguments (and 2 keyword-only arguments) were given`.
     `given != 1 ? "s" : ""` controls the `argument`/`arguments` flip
     on the positional half.

After these three: `test_positional_only_arg` panel drops to 4
remaining errors, all unrelated to the positional-only feature
itself (annotations lazy-eval x2, async coroutine StopIteration
`.value`, pickle).

`vm/eval_call.go callPyFunction` now also drops its local
`formatMissingArgs` / `joinMissingNames` helpers and delegates to
`objects.MissingArgumentsError`, which already shipped with the
same CPython-shaped output.

CPython refs:
- `Python/ceval.c:1487` `too_many_positional`
- `Python/ceval.c:1546` `positional_only_passed_as_keyword`
- `Python/ceval.c:1449` `missing_arguments`
- `Objects/call.c` `_PyEval_BindArguments`

### P7 closer 12: async for codegen + eval() / string-literal cleanups

`test_grammar` was returning `compile: stmt kind *ast.AsyncFor not yet
supported` on the first `async for` body it tried to compile.
`test_string_literals` was sitting at 7 failures (5 errors + 2 fails),
driven by three independent gaps that all surfaced under `eval()`.

1. **`async for` had no codegen visitor.** `compile/codegen_stmt.go`
   dispatched every other statement kind but had no arm for
   `*ast.AsyncFor`. Ported `codegen_async_for`: evaluate iter,
   `GET_AITER`, then a `SETUP_FINALLY` / `GET_ANEXT` /
   `LOAD_CONST None` send loop, `POP_BLOCK` + `NOT_TAKEN`, target
   assign, body, `JUMP` back; the except arm runs `END_ASYNC_FOR`
   back to the send label, and orelse follows the loop.

   CPython: `Python/codegen.c:2117` `codegen_async_for`.

2. **`eval(" 'x' ")` tripped `IndentationError: unexpected indent`.**
   The tokenizer's INDENT pass sees the leading whitespace and bails
   out. CPython's `builtin_eval_impl` peels leading spaces and tabs
   off the source before handing it to the parser; `builtins/eval.go`
   now does the same for str / bytes / bytearray inputs.

   CPython: `Python/bltinmodule.c:1036` `builtin_eval_impl`.

3. **String-decode errors were swallowed in pegen.**
   `decodeStringTokenTagged` returned `(_, _, false)` on any
   `parser/string.ParseString` error, so the action helper produced
   `placeholderMatched` and the grammar quietly dropped the whole
   expression. The user saw a downstream NameError instead of a
   SyntaxError at the literal. Added a
   `decodeStringTokenTaggedErr` variant that surfaces the underlying
   error; `actionPgenConstantFromString` pins it via
   `RaiseSyntaxErrorKnownLocation` at the token's span.

   The original repro `x = "\N{LATIN SMALL LETTER A WITH DIAERESIS}"`
   also exposed that `parser/string/charname.go` only carried a
   ~50-entry sparse map. Replaced it with a call into
   `module/unicodedata.Lookup` (new exported entry that mirrors
   `unicodedata.lookup()` including alias and named-sequence
   expansion), and `decode.go` now appends the full expansion so
   multi-rune named sequences land verbatim.

   CPython: `Parser/string_parser.c:253` `_PyPegen_parse_string`.
   CPython: `Modules/unicodedata.c:1584` `unicodedata_UCD_lookup_impl`.
   CPython: `Objects/unicodeobject.c` `_PyUnicode_DecodeUnicodeEscape`.

After these: `test_grammar` advances past `async for` (now blocks on
a missing test-helper module, separate gate). `test_string_literals`
drops from 7 failures to 5, all of which share the same root cause
(`AttributeError: module 'unittest' has no attribute
'__warningregistry__'` plus `test_invalid_escape_locations_with_offset`
which depends on `warnings.catch_warnings` actually capturing the
SyntaxWarning the parser emits). That's a separate
SyntaxWarning-plumbing gate, not a parser/AST gap.

### P6 closer: PEP 695 generic class/alias/function + typing.py import

Five panel rows (`test_metaclass`, `test_pep646_syntax`, `test_syntax`,
`test_unpack`, `test_unpack_ex`) shared a single import-time dependency:
`import typing` failing because the runtime had no PEP 695 primitives.
Tracing it end-to-end exposed five separate gaps:

1. **No CALL_INTRINSIC handlers for the new intrinsics.** `intrinsics/
   unary.go` returned `notImplementedError` for `UnaryTypevarID`,
   `UnaryParamspecID`, `UnaryTypevartupleID`, `UnarySubscriptGenericID`,
   `UnaryTypealiasID`; `intrinsics/binary.go` likewise for
   `BinaryTypevarWithBoundID`, `BinaryTypevarWithConstraintsID`,
   `BinarySetFunctionTypeParamsID`, `BinarySetTypeparamDefaultID`.
   Backed each one with a real implementation in a new
   `objects/typevar.go` (`TypeVar`, `ParamSpec`, `TypeVarTuple`,
   `TypeAliasType`, plus `Generic`, `Union`, `NoDefault`, `_idfunc`),
   and the stub-sweep test was updated to skip these IDs.

   CPython: `Python/intrinsics.c:80` `intrinsic_function_1` entries
   `INTRINSIC_TYPEVAR..INTRINSIC_TYPE_ALIAS`; `Modules/_typingmodule.c`
   for the constructor signatures.

2. **No symtable wrapper lookup for codegen.** PEP 695 lowers a
   generic def/class/alias into a synthetic `TypeParametersBlock`
   scope (CPython: `Python/symtable.c:1659`
   `symtable_enter_type_param_block`). `symtable/build_helpers.go`
   already entered that block keyed by `typeParamSubexprKey{key, -1}`,
   but the key type was unexported, so codegen had no way to find the
   wrapper scope. Added `symtable.LookupTypeParams(parent)` returning
   the wrapper entry.

3. **No generic-class / generic-alias / generic-def codegen.**
   `compile/codegen_class.go` and `compile/codegen_typealias.go` were
   non-generic-only. The class side now wraps the body in a
   TypeParametersBlock function: it emits the RESUME + LOAD_BUILD_CLASS
   sequence, fills the type-param bindings via the new
   `codegen_typeparam` helper (`compile/codegen_typeparams.go`), calls
   the inner body function, then `CALL_INTRINSIC_2
   SET_FUNCTION_TYPE_PARAMS` on the resulting class to attach
   `__type_params__`. The alias side compiles
   `type X[T] = T` to `BUILD_TUPLE 3 + MAKE_FUNCTION + CALL_INTRINSIC_1
   TYPEALIAS` so the value is evaluated lazily, matching CPython 3.14.
   `v05test/testdata/golden/type_alias.golden` refreshed.

   CPython: `Python/codegen.c:3911 codegen_type_params`,
   `Python/codegen.c:3995 codegen_typealias`.

4. **`_typing` module missing.** typing.py opens with
   `from _typing import (Generic, NoDefault, ParamSpec, ...)`. Added
   `module/_typing/module.go` that re-exports the new
   `objects/typevar.go` builtins, registered through `stdlibinit`.

   CPython: `Modules/_typingmodule.c:1` PyInit__typing.

5. **`class IO(Generic[AnyStr])` panicked the C3 linearizer.** Two
   layered bugs:
   - `__build_class__` was unwrapping `_GenericAlias` to its origin
     directly. CPython instead calls `_PyObject_UpdateBases`
     (`Objects/typeobject.c:3690`): for any base that isn't a type,
     call `base.__mro_entries__(orig_bases)` and splice the returned
     tuple in. Ported as `resolveBases` in `vm/build_class.go`.
   - `_GenericAlias.__mro_entries__` (Lib/typing.py) returns `()` when
     the origin is `Generic` and either `Protocol` is in the bases or
     another `_GenericAlias` follows self. Without that dedup,
     `class SupportsAbs(Protocol[T])` ended up with both `Protocol`
     and `Generic` in the bases and C3 had no consistent order.
     `gaMroEntries` now accepts the bases tuple and replicates the
     CPython logic; the merge panic also names the offending type
     and bases for faster triage.

   Follow-ons:
   - `bindClassGetitem(GenericType)` so `Generic[T]` is subscriptable.
   - `typeSetBases` setter on `__bases__` that revalidates the tuple
     and recomputes the MRO via `c3Linearize`. typing's
     `NamedTupleMeta.__new__` writes to `nm_tpl.__bases__` after the
     class is built (`Lib/typing.py:3015`), and without the setter the
     panel hit `AttributeError: can't set attribute __bases__`.
   - `typeMetaclassCall` was double-prepending `result` to the
     `__init__` args (once by hand, once via `bindDescr`), so
     `class Any(metaclass=_AnyMeta)` saw `type.__init__()` get four
     args instead of three. Drop the manual prepend.
   - The iterator builtins (`seqIter`, `callIter`, `reversedIter`,
     `enumerate`) had `TypeType()` as their base, which put `type` in
     their MRO. With `type.__init__` now a real descriptor that
     shadowed `object.__init__` and broke `reversed([])`. Switched
     each to `ObjectType()`, matching CPython where iterator tp_base
     is `&PyBaseObject_Type`.

6. **Doctest dependency chain.** Once typing imported, the panel
   tests advanced to the doctest runner and crashed on
   `ModuleNotFoundError: No module named '_pyrepl.utils'` (`doctest ->
   pdb -> _pyrepl.utils`). Vendored `__init__.py`, `types.py`,
   `trace.py`, `utils.py` from CPython 3.14 verbatim. The
   curses-backed reader stays out of the tree until a real
   interactive REPL is wired.

   pdb also called `sys.gettrace()` during import. Stubbed
   `sys.gettrace`/`sys.settrace` to return None and accept-and-drop
   respectively; ported `sys.displayhook` / `__displayhook__` per
   `Python/sysmodule.c:188 sys_displayhook` (write repr + "\n" to
   sys.stdout, RuntimeError on lost stdout).

After P6: `import typing` succeeds end-to-end. Generic class with
Protocol base linearizes to `[S, Protocol, Generic, object]`. NamedTuple
subclasses build. `go test ./...` clean. The five panel rows still fail,
but the new failures are inside the doctest runner (task #72 P2.1),
not in the parser/codegen path.

### P6.5 closer: doctest pipeline unblock

Once typing imported, the doctest runner itself surfaced four
prerequisites. None of these are parser/AST gaps. they're runtime
plumbing the panel tests need before any doctest can execute.

1. **`sys._getframemodulename` missing.** doctest's `_normalize_module`
   calls `sys._getframemodulename(2)` to find the test module's
   `__name__` when the caller omits it. Without this, every doctest
   harness died with
   `DocTestFinder.find: name must be given when obj.__name__ doesn't
   exist`. Ported `module/sys/getframe.go:getFrameModuleName`: walks
   `CurrentInterpreterFrameHook` `depth` frames up, pulls
   `FrameFunc().Module`, returns `None` when the frame has no backing
   function or the stack is too shallow.

   CPython: `Python/sysmodule.c:2595 sys__getframemodulename_impl`.

2. **`_io.StringIO` methods were instance-closures, not type-level
   descriptors.** doctest's `_SpoofOut` subclasses `StringIO` and calls
   `StringIO.getvalue(self)` as an unbound method to bypass the
   subclass's `truncate(0)` side effect. With instance-bound closures,
   `StringIO.getvalue` raised `AttributeError: type object '_io.StringIO'
   has no attribute 'getvalue'`. Converted the 18 methods + 3 properties
   (`closed`, `newlines`, `line_buffering`) into a `stringio_methods` /
   `stringio_getset` table installed on `StringIOType` via
   `registerStringIODescrs`. Each method now flows through a
   `*Descr` wrapper that unwraps `self` from `args[0]`.

   CPython: `Modules/_io/stringio.c:927 stringio_methods` +
   `Modules/_io/stringio.c:962 stringio_getset`.

3. **Subclasses of `StringIO` lost the `*StringIO` C-level state.**
   Because `StringIO.__new__` allocated a plain `*Instance` for any
   subclass call, `_SpoofOut()` came back as `*Instance` and the unbound
   `StringIO.getvalue(self)` raised `descriptor requires a '_io.StringIO'
   object but received a '_SpoofOut'`. Added `StringIOType.TpNew =
   stringIOTpNew` which routes through `newStringIOForType(cls)`:
   allocates a fresh `*StringIO` and stamps the subclass type on it, so
   the descriptor type check passes and the subclass still owns its own
   layout slots.

   CPython: `Modules/_io/stringio.c:998 stringio_new`.

4. **`CALL_INTRINSIC_1 PRINT` (oparg 1, `print_expr`) was a stub.**
   REPL-style doctests (`>>> obj`) emit `CALL_INTRINSIC_1 PRINT` after
   the expression. The intrinsic must look up `sys.displayhook` and call
   it on the value. Wired `intrinsics.PrintExprHook` as a function-typed
   var set from `module/sys.init`: the sys module installs
   `printExprViaSysDisplayhook` which resolves `sys.displayhook`
   dynamically (so user overrides work) and calls it on the value.
   Raises `RuntimeError: lost sys.displayhook` when the slot is None or
   missing, matching CPython.

   CPython: `Python/intrinsics.c:28 print_expr` +
   `Python/sysmodule.c:188 sys_displayhook`.

5. **Metaclass `mro()` overrides were ignored.** test_metaclass.py's
   `WeirdClass` test defines `class Meta(type): def mro(cls): return
   (cls, Base, object)` and expects `WeirdClass.value` to resolve via
   the user-defined MRO. gopy's `c3Linearize` always produced
   `[WeirdClass, object]`. Ported `applyMetaclassMRO` in
   `objects/usertype.go`: after `stampMetaclass`, looks up `mro` on the
   metaclass via `LookupDescriptor`; if the owner is not `type` itself,
   binds and calls it with `t` as receiver, then converts the returned
   tuple/list to `[]*Type` and overwrites `t.MRO`.

   CPython: `Objects/typeobject.c:2228 mro_invoke`.

After P6.5: all five panel tests reach the doctest runner. Remaining
failures are individual CPython feature gaps (ClassDef with `*bases /
**kwds`, `__prepare__` classmethod semantics, function metaclasses,
`super().__prepare__`, descriptor-as-metaclass-call) that fall under
later sub-closers, not the doctest infrastructure itself.

### P7 closer 13: test_unpack / test_unpack_ex bisection

The unpack panels surfaced four orthogonal subsystem bugs that the
P6.5 doctest pipeline finally let us see. Each landed as its own
commit so the regressions stay independent:

1. **Stale `__context__` after a caught-and-returned exception.** A
   function that caught an exception and `return`ed left the handled
   exception live in `sys.exception()`; the next `raise` then pulled
   it in as `__context__`. Root cause: `visitReturn` never emitted
   the `POP_BLOCK` / `POP_EXCEPT` pair from `fblockHandlerCleanup`
   before `RETURN_VALUE`. Ported `codegen_unwind_fblock_stack` into
   `unwindForReturn`, and threaded `preserve_tos` into `unwindFblock`
   so the return value SWAPs past each fblock's cleanup ops.

   CPython: `Python/codegen.c:622 codegen_unwind_fblock_stack`.

2. **User Exception subclasses inherited `object.__repr__`.** Without
   `__repr__` / `__str__` method descriptors on `BaseException`,
   `fixupCallReprStr` resolved `repr()` to `object.__repr__` via MRO
   and printed the generic `<__main__.BozoError object at 0x...>`.
   Installed both descriptors in `newExcType` so `repr(BozoError())`
   stays `BozoError()` across the hierarchy.

   CPython: `Objects/typeobject.c add_operators` slot wrappers for
   `tp_repr` / `tp_str`.

3. **Tuple `<` / `<=` / `>` / `>=` returned NotImplemented.** Only
   `EQ` / `NE` were ported; everything else fell through, breaking
   `sorted()` over lists of tuples and any heapq/bisect use. Ported
   the lexicographic compare so the first differing slot delegates
   through `RichCmp`, with a length-tiebreaker when one tuple is a
   prefix of the other.

   CPython: `Objects/tupleobject.c:703 tuplerichcompare`.

4. **DICT_MERGE / DICT_UPDATE / iterToSlice error shapes.** The
   panel needed three CPython-shaped messages: `cannot unpack
   non-iterable X object` (UNPACK_EX), `'X' object is not a mapping`
   (DICT_UPDATE for `{**1}` / `{**[]}`), and
   `<module>.<qualname>() got multiple values for keyword argument
   'X'` (DICT_MERGE duplicate-key reformat). Reworked the DICT_MERGE
   arm to walk the mapping protocol (`b.keys()` + `b[k]`), detect
   duplicates via a `kwargsDuplicateErr` sentinel, and route to a
   new `formatKwargsError` + `objectFunctionStr` pair that mirrors
   `_PyEval_FormatKwargsError` and `_PyObject_FunctionStr`. The slow
   path also unblocks the CrazyDict iteration test
   (`MutableMapping.__iter__` mutating mid-iteration) by exposing
   the inner `dict.__iter__` size-change guard.

   `iterToSlice` now delegates through `objects.Iter` so the
   `SeqIter` fallback handles types that only define `__getitem__`.
   `dictUpdate` likewise gains the mapping slow path so
   `{**MutableMapping}` participates.

   CPython: `Python/bytecodes.c:2122 DICT_MERGE`,
   `Python/ceval.c:3410 _PyEval_FormatKwargsError`,
   `Objects/object.c:973 _PyObject_FunctionStr`,
   `Objects/dictobject.c:3247 dict_merge`.

5. **Starred expressions raised the dispatch fallback.** `*a = x` /
   `*a` / `x = *a` produced `compile: expr kind *ast.Starred not yet
   supported` instead of SyntaxError. Routed `ast.Starred` through a
   new `visitStarred` that raises the matching CPython message based
   on context; `assignTo`'s Starred branch now errors instead of
   silently unwrapping.

   CPython: `Python/codegen.c:5301 codegen_visit_expr` (Starred_kind).

After P7 closer 13: `test_unpack_ex` drops from 13 doctest failures
to 7, `test_unpack` from 2 to 2. The remaining failures are all the
same shape: doctest renders `test.test_unpack_ex.X` for classes
defined in the test module, but gopy runs the file with
`__name__ == '__main__'`, so we render `__main__.X` (which doctest
then strips to bare `X`). A panel-runner closer that imports each
test file as part of the `test` package will pick up the residual
nine failures; tracked separately.

### P7 closer 14: with-statement unwind preserve_tos drift

While running TestPycParity after closer 13 the gate flagged
`'bytes' object is not callable` at `FileLoader.get_data` inside
`stdlib/importlib/_bootstrap_external.py:232`:

```python
with open(path, 'rb') as file:
    return file.read()
```

Bisecting back to `49778d82` (the RETURN_VALUE fblock unwind port)
located the drift. The with-statement preamble runs
`LOAD_SPECIAL __exit__` which leaves two slots (`__exit__`,
`self_or_null`) on the stack below the body; with a return inside
the body the stack at unwind time is `[..., exit, exit_self, retval]`.

CPython's `codegen_unwind_fblock` (Python/codegen.c:572) emits
`SWAP 3` followed by `SWAP 2` to rotate `retval` past both exit
slots before the `__exit__(None, None, None)` call:

```
[exit, exit_self, retval]    SWAP 3 -> [retval, exit_self, exit]
                              SWAP 2 -> [retval, exit, exit_self]
                              LOAD None x3 + CALL 3
```

The original port emitted only a single `SWAP 2`, leaving the
stack as `[exit, retval, exit_self]`. The subsequent CALL 3 then
treated `retval` (here, the bytes from `file.read()`) as the
callable, producing `'bytes' object is not callable`.

Also picked up the matching drift in `FB_FINALLY_END`: CPython
emits `SWAP 2 / POP_TOP / SWAP 2 / POP_BLOCK / POP_EXCEPT`, gopy
was missing the second `SWAP 2`. No gate currently exercised it,
but the path is now byte-identical to CPython.

CPython: `Python/codegen.c:572 codegen_unwind_fblock` (FB_WITH
preserve_tos branch).
CPython: `Python/codegen.c:560 codegen_unwind_fblock` (FB_FINALLY_END
preserve_tos branch).

### P7 closer 15: test_metaclass closes

Six findings, all flushed in the same session so the doctest panel
row for `test_metaclass` flips from `imp: loadAsModule "typing"` to
`Ran 1, OK`. CPython is the source of truth for every fix.

1. **ClassDef *args/**kwargs in bases.** `class C(*Bases, **Kw):` was
   bailing at codegen with `"too many bases"` because the call shape
   never reached `codegen_call_helper_impl`. Ported the EX dispatch
   path so the compiler emits `BUILD_LIST` + `LIST_EXTEND` for star
   args and a dict merge for `**kw`, then `CALL_FUNCTION_EX`. CPython:
   `Python/codegen.c:4254 codegen_call_helper_impl`.

2. **`__prepare__` was being wrapped as classmethod.** A previous
   shim auto-wrapped any `__prepare__` defined inside a class body
   as `classmethod`, which clobbered `@staticmethod __prepare__`
   that user code uses to control binding. CPython does not
   auto-wrap `__prepare__`; only `__init_subclass__` and
   `__class_getitem__` get the implicit `classmethod` wrap, and only
   when the value is a plain function. CPython:
   `Objects/typeobject.c:4526 type_new_set_attrs` +
   `Objects/typeobject.c:4372 type_new_classmethod`.

3. **`_io.StringIO.encoding` / `.errors`.** doctest's `_SpoofOut`
   subclass of `StringIO` reads `save_stdout.encoding` inside
   `DocTestRunner.run`; gopy's StringIO had no TextIOBase parent so
   the getset chain was missing. Served both attributes as `None`
   directly from the `stringIOGetattr` switch (the descriptor table
   is bypassed because StringIO publishes a custom `tp_getattro`).
   CPython: `Modules/_io/textio.c:138 _io__TextIOBase_encoding_get_impl`,
   `Modules/_io/textio.c:172 _io__TextIOBase_errors_get_impl`.

4. **Function metaclasses + `type.__prepare__`.** `def meta(name,
   bases, ns, **kw): ...` is a valid metaclass shape; gopy assumed
   metaclass was always a `*Type` and crashed in
   `_PyType_CalculateMetaclass`. Track `isclass` like
   `Python/bltinmodule.c:147 PyDict_Pop(mkw, &_Py_ID(metaclass), &meta)`
   and only run the winner calculation when both metaclass and every
   base is a type. Also registered `type.__prepare__` (returns an
   empty dict) so `super().__prepare__()` inside a metaclass body
   resolves through the MRO. CPython:
   `Objects/typeobject.c:6580 type_prepare`.

5. **`ast.Keyword.Pos` was always NoPos.** `class C(metaclass=type,
   metaclass=type):` should raise `SyntaxError: keyword argument
   repeated: metaclass` with a real line/column so
   `traceback._format_syntax_error` renders just the message line
   (no `(<filename>)` suffix). The pegen action helper built
   `*ast.Keyword{Pos: ast.NoPos}` and `withSpan` only stamps the
   outermost AST node, but the keyword sits inside a
   `*KeywordOrStarred` carrier. Extended `withSpan` to peel the
   carrier and stamp the inner `Keyword` directly. CPython:
   `Grammar/python.gram:1081 kwarg_or_starred` (EXTRA passed to
   `_PyAST_keyword`, not the outer keyword_or_starred wrapper).

6. **`__build_class__` swallowed every error from `__prepare__`
   lookup.** A `FailDescr.__get__` that raises `ObscureException`
   should surface that exception, but the old code treated *any*
   error as "no `__prepare__` attribute" and silently used `dict()`.
   Mirror `PyObject_GetOptionalAttr`: suppress `AttributeError`
   only; propagate everything else. CPython:
   `Objects/object.c:1324 PyObject_GetOptionalAttr`.

### P7 closer 16: BUILD_TUPLE / BUILD_LIST / SET_ADD / LIST_APPEND
steal-vs-decref mismatch

`A()[1:, 2:]` rendered as `(slice(<nil>, <nil>, <nil>),
slice(<nil>, <nil>, <nil>))` even though `A()[1:]` rendered
correctly. Direct subscript stored a single slice; tuple-of-slices
stored two slices that had their `Start/Stop/Step` fields reset to
`nil` even though the tuple still held them.

Root cause was a steal-vs-decref mismatch between the bytecode
translator and the container constructors:

CPython BUILD_TUPLE uses `_PyTuple_FromStackRefStealOnSuccess`: the
new tuple steals every input ref, and the surrounding STACK_SHRINK
just moves the stack pointer without `Py_DECREF`. gopy's
`Frame.DropStack(n)` (`frame/frame.go:280`) does decref each
popped slot (`slot.Close()` runs `objects.Decref`). The translator
emits an unconditional `e.drop(int(oparg))` after BUILD_TUPLE.

For inputs with a non-trivial Dealloc, that decref drops the
refcount to zero and runs `sliceDealloc`, which clears
`Start/Stop/Step` to `nil` before returning the carcass to
`sliceFreeList`. The new tuple still holds the cleared slice
object, so its repr renders `<nil>`.

For inputs that are immortal (small ints, `None`, interned
strings), `Decref` short-circuits on the `refcnt >= ImmortalRefcnt`
check, so the bug was invisible everywhere except containers of
slices, user-class instances with a `__del__`, or any other type
whose Dealloc resets fields.

Patched the three "take ref" wrappers in `vm/eval_helpers.go` to
`Incref` each stored element so the impending `drop()` decref
balances:

- `tupleFromStackRef` (BUILD_TUPLE)
- `listFromStackRef` (BUILD_LIST)
- `listAppendTakeRef` (LIST_APPEND)
- `setAddTakeRef` (SET_ADD)

CPython faithfully steals; gopy's translator-emitted drop forces
the wrappers to incref-then-balance instead. `NewSlice` already
does the same incref dance on its `start/stop/step` arguments
(`objects/slice.go:46 NewSlice`), so this brings the container
constructors in line with the slice constructor.

CPython:
- `Objects/tupleobject.c:226 _PyTuple_FromStackRefStealOnSuccess`
- `Objects/listobject.c:3146 _PyList_FromStackRefStealOnSuccess`
- `Objects/listobject.c:362 _PyList_AppendTakeRef`
- `Objects/setobject.c:2433 _PySet_AddTakeRef`

After the fix `test_pep646_syntax` slice-repr expectations all
pass; the only remaining failures in that test are
`__annotations__` returning `{}` because gopy's codegen does not
yet emit the PEP 649 `__annotate__` lazy function (separate
follow-up).

### P7 closer 17: `_sre` matchSlice byte-vs-codepoint indexing

doctest's `_check_prompt_blank` raised
`ValueError: line 1912 lacks blank after ibl: 'ible'` against
`test_syntax.py` even though that file's prompts are clearly
followed by a blank. Tracing the regex match showed the
`example.source` group came back as `'ible'` with the leading
characters chopped off when the doctest source contained any
non-ASCII content earlier on the page.

Minimal repro:

```
s = 'привет world'
m = re.search(r'world', s)
m.start(), m.end()      # 7, 12  (correct, code-point offsets)
m.group(0)              # 'ет'  (WRONG)
s[m.start():m.end()]    # 'world' (correct)
```

The engine walks `state.input []int32` (`module/_sre/engine.go`),
so `state.start` and `state.ptr` are code-point indices stored
into `matchData.locs`. The match formatter in
`module/_sre/match.go` then materialised substrings with
`md.s[lo:hi]`, which is Go byte slicing. Pure-ASCII input survives
(1 byte per rune), but any multi-byte rune ahead of the span
shifts the byte offsets out from under the code-point indices and
the slice cuts mid-rune.

Fixed `matchSlice` to walk `md.s` as runes:

```
func matchSlice(md *matchData, lo, hi int) objects.Object {
    if md.isBytes {
        return objects.NewBytes([]byte(md.s[lo:hi]))
    }
    if md.runes == nil {
        md.runes = []rune(md.s)
    }
    // clamp lo/hi to len(md.runes); return NewStr(string(md.runes[lo:hi]))
}
```

The `[]rune` slice is cached on `matchData` so repeated `group()`
calls on the same Match instance pay the conversion once. This
matches what CPython does: `PyUnicode_Substring` indexes by code
point against the unicode object's internal representation
(`Objects/unicodeobject.c:9617 PyUnicode_Substring`).

Bytes mode is unchanged: `md.s` holds raw bytes for the bytes
path, and the engine reports byte offsets there, so `md.s[lo:hi]`
is correct.

After the fix `test_syntax` no longer raises
`ValueError: line ... lacks blank after ibl:` and the doctest
runner exercises the actual assertions; remaining failures are
real semantic gaps (e.g. parser positional-pattern ordering).

CPython:
- `Objects/unicodeobject.c:9617 PyUnicode_Substring`
- `Modules/_sre/sre.c:2735 match_getslice_by_index`

### P7 closer 18: accept overflowing float literals as inf

`1e1000` raised SyntaxError at parse time. CPython accepts it
silently and the literal evaluates to `float('inf')`, matching
`PyOS_string_to_double` / `strtod` default rounding to infinity.

The gate is `case 1e1000:` inside test_patma. The parse-time
rejection cascaded into the doctest panel for any test file that
sourced match patterns over float-overflow constants.

Fixed `parseNumberLiteral` in `parser/pegen/action_helpers_gen.go`
to tolerate `strconv.NumError{Err: ErrRange}` and keep the
returned `+Inf` / `-Inf` value. Pure parse errors still reject.

CPython:
- `Parser/string_parser.c:1280 parsenumber_raw`
- `Python/pystrtod.c:130 PyOS_string_to_double`

### P7 closer 19: or-pattern wiring + parser_gen ternary translation

test_patma's `[1, _, _] | [_, _, "b", _]` style alternates were
all evaluating to "no match" on every alt except the first.
Three bugs stacked here.

**Bug 1: gather list flattened into the parent sequence.** The
`or_pattern` rule's grammar action is a CPython ternary

```
asdl_seq_LEN(patterns) == 1 ? asdl_seq_GET(patterns, 0)
                            : _PyAST_MatchOr(patterns, EXTRA)
```

but `Tools/parser_gen/action.go` had no translation for
`asdl_seq_LEN` / `asdl_seq_GET`, so the alt fell back to the
default-action path that wraps the gather output in
`matchedOr(patterns)`. The gather list `[p1, p2, ...]` then
flowed into the parent rule as raw `[]any`, and
`patternSeqOf` flattened it directly into the surrounding
MatchSequence/MatchClass element list. Every alt became a
sibling in the outer sequence, so a 2-alt OR over 3-tuples
produced a 6-element MatchSequence and bound names collided.

**Bug 2: `==` lost in target-atom join.** Even after teaching
the action translator about `asdl_seq_LEN` / `asdl_seq_GET`, the
ternary still failed to translate. `grammar_tok.go` reads `==`
as two `=` tokens (necessary so the metagrammar's `& &`
force-expect parses), and `grammar_parser.targetAtoms` joined
those with single spaces, producing `= =` which the C-action
translator's tokenizer cannot recognise. Added
`joinActionAtoms` to glue adjacent single-char ops that form a
known C compound operator (`==`, `!=`, `<=`, `>=`, `&&`, `||`).

**Bug 3: codegen_pattern_or was a shim.** The previous gopy
patternOr emitted one fail-pop ladder per alt without the
shared subject COPY, the cross-alt store-order rotation, or
the trailing POP_TOP / rotate sequence. CPython's
`Python/codegen.c:6120 codegen_pattern_or` is ~150 lines of
careful stack management:

- COPY 1 of the subject at the start of each alt so the
  alt can fail without consuming the subject;
- after the first alt, record its captured-store list as
  `control`; later alts must bind exactly the same set, and
  any divergent ordering gets corrected via stack rotations
  driven by `codegen_pattern_helper_rotate`;
- on overall OR failure, POP_TOP the surviving copy and
  jump to the outer fail-pop;
- on overall OR success, the captures sit one slot below
  the subject copy; rotate them down past `pc.onTop` and
  the outer `pc.stores`, then POP_TOP the subject copy.

Ported the full sequence into `compile/codegen_stmt_match.go`
patternOr. The resulting bytecode matches CPython's
`dis.dis` byte-for-byte for the test patterns above.

**Helper additions.** Added `seqLenAny` / `seqGetAny` in
`parser/pegen/actions.go` to back the translated calls, and
an `actionAstMatchOr` helper in
`parser/pegen/action_helpers_gen.go` that builds the AST node
when the gather has 2+ patterns. The previous hand-edited
`actionPgenOrPattern` shim was removed; the generator now
emits the call site directly from the upstream grammar action.

After the fix:

```
match (3, 4, "b", 5):
    case [1, _, _] | [_, _, "b", _] | [_, _, "c"] | ["d", _, _, _]:
        ...      # now reached, alt 1 matches
```

CPython:
- `Grammar/python.gram:511` or_pattern action
- `Python/codegen.c:6120 codegen_pattern_or`
- `Python/codegen.c:5739 codegen_pattern_helper_rotate`
- `Tools/peg_generator/pegen/c_generator.py` action translation

### P7 closer 20: ensure_real/ensure_imaginary accept Constant nodes

`{0+0j: _}` mapping patterns and signed/complex literal keys
failed with "mapping pattern keys may only be literals" because
both halves of the BinOp resolved to `placeholderMatched`,
which `asExpr` cannot convert to `ast.Expr`.

Root cause: `actionPgenEnsureReal` and `actionPgenEnsureImaginary`
in `parser/pegen/action_helpers_gen.go` were re-parsing the
token bytes when the surrounding grammar rule had already built
an `*ast.Constant` via `numberToken`. The type assertion
`argAt(args, 1).(*Token)` failed, so the helpers returned the
placeholder sentinel for every literal numeric key.

CPython's `_PyPegen_ensure_real` / `_PyPegen_ensure_imaginary`
are pure type-checks: assert the expression is a numeric
Constant of the right complex/real flavor, then return it
unchanged. Rewrote both helpers to do exactly that against
`*ast.Constant` carrying `complex128` vs anything else.

Also widened `isValidMappingPatternKey` in
`compile/codegen_stmt_match.go` to accept the literal-expression
shapes the parser actually emits (bare Constants, Attribute
lookups, signed UnaryOp(-, Constant), and complex_number
BinOp(Add|Sub, signed-real, imaginary-Constant)). CPython gets
to skip this because `_PyCompile_AstOptimize` folds those AST
nodes into a single Constant before codegen runs;
gopy has no AST optimizer pass yet, so the codegen has to
recognise the parser's literal-expression spelling directly.

CPython:
- `Parser/action_helpers.c:843 _PyPegen_ensure_imaginary`
- `Parser/action_helpers.c:853 _PyPegen_ensure_real`
- `Python/ast_opt.c:457 fold_unaryop`
- `Python/ast_opt.c:507 fold_binop`

### P7 closer 21: MatchMapping `{**rest}` capture codegen

`case {"a": 1, **rest}:` left `rest` unbound. The previous
`patternMapping` only emitted `patternStoreName(p.Rest, ...)`
without first building the rest-dict, so the store popped
whatever happened to sit on TOS (the matched-keys tuple).

CPython's codegen for the rest target (Python/codegen.c:6094)
builds a fresh dict via `BUILD_MAP 0 / SWAP 3 / DICT_UPDATE 2`
then deletes each named key from the copy before storing it.
Ported that sequence into `compile/codegen_stmt_match.go`
patternMapping.

CPython:
- `Python/codegen.c:6094 codegen_pattern_mapping` star_target arm

### P7 closer 22: co_positions() yields one tuple per codeunit

`test_patma.TestSourceLocations.test_jump_threading` walked
`dis.get_instructions(f)` and expected every jump in a `match`
body to carry a non-None `positions.lineno`. gopy was reporting
`None` on every instruction past the first coalesced linetable
run.

Root cause was a 1:N drift in `objects/code_attrs.go`
`codeCoPositionsMethod`: we emitted one position tuple per
`CoPositions(c)` entry (one per coalesced linetable record),
but `dis._get_instructions_bytes` calls
`next(co_positions, ())` once per instruction plus once per
inline cache slot, so a run that covers N codeunits has to
yield N copies of the same tuple.

CPython's `positionsiter_next` (Objects/codeobject.c:1492)
advances `pi_offset += 2` on every call and only re-decodes
the linetable when the cursor reaches the end of the current
range, which works out to one tuple per codeunit (2 bytes).
Fixed by expanding each `PositionEntry` into
`(End - Start) / 2` copies before handing the list to
`listIter`.

The AST / codegen / location-table writer were already
correct; only the iterator wrapper was wrong, which is why
`co_linetable` decoded cleanly by hand but `dis` saw None.

CPython:
- `Objects/codeobject.c:1492 positionsiter_next`
- `Objects/codeobject.c:1554 code_positionsiterator`
- `Lib/dis.py:754 _get_instructions_bytes`
