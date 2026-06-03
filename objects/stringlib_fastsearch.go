// stringlib: fastsearch implementation, ported from CPython's
// Objects/stringlib/fastsearch.h for the 1-byte-char instantiation
// (bytes / bytearray). It is a mix of Boyer-Moore and Horspool with a
// compressed bad-character table, escalating to Crochemore and Perrin's
// Two-Way algorithm for long inputs so that find / count / index stay
// O(n + m) instead of the naive O(n * m).
//
// CPython: Objects/stringlib/fastsearch.h FASTSEARCH
//
// The naive byte-by-byte loops the bytes method panel used before would
// degrade to quadratic time on adversarial inputs (the
// string_tests.test_adaptive_find case feeds megabyte-scale needles);
// this port restores CPython's worst-case linear behavior.

package objects

import (
	"bytes"
	"math"
)

// Search modes, matching the FAST_* constants.
//
// CPython: Objects/stringlib/fastsearch.h:25 FAST_COUNT / FAST_SEARCH / FAST_RSEARCH
const (
	fastCount   = 0
	fastSearch  = 1
	fastRSearch = 2
)

// The Bloom filter is one machine word wide. gopy targets 64-bit hosts,
// so the table holds 64 buckets, matching STRINGLIB_BLOOM_WIDTH on
// LONG_BIT >= 64 builds.
//
// CPython: Objects/stringlib/fastsearch.h:32 STRINGLIB_BLOOM_WIDTH
const stringlibBloomWidth = 64

// CPython: Objects/stringlib/fastsearch.h:38 STRINGLIB_BLOOM_ADD
func bloomAdd(mask uint64, ch byte) uint64 {
	return mask | (uint64(1) << (uint(ch) & (stringlibBloomWidth - 1)))
}

// CPython: Objects/stringlib/fastsearch.h:40 STRINGLIB_BLOOM
func bloomTest(mask uint64, ch byte) bool {
	return mask&(uint64(1)<<(uint(ch)&(stringlibBloomWidth-1))) != 0
}

// findChar returns the index of ch in s, or -1. For 1-byte chars
// CPython uses memchr; Go's bytes.IndexByte is the equivalent.
//
// CPython: Objects/stringlib/fastsearch.h:49 find_char
func findChar(s []byte, ch byte) int {
	return bytes.IndexByte(s, ch)
}

// rfindChar returns the last index of ch in s, or -1.
//
// CPython: Objects/stringlib/fastsearch.h:113 rfind_char
func rfindChar(s []byte, ch byte) int {
	return bytes.LastIndexByte(s, ch)
}

// lexSearch does a lexicographic search returning the maximal suffix and
// the period of the right half. invertAlphabet flips the ordering, used
// for the second of the two critical-factorization cuts.
//
// CPython: Objects/stringlib/fastsearch.h:200 _lex_search
func lexSearch(needle []byte, invertAlphabet bool) (maxSuffix, period int) {
	lenNeedle := len(needle)
	maxSuffix = 0
	candidate := 1
	k := 0
	period = 1

	for candidate+k < lenNeedle {
		a := needle[candidate+k]
		b := needle[maxSuffix+k]
		var fellShort bool
		if invertAlphabet {
			fellShort = b < a
		} else {
			fellShort = a < b
		}
		switch {
		case fellShort:
			candidate += k + 1
			k = 0
			period = candidate - maxSuffix
		case a == b:
			if k+1 != period {
				k++
			} else {
				candidate += period
				k = 0
			}
		default:
			maxSuffix = candidate
			candidate++
			k = 0
			period = 1
		}
	}
	return maxSuffix, period
}

// factorize computes a critical factorization of needle: the cut index
// where the local period is maximal (which equals the global period).
//
// CPython: Objects/stringlib/fastsearch.h:251 _factorize
func factorize(needle []byte) (cut, period int) {
	cut1, period1 := lexSearch(needle, false)
	cut2, period2 := lexSearch(needle, true)
	if cut1 > cut2 {
		return cut1, period1
	}
	return cut2, period2
}

