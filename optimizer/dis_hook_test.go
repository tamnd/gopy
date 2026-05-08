package optimizer

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func TestDisassembleRuntime_NoExecutors(t *testing.T) {
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.RETURN_VALUE), 0,
		},
	}
	got := DisassembleRuntime(co)
	if !strings.Contains(got, "LOAD_CONST") || !strings.Contains(got, "RETURN_VALUE") {
		t.Errorf("missing opnames\n%s", got)
	}
}

func TestDisassembleRuntime_ENTER_EXECUTOR_Rewrite(t *testing.T) {
	// Simulate the post-install state: ENTER_EXECUTOR at offset 0 with
	// slot index 0; the executor stashed (LOAD_FAST, 7) as the original.
	co := &objects.Code{
		Code: []byte{
			byte(compile.ENTER_EXECUTOR), 0,
			byte(compile.RETURN_VALUE), 0,
		},
	}
	exec := AllocateExecutor(0, 0)
	exec.VMData.Opcode = uint8(compile.LOAD_FAST)
	exec.VMData.Oparg = 7
	co.Executors = &ExecutorArray{Capacity: 1, Size: 1, Entries: []*Executor{exec}}

	got := DisassembleRuntime(co)
	if strings.Contains(got, "ENTER_EXECUTOR") {
		t.Errorf("ENTER_EXECUTOR should be rewritten away\n%s", got)
	}
	if !strings.Contains(got, "LOAD_FAST") {
		t.Errorf("missing rewritten LOAD_FAST\n%s", got)
	}
	if !strings.Contains(got, " 7\n") {
		t.Errorf("missing oparg 7\n%s", got)
	}
}
