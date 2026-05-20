// Fast goid: read the current goroutine's id via the dedicated g
// register + a known field offset, instead of formatting the goroutine
// stack to a string and parsing the prefix. The slow path costs ~4us
// per call (76% of EvalCode time on tight benchmarks); the fast path
// is a single load and amortizes to free.
//
// The g struct's goid field offset changes between Go releases. To
// stay version-safe we discover the offset at init by spawning a tiny
// helper goroutine, taking its goid via the slow path, and scanning
// the g struct for the matching uint64. If we cannot find a unique
// match we fall back to slow goid; correctness is preserved either
// way.
//
// CPython: Include/internal/pycore_pystate.h _PyThreadState_GET reads
// _PyRuntime.gilstate.tstate_current via TLS; this is the Go-runtime
// equivalent (read the g register, then read goid out of the g struct).

package vm

import (
	"runtime"
	"strconv"
	"sync"
	"unsafe"
)

// getg returns the running goroutine's *g pointer. Implemented in
// goid_<arch>.s; on unsupported arches the build falls back to slow
// goid via the var-init below.
func getg() unsafe.Pointer

// goidOffset is the byte offset of g.goid inside the runtime g struct
// for this build of Go. Discovered once at init; 0 means "use slow
// path".
var goidOffset uintptr

// goidOffsetOnce protects discovery so the init helper only runs on
// the first call and a concurrent process startup cannot race.
var goidOffsetOnce sync.Once

// slowGoid is the original goroutine.Stack-based implementation. Kept
// as the fallback for unsupported arches and as the cross-check used
// to discover goidOffset.
func slowGoid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	const prefix = "goroutine "
	if n <= len(prefix) {
		return 0
	}
	s := buf[len(prefix):n]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	id, _ := strconv.ParseUint(string(s[:end]), 10, 64)
	return id
}

// discoverGoidOffset scans the g struct for the field matching the
// slow-path goid value. We scan from offset 120 (past the largest
// reasonable preamble) to 400 in uint64 strides; the g struct's
// goid field has lived in this range across every Go release that
// matters.
func discoverGoidOffset() {
	want := slowGoid()
	gp := getg()
	if gp == nil {
		return
	}
	for off := uintptr(120); off <= 400; off += 8 {
		if *(*uint64)(unsafe.Pointer(uintptr(gp) + off)) == want {
			// Sanity: read again on a freshly spawned goroutine and
			// confirm the field matches its slow-path goid too.
			ok := make(chan bool, 1)
			go func() {
				w := slowGoid()
				g2 := getg()
				ok <- (*(*uint64)(unsafe.Pointer(uintptr(g2) + off)) == w)
			}()
			if <-ok {
				goidOffset = off
				return
			}
		}
	}
}

func init() {
	goidOffsetOnce.Do(discoverGoidOffset)
}

// goid is the in-loop fast variant. Reads the g register and loads
// goid directly. Falls back to the slow path when offset discovery
// did not converge (e.g. a future Go release that moved the field
// further than the scan window).
func goid() uint64 {
	if goidOffset == 0 {
		return slowGoid()
	}
	return *(*uint64)(unsafe.Pointer(uintptr(getg()) + goidOffset))
}
