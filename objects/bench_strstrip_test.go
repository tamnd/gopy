package objects

import "testing"

// BenchmarkStrStripASCII gauges the byte-indexed strip fast path. The
// rune-walk arm calls stripRunesWhitespace by forcing a non-ASCII byte
// into the prefix so isASCII returns false. Both arms trim the same
// payload across both ends, which is the shape pyperformance hits in
// the `int_filter` / `2to3` lines.
//
// CPython: Objects/stringlib/transmogrify.h:80 do_strip
func BenchmarkStrStripASCII(b *testing.B) {
	s := " \t  hello world  \t "
	b.ReportAllocs()
	for b.Loop() {
		_ = stripASCIIWhitespace(s, true, true)
	}
}

func BenchmarkStrStripRunes(b *testing.B) {
	s := " \t  hello world  \t "
	b.ReportAllocs()
	for b.Loop() {
		_ = stripRunesWhitespace(s, true, true)
	}
}

func BenchmarkStrLStripASCII(b *testing.B) {
	s := " \t  hello world  \t "
	b.ReportAllocs()
	for b.Loop() {
		_ = stripASCIIWhitespace(s, true, false)
	}
}

func BenchmarkStrRStripASCII(b *testing.B) {
	s := " \t  hello world  \t "
	b.ReportAllocs()
	for b.Loop() {
		_ = stripASCIIWhitespace(s, false, true)
	}
}
