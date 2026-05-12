package _struct

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestCalcsize verifies that calcsize returns the correct byte count.
// struct.calcsize('<iif') should return 12 (4+4+4).
func TestCalcsize(t *testing.T) {
	args := []objects.Object{objects.NewStr("<iif")}
	result, err := moduleCalcsize(args, nil)
	if err != nil {
		t.Fatalf("calcsize error: %v", err)
	}
	n, ok := result.(*objects.Int)
	if !ok {
		t.Fatalf("calcsize returned non-int: %T", result)
	}
	v, _ := n.Int64()
	if v != 12 {
		t.Fatalf("calcsize('<iif') = %d, want 12", v)
	}
}

// TestPackBigHH verifies that pack('>HH', 1, 2) produces \x00\x01\x00\x02.
func TestPackBigHH(t *testing.T) {
	args := []objects.Object{
		objects.NewStr(">HH"),
		objects.NewInt(1),
		objects.NewInt(2),
	}
	result, err := modulePack(args, nil)
	if err != nil {
		t.Fatalf("pack error: %v", err)
	}
	b, ok := result.(*objects.Bytes)
	if !ok {
		t.Fatalf("pack returned non-bytes: %T", result)
	}
	want := []byte{0x00, 0x01, 0x00, 0x02}
	got := b.Bytes()
	if string(got) != string(want) {
		t.Fatalf("pack('>HH', 1, 2) = %v, want %v", got, want)
	}
}

// TestUnpackBigHH verifies that unpack('>HH', b'\x00\x01\x00\x02') returns (1, 2).
func TestUnpackBigHH(t *testing.T) {
	buf := objects.NewBytes([]byte{0x00, 0x01, 0x00, 0x02})
	args := []objects.Object{objects.NewStr(">HH"), buf}
	result, err := moduleUnpack(args, nil)
	if err != nil {
		t.Fatalf("unpack error: %v", err)
	}
	tup, ok := result.(*objects.Tuple)
	if !ok {
		t.Fatalf("unpack returned non-tuple: %T", result)
	}
	if tup.Len() != 2 {
		t.Fatalf("unpack result len = %d, want 2", tup.Len())
	}
	for i, want := range []int64{1, 2} {
		v, ok2 := tup.Item(i).(*objects.Int)
		if !ok2 {
			t.Fatalf("item[%d] is not int: %T", i, tup.Item(i))
		}
		got, _ := v.Int64()
		if got != want {
			t.Fatalf("item[%d] = %d, want %d", i, got, want)
		}
	}
}

// TestStructClass exercises the Struct class round-trip.
func TestStructClass(t *testing.T) {
	sObj, err := structNew(StructType, []objects.Object{objects.NewStr(">HH")}, nil)
	if err != nil {
		t.Fatalf("Struct() error: %v", err)
	}
	s := sObj.(*Struct)
	if s.size != 4 {
		t.Fatalf("Struct.size = %d, want 4", s.size)
	}

	packed, err := structPack([]objects.Object{sObj, objects.NewInt(1), objects.NewInt(2)}, nil)
	if err != nil {
		t.Fatalf("Struct.pack error: %v", err)
	}
	b := packed.(*objects.Bytes)
	want := []byte{0x00, 0x01, 0x00, 0x02}
	if string(b.Bytes()) != string(want) {
		t.Fatalf("Struct.pack result = %v, want %v", b.Bytes(), want)
	}

	unpacked, err := structUnpack([]objects.Object{sObj, packed}, nil)
	if err != nil {
		t.Fatalf("Struct.unpack error: %v", err)
	}
	tup := unpacked.(*objects.Tuple)
	for i, wantVal := range []int64{1, 2} {
		got, _ := tup.Item(i).(*objects.Int).Int64()
		if got != wantVal {
			t.Fatalf("Struct.unpack item[%d] = %d, want %d", i, got, wantVal)
		}
	}
}
