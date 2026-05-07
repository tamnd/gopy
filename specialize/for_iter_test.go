package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func forIterBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.FOR_ITER)))
	buf[0] = byte(compile.FOR_ITER)
	return buf
}

func TestForIterList(t *testing.T) {
	buf := forIterBuf()
	it, err := objects.Iter(objects.NewList(nil))
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	ForIter(it, buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.FOR_ITER_LIST {
		t.Fatalf("opcode: got %s want FOR_ITER_LIST", got.Name())
	}
}

func TestForIterTuple(t *testing.T) {
	buf := forIterBuf()
	it, err := objects.Iter(objects.NewTuple(nil))
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	ForIter(it, buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.FOR_ITER_TUPLE {
		t.Fatalf("opcode: got %s want FOR_ITER_TUPLE", got.Name())
	}
}

func TestForIterRange(t *testing.T) {
	buf := forIterBuf()
	r, err := objects.NewRange(objects.NewInt(0), objects.NewInt(3), objects.NewInt(1))
	if err != nil {
		t.Fatalf("NewRange: %v", err)
	}
	it, err := objects.Iter(r)
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	ForIter(it, buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.FOR_ITER_RANGE {
		t.Fatalf("opcode: got %s want FOR_ITER_RANGE", got.Name())
	}
}

func TestForIterUnspecializeOther(t *testing.T) {
	buf := forIterBuf()
	it, err := objects.Iter(objects.NewDict())
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	ForIter(it, buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.FOR_ITER {
		t.Fatalf("opcode: got %s want FOR_ITER", got.Name())
	}
}
