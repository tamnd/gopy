---
format: md
id: 1710_v0124_lexer_tokenizer_full_port
title: "1710. v0.12.4 lexer/tokenizer full port"
sidebar_label: "1710. v0.12.4 lexer/tokenizer"
sidebar_position: 1710
slug: /specs/1710-v0124-lexer-tokenizer
description: "Port every CPython 3.14 lexer/tokenizer source file (Parser/lexer/, Parser/tokenizer/, Python/Python-tokenize.c, Lib/{tokenize,keyword,tabnanny}.py) into gopy, then gate on the five Lib/test/test_* files the 1700 spec assigns to this subsystem."
---

## Checklist

Sources to fully port (CPython 3.14):

- [ ] `Parser/lexer/buffer.c` (76 lines) → `parser/lexer/buffer.go`
- [ ] `Parser/lexer/lexer.c` (1635 lines) → `parser/lexer/lexer.go`
- [ ] `Parser/lexer/state.c` (151 lines) → `parser/lexer/state.go`
- [ ] `Parser/tokenizer/helpers.c` (581 lines) → `parser/lexer/helpers.go`
- [ ] `Parser/tokenizer/file_tokenizer.c` (493 lines) → `parser/lexer/driver_file.go`
- [ ] `Parser/tokenizer/readline_tokenizer.c` (134 lines) → `parser/lexer/driver_readline.go`
- [ ] `Parser/tokenizer/string_tokenizer.c` (148 lines) → `parser/lexer/driver_string.go`
- [ ] `Parser/tokenizer/utf8_tokenizer.c` (55 lines) → `parser/lexer/driver_string.go` (utf-8 path)
- [ ] `Python/Python-tokenize.c` → `module/_tokenize/` (replaces the stub `TokenizerIter`)
- [ ] `Lib/keyword.py` (64 lines) → `module/keyword/` (vendor verbatim)
- [ ] `Lib/tokenize.py` (598 lines) → `module/tokenize/` (vendor verbatim)
- [ ] `Lib/tabnanny.py` (338 lines) → `module/tabnanny/` (vendor verbatim)

Gate tests to land green under `test/cpython/`:

- [x] `test_keyword.py` (56 lines) — vendored; 10/11 sub-tests green. The eleventh, `test_all_keywords_fail_to_be_used_as_names`, calls `compile(name + '=2', '<s>', 'single')` and hits a parser-generator gap (`parser: generated rule bodies not yet emitted`) unrelated to lexer/tokenizer. Tracked separately. Also vendored at `stdtest/test_keyword.py` and gated via `TestStdtestCorpus`.
- [ ] `test_utf8source.py` (41 lines) — vendored; suite now runs end-to-end after `_io.File.fileno()` / `_io.File.isatty()` / `os.isatty()` landed. 1/3 sub-tests green. The other two fail in unrelated subsystems: `compile()` rejecting `bytes` (compile panel) and a missing `test.tokenizedata.badsyntax_pep3120` fixture (test corpus). Both tracked separately.
- [ ] `test_source_encoding.py` (547 lines) — vendored; blocked on `import inspect` (stdlib not yet vendored).
- [ ] `test_tabnanny.py` (354 lines) — vendored; blocked on `import asyncio` via `unittest.mock` (stdlib not yet vendored).
- [ ] `test_tokenize.py` (3480 lines) — vendored; blocked on `import tempfile` (stdlib not yet vendored).

## Goal

Replace the partial lexer/tokenizer port that grew up alongside the
v0.5.5 parser work with a one-to-one translation of every CPython 3.14
source file in the subsystem, then pin the result with the five
`Lib/test/test_*` files the 1700 spec already assigned to this panel.

Today `parser/lexer/lexer.go` is 633 lines against CPython's 1635-line
`Parser/lexer/lexer.c`. The delta is the gap this spec closes. The
v0.12.4 series treats every subsystem the same way: port full, then
gate on the upstream tests.

## Sources of truth

Lexer / tokenizer C sources (3.14):

| CPython file | Lines | gopy destination |
|--------------|------:|------------------|
| Parser/lexer/buffer.c | 76 | parser/lexer/buffer.go |
| Parser/lexer/lexer.c | 1635 | parser/lexer/lexer.go |
| Parser/lexer/state.c | 151 | parser/lexer/state.go |
| Parser/tokenizer/helpers.c | 581 | parser/lexer/helpers.go |
| Parser/tokenizer/file_tokenizer.c | 493 | parser/lexer/driver_file.go |
| Parser/tokenizer/readline_tokenizer.c | 134 | parser/lexer/driver_readline.go |
| Parser/tokenizer/string_tokenizer.c | 148 | parser/lexer/driver_string.go |
| Parser/tokenizer/utf8_tokenizer.c | 55 | parser/lexer/driver_string.go |
| Python/Python-tokenize.c | (see file) | module/_tokenize/ |

Python sources (3.14):

| CPython file | Lines | gopy destination |
|--------------|------:|------------------|
| Lib/keyword.py | 64 | module/keyword/ |
| Lib/tokenize.py | 598 | module/tokenize/ |
| Lib/tabnanny.py | 338 | module/tabnanny/ |

