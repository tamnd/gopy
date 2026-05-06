package objects

import (
	"testing"

	"github.com/tamnd/gopy/hash"
)

func TestHashStringMatchesHashBuffer(t *testing.T) {
	if HashString("hello") != hash.Buffer([]byte("hello")) {
		t.Fatalf("HashString diverges from hash.Buffer")
	}
	if HashBytes([]byte("hi")) != hash.Buffer([]byte("hi")) {
		t.Fatalf("HashBytes diverges from hash.Buffer")
	}
}

func TestStrStubHashRoutesThroughSipHash(t *testing.T) {
	s := NewStr("widget")
	got, err := Hash(s)
	if err != nil {
		t.Fatalf("Hash(str): %v", err)
	}
	if got != HashString("widget") {
		t.Fatalf("strStub hash %d, want %d", got, HashString("widget"))
	}
}

func TestEmptyStringHash(t *testing.T) {
	if got := HashString(""); got != 0 {
		t.Fatalf("empty string hash = %d, want 0", got)
	}
}
