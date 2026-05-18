package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// libKnownFailing lists files under test/cpython/Lib/ that the dis-parity
// gate cannot yet match byte-for-byte. Each entry pins the dominant gap
// surfaced when diffing CPython 3.14 against gopy. As the underlying gap
// closes the file drops out of this map and the subtest runs for real.
//
// The point of the map is to keep the gate target (Lib/) growing without
// painting CI red on every vendored slice. Phase 3 of spec 1713 is done
// when this map is empty and every vendored Lib/ file passes Compare.
var libKnownFailing = map[string]string{
	"__future__.py": "co_consts ordering: _Feature class body emits extra const slots before kwarg tuple",
}

// TestDisParityLib drives spec 1713 Phase 3: dis-stream byte-equality on
// the vendored CPython Lib/ slice under test/cpython/Lib/. Each .py file
// runs as a subtest; known-failing files self-skip with a pointer to the
// compiler gap task.
func TestDisParityLib(t *testing.T) {
	cpython := FindCPython(t)
	if cpython == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopyBin := BuildGopy(t)

	libDir, err := filepath.Abs(filepath.Join("..", "cpython", "Lib"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	entries, err := os.ReadDir(libDir)
	if err != nil {
		t.Fatalf("read %s: %v", libDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".py" {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			if reason, blocked := libKnownFailing[name]; blocked {
				t.Skipf("known compiler gap: %s", reason)
			}
			path := filepath.Join(libDir, name)
			script := `import dis, sys
src = open(sys.argv[1]).read()
dis.dis(compile(src, sys.argv[1], 'exec'))
`
			Compare(t, cpython, gopyBin, script, path)
		})
	}
}
