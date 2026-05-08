// time module: minimal Go-backed surface around Go's time package.
// Lib/time covers monotonic / perf_counter / time / process_time /
// sleep / strftime; this stub wires those that unittest pulls in
// (time, monotonic, perf_counter, sleep) so the import resolves and
// timing in TextTestRunner produces meaningful numbers.
//
// CPython: Modules/timemodule.c the public surface

package time

import (
	gotime "time"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("time", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("time")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"time", objects.NewBuiltinFunction("time", nowFloat)},
		{"monotonic", objects.NewBuiltinFunction("monotonic", nowFloat)},
		{"perf_counter", objects.NewBuiltinFunction("perf_counter", nowFloat)},
		{"process_time", objects.NewBuiltinFunction("process_time", nowFloat)},
		{"time_ns", objects.NewBuiltinFunction("time_ns", nowNs)},
		{"monotonic_ns", objects.NewBuiltinFunction("monotonic_ns", nowNs)},
		{"perf_counter_ns", objects.NewBuiltinFunction("perf_counter_ns", nowNs)},
		{"sleep", objects.NewBuiltinFunction("sleep", sleep)},
		{"strftime", objects.NewBuiltinFunction("strftime", strftimeStub)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func nowFloat(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewFloat(float64(gotime.Now().UnixNano()) / 1e9), nil
}

func nowNs(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(gotime.Now().UnixNano()), nil
}

func sleep(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		switch v := args[0].(type) {
		case *objects.Float:
			gotime.Sleep(gotime.Duration(v.Float64() * float64(gotime.Second)))
		case *objects.Int:
			n, _ := v.Int64()
			gotime.Sleep(gotime.Duration(n) * gotime.Second)
		}
	}
	return objects.None(), nil
}

func strftimeStub(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		s, _ := objects.Str(args[0])
		return objects.NewStr(s), nil
	}
	return objects.NewStr(""), nil
}
