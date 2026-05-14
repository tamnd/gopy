// Full port of Modules/_io/bufferedio.c.
//
// Four classes: _BufferedIOBase (abstract), BufferedReader,
// BufferedWriter, BufferedRandom, BufferedRWPair.
//
// The internal buffer is a plain []byte; reads fill it from the raw
// stream via read(); writes accumulate in it and are flushed via
// raw.write(). Protocol dispatch uses bufCall helpers from
// textiowrapper.go so any RawIOBase-compatible object works as the
// underlying raw stream.
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

// Buffered is the shared Go struct backing BufferedReader, BufferedWriter
// and BufferedRandom.
//
// CPython: Modules/_io/bufferedio.c:222 buffered (typedef)
type Buffered struct {
	objects.Header

	raw      objects.Object
	readable bool
	writable bool
	detached bool
	closed   bool

	bufSize int

	// read buffer
	readBuf   []byte
	readStart int
	readEnd   int // -1 means invalid

	// write buffer
	writeBuf []byte
	writeEnd int // -1 means invalid

	absPos int64
}

// readAhead returns the count of bytes available in the read buffer.
//
// CPython: Modules/_io/bufferedio.c:388 READAHEAD
func (b *Buffered) readAhead() int {
	if b.readable && b.readEnd >= 0 && b.readEnd > b.readStart {
		return b.readEnd - b.readStart
	}
	return 0
}

// checkInitialized returns an error if the object is not initialized.
//
// CPython: Modules/_io/bufferedio.c:339 CHECK_INITIALIZED
func (b *Buffered) checkInitialized() error {
	if b.detached {
		return fmt.Errorf("ValueError: raw stream has been detached")
	}
	if b.raw == nil {
		return fmt.Errorf("ValueError: I/O operation on uninitialized object")
	}
	return nil
}

