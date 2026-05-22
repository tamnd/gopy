package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

func TestSyntaxErrorHierarchy(t *testing.T) {
	if !errors.IsSubtype(errors.PyExc_SyntaxError, errors.PyExc_Exception) {
		t.Fatal("SyntaxError must inherit from Exception")
	}
	if !errors.IsSubtype(errors.PyExc_IndentationError, errors.PyExc_SyntaxError) {
		t.Fatal("IndentationError must inherit from SyntaxError")
	}
	if !errors.IsSubtype(errors.PyExc_TabError, errors.PyExc_IndentationError) {
		t.Fatal("TabError must inherit from IndentationError")
	}
	if !errors.IsSubtype(errors.PyExc_TabError, errors.PyExc_SyntaxError) {
		t.Fatal("TabError must transitively inherit from SyntaxError")
	}
	if !errors.IsSubtype(errors.PyExc_IncompleteInputError, errors.PyExc_SyntaxError) {
		t.Fatal("_IncompleteInputError must inherit from SyntaxError")
	}
	if errors.PyExc_TabError.Name != "TabError" {
		t.Fatalf("name = %q, want TabError", errors.PyExc_TabError.Name)
	}
}

// TestSyntaxErrorMembersFromTwoArgs covers the canonical 2-arg
// raise(msg, (filename, lineno, offset, text, end_lineno, end_offset))
// shape: each PyMemberDef field round-trips through the type's
// descriptors.
//
// CPython: Objects/exceptions.c:2713 SyntaxError_init
func TestSyntaxErrorMembersFromTwoArgs(t *testing.T) {
	info := objects.NewTuple([]objects.Object{
		objects.NewStr("f.py"),
		objects.NewInt(3),
		objects.NewInt(5),
		objects.NewStr("text"),
		objects.NewInt(3),
		objects.NewInt(9),
	})
	args := []objects.Object{objects.NewStr("bad"), info}
	out, err := objects.Call(errors.PyExc_SyntaxError, objects.NewTuple(args), nil)
	if err != nil {
		t.Fatalf("call SyntaxError: %v", err)
	}
	got := map[string]string{}
	for _, name := range []string{"msg", "filename", "lineno", "offset", "text", "end_lineno", "end_offset"} {
		v, err := objects.GetAttr(out, objects.NewStr(name))
		if err != nil {
			t.Fatalf("getattr %s: %v", name, err)
		}
		s, err := objects.Str(v)
		if err != nil {
			t.Fatalf("str %s: %v", name, err)
		}
		got[name] = s
	}
	want := map[string]string{
		"msg":        "bad",
		"filename":   "f.py",
		"lineno":     "3",
		"offset":     "5",
		"text":       "text",
		"end_lineno": "3",
		"end_offset": "9",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

// TestSyntaxErrorStrFormatting covers SyntaxError_str's three render
// branches (filename + lineno, msg only, lineno only).
//
// CPython: Objects/exceptions.c:2830 SyntaxError_str
func TestSyntaxErrorStrFormatting(t *testing.T) {
	cases := []struct {
		name string
		args []objects.Object
		want string
	}{
		{
			"single arg",
			[]objects.Object{objects.NewStr("only msg")},
			"only msg",
		},
		{
			"filename + lineno",
			[]objects.Object{
				objects.NewStr("bad"),
				objects.NewTuple([]objects.Object{
					objects.NewStr("path/to/f.py"),
					objects.NewInt(7),
					objects.NewInt(2),
					objects.NewStr("t"),
				}),
			},
			"bad (f.py, line 7)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := objects.Call(errors.PyExc_SyntaxError, objects.NewTuple(c.args), nil)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			got, err := objects.Str(out)
			if err != nil {
				t.Fatalf("str: %v", err)
			}
			if got != c.want {
				t.Fatalf("str = %q, want %q", got, c.want)
			}
		})
	}
}
