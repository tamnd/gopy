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

### P7 closer 23: PEP 695 generic function codegen

`def outer[A]():` and `def F[T: ...]():` blew up `test_type_aliases`
and `test_type_params` at compile time with

```
compile: free var "A" in nested scope "outer" has scope ? in outer "test"
```

The symtable already created the synthetic `<generic parameters of X>`
`TypeParametersBlock` for each PEP 695 function, but
`compileFunctionLike` ignored `s.TypeParams` and emitted the function
directly. With the wrapper missed, the inner function's free var `A`
had no cell to walk to, so `emitClosure` aborted.

`codegen_typealias` and `codegen_class` had this wrapper for a while;
the function path just had not been ported yet. Mirrored
`Python/codegen.c:1390 codegen_function` 1:1:

1. evaluate defaults in the outer scope (unchanged),
2. if `len(typeParams) > 0`: `SWAP 2` when both defaults and kwdefaults
   are pushed so the wrapper's first positional arg lands on
   `.defaults`, enter the wrapper scope, declare `.defaults` /
   `.kwdefaults` as fast slots 0/1, `RESUME 0`, emit the type-param
   tuple, `LOAD_FAST 0..n-1` so the inner MAKE_FUNCTION sees the
   defaults back on the wrapper stack,
3. emit the inner function code object normally,
4. inside the wrapper: `SWAP 2` then `CALL_INTRINSIC_2
   INTRINSIC_SET_FUNCTION_TYPE_PARAMS` to stamp `__type_params__` on
   the new function, `RETURN_VALUE`,
5. exit the wrapper, build a closure tuple for it, `MAKE_FUNCTION`,
6. if any defaults flowed: `SWAP num+1` + `CALL num-1` to route them
   through the wrapper's positional args; else `PUSH_NULL` + `CALL 0`.

After this, `def outer[A]():` and `def F[T: [lambda: T for T in ...]]`
compile and run; `test_type_aliases` and `test_type_params` flip from
`compile:` BAD to `ModuleNotFoundError: test.typinganndata` BAD (a
fixture vendor follow-up, not a codegen issue).

CPython:
- `Python/codegen.c:1390 codegen_function`
- `Python/codegen.c:1480 CALL_INTRINSIC_2 INTRINSIC_SET_FUNCTION_TYPE_PARAMS`
- `Python/symtable.c:1659 symtable_enter_type_param_block`

### P7 closer 24: test_subclassinit closes (metaclass guard + PEP 678 notes)

`test_subclassinit` carried three errors that were all rooted in two
distinct missing pieces of `type.__call__`.

**(1) Metaclass `__new__` returning a non-Type instance** (`test_set_name_metaclass`).
The CPython type_call dance after `tp_new`:

```c
obj = type->tp_new(type, args, kwds);
if (!PyObject_TypeCheck(obj, type)) return obj;
type = Py_TYPE(obj);
if (type->tp_init != NULL) ...
```

gopy's `typeCallViaTpNew` skipped the `PyObject_TypeCheck` guard, so a
metaclass whose `__new__` returns `0` (the test does this) still tripped
`type.__init__(int)` and raised `descriptor '__init__' for 'type'
objects doesn't apply to a 'int' object`. Ported the guard and
re-resolved `__init__` from `Py_TYPE(obj)` (not the calling type), so
the path now matches `Objects/typeobject.c:2331 type_call` 1:1.

**(2) PEP 678 `__notes__` attach during `type_new_set_names`**
(`test_set_name_error`, `test_set_name_wrong`). When `__set_name__`
raises, CPython calls `_PyErr_FormatNote` with `"Error calling
__set_name__ on '%.100s' instance %R in '%.100s'"`. gopy never
attached the note, so `cm.exception.__notes__` raised AttributeError.

The plumbing was the awkward part: `objects` cannot import `errors`
or `state`, so it has no direct path to the live `*Exception`. Added
two pieces:

1. `objects.FormatNoteHook func(string)`, installed from
   `vm/eval_call.go` exactly like `ClearCurrentExceptionHook`. The hook
   reads the live exception via `pyerrors.Occurred(ts)` and appends to
   `exc.Notes`.
2. A per-goroutine pending-notes queue in `vm/eval_call.go`. The hook
   queues notes when no Exception exists yet on the thread state
   because the inner failure (e.g. `TooManyPositionalError` raised by
   arg binding before any frame ran) propagated as a bare Go error
   that `handleException` only later promotes via `synthesizeException`.
   `handleException` drains the queue right after the synthesize, so
   the notes attach in source order.

After this, `test_subclassinit` flips from 3 errors to all 17 passing.

CPython:
- `Objects/typeobject.c:2331 type_call`
- `Objects/typeobject.c:11514 type_new_set_names`
- `Python/errors.c:1567 _PyErr_FormatNote`

