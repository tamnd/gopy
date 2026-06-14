//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

// exitSigint resets SIGINT to its default disposition and delivers it
// to this process, so an unhandled KeyboardInterrupt terminates the
// interpreter by signal (exit status -SIGINT / 128+SIGINT).
//
// CPython: Modules/main.c:730 exit_sigint
func exitSigint() int {
	signal.Reset(syscall.SIGINT)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		// Impossible in normal environments; fall back to the code
		// CPython returns when the signal could not be delivered.
		return int(syscall.SIGINT) + 128
	}
	// Give the signal a moment to be delivered before falling through.
	select {}
}
