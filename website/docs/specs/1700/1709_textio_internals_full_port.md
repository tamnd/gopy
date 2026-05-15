---
format: md
id: 1709_textio_internals_full_port
title: "1709. Modules/_io/textio.c internals full port"
sidebar_label: "1709. textio_internals"
sidebar_position: 1709
slug: /specs/1709-textio_internals
description: "Bring module/io/textiowrapper.go from the current one-shot decode/encode shim up to a 1:1 port of Python/Modules/_io/textio.c: stateful IncrementalDecoder/IncrementalEncoder per encoding, _textiowrapper_read_chunk with decoder snapshotting and b2cratio, and the 8-field tell/seek cookie format."
---


## Rule

Same as 1704 / 1705 / 1708. The deliverable is a Go file whose
function list 1:1 covers `Modules/_io/textio.c`. Once this spec
lands the `decodeBytes` / `encodeString` one-shot helpers in
`module/io/textiowrapper.go` are deleted, and read / write /
tell / seek go through the real CPython incremental-codec
pipeline.

## Why this spec exists

`module/io/textiowrapper.go` (1493 lines) covers the high-level
TextIOWrapper surface: `__init__`, `read`, `readline`, `write`,
`flush`, `close`, `detach`, the universal-newline pipeline, the
member descriptors, the context-manager hooks, the instance dict.
The pieces missing against CPython's 3433-line `textio.c` are all
in the codec / cookie layer:

- `decodeBytes` (`textiowrapper.go:461`) and `encodeString`
  (`:491`) are switch tables over four hardcoded encodings, with
  a fallback to `codecs.go` for everything else. They are one-shot
  whole-buffer transforms. CPython holds a stateful
  `IncrementalDecoder` / `IncrementalEncoder` per wrapper
  (`textio.c:912` `_PyCodecInfo_GetIncrementalDecoder`) and feeds
  the buffer in chunks, calling `decoder.decode(input, final)` so
  that multi-byte sequences split across reads work correctly.
- `read` (`textiowrapper.go:552`) reads the whole pending buffer in
  one shot and decodes it. CPython's `_textiowrapper_read_chunk`
  (`textio.c:1853`) reads `max(chunk_size, b2cratio*size_hint)`
  bytes via `buffer.read1`, feeds them to the decoder, snapshots
  `(dec_flags, bytes_fed)` for tell, and updates `b2cratio` so the
  next chunk size adapts to the encoding's expansion ratio.
- `Tell` (`textiowrapper.go:754`) raises `OSError` when any decoded
  text is buffered. CPython packs five integer fields into a single
  Python int "cookie" (`textio.c:2387 cookie_type`), so a tell mid-
  decode returns a number the matching `Seek` can unpack to
  reconstruct the exact `(file_pos, dec_flags, bytes_fed,
  chars_skipped, need_eof)` state. Without this, anything that
  round-trips position (`for line in f:` + `f.tell()` /
  `f.seek(saved)`) raises in gopy where CPython would return.
- `Reconfigure` is partially wired (`textiowrapper.go:1086`) but
  does not rebuild the codecs when `encoding=` / `errors=` change,
  because there is no codec object to rebuild — just a string.

Sources of truth: `/Users/apple/cpython-314/Modules/_io/textio.c`
and `/Users/apple/cpython-314/Lib/codecs.py` for the abstract
IncrementalDecoder/Encoder interface.

## Files in scope

| # | CPython | Lines | gopy target | Status |
|---|---------|------:|-------------|--------|
| A | `Modules/_io/textio.c` codec hook block: `_textiowrapper_set_decoder`, `_textiowrapper_set_encoder`, `_textiowrapper_fix_encoder_state`, `_textiowrapper_decode` | ~150 | `module/io/textio_codec.go` (new) | pending |
| B | `Modules/_io/textio.c` read pipeline: `textiowrapper_read_chunk`, `textiowrapper_set_decoded_chars`, `textiowrapper_get_decoded_chars`, `_textiowrapper_writeflush` already done | ~300 | `module/io/textio_read.go` (new) — move read / readline off `decodeBytes` | pending |
| C | `Modules/_io/textio.c` cookie: `cookie_type`, `textiowrapper_parse_cookie`, `textiowrapper_build_cookie`, the tell/seek `_impl` bodies | ~280 | `module/io/textio_cookie.go` (new) + rewrite of `Tell` / `Seek` in `textiowrapper.go` | pending |
| D | `Modules/_io/textio.c` reconfigure: `_io_TextIOWrapper_reconfigure_impl`, validation of `encoding=` / `errors=` / `newline=` changes against the live decoder / encoder | ~140 | `module/io/textiowrapper.go` (replace existing skeleton) | pending |

