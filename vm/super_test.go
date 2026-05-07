// End-to-end coverage for the super proxy: a `class B(A): ...` source
// can call `super(B, self).method(arg)` and reach A.method through the
// MRO. Exercises the full pipeline (parser, compiler, vm) plus the
// SuperType.Call slot wired in objects/super.go.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestSuperTwoArgChainsToBase(t *testing.T) {
	src := "class A:\n" +
		"  def tag(self):\n" +
		"    return 1\n" +
		"class B(A):\n" +
		"  def tag(self):\n" +
		"    return super(B, self).tag() + 10\n" +
		"b = B()\n" +
		"r = b.tag()\n"
	g := runClassSrc(t, src)
	r := lookupName(t, g, "r")
	n, _ := r.(*objects.Int).Int64()
	if n != 11 {
		t.Fatalf("r = %d, want 11", n)
	}
}

func TestSuperReachesGrandparent(t *testing.T) {
	src := "class A:\n" +
		"  def tag(self):\n" +
		"    return 1\n" +
		"class B(A):\n" +
		"  pass\n" +
		"class C(B):\n" +
		"  def tag(self):\n" +
		"    return super(C, self).tag() + 100\n" +
		"c = C()\n" +
		"r = c.tag()\n"
	g := runClassSrc(t, src)
	r := lookupName(t, g, "r")
	n, _ := r.(*objects.Int).Int64()
	if n != 101 {
		t.Fatalf("r = %d, want 101", n)
	}
}

func TestSuperInitChain(t *testing.T) {
	src := "class A:\n" +
		"  def __init__(self, v):\n" +
		"    self.v = v\n" +
		"class B(A):\n" +
		"  def __init__(self, v):\n" +
		"    super(B, self).__init__(v + 1)\n" +
		"b = B(10)\n"
	g := runClassSrc(t, src)
	b := lookupName(t, g, "b")
	v, err := objects.GetAttr(b, objects.NewStr("v"))
	if err != nil {
		t.Fatalf("b.v: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 11 {
		t.Fatalf("b.v = %d, want 11", n)
	}
}
