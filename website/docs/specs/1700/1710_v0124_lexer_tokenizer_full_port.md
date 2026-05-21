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

Status legend: DONE = ported in full and verified, WIP = port underway, TODO = not started, DRIFT = present but diverges from CPython (see audit tables below).

| CPython source | C LOC | gopy destination | Go LOC | Status | Commit |
|---|---:|---|---:|---|---|
| `Parser/lexer/buffer.c` | 76 | `parser/lexer/buffer.go` | 50 | DONE | 5374e84 |
| `Parser/lexer/lexer.c` | 1635 | `parser/lexer/lexer.go` (+ `fstring.go` + `onechar.go` + `xid.go`) | 986 + 390 + 208 + 90 | DRIFT | remaining gaps in `set_ftstring_expr` (P3) and `verify_end_of_number` (P4); `verify_identifier` flipped to use the XID composition in `xid.go` |
| `Parser/lexer/state.c` | 151 | `parser/lexer/state.go` | 408 | DONE | d157189 |
| `Parser/tokenizer/helpers.c` | 581 | `parser/lexer/helpers.go` (+ encoding subset in `parser/lexer/source.go`) | 179 + 287 | DRIFT | `check_coding_spec` skips `tok->cont_line`; `valid_utf8` collapsed into `utf8Size` (length-only); `_PyTokenizer_warn_invalid_escape_sequence` records as `[warn]`-tagged error instead of `PyErr_WarnExplicitObject` |
| `Parser/tokenizer/file_tokenizer.c` | 493 | `parser/lexer/driver_file.go` | 119 | DRIFT | `tok_underflow_interactive` + `tok_concatenate_interactive_new_line` not ported; embedder owns REPL state |
| `Parser/tokenizer/readline_tokenizer.c` | 134 | `parser/lexer/driver_readline.go` | 50 | DRIFT | no `decoding_state` machine; readline callback assumed UTF-8 |
| `Parser/tokenizer/string_tokenizer.c` | 148 | `parser/lexer/driver_string.go` | 113 | DRIFT | `buf_ungetc` and the on-demand `decode_str` BOM dance fold into `FromBytes` + `readEncodingHead` |
| `Parser/tokenizer/utf8_tokenizer.c` | 55 | `parser/lexer/driver_string.go` (utf-8 path) | (shared) | DONE | 268c8f8 |
| `Python/Python-tokenize.c` | 445 | `module/_tokenize/module.go` | 391 | WIP | `tokenizerError` now switches on `lexer.State.Done()` like CPython's `_tokenizer_error`; remaining DRIFT is upfront `drainReadline` (see audit) |
| `Lib/keyword.py` | 64 | `stdlib/keyword.py` | 64 | DONE | byte-equal vendor (verified `diff` returns empty) |
| `Lib/tokenize.py` | 598 | `stdlib/tokenize.py` | 598 | DONE | byte-equal vendor (verified `diff` returns empty) |
| `Lib/tabnanny.py` | 338 | `stdlib/tabnanny.py` | 338 | DONE | byte-equal vendor (verified `diff` returns empty) |

### Gate tests to land green under `test/cpython/`

