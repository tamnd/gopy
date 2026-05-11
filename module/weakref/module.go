// weakref module: minimal Go-backed surface. Lib/weakref.py is
// 700 lines (ref/proxy/WeakSet/WeakValueDictionary/WeakKeyDictionary/
// finalize). gopy doesn't surface weak references yet, so this stub
// treats `ref` as a closure capturing the strong reference and
// returning it on call. Sufficient for unittest's loader, which only
// uses ref() as a callback hook.
//
// CPython: Lib/weakref.py the public surface

package weakref

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("weakref", buildModule)
	_ = imp.AppendInittab("_weakref", buildModule)
	_ = imp.AppendInittab("_weakrefset", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("weakref")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"ref", refType},
		{"proxy", objects.NewBuiltinFunction("proxy", identity)},
		{"getweakrefcount", objects.NewBuiltinFunction("getweakrefcount", zeroInt)},
		{"getweakrefs", objects.NewBuiltinFunction("getweakrefs", emptyList)},
		// The WeakSet / WeakValue / WeakKey shims masquerade as the
		// real types so unittest.signals and functools.singledispatch
		// can use them as drop-in containers. They have no actual
		// weakref semantics yet; that lands when 1517 ports weakref.
		{"WeakSet", objects.SetType},
		{"WeakValueDictionary", objects.DictType},
		{"WeakKeyDictionary", objects.DictType},
		{"WeakMethod", refType},
		{"finalize", finalizeType},
		{"ReferenceType", refType},
		{"ProxyType", proxyType},
		{"CallableProxyType", proxyType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var (
	refType           = newRefType()
	proxyType         = objects.NewType("ProxyType", []*objects.Type{objects.ObjectType()})
	weakSetType       = objects.NewType("WeakSet", []*objects.Type{objects.ObjectType()})
	weakValueDictType = objects.NewType("WeakValueDictionary", []*objects.Type{objects.DictType})
	weakKeyDictType   = objects.NewType("WeakKeyDictionary", []*objects.Type{objects.DictType})
	finalizeType      = objects.NewType("finalize", []*objects.Type{objects.ObjectType()})
)

// newRefType builds a callable type that returns the captured
// referent when invoked. Mirrors weakref.ref enough for callbacks.
//
// CPython: Lib/weakref.py:78 ref / Modules/_weakref.c:280
func init() {
	for _, t := range []*objects.Type{weakSetType, weakValueDictType, weakKeyDictType, finalizeType, proxyType} {
		t.HasDict = true
		t.TpNew = func(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewInstance(cls), nil
		}
	}
}

func newRefType() *objects.Type {
	t := objects.NewType("ref", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		captured := objects.None()
		if len(args) >= 1 {
			captured = args[0]
		}
		inst := objects.NewInstance(cls)
		_ = inst.Dict().SetItem(objects.NewStr("__referent__"), captured)
		t.Call = func(o objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if i, ok := o.(*objects.Instance); ok {
				if v, err := i.Dict().GetItem(objects.NewStr("__referent__")); err == nil && v != nil {
					return v, nil
				}
			}
			return objects.None(), nil
		}
		return inst, nil
	}
	return t
}

func identity(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.None(), nil
}

func zeroInt(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}

func emptyList(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewList(nil), nil
}
