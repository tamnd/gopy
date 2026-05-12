// Package _stat is the gopy port of CPython's Modules/_stat.c.
// It provides file-status constants and helper functions that back
// Lib/stat.py.
//
// CPython: Modules/_stat.c

package _stat

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_stat", buildModule)
}

// ---------------------------------------------------------------------------
// Constants (POSIX values matching CPython's stat.h usage).
// ---------------------------------------------------------------------------

// Mode type masks.
//
// CPython: Modules/_stat.c:27
const (
	sFmt  = 0o170000 // S_IFMT
	sDir  = 0o040000 // S_IFDIR
	sChr  = 0o020000 // S_IFCHR
	sBlk  = 0o060000 // S_IFBLK
	sReg  = 0o100000 // S_IFREG
	sFifo = 0o010000 // S_IFIFO
	sLnk  = 0o120000 // S_IFLNK
	sSock = 0o140000 // S_IFSOCK
)

// Permission bits.
//
// CPython: Modules/_stat.c:56
const (
	sIsuid = 0o4000 // S_ISUID
	sIsgid = 0o2000 // S_ISGID
	sIsvtx = 0o1000 // S_ISVTX

	sIrwxu = 0o0700 // S_IRWXU
	sIrusr = 0o0400 // S_IRUSR
	sIwusr = 0o0200 // S_IWUSR
	sIxusr = 0o0100 // S_IXUSR

	sIrwxg = 0o0070 // S_IRWXG
	sIrgrp = 0o0040 // S_IRGRP
	sIwgrp = 0o0020 // S_IWGRP
	sIxgrp = 0o0010 // S_IXGRP

	sIrwxo = 0o0007 // S_IRWXO
	sIroth = 0o0004 // S_IROTH
	sIwoth = 0o0002 // S_IWOTH
	sIxoth = 0o0001 // S_IXOTH
)

// UF_ and SF_ flags (BSD/macOS extended flags).
//
// CPython: Modules/_stat.c:91
const (
	ufNodump     = 0x00000001
	ufImmutable  = 0x00000002
	ufAppend     = 0x00000004
	ufOpaque     = 0x00000008
	ufNounlink   = 0x00000010
	ufCompressed = 0x00000020
	ufHidden     = 0x00008000
	sfArchived   = 0x00010000
	sfImmutable  = 0x00020000
	sfAppend     = 0x00040000
	sfNounlink   = 0x00100000
	sfSnapshot   = 0x00200000
)

// buildModule materializes the _stat module dict.
//
// CPython: Modules/_stat.c:290 stat_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_stat")
	d := m.Dict()

	intConsts := []struct {
		name string
		val  int64
	}{
		// Mode type masks.
		{"S_IFMT", sFmt},
		{"S_IFDIR", sDir},
		{"S_IFCHR", sChr},
		{"S_IFBLK", sBlk},
		{"S_IFREG", sReg},
		{"S_IFIFO", sFifo},
		{"S_IFLNK", sLnk},
		{"S_IFSOCK", sSock},
		// Permission bits.
		{"S_ISUID", sIsuid},
		{"S_ISGID", sIsgid},
		{"S_ISVTX", sIsvtx},
		{"S_IRWXU", sIrwxu},
		{"S_IRUSR", sIrusr},
		{"S_IWUSR", sIwusr},
		{"S_IXUSR", sIxusr},
		{"S_IRWXG", sIrwxg},
		{"S_IRGRP", sIrgrp},
		{"S_IWGRP", sIwgrp},
		{"S_IXGRP", sIxgrp},
		{"S_IRWXO", sIrwxo},
		{"S_IROTH", sIroth},
		{"S_IWOTH", sIwoth},
		{"S_IXOTH", sIxoth},
		// UF_ flags.
		{"UF_NODUMP", ufNodump},
		{"UF_IMMUTABLE", ufImmutable},
		{"UF_APPEND", ufAppend},
		{"UF_OPAQUE", ufOpaque},
		{"UF_NOUNLINK", ufNounlink},
		{"UF_COMPRESSED", ufCompressed},
		{"UF_HIDDEN", ufHidden},
		// SF_ flags.
		{"SF_ARCHIVED", sfArchived},
		{"SF_IMMUTABLE", sfImmutable},
		{"SF_APPEND", sfAppend},
		{"SF_NOUNLINK", sfNounlink},
		{"SF_SNAPSHOT", sfSnapshot},
	}
	for _, c := range intConsts {
		if err := d.SetItem(objects.NewStr(c.name), objects.NewInt(c.val)); err != nil {
			return nil, err
		}
	}

	funcEntries := []struct {
		name string
		val  objects.Object
	}{
		{"S_ISDIR", objects.NewBuiltinFunction("S_ISDIR", statIsDir)},
		{"S_ISCHR", objects.NewBuiltinFunction("S_ISCHR", statIsChr)},
		{"S_ISBLK", objects.NewBuiltinFunction("S_ISBLK", statIsBlk)},
		{"S_ISREG", objects.NewBuiltinFunction("S_ISREG", statIsReg)},
		{"S_ISFIFO", objects.NewBuiltinFunction("S_ISFIFO", statIsFifo)},
		{"S_ISLNK", objects.NewBuiltinFunction("S_ISLNK", statIsLnk)},
		{"S_ISSOCK", objects.NewBuiltinFunction("S_ISSOCK", statIsSock)},
		{"filemode", objects.NewBuiltinFunction("filemode", filemode)},
	}
	for _, e := range funcEntries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// modeArg extracts a single integer mode argument.
