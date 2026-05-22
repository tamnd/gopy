// Package array is the gopy port of CPython's array C extension.
// It provides the array.array type: a typed mutable sequence backed by
// a contiguous machine-word buffer, with one of fourteen typecodes
// selecting the per-item C representation.
//
// CPython: Modules/arraymodule.c
package array

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("array", buildModule)
}

// arraydescr is one row of the typecode dispatch table. Each row
// captures the C type's size, the pack/unpack helpers, and the
// signed/integer flags used by the comparison and overflow paths.
//
// CPython: Modules/arraymodule.c:32 arraydescr
type arraydescr struct {
	typecode     byte
	itemsize     int
	getitem      func(ap *arrayObject, i int) (objects.Object, error)
	setitem      func(ap *arrayObject, i int, v objects.Object) error
	compareitems func(a, b []byte, n int) int
}

// arrayObject is the Go backing for an array.array instance. Mirrors
// the C arrayobject struct: ob_item is the raw element buffer, count
// is ob_size, allocated is the allocated capacity, descr points at
// the typecode dispatch row.
//
// CPython: Modules/arraymodule.c:43 arrayobject
type arrayObject struct {
	objects.Header
	obItem    []byte
	count     int
	allocated int
	descr     *arraydescr
	obExports int
}

// nbytes returns the live buffer length in bytes (count * itemsize).
func (a *arrayObject) nbytes() int { return a.count * a.descr.itemsize }

// itemSlice returns the byte slice covering item i. Caller must hold
// bounds (0 <= i < a.count).
func (a *arrayObject) itemSlice(i int) []byte {
	off := i * a.descr.itemsize
	return a.obItem[off : off+a.descr.itemsize]
}

// descriptors mirrors CPython's static descriptors[] table. The order
// is the order of the typecode lookup loop in array_new; tests rely on
// `typecodes` being concatenated in this same order.
//
// gopy fixes 'l'/'L' at 8 bytes (matching the 64-bit Unix ABI) and
// 'i'/'I' at 4 bytes. CPython uses sizeof(long) and sizeof(int) which
// agree on every platform gopy supports.
//
// CPython: Modules/arraymodule.c:672 descriptors
var descriptors = []*arraydescr{
	{'b', 1, bGetitem, bSetitem, bCompareitems},
	{'B', 1, bbGetitem, bbSetitem, bbCompareitems},
	{'u', 2, uGetitem, uSetitem, uCompareitems},
	{'w', 4, wGetitem, wSetitem, wCompareitems},
	{'h', 2, hGetitem, hSetitem, hCompareitems},
	{'H', 2, hhGetitem, hhSetitem, hhCompareitems},
	{'i', 4, iGetitem, iSetitem, iCompareitems},
	{'I', 4, iiGetitem, iiSetitem, iiCompareitems},
	{'l', 8, lGetitem, lSetitem, lCompareitems},
	{'L', 8, llGetitem, llSetitem, llCompareitems},
	{'q', 8, qGetitem, qSetitem, qCompareitems},
	{'Q', 8, qqGetitem, qqSetitem, qqCompareitems},
	{'f', 4, fGetitem, fSetitem, nil},
	{'d', 8, dGetitem, dSetitem, nil},
}

// typecodesStr is the module-level `array.typecodes` constant: the
// concatenation of every supported typecode character. The order
// matches the descriptors table.
//
// CPython: Modules/arraymodule.c:3308 PyModule_AddStringConstant("typecodes", ...)
var typecodesStr = func() string {
	var b strings.Builder
	for _, d := range descriptors {
		b.WriteByte(d.typecode)
	}
	return b.String()
}()

// findDescr returns the arraydescr for typecode c, or nil if no
// descriptor matches.
//
// CPython: Modules/arraymodule.c:2843 the descriptors lookup loop in array_new
func findDescr(c byte) *arraydescr {
	for _, d := range descriptors {
		if d.typecode == c {
			return d
		}
	}
	return nil
}

// newArrayObject allocates a fresh array instance with the given
// typecode descriptor, sized to hold n items. Mirrors newarrayobject:
// the buffer is zero-initialized (PyMem_NEW with calloc semantics).
//
// CPython: Modules/arraymodule.c:700 newarrayobject
func newArrayObject(cls *objects.Type, n int, descr *arraydescr) (*arrayObject, error) {
	if n < 0 {
		return nil, fmt.Errorf("SystemError: bad internal call: newArrayObject size %d", n)
	}
	if n > math.MaxInt/descr.itemsize {
		return nil, fmt.Errorf("MemoryError")
	}
	a := &arrayObject{descr: descr, count: n, allocated: n}
	if n > 0 {
		a.obItem = make([]byte, n*descr.itemsize)
	}
	a.Init(cls)
	return a, nil
}

