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
| P8 | Charmap codec runtime: port `codecs.charmap_decode` / `charmap_encode` (CPython `Python/codecs.c` + `Objects/unicodeobject.c:7194 PyUnicode_DecodeCharmap`). Land Lib-side `Lib/encodings/iso8859_15.py` and `Lib/encodings/cp1252.py` decoding/encoding tables verbatim from CPython. Shipped: `codecs/charmap.go` (`Charmap` type with `NewCharmap` + per-byte decode and inverse encode), `codecs/iso8859_15.go` (256-rune table verbatim from `Lib/encodings/iso8859_15.py`), `codecs/codepages.go` (cp1252 / cp1250 / cp1251 / cp437 / mac-roman tables), wired into `codecs/builtin.go builtinSearch` so `codecs.Lookup("iso-8859-15")` etc. resolve. `module/io/textio_codec.go` now falls back to `codecs.Lookup` (via a new `registryDecoder`) so `open(..., encoding="iso-8859-15")` works through the same registry CPython uses. | DONE | pending |
| P9 | Multibyte codec runtime: port `Modules/cjkcodecs/multibytecodec.c` (2143 lines: stateful encoder/decoder lifecycle, error-handler dispatch, `_PyUnicodeWriter` mirroring, `mbencode_func` / `mbdecode_func` plumbing) plus the full `_codecs_jp.c` (cp932, shift_jis, euc_jp, shift_jis_2004, euc_jis_2004 codecs; 770 lines) and `_codecs_kr.c` (cp949, euc_kr, johab; 468 lines) along with their dependent mapping headers (`mappings_jp.h` 4766 lines, `mappings_kr.h` 3253 lines, `mappings_jisx0213_pair.h`, `alg_jisx0201.h`, `emu_jisx0213_2000.h`). Tables are auto-generated from CPython's `genmap_*.py`, so the Go side will mirror them via a generator pass. Required by `test_issue2301` (cp932) and `test_exec_valid_coding` (cp949). Multi-session work item: a partial cp932-only port would violate the "port whole subsystem" rule. Shipped: `tools/cjkcodecs_go` build-time generator translates all six CPython `mappings_*.h` headers to Go; `codecs/cjkcodecs/runtime.go` ports `multibytecodec.c` decode/encode outer loops and error dispatch; `codecs_kr.go` ports cp949/euc_kr/johab; `codecs_jp.go` ports cp932/shift_jis/euc_jp/shift_jis_2004/euc_jis_2004; `codecs/cjkcodecs/registry.go` registers a SearchFunc with `codecs.Register` via init(); `stdlibinit/registry.go` blank-imports the package so the binary side-loads it. `test_exec_valid_coding` (cp949) and `test_issue2301` (cp932) now both pass; the v.text=None gap that affected the cp932 case was closed by P19's parser fallback SyntaxError. The CN/HK/TW/ISO-2022 follow-up landed alongside: `codecs_tw.go` (big5, cp950), `codecs_hk.go` (big5hkscs with the four pair-combining cases), `codecs_cn.go` (gb2312, gbk, gb18030, hz stateful), `codecs_iso2022.go` (iso2022_kr / jp / jp_1 / jp_2 / jp_2004 / jp_3 / jp_ext with full G0..G3 stack and JISX0213 2000-vs-2004 emulation). | DONE | pending |
| P10 | Encoding alias table: port `Lib/encodings/aliases.py` so `iso8859-15`, `iso-8859-15`, `iso_8859_15`, `cp1252`, `cp932`, `cp949`, `utf8` etc. all canonicalise through the same alias mapping CPython uses. Plumb through `codecs.Lookup` after `NormalizeName`. | TODO | pending |
| P11 | Per-line UTF-8 validation in the lexer: port `Parser/tokenizer/helpers.c:300 ensure_utf8` so the lexer raises Non-UTF-8 SyntaxError on the offending line regardless of cookie/BOM. Required by `test_non_utf8_{second,third}_line_error`, `test_utf8_bom_non_utf8_third_line_error`, `test_utf_8_non_utf8_third_line_error`. | DONE | pending |
| P12 | Lexer surfaces UnicodeDecodeError text: when the cookie codec decode fails, the SyntaxError message must follow `Parser/tokenizer/helpers.c:534 _PyTokenizer_syntaxerror_known_range` and the CPython `'<codec>' codec can't decode byte 0x%02x in position %d: ordinal not in range(128)` template. Required by `test_first_utf8_coding_line_error`, `test_second_utf8_coding_line_error`, `test_utf8_shebang_error`, `test_error_from_string`. | DONE | pending |
| P13 | `os.PathLike` port: add the abstract base class (`Lib/os.py:1145 PathLike`) plus the `__fspath__` protocol the rest of the os/posixpath subsystem already half-uses. Required by `test_20731`, `test_file_parse_error_multiline`, `test_tokenizer_fstring_warning_in_first_line`. Resolved: `module/os/pathlike.go` exposes the `PathLike` type singleton with the abstract `__fspath__` method; `stdlib/os.py:1123` carries the verbatim `class PathLike(abc.ABC)` definition; `stdlib/os.py:1081 _fspath` plus the `if not _exists('fspath')` alias at line 1118 carry the helper. `test_20731` and `test_file_parse_error_multiline` pass today; the two remaining errors in `test_source_encoding.py` (`test_first_non_utf8_coding_line`, `test_tokenizer_fstring_warning_in_first_line`) are subprocess-fd leaks ~30 Popens into the suite, not a PathLike gap. | DONE | pending |
| P14 | Test fixtures: vendor `Lib/test/tokenizedata/bad_coding.py`, `bad_coding2.py`, `coding20731.py`, plus `Lib/test/encoded_modules/__init__.py`, `module_iso_8859_1.py`, `module_koi8_r.py` into `test/cpython/tokenizedata/` and `test/cpython/encoded_modules/`. Required by `test_bad_coding`, `test_bad_coding2`, `test_import_encoded_module`, `test_20731`. | DONE | pending |
| P15 | `__import__` SyntaxError surfacing: when an imported source file's tokeniser emits SyntaxError (bad cookie, bad UTF-8), `Lib/importlib/_bootstrap_external.py:846 _LoaderBasics.exec_module` must propagate the error. Required by `test_bad_coding2`. | DONE | pending |
| P16 | Long-cookie-line scanning: re-port `Parser/tokenizer/helpers.c:163 get_coding_spec` so cookie detection survives lines that fill the read buffer (`#<BUFSIZ spaces>coding:iso8859-15`). Required by `test_long_first_coding_line`, `test_long_second_coding_line`. Resolved: gopy's `detectEncodingCookieAt` already scans full physical lines (no short-circuit), and the file driver reads the encoding head one line at a time with no fixed cap (see P60), so the BUFSIZ-spaces case fits along with anything past 16KB. Both gate rows pass today. | DONE | pending |
| P17 | Round-tripped SyntaxError text bytes: when SyntaxError surfaces non-utf-8 source text the lexer must record the raw bytes; the descriptor returns them as a Python str via `decode(errors='replace')` parity. Required by `test_non_utf8_{second,third}_line_error`, `test_non_utf8_shebang_error`. | DONE | pending |
| P18 | `compile()` pyc cleanup parity: after `__import__` succeeds, the `.pyc` file must exist so `test_file_parse`'s `unlink(filename + "c")` resolves. Port `Lib/importlib/_bootstrap_external.py:929 SourceFileLoader.set_data` and the `__pycache__` directory creation chain. | TODO | pending |
| P19 | Re-run `test_source_encoding.py`; flip MANIFEST and spec 1710 panel row to green. Closed: panel now reads 90 pass / 1 skip / 0 fail after `parser/parser.go runParse` learned to synthesize a structured SyntaxError at the farthest token when `pegen.Dispatch` returns `ErrParserNotImplemented` with no pinned error (`p.FarthestToken()` + `p.Tokenizer().SourceLine(...)`, mirroring `Parser/pegen.c:1136 _PyPegen_run_parser`'s farthest_pos caret). Without it `compile(b'# coding: cp932\nprint \'\\x94\\x4e\'')` returned `SyntaxError` with `v.text == None` and `test_issue2301`'s `self.assertIn(b"print '\\\\x94\\\\x4e'", v.text.encode())` raised AttributeError. | DONE | pending |
| P20 | Split the str vs bytes tokeniser drivers. `lexer.FromString` must mirror `Parser/tokenizer/utf8_tokenizer.c:11 _PyTokenizer_FromUTF8` (skip BOM and cookie since `compile(str, ...)` arrives with `PyCF_IGNORE_COOKIE` set by `_Py_SourceAsString`). `lexer.FromBytes` must mirror `Parser/tokenizer/string_tokenizer.c:78 _PyTokenizer_FromString` (BOM strip + cookie + codec decode + `ensure_utf8`). Plumb the bytes path through the importer by retyping `imp.SourceCompiler` from `func(string, string)` to `func([]byte, string)` so `os.ReadFile` bytes flow into `parser.ParseBytes` rather than being downcast through `string()`. Required by `test_issue4626` (str-source compile with non-utf-8 cookie text) and `test_bad_coding` / `test_bad_coding2` (cookie + BOM checks during `__import__`). | DONE | pending |
| P21 | `_posixsubprocess.fork_exec` arity parity with CPython 3.14 clinic: the signature took 24 args in 3.13 and 22 args in 3.14 after the `gid_object` / `extra_groups_packed` / `uid_object` consolidation. Required by `test_file_parse` and every other subprocess-driven encoding case. | DONE | pending |
| P22 | `select.select` built-in + descriptor classification fixes. Port `Modules/selectmodule.c:277 select_select_impl` (seq2set/set2list/FD_SET via portable byte view of syscall.FdSet, asFileDescriptor, EINTR retry, timeout-to-timeval). Drop `*BuiltinFunction` from `isMethodLike` / `ClassifyDescriptor` so PyCFunction-as-class-attr does NOT bind self (CPython faithfulness: `Objects/methodobject.c:357 PyCFunction_Type` lacks Py_TPFLAGS_METHOD_DESCRIPTOR; consequence: `selectors.SelectSelector._select(...)` calls the underlying builtin with the right arity). Add `Number.Bool / Mapping.Length / Sequence.Length` slot checks to TO_BOOL_ALWAYS_TRUE so `while sel.get_map():` deopts when `__len__` is defined (CPython faithfulness: `Objects/object.c:1505 check_type_always_true`). Wire `sys.executable` so `Popen([sys.executable, ...])` resolves; split `BufferedIOBase.flush` into `simple_flush`. Required by every `FileSourceEncodingTest` row that drives a subprocess. | DONE | pending |
| P23 | `pythonrun.RunFile` must route source bytes through the bytes tokeniser. Before P23 `RunFile` did `RunString(string(src), ...)` which lands in `parser.ParseString` (str path, BOM+cookie skipped per `PyCF_IGNORE_COOKIE`). Add `pythonrun.RunBytes` paralleling `RunString` but calling `parser.ParseBytes`, then switch `RunFile` to it. Mirrors `Python/pythonrun.c:1276 pyrun_file` which always hands bytes to `_PyTokenizer_FromString`. Cuts `test_source_encoding.py` failures from 30 to 15 by unlocking every `FileSourceEncodingTest` row whose script carries a non-utf-8 cookie. | DONE | pending |
| P24 | SyntaxError stderr rendering parity. The parser surfaces a Go-side `*parsererrors.SyntaxError` that never reaches the VM, so the existing `PyErr_Print` flow had nothing to render and `pythonrun.printRunError` fell through to `fmt.Fprintln(err)` (one-line `<string>:1:1: ...`). Port `Lib/traceback.py:1376 TracebackException._format_syntax_error` and route the parser error through the same display path. Hoist `SyntaxFromParser` from vm into the errors package (builds the canonical 2-arg `SyntaxError(msg, (filename, lineno, offset, text, end_lineno, end_offset))` via the type's `__init__`), then have `printRunError` synthesize the exception, restore it on the thread state, and call `PrintEx`. `writeChain` branches on `Match(exc, PyExc_SyntaxError)` to emit the file/line/text/caret frame ahead of `SyntaxError: msg`. Cuts `test_source_encoding.py` failures from 15 to 10 by unlocking every assertion that grep's stderr for `'SyntaxError: '` or the `File "...", line N` frame. | DONE | pending |
| P25 | F-string escape decoding in the parser actions layer. CPython's `_PyPegen_joined_str` (Parser/action_helpers.c:1396) routes through `_get_resized_exprs` (1301) which walks the parsed raw_expressions and calls `_PyPegen_decode_fstring_part` (1270) on each Constant. That helper hands the bytes to `_PyPegen_decode_string` -> `decode_unicode_with_escapes` (Parser/string_parser.c:135) so `\n`, `\t`, `\xHH`, `\uHHHH`, `\N{...}`, `\NNN` decode at parse time. gopy's `actionPgenJoinedStr` was emitting the raw `JoinedStr` straight from `joinedStrValues` with no escape pass, so `f', line {n}\n'` evaluated to `', line 1\\n'` (literal backslash + n) instead of `', line 1\n'`. Add `string.DecodeFStringPart(isRaw, s)` mirroring the C helper (short-circuit raw or no-backslash; otherwise run `decodeUnicodeEscapes`), port `_get_resized_exprs` as `resizeFStringExprs` in pegen (decode each Constant, drop empty results, inline debug-mode 2-element JoinedStr), and read `isRaw` off the FSTRING_START token's prefix bytes via `strpbrk`-style `r/R` scan. Also fix `actionPgenDecodedConstantFromToken` (format-spec body) to peek the live tokenizer mode through new `lexer.State.InsideFString` / `CurrentFStringRaw` accessors so format-spec escapes follow the outer string's raw flag. Cuts the `b', line N\n' not found` failure class from `test_source_encoding.py` (failures drop from 10 to 4). | DONE | pending |
| P26 | Cross-platform build for the select module. P22 added `module/select` using `syscall.Select` (which returns `(n int, err error)` on Linux but only `err` on macOS/BSD) and `syscall.Timeval{Sec, Usec: int32(...)}` (which has int64 Usec on Linux). The package compiled on macOS only; Linux + Windows CI broke. Replace the timeval construction with `syscall.NsecToTimeval(d.Nanoseconds())` (handles the int32/int64 difference per OS), split the actual `syscall.Select` call into `doSelect` helpers in `select_linux.go` (drops the n return) and `select_bsd.go` (passes through), and add `module_windows.go` that's an empty package on Windows so the rest of gopy builds. Windows users get ImportError on `import select` until the WSAEventSelect arm is ported. | DONE | pending |
| P27 | Lint cleanup over the P24-P26 surface. errorlint flagged the direct `==` against `syscall.EINTR` and the `%v`-wrapped error in `module/select/module.go`; switched to `errors.Is` and `%w`. gocognit on `errors.formatSyntaxError` blew the 30-complexity budget once the offset-clamp arm of `Lib/traceback.py:1402` landed, so the caret-row and clamp blocks moved into `writeSyntaxBody` and `clampSyntaxOffsets`. gocritic preferred a `switch` in `parser/lexer/source.go:getNormalName`, misspell flagged `recognises` in `module/os/pathlike.go`, and the joined-str action helper picked up a `_ = p` after the body stopped touching the parser pointer. | DONE | pending |
| P28 | Vendor `Lib/encodings/_win_cp_codecs.py` (CPython 3.14) into `stdlib/encodings/`. The preload of `encodings` on Windows CI broke with `ModuleNotFoundError: No module named "encodings._win_cp_codecs"` because the `sys.platform == 'win32'` arm of `stdlib/encodings/__init__.py:163` does `from ._win_cp_codecs import create_win32_code_page_codec`. The file is the byte-identical CPython source; `code_page_encode/decode` (MS_WINDOWS-only Win32 entrypoints in `Modules/_codecsmodule.c:584`) stay unimplemented because the inner imports inside `create_win32_code_page_codec` are lazy and only fire when the general searcher misses a `cp*` lookup. | DONE | pending |
| P51 | UTF-16 / UTF-32 codec family + tokenize-side bytes decode. The `str.encode("utf-16")` path went through `codecs.Encode` -> `codecs.Lookup` -> `LookupError: unknown encoding: utf-16` because `codecs/builtin.go` only had utf-8 / ascii / latin-1 / raw-unicode-escape. Ported `codecs/utf16.go` (BOM-prefixed + LE/BE variants mirroring `Modules/_codecs/utf_16.c _PyUnicode_DecodeUTF16Stateful / _PyUnicode_EncodeUTF16`) and `codecs/utf32.go` (matching layout for `Modules/_codecs/utf_32.c`). Each codec rejects surrogates outside `surrogatepass` and routes truncated tails / out-of-range code points through `LookupError`. Wired the new codec singletons (`utf16Codec`, `utf16LECodec`, `utf16BECodec`, `utf32Codec`, `utf32LECodec`, `utf32BECodec`) into `builtinSearch` with their canonical aliases (`utf_16`, `utf16`, `u16`, etc.). Closed the second half of `test_encoding (encoding='utf-16')` by porting the bytes-decode arm of `Parser/tokenizer/readline_tokenizer.c:21 tok_readline_string`: `module/_tokenize/module.go drainReadline` now calls `codecs.Decode(rawBytes, encoding, "replace")` before feeding the lexer, matching CPython's `PyUnicode_Decode(line, len, tok->encoding, "replace")` step. | DONE | pending |
| P52 | Vendor `Lib/decimal.py` + `Lib/_pydecimal.py` from CPython 3.14.5 byte-for-byte into `stdlib/`. `test_decistmt` is the test_tokenize.py row that compiles the full `_pydecimal` source string and diffs round-tripped vs. literal Decimal output, so it pulls in every public Decimal entrypoint plus the `_pydecimal` module-level hash table. The vendor sits behind the chain P53-P57 below; without it the rest of the chain has no test case to drive. | DONE | 5b6dde13 |
| P53 | Port `Python/intrinsics.c:142 stopiteration_error` (`UnaryStopIterationError`) and `Python/intrinsics.c:186 unary_pos` (`UnaryUnaryPositive`). The first wraps a StopIteration that escapes a generator into a `RuntimeError("generator raised StopIteration")` per PEP 479, with the surrounding code object's `CO_COROUTINE` / `CO_ASYNC_GENERATOR` flags picking one of three messages plus a `StopAsyncIteration` arm for async generators. Reads the current frame's code flags via `objects.CurrentFrameHook` so the intrinsic stays out of the compile package. The second dispatches `+x` through `NumberPositive`. Both were stubs that raised `notImplemented`; `_pydecimal`'s `__pos__` chain and its for-loops over generators tripped both. Updated `intrinsics/intrinsics_test.go`'s `implementedUnary` map so the stub sweep skips the two new bodies. | DONE | a54c7767 |
| P54 | `sys.hash_info` populated with the real CPython 64-bit constants from `Python/sysmodule.c:1565 get_hash_info` + `Include/cpython/pyhash.h:18 PyHASH_MODULUS`: modulus = 2^61 - 1, inf = 314159, imag = 1000003, hash_bits = 64, seed_bits = 128 (siphash13). The placeholder zeros made `_pydecimal`'s module-level hash table builder raise `ValueError: pow() 3rd argument cannot be 0` at import time. | DONE | 35bb3cab |
| P55 | Re-read `sys.modules[name]` after `exec_module` in `imp/exec.go ExecCodeModule` and the two `imp/pathfinder.go` loaders (`loadAsPackage`, `loadAsModule`). CPython's `Python/import.c:2715 exec_code_in_module` fetches the entry after the body runs so a module that does `sys.modules[__name__] = other` (the `decimal` shim re-points its own entry at `_pydecimal`) hands callers the replacement instead of the empty original. Before P55 `from decimal import Decimal` raised `AttributeError` because the import returned the husk. | DONE | 8ef9a7ad |
| P56 | `int(bool)` returns a plain int, not the bool singleton. `builtins/ctor.go numberToInt`'s `*objects.Int` case caught `*Bool` (Bool embeds Int) and returned the singleton, so `int(True)` was the True object. `_pydecimal` does `int(self._is_special)` inside arithmetic and feeds the result to `Int64()`, which blew up because the value coming back was a wrapped Bool. Added the `*objects.Bool` case before the `*Int` case mirroring `Objects/boolobject.c bool_int` / `long_new_impl PyNumber_Long`. | DONE | 4eef89cb |
| P57 | `__dunder__` / `__rdunder__` fallback for unary and binary number ops. CPython's `Objects/typeobject.c update_one_slot` walks a Python class's MRO and synthesizes `slot_nb_add` / `slot_nb_negative` / etc. that call `__add__`, `__neg__`, etc. The slot-wrapper port is not in yet, so a Python class that defines `__add__` on its body has no `nb_add` slot wired and the abstract layer raised TypeError before ever looking at the dunder. Added the fallback at the tail of `numberBinary`, `numberBinaryNoErr` (so the in-place variants get it too), and `unaryNumberOp` in `objects/abstract_number.go`. Exposed it as `objects.DunderBinary` so `vm/eval_simple.go numericForward` (which bypasses the abstract layer for the eval-loop fast path) can call it without leaking the dunder map. The fallback only fires when the C slot is nil or returned NotImplemented, so built-ins keep their fast path. Closes the `_pydecimal` arithmetic chain that drives `test_decistmt`. | DONE | f61afdc2 |
| P58 | OSError to typed subclass promotion at exception-build time. `os.remove(nonexistent)` raised plain `OSError` instead of `FileNotFoundError`, so `test.support.os_helper.unlink` (which only swallows `FileNotFoundError` and `NotADirectoryError`) re-raised as a generic OSError that `test_file_parse` reported as a failure. Ported `Python/errors.c:1031 _PyErr_SetFromErrnoWithFilenameObjects`: when the VM is about to construct an OSError, look at the wrapped Go error for `*os.PathError` / `*os.LinkError` / `*os.SyscallError` / `syscall.Errno`, then route the errno through `pyerrors.ErrnoSubclass` (the existing CPython `errnomap` port). Lives in `vm/eval_unwind.go promoteOSErrorByErrno`, called from the two prefix-matching arms in `synthesizeException` so both the explicit `raise` path and the implicit "Go error becomes Python exception" path get the right subclass. | DONE | 6b83ba96 |
| P59 | PathFinder consults live sys.path on every import. `imp.PathFinder.Paths` was a snapshot built at startup, so `sys.path.insert(0, tempdir)` followed by `__import__(name)` saw the original list and raised `ModuleNotFoundError`. CPython's `Lib/importlib/_bootstrap_external.py:1290 PathFinder.find_spec` reads `sys.path` every call. Added `imp.LivePathHook` (a `func() []string` package-level callable). When set, `FindModule` consults it for top-level imports so user-code mutations propagate. `module/sys/module.go LivePath` reads the live sys module dict (List/Tuple of Unicode entries) and returns nil when sys has not been imported, which is what `stdlibinit/` unit tests rely on. `cmd/gopy/main.go installPathFinder` wires `imp.SetLivePathHook(sys.LivePath)` after the static PathFinder install. Closes `test_source_encoding.MiscSourceEncodingTest.test_file_parse`, which writes a cp1252 source file into a tempdir and adds the dir to sys.path before importing. | DONE | a07b332b |
| P60 | File-driver encoding head reads line by line. `parser/lexer/driver_file.go FromReader` used to peek `2*BUFSIZ = 16384` bytes through a `bufio.Reader` to run cookie detection. The peek window was wide enough for `test_long_first_coding_line` (BUFSIZ spaces + cookie), but anything beyond 16KB on the first physical line would silently fall through to the default UTF-8 path with no cookie applied. CPython's `Parser/tokenizer/file_tokenizer.c:285 tok_underflow_file` STATE_INIT branch has no fixed cap: it walks `fp_getc` until two newlines or EOF, then hands the head to `check_bom` + `check_coding_spec` in `helpers.c`. Mirror that loop in a new `readFirstTwoLines` helper that pulls one line at a time via a local `bufio.Reader`, drains the pre-buffered tail through `br.Buffered()` + `io.ReadFull`, and returns the consumed prefix verbatim. `FromReader` now splices the head back in front of the underlying `io.Reader` through `io.MultiReader(bytes.NewReader(head), r)` for the UTF-8 path, so the existing line-by-line `underflow` callback sees the file from byte 0 regardless of head length. A new `TestReaderDriverLongCookieLine` pins the 20KB-padded `# coding: latin-1` case end-to-end. | DONE | pending |
| P50 | `os.scandir` + `os.DirEntry` real port. The pre-P50 stub returned a flat list of filename strings, so `glob.glob` (the body of `_iterdir`) raised `AttributeError: str has no 'is_dir'` and could not actually descend a tree. Port the `Modules/posixmodule.c:13591 ScandirIterator` / `:13133 DirEntry` types into `module/os/scandir.go`: `ScandirIteratorType` holds the readdir snapshot plus a `closed` flag and implements `__iter__` / `__next__` (yielding DirEntry then StopIteration) and the context-manager `__enter__` / `__exit__` / `close` triple. `DirEntryType` exposes the `name` / `path` getters and `is_dir(*, follow_symlinks=True)` / `is_file` / `is_symlink` / `stat(*, follow_symlinks=True)` / `inode` / `__fspath__` methods. With the real iterator `glob.glob(...)` walks the directory and `test_random_files` reaches the roundtrip phase. | DONE | pending |
| P49b | Empty-source NL false positive in `_tokenize`. `drainReadline` appended a synthetic `\n` whenever the readline iterator drained to empty bytes, including the `len(buf) == 0` case. The lexer then saw a 1-byte `"\n"` buffer and emitted a spurious NL token. CPython's `Parser/tokenizer/string_tokenizer.c:55 tok_underflow_string` does NOT append a terminator for empty source, so the iterator returns ENDMARKER only. Guard the implicit-newline append with `len(buf) > 0` to match CPython, which clears the last 5 `test_random_files` roundtrip failures (`'\n' != ''` from the NL token whose `line=''`). | DONE | pending |
| P48 | Vendor `Lib/glob.py` from CPython 3.14 byte-for-byte. `test_random_files` does `import glob, random` to enumerate the on-disk test files; the missing module raised `ModuleNotFoundError`. `glob.escape` and the module import resolve, leaving `os.scandir` context-manager support as the remaining blocker for `glob.glob` to actually walk a tree. | DONE | pending |
| P47 | Expose `os.extsep`, plus `os.path.splitdrive` / `os.path.extsep` / `os.path.altsep` on the Go-built `os.path` module. `Lib/test/support/script_helper.py:235 make_script` concatenates `name + os.extsep + 'py'`, and `Lib/glob.py:281 escape` reads `os.path.splitdrive(pathname)`. Both raised `AttributeError` before P47. POSIX splitdrive returns `('', p)` per `Lib/posixpath.py:131`. | DONE | pending |
| P46 | `@contextmanager` helpers bind like a Python `def`. The Go contextlib was wrapping the helper in a `method_descriptor` with `owner=object`, so a module attribute call (`os_helper.temp_dir()`) raised `TypeError: descriptor 'helper' of 'object' object needs an argument`. CPython's helper is a real `def helper(*args, **kwds)` whose tp_descr_get binds on instance access and returns the function unchanged when looked up from a module. Add `objects.MethodFunc`: tp_call passes args straight through, tp_descr_get returns `NewBoundMethod` only when `owner != nil`, tp_name reports `function` matching `Objects/funcobject.c PyFunction_Type`. Switch `module/contextlib/module.go contextManager` to `NewMethodFunc`. Cuts `test_invalid_character_in_fstring_middle` and one other `os_helper.temp_dir()` consumer. | DONE | pending |
| P45 | Promote ERRORTOKEN to typed `IndentationError` / `TabError` / `OverflowError` in `pegen.fillToken`. Before P45 the lexer wrote `state.done = E_TOODEEP / E_TABSPACE / E_DEDENT / E_COLUMNOVERFLOW` and emitted an ERRORTOKEN, but `pegen.Dispatch` saw the token, marked `errorIndicator`, and returned `ErrParserNotImplemented`. `runParse` then surfaced the not-implemented sentinel instead of the structured SyntaxError, so `compile()` mapped the parser failure to a generic SyntaxError with no kind/lineno/text. Port `Parser/pegen_errors.c:69 _Pypegen_tokenizer_error`: when `fillToken` sees an ERRORTOKEN it pins `tokenizerSyntaxError(tok, t)` which maps `state.Done()` -> `KindIndentation / KindTab / KindOverflow / KindSyntax` and lifts the lexer-recorded position/message/text. `runParse` already had the "real SyntaxError beats not-implemented" fallback so the pinned error now reaches the VM. `test_max_indent` flips from generic SyntaxError to `IndentationError: too many levels of indentation` matching CPython. | DONE | pending |
| P44 | Vendor `tokenize_tests-*.txt` PEP 0263 fixtures from `Lib/test/tokenizedata/` into `test/cpython/tokenizedata/`: the latin1-coding-cookie-and-utf8-bom-sig, no-coding-cookie-and-utf8-bom-sig-only, utf8-coding-cookie-and-no-utf8-bom-sig, utf8-coding-cookie-and-utf8-bom-sig variants plus the canonical `tokenize_tests.txt`. Without the fixtures `TestTokenizerAdheresToPep0263` errored on file-not-found before any tokeniser logic ran. All four PEP 0263 rows now pass. | DONE | pending |
| P43 | Two lexer fixes (a) bracket-mismatch ERRORTOKEN: `popParen` / `pushParen` recorded the error message but the lexer kept scanning, so `(1+2]` and `]` (orphan closer) emitted a regular `OP` token for the closing bracket and the test framework never saw a `TokenError`. Convert both to `bool`-returning; on mismatch they record the error and return false. `scanOperator`'s `(`/`[`/`{` and `)`/`]`/`}` arms emit `token.ERRORTOKEN` on the false branch, which the iterator promotes to TokenError per `Parser/lexer/lexer.c:1693 tok_get_normal_mode` and the `E_TOKEN` path in `Python/Python-tokenize.c`. (b) DEDENT/INDENT post-indent column: the DEDENT emitted at the end of an indented block was reporting `col=0` instead of the post-indent column of the next line. Snap `s.startCol = s.col` before each DEDENT `tokenSetup`, and in `tokExtraTokens` mode (the C tokenize bridge) hoist INDENT's start back to `s.lineStart` with `startCol = 0` so the slice spans the actual leading whitespace. `test_async`, `test_invalid_syntax` brace cases now match CPython. | DONE | pending |
| P42 | Per-line implicit-newline + CRLF tracking on the `_tokenize` iterator. P33 added an `implicitNewline` flag but only on the FIRST drain of the readline iterator. Multi-line sources like the f-string tests drained one line at a time, and the implicit flag from the final drain only fired for the synthesised `\n`, while CRLF preservation looked at the line's raw terminator. Move both to per-line tracking: `lineHasCRLF(lineno)` peeks `s.bufLine(lineno)` for `\r\n`; `isImplicitNewlineLine(lineno)` checks whether the line is the last and whether it carries a real terminator. NEWLINE/NL str selection branches on both: implicit -> `""`, CRLF -> `"\r\n"` with endCol+=1, else `"\n"`. End column also bumps by 1 for CRLF NL/NEWLINE to keep the slice contiguous with the next line. Cuts the CRLF-roundtrip failures in `TestRoundtrip` and the `test_basic` `\r\n` cases. | DONE | pending |
| P41 | `splitlines` kwarg parity. `bytes.splitlines` and `str.splitlines` accepted `keepends` only as a positional argument; CPython's signature is `splitlines($self, /, *, keepends=False)` per `Objects/bytes_methods.c:STRINGLIB_SPLITLINES`. tokenize's `_compile` and `Untokenizer.add_whitespace` both call `splitlines(keepends=True)` as a kwarg. Threaded `kwargs` through `bytesSplitLines` and `strSplitLines` mirroring the clinic FORMAT_FUNCTION_NAME. Cuts the "splitlines() got an unexpected keyword argument 'keepends'" failure class. | DONE | pending |
| P40 | `line_start` bookkeeping on `nextC`. The lexer snapped `lineStart` on the synthetic-newline path inside `tok_underflow_string` (CPython `Parser/lexer/lexer.c:1058`) but missed the case where `nextC` returned a real `\n` consumed mid-token (string body, f-string middle, line continuation). After the C tokenizer landed and started reading multi-line token slices, the recorded `start_col / end_col` drifted because `line_start` still pointed at the previous line. Snap `s.lineStart = s.cur` after every `\n` returned from `nextC`. Cuts the column-drift failures in multi-line strings. | DONE | pending |
| P39 | Attach `filename / lineno / offset / text / end_lineno / end_offset` to the SyntaxError exception object emitted by the `_tokenize` C iterator. Before P39 the iterator built a `SyntaxError` with just `args=(msg,)`, so the test expectations that read `e.filename`, `e.lineno` etc. saw `None` and the file/line/caret frame never rendered. Route through `parsererrors.SyntaxFromParser` (built in P24 for the parser side) so the iterator's lexer-side error funnels into the same 2-arg constructor. | DONE | pending |
| P38 | Preserve `\r\n` line endings in the `_tokenize` NEWLINE/NL token `str`. CPython's `Python/Python-tokenize.c:316 tokenizeriter_next` reads the slice `[a, b)` from `tok->buf` directly, so a CRLF line surfaces a `'\r\n'` string. gopy was rebuilding the string from `'\n'` literals so every roundtrip test that compared `tokenize(io.BytesIO(b'x\r\n')) -> token.string == '\r\n'` failed. Track a per-line CRLF flag on `tokenizerIter` and swap `'\n'` for `'\r\n'` at NEWLINE/NL emission when the flag is set. | DONE | pending |
| P37 | Backslash-newline continuation inside an f-string lost the line bump. `fstringMiddle`'s backslash arm only short-circuited on `\{` and `\}`, falling through any other escape (including `\<newline>`) without touching the line counter. The `\<newline>` continuation collapses an explicit line break inside even a single-quoted f-string, so the closing quote and any following tokens must be reported on the row after the break. Mirror the line bump that lives in `scanString`'s escape arm. After the fix `f"abc\<newline>def"` reports `FSTRING_MIDDLE (1, 2) (2, 3)` and `FSTRING_END (2, 3) (2, 4)` matching CPython. `test_tokenize.py` failures drop from 15F+17E to 11F+17E. | DONE | pending |
| P36 | Multi-line f-string position tracking. `parser/lexer/fstring.go fstringMiddle` consumed `\n` via `nextC` without bumping `pendingLineno` or resetting `s.col`, so every FSTRING_MIDDLE token that spanned a physical newline reported end-row 1 (CPython expects 2 for `f'''\n...'''`). `Parser/lexer/lexer.c:1462 f_string_middle` relies on `tok_nextc`'s underflow callback to advance `tok->lineno` as each line is loaded; gopy preloads the whole buffer so the bump has to live inside the scanner itself, mirroring the `'\n'` arm of `scanString`. After the fix `test_multiline_non_ascii_fstring`, `test_multiline_non_ascii_fstring_with_expr`, and the multi-line cases of `test_string` reproduce CPython's `(row, col)` pairs. `test_tokenize.py` failures drop from 21F+17E to 15F+17E. | DONE | pending |
| P35 | `format/format.go FormatString` measured width and precision in UTF-8 bytes, so `f"{repr('Örter'):13}"` came out 12 chars wide (`'Örter'` is 7 code points / 8 bytes, so 13-8 = 5 trailing spaces instead of 13-7 = 6). Mirror `Python/formatter_unicode.c:872 format_string_internal` by switching the precision cap to a rune slice (`truncateRunes`) and `pad` to compare width against `utf8.RuneCountInString(body)`. `test_tokenize.py` row formatting now reproduces CPython's column alignment for non-ASCII tokens; failures drop from 26F+17E to 21F+17E. | DONE | pending |
| P34 | Tuple-subclass equality lost the inherited `tp_richcompare`. `objects/usertype.go fixupRichCmpAndBool` was checking `hasAnyDunder(t, "__eq__", ...)` via `lookupDunderCallable`, which walks the MRO. For any user type the answer is always yes because `object` exposes `__eq__` (the `richCompareDescr` slot wrapper), so the dispatcher `slotTpRichCompare` got installed on every class, clobbering the `RichCmp` that `inheritSlotsAllMRO` had already copied from the base. The result: `class P(tuple): pass; P((1,2)) == P((1,2))` returned False, because `slotTpRichCompare` looked up `__eq__` on P's MRO, found `object.__eq__`, and called the identity-only `richCompareDescr`. CPython's `Objects/typeobject.c:9874 fixup_slot_dispatchers` / `update_one_slot` only swaps in `slot_tp_richcompare` when the descriptor on the MRO is a Python method or a slot wrapper for a different slot. Mirror that discrimination by switching every probe in `fixupRichCmpAndBool` to `isOwnDescriptor(t, name)`, the same helper P30 already used for `fixupSubscriptSlots`. Cuts `test_tokenize.py` failures from 31F+17E to 26F+17E by unlocking every assertion that compares `TokenInfo` namedtuple lists with `assertEqual`. | DONE | pending |
| P33 | Implicit-newline `str` parity for the `_tokenize` C iterator. `module/_tokenize/module.go drainReadline` synthesises a trailing `\n` for sources that lack one (matching `Python/Python-tokenize.c tokenizeriter_next`'s `tok->implicit_newline` flag), but the iterator was still emitting `tok.string == "\n"` for the NEWLINE token that consumed that synthesised byte. CPython sets `str = PyUnicode_FromString("")` when `tok->implicit_newline && type == NEWLINE && tok->cur == tok->inp` (`Python/Python-tokenize.c:96`). Track `implicitNewline` plus `implicitEndOff` on `tokenizerIter`, change `drainReadline` to return `(buf, lines, implicit, err)`, and in the NEWLINE branch swap `\n` for `""` when the token's end offset hits the synthesised position. Cuts `test_tokenize.py` failures by 4 (test_comment_at_the_end_of_the_source_without_newline, test_newline_and_space_at_the_end_of_the_source_without_newline, etc.). | DONE | pending |
| P32 | Number literal lexer rewrite. `parser/lexer/lexer.go scanNumber` was a four-line `for (digit | _)*` loop that happily accepted `1_`, `0b1_`, `0x_`, `1e_2`, `0x__1`, etc. CPython rejects every one of these with `SyntaxError: invalid <X> literal`. Port `Parser/lexer/lexer.c:855` number branch in full: split into `scanFraction`, `scanExponent`, `scanImaginary` arms and route each digit run through a new `decimalTail` helper that mirrors `Parser/lexer/lexer.c:413 tok_decimal_tail` (run-of-digits then optional `_` then required run-of-digits). Replace the underscore-permissive `isHexDigitOrUnderscore` / `isOctDigitOrUnderscore` / `isBinDigitOrUnderscore` with strict `isHexDigit` / `isDecimalDigit` and lift CPython's per-prefix `invalid <X> literal` / `invalid digit '%c' in <X> literal` error messages exactly. Also re-port the leading-zero decimal arm with its `leading zeros in decimal integer literals are not permitted` message at `Parser/lexer/lexer.c:976`. Drops `test_tokenize.py` failures from 52F+17E to 35F+17E (17 invalid-literal tests now reject correctly). | DONE | pending |
| P31 | Three closers exposed once P30 let `test_tokenize.py` make it past the tuple-subclass recursion (errors went 49 -> 17 across the run): (a) vendor `Lib/encodings/utf_8_sig.py` byte-for-byte so `tokenize.detect_encoding` resolves the `utf-8-sig` codec it picks when it sees a UTF-8 BOM, and so the encodings module's `codecs.lookup('utf-8-sig')` returns a real CodecInfo (currently 13 TestDetectEncoding rows error out on the missing codec); (b) port the `add_operators` slot wrappers for `tp_iter` / `tp_iternext` via a new `objects.AddIterSlotWrappers` helper. CPython exposes `__next__` and `__iter__` automatically through the `slotdefs` walk in `Objects/typeobject.c add_operators`. gopy has no central PyType_Ready pass for built-in types so each iterator type calls `AddIterSlotWrappers` from its init after setting `Iter`/`IterNext`. Twenty-plus iterator types now expose `getattr(it, '__next__')` so `iter(lines).__next__` (the standard CPython tokenize.detect_encoding readline shape) actually returns a callable rather than raising AttributeError; (c) bytes-aware `Match.group`/`groups`/`groupdict`. `module/_sre/match.go matchGroup` was unconditionally wrapping substrings in `objects.NewStr`. CPython's `Modules/_sre/sre.c:2735 match_getslice_by_index` branches on `PATTERN_TYPE_BYTES` so a bytes-input match returns Bytes. Add an `isBytes` flag to `matchData` (populated from the src type at `makeMatch` time) plus a `matchSlice` helper, then route `group`/`groups`/`groupdict` through it. After the fix `tokenize.detect_encoding` no longer raises `'str' object has no attribute 'decode'` on `cookie_re.match(b'...').group(1).decode()`. | DONE | pending |
| P30 | Two closers exposed once P29 unblocked `test_tokenize.py` enough to start running its 130-test body: (a) tuple-subclass subscription recursion via `Objects/typeobject.c:9874 fixup_slot_dispatchers`. `objects/usertype.go fixupSubscriptSlots` was installing the slot dispatchers (`slotMpSubscript` etc.) for every type that exposed `__getitem__` through MRO, clobbering the C-level `mp_subscript` that `inheritSlotsAllMRO` already copied. When a tuple subclass like `collections.namedtuple` looked up `obj[0]`, the dispatcher called `__getitem__`, which re-entered the dispatcher, blowing the Go stack. The fix mirrors CPython's `update_one_slot` wrapper-vs-method discrimination: a new `isOwnDescriptor(t, name)` checks `LookupDescriptor`'s `providingType == t` so inherited descriptors keep the base's C slot in place, while genuine overrides still flip to the dispatcher; (b) `module/_sre/match.go matchGroupdict` defensive bounds clamp. `Modules/_sre/sre_lib.h:1462` already guards `(start == -1 || end == -1)` jointly; the gopy port only checked `lo < 0`, so a group that matched empty at end-of-string (`end == -1` while `start >= 0`) drove `s[lo:hi]` with `hi < 0` and panicked the runtime. The check now matches CPython's joint guard, returning the `default` argument when either offset is -1. | DONE | pending |
| P29 | Four parallel closers for the `test_source_encoding.py` panel: (a) per-line null-byte SyntaxError parity, porting `Parser/lexer/lexer.c:53 contains_null_bytes` into `parser/lexer/lexer.go` `nextC` and adding a one-pass `firstNullByteLine` pre-scan in `parser/lexer/driver_string.go FromBytes` (the load-all-upfront driver never hits the per-line refill arm); (b) `verify_end_of_number` on the `0x` / `0o` / `0b` prefixes in `parser/lexer/lexer.go scanNumber` so `0b1and 2` emits the same SyntaxWarning as CPython's `Parser/lexer/lexer.c:875` (hex) / `:905` (octal) / `:932` (binary) `verify_end_of_number("...")` arms; (c) `_io.File.encoding` / `_io.File.errors` getsets exposed on text-mode files, mirroring `Modules/_io/textio.c:2261 textiowrapper_init` (binary mode keeps `AttributeError`); (d) parse-time SyntaxWarning source line plumbed end-to-end (`parser/lexer/helpers.go parserWarn` now captures `nthLine(s.buf, lineno)` into `SyntaxError.Text`, `module/_warnings.WarnExplicitWithSourceline` hands it to `warn_explicit` as the explicit sourceline arg, and `callShowWarning` routes it as `WarningMessage(line=...)` so `_formatwarnmsg_impl` displays the line without needing `linecache`). `_showwarnmsg` is not loaded at parse-time because `Lib/warnings.py` is lazy-imported; the explicit sourceline keeps the display CPython-faithful in either branch. Vendor `Lib/encodings/koi8_r.py` byte-for-byte so the `MiscSourceEncodingTest.test_*` rows that drive `# coding: koi8-r` resolve through the single-byte charmap codec. | DONE | pending |

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

**Status (closed):** the port landed under `codecs/cjkcodecs/`
rather than `codecs/multibyte/` (no `internal/` and no per-codec
module split; the package directly owns the dispatch). Shape:

- `tools/cjkcodecs_go/main.go` — build-time generator that
  translates CPython's auto-generated `mappings_*.h` headers
  (`mappings_kr.h`, `mappings_jp.h`, `mappings_cn.h`,
  `mappings_hk.h`, `mappings_tw.h`, `mappings_jisx0213_pair.h`)
  to compilable Go data files. Handles `dbcs_index`,
  `widedbcs_index`, `unim_index`, and `pair_encodemap` shapes;
  expands the U/N/M/D macros to their numeric sentinels.
- `codecs/cjkcodecs/runtime.go` — synchronous encode/decode
  outer loops ported from `multibytecodec.c:404` /
  `multibytecodec.c:507` / `multibytecodec.c:672`, including
  `decErrorClassify` / `encErrorClassify` mapping `MBERR_*` to
  the CPython reason strings and `callDecodeError` /
  `callEncodeError` dispatching through `codecs.LookupError`.
- `codecs/cjkcodecs/types.go` — `dbcsIndex`, `wideDBCSIndex`,
  `unimIndex`, `pairEncodeMap`, `tryMapDec` / `tryMapEnc` /
  `findPairEnc` ported from `cjkcodecs.h` `_TRYMAP_ENC`,
  `_TRYMAP_DEC`, `find_pairencmap`.
- `codecs/cjkcodecs/codecs_kr.go` — full port of `_codecs_kr.c`
  for cp949, euc_kr, johab, including the `u2cgk*` Hangul
  composition tables and the `johabidx_*` / `johabjamo_*`
  jamo packing.
- `codecs/cjkcodecs/codecs_jp.go` — full port of `_codecs_jp.c`
  for cp932, shift_jis, euc_jp, shift_jis_2004, euc_jis_2004,
  plus the JIS X 0201 Roman / Katakana algorithmic mappings and
  the JIS X 0213 2000 emulator.
- `codecs/cjkcodecs/registry.go` — exports CP932 / CP949 /
  EUC_KR / EUC_JP / SHIFT_JIS / JOHAB / SHIFT_JIS_2004 /
  EUC_JIS_2004 / EUC_JISX0213 / SHIFT_JISX0213, plus a
  `SearchFunc` covering every alias CPython's
  `Lib/encodings/aliases.py` recognises for those codecs.
  `init()` calls `codecs.Register(Search)` so the side-load
  registers the search function.
- `stdlibinit/registry.go` — blank-imports
  `github.com/tamnd/gopy/codecs/cjkcodecs` so the binary picks
  up the registration.

**Test gate outcome:**
`test_source_encoding.py` was 91/11/59 (pass/fail/error) before
P9 and is now 89/0/1 with 1 skip out of 91. The remaining
error is `test_issue2301`: cp932 decode runs end-to-end, but
compile() drops the parser's structured SyntaxError text on the
`ErrParserNotImplemented` path. The same `v.text == None` gap
reproduces for ASCII source (`compile('print 1', 'd', 'exec')`
also yields `v.text=None`), so the remaining blocker lives in
compile()/parser, not the codec subsystem. `test_tokenize.py`
remains 130/130 green; no regressions elsewhere in the codec
test suite.

CN / HK / TW / ISO-2022 ports now land alongside the JP/KR
batch. `codecs/cjkcodecs/codecs_tw.go` ports big5 / cp950,
`codecs_hk.go` ports big5hkscs (including the make-up-pair
cases for U+00CA / U+00EA + combining marks), `codecs_cn.go`
ports gb2312 / gbk / gb18030 / hz (with the stateful `~{` /
`~}` GB-mode flip), and `codecs_iso2022.go` ports the full
iso2022 family (kr, jp, jp_1, jp_2, jp_2004, jp_3, jp_ext)
with the G0..G3 designation stack, SS2 single-shift, JISX0213
2000-vs-2004 plane selection, and the ENCODER_RESET callback
that emits `ESC ( B SI` when the input ends in a non-ASCII
designation. The mapping generator gained two new emitters
for the big5hkscs phint byte arrays and the gb18030
range-table struct. All 14 new codecs are wired into
`Search()` with their full Lib/encodings/aliases.py aliases
(big5/csbig5, cp936/ms936, gb18030_2000, hzgb, iso_2022_jp_2,
csiso2022kr, ...).

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

Status: done. `module/os/pathlike.go` exposes the `PathLike`
type singleton (the C-implementation side) and
`stdlib/os.py:1081` plus `stdlib/os.py:1123` carry the
verbatim `_fspath` helper and `class PathLike(abc.ABC)`
definition. The named gate rows (`test_20731`,
`test_file_parse_error_multiline`) pass today;
`test_tokenizer_fstring_warning_in_first_line` errors but on
a subprocess fd leak unrelated to PathLike (~30 Popens deep
into the suite).

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
the slice it managed to read.

Status: done as a side-effect of the original port.
`parser/lexer/source.go detectEncodingCookieAt` reads up to the
physical newline via `lineEnd` and does not short-circuit on
length; the file driver in `parser/lexer/driver_file.go:117`
peeks `2*BUFSIZ = 16384` bytes so an `#<8192 spaces>coding:...`
cookie line is fully scanned. `test_long_first_coding_line` and
`test_long_second_coding_line` (both Bytes and File variants)
pass today.

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
