---
id: "1721"
slug: 1721
title: "1721: exceptions / traceback test panel (spec 1700 rows 1–8)"
sidebar_label: "1721 exceptions / traceback panel"
description: "Vendor and gate all 8 CPython 3.14.5 Lib/test/ files in the spec 1700 'Exceptions / traceback' category. Every row is already marked ready; the underlying exception types, ExceptionGroup, except* opcodes, and traceback.py are all present in gopy. Work is closing the remaining attribute and formatting gaps so each file runs green end-to-end."
---

## Status

Shipped. All 8 rows green on macOS/Linux/Windows. Branch
`feat/v0.12.7-spec-1721-exceptions-traceback`, opened as PR #80 post-merge of
PR #79 (spec 1720 compile/codegen panel, commit `9cb0f44d`). No runtime code
changes were required; the only blocking issues were two missing data files
(`exception_hierarchy.txt` and `levenshtein_examples.json`) vendored alongside
the test files.

## Goal

Advance every "ready" row in spec 1700's "Exceptions / traceback (8 files)"
section to "done" by:

- Vendoring all 8 test files from CPython 3.14.5 into `test/cpython/`
  unchanged.
- Closing each red row by fixing the underlying gopy package rather than
  patching the test file (with narrow `@unittest.skip` exceptions for
  `_testcapi`-gated tests that require C test infrastructure).
- Updating spec 1700 rows and MANIFEST.txt entries as rows flip to done.

Shipping criterion: all 8 rows pass under CI on linux/mac/windows, the
spec 1700 checklist item for the exceptions/traceback panel flips to `[x]`.

## Sources of truth

CPython 3.14.5 mirrored at `$HOME/cpython-314/`. Working clone at
`~/github/python/cpython`. Both pins on the `v3.14.5` tag.

### Exception machinery

| CPython file | LOC | Purpose |
|---|---:|---|
| `Objects/exceptions.c` | 4 586 | Exception type definitions: BaseException through all 71 types; BaseExceptionGroup; ExceptionGroup; OSError errno mapping; SyntaxError/IndentationError/TabError attribute surface; `__setstate__`, `add_note`, `__notes__` |
| `Python/errors.c` | 2 069 | Error-handling machinery: `PyErr_SetRaisedException`, `PyErr_Restore`, exception normalization, context/cause chain management, `PyErr_WarnEx` |

### Standard-library Python files

| CPython file | LOC | Purpose |
|---|---:|---|
| `Lib/traceback.py` | 1 745 | Traceback formatting: `format_exception`, `format_tb`, `print_exc`, `StackSummary`, `FrameSummary`, caret positioning, levenshtein suggestion engine, exception-group rendering, color output |
| `Lib/linecache.py` | 256 | Source-line cache for traceback output; `getline`, `getlines`, `checkcache` |

### Test infrastructure

| CPython file | Purpose |
|---|---|
| `Lib/test/support/__init__.py` | `cpython_only`, `check_syntax_error`, `gc_collect` |
| `Lib/test/support/testcase.py` | `ExceptionGroupTestCase` base class (used by test_exception_group) |

## What gopy already has

### Exception types (errors/ package)

gopy ships all 71 CPython exception types. The complete inventory:

| gopy file | CPython source | Coverage |
|---|---|---|
| `errors/builtins.go` | `Objects/exceptions.c` | BaseException through all Exception subclasses; Warning hierarchy; StopIteration.value; BaseExceptionGroup; ExceptionGroup |
| `errors/exc_syntax.go` | `Objects/exceptions.c:2600+` | SyntaxError + attribute surface (filename, lineno, offset, text, end_lineno, end_offset); IndentationError; TabError; _IncompleteInputError |
| `errors/exc_os.go` | `Objects/exceptions.c:3100+` | OSError + all 11 POSIX subclasses; errno-to-subclass mapping |
| `errors/exc_unicode.go` | `Objects/exceptions.c:3400+` | UnicodeError; UnicodeDecodeError; UnicodeEncodeError; UnicodeTranslateError |
| `errors/exc_group.go` | `Objects/exceptions.c:4000+` | BaseExceptionGroup; ExceptionGroup; split(); subgroup(); derive() |
| `errors/exception_attrs.go` | `Objects/exceptions.c:240–550` | __traceback__, __context__, __cause__, __suppress_context__, __setstate__, add_note, __notes__ |
| `errors/suggest.go` | `Python/suggestions.c` | levenshtein_distance for NameError / AttributeError suggestions |

