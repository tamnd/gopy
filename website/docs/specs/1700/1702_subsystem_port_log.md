---
format: md
id: 1702_subsystem_port_log
title: "1702. Subsystem port log (v0.12.1 unittest enablement)"
sidebar_label: "1702. subsystem_port_log"
sidebar_position: 1702
slug: /specs/1702-subsystem_port_log
description: "Per-subsystem port detail for the unittest enablement push under spec 1701. A single status table tracks every subsystem; paragraphs below capture the structured detail (surface, location, deferred items, gate) for each one that has landed or is blocked. Read this when triaging downstream failures so you know which CPython source files have been brought across in full versus which are still identity-shim placeholders."
---


This document is the audit trail for spec 1701's "no partial stubs"
rule. The status table below names every subsystem the unittest
gate touches; entries marked **done** or **partial** have a
paragraph in the detail section. If a paragraph and the code ever
disagree, the code wins and both must be reconciled in the same
commit that fixes the drift.

## Rule

Every CPython source file backing a pending or partial subsystem
below is ported **in full**. No function in those files may be
left unported. The deliverable for each subsystem is a Go file
whose function list 1:1 covers the C / Python function list and
whose public names match CPython 3.14. Identity-shim placeholders
are not acceptable: once a subsystem lands under this spec we do
not come back to it for a missing function. This is the same rule
1704 applies to the object protocol; 1702 extends it to the
unittest enablement subsystems.

## Pending subsystem deep dives

Each section below covers one pending subsystem with the structure
1704 uses for its in-scope files: an overview paragraph that frames
what we are porting and why, a Files-in-scope table, a
Functions-to-port table per file naming every C / Python entity
that must land, a Gate paragraph with the exact acceptance script,
and a Deferred / Notes paragraph capturing dependencies and
acknowledged shortcuts. When a port lands, flip the row's status
in the per-file table and in the master Status table near the top
of the file; both must agree.

Sources of truth live under `/Users/apple/cpython-314/`. Every Go
function ported under this spec carries a
`// CPython: <path>:<line> <name>` citation pointing at the exact
upstream line that motivated it.

### collections (#497)

**Overview.** The `collections` subsystem ships six recurring data
structures the unittest stack assumes are present:
`deque`, `OrderedDict`, `defaultdict`, `Counter`, `ChainMap`, and
the `namedtuple` factory. The runtime-level containers
(`deque`, `OrderedDict`, `defaultdict`) live in the C extension
`Modules/_collectionsmodule.c` because they need C-speed
operations and inline struct layout; the pure-Python adapters
(`Counter`, `ChainMap`, `namedtuple`, `UserDict`, `UserList`,
`UserString`) and the `__init__.py` re-exports live in
`Lib/collections/__init__.py`. Both files must land before
`unittest.mock`, which is the next-up consumer.

**Files in scope.**

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Modules/_collectionsmodule.c` | ~3,000 | `module/_collections/` | done |
| B | `Lib/collections/__init__.py` | ~1,500 | `stdlib/collections/__init__.py` | done |

**Functions to port (A: `_collectionsmodule.c`).** Every C struct
type below lands as a Go `*Type` with a backing Go struct.

| C type / function | Exposed as | gopy hook | Status |
|-------------------|-----------|-----------|--------|
| `deque_type` (`deque_new`, `deque_init`, `deque_dealloc`, `deque_traverse`, `deque_clear`, `deque_methods` table) | `_collections.deque` | `module/_collections/deque.go` | done |
| `deque_append`, `deque_appendleft`, `deque_pop`, `deque_popleft`, `deque_extend`, `deque_extendleft`, `deque_rotate`, `deque_reverse`, `deque_remove`, `deque_count`, `deque_index`, `deque_insert`, `deque_copy`, `deque_clearmethod` | `deque.<method>` | descriptors on `deque_type` | done |
| `deque_richcompare`, `deque_iter`, `deque_reduce`, `deque_repr`, `deque_concat`, `deque_inplace_concat`, `deque_contains`, `deque_len`, `deque_getitem`, `deque_setitem` | dunder + sequence slots on `deque_type` | slot fields | done |
| `defdict_type` (`defdict_init`, `defdict_missing`, `defdict_copy`, `defdict_reduce`, `defdict_repr`, `defdict_or`, `defdict_ror`) | `_collections.defaultdict` | `module/_collections/defaultdict.go` | done |
| `odictobject` (`odict_new`, `odict_init`, `odict_dealloc`, `odict_traverse`, `odict_clear`, `odict_iter`, `odict_repr`, `odict_richcompare`, `odict_eq`, `odict_reduce`, `odict_copy`, `odict_setitem`, `odict_delitem`, `odict_popitem`, `odict_move_to_end`, `odict_keys`, `odict_values`, `odict_items`, `OrderedDict_*Iterator`) | `_collections.OrderedDict` | reuse / extend existing `objects/odict.go` | done |
| `tuplegetter_type` (`tuplegetter_new`, `tuplegetter_descr_get`, `tuplegetter_descr_set`, `tuplegetter_reduce`) | `_collections._tuplegetter` (named-tuple helper) | `module/_collections/tuplegetter.go` | done |
| module-level init (`_collectionsmodule_exec`, type ready) | `import _collections` | `module/_collections/module.go` + `stdlibinit/registry.go` blank-import | done |

**Functions to port (B: `collections/__init__.py`).** Vendor
byte-equal once A is in. Public surface:

| Python entity | Source span | Status |
|---------------|-------------|--------|
| `__all__` re-export list | top of file | done |
| `Counter` (init, `most_common`, `subtract`, `__add__`, `__sub__`, `__or__`, `__and__`, `__pos__`, `__neg__`, `total`) | `Lib/collections/__init__.py:600-870` | done |
| `ChainMap` (init, `new_child`, `parents`, `maps`, the MutableMapping methods, `__missing__`, `__contains__`, `__bool__`, `__or__`, `__ror__`, `__ior__`) | `Lib/collections/__init__.py:912-1090` | done |
| `namedtuple(typename, field_names, ...)` factory and the `_*` helper module | `Lib/collections/__init__.py:381-595` | done |
| `UserDict`, `UserList`, `UserString` | `Lib/collections/__init__.py:1100-1530` | done |

**Gate.** `TestCollectionsImportResolvesNames` passes: `import collections` loads `stdlib/collections/__init__.py` via PathFinder and exposes `OrderedDict` and `deque` in the module dict.

**Deferred.** None.

### traceback (#496)

**Overview.** `traceback` is what every Python exception printer
calls. It is consumed directly by `unittest.TestResult` (via
`unittest._textresult._exc_info_to_string`) and indirectly by the
default `sys.excepthook`. Vendor byte-equal and add only the Go
helpers needed to back names the vendor cannot supply (frame
introspection, `linecache` integration, the `_colorize` hook).

**Files in scope.**

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Lib/traceback.py` | ~1,300 | `stdlib/traceback.py` | partial |

**Functions to port (A: `traceback.py`).**

| Python entity | Source span | Status |
|---------------|-------------|--------|
| Module-level helpers `print_tb`, `format_tb`, `print_exception`, `format_exception`, `print_exc`, `format_exc`, `print_last`, `print_stack`, `format_stack`, `extract_tb`, `extract_stack`, `clear_frames`, `walk_tb`, `walk_stack` | `Lib/traceback.py:30-260` | partial |
| `FrameSummary` (init, `__repr__`, `__eq__`, `line`) | `Lib/traceback.py:270-360` | partial |
| `StackSummary` (init, `extract`, `from_list`, `format`, `format_frame_summary`) | `Lib/traceback.py:370-540` | partial |
| `TracebackException` (init, `from_exception`, `format`, `format_exception_only`, `_format_final_exc_line`, `__eq__`, `__str__`, exception chain `__cause__` / `__context__` formatting) | `Lib/traceback.py:550-1050` | partial |
| `_Sentinel`, `_safe_string`, `_some_str`, `_format_traceback_exception_list`, `_walk_tb_with_full_positions`, `_extract_caret_anchors_from_line_segment`, the syntax-error caret renderer | `Lib/traceback.py:1060-1300` | partial |