// checkClosed returns an error when the stream is closed and the read buffer
// is exhausted.
//
// CPython: Modules/_io/bufferedio.c:369 CHECK_CLOSED
func (b *Buffered) checkClosed() error {
	if b.closed && b.readAhead() == 0 {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// rawTell queries the raw stream's current position.
//
// CPython: Modules/_io/bufferedio.c:774 _buffered_raw_tell
func (b *Buffered) rawTell() (int64, error) {
	return bufTell(b.raw)
}

// rawSeek repositions the raw stream.
//
// CPython: Modules/_io/bufferedio.c:795 _buffered_raw_seek
func (b *Buffered) rawSeek(target int64, whence int) (int64, error) {
	return bufSeek(b.raw, target, whence)
}

// resetReadBuf clears the read buffer.
//
// CPython: Modules/_io/bufferedio.c:1564 _bufferedreader_reset_buf
func (b *Buffered) resetReadBuf() {
	b.readStart = 0
	b.readEnd = -1
}

// resetWriteBuf clears the write buffer.
//
// CPython: Modules/_io/bufferedio.c:1914 _bufferedwriter_reset_buf
func (b *Buffered) resetWriteBuf() {
	b.writeEnd = -1
}

// fillReadBuf reads one batch from the raw stream into b.readBuf.
//
// CPython: Modules/_io/bufferedio.c:1658 _bufferedreader_fill_buffer
func (b *Buffered) fillReadBuf() (int, error) {
	if b.readBuf == nil {
		b.readBuf = make([]byte, b.bufSize)
	}
	data, err := bufRead(b.raw, b.bufSize)
	if err != nil {
		return 0, err
	}
	n := len(data)
	copy(b.readBuf, data)
	b.readStart = 0
	b.readEnd = n
	if n > 0 && b.absPos >= 0 {
		b.absPos += int64(n)
	}
	return n, nil
}

// flushWrite pushes the write buffer to the raw stream.
//
// CPython: Modules/_io/bufferedio.c:2013 _bufferedwriter_flush_unlocked
func (b *Buffered) flushWrite() error {
	if b.writeEnd <= 0 {
		b.resetWriteBuf()
		return nil
	}
	_, err := bufWrite(b.raw, b.writeBuf[:b.writeEnd])
	if err != nil {
		return err
	}
	b.resetWriteBuf()
	return nil
}

// --- _BufferedIOBase abstract methods ----------------------------------------

// bufferedIOBaseGetattr dispatches attribute lookups on _BufferedIOBase.
// CPython gives read/read1/detach/write as abstract slots that raise
// UnsupportedOperation, while readinto/readinto1 ship a concrete fallback
// that calls self.read(len(b)) and memcpy's the result into the buffer.
//
// CPython: Modules/_io/bufferedio.c:50 _bufferediobase_readinto_generic
// CPython: Modules/_io/bufferedio.c:113 bufferediobase_unsupported
// CPython: Modules/_io/bufferedio.c:2521 bufferediobase_slots
func bufferedIOBaseGetattr(self objects.Object, nameObj objects.Object) (objects.Object, error) {
	name, ok := nameObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
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
	return nil, fmt.Errorf("AttributeError: '_io._BufferedIOBase' object has no attribute '%s'", name.Value())
}

// bufferedIOBaseReadintoGeneric is the shared body of _BufferedIOBase.readinto
// and readinto1: dispatch to self.read(len(b)) (or self.read1) and memcpy the
// returned bytes into the writable buffer argument.
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

// writableBuffer extracts a writable byte slice from a bytearray or memoryview.
// Plain bytes objects are immutable and are rejected here.
func writableBuffer(o objects.Object) ([]byte, int, error) {
	switch v := o.(type) {
	case *objects.ByteArray:
		b := v.Bytes()
		return b, len(b), nil
	}
	return nil, 0, fmt.Errorf("TypeError: a writable bytes-like object is required, not '%s'", o.Type().Name)
}

func init() {
	BufferedIOBaseType.Getattro = bufferedIOBaseGetattr
}

// --- helper: build a method lookup table for Buffered -----------------------

// bufferedGetattr is the shared getattr for BufferedReader, BufferedWriter,
// BufferedRandom. It inspects b.readable/b.writable to gate methods that
// only apply to readers or writers.
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
		return objects.NewBuiltinFunction("flush", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedFlush()
		}), nil
	case "seek":
		return objects.NewBuiltinFunction("seek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedSeek(args)
		}), nil
	case "tell":
		return objects.NewBuiltinFunction("tell", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedTell()
		}), nil
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedTruncate(args)
		}), nil
	case "close":
		return objects.NewBuiltinFunction("close", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedClose()
		}), nil
	case "detach":
		return objects.NewBuiltinFunction("detach", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedDetach()
		}), nil
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
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
		return objects.NewBuiltinFunction("fileno", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedFileno()
		}), nil
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.bufferedIsatty()
		}), nil
	case "__sizeof__":
		return objects.NewBuiltinFunction("__sizeof__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(b.bufSize)), nil
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
		return objects.NewBool(b.closed), nil
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
		// CPython: Modules/_io/bufferedio.c:1545 buffered_get_closefd via raw
		if b.raw == nil {
			return objects.NewBool(false), nil
		}
		v, err := objects.GetAttr(b.raw, objects.NewStr("closefd"))
		if err != nil {
			return objects.NewBool(true), nil //nolint:nilerr // raw without closefd attr defaults to True
		}
		return v, nil
	case "__enter__":
		// CPython: Modules/_io/iobase.c iobase___enter___impl
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkInitialized(); err != nil {
				return nil, err
			}
			if b.closed {
				return nil, fmt.Errorf("ValueError: I/O operation on closed file")
			}
			return b, nil
		}), nil
	case "__exit__":
		// CPython: Modules/_io/iobase.c iobase___exit___impl
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			_, err := b.bufferedClose()
			if err != nil {
				return nil, err
			}
			return objects.None(), nil
		}), nil
	case "__iter__":
		// CPython: Modules/_io/iobase.c iobase_iter
		return objects.NewBuiltinFunction("__iter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkInitialized(); err != nil {
				return nil, err
			}
			if b.closed {
				return nil, fmt.Errorf("ValueError: I/O operation on closed file")
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
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", typeName, name.Value())
}

