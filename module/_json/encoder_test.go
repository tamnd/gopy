package _json

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// makeBenchEncoder builds an Encoder configured the same way
// json.dumps(obj) does (no indent, ", " / ": " separators, no sorting,
// allow_nan=True, encode_basestring_ascii for the string fast path).
//
// CPython: Lib/json/encoder.py:253 c_make_encoder(...).
func makeBenchEncoder(t *testing.T) *Encoder {
	t.Helper()
	ascii := objects.NewBuiltinFunction("encode_basestring_ascii", moduleEncodeBasestringASCII)
	args := []objects.Object{
		objects.None(),
		objects.NewBuiltinFunction("default", func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
			return nil, nil
		}),
		ascii,
		objects.None(),
		objects.NewStr(": "),
		objects.NewStr(", "),
		objects.False(),
		objects.False(),
		objects.True(),
	}
	o, err := encoderTpNew(encoderType, args, nil)
	if err != nil {
		t.Fatalf("encoderTpNew: %v", err)
	}
	return o.(*Encoder)
}

func encodeOne(t *testing.T, e *Encoder, obj objects.Object) string {
	t.Helper()
	got, err := encoderCall(e, []objects.Object{obj, objects.NewInt(0)}, nil)
	if err != nil {
		t.Fatalf("encoderCall: %v", err)
	}
	tup, ok := got.(*objects.Tuple)
	if !ok {
		t.Fatalf("encoderCall returned %T, want tuple", got)
	}
	if tup.Len() != 1 {
		t.Fatalf("tuple len %d, want 1", tup.Len())
	}
	s, err := objects.Str(tup.Item(0))
	if err != nil {
		t.Fatalf("Str: %v", err)
	}
	return s
}

func TestEncoderEmptyDict(t *testing.T) {
	e := makeBenchEncoder(t)
	d := objects.NewDict()
	if got := encodeOne(t, e, d); got != "{}" {
		t.Fatalf("encode {} = %q, want %q", got, "{}")
	}
}

func TestEncoderEmptyList(t *testing.T) {
	e := makeBenchEncoder(t)
	l := objects.NewList(nil)
	if got := encodeOne(t, e, l); got != "[]" {
		t.Fatalf("encode [] = %q, want %q", got, "[]")
	}
}

func TestEncoderScalars(t *testing.T) {
	e := makeBenchEncoder(t)
	cases := []struct {
		obj  objects.Object
		want string
	}{
		{objects.None(), "null"},
		{objects.True(), "true"},
		{objects.False(), "false"},
		{objects.NewInt(42), "42"},
		{objects.NewStr("hello"), `"hello"`},
		{objects.NewStr("a\"b"), `"a\"b"`},
	}
	for _, c := range cases {
		if got := encodeOne(t, e, c.obj); got != c.want {
			t.Fatalf("encode %v = %q, want %q", c.obj, got, c.want)
		}
	}
}

// TestEncoderSimpleDict mirrors json_dumps SIMPLE bench shape.
func TestEncoderSimpleDict(t *testing.T) {
	e := makeBenchEncoder(t)
	d := objects.NewDict()
	mustSet(t, d, "key1", objects.NewInt(0))
	mustSet(t, d, "key2", objects.True())
	mustSet(t, d, "key3", objects.NewStr("value"))
	mustSet(t, d, "key4", objects.NewStr("foo"))
	mustSet(t, d, "key5", objects.NewStr("bar"))
	got := encodeOne(t, e, d)
	want := `{"key1": 0, "key2": true, "key3": "value", "key4": "foo", "key5": "bar"}`
	if got != want {
		t.Fatalf("simple dict:\ngot:  %s\nwant: %s", got, want)
	}
}

// TestEncoderNestedDict mirrors json_dumps NESTED shape.
func TestEncoderNestedDict(t *testing.T) {
	e := makeBenchEncoder(t)
	simple := objects.NewDict()
	mustSet(t, simple, "k", objects.NewInt(1))
	outer := objects.NewDict()
	mustSet(t, outer, "key", simple)
	mustSet(t, outer, "list", objects.NewList([]objects.Object{simple, simple}))
	got := encodeOne(t, e, outer)
	want := `{"key": {"k": 1}, "list": [{"k": 1}, {"k": 1}]}`
	if got != want {
		t.Fatalf("nested:\ngot:  %s\nwant: %s", got, want)
	}
}

// TestEncoderListOfDicts is the bench HUGE shape with the dict count
// dropped to two so the assertion stays short.
func TestEncoderListOfDicts(t *testing.T) {
	e := makeBenchEncoder(t)
	inner := objects.NewDict()
	mustSet(t, inner, "a", objects.NewInt(1))
	mustSet(t, inner, "b", objects.NewInt(2))
	huge := objects.NewList([]objects.Object{inner, inner})
	got := encodeOne(t, e, huge)
	want := `[{"a": 1, "b": 2}, {"a": 1, "b": 2}]`
	if got != want {
		t.Fatalf("huge:\ngot:  %s\nwant: %s", got, want)
	}
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("missing brackets: %q", got)
	}
}

func mustSet(t *testing.T, d *objects.Dict, key string, v objects.Object) {
	t.Helper()
	if err := d.SetItem(objects.NewStr(key), v); err != nil {
		t.Fatalf("SetItem(%q): %v", key, err)
	}
}
