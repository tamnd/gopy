package sys

import (
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/objects"
)

// UpdateConfig stamps the PyConfig-derived attributes onto d:
// argv, orig_argv, path, warnoptions, _xoptions, executable, the
// prefix family, platlibdir, and pycache_prefix. Modules dict is
// installed once 1623 lands the import system.
//
// This is the v0.7 port of _PySys_UpdateConfig: stringly-typed
// fields (executable, prefix, ...) become str; wide-string lists
// (argv, path, warnoptions) become tuples of str (CPython uses
// list, but a tuple is the right thing to expose until builtins
// wires up list literals).
//
// CPython: Python/sysmodule.c:3951 _PySys_UpdateConfig
func UpdateConfig(d *objects.Dict, cfg *initconfig.PyConfig) error {
	if cfg.ModuleSearchPathsSet != 0 {
		if err := setItem(d, "path", strList(cfg.ModuleSearchPaths)); err != nil {
			return err
		}
	} else if has, _ := d.Contains(objects.NewStr("path")); !has {
		if err := setItem(d, "path", objects.NewTuple(nil)); err != nil {
			return err
		}
	}

	if err := setItem(d, "argv", strList(cfg.Argv)); err != nil {
		return err
	}
	if err := setItem(d, "orig_argv", strList(cfg.OrigArgv)); err != nil {
		return err
	}
	if err := setItem(d, "warnoptions", strList(cfg.WarnOptions)); err != nil {
		return err
	}
	if err := setItem(d, "_xoptions", xOptionsDict(cfg.XOptions)); err != nil {
		return err
	}
	if err := setItem(d, "flags", makeFlags(cfg)); err != nil {
		return err
	}

	for _, kv := range []struct {
		name  string
		value string
	}{
		{"executable", cfg.Executable},
		{"_base_executable", cfg.BaseExecutable},
		{"prefix", cfg.Prefix},
		{"base_prefix", cfg.BasePrefix},
		{"exec_prefix", cfg.ExecPrefix},
		{"base_exec_prefix", cfg.BaseExecPrefix},
		{"platlibdir", cfg.Platlibdir},
	} {
		if err := setStr(d, kv.name, kv.value); err != nil {
			return err
		}
	}

	if cfg.PycachePrefix != "" {
		if err := setStr(d, "pycache_prefix", cfg.PycachePrefix); err != nil {
			return err
		}
	} else {
		if err := setItem(d, "pycache_prefix", objects.None()); err != nil {
			return err
		}
	}

	return nil
}

// strList lifts a Go string slice to a tuple of str. The CPython
// equivalent is _PyWideStringList_AsList, which returns a list;
// once the list type stabilizes the return changes from Tuple to
// List without affecting callers that read it.
//
// CPython: Python/sysmodule.c:3958 COPY_LIST
func strList(items []string) *objects.Tuple {
	out := make([]objects.Object, len(items))
	for i, s := range items {
		out[i] = objects.NewStr(s)
	}
	return objects.NewTuple(out)
}

// xOptionsDict converts the -X foo=bar list to the dict shape
// CPython exposes through sys._xoptions: foo without "=" maps to
// True; foo=bar maps to "bar".
//
// CPython: Python/initconfig.c:_PyConfig_CreateXOptionsDict
func xOptionsDict(opts []string) *objects.Dict {
	out := objects.NewDict()
	for _, opt := range opts {
		k, v := splitXOption(opt)
		if v == nil {
			_ = out.SetItem(objects.NewStr(k), objects.True())
			continue
		}
		_ = out.SetItem(objects.NewStr(k), objects.NewStr(*v))
	}
	return out
}

// splitXOption parses one -X argument. Returns the key and the
// value; value is nil when no "=" appears.
//
// CPython: Python/initconfig.c:config_init_xoptions split on '='
func splitXOption(opt string) (string, *string) {
	for i := 0; i < len(opt); i++ {
		if opt[i] == '=' {
			rest := opt[i+1:]
			return opt[:i], &rest
		}
	}
	return opt, nil
}