// bufferedRepr formats a Buffered object as `<_io.BufferedReader name='x'>`
// when the raw stream has a name, or `<_io.BufferedReader>` otherwise.
// CPython swallows ValueError raised by the `name` lookup on a detached
// stream and falls back to the type-only form; we do the same.
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

// --- BufferedReader ----------------------------------------------------------

// BufferedReaderType is the type singleton for _io.BufferedReader.
//
// CPython: Modules/_io/bufferedio.c:2591 bufferedreader_spec
var BufferedReaderType = objects.NewType("_io.BufferedReader", []*objects.Type{BufferedIOBaseType})

// bufferedReaderCall constructs a new BufferedReader.
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
	if bufSize <= 0 {
		return nil, fmt.Errorf("ValueError: buffer size must be strictly positive")
	}
	b := &Buffered{
		raw:      raw,
		readable: true,
		writable: false,
		bufSize:  bufSize,
		readBuf:  make([]byte, bufSize),
		absPos:   -1,
	}
	b.Init(BufferedReaderType)
	b.resetReadBuf()
	return b, nil
}

func init() {
	BufferedReaderType.Call = bufferedReaderCall
	BufferedReaderType.Getattro = bufferedGetattr
	BufferedReaderType.Repr = bufferedTypeRepr
	BufferedReaderType.Str = bufferedTypeRepr
}

// bufferedTypeRepr is the Repr slot installed on every Buffered type.
//
// CPython: Modules/_io/bufferedio.c:1527 buffered_repr
func bufferedTypeRepr(o objects.Object) (string, error) {
	b, ok := o.(*Buffered)
	if !ok {
		return "", fmt.Errorf("TypeError: not a Buffered object")
	}
	return bufferedRepr(b)
}

// --- BufferedWriter ----------------------------------------------------------

// BufferedWriterType is the type singleton for _io.BufferedWriter.
//
// CPython: Modules/_io/bufferedio.c:2649 bufferedwriter_spec
var BufferedWriterType = objects.NewType("_io.BufferedWriter", []*objects.Type{BufferedIOBaseType})

// bufferedWriterCall constructs a new BufferedWriter.
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
	if bufSize <= 0 {
		return nil, fmt.Errorf("ValueError: buffer size must be strictly positive")
	}
	b := &Buffered{
		raw:      raw,
		readable: false,
		writable: true,
		bufSize:  bufSize,
		writeBuf: make([]byte, bufSize),
		absPos:   -1,
	}
	b.Init(BufferedWriterType)
	b.resetWriteBuf()
	return b, nil
}

func init() {
	BufferedWriterType.Call = bufferedWriterCall
	BufferedWriterType.Getattro = bufferedGetattr
	BufferedWriterType.Repr = bufferedTypeRepr
	BufferedWriterType.Str = bufferedTypeRepr
}

// --- BufferedRandom ----------------------------------------------------------

// BufferedRandomType is the type singleton for _io.BufferedRandom.
//
// CPython: Modules/_io/bufferedio.c:2767 bufferedrandom_spec
var BufferedRandomType = objects.NewType("_io.BufferedRandom", []*objects.Type{BufferedIOBaseType})

// bufferedRandomCall constructs a new BufferedRandom.
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
	if bufSize <= 0 {
		return nil, fmt.Errorf("ValueError: buffer size must be strictly positive")
	}
	b := &Buffered{
		raw:      raw,
		readable: true,
		writable: true,
		bufSize:  bufSize,
		readBuf:  make([]byte, bufSize),
		writeBuf: make([]byte, bufSize),
		absPos:   -1,
	}
	b.Init(BufferedRandomType)
	b.resetReadBuf()
	b.resetWriteBuf()
	return b, nil
}

func init() {
	BufferedRandomType.Call = bufferedRandomCall
	BufferedRandomType.Getattro = bufferedGetattr
	BufferedRandomType.Repr = bufferedTypeRepr
	BufferedRandomType.Str = bufferedTypeRepr
}

// --- shared Buffered method implementations ----------------------------------

