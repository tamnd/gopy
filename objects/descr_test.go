package objects

import (
	"errors"
	"testing"
)

func TestGetSetDescrReadOnly(t *testing.T) {
	d := NewGetSetDescr("x",
		func(owner Object) (Object, error) {
			return NewInt(42), nil
		},
		nil,
	)
	v, err := getsetDescrGet(d, NewInt(0), nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	iv, ok := v.(*Int)
	if !ok {
		t.Fatalf("get returned %T", v)
	}
	n, _ := iv.Int64()
	if n != 42 {
		t.Errorf("got %d, want 42", n)
	}
	if err := getsetDescrSet(d, NewInt(0), NewInt(7)); err == nil {
		t.Error("set on read-only descriptor should error")
	}
}

func TestGetSetDescrReadWrite(t *testing.T) {
	storage := int64(0)
	d := NewGetSetDescr("x",
		func(owner Object) (Object, error) {
			return NewInt(storage), nil
		},
		func(owner Object, value Object) error {
			iv, ok := value.(*Int)
			if !ok {
				return errors.New("TypeError")
			}
			storage, _ = iv.Int64()
			return nil
		},
	)
	if err := getsetDescrSet(d, NewInt(0), NewInt(99)); err != nil {
		t.Fatalf("set: %v", err)
	}
	if storage != 99 {
		t.Fatalf("storage = %d, want 99", storage)
	}
}

func TestGetSetDescrClassAccess(t *testing.T) {
	d := NewGetSetDescr("x",
		func(owner Object) (Object, error) {
			t.Error("fget called on class-level access")
			return nil, nil
		},
		nil,
	)
	v, err := getsetDescrGet(d, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != d {
		t.Errorf("class access should return descriptor, got %v", v)
	}
}

func TestLookupDescriptor(t *testing.T) {
	tp := NewType("desctest", []*Type{objectType})
	d := NewGetSetDescr("foo",
		func(owner Object) (Object, error) { return NewInt(1), nil },
		nil,
	)
	SetTypeDescr(tp, "foo", d)
	got, where := LookupDescriptor(tp, "foo")
	if got != d {
		t.Errorf("LookupDescriptor returned %v, want %v", got, d)
	}
	if where != tp {
		t.Errorf("LookupDescriptor located %v, want %v", where, tp)
	}
	if got, _ := LookupDescriptor(tp, "missing"); got != nil {
		t.Errorf("LookupDescriptor(missing) = %v, want nil", got)
	}
}
