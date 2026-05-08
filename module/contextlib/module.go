// contextlib module: minimal Go-backed surface. Lib/contextlib.py
// covers contextmanager, asynccontextmanager, ExitStack, suppress,
// nullcontext, redirect_stdout/stderr, AbstractContextManager. unittest
// pulls in `suppress` (case.py) and `contextmanager` decorator
// (assertWarns / assertLogs); this stub exposes the public names so
// the imports resolve. Real ports follow.
//
// CPython: Lib/contextlib.py the public surface

package contextlib

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("contextlib", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("contextlib")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"contextmanager", objects.NewBuiltinFunction("contextmanager", contextManager)},
		{"asynccontextmanager", objects.NewBuiltinFunction("asynccontextmanager", contextManager)},
		{"suppress", suppressType},
		{"nullcontext", nullcontextType},
		{"ExitStack", exitStackType},
		{"AsyncExitStack", exitStackType},
		{"closing", closingType},
		{"redirect_stdout", redirectType},
		{"redirect_stderr", redirectType},
		{"AbstractContextManager", abstractCMType},
		{"AbstractAsyncContextManager", abstractCMType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// contextManager is a stand-in for @contextmanager: returns the
// generator function unchanged. The real Lib/contextlib.contextmanager
// wraps the generator into a _GeneratorContextManager class; the
// simplification keeps decorated names callable for import-time code.
//
// CPython: Lib/contextlib.py:308 contextmanager
func contextManager(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.None(), nil
}

var (
	suppressType    = newPassThroughCM("suppress")
	nullcontextType = newPassThroughCM("nullcontext")
	exitStackType   = newPassThroughCM("ExitStack")
	closingType     = newPassThroughCM("closing")
	redirectType    = newPassThroughCM("redirect")
	abstractCMType  = objects.NewType("AbstractContextManager", []*objects.Type{objects.ObjectType()})
)

// newPassThroughCM builds a Type whose instances act as no-op context
// managers: __enter__ returns self (or the captured arg), __exit__
// returns False (suppresses nothing). Mirrors the surface unittest
// reaches for at module-load.
//
// CPython: Lib/contextlib.py:475 suppress / Lib/contextlib.py:728 nullcontext
func newPassThroughCM(name string) *objects.Type {
	t := objects.NewType(name, []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		inst := objects.NewInstance(cls)
		if len(args) >= 1 {
			_ = inst.Dict().SetItem(objects.NewStr("enter_result"), args[0])
		}
		objects.SetTypeDescr(cls, "__enter__", objects.NewMethodDescr(cls, "__enter__", cmEnter))
		objects.SetTypeDescr(cls, "__exit__", objects.NewMethodDescr(cls, "__exit__", cmExit))
		return inst, nil
	}
	return t
}

func cmEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		if inst, ok := args[0].(*objects.Instance); ok {
			if v, err := inst.Dict().GetItem(objects.NewStr("enter_result")); err == nil && v != nil {
				return v, nil
			}
		}
		return args[0], nil
	}
	return objects.None(), nil
}

func cmExit(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewBool(false), nil
}
