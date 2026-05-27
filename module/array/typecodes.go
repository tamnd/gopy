// Per-typecode getitem / setitem / compareitems helpers. Each pair
// mirrors CPython's X_getitem / X_setitem functions from Modules/
// arraymodule.c. Signed-range overflow checks are written out per
// typecode to match the CPython error messages.
//
// CPython: Modules/arraymodule.c:235 b_getitem and following

package array

import (
	"bytes"
	"fmt"
	"unicode/utf16"

	"github.com/tamnd/gopy/objects"
)

// --- 'b' signed char ---------------------------------------------------------

// CPython: Modules/arraymodule.c:235 b_getitem
func bGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(int8(ap.obItem[i]))), nil
}

// CPython: Modules/arraymodule.c:242 b_setitem
func bSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asLongRange(v, -128, 127, "signed char")
	if err != nil {
		return err
	}
	if i >= 0 {
		ap.obItem[i] = byte(int8(x))
	}
	return nil
}

// --- 'B' unsigned char -------------------------------------------------------

// CPython: Modules/arraymodule.c:270 BB_getitem
func bbGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(ap.obItem[i])), nil
}

// CPython: Modules/arraymodule.c:276 BB_setitem
func bbSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asLongRange(v, 0, 0xFF, "unsigned char")
	if err != nil {
		return err
	}
	if i >= 0 {
		ap.obItem[i] = byte(x)
	}
	return nil
}

// --- 'u' wchar_t (16-bit UTF-16) --------------------------------------------

// CPython: Modules/arraymodule.c:293 u_getitem
func uGetitem(ap *arrayObject, i int) (objects.Object, error) {
	w := nativeOrder.Uint16(ap.itemSlice(i))
	if utf16.IsSurrogate(rune(w)) {
		// Lone surrogate; CPython produces a single-char string from
		// the codepoint via PyUnicode_FromOrdinal which permits the
		// surrogate range.
		return objects.NewStr(string(rune(w))), nil
	}
	return objects.NewStr(string(rune(w))), nil
}

// CPython: Modules/arraymodule.c:299 u_setitem
func uSetitem(ap *arrayObject, i int, v objects.Object) error {
	s, ok := v.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: array item must be a unicode character, not %s", v.Type().Name)
	}
	runes := []rune(s.Value())
	if len(runes) != 1 {
		return fmt.Errorf("TypeError: array item must be a unicode character, not a string of length %d", len(runes))
	}
	if i >= 0 {
		u16 := utf16.Encode([]rune{runes[0]})
		if len(u16) != 1 {
			return fmt.Errorf("TypeError: character requires a surrogate pair, not representable as a single wchar_t")
		}
		nativeOrder.PutUint16(ap.itemSlice(i), u16[0])
	}
	return nil
}

// --- 'w' Py_UCS4 -------------------------------------------------------------

// CPython: Modules/arraymodule.c:347 w_getitem
func wGetitem(ap *arrayObject, i int) (objects.Object, error) {
	r := rune(nativeOrder.Uint32(ap.itemSlice(i)))
	return objects.NewStr(string(r)), nil
}

// CPython: Modules/arraymodule.c:353 w_setitem
func wSetitem(ap *arrayObject, i int, v objects.Object) error {
	s, ok := v.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: array item must be a unicode character, not %s", v.Type().Name)
	}
	runes := []rune(s.Value())
	if len(runes) != 1 {
		return fmt.Errorf("TypeError: array item must be a unicode character, not a string of length %d", len(runes))
	}
	if i >= 0 {
		nativeOrder.PutUint32(ap.itemSlice(i), uint32(runes[0]))
	}
	return nil
}

// --- 'h' signed short --------------------------------------------------------

// CPython: Modules/arraymodule.c:393 h_getitem
func hGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(int16(nativeOrder.Uint16(ap.itemSlice(i))))), nil
}

// CPython: Modules/arraymodule.c:399 h_setitem
func hSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asLongRange(v, -32768, 32767, "signed short")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint16(ap.itemSlice(i), uint16(int16(x)))
	}
	return nil
}

// --- 'H' unsigned short ------------------------------------------------------

// CPython: Modules/arraymodule.c:413 HH_getitem
func hhGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(nativeOrder.Uint16(ap.itemSlice(i)))), nil
}

// CPython: Modules/arraymodule.c:419 HH_setitem
func hhSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asLongRange(v, 0, 0xFFFF, "unsigned short")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint16(ap.itemSlice(i), uint16(x))
	}
	return nil
}

// --- 'i' signed int ----------------------------------------------------------

// CPython: Modules/arraymodule.c:432 i_getitem
func iGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(int32(nativeOrder.Uint32(ap.itemSlice(i))))), nil
}

// CPython: Modules/arraymodule.c:438 i_setitem
func iSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asLongRange(v, -2147483648, 2147483647, "signed int")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint32(ap.itemSlice(i), uint32(int32(x)))
	}
	return nil
}

// --- 'I' unsigned int --------------------------------------------------------

// CPython: Modules/arraymodule.c:451 II_getitem
func iiGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(nativeOrder.Uint32(ap.itemSlice(i)))), nil
}

// CPython: Modules/arraymodule.c:457 II_setitem
func iiSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asUlongRange(v, 0xFFFFFFFF, "unsigned int")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint32(ap.itemSlice(i), uint32(x))
	}
	return nil
}

