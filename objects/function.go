package objects

// Function is a Python-defined function: a code object plus the
// captured globals, defaults, and closure cells. The runtime call
// arm pushes a new frame off the bound code, threads the callable's
// globals through to the frame's f_globals, and dispatches.
//
// v0.6 holds Code as Object so we don't need to import frame here;
// the call site does the type-check.
//
// CPython: Include/cpython/funcobject.h PyFunctionObject
type Function struct {
	Header
	Name       string
	Qualname   string
	Code       *Code
	Globals    Object
	Defaults   *Tuple // positional defaults, may be nil
	KwDefaults *Dict  // keyword-only defaults, may be nil
	Closure    *Tuple // tuple of cells, may be nil
}

// FunctionType is the type singleton for Python-defined functions.
//
// CPython: Objects/funcobject.c PyFunction_Type
var FunctionType = NewType("function", []*Type{objectType})

func init() {
	FunctionType.Repr = func(o Object) (string, error) {
		f := o.(*Function)
		return "<function " + f.Name + ">", nil
	}
	FunctionType.Str = FunctionType.Repr
	// FunctionType.Call is wired by the vm package when it imports
	// objects, since the call needs to push a new frame and drive Eval.
	// objects can't do that without circular imports, so the vm
	// initializes FunctionType.Call on package init.
}

// NewFunction builds a function from a code object plus its globals.
// Defaults / closure can be set on the returned value before exposing it.
//
// CPython: Objects/funcobject.c PyFunction_New
func NewFunction(name string, code *Code, globals Object) *Function {
	f := &Function{Name: name, Qualname: name, Code: code, Globals: globals}
	f.init(FunctionType)
	return f
}
