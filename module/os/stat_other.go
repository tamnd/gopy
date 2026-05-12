//go:build !darwin && !freebsd && !linux && !windows

// CPython: Modules/posixmodule.c:3238 os_stat_impl (fallback stub)

package os

import (
	goos "os"

	"github.com/tamnd/gopy/objects"
)

// statSysFields returns minimal values on unsupported platforms.
// CPython: Modules/posixmodule.c:3238 os_stat_impl
func statSysFields(info goos.FileInfo) (ino, dev, nlink uint64, uid, gid uint32, atime, ctime int64) {
	mtime := info.ModTime().Unix()
	return 0, 0, 1, 0, 0, mtime, mtime
}

// getuid returns 0 on unsupported platforms.
// CPython: Modules/posixmodule.c:9635 os_getuid_impl
func getuid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}