// Compressed bad-character table parameters.
//
// CPython: Objects/stringlib/fastsearch.h:303 TABLE_SIZE / TABLE_MASK
const (
	stringlibMaxShift  = math.MaxUint8
	stringlibTableSize = 1 << 6
	stringlibTableMask = stringlibTableSize - 1
)

// prework holds the precomputed Two-Way state for a needle.
//
// CPython: Objects/stringlib/fastsearch.h:306 prework
type prework struct {
	needle     []byte
	cut        int
	period     int
	gap        int
	isPeriodic bool
	table      [stringlibTableSize]uint8
}

// preprocess fills a prework for needle.
//
// CPython: Objects/stringlib/fastsearch.h:317 _preprocess
func preprocess(needle []byte) *prework {
	p := &prework{needle: needle}
	lenNeedle := len(needle)
	p.cut, p.period = factorize(needle)
	p.isPeriodic = bytes.Equal(needle[:p.cut], needle[p.period:p.period+p.cut])
	if !p.isPeriodic {
		// A lower bound on the period.
		p.period = maxInt(p.cut, lenNeedle-p.cut) + 1
	}
	// The gap between the last character and the previous occurrence of
	// an equivalent character (modulo TABLE_SIZE).
	p.gap = lenNeedle
	last := needle[lenNeedle-1] & stringlibTableMask
	for i := lenNeedle - 2; i >= 0; i-- {
		if needle[i]&stringlibTableMask == last {
			p.gap = lenNeedle - 1 - i
			break
		}
	}
	// Fill up the compressed Boyer-Moore bad-character table.
	notFoundShift := minInt(lenNeedle, stringlibMaxShift)
	for i := range p.table {
		p.table[i] = uint8(notFoundShift)
	}
	for i := lenNeedle - notFoundShift; i < lenNeedle; i++ {
		p.table[needle[i]&stringlibTableMask] = uint8(lenNeedle - 1 - i)
	}
	return p
}

// twoWay runs the Two-Way scan of needle (preprocessed in p) over
// haystack, returning the match offset or -1.
//
// CPython: Objects/stringlib/fastsearch.h:359 _two_way
func twoWay(haystack []byte, p *prework) int {
	needle := p.needle
	lenNeedle := len(needle)
	lenHaystack := len(haystack)
	cut := p.cut
	period := p.period
	table := &p.table
	gap := p.gap
	gapJumpEnd := minInt(lenNeedle, cut+gap)

	// windowLast is an index into haystack, mirroring the pointer
	// window_last = haystack + len_needle - 1.
	windowLast := lenNeedle - 1

	if p.isPeriodic {
		memory := 0
	periodicWindowLoop:
		for windowLast < lenHaystack {
			for {
				shift := int(table[haystack[windowLast]&stringlibTableMask])
				windowLast += shift
				if shift == 0 {
					break
				}
				if windowLast >= lenHaystack {
					return -1
				}
			}
		noShift:
			window := windowLast - lenNeedle + 1
			i := maxInt(cut, memory)
			for ; i < lenNeedle; i++ {
				if needle[i] != haystack[window+i] {
					if i < gapJumpEnd {
						windowLast += gap
					} else {
						windowLast += i - cut + 1
					}
					memory = 0
					continue periodicWindowLoop
				}
			}
			for i = memory; i < cut; i++ {
				if needle[i] != haystack[window+i] {
					windowLast += period
					memory = lenNeedle - period
					if windowLast >= lenHaystack {
						return -1
					}
					shift := int(table[haystack[windowLast]&stringlibTableMask])
					if shift != 0 {
						// A mismatch was found to the right of where i
						// will next start, so jump at least as far as a
						// first-comparison mismatch would allow.
						memJump := maxInt(cut, memory) - cut + 1
						memory = 0
						windowLast += maxInt(shift, memJump)
						continue periodicWindowLoop
					}
					goto noShift
				}
			}
			return window
		}
	} else {
		period = maxInt(gap, period)
	windowLoop:
		for windowLast < lenHaystack {
			for {
				shift := int(table[haystack[windowLast]&stringlibTableMask])
				windowLast += shift
				if shift == 0 {
					break
				}
				if windowLast >= lenHaystack {
					return -1
				}
			}
			window := windowLast - lenNeedle + 1
			i := cut
			for ; i < lenNeedle; i++ {
				if needle[i] != haystack[window+i] {
					if i < gapJumpEnd {
						windowLast += gap
					} else {
						windowLast += i - cut + 1
					}
					continue windowLoop
				}
			}
			for i := 0; i < cut; i++ {
				if needle[i] != haystack[window+i] {
					windowLast += period
					continue windowLoop
				}
			}
			return window
		}
	}
	return -1
}

