// Drift check: every generated artefact carries the SHA256 of the
// bytecodes.c it was emitted from. The `-check-drift` mode of
// bytecodes_gen recomputes the hash against the live cpython-3.14
// checkout and exits non-zero on mismatch, so CI can pin the
// generator's input the same way the parser pins python.gram.
//
// CPython: Tools/cases_generator/generate_cases.py header lines
// (the upstream tool stamps the bytecodes.c hash into the generated
// header preamble for the same reason).

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// driftMarker is the prefix the generated files use to carry the
// bytecodes.c hash. Easy to grep for when reading a generated file.
const driftMarker = "// bytecodes.c sha256: "

// HashFile returns the lowercase hex SHA256 of the file at path.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash bytecodes.c: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// MarkerLine returns the comment line that should be emitted at the
// top of every generated file.
func MarkerLine(hash string) string {
	return driftMarker + hash + "\n"
}

// ExtractMarker pulls the recorded hash out of generated source. The
// scan is line-oriented and stops at the first match; returns "" if no
// marker is present (e.g. a hand-written file). Designed to be cheap
// since CI runs it across every generated artefact on every build.
func ExtractMarker(src []byte) string {
	const m = driftMarker
	mlen := len(m)
	for i := 0; i+mlen < len(src); i++ {
		if string(src[i:i+mlen]) == m {
			j := i + mlen
			for j < len(src) && src[j] != '\n' && src[j] != '\r' {
				j++
			}
			return string(src[i+mlen : j])
		}
	}
	return ""
}

// CheckDrift re-hashes bytecodesPath and compares it to the marker in
// each path under generated. Returns nil on agreement; an error
// describing the first mismatch otherwise.
func CheckDrift(bytecodesPath string, generated []string) error {
	want, err := HashFile(bytecodesPath)
	if err != nil {
		return err
	}
	for _, g := range generated {
		data, err := os.ReadFile(g)
		if err != nil {
			return fmt.Errorf("read %s: %w", g, err)
		}
		got := ExtractMarker(data)
		if got == "" {
			return fmt.Errorf("%s: missing %q marker", g, driftMarker)
		}
		if got != want {
			return fmt.Errorf("%s: drift\n  recorded: %s\n  current : %s\n  rerun bytecodes_gen", g, got, want)
		}
	}
	return nil
}
