package _stat

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestSIsDir verifies that S_ISDIR returns True for S_IFDIR mode and
// False for a regular file mode.
func TestSIsDir(t *testing.T) {
	// S_IFDIR mode.
	res, err := statIsDir([]objects.Object{objects.NewInt(sDir)}, nil)
	if err != nil {
		t.Fatalf("S_ISDIR(S_IFDIR): %v", err)
	}
	b, _ := objects.IsTruthy(res)
	if !b {
		t.Error("S_ISDIR(S_IFDIR) = False, want True")
	}

	// Regular file mode should not be a directory.
	res, err = statIsDir([]objects.Object{objects.NewInt(sReg)}, nil)
	if err != nil {
		t.Fatalf("S_ISDIR(S_IFREG): %v", err)
	}
	b, _ = objects.IsTruthy(res)
	if b {
		t.Error("S_ISDIR(S_IFREG) = True, want False")
	}
}

// TestSIsReg verifies that S_ISREG returns True for a regular file.
func TestSIsReg(t *testing.T) {
	res, err := statIsReg([]objects.Object{objects.NewInt(sReg)}, nil)
	if err != nil {
		t.Fatalf("S_ISREG: %v", err)
	}
	b, _ := objects.IsTruthy(res)
	if !b {
		t.Error("S_ISREG(S_IFREG) = False, want True")
	}
}

// TestFilemodeDirRwxrXrX verifies the filemode string for a typical
// directory mode 0o40755 (drwxr-xr-x).
func TestFilemodeDirRwxrXrX(t *testing.T) {
	mode := int64(sDir | sIrwxu | sIrgrp | sIxgrp | sIroth | sIxoth) // 0o40755
	res, err := filemode([]objects.Object{objects.NewInt(mode)}, nil)
	if err != nil {
		t.Fatalf("filemode: %v", err)
	}
	s, ok := res.(interface{ GoString() string })
	_ = s
	_ = ok
	// Extract the string value.
	str, err := objects.Str(res)
	if err != nil {
		t.Fatalf("Str(filemode result): %v", err)
	}
	want := "drwxr-xr-x"
	if str != want {
		t.Errorf("filemode(0o40755) = %q, want %q", str, want)
	}
}

// TestFilemodeRegularFile verifies filemode for mode 0o100644 (-rw-r--r--).
func TestFilemodeRegularFile(t *testing.T) {
	mode := int64(sReg | sIrusr | sIwusr | sIrgrp | sIroth) // 0o100644
	res, err := filemode([]objects.Object{objects.NewInt(mode)}, nil)
	if err != nil {
		t.Fatalf("filemode: %v", err)
	}
	str, err := objects.Str(res)
	if err != nil {
		t.Fatalf("Str(filemode result): %v", err)
	}
	want := "-rw-r--r--"
	if str != want {
		t.Errorf("filemode(0o100644) = %q, want %q", str, want)
	}
}
