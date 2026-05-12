---
format: md
id: 1703_re_sre_full_port
title: "1703. gopy re / _sre full port"
sidebar_label: "1703. re_sre_full_port"
sidebar_position: 1703
slug: /specs/1703-re_sre_full_port
description: "Replace the RE2-wrapping _sre shim with a CPython-faithful bytecode interpreter so the vendored Lib/re/ compiler's output runs unchanged. Closes task #510 and unblocks fnmatch option-A, TestImportTokenize, and the regex-using slice of the unittest test corpus."
---


## Checklist

Status legend: `done` shipped and verified; `partial` landed with
known follow-up; `pending` not started.

| Phase | Status | Description |
|-------|--------|-------------|
| 1 | done | Port `Lib/re/_constants.py` + `Modules/_sre/sre_constants.h`: 43 opcodes, AT codes, category codes, flag bits. |
| 2 | done | Port `Lib/re/_casefix.py`: 46-entry case-fold lookup. |
| 3 | done | Port `Modules/_sre/sre_lib.h` match engine: `SRE_STATE`, dispatch loop, 43 opcode handlers, repeat / backtrack stacks. |
| 4 | done | Pattern type: real `_sre.compile` honouring the bytecode argument, `match` / `fullmatch` / `search` / `scanner`. |
| 5 | done | Match type: `group` / `groups` / `groupdict` / `span` / `expand` / `regs` plus `string` / `re` / `pos` / `endpos` / `lastindex` / `lastgroup`. |
| 6 | done | Pattern higher-level methods: `findall`, `finditer`, `split`, `sub`, `subn`. |
| 7 | partial | Engine drives the bytecode `_compiler.py` emits for the three Phase 7 gate patterns (Go-side gate in `module/_sre/phase7_test.go`). Full CLI gate `gopy -c 'import re; ...'` blocked on `enum` import (unrelated to spec 1703). |
| 8 | partial | RE2 wrapper retired in Phase 4 (commit 79bc384). fnmatch option-A migration deferred: depends on the vendored `re` package importing, which is blocked on `enum`. |
| Final gate | pending | `gopy -c 'import re; print(re.match(r"(\d+)-(\d+)", "12-34").groups())'` prints `('12', '34')`; `TestImportTokenize` flips from Skip to Pass; spec 1702 `re / _sre` row marks done. |

## Goal

Make CPython's regex behaviour the gopy regex behaviour. Today the
`_sre` "port" is a 1075-line wrapper around Go's `regexp` package
(RE2). It accepts the bytecode argument to `_sre.compile` and
throws it away, then re-parses the source pattern string with
`gore.Compile()`. That works for common patterns and silently
miscompiles or hard-rejects the CPython-specific constructs
RE2 cannot express. The vendored `stdlib/re/_compiler.py` already
produces correct bytecode, but nothing executes it.

The fix is a full port of `Modules/_sre/`: implement the bytecode
interpreter in Go so the vendored compiler's output runs as-is.

## Why this scale of port

Spec 1701 names the rule: no partial stubs. Patching the RE2 wrapper
to handle one rejected construct at a time is the exact pattern that
rule forbids. The branch has three confirmations on this point
already (exception unwind, generator prefix, cell binding): bug-by-bug
fixes accrete bugs faster than full subsystem ports clear them. Regex
is bigger than those three combined but the shape of the work is the
same. One subsystem, one pass, citations on every function.

## Why now

Three downstream gates wait on this port.

