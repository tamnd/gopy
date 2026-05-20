package _pickle

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// fakeFile is a minimal file-like exposing read(n) and write(b) so the
// Pickler / Unpickler tests can drive .dump() / .load() without
// importing io.BytesIO from another package.
type fakeFile struct {
	objects.Header
	buf *bytes.Buffer
}

var fakeFileType = func() *objects.Type {
	t := objects.NewType("fakeFile", []*objects.Type{objects.ObjectType()})
	t.Getattro = fakeFileGetattr
	return t
}()

func newFakeFile() *fakeFile {
	f := &fakeFile{buf: new(bytes.Buffer)}
	f.Init(fakeFileType)
	return f
}

func fakeFileGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	f := o.(*fakeFile)
	n := name.(*objects.Unicode).Value()
	switch n {
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			b := args[0].(*objects.Bytes).Bytes()
			f.buf.Write(b)
			return objects.NewInt(int64(len(b))), nil
		}), nil
	case "read":
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			n := -1
			if len(args) >= 1 {
				v, _ := args[0].(*objects.Int).Int64()
				n = int(v)
			}
			if n < 0 || n >= f.buf.Len() {
				out := f.buf.Bytes()
				f.buf.Reset()
				return objects.NewBytes(out), nil
			}
			out := make([]byte, n)
			f.buf.Read(out)
			return objects.NewBytes(out), nil
		}), nil
	}
	return nil, fmt.Errorf("AttributeError: %q", n)
}

// TestPicklerType_RoundTrip verifies Pickler(file).dump(obj) lands the
// proto-5 stream onto file and Unpickler(file).load() recovers obj.
func TestPicklerType_RoundTrip(t *testing.T) {
	values := []objects.Object{
		objects.None(),
		objects.True(),
		objects.NewInt(42),
		objects.NewStr("hello"),
		objects.NewBytes([]byte{1, 2, 3}),
		objects.NewList([]objects.Object{
			objects.NewInt(1), objects.NewInt(2), objects.NewInt(3),
		}),
	}
	for _, in := range values {
		t.Run(fmt.Sprintf("%T", in), func(t *testing.T) {
			f := newFakeFile()
			p, err := picklerTpNew(picklerType, []objects.Object{f}, nil)
			if err != nil {
				t.Fatalf("Pickler(): %v", err)
			}
			dump, err := objects.GetAttr(p, objects.NewStr("dump"))
			if err != nil {
				t.Fatalf("Pickler.dump getattr: %v", err)
			}
			if _, err := objects.CallOneArg(dump, in); err != nil {
				t.Fatalf("Pickler.dump(): %v", err)
			}

			u, err := unpicklerTpNew(unpicklerType, []objects.Object{f}, nil)
			if err != nil {
				t.Fatalf("Unpickler(): %v", err)
			}
			load, err := objects.GetAttr(u, objects.NewStr("load"))
			if err != nil {
				t.Fatalf("Unpickler.load getattr: %v", err)
			}
			got, err := objects.Call(load, objects.NewTuple(nil), nil)
			if err != nil {
				t.Fatalf("Unpickler.load(): %v", err)
			}
			if !objectsEqual(got, in) {
				t.Fatalf("round trip mismatch\n got: %v\nwant: %v", got, in)
			}
		})
	}
}

// TestPicklerType_Bytes pins the exact wire form produced by
// Pickler(file).dump against the same proto-5 hex the encoder gate
// uses, so the class-driven path stays byte-identical to dumps().
func TestPicklerType_Bytes(t *testing.T) {
	f := newFakeFile()
	p, err := picklerTpNew(picklerType, []objects.Object{f}, nil)
	if err != nil {
		t.Fatalf("Pickler(): %v", err)
	}
	dump, _ := objects.GetAttr(p, objects.NewStr("dump"))
	if _, err := objects.CallOneArg(dump, objects.NewInt(1)); err != nil {
		t.Fatalf("dump: %v", err)
	}
	want, _ := hex.DecodeString("80054b012e")
	if !bytes.Equal(f.buf.Bytes(), want) {
		t.Fatalf("byte mismatch\n got: %s\nwant: %s",
			hex.EncodeToString(f.buf.Bytes()), hex.EncodeToString(want))
	}
}

// TestPicklerType_ProtocolKW exercises both positional and keyword
// protocol args, plus the negative-picks-highest rule.
func TestPicklerType_ProtocolKW(t *testing.T) {
	f := newFakeFile()
	p, err := picklerTpNew(picklerType, []objects.Object{f, objects.NewInt(-1)}, nil)
	if err != nil {
		t.Fatalf("Pickler(neg): %v", err)
	}
	if p.(*Pickler).proto != highestProtocol {
		t.Fatalf("proto: got %d, want %d", p.(*Pickler).proto, highestProtocol)
	}
	if _, err := picklerTpNew(picklerType,
		[]objects.Object{f},
		map[string]objects.Object{"protocol": objects.NewInt(5)},
	); err != nil {
		t.Fatalf("Pickler(proto kw): %v", err)
	}
	if _, err := picklerTpNew(picklerType,
		[]objects.Object{f, objects.NewInt(99)}, nil,
	); err == nil {
		t.Fatal("Pickler(proto=99): want ValueError")
	}
}

// TestPicklerType_Errors covers the constructor argument checks.
func TestPicklerType_Errors(t *testing.T) {
	if _, err := picklerTpNew(picklerType, nil, nil); err == nil {
		t.Fatal("Pickler(): want TypeError")
	}
	if _, err := picklerTpNew(picklerType,
		[]objects.Object{objects.NewInt(1)}, nil,
	); err == nil {
		t.Fatal("Pickler(non-file): want TypeError")
	}
	if _, err := unpicklerTpNew(unpicklerType, nil, nil); err == nil {
		t.Fatal("Unpickler(): want TypeError")
	}
	if _, err := unpicklerTpNew(unpicklerType,
		[]objects.Object{objects.NewInt(1)}, nil,
	); err == nil {
		t.Fatal("Unpickler(non-file): want TypeError")
	}
}

// TestModuleExposesPicklerUnpickler verifies the C-import block in
// pickle.py would resolve: Pickler, Unpickler, dump, dumps, load,
// loads, PickleError, PicklingError, UnpicklingError all present.
func TestModuleExposesPicklerUnpickler(t *testing.T) {
	m, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	d := m.Dict()
	for _, name := range []string{
		"Pickler", "Unpickler", "dump", "dumps", "load", "loads",
		"PickleError", "PicklingError", "UnpicklingError",
		"HIGHEST_PROTOCOL", "DEFAULT_PROTOCOL",
	} {
		if _, err := d.GetItem(objects.NewStr(name)); err != nil {
			t.Fatalf("missing module attr %q: %v", name, err)
		}
	}
}