// arrayResize changes the live length to newsize, reallocating obItem
// using the same over-allocation curve as CPython (~12.5% slack).
// Refuses to resize when buffer exports are outstanding.
//
// CPython: Modules/arraymodule.c:153 array_resize
func arrayResize(a *arrayObject, newsize int) error {
	if a.obExports > 0 {
		return fmt.Errorf("BufferError: cannot resize an array that is exporting buffers")
	}
	if newsize == a.allocated && newsize >= a.count {
		a.count = newsize
		return nil
	}
	if newsize == 0 {
		a.obItem = nil
		a.count = 0
		a.allocated = 0
		return nil
	}
	if newsize > math.MaxInt/a.descr.itemsize {
		return fmt.Errorf("MemoryError")
	}
	newAllocated := newsize + (newsize >> 3) + 6
	if newsize-a.count > newAllocated-newsize {
		newAllocated = newsize + 3
	}
	if newAllocated > math.MaxInt/a.descr.itemsize {
		return fmt.Errorf("MemoryError")
	}
	nbytes := newAllocated * a.descr.itemsize
	if cap(a.obItem) >= nbytes {
		a.obItem = a.obItem[:nbytes]
	} else {
		buf := make([]byte, nbytes)
		copy(buf, a.obItem)
		a.obItem = buf
	}
	a.count = newsize
	a.allocated = newAllocated
	return nil
}

// getarrayitem returns item i as a Python object via the typecode's
// getitem helper. Caller must hold bounds.
//
// CPython: Modules/arraymodule.c:738 getarrayitem
func getarrayitem(a *arrayObject, i int) (objects.Object, error) {
	return a.descr.getitem(a, i)
}

// setarrayitem assigns v to slot i via the typecode's setitem helper.
//
// CPython: Modules/arraymodule.c:782 setarrayitem
func setarrayitem(a *arrayObject, i int, v objects.Object) error {
	return a.descr.setitem(a, i, v)
}

// ArrayType is the Python-visible array.array class.
//
// CPython: Modules/arraymodule.c:3058 array_spec / Arraytype
var ArrayType = objects.NewType("array.array", []*objects.Type{objects.ObjectType()})

func init() {
	ArrayType.TpNew = arrayNew
	ArrayType.Repr = arrayRepr
	ArrayType.Str = arrayRepr
	ArrayType.Iter = arrayIter
	ArrayType.RichCmp = arrayRichCompare
	ArrayType.Sequence = &objects.SequenceMethods{
		Length:        func(o objects.Object) (int, error) { return o.(*arrayObject).count, nil },
		Concat:        arrayConcat,
		Repeat:        arrayRepeat,
		GetItem:       arraySqGetItem,
		SetItem:       arraySqSetItem,
		Contains:      arrayContains,
		InPlaceConcat: arrayInPlaceConcat,
		InPlaceRepeat: arrayInPlaceRepeat,
	}
	ArrayType.Mapping = &objects.MappingMethods{
		Length:  func(o objects.Object) (int, error) { return o.(*arrayObject).count, nil },
		GetItem: arrayMpSubscript,
		SetItem: arrayMpAssSubscript,
		DelItem: arrayMpDelItem,
	}
	ArrayType.TpFlags = objects.TpFlagSequence
	registerArrayMethods(ArrayType)
}

// nativeOrder is the host byte order used by the native typecodes.
// Mirrors CPython, which packs in native order for everything except
// the network-byte-order helpers in the struct module.
var nativeOrder binary.ByteOrder = binary.NativeEndian

