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
	// _frozen_importlib — Lib/importlib/_bootstrap.py. gopy loads the
	// bootstrap from disk at startup and caches it in sys.modules, so this
	// frozen code is never executed; it exists so FrozenImporter.find_spec
	// reports the module with origname "importlib._bootstrap", matching
	// the build-time frozen alias.
	//
	// CPython: Python/frozen.c:70 bootstrap_modules / :116 aliases
	RegisterFrozen(&FrozenModule{
		Name:      "_frozen_importlib",
		Embedded:  true,
		OrigName:  "importlib._bootstrap",
		IsPackage: false,
	})

	// _frozen_importlib_external — Lib/importlib/_bootstrap_external.py
	// CPython: Python/frozen.c:71 bootstrap_modules / :117 aliases
	RegisterFrozen(&FrozenModule{
		Name:      "_frozen_importlib_external",
		Embedded:  true,
		OrigName:  "importlib._bootstrap_external",
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
