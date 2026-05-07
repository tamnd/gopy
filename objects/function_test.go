package objects

import "testing"

func TestNewFunctionInheritsName(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	code.Qualname = "f"
	g := NewDict()
	fn := NewFunction("f", code, g)
	if fn.Name != "f" {
		t.Errorf("Name = %q, want %q", fn.Name, "f")
	}
	if fn.Qualname != "f" {
		t.Errorf("Qualname = %q, want %q", fn.Qualname, "f")
	}
	if fn.Code != code {
		t.Error("Code not threaded through")
	}
}

func TestNewFunctionWithQualNameOverridesQualname(t *testing.T) {
	code := NewCode()
	code.Name = "inner"
	code.Qualname = "Outer.inner"
	fn, err := NewFunctionWithQualName(code, NewDict(), "different")
	if err != nil {
		t.Fatal(err)
	}
	if fn.Qualname != "different" {
		t.Errorf("Qualname = %q, want %q", fn.Qualname, "different")
	}
	if fn.Name != "inner" {
		t.Errorf("Name = %q, want %q", fn.Name, "inner")
	}
}

func TestNewFunctionWithQualNameFallsBackToCodeQualname(t *testing.T) {
	code := NewCode()
	code.Name = "inner"
	code.Qualname = "Outer.inner"
	fn, err := NewFunctionWithQualName(code, NewDict(), "")
	if err != nil {
		t.Fatal(err)
	}
	if fn.Qualname != "Outer.inner" {
		t.Errorf("Qualname = %q, want fallback to code.Qualname", fn.Qualname)
	}
}

func TestNewFunctionWithQualNameRejectsNilCode(t *testing.T) {
	if _, err := NewFunctionWithQualName(nil, NewDict(), ""); err == nil {
		t.Error("nil code should error")
	}
}

func TestNewFunctionPicksDoc(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	code.Flags = CoHasDocstring
	code.Consts = []any{"hello"}
	fn := NewFunction("f", code, NewDict())
	doc, err := Str(fn.Doc)
	if err != nil {
		t.Fatal(err)
	}
	if doc != "hello" {
		t.Errorf("Doc = %q, want %q", doc, "hello")
	}
}

func TestNewFunctionDocDefaultsToNoneWithoutFlag(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	code.Consts = []any{"would-be-doc"}
	fn := NewFunction("f", code, NewDict())
	if fn.Doc != None() {
		t.Errorf("Doc = %v, want None when CoHasDocstring is unset", fn.Doc)
	}
}

func TestNewFunctionLoadsModuleFromGlobals(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	g := NewDict()
	_ = g.SetItem(NewStr("__name__"), NewStr("mymod"))
	fn := NewFunction("f", code, g)
	if fn.Module == nil {
		t.Fatal("Module should be loaded from globals['__name__']")
	}
	mod, _ := Str(fn.Module)
	if mod != "mymod" {
		t.Errorf("Module = %q, want %q", mod, "mymod")
	}
}

func TestNewFunctionLoadsBuiltinsFromGlobals(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	builtins := NewDict()
	_ = builtins.SetItem(NewStr("len"), NewInt(0))
	g := NewDict()
	_ = g.SetItem(NewStr("__builtins__"), builtins)
	fn := NewFunction("f", code, g)
	if fn.Builtins == nil {
		t.Fatal("Builtins should be loaded from globals['__builtins__']")
	}
	if fn.Builtins != Object(builtins) {
		t.Error("Builtins should be the same dict as globals['__builtins__']")
	}
}

func TestSetCodeRejectsClosureMismatch(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	fn := NewFunction("f", code, NewDict())
	fn.Closure = NewTuple([]Object{NewInt(1)})

	other := NewCode()
	other.Name = "f"
	other.Freevars = nil
	if err := fn.SetCode(other); err == nil {
		t.Error("SetCode should reject when closure size != freevar count")
	}
}

func TestSetCodeAcceptsMatchingFreevars(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	fn := NewFunction("f", code, NewDict())
	fn.Closure = NewTuple([]Object{NewInt(1)})
	fn.Version = 42

	other := NewCode()
	other.Name = "f"
	other.Freevars = []string{"x"}
	if err := fn.SetCode(other); err != nil {
		t.Fatal(err)
	}
	if fn.Code != other {
		t.Error("SetCode should rebind Code")
	}
	if fn.Version != 0 {
		t.Errorf("Version = %d, want 0 after SetCode", fn.Version)
	}
}

func TestSetCodeRejectsNil(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	if err := fn.SetCode(nil); err == nil {
		t.Error("SetCode(nil) should TypeError")
	}
}

func TestSetDefaultsClearsVersion(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	fn.Version = 99
	t1 := NewTuple([]Object{NewInt(1)})
	fn.SetDefaults(t1)
	if fn.Defaults != t1 {
		t.Error("SetDefaults should rebind Defaults")
	}
	if fn.Version != 0 {
		t.Errorf("Version = %d, want 0 after SetDefaults", fn.Version)
	}
}

func TestSetKwDefaultsClearsVersion(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	fn.Version = 99
	d := NewDict()
	fn.SetKwDefaults(d)
	if fn.KwDefaults != d {
		t.Error("SetKwDefaults should rebind KwDefaults")
	}
	if fn.Version != 0 {
		t.Errorf("Version = %d, want 0 after SetKwDefaults", fn.Version)
	}
}

func TestSetClosureClearsVersion(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	fn.Version = 99
	tup := NewTuple([]Object{NewInt(1)})
	fn.SetClosure(tup)
	if fn.Closure != tup {
		t.Error("SetClosure should rebind Closure")
	}
	if fn.Version != 0 {
		t.Errorf("Version = %d, want 0 after SetClosure", fn.Version)
	}
}

func TestSetAnnotationsKeepsVersion(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	fn.Version = 99
	d := NewDict()
	fn.SetAnnotations(d)
	if fn.Annotations != d {
		t.Error("SetAnnotations should rebind Annotations")
	}
	if fn.Version != 99 {
		t.Errorf("Version = %d, want 99 (annotations are not a critical attribute)", fn.Version)
	}
}

func TestGetAnnotationsDirect(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	d := NewDict()
	_ = d.SetItem(NewStr("x"), NewInt(1))
	fn.Annotations = d
	got, err := fn.GetAnnotations()
	if err != nil {
		t.Fatal(err)
	}
	if got != d {
		t.Error("GetAnnotations should return the cached dict")
	}
}

func TestGetAnnotationsNilWhenNoSource(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	got, err := fn.GetAnnotations()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("GetAnnotations should be nil when neither Annotations nor Annotate is set")
	}
}

func TestFunctionRepr(t *testing.T) {
	fn := NewFunction("speak", NewCode(), NewDict())
	got, err := Repr(fn)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<function speak>" {
		t.Errorf("Repr = %q", got)
	}
}
