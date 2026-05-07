package objects

import (
	"strings"
	"testing"
)

func TestCodeReprWithFilename(t *testing.T) {
	c := NewCode()
	c.Name = "f"
	c.Filename = "/tmp/x.py"
	c.Firstlineno = 7
	got, err := Repr(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "<code object f at ") {
		t.Errorf("repr prefix wrong: %q", got)
	}
	if !strings.Contains(got, `file "/tmp/x.py"`) {
		t.Errorf("repr filename missing: %q", got)
	}
	if !strings.HasSuffix(got, ", line 7>") {
		t.Errorf("repr suffix wrong: %q", got)
	}
}

func TestCodeReprWithoutFilename(t *testing.T) {
	c := NewCode()
	c.Name = "f"
	got, err := Repr(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "file ???") {
		t.Errorf("missing-filename marker absent: %q", got)
	}
	if !strings.HasSuffix(got, "line -1>") {
		t.Errorf("zero firstlineno should render as -1: %q", got)
	}
}

func TestCodeHashEqualForEqualCode(t *testing.T) {
	build := func() *Code {
		c := NewCode()
		c.Name = "same"
		c.Argcount = 2
		c.Code = []byte{1, 2, 3}
		c.Names = []string{"x", "y"}
		c.Linetable = []byte{0x80, 0x57}
		return c
	}
	h1, err := Hash(build())
	if err != nil {
		t.Fatal(err)
	}
	h2, err := Hash(build())
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("equal code objects hashed to different values: %d vs %d", h1, h2)
	}
}

func TestCodeHashDiffersWhenNameChanges(t *testing.T) {
	a := NewCode()
	a.Name = "f"
	b := NewCode()
	b.Name = "g"
	ha, _ := Hash(a)
	hb, _ := Hash(b)
	if ha == hb {
		t.Error("different names should hash differently (probabilistically)")
	}
}

func TestCodeHashNeverReturnsMinusOne(t *testing.T) {
	for i := 0; i < 64; i++ {
		c := NewCode()
		c.Name = "x"
		c.Firstlineno = i
		h, err := Hash(c)
		if err != nil {
			t.Fatal(err)
		}
		if h == -1 {
			t.Errorf("hash should never be -1 (got -1 at firstlineno=%d)", i)
		}
	}
}

func TestCodeRichCompareEqual(t *testing.T) {
	a := NewCode()
	a.Name = "f"
	a.Argcount = 1
	a.Code = []byte{1, 2}
	a.Consts = []any{int64(7), "hi"}
	a.Names = []string{"a"}
	b := NewCode()
	b.Name = "f"
	b.Argcount = 1
	b.Code = []byte{1, 2}
	b.Consts = []any{int64(7), "hi"}
	b.Names = []string{"a"}
	got, err := RichCmpBool(a, b, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("expected equal")
	}
	got, err = RichCmpBool(a, b, CompareNE)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("CompareNE should be false on equal codes")
	}
}

func TestCodeRichCompareNotEqualOnFlags(t *testing.T) {
	a := NewCode()
	a.Name = "f"
	a.Flags = 1
	b := NewCode()
	b.Name = "f"
	b.Flags = 2
	got, err := RichCmpBool(a, b, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("flags differ; should not be equal")
	}
}

func TestCodeRichCompareNotEqualOnConsts(t *testing.T) {
	a := NewCode()
	a.Name = "f"
	a.Consts = []any{int64(1)}
	b := NewCode()
	b.Name = "f"
	b.Consts = []any{int64(2)}
	got, err := RichCmpBool(a, b, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("consts differ; should not be equal")
	}
}

func TestCodeRichCompareOrderingNotImplemented(t *testing.T) {
	a := NewCode()
	b := NewCode()
	got, err := RichCmp(a, b, CompareLT)
	if err != nil {
		t.Fatal(err)
	}
	if got != NotImplemented() {
		t.Errorf("ordering should be NotImplemented, got %v", got)
	}
}

func TestCodeRichCompareNonCodeFallsThroughToFalse(t *testing.T) {
	// codeRichCompare returns NotImplemented when other isn't a Code;
	// the dispatcher's identity fallback then yields False for ==.
	a := NewCode()
	got, err := RichCmpBool(a, NewInt(0), CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("code == int should be False after both sides return NotImplemented")
	}
}

func TestCodeCopyDeepCopiesSlices(t *testing.T) {
	a := NewCode()
	a.Name = "f"
	a.Code = []byte{1, 2, 3}
	a.Names = []string{"x"}
	a.Consts = []any{int64(1)}
	a.Linetable = []byte{0xf8}

	b := a.Copy()
	if b == a {
		t.Error("Copy should return a new pointer")
	}
	a.Code[0] = 99
	if b.Code[0] == 99 {
		t.Error("Copy should not share Code slice")
	}
	a.Names[0] = "y"
	if b.Names[0] == "y" {
		t.Error("Copy should not share Names slice")
	}
}

func TestCodeReplaceOverridesOnlySpecifiedFields(t *testing.T) {
	a := NewCode()
	a.Name = "old"
	a.Argcount = 1
	a.Flags = 4
	a.Code = []byte{1, 2}

	newName := "new"
	newArgcount := 3
	r := CodeReplace{
		Name:     &newName,
		Argcount: &newArgcount,
	}
	b, err := a.Replace(r)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "new" {
		t.Errorf("Name = %q, want new", b.Name)
	}
	if b.Argcount != 3 {
		t.Errorf("Argcount = %d, want 3", b.Argcount)
	}
	if b.Flags != 4 {
		t.Errorf("Flags = %d, want 4 (unchanged)", b.Flags)
	}
	if !byteSliceEqual(b.Code, []byte{1, 2}) {
		t.Errorf("Code = %v, want unchanged", b.Code)
	}
	if a.Name != "old" {
		t.Error("Replace should not mutate the receiver")
	}
}

func TestCodeReplaceCanInstallEmptySlice(t *testing.T) {
	a := NewCode()
	a.Name = "f"
	a.Names = []string{"x", "y"}

	r := CodeReplace{SetNames: true, Names: nil}
	b, err := a.Replace(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Names) != 0 {
		t.Errorf("Names = %v, want empty", b.Names)
	}
	if len(a.Names) != 2 {
		t.Error("Replace should not mutate the receiver")
	}
}

func TestCodeReplaceRejectsNegativeInts(t *testing.T) {
	a := NewCode()
	bad := -1
	r := CodeReplace{Argcount: &bad}
	if _, err := a.Replace(r); err == nil {
		t.Error("negative argcount should ValueError")
	}
}
