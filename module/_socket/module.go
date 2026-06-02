// Package _socket is the gopy port of CPython's _socket C module.
// It backs Lib/socket.py with the low-level socket API. The fd type
// is socketFd, defined per-platform in fd_unix.go / fd_windows.go,
// so the same code runs on POSIX file descriptors and Winsock
// SOCKET handles.
//
// CPython: Modules/socketmodule.c

package _socket

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/vm"
)

var (
	// socketErrorType = OSError. CPython: Modules/socketmodule.c:7071 PySocketModule_InsertConstants.
	socketErrorType   = pyerrors.PyExc_OSError
	socketGAIError    = pyerrors.NewExcType("socket.gaierror", []*objects.Type{pyerrors.PyExc_OSError})
	socketHError      = pyerrors.NewExcType("socket.herror", []*objects.Type{pyerrors.PyExc_OSError})
	socketTimeoutType = pyerrors.NewExcType("socket.timeout", []*objects.Type{pyerrors.PyExc_TimeoutError})
)

func init() {
	vm.RegisterErrorPrefix("socket.gaierror:", socketGAIError)
	vm.RegisterErrorPrefix("socket.herror:", socketHError)
	vm.RegisterErrorPrefix("socket.timeout:", socketTimeoutType)
	vm.RegisterErrorPrefix("socket.error:", socketErrorType)
	_ = imp.AppendInittab("_socket", buildModule)
}

// ---------------------------------------------------------------------------
// Default timeout state.
// ---------------------------------------------------------------------------

// defaultTimeout mirrors the module-level timeout used by newly created
// sockets. -1 means blocking (no timeout).
//
// CPython: Modules/socketmodule.c:700 defaulttimeout
var (
	defaultTimeoutMu  sync.Mutex
	defaultTimeoutVal float64 = -1.0
)

// ---------------------------------------------------------------------------
// SocketType - the Python type for socket objects.
// ---------------------------------------------------------------------------

// SocketType is the Python type for socket objects.
//
// CPython: Modules/socketmodule.c:256 PySocketSockObject
var SocketType *objects.Type

func init() {
	SocketType = newSocketType()
}

// sockObj is the runtime shape of a _socket.socket instance.
//
// CPython: Modules/socketmodule.c:256 PySocketSockObject
type sockObj struct {
	objects.Header
	fd      socketFd
	family  int
	typ     int
	proto   int
	timeout float64 // -1 = blocking, 0 = non-blocking, >0 = timeout in seconds
	mu      sync.Mutex
	attrs   *objects.Dict // instance __dict__ for Python-level attributes
}

func newSocketType() *objects.Type {
	t := objects.NewType("socket", []*objects.Type{objects.ObjectType()})
	t.Getattro = sockGetattr
	t.Setattro = sockSetattr
	t.Repr = sockRepr
	t.Str = sockRepr

	objects.SetTypeDescr(t, "connect", objects.NewMethodDescr(t, "connect", sockConnect))
	objects.SetTypeDescr(t, "bind", objects.NewMethodDescr(t, "bind", sockBind))
	objects.SetTypeDescr(t, "listen", objects.NewMethodDescr(t, "listen", sockListen))
	objects.SetTypeDescr(t, "accept", objects.NewMethodDescr(t, "accept", sockAccept))
	objects.SetTypeDescr(t, "close", objects.NewMethodDescr(t, "close", sockClose))
	objects.SetTypeDescr(t, "recv", objects.NewMethodDescr(t, "recv", sockRecv))
	objects.SetTypeDescr(t, "recvfrom", objects.NewMethodDescr(t, "recvfrom", sockRecvfrom))
	objects.SetTypeDescr(t, "send", objects.NewMethodDescr(t, "send", sockSend))
	objects.SetTypeDescr(t, "sendto", objects.NewMethodDescr(t, "sendto", sockSendto))
	objects.SetTypeDescr(t, "setsockopt", objects.NewMethodDescr(t, "setsockopt", sockSetsockopt))
	objects.SetTypeDescr(t, "getsockopt", objects.NewMethodDescr(t, "getsockopt", sockGetsockopt))
	objects.SetTypeDescr(t, "getsockname", objects.NewMethodDescr(t, "getsockname", sockGetsockname))
	objects.SetTypeDescr(t, "getpeername", objects.NewMethodDescr(t, "getpeername", sockGetpeername))
	objects.SetTypeDescr(t, "fileno", objects.NewMethodDescr(t, "fileno", sockFileno))
	objects.SetTypeDescr(t, "setblocking", objects.NewMethodDescr(t, "setblocking", sockSetblocking))
	objects.SetTypeDescr(t, "settimeout", objects.NewMethodDescr(t, "settimeout", sockSettimeout))
	objects.SetTypeDescr(t, "gettimeout", objects.NewMethodDescr(t, "gettimeout", sockGettimeout))
	objects.SetTypeDescr(t, "shutdown", objects.NewMethodDescr(t, "shutdown", sockShutdown))
	objects.SetTypeDescr(t, "sendall", objects.NewMethodDescr(t, "sendall", sockSendall))
	objects.SetTypeDescr(t, "recvinto", objects.NewMethodDescr(t, "recvinto", sockRecvinto))
	objects.SetTypeDescr(t, "recv_into", objects.NewMethodDescr(t, "recv_into", sockRecvInto))
	objects.SetTypeDescr(t, "connect_ex", objects.NewMethodDescr(t, "connect_ex", sockConnectEx))
	objects.SetTypeDescr(t, "dup", objects.NewMethodDescr(t, "dup", sockDup))
	objects.SetTypeDescr(t, "detach", objects.NewMethodDescr(t, "detach", sockDetach))

	objects.SetTypeDescr(t, "family", objects.NewGetSetDescr(
		"family",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(o.(*sockObj).family)), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "type", objects.NewGetSetDescr(
		"type",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(o.(*sockObj).typ)), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "proto", objects.NewGetSetDescr(
		"proto",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(o.(*sockObj).proto)), nil
		},
		nil,
	))
	objects.SetTypeDescr(t, "timeout", objects.NewGetSetDescr(
		"timeout",
		func(o objects.Object) (objects.Object, error) {
			s := o.(*sockObj)
			if s.timeout < 0 {
				return objects.None(), nil
			}
			return objects.NewFloat(s.timeout), nil
		},
		nil,
	))

	// __init__ descriptor: socket.__init__(self, family=AF_INET, type=SOCK_STREAM,
	// proto=0, fileno=None). Called by socket.py's socket.__init__.
	// CPython: Modules/socketmodule.c:3453 sock_initobj
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", sockInitDescr))

	// TpNew allocates a bare sockObj so TpNew + __init__ work together.
	// CPython: Modules/socketmodule.c:3390 sock_new
	t.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		s := &sockObj{fd: invalidFd}
		s.Init(cls)
		return s, nil
	}

	return t
}

