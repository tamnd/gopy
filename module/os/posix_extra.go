// Cross-platform POSIX functions beyond the initial slice: chmod,
// symlink, readlink, link, _exit, urandom, cpu_count.
//
// CPython: Modules/posixmodule.c (os_chmod_impl, os_symlink_impl,
// os_readlink_impl, os_link_impl, os__exit_impl, os_urandom_impl,
// os_cpu_count_impl)

package os

import (
	"crypto/rand"
	"fmt"
	goos "os"
	"runtime"

	"github.com/tamnd/gopy/objects"
)

// osChmod changes the mode of path. The dir_fd / follow_symlinks
// keyword arguments are accepted but the underlying Go API only honors
// the default behavior (follow symlinks).
//
// CPython: Modules/posixmodule.c os_chmod_impl
func osChmod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: chmod() missing required arguments")
	}
	p, err := pathStringArg(args[0], "chmod")
	if err != nil {
		return nil, err
	}
	mode, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: chmod() mode must be int")
	}
	m, _ := mode.Int64()
	if err := goos.Chmod(p, goos.FileMode(m)); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// pathStringArg coerces a path argument the way CPython's path_converter
// does: a str is taken verbatim, bytes are decoded, and any other object is
// run through os.fspath (__fspath__) so pathlib.Path and other PathLike
// objects are accepted.
//
// CPython: Modules/posixmodule.c:1093 path_converter
func pathStringArg(o objects.Object, fname string) (string, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	}
	m, err := objects.GetAttr(o, objects.NewStr("__fspath__"))
	if err != nil {
		return "", fmt.Errorf("TypeError: %s: path should be string, bytes or os.PathLike, not %s", fname, o.Type().Name)
	}
	r, err := objects.Call(m, objects.NewTuple(nil), nil)
	if err != nil {
		return "", err
	}
	switch v := r.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	}
	return "", fmt.Errorf("TypeError: expected __fspath__ to return str or bytes, not %s", r.Type().Name)
}

// osSymlink creates a symbolic link at link_name pointing at src.
//
// CPython: Modules/posixmodule.c os_symlink_impl
func osSymlink(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: symlink() requires src and dst")
	}
	src, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: symlink() src must be str")
	}
	dst, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: symlink() dst must be str")
	}
	if err := goos.Symlink(src.Value(), dst.Value()); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// osReadlink returns the destination of a symbolic link at path.
//
// CPython: Modules/posixmodule.c os_readlink_impl
func osReadlink(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: readlink() missing path")
	}
	path, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: readlink() path must be str")
	}
	target, err := goos.Readlink(path.Value())
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewStr(target), nil
}

// osLink creates a hard link from src to dst.
//
// CPython: Modules/posixmodule.c os_link_impl
func osLink(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: link() requires src and dst")
	}
	src, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: link() src must be str")
	}
	dst, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: link() dst must be str")
	}
	if err := goos.Link(src.Value(), dst.Value()); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// osExit terminates the process immediately with the given status, skipping
// cleanup. CPython exposes this as os._exit.
//
// CPython: Modules/posixmodule.c os__exit_impl
func osExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: _exit() missing status")
	}
	status, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: _exit() status must be int")
	}
	n, _ := status.Int64()
	goos.Exit(int(n))
	return objects.None(), nil
}

// osUrandom returns n cryptographically random bytes.
//
// CPython: Modules/posixmodule.c os_urandom_impl
func osUrandom(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: urandom() missing size")
	}
	n, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: urandom() size must be int")
	}
	size, _ := n.Int64()
	if size < 0 {
		return nil, fmt.Errorf("ValueError: negative argument not allowed")
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewBytes(buf), nil
}

// osCPUCount returns the number of logical CPUs as reported by Go's
// runtime, mirroring os.cpu_count(). Returns None if unknown (never
// here, but kept for API parity).
//
// CPython: Modules/posixmodule.c os_cpu_count_impl
func osCPUCount(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	n := runtime.NumCPU()
	if n <= 0 {
		return objects.None(), nil
	}
	return objects.NewInt(int64(n)), nil
}

// osIsatty returns True if fd is a tty. The implementation Stats the
// fd through the goos package and tests the char-device bit, which
// matches what `isatty(3)` reports for the common cases _colorize
// cares about.
//
// CPython: Modules/posixmodule.c:11947 os_isatty_impl
func osIsatty(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: isatty() missing required argument: 'fd'")
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	fdVal, _ := fdObj.Int64()
	// The fd is owned by the caller (a live file, socket, or one of the
	// std streams); this wrapper only borrows it to Stat. Clear Go's
	// runtime finalizer so a later GC of the throwaway *os.File never
	// closes a descriptor we do not own. Without this, a GC mid-run
	// closes a borrowed fd and unrelated writes fail with EBADF.
	//
	// CPython: Modules/posixmodule.c:11947 os_isatty_impl borrows the fd
	// and never closes it.
	f := goos.NewFile(uintptr(fdVal), "")
	if f == nil {
		return objects.NewBool(false), nil
	}
	runtime.SetFinalizer(f, nil)
	info, err := f.Stat()
	if err != nil {
		// CPython os.isatty returns False on any error rather than
		// raising; a Stat failure here means the fd is not a real
		// device, which is exactly what callers want to know.
		return objects.NewBool(false), nil //nolint:nilerr // CPython os.isatty parity
	}
	return objects.NewBool((info.Mode() & goos.ModeCharDevice) != 0), nil
}

