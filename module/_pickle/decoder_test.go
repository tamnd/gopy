package _pickle

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestLoadsAtomFromFixtures pins the decoder against the same proto-5
// hex fixtures the encoder gate uses. Driven by `pickle.dumps(value, 5)`
// captures from CPython 3.14, so any decoder mistake shows up as a
// mismatch against the original object.
func TestLoadsAtomFromFixtures(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want objects.Object
	}{
		{"none", "80054e2e", objects.None()},
		{"true", "8005882e", objects.True()},
		{"false", "8005892e", objects.False()},

		{"int_0", "80054b002e", objects.NewInt(0)},
		{"int_1", "80054b012e", objects.NewInt(1)},
		{"int_255", "80054bff2e", objects.NewInt(255)},
		{"int_256", "80059504000000000000004d00012e", objects.NewInt(256)},
		{"int_65535", "80059504000000000000004dffff2e", objects.NewInt(65535)},
		{"int_65536", "80059506000000000000004a000001002e", objects.NewInt(65536)},
		{"int_neg1", "80059506000000000000004affffffff2e", objects.NewInt(-1)},
		{"int_max32", "80059506000000000000004affffff7f2e", objects.NewInt(1<<31 - 1)},
		{"int_min32", "80059506000000000000004a000000802e", objects.NewInt(-1 << 31)},
		{"int_max32p1", "80059508000000000000008a0500000080002e", objects.NewInt(1 << 31)},
		{"int_min32m1", "80059508000000000000008a05ffffff7fff2e", objects.NewInt(-1<<31 - 1)},

		{"float_0", "8005950a000000000000004700000000000000002e", objects.NewFloat(0.0)},
		{"float_1", "8005950a00000000000000473ff00000000000002e", objects.NewFloat(1.0)},
		{"float_neg", "8005950a0000000000000047c0040000000000002e", objects.NewFloat(-2.5)},

		{"bytes_empty", "80059504000000000000004300942e", objects.NewBytes(nil)},
		{"bytes_3", "80059507000000000000004303616263942e", objects.NewBytes([]byte("abc"))},

		{"str_empty", "80059504000000000000008c00942e", objects.NewStr("")},
		{"str_3", "80059507000000000000008c03616263942e", objects.NewStr("abc")},
		{"str_utf8", "8005950a000000000000008c0668c3a96c6c6f942e", objects.NewStr("héllo")},

		{"tuple_empty", "8005292e", objects.NewTuple(nil)},
		{"tuple_1", "80059505000000000000004b0185942e",
			objects.NewTuple([]objects.Object{objects.NewInt(1)})},
		{"tuple_2", "80059507000000000000004b014b0286942e",
			objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})},
		{"tuple_3", "80059509000000000000004b014b024b0387942e",
			objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2), objects.NewInt(3)})},
		{"tuple_4", "8005950c00000000000000284b014b024b034b0474942e",
			objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2), objects.NewInt(3), objects.NewInt(4)})},

		{"list_empty", "80055d942e", objects.NewList(nil)},
		{"list_123", "8005950b000000000000005d94284b014b024b03652e",
			objects.NewList([]objects.Object{
				objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
			})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("hex decode: %v", err)
			}
			got, err := loadsAtom(buf)
			if err != nil {
				t.Fatalf("loadsAtom: %v", err)
			}
			if !objectsEqual(got, c.want) {
				t.Fatalf("loadsAtom mismatch\n got: %v (%T)\nwant: %v (%T)", got, got, c.want, c.want)
			}
		})
	}
}