// AttrDict implements objects.AttrDictHolder so MemberDescr slot descriptors
// from Python subclasses (e.g. socket.socket's __slots__) work on sockObj.
func (s *sockObj) AttrDict() *objects.Dict { return s.attrs }

// EnsureAttrDict implements objects.AttrDictHolder.
func (s *sockObj) EnsureAttrDict() *objects.Dict {
	if s.attrs == nil {
		s.attrs = objects.NewDict()
	}
	return s.attrs
}

// sockGetattr routes attribute lookups: type descriptors first, then attrs dict.
func sockGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	// Check type descriptors first via GenericGetAttr.
	v, err := objects.GenericGetAttr(o, name)
	if err == nil {
		return v, nil
	}
	// Fall back to instance attrs dict.
	s, ok := o.(*sockObj)
	if ok && s.attrs != nil {
		if v2, err2 := s.attrs.GetItem(name); err2 == nil {
			return v2, nil
		}
	}
	return nil, err
}

// sockSetattr stores non-descriptor attributes in the instance attrs dict.
func sockSetattr(o objects.Object, name objects.Object, value objects.Object) error {
	// If the type has a data descriptor, route through it.
	tp := o.Type()
	nameStr, err := objects.Str(name)
	if err != nil {
		return fmt.Errorf("TypeError: attribute name must be a string")
	}
	if descr, _ := objects.LookupDescriptor(tp, nameStr); descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	s, ok := o.(*sockObj)
	if !ok {
		return fmt.Errorf("TypeError: expected socket object")
	}
	if s.attrs == nil {
		s.attrs = objects.NewDict()
	}
	return s.attrs.SetItem(name, value)
}

// sockInitDescr implements _socket.socket.__init__(self, family, type, proto, fileno).
// Called by socket.py's socket.__init__ to actually create the OS socket.
//
// CPython: Modules/socketmodule.c:3453 sock_initobj
func sockInitDescr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __init__() requires at least 1 argument")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: __init__() argument must be socket object")
	}
	family := syscall.AF_INET
	typ := syscall.SOCK_STREAM
	proto := 0

	if len(args) >= 2 {
		f, ok2 := args[1].(*objects.Int)
		if ok2 {
			f64, _ := f.Int64()
			family = int(f64)
		}
	}
	if len(args) >= 3 {
		tp, ok2 := args[2].(*objects.Int)
		if ok2 {
			t64, _ := tp.Int64()
			typ = int(t64)
		}
	}
	if len(args) >= 4 {
		pr, ok2 := args[3].(*objects.Int)
		if ok2 {
			p64, _ := pr.Int64()
			proto = int(p64)
		}
	}
	// When a fileno is supplied (args[4] not None) the socket adopts that
	// existing descriptor instead of opening a new one. socket.py's
	// socket.__init__ uses this to wrap fds returned by accept() and
	// socketpair(); CPython's sock_initobj takes the same branch.
	//
	// CPython: Modules/socketmodule.c:3453 sock_initobj (fdobj != Py_None)
	if len(args) >= 5 && args[4] != objects.None() {
		f, ok2 := args[4].(*objects.Int)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: fileno must be int, not %s", args[4].Type().Name)
		}
		f64, _ := f.Int64()
		if s.fd >= 0 {
			_ = syscall.Close(s.fd)
		}
		defaultTimeoutMu.Lock()
		timeout := defaultTimeoutVal
		defaultTimeoutMu.Unlock()
		s.fd = int(f64)
		s.family = family
		s.typ = typ
		s.proto = proto
		s.timeout = timeout
		return objects.None(), nil
	}
	if s.fd >= 0 {
		_ = syscall.Close(s.fd)
	}
	fd, err := syscall.Socket(family, typ, proto)
	if err != nil {
		return nil, osError(err)
	}
	defaultTimeoutMu.Lock()
	timeout := defaultTimeoutVal
	defaultTimeoutMu.Unlock()

	s.fd = fd
	s.family = family
	s.typ = typ
	s.proto = proto
	s.timeout = timeout
	return objects.None(), nil
}

// sockRepr returns a repr string for the socket object.
//
// CPython: Modules/socketmodule.c:871 sock_repr
func sockRepr(o objects.Object) (string, error) {
	s := o.(*sockObj)
	return fmt.Sprintf("<socket object, fd=%d, family=%d, type=%d, proto=%d>",
		socketFdInt(s.fd), s.family, s.typ, s.proto), nil
}

// osError formats an OS error as Python's "OSError: [Errno N] msg".
// Wraps the syscall.Errno cast safely.
func osError(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return fmt.Errorf("OSError: [Errno %d] %s", int(errno), errno.Error())
	}
	return fmt.Errorf("OSError: %s", err.Error())
}

// ---------------------------------------------------------------------------
// Address conversion helpers.
// ---------------------------------------------------------------------------

// tupleToSockaddr converts a Python address tuple to a syscall.Sockaddr.
// IPv4: (host_str, port_int)
// IPv6: (host_str, port_int, flowinfo_int, scope_id_int)
//
// CPython: Modules/socketmodule.c:1524 setipaddr / getsockaddrarg
func tupleToSockaddr(family int, addr objects.Object) (syscall.Sockaddr, error) {
	tup, ok := addr.(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: getsockaddrarg: AF_INET address must be tuple, not %T", addr)
	}
	if tup.Len() < 2 {
		return nil, fmt.Errorf("TypeError: AF_INET address must be a pair (host, port)")
	}
	host, err := objects.Str(tup.Item(0))
	if err != nil {
		return nil, fmt.Errorf("TypeError: getsockaddrarg: host must be str")
	}
	portObj, ok2 := tup.Item(1).(*objects.Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: getsockaddrarg: port must be int")
	}
	port64, _ := portObj.Int64()
	port := int(port64)

	switch family {
	case syscall.AF_INET:
		ip := net.ParseIP(host)
		if ip == nil {
			addrs, err2 := net.LookupHost(host)
			if err2 != nil || len(addrs) == 0 {
				return nil, fmt.Errorf("OSError: [Errno 11001] getaddrinfo failed: %s", host)
			}
			ip = net.ParseIP(addrs[0])
		}
		ip4 := ip.To4()
		if ip4 == nil {
			return nil, fmt.Errorf("OSError: not an IPv4 address: %s", host)
		}
		sa := &syscall.SockaddrInet4{Port: port}
		copy(sa.Addr[:], ip4)
		return sa, nil

	case syscall.AF_INET6:
		flowinfo := 0
		scopeID := 0
		if tup.Len() >= 3 {
			if fi, ok3 := tup.Item(2).(*objects.Int); ok3 {
				fi64, _ := fi.Int64()
				flowinfo = int(fi64)
			}
		}
		if tup.Len() >= 4 {
			if si, ok4 := tup.Item(3).(*objects.Int); ok4 {
				si64, _ := si.Int64()
				scopeID = int(si64)
			}
		}
		ip := net.ParseIP(host)
		if ip == nil {
			addrs, err2 := net.LookupHost(host)
			if err2 != nil || len(addrs) == 0 {
				return nil, fmt.Errorf("OSError: [Errno 11001] getaddrinfo failed: %s", host)
			}
			ip = net.ParseIP(addrs[0])
		}
		ip6 := ip.To16()
		if ip6 == nil {
			return nil, fmt.Errorf("OSError: not an IPv6 address: %s", host)
		}
		sa := &syscall.SockaddrInet6{Port: port, ZoneId: uint32(scopeID)}
		_ = flowinfo
		copy(sa.Addr[:], ip6)
		return sa, nil

	case syscall.AF_UNIX:
		path, err2 := objects.Str(tup.Item(0))
		if err2 != nil {
			return nil, fmt.Errorf("TypeError: AF_UNIX address must be str")
		}
		return &syscall.SockaddrUnix{Name: path}, nil

	default:
		return nil, fmt.Errorf("OSError: address family not supported")
	}
}

