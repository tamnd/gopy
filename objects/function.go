// Function is the gopy port of PyFunctionObject. It bundles the
// code object with the captured execution environment plus the
// per-function slots that descriptor protocol and introspection
// consume: __defaults__, __kwdefaults__, __closure__,
// __annotations__, __dict__, __module__, __doc__, __builtins__,
// __type_params__.
//
// CPython: Include/cpython/funcobject.h:36 PyFunctionObject

package objects

import (
	"errors"
	"fmt"
)

// CoHasDocstring is the CO_HAS_DOCSTRING flag bit. When the bit is
// set on Code.Flags, co_consts[0] holds the function's docstring;
// NewFunctionWithQualName picks it up from there.
//
// CPython: Include/cpython/code.h CO_HAS_DOCSTRING
const CoHasDocstring = 0x4000000

// Function represents a Python-defined function. The runtime call
// arm pushes a new frame off Code, threads Globals into the frame's
// f_globals, and dispatches.
//
// CPython: Include/cpython/funcobject.h:36 PyFunctionObject
type Function struct {
	Header
	Name        string
	Qualname    string
	Code        *Code
	Globals     Object // __globals__, typically *Dict
	Builtins    Object // __builtins__, loaded from globals['__builtins__']
	Module      Object // __module__, from globals['__name__'], may be nil
	Doc         Object // __doc__, from co_consts[0] when CoHasDocstring, else None
	Defaults    *Tuple // __defaults__, may be nil
	KwDefaults  *Dict  // __kwdefaults__, may be nil
	Closure     *Tuple // __closure__, tuple of cells, may be nil
	Annotations *Dict  // __annotations__, may be nil
	Annotate    Object // __annotate__, callable that fills annotations on demand
	Typeparams  *Tuple // __type_params__, may be nil
	Dict        *Dict  // __dict__, may be nil

	// Version is the specializer key. Reset to zero whenever Code,
	// Defaults, KwDefaults, Closure, or Annotations change. Specialized
	// CALL guards check this against an inline cache; gopy doesn't run
	// the specializer yet, but the field tracks the same invariants so
	// callers can pin it later.
	//
	// CPython: Include/cpython/funcobject.h:55 func_version
	Version uint32
}

// FunctionType is the type singleton for Python-defined functions.
//
// CPython: Objects/funcobject.c:1232 PyFunction_Type
var FunctionType = NewType("function", []*Type{objectType})

func init() {
	FunctionType.Repr = functionRepr
	FunctionType.Str = functionRepr
	// Functions are descriptors: fetched off an instance they bind
	// into a method that prepends self. Fetched off the class they
	// pass through unchanged.
	//
	// CPython: Objects/funcobject.c:1057 func_descr_get
	FunctionType.DescrGet = functionDescrGet
	FunctionType.Getattro = funcGetAttr
	FunctionType.Setattro = funcSetAttr
	// func_traverse visits every reachable Object slot so the cycle
	// collector can walk the function's reference graph. CPython
	// visits the PyObject form of every PyFunctionObject slot; we
	// skip Name and Qualname here because gopy stores them as Go
	// strings, not PyUnicode.
	//
	// CPython: Objects/funcobject.c:1093 func_traverse
	FunctionType.TpTraverse = functionTraverse
	// Identity hash. Functions inherit tp_hash from object in CPython
	// and are routinely stuffed into sets (e.g. enum's _find_new_).
	//
	// CPython: Objects/funcobject.c:1232 PyFunction_Type (no tp_hash
	// override, so object_hash is inherited)
	FunctionType.Hash = identityHash
	registerFunctionGetSets()
	// function.__new__: clinic-generated wrapper that accepts (code,
	// globals, name=None, argdefs=None, closure=None, kwdefaults=None)
	// and validates each argument before assembling the function.
	//
	// CPython: Objects/funcobject.c:1030 func_new_impl
	FunctionType.TpNew = funcTpNew
	// FunctionType.Call is wired by the vm package on init since the
	// call needs to push a frame and drive Eval; doing that from
	// objects would be a circular import.
	addDescriptorSlotWrappers(FunctionType)
}

