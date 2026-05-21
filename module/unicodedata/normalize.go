// Unicode normalization: NFD / NFKD via nfdNFKD, NFC / NFKC via
// nfcNFKC plus the is_normalized quickcheck. Port of CPython's
// nfd_nfkd, nfc_nfkc, and is_normalized_quickcheck.
//
// CPython: Modules/unicodedata.c:514 nfd_nfkd
// CPython: Modules/unicodedata.c:665 nfc_nfkc
// CPython: Modules/unicodedata.c:820 is_normalized_quickcheck

package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// quickcheckResult mirrors CPython's three-state enum.
//
// CPython: Modules/unicodedata.c:805 QuickcheckResult enum
type quickcheckResult int

const (
	quickcheckYes   quickcheckResult = 0
	quickcheckMaybe quickcheckResult = 1
	quickcheckNo    quickcheckResult = 2
)

// parseForm returns (nfc, k) for the given form name, or an error
// when form is not one of NFC / NFKC / NFD / NFKD.
//
// CPython: Modules/unicodedata.c:885 unicodedata_UCD_is_normalized_impl
// (form-string switch)
func parseForm(form string) (nfc, k bool, err error) {
	switch form {
	case "NFC":
		return true, false, nil
	case "NFKC":
		return true, true, nil
	case "NFD":
		return false, false, nil
	case "NFKD":
		return false, true, nil
	}
	return false, false, fmt.Errorf("ValueError: invalid normalization form")
}

// isAscii reports whether every rune in s is < 0x80.
func isAscii(s []rune) bool {
	for _, r := range s {
		if r >= 0x80 {
			return false
		}
	}
	return true
}

// isNormalizedQuickcheck runs the UAX #15 quickcheck algorithm.
// Returns YES when input is definitely normalized, NO when it
// definitely is not, and MAYBE when a full normalize-and-compare
// is required to be sure.
//
// CPython: Modules/unicodedata.c:820 is_normalized_quickcheck
func isNormalizedQuickcheck(input []rune, nfc, k, yesOnly bool) quickcheckResult {
	if isAscii(input) {
		return quickcheckYes
	}
	prevCombining := uint8(0)
	shift := uint(0)
	if nfc {
		shift += 4
	}
	if k {
		shift += 2
	}
	result := quickcheckYes
	for _, ch := range input {
		rec := getRecord(ch)
		combining := rec.Combining
		if combining != 0 && prevCombining > combining {
			return quickcheckNo
		}
		prevCombining = combining
		qc := rec.NormalizationQC
		if yesOnly {
			if qc&(3<<shift) != 0 {
				return quickcheckMaybe
			}
			continue
		}
		switch (qc >> shift) & 3 {
		case uint8(quickcheckNo):
			return quickcheckNo
		case uint8(quickcheckMaybe):
			result = quickcheckMaybe
		}
	}
	return result
}