// sockaddrToTuple converts a syscall.Sockaddr to a Python tuple.
//
// CPython: Modules/socketmodule.c:1750 makesockaddr
func sockaddrToTuple(sa syscall.Sockaddr) (objects.Object, error) {
	switch v := sa.(type) {
	case *syscall.SockaddrInet4:
		host := net.IP(v.Addr[:]).String()
		return objects.NewTuple([]objects.Object{
			objects.NewStr(host),
			objects.NewInt(int64(v.Port)),
		}), nil
	case *syscall.SockaddrInet6:
		host := net.IP(v.Addr[:]).String()
		return objects.NewTuple([]objects.Object{
			objects.NewStr(host),
			objects.NewInt(int64(v.Port)),
			objects.NewInt(0), // flowinfo
			objects.NewInt(int64(v.ZoneId)),
		}), nil
	case *syscall.SockaddrUnix:
		return objects.NewStr(v.Name), nil
	default:
		return objects.NewStr(""), nil
	}
}

// ---------------------------------------------------------------------------
// Socket methods.
// ---------------------------------------------------------------------------

// sockConnect implements socket.connect(address).
//
// CPython: Modules/socketmodule.c:3606 sock_connect_impl
func sockConnect(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: connect() takes exactly one argument (address)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'connect' requires a 'socket' object")
	}
	sa, err := tupleToSockaddr(s.family, args[1])
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := syscall.Connect(s.fd, sa); err != nil {
		return nil, osError(err)
	}
	return objects.None(), nil
}

// sockBind implements socket.bind(address).
//
// CPython: Modules/socketmodule.c:3539 sock_bind (PyDoc_STRVAR)
func sockBind(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: bind() takes exactly one argument (address)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'bind' requires a 'socket' object")
	}
	sa, err := tupleToSockaddr(s.family, args[1])
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := syscall.Bind(s.fd, sa); err != nil {
		return nil, osError(err)
	}
	return objects.None(), nil
}

// sockListen implements socket.listen([backlog]).
//
// CPython: Modules/socketmodule.c:3883 listen_doc
func sockListen(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'listen' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'listen' requires a 'socket' object")
	}
	backlog := 0
	if len(args) >= 2 {
		bObj, ok2 := args[1].(*objects.Int)
		if ok2 {
			b64, _ := bObj.Int64()
			backlog = int(b64)
		}
	}
	if backlog == 0 {
		backlog = syscall.SOMAXCONN
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := syscall.Listen(s.fd, backlog); err != nil {
		return nil, osError(err)
	}
	return objects.None(), nil
}

// sockAccept implements socket.accept(). Returns (socket, address) tuple.
//
// CPython: Modules/socketmodule.c:3029 sock_accept_impl
func sockAccept(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'accept' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'accept' requires a 'socket' object")
	}
	s.mu.Lock()
	nfd, sa, err := syscall.Accept(s.fd)
	s.mu.Unlock()
	if err != nil {
		return nil, osError(err)
	}
	newSock := &sockObj{
		fd:      nfd,
		family:  s.family,
		typ:     s.typ,
		proto:   s.proto,
		timeout: s.timeout,
	}
	newSock.Init(SocketType)

	addrTup, err2 := sockaddrToTuple(sa)
	if err2 != nil {
		_ = closeFd(nfd)
		return nil, err2
	}
	return objects.NewTuple([]objects.Object{newSock, addrTup}), nil
}

// socketSocketpair implements _socket.socketpair([family[, type[, proto]]]).
// Returns a pair of connected socket objects created with the native
// socketpair(2) syscall. socket.py wraps each in a high-level socket via
// detach()/fileno construction.
//
// CPython: Modules/socketmodule.c:5836 socket_socketpair
func socketSocketpair(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	family := syscall.AF_UNIX
	typ := syscall.SOCK_STREAM
	proto := 0
	if len(args) >= 1 {
		if f, ok := args[0].(*objects.Int); ok {
			v, _ := f.Int64()
			family = int(v)
		}
	}
	if len(args) >= 2 {
		if t, ok := args[1].(*objects.Int); ok {
			v, _ := t.Int64()
			typ = int(v)
		}
	}
	if len(args) >= 3 {
		if p, ok := args[2].(*objects.Int); ok {
			v, _ := p.Int64()
			proto = int(v)
		}
	}
	fds, err := syscall.Socketpair(family, typ, proto)
	if err != nil {
		return nil, osError(err)
	}
	defaultTimeoutMu.Lock()
	timeout := defaultTimeoutVal
	defaultTimeoutMu.Unlock()
	mk := func(fd int) *sockObj {
		s := &sockObj{fd: fd, family: family, typ: typ, proto: proto, timeout: timeout}
		s.Init(SocketType)
		return s
	}
	return objects.NewTuple([]objects.Object{mk(fds[0]), mk(fds[1])}), nil
}

// sockClose implements socket.close().
//
// CPython: Modules/socketmodule.c:2656 sock_close (approx line)
func sockClose(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'close' requires a 'socket' object")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if socketFdValid(s.fd) {
		_ = closeFd(s.fd)
		s.fd = invalidFd
	}
	return objects.None(), nil
}

