// Port of builtin___import___impl. The builtin is the Python entry
// point for the import machinery: bytecode IMPORT_NAME calls it with
// the same five-argument shape, and user code can call it directly
// for dynamic import. The body delegates to the imp package's
// ImportModuleLevel via a hook installed by vm.init, so the package
// graph stays builtins -> objects, vm -> builtins / imp.
//
// CPython: Python/bltinmodule.c:259 builtin___import___impl
// CPython: Python/import.c:1561 PyImport_ImportModuleLevelObject

package builtins

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

// Importer resolves a module by name, with pkgname as the anchor for
// relative imports and level as the dot-count. fromlist is empty for
// `import a.b.c` and non-empty for `from a.b import c, d`. The hook
// returns the resolved module along with the same chain CPython hands
// back: when fromlist is empty the caller wants the top-level package,
// when fromlist is non-empty the caller wants the deepest module so
// IMPORT_FROM can grab attributes off it.
//
// CPython: Python/import.c:1561 PyImport_ImportModuleLevelObject
type Importer func(name, pkgname string, level int, fromlist []string) (objects.Object, error)

var currentImporter Importer

// SetImporter installs the import hook used by __import__. vm.init
// wires this on startup; tests can override directly.
//
// CPython: equivalent of PyImport_GetImportFunc reading
// builtins.__import__
func SetImporter(p Importer) {
	currentImporter = p
}

// Import implements builtins.__import__(name, globals=None, locals=None,
// fromlist=(), level=0). The signature matches CPython's
// builtin___import__ argument clinic spec; locals is parsed and
// ignored, just like in CPython where it is reserved for future use.
//
// CPython: Python/bltinmodule.c:259 builtin___import___impl
func Import(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	parsed, err := parseImportArgs(args, kwargs)
	if err != nil {
		return nil, err
	}
	// _sanity_check rejects an empty absolute module name before any
	// finder runs. The level < 0 case is screened in parseImportArgs.
	//
	// CPython: Lib/importlib/_bootstrap.py:1380 _sanity_check
	if parsed.name == "" && parsed.level == 0 {
		return nil, fmt.Errorf("ValueError: Empty module name")
	}
	if currentImporter == nil {
		return nil, fmt.Errorf("ImportError: __import__ not configured")
	}
	// The bound __import__ only computes the package anchor for a
	// relative import; an absolute import (level == 0) never touches
	// _calc___package__, so its ImportWarning fallback only fires here.
	//
	// CPython: Lib/importlib/_bootstrap.py:1390 __import__
	if parsed.level > 0 {
		if err := warnPackageFallback(parsed.globals); err != nil {
			return nil, err
		}
	}
	pkgname := pkgnameFromGlobals(parsed.globals)
	return currentImporter(parsed.name, pkgname, parsed.level, parsed.fromlist)
}

type importArgs struct {
	name     string
	globals  objects.Object
	fromlist []string
	level    int
}

// parseImportArgs binds positional and keyword arguments to the
// __import__ signature. The order is name, globals, locals, fromlist,
// level; locals is accepted for signature parity but never read.
//
// CPython: Python/clinic/bltinmodule.c.h builtin___import__ argument
// clinic block
func parseImportArgs(args []objects.Object, kwargs map[string]objects.Object) (importArgs, error) {
	const maxArgs = 5
	if len(args) > maxArgs {
		return importArgs{}, fmt.Errorf("TypeError: __import__ expected at most 5 arguments, got %d", len(args))
	}
	names := []string{"name", "globals", "locals", "fromlist", "level"}
	bound := make([]objects.Object, maxArgs)
	copy(bound, args)
	for k, v := range kwargs {
		idx := -1
		for i, n := range names {
			if n == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			return importArgs{}, fmt.Errorf("TypeError: __import__() got an unexpected keyword argument %q", k)
		}
		if bound[idx] != nil {
			return importArgs{}, fmt.Errorf("TypeError: __import__() got multiple values for argument %q", k)
		}
		bound[idx] = v
	}
	if bound[0] == nil {
		return importArgs{}, fmt.Errorf("TypeError: __import__() missing required argument: 'name'")
	}
	name, err := stringArg(bound[0], "name")
	if err != nil {
		return importArgs{}, err
	}
	level := 0
	if bound[4] != nil {
		level, err = intArg(bound[4], "level")
		if err != nil {
			return importArgs{}, err
		}
		if level < 0 {
			return importArgs{}, fmt.Errorf("ValueError: level must be >= 0")
		}
	}
	fromlist, err := fromlistArg(bound[3])
	if err != nil {
		return importArgs{}, err
	}
	return importArgs{
		name:     name,
		globals:  bound[1],
		fromlist: fromlist,
		level:    level,
	}, nil
}