All rows are partial rather than pending: `stdlib/traceback.py` is vendored byte-equal and the Python code runs. `format_exc()` returns `"ValueError: sentinel\n"` correctly. The stack-frame rows (`FrameSummary`, `StackSummary.extract`, `_walk_tb_with_full_positions`) execute but produce empty output because the VM does not yet record `exc.__traceback__` (the `tb_frame` / `tb_lineno` chain). Every function runs without raising; the output is just missing the "Traceback (most recent call last)" preamble and the file/line lines.

**Runtime support landed.** `sys.exc_info()` (3-tuple) and `sys.exception()` (Python 3.11+ single-value form) both present and wired to the per-thread handled-exception slot. `linecache` imports and `getline` resolves. `frame.f_code.co_filename` and `f_lineno` are exposed via `objects/frame.go`.

**Blocker.** VM does not set `exc.__traceback__` when an exception is raised. `TracebackException.__init__` calls `walk_tb(tb)` which calls `tb.tb_frame` and `tb.tb_lineno`; with a nil tb the walk yields nothing. Fixing this requires the VM to construct a `TracebackObject` on every RAISE_VARARGS and chain it through try/except unwind. This is the next open item under #496.

**Gate (current).** `TestTracebackFormatExc` passes: `format_exc()` returns a string containing both "ValueError" and "sentinel". Full multi-line stack trace gate is blocked on __traceback__ population.

### io / _io (#514)

**Overview.** The `io` stack is the entire byte / text streaming
surface: open() routes through here, every file-like wrapper that
`unittest`, `logging`, `argparse`, and the runner use sits on
top. CPython splits the C extension across seven files
(`Modules/_io/`); each defines one class. The pure-Python `Lib/io.py`
is a thin re-export shim that the vendor will replace once the C
side is real.

