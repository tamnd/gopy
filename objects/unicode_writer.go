// UnicodeWriter is gopy's port of CPython's _PyUnicodeWriter struct and
// the 13 _PyUnicodeWriter_* helpers that build str values incrementally.
// CPython's writer flips between three character widths (1, 2, 4 bytes)
// and grows the underlying buffer with an overallocation factor. The
// gopy Unicode type is backed by a canonical UTF-8 Go string, so the
// writer here accumulates UTF-8 bytes in a []byte, tracks codepoint
// count and maxchar, and reproduces CPython's readonly / overallocate
// short-circuits. Finish returns a *Unicode whose length / kind / ascii
// fields are filled in without a second walk.
//
// CPython: Objects/unicodeobject.c:13712 _PyUnicodeWriter_Update
// CPython: Objects/unicodeobject.c:13736 _PyUnicodeWriter_Init
// CPython: Objects/unicodeobject.c:13793 _PyUnicodeWriter_InitWithBuffer
// CPython: Objects/unicodeobject.c:13803 _PyUnicodeWriter_PrepareInternal
// CPython: Objects/unicodeobject.c:13881 _PyUnicodeWriter_PrepareKindInternal
// CPython: Objects/unicodeobject.c:13902 _PyUnicodeWriter_WriteCharInline
// CPython: Objects/unicodeobject.c:13913 _PyUnicodeWriter_WriteChar
// CPython: Objects/unicodeobject.c:13931 _PyUnicodeWriter_WriteStr
// CPython: Objects/unicodeobject.c:14006 _PyUnicodeWriter_WriteSubstring
// CPython: Objects/unicodeobject.c:14062 _PyUnicodeWriter_WriteASCIIString
// CPython: Objects/unicodeobject.c:14185 _PyUnicodeWriter_WriteLatin1String
// CPython: Objects/unicodeobject.c:14199 _PyUnicodeWriter_Finish
// CPython: Objects/unicodeobject.c:14242 _PyUnicodeWriter_Dealloc

package objects

import (
	"fmt"
	"unicode/utf8"
)

// MaxUnicodeChar is the largest valid Unicode scalar value.
//
// CPython: Objects/unicodeobject.c:107 MAX_UNICODE
const MaxUnicodeChar rune = 0x10ffff

// unicodeWriterOverallocFactor mirrors CPython's OVERALLOCATE_FACTOR on
// non-Windows builds (25% overallocation on growth).
//
// CPython: Objects/unicodeobject.c:228 OVERALLOCATE_FACTOR (Linux)
const unicodeWriterOverallocFactor = 4

// UnicodeWriter is the streaming str builder. Field semantics mirror
// CPython's _PyUnicodeWriter; storage is UTF-8 instead of kind-tagged
// buckets.
//
// CPython: Include/cpython/unicodeobject.h _PyUnicodeWriter
type UnicodeWriter struct {
	// alias is set when the writer is aliased to an existing Unicode in
	// readonly / copy-on-write mode. Finish returns this directly when
	// no growth has happened, matching CPython's Py_NewRef shortcut.
	alias *Unicode

	// buf is the UTF-8 byte accumulator once a private buffer exists.
	buf []byte

	// pos is the codepoint count written so far (CPython's writer.pos
	// indexes a kind-tagged buffer; here it counts runes since the
	// underlying bytes are variable-width UTF-8).
	pos int

	// size is the codepoint capacity hint. CPython uses size to drive
	// the "fits in current buffer" check; the port keeps the same
	// invariant of comparing pos+length against size in PrepareInternal.
	size int

	// maxchar is the widest codepoint written so far. CPython initial
	// value is 0 (effectively); we mirror that.
	maxchar rune

	// minChar floors maxchar growth (Init sets it to 127 so a string
	// of ASCII writes keeps maxchar at 127 and kind at 1, matching the
	// CPython default writer.min_char = 127).
	minChar rune

	// minLength floors size growth; populated by InitWithBuffer so a
	// pre-sized writer never re-shrinks below its initial buffer.
	minLength int

	overallocate bool
	readonly     bool

	// kind is the PEP 393 kind tag (1/2/4). Re-derived from maxchar so
	// callers reading writer.kind see consistent values.
	kind byte
}

