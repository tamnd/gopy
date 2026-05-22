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
// path stored. s is the original input string. isBytes is true when
// the original input was a bytes-like object so group() rebuilds the
// substring as Bytes rather than Unicode, mirroring CPython's
// PATTERN_TYPE_BYTES handling in Modules/_sre/sre.c:2980
// _sre_SRE_Match_group_impl.
type matchData struct {
	locs    []int
	s       string
	isBytes bool
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
	d := inst.EnsureDict()
	_ = d.SetItem(objects.NewStr("_pattern"), args[0])
	_ = d.SetItem(objects.NewStr("_template"), args[1])
	return inst, nil
}

var templateType = objects.NewType("SRE_Template", []*objects.Type{objects.ObjectType()})

func init() {
	templateType.HasDict = true
	templateType.Getattro = objects.GenericGetAttr
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
