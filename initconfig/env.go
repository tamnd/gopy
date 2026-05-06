package initconfig

import (
	"os"
	"strconv"
)

// GetEnv reads name from the process environment iff useEnv is
// non-zero and the value is non-empty. Mirrors _Py_GetEnv: the
// "Python ignores the environment when -E or PYTHONNOENV is in
// effect" guard lives here.
//
// CPython: Python/preconfig.c:527 _Py_GetEnv
func GetEnv(useEnv int, name string) string {
	if useEnv == 0 {
		return ""
	}
	v, ok := os.LookupEnv(name)
	if !ok {
		return ""
	}
	if v == "" {
		return ""
	}
	return v
}

// StrToInt parses str as a decimal integer. Reports false if the
// string has trailing junk or overflows int. Mirrors _Py_str_to_int:
// strtol with errno + endptr checks against the C int range.
//
// CPython: Python/preconfig.c:545 _Py_str_to_int
func StrToInt(str string) (int, bool) {
	v, err := strconv.ParseInt(str, 10, 0)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

// GetEnvFlag updates *flag from the integer value of the named env
// variable, but only if that value is greater than the current
// *flag. Treats unparseable / negative values as 1, which is how
// CPython models PYTHONDEBUG=text and PYTHONDEBUG=-2.
//
// CPython: Python/preconfig.c:563 _Py_get_env_flag
func GetEnvFlag(useEnv int, flag *int, name string) {
	v := GetEnv(useEnv, name)
	if v == "" {
		return
	}
	value, ok := StrToInt(v)
	if !ok || value < 0 {
		value = 1
	}
	if *flag < value {
		*flag = value
	}
}

// EnvVars is the v0.7 snapshot of the PYTHON* variables PyConfig_Read
// consumes. Each int field uses the CPython tri-state convention
// where -1 means "not set in the environment" so the caller can layer
// CLI / explicit writes on top.
//
// CPython: Python/initconfig.c:1846 config_read_env_vars (subset)
type EnvVars struct {
	Home              string
	Path              string
	HashSeed          string
	DontWriteBytecode int
	Unbuffered        int
	UTF8Mode          int
	Debug             int
	Verbose           int
	Optimize          int
	NoUserSite        int
	DevMode           int
}

// ReadEnvVars walks the PYTHON* variables gopy honors and returns a
// snapshot. useEnv mirrors PyConfig.use_environment: 0 means ignore
// the environment entirely (for -E / PYTHONNOENV mode).
//
// The flag-style reads use GetEnvFlag, which leaves the field at its
// "not set" sentinel when the variable is absent or empty. The string
// fields fall through to "" on absence.
//
// CPython: Python/initconfig.c:1843 config_read_env_vars
func ReadEnvVars(useEnv int) EnvVars {
	v := EnvVars{
		DontWriteBytecode: -1,
		Unbuffered:        -1,
		UTF8Mode:          -1,
		Debug:             -1,
		Verbose:           -1,
		Optimize:          -1,
		NoUserSite:        -1,
		DevMode:           -1,
	}
	v.Home = GetEnv(useEnv, "PYTHONHOME")
	v.Path = GetEnv(useEnv, "PYTHONPATH")
	v.HashSeed = GetEnv(useEnv, "PYTHONHASHSEED")

	GetEnvFlag(useEnv, &v.Debug, "PYTHONDEBUG")
	GetEnvFlag(useEnv, &v.Verbose, "PYTHONVERBOSE")
	GetEnvFlag(useEnv, &v.Optimize, "PYTHONOPTIMIZE")
	GetEnvFlag(useEnv, &v.DontWriteBytecode, "PYTHONDONTWRITEBYTECODE")
	GetEnvFlag(useEnv, &v.NoUserSite, "PYTHONNOUSERSITE")
	GetEnvFlag(useEnv, &v.Unbuffered, "PYTHONUNBUFFERED")
	GetEnvFlag(useEnv, &v.UTF8Mode, "PYTHONUTF8")
	GetEnvFlag(useEnv, &v.DevMode, "PYTHONDEVMODE")
	return v
}
