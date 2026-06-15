// Full port of Modules/_io/bufferedio.c.
//
// Four classes: _BufferedIOBase (abstract), BufferedReader,
// BufferedWriter, BufferedRandom, BufferedRWPair.
//
// The Buffered struct mirrors CPython's `buffered` struct (one slab
// shared between reads and writes, with pos / raw_pos / read_end /
// write_pos / write_end offsets) so that BufferedRandom can interleave
// reads and writes without losing position.
//
// CPython: Modules/_io/bufferedio.c

package io

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// BufferedIOBaseType is the type singleton for _io._BufferedIOBase.
//
// CPython: Modules/_io/bufferedio.c:2528 bufferediobase_spec
var BufferedIOBaseType = objects.NewType("_io._BufferedIOBase", []*objects.Type{IOBaseType})

// --- shared buffered struct --------------------------------------------------

// Buffered backs BufferedReader, BufferedWriter, and BufferedRandom.
//
// Field names mirror CPython's `buffered` struct exactly so the ported
// logic reads side-by-side with the C source.
//
// CPython: Modules/_io/bufferedio.c:222 buffered (typedef)
type Buffered struct {
	objects.Header

	raw      objects.Object
	ok       bool
	detached bool
	readable bool
	writable bool

	// Absolute position inside the raw stream (-1 if unknown).
	absPos int64

	// The slab. Sized at construction time and never resized.
	buffer []byte
	// Current logical position in the buffer.
	pos int
	// Position of the raw stream inside the buffer.
	rawPos int
	// One past the last buffered byte (read view); -1 if read buffer invalid.
	readEnd int
	// One past the last written byte that has been flushed to raw.
	writePos int
	// One past the last byte waiting to be written; -1 if write buffer invalid.
	writeEnd int

	bufferSize int
	bufferMask int

	closed bool
}

// validReadBuf reports whether the read buffer view is valid.
//
// CPython: Modules/_io/bufferedio.c:375 VALID_READ_BUFFER
func (b *Buffered) validReadBuf() bool { return b.readable && b.readEnd != -1 }

// validWriteBuf reports whether the write buffer view is valid.
//
// CPython: Modules/_io/bufferedio.c:378 VALID_WRITE_BUFFER
func (b *Buffered) validWriteBuf() bool { return b.writable && b.writeEnd != -1 }

// readAhead returns the count of buffered bytes still available for reading.
//
// CPython: Modules/_io/bufferedio.c:388 READAHEAD
func (b *Buffered) readAhead() int {
	if b.validReadBuf() {
		return b.readEnd - b.pos
	}
	return 0
}

// rawOffset returns the distance from the raw stream's position back to
// the logical buffer position.
//
// CPython: Modules/_io/bufferedio.c:392 RAW_OFFSET
func (b *Buffered) rawOffset() int {
	if (b.validReadBuf() || b.validWriteBuf()) && b.rawPos >= 0 {
		return b.rawPos - b.pos
	}
	return 0
}

// adjustPosition moves pos and extends read_end if the new position
// would step past the read view.
//
// CPython: Modules/_io/bufferedio.c:381 ADJUST_POSITION
func (b *Buffered) adjustPosition(newPos int) {
	b.pos = newPos
	if b.validReadBuf() && b.readEnd < b.pos {
		b.readEnd = b.pos
	}
}

// minusLastBlock rounds size down to the nearest buffer-block boundary.
//
// CPython: Modules/_io/bufferedio.c:399 MINUS_LAST_BLOCK
func (b *Buffered) minusLastBlock(size int) int {
	if b.bufferMask != 0 {
		return size &^ b.bufferMask
	}
	return b.bufferSize * (size / b.bufferSize)
}

// checkInitialized returns an error if the object is uninitialized or detached.
//
// CPython: Modules/_io/bufferedio.c:339 CHECK_INITIALIZED
func (b *Buffered) checkInitialized() error {
	if b.ok {
		return nil
	}
	if b.detached {
		return fmt.Errorf("ValueError: raw stream has been detached")
	}
	return fmt.Errorf("ValueError: I/O operation on uninitialized object")
}

// isClosed reports whether the underlying raw stream is closed.
//
// CPython: Modules/_io/bufferedio.c:363 IS_CLOSED / buffered_closed
func (b *Buffered) isClosed() (bool, error) {
	if b.buffer == nil {
		return true, nil
	}
	if b.raw == nil {
		return true, nil
	}
	res, err := objects.GetAttr(b.raw, objects.NewStr("closed"))
	if err != nil {
		return false, err
	}
	return objects.IsTruthy(res)
}

// checkClosedMsg returns an error if the stream is closed and the read
// buffer is exhausted.
//
// CPython: Modules/_io/bufferedio.c:369 CHECK_CLOSED
func (b *Buffered) checkClosedMsg(msg string) error {
	closed, err := b.isClosed()
	if err != nil {
		return err
	}
	if closed && b.readAhead() == 0 {
		return fmt.Errorf("ValueError: %s", msg)
	}
	return nil
}

// rawTell queries the raw stream's current position and caches it.
//
// CPython: Modules/_io/bufferedio.c:774 _buffered_raw_tell
func (b *Buffered) rawTell() (int64, error) {
	n, err := bufTell(b.raw)
	if err != nil {
		return -1, err
	}
	if n < 0 {
		return -1, fmt.Errorf("OSError: Raw stream returned invalid position %d", n)
	}
	b.absPos = n
	return n, nil
}

// rawSeek seeks the raw stream and caches the new absolute position.
//
// CPython: Modules/_io/bufferedio.c:795 _buffered_raw_seek
func (b *Buffered) rawSeek(target int64, whence int) (int64, error) {
	n, err := bufSeek(b.raw, target, whence)
	if err != nil {
		return -1, err
	}
	if n < 0 {
		return -1, fmt.Errorf("OSError: Raw stream returned invalid position %d", n)
	}
	b.absPos = n
	return n, nil
}

// bufferedInit allocates the slab and computes the buffer mask.
//
// CPython: Modules/_io/bufferedio.c:828 _buffered_init
func (b *Buffered) bufferedInit() error {
	if b.bufferSize <= 0 {
		return fmt.Errorf("ValueError: buffer size must be strictly positive")
	}
	b.buffer = make([]byte, b.bufferSize)
	// Find out whether buffer_size is a power of 2.
	n := b.bufferSize - 1
	for n&1 != 0 {
		n >>= 1
	}
	if n == 0 {
		b.bufferMask = b.bufferSize - 1
	} else {
		b.bufferMask = 0
	}
	if _, err := b.rawTell(); err != nil {
		b.absPos = -1
	}
	return nil
}