// functionTraverse mirrors func_traverse. Visit order matches CPython
// so any future GC consumer sees the same reachability profile.
//
// CPython: Objects/funcobject.c:1093 func_traverse
func functionTraverse(o Object, visit Visitor) error {
	f := o.(*Function)
	visits := [...]Object{
		codeAsObject(f.Code),
		f.Globals,
		f.Module,
		tupleAsObject(f.Defaults),
		dictAsObject(f.KwDefaults),
		f.Doc,
		dictAsObject(f.Dict),
		tupleAsObject(f.Closure),
		dictAsObject(f.Annotations),
		f.Annotate,
		tupleAsObject(f.Typeparams),
		f.Builtins,
	}
	for _, v := range visits {
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	return nil
}

// codeAsObject promotes *Code to Object only when non-nil; the visit
// loop relies on a nil result to skip missing slots without growing a
// per-field guard.
func codeAsObject(c *Code) Object {
	if c == nil {
		return nil
	}
	return c
}

func tupleAsObject(t *Tuple) Object {
	if t == nil {
		return nil
	}
	return t
}

func dictAsObject(d *Dict) Object {
	if d == nil {
		return nil
	}
	return d
}

// registerFunctionGetSets exposes the introspection attributes
// CPython publishes on function objects. Mirrors func_getsetlist
// plus the func_memberlist surface (__doc__, __module__, __globals__,
// __builtins__, __closure__).
//
// CPython: Objects/funcobject.c:987 func_getsetlist
// CPython: Objects/funcobject.c:633 func_memberlist
func registerFunctionGetSets() {
	registerFunctionIdentityGetSets()
	registerFunctionReadOnlyGetSets()
	registerFunctionMutableGetSets()
	registerFunctionAnnotateGetSets()
	registerFunctionTypeParamsGetSet()
	registerFunctionDictGetSets()
}

// noneIfNil returns None when o is nil, otherwise o. Used by the
// function getters that map an absent Go field to Python's None.
func noneIfNil(o Object) Object {
	if o == nil {
		return None()
	}
	return o
}

// registerFunctionIdentityGetSets installs __doc__, __name__,
// __qualname__, __module__: the small mutable identity surface.
func registerFunctionIdentityGetSets() {
	SetTypeDescr(FunctionType, "__doc__", NewGetSetDescr("__doc__",
		func(o Object) (Object, error) { return noneIfNil(o.(*Function).Doc), nil },
		func(o Object, v Object) error { o.(*Function).Doc = v; return nil }))
	SetTypeDescr(FunctionType, "__name__", NewGetSetDescr("__name__",
		func(o Object) (Object, error) { return NewStr(o.(*Function).Name), nil },
		func(o Object, v Object) error {
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __name__ must be set to a string object")
			}
			o.(*Function).Name = s.Value()
			return nil
		}))
	SetTypeDescr(FunctionType, "__qualname__", NewGetSetDescr("__qualname__",
		func(o Object) (Object, error) { return NewStr(o.(*Function).Qualname), nil },
		func(o Object, v Object) error {
			s, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: __qualname__ must be set to a string object")
			}
			o.(*Function).Qualname = s.Value()
			return nil
		}))
	SetTypeDescr(FunctionType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) { return noneIfNil(o.(*Function).Module), nil },
		func(o Object, v Object) error { o.(*Function).Module = v; return nil }))
}

// registerFunctionReadOnlyGetSets installs the read-only surface from
// func_memberlist: __closure__, __globals__, __builtins__. CPython
// keeps these strictly read-only on the Python side; assignment
// raises AttributeError.
//
// CPython: Objects/funcobject.c:633 func_memberlist
func registerFunctionReadOnlyGetSets() {
	SetTypeDescr(FunctionType, "__closure__", NewGetSetDescr("__closure__",
		func(o Object) (Object, error) {
			f := o.(*Function)
			if f.Closure == nil {
				return None(), nil
			}
			return f.Closure, nil
		},
		nil))
	SetTypeDescr(FunctionType, "__globals__", NewGetSetDescr("__globals__",
		func(o Object) (Object, error) { return noneIfNil(o.(*Function).Globals), nil },
		nil))
	SetTypeDescr(FunctionType, "__builtins__", NewGetSetDescr("__builtins__",
		func(o Object) (Object, error) { return noneIfNil(o.(*Function).Builtins), nil },
		nil))
}

