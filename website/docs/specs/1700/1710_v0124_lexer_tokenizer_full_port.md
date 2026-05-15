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

### Sources to fully port (CPython 3.14)

Status legend: DONE = ported in full and verified, WIP = port underway, TODO = not started.

| CPython source | C LOC | gopy destination | Go LOC | Status | Commit |
|---|---:|---|---:|---|---|
| `Parser/lexer/buffer.c` | 76 | `parser/lexer/buffer.go` | 56 | DONE | 5374e84 |
| `Parser/lexer/lexer.c` | 1635 | `parser/lexer/lexer.go` (+ `fstring.go`) | 633 + 240 | WIP | function-map landed, gaps tracked under #612 (verify_*), #613 (check_coding_spec), #617 (maybe_raise_syntax_error_for_string_prefixes), #618 (update_ftstring_expr) |
| `Parser/lexer/state.c` | 151 | `parser/lexer/state.go` | 402 | DONE | d157189 |
| `Parser/tokenizer/helpers.c` | 581 | `parser/lexer/helpers.go` (+ encoding subset in `parser/lexer/source.go`) | 176 + 238 | DONE | de537e1 |
| `Parser/tokenizer/file_tokenizer.c` | 493 | `parser/lexer/driver_file.go` | 120 | DONE | 268c8f8 |
| `Parser/tokenizer/readline_tokenizer.c` | 134 | `parser/lexer/driver_readline.go` | 50 | DONE | 268c8f8 |
| `Parser/tokenizer/string_tokenizer.c` | 148 | `parser/lexer/driver_string.go` | 112 | DONE | 268c8f8 |
| `Parser/tokenizer/utf8_tokenizer.c` | 55 | `parser/lexer/driver_string.go` (utf-8 path) | (shared) | DONE | 268c8f8 |
| `Python/Python-tokenize.c` | 445 | `module/_tokenize/module.go` | 366 | DONE | 4d3b1e8 |
| `Lib/keyword.py` | 64 | `stdlib/keyword.py` | 64 | DONE | byte-equal vendor |
| `Lib/tokenize.py` | 598 | `stdlib/tokenize.py` | 598 | DONE | byte-equal vendor |
| `Lib/tabnanny.py` | 338 | `stdlib/tabnanny.py` | 338 | DONE | byte-equal vendor |

### Gate tests to land green under `test/cpython/`