// osFsdecode decodes filename from the filesystem encoding (utf-8 on
// gopy's posix targets) into a str. str inputs pass through unchanged;
// bytes are decoded directly; anything else goes through __fspath__.
//
// CPython does this through _Py_DecodeLocaleEx with the
// surrogateescape error handler, which gopy doesn't implement yet.
// Plain utf-8 decode is enough for the file paths the gates touch.
//
// CPython: Lib/os.py:863 fsdecode
func osFsdecode(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: fsdecode() takes exactly one argument (%d given)", len(args))
	}
	o, err := fspath(args, nil)
	if err != nil {
		return nil, err
	}
	switch v := o.(type) {
	case *objects.Unicode:
		return v, nil
	case *objects.Bytes:
		return objects.NewStr(string(v.Bytes())), nil
	}
	return nil, fmt.Errorf("TypeError: fsdecode() argument must be str, bytes, or os.PathLike, not %s", o.Type().Name)
}

// osFsencode encodes filename to the filesystem encoding. bytes pass
// through; str is encoded as utf-8; path-likes route through fspath
// first.
//
// CPython: Lib/os.py:851 fsencode
func osFsencode(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: fsencode() takes exactly one argument (%d given)", len(args))
	}
	o, err := fspath(args, nil)
	if err != nil {
		return nil, err
	}
	switch v := o.(type) {
	case *objects.Bytes:
		return v, nil
	case *objects.Unicode:
		return objects.NewBytes([]byte(v.Value())), nil
	}
	return nil, fmt.Errorf("TypeError: fsencode() argument must be str, bytes, or os.PathLike, not %s", o.Type().Name)
}

// osWaitstatusToExitcode converts a wait status returned by waitpid
// into a meaningful exit code. Mirrors WIFEXITED / WIFSIGNALED parity:
// non-negative for normal exit, negative for signal exit. gopy does
// not host child processes yet; subprocess.py imports the symbol at
// module top level so the stub keeps that import alive.
//
// CPython: Modules/posixmodule.c os_waitstatus_to_exitcode_impl
func osWaitstatusToExitcode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var arg objects.Object
	if len(args) == 1 {
		arg = args[0]
	} else if s, ok := kwargs["status"]; ok {
		arg = s
	} else {
		return nil, fmt.Errorf("TypeError: waitstatus_to_exitcode() requires one argument")
	}
	status, ok := arg.(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: waitstatus_to_exitcode() argument must be int, not %s", arg.Type().Name)
	}
	v, _ := status.Int64()
	if v&0x7f == 0 {
		return objects.NewInt((v >> 8) & 0xff), nil
	}
	return objects.NewInt(-(v & 0x7f)), nil
}

// waitStatusArg unwraps the single-int wait status argument the W*
// helpers all share.
func waitStatusArg(args []objects.Object, name string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("TypeError: %s() takes exactly one argument (%d given)", name, len(args))
	}
	i, ok := args[0].(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s() argument must be int, not %s", name, args[0].Type().Name)
	}
	v, _ := i.Int64()
	return v, nil
}

// CPython: Modules/posixmodule.c os_WIFSTOPPED_impl
func osWifstopped(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	v, err := waitStatusArg(args, "WIFSTOPPED")
	if err != nil {
		return nil, err
	}
	return objects.NewBool(v&0xff == 0x7f), nil
}

// CPython: Modules/posixmodule.c os_WIFEXITED_impl
func osWifexited(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	v, err := waitStatusArg(args, "WIFEXITED")
	if err != nil {
		return nil, err
	}
	return objects.NewBool(v&0x7f == 0), nil
}

// CPython: Modules/posixmodule.c os_WIFSIGNALED_impl
func osWifsignaled(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	v, err := waitStatusArg(args, "WIFSIGNALED")
	if err != nil {
		return nil, err
	}
	sig := v & 0x7f
	return objects.NewBool(sig != 0 && sig != 0x7f), nil
}

// CPython: Modules/posixmodule.c os_WEXITSTATUS_impl
func osWexitstatus(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	v, err := waitStatusArg(args, "WEXITSTATUS")
	if err != nil {
		return nil, err
	}
	return objects.NewInt((v >> 8) & 0xff), nil
}

// CPython: Modules/posixmodule.c os_WTERMSIG_impl
func osWtermsig(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	v, err := waitStatusArg(args, "WTERMSIG")
	if err != nil {
		return nil, err
	}
	return objects.NewInt(v & 0x7f), nil
}

// CPython: Modules/posixmodule.c os_WSTOPSIG_impl
func osWstopsig(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	v, err := waitStatusArg(args, "WSTOPSIG")
	if err != nil {
		return nil, err
	}
	return objects.NewInt((v >> 8) & 0xff), nil
}
