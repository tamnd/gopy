package sys

import (
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/objects"
)

// flagsType is the struct-sequence type backing sys.flags. CPython
// builds it from PyStructSequence_Desc flags_desc; gopy declares the
// same field list in the same order so positional iteration and
// named attribute access both match.
//
// CPython: Python/sysmodule.c:3388 flags_fields
var flagsType = objects.NewStructSeqType("sys.flags", []objects.StructSeqField{
	{Name: "debug"},
	{Name: "inspect"},
	{Name: "interactive"},
	{Name: "optimize"},
	{Name: "dont_write_bytecode"},
	{Name: "no_user_site"},
	{Name: "no_site"},
	{Name: "ignore_environment"},
	{Name: "verbose"},
	{Name: "bytes_warning"},
	{Name: "quiet"},
	{Name: "hash_randomization"},
	{Name: "isolated"},
	{Name: "dev_mode"},
	{Name: "utf8_mode"},
	{Name: "warn_default_encoding"},
	{Name: "safe_path"},
	{Name: "int_max_str_digits"},
	{Name: "gil"},
	{Name: "thread_inherit_context"},
	{Name: "context_aware_warnings"},
})

// makeFlags returns sys.flags as a struct-sequence in the field order
// CPython's flags_desc declares. Each element is the int (or bool)
// the corresponding -X / -E / PYTHON* knob computes from PyConfig.
//
// CPython: Python/sysmodule.c:3478 set_flags_from_config
func makeFlags(cfg *initconfig.PyConfig) objects.Object {
	return objects.NewStructSeq(flagsType, []objects.Object{
		intFlag(cfg.ParserDebug),
		intFlag(cfg.Inspect),
		intFlag(cfg.Interactive),
		intFlag(cfg.OptimizationLevel),
		negFlag(cfg.WriteBytecode),
		negFlag(cfg.UserSiteDirectory),
		negFlag(cfg.SiteImport),
		negFlag(cfg.UseEnvironment),
		intFlag(cfg.Verbose),
		intFlag(cfg.BytesWarning),
		intFlag(cfg.Quiet),
		intFlag(hashRandomization(cfg)),
		intFlag(cfg.Isolated),
		boolFlag(cfg.DevMode),
		intFlag(0), // utf8_mode lives on PyPreConfig; pinned to 0 until preconfig lands.
		intFlag(cfg.WarnDefaultEncoding),
		boolFlag(cfg.SafePath),
		intFlag(cfg.IntMaxStrDigits),
		// gil: 1 == default (with-GIL build). Free-threaded builds set
		// it to 0; gopy is single-runtime so report 1.
		// CPython: Python/sysmodule.c:3505 SetFlag(config->enable_gil)
		objects.NewInt(1),
		// thread_inherit_context: defaults to 1 in 3.14 (PEP 768).
		// CPython: Python/sysmodule.c:3510 SetFlag(config->thread_inherit_context)
		objects.NewInt(1),
		// context_aware_warnings: defaults to 1 in 3.14 (PEP 768).
		// CPython: Python/sysmodule.c:3511 SetFlag(config->context_aware_warnings)
		objects.NewInt(1),
	})
}

// hashRandomization mirrors CPython's flag formula: hashing is
// randomized when use_hash_seed is unset (-1 maps to 0 in the runtime
// after preconfig, treated identically here) or when an explicit
// non-zero seed was supplied.
//
// CPython: Python/sysmodule.c:3534 hash_randomization expression
func hashRandomization(cfg *initconfig.PyConfig) int {
	if cfg.UseHashSeed == 0 {
		return 1
	}
	if cfg.HashSeed != 0 {
		return 1
	}
	return 0
}

// intFlag clamps a tri-state config int to the 0/1/N exposed on
// sys.flags. Negative values (the "ask the env" sentinel) become 0.
//
// CPython: Python/sysmodule.c:3519 SetFlag macro
func intFlag(v int) *objects.Int {
	if v < 0 {
		return objects.NewInt(0)
	}
	return objects.NewInt(int64(v))
}

// negFlag is the !field shape: dont_write_bytecode = !write_bytecode.
// A tri-state -1 reads as on (CPython treats unset as the disabled
// default, so the negation is on).
//
// CPython: Python/sysmodule.c:3528 SetFlag(!config->write_bytecode)
func negFlag(v int) *objects.Int {
	if v <= 0 {
		return objects.NewInt(1)
	}
	return objects.NewInt(0)
}

// boolFlag matches PyBool_FromLong: dev_mode and safe_path land as
// True/False ints (CPython exposes them as bool in flags). gopy emits
// the underlying Int for now; the bool wrapping is a follow-up once
// True/False are wired through sys.flags.
//
// CPython: Python/sysmodule.c:3537 SetFlagObj(PyBool_FromLong(...))
func boolFlag(v int) objects.Object {
	if v > 0 {
		return objects.True()
	}
	return objects.False()
}
