// Port of the reflection panel from Python/bltinmodule.c: type,
// isinstance, issubclass, callable, id, hash, repr, str.

package builtins

import (
	"fmt"
	"unsafe"

	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/objects"
)

// TypeOf ports builtin_type. Single-argument form returns Py_TYPE(obj);
// the three-argument form (name, bases, dict) builds a class and lands
// once the class machinery is fully ported. v0.7 ships only the
// reflection branch.
//
// CPython: Python/bltinmodule.c:1183 builtin_type
func TypeOf(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: type() takes 1 or 3 arguments")
	}
	return args[0].Type(), nil
}

// IsInstance ports builtin_isinstance. The fast path handles the
// type-exact case; otherwise the call falls through to the metaclass's
// __instancecheck__ (so abc.ABCMeta's virtual-subclass registry takes
// effect for collections.abc and the like).
//
// CPython: Objects/abstract.c:2632 object_recursive_isinstance
func IsInstance(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: isinstance expected 2 arguments, got %d", len(args))
	}
	ok, err := objects.ObjectIsInstance(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(ok), nil
}

// IsSubclass ports builtin_issubclass. As with IsInstance, the call
// routes through __subclasscheck__ when the class's metaclass overrides
// it; otherwise it falls back to the MRO subtype test.
//
// CPython: Objects/abstract.c:2742 object_issubclass
func IsSubclass(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: issubclass expected 2 arguments, got %d", len(args))
	}
	ok2, err := objects.ObjectIsSubclassObj(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(ok2), nil
}

// Callable ports builtin_callable. Returns True iff the type has a
// Call or Vectorcall slot, matching the PyCallable_Check shape.
//
// CPython: Python/bltinmodule.c:459 builtin_callable
func Callable(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: callable() takes exactly one argument (%d given)", len(args))
	}
	t := args[0].Type()
	if t.Call != nil || t.Vectorcall != nil {
		return objects.True(), nil
	}
	return objects.False(), nil
}

// Id ports builtin_id. Returns the object's pointer cast to int. Go
// pinning here is best-effort; the Go runtime may move objects, so
// repeated calls on the same value can return different ints. The
// ordering invariant CPython guarantees (id is stable for the
// object's lifetime) holds within a single GC cycle.
//
// CPython: Python/bltinmodule.c:1335 builtin_id_impl
func ID(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: id() takes exactly one argument (%d given)", len(args))
	}
	addr := uintptr(unsafe.Pointer(args[0].Hdr()))
	return objects.NewInt(int64(addr)), nil
}

// Hash ports builtin_hash. Routes through PyObject_Hash; types with
// no Hash slot raise TypeError (mirrors the PyObject_Hash error).
//
// CPython: Python/bltinmodule.c:1775 builtin_hash
func Hash(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: hash() takes exactly one argument (%d given)", len(args))
	}
	h, err := objects.Hash(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: unhashable type: '%s'", args[0].Type().Name)
	}
	return objects.NewInt(h), nil
}

// Repr ports builtin_repr. Routes through PyObject_Repr.
//
// CPython: Python/bltinmodule.c:2581 builtin_repr
func Repr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: repr() takes exactly one argument (%d given)", len(args))
	}
	return objects.ReprObject(args[0])
}

