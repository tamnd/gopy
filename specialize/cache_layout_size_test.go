package specialize

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

// TestCacheLayoutSizeParity is the spec 1714 phase 3 gate. It
// reads the generated specialize/cache_layouts_gen.go (still
// behind //go:build ignore during the migration window) and
// asserts that every struct's `Codeunits()` matches the cache
// width that the metadata generator emits for the matching
// opcode in compile/opcode_metadata_gen.go.
//
// The mapping from cache struct to opcode lives in CPython's
// pycore_code.h via INLINE_CACHE_ENTRIES_<OP> macros. We
// hard-code it here since there are only a dozen rows. When a
// new cache type lands, this table grows alongside the spec.
func TestCacheLayoutSizeParity(t *testing.T) {
	dir := pkgDir(t)
	repo := filepath.Dir(dir)

	sizes := parseCodeunits(t, filepath.Join(dir, "cache_layouts_gen.go"))
	caches := parseOpcodeCaches(t, filepath.Join(repo, "compile", "opcode_metadata_gen.go"))

	cases := []struct {
		cacheType string
		opcode    string
	}{
		{"LoadGlobalCache", "LOAD_GLOBAL"},
		{"BinaryOpCache", "BINARY_OP"},
		{"UnpackSequenceCache", "UNPACK_SEQUENCE"},
		{"CompareOpCache", "COMPARE_OP"},
		{"SuperAttrCache", "LOAD_SUPER_ATTR"},
		{"AttrCache", "STORE_ATTR"},
		{"LoadMethodCache", "LOAD_ATTR"},
		{"CallCache", "CALL"},
		{"CallCache", "CALL_KW"},
		{"StoreSubscrCache", "STORE_SUBSCR"},
		{"ForIterCache", "FOR_ITER"},
		{"SendCache", "SEND"},
		{"ToBoolCache", "TO_BOOL"},
		{"ContainsOpCache", "CONTAINS_OP"},
	}

	for _, c := range cases {
		got, ok := sizes[c.cacheType]
		if !ok {
			t.Errorf("cache_layouts_gen.go missing %s", c.cacheType)
			continue
		}
		want, ok := caches[c.opcode]
		if !ok {
			t.Errorf("opcode_metadata_gen.go missing %s cache width", c.opcode)
			continue
		}
		if got != want {
			t.Errorf("%s codeunits=%d, opcodeCachesGen[%s]=%d", c.cacheType, got, c.opcode, want)
		}
	}
}

func pkgDir(t *testing.T) string {
	t.Helper()
	_, this, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(this)
}

var codeunitsRe = regexp.MustCompile(`^func \((\w+)\) Codeunits\(\) int \{ return (\d+) \}`)

func parseCodeunits(t *testing.T, path string) map[string]int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	out := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := codeunitsRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[2])
		out[m[1]] = v
	}
	if len(out) == 0 {
		t.Fatalf("%s: no Codeunits() methods found", path)
	}
	return out
}

var cacheRowRe = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]*)\s*:\s*(\d+)\s*,\s*$`)

func parseOpcodeCaches(t *testing.T, path string) map[string]int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	out := map[string]int{}
	sc := bufio.NewScanner(f)
	inBlock := false
	for sc.Scan() {
		line := sc.Text()
		if !inBlock {
			if line == "var opcodeCachesGen = [256]uint8{" {
				inBlock = true
			}
			continue
		}
		if line == "}" {
			break
		}
		m := cacheRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[2])
		out[m[1]] = v
	}
	if len(out) == 0 {
		t.Fatalf("%s: no opcodeCachesGen rows", path)
	}
	return out
}
