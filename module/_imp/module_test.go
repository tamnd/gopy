package _imp

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/tamnd/gopy/hash"
	"github.com/tamnd/gopy/marshal"
	"github.com/tamnd/gopy/objects"
)

// TestSourceHashMatchesKeyedHash pins that source_hash(key, data)
// returns the 8 little-endian bytes of hash.KeyedHash, byte-for-byte.
// The .pyc hash-mode header stores this value verbatim, so any drift
// from CPython here propagates straight into the byte-equality gate.
func TestSourceHashMatchesKeyedHash(t *testing.T) {
	src := []byte("print('hello')\n")
	var key uint64 = 0x0123456789ABCDEF

	out, err := sourceHash([]objects.Object{
		objects.NewInt(int64(key)),
		objects.NewBytes(src),
	}, nil)
	if err != nil {
		t.Fatalf("sourceHash: %v", err)
	}
	b, ok := out.(*objects.Bytes)
	if !ok {
		t.Fatalf("sourceHash returned %T, want *objects.Bytes", out)
	}
	if len(b.Bytes()) != 8 {
		t.Fatalf("sourceHash returned %d bytes, want 8", len(b.Bytes()))
	}

	var want [8]byte
	binary.LittleEndian.PutUint64(want[:], hash.KeyedHash(key, src))
	if !bytes.Equal(b.Bytes(), want[:]) {
		t.Errorf("sourceHash bytes = % x, want % x", b.Bytes(), want[:])
	}
}

// TestSourceHashAcceptsStr pins that source_hash accepts a str source
// (CPython's Py_buffer would, by virtue of __buffer__-on-str through
// the encode path; our toBuffer takes the same liberty importlib does).
func TestSourceHashAcceptsStr(t *testing.T) {
	_, err := sourceHash([]objects.Object{
		objects.NewInt(0),
		objects.NewStr("x"),
	}, nil)
	if err != nil {
		t.Fatalf("sourceHash(str): %v", err)
	}
}

// TestSourceHashBadArity pins the TypeError surface for too-few /
// too-many positional args.
func TestSourceHashBadArity(t *testing.T) {
	for _, args := range [][]objects.Object{
		{},
		{objects.NewInt(0)},
		{objects.NewInt(0), objects.NewBytes(nil), objects.NewBytes(nil)},
	} {
		_, err := sourceHash(args, nil)
		if err == nil {
			t.Errorf("sourceHash(%d args): expected error", len(args))
		}
	}
}

// TestPycMagicNumberTokenMatchesMarshal pins that the int exposed as
// _imp.pyc_magic_number_token equals marshal.MagicNumber, so that
// importlib's MAGIC_NUMBER = pyc_magic_number_token.to_bytes(4, 'little')
// produces the same four bytes WritePyc writes.
func TestPycMagicNumberTokenMatchesMarshal(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	v, err := m.Dict().GetItem(objects.NewStr("pyc_magic_number_token"))
	if err != nil {
		t.Fatalf("GetItem(pyc_magic_number_token): %v", err)
	}
	i, ok := v.(*objects.Int)
	if !ok {
		t.Fatalf("pyc_magic_number_token = %T, want *objects.Int", v)
	}
	got, _ := i.Int64()
	if uint32(got) != marshal.MagicNumber {
		t.Errorf("pyc_magic_number_token = 0x%08x, want 0x%08x", uint32(got), marshal.MagicNumber)
	}
}
