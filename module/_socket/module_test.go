package _socket

import (
	"syscall"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// makeInt is a test helper to create a Python int from a Go int64.
func makeInt(n int64) *objects.Int {
	return objects.NewInt(n)
}

// makeStr is a test helper to create a Python str from a Go string.
func makeStr(s string) objects.Object {
	return objects.NewStr(s)
}

// makeTuple is a test helper to create a Python tuple.
func makeTuple(items ...objects.Object) *objects.Tuple {
	return objects.NewTuple(items)
}

// ---------------------------------------------------------------------------
// Module-level function tests.
// ---------------------------------------------------------------------------

// TestGethostname verifies that gethostname() returns a non-empty string.
func TestGethostname(t *testing.T) {
	result, err := socketGethostname(nil, nil)
	if err != nil {
		t.Fatalf("gethostname() error: %v", err)
	}
	s, err := objects.Str(result)
	if err != nil {
		t.Fatalf("gethostname() result not str: %v", err)
	}
	if s == "" {
		t.Fatal("gethostname() returned empty string")
	}
	t.Logf("hostname: %s", s)
}

// TestGethostbyname verifies that gethostbyname("localhost") returns an IP.
func TestGethostbyname(t *testing.T) {
	result, err := socketGethostbyname([]objects.Object{makeStr("localhost")}, nil)
	if err != nil {
		t.Fatalf("gethostbyname() error: %v", err)
	}
	ip, err := objects.Str(result)
	if err != nil {
		t.Fatalf("gethostbyname() result not str: %v", err)
	}
	if ip == "" {
		t.Fatal("gethostbyname() returned empty string")
	}
	t.Logf("localhost -> %s", ip)
}

// TestInetAton verifies that inet_aton converts a dotted-decimal string to
// a 4-byte bytes object.
//
// CPython: Modules/socketmodule.c:6687 _socket_inet_aton_impl
func TestInetAton(t *testing.T) {
	result, err := socketInetAton([]objects.Object{makeStr("127.0.0.1")}, nil)
	if err != nil {
		t.Fatalf("inet_aton() error: %v", err)
	}
	b, ok := result.(*objects.Bytes)
	if !ok {
		t.Fatalf("inet_aton() result not bytes: %T", result)
	}
	raw := b.Bytes()
	if len(raw) != 4 {
		t.Fatalf("inet_aton() expected 4 bytes, got %d", len(raw))
	}
	if raw[0] != 127 || raw[1] != 0 || raw[2] != 0 || raw[3] != 1 {
		t.Fatalf("inet_aton(127.0.0.1) = %v, want [127 0 0 1]", raw)
	}
}

// TestInetNtoa verifies that inet_ntoa converts packed bytes to dotted-decimal.
//
// CPython: Modules/socketmodule.c:6760 _socket_inet_ntoa_impl
func TestInetNtoa(t *testing.T) {
	packed := objects.NewBytes([]byte{127, 0, 0, 1})
	result, err := socketInetNtoa([]objects.Object{packed}, nil)
	if err != nil {
		t.Fatalf("inet_ntoa() error: %v", err)
	}
	s, err := objects.Str(result)
	if err != nil {
		t.Fatalf("inet_ntoa() result not str: %v", err)
	}
	if s != "127.0.0.1" {
		t.Fatalf("inet_ntoa([127 0 0 1]) = %q, want %q", s, "127.0.0.1")
	}
}

// TestHtonsNtohs verifies byte-swap round-trip for 16-bit values.
//
// CPython: Modules/socketmodule.c:6622 _socket_ntohs_impl / 6654 _socket_htons_impl
func TestHtonsNtohs(t *testing.T) {
	val := int64(0x1234)
	r1, err := socketHtons([]objects.Object{makeInt(val)}, nil)
	if err != nil {
		t.Fatalf("htons() error: %v", err)
	}
	r2, err := socketNtohs([]objects.Object{r1}, nil)
	if err != nil {
		t.Fatalf("ntohs() error: %v", err)
	}
	got, _ := r2.(*objects.Int).Int64()
	if got != val {
		t.Fatalf("ntohs(htons(0x%04x)) = 0x%04x, want 0x%04x", val, got, val)
	}
}

// TestHtonlNtohl verifies byte-swap round-trip for 32-bit values.
//
// CPython: Modules/socketmodule.c:6638 _socket_ntohl_impl / 6670 _socket_htonl_impl
func TestHtonlNtohl(t *testing.T) {
	val := int64(0x12345678)
	r1, err := socketHtonl([]objects.Object{makeInt(val)}, nil)
	if err != nil {
		t.Fatalf("htonl() error: %v", err)
	}
	r2, err := socketNtohl([]objects.Object{r1}, nil)
	if err != nil {
		t.Fatalf("ntohl() error: %v", err)
	}
	got, _ := r2.(*objects.Int).Int64()
	if got != val {
		t.Fatalf("ntohl(htonl(0x%08x)) = 0x%08x, want 0x%08x", val, got, val)
	}
}

// TestDefaultTimeout verifies setdefaulttimeout/getdefaulttimeout round-trip.
//
// CPython: Modules/socketmodule.c:7144 socket_getdefaulttimeout
// CPython: Modules/socketmodule.c:7165 socket_setdefaulttimeout
func TestDefaultTimeout(t *testing.T) {
	// Save original.
	orig, _ := socketGetdefaulttimeout(nil, nil)

	// Set to 5.0.
	_, err := socketSetdefaulttimeout([]objects.Object{objects.NewFloat(5.0)}, nil)
	if err != nil {
		t.Fatalf("setdefaulttimeout(5.0) error: %v", err)
	}
	got, err := socketGetdefaulttimeout(nil, nil)
	if err != nil {
		t.Fatalf("getdefaulttimeout() error: %v", err)
	}
	fv, ok := got.(*objects.Float)
	if !ok {
		t.Fatalf("getdefaulttimeout() should return float, got %T", got)
	}
	if fv.Float64() != 5.0 {
		t.Fatalf("getdefaulttimeout() = %v, want 5.0", fv.Float64())
	}

	// Reset to None (blocking).
	_, err = socketSetdefaulttimeout([]objects.Object{objects.None()}, nil)
	if err != nil {
		t.Fatalf("setdefaulttimeout(None) error: %v", err)
	}
	got2, _ := socketGetdefaulttimeout(nil, nil)
	if got2 != objects.None() {
		t.Fatalf("getdefaulttimeout() after reset should be None, got %T", got2)
	}

	// Restore original value.
	if orig != objects.None() {
		socketSetdefaulttimeout([]objects.Object{orig}, nil)
	}
}

// ---------------------------------------------------------------------------
// Socket object tests: bind, listen, accept, connect, send, recv.
// ---------------------------------------------------------------------------

// TestSocketCreate verifies that socket() creates a socket object.
//
// CPython: Modules/socketmodule.c:3453 sock_initobj
func TestSocketCreate(t *testing.T) {
	args := []objects.Object{
		makeInt(int64(AF_INET_VAL)),
		makeInt(int64(SOCK_STREAM_VAL)),
		makeInt(0),
	}
	result, err := socketSocket(args, nil)
	if err != nil {
		t.Fatalf("socket() error: %v", err)
	}
	s, ok := result.(*sockObj)
	if !ok {
		t.Fatalf("socket() result not sockObj: %T", result)
	}
	if !socketFdValid(s.fd) {
		t.Fatalf("socket() fd = %d, want valid", socketFdInt(s.fd))
	}
	// Close it.
	sockClose([]objects.Object{s}, nil)
}

// TestBindListenAcceptConnect tests a full server-client exchange over
// loopback TCP.
//
// CPython: Modules/socketmodule.c:3539 sock_bind (PyDoc_STRVAR)
// CPython: Modules/socketmodule.c:3883 listen_doc
// CPython: Modules/socketmodule.c:3029 sock_accept_impl
// CPython: Modules/socketmodule.c:3606 sock_connect_impl
// CPython: Modules/socketmodule.c:4587 sock_send_impl
// CPython: Modules/socketmodule.c:3900 sock_recv_impl
func TestBindListenAcceptConnect(t *testing.T) {
	// Create server socket.
	srvArgs := []objects.Object{
		makeInt(int64(AF_INET_VAL)),
		makeInt(int64(SOCK_STREAM_VAL)),
		makeInt(0),
	}
	srvResult, err := socketSocket(srvArgs, nil)
	if err != nil {
		t.Fatalf("socket(server) error: %v", err)
	}
	srv := srvResult.(*sockObj)
	defer sockClose([]objects.Object{srv}, nil)

	// Set SO_REUSEADDR.
	sockSetsockopt([]objects.Object{
		srv,
		makeInt(int64(SOL_SOCKET_VAL)),
		makeInt(int64(SO_REUSEADDR_VAL)),
		makeInt(1),
	}, nil)

	// Bind to localhost:0 (OS picks the port).
	bindAddr := makeTuple(makeStr("127.0.0.1"), makeInt(0))
	_, err = sockBind([]objects.Object{srv, bindAddr}, nil)
	if err != nil {
		t.Fatalf("bind() error: %v", err)
	}

	// Listen.
	_, err = sockListen([]objects.Object{srv, makeInt(5)}, nil)
	if err != nil {
		t.Fatalf("listen() error: %v", err)
	}

	// Get the actual bound port.
	nameResult, err := sockGetsockname([]objects.Object{srv}, nil)
	if err != nil {
		t.Fatalf("getsockname() error: %v", err)
	}
	nameTup := nameResult.(*objects.Tuple)
	port := nameTup.Item(1).(*objects.Int)
	portNum, _ := port.Int64()
	t.Logf("server bound to port %d", portNum)

	// Connect in a goroutine.
	type connResult struct {
		sock *sockObj
		err  error
	}
	acceptCh := make(chan connResult, 1)
	go func() {
		r, err2 := sockAccept([]objects.Object{srv}, nil)
		if err2 != nil {
			acceptCh <- connResult{nil, err2}
			return
		}
		tup := r.(*objects.Tuple)
		acceptCh <- connResult{tup.Item(0).(*sockObj), nil}
	}()

	// Client socket.
	cliResult, err := socketSocket(srvArgs, nil)
	if err != nil {
		t.Fatalf("socket(client) error: %v", err)
	}
	cli := cliResult.(*sockObj)
	defer sockClose([]objects.Object{cli}, nil)

	connAddr := makeTuple(makeStr("127.0.0.1"), port)
	_, err = sockConnect([]objects.Object{cli, connAddr}, nil)
	if err != nil {
		t.Fatalf("connect() error: %v", err)
	}

	// Wait for accept.
	cr := <-acceptCh
	if cr.err != nil {
		t.Fatalf("accept() error: %v", cr.err)
	}
	conn := cr.sock
	defer sockClose([]objects.Object{conn}, nil)

	// Send from client.
	msg := objects.NewBytes([]byte("hello"))
	_, err = sockSend([]objects.Object{cli, msg, makeInt(0)}, nil)
	if err != nil {
		t.Fatalf("send() error: %v", err)
	}

	// Recv on server side.
	recvResult, err := sockRecv([]objects.Object{conn, makeInt(1024), makeInt(0)}, nil)
	if err != nil {
		t.Fatalf("recv() error: %v", err)
	}
	recvBytes, ok := recvResult.(*objects.Bytes)
	if !ok {
		t.Fatalf("recv() returned %T, want *objects.Bytes", recvResult)
	}
	if string(recvBytes.Bytes()) != "hello" {
		t.Fatalf("recv() = %q, want %q", recvBytes.Bytes(), "hello")
	}
}

// TestFileno verifies that fileno() returns a valid fd.
//
// CPython: Modules/socketmodule.c:3783 fileno_doc
func TestFileno(t *testing.T) {
	args := []objects.Object{
		makeInt(int64(AF_INET_VAL)),
		makeInt(int64(SOCK_STREAM_VAL)),
		makeInt(0),
	}
	result, _ := socketSocket(args, nil)
	s := result.(*sockObj)
	defer sockClose([]objects.Object{s}, nil)

	fdResult, err := sockFileno([]objects.Object{s}, nil)
	if err != nil {
		t.Fatalf("fileno() error: %v", err)
	}
	fd, _ := fdResult.(*objects.Int).Int64()
	if fd < 0 {
		t.Fatalf("fileno() = %d, want >= 0", fd)
	}
}

// TestSetblockingGettimeout verifies the setblocking/gettimeout interaction.
//
// CPython: Modules/socketmodule.c:3170 setblocking_doc
func TestSetblockingGettimeout(t *testing.T) {
	args := []objects.Object{
		makeInt(int64(AF_INET_VAL)),
		makeInt(int64(SOCK_STREAM_VAL)),
		makeInt(0),
	}
	result, _ := socketSocket(args, nil)
	s := result.(*sockObj)
	defer sockClose([]objects.Object{s}, nil)

	// setblocking(False) -> timeout = 0.
	sockSetblocking([]objects.Object{s, objects.NewInt(0)}, nil)
	tResult, _ := sockGettimeout([]objects.Object{s}, nil)
	tv, ok := tResult.(*objects.Float)
	if !ok {
		t.Fatalf("gettimeout() after setblocking(False) should return float, got %T", tResult)
	}
	if tv.Float64() != 0.0 {
		t.Fatalf("gettimeout() = %v, want 0.0", tv.Float64())
	}

	// setblocking(True) -> timeout = None.
	sockSetblocking([]objects.Object{s, objects.NewInt(1)}, nil)
	tResult2, _ := sockGettimeout([]objects.Object{s}, nil)
	if tResult2 != objects.None() {
		t.Fatalf("gettimeout() after setblocking(True) should be None, got %T", tResult2)
	}
}

// TestGetaddrinfo verifies that getaddrinfo returns results for localhost.
//
// CPython: Modules/socketmodule.c:6898 socket_getaddrinfo
func TestGetaddrinfo(t *testing.T) {
	args := []objects.Object{
		makeStr("localhost"),
		makeInt(80),
		makeInt(0),                     // family
		makeInt(int64(SOCK_STREAM_VAL)), // type
		makeInt(0),                     // proto
		makeInt(0),                     // flags
	}
	result, err := socketGetaddrinfo(args, nil)
	if err != nil {
		t.Fatalf("getaddrinfo() error: %v", err)
	}
	lst, ok := result.(*objects.List)
	if !ok {
		t.Fatalf("getaddrinfo() returned %T, want *objects.List", result)
	}
	if lst.Len() == 0 {
		t.Fatal("getaddrinfo() returned empty list")
	}
	t.Logf("getaddrinfo(localhost, 80) returned %d results", lst.Len())
}

// TestModuleConstants verifies that buildModule populates key constants.
func TestModuleConstants(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule() error: %v", err)
	}
	d := m.Dict()
	for _, name := range []string{
		"AF_INET", "AF_INET6", "AF_UNIX",
		"SOCK_STREAM", "SOCK_DGRAM", "SOCK_RAW",
		"SOL_SOCKET", "SO_REUSEADDR",
		"IPPROTO_TCP", "IPPROTO_UDP",
		"SHUT_RD", "SHUT_WR", "SHUT_RDWR",
	} {
		val, err2 := d.GetItem(objects.NewStr(name))
		if err2 != nil || val == nil {
			t.Errorf("module missing constant %q", name)
		}
	}
}

// AF_INET_VAL and friends alias the syscall constants for use in this test
// file. They are resolved at compile time from the syscall package which is
// already imported by module.go in this same package.
const (
	AF_INET_VAL      = syscall.AF_INET
	SOCK_STREAM_VAL  = syscall.SOCK_STREAM
	SOL_SOCKET_VAL   = syscall.SOL_SOCKET
	SO_REUSEADDR_VAL = syscall.SO_REUSEADDR
)
