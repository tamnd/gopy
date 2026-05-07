package frame

import (
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// Compile-time assertion that *Frame satisfies the contract the
// objects-side wrapper expects.
var _ objects.InterpreterFrame = (*Frame)(nil)

func TestFrameAsInterpreterFrame(t *testing.T) {
	co := mkCode(2, 1, 1, 0)
	co.Varnames = []string{"a", "b"}
	co.Cellvars = []string{"c"}
	co.Freevars = []string{"f"}

	g, b := objects.NewDict(), objects.NewDict()
	var f Frame
	f.Init(co, g, b, nil, nil)
	f.InstrPtr = 6

	a := objects.NewInt(10)
	cv := objects.NewInt(20)
	fv := objects.NewInt(30)
	f.SetLocal(0, stackref.FromObject(a))
	f.LocalsPlus[CellsStart(co)] = stackref.FromObject(cv)
	f.LocalsPlus[FreesStart(co)] = stackref.FromObject(fv)

	var iface objects.InterpreterFrame = &f
	if iface.FrameCode() != co {
		t.Error("FrameCode")
	}
	if iface.FrameGlobals() != g || iface.FrameBuiltins() != b {
		t.Error("FrameGlobals/FrameBuiltins")
	}
	if iface.FrameLocals() != nil {
		t.Error("FrameLocals defaults to nil for fast-locals frames")
	}
	if iface.FrameLasti() != 6 {
		t.Errorf("FrameLasti = %d", iface.FrameLasti())
	}
	if iface.FrameNumLocals() != 2 || iface.FrameNumCells() != 1 || iface.FrameNumFrees() != 1 {
		t.Error("count helpers")
	}
	if iface.FrameFastLocal(0) != a {
		t.Error("FrameFastLocal(0)")
	}
	if iface.FrameFastLocal(1) != nil {
		t.Error("unbound fast local should be nil")
	}
	if iface.FrameCellLocal(0) != cv {
		t.Error("FrameCellLocal")
	}
	if iface.FrameFreeLocal(0) != fv {
		t.Error("FrameFreeLocal")
	}
	if iface.FrameBack() != nil {
		t.Error("FrameBack at root should be nil")
	}
}

func TestFrameBackChain(t *testing.T) {
	co := mkCode(0, 0, 0, 0)
	var parent, child Frame
	parent.Init(co, nil, nil, nil, nil)
	child.Init(co, nil, nil, nil, &parent)

	back := objects.InterpreterFrame(&child).FrameBack()
	if back == nil {
		t.Fatal("FrameBack should return parent")
	}
	if back.FrameCode() != co {
		t.Error("FrameBack handed back the wrong activation record")
	}
}

// Round-trip through objects.NewFrame to verify the wrapper plumbs
// through to the underlying frame.Frame.
func TestFrameObjectsWrapper(t *testing.T) {
	co := mkCode(1, 0, 0, 0)
	co.Varnames = []string{"x"}
	co.Name = "fn"
	co.Filename = "t.py"

	var f Frame
	f.Init(co, nil, nil, nil, nil)
	x := objects.NewInt(99)
	f.SetLocal(0, stackref.FromObject(x))

	pyFrame := objects.NewFrame(&f)
	d, err := objects.FrameFastToLocals(pyFrame)
	if err != nil {
		t.Fatalf("FrameFastToLocals: %v", err)
	}
	got, _ := d.GetItem(objects.NewStr("x"))
	if got != x {
		t.Errorf("snapshot dict missing x: got %v", got)
	}
}
