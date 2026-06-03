package _sre

// Pattern type bound to the phase-3 bytecode engine.
//
// `_sre.compile` is the boundary between the Python-side compiler
// (`Lib/re/_compiler.py`) and this engine. The compiler hands us a
// list of ints encoding the SRE bytecode; we store it on the Pattern
// instance and drive `match`/`search` against it. No `regexp` import,
// no re-parsing of the source pattern string.
//
// CPython: Modules/_sre/sre.c:1621 _sre_compile_impl

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// compiledPattern is the engine-facing view of a Pattern instance.
// It mirrors the fields the C SRE_Pattern struct exposes to the
// matcher: bytecode buffer plus capture-group metadata.
//
// CPython: Modules/_sre/sre.h:31 SRE_Pattern
type compiledPattern struct {
	code    []uint32
	groups  int
	flags   uint32
	pattern string // source string, for repr / debugging
	isbytes bool
}

// patternByInstance is the engine-side replacement for the previous
// RE2 patternStore. Keyed by the Pattern Instance pointer.
var patternByInstance = map[objects.Object]*compiledPattern{}

func lookupCompiled(o objects.Object) (*compiledPattern, bool) {
	cp, ok := patternByInstance[o]
	return cp, ok
}

// listToCode decodes the bytecode list `_compiler.py` passes as
// args[2]. Each element must be a non-negative Python int that fits in
// uint32; CPython's `_sre.compile` checks the same.
//
// CPython: Modules/_sre/sre.c:1583 sre_compile (loop that copies code into
// pattern->code).
func listToCode(o objects.Object) ([]uint32, error) {
	l, ok := o.(*objects.List)
	if !ok {
		return nil, fmt.Errorf("TypeError: code must be a list")
	}
	out := make([]uint32, l.Len())
	for i := 0; i < l.Len(); i++ {
		it := l.Item(i)
		intv, ok := it.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: code[%d] must be int", i)
		}
		v, _ := intv.Int64()
		if v < 0 || v > 0xFFFFFFFF {
			return nil, fmt.Errorf("OverflowError: code[%d]=%d out of range", i, v)
		}
		out[i] = uint32(v)
	}
	return out, nil
}

// inputForString turns a Python str or bytes into the []int32 the
// engine expects (one slot per code point for str, one slot per byte
// for bytes) and reports whether the input is bytes.
func inputForString(o objects.Object) ([]int32, bool, error) {
	if s, ok := o.(*objects.Unicode); ok {
		r := []rune(s.Value())
		out := make([]int32, len(r))
		for i, c := range r {
			out[i] = int32(c)
		}
		return out, false, nil
	}
	// Any buffer-bearing object (bytes, bytearray, memoryview, array) is
	// matched as a byte string, mirroring CPython's getstring(), which calls
	// PyObject_GetBuffer for non-str inputs.
	//
	// CPython: Modules/_sre/sre.c:1308 getstring
	if buf, ok := objects.AsBytesLike(o); ok {
		out := make([]int32, len(buf))
		for i, by := range buf {
			out[i] = int32(by)
		}
		return out, true, nil
	}
	return nil, false, fmt.Errorf("TypeError: expected str or bytes")
}

// extractInt pulls an int out of a Python Int argument with a default
// when the slot is missing or not an int.
func extractInt(args []objects.Object, idx int, def int) int {
	if idx >= len(args) {
		return def
	}
	if i, ok := args[idx].(*objects.Int); ok {
		v, _ := i.Int64()
		return int(v)
	}
	return def
}

// sreCompile is the bytecode-aware replacement of the previous
// RE2-wrapping compiler. Honours every argument `_compiler.py` passes,
// most importantly `code` (args[2]).
//
// CPython: Modules/_sre/sre.c:1621 _sre_compile_impl
func sreCompile(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 6 {
		return nil, fmt.Errorf("TypeError: compile() takes 6 arguments (%d given)", len(args))
	}

	patStr := ""
	isbytes := false
	switch p := args[0].(type) {
	case *objects.Unicode:
		patStr = p.Value()
	case *objects.Bytes:
		patStr = string(p.Bytes())
		isbytes = true
	default:
		if !objects.IsNone(args[0]) {
			s, err := objects.Str(args[0])
			if err != nil {
				return nil, fmt.Errorf("TypeError: compile() pattern must be str, bytes, or None")
			}
			patStr = s
		}
	}

	flags := int64(0)
	if i, ok := args[1].(*objects.Int); ok {
		flags, _ = i.Int64()
	}

	code, cerr := listToCode(args[2])
	if cerr != nil {
		return nil, cerr
	}

	groups := int64(0)
	if i, ok := args[3].(*objects.Int); ok {
		groups, _ = i.Int64()
	}

	groupindex := args[4]
	indexgroup := args[5]

	cp := &compiledPattern{
		code:    code,
		groups:  int(groups),
		flags:   uint32(flags),
		pattern: patStr,
		isbytes: isbytes,
	}

	inst := objects.NewInstance(PatternType)
	d := inst.EnsureDict()
	_ = d.SetItem(objects.NewStr("pattern"), args[0])
	_ = d.SetItem(objects.NewStr("flags"), objects.NewInt(flags))
	_ = d.SetItem(objects.NewStr("groups"), objects.NewInt(groups))
	_ = d.SetItem(objects.NewStr("groupindex"), groupindex)
	_ = d.SetItem(objects.NewStr("indexgroup"), indexgroup)
	patternByInstance[inst] = cp
	return inst, nil
}

