// Tests for the CPython-faithful CSV state-machine parser. Fixtures
// were captured from CPython 3.14 with the same dialect knobs so the
// reader's behaviour stays byte-identical to the upstream module on
// each quoting variant, line-continuation case, and strict-mode error.

package _csv

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// readRows drives csvReader on a multi-line source string, splitting
// it into lines exactly like the io.TextIOBase iterator would.
func readRows(t *testing.T, source string, kwargs map[string]objects.Object) [][]objects.Object {
	t.Helper()
	parts := strings.SplitAfter(source, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	items := make([]objects.Object, len(parts))
	for i, p := range parts {
		items[i] = objects.NewStr(p)
	}
	lines := objects.NewList(items)
	rdr, err := csvReader([]objects.Object{lines}, kwargs)
	if err != nil {
		t.Fatalf("csvReader: %v", err)
	}
	var rows [][]objects.Object
	for {
		row, err := readerIterNext(rdr)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			t.Fatalf("readerIterNext: %v", err)
		}
		lst := row.(*objects.List)
		out := make([]objects.Object, lst.Len())
		for i := 0; i < lst.Len(); i++ {
			out[i] = lst.Item(i)
		}
		rows = append(rows, out)
	}
	return rows
}

// strRow extracts a row as []string, asserting every cell is *Unicode.
func strRow(t *testing.T, row []objects.Object) []string {
	t.Helper()
	out := make([]string, len(row))
	for i, v := range row {
		u, ok := v.(*objects.Unicode)
		if !ok {
			t.Fatalf("row[%d]: expected Unicode, got %T", i, v)
		}
		out[i] = u.Value()
	}
	return out
}

// TestReaderQuoted exercises doublequote (`""` -> `"` inside a quoted
// field) and line continuation inside a quoted field.
func TestReaderQuoted(t *testing.T) {
	rows := readRows(t, "\"a\"\"b\",c\n\"line1\nline2\",d\n", nil)
	if len(rows) != 2 {
		t.Fatalf("rows: got %d, want 2", len(rows))
	}
	want := [][]string{{`a"b`, "c"}, {"line1\nline2", "d"}}
	for i, w := range want {
		got := strRow(t, rows[i])
		if strings.Join(got, "|") != strings.Join(w, "|") {
			t.Fatalf("row %d: got %v, want %v", i, got, w)
		}
	}
}

// TestReaderEscapedChar tests escapechar='\\': a backslash before a
// delimiter or quotechar preserves it as a literal field byte.
func TestReaderEscapedChar(t *testing.T) {
	rows := readRows(t, "a\\,b,c\n", map[string]objects.Object{
		"escapechar": objects.NewStr("\\"),
	})
	if len(rows) != 1 {
		t.Fatalf("rows: got %d, want 1", len(rows))
	}
	got := strRow(t, rows[0])
	want := []string{"a,b", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("row: got %v, want %v", got, want)
	}
}

// TestReaderQuoteNonNumeric: unquoted fields parse to floats, quoted
// stay as strings.
func TestReaderQuoteNonNumeric(t *testing.T) {
	rows := readRows(t, "\"a\",1.5,2\n", map[string]objects.Object{
		"quoting": objects.NewInt(int64(quoteNonNumeric)),
	})
	if len(rows) != 1 {
		t.Fatalf("rows: got %d, want 1", len(rows))
	}
	row := rows[0]
	if u, ok := row[0].(*objects.Unicode); !ok || u.Value() != "a" {
		t.Fatalf("row[0]: got %v, want \"a\"", row[0])
	}
	if f, ok := row[1].(*objects.Float); !ok || f.Float64() != 1.5 {
		t.Fatalf("row[1]: got %v, want 1.5", row[1])
	}
	if f, ok := row[2].(*objects.Float); !ok || f.Float64() != 2.0 {
		t.Fatalf("row[2]: got %v, want 2.0", row[2])
	}
}

// TestReaderQuoteNotNull: empty unquoted fields become None.
func TestReaderQuoteNotNull(t *testing.T) {
	rows := readRows(t, "a,,c\n", map[string]objects.Object{
		"quoting": objects.NewInt(int64(quoteNotNull)),
	})
	if len(rows) != 1 {
		t.Fatalf("rows: got %d, want 1", len(rows))
	}
	row := rows[0]
	if u, ok := row[0].(*objects.Unicode); !ok || u.Value() != "a" {
		t.Fatalf("row[0]: got %v, want \"a\"", row[0])
	}
	if row[1] != objects.None() {
		t.Fatalf("row[1]: got %v, want None", row[1])
	}
	if u, ok := row[2].(*objects.Unicode); !ok || u.Value() != "c" {
		t.Fatalf("row[2]: got %v, want \"c\"", row[2])
	}
}

// TestReaderStrictUnexpectedEnd: strict=True raises on unterminated
// quoted field at EOF.
func TestReaderStrictUnexpectedEnd(t *testing.T) {
	parts := []objects.Object{objects.NewStr("\"unterminated\n")}
	lines := objects.NewList(parts)
	rdr, err := csvReader([]objects.Object{lines}, map[string]objects.Object{
		"strict": objects.NewBool(true),
	})
	if err != nil {
		t.Fatalf("csvReader: %v", err)
	}
	if _, err := readerIterNext(rdr); err == nil {
		t.Fatal("readerIterNext: want error on unterminated quoted field")
	}
}

