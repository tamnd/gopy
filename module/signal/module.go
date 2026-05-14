// _signal module: full port of Modules/signalmodule.c for POSIX/darwin.
// Registers as "_signal" so Lib/signal.py can wrap it with the
// Signals/Handlers IntEnum classes.
//
// CPython: Modules/signalmodule.c

//go:build darwin

package signal

import (
	"fmt"
	goos "os"
	gosig "os/signal"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// POSIX constants not exported by Go's syscall package on darwin.
const (
	sigNSIG    = 32 // Py_NSIG on darwin: signals 1..31
	sigBlock   = 1  // SIG_BLOCK for pthread_sigmask
	sigUnblock = 2  // SIG_UNBLOCK
	sigSetmask = 3  // SIG_SETMASK

	itimerReal    = 0 // ITIMER_REAL
	itimerVirtual = 1 // ITIMER_VIRTUAL
	itimerProf    = 2 // ITIMER_PROF

	saRestart = 0x2 // SA_RESTART flag
)

// darwinTimeval mirrors struct timeval on darwin arm64/amd64.
// CPython: Modules/signalmodule.c:776 (timeval_from_double)
type darwinTimeval struct {
	Sec  int64
	Usec int32
	_    [4]byte
}

// darwinItimerval mirrors struct itimerval on darwin.
// CPython: Modules/signalmodule.c:866 setitimer_impl
type darwinItimerval struct {
	Interval darwinTimeval
	Value    darwinTimeval
}

// darwinSigset is sigset_t on darwin: 32-bit bitmask of signals 1..31.
// CPython: Modules/signalmodule.c:969 pthread_sigmask_impl
type darwinSigset = uint32

// darwinSigaction mirrors struct __sigaction on darwin arm64/amd64.
// Used by siginterrupt_impl (sigaction(2) call).
// CPython: Modules/signalmodule.c:681 siginterrupt_impl
type darwinSigaction struct {
	Handler uintptr
	Mask    darwinSigset
	Flags   int32
}

// handlerMu guards prevHandler.
var handlerMu sync.RWMutex

// prevHandler stores the Python handler registered for each signal.
// Index is the signal number (1..31).
//
// CPython: Modules/signalmodule.c:100 Handlers[]
var prevHandler [sigNSIG]objects.Object

// wakeupFd is the current wakeup file descriptor (-1 means none).
//
// CPython: Modules/signalmodule.c:214 struct wakeupFd
var wakeupFd = int64(-1)

// sigNotify holds gosig notification channels for Python callable handlers.
var sigNotify = map[int]chan goos.Signal{}

func init() {
	_ = imp.AppendInittab("_signal", buildModule)
	// Initialize prevHandler table: SIG_DFL (0) for all signals.
	// CPython: Modules/signalmodule.c:1579 signal_get_set_handlers
	for i := range prevHandler {
		prevHandler[i] = objects.NewInt(0) // SIG_DFL
	}
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_signal")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		// Functions
		{"signal", objects.NewBuiltinFunction("signal", signalFn)},
		{"getsignal", objects.NewBuiltinFunction("getsignal", getsignalFn)},
		{"strsignal", objects.NewBuiltinFunction("strsignal", strsignalFn)},
		{"raise_signal", objects.NewBuiltinFunction("raise_signal", raiseSignalFn)},
		{"default_int_handler", objects.NewBuiltinFunction("default_int_handler", defaultIntHandlerFn)},
		{"alarm", objects.NewBuiltinFunction("alarm", alarmFn)},
		{"pause", objects.NewBuiltinFunction("pause", pauseFn)},
		{"siginterrupt", objects.NewBuiltinFunction("siginterrupt", siginterruptFn)},
		{"set_wakeup_fd", objects.NewBuiltinFunction("set_wakeup_fd", setWakeupFdFn)},
		{"setitimer", objects.NewBuiltinFunction("setitimer", setitimerFn)},
		{"getitimer", objects.NewBuiltinFunction("getitimer", getitimerFn)},
		{"pthread_sigmask", objects.NewBuiltinFunction("pthread_sigmask", pthreadSigmaskFn)},
		{"sigpending", objects.NewBuiltinFunction("sigpending", sigpendingFn)},
		{"sigwait", objects.NewBuiltinFunction("sigwait", sigwaitFn)},
		{"valid_signals", objects.NewBuiltinFunction("valid_signals", validSignalsFn)},
		{"pthread_kill", objects.NewBuiltinFunction("pthread_kill", pthreadKillFn)},
		// SIG_DFL / SIG_IGN sentinels
		{"SIG_DFL", objects.NewInt(0)},
		{"SIG_IGN", objects.NewInt(1)},
		// pthread_sigmask how-constants
		{"SIG_BLOCK", objects.NewInt(sigBlock)},
		{"SIG_UNBLOCK", objects.NewInt(sigUnblock)},
		{"SIG_SETMASK", objects.NewInt(sigSetmask)},
		// NSIG
		{"NSIG", objects.NewInt(sigNSIG)},
		// Signal numbers — all that CPython exports on darwin/macOS.
		// CPython: Modules/signalmodule.c:1412 signal_add_constants
		{"SIGHUP", objects.NewInt(int64(syscall.SIGHUP))},
		{"SIGINT", objects.NewInt(int64(syscall.SIGINT))},
		{"SIGQUIT", objects.NewInt(int64(syscall.SIGQUIT))},
		{"SIGILL", objects.NewInt(int64(syscall.SIGILL))},
		{"SIGTRAP", objects.NewInt(int64(syscall.SIGTRAP))},
		{"SIGABRT", objects.NewInt(int64(syscall.SIGABRT))},
		{"SIGIOT", objects.NewInt(int64(syscall.SIGIOT))},
		{"SIGEMT", objects.NewInt(int64(syscall.SIGEMT))},
		{"SIGFPE", objects.NewInt(int64(syscall.SIGFPE))},
		{"SIGKILL", objects.NewInt(int64(syscall.SIGKILL))},
		{"SIGBUS", objects.NewInt(int64(syscall.SIGBUS))},
		{"SIGSEGV", objects.NewInt(int64(syscall.SIGSEGV))},
		{"SIGSYS", objects.NewInt(int64(syscall.SIGSYS))},
		{"SIGPIPE", objects.NewInt(int64(syscall.SIGPIPE))},
		{"SIGALRM", objects.NewInt(int64(syscall.SIGALRM))},
		{"SIGTERM", objects.NewInt(int64(syscall.SIGTERM))},
		{"SIGURG", objects.NewInt(int64(syscall.SIGURG))},
		{"SIGSTOP", objects.NewInt(int64(syscall.SIGSTOP))},
		{"SIGTSTP", objects.NewInt(int64(syscall.SIGTSTP))},
		{"SIGCONT", objects.NewInt(int64(syscall.SIGCONT))},
		{"SIGCHLD", objects.NewInt(int64(syscall.SIGCHLD))},
		{"SIGTTIN", objects.NewInt(int64(syscall.SIGTTIN))},
		{"SIGTTOU", objects.NewInt(int64(syscall.SIGTTOU))},
		{"SIGIO", objects.NewInt(int64(syscall.SIGIO))},
		{"SIGXCPU", objects.NewInt(int64(syscall.SIGXCPU))},
		{"SIGXFSZ", objects.NewInt(int64(syscall.SIGXFSZ))},
		{"SIGVTALRM", objects.NewInt(int64(syscall.SIGVTALRM))},
		{"SIGPROF", objects.NewInt(int64(syscall.SIGPROF))},
		{"SIGWINCH", objects.NewInt(int64(syscall.SIGWINCH))},
		{"SIGINFO", objects.NewInt(int64(syscall.SIGINFO))},
		{"SIGUSR1", objects.NewInt(int64(syscall.SIGUSR1))},
		{"SIGUSR2", objects.NewInt(int64(syscall.SIGUSR2))},
		// Interval-timer which-constants
		{"ITIMER_REAL", objects.NewInt(itimerReal)},
		{"ITIMER_VIRTUAL", objects.NewInt(itimerVirtual)},
		{"ITIMER_PROF", objects.NewInt(itimerProf)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------- helper: extract int arg ----------

func intArg(args []objects.Object, i int) (int64, bool) {
	if i >= len(args) {
		return 0, false
	}
	v, ok := args[i].(*objects.Int)
	if !ok {
		return 0, false
	}
	n, _ := v.Int64()
	return n, true
}

func floatArg(args []objects.Object, kwargs map[string]objects.Object, i int, kw string) (float64, bool) {
	var obj objects.Object
	if i < len(args) {
		obj = args[i]
	} else if kw != "" {
		obj = kwargs[kw]
	}
	if obj == nil {
		return 0, false
	}
	switch v := obj.(type) {
	case *objects.Int:
		n, _ := v.Int64()
		return float64(n), true
	case *objects.Float:
		return v.Float64(), true
	}
	return 0, false
}

// ---------- signal_default_int_handler_impl ----------
// CPython: Modules/signalmodule.c:232 signal_default_int_handler_impl

func defaultIntHandlerFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("KeyboardInterrupt")
}

// ---------- signal_signal_impl ----------
// CPython: Modules/signalmodule.c:480 signal_signal_impl

func signalFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: signal() takes exactly 2 arguments (%d given)", len(args))
	}
	signum, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	if signum < 1 || signum >= sigNSIG {
		return nil, fmt.Errorf("ValueError: signal number out of range")
	}
	handler := args[1]

	handlerMu.Lock()
	old := prevHandler[signum]
	prevHandler[signum] = handler
	handlerMu.Unlock()

	sig := syscall.Signal(signum)
	// Wire the OS-level signal disposition.
	n, isInt := intArg([]objects.Object{handler}, 0)
	if isInt {
		switch n {
		case 0: // SIG_DFL
			gosig.Reset(sig)
		case 1: // SIG_IGN
			gosig.Ignore(sig)
		}
	} else {
		// Callable: subscribe via gosig.Notify so signals are not lost.
		ch := make(chan goos.Signal, 1)
		gosig.Notify(ch, sig)
		sigNotify[int(signum)] = ch
	}

	if old == nil {
		old = objects.NewInt(0)
	}
	return old, nil
}

