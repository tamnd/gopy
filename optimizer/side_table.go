// Code.Executors side-table management. The side table holds at most
// MaxExecutorsSize executors per Code object. ENTER_EXECUTOR's oparg
// indexes into Entries and the dispatch loop reads
// code.Executors.(*ExecutorArray).Entries[oparg] when the warmup
// counter overflows. The capacity grows by doubling so the entries
// slot is stable across reallocations.
//
// CPython: Python/optimizer.c:34-193

package optimizer

import (
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// MaxExecutorsSize caps the number of executors the side table holds
// for a single Code. CPython pins this so ENTER_EXECUTOR's oparg fits
// in a single byte.
//
// CPython: Python/optimizer.c:29 MAX_EXECUTORS_SIZE
const MaxExecutorsSize = 256

// codeExecutors returns the side table or nil if Code has not yet
// allocated one. Encapsulates the any-typed handoff between objects
// and optimizer.
func codeExecutors(code *objects.Code) *ExecutorArray {
	if code.Executors == nil {
		return nil
	}
	return code.Executors.(*ExecutorArray)
}

// hasSpaceForExecutor reports whether Code has room for another
// executor anchored at instr. Re-installation at an existing
// ENTER_EXECUTOR slot is always allowed.
//
// CPython: Python/optimizer.c:33-43 has_space_for_executor
func hasSpaceForExecutor(code *objects.Code, instrIndex int) bool {
	if compile.Opcode(code.Code[instrIndex*2]) == compile.ENTER_EXECUTOR {
		return true
	}
	arr := codeExecutors(code)
	if arr == nil {
		return true
	}
	return arr.Size < MaxExecutorsSize
}

// getIndexForExecutor returns the slot index a fresh executor will
// occupy. Reinstalling at an existing ENTER_EXECUTOR returns the
// slot the existing executor already owns. Otherwise it grows the
// array to make room and returns the next free slot. Returns -1 on
// allocation failure (Go's append never fails, so the failure path
// is reserved for parity).
//
// CPython: Python/optimizer.c:45-75 get_index_for_executor
func getIndexForExecutor(code *objects.Code, instrIndex int) int {
	op := compile.Opcode(code.Code[instrIndex*2])
	if op == compile.ENTER_EXECUTOR {
		return int(code.Code[instrIndex*2+1])
	}
	arr := codeExecutors(code)
	size := 0
	capacity := 0
	if arr != nil {
		size = arr.Size
		capacity = arr.Capacity
	}
	if size == capacity {
		newCapacity := 4
		if capacity != 0 {
			newCapacity = capacity * 2
		}
		newEntries := make([]*Executor, newCapacity)
		if arr != nil {
			copy(newEntries, arr.Entries)
		} else {
			arr = &ExecutorArray{}
			code.Executors = arr
		}
		arr.Capacity = newCapacity
		arr.Entries = newEntries
	}
	return size
}

// insertExecutor binds executor into Code's side table at index and
// patches the bytecode at instrIndex to ENTER_EXECUTOR with the slot
// number as its oparg. Reusing an existing ENTER_EXECUTOR slot
// detaches the previous executor first.
//
// CPython: Python/optimizer.c:77-99 insert_executor
func insertExecutor(code *objects.Code, instrIndex, index int, executor *Executor) {
	op := compile.Opcode(code.Code[instrIndex*2])
	arg := code.Code[instrIndex*2+1]
	arr := codeExecutors(code)
	if op == compile.ENTER_EXECUTOR {
		detachExecutor(arr.Entries[index])
	} else {
		arr.Size++
	}
	executor.VMData.Opcode = uint8(op)
	executor.VMData.Oparg = arg
	executor.VMData.Code = code
	executor.VMData.Index = instrIndex
	arr.Entries[index] = executor
	code.Code[instrIndex*2] = byte(compile.ENTER_EXECUTOR)
	code.Code[instrIndex*2+1] = byte(index)
}

// getExecutorLockHeld walks Code's bytecode looking for the
// ENTER_EXECUTOR at the requested byte offset. CPython holds the
// per-code critical section here; gopy is single-threaded, so the
// lock-held suffix is preserved purely for trace symmetry.
//
// CPython: Python/optimizer.c:166-181 get_executor_lock_held
func getExecutorLockHeld(code *objects.Code, offset int) (*Executor, error) {
	codeLen := len(code.Code) / 2
	for i := 0; i < codeLen; {
		op := compile.Opcode(code.Code[i*2])
		if op == compile.ENTER_EXECUTOR && i*2 == offset {
			arr := codeExecutors(code)
			oparg := int(code.Code[i*2+1])
			return arr.Entries[oparg], nil
		}
		i += 1 + specialize.CacheCount(op)
	}
	return nil, fmt.Errorf("ValueError: no executor at given byte offset")
}

// GetExecutor returns the executor anchored at offset bytes into
// code's bytecode, or an error if no ENTER_EXECUTOR exists there.
// This is the public entry point sys.monitoring and dis.dis use to
// resolve ENTER_EXECUTOR's oparg back to the trace it routes to.
//
// CPython: Python/optimizer.c:184-193 _Py_GetExecutor
func GetExecutor(code *objects.Code, offset int) (*Executor, error) {
	return getExecutorLockHeld(code, offset)
}
