// FunctionStr builds the callable display name CPython uses inside
// call-time TypeError messages ("dict.__contains__() takes exactly one
// argument", "foo() got multiple values ..."). It reads __qualname__,
// falling back to str(x) when the attribute is absent, and prefixes
// __module__ unless that module is None or "builtins". Built-in method
// descriptors carry a dotted __qualname__ (e.g. "dict.__contains__")
// and no __module__, so they render as "dict.__contains__()"; a
// module-level builtin keeps its bare name ("len()").
//
// CPython: Objects/object.c:973 _PyObject_FunctionStr

package objects

import "fmt"

// FunctionStr ports _PyObject_FunctionStr.
//
// CPython: Objects/object.c:973 _PyObject_FunctionStr
func FunctionStr(x Object) (string, error) {
	qualname, err := GetAttr(x, NewStr("__qualname__"))
	if err != nil {
		if isAttributeError(err) {
			// No __qualname__: fall back to str(x), matching the
			// PyObject_Str branch.
			return Str(x)
		}
		return "", err
	}
	qn, err := Str(qualname)
	if err != nil {
		return "", err
	}
	module, merr := GetAttr(x, NewStr("__module__"))
	if merr != nil {
		if isAttributeError(merr) {
			return qn + "()", nil
		}
		return "", merr
	}
	if module != nil && module != None() {
		mod, err := Str(module)
		if err != nil {
			return "", err
		}
		if mod != "builtins" {
			return fmt.Sprintf("%s.%s()", mod, qn), nil
		}
	}
	return qn + "()", nil
}