// sockRecv implements socket.recv(bufsize[, flags]).
//
// CPython: Modules/socketmodule.c:3900 sock_recv_impl
func sockRecv(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: recv() takes at least 1 argument (bufsize)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'recv' requires a 'socket' object")
	}
	bufsizeObj, ok2 := args[1].(*objects.Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: recv() argument 1 must be int")
	}
	bufsize64, _ := bufsizeObj.Int64()
	bufsize := int(bufsize64)

	flags := 0
	if len(args) >= 3 {
		if fObj, ok3 := args[2].(*objects.Int); ok3 {
			f64, _ := fObj.Int64()
			flags = int(f64)
		}
	}

	if flags != 0 {
		buf := make([]byte, bufsize)
		n, _, err := syscall.Recvfrom(s.fd, buf, flags)
		if err != nil {
			return nil, osError(err)
		}
		return objects.NewBytes(buf[:n]), nil
	}

	buf := make([]byte, bufsize)
	s.mu.Lock()
	n, err := readFd(s.fd, buf)
	s.mu.Unlock()
	if err != nil {
		return nil, osError(err)
	}
	return objects.NewBytes(buf[:n]), nil
}

// sockRecvfrom implements socket.recvfrom(bufsize[, flags]).
// Returns (data, address) tuple.
//
// CPython: Modules/socketmodule.c:4072 sock_recvfrom_impl
func sockRecvfrom(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: recvfrom() takes at least 1 argument (bufsize)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'recvfrom' requires a 'socket' object")
	}
	bufsizeObj, ok2 := args[1].(*objects.Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: recvfrom() argument 1 must be int")
	}
	bufsize64, _ := bufsizeObj.Int64()
	flags := 0
	if len(args) >= 3 {
		if fObj, ok3 := args[2].(*objects.Int); ok3 {
			f64, _ := fObj.Int64()
			flags = int(f64)
		}
	}

	buf := make([]byte, int(bufsize64))
	n, sa, err := syscall.Recvfrom(s.fd, buf, flags)
	if err != nil {
		return nil, osError(err)
	}
	addrTup, err2 := sockaddrToTuple(sa)
	if err2 != nil {
		return nil, err2
	}
	return objects.NewTuple([]objects.Object{
		objects.NewBytes(buf[:n]),
		addrTup,
	}), nil
}

// sockSend implements socket.send(data[, flags]).
//
// CPython: Modules/socketmodule.c:4587 sock_send_impl
func sockSend(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: send() takes at least 1 argument (data)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'send' requires a 'socket' object")
	}
	data, err := toBytes(args[1])
	if err != nil {
		return nil, err
	}
	flags := 0
	if len(args) >= 3 {
		if fObj, ok3 := args[2].(*objects.Int); ok3 {
			f64, _ := fObj.Int64()
			flags = int(f64)
		}
	}
	var n int
	if flags == 0 {
		s.mu.Lock()
		n, err = writeFd(s.fd, data)
		s.mu.Unlock()
	} else {
		err = syscall.Sendto(s.fd, data, flags, nil)
		n = len(data)
	}
	if err != nil {
		return nil, osError(err)
	}
	return objects.NewInt(int64(n)), nil
}

// sockSendall implements socket.sendall(data[, flags]).
// Keeps sending until all bytes are written.
//
// CPython: Modules/socketmodule.c:4649 sock_sendall
func sockSendall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: sendall() takes at least 1 argument (data)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'sendall' requires a 'socket' object")
	}
	data, err := toBytes(args[1])
	if err != nil {
		return nil, err
	}
	for len(data) > 0 {
		s.mu.Lock()
		n, werr := writeFd(s.fd, data)
		s.mu.Unlock()
		if werr != nil {
			return nil, osError(werr)
		}
		data = data[n:]
	}
	return objects.None(), nil
}

// sockRecvinto implements socket.recv_into(buffer[, nbytes[, flags]]).
//
// CPython: Modules/socketmodule.c:3958 sock_recv_into_impl
func sockRecvinto(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: recv_into() takes at least 1 argument (buffer)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'recv_into' requires a 'socket' object")
	}
	bufsize := 4096
	if len(args) >= 3 {
		if n, ok2 := args[2].(*objects.Int); ok2 {
			n64, _ := n.Int64()
			bufsize = int(n64)
		}
	}
	buf := make([]byte, bufsize)
	s.mu.Lock()
	n, err := readFd(s.fd, buf)
	s.mu.Unlock()
	if err != nil {
		return nil, osError(err)
	}
	return objects.NewInt(int64(n)), nil
}

// sockRecvInto implements socket.recv_into(buffer[, nbytes[, flags]]).
// Reads directly into a writable buffer (bytearray or similar).
//
// CPython: Modules/socketmodule.c:3958 sock_recv_into_impl
func sockRecvInto(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: recv_into() takes at least 1 argument (buffer)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'recv_into' requires a 'socket' object")
	}
	// Get the writable buffer object.
	bufObj := args[1]
	var buf []byte
	var n int
	var err error
	switch b := bufObj.(type) {
	case *objects.ByteArray:
		buf = b.Bytes()
		if len(buf) == 0 {
			return objects.NewInt(0), nil
		}
		s.mu.Lock()
		n, err = readFd(s.fd, buf)
		s.mu.Unlock()
		if err != nil {
			return nil, osError(err)
		}
		// Write back into the bytearray.
		raw := b.Bytes()
		copy(raw, buf[:n])
	case *objects.Bytes:
		// bytes is immutable; fallback to a temp buffer.
		buf = make([]byte, b.Len())
		s.mu.Lock()
		n, err = readFd(s.fd, buf)
		s.mu.Unlock()
		if err != nil {
			return nil, osError(err)
		}
	default:
		// Generic writable buffer: use a temporary read buffer.
		bufsize := 4096
		if len(args) >= 3 {
			if iv, ok2 := args[2].(*objects.Int); ok2 {
				v, _ := iv.Int64()
				bufsize = int(v)
			}
		}
		buf = make([]byte, bufsize)
		s.mu.Lock()
		n, err = readFd(s.fd, buf)
		s.mu.Unlock()
		if err != nil {
			return nil, osError(err)
		}
	}
	return objects.NewInt(int64(n)), nil
}

// sockConnectEx implements socket.connect_ex(address).
// Returns the error number instead of raising an exception.
//
// CPython: Modules/socketmodule.c:3565 sock_connect_ex
func sockConnectEx(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: connect_ex() takes 1 argument (address)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'connect_ex' requires a 'socket' object")
	}
	sa, err := tupleToSockaddr(s.family, args[1])
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	err = syscall.Connect(s.fd, sa)
	s.mu.Unlock()
	if err != nil {
		if errno, ok2 := err.(syscall.Errno); ok2 {
			return objects.NewInt(int64(errno)), nil
		}
		return objects.NewInt(int64(syscall.ECONNREFUSED)), nil
	}
	return objects.NewInt(0), nil
}