// registerFunctionMutableGetSets installs the mutable surface from
// func_getsetlist: __code__, __defaults__, __kwdefaults__,
// __annotations__. Each setter mirrors the CPython type check and
// clears Version so the specializer's inline caches invalidate.
//
// CPython: Objects/funcobject.c:660 func_set_code
// CPython: Objects/funcobject.c:766 func_set_defaults
// CPython: Objects/funcobject.c:809 func_set_kwdefaults
// CPython: Objects/funcobject.c:916 function___annotations___set_impl
func registerFunctionMutableGetSets() {
	SetTypeDescr(FunctionType, "__code__", NewGetSetDescr("__code__",
		func(o Object) (Object, error) {
			c := o.(*Function).Code
			if c == nil {
				return None(), nil
			}
			return c, nil
		},
		funcSetCodeAttr))
	SetTypeDescr(FunctionType, "__defaults__", NewGetSetDescr("__defaults__",
		func(o Object) (Object, error) {
			f := o.(*Function)
			if f.Defaults == nil {
				return None(), nil
			}
			return f.Defaults, nil
		},
		funcSetDefaultsAttr))
	SetTypeDescr(FunctionType, "__kwdefaults__", NewGetSetDescr("__kwdefaults__",
		func(o Object) (Object, error) {
			f := o.(*Function)
			if f.KwDefaults == nil {
				return None(), nil
			}
			return f.KwDefaults, nil
		},
		funcSetKwDefaultsAttr))
	SetTypeDescr(FunctionType, "__annotations__", NewGetSetDescr("__annotations__",
		funcGetAnnotationsAttr,
		funcSetAnnotationsAttr))
}

// registerFunctionAnnotateGetSets installs __annotate__: the callable
// that lazily builds the function's annotation dict. Setting it to a
// callable clears any cached __annotations__ so the next read
// re-runs the annotator.
//
// CPython: Objects/funcobject.c:846 function___annotate___get_impl
// CPython: Objects/funcobject.c:862 function___annotate___set_impl
func registerFunctionAnnotateGetSets() {
	SetTypeDescr(FunctionType, "__annotate__", NewGetSetDescr("__annotate__",
		func(o Object) (Object, error) { return noneIfNil(o.(*Function).Annotate), nil },
		funcSetAnnotateAttr))
}

// funcSetCodeAttr is the __code__ setter. Mirrors func_set_code: the
// new value must be a code object, the free-var count must match the
// captured closure size, and Version resets so the specializer
// invalidates anything keyed on the old code identity.
//
// CPython: Objects/funcobject.c:660 func_set_code
func funcSetCodeAttr(o Object, v Object) error {
	if v == nil {
		return fmt.Errorf("TypeError: __code__ must be set to a code object")
	}
	c, ok := v.(*Code)
	if !ok {
		return fmt.Errorf("TypeError: __code__ must be set to a code object")
	}
	return o.(*Function).SetCode(c)
}

// funcSetDefaultsAttr is the __defaults__ setter. None clears the
// tuple; anything other than None or tuple raises TypeError.
//
// CPython: Objects/funcobject.c:766 func_set_defaults
func funcSetDefaultsAttr(o Object, v Object) error {
	f := o.(*Function)
	if v == nil || v == None() {
		f.SetDefaults(nil)
		return nil
	}
	t, ok := v.(*Tuple)
	if !ok {
		return fmt.Errorf("TypeError: __defaults__ must be set to a tuple object")
	}
	f.SetDefaults(t)
	return nil
}

// funcSetKwDefaultsAttr is the __kwdefaults__ setter. None clears the
// dict; anything other than None or dict raises TypeError.
//
// CPython: Objects/funcobject.c:809 func_set_kwdefaults
func funcSetKwDefaultsAttr(o Object, v Object) error {
	f := o.(*Function)
	if v == nil || v == None() {
		f.SetKwDefaults(nil)
		return nil
	}
	d, ok := v.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: __kwdefaults__ must be set to a dict object")
	}
	f.SetKwDefaults(d)
	return nil
}

