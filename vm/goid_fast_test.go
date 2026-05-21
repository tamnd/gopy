// Validates the fast goid() path matches the slow runtime.Stack-based
// reading. If the Go release moved g.goid past the scan window or
// changed the field type, this test catches it before benchmarks
// silently regress.

package vm

import (
	"sync"
	"testing"
)

// TestGoidMatchesSlowPath confirms the asm + offset-discovery path
// agrees with the slow runtime.Stack-based parser. Must hold across
// the main goroutine and a spawned one.
func TestGoidMatchesSlowPath(t *testing.T) {
	if got, want := goid(), slowGoid(); got != want {
		t.Fatalf("main goroutine: goid()=%d slowGoid()=%d", got, want)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, want := goid(), slowGoid(); got != want {
				t.Errorf("spawned goroutine: goid()=%d slowGoid()=%d", got, want)
			}
		}()
	}
	wg.Wait()
}

// TestGoidOffsetDiscovered fails loud if the init-time scan did not
// find the field; without it the fast path silently falls back and
// the bench wins disappear.
func TestGoidOffsetDiscovered(t *testing.T) {
	if goidOffset == 0 {
		t.Fatal("goidOffset = 0: discovery did not find g.goid in the scan window; fast goid is falling back to runtime.Stack")
	}
}

func BenchmarkGoid(b *testing.B) {
	var sum uint64
	for i := 0; i < b.N; i++ {
		sum += goid()
	}
	if sum == 0 {
		b.Fatal("goid loop produced 0 sum")
	}
}

func BenchmarkSlowGoid(b *testing.B) {
	var sum uint64
	for i := 0; i < b.N; i++ {
		sum += slowGoid()
	}
	if sum == 0 {
		b.Fatal("slowGoid loop produced 0 sum")
	}
}
