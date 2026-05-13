---
format: md
id: 1705_core_vm_files
title: "1705. gopy core-VM file ports (Unicode escape, list compare, slice assign)"
sidebar_label: "1705. core_vm_files"
sidebar_position: 1705
slug: /specs/1705-core_vm_files
description: "Full-file ports of the CPython source files that back the runtime primitives the 1702 vendoring rows keep tripping on: Unicode escape decoding, list comparison, list slice-assign, and the string-literal parser path that feeds them. Every function in each file lands with a citation. Sister spec to 1704 - same rule, different files."
---


## Rule

Every CPython source file in scope is ported **in full**. No
function in those files may be left unported. The deliverable for
each file is a Go file whose function list 1:1 covers the C
function list. Once this spec lands we never come back to these
files for a missing slot.

Same rule as 1704. Same shape of work. Different files: the
runtime primitives that vendored stdlib code hits during import,
not the object protocol.

## Why this spec exists

Spec 1702 vendors CPython `Lib/*.py` byte-equal and then walks the
test suite to see what breaks. Each row we tried (`posixpath`,
`ntpath`, `textwrap`) surfaced a distinct primitive in the runtime
that was either half-ported or wrong:

- `min`/`max` on lists returned `NotImplemented` because
  `list_richcompare` only wired EQ/NE. Blocked `posixpath.relpath`.
- `\xb9` in a string literal got written into the output as a raw
  byte, producing invalid UTF-8 that silently corrupted later
  NAME lookups in the same module. Blocked `ntpath` at line 307.
- Slice-assign `lst[a:b] = x` reported "can only assign an
  iterable" for inputs that *are* iterable. Blocked `textwrap`.

These are not stdlib bugs. They are gaps in functions whose CPython
originals are short and well-defined. Spec 1702 cannot make
progress on the vendoring rows until these primitives are
faithfully ported, and the only way to stop these one-off
discoveries is to take the whole containing function each time
rather than spot-patching the case we tripped on.