// ---------- signal_getsignal_impl ----------
// CPython: Modules/signalmodule.c:568 signal_getsignal_impl

func getsignalFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	signum, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	if signum < 1 || signum >= sigNSIG {
		return nil, fmt.Errorf("ValueError: signal number out of range")
	}
	handlerMu.RLock()
	h := prevHandler[signum]
	handlerMu.RUnlock()
	if h == nil {
		h = objects.NewInt(0)
	}
	return h, nil
}

// ---------- signal_strsignal_impl ----------
// CPython: Modules/signalmodule.c:601 signal_strsignal_impl

func strsignalFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: strsignal() takes exactly 1 argument (%d given)", len(args))
	}
	n, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[0].Type().Name)
	}
	v, _ := n.Int64()
	if v < 1 || v >= sigNSIG {
		return nil, fmt.Errorf("ValueError: signal number out of range")
	}
	s := strings.TrimPrefix(syscall.Signal(v).String(), "signal ")
	if s != "" {
		s = strings.ToUpper(s[:1]) + s[1:]
	}
	return objects.NewStr(s), nil
}

// ---------- signal_raise_signal_impl ----------
// CPython: Modules/signalmodule.c:439 signal_raise_signal_impl

func raiseSignalFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	signum, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	pid := syscall.Getpid()
	if err := syscall.Kill(pid, syscall.Signal(signum)); err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.None(), nil
}