// matchPosEnd resolves (string, pos, endpos) from the trailing args of
// a Pattern method call. Returns the input slice the engine consumes,
// the original Python string object, and the (pos, endpos) the Match
// will surface.
func matchPosEnd(cp *compiledPattern, args []objects.Object) ([]int32, objects.Object, int, int, error) {
	if len(args) < 2 {
		return nil, nil, 0, 0, fmt.Errorf("TypeError: method needs a string argument")
	}
	in, isbytes, err := inputForString(args[1])
	if err != nil {
		return nil, nil, 0, 0, err
	}
	if isbytes != cp.isbytes {
		return nil, nil, 0, 0, fmt.Errorf("TypeError: cannot use a %s pattern on a %s-like object",
			boolStr(cp.isbytes, "bytes", "string"),
			boolStr(isbytes, "bytes", "string"))
	}
	pos := extractInt(args, 2, 0)
	endpos := extractInt(args, 3, len(in))
	if pos < 0 {
		pos = 0
	}
	if endpos > len(in) {
		endpos = len(in)
	}
	if pos > endpos {
		pos = endpos
	}
	return in, args[1], pos, endpos, nil
}

func boolStr(b bool, ifTrue, ifFalse string) string {
	if b {
		return ifTrue
	}
	return ifFalse
}

// engineMatch invokes the phase-3 engine in match (anchored) mode
// against a freshly-built state and returns the state on success or
// nil if no match.
func engineMatch(cp *compiledPattern, in []int32, pos, endpos int, matchAll, mustAdvance bool) (*state, error) {
	st := newState(in, cp.code, cp.groups, pos, endpos, cp.isbytes)
	st.matchAll = matchAll
	st.mustAdvance = mustAdvance
	r, err := match(st, 0, true)
	if err != nil {
		return nil, fmt.Errorf("re.error: %w", err)
	}
	if r <= 0 {
		return nil, nil
	}
	return st, nil
}

// engineSearch invokes the engine's search() helper.
func engineSearch(cp *compiledPattern, in []int32, pos, endpos int, mustAdvance bool) (*state, error) {
	st := newState(in, cp.code, cp.groups, pos, endpos, cp.isbytes)
	st.mustAdvance = mustAdvance
	r, err := search(st, 0)
	if err != nil {
		return nil, fmt.Errorf("re.error: %w", err)
	}
	if r <= 0 {
		return nil, nil
	}
	return st, nil
}

// makeMatch builds a SRE_Match instance from a successful engine
// state. The returned Match exposes the same `string`, `re`, `pos`,
// `endpos`, `lastindex` attributes the previous RE2 path emitted; the
// underlying side store records a locs slice in the
// `[group0_start, group0_end, group1_start, group1_end, ...]` shape
// that the previous Match methods already consume.
//
// CPython: Modules/_sre/sre.c:2900 pattern_new_match
func makeMatch(cp *compiledPattern, st *state, src objects.Object, patInst objects.Object, pos, endpos int) objects.Object {
	inst := objects.NewInstance(MatchType)
	d := inst.EnsureDict()
	_ = d.SetItem(objects.NewStr("string"), src)
	_ = d.SetItem(objects.NewStr("re"), patInst)
	_ = d.SetItem(objects.NewStr("pos"), objects.NewInt(int64(pos)))
	_ = d.SetItem(objects.NewStr("endpos"), objects.NewInt(int64(endpos)))
	lastIndex := objects.None()
	if st.lastindex > 0 {
		lastIndex = objects.NewInt(int64(st.lastindex))
	}
	_ = d.SetItem(objects.NewStr("lastindex"), lastIndex)

	// Build locs as the previous matchData shape: group 0 spans
	// (st.start, st.ptr); group N (N>=1) spans (mark[2N-2], mark[2N-1]).
	// Unset marks become -1 — same sentinel the locs[] consumers expect.
	locs := make([]int, 2+2*cp.groups)
	locs[0] = st.start
	locs[1] = st.ptr
	for i := 0; i < 2*cp.groups; i++ {
		locs[2+i] = st.mark[i] // already -1 for unset
	}
	srcStr := ""
	isBytes := false
	if u, ok := src.(*objects.Unicode); ok {
		srcStr = u.Value()
	} else if b, ok := src.(*objects.Bytes); ok {
		srcStr = string(b.Bytes())
		isBytes = true
	}
	matchStore[inst] = &matchData{locs: locs, s: srcStr, isBytes: isBytes}
	_ = d.SetItem(objects.NewStr("regs"), buildRegs(locs))
	_ = d.SetItem(objects.NewStr("lastgroup"), computeLastgroup(patInst, st.lastindex))
	return inst
}

