// Read-side internals for TextIOWrapper: codec lazy build, the
// _textiowrapper_read_chunk port, and helpers that drain decoded_chars.
//
// CPython: Modules/_io/textio.c

package io

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// ensureCodecs builds the IncrementalDecoder / IncrementalEncoder
// lazily. CPython does this in textiowrapper_init via
// _PyCodecInfo_GetIncrementalDecoder / Encoder; we defer until the
// first read or write so callers that only touch the wrapper's metadata
// (mode, encoding) do not pay the lookup cost.
//
// CPython: Modules/_io/textio.c:1240 _textiowrapper_set_decoder /
//
//	:1356 _textiowrapper_set_encoder
func (t *TextIOWrapper) ensureCodecs() error {
	if t.decoder == nil {
		d, err := getIncrementalDecoder(t.encoding, t.errors)
		if err != nil {
			return err
		}
		t.decoder = d
	}
	if t.encoder == nil {
		e, err := getIncrementalEncoder(t.encoding, t.errors)
		if err != nil {
			return err
		}
		t.encoder = e
	}
	return nil
}

// resetCodecs drops the lazy codecs so the next read or write rebuilds
// them. Used by seek/reconfigure when stream identity changes.
//
// CPython: Modules/_io/textio.c:1483 textiowrapper_seek_impl (codec rebuild)
func (t *TextIOWrapper) resetCodecs() {
	t.decoder = nil
	t.encoder = nil
}