## Phase index

| Phase | File | Block | Status |
|-------|------|-------|--------|
| 1 | A | IncrementalDecoder / IncrementalEncoder layer: a `codecs.lookup`-style dispatcher that hands back a stateful object with `decode(input, final)`, `getstate() -> (bytes, int)`, `setstate(state)`, `reset()`. Built-in encodings (utf-8, utf-16, utf-16-le/be, utf-32, utf-32-le/be, ascii, latin-1, cp1252, etc.) get a Go-side fast-path implementation; everything else routes through `codecs.lookup(encoding).incrementaldecoder(errors)` on the Python side. Wire `setDecoder` / `setEncoder` into `__init__` and `reconfigure`. | pending |
| 2 | B | Port `textiowrapper_read_chunk`: call `buffer.read1(size)`, feed to the decoder, snapshot `(dec_flags, bytes_fed)`, update `b2cratio`, drive `decoded_chars` / `decoded_chars_used`. Rewrite `read` and `readline` to go through it. Delete `decodeBytes` and the one-shot path. | pending |
| 3 | C | Port the cookie: 5 fields (`start_pos`, `dec_flags`, `bytes_to_feed`, `chars_to_skip`, `need_eof`) packed via `_PyLong_FromByteArray` / `_PyLong_AsByteArray` in little-endian order. Rewrite `Tell` to build the cookie from the decoder snapshot + chars already consumed; rewrite `Seek` to parse the cookie, reposition the buffer, feed bytes back through a fresh decoder, then skip chars. | pending |
| 4 | D | Port `reconfigure` fully: validate that no read-ahead / write-ahead is pending, rebuild the codecs when `encoding` / `errors` changes, re-run `setNewline` when `newline` changes, and re-wire `line_buffering` / `write_through`. | pending |
| Gate | | After all four phases land: `t = open(path, 'r', encoding='utf-16'); t.read(2); pos = t.tell(); rest = t.read(); t.seek(pos); t.read() == rest` — i.e. tell+seek round-trips mid-stream against a multi-byte encoding. `for line in open(...): pass` with mixed CR/LF still green. `reconfigure(encoding='latin-1', newline=None)` after a `read` raises the expected `ValueError` (CPython rejects encoding swap when read-ahead is pending). | pending |

## Phase 1 — IncrementalDecoder / IncrementalEncoder

`module/io/textio_codec.go` (new) exports:

```go
type IncrementalDecoder interface {
    Decode(input []byte, final bool) (string, error)
    GetState() (buffer []byte, flags int64)
    SetState(buffer []byte, flags int64) error
    Reset()
}

type IncrementalEncoder interface {
    Encode(input string, final bool) ([]byte, error)
    GetState() int64
    SetState(state int64) error
    Reset()
}

func getIncrementalDecoder(encoding, errors string) (IncrementalDecoder, error)
func getIncrementalEncoder(encoding, errors string) (IncrementalEncoder, error)
```

`textio.c:912` `_textiowrapper_set_decoder` and `:968`
`_textiowrapper_set_encoder` call these and stash the result on
`textio.decoder` / `textio.encoder`. After phase 1, the existing
`decodeBytes` / `encodeString` switch becomes the bodies of the
built-in Go decoders / encoders for utf-8, ascii, and latin-1.

`utf-16` and `utf-32` need real state — `dec_flags` encodes "I am
mid-BOM" / "I emitted the BOM" so a chunk boundary mid-BOM works.
This is the case the current code silently mis-handles.

### Gate

`stdlibinit/textio_codec_test.go` (new): open a file written as
`"héllo".encode("utf-16")`, read it one byte at a time through a
chain of `decode(b, False)` calls, assert the concatenation equals
`"héllo"`.

## Phase 2 — `_textiowrapper_read_chunk`

Per CPython `textio.c:1853`:

1. Snapshot the decoder state before the read so a future `tell`
   knows how to back up.