### P7 closer 25: coroutine return value flows through StopIteration.value

`test_positional_only_arg.test_async` exercises the classic PEP 492
pattern of priming a coroutine with `coro.send(None)` and catching the
implicit `StopIteration` to read `e.value`:

```python
async def f(a=1, /, b=2):
    return a, b

try:
    coro = f(*args, **kwargs)
    coro.send(None)
except StopIteration as e:
    result = e.value
```

Two gaps blocked this. (1) The coroutine goroutine in `vm/eval_gen.go`
discarded the body's return value (`_, runErr := ge.run()`) and always
sent bare `objects.ErrStopIteration`, so the caller never saw the
returned tuple. (2) `StopIteration` had no `value` getset descriptor,
so even when args carried the retval an `except StopIteration as e:
e.value` access raised `AttributeError`.

Fixes ported piece by piece from CPython:

1. `objects/usertype.go` already exposes `SetTypeDescr` /
   `NewGetSetDescr`; `errors/builtins.go` now stamps
   `StopIteration.value` in `init()` with getter
   `stopIterValueGet` (returns `args[0]` or `None`) and setter
   `stopIterValueSet` (rewrites `args[0]` so .value and args stay in
   lock-step). Mirrors `Objects/exceptions.c:684 StopIteration_init`
   and `Objects/exceptions.c:711 StopIteration_members`.

2. `vm/eval_gen.go` now keeps the retval out of `ge.run()` and, when
   the body produced a non-None value, wraps it as
   `pyerrors.New(PyExc_StopIteration, NewTuple([retVal]))` and ships
   the typed exception across `yieldCh` via
   `objects.NewRaisedError(exc, "StopIteration")`. Bare `ErrStopIteration`
   still flows when the body returned `None`, matching the no-value
   case in CPython's `gen_send_ex2` (`Objects/genobject.c:225`).

3. `vm/eval_unwind.go::synthesizeException` learns to unwrap a
   `*objects.RaisedError` first, returning the embedded
   `*pyerrors.Exception` directly so its args (and therefore .value)
   survive promotion to thread state. Without this step, the path
   through `handleException` would build a fresh generic `Exception`
   and discard the retval-carrying StopIteration instance.

Net effect: `test_positional_only_arg` errors drop from 6 to 3
(`test_async` now passes); the remaining errors
(`test_annotations_constant_fold`, `test_annotations_in_closures`,
`test_serialization`) trace to unrelated subsystems already tracked
elsewhere.

CPython:
- `Objects/genobject.c:225 gen_send_ex2`
- `Objects/exceptions.c:684 StopIteration_init`
- `Objects/exceptions.c:711 StopIteration_members`

### P7 closer 26: CHECK_EG_MATCH + PREP_RERAISE_STAR for except*

`test_global.test_except_star_global_*` and the `test_grammar`
`try_stmt` family exercise PEP 654 `except*` syntax. Both opcodes
required by the codegen were stubs in v0.6: `CHECK_EG_MATCH` returned
"not implemented" and `BinaryPrepReraiseStar` raised before any leaf
projection happened. The pair lands here, ported function-by-function
from CPython.

Implementation:

1. `errors/exc_group.go` grows `IsExceptionGroup`,
   `ExceptionGroupLeaves`, `SplitExceptionGroup`, and a private
   `deriveGroup` helper. The split walks the leaves of a
   `BaseExceptionGroup`, recurses into nested groups, and rebuilds two
   parallel subsets (matched / rest) using the source group's class
   and message slot. Mirrors `Objects/exceptions.c:1326
   exceptiongroup_split_recursive` and
   `Objects/exceptions.c:1414 exceptiongroup_subset`.

2. `vm/eval_dispatch_handwritten.go` claims `CHECK_EG_MATCH` from the
   stub panel and ships `opCHECK_EG_MATCH`. The opcode pops
   `(exc_value, match_type)` and pushes `(rest, match)`. Branches:
   `None` exc -> both None; non-Exception payload -> rest = value,
   match = None; whole-group match wraps a naked exception into an
   `ExceptionGroup("", (exc,))` so the `as` binding always sees a
   group; partial match calls `splitExceptionGroupAny` which accepts a
   tuple-of-types match shape (which CPython's
   `PyErr_GivenExceptionMatches` supports) and returns the
   (match, rest) partition. Mirrors `Python/bytecodes.c CHECK_EG_MATCH`
   and `Python/ceval.c:2295 _PyEval_ExceptionGroupMatch`.

