// .pyc file header read/write. The header is exactly 16 bytes for both
// timestamp-based and hash-based variants (PEP 552). Layout:
//
//	[0:4]  magic number  — 2-byte little-endian magic + b'\r\n'
//	[4:8]  flags         — 0=timestamp, 1=checked-hash, 2=unchecked-hash
//	[8:16] payload       — mtime(4) + source_size(4) when flags==0,
//	                       source_hash(8) when flags & 1
//	[16:]  marshal.Dump(code)
//
// CPython: Lib/importlib/_bootstrap_external.py:L756 _validate_bytecode_header
// CPython: Lib/importlib/_bootstrap_external.py:L813 _compile_bytecode
package marshal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/tamnd/gopy/objects"
)

// MagicNumber is the gopy .pyc wire-format discriminator. The low two
// bytes are a little-endian magic tag; the high two bytes are always
// \r\n so that text-mode file transfers are detectable. The numeric
// tag (3627) tracks CPython 3.14's PYC_MAGIC_NUMBER one-for-one; bump
// this constant in lockstep with the header any time CPython does.
//
// CPython: Include/internal/pycore_magic_number.h:295 PYC_MAGIC_NUMBER
// CPython: Lib/importlib/_bootstrap_external.py:222 MAGIC_NUMBER
const MagicNumber uint32 = (3627 | (0x0D0A << 16))

// pyc flags for the bit-field at bytes [4:8].
//
// CPython: Lib/importlib/_bootstrap_external.py:L248 _RAW_MAGIC_NUMBER
const (
	pycFlagTimestamp     = 0 // mtime + source_size (default)
	pycFlagCheckedHash   = 1 // checked hash
	pycFlagUncheckedHash = 2 // unchecked hash
)

// ErrBadMagic is returned by ReadPyc when the magic number does not
// match MagicNumber.
var ErrBadMagic = errors.New("marshal: .pyc magic number mismatch")

// PycHeader holds the decoded header of a .pyc file.
//
// CPython: Lib/importlib/_bootstrap_external.py:L756 _validate_bytecode_header
type PycHeader struct {
	// Flags is the bit-field at bytes [4:8]. 0 = timestamp mode.
	Flags uint32
	// Mtime is the source-file modification time (timestamp mode only).
	Mtime uint32
	// SourceSize is the source-file byte count (timestamp mode only).
	SourceSize uint32
	// SourceHash is the 8-byte SipHash of the source (hash mode only).
	SourceHash uint64
}

// WritePyc writes a timestamp-based .pyc file to w. The 16-byte header
// is followed by the code object serialized with Dump.
//
// CPython: Lib/importlib/_bootstrap_external.py:L813 _compile_bytecode
func WritePyc(w io.Writer, code *objects.Code, mtime, sourceSize uint32) error {
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], MagicNumber)
	binary.LittleEndian.PutUint32(hdr[4:], pycFlagTimestamp)
	binary.LittleEndian.PutUint32(hdr[8:], mtime)
	binary.LittleEndian.PutUint32(hdr[12:], sourceSize)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	return Dump(w, code)
}

// WritePycHash writes a hash-based .pyc file. flags must be
// pycFlagCheckedHash or pycFlagUncheckedHash; sourceHash is the
// 8-byte SipHash of the source text.
//
// CPython: Lib/importlib/_bootstrap_external.py:L813 _compile_bytecode (hash branch)
func WritePycHash(w io.Writer, code *objects.Code, flags uint32, sourceHash uint64) error {
	if flags != pycFlagCheckedHash && flags != pycFlagUncheckedHash {
		return fmt.Errorf("marshal: WritePycHash: flags must be 1 or 2, got %d", flags)
	}
	hdr := make([]byte, 16)
	binary.LittleEndian.PutUint32(hdr[0:], MagicNumber)
	binary.LittleEndian.PutUint32(hdr[4:], flags)
	binary.LittleEndian.PutUint64(hdr[8:], sourceHash)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	return Dump(w, code)
}

// ReadPyc reads a .pyc file from r. It validates the magic number,
// decodes the header, and returns the code object together with the
// decoded PycHeader.
//
// CPython: Lib/importlib/_bootstrap_external.py:L756 _validate_bytecode_header
func ReadPyc(r io.Reader) (*objects.Code, PycHeader, error) {
	var raw [16]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return nil, PycHeader{}, fmt.Errorf("marshal: ReadPyc: header: %w", err)
	}
	magic := binary.LittleEndian.Uint32(raw[0:])
	if magic != MagicNumber {
		return nil, PycHeader{}, fmt.Errorf("%w: got 0x%08x, want 0x%08x", ErrBadMagic, magic, MagicNumber)
	}
	flags := binary.LittleEndian.Uint32(raw[4:])
	var hdr PycHeader
	hdr.Flags = flags
	if flags == pycFlagTimestamp {
		hdr.Mtime = binary.LittleEndian.Uint32(raw[8:])
		hdr.SourceSize = binary.LittleEndian.Uint32(raw[12:])
	} else {
		hdr.SourceHash = binary.LittleEndian.Uint64(raw[8:])
	}
	v, err := Load(r)
	if err != nil {
		return nil, hdr, fmt.Errorf("marshal: ReadPyc: body: %w", err)
	}
	code, ok := v.(*objects.Code)
	if !ok {
		return nil, hdr, fmt.Errorf("marshal: ReadPyc: expected code object, got %T", v)
	}
	return code, hdr, nil
}
