package _pickle

import (
	"encoding/hex"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestContainerByteEquality pins the Phase 3 container savers against
// the pickle.dumps(value, protocol=5) byte stream CPython emits.
// Covers: empty / non-empty list, empty tuple (special-cased to
// EMPTY_TUPLE with no MEMOIZE), 1/2/3-element TUPLE1/2/3, >3-element
// MARK + items + TUPLE, empty / non-empty dict with SETITEMS, empty
// / non-empty set with ADDITEMS, empty / non-empty frozenset.
//
// All fixtures captured from CPython 3.14 via
// `python3 -c "import pickle; print(pickle.dumps(value, 5).hex())"`.
func TestContainerByteEquality(t *testing.T) {
	mustFrozen := func(items []objects.Object) objects.Object {
		fs, err := objects.NewFrozenset(items)
		if err != nil {
			t.Fatalf("NewFrozenset: %v", err)
		}
		return fs
	}
	mustSet := func(items []objects.Object) objects.Object {
		s := objects.NewSet()
		for _, item := range items {
			if err := s.Add(item); err != nil {
				t.Fatalf("Set.Add: %v", err)
			}
		}
		return s
	}
	mustDict := func(pairs [][2]objects.Object) objects.Object {
		d := objects.NewDict()
		for _, kv := range pairs {
			if err := d.SetItem(kv[0], kv[1]); err != nil {
				t.Fatalf("Dict.SetItem: %v", err)
			}
		}
		return d
	}

	cases := []struct {
		name string
		obj  objects.Object
		hex  string
	}{
		{"list_empty", objects.NewList(nil), "80055d942e"},
		{"list_123", objects.NewList([]objects.Object{
			objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
		}), "8005950b000000000000005d94284b014b024b03652e"},

		{"tuple_empty", objects.NewTuple(nil), "8005292e"},
		{"tuple_1", objects.NewTuple([]objects.Object{objects.NewInt(1)}),
			"80059505000000000000004b0185942e"},
		{"tuple_2", objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)}),
			"80059507000000000000004b014b0286942e"},
		{"tuple_3", objects.NewTuple([]objects.Object{
			objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
		}), "80059509000000000000004b014b024b0387942e"},
		{"tuple_4", objects.NewTuple([]objects.Object{
			objects.NewInt(1), objects.NewInt(2), objects.NewInt(3), objects.NewInt(4),
		}), "8005950c00000000000000284b014b024b034b0474942e"},

		{"dict_empty", objects.NewDict(), "80057d942e"},
		{"dict_ab", mustDict([][2]objects.Object{
			{objects.NewStr("a"), objects.NewInt(1)},
			{objects.NewStr("b"), objects.NewInt(2)},
		}), "80059511000000000000007d94288c0161944b018c0162944b02752e"},

		{"set_empty", mustSet(nil), "80058f942e"},
		{"set_123", mustSet([]objects.Object{
			objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
		}), "8005950b000000000000008f94284b014b024b03902e"},

		{"frozenset_empty", mustFrozen(nil), "80059504000000000000002891942e"},
		{"frozenset_123", mustFrozen([]objects.Object{
			objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
		}), "8005950a00000000000000284b014b024b0391942e"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := dumpsAtom(c.obj, 5)
			if err != nil {
				t.Fatalf("dumpsAtom: %v", err)
			}
			want, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("hex decode: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("byte mismatch\n got: %s\nwant: %s", hex.EncodeToString(got), c.hex)
			}
		})
	}
}

// TestNestedContainers exercises recursive save() dispatch for
// nested containers and mixed-type lists. All fixtures from CPython
// 3.14.
func TestNestedContainers(t *testing.T) {
	mk := func(v ...objects.Object) []objects.Object {
		return v
	}
	mustDict := func(pairs [][2]objects.Object) objects.Object {
		d := objects.NewDict()
		for _, kv := range pairs {
			if err := d.SetItem(kv[0], kv[1]); err != nil {
				t.Fatalf("Dict.SetItem: %v", err)
			}
		}
		return d
	}

	cases := []struct {
		name string
		obj  objects.Object
		hex  string
	}{
		{"list_of_lists", objects.NewList(mk(
			objects.NewList(mk(objects.NewInt(1), objects.NewInt(2))),
			objects.NewList(mk(objects.NewInt(3), objects.NewInt(4))),
		)), "80059515000000000000005d94285d94284b014b02655d94284b034b0465652e"},

		{"dict_of_tuples", mustDict([][2]objects.Object{
			{objects.NewStr("a"), objects.NewTuple(mk(objects.NewInt(1), objects.NewInt(2)))},
			{objects.NewStr("b"), objects.NewTuple(mk(objects.NewInt(3), objects.NewInt(4)))},
		}), "80059519000000000000007d94288c0161944b014b0286948c0162944b034b048694752e"},

		{"list_with_strs", objects.NewList(mk(
			objects.NewStr("a"), objects.NewStr("b"), objects.NewStr("c"),
		)), "80059511000000000000005d94288c0161948c0162948c016394652e"},

		{"mixed", objects.NewList(mk(
			objects.NewInt(1),
			objects.NewStr("a"),
			objects.None(),
			objects.True(),
			objects.NewFloat(3.14),
		)), "80059516000000000000005d94284b018c0161944e884740091eb851eb851f652e"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := dumpsAtom(c.obj, 5)
			if err != nil {
				t.Fatalf("dumpsAtom: %v", err)
			}
			want, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("hex decode: %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("byte mismatch\n got: %s\nwant: %s", hex.EncodeToString(got), c.hex)
			}
		})
	}
}
