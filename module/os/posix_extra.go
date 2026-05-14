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
	path, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: chmod() path must be str")
	}
	mode, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: chmod() mode must be int")
	}
	m, _ := mode.Int64()
	if err := goos.Chmod(path.Value(), goos.FileMode(m)); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
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