// funcGetAnnotationsAttr lazily materializes the annotations dict.
// If neither annotations nor a callable __annotate__ is available, a
// fresh empty dict is created and stored so subsequent reads see the
// same identity (matching CPython's `func_annotations = PyDict_New()`
// shortcut in function___annotations___get_impl).
//
// CPython: Objects/funcobject.c:895 function___annotations___get_impl
func funcGetAnnotationsAttr(o Object) (Object, error) {
	f := o.(*Function)
	if f.Annotations == nil && f.Annotate == nil {
		f.Annotations = NewDict()
	}
	d, err := f.GetAnnotations()
	if err != nil {
		return nil, err
	}
	if d == nil {
		return NewDict(), nil
	}
	return d, nil
}

// funcSetAnnotationsAttr is the __annotations__ setter. None clears
// the dict; anything other than None or dict raises TypeError.
// Assigning also drops any pending Annotate so the new value is
// authoritative.
//
// CPython: Objects/funcobject.c:916 function___annotations___set_impl
func funcSetAnnotationsAttr(o Object, v Object) error {
	f := o.(*Function)
	if v == nil || v == None() {
		f.SetAnnotations(nil)
		return nil
	}
	d, ok := v.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: __annotations__ must be set to a dict object")
	}
	f.SetAnnotations(d)
	return nil
}

// funcSetAnnotateAttr is the __annotate__ setter. Deletion (value
// nil) raises TypeError; None clears the annotator; a callable swaps
// it in and clears any cached __annotations__ so the next read
// re-runs the annotator.
//
// CPython: Objects/funcobject.c:862 function___annotate___set_impl
func funcSetAnnotateAttr(o Object, v Object) error {
	f := o.(*Function)
	if v == nil {
		return fmt.Errorf("TypeError: __annotate__ cannot be deleted")
	}
	if v == None() {
		f.Annotate = None()
		return nil
	}
	if !isCallable(v) {
		return fmt.Errorf("TypeError: __annotate__ must be callable or None")
	}
	f.Annotate = v
	f.Annotations = nil
	return nil
}

// isCallable mirrors PyCallable_Check: an object is callable when its
// type defines tp_call or tp_vectorcall. Kept narrow on purpose so
// the function-setter validation matches CPython.
//
// CPython: Objects/call.c:240 PyCallable_Check
func isCallable(o Object) bool {
	if o == nil {
		return false
	}
	t := o.Type()
	return t != nil && (t.Call != nil || t.Vectorcall != nil)
}

