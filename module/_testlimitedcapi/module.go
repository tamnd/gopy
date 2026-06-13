// Package testlimitedcapi is the gopy port of CPython's
// Modules/_testlimitedcapi C extension. CPython builds this module from
// several .c files that exercise the public/limited C API; gopy ports
// the abstract-protocol surface (Modules/_testlimitedcapi/abstract.c)
// because the standard-library test suite reaches for it to drive the
// PyObject_*, PyMapping_* and PySequence_* entry points directly,
// independent of the syntactic operators.
//
// CPython: Modules/_testlimitedcapi/abstract.c:1 abstract-protocol tests
//
// Only the abstract.c functions are ported here; the other
// _testlimitedcapi translation units (bytes.c, float.c, ...) are added
// when a gate needs them. Each function below is a thin wrapper over the
// matching gopy objects.* protocol helper, mirroring the C wrapper that
// just forwards to PyObject_*/PyMapping_*/PySequence_*.
package testlimitedcapi

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_testlimitedcapi", buildModule)
}

// arity rejects a call whose positional-argument count differs from the
// fixed number the C signature parses with PyArg_ParseTuple, matching
// the TypeError CPython raises on a bad tuple unpack.
func arity(name string, args []objects.Object, n int) error {
	if len(args) != n {
		return fmt.Errorf("TypeError: %s() takes exactly %d argument(s) (%d given)", name, n, len(args))
	}
	return nil
}

// asIndex coerces an argument parsed with the "n" (Py_ssize_t) format
// to an int, running __index__ exactly as PyArg_ParseTuple does. The
// test_bytes "mutating index" cases rely on __index__ firing here.
//
// CPython: Python/getargs.c convertsimple 'n' -> _PyNumber_Index
func asIndex(o objects.Object) (int, error) {
	idx, err := objects.NumberIndex(o)
	if err != nil {
		return 0, err
	}
	i, ok := idx.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
	}
	v, fits := i.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to C ssize_t")
	}
	return int(v), nil
}

// asStr extracts a C-string argument (the "z#"/"s" formats). gopy passes
// it as a str object; a non-str is the converter's TypeError.
func asStr(name string, o objects.Object) (string, error) {
	u, ok := o.(*objects.Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: %s() argument must be str, not %s", name, o.Type().Name)
	}
	return u.Value(), nil
}

// returnInt mirrors the RETURN_INT macro: a non-error C call that
// returns 0 yields the Python int 0. gopy helpers report failure via a
// Go error, so success simply maps to NewInt(0).
//
// CPython: Modules/_testlimitedcapi/util.h:7 RETURN_INT
func returnInt(err error) (objects.Object, error) {
	if err != nil {
		return nil, err
	}
	return objects.NewInt(0), nil
}

// fromLong mirrors the PyLong_FromLong wrappers used by the boolean-ish
// helpers (PyObject_HasAttr, PyMapping_Check, ...).
func fromLong(b bool) objects.Object {
	if b {
		return objects.NewInt(1)
	}
	return objects.NewInt(0)
}

// --- object protocol -----------------------------------------------------

// CPython: Modules/_testlimitedcapi/abstract.c:5 object_repr
func objectRepr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_repr", args, 1); err != nil {
		return nil, err
	}
	return objects.ReprObject(args[0])
}

// CPython: Modules/_testlimitedcapi/abstract.c:12 object_ascii
func objectASCII(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_ascii", args, 1); err != nil {
		return nil, err
	}
	s, err := objects.ASCII(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:19 object_str
func objectStr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_str", args, 1); err != nil {
		return nil, err
	}
	return objects.StrObject(args[0])
}

// CPython: Modules/_testlimitedcapi/abstract.c:26 object_bytes
func objectBytes(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_bytes", args, 1); err != nil {
		return nil, err
	}
	b, err := objects.ObjectBytes(args[0])
	if err != nil {
		return nil, err
	}
	return b, nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:33 object_getattr
func objectGetattr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_getattr", args, 2); err != nil {
		return nil, err
	}
	return objects.GetAttr(args[0], args[1])
}

// CPython: Modules/_testlimitedcapi/abstract.c:45 object_getattrstring
func objectGetattrstring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_getattrstring", args, 2); err != nil {
		return nil, err
	}
	name, err := asStr("object_getattrstring", args[1])
	if err != nil {
		return nil, err
	}
	return objects.GetAttr(args[0], objects.NewStr(name))
}