1. **fnmatch option-A migration (#519).** Spec 1702 § fnmatch
   documents that `Lib/fnmatch.py` cannot be vendored byte-equal
   because the current `_sre` rejects `\Z` and `(?>...)` that
   `fnmatch.translate()` emits. The Go-side glob matcher exists
   only to work around that gap.
2. **TestImportTokenize and the enum / re import chain.** Several
   `stdlibinit/stdlib_import_test.go` tests skip today because
   `import re` fails on regex compilation paths the RE2 shim
   miscompiles.
3. **Panel tasks #474 - #487 (spec 1700).** Counted from `stdlib/`:
   98 Python modules import re or `from re`. Most unittest
   `test_*.py` files match the same shape. Any panel task that
   lands a regex-using test (which is most of them) hits the gap.

## Sources of truth

CPython 3.14 source: `/Users/apple/cpython-314/`.

**`Modules/_sre/` (~7,180 lines C):**

| File | Lines | Purpose |
|------|------:|---------|
| `sre.c` | 3,456 | Module entry, Pattern / Match / Scanner objects |
| `sre.h` | 123 | Type definitions |
| `sre_constants.h` | 98 | Opcode enum (auto-generated from `_constants.py`) |
| `sre_lib.h` | 1,874 | Match engine (instantiated 3x for UCS1 / UCS2 / UCS4) |
| `sre_targets.h` | 58 | Computed-goto label table for dispatch |
| `clinic/sre.c.h` | 1,571 | Generated argument unpackers (Go port skips) |

**`Lib/re/` (~2,606 lines Python, already vendored byte-equal):**

| File | Lines | Purpose |
|------|------:|---------|
| `__init__.py` | 428 | Public API (`compile`, `match`, `search`, ...) |
| `_parser.py` | 1,066 | Pattern string -> parse tree |
| `_compiler.py` | 782 | Parse tree -> bytecode list passed to `_sre.compile` |
| `_constants.py` | 224 | Opcode and flag enums |
| `_casefix.py` | 106 | Case-fold lookup table |

## Why it is broken today

`module/_sre/module.go:307 sreCompile` takes the
`(pattern, flags, code, groups, groupindex, indexgroup)` tuple
CPython's `_sre.compile` takes, but reads only `pattern` and
`flags`. The bytecode list in `code` (the 43-opcode product of
`_compiler.py`) is discarded. Pattern matching reaches Go's RE2
through `gore.Compile()`. RE2 is a finite-automaton engine with
linear-time guarantees; CPython's SRE is an imperative bytecode
VM with arbitrary backtracking. The grammars do not align:

| CPython feature | RE2 status |
|-----------------|------------|
| `\Z` end-of-string | RE2 spells it `\z`. Mismatch on every CPython-produced regex that anchors to end-of-string. |
| `(?>...)` atomic group | RE2 rejects. Used by `fnmatch.translate()`. |
| `(?=...)`, `(?!...)` lookahead | RE2 rejects. |
| `(?<=...)`, `(?<!...)` lookbehind | RE2 rejects. |
| `\1`...`\99` backreferences | RE2 rejects. Our wrapper raises a typed `re.error` for these. |
| `(?(name)yes\|no)` conditional | RE2 rejects. |
| Possessive repeats `a*+` | RE2 rejects. |
| `_casefix.py` extra-case folding | RE2 case-insensitive misfolds Greek beta, Latin long s, dotless i, German sharp s. |

The Go wrapper papers over a couple of these (strips inline flags
it recognises, rewrites named groups, rejects backreferences with
a typed error), but most pass through as silent miscompiles or
hard rejects.

## Plan

Eight phases. Each lands as one or more commits with its own gate
so the PR stays green at every checkpoint and the existing test
suite never regresses. Phases 1 and 2 are independent and can run
in parallel. Phase 3 is the bulk of the work. Phases 4, 5, 6 layer
on top of 3. Phase 7 is verification. Phase 8 retires the shim.

### Phase 1. _constants port

Port `Lib/re/_constants.py` and `Modules/_sre/sre_constants.h` into
Go `iota`-defined constants. One source file:
`module/_sre/constants.go`.

**Surface.** 43 `Op*` opcodes (`OpSuccess`, `OpFailure`, `OpAny`,
`OpLiteral`, `OpMark`, `OpRepeat`, ..., `OpAtomicGroup`,
`OpPossessiveRepeat`, `OpPossessiveRepeatOne`); 12 AT-position
codes (`AtBeginning`, `AtEnd`, `AtBoundary`, ...); 10 category
codes (`CategoryDigit`, `CategoryWord`, ...); the flag bits
(`SreFlagIgnoreCase`, `SreFlagMultiline`, `SreFlagDotAll`,
`SreFlagUnicode`, `SreFlagAscii`, `SreFlagLocale`, `SreFlagVerbose`,
`SreFlagDebug`, `SreFlagTemplate`).

**Gate.** `module/_sre/constants_test.go` asserts every Go constant
equals the numeric value `_constants.py` ships, by reading
`_constants.py` at test time and comparing each name.

**CPython references.**
- `Modules/_sre/sre_constants.h:1` `SRE_OP_*` enum
- `Lib/re/_constants.py:78` `OPCODES`

### Phase 2. _casefix port

Port `Lib/re/_casefix.py` to a Go `map[rune][]rune`. The upstream
file is auto-generated from Unicode case-folding data, so we copy
the table directly and cite the generator.

**Surface.** `extraCases map[rune][]rune` with 46 entries.

**Gate.** `re.compile(r"i", re.I).match("ı")` succeeds (dotless i
case-folds to ASCII i under Unicode rules).

**CPython reference.**
- `Lib/re/_casefix.py:1` `_EXTRA_CASES`

### Phase 3. Match engine core

Port `Modules/_sre/sre_lib.h` to Go. One source file:
`module/_sre/engine.go`. This is the bulk of the work.

**Surface.**

- `state` struct mirroring `SRE_STATE` (sre.h:81): input slice,
  start / end / cursor positions, mark array, repeat-stack head,
  data-stack for context frames.
- `match(s *state, code []int32, toplevel bool) bool`. The dispatch
  loop. One handler per opcode, dispatched through a switch.
  CPython's `USE_COMPUTED_GOTOS` is a C-specific micro-optimisation;
  Go's switch produces correct semantics, just a measurable cycle
  difference, and the gopy port follows CPython's switch fallback
  branch (sre_lib.h:644) line-for-line.
- `search(s *state, code []int32) bool` - the start-position walk.
- `count(s *state, code []int32, maxcount int) int` - the
  `REPEAT_ONE` inner counter.
- Explicit backtracking stack of context frames; not host recursion.
  CPython does it this way to bound C stack usage. Go does not need
  that for stack safety, but mirroring the structure means the port
  reads against `sre_lib.h` one frame at a time and makes future
  CPython rebases auditable.
- Helpers: char-class membership (`charsetMember`); AT-position
  predicates (`atIsBoundary`, `atIsBeginning`, ...); case-fold
  lookup through phase 2's table; Unicode-category predicates via
  Go's `unicode` package (`unicode.IsDigit`, `IsLetter`, `IsSpace`,
  etc., mapped to CPython's `Py_UNICODE_IS*` semantics).

**Gate.** A Go-level engine test (no Python integration yet)
hand-builds a bytecode program (`OpLiteral 'a'`, `OpSuccess`) and
asserts `match([]rune("abc"), code, true) == true`. Cover one
opcode per family in the test: literal, charset, repeat,
alternation, mark, groupref, AT-boundary.

**CPython references.**
- `Modules/_sre/sre_lib.h:599` `SRE(match)` - dispatcher
- `Modules/_sre/sre_lib.h:1692` `SRE(search)` - search wrapper
- `Modules/_sre/sre_lib.h:193` `SRE(count)` - REPEAT_ONE counter
- `Modules/_sre/sre.h:71` `SRE_REPEAT` - repeat-stack node
- `Modules/_sre/sre.h:81` `SRE_STATE` - match state

### Phase 4. Pattern type and compile

Replace the RE2-wrapping `Pattern` in `module/_sre/module.go` with
a Pattern that carries the bytecode produced by `_compiler.py`.

**Surface.**

- `sreCompile(pattern, flags, code, groups, groupindex, indexgroup)`
  builds a `*Pattern` carrying the raw `code` slice plus the group
  metadata. No RE2 re-compile and no `regexp` import.
- `Pattern.match(string, pos, endpos)` runs the phase 3 engine in
  match mode and returns a `*Match` or `None`.
- `Pattern.fullmatch`, `Pattern.search`, `Pattern.scanner` follow.
- Properties: `pattern` (source str), `flags` (int), `groups`
  (int), `groupindex` (dict).
- `__copy__` and `__deepcopy__` return self (CPython does too;
  patterns are immutable).

**Gate.** `gopy -c 'import _sre; ...; print(p.match("hello"))'`
with a hand-crafted bytecode list returns a Match.

**CPython references.**
- `Modules/_sre/sre.c:1621` `_sre_compile_impl`
- `Modules/_sre/sre.c:3166` Pattern_match
- `Modules/_sre/sre.c:3225` Pattern_search
- `Modules/_sre/sre.c:2959` Scanner_match

### Phase 5. Match type

Port the Match object surface.

**Surface.**

- `Match.group(*args)`. Variadic; zero args returns the whole
  match, one int / str returns one group, more than one returns a
  tuple, unmatched optional groups return `None`.
- `Match.groups(default=None)`, `Match.groupdict(default=None)`.
- `Match.span(group)`, `Match.start(group)`, `Match.end(group)`.
- `Match.expand(template)` evaluating `\g<name>` / `\1` references.
- `Match.regs`.
- Properties: `string`, `re`, `pos`, `endpos`, `lastindex`,
  `lastgroup`.

**Gate.** Round-trip group access from Python against a compiled
pattern. Both numeric and named group access pinned.

**CPython reference.**
- `Modules/_sre/sre.c:3225` match methods block

### Phase 6. Higher-level Pattern methods

`findall`, `finditer`, `split`, `sub`, `subn`. Each is layered on
the phase 4 + 5 primitives plus the `\g<name>` template parser.

**Gate.** One Python-level assertion per method against worked
CPython examples (sub with a function `repl`, split with a
zero-width separator that CPython 3.7+ tolerates, findall on a
multi-group pattern returning tuples).

**CPython references.**
- `Modules/_sre/sre.c:2400` Pattern_findall
- `Modules/_sre/sre.c:2453` Pattern_finditer
- `Modules/_sre/sre.c:2509` Pattern_split
- `Modules/_sre/sre.c:2618` Pattern_sub
- `Modules/_sre/sre.c:2705` Pattern_subn

### Phase 7. Vendor the Python layer

`stdlib/re/__init__.py`, `_compiler.py`, `_parser.py`,
`_constants.py`, `_casefix.py` are already vendored byte-equal and
recorded in `stdlib/MANIFEST.txt`. The phase 1 - 6 work changes no
Python-visible names, so this phase is verification only: confirm
the vendored layer still imports against the new `_sre` and runs
its smoke gates.

**Gate.** `gopy -c 'import re; m = re.match(r"(\d+)-(\d+)", "12-34"); print(m.groups())'`
prints `('12', '34')`. `gopy -c 'import re; print(re.findall(r"\w+", "a b c"))'`
prints `['a', 'b', 'c']`. `gopy -c 'import re; print(re.sub(r"\d", "_", "a1b2"))'`
prints `'a_b_'`.

### Phase 8. Retire the RE2 shim

Delete the RE2 wrapper logic in `module/_sre/module.go`. The file
shrinks to Pattern / Match / Scanner type definitions plus module
exports. No `import "regexp"`. The new engine is the only path.

fnmatch (#519) deletes its Go-side glob matcher and delegates to
`re.compile(translate(pat)).match`, closing the option-A migration
spec 1702 § fnmatch left open. The delete can land in the same
PR or as an immediate follow-up.

**Gate.** `go test ./...` clean.

## Out of scope

- **`template()` deprecated function.** CPython keeps it as a stub
  emitting a deprecation warning; we mirror that with a one-line
  shim.
- **JIT / DFA precompilation.** CPython has none either; this is
  automatic.
- **Computed-goto dispatch.** Go has no computed-goto. The switch
  dispatcher is correct, just measurably slower in CPython too
  when the goto path is disabled. Not a semantics gap.
- **`LOCALE` flag character classes.** Python 3 made these
  locale-only and CPython itself recommends against them. We
  accept the flag and fall through to ASCII semantics. Document
  the deviation in spec 1702 when this row flips to done.
- **Buffer-protocol input (memoryview, mmap).** CPython's `_sre`
  accepts buffer-protocol objects. The phase 4 - 6 work covers
  `str` and `bytes` only. Buffer input lands as a follow-up
  if a test corpus consumer hits it.
- **`scanner()` Scanner.search / Scanner.match.** Ports in phase 4
  using the same engine path as Pattern.match, but the
  `re.Scanner` higher-level Python wrapper in `Lib/re/__init__.py`
  is not in the unittest critical path and can land last.

## Verification

After phase 8:

1. `go test ./...` green; no `regexp` import anywhere under
   `module/_sre/`.
2. `gopy -c 'import re; ...'` smoke tests for each phase 4 - 6 gate
   pinned in `module/_sre/integration_test.go`.
3. fnmatch (#519) option-A migration lands; `module/fnmatch/`
   Go-side glob matcher deleted.
4. `TestImportTokenize` in `stdlibinit/stdlib_import_test.go` flips
   from Skip to Pass. (The codecs `__build_class__` failure on the
   same import chain is a separate VM / frame-builtins audit item
   under task #521 and is not blocked by this spec.)
5. Spec 1702's `re / _sre` row flips to `done` with the standard
   four-block detail paragraph (Surface, Location, Deferred,
   Gate).

## Tasks

The work splits into one task per phase, blocking #510. Phase
order is strict:
- Phases 1 and 2 are independent and may run in parallel.
- Phase 3 blocks phases 4, 5, 6.
- Phase 4 blocks phases 5 and 6.
- Phase 7 verifies phases 4 - 6.
- Phase 8 retires the shim and lands the fnmatch follow-up.