// funcTpNew is function.__new__. Mirrors func_new_impl:
// (code, globals, name=None, defaults=None, closure=None,
// kwdefaults=None). Each argument has a typed check; the closure
// must match the code's free-var count exactly and every element must
// be a cell.
//
// CPython: Objects/funcobject.c:1030 func_new_impl
func funcTpNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	pos := append([]Object(nil), args...)
	if v, ok := kwargs["code"]; ok && len(pos) < 1 {
		pos = append(pos, v)
	}
	if v, ok := kwargs["globals"]; ok && len(pos) < 2 {
		pos = append(pos, v)
	}
	for _, name := range []string{"name", "argdefs", "closure", "kwdefaults"} {
		if v, ok := kwargs[name]; ok {
			pos = append(pos, v)
		}
	}
	for len(pos) < 6 {
		pos = append(pos, None())
	}
	if len(pos) < 2 || len(pos) > 6 {
		return nil, fmt.Errorf("TypeError: function expected 2 to 6 arguments, got %d", len(args))
	}
	code, ok := pos[0].(*Code)
	if !ok {
		return nil, fmt.Errorf("TypeError: arg 1 (code) must be code, not %s", pos[0].Type().Name)
	}
	if _, ok := pos[1].(*Dict); !ok {
		return nil, fmt.Errorf("TypeError: arg 2 (globals) must be dict, not %s", pos[1].Type().Name)
	}
	name := pos[2]
	defaults := pos[3]
	closure := pos[4]
	kwdefaults := pos[5]

	if name != None() {
		if _, ok := name.(*Unicode); !ok {
			return nil, fmt.Errorf("TypeError: arg 3 (name) must be None or string")
		}
	}
	if defaults != None() {
		if _, ok := defaults.(*Tuple); !ok {
			return nil, fmt.Errorf("TypeError: arg 4 (defaults) must be None or tuple")
		}
	}
	if _, ok := closure.(*Tuple); !ok {
		if len(code.Freevars) > 0 && closure == None() {
			return nil, fmt.Errorf("TypeError: arg 5 (closure) must be tuple")
		}
		if closure != None() {
			return nil, fmt.Errorf("TypeError: arg 5 (closure) must be None or tuple")
		}
	}
	if kwdefaults != None() {
		if _, ok := kwdefaults.(*Dict); !ok {
			return nil, fmt.Errorf("TypeError: arg 6 (kwdefaults) must be None or dict")
		}
	}

	nclosure := 0
	if t, ok := closure.(*Tuple); ok {
		nclosure = t.Len()
	}
	if len(code.Freevars) != nclosure {
		return nil, fmt.Errorf("ValueError: %s requires closure of length %d, not %d",
			code.Name, len(code.Freevars), nclosure)
	}
	if t, ok := closure.(*Tuple); ok {
		for i := 0; i < t.Len(); i++ {
			if _, isCell := t.Item(i).(*Cell); !isCell {
				return nil, fmt.Errorf("TypeError: arg 5 (closure) expected cell, found %s",
					t.Item(i).Type().Name)
			}
		}
	}

	f, err := newFunction(code.Name, code, pos[1], "")
	if err != nil {
		return nil, err
	}
	if n, ok := name.(*Unicode); ok {
		f.Name = n.Value()
	}
	if t, ok := defaults.(*Tuple); ok {
		f.Defaults = t
	}
	if t, ok := closure.(*Tuple); ok {
		f.Closure = t
	}
	if d, ok := kwdefaults.(*Dict); ok {
		f.KwDefaults = d
	}
	return f, nil
}

// registerFunctionTypeParamsGetSet installs __type_params__, which is
// the only field with a typed setter (must be tuple) that did not fit
// the identity bucket.
func registerFunctionTypeParamsGetSet() {
	SetTypeDescr(FunctionType, "__type_params__", NewGetSetDescr("__type_params__",
		func(o Object) (Object, error) {
			t := o.(*Function).Typeparams
			if t == nil {
				return NewTuple(nil), nil
			}
			return t, nil
		},
		func(o Object, v Object) error {
			f := o.(*Function)
			if v == nil {
				f.Typeparams = nil
				return nil
			}
			t, ok := v.(*Tuple)
			if !ok {
				return fmt.Errorf("TypeError: __type_params__ must be a tuple, not '%s'", v.Type().Name)
			}
			f.Typeparams = t
			return nil
		}))
}

// registerFunctionDictGetSets installs __isabstractmethod__ and
// __dict__, both of which read through f.Dict so decorators like
// abstractmethod can stamp attributes without a dedicated field.
//
// CPython: Objects/funcobject.c:755 func_get_dict / func_set_dict
// CPython: Objects/funcobject.c:805 func_get_isabstractmethod
// CPython: Objects/funcobject.c:823 func_set_isabstractmethod
func registerFunctionDictGetSets() {
	SetTypeDescr(FunctionType, "__isabstractmethod__", NewGetSetDescr("__isabstractmethod__",
		funcGetIsAbstractMethod,
		funcSetIsAbstractMethod))
	SetTypeDescr(FunctionType, "__dict__", NewGetSetDescr("__dict__",
		funcGetDict,
		funcSetDict))
}

func funcGetIsAbstractMethod(o Object) (Object, error) {
	f := o.(*Function)
	if f.Dict == nil {
		return False(), nil
	}
	v, err := f.Dict.GetItem(NewStr("__isabstractmethod__"))
	if err != nil || v == nil {
		//nolint:nilerr // missing key reads as False, per func_get_isabstractmethod
		return False(), nil
	}
	return v, nil
}

func funcSetIsAbstractMethod(o Object, v Object) error {
	f := o.(*Function)
	if v == nil {
		if f.Dict == nil {
			return nil
		}
		return f.Dict.DelItem(NewStr("__isabstractmethod__"))
	}
	if f.Dict == nil {
		f.Dict = NewDict()
	}
	return f.Dict.SetItem(NewStr("__isabstractmethod__"), v)
}