// sockDup implements socket.dup(). Returns a new socket with a duplicated fd.
//
// CPython: Modules/socketmodule.c:3148 sock_dup
func sockDup(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'dup' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'dup' requires a 'socket' object")
	}
	s.mu.Lock()
	newfd, err := dupFd(s.fd)
	s.mu.Unlock()
	if err != nil {
		return nil, osError(err)
	}
	ns := &sockObj{fd: newfd, family: s.family, typ: s.typ, proto: s.proto, timeout: s.timeout}
	ns.Init(s.Type())
	return ns, nil
}

// sockDetach implements socket.detach(). Returns the fd and marks the socket closed.
//
// CPython: Modules/socketmodule.c:3127 sock_detach
func sockDetach(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'detach' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'detach' requires a 'socket' object")
	}
	s.mu.Lock()
	fd := s.fd
	s.fd = invalidFd
	s.mu.Unlock()
	return objects.NewInt(int64(fd)), nil
}

// sockSendto implements socket.sendto(data, address) or
// socket.sendto(data, flags, address).
//
// CPython: Modules/socketmodule.c:4730 sock_sendto_impl
func sockSendto(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: sendto() takes 2 or 3 arguments")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'sendto' requires a 'socket' object")
	}
	data, err := toBytes(args[1])
	if err != nil {
		return nil, err
	}
	var flags int
	var addrArg objects.Object
	if len(args) == 3 {
		flags = 0
		addrArg = args[2]
	} else {
		fObj, ok3 := args[2].(*objects.Int)
		if !ok3 {
			return nil, fmt.Errorf("TypeError: sendto() argument 2 must be int")
		}
		f64, _ := fObj.Int64()
		flags = int(f64)
		addrArg = args[3]
	}
	sa, err2 := tupleToSockaddr(s.family, addrArg)
	if err2 != nil {
		return nil, err2
	}
	if err3 := syscall.Sendto(s.fd, data, flags, sa); err3 != nil {
		return nil, osError(err3)
	}
	return objects.NewInt(int64(len(data))), nil
}

// sockSetsockopt implements socket.setsockopt(level, optname, value).
// value may be int (passed as 4-byte native-endian int) or bytes.
//
// CPython: Modules/socketmodule.c:3370 sock_setsockopt
func sockSetsockopt(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("TypeError: setsockopt() takes 3 arguments (level, optname, value)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'setsockopt' requires a 'socket' object")
	}
	levelObj, ok2 := args[1].(*objects.Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: setsockopt() argument 1 must be int")
	}
	optObj, ok3 := args[2].(*objects.Int)
	if !ok3 {
		return nil, fmt.Errorf("TypeError: setsockopt() argument 2 must be int")
	}
	l64, _ := levelObj.Int64()
	o64, _ := optObj.Int64()
	level := int(l64)
	optname := int(o64)

	switch v := args[3].(type) {
	case *objects.Int:
		val32, _ := v.Int64()
		if err := syscall.SetsockoptInt(s.fd, level, optname, int(val32)); err != nil {
			return nil, osError(err)
		}
	case *objects.Bytes:
		buf := v.Bytes()
		if err := setsockoptBytes(s.fd, level, optname, buf); err != nil {
			return nil, osError(err)
		}
	default:
		return nil, fmt.Errorf("TypeError: setsockopt() argument 3 must be int or bytes")
	}
	return objects.None(), nil
}

// sockGetsockopt implements socket.getsockopt(level, optname[, buffersize]).
//
// CPython: Modules/socketmodule.c:3412 sock_getsockopt
func sockGetsockopt(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: getsockopt() takes 2 or 3 arguments (level, optname[, buflen])")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'getsockopt' requires a 'socket' object")
	}
	levelObj, ok2 := args[1].(*objects.Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: getsockopt() argument 1 must be int")
	}
	optObj, ok3 := args[2].(*objects.Int)
	if !ok3 {
		return nil, fmt.Errorf("TypeError: getsockopt() argument 2 must be int")
	}
	l64, _ := levelObj.Int64()
	o64, _ := optObj.Int64()

	buflen := 0
	if len(args) >= 4 {
		if bl, ok4 := args[3].(*objects.Int); ok4 {
			b64, _ := bl.Int64()
			buflen = int(b64)
		}
	}

	if buflen == 0 {
		val, err := syscall.GetsockoptInt(s.fd, int(l64), int(o64))
		if err != nil {
			return nil, osError(err)
		}
		return objects.NewInt(int64(val)), nil
	}
	buf, err := getsockoptBytes(s.fd, int(l64), int(o64), buflen)
	if err != nil {
		return nil, osError(err)
	}
	return objects.NewBytes(buf), nil
}

// sockGetsockname implements socket.getsockname().
//
// CPython: Modules/socketmodule.c:3813 getsockname_doc
func sockGetsockname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'getsockname' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'getsockname' requires a 'socket' object")
	}
	sa, err := syscall.Getsockname(s.fd)
	if err != nil {
		return nil, osError(err)
	}
	return sockaddrToTuple(sa)
}

// sockGetpeername implements socket.getpeername().
//
// CPython: Modules/socketmodule.c:3847 getpeername_doc
func sockGetpeername(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'getpeername' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'getpeername' requires a 'socket' object")
	}
	sa, err := syscall.Getpeername(s.fd)
	if err != nil {
		return nil, osError(err)
	}
	return sockaddrToTuple(sa)
}

// sockFileno implements socket.fileno(). Returns the underlying fd as
// an int. On Windows the SOCKET handle is exposed directly, matching
// CPython's behavior on _WIN32.
//
// CPython: Modules/socketmodule.c:3783 fileno_doc
func sockFileno(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'fileno' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'fileno' requires a 'socket' object")
	}
	return objects.NewInt(int64(socketFdInt(s.fd))), nil
}

// sockSetblocking implements socket.setblocking(flag).
//
// CPython: Modules/socketmodule.c:3170 setblocking_doc
func sockSetblocking(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: setblocking() takes exactly one argument (flag)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'setblocking' requires a 'socket' object")
	}
	flag, err := objects.IsTruthy(args[1])
	if err != nil {
		return nil, err
	}
	if flag {
		s.timeout = -1.0
	} else {
		s.timeout = 0.0
	}
	return objects.None(), nil
}

// sockSettimeout implements socket.settimeout(timeout).
//
// CPython: Modules/socketmodule.c:3245 sock_settimeout
func sockSettimeout(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: settimeout() takes exactly one argument (timeout)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'settimeout' requires a 'socket' object")
	}
	if args[1] == objects.None() {
		s.timeout = -1.0
		return objects.None(), nil
	}
	switch v := args[1].(type) {
	case *objects.Float:
		t := v.Float64()
		if t < 0 {
			return nil, fmt.Errorf("ValueError: Timeout value out of range")
		}
		s.timeout = t
	case *objects.Int:
		t64, _ := v.Int64()
		if t64 < 0 {
			return nil, fmt.Errorf("ValueError: Timeout value out of range")
		}
		s.timeout = float64(t64)
	default:
		return nil, fmt.Errorf("TypeError: a float is required")
	}
	return objects.None(), nil
}

