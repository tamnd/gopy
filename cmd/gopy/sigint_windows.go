//go:build windows

package main

// exitSigint mirrors the Windows branch of CPython's exit_sigint: there is
// no POSIX kill(getpid, SIGINT), so the interpreter exits with SIGINT+128
// (the value CPython returns when raise(SIGINT) does not abort the process).
//
// CPython: Modules/main.c:730 exit_sigint
func exitSigint() int {
	// SIGINT is 2 on Windows; 2 + 128 = 130.
	return 2 + 128
}
