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
		objects.SetTypeDescr(cls, "add_argument", objects.NewMethodDescr(cls, "add_argument", addArgument))
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

// addArgument records the dest/default pair for each registered
// option on the parser instance so parseArgs can later stamp the
// caller-supplied namespace. CPython's argparse derives dest from the
// first long option name when not given; we replicate the minimum
// needed by unittest: an explicit kwargs["dest"], otherwise the first
// positional name with leading dashes stripped.
//
// CPython: Lib/argparse.py:1453 _ActionsContainer.add_argument
func addArgument(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	self, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.None(), nil
	}
	dest := destFromArgs(args[1:], kwargs)
	if dest == "" {
		return objects.None(), nil
	}
	def := kwargs["default"]
	if def == nil {
		def = objects.None()
	}
	registered, _ := self.Dict().GetItem(registeredKey)
	lst, _ := registered.(*objects.List)
	if lst == nil {
		lst = objects.NewList(nil)
		_ = self.Dict().SetItem(registeredKey, lst)
	}
	lst.Append(objects.NewTuple([]objects.Object{objects.NewStr(dest), def}))
	return objects.None(), nil
}

var registeredKey = objects.NewStr("_argparse_registered")

func destFromArgs(names []objects.Object, kwargs map[string]objects.Object) string {
	if d, ok := kwargs["dest"]; ok {
		if s, ok := d.(*objects.Unicode); ok {
			return s.Value()
		}
	}
	for _, n := range names {
		s, ok := n.(*objects.Unicode)
		if !ok {
			continue
		}
		v := s.Value()
		if v == "" {
			continue
		}
		if v[0] != '-' {
			return v
		}
	}
	for _, n := range names {
		s, ok := n.(*objects.Unicode)
		if !ok {
			continue
		}
		v := s.Value()
		for len(v) > 0 && v[0] == '-' {
			v = v[1:]
		}
		if v != "" {
			return replaceDashes(v)
		}
	}
	return ""
}

func replaceDashes(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] == '-' {
			b[i] = '_'
		}
	}
	return string(b)
}

// parseArgs returns the caller-supplied namespace (when present)
// pre-populated with each registered dest's default; otherwise builds
// a fresh Namespace. Actual argv parsing is not implemented: unittest
// branches on whether the namespace attrs are truthy, and the
// defaults are enough to fall through to the discovery path.
//
// CPython: Lib/argparse.py:1916 ArgumentParser.parse_args
func parseArgs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	ns := namespaceFromCall(args, kwargs)
	stampDefaults(args, ns)
	return ns, nil
}

func parseKnownArgs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	ns := namespaceFromCall(args, kwargs)
	stampDefaults(args, ns)
	return objects.NewTuple([]objects.Object{ns, objects.NewList(nil)}), nil
}

func namespaceFromCall(args []objects.Object, kwargs map[string]objects.Object) *objects.Instance {
	// args[0] = self (parser); args[1] = argv; args[2] = namespace.
	if len(args) >= 3 {
		if inst, ok := args[2].(*objects.Instance); ok {
			return inst
		}
	}
	if v, ok := kwargs["namespace"]; ok {
		if inst, ok := v.(*objects.Instance); ok {
			return inst
		}
	}
	return objects.NewInstance(namespaceType)
}

func stampDefaults(args []objects.Object, ns *objects.Instance) {
	if len(args) < 1 || ns == nil {
		return
	}
	self, ok := args[0].(*objects.Instance)
	if !ok {
		return
	}
	registered, _ := self.Dict().GetItem(registeredKey)
	lst, _ := registered.(*objects.List)
	if lst == nil {
		return
	}
	for i := 0; i < lst.Len(); i++ {
		entry := lst.Item(i)
		tup, _ := entry.(*objects.Tuple)
		if tup == nil || tup.Len() < 2 {
			continue
		}
		nameObj := tup.Item(0)
		defObj := tup.Item(1)
		nameStr, ok := nameObj.(*objects.Unicode)
		if !ok {
			continue
		}
		key := objects.NewStr(nameStr.Value())
		if has, _ := ns.Dict().Contains(key); has {
			continue
		}
		_ = ns.Dict().SetItem(key, defObj)
	}
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
