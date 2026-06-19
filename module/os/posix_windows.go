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
	"unsafe"

	"github.com/tamnd/gopy/objects"
)

// procGetHandleInformation resolves kernel32!GetHandleInformation, which
// the std syscall package does not export (only SetHandleInformation).
var procGetHandleInformation = syscall.NewLazyDLL("kernel32.dll").NewProc("GetHandleInformation")

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
	case *objects.MemoryView:
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

// osGetInheritable returns whether the handle will be inherited by
// child processes. On Windows that is the HANDLE_FLAG_INHERIT bit read
// off GetHandleInformation.
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
	var flags uint32
	// The std syscall package exports SetHandleInformation but not the
	// Get half, so GetHandleInformation is resolved from kernel32 directly.
	r, _, callErr := procGetHandleInformation.Call(uintptr(syscall.Handle(fdVal)), uintptr(unsafe.Pointer(&flags)))
	if r == 0 {
		return nil, fmt.Errorf("OSError: %w", callErr)
	}
	return objects.NewBool(flags&syscall.HANDLE_FLAG_INHERIT != 0), nil
}

// osSetInheritable sets or clears the handle's inheritable flag through
// SetHandleInformation.
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
	var flag uint32
	if objects.IsTrue(args[1]) {
		flag = syscall.HANDLE_FLAG_INHERIT
	}
	if err := syscall.SetHandleInformation(syscall.Handle(fdVal), syscall.HANDLE_FLAG_INHERIT, flag); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
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

// posixIdentityEntries returns no entries on Windows: CPython's
// posixmodule.c omits geteuid / getegid / getgid / getgroups when the
// platform lacks HAVE_GETEUID, so `os.geteuid` raises AttributeError.
//
// CPython: Modules/posixmodule.c HAVE_GETEUID block
func posixIdentityEntries() []struct {
	name string
	val  objects.Object
} {
	return nil
}

// osUmask returns 0; Windows has no umask equivalent.
func osUmask(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: umask() missing mask")
	}
	return objects.NewInt(0), nil
}

// winPathEntries returns the Windows-only path helpers posixmodule.c registers
// inside its #ifdef MS_WINDOWS block. Only _path_splitroot is needed by the
// stdlib bootstrap; the rest of the listdrives/_path_* family is unported.
//
// CPython: Modules/posixmodule.c:4707 #ifdef MS_WINDOWS
func winPathEntries() []struct {
	name string
	val  objects.Object
} {
	return []struct {
		name string
		val  objects.Object
	}{
		{"_path_splitroot", objects.NewBuiltinFunction("_path_splitroot", osPathSplitroot)},
	}
}

// osPathSplitroot splits a Windows path into (root, rest), where root is
// everything up to and including the leading separator after a drive or UNC
// share. importlib._bootstrap_external uses it to reimplement os.path.join and
// os.path.isabs without importing ntpath at bootstrap time.
//
// The C accelerator runs PathCchSkipRoot over a copy with forward slashes
// folded to backslashes, then slices the original (unfolded) path at the root
// length. That is exactly the drive+root prefix ntpath.splitroot computes, so
// this port follows the ntpath.splitroot algorithm and joins its (drive, root)
// halves into the single root element the 2-tuple form returns.
//
// CPython: Modules/posixmodule.c:5230 os__path_splitroot_impl
// CPython: Lib/ntpath.py:172 splitroot
func osPathSplitroot(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _path_splitroot() takes exactly one argument (%d given)", len(args))
	}
	s, err := objects.Str(args[0])
	if err != nil {
		return nil, err
	}
	root, rest := splitrootWindows(s)
	return objects.NewTuple([]objects.Object{objects.NewStr(root), objects.NewStr(rest)}), nil
}

const (
	srSep   = '\\'
	srAlt   = '/'
	srColon = ':'
)

// srAt reads the slash-folded rune at index i (out-of-range yields 0), so the
// structural tests run over the normp = p.replace('/', '\\') view.
func srAt(r []rune, i int) rune {
	if i < 0 || i >= len(r) {
		return 0
	}
	if r[i] == srAlt {
		return srSep
	}
	return r[i]
}

// srSlice returns string(r[a:b]) clamped to bounds, the Python p[a:b] slice.
func srSlice(r []rune, a, b int) string {
	if a < 0 {
		a = 0
	}
	if b > len(r) {
		b = len(r)
	}
	if a >= b {
		return ""
	}
	return string(r[a:b])
}

// srFindSep is normp.find('\\', start) over the slash-folded view.
func srFindSep(r []rune, start int) int {
	for i := start; i < len(r); i++ {
		if srAt(r, i) == srSep {
			return i
		}
	}
	return -1
}

// srHasUNCPrefix reports normp[:8].upper() == '\\\\?\\UNC\\'.
func srHasUNCPrefix(r []rune) bool {
	want := [8]rune{srSep, srSep, '?', srSep, 'U', 'N', 'C', srSep}
	if len(r) < 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		c := srAt(r, i)
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c != want[i] {
			return false
		}
	}
	return true
}

// srSplitUNC handles \\server\share or \\?\UNC\server\share roots.
func srSplitUNC(p string, r []rune) (string, string) {
	start := 2
	if srHasUNCPrefix(r) {
		start = 8
	}
	index := srFindSep(r, start)
	if index == -1 {
		return p, ""
	}
	index2 := srFindSep(r, index+1)
	if index2 == -1 {
		return p, ""
	}
	// drive=p[:index2], root=p[index2:index2+1], tail=p[index2+1:].
	return srSlice(r, 0, index2+1), srSlice(r, index2+1, len(r))
}

// splitrootWindows is the ntpath.splitroot algorithm folded to the 2-tuple
// (drive+root, tail) shape os._path_splitroot returns. It indexes by rune to
// preserve Python str (code-point) slicing semantics.
//
// CPython: Lib/ntpath.py:172 splitroot
func splitrootWindows(p string) (root, tail string) {
	r := []rune(p)
	switch {
	case srAt(r, 0) == srSep:
		if srAt(r, 1) == srSep {
			return srSplitUNC(p, r)
		}
		// Relative path with root, e.g. \Windows: drive="", root=p[:1].
		return srSlice(r, 0, 1), srSlice(r, 1, len(r))
	case srAt(r, 1) == srColon:
		if srAt(r, 2) == srSep {
			// Absolute drive-letter path, e.g. X:\Windows.
			return srSlice(r, 0, 3), srSlice(r, 3, len(r))
		}
		// Relative path with drive, e.g. X:Windows: drive=p[:2], root="".
		return srSlice(r, 0, 2), srSlice(r, 2, len(r))
	default:
		// Relative path, e.g. Windows.
		return "", p
	}
}
