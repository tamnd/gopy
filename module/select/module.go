//go:build unix

// Package selectmod is the gopy port of CPython's Modules/selectmodule.c.
// It exposes the select.select primitive that selectors.SelectSelector
// and subprocess.Popen._communicate depend on. Only the select function
// is wired today; poll/epoll/kqueue belong to follow-up phases.
//
// CPython: Modules/selectmodule.c:1 selectmodule
//
// The Go module name is selectmod (not "select") because "select" is a
// reserved Go keyword. The Python-visible name installed in the inittab
// is "select" so `import select` resolves to this module.
//
// The unix build tag gates the POSIX select(2) implementation. Windows
// drives select(2) through Winsock's WSAEventSelect, which is a
// separate CPython arm (Modules/selectmodule.c MS_WINDOWS branches);
// that port lands in a follow-up. Until then `import select` on
// Windows raises ImportError, matching CPython's behavior when the
// module is excluded from the build.
package selectmod

import (
	"fmt"
	"math"
	"syscall"
	"time"
	"unsafe"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("select", buildModule)
}

// fdSetBytes returns the bit storage of an FdSet as a portable byte
// slice so FD_SET / FD_ISSET can be implemented without per-arch
// branches on the Bits field's element type.
func fdSetBytes(s *syscall.FdSet) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(s)), unsafe.Sizeof(*s))
}

// fdSetSize returns the maximum fd value the FdSet can hold.
//
// CPython: include/sys/select.h FD_SETSIZE
func fdSetSize() int { return int(unsafe.Sizeof(syscall.FdSet{}) * 8) }

// fdSet mirrors the FD_SET macro.
//
// CPython: Modules/selectmodule.c:181 FD_SET(v, set)
func fdSet(s *syscall.FdSet, fd int) {
	b := fdSetBytes(s)
	b[fd/8] |= 1 << uint(fd%8)
}

// fdIsSet mirrors the FD_ISSET macro.
//
// CPython: Modules/selectmodule.c:212 FD_ISSET
func fdIsSet(s *syscall.FdSet, fd int) bool {
	b := fdSetBytes(s)
	return b[fd/8]&(1<<uint(fd%8)) != 0
}

// fdEntry pairs a Python object with the underlying fd extracted from
// it. set2list rebuilds the result list by walking the original entry
// table so callers get back the same objects they passed in.
//
// CPython: Modules/selectmodule.c:108 pylist
type fdEntry struct {
	obj objects.Object
	fd  int
}

// seq2set fills an FdSet from a Python iterable, mirroring
// CPython's seq2set. It returns the slice of objects (in iteration
// order) and the largest fd (for the nfds argument of select).
//
// CPython: Modules/selectmodule.c:142 seq2set
func seq2set(seq objects.Object, set *syscall.FdSet) ([]fdEntry, int, error) {
	*set = syscall.FdSet{}
	items, err := objects.IterToSlice(seq)
	if err != nil {
		return nil, -1, err
	}
	maxFd := -1
	entries := make([]fdEntry, 0, len(items))
	limit := fdSetSize()
	for _, o := range items {
		fd, ferr := asFileDescriptor(o)
		if ferr != nil {
			return nil, -1, ferr
		}
		if fd < 0 || fd >= limit {
			return nil, -1, fmt.Errorf(
				"ValueError: filedescriptor out of range in select()")
		}
		fdSet(set, fd)
		entries = append(entries, fdEntry{obj: o, fd: fd})
		if fd > maxFd {
			maxFd = fd
		}
	}
	return entries, maxFd, nil
}

// asFileDescriptor extracts an integer fd from o by first checking for
// int and then falling back to fileno().
//
// CPython: Objects/fileobject.c:104 PyObject_AsFileDescriptor
func asFileDescriptor(o objects.Object) (int, error) {
	if i, ok := o.(*objects.Int); ok {
		v, _ := i.Int64()
		return int(v), nil
	}
	fn, err := objects.GetAttr(o, objects.NewStr("fileno"))
	if err != nil {
		return -1, fmt.Errorf(
			"TypeError: argument must be an int, or have a fileno() method")
	}
	res, err := objects.Call(fn, objects.NewTuple(nil), nil)
	if err != nil {
		return -1, err
	}
	i, ok := res.(*objects.Int)
	if !ok {
		return -1, fmt.Errorf(
			"TypeError: fileno() returned a non-integer")
	}
	v, _ := i.Int64()
	if v < 0 {
		return -1, fmt.Errorf("ValueError: file descriptor cannot be a negative integer (%d)", v)
	}
	return int(v), nil
}

