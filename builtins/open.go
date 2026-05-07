// Port of io.open via the pure-Python reference in Lib/_pyio.py:75
// def open. CPython's C entry point is Modules/_io/_iomodule.c io_open;
// both share the same shape: validate the mode string, open the file
// descriptor, then layer the buffered / text adapters on top. gopy's
// File type already collapses those adapters, so this function is the
// argument validator + os.OpenFile wrapper.
//
// CPython: Lib/_pyio.py:75 open (reference Python implementation)
// CPython: Modules/_io/_iomodule.c:177 io_open

package builtins

import (
	"errors"
	"fmt"
	"os"

	"github.com/tamnd/gopy/objects"
)

// Open implements the open() builtin. It accepts the file path
// (str / bytes), a mode string, and the optional buffering / encoding
// / errors / newline / closefd / opener arguments. The returned object
// is a File: in binary mode it satisfies the BufferedReader /
// BufferedWriter contract (read/write trades in bytes); in text mode it
// satisfies TextIOWrapper (read/write trades in str).
//
// CPython: Lib/_pyio.py:193 open body
// CPython: Modules/_io/_iomodule.c:177 io_open
func Open(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	a, err := bindOpenArgs(args, kwargs)
	if err != nil {
		return nil, err
	}
	if err := validateOpenMode(a); err != nil {
		return nil, err
	}
	return openFile(a)
}

// openArgs holds the bound + decoded form of every parameter open()
// accepts. Keeping the bag separate from Open keeps the validator
// helpers small and lets the test code construct one directly.
type openArgs struct {
	file   string
	mode   string
	create bool
	read   bool
	write  bool
	append bool
	update bool
	text   bool
	binary bool
}

// bindOpenArgs unpacks the call into named slots. CPython's clinic
// schema is open(file, mode='r', buffering=-1, encoding=None,
// errors=None, newline=None, closefd=True, opener=None); the v0.10.1
// port surfaces file + mode and validates the rest as accepted but
// otherwise ignored (only buffering=0 in text mode is rejected, since
// CPython does so unconditionally).
//
// CPython: Lib/_pyio.py:75 open signature; argument-binding helpers
// from Modules/_io/_iomodule.c io_open.
func bindOpenArgs(args []objects.Object, kwargs map[string]objects.Object) (*openArgs, error) {
	if len(args) > 8 {
		return nil, fmt.Errorf("TypeError: open() takes at most 8 arguments (%d given)", len(args))
	}
	names := []string{"file", "mode", "buffering", "encoding", "errors", "newline", "closefd", "opener"}
	bound, err := bindByName(names, args, kwargs, "open")
	if err != nil {
		return nil, err
	}
	if bound[0] == nil {
		return nil, errors.New("TypeError: open() missing required argument: 'file'")
	}
	a := &openArgs{}
	if a.file, err = openFileArg(bound[0]); err != nil {
		return nil, err
	}
	if a.mode, err = openModeArg(bound[1]); err != nil {
		return nil, err
	}
	if err := openBufferingArg(bound[2]); err != nil {
		return nil, err
	}
	if err := openOptionalStr(bound[3], "encoding"); err != nil {
		return nil, err
	}
	if err := openOptionalStr(bound[4], "errors"); err != nil {
		return nil, err
	}
	if err := openOptionalStr(bound[5], "newline"); err != nil {
		return nil, err
	}
	return a, nil
}

// bindByName is the small kwargs binder shared by Open. It reuses the
// shape eval/exec/compile use so the call-site error wording stays
// consistent with the rest of the builtins package.
func bindByName(names []string, args []objects.Object, kwargs map[string]objects.Object, fnName string) ([]objects.Object, error) {
	bound := make([]objects.Object, len(names))
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
			return nil, fmt.Errorf("TypeError: %s() got an unexpected keyword argument %q", fnName, k)
		}
		if bound[idx] != nil {
			return nil, fmt.Errorf("TypeError: %s() got multiple values for argument %q", fnName, k)
		}
		bound[idx] = v
	}
	return bound, nil
}

// openFileArg coerces the file argument into a path string. The full
// CPython open() also accepts an int file descriptor and runs a path
// through os.fspath; the v0.10.1 cut accepts str and bytes only.
//
// CPython: Lib/_pyio.py:193 isinstance(file, ...) checks
func openFileArg(o objects.Object) (string, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	}
	return "", fmt.Errorf("TypeError: invalid file: %s", o.Type().Name)
}

