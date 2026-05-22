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

All 27 top-level tests live in `/Users/apple/cpython-314/Lib/test/`.
They will be vendored unchanged into `test/cpython/` with no edits
beyond what `make vendor-tests` does today.

| Test | Upstream LOC | Spec 1700 mark | Owner subsystem |
|------|-------------:|----------------|-----------------|
| test_grammar              |   ? | done   | Parser/parser.c (every rule) |
| test_syntax               |   ? | ready  | Parser/pegen_errors.c |
| test_eof                  |   ? | ready  | Parser/pegen.c continuation handling |
| test_fstring              |   ? | ready  | Parser/string_parser.c f-string arm |
| test_tstring              |   ? | ready  | Parser/string_parser.c triple-quoted |
| test_named_expressions    |   ? | ready  | Parser walrus + compile codegen |
| test_unparse              |   ? | ready  | Lib/_ast_unparse.py |
| test_asdl_parser          |   ? | ready  | Parser/asdl.py + Parser/asdl_c.py |
| test_pep646_syntax        |   ? | ready  | Parser star-expansion |
| test_type_params          |   ? | ready  | PEP 695 type parameter grammar |
| test_type_aliases         |   ? | ready  | `type X = Y` statement |
| test_type_annotations     |   ? | ready  | Annotation lowering |
| test_type_comments        |   ? | ready  | Parser `type_comments=True` mode |
| test_annotationlib        |   ? | ready  | Lib/annotationlib.py (lazy eval) |
| test_string_literals      |   ? | ready  | Parser/string_parser.c escape paths |
| test_int_literal          |   ? | ready  | Number lexer + `int.__new__` |
| test_unicode_identifiers  |   ? | ready  | NFKC identifier normalization |
| test_patma                |   ? | ready  | PEP 634 pattern matching |
| test_decorators           |   ? | ready  | Decorator AST + codegen |
| test_global               |   ? | ready  | symtable global/nonlocal pass |
| test_scope                |   ? | ready  | symtable scoping rules |
| test_keywordonlyarg       |   ? | ready  | Parser kw-only args |
| test_positional_only_arg  |   ? | ready  | Parser pos-only args |
| test_unpack               |   ? | ready  | Tuple unpack codegen |
| test_unpack_ex            |   ? | ready  | Starred unpack |
| test_metaclass            |   ? | ready  | `__build_class__` + type creation |
| test_subclassinit         |   ? | ready  | `__init_subclass__` hook |

Plus the three packages:

| Package | Files | Mark | Owner subsystem |
|---------|-------|------|-----------------|
| `test_ast/`           | `__init__.py`, `test_ast.py`, `snippets.py`, `utils.py`, `data/` | ready | Python/ast.c validator + Lib/ast.py |
| `test_future_stmt/`   | 9 files (see C section below) | ready | `__future__` flag plumbing in parser + compile |
| `test_peg_generator/` | 6 files (see C section below) | deferred | `Tools/peg_generator/`; deferred until generator port |

`test_grammar` is the only row already marked `done` (v0.10.2 release
notes pinned it). Re-run on top of the lexer panel close and confirm
it stays green; if not, treat it as red and re-port the failing rules.

LOC column will be filled in alongside the vendoring commit so we have
a sizing signal before opening each closer PR.

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
- [ ] P1: vendor 27 top-level tests + 3 packages, record baseline counts
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