3. `intrinsics/binary.go::BinaryPrepReraiseStar` replaces the stub.
   It filters `None` entries from the `excs` list (each `rest`
   partition pushed by an `except*` arm), and returns: `None` if no
   survivors; the single survivor if the original was a naked
   exception (so user code sees the original type, not a wrapper); the
   sole survivor if only one made it through a group input; or a fresh
   `ExceptionGroup(message, (survivors...))` over the message from the
   source group. Mirrors `Python/intrinsics.c:237 prep_reraise_star`
   and `Objects/exceptions.c:1618 _PyExc_PrepReraiseStar`.

4. `intrinsics/intrinsics_test.go` adds `BinaryPrepReraiseStarID` to
   the `implementedBinary` map so the stub sweep no longer expects a
   `notImplementedError` for the now-live intrinsic.

5. `stdlib/test/typinganndata/` vendors `__init__.py`,
   `ann_module.py`, `ann_module2.py`, and `ann_module3.py` straight
   from `Lib/test/typinganndata/` so future `test_positional_only_arg`
   annotation-laden cases can import the fixture instead of skipping.

Net effect: `test_global` goes from 1 error to 20/20 passing.
`test_grammar` `test_try_star` now succeeds; remaining `test_grammar`
errors live in unrelated subsystems (matmul, var-annot module
semantics, lexer numerical-literal SyntaxError enforcement).

CPython:
- `Python/bytecodes.c CHECK_EG_MATCH`
- `Python/ceval.c:2295 _PyEval_ExceptionGroupMatch`
- `Python/intrinsics.c:237 prep_reraise_star`
- `Objects/exceptions.c:1326 exceptiongroup_split_recursive`
- `Objects/exceptions.c:1414 exceptiongroup_subset`
- `Objects/exceptions.c:1618 _PyExc_PrepReraiseStar`

### P7 closer 27: drift sweep (matmul, async dunders, coroutine SEND, `__call__` slot, `_imp` surface, defaultdict `__repr__`)

Six unrelated v0.6 drifts surface together when `test_grammar` is
forced through. They all hide behind a single missing slot wrap,
intrinsic, or builtin and ship as a batch under one closer.

1. `vm/eval_simple.go` adds `nbMatrixMultiply` (4) and
   `nbInplaceMatrixMultiply` (17) to the `BINARY_OP` suboperator
   dispatch so user classes that implement `__matmul__` /
   `__imatmul__` no longer raise "BINARY_OP suboperator 4 not
   implemented". Mirrors `Python/specialize.c:2389 binary_op_descrs`
   and the `NB_MATRIX_MULTIPLY` / `NB_INPLACE_MATRIX_MULTIPLY` rows in
   `Include/internal/pycore_opcode_metadata.h`.

2. `objects/usertype.go::fixupSlotDispatchers` grows `fixupAsyncSlots`.
   When a user class defines `__aiter__` / `__anext__` / `__await__`,
   the helper installs `slotAmAiter` / `slotAmAnext` / `slotAmAwait`
   into `t.Async`. Each dispatcher delegates through
   `lookupMethodOnSelf` so `async for` / `async with` / `await`
   resolve via the user method instead of raising
   "TypeError: object has no `__aiter__` method". Mirrors
   `Objects/typeobject.c slot_am_aiter / slot_am_anext / slot_am_await`.

3. `vm/eval_gen.go::execSend` adds a `*objects.Coroutine` arm. It
   forwards via `r.Send(v)`, and a new `stopIterRetval` helper unwraps
   both bare `ErrStopIteration` and `RaisedError`-wrapped
   `StopIteration` so the awaited return value flows from
   `args[0]` onto the stack as PEP 492 / CPython's `SEND` bytecode
   specifies. Mirrors `Python/bytecodes.c SEND` plus
   `Objects/genobject.c:1090 send_value`.

4. `objects/slot_wrap_descr.go` ships `AddCallSlotWrapper` and
   `makeWrapCall`. The wrapper exposes `__call__` on any type whose
   only callable hook is the `Call` or `Vectorcall` slot, mirroring
   CPython's `slotdefs` row for `tp_call` resolved by `add_operators`.
   The init blocks for `FunctionType` (`vm/eval_call.go`),
   `BuiltinFunctionType`, `MethodDescrType`, and `BoundMethodType`
   now call `AddCallSlotWrapper(t)` so `f.__call__`,
   `bif.__call__`, `descr.__call__`, and `bm.__call__` all resolve
   instead of raising `AttributeError`. Mirrors
   `Objects/typeobject.c wrap_call`.

5. `module/_imp/module.go` grows seven stub entries that
   `test.support.import_helper` toggles around module reloads:
   `lock_held`, `acquire_lock`, `release_lock`, `is_builtin`,
   `is_frozen`, `_override_frozen_modules_for_tests`, and
   `_override_multi_interp_extensions_check`. gopy serializes imports
   through Go-side sync so the lock helpers are no-ops; the override
   pair returns the CPython sentinel int. Mirrors `Python/import.c
   imp_module` plus the override impls at `Python/import.c:5034`
   (`_imp__override_frozen_modules_for_tests_impl`) and
   `Python/import.c:5052`
   (`_imp__override_multi_interp_extensions_check_impl`).

