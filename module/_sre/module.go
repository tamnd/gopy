// Package _sre is the gopy port of CPython's _sre C extension. It
// provides the compiled regex pattern engine consumed by the vendored
// Lib/re/ package. The Python-side compiler (Lib/re/_compiler.py)
// produces SRE bytecode; _sre.compile stores it on a Pattern instance
// and the engine in engine.go drives the match.
//
// CPython: Modules/_sre/sre.c
package _sre

import (
	"fmt"
	"unicode"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_sre", buildModule)
}

// buildModule materializes the _sre module dict. Mirrors PyInit__sre.
//
// CPython: Modules/_sre/sre.c:3338 _functions
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_sre")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		// Integer constants.
		// CPython: Modules/_sre/sre.c:3410 MAGIC
		{"MAGIC", objects.NewInt(int64(MagicNumber))},
		// CPython: Modules/_sre/sre.c:3414 CODESIZE
		{"CODESIZE", objects.NewInt(int64(CodeSize))},
		// CPython: Modules/_sre/sre.h:20 SRE_MAXREPEAT
		{"MAXREPEAT", objects.NewInt(int64(MaxRepeat))},
		// CPython: Modules/_sre/sre.h:21 SRE_MAXGROUPS
		{"MAXGROUPS", objects.NewInt(int64(MaxGroups))},

		// Function exports.
		{"compile", objects.NewBuiltinFunction("compile", sreCompile)},
		{"template", objects.NewBuiltinFunction("template", sreTemplate)},
		{"getcodesize", objects.NewBuiltinFunction("getcodesize", sreGetcodesize)},
		{"unicode_iscased", objects.NewBuiltinFunction("unicode_iscased", sreUnicodeIscased)},
		{"ascii_iscased", objects.NewBuiltinFunction("ascii_iscased", sreAsciiIscased)},
		{"unicode_tolower", objects.NewBuiltinFunction("unicode_tolower", sreUnicodeTolower)},
		{"ascii_tolower", objects.NewBuiltinFunction("ascii_tolower", sreAsciiTolower)},

		// Type exports.
		{"SRE_Pattern", PatternType},
		{"SRE_Match", MatchType},
		{"SRE_Scanner", ScannerType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Type singletons.

var (
	PatternType = objects.NewType("SRE_Pattern", []*objects.Type{objects.ObjectType()})
	MatchType   = objects.NewType("SRE_Match", []*objects.Type{objects.ObjectType()})
	ScannerType = objects.NewType("SRE_Scanner", []*objects.Type{objects.ObjectType()})
)

func init() {
	PatternType.HasDict = true
	MatchType.HasDict = true
	ScannerType.HasDict = true
	PatternType.Getattro = objects.GenericGetAttr
	MatchType.Getattro = objects.GenericGetAttr
	ScannerType.Getattro = objects.GenericGetAttr

	bindP := func(name string, fn func([]objects.Object, map[string]objects.Object) (objects.Object, error)) {
		objects.SetTypeDescr(PatternType, name, objects.NewMethodDescr(PatternType, name, fn))
	}
	bindP("match", patternMatch)
	bindP("fullmatch", patternFullmatch)
	bindP("search", patternSearch)
	bindP("findall", patternFindall)
	bindP("finditer", patternFinditer)
	bindP("sub", patternSub)
	bindP("subn", patternSubn)
	bindP("split", patternSplit)
	bindP("scanner", patternScanner)

	bindM := func(name string, fn func([]objects.Object, map[string]objects.Object) (objects.Object, error)) {
		objects.SetTypeDescr(MatchType, name, objects.NewMethodDescr(MatchType, name, fn))
	}
	bindM("group", matchGroup)
	bindM("groups", matchGroups)
	bindM("groupdict", matchGroupdict)
	bindM("start", matchStart)
	bindM("end", matchEnd)
	bindM("span", matchSpan)
	bindM("expand", matchExpand)

	bindS := func(name string, fn func([]objects.Object, map[string]objects.Object) (objects.Object, error)) {
		objects.SetTypeDescr(ScannerType, name, objects.NewMethodDescr(ScannerType, name, fn))
	}
	bindS("match", scannerMatch)
	bindS("search", scannerSearch)
}

// ---------------------------------------------------------------------------
// Side stores: maps *Instance -> Go-native values.
// Keyed by the *objects.Instance pointer (as objects.Object interface
// value) so lookups are O(1) without polluting the Python-visible dict.

var matchStore = map[objects.Object]*matchData{}
var scannerStore = map[objects.Object]*scannerState{}

// matchData is the engine-side payload stored next to a SRE_Match
// instance. locs has the shape
// [group0_start, group0_end, group1_start, group1_end, ...] with -1 in
// any slot whose group was not matched, matching what the previous RE2
// path stored. s is the original input string.
type matchData struct {
	locs []int
	s    string
}

// ---------------------------------------------------------------------------
// _sre module-level functions other than compile/template.

// sreGetcodesize returns CODESIZE.
//
// CPython: Modules/_sre/sre.c:3341 _SRE_GETCODESIZE_METHODDEF
func sreGetcodesize(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(CodeSize)), nil
}