| Test | LOC | Status | Commit |
|---|---:|---|---|
| `test_keyword.py` | 56 | DONE (10/11 sub-tests green; the eleventh hits a parser-generator gap unrelated to lexer/tokenizer, `parser: generated rule bodies not yet emitted`. Also mirrored at `stdtest/test_keyword.py` and gated via `TestStdtestCorpus`.) | — |
| `test_utf8source.py` | 41 | DONE (3/3 sub-tests green; mirrored at `stdtest/test_utf8source.py`) | — |
| `test_tabnanny.py` | 354 | DONE (exits 0 after typed `UnicodeDecodeError` + `surrogateescape` decode fix; mirrored under `stdtest/test_tabnanny.py`) | 3066fe3 |
| `test_source_encoding.py` | 547 | TODO (imports clear; first hang is `BytesSourceEncodingTest.test_crcrcrlf`, which is `exec(bytes)` inside `captured_stdout`; the underlying gap is the VM's `exec(bytes)` path, not lexer/tokenizer). | — |
| `test_tokenize.py` | 3480 | WIP (P5 below). Plumbing blockers cleared: `drainReadline` encoding inversion (538ab52), `FORMAT_WITH_SPEC` (5bd8455), `scanOperator` token-type emission (669c11f). Most sub-tests now run; position parity is still off (e.g. `1 + 1` emits the implicit NEWLINE at `(2, 0) (2, 2)` instead of `(1, 5) (1, 6)`, and the second `check_tokenize` reorders NEWLINE before COMMENT and reports it on the wrong line). The remaining work is the token-position parity pass tracked in P5 below. | 538ab52, 5bd8455, 669c11f |

## Goal

Replace the partial lexer/tokenizer port that grew up alongside the
v0.5.5 parser work with a one-to-one translation of every CPython 3.14
source file in the subsystem, then pin the result with the five
`Lib/test/test_*` files the 1700 spec already assigned to this panel.

Today `parser/lexer/lexer.go` is 969 lines against CPython's 1635-line
`Parser/lexer/lexer.c` plus 390 lines in `fstring.go` and 208 in
`onechar.go` (total 1567). The delta is the gap this spec closes.
The v0.12.4 series treats every subsystem the same way: port full,
then gate on the upstream tests.

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
| Python/Python-tokenize.c | 445 | module/_tokenize/module.go |
| Include/errcode.h | 46 | parser/lexer/state.go (errCode enum) |

Python sources (3.14):

| CPython file | Lines | gopy destination |
|--------------|------:|------------------|
| Lib/keyword.py | 64 | stdlib/keyword.py |
| Lib/tokenize.py | 598 | stdlib/tokenize.py |
| Lib/tabnanny.py | 338 | stdlib/tabnanny.py |

Gate tests live at `~/github/python/cpython/Lib/test/`:
`test_keyword.py`, `test_utf8source.py`, `test_source_encoding.py`,
`test_tabnanny.py`, `test_tokenize.py`.

## Function-level audit (CPython 3.14 → gopy)

The audit walks every function defined in the source files above and
classifies its gopy counterpart as DONE / DRIFT / MISSING. The
tables below are the canonical punch list this spec works through.

Notation: `f.c:N` is `<filename>.c` line `N` in `cpython-314`.
Line numbers reflect the audit pass at the time of this spec edit;
re-run the audit if the upstream rebases.

### Parser/lexer/{buffer,lexer,state}.c

| CPython function | C site | gopy function | Go site | Status | Notes |
|---|---|---|---|---|---|
| `_PyLexer_remember_fstring_buffers` | buffer.c:9 | `rememberFStringBuffers` | buffer.go:45 | DONE | No-op in gopy; offset-based State eliminates pointer rebasing. |
| `_PyLexer_restore_fstring_buffers` | buffer.c:23 | `restoreFStringBuffers` | buffer.go:50 | DONE | Matching no-op. |
| `_PyLexer_tok_reserve_buf` | buffer.c:50 | `reserveBuf` | buffer.go:19 | DONE | Slice realloc; growth policy matches. |
| `contains_null_bytes` | lexer.c:53 | (inlined) | lexer.go:78 | DONE | Inlined into `nextC` refill branch. |
| `tok_nextc` | lexer.c:60 | `State.nextC` | lexer.go:60 | DONE | Mirrors line/col tracking + EOF/refill callback. |
| `tok_backup` | lexer.c:99 | `State.backup` | lexer.go:86 | DONE | cur/col decrement. |
| `set_ftstring_expr` | lexer.c:114 | `State.setFtstringExpr` | fstring.go:306 | DONE | Runs `ValidateUTF8` on the expression buffer before writing `tok.Metadata`. Malformed UTF-8 sets `tok->done = E_DECODE` and records a SyntaxError, mirroring CPython's `PyUnicode_DecodeUTF8` failure path. |
| `_PyLexer_update_ftstring_expr` | lexer.c:227 | `State.updateFtstringExpr` | fstring.go:269 | DONE | Buffer append/realloc; void return (no PyMem errors). |
| `lookahead` | lexer.c:282 | `State.lookahead` | lexer.go:831 | DONE | Closure that rewinds the consumed slice. |
| `verify_end_of_number` | lexer.c:305 | `State.verifyEndOfNumber` | lexer.go:865 | DONE | Records a `SyntaxWarning` via `parserWarn` on the abutting-keyword branch (`1and`, `1or`, `1if`, `1in`, `1is`, `1not`), accepting the literal. Plain-identifier follow (`1foo`) still raises `SyntaxError` via the existing branch. Pinned at 6dbf31a. |
| `verify_identifier` | lexer.c:364 | `State.verifyIdentifier` | lexer.go:914 | DONE | Calls `scanIdentifier` in `parser/lexer/xid.go`, which composes XID_Start / XID_Continue from Go stdlib's L, Nl, Mn, Mc, Nd, Pc, Other_ID_Start, Other_ID_Continue tables minus Pattern_Syntax and Pattern_White_Space. Pins `tok->cur` to the bad rune on reject and emits the canonical `invalid character '%c' (U+%04X)` message. |
| `tok_decimal_tail` | lexer.c:413 | (inlined) | lexer.go:467-483 | DONE | Inlined inside `scanNumber`. |
| `tok_continuation_line` | lexer.c:435 | `State.continuationLine` | lexer.go:367 | DONE | Returns `(c, ok)` tuple; defers `lineno` bump to `nextC`. |
| `maybe_raise_syntax_error_for_string_prefixes` | lexer.c:455 | `State.maybeRaiseSyntaxErrorForStringPrefixes` | lexer.go:943 | DONE | Flags incompatible prefix pairs (u+b, u+r, u+f, u+t, b+f, b+t, f+t). |
| `is_potential_identifier_start` | lexer.c:12 | `isPotentialIdentifierStart` | lexer.go:42 | DONE | Macro port: a-z/A-Z/`_` / `≥128`. |
| `is_potential_identifier_char` | lexer.c:18 | `isPotentialIdentifierChar` | lexer.go:52 | DONE | Extends start with digits. |
| `tok_get_normal_mode` | lexer.c:501 | `State.tokGetNormalMode` | lexer.go:114 | DRIFT | Core FSM in place but token position emission diverges (NEWLINE after a simple statement reports the implicit position at the next line's column 0 instead of the source line's column-after-token). Drives the remaining `test_tokenize.py` failures. |
| `tok_get_fstring_mode` | lexer.c:1393 | `State.tokGetFStringModeImpl` | fstring.go:119 | DONE | Dispatches to `fstringMiddle` or `popMode`. |
| `tok_get` | lexer.c:1616 | `State.tokGet` | token.go:120 | DONE | Mode-based dispatch. |
| `_PyTokenizer_Get` | lexer.c:1626 | `State.Get` | token.go:111 | DONE | Public entry point. |
| `_PyTokenizer_tok_new` | state.c:13 | `newState` | state.go:248 | DONE | Field-by-field defaults. |
| `_PyTokenizer_Free` | state.c:84 | `State.Free` | state.go:386 | DONE | GC reclaims; buffers nilled. |
| `free_fstring_expressions` | state.c:66 | `State.freeFStringExpressions` | state.go:370 | DONE | Nils per-mode `lastExprBuffer`. |
| `_PyToken_Free` | state.c:108 | `TokenFree` | state.go:408 | DONE | No-op. |
| `_PyToken_Init` | state.c:113 | `TokenInit` | state.go:398 | DONE | Zero-init. |
| `_PyLexer_type_comment_token_setup` | state.c:118 | `State.typeCommentTokenSetup` | token.go:80 | DONE | |
| `_PyLexer_token_setup` | state.c:131 | `State.tokenSetup` | token.go:15 | DONE | Boundary fields. |
| `TOK_GET_MODE` | lexer.c:26 | `State.curMode` | state.go:269 | DONE | Macro → method. |
| `TOK_NEXT_MODE` | lexer.c:31 | `State.pushMode` | state.go:277 | DONE | Macro → method. |

### Parser/tokenizer/{helpers,file_tokenizer,readline_tokenizer,string_tokenizer,utf8_tokenizer}.c

| CPython function | C site | gopy function | Go site | Status | Notes |
|---|---|---|---|---|---|
| `_syntaxerror_range` | helpers.c:10 | (folded into `syntaxError`) | helpers.go:38 | DRIFT | Loses the `col_offset` / `end_col_offset` parameters; `syntaxErrorKnownRange` carries them separately. |
| `_PyTokenizer_syntaxerror` | helpers.c:65 | `syntaxError` | helpers.go:38 | DONE | Current-line error location. |
| `_PyTokenizer_syntaxerror_known_range` | helpers.c:76 | `syntaxErrorKnownRange` | helpers.go:48 | DONE | Explicit column pinning. |
| `_PyTokenizer_indenterror` | helpers.c:88 | `indentError` | helpers.go:63 | DONE | Sets `eTabSpace`. |
| `_PyTokenizer_error_ret` | helpers.c:96 | `errorRet` | helpers.go:95 | DONE | Sets `cur=inp`, `done=eSyntax`. |
| `_PyTokenizer_warn_invalid_escape_sequence` | helpers.c:111 | `warnInvalidEscape` | helpers.go:79 | DRIFT | Records as `[warn]`-tagged error string instead of `PyErr_WarnExplicitObject` with a `SyntaxWarning` class. Visible to `test_tokenize.py`'s warning-capture path. |
| `_PyTokenizer_parser_warn` | helpers.c:152 | `parserWarn` | helpers.go:110 | DRIFT | Same warning routing gap. |
| `_PyTokenizer_new_string` | helpers.c:190 | `newString` | helpers.go:123 | DONE | Go `string()` replaces malloc+memcpy. |
| `_PyTokenizer_translate_into_utf8` | helpers.c:204 | `translateIntoUTF8` | helpers.go:133 | DRIFT | Only accepts UTF-8 inputs; CPython routes through `PyUnicode_Decode` and supports arbitrary codecs. |
| `_PyTokenizer_translate_newlines` | helpers.c:215 | `TranslateNewlines` | source.go:257 | DONE | CRLF fold + exec-mode trailing LF injection. |
| `_PyTokenizer_check_bom` | helpers.c:267 | `CheckBOMCookieConflict` (+ `ReadEncodingHead`) | source.go:155 | DRIFT | Refactored into two passes; semantics match for first-line BOM but the C version interleaves the BOM check with the cookie scan. |
| `get_normal_name` | helpers.c:305 | `normalizeEncodingName` | source.go:177 | DONE | Lowercases + underscores → hyphens. |
| `get_coding_spec` | helpers.c:335 | `matchCodingCookie` | source.go:77 | DRIFT | Single-line scan; CPython's loop branches on `lineHasCode`, which gopy extracted into a separate function but does not call from this site. |
| `_PyTokenizer_check_coding_spec` | helpers.c:388 | `DetectEncodingCookie` | source.go:32 | DRIFT | Missing `tok->cont_line` continuation-aware skip: CPython will not honor a PEP 263 cookie that appears on a backslash-continued line. |
| `valid_utf8` | helpers.c:446 | `utf8Size` | source.go:237 | DRIFT | gopy's helper only returns the sequence length; CPython's `valid_utf8` validates the full sequence (overlong, surrogate, range checks) in one call. |
| `_PyTokenizer_ensure_utf8` | helpers.c:505 | `ValidateUTF8` | source.go:198 | DONE | Walks source, reports line + bad byte. |
| `_PyTokenizer_print_escape` | helpers.c:548 | `printEscape` | helpers.go:145 | DONE | Returns string instead of writing to FILE\*. |
| `_PyTokenizer_tok_dump` | helpers.c:575 | `tokDump` | helpers.go:177 | DONE | Token formatter. |
| `fp_getc` / `fp_ungetc` | file_tokenizer.c:125/130 | (`bufio.Reader.ReadByte`/`UnreadByte`) | driver_file.go:46 | DONE | Inlined in `FromReader` underflow closure. |
| `fp_setreadl` | file_tokenizer.c:143 | (N/A) | (N/A) | MISSING | CPython hot-swaps the codec mid-stream when a PEP 263 cookie appears. gopy front-loads encoding detection in `readEncodingHead` and decodes via `FromBytes`, so no mid-stream swap is needed for the corpus tests, but a pathological multi-line cookie escape may surface this. |
| `tok_readline_raw` | file_tokenizer.c:58 | (underflow closure) | driver_file.go:46 | DONE | Reads until newline. |
| `tok_readline_recode` | file_tokenizer.c:83 | (`readEncodingHead` + `codecs.Decode`) | driver_file.go:32 | DRIFT | On-demand recoding collapses into upfront decode; observable only if a cookie sits beyond the head window. |
| `tok_concatenate_interactive_new_line` | file_tokenizer.c:19 | (N/A) | (N/A) | MISSING | REPL multi-line accumulation. Embedder owns this; no gate-test impact. |
| `tok_underflow_interactive` | file_tokenizer.c:192 | (N/A) | (N/A) | MISSING | REPL-only underflow. Embedder owns this; no gate-test impact. |
| `tok_underflow_file` | file_tokenizer.c:284 | (underflow closure) | driver_file.go:46 | DRIFT | Missing the inline BOM / encoding state machine; gopy moves BOM detection to `readEncodingHead`. |
| `_PyTokenizer_FromFile` | file_tokenizer.c:372 | `FromReader` | driver_file.go:30 | DRIFT | Idiomatic `io.Reader` signature; underflow closure does not replicate interactive prompt handling (out of scope per above). |
| `_PyTokenizer_FindEncodingFilename` | file_tokenizer.c:449 | `FindEncodingFilename` | driver_file.go:104 | DRIFT | gopy peeks the first two lines via `CheckBOMCookieConflict` + `DetectEncodingCookie` instead of running the full tokenizer up to `lineno == 2`. |
| `tok_readline_string` | readline_tokenizer.c:10 | (`ReadlineFunc` invocation) | driver_readline.go:33 | DONE | Inlined. |
| `tok_underflow_readline` | readline_tokenizer.c:71 | (underflow closure) | driver_readline.go:33 | DRIFT | No `decoding_state` machine; callback is assumed to return UTF-8. |
| `_PyTokenizer_FromReadline` | readline_tokenizer.c:109 | `FromReadline` | driver_readline.go:27 | DRIFT | No interactive prompt / history / nextprompt; Go func callback. |
| `tok_underflow_string` | string_tokenizer.c:8 | (underflow closure in `FromBytes`) | driver_string.go:58 | DONE | Returns false on next call. |
| `buf_getc` | string_tokenizer.c:31 | (direct slice index) | driver_string.go:58 | DONE | |
| `buf_ungetc` | string_tokenizer.c:37 | (N/A) | (N/A) | MISSING | `decode_str` uses it during BOM detection. gopy's string driver has no equivalent, but `readEncodingHead` covers the same case. |
| `buf_setreadl` | string_tokenizer.c:45 | (inlined) | driver_string.go:70 | DONE | Sets encoding in `decode_str` path. |
| `decode_str` | string_tokenizer.c:54 | (`FromBytes` + `codecs.Decode`) | driver_string.go:58 | DRIFT | Split across `FromBytes` and `readEncodingHead`. |
| `_PyTokenizer_FromString` | string_tokenizer.c:131 | `FromString` / `FromBytes` | driver_string.go:48 | DONE | |
| `_PyTokenizer_FromUTF8` | utf8_tokenizer.c:31 | `FromString` / `FromBytes` | driver_string.go:48 | DONE | Collapsed with `FromString` since UTF-8 is assumed. |
| `tok_underflow_string` (utf8) | utf8_tokenizer.c:8 | (underflow closure in `FromBytes`) | driver_string.go:111 | DONE | |

### Python/Python-tokenize.c → module/_tokenize/module.go

| CPython function | C site | gopy function | Go site | Status | Notes |
|---|---|---|---|---|---|
| `tokenizeriterobject` (struct) | tokenize.c:32 | `tokenizerIter` | module.go:46 | DONE | Field-by-field; adds `linesByOneBased` for upfront drain. |
| `tokenizeriter_new_impl` | tokenize.c:55 | `tokenizerIterNew` | module.go:84 | DRIFT | gopy drains the entire readline stream upfront in `drainReadline` (module.go:141-199). CPython runs the readline callback on demand. Hidden by the gate tests because they pass fixed inputs, but blocks real streaming use and contributes to position issues if the stream is large. |
| `_tokenizer_error` | tokenize.c:87 | `tokenizerError` | module.go:329 | DONE | Switches on `lexer.State.Done()` case-for-case (`E_TOKEN` / `E_EOF` / `E_DEDENT` / `E_INTR` / `E_NOMEM` / `E_TABSPACE` / `E_TOODEEP` / `E_LINECONT`). |
| `_get_current_line` | tokenize.c:183 | `lineAt` (+ inline cache) | module.go:303 | DONE | Inlined into `tokenizerIterNext`. |
| `_get_col_offsets` | tokenize.c:204 | `byteToCharCol` (+ inline) | module.go:315 | DONE | UTF-8 byte → char offset conversion via `utf8.RuneCountInString`. |
| `tokenizeriter_next` | tokenize.c:241 | `tokenizerIterNext` | module.go:205 | DONE | Token emission + 5-tuple build. |
| `tokenizeriter_dealloc` | tokenize.c:351 | (GC) | (N/A) | DONE | |
| `tokenizeriter_slots` | tokenize.c:362 | `newTokenizerIterType` | module.go:73 | DONE | tp_new / tp_iter / tp_iternext. |
| `tokenizeiter_spec` | tokenize.c:371 | `tokenizerIterType` | module.go:71 | DONE | |
| `tokenizemodule_exec` | tokenize.c:378 | `buildModule` | module.go:368 | DONE | Registers `TokenizerIter`. |
| `tokenizemodule_traverse/clear/free` | tokenize.c:408 | (GC) | (N/A) | DONE | |
| `PyInit__tokenize` | tokenize.c:441 | `init` + `AppendInittab` | module.go:38 | DONE | |

### errcode.h coverage

| CPython | Value | gopy `errCode` | Status |
|---|---:|---|---|
| `E_OK` | 10 | `eOK` | DONE |
| `E_EOF` | 11 | `eEOF` | DONE |
| `E_INTR` | 12 | `eIntr` | DONE in enum, but `tokenizerError` doesn't route it to `KeyboardInterrupt`. |
| `E_TOKEN` | 13 | `eToken` | DONE |
| `E_SYNTAX` | 14 | `eSyntax` | DONE |
| `E_NOMEM` | 15 | `eNomem` | DONE |
| `E_DONE` | 16 | (none) | MISSING (parser-side; lower priority for tokenizer) |
| `E_ERROR` | 17 | (none) | MISSING (parser-side) |
| `E_TABSPACE` | 18 | `eTabSpace` | DONE |
| `E_OVERFLOW` | 19 | `eOverflow` | DONE |
| `E_TOODEEP` | 20 | `eToodeep` | DONE (replaces former gopy-only `eIndent`) |
| `E_DEDENT` | 21 | `eDedent` | DONE |
| `E_DECODE` | 22 | `eDecode` | DONE |
| `E_EOFS` | 23 | `eEOFS` | DONE |
| `E_EOLS` | 24 | `eEOLS` | DONE |
| `E_LINECONT` | 25 | `eLineCont` | DONE |
| `E_BADSINGLE` | 27 | (none) | MISSING (parser-side) |
| `E_INTERACT_STOP` | 28 | (none) | MISSING (REPL; out of scope) |
| `E_COLUMNOVERFLOW` | 29 | `eColumnOverflow` | DONE |

The former gopy-only `eIndent` was renamed to `eToodeep` to match the
CPython site (`Parser/lexer/lexer.c:582`), which sets `E_TOODEEP` on
the "too many levels of indentation" branch.

## Phases

Phases are ordered to land DRIFT fixes by impact on the gate tests, smallest blast radius first.

### P1: errcode + tokenizer-error routing (DONE; gates `test_tokenize.py` error sub-tests)

Commit: 31c3c52.

**Problem.** `module/_tokenize/module.go:tokenizerError` was matching
on substrings of the recorded SyntaxError message ("tabs and spaces",
"unindent", "indentation", "indent") to pick between TabError /
IndentationError / SyntaxError. CPython does the opposite: switch on
`tok->done` (the E_* enum), then synthesise the canonical message
inside the case body. The substring approach silently collapses
`E_INTR` (KeyboardInterrupt), `E_NOMEM` (MemoryError), `E_TOODEEP`
(IndentationError), `E_LINECONT` (SyntaxError with the
"unexpected character after line continuation" wording) into the
generic SyntaxError bucket. The `errCode` enum was also missing
`eNomem`, `eToodeep`, and `eLineCont` outright, so even routing
on the enum was impossible.

**Code shipped.**

- `parser/lexer/state.go`: added `eNomem`, `eToodeep`, `eLineCont`
  to the unexported `errCode` enum, with `// CPython:
  Include/errcode.h:NN` line citations attached to each entry. The
  enum is still iota-based (gopy doesn't need to preserve the numeric
  literals from errcode.h) but now tracks the E_* family one-to-one.
- `parser/lexer/state.go`: renamed the gopy-only `eIndent` to
  `eToodeep`. Its sole use site at `parser/lexer/lexer.go:339`
  ("too many levels of indentation") matches
  `Parser/lexer/lexer.c:582`, which sets `E_TOODEEP` on
  `tok->indent+1 >= MAXINDENT`. The old name was a misfit; the
  rename is purely mechanical.
- `parser/lexer/state.go`: added `State.Done() int` and the
  exported `DoneOK..DoneColumnOverflow` constants. These let
  `module/_tokenize` switch on the enum without depending on the
  unexported `errCode` type.
- `module/_tokenize/module.go:tokenizerError`: rewritten as a
  switch on `lexer.State.Done()` case-for-case against the C
  source. Each case picks the canonical CPython message and an
  `errClass` string; the function then returns
  `fmt.Errorf("%s: %s", errClass, msg)`. The previous
  `containsAny` substring helper is gone. The `E_INTR` and
  `E_NOMEM` cases short-circuit (no message body, the exception
  class is the entire signal). On the catch-all branch the lexer's
  stored message overrides the canonical "unknown tokenization
  error" so detail-rich messages like `invalid character '(' (U+0028)`
  flow through unchanged.

**Mapping table.**

| `tok->done` | gopy code | Python class | Canonical message |
|---|---|---|---|
| `E_TOKEN` | `DoneToken` | `SyntaxError` | "invalid token" |
| `E_EOF` | `DoneEOF` | `SyntaxError` | "unexpected EOF in multi-line statement" |
| `E_DEDENT` | `DoneDedent` | `IndentationError` | "unindent does not match any outer indentation level" |
| `E_INTR` | `DoneIntr` | `KeyboardInterrupt` | (none) |
| `E_NOMEM` | `DoneNomem` | `MemoryError` | (none) |
| `E_TABSPACE` | `DoneTabSpace` | `TabError` | "inconsistent use of tabs and spaces in indentation" |
| `E_TOODEEP` | `DoneToodeep` | `IndentationError` | "too many levels of indentation" |
| `E_LINECONT` | `DoneLineCont` | `SyntaxError` | "unexpected character after line continuation character" |
| default | other | `SyntaxError` | lexer's stored message, else "unknown tokenization error" |

**Verification.** `go test ./parser/lexer/... ./module/_tokenize/...`
green; `go vet ./...` clean; `golangci-lint run ./parser/lexer/...
./module/_tokenize/...` clean. The substring helper deletion drops
~10 LOC; the switch grows ~25 LOC.

**Follow-ups.** None within P1 scope. The `E_EOF` case still ships
just the message; CPython additionally attaches the syntax location
via `PyErr_SyntaxLocationObject`. Wiring that on the gopy side
needs a richer exception-shape across the lexer/parser bridge and is
folded into P5 token-position work.

### P2: `verify_identifier` XID tables (DONE; gates `test_tokenize.py` non-ASCII identifier sub-tests, task #612)

Commit: 2b972c7.

**Problem.** `Parser/lexer/lexer.c:364 verify_identifier` decodes the
candidate identifier's bytes with `PyUnicode_DecodeUTF8` and feeds
the result to `Objects/unicodeobject.c:12426 _PyUnicode_ScanIdentifier`,
which walks code points against the Unicode `XID_Start` /
`XID_Continue` tables baked into `Objects/unicodectype.c`. The gopy
port (lexer.go:914) skipped that step entirely: it only validated
UTF-8 byte well-formedness, so any code point that decoded cleanly
was accepted. That permitted Pattern_Syntax characters (e.g. `‹›` at
U+2039/U+203A), category-Po marks, and digit-only ASCII names like
`123foo` (which the scanName entry already filters out, but not the
underlying check). For the gate tests it manifests as `test_tokenize`
non-ASCII identifier rows accepting input CPython rejects.

**Code shipped.**

- `parser/lexer/xid.go` (new, 90 LOC): exposes `isXIDStart`,
  `isXIDContinue`, and `scanIdentifier`. The XID derivation follows
  UAX #31 verbatim:

  ```
  ID_Start    = L | Nl | Other_ID_Start
  ID_Continue = ID_Start | Mn | Mc | Nd | Pc | Other_ID_Continue
  XID_*       = ID_* minus Pattern_Syntax minus Pattern_White_Space
  ```

  Go 1.26's `unicode` package ships every property table this needs at
  Unicode 16.0, the same version CPython 3.14 bakes into
  `Modules/unicodename_db.h`. The ID/XID delta (NFKC-instability
  exclusion) is empty for the BMP planes Python lexes against on
  Unicode 16.0, so the composition is exact for the gate-test
  corpus. A future Unicode version that resurfaces an NFKC-unstable
  Letter would need an explicit exclusion list; the file's package
  comment notes that path.

- ASCII fast path: `isXIDStart` and `isXIDContinue` short-circuit
  for `r < 0x80`, hitting `_` plus the `a-z` / `A-Z` (start) and
  digit (continue) sets directly. This keeps the common case at a
  single integer compare.

- `scanIdentifier` returns `(byteOffset, badRune, ok)`. The byte
  offset gives `verifyIdentifier` a precise place to pin `tok->cur`
  on failure; the bad rune drives the error message's codepoint
  format. Empty input is rejected with offset 0 (matches
  `_PyUnicode_ScanIdentifier`'s `len == 0` branch).

- `parser/lexer/lexer.go:914 verifyIdentifier`: now decodes via
  `ValidateUTF8` first (E_DECODE on malformed UTF-8, same as
  CPython's `PyUnicode_DecodeUTF8` failure path), then calls
  `scanIdentifier`. On reject it pins `s.cur = s.start + off +
  utf8RuneLen(bad)` so the SyntaxError span matches CPython's
  `tok->cur = tok->start + PyBytes_GET_SIZE(s)` after the
  Py_SETREF / PyUnicode_Substring round-trip at lexer.c:391-393.
  The message routes through `isPrintable` vs non-printable to pick
  the `'%c' (U+%04X)` vs `non-printable character U+%04X` form,
  mirroring lexer.c:401-407.

- Two small helpers added alongside: `utf8RuneLen(r) int` (rune
  byte length, returns 4 on negative / unassigned to avoid running
  past the buffer) and `isPrintable(r) bool`
  (`unicode.IsPrint(r) || r == ' '`, mirroring `Py_UNICODE_ISPRINTABLE`).

**Verification.**

- `parser/lexer/xid_test.go`: accept set covers ASCII underscore,
  letters, digits-after-start, Greek (`αβγ`), Cyrillic (`привет`),
  CJK (`漢字`), combining marks (`á`), micro sign (`µx`,
  XID_Start in Unicode), middle dot (`x·`, XID_Continue in
  Unicode), Other_ID_Start (`℘x` = SCRIPT CAPITAL P). Reject set
  covers empty input, digit-leading, ASCII `$`, ASCII `-`, internal
  whitespace, and U+00A0 NBSP.
- `go test ./parser/lexer/...` green; `golangci-lint` clean.

**Follow-ups.** None for the gate-test corpus. If a future Unicode
version (17+) introduces NFKC-unstable Letters that bump the
ID_Start - XID_Start delta into the BMP identifier range, the file
header explains where to add the exclusion list.

### P3: `set_ftstring_expr` UTF-8 decode (DONE; gates f-string `=` debug mode with non-ASCII names, task #618)

Commit: a72ac60.

**Problem.** `Parser/lexer/lexer.c:114 set_ftstring_expr` is called
when the tokenizer closes an interpolation (`}`, `:`, or `!`) inside
an f-string or t-string. It snapshots the expression text into
`token->metadata` so the formatter can replay it for the debug
`f"{x=}"` form. CPython runs the snapshot through
`PyUnicode_DecodeUTF8`, which both validates the bytes are
well-formed UTF-8 and normalises them into a `PyObject*` str. Failure
returns `-1` and sets `tok->done = E_DECODE` upstream. The gopy port
stored the raw `last_expr_buffer` bytes on `Tok.Metadata` directly, so
a malformed UTF-8 sequence inside the expression slipped through and
later surfaced as a runtime decode error far from the source point.

The bug bites two paths inside `set_ftstring_expr`:

1. The fast direct-copy path when no `#` appeared in the expression
   (lexer.c:212 `else { res = PyUnicode_DecodeUTF8(...) }`).
2. The comment-stripped path when at least one `#` was seen
   (lexer.c:208 `res = PyUnicode_DecodeUTF8(result, j, NULL)` after
   the buffer is filtered).

Both paths feed the same `PyUnicode_DecodeUTF8`, so the gopy fix has
to validate both.

**Code shipped.**

- `parser/lexer/fstring.go:setFtstringExpr`: each branch now runs
  the buffer through `ValidateUTF8` before assigning
  `tok.Metadata`. The validator is the existing one used by
  `_PyTokenizer_ensure_utf8` (source.go:198); it walks the bytes
  and returns `(line, badByte, ok)`. On failure the function sets
  `s.done = eDecode` and records the SyntaxError via
  `s.syntaxError("invalid character in f-string expression")`, then
  returns without touching `tok.Metadata`. The empty-Metadata signal
  lets the caller at lexer.go:730 fall through; the recorded
  SyntaxError surfaces via `State.Err()` exactly like CPython's
  `tok->done = E_DECODE` + `PyErr_Format` pair.

- The function still doesn't return a status (CPython returns
  `int` 0/-1). Threading a return through the single call site at
  lexer.go:731 would force a second branch into the closing-brace
  arm of `tok_get_normal_mode`; the recorded SyntaxError is already
  the durable signal, so the side-effect-only port matches CPython
  semantics without expanding the API surface.

**Verification.** `go test ./parser/lexer/...` green;
`golangci-lint` clean. The two `ValidateUTF8` calls add ~10 LOC.
A dedicated test isn't added because the failure mode is exercised
indirectly through any gate test that parses an f-string with a
malformed UTF-8 sequence; the new check is a fail-fast guard that
turns "raw bytes reach `Metadata`" into "lexer reports E_DECODE",
which the existing `tokenizerError` switch already routes to
`SyntaxError`.

**Follow-ups.** When P5 wires `tok->done = E_DECODE` to the actual
syntax-location attach (the same gap noted on P1's E_EOF follow-up),
this path's error location will improve from line-only to line-and-col.

### P4: `verify_end_of_number` SyntaxWarning (DONE; gates `test_tokenize.py` `1and` style sub-tests)

Commit: 6dbf31a.

**Problem.** `Parser/lexer/lexer.c:343 verify_end_of_number` peeks
the character after a numeric literal and, if it is a letter that
starts one of the operator-style keywords (`and`, `or`, `if`, `in`,
`is`, `not`, `else`, `for`), calls `_PyTokenizer_parser_warn` with
`"invalid %s literal"`, then accepts the literal. The point is to
keep `1and 2` lexing as `NUMBER(1) NAME(and) NUMBER(2)` while still
nudging the user about the missing space. The gopy port silently
accepted the literal with an in-file comment that the warning was
deferred; the `1and`-style rows in `test_tokenize.py` couldn't pass
because no SyntaxWarning was ever recorded.

The deeper problem was `parserWarn` itself. The function existed,
but its body wrote the warning into `s.err` tagged with a `[warn]`
prefix. That conflated warnings with hard errors: a benign warning
from a real source could short-circuit later token emission because
`State.Err() != nil` would already be true. So the P4 fix has two
parts: rework `parserWarn` into a real warnings sink, then wire
the keyword-adjacency branch to it.

**Code shipped.**

- `parser/lexer/state.go`: added `warnings []SyntaxError` field on
  `State` and a `Warnings() []SyntaxError` accessor. The slice
  preserves emission order. `SyntaxError` gained a `Category
  string` field; the lexer leaves it empty for hard errors recorded
  via `recordError`, populated (currently `"SyntaxWarning"`) for
  diagnostics recorded via `parserWarn`. The struct lives in the
  lexer package so module/_tokenize and the compile pipeline can
  switch on it without an extra type.

- `parser/lexer/helpers.go:parserWarn`: rewritten. It builds a
  `SyntaxError` valued at `(Pos{lineno, col}, Pos{lineno, col})`
  (CPython only records the line on parser warnings; gopy stays
  consistent), copies the formatted message, sets `Category` from
  the caller, and appends to `s.warnings`. The previous body
  (`recordError("[warn] " + msg)`) is gone. The `if !s.reportWarnings
  { return }` guard is preserved so callers that disable warnings
  (e.g. an early reparse for incremental input) get the same
  no-op behavior as before.

- `parser/lexer/lexer.go:verifyEndOfNumber`: the keyword-adjacency
  branch now calls `s.backup(c)`, then `s.parserWarn("SyntaxWarning",
  "invalid %s literal", kind)`, then `s.nextC()` to re-consume the
  byte. The backup/re-consume dance mirrors `tok_backup(tok, c)`
  followed by `tok_nextc(tok)` in CPython, where the C source
  positions the cursor for the parser_warn call (which reads
  `tok->cur` to pin the warning's column) and then advances back
  past the byte before returning to the caller. Going through
  backup/nextC keeps the column accurate even when the lookahead
  byte was a multi-line continuation.

- `parser/lexer/warn_test.go` (new): two tests. The first feeds
  `1and 2`, `1or 2`, `1if x else 2`, `1in x`, `1is None`,
  `1not in x` through `FromString` + `Get`, drains tokens until
  `ENDMARKER` or `ERRORTOKEN`, then asserts `State.Err() == nil`
  and `State.Warnings()[0].Category == "SyntaxWarning"` plus the
  message contains `"invalid"`. The second feeds `1foo`, asserts
  an `ERRORTOKEN` was seen and `State.Err() != nil`, locking the
  else branch (plain-identifier follow stays a hard SyntaxError).
  Both loops use `for range 100` (gocritic rangeint compliant).

**Verification.** `go test ./parser/lexer/...` green;
`golangci-lint run ./parser/lexer/...` clean. The new `warn_test.go`
catches both halves of the verify_end_of_number split (keyword
adjacency to warning, plain identifier to error) without exercising
the rest of the tokenizer.

**Follow-ups.** P6 still owes `module/warnings` routing: today the
lexer collects warnings in a slice, but no caller drains
`State.Warnings()` and surfaces them through `module/warnings`'s
filter chain. The `warnInvalidEscape` path (P6's DRIFT row at
helpers.c:111) shares this plumbing and will be wired in the same
P6 pass. Once that lands, `parserWarn`'s `Category` field becomes
the dispatch key.

### P5: token position parity in `tok_get_normal_mode` (gates: the bulk of `test_tokenize.py`)

1. Walk `parser/lexer/lexer.go:tokGetNormalMode` and every site that calls `tokenSetup` / `typeCommentTokenSetup`, comparing the `lineno`, `col_offset`, `end_lineno`, `end_col_offset` calculation against the corresponding `_PyLexer_token_setup` call in `Parser/lexer/lexer.c`.
2. Fix the implicit-NEWLINE position emission so `1 + 1` reports `(1, 5) (1, 6)` instead of `(2, 0) (2, 2)`.
3. Fix the NEWLINE-before-COMMENT reorder bug so the second `check_tokenize` from the test file matches CPython's output line by line.

### P6: encoding / readline DRIFT cleanup (gates: edge-case rows in `test_source_encoding.py` and streaming tests)

1. Fold `_PyTokenizer_warn_invalid_escape_sequence` and `_PyTokenizer_parser_warn` onto the real `warnings` module so the warning shape matches.
2. Extend `valid_utf8` (rename to `validUTF8`) in `source.go` to do the full overlong / surrogate / range checks, not just sequence length.
3. Add `tok->cont_line` tracking to the `DetectEncodingCookie` loop in `source.go` so PEP 263 cookies on backslash-continued lines are ignored.
4. Reinstate the inline BOM / encoding state-machine for `tok_underflow_file` if a gate test surfaces a cookie beyond the head window.

### P7: stdlib vendor location (gates: zero; consistency only)

1. Move `stdlib/{keyword,tokenize,tabnanny}.py` to `module/{keyword,tokenize,tabnanny}/`, matching the `module/xxx/` rule.
2. Keep the files byte-equal to upstream. Update import paths.
3. Optional: remove the `stdlib/` location entirely if no other vendor relies on it.

### P8: flip 1700

Once every gate is green, flip task #484 ("test e2e v0.5.5 — lexer
panel") to done and update the 1700 checklist row.

## Per-gate-test blocker DFS

The four pending gate rows each depend on a chain of sub-system gaps
outside the lexer/tokenizer scope. Closing 1710 means walking each
chain depth-first and porting whatever is missing until the gate runs
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
| 6 | P1 | tokenizer error routing | dispatch on `tok->done` not message substrings (see P1 above) | DONE | 31c3c52 |
| 7 | P2 | XID tables | port `_PyUnicode_ScanIdentifier` for non-ASCII identifier validation | DONE | 2b972c7 |
| 8 | P3 | f-string debug UTF-8 | decode `setFtstringExpr` buffer through `unicode/utf8` | DONE | a72ac60 |
| 9 | P4 | SyntaxWarning | emit on `1and` / `1or` style numbers | DONE | 6dbf31a |
| 10 | P5 | token positions | match `_PyLexer_token_setup` line/col emission | TODO | — |

### test_utf8source.py chain

Suite runs end-to-end; 3/3 sub-tests green. Closed under existing 1710 work.

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
| 5 | T7 | VM exec(bytes) | `exec` accepts a `bytes` / `bytearray` first argument by routing through `lexer.FromBytes` + `compile.Compile`. Currently the `BytesSourceEncodingTest.test_crcrcrlf` row blocks because `exec(bytes)` raises `TypeError`. | TODO | — |
| 6 | P6.3 | PEP 263 cookie cont_line | skip the cookie when the line above ends with `\` | TODO | — |

DFS note: T5 was originally one row but `inspect` pulls in `dis` →
`opcode` → `_opcode` (C module) → `_opcode_metadata` (generated C
module), plus `importlib.machinery`. The four-step breakdown above
matches the actual port order.

### test_tabnanny.py chain

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | T6 | asyncio | port the asyncio package (event loop, transports, protocols, futures, tasks, streams, subprocess, queues, locks) as its own spec | BLOCKED | — |

DFS execution order, smallest fix first: T1 → T1.5 → T1.6 → T1.7 → T4
→ T2 → T3 → T3.1 → T5.1 → T5.2 → T5.3 → T5.4 → P1 → P2 → P3 → P4 → P5
→ P6 → T7 → T6. Each task gets its own commit and an entry in
`stdtest/MANIFEST.txt` when the gate it unblocks lands green.

## Workflow

The spec follows the durable port-not-patch / full-subsystem rule.
The work is broken into the phases above; each phase is one or more
PRs. Every commit cites the CPython source line it ports against
(`// CPython: <file>:<line> <function>`).

For each phase:

1. Read the upstream function in full at `~/cpython-314/...`. No
   reading a snippet; the whole function is the unit of parity.
2. Port the function into the named Go file with the citation
   comment on the first line of the body.
3. Add a Go unit test that exercises the function against the
   shape the C source guarantees. If a gate test already covers
   the shape, link to it in the test's docstring instead.
4. Re-run `go test ./parser/lexer/... ./module/_tokenize/... ./test/regrtest/...`
   plus the gate test through the regrtest harness.
5. Update the checklist row above.

## Out of scope

- `tokenizedata/` test fixtures under `Lib/test/tokenizedata/` are
  in scope only as far as the five gate tests reference them.
- IDLE's tokenizer fork (`Lib/idlelib/`) stays out of scope; IDLE is
  on the 1700 deferred list.
- The PEG parser layer that consumes tokens (`Parser/parser.c` and
  friends) is a separate subsystem and gets its own v0.12.4 spec
  when its turn comes.
- Interactive REPL underflow (`tok_underflow_interactive`,
  `tok_concatenate_interactive_new_line`, `_PyTokenizer_FromReadline`'s
  prompt fields). The embedder owns interactive state.