// readChunk drives one read1/decode cycle: pull a chunk from the
// underlying buffer, push it through the incremental decoder, then run
// the result through the newline decoder. Returns true while data may
// remain in the source.
//
// CPython: Modules/_io/textio.c:1853 _textiowrapper_read_chunk
func (t *TextIOWrapper) readChunk(sizeHint int) (bool, error) {
	if err := t.ensureCodecs(); err != nil {
		return false, err
	}

	// CPython sizes the read by chunk_size * max(1, b2cratio); the b2cratio
	// climbs whenever a chunk decodes to fewer chars than bytes (utf-16,
	// utf-32, multi-byte codecs).
	want := t.chunkSize
	if sizeHint > 0 {
		want = sizeHint
	}
	if t.b2cratio > 1.0 {
		scaled := float64(want) * t.b2cratio
		if scaled < float64(want) {
			scaled = float64(want)
		}
		want = int(scaled)
	}

	// Snapshot the decoder state and buffer position before the read so
	// a later tell can build a cookie that round-trips.
	preBuf, preFlags := t.decoder.GetState()
	preCopy := append([]byte(nil), preBuf...)
	startPos, posErr := bufTell(t.buf)
	if posErr != nil {
		startPos = 0
	}
	preNLPendCR := false
	preNLSeenNL := 0
	if t.nlDecoder != nil {
		preNLPendCR = t.nlDecoder.pendingcr
		preNLSeenNL = t.nlDecoder.seennl
	}

	raw, err := readOneChunk(t.buf, want)
	if err != nil {
		return false, err
	}
	eof := len(raw) == 0

	decoded, err := t.decoder.Decode(raw, eof)
	if err != nil {
		return false, err
	}
	if t.readuniversal {
		t.ensureNLDecoder()
		decoded = t.nlDecoder.translateNewlines(decoded, eof)
	}
	// The snapshot only describes the current chunk; if leftover from a
	// prior chunk is still in decodedBuf, a future tell cannot rebuild
	// state from a single replay, so invalidate.
	if len(t.decodedBuf) > 0 {
		t.snapshotValid = false
	} else {
		t.snapshotValid = true
		t.snapshotStartPos = startPos
		t.snapshotDecBuf = preCopy
		t.snapshotDecFlags = preFlags
		t.snapshotBytesFed = len(raw)
		t.snapshotChunkLen = len(decoded)
		t.snapshotNLPendCR = preNLPendCR
		t.snapshotNLSeenNL = preNLSeenNL
	}
	if decoded != "" {
		t.decodedBuf += decoded
	}

	// CPython: blend the new ratio in at 0.625/0.375 weights so transient
	// hot/cold chunks do not whipsaw the size.
	if !eof && len(raw) > 0 {
		ratio := float64(len(raw)) / float64(max1(len(decoded)))
		if t.b2cratio == 0 {
			t.b2cratio = ratio
		} else {
			t.b2cratio = 0.625*ratio + 0.375*t.b2cratio
		}
	}

	return !eof, nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// readOneChunk pulls one chunk from the buffer. Prefers read1 (matches
// CPython's BufferedReader read1) so we do not block waiting for the
// next read, falling back to read for buffers that lack read1.
//
// CPython: Modules/_io/textio.c:1853 _textiowrapper_read_chunk (read1 call)
func readOneChunk(buf objects.Object, size int) ([]byte, error) {
	if size < 0 {
		return bufRead(buf, -1)
	}
	if fn, err := objects.GetAttr(buf, objects.NewStr("read1")); err == nil {
		result, callErr := objects.Call(fn, objects.NewTuple([]objects.Object{objects.NewInt(int64(size))}), nil)
		if callErr == nil {
			switch v := result.(type) {
			case *objects.Bytes:
				return v.Bytes(), nil
			case *objects.Unicode:
				return []byte(v.Value()), nil
			}
			if objects.IsNone(result) {
				return nil, nil
			}
			return nil, fmt.Errorf("TypeError: buffer.read1() returned %s, expected bytes", result.Type().Name)
		}
	}
	return bufRead(buf, size)
}

// drainAll pulls chunks until EOF, returning everything in the decoded
// buffer concatenated with the new data. Used by read(-1).
//
// CPython: Modules/_io/textio.c:1988 textiowrapper_read_impl (size<0 path)
func (t *TextIOWrapper) drainAll() (string, error) {
	for {
		more, err := t.readChunk(-1)
		if err != nil {
			return "", err
		}
		if !more {
			break
		}
	}
	out := t.decodedBuf
	t.decodedBuf = ""
	return out, nil
}

// drainN pulls chunks until decodedBuf holds at least n chars or EOF.
//
// CPython: Modules/_io/textio.c:1988 textiowrapper_read_impl (size>=0 path)
func (t *TextIOWrapper) drainN(n int) (string, error) {
	for len(t.decodedBuf) < n {
		more, err := t.readChunk(t.chunkSize)
		if err != nil {
			return "", err
		}
		if !more {
			break
		}
	}
	if len(t.decodedBuf) <= n {
		out := t.decodedBuf
		t.decodedBuf = ""
		return out, nil
	}
	out := t.decodedBuf[:n]
	t.decodedBuf = t.decodedBuf[n:]
	return out, nil
}

// drainLine pulls chunks until a newline appears or EOF.
//
// CPython: Modules/_io/textio.c:2206 _textiowrapper_readline (chunked path)
func (t *TextIOWrapper) drainLine(limit int) (string, error) {
	for {
		if idx := strings.IndexByte(t.decodedBuf, '\n'); idx >= 0 {
			line := t.decodedBuf[:idx+1]
			t.decodedBuf = t.decodedBuf[idx+1:]
			if limit >= 0 && len(line) > limit {
				t.decodedBuf = line[limit:] + t.decodedBuf
				line = line[:limit]
			}
			return line, nil
		}
		if limit >= 0 && len(t.decodedBuf) >= limit {
			line := t.decodedBuf[:limit]
			t.decodedBuf = t.decodedBuf[limit:]
			return line, nil
		}
		more, err := t.readChunk(t.chunkSize)
		if err != nil {
			return "", err
		}
		if !more {
			out := t.decodedBuf
			t.decodedBuf = ""
			if limit >= 0 && len(out) > limit {
				t.decodedBuf = out[limit:]
				out = out[:limit]
			}
			return out, nil
		}
	}
}
