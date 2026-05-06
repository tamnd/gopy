package objects

import (
	"strings"
	"testing"
)

func bytesEq(a *Bytes, want string) bool { return string(a.v) == want }

func TestBytesFindAndRFind(t *testing.T) {
	b := NewBytesFromString("ababcab")
	if got := b.Find([]byte("ab"), 0, bytesMaxIndex); got != 0 {
		t.Errorf("find ab = %d, want 0", got)
	}
	if got := b.Find([]byte("ab"), 1, bytesMaxIndex); got != 2 {
		t.Errorf("find ab from 1 = %d, want 2", got)
	}
	if got := b.RFind([]byte("ab"), 0, bytesMaxIndex); got != 5 {
		t.Errorf("rfind ab = %d, want 5", got)
	}
	if got := b.Find([]byte("zz"), 0, bytesMaxIndex); got != -1 {
		t.Errorf("find missing = %d, want -1", got)
	}
}

func TestBytesIndexAndRIndex(t *testing.T) {
	b := NewBytesFromString("hello")
	if i, err := b.Index([]byte("ll"), 0, bytesMaxIndex); err != nil || i != 2 {
		t.Errorf("index ll = (%d, %v), want (2, nil)", i, err)
	}
	if _, err := b.Index([]byte("zz"), 0, bytesMaxIndex); err == nil {
		t.Error("expected ValueError on missing")
	}
}

func TestBytesCount(t *testing.T) {
	b := NewBytesFromString("abababab")
	if got := b.Count([]byte("ab"), 0, bytesMaxIndex); got != 4 {
		t.Errorf("count ab = %d, want 4", got)
	}
	if got := b.Count([]byte("aba"), 0, bytesMaxIndex); got != 2 {
		t.Errorf("count aba = %d, want 2 (non-overlapping)", got)
	}
	if got := b.Count(nil, 0, bytesMaxIndex); got != len(b.v)+1 {
		t.Errorf("count empty = %d, want %d", got, len(b.v)+1)
	}
}

func TestBytesStartsAndEnds(t *testing.T) {
	b := NewBytesFromString("hello world")
	if !b.StartsWith([]byte("hello"), 0, bytesMaxIndex) {
		t.Error("startswith hello should be true")
	}
	if !b.StartsWith([]byte("world"), 6, bytesMaxIndex) {
		t.Error("startswith world from 6 should be true")
	}
	if !b.EndsWith([]byte("world"), 0, bytesMaxIndex) {
		t.Error("endswith world should be true")
	}
	if b.StartsWith([]byte("xx"), 0, bytesMaxIndex) {
		t.Error("startswith xx should be false")
	}
}