// twoWayFind preprocesses needle and runs a single Two-Way search.
//
// CPython: Objects/stringlib/fastsearch.h:496 _two_way_find
func twoWayFind(haystack, needle []byte) int {
	p := preprocess(needle)
	return twoWay(haystack, p)
}

// twoWayCount preprocesses needle and counts non-overlapping matches,
// stopping at maxcount.
//
// CPython: Objects/stringlib/fastsearch.h:508 _two_way_count
func twoWayCount(haystack, needle []byte, maxcount int) int {
	p := preprocess(needle)
	index, count := 0, 0
	lenNeedle := len(needle)
	for {
		result := twoWay(haystack[index:], p)
		if result == -1 {
			return count
		}
		count++
		if count == maxcount {
			return maxcount
		}
		index += result + lenNeedle
	}
}

// defaultFind is the Boyer-Moore/Horspool scan with a Bloom skip, used
// for short inputs and as the search/count workhorse.
//
// CPython: Objects/stringlib/fastsearch.h:545 default_find
func defaultFind(s, p []byte, maxcount, mode int) int {
	n := len(s)
	m := len(p)
	w := n - m
	mlast := m - 1
	count := 0
	gap := mlast
	last := p[mlast]
	// ss[i] corresponds to s[mlast + i].

	var mask uint64
	for i := 0; i < mlast; i++ {
		mask = bloomAdd(mask, p[i])
		if p[i] == last {
			gap = mlast - i - 1
		}
	}
	mask = bloomAdd(mask, last)

	for i := 0; i <= w; i++ {
		if s[mlast+i] == last {
			// candidate match
			j := 0
			for ; j < mlast; j++ {
				if s[i+j] != p[j] {
					break
				}
			}
			if j == mlast {
				if mode != fastCount {
					return i
				}
				count++
				if count == maxcount {
					return maxcount
				}
				i += mlast
				continue
			}
			// miss: is the next character part of the pattern?
			if i+1 <= w && !bloomTest(mask, s[mlast+i+1]) {
				i += m
			} else {
				i += gap
			}
		} else {
			// skip: is the next character part of the pattern?
			if i+1 <= w && !bloomTest(mask, s[mlast+i+1]) {
				i += m
			}
		}
	}
	if mode == fastCount {
		return count
	}
	return -1
}

