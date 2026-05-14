// End-to-end import gate for the traceback stdlib module.
//
// CPython: Lib/traceback.py
package stdlibinit

import "testing"

// TestImportTraceback verifies that `import traceback` succeeds
// end-to-end through the real PathFinder + VM pipeline.
func TestImportTraceback(t *testing.T) {
	_, err := importStdlib(t, "traceback")
	if err != nil {
		skipIfMissingDep(t, err, "traceback")
		t.Fatalf("import traceback: %v", err)
	}
}
