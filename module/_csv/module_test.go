package _csv

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestReaderBasic checks that a reader on ["a,b,c"] yields ["a","b","c"].
func TestReaderBasic(t *testing.T) {
	// Build a Python list ["a,b,c"] to use as the source iterable.
	lines := objects.NewList([]objects.Object{objects.NewStr("a,b,c")})
	rdr, err := csvReader([]objects.Object{lines}, nil)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	r, ok := rdr.(*Reader)
	if !ok {
		t.Fatalf("reader: expected *Reader, got %T", rdr)
	}
	row, err := readerIterNext(r)
	if err != nil {
		t.Fatalf("IterNext: %v", err)
	}
	lst, ok := row.(*objects.List)
	if !ok {
		t.Fatalf("IterNext: expected *List, got %T", row)
	}
	if lst.Len() != 3 {
		t.Fatalf("row length: got %d, want 3", lst.Len())
	}
	for i, want := range []string{"a", "b", "c"} {
		v := lst.Item(i)
		s, ok := v.(*objects.Unicode)
		if !ok {
			t.Fatalf("item %d: expected str, got %T", i, v)
		}
		if s.Value() != want {
			t.Fatalf("item %d: got %q, want %q", i, s.Value(), want)
		}
	}
}

// csvTestOutput is a minimal Python-visible object with a write() method
// used to capture CSV output in tests. It wires Getattro to look up
// "write" directly from a field on the struct, bypassing the instance dict.
type csvTestOutput struct {
	objects.Header
	buf strings.Builder
}

var csvTestOutputType *objects.Type

func init() {
	t := objects.NewType("_CsvTestOutput", []*objects.Type{objects.ObjectType()})
	t.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		co, ok := o.(*csvTestOutput)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected _CsvTestOutput")
		}
		n, ok2 := name.(*objects.Unicode)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: attribute name must be str")
		}
		if n.Value() == "write" {
			return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("write() takes 1 argument")
				}
				s, ok := args[0].(*objects.Unicode)
				if !ok {
					return nil, fmt.Errorf("write() argument must be str")
				}
				co.buf.WriteString(s.Value())
				return objects.NewInt(int64(len(s.Value()))), nil
			}), nil
		}
		return nil, fmt.Errorf("AttributeError: _CsvTestOutput has no attribute %q", n.Value())
	}
	csvTestOutputType = t
}

// TestWriterWriteRow checks that writerow(["x","y"]) produces "x,y\r\n".
func TestWriterWriteRow(t *testing.T) {
	out := &csvTestOutput{}
	out.Init(csvTestOutputType)

	wrt, err := csvWriter([]objects.Object{out}, nil)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	w, ok := wrt.(*Writer)
	if !ok {
		t.Fatalf("writer: expected *Writer, got %T", wrt)
	}
	row := objects.NewList([]objects.Object{objects.NewStr("x"), objects.NewStr("y")})
	_, err = writerWriteRow(w, []objects.Object{row}, nil)
	if err != nil {
		t.Fatalf("writerow: %v", err)
	}
	got := out.buf.String()
	if got != "x,y\r\n" {
		t.Fatalf("writerow: got %q, want %q", got, "x,y\r\n")
	}
}

// TestRegisterDialect checks that a dialect can be registered and retrieved.
func TestRegisterDialect(t *testing.T) {
	_, err := registerDialect([]objects.Object{
		objects.NewStr("testdialect"),
	}, map[string]objects.Object{
		"delimiter": objects.NewStr("|"),
	})
	if err != nil {
		t.Fatalf("register_dialect: %v", err)
	}
	got, err := getDialect([]objects.Object{objects.NewStr("testdialect")}, nil)
	if err != nil {
		t.Fatalf("get_dialect: %v", err)
	}
	d, ok := got.(*Dialect)
	if !ok {
		t.Fatalf("get_dialect: expected *Dialect, got %T", got)
	}
	if d.delimiter != '|' {
		t.Fatalf("get_dialect: delimiter = %q, want %q", d.delimiter, '|')
	}
}

// TestListDialects checks that built-in dialects are listed.
func TestListDialects(t *testing.T) {
	out, err := listDialects(nil, nil)
	if err != nil {
		t.Fatalf("list_dialects: %v", err)
	}
	lst, ok := out.(*objects.List)
	if !ok {
		t.Fatalf("list_dialects: expected list, got %T", out)
	}
	if lst.Len() < 2 {
		t.Fatalf("list_dialects: expected at least 2 entries, got %d", lst.Len())
	}
}

// TestFieldSizeLimit checks get/set of the field size limit.
func TestFieldSizeLimit(t *testing.T) {
	// Get current.
	out, err := fieldSizeLimitFn(nil, nil)
	if err != nil {
		t.Fatalf("field_size_limit (get): %v", err)
	}
	iv := out.(*objects.Int)
	old, _ := iv.Int64()
	if old != int64(defaultFieldSizeLimit) {
		t.Fatalf("field_size_limit: default = %d, want %d", old, defaultFieldSizeLimit)
	}
	// Set new value.
	_, err = fieldSizeLimitFn([]objects.Object{objects.NewInt(512)}, nil)
	if err != nil {
		t.Fatalf("field_size_limit (set): %v", err)
	}
	if fieldSizeLimit != 512 {
		t.Fatalf("field_size_limit: after set = %d, want 512", fieldSizeLimit)
	}
	// Restore.
	fieldSizeLimit = int(old)
}

// TestBuildModule checks the module is constructed without errors.
func TestBuildModule(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	if m == nil {
		t.Fatal("buildModule: returned nil")
	}
}