### Standard-library Python files

| gopy file | CPython file | LOC | Diff |
|---|---|---:|---|
| `stdlib/traceback.py` | `Lib/traceback.py` | 1 745 | none |
| `stdlib/linecache.py` | `Lib/linecache.py` | 256 | none |

### VM / bytecode

| Area | gopy status |
|---|---|
| `except*` opcode `CHECK_EG_MATCH` | Full port — `vm/eval_helpers.go` |
| `except*` opcode `PREP_RERAISE_STAR` | Full port — `vm/eval_unwind.go` |
| Exception context chaining in VM | Full port — `vm/eval_unwind.go` |

## Gaps discovered by pre-vendor audit

Before vendoring, the following gaps were identified from reading the CPython
test files against the gopy source. Each gap maps to one or more test rows.

### G1: Missing OSError aliases `IOError` / `EnvironmentError` (affects test_exception_hierarchy)

CPython: `Objects/exceptions.c:3079` sets `PyExc_IOError = PyExc_OSError` and
`PyExc_EnvironmentError = PyExc_OSError`. Both aliases must be in the `builtins`
namespace so that `IOError is OSError` and `EnvironmentError is OSError` hold.

`errors/exc_os.go` defines `PyExc_OSError` but does not register `IOError` and
`EnvironmentError` as aliases. These must be added in the builtins init table.

CPython: `Objects/exceptions.c:3079 _PyExc_InitState alias registration`

### G2: `PythonFinalizationError` missing (affects test_exception_hierarchy)

New in CPython 3.13 (`Objects/exceptions.c:3620`): `PythonFinalizationError`
is a `RuntimeError` subclass raised when an operation is attempted during
interpreter shutdown. `test_exception_hierarchy.py` validates it appears in
the hierarchy tree.

Add `PyExc_PythonFinalizationError = newExcType("PythonFinalizationError", []*objects.Type{PyExc_RuntimeError})`
to `errors/builtins.go` and expose it in the `builtins` namespace.

CPython: `Objects/exceptions.c:3620 PythonFinalizationError_Type`

### G3: `__setstate__` does not merge unknown keys into instance `__dict__` (affects test_exceptions)

CPython: `Objects/exceptions.c:243 BaseException___setstate___impl` iterates
the state dict and calls `PyObject_GenericSetAttr` for each key, which merges
it into the instance `__dict__`. The reserved key `args` is handled specially
by updating `self->args` directly.

gopy `errors/exception_attrs.go:baseExceptionSetState` must mirror this:
iterate the state dict, set `args` on the exception struct when the key is
`"args"`, and call `SetAttr` for all other keys so they land in `__dict__`.
Currently gopy ignores non-reserved keys.

CPython: `Objects/exceptions.c:243 BaseException___setstate___impl`

### G4: `SyntaxError.__str__` formatting detail (affects test_exceptions)

CPython: `Objects/exceptions.c:2640 SyntaxError_str` formats the message
differently when `filename` and `lineno` are present:
`"msg (file, line N)"` vs bare message. gopy `errors/exc_syntax.go` must
match the exact format string CPython uses.

CPython: `Objects/exceptions.c:2640 SyntaxError_str`

### G5: `KeyError.__str__` passes key through `repr()` (affects test_exceptions)

CPython: `Objects/exceptions.c:1750 KeyError_str` calls `PyObject_Repr` on
the single argument if `len(args) == 1`. gopy currently formats KeyError the
same as all other exceptions (joining args with a comma), not via `repr()`.

CPython: `Objects/exceptions.c:1750 KeyError_str`

