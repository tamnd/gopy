// Python-object surface for Executor. Mirrors the _PyUOpExecutor_Type
// slots: tp_dealloc routes through the pending-deletion list, tp_clear
// runs the lifecycle clear, tp_as_sequence exposes len(executor) and
// executor[i] so dis.dis can iterate the trace, tp_traverse walks
// chained side-exit executors so the cycle collector handles the
// trace tree. The is_valid / get_opcode / get_oparg accessors are
// exported as Go methods rather than Python methods; the dis.dis
// pretty-printer reaches them through the optimizer package directly.
//
// CPython: Python/optimizer.c:193-432

package optimizer

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// ExecutorType is the type singleton for Tier-2 executors. dis.dis
// recognizes this type so disassembly walks the trace through the
// sequence protocol.
//
// CPython: Python/optimizer.c:421-432 _PyUOpExecutor_Type
var ExecutorType = objects.NewType("uop_executor", []*objects.Type{objects.ObjectType()})

func init() {
	ExecutorType.Repr = executorRepr
	ExecutorType.Str = executorRepr
	ExecutorType.Sequence = &objects.SequenceMethods{
		Length:  executorLength,
		GetItem: executorItem,
	}
	ExecutorType.TpTraverse = executorTraverse
}

// executorRepr formats Executor like CPython's tp_repr fallback for
// the executor type: <uop_executor at 0xADDR>. dis.dis renders the
// trace by iterating, not by repr, so the format is deliberately
// minimal.
func executorRepr(o objects.Object) (string, error) {
	e := o.(*Executor)
	return fmt.Sprintf("<uop_executor at %p>", e), nil
}

// executorLength returns code_size. CPython exposes this through
// sq_length so len(executor) gives the number of uops.
//
// CPython: Python/optimizer.c:331-336 uop_len
func executorLength(o objects.Object) (int, error) {
	return int(o.(*Executor).CodeSize), nil
}

// executorItem returns a 4-tuple (name, oparg, target, operand0) for
// the i-th uop. dis.dis builds its uop rows out of this tuple. Out
// of range returns IndexError.
//
// CPython: Python/optimizer.c:338-375 uop_item
func executorItem(o objects.Object, i int) (objects.Object, error) {
	e := o.(*Executor)
	if i < 0 || i >= int(e.CodeSize) {
		return nil, fmt.Errorf("IndexError: trace index out of range")
	}
	inst := &e.Trace[i]
	name := UOpName(inst.Opcode())
	if name == "" {
		name = "<nil>"
	}
	return objects.NewTuple([]objects.Object{
		objects.NewStr(name),
		objects.NewInt(int64(inst.Oparg)),
		objects.NewInt(int64(inst.Target)),
		objects.NewInt(int64(inst.Operand0)),
	}), nil
}

// executorTraverse visits every chained side-exit executor so the
// cycle collector can find references the trace holds.
//
// CPython: Python/optimizer.c:382-390 executor_traverse
func executorTraverse(o objects.Object, visit objects.Visitor) error {
	e := o.(*Executor)
	for i := uint32(0); i < e.ExitCount; i++ {
		exit := e.Exits[i].Executor
		if exit == nil {
			continue
		}
		if err := visit(exit); err != nil {
			return err
		}
	}
	return nil
}

// IsValid reports whether the executor is still wired into the
// runtime. CPython exposes this as the is_valid() Python method;
// gopy callers (mainly dis.dis and the gate fixtures) call this
// directly.
//
// CPython: Python/optimizer.c:192-196 is_valid
func (e *Executor) IsValid() bool {
	return e.VMData.Valid
}

// GetOpcode returns the original Tier-1 opcode at the install site.
// dis.dis uses this to render the deopt arrow alongside the trace.
//
// CPython: Python/optimizer.c:198-202 get_opcode
func (e *Executor) GetOpcode() uint8 {
	return e.VMData.Opcode
}

// GetOparg returns the original Tier-1 oparg.
//
// CPython: Python/optimizer.c:204-208 get_oparg
func (e *Executor) GetOparg() uint8 {
	return e.VMData.Oparg
}

// UOpName looks up the human-readable uop name. Returns "" when the
// id is out of range, matching CPython's NULL return; callers that
// want a placeholder substitute "<nil>" themselves. The id->name map
// is generated; see optimizer/uop_ids_gen.go.
//
// CPython: Python/optimizer.c:285-292 _PyUOpName
func UOpName(id uint16) string {
	return UopNames[id]
}