// readerResetBuf invalidates the read view.
//
// CPython: Modules/_io/bufferedio.c:1564 _bufferedreader_reset_buf
func (b *Buffered) readerResetBuf() { b.readEnd = -1 }

// writerResetBuf invalidates the write view.
//
// CPython: Modules/_io/bufferedio.c:1914 _bufferedwriter_reset_buf
func (b *Buffered) writerResetBuf() {
	b.writePos = 0
	b.writeEnd = -1
}

// --- raw I/O helpers ---------------------------------------------------------

// readerRawRead reads up to len bytes from the raw stream directly into dst.
// Returns the number of bytes written, or -2 for "would block".
//
// CPython: Modules/_io/bufferedio.c:1608 _bufferedreader_raw_read
func (b *Buffered) readerRawRead(dst []byte) (int, error) {
	chunk, err := bufRead(b.raw, len(dst))
	if err != nil {
		return -1, err
	}
	if chunk == nil {
		return -2, nil
	}
	if len(chunk) > len(dst) {
		return -1, fmt.Errorf("OSError: raw readinto() returned invalid length %d (should have been between 0 and %d)", len(chunk), len(dst))
	}
	copy(dst, chunk)
	if len(chunk) > 0 && b.absPos != -1 {
		b.absPos += int64(len(chunk))
	}
	return len(chunk), nil
}

// readerFillBuffer reads one batch into the slab, extending readEnd.
//
// CPython: Modules/_io/bufferedio.c:1658 _bufferedreader_fill_buffer
func (b *Buffered) readerFillBuffer() (int, error) {
	start := 0
	if b.validReadBuf() {
		start = b.readEnd
	}
	n, err := b.readerRawRead(b.buffer[start:b.bufferSize])
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return n, nil
	}
	b.readEnd = start + n
	b.rawPos = start + n
	return n, nil
}

// writerRawWrite writes from src to the raw stream.
// Returns the count actually written, or -2 for "would block".
//
// CPython: Modules/_io/bufferedio.c:1966 _bufferedwriter_raw_write
func (b *Buffered) writerRawWrite(src []byte) (int, error) {
	n, err := bufWrite(b.raw, src)
	if err != nil {
		return -1, err
	}
	if n < 0 || n > len(src) {
		return -1, fmt.Errorf("OSError: raw write() returned invalid length %d (should have been between 0 and %d)", n, len(src))
	}
	if n > 0 && b.absPos != -1 {
		b.absPos += int64(n)
	}
	return n, nil
}

// writerFlushUnlocked drains the pending write buffer.
//
// CPython: Modules/_io/bufferedio.c:2013 _bufferedwriter_flush_unlocked
func (b *Buffered) writerFlushUnlocked() error {
	if !b.validWriteBuf() || b.writePos == b.writeEnd {
		b.writerResetBuf()
		return nil
	}
	rewind := b.rawOffset() + (b.pos - b.writePos)
	if rewind != 0 {
		if _, err := b.rawSeek(int64(-rewind), 1); err != nil {
			return err
		}
		b.rawPos -= rewind
	}
	for b.writePos < b.writeEnd {
		n, err := b.writerRawWrite(b.buffer[b.writePos:b.writeEnd])
		if err != nil {
			return err
		}
		if n == -2 {
			return fmt.Errorf("BlockingIOError: write could not complete without blocking")
		}
		b.writePos += n
		b.rawPos = b.writePos
	}
	b.writerResetBuf()
	return nil
}

// flushAndRewindUnlocked flushes the writer and rewinds the raw stream
// so its position matches the logical position.
//
// CPython: Modules/_io/bufferedio.c:898 buffered_flush_and_rewind_unlocked
func (b *Buffered) flushAndRewindUnlocked() error {
	if err := b.writerFlushUnlocked(); err != nil {
		return err
	}
	if b.readable {
		if _, err := b.rawSeek(int64(-b.rawOffset()), 1); err != nil {
			b.readerResetBuf()
			return err
		}
		b.readerResetBuf()
	}
	return nil
}

// --- _BufferedIOBase abstract methods ----------------------------------------

// bufferedIOBaseGetattr dispatches attribute lookups on _BufferedIOBase.
//
// CPython: Modules/_io/bufferedio.c:2521 bufferediobase_slots
func bufferedIOBaseGetattr(self objects.Object, nameObj objects.Object) (objects.Object, error) {
	name, ok := nameObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	if v, ok, err := ioUserInstanceAttr(self, nameObj); ok || err != nil {
		return v, err
	}
	switch name.Value() {
	case "detach":
		return objects.NewBuiltinFunction("detach", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: detach")
		}), nil
	case "read":
		return objects.NewBuiltinFunction("read", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: read")
		}), nil
	case "read1":
		return objects.NewBuiltinFunction("read1", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: read1")
		}), nil
	case "readinto":
		return objects.NewBuiltinFunction("readinto", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return bufferedIOBaseReadintoGeneric(self, args, false)
		}), nil
	case "readinto1":
		return objects.NewBuiltinFunction("readinto1", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return bufferedIOBaseReadintoGeneric(self, args, true)
		}), nil
	case "write":
		return objects.NewBuiltinFunction("write", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: write")
		}), nil
	}
	// Dunders such as __class__/__dict__ resolve through the MRO walk.
	return objects.GenericGetAttr(self, nameObj)
}

// bufferedIOBaseReadintoGeneric implements the shared concrete fallback for
// readinto / readinto1: dispatch to self.read(len(b)) and memcpy.
//
// CPython: Modules/_io/bufferedio.c:50 _bufferediobase_readinto_generic
func bufferedIOBaseReadintoGeneric(self objects.Object, args []objects.Object, readinto1 bool) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: readinto() takes exactly 1 argument (%d given)", len(args))
	}
	dst, dstLen, err := writableBuffer(args[0])
	if err != nil {
		return nil, err
	}
	methodName := "read"
	if readinto1 {
		methodName = "read1"
	}
	fn, err := objects.GetAttr(self, objects.NewStr(methodName))
	if err != nil {
		return nil, err
	}
	res, err := objects.Call(fn, objects.NewTuple([]objects.Object{objects.NewInt(int64(dstLen))}), nil)
	if err != nil {
		return nil, err
	}
	bobj, ok := res.(*objects.Bytes)
	if !ok {
		return nil, fmt.Errorf("TypeError: read() should return bytes")
	}
	src := bobj.Bytes()
	if len(src) > dstLen {
		return nil, fmt.Errorf("ValueError: read() returned too much data: %d bytes requested, %d returned", dstLen, len(src))
	}
	copy(dst, src)
	return objects.NewInt(int64(len(src))), nil
}

