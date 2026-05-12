// warnings module: minimal Go-backed surface. gopy already has a
// warnings/ package implementing _Py_Warn semantics for the C side;
// this stub exposes the public Python names (warn, filterwarnings,
// catch_warnings, simplefilter) so unittest's import resolves.
//
// CPython: Lib/warnings.py the public surface

package warnings

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("warnings", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("warnings")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"warn", objects.NewBuiltinFunction("warn", noop)},
		{"warn_explicit", objects.NewBuiltinFunction("warn_explicit", noop)},
		{"filterwarnings", objects.NewBuiltinFunction("filterwarnings", noop)},
		{"simplefilter", objects.NewBuiltinFunction("simplefilter", noop)},
		{"resetwarnings", objects.NewBuiltinFunction("resetwarnings", noop)},
		{"showwarning", objects.NewBuiltinFunction("showwarning", noop)},
		{"formatwarning", objects.NewBuiltinFunction("formatwarning", emptyStr)},
		{"catch_warnings", catchWarningsType},
		{"WarningMessage", warningMessageType},
		{"filters", objects.NewList(nil)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func noop(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

func emptyStr(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewStr(""), nil
}

var (
	catchWarningsType  = newCatchWarningsType()
	warningMessageType = objects.NewType("WarningMessage", []*objects.Type{objects.ObjectType()})
)

func newCatchWarningsType() *objects.Type {
	t := objects.NewType("catch_warnings", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		objects.SetTypeDescr(cls, "__enter__", objects.NewMethodDescr(cls, "__enter__", catchEnter))
		objects.SetTypeDescr(cls, "__exit__", objects.NewMethodDescr(cls, "__exit__", catchExit))
		return inst, nil
	}
	return t
}

func catchEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.None(), nil
}

func catchExit(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}