// sreUnicodeIscased reports whether the code point has distinct upper/lower.
//
// CPython: Modules/_sre/sre.c:425 _sre.ascii_iscased
func sreUnicodeIscased(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ch, err := intArg(args, 0, "unicode_iscased")
	if err != nil {
		return nil, err
	}
	r := rune(ch)
	if unicode.IsUpper(r) || unicode.IsLower(r) {
		return objects.True(), nil
	}
	return objects.False(), nil
}

// sreAsciiIscased reports whether the ASCII character has distinct case.
//
// CPython: Modules/_sre/sre.c:433 _sre_ascii_iscased_impl
func sreAsciiIscased(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ch, err := intArg(args, 0, "ascii_iscased")
	if err != nil {
		return nil, err
	}
	if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
		return objects.True(), nil
	}
	return objects.False(), nil
}

// sreUnicodeTolower returns the Unicode lowercased code point.
//
// CPython: Modules/_sre/sre.c:441 _sre.unicode_iscased
func sreUnicodeTolower(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ch, err := intArg(args, 0, "unicode_tolower")
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(unicode.ToLower(rune(ch)))), nil
}

// sreAsciiTolower returns the ASCII-lowercased code point.
//
// CPython: Modules/_sre/sre.c:449 _sre_unicode_iscased_impl
func sreAsciiTolower(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ch, err := intArg(args, 0, "ascii_tolower")
	if err != nil {
		return nil, err
	}
	if ch >= 'A' && ch <= 'Z' {
		ch = ch + 32
	}
	return objects.NewInt(ch), nil
}

// sreTemplate implements _sre.template(pattern, template_list).
// template_list is [literal0, group1, literal1, group2, literal2, ...] as
// produced by _parser.parse_template. We store it for use by Pattern.sub.
//
// CPython: Modules/_sre/sre.c:1719 _sre_template_impl
func sreTemplate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: template() takes 2 arguments")
	}
	inst := objects.NewInstance(templateType)
	_ = inst.Dict().SetItem(objects.NewStr("_pattern"), args[0])
	_ = inst.Dict().SetItem(objects.NewStr("_template"), args[1])
	return inst, nil
}

var templateType = objects.NewType("SRE_Template", []*objects.Type{objects.ObjectType()})

func init() {
	templateType.HasDict = true
	templateType.Getattro = objects.GenericGetAttr
}

// ---------------------------------------------------------------------------
// Higher-level Pattern methods (findall / finditer / sub / subn / split)
// are the Phase 6 work in spec 1703. Phase 4 leaves these as typed
// errors so callers see a clear "not yet implemented" instead of the
// previous RE2-backed answers, which would silently miscompile
// CPython-specific syntax now that the bytecode path is the only
// truth.
//
// CPython references for these methods are pinned in spec 1703 §
// Phase 6 and the implementations land in pattern_methods.go in
// a follow-up commit.

