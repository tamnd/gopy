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

// IsInstance ports builtin_isinstance. Tuple second argument runs the
// recursive check; otherwise the test is sub.MRO contains super.
// __instancecheck__ landings come with the descriptor port.
//
// CPython: Objects/abstract.c:2632 object_recursive_isinstance
func IsInstance(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: isinstance expected 2 arguments, got %d", len(args))
	}
	ok, err := isInstanceCheck(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(ok), nil
}

func isInstanceCheck(inst, cls objects.Object) (bool, error) {
	if tup, ok := cls.(*objects.Tuple); ok {
		for i := 0; i < tup.Len(); i++ {
			ok, err := isInstanceCheck(inst, tup.Item(i))
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	t, ok := cls.(*objects.Type)
	if !ok {
		return false, fmt.Errorf("TypeError: isinstance() arg 2 must be a type, a tuple of types, or a union")
	}
	return objects.IsSubtype(inst.Type(), t), nil
}

// IsSubclass ports builtin_issubclass. Both arguments must be types
// (or the second a tuple of types); the test runs IsSubtype.
//
// CPython: Objects/abstract.c:2742 object_issubclass
func IsSubclass(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: issubclass expected 2 arguments, got %d", len(args))
	}
	sub, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: issubclass() arg 1 must be a class")
	}
	ok2, err := isSubclassCheck(sub, args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(ok2), nil
}

func isSubclassCheck(sub *objects.Type, cls objects.Object) (bool, error) {
	if tup, ok := cls.(*objects.Tuple); ok {
		for i := 0; i < tup.Len(); i++ {
			ok, err := isSubclassCheck(sub, tup.Item(i))
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	t, ok := cls.(*objects.Type)
	if !ok {
		return false, fmt.Errorf("TypeError: issubclass() arg 2 must be a class, a tuple of classes, or a union")
	}
	return objects.IsSubtype(sub, t), nil
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
	s, err := objects.Repr(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

// StrOf ports the str(obj) factory shape. Single-argument form
// returns PyObject_Str; multi-argument form str(buffer, encoding,
// errors) decodes a bytes-like buffer through the codec registry,
// the way Objects/unicodeobject.c routes bytes inputs.
//
// CPython: Objects/unicodeobject.c:14112 unicode_new
func StrOf(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 && len(kwargs) == 0 {
		return objects.NewStr(""), nil
	}
	if len(args) > 1 || len(kwargs) > 0 {
		buf, encoding, errs, err := strOfDecodeArgs(args, kwargs)
		if err != nil {
			return nil, err
		}
		out, _, derr := codecs.Decode(buf, encoding, errs)
		if derr != nil {
			return nil, derr
		}
		return objects.NewStr(out), nil
	}
	s, err := objects.Str(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

// strOfDecodeArgs pulls the (object, encoding, errors) triple out of
// str()'s call signature. encoding defaults to "utf-8", errors to
// "strict", matching Objects/unicodeobject.c:unicode_new.
func strOfDecodeArgs(args []objects.Object, kwargs map[string]objects.Object) ([]byte, string, string, error) {
	if len(args) == 0 {
		return nil, "", "", fmt.Errorf("TypeError: str() missing positional argument")
	}
	encoding := "utf-8"
	errs := "strict"
	if len(args) >= 2 {
		s, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, "", "", fmt.Errorf("TypeError: str() argument 'encoding' must be str")
		}
		encoding = s.Value()
	}
	if len(args) >= 3 {
		s, ok := args[2].(*objects.Unicode)
		if !ok {
			return nil, "", "", fmt.Errorf("TypeError: str() argument 'errors' must be str")
		}
		errs = s.Value()
	}
	if v, ok := kwargs["encoding"]; ok {
		s, ok := v.(*objects.Unicode)
		if !ok {
			return nil, "", "", fmt.Errorf("TypeError: str() argument 'encoding' must be str")
		}
		encoding = s.Value()
	}
	if v, ok := kwargs["errors"]; ok {
		s, ok := v.(*objects.Unicode)
		if !ok {
			return nil, "", "", fmt.Errorf("TypeError: str() argument 'errors' must be str")
		}
		errs = s.Value()
	}
	buf, ok := bytesLike(args[0])
	if !ok {
		return nil, "", "", fmt.Errorf("TypeError: decoding to str: need a bytes-like object, %s found", args[0].Type().Name)
	}
	return buf, encoding, errs, nil
}

func bytesLike(o objects.Object) ([]byte, bool) {
	switch x := o.(type) {
	case *objects.Bytes:
		return x.Bytes(), true
	case *objects.ByteArray:
		return x.Bytes(), true
	}
	return nil, false
}
