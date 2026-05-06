// Sanity check that the builtins.SetCurrentScope hook returns the
// running frame's globals/locals and falls back to (nil, nil) when no
// frame is on the stack.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func TestCurrentScopeNoFrameReturnsNil(t *testing.T) {
	g, l := currentScope()
	if g != nil || l != nil {
		t.Fatalf("currentScope() outside Eval = (%v, %v), want (nil, nil)", g, l)
	}
}

func TestCurrentScopeReturnsActiveFrame(t *testing.T) {
	ts := state.NewThread()
	prev := setActiveThread(ts)
	defer restoreActiveThread(prev)

	g := objects.NewDict()
	co := codeWithBytecode(append(
		instr(compile.LOAD_SMALL_INT, 1),
		instr(compile.RETURN_VALUE, 0)...))
	stack := frameStackFor(ts)
	stack.Push(co, g, nil, nil, nil)
	defer stack.Pop()

	gotG, gotL := currentScope()
	if gotG != g {
		t.Fatalf("currentScope globals = %v, want %v", gotG, g)
	}
	if gotL == nil {
		t.Fatal("currentScope locals = nil, want a snapshot dict")
	}
	if _, ok := gotL.(*objects.Dict); !ok {
		t.Fatalf("currentScope locals = %T, want *Dict", gotL)
	}
}

func TestCurrentScopeReturnsExplicitLocals(t *testing.T) {
	ts := state.NewThread()
	prev := setActiveThread(ts)
	defer restoreActiveThread(prev)

	g := objects.NewDict()
	l := objects.NewDict()
	_ = l.SetItem(objects.NewStr("x"), objects.NewInt(1))
	co := codeWithBytecode(append(
		instr(compile.LOAD_SMALL_INT, 1),
		instr(compile.RETURN_VALUE, 0)...))
	stack := frameStackFor(ts)
	f := stack.Push(co, g, nil, nil, nil)
	f.Locals = l
	defer stack.Pop()

	gotG, gotL := currentScope()
	if gotG != g {
		t.Fatalf("globals = %v, want %v", gotG, g)
	}
	if gotL != l {
		t.Fatalf("locals = %v, want explicit dict", gotL)
	}
}
