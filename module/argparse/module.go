// argparse module: minimal Go-backed surface. Lib/argparse.py is
// 2500 lines (ArgumentParser, action subtypes, formatter classes,
// HelpFormatter). unittest's __main__.py uses ArgumentParser to
// parse `-v`, `-q`, `-k`, `-f`, `--locals`. This stub gives an
// ArgumentParser whose add_argument records names and parse_args
// returns a Namespace whose attribute lookups default to None, which
// is what unittest's main does its own conditional branching on.
//
// CPython: Lib/argparse.py the public surface

package argparse

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("argparse", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("argparse")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"ArgumentParser", argParserType},
		{"Namespace", namespaceType},
		{"Action", actionType},
		{"FileType", fileType},
		{"ArgumentError", errorType},
		{"ArgumentTypeError", errorType},
		{"HelpFormatter", formatterType},
		{"RawDescriptionHelpFormatter", formatterType},
		{"RawTextHelpFormatter", formatterType},
		{"ArgumentDefaultsHelpFormatter", formatterType},
		{"MetavarTypeHelpFormatter", formatterType},
		{"BooleanOptionalAction", actionType},
		{"SUPPRESS", objects.NewStr("==SUPPRESS==")},
		{"OPTIONAL", objects.NewStr("?")},
		{"ZERO_OR_MORE", objects.NewStr("*")},
		{"ONE_OR_MORE", objects.NewStr("+")},
		{"PARSER", objects.NewStr("A...")},
		{"REMAINDER", objects.NewStr("...")},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var (
	argParserType = newArgParserType()
	namespaceType = newNamespaceType()
	actionType    = objects.NewType("Action", []*objects.Type{objects.ObjectType()})
	fileType      = objects.NewType("FileType", []*objects.Type{objects.ObjectType()})
	errorType     = objects.NewType("ArgumentError", []*objects.Type{objects.ObjectType()})
	formatterType = objects.NewType("HelpFormatter", []*objects.Type{objects.ObjectType()})
)

// newArgParserType returns a Type whose instance has add_argument /
// parse_args / parse_known_args methods. add_argument is a no-op;
// parse_args returns an empty Namespace.
//
// CPython: Lib/argparse.py:1670 ArgumentParser
func newArgParserType() *objects.Type {
	t := objects.NewType("ArgumentParser", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.Setattro = objects.GenericSetAttr
	t.TpNew = func(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		objects.SetTypeDescr(cls, "add_argument", objects.NewMethodDescr(cls, "add_argument", noop))
		objects.SetTypeDescr(cls, "add_argument_group", objects.NewMethodDescr(cls, "add_argument_group", returnSelf))
		objects.SetTypeDescr(cls, "add_mutually_exclusive_group", objects.NewMethodDescr(cls, "add_mutually_exclusive_group", returnSelf))
		objects.SetTypeDescr(cls, "add_subparsers", objects.NewMethodDescr(cls, "add_subparsers", returnSelf))
		objects.SetTypeDescr(cls, "parse_args", objects.NewMethodDescr(cls, "parse_args", parseArgs))
		objects.SetTypeDescr(cls, "parse_known_args", objects.NewMethodDescr(cls, "parse_known_args", parseKnownArgs))
		objects.SetTypeDescr(cls, "set_defaults", objects.NewMethodDescr(cls, "set_defaults", noop))
		objects.SetTypeDescr(cls, "error", objects.NewMethodDescr(cls, "error", noop))
		objects.SetTypeDescr(cls, "print_help", objects.NewMethodDescr(cls, "print_help", noop))
		objects.SetTypeDescr(cls, "format_help", objects.NewMethodDescr(cls, "format_help", emptyStr))
		return inst, nil
	}
	return t
}

func newNamespaceType() *objects.Type {
	t := objects.NewType("Namespace", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		if inst, ok := o.(*objects.Instance); ok {
			if v, err := inst.Dict().GetItem(name); err == nil && v != nil {
				return v, nil
			}
		}
		return objects.None(), nil
	}
	t.TpNew = func(cls *objects.Type, _ []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		for k, v := range kwargs {
			_ = inst.Dict().SetItem(objects.NewStr(k), v)
		}
		return inst, nil
	}
	return t
}

func parseArgs(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInstance(namespaceType), nil
}

func parseKnownArgs(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewTuple([]objects.Object{
		objects.NewInstance(namespaceType),
		objects.NewList(nil),
	}), nil
}

func noop(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

func emptyStr(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewStr(""), nil
}

func returnSelf(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.None(), nil
}