// sockGettimeout implements socket.gettimeout().
//
// CPython: Modules/socketmodule.c:3245 sock_gettimeout
func sockGettimeout(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor 'gettimeout' requires a 'socket' object")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'gettimeout' requires a 'socket' object")
	}
	if s.timeout < 0 {
		return objects.None(), nil
	}
	return objects.NewFloat(s.timeout), nil
}

// sockShutdown implements socket.shutdown(how).
//
// CPython: Modules/socketmodule.c:5293 shutdown_doc
func sockShutdown(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: shutdown() takes exactly one argument (how)")
	}
	s, ok := args[0].(*sockObj)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'shutdown' requires a 'socket' object")
	}
	howObj, ok2 := args[1].(*objects.Int)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: shutdown() argument 1 must be int")
	}
	how64, _ := howObj.Int64()
	if err := syscall.Shutdown(s.fd, int(how64)); err != nil {
		return nil, osError(err)
	}
	return objects.None(), nil
}

// ---------------------------------------------------------------------------
// Module-level functions.
// ---------------------------------------------------------------------------

// socketSocket implements _socket.socket(family, type, proto).
//
// CPython: Modules/socketmodule.c:3453 sock_initobj
func socketSocket(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	family := syscall.AF_INET
	typ := syscall.SOCK_STREAM
	proto := 0

	if len(args) >= 1 {
		fObj, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: socket() argument 1 (family) must be int")
		}
		f64, _ := fObj.Int64()
		family = int(f64)
	}
	if len(args) >= 2 {
		tObj, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: socket() argument 2 (type) must be int")
		}
		t64, _ := tObj.Int64()
		typ = int(t64)
	}
	if len(args) >= 3 {
		pObj, ok := args[2].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: socket() argument 3 (proto) must be int")
		}
		p64, _ := pObj.Int64()
		proto = int(p64)
	}

	fd, err := syscall.Socket(family, typ, proto)
	if err != nil {
		return nil, osError(err)
	}

	defaultTimeoutMu.Lock()
	timeout := defaultTimeoutVal
	defaultTimeoutMu.Unlock()

	s := &sockObj{
		fd:      fd,
		family:  family,
		typ:     typ,
		proto:   proto,
		timeout: timeout,
	}
	s.Init(SocketType)
	return s, nil
}

// socketGethostname implements _socket.gethostname().
//
// CPython: Modules/socketmodule.c:5854 socket_gethostname
func socketGethostname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	name, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("OSError: %s", err.Error())
	}
	return objects.NewStr(name), nil
}

// socketGethostbyname implements _socket.gethostbyname(hostname).
//
// CPython: Modules/socketmodule.c:5964 socket_gethostbyname
func socketGethostbyname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: gethostbyname() takes exactly one argument")
	}
	host, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: gethostbyname() argument must be str")
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return objects.NewStr(ip4.String()), nil
		}
		return objects.NewStr(ip.String()), nil
	}
	addrs, err2 := net.LookupHost(host)
	if err2 != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("OSError: [Errno 11001] getaddrinfo failed")
	}
	return objects.NewStr(addrs[0]), nil
}

// socketGethostbyaddr implements _socket.gethostbyaddr(ip_address).
// Returns (hostname, aliases, ipaddrs) tuple.
//
// CPython: Modules/socketmodule.c:6235 socket_gethostbyaddr
func socketGethostbyaddr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: gethostbyaddr() takes exactly one argument")
	}
	ipStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: gethostbyaddr() argument must be str")
	}
	names, err := net.LookupAddr(ipStr)
	if err != nil {
		// fallback: try resolving as a hostname to get the canonical name
		cname, err2 := net.LookupCNAME(ipStr)
		if err2 != nil {
			return nil, fmt.Errorf("socket.gaierror: [Errno 11001] getaddrinfo failed")
		}
		// strip trailing dot from CNAME
		hostname := strings.TrimSuffix(cname, ".")
		addrs, _ := net.LookupHost(ipStr)
		ipList := make([]objects.Object, len(addrs))
		for i, a := range addrs {
			ipList[i] = objects.NewStr(a)
		}
		result := []objects.Object{
			objects.NewStr(hostname),
			objects.NewList([]objects.Object{}),
			objects.NewList(ipList),
		}
		return objects.NewTuple(result), nil
	}
	hostname := strings.TrimSuffix(names[0], ".")
	aliases := make([]objects.Object, 0, len(names)-1)
	for _, n := range names[1:] {
		aliases = append(aliases, objects.NewStr(strings.TrimSuffix(n, ".")))
	}
	result := []objects.Object{
		objects.NewStr(hostname),
		objects.NewList(aliases),
		objects.NewList([]objects.Object{objects.NewStr(ipStr)}),
	}
	return objects.NewTuple(result), nil
}

// socketGethostbynameEx implements _socket.gethostbyname_ex(hostname).
// Returns (hostname, aliases, ipaddrs) tuple.
//
// CPython: Modules/socketmodule.c:6008 setipaddr (shared by gethostbyname_ex/gethostbyaddr)
func socketGethostbynameEx(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: gethostbyname_ex() takes exactly one argument")
	}
	host, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: gethostbyname_ex() argument must be str")
	}
	cname, err := net.LookupCNAME(host)
	if err != nil {
		return nil, fmt.Errorf("socket.gaierror: [Errno 11001] getaddrinfo failed")
	}
	hostname := strings.TrimSuffix(cname, ".")
	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("socket.gaierror: [Errno 11001] getaddrinfo failed")
	}
	ipList := make([]objects.Object, len(addrs))
	for i, a := range addrs {
		ipList[i] = objects.NewStr(a)
	}
	result := []objects.Object{
		objects.NewStr(hostname),
		objects.NewList([]objects.Object{}),
		objects.NewList(ipList),
	}
	return objects.NewTuple(result), nil
}