// fromlistArg unpacks the fromlist argument into a flat []string.
// None and missing both mean "empty"; a tuple or list is iterated; any
// other type is a TypeError. The element check matches CPython's
// import.c which rejects non-str entries before lookup.
//
// CPython: Python/import.c:1726 import_from
func fromlistArg(o objects.Object) ([]string, error) {
	if o == nil || objects.IsNone(o) {
		return nil, nil
	}
	var raw []objects.Object
	switch v := o.(type) {
	case *objects.Tuple:
		raw = make([]objects.Object, v.Len())
		for i := range raw {
			raw[i] = v.Item(i)
		}
	case *objects.List:
		raw = make([]objects.Object, v.Len())
		for i := range raw {
			raw[i] = v.Item(i)
		}
	default:
		return nil, fmt.Errorf("TypeError: fromlist must be a tuple or list")
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, err := stringArg(item, "fromlist item")
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// stringArg coerces o to a Go string, raising TypeError when o isn't a
// Python str. The label is the argument name used in the error.
func stringArg(o objects.Object, label string) (string, error) {
	if u, ok := o.(*objects.Unicode); ok {
		return u.Value(), nil
	}
	return "", fmt.Errorf("TypeError: %s must be str, not %s", label, o.Type().Name)
}

// intArg coerces o to a non-negative Go int, raising TypeError when o
// isn't a Python int and OverflowError when the value won't fit.
func intArg(o objects.Object, label string) (int, error) {
	iv, ok := o.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s must be int, not %s", label, o.Type().Name)
	}
	v, exact := iv.Int64()
	if !exact {
		return 0, fmt.Errorf("OverflowError: %s does not fit in a Go int", label)
	}
	return int(v), nil
}

// pkgnameFromGlobals reads the import anchor out of the caller's
// globals dict. __package__ wins when set; otherwise __name__ stands
// in. A missing or non-dict globals yields "", which makes any level>0
// call fail in resolveAbsName, matching CPython's resolve_name.
//
// CPython: Python/import.c:1572 resolve_name
func pkgnameFromGlobals(globals objects.Object) string {
	d, ok := globals.(*objects.Dict)
	if !ok {
		return ""
	}
	if pkg := dictStringEntry(d, "__package__"); pkg != "" {
		return pkg
	}
	name := dictStringEntry(d, "__name__")
	if name == "" {
		return ""
	}
	// __spec__.parent would be the third fallback in CPython, but
	// gopy doesn't carry __spec__ on module dicts yet; until then we
	// strip the trailing component of __name__ for package modules
	// the way CPython does once it has determined __name__ refers to
	// a package via __path__.
	if dictStringEntry(d, "__path__") != "" {
		return name
	}
	// _calc___package__ computes __name__.rpartition('.')[0] when the
	// module is not a package. For a top-level name like "__main__" that
	// is the empty string, so a level>0 import from it has no anchor and
	// _sanity_check raises ImportError.
	//
	// CPython: Lib/importlib/_bootstrap.py:1349 _calc___package__
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[:dot]
	}
	return ""
}

// warnPackageFallback mirrors the else branch of _calc___package__:
// when a relative import (level > 0) runs in a module whose globals
// carry neither a non-None __package__ nor a non-None __spec__, the
// machinery falls back to __name__/__path__ and emits an ImportWarning
// before doing so (bpo-37409). A non-dict globals has nothing to read,
// so it warns nothing.
//
// CPython: Lib/importlib/_bootstrap.py:1349 _calc___package__
func warnPackageFallback(globals objects.Object) error {
	d, ok := globals.(*objects.Dict)
	if !ok {
		return nil
	}
	if dictHasNonNone(d, "__package__") || dictHasNonNone(d, "__spec__") {
		return nil
	}
	return emitImportWarning("can't resolve package from __spec__ or __package__, falling back on __name__ and __path__")
}

// dictHasNonNone reports whether d[key] exists and is not None. Used to
// decide whether __package__ / __spec__ supply a usable anchor before
// falling back to __name__.
func dictHasNonNone(d *objects.Dict, key string) bool {
	v, err := d.GetItem(objects.NewStr(key))
	if err != nil || v == nil || objects.IsNone(v) {
		return false
	}
	return true
}

// emitImportWarning routes message through warnings.warn(message,
// ImportWarning) so the emission walks the live filter list and any
// recording context manager (catch_warnings / assertWarns).
//
// CPython: Python/_warnings.c PyErr_WarnFormat(PyExc_ImportWarning, ...)
func emitImportWarning(message string) error {
	mod, err := importBreakpointModule("warnings")
	if err != nil {
		return err
	}
	warn, err := objects.GetAttr(mod, objects.NewStr("warn"))
	if err != nil {
		return err
	}
	_, err = objects.Call(warn, objects.NewTuple([]objects.Object{
		objects.NewStr(message),
		errors.PyExc_ImportWarning,
	}), nil)
	return err
}

// dictStringEntry reads dict[key] and returns its string value, or ""
// when the key is missing, the value is None, or the value isn't a
// str.
func dictStringEntry(d *objects.Dict, key string) string {
	v, err := d.GetItem(objects.NewStr(key))
	if err != nil || v == nil || objects.IsNone(v) {
		return ""
	}
	if u, ok := v.(*objects.Unicode); ok {
		return u.Value()
	}
	return ""
}