// nfdNFKD decomposes input. When k=false produces NFD; when k=true
// produces NFKD. Output is canonically reordered per CCC.
//
// CPython: Modules/unicodedata.c:514 nfd_nfkd
func nfdNFKD(input []rune, k bool) []rune {
	out := make([]rune, 0, len(input)+10)
	var stack [20]rune
	stackPtr := 0
	for i := 0; i < len(input); i++ {
		stack[stackPtr] = input[i]
		stackPtr++
		for stackPtr > 0 {
			stackPtr--
			code := stack[stackPtr]

			// Hangul algorithmic decomposition.
			if code >= hangulSBase && code < hangulSBase+hangulSCount {
				s := int(code - hangulSBase)
				l := rune(hangulLBase + s/hangulNCount)
				v := rune(hangulVBase + (s%hangulNCount)/hangulTCount)
				t := rune(hangulTBase + s%hangulTCount)
				out = append(out, l, v)
				if t != hangulTBase {
					out = append(out, t)
				}
				continue
			}

			index, prefix, count := getDecompRecord(code)
			// Either no decomposition, or a compatibility
			// decomposition while we are doing NFD: copy as-is.
			if count == 0 || (prefix != 0 && !k) {
				out = append(out, code)
				continue
			}
			// Push decomposition onto the stack in reverse order.
			for j := count - 1; j >= 0; j-- {
				if stackPtr >= len(stack) {
					out = append(out, rune(decompData[index+j]))
					continue
				}
				stack[stackPtr] = rune(decompData[index+j])
				stackPtr++
			}
		}
	}

	// Canonical reordering: stable sort by Canonical_Combining_Class
	// within runs of non-starters.
	for i := 1; i < len(out); i++ {
		curCC := getRecord(out[i]).Combining
		if curCC == 0 {
			continue
		}
		prevCC := getRecord(out[i-1]).Combining
		if prevCC == 0 || prevCC <= curCC {
			continue
		}
		for j := i; j > 0; j-- {
			cur := getRecord(out[j]).Combining
			prev := getRecord(out[j-1]).Combining
			if cur == 0 || prev == 0 || prev <= cur {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// findNFCIndex walks the reindex table looking for code, returning
// the rebased index or -1 when code is not a composable input.
//
// CPython: Modules/unicodedata.c:649 find_nfc_index
func findNFCIndex(table []reindexEntry, code rune) int {
	for _, e := range table {
		if e.Start == 0 {
			break
		}
		start := rune(e.Start)
		if code < start {
			return -1
		}
		if code <= start+rune(e.Count) {
			return e.Index + int(code-start)
		}
	}
	return -1
}

// nfcNFKC composes input. nfc_nfkc starts from the NFD/NFKD
// output of nfdNFKD and walks it joining starter+combining pairs
// through the comp_index / comp_data tables.
//
// CPython: Modules/unicodedata.c:665 nfc_nfkc
func nfcNFKC(input []rune, k bool) []rune {
	decomp := nfdNFKD(input, k)
	length := len(decomp)
	if length == 0 {
		return decomp
	}
	out := make([]rune, length)
	var skipped [20]int
	cskipped := 0
	i, o := 0, 0
loop:
	for i < length {
		// Drop characters already consumed by a prior pair.
		for idx := 0; idx < cskipped; idx++ {
			if skipped[idx] == i {
				skipped[idx] = skipped[cskipped-1]
				cskipped--
				i++
				continue loop
			}
		}

		code := decomp[i]
		// Hangul L+V (and optionally +T) composition.
		if code >= hangulLBase && code < hangulLBase+hangulLCount &&
			i+1 < length &&
			decomp[i+1] >= hangulVBase && decomp[i+1] < hangulVBase+hangulVCount {
			lIndex := int(code - hangulLBase)
			vIndex := int(decomp[i+1] - hangulVBase)
			composed := rune(hangulSBase + (lIndex*hangulVCount+vIndex)*hangulTCount)
			i += 2
			if i < length && decomp[i] > hangulTBase && decomp[i] < hangulTBase+hangulTCount {
				composed += decomp[i] - hangulTBase
				i++
			}
			out[o] = composed
			o++
			continue
		}

		f := findNFCIndex(nfcFirst, code)
		if f == -1 {
			out[o] = code
			o++
			i++
			continue
		}

		i1 := i + 1
		comb := uint8(0)
		out[o] = decomp[i]
		for i1 < length {
			code1 := decomp[i1]
			comb1 := getRecord(code1).Combining
			if comb != 0 {
				if comb1 == 0 {
					break
				}
				if comb >= comb1 {
					i1++
					continue
				}
			}
			l := findNFCIndex(nfcLast, code1)
			if l == -1 {
				if comb1 == 0 {
					break
				}
				comb = comb1
				i1++
				continue
			}
			index := f*totalLast + l
			index1 := int(compIndex[index>>compShift])
			candidate := compData[(index1<<compShift)+(index&((1<<compShift)-1))]
			if candidate == 0 {
				if comb1 == 0 {
					break
				}
				comb = comb1
				i1++
				continue
			}
			out[o] = rune(candidate)
			if cskipped < len(skipped) {
				skipped[cskipped] = i1
				cskipped++
			}
			i1++
			f = findNFCIndex(nfcFirst, out[o])
			if f == -1 {
				break
			}
		}
		o++
		i++
	}
	if o == length {
		return decomp
	}
	return out[:o]
}

// CPython: Modules/unicodedata.c:885 unicodedata_UCD_is_normalized_impl
func isNormalizedBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	form, input, err := normalizeArgs("is_normalized", args)
	if err != nil {
		return nil, err
	}
	if len(input) == 0 {
		return objects.True(), nil
	}
	nfc, k, err := parseForm(form)
	if err != nil {
		return nil, err
	}
	m := isNormalizedQuickcheck(input, nfc, k, false)
	if m == quickcheckMaybe {
		var cmp []rune
		if nfc {
			cmp = nfcNFKC(input, k)
		} else {
			cmp = nfdNFKD(input, k)
		}
		if string(cmp) == string(input) {
			return objects.True(), nil
		}
		return objects.False(), nil
	}
	if m == quickcheckYes {
		return objects.True(), nil
	}
	return objects.False(), nil
}

// CPython: Modules/unicodedata.c:953 unicodedata_UCD_normalize_impl
func normalizeBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	form, input, err := normalizeArgs("normalize", args)
	if err != nil {
		return nil, err
	}
	if len(input) == 0 {
		return objects.NewStr(""), nil
	}
	nfc, k, err := parseForm(form)
	if err != nil {
		return nil, err
	}
	if isNormalizedQuickcheck(input, nfc, k, true) == quickcheckYes {
		return objects.NewStr(string(input)), nil
	}
	var out []rune
	if nfc {
		out = nfcNFKC(input, k)
	} else {
		out = nfdNFKD(input, k)
	}
	return objects.NewStr(string(out)), nil
}

// normalizeArgs unpacks the (form, unistr) pair shared by
// normalize and is_normalized. Mirrors the clinic conversion that
// rejects non-unicode arguments with TypeError.
func normalizeArgs(fname string, args []objects.Object) (string, []rune, error) {
	if len(args) != 2 {
		return "", nil, fmt.Errorf("TypeError: %s() takes exactly 2 arguments (%d given)", fname, len(args))
	}
	formObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return "", nil, fmt.Errorf("TypeError: %s() argument 1 must be str, not %s", fname, args[0].Type().Name)
	}
	inputObj, ok := args[1].(*objects.Unicode)
	if !ok {
		return "", nil, fmt.Errorf("TypeError: %s() argument 2 must be str, not %s", fname, args[1].Type().Name)
	}
	return formObj.Value(), []rune(inputObj.Value()), nil
}
