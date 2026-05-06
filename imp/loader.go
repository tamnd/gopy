// Source and bytecode loaders. Mirrors the CPython SourceFileLoader
// and SourcelessFileLoader from Lib/importlib/_bootstrap_external.py.
//
// LoadPyc reads a .pyc file and calls ExecCodeModule. LoadSource
// compiles a .py file via a caller-provided SourceCompiler then calls
// ExecCodeModule; the compiler function is injected to avoid a
// circular import between imp and the parser/compile packages.
//
// CPython: Lib/importlib/_bootstrap_external.py:L1045 SourceFileLoader
// CPython: Lib/importlib/_bootstrap_external.py:L1215 SourcelessFileLoader
package imp

import (
	"fmt"
	"os"

	"github.com/tamnd/gopy/marshal"
	"github.com/tamnd/gopy/objects"
)

// SourceCompiler compiles Python source text to a code object.
// Callers (lifecycle, pythonrun) inject a concrete implementation;
// this breaks the circular dependency between imp and parser/compile.
//
// CPython: Python/pythonrun.c:L1102 Py_CompileStringExFlags
type SourceCompiler func(src, filename string) (*objects.Code, error)

// LoadPyc opens filename, validates the .pyc header, and executes the
// embedded code object in a module named name. The returned module is
// registered in sys.modules.
//
// CPython: Lib/importlib/_bootstrap_external.py:L1215 SourcelessFileLoader.exec_module
func LoadPyc(exec Executor, filename, name string) (*objects.Module, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("imp: LoadPyc %q: %w", filename, err)
	}
	defer f.Close()

	code, _, err := marshal.ReadPyc(f)
	if err != nil {
		return nil, fmt.Errorf("imp: LoadPyc %q: %w", filename, err)
	}

	return ExecCodeModule(exec, name, code)
}

// LoadSource compiles src to a code object via compiler, then executes
// it in a module named name. The returned module is registered in
// sys.modules.
//
// CPython: Lib/importlib/_bootstrap_external.py:L1045 SourceFileLoader.exec_module
func LoadSource(exec Executor, compiler SourceCompiler, src, filename, name string) (*objects.Module, error) {
	code, err := compiler(src, filename)
	if err != nil {
		return nil, fmt.Errorf("imp: LoadSource %q: compile: %w", filename, err)
	}
	return ExecCodeModule(exec, name, code)
}

// LoadSourceFile reads filename, compiles it via compiler, and calls
// LoadSource. It is a convenience wrapper for the common case of
// loading from disk.
//
// CPython: Lib/importlib/_bootstrap_external.py:L1045 SourceFileLoader.get_code
func LoadSourceFile(exec Executor, compiler SourceCompiler, filename, name string) (*objects.Module, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("imp: LoadSourceFile %q: %w", filename, err)
	}
	return LoadSource(exec, compiler, string(src), filename, name)
}