func funcGetDict(o Object) (Object, error) {
	f := o.(*Function)
	if f.Dict == nil {
		f.Dict = NewDict()
	}
	return f.Dict, nil
}

func funcSetDict(o Object, v Object) error {
	f := o.(*Function)
	if v == nil {
		f.Dict = nil
		return nil
	}
	d, ok := v.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: __dict__ must be set to a dict object")
	}
	f.Dict = d
	return nil
}

// funcGetAttr is FunctionType.Getattro. It resolves getset descriptors
// first, then reads from the function's __dict__ for arbitrary
// per-function attributes set by decorators.
//
// CPython: Objects/funcobject.c:687 func_getattro
func funcGetAttr(o Object, name Object) (Object, error) {
	if name == nil || name.Type() != strType {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := o.Type()
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil {
			return dt.DescrGet(descr, o, tp)
		}
		return descr, nil
	}
	fn := o.(*Function)
	if fn.Dict != nil {
		v, err := fn.Dict.GetItem(name)
		if err == nil && v != nil {
			return v, nil
		}
	}
	return nil, fmt.Errorf("AttributeError: 'function' object has no attribute '%s'", attrNameStr(name))
}

// funcSetAttr is FunctionType.Setattro. It dispatches to getset
// descriptor setters first, then falls back to the function's __dict__
// for arbitrary decorator-assigned attributes.
//
// CPython: Objects/funcobject.c:714 func_setattro
func funcSetAttr(o Object, name Object, value Object) error {
	if name == nil || name.Type() != strType {
		return fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := o.Type()
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	if descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	fn := o.(*Function)
	if value == nil {
		if fn.Dict == nil {
			return fmt.Errorf("AttributeError: 'function' object has no attribute '%s'", attrNameStr(name))
		}
		return fn.Dict.DelItem(name)
	}
	if fn.Dict == nil {
		fn.Dict = NewDict()
	}
	return fn.Dict.SetItem(name, value)
}

// functionDescrGet implements the function descriptor protocol:
// `instance.method` returns a BoundMethod, `Class.method` returns the
// raw function. owner==nil signals access through the class itself
// (e.g. `Class.method`).
//
// CPython: Objects/funcobject.c:1057 func_descr_get
func functionDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	if owner == nil {
		return descr, nil
	}
	return NewBoundMethod(descr, owner), nil
}

// functionRepr matches CPython's `<function QUALNAME at 0xPTR>`. The
// pointer suffix is what `inspect.getsource` and traceback formatting
// key off when two functions share a qualname (lambdas, dynamically
// generated wrappers).
//
// CPython: Objects/funcobject.c:920 func_repr
func functionRepr(o Object) (string, error) {
	f := o.(*Function)
	name := f.Qualname
	if name == "" {
		name = f.Name
	}
	return fmt.Sprintf("<function %s at %p>", name, f), nil
}

// NewFunction is the no-qualname form of NewFunctionWithQualName.
//
// CPython: Objects/funcobject.c:407 PyFunction_New
func NewFunction(name string, code *Code, globals Object) *Function {
	f, _ := newFunction(name, code, globals, "")
	return f
}

// NewFunctionWithQualName builds a function from a code object and
// captured globals, picking the docstring out of co_consts[0] when
// CoHasDocstring is set, the module name from globals['__name__'],
// and the builtins dict from globals['__builtins__']. qualname==""
// falls back to code.Qualname.
//
// CPython: Objects/funcobject.c:148 PyFunction_NewWithQualName
func NewFunctionWithQualName(code *Code, globals Object, qualname string) (*Function, error) {
	if code == nil {
		return nil, errors.New("SystemError: NewFunctionWithQualName: nil code")
	}
	return newFunction(code.Name, code, globals, qualname)
}

func newFunction(name string, code *Code, globals Object, qualname string) (*Function, error) {
	f := &Function{Name: name, Code: code, Globals: globals}
	switch {
	case qualname != "":
		f.Qualname = qualname
	case code != nil:
		f.Qualname = code.Qualname
		if f.Qualname == "" {
			f.Qualname = name
		}
	default:
		f.Qualname = name
	}
	f.Doc = functionDocFromCode(code)
	if d, ok := globals.(*Dict); ok {
		if mod, err := d.GetItem(NewStr("__name__")); err == nil {
			f.Module = mod
		}
		if b, err := d.GetItem(NewStr("__builtins__")); err == nil {
			f.Builtins = b
		}
	}
	f.init(FunctionType)
	return f, nil
}