// writableBuffer extracts a writable byte slice from a bytearray.
func writableBuffer(o objects.Object) ([]byte, int, error) {
	if v, ok := o.(*objects.ByteArray); ok {
		b := v.Bytes()
		return b, len(b), nil
	}
	return nil, 0, fmt.Errorf("TypeError: a writable bytes-like object is required, not '%s'", o.Type().Name)
}

func init() { BufferedIOBaseType.Getattro = bufferedIOBaseGetattr }

// --- Reader helpers ----------------------------------------------------------

// readerReadAll reads all remaining bytes from the stream.
//
// CPython: Modules/_io/bufferedio.c:1675 _bufferedreader_read_all
func (b *Buffered) readerReadAll() (objects.Object, error) {
	currentSize := b.readAhead()
	var data []byte
	if currentSize > 0 {
		data = append(data, b.buffer[b.pos:b.pos+currentSize]...)
		b.pos += currentSize
	}
	if b.writable {
		if err := b.flushAndRewindUnlocked(); err != nil {
			return nil, err
		}
	}
	b.readerResetBuf()
	// Try raw.readall() first.
	if readall, err := objects.GetAttr(b.raw, objects.NewStr("readall")); err == nil {
		res, callErr := objects.Call(readall, objects.NewTuple(nil), nil)
		if callErr != nil {
			return nil, callErr
		}
		if objects.IsNone(res) {
			if currentSize == 0 {
				return objects.None(), nil
			}
			return objects.NewBytes(data), nil
		}
		bts, ok := res.(*objects.Bytes)
		if !ok {
			return nil, fmt.Errorf("TypeError: readall() should return bytes")
		}
		if currentSize == 0 {
			return bts, nil
		}
		return objects.NewBytes(append(data, bts.Bytes()...)), nil
	}
	// Fallback: loop on raw.read() until EOF.
	for {
		chunk, err := bufRead(b.raw, -1)
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			if currentSize == 0 {
				return objects.None(), nil
			}
			return objects.NewBytes(data), nil
		}
		if len(chunk) == 0 {
			return objects.NewBytes(data), nil
		}
		data = append(data, chunk...)
		currentSize += len(chunk)
		if b.absPos != -1 {
			b.absPos += int64(len(chunk))
		}
	}
}

// readerReadFast returns n bytes from the buffer or nil if not enough buffered.
//
// CPython: Modules/_io/bufferedio.c:1767 _bufferedreader_read_fast
func (b *Buffered) readerReadFast(n int) objects.Object {
	if n <= b.readAhead() {
		out := make([]byte, n)
		copy(out, b.buffer[b.pos:b.pos+n])
		b.pos += n
		return objects.NewBytes(out)
	}
	return nil
}

// readerReadGeneric reads exactly n bytes (or fewer at EOF).
//
// CPython: Modules/_io/bufferedio.c:1786 _bufferedreader_read_generic
func (b *Buffered) readerReadGeneric(n int) (objects.Object, error) {
	current := b.readAhead()
	if n <= current {
		return b.readerReadFast(n), nil
	}
	out := make([]byte, n)
	remaining := n
	written := 0
	if current > 0 {
		copy(out, b.buffer[b.pos:b.pos+current])
		remaining -= current
		written += current
		b.pos += current
	}
	if b.writable {
		if err := b.flushAndRewindUnlocked(); err != nil {
			return nil, err
		}
	}
	b.readerResetBuf()
	for remaining > 0 {
		r := b.minusLastBlock(remaining)
		if r == 0 {
			break
		}
		got, err := b.readerRawRead(out[written : written+r])
		if err != nil {
			return nil, err
		}
		if got == 0 || got == -2 {
			if written > 0 || got == 0 {
				return objects.NewBytes(out[:written]), nil
			}
			return objects.None(), nil
		}
		remaining -= got
		written += got
	}
	b.pos = 0
	b.rawPos = 0
	b.readEnd = 0
	for remaining > 0 && b.readEnd < b.bufferSize {
		r, err := b.readerFillBuffer()
		if err != nil {
			return nil, err
		}
		if r == 0 || r == -2 {
			if written > 0 || r == 0 {
				return objects.NewBytes(out[:written]), nil
			}
			return objects.None(), nil
		}
		take := min(r, remaining)
		copy(out[written:], b.buffer[b.pos:b.pos+take])
		written += take
		b.pos += take
		remaining -= take
	}
	return objects.NewBytes(out[:written]), nil
}

// readerPeekUnlocked returns the buffered bytes without advancing the position.
//
// CPython: Modules/_io/bufferedio.c:1883 _bufferedreader_peek_unlocked
func (b *Buffered) readerPeekUnlocked() (objects.Object, error) {
	have := b.readAhead()
	if have > 0 {
		out := make([]byte, have)
		copy(out, b.buffer[b.pos:b.pos+have])
		return objects.NewBytes(out), nil
	}
	b.readerResetBuf()
	r, err := b.readerFillBuffer()
	if err != nil {
		return nil, err
	}
	if r == -2 {
		r = 0
	}
	b.pos = 0
	out := make([]byte, r)
	copy(out, b.buffer[:r])
	return objects.NewBytes(out), nil
}

// --- shared method implementations -------------------------------------------

// bufferedRead implements read([size]).
//
// CPython: Modules/_io/bufferedio.c:982 _io__Buffered_read_impl
func (b *Buffered) bufferedRead(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	size := -1
	if len(args) > 0 && !objects.IsNone(args[0]) {
		n, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: read() size must be int or None")
		}
		v, _ := n.Int64()
		size = int(v)
	}
	if size < -1 {
		return nil, fmt.Errorf("ValueError: read length must be non-negative or -1")
	}
	if err := b.checkClosedMsg("read of closed file"); err != nil {
		return nil, err
	}
	if size == -1 {
		return b.readerReadAll()
	}
	if res := b.readerReadFast(size); res != nil {
		return res, nil
	}
	return b.readerReadGeneric(size)
}

