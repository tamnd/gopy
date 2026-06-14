// Package xxsubtype is the gopy port of CPython's Modules/xxsubtype.c, the
// example extension that subtypes the builtin list and dict types from C.
// test.test_descr requires it for its spamlist/spamdict, classmethods_in_c
// and staticmethods_in_c cases; without the module those four tests skip,
// which would diverge from CPython (which links xxsubtype statically and
// runs them).
//
// spamlist subclasses list and spamdict subclasses dict, each adding an int
// "state" plus getstate()/setstate() accessors. spamlist also exposes a
// "state" getset and the classmeth/staticmeth fixtures that exercise the
// METH_CLASS / METH_STATIC binding paths. gopy models the per-instance int
// state in a side table keyed by object identity, mirroring CPython's
// dedicated C struct slot that lives outside the instance __dict__.
//
// CPython: Modules/xxsubtype.c:1
package xxsubtype

import (
	"fmt"
	"sync"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("xxsubtype", buildModule)
}

// stateTable holds the per-instance int state. CPython stores it in the
// spamlistobject/spamdictobject C struct (offset past the base object); gopy
// keeps it in a side map keyed by the instance pointer so the base *List /
// *Dict layout is untouched and the value never leaks into __dict__.
var (
	stateMu    sync.Mutex
	stateTable = map[objects.Object]int64{}
)

func getState(self objects.Object) int64 {
	stateMu.Lock()
	defer stateMu.Unlock()
	return stateTable[self]
}

func setState(self objects.Object, v int64) {
	stateMu.Lock()
	defer stateMu.Unlock()
	stateTable[self] = v
}

// getstate implements the METH_VARARGS getstate(): it takes no arguments and
// returns the stored state as an int.
//
// CPython: Modules/xxsubtype.c:33 spamlist_getstate / :164 spamdict_getstate
func getstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self := args[0]
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: getstate() takes no arguments (%d given)", len(args)-1)
	}
	return objects.NewInt(getState(self)), nil
}

// setstate implements the METH_VARARGS setstate(state): it parses a single
// int and stores it, returning None.
//
// CPython: Modules/xxsubtype.c:43 spamlist_setstate / :174 spamdict_setstate
func setstate(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self := args[0]
	rest := args[1:]
	if len(rest) != 1 {
		return nil, fmt.Errorf("TypeError: setstate() takes exactly one argument (%d given)", len(rest))
	}
	iv, ok := rest[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", rest[0].Type().Name)
	}
	v, _ := iv.Int64()
	setState(self, v)
	return objects.None(), nil
}

// stateGet is the read accessor shared by spamlist's getset "state" and
// spamdict's read-only "state" member.
//
// CPython: Modules/xxsubtype.c:96 spamlist_state_get / :208 spamdict_members
func stateGet(owner objects.Object) (objects.Object, error) {
	return objects.NewInt(getState(owner)), nil
}

// specialMethDef builds the classmeth/staticmeth fixture: it returns the
// (self, args, kw) triple so the test can confirm which object the binding
// passed through. self is the class for METH_CLASS and None for METH_STATIC.
//
// CPython: Modules/xxsubtype.c:54 spamlist_specialmeth
func specialMethDef(name string) *objects.MethodDef {
	return &objects.MethodDef{
		Name:  name,
		Flags: objects.MethVarargs | objects.MethKeywords,
		VarargsKw: func(self objects.Object, args *objects.Tuple, kw *objects.Dict) (objects.Object, error) {
			kwObj := objects.Object(kw)
			if kw == nil {
				kwObj = objects.None()
			}
			return objects.NewTuple([]objects.Object{self, args, kwObj}), nil
		},
	}
}

// spamBench implements xxsubtype.bench(obj, name[, n]): it fetches name off
// obj n times and reports elapsed time. gopy reports 0.0 (the wall-clock
// figure is informational and untested); the attribute fetches still run so
// errors surface.
//
// CPython: Modules/xxsubtype.c:271 spam_bench
func spamBench(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: bench() takes 2 or 3 arguments (%d given)", len(args))
	}
	obj := args[0]
	name := args[1]
	n := int64(1000)
	if len(args) == 3 {
		iv, ok := args[2].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[2].Type().Name)
		}
		n, _ = iv.Int64()
	}
	for ; n > 0; n-- {
		if _, err := objects.GetAttr(obj, name); err != nil {
			return nil, err
		}
	}
	return objects.NewFloat(0.0), nil
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("xxsubtype")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("__doc__"), objects.NewStr(xxsubtypeDoc)); err != nil {
		return nil, err
	}

	// spamlist: a list subtype. TpNew is inherited from list, so an
	// instance is a *List carrying the spamlist type; the side table holds
	// its int state.
	//
	// CPython: Modules/xxsubtype.c:107 spamlist_type
	spamlist := objects.NewType("xxsubtype.spamlist", []*objects.Type{objects.ListType})
	objects.SetTypeDescr(spamlist, "getstate", objects.NewMethodDescrConv(spamlist, "getstate", objects.MethVarargs, getstate))
	objects.SetTypeDescr(spamlist, "setstate", objects.NewMethodDescrConv(spamlist, "setstate", objects.MethVarargs, setstate))
	objects.SetTypeDescr(spamlist, "state", objects.NewGetSetDescr("state", stateGet, nil))
	objects.SetTypeDescr(spamlist, "classmeth", objects.NewClassMethodDescr(spamlist, specialMethDef("classmeth")))
	staticCF, err := objects.NewCFunction(specialMethDef("staticmeth"), objects.None(), nil, nil)
	if err != nil {
		return nil, err
	}
	objects.SetTypeDescr(spamlist, "staticmeth", objects.NewStaticMethod(staticCF))
	objects.ReadyBuiltinSubtype(spamlist)

	// spamdict: a dict subtype with getstate/setstate and a read-only
	// "state" accessor (a Py_READONLY member in C; gopy uses a getset with
	// no setter so writes raise the same AttributeError).
	//
	// CPython: Modules/xxsubtype.c:207 spamdict_type
	spamdict := objects.NewType("xxsubtype.spamdict", []*objects.Type{objects.DictType})
	objects.SetTypeDescr(spamdict, "getstate", objects.NewMethodDescrConv(spamdict, "getstate", objects.MethVarargs, getstate))
	objects.SetTypeDescr(spamdict, "setstate", objects.NewMethodDescrConv(spamdict, "setstate", objects.MethVarargs, setstate))
	objects.SetTypeDescr(spamdict, "state", objects.NewGetSetDescr("state", stateGet, nil))
	objects.ReadyBuiltinSubtype(spamdict)

	if err := d.SetItem(objects.NewStr("spamlist"), spamlist); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("spamdict"), spamdict); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("bench"), objects.NewBuiltinFunction("bench", spamBench)); err != nil {
		return nil, err
	}
	return m, nil
}

const xxsubtypeDoc = "xxsubtype is an example module showing how to subtype builtin types from C.\n" +
	"test_descr.py in the standard test suite requires it in order to complete.\n" +
	"If you don't care about the examples, and don't intend to run the Python\n" +
	"test suite, you can recompile Python without Modules/xxsubtype.c."
