// Decomposition table walks: get_decomp_record (the low-level
// lookup that returns the {prefix, count, index} triple) and
// decompositionBuiltin (the Python-facing `unicodedata.decomposition`
// query that turns that triple into the printable hex form).
//
// CPython: Modules/unicodedata.c:415 unicodedata_UCD_decomposition_impl
// CPython: Modules/unicodedata.c:488 get_decomp_record

package unicodedata

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// Hangul-syllable constants. The Hangul block has algorithmic
// decomposition, so it never appears in the decomp_* tables.
//
// CPython: Modules/unicodedata.c:392 SBase / LBase / VBase / TBase
const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// getDecompRecord ports CPython's get_decomp_record: returns the
// (index, prefix, count) triple for code. index is the position
// in decompData of the FIRST decomposition rune (count gives the
// remaining length). prefix is the decompPrefix slot for
// `<compat>` / `<noBreak>` / etc.
//
// CPython: Modules/unicodedata.c:488 get_decomp_record
func getDecompRecord(code rune) (index, prefix, count int) {
	if code < 0 || code >= 0x110000 {
		index = 0
	} else {
		i := int(decompIndex1[code>>decompShift])
		i = int(decompIndex2[(i<<decompShift)+(int(code)&((1<<decompShift)-1))])
		index = i
	}
	count = int(decompData[index] >> 8)
	prefix = int(decompData[index] & 255)
	return index + 1, prefix, count
}

// CPython: Modules/unicodedata.c:415 unicodedata_UCD_decomposition_impl
func decompositionBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("decomposition", args)
	if err != nil {
		return nil, err
	}

	// Hangul Syllable Decomposition.
	if r >= hangulSBase && r < hangulSBase+hangulSCount {
		s := int(r - hangulSBase)
		l := hangulLBase + s/hangulNCount
		v := hangulVBase + (s%hangulNCount)/hangulTCount
		t := hangulTBase + s%hangulTCount
		if t != hangulTBase {
			return objects.NewStr(fmt.Sprintf("%04X %04X %04X", l, v, t)), nil
		}
		return objects.NewStr(fmt.Sprintf("%04X %04X", l, v)), nil
	}

	index, prefix, count := getDecompRecord(r)
	if count == 0 && prefix == 0 {
		return objects.NewStr(""), nil
	}

	var b strings.Builder
	b.WriteString(decompPrefix[prefix])
	for i := 0; i < count; i++ {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%04X", decompData[index+i])
	}
	return objects.NewStr(b.String()), nil
}