func modeArg(name string, args []objects.Object, kwargs map[string]objects.Object) (int64, error) {
	if len(kwargs) != 0 {
		return 0, fmt.Errorf("TypeError: %s() takes no keyword arguments", name)
	}
	if len(args) != 1 {
		return 0, fmt.Errorf("TypeError: %s() expected 1 argument, got %d", name, len(args))
	}
	i, ok := args[0].(*objects.Int)
	if !ok {
		return 0, errors.New("TypeError: mode must be an integer")
	}
	v, ok2 := i.Int64()
	if !ok2 {
		return 0, errors.New("OverflowError: mode value too large")
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// S_IS* functions.
// ---------------------------------------------------------------------------

// statIsDir returns True if mode indicates a directory.
//
// CPython: Modules/_stat.c:110 stat_S_ISDIR_impl
func statIsDir(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("S_ISDIR", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool((mode & sFmt) == sDir), nil
}

// statIsChr returns True if mode indicates a character device.
//
// CPython: Modules/_stat.c:121 stat_S_ISCHR_impl
func statIsChr(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("S_ISCHR", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool((mode & sFmt) == sChr), nil
}

// statIsBlk returns True if mode indicates a block device.
//
// CPython: Modules/_stat.c:132 stat_S_ISBLK_impl
func statIsBlk(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("S_ISBLK", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool((mode & sFmt) == sBlk), nil
}

// statIsReg returns True if mode indicates a regular file.
//
// CPython: Modules/_stat.c:143 stat_S_ISREG_impl
func statIsReg(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("S_ISREG", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool((mode & sFmt) == sReg), nil
}

// statIsFifo returns True if mode indicates a FIFO (named pipe).
//
// CPython: Modules/_stat.c:154 stat_S_ISFIFO_impl
func statIsFifo(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("S_ISFIFO", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool((mode & sFmt) == sFifo), nil
}

// statIsLnk returns True if mode indicates a symbolic link.
//
// CPython: Modules/_stat.c:165 stat_S_ISLNK_impl
func statIsLnk(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("S_ISLNK", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool((mode & sFmt) == sLnk), nil
}

// statIsSock returns True if mode indicates a socket.
//
// CPython: Modules/_stat.c:176 stat_S_ISSOCK_impl
func statIsSock(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("S_ISSOCK", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool((mode & sFmt) == sSock), nil
}

// ---------------------------------------------------------------------------
// filemode.
// ---------------------------------------------------------------------------

// filemode converts an integer mode to a 10-character string like
// `drwxr-xr-x`. Mirrors the logic in stat.filemode / _filemode.
//
// CPython: Modules/_stat.c:200 stat_filemode_impl
func filemode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	mode, err := modeArg("filemode", args, kwargs)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 10)
	// File type character.
	switch mode & sFmt {
	case sDir:
		buf[0] = 'd'
	case sLnk:
		buf[0] = 'l'
	case sSock:
		buf[0] = 's'
	case sBlk:
		buf[0] = 'b'
	case sChr:
		buf[0] = 'c'
	case sFifo:
		buf[0] = 'p'
	default:
		buf[0] = '-'
	}
	// Owner permissions.
	buf[1] = rwx(mode, sIrusr, 'r')
	buf[2] = rwx(mode, sIwusr, 'w')
	buf[3] = execBit(mode, sIxusr, sIsuid, 's', 'S', 'x')
	// Group permissions.
	buf[4] = rwx(mode, sIrgrp, 'r')
	buf[5] = rwx(mode, sIwgrp, 'w')
	buf[6] = execBit(mode, sIxgrp, sIsgid, 's', 'S', 'x')
	// Other permissions.
	buf[7] = rwx(mode, sIroth, 'r')
	buf[8] = rwx(mode, sIwoth, 'w')
	buf[9] = execBit(mode, sIxoth, sIsvtx, 't', 'T', 'x')
	return objects.NewStr(string(buf)), nil
}

func rwx(mode, bit int64, ch byte) byte {
	if mode&bit != 0 {
		return ch
	}
	return '-'
}

// execBit handles the combined exec + setid/sticky bit display.
// If the special bit is set: exec -> lower, no-exec -> upper.
// If the special bit is not set: exec -> ch, no-exec -> '-'.
func execBit(mode, execMask, specialMask int64, lower, upper, plain byte) byte {
	hasExec := mode&execMask != 0
	hasSpecial := mode&specialMask != 0
	switch {
	case hasSpecial && hasExec:
		return lower
	case hasSpecial && !hasExec:
		return upper
	case hasExec:
		return plain
	default:
		return '-'
	}
}