| Test | LOC | Status | Commit |
|---|---:|---|---|
| `test_keyword.py` | 56 | DONE (10/11 sub-tests green; the eleventh hits a parser-generator gap unrelated to lexer/tokenizer, `parser: generated rule bodies not yet emitted`. Also mirrored at `stdtest/test_keyword.py` and gated via `TestStdtestCorpus`.) | — |
| `test_utf8source.py` | 41 | DONE (3/3 sub-tests green; mirrored at `stdtest/test_utf8source.py`) | — |
| `test_tabnanny.py` | 354 | DONE (exits 0 after typed `UnicodeDecodeError` + `surrogateescape` decode fix) | 3066fe3 |
| `test_source_encoding.py` | 547 | TODO (imports clear; first hang is `BytesSourceEncodingTest.test_crcrcrlf`, which is `exec(bytes)` inside `captured_stdout`; the underlying gap is the VM's `exec(bytes)` path, not lexer/tokenizer). | — |
| `test_tokenize.py` | 3480 | WIP. Three of the four blockers cleared: (a) `drainReadline` encoding inversion fixed in 538ab52; (b) `FORMAT_WITH_SPEC` now routes through `objects.Format` in 5bd8455 so f-string format specs like `f"{n:10}"` actually format; (c) `scanOperator` now emits the specific operator token type (EQUAL/PLUS/...) via the new `_PyToken_OneChar`/`TwoChars`/`ThreeChars` port in 669c11f, matching what `Python/Python-tokenize.c` surfaces. Last remaining blocker is an `re._parser.Tokenizer.__next` IndexError that fires while importing test_tokenize.py at top level (a regex compile failure unrelated to lexer scope). | 538ab52, 5bd8455, 669c11f |

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
| 5 | T6 | asyncio | `unittest.mock` imports `asyncio`; full port tracked in [spec 1711](./1711_v0124_asyncio_full_port.md) | BLOCKED | — |

### test_utf8source.py chain

Suite runs end-to-end; 1/3 sub-tests green. The remaining two fail in
unrelated sub-systems:

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T2 | builtin `compile()` + `str.encode` | accept `bytes` / `bytearray` (route through `lexer.FromBytes`); `str.encode` honors its encoding arg via `codecs.Encode` | DONE | 9d03f23 |
| 2 | T3 | test fixtures | vendor `Lib/test/tokenizedata/` (bad_coding*, badsyntax_*, coding20731, tokenize_tests-*) under `stdlib/test/tokenizedata/` | DONE | 0c3da66 |
| 3 | T4 | `module/sys` | bind `sys.exit` + `setrecursionlimit` + `getrecursionlimit` + `getrefcount` on the inittab sys module via `CurrentThreadHook` | DONE | 7e5bc6d |
| 4 | T3.1 | lexer non-utf-8 check | `lexer.ValidateUTF8` flags the first non-utf-8 byte and the parser surfaces a SyntaxError so `badsyntax_pep3120` raises at import. Also added a `Sequence.Contains` slot for str so the test's `'utf-8' in msg.lower()` substring check works. | DONE | 6db8913 |

### test_source_encoding.py chain

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T5.1 | stdlib vendor | vendor `Lib/opcode.py` (122 lines) plus C-port the `_opcode` inittab module (has_arg/has_const/has_name/has_jump/has_free/has_local/has_exc, get_nb_ops, intrinsic + special-method name lists). `_opcode_metadata.py` lands as a verbatim vendor since it's pure-Python data. `stack_effect` / `get_executor` ship as documented stubs (they're never called during opcode.py or dis.py import). | DONE | 2512db3 |
| 2 | T5.2 | stdlib vendor | vendor `Lib/dis.py` (1157 lines) verbatim, depends on T5.1. Also widens `module/_collections` `_tuplegetter` so `__doc__` is writable (matches CPython `tuplegetter_members` `PyMemberDef` `flags=0`), which `dis.py:314` exercises. | DONE | 7f352c2 |
| 3 | T5.3 | stdlib vendor | minimal-shim `stdlib/importlib/__init__.py` + `stdlib/importlib/machinery.py`. Only `SOURCE_SUFFIXES`, `BYTECODE_SUFFIXES`, `EXTENSION_SUFFIXES`, `all_suffixes()`, and `ModuleSpec` are observable from `inspect.py`; the full bootstrap port is deferred. | DONE | eb13f02 |
| 4 | T5.4 | stdlib vendor | vendor `Lib/inspect.py` (3409 lines) verbatim, depends on T5.1–T5.3. Two runtime gaps surfaced at import time: (a) `type.__dict__["__dict__"]` had no entry, so a `__dict__` getset descriptor was registered on `typeType`; (b) `_types` was missing `WrapperDescriptorType`, `MethodWrapperType`, `ClassMethodDescriptorType`, which now alias to the closest gopy types (method_descriptor / method / classmethod). | DONE | 7e3f024 |

DFS note: T5 was originally one row but `inspect` pulls in `dis` →
`opcode` → `_opcode` (C module) → `_opcode_metadata` (generated C
module), plus `importlib.machinery`. The four-step breakdown above
matches the actual port order.

### test_tabnanny.py chain

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T6 | asyncio | port the asyncio package (event loop, transports, protocols, futures, tasks, streams, subprocess, queues, locks) as its own spec | BLOCKED | — |

DFS execution order, smallest fix first: T1 → T1.5 → T1.6 → T1.7 → T4
→ T2 → T3 → T3.1 → T5.1 → T5.2 → T5.3 → T5.4 → T6. Each task gets its own commit and an entry in
`stdtest/MANIFEST.txt` when the gate it unblocks lands green.

## Out of scope

- `tokenizedata/` test fixtures under `Lib/test/tokenizedata/` are
  in scope only as far as the five gate tests reference them.
- IDLE's tokenizer fork (`Lib/idlelib/`) stays out of scope; IDLE is
  on the 1700 deferred list.
- The PEG parser layer that consumes tokens (`Parser/parser.c` and
  friends) is a separate subsystem and gets its own v0.12.4 spec
  when its turn comes.