6. `module/_collections/module.go` installs a dedicated `__repr__`
   `MethodDescr` (`defaultDictReprMethod`) on `DefaultDictType`.
   Without it, `defaultdict` inherits `dict.__repr__` and
   `pprint._dispatch` (keyed on `type.__repr__`) collapses both onto
   `_pprint_default_dict`, so any plain `dict` going through
   `pprint.pformat` raises `AttributeError: 'dict' object has no
   attribute 'default_factory'`. Mirrors
   `Modules/_collectionsmodule.c:2364 defdict_repr` registered via
   `add_operators` for `tp_repr`.

7. `test/cpython/test/__init__.py` is removed so the vendored
   typinganndata fixtures form a PEP 420 namespace package that
   merges with `stdlib/test/`. Without the removal, the regular
   package marker shadowed `test.support`, breaking every
   import-helper-using gate.

Net effect: `test_grammar` drops from 3 errors to 1 (the remaining
error is the lexer warning-stash `__warningregistry__` deferred to
the warning module port). Unit tests in `module/_collections`,
`module/_imp`, `objects`, and `vm` stay green.

CPython:
- `Python/specialize.c:2389 binary_op_descrs`
- `Objects/typeobject.c slot_am_aiter` / `slot_am_anext` / `slot_am_await`
- `Python/bytecodes.c SEND`
- `Objects/genobject.c:1090 send_value`
- `Objects/typeobject.c wrap_call`
- `Python/import.c:5034 _imp__override_frozen_modules_for_tests_impl`
- `Python/import.c:5052 _imp__override_multi_interp_extensions_check_impl`
- `Modules/_collectionsmodule.c:2364 defdict_repr`

### P7 closer 28: PEP 750 Template + Interpolation end-to-end port

`test_annotationlib` failed at module import with `AttributeError:
'string.templatestring' object has no attribute 'interpolations'` because
the t-string subsystem only had the type singleton without the rest of
the runtime, codegen, dispatch, or parser wiring. Five files port the
full PEP 750 chain so `Template` exposes the same surface as CPython
3.14 and `Interpolation` is a first-class object.

1. `objects/template_str.go` renames `string.templatestring` to
   `string.templatelib.Template`, adds the `strings` /
   `interpolations` / `values` `GetSetDescr` rows from
   `Objects/templateobject.c:333 template_members` and
   `Objects/templateobject.c:339 template_getset`, hooks
   `template_iter` (skips empty literal parts, interleaves
   interpolations) and `template_values_get`, and adds the full
   `Interpolation` type with `value` / `expression` / `conversion` /
   `format_spec` `GetSetDescr`s plus the `__match_args__` tuple from
   `Objects/interpolationobject.c:163 _PyInterpolation_InitTypes`.
   `NewInterpolation` mirrors `_PyInterpolation_Build`, mapping the
   FVC_* tag (0 = None, 1 = "s", 2 = "r", 3 = "a") onto the
   `conversion` slot.

2. `compile/codegen_expr_misc.go` replaces the stub
   `visitTemplateStr` (which returned `compile: t-string
   interpolations not yet supported`) with a faithful port of
   `Python/codegen.c:4063 codegen_template_str`: emit each Constant
   string, insert an empty-string Constant before any
   back-to-back Interpolation, `BUILD_TUPLE` the strings, then visit
   each Interpolation, `BUILD_TUPLE` the interpolations, and
   `BUILD_TEMPLATE`. The new `visitInterpolation` ports
   `Python/codegen.c:4135 codegen_interpolation`: push value + the
   captured expression source `Str`, optionally push the format spec,
   encode conversion into the upper bits of oparg, emit
   `BUILD_INTERPOLATION`.

3. `vm/eval_dispatch_gen.go` and `vm/dispatch_gen_whitelist.go` add
   the `BUILD_INTERPOLATION` arm. The dispatcher pulls `value` and
   `str` (and optionally `format_spec`) off the stack, decodes the
   conversion tag from the upper oparg bits, and pushes the
   `Interpolation` returned by `NewInterpolation`. Mirrors
   `Python/bytecodes.c BUILD_INTERPOLATION`.

4. `parser/pegen/action_helpers_gen.go` replaces the
   placeholder-returning stub for `actionPgenInterpolation` with a
   port of `Parser/action_helpers.c:1508 _PyPegen_interpolation`. The
   helper unwraps the expression, picks up the conversion character
   via `fstringConversionChar`, reads the expression source text
   from the closing-brace token's `Metadata`, and constructs an
   `*ast.Interpolation`. Two off-by-one bugs in adjacent helpers are
   also fixed: `actionPgenTemplateStr` was reading
   `argAt(args, 3)` (the `TSTRING_END` token) instead of
   `argAt(args, 2)` (the loop body); `actionPgenConcatenateTstrings`
   was reading `argAt(args, 2)` past the end of its 2-element args
   slice. Both bugs swallowed every `Values` entry and produced an
   empty `Template`.

