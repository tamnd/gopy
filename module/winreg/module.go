// Package winreg is the gopy port of CPython's PC/winreg.c.
// CPython only registers winreg on Windows. gopy registers it on every
// platform so importlib._bootstrap_external (which does `import winreg`
// at module top level under `if sys.platform == 'win32'`) imports
// regardless; stdlib consumers gate their use behind a win32 check so the
// module is only actually loaded on Windows. The exposed surface is the
// HKEY_*/KEY_*/REG_* integer constants and the `error` alias that
// _bootstrap_external's WindowsRegistryFinder reads at find_spec time.
// The function surface (OpenKey, QueryValue, EnumKey, CreateKey, the PyHKEY
// type, etc.) is not yet ported: it requires the Windows registry syscalls
// (advapi32), which gopy has no host binding for, and the default meta_path
// never installs WindowsRegistryFinder, so those names are never reached.
// Attribute lookups for the unported names raise AttributeError; that
// surfaces at call time on Windows for the deprecated registry finder only.
//
// CPython: PC/winreg.c:1 winreg module
// CPython: PC/winreg.c:2121 exec_module (constant registration)
package winreg

import (
	"math/big"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("winreg", buildModule)
}

// buildModule constructs the winreg module dict with the constants
// CPython's exec_module registers. The HKEY_* predefined handles are the
// sign-extended 64-bit pointer values PyLong_FromVoidPtr yields on a 64-bit
// build (e.g. HKEY_CLASSES_ROOT == (HKEY)0x80000000 widened to
// 0xFFFFFFFF80000000). The KEY_*/REG_* access and value-type constants are
// the documented Windows SDK (winnt.h, winreg.h) literals inskey/ADD_INT
// register.
//
// CPython: PC/winreg.c:2121 exec_module
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("winreg")
	d := m.Dict()

	// Predefined HKEY handles. CPython: PC/winreg.c:2125-2132 inskey.
	// On a 64-bit build PyLong_FromVoidPtr sign-extends the (HKEY)0x8000000N
	// pointer to a 64-bit value that overflows int64, so they are built from
	// big.Int.
	hkeys := map[string]uint64{
		"HKEY_CLASSES_ROOT":     0xFFFFFFFF80000000,
		"HKEY_CURRENT_USER":     0xFFFFFFFF80000001,
		"HKEY_LOCAL_MACHINE":    0xFFFFFFFF80000002,
		"HKEY_USERS":            0xFFFFFFFF80000003,
		"HKEY_PERFORMANCE_DATA": 0xFFFFFFFF80000004,
		"HKEY_CURRENT_CONFIG":   0xFFFFFFFF80000005,
		"HKEY_DYN_DATA":         0xFFFFFFFF80000006,
	}
	for name, value := range hkeys {
		b := new(big.Int).SetUint64(value)
		if err := d.SetItem(objects.NewStr(name), objects.NewIntFromBig(b)); err != nil {
			return nil, err
		}
	}

	// Access-right and value-type constants. CPython: PC/winreg.c:2135-2210
	// ADD_INT. Values are the winnt.h / winreg.h public literals.
	consts := map[string]int64{
		// Registry access rights (winnt.h).
		"KEY_QUERY_VALUE":        0x0001,
		"KEY_SET_VALUE":          0x0002,
		"KEY_CREATE_SUB_KEY":     0x0004,
		"KEY_ENUMERATE_SUB_KEYS": 0x0008,
		"KEY_NOTIFY":             0x0010,
		"KEY_CREATE_LINK":        0x0020,
		"KEY_WOW64_64KEY":        0x0100,
		"KEY_WOW64_32KEY":        0x0200,
		"KEY_READ":               0x20019,
		"KEY_WRITE":              0x20006,
		"KEY_EXECUTE":            0x20019,
		"KEY_ALL_ACCESS":         0xF003F,

		// RegCreateKeyEx / RegOpenKeyEx options (winnt.h).
		"REG_OPTION_RESERVED":       0x0000,
		"REG_OPTION_NON_VOLATILE":   0x0000,
		"REG_OPTION_VOLATILE":       0x0001,
		"REG_OPTION_CREATE_LINK":    0x0002,
		"REG_OPTION_BACKUP_RESTORE": 0x0004,
		"REG_OPTION_OPEN_LINK":      0x0008,
		"REG_LEGAL_OPTION":          0x000F,

		// RegCreateKeyEx disposition (winnt.h).
		"REG_CREATED_NEW_KEY":     0x00000001,
		"REG_OPENED_EXISTING_KEY": 0x00000002,

		// RegRestoreKey / RegReplaceKey flags (winnt.h).
		"REG_WHOLE_HIVE_VOLATILE": 0x00000001,
		"REG_REFRESH_HIVE":        0x00000002,
		"REG_NO_LAZY_FLUSH":       0x00000004,

		// RegNotifyChangeKeyValue filter (winnt.h).
		"REG_NOTIFY_CHANGE_NAME":       0x00000001,
		"REG_NOTIFY_CHANGE_ATTRIBUTES": 0x00000002,
		"REG_NOTIFY_CHANGE_LAST_SET":   0x00000004,
		"REG_NOTIFY_CHANGE_SECURITY":   0x00000008,
		"REG_LEGAL_CHANGE_FILTER":      0x0000000F,

		// Registry value types (winnt.h).
		"REG_NONE":                       0,
		"REG_SZ":                         1,
		"REG_EXPAND_SZ":                  2,
		"REG_BINARY":                     3,
		"REG_DWORD":                      4,
		"REG_DWORD_LITTLE_ENDIAN":        4,
		"REG_DWORD_BIG_ENDIAN":           5,
		"REG_LINK":                       6,
		"REG_MULTI_SZ":                   7,
		"REG_RESOURCE_LIST":              8,
		"REG_FULL_RESOURCE_DESCRIPTOR":   9,
		"REG_RESOURCE_REQUIREMENTS_LIST": 10,
		"REG_QWORD":                      11,
		"REG_QWORD_LITTLE_ENDIAN":        11,
	}
	for name, value := range consts {
		if err := d.SetItem(objects.NewStr(name), objects.NewInt(value)); err != nil {
			return nil, err
		}
	}

	// winreg.error is an alias of OSError. CPython: PC/winreg.c:2218
	// (st->PyHKEY_Type aside, the module sets error = PyExc_OSError).
	if err := d.SetItem(objects.NewStr("error"), errors.PyExc_OSError); err != nil {
		return nil, err
	}

	return m, nil
}