// --- 'l' signed long (8 bytes on gopy) ---------------------------------------

// CPython: Modules/arraymodule.c:473 l_getitem
func lGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(nativeOrder.Uint64(ap.itemSlice(i)))), nil
}

// CPython: Modules/arraymodule.c:479 l_setitem
func lSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asInt64(v, true, "signed long")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint64(ap.itemSlice(i), uint64(x))
	}
	return nil
}

// --- 'L' unsigned long (8 bytes) ---------------------------------------------

// CPython: Modules/arraymodule.c:492 LL_getitem
func llGetitem(ap *arrayObject, i int) (objects.Object, error) {
	x := nativeOrder.Uint64(ap.itemSlice(i))
	return newIntFromUint64(x), nil
}

// CPython: Modules/arraymodule.c:498 LL_setitem
func llSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asUint64(v, "unsigned long")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint64(ap.itemSlice(i), x)
	}
	return nil
}

// --- 'q' signed long long ----------------------------------------------------

// CPython: Modules/arraymodule.c:512 q_getitem
func qGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return objects.NewInt(int64(nativeOrder.Uint64(ap.itemSlice(i)))), nil
}

// CPython: Modules/arraymodule.c:518 q_setitem
func qSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asInt64(v, true, "signed long long")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint64(ap.itemSlice(i), uint64(x))
	}
	return nil
}

// --- 'Q' unsigned long long --------------------------------------------------

// CPython: Modules/arraymodule.c:530 QQ_getitem
func qqGetitem(ap *arrayObject, i int) (objects.Object, error) {
	return newIntFromUint64(nativeOrder.Uint64(ap.itemSlice(i))), nil
}

// CPython: Modules/arraymodule.c:536 QQ_setitem
func qqSetitem(ap *arrayObject, i int, v objects.Object) error {
	x, err := asUint64(v, "unsigned long long")
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint64(ap.itemSlice(i), x)
	}
	return nil
}

// --- 'f' float (4 bytes IEEE 754) -------------------------------------------

// CPython: Modules/arraymodule.c:550 f_getitem
func fGetitem(ap *arrayObject, i int) (objects.Object, error) {
	bits := nativeOrder.Uint32(ap.itemSlice(i))
	return objects.NewFloat(float64(float32FromBits(bits))), nil
}

// CPython: Modules/arraymodule.c:556 f_setitem
func fSetitem(ap *arrayObject, i int, v objects.Object) error {
	f, err := asFloat(v)
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint32(ap.itemSlice(i), float32Bits(float32(f)))
	}
	return nil
}

// --- 'd' double (8 bytes IEEE 754) ------------------------------------------

// CPython: Modules/arraymodule.c:568 d_getitem
func dGetitem(ap *arrayObject, i int) (objects.Object, error) {
	bits := nativeOrder.Uint64(ap.itemSlice(i))
	return objects.NewFloat(float64FromBits(bits)), nil
}

// CPython: Modules/arraymodule.c:574 d_setitem
func dSetitem(ap *arrayObject, i int, v objects.Object) error {
	f, err := asFloat(v)
	if err != nil {
		return err
	}
	if i >= 0 {
		nativeOrder.PutUint64(ap.itemSlice(i), float64Bits(f))
	}
	return nil
}

// --- compareitems ------------------------------------------------------------
//
// Each compareitems returns -1, 0, or 1 for lexicographic ordering
// over the two byte slices, interpreting each itemsize-wide chunk
// through the typecode's native representation. CPython generates
// these from the DEFINE_COMPAREITEMS macro; we expand them by hand.
//
// CPython: Modules/arraymodule.c:643 DEFINE_COMPAREITEMS

func bCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := int8(a[i])
		bv := int8(b[i])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func bbCompareitems(a, b []byte, n int) int {
	return bytes.Compare(a[:n], b[:n])
}

func uCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := nativeOrder.Uint16(a[i*2:])
		bv := nativeOrder.Uint16(b[i*2:])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func wCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := nativeOrder.Uint32(a[i*4:])
		bv := nativeOrder.Uint32(b[i*4:])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func hCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := int16(nativeOrder.Uint16(a[i*2:]))
		bv := int16(nativeOrder.Uint16(b[i*2:]))
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func hhCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := nativeOrder.Uint16(a[i*2:])
		bv := nativeOrder.Uint16(b[i*2:])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func iCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := int32(nativeOrder.Uint32(a[i*4:]))
		bv := int32(nativeOrder.Uint32(b[i*4:]))
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func iiCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := nativeOrder.Uint32(a[i*4:])
		bv := nativeOrder.Uint32(b[i*4:])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func lCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := int64(nativeOrder.Uint64(a[i*8:]))
		bv := int64(nativeOrder.Uint64(b[i*8:]))
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func llCompareitems(a, b []byte, n int) int {
	for i := 0; i < n; i++ {
		av := nativeOrder.Uint64(a[i*8:])
		bv := nativeOrder.Uint64(b[i*8:])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func qCompareitems(a, b []byte, n int) int {
	return lCompareitems(a, b, n)
}

func qqCompareitems(a, b []byte, n int) int {
	return llCompareitems(a, b, n)
}
