// Windows errno table. CPython's errno module on Windows exposes the
// codes the Universal CRT's <errno.h> defines, with the CRT's small
// POSIX-style values (EEXIST == 17), NOT Go's syscall package, which
// fabricates E* constants as 1<<29+iota on Windows. Hard-code the CRT
// values so errno.EEXIST and friends match CPython exactly and line up
// with the winerror->errno translation the VM applies to OSError.
//
// CPython: Modules/errnomodule.c add_errcode block (values from ucrt
// <errno.h>; the classic 1-42 range plus the POSIX-2008 100-140 range)

package errno

// errnoEntries returns every (name, code) pair the errno module exposes
// on Windows.
//
// CPython: Modules/errnomodule.c:88 errno_exec (Windows slice)
func errnoEntries() []errnoEntry {
	return []errnoEntry{
		{"EPERM", 1},
		{"ENOENT", 2},
		{"ESRCH", 3},
		{"EINTR", 4},
		{"EIO", 5},
		{"ENXIO", 6},
		{"E2BIG", 7},
		{"ENOEXEC", 8},
		{"EBADF", 9},
		{"ECHILD", 10},
		{"EAGAIN", 11},
		{"ENOMEM", 12},
		{"EACCES", 13},
		{"EFAULT", 14},
		{"EBUSY", 16},
		{"EEXIST", 17},
		{"EXDEV", 18},
		{"ENODEV", 19},
		{"ENOTDIR", 20},
		{"EISDIR", 21},
		{"EINVAL", 22},
		{"ENFILE", 23},
		{"EMFILE", 24},
		{"ENOTTY", 25},
		{"EFBIG", 27},
		{"ENOSPC", 28},
		{"ESPIPE", 29},
		{"EROFS", 30},
		{"EMLINK", 31},
		{"EPIPE", 32},
		{"EDOM", 33},
		{"ERANGE", 34},
		{"EDEADLK", 36},
		{"EDEADLOCK", 36},
		{"ENAMETOOLONG", 38},
		{"ENOLCK", 39},
		{"ENOSYS", 40},
		{"ENOTEMPTY", 41},
		{"EILSEQ", 42},
		{"EADDRINUSE", 100},
		{"EADDRNOTAVAIL", 101},
		{"EAFNOSUPPORT", 102},
		{"EALREADY", 103},
		{"EBADMSG", 104},
		{"ECANCELED", 105},
		{"ECONNABORTED", 106},
		{"ECONNREFUSED", 107},
		{"ECONNRESET", 108},
		{"EDESTADDRREQ", 109},
		{"EHOSTUNREACH", 110},
		{"EIDRM", 111},
		{"EINPROGRESS", 112},
		{"EISCONN", 113},
		{"ELOOP", 114},
		{"EMSGSIZE", 115},
		{"ENETDOWN", 116},
		{"ENETRESET", 117},
		{"ENETUNREACH", 118},
		{"ENOBUFS", 119},
		{"ENODATA", 120},
		{"ENOLINK", 121},
		{"ENOMSG", 122},
		{"ENOPROTOOPT", 123},
		{"ENOSR", 124},
		{"ENOSTR", 125},
		{"ENOTCONN", 126},
		{"ENOTRECOVERABLE", 127},
		{"ENOTSOCK", 128},
		{"ENOTSUP", 129},
		{"EOPNOTSUPP", 130},
		{"EOVERFLOW", 132},
		{"EOWNERDEAD", 133},
		{"EPROTO", 134},
		{"EPROTONOSUPPORT", 135},
		{"EPROTOTYPE", 136},
		{"ETIME", 137},
		{"ETIMEDOUT", 138},
		{"ETXTBSY", 139},
		{"EWOULDBLOCK", 140},
	}
}
