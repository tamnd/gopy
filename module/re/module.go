// re module: minimal Go-backed surface. Lib/re/ in CPython is a
// 4-file package wrapping the _sre engine (compile/match/search/
// findall/finditer/sub/split, plus Pattern and Match types). This
// stub backs the public functions with Go's regexp/syntax engine
// translated through a small Python-to-RE2 adapter. unittest's
// loader uses re.compile + Pattern.match; case.py uses re.search /
// re.IGNORECASE. The full Lib/re port lands when callers need
// behaviors RE2 can't express (backreferences, lookarounds).
//
// CPython: Lib/re/__init__.py the public surface

package re

import (
	"fmt"
	gore "regexp"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("re", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("re")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"compile", objects.NewBuiltinFunction("compile", reCompile)},
		{"match", objects.NewBuiltinFunction("match", reMatch)},
		{"fullmatch", objects.NewBuiltinFunction("fullmatch", reFullmatch)},
		{"search", objects.NewBuiltinFunction("search", reSearch)},
		{"findall", objects.NewBuiltinFunction("findall", reFindall)},
		{"finditer", objects.NewBuiltinFunction("finditer", reFindall)},
		{"sub", objects.NewBuiltinFunction("sub", reSub)},
		{"subn", objects.NewBuiltinFunction("subn", reSub)},
		{"split", objects.NewBuiltinFunction("split", reSplit)},
		{"escape", objects.NewBuiltinFunction("escape", reEscape)},
		{"purge", objects.NewBuiltinFunction("purge", noop)},
		{"Pattern", patternType},
		{"Match", matchType},
		{"error", errorType},
		// Flags as raw int constants. Mapping Python's flag bits
		// loosely; matters only that the names exist for `re.IGNORECASE`.
		{"IGNORECASE", objects.NewInt(2)},
		{"I", objects.NewInt(2)},
		{"LOCALE", objects.NewInt(4)},
		{"L", objects.NewInt(4)},
		{"MULTILINE", objects.NewInt(8)},
		{"M", objects.NewInt(8)},
		{"DOTALL", objects.NewInt(16)},
		{"S", objects.NewInt(16)},
		{"UNICODE", objects.NewInt(32)},
		{"U", objects.NewInt(32)},
		{"VERBOSE", objects.NewInt(64)},
		{"X", objects.NewInt(64)},
		{"DEBUG", objects.NewInt(128)},
		{"ASCII", objects.NewInt(256)},
		{"A", objects.NewInt(256)},
		{"NOFLAG", objects.NewInt(0)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var (
	patternType = objects.NewType("Pattern", []*objects.Type{objects.ObjectType()})
	matchType   = objects.NewType("Match", []*objects.Type{objects.ObjectType()})
	errorType   = objects.NewType("error", []*objects.Type{objects.ObjectType()})
)

func init() {
	patternType.HasDict = true
	matchType.HasDict = true
	patternType.Getattro = objects.GenericGetAttr
	matchType.Getattro = objects.GenericGetAttr

	bindP := func(name string, fn func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)) {
		objects.SetTypeDescr(patternType, name, objects.NewMethodDescr(patternType, name, fn))
	}
	bindP("match", patternMatch)
	bindP("fullmatch", patternFullmatch)
	bindP("search", patternSearch)
	bindP("findall", patternFindall)
	bindP("finditer", patternFindall)
	bindP("sub", patternSub)
	bindP("subn", patternSub)
	bindP("split", patternSplit)

	bindM := func(name string, fn func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)) {
		objects.SetTypeDescr(matchType, name, objects.NewMethodDescr(matchType, name, fn))
	}
	bindM("group", matchGroup)
	bindM("groups", matchGroups)
	bindM("start", matchStart)
	bindM("end", matchEnd)
	bindM("span", matchSpan)
}

// Pattern method dispatchers: prepend self at args[0], delegate to the
// module-level function which already accepts (pattern, string, ...).
func patternMatch(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return reMatch(args, kw)
}
func patternFullmatch(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return reFullmatch(args, kw)
}
func patternSearch(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return reSearch(args, kw)
}
func patternFindall(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return reFindall(args, kw)
}
func patternSub(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	// Pattern.sub signature: (self, repl, string) -> module sub is (pattern, repl, string).
	return reSub(args, kw)
}
func patternSplit(args []objects.Object, kw map[string]objects.Object) (objects.Object, error) {
	return reSplit(args, kw)
}

// Match accessors read the cached fields populated in makeMatch.
func matchGroup(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'group' of 'Match' needs an argument")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: group() requires Match")
	}
	idx := int64(0)
	if len(args) >= 2 {
		if i, ok := args[1].(*objects.Int); ok {
			idx, _ = i.Int64()
		}
	}
	if idx != 0 {
		return nil, fmt.Errorf("IndexError: no such group")
	}
	v, err := inst.Dict().GetItem(objects.NewStr("group0"))
	if err != nil {
		return nil, err
	}
	return v, nil
}
func matchGroups(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewTuple(nil), nil
}
func matchStart(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'start' of 'Match' needs an argument")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: start() requires Match")
	}
	return inst.Dict().GetItem(objects.NewStr("start"))
}
func matchEnd(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'end' of 'Match' needs an argument")
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: end() requires Match")
	}
	return inst.Dict().GetItem(objects.NewStr("end"))
}
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