// ---------- signal_alarm_impl ----------
// CPython: Modules/signalmodule.c:394 signal_alarm_impl
// On darwin, alarm(2) is equivalent to setitimer(ITIMER_REAL, ...).
// The return value is the remaining seconds of the previous alarm.

func alarmFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	secs, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	if secs < 0 {
		return nil, fmt.Errorf("ValueError: alarm() argument must be a non-negative integer")
	}
	newv := darwinItimerval{Value: darwinTimeval{Sec: secs}}
	var oldv darwinItimerval
	if _, _, errno := syscall.Syscall(syscall.SYS_SETITIMER,
		uintptr(itimerReal),
		uintptr(unsafe.Pointer(&newv)),
		uintptr(unsafe.Pointer(&oldv))); errno != 0 {
		return nil, fmt.Errorf("OSError: %w", errno)
	}
	remaining := oldv.Value.Sec
	if oldv.Value.Usec > 0 {
		remaining++
	}
	return objects.NewInt(remaining), nil
}

// ---------- signal_pause_impl ----------
// CPython: Modules/signalmodule.c:412 signal_pause_impl
// Suspends the goroutine until any signal arrives via gosig.Notify.

func pauseFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// Subscribe to all catchable signals and block until one arrives.
	ch := make(chan goos.Signal, 1)
	sigs := make([]goos.Signal, 0, sigNSIG-1)
	for i := 1; i < sigNSIG; i++ {
		s := syscall.Signal(i)
		if s != syscall.SIGKILL && s != syscall.SIGSTOP {
			sigs = append(sigs, s)
		}
	}
	gosig.Notify(ch, sigs...)
	defer gosig.Stop(ch)
	<-ch
	return objects.None(), nil
}

