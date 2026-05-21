---
format: md
id: 1718_lexer_gate_closers
title: "1718. v0.12.4 lexer-test gate closers"
sidebar_label: "1718. lexer gate closers"
sidebar_position: 1718
slug: /specs/1718-lexer-gate-closers
description: "Five 1:1 CPython 3.14 subsystem ports that gate the test_tokenize.py and test_source_encoding.py panel rows in spec 1710: slot inheritance in Objects/typeobject.c, with_traceback / __setstate__ on BaseException, the SyntaxError PyMemberDef table, print() dynamic sys.stdout resolution in Python/bltinmodule.c, and the string-name-string adjacency case in Parser/lexer/lexer.c."
---

## Rule

Same as 1704 / 1705 / 1708 / 1709 / 1710 / 1717. Every phase is a
straight port of a CPython 3.14.5 source slice into the matching gopy
package, with `// CPython: <path>:<line> <function>` citations on every
ported function. No custom shims, no behavioural adaptations: each
function lands as a 1:1 rewrite of the upstream body so the gate test
output stays interpretable against `~/cpython-314`.

## Why this spec exists

The spec 1710 panel rows for `test_tokenize.py` and
`test_source_encoding.py` advanced past the unicodedata import wall
(spec 1717, d48fae8) but still fail. Running them under
`test/cpython/` exposes five independent gaps, every one of which is
a partial port of a CPython subsystem rather than a tokenizer issue:

1. **`__slots__` inheritance is broken.** `objects/usertype.go`
   `installSlots` (line 1062) and `objects/instance.go` `NewInstance`
   (line 54) ignore parent classes' slot tables, so a subclass that
   redeclares `__slots__ = ()` cannot set names inherited from its
   parent. `_collections_abc.MappingView` declares
   `__slots__ = '_mapping',`; every `ItemsView` / `KeysView` /
   `ValuesView` subclass with `__slots__ = ()` hits AttributeError on
   first store. This cascades into
   `unittest.TestCase.subTest` formatting through
   `_OrderedChainMap.items()`, turning every sub-test failure into a
   secondary AttributeError that swallows the original assertion.
2. **`BaseException.with_traceback` and `__setstate__` are missing.**
   `errors/exception_attrs.go` exposes `args`, `add_note`,
   `__notes__`, `__cause__`, `__context__`, `__suppress_context__`,
   `__traceback__`. The two methods CPython binds at
   `Objects/exceptions.c:243` (`__setstate__`) and `:279`
   (`with_traceback`) are not bound. `test_max_indent` reraises via
   `e.with_traceback(tb)`.
3. **`SyntaxError` attributes are not exposed.**
   `errors/exc_syntax.go` defines `SyntaxErrorInfo` with the fields,
   but no getsets / members register them on the type. Reading
   `e.lineno`, `e.offset`, `e.text`, `e.filename`, `e.msg`,
   `e.end_lineno`, `e.end_offset` raises AttributeError. CPython
   exposes all of them as `PyMemberDef` in
   `Objects/exceptions.c:2875` `SyntaxError_members`.
4. **`print()` ignores `sys.stdout` reassignment.**
   `builtins/print.go:23` `Print(defaultFile io.Writer)` captures
   `defaultFile` at binding time and never reads `sys.stdout` on
   call. CPython resolves the stream via `_PySys_GetRequiredAttr` on
   every invocation (`Python/bltinmodule.c:2231`).
   `support.captured_stdout()` swaps `sys.stdout` and depends on the
   dynamic lookup; every `BytesSourceEncodingTest` test hangs because
   the helper output never reaches the StringIO buffer.
5. **The tokenizer mis-parses string-name-string adjacency.** Given
   `x = "doesn\'t "shrink", does it"`, CPython emits five
   meaningful tokens (NAME `x` / EQUAL / STRING / NAME `shrink` /
   STRING). gopy's `parser/lexer/lexer.go` consumes the trailing
   identifier as if it were a string prefix continuation, emitting
   `STRING(shrink", does it")` and swallowing the NAME. Under the
   panel this is the surface failure of `CTokenizeTest.test_string`,
   and the recursive AttributeError from blocker 1 turns the assert
   into a Go-level crash (exit 2, no stderr).

These gaps are independent. Order in this spec matches the suggested
port order from the deep-dive note: slots first so subTest reporting
becomes legible, then exception attributes so introspection-based
asserts unlock, then `print()` so captured_stdout works, then the
tokenizer fix so the last surface failure clears.

## Sources of truth

| CPython 3.14 file | Lines | gopy destination |
|-------------------|------:|------------------|
| `Objects/typeobject.c` (`type_new_slots`, `type_new_descriptors`, `type_new_alloc`) | ~250 | `objects/usertype.go`, `objects/instance.go`, `objects/type.go` |
| `Objects/exceptions.c` (`BaseException___setstate___impl`, `BaseException_with_traceback_impl`) | ~50 | `errors/exception_attrs.go` |
| `Objects/exceptions.c` (`PySyntaxErrorObject`, `SyntaxError_init`, `SyntaxError_str`, `SyntaxError_members`) | ~200 | `errors/exc_syntax.go` |
| `Python/bltinmodule.c` (`builtin_print_impl`) | 85 | `builtins/print.go` |
| `Parser/lexer/lexer.c` (`tok_get_normal_mode` string-literal arm) | ~120 | `parser/lexer/lexer.go` |