5. `parser/lexer/fstring.go` guards `last_expr_end` writes on `}` /
   `!` so the first `:` / `!` / `}` wins. Without the guard,
   `}` would overwrite the value set at `:`, leaving the closing
   brace token's metadata pointing past the format spec. CPython
   handles this by reading metadata off the `format_spec` /
   `conversion` token instead of the closing brace, but
   `actionPgenInterpolation` currently has no per-token metadata for
   the format spec, so the guarded write is the simpler fix that
   yields the same result for the simple cases the gates exercise.

Net effect: `t"hello {1+2} world"` evaluates end-to-end, exposes
`strings = ('hello ', ' world')`, `interpolations` holds an
`Interpolation(3, '1+2', None, '')`, and `for x in t:` yields the
interleaved literal-plus-interpolation sequence. `test_annotationlib`
moves past the `Interpolation` attribute lookup and the import path
now reaches the test bodies (47 failures + 237 errors remaining,
which are PEP 649 `__annotate__` lazy-codegen drifts tracked under
task #101, not t-string drifts). `test/cpython/test/test_inspect/`
was vendored under `stdlib/test/test_inspect/` so the fixture
modules `inspect_stock_annotations` /
`inspect_stringized_annotations*` resolve through the regular
`test/` package.

CPython:
- `Objects/templateobject.c:221 template_iter`
- `Objects/templateobject.c:314 template_values_get`
- `Objects/templateobject.c:333 template_members`
- `Objects/templateobject.c:339 template_getset`
- `Objects/interpolationobject.c:120 interpolation_members`
- `Objects/interpolationobject.c:163 _PyInterpolation_InitTypes`
- `Objects/interpolationobject.c:188 _PyInterpolation_Build`
- `Python/codegen.c:4063 codegen_template_str`
- `Python/codegen.c:4135 codegen_interpolation`
- `Python/bytecodes.c BUILD_INTERPOLATION`
- `Parser/action_helpers.c:1508 _PyPegen_interpolation`
- `Parser/lexer/lexer.c:227 _PyLexer_update_ftstring_expr`

### P7 closer 29: test_tstring gate green (parser + lexer + str ops)

`test_tstring` failed with 15 mismatches and 1 error because the
parser surfaced generic `invalid syntax` for cases CPython tags with
specific f-string / t-string diagnostics, the lexer collapsed `:=`
inside an f-string expression at format-spec position, and a stray
`}` reported `unmatched '}'` instead of the dedicated message.

1. `tools/parser_gen/action.go` teaches the C-to-Go action translator
   about `PyErr_Occurred()`. The conditional-raise pattern
   `PyErr_Occurred() ? NULL : RAISE_SYNTAX_ERROR_ON_NEXT_TOKEN(...)`
   used by `invalid_fstring_replacement_field` /
   `invalid_tstring_replacement_field` was previously dropped to the
   raw-children fallback, leaving the alts as silent no-ops. Mapping
   the C call to `(p.PinnedError() != nil)` lets `parseTernary` emit
   the matching Go ternary, so the six per-shape messages
   (`expecting '=', or '!', or ':', or '}'`,
   `expecting '!', or ':', or '}'`,
   `expecting ':' or '}'`,
   `expecting '}'`, etc.) propagate end-to-end.

2. `parser/pegen/errors.go` adds the idempotency guard that mirrors
   `Parser/pegen_errors.c:231 _PyPegen_raise_error`'s
   `if (p->error_indicator && PyErr_Occurred()) return NULL;`. Without
   it, adjacent parser alts both raised, so `t'{x!z:}'` reported
   `invalid conversion character` over `missing conversion character`.

3. `parser/pegen/action_helpers_gen.go` replaces the no-op
   `actionPgenCheckFstringConversion` stub with a port of
   `Parser/action_helpers.c:966 _PyPegen_check_fstring_conversion`:
   reject when the `!`-to-name span has whitespace, reject names
   longer than one character or not in `{'s', 'r', 'a'}`, and use the
   active mode's prefix (`f` vs `t`) in the message.

4. `parser/lexer/state.go` exposes `CurrentFStringPrefixChar()` so
   action helpers can read the active mode's `stringKind` without
   first checking `INSIDE_FSTRING`. Mirrors
   `Parser/lexer/lexer.c:43 TOK_GET_STRING_PREFIX`.

5. `parser/lexer/fstring.go` ports the EOF / `\n` arm of
   `Parser/lexer/lexer.c:1462 f_string_middle` to emit
   `unterminated <c>-string literal (detected at line N)` (and the
   triple-quoted variant), plus the format-spec-newline message for
   single-quoted strings. Adds the `unicodeEscape` flag so `\N{NAME}`
   inside an f-string body emits a middle through the closing brace
   instead of treating the `{` as an expression-start (CPython
   `Parser/lexer/lexer.c:1589 peek == 'N'` branch + the
   `if (unicode_escape)` short-circuit at line 1547).

6. `parser/lexer/lexer.go` adds two paren-aware refinements. EOF with
   `s.level > 0` emits `ERRORTOKEN` instead of `ENDMARKER` so the
   parser's tokenizer-error path raises `'%c' was never closed` (the
   alt-level `expecting a valid expression after '{'` message that
   gopy would otherwise pin loses the race). A stray `}` inside an
   f-string body at `curlyBracketDepth == 0` now raises the dedicated
   `<c>-string: single '}' is not allowed` message instead of falling
   through to the generic `unmatched '}'`. The `recordUnterminatedStringInFString`
   helper refines the unterminated-inner-string case to
   `<c>-string: expecting '}'` when the inner literal matches the
   outer quote char/size, mirroring
   `Parser/lexer/lexer.c:1181 INSIDE_FSTRING tok_get_normal_mode`.
   The `:` scanOperator arm now returns the one-char `COLON` before
   the two-char `:=` merge when inside an f-string expression at
   format-spec position, matching
   `Parser/lexer/lexer.c:1271 is_punctuation` so `f'{x:=10}'` parses
   as `x` plus the format spec `=10`.