// socketGetaddrinfo implements _socket.getaddrinfo(host, port[, family, type, proto, flags]).
//
// CPython: Modules/socketmodule.c:6898 socket_getaddrinfo
func socketGetaddrinfo(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: getaddrinfo() takes 2 to 6 arguments")
	}

	var hostStr string
	if args[0] == objects.None() {
		hostStr = ""
	} else {
		h, err := objects.Str(args[0])
		if err != nil {
			return nil, fmt.Errorf("TypeError: getaddrinfo() argument 1 must be str or None")
		}
		hostStr = h
	}

	var portStr string
	switch p := args[1].(type) {
	case *objects.Int:
		p64, _ := p.Int64()
		portStr = fmt.Sprintf("%d", p64)
	case *objects.Unicode:
		s, _ := objects.Str(p)
		portStr = s
	default:
		if args[1] == objects.None() {
			portStr = ""
		} else {
			return nil, fmt.Errorf("TypeError: getaddrinfo() argument 2 must be int, str, or None")
		}
	}

	sockType := 0
	if len(args) >= 4 {
		if tObj, ok := args[3].(*objects.Int); ok {
			t64, _ := tObj.Int64()
			sockType = int(t64)
		}
	}

	var addrs []net.IPAddr
	if hostStr == "" {
		addrs = []net.IPAddr{{IP: net.IPv4(0, 0, 0, 0)}}
	} else {
		var err error
		addrs, err = net.DefaultResolver.LookupIPAddr(context.Background(), hostStr)
		if err != nil {
			return nil, fmt.Errorf("OSError: [Errno 11001] getaddrinfo failed: %s", err.Error())
		}
	}

	var results []objects.Object
	for _, addr := range addrs {
		var portInt int
		if portStr != "" {
			fmt.Sscanf(portStr, "%d", &portInt)
		}

		var family int
		var sockaddr objects.Object
		if ip4 := addr.IP.To4(); ip4 != nil {
			family = syscall.AF_INET
			sockaddr = objects.NewTuple([]objects.Object{
				objects.NewStr(ip4.String()),
				objects.NewInt(int64(portInt)),
			})
		} else {
			family = syscall.AF_INET6
			sockaddr = objects.NewTuple([]objects.Object{
				objects.NewStr(addr.IP.String()),
				objects.NewInt(int64(portInt)),
				objects.NewInt(0),
				objects.NewInt(0),
			})
		}

		proto := 0
		if len(args) >= 5 {
			if pObj, ok := args[4].(*objects.Int); ok {
				p64, _ := pObj.Int64()
				proto = int(p64)
			}
		}

		result := objects.NewTuple([]objects.Object{
			objects.NewInt(int64(family)),
			objects.NewInt(int64(sockType)),
			objects.NewInt(int64(proto)),
			objects.NewStr(""),
			sockaddr,
		})
		results = append(results, result)
	}

	return objects.NewList(results), nil
}

// socketGetservbyname implements _socket.getservbyname(servicename[, protocolname]).
//
// CPython: Modules/socketmodule.c:6338 socket_getservbyname
func socketGetservbyname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: getservbyname() takes 1 or 2 arguments")
	}
	svcName, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: getservbyname() argument 1 must be str")
	}
	proto := ""
	if len(args) >= 2 {
		p, err2 := objects.Str(args[1])
		if err2 == nil {
			proto = p
		}
	}
	port, err2 := net.LookupPort(proto, svcName)
	if err2 != nil {
		return nil, fmt.Errorf("OSError: service/proto not found: %s", err2.Error())
	}
	return objects.NewInt(int64(port)), nil
}

// socketSetdefaulttimeout implements _socket.setdefaulttimeout(timeout).
//
// CPython: Modules/socketmodule.c:7165 socket_setdefaulttimeout
func socketSetdefaulttimeout(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: setdefaulttimeout() takes exactly one argument")
	}
	defaultTimeoutMu.Lock()
	defer defaultTimeoutMu.Unlock()
	if args[0] == objects.None() {
		defaultTimeoutVal = -1.0
		return objects.None(), nil
	}
	switch v := args[0].(type) {
	case *objects.Float:
		t := v.Float64()
		if t < 0 {
			return nil, fmt.Errorf("ValueError: Timeout value out of range")
		}
		defaultTimeoutVal = t
	case *objects.Int:
		t64, _ := v.Int64()
		if t64 < 0 {
			return nil, fmt.Errorf("ValueError: Timeout value out of range")
		}
		defaultTimeoutVal = float64(t64)
	default:
		return nil, fmt.Errorf("TypeError: a float is required")
	}
	return objects.None(), nil
}

// socketGetdefaulttimeout implements _socket.getdefaulttimeout().
//
// CPython: Modules/socketmodule.c:7144 socket_getdefaulttimeout
func socketGetdefaulttimeout(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	defaultTimeoutMu.Lock()
	t := defaultTimeoutVal
	defaultTimeoutMu.Unlock()
	if t < 0 {
		return objects.None(), nil
	}
	return objects.NewFloat(t), nil
}

// socketInetAton implements _socket.inet_aton(string).
//
// CPython: Modules/socketmodule.c:6687 _socket_inet_aton_impl
func socketInetAton(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: inet_aton() takes exactly one argument")
	}
	ipStr, err := objects.Str(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: inet_aton() argument must be str")
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("OSError: illegal IP address string passed to inet_aton")
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("OSError: illegal IP address string passed to inet_aton")
	}
	return objects.NewBytes([]byte(ip4)), nil
}

// socketInetNtoa implements _socket.inet_ntoa(packed_ip).
//
// CPython: Modules/socketmodule.c:6760 _socket_inet_ntoa_impl
func socketInetNtoa(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: inet_ntoa() takes exactly one argument")
	}
	data, err := toBytes(args[0])
	if err != nil {
		return nil, err
	}
	if len(data) != 4 {
		return nil, fmt.Errorf("OSError: packed IP wrong length for inet_ntoa")
	}
	ip := net.IP(data)
	return objects.NewStr(ip.String()), nil
}

// socketHtons implements _socket.htons(x).
//
// CPython: Modules/socketmodule.c:6654 _socket_htons_impl
func socketHtons(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: htons() takes exactly one argument")
	}
	v, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: htons() argument must be int")
	}
	val, _ := v.Int64()
	result := ((val & 0xFF) << 8) | ((val >> 8) & 0xFF)
	return objects.NewInt(result & 0xFFFF), nil
}

// socketHtonl implements _socket.htonl(x).
//
// CPython: Modules/socketmodule.c:6670 _socket_htonl_impl
func socketHtonl(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: htonl() takes exactly one argument")
	}
	v, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: htonl() argument must be int")
	}
	val, _ := v.Int64()
	b := make([]byte, 4)
	b[0] = byte(val)
	b[1] = byte(val >> 8)
	b[2] = byte(val >> 16)
	b[3] = byte(val >> 24)
	result := int64(b[0])<<24 | int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3])
	return objects.NewInt(result & 0xFFFFFFFF), nil
}

// socketNtohs implements _socket.ntohs(x).
//
// CPython: Modules/socketmodule.c:6622 _socket_ntohs_impl
func socketNtohs(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return socketHtons(args, nil)
}