// StrOf ports the str() builtin factory. Follows CPython's unicode_new.
// str() or str(object) returns PyObject_Str of the argument.
// str(object, encoding) and str(object, encoding, errors) decode a bytes-like
// buffer through the codec registry. The 'object' positional argument may also
// be passed by keyword. When only 'errors' is given and the object is
// bytes-like, utf-8 is used as the encoding.
//
// CPython: Objects/unicodeobject.c:14112 unicode_new
func StrOf(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// Validate keyword argument names.
	for k := range kwargs {
		if k != "object" && k != "encoding" && k != "errors" {
			return nil, fmt.Errorf("TypeError: str() got an unexpected keyword argument '%s'", k)
		}
	}

	// Check argument counts. Pure positional overflow uses a different message
	// than positional+keyword overflow (both patterns from Argument Clinic).
	// CPython: Objects/unicodeobject.c:14112 unicode_new
	if len(args) > 3 && len(kwargs) == 0 {
		return nil, fmt.Errorf("TypeError: str expected at most 3 arguments, got %d", len(args))
	}
	if len(args)+len(kwargs) > 3 && len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: str() takes at most 3 arguments (%d given)", len(args)+len(kwargs))
	}

	// Extract 'object' from positional or keyword args.
	var objArg objects.Object
	hasObj := false
	if len(args) >= 1 {
		objArg = args[0]
		hasObj = true
		if _, ok := kwargs["object"]; ok {
			return nil, fmt.Errorf("TypeError: argument for str() given by name ('object') and position (1)")
		}
	} else if v, ok := kwargs["object"]; ok {
		objArg = v
		hasObj = true
	}

	// Extract encoding and errors.
	var encodingArg, errorsArg objects.Object
	if len(args) >= 2 {
		encodingArg = args[1]
		if _, ok := kwargs["encoding"]; ok {
			return nil, fmt.Errorf("TypeError: argument for str() given by name ('encoding') and position (2)")
		}
	} else if v, ok := kwargs["encoding"]; ok {
		encodingArg = v
	}
	if len(args) >= 3 {
		errorsArg = args[2]
		if _, ok := kwargs["errors"]; ok {
			return nil, fmt.Errorf("TypeError: argument for str() given by name ('errors') and position (3)")
		}
	} else if v, ok := kwargs["errors"]; ok {
		errorsArg = v
	}
	// str() with no object and no encoding: return ''.
	// CPython: Objects/unicodeobject.c:14150 unicode_new_impl (x==NULL early return)
	if !hasObj && encodingArg == nil {
		return objects.NewStr(""), nil
	}

	// If encoding or errors is given (or object is bytes-like with errors),
	// enter decoding mode.
	hasEncoding := encodingArg != nil
	hasErrors := errorsArg != nil

	// Validate encoding and errors types early (CPython checks these before
	// deciding the call mode, so type errors always take priority).
	encoding := "utf-8"
	errs := "strict"
	if encodingArg != nil {
		s, ok := encodingArg.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: str() argument 'encoding' must be str, not %s", encodingArg.Type().Name)
		}
		encoding = s.Value()
	}
	if errorsArg != nil {
		s, ok := errorsArg.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: str() argument 'errors' must be str, not %s", errorsArg.Type().Name)
		}
		errs = s.Value()
	}

	// Any of encoding, errors, or (errors without encoding but with obj) triggers decode mode.
	// CPython: Objects/unicodeobject.c:14150 (encoding != NULL || errors != NULL path)
	decodeMode := hasEncoding || hasErrors

	if decodeMode {
		if !hasObj {
			return objects.NewStr(""), nil
		}
		// Check for str object in decode mode.
		if _, ok := objArg.(*objects.Unicode); ok {
			return nil, fmt.Errorf("TypeError: decoding str is not supported")
		}
		buf, ok := bytesLike(objArg)
		if !ok {
			return nil, fmt.Errorf("TypeError: decoding to str: need a bytes-like object, %s found", objArg.Type().Name)
		}
		out, _, derr := codecs.Decode(buf, encoding, errs)
		if derr != nil {
			return nil, derr
		}
		return objects.NewStr(out), nil
	}

	// errors given without encoding and non-bytes object: return '' if no obj, else str(obj).
	if !hasObj {
		return objects.NewStr(""), nil
	}
	return objects.StrObject(objArg)
}

func bytesLike(o objects.Object) ([]byte, bool) {
	switch x := o.(type) {
	case *objects.Bytes:
		return x.Bytes(), true
	case *objects.ByteArray:
		return x.Bytes(), true
	case *objects.MemoryView:
		return x.Bytes(), true
	}
	return nil, false
}