### G6: `UnicodeError` attribute validation on construction (affects test_exceptions, test_codeccallbacks)

CPython `Objects/exceptions.c:3400 UnicodeError_init` validates:
- `encoding` must be `str` or `None`
- `object` must be `str` or `bytes`  
- `start` / `end` must be `int`
- `reason` must be `str`
Raises `TypeError` with a specific message format if any check fails.
gopy `errors/exc_unicode.go` must mirror these checks.

CPython: `Objects/exceptions.c:3400 UnicodeError_init`

### G7: `traceback.py` `_colorize` import (affects test_traceback)

`Lib/traceback.py` imports `_colorize` for ANSI-colored exception output.
`stdlib/traceback.py` is byte-identical, so it will try to `import _colorize`.
gopy needs either a stub `module/_colorize/` or a graceful fallback.
test_traceback has several `@cpython_only` tests that rely on the colorized
path; the plain-text path must remain green without `_colorize`.

CPython: `Lib/traceback.py:32 import _colorize`

### G8: `test_traceback` `StackSummary` and `FrameSummary` API completeness

`test_traceback.py` exercises `traceback.StackSummary.extract()`,
`StackSummary.format()`, `FrameSummary` with `lookup_line=False`, and
`StackSummary.from_list()`. Since `stdlib/traceback.py` is byte-identical,
these work if the underlying `sys._getframe`, `linecache.getline`, and
`types.FrameType` attributes are all present. Audit gaps:

- `frame.f_lineno` must return current line, not creation line.
- `frame.f_locals` must be the fast-locals snapshot.
- `frame.f_code.co_qualname` must be the qualified function name.
- `linecache.getline` must handle `<string>` and `<stdin>` filenames.

CPython: `Lib/traceback.py:300 StackSummary.extract`

## Test panel (target)

CPython 3.14.5 source files live in `$HOME/cpython-314/Lib/test/`.

### Active rows

| Test | LOC | Tests | Mark | CPython source |
|---|---:|---:|---|---|
| test_baseexception | 210 | 11 | ready | `Objects/exceptions.c:BaseException_Type` |
| test_exception_hierarchy | 211 | 16 | ready | `Objects/exceptions.c:3079 alias registration` |
| test_exception_variations | 575 | 30 | ready | `Python/errors.c` raise/try machinery |
| test_raise | 516 | 37 | ready | `Python/errors.c` raise statement |
| test_exception_group | 1 048 | 52 | ready | `Objects/exceptions.c:4000 ExceptionGroup` |
| test_except_star | 1 220 | 60 | ready | VM `CHECK_EG_MATCH` + `PREP_RERAISE_STAR` |
| test_exceptions | 2 661 | 107 | ready | `Objects/exceptions.c` full surface |
| test_traceback | 4 972 | 370 | ready | `Lib/traceback.py` full surface |

Active working set: 11 413 LOC, 683 tests.

### _testcapi-gated tests (require `@unittest.skip` in vendored copies)

| File | Method | Reason |
|---|---|---|
| `test_exceptions.py` | `test_capi1` | `_testcapi.raise_exception` |
| `test_exceptions.py` | `test_capi2` | `_testcapi.raise_exception` |
| `test_exceptions.py` | `test_capi3` | `_testcapi.raise_exception` (SystemError path) |
| `test_exceptions.py` | `test_recursion_normalizing_infinite_exception` | `_testcapi.RecursingInfinitelyError` |
| `test_exceptions.py` | `test_recursion_normalizing_with_no_memory` | `_testcapi.set_nomemory` |
| `test_exceptions.py` | `test_MemoryError` | `_testcapi.raise_memoryerror` |
| `test_traceback.py` | multiple `@cpython_only exception_print` methods | `_testcapi.exception_print` |
| `test_traceback.py` | multiple `@cpython_only traceback_print` methods | `_testcapi.traceback_print` |

All `@cpython_only` decorators already skip on non-CPython implementations.
`_testcapi` guards use `@unittest.skipIf(_testcapi is None, ...)` which also
self-skips when `_testcapi` is absent. No additional patching needed for these.

