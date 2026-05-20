package _pickle

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestDumpsAtomByteEquality pins the Phase 2 encoder against the
// pickle.dumps(value, protocol=5) byte stream CPython emits. Every
// fixture is the exact hex output captured from
// `python3 -c "import pickle; print(pickle.dumps(value, 5).hex())"`
// on CPython 3.14, so any drift in opcode selection, FRAME framing,
// MEMOIZE placement, or two's-complement encoding shows up as a
// hex-level diff.
//
// The fixtures intentionally cover the BININT1 / BININT2 / BININT /
// LONG1 width boundaries and the SHORT_BINBYTES / BINBYTES split at
// 256 bytes, since those are the opcode-selection edges save_long
// and save_bytes care about.
func TestDumpsAtomByteEquality(t *testing.T) {
	xstr := func(n int, r rune) string {
		return strings.Repeat(string(r), n)
	}
	xbytes := func(n int, b byte) []byte {
		out := make([]byte, n)
		for i := range out {
			out[i] = b
		}
		return out
	}

	cases := []struct {
		name string
		obj  objects.Object
		hex  string
	}{
		{"none", objects.None(), "80054e2e"},
		{"true", objects.True(), "8005882e"},
		{"false", objects.False(), "8005892e"},

		// BININT1 width: 0..255 fit in one unsigned byte.
		{"int_0", objects.NewInt(0), "80054b002e"},
		{"int_1", objects.NewInt(1), "80054b012e"},
		{"int_255", objects.NewInt(255), "80054bff2e"},

		// BININT2 width: 256..65535 fit in two LE unsigned bytes.
		{"int_256", objects.NewInt(256), "80059504000000000000004d00012e"},
		{"int_65535", objects.NewInt(65535), "80059504000000000000004dffff2e"},

		// BININT width: -2^31..2^31-1 minus the smaller widths.
		{"int_65536", objects.NewInt(65536), "80059506000000000000004a000001002e"},
		{"int_neg1", objects.NewInt(-1), "80059506000000000000004affffffff2e"},
		{"int_max32", objects.NewInt(1<<31 - 1), "80059506000000000000004affffff7f2e"},
		{"int_min32", objects.NewInt(-1 << 31), "80059506000000000000004a000000802e"},

		// LONG1 path: values that exceed int32.
		{"int_max32p1", objects.NewInt(1 << 31), "80059508000000000000008a0500000080002e"},
		{"int_min32m1", objects.NewInt(-1<<31 - 1), "80059508000000000000008a05ffffff7fff2e"},

		// Huge ints exercising the BigInt LONG1 path.
		{"int_huge", objects.NewIntFromBig(bigPow10(40)),
			"80059514000000000000008a11000000000061f5b9abbfa45cc3f129631d2e"},
		{"int_neg_huge", objects.NewIntFromBig(new(big.Int).Neg(bigPow10(40))),
			"80059514000000000000008a1100000000009f0a4654405ba33c0ed69ce22e"},

		// BINFLOAT: 8 big-endian IEEE 754 bytes.
		{"float_0", objects.NewFloat(0.0), "8005950a000000000000004700000000000000002e"},
		{"float_1", objects.NewFloat(1.0), "8005950a00000000000000473ff00000000000002e"},
		{"float_pi", objects.NewFloat(3.14159265358979), "8005950a0000000000000047400921fb54442d112e"},
		{"float_neg", objects.NewFloat(-2.5), "8005950a0000000000000047c0040000000000002e"},

		// SHORT_BINBYTES (size <= 0xff) plus MEMOIZE.
		{"bytes_empty", objects.NewBytes(nil), "80059504000000000000004300942e"},
		{"bytes_3", objects.NewBytes([]byte("abc")), "80059507000000000000004303616263942e"},
		{"bytes_255", objects.NewBytes(xbytes(255, 'x')),
			"800595030100000000000043ff" + strings.Repeat("78", 255) + "942e"},

		// BINBYTES (256 byte payload crosses the SHORT_BINBYTES boundary).
		{"bytes_256", objects.NewBytes(xbytes(256, 'x')),
			"8005950701000000000000420001000078" + strings.Repeat("78", 255) + "942e"},

		// SHORT_BINUNICODE / BINUNICODE plus MEMOIZE.
		{"str_empty", objects.NewStr(""), "80059504000000000000008c00942e"},
		{"str_3", objects.NewStr("abc"), "80059507000000000000008c03616263942e"},
		{"str_255", objects.NewStr(xstr(255, 'x')),
			"80059503010000000000008cff" + strings.Repeat("78", 255) + "942e"},
		{"str_256", objects.NewStr(xstr(256, 'x')),
			"8005950701000000000000580001000078" + strings.Repeat("78", 255) + "942e"},

		// Non-ASCII str: 'héllo' encodes to 6 UTF-8 bytes.
		{"str_utf8", objects.NewStr("héllo"), "8005950a000000000000008c0668c3a96c6c6f942e"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := dumpsAtom(c.obj, 5)
			if err != nil {
				t.Fatalf("dumpsAtom: %v", err)
			}
			want, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("hex decode fixture: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("byte mismatch\n got: %s\nwant: %s", hex.EncodeToString(got), c.hex)
			}
		})
	}
}

// TestFrameSuppression locks in the FRAME_SIZE_MIN behaviour: a body
// shorter than 4 bytes leaves no FRAME header on the wire (so a bare
// `pickle.dumps(None)` is just `PROTO 5 NONE STOP`, 4 bytes total).
// A body that clears 4 bytes triggers the FRAME header and the 8-byte
// LE length. This invariant lives in CPython's _Pickler_CommitFrame
// at Modules/_pickle.c:993.
func TestFrameSuppression(t *testing.T) {
	b, err := dumpsAtom(objects.None(), 5)
	if err != nil {
		t.Fatalf("dumpsAtom None: %v", err)
	}
	if len(b) != 4 {
		t.Fatalf("dumpsAtom(None) length: got %d, want 4", len(b))
	}
	if b[2] == opFrame {
		t.Fatalf("dumpsAtom(None) emitted FRAME: % x", b)
	}

	b, err = dumpsAtom(objects.NewInt(256), 5)
	if err != nil {
		t.Fatalf("dumpsAtom 256: %v", err)
	}
	if b[2] != opFrame {
		t.Fatalf("dumpsAtom(256) missing FRAME: % x", b)
	}
}

func bigPow10(n int) *big.Int {
	out := new(big.Int).SetInt64(1)
	ten := big.NewInt(10)
	for i := 0; i < n; i++ {
		out.Mul(out, ten)
	}
	return out
}
