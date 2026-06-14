// Package zlib is the gopy port of CPython's zlib C module
// (Modules/zlibmodule.c). It provides one-shot compress/decompress,
// CRC-32/Adler-32 checksums, and streaming compressobj/decompressobj
// backed entirely by Go's standard library (compress/zlib, compress/flate,
// compress/gzip, hash/crc32).
//
// CPython: Modules/zlibmodule.c

package zlib

import (
	"bytes"
	"compress/flate"
	gzip "compress/gzip"
	zstd "compress/zlib"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("zlib", buildModule)
}

// ---------------------------------------------------------------------------
// Constants (mirror CPython's zlibmodule.c and zlib.h).
// ---------------------------------------------------------------------------

const (
	zDefaultCompression = -1
	zNoCompression      = 0
	zBestSpeed          = 1
	zBestCompression    = 9
	zDefaultStrategy    = 0
	zFiltered           = 1
	zHuffmanOnly        = 2
	zSyncFlush          = 2
	zFullFlush          = 3
	zFinish             = 4
	deflated            = 8
	maxWbits            = 15
	defBufSize          = 16384
	defMemLevel         = 8
	zlibVersion         = "1.3.1"
)

// ---------------------------------------------------------------------------
// Compress object type.
// ---------------------------------------------------------------------------

// CompressType is the Python type for streaming compressor objects.
//
// CPython: Modules/zlibmodule.c:222 zlib_compressobj_impl (Comptype)
var CompressType *objects.Type

func init() {
	CompressType = newCompressType()
}

// compressObj is the runtime shape of a zlib.Compress instance.
//
// CPython: Modules/zlibmodule.c:200 compobject (Compress side)
type compressObj struct {
	objects.Header
	level    int
	wbits    int
	strategy int
	buf      bytes.Buffer
	w        io.WriteCloser
	closed   bool
}

func newCompressType() *objects.Type {
	t := objects.NewType("zlib.Compress", []*objects.Type{objects.ObjectType()})
	t.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		return objects.GenericGetAttr(o, name)
	}
	objects.SetTypeDescr(t, "compress", objects.NewMethodDescr(t, "compress", compressObjCompress))
	objects.SetTypeDescr(t, "flush", objects.NewMethodDescr(t, "flush", compressObjFlush))
	objects.SetTypeDescr(t, "copy", objects.NewMethodDescr(t, "copy", compressObjCopy))
	return t
}

// newCompressWriter creates the Go writer for the given wbits value.
// Negative wbits -> raw deflate. wbits > MAX_WBITS (25-31) -> gzip. Otherwise -> zlib.
//
// CPython: Modules/zlibmodule.c:222 zlib_compressobj_impl
func newCompressWriter(buf *bytes.Buffer, level, wbits int) (io.WriteCloser, error) {
	goLevel := cpythonLevelToGo(level)
	if wbits < 0 {
		return flate.NewWriter(buf, goLevel)
	}
	if wbits > maxWbits {
		return gzip.NewWriterLevel(buf, goLevel)
	}
	return zstd.NewWriterLevel(buf, goLevel)
}

// cpythonLevelToGo maps Z_DEFAULT_COMPRESSION (-1) to flate.DefaultCompression.
func cpythonLevelToGo(level int) int {
	if level == zDefaultCompression {
		return flate.DefaultCompression
	}
	return level
}

// compressObjCompress feeds data into the streaming compressor and returns
// any compressed bytes produced so far.
//
// CPython: Modules/zlibmodule.c:414 zlib_Compress_compress_impl
func compressObjCompress(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: compress() takes exactly one argument")
	}
	self, ok := args[0].(*compressObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'compress' requires a 'zlib.Compress' object")
	}
	if self.closed {
		return nil, fmt.Errorf("ValueError: compressor has been flushed")
	}
	data, err := toBytes(args[1])
	if err != nil {
		return nil, err
	}
	if _, err = self.w.Write(data); err != nil {
		return nil, fmt.Errorf("zlib.error: error while compressing data: %w", err)
	}
	out := make([]byte, self.buf.Len())
	copy(out, self.buf.Bytes())
	self.buf.Reset()
	return objects.NewBytes(out), nil
}