// arrayNew is the tp_new constructor for array.array.
//
// Signature: array(typecode, initializer=None). The typecode is a
// single-character string (or int, via the 'C' format) selecting the
// descriptor row. The initializer, when present, must be one of:
// list, tuple, bytes, bytearray, str (only for 'u'/'w' typecodes),
// another array of the same typecode, or any iterable.
//
// CPython: Modules/arraymodule.c:2778 array_new
func arrayNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if cls == ArrayType && len(kwargs) > 0 {
		return nil, errors.New("TypeError: array.array() takes no keyword arguments")
	}
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: array() takes 1 or 2 arguments (%d given)", len(args))
	}
	c, err := parseTypecodeArg(args[0])
	if err != nil {
		return nil, err
	}
	descr := findDescr(c)
	if descr == nil {
		return nil, fmt.Errorf("ValueError: bad typecode (must be %s)", typecodesStr)
	}
	var initial objects.Object
	if len(args) == 2 {
		initial = args[1]
	}
	if err := validateInitializerType(c, initial); err != nil {
		return nil, err
	}
	src, err := classifyInitializer(c, initial)
	if err != nil {
		return nil, err
	}
	a, err := newArrayObject(cls, src.upfrontLen(), descr)
	if err != nil {
		return nil, err
	}
	if err := src.populate(a); err != nil {
		return nil, err
	}
	return a, nil
}

// validateInitializerType rejects str/unicode-array initializers for
// non-unicode typecodes. Mirrors the two early TypeError branches at
// the top of array_new.
//
// CPython: Modules/arraymodule.c:2800 array_new (TypeError branches)
func validateInitializerType(c byte, initial objects.Object) error {
	if initial == nil {
		return nil
	}
	isUnicode := c == 'u' || c == 'w'
	if isUnicode {
		return nil
	}
	if _, ok := initial.(*objects.Unicode); ok {
		return fmt.Errorf("TypeError: cannot use a str to initialize an array with typecode '%c'", c)
	}
	if other, ok := initial.(*arrayObject); ok {
		ic := other.descr.typecode
		if ic == 'u' || ic == 'w' {
			return fmt.Errorf("TypeError: cannot use a unicode array to initialize an array with typecode '%c'", c)
		}
	}
	return nil
}

// initSource is the bundled outcome of CPython's initializer routing.
// Exactly one of list/tup/bytes/str/arr/iter is set; the other fields
// stay zero. The populate() method copies the source into the freshly
// allocated array, sharing per-source loops between this constructor
// and the .fromlist / .frombytes / .fromunicode methods.
type initSource struct {
	list  *objects.List
	tup   *objects.Tuple
	bytes []byte
	str   string
	arr   *arrayObject
	iter  objects.Object
}

func (s *initSource) upfrontLen() int {
	switch {
	case s.list != nil:
		return s.list.Len()
	case s.tup != nil:
		return s.tup.Len()
	case s.arr != nil:
		return s.arr.count
	}
	return 0
}

func (s *initSource) populate(a *arrayObject) error {
	switch {
	case s.list != nil:
		for i := 0; i < s.list.Len(); i++ {
			if err := setarrayitem(a, i, s.list.Item(i)); err != nil {
				return err
			}
		}
	case s.tup != nil:
		for i := 0; i < s.tup.Len(); i++ {
			if err := setarrayitem(a, i, s.tup.Item(i)); err != nil {
				return err
			}
		}
	case s.arr != nil:
		copy(a.obItem, s.arr.obItem[:s.arr.nbytes()])
	case s.bytes != nil:
		return arrayFrombytesRaw(a, s.bytes)
	case s.str != "":
		return arrayFromunicodeRaw(a, s.str)
	case s.iter != nil:
		return populateFromIter(a, s.iter)
	}
	return nil
}

func populateFromIter(a *arrayObject, it objects.Object) error {
	for {
		next, nerr := objects.IterNext(it)
		if nerr != nil {
			if errors.Is(nerr, objects.ErrStopIteration) {
				return nil
			}
			return nerr
		}
		if err := arrayAppendOne(a, next); err != nil {
			return err
		}
	}
}

// classifyInitializer maps the runtime type of initial to one of the
// dispatch buckets in initSource. Mirrors the cascading branches at
// the top of CPython's array_new.
//
// CPython: Modules/arraymodule.c:2830 array_new initializer routing
func classifyInitializer(c byte, initial objects.Object) (*initSource, error) {
	src := &initSource{}
	switch v := initial.(type) {
	case nil:
		return src, nil
	case *objects.List:
		src.list = v
	case *objects.Tuple:
		src.tup = v
	case *objects.Bytes:
		src.bytes = v.Bytes()
	case *objects.ByteArray:
		src.bytes = append([]byte(nil), v.Bytes()...)
	case *objects.Unicode:
		src.str = v.Value()
	case *arrayObject:
		if v.descr.typecode == c {
			src.arr = v
		} else {
			it, ierr := objects.Iter(initial)
			if ierr != nil {
				return nil, ierr
			}
			src.iter = it
		}
	default:
		it, ierr := objects.Iter(initial)
		if ierr != nil {
			return nil, ierr
		}
		src.iter = it
	}
	return src, nil
}

