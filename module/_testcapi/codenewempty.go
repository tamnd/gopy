// Port of CPython's PyCode_NewEmpty, exposed to the test suite as
// _testcapi.code_newempty. test_code.py drives it to build a minimal
// code object that raises AssertionError on exec.
//
// CPython: Objects/codeobject.c:955 PyCode_NewEmpty

package testcapi

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// assert0 is the six-byte bytecode PyCode_NewEmpty installs: resume at
// function start, load the AssertionError common constant, then raise
// it with one argument count.
//
// CPython: Objects/codeobject.c:941 assert0
var assert0 = []byte{
	128, 0, // RESUME, RESUME_AT_FUNC_START
	81, 0, // LOAD_COMMON_CONSTANT, CONSTANT_ASSERTIONERROR
	104, 1, // RAISE_VARARGS, 1
}

// emptyLinetable is the two-byte location table covering the three code
// units of assert0 with no column info and a zero line offset.
//
// CPython: Objects/codeobject.c:947 linetable
//
//	(1<<7) | (PY_CODE_LOCATION_INFO_NO_COLUMNS<<3) | (3-1) = 0xEA
var emptyLinetable = []byte{0xEA, 0}

// codeNewempty ports _testcapi.code_newempty(filename, funcname,
// firstlineno), a thin wrapper over PyCode_NewEmpty.
//
// CPython: Modules/_testcapimodule.c code_newempty / Objects/codeobject.c:955
func codeNewempty(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: code_newempty expected 3 arguments, got %d", len(args))
	}
	filename, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument 1 must be str, not %s", args[0].Type().Name)
	}
	funcname, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument 2 must be str, not %s", args[1].Type().Name)
	}
	lineno, ok := args[2].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument 3 must be int, not %s", args[2].Type().Name)
	}
	firstlineno, _ := lineno.Int64()

	c := objects.NewCode()
	c.Filename = filename.Value()
	c.Name = funcname.Value()
	c.Qualname = funcname.Value()
	c.Code = append([]byte(nil), assert0...)
	c.Firstlineno = int(firstlineno)
	c.Linetable = append([]byte(nil), emptyLinetable...)
	c.Stacksize = 1
	c.SyncConstObjs()
	c.SyncNameObjs()
	c.SyncLocalsplusCounts()
	return c, nil
}
