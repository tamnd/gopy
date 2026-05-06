// list.sort: an adaptive, stable, natural mergesort (Timsort+powersort).
// This is a direct port of CPython's listsort. The shape of the
// algorithm is preserved: the same minrun computation, the same
// gallop_left / gallop_right binary-search-then-binary-search pattern,
// the same merge_lo / merge_hi split based on which run is smaller,
// and the same powersort merge stack. See Objects/listsort.txt in
// CPython for the underlying analysis.
//
// One thing that's left out of this port is the pre-sort type
// homogeneity check. CPython uses it to swap key_compare in for
// unsafe_latin_compare, unsafe_long_compare, unsafe_float_compare,
// and unsafe_tuple_compare. Those are correctness-preserving
// optimisations: same answer, fewer indirections per compare. The
// gopy str layout (1677-A) is still strStub, so unsafe_latin_compare
// would have to be rewritten when that lands. Until then every
// compare goes through RichCmpBool, which is what safe_object_compare
// does in CPython too.
//
// CPython: Objects/listobject.c:2900 list_sort_impl

package objects

import "fmt"

const (
	// CPython: Objects/listobject.c:1678 MAX_MERGE_PENDING
	maxMergePending = 64
	// CPython: Objects/listobject.c:1683 MIN_GALLOP
	minGallop = 7
	// CPython: Objects/listobject.c:1686 MERGESTATE_TEMP_SIZE
	mergestateTempSize = 256
	// CPython: Objects/listobject.c:1692 MAX_MINRUN
	maxMinrun = 64
)

// sortslice is a sliding window over the keys array (and, when a key
// function was supplied, a parallel values array). All positions are
// expressed as offsets from base into the underlying slices, which
// stay fixed for the duration of the sort. Advancing the window is
// just a base += d, so it can move both forward (merge_lo) and back
// (merge_hi) without losing the buffer it points into.
//
// CPython: Objects/listobject.c:1631 sortslice
type sortslice struct {
	keys   []Object
	values []Object // nil iff key function was None
	base   int
}

func (s sortslice) keyAt(i int) Object   { return s.keys[s.base+i] }
func (s sortslice) valAt(i int) Object   { return s.values[s.base+i] }
func (s *sortslice) advance(d int)       { s.base += d }
func (s sortslice) hasValues() bool      { return s.values != nil }

// sortsliceCopy puts (src.key[i], src.val[i]) at (dst.key[j], dst.val[j]).
//
// CPython: Objects/listobject.c:1640 sortslice_copy
func sortsliceCopy(dst sortslice, j int, src sortslice, i int) {
	dst.keys[dst.base+j] = src.keys[src.base+i]
	if dst.hasValues() {
		dst.values[dst.base+j] = src.values[src.base+i]
	}
}

// sortsliceCopyIncr writes one entry at *dst then advances both dst
// and src by 1 step forward. CPython's _incr/_decr macros are how the
// merge inner loops walk in lockstep.
//
// CPython: Objects/listobject.c:1648 sortslice_copy_incr
func sortsliceCopyIncr(dst, src *sortslice) {
	dst.keys[dst.base] = src.keys[src.base]
	if dst.hasValues() {
		dst.values[dst.base] = src.values[src.base]
	}
	dst.base++
	src.base++
}

// sortsliceCopyDecr is the merge_hi cousin: write at *dst then step
// both dst and src one slot earlier.
//
// CPython: Objects/listobject.c:1657 sortslice_copy_decr
func sortsliceCopyDecr(dst, src *sortslice) {
	dst.keys[dst.base] = src.keys[src.base]
	if dst.hasValues() {
		dst.values[dst.base] = src.values[src.base]
	}
	dst.base--
	src.base--
}

// sortsliceMemcpy bulk-copies n entries from src+i into dst+j. Used
// by gallop-driven copy bursts and to seed the temp buffer before a
// merge. Backing arrays may differ.
//
// CPython: Objects/listobject.c:1666 sortslice_memcpy
func sortsliceMemcpy(dst sortslice, j int, src sortslice, i int, n int) {
	copy(dst.keys[dst.base+j:dst.base+j+n], src.keys[src.base+i:src.base+i+n])
	if dst.hasValues() {
		copy(dst.values[dst.base+j:dst.base+j+n], src.values[src.base+i:src.base+i+n])
	}
}

