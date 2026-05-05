package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("HashFile = %s, want %s", got, want)
	}
}

func TestExtractMarker(t *testing.T) {
	src := []byte("// header\n" + MarkerLine("abc123") + "package x\n")
	if got := ExtractMarker(src); got != "abc123" {
		t.Errorf("ExtractMarker = %q, want abc123", got)
	}
	if got := ExtractMarker([]byte("no marker here")); got != "" {
		t.Errorf("ExtractMarker on plain file = %q, want empty", got)
	}
}

func TestCheckDriftMatch(t *testing.T) {
	dir := t.TempDir()
	bytecodes := filepath.Join(dir, "bytecodes.c")
	if err := os.WriteFile(bytecodes, []byte("// fake bytecodes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := HashFile(bytecodes)
	if err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(dir, "gen.go")
	if err := os.WriteFile(gen, []byte(MarkerLine(hash)+"package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckDrift(bytecodes, []string{gen}); err != nil {
		t.Errorf("CheckDrift on matching artefacts: %v", err)
	}
}

func TestCheckDriftMismatch(t *testing.T) {
	dir := t.TempDir()
	bytecodes := filepath.Join(dir, "bytecodes.c")
	if err := os.WriteFile(bytecodes, []byte("// new bytecodes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(dir, "gen.go")
	if err := os.WriteFile(gen, []byte(MarkerLine("staleeeeeee")+"package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckDrift(bytecodes, []string{gen})
	if err == nil {
		t.Fatal("CheckDrift should fail on mismatch")
	}
	if !strings.Contains(err.Error(), "drift") {
		t.Errorf("err = %v, want mention of drift", err)
	}
}

func TestCheckDriftMissingMarker(t *testing.T) {
	dir := t.TempDir()
	bytecodes := filepath.Join(dir, "bytecodes.c")
	if err := os.WriteFile(bytecodes, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(dir, "gen.go")
	if err := os.WriteFile(gen, []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckDrift(bytecodes, []string{gen})
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Errorf("err = %v, want missing-marker error", err)
	}
}
