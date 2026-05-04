package symtable

import "testing"

func TestMangleBasic(t *testing.T) {
	cases := []struct {
		private string
		name    string
		want    string
	}{
		{"", "__x", "__x"},
		{"C", "__x", "_C__x"},
		{"_C", "__x", "_C__x"},
		{"__C", "__x", "_C__x"},
		{"C", "x", "x"},
		{"C", "__x__", "__x__"}, // dunder pass-through
		{"C", "__x.y", "__x.y"}, // dotted pass-through
		{"_", "__x", "__x"},     // private="_" strips to "" -> no mangle
		{"___", "__x", "__x"},   // all-underscore private -> no mangle
		{"C", "_x", "_x"},       // single leading underscore -> no mangle
		{"C", "x__", "x__"},     // not double-leading -> no mangle
	}
	for _, tc := range cases {
		got := Mangle(tc.private, tc.name)
		if got != tc.want {
			t.Errorf("Mangle(%q, %q) = %q, want %q", tc.private, tc.name, got, tc.want)
		}
	}
}

func TestMaybeMangleAllowList(t *testing.T) {
	ste := &Entry{MangledNames: map[string]struct{}{"__t": {}}}
	if got := MaybeMangle("C", ste, "__t"); got != "_C__t" {
		t.Errorf("MaybeMangle in allow-list = %q, want _C__t", got)
	}
	if got := MaybeMangle("C", ste, "__u"); got != "__u" {
		t.Errorf("MaybeMangle outside allow-list = %q, want __u", got)
	}
	if got := MaybeMangle("C", nil, "__t"); got != "_C__t" {
		t.Errorf("MaybeMangle nil ste = %q, want _C__t", got)
	}
}
