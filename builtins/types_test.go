package builtins

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestInitExposesTypeSingletons(t *testing.T) {
	var buf bytes.Buffer
	dict, err := Init(&buf)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	cases := []struct {
		name string
		t    *objects.Type
	}{
		{"object", objects.ObjectType()},
		{"bytes", objects.BytesType},
		{"bytearray", objects.ByteArrayType},
		{"complex", objects.ComplexType},
		{"slice", objects.SliceType},
		{"property", objects.PropertyType},
		{"classmethod", objects.ClassMethodType},
		{"staticmethod", objects.StaticMethodType},
	}
	for _, c := range cases {
		v, err := dict.GetItem(objects.NewStr(c.name))
		if err != nil || v == nil {
			t.Errorf("%s missing from builtins: err=%v v=%v", c.name, err, v)
			continue
		}
		got, ok := v.(*objects.Type)
		if !ok {
			t.Errorf("%s: got %T, want *objects.Type", c.name, v)
			continue
		}
		if got != c.t {
			t.Errorf("%s: type singleton mismatch", c.name)
		}
	}
}

func TestInitExposesEllipsis(t *testing.T) {
	var buf bytes.Buffer
	dict, err := Init(&buf)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, err := dict.GetItem(objects.NewStr("Ellipsis"))
	if err != nil {
		t.Fatalf("Ellipsis: %v", err)
	}
	if !objects.IsEllipsis(v) {
		t.Fatalf("Ellipsis = %v, want the Ellipsis singleton", v)
	}
}
