package string

import (
	"strings"
	"testing"
)

func TestConcatStrings(t *testing.T) {
	parts := []Result{{Text: "ab"}, {Text: "cd"}, {Text: "ef"}}
	r, err := Concat(parts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if r.Text != "abcdef" {
		t.Errorf("Text = %q", r.Text)
	}
	if r.IsBytes {
		t.Errorf("IsBytes = true")
	}
}

func TestConcatBytes(t *testing.T) {
	parts := []Result{
		{Bytes: []byte("ab"), IsBytes: true},
		{Bytes: []byte("cd"), IsBytes: true},
	}
	r, err := Concat(parts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(r.Bytes) != "abcd" || !r.IsBytes {
		t.Errorf("got %+v", r)
	}
}

func TestConcatRejectsMixed(t *testing.T) {
	parts := []Result{
		{Text: "ab"},
		{Bytes: []byte("cd"), IsBytes: true},
	}
	_, err := Concat(parts)
	if err == nil || !strings.Contains(err.Error(), "cannot mix bytes and nonbytes literals") {
		t.Errorf("err = %v, want mix error", err)
	}
}

func TestConcatRejectsFTMix(t *testing.T) {
	parts := []Result{
		{Text: "f", IsFString: true},
		{Text: "t", IsTString: true},
	}
	_, err := Concat(parts)
	if err == nil || !strings.Contains(err.Error(), "cannot mix t-string literals with f-string literals") {
		t.Errorf("err = %v, want f/t mix error", err)
	}
}

func TestConcatPropagatesFlags(t *testing.T) {
	parts := []Result{{Text: "a"}, {Text: "b", IsFString: true}}
	r, err := Concat(parts)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !r.IsFString {
		t.Errorf("IsFString lost on fold: %+v", r)
	}
}

func TestConcatSingleton(t *testing.T) {
	r, err := Concat([]Result{{Text: "only"}})
	if err != nil || r.Text != "only" {
		t.Errorf("singleton fold dropped data: %+v err=%v", r, err)
	}
}