func notYetImplemented(name string) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: SRE_Pattern.%s is part of spec 1703 phase 6", name)
}

func patternFindall(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return notYetImplemented("findall")
}

func patternFinditer(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return notYetImplemented("finditer")
}

func patternSub(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return notYetImplemented("sub")
}

func patternSubn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return notYetImplemented("subn")
}

func patternSplit(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return notYetImplemented("split")
}

// ---------------------------------------------------------------------------
// Match methods. These read out of matchData.locs, which the engine
// fills via makeMatch in pattern.go. Phase 5 expands the surface
// (regs, lastgroup, named-group access through group()); the basic
// numeric-group plumbing here is the working subset Phase 4 needs to
// land green.

func getMatchLocs(inst *objects.Instance) ([]int, string, error) {
	md, ok := matchStore[inst]
	if !ok {
		return nil, "", fmt.Errorf("Match has no locs data")
	}
	return md.locs, md.s, nil
}

// groupNumber returns the 0-based submatch index from a group arg.
func groupNumber(arg objects.Object) (int, error) {
	if objects.IsNone(arg) {
		return -1, fmt.Errorf("no such group")
	}
	i, ok := arg.(*objects.Int)
	if !ok {
		return -1, fmt.Errorf("no such group")
	}
	v, _ := i.Int64()
	return int(v), nil
}

// matchGroup implements SRE_Match.group([group1, ...]).
//
// CPython: Modules/_sre/sre.c:2980 _sre_SRE_Match_group_impl
func matchGroup(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: group() requires Match self")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: group() requires Match")
	}
	locs, s, err := getMatchLocs(inst)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return objects.NewStr(s[locs[0]:locs[1]]), nil
	}
	if len(args) == 2 {
		g, gerr := groupNumber(args[1])
		if gerr != nil {
			return nil, gerr
		}
		if g < 0 || 2*g+1 >= len(locs) {
			return nil, fmt.Errorf("IndexError: no such group")
		}
		lo, hi := locs[2*g], locs[2*g+1]
		if lo < 0 {
			return objects.None(), nil
		}
		return objects.NewStr(s[lo:hi]), nil
	}
	out := make([]objects.Object, len(args)-1)
	for i, a := range args[1:] {
		g, gerr := groupNumber(a)
		if gerr != nil {
			return nil, gerr
		}
		if g < 0 || 2*g+1 >= len(locs) {
			return nil, fmt.Errorf("IndexError: no such group")
		}
		lo, hi := locs[2*g], locs[2*g+1]
		if lo < 0 {
			out[i] = objects.None()
		} else {
			out[i] = objects.NewStr(s[lo:hi])
		}
	}
	return objects.NewTuple(out), nil
}

// matchGroups implements SRE_Match.groups([default]).
//
// CPython: Modules/_sre/sre.c:2996 _sre_SRE_Match_groups_impl
func matchGroups(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: groups() requires Match self")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: groups() requires Match")
	}
	locs, s, err := getMatchLocs(inst)
	if err != nil {
		return nil, err
	}
	def := objects.None()
	if len(args) >= 2 {
		def = args[1]
	}
	ngroups := len(locs)/2 - 1
	if ngroups < 0 {
		ngroups = 0
	}
	out := make([]objects.Object, ngroups)
	for g := 0; g < ngroups; g++ {
		lo, hi := locs[2+2*g], locs[2+2*g+1]
		if lo < 0 {
			out[g] = def
		} else {
			out[g] = objects.NewStr(s[lo:hi])
		}
	}
	return objects.NewTuple(out), nil
}