// NewUnicodeWriter mirrors PyUnicodeWriter_Create: allocate, init, and
// pre-size for `length` codepoints with overallocate=1.
//
// CPython: Objects/unicodeobject.c:13751 PyUnicodeWriter_Create
func NewUnicodeWriter(length int) (*UnicodeWriter, error) {
	if length < 0 {
		return nil, fmt.Errorf("ValueError: length must be positive")
	}
	w := &UnicodeWriter{}
	w.Init()
	if err := w.Prepare(length, 127); err != nil {
		return nil, err
	}
	w.overallocate = true
	return w, nil
}

// Init zeros the writer and seeds minChar=127. Mirrors
// _PyUnicodeWriter_Init: a fresh writer accumulates ASCII at kind=1
// until something wider is written.
//
// CPython: Objects/unicodeobject.c:13736 _PyUnicodeWriter_Init
func (w *UnicodeWriter) Init() {
	*w = UnicodeWriter{}
	w.minChar = 127
	// kind == 0 < PyUnicode_1BYTE_KIND so PrepareKindInternal triggers
	// a buffer copy on the first widening write, exactly as CPython does.
	w.kind = 0
}

// InitWithBuffer aliases an existing *Unicode for further writes; the
// writer enters readonly / copy-on-write mode. min_length records the
// alias length so PrepareInternal never shrinks below it.
//
// CPython: Objects/unicodeobject.c:13793 _PyUnicodeWriter_InitWithBuffer
func (w *UnicodeWriter) InitWithBuffer(buffer *Unicode) {
	*w = UnicodeWriter{}
	w.alias = buffer
	w.update()
	w.minLength = w.size
}

// update refreshes maxchar / kind / size / data fields from the
// current buffer. Mirrors _PyUnicodeWriter_Update.
//
// CPython: Objects/unicodeobject.c:13712 _PyUnicodeWriter_Update
func (w *UnicodeWriter) update() {
	if w.alias != nil {
		w.maxchar = unicodeMaxCharFromKind(w.alias.kind, w.alias.ascii)
		if !w.readonly {
			// CPython tracks size as the codepoint capacity of the
			// owned buffer. When alias is set but readonly hasn't been
			// flagged yet (i.e. callers wrote through InitWithBuffer),
			// the alias acts as the owned buffer.
			w.kind = w.alias.kind
			w.size = w.alias.length
			w.pos = w.alias.length
		} else {
			// readonly: smaller-than-1 kind so the next PrepareKind
			// forces a copy.
			w.kind = 0
			w.size = 0
		}
		return
	}
	// Plain buf-backed mode. We compute maxchar lazily from the rune
	// already written. size tracks the codepoint capacity of buf.
	w.kind = unicodeKindForMaxChar(w.maxchar)
	// Note: w.size is updated by PrepareInternal when buf grows.
}

// unicodeMaxCharFromKind returns the PEP 393 max codepoint implied by
// kind / ascii.
//
// CPython: Include/cpython/unicodeobject.h PyUnicode_MAX_CHAR_VALUE
func unicodeMaxCharFromKind(kind byte, ascii bool) rune {
	switch kind {
	case StrKind1Byte:
		if ascii {
			return 0x7f
		}
		return 0xff
	case StrKind2Byte:
		return 0xffff
	case StrKind4Byte:
		return MaxUnicodeChar
	}
	return 0
}

// unicodeKindForMaxChar mirrors PyUnicode_New's kind selection.
//
// CPython: Objects/unicodeobject.c:L1696 find_maxchar_surrogates
func unicodeKindForMaxChar(mc rune) byte {
	switch {
	case mc < 0x100:
		return StrKind1Byte
	case mc < 0x10000:
		return StrKind2Byte
	default:
		return StrKind4Byte
	}
}

// Prepare wraps PrepareInternal with the macro-style early-exit
// _PyUnicodeWriter_Prepare uses (skip the call when the request fits
// within the existing buffer and maxchar).
//
// CPython: Include/internal/pycore_unicodeobject.h _PyUnicodeWriter_Prepare
func (w *UnicodeWriter) Prepare(length int, maxchar rune) error {
	if maxchar <= w.maxchar && length <= w.size-w.pos {
		return nil
	}
	return w.PrepareInternal(length, maxchar)
}