// openModeArg returns the mode string. nil/None defaults to "r" the
// way the Python signature default does.
func openModeArg(o objects.Object) (string, error) {
	if o == nil || objects.IsNone(o) {
		return "r", nil
	}
	s, ok := o.(*objects.Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: invalid mode: %s", o.Type().Name)
	}
	return s.Value(), nil
}

// openBufferingArg validates the buffering argument's type. Real
// buffering control lands with the BufferedReader / BufferedWriter
// split; for now any int is accepted and ignored, anything else is a
// TypeError.
func openBufferingArg(o objects.Object) error {
	if o == nil || objects.IsNone(o) {
		return nil
	}
	if _, ok := o.(*objects.Int); !ok {
		return fmt.Errorf("TypeError: invalid buffering: %s", o.Type().Name)
	}
	return nil
}

// openOptionalStr enforces "None or str" on the encoding / errors /
// newline kwargs. The values themselves are not yet honored: text
// mode goes through Go's UTF-8 decoder regardless. CPython's text
// layer would consult encoding for codec lookup and newline for
// universal-newline translation.
//
// CPython: Lib/_pyio.py:201 type checks
func openOptionalStr(o objects.Object, name string) error {
	if o == nil || objects.IsNone(o) {
		return nil
	}
	if _, ok := o.(*objects.Unicode); !ok {
		return fmt.Errorf("TypeError: invalid %s: %s", name, o.Type().Name)
	}
	return nil
}

// validateOpenMode walks the mode string with the exact rule set
// _pyio.open uses: every char must come from "axrwb+t", no duplicates,
// exactly one of r/w/a/x, no t+b. The booleans on a are filled in here
// so openFile can pick the os.OpenFile flags by simple boolean math.
//
// CPython: Lib/_pyio.py:205 mode validation block
func validateOpenMode(a *openArgs) error {
	mode := a.mode
	allowed := "axrwb+t"
	seen := map[byte]bool{}
	for i := 0; i < len(mode); i++ {
		c := mode[i]
		if !containsByte(allowed, c) {
			return fmt.Errorf("ValueError: invalid mode: %q", mode)
		}
		if seen[c] {
			return fmt.Errorf("ValueError: invalid mode: %q", mode)
		}
		seen[c] = true
	}
	a.create = seen['x']
	a.read = seen['r']
	a.write = seen['w']
	a.append = seen['a']
	a.update = seen['+']
	a.text = seen['t']
	a.binary = seen['b']
	if a.text && a.binary {
		return errors.New("ValueError: can't have text and binary mode at once")
	}
	if boolToInt(a.create)+boolToInt(a.read)+boolToInt(a.write)+boolToInt(a.append) > 1 {
		return errors.New("ValueError: can't have read/write/append mode at once")
	}
	if !a.create && !a.read && !a.write && !a.append {
		return errors.New("ValueError: must have exactly one of read/write/append mode")
	}
	return nil
}

func containsByte(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// openFile turns the validated openArgs into an os.OpenFile call and
// wraps the resulting descriptor in objects.NewFile. Mode booleans map
// to the standard POSIX flag combos: read-only, write-truncate,
// append, exclusive-create, plus the +-update read/write toggle.
//
// CPython: Lib/_pyio.py:232 raw = FileIO(...)
func openFile(a *openArgs) (objects.Object, error) {
	flag, readable, writable := openFlagsFor(a)
	f, err := os.OpenFile(a.file, flag, 0o600)
	if err != nil {
		return nil, fmt.Errorf("OSError: %s", err.Error())
	}
	binary := a.binary
	return objects.NewFile(f, a.file, a.mode, binary, readable, writable), nil
}

// openFlagsFor returns (os.OpenFile flags, readable, writable) for
// the validated mode. Updating ('+') flips both readable and writable
// regardless of the base mode, mirroring _pyio.open's BufferedRandom
// branch.
//
// CPython: Lib/_pyio.py:233 the (creating?"x":"" | reading?"r":"" |
// ...) string passed to FileIO.
func openFlagsFor(a *openArgs) (int, bool, bool) {
	var flag int
	readable := a.read || a.update
	writable := a.write || a.append || a.create || a.update
	switch {
	case a.read:
		flag = os.O_RDONLY
	case a.write:
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case a.append:
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case a.create:
		flag = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	if a.update {
		flag = (flag &^ (os.O_RDONLY | os.O_WRONLY)) | os.O_RDWR
	}
	return flag, readable, writable
}
