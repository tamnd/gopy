package marshal

import (
	"bytes"
	"reflect"
	"testing"
)

// TestRoundtrip walks each value supported by the v0.5 skeleton
// through Dump then Load and asserts the result equals the input.
func TestRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		in   any
		out  any // expected after Load (may differ by widening)
	}{
		{"None", nil, nil},
		{"True", true, true},
		{"False", false, false},
		{"int small", int64(42), int64(42)},
		{"int neg", int64(-1), int64(-1)},
		{"int max32", int64(0x7FFFFFFF), int64(0x7FFFFFFF)},
		{"int wider", int64(0x100000000), int64(0x100000000)},
		{"float", 3.14, 3.14},
		{"ascii short", "hello", "hello"},
		{"unicode long", string(make([]byte, 300)), string(make([]byte, 300))},
		{"bytes", []byte{0x00, 0xff, 0x7f}, []byte{0x00, 0xff, 0x7f}},
		{"tuple small", []any{int64(1), "two", nil}, []any{int64(1), "two", nil}},
		{"tuple nested", []any{[]any{int64(1)}, true}, []any{[]any{int64(1)}, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Dump(&buf, tc.in); err != nil {
				t.Fatalf("Dump: %v", err)
			}
			got, err := Load(&buf)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(got, tc.out) {
				t.Errorf("got %#v, want %#v", got, tc.out)
			}
		})
	}
}

// TestUnmarshallable: a value whose type is outside the v0.5 subset
// surfaces ErrUnmarshallable rather than panicking.
func TestUnmarshallable(t *testing.T) {
	var buf bytes.Buffer
	err := Dump(&buf, complex(1, 2))
	if err == nil {
		t.Fatal("expected error for complex value")
	}
}

// TestVersionConstant pins the wire-format version against
// CPython 3.14's Py_MARSHAL_VERSION.
func TestVersionConstant(t *testing.T) {
	if Version != 5 {
		t.Errorf("Version = %d, want 5", Version)
	}
}
