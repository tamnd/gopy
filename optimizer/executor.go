// Executor lifecycle. Allocation, init, link/unlink against the
// per-interpreter list, detach from Code, clear, and the two-phase
// free-via-pending-deletion-list scheme. tp_dealloc cannot run while
// another thread is inside the executor, so deletion is staged on
// InterpState.ExecutorDeletionListHead and swept by
// ClearExecutorDeletionList when the capacity gauge underflows.
//
// CPython: Python/optimizer.c:216-272, 1100-1115, 1417-1518

package optimizer

import (
	"unsafe"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// InterpState is the per-interpreter Tier-2 bookkeeping. The executor
// list head links every live executor; the deletion list head holds
// executors awaiting a sweep. RemainingCapacity is decremented on
// each pending-deletion enqueue and refilled when the sweep runs.
//
// CPython: Include/internal/pycore_interp_structs.h
// executor_list_head / executor_deletion_list_head /
// executor_deletion_list_remaining_capacity
type InterpState struct {
	ExecutorListHead         *Executor
	ExecutorDeletionListHead *Executor
	RemainingCapacity        int
}

// interpState returns the optimizer state for interp, lazily
// allocating it on first access.
func interpState(interp *state.Interpreter) *InterpState {
	if interp.Optimizer == nil {
		interp.Optimizer = &InterpState{
			RemainingCapacity: ExecutorDeleteListMax,
		}
	}
	return interp.Optimizer.(*InterpState)
}

// AllocateExecutor returns a fresh executor with room for length uops
// and exitCount side exits. Trace and Exits slices share the same
// underlying memory in CPython through a flexible array member; gopy
// allocates them as separate slices since Go has no equivalent.
//
// CPython: Python/optimizer.c:1103-1115 allocate_executor
func AllocateExecutor(exitCount, length int) *Executor {
	res := &Executor{
		Trace:     make([]UOPInstruction, length),
		Exits:     make([]ExitData, exitCount),
		CodeSize:  uint32(length),
		ExitCount: uint32(exitCount),
	}
	return res
}

// ExecutorInit stamps the dependency bloom into the executor and
// links it onto the per-interpreter list. Optimizers must call this
// before exposing the executor to the dispatch loop.
//
// CPython: Python/optimizer.c:1466-1474 _Py_ExecutorInit
func ExecutorInit(interp *state.Interpreter, executor *Executor, dependencies *BloomFilter) {
	executor.VMData.Valid = true
	for i := 0; i < BloomFilterWords; i++ {
		executor.VMData.Bloom.Bits[i] = dependencies.Bits[i]
	}
	linkExecutor(interp, executor)
}

// linkExecutor prepends executor to the interpreter's list. The list
// is doubly-linked so unlink_executor is O(1) without scanning.
//
// CPython: Python/optimizer.c:1417-1438 link_executor
func linkExecutor(interp *state.Interpreter, executor *Executor) {
	st := interpState(interp)
	links := &executor.VMData.Links
	head := st.ExecutorListHead
	if head == nil {
		st.ExecutorListHead = executor
		links.Prev = nil
		links.Next = nil
	} else {
		links.Prev = nil
		links.Next = head
		head.VMData.Links.Prev = executor
		st.ExecutorListHead = executor
	}
	executor.VMData.Linked = true
}

// unlinkExecutor splices executor out of the interpreter's list. A
// no-op if the executor is already unlinked. Unlinked executors stay
// in their last-known state until the deletion sweep frees them.
//
// CPython: Python/optimizer.c:1440-1463 unlink_executor
func unlinkExecutor(interp *state.Interpreter, executor *Executor) {
	if !executor.VMData.Linked {
		return
	}
	links := &executor.VMData.Links
	next := links.Next
	prev := links.Prev
	if next != nil {
		next.VMData.Links.Prev = prev
	}
	if prev != nil {
		prev.VMData.Links.Next = next
	} else {
		st := interpState(interp)
		st.ExecutorListHead = next
	}
	executor.VMData.Linked = false
}

// detachExecutor clears the bytecode patch that routes
// ENTER_EXECUTOR back to executor and forgets the slot in
// Code.Executors. Safe to call when the executor never patched any
// site (vm_data.Code is nil for non-progress-needed executors).
//
// CPython: Python/optimizer.c:1478-1494 _Py_ExecutorDetach
func detachExecutor(executor *Executor) {
	code := executor.VMData.Code
	if code == nil {
		return
	}
	idx := executor.VMData.Index
	arr := codeExecutors(code)
	slot := int(code.Code[idx*2+1])
	code.Code[idx*2] = executor.VMData.Opcode
	code.Code[idx*2+1] = executor.VMData.Oparg
	executor.VMData.Code = nil
	arr.Entries[slot] = nil
}

// ExecutorClear runs tp_clear on the executor: unlinks from the
// interpreter list, marks it invalid, drops every chained
// side-exit reference, and detaches from its anchor Code. Idempotent
// once Valid is false.
//
// CPython: Python/optimizer.c:1496-1518 executor_clear
func ExecutorClear(interp *state.Interpreter, executor *Executor) {
	if !executor.VMData.Valid {
		return
	}
	unlinkExecutor(interp, executor)
	executor.VMData.Valid = false
	for i := uint32(0); i < executor.ExitCount; i++ {
		executor.Exits[i].Temperature = initialUnreachableBackoffCounter()
		executor.Exits[i].Executor = nil
	}
	detachExecutor(executor)
}

// FreeExecutor releases the executor's storage. CPython hands the
// memory back to the GC arena; gopy lets Go's runtime reclaim it
// once the last reference drops.
//
// CPython: Python/optimizer.c:217-223 free_executor
func FreeExecutor(executor *Executor) {
	executor.Trace = nil
	executor.Exits = nil
}

// AddToPendingDeletionList enqueues self on the per-interpreter
// deletion list. The list is a singly-linked stack reusing the
// VMData.Links.Next pointer for cheap intrusive linkage. Each push
// decrements RemainingCapacity; once it hits zero the sweep runs to
// reclaim everything not currently executing.
//
// CPython: Python/optimizer.c:260-272 add_to_pending_deletion_list
func AddToPendingDeletionList(interp *state.Interpreter, self *Executor) {
	st := interpState(interp)
	self.VMData.Links.Next = st.ExecutorDeletionListHead
	st.ExecutorDeletionListHead = self
	if st.RemainingCapacity > 0 {
		st.RemainingCapacity--
	} else {
		ClearExecutorDeletionList(interp)
	}
}

// ClearExecutorDeletionList walks the deletion list and frees every
// executor that is not currently executing on a thread. The CPython
// path consults each thread's current_executor; gopy is single-
// threaded for now, so nothing is "currently executing" outside the
// caller and the entire list is freed.
//
// CPython: Python/optimizer.c:225-258 _Py_ClearExecutorDeletionList
func ClearExecutorDeletionList(interp *state.Interpreter) {
	st := interpState(interp)
	prevToNext := &st.ExecutorDeletionListHead
	exec := *prevToNext
	for exec != nil {
		if exec.VMData.Linked {
			exec.VMData.Linked = false
			prevToNext = &exec.VMData.Links.Next
		} else {
			*prevToNext = exec.VMData.Links.Next
			FreeExecutor(exec)
		}
		exec = *prevToNext
	}
	st.RemainingCapacity = ExecutorDeleteListMax
}

// initialUnreachableBackoffCounter returns the temperature value an
// invalidated exit takes on. CPython uses a bit pattern that prevents
// the side exit from ever warming back up; we mirror it byte for
// byte.
//
// CPython: Include/internal/pycore_backoff.h:99
// initial_unreachable_backoff_counter
func initialUnreachableBackoffCounter() BackoffCounter {
	return BackoffCounter{ValueAndBackoff: 0xFFFF}
}

// ExecutorDependsOn stamps obj into executor's dependency bloom. The
// trace projector calls this for every guarded type / dict / code
// pointer; later mutations matching the bloom invalidate the trace.
//
// CPython: Python/optimizer.c:1520-1525 _Py_Executor_DependsOn
func ExecutorDependsOn(executor *Executor, obj unsafe.Pointer) {
	executor.VMData.Bloom.Add(obj)
}

// CodeClearExecutors detaches every executor anchored at code's side
// table and drops the table. Called from `_Py_Executors_InvalidateAll`
// when a code object's executors must all go.
//
// CPython: Objects/codeobject.c:2272-2289 clear_executors / _PyCode_Clear_Executors
func CodeClearExecutors(code *objects.Code) {
	arr := codeExecutors(code)
	if arr == nil {
		return
	}
	for i := 0; i < arr.Size; i++ {
		if arr.Entries[i] != nil {
			detachExecutor(arr.Entries[i])
		}
	}
	code.Executors = nil
}

// ExecutorsInvalidateDependency clears every executor whose
// dependency bloom may contain obj. The CPython path stages the matches
// into a list before clearing because executor_clear can deallocate
// peers; gopy mirrors the staging pattern even though Go's GC removes
// the deallocation hazard, so the walk semantics are byte-for-byte
// identical.
//
// CPython: Python/optimizer.c:1530-1568 _Py_Executors_InvalidateDependency
func ExecutorsInvalidateDependency(interp *state.Interpreter, obj unsafe.Pointer, isInvalidation bool) {
	var objFilter BloomFilter
	objFilter.Init()
	objFilter.Add(obj)
	st := interpState(interp)
	var pending []*Executor
	for exec := st.ExecutorListHead; exec != nil; {
		next := exec.VMData.Links.Next
		if exec.VMData.Bloom.MayContain(&objFilter) {
			pending = append(pending, exec)
		}
		exec = next
	}
	for _, exec := range pending {
		ExecutorClear(interp, exec)
		if isInvalidation {
			optStatExecutorsInvalidated++
		}
	}
}

// ExecutorsInvalidateAll clears every executor in interp. Executors
// anchored at a code object route through CodeClearExecutors so the
// side table is reclaimed too; standalone executors clear directly.
//
// CPython: Python/optimizer.c:1572-1588 _Py_Executors_InvalidateAll
func ExecutorsInvalidateAll(interp *state.Interpreter, isInvalidation bool) {
	st := interpState(interp)
	for st.ExecutorListHead != nil {
		exec := st.ExecutorListHead
		if exec.VMData.Code != nil {
			CodeClearExecutors(exec.VMData.Code)
		} else {
			ExecutorClear(interp, exec)
		}
		if isInvalidation {
			optStatExecutorsInvalidated++
		}
	}
}

// ExecutorsInvalidateCold clears every executor whose Warm flag is
// unset and downgrades the rest to "not warm". Two passes against the
// same flag implement a CLOCK-style aging policy: a trace must keep
// warming up between sweeps to survive.
//
// CPython: Python/optimizer.c:1590-1626 _Py_Executors_InvalidateCold
func ExecutorsInvalidateCold(interp *state.Interpreter) {
	st := interpState(interp)
	var pending []*Executor
	for exec := st.ExecutorListHead; exec != nil; {
		next := exec.VMData.Links.Next
		if !exec.VMData.Warm {
			pending = append(pending, exec)
		} else {
			exec.VMData.Warm = false
		}
		exec = next
	}
	for _, exec := range pending {
		ExecutorClear(interp, exec)
	}
}

// optStatExecutorsInvalidated is the shadow of CPython's
// `OPT_STAT_INC(executors_invalidated)`. Exposed for the gate tests so
// they can assert the watcher arm fired.
//
// CPython: Include/internal/pycore_optimizer.h OPT_STAT_INC
var optStatExecutorsInvalidated int
