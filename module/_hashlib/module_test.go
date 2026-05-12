package _hashlib

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestNewMD5KnownDigest creates an md5 hash via new(), feeds a known
// string, and checks hexdigest against the well-known value.
func TestNewMD5KnownDigest(t *testing.T) {
	// md5("") = d41d8cd98f00b204e9800998ecf8427e
	h, err := hashlibNew([]objects.Object{objects.NewStr("md5")}, nil)
	if err != nil {
		t.Fatalf("new(md5): %v", err)
	}
	got, err := hashHexdigest([]objects.Object{h}, nil)
	if err != nil {
		t.Fatalf("hexdigest: %v", err)
	}
	want := "d41d8cd98f00b204e9800998ecf8427e"
	if s, _ := objects.Str(got); s != want {
		t.Errorf("md5('') = %q, want %q", s, want)
	}
}

// TestNewSHA256KnownDigest checks sha256 against the RFC 6234 test vector
// for the empty string.
func TestNewSHA256KnownDigest(t *testing.T) {
	// sha256("abc") per FIPS 180-4 test vector.
	h, err := hashlibNew([]objects.Object{objects.NewStr("sha256")}, nil)
	if err != nil {
		t.Fatalf("new(sha256): %v", err)
	}
	_, err = hashUpdate([]objects.Object{h, objects.NewBytes([]byte("abc"))}, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := hashHexdigest([]objects.Object{h}, nil)
	if err != nil {
		t.Fatalf("hexdigest: %v", err)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if s, _ := objects.Str(got); s != want {
		t.Errorf("sha256('abc') = %q, want %q", s, want)
	}
}

// TestDigestReturnsBytesObject verifies that digest() returns a *objects.Bytes
// whose length equals the algorithm's digest_size.
func TestDigestReturnsBytesObject(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{"md5", 16},
		{"sha1", 20},
		{"sha256", 32},
		{"sha512", 64},
	} {
		h, err := hashlibNew([]objects.Object{objects.NewStr(tc.name)}, nil)
		if err != nil {
			t.Fatalf("new(%s): %v", tc.name, err)
		}
		raw, err := hashDigest([]objects.Object{h}, nil)
		if err != nil {
			t.Fatalf("%s digest(): %v", tc.name, err)
		}
		b, ok := raw.(*objects.Bytes)
		if !ok {
			t.Fatalf("%s digest() returned %T, want *objects.Bytes", tc.name, raw)
		}
		if b.Len() != tc.size {
			t.Errorf("%s digest() len=%d, want %d", tc.name, b.Len(), tc.size)
		}
	}
}

// TestCopyPreservesState checks that copy() gives a snapshot: feeding
// more data into the original does not affect the copy.
func TestCopyPreservesState(t *testing.T) {
	h, err := hashlibNew([]objects.Object{objects.NewStr("sha256")}, nil)
	if err != nil {
		t.Fatalf("new(sha256): %v", err)
	}
	_, err = hashUpdate([]objects.Object{h, objects.NewBytes([]byte("hello"))}, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	copyObj, err := hashCopy([]objects.Object{h}, nil)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	// Feed more data into original.
	_, err = hashUpdate([]objects.Object{h, objects.NewBytes([]byte(" world"))}, nil)
	if err != nil {
		t.Fatalf("update after copy: %v", err)
	}
	// The copy should still reflect "hello" only.
	origHex, _ := hashHexdigest([]objects.Object{h}, nil)
	copyHex, _ := hashHexdigest([]objects.Object{copyObj}, nil)
	origStr, _ := objects.Str(origHex)
	copyStr, _ := objects.Str(copyHex)
	if origStr == copyStr {
		t.Errorf("copy and original share state: both = %q", origStr)
	}
}

// TestAttributesDigestSizeAndBlockSize reads digest_size and block_size
// from a sha256 hash object through the attribute descriptor.
func TestAttributesDigestSizeAndBlockSize(t *testing.T) {
	h, err := hashlibNew([]objects.Object{objects.NewStr("sha256")}, nil)
	if err != nil {
		t.Fatalf("new(sha256): %v", err)
	}
	ho := h.(*hashObj)
	if ho.digestSize != 32 {
		t.Errorf("sha256 digest_size = %d, want 32", ho.digestSize)
	}
	if ho.blockSize != 64 {
		t.Errorf("sha256 block_size = %d, want 64", ho.blockSize)
	}
}

// TestOpensslMD5Shortcut checks that openssl_md5() produces the same
// digest as new("md5").
func TestOpensslMD5Shortcut(t *testing.T) {
	data := []byte("hello")
	h1, _ := hashlibNew([]objects.Object{objects.NewStr("md5"), objects.NewBytes(data)}, nil)
	h2, _ := opensslMD5([]objects.Object{objects.NewBytes(data)}, nil)
	hex1, _ := hashHexdigest([]objects.Object{h1}, nil)
	hex2, _ := hashHexdigest([]objects.Object{h2}, nil)
	s1, _ := objects.Str(hex1)
	s2, _ := objects.Str(hex2)
	if s1 != s2 {
		t.Errorf("new(md5) hex=%q, openssl_md5 hex=%q; should be equal", s1, s2)
	}
}

// TestUnsupportedAlgorithmError confirms that new() returns an error
// for unknown algorithm names.
func TestUnsupportedAlgorithmError(t *testing.T) {
	_, err := hashlibNew([]objects.Object{objects.NewStr("not_a_hash")}, nil)
	if err == nil {
		t.Fatal("expected error for unknown algorithm, got nil")
	}
}

// TestAlgorithmsGuaranteedInModule checks that buildModule exports the
// algorithms_guaranteed frozenset containing at least the core set.
func TestAlgorithmsGuaranteedInModule(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	d := m.Dict()
	val, err := d.GetItem(objects.NewStr("algorithms_guaranteed"))
	if err != nil {
		t.Fatal("algorithms_guaranteed not in module dict")
	}
	fs, ok := val.(*objects.Set)
	if !ok {
		t.Fatalf("algorithms_guaranteed is %T, want *objects.Set (frozenset)", val)
	}
	required := []string{"md5", "sha1", "sha256", "sha512"}
	for _, name := range required {
		ok2, err2 := fs.Contains(objects.NewStr(name))
		if err2 != nil {
			t.Errorf("algorithms_guaranteed Contains(%q): %v", name, err2)
			continue
		}
		if !ok2 {
			t.Errorf("algorithms_guaranteed missing %q", name)
		}
	}
}