// functionDocFromCode pulls the docstring out of co_consts[0] when
// the code object has CoHasDocstring set. Mirrors the eager doc
// lookup PyFunction_NewWithQualName performs.
func functionDocFromCode(code *Code) Object {
	if code == nil {
		return None()
	}
	if code.Flags&CoHasDocstring == 0 {
		return None()
	}
	if len(code.Consts) == 0 {
		return None()
	}
	if doc, ok := code.Consts[0].(string); ok {
		return NewStr(doc)
	}
	if doc, ok := code.Consts[0].(Object); ok {
		if _, isStr := doc.(*Unicode); isStr {
			return doc
		}
	}
	return None()
}

// GetVersionForCurrentState returns func_version. Mirrors CPython's
// helper of the same name; the Tier-2 globals folder stamps the
// returned value into _CHECK_FUNCTION uops so the dispatch loop can
// short-circuit when the function shape still matches.
//
// CPython: Objects/funcobject.c:401 _PyFunction_GetVersionForCurrentState
func (f *Function) GetVersionForCurrentState() uint32 { return f.Version }

// SetCode binds a new code object. Rejects code with a free-var
// count that doesn't match the captured closure size, mirroring
// func_set_code's nclosure==nfreevars guard.
//
// CPython: Objects/funcobject.c:661 func_set_code
func (f *Function) SetCode(code *Code) error {
	if code == nil {
		return fmt.Errorf("TypeError: __code__ must be set to a code object")
	}
	nclosure := 0
	if f.Closure != nil {
		nclosure = f.Closure.Len()
	}
	nfree := len(code.Freevars)
	if nclosure != nfree {
		return fmt.Errorf("ValueError: %s() requires a code object with %d free vars, not %d", f.Name, nclosure, nfree)
	}
	f.Code = code
	f.Version = 0
	return nil
}

// SetDefaults sets __defaults__ to t (nil clears it) and zeros
// Version.
//
// CPython: Objects/funcobject.c:453 PyFunction_SetDefaults
func (f *Function) SetDefaults(t *Tuple) {
	f.Defaults = t
	f.Version = 0
}

// SetKwDefaults sets __kwdefaults__ to d (nil clears it) and zeros
// Version.
//
// CPython: Objects/funcobject.c:494 PyFunction_SetKwDefaults
func (f *Function) SetKwDefaults(d *Dict) {
	f.KwDefaults = d
	f.Version = 0
}

// SetClosure sets __closure__ to t (nil clears it) and zeros
// Version.
//
// CPython: Objects/funcobject.c:528 PyFunction_SetClosure
func (f *Function) SetClosure(t *Tuple) {
	f.Closure = t
	f.Version = 0
}

// SetAnnotations sets __annotations__ to d (nil clears it) and
// drops any pending Annotate callable. Does not zero Version since
// CPython treats annotations as a non-critical attribute for the
// specializer.
//
// CPython: Objects/funcobject.c:607 PyFunction_SetAnnotations
func (f *Function) SetAnnotations(d *Dict) {
	f.Annotations = d
	f.Annotate = nil
}

// GetAnnotations returns __annotations__, lazily materializing the
// dict from the Annotate callable on first access. Mirrors
// func_get_annotation_dict, which calls __annotate__(1) and caches
// the result.
//
// CPython: Objects/funcobject.c:551 func_get_annotation_dict
func (f *Function) GetAnnotations() (*Dict, error) {
	if f.Annotations != nil {
		return f.Annotations, nil
	}
	if f.Annotate == nil {
		return nil, nil
	}
	out, err := Call(f.Annotate, NewTuple([]Object{NewInt(1)}), nil)
	if err != nil {
		return nil, err
	}
	d, ok := out.(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: __annotate__ returned non-dict of type '%s'", out.Type().Name)
	}
	f.Annotations = d
	return d, nil
}
