// End-to-end coverage for LOAD_BUILD_CLASS plus __build_class__: a
// `class C: ...` source program parses, compiles, runs, and produces
// a usable class object whose attributes are reachable from instances
// and whose methods bind through the descriptor protocol.

package vm

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
)

func runClassSrc(t *testing.T, src string) *objects.Dict {
	t.Helper()
	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	if src[len(src)-1] != '\n' {
		src += "\n"
	}
	mod, err := parser.ParseString(src, "<class_test>", parser.ModeFile)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cco, err := compile.Compile(mod, "<class_test>", 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	co := liftCompileCode(cco)
	ts := state.NewThread()
	if _, err := EvalCode(ts, co, g, nil); err != nil {
		t.Fatalf("eval: %v", err)
	}
	return g
}

// liftCompileCode is the local copy of pythonrun.liftCode; pythonrun
// is downstream of vm so we can't import it from here.
func liftCompileCode(c *compile.Code) *objects.Code {
	return &objects.Code{
		Code:           c.Code,
		Consts:         c.Consts,
		Names:          c.Names,
		Varnames:       c.VarNames,
		Freevars:       c.FreeVars,
		Cellvars:       c.CellVars,
		Stacksize:      c.Stacksize,
		ExceptionTable: c.ExceptionTable,
	}
}

func lookupName(t *testing.T, g *objects.Dict, name string) objects.Object {
	t.Helper()
	v, err := g.GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("globals[%q]: %v", name, err)
	}
	return v
}

func TestBuildClassEmptyBody(t *testing.T) {
	g := runClassSrc(t, "class C:\n  pass\n")
	c, ok := lookupName(t, g, "C").(*objects.Type)
	if !ok {
		t.Fatalf("C is %T, want *Type", lookupName(t, g, "C"))
	}
	if c.Name != "C" {
		t.Fatalf("C.Name = %q, want %q", c.Name, "C")
	}
	if !c.IsUser {
		t.Fatal("C.IsUser = false, want true for a Python-defined class")
	}
}

func TestBuildClassAttributeOnType(t *testing.T) {
	g := runClassSrc(t, "class C:\n  x = 7\n")
	c := lookupName(t, g, "C").(*objects.Type)
	descr, _ := objects.LookupDescriptor(c, "x")
	if descr == nil {
		t.Fatal("C.x missing from descriptor table")
	}
	n, _ := descr.(*objects.Int).Int64()
	if n != 7 {
		t.Fatalf("C.x = %d, want 7", n)
	}
}

func TestBuildClassInstantiation(t *testing.T) {
	g := runClassSrc(t, "class C:\n  x = 7\nc = C()\n")
	c := lookupName(t, g, "c")
	if _, ok := c.(*objects.Instance); !ok {
		t.Fatalf("c is %T, want *Instance", c)
	}
	v, err := objects.GetAttr(c, objects.NewStr("x"))
	if err != nil {
		t.Fatalf("c.x: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 7 {
		t.Fatalf("c.x = %d, want 7", n)
	}
}

func TestBuildClassInstanceAttrSetGet(t *testing.T) {
	src := "class C:\n  pass\nc = C()\nc.y = 42\n"
	g := runClassSrc(t, src)
	c := lookupName(t, g, "c")
	v, err := objects.GetAttr(c, objects.NewStr("y"))
	if err != nil {
		t.Fatalf("c.y: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 42 {
		t.Fatalf("c.y = %d, want 42", n)
	}
}

func TestBuildClassMethodBindAndCall(t *testing.T) {
	src := "class C:\n" +
		"  def foo(self):\n" +
		"    return 99\n" +
		"c = C()\n" +
		"r = c.foo()\n"
	g := runClassSrc(t, src)
	r := lookupName(t, g, "r")
	n, _ := r.(*objects.Int).Int64()
	if n != 99 {
		t.Fatalf("r = %d, want 99", n)
	}
}

func TestBuildClassInitRunsWithArgs(t *testing.T) {
	src := "class C:\n" +
		"  def __init__(self, v):\n" +
		"    self.v = v\n" +
		"c = C(11)\n"
	g := runClassSrc(t, src)
	c := lookupName(t, g, "c")
	v, err := objects.GetAttr(c, objects.NewStr("v"))
	if err != nil {
		t.Fatalf("c.v: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 11 {
		t.Fatalf("c.v = %d, want 11", n)
	}
}

func TestBuildClassInheritsBaseAttrs(t *testing.T) {
	src := "class A:\n  x = 1\n" +
		"class B(A):\n  pass\n" +
		"b = B()\n"
	g := runClassSrc(t, src)
	b := lookupName(t, g, "b")
	v, err := objects.GetAttr(b, objects.NewStr("x"))
	if err != nil {
		t.Fatalf("b.x: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 1 {
		t.Fatalf("b.x = %d, want 1", n)
	}
}

func TestBuildClassMethodAccessThroughClassReturnsRawFunction(t *testing.T) {
	src := "class C:\n" +
		"  def foo(self):\n" +
		"    return 1\n"
	g := runClassSrc(t, src)
	c := lookupName(t, g, "C").(*objects.Type)
	descr, _ := objects.LookupDescriptor(c, "foo")
	if _, ok := descr.(*objects.Function); !ok {
		t.Fatalf("C.foo descriptor = %T, want *Function", descr)
	}
}