Gate tests live at `~/github/python/cpython/Lib/test/`:
`test_keyword.py`, `test_utf8source.py`, `test_source_encoding.py`,
`test_tabnanny.py`, `test_tokenize.py`.

## Workflow

The spec follows the durable port-not-patch / full-subsystem rule.
The work is broken into the phases below; each phase is one or more
PRs.

### Phase 1: audit + fill the C-tokenizer port

For every `Parser/lexer/*.c` and `Parser/tokenizer/*.c` function, find
the Go counterpart in `parser/lexer/`. Where a function is missing,
port it with a `// CPython: <file>:<line> <function>` citation. Where
a function is present but diverges from CPython, rewrite it to match.
The deliverable is parser/lexer Go LOC roughly matching the upstream C
LOC, with every CPython function accounted for.

### Phase 2: replace the `_tokenize` stub

`module/_tokenize/module.go` raises NotImplementedError on every call.
Port `Python/Python-tokenize.c` end-to-end: `TokenizerIter_Type`,
`tokenizeriter_new`, `tokenizeriter_next`, the helpers that materialize
`TokenInfo` tuples, and the readline / encoding plumbing.
`module/tokenize/` (next phase) drives this iterator directly.

### Phase 3: vendor `Lib/keyword.py`, `Lib/tokenize.py`, `Lib/tabnanny.py`

The Python layer is a verbatim vendoring under `module/keyword/`,
`module/tokenize/`, `module/tabnanny/` (following the standing rule:
"module ports under `module/`, name = CPython public name minus the
`py` prefix"). The Python files stay byte-equal to upstream so future
3.14.x point releases rebase via `git diff`.

### Phase 4: land the gate tests

For each of the five tests:

1. Copy the test file from `~/github/python/cpython/Lib/test/` into
   `test/cpython/` verbatim.
2. Run it through `test/regrtest`.
3. If green, mark the 1700 panel row done and move to the next.
4. If red, fix the divergence in `parser/lexer/`, `module/_tokenize/`,
   or the vendored Python file. Never edit the test.

### Phase 5: flip 1700

Once every gate is green, flip task #484 ("test e2e v0.5.5 — lexer
panel") to done and update the 1700 checklist row.

## Sub-system blockers (DFS)

The four pending gate rows each depend on a chain of sub-system gaps
outside the lexer/tokenizer scope. Closing 1710 means walking each
chain depth-first and porting whatever's missing until the gate runs
green. Status legend: DONE = landed and verified, WIP = in progress,
TODO = not started, BLOCKED = waiting on a larger sub-system spec.

### test_tokenize.py chain

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T1 | numbers/long | `int.__pow__(int, neg_int)` returns float; float `__pow__` slot wired | DONE | 5d9c85d |
| 2 | T1.5 | VM attr machinery | `AttrDictHolder` lets C-port subclasses carry an instance __dict__; `_random.RandomObject` opts in | DONE | 7d9e729 |
| 3 | T1.6 | `module/os` | bind `os.fsdecode` + `os.fsencode` on the inittab module | DONE | 9bd4675 |
| 4 | T1.7 | stdlib vendor | byte-equal `Lib/bisect.py` and `Lib/tempfile.py` under `stdlib/` | DONE | 4350edf |
| 5 | T6 | asyncio | `unittest.mock` imports `asyncio`; large sub-system, its own spec | BLOCKED | — |

### test_utf8source.py chain

Suite runs end-to-end; 1/3 sub-tests green. The remaining two fail in
unrelated sub-systems:

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T2 | builtin `compile()` | accept `bytes` / `bytearray` / `str` / AST instead of rejecting bytes | TODO | — |
| 2 | T3 | test fixtures | vendor `Lib/test/tokenizedata/` fixtures the panel tests reference (e.g. `badsyntax_pep3120`) | TODO | — |
| 3 | T4 | `module/sys` | bind `sys.exit` + `setrecursionlimit` + `getrecursionlimit` + `getrefcount` on the inittab sys module via `CurrentThreadHook` | DONE | 7e5bc6d |

### test_source_encoding.py chain

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T5 | stdlib vendor | vendor `Lib/inspect.py` (3409 lines) verbatim plus any new deps | TODO | — |

### test_tabnanny.py chain

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T6 | asyncio | port the asyncio package (event loop, transports, protocols, futures, tasks, streams, subprocess, queues, locks) as its own spec | BLOCKED | — |

DFS execution order, smallest fix first: T1 → T1.5 → T1.6 → T1.7 → T4
→ T2 → T3 → T5 → T6. Each task gets its own commit and an entry in
`stdtest/MANIFEST.txt` when the gate it unblocks lands green.

## Out of scope

- `tokenizedata/` test fixtures under `Lib/test/tokenizedata/` are
  in scope only as far as the five gate tests reference them.
- IDLE's tokenizer fork (`Lib/idlelib/`) stays out of scope; IDLE is
  on the 1700 deferred list.
- The PEG parser layer that consumes tokens (`Parser/parser.c` and
  friends) is a separate subsystem and gets its own v0.12.4 spec
  when its turn comes.
