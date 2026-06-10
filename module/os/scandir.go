// os.scandir() iterator and os.DirEntry objects. CPython splits the
// implementation across Modules/posixmodule.c (the C-level ScandirIterator
// and DirEntry types) and Lib/os.py (the module-level binding). The
// iterator is also a context manager so callers can write
// `with os.scandir(path) as it:` and have the handle closed eagerly.
//
// CPython: Modules/posixmodule.c:13291 os_scandir_impl
// CPython: Modules/posixmodule.c:13133 DirEntry (struct + type)

package os

import (
	"fmt"
	goos "os"
	"path/filepath"

	"github.com/tamnd/gopy/objects"
)

// ScandirIterator wraps a snapshot of the directory contents. CPython
// uses the platform readdir() stream and closes it lazily; gopy reads
// the entire directory up front via goos.ReadDir so close() is a no-op
// other than blocking further iteration.
//
// CPython: Modules/posixmodule.c:13591 ScandirIterator
type ScandirIterator struct {
	objects.Header

	root    string
	entries []goos.DirEntry
	pos     int
	closed  bool
}

// ScandirIteratorType is the type singleton for posix.ScandirIterator.
//
// CPython: Modules/posixmodule.c:13705 ScandirIteratorType
var ScandirIteratorType = objects.NewType("posix.ScandirIterator", []*objects.Type{objects.ObjectType()})

// DirEntry mirrors the CPython DirEntry struct: the cached name + full
// path, the goos.DirEntry for fast is_dir/is_file checks, and the lstat
// result cached on first stat() call.
//
// CPython: Modules/posixmodule.c:13133 DirEntry
type DirEntry struct {
	objects.Header

	name string
	path string
	ent  goos.DirEntry
}

// DirEntryType is the type singleton for os.DirEntry.
//
// CPython: Modules/posixmodule.c:13380 DirEntryType
var DirEntryType = objects.NewType("os.DirEntry", []*objects.Type{objects.ObjectType()})

func init() {
	ScandirIteratorType.Iter = scandirIter
	ScandirIteratorType.IterNext = scandirIterNext
	objects.SetTypeDescr(ScandirIteratorType, "__enter__", objects.NewBuiltinFunction("__enter__", scandirEnter))
	objects.SetTypeDescr(ScandirIteratorType, "__exit__", objects.NewBuiltinFunction("__exit__", scandirExit))
	objects.SetTypeDescr(ScandirIteratorType, "close", objects.NewBuiltinFunction("close", scandirCloseMethod))
	objects.AddIterSlotWrappers(ScandirIteratorType)

	DirEntryType.Repr = direntryRepr
	DirEntryType.Str = direntryRepr
	DirEntryType.Getattro = direntryGetattr
}

// newScandirIterator reads the directory and returns a fresh iterator.
//
// CPython: Modules/posixmodule.c:13839 posix_scandir
func newScandirIterator(root string) (*ScandirIterator, error) {
	ents, err := goos.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	it := &ScandirIterator{root: root, entries: ents}
	it.Init(ScandirIteratorType)
	return it, nil
}

// scandirIter implements __iter__: the iterator returns itself.
//
// CPython: Modules/posixmodule.c:13644 ScandirIterator_iternext (tp_iter wrapper)
func scandirIter(o objects.Object) (objects.Object, error) {
	return o, nil
}

// scandirIterNext yields the next DirEntry or StopIteration.
//
// CPython: Modules/posixmodule.c:13644 ScandirIterator_iternext
func scandirIterNext(o objects.Object) (objects.Object, error) {
	it := o.(*ScandirIterator)
	if it.closed {
		return nil, objects.ErrStopIteration
	}
	if it.pos >= len(it.entries) {
		return nil, objects.ErrStopIteration
	}
	ent := it.entries[it.pos]
	it.pos++
	de := &DirEntry{
		name: ent.Name(),
		path: filepath.Join(it.root, ent.Name()),
		ent:  ent,
	}
	de.Init(DirEntryType)
	return de, nil
}

// scandirEnter returns the iterator itself for `with os.scandir(p) as it:`.
//
// CPython: Modules/posixmodule.c:13710 ScandirIterator___enter___impl
func scandirEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self argument")
	}
	it, ok := args[0].(*ScandirIterator)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__() expected posix.ScandirIterator self")
	}
	return it, nil
}

// scandirExit closes the iterator on __exit__. Always returns None so the
// `with` block re-raises any pending exception.
//
// CPython: Modules/posixmodule.c:13720 ScandirIterator___exit___impl
func scandirExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self argument")
	}
	it, ok := args[0].(*ScandirIterator)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__() expected posix.ScandirIterator self")
	}
	it.closed = true
	it.entries = nil
	return objects.None(), nil
}

