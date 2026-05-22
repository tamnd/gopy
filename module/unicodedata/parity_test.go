// Parity sweep that compares Go's name/lookup against a captured
// CPython dump. The dump file is generated locally via the helper in
// parity_test_helper.sh (see comments); the test silently skips
// when the file is missing so CI doesn't depend on it.

package unicodedata

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestNameLookupParitySweep(t *testing.T) {
	path := os.Getenv("UNICODEDATA_PARITY_DUMP")
	if path == "" {
		t.Skip("set UNICODEDATA_PARITY_DUMP to a CPython name dump to run")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("dump not available: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	total, mismatches := 0, 0
	for sc.Scan() {
		line := sc.Text()
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimPrefix(line[:sp], "0x"), 16, 64)
		if err != nil {
			continue
		}
		want := line[sp+1:]
		total++
		got, ok := getUCName(nil, rune(v), false)
		if !ok || got != want {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("name(%#x) = %q ok=%v; want %q", v, got, ok, want)
			}
		}
		gotCp := getCode(want)
		if gotCp != rune(v) {
			mismatches++
			if mismatches <= 10 {
				t.Errorf("lookup(%q) = %#x; want %#x", want, gotCp, v)
			}
		}
	}
	if mismatches != 0 {
		t.Logf("parity sweep: %d/%d mismatched", mismatches, total)
	} else {
		t.Logf("parity sweep: %d entries clean", total)
	}
}