**Files in scope.**

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Modules/_io/_iomodule.c` | ~700 | `module/io/module.go` | done |
| B | `Modules/_io/iobase.c` | ~900 | `module/io/iobase.go` | partial |
| C | `Modules/_io/fileio.c` | ~1,200 | `module/io/fileio.go` | partial |
| D | `Modules/_io/bufferedio.c` | ~2,500 | `module/io/bufferedio.go` | partial |
| E | `Modules/_io/textio.c` | ~3,400 | `module/io/textiowrapper.go` | partial |
| F | `Modules/_io/stringio.c` | ~1,100 | `module/io/stringio.go` | done |
| G | `Modules/_io/bytesio.c` | ~1,100 | `module/io/bytesio.go` | done |
| H | `Lib/io.py` | ~100 | `stdlib/io.py` | done |

**Audit (2026-05-14).** A re-audit on 2026-05-14 found the
2026-05-13 commits that flipped D and E to **done** ("io D: full
port of Modules/_io/bufferedio.c", "io E: full port of
Modules/_io/textio.c") shipped ports that are 31–41% of the
upstream line count with major functional gaps. The same audit
shows B, C, F, G ship 35–55% of upstream and miss whole methods
(`readinto` on FileIO, `getbuffer` on BytesIO, the
`__getstate__/__setstate__` pickle pair on StringIO). The status
above and the per-function tables below have been flipped back
from **done** to the truth. Outstanding work is captured in the
"Missing" notes after each function table; #571–#576 are
re-opened and a follow-up commit will port the remaining
functions in full.

**Functions to port (A: `_iomodule.c`).**

| C function | Exposed as | Status |
|------------|-----------|--------|
| `_io_open_impl` (the `open()` builtin's real implementation) | `_io.open` (and re-exported as `builtins.open`) | done |
| `_io_open_code_impl`, `_io_text_encoding_impl` | `_io.open_code`, `_io.text_encoding` | done |
| `_io_UnsupportedOperation` class init | `_io.UnsupportedOperation` (subclass of OSError + ValueError) | done |
| `_io_BlockingIOError` definition | `_io.BlockingIOError` | done |
| Module-level `_iomodule_exec` (type ready, exception ready, constant install) | `import _io` | done |

**Functions to port (B: `iobase.c`).**

| C function | Exposed as | Status |
|------------|-----------|--------|
| `iobase_seek`, `iobase_tell`, `iobase_truncate`, `iobase_flush`, `iobase_close`, `iobase_closed` getset, `iobase_readable`, `iobase_writable`, `iobase_seekable` | `_IOBase.<method>` | done |
| `iobase_iter`, `iobase_iternext`, `iobase_readline`, `iobase_readlines`, `iobase_writelines`, `_checkClosed`, `_checkSeekable`, `_checkReadable`, `_checkWritable` | dunder + check helpers on `_IOBase` | done |
| `iobase_enter`, `iobase_exit` | `__enter__`, `__exit__` context manager | done |
| `iobase_fileno`, `iobase_isatty` | `_IOBase.fileno`, `_IOBase.isatty` | done |
| `rawiobase_read`, `rawiobase_readall`, `rawiobase_readinto`, `rawiobase_write` | `_RawIOBase` | partial |
| `bufferediobase_*` | `_BufferedIOBase` (in bufferedio.c, not iobase.c) | partial |
| `textiobase_*` | `_TextIOBase` (in textio.c, not iobase.c) | done |

**Missing (B audit 2026-05-14).** `_RawIOBase.readinto` and
`_RawIOBase.write` are stubs that raise `NotImplementedError`
rather than the CPython `UnsupportedOperation` from
`rawiobase_readinto`/`rawiobase_write`; the abstract bodies need
to call `PyErr_SetString(io.UnsupportedOperation, ...)`.
`_BufferedIOBase` is a Go-side shim with method bodies that
return `UnsupportedOperation` for `read`/`read1`/`readinto`/
`readinto1`/`write`/`detach` but does **not** ship the abstract
fallback `BufferedIOBase.readinto`/`readinto1` (which CPython
implements by calling `self.read(len(b))` and copying); see
`Modules/_io/bufferedio.c:74-130`. `_IOBase.__del__` (the
finalizer that auto-flushes) and `_PyIOBase_finalize` are not
ported.

**Functions to port (C: `fileio.c`).**

| C function | Exposed as | Status |
|------------|-----------|--------|
| `fileio_init`, `fileio_dealloc`, `fileio_close`, `fileio_closefd`, `fileio_fileno`, `fileio_isatty`, `fileio_readable`, `fileio_writable`, `fileio_seekable`, `fileio_mode_repr`, `fileio_name` | `FileIO.<method>` | partial |
| `fileio_read`, `fileio_readall`, `fileio_readinto`, `fileio_write`, `fileio_seek`, `fileio_tell`, `fileio_truncate`, `fileio_repr` | core I/O ops | partial |
| Open-flag resolution (`fileio_check_closed`, `mode_to_flags`, `_PyFileIO_closefd`) | helpers | partial |

**Missing (C audit 2026-05-14).**
`_io_FileIO_readinto_impl` is **not** ported; `readinto(b)` does
not exist on `FileIO` even though the buffered layer calls it.
`fileno()` returns no fd (the method is absent). `closefd=False`
is silently ignored. The `opener` callable is parsed and
discarded. Existing-fd construction (`FileIO(fd_int, ...)`) is
not supported. `fileio_isatty_open_only`, `fileio_dealloc_warn`,
the `blksize` getset, and the path-converter slow-path are all
absent.

**Functions to port (D: `bufferedio.c`).**

| C class | Public methods | Status |
|---------|---------------|--------|
| `BufferedReader` | `read`, `read1`, `peek`, `readline`, `readinto`, `readinto1`, `seek`, `tell`, `close`, `flush`, `__init__`, `raw`, `mode`, `name`, `closed` | partial |
| `BufferedWriter` | `write`, `flush`, `close`, `seek`, `tell`, `truncate`, `detach`, `__init__` | partial |
| `BufferedRandom` | union of Reader + Writer behaviours | partial |
| `BufferedRWPair` | `read`, `write`, `peek`, `readinto`, `close`, `flush`, `readable`, `writable` | partial |
| Shared internals (`_bufferedreader_raw_read`, `_bufferedwriter_flush_unlocked`, `_PyIO_State`) | helpers | partial |

**Missing (D audit 2026-05-14).** The current 1133-line port is
41% of `bufferedio.c` (2773 lines) and diverges from CPython on
the *buffer model itself*: gopy stores a separate `readBuf` and
`writeBuf`, while CPython's `buffered` struct (line 222) has a
single `buffer` slab with `pos`, `raw_pos`, `read_end`,
`write_pos`, `write_end` pointers so a `BufferedRandom` can
interleave reads and writes without losing position. Specific
gaps: `_io__Buffered___sizeof___impl` returns `bufSize` instead
of `tp_basicsize + bufSize`; `_io__Buffered_closed_get_impl`
reads a local Go flag instead of `self.raw.closed`, so a raw
stream closed by something else leaves the buffered wrapper
appearing open; `_buffered_init` does not validate that the raw
of a `BufferedRandom` is seekable; `buffered_flush_and_rewind_unlocked`,
`_bufferedreader_raw_read`, `_bufferedreader_read_fast`,
`_bufferedwriter_raw_write`, `_io__Buffered__dealloc_warn_impl`,
`buffered_iternext` (so `for line in bf:` falls back to the
generic IOBase iterator), `buffered_repr`, and the
`__init__`-as-real-method path are absent. No thread lock /
owner reentrancy guard. `bufferedTell` does not subtract the
write-buffer offset. Several `bufRead` error returns are
swallowed (the nilerr lints flag this).

**Functions to port (E: `textio.c`).**

| C class | Public methods | Status |
|---------|---------------|--------|
| `IncrementalNewlineDecoder` | `decode`, `getstate`, `setstate`, `reset`, `newlines` | partial |
| `TextIOWrapper` | `__init__`, `read`, `readline`, `readlines`, `write`, `seek`, `tell`, `truncate`, `flush`, `close`, `detach`, `reconfigure`, `buffer`, `encoding`, `errors`, `newlines`, `line_buffering`, `write_through`, `name`, `mode`, `closed`, `__iter__`, `__next__` | partial |
| Internals (`_textiowrapper_decoder_setstate`, `_textiowrapper_encoder_setstate`, `_textiowrapper_writeflush`) | helpers | partial |

**Missing (E audit 2026-05-14).** The 1075-line port is 31% of
`textio.c` (3433 lines). Specific gaps: codec resolution
hardcodes utf-8/ascii/latin-1 in `decodeBytes`/`encodeString`
instead of routing through `codecs.lookup(encoding)`, so most
real encodings raise `NotImplementedError`. `read()` and
`readline()` read the whole underlying buffer at once and decode
in one shot; CPython's `textiowrapper_read_chunk` reads
`CHUNK_SIZE` bytes, decodes incrementally via
`IncrementalDecoder`, and tracks a `dec_flags` "snapshot" cookie
so `tell()` returns a position the next `seek()` can replay.
`IncrementalNewlineDecoder.decode` does not honor `final=False`
boundary: if a chunk ends in `\r` it must stay `pendingcr` for
the next call (gopy's `processNewlines` always consumes the `\r`
via `strings.NewReplacer`, so a `\r\n` split across chunks
decodes wrong). Universal-newline translation in TextIOWrapper
read path is absent (the wrapper hands strings through verbatim).
Line buffering and write-through in the write path are recorded
but not acted on. The `newline=` constructor argument is
accepted but ignored. `_textiowrapper_decoder_setstate`,
`_textiowrapper_encoder_setstate`, `_textiowrapper_writeflush`,
`textiowrapper_change_encoding` are not ported.
`textiowrapper_tell_impl` / `textiowrapper_seek_impl` do not
encode/decode the CPython cookie format and just delegate the
raw position to the buffer.

**Functions to port (F: `stringio.c`).** Audit pass: gopy already
has a StringIO. The audit must confirm every C function below is
covered by the existing Go implementation, and add any missing
piece in the same PR.

| C function | gopy equivalent | Status |
|------------|----------------|--------|
| `stringio_new`, `stringio_init`, `stringio_dealloc` | constructor + init | done |
| `stringio_read`, `stringio_readline`, `stringio_write`, `stringio_seek`, `stringio_tell`, `stringio_truncate`, `stringio_close`, `stringio_getvalue`, `stringio_iternext`, `stringio_readable`, `stringio_writable`, `stringio_seekable`, `stringio_closed` | corresponding methods | done |
| `_stringio_writebuffer`, `_stringio_seek_internal` | helpers | done |

**Status (F, 2026-05-14, PR #29).** Full port landed.
`string_size` is tracked separately from `len(buf)`, over-seek
zero-pads on write, `__getstate__` / `__setstate__` exchange the
4-tuple `(initial_value, readnl, pos, dict)`, `newlines` reports
the set of terminators observed during reads, and
`line_buffering` is exposed. Newline modes: `newline=None`
translates `\r\n` and `\r` to `\n` on write; `newline=""` does
universal newline detection on read; `newline="\r"` / `"\r\n"`
translates `\n` to that sequence on write. `readlines` honours
its hint and `writelines` iterates any iterable.
`stringio_traverse` / `stringio_clear` (gc slots) stay out of
scope until gopy has a cyclic-gc protocol. The accumulating
writer optimisation is omitted: the buffer is the rune slab from
the start, with no behavioural difference at the public surface.

**Functions to port (G: `bytesio.c`).**

| C function | Exposed as | Status |
|------------|-----------|--------|
| `bytesio_new`, `bytesio_init`, `bytesio_dealloc`, `bytesio_getstate`, `bytesio_setstate` | constructor + pickle hooks | done |
| `bytesio_read`, `bytesio_read1`, `bytesio_readline`, `bytesio_readlines`, `bytesio_readinto`, `bytesio_write`, `bytesio_writelines`, `bytesio_seek`, `bytesio_tell`, `bytesio_truncate`, `bytesio_getvalue`, `bytesio_getbuffer`, `bytesio_close`, `bytesio_closed`, `bytesio_iternext` | `BytesIO.<method>` | done |

**Status (G, 2026-05-14, PR #28).** Full port landed.
`string_size` is tracked separately from the underlying slab,
`exports` guards resize while a memoryview is live,
`write_bytes_lock_held` zero-pads over-seek gaps, `seek` clamps
negative results for whence=1/2, `writelines` iterates any
iterable, and `getbuffer` / `__getstate__` / `__setstate__` /
`__sizeof__` / `readinto` are all wired up. `bytesio_traverse` /
`bytesio_clear` (gc slots) remain out of scope: gopy has no
cyclic-gc protocol yet.

**Functions to port (H: `io.py`).** Byte-equal vendor that does
`from _io import *` plus the conventional re-binds. Gate is the
import of every name in `io.__all__`.

**Gate.** `gopy -c "import io; b=io.BytesIO(); b.write(b'hello'); print(b.getvalue()); t=io.TextIOWrapper(io.BytesIO(b'caf\xc3\xa9'), encoding='utf-8'); print(t.read()); f=open('/tmp/iogate.txt','w'); f.write('ok'); f.close(); print(open('/tmp/iogate.txt').read())"` prints `b'hello'`, `café`, `ok`.

**Deferred.** Async I/O (`asyncio`-driven `aiofiles`) is out of
scope and tracked separately. Memory-mapped file integration
(`mmap`) is its own port.

### argparse (#515)

**Overview.** `argparse` is what `unittest.__main__` drives: the
test runner's command-line parser is one of the deepest argparse
users in the stdlib (subparsers, mutually exclusive groups,
typed arguments, custom actions). Vendor byte-equal. The file is
self-contained Python; the only runtime support it needs from
gopy is `gettext`-style identity (`_` returning its argument when
no translation is installed) and `os.path` for help-formatting.

**Files in scope.**

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Lib/argparse.py` | ~2,700 | `stdlib/argparse.py` | done |

**Functions / classes to port (A).** All public; vendor byte-equal.