// socketNtohl implements _socket.ntohl(x).
//
// CPython: Modules/socketmodule.c:6638 _socket_ntohl_impl
func socketNtohl(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return socketHtonl(args, nil)
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// toBytes extracts a []byte from a bytes or str Python object.
func toBytes(o objects.Object) ([]byte, error) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.Unicode:
		s, err := objects.Str(o)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%T'", o)
}

// ---------------------------------------------------------------------------
// Module builder.
// ---------------------------------------------------------------------------

// buildModule materializes the _socket module dict.
//
// CPython: Modules/socketmodule.c:7462 PyInit__socket
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_socket")
	d := m.Dict()

	set := func(name string, val objects.Object) error {
		return d.SetItem(objects.NewStr(name), val)
	}
	fn := func(name string, f func([]objects.Object, map[string]objects.Object) (objects.Object, error)) objects.Object {
		return objects.NewBuiltinFunction(name, f)
	}

	funcs := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"socket", socketSocket},
		{"socketpair", socketSocketpair},
		{"gethostname", socketGethostname},
		{"gethostbyname", socketGethostbyname},
		{"gethostbyname_ex", socketGethostbynameEx},
		{"gethostbyaddr", socketGethostbyaddr},
		{"getaddrinfo", socketGetaddrinfo},
		{"getservbyname", socketGetservbyname},
		{"setdefaulttimeout", socketSetdefaulttimeout},
		{"getdefaulttimeout", socketGetdefaulttimeout},
		{"inet_aton", socketInetAton},
		{"inet_ntoa", socketInetNtoa},
		{"htons", socketHtons},
		{"htonl", socketHtonl},
		{"ntohs", socketNtohs},
		{"ntohl", socketNtohl},
	}
	for _, f := range funcs {
		if err := set(f.name, fn(f.name, f.fn)); err != nil {
			return nil, err
		}
	}

	if err := set("socket", SocketType); err != nil {
		return nil, err
	}

	// CPython: Modules/socketmodule.c:7071 PySocketModule_InsertConstants
	excTypes := []struct {
		name string
		t    *objects.Type
	}{
		{"error", socketErrorType},
		{"gaierror", socketGAIError},
		{"herror", socketHError},
		{"timeout", socketTimeoutType},
	}
	for _, e := range excTypes {
		if err := set(e.name, e.t); err != nil {
			return nil, err
		}
	}

	constants := []struct {
		name string
		val  int
	}{
		{"AF_UNSPEC", syscall.AF_UNSPEC},
		{"AF_INET", syscall.AF_INET},
		{"AF_INET6", syscall.AF_INET6},
		{"AF_UNIX", syscall.AF_UNIX},

		{"SOCK_STREAM", syscall.SOCK_STREAM},
		{"SOCK_DGRAM", syscall.SOCK_DGRAM},
		{"SOCK_RAW", syscall.SOCK_RAW},

		{"SOL_SOCKET", syscall.SOL_SOCKET},
		{"SO_REUSEADDR", syscall.SO_REUSEADDR},
		{"SO_KEEPALIVE", syscall.SO_KEEPALIVE},

		{"IPPROTO_TCP", syscall.IPPROTO_TCP},
		{"IPPROTO_UDP", syscall.IPPROTO_UDP},
		{"IPPROTO_IP", syscall.IPPROTO_IP},

		{"TCP_NODELAY", syscall.TCP_NODELAY},

		{"SHUT_RD", syscall.SHUT_RD},
		{"SHUT_WR", syscall.SHUT_WR},
		{"SHUT_RDWR", syscall.SHUT_RDWR},

		{"SO_ERROR", so_error},
		{"SO_TYPE", so_type},
		{"SO_SNDBUF", int(syscall.SO_SNDBUF)},
		{"SO_RCVBUF", int(syscall.SO_RCVBUF)},
		{"SO_BROADCAST", int(syscall.SO_BROADCAST)},
		{"SO_OOBINLINE", so_oobinline},
		{"SO_LINGER", int(syscall.SO_LINGER)},
		{"SO_RCVLOWAT", so_rcvlowat},
		{"SO_SNDLOWAT", so_sndlowat},
		{"SO_RCVTIMEO", so_rcvtimeo},
		{"SO_SNDTIMEO", so_sndtimeo},

		{"IP_TOS", syscall.IP_TOS},
		{"IP_TTL", syscall.IP_TTL},
		{"IP_MULTICAST_IF", syscall.IP_MULTICAST_IF},
		{"IP_MULTICAST_TTL", syscall.IP_MULTICAST_TTL},
		{"IP_MULTICAST_LOOP", syscall.IP_MULTICAST_LOOP},
		{"IP_ADD_MEMBERSHIP", syscall.IP_ADD_MEMBERSHIP},
		{"IP_DROP_MEMBERSHIP", syscall.IP_DROP_MEMBERSHIP},

		{"INADDR_ANY", inaddr_any},
		{"INADDR_BROADCAST", inaddr_broadcast},
		{"INADDR_LOOPBACK", inaddr_loopback},
		{"INADDR_NONE", inaddr_none},

		{"EAI_NONAME", eai_noname},
		{"EAI_AGAIN", eai_again},
		{"EAI_FAIL", eai_fail},
		{"EAI_NODATA", eai_nodata},

		{"AI_PASSIVE", ai_passive},
		{"AI_CANONNAME", ai_canonname},
		{"AI_NUMERICHOST", ai_numerichost},
		{"AI_V4MAPPED", ai_v4mapped},
		{"AI_ALL", ai_all},
		{"AI_ADDRCONFIG", ai_addrconfig},
		{"AI_NUMERICSERV", ai_numericserv},

		{"NI_NOFQDN", ni_nofqdn},
		{"NI_NUMERICHOST", ni_numerichost},
		{"NI_NAMEREQD", ni_namereqd},
		{"NI_NUMERICSERV", ni_numericserv},
		{"NI_DGRAM", ni_dgram},

		{"MSG_OOB", msg_oob},
		{"MSG_PEEK", msg_peek},
		{"MSG_DONTROUTE", msg_dontroute},
		{"MSG_WAITALL", msg_waitall},
		{"MSG_DONTWAIT", msg_dontwait},
	}
	for _, c := range constants {
		if err := set(c.name, objects.NewInt(int64(c.val))); err != nil {
			return nil, err
		}
	}

	// Boolean module attributes.
	//
	// CPython: Modules/socketmodule.c PySocketModule_InsertConstants
	// has_ipv6: always true; gopy binds AF_INET6 on all platforms.
	if err := set("has_ipv6", objects.True()); err != nil {
		return nil, err
	}

	return m, nil
}

// Silence unused-import warnings for binary and time which are kept for
// future htonll / timeout-driven branches.
var (
	_ = binary.NativeEndian
	_ = time.Second
)