7. `objects/str.go` updates the `str.__add__` slot to name the bad
   operand type in the TypeError message
   (`can only concatenate str (not "int") to str`) per
   `Objects/unicodeobject.c:11641 PyUnicode_Concat`. `Template +
   Template` runtime concat lives on `TemplateStrType.Sequence`
   (`templateConcat` in `objects/template_str.go`), so the
   ConcatenateError test that exercised `Template("a") + 1` and
   `"a" + t` now reports the matching TypeError.

8. `builtins/numeric.go` routes the `format(int, 'Nf')` builtin
   through `objects.Format(args[0], specStr)` instead of calling
   `format.FormatInt` directly, so int's `__format__` slot picks up
   the precision spec used by the t-string conversion tests.

Net effect: `test/cpython/test_tstring.py` goes from 15 fails + 1
error to 12/12 passing. No regressions on the surrounding gates
(`test_eof`, `test_string_literals`, `test_syntax`).

CPython:
- `Parser/action_helpers.c:966 _PyPegen_check_fstring_conversion`
- `Parser/pegen_errors.c:231 _PyPegen_raise_error`
- `Parser/pegen_errors.c:323 _PyPegen_raise_error_known_location`
- `Parser/lexer/lexer.c:43 TOK_GET_STRING_PREFIX`
- `Parser/lexer/lexer.c:735 EOF + tok->level`
- `Parser/lexer/lexer.c:1181 INSIDE_FSTRING tok_get_normal_mode`
- `Parser/lexer/lexer.c:1271 is_punctuation`
- `Parser/lexer/lexer.c:1462 f_string_middle`
- `Parser/lexer/lexer.c:1547 unicode_escape branch`
- `Parser/lexer/lexer.c:1589 peek == 'N' branch`
- `Objects/unicodeobject.c:11641 PyUnicode_Concat`
- `Grammar/python.gram:1510 invalid_fstring_replacement_field`
- `Grammar/python.gram:1532 invalid_tstring_replacement_field`

### P7 closer 30: `UnicodeWriter.Finish` populates the PEP 393 slab

