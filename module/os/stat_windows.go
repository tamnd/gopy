//go:build windows

// CPython: Modules/posixmodule.c:3238 os_stat_impl (Windows branch via win32_stat)

package os

import (
	goos "os"
	"syscall"

	"github.com/tamnd/gopy/objects"
)

// statSysFields extracts platform fields from a Windows FileInfo's
// Win32FileAttributeData. Windows reports CreationTime / LastAccessTime
// / LastWriteTime as FILETIME (100-ns intervals since 1601-01-01); we
// publish those as ctime / atime / mtime, matching the win32_stat
// branch in posixmodule.c. Inode, device, uid and gid are not surfaced
// by FindFirstFile; CPython fills them from a separate
// GetFileInformationByHandle round-trip, which we skip here.
//
// CPython: Modules/posixmodule.c:1924 win32_stat
func statSysFields(info goos.FileInfo) (ino, dev, nlink uint64, uid, gid uint32, atime, ctime int64) {
	mtime := info.ModTime().UnixNano()
	atime = mtime
	ctime = mtime
	nlink = 1
	sys, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || sys == nil {
		return
	}
	atime = sys.LastAccessTime.Nanoseconds()
	ctime = sys.CreationTime.Nanoseconds()
	return
}

// statBlockFields returns zeros on Windows; FindFirstFile does not
// surface block size / block count / rdev.
// CPython: Modules/posixmodule.c:2521 _pystat_fromstructstat
func statBlockFields(_ goos.FileInfo) (blksize, blocks, rdev int64) {
	return 0, 0, 0
}

// statMode synthesizes a POSIX st_mode from Go's os.FileMode. Windows
// has no inode mode; CPython's win32 stat builds one from the file
// attributes the same way, so the S_IF* nibble and permission bits land
// where stat.S_ISDIR / S_ISREG expect them.
// CPython: Modules/posixmodule.c:1862 attributes_to_mode
func statMode(info goos.FileInfo) int64 {
	return goFileModeToStMode(info.Mode())
}

// getuid returns 0 on Windows. CPython does not expose os.getuid on
// Windows at all (the attribute is absent); we return 0 to keep the
// shared module.go entry table simple. Callers wanting strict CPython
// parity should treat its presence on Windows as a gopy extension.
//
// CPython: Modules/posixmodule.c:9635 os_getuid_impl (POSIX-only)
func getuid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(0), nil
}
