package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func storeSubBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.STORE_SUBSCR)))
	buf[0] = byte(compile.STORE_SUBSCR)
	return buf
}

func TestStoreSubscrListInt(t *testing.T) {
	buf := storeSubBuf()
	l := objects.NewList([]objects.Object{objects.NewInt(0), objects.NewInt(1)})
	StoreSubscr(l, objects.NewInt(1), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_SUBSCR_LIST_INT {
		t.Fatalf("opcode: got %s want STORE_SUBSCR_LIST_INT", got.Name())
	}
}

func TestStoreSubscrListIntOutOfRange(t *testing.T) {
	buf := storeSubBuf()
	l := objects.NewList([]objects.Object{objects.NewInt(0)})
	StoreSubscr(l, objects.NewInt(5), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_SUBSCR {
		t.Fatalf("opcode: got %s want STORE_SUBSCR (out-of-range falls back)", got.Name())
	}
}

func TestStoreSubscrListIntNegative(t *testing.T) {
	buf := storeSubBuf()
	l := objects.NewList([]objects.Object{objects.NewInt(0)})
	StoreSubscr(l, objects.NewInt(-1), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_SUBSCR {
		t.Fatalf("opcode: got %s want STORE_SUBSCR (negative falls back)", got.Name())
	}
}

func TestStoreSubscrListSliceFallsBack(t *testing.T) {
	buf := storeSubBuf()
	l := objects.NewList(nil)
	sl := objects.NewSlice(objects.None(), objects.None(), objects.None())
	StoreSubscr(l, sl, buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_SUBSCR {
		t.Fatalf("opcode: got %s want STORE_SUBSCR (slice has no variant)", got.Name())
	}
}

func TestStoreSubscrDict(t *testing.T) {
	buf := storeSubBuf()
	StoreSubscr(objects.NewDict(), objects.NewStr("k"), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_SUBSCR_DICT {
		t.Fatalf("opcode: got %s want STORE_SUBSCR_DICT", got.Name())
	}
}

func TestStoreSubscrUnspecializeOther(t *testing.T) {
	buf := storeSubBuf()
	StoreSubscr(objects.NewTuple(nil), objects.NewInt(0), buf, 0)
	if got := compile.Opcode(buf[0]); got != compile.STORE_SUBSCR {
		t.Fatalf("opcode: got %s want STORE_SUBSCR", got.Name())
	}
}