| Class | Span | Status |
|-------|------|--------|
| `HelpFormatter` and its subclasses `RawDescriptionHelpFormatter`, `RawTextHelpFormatter`, `ArgumentDefaultsHelpFormatter`, `MetavarTypeHelpFormatter` | `Lib/argparse.py:200-690` | done |
| `ArgumentError`, `ArgumentTypeError`, `_AttributeHolder`, `Namespace` | `Lib/argparse.py:730-870` | done |
| `Action` hierarchy (`Action`, `BooleanOptionalAction`, `_StoreAction`, `_StoreConstAction`, `_StoreTrueAction`, `_StoreFalseAction`, `_AppendAction`, `_AppendConstAction`, `_CountAction`, `_HelpAction`, `_VersionAction`, `_SubParsersAction`, `_ExtendAction`) | `Lib/argparse.py:880-1380` | done |
| `FileType` | `Lib/argparse.py:1390-1430` | done |
| `_ActionsContainer`, `_ArgumentGroup`, `_MutuallyExclusiveGroup`, `_SubParsersAction.add_parser` | `Lib/argparse.py:1450-1860` | done |
| `ArgumentParser` (init, `parse_args`, `parse_known_args`, `_parse_known_args`, `_get_optional_kwargs`, `_get_positional_kwargs`, `format_help`, `format_usage`, `exit`, `error`, the env / file-prefix reader) | `Lib/argparse.py:1870-2710` | done |

**Gate.** `TestImportArgparse` passes. Vendored `stdlib/argparse.py` loads via PathFinder. VM fix: `tuple.__mul__` (sq_concat + sq_repeat) was missing, causing `(x,)*n` to TypeError in `_metavar_formatter`.

**Deferred.** Translations (`_(...)` calls) keep the identity
fallback gopy already uses for `gettext`.

### signal / _signal (#516)

**Overview.** `signal` is small in surface but lives at the
runtime boundary: every signal arrives through OS facilities and
the C extension owns the handler dispatch. `unittest` uses
`signal.SIGINT` and `signal.SIGALRM` to time out hung tests; the
gopy port must wire the same kqueue / signalfd paths Go already
provides through `os/signal`. Pure-Python `Lib/signal.py` is a
thin re-export.

**Files in scope.**

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Modules/signalmodule.c` | ~2,000 | `module/signal/module.go` | done |
| B | `Lib/signal.py` | ~120 | `stdlib/signal.py` | done |

**Functions to port (A).**

| C function | Exposed as | Status |
|------------|-----------|--------|
| `signal_signal_impl` | `_signal.signal(signum, handler)` | done |
| `signal_getsignal_impl` | `_signal.getsignal` | done |
| `signal_raise_signal_impl` | `_signal.raise_signal` | done |
| `signal_strsignal_impl` | `_signal.strsignal` | done |
| `signal_default_int_handler` | `_signal.default_int_handler` | done |
| `signal_alarm_impl`, `signal_pause_impl`, `signal_setitimer_impl`, `signal_getitimer_impl` | itimer family (POSIX) | done |
| `signal_pthread_kill_impl`, `signal_pthread_sigmask_impl`, `signal_sigpending_impl`, `signal_sigwait_impl`, `signal_valid_signals_impl` | POSIX threading hooks | done |
| `signal_sigwaitinfo_impl`, `signal_sigtimedwait_impl` | Linux-only RT-signal wait | deferred (Linux-specific; darwin port skips; add when Linux CI lands) |
| `signal_set_wakeup_fd_impl`, `signal_siginterrupt_impl` | wakeup-fd machinery | done |
| Constant install (`SIGINT`, `SIGTERM`, `SIGKILL`, `SIG_DFL`, `SIG_IGN`, `ITIMER_REAL`, etc.) | module attrs | done |

**Functions to port (B).** Byte-equal vendor `Lib/signal.py`:
`Signals` IntEnum (and `Handlers`, `Sigmasks`), the
`default_int_handler` re-bind, `__all__` re-export, the
`getsignal` Enum decoration.

**Gate.** `gopy -c "import signal; print(int(signal.SIGINT)); print(signal.strsignal(signal.SIGTERM))"` prints `2` (Linux/Darwin) and the platform-conventional `"Terminated"`.

**Deferred.** Real handler invocation from arbitrary signal
delivery is wired through Go's `os/signal.Notify`; CPython's
direct-from-handler `PyErr_CheckSignals` path is approximated
because Go does not let arbitrary code run inside a signal
handler. Pin behaviour to "handler runs on next bytecode
boundary" matching CPython's pending-call shape.

### os + posixpath + ntpath (#518)

**Overview.** `os` is the largest pure-Python module in the
stdlib once you count its companion `os.path`. The C side
(`Modules/posixmodule.c`) supplies the syscall wrappers; the
pure-Python `Lib/os.py` decides which platform module to import
and re-exports. The four path modules (`posixpath.py`,
`ntpath.py`, `genericpath.py`) split the path-manipulation code
by platform plus a shared base. Vendor byte-equal for the four
Python files; port the syscall wrappers `unittest` and
`argparse` use (open, stat, listdir, environ, getcwd, sep
constants, errno bridge).

**Files in scope.**

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Modules/posixmodule.c` (slice) | ~15,000 | `module/os/module.go` | done |
| B | `Lib/os.py` | ~1,100 | `stdlib/os.py` | done |
| C | `Lib/posixpath.py` | ~600 | `stdlib/posixpath.py` | done (commonpath blocked on listcomp cell-binding bug, tracked under VM audit) |
| D | `Lib/ntpath.py` | ~900 | `stdlib/ntpath.py` | done (unblocked by spec 1705 phases 1 + 5: `\x` codepoint emission and set-union grow-before-place; `import ntpath` is green) |
| E | `Lib/genericpath.py` | ~170 | `stdlib/genericpath.py` | done |

**Functions to port (A).** Minimum slice for unittest enablement.

| C function | Exposed as | Status |
|------------|-----------|--------|
| `os_getcwd_impl` | `posix.getcwd` | done |
| `os_getcwdb_impl`, `os_chdir_impl` | `posix.getcwdb`, `posix.chdir` | done |
| `os_listdir_impl`, `os_scandir_impl`, `DirEntry` type | directory iteration | done |
| `os_stat_impl`, the `stat_result` struct | metadata | done |
| `os_lstat_impl`, `os_fstat_impl` | lstat / fstat | done |
| `os_open_impl` | `posix.open` (fd-level) | done |
| `os_close_impl`, `os_read_impl`, `os_write_impl`, `os_lseek_impl`, `os_dup_impl`, `os_pipe_impl` | low-level fd ops | done |
| `os_unlink_impl`, `os_remove_impl`, `os_rename_impl`, `os_mkdir_impl`, `os_rmdir_impl`, `os_makedirs` helper | mutation | done |
| `os_replace_impl` | atomic rename | done |
| `os_getenv_impl` + environ dict population | `posix.environ` | done |
| `os_getpid_impl`, `os_getuid_impl` | process identity | done |
| `os_getppid_impl`, `os_kill_impl`, `os_waitpid_impl` | process ops | done |
| `os_fspath_impl`, `path_converter` | `posix.fspath` | done |
| `os_access_impl` | `posix.access` | done |
| `os_get_terminal_size_impl` | `posix.get_terminal_size` | done |
| Constant install (O_RDONLY, O_WRONLY, O_RDWR, O_CREAT, O_APPEND, O_TRUNC, O_EXCL, F_OK, R_OK, W_OK, X_OK, sep, pathsep, linesep, devnull) | module attrs | done |

**Functions to port (B-E).** Byte-equal vendors. The os.py
top-level chooses between `posixpath` and `ntpath` based on
`sys.platform`. The four files together define `walk`, `fwalk`,
`makedirs`, `removedirs`, `renames`, `chmod`-equivalent helpers
that ride on top of the C surface.

**Gate.** `gopy -c "import os, os.path; print(os.getcwd()); print(os.listdir('.')[:1]); print(os.path.join('a','b','c.py')); print(os.path.split('/tmp/x.py'))"` prints a real cwd, a non-empty listing, `a/b/c.py` (or `a\b\c.py` on nt), and `('/tmp', 'x.py')`.

