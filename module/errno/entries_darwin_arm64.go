//go:build darwin && arm64

package errno

import "syscall"

// darwinArchEntries returns errno names that exist only on darwin/arm64.
// syscall.EQFULL is published in zerrors_darwin_arm64.go but not in the
// amd64 counterpart, so we have to gate it on GOARCH.
func darwinArchEntries() []errnoEntry {
	return []errnoEntry{
		{"EQFULL", int(syscall.EQFULL)},
	}
}