2. Read `max(chunk_size, b2cratio * size_hint)` bytes from the
   buffer via `read1` (read once, do not loop) so we don't block
   waiting for more than one chunk on a slow stream.
3. Feed the bytes through `decoder.decode(input, eof)` where
   `eof = (len(input) == 0)`.
4. Update `b2cratio = bytes_read / chars_decoded` (defaults to
   1.0 on the first chunk, smoothed thereafter).
5. Stash `decoded_chars` and reset `decoded_chars_used` so
   `_textiowrapper_get_decoded_chars(n)` can hand out characters
   from it.

Rewrite `read(size)` and `readline(size)` to consume
`_textiowrapper_get_decoded_chars` and call `_read_chunk` on
exhaustion, dropping the current `bufRead(buf, 1)` byte-at-a-time
fallback in `readline`.

### Gate

Round-trip read fixture: write a fixed corpus to a `BytesIO` wrapped
in a `TextIOWrapper(encoding='utf-8')`, read the whole thing back
character by character. Match CPython byte-for-byte.

## Phase 3 — Cookie pack / unpack

`textio.c:2387 cookie_type`:

```c
typedef struct {
    Py_off_t start_pos;     /* file pos at start of decoded chunk */
    int      dec_flags;     /* decoder flags at start_pos */
    int      bytes_to_feed; /* bytes fed into the decoder since start_pos */
    int      chars_to_skip; /* chars consumed from the decoded output */
    char     need_eof;      /* did we need to set the EOF flag to decode? */
} cookie_type;
```

Packed little-endian into a Python int via `_PyLong_FromByteArray`.

Gopy port: `module/io/textio_cookie.go` (new):

```go
type cookie struct {
    StartPos     int64
    DecFlags     int32
    BytesToFeed  int32
    CharsToSkip  int32
    NeedEOF      bool
}

func packCookie(c cookie) *big.Int
func parseCookie(v *big.Int) (cookie, error)
```

Rewrite `Tell` to build it from the live snapshot and
`Seek(pos, 0)` to parse + reposition + replay through a fresh
decoder.

### Gate

`stdlibinit/textio_tell_seek_test.go` (new): open a utf-16 file,
read past the BOM, tell, read more, seek back, re-read, assert
equality.

## Phase 4 — `reconfigure`

`textio.c:1370` `_io_TextIOWrapper_reconfigure_impl`. The current
gopy skeleton accepts the call but never rebuilds the codec
objects (because there are none). Phase 4 wires it for real:

1. Reject changes to `encoding=` / `errors=` when there is
   buffered decoded text (CPython raises `ValueError: I/O
   operation on already-decoded stream` equivalent).
2. Tear down decoder / encoder, rebuild via the phase-1 helpers.
3. Re-run `setNewline` and recompute `readuniversal` /
   `readtranslate` / `writenl`.
4. Update `line_buffering` / `write_through` if the call set them.

### Gate

Open in `'r'`, read partial content, call
`f.reconfigure(encoding='latin-1')` → expect `ValueError`. Open
fresh, no read, call same — expect success and the next `read`
returns latin-1 decoded bytes.

## Final gate

After all four phases land:

1. `decodeBytes` and `encodeString` are deleted from
   `module/io/textiowrapper.go`.
2. `TextIOWrapper.encoding` is no longer the single source of
   truth — the decoder / encoder objects are.
3. `module/io/codecs.go` either gets folded into the new
   `textio_codec.go` layer or stays as the low-level codec table
   the new layer reads from.
4. `go test ./...` green, including the three new phase gates.
5. Round-trip on a utf-16 file with tell/seek mid-stream returns
   the same bytes CPython would.

## Checklist

- [ ] Phase 1: IncrementalDecoder / IncrementalEncoder layer in `module/io/textio_codec.go`; utf-16 BOM split across chunks works
- [ ] Phase 2: `_textiowrapper_read_chunk` ported; `read` / `readline` go through it; `decodeBytes` deleted
- [ ] Phase 3: cookie pack/unpack in `module/io/textio_cookie.go`; `Tell` builds a real cookie; `Seek(cookie, 0)` round-trips
- [ ] Phase 4: `reconfigure` rebuilds decoder / encoder; rejects mid-stream encoding swap
- [ ] Final gate: `decodeBytes` / `encodeString` deleted, utf-16 tell/seek round-trip green, all tests pass