func TestBytesSplitOnSep(t *testing.T) {
	b := NewBytesFromString("a,b,c,d")
	parts, err := b.Split([]byte(","), -1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c", "d"}
	if len(parts) != len(want) {
		t.Fatalf("split len = %d, want %d", len(parts), len(want))
	}
	for i, p := range parts {
		if !bytesEq(p, want[i]) {
			t.Errorf("split[%d] = %q, want %q", i, p.v, want[i])
		}
	}
}

func TestBytesSplitMaxsplit(t *testing.T) {
	b := NewBytesFromString("a,b,c,d")
	parts, _ := b.Split([]byte(","), 2)
	want := []string{"a", "b", "c,d"}
	if len(parts) != 3 {
		t.Fatalf("maxsplit=2 len = %d, want 3", len(parts))
	}
	for i, p := range parts {
		if !bytesEq(p, want[i]) {
			t.Errorf("split[%d] = %q, want %q", i, p.v, want[i])
		}
	}
}

func TestBytesSplitWhitespace(t *testing.T) {
	b := NewBytesFromString("  a   b\tc\n d ")
	parts, _ := b.Split(nil, -1)
	want := []string{"a", "b", "c", "d"}
	if len(parts) != len(want) {
		t.Fatalf("ws split len = %d, want %d (%v)", len(parts), len(want), parts)
	}
	for i, p := range parts {
		if !bytesEq(p, want[i]) {
			t.Errorf("ws split[%d] = %q, want %q", i, p.v, want[i])
		}
	}
}

func TestBytesSplitEmptySepIsError(t *testing.T) {
	b := NewBytesFromString("abc")
	if _, err := b.Split([]byte{}, -1); err == nil {
		t.Error("split with empty sep should ValueError")
	}
}

func TestBytesRSplitMaxsplit(t *testing.T) {
	b := NewBytesFromString("a,b,c,d")
	parts, _ := b.RSplit([]byte(","), 2)
	want := []string{"a,b", "c", "d"}
	if len(parts) != 3 {
		t.Fatalf("rsplit len = %d, want 3 (%v)", len(parts), parts)
	}
	for i, p := range parts {
		if !bytesEq(p, want[i]) {
			t.Errorf("rsplit[%d] = %q, want %q", i, p.v, want[i])
		}
	}
}

func TestBytesRSplitWhitespace(t *testing.T) {
	b := NewBytesFromString("  a b c  ")
	parts, _ := b.RSplit(nil, 1)
	want := []string{"  a b", "c"}
	if len(parts) != 2 {
		t.Fatalf("rsplit ws len = %d, want 2 (%v)", len(parts), parts)
	}
	for i, p := range parts {
		if !bytesEq(p, want[i]) {
			t.Errorf("rsplit ws [%d] = %q, want %q", i, p.v, want[i])
		}
	}
}

func TestBytesSplitLines(t *testing.T) {
	b := NewBytesFromString("a\nb\r\nc\rd")
	parts := b.SplitLines(false)
	want := []string{"a", "b", "c", "d"}
	if len(parts) != len(want) {
		t.Fatalf("splitlines = %v", parts)
	}
	for i, p := range parts {
		if !bytesEq(p, want[i]) {
			t.Errorf("splitlines[%d] = %q, want %q", i, p.v, want[i])
		}
	}
}

func TestBytesSplitLinesKeepEnds(t *testing.T) {
	b := NewBytesFromString("a\nb\r\nc")
	parts := b.SplitLines(true)
	want := []string{"a\n", "b\r\n", "c"}
	if len(parts) != len(want) {
		t.Fatalf("splitlines keepends len = %d", len(parts))
	}
	for i, p := range parts {
		if !bytesEq(p, want[i]) {
			t.Errorf("keepends[%d] = %q, want %q", i, p.v, want[i])
		}
	}
}

func TestBytesPartition(t *testing.T) {
	b := NewBytesFromString("a-b-c")
	head, sep, tail, err := b.Partition([]byte("-"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEq(head, "a") || !bytesEq(sep, "-") || !bytesEq(tail, "b-c") {
		t.Errorf("partition = (%q, %q, %q)", head.v, sep.v, tail.v)
	}
	head, sep, tail, _ = b.Partition([]byte("zz"))
	if !bytesEq(head, "a-b-c") || !bytesEq(sep, "") || !bytesEq(tail, "") {
		t.Errorf("partition miss = (%q, %q, %q)", head.v, sep.v, tail.v)
	}
}

func TestBytesRPartition(t *testing.T) {
	b := NewBytesFromString("a-b-c")
	head, sep, tail, _ := b.RPartition([]byte("-"))
	if !bytesEq(head, "a-b") || !bytesEq(sep, "-") || !bytesEq(tail, "c") {
		t.Errorf("rpartition = (%q, %q, %q)", head.v, sep.v, tail.v)
	}
	head, sep, tail, _ = b.RPartition([]byte("zz"))
	if !bytesEq(head, "") || !bytesEq(sep, "") || !bytesEq(tail, "a-b-c") {
		t.Errorf("rpartition miss = (%q, %q, %q)", head.v, sep.v, tail.v)
	}
}

func TestBytesJoin(t *testing.T) {
	sep := NewBytesFromString(", ")
	items := []Object{NewBytesFromString("a"), NewBytesFromString("b"), NewBytesFromString("c")}
	got, err := sep.Join(items)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEq(got, "a, b, c") {
		t.Errorf("join = %q", got.v)
	}
}

func TestBytesJoinAcceptsByteArray(t *testing.T) {
	sep := NewBytesFromString("")
	got, err := sep.Join([]Object{NewBytes([]byte{1, 2}), NewByteArray([]byte{3, 4})})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.v) != "\x01\x02\x03\x04" {
		t.Errorf("join mixed = % x", got.v)
	}
}

func TestBytesJoinRejectsOther(t *testing.T) {
	sep := NewBytesFromString("")
	_, err := sep.Join([]Object{NewInt(1)})
	if err == nil {
		t.Error("join with non-bytes should TypeError")
	}
}

func TestBytesReplace(t *testing.T) {
	b := NewBytesFromString("aaaa")
	if got := b.Replace([]byte("a"), []byte("bb"), -1); !bytesEq(got, "bbbbbbbb") {
		t.Errorf("replace -1 = %q", got.v)
	}
	if got := b.Replace([]byte("a"), []byte("bb"), 2); !bytesEq(got, "bbbbaa") {
		t.Errorf("replace 2 = %q", got.v)
	}
}

func TestBytesStripDefaults(t *testing.T) {
	b := NewBytesFromString(" \t hello \r\n")
	if got := b.Strip(nil); !bytesEq(got, "hello") {
		t.Errorf("strip = %q", got.v)
	}
	if got := b.LStrip(nil); !bytesEq(got, "hello \r\n") {
		t.Errorf("lstrip = %q", got.v)
	}
	if got := b.RStrip(nil); !bytesEq(got, " \t hello") {
		t.Errorf("rstrip = %q", got.v)
	}
}

func TestBytesStripChars(t *testing.T) {
	b := NewBytesFromString("xxabcxx")
	if got := b.Strip([]byte("x")); !bytesEq(got, "abc") {
		t.Errorf("strip x = %q", got.v)
	}
}

func TestBytesTranslate(t *testing.T) {
	tbl, err := MakeBytesTrans([]byte("abc"), []byte("xyz"))
	if err != nil {
		t.Fatal(err)
	}
	b := NewBytesFromString("aabbcc")
	out, err := b.Translate(tbl.v, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEq(out, "xxyyzz") {
		t.Errorf("translate = %q", out.v)
	}
}

func TestBytesTranslateDelete(t *testing.T) {
	b := NewBytesFromString("hello")
	out, err := b.Translate(nil, []byte("l"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEq(out, "heo") {
		t.Errorf("translate delete l = %q", out.v)
	}
}

func TestBytesTranslateBadTable(t *testing.T) {
	b := NewBytesFromString("a")
	if _, err := b.Translate([]byte{1, 2, 3}, nil); err == nil {
		t.Error("expected ValueError for non-256-byte table")
	}
}

func TestBytesMakeTransLengthMismatch(t *testing.T) {
	if _, err := MakeBytesTrans([]byte("ab"), []byte("xyz")); err == nil {
		t.Error("expected ValueError on mismatched lengths")
	}
}

func TestBytesExpandTabs(t *testing.T) {
	b := NewBytesFromString("a\tbc\tdef\nx\ty")
	out := b.ExpandTabs(4)
	want := "a   bc  def\nx   y"
	if !bytesEq(out, want) {
		t.Errorf("expandtabs = %q, want %q", out.v, want)
	}
}

func TestBytesCenter(t *testing.T) {
	b := NewBytesFromString("abc")
	if got := b.Center(7, '*'); !bytesEq(got, "**abc**") {
		t.Errorf("center 7 = %q", got.v)
	}
	if got := b.Center(2, '*'); !bytesEq(got, "abc") {
		t.Errorf("center too small = %q", got.v)
	}
}

func TestBytesLJustRJust(t *testing.T) {
	b := NewBytesFromString("abc")
	if got := b.LJust(6, '.'); !bytesEq(got, "abc...") {
		t.Errorf("ljust = %q", got.v)
	}
	if got := b.RJust(6, '.'); !bytesEq(got, "...abc") {
		t.Errorf("rjust = %q", got.v)
	}
}

func TestBytesZFill(t *testing.T) {
	if got := NewBytesFromString("42").ZFill(5); !bytesEq(got, "00042") {
		t.Errorf("zfill 42 = %q", got.v)
	}
	if got := NewBytesFromString("-42").ZFill(5); !bytesEq(got, "-0042") {
		t.Errorf("zfill -42 = %q", got.v)
	}
	if got := NewBytesFromString("+1").ZFill(4); !bytesEq(got, "+001") {
		t.Errorf("zfill +1 = %q", got.v)
	}
	if got := NewBytesFromString("hello").ZFill(3); !bytesEq(got, "hello") {
		t.Errorf("zfill no-op = %q", got.v)
	}
}

func TestBytesHexNoSep(t *testing.T) {
	b := NewBytes([]byte{0xb9, 0x01, 0xef})
	if got := b.Hex(0, 0); got != "b901ef" {
		t.Errorf("hex = %q", got)
	}
}

func TestBytesHexWithSep(t *testing.T) {
	b := NewBytes([]byte{0xb9, 0x01, 0xef})
	if got := b.Hex(':', 1); got != "b9:01:ef" {
		t.Errorf("hex sep=':' bps=1 = %q", got)
	}
	if got := b.Hex(':', 2); got != "b9:01ef" {
		t.Errorf("hex bps=2 = %q (want b9:01ef)", got)
	}
	if got := b.Hex(':', -2); got != "b901:ef" {
		t.Errorf("hex bps=-2 = %q (want b901:ef)", got)
	}
}

func TestBytesFromHex(t *testing.T) {
	b, err := BytesFromHex("B9 01EF")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xb9, 0x01, 0xef}
	if string(b.v) != string(want) {
		t.Errorf("fromhex = % x, want % x", b.v, want)
	}
}

func TestBytesFromHexErrorOnOddLen(t *testing.T) {
	_, err := BytesFromHex("abc")
	if err == nil || !strings.Contains(err.Error(), "even number") {
		t.Errorf("expected even-number error, got %v", err)
	}
}

func TestBytesFromHexErrorOnInvalid(t *testing.T) {
	_, err := BytesFromHex("zz")
	if err == nil || !strings.Contains(err.Error(), "non-hexadecimal") {
		t.Errorf("expected non-hexadecimal error, got %v", err)
	}
}

func TestBytesFindNegativeIndex(t *testing.T) {
	b := NewBytesFromString("abcabc")
	if got := b.Find([]byte("a"), -3, bytesMaxIndex); got != 3 {
		t.Errorf("find with negative start: %d, want 3", got)
	}
}

func TestBytesCountWithBounds(t *testing.T) {
	b := NewBytesFromString("aaaa")
	if got := b.Count([]byte("a"), 1, 3); got != 2 {
		t.Errorf("count [1:3] = %d, want 2", got)
	}
}
