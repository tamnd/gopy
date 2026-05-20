package _pickle

import (
	"encoding/hex"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestPickleDumpsModuleEntry exercises the Python-callable
// `_pickle.dumps` shim and confirms it routes into the proto-5
// encoder. Same fixtures the byte-equality gate uses, but driven via
// the args/kwargs surface a caller from Python would hit.
func TestPickleDumpsModuleEntry(t *testing.T) {
	cases := []struct {
		name string
		args []objects.Object
		kw   map[string]objects.Object
		hex  string
	}{
		{
			name: "default_proto_none",
			args: []objects.Object{objects.None()},
			hex:  "80054e2e",
		},
		{
			name: "explicit_proto_positional",
			args: []objects.Object{objects.NewInt(1), objects.NewInt(5)},
			hex:  "80054b012e",
		},
		{
			name: "explicit_proto_kw",
			args: []objects.Object{objects.NewInt(1)},
			kw:   map[string]objects.Object{"protocol": objects.NewInt(5)},
			hex:  "80054b012e",
		},
		{
			name: "protocol_negative_picks_highest",
			args: []objects.Object{objects.NewInt(1), objects.NewInt(-1)},
			hex:  "80054b012e",
		},
		{
			name: "list_with_strs",
			args: []objects.Object{
				objects.NewList([]objects.Object{
					objects.NewStr("a"), objects.NewStr("b"), objects.NewStr("c"),
				}),
			},
			hex: "80059511000000000000005d94288c0161948c0162948c016394652e",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pickleDumps(c.args, c.kw)
			if err != nil {
				t.Fatalf("pickleDumps: %v", err)
			}
			b, ok := got.(*objects.Bytes)
			if !ok {
				t.Fatalf("pickleDumps: want *objects.Bytes, got %T", got)
			}
			want, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("hex decode: %v", err)
			}
			if string(b.Bytes()) != string(want) {
				t.Fatalf("byte mismatch\n got: %s\nwant: %s",
					hex.EncodeToString(b.Bytes()), c.hex)
			}
		})
	}
}

// TestPickleDumpsErrors checks the error paths: too many positionals,
// unknown kwargs, protocol both by name and position, protocol > max.
func TestPickleDumpsErrors(t *testing.T) {
	if _, err := pickleDumps(nil, nil); err == nil {
		t.Fatal("dumps() with no args: want error")
	}
	if _, err := pickleDumps([]objects.Object{
		objects.NewInt(1), objects.NewInt(5), objects.NewInt(3),
	}, nil); err == nil {
		t.Fatal("dumps() with 3 positionals: want error")
	}
	if _, err := pickleDumps(
		[]objects.Object{objects.NewInt(1), objects.NewInt(5)},
		map[string]objects.Object{"protocol": objects.NewInt(5)},
	); err == nil {
		t.Fatal("dumps() protocol given twice: want error")
	}
	if _, err := pickleDumps(
		[]objects.Object{objects.NewInt(1), objects.NewInt(99)}, nil,
	); err == nil {
		t.Fatal("dumps() protocol > HIGHEST_PROTOCOL: want error")
	}
	if _, err := pickleDumps(
		[]objects.Object{objects.NewInt(1)},
		map[string]objects.Object{"bogus": objects.None()},
	); err == nil {
		t.Fatal("dumps() unknown kwarg: want error")
	}
}
