package optimizer

import (
	"testing"
	"unsafe"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// installLoopExecutor builds a tiny `LOAD_CONST/POP_TOP/JUMP_BACKWARD`
// loop, runs Optimize, and asserts the install was successful. Used as
// a fixture for the invalidation helpers below.
func installLoopExecutor(t *testing.T, interp *state.Interpreter) (*objects.Code, *Executor) {
	t.Helper()
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.POP_TOP), 0,
			byte(compile.JUMP_BACKWARD), 4,
			0, 0,
		},
	}
	exec, status := Optimize(interp, nil, co, 0, 0, 0)
	if status != 1 || exec == nil {
		t.Fatalf("Optimize status=%d exec=%v, want successful install", status, exec)
	}
	return co, exec
}

func TestExecutorDependsOn_StampsBloom(t *testing.T) {
	exec := AllocateExecutor(0, 1)
	exec.VMData.Valid = true
	var x int
	ExecutorDependsOn(exec, unsafe.Pointer(&x))
	var probe BloomFilter
	probe.Init()
	probe.Add(unsafe.Pointer(&x))
	if !exec.VMData.Bloom.MayContain(&probe) {
		t.Errorf("bloom should contain stamped pointer")
	}
	var y int
	probe.Init()
	probe.Add(unsafe.Pointer(&y))
	// May produce a false positive (bloom is probabilistic), but with
	// a freshly-initialized bloom and a single unrelated pointer the
	// probability is dominated by the seed; we treat a hit as benign.
}

func TestExecutorsInvalidateAll_ClearsCodeAnchored(t *testing.T) {
	interp := &state.Interpreter{}
	co, exec := installLoopExecutor(t, interp)

	prev := optStatExecutorsInvalidated
	ExecutorsInvalidateAll(interp, true)
	if optStatExecutorsInvalidated <= prev {
		t.Errorf("OPT_STAT_INC(executors_invalidated) did not fire")
	}
	if interpState(interp).ExecutorListHead != nil {
		t.Errorf("executor list head should be nil after InvalidateAll")
	}
	if exec.VMData.Valid {
		t.Errorf("cleared executor must have Valid=false")
	}
	if co.Executors != nil {
		t.Errorf("Code.Executors must be released by CodeClearExecutors")
	}
	if compile.Opcode(co.Code[0]) == compile.ENTER_EXECUTOR {
		t.Errorf("install site bytecode should be restored after invalidation")
	}
}

func TestExecutorsInvalidateDependency_HitsViaBloom(t *testing.T) {
	interp := &state.Interpreter{}
	_, exec := installLoopExecutor(t, interp)

	var dep int
	ExecutorDependsOn(exec, unsafe.Pointer(&dep))

	prev := optStatExecutorsInvalidated
	ExecutorsInvalidateDependency(interp, unsafe.Pointer(&dep), true)
	if optStatExecutorsInvalidated <= prev {
		t.Errorf("InvalidateDependency should have incremented stat counter")
	}
	if exec.VMData.Valid {
		t.Errorf("dependent executor must be cleared")
	}
}

func TestExecutorsInvalidateDependency_MissesUnrelated(t *testing.T) {
	interp := &state.Interpreter{}
	_, exec := installLoopExecutor(t, interp)

	var dep int
	ExecutorDependsOn(exec, unsafe.Pointer(&dep))

	prev := optStatExecutorsInvalidated
	// The bloom is small and may report a false positive; iterate
	// until we find a probe that the executor's bloom rejects, with
	// a generous bound so we never spin.
	probes := make([]int, 64)
	var unrelated unsafe.Pointer
	for i := range probes {
		var probe BloomFilter
		probe.Init()
		probe.Add(unsafe.Pointer(&probes[i]))
		if !exec.VMData.Bloom.MayContain(&probe) {
			unrelated = unsafe.Pointer(&probes[i])
			break
		}
	}
	if unrelated == nil {
		t.Skip("could not find a non-colliding probe pointer")
	}
	ExecutorsInvalidateDependency(interp, unrelated, true)
	if optStatExecutorsInvalidated != prev {
		t.Errorf("non-matching probe should not invalidate any executor")
	}
	if !exec.VMData.Valid {
		t.Errorf("non-matching probe must not clear executor")
	}
}

func TestExecutorsInvalidateCold_DropsNonWarm(t *testing.T) {
	interp := &state.Interpreter{}
	_, exec := installLoopExecutor(t, interp)
	exec.VMData.Warm = false

	ExecutorsInvalidateCold(interp)
	if exec.VMData.Valid {
		t.Errorf("cold executor must be cleared on first sweep")
	}
}

func TestExecutorsInvalidateCold_PreservesWarm(t *testing.T) {
	interp := &state.Interpreter{}
	_, exec := installLoopExecutor(t, interp)
	exec.VMData.Warm = true

	ExecutorsInvalidateCold(interp)
	if !exec.VMData.Valid {
		t.Errorf("warm executor must survive the first sweep")
	}
	if exec.VMData.Warm {
		t.Errorf("first sweep should clear Warm so the next sweep removes it")
	}
	// Second sweep with Warm now false should clear it.
	ExecutorsInvalidateCold(interp)
	if exec.VMData.Valid {
		t.Errorf("second sweep with Warm=false must clear executor")
	}
}
