// Windows errno table. The Windows C runtime defines a smaller subset
// of POSIX E* codes than Linux; this list mirrors the ones exposed by
// Go's syscall package on windows/amd64.
//
// CPython: Modules/errnomodule.c:121 add_errcode block (MS_WINDOWS arms)

package errno

import "syscall"

// errnoEntries returns every (name, code) pair the errno module exposes
// on Windows.
//
// CPython: Modules/errnomodule.c:88 errno_exec (Windows slice)
func errnoEntries() []errnoEntry {
	return []errnoEntry{
		{"EPERM", int(syscall.EPERM)},
		{"ENOENT", int(syscall.ENOENT)},
		{"ESRCH", int(syscall.ESRCH)},
		{"EINTR", int(syscall.EINTR)},
		{"EIO", int(syscall.EIO)},
		{"ENXIO", int(syscall.ENXIO)},
		{"E2BIG", int(syscall.E2BIG)},
		{"ENOEXEC", int(syscall.ENOEXEC)},
		{"EBADF", int(syscall.EBADF)},
		{"ECHILD", int(syscall.ECHILD)},
		{"EAGAIN", int(syscall.EAGAIN)},
		{"ENOMEM", int(syscall.ENOMEM)},
		{"EACCES", int(syscall.EACCES)},
		{"EFAULT", int(syscall.EFAULT)},
		{"EBUSY", int(syscall.EBUSY)},
		{"EEXIST", int(syscall.EEXIST)},
		{"EXDEV", int(syscall.EXDEV)},
		{"ENODEV", int(syscall.ENODEV)},
		{"ENOTDIR", int(syscall.ENOTDIR)},
		{"EISDIR", int(syscall.EISDIR)},
		{"EINVAL", int(syscall.EINVAL)},
		{"ENFILE", int(syscall.ENFILE)},
		{"EMFILE", int(syscall.EMFILE)},
		{"ENOTTY", int(syscall.ENOTTY)},
		{"EFBIG", int(syscall.EFBIG)},
		{"ENOSPC", int(syscall.ENOSPC)},
		{"ESPIPE", int(syscall.ESPIPE)},
		{"EROFS", int(syscall.EROFS)},
		{"EMLINK", int(syscall.EMLINK)},
		{"EPIPE", int(syscall.EPIPE)},
		{"EDOM", int(syscall.EDOM)},
		{"ERANGE", int(syscall.ERANGE)},
		{"EDEADLK", int(syscall.EDEADLK)},
		{"ENAMETOOLONG", int(syscall.ENAMETOOLONG)},
		{"ENOLCK", int(syscall.ENOLCK)},
		{"ENOSYS", int(syscall.ENOSYS)},
		{"ENOTEMPTY", int(syscall.ENOTEMPTY)},
		{"EILSEQ", int(syscall.EILSEQ)},
	}
}
