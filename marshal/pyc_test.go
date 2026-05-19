package marshal

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func newTestCode() *objects.Code {
	c := objects.NewCode()
	c.Name = "test"
	c.Filename = "test.py"
	c.Code = []byte{0x64, 0x00, 0x53, 0x00}
	c.Consts = []any{nil}
	return c
}

// TestWriteReadPycTimestamp pins the round-trip through WritePyc /
// ReadPyc for the timestamp-based header variant.
func TestWriteReadPycTimestamp(t *testing.T) {
	code := newTestCode()
	var mtime uint32 = 1700000000
	var sourceSize uint32 = 512

	var buf bytes.Buffer
	if err := WritePyc(&buf, code, mtime, sourceSize); err != nil {
		t.Fatalf("WritePyc: %v", err)
	}
	if buf.Len() < 16 {
		t.Fatalf("output too short: %d bytes", buf.Len())
	}

	got, hdr, err := ReadPyc(&buf)
	if err != nil {
		t.Fatalf("ReadPyc: %v", err)
	}
	if hdr.Flags != 0 {
		t.Errorf("Flags = %d, want 0", hdr.Flags)
	}
	if hdr.Mtime != mtime {
		t.Errorf("Mtime = %d, want %d", hdr.Mtime, mtime)
	}
	if hdr.SourceSize != sourceSize {
		t.Errorf("SourceSize = %d, want %d", hdr.SourceSize, sourceSize)
	}
	if got.Name != code.Name {
		t.Errorf("code.Name = %q, want %q", got.Name, code.Name)
	}
}

// TestWriteReadPycHash pins the round-trip through WritePycHash /
// ReadPyc for the unchecked-hash header variant.
func TestWriteReadPycHash(t *testing.T) {
	code := newTestCode()
	var sourceHash uint64 = 0xDEADBEEFCAFEBABE

	var buf bytes.Buffer
	if err := WritePycHash(&buf, code, pycFlagUncheckedHash, sourceHash); err != nil {
		t.Fatalf("WritePycHash: %v", err)
	}

	got, hdr, err := ReadPyc(&buf)
	if err != nil {
		t.Fatalf("ReadPyc: %v", err)
	}
	if hdr.Flags != pycFlagUncheckedHash {
		t.Errorf("Flags = %d, want %d", hdr.Flags, pycFlagUncheckedHash)
	}
	if hdr.SourceHash != sourceHash {
		t.Errorf("SourceHash = 0x%x, want 0x%x", hdr.SourceHash, sourceHash)
	}
	if got.Name != code.Name {
		t.Errorf("code.Name = %q, want %q", got.Name, code.Name)
	}
}

// TestReadPycBadMagic pins that a wrong magic number surfaces ErrBadMagic.
func TestReadPycBadMagic(t *testing.T) {
	var buf bytes.Buffer
	// Write header with wrong magic
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], 0xDEADBEEF)
	buf.Write(hdr)

	_, _, err := ReadPyc(&buf)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("magic")) {
		t.Errorf("error %q does not mention magic", err)
	}
}

// TestMagicNumberFormat pins that MagicNumber writes b'\r\n' at bytes
// 2 and 3 of the on-disk header, matching CPython's
// text-transfer-detection convention. The check goes through the same
// little-endian PutUint32 path WritePyc uses, so it catches the
// uint16-vs-byte-order confusion that shipped on the original Phase 1
// commit.
//
// CPython: Lib/importlib/_bootstrap_external.py:L246 MAGIC_NUMBER
func TestMagicNumberFormat(t *testing.T) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], MagicNumber)
	want := [4]byte{0x2b, 0x0e, '\r', '\n'}
	if buf != want {
		t.Errorf("MagicNumber bytes = % x, want % x", buf[:], want[:])
	}
}

// TestWritePycHashBadFlags pins that WritePycHash rejects flags other
// than 1 and 2.
func TestWritePycHashBadFlags(t *testing.T) {
	var buf bytes.Buffer
	err := WritePycHash(&buf, newTestCode(), 0, 0)
	if err == nil {
		t.Fatal("expected error for flags=0")
	}
}
