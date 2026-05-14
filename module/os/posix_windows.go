//go:build windows

// Windows implementations of low-level fd operations. Windows uses
// syscall.Handle for file descriptors rather than plain int.
//
// CPython: Modules/posixmodule.c (os_close_impl, os_read_impl, os_write_impl,
// os_lseek_impl, os_dup_impl, os_pipe_impl, os_getppid_impl, os_kill_impl,
// os_waitpid_impl)

package os

import (
	"fmt"
	goos "os"
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
	if err := syscall.Close(syscall.Handle(fdVal)); err != nil {
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
	var n uint32
	if err := syscall.ReadFile(syscall.Handle(fdVal), buf, &n, nil); err != nil {
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
	var n uint32
	if err := syscall.WriteFile(syscall.Handle(fdVal), data, &n, nil); err != nil {
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
	off, err := syscall.Seek(syscall.Handle(fdVal), posVal, int(howVal))
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewInt(off), nil
}

// osDup duplicates file descriptor fd using DuplicateHandle.
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
	proc := syscall.Handle(^uintptr(0)) // GetCurrentProcess pseudo-handle
	var newHandle syscall.Handle
	if err := syscall.DuplicateHandle(proc, syscall.Handle(fdVal), proc, &newHandle, 0, false, syscall.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewInt(int64(newHandle)), nil
}

// osPipe creates a pipe and returns (read_fd, write_fd).
//
// CPython: Modules/posixmodule.c:8024 os_pipe_impl
func osPipe(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	var r, w syscall.Handle
	if err := syscall.CreatePipe(&r, &w, nil, 0); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(r)),
		objects.NewInt(int64(w)),
	}), nil
}

// osGetppid returns the parent process ID.
// Windows does not expose getppid natively; raise NotImplementedError.
//
// CPython: Modules/posixmodule.c:9148 os_getppid_impl
func osGetppid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: getppid is not supported on Windows")
}

// osKill sends a termination signal to process pid.
// On Windows, only SIGTERM (15) causes process termination via TerminateProcess.
//
// CPython: Modules/posixmodule.c:9162 os_kill_impl
func osKill(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: kill() requires pid and sig")
	}
	pidObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (pid)")
	}
	pidVal, _ := pidObj.Int64()
	proc, err := goos.FindProcess(int(pidVal))
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	if err := proc.Kill(); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// osWaitpid waits for a child process and returns (pid, exitcode).
// Uses WaitForSingleObject on the process handle.
//
// CPython: Modules/posixmodule.c:9208 os_waitpid_impl
func osWaitpid(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: waitpid() requires pid and options")
	}
	pidObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (pid)")
	}
	pidVal, _ := pidObj.Int64()
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION|syscall.SYNCHRONIZE, false, uint32(pidVal))
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	defer syscall.CloseHandle(handle)
	if _, err := syscall.WaitForSingleObject(handle, syscall.INFINITE); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewTuple([]objects.Object{
		objects.NewInt(pidVal),
		objects.NewInt(int64(exitCode)),
	}), nil
}

// osGeteuid is not available on Windows; returns 0 to match CPython where
// posix_geteuid is gated on HAVE_GETEUID.
func osGeteuid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}

func osGetegid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}

func osGetgid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}

func osGetgroups(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewList(nil), nil
}

// osUmask returns 0; Windows has no umask equivalent.
func osUmask(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: umask() missing mask")
	}
	return objects.NewInt(0), nil
}
