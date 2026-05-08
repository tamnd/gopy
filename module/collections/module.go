// collections built-in module: minimal Go-backed surface for the
// names unittest pulls in at import. The full Lib/collections/__init__.py
// is 1600 lines (namedtuple metaclass machinery, OrderedDict, deque,
// ChainMap, UserDict / UserList / UserString) and depends on
// _collections_abc, which is another 1100. Vendoring the chain is its
// own project; until that lands this module exposes the public names
// stamping subclassable Type objects in their place so `import
// collections` and `from collections import namedtuple, Counter`
// resolve.
//
// CPython: Lib/collections/__init__.py the public surface

package collections

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("collections", buildModule)
}

// buildModule materializes the collections module dict.
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("collections")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		{"namedtuple", objects.NewBuiltinFunction("namedtuple", namedtupleCtor)},
		{"Counter", counterType},
		{"OrderedDict", orderedDictType},
		{"defaultdict", defaultDictType},
		{"deque", dequeType},
		{"ChainMap", chainMapType},
		{"UserDict", userDictType},
		{"UserList", userListType},
		{"UserString", userStringType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// namedtupleCtor ports collections.namedtuple. The CPython version
// uses exec() over a generated source string; that path needs the
// full eval loop and a pile of templating. For now produce a class
// whose Call returns a tuple-shaped Instance with the named fields
// bound in __dict__. Field-name iteration matches CPython for the
// import-time use case (tuple-of-strings or whitespace string) so
// that `_LoggingWatcher = namedtuple("_LoggingWatcher", ["records",
// "output"])` resolves as a callable class.
//
// CPython: Lib/collections/__init__.py:387 namedtuple
func namedtupleCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: namedtuple() missing 'typename' or 'field_names'")
	}
	typename, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: namedtuple() typename must be str")
	}
	fields, err := extractFieldNames(args[1])
	if err != nil {
		return nil, err
	}
	t := objects.NewType(typename.Value(), []*objects.Type{objects.TupleType})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, callArgs []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(callArgs) < len(fields) {
			return nil, fmt.Errorf("TypeError: %s() missing positional arguments", typename.Value())
		}
		inst := objects.NewInstance(cls)
		dict := inst.Dict()
		for i, f := range fields {
			if err := dict.SetItem(objects.NewStr(f), callArgs[i]); err != nil {
				return nil, err
			}
		}
		if err := dict.SetItem(objects.NewStr("_fields"), fieldTuple(fields)); err != nil {
			return nil, err
		}
		return inst, nil
	}
	return t, nil
}

func extractFieldNames(obj objects.Object) ([]string, error) {
	if s, ok := obj.(*objects.Unicode); ok {
		val := s.Value()
		var names []string
		cur := ""
		for _, ch := range val {
			if ch == ' ' || ch == ',' || ch == '\t' {
				if cur != "" {
					names = append(names, cur)
					cur = ""
				}
				continue
			}
			cur += string(ch)
		}
		if cur != "" {
			names = append(names, cur)
		}
		return names, nil
	}
	tp := obj.Type()
	if tp.Iter == nil {
		return nil, fmt.Errorf("TypeError: namedtuple() field_names must be a sequence")
	}
	it, err := tp.Iter(obj)
	if err != nil {
		return nil, err
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, fmt.Errorf("TypeError: namedtuple() field_names iterator is invalid")
	}
	var out []string
	for {
		v, ierr := itType.IterNext(it)
		if ierr != nil {
			break
		}
		if v == nil {
			break
		}
		s, _ := objects.Str(v)
		out = append(out, s)
	}
	return out, nil
}

func fieldTuple(fields []string) objects.Object {
	items := make([]objects.Object, len(fields))
	for i, f := range fields {
		items[i] = objects.NewStr(f)
	}
	return objects.NewTuple(items)
}

// counterType / orderedDictType / defaultDictType / dequeType /
// chainMapType / userDictType / userListType / userStringType: stub
// classes whose Call delegates to dict / list / str so that the
// common `Counter()`, `OrderedDict()`, `defaultdict(int)` shapes
// behave as their primitive counterparts.
//
// CPython: Lib/collections/__init__.py the named classes
var (
	counterType     = newDictAlias("Counter")
	orderedDictType = newDictAlias("OrderedDict")
	defaultDictType = newDictAlias("defaultdict")
	dequeType       = newListAlias("deque")
	chainMapType    = newDictAlias("ChainMap")
	userDictType    = newDictAlias("UserDict")
	userListType    = newListAlias("UserList")
	userStringType  = newStringAlias("UserString")
)

func newDictAlias(name string) *objects.Type {
	t := objects.NewType(name, []*objects.Type{objects.DictType})
	t.HasDict = true
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		_ = cls
		_ = args
		return objects.NewDict(), nil
	}
	return t
}

func newListAlias(name string) *objects.Type {
	t := objects.NewType(name, []*objects.Type{objects.ListType})
	t.HasDict = true
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		_ = cls
		if len(args) >= 1 {
			tp := args[0].Type()
			if tp.Iter != nil {
				it, err := tp.Iter(args[0])
				if err == nil {
					var items []objects.Object
					itType := it.Type()
					for {
						v, ierr := itType.IterNext(it)
						if ierr != nil || v == nil {
							break
						}
						items = append(items, v)
					}
					return objects.NewList(items), nil
				}
			}
		}
		return objects.NewList(nil), nil
	}
	return t
}

func newStringAlias(name string) *objects.Type {
	t := objects.NewType(name, []*objects.Type{objects.StrType()})
	t.HasDict = true
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		_ = cls
		if len(args) >= 1 {
			s, err := objects.Str(args[0])
			if err == nil {
				return objects.NewStr(s), nil
			}
		}
		return objects.NewStr(""), nil
	}
	return t
}