**Deferred.** File-descriptor inheritance flags
(`os.set_inheritable`), `os.fork` / `os.exec*` family, scandir's
extended attribute access on non-POSIX systems. Each stays
pending and lands in a follow-up row when a consumer needs it.

### VM / compile audit (#521)

**Overview.** This is the audit sweep that closes 1702. With the
six subsystems above landed, the unittest run will reach the VM
in shapes the rest of the spec did not exercise. The audit walks
`Python/ceval.c` and `Python/bytecodes.c` opcode by opcode,
comparing CPython's implementation against `vm/`, and walks
`Python/compile.c` function by function comparing against
`compile/`. Each missing opcode or compiler step gets a new row
in the table below and lands in the same PR.

**Files in scope.**

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Python/ceval.c` (remaining opcode handlers) | varies | `vm/` (existing) | pending |
| B | `Python/bytecodes.c` (remaining opcode bodies) | varies | `vm/` (existing) | pending |
| C | `Python/compile.c` (remaining codegen) | ~7,000 | `compile/` (existing) | pending |

**Audit method.** For each file, walk the upstream definition
list (the `TARGET(...)` macros in `bytecodes.c`, the
`compiler_visit_*` functions in `compile.c`), confirm gopy has a
matching entry, and add it if not. Every added function carries a
`// CPython:` citation pointing at its upstream line; every gate
is the smallest Python snippet that exercises the new behaviour.

**Gate.** The audit is done when `gopy -m unittest discover -s
test/regrtest -p 'test_*.py'` runs every vendored unittest test
without raising a missing-opcode or compiler-internal error.
Intermediate gates are recorded as new rows here as they appear.

**Deferred.** Adaptive specializer additions discovered during
the audit go into a separate follow-up under spec 1693, not 1702,
because they are tier-2 optimizations rather than correctness
gaps.

## Workflow per port

Every per-file row above lands the same way. The cadence matches
1704 file A / B / C / D:

1. Pick the row. Set its task to in-progress.
2. Read the CPython source end-to-end so the function list is in
   your head before you write Go.
3. Port every function in the row to its gopy target with
   `// CPython:` citations. No identity shims.
4. Write the row's Gate test as a Go test (or a `gopy -c '...'`
   integration check) and confirm it passes.
5. Flip the row's Status to done in the per-subsystem table
   **and** in the master Status table.
6. Run `go test ./...` and `golangci-lint run ./...`. Both must
   be green before pushing.
7. Push the commit and leave a human-style note on PR #27
   summarising what changed, what the gate now proves, and what
   was deferred (if anything).
8. Mark the task completed.

If a row reveals a downstream dependency mid-port, add a new row
above it, port the dependency first, and continue. Never leave a
half-finished row.


Status legend:

| Symbol | Meaning |
|--------|---------|
| done | full port landed; every public name real and cited |
| partial | vendored or shim in place but the real load is blocked; see paragraph for the blocker |
| in-flight | a parallel agent is porting it right now |
| pending | not started |

## Status table

| Subsystem | Task | Status | CPython source | gopy destination | Wave | Notes |
|-----------|------|--------|----------------|------------------|------|-------|
| errno | #499 | done | `Modules/errnomodule.c` | `module/errno/` | 1 | C-extension; inittab is the final mechanism |
| _colorize | #520 | partial | `Lib/_colorize.py` | `stdlib/_colorize.py` + `module/colorize/` shim | 1 | Vendored; inittab shim still wins; blocked on `dataclasses` |
| fnmatch | #519 | done | `Lib/fnmatch.py` | `module/fnmatch/module.go` | 1 | Option B (Go port); vendor blocked on `itertools`, `posixpath`, `os.path.normcase`, `re` gaps |
| functools | #498 | partial | `Lib/functools.py` + `Modules/_functoolsmodule.c` | `stdlib/functools.py` + `module/functools/` + `module/_functools/` | 1 | `cmp_to_key` gate cleared. Vendor `.py` shadowed by inittab until `abc`, `operator`, `reprlib`, `_thread` land. `total_ordering` and `singledispatch` are partial. |
| types | #509 | partial | `Lib/types.py` + `Modules/_typesmodule.c` | `stdlib/types.py` + `module/types/` (registered as `_types`) | 1 | Vendored + `_types` builtin shipped; missing `ClassMethodDescriptorType`, `MethodWrapperType`, `WrapperDescriptorType`, `GenericAlias`, `UnionType` (no runtime equivalents yet) |
| collections | #497 | done | `Lib/collections/__init__.py` + `Modules/_collectionsmodule.c` | `stdlib/collections/` + `module/_collections/` | 2 | `_collections` fully ported (deque, defaultdict, _tuplegetter, _count_elements); `collections/__init__.py` vendored byte-equal. Gate: `TestCollectionsImportResolvesNames` green. |
| contextlib | #508 | done | `Lib/contextlib.py` | `stdlib/contextlib.py` | 2 | Vendored byte-equal; loads via PathFinder; gate cleared in PR #21 |
| abc | #533 | done | `Lib/abc.py` + `Modules/_abc.c` | `stdlib/abc.py` + `module/_abc/` | 2 | `_abc` fully ported (702 lines, all six step check + invalidation counter). Gate: `TestAbcImportResolvesNames` and `TestAbcAbstractMethodEnforced` green. |
| collections.abc | #531 | done | `Lib/_collections_abc.py` | `stdlib/_collections_abc.py` + `stdlib/collections/abc.py` | 2 | Vendored byte-equal (1167 lines). Loads via PathFinder. Unblocks `from collections.abc import Callable, Iterator, Mapping`. |
| operator | #532 | done | `Lib/operator.py` + `Modules/_operator.c` | `stdlib/operator.py` + `module/_operator/` | 2 | `_operator` fully ported (1095 lines): binary/unary arithmetic, rich comparisons, itemgetter, attrgetter, methodcaller, length_hint. `stdlib/operator.py` vendored. |
| warnings | #513 | done | `Lib/warnings.py` + `Python/_warnings.c` | `stdlib/warnings.py` + `module/_warnings/` | 2 | `_warnings` fully ported (1089 lines): filter registry, PyErr_WarnEx/Explicit/Format family, lock plumbing. `stdlib/warnings.py` vendored. |
| pprint | #511 | partial | `Lib/pprint.py` | `module/pprint/module.go` | 2 | Minimal stub (67 lines) forwarding `pformat` to `repr`; enough for `unittest.case` assertion repr. Full PrettyPrinter class not ported. |
| traceback | #496 | partial | `Lib/traceback.py` | `stdlib/traceback.py` | 3 | Vendored byte-equal; `format_exc()` works end-to-end (TestTracebackFormatExc green). Exception message + class printed. Full stack frames missing: `exc.TB` (__traceback__ chain) not yet populated by the VM, so `StackSummary.extract` sees an empty tb and emits only the "ValueError: sentinel" line. |
| dataclasses | #522 | partial | `Lib/dataclasses.py` | `module/dataclasses/` (Go port, option B) | 3 | `_colorize` shim still wins: vendor blocked by missing `collections.abc`, subscriptable `Mapping[str, str]`, zero-arg `super()`. `make_dataclass`, `__hash__` gen, `order=True`, `slots=True`, recursive `asdict`/`astuple`, `InitVar` integration all deferred. |
| GenericAlias + UnionType | #523 | done | `Objects/genericaliasobject.c` + `Objects/unionobject.c` | `objects/generic_alias.go` + `objects/union_type.go` + `objects/class_getitem.go` | 3 | Clears `unittest.case` `types.GenericAlias` gate. `TypeVar` / `ParamSpec` substitution deferred (needs `typing` C accelerator). |
| time | #500 | partial | `Modules/timemodule.c` | `module/time/` | I | Minimal stub (74 lines): `time`, `monotonic`, `perf_counter`, `sleep` wired to Go. Full timemodule.c port (gmtime, strftime, struct_time, timezone) pending. |
| re / _sre | #510 | done | `Lib/re/` + `Modules/_sre/` | `stdlib/re/` + `module/_sre/` | I | Full CPython-faithful bytecode interpreter; vendored Python layer drives it. Final gate pinned in `stdlibinit/re_match_smoke_test.go`. See spec 1703. |
| enum | #544 | done | `Lib/enum.py` | `stdlib/enum.py` | I | Vendored byte-equal; PEP 487 hooks (`__init_subclass__`, `__set_name__`) and the `@enum.global_enum` decorator land via the mappingproxy methodlist + `dict.update(keys() fast path)` fix. Pinned by `stdlibinit/enum_import_test.go` and `stdlibinit/re_match_smoke_test.go`. |
| difflib | #512 | done | `Lib/difflib.py` | `stdlib/difflib.py` | I | Vendored byte-equal (2064 lines). Loads via PathFinder. |
| io / _io | #514 | partial | `Lib/io.py` + `Modules/_io/` | `stdlib/io.py` + `module/_io/` | I | All seven C files have a Go file in place. `_iomodule.c` / `iobase.c` / `bytesio.c` / `stringio.c` are full ports; the other three (fileio, bufferedio, textio) were flipped to **done** on 2026-05-13 but a 2026-05-14 re-audit found 31–55% line coverage with missing methods (FileIO.readinto), missing helpers (`_bufferedreader_raw_read`, `_textiowrapper_writeflush`), and an incorrect buffer model in `Buffered` (separate read/write slabs instead of CPython's unified slab with raw_pos/write_pos pointers). See the per-file "Missing" notes above. `stdlib/io.py` is vendored byte-equal; `TestImportIO` green. |
| argparse | #515 | done | `Lib/argparse.py` | `stdlib/argparse.py` (via PathFinder) | I | Removed inittab shim; PathFinder serves Lib/argparse.py. VM fix: `tuple.__mul__` (sq_concat + sq_repeat) was missing, causing `(x,)*n` to TypeError in `_metavar_formatter`. Gate: `add_argument('--name'); add_argument('-v', action='count'); parse_args(['--name','x','-vv'])` → `x\n2`. Pinned in `stdlibinit/argparse_import_test.go`. |
| signal / _signal | #516 | done | `Modules/signalmodule.c` + `Lib/signal.py` | `module/_signal/` + `stdlib/signal.py` | I | Full port of signalmodule.c (16 functions, raw darwin syscalls); `stdlib/signal.py` vendored byte-equal. Gate: `signal.Signals.SIGINT.value==2`, `signal.Handlers.SIG_DFL.value==0`. |
| weakref / _weakref | #517 | done | `Lib/weakref.py` + `Modules/_weakref.c` | `stdlib/weakref.py` (via PathFinder) + `module/_weakref/` | I | Removed inittab shim so PathFinder serves Lib/weakref.py. Fixed `property.getter/setter/deleter` and `WeakrefType.TpNew`. Gate: `r() is obj → True`, `getweakrefcount → 1`. |
| os + posixpath + ntpath | #518 | done | `Lib/os.py`, `Lib/posixpath.py`, `Lib/ntpath.py`, `Modules/posixmodule.c` slice | `stdlib/` + `module/os/` | I | All posixmodule.c slice functions done: getcwd, getcwdb, chdir, listdir, scandir, stat, lstat, fstat, open, close, read, write, lseek, dup, pipe, unlink/remove/rename/mkdir/rmdir/makedirs/replace, getenv/environ, getpid/getuid/getppid, kill, waitpid, fspath, access, get_terminal_size, O_*/F_*/SEEK_* constants. os.py, posixpath.py, ntpath.py, genericpath.py vendored byte-equal. |
| VM / compile audit | #521 | pending | `Python/ceval.c`, `Python/bytecodes.c`, `Python/compile.c` | (in-tree) | A | Sweep after built-ins land; record findings as additional rows here |

