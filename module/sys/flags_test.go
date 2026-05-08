package sys

import (
	"testing"

	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/objects"
)

// TestUpdateConfigFlagsTupleShape pins sys.flags as an 18-tuple
// (CPython's flags_desc.n_in_sequence). The struct-sequence fields
// beyond 18 (gil, thread_inherit_context, context_aware_warnings)
// land with the named-attribute facade; v0.7 ships the positional
// tuple only.
func TestUpdateConfigFlagsTupleShape(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	v, _ := d.GetItem(objects.NewStr("flags"))
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("flags is %T, want *Tuple", v)
	}
	if tup.Len() != 18 {
		t.Errorf("len(sys.flags) = %d, want 18", tup.Len())
	}
}

// TestUpdateConfigFlagsDefaultsMatchPython pins the slot-by-slot
// default for InitPythonConfig: optimization_level zero, write_bytecode
// on (so dont_write_bytecode == 0), site_import on, use_environment on,
// dev_mode False, safe_path False, isolated 0.
func TestUpdateConfigFlagsDefaultsMatchPython(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	v, _ := d.GetItem(objects.NewStr("flags"))
	tup := v.(*objects.Tuple)

	cases := []struct {
		idx  int
		name string
		want int64
	}{
		{0, "debug", 0},
		{3, "optimize", 0},
		{4, "dont_write_bytecode", 0},
		{5, "no_user_site", 0},
		{6, "no_site", 0},
		{7, "ignore_environment", 0},
		{12, "isolated", 0},
		{15, "warn_default_encoding", 0},
		{17, "int_max_str_digits", 0},
	}
	for _, tc := range cases {
		got, _ := tup.Item(tc.idx).(*objects.Int).Int64()
		if got != tc.want {
			t.Errorf("flags[%d] (%s) = %d, want %d", tc.idx, tc.name, got, tc.want)
		}
	}
}

// TestUpdateConfigFlagsDevModeAndSafePathAreBool pins that dev_mode
// and safe_path land as the True/False singletons (CPython wraps them
// in PyBool_FromLong).
func TestUpdateConfigFlagsDevModeAndSafePathAreBool(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.DevMode = 1
	cfg.SafePath = 1
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatalf("UpdateConfig: %v", uerr)
	}
	tup := mustFlags(t, d)

	if tup.Item(13) != objects.True() {
		t.Errorf("flags[13] (dev_mode) = %v, want True", tup.Item(13))
	}
	if tup.Item(16) != objects.True() {
		t.Errorf("flags[16] (safe_path) = %v, want True", tup.Item(16))
	}
}

// TestUpdateConfigFlagsHashRandomization pins the formula:
// use_hash_seed == 0 OR hash_seed != 0 turns hash_randomization on.
func TestUpdateConfigFlagsHashRandomization(t *testing.T) {
	cases := []struct {
		name string
		use  int
		seed uint64
		want int64
	}{
		{"use_zero", 0, 0, 1},
		{"use_one_seed_zero", 1, 0, 0},
		{"use_one_seed_nonzero", 1, 42, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Init()
			if err != nil {
				t.Fatal(err)
			}
			cfg := &initconfig.PyConfig{}
			cfg.InitPythonConfig()
			cfg.UseHashSeed = tc.use
			cfg.HashSeed = tc.seed
			if uerr := UpdateConfig(d, cfg); uerr != nil {
				t.Fatal(uerr)
			}
			tup := mustFlags(t, d)
			got, _ := tup.Item(11).(*objects.Int).Int64()
			if got != tc.want {
				t.Errorf("flags[11] (hash_randomization) = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestUpdateConfigFlagsNegationFromWriteBytecode pins the !write_bytecode
// shape: when WriteBytecode is on, dont_write_bytecode is 0; when off,
// dont_write_bytecode is 1.
func TestUpdateConfigFlagsNegationFromWriteBytecode(t *testing.T) {
	d, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.WriteBytecode = 0
	if uerr := UpdateConfig(d, cfg); uerr != nil {
		t.Fatal(uerr)
	}
	tup := mustFlags(t, d)
	got, _ := tup.Item(4).(*objects.Int).Int64()
	if got != 1 {
		t.Errorf("flags[4] (dont_write_bytecode) = %d, want 1 when WriteBytecode==0", got)
	}
}

func mustFlags(t *testing.T, d *objects.Dict) *objects.Tuple {
	t.Helper()
	v, err := d.GetItem(objects.NewStr("flags"))
	if err != nil {
		t.Fatalf("GetItem(flags): %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("flags is %T, want *Tuple", v)
	}
	return tup
}
