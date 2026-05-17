package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDisParity drives spec 1713's .pyc byte-equality goal one source
// file at a time. For each fixture under disdata/, the wrapper script
// reads the source, compiles it, and prints dis.dis() output; CPython
// 3.14 and gopy must emit byte-identical disassemblies.
//
// As gopy's codegen closes the remaining gaps (LOAD_SMALL_INT, RETURN
// line-number sentinels, etc.) more fixtures land here.
func TestDisParity(t *testing.T) {
	cpython := FindCPython(t)
	if cpython == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopyBin := BuildGopy(t)

	entries, err := os.ReadDir("disdata")
	if err != nil {
		t.Fatalf("read disdata: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".py" {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join("disdata", name))
			if err != nil {
				t.Fatalf("abs: %v", err)
			}
			script := `import dis, sys
src = open(sys.argv[1]).read()
dis.dis(compile(src, sys.argv[1], 'exec'))
`
			Compare(t, cpython, gopyBin, script, path)
		})
	}
}
