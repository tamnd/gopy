// TYPE_CODE encoder/decoder. Mirrors the `case TYPE_CODE:` arm in
// CPython's r_object / w_object. The wire format is the 3.11+ layout:
// argcount, posonlyargcount, kwonlyargcount, stacksize, flags as 32-bit
// ints, then code/consts/names/localsplusnames/localspluskinds as
// marshaled objects, then filename/name/qualname as strings,
// firstlineno as int32, linetable and exceptiontable as bytes.
//
// CPython: Python/marshal.c:L557 w_complex_object TYPE_CODE
// CPython: Python/marshal.c:L744 r_object TYPE_CODE
package marshal

import (
	"encoding/binary"
	"fmt"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// localspluskinds bit flags matching CPython's _PyLocalsKinds.
//
// CPython: Include/internal/pycore_code.h:42 CO_FAST_*
const (
	coFastArgPos uint8 = 0x02
	coFastArgKw  uint8 = 0x04
	coFastArgVar uint8 = 0x08
	coFastHidden uint8 = 0x10
	coFastLocal  uint8 = 0x20
	coFastCell   uint8 = 0x40
	coFastFree   uint8 = 0x80
)

// coVarargs / coVarkeywords mirror CPython's CO_VARARGS / CO_VARKEYWORDS
// flags. Used by buildLocalsplusnames to recover the *args / **kwargs
// arg-kind buckets when the Code carries only Argcount metadata.
//
// CPython: Include/cpython/code.h:120 CO_VARARGS / CO_VARKEYWORDS
const (
	coVarargs     = 0x0004
	coVarkeywords = 0x0008
)

// marshalCode writes a Code object as TYPE_CODE. flag carries
// FLAG_REF from the w_object dispatch. co_code, co_linetable, and
// co_exceptiontable go through writeCachedBytes because CPython's
// assemble.c funnels all three through _PyCompile_ConstCacheMergeOne
// so identical-content blobs share PyBytes identity and end up
// memoized with FLAG_REF. localspluskinds is built fresh per code
// object in makecode and bypasses the const cache, so it stays on
// the generic enc.write path which only memoizes the empty-bytes
// singleton.
//
// CPython: Python/marshal.c:557 w_complex_object TYPE_CODE
// CPython: Python/assemble.c:447 _PyCompile_ConstCacheMergeOne calls.
func marshalCode(enc *encoder, c *objects.Code, flag byte) error {
	if err := enc.writeByte(typeCode | flag); err != nil {
		return err
	}
	for _, v := range []int32{
		int32(c.Argcount),
		int32(c.PosonlyArgcount),
		int32(c.KwonlyArgcount),
		int32(c.Stacksize),
		int32(c.Flags),
	} {
		if err := enc.writeInt32(v); err != nil {
			return err
		}
	}
	// Mirror CPython's _PyCode_GetCode pre-write deopt: walk every
	// codeunit and rewrite specialized opcodes back to their adaptive
	// parent, then zero each trailing cache cell. Without this step a
	// .pyc would carry whatever specialization state the in-memory Code
	// happened to warm by marshal time, which is non-deterministic
	// across runs and breaks byte-equality with the cpython oracle.
	// specialize.Enable on unmarshalCode re-runs Quicken so adaptive
	// counters get reseeded on load.
	//
	// CPython: Objects/codeobject.c:2310 _PyCode_GetCode (deopts before write)
	if err := enc.writeCachedBytes(specialize.DeoptCode(c.Code), true); err != nil {
		return err
	}
	consts := make([]any, len(c.Consts))
	for i, v := range c.Consts {
		consts[i] = wrapConstStrings(v)
	}
	if err := enc.write(consts); err != nil {
		return err
	}
	if err := enc.write(stringsToAny(c.Names)); err != nil {
		return err
	}
	lplus, lkinds := buildLocalsplusnames(c)
	if err := enc.write(lplus); err != nil {
		return err
	}
	if err := enc.write(lkinds); err != nil {
		return err
	}
	if err := enc.write(c.Filename); err != nil {
		return err
	}
	if err := enc.write(c.Name); err != nil {
		return err
	}
	if err := enc.write(c.Qualname); err != nil {
		return err
	}
	if err := enc.writeInt32(int32(c.Firstlineno)); err != nil {
		return err
	}
	if err := enc.writeCachedBytes(c.Linetable, false); err != nil {
		return err
	}
	return enc.writeCachedBytes(c.ExceptionTable, false)
}

// unmarshalCode reads a TYPE_CODE value. The tag byte has been consumed.
//
// CPython: Python/marshal.c:L744 r_object TYPE_CODE
//
//nolint:gocognit,gocyclo // sequential field reads mirror CPython's r_object TYPE_CODE arm one-for-one.
func unmarshalCode(d *decoder) (*objects.Code, error) {
	var buf [4]byte
	readInt32 := func() (int32, error) {
		for i := range 4 {
			b, err := d.r.ReadByte()
			if err != nil {
				return 0, err
			}
			buf[i] = b
		}
		return int32(binary.LittleEndian.Uint32(buf[:])), nil
	}

	c := objects.NewCode()
	argcount, err := readInt32()
	if err != nil {
		return nil, fmt.Errorf("marshal: code argcount: %w", err)
	}
	c.Argcount = int(argcount)
	posonlyargcount, err := readInt32()
	if err != nil {
		return nil, fmt.Errorf("marshal: code posonlyargcount: %w", err)
	}
	c.PosonlyArgcount = int(posonlyargcount)
	kwonlyargcount, err := readInt32()
	if err != nil {
		return nil, fmt.Errorf("marshal: code kwonlyargcount: %w", err)
	}
	c.KwonlyArgcount = int(kwonlyargcount)
	stacksize, err := readInt32()
	if err != nil {
		return nil, fmt.Errorf("marshal: code stacksize: %w", err)
	}
	c.Stacksize = int(stacksize)
	flags, err := readInt32()
	if err != nil {
		return nil, fmt.Errorf("marshal: code flags: %w", err)
	}
	c.Flags = int(flags)

	// code bytes
	codeObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.code: %w", err)
	}
	code, ok := codeObj.([]byte)
	if !ok {
		return nil, fmt.Errorf("marshal: code.code expected bytes, got %T", codeObj)
	}
	c.Code = code

	// consts tuple
	constsObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.consts: %w", err)
	}
	constsTuple, ok := constsObj.([]any)
	if !ok {
		return nil, fmt.Errorf("marshal: code.consts expected tuple, got %T", constsObj)
	}
	c.Consts = constsTuple
	c.SyncConstObjs()

	// names tuple
	namesObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.names: %w", err)
	}
	c.Names, err = anyToStrings(namesObj, "names")
	if err != nil {
		return nil, err
	}
	c.SyncNameObjs()

	// localsplusnames + localspluskinds
	lplusObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.localsplusnames: %w", err)
	}
	lkindsObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.localspluskinds: %w", err)
	}
	lplusTuple, ok := lplusObj.([]any)
	if !ok {
		return nil, fmt.Errorf("marshal: localsplusnames expected tuple, got %T", lplusObj)
	}
	lkinds, ok := lkindsObj.([]byte)
	if !ok {
		return nil, fmt.Errorf("marshal: localspluskinds expected bytes, got %T", lkindsObj)
	}
	c.LocalsplusNames = make([]string, len(lplusTuple))
	for i, x := range lplusTuple {
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("marshal: localsplusnames[%d] expected str, got %T", i, x)
		}
		c.LocalsplusNames[i] = s
	}
	c.LocalsplusKinds = make([]byte, len(lkinds))
	copy(c.LocalsplusKinds, lkinds)
	c.Varnames, c.Cellvars, c.Freevars = splitLocalsplusnames(lplusTuple, lkinds)
	c.SyncLocalsplusCounts()

	// filename, name, qualname
	filenameObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.filename: %w", err)
	}
	c.Filename, ok = filenameObj.(string)
	if !ok {
		return nil, fmt.Errorf("marshal: code.filename expected str, got %T", filenameObj)
	}
	nameObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.name: %w", err)
	}
	c.Name, ok = nameObj.(string)
	if !ok {
		return nil, fmt.Errorf("marshal: code.name expected str, got %T", nameObj)
	}
	qualnameObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.qualname: %w", err)
	}
	c.Qualname, ok = qualnameObj.(string)
	if !ok {
		return nil, fmt.Errorf("marshal: code.qualname expected str, got %T", qualnameObj)
	}

	// firstlineno
	firstlineno, err := readInt32()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.firstlineno: %w", err)
	}
	c.Firstlineno = int(firstlineno)

	// linetable
	linetableObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.linetable: %w", err)
	}
	c.Linetable, ok = linetableObj.([]byte)
	if !ok {
		return nil, fmt.Errorf("marshal: code.linetable expected bytes, got %T", linetableObj)
	}

	// exceptiontable
	exctableObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.exceptiontable: %w", err)
	}
	c.ExceptionTable, ok = exctableObj.([]byte)
	if !ok {
		return nil, fmt.Errorf("marshal: code.exceptiontable expected bytes, got %T", exctableObj)
	}

	specialize.Enable(c)

	return c, nil
}