// sortsliceMemmove is sortsliceMemcpy with overlapping ranges allowed.
// In Go copy() already handles overlap so this is the same call.
//
// CPython: Objects/listobject.c:1672 sortslice_memmove
func sortsliceMemmove(dst sortslice, j int, src sortslice, i int, n int) {
	sortsliceMemcpy(dst, j, src, i, n)
}

// reverseSlice reverses keys[lo:hi] in place and the parallel slice
// of values when present. Used by countRun to flip a descending run
// into ascending and by reverse=True at the entry/exit.
//
// CPython: Objects/listobject.c:1623 reverse_slice
func reverseSlice(s sortslice, lo, hi int) {
	for lo+1 < hi {
		hi--
		s.keys[s.base+lo], s.keys[s.base+hi] = s.keys[s.base+hi], s.keys[s.base+lo]
		if s.hasValues() {
			s.values[s.base+lo], s.values[s.base+hi] = s.values[s.base+hi], s.values[s.base+lo]
		}
		lo++
	}
}

// sliceRun is one entry on the pending-runs stack. base points at the
// run's first element; len is its length; power is the powersort depth
// computed when the run was pushed.
//
// CPython: Objects/listobject.c:1696 s_slice
type sliceRun struct {
	base  sortslice
	len   int
	power int
}

// mergeState holds everything that survives between the per-run
// passes: temp buffer, pending stack, and the dynamic gallop
// threshold that nudges up after a merge wastes time in linear mode.
//
// CPython: Objects/listobject.c:1701 MergeState
type mergeState struct {
	a         sortslice // temp buffer for merging
	alloced   int
	n         int
	pending   [maxMergePending]sliceRun
	minGallop int
	listlen   int
	basekeys  []Object // start of the keys slice; pending power is in elements offset from this
	keyCmp    func(a, b Object) (int, error)
}

// compareLT returns 1 if a < b, 0 if not, -1 on error. Mirrors the
// IFLT(...) macro: the whole sort is written in terms of strict-less
// rather than richer ordering.
//
// CPython: Objects/listobject.c:1606 IFLT (the macro the algorithm uses)
func compareLT(ms *mergeState, a, b Object) (int, error) {
	return ms.keyCmp(a, b)
}

// safeObjectCompare is the compare used for any pair of keys. CPython
// dispatches to one of unsafe_{latin,long,float,tuple}_compare when
// the pre-sort check proves it's safe; we always come here.
//
// CPython: Objects/listobject.c:2724 safe_object_compare
func safeObjectCompare(a, b Object) (int, error) {
	lt, err := RichCmpBool(a, b, CompareLT)
	if err != nil {
		return -1, err
	}
	if lt {
		return 1, nil
	}
	return 0, nil
}

// mergeInit sets up the temp buffer and stack. With a key function
// the temp area is split between keys and values so a single
// allocation backs both halves.
//
// CPython: Objects/listobject.c:2185 merge_init
func mergeInit(ms *mergeState, listSize int, hasKeyfunc bool, lo sortslice) {
	if hasKeyfunc {
		alloced := (listSize + 1) / 2
		if mergestateTempSize/2 < alloced {
			alloced = mergestateTempSize / 2
		}
		ms.alloced = alloced
		ms.a = sortslice{
			keys:   make([]Object, alloced),
			values: make([]Object, alloced),
		}
	} else {
		ms.alloced = mergestateTempSize
		ms.a = sortslice{keys: make([]Object, mergestateTempSize)}
	}
	ms.n = 0
	ms.minGallop = minGallop
	ms.listlen = listSize
	ms.basekeys = lo.keys
}

// mergeGetmem grows the temp buffer to hold at least need entries.
// CPython grows by doubling; we let the slice allocator decide and
// just allocate exactly need.
//
// CPython: Objects/listobject.c:2233 merge_getmem
func mergeGetmem(ms *mergeState, need int) {
	if need <= ms.alloced {
		return
	}
	ms.a.keys = make([]Object, need)
	if ms.a.values != nil {
		ms.a.values = make([]Object, need)
	}
	ms.a.base = 0
	ms.alloced = need
}

