// MethodFunc bridges a Go closure into the descriptor protocol the
// way Python's `def` does: attribute access on an instance binds self
// (returning a BoundMethod), attribute access on a class returns the
// callable unchanged, and a bare call passes args straight through.
// PyCFunction (BuiltinFunction in gopy) lacks tp_descr_get on purpose
// (CPython faithfulness: Objects/methodobject.c:357 PyCFunction_Type
// has no Py_TPFLAGS_METHOD_DESCRIPTOR), so it cannot stand in for a
// Python def when a decorator must work both as a free function and
// as a method.
//
// CPython: Objects/funcobject.c:411 PyFunction_NewWithQualName
// CPython: Objects/funcobject.c:1057 func_descr_get

package objects

// MethodFunc is the gopy-internal callable used when a Go closure needs
// the full PyFunction descriptor surface but cannot back the body with
// bytecode (e.g. the @contextmanager helper).
type MethodFunc struct {
	Header
	name string
	fn   func(args []Object, kwargs map[string]Object) (Object, error)
}

// MethodFuncType reports its Python name as "function" so user code
// that inspects type(helper).__name__ matches CPython's PyFunction.
//
// CPython: Objects/funcobject.c:1370 PyFunction_Type
var MethodFuncType = NewType("function", []*Type{objectType})

func init() {
	MethodFuncType.Repr = methodFuncRepr
	MethodFuncType.Str = methodFuncRepr
	MethodFuncType.Call = methodFuncCall
	MethodFuncType.DescrGet = methodFuncDescrGet
	MethodFuncType.Hash = identityHash
}

// NewMethodFunc wraps fn so it dispatches like a Python def.
func NewMethodFunc(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) *MethodFunc {
	f := &MethodFunc{name: name, fn: fn}
	f.init(MethodFuncType)
	return f
}

// Name returns the function name surfaced through __name__.
func (f *MethodFunc) Name() string { return f.name }

func methodFuncRepr(o Object) (string, error) {
	f := o.(*MethodFunc)
	return "<function " + f.name + ">", nil
}

func methodFuncCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	f := o.(*MethodFunc)
	return f.fn(args, kwargs)
}

// methodFuncDescrGet implements the function descriptor protocol:
// `instance.method` returns a BoundMethod, `Class.method` returns the
// raw function. owner==nil signals access through the class itself,
// which mirrors functionDescrGet.
//
// CPython: Objects/funcobject.c:1057 func_descr_get
func methodFuncDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	if owner == nil {
		return descr, nil
	}
	return NewBoundMethod(descr, owner), nil
}
