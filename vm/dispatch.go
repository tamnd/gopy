// Dispatch driver. The generated per-opcode arms in opcodes_gen.go
// (1621) are reached through dispatch, which the loop calls each
// iteration. Until the generator runs, dispatch returns
// ErrNotImplemented for every opcode so the loop driver can be tested
// in isolation.
//
// CPython: Python/ceval.c TARGET / DISPATCH macros expand into the
// per-opcode arm in CPython; here the indirection is one extra
// switch.

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// dispatch executes one instruction and returns the next-pc, a
// terminal return value (set when retDone is true), or an error.
//
// Return contract:
//
//   - retDone=true: the loop returns retVal/retErr immediately. Used
//     by RETURN_VALUE / RETURN_CONST / INTERPRETER_EXIT.
//   - err != nil: the loop walks the exception table.
//   - otherwise: the loop sets InstrPtr = next and continues.
//
// CPython: Python/ceval.c switch over op
func (e *evalState) dispatch(op compile.Opcode, oparg uint32) (next int, retVal objects.Object, retErr error, retDone bool, err error) {
	// The generated table from 1621/B2-B5 will replace this body.
	// Until then, every opcode reports not-implemented so the loop
	// driver can be exercised by tests with synthetic codes.
	_ = oparg
	return 0, nil, nil, false, opcodeNotImplemented(op)
}

// opcodeNotImplemented wraps ErrNotImplemented with the offending op.
func opcodeNotImplemented(op compile.Opcode) error {
	return &notImplemented{op: op}
}

type notImplemented struct{ op compile.Opcode }

func (e *notImplemented) Error() string {
	return "vm: opcode " + opcodeName(e.op) + " not implemented in v0.6"
}

func (e *notImplemented) Unwrap() error { return ErrNotImplemented }

// opcodeName returns the mnemonic for op, falling back to "<unknown>".
func opcodeName(op compile.Opcode) string {
	if name := op.Name(); name != "" {
		return name
	}
	return "<unknown>"
}