// PrepareInternal grows the buffer (and widens kind) to accommodate
// `length` more codepoints with the given maxchar.
//
// CPython: Objects/unicodeobject.c:13803 _PyUnicodeWriter_PrepareInternal
func (w *UnicodeWriter) PrepareInternal(length int, maxchar rune) error {
	if length < 0 {
		return fmt.Errorf("SystemError: negative length")
	}
	if maxchar > MaxUnicodeChar {
		return fmt.Errorf("SystemError: maxchar out of range")
	}
	if length > (1<<62)-w.pos {
		return fmt.Errorf("MemoryError")
	}
	newlen := w.pos + length
	if maxchar < w.minChar {
		maxchar = w.minChar
	}

	if w.alias == nil && w.buf == nil {
		// No buffer yet: allocate.
		if w.overallocate && newlen <= (1<<62)-newlen/unicodeWriterOverallocFactor {
			newlen += newlen / unicodeWriterOverallocFactor
		}
		if newlen < w.minLength {
			newlen = w.minLength
		}
		w.buf = make([]byte, 0, byteCapForCodepoints(newlen, maxchar))
		w.size = newlen
		if maxchar > w.maxchar {
			w.maxchar = maxchar
		}
		w.update()
		return nil
	}

	if newlen > w.size {
		// Need a wider buffer or more capacity.
		if w.overallocate && newlen <= (1<<62)-newlen/unicodeWriterOverallocFactor {
			newlen += newlen / unicodeWriterOverallocFactor
		}
		if newlen < w.minLength {
			newlen = w.minLength
		}
		if w.readonly || w.alias != nil {
			// Promote alias to owned buf, widening if needed.
			if maxchar < w.maxchar {
				maxchar = w.maxchar
			}
			src := []byte(w.alias.v)
			b := make([]byte, len(src), byteCapForCodepoints(newlen, maxchar))
			copy(b, src)
			w.buf = b
			w.pos = w.alias.length
			w.alias = nil
			w.readonly = false
		} else {
			// Grow owned buf.
			if maxchar < w.maxchar {
				maxchar = w.maxchar
			}
			cap := byteCapForCodepoints(newlen, maxchar)
			if cap > len(w.buf) {
				b := make([]byte, len(w.buf), cap)
				copy(b, w.buf)
				w.buf = b
			}
		}
		w.size = newlen
		if maxchar > w.maxchar {
			w.maxchar = maxchar
		}
		w.update()
		return nil
	}

	if maxchar > w.maxchar {
		// Same size, just widen kind. UTF-8 storage doesn't require a
		// re-encode, so just bump maxchar/kind.
		if w.alias != nil {
			// Aliased + widening: copy out.
			src := []byte(w.alias.v)
			b := make([]byte, len(src), byteCapForCodepoints(w.size, maxchar))
			copy(b, src)
			w.buf = b
			w.pos = w.alias.length
			w.alias = nil
			w.readonly = false
		}
		w.maxchar = maxchar
		w.update()
	}
	return nil
}

// byteCapForCodepoints returns a UTF-8 byte capacity for the given
// codepoint count. Worst case is 4 bytes/codepoint at maxchar above
// the BMP; tighter bounds for narrower runs.
func byteCapForCodepoints(n int, maxchar rune) int {
	switch {
	case maxchar < 0x80:
		return n
	case maxchar < 0x800:
		return n * 2
	case maxchar < 0x10000:
		return n * 3
	default:
		return n * 4
	}
}

// PrepareKindInternal widens kind to the given PEP 393 level. The
// public _PyUnicodeWriter_PrepareKind macro filters by writer->kind <
// kind before dispatching.
//
// CPython: Objects/unicodeobject.c:13881 _PyUnicodeWriter_PrepareKindInternal
func (w *UnicodeWriter) PrepareKindInternal(kind byte) error {
	var mc rune
	switch kind {
	case StrKind1Byte:
		mc = 0xff
	case StrKind2Byte:
		mc = 0xffff
	case StrKind4Byte:
		mc = MaxUnicodeChar
	default:
		return fmt.Errorf("SystemError: invalid PEP 393 kind %d", kind)
	}
	return w.PrepareInternal(0, mc)
}

