package symtable

import "strings"

// Mangle implements PEP 8 private name mangling: identifiers starting
// with two underscores (and not ending with two underscores) inside a
// class body get rewritten to "_<ClassName><name>". The class name's
// leading underscores are stripped before splicing.
//
// `private` is the name of the lexically enclosing class, or "" when
// no class is in scope. With `private` empty, or when the rules below
// disqualify mangling, the original name is returned unchanged.
//
// Specific rules ported from _Py_Mangle:
//   - the identifier must start with "__"
//   - identifiers ending with "__" are left alone (e.g. "__init__")
//   - identifiers containing "." are left alone (the qualifier in a
//     dotted import is not a Python name)
//   - if `private` is all underscores ("_", "__", ...), no mangling
//
// CPython: Python/symtable.c:L3207 _Py_Mangle
func Mangle(private, name string) string {
	if private == "" {
		return name
	}
	if len(name) < 2 || name[0] != '_' || name[1] != '_' {
		return name
	}
	if strings.HasSuffix(name, "__") {
		return name
	}
	if strings.Contains(name, ".") {
		return name
	}
	stripped := strings.TrimLeft(private, "_")
	if stripped == "" {
		return name
	}
	return "_" + stripped + name
}

// MaybeMangle is the variant the visit pass calls. Most blocks mangle
// every name (`Mangle`); a class scope's enclosed type-parameters
// block carries a MangledNames allow-list and only mangles the names
// in that set. Mirrors the PEP 695 rule that lets a type parameter
// with a private name be referenced as the unmangled identifier
// outside the class body.
//
// CPython: Python/symtable.c:L3187 _Py_MaybeMangle
func MaybeMangle(private string, ste *Entry, name string) string {
	if ste != nil && ste.MangledNames != nil {
		if _, ok := ste.MangledNames[name]; !ok {
			return name
		}
	}
	return Mangle(private, name)
}