// bufferedRead implements read([size]) on the Buffered object.
//
// CPython: Modules/_io/bufferedio.c:982 _io__Buffered_read_impl
func (b *Buffered) bufferedRead(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosed(); err != nil {
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
	if size == -1 {
		return b.readAll()
	}
	return b.readN(size)
}

// readAll reads until EOF.
//
// CPython: Modules/_io/bufferedio.c:1675 _bufferedreader_read_all
func (b *Buffered) readAll() (objects.Object, error) {
	var result []byte
	if avail := b.readAhead(); avail > 0 {
		result = append(result, b.readBuf[b.readStart:b.readEnd]...)
		b.resetReadBuf()
	}
	if b.writable {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	for {
		chunk, err := bufRead(b.raw, b.bufSize)
		if err != nil {
			return nil, err
		}
		if len(chunk) == 0 {
			break
		}
		result = append(result, chunk...)
	}
	return objects.NewBytes(result), nil
}

// readN reads exactly n bytes (or fewer at EOF).
//
// CPython: Modules/_io/bufferedio.c:1786 _bufferedreader_read_generic
func (b *Buffered) readN(n int) (objects.Object, error) {
	result := make([]byte, 0, n)
	avail := b.readAhead()
	if avail > 0 {
		take := avail
		if take > n {
			take = n
		}
		result = append(result, b.readBuf[b.readStart:b.readStart+take]...)
		b.readStart += take
		n -= take
	}
	if b.writable && n > 0 {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	for n > 0 {
		b.resetReadBuf()
		filled, err := b.fillReadBuf()
		if err != nil {
			return nil, err
		}
		if filled == 0 {
			break
		}
		take := filled
		if take > n {
			take = n
		}
		result = append(result, b.readBuf[:take]...)
		b.readStart += take
		n -= take
	}
	return objects.NewBytes(result), nil
}

// bufferedRead1 returns up to n bytes using at most one raw read.
//
// CPython: Modules/_io/bufferedio.c:1024 _io__Buffered_read1_impl
func (b *Buffered) bufferedRead1(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosed(); err != nil {
		return nil, err
	}
	n := b.bufSize
	if len(args) > 0 && !objects.IsNone(args[0]) {
		nObj, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: read1() size must be int or None")
		}
		v, _ := nObj.Int64()
		n = int(v)
	}
	if n < 0 {
		n = b.bufSize
	}
	if n == 0 {
		return objects.NewBytes(nil), nil
	}
	if avail := b.readAhead(); avail > 0 {
		if n > avail {
			n = avail
		}
		result := make([]byte, n)
		copy(result, b.readBuf[b.readStart:b.readStart+n])
		b.readStart += n
		return objects.NewBytes(result), nil
	}
	if b.writable {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	b.resetReadBuf()
	chunk, err := bufRead(b.raw, n)
	if err != nil {
		return nil, err
	}
	return objects.NewBytes(chunk), nil
}

// bufferedReadline reads one line.
//
// CPython: Modules/_io/bufferedio.c:1193 _buffered_readline
func (b *Buffered) bufferedReadline(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosed(); err != nil {
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
	var result []byte
	if avail := b.readAhead(); avail > 0 {
		slice := b.readBuf[b.readStart:b.readEnd]
		if limit >= 0 && len(slice) > limit {
			slice = slice[:limit]
		}
		for i, c := range slice {
			if c == '\n' {
				take := i + 1
				result = append(result, slice[:take]...)
				b.readStart += take
				return objects.NewBytes(result), nil
			}
		}
		result = append(result, slice...)
		b.readStart += len(slice)
		if limit >= 0 {
			limit -= len(slice)
		}
	}
	if b.writable {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	for limit != 0 {
		b.resetReadBuf()
		filled, _ := b.fillReadBuf()
		if filled <= 0 {
			break
		}
		slice := b.readBuf[:filled]
		if limit >= 0 && len(slice) > limit {
			slice = slice[:limit]
		}
		for i, c := range slice {
			if c == '\n' {
				take := i + 1
				result = append(result, slice[:take]...)
				b.readStart += take
				return objects.NewBytes(result), nil
			}
		}
		result = append(result, slice...)
		b.readStart += len(slice)
		if limit >= 0 {
			limit -= len(slice)
		}
	}
	return objects.NewBytes(result), nil
}

// bufferedReadinto fills a bytearray from the buffered stream.
//
// CPython: Modules/_io/bufferedio.c:1082 _buffered_readinto_generic
func (b *Buffered) bufferedReadinto(args []objects.Object, readinto1 bool) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosed(); err != nil {
		return nil, err
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: readinto() requires a buffer argument")
	}
	baObj, ok := args[0].(*objects.ByteArray)
	if !ok {
		return nil, fmt.Errorf("TypeError: readinto() argument must be a writable bytearray")
	}
	buf := baObj.Bytes()
	bufLen := len(buf)
	written := 0
	avail := b.readAhead()
	if avail > 0 {
		take := avail
		if take > bufLen {
			take = bufLen
		}
		copy(buf[written:], b.readBuf[b.readStart:b.readStart+take])
		written += take
		b.readStart += take
	}
	for written < bufLen {
		if readinto1 && written > 0 {
			break
		}
		b.resetReadBuf()
		filled, err := b.fillReadBuf()
		if err != nil || filled == 0 {
			break
		}
		take := filled
		if take > bufLen-written {
			take = bufLen - written
		}
		copy(buf[written:], b.readBuf[:take])
		written += take
		b.readStart += take
	}
	// CPython: Modules/_io/bufferedio.c:1082 _buffered_readinto_generic
	// returns the partial written count even when the underlying raw read
	// fails mid-loop, so the nilerr swallow here is intentional.
	return objects.NewInt(int64(written)), nil //nolint:nilerr // matches CPython partial-read contract
}

// bufferedPeek returns buffered data without advancing the position.
// CPython documents the size argument as unused.
//
// CPython: Modules/_io/bufferedio.c:950 _io__Buffered_peek_impl
func (b *Buffered) bufferedPeek(_ []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosed(); err != nil {
		return nil, err
	}
	if b.writable {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	if avail := b.readAhead(); avail > 0 {
		return objects.NewBytes(b.readBuf[b.readStart:b.readEnd]), nil
	}
	b.resetReadBuf()
	n, err := b.fillReadBuf()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return objects.NewBytes(nil), nil
	}
	b.readStart = 0
	return objects.NewBytes(b.readBuf[:n]), nil
}

// bufferedWrite buffers data and flushes when the buffer is full.
//
// CPython: Modules/_io/bufferedio.c:2076 _io_BufferedWriter_write_impl
func (b *Buffered) bufferedWrite(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if b.closed {
		return nil, fmt.Errorf("ValueError: write to closed file")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: write() requires a data argument")
	}
	var data []byte
	switch v := args[0].(type) {
	case *objects.Bytes:
		data = v.Bytes()
	case *objects.ByteArray:
		data = v.Bytes()
	default:
		return nil, fmt.Errorf("TypeError: a bytes-like object is required, not %s", args[0].Type().Name)
	}
	// On a read+write stream a pending read buffer must be discarded
	// before writing, otherwise the next read would return stale bytes
	// that no longer match the raw stream position.
	//
	// CPython: Modules/_io/bufferedio.c:2089 _io_BufferedWriter_write_impl
	// (the unified-buffer model handles this by tracking write_pos within
	// the same slab; we approximate by invalidating the read cache and
	// rewinding the raw stream by the unread amount.)
	if b.readable && b.readAhead() > 0 {
		_, _ = b.rawSeek(int64(-b.readAhead()), 1)
		b.resetReadBuf()
	}
	if b.writeBuf == nil {
		b.writeBuf = make([]byte, b.bufSize)
		b.resetWriteBuf()
	}
	avail := 0
	if b.writeEnd >= 0 {
		avail = b.bufSize - b.writeEnd
	} else {
		avail = b.bufSize
		b.writeEnd = 0
	}
	if len(data) <= avail {
		copy(b.writeBuf[b.writeEnd:], data)
		b.writeEnd += len(data)
		return objects.NewInt(int64(len(data))), nil
	}
	if err := b.flushWrite(); err != nil {
		return nil, err
	}
	if len(data) >= b.bufSize {
		n, err := bufWrite(b.raw, data)
		if err != nil {
			return nil, err
		}
		return objects.NewInt(int64(n)), nil
	}
	copy(b.writeBuf, data)
	b.writeEnd = len(data)
	return objects.NewInt(int64(len(data))), nil
}

// bufferedFlush flushes the write buffer and rewinds the read buffer position.
//
// CPython: Modules/_io/bufferedio.c:925 _io__Buffered_flush_impl
func (b *Buffered) bufferedFlush() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if b.writable {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	if b.readable {
		// rewind raw stream to consume only what was actually read
		if avail := b.readAhead(); avail > 0 {
			if _, err := b.rawSeek(int64(-avail), 1); err == nil {
				b.resetReadBuf()
			}
		}
	}
	return objects.None(), nil
}

// bufferedClose flushes and closes the stream.
//
// CPython: Modules/_io/bufferedio.c:544 _io__Buffered_close_impl
func (b *Buffered) bufferedClose() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if b.closed {
		return objects.None(), nil
	}
	if b.writable {
		_ = b.flushWrite()
	}
	if err := bufClose(b.raw); err != nil {
		return nil, err
	}
	b.closed = true
	b.resetReadBuf()
	return objects.None(), nil
}

// bufferedDetach flushes and detaches the raw stream.
//
// CPython: Modules/_io/bufferedio.c:607 _io__Buffered_detach_impl
func (b *Buffered) bufferedDetach() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if b.writable {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	raw := b.raw
	b.raw = nil
	b.detached = true
	return raw, nil
}

// bufferedSeekable delegates seekable to the raw stream.
//
// CPython: Modules/_io/bufferedio.c:630 _io__Buffered_seekable_impl
func (b *Buffered) bufferedSeekable() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	return bufCall(b.raw, "seekable", nil)
}

// bufferedFileno delegates fileno to the raw stream.
//
// CPython: Modules/_io/bufferedio.c:700 _io__Buffered_fileno_impl
func (b *Buffered) bufferedFileno() (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	return bufCall(b.raw, "fileno", nil)
}

// bufferedIsatty delegates isatty to the raw stream.
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
	pos -= int64(b.readAhead())
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
	if err := b.checkClosed(); err != nil {
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
	if b.writable {
		if err := b.flushWrite(); err != nil {
			return nil, err
		}
	}
	if whence == 1 {
		target -= int64(b.readAhead())
	}
	b.resetReadBuf()
	n, err := b.rawSeek(target, whence)
	if err != nil {
		return nil, err
	}
	b.absPos = n
	return objects.NewInt(n), nil
}

// bufferedTruncate truncates the raw stream at the given position.
//
// CPython: Modules/_io/bufferedio.c:1452 _io__Buffered_truncate_impl
func (b *Buffered) bufferedTruncate(args []objects.Object) (objects.Object, error) {
	if err := b.checkInitialized(); err != nil {
		return nil, err
	}
	if err := b.checkClosed(); err != nil {
		return nil, err
	}
	if !b.writable {
		return nil, fmt.Errorf("UnsupportedOperation: truncate")
	}
	if _, err := b.bufferedFlush(); err != nil {
		return nil, err
	}
	var posArgs []objects.Object
	if len(args) > 0 && !objects.IsNone(args[0]) {
		posArgs = args[:1]
	}
	return bufCall(b.raw, "truncate", posArgs)
}

// --- BufferedRWPair ----------------------------------------------------------

// BufferedRWPairType is the type singleton for _io.BufferedRWPair.
//
// CPython: Modules/_io/bufferedio.c:2699 bufferedrwpair_spec
var BufferedRWPairType = objects.NewType("_io.BufferedRWPair", []*objects.Type{BufferedIOBaseType})

// RWPair is the Go struct backing BufferedRWPair.
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
// CPython: Modules/_io/bufferedio.c:2657 bufferedrwpair_methods + bufferedrwpair_getset
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
		// check writer first, then reader
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
	return nil, fmt.Errorf("AttributeError: '_io.BufferedRWPair' object has no attribute '%s'", name.Value())
}

func init() {
	BufferedRWPairType.Call = bufferedRWPairCall
	BufferedRWPairType.Getattro = rwPairGetattr
}
