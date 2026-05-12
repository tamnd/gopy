//go:build linux

// CPython: Modules/posixmodule.c:3238 os_stat_impl (Linux atime/ctime extraction)

package os

import (
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
	ino = sys.Ino
	dev = sys.Dev
	nlink = sys.Nlink
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