// ---------- signal_siginterrupt_impl ----------
// CPython: Modules/signalmodule.c:681 signal_siginterrupt_impl
// Modifies SA_RESTART in the signal's sigaction flags.

func siginterruptFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	signum, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	flag, ok2 := intArg(args, 1)
	if !ok2 {
		return nil, fmt.Errorf("TypeError: an integer is required for flag")
	}
	if signum < 1 || signum >= sigNSIG {
		return nil, fmt.Errorf("ValueError: signal number out of range")
	}
	// Read current sigaction.
	var act darwinSigaction
	if _, _, errno := syscall.Syscall(syscall.SYS_SIGACTION,
		uintptr(signum),
		0,
		uintptr(unsafe.Pointer(&act))); errno != 0 {
		return nil, fmt.Errorf("OSError: %w", errno)
	}
	// Modify SA_RESTART flag.
	if flag != 0 {
		act.Flags &^= saRestart // clear SA_RESTART → interrupting mode
	} else {
		act.Flags |= saRestart // set SA_RESTART → restarting mode
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_SIGACTION,
		uintptr(signum),
		uintptr(unsafe.Pointer(&act)),
		0); errno != 0 {
		return nil, fmt.Errorf("OSError: %w", errno)
	}
	return objects.None(), nil
}

// ---------- signal_set_wakeup_fd_impl ----------
// CPython: Modules/signalmodule.c:728 signal_set_wakeup_fd_impl
// Stores the wakeup file descriptor and returns the old one.

func setWakeupFdFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	fd, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	old := wakeupFd
	wakeupFd = fd
	return objects.NewInt(old), nil
}

// ---------- signal_setitimer_impl ----------
// CPython: Modules/signalmodule.c:866 signal_setitimer_impl

func setitimerFn(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	which, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required for which")
	}
	secs, ok2 := floatArg(args, kwargs, 1, "seconds")
	if !ok2 {
		return nil, fmt.Errorf("TypeError: a number is required for seconds")
	}
	interval, _ := floatArg(args, kwargs, 2, "interval")

	newv := darwinItimerval{
		Value:    dtvFromFloat(secs),
		Interval: dtvFromFloat(interval),
	}
	var oldv darwinItimerval
	if _, _, errno := syscall.Syscall(syscall.SYS_SETITIMER,
		uintptr(which),
		uintptr(unsafe.Pointer(&newv)),
		uintptr(unsafe.Pointer(&oldv))); errno != 0 {
		return nil, fmt.Errorf("ItimerError: [Errno %d] %w", int(errno), errno)
	}
	return itimervalToTuple(&oldv), nil
}

// ---------- signal_getitimer_impl ----------
// CPython: Modules/signalmodule.c:903 signal_getitimer_impl

func getitimerFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	which, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required for which")
	}
	var oldv darwinItimerval
	if _, _, errno := syscall.Syscall(syscall.SYS_GETITIMER,
		uintptr(which),
		uintptr(unsafe.Pointer(&oldv)),
		0); errno != 0 {
		return nil, fmt.Errorf("ItimerError: [Errno %d] %w", int(errno), errno)
	}
	return itimervalToTuple(&oldv), nil
}

// ---------- signal_pthread_sigmask_impl ----------
// CPython: Modules/signalmodule.c:969 signal_pthread_sigmask_impl

func pthreadSigmaskFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	how, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required for how")
	}
	mask, err := sigsetFromArg(args, 1)
	if err != nil {
		return nil, err
	}
	var prev darwinSigset
	if _, _, errno := syscall.Syscall(syscall.SYS___PTHREAD_SIGMASK,
		uintptr(how),
		uintptr(unsafe.Pointer(&mask)),
		uintptr(unsafe.Pointer(&prev))); errno != 0 {
		return nil, fmt.Errorf("OSError: %w", errno)
	}
	return sigsetToSet(prev), nil
}

// ---------- signal_sigpending_impl ----------
// CPython: Modules/signalmodule.c:1004 signal_sigpending_impl

func sigpendingFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	var mask darwinSigset
	if _, _, errno := syscall.Syscall(syscall.SYS_SIGPENDING,
		uintptr(unsafe.Pointer(&mask)),
		0, 0); errno != 0 {
		return nil, fmt.Errorf("OSError: %w", errno)
	}
	return sigsetToSet(mask), nil
}

// ---------- signal_sigwait_impl ----------
// CPython: Modules/signalmodule.c:1034 signal_sigwait_impl

func sigwaitFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mask, err := sigsetFromArg(args, 0)
	if err != nil {
		return nil, err
	}
	var sig int32
	if _, _, errno := syscall.Syscall(syscall.SYS___SIGWAIT,
		uintptr(unsafe.Pointer(&mask)),
		uintptr(unsafe.Pointer(&sig)),
		0); errno != 0 {
		return nil, fmt.Errorf("OSError: %w", errno)
	}
	return objects.NewInt(int64(sig)), nil
}

// ---------- signal_valid_signals_impl ----------
// CPython: Modules/signalmodule.c:1065 signal_valid_signals_impl
// On darwin, uses sigfillset to enumerate all valid signals.

func validSignalsFn(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// Emulate sigfillset: all signals 1..NSIG-1 are valid on darwin.
	result := objects.NewSet()
	for i := int64(1); i < sigNSIG; i++ {
		if err := result.Add(objects.NewInt(i)); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// ---------- signal_pthread_kill_impl ----------
// CPython: Modules/signalmodule.c:1283 signal_pthread_kill_impl

func pthreadKillFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	tid, ok := intArg(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required for thread_id")
	}
	signum, ok := intArg(args, 1)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required for signalnum")
	}
	if _, _, errno := syscall.Syscall(syscall.SYS___PTHREAD_KILL,
		uintptr(tid),
		uintptr(signum),
		0); errno != 0 {
		return nil, fmt.Errorf("OSError: %w", errno)
	}
	return objects.None(), nil
}

// ---------- internal helpers ----------

// dtvFromFloat converts a float64 number of seconds to a darwinTimeval.
// CPython: Modules/signalmodule.c:776 timeval_from_double
func dtvFromFloat(secs float64) darwinTimeval {
	whole := int64(secs)
	frac := secs - float64(whole)
	return darwinTimeval{Sec: whole, Usec: int32(frac * 1e6)}
}

// itimervalToTuple builds a (value, interval) float tuple.
// CPython: Modules/signalmodule.c:837 itimer_retval
func itimervalToTuple(v *darwinItimerval) objects.Object {
	toFloat := func(tv darwinTimeval) objects.Object {
		f := float64(tv.Sec) + float64(tv.Usec)*1e-6
		return objects.NewFloat(f)
	}
	return objects.NewTuple([]objects.Object{toFloat(v.Value), toFloat(v.Interval)})
}

// sigsetFromArg interprets a Python set/frozenset/iterable as a darwinSigset bitmask.
// CPython: Modules/signalmodule.c clinic_sigset_converter
func sigsetFromArg(args []objects.Object, i int) (darwinSigset, error) {
	if i >= len(args) {
		return 0, fmt.Errorf("TypeError: sigset argument is required")
	}
	arg := args[i]
	var mask darwinSigset
	// Accept a Python set/frozenset of ints, or a single int.
	switch v := arg.(type) {
	case *objects.Int:
		n, _ := v.Int64()
		mask = darwinSigset(n)
	case *objects.Set:
		for _, item := range v.Items() {
			n, ok := intArg([]objects.Object{item}, 0)
			if !ok {
				return 0, fmt.Errorf("TypeError: sigset items must be integers")
			}
			if n >= 1 && n < sigNSIG {
				mask |= 1 << uint(n)
			}
		}
	default:
		return 0, fmt.Errorf("TypeError: sigset must be a set of integers")
	}
	return mask, nil
}

// sigsetToSet converts a darwinSigset bitmask to a Python set of ints.
// CPython: Modules/signalmodule.c:961 sigset_to_set
func sigsetToSet(mask darwinSigset) objects.Object {
	result := objects.NewSet()
	for sig := int64(1); sig < sigNSIG; sig++ {
		if mask&(1<<uint(sig)) != 0 {
			_ = result.Add(objects.NewInt(sig))
		}
	}
	return result
}

// Ensure gosig is used (the Notify/Stop/Reset/Ignore calls reference it).
var _ = gosig.Notify
