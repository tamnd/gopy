// fnmatch module: minimal Go-backed surface. Lib/fnmatch.py builds a
// regex from the shell glob and dispatches through re; this stub
// covers the four public names with a hand-rolled glob matcher
// (`*` `?` `[abc]`) so unittest's loader can match test patterns at
// module load time. The full Lib/fnmatch.py port lands when the re
// module ports.
//
// CPython: Lib/fnmatch.py:11 fnmatch / fnmatchcase / filter / translate

package fnmatch

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("fnmatch", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("fnmatch")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"fnmatch", objects.NewBuiltinFunction("fnmatch", fnmatchFn)},
		{"fnmatchcase", objects.NewBuiltinFunction("fnmatchcase", fnmatchCase)},
		{"filter", objects.NewBuiltinFunction("filter", fnmatchFilter)},
		{"translate", objects.NewBuiltinFunction("translate", translate)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// fnmatchFn lowercases name+pattern and dispatches to the case-aware
// matcher.
//
// CPython: Lib/fnmatch.py:36 fnmatch
func fnmatchFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: fnmatch() takes 2 arguments")
	}
	name, _ := objects.Str(args[0])
	pat, _ := objects.Str(args[1])
	return objects.NewBool(globMatch(strings.ToLower(name), strings.ToLower(pat))), nil
}

// fnmatchCase matches name against pattern case-sensitively.
//
// CPython: Lib/fnmatch.py:60 fnmatchcase
func fnmatchCase(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: fnmatchcase() takes 2 arguments")
	}
	name, _ := objects.Str(args[0])
	pat, _ := objects.Str(args[1])
	return objects.NewBool(globMatch(name, pat)), nil
}

// fnmatchFilter returns the subset of names that match pattern.
//
// CPython: Lib/fnmatch.py:48 filter
func fnmatchFilter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: filter() takes 2 arguments")
	}
	tp := args[0].Type()
	if tp.Iter == nil {
		return objects.NewList(nil), nil
	}
	it, err := tp.Iter(args[0])
	if err != nil {
		return nil, err
	}
	pat, _ := objects.Str(args[1])
	itType := it.Type()
	var out []objects.Object
	for {
		v, ierr := itType.IterNext(it)
		if ierr != nil || v == nil {
			break
		}
		s, _ := objects.Str(v)
		if globMatch(s, pat) {
			out = append(out, v)
		}
	}
	return objects.NewList(out), nil
}

// translate ports fnmatch.translate. Returns a regex-shaped string;
// without re wired up, callers cannot use it, but unittest's loader
// only ever calls fnmatch / fnmatchcase, so a no-op stub keeps the
// import alive.
//
// CPython: Lib/fnmatch.py:79 translate
func translate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: translate() takes 1 argument")
	}
	s, _ := objects.Str(args[0])
	return objects.NewStr(s), nil
}

// globMatch is a hand-rolled `*` `?` `[abc]` matcher. Mirrors the
// behavior of Python's fnmatch for the patterns unittest's loader
// produces (e.g. "test*.py").
//
// CPython: Lib/fnmatch.py:79 translate -> re.match
func globMatch(name, pat string) bool {
	return matchHere(name, pat, 0, 0)
}

func matchHere(name, pat string, i, j int) bool {
	for j < len(pat) {
		switch pat[j] {
		case '*':
			for k := j + 1; k < len(pat) && pat[k] == '*'; k++ {
				j = k
			}
			if j+1 == len(pat) {
				return true
			}
			for k := i; k <= len(name); k++ {
				if matchHere(name, pat, k, j+1) {
					return true
				}
			}
			return false
		case '?':
			if i >= len(name) {
				return false
			}
			i++
			j++
		case '[':
			closeIdx := strings.IndexByte(pat[j:], ']')
			if closeIdx < 0 || i >= len(name) {
				if i < len(name) && name[i] == pat[j] {
					i++
					j++
					continue
				}
				return false
			}
			cls := pat[j+1 : j+closeIdx]
			ch := name[i]
			matched := strings.IndexByte(cls, ch) >= 0
			if !matched {
				return false
			}
			i++
			j += closeIdx + 1
		default:
			if i >= len(name) || name[i] != pat[j] {
				return false
			}
			i++
			j++
		}
	}
	return i == len(name)
}
