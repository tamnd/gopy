// Tests for the _posixsubprocess builtin module. They exercise fork_exec
// at the Go layer: argument validation, fd wiring, and successful child
// process launch with PID recovery.
package posixsubprocess

import (
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// loadModule builds the _posixsubprocess module and returns it.
func loadModule(t *testing.T) *objects.Module {
	t.Helper()
	fn := imp.FindInitFunc("_posixsubprocess")
	if fn == nil {
		t.Fatal("imp.FindInitFunc(\"_posixsubprocess\") = nil")
	}
	m, err := fn()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	return m
}

// getForkExec looks up the fork_exec callable from the module dict.
func getForkExec(t *testing.T, m *objects.Module) objects.Object {
	t.Helper()
	v, err := m.Dict().GetItem(objects.NewStr("fork_exec"))
	if err != nil {
		t.Fatalf("missing fork_exec: %v", err)
	}
	return v
}

// makeArgs constructs the 23-element positional argument list that
// subprocess.py passes to fork_exec. All optional parameters are set to
// safe no-op values.
//
// CPython: Modules/_posixsubprocess.c:965 fork_exec clinic positional order
func makeArgs(argv []string, executable string, cwd string) []objects.Object {
	// Build argv list.
	argItems := make([]objects.Object, len(argv))
	for i, s := range argv {
		argItems[i] = objects.NewStr(s)
	}
	argList := objects.NewList(argItems)

	// Build executable list (first entry wins).
	execList := objects.NewList([]objects.Object{objects.NewStr(executable)})

	var cwdObj objects.Object = objects.None()
	if cwd != "" {
		cwdObj = objects.NewStr(cwd)
	}

	intObj := func(n int) objects.Object { return objects.NewInt(int64(n)) }

	// 23 arguments in CPython clinic order:
	return []objects.Object{
		argList,                              // args (process_args)
		execList,                             // executable_list
		objects.False(),                      // close_fds
		objects.NewTuple([]objects.Object{}), // pass_fds
		cwdObj,                               // cwd
		objects.None(),                       // env (inherit)
		intObj(-1),                           // p2cread
		intObj(-1),                           // p2cwrite
		intObj(-1),                           // c2pread
		intObj(-1),                           // c2pwrite
		intObj(-1),                           // errread
		intObj(-1),                           // errwrite
		intObj(-1),                           // errpipe_read
		intObj(-1),                           // errpipe_write
		objects.True(),                       // restore_signals
		objects.False(),                      // call_setsid
		intObj(-1),                           // pgid_to_set
		objects.None(),                       // gid
		objects.None(),                       // extra_groups
		objects.None(),                       // uid
		intObj(-1),                           // child_umask
		objects.None(),                       // preexec_fn
		objects.False(),                      // use_vfork
	}
}

// TestBuildModule verifies that the module dict contains fork_exec.
func TestBuildModule(t *testing.T) {
	m := loadModule(t)
	fn := getForkExec(t, m)
	if fn == nil {
		t.Fatal("fork_exec is nil")
	}
}

// TestForkExecTrue spawns /usr/bin/true (always exits 0) and checks that
// fork_exec returns a positive PID.
//
// CPython: Modules/_posixsubprocess.c:1261 do_fork_exec invocation
func TestForkExecTrue(t *testing.T) {
	m := loadModule(t)
	fn := getForkExec(t, m)

	a := makeArgs([]string{"/usr/bin/true"}, "/usr/bin/true", "")
	result, err := fn.Type().Call(fn, a, nil)
	if err != nil {
		t.Fatalf("fork_exec: %v", err)
	}
	// CPython returns PyLong_FromPid(pid): fork_exec yields the child PID as
	// a plain int, not a tuple. subprocess.py assigns self.pid directly.
	pidObj, ok := result.(*objects.Int)
	if !ok {
		t.Fatalf("expected int pid, got %T", result)
	}
	pid, _ := pidObj.Int64()
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}
}

// TestForkExecEcho spawns /bin/echo with an argument and verifies that the
// returned PID is valid. This exercises the argv-forwarding path.
//
// CPython: Modules/_posixsubprocess.c:1014 subprocess_fork_exec_impl
func TestForkExecEcho(t *testing.T) {
	m := loadModule(t)
	fn := getForkExec(t, m)

	a := makeArgs([]string{"/bin/echo", "hello"}, "/bin/echo", "")
	result, err := fn.Type().Call(fn, a, nil)
	if err != nil {
		t.Fatalf("fork_exec /bin/echo: %v", err)
	}
	// CPython: Modules/_posixsubprocess.c:1325 return PyLong_FromPid(pid).
	pidObj, ok := result.(*objects.Int)
	if !ok {
		t.Fatalf("expected int pid, got %T", result)
	}
	pid, _ := pidObj.Int64()
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}
}

// TestForkExecMissingArgs verifies that fewer than 23 arguments returns a
// TypeError rather than panicking.
//
// CPython: Modules/_posixsubprocess.c:965 clinic arg count enforcement
func TestForkExecMissingArgs(t *testing.T) {
	m := loadModule(t)
	fn := getForkExec(t, m)

	// Pass only 5 args (way below the required 23).
	short := []objects.Object{
		objects.NewStr("x"),
		objects.NewStr("x"),
		objects.False(),
		objects.None(),
		objects.None(),
	}
	_, err := fn.Type().Call(fn, short, nil)
	if err == nil {
		t.Fatal("expected error for too few arguments, got nil")
	}
}

// TestForkExecBadExecutable verifies that an invalid executable path returns
// a SubprocessError.
//
// CPython: Modules/_posixsubprocess.c:1236 do_fork_exec exec failure
func TestForkExecBadExecutable(t *testing.T) {
	m := loadModule(t)
	fn := getForkExec(t, m)

	a := makeArgs([]string{"/no/such/binary"}, "/no/such/binary", "")
	_, err := fn.Type().Call(fn, a, nil)
	if err == nil {
		t.Fatal("expected error for missing executable, got nil")
	}
}
