// _colorize module: minimal Go-backed surface. Lib/_colorize.py
// provides get_theme/can_colorize/decolor for the traceback / unittest
// pretty-printers. unittest's runner.py imports it. This stub returns
// a default theme object whose `t.<attr>` lookups fall back to empty
// strings so format strings interpolate cleanly.
//
// CPython: Lib/_colorize.py the public surface

package colorize

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_colorize", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_colorize")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"get_theme", objects.NewBuiltinFunction("get_theme", getTheme)},
		{"can_colorize", objects.NewBuiltinFunction("can_colorize", falseFn)},
		{"decolor", objects.NewBuiltinFunction("decolor", identity)},
		{"set_theme", objects.NewBuiltinFunction("set_theme", noop)},
		{"reset_theme", objects.NewBuiltinFunction("reset_theme", noop)},
		{"ANSIColors", themeType},
		{"NoColors", themeType},
		{"Theme", themeType},
		{"default_theme", themeInst()},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// themeType backs the top-level Theme. Attribute access on a theme
// returns one of its sections (argparse / syntax / traceback / unittest);
// each section in turn exposes the named color slots as strings. The
// stub matches the no-colors variant of CPython's get_theme: every slot
// is the empty string, which makes the f-string interpolation in
// runner.py / traceback render uncolored output.
//
// CPython: Lib/_colorize.py:211 Theme
var themeType = newThemeType()

// sectionType is shared by argparse / syntax / traceback / unittest
// sections. Attribute access returns the empty string for every name,
// matching the no_colors() theme variant where all color codes are
// disabled.
//
// CPython: Lib/_colorize.py:115 ThemeSection
var sectionType = newSectionType()

func newThemeType() *objects.Type {
	t := objects.NewType("Theme", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		if inst, ok := o.(*objects.Instance); ok {
			if v, err := inst.Dict().GetItem(name); err == nil && v != nil {
				return v, nil
			}
		}
		return objects.NewInstance(sectionType), nil
	}
	t.TpNew = func(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInstance(cls), nil
	}
	return t
}

func newSectionType() *objects.Type {
	t := objects.NewType("ThemeSection", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		if inst, ok := o.(*objects.Instance); ok {
			if v, err := inst.Dict().GetItem(name); err == nil && v != nil {
				return v, nil
			}
		}
		return objects.NewStr(""), nil
	}
	return t
}

func themeInst() objects.Object {
	return objects.NewInstance(themeType)
}

func getTheme(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return themeInst(), nil
}

func falseFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBool(false), nil
}

func noop(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

func identity(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.NewStr(""), nil
}
