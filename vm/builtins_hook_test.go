// Sanity check that the builtins.SetCurrentScope hook returns the
// running frame's globals/locals and falls back to (nil, nil) when no
// frame is on the stack.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/imp"
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
	prev, gid := setActiveThread(ts)
	defer restoreActiveThread(prev, gid)

	g := objects.NewDict()
	co := codeWithBytecode(append(
		instr(compile.LOAD_SMALL_INT, 1),
		instr(compile.RETURN_VALUE, 0)...))
	stack := frameStackFor(ts)
	stack.Push(co, g, nil, nil)
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
	prev, gid := setActiveThread(ts)
	defer restoreActiveThread(prev, gid)

	g := objects.NewDict()
	l := objects.NewDict()
	_ = l.SetItem(objects.NewStr("x"), objects.NewInt(1))
	co := codeWithBytecode(append(
		instr(compile.LOAD_SMALL_INT, 1),
		instr(compile.RETURN_VALUE, 0)...))
	stack := frameStackFor(ts)
	f := stack.Push(co, g, nil, nil)
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

// TestCurrentImporterRoutesThroughInittab pins that the __import__ hook
// installed by vm.init forwards to imp.ImportModuleLevel: a builtin
// registered in the inittab is reachable via currentImporter.
func TestCurrentImporterRoutesThroughInittab(t *testing.T) {
	const name = "_test_vm_importer_module"
	imp.RemoveModule(name)
	want := objects.NewModule(name)
	if err := imp.AppendInittab(name, func() (*objects.Module, error) { return want, nil }); err != nil {
		t.Fatalf("AppendInittab: %v", err)
	}
	defer imp.RemoveModule(name)

	got, err := currentImporter(name, "", 0, nil)
	if err != nil {
		t.Fatalf("currentImporter: %v", err)
	}
	if got != want {
		t.Fatalf("currentImporter = %v, want %v", got, want)
	}
}