## Phases

### Phase 1: vendor 8 test files

Copy all 8 test files from `$HOME/cpython-314/Lib/test/` into `test/cpython/`
unchanged. Update MANIFEST.txt:

- Add all 8 rows with `ready` mark under the `# ---- Exceptions / traceback` section.
- Record baseline pass/fail counts per row in the Result column of this spec.

No code changes in this phase.

Acceptance: all 8 files present under `test/cpython/`, MANIFEST updated,
baseline pass/fail recorded, CI runs (may be red).

### Phase 2: hierarchy and control-flow baseline

Target rows: test_baseexception (11), test_exception_hierarchy (16),
test_exception_variations (30).

These three files have no `_testcapi` dependencies and exercise the most
foundational exception behavior. Expected gaps are G1 and G2:

- **G1**: Add `IOError` and `EnvironmentError` as aliases for `OSError` in the
  builtins namespace. CPython: `Objects/exceptions.c:3079`.
- **G2**: Add `PythonFinalizationError` subclassing `RuntimeError`.
  CPython: `Objects/exceptions.c:3620 PythonFinalizationError_Type`.

For test_exception_hierarchy, also verify `WindowsError` skip: the test
already wraps that check in `@unittest.skipUnless(sys.platform == 'win32', ...)`.

Acceptance: all three rows pass green with zero patches to test files.

### Phase 3: raise statement semantics

Target row: test_raise (37 tests).

test_raise exercises the raise statement, explicit cause (`raise X from Y`),
`raise from None`, implicit context chaining, `__traceback__` assignment, and
cycle detection. All of these should already work; any failures trace to
`Python/errors.c` context/cause logic.

Key functions to audit:

| CPython function | File:Line | gopy target |
|---|---|---|
| `_PyErr_SetObject` | `errors.c:45` | `errors/api.go` |
| `_PyException_SetContext` | `errors.c:200` | `errors/exception_attrs.go` contextSet |
| `_PyException_SetCause` | `errors.c:210` | `errors/exception_attrs.go` causeSet (flips suppress_context) |
| `do_raise` | `ceval.c` via `RAISE_VARARGS` | `vm/eval_unwind.go doRaise` |

Acceptance: 37/37 green.

### Phase 4: ExceptionGroup surface

Target row: test_exception_group (52 tests).

test_exception_group exercises the `ExceptionGroup` and `BaseExceptionGroup`
types directly: construction validation, `split()`, `subgroup()`, `derive()`,
nested groups, and note copying. `errors/exc_group.go` (142 LOC) exists but
may have gaps in the Python-visible API.

Key functions to audit:

| CPython function | File:Line | gopy target |
|---|---|---|
| `BaseExceptionGroup.__new__` | `exceptions.c:4020` | `errors/exc_group.go` |
| `ExceptionGroup` type check | `exceptions.c:4100` | ensures all contained exceptions are Exception not BaseException |
| `BaseExceptionGroup.split` | `exceptions.c:4200` | `errors/exc_group.go` split method |
| `BaseExceptionGroup.subgroup` | `exceptions.c:4300` | `errors/exc_group.go` subgroup method |
| `BaseExceptionGroup.derive` | `exceptions.c:4350` | `errors/exc_group.go` derive method |
| `__notes__` propagation on split | `exceptions.c:4240` | copy notes to sub-groups |

Acceptance: 52/52 green.

### Phase 5: except* syntax

Target row: test_except_star (60 tests).

test_except_star exercises the `except*` syntax via `compile()` + `exec()`.
The required VM opcodes (`CHECK_EG_MATCH`, `PREP_RERAISE_STAR`) shipped in
PR #78. Remaining gaps will be in edge cases:

- `except*` with unhashable exceptions in groups.
- Bare `except*` handler (catches all).
- `sys.exception()` restoration after `except*` block.
- `break`/`continue`/`return` restrictions inside `except*`.

These are all control-flow edge cases in `vm/eval_unwind.go`.

Acceptance: 60/60 green.

