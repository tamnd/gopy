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
	if len(got) == 0 || got[0] != fn {
		t.Fatalf("classmethod traverse = %v, want callable first", got)
	}
	// CPython visits cm_dict too once functools_wraps allocates it; we
	// only assert the callable lands as the first visit.
}

func TestStaticMethodTraverseVisitsCallable(t *testing.T) {
	fn := NewStr("fn")
	sm := NewStaticMethod(fn)
	got, err := collectVisited(sm)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) == 0 || got[0] != fn {
		t.Fatalf("staticmethod traverse = %v, want callable first", got)
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
func (f *fakeInterp) FrameFunc() Object           { return nil }
func (f *fakeInterp) FrameClearLocals() {
	f.fast = nil
	f.cells = nil
	f.frees = nil
}
func (f *fakeInterp) FrameTakeOwnership()       {}
func (f *fakeInterp) FrameNumStack() int        { return 0 }
func (f *fakeInterp) FrameStackItem(int) Object { return nil }
func (f *fakeInterp) FrameGenOwner() Object     { return nil }
func (f *fakeInterp) FrameLocalsPlusItem(i int) Object {
	n := len(f.fast)
	if i < n {
		return f.fast[i]
	}
	if i < n+len(f.cells) {
		return f.cells[i-n]
	}
	if i < n+len(f.cells)+len(f.frees) {
		return f.frees[i-n-len(f.cells)]
	}
	return nil
}

func (f *fakeInterp) FrameSetLocalsPlusItem(i int, v Object) {
	n := len(f.fast)
	switch {
	case i < n:
		f.fast[i] = v
	case i < n+len(f.cells):
		f.cells[i-n] = v
	case i < n+len(f.cells)+len(f.frees):
		f.frees[i-n-len(f.cells)] = v
	}
}

func TestFrameTraverseVisitsScopeAndLocals(t *testing.T) {
	l := NewDict()
	fast := NewStr("fast")
	cell := NewStr("cell")
	free := NewStr("free")
	ip := &fakeInterp{
		globals:  NewDict(),
		builtins: NewDict(),
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
	// f_globals / f_builtins are intentionally NOT in the want list:
	// CPython reaches them transitively via f_funcobj's tp_traverse
	// (Python/frame.c:11 _PyFrame_Traverse). Visiting them directly
	// here over-counts shared module dicts across recursive frames.
	want := []Object{tracer, l, fast, cell, free}
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
	innerLocal := NewStr("inner-local")
	outerLocal := NewStr("outer-local")
	outer := &fakeInterp{globals: NewDict(), fast: []Object{outerLocal}}
	inner := &fakeInterp{globals: NewDict(), fast: []Object{innerLocal}, back: outer}
	f := NewFrame(inner)
	got, err := collectVisited(f)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	// Per-frame state from both iframes must surface. Globals/builtins
	// stay out (reached via f_funcobj in CPython); the back-chain walk
	// has to land the outer iframe's locals so generators / suspended
	// frames still keep their captured state alive.
	if len(got) != 2 || got[0] != innerLocal || got[1] != outerLocal {
		t.Fatalf("frame traverse with back-chain = %v, want [inner-local outer-local]", got)
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