// TestReaderStrictNewlineUnquoted: strict mode + a CR/LF that should
// not be there raises rather than tolerating.
func TestReaderStrictBadQuoteEnd(t *testing.T) {
	parts := []objects.Object{objects.NewStr("\"abc\"xyz,d\n")}
	lines := objects.NewList(parts)
	rdr, err := csvReader([]objects.Object{lines}, map[string]objects.Object{
		"strict": objects.NewBool(true),
	})
	if err != nil {
		t.Fatalf("csvReader: %v", err)
	}
	if _, err := readerIterNext(rdr); err == nil {
		t.Fatal("readerIterNext: want strict error after closing quote")
	}
}

// TestWriterQuoting walks each quoting mode against the CPython
// reference output captured from `csv.writer(io.StringIO(), quoting=...,
// lineterminator='\\n').writerow(row)` for a known row containing a
// delimiter, a quotechar, and a numeric.
func TestWriterQuoting(t *testing.T) {
	row := objects.NewList([]objects.Object{
		objects.NewStr("a,b"),    // delimiter inside field
		objects.NewStr("c\"d"),   // quotechar inside field
		objects.NewInt(7),        // numeric -> needs str() for non-quoteAll modes
		objects.None(),           // None
	})
	cases := []struct {
		name    string
		quoting int
		want    string
	}{
		{"minimal", quoteMinimal, `"a,b","c""d",7,` + "\n"},
		{"all", quoteAll, `"a,b","c""d","7",""` + "\n"},
		{"nonnumeric", quoteNonNumeric, `"a,b","c""d",7,""` + "\n"},
		{"strings", quoteStrings, `"a,b","c""d",7,` + "\n"},
		{"notnull", quoteNotNull, `"a,b","c""d","7",` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &csvTestOutput{}
			out.Init(csvTestOutputType)
			wrt, err := csvWriter([]objects.Object{out}, map[string]objects.Object{
				"quoting":        objects.NewInt(int64(tc.quoting)),
				"lineterminator": objects.NewStr("\n"),
			})
			if err != nil {
				t.Fatalf("csvWriter: %v", err)
			}
			w := wrt.(*Writer)
			if _, err := writerWriteRow(w, []objects.Object{row}, nil); err != nil {
				t.Fatalf("writerow: %v", err)
			}
			if got := out.buf.String(); got != tc.want {
				t.Fatalf("quoting=%s: got %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestWriterEscapeChar: with quoting=QUOTE_NONE every special char
// must be backslash-escaped.
func TestWriterEscapeChar(t *testing.T) {
	out := &csvTestOutput{}
	out.Init(csvTestOutputType)
	wrt, err := csvWriter([]objects.Object{out}, map[string]objects.Object{
		"quoting":        objects.NewInt(int64(quoteNone)),
		"escapechar":     objects.NewStr("\\"),
		"lineterminator": objects.NewStr("\n"),
	})
	if err != nil {
		t.Fatalf("csvWriter: %v", err)
	}
	w := wrt.(*Writer)
	row := objects.NewList([]objects.Object{
		objects.NewStr("a,b"),
		objects.NewStr("c\\d"),
	})
	if _, err := writerWriteRow(w, []objects.Object{row}, nil); err != nil {
		t.Fatalf("writerow: %v", err)
	}
	want := `a\,b,c\\d` + "\n"
	if got := out.buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestWriterRoundTrip: parsing the output of writerow yields the same
// row back for representative shapes.
func TestWriterRoundTrip(t *testing.T) {
	rows := [][]string{
		{"plain", "x", "y"},
		{"with,comma", "x", "y"},
		{`with "quote"`, "x", "y"},
		{"with\nnewline", "x", "y"},
		{"", "x", ""},
	}
	for _, fields := range rows {
		out := &csvTestOutput{}
		out.Init(csvTestOutputType)
		wrt, err := csvWriter([]objects.Object{out}, map[string]objects.Object{
			"lineterminator": objects.NewStr("\n"),
		})
		if err != nil {
			t.Fatalf("csvWriter: %v", err)
		}
		w := wrt.(*Writer)
		items := make([]objects.Object, len(fields))
		for i, f := range fields {
			items[i] = objects.NewStr(f)
		}
		row := objects.NewList(items)
		if _, err := writerWriteRow(w, []objects.Object{row}, nil); err != nil {
			t.Fatalf("writerow: %v", err)
		}
		round := readRows(t, out.buf.String(), nil)
		if len(round) != 1 {
			t.Fatalf("round trip: got %d rows, want 1 (wire=%q)", len(round), out.buf.String())
		}
		got := strRow(t, round[0])
		if strings.Join(got, "|") != strings.Join(fields, "|") {
			t.Fatalf("round trip: got %v, want %v (wire=%q)", got, fields, out.buf.String())
		}
	}
}
