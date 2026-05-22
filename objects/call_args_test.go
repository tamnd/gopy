package objects

import (
	"strings"
	"testing"
)

func TestUnpackKwargsRoundTrip(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	_ = d.SetItem(NewStr("b"), NewInt(2))
	kwnames, vals, err := UnpackKwargs(d)
	if err != nil {
		t.Fatal(err)
	}
	if kwnames.Len() != 2 || len(vals) != 2 {
		t.Fatalf("got kwnames=%d vals=%d, want 2/2", kwnames.Len(), len(vals))
	}
	for i := 0; i < kwnames.Len(); i++ {
		k, _ := Str(kwnames.Item(i))
		v, _ := vals[i].(*Int).Int64()
		switch k {
		case "a":
			if v != 1 {
				t.Errorf("a = %d, want 1", v)
			}
		case "b":
			if v != 2 {
				t.Errorf("b = %d, want 2", v)
			}
		default:
			t.Errorf("unexpected key %q", k)
		}
	}
}

func TestUnpackKwargsEmpty(t *testing.T) {
	if kw, vals, err := UnpackKwargs(nil); kw != nil || vals != nil || err != nil {
		t.Errorf("nil kwargs should return nil/nil/nil; got %v/%v/%v", kw, vals, err)
	}
	if kw, vals, err := UnpackKwargs(NewDict()); kw != nil || vals != nil || err != nil {
		t.Errorf("empty kwargs should return nil/nil/nil; got %v/%v/%v", kw, vals, err)
	}
}

func TestUnpackKwargsRejectsNonStringKey(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewInt(1), NewInt(2))
	_, _, err := UnpackKwargs(d)
	if err == nil {
		t.Fatal("expected TypeError for int key")
	}
	if !strings.Contains(err.Error(), "keywords must be strings") {
		t.Errorf("error = %q, want 'keywords must be strings' substring", err)
	}
}

func TestCoerceStarArgsTuplePassThrough(t *testing.T) {
	in := NewTuple([]Object{NewInt(1), NewInt(2)})
	out, err := CoerceStarArgs(in)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Error("Tuple input should pass through identically")
	}
}

func TestCoerceStarArgsList(t *testing.T) {
	in := NewList([]Object{NewInt(7), NewInt(8), NewInt(9)})
	out, err := CoerceStarArgs(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Len() != 3 {
		t.Fatalf("len = %d, want 3", out.Len())
	}
	for i, want := range []int64{7, 8, 9} {
		if got, _ := out.Item(i).(*Int).Int64(); got != want {
			t.Errorf("item[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestCoerceStarArgsRejectsNonIterable(t *testing.T) {
	if _, err := CoerceStarArgs(NewInt(7)); err == nil {
		t.Error("int should not be iterable")
	}
}

func TestCoerceStarKwargsDictPassThrough(t *testing.T) {
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(1))
	out, err := CoerceStarKwargs(d)
	if err != nil {
		t.Fatal(err)
	}
	if out != d {
		t.Error("Dict input should pass through identically")
	}
}

func TestCoerceStarKwargsRejectsNonMapping(t *testing.T) {
	if _, err := CoerceStarKwargs(NewInt(7)); err == nil {
		t.Error("int should not be a mapping")
	}
}

func TestMissingArgumentsErrorOneName(t *testing.T) {
	err := MissingArgumentsError("f", "positional", []string{"x"})
	want := "TypeError: f() missing 1 required positional argument: 'x'"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestMissingArgumentsErrorTwoNames(t *testing.T) {
	err := MissingArgumentsError("f", "positional", []string{"x", "y"})
	want := "TypeError: f() missing 2 required positional arguments: 'x' and 'y'"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestMissingArgumentsErrorOxfordComma(t *testing.T) {
	err := MissingArgumentsError("f", "keyword-only", []string{"x", "y", "z"})
	want := "TypeError: f() missing 3 required keyword-only arguments: 'x', 'y', and 'z'"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestMissingArgumentsErrorEmpty(t *testing.T) {
	if err := MissingArgumentsError("f", "positional", nil); err != nil {
		t.Errorf("zero names should be no error, got %v", err)
	}
}

func TestTooManyPositionalErrorWithDefaults(t *testing.T) {
	err := TooManyPositionalError("f", 5, 1, 3, 0)
	want := "TypeError: f() takes from 1 to 3 positional arguments but 5 were given"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestTooManyPositionalErrorExactCount(t *testing.T) {
	err := TooManyPositionalError("f", 4, 2, 2, 0)
	want := "TypeError: f() takes 2 positional arguments but 4 were given"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestTooManyPositionalErrorOneArgWasGiven(t *testing.T) {
	err := TooManyPositionalError("f", 1, 0, 0, 0)
	want := "TypeError: f() takes 0 positional arguments but 1 was given"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestTooManyPositionalErrorWithKwonly(t *testing.T) {
	err := TooManyPositionalError("f", 3, 1, 1, 2)
	want := "TypeError: f() takes 1 positional argument but 3 positional arguments (and 2 keyword-only arguments) were given"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestUnexpectedKeywordError(t *testing.T) {
	err := UnexpectedKeywordError("f", "z")
	want := "TypeError: f() got an unexpected keyword argument 'z'"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestMultipleValuesForArgumentError(t *testing.T) {
	err := MultipleValuesForArgumentError("f", "x")
	want := "TypeError: f() got multiple values for argument 'x'"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestPositionalOnlyAsKeywordError(t *testing.T) {
	err := PositionalOnlyAsKeywordError("f", []string{"a", "b"})
	want := "TypeError: f() got some positional-only arguments passed as keyword arguments: 'a, b'"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
	if PositionalOnlyAsKeywordError("f", nil) != nil {
		t.Error("empty names should be no error")
	}
}
