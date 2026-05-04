package hash

import (
	"bytes"
	"errors"
	"testing"
)

func TestInitRandom(t *testing.T) {
	Reset()
	t.Setenv("PYTHONHASHSEED", "")
	m, err := Init(nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m != SecretRandom {
		t.Fatalf("mode = %v, want SecretRandom", m)
	}
	if Secret == [SecretSize]byte{} {
		t.Fatal("Secret is all zero after random Init")
	}
}

func TestInitZeroed(t *testing.T) {
	Reset()
	m, err := Init(&Config{UseHashSeed: true, HashSeed: 0})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m != SecretZeroed {
		t.Fatalf("mode = %v, want SecretZeroed", m)
	}
	if Secret != [SecretSize]byte{} {
		t.Fatal("Secret is non-zero after seed 0")
	}
}

func TestInitSeededDeterministic(t *testing.T) {
	Reset()
	if _, err := Init(&Config{UseHashSeed: true, HashSeed: 0xdeadbeef}); err != nil {
		t.Fatal(err)
	}
	first := Secret
	Reset()
	if _, err := Init(&Config{UseHashSeed: true, HashSeed: 0xdeadbeef}); err != nil {
		t.Fatal(err)
	}
	if first != Secret {
		t.Fatal("seeded Init not deterministic")
	}
}

func TestInitOnce(t *testing.T) {
	Reset()
	if _, err := Init(&Config{UseHashSeed: true, HashSeed: 1}); err != nil {
		t.Fatal(err)
	}
	first := Secret
	if _, err := Init(&Config{UseHashSeed: true, HashSeed: 2}); err != nil {
		t.Fatal(err)
	}
	if first != Secret {
		t.Fatal("second Init mutated Secret")
	}
}

func TestInitFromEnvInvalid(t *testing.T) {
	Reset()
	t.Setenv("PYTHONHASHSEED", "not-a-number")
	_, err := Init(nil)
	if !errors.Is(err, ErrInvalidSeed) {
		t.Fatalf("Init returned %v, want ErrInvalidSeed", err)
	}
}

func TestInitFromEnvRandom(t *testing.T) {
	Reset()
	t.Setenv("PYTHONHASHSEED", "random")
	m, err := Init(nil)
	if err != nil {
		t.Fatal(err)
	}
	if m != SecretRandom {
		t.Fatalf("mode = %v, want SecretRandom", m)
	}
}

func TestInitFromEnvNumeric(t *testing.T) {
	Reset()
	t.Setenv("PYTHONHASHSEED", "42")
	m, err := Init(nil)
	if err != nil {
		t.Fatal(err)
	}
	if m != SecretSeeded {
		t.Fatalf("mode = %v, want SecretSeeded", m)
	}
	expected := make([]byte, SecretSize)
	lcgURandom(42, expected)
	if !bytes.Equal(Secret[:], expected) {
		t.Fatal("Secret does not match lcgURandom(42)")
	}
}

// TestLCGFirstBytes locks the LCG output so any future refactor that
// changes its arithmetic will fail loudly. The reference is the
// first 16 bytes produced by cpython/Python/bootstrap_hash.c's
// lcg_urandom for seed 1, computed once with the Go implementation
// and validated against the C math by hand.
func TestLCGFirstBytes(t *testing.T) {
	out := make([]byte, 16)
	lcgURandom(1, out)
	// Computed by following the C arithmetic by hand for seed 1:
	//   x_{n+1} = x_n * 214013 + 2531011 (mod 2^32)
	//   byte_n  = (x_{n+1} >> 16) & 0xff
	want := []byte{0x29, 0x23, 0xbe, 0x84, 0xe1, 0x6c, 0xd6, 0xae,
		0x52, 0x90, 0x49, 0xf1, 0xf1, 0xbb, 0xe9, 0xeb}
	if !bytes.Equal(out, want) {
		t.Fatalf("lcg(1) = % x\nwant         % x", out, want)
	}
}
