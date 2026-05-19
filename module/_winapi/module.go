// Package winapi is the gopy port of CPython's Modules/_winapi.c.
// CPython only registers _winapi on Windows. gopy registers it on every
// platform so the import statement works regardless; stdlib consumers
// (shutil, subprocess) gate their use behind `sys.platform == 'win32'`
// so the module is only actually loaded on Windows. The exposed surface
// is the integer constants stdlib/shutil.py and stdlib/subprocess.py
// import at module top level. The function surface (CreateProcess,
// CreatePipe, WaitFor*, GetStdHandle, NeedCurrentDirectoryForExePath,
// CopyFile2, etc.) is not yet ported. Attribute lookups for those names
// raise AttributeError; callers that need them (subprocess.Popen,
// shutil.copy2, shutil.which on PATH search) surface that error at call
// time on Windows.
//
// CPython: Modules/_winapi.c:1 _winapi module
// CPython: Modules/_winapi.c:3023 _winapi_exec (constant registration)
package winapi

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_winapi", buildModule)
}

// buildModule constructs the _winapi module dict with the integer
// constants CPython's _winapi_exec registers. Values are taken from
// the Windows SDK headers (process creation flags, priority classes,
// std handles, show-window flags, STARTUPINFO flags). The flag literal
// values are documented public Windows API constants.
//
// CPython: Modules/_winapi.c:3023 _winapi_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_winapi")
	d := m.Dict()

	// Process creation flags. CPython: Modules/_winapi.c:3034-3035, 3131-3134.
	consts := map[string]int64{
		"CREATE_NEW_CONSOLE":        0x00000010,
		"CREATE_NEW_PROCESS_GROUP":  0x00000200,
		"CREATE_NO_WINDOW":          0x08000000,
		"DETACHED_PROCESS":          0x00000008,
		"CREATE_DEFAULT_ERROR_MODE": 0x04000000,
		"CREATE_BREAKAWAY_FROM_JOB": 0x01000000,

		// Priority classes. CPython: Modules/_winapi.c:3124-3129.
		"ABOVE_NORMAL_PRIORITY_CLASS": 0x00008000,
		"BELOW_NORMAL_PRIORITY_CLASS": 0x00004000,
		"HIGH_PRIORITY_CLASS":         0x00000080,
		"IDLE_PRIORITY_CLASS":         0x00000040,
		"NORMAL_PRIORITY_CLASS":       0x00000020,
		"REALTIME_PRIORITY_CLASS":     0x00000100,

		// GetStdHandle identifiers. CPython: Modules/_winapi.c:3115-3117.
		// Stored as signed DWORD per Windows SDK (STD_INPUT_HANDLE = -10).
		"STD_INPUT_HANDLE":  -10,
		"STD_OUTPUT_HANDLE": -11,
		"STD_ERROR_HANDLE":  -12,

		// ShowWindow command. CPython: Modules/_winapi.c:3119.
		"SW_HIDE": 0,

		// STARTUPINFO dwFlags. CPython: Modules/_winapi.c:73, 91, 94, 97, 3101, 3107-3109.
		"STARTF_USESHOWWINDOW":    0x00000001,
		"STARTF_FORCEONFEEDBACK":  0x00000040,
		"STARTF_FORCEOFFFEEDBACK": 0x00000080,
		"STARTF_USESTDHANDLES":    0x00000100,
	}

	for name, value := range consts {
		if err := d.SetItem(objects.NewStr(name), objects.NewInt(value)); err != nil {
			return nil, err
		}
	}
	return m, nil
}