// CPython: Modules/_testlimitedcapi/abstract.c:58 object_hasattr
func objectHasattr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_hasattr", args, 2); err != nil {
		return nil, err
	}
	ok, err := objects.HasAttr(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return fromLong(ok), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:70 object_hasattrstring
func objectHasattrstring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_hasattrstring", args, 2); err != nil {
		return nil, err
	}
	name, err := asStr("object_hasattrstring", args[1])
	if err != nil {
		return nil, err
	}
	ok, err := objects.HasAttrString(args[0], name)
	if err != nil {
		return nil, err
	}
	return fromLong(ok), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:83 object_setattr
func objectSetattr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_setattr", args, 3); err != nil {
		return nil, err
	}
	return returnInt(objects.SetAttr(args[0], args[1], args[2]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:96 object_setattrstring
func objectSetattrstring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_setattrstring", args, 3); err != nil {
		return nil, err
	}
	name, err := asStr("object_setattrstring", args[1])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.SetAttr(args[0], objects.NewStr(name), args[2]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:110 object_delattr
func objectDelattr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_delattr", args, 2); err != nil {
		return nil, err
	}
	return returnInt(objects.DelAttr(args[0], args[1]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:123 object_delattrstring
func objectDelattrstring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_delattrstring", args, 2); err != nil {
		return nil, err
	}
	name, err := asStr("object_delattrstring", args[1])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.DelAttr(args[0], objects.NewStr(name)))
}

// --- number protocol -----------------------------------------------------

// CPython: Modules/_testlimitedcapi/abstract.c:136 number_check
func numberCheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("number_check", args, 1); err != nil {
		return nil, err
	}
	return objects.NewBool(objects.NumberCheck(args[0])), nil
}

// --- mapping protocol -----------------------------------------------------

// CPython: Modules/_testlimitedcapi/abstract.c:143 mapping_check
func mappingCheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_check", args, 1); err != nil {
		return nil, err
	}
	return fromLong(objects.MappingCheck(args[0])), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:150 mapping_size
func mappingSize(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_size", args, 1); err != nil {
		return nil, err
	}
	n, err := objects.MappingSize(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:157 mapping_length
func mappingLength(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_length", args, 1); err != nil {
		return nil, err
	}
	n, err := objects.MappingLength(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:164 object_getitem
func objectGetitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_getitem", args, 2); err != nil {
		return nil, err
	}
	return objects.GetItem(args[0], args[1])
}

// CPython: Modules/_testlimitedcapi/abstract.c:176 mapping_getitemstring
func mappingGetitemstring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_getitemstring", args, 2); err != nil {
		return nil, err
	}
	key, err := asStr("mapping_getitemstring", args[1])
	if err != nil {
		return nil, err
	}
	return objects.MappingGetItemString(args[0], key)
}

// CPython: Modules/_testlimitedcapi/abstract.c:189 mapping_haskey
func mappingHaskey(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_haskey", args, 2); err != nil {
		return nil, err
	}
	return fromLong(objects.MappingHasKey(args[0], args[1])), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:201 mapping_haskeystring
func mappingHaskeystring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_haskeystring", args, 2); err != nil {
		return nil, err
	}
	key, err := asStr("mapping_haskeystring", args[1])
	if err != nil {
		return nil, err
	}
	return fromLong(objects.MappingHasKeyString(args[0], key)), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:214 mapping_haskeywitherror
func mappingHaskeywitherror(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_haskeywitherror", args, 2); err != nil {
		return nil, err
	}
	ok, err := objects.MappingHasKeyWithError(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return fromLong(ok), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:226 mapping_haskeystringwitherror
func mappingHaskeystringwitherror(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_haskeystringwitherror", args, 2); err != nil {
		return nil, err
	}
	key, err := asStr("mapping_haskeystringwitherror", args[1])
	if err != nil {
		return nil, err
	}
	ok, err := objects.MappingHasKeyStringWithError(args[0], key)
	if err != nil {
		return nil, err
	}
	return fromLong(ok), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:238 object_setitem
func objectSetitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_setitem", args, 3); err != nil {
		return nil, err
	}
	return returnInt(objects.SetItem(args[0], args[1], args[2]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:251 mapping_setitemstring
func mappingSetitemstring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_setitemstring", args, 3); err != nil {
		return nil, err
	}
	key, err := asStr("mapping_setitemstring", args[1])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.MappingSetItemString(args[0], key, args[2]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:265 object_delitem
func objectDelitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("object_delitem", args, 2); err != nil {
		return nil, err
	}
	return returnInt(objects.DelItem(args[0], args[1]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:277 mapping_delitem
func mappingDelitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_delitem", args, 2); err != nil {
		return nil, err
	}
	return returnInt(objects.MappingDelItem(args[0], args[1]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:289 mapping_delitemstring
func mappingDelitemstring(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_delitemstring", args, 2); err != nil {
		return nil, err
	}
	key, err := asStr("mapping_delitemstring", args[1])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.MappingDelItemString(args[0], key))
}

// CPython: Modules/_testlimitedcapi/abstract.c:302 mapping_keys
func mappingKeys(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_keys", args, 1); err != nil {
		return nil, err
	}
	return objects.MappingKeys(args[0])
}

// CPython: Modules/_testlimitedcapi/abstract.c:309 mapping_values
func mappingValues(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_values", args, 1); err != nil {
		return nil, err
	}
	return objects.MappingValues(args[0])
}

// CPython: Modules/_testlimitedcapi/abstract.c:316 mapping_items
func mappingItems(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("mapping_items", args, 1); err != nil {
		return nil, err
	}
	return objects.MappingItems(args[0])
}

// --- sequence protocol ----------------------------------------------------

// CPython: Modules/_testlimitedcapi/abstract.c:324 sequence_check
func sequenceCheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_check", args, 1); err != nil {
		return nil, err
	}
	return fromLong(objects.SequenceCheck(args[0])), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:331 sequence_size