// binarysort runs binary insertion sort on a[:n], assuming a[:ok] is
// already sorted. CPython falls back to this for any run shorter than
// minrun; the algorithm boosts the run rather than swapping in a
// different sort, because all the merge invariants depend on every
// run being at least minrun long.
//
// CPython: Objects/listobject.c:1766 binarysort
func binarysort(ms *mergeState, ss sortslice, n, ok int) error {
	if ok == 0 {
		ok++
	}
	for ; ok < n; ok++ {
		L, R := 0, ok
		pivot := ss.keys[ss.base+ok]
		var pivotVal Object
		if ss.hasValues() {
			pivotVal = ss.values[ss.base+ok]
		}
		for L < R {
			M := (L + R) >> 1
			lt, err := compareLT(ms, pivot, ss.keys[ss.base+M])
			if err != nil {
				return err
			}
			if lt > 0 {
				R = M
			} else {
				L = M + 1
			}
		}
		for M := ok; M > L; M-- {
			ss.keys[ss.base+M] = ss.keys[ss.base+M-1]
		}
		ss.keys[ss.base+L] = pivot
		if ss.hasValues() {
			for M := ok; M > L; M-- {
				ss.values[ss.base+M] = ss.values[ss.base+M-1]
			}
			ss.values[ss.base+L] = pivotVal
		}
	}
	return nil
}

// countRun finds the next monotone run starting at slo and rotates a
// descending run in place so that it's ascending on return. Equal
// neighbors are not split off into a separate run. Returns the run
// length.
//
// CPython: Objects/listobject.c:1904 count_run
func countRun(ms *mergeState, slo sortslice, nremaining int) (int, error) {
	n := 1
	for ; n < nremaining; n++ {
		smaller, err := compareLT(ms, slo.keys[slo.base+n], slo.keys[slo.base+n-1])
		if err != nil {
			return -1, err
		}
		if smaller > 0 {
			break
		}
	}
	if n == nremaining {
		return n, nil
	}
	if n > 1 {
		lt, err := compareLT(ms, slo.keys[slo.base], slo.keys[slo.base+n-1])
		if err != nil {
			return -1, err
		}
		if lt > 0 {
			return n, nil
		}
		reverseSlice(slo, 0, n)
	}
	n++
	neq := 0
	for ; n < nremaining; n++ {
		smaller, err := compareLT(ms, slo.keys[slo.base+n], slo.keys[slo.base+n-1])
		if err != nil {
			return -1, err
		}
		if smaller > 0 {
			if neq != 0 {
				neq++
				reverseSlice(slo, n-neq, n)
				neq = 0
			}
		} else {
			larger, err := compareLT(ms, slo.keys[slo.base+n-1], slo.keys[slo.base+n])
			if err != nil {
				return -1, err
			}
			if larger > 0 {
				break
			}
			neq++
		}
	}
	if neq != 0 {
		neq++
		reverseSlice(slo, n-neq, n)
	}
	reverseSlice(slo, 0, n)
	for ; n < nremaining; n++ {
		smaller, err := compareLT(ms, slo.keys[slo.base+n], slo.keys[slo.base+n-1])
		if err != nil {
			return -1, err
		}
		if smaller > 0 {
			break
		}
	}
	return n, nil
}

