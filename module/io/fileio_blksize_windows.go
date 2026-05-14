//go:build windows

// statBlksizeOf is a no-op on Windows; the _blksize getter falls back
// to DEFAULT_BUFFER_SIZE.
//
// CPython: Modules/_io/fileio.c:1292 fileio_get_blksize

package io

import stdos "os"

func statBlksizeOf(_ stdos.FileInfo) int64 {
	return 0
}
