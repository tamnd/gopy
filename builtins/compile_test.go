// Coverage for the compile() builtin: argument parsing, mode mapping,
// and the parse + compile round-trip producing a usable code object.

package builtins

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestCompileExecReturnsCodeObject(t *testing.T) {
	out, err := Compile([]objects.Object{
		objects.NewStr("x = 1\n"),
		objects.NewStr("<test>"),
		objects.NewStr("exec"),
	}, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, ok := out.(*objects.Code); !ok {
		t.Fatalf("compile returned %T, want *objects.Code", out)
	}
}

func TestCompileEvalAcceptsExpression(t *testing.T) {
	out, err := Compile([]objects.Object{
		objects.NewStr("1 + 2"),
		objects.NewStr("<test>"),
		objects.NewStr("eval"),
	}, nil)
	if err != nil {
		t.Fatalf("compile eval: %v", err)
	}
	if _, ok := out.(*objects.Code); !ok {
		t.Fatalf("compile eval returned %T, want *objects.Code", out)
	}
}

func TestCompileSingleAcceptsStatement(t *testing.T) {
	out, err := Compile([]objects.Object{
		objects.NewStr("print(1)\n"),
		objects.NewStr("<test>"),
		objects.NewStr("single"),
	}, nil)
	if err != nil {
		t.Fatalf("compile single: %v", err)
	}
	if _, ok := out.(*objects.Code); !ok {
		t.Fatalf("compile single returned %T, want *objects.Code", out)
	}
}

func TestCompileBadModeRaisesValueError(t *testing.T) {
	_, err := Compile([]objects.Object{
		objects.NewStr("x = 1\n"),
		objects.NewStr("<test>"),
		objects.NewStr("bogus"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError") {
		t.Fatalf("err = %v, want ValueError on bad mode", err)
	}
}

func TestCompileFuncTypeWithoutASTFlagRejected(t *testing.T) {
	_, err := Compile([]objects.Object{
		objects.NewStr("def f() -> int: ...\n"),
		objects.NewStr("<test>"),
		objects.NewStr("func_type"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError") {
		t.Fatalf("err = %v, want ValueError on func_type", err)
	}
}

func TestCompileMissingSourceRaisesTypeError(t *testing.T) {
	_, err := Compile([]objects.Object{}, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError on missing source", err)
	}
}

func TestCompileTooManyArgsRaisesTypeError(t *testing.T) {
	args := []objects.Object{
		objects.NewStr("x = 1\n"),
		objects.NewStr("<test>"),
		objects.NewStr("exec"),
		objects.NewInt(0),
		objects.NewInt(0),
		objects.NewInt(-1),
		objects.NewInt(-1),
	}
	_, err := Compile(args, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError on extra arg", err)
	}
}

func TestCompileOptimizeOutOfRange(t *testing.T) {
	_, err := Compile([]objects.Object{
		objects.NewStr("x = 1\n"),
		objects.NewStr("<test>"),
		objects.NewStr("exec"),
		objects.NewInt(0),
		objects.False(),
		objects.NewInt(7),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "ValueError") {
		t.Fatalf("err = %v, want ValueError on optimize=7", err)
	}
}

func TestCompileSourceMustBeStr(t *testing.T) {
	_, err := Compile([]objects.Object{
		objects.NewInt(1),
		objects.NewStr("<test>"),
		objects.NewStr("exec"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError on non-str source", err)
	}
}

func TestCompileKeywordArgs(t *testing.T) {
	out, err := Compile(
		[]objects.Object{objects.NewStr("x = 1\n")},
		map[string]objects.Object{
			"filename": objects.NewStr("<kw>"),
			"mode":     objects.NewStr("exec"),
			"optimize": objects.NewInt(-1),
		},
	)
	if err != nil {
		t.Fatalf("compile kwargs: %v", err)
	}
	if _, ok := out.(*objects.Code); !ok {
		t.Fatalf("compile kwargs returned %T, want *objects.Code", out)
	}
}

func TestCompileUnknownKwarg(t *testing.T) {
	_, err := Compile(
		[]objects.Object{
			objects.NewStr("x = 1\n"),
			objects.NewStr("<test>"),
			objects.NewStr("exec"),
		},
		map[string]objects.Object{"bogus": objects.NewInt(1)},
	)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError on unknown kwarg", err)
	}
}
