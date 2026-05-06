package objects

import "testing"

func TestFrameFastToLocalsAllSlots(t *testing.T) {
	code := &Code{
		Varnames: []string{"x", "y"},
		Cellvars: []string{"c"},
		Freevars: []string{"fr"},
	}
	x := NewInt(1)
	y := NewInt(2)
	c := NewInt(3)
	fr := NewInt(4)
	interp := &fakeInterpFrame{
		code:  code,
		fast:  []Object{x, y},
		cells: []Object{c},
		frees: []Object{fr},
	}
	f := NewFrame(interp)

	d, err := FrameFastToLocals(f)
	if err != nil {
		t.Fatalf("FrameFastToLocals: %v", err)
	}
	cases := map[string]Object{"x": x, "y": y, "c": c, "fr": fr}
	for name, want := range cases {
		got, _ := d.GetItem(NewStr(name))
		if got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	if d.Len() != 4 {
		t.Errorf("len = %d, want 4", d.Len())
	}
}

func TestFrameFastToLocalsSkipsUnbound(t *testing.T) {
	code := &Code{Varnames: []string{"a", "b", "c"}}
	interp := &fakeInterpFrame{
		code: code,
		fast: []Object{NewInt(1), nil, NewInt(3)},
	}
	d, err := FrameFastToLocals(NewFrame(interp))
	if err != nil {
		t.Fatalf("FrameFastToLocals: %v", err)
	}
	if d.Len() != 2 {
		t.Errorf("len = %d, want 2 (b unbound)", d.Len())
	}
	if got, _ := d.GetItem(NewStr("b")); got != nil {
		t.Error("unbound slot should be absent from dict")
	}
}

func TestFrameFastToLocalsNilFrame(t *testing.T) {
	d, err := FrameFastToLocals(nil)
	if err != nil {
		t.Fatalf("nil frame: %v", err)
	}
	if d == nil || d.Len() != 0 {
		t.Error("nil frame should yield an empty dict")
	}
}

func TestFrameFastToLocalsNilCode(t *testing.T) {
	f := NewFrame(&fakeInterpFrame{}) // code is nil
	d, err := FrameFastToLocals(f)
	if err != nil {
		t.Fatalf("nil code: %v", err)
	}
	if d.Len() != 0 {
		t.Errorf("nil code should yield empty dict, got len %d", d.Len())
	}
}

func TestFrameFastToLocalsOrder(t *testing.T) {
	// CPython walks fast -> cells -> frees. Verify a name shadowed
	// across regions ends up holding the last write (frees > cells > fast).
	code := &Code{
		Varnames: []string{"name"},
		Cellvars: []string{"name"},
		Freevars: []string{"name"},
	}
	fast := NewInt(1)
	cell := NewInt(2)
	free := NewInt(3)
	interp := &fakeInterpFrame{
		code:  code,
		fast:  []Object{fast},
		cells: []Object{cell},
		frees: []Object{free},
	}
	d, err := FrameFastToLocals(NewFrame(interp))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetItem(NewStr("name"))
	if got != free {
		t.Errorf("shadowed name = %v, want frees value %v", got, free)
	}
}