// bufferedRead1 returns up to n bytes using at most one raw read.
//
// CPython: Modules/_io/bufferedio.c:1024 _io__Buffered_read1_impl
func (b *Buffered) bufferedRead1(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	n := -1
	if len(args) > 0 && !objects.IsNone(args[0]) {
		nObj, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: read1() size must be int or None")
		}
		v, _ := nObj.Int64()
		n = int(v)
	}
	if n < 0 {
		n = b.bufferSize
	}
	if err := b.checkClosedMsg("read of closed file"); err != nil {
		return nil, err
	}
	if n == 0 {
		return objects.NewBytes(nil), nil
	}
	have := b.readAhead()
	if have > 0 {
		if n > have {
			n = have
		}
		return b.readerReadFast(n), nil
	}
	out := make([]byte, n)
	if b.writable {
		if err := b.flushAndRewindUnlocked(); err != nil {
			return nil, err
		}
	}
	b.readerResetBuf()
	r, err := b.readerRawRead(out)
	if err != nil {
		return nil, err
	}
	if r == -2 {
		r = 0
	}
	return objects.NewBytes(out[:r]), nil
}

// bufferedReadintoGeneric fills the destination buffer.
//
// CPython: Modules/_io/bufferedio.c:1082 _buffered_readinto_generic
func (b *Buffered) bufferedReadintoGeneric(buffer []byte, readinto1 bool) (int, error) {
	if err := b.checkInitialized(); err != nil {
		return 0, err
	}
	if err := b.checkClosedMsg("readinto of closed file"); err != nil {
		return 0, err
	}
	bufLen := len(buffer)
	written := 0
	have := b.readAhead()
	if have > 0 {
		if have >= bufLen {
			copy(buffer, b.buffer[b.pos:b.pos+bufLen])
			b.pos += bufLen
			return bufLen, nil
		}
		copy(buffer, b.buffer[b.pos:b.pos+have])
		b.pos += have
		written = have
	}
	if b.writable {
		if err := b.flushAndRewindUnlocked(); err != nil {
			return written, err
		}
	}
	b.readerResetBuf()
	b.pos = 0
	for remaining := bufLen - written; remaining > 0; {
		var n int
		var err error
		if remaining > b.bufferSize {
			n, err = b.readerRawRead(buffer[written : written+remaining])
		} else if !readinto1 || written == 0 {
			n, err = b.readerFillBuffer()
			if err != nil {
				return written, err
			}
			if n > 0 {
				if n > remaining {
					n = remaining
				}
				copy(buffer[written:], b.buffer[b.pos:b.pos+n])
				b.pos += n
				written += n
				remaining -= n
				continue
			}
		} else {
			n = 0
		}
		if err != nil {
			return written, err
		}
		if n == 0 || (n == -2 && written > 0) {
			break
		}
		if n < 0 {
			return written, nil
		}
		written += n
		remaining -= n
		if readinto1 {
			break
		}
	}
	return written, nil
}

// bufferedReadinto is the public dispatch for readinto / readinto1.
func (b *Buffered) bufferedReadinto(args []objects.Object, readinto1 bool) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: readinto() requires a buffer argument")
	}
	ba, ok := args[0].(*objects.ByteArray)
	if !ok {
		return nil, fmt.Errorf("TypeError: readinto() argument must be a writable bytearray")
	}
	n, err := b.bufferedReadintoGeneric(ba.Bytes(), readinto1)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// bufferedReadline reads one line.
//
// CPython: Modules/_io/bufferedio.c:1193 _buffered_readline
func (b *Buffered) bufferedReadline(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosedMsg("readline of closed file"); err != nil {
		return nil, err
	}
	limit := -1
	if len(args) > 0 && !objects.IsNone(args[0]) {
		n, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: readline() size must be int or None")
		}
		v, _ := n.Int64()
		limit = int(v)
	}
	n := b.readAhead()
	if limit >= 0 && n > limit {
		n = limit
	}
	if n > 0 {
		slice := b.buffer[b.pos : b.pos+n]
		for i, c := range slice {
			if c == '\n' {
				out := make([]byte, i+1)
				copy(out, slice[:i+1])
				b.pos += i + 1
				return objects.NewBytes(out), nil
			}
		}
		if n == limit {
			out := make([]byte, n)
			copy(out, slice)
			b.pos += n
			return objects.NewBytes(out), nil
		}
	}
	var result []byte
	if n > 0 {
		result = append(result, b.buffer[b.pos:b.pos+n]...)
		b.pos += n
		if limit >= 0 {
			limit -= n
		}
	}
	if b.writable {
		if err := b.flushAndRewindUnlocked(); err != nil {
			return nil, err
		}
	}
	for {
		b.readerResetBuf()
		r, err := b.readerFillBuffer()
		if err != nil {
			return nil, err
		}
		if r <= 0 {
			break
		}
		take := r
		if limit >= 0 && take > limit {
			take = limit
		}
		slice := b.buffer[:take]
		found := -1
		for i, c := range slice {
			if c == '\n' {
				found = i
				break
			}
		}
		if found >= 0 {
			result = append(result, slice[:found+1]...)
			b.pos = found + 1
			return objects.NewBytes(result), nil
		}
		result = append(result, slice...)
		if take == limit {
			b.pos = take
			break
		}
		if limit >= 0 {
			limit -= take
		}
	}
	return objects.NewBytes(result), nil
}

// bufferedPeek returns up to one buffer-load of bytes without advancing.
//
// CPython: Modules/_io/bufferedio.c:950 _io__Buffered_peek_impl
func (b *Buffered) bufferedPeek(_ []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosedMsg("peek of closed file"); err != nil {
		return nil, err
	}
	if b.writable {
		if err := b.flushAndRewindUnlocked(); err != nil {
			return nil, err
		}
	}
	return b.readerPeekUnlocked()
}

