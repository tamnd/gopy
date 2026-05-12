package _uuid

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestGenerateTimeSafe(t *testing.T) {
	result, err := generateTimeSafe(nil, nil)
	if err != nil {
		t.Fatalf("generateTimeSafe returned error: %v", err)
	}

	tup, ok := result.(*objects.Tuple)
	if !ok {
		t.Fatalf("expected *objects.Tuple, got %T", result)
	}
	if tup.Len() != 2 {
		t.Fatalf("expected tuple of length 2, got %d", tup.Len())
	}

	// First element: bytes of length 16.
	b, ok := tup.Item(0).(*objects.Bytes)
	if !ok {
		t.Fatalf("expected *objects.Bytes as first element, got %T", tup.Item(0))
	}
	if len(b.Bytes()) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(b.Bytes()))
	}

	// Second element: safety int == UUID_SAFE_THREAD (2).
	safetyObj, ok := tup.Item(1).(*objects.Int)
	if !ok {
		t.Fatalf("expected *objects.Int as second element, got %T", tup.Item(1))
	}
	safetyVal, _ := safetyObj.Int64()
	if safetyVal != uuidSafeThread {
		t.Fatalf("expected safety=%d (UUID_SAFE_THREAD), got %d", uuidSafeThread, safetyVal)
	}
}

func TestGenerateTimeSafeTwoDifferent(t *testing.T) {
	r1, err := generateTimeSafe(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := generateTimeSafe(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b1 := r1.(*objects.Tuple).Item(0).(*objects.Bytes).Bytes()
	b2 := r2.(*objects.Tuple).Item(0).(*objects.Bytes).Bytes()
	// With 128 bits of entropy the probability of collision is negligible.
	same := true
	for i := range b1 {
		if b1[i] != b2[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("two calls to generateTimeSafe returned identical bytes")
	}
}

func TestBuildModule(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule returned error: %v", err)
	}
	if m == nil {
		t.Fatal("buildModule returned nil module")
	}

	d := m.Dict()
	keys := []string{
		"generate_time_safe",
		"UUID_SAFETY_UNKNOWN",
		"UUID_SAFE_MULTIPROCESSING",
		"UUID_SAFE_THREAD",
		"has_uuid_generate_time_safe",
	}
	for _, k := range keys {
		v, err := d.GetItem(objects.NewStr(k))
		if err != nil || v == nil {
			t.Errorf("module missing key %q", k)
		}
	}
}
