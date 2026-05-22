// Tests for the _tokenize wrapper. The fixtures come straight from
// CPython 3.14.5 via:
//
//	import _tokenize, io
//	it = _tokenize.TokenizerIter(io.BytesIO(src.encode()).readline,
//	                             extra_tokens=True, encoding='utf-8')
//	for tup in it: print(tup)
//
// so the table doubles as the gate against the wrapper drifting.

package _tokenize

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser/lexer"
	"github.com/tamnd/gopy/token"
)

// newIter builds a tokenizerIter from a source string for direct
// calls into tokenizerIterNext. Mirrors tokenizerIterNew but skips
// the readline plumbing so the test can drive the wrapper directly.
func newIter(src string, extraTokens bool) *tokenizerIter {
	st := lexer.FromBytes([]byte(src), lexer.ModeFile)
	st.SetFilename("<test>")
	if extraTokens {
		st.SetExtraTokens(true)
	}
	lines := []string{""} // 1-based padding
	for _, line := range strings.SplitAfter(src, "\n") {
		if line == "" {
			continue
		}
		trim := strings.TrimSuffix(line, "\n")
		trim = strings.TrimSuffix(trim, "\r")
		lines = append(lines, trim)
	}
	return &tokenizerIter{
		tok:             st,
		extraTokens:     extraTokens,
		linesByOneBased: lines,
	}
}

// tup captures the 4-int header of a tokenize tuple.
type tup struct {
	kind  token.Type
	sLine int
	sCol  int
	eLine int
	eCol  int
	str   string
}

func (t tup) String() string {
	return fmt.Sprintf("%s %q (%d,%d)-(%d,%d)", t.kind, t.str, t.sLine, t.sCol, t.eLine, t.eCol)
}

func intOf(o objects.Object) int {
	v, _ := o.(*objects.Int).Int64()
	return int(v)
}

func decode(o objects.Object) tup {
	tu := o.(*objects.Tuple)
	str := tu.Item(1).(*objects.Unicode).Value()
	startT := tu.Item(2).(*objects.Tuple)
	endT := tu.Item(3).(*objects.Tuple)
	return tup{
		kind:  token.Type(intOf(tu.Item(0))),
		sLine: intOf(startT.Item(0)),
		sCol:  intOf(startT.Item(1)),
		eLine: intOf(endT.Item(0)),
		eCol:  intOf(endT.Item(1)),
		str:   str,
	}
}

// TestWrapperPositionsMatchCPythonExtraTokens pins the wrapper's
// extra_tokens=True output against CPython 3.14.5 raw fixtures. The
// wrapper applies:
//   - +1 on NEWLINE end_col (so "1 + 1\n" NEWLINE end_col 5 -> 6)
//   - (lineno+1, 0) reshape on ENDMARKER and DEDENT-at-EOF
//   - OP collapse for operator tokens (LPAR/PLUS/... -> OP/55)
//   - "" -> "\n" on NEWLINE str when implicit_newline is off
//
// CPython: Python/Python-tokenize.c:281-336 tokenizeriter_next
// (extra_tokens branch).
func TestWrapperPositionsMatchCPythonExtraTokens(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []tup
	}{
		{
			name: "simple_expr",
			src:  "1 + 1\n",
			want: []tup{
				{token.NUMBER, 1, 0, 1, 1, "1"},
				{token.OP, 1, 2, 1, 3, "+"},
				{token.NUMBER, 1, 4, 1, 5, "1"},
				{token.NEWLINE, 1, 5, 1, 6, "\n"},
				{token.ENDMARKER, 2, 0, 2, 0, ""},
			},
		},
		{
			name: "def_pass",
			src:  "def f():\n    pass\n",
			want: []tup{
				{token.NAME, 1, 0, 1, 3, "def"},
				{token.NAME, 1, 4, 1, 5, "f"},
				{token.OP, 1, 5, 1, 6, "("},
				{token.OP, 1, 6, 1, 7, ")"},
				{token.OP, 1, 7, 1, 8, ":"},
				{token.NEWLINE, 1, 8, 1, 9, "\n"},
				{token.INDENT, 2, 0, 2, 4, "    "},
				{token.NAME, 2, 4, 2, 8, "pass"},
				{token.NEWLINE, 2, 8, 2, 9, "\n"},
				{token.DEDENT, 3, 0, 3, 0, ""},
				{token.ENDMARKER, 3, 0, 3, 0, ""},
			},
		},
		{
			name: "comment_only",
			src:  "# only\n",
			want: []tup{
				{token.COMMENT, 1, 0, 1, 6, "# only"},
				{token.NL, 1, 6, 1, 7, "\n"},
				{token.ENDMARKER, 2, 0, 2, 0, ""},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it := newIter(tc.src, true)
			var got []tup
			for range 50 {
				o, err := tokenizerIterNext(it)
				if err != nil {
					if errors.Is(err, objects.ErrStopIteration) {
						break
					}
					t.Fatalf("iter error: %v", err)
				}
				got = append(got, decode(o))
				if got[len(got)-1].kind == token.ENDMARKER {
					break
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("token count = %d, want %d\n got=%v\nwant=%v", len(got), len(tc.want), got, tc.want)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("token %d:\n  got  %s\n  want %s", i, got[i], w)
				}
			}
		})
	}
}