// patternMatch implements SRE_Pattern.match(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2631 _sre_SRE_Pattern_match_impl
func patternMatch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: match() called on non-Pattern")
	}
	in, src, pos, endpos, err := matchPosEnd(cp, args)
	if err != nil {
		return nil, err
	}
	st, err := engineMatch(cp, in, pos, endpos, false, false)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return objects.None(), nil
	}
	return makeMatch(cp, st, src, args[0], pos, endpos), nil
}

// patternFullmatch implements SRE_Pattern.fullmatch(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2672 _sre_SRE_Pattern_fullmatch_impl
func patternFullmatch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: fullmatch() called on non-Pattern")
	}
	in, src, pos, endpos, err := matchPosEnd(cp, args)
	if err != nil {
		return nil, err
	}
	st, err := engineMatch(cp, in, pos, endpos, true, false)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return objects.None(), nil
	}
	return makeMatch(cp, st, src, args[0], pos, endpos), nil
}

// patternSearch implements SRE_Pattern.search(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2594 _sre_SRE_Pattern_search_impl
func patternSearch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: search() called on non-Pattern")
	}
	in, src, pos, endpos, err := matchPosEnd(cp, args)
	if err != nil {
		return nil, err
	}
	st, err := engineSearch(cp, in, pos, endpos, false)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return objects.None(), nil
	}
	return makeMatch(cp, st, src, args[0], pos, endpos), nil
}

// patternScanner implements SRE_Pattern.scanner(string[, pos[, endpos]]).
// CPython exposes Scanner as a stateful iterator whose match() and
// search() methods consume the input one match at a time, advancing
// past the previous end and honouring zero-width-match deduplication.
//
// CPython: Modules/_sre/sre.c:2848 _sre_SRE_Pattern_scanner_impl
func patternScanner(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cp, ok := lookupCompiled(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: scanner() called on non-Pattern")
	}
	in, src, pos, endpos, err := matchPosEnd(cp, args)
	if err != nil {
		return nil, err
	}
	inst := objects.NewInstance(ScannerType)
	d := inst.EnsureDict()
	_ = d.SetItem(objects.NewStr("string"), src)
	_ = d.SetItem(objects.NewStr("pattern"), args[0])
	scannerStore[inst] = &scannerState{
		cp:     cp,
		input:  in,
		src:    src,
		pat:    args[0],
		pos:    pos,
		endpos: endpos,
	}
	return inst, nil
}

// scannerState backs an SRE_Scanner instance. Mirrors the C
// `ScannerObject` fields: pattern, last position, must-advance bit.
//
// CPython: Modules/_sre/sre.c:3107 ScannerObject
type scannerState struct {
	cp          *compiledPattern
	input       []int32
	src         objects.Object
	pat         objects.Object
	pos         int
	endpos      int
	mustAdvance bool
}

// scannerStep advances the scanner one match. mode selects match vs
// search semantics.
//
// CPython: Modules/_sre/sre.c:3116 _sre_SRE_Scanner_match_impl
// CPython: Modules/_sre/sre.c:3132 _sre_SRE_Scanner_search_impl
func scannerStep(args []objects.Object, name string, doSearch bool) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: %s() requires Scanner self", name)
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: %s() requires Scanner", name)
	}
	sc, ok := scannerStore[inst]
	if !ok {
		return objects.None(), nil
	}
	if sc.pos > sc.endpos {
		return objects.None(), nil
	}
	var st *state
	var err error
	if doSearch {
		st, err = engineSearch(sc.cp, sc.input, sc.pos, sc.endpos, sc.mustAdvance)
	} else {
		st, err = engineMatch(sc.cp, sc.input, sc.pos, sc.endpos, false, sc.mustAdvance)
	}
	if err != nil {
		return nil, err
	}
	if st == nil {
		sc.pos = sc.endpos + 1
		return objects.None(), nil
	}
	m := makeMatch(sc.cp, st, sc.src, sc.pat, sc.pos, sc.endpos)
	// Advance past this match. If the match was zero-width, require
	// the next call to step at least one position forward.
	if st.start == st.ptr {
		sc.pos = st.start + 1
		sc.mustAdvance = false
	} else {
		sc.pos = st.ptr
		sc.mustAdvance = (st.start == st.ptr)
	}
	return m, nil
}

func scannerMatch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return scannerStep(args, "match", false)
}

func scannerSearch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return scannerStep(args, "search", true)
}