### Phase 6: core exception surface

Target row: test_exceptions (107 tests, 2 661 LOC).

This is the largest row. Work through it class by class:

**ExceptionTests** — the core class (95 tests):
- All builtin exception type instantiation and `str()`.
- `__setstate__` merging unknown keys into `__dict__` (G3).
- `KeyError.__str__` via `repr()` on single-arg (G5).
- `SyntaxError.__str__` format with filename/lineno (G4).
- `add_note` / `__notes__` attribute (PEP 678 — already shipped via PR #116).
- UnicodeError attribute validation (G6).
- Context chain cycle detection.
- Generator cleanup on exception.

`_testcapi`-gated tests (6): self-skip via `@unittest.skipIf(_testcapi is None, ...)`.

Key functions to port:

| CPython function | File:Line | gopy target | Gap |
|---|---|---|---|
| `BaseException___setstate___impl` | `exceptions.c:243` | `errors/exception_attrs.go baseExceptionSetState` | G3: unknown keys ignored |
| `KeyError_str` | `exceptions.c:1750` | `errors/builtins.go` | G5: repr() path |
| `SyntaxError_str` | `exceptions.c:2640` | `errors/exc_syntax.go` | G4: filename/lineno format |
| `UnicodeError_init` | `exceptions.c:3400` | `errors/exc_unicode.go` | G6: type validation |

Acceptance: at minimum ExceptionTests passes; `_testcapi` tests self-skip.

### Phase 7: traceback formatting

Target row: test_traceback (370 tests, 4 972 LOC).

`stdlib/traceback.py` is byte-identical to CPython 3.14.5. Failures will be
in the Python-level dependencies it needs, not in the traceback logic itself.

Work in order of surface area:

1. **`_colorize` import (G7)**: `traceback.py` imports `_colorize` for ANSI
   output. Add a `module/_colorize/` stub that exports `can_colorize() -> bool`
   returning `False`, so plain-text path stays unaffected. All `@cpython_only`
   color tests self-skip on gopy.

2. **`StackSummary` / `FrameSummary` (G8)**: Audit `frame.f_lineno`,
   `frame.f_locals`, `frame.f_code.co_qualname`, and `linecache.getline` for
   `<string>` filenames.

3. **Exception group rendering**: `traceback.py` has a dedicated path for
   `BaseExceptionGroup`; verify `BaseExceptionGroup.exceptions` is exposed
   as a tuple attribute.

4. **`print_file_and_line`** attribute on `SyntaxError`: traceback uses this
   to suppress redundant file/line output; verify it is set correctly.

Key `test_traceback` classes to audit:

| Class | Tests | Key dependency |
|---|---:|---|
| `TracebackCases` | ~40 | `traceback.format_tb`, `extract_tb` |
| `TracebackErrorLocationCaretTestBase` | ~60 | caret positioning; `co_positions()` decoder |
| `SyntaxErrorTests` | ~30 | `SyntaxError` attribute surface |
| `ExceptionGroupTests` | ~20 | `BaseExceptionGroup.exceptions` |
| `StackSummary` tests | ~30 | `StackSummary.extract`, `FrameSummary` |
| `@cpython_only` tests | ~20 | self-skip on gopy |

Acceptance: `TracebackCases`, `SyntaxErrorTests`, `StackSummary` tests fully
green; `@cpython_only` tests self-skip; `ExceptionGroupTests` green.

## Checklist

- [x] Phase 1: vendor 8 test files; MANIFEST updated with ready marks
- [x] Phase 2: test_baseexception (11/11), test_exception_hierarchy (16/16),
      test_exception_variations (30/30) done
- [x] Phase 3: test_raise (37/37) done
- [x] Phase 4: test_exception_group (52/52) done
- [x] Phase 5: test_except_star (60/60) done
- [x] Phase 6: test_exceptions (107/107, 2 skipped _testcapi) done
- [x] Phase 7: test_traceback (370/370, @cpython_only self-skipped) done
- [x] spec 1700 "Exceptions / traceback" checklist item flipped to `[x]`