// bufferedWrite buffers data and flushes when the buffer is full.
//
// CPython: Modules/_io/bufferedio.c:2076 _io_BufferedWriter_write_impl
func (b *Buffered) bufferedWrite(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	closed, err := b.isClosed()
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, fmt.Errorf("ValueError: write to closed file")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: write() requires a data argument")
	}
	data, ok := objects.AsBytesLike(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: a bytes-like object is required, not %s", args[0].Type().Name)
	}
	written := 0
	// Fast path: data fits entirely in current buffer slot.
	if !b.validReadBuf() && !b.validWriteBuf() {
		b.pos = 0
		b.rawPos = 0
	}
	avail := b.bufferSize - b.pos
	if len(data) <= avail && len(data) < b.bufferSize {
		copy(b.buffer[b.pos:], data)
		if !b.validWriteBuf() || b.writePos > b.pos {
			b.writePos = b.pos
		}
		b.adjustPosition(b.pos + len(data))
		if b.pos > b.writeEnd {
			b.writeEnd = b.pos
		}
		return objects.NewInt(int64(len(data))), nil
	}
	// Flush existing write buffer.
	if err := b.writerFlushUnlocked(); err != nil {
		return nil, err
	}
	// Sync raw stream position with logical pos.
	off := b.rawOffset()
	if off != 0 {
		if _, err := b.rawSeek(int64(-off), 1); err != nil {
			return nil, err
		}
		b.rawPos -= off
	}
	// Write bulk data directly through raw stream.
	remaining := len(data)
	for remaining >= b.bufferSize {
		n, err := b.writerRawWrite(data[written:])
		if err != nil {
			return nil, err
		}
		if n == -2 {
			break
		}
		written += n
		remaining -= n
	}
	if b.readable {
		b.readerResetBuf()
	}
	if remaining > 0 {
		copy(b.buffer, data[written:])
		written += remaining
	}
	b.writePos = 0
	b.writeEnd = remaining
	b.adjustPosition(remaining)
	b.rawPos = 0
	return objects.NewInt(int64(written)), nil
}

// bufferedFlush flushes the write buffer and rewinds the raw stream.
//
// CPython: Modules/_io/bufferedio.c:925 _io__Buffered_flush_impl
func (b *Buffered) bufferedFlush() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosedMsg("flush of closed file"); err != nil {
		return nil, err
	}
	if err := b.flushAndRewindUnlocked(); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// bufferedSimpleFlush forwards flush() straight to the raw stream
// without rewinding. BufferedReader installs this in its method table.
//
// CPython: Modules/_io/bufferedio.c:503 _io__Buffered_simple_flush_impl
func (b *Buffered) bufferedSimpleFlush() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	return bufCall(b.raw, "flush", nil)
}

// bufferedClose flushes and closes the stream.
//
// CPython: Modules/_io/bufferedio.c:544 _io__Buffered_close_impl
func (b *Buffered) bufferedClose() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	closed, err := b.isClosed()
	if err != nil {
		return nil, err
	}
	if closed {
		return objects.None(), nil
	}
	// _PyFile_Flush(self) dispatches via self.flush(). BufferedReader binds
	// flush to simple_flush (raw.flush() only); BufferedWriter and
	// BufferedRandom bind it to the full flush_and_rewind path. Mirror
	// that here so closing a read-only stream on a pipe does not seek.
	// CPython: Modules/_io/bufferedio.c:573 _PyFile_Flush + :2538 simple_flush binding
	var flushErr error
	if b.readable && !b.writable {
		_, flushErr = bufCall(b.raw, "flush", nil)
	} else {
		flushErr = b.flushAndRewindUnlocked()
	}
	if err := bufClose(b.raw); err != nil {
		return nil, err
	}
	b.buffer = nil
	b.closed = true
	b.readEnd = 0
	b.pos = 0
	if flushErr != nil {
		return nil, flushErr
	}
	return objects.None(), nil
}

// bufferedDetach flushes and returns the raw stream.
//
// CPython: Modules/_io/bufferedio.c:607 _io__Buffered_detach_impl
func (b *Buffered) bufferedDetach() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.flushAndRewindUnlocked(); err != nil {
		return nil, err
	}
	raw := b.raw
	b.raw = nil
	b.detached = true
	b.ok = false
	return raw, nil
}

// bufferedSeekable delegates to the raw stream.
//
// CPython: Modules/_io/bufferedio.c:630 _io__Buffered_seekable_impl
func (b *Buffered) bufferedSeekable() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	return bufCall(b.raw, "seekable", nil)
}

// bufferedFileno delegates to the raw stream.
//
// CPython: Modules/_io/bufferedio.c:700 _io__Buffered_fileno_impl
func (b *Buffered) bufferedFileno() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	return bufCall(b.raw, "fileno", nil)
}

// bufferedIsatty delegates to the raw stream.
//
// CPython: Modules/_io/bufferedio.c:713 _io__Buffered_isatty_impl
func (b *Buffered) bufferedIsatty() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	return bufCall(b.raw, "isatty", nil)
}

// bufferedTell returns the logical stream position.
//
// CPython: Modules/_io/bufferedio.c:1322 _io__Buffered_tell_impl
func (b *Buffered) bufferedTell() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	pos, err := b.rawTell()
	if err != nil {
		return nil, err
	}
	pos -= int64(b.rawOffset())
	if pos < 0 {
		pos = 0
	}
	return objects.NewInt(pos), nil
}

// bufferedSeek repositions the stream.
//
// CPython: Modules/_io/bufferedio.c:1349 _io__Buffered_seek_impl
func (b *Buffered) bufferedSeek(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: seek() requires target argument")
	}
	targetObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: seek() target must be int")
	}
	target, _ := targetObj.Int64()
	whence := 0
	if len(args) >= 2 {
		w, ok2 := args[1].(*objects.Int)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: seek() whence must be int")
		}
		v, _ := w.Int64()
		whence = int(v)
	}
	if whence < 0 || whence > 2 {
		return nil, fmt.Errorf("ValueError: whence value %d unsupported", whence)
	}
	if err := b.checkClosedMsg("seek of closed file"); err != nil {
		return nil, err
	}
	// Fast path: stay within current buffer view for SEEK_SET / SEEK_CUR.
	if (whence == 0 || whence == 1) && b.readable {
		current := b.absPos
		if current < 0 {
			n, err := b.rawTell()
			if err == nil {
				current = n
			}
		}
		avail := int64(b.readAhead())
		if avail > 0 && current >= 0 {
			var offset int64
			if whence == 0 {
				offset = target - (current - int64(b.rawOffset()))
			} else {
				offset = target
			}
			if offset >= -int64(b.pos) && offset <= avail {
				b.pos += int(offset)
				newPos := max(current-avail+offset, 0)
				return objects.NewInt(newPos), nil
			}
		}
	}
	if b.writable {
		if err := b.writerFlushUnlocked(); err != nil {
			return nil, err
		}
	}
	if whence == 1 {
		target -= int64(b.rawOffset())
	}
	n, err := b.rawSeek(target, whence)
	if err != nil {
		return nil, err
	}
	b.rawPos = -1
	if b.readable {
		b.readerResetBuf()
	}
	return objects.NewInt(n), nil
}

