package errors

import (
	"github.com/tamnd/gopy/objects"
)

// importErrorKwlist is the keyword-only parameter list ImportError_init
// hands to PyArg_ParseTupleAndKeywords as "|$OOO:ImportError". The same
// list drives both the over-supply count check and the
// unexpected-keyword suggestion lookup.
//
// CPython: Objects/exceptions.c:1811 ImportError_init kwlist
var importErrorKwlist = []string{"name", "path", "name_from"}

// init wires ImportError_init onto PyExc_ImportError. ModuleNotFoundError
// inherits the slot through the MRO walk, exactly as CPython shares
// ImportError_init across the subclass.
//
// CPython: Objects/exceptions.c:1953 ImportError type (tp_init)
func init() {
	objects.SetTypeDescr(PyExc_ImportError, "__init__",
		objects.NewMethodDescr(PyExc_ImportError, "__init__", importErrorInit).
			WithKwParams("ImportError", importErrorKwlist, len(importErrorKwlist)))

	// msg / name / path / name_from are Py_T_OBJECT members on
	// PyImportErrorObject: reading a member that was never set yields
	// None rather than raising AttributeError, and writing stores the
	// value. runpy/importlib both read e.name on a caught ImportError,
	// so the attribute must always exist. msg is set from the single
	// positional arg by ImportError_init rather than via a keyword.
	//
	// CPython: Objects/exceptions.c:1932 ImportError_members
	for _, name := range append([]string{"msg"}, importErrorKwlist...) {
		field := name
		objects.SetTypeDescr(PyExc_ImportError, field, objects.NewGetSetDescr(field,
			func(o objects.Object) (objects.Object, error) { return importErrorMember(o, field) },
			func(o, v objects.Object) error { return importErrorMemberSet(o, field, v) }))
	}
}

// importErrorMember reads an ImportError member from the instance attr
// dict, returning None when unset to mirror Py_T_OBJECT's NULL->None.
//
// CPython: Include/descrobject.h Py_T_OBJECT (member_get NULL -> None)
func importErrorMember(o objects.Object, field string) (objects.Object, error) {
	e, ok := o.(*Exception)
	if !ok {
		return objects.None(), nil
	}
	d := e.AttrDict()
	if d == nil {
		return objects.None(), nil
	}
	// A missing member reads back as None (Py_T_OBJECT NULL->None), so a
	// lookup miss is not an error here; discard it deliberately.
	v, _ := d.GetItem(objects.NewStr(field))
	if v == nil {
		return objects.None(), nil
	}
	return v, nil
}

// importErrorMemberSet writes an ImportError member through the
// instance attr dict, allocating it lazily.
//
// CPython: Objects/exceptions.c:1893 ImportError_members (member_set)
func importErrorMemberSet(o objects.Object, field string, v objects.Object) error {
	e, ok := o.(*Exception)
	if !ok {
		return nil
	}
	if v == nil {
		v = objects.None()
	}
	return e.EnsureAttrDict().SetItem(objects.NewStr(field), v)
}

// importErrorInit ports ImportError_init: it runs BaseException_init over
// the positional args, then parses the keyword-only name / path /
// name_from parameters. Passing any other keyword (or more keywords than
// the three the signature accepts) raises the same vgetargskeywords
// TypeErrors PyArg_ParseTupleAndKeywords would, including the
// "Did you mean 'X'?" suggestion tail.
//
// CPython: Objects/exceptions.c:1809 ImportError_init
func importErrorInit(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// BaseException_init(op, args, NULL): store the positional args on
	// self.args. The keyword dict is parsed separately below, so it is
	// withheld from the base initializer.
	if _, err := baseExceptionInit(args, nil); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return objects.None(), nil
	}
	e, ok := args[0].(*Exception)
	if !ok {
		return objects.None(), nil
	}

	// msg is set from the lone positional argument: PyTuple_GET_SIZE(args)
	// counts the exception args tuple, which here is args[1:].
	//
	// CPython: Objects/exceptions.c:1836 ImportError_init (self->msg)
	if len(args) == 2 {
		_ = e.EnsureAttrDict().SetItem(objects.NewStr("msg"), args[1])
	}

	if len(kwargs) > 0 {
		// PyArg_ParseTupleAndKeywords("|$OOO") with an empty positional
		// tuple: the surplus check counts every keyword against the three
		// declared parameters, then each keyword must match one of them.
		//
		// CPython: Python/getargs.c:1638 vgetargskeywords
		if err := objects.CheckKeywordCount("ImportError", 0, len(kwargs), len(importErrorKwlist)); err != nil {
			return nil, err
		}
		for k := range kwargs {
			switch k {
			case "name", "path", "name_from":
			default:
				return nil, objects.UnexpectedKeywordError("ImportError", k, importErrorKwlist)
			}
		}
		// Store the parsed members so exc.name / exc.path / exc.name_from
		// read back the supplied values, matching the Py_XSETREF stores.
		//
		// CPython: Objects/exceptions.c:1832 Py_XSETREF(self->name, ...)
		d := e.EnsureAttrDict()
		for _, k := range importErrorKwlist {
			if v, ok := kwargs[k]; ok {
				_ = d.SetItem(objects.NewStr(k), v)
			}
		}
	}
	return objects.None(), nil
}
