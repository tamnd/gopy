// Frozen test-module registrations. CPython compiles a handful of toy
// modules (__hello__, __phello__ and friends) into the interpreter so
// the import machinery has frozen targets to exercise without touching
// the filesystem. test_frozen and the importlib frozen tests import
// them through FrozenImporter.
//
// gopy keeps the source text (vendored verbatim from CPython's Lib/)
// rather than a marshalled blob and compiles it lazily via
// FrozenCompiler. The same modules are also vendored on disk under the
// stdlib root so the "frozen disabled" code paths can load them through
// the path finder, exactly as CPython ships Lib/__hello__.py alongside
// the frozen copy.
//
// CPython: Python/frozen.c:98 _PyImport_FrozenModules test entries
package imp

// Canonical source for the frozen test modules. These mirror
// Lib/__hello__.py and the Lib/__phello__/ package byte-for-byte.
//
// CPython: Lib/__hello__.py
const frozenHelloSource = `initialized = True

class TestFrozenUtf8_1:
    """\u00b6"""

class TestFrozenUtf8_2:
    """\u03c0"""

class TestFrozenUtf8_4:
    """\U0001f600"""

def main():
    print("Hello world!")

if __name__ == '__main__':
    main()
`

// CPython: Lib/__phello__/__init__.py and Lib/__phello__/spam.py (same body)
const frozenPhelloSource = `initialized = True

def main():
    print("Hello world!")

if __name__ == '__main__':
    main()
`

func init() {
	// __hello__ and its aliases share one source module; the alias
	// entries report __hello__ as their origin so FrozenImporter resolves
	// the on-disk __file__ against Lib/__hello__.py.
	//
	// CPython: Python/frozen.c:98 / _PyImport_FrozenAliases
	RegisterFrozen(&FrozenModule{Name: "__hello__", Source: frozenHelloSource})
	RegisterFrozen(&FrozenModule{Name: "__hello_alias__", Source: frozenHelloSource, OrigName: "__hello__"})
	RegisterFrozen(&FrozenModule{Name: "__phello_alias__", Source: frozenHelloSource, OrigName: "__hello__", IsPackage: true})
	RegisterFrozen(&FrozenModule{Name: "__phello_alias__.spam", Source: frozenHelloSource, OrigName: "__hello__"})

	// __phello__ is a real frozen package with a frozen submodule (spam).
	//
	// CPython: Python/frozen.c:102
	RegisterFrozen(&FrozenModule{Name: "__phello__", Source: frozenPhelloSource, IsPackage: true})
	RegisterFrozen(&FrozenModule{Name: "__phello__.spam", Source: frozenPhelloSource})
}
