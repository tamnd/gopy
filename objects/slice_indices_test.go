package objects

import "testing"

func TestSliceUnpackDefaults(t *testing.T) {
	s := NewSlice(None(), None(), None())
	start, stop, step, err := s.Unpack()
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if step != 1 || start != 0 || stop != ssizeMax {
		t.Fatalf("defaults = (%d, %d, %d), want (0, MAX, 1)", start, stop, step)
	}
}

func TestSliceUnpackNegativeStep(t *testing.T) {
	s := NewSlice(None(), None(), NewInt(-1))
	start, stop, step, err := s.Unpack()
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if step != -1 || start != ssizeMax || stop != ssizeMin {
		t.Fatalf("backward defaults = (%d, %d, %d)", start, stop, step)
	}
}

func TestSliceUnpackZeroStepRejected(t *testing.T) {
	s := NewSlice(None(), None(), NewInt(0))
	if _, _, _, err := s.Unpack(); err == nil {
		t.Fatal("step=0 must error")
	}
}

func TestSliceAdjustIndicesForward(t *testing.T) {
	cases := []struct {
		start, stop, step, length, ws, we, wlen int
	}{
		{0, 5, 1, 10, 0, 5, 5},
		{-3, -1, 1, 10, 7, 9, 2},
		{0, 100, 1, 10, 0, 10, 10},
		{-100, 100, 1, 10, 0, 10, 10},
		{2, 2, 1, 10, 2, 2, 0},
	}
	for _, c := range cases {
		start, stop := c.start, c.stop
		got := AdjustIndices(c.length, &start, &stop, c.step)
		if start != c.ws || stop != c.we || got != c.wlen {
			t.Errorf("Adjust(%d, %d, %d, len=%d) = (%d,%d,%d), want (%d,%d,%d)",
				c.start, c.stop, c.step, c.length, start, stop, got,
				c.ws, c.we, c.wlen)
		}
	}
}

func TestSliceAdjustIndicesBackward(t *testing.T) {
	// a[::-1] on len=5 lowers to start=MAX, stop=MIN, step=-1 after
	// Unpack. AdjustIndices clamps to (4, -1, 5) — the full reversed
	// range.
	start, stop := ssizeMax, ssizeMin
	got := AdjustIndices(5, &start, &stop, -1)
	if start != 4 || stop != -1 || got != 5 {
		t.Fatalf("backward defaults clamp = (%d,%d,%d)", start, stop, got)
	}
}

func TestSliceIndices(t *testing.T) {
	s := NewSlice(NewInt(1), NewInt(8), NewInt(2))
	got, err := s.Indices(10)
	if err != nil {
		t.Fatalf("Indices: %v", err)
	}
	tup, ok := got.(*Tuple)
	if !ok || tup.Len() != 3 {
		t.Fatalf("expected 3-tuple, got %v", got)
	}
	want := []int64{1, 8, 2}
	for i, w := range want {
		v, _ := tup.Item(i).(*Int).Int64()
		if v != w {
			t.Errorf("indices[%d] = %d, want %d", i, v, w)
		}
	}
}

func TestSliceIndicesNegativeLength(t *testing.T) {
	s := NewSlice(None(), None(), None())
	if _, err := s.Indices(-1); err == nil {
		t.Fatal("negative length must error")
	}
}
