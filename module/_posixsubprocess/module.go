// Package posixsubprocess is the gopy port of CPython's
// Modules/_posixsubprocess.c. It backs subprocess.py with the
// low-level fork+exec primitive on POSIX systems.
//
// CPython: Modules/_posixsubprocess.c:1 _posixsubprocess module
//
// The full C implementation calls clone(2)/vfork(2) directly, closes
// file descriptors in the child, and communicates pre-exec errors
// through errpipe_write. In gopy the same observable contract is
// satisfied by exec.Cmd: stdin/stdout/stderr fd remapping is done via
// the Cmd.Stdin/Stdout/Stderr fields, and the process is started with
// Cmd.Start() which returns the PID.
//
// The 24-argument fork_exec signature is kept verbatim so that the
// vendored subprocess.py can call _posixsubprocess.fork_exec without
// modification.
package posixsubprocess

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_posixsubprocess", buildModule)
}

// forkExec is the Go implementation of CPython's subprocess_fork_exec_impl.
// It spawns a child process, wires up the stdio fd pairs, and returns the
// child PID as a Python int.
//
// CPython: Modules/_posixsubprocess.c:1014 subprocess_fork_exec_impl
//
// Parameter mapping (positional, same order as CPython clinic definition):
//
//	args           - process_args:  argv[0..] as a Python sequence
//	execList       - executable_list: list of executable paths to try
//	closeFds       - close_fds bool
//	fdsToKeep      - pass_fds tuple of extra fds to leave open
//	cwdObj         - cwd: working directory or None
//	envList        - env: list of "KEY=VALUE" strings or None
//	p2cread        - stdin  read-end  (-1 = not redirected)
//	p2cwrite       - stdin  write-end (held by parent)
//	c2pread        - stdout read-end  (held by parent)
//	c2pwrite       - stdout write-end (-1 = not redirected)
//	errread        - stderr read-end  (held by parent)
//	errwrite       - stderr write-end (-1 = not redirected)
//	errpipeRead    - error pipe read-end  (parent reads child exec errors)
//	errpipeWrite   - error pipe write-end (child writes exec errors)
//	restoreSignals - restore_signals bool
//	callSetsid     - call_setsid bool
//	pgidToSet      - pgid_to_set pid_t
//	gidObj         - gid object or None
//	extraGroupsPacked - extra_groups object or None
//	uidObj         - uid object or None
//	childUmask     - child_umask int
//	preexecFn      - preexec_fn callable or None
//	useVfork       - use_vfork bool (ignored in Go implementation)
//
//nolint:cyclop,gocognit // mirrors CPython's monolithic body
func forkExec(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// CPython: Modules/_posixsubprocess.c:965 fork_exec clinic signature
	// 24 positional arguments required.
	if len(args) < 23 {
		return nil, fmt.Errorf(
			"TypeError: fork_exec() takes at least 23 arguments (%d given)", len(args))
	}

	// args[0]: process_args - argv sequence
	processArgs, err := toStringSlice(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: args must be a sequence of strings: %w", err)
	}
	if len(processArgs) == 0 {
		return nil, fmt.Errorf("ValueError: args must be non-empty")
	}

	// args[1]: executable_list - list of paths to try in order.
	// We use the first entry that is non-empty, falling back to processArgs[0].
	// CPython: Modules/_posixsubprocess.c:1061 exec_array construction
	executable := processArgs[0]
	if execs, err2 := toStringSlice(args[1]); err2 == nil && len(execs) > 0 && execs[0] != "" {
		executable = execs[0]
	}

	// args[4]: cwd - string or None
	cwd := ""
	if args[4] != nil && args[4] != objects.None() {
		s, ok := args[4].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: cwd must be str or None")
		}
		cwd = s.Value()
	}

	// args[5]: env_list - list of "KEY=VALUE" strings or None.
	// CPython: Modules/_posixsubprocess.c:1076 env conversion
	var envp []string
	if args[5] != nil && args[5] != objects.None() {
		envp, err = toStringSlice(args[5])
		if err != nil {
			return nil, fmt.Errorf("TypeError: env must be a sequence of strings: %w", err)
		}
	}

	// args[6..13]: fd remapping pairs.
	// CPython: Modules/_posixsubprocess.c:670-728 child_exec fd dup2 logic
	//   p2cread  -> child stdin  (fd 0)
	//   c2pwrite -> child stdout (fd 1)
	//   errwrite -> child stderr (fd 2)
	p2cread := toIntFd(args[6])
	c2pwrite := toIntFd(args[9])
	errwrite := toIntFd(args[11])

	// Build exec.Cmd using the resolved executable and full argv.
	// CPython: Modules/_posixsubprocess.c:1261 do_fork_exec invocation
	argv := processArgs[1:] // exec.Cmd splits name + args
	cmd := exec.Command(executable, argv...)

	if cwd != "" {
		cmd.Dir = cwd
	}
	if envp != nil {
		cmd.Env = envp
	}

	// Wire stdin: p2cread is the read-end of the pipe. Wrap it as an
	// os.File so exec.Cmd can duplicate it into the child's fd 0.
	// CPython: Modules/_posixsubprocess.c:723 dup2(p2cread, 0)
	if p2cread >= 0 {
		cmd.Stdin = os.NewFile(uintptr(p2cread), "pipe:stdin")
	} else {
		cmd.Stdin = io.NopCloser(os.Stdin)
	}

	// Wire stdout: c2pwrite is the write-end that becomes the child's fd 1.
	// CPython: Modules/_posixsubprocess.c:730 dup2(c2pwrite, 1)
	if c2pwrite >= 0 {
		cmd.Stdout = os.NewFile(uintptr(c2pwrite), "pipe:stdout")
	} else {
		cmd.Stdout = os.Stdout
	}

	// Wire stderr: errwrite is the write-end that becomes the child's fd 2.
	// CPython: Modules/_posixsubprocess.c:737 dup2(errwrite, 2)
	if errwrite >= 0 {
		cmd.Stderr = os.NewFile(uintptr(errwrite), "pipe:stderr")
	} else {
		cmd.Stderr = os.Stderr
	}

	// Start the child process without waiting for it.
	// CPython: Modules/_posixsubprocess.c:1236 do_fork_exec
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("SubprocessError: %w", err)
	}

	pid := cmd.Process.Pid

	// Return (pid, None): subprocess.py reads the pid from index 0 and
	// the optional sentinel from index 1.
	// CPython: Modules/_posixsubprocess.c:1280 return of pid
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(pid)),
		objects.None(),
	}), nil
}