// bufferedTruncate truncates the raw stream.
//
// CPython: Modules/_io/bufferedio.c:1452 _io__Buffered_truncate_impl
func (b *Buffered) bufferedTruncate(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosedMsg("truncate of closed file"); err != nil {
		return nil, err
	}
	if !b.writable {
		return nil, fmt.Errorf("UnsupportedOperation: truncate")
	}
	if err := b.flushAndRewindUnlocked(); err != nil {
		return nil, err
	}
	var posArgs []objects.Object
	if len(args) > 0 && !objects.IsNone(args[0]) {
		posArgs = args[:1]
	}
	res, err := bufCall(b.raw, "truncate", posArgs)
	if err != nil {
		return nil, err
	}
	if _, terr := b.rawTell(); terr != nil {
		b.absPos = -1
	}
	return res, nil
}

// bufferedDeallocWarn forwards a _dealloc_warn(source) call to the raw stream.
//
// CPython: Modules/_io/bufferedio.c:477 _io__Buffered__dealloc_warn_impl
func (b *Buffered) bufferedDeallocWarn(source objects.Object) (objects.Object, error) {
	if b.ok && b.raw != nil {
		if fn, err := objects.GetAttr(b.raw, objects.NewStr("_dealloc_warn")); err == nil {
			if _, callErr := objects.Call(fn, objects.NewTuple([]objects.Object{source}), nil); callErr == nil {
				return objects.None(), nil
			}
		}
	}
	return objects.None(), nil
}

// --- repr ----------------------------------------------------------------------

// bufferedRepr formats a Buffered object.
//
// CPython: Modules/_io/bufferedio.c:1527 buffered_repr
func bufferedRepr(b *Buffered) (string, error) {
	typeName := b.Type().Name
	if b.raw == nil {
		return fmt.Sprintf("<%s>", typeName), nil
	}
	nameObj, err := objects.GetAttr(b.raw, objects.NewStr("name"))
	if err != nil || nameObj == nil {
		return fmt.Sprintf("<%s>", typeName), nil //nolint:nilerr // raw without name → repr without name (matches CPython)
	}
	if s, ok := nameObj.(*objects.Unicode); ok {
		return fmt.Sprintf("<%s name='%s'>", typeName, s.Value()), nil
	}
	if i, ok := nameObj.(*objects.Int); ok {
		n, _ := i.Int64()
		return fmt.Sprintf("<%s name=%d>", typeName, n), nil
	}
	return fmt.Sprintf("<%s>", typeName), nil
}

// bufferedTypeRepr is the Repr slot wrapper.
func bufferedTypeRepr(o objects.Object) (string, error) {
	b, ok := o.(*Buffered)
	if !ok {
		return "", fmt.Errorf("TypeError: not a Buffered object")
	}
	return bufferedRepr(b)
}

// --- getattr dispatch -------------------------------------------------------

// bufferedGetattr dispatches attribute lookups for the three Buffered classes.
//
// CPython: Modules/_io/bufferedio.c:2535,2599,2708 method tables
func bufferedGetattr(self objects.Object, nameObj objects.Object) (objects.Object, error) {
	b, ok := self.(*Buffered)
	if !ok {
		return nil, fmt.Errorf("TypeError: not a Buffered object")
	}
	name, ok2 := nameObj.(*objects.Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	typeName := b.Type().Name
	switch name.Value() {
	case "read":
		if !b.readable {
			break
		}
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedRead(args)
		}), nil
	case "read1":
		if !b.readable {
			break
		}
		return objects.NewBuiltinFunction("read1", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedRead1(args)
		}), nil
	case "readline":
		if !b.readable {
			break
		}
		return objects.NewBuiltinFunction("readline", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedReadline(args)
		}), nil
	case "readinto":
		if !b.readable {
			break
		}
		return objects.NewBuiltinFunction("readinto", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedReadinto(args, false)
		}), nil
	case "readinto1":
		if !b.readable {
			break
		}
		return objects.NewBuiltinFunction("readinto1", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedReadinto(args, true)
		}), nil
	case "peek":
		if !b.readable {
			break
		}
		return objects.NewBuiltinFunction("peek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedPeek(args)
		}), nil
	case "write":
		if !b.writable {
			break
		}
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedWrite(args)
		}), nil
	case "flush":
		// BufferedReader binds flush to simple_flush (raw.flush() only);
		// BufferedWriter and BufferedRandom use the full flush_and_rewind.
		// CPython: Modules/_io/bufferedio.c:2538 SIMPLE_FLUSH (reader)
		//          Modules/_io/bufferedio.c:2598 FLUSH (writer)
		//          Modules/_io/bufferedio.c:2706 FLUSH (random)
		if b.readable && !b.writable {
			return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
				return b.bufferedSimpleFlush()
			}), nil
		}
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedFlush()
		}), nil
	case "seek":
		return objects.NewBuiltinFunction("seek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedSeek(args)
		}), nil
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedTell()
		}), nil
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedTruncate(args)
		}), nil
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedClose()
		}), nil
	case "detach":
		return objects.NewBuiltinFunction("detach", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedDetach()
		}), nil
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedSeekable()
		}), nil
	case "readable":
		rv := b.readable
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(rv), nil
		}), nil
	case "writable":
		wv := b.writable
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(wv), nil
		}), nil
	case "fileno":
		return objects.NewBuiltinFunction("fileno", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedFileno()
		}), nil
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedIsatty()
		}), nil
	case "_dealloc_warn":
		return objects.NewBuiltinFunction("_dealloc_warn", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: _dealloc_warn() takes exactly one argument")
			}
			return b.bufferedDeallocWarn(args[0])
		}), nil
	case "__sizeof__":
		// CPython: Modules/_io/bufferedio.c:444 _io__Buffered___sizeof___impl
		basic := int64(64)
		size := basic + int64(b.bufferSize)
		return objects.NewBuiltinFunction("__sizeof__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewInt(size), nil
		}), nil
	case "__getstate__":
		return objects.NewBuiltinFunction("__getstate__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("TypeError: cannot pickle '%s' object", typeName)
		}), nil
	case "raw":
		if b.raw == nil {
			return objects.None(), nil
		}
		return b.raw, nil
	case "closed":
		// CPython: Modules/_io/bufferedio.c:531 _io__Buffered_closed_get_impl
		if err := b.checkInitialized(); err != nil {
			return nil, err
		}
		return objects.GetAttr(b.raw, objects.NewStr("closed"))
	case "name":
		if b.raw == nil {
			return nil, fmt.Errorf("ValueError: raw stream has been detached")
		}
		return objects.GetAttr(b.raw, objects.NewStr("name"))
	case "mode":
		if b.raw == nil {
			return nil, fmt.Errorf("ValueError: raw stream has been detached")
		}
		return objects.GetAttr(b.raw, objects.NewStr("mode"))
	case "closefd":
		if b.raw == nil {
			return objects.NewBool(false), nil
		}
		v, err := objects.GetAttr(b.raw, objects.NewStr("closefd"))
		if err != nil {
			return objects.NewBool(true), nil //nolint:nilerr // raw without closefd attr → True
		}
		return v, nil
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkInitialized(); err != nil {
				return nil, err
			}
			closed, err := b.isClosed()
			if err != nil {
				return nil, err
			}
			if closed {
				return nil, fmt.Errorf("ValueError: I/O operation on closed file")
			}
			return b, nil
		}), nil
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			_, err := b.bufferedClose()
			if err != nil {
				return nil, err
			}
			return objects.None(), nil
		}), nil
	case "__iter__":
		return objects.NewBuiltinFunction("__iter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkInitialized(); err != nil {
				return nil, err
			}
			return b, nil
		}), nil
	case "__next__":
		// CPython: Modules/_io/bufferedio.c:1486 buffered_iternext
		return objects.NewBuiltinFunction("__next__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			line, err := b.bufferedReadline(nil)
			if err != nil {
				return nil, err
			}
			lb, ok := line.(*objects.Bytes)
			if !ok || len(lb.Bytes()) == 0 {
				return nil, objects.ErrStopIteration
			}
			return line, nil
		}), nil
	case "__repr__":
		return objects.NewBuiltinFunction("__repr__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			s, err := bufferedRepr(b)
			if err != nil {
				return nil, err
			}
			return objects.NewStr(s), nil
		}), nil
	}
	// Dunders such as __class__/__dict__ resolve through the MRO walk.
	return objects.GenericGetAttr(self, nameObj)
}

