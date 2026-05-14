//go:build !darwin

// _signal is not yet ported for non-POSIX/darwin platforms.
// The stub keeps the package importable so registry.go compiles
// on Linux and Windows CI runners.

package signal
