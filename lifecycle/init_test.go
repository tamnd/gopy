package lifecycle

import (
	"testing"

	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/state"
)

func TestInitializeNilConfigErrors(t *testing.T) {
	status := func() initconfig.Status {
		_, s := Initialize(nil, nil)
		return s
	}()
	if !status.IsError() {
		t.Fatalf("expected error status, got %+v", status)
	}
}

func TestInitializeBuildsRuntimeAndThread(t *testing.T) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.ParseArgv = 0

	rt := state.NewRuntime()
	tstate, status := Initialize(rt, cfg)
	if status.IsException() {
		t.Fatalf("Initialize: %+v", status)
	}
	if tstate == nil {
		t.Fatal("Initialize returned nil thread")
	}
	if rt.CoreInitialized != 1 {
		t.Errorf("CoreInitialized = %d, want 1", rt.CoreInitialized)
	}
	if rt.Initialized != 1 {
		t.Errorf("Initialized = %d, want 1", rt.Initialized)
	}
	if len(rt.Interpreters()) != 1 {
		t.Errorf("Interpreters count = %d, want 1", len(rt.Interpreters()))
	}
	if tstate.Interp().Runtime() != rt {
		t.Errorf("thread interp.Runtime is not the runtime we passed in")
	}
}

func TestInitializeStoresConfigOnInterpreter(t *testing.T) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.ParseArgv = 0
	cfg.ProgramName = "gopy-init-test"

	tstate, status := Initialize(nil, cfg)
	if status.IsException() {
		t.Fatalf("Initialize: %+v", status)
	}
	stored, ok := tstate.Interp().Config.(*initconfig.PyConfig)
	if !ok {
		t.Fatalf("Interp.Config not *initconfig.PyConfig: %T", tstate.Interp().Config)
	}
	if stored.ProgramName != "gopy-init-test" {
		t.Errorf("ProgramName = %q, want gopy-init-test", stored.ProgramName)
	}
	// Confirm pyinit_core copied (caller's struct must not have been
	// aliased into Interp.Config) — the lifecycle runs ConfigRead on
	// its own copy.
	if stored == cfg {
		t.Error("Interp.Config aliases the caller's config (should be a copy)")
	}
}

// TestInitializeReadsEnvAndCLIThroughConfigRead verifies the
// pyinit_core path actually pipes env + CLI through ConfigRead.
func TestInitializeReadsEnvAndCLIThroughConfigRead(t *testing.T) {
	t.Setenv("PYTHONOPTIMIZE", "1")

	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.Argv = []string{"gopy", "-O", "script.py"}
	callerArgvHead := cfg.Argv[0]

	tstate, status := Initialize(nil, cfg)
	if status.IsException() {
		t.Fatalf("Initialize: %+v", status)
	}
	resolved := tstate.Interp().Config.(*initconfig.PyConfig)
	if resolved.OptimizationLevel != 2 {
		t.Errorf("OptimizationLevel = %d (env=1, CLI -O adds 1 -> 2)", resolved.OptimizationLevel)
	}
	if resolved.RunFilename != "script.py" {
		t.Errorf("RunFilename = %q", resolved.RunFilename)
	}
	// Caller's argv slice header must not have been clobbered by the
	// CLI parser — pyinit_core works on a copy.
	if cfg.Argv[0] != callerArgvHead {
		t.Errorf("caller cfg.Argv[0] mutated to %q", cfg.Argv[0])
	}
}

// TestInitializeReentryUsesReconfigureBranch confirms that calling
// Initialize again on an already-initialized runtime takes the
// reconfigure path: it does not allocate a second interpreter and it
// returns the existing thread state.
func TestInitializeReentryUsesReconfigureBranch(t *testing.T) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.ParseArgv = 0

	rt := state.NewRuntime()
	t1, status := Initialize(rt, cfg)
	if status.IsException() {
		t.Fatalf("first Initialize: %+v", status)
	}

	cfg2 := &initconfig.PyConfig{}
	cfg2.InitPythonConfig()
	cfg2.ParseArgv = 0
	cfg2.ProgramName = "second"

	t2, status := Initialize(rt, cfg2)
	if status.IsException() {
		t.Fatalf("second Initialize: %+v", status)
	}
	if t2 != t1 {
		t.Error("reconfigure branch should return the existing thread, got a new one")
	}
	if got := len(rt.Interpreters()); got != 1 {
		t.Errorf("interpreter count after reconfigure = %d, want 1", got)
	}
	resolved := t2.Interp().Config.(*initconfig.PyConfig)
	if resolved.ProgramName != "second" {
		t.Errorf("ProgramName after reconfigure = %q, want second", resolved.ProgramName)
	}
}

// TestInitializeSkipsMainWhenInitMainZero confirms the InitMain flag
// gating: a config with _init_main=0 stops after pyinit_core.
func TestInitializeSkipsMainWhenInitMainZero(t *testing.T) {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	cfg.ParseArgv = 0
	cfg.InitMain = 0

	rt := state.NewRuntime()
	if _, status := Initialize(rt, cfg); status.IsException() {
		t.Fatalf("Initialize: %+v", status)
	}
	if rt.CoreInitialized != 1 {
		t.Errorf("CoreInitialized = %d, want 1", rt.CoreInitialized)
	}
	if rt.Initialized != 0 {
		t.Errorf("Initialized = %d, want 0 (init_main was off)", rt.Initialized)
	}
}