// set2list rebuilds a Python list of objects ready for I/O from the
// entries collected by seq2set.
//
// CPython: Modules/selectmodule.c:205 set2list
func set2list(entries []fdEntry, set *syscall.FdSet) *objects.List {
	out := objects.NewList(nil)
	for _, e := range entries {
		if fdIsSet(set, e.fd) {
			out.Append(e.obj)
		}
	}
	return out
}

// selectSelect implements select.select(rlist, wlist, xlist, timeout=None).
//
// CPython: Modules/selectmodule.c:277 select_select_impl
func selectSelect(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, fmt.Errorf(
			"TypeError: select() takes 3 or 4 arguments (%d given)", len(args))
	}
	var tvPtr *syscall.Timeval
	if len(args) == 4 && args[3] != nil && args[3] != objects.None() {
		secs, err := timeoutSeconds(args[3])
		if err != nil {
			return nil, err
		}
		if secs < 0 {
			return nil, fmt.Errorf("ValueError: timeout must be non-negative")
		}
		tv := durationToTimeval(time.Duration(secs * float64(time.Second)))
		tvPtr = &tv
	}

	var rset, wset, eset syscall.FdSet
	rEntries, rMax, err := seq2set(args[0], &rset)
	if err != nil {
		return nil, err
	}
	wEntries, wMax, err := seq2set(args[1], &wset)
	if err != nil {
		return nil, err
	}
	eEntries, eMax, err := seq2set(args[2], &eset)
	if err != nil {
		return nil, err
	}
	nfds := rMax
	if wMax > nfds {
		nfds = wMax
	}
	if eMax > nfds {
		nfds = eMax
	}

	var rArg, wArg, eArg *syscall.FdSet
	if len(rEntries) > 0 {
		rArg = &rset
	}
	if len(wEntries) > 0 {
		wArg = &wset
	}
	if len(eEntries) > 0 {
		eArg = &eset
	}

	for {
		// CPython: Modules/selectmodule.c:359 select(...)
		// doSelect wraps the per-OS syscall.Select signature (Linux
		// returns (n, err); BSD/darwin returns err only) into one
		// shape this loop can consume.
		serr := doSelect(nfds+1, rArg, wArg, eArg, tvPtr)
		if serr == nil {
			break
		}
		if serr == syscall.EINTR {
			// CPython: Modules/selectmodule.c:367 EINTR retry
			continue
		}
		return nil, fmt.Errorf("OSError: %v", serr)
	}

	rOut := set2list(rEntries, &rset)
	wOut := set2list(wEntries, &wset)
	eOut := set2list(eEntries, &eset)
	return objects.NewTuple([]objects.Object{rOut, wOut, eOut}), nil
}

// timeoutSeconds converts a Python timeout to seconds.
//
// CPython: Modules/selectmodule.c:301 _PyTime_FromSecondsObject
func timeoutSeconds(o objects.Object) (float64, error) {
	switch v := o.(type) {
	case *objects.Int:
		n, _ := v.Int64()
		return float64(n), nil
	case *objects.Float:
		f := v.Float64()
		if math.IsNaN(f) {
			return 0, fmt.Errorf("ValueError: timeout must not be NaN")
		}
		return f, nil
	}
	return 0, fmt.Errorf("TypeError: timeout must be a float or None")
}

// durationToTimeval converts a Go duration to a syscall.Timeval.
// syscall.NsecToTimeval handles the per-OS int32/int64 Usec field
// (Linux Timeval.Usec is int64, macOS is int32) without manual casts.
func durationToTimeval(d time.Duration) syscall.Timeval {
	if d < 0 {
		d = 0
	}
	return syscall.NsecToTimeval(d.Nanoseconds())
}

// buildModule constructs the select module dict and stamps the
// canonical PIPE_BUF / POLL* constants that subprocess and selectors
// import unconditionally.
//
// CPython: Modules/selectmodule.c:2855 select_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("select")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("select"),
		objects.NewBuiltinFunction("select", selectSelect)); err != nil {
		return nil, err
	}
	// CPython: Modules/selectmodule.c constants exported by select_exec
	consts := map[string]int64{
		"PIPE_BUF": 512,
		"POLLIN":   0x001,
		"POLLPRI":  0x002,
		"POLLOUT":  0x004,
		"POLLERR":  0x008,
		"POLLHUP":  0x010,
		"POLLNVAL": 0x020,
	}
	for k, v := range consts {
		if err := d.SetItem(objects.NewStr(k), objects.NewInt(v)); err != nil {
			return nil, err
		}
	}
	return m, nil
}
