// Behavioral tests for the tp_traverse slot on cell, bound method,
// classmethod, staticmethod, frame, coroutine wrapper, and the async
// generator awaitables. Pins the 1613-N additions to the panel.

package objects

import "testing"

func TestCellTraverseVisitsContents(t *testing.T) {
	a := NewStr("a")
	c := NewCell(a)
	got, err := collectVisited(c)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0] != a {
		t.Fatalf("cell traverse = %v, want [%v]", got, a)
	}
}

func TestCellTraverseEmptyOnUnbound(t *testing.T) {
	c := NewCell(nil)
	got, err := collectVisited(c)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unbound cell traverse = %v, want []", got)
	}
}

func TestBoundMethodTraverseVisitsFuncAndSelf(t *testing.T) {
	fn := NewStr("fn-stand-in")
	self := NewStr("self-stand-in")
	m := NewBoundMethod(fn, self)
	got, err := collectVisited(m)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 2 || got[0] != fn || got[1] != self {
		t.Fatalf("method traverse = %v, want [%v %v]", got, fn, self)
	}
}

func TestClassMethodTraverseVisitsCallable(t *testing.T) {
	fn := NewStr("fn")
	cm := NewClassMethod(fn)
	got, err := collectVisited(cm)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0] != fn {
		t.Fatalf("classmethod traverse = %v, want [%v]", got, fn)
	}
}

func TestStaticMethodTraverseVisitsCallable(t *testing.T) {
	fn := NewStr("fn")
	sm := NewStaticMethod(fn)
	got, err := collectVisited(sm)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0] != fn {
		t.Fatalf("staticmethod traverse = %v, want [%v]", got, fn)
	}
}

// fakeInterp is a stand-in InterpreterFrame that returns fixed objects
// from each accessor so frameTraverse can be exercised without the
// vm/frame package.
type fakeInterp struct {
	globals, builtins, locals Object
	fast, cells, frees        []Object
	back                      InterpreterFrame
}

func (f *fakeInterp) FrameCode() *Code            { return nil }
func (f *fakeInterp) FrameGlobals() Object        { return f.globals }
func (f *fakeInterp) FrameBuiltins() Object       { return f.builtins }
func (f *fakeInterp) FrameLocals() Object         { return f.locals }
func (f *fakeInterp) FrameBack() InterpreterFrame { return f.back }
func (f *fakeInterp) FrameLasti() int             { return 0 }
func (f *fakeInterp) FrameNumLocals() int         { return len(f.fast) }
func (f *fakeInterp) FrameFastLocal(i int) Object { return f.fast[i] }
func (f *fakeInterp) FrameNumCells() int          { return len(f.cells) }
func (f *fakeInterp) FrameCellLocal(i int) Object { return f.cells[i] }
func (f *fakeInterp) FrameNumFrees() int          { return len(f.frees) }
func (f *fakeInterp) FrameFreeLocal(i int) Object { return f.frees[i] }

func TestFrameTraverseVisitsScopeAndLocals(t *testing.T) {
	g := NewDict()
	b := NewDict()
	l := NewDict()
	fast := NewStr("fast")
	cell := NewStr("cell")
	free := NewStr("free")
	ip := &fakeInterp{
		globals:  g,
		builtins: b,
		locals:   l,
		fast:     []Object{fast, nil},
		cells:    []Object{cell},
		frees:    []Object{free},
	}
	f := NewFrame(ip)
	tracer := NewStr("trace")
	f.trace = tracer

	got, err := collectVisited(f)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	want := []Object{tracer, g, b, l, fast, cell, free}
	if len(got) != len(want) {
		t.Fatalf("frame traverse = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("frame traverse[%d] = %v, want %v", i, got[i], w)
		}
	}
}

func TestFrameTraverseFollowsBackChain(t *testing.T) {
	outer := &fakeInterp{globals: NewDict()}
	inner := &fakeInterp{globals: NewDict(), back: outer}
	f := NewFrame(inner)
	got, err := collectVisited(f)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 2 || got[0] != inner.globals || got[1] != outer.globals {
		t.Fatalf("frame traverse with back-chain = %v, want [inner-g outer-g]", got)
	}
}

func TestCoroAwaiterTraverseVisitsCoro(t *testing.T) {
	c := NewCoroutine("c")
	w := c.Await()
	got, err := collectVisited(w)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0] != c {
		t.Fatalf("coro_wrapper traverse = %v, want [%v]", got, c)
	}
}

func TestAsyncGenASendTraverseVisitsGenAndVal(t *testing.T) {
	g := NewAsyncGenerator("g")
	v := NewStr("v")
	a := g.Asend(v)
	got, err := collectVisited(a)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 2 || got[0] != g || got[1] != v {
		t.Fatalf("asend traverse = %v, want [%v %v]", got, g, v)
	}
}

func TestAsyncGenAThrowTraverseVisitsGen(t *testing.T) {
	g := NewAsyncGenerator("g")
	a := g.Aclose()
	got, err := collectVisited(a)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0] != g {
		t.Fatalf("athrow traverse = %v, want [%v]", got, g)
	}
}