// matchGroupdict implements SRE_Match.groupdict([default]).
//
// CPython: Modules/_sre/sre.c:3021 _sre_SRE_Match_groupdict_impl
func matchGroupdict(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: groupdict() requires Match self")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: groupdict() requires Match")
	}
	locs, s, err := getMatchLocs(inst)
	if err != nil {
		return nil, err
	}
	def := objects.None()
	if len(args) >= 2 {
		def = args[1]
	}

	d := objects.NewDict()
	reObj, rerr := inst.Dict().GetItem(objects.NewStr("re"))
	if rerr == nil {
		if patInst, ok := reObj.(*objects.Instance); ok {
			giObj, gierr := patInst.Dict().GetItem(objects.NewStr("groupindex"))
			if gierr == nil {
				if gi, ok := giObj.(*objects.Dict); ok {
					for _, k := range gi.Keys() {
						v, _ := gi.GetItem(k)
						gi2, ok := v.(*objects.Int)
						if !ok {
							continue
						}
						g, _ := gi2.Int64()
						idx := int(g) * 2
						if idx+1 >= len(locs) {
							_ = d.SetItem(k, def)
							continue
						}
						lo, hi := locs[idx], locs[idx+1]
						if lo < 0 {
							_ = d.SetItem(k, def)
						} else {
							_ = d.SetItem(k, objects.NewStr(s[lo:hi]))
						}
					}
				}
			}
		}
	}
	return d, nil
}

// matchStart implements SRE_Match.start([group]).
//
// CPython: Modules/_sre/sre.c:3042 _sre_SRE_Match_start_impl
func matchStart(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	_, locs, _, err := matchInstLocs(args, "start")
	if err != nil {
		return nil, err
	}
	g := 0
	if len(args) >= 2 {
		if i, ok := args[1].(*objects.Int); ok {
			v, _ := i.Int64()
			g = int(v)
		}
	}
	if 2*g+1 >= len(locs) {
		return nil, fmt.Errorf("IndexError: no such group")
	}
	return objects.NewInt(int64(locs[2*g])), nil
}

// matchEnd implements SRE_Match.end([group]).
//
// CPython: Modules/_sre/sre.c:3059 _sre_SRE_Match_end_impl
func matchEnd(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	_, locs, _, err := matchInstLocs(args, "end")
	if err != nil {
		return nil, err
	}
	g := 0
	if len(args) >= 2 {
		if i, ok := args[1].(*objects.Int); ok {
			v, _ := i.Int64()
			g = int(v)
		}
	}
	if 2*g+1 >= len(locs) {
		return nil, fmt.Errorf("IndexError: no such group")
	}
	return objects.NewInt(int64(locs[2*g+1])), nil
}

// matchSpan implements SRE_Match.span([group]).
//
// CPython: Modules/_sre/sre.c:3077 _sre_SRE_Match_span_impl
func matchSpan(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := matchStart(args, nil)
	if err != nil {
		return nil, err
	}
	e, err := matchEnd(args, nil)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{s, e}), nil
}

// matchExpand implements SRE_Match.expand(template). Phase 5 ports the
// real `\g<name>` / `\N` template parser; until then we keep the slot
// bound but error out so callers cannot rely on the old RE2-flavoured
// template substitution.
//
// CPython: Modules/_sre/sre.c:2961 _sre_SRE_Match_expand_impl
func matchExpand(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: SRE_Match.expand is part of spec 1703 phase 5")
}

// matchInstLocs is a helper that extracts the Instance, locs, and string.
func matchInstLocs(args []objects.Object, name string) (*objects.Instance, []int, string, error) {
	if len(args) < 1 {
		return nil, nil, "", fmt.Errorf("TypeError: %s() requires Match self", name)
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, nil, "", fmt.Errorf("TypeError: %s() requires Match", name)
	}
	locs, s, err := getMatchLocs(inst)
	return inst, locs, s, err
}

// ---------------------------------------------------------------------------
// Utilities.

// intArg extracts an int64 from args[idx].
func intArg(args []objects.Object, idx int, name string) (int64, error) {
	if idx >= len(args) {
		return 0, fmt.Errorf("TypeError: %s() requires at least %d arguments", name, idx+1)
	}
	i, ok := args[idx].(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s() argument %d must be int", name, idx)
	}
	v, _ := i.Int64()
	return v, nil
}
