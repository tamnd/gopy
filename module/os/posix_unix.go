//go:build !windows

// Low-level POSIX file-descriptor operations. Windows uses Handle-based
// syscalls with different types; those live in posix_windows.go.
//
// CPython: Modules/posixmodule.c (os_close_impl, os_read_impl, os_write_impl,
// os_lseek_impl, os_dup_impl)

package os

import (
	"fmt"
	"syscall"

	"github.com/tamnd/gopy/objects"
)

// osClose closes a file descriptor.
//
// CPython: Modules/posixmodule.c:3730 os_close_impl
func osClose(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: close() missing required argument: 'fd'")
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	fdVal, _ := fdObj.Int64()
	if err := syscall.Close(int(fdVal)); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// osRead reads at most n bytes from file descriptor fd.
//
// CPython: Modules/posixmodule.c:7842 os_read_impl
func osRead(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: read() requires fd and n")
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (fd)")
	}
	nObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (n)")
	}
	fdVal, _ := fdObj.Int64()
	nVal, _ := nObj.Int64()
	if nVal < 0 {
		return nil, fmt.Errorf("ValueError: read length must be non-negative")
	}
	buf := make([]byte, nVal)
	n, err := syscall.Read(int(fdVal), buf)
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewBytes(buf[:n]), nil
}

// osWrite writes data to file descriptor fd.
//
// CPython: Modules/posixmodule.c:7879 os_write_impl
func osWrite(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: write() requires fd and data")
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (fd)")
	}
	var data []byte
	switch v := args[1].(type) {
	case *objects.Bytes:
		data = v.Bytes()
	case *objects.ByteArray:
		data = v.Bytes()
	case *objects.MemoryView:
		data = v.Bytes()
	default:
		return nil, fmt.Errorf("TypeError: a bytes-like object is required, not %s", args[1].Type().Name)
	}
	fdVal, _ := fdObj.Int64()
	n, err := syscall.Write(int(fdVal), data)
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewInt(int64(n)), nil
}

// osLseek repositions the file offset of fd to position according to how.
//
// CPython: Modules/posixmodule.c:7913 os_lseek_impl
func osLseek(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: lseek() requires fd, position, and how")
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (fd)")
	}
	posObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (position)")
	}
	howObj, ok := args[2].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (how)")
	}
	fdVal, _ := fdObj.Int64()
	posVal, _ := posObj.Int64()
	howVal, _ := howObj.Int64()
	off, err := syscall.Seek(int(fdVal), posVal, int(howVal))
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewInt(off), nil
}

// osGeteuid returns the current process's effective user ID.
//
// CPython: Modules/posixmodule.c os_geteuid_impl
func osGeteuid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(syscall.Geteuid())), nil
}

// osGetegid returns the current process's effective group ID.
//
// CPython: Modules/posixmodule.c os_getegid_impl
func osGetegid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(syscall.Getegid())), nil
}

// osGetgid returns the current process's real group ID.
//
// CPython: Modules/posixmodule.c os_getgid_impl
func osGetgid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(syscall.Getgid())), nil
}

// osGetgroups returns the supplementary group IDs of the current process.
//
// CPython: Modules/posixmodule.c os_getgroups_impl
func osGetgroups(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	gids, err := syscall.Getgroups()
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	items := make([]objects.Object, len(gids))
	for i, g := range gids {
		items[i] = objects.NewInt(int64(g))
	}
	return objects.NewList(items), nil
}

// osUmask sets the process file mode creation mask to mask and returns the
// previous value.
//
// CPython: Modules/posixmodule.c os_umask_impl
func osUmask(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: umask() missing mask")
	}
	m, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: umask() mask must be int")
	}
	v, _ := m.Int64()
	prev := syscall.Umask(int(v))
	return objects.NewInt(int64(prev)), nil
}

// osDup duplicates file descriptor fd.
//
// CPython: Modules/posixmodule.c:7965 os_dup_impl
func osDup(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: dup() missing required argument: 'fd'")
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	fdVal, _ := fdObj.Int64()
	newfd, err := syscall.Dup(int(fdVal))
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewInt(int64(newfd)), nil
}

// osGetInheritable returns whether the file descriptor will be
// inherited by child processes. On POSIX a descriptor is inheritable
// exactly when FD_CLOEXEC is clear, so the result is read off
// fcntl(fd, F_GETFD).
//
// CPython: Modules/posixmodule.c:9531 os_get_inheritable_impl
func osGetInheritable(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: get_inheritable() missing required argument: 'fd'")
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	fdVal, _ := fdObj.Int64()
	flags, err := fcntlGetFD(int(fdVal))
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewBool(flags&syscall.FD_CLOEXEC == 0), nil
}

// osSetInheritable sets or clears the descriptor's inheritable flag by
// toggling FD_CLOEXEC through fcntl.
//
// CPython: Modules/posixmodule.c:9554 os_set_inheritable_impl
func osSetInheritable(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: set_inheritable() takes exactly 2 arguments (%d given)", len(args))
	}
	fdObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	fdVal, _ := fdObj.Int64()
	flags, err := fcntlGetFD(int(fdVal))
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	if objects.IsTrue(args[1]) {
		flags &^= syscall.FD_CLOEXEC
	} else {
		flags |= syscall.FD_CLOEXEC
	}
	if err := fcntlSetFD(int(fdVal), flags); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// fcntlGetFD reads the F_GETFD descriptor flags. The std syscall
// package does not export FcntlInt on every GOOS, so the raw fcntl
// syscall is invoked directly.
func fcntlGetFD(fd int) (int, error) {
	r, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

// fcntlSetFD writes the F_SETFD descriptor flags.
func fcntlSetFD(fd, flags int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFD), uintptr(flags))
	if errno != 0 {
		return errno
	}
	return nil
}

// posixIdentityEntries returns the geteuid / getegid / getgid /
// getgroups bindings. These exist only on builds with HAVE_GETEUID.
//
// CPython: Modules/posixmodule.c HAVE_GETEUID block
func posixIdentityEntries() []struct {
	name string
	val  objects.Object
} {
	return []struct {
		name string
		val  objects.Object
	}{
		{"geteuid", objects.NewBuiltinFunction("geteuid", osGeteuid)},
		{"getegid", objects.NewBuiltinFunction("getegid", osGetegid)},
		{"getgid", objects.NewBuiltinFunction("getgid", osGetgid)},
		{"getgroups", objects.NewBuiltinFunction("getgroups", osGetgroups)},
	}
}