// scandirCloseMethod is the `close()` method, equivalent to triggering
// __exit__ without exception info.
//
// CPython: Modules/posixmodule.c:13728 ScandirIterator_close
func scandirCloseMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: close() missing self argument")
	}
	it, ok := args[0].(*ScandirIterator)
	if !ok {
		return nil, fmt.Errorf("TypeError: close() expected posix.ScandirIterator self")
	}
	it.closed = true
	it.entries = nil
	return objects.None(), nil
}

func direntryRepr(o objects.Object) (string, error) {
	de := o.(*DirEntry)
	return fmt.Sprintf("<DirEntry %q>", de.name), nil
}

// direntryGetattr exposes the name / path attributes and the
// is_dir / is_file / is_symlink / stat / inode methods.
//
// CPython: Modules/posixmodule.c:13380 DirEntryType (tp_getset + tp_methods)
func direntryGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	de := o.(*DirEntry)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "name":
		return objects.NewStr(de.name), nil
	case "path":
		return objects.NewStr(de.path), nil
	case "is_dir":
		return objects.NewBuiltinFunction("is_dir", direntryIsDir(de)), nil
	case "is_file":
		return objects.NewBuiltinFunction("is_file", direntryIsFile(de)), nil
	case "is_symlink":
		return objects.NewBuiltinFunction("is_symlink", direntryIsSymlink(de)), nil
	case "stat":
		return objects.NewBuiltinFunction("stat", direntryStat(de)), nil
	case "inode":
		return objects.NewBuiltinFunction("inode", direntryInode(de)), nil
	case "__fspath__":
		return objects.NewBuiltinFunction("__fspath__", direntryFspath(de)), nil
	}
	return nil, fmt.Errorf("AttributeError: 'os.DirEntry' object has no attribute %q", n.Value())
}

// direntryIsDir builds the bound is_dir(*, follow_symlinks=True) method.
//
// CPython: Modules/posixmodule.c:13180 DirEntry_is_dir
func direntryIsDir(de *DirEntry) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		follow := true
		if v, ok := kwargs["follow_symlinks"]; ok {
			follow = objectBool(v)
		}
		if de.ent.Type()&goos.ModeSymlink != 0 {
			if !follow {
				return objects.False(), nil
			}
			info, err := goos.Stat(de.path)
			if err != nil {
				return objects.False(), nil //nolint:nilerr // CPython treats stat failure on symlink target as False
			}
			return objects.NewBool(info.IsDir()), nil
		}
		return objects.NewBool(de.ent.IsDir()), nil
	}
}

// direntryIsFile builds the bound is_file(*, follow_symlinks=True) method.
//
// CPython: Modules/posixmodule.c:13197 DirEntry_is_file
func direntryIsFile(de *DirEntry) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		follow := true
		if v, ok := kwargs["follow_symlinks"]; ok {
			follow = objectBool(v)
		}
		if de.ent.Type()&goos.ModeSymlink != 0 {
			if !follow {
				return objects.False(), nil
			}
			info, err := goos.Stat(de.path)
			if err != nil {
				return objects.False(), nil //nolint:nilerr // CPython treats stat failure on symlink target as False
			}
			return objects.NewBool(info.Mode().IsRegular()), nil
		}
		return objects.NewBool(de.ent.Type().IsRegular()), nil
	}
}

// direntryIsSymlink builds the bound is_symlink() method.
//
// CPython: Modules/posixmodule.c:13214 DirEntry_is_symlink
func direntryIsSymlink(de *DirEntry) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewBool(de.ent.Type()&goos.ModeSymlink != 0), nil
	}
}

// direntryStat builds the bound stat(*, follow_symlinks=True) method.
//
// CPython: Modules/posixmodule.c:13278 DirEntry_stat
func direntryStat(de *DirEntry) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(_ []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		follow := true
		if v, ok := kwargs["follow_symlinks"]; ok {
			follow = objectBool(v)
		}
		var info goos.FileInfo
		var err error
		if follow {
			info, err = goos.Stat(de.path)
		} else {
			info, err = goos.Lstat(de.path)
		}
		if err != nil {
			return nil, fmt.Errorf("OSError: %w", err)
		}
		ino, dev, nlink, uid, gid, atime, ctime := statSysFields(info)
		mtime := info.ModTime().Unix()
		blksize, blocks, rdev := statBlockFields(info)
		return newStatResult(statMode(info), int64(ino), int64(dev), int64(nlink), int64(uid), int64(gid), info.Size(), atime, mtime, ctime, blksize, blocks, rdev), nil
	}
}

// direntryInode builds the bound inode() method.
//
// CPython: Modules/posixmodule.c:13166 DirEntry_inode
func direntryInode(de *DirEntry) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		info, err := goos.Lstat(de.path)
		if err != nil {
			return nil, fmt.Errorf("OSError: %w", err)
		}
		ino, _, _, _, _, _, _ := statSysFields(info)
		return objects.NewInt(int64(ino)), nil
	}
}

// direntryFspath builds the bound __fspath__() method.
//
// CPython: Modules/posixmodule.c:13363 DirEntry___fspath___impl
func direntryFspath(de *DirEntry) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewStr(de.path), nil
	}
}