Wave column: `1` = first parallel batch, `2` = second batch
(queued after wave 1 merges), `3` = depends on earlier waves,
`I` = independent infrastructure (any order),
`A` = audit pass.

## Detail format

Each subsystem paragraph below follows the same structure so the
table and the prose stay in sync:

- **Surface.** What public names are now real.
- **Location.** Files added or modified, plus the inittab /
  PathFinder mechanism.
- **Deferred.** Anything intentionally left out, with the reason
  and the follow-up task.
- **Gate.** What this port unblocks (or, for partial entries, what
  is still blocking it).

## errno

**Surface.** Every per-platform `Exxx` constant CPython exposes is
present, plus the `errorcode` reverse-lookup dict. Linux aliases
(`EDEADLOCK` / `EDEADLK`, etc.) resolve to the canonical name
CPython picks because `addErrcode` writes only the first-seen
`code -> name` pair.

**Location.** `module/errno/` is the full port of CPython 3.14
`Modules/errnomodule.c`. `module.go` registers through
`imp.AppendInittab("errno", buildModule)` in `init()`. Per-platform
constants live in three build-tag files: `entries_darwin.go` (103
constants), `entries_linux.go` (132 constants), `entries_other.go`
(empty fallback under `//go:build !darwin && !linux`). Every Go
helper carries a `// CPython: Modules/errnomodule.c:NN` citation.
The inittab blank import was added to `stdlibinit/registry.go`
with the `config.c.in:46` citation.

**Deferred.** None. This is a built-in C-extension equivalent so
there is no Python-level vendor file and nothing for PathFinder to
load: inittab is the correct and final mechanism.

**Gate.** Acceptance run
`gopy -c 'import errno; print(errno.ENOENT, errno.errorcode[errno.ENOENT])'`
prints `2 ENOENT`; `go test ./...` is green; cross-compiles to
darwin, linux, and windows are all clean.

## _colorize

**Surface.** `stdlib/_colorize.py` is a byte-equal vendor of
CPython 3.14 `Lib/_colorize.py` (sha256
`15db41bec8e25fd1ba81cf234df2cc7446d0fbc10285fda73186176365efd730`),
recorded in `stdlib/MANIFEST.txt`. **The vendored file does not
load yet.** Runtime behaviour today is still the
`module/colorize/module.go` shim: `can_colorize` hard-wired to
`False`, `decolor` as `identity`, `set_theme` a no-op, and
`Theme.__getattr__` returning `""` for every attribute so callers
silently see empty strings instead of `AttributeError`. This is
exactly the identity-decorator shape spec 1701 forbids.

**Location.** Vendor file at `stdlib/_colorize.py`; shim still
registered through `module/colorize/module.go` via
`imp.AppendInittab`. Import order in `imp/import.go` is
frozen → inittab → PathFinder, so `import _colorize` short-circuits
on the shim before PathFinder ever reaches the `.py`.

**Deferred.** Two prerequisites are missing for the vendored file
to win: (1) a real `dataclasses` port: the vendored `_colorize.py`
opens with `from dataclasses import dataclass, field, Field` and
applies `@dataclass(frozen=True)` / `@dataclass(frozen=True, kw_only=True)`
to five classes, so the file cannot import until `dataclasses`
lands; (2) the inittab entry in `module/colorize/module.go` must
be removed (or guarded) so PathFinder takes the import. Follow-up:
queue a `dataclasses` port task (wave 3 in the table above), then
delete `module/colorize/`.

**Gate.** Blocked on `dataclasses`. Once dataclasses lands and the
shim is retired, the gate is whatever the next consumer needs
from `_colorize` (currently `traceback`, which imports it for
coloured output).

## fnmatch

**Surface.** Full public surface present and cited:
`fnmatch`, `fnmatchcase`, `filter`, `filterfalse`, `translate`.
`filterfalse` (missing from the prior shim but in CPython's
`__all__`) is now shipped.