// WriteChar appends a single codepoint. Mirrors
// _PyUnicodeWriter_WriteCharInline + _PyUnicodeWriter_WriteChar.
//
// CPython: Objects/unicodeobject.c:13902 _PyUnicodeWriter_WriteCharInline
// CPython: Objects/unicodeobject.c:13913 _PyUnicodeWriter_WriteChar
func (w *UnicodeWriter) WriteChar(ch rune) error {
	if ch > MaxUnicodeChar {
		return fmt.Errorf("ValueError: character must be in range(0x110000)")
	}
	if err := w.Prepare(1, ch); err != nil {
		return err
	}
	if w.alias != nil {
		// PrepareInternal must have promoted alias to buf when
		// growth/widening was needed. If we land here with alias still
		// set, the char fits without growth and the alias path stays.
		// But to append, we still must materialize.
		w.materializeAlias()
	}
	w.buf = utf8.AppendRune(w.buf, ch)
	w.pos++
	if ch > w.maxchar {
		w.maxchar = ch
		w.kind = unicodeKindForMaxChar(w.maxchar)
	}
	return nil
}

// materializeAlias copies the aliased Unicode into a private buf.
// Called when a write demands appending bytes (alias-mode buffers are
// read-only).
func (w *UnicodeWriter) materializeAlias() {
	if w.alias == nil {
		return
	}
	src := []byte(w.alias.v)
	cap := max(byteCapForCodepoints(w.size, w.maxchar), len(src))
	b := make([]byte, len(src), cap)
	copy(b, src)
	w.buf = b
	w.pos = w.alias.length
	w.alias = nil
	w.readonly = false
}

// WriteStr appends a *Unicode. Honors CPython's readonly fast path:
// when the writer has no buffer yet and !overallocate, the writer
// aliases str directly.
//
// CPython: Objects/unicodeobject.c:13931 _PyUnicodeWriter_WriteStr
func (w *UnicodeWriter) WriteStr(s *Unicode) error {
	if s == nil {
		return fmt.Errorf("SystemError: WriteStr nil str")
	}
	length := s.length
	if length == 0 {
		return nil
	}
	maxchar := unicodeMaxCharFromKind(s.kind, s.ascii)
	if maxchar > w.maxchar || length > w.size-w.pos {
		if w.alias == nil && w.buf == nil && !w.overallocate {
			// Readonly fast path: alias the source string.
			w.readonly = true
			w.alias = s
			w.update()
			w.pos += length
			return nil
		}
		if err := w.PrepareInternal(length, maxchar); err != nil {
			return err
		}
	}
	if w.alias != nil {
		w.materializeAlias()
	}
	w.buf = append(w.buf, s.v...)
	w.pos += length
	if maxchar > w.maxchar {
		w.maxchar = maxchar
		w.kind = unicodeKindForMaxChar(w.maxchar)
	}
	return nil
}

// WriteSubstring appends s[start:end] (codepoint indices). Mirrors
// _PyUnicodeWriter_WriteSubstring.
//
// CPython: Objects/unicodeobject.c:14006 _PyUnicodeWriter_WriteSubstring
func (w *UnicodeWriter) WriteSubstring(s *Unicode, start, end int) error {
	if start < 0 || start > end || end > s.length {
		return fmt.Errorf("ValueError: invalid substring bounds")
	}
	if start == 0 && end == s.length {
		return w.WriteStr(s)
	}
	length := end - start
	if length == 0 {
		return nil
	}
	srcMax := unicodeMaxCharFromKind(s.kind, s.ascii)
	var maxchar rune
	if srcMax > w.maxchar {
		maxchar = unicodeFindMaxChar(s.v, start, end)
	} else {
		maxchar = w.maxchar
	}
	if err := w.Prepare(length, maxchar); err != nil {
		return err
	}
	if w.alias != nil {
		w.materializeAlias()
	}
	// Slice the substring out of s.v by walking codepoints.
	sb, se := codepointByteRange(s.v, start, end)
	w.buf = append(w.buf, s.v[sb:se]...)
	w.pos += length
	if maxchar > w.maxchar {
		w.maxchar = maxchar
		w.kind = unicodeKindForMaxChar(w.maxchar)
	}
	return nil
}

// codepointByteRange converts codepoint indices into byte offsets in a
// UTF-8 string. ASCII strings map 1:1.
func codepointByteRange(s string, start, end int) (int, int) {
	bi, cp := 0, 0
	sb, se := -1, -1
	for bi < len(s) {
		if cp == start {
			sb = bi
		}
		if cp == end {
			se = bi
			break
		}
		_, n := utf8.DecodeRuneInString(s[bi:])
		bi += n
		cp++
	}
	if sb < 0 && cp == start {
		sb = bi
	}
	if se < 0 && cp == end {
		se = bi
	}
	if sb < 0 {
		sb = bi
	}
	if se < 0 {
		se = bi
	}
	return sb, se
}

