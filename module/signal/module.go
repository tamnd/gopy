// signal module: minimal Go-backed surface. Lib/signal.py wraps
// Modules/signalmodule.c with raise_signal / signal / siginterrupt.
// unittest's runner.py installs a SIGINT handler so Ctrl-C between
// tests is graceful; this stub treats signal()/getsignal() as no-ops
// returning the previous handler value, which is sufficient because
// gopy does not yet route OS signals into the Python layer.
//
// CPython: Modules/signalmodule.c the public surface

package signal

import (
	gosig "os/signal"
	"syscall"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("signal", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("signal")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"signal", objects.NewBuiltinFunction("signal", signalFn)},
		{"getsignal", objects.NewBuiltinFunction("getsignal", getsignalFn)},
		{"set_wakeup_fd", objects.NewBuiltinFunction("set_wakeup_fd", noopInt)},
		{"siginterrupt", objects.NewBuiltinFunction("siginterrupt", noop)},
		{"raise_signal", objects.NewBuiltinFunction("raise_signal", noop)},
		{"default_int_handler", objects.NewBuiltinFunction("default_int_handler", noop)},
		{"SIG_DFL", objects.NewInt(0)},
		{"SIG_IGN", objects.NewInt(1)},
		{"SIGINT", objects.NewInt(int64(syscall.SIGINT))},
		{"SIGTERM", objects.NewInt(int64(syscall.SIGTERM))},
		{"SIGHUP", objects.NewInt(1)},
		{"SIGKILL", objects.NewInt(9)},
		{"SIGUSR1", objects.NewInt(10)},
		{"SIGUSR2", objects.NewInt(12)},
		{"SIGALRM", objects.NewInt(14)},
		{"SIGPIPE", objects.NewInt(13)},
		{"SIGSEGV", objects.NewInt(11)},
		{"Signals", signalsType},
		{"Handlers", handlersType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var (
	signalsType  = objects.NewType("Signals", []*objects.Type{objects.ObjectType()})
	handlersType = objects.NewType("Handlers", []*objects.Type{objects.ObjectType()})
	prevHandler  = map[int64]objects.Object{}
	_            = gosig.Ignore
)

// signalFn returns the previous handler and stashes the new one.
// Does not actually wire the signal up to the OS.
//
// CPython: Modules/signalmodule.c:347 signal_signal_impl
func signalFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return objects.NewInt(0), nil
	}
	sig, ok := args[0].(*objects.Int)
	if !ok {
		return objects.NewInt(0), nil
	}
	n, _ := sig.Int64()
	old := prevHandler[n]
	if old == nil {
		old = objects.NewInt(0)
	}
	prevHandler[n] = args[1]
	return old, nil
}

func getsignalFn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		if sig, ok := args[0].(*objects.Int); ok {
			n, _ := sig.Int64()
			if h, ok := prevHandler[n]; ok {
				return h, nil
			}
		}
	}
	return objects.NewInt(0), nil
}

func noop(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

func noopInt(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(-1), nil
}
