package objects

import (
	"fmt"
	"strings"
	"testing"
)

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
	want := fmt.Sprintf("<function speak at %p>", fn)
	if got != want {
		t.Errorf("Repr = %q, want %q", got, want)
	}
}

// gate 4.3: repr starts with `<function NAME at` regardless of the
// pointer suffix. Pinning the prefix in a test catches accidental
// format regressions without binding to the address.
//
// CPython: Objects/funcobject.c:920 func_repr
func TestFunctionReprPrefix(t *testing.T) {
	fn := NewFunction("speak", NewCode(), NewDict())
	got, err := Repr(fn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "<function speak at ") {
		t.Errorf("Repr = %q, want prefix %q", got, "<function speak at ")
	}
}

// func_traverse must visit every Object-bearing slot. CPython's
// cycle collector walks code, globals, module, defaults, kwdefaults,
// doc, dict, closure, annotations, annotate, typeparams, builtins.
// Anything else we add to *Function later should land here too.
//
// CPython: Objects/funcobject.c:1093 func_traverse
func TestFunctionTraverseVisitsEverySlot(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	g := NewDict()
	fn := NewFunction("f", code, g)
	fn.Module = NewStr("mymod")
	fn.Defaults = NewTuple([]Object{NewInt(1)})
	fn.KwDefaults = NewDict()
	fn.Doc = NewStr("doc")
	fn.Dict = NewDict()
	fn.Closure = NewTuple([]Object{NewCell(nil)})
	fn.Annotations = NewDict()
	fn.Annotate = NewBuiltinFunction("annotate", func(_ []Object, _ map[string]Object) (Object, error) {
		return NewDict(), nil
	})
	fn.Typeparams = NewTuple([]Object{NewStr("T")})
	fn.Builtins = NewDict()

	got, err := collectVisited(fn)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	want := []Object{
		code, g, fn.Module, fn.Defaults, fn.KwDefaults, fn.Doc, fn.Dict,
		fn.Closure, fn.Annotations, fn.Annotate, fn.Typeparams, fn.Builtins,
	}
	if len(got) != len(want) {
		t.Fatalf("traverse = %v (%d), want %d entries", got, len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("traverse[%d] = %v, want %v", i, got[i], w)
		}
	}
}

func TestFunctionTraverseSkipsNilSlots(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	got, err := collectVisited(fn)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	// Default state: Code + Globals + Doc(=None). Module is nil here
	// because the globals dict has no __name__ entry.
	for _, v := range got {
		if v == nil {
			t.Fatalf("traverse yielded nil entry: %v", got)
		}
	}
}

// __code__ setter goes through SetAttr; the descriptor must reject
// non-code values with TypeError and accept matching freevar counts.
//
// CPython: Objects/funcobject.c:597 func_set_code
func TestFuncCodeAttrSetter(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	other := NewCode()
	other.Name = "g"
	if err := SetAttr(fn, NewStr("__code__"), other); err != nil {
		t.Fatalf("SetAttr __code__: %v", err)
	}
	if fn.Code != other {
		t.Error("__code__ setter should rebind Code")
	}
	if err := SetAttr(fn, NewStr("__code__"), NewInt(5)); err == nil {
		t.Error("__code__ setter should reject non-code")
	}
}

// __defaults__ setter: None clears, tuple binds, anything else errors.
//
// CPython: Objects/funcobject.c:766 func_set_defaults
func TestFuncDefaultsAttrSetter(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	tup := NewTuple([]Object{NewInt(1)})
	if err := SetAttr(fn, NewStr("__defaults__"), tup); err != nil {
		t.Fatalf("SetAttr __defaults__: %v", err)
	}
	if fn.Defaults != tup {
		t.Error("__defaults__ setter should rebind Defaults")
	}
	if err := SetAttr(fn, NewStr("__defaults__"), None()); err != nil {
		t.Fatalf("SetAttr __defaults__ = None: %v", err)
	}
	if fn.Defaults != nil {
		t.Error("__defaults__ = None should clear Defaults")
	}
	if err := SetAttr(fn, NewStr("__defaults__"), NewInt(5)); err == nil {
		t.Error("__defaults__ setter should reject non-tuple")
	}
}

// __kwdefaults__ setter: None clears, dict binds, anything else errors.
//
// CPython: Objects/funcobject.c:809 func_set_kwdefaults
func TestFuncKwDefaultsAttrSetter(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	d := NewDict()
	if err := SetAttr(fn, NewStr("__kwdefaults__"), d); err != nil {
		t.Fatalf("SetAttr __kwdefaults__: %v", err)
	}
	if fn.KwDefaults != d {
		t.Error("__kwdefaults__ setter should rebind KwDefaults")
	}
	if err := SetAttr(fn, NewStr("__kwdefaults__"), None()); err != nil {
		t.Fatalf("SetAttr __kwdefaults__ = None: %v", err)
	}
	if fn.KwDefaults != nil {
		t.Error("__kwdefaults__ = None should clear KwDefaults")
	}
	if err := SetAttr(fn, NewStr("__kwdefaults__"), NewInt(5)); err == nil {
		t.Error("__kwdefaults__ setter should reject non-dict")
	}
}

// __annotations__ getter lazily materializes an empty dict.
//
// CPython: Objects/funcobject.c:895 function___annotations___get_impl
func TestFuncAnnotationsAttrGetterMaterializes(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	v, err := GetAttr(fn, NewStr("__annotations__"))
	if err != nil {
		t.Fatalf("GetAttr __annotations__: %v", err)
	}
	if _, ok := v.(*Dict); !ok {
		t.Fatalf("__annotations__ = %v, want empty dict", v)
	}
	v2, err := GetAttr(fn, NewStr("__annotations__"))
	if err != nil {
		t.Fatalf("GetAttr __annotations__ (2nd): %v", err)
	}
	if v != v2 {
		t.Error("__annotations__ second read should return cached identity")
	}
}

