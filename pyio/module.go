// _io built-in module. Mirrors the slice of cpython/Modules/_io/
// that the public io module re-exports. The full _io module is a
// large family of types (FileIO, BytesIO, BufferedReader/Writer/Random,
// BufferedRWPair, TextIOWrapper, IncrementalNewlineDecoder, plus the
// _IOBase / _RawIOBase / _BufferedIOBase / _TextIOBase ABC hierarchy);
// this port lands StringIO first because that is what unittest.result
// reaches for, and stubs the rest as bare class objects so the public
// `from _io import (...)` import in Lib/io.py does not fail at the
// name-resolution step.
//
// CPython: Modules/_io/_iomodule.c:580 module_methods (module-level fns)
// CPython: Modules/_io/_iomodule.c:782 PyInit__io (the build hook)

package pyio

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_io", buildModule)
}

// DefaultBufferSize is the value Lib/io.py copies into its module-level
// constant of the same name. Mirrors the macro in _iomodule.h.
//
// CPython: Modules/_io/_iomodule.h:14 DEFAULT_BUFFER_SIZE
const DefaultBufferSize = 8192

// stubType is a placeholder Type used to occupy class names io.py
// imports through `from _io import ...`. They are subclassable and
// instantiable but raise NotImplementedError when called, since the
// underlying implementation has not been ported yet.
//
// CPython: Modules/_io/_iomodule.c:782 PyInit__io (those rows come
// from individual ported types; this is the placeholder until each
// gets its own port).
func stubType(name string) *objects.Type {
	t := objects.NewType(name, []*objects.Type{objects.ObjectType()})
	t.Call = func(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return nil, fmt.Errorf("NotImplementedError: %s is not yet ported", name)
	}
	return t
}

// unsupportedOpType is the _io.UnsupportedOperation exception class.
// CPython makes it a subclass of OSError + ValueError; this port keeps
// the surface narrow (a callable that returns itself wrapped in an
// error message) since no current import path raises it.
//
// CPython: Modules/_io/_iomodule.c:621 _PyIO_get_module_state +
// CPython: Modules/_io/_iomodule.c:709 (UnsupportedOperation registration)
var unsupportedOpType = stubType("_io.UnsupportedOperation")

// buildModule materializes the _io module dict. Mirrors the
// PyInit__io entry: register every type and module-level constant
// the public io module imports through `from _io import ...`.
//
// CPython: Modules/_io/_iomodule.c:782 PyInit__io
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_io")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		// StringIO: ported in stringio.go.
		{"StringIO", StringIOType},

		// Stubs for the rest of the io.py import surface. Each will
		// be replaced by a real port when its caller path matters.
		{"BytesIO", stubType("_io.BytesIO")},
		{"FileIO", stubType("_io.FileIO")},
		{"BufferedReader", stubType("_io.BufferedReader")},
		{"BufferedWriter", stubType("_io.BufferedWriter")},
		{"BufferedRWPair", stubType("_io.BufferedRWPair")},
		{"BufferedRandom", stubType("_io.BufferedRandom")},
		{"TextIOWrapper", stubType("_io.TextIOWrapper")},
		{"IncrementalNewlineDecoder", stubType("_io.IncrementalNewlineDecoder")},
		{"_IOBase", stubType("_io._IOBase")},
		{"_RawIOBase", stubType("_io._RawIOBase")},
		{"_BufferedIOBase", stubType("_io._BufferedIOBase")},
		{"_TextIOBase", stubType("_io._TextIOBase")},

		{"UnsupportedOperation", unsupportedOpType},

		{"DEFAULT_BUFFER_SIZE", objects.NewInt(int64(DefaultBufferSize))},

		// open / open_code / text_encoding: the io.py shim copies
		// these straight out, so placeholder builtins are enough for
		// the import to succeed; calling them surfaces the gap.
		//
		// CPython: Modules/_io/_iomodule.c:218 _io_open_impl
		// CPython: Modules/_io/_iomodule.c:432 _io_open_code_impl
		// CPython: Modules/_io/_iomodule.c:475 _io_text_encoding_impl
		{"open", objects.NewBuiltinFunction("open", openStub)},
		{"open_code", objects.NewBuiltinFunction("open_code", openStub)},
		{"text_encoding", objects.NewBuiltinFunction("text_encoding", textEncodingStub)},
	}

	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// openStub is the placeholder for _io.open until the FileIO /
// BufferedReader / BufferedWriter / TextIOWrapper port lands.
//
// CPython: Modules/_io/_iomodule.c:218 _io_open_impl
func openStub(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: _io.open is not yet ported; only StringIO is wired today")
}

// textEncodingStub is the placeholder for _io.text_encoding. Defaults
// to "utf-8" when called with None to keep API users moving until the
// real flag-driven resolver lands.
//
// CPython: Modules/_io/_iomodule.c:475 _io_text_encoding_impl
func textEncodingStub(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 && !objects.IsNone(args[0]) {
		return args[0], nil
	}
	return objects.NewStr("utf-8"), nil
}
