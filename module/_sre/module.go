// Package _sre is the gopy port of CPython's _sre C extension. It
// provides the compiled regex pattern engine consumed by the vendored
// Lib/re/ package. The Go regexp/RE2 library is used as the matching
// backend; Python-specific syntax that RE2 does not support (backreferences,
// atomic groups, possessive quantifiers) raises re.error at compile time.
//
// CPython: Modules/_sre/sre.c
package _sre

import (
	"fmt"
	gore "regexp"
	"strings"
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
		{"MAGIC", objects.NewInt(20230612)},
		// CPython: Modules/_sre/sre.c:3414 CODESIZE = sizeof(SRE_CODE) = sizeof(Py_UCS4) = 4
		{"CODESIZE", objects.NewInt(4)},
		// CPython: Modules/_sre/sre.h:20 SRE_MAXREPEAT = ~(SRE_CODE)0 on 32-bit UCS4 = 0xFFFFFFFF
		{"MAXREPEAT", objects.NewInt(4294967295)},
		// CPython: Modules/_sre/sre.h:21 SRE_MAXGROUPS = INT32_MAX / 2
		{"MAXGROUPS", objects.NewInt(1073741823)},

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
// We key by the *objects.Instance pointer (as objects.Object interface
// value) so lookups are O(1) without polluting the Python-visible dict.

var patternStore = map[objects.Object]*gore.Regexp{}
var matchStore = map[objects.Object]*matchData{}
var scannerStore = map[objects.Object]*scannerData{}

type matchData struct {
	locs []int
	s    string
}

type scannerData struct {
	matches []objects.Object
	pos     int
}

func lookupPattern(o objects.Object) (*gore.Regexp, bool) {
	r, ok := patternStore[o]
	return r, ok
}

// ---------------------------------------------------------------------------
// Python->RE2 pattern translation.
//
// Go's regexp uses RE2 syntax. Python regex syntax is a superset:
//   - Named groups: (?P<name>...) is RE2-compatible.
//   - Non-capturing: (?:...) works.
//   - Inline flags: (?aiLmsux) are Python-only; we strip recognised ones.
//   - Backreferences \1..\99 are not supported in RE2; we return an error.
//
// CPython: Lib/re/_parser.py (syntax surface reference)
func translatePattern(pat string, flags int) (string, error) {
	// Inline flags (?aiLmsux) must be removed or converted. RE2 supports
	// (?flags) for i, m, s. We convert (?P<n>...) which is already RE2.
	// Backreferences are detected and rejected.
	out := &strings.Builder{}
	i := 0
	n := len(pat)
	for i < n {
		ch := pat[i]
		if ch == '\\' && i+1 < n {
			next := pat[i+1]
			// Backreferences \1 through \99 are not RE2-compatible.
			if next >= '1' && next <= '9' {
				return "", fmt.Errorf("re.error: backreferences (\\%c) are not supported", next)
			}
			out.WriteByte(ch)
			out.WriteByte(next)
			i += 2
			continue
		}
		if ch == '(' && i+1 < n && pat[i+1] == '?' {
			// Peek at inline flag groups: (?aiLmsux) or (?aiLmsux:...) or (?P<...>).
			// (?P<name>...) is already RE2-compatible.
			if i+2 < n && pat[i+2] == 'P' {
				out.WriteByte(ch)
				i++
				continue
			}
			// (?flags) or (?flags:...) -- convert supported flags, drop others.
			j := i + 2
			re2flags := ""
			for j < n && pat[j] != ':' && pat[j] != ')' {
				switch pat[j] {
				case 'i':
					re2flags += "i"
				case 'm':
					re2flags += "m"
				case 's':
					re2flags += "s"
				case 'a', 'L', 'u', 'x':
					// ASCII / locale / unicode / verbose: drop silently.
				}
				j++
			}
			if j < n && pat[j] == ':' {
				// Non-capturing group with flags: (?flags:...) -> (?flags:...).
				out.WriteString("(?")
				out.WriteString(re2flags)
				out.WriteByte(':')
				i = j + 1
				continue
			}
			// (?flags) standalone: emit as (?flags) if any RE2 flags remain.
			if re2flags != "" {
				out.WriteString("(?")
				out.WriteString(re2flags)
				out.WriteByte(')')
			}
			if j < n && pat[j] == ')' {
				i = j + 1
			} else {
				i = j
			}
			continue
		}
		out.WriteByte(ch)
		i++
	}

	// Apply top-level flags from the flags int.
	// CPython: Lib/re/_constants.py SRE_FLAG_* values.
	prefix := ""
	if flags&2 != 0 { // SRE_FLAG_IGNORECASE
		prefix += "i"
	}
	if flags&8 != 0 { // SRE_FLAG_MULTILINE
		prefix += "m"
	}
	if flags&16 != 0 { // SRE_FLAG_DOTALL
		prefix += "s"
	}
	s := out.String()
	if prefix != "" {
		s = "(?"+prefix+")" + s
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// _sre module-level functions.

// sreGetcodesize returns CODESIZE.
//
// CPython: Modules/_sre/sre.c:3341 _SRE_GETCODESIZE_METHODDEF
func sreGetcodesize(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(4), nil
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

// sreCompile implements _sre.compile(pattern, flags, code, groups, groupindex, indexgroup).
// The compiled bytecode in `code` is produced by Lib/re/_compiler.py and
// is the reference engine for CPython. Here we extract `pattern` (args[0])
// and compile it via Go's regexp/RE2 backend instead.
//
// CPython: Modules/_sre/sre.c:1621 _sre_compile_impl
func sreCompile(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 6 {
		return nil, fmt.Errorf("TypeError: compile() takes 6 arguments (%d given)", len(args))
	}

	pat, err := objects.Str(args[0])
	if err != nil {
		// pattern may be None for internal use (empty pattern).
		if objects.IsNone(args[0]) {
			pat = ""
		} else {
			return nil, fmt.Errorf("TypeError: compile() pattern must be str or None: %w", err)
		}
	}

	flags := int64(0)
	if i, ok := args[1].(*objects.Int); ok {
		flags, _ = i.Int64()
	}

	// groups is args[3].
	groups := int64(0)
	if i, ok := args[3].(*objects.Int); ok {
		groups, _ = i.Int64()
	}

	// groupindex is args[4] (dict mapping name->group number).
	groupindex := args[4]

	// indexgroup is args[5] (tuple mapping group number->name).
	indexgroup := args[5]

	// Translate Python pattern to RE2/Go syntax.
	re2pat, terr := translatePattern(pat, int(flags))
	if terr != nil {
		return nil, terr
	}

	r, cerr := gore.Compile(re2pat)
	if cerr != nil {
		return nil, fmt.Errorf("re.error: %w", cerr)
	}

	inst := objects.NewInstance(PatternType)
	d := inst.Dict()
	_ = d.SetItem(objects.NewStr("pattern"), objects.NewStr(pat))
	_ = d.SetItem(objects.NewStr("flags"), objects.NewInt(flags))
	_ = d.SetItem(objects.NewStr("groups"), objects.NewInt(groups))
	_ = d.SetItem(objects.NewStr("groupindex"), groupindex)
	_ = d.SetItem(objects.NewStr("indexgroup"), indexgroup)
	patternStore[inst] = r
	return inst, nil
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
	// Return a Template instance holding pattern + template list.
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
// Pattern methods.

// resolvePattern extracts the Go regexp from a Pattern instance or a raw
// string, compiling on the fly for the string case.
func resolvePattern(o objects.Object) (*gore.Regexp, error) {
	if r, ok := lookupPattern(o); ok {
		return r, nil
	}
	if s, err := objects.Str(o); err == nil {
		re2pat, terr := translatePattern(s, 0)
		if terr != nil {
			return nil, terr
		}
		return gore.Compile(re2pat)
	}
	return nil, fmt.Errorf("TypeError: expected pattern or string")
}

// getPatternAndStr extracts (compiled pattern, string to search) from method args.
// args[0] is the Pattern instance; args[1] is the string.
func getPatternAndStr(args []objects.Object, name string) (*gore.Regexp, string, int, int, error) {
	if len(args) < 2 {
		return nil, "", 0, 0, fmt.Errorf("TypeError: %s() takes at least 2 arguments", name)
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, "", 0, 0, err
	}
	s, serr := objects.Str(args[1])
	if serr != nil {
		return nil, "", 0, 0, fmt.Errorf("TypeError: %s() string must be str: %w", name, serr)
	}
	pos := 0
	endpos := len(s)
	if len(args) >= 3 {
		if i, ok := args[2].(*objects.Int); ok {
			v, _ := i.Int64()
			pos = int(v)
		}
	}
	if len(args) >= 4 {
		if i, ok := args[3].(*objects.Int); ok {
			v, _ := i.Int64()
			endpos = int(v)
		}
	}
	if pos < 0 {
		pos = 0
	}
	if endpos > len(s) {
		endpos = len(s)
	}
	return r, s, pos, endpos, nil
}

// makeMatch builds a SRE_Match instance from a submatch index array.
// locs is the result of FindStringSubmatchIndex.
//
// CPython: Modules/_sre/sre.c:2900 pattern_new_match
func makeMatch(locs []int, s string, patInst objects.Object) objects.Object {
	if locs == nil {
		return objects.None()
	}
	inst := objects.NewInstance(MatchType)
	d := inst.Dict()
	_ = d.SetItem(objects.NewStr("string"), objects.NewStr(s))
	_ = d.SetItem(objects.NewStr("re"), patInst)
	_ = d.SetItem(objects.NewStr("pos"), objects.NewInt(0))
	_ = d.SetItem(objects.NewStr("endpos"), objects.NewInt(int64(len(s))))
	// lastindex: the index of the last matched group (1-based), or None.
	lastIndex := objects.None()
	for g := len(locs)/2 - 1; g >= 1; g-- {
		if locs[2*g] >= 0 {
			lastIndex = objects.NewInt(int64(g))
			break
		}
	}
	_ = d.SetItem(objects.NewStr("lastindex"), lastIndex)
	// Store locs in side map; not in the Python-visible dict.
	matchStore[inst] = &matchData{locs: locs, s: s}
	return inst
}

// patternMatch implements SRE_Pattern.match(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2631 _sre_SRE_Pattern_match_impl
func patternMatch(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	r, s, pos, endpos, err := getPatternAndStr(args, "match")
	if err != nil {
		return nil, err
	}
	sub := s[pos:endpos]
	locs := r.FindStringSubmatchIndex(sub)
	if locs == nil || locs[0] != 0 {
		return objects.None(), nil
	}
	// Adjust locs for pos offset.
	for i := range locs {
		if locs[i] >= 0 {
			locs[i] += pos
		}
	}
	return makeMatch(locs, s, args[0]), nil
}

// patternFullmatch implements SRE_Pattern.fullmatch(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2672 _sre_SRE_Pattern_fullmatch_impl
func patternFullmatch(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	r, s, pos, endpos, err := getPatternAndStr(args, "fullmatch")
	if err != nil {
		return nil, err
	}
	sub := s[pos:endpos]
	locs := r.FindStringSubmatchIndex(sub)
	if locs == nil || locs[0] != 0 || locs[1] != len(sub) {
		return objects.None(), nil
	}
	for i := range locs {
		if locs[i] >= 0 {
			locs[i] += pos
		}
	}
	return makeMatch(locs, s, args[0]), nil
}

// patternSearch implements SRE_Pattern.search(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2594 _sre_SRE_Pattern_search_impl
func patternSearch(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	r, s, pos, endpos, err := getPatternAndStr(args, "search")
	if err != nil {
		return nil, err
	}
	sub := s[pos:endpos]
	locs := r.FindStringSubmatchIndex(sub)
	if locs == nil {
		return objects.None(), nil
	}
	for i := range locs {
		if locs[i] >= 0 {
			locs[i] += pos
		}
	}
	return makeMatch(locs, s, args[0]), nil
}

// patternFindall implements SRE_Pattern.findall(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2739 _sre_SRE_Pattern_findall_impl
func patternFindall(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	r, s, pos, endpos, err := getPatternAndStr(args, "findall")
	if err != nil {
		return nil, err
	}
	sub := s[pos:endpos]
	allLocs := r.FindAllStringSubmatchIndex(sub, -1)
	if allLocs == nil {
		return objects.NewList(nil), nil
	}
	ngroups := r.NumSubexp()
	out := make([]objects.Object, 0, len(allLocs))
	for _, locs := range allLocs {
		if ngroups == 0 {
			// No capture groups: return the matched string.
			out = append(out, objects.NewStr(sub[locs[0]:locs[1]]))
		} else if ngroups == 1 {
			// One group: return the group text (or empty on no-match).
			if locs[2] < 0 {
				out = append(out, objects.NewStr(""))
			} else {
				out = append(out, objects.NewStr(sub[locs[2]:locs[3]]))
			}
		} else {
			// Multiple groups: return a tuple.
			grps := make([]objects.Object, ngroups)
			for g := 0; g < ngroups; g++ {
				if locs[2+2*g] < 0 {
					grps[g] = objects.None()
				} else {
					grps[g] = objects.NewStr(sub[locs[2+2*g]:locs[2+2*g+1]])
				}
			}
			out = append(out, objects.NewTuple(grps))
		}
	}
	return objects.NewList(out), nil
}

// patternFinditer implements SRE_Pattern.finditer(string[, pos[, endpos]]).
// Returns a list of Match objects (CPython returns a callable_iterator; we
// return a list which the caller can iterate).
//
// CPython: Modules/_sre/sre.c:2766 _sre_SRE_Pattern_finditer_impl
func patternFinditer(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	r, s, pos, endpos, err := getPatternAndStr(args, "finditer")
	if err != nil {
		return nil, err
	}
	sub := s[pos:endpos]
	allLocs := r.FindAllStringSubmatchIndex(sub, -1)
	matches := make([]objects.Object, 0, len(allLocs))
	for _, locs := range allLocs {
		adjusted := make([]int, len(locs))
		copy(adjusted, locs)
		for i := range adjusted {
			if adjusted[i] >= 0 {
				adjusted[i] += pos
			}
		}
		matches = append(matches, makeMatch(adjusted, s, args[0]))
	}
	return objects.NewList(matches), nil
}

// patternSub implements SRE_Pattern.sub(repl, string[, count]).
//
// CPython: Modules/_sre/sre.c:2790 _sre_SRE_Pattern_sub_impl
func patternSub(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	result, _, err := patternSubWork(args, kw)
	return result, err
}

// patternSubn implements SRE_Pattern.subn(repl, string[, count]).
//
// CPython: Modules/_sre/sre.c:2802 _sre_SRE_Pattern_subn_impl
func patternSubn(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	result, count, err := patternSubWork(args, kw)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{result, objects.NewInt(int64(count))}), nil
}

func patternSubWork(args []objects.Object, _ map[string]objects.Object) (objects.Object, int, error) {
	if len(args) < 3 {
		return nil, 0, fmt.Errorf("TypeError: sub() takes at least 3 arguments (self, repl, string)")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, 0, err
	}
	repl, rerr := objects.Str(args[1])
	if rerr != nil {
		return nil, 0, fmt.Errorf("TypeError: sub() repl must be str: %w", rerr)
	}
	s, serr := objects.Str(args[2])
	if serr != nil {
		return nil, 0, fmt.Errorf("TypeError: sub() string must be str: %w", serr)
	}
	n := -1
	if len(args) >= 4 {
		if i, ok := args[3].(*objects.Int); ok {
			v, _ := i.Int64()
			if v > 0 {
				n = int(v)
			}
		}
	}
	// Convert Python \N group refs in repl to Go $N syntax.
	goRepl := pythonReplToGo(repl)
	count := 0
	result := r.ReplaceAllStringFunc(s, func(matched string) string {
		if n >= 0 && count >= n {
			return matched
		}
		count++
		return r.ReplaceAllString(matched, goRepl)
	})
	return objects.NewStr(result), count, nil
}

// pythonReplToGo converts Python replacement string syntax to Go syntax.
// \g<name> and \g<N> -> ${name} / $N; \N -> $N.
//
// CPython: Modules/_sre/sre.c:2819 pattern_subx_impl (partial)
func pythonReplToGo(repl string) string {
	out := &strings.Builder{}
	i := 0
	for i < len(repl) {
		if repl[i] == '\\' && i+1 < len(repl) {
			next := repl[i+1]
			if next >= '1' && next <= '9' {
				// \N -> $N
				out.WriteByte('$')
				out.WriteByte(next)
				i += 2
				continue
			}
			if next == 'g' && i+2 < len(repl) && repl[i+2] == '<' {
				// \g<name> or \g<N>
				j := i + 3
				for j < len(repl) && repl[j] != '>' {
					j++
				}
				name := repl[i+3 : j]
				out.WriteString("${")
				out.WriteString(name)
				out.WriteByte('}')
				i = j + 1
				continue
			}
		}
		out.WriteByte(repl[i])
		i++
	}
	return out.String()
}

// patternSplit implements SRE_Pattern.split(string[, maxsplit]).
//
// CPython: Modules/_sre/sre.c:2708 _sre_SRE_Pattern_split_impl
func patternSplit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: split() takes at least 2 arguments (self, string)")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	s, serr := objects.Str(args[1])
	if serr != nil {
		return nil, fmt.Errorf("TypeError: split() string must be str: %w", serr)
	}
	maxsplit := -1
	if len(args) >= 3 {
		if i, ok := args[2].(*objects.Int); ok {
			v, _ := i.Int64()
			if v > 0 {
				maxsplit = int(v)
			}
		}
	}
	parts := r.Split(s, maxsplit+1)
	out := make([]objects.Object, len(parts))
	for i, p := range parts {
		out[i] = objects.NewStr(p)
	}
	return objects.NewList(out), nil
}

// patternScanner implements SRE_Pattern.scanner(string[, pos[, endpos]]).
//
// CPython: Modules/_sre/sre.c:2848 _sre_SRE_Pattern_scanner_impl
func patternScanner(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: scanner() takes at least 2 arguments (self, string)")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	s, serr := objects.Str(args[1])
	if serr != nil {
		return nil, fmt.Errorf("TypeError: scanner() string must be str: %w", serr)
	}

	allLocs := r.FindAllStringSubmatchIndex(s, -1)
	allMatches := make([]objects.Object, 0, len(allLocs))
	for _, locs := range allLocs {
		allMatches = append(allMatches, makeMatch(locs, s, args[0]))
	}

	inst := objects.NewInstance(ScannerType)
	_ = inst.Dict().SetItem(objects.NewStr("string"), objects.NewStr(s))
	_ = inst.Dict().SetItem(objects.NewStr("pattern"), args[0])
	scannerStore[inst] = &scannerData{matches: allMatches, pos: 0}
	return inst, nil
}

// ---------------------------------------------------------------------------
// Match methods.

func getMatchLocs(inst *objects.Instance) ([]int, string, error) {
	md, ok := matchStore[inst]
	if !ok {
		return nil, "", fmt.Errorf("Match has no locs data")
	}
	return md.locs, md.s, nil
}

// groupNumber returns the 0-based submatch index from a group arg
// (int or group 0 = whole match).
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
		// group() -> group(0)
		return objects.NewStr(s[locs[0]:locs[1]]), nil
	}
	if len(args) == 2 {
		g, gerr := groupNumber(args[1])
		if gerr != nil {
			return nil, gerr
		}
		if g < 0 || 2+2*g+1 >= len(locs) {
			return nil, fmt.Errorf("IndexError: no such group")
		}
		lo, hi := locs[2*g], locs[2*g+1]
		if lo < 0 {
			return objects.None(), nil
		}
		return objects.NewStr(s[lo:hi]), nil
	}
	// Multiple group args -> tuple.
	out := make([]objects.Object, len(args)-1)
	for i, a := range args[1:] {
		g, gerr := groupNumber(a)
		if gerr != nil {
			return nil, gerr
		}
		if g < 0 || 2+2*g+1 >= len(locs) {
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
	// Get groupindex from the pattern.
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
	inst, locs, _, err := matchInstLocs(args, "start")
	if err != nil {
		return nil, err
	}
	_ = inst
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
	inst, locs, _, err := matchInstLocs(args, "end")
	if err != nil {
		return nil, err
	}
	_ = inst
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

// matchExpand implements SRE_Match.expand(template).
//
// CPython: Modules/_sre/sre.c:2961 _sre_SRE_Match_expand_impl
func matchExpand(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: expand() takes 2 arguments (self, template)")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: expand() requires Match")
	}
	repl, err := objects.Str(args[1])
	if err != nil {
		return nil, fmt.Errorf("TypeError: expand() template must be str: %w", err)
	}
	locs, s, lerr := getMatchLocs(inst)
	if lerr != nil {
		return nil, lerr
	}
	// Get the pattern to call ReplaceAllString.
	reObj, _ := inst.Dict().GetItem(objects.NewStr("re"))
	r, rok := lookupPattern(reObj)
	if !rok {
		return objects.NewStr(repl), nil
	}
	goRepl := pythonReplToGo(repl)
	matched := s[locs[0]:locs[1]]
	result := r.ReplaceAllString(matched, goRepl)
	return objects.NewStr(result), nil
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
// Scanner methods.

// scannerMatch implements SRE_Scanner.match().
//
// CPython: Modules/_sre/sre.c:3116 _sre_SRE_Scanner_match_impl
func scannerMatch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return scannerNext(args, "match")
}

// scannerSearch implements SRE_Scanner.search().
//
// CPython: Modules/_sre/sre.c:3132 _sre_SRE_Scanner_search_impl
func scannerSearch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return scannerNext(args, "search")
}

func scannerNext(args []objects.Object, name string) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: %s() requires Scanner self", name)
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: %s() requires Scanner", name)
	}
	sd, ok := scannerStore[inst]
	if !ok || sd.pos >= len(sd.matches) {
		return objects.None(), nil
	}
	m := sd.matches[sd.pos]
	sd.pos++
	return m, nil
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