// compressObjFlush flushes the streaming compressor.
// mode: Z_SYNC_FLUSH (default), Z_FULL_FLUSH, Z_FINISH.
//
// CPython: Modules/zlibmodule.c:455 zlib_Compress_flush_impl
func compressObjFlush(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: flush() missing self argument")
	}
	self, ok := args[0].(*compressObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'flush' requires a 'zlib.Compress' object")
	}
	if self.closed {
		return nil, fmt.Errorf("ValueError: compressor has already been flushed")
	}

	// flush()'s mode argument defaults to Z_FINISH, not Z_SYNC_FLUSH:
	// the common `compressobj().compress(x) + flush()` idiom must emit a
	// complete stream (final deflate block) so a one-shot decompressor can
	// read it back. zipfile relies on this when it stores compressed
	// members, and zipimport then decompresses them with raw inflate.
	//
	// CPython: Modules/zlibmodule.c:478 zlib_Compress_flush_impl (mode=Z_FINISH)
	mode := zFinish
	if len(args) >= 2 {
		m, err := intFromObj(args[1])
		if err != nil {
			return nil, err
		}
		mode = m
	}

	if mode == zFinish {
		if err := self.w.Close(); err != nil {
			return nil, fmt.Errorf("zlib.error: error while flushing: %w", err)
		}
		self.closed = true
	} else {
		type flusher interface{ Flush() error }
		if f, ok := self.w.(flusher); ok {
			if err := f.Flush(); err != nil {
				return nil, fmt.Errorf("zlib.error: error while flushing: %w", err)
			}
		}
	}

	out := self.buf.Bytes()
	result := make([]byte, len(out))
	copy(result, out)
	self.buf.Reset()
	return objects.NewBytes(result), nil
}

// compressObjCopy returns a new compressor with the same configuration.
// Go's streaming writers don't expose state cloning, so only parameters
// are preserved.
//
// CPython: Modules/zlibmodule.c:504 zlib_Compress_copy_impl
func compressObjCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: copy() missing self argument")
	}
	src, ok := args[0].(*compressObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'copy' requires a 'zlib.Compress' object")
	}
	return newCompressObjWith(src.level, src.wbits, src.strategy)
}

// newCompressObjWith allocates a compressObj with the given parameters.
func newCompressObjWith(level, wbits, strategy int) (*compressObj, error) {
	obj := &compressObj{
		level:    level,
		wbits:    wbits,
		strategy: strategy,
	}
	obj.Init(CompressType)
	w, err := newCompressWriter(&obj.buf, level, wbits)
	if err != nil {
		return nil, fmt.Errorf("zlib.error: error initializing compressor: %w", err)
	}
	obj.w = w
	return obj, nil
}

// ---------------------------------------------------------------------------
// Decompress object type.
// ---------------------------------------------------------------------------

// DecompressType is the Python type for streaming decompressor objects.
//
// CPython: Modules/zlibmodule.c:353 zlib_decompressobj_impl (Decomptype)
var DecompressType *objects.Type

func init() {
	DecompressType = newDecompressType()
}

// decompressObj is the runtime shape of a zlib.Decompress instance.
//
// CPython: Modules/zlibmodule.c:200 compobject (Decompress side)
type decompressObj struct {
	objects.Header
	wbits      int
	zdict      []byte
	unusedData []byte
	unconsumed []byte
	eof        bool
	inputBuf   []byte
}