**Location.** `module/fnmatch/module.go` rewritten as a Go port
(option B), with a companion `module/fnmatch/module_test.go`.
Every function carries a `// CPython: Lib/fnmatch.py:NN` citation.
`translate()` is a line-for-line port of CPython `_translate` +
`_join_translated_parts` and produces **byte-identical** regex
output for the `(?s:...)\z` framing, atomic groups around interior
`STAR fixed` pairings, set-operation escaping, range-collapse, and
the `[!]` → `.` / `[]` → `(?!)` edge cases. Matching uses a
parallel Go-side glob interpreter (`compileGlob`, `matchGlob`,
`parseClass`) that mirrors the same bracket grammar.
`stdlibinit/registry.go` already had the inittab entry; no change
there.

**Deferred.** Option A (vendor `Lib/fnmatch.py` to
`stdlib/fnmatch.py`) was attempted first and abandoned because
four upstream dependencies are missing or insufficient in gopy:
(1) `import itertools` returns `ModuleNotFoundError` (no
`module/itertools/`); (2) `import posixpath` returns
`ModuleNotFoundError` (`module/os/` registers `posixpath` only as
an alias of `os.path`, but `fnmatch.py` does a plain top-level
`import posixpath`); (3) `os.path.normcase` is not exposed
(the os port lists `basename`, `dirname`, etc. but not `normcase`);
(4) the current `re` port wraps Go's RE2 and rejects two regex
constructs CPython's `translate()` always emits: `\Z`
(`re.error: invalid escape sequence`) and `(?>...)` atomic groups
(`invalid or unsupported Perl syntax`). Spec calls out the re full
port as task #510, out of scope here. When #510 ships, the
Go-side glob matcher can be deleted and `fnmatchcase` can
delegate to `re.compile(translate(pat)).match`, completing the
option-A migration.

**Gate.** Acceptance run all green:
`fnmatch('test_one.py', 'test*.py')` → `True`,
`fnmatchcase('Test.py', 'test*.py')` → `False`,
`translate('*.py')` → `(?s:.*\.py)\z`,
`filter([...], '*.py')` → `['a.py', 'c.py']`;
`go test ./...` clean with no regressions.

## types

**Surface.** Vendored `Lib/types.py` is now the active code path
via `try: from _types import *`. `_types` exposes the canonical
type singletons that `types.py` re-binds (`FunctionType`,
`MethodType`, `ModuleType`, `CodeType`, `MappingProxyType`,
`SimpleNamespace`, etc.). Five upstream names are deliberately
absent because gopy has no runtime equivalent yet:
`ClassMethodDescriptorType`, `MethodWrapperType`,
`WrapperDescriptorType`, `GenericAlias`, `UnionType`. They are
not exposed under `types.X` until the matching runtime ports
land. The `_f.__code__` / `type.__dict__` / `sys.implementation`
fallback path in `types.py` is dead code in gopy (CPython solves
this at upstream the same way), so the architecture matches
CPython rather than diverging.

**Location.** `stdlib/types.py` is a byte-equal vendor of CPython
3.14 `Lib/types.py` (sha256 `fcfdd1c5…`), recorded in
`stdlib/MANIFEST.txt`. `module/types/module.go` is rebuilt to be
the `_types` builtin (mirrors `Modules/_typesmodule.c`),
registered through `imp.AppendInittab("_types", …)` from
`stdlibinit/registry.go`. The runtime support that this port
exposed came along the way:
`objects/mapping_proxy.go` (new port of CPython mappingproxy from
`Objects/descrobject.c:1034`, with read-only enforcement, repr,
iter, contains, hash, richcompare),
`objects/namespace.go` (`TpNew` so `SimpleNamespace(a=1, b=2)`
works), `objects/property.go` (`TpNew` so `@property` decorators
inside `types.py` instantiate), `objects/module.go`
(`module.__dict__` attribute access returns the module's dict,
needed for `from x import *`),
`pythonrun/runstring.go` (`RunSimpleString` now stamps
`__name__ = "__main__"` on `-c` globals),
`vm/eval_import.go` (promotes `ErrModuleNotFound` to a typed
`ModuleNotFoundError` on the thread state so
`except ImportError:` matches),
`vm/eval_simple.go` (`exceptionMatches` consults `IsSubtype` so
`ModuleNotFoundError` is recognised as `ImportError`), and
`vm/eval_unwind.go` (`synthesizeException` maps `TypeError:` /
`ValueError:` / etc. message prefixes to typed `PyExc_*` classes
when bare Go errors unwind).

**Deferred.** Five `types.py` re-exports are missing on purpose
(see Surface). Follow-up tasks needed: port the three descriptor
types as part of a broader descriptor pass; port `GenericAlias`
(`Objects/genericaliasobject.c`) and `UnionType`
(`Objects/unionobject.c`). Separately, the bare-Go-error →
typed-exception promotion in `vm/eval_unwind.go` is currently
prefix-based on the error message; the proper fix is to route
every call site through `errors.SetString`, which is a larger
refactor and is outside the scope of #509.

**Gate.** Acceptance run green: `type(lambda: 0)` →
`<class 'function'>`; `SimpleNamespace(a=1, b=2)` → `1 2`;
`MappingProxyType({'a': 1})['a']` → `1`; `types.FunctionType`
resolves OK. `go test ./...` clean.

## functools

**Surface.** `_functools` ships the full C accelerator surface:
`PartialType` / `partial` with placeholder-aware merge,
`KeyObjectType` / `cmp_to_key` plus `keyobject_richcompare`,
`reduce` with seeded and unseeded paths and the empty-iterable
`TypeError`, `LruCacheWrapperType` with all three CPython wrapper
variants (uncached, infinite, bounded), the doubly-linked recency
list, `cache_info` / `cache_clear` / `__copy__` / `__deepcopy__`,
descriptor binding, and the `Placeholder` singleton. The Go-side
`module/functools/module.go` re-exports `_functools` and adds the
pure-Python wrappers: `wraps`, `update_wrapper`, `lru_cache`,
`cache`, `cached_property`, `total_ordering`, `singledispatch`,
`partialmethod`, plus a Go-side `_CacheInfo` named-tuple
stand-in.

**Location.** `module/_functools/module.go` is the 978-line port
of CPython 3.14 `Modules/_functoolsmodule.c`; every exported name
carries a `// CPython: Modules/_functoolsmodule.c:NN <func>`
citation. `module/functools/module.go` is the Go-native port of
`Lib/functools.py`. `stdlibinit/registry.go` blank-imports
`module/_functools` next to `module/functools`. `stdlib/functools.py`
is a byte-equal vendor of `Lib/functools.py` (1165 lines), recorded
in `stdlib/MANIFEST.txt`, but it is shadowed today because the
inittab entry wins over the path finder.

**Deferred.** Four follow-ups: (1) the vendored `stdlib/functools.py`
cannot load yet because `abc`, `operator`, `reprlib`, `_thread`
are not ported: once those land, delete the `module/functools`
inittab entry so the vendored `.py` takes over. (2) `total_ordering`
currently returns the class unchanged; the CPython behaviour of
filling in missing comparison methods needs a class-mutating
decorator path, which lands when gopy exposes the typeobject
mutation API. (3) `singledispatch` dispatches by exact type only;
the full MRO walk lands when `__mro__` traversal is wired into the
registry. (4) Test coverage relies on `go test ./...` which Go's
glob skips for `_`-prefixed directories, so `module/_functools` is
reached only via `module/functools`'s end-to-end tests.

**Gate.** All five primary acceptance gates green: `cmp_to_key`
returns a `KeyWrapper`; `sorted([3,1,2], key=cmp_to_key(...))` →
`[1, 2, 3]`; `partial(add, 3)(4)` → `7`;
`@lru_cache(maxsize=2)` decorated `f(3)` returns `6` with
`CacheInfo(hits=0, misses=1, maxsize=2, currsize=1)`;
`reduce(add, [1,2,3,4])` → `10`. Sixth gate (`gopy /tmp/test_simple.py`)
now advances past the `cmp_to_key` failure and lands in
`unittest.case`, where it hits the already-documented
`types.GenericAlias` gap from the types port.

## GenericAlias + UnionType