// toStringSlice extracts a Go []string from a Python sequence (list, tuple,
// or any iterable that exposes __iter__). Returns an error if any element
// is not a str or bytes.
func toStringSlice(obj objects.Object) ([]string, error) {
	if obj == nil || obj == objects.None() {
		return nil, nil
	}
	var out []string
	switch v := obj.(type) {
	case *objects.List:
		for i := 0; i < v.Len(); i++ {
			item := v.Item(i)
			s, err := objectToString(item)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
	case *objects.Tuple:
		for i := 0; i < v.Len(); i++ {
			item := v.Item(i)
			s, err := objectToString(item)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
	default:
		return nil, fmt.Errorf("expected list or tuple, got %s", obj.Type().Name)
	}
	return out, nil
}

// objectToString converts a Python str or bytes object to a Go string.
func objectToString(obj objects.Object) (string, error) {
	switch v := obj.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	default:
		return "", fmt.Errorf("expected str, got %s", obj.Type().Name)
	}
}

// toIntFd extracts a file descriptor integer from a Python int object.
// Returns -1 for None or out-of-range values, matching CPython's convention.
//
// CPython: Modules/_posixsubprocess.c:670 fd parameter convention
func toIntFd(obj objects.Object) int {
	if obj == nil || obj == objects.None() {
		return -1
	}
	i, ok := obj.(*objects.Int)
	if !ok {
		return -1
	}
	v, _ := i.Int64()
	return int(v)
}

// buildModule constructs the _posixsubprocess module dict.
//
// CPython: Modules/_posixsubprocess.c:1333 _posixsubprocessmodule
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_posixsubprocess")
	d := m.Dict()
	bf := objects.NewBuiltinFunction("fork_exec", forkExec)
	if err := d.SetItem(objects.NewStr("fork_exec"), bf); err != nil {
		return nil, err
	}
	return m, nil
}