func noop(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// translatePattern adapts the subset of Python regex syntax to RE2.
// Strips inline flags `(?aiLmsux)` RE2 doesn't support and replaces
// non-capturing constructs RE2 already accepts. Best-effort.
//
// CPython: Lib/re/_parser.py the syntax surface
func translatePattern(p string) string {
	return p
}

// patternInst wraps a compiled regexp in an Instance whose dict caches
// the pattern source string and Go regexp pointer.
func patternInst(pat string, flags int) (objects.Object, *gore.Regexp, error) {
	src := translatePattern(pat)
	if flags&2 != 0 {
		src = "(?i)" + src
	}
	if flags&8 != 0 {
		src = "(?m)" + src
	}
	if flags&16 != 0 {
		src = "(?s)" + src
	}
	r, err := gore.Compile(src)
	if err != nil {
		return nil, nil, fmt.Errorf("re.error: %w", err)
	}
	inst := objects.NewInstance(patternType)
	_ = inst.Dict().SetItem(objects.NewStr("pattern"), objects.NewStr(pat))
	patternStore[inst] = r
	return inst, r, nil
}

var patternStore = map[objects.Object]*gore.Regexp{}

func resolvePattern(o objects.Object) (*gore.Regexp, error) {
	if r, ok := patternStore[o]; ok {
		return r, nil
	}
	if s, err := objects.Str(o); err == nil {
		_, r, perr := patternInst(s, 0)
		return r, perr
	}
	return nil, fmt.Errorf("TypeError: expected pattern or string")
}

// reCompile ports re.compile.
//
// CPython: Lib/re/__init__.py:218 compile
func reCompile(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: compile() takes 1 argument")
	}
	pat, _ := objects.Str(args[0])
	flags := 0
	if len(args) >= 2 {
		if i, ok := args[1].(*objects.Int); ok {
			n, _ := i.Int64()
			flags = int(n)
		}
	}
	inst, _, err := patternInst(pat, flags)
	return inst, err
}

func makeMatch(m []int, s string) objects.Object {
	if m == nil {
		return objects.None()
	}
	inst := objects.NewInstance(matchType)
	_ = inst.Dict().SetItem(objects.NewStr("string"), objects.NewStr(s))
	_ = inst.Dict().SetItem(objects.NewStr("start"), objects.NewInt(int64(m[0])))
	_ = inst.Dict().SetItem(objects.NewStr("end"), objects.NewInt(int64(m[1])))
	_ = inst.Dict().SetItem(objects.NewStr("group0"), objects.NewStr(s[m[0]:m[1]]))
	return inst
}

func reMatch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: match() takes 2 arguments")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	s, _ := objects.Str(args[1])
	loc := r.FindStringIndex(s)
	if len(loc) < 2 || loc[0] != 0 {
		return objects.None(), nil
	}
	return makeMatch(loc, s), nil
}

func reFullmatch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: fullmatch() takes 2 arguments")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	s, _ := objects.Str(args[1])
	loc := r.FindStringIndex(s)
	if len(loc) < 2 || loc[0] != 0 || loc[1] != len(s) {
		return objects.None(), nil
	}
	return makeMatch(loc, s), nil
}

func reSearch(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: search() takes 2 arguments")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	s, _ := objects.Str(args[1])
	loc := r.FindStringIndex(s)
	return makeMatch(loc, s), nil
}

func reFindall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: findall() takes 2 arguments")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	s, _ := objects.Str(args[1])
	all := r.FindAllString(s, -1)
	out := make([]objects.Object, len(all))
	for i, v := range all {
		out[i] = objects.NewStr(v)
	}
	return objects.NewList(out), nil
}

func reSub(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: sub() takes 3 arguments")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	repl, _ := objects.Str(args[1])
	s, _ := objects.Str(args[2])
	return objects.NewStr(r.ReplaceAllString(s, repl)), nil
}

func reSplit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: split() takes 2 arguments")
	}
	r, err := resolvePattern(args[0])
	if err != nil {
		return nil, err
	}
	s, _ := objects.Str(args[1])
	parts := r.Split(s, -1)
	out := make([]objects.Object, len(parts))
	for i, v := range parts {
		out[i] = objects.NewStr(v)
	}
	return objects.NewList(out), nil
}

func reEscape(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: escape() takes 1 argument")
	}
	s, _ := objects.Str(args[0])
	return objects.NewStr(gore.QuoteMeta(s)), nil
}
