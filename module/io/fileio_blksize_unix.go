//go:build !windows

// statBlksizeOf extracts st_blksize from a Unix stat result. Falls back
// to 0 when the syscall stat isn't available, which makes the _blksize
// getter use DEFAULT_BUFFER_SIZE.
//
// CPython: Modules/_io/fileio.c:1292 fileio_get_blksize

package io

import (
	stdos "os"
	"syscall"
)

func statBlksizeOf(info stdos.FileInfo) int64 {
	if sys, ok := info.Sys().(*syscall.Stat_t); ok && sys != nil {
		return int64(sys.Blksize)
	}
	return 0
}
