package testcapi

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// testWithDocstring is the docstring fixture test_descr drives as a property
// getter: it ignores its arguments and returns None.
//
// CPython: Modules/_testcapi/docstring.c:62 test_with_docstring
func testWithDocstring(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// badGet mimics a __get__ descriptor that instantiates the owning class
// before returning repr(self). bpo-25750 exercises a descriptor that deletes
// itself from the class during the call; the fixture must survive that.
//
// CPython: Modules/_testcapimodule.c:1705 bad_get
func badGet(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: bad_get expected 3 arguments, got %d", len(args))
	}
	self, _, cls := args[0], args[1], args[2]
	res, err := objects.Call(cls, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	objects.Decref(res)
	s, err := objects.Repr(self)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}