func newDecompressType() *objects.Type {
	t := objects.NewType("zlib.Decompress", []*objects.Type{objects.ObjectType()})
	t.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		return objects.GenericGetAttr(o, name)
	}
	objects.SetTypeDescr(t, "decompress", objects.NewMethodDescr(t, "decompress", decompressObjDecompress))
	objects.SetTypeDescr(t, "flush", objects.NewMethodDescr(t, "flush", decompressObjFlush))
	objects.SetTypeDescr(t, "copy", objects.NewMethodDescr(t, "copy", decompressObjCopy))
	objects.SetTypeDescr(t, "unused_data", objects.NewGetSetDescr(
		"unused_data",
		func(o objects.Object) (objects.Object, error) {
			d := o.(*decompressObj).unusedData
			if d == nil {
				d = []byte{}
			}
			return objects.NewBytes(d), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "unconsumed_tail", objects.NewGetSetDescr(
		"unconsumed_tail",
		func(o objects.Object) (objects.Object, error) {
			d := o.(*decompressObj).unconsumed
			if d == nil {
				d = []byte{}
			}
			return objects.NewBytes(d), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "eof", objects.NewGetSetDescr(
		"eof",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewBool(o.(*decompressObj).eof), nil
		},
		nil,
	))
	return t
}

// decompressChunk decompresses src using wbits semantics.
func decompressChunk(wbits int, src []byte) ([]byte, error) {
	if wbits < 0 {
		r := flate.NewReader(bytes.NewReader(src))
		defer r.Close()
		return io.ReadAll(r)
	}
	if wbits > maxWbits {
		gr, err := gzip.NewReader(bytes.NewReader(src))
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		return io.ReadAll(gr)
	}
	zr, err := zstd.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// decompressObjDecompress decompresses data through the streaming decompressor.
// max_length limits output (0 = unlimited).
//
// CPython: Modules/zlibmodule.c:305 zlib_Decompress_decompress_impl
func decompressObjDecompress(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: decompress() takes at least one argument")
	}
	self, ok := args[0].(*decompressObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'decompress' requires a 'zlib.Decompress' object")
	}
	if self.eof {
		return nil, fmt.Errorf("EOFError: end of stream already reached")
	}
	data, err := toBytes(args[1])
	if err != nil {
		return nil, err
	}

	maxLength := 0
	if len(args) >= 3 {
		maxLength, err = intFromObj(args[2])
		if err != nil {
			return nil, err
		}
	}

	self.inputBuf = append(self.inputBuf, data...)

	out, err := decompressChunk(self.wbits, self.inputBuf)
	if err != nil {
		return nil, fmt.Errorf("zlib.error: error -3 while decompressing data: %w", err)
	}

	self.eof = true
	self.inputBuf = nil
	self.unusedData = []byte{}
	self.unconsumed = []byte{}

	if maxLength > 0 && len(out) > maxLength {
		tail := make([]byte, len(out)-maxLength)
		copy(tail, out[maxLength:])
		self.unconsumed = tail
		self.eof = false
		out = out[:maxLength]
	}

	return objects.NewBytes(out), nil
}

// decompressObjFlush returns any remaining buffered output.
// The Go-backed implementation processes data eagerly, so this always
// returns empty bytes.
//
// CPython: Modules/zlibmodule.c:344 zlib_Decompress_flush_impl
func decompressObjFlush(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: flush() missing self argument")
	}
	if _, ok := args[0].(*decompressObj); !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'flush' requires a 'zlib.Decompress' object")
	}
	return objects.NewBytes([]byte{}), nil
}

// decompressObjCopy returns a copy of the decompressor.
//
// CPython: Modules/zlibmodule.c:499 zlib_Decompress_copy_impl
func decompressObjCopy(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: copy() missing self argument")
	}
	src, ok := args[0].(*decompressObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'copy' requires a 'zlib.Decompress' object")
	}
	dst := &decompressObj{
		wbits:      src.wbits,
		eof:        src.eof,
		unusedData: append([]byte(nil), src.unusedData...),
		unconsumed: append([]byte(nil), src.unconsumed...),
		inputBuf:   append([]byte(nil), src.inputBuf...),
	}
	if src.zdict != nil {
		dst.zdict = append([]byte(nil), src.zdict...)
	}
	dst.Init(DecompressType)
	return dst, nil
}

// ---------------------------------------------------------------------------
// Module-level functions.
// ---------------------------------------------------------------------------

