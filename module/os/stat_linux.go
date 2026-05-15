//go:build linux

// CPython: Modules/posixmodule.c:3238 os_stat_impl (Linux atime/ctime extraction)

package os

import (
	"fmt"
	goos "os"
	"syscall"

	"github.com/tamnd/gopy/objects"
)

// statSysFields extracts platform fields from a FileInfo's syscall.Stat_t.
// Linux carries atime/ctime in Atim/Ctim.
// CPython: Modules/posixmodule.c:3238 os_stat_impl
func statSysFields(info goos.FileInfo) (ino, dev, nlink uint64, uid, gid uint32, atime, ctime int64) {
	mtime := info.ModTime().Unix()
	atime = mtime
	ctime = mtime
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok || sys == nil {
		return
	}
	ino = uint64(sys.Ino)
	dev = uint64(sys.Dev)
	nlink = uint64(sys.Nlink)
	uid = sys.Uid
	gid = sys.Gid
	atime = sys.Atim.Sec
	ctime = sys.Ctim.Sec
	return
}

// getuid returns the real user ID of the calling process.
// CPython: Modules/posixmodule.c:9635 os_getuid_impl
func getuid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(syscall.Getuid())), nil
}

// osPipe creates a pipe and returns (read_fd, write_fd).
// CPython: Modules/posixmodule.c:8024 os_pipe_impl
func osPipe(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	fds := make([]int, 2)
	if err := syscall.Pipe(fds); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(fds[0])),
		objects.NewInt(int64(fds[1])),
	}), nil
}

// osGetppid returns the parent process ID.
// CPython: Modules/posixmodule.c:9148 os_getppid_impl
func osGetppid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(syscall.Getppid())), nil
}

// osKill sends signal sig to process pid.
// CPython: Modules/posixmodule.c:9162 os_kill_impl
func osKill(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: kill() requires pid and sig")
	}
	pidObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (pid)")
	}
	sigObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (sig)")
	}
	pidVal, _ := pidObj.Int64()
	sigVal, _ := sigObj.Int64()
	if err := syscall.Kill(int(pidVal), syscall.Signal(sigVal)); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// osWaitpid waits for a child process and returns (pid, status).
// CPython: Modules/posixmodule.c:9208 os_waitpid_impl
func osWaitpid(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: waitpid() requires pid and options")
	}
	pidObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (pid)")
	}
	optsObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (options)")
	}
	pidVal, _ := pidObj.Int64()
	optsVal, _ := optsObj.Int64()
	var ws syscall.WaitStatus
	wpid, err := syscall.Wait4(int(pidVal), &ws, int(optsVal), nil)
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(wpid)),
		objects.NewInt(int64(ws)),
	}), nil
}
