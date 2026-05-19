package objects

import (
	"bytes"
	"math/big"
	"testing"
)

func TestIntToByteArray(t *testing.T) {
	cases := []struct {
		name      string
		v         int64
		length    int
		le        bool
		signed    bool
		want      []byte
		expectErr bool
	}{
		{"zero len zero big", 0, 0, false, false, []byte{}, false},
		{"zero len zero little", 0, 0, true, false, []byte{}, false},
		{"one len two big", 1, 2, false, false, []byte{0x00, 0x01}, false},
		{"one len two little", 1, 2, true, false, []byte{0x01, 0x00}, false},
		{"255 len one", 255, 1, false, false, []byte{0xff}, false},
		{"256 len one overflows", 256, 1, false, false, nil, true},
		{"neg one len two signed big", -1, 2, false, true, []byte{0xff, 0xff}, false},
		{"neg one len one signed big", -1, 1, false, true, []byte{0xff}, false},
		{"neg 128 len one signed big", -128, 1, false, true, []byte{0x80}, false},
		{"neg 129 len one signed overflows", -129, 1, false, true, nil, true},
		{"neg one len zero signed overflows", -1, 0, false, true, nil, true},
		{"neg one unsigned errs", -1, 2, false, false, nil, true},
		{"neg one len four signed little", -1, 4, true, true, []byte{0xff, 0xff, 0xff, 0xff}, false},
		{"magic number little", 0x0a0d0e2b, 4, true, false, []byte{0x2b, 0x0e, 0x0d, 0x0a}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := intToByteArray(big.NewInt(c.v), c.length, c.le, c.signed)
			if c.expectErr {
				if err == nil {
					t.Fatalf("expected error, got %x", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("got %x want %x", got, c.want)
			}
		})
	}
}

func TestIntFromByteArray(t *testing.T) {
	cases := []struct {
		name   string
		data   []byte
		le     bool
		signed bool
		want   int64
	}{
		{"empty", []byte{}, false, false, 0},
		{"one byte unsigned big", []byte{0x00, 0x01}, false, false, 1},
		{"one byte unsigned little", []byte{0x01, 0x00}, true, false, 1},
		{"all ones signed big", []byte{0xff, 0xff}, false, true, -1},
		{"all ones one byte signed", []byte{0xff}, false, true, -1},
		{"min byte signed", []byte{0x80}, false, true, -128},
		{"all ones unsigned", []byte{0xff, 0xff}, false, false, 65535},
		{"magic number little", []byte{0x2b, 0x0e, 0x0d, 0x0a}, true, false, 0x0a0d0e2b},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := intFromByteArray(c.data, c.le, c.signed)
			if got.Int64() != c.want {
				t.Fatalf("got %d want %d", got.Int64(), c.want)
			}
		})
	}
}

func TestIntToBytesRoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, -1, 127, -128, 32767, -32768, 0x0a0d0e2b, -0x0a0d0e2b} {
		t.Run("", func(t *testing.T) {
			b, err := intToByteArray(big.NewInt(v), 8, true, true)
			if err != nil {
				t.Fatal(err)
			}
			r := intFromByteArray(b, true, true)
			if r.Int64() != v {
				t.Fatalf("roundtrip %d != %d", r.Int64(), v)
			}
		})
	}
}
