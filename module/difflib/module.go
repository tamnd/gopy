// difflib module: minimal Go-backed surface. Lib/difflib.py is the
// SequenceMatcher / Differ / unified_diff family; unittest case.py
// uses `ndiff` for assertion failure messages. This stub returns a
// trivial line-by-line listing prefixed with `  ` so failure diffs
// remain readable until the real port lands.
//
// CPython: Lib/difflib.py the public surface

package difflib

import (
	"strings"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("difflib", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("difflib")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"ndiff", objects.NewBuiltinFunction("ndiff", ndiff)},
		{"unified_diff", objects.NewBuiltinFunction("unified_diff", ndiff)},
		{"context_diff", objects.NewBuiltinFunction("context_diff", ndiff)},
		{"get_close_matches", objects.NewBuiltinFunction("get_close_matches", getCloseMatches)},
		{"SequenceMatcher", sequenceMatcherType},
		{"Differ", differType},
		{"HtmlDiff", htmlDiffType},
		{"IS_LINE_JUNK", objects.NewBuiltinFunction("IS_LINE_JUNK", falseFn)},
		{"IS_CHARACTER_JUNK", objects.NewBuiltinFunction("IS_CHARACTER_JUNK", falseFn)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var (
	sequenceMatcherType = objects.NewType("SequenceMatcher", []*objects.Type{objects.ObjectType()})
	differType          = objects.NewType("Differ", []*objects.Type{objects.ObjectType()})
	htmlDiffType        = objects.NewType("HtmlDiff", []*objects.Type{objects.ObjectType()})
)

func collectLines(o objects.Object) []string {
	tp := o.Type()
	if tp.Iter == nil {
		return nil
	}
	it, err := tp.Iter(o)
	if err != nil {
		return nil
	}
	itType := it.Type()
	var out []string
	for {
		v, ierr := itType.IterNext(it)
		if ierr != nil || v == nil {
			break
		}
		s, _ := objects.Str(v)
		out = append(out, s)
	}
	return out
}

// ndiff yields a `-` / `+` / `  ` prefixed merge of the two line
// streams, mirroring difflib.ndiff's output enough for unittest
// assertion messages to be readable.
//
// CPython: Lib/difflib.py:846 ndiff
func ndiff(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return objects.NewList(nil), nil
	}
	a := collectLines(args[0])
	b := collectLines(args[1])
	var out []objects.Object
	for _, line := range a {
		if !contains(b, line) {
			out = append(out, objects.NewStr("- "+strings.TrimRight(line, "\n")))
		}
	}
	for _, line := range b {
		if !contains(a, line) {
			out = append(out, objects.NewStr("+ "+strings.TrimRight(line, "\n")))
		}
	}
	return objects.NewList(out), nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func getCloseMatches(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewList(nil), nil
}

func falseFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBool(false), nil
}
