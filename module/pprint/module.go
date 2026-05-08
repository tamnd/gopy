// pprint module: minimal Go-backed surface. Lib/pprint.py is a
// 700-line pretty-printer with PrettyPrinter class, dispatch table per
// type, width-aware wrapping. unittest pulls `pprint.pformat` via
// case.py for assertion repr fallbacks; this stub forwards to repr.
//
// CPython: Lib/pprint.py the public surface

package pprint

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("pprint", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("pprint")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"pprint", objects.NewBuiltinFunction("pprint", pprintFn)},
		{"pformat", objects.NewBuiltinFunction("pformat", pformatFn)},
		{"saferepr", objects.NewBuiltinFunction("saferepr", pformatFn)},
		{"isreadable", objects.NewBuiltinFunction("isreadable", trueFn)},
		{"isrecursive", objects.NewBuiltinFunction("isrecursive", falseFn)},
		{"PrettyPrinter", prettyPrinterType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var prettyPrinterType = objects.NewType("PrettyPrinter", []*objects.Type{objects.ObjectType()})

func pprintFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		s, _ := objects.Repr(args[0])
		fmt.Println(s)
	}
	return objects.None(), nil
}

func pformatFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.NewStr(""), nil
	}
	s, _ := objects.Repr(args[0])
	return objects.NewStr(s), nil
}

func trueFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBool(true), nil
}

func falseFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBool(false), nil
}