**Surface.** Full runtime representation of `list[int]`, `dict[str, int]`,
`tuple[int, ...]`, and PEP 604 `int | str`. `types.GenericAlias` and
`types.UnionType` are now real types accessible from Python. The
`__class_getitem__` hook is bound on the six canonical generic
builtins (`list`, `dict`, `tuple`, `set`, `frozenset`, `type`), so
subscripting them produces a `GenericAlias`. The `|` operator on
`type` produces a `UnionType`; `__or__` and `__ror__` are wired on
both `GenericAlias` and `UnionType` so chained constructions
(`int | str | bytes`) compose correctly. `union_repr`, `union_hash`,
`union_richcompare` (set-equality on args with order preserved),
`isinstance(x, int | str)` / `issubclass`, `__args__`, `__parameters__`,
`__origin__`, `__mro_entries__`, `__copy__`, `__deepcopy__`,
`__reduce__` all behave per CPython.

**Location.** `objects/generic_alias.go` (~440 lines, full port of
`Objects/genericaliasobject.c`), `objects/union_type.go` (~430 lines,
full port of `Objects/unionobject.c`), `objects/class_getitem.go`
(~65 lines, the `__class_getitem__` and `Number.Or` slot wiring).
Two entries added to `module/types/module.go` exposing the new
types under `_types`. `vm/eval_simple.go` extended with a
`typeSubscript` fallback at the tail of `getItem` so `cls[arg]`
reaches `__class_getitem__` when no direct `mp_subscript` slot
exists. Every Go helper carries a
`// CPython: Objects/<file>.c:NN <func>` citation. New public API:
`objects.GenericAliasType`, `objects.NewGenericAlias(origin, args)`,
`objects.UnionTypeType`, `objects.NewUnionType(args)`. Helpers
`unionTypeOr` and `typingTypeRepr` are package-private and shared
between the two files.

**Deferred.** Three items, all justified: (1) `TypeVar`, `ParamSpec`,
`TypeVarTuple` substitution short-circuits with CPython's "is not
a generic class" `TypeError` when the parameters tuple is empty.
Real substitution requires the `typing` C accelerator types, which
gopy does not yet ship; this matches the CPython behaviour for
`list[int][str]`-style re-parameterization on non-typing builtins.
(2) `__class_getitem__` is bound on the six canonical builtins
only; generator, coroutine, contextvar, etc. are not wired. Each
is a one-line `bindClassGetitem(T)` call when needed. (3) Union
flattening when one arg is itself a `UnionType` from a different
construction path is not special-cased; the common left-associative
`int | str | bytes` is covered by the existing `union_or` path.

**Gate.** All five primary gates green: `types.GenericAlias` resolves
to the class; `list[int]` prints `list[int]`; `int | str` prints
`int | str`; `x: list[int] = [1,2,3]; print(x)` prints `[1, 2, 3]`;
`go test ./...` clean. Sixth gate (`gopy /tmp/test_simple.py`)
advances past the `types.GenericAlias` failure and now fails at
`AttributeError: 'method' object has no attribute '__doc__'` inside
the unittest import chain: a separate bound-method descriptor
gap, outside the generics subsystem.

## dataclasses

**Surface.** Full public surface of `Lib/dataclasses.py` exposed
through a Go-native port: `dataclass`, `field`, `Field`, `fields`,
`asdict`, `astuple`, `replace`, `is_dataclass`, `MISSING`,
`KW_ONLY`, `InitVar`, `FrozenInstanceError`. The `@dataclass`
decorator handles `frozen=True` (and `frozen=True, kw_only=True`,
which is exactly the shape `_colorize` uses on five classes),
generates `__init__` from field declarations with defaults and
`field(default_factory=...)`, sets `Repr` and `Eq` on the resulting
class, and enforces frozen attribute writes through
`FrozenInstanceError`.

**Location.** `module/dataclasses/module.go` (~1161 lines).
`objects/descr.go` was extended with `TypeOwnDescrs` and
`DelTypeDescr` helpers so dataclass field discovery can introspect
a class's own descriptor table. `stdlibinit/registry.go` blank-imports
`module/dataclasses`. Every Go helper carries a
`// CPython: Lib/dataclasses.py:NN <func>` citation.

**Deferred.** Seven items, all reported with upstream line numbers
so follow-ups are precise. (1) `make_dataclass` raises
`NotImplementedError`; requires runtime synthesis of a new `Type`
from a field list (CPython `dataclasses.py:1635`). Not used by
`_colorize` or `unittest.mock`. (2) `__hash__` generation under
`eq=True frozen=True` (CPython `dataclasses.py:936-984`
`_hash_*`) and `unsafe_hash=True` not wired; the `Repr` / `Eq`
slots are set but `Hash` falls through. (3) Order comparisons
under `order=True` (CPython `dataclasses.py:1149` `_cmp_fn`) not
emitted. (4) `slots=True` and `weakref_slot=True` (CPython
`dataclasses.py:1337` `_add_slots`) parsed but the class is not
rebuilt with `__slots__`. (5) `asdict` / `astuple` do a shallow
`dict()` over top-level attrs; the recursive walk for nested
dataclasses (CPython `dataclasses.py:1507` `_asdict_inner`) is
not implemented. `_colorize` and `mock` do not nest. (6)
`dict_factory` / `tuple_factory` keyword arguments parsed but
ignored. (7) `InitVar` is exposed but `_get_field` integration
(CPython `dataclasses.py:825`) is not, so InitVar-typed fields
would be treated as regular fields.

**Gate.** All four primary gates green: `import dataclasses; print(dataclasses.dataclass)`
prints the decorator; `@dataclass class C: x:int=1; C(x=5).x` →
`5`; `@dataclass(frozen=True)` variant prints `5`; `go test ./...`
clean. The fifth gate ("delete `module/colorize/` and let the
vendored `stdlib/_colorize.py` win") **was not done** by design ;
the vendored file fails on its second line
(`from collections.abc import Callable, Iterator, Mapping`)
because `collections.abc` is not exposed, and would further need
subscriptable `Mapping[str, str]` and zero-arg `super()` to
complete its class definitions. The shim therefore stays in place
until those three runtime gaps land. The dataclasses port itself
was verified to handle the same `_colorize`-shape classes by
inlining the definitions and confirming construct, repr, eq, and
frozen-guard all work.

### Runtime gaps surfaced by this port

The dataclasses agent identified six runtime gaps that block a
clean vendor of `Lib/dataclasses.py` and adjacent stdlib modules.
These are separate follow-ups, not part of #522, but listed here
because they will keep coming up as later subsystems land:

1. **`__annotations__` not populated on class bodies.** The
   compiler records annotations in
   `compile/codegen_stmt_misc.go:151` (`visitAnnAssign`) into
   `DeferredAnnotations` but never emits the runtime store.
   `SETUP_ANNOTATIONS` exists as an opcode but is never emitted.
   This is why field discovery in `module/dataclasses` had to walk
   the descriptor table heuristically instead of reading
   `cls.__annotations__`. Fixing this unblocks a future vendor of
   `Lib/dataclasses.py`.
2. **Multi-line `def` inside `exec()` from a function-scope frame
   fails.** Top-level `exec("def f(x):\n  return x", g)` works;
   the same call from inside `def make(): ...` raises
   `parser: generated rule bodies not yet emitted`. This blocks
   the CPython `_FuncBuilder` codegen path that dataclasses,
   functools, and others rely on.
3. **`collections.abc` not exposed.** Required by the vendored
   `_colorize.py`. `module/collections` is a partial shim that
   publishes the dotted `collections` name only; it should either
   register a separate `collections.abc` inittab entry or attach
   an `abc` submodule attribute.
4. **Zero-arg `super()`** raises `RuntimeError: super(): no
   arguments (zero-arg form not yet supported)`. Required by
   `_colorize.ThemeSection.__post_init__`.
5. **`object.__setattr__` not exposed.** The documented workaround
   for writing through a frozen class is not available.
6. **`repr(c)` does not dispatch to user `__repr__`** on plain
   user classes (only when a class sets `cls.Repr` directly). The
   dataclass port works around this by setting `cls.Repr` from
   Go; user classes with a Python-level `__repr__` method still
   print the default `<C object at 0x...>` form.
