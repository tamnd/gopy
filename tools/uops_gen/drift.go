// Drift check: every generated artifact carries the SHA256 of the
// source header it was emitted from. The `-check-drift` mode of
// uops_gen recomputes the hash against the live cpython-3.14 checkout
// and exits non-zero on mismatch, so CI can pin the generator's input
// the same way bytecodes_gen pins bytecodes.c.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// driftMarker is the prefix the generated files use to carry the
// header sha256. Easy to grep for when reading a generated file.
const driftMarker = "// uop header sha256: "

// HashFile returns the lowercase hex SHA256 of the file at path.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash header: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// MarkerLine returns the comment line that should be emitted at the
// top of every generated file.
func MarkerLine(hash string) string {
	return driftMarker + hash + "\n"
}

// ExtractMarker pulls the recorded hash out of generated source.
// Returns "" if no marker is present.
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