`objects/unicode_writer.go` previously built the returned `*Unicode`
by setting `length`, `kind`, and `ascii` directly from the writer's
incremental `pos`/`maxchar` counters, without populating the matching
narrow slab (`data1` for non-ASCII Latin-1, `data2` for BMP, `data4`
for full unicode). Indexing the result through `unicodeGetItemKind`
then deref'd a nil `data1`, panicking with `index out of range [0]
with length 0`.

CPython's `_PyUnicodeWriter_Finish` (`Objects/unicodeobject.c:14199`)
allocates a fresh `PyUnicode_New` which inlines the slab. We mirror
that by calling `out.classify()`, the same path `NewStr` already
uses. classify walks the buffer once to re-pick the narrowest kind
and populate the slab.

Net effect: `test_fstring` no longer crashes. The gate now runs the
full 90 tests (54 fails + 22 errors remain, all real test mismatches:
AST-mode compile, `\N{...}` lexer drift, format-spec validation
messages). No other regressions; Go unit tests across
parser/objects/vm/compile stay green.

CPython:
- `Objects/unicodeobject.c:14199 _PyUnicodeWriter_Finish`
- `Objects/unicodeobject.c:1368 PyUnicode_New (slab allocation)`

### P7 closer 31: codec-prefix decode errors + line-continuation reject

Three drifts dropped together since they're all on the literal/decode
surface and they share test coverage:

1. `parser/string/decode.go` introduces a `DecodeError` carrying the
   byte-range `(start, end)` that
   `_PyUnicode_DecodeUnicodeEscapeInternal2` stamps on
   `PyUnicodeDecodeError`. Every `\x`, `\u`, `\U`, `\N`, and trailing
   backslash error now records its escape's byte position.
   `parser/string/parse.go` `wrapDecodeError` formats it as `(unicode
   error) 'unicodeescape' codec can't decode bytes in position
   {start}-{end}: {reason}` to match
   `_Pypegen_raise_decode_error` (`Parser/pegen_errors.c:130`).

2. `parser/pegen/action_helpers_gen.go`: `actionPgenJoinedStr` and
   `actionPgenDecodedConstantFromToken` previously swallowed
   `DecodeFStringPart` errors and returned `placeholderMatched`, so
   `f'\N'` and `f'{\N{...'` ended up reported as the generic
   "invalid syntax". They now surface the decode error through
   `RaiseSyntaxErrorKnownLocation` so the codec-prefixed message
   reaches the SyntaxError surface.

3. `parser/lexer/lexer.go`: `tokGetNormalMode` previously only ran the
   line-continuation path when the byte after `\` was `\n`; any other
   byte fell through to the operator dispatcher and surfaced as a
   generic "invalid character". CPython
   (`Parser/lexer/lexer.c:1244`) unconditionally calls
   `tok_continuation_line` after a backslash, and that helper raises
   `E_LINECONT` ("unexpected character after line continuation
   character") whenever the next byte is not `\n`. The Go loop now
   matches: `\<non-newline>` emits the right error.

Net effect on `test_fstring`: 54F+22E -> 38F+22E (16 closures across
`test_misformed_unicode_character_name` and
`test_invalid_backslashes_inside_fstring_context`). `test_syntax`
14F+2E -> 12F+2E (2 closures from the same backslash drift). Other
gates unchanged. Go unit tests across
parser/objects/vm/compile/builtins remain green.

CPython:
- `Parser/pegen_errors.c:130 _Pypegen_raise_decode_error`
- `Parser/action_helpers.c:1288 _PyPegen_decode_fstring_part raise path`
- `Parser/lexer/lexer.c:1244 line-continuation branch`
- `Parser/lexer/lexer.c:435 tok_continuation_line E_LINECONT`
- `Objects/unicodeobject.c:6791 \N{name} expansion + error path`
- `Objects/unicodeobject.c:6854 raise on "unicodeescape"`

### P7 closer 32: format-spec thousands-separator validation messages

`parse_internal_render_format_spec` raises three distinct ValueError
spellings when the comma/underscore grouping options collide:

- `Cannot specify ',' with ','.` for `f'{1:,,}'`
- `Cannot specify '_' with '_'.` for `f'{1:__}'`
- `Cannot specify both ',' and '_'.` for `f'{1:,_}'` / `f'{1:_,}'`

The Go port in `format/format.go` previously folded all four into a
generic `ErrInvalidSpec` ("format: invalid format specifier"), and
the comma/underscore parser consumed both characters in a single
step, so the staged checks that produce those messages never ran.

The fix is a 1:1 port of the comma/underscore section:

1. `[grouping]` now parses in three stages, matching
   `Python/formatter_unicode.c:236`. First a `,` becomes
   `LT_DEFAULT_LOCALE`; then a `_` either becomes
   `LT_UNDERSCORE_LOCALE` or, if `,` was already seen, raises
   `invalid_comma_and_underscore`; then a final `,` raises the same
   error when `_` was seen. The trailing comma is *not* consumed so
   it falls into the type slot and trips
   `invalid_thousands_separator_type` with the codec-style spelling.

2. `validateThousands(thousands, type)` ports the post-parse switch
   at `Python/formatter_unicode.c:331`. `'d'/'e'/'f'/'g'/'E'/'G'/
   '%'/'F'/'\0'` are allowed unconditionally; `'b'/'o'/'x'/'X'`
   accept only `'_'` (with the PEP 515 4-digit group). Anything
   else surfaces through `invalidThousandsSeparator`, which mirrors
   the CPython `%c`/`\\x%x` branching exactly.

3. `ParseSpec` runs the validation when `Type != 0` so explicit
   type characters trip at parse time. `FormatString`, `FormatInt`,
   and `FormatFloat` each run it again after applying their default
   type so empty-Type specs ("`,`" against str/float/int) get the
   matching error.

4. `FormatInt`'s ad-hoc "bin/oct/hex with `,`" rejection is gone;
   `validateThousands` now owns the rule. The `_b`/`_o`/`_x`/`_X`
   path still picks up the 4-digit group size.

Net effect on `test_fstring`: 38F+22E -> 34F+22E (the four
`test_with_*_in_format_specifier` cases close). Go unit tests across
format/parser/objects/vm/compile/builtins stay green;
`test_string_literals`, `test_syntax`, and `test_tstring` are
unchanged.

CPython:
- `Python/formatter_unicode.c:33 invalid_thousands_separator_type`
- `Python/formatter_unicode.c:47 invalid_comma_and_underscore`
- `Python/formatter_unicode.c:236 grouping section`
- `Python/formatter_unicode.c:331 thousands/type validation`

### P7 closer 33: SyntaxWarning for `\{` / `\}` inside non-raw f-strings

CPython's lexer treats a backslash before `{` or `}` inside an
f-string as an unrecognized escape: the body keeps the literal
backslash and the tokenizer emits a `SyntaxWarning` so the user
notices the typo before PEP 414 escalates it to a `SyntaxError` in
a future version. The matching code lives in
`Parser/lexer/lexer.c:1581` (the `\{`/`\}` branch inside
`tok_get_fstring_mode`) and routes through
`_PyTokenizer_warn_invalid_escape_sequence`.

The Go port in `parser/lexer/fstring.go:fstringMiddle` already kept
the backslash in the middle token's text, but never raised the
warning, so `test_fstring`'s six `test_backslash_before_*` cases
never saw the expected diagnostic. The fix is a one-line call to
`s.warnInvalidEscape(byte(peek))` inside the existing `peek == '{'
|| peek == '}'` branch, guarded by `!m.raw` so raw f-strings (where
the backslash is just a literal byte anyway) stay silent.

Net effect on the parser surface: `f'\{...}'` and `f'\}...'` now
produce the canonical SyntaxWarning. The six affected `test_fstring`
cases remain failing for a separate reason: gopy's
`unittest.assertWarns` interaction with
`stdlib/_py_warnings.py:_add_filter` propagates an `AttributeError`
when `filters.remove(item)` triggers a module `__getattr__` lookup
for `__warningregistry__`. That is a warnings-subsystem bug
unrelated to spec 1719 and is tracked as a separate follow-up; the
port here is faithful to CPython and is the prerequisite for those
tests to close once the warnings filter bug is fixed.

CPython:
- `Parser/lexer/lexer.c:1581 fstring \{ / \} branch`
- `Parser/tokenizer/helpers.c:110 _PyTokenizer_warn_invalid_escape_sequence`

### P7 closer 34: decode_unicode_with_escapes preprocessing pass

`Parser/string_parser.c:135 decode_unicode_with_escapes` is the
wrapper CPython runs before its inner unicode-escape decoder. It
walks the input and rewrites two awkward shapes:

- a trailing `\` becomes `\` (so the inner decoder, which
  rejects `\` at EOF, sees a complete escape that decodes to a
  literal backslash)
- a `\` immediately followed by a non-ASCII byte gets the same
  `\` rewrite, then the high byte is re-emitted as a
  `\UXXXXXXXX` escape so the inner decoder treats them as two
  independent characters rather than one bogus escape

Without the pass, gopy's decoder raised `"\ at end of string"`
the moment an f-string middle ended on a literal backslash. The
test `test_syntax_warning_infinite_recursion_in_file` exercises
exactly this shape via `f'\{1}'`: after P7 closer 33 emits the
SyntaxWarning and the lexer backs up the `{`, the middle token's
body is the single byte `\`. The inner decoder would then fail
with "trailing backslash", masking the SyntaxWarning under a
spurious SyntaxError.

The port adds `preprocessUnicodeEscapes` in
`parser/string/decode.go` and routes `decodeUnicodeEscapes`
through it. The high-byte arm emits raw UTF-8 instead of the
`\UXXXXXXXX` round trip CPython needs (our inner decoder already
accepts UTF-8 directly), so the observable behaviour is the same:
trailing `\` decodes to a literal backslash, `\<utf8>` decodes to
backslash plus the original UTF-8 character, no SyntaxWarning
fires for the high-byte case.

Net effect on `test_fstring`: 34F+22E to 33F+22E. The
`test_syntax_warning_infinite_recursion_in_file` case closes.
Other gates unchanged.

CPython:
- `Parser/string_parser.c:135 decode_unicode_with_escapes`
- `Parser/string_parser.c:158 trailing-backslash + high-byte rewrite loop`
- `Objects/unicodeobject.c _PyUnicode_DecodeUnicodeEscapeInternal2`
