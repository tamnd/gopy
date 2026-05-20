package objects

import "testing"

// BenchmarkStrSplitWhitespaceASCII gauges the byte-indexed sep=None
// fast path against the rune-walk slow path. Both arms split the same
// 11-word ASCII line ("the quick brown fox jumps over the lazy dog
// runs fast") which is representative of pyperformance text corpora.
//
// CPython: Objects/stringlib/split.h:53 stringlib_split_whitespace
func BenchmarkStrSplitWhitespaceASCII(b *testing.B) {
	s := "the quick brown fox jumps over the lazy dog runs fast"
	b.ReportAllocs()
	for b.Loop() {
		_ = strSplitWhitespaceASCII(s, -1, false)
	}
}

func BenchmarkStrSplitWhitespaceRunes(b *testing.B) {
	s := "the quick brown fox jumps over the lazy dog runs fast"
	b.ReportAllocs()
	for b.Loop() {
		_ = strSplitWhitespaceRunes(s, -1, false)
	}
}

func BenchmarkStrRSplitWhitespaceASCII(b *testing.B) {
	s := "the quick brown fox jumps over the lazy dog runs fast"
	b.ReportAllocs()
	for b.Loop() {
		_ = strSplitWhitespaceASCII(s, -1, true)
	}
}

func BenchmarkStrRSplitWhitespaceRunes(b *testing.B) {
	s := "the quick brown fox jumps over the lazy dog runs fast"
	b.ReportAllocs()
	for b.Loop() {
		_ = strSplitWhitespaceRunes(s, -1, true)
	}
}
