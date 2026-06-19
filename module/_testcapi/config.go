package testcapi

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// defaultConfig is the fallback configuration used when the lifecycle
// has not stamped a PyConfig onto the interpreter. The cmd entry point
// still resolves paths by hand rather than running initconfig end to
// end, so PyConfig_Get reads from the layered Python defaults, which
// already carry the runtime-true knobs the config tests inspect
// (code_debug_ranges on, write_bytecode on, optimization_level zero).
//
// CPython: Python/initconfig.c:1106 PyConfig_InitPythonConfig
var (
	defaultConfigOnce sync.Once
	defaultConfig     initconfig.PyConfig
)

func sharedDefaultConfig() *initconfig.PyConfig {
	defaultConfigOnce.Do(func() {
		defaultConfig.InitPythonConfig()
	})
	return &defaultConfig
}

// activeConfig returns the *initconfig.PyConfig the lifecycle stamped on
// the main interpreter, the live configuration _Py_GetConfig hands to
// PyConfig_Get. It falls back to the layered Python defaults until the
// cmd entry point wires initconfig through to the interpreter.
//
// CPython: Python/initconfig.c:4461 PyConfig_Get (_Py_GetConfig)
func activeConfig() (*initconfig.PyConfig, error) {
	interp := state.MainInterpreter()
	if interp != nil {
		if cfg, ok := interp.Config.(*initconfig.PyConfig); ok && cfg != nil {
			return cfg, nil
		}
	}
	return sharedDefaultConfig(), nil
}

// configGetObject wraps a resolved config member into the Python object
// PyConfig_Get returns: ints/uints become int, bools become bool, the
// optional wide strings become None when empty, and the wide-string
// lists become tuples. SYS_ATTR members are read back from the live sys
// module instead, matching config_get's use_sys delegation.
//
// CPython: Python/initconfig.c:4378 config_get
func configGetObject(name string) (objects.Object, error) {
	cfg, err := activeConfig()
	if err != nil {
		return nil, err
	}
	member, found := cfg.ConfigGet(name)
	if !found {
		// CPython: Python/initconfig.c:4451 config_unknown_name_error
		return nil, fmt.Errorf("ValueError: unknown config option name: %s", name)
	}

	// use_sys is always 1 for PyConfig_Get: a member exposed through sys
	// reads the live sys attribute so command-line and runtime overrides
	// are visible.
	//
	// CPython: Python/initconfig.c:4382 config_get (spec->sys.attr)
	if member.SysAttr != "" {
		return sysRequiredAttr(member.SysAttr)
	}

	switch member.Type {
	case initconfig.ConfigMemberInt, initconfig.ConfigMemberUint:
		return objects.NewInt(int64(member.Value.(int))), nil
	case initconfig.ConfigMemberBool:
		return objects.NewBool(member.Value.(int) != 0), nil
	case initconfig.ConfigMemberULong:
		return objects.NewIntFromBig(new(big.Int).SetUint64(member.Value.(uint64))), nil
	case initconfig.ConfigMemberWStr:
		return objects.NewStr(member.Value.(string)), nil
	case initconfig.ConfigMemberWStrOpt:
		s := member.Value.(string)
		if s == "" {
			return objects.None(), nil
		}
		return objects.NewStr(s), nil
	case initconfig.ConfigMemberWStrList:
		items := member.Value.([]string)
		objs := make([]objects.Object, len(items))
		for i, s := range items {
			objs[i] = objects.NewStr(s)
		}
		return objects.NewTuple(objs), nil
	default:
		return nil, fmt.Errorf("SystemError: unreachable config member type")
	}
}

// sysRequiredAttr mirrors _PySys_GetRequiredAttrString: read the named
// attribute from the live sys module, raising RuntimeError when sys or
// the attribute is missing.
//
// CPython: Python/sysmodule.c:99 _PySys_GetRequiredAttrString
func sysRequiredAttr(attr string) (objects.Object, error) {
	mod, ok := imp.GetModule("sys")
	if !ok {
		return nil, fmt.Errorf("RuntimeError: lost sys module")
	}
	v, err := mod.Dict().GetItem(objects.NewStr(attr))
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("RuntimeError: lost sys.%s", attr)
	}
	return v, nil
}

// configGet ports _testcapi.config_get: parse the option name and return
// PyConfig_Get(name).
//
// CPython: Modules/_testcapi/config.c:4 _testcapi_config_get
func configGet(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: config_get expected 1 argument, got %d", len(args))
	}
	name, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument must be str, not %s", args[0].Type().Name)
	}
	return configGetObject(name.Value())
}

// configGetint ports _testcapi.config_getint: PyConfig_GetInt(name),
// which is PyConfig_Get(name) constrained to an int result.
//
// CPython: Modules/_testcapi/config.c:16 _testcapi_config_getint
func configGetint(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: config_getint expected 1 argument, got %d", len(args))
	}
	name, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument must be str, not %s", args[0].Type().Name)
	}
	obj, err := configGetObject(name.Value())
	if err != nil {
		return nil, err
	}
	// CPython: Python/initconfig.c:4478 PyConfig_GetInt (PyLong_Check)
	if _, ok := obj.(*objects.Int); !ok {
		return nil, fmt.Errorf("TypeError: config option %s is not an int", name.Value())
	}
	return obj, nil
}

// configNames ports _testcapi.config_names: the frozenset of every known
// config option name.
//
// CPython: Modules/_testcapi/config.c:32 _testcapi_config_names
func configNames(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	names := initconfig.ConfigNames()
	items := make([]objects.Object, len(names))
	for i, n := range names {
		items[i] = objects.NewStr(n)
	}
	return objects.NewFrozenset(items)
}