// buildLocalsplusnames materializes the co_localsplusnames tuple and
// co_localspluskinds bytes for marshal.
//
// When the Code object carries the flat LocalsplusNames /
// LocalsplusKinds slabs (populated by compile/assemble.c's
// compute_localsplus_info), this just returns copies of them so the
// wire bytes match CPython exactly. For code objects produced by
// older paths that only store Varnames / Cellvars / Freevars, we
// recover the same layout in-place: walk Varnames through the
// argvarkinds buckets (posonly, posorkw, kwonly, *args, **kwargs,
// then the remaining locals) so each arg slot gets CO_FAST_LOCAL plus
// the matching CO_FAST_ARG_* sub-flags, drop arg-cell duplicates, and
// stamp surviving cells and frees.
//
// CPython: Python/assemble.c:483 compute_localsplus_info
func buildLocalsplusnames(c *objects.Code) (names []any, kinds []byte) {
	if len(c.LocalsplusNames) > 0 && len(c.LocalsplusKinds) == len(c.LocalsplusNames) {
		names = make([]any, len(c.LocalsplusNames))
		for i, n := range c.LocalsplusNames {
			names[i] = n
		}
		kinds = make([]byte, len(c.LocalsplusKinds))
		copy(kinds, c.LocalsplusKinds)
		return names, kinds
	}

	cellset := make(map[string]bool, len(c.Cellvars))
	for _, n := range c.Cellvars {
		cellset[n] = true
	}
	varset := make(map[string]bool, len(c.Varnames))
	for _, n := range c.Varnames {
		varset[n] = true
	}

	hasVarargs := c.Flags&coVarargs != 0
	hasVarkeywords := c.Flags&coVarkeywords != 0
	argvarkinds := [6]struct {
		count int
		kind  uint8
	}{
		{c.PosonlyArgcount, coFastArgPos},
		{c.Argcount - c.PosonlyArgcount, coFastArgPos | coFastArgKw},
		{c.KwonlyArgcount, coFastArgKw},
		{boolCount(hasVarargs), coFastArgVar | coFastArgPos},
		{boolCount(hasVarkeywords), coFastArgVar | coFastArgKw},
		{-1, 0},
	}

	nlocals := len(c.Varnames)
	nlocalsplus := nlocals + len(c.Cellvars) + len(c.Freevars)
	for _, n := range c.Cellvars {
		if varset[n] {
			nlocalsplus--
		}
	}

	names = make([]any, nlocalsplus)
	kinds = make([]byte, nlocalsplus)

	pos := 0
	bucketMax := 0
	for i := range 6 {
		if argvarkinds[i].count < 0 {
			bucketMax = nlocals
		} else {
			bucketMax += argvarkinds[i].count
		}
		for pos < bucketMax && pos < nlocals {
			name := c.Varnames[pos]
			kind := coFastLocal | argvarkinds[i].kind
			if name != "" && name[0] == '.' {
				kind |= coFastHidden
			}
			if cellset[name] {
				kind |= coFastCell
			}
			names[pos] = name
			kinds[pos] = kind
			pos++
		}
	}

	numdropped := 0
	for i, n := range c.Cellvars {
		if varset[n] {
			numdropped++
			continue
		}
		off := i - numdropped + nlocals
		names[off] = n
		kinds[off] = coFastCell
	}
	for i, n := range c.Freevars {
		off := i + len(c.Cellvars) - numdropped + nlocals
		names[off] = n
		kinds[off] = coFastFree
	}
	return names, kinds
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

// splitLocalsplusnames reconstructs varnames/cellvars/freevars from
// the wire-format combined array.
func splitLocalsplusnames(names []any, kinds []byte) (varnames []string, cellvars []string, freevars []string) {
	for i, n := range names {
		s, _ := n.(string)
		if i >= len(kinds) {
			break
		}
		switch {
		case kinds[i]&coFastFree != 0:
			freevars = append(freevars, s)
		case kinds[i]&coFastCell != 0:
			cellvars = append(cellvars, s)
		default:
			varnames = append(varnames, s)
		}
	}
	return varnames, cellvars, freevars
}

// wrapConstStrings recursively rewrites the const tree so plain string
// nodes split into two camps: identifier-shaped strings stay plain (they
// match CPython's should_intern_string and get interned by _PyCode_New /
// intern_constants, so the marshal layer sees an interned PyUnicode and
// emits FLAG_REF + TYPE_*_INTERNED), while everything else wraps in
// literalString so the encoder skips the memo and emits plain
// TYPE_SHORT_ASCII / TYPE_ASCII / TYPE_UNICODE. Tuples are walked
// element-by-element. *objects.Code values pass through unchanged so
// their inner marshalCode re-wraps their own consts.
//
// CPython: Objects/codeobject.c:117 should_intern_string
// CPython: Objects/codeobject.c:203 intern_constants
// CPython: Python/marshal.c:391 w_ref interned check.
func wrapConstStrings(v any) any {
	switch x := v.(type) {
	case string:
		if shouldInternString(x) {
			return x
		}
		return literalString(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = wrapConstStrings(item)
		}
		return out
	}
	return v
}

// shouldInternString reports whether a string in co_consts ends up
// interned by the time it reaches marshal. Two CPython mechanisms feed
// this prediction:
//
//  1. _PyCode_New runs every co_consts entry through
//     intern_constants → should_intern_string, which interns ASCII
//     strings whose every byte is in `[A-Za-z0-9_]`. Empty strings pass
//     the loop and are interned.
//  2. PyUnicode_FromString routes every 1-byte ASCII unicode through
//     unicode_result, which substitutes the latin1 singleton from
//     LATIN1(ch). The singleton is statically allocated and marked
//     interned (the intern_common assert at unicodeobject.c:16044
//     confirms `PyUnicode_CHECK_INTERNED(r)`). So every single-byte
//     ASCII literal lands in marshal as interned regardless of the
//     identifier-character test.
//
// CPython: Objects/codeobject.c:117 should_intern_string
// CPython: Objects/unicodeobject.c:818 unicode_result (latin1 singleton)
// CPython: Objects/unicodeobject.c:16040 intern_common (latin1 fast path).
func shouldInternString(s string) bool {
	if len(s) <= 1 {
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x80 {
			return false
		}
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if !isAlpha && !isDigit && c != '_' {
			return false
		}
	}
	return true
}

func stringsToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func anyToStrings(v any, field string) ([]string, error) {
	tup, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("marshal: code.%s expected tuple, got %T", field, v)
	}
	out := make([]string, len(tup))
	for i, x := range tup {
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("marshal: code.%s[%d] expected str, got %T", field, i, x)
		}
		out[i] = s
	}
	return out, nil
}
