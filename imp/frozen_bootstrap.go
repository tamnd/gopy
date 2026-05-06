// Frozen bootstrap module registrations. These correspond to the
// modules that CPython compiles from Lib/importlib/_bootstrap.py and
// Lib/importlib/_bootstrap_external.py at build time and embeds as
// frozen bytecode.
//
// In gopy the Code fields start as nil (placeholder). They are filled
// in by the imp/bootstrap.go init sequence once the compiler produces
// compatible code objects.
//
// CPython: Python/frozen.c:L56 _PyImport_FrozenModules bootstrap entries
package imp

func init() {
	// _frozen_importlib — Lib/importlib/_bootstrap.py
	// CPython: Python/frozen.c:L56
	RegisterFrozen(&FrozenModule{
		Name:      "_frozen_importlib",
		Code:      nil,
		IsPackage: false,
	})

	// _frozen_importlib_external — Lib/importlib/_bootstrap_external.py
	// CPython: Python/frozen.c:L63
	RegisterFrozen(&FrozenModule{
		Name:      "_frozen_importlib_external",
		Code:      nil,
		IsPackage: false,
	})

	// importlib — the top-level importlib package
	// CPython: Python/frozen.c:L70
	RegisterFrozen(&FrozenModule{
		Name:      "importlib",
		Code:      nil,
		IsPackage: true,
	})

	// importlib._bootstrap
	// CPython: Python/frozen.c:L77
	RegisterFrozen(&FrozenModule{
		Name:      "importlib._bootstrap",
		Code:      nil,
		IsPackage: false,
	})

	// importlib._bootstrap_external
	// CPython: Python/frozen.c:L84
	RegisterFrozen(&FrozenModule{
		Name:      "importlib._bootstrap_external",
		Code:      nil,
		IsPackage: false,
	})
}
