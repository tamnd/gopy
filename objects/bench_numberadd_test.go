package objects

import "testing"

func BenchmarkNumberAddIntsViaProtocol(b *testing.B) {
	x := NewInt(7)
	y := NewInt(11)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = NumberAdd(x, y)
	}
}

func BenchmarkNumberMulIntsViaProtocol(b *testing.B) {
	x := NewInt(7)
	y := NewInt(11)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = NumberMultiply(x, y)
	}
}
