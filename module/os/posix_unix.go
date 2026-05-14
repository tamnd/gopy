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