// gallopLeft returns k in [0, n] such that a[k-1] < key <= a[k]: the
// leftmost insertion point that still keeps the array sorted. Hint
// is the prior expected location; closer hints make the doubling
// search converge faster.
//
// CPython: Objects/listobject.c:2020 gallop_left
func gallopLeft(ms *mergeState, key Object, a []Object, base, n, hint int) (int, error) {
	lastofs, ofs := 0, 1
	lt, err := compareLT(ms, a[base+hint], key)
	if err != nil {
		return -1, err
	}
	if lt > 0 {
		maxofs := n - hint
		for ofs < maxofs {
			lt2, err := compareLT(ms, a[base+hint+ofs], key)
			if err != nil {
				return -1, err
			}
			if lt2 > 0 {
				lastofs = ofs
				ofs = (ofs << 1) + 1
			} else {
				break
			}
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		lastofs += hint
		ofs += hint
	} else {
		maxofs := hint + 1
		for ofs < maxofs {
			lt2, err := compareLT(ms, a[base+hint-ofs], key)
			if err != nil {
				return -1, err
			}
			if lt2 > 0 {
				break
			}
			lastofs = ofs
			ofs = (ofs << 1) + 1
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		lastofs, ofs = hint-ofs, hint-lastofs
	}
	lastofs++
	for lastofs < ofs {
		m := lastofs + ((ofs - lastofs) >> 1)
		lt2, err := compareLT(ms, a[base+m], key)
		if err != nil {
			return -1, err
		}
		if lt2 > 0 {
			lastofs = m + 1
		} else {
			ofs = m
		}
	}
	return ofs, nil
}

// gallopRight is the gallop_left twin returning the position
// immediately after the rightmost equal element instead of before
// the leftmost. The two are kept as separate routines because the
// asymmetry is hidden inside the strict-less compare.
//
// CPython: Objects/listobject.c:2109 gallop_right
func gallopRight(ms *mergeState, key Object, a []Object, base, n, hint int) (int, error) {
	lastofs, ofs := 0, 1
	lt, err := compareLT(ms, key, a[base+hint])
	if err != nil {
		return -1, err
	}
	if lt > 0 {
		maxofs := hint + 1
		for ofs < maxofs {
			lt2, err := compareLT(ms, key, a[base+hint-ofs])
			if err != nil {
				return -1, err
			}
			if lt2 > 0 {
				lastofs = ofs
				ofs = (ofs << 1) + 1
			} else {
				break
			}
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		lastofs, ofs = hint-ofs, hint-lastofs
	} else {
		maxofs := n - hint
		for ofs < maxofs {
			lt2, err := compareLT(ms, key, a[base+hint+ofs])
			if err != nil {
				return -1, err
			}
			if lt2 > 0 {
				break
			}
			lastofs = ofs
			ofs = (ofs << 1) + 1
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		lastofs += hint
		ofs += hint
	}
	lastofs++
	for lastofs < ofs {
		m := lastofs + ((ofs - lastofs) >> 1)
		lt2, err := compareLT(ms, key, a[base+m])
		if err != nil {
			return -1, err
		}
		if lt2 > 0 {
			ofs = m
		} else {
			lastofs = m + 1
		}
	}
	return ofs, nil
}

// mergeLo merges adjacent runs ssa and ssb in place, choosing the
// smaller-on-the-left layout because we know na <= nb. The temp
// buffer holds ssa during the merge so writes to dest don't trample
// reads from ssb (ssb is in-place after na slots).
//
// CPython: Objects/listobject.c:2272 merge_lo
func mergeLo(ms *mergeState, ssa sortslice, na int, ssb sortslice, nb int) error {
	mergeGetmem(ms, na)
	sortsliceMemcpy(ms.a, 0, ssa, 0, na)
	dest := ssa
	ssa = sortslice{keys: ms.a.keys, values: ms.a.values, base: 0}

	sortsliceCopyIncr(&dest, &ssb)
	nb--
	if nb == 0 {
		return mergeFinishLo(dest, ssa, na)
	}
	if na == 1 {
		sortsliceMemmove(dest, 0, ssb, 0, nb)
		sortsliceCopy(dest, nb, ssa, 0)
		return nil
	}

	mg := ms.minGallop
mainLoop:
	for {
		acount, bcount := 0, 0
		for {
			lt, err := compareLT(ms, ssb.keys[ssb.base], ssa.keys[ssa.base])
			if err != nil {
				_ = mergeFinishLo(dest, ssa, na)
				return err
			}
			if lt > 0 {
				sortsliceCopyIncr(&dest, &ssb)
				bcount++
				acount = 0
				nb--
				if nb == 0 {
					return mergeFinishLo(dest, ssa, na)
				}
				if bcount >= mg {
					break
				}
			} else {
				sortsliceCopyIncr(&dest, &ssa)
				acount++
				bcount = 0
				na--
				if na == 1 {
					sortsliceMemmove(dest, 0, ssb, 0, nb)
					sortsliceCopy(dest, nb, ssa, 0)
					return nil
				}
				if acount >= mg {
					break
				}
			}
		}

		mg++
		for {
			mg -= boolToInt(mg > 1)
			ms.minGallop = mg

			k, err := gallopRight(ms, ssb.keys[ssb.base], ssa.keys, ssa.base, na, 0)
			if err != nil {
				_ = mergeFinishLo(dest, ssa, na)
				return err
			}
			acount = k
			if k != 0 {
				sortsliceMemcpy(dest, 0, ssa, 0, k)
				dest.advance(k)
				ssa.advance(k)
				na -= k
				if na == 1 {
					sortsliceMemmove(dest, 0, ssb, 0, nb)
					sortsliceCopy(dest, nb, ssa, 0)
					return nil
				}
				if na == 0 {
					return mergeFinishLo(dest, ssa, na)
				}
			}
			sortsliceCopyIncr(&dest, &ssb)
			nb--
			if nb == 0 {
				return mergeFinishLo(dest, ssa, na)
			}

			k, err = gallopLeft(ms, ssa.keys[ssa.base], ssb.keys, ssb.base, nb, 0)
			if err != nil {
				_ = mergeFinishLo(dest, ssa, na)
				return err
			}
			bcount = k
			if k != 0 {
				sortsliceMemmove(dest, 0, ssb, 0, k)
				dest.advance(k)
				ssb.advance(k)
				nb -= k
				if nb == 0 {
					return mergeFinishLo(dest, ssa, na)
				}
			}
			sortsliceCopyIncr(&dest, &ssa)
			na--
			if na == 1 {
				sortsliceMemmove(dest, 0, ssb, 0, nb)
				sortsliceCopy(dest, nb, ssa, 0)
				return nil
			}
			if acount < minGallop && bcount < minGallop {
				break
			}
		}
		mg++
		ms.minGallop = mg
		continue mainLoop
	}
}

// mergeFinishLo flushes ssa back into dest at the current dest
// position. CPython runs this from both the Succeed and Fail labels,
// because in merge_lo ssa lives in the temp buffer; without the flush
// the dest buffer's tail keeps the pre-merge values that were sitting
// in the underlying array before we copied ssa into temp.
//
// CPython: Objects/listobject.c:2386 (the Fail label inside merge_lo)
func mergeFinishLo(dest, ssa sortslice, na int) error {
	if na > 0 {
		sortsliceMemcpy(dest, 0, ssa, 0, na)
	}
	return nil
}

// mergeHi mirrors mergeLo for the opposite size disparity. It walks
// both runs from the right end, copying the larger of the trailing
// elements into the trailing slot, so that the temp buffer holds
// only the (smaller) ssb run.
//
// CPython: Objects/listobject.c:2404 merge_hi
func mergeHi(ms *mergeState, ssa sortslice, na int, ssb sortslice, nb int) error {
	mergeGetmem(ms, nb)
	dest := ssb
	dest.advance(nb - 1)
	sortsliceMemcpy(ms.a, 0, ssb, 0, nb)
	basea := ssa
	baseb := sortslice{keys: ms.a.keys, values: ms.a.values, base: 0}
	ssb = sortslice{keys: ms.a.keys, values: ms.a.values, base: nb - 1}
	ssa.advance(na - 1)

	sortsliceCopyDecr(&dest, &ssa)
	na--
	if na == 0 {
		return mergeFinishHi(dest, baseb, nb)
	}
	if nb == 1 {
		dest.advance(-na)
		ssa.advance(-na)
		sortsliceMemmove(dest, 1, ssa, 1, na)
		sortsliceCopy(dest, 0, ssb, 0)
		return nil
	}

	mg := ms.minGallop
mainLoop:
	for {
		acount, bcount := 0, 0
		for {
			lt, err := compareLT(ms, ssb.keys[ssb.base], ssa.keys[ssa.base])
			if err != nil {
				_ = mergeFinishHi(dest, baseb, nb)
				return err
			}
			if lt > 0 {
				sortsliceCopyDecr(&dest, &ssa)
				acount++
				bcount = 0
				na--
				if na == 0 {
					return mergeFinishHi(dest, baseb, nb)
				}
				if acount >= mg {
					break
				}
			} else {
				sortsliceCopyDecr(&dest, &ssb)
				bcount++
				acount = 0
				nb--
				if nb == 1 {
					dest.advance(-na)
					ssa.advance(-na)
					sortsliceMemmove(dest, 1, ssa, 1, na)
					sortsliceCopy(dest, 0, ssb, 0)
					return nil
				}
				if bcount >= mg {
					break
				}
			}
		}

		mg++
		for {
			mg -= boolToInt(mg > 1)
			ms.minGallop = mg

			k, err := gallopRight(ms, ssb.keys[ssb.base], basea.keys, basea.base, na, na-1)
			if err != nil {
				_ = mergeFinishHi(dest, baseb, nb)
				return err
			}
			k = na - k
			acount = k
			if k != 0 {
				dest.advance(-k)
				ssa.advance(-k)
				sortsliceMemmove(dest, 1, ssa, 1, k)
				na -= k
				if na == 0 {
					return mergeFinishHi(dest, baseb, nb)
				}
			}
			sortsliceCopyDecr(&dest, &ssb)
			nb--
			if nb == 1 {
				dest.advance(-na)
				ssa.advance(-na)
				sortsliceMemmove(dest, 1, ssa, 1, na)
				sortsliceCopy(dest, 0, ssb, 0)
				return nil
			}

			k, err = gallopLeft(ms, ssa.keys[ssa.base], baseb.keys, baseb.base, nb, nb-1)
			if err != nil {
				_ = mergeFinishHi(dest, baseb, nb)
				return err
			}
			k = nb - k
			bcount = k
			if k != 0 {
				dest.advance(-k)
				ssb.advance(-k)
				sortsliceMemcpy(dest, 1, ssb, 1, k)
				nb -= k
				if nb == 1 {
					dest.advance(-na)
					ssa.advance(-na)
					sortsliceMemmove(dest, 1, ssa, 1, na)
					sortsliceCopy(dest, 0, ssb, 0)
					return nil
				}
				if nb == 0 {
					return mergeFinishHi(dest, baseb, nb)
				}
			}
			sortsliceCopyDecr(&dest, &ssa)
			na--
			if na == 0 {
				return mergeFinishHi(dest, baseb, nb)
			}
			if acount < minGallop && bcount < minGallop {
				break
			}
		}
		mg++
		ms.minGallop = mg
		continue mainLoop
	}
}

// mergeFinishHi flushes the leftover ssb-from-temp back into dest
// after a compare error in mergeHi. The base position of dest tells
// us where the unwritten tail begins.
//
// CPython: Objects/listobject.c:2526 (the Fail label inside merge_hi)
func mergeFinishHi(dest, baseb sortslice, nb int) error {
	if nb > 0 {
		sortsliceMemcpy(dest, -(nb - 1), baseb, 0, nb)
	}
	return nil
}

// mergeAt merges runs i and i+1 on the pending stack. The smaller of
// the two ends up in the temp buffer; the picked merge_lo / merge_hi
// arrangement keeps the algorithm using only O(min(na,nb)) extra
// space.
//
// CPython: Objects/listobject.c:2543 merge_at
func mergeAt(ms *mergeState, i int) error {
	ssa := ms.pending[i].base
	na := ms.pending[i].len
	ssb := ms.pending[i+1].base
	nb := ms.pending[i+1].len

	ms.pending[i].len = na + nb
	if i == ms.n-3 {
		ms.pending[i+1] = ms.pending[i+2]
	}
	ms.n--

	k, err := gallopRight(ms, ssb.keys[ssb.base], ssa.keys, ssa.base, na, 0)
	if err != nil {
		return err
	}
	ssa.advance(k)
	na -= k
	if na == 0 {
		return nil
	}

	nb, err = gallopLeft(ms, ssa.keys[ssa.base+na-1], ssb.keys, ssb.base, nb, nb-1)
	if err != nil {
		return err
	}
	if nb <= 0 {
		return nil
	}

	if na <= nb {
		return mergeLo(ms, ssa, na, ssb, nb)
	}
	return mergeHi(ms, ssa, na, ssb, nb)
}

// powerloop computes the powersort merge depth for the topmost
// pending run paired with a freshly-found run of length n2. The
// emulated long division avoids overflow that a direct (a/n) would
// hit on big lists.
//
// CPython: Objects/listobject.c:2603 powerloop
func powerloop(s1, n1, n2, n int) int {
	result := 0
	a := 2*s1 + n1
	b := a + n1 + n2
	for {
		result++
		if a >= n {
			a -= n
			b -= n
		} else if b >= n {
			break
		}
		a <<= 1
		b <<= 1
	}
	return result
}

// foundNewRun is the powersort merge-decision point: when the next
// run has been identified, merge any pending runs whose power exceeds
// the new run's power before pushing it. That's what gives the
// algorithm its bounded merge stack and near-optimal merge tree.
//
// CPython: Objects/listobject.c:2650 found_new_run
func foundNewRun(ms *mergeState, n2 int) error {
	if ms.n == 0 {
		return nil
	}
	p := &ms.pending
	s1 := p[ms.n-1].base.base // start index relative to basekeys
	n1 := p[ms.n-1].len
	power := powerloop(s1, n1, n2, ms.listlen)
	for ms.n > 1 && p[ms.n-2].power > power {
		if err := mergeAt(ms, ms.n-2); err != nil {
			return err
		}
	}
	p[ms.n-1].power = power
	return nil
}

// mergeForceCollapse drains the pending stack at end-of-input,
// merging the smaller-of-two-neighbors pair on each step. This is
// the cleanup pass that turns the final stack of runs into one
// sorted run.
//
// CPython: Objects/listobject.c:2675 merge_force_collapse
func mergeForceCollapse(ms *mergeState) error {
	for ms.n > 1 {
		n := ms.n - 2
		if n > 0 {
			lt, err := boolLT(ms.pending[n-1].len, ms.pending[n+1].len)
			if err != nil {
				return err
			}
			if lt {
				n--
			}
		}
		if err := mergeAt(ms, n); err != nil {
			return err
		}
	}
	return nil
}

func boolLT(a, b int) (bool, error) { return a < b, nil }

// mergeComputeMinrun rounds n down to a power of two between 32 and
// 64, biased so n/minrun is close to a power of two from below. Tiny
// arrays sort directly via binary insertion sort with no run finding.
//
// CPython: Objects/listobject.c:2701 merge_compute_minrun
func mergeComputeMinrun(n int) int {
	r := 0
	for n >= maxMinrun {
		r |= n & 1
		n >>= 1
	}
	return n + r
}

// Sort sorts l in place using Timsort+powersort. keyfunc may be nil
// (no key projection); reverse=true sorts descending while keeping
// stability by reversing the input, sorting forward, then reversing
// the output.
//
// The list is "detached" by saving its items and resetting len to 0
// for the duration of the sort: any callback that mutates the list
// during the comparison sees an empty list rather than corrupting
// the slice we're sorting under it. CPython uses the same trick to
// avoid core dumps from comparison-time mutation.
//
// CPython: Objects/listobject.c:2900 list_sort_impl
func (l *List) Sort(keyfunc Object, reverse bool) (retErr error) {
	if keyfunc != nil && IsNone(keyfunc) {
		keyfunc = nil
	}

	savedItems := l.items
	savedLen := len(savedItems)
	l.items = nil
	l.size = 0
	defer func() {
		mutated := l.items
		l.items = savedItems
		l.size = int64(len(savedItems))
		if retErr == nil && len(mutated) != 0 {
			retErr = fmt.Errorf("ValueError: list modified during sort")
		}
	}()

	var lo sortslice
	var keys []Object
	if keyfunc == nil {
		lo = sortslice{keys: savedItems}
	} else {
		keys = make([]Object, savedLen)
		for i := 0; i < savedLen; i++ {
			k, err := CallOneArg(keyfunc, savedItems[i])
			if err != nil {
				return err
			}
			keys[i] = k
		}
		lo = sortslice{keys: keys, values: savedItems}
	}

	var ms mergeState
	ms.keyCmp = safeObjectCompare
	mergeInit(&ms, savedLen, keyfunc != nil, lo)

	nremaining := savedLen
	if nremaining < 2 {
		return nil
	}

	if reverse {
		if keys != nil {
			reverseSlice(sortslice{keys: keys}, 0, savedLen)
		}
		reverseSlice(sortslice{keys: savedItems}, 0, savedLen)
	}

	minrun := mergeComputeMinrun(nremaining)
	for nremaining > 0 {
		n, err := countRun(&ms, lo, nremaining)
		if err != nil {
			return err
		}
		if n < minrun {
			force := nremaining
			if minrun < force {
				force = minrun
			}
			if err := binarysort(&ms, lo, force, n); err != nil {
				return err
			}
			n = force
		}
		if err := foundNewRun(&ms, n); err != nil {
			return err
		}
		if ms.n >= maxMergePending {
			return fmt.Errorf("RuntimeError: merge stack overflow")
		}
		ms.pending[ms.n].base = lo
		ms.pending[ms.n].len = n
		ms.n++
		lo.advance(n)
		nremaining -= n
	}

	if err := mergeForceCollapse(&ms); err != nil {
		return err
	}

	if reverse && savedLen > 1 {
		reverseSlice(sortslice{keys: savedItems}, 0, savedLen)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
