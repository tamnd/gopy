package pystrconv_test

import (
	"testing"

	"github.com/tamnd/gopy/pystrconv"
)

func TestHexBasic(t *testing.T) {
	if got := pystrconv.Hex([]byte{0x01, 0x02, 0xab}); got != "0102ab" {
		t.Fatalf("got %q", got)
	}
	if got := pystrconv.Hex(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestHexWithSepRightGrouped(t *testing.T) {
	// CPython: bytes.fromhex('aabbccddeeff').hex(':', 2) -> 'aabb:ccdd:eeff'
	got := pystrconv.HexWithSep([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, ':', 2)
	if got != "aabb:ccdd:eeff" {
		t.Fatalf("got %q", got)
	}
}

func TestHexWithSepLeftGrouped(t *testing.T) {
	// CPython: b'\xaa\xbb\xcc\xdd\xee'.hex(':', -2) -> 'aabb:ccdd:ee'
	got := pystrconv.HexWithSep([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee}, ':', -2)
	if got != "aabb:ccdd:ee" {
		t.Fatalf("got %q", got)
	}
}

func TestHexWithSepUnitGroup(t *testing.T) {
	got := pystrconv.HexWithSep([]byte{0x01, 0x02, 0x03}, '-', 1)
	if got != "01-02-03" {
		t.Fatalf("got %q", got)
	}
}

func TestHexWithSepGroupTooLarge(t *testing.T) {
	// Group >= len(buf) means no separator is inserted.
	got := pystrconv.HexWithSep([]byte{0x01, 0x02}, ':', 4)
	if got != "0102" {
		t.Fatalf("got %q", got)
	}
}

func TestHexBytesVariants(t *testing.T) {
	if got := pystrconv.HexBytes([]byte{0x01, 0xab}); string(got) != "01ab" {
		t.Fatalf("HexBytes got %q", got)
	}
	got := pystrconv.HexBytesWithSep([]byte{0xaa, 0xbb, 0xcc, 0xdd}, ':', 2)
	if string(got) != "aabb:ccdd" {
		t.Fatalf("HexBytesWithSep got %q", got)
	}
}