## Files in scope

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Objects/unicodeobject.c` (escape-decode slice: `_PyUnicode_DecodeUnicodeEscapeInternal2`, `_PyUnicode_DecodeUnicodeEscapeStateful`, `PyUnicode_DecodeUnicodeEscape`) | ~290 | `parser/string/decode.go` (`decodeUnicodeEscapes`) | partial (\x and octal cases ported faithfully; default-branch invalid-escape recording and the stateful/`consumed` plumbing still pending) |
| B | `Objects/bytesobject.c` (escape-decode slice: `_PyBytes_DecodeEscape`) | ~140 | `parser/string/decode.go` (`decodeBytesEscapes`) | pending |
| C | `Objects/listobject.c` (`list_richcompare` + slice-assign helpers `list_ass_slice`, `list_ass_subscript`, `list_extend`) | ~400 | `objects/list_misc.go`, `objects/list.go` | partial (richcompare done; slice-assign plumbing pending) |
| D | `Objects/setobject.c` (add/insert path: `set_add_entry`, `set_insert_clean`, `set_table_resize`; binary ops `set_or`, `set_and`, `set_sub`, `set_xor`) | ~400 | `objects/set.go` | partial (insert now grows-before-place; resize/binary-op functions cited but full audit of remaining members pending) |

Sources of truth live under `/Users/apple/cpython-314/`.

## Phase index

Each phase ports one file (or one disjoint block of a file) end
to end. Phases are independent unless the **Blocks** column says
otherwise. Final gate is "vendored stdlib import chain runs
through `textwrap`, `traceback`, and `ntpath` without raising."

| Phase | File | Block | Blocks | Status |
|-------|------|-------|--------|--------|
| 1 | A `unicodeobject.c` | `_PyUnicode_DecodeUnicodeEscapeInternal2` | - | partial (\x and octal codepoint emission shipped; invalid-escape recording + stateful/consumed pending) |
| 2 | B `bytesobject.c` | `_PyBytes_DecodeEscape` | - | pending |
| 3 | C `listobject.c` | `list_richcompare` (all six op branches) | - | done |
| 4 | C `listobject.c` | `list_ass_slice` + `list_ass_subscript` (slice assignment, deletion, slice-step) | - | pending |
| 5 | D `setobject.c` | `set_add_entry` (insert resizes before place) + `set_insert_clean` (rehash) | - | done |
| 4a | C `listobject.c` | `drainIterableForSlice` routed through `PyObject_GetIter` (SeqIter fallback) so `__getitem__`-only iterables work for slice-assign | - | done |
| Gate | - | `import textwrap`, `import traceback`, `import ntpath` all green | 1,2,3,4,5 | partial (`import ntpath` green; `import textwrap`/`import traceback` block on a separate listcomp cell-binding bug in `re/_compiler.py`, already tracked under 1702 VM audit tasks #586/#587/#588) |

## Phase 1 - `Objects/unicodeobject.c` escape decoder

### Functions to port

CPython exposes the unicode-escape decoder through a three-layer
API. Every layer lands.

| C function | gopy hook | Status |
|------------|-----------|--------|
| `_PyUnicode_DecodeUnicodeEscapeInternal2` | `decodeUnicodeEscapes` | partial (codepoint emission for `\x` and octal correct; default-branch invalid-escape position tracking + stateful `consumed` parameter still missing) |
| `_PyUnicode_DecodeUnicodeEscapeStateful` | (not yet exposed) | pending |
| `PyUnicode_DecodeUnicodeEscape` | (not yet exposed) | pending |

### What was wrong before this phase

The Go port wrote `byte(v)` for `\xNN` and for octal escapes in
the `< 0x100` range. CPython treats those as codepoint U+00NN and
emits them with the unicode writer (`WRITE_CHAR`), which expands
to multi-byte UTF-8 above 0x7F. Writing the raw byte left the
output string invalid UTF-8 - downstream code that hashed the
string or used it as a dict key silently misbehaved, which is how
`s = '\xb9'; print(s)` came out as `NameError`.

### Gate

`s = '\xb9'; print(s)` prints `¹` and `ord(s) == 185`. `import
ntpath` reaches end-of-module without hanging.

## Phase 2 - `Objects/bytesobject.c` escape decoder

The bytes form (`b'\xb9'`) is *supposed* to write a raw byte; this
phase is here to keep the port faithful and audited rather than
silently diverging. Mostly a re-citation pass, plus picking up the
range/error behaviour for octal escapes that CPython enforces.

## Phase 3 - `Objects/listobject.c` `list_richcompare`

Done in PR #27. All six op branches walk pairwise to the first
non-equal pair and defer to that pair's comparison, falling back
to length when one list is a prefix of the other.

## Phase 4 - `Objects/listobject.c` slice-assign

`lst[a:b] = x` currently routes through `drainIterableForSlice`
and raises "can only assign an iterable" for inputs that have an
`Iter` slot but go through a code path the helper doesn't reach.
The fix is to port `list_ass_slice` (no-step case) and
`list_ass_subscript` (step case) together, so the iterable
extraction and the slice splice are one block of code that
matches CPython 1:1.

Blocks: `import textwrap`, which the `traceback` row in spec 1702
depends on.

Sub-phase 4a (done): `drainIterableForSlice` previously gated on
`tp_iter` alone, which rejected objects (like `re._parser.SubPattern`)
that expose only `__getitem__`. Route through `Iter()`
(PyObject_GetIter) so the SeqIter fallback fires the same way it
does for `for x in obj`, matching CPython `PySequence_Fast`. The
remaining `list_ass_slice` / `list_ass_subscript` audit is still
pending under Phase 4.

## Phase 5 - `Objects/setobject.c` add/insert path

### What was wrong before this phase

`(*Set).insert` placed an entry without checking the fill ratio.
`(*Set).add` checked the ratio first and called `insert`, but the
binary-op helpers (`setUnion`, `setIntersect`, `setDiff`,
`setSymDiff`) and the `frozenset` constructor short-circuited
through `insert` directly. As a result `set.__or__` between any
two sets whose combined size exceeded the initial 8-slot table
ran the open-addressed probe forever, because every slot was full
and `lookup` had no termination condition for that case. This
surfaced as a hang at line 304 of vendored `ntpath.py` where the
module forms `_RESERVED_NAMES | {'"', '*', ':'}`.

### Functions ported

| C function | gopy hook | Status |
|------------|-----------|--------|
| `set_add_entry` | `(*Set).insert` (grow-then-place) | done |
| `set_insert_clean` | `(*Set).insertClean` (rehash-only) | done |
| `set_table_resize` (size-doubling case) | `(*Set).grow` | done |

### Gate

`a = {chr(i) for i in range(32)}; b = {'"', '*', ':'}; print(len(a
| b))` prints `35`. `import ntpath` completes without hanging.

## Workflow per port

Same eight-step cadence as 1702 and 1704:

1. Pick the next row from "Files in scope".
2. Mark the matching task in_progress.
3. Read the CPython source, port every function in the row, add
   `// CPython: <file>:<line> <name>` citations.
4. Run the row's gate from the table above.
5. Flip the row status in this spec to `done`.
6. `go build ./...`, `go test ./...`, fix lint diagnostics.
7. Commit, push, and post a human comment on the PR.
8. Mark the task `completed` and pick the next row.