Gate tests live at `~/github/python/cpython/Lib/test/`:

- `test_tokenize.py` (`CTokenizeTest`, `TokenizeTest`).
- `test_source_encoding.py` (`BytesSourceEncodingTest`).

## Checklist

Status legend: DONE = ported in full and verified, WIP = port
underway, TODO = not started.

| Phase | Title | Status | Commit |
| ----- | ----- | ------ | ------ |
| P1 | Slot inheritance: walk MRO in `installSlots`, size `inst.slots` to cumulative parent + own count. Test: subclass with empty `__slots__` can set inherited names. | DONE | pending |
| P2 | Port `SyntaxError_init` + `SyntaxError_str` + `SyntaxError_members` so `lineno`, `offset`, `text`, `filename`, `msg`, `end_lineno`, `end_offset`, `print_file_and_line`, `_metadata` resolve through `GenericGetAttr`. | DONE | pending |
| P3 | Port `BaseException___setstate___impl` + `BaseException_with_traceback_impl`. Wire into `errors/exception_attrs.go init()` registration. | DONE | pending |
| P4 | Rewrite `Print` to drop `defaultFile`, resolve `sys.stdout` on every call via the runtime sys lookup. `_PyFile_Flush` parity. | DONE | pending |
| P5 | Re-port the string-literal arm of `tok_get_normal_mode`: a NAME-start byte after a closing quote breaks the string instead of continuing it; add lexer position-parity test for the adjacency snippet. | DONE | pending |
| P6 | Re-run `test_tokenize.py` + `test_source_encoding.py` panel rows; flip MANIFEST to green or to the next out-of-scope blocker. Update spec 1710's panel rows. | DONE | pending |

## Phase notes

### P1: slot inheritance

Repro:
```python
class A:
    __slots__ = '_x',
class B(A):
    __slots__ = ()
B()._x = 1  # AttributeError
```

CPython collects inherited slots through `type_new_alloc` (computes
`itemsize` from the base) and `inherit_slots` (Objects/typeobject.c).
In gopy the equivalent state is:

- `Type.Slots []string` (current-class only)
- `installSlots` only walks the current `__slots__`
- `NewInstance` sizes `inst.slots` to `len(t.Slots)`
- `MemberDescr.index` indexes into `inst.slots`

The port computes a flattened slot table for the type at `installSlots`
time using MRO order, registers `MemberDescr` entries for every base's
slot names too, and sizes `inst.slots` to that flattened count.
Indices stay stable per-name because subclass declarations append
after the parent's entries.

### P2: SyntaxError member table

CPython exposes nine fields via `PyMemberDef` of type `_Py_T_OBJECT`
(read/write, no doc-string requirement). The gopy port adds named
getset descriptors to `PyExc_SyntaxError` whose getters / setters
read and write the `SyntaxErrorInfo` struct. `SyntaxError_init`
ports the 2-arg unpack: `args[0]` becomes `msg`, `args[1]` is the
6/7-tuple `(filename, lineno, offset, text [, end_lineno, end_offset, _metadata])`.
`SyntaxError_str` ports the `%S (%U, line %ld)` formatting so that
`str(e)` matches CPython byte-for-byte. `IndentationError` and
`TabError` inherit the table automatically through MRO descriptor
lookup once registration is on the SyntaxError type.

### P3: with_traceback + __setstate__

Both are one-page ports. `with_traceback(tb)` calls the existing
`tracebackSet` and returns `self`. `__setstate__(state)` iterates
`state` (a dict) and stores each `(key, value)` back through
`SetAttr`, exactly mirroring `BaseException___setstate___impl`.

### P4: print()

The current `Print(defaultFile)` factory is exactly the shim the
"no shims" rule forbids: it pins stdout at construction and never
re-reads sys. The port:

- removes the factory; `print` becomes a plain `func(args, kwargs)`
- resolves `sys.stdout` on every call via the runtime's sys module
  (CPython: `_PySys_GetRequiredAttr(&_Py_ID(stdout))`)
- emits the same Py_None handling for sep/end (None means default)
- flushes via the runtime's `_PyFile_Flush` lookup chain when
  `flush=True`

The `defaultFile` parameter in callers (`builtins.Init`) is dropped;
the sys module owns stdout/stderr/stdin attachment.

### P5: tokenizer string adjacency

CPython's `Parser/lexer/lexer.c` `tok_get_normal_mode` calls
`tok_decimal_tail` / quote-scanning helpers that explicitly check the
character class of the next byte after a closing quote: only a
prefix character (`r`, `R`, `b`, `B`, `u`, `U`, `f`, `F`) plus a
fresh quote continues a string. The gopy port mistakes any
identifier start after the closing quote for continuation. The fix
mirrors the upstream check 1:1 and lands a position-parity row in
`parser/lexer/position_test.go` for the failing snippet.

### P6: re-run panel + update MANIFEST

After P1-P5 land:

```
go test ./test/cpython/... -run TestTokenize -v
go test ./test/cpython/... -run TestSourceEncoding -v
```

Update the row in `test/cpython/MANIFEST.txt`, then update spec
1710's `test_tokenize.py` and `test_source_encoding.py` rows. Update
this spec's checklist to DONE.
