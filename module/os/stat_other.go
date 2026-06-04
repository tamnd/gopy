//go:build !darwin && !freebsd && !linux && !windows

// CPython: Modules/posixmodule.c:3238 os_stat_impl (fallback stub)

package os

import (
	"fmt"
	goos "os"

	"github.com/tamnd/gopy/objects"
)

// statSysFields returns minimal values on unsupported platforms.
// CPython: Modules/posixmodule.c:3238 os_stat_impl
func statSysFields(info goos.FileInfo) (ino, dev, nlink uint64, uid, gid uint32, atime, ctime int64) {
	mtime := info.ModTime().Unix()
	return 0, 0, 1, 0, 0, mtime, mtime
}

// statBlockFields returns zeros on unsupported platforms.
// CPython: Modules/posixmodule.c:2521 _pystat_fromstructstat
func statBlockFields(_ goos.FileInfo) (blksize, blocks, rdev int64) {
	return 0, 0, 0
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