// __annotations__ setter: None clears, dict binds, anything else errors.
//
// CPython: Objects/funcobject.c:916 function___annotations___set_impl
func TestFuncAnnotationsAttrSetter(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	d := NewDict()
	if err := SetAttr(fn, NewStr("__annotations__"), d); err != nil {
		t.Fatalf("SetAttr __annotations__: %v", err)
	}
	if fn.Annotations != d {
		t.Error("__annotations__ setter should rebind Annotations")
	}
	if err := SetAttr(fn, NewStr("__annotations__"), None()); err != nil {
		t.Fatalf("SetAttr __annotations__ = None: %v", err)
	}
	if fn.Annotations != nil {
		t.Error("__annotations__ = None should clear Annotations")
	}
	if err := SetAttr(fn, NewStr("__annotations__"), NewInt(5)); err == nil {
		t.Error("__annotations__ setter should reject non-dict")
	}
}

// __annotate__ setter: callable swaps and clears the cached
// annotations dict so the next read re-runs the annotator.
//
// CPython: Objects/funcobject.c:862 function___annotate___set_impl
func TestFuncAnnotateAttrSetter(t *testing.T) {
	fn := NewFunction("f", NewCode(), NewDict())
	fn.Annotations = NewDict()
	annotator := NewBuiltinFunction("annotate", func(_ []Object, _ map[string]Object) (Object, error) {
		return NewDict(), nil
	})
	if err := SetAttr(fn, NewStr("__annotate__"), annotator); err != nil {
		t.Fatalf("SetAttr __annotate__: %v", err)
	}
	if fn.Annotate != annotator {
		t.Error("__annotate__ setter should rebind Annotate")
	}
	if fn.Annotations != nil {
		t.Error("__annotate__ setter should clear cached Annotations")
	}
	if err := SetAttr(fn, NewStr("__annotate__"), NewInt(5)); err == nil {
		t.Error("__annotate__ setter should reject non-callable")
	}
}

// function.__new__(code, globals) — the minimal two-argument form
// returns a fresh function bound to the supplied code and globals.
//
// CPython: Objects/funcobject.c:432 func_new_impl
func TestFuncTpNewMinimalArgs(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	g := NewDict()
	obj, err := FunctionType.TpNew(FunctionType, []Object{code, g}, nil)
	if err != nil {
		t.Fatalf("function.__new__: %v", err)
	}
	fn, ok := obj.(*Function)
	if !ok {
		t.Fatalf("function.__new__ returned %T, want *Function", obj)
	}
	if fn.Code != code {
		t.Error("function.__new__ should bind Code")
	}
	if fn.Globals != Object(g) {
		t.Error("function.__new__ should bind Globals")
	}
}

// function.__new__ enforces typed validation on each positional
// argument, mirroring func_new_impl's parsing block.
//
// CPython: Objects/funcobject.c:432 func_new_impl
func TestFuncTpNewValidates(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	g := NewDict()
	cases := []struct {
		name string
		args []Object
	}{
		{"code-not-code", []Object{NewInt(1), g}},
		{"globals-not-dict", []Object{code, NewInt(1)}},
		{"name-not-str", []Object{code, g, NewInt(1)}},
		{"defaults-not-tuple", []Object{code, g, None(), NewInt(1)}},
		{"closure-not-tuple", []Object{code, g, None(), None(), NewInt(1)}},
		{"kwdefaults-not-dict", []Object{code, g, None(), None(), None(), NewInt(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FunctionType.TpNew(FunctionType, tc.args, nil); err == nil {
				t.Errorf("function.__new__(%s) should error", tc.name)
			}
		})
	}
}

// function.__new__ rejects closures whose length does not match the
// code object's freevar count.
//
// CPython: Objects/funcobject.c:432 func_new_impl
func TestFuncTpNewRejectsBadClosureSize(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	code.Freevars = []string{"a", "b"}
	tup := NewTuple([]Object{NewCell(nil)})
	_, err := FunctionType.TpNew(FunctionType, []Object{code, NewDict(), None(), None(), tup}, nil)
	if err == nil {
		t.Error("function.__new__ should reject mismatched closure length")
	}
}

// function.__new__ requires every closure entry to be a Cell.
//
// CPython: Objects/funcobject.c:432 func_new_impl
func TestFuncTpNewRejectsNonCellClosureEntry(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	code.Freevars = []string{"a"}
	tup := NewTuple([]Object{NewInt(1)})
	_, err := FunctionType.TpNew(FunctionType, []Object{code, NewDict(), None(), None(), tup}, nil)
	if err == nil {
		t.Error("function.__new__ should reject non-cell closure entry")
	}
}

// function.__new__ accepts kwargs for code/globals/name/argdefs/
// closure/kwdefaults, matching the clinic-generated keyword names.
//
// CPython: Objects/funcobject.c:432 func_new_impl
func TestFuncTpNewAcceptsKwargs(t *testing.T) {
	code := NewCode()
	code.Name = "f"
	g := NewDict()
	obj, err := FunctionType.TpNew(FunctionType, nil, map[string]Object{
		"code":    code,
		"globals": g,
		"name":    NewStr("renamed"),
	})
	if err != nil {
		t.Fatalf("function.__new__ kwargs: %v", err)
	}
	fn := obj.(*Function)
	if fn.Name != "renamed" {
		t.Errorf("Name = %q, want %q", fn.Name, "renamed")
	}
}
