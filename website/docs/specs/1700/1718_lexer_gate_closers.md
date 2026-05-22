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
| P7 | Callable check parity: replace every `fn.Type().Call == nil` guard with `objects.Callable(fn)` so Vectorcall-only callables (bound methods, classmethods, etc.) pass the gate. Sites: `module/atexit/module.go`, `module/_functools/module.go`, `module/_collections/module.go`. | TODO | pending |
| P8 | Charmap codec runtime: port `codecs.charmap_decode` / `charmap_encode` (CPython `Python/codecs.c` + `Objects/unicodeobject.c:7194 PyUnicode_DecodeCharmap`). Land Lib-side `Lib/encodings/iso8859_15.py` and `Lib/encodings/cp1252.py` decoding/encoding tables verbatim from CPython. | TODO | pending |
| P9 | Multibyte codec runtime: port `Modules/cjkcodecs/multibytecodec.c` plus `_codecs_jp` (`cp932`) and `_codecs_kr` (`cp949`) with `mappings_jp.h` / `mappings_kr.h` tables. Required by `test_issue2301` (cp932) and `test_exec_valid_coding` (cp949). | TODO | pending |
| P10 | Encoding alias table: port `Lib/encodings/aliases.py` so `iso8859-15`, `iso-8859-15`, `iso_8859_15`, `cp1252`, `cp932`, `cp949`, `utf8` etc. all canonicalise through the same alias mapping CPython uses. Plumb through `codecs.Lookup` after `NormalizeName`. | TODO | pending |
| P11 | Per-line UTF-8 validation in the lexer: port `Parser/tokenizer/helpers.c:300 ensure_utf8` so the lexer raises Non-UTF-8 SyntaxError on the offending line regardless of cookie/BOM. Required by `test_non_utf8_{second,third}_line_error`, `test_utf8_bom_non_utf8_third_line_error`, `test_utf_8_non_utf8_third_line_error`. | DONE | pending |
| P12 | Lexer surfaces UnicodeDecodeError text: when the cookie codec decode fails, the SyntaxError message must follow `Parser/tokenizer/helpers.c:534 _PyTokenizer_syntaxerror_known_range` and the CPython `'<codec>' codec can't decode byte 0x%02x in position %d: ordinal not in range(128)` template. Required by `test_first_utf8_coding_line_error`, `test_second_utf8_coding_line_error`, `test_utf8_shebang_error`, `test_error_from_string`. | DONE | pending |
| P13 | `os.PathLike` port: add the abstract base class (`Lib/os.py:1145 PathLike`) plus the `__fspath__` protocol the rest of the os/posixpath subsystem already half-uses. Required by `test_20731`, `test_file_parse_error_multiline`, `test_tokenizer_fstring_warning_in_first_line`. | TODO | pending |
| P14 | Test fixtures: vendor `Lib/test/tokenizedata/bad_coding.py`, `bad_coding2.py`, `coding20731.py`, plus `Lib/test/encoded_modules/__init__.py`, `module_iso_8859_1.py`, `module_koi8_r.py` into `test/cpython/tokenizedata/` and `test/cpython/encoded_modules/`. Required by `test_bad_coding`, `test_bad_coding2`, `test_import_encoded_module`, `test_20731`. | DONE | pending |
| P15 | `__import__` SyntaxError surfacing: when an imported source file's tokeniser emits SyntaxError (bad cookie, bad UTF-8), `Lib/importlib/_bootstrap_external.py:846 _LoaderBasics.exec_module` must propagate the error. Required by `test_bad_coding2`. | DONE | pending |
| P16 | Long-cookie-line scanning: re-port `Parser/tokenizer/helpers.c:163 get_coding_spec` so cookie detection survives lines that fill the read buffer (`#<BUFSIZ spaces>coding:iso8859-15`). Required by `test_long_first_coding_line`, `test_long_second_coding_line`. | TODO | pending |
| P17 | Round-tripped SyntaxError text bytes: when SyntaxError surfaces non-utf-8 source text the lexer must record the raw bytes; the descriptor returns them as a Python str via `decode(errors='replace')` parity. Required by `test_non_utf8_{second,third}_line_error`, `test_non_utf8_shebang_error`. | DONE | pending |
| P18 | `compile()` pyc cleanup parity: after `__import__` succeeds, the `.pyc` file must exist so `test_file_parse`'s `unlink(filename + "c")` resolves. Port `Lib/importlib/_bootstrap_external.py:929 SourceFileLoader.set_data` and the `__pycache__` directory creation chain. | TODO | pending |
| P19 | Re-run `test_source_encoding.py`; flip MANIFEST and spec 1710 panel row to green. | TODO | pending |
| P20 | Split the str vs bytes tokeniser drivers. `lexer.FromString` must mirror `Parser/tokenizer/utf8_tokenizer.c:11 _PyTokenizer_FromUTF8` (skip BOM and cookie since `compile(str, ...)` arrives with `PyCF_IGNORE_COOKIE` set by `_Py_SourceAsString`). `lexer.FromBytes` must mirror `Parser/tokenizer/string_tokenizer.c:78 _PyTokenizer_FromString` (BOM strip + cookie + codec decode + `ensure_utf8`). Plumb the bytes path through the importer by retyping `imp.SourceCompiler` from `func(string, string)` to `func([]byte, string)` so `os.ReadFile` bytes flow into `parser.ParseBytes` rather than being downcast through `string()`. Required by `test_issue4626` (str-source compile with non-utf-8 cookie text) and `test_bad_coding` / `test_bad_coding2` (cookie + BOM checks during `__import__`). | DONE | pending |
| P21 | `_posixsubprocess.fork_exec` arity parity with CPython 3.14 clinic: the signature took 24 args in 3.13 and 22 args in 3.14 after the `gid_object` / `extra_groups_packed` / `uid_object` consolidation. Required by `test_file_parse` and every other subprocess-driven encoding case. | DONE | pending |
| P22 | `select.select` built-in + descriptor classification fixes. Port `Modules/selectmodule.c:277 select_select_impl` (seq2set/set2list/FD_SET via portable byte view of syscall.FdSet, asFileDescriptor, EINTR retry, timeout-to-timeval). Drop `*BuiltinFunction` from `isMethodLike` / `ClassifyDescriptor` so PyCFunction-as-class-attr does NOT bind self (CPython faithfulness: `Objects/methodobject.c:357 PyCFunction_Type` lacks Py_TPFLAGS_METHOD_DESCRIPTOR; consequence: `selectors.SelectSelector._select(...)` calls the underlying builtin with the right arity). Add `Number.Bool / Mapping.Length / Sequence.Length` slot checks to TO_BOOL_ALWAYS_TRUE so `while sel.get_map():` deopts when `__len__` is defined (CPython faithfulness: `Objects/object.c:1505 check_type_always_true`). Wire `sys.executable` so `Popen([sys.executable, ...])` resolves; split `BufferedIOBase.flush` into `simple_flush`. Required by every `FileSourceEncodingTest` row that drives a subprocess. | DONE | pending |
| P23 | `pythonrun.RunFile` must route source bytes through the bytes tokeniser. Before P23 `RunFile` did `RunString(string(src), ...)` which lands in `parser.ParseString` (str path, BOM+cookie skipped per `PyCF_IGNORE_COOKIE`). Add `pythonrun.RunBytes` paralleling `RunString` but calling `parser.ParseBytes`, then switch `RunFile` to it. Mirrors `Python/pythonrun.c:1276 pyrun_file` which always hands bytes to `_PyTokenizer_FromString`. Cuts `test_source_encoding.py` failures from 30 to 15 by unlocking every `FileSourceEncodingTest` row whose script carries a non-utf-8 cookie. | DONE | pending |
| P24 | SyntaxError stderr rendering parity. The parser surfaces a Go-side `*parsererrors.SyntaxError` that never reaches the VM, so the existing `PyErr_Print` flow had nothing to render and `pythonrun.printRunError` fell through to `fmt.Fprintln(err)` (one-line `<string>:1:1: ...`). Port `Lib/traceback.py:1376 TracebackException._format_syntax_error` and route the parser error through the same display path. Hoist `SyntaxFromParser` from vm into the errors package (builds the canonical 2-arg `SyntaxError(msg, (filename, lineno, offset, text, end_lineno, end_offset))` via the type's `__init__`), then have `printRunError` synthesize the exception, restore it on the thread state, and call `PrintEx`. `writeChain` branches on `Match(exc, PyExc_SyntaxError)` to emit the file/line/text/caret frame ahead of `SyntaxError: msg`. Cuts `test_source_encoding.py` failures from 15 to 10 by unlocking every assertion that grep's stderr for `'SyntaxError: '` or the `File "...", line N` frame. | DONE | pending |
| P25 | F-string escape decoding in the parser actions layer. CPython's `_PyPegen_joined_str` (Parser/action_helpers.c:1396) routes through `_get_resized_exprs` (1301) which walks the parsed raw_expressions and calls `_PyPegen_decode_fstring_part` (1270) on each Constant. That helper hands the bytes to `_PyPegen_decode_string` -> `decode_unicode_with_escapes` (Parser/string_parser.c:135) so `\n`, `\t`, `\xHH`, `\uHHHH`, `\N{...}`, `\NNN` decode at parse time. gopy's `actionPgenJoinedStr` was emitting the raw `JoinedStr` straight from `joinedStrValues` with no escape pass, so `f', line {n}\n'` evaluated to `', line 1\\n'` (literal backslash + n) instead of `', line 1\n'`. Add `string.DecodeFStringPart(isRaw, s)` mirroring the C helper (short-circuit raw or no-backslash; otherwise run `decodeUnicodeEscapes`), port `_get_resized_exprs` as `resizeFStringExprs` in pegen (decode each Constant, drop empty results, inline debug-mode 2-element JoinedStr), and read `isRaw` off the FSTRING_START token's prefix bytes via `strpbrk`-style `r/R` scan. Also fix `actionPgenDecodedConstantFromToken` (format-spec body) to peek the live tokenizer mode through new `lexer.State.InsideFString` / `CurrentFStringRaw` accessors so format-spec escapes follow the outer string's raw flag. Cuts the `b', line N\n' not found` failure class from `test_source_encoding.py` (failures drop from 10 to 4). | DONE | pending |
| P26 | Cross-platform build for the select module. P22 added `module/select` using `syscall.Select` (which returns `(n int, err error)` on Linux but only `err` on macOS/BSD) and `syscall.Timeval{Sec, Usec: int32(...)}` (which has int64 Usec on Linux). The package compiled on macOS only; Linux + Windows CI broke. Replace the timeval construction with `syscall.NsecToTimeval(d.Nanoseconds())` (handles the int32/int64 difference per OS), split the actual `syscall.Select` call into `doSelect` helpers in `select_linux.go` (drops the n return) and `select_bsd.go` (passes through), and add `module_windows.go` that's an empty package on Windows so the rest of gopy builds. Windows users get ImportError on `import select` until the WSAEventSelect arm is ported. | DONE | pending |

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

### P7: Callable check parity

`module/atexit/module.go:65` reads `fn.Type().Call == nil` and rejects
the callable. `objects.BoundMethodType` sets `Vectorcall` but not
`Call`, so every method passed to `atexit.register` (eg
`finalize._exitfunc`) raises `TypeError: the first argument must be
callable`. This blocks the 37 `FileSourceEncodingTest` cases via
`tempfile.TemporaryDirectory` → `weakref.finalize` → `atexit.register`.

The port: replace every direct `Type().Call == nil` check on a
"must be callable" guard with `objects.Callable(fn)` which mirrors
`PyCallable_Check` (`Objects/object.c:2100`). Same fix applies to
the `partial` / `lru_cache` constructors in `_functools` and the
`deque` keyfunc check in `_collections`.

### P8: charmap codec runtime + iso8859-15 + cp1252

CPython `Lib/encodings/iso8859_15.py` ships a 256-entry
`decoding_table` (and inverse `encoding_table`) as a literal
`str` of length 256 plus a translation dict. The actual decode
work is in C: `Objects/unicodeobject.c:7194 PyUnicode_DecodeCharmap`
and `Python/codecs.c PyCodec_CharmapDecode`. The gopy port:

- adds `codecs.CharmapDecode(input, errors, table)` / `CharmapEncode`
  in `codecs/charmap.go` mirroring the CPython loop byte-for-byte
- ports `Lib/encodings/iso8859_15.py` to `stdlib/encodings/iso8859_15.py`
  using the same module shape
- ports `Lib/encodings/cp1252.py` the same way
- registers the search function so `codecs.lookup('iso8859-15')`
  and aliases (P10) resolve through the cache

### P9: multibyte codec runtime

CPython splits multibyte codecs across:

- `Modules/cjkcodecs/multibytecodec.c` (~1500 lines): the
  `MultibyteCodec` runtime, Codec/IncrementalCodec/StreamReader
- `Modules/cjkcodecs/cjkcodecs.h`: shared decode/encode helpers
- `Modules/cjkcodecs/_codecs_jp.c` + `mappings_jp.h` (~4800 lines)
- `Modules/cjkcodecs/_codecs_kr.c` + `mappings_kr.h` (~3200 lines)

The port lands the runtime in `codecs/multibyte/`, the tables in
`module/_codecs_jp/` and `module/_codecs_kr/`, with the Python
wrappers `stdlib/encodings/cp932.py` and `cp949.py` ported
verbatim. Required only by `test_issue2301` (cp932) and
`test_exec_valid_coding` (cp949); ship as its own commit.

### P10: encoding alias table

CPython's `Lib/encodings/aliases.py` is one large dict that maps
normalised names (lowercase, hyphens → underscores) to canonical
encoding modules. Today `codecs.Lookup` falls back to the
`builtinSearch` switch with a handful of hardcoded aliases.

The port: import `Lib/encodings/aliases.py` verbatim into
`stdlib/encodings/aliases.py`. Inside `codecs.Lookup`, after
`NormalizeName` and before the search-function chain, consult the
alias table and substitute the canonical name. Search functions
then look up that canonical name (`encodings.<name>.getregentry`)
following CPython's lazy-import flow (`Lib/encodings/__init__.py`).

### P11: per-line UTF-8 validation

CPython validates UTF-8 in two places:

1. `Parser/tokenizer/helpers.c:332 ensure_utf8` once at startup
   when no cookie / BOM declares the encoding.
2. `Parser/tokenizer/file_tokenizer.c` line-by-line during
   `tok_readline_*` whenever the declared encoding is utf-8
   (cookie `utf-8` or BOM), so a bad byte on line 3 still raises
   `Non-UTF-8 code starting with '\xXX' on line 3`.

gopy currently only runs (1). The port adds the line-by-line
validation pass after `TranslateNewlines` when the lexer's
encoding is `utf-8`, recording the SyntaxError at the offending
line/column and matching the CPython message template exactly.

Status (P12-era): the string driver already validates the whole
buffer via `ValidateUTF8` at `parser/lexer/driver_string.go:79`,
so the bytes path already raises Non-UTF-8 SyntaxError on the
correct line. The streaming `FromReader` driver now does the
same per-line check in its underflow callback when no encoding
is declared (matching `file_tokenizer.c:352`), so test fixtures
that route through the file driver also raise at the offending
line. The remaining `test_non_utf8_{second,third}_line_error`
mismatches reduce to a unicode-equality drift on strings built
through `bytes.decode(errors='replace')` versus the lexer's text
field. That drift lives outside the cookie/UTF-8 validation
subsystem.

### P12: ASCII / UTF-8 decode error templates

When the cookie is `ascii` and the source contains `\xc3\xa4`,
CPython raises a `UnicodeDecodeError` from `PyUnicode_DecodeASCII`
(`Objects/unicodeobject.c:7656`, reason
`"ordinal not in range(128)"`). The string tokenizer fails its
`translate_into_utf8` step, the pending `UnicodeDecodeError` is
turned into a `SyntaxError` by
`_PyPegen_raise_tokenizer_init_error`
(`Parser/pegen_errors.c:13`), and `args[0]` ends up as the bare
`str()` of the original `UnicodeDecodeError`: `'ascii' codec
can't decode byte 0xe2 in position N: ordinal not in range(128)`
(no `(unicode error)` prefix — that prefix is only attached by
`_Pypegen_raise_decode_error` for the in-parser path, not the
init-error path).

Port:

- `codecs/errors.go`: extend `ErrorHandler` with a `reason`
  argument, mirror CPython's
  `unicode_decode_call_errorhandler_writer` (encoding + reason
  are passed alongside the position).
- `strictHandler`: emit the singular byte form when
  `end == start + 1`, plural otherwise. Same format as
  `Objects/exceptions.c:3815 UnicodeDecodeError_str`.
- `codecs/builtin.go`, `codecs/raw_unicode_escape.go`,
  `module/_codecs/module.go`: thread the codec-specific reason
  (`"ordinal not in range(128)"` for ascii,
  `"ordinal not in range(256)"` for iso-8859-1,
  `"invalid start byte"` / `"surrogates not allowed"` for utf-8,
  `"character maps to <undefined>"` for charmap) into every
  handler call.
- `parser/lexer/driver_string.go`: stop replacing the codec
  error with `encoding problem: <name>`. Surface the codec
  message verbatim into the `SyntaxError.Text` and `.Message`.
  No `(unicode error)` wrap: the prefix is added by the parser
  path, not the tokenizer-init path.

After this lands, `test_error_from_string` passes; the regex
gate (`(\\(unicode error\\) )?'ascii' codec can't decode byte`)
in `test_first_utf8_coding_line_error` /
`test_second_utf8_coding_line_error` matches the bare form via
the optional alternative in the regex.

### P13: os.PathLike

CPython exposes `os.PathLike` as an `abc.ABCMeta` ABC with a
`__fspath__` virtual method (`Lib/os.py:1145`). The port lands
the ABC verbatim into `stdlib/os.py`, registers `str` and `bytes`
as virtual subclasses, and wires the `os.fspath` helper at
`Lib/os.py:1083` so `os.PathLike` instances work everywhere
posixpath joins them.

### P14: vendor fixtures

`Lib/test/tokenizedata/bad_coding.py` is `# -*- coding: uft-8 -*-`
(typo). `bad_coding2.py` is BOM + `#coding: utf8` + non-utf-8
source bytes. `coding20731.py` exercises tokeniser cookie parity.
The encoded_modules fixtures are short utf-8/koi8-r modules with
a `test` attribute. Port all six files verbatim into
`test/cpython/tokenizedata/` and `test/cpython/encoded_modules/`.

Status: vendored under `test/cpython/tokenizedata/` (bad_coding.py,
bad_coding2.py, coding20731.py with CRLF preserved via `cp` from
cpython-314) and `test/cpython/encoded_modules/` (__init__.py,
module_iso_8859_1.py, module_koi8_r.py). `test_bad_coding` now
passes; `test_bad_coding2` still depends on P15, and
`test_import_encoded_module` still depends on P9 because koi8-r is
a charmap codec not yet wired through `codecs.Lookup`.

### P15: __import__ SyntaxError surfacing

`Lib/importlib/_bootstrap_external.py:846 _LoaderBasics.exec_module`
catches and re-raises SyntaxError so `__import__('test.tokenizedata.
bad_coding2')` raises SyntaxError. gopy's importer currently
swallows it; the port walks the importlib bootstrap and lifts
SyntaxError up through the `_call_with_frames_removed` chain.

Status: the importer already lifts SyntaxError correctly. The
remaining gap surfaced by `test_bad_coding2` was that
`bad_coding2.py` (BOM + `#coding: utf8`) was silently accepted by
the lexer because `isUTF8Name` folded the `utf8` cookie alias to
`utf-8`. CPython's `get_normal_name` only folds `utf-8` /
`utf-8-*`; `utf8` and `u8` pass through untouched and the
BOM-vs-cookie `strcmp` in `_PyTokenizer_check_coding_spec`
(helpers.c:425) raises `encoding problem: utf8 with BOM`. Made
`isUTF8Name` strict (only the canonical `utf-8` matches) so the
BOM check fires on `utf8` cookies as CPython does.

### P16: long cookie line scanning

`Parser/tokenizer/helpers.c:163 get_coding_spec` reads the entire
line into a stack buffer (`MAXBUFSIZE = 500`) and scans for
`coding[:=]`. When the line exceeds the buffer it still consults
the slice it managed to read. gopy's `parser/lexer/source.go`
`detectEncodingCookieAt` short-circuits when the line is too long.
The port matches CPython's "scan as much of the line as we have"
behaviour: even an 8KB-spaced cookie line resolves.

### P17: rountripped SyntaxError text

When a Non-UTF-8 byte appears on line N, `e.text` must be the raw
source line decoded with `errors='replace'`. Today the lexer
captures `nthLine(src, lineno)` and passes the bytes through Go's
implicit utf-8 string conversion, which substitutes U+FFFD using
Go's RuneError mapping rather than CPython's `error='replace'`
codec. The port routes the line through `codecs.Decode(bytes,
"utf-8", "replace")` so the replacement bytes match.

Status: done. `vm/eval_unwind.go syntaxExceptionFromParserError`
now routes `se.Text` through `codecs.Decode(se.Text, "utf-8",
"replace")` before wrapping it in `objects.NewStr`. The Python
`*Unicode.v` field ends up holding canonical UTF-8 bytes
(`#second\xef\xbf\xbd`) instead of the raw single-byte form
(`#second\xa4`), so equality against
`src.splitlines()[i].decode(errors='replace')` succeeds.
`test_non_utf8_{second,third}_line_error` and
`test_non_utf8_shebang_error` now pass.

### P18: pyc cleanup parity

`test_file_parse` writes a `.py` file, imports it, then unlinks
`.pyc` from the same directory. CPython's `SourceFileLoader.set_data`
writes `<dir>/__pycache__/<name>.cpython-314.pyc` and the test
removes both via `unlink(filename + "c")` (no error suppression)
and `rmtree('__pycache__')`. gopy doesn't emit `.pyc` files. The
port wires `SourceFileLoader.set_data` to write the byte-compiled
file path-equal to CPython's location.

### P19: re-run + flip gate

```
go run ./cmd/gopy test/cpython/test_source_encoding.py
```

Expected: 91/91 passing (BytesSourceEncodingTest 31,
FileSourceEncodingTest 31, MiscSourceEncodingTest 28, one
linux-only test skipped). Update `test/cpython/MANIFEST.txt` to
green and spec 1710's row.

### P20: split str vs bytes tokeniser drivers

Two CPython entry points, two compile-time contracts, one Go shim:

- `_PyTokenizer_FromString` (`Parser/tokenizer/string_tokenizer.c:78`)
  is the bytes path. It strips a UTF-8 BOM, runs PEP 263 cookie
  detection, codec-decodes the source when the cookie names a
  non-utf-8 codec, then calls `_PyTokenizer_ensure_utf8` to verify
  the resulting buffer.
- `_PyTokenizer_FromUTF8` (`Parser/tokenizer/utf8_tokenizer.c:11`)
  is the str path. The caller is `compile(str_source, ...)` which
  routes through `_Py_SourceAsString` with `PyCF_IGNORE_COOKIE`
  set, so BOM and cookie handling are deliberately skipped: the
  source is already canonical UTF-8 from the str object.

gopy folded both into one entry, so `compile("# coding=latin-1\n\xc6 = 1", ...)`
ran cookie detection on a str whose `\xc6` was the U+00C6 codepoint
encoded as `\xc3\x86`; the cookie said latin-1 so the codec turned
that 2-byte UTF-8 into "Æ" garbage and the parser raised
SyntaxError. CPython skips the cookie on str input and the snippet
compiles cleanly. `test_issue4626` pins this.

Conversely, the importer reads files as bytes and the cookie must
fire so `import bad_coding` sees the bad cookie. With both drivers
folded into a string-shaped API gopy lost the bytes route through
`SourceFileLoader`: `os.ReadFile` returned `[]byte`, the importer
downcast through `string(src)`, and `gopyCompile` ran the str path
that now skips the cookie. `test_bad_coding` and `test_bad_coding2`
both regressed.

Fix:

1. `lexer.FromString` ports `_PyTokenizer_FromUTF8`: no BOM, no
   cookie, just `ValidateUTF8` (so a Go string carrying invalid
   bytes still surfaces the Non-UTF-8 SyntaxError on the right
   line) followed by `TranslateNewlines`.
2. `lexer.FromBytes` keeps the full `_PyTokenizer_FromString`
   protocol: BOM strip, cookie detect, codec decode for non-utf-8
   cookies, BOM-vs-cookie strict strcmp via `isUTF8Name`, then
   `ValidateUTF8` whenever the effective encoding is utf-8.
3. `imp.SourceCompiler` retypes from `func(string, string)` to
   `func([]byte, string)`. `LoadSourceFile` hands `os.ReadFile`
   bytes straight to the compiler, which calls `parser.ParseBytes`.
   This mirrors CPython's `Lib/importlib/_bootstrap_external.py:866
   SourceLoader.source_to_code` which feeds bytes to `compile(...)`
   verbatim from `get_data`.

All SourceCompiler implementations (`cmd/gopy.gopyCompile`, the test
compilers in `imp/pathfinder_test.go`, `stdlibinit/*_test.go`) switch
to the bytes signature in the same commit so the type change is
atomic and there is no half-converted call site.

### P21: fork_exec 22-arg signature

CPython 3.14 dropped `use_vfork` (and folded the gid/extra-groups/uid
trio that 3.13 took as separate parameters) so the clinic signature
went from 24 to 22 positional arguments. gopy's bridge required 23
positional args and stamped a `useVfork` doc that no longer matches.
The fix updates `module/_posixsubprocess/module.go` to accept 22 args
and trims the stale parameter doc. Without this every FileSourceEncodingTest
that shells out fails with a TypeError before reaching the subprocess.
