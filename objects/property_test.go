package objects

import (
	"errors"
	"testing"
)

func TestPropertyGet(t *testing.T) {
	fget := NewBuiltinFunction("fget", func(args []Object, _ map[string]Object) (Object, error) {
		return NewInt(7), nil
	})
	p := NewProperty(fget, nil, nil, nil)
	v, err := propertyDescrGet(p, NewInt(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	iv := v.(*Int)
	n, _ := iv.Int64()
	if n != 7 {
		t.Errorf("got %d, want 7", n)
	}
}

func TestPropertyGetMissing(t *testing.T) {
	p := NewProperty(nil, nil, nil, nil)
	if _, err := propertyDescrGet(p, NewInt(0), nil); err == nil {
		t.Error("read on no-getter property should error")
	}
}

func TestPropertySet(t *testing.T) {
	captured := int64(0)
	fset := NewBuiltinFunction("fset", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, errors.New("expect 2 args")
		}
		iv, ok := args[1].(*Int)
		if !ok {
			return nil, errors.New("expect int value")
		}
		captured, _ = iv.Int64()
		return None(), nil
	})
	p := NewProperty(nil, fset, nil, nil)
	if err := propertyDescrSet(p, NewInt(0), NewInt(42)); err != nil {
		t.Fatal(err)
	}
	if captured != 42 {
		t.Errorf("captured = %d, want 42", captured)
	}
}

func TestPropertyClassLevelAccess(t *testing.T) {
	p := NewProperty(nil, nil, nil, nil)
	v, err := propertyDescrGet(p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != p {
		t.Error("class-level access should return property")
	}
}

func TestPropertyDeleterMissing(t *testing.T) {
	p := NewProperty(nil, nil, nil, nil)
	if err := propertyDescrSet(p, NewInt(0), nil); err == nil {
		t.Error("delete on no-deleter property should error")
	}
}

func TestPropertyChain(t *testing.T) {
	fget := NewBuiltinFunction("fget", func(args []Object, _ map[string]Object) (Object, error) {
		return NewInt(1), nil
	})
	fset := NewBuiltinFunction("fset", func(args []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	p := NewProperty(nil, nil, nil, nil).Getter(fget).Setter(fset)
	if p.fget != fget || p.fset != fset {
		t.Error("decorator chain did not preserve fget/fset")
	}
}
