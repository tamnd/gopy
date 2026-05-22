//go:build windows

// Package selectmod stub for Windows. CPython routes select through
// Winsock (WSAEventSelect / WSAsocketSelect); that arm of
// selectmodule.c lives behind the MS_WINDOWS guards and has not been
// ported yet. The package compiles empty here so the rest of gopy
// builds on Windows; `import select` raises ImportError because
// nothing registers the inittab entry, matching CPython's behavior
// when the module is excluded at build time.
//
// CPython: Modules/selectmodule.c MS_WINDOWS branches
package selectmod