// --- constructors ------------------------------------------------------------

// BufferedReaderType is the type singleton for _io.BufferedReader.
//
// CPython: Modules/_io/bufferedio.c:2591 bufferedreader_spec
var BufferedReaderType = objects.NewType("_io.BufferedReader", []*objects.Type{BufferedIOBaseType})

// bufferedReaderCall constructs a BufferedReader.
//
// CPython: Modules/_io/bufferedio.c:1577 _io_BufferedReader___init___impl
func bufferedReaderCall(_ objects.Object, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: BufferedReader() missing required argument 'raw'")
	}
	raw := args[0]
	bufSize := DefaultBufferSize
	if len(args) >= 2 {
		n, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: buffer_size must be int")
		}
		v, _ := n.Int64()
		bufSize = int(v)
	}
	b := &Buffered{
		raw:        raw,
		readable:   true,
		writable:   false,
		bufferSize: bufSize,
		absPos:     -1,
	}
	b.Init(BufferedReaderType)
	if err := b.bufferedInit(); err != nil {
		return nil, err
	}
	b.readerResetBuf()
	b.ok = true
	return b, nil
}

func init() {
	BufferedReaderType.Call = bufferedReaderCall
	BufferedReaderType.Getattro = bufferedGetattr
	BufferedReaderType.Repr = bufferedTypeRepr
	BufferedReaderType.Str = bufferedTypeRepr
	// Buffered streams inherit object's identity-based __hash__ so they
	// can be used as dict keys (subprocess._communicate stores them in
	// _fileobj2output).
	// CPython: Objects/typeobject.c:7970 PyBaseObject_Type tp_hash
	BufferedReaderType.Hash = objects.IdentityHash
	registerBufferedContextManager(BufferedReaderType)
}

// registerBufferedContextManager installs type-level __enter__ /
// __exit__ descriptors so LOAD_SPECIAL (which walks the type MRO, not
// the instance) can resolve `with buf:`.
//
// CPython: Modules/_io/iobase.c:391 iobase_enter / :409 iobase_exit
func registerBufferedContextManager(t *objects.Type) {
	objects.SetTypeDescr(t, "__enter__", objects.NewBuiltinFunction("__enter__", bufferedEnterDescr))
	objects.SetTypeDescr(t, "__exit__", objects.NewBuiltinFunction("__exit__", bufferedExitDescr))
}

func bufferedEnterDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self argument")
	}
	b, ok := args[0].(*Buffered)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__() expected buffered object")
	}
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	closed, err := b.isClosed()
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return b, nil
}

func bufferedExitDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self argument")
	}
	b, ok := args[0].(*Buffered)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__() expected buffered object")
	}
	if _, err := b.bufferedClose(); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// BufferedWriterType is the type singleton for _io.BufferedWriter.
//
// CPython: Modules/_io/bufferedio.c:2649 bufferedwriter_spec
var BufferedWriterType = objects.NewType("_io.BufferedWriter", []*objects.Type{BufferedIOBaseType})

// bufferedWriterCall constructs a BufferedWriter.
//
// CPython: Modules/_io/bufferedio.c:1933 _io_BufferedWriter___init___impl
func bufferedWriterCall(_ objects.Object, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: BufferedWriter() missing required argument 'raw'")
	}
	raw := args[0]
	bufSize := DefaultBufferSize
	if len(args) >= 2 {
		n, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: buffer_size must be int")
		}
		v, _ := n.Int64()
		bufSize = int(v)
	}
	b := &Buffered{
		raw:        raw,
		readable:   false,
		writable:   true,
		bufferSize: bufSize,
		absPos:     -1,
	}
	b.Init(BufferedWriterType)
	if err := b.bufferedInit(); err != nil {
		return nil, err
	}
	b.writerResetBuf()
	b.pos = 0
	b.ok = true
	return b, nil
}

func init() {
	BufferedWriterType.Call = bufferedWriterCall
	BufferedWriterType.Getattro = bufferedGetattr
	BufferedWriterType.Repr = bufferedTypeRepr
	BufferedWriterType.Str = bufferedTypeRepr
	BufferedWriterType.Hash = objects.IdentityHash
	registerBufferedContextManager(BufferedWriterType)
}

// BufferedRandomType is the type singleton for _io.BufferedRandom.
//
// CPython: Modules/_io/bufferedio.c:2767 bufferedrandom_spec
var BufferedRandomType = objects.NewType("_io.BufferedRandom", []*objects.Type{BufferedIOBaseType})

