// Pins the v0.9 surface for the async-iter and awaitable opcodes:
// none of them have a real implementation yet, but they all return
// errors that mention "v0.9" so a caller hitting one knows the gap is
// intentional rather than a regression.
//
// CPython: Python/bytecodes.c GET_AITER, GET_ANEXT, GET_AWAITABLE, END_ASYNC_FOR

package vm

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func TestEvalAsyncStubsMentionV09(t *testing.T) {
	cases := []struct {
		name string
		op   compile.Opcode
	}{
		{"GET_AITER", compile.GET_AITER},
		{"GET_ANEXT", compile.GET_ANEXT},
		{"GET_AWAITABLE", compile.GET_AWAITABLE},
		{"END_ASYNC_FOR", compile.END_ASYNC_FOR},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := state.NewThread()
			co := &objects.Code{
				Code: concat(
					instr(compile.LOAD_CONST, 0),
					instr(tc.op, 0),
					instr(compile.RETURN_VALUE, 0),
				),
				Consts:    []any{int64(0)},
				Stacksize: 4,
			}
			_, err := EvalCode(ts, co, nil, nil)
			if err == nil {
				t.Fatalf("%s: expected error, got nil", tc.name)
			}
			msg := err.Error()
			if !strings.Contains(msg, "v0.9") {
				t.Errorf("%s: err=%q, want mention of v0.9", tc.name, msg)
			}
			if !strings.Contains(msg, tc.name) {
				t.Errorf("%s: err=%q, want mention of opcode name", tc.name, msg)
			}
		})
	}
}