// TestLoadsAtomBigInt exercises the LONG1 path for values that overflow
// int64.
func TestLoadsAtomBigInt(t *testing.T) {
	hexes := []struct {
		hex  string
		want *big.Int
	}{
		{"80059514000000000000008a11000000000061f5b9abbfa45cc3f129631d2e", bigPow10D(40)},
		{"80059514000000000000008a1100000000009f0a4654405ba33c0ed69ce22e",
			new(big.Int).Neg(bigPow10D(40))},
	}
	for _, c := range hexes {
		buf, err := hex.DecodeString(c.hex)
		if err != nil {
			t.Fatalf("hex decode: %v", err)
		}
		got, err := loadsAtom(buf)
		if err != nil {
			t.Fatalf("loadsAtom: %v", err)
		}
		i, ok := got.(*objects.Int)
		if !ok {
			t.Fatalf("loadsAtom: want *Int, got %T", got)
		}
		if i.BigInt().Cmp(c.want) != 0 {
			t.Fatalf("loadsAtom big int mismatch\n got: %s\nwant: %s", i.BigInt(), c.want)
		}
	}
}

// TestRoundTrip pins encoder + decoder together. Drives values through
// dumpsAtom and back through loadsAtom and verifies structural equality.
func TestRoundTrip(t *testing.T) {
	cases := []objects.Object{
		objects.None(),
		objects.True(),
		objects.False(),
		objects.NewInt(0),
		objects.NewInt(-1),
		objects.NewInt(1 << 31),
		objects.NewStr(""),
		objects.NewStr("abc"),
		objects.NewStr("héllo"),
		objects.NewBytes(nil),
		objects.NewBytes([]byte("abc")),
		objects.NewFloat(3.14159),
		objects.NewFloat(-2.5),
		objects.NewTuple(nil),
		objects.NewTuple([]objects.Object{objects.NewInt(1)}),
		objects.NewTuple([]objects.Object{
			objects.NewInt(1), objects.NewInt(2), objects.NewInt(3), objects.NewInt(4),
		}),
		objects.NewList(nil),
		objects.NewList([]objects.Object{
			objects.NewStr("a"), objects.NewStr("b"), objects.NewStr("c"),
		}),
	}
	for _, in := range cases {
		buf, err := dumpsAtom(in, 5)
		if err != nil {
			t.Fatalf("dumpsAtom: %v", err)
		}
		out, err := loadsAtom(buf)
		if err != nil {
			t.Fatalf("loadsAtom: %v", err)
		}
		if !objectsEqual(out, in) {
			t.Fatalf("round trip mismatch\n  in: %v (%T)\n out: %v (%T)", in, in, out, out)
		}
	}
}

// objectsEqual compares two objects structurally for the proto-5 atom
// + simple container types. Not a general object equality routine; just
// enough for the round-trip fixtures.
func objectsEqual(a, b objects.Object) bool {
	if a == nil || b == nil {
		return a == b
	}
	if objects.IsNone(a) || objects.IsNone(b) {
		return objects.IsNone(a) && objects.IsNone(b)
	}
	switch av := a.(type) {
	case *objects.Bool:
		bv, ok := b.(*objects.Bool)
		return ok && av == bv
	case *objects.Int:
		bv, ok := b.(*objects.Int)
		if !ok {
			return false
		}
		return av.BigInt().Cmp(bv.BigInt()) == 0
	case *objects.Float:
		bv, ok := b.(*objects.Float)
		return ok && av.Float64() == bv.Float64()
	case *objects.Unicode:
		bv, ok := b.(*objects.Unicode)
		return ok && av.Value() == bv.Value()
	case *objects.Bytes:
		bv, ok := b.(*objects.Bytes)
		return ok && string(av.Bytes()) == string(bv.Bytes())
	case *objects.Tuple:
		bv, ok := b.(*objects.Tuple)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		for i := 0; i < av.Len(); i++ {
			if !objectsEqual(av.Item(i), bv.Item(i)) {
				return false
			}
		}
		return true
	case *objects.List:
		bv, ok := b.(*objects.List)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		for i := 0; i < av.Len(); i++ {
			if !objectsEqual(av.Item(i), bv.Item(i)) {
				return false
			}
		}
		return true
	}
	return false
}

func bigPow10D(n int) *big.Int {
	out := new(big.Int).SetInt64(1)
	ten := big.NewInt(10)
	for i := 0; i < n; i++ {
		out.Mul(out, ten)
	}
	return out
}
