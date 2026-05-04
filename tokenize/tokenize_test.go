package tokenize

import (
	"errors"
	"io"
	"testing"
)

// TestSkeletonReturnsEOF: the v0.5 skeleton always reports io.EOF.
// This test pins the contract so consumers can program against the
// stable surface today.
func TestSkeletonReturnsEOF(t *testing.T) {
	it := New("x = 1", false)
	_, err := it.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("Next() error = %v, want io.EOF", err)
	}
}

func TestNewReadlineSkeleton(t *testing.T) {
	it := NewReadline(func() (string, error) { return "", io.EOF }, true)
	if it == nil {
		t.Fatal("NewReadline returned nil")
	}
	_, err := it.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("Next() error = %v, want io.EOF", err)
	}
}