// parseTypecodeArg parses the first argument to array.array(). CPython
// uses PyArg_ParseTuple "C" which accepts a single-character str.
//
// CPython: Modules/arraymodule.c:2789 PyArg_ParseTuple(args, "C|O:array", &c, &initial)
func parseTypecodeArg(o objects.Object) (byte, error) {
	s, ok := o.(*objects.Unicode)
	if !ok {
		return 0, fmt.Errorf("TypeError: array() argument 1 must be a unicode character, not %s", o.Type().Name)
	}
	v := s.Value()
	if len(v) != 1 {
		return 0, fmt.Errorf("TypeError: array() argument 1 must be a unicode character, not str of length %d", len(v))
	}
	return v[0], nil
}

// arrayRepr mirrors CPython's array_repr. For the 'u' and 'w' typecodes
// the repr is `array('u', 'abc')`, otherwise `array('b', [1, 2, 3])`,
// with an empty initializer abbreviated to `array('b')`.
//
// CPython: Modules/arraymodule.c:1690 array_repr
func arrayRepr(o objects.Object) (string, error) {
	a := o.(*arrayObject)
	if a.count == 0 {
		return fmt.Sprintf("array('%c')", a.descr.typecode), nil
	}
	if a.descr.typecode == 'u' || a.descr.typecode == 'w' {
		s, err := arrayTounicodeRaw(a)
		if err != nil {
			return "", err
		}
		r, err := objects.Repr(objects.NewStr(s))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("array('%c', %s)", a.descr.typecode, r), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "array('%c', [", a.descr.typecode)
	for i := 0; i < a.count; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		v, err := getarrayitem(a, i)
		if err != nil {
			return "", err
		}
		r, err := objects.Repr(v)
		if err != nil {
			return "", err
		}
		b.WriteString(r)
	}
	b.WriteString("])")
	return b.String(), nil
}

