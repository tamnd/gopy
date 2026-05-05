// Grammar drift detection. The generator stamps the SHA256 of
// python.gram into a header comment of parser_gen.go; the drift
// check compares that recorded hash against the current grammar
// source so a stale generated file fails loudly in CI rather than
// silently parsing an old language.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
)

// hashGrammar returns the hex SHA256 of the grammar source. The
// empty string is returned for a nil/empty input so callers can
// check for "hash known" cheaply.
func hashGrammar(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var gramHashLine = regexp.MustCompile(`(?m)^// gram-sha256: ([0-9a-f]+)\s*$`)

// recordedGrammarHash extracts the gram-sha256 line written into
// the generated file's preamble. Returns the empty string if the
// header is missing or malformed.
func recordedGrammarHash(generated []byte) string {
	m := gramHashLine.FindSubmatch(generated)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// CheckGrammarDrift compares the SHA256 of grammarPath against the
// hash recorded in generatedPath. It returns nil when they match,
// an error when they differ, and a wrapped os error when either
// file cannot be read.
func CheckGrammarDrift(grammarPath, generatedPath string) error {
	gram, err := os.ReadFile(grammarPath)
	if err != nil {
		return fmt.Errorf("read grammar: %w", err)
	}
	gen, err := os.ReadFile(generatedPath)
	if err != nil {
		return fmt.Errorf("read generated: %w", err)
	}
	want := hashGrammar(gram)
	got := recordedGrammarHash(gen)
	if got == "" {
		return fmt.Errorf("generated file has no gram-sha256 header; rerun parser_gen")
	}
	if got != want {
		return fmt.Errorf("grammar drift: %s recorded %s, current is %s; rerun parser_gen", generatedPath, got[:12], want[:12])
	}
	return nil
}
