package initconfig

import "strconv"

// MaxHashSeed is the upper bound on PYTHONHASHSEED, mirroring the
// CPython MAX_HASH_SEED macro (UINT32_MAX).
//
// CPython: Python/initconfig.c:1738 MAX_HASH_SEED
const MaxHashSeed = 0xFFFFFFFF

// ConfigReadEnvVars merges PYTHON* environment variables into c. Only
// runs when c.UseEnvironment is non-zero. The flag-style variables
// (PYTHONDEBUG, PYTHONVERBOSE, ...) feed through GetEnvFlag, which
// honors the "smaller does not lower" rule. The string-style
// variables (PYTHONHOME, PYTHONPATH, PYTHONPLATLIBDIR) only overwrite
// their PyConfig slot if it is empty so explicit writes survive.
//
// CPython: Python/initconfig.c:1839 config_read_env_vars
func ConfigReadEnvVars(c *PyConfig) Status {
	useEnv := c.UseEnvironment

	GetEnvFlag(useEnv, &c.ParserDebug, "PYTHONDEBUG")
	GetEnvFlag(useEnv, &c.Verbose, "PYTHONVERBOSE")
	GetEnvFlag(useEnv, &c.OptimizationLevel, "PYTHONOPTIMIZE")
	if c.Inspect == 0 && GetEnv(useEnv, "PYTHONINSPECT") != "" {
		c.Inspect = 1
	}

	dontWriteBytecode := 0
	GetEnvFlag(useEnv, &dontWriteBytecode, "PYTHONDONTWRITEBYTECODE")
	if dontWriteBytecode != 0 {
		c.WriteBytecode = 0
	}

	noUserSiteDir := 0
	GetEnvFlag(useEnv, &noUserSiteDir, "PYTHONNOUSERSITE")
	if noUserSiteDir != 0 {
		c.UserSiteDirectory = 0
	}

	unbuffered := 0
	GetEnvFlag(useEnv, &unbuffered, "PYTHONUNBUFFERED")
	if unbuffered != 0 {
		c.BufferedStdio = 0
	}

	if c.PythonpathEnv == "" {
		c.PythonpathEnv = GetEnv(useEnv, "PYTHONPATH")
	}
	if c.Platlibdir == "" {
		c.Platlibdir = GetEnv(useEnv, "PYTHONPLATLIBDIR")
	}

	if c.UseHashSeed < 0 {
		if status := configInitHashSeed(c); status.IsException() {
			return status
		}
	}

	if GetEnv(useEnv, "PYTHONSAFEPATH") != "" {
		c.SafePath = 1
	}

	return StatusOk()
}

// configInitHashSeed parses PYTHONHASHSEED and stamps c.UseHashSeed
// and c.HashSeed. "random" (or unset) means a random seed
// (UseHashSeed=0); a numeric value in [0, MaxHashSeed] pins the seed
// (UseHashSeed=1). Anything else is rejected.
//
// CPython: Python/initconfig.c:1765 config_init_hash_seed
func configInitHashSeed(c *PyConfig) Status {
	seedText := GetEnv(c.UseEnvironment, "PYTHONHASHSEED")
	if seedText != "" && seedText != "random" {
		seed, err := strconv.ParseUint(seedText, 10, 64)
		if err != nil || seed > MaxHashSeed {
			return StatusErr(`PYTHONHASHSEED must be "random" or an integer in range [0; 4294967295]`)
		}
		c.UseHashSeed = 1
		c.HashSeed = seed
	} else {
		c.UseHashSeed = 0
		c.HashSeed = 0
	}
	return StatusOk()
}