// arrayRichCompare mirrors array_richcompare. The fast path compares
// the raw buffer when both operands are arrays with the same typecode
// and a non-nil compareitems hook; otherwise falls back to item-by-item
// equality through the typecode's getitem.
//
// CPython: Modules/arraymodule.c:730 array_richcompare
func arrayRichCompare(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
	aa, aok := a.(*arrayObject)
	bb, bok := b.(*arrayObject)
	if !aok || !bok {
		return objects.NotImplemented(), nil
	}
	switch op {
	case objects.CompareEQ, objects.CompareNE:
		if aa.count != bb.count {
			return objects.NewBool(op == objects.CompareNE), nil
		}
	}
	if aa.descr == bb.descr && aa.descr.compareitems != nil {
		n := min(bb.count, aa.count)
		cmp := aa.descr.compareitems(aa.obItem[:n*aa.descr.itemsize], bb.obItem[:n*bb.descr.itemsize], n)
		if cmp == 0 {
			return compareLen(aa.count, bb.count, op), nil
		}
		return compareSign(cmp, op), nil
	}
	n := min(bb.count, aa.count)
	for i := range n {
		av, err := getarrayitem(aa, i)
		if err != nil {
			return nil, err
		}
		bv, err := getarrayitem(bb, i)
		if err != nil {
			return nil, err
		}
		eq, err := objects.RichCmpBool(av, bv, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if !eq {
			return objects.RichCmp(av, bv, op)
		}
	}
	return compareLen(aa.count, bb.count, op), nil
}

func compareLen(la, lb int, op objects.CompareOp) objects.Object {
	switch op {
	case objects.CompareLT:
		return objects.NewBool(la < lb)
	case objects.CompareLE:
		return objects.NewBool(la <= lb)
	case objects.CompareEQ:
		return objects.NewBool(la == lb)
	case objects.CompareNE:
		return objects.NewBool(la != lb)
	case objects.CompareGT:
		return objects.NewBool(la > lb)
	case objects.CompareGE:
		return objects.NewBool(la >= lb)
	}
	return objects.NewBool(false)
}

func compareSign(cmp int, op objects.CompareOp) objects.Object {
	switch op {
	case objects.CompareLT:
		return objects.NewBool(cmp < 0)
	case objects.CompareLE:
		return objects.NewBool(cmp <= 0)
	case objects.CompareEQ:
		return objects.NewBool(cmp == 0)
	case objects.CompareNE:
		return objects.NewBool(cmp != 0)
	case objects.CompareGT:
		return objects.NewBool(cmp > 0)
	case objects.CompareGE:
		return objects.NewBool(cmp >= 0)
	}
	return objects.NewBool(false)
}

// arrayAppendOne extends the array by one item, copying v through the
// typecode setitem hook. Shared between the constructor's iterator
// fallback and the public .append() method.
//
// CPython: Modules/arraymodule.c:837 ins1 (where == self->ob_size)
func arrayAppendOne(a *arrayObject, v objects.Object) error {
	if err := arrayResize(a, a.count+1); err != nil {
		return err
	}
	if err := setarrayitem(a, a.count-1, v); err != nil {
		// Roll back the size bump so a half-initialized slot is not visible.
		_ = arrayResize(a, a.count-1)
		return err
	}
	return nil
}

// arrayFrombytesRaw appends the contents of buf to a. The buffer must
// be a whole number of itemsizes long.
//
// CPython: Modules/arraymodule.c:2207 array_array_frombytes
func arrayFrombytesRaw(a *arrayObject, buf []byte) error {
	itemsize := a.descr.itemsize
	if len(buf)%itemsize != 0 {
		return fmt.Errorf("ValueError: bytes length not a multiple of item size")
	}
	n := len(buf) / itemsize
	oldCount := a.count
	if err := arrayResize(a, a.count+n); err != nil {
		return err
	}
	copy(a.obItem[oldCount*itemsize:], buf)
	return nil
}

// arrayFromunicodeRaw extends a with the codepoints of s. Only the 'u'
// and 'w' typecodes accept this path; the constructor already
// validates the typecode.
//
// CPython: Modules/arraymodule.c:2381 array_array_fromunicode
func arrayFromunicodeRaw(a *arrayObject, s string) error {
	switch a.descr.typecode {
	case 'u':
		// 'u' is wchar_t. On Windows that's 16-bit UTF-16; everywhere
		// else it's 32-bit UCS-4. gopy fixes 'u' at 16 bits (UTF-16)
		// so the typecode matches the platform CPython was historically
		// tested against on the Windows build.
		var u16 []uint16
		for _, r := range s {
			u16 = utf16.AppendRune(u16, r)
		}
		oldCount := a.count
		if err := arrayResize(a, a.count+len(u16)); err != nil {
			return err
		}
		for i, w := range u16 {
			nativeOrder.PutUint16(a.itemSlice(oldCount+i), w)
		}
		return nil
	case 'w':
		// 'w' is Py_UCS4: one slot per code point.
		runes := []rune(s)
		oldCount := a.count
		if err := arrayResize(a, a.count+len(runes)); err != nil {
			return err
		}
		for i, r := range runes {
			nativeOrder.PutUint32(a.itemSlice(oldCount+i), uint32(r))
		}
		return nil
	}
	return fmt.Errorf("ValueError: fromunicode() may only be called on unicode type arrays")
}

// arrayTounicodeRaw decodes a 'u' or 'w' typed array into the
// corresponding Go string.
//
// CPython: Modules/arraymodule.c:2411 array_array_tounicode
func arrayTounicodeRaw(a *arrayObject) (string, error) {
	switch a.descr.typecode {
	case 'u':
		buf := make([]uint16, a.count)
		for i := 0; i < a.count; i++ {
			buf[i] = nativeOrder.Uint16(a.itemSlice(i))
		}
		return string(utf16.Decode(buf)), nil
	case 'w':
		runes := make([]rune, a.count)
		for i := 0; i < a.count; i++ {
			runes[i] = rune(nativeOrder.Uint32(a.itemSlice(i)))
		}
		return string(runes), nil
	}
	return "", fmt.Errorf("ValueError: tounicode() may only be called on unicode type arrays")
}

// buildModule materializes the array module dict. Mirrors the array
// module's Py_mod_exec callback.
//
// CPython: Modules/arraymodule.c:3225 array_modexec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("array")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"array", ArrayType},
		{"ArrayType", ArrayType},
		{"typecodes", objects.NewStr(typecodesStr)},
		{"__doc__", objects.NewStr(moduleDoc)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

const moduleDoc = "This module defines an object type which can efficiently represent\n" +
	"an array of basic values: characters, integers, floating-point\n" +
	"numbers.  Arrays are sequence types and behave very much like lists,\n" +
	"except that the type of objects stored in them is constrained.\n"