// zlibCompress compresses data in one shot.
// compress(data, level=Z_DEFAULT_COMPRESSION, wbits=MAX_WBITS) -> bytes
//
// CPython: Modules/zlibmodule.c:77 zlib_compress_impl
func zlibCompress(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: compress() requires at least 1 argument")
	}
	data, err := toBytes(args[0])
	if err != nil {
		return nil, err
	}

	level := zDefaultCompression
	if len(args) >= 2 {
		level, err = intFromObj(args[1])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["level"]; ok {
		level, err = intFromObj(v)
		if err != nil {
			return nil, err
		}
	}

	wbits := maxWbits
	if len(args) >= 3 {
		wbits, err = intFromObj(args[2])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["wbits"]; ok {
		wbits, err = intFromObj(v)
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	w, err := newCompressWriter(&buf, level, wbits)
	if err != nil {
		return nil, fmt.Errorf("zlib.error: error initializing compressor: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("zlib.error: error while compressing data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("zlib.error: error while finishing compression: %w", err)
	}
	return objects.NewBytes(buf.Bytes()), nil
}

// zlibDecompress decompresses data in one shot.
// decompress(data, wbits=MAX_WBITS, bufsize=DEF_BUF_SIZE) -> bytes
//
// CPython: Modules/zlibmodule.c:122 zlib_decompress_impl
func zlibDecompress(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: decompress() requires at least 1 argument")
	}
	data, err := toBytes(args[0])
	if err != nil {
		return nil, err
	}

	wbits := maxWbits
	if len(args) >= 2 {
		wbits, err = intFromObj(args[1])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["wbits"]; ok {
		wbits, err = intFromObj(v)
		if err != nil {
			return nil, err
		}
	}

	out, err := decompressChunk(wbits, data)
	if err != nil {
		return nil, fmt.Errorf("zlib.error: error while decompressing data: %w", err)
	}
	return objects.NewBytes(out), nil
}

// zlibCRC32 computes the CRC-32 checksum, optionally updating a previous value.
// crc32(data, value=0) -> int
//
// CPython: Modules/zlibmodule.c:556 zlib_crc32_impl
func zlibCRC32(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: crc32() requires at least 1 argument")
	}
	data, err := toBytes(args[0])
	if err != nil {
		return nil, err
	}

	prev := uint32(0)
	if len(args) >= 2 {
		v, err := intFromObj(args[1])
		if err != nil {
			return nil, err
		}
		prev = uint32(v)
	} else if v, ok := kwargs["value"]; ok {
		vi, err := intFromObj(v)
		if err != nil {
			return nil, err
		}
		prev = uint32(vi)
	}

	result := crc32.Update(prev, crc32.IEEETable, data)
	// CPython returns the checksum as an unsigned 32-bit value widened to
	// a Python int (PyLong_FromUnsignedLong(value & 0xffffffffU)).
	//
	// CPython: Modules/zlibmodule.c:1901 zlib_crc32_impl
	return objects.NewInt(int64(uint64(result))), nil
}

// zlibAdler32 computes the Adler-32 checksum, optionally updating a previous value.
// adler32(data, value=1) -> int
//
// CPython: Modules/zlibmodule.c:527 zlib_adler32_impl
func zlibAdler32(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: adler32() requires at least 1 argument")
	}
	data, err := toBytes(args[0])
	if err != nil {
		return nil, err
	}

	prev := uint32(1)
	if len(args) >= 2 {
		v, err := intFromObj(args[1])
		if err != nil {
			return nil, err
		}
		prev = uint32(v)
	} else if v, ok := kwargs["value"]; ok {
		vi, err := intFromObj(v)
		if err != nil {
			return nil, err
		}
		prev = uint32(vi)
	}

	result := adler32Update(prev, data)
	// CPython returns the checksum as an unsigned 32-bit value widened to
	// a Python int (PyLong_FromUnsignedLong(value & 0xffffffffU)).
	//
	// CPython: Modules/zlibmodule.c:1901 zlib_adler32_impl
	return objects.NewInt(int64(uint64(result))), nil
}

// zlibCompressobj returns a streaming Compress object.
// compressobj(level=-1, method=DEFLATED, wbits=MAX_WBITS, memLevel=DEF_MEM_LEVEL, strategy=Z_DEFAULT_STRATEGY, zdict=b"") -> Compress
//
// CPython: Modules/zlibmodule.c:222 zlib_compressobj_impl
func zlibCompressobj(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var err error

	level := zDefaultCompression
	if len(args) >= 1 {
		level, err = intFromObj(args[0])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["level"]; ok {
		level, err = intFromObj(v)
		if err != nil {
			return nil, err
		}
	}

	// args[1] is method (DEFLATED only) - validated but not used in Go backend.

	wbits := maxWbits
	if len(args) >= 3 {
		wbits, err = intFromObj(args[2])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["wbits"]; ok {
		wbits, err = intFromObj(v)
		if err != nil {
			return nil, err
		}
	}

	strategy := zDefaultStrategy
	if len(args) >= 5 {
		strategy, err = intFromObj(args[4])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["strategy"]; ok {
		strategy, err = intFromObj(v)
		if err != nil {
			return nil, err
		}
	}

	return newCompressObjWith(level, wbits, strategy)
}

// zlibDecompressobj returns a streaming Decompress object.
// decompressobj(wbits=MAX_WBITS, zdict=b"") -> Decompress
//
// CPython: Modules/zlibmodule.c:353 zlib_decompressobj_impl
func zlibDecompressobj(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var err error

	wbits := maxWbits
	if len(args) >= 1 {
		wbits, err = intFromObj(args[0])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["wbits"]; ok {
		wbits, err = intFromObj(v)
		if err != nil {
			return nil, err
		}
	}

	var zdict []byte
	if len(args) >= 2 {
		zdict, err = toBytes(args[1])
		if err != nil {
			return nil, err
		}
	} else if v, ok := kwargs["zdict"]; ok {
		zdict, err = toBytes(v)
		if err != nil {
			return nil, err
		}
	}

	obj := &decompressObj{
		wbits:      wbits,
		zdict:      zdict,
		unusedData: []byte{},
		unconsumed: []byte{},
	}
	obj.Init(DecompressType)
	return obj, nil
}

// ---------------------------------------------------------------------------
// Module builder.
// ---------------------------------------------------------------------------

// buildModule materializes the zlib module dict.
//
// CPython: Modules/zlibmodule.c:600 zlib_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("zlib")
	d := m.Dict()

	setObj := func(name string, val objects.Object) error {
		return d.SetItem(objects.NewStr(name), val)
	}
	setInt := func(name string, v int) error {
		return setObj(name, objects.NewInt(int64(v)))
	}
	setStr := func(name string, v string) error {
		return setObj(name, objects.NewStr(v))
	}

	fns := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"compress", zlibCompress},
		{"decompress", zlibDecompress},
		{"crc32", zlibCRC32},
		{"adler32", zlibAdler32},
		{"compressobj", zlibCompressobj},
		{"decompressobj", zlibDecompressobj},
	}
	for _, e := range fns {
		if err := setObj(e.name, objects.NewBuiltinFunction(e.name, e.fn)); err != nil {
			return nil, err
		}
	}

	intConsts := []struct {
		name string
		val  int
	}{
		{"Z_DEFAULT_COMPRESSION", zDefaultCompression},
		{"Z_NO_COMPRESSION", zNoCompression},
		{"Z_BEST_SPEED", zBestSpeed},
		{"Z_BEST_COMPRESSION", zBestCompression},
		{"Z_DEFAULT_STRATEGY", zDefaultStrategy},
		{"Z_FILTERED", zFiltered},
		{"Z_HUFFMAN_ONLY", zHuffmanOnly},
		{"Z_SYNC_FLUSH", zSyncFlush},
		{"Z_FULL_FLUSH", zFullFlush},
		{"Z_FINISH", zFinish},
		{"DEFLATED", deflated},
		{"MAX_WBITS", maxWbits},
		{"DEF_BUF_SIZE", defBufSize},
		{"DEF_MEM_LEVEL", defMemLevel},
	}
	for _, c := range intConsts {
		if err := setInt(c.name, c.val); err != nil {
			return nil, err
		}
	}

	if err := setStr("ZLIB_VERSION", zlibVersion); err != nil {
		return nil, err
	}
	if err := setStr("ZLIB_RUNTIME_VERSION", zlibVersion); err != nil {
		return nil, err
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// intFromObj extracts a Go int from a Python Int object.
func intFromObj(o objects.Object) (int, error) {
	i, ok := o.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: an integer is required, not '%T'", o)
	}
	v, fits := i.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to int")
	}
	return int(v), nil
}

// adler32Update computes Adler-32 starting from a previous accumulator value.
// Go's hash/adler32 package only provides Checksum (seeded from 1), so we
// replicate the RFC 1950 update loop directly.
const adler32Mod = 65521

func adler32Update(prev uint32, data []byte) uint32 {
	s1 := prev & 0xffff
	s2 := (prev >> 16) & 0xffff
	for _, b := range data {
		s1 = (s1 + uint32(b)) % adler32Mod
		s2 = (s2 + s1) % adler32Mod
	}
	return (s2 << 16) | s1
}

// toBytes extracts a []byte from a bytes or str Python object.
func toBytes(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	case *objects.Unicode:
		s, err := objects.Str(o)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%T'", o)
}
