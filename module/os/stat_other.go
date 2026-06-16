//go:build !darwin && !freebsd && !linux && !windows

// CPython: Modules/posixmodule.c:3238 os_stat_impl (fallback stub)

package os

import (
	"fmt"
	goos "os"
	"runtime"

	"github.com/tamnd/gopy/objects"
)

// fstatResult stats an open descriptor through a temporary os.File on
// platforms without a syscall.Stat_t. SetFinalizer is cleared on a
// best-effort basis; these fallback targets do not run the kqueue
// netpoller that makes the borrowed-fd close fatal on Darwin.
//
// CPython: Modules/posixmodule.c:3399 os_fstat_impl
func fstatResult(fdVal int64) (*objects.StructSeq, error) {
	f := goos.NewFile(uintptr(fdVal), "")
	runtime.SetFinalizer(f, nil)
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	ino, dev, nlink, uid, gid, atime, ctime := statSysFields(info)
	mtime := info.ModTime().UnixNano()
	blksize, blocks, rdev := statBlockFields(info)
	return newStatResult(statMode(info), int64(ino), int64(dev), int64(nlink), int64(uid), int64(gid), info.Size(), atime, mtime, ctime, blksize, blocks, rdev), nil
}

// statSysFields returns minimal values on unsupported platforms.
// CPython: Modules/posixmodule.c:3238 os_stat_impl
func statSysFields(info goos.FileInfo) (ino, dev, nlink uint64, uid, gid uint32, atime, ctime int64) {
	mtime := info.ModTime().UnixNano()
	return 0, 0, 1, 0, 0, mtime, mtime
}

// statBlockFields returns zeros on unsupported platforms.
// CPython: Modules/posixmodule.c:2521 _pystat_fromstructstat
func statBlockFields(_ goos.FileInfo) (blksize, blocks, rdev int64) {
	return 0, 0, 0
}

// statMode reconstructs a POSIX st_mode from Go's os.FileMode on
// platforms without a syscall.Stat_t.
// CPython: Modules/posixmodule.c:2521 _pystat_fromstructstat (st_mode)
func statMode(info goos.FileInfo) int64 {
	return goFileModeToStMode(info.Mode())
}

// getuid returns 0 on unsupported platforms.
// CPython: Modules/posixmodule.c:9635 os_getuid_impl
func getuid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}

// osPipe, osGetppid, osKill, osWaitpid: stubs for unsupported platforms.
// CPython: Modules/posixmodule.c:8024, 9148, 9162, 9208
func osPipe(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: pipe is not supported on this platform")
}

func osGetppid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: getppid is not supported on this platform")
}

func osKill(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: kill is not supported on this platform")
}

func osWaitpid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError: waitpid is not supported on this platform")
}
