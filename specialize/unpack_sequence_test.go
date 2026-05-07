package specialize

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func unpackBuf() []byte {
	buf := make([]byte, 2*(1+CacheCount(compile.UNPACK_SEQUENCE)))
	buf[0] = byte(compile.UNPACK_SEQUENCE)
	return buf
}

func TestUnpackSequenceTwoTuple(t *testing.T) {
	buf := unpackBuf()
	tup := objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	UnpackSequence(tup, buf, 0, 2)
	if got := compile.Opcode(buf[0]); got != compile.UNPACK_SEQUENCE_TWO_TUPLE {
		t.Fatalf("opcode: got %s want UNPACK_SEQUENCE_TWO_TUPLE", got.Name())
	}
}

func TestUnpackSequenceTuple(t *testing.T) {
	buf := unpackBuf()
	tup := objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2), objects.NewInt(3)})
	UnpackSequence(tup, buf, 0, 3)
	if got := compile.Opcode(buf[0]); got != compile.UNPACK_SEQUENCE_TUPLE {
		t.Fatalf("opcode: got %s want UNPACK_SEQUENCE_TUPLE", got.Name())
	}
}

func TestUnpackSequenceList(t *testing.T) {
	buf := unpackBuf()
	l := objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	UnpackSequence(l, buf, 0, 2)
	if got := compile.Opcode(buf[0]); got != compile.UNPACK_SEQUENCE_LIST {
		t.Fatalf("opcode: got %s want UNPACK_SEQUENCE_LIST", got.Name())
	}
}

func TestUnpackSequenceLengthMismatch(t *testing.T) {
	buf := unpackBuf()
	tup := objects.NewTuple([]objects.Object{objects.NewInt(1)})
	UnpackSequence(tup, buf, 0, 2)
	if got := compile.Opcode(buf[0]); got != compile.UNPACK_SEQUENCE {
		t.Fatalf("opcode: got %s want UNPACK_SEQUENCE", got.Name())
	}
}

func TestUnpackSequenceUnspecializeOther(t *testing.T) {
	buf := unpackBuf()
	UnpackSequence(objects.NewDict(), buf, 0, 0)
	if got := compile.Opcode(buf[0]); got != compile.UNPACK_SEQUENCE {
		t.Fatalf("opcode: got %s want UNPACK_SEQUENCE", got.Name())
	}
}
