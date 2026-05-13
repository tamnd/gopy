//go:build windows

package os

// terminalSize returns the (columns, lines) of the controlling terminal.
// On Windows this requires the console API; return a safe fallback for now.
//
// CPython: Modules/posixmodule.c:15358 os_get_terminal_size_impl
func terminalSize() (int, int, error) {
	return 80, 24, nil
}