// adaptiveFind is defaultFind that escalates to the Two-Way algorithm
// once it has matched O(m) characters without a full needle hit, giving
// good worst-case behavior when the needle is a large fraction of the
// haystack.
//
// CPython: Objects/stringlib/fastsearch.h:606 adaptive_find
func adaptiveFind(s, p []byte, maxcount, mode int) int {
	n := len(s)
	m := len(p)
	w := n - m
	mlast := m - 1
	count := 0
	gap := mlast
	hits := 0
	last := p[mlast]

	var mask uint64
	for i := 0; i < mlast; i++ {
		mask = bloomAdd(mask, p[i])
		if p[i] == last {
			gap = mlast - i - 1
		}
	}
	mask = bloomAdd(mask, last)

	for i := 0; i <= w; i++ {
		if s[mlast+i] == last {
			j := 0
			for ; j < mlast; j++ {
				if s[i+j] != p[j] {
					break
				}
			}
			if j == mlast {
				if mode != fastCount {
					return i
				}
				count++
				if count == maxcount {
					return maxcount
				}
				i += mlast
				continue
			}
			hits += j + 1
			if hits > m/4 && w-i > 2000 {
				if mode == fastSearch {
					res := twoWayFind(s[i:], p)
					if res == -1 {
						return -1
					}
					return res + i
				}
				res := twoWayCount(s[i:], p, maxcount-count)
				return res + count
			}
			if i+1 <= w && !bloomTest(mask, s[mlast+i+1]) {
				i += m
			} else {
				i += gap
			}
		} else {
			if i+1 <= w && !bloomTest(mask, s[mlast+i+1]) {
				i += m
			}
		}
	}
	if mode == fastCount {
		return count
	}
	return -1
}

// defaultRfind is the reverse Boyer-Moore scan.
//
// CPython: Objects/stringlib/fastsearch.h:692 default_rfind
func defaultRfind(s, p []byte) int {
	n := len(s)
	m := len(p)
	mlast := m - 1
	skip := m - 1
	w := n - m

	var mask uint64
	mask = bloomAdd(mask, p[0])
	for i := mlast; i > 0; i-- {
		mask = bloomAdd(mask, p[i])
		if p[i] == p[0] {
			skip = i - 1
		}
	}

	for i := w; i >= 0; i-- {
		if s[i] == p[0] {
			j := mlast
			for ; j > 0; j-- {
				if s[i+j] != p[j] {
					break
				}
			}
			if j == 0 {
				return i
			}
			if i > 0 && !bloomTest(mask, s[i-1]) {
				i -= m
			} else {
				i -= skip
			}
		} else {
			if i > 0 && !bloomTest(mask, s[i-1]) {
				i -= m
			}
		}
	}
	return -1
}

// countChar counts occurrences of p0 in s, stopping at maxcount.
//
// CPython: Objects/stringlib/fastsearch.h:736 count_char
func countChar(s []byte, p0 byte, maxcount int) int {
	if maxcount == math.MaxInt {
		return bytes.Count(s, []byte{p0})
	}
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] == p0 {
			count++
			if count == maxcount {
				return maxcount
			}
		}
	}
	return count
}

// stringlibFastSearch is the dispatcher: it picks find_char/rfind_char
// for 1-char needles, then chooses default vs Two-Way vs adaptive scans
// by the same size thresholds CPython uses.
//
// CPython: Objects/stringlib/fastsearch.h:769 FASTSEARCH
func stringlibFastSearch(s, p []byte, maxcount, mode int) int {
	n := len(s)
	m := len(p)
	if n < m || (mode == fastCount && maxcount == 0) {
		return -1
	}

	// special cases
	if m <= 1 {
		if m <= 0 {
			return -1
		}
		switch mode {
		case fastSearch:
			return findChar(s, p[0])
		case fastRSearch:
			return rfindChar(s, p[0])
		default:
			return countChar(s, p[0], maxcount)
		}
	}

	if mode != fastRSearch {
		switch {
		case n < 2500 || (m < 100 && n < 30000) || m < 6:
			return defaultFind(s, p, maxcount, mode)
		case (m>>2)*3 < (n >> 2):
			// 33% threshold: when the needle is not a huge fraction of
			// the haystack, the O(m) Two-Way startup pays off.
			if mode == fastSearch {
				return twoWayFind(s, p)
			}
			return twoWayCount(s, p, maxcount)
		default:
			return adaptiveFind(s, p, maxcount, mode)
		}
	}
	return defaultRfind(s, p)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
