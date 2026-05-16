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
// CPython: Include/internal/pycore_code.h:L29 CO_FAST_LOCAL et al.
const (
	coFastLocal = 0x20
	coFastCell  = 0x40
	coFastFree  = 0x80
)

// marshalCode writes a Code object as TYPE_CODE.
//
// CPython: Python/marshal.c:L557 w_complex_object TYPE_CODE
func marshalCode(enc *encoder, c *objects.Code) error {
	if err := enc.writeByte(typeCode); err != nil {
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
	// code bytes
	if err := enc.write(c.Code); err != nil {
		return err
	}
	// consts tuple
	consts := make([]any, len(c.Consts))
	copy(consts, c.Consts)
	if err := enc.write(consts); err != nil {
		return err
	}
	// names tuple
	if err := enc.write(stringsToAny(c.Names)); err != nil {
		return err
	}
	// localsplusnames + localspluskinds
	lplus, lkinds := buildLocalsplusnames(c)
	if err := enc.write(lplus); err != nil {
		return err
	}
	if err := enc.write(lkinds); err != nil {
		return err
	}
	// filename, name, qualname
	if err := enc.write(c.Filename); err != nil {
		return err
	}
	if err := enc.write(c.Name); err != nil {
		return err
	}
	if err := enc.write(c.Qualname); err != nil {
		return err
	}
	// firstlineno
	if err := enc.writeInt32(int32(c.Firstlineno)); err != nil {
		return err
	}
	// linetable, exceptiontable
	if err := enc.write(c.Linetable); err != nil {
		return err
	}
	return enc.write(c.ExceptionTable)
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

	// names tuple
	namesObj, err := d.read()
	if err != nil {
		return nil, fmt.Errorf("marshal: code.names: %w", err)
	}
	c.Names, err = anyToStrings(namesObj, "names")
	if err != nil {
		return nil, err
	}

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
	c.Varnames, c.Cellvars, c.Freevars = splitLocalsplusnames(lplusTuple, lkinds)

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

// buildLocalsplusnames constructs the localsplusnames tuple and
// localspluskinds bytes from the Code's separate name arrays.
func buildLocalsplusnames(c *objects.Code) (names []any, kinds []byte) {
	total := len(c.Varnames) + len(c.Cellvars) + len(c.Freevars)
	names = make([]any, 0, total)
	kinds = make([]byte, 0, total)
	for _, n := range c.Varnames {
		names = append(names, n)
		kinds = append(kinds, coFastLocal)
	}
	for _, n := range c.Cellvars {
		names = append(names, n)
		kinds = append(kinds, coFastCell)
	}
	for _, n := range c.Freevars {
		names = append(names, n)
		kinds = append(kinds, coFastFree)
	}
	return names, kinds
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