// bufferedRandomCall constructs a BufferedRandom.
//
// CPython: Modules/_io/bufferedio.c:2469 _io_BufferedRandom___init___impl
func bufferedRandomCall(_ objects.Object, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: BufferedRandom() missing required argument 'raw'")
	}
	raw := args[0]
	bufSize := DefaultBufferSize
	if len(args) >= 2 {
		n, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: buffer_size must be int")
		}
		v, _ := n.Int64()
		bufSize = int(v)
	}
	// CPython enforces seekable + readable + writable on the raw stream.
	if seekFn, err := objects.GetAttr(raw, objects.NewStr("seekable")); err == nil {
		if res, callErr := objects.Call(seekFn, objects.NewTuple(nil), nil); callErr == nil {
			if ok, _ := objects.IsTruthy(res); !ok {
				return nil, fmt.Errorf("UnsupportedOperation: File or stream is not seekable")
			}
		}
	}
	b := &Buffered{
		raw:        raw,
		readable:   true,
		writable:   true,
		bufferSize: bufSize,
		absPos:     -1,
	}
	b.Init(BufferedRandomType)
	if err := b.bufferedInit(); err != nil {
		return nil, err
	}
	b.readerResetBuf()
	b.writerResetBuf()
	b.pos = 0
	b.ok = true
	return b, nil
}

func init() {
	BufferedRandomType.Call = bufferedRandomCall
	BufferedRandomType.Getattro = bufferedGetattr
	BufferedRandomType.Repr = bufferedTypeRepr
	BufferedRandomType.Str = bufferedTypeRepr
	BufferedRandomType.Hash = objects.IdentityHash
	registerBufferedContextManager(BufferedRandomType)
}

// --- BufferedRWPair ----------------------------------------------------------

// BufferedRWPairType is the type singleton for _io.BufferedRWPair.
//
// CPython: Modules/_io/bufferedio.c:2699 bufferedrwpair_spec
var BufferedRWPairType = objects.NewType("_io.BufferedRWPair", []*objects.Type{BufferedIOBaseType})

// RWPair backs BufferedRWPair.
//
// CPython: Modules/_io/bufferedio.c:2230 rwpair (typedef)
type RWPair struct {
	objects.Header
	reader *Buffered
	writer *Buffered
}

// bufferedRWPairCall constructs a BufferedRWPair.
//
// CPython: Modules/_io/bufferedio.c:2258 _io_BufferedRWPair___init___impl
func bufferedRWPairCall(_ objects.Object, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: BufferedRWPair() requires reader and writer arguments")
	}
	bufSize := DefaultBufferSize
	if len(args) >= 3 {
		n, ok := args[2].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: buffer_size must be int")
		}
		v, _ := n.Int64()
		bufSize = int(v)
	}
	rObj, err := bufferedReaderCall(nil, []objects.Object{args[0], objects.NewInt(int64(bufSize))}, nil)
	if err != nil {
		return nil, err
	}
	wObj, err := bufferedWriterCall(nil, []objects.Object{args[1], objects.NewInt(int64(bufSize))}, nil)
	if err != nil {
		return nil, err
	}
	p := &RWPair{
		reader: rObj.(*Buffered),
		writer: wObj.(*Buffered),
	}
	p.Init(BufferedRWPairType)
	return p, nil
}

// rwPairGetattr dispatches attribute lookups on BufferedRWPair.
//
// CPython: Modules/_io/bufferedio.c:2657 bufferedrwpair_methods + getset
func rwPairGetattr(self objects.Object, nameObj objects.Object) (objects.Object, error) {
	p, ok := self.(*RWPair)
	if !ok {
		return nil, fmt.Errorf("TypeError: not a BufferedRWPair")
	}
	name, ok2 := nameObj.(*objects.Unicode)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch name.Value() {
	case "read":
		return objects.GetAttr(p.reader, objects.NewStr("read"))
	case "read1":
		return objects.GetAttr(p.reader, objects.NewStr("read1"))
	case "peek":
		return objects.GetAttr(p.reader, objects.NewStr("peek"))
	case "readinto":
		return objects.GetAttr(p.reader, objects.NewStr("readinto"))
	case "readinto1":
		return objects.GetAttr(p.reader, objects.NewStr("readinto1"))
	case "readline":
		return objects.GetAttr(p.reader, objects.NewStr("readline"))
	case "write":
		return objects.GetAttr(p.writer, objects.NewStr("write"))
	case "flush":
		return objects.GetAttr(p.writer, objects.NewStr("flush"))
	case "readable":
		return objects.GetAttr(p.reader, objects.NewStr("readable"))
	case "writable":
		return objects.GetAttr(p.writer, objects.NewStr("writable"))
	case "isatty":
		// CPython: Modules/_io/bufferedio.c:2425 bufferedrwpair_isatty
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			fn, err := objects.GetAttr(p.writer, objects.NewStr("isatty"))
			if err != nil {
				return nil, err
			}
			ret, err2 := objects.Call(fn, objects.NewTuple(nil), nil)
			if err2 != nil {
				return nil, err2
			}
			if objects.IsTrue(ret) {
				return ret, nil
			}
			fn2, err3 := objects.GetAttr(p.reader, objects.NewStr("isatty"))
			if err3 != nil {
				return nil, err3
			}
			return objects.Call(fn2, objects.NewTuple(nil), nil)
		}), nil
	case "close":
		// CPython: Modules/_io/bufferedio.c:2405 bufferedrwpair_close
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			wFn, err := objects.GetAttr(p.writer, objects.NewStr("close"))
			if err != nil {
				return nil, err
			}
			_, writerErr := objects.Call(wFn, objects.NewTuple(nil), nil)
			rFn, err2 := objects.GetAttr(p.reader, objects.NewStr("close"))
			if err2 != nil {
				return nil, err2
			}
			_, readerErr := objects.Call(rFn, objects.NewTuple(nil), nil)
			if writerErr != nil {
				return nil, writerErr
			}
			return objects.None(), readerErr
		}), nil
	case "closed":
		// CPython: Modules/_io/bufferedio.c:2441 bufferedrwpair_closed_get
		return objects.NewBool(p.writer.closed), nil
	}
	// Dunders such as __class__/__dict__ resolve through the MRO walk.
	return objects.GenericGetAttr(self, nameObj)
}

func init() {
	BufferedRWPairType.Call = bufferedRWPairCall
	BufferedRWPairType.Getattro = rwPairGetattr
	BufferedRWPairType.Hash = objects.IdentityHash
}
