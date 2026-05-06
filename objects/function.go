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
	// FunctionType.Call is wired by the vm package on init since the
	// call needs to push a frame and drive Eval; doing that from
	// objects would be a circular import.
}

func functionRepr(o Object) (string, error) {
	f := o.(*Function)
	return "<function " + f.Name + ">", nil
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
	if qualname != "" {
		f.Qualname = qualname
	} else if code != nil {
		f.Qualname = code.Qualname
		if f.Qualname == "" {
			f.Qualname = name
		}
	} else {
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
		if _, isStr := doc.(*strStub); isStr {
			return doc
		}
	}
	return None()
}

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
