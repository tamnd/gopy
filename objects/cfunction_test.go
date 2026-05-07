package objects

import (
	"errors"
	"testing"
)

func TestCFunctionMethNoArgs(t *testing.T) {
	def := &MethodDef{
		Name:  "ping",
		Flags: MethNoArgs,
		NoArgs: func(self Object) (Object, error) {
			return NewInt(7), nil
		},
	}
	cf, err := NewCFunction(def, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Vectorcall(cf, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 7 {
		t.Errorf("got %d, want 7", intVal(got, t))
	}
}

func TestCFunctionMethNoArgsRejectsArgs(t *testing.T) {
	def := &MethodDef{
		Name:   "ping",
		Flags:  MethNoArgs,
		NoArgs: func(self Object) (Object, error) { return NewInt(0), nil },
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	if _, err := Vectorcall(cf, []Object{NewInt(1)}, 1, nil); err == nil {
		t.Error("MethNoArgs with 1 positional arg should TypeError")
	}
}

func TestCFunctionMethO(t *testing.T) {
	def := &MethodDef{
		Name:  "negate",
		Flags: MethO,
		O: func(self, arg Object) (Object, error) {
			i := arg.(*Int)
			v, _ := i.Int64()
			return NewInt(-v), nil
		},
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	got, err := Vectorcall(cf, []Object{NewInt(5)}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != -5 {
		t.Errorf("got %d, want -5", intVal(got, t))
	}
}

func TestCFunctionMethORejectsWrongArity(t *testing.T) {
	def := &MethodDef{
		Name:  "id",
		Flags: MethO,
		O:     func(self, arg Object) (Object, error) { return arg, nil },
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	if _, err := Vectorcall(cf, nil, 0, nil); err == nil {
		t.Error("MethO with 0 args should TypeError")
	}
	if _, err := Vectorcall(cf, []Object{NewInt(1), NewInt(2)}, 2, nil); err == nil {
		t.Error("MethO with 2 args should TypeError")
	}
}

func TestCFunctionMethFastcall(t *testing.T) {
	def := &MethodDef{
		Name:  "sum3",
		Flags: MethFastcall,
		Fastcall: func(self Object, args []Object) (Object, error) {
			var total int64
			for _, a := range args {
				v, _ := a.(*Int).Int64()
				total += v
			}
			return NewInt(total), nil
		},
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	got, err := Vectorcall(cf, []Object{NewInt(1), NewInt(2), NewInt(3)}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 6 {
		t.Errorf("got %d, want 6", intVal(got, t))
	}
}

func TestCFunctionMethFastcallRejectsKwargs(t *testing.T) {
	def := &MethodDef{
		Name:     "sum",
		Flags:    MethFastcall,
		Fastcall: func(self Object, args []Object) (Object, error) { return NewInt(0), nil },
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	kwnames := NewTuple([]Object{NewStr("x")})
	if _, err := Vectorcall(cf, []Object{NewInt(1)}, 0, kwnames); err == nil {
		t.Error("MethFastcall with kwargs should TypeError")
	}
}

func TestCFunctionMethFastcallKeywords(t *testing.T) {
	captured := map[string]int64{}
	def := &MethodDef{
		Name:  "f",
		Flags: MethFastcall | MethKeywords,
		FastcallKw: func(self Object, args []Object, kwnames *Tuple) (Object, error) {
			nargs := len(args)
			if kwnames != nil {
				nargs -= kwnames.Len()
				for i := 0; i < kwnames.Len(); i++ {
					name, _ := Str(kwnames.Item(i))
					v, _ := args[nargs+i].(*Int).Int64()
					captured[name] = v
				}
			}
			return NewInt(int64(nargs)), nil
		},
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	args := []Object{NewInt(10), NewInt(20), NewInt(99)}
	kwnames := NewTuple([]Object{NewStr("k")})
	got, err := Vectorcall(cf, args, 2, kwnames)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 2 {
		t.Errorf("nargs = %d, want 2", intVal(got, t))
	}
	if captured["k"] != 99 {
		t.Errorf("k = %d, want 99", captured["k"])
	}
}

func TestCFunctionMethVarargs(t *testing.T) {
	def := &MethodDef{
		Name:  "tup",
		Flags: MethVarargs,
		Varargs: func(self Object, args *Tuple) (Object, error) {
			return NewInt(int64(args.Len())), nil
		},
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	got, err := Call(cf, NewTuple([]Object{NewInt(1), NewInt(2), NewInt(3)}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 3 {
		t.Errorf("got %d, want 3", intVal(got, t))
	}
}

func TestCFunctionMethVarargsRejectsKwargs(t *testing.T) {
	def := &MethodDef{
		Name:    "tup",
		Flags:   MethVarargs,
		Varargs: func(self Object, args *Tuple) (Object, error) { return NewInt(0), nil },
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	kwargs := NewDict()
	_ = kwargs.SetItem(NewStr("k"), NewInt(1))
	if _, err := Call(cf, NewTuple(nil), kwargs); err == nil {
		t.Error("MethVarargs with kwargs should TypeError")
	}
}

func TestCFunctionMethVarargsKeywords(t *testing.T) {
	def := &MethodDef{
		Name:  "with_kw",
		Flags: MethVarargs | MethKeywords,
		VarargsKw: func(self Object, args *Tuple, kw *Dict) (Object, error) {
			n := args.Len()
			if kw != nil {
				n += kw.Len()
			}
			return NewInt(int64(n)), nil
		},
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	kwargs := NewDict()
	_ = kwargs.SetItem(NewStr("k"), NewInt(1))
	got, err := Call(cf, NewTuple([]Object{NewInt(10), NewInt(20)}), kwargs)
	if err != nil {
		t.Fatal(err)
	}
	if intVal(got, t) != 3 {
		t.Errorf("got %d, want 3", intVal(got, t))
	}
}

func TestCFunctionMethMethod(t *testing.T) {
	owner := NewType("ownerCls", []*Type{objectType})
	var sawCls *Type
	def := &MethodDef{
		Name:  "m",
		Flags: MethMethod | MethFastcall | MethKeywords,
		Method: func(self Object, cls *Type, args []Object, kwnames *Tuple) (Object, error) {
			sawCls = cls
			return NewInt(int64(len(args))), nil
		},
	}
	cf, err := NewCFunction(def, nil, nil, owner)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Vectorcall(cf, []Object{NewInt(1), NewInt(2)}, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sawCls != owner {
		t.Errorf("Method got cls %p, want %p", sawCls, owner)
	}
	if intVal(got, t) != 2 {
		t.Errorf("got %d, want 2", intVal(got, t))
	}
}

func TestCFunctionMethMethodRequiresCls(t *testing.T) {
	def := &MethodDef{
		Name:   "m",
		Flags:  MethMethod | MethFastcall | MethKeywords,
		Method: func(self Object, cls *Type, args []Object, kwnames *Tuple) (Object, error) { return NewInt(0), nil },
	}
	if _, err := NewCFunction(def, nil, nil, nil); err == nil {
		t.Error("MethMethod without cls should SystemError")
	}
}

func TestCFunctionRejectsBadFlags(t *testing.T) {
	def := &MethodDef{Name: "weird", Flags: MethVarargs | MethFastcall}
	if _, err := NewCFunction(def, nil, nil, nil); err == nil {
		t.Error("incompatible flag combo should SystemError")
	}
}

func TestCFunctionRejectsMissingClosure(t *testing.T) {
	def := &MethodDef{Name: "blank", Flags: MethNoArgs}
	if _, err := NewCFunction(def, nil, nil, nil); err == nil {
		t.Error("MethNoArgs without NoArgs closure should SystemError")
	}
}

func TestCFunctionRejectsClsWithoutMethodFlag(t *testing.T) {
	owner := NewType("ownerCls2", []*Type{objectType})
	def := &MethodDef{
		Name:    "v",
		Flags:   MethVarargs,
		Varargs: func(self Object, args *Tuple) (Object, error) { return NewInt(0), nil },
	}
	if _, err := NewCFunction(def, nil, nil, owner); err == nil {
		t.Error("non-MethMethod with cls should SystemError")
	}
}

func TestCFunctionRepr(t *testing.T) {
	def := &MethodDef{
		Name:   "speak",
		Flags:  MethNoArgs,
		NoArgs: func(self Object) (Object, error) { return NewInt(0), nil },
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	r, err := Repr(cf)
	if err != nil {
		t.Fatal(err)
	}
	if r != "<built-in function speak>" {
		t.Errorf("got %q", r)
	}
}

func TestCFunctionPropagatesError(t *testing.T) {
	want := errors.New("boom")
	def := &MethodDef{
		Name:   "fail",
		Flags:  MethNoArgs,
		NoArgs: func(self Object) (Object, error) { return nil, want },
	}
	cf, _ := NewCFunction(def, nil, nil, nil)
	if _, err := CallNoArgs(cf); !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestCFunctionSelfBound(t *testing.T) {
	bound := NewInt(42)
	def := &MethodDef{
		Name:   "who",
		Flags:  MethNoArgs,
		NoArgs: func(self Object) (Object, error) { return self, nil },
	}
	cf, _ := NewCFunction(def, bound, nil, nil)
	got, err := CallNoArgs(cf)
	if err != nil {
		t.Fatal(err)
	}
	if got != bound {
		t.Error("self should be the bound object")
	}
}