// unicodeFindMaxChar returns the widest codepoint in s[start:end].
// Mirrors _PyUnicode_FindMaxChar.
//
// CPython: Objects/unicodeobject.c:2446 _PyUnicode_FindMaxChar
func unicodeFindMaxChar(s string, start, end int) rune {
	if start == end {
		return 127
	}
	sb, se := codepointByteRange(s, start, end)
	var maxr rune
	for _, r := range s[sb:se] {
		if r > maxr {
			maxr = r
		}
	}
	if maxr < 127 {
		return 127
	}
	return maxr
}

// WriteASCIIString appends an ASCII-only Go string. CPython takes
// `len=-1` to mean strlen; the Go caller passes len(ascii) or -1.
//
// CPython: Objects/unicodeobject.c:14062 _PyUnicodeWriter_WriteASCIIString
func (w *UnicodeWriter) WriteASCIIString(ascii string, length int) error {
	if length < 0 {
		length = len(ascii)
	}
	if length == 0 {
		return nil
	}
	if length > len(ascii) {
		return fmt.Errorf("SystemError: length exceeds ascii buffer")
	}
	// Caller contract: ascii[:length] is ASCII-only.
	for i := 0; i < length; i++ {
		if ascii[i] >= 0x80 {
			return fmt.Errorf("SystemError: WriteASCIIString got non-ASCII byte 0x%02x", ascii[i])
		}
	}
	if w.alias == nil && w.buf == nil && !w.overallocate {
		// Promote to a freshly-allocated str alias-equivalent.
		s := NewStr(ascii[:length]).(*Unicode)
		w.alias = s
		w.readonly = true
		w.update()
		w.pos += length
		return nil
	}
	if err := w.Prepare(length, 127); err != nil {
		return err
	}
	if w.alias != nil {
		w.materializeAlias()
	}
	w.buf = append(w.buf, ascii[:length]...)
	w.pos += length
	if w.maxchar < 127 {
		w.maxchar = 127
		w.kind = StrKind1Byte
	}
	return nil
}

// WriteLatin1String appends a Latin-1 (0x00..0xFF) byte run. Each byte
// becomes one codepoint; bytes >=0x80 widen kind to non-ASCII.
//
// CPython: Objects/unicodeobject.c:14185 _PyUnicodeWriter_WriteLatin1String
func (w *UnicodeWriter) WriteLatin1String(s string, length int) error {
	if length < 0 {
		length = len(s)
	}
	if length == 0 {
		return nil
	}
	if length > len(s) {
		return fmt.Errorf("SystemError: length exceeds latin1 buffer")
	}
	var maxr rune
	for i := 0; i < length; i++ {
		r := rune(s[i])
		if r > maxr {
			maxr = r
		}
	}
	if err := w.Prepare(length, maxr); err != nil {
		return err
	}
	if w.alias != nil {
		w.materializeAlias()
	}
	for i := 0; i < length; i++ {
		w.buf = utf8.AppendRune(w.buf, rune(s[i]))
	}
	w.pos += length
	if maxr > w.maxchar {
		w.maxchar = maxr
		w.kind = unicodeKindForMaxChar(w.maxchar)
	}
	return nil
}

// Finish closes the writer and returns the accumulated *Unicode.
// Mirrors _PyUnicodeWriter_Finish: empty -> "", readonly alias -> alias,
// else build a Unicode from buf.
//
// CPython: Objects/unicodeobject.c:14199 _PyUnicodeWriter_Finish
func (w *UnicodeWriter) Finish() *Unicode {
	if w.pos == 0 {
		w.alias = nil
		w.buf = nil
		return NewStr("").(*Unicode)
	}
	if w.readonly && w.alias != nil {
		out := w.alias
		w.alias = nil
		return out
	}
	out := &Unicode{
		v:      string(w.buf),
		length: w.pos,
		hash:   -1,
		ready:  true,
	}
	out.kind = unicodeKindForMaxChar(w.maxchar)
	out.ascii = w.maxchar < 0x80
	out.init(strType)
	w.buf = nil
	return out
}

// Dealloc releases writer state. Mirrors _PyUnicodeWriter_Dealloc.
//
// CPython: Objects/unicodeobject.c:14242 _PyUnicodeWriter_Dealloc
func (w *UnicodeWriter) Dealloc() {
	w.buf = nil
	w.alias = nil
}