func sequenceSize(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_size", args, 1); err != nil {
		return nil, err
	}
	n, err := objects.SequenceSize(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:338 sequence_length
// (PySequence_Length is an alias for PySequence_Size.)
func sequenceLength(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_length", args, 1); err != nil {
		return nil, err
	}
	n, err := objects.SequenceSize(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:345 sequence_concat
func sequenceConcat(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_concat", args, 2); err != nil {
		return nil, err
	}
	return objects.SequenceConcat(args[0], args[1])
}

// CPython: Modules/_testlimitedcapi/abstract.c:358 sequence_repeat
func sequenceRepeat(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_repeat", args, 2); err != nil {
		return nil, err
	}
	count, err := asIndex(args[1])
	if err != nil {
		return nil, err
	}
	return objects.SequenceRepeat(args[0], count)
}

// CPython: Modules/_testlimitedcapi/abstract.c:371 sequence_inplaceconcat
func sequenceInplaceconcat(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_inplaceconcat", args, 2); err != nil {
		return nil, err
	}
	return objects.SequenceInPlaceConcat(args[0], args[1])
}

// CPython: Modules/_testlimitedcapi/abstract.c:384 sequence_inplacerepeat
func sequenceInplacerepeat(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_inplacerepeat", args, 2); err != nil {
		return nil, err
	}
	count, err := asIndex(args[1])
	if err != nil {
		return nil, err
	}
	return objects.SequenceInPlaceRepeat(args[0], count)
}

// CPython: Modules/_testlimitedcapi/abstract.c:397 sequence_getitem
func sequenceGetitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_getitem", args, 2); err != nil {
		return nil, err
	}
	i, err := asIndex(args[1])
	if err != nil {
		return nil, err
	}
	return objects.SequenceGetItem(args[0], i)
}

// CPython: Modules/_testlimitedcapi/abstract.c:410 sequence_setitem
func sequenceSetitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_setitem", args, 3); err != nil {
		return nil, err
	}
	i, err := asIndex(args[1])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.SequenceSetItem(args[0], i, args[2]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:425 sequence_delitem
func sequenceDelitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_delitem", args, 2); err != nil {
		return nil, err
	}
	i, err := asIndex(args[1])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.SequenceDelItem(args[0], i))
}

// CPython: Modules/_testlimitedcapi/abstract.c:438 sequence_setslice
func sequenceSetslice(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_setslice", args, 4); err != nil {
		return nil, err
	}
	i1, err := asIndex(args[1])
	if err != nil {
		return nil, err
	}
	i2, err := asIndex(args[2])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.SequenceSetSlice(args[0], i1, i2, args[3]))
}

// CPython: Modules/_testlimitedcapi/abstract.c:452 sequence_delslice
func sequenceDelslice(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_delslice", args, 3); err != nil {
		return nil, err
	}
	i1, err := asIndex(args[1])
	if err != nil {
		return nil, err
	}
	i2, err := asIndex(args[2])
	if err != nil {
		return nil, err
	}
	return returnInt(objects.SequenceDelSlice(args[0], i1, i2))
}

