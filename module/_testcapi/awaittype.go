package testcapi

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// awaitObject backs the _testcapi.awaitType fixture: a minimal C-level
// type whose tp_as_async->am_await returns a stored object. The test
// suite uses it to exercise the tp_await to __await__ mapping, including
// the non-iterator error path.
//
// CPython: Modules/_testcapimodule.c:2796 awaitObject
type awaitObject struct {
	objects.Header
	iterator objects.Object
}

// awaitType is the type singleton for awaitObject instances.
//
// CPython: Modules/_testcapimodule.c:2846 awaitType
var awaitType *objects.Type

// awaitObjectAwait ports awaitObject_await: the am_await slot hands back
// the stored object unchanged. await machinery then validates it is an
// iterator, so storing a non-iterator surfaces the "__await__ returned
// non-iterator" TypeError the test expects.
//
// CPython: Modules/_testcapimodule.c:2832 awaitObject_await
func awaitObjectAwait(o objects.Object) (objects.Object, error) {
	ao, ok := o.(*awaitObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: am_await requires an awaitType object")
	}
	return ao.iterator, nil
}

// awaitObjectAwaitMethod exposes the same slot as the __await__ method
// descriptor so future.__await__() reads back the stored object.
//
// CPython: Modules/_testcapimodule.c:2838 awaitType_as_async.am_await
func awaitObjectAwaitMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __await__() missing self argument")
	}
	return awaitObjectAwait(args[0])
}

// awaitObjectNew ports awaitObject_new: unpack exactly one positional
// argument and store it on the new instance.
//
// CPython: Modules/_testcapimodule.c:2803 awaitObject_new
func awaitObjectNew(_ *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: awaitObject expected exactly 1 argument (%d given)", len(args))
	}
	// Py_NewRef(v): the new instance holds a strong reference to the
	// stored object, keeping the iterator (and the sequence it walks)
	// alive for the lifetime of the awaitObject.
	objects.Incref(args[0])
	ao := &awaitObject{iterator: args[0]}
	ao.Init(awaitType)
	return ao, nil
}

// awaitObjectTraverse visits the stored object so the cycle collector
// keeps it reachable while the awaitObject is live.
//
// CPython: Modules/_testcapimodule.c:2796 awaitObject (ao_iterator)
func awaitObjectTraverse(o objects.Object, visit objects.Visitor) error {
	ao, ok := o.(*awaitObject)
	if !ok || ao.iterator == nil {
		return nil
	}
	return visit(ao.iterator)
}

func init() {
	awaitType = objects.NewType("awaitType", []*objects.Type{objects.ObjectType()})
	awaitType.TpNew = awaitObjectNew
	awaitType.TpTraverse = awaitObjectTraverse
	awaitType.Async = &objects.AsyncMethods{Await: awaitObjectAwait}
	objects.SetTypeDescr(awaitType, "__await__",
		objects.NewMethodDescr(awaitType, "__await__", awaitObjectAwaitMethod))
}
