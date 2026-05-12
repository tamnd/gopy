//go:build windows

// CPython: Modules/posixmodule.c:3238 os_stat_impl (Windows stub)

package os

import (
	goos "os"

	"github.com/tamnd/gopy/objects"
)

// statSysFields returns zero values on Windows; the full os.stat_result
// fields that require a Unix Stat_t are unavailable.
// CPython: Modules/posixmodule.c:3238 os_stat_impl
func statSysFields(info goos.FileInfo) (ino, dev, nlink uint64, uid, gid uint32, atime, ctime int64) {
	mtime := info.ModTime().Unix()
	return 0, 0, 1, 0, 0, mtime, mtime
}

// getuid returns 0 on Windows (no Unix UID concept).
// CPython: Modules/posixmodule.c:9635 os_getuid_impl
func getuid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}