// CPython: Modules/_testlimitedcapi/abstract.c:465 sequence_count
func sequenceCount(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_count", args, 2); err != nil {
		return nil, err
	}
	n, err := objects.SequenceCount(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:478 sequence_contains
func sequenceContains(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_contains", args, 2); err != nil {
		return nil, err
	}
	ok, err := objects.SequenceContains(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return returnInt2(ok)
}

// CPython: Modules/_testlimitedcapi/abstract.c:491 sequence_index
func sequenceIndex(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_index", args, 2); err != nil {
		return nil, err
	}
	n, err := objects.SequenceIndex(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// CPython: Modules/_testlimitedcapi/abstract.c:504 sequence_list
func sequenceList(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_list", args, 1); err != nil {
		return nil, err
	}
	return objects.SequenceList(args[0])
}

// CPython: Modules/_testlimitedcapi/abstract.c:511 sequence_tuple
func sequenceTuple(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_tuple", args, 1); err != nil {
		return nil, err
	}
	return objects.SequenceTuple(args[0])
}

// CPython: Modules/_testlimitedcapi/abstract.c:519 sequence_fast
func sequenceFast(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("sequence_fast", args, 2); err != nil {
		return nil, err
	}
	msg, err := asStr("sequence_fast", args[1])
	if err != nil {
		return nil, err
	}
	return objects.SequenceFast(args[0], msg)
}

// returnInt2 mirrors RETURN_INT for a C call returning a boolean-coded
// int (1 true / 0 false), where the value itself is the result rather
// than a success/failure code.
//
// CPython: Modules/_testlimitedcapi/util.h:7 RETURN_INT
func returnInt2(b bool) (objects.Object, error) {
	if b {
		return objects.NewInt(1), nil
	}
	return objects.NewInt(0), nil
}

// buildModule assembles the _testlimitedcapi module dict, registering
// the abstract-protocol test functions.
//
// CPython: Modules/_testlimitedcapi/abstract.c:548 test_methods
// CPython: Modules/_testlimitedcapi/abstract.c:589 _PyTestLimitedCAPI_Init_Abstract
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_testlimitedcapi")
	d := m.Dict()
	methods := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"object_repr", objectRepr},
		{"object_ascii", objectASCII},
		{"object_str", objectStr},
		{"object_bytes", objectBytes},
		{"object_getattr", objectGetattr},
		{"object_getattrstring", objectGetattrstring},
		{"object_hasattr", objectHasattr},
		{"object_hasattrstring", objectHasattrstring},
		{"object_setattr", objectSetattr},
		{"object_setattrstring", objectSetattrstring},
		{"object_delattr", objectDelattr},
		{"object_delattrstring", objectDelattrstring},
		{"number_check", numberCheck},
		{"mapping_check", mappingCheck},
		{"mapping_size", mappingSize},
		{"mapping_length", mappingLength},
		{"object_getitem", objectGetitem},
		{"mapping_getitemstring", mappingGetitemstring},
		{"mapping_haskey", mappingHaskey},
		{"mapping_haskeystring", mappingHaskeystring},
		{"mapping_haskeywitherror", mappingHaskeywitherror},
		{"mapping_haskeystringwitherror", mappingHaskeystringwitherror},
		{"object_setitem", objectSetitem},
		{"mapping_setitemstring", mappingSetitemstring},
		{"object_delitem", objectDelitem},
		{"mapping_delitem", mappingDelitem},
		{"mapping_delitemstring", mappingDelitemstring},
		{"mapping_keys", mappingKeys},
		{"mapping_values", mappingValues},
		{"mapping_items", mappingItems},
		{"sequence_check", sequenceCheck},
		{"sequence_size", sequenceSize},
		{"sequence_length", sequenceLength},
		{"sequence_concat", sequenceConcat},
		{"sequence_repeat", sequenceRepeat},
		{"sequence_inplaceconcat", sequenceInplaceconcat},
		{"sequence_inplacerepeat", sequenceInplacerepeat},
		{"sequence_getitem", sequenceGetitem},
		{"sequence_setitem", sequenceSetitem},
		{"sequence_delitem", sequenceDelitem},
		{"sequence_setslice", sequenceSetslice},
		{"sequence_delslice", sequenceDelslice},
		{"sequence_count", sequenceCount},
		{"sequence_contains", sequenceContains},
		{"sequence_index", sequenceIndex},
		{"sequence_list", sequenceList},
		{"sequence_tuple", sequenceTuple},
		{"sequence_fast", sequenceFast},
		{"call_vectorcall", callVectorcall},
		{"call_vectorcall_method", callVectorcallMethod},
	}
	for _, mm := range methods {
		if err := d.SetItem(objects.NewStr(mm.name), objects.NewBuiltinFunction(mm.name, mm.fn)); err != nil {
			return nil, err
		}
	}
	if err := buildVectorcallLimitedTypes(d); err != nil {
		return nil, err
	}
	return m, nil
}
