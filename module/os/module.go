// os and os.path modules: minimal Go-backed surface. CPython splits
// os into the OS-specific posixmodule.c (the syscalls) and Lib/os.py
// (the cross-platform glue) plus posixpath.py for the path operations
// re-exported as os.path. unittest reaches in for os.path.basename /
// os.path.isfile / os.path.abspath / os.path.dirname / os.path.join /
// os.path.normpath / os.path.splitext / os.path.commonprefix /
// os.path.relpath / os.path.sep / os.path.isabs / os.pardir / os.sep /
// os.getcwd. Until the full posixpath port lands, route those through
// Go's path/filepath so the import succeeds and basic loader behavior
// keeps working.
//
// CPython: Modules/posixmodule.c posix-style syscalls
// CPython: Lib/os.py public surface
// CPython: Lib/posixpath.py path helpers re-exported as os.path

package os

import (
	"fmt"
	goos "os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// statResultType is the struct-sequence type for os.stat_result.
// CPython: Modules/posixmodule.c:3238 os_stat_impl
var statResultType = objects.NewStructSeqType("os.stat_result", []objects.StructSeqField{
	{Name: "st_mode"},
	{Name: "st_ino"},
	{Name: "st_dev"},
	{Name: "st_nlink"},
	{Name: "st_uid"},
	{Name: "st_gid"},
	{Name: "st_size"},
	{Name: "st_atime"},
	{Name: "st_mtime"},
	{Name: "st_ctime"},
})

func init() {
	_ = imp.AppendInittab("os", buildOS)
	_ = imp.AppendInittab("os.path", buildPath)
	_ = imp.AppendInittab("posixpath", buildPath)
	_ = imp.AppendInittab("ntpath", buildPath)
}

// buildPath populates the os.path / posixpath module.
func buildPath() (*objects.Module, error) {
	m := objects.NewModule("posixpath")
	d := m.Dict()
	for _, e := range pathEntries() {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// buildOS populates the os module. The path attribute holds a
// reference to the same posixpath module that buildPath returns,
// keeping `os.path.X` and `from os.path import X` consistent.
func buildOS() (*objects.Module, error) {
	pathMod, err := buildPath()
	if err != nil {
		return nil, err
	}

	// environ: populate from the real process environment.
	// CPython: Modules/posixmodule.c:1768 convertenviron
	environDict := objects.NewDict()
	for _, kv := range goos.Environ() {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		k, v := kv[:idx], kv[idx+1:]
		if err := environDict.SetItem(objects.NewStr(k), objects.NewStr(v)); err != nil {
			return nil, err
		}
	}

	m := objects.NewModule("os")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("path"), pathMod); err != nil {
		return nil, err
	}

	// Platform constants.
	sep := string(filepath.Separator)
	linesep := "\n"
	pathsep := ":"
	osName := "posix"
	if runtime.GOOS == "windows" {
		linesep = "\r\n"
		pathsep = ";"
		osName = "nt"
	}

	entries := []struct {
		name string
		val  objects.Object
	}{
		{"sep", objects.NewStr(sep)},
		{"pardir", objects.NewStr("..")},
		{"curdir", objects.NewStr(".")},
		{"linesep", objects.NewStr(linesep)},
		{"pathsep", objects.NewStr(pathsep)},
		{"devnull", objects.NewStr(devNull())},
		{"name", objects.NewStr(osName)},
		{"environ", environDict},
		// functions
		{"getcwd", objects.NewBuiltinFunction("getcwd", getcwd)},
		{"listdir", objects.NewBuiltinFunction("listdir", listdir)},
		{"stat", objects.NewBuiltinFunction("stat", stat)},
		{"getenv", objects.NewBuiltinFunction("getenv", getenv)},
		{"getpid", objects.NewBuiltinFunction("getpid", getpid)},
		{"getuid", objects.NewBuiltinFunction("getuid", getuid)},
		{"makedirs", objects.NewBuiltinFunction("makedirs", makedirs)},
		{"mkdir", objects.NewBuiltinFunction("mkdir", mkdir)},
		{"remove", objects.NewBuiltinFunction("remove", remove)},
		{"unlink", objects.NewBuiltinFunction("unlink", remove)},
		{"rename", objects.NewBuiltinFunction("rename", rename)},
		{"rmdir", objects.NewBuiltinFunction("rmdir", rmdir)},
		{"walk", objects.NewBuiltinFunction("walk", walk)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func pathEntries() []struct {
	name string
	val  objects.Object
} {
	return []struct {
		name string
		val  objects.Object
	}{
		{"sep", objects.NewStr(string(filepath.Separator))},
		{"pardir", objects.NewStr("..")},
		{"curdir", objects.NewStr(".")},
		{"basename", objects.NewBuiltinFunction("basename", basename)},
		{"dirname", objects.NewBuiltinFunction("dirname", dirname)},
		{"join", objects.NewBuiltinFunction("join", join)},
		{"split", objects.NewBuiltinFunction("split", splitPath)},
		{"splitext", objects.NewBuiltinFunction("splitext", splitext)},
		{"isabs", objects.NewBuiltinFunction("isabs", isabs)},
		{"abspath", objects.NewBuiltinFunction("abspath", abspath)},
		{"normpath", objects.NewBuiltinFunction("normpath", normpath)},
		{"relpath", objects.NewBuiltinFunction("relpath", relpath)},
		{"isfile", objects.NewBuiltinFunction("isfile", isfile)},
		{"isdir", objects.NewBuiltinFunction("isdir", isdir)},
		{"exists", objects.NewBuiltinFunction("exists", exists)},
		{"commonprefix", objects.NewBuiltinFunction("commonprefix", commonprefix)},
		{"expanduser", objects.NewBuiltinFunction("expanduser", expanduser)},
		{"realpath", objects.NewBuiltinFunction("realpath", abspath)},
	}
}

func argString(args []objects.Object) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("TypeError: missing argument")
	}
	return objects.Str(args[0])
}

// objectBool extracts a Go bool from a Python object.
func objectBool(o objects.Object) bool {
	b, _ := objects.IsTruthy(o)
	return b
}

// objectInt64 extracts an int64 from a Python int object.
func objectInt64(o objects.Object) (int64, bool) {
	if i, ok := o.(*objects.Int); ok {
		return i.BigInt().Int64(), true
	}
	return 0, false
}

func basename(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(filepath.Base(s)), nil
}

func dirname(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(filepath.Dir(s)), nil
}

func join(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	parts := make([]string, len(args))
	for i := range args {
		s, err := objects.Str(args[i])
		if err != nil {
			return nil, err
		}
		parts[i] = s
	}
	return objects.NewStr(filepath.Join(parts...)), nil
}

func splitPath(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	dir, base := filepath.Split(s)
	dir = strings.TrimRight(dir, string(filepath.Separator))
	return objects.NewTuple([]objects.Object{objects.NewStr(dir), objects.NewStr(base)}), nil
}

func splitext(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	ext := filepath.Ext(s)
	root := strings.TrimSuffix(s, ext)
	return objects.NewTuple([]objects.Object{objects.NewStr(root), objects.NewStr(ext)}), nil
}

func isabs(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(filepath.IsAbs(s)), nil
}

func abspath(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	abs, perr := filepath.Abs(s)
	if perr != nil {
		return objects.NewStr(s), nil //nolint:nilerr // mirror posixpath: fall back to the input on resolve failure
	}
	return objects.NewStr(abs), nil
}

func normpath(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	return objects.NewStr(filepath.Clean(s)), nil
}

func relpath(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	target, err := argString(args)
	if err != nil {
		return nil, err
	}
	base := ""
	if len(args) >= 2 {
		base, _ = objects.Str(args[1])
	}
	if base == "" {
		base, _ = goos.Getwd()
	}
	rel, perr := filepath.Rel(base, target)
	if perr != nil {
		return objects.NewStr(target), nil //nolint:nilerr // mirror posixpath: fall back to the input on resolve failure
	}
	return objects.NewStr(rel), nil
}

func isfile(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	info, ferr := goos.Stat(s)
	return objects.NewBool(ferr == nil && !info.IsDir()), nil
}

func isdir(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	info, ferr := goos.Stat(s)
	return objects.NewBool(ferr == nil && info.IsDir()), nil
}

func exists(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	_, ferr := goos.Stat(s)
	return objects.NewBool(ferr == nil), nil
}

func commonprefix(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.NewStr(""), nil
	}
	tp := args[0].Type()
	if tp.Iter == nil {
		return nil, fmt.Errorf("TypeError: commonprefix() requires an iterable")
	}
	it, err := tp.Iter(args[0])
	if err != nil {
		return nil, err
	}
	itType := it.Type()
	var strs []string
	for {
		v, ierr := itType.IterNext(it)
		if ierr != nil || v == nil {
			break
		}
		s, _ := objects.Str(v)
		strs = append(strs, s)
	}
	if len(strs) == 0 {
		return objects.NewStr(""), nil
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		limit := len(prefix)
		if len(s) < limit {
			limit = len(s)
		}
		i := 0
		for i < limit && prefix[i] == s[i] {
			i++
		}
		prefix = prefix[:i]
	}
	return objects.NewStr(prefix), nil
}

func expanduser(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, err := argString(args)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(s, "~") {
		return objects.NewStr(s), nil
	}
	home, _ := goos.UserHomeDir()
	return objects.NewStr(home + s[1:]), nil
}

// CPython: Modules/posixmodule.c:4324 os_getcwd_impl
func getcwd(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cwd, err := goos.Getwd()
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewStr(cwd), nil
}

// CPython: Modules/posixmodule.c:4692 os_listdir_impl
func listdir(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	dir := "."
	if len(args) >= 1 {
		dir, _ = objects.Str(args[0])
	}
	ents, err := goos.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	items := make([]objects.Object, len(ents))
	for i, e := range ents {
		items[i] = objects.NewStr(e.Name())
	}
	return objects.NewList(items), nil
}

// CPython: Modules/posixmodule.c:3238 os_stat_impl
func stat(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	path, err := argString(args)
	if err != nil {
		return nil, err
	}
	info, serr := goos.Stat(path)
	if serr != nil {
		return nil, fmt.Errorf("OSError: %w", serr)
	}
	ino, dev, nlink, uid, gid, atime, ctime := statSysFields(info)
	mtime := info.ModTime().Unix()
	return objects.NewStructSeq(statResultType, []objects.Object{
		objects.NewInt(int64(info.Mode())),
		objects.NewInt(int64(ino)),
		objects.NewInt(int64(dev)),
		objects.NewInt(int64(nlink)),
		objects.NewInt(int64(uid)),
		objects.NewInt(int64(gid)),
		objects.NewInt(info.Size()),
		objects.NewFloat(float64(atime)),
		objects.NewFloat(float64(mtime)),
		objects.NewFloat(float64(ctime)),
	}), nil
}

// getenv mirrors Lib/os.py:818 getenv: returns environ[key] or default.
// CPython: Lib/os.py:818 getenv
func getenv(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: getenv() missing required argument: 'key'")
	}
	key, err := objects.Str(args[0])
	if err != nil {
		return nil, err
	}
	val, found := goos.LookupEnv(key)
	if found {
		return objects.NewStr(val), nil
	}
	if len(args) >= 2 {
		return args[1], nil
	}
	if v, ok := kwargs["default"]; ok {
		return v, nil
	}
	return objects.None(), nil
}

// CPython: Modules/posixmodule.c:9121 os_getpid_impl
func getpid(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(goos.Getpid())), nil
}

// CPython: Lib/os.py:211 makedirs (pure Python wrapper around os.mkdir)
func makedirs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: makedirs() missing required argument: 'name'")
	}
	path, err := objects.Str(args[0])
	if err != nil {
		return nil, err
	}
	// mode is args[1] or kwargs["mode"] — ignored by os.MkdirAll but
	// accepted for API compatibility.
	existOK := false
	if len(args) >= 3 {
		existOK = objectBool(args[2])
	} else if v, ok := kwargs["exist_ok"]; ok {
		existOK = objectBool(v)
	}
	merr := goos.MkdirAll(path, 0o777)
	if merr != nil {
		if goos.IsExist(merr) && existOK {
			return objects.None(), nil
		}
		return nil, fmt.Errorf("OSError: %w", merr)
	}
	return objects.None(), nil
}

// CPython: Modules/posixmodule.c:5715 os_mkdir_impl
func mkdir(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	path, err := argString(args)
	if err != nil {
		return nil, err
	}
	mode := goos.FileMode(0o777)
	if len(args) >= 2 {
		if m, ok := objectInt64(args[1]); ok {
			mode = goos.FileMode(m)
		}
	}
	if merr := goos.Mkdir(path, mode); merr != nil {
		return nil, fmt.Errorf("OSError: %w", merr)
	}
	return objects.None(), nil
}

// CPython: Modules/posixmodule.c:6269 os_remove_impl
func remove(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	path, err := argString(args)
	if err != nil {
		return nil, err
	}
	if rerr := goos.Remove(path); rerr != nil {
		return nil, fmt.Errorf("OSError: %w", rerr)
	}
	return objects.None(), nil
}

// CPython: Modules/posixmodule.c:5984 os_rename_impl
func rename(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: rename() requires src and dst")
	}
	src, err := objects.Str(args[0])
	if err != nil {
		return nil, err
	}
	dst, err := objects.Str(args[1])
	if err != nil {
		return nil, err
	}
	if rerr := goos.Rename(src, dst); rerr != nil {
		return nil, fmt.Errorf("OSError: %w", rerr)
	}
	return objects.None(), nil
}

// CPython: Modules/posixmodule.c:6029 os_rmdir_impl
func rmdir(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	path, err := argString(args)
	if err != nil {
		return nil, err
	}
	if rerr := goos.Remove(path); rerr != nil {
		return nil, fmt.Errorf("OSError: %w", rerr)
	}
	return objects.None(), nil
}

// walk yields (dirpath, dirnames, filenames) for each directory in the
// tree rooted at top. It mirrors Lib/os.py:297 walk.
//
// CPython: Lib/os.py:297 walk
func walk(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	top, err := argString(args)
	if err != nil {
		return nil, err
	}
	topdown := true
	if len(args) >= 2 {
		topdown = objectBool(args[1])
	} else if v, ok := kwargs["topdown"]; ok {
		topdown = objectBool(v)
	}
	followlinks := false
	if len(args) >= 4 {
		followlinks = objectBool(args[3])
	} else if v, ok := kwargs["followlinks"]; ok {
		followlinks = objectBool(v)
	}

	g := objects.NewGenerator("walk")
	go func() {
		walkDir(g, top, topdown, followlinks)
		g.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
	}()
	return g, nil
}

// walkDir is the recursive helper for walk. It communicates results
// through the generator's YieldCh channel, pausing at each yield until
// the consumer sends the next message.
func walkDir(g *objects.Generator, root string, topdown, followlinks bool) {
	ents, err := goos.ReadDir(root)
	if err != nil {
		return
	}

	var dirs []objects.Object
	var files []objects.Object
	for _, e := range ents {
		name := objects.NewStr(e.Name())
		info := e
		if e.Type()&goos.ModeSymlink != 0 && followlinks {
			// Treat symlinks as directories if they point at a directory.
			full := filepath.Join(root, e.Name())
			if fi, serr := goos.Stat(full); serr == nil && fi.IsDir() {
				dirs = append(dirs, name)
				continue
			}
		}
		if info.IsDir() {
			dirs = append(dirs, name)
		} else {
			files = append(files, name)
		}
	}

	dirList := objects.NewList(dirs)
	fileList := objects.NewList(files)
	triple := objects.NewTuple([]objects.Object{objects.NewStr(root), dirList, fileList})

	if topdown {
		// Yield this directory first, then recurse.
		g.YieldCh <- objects.GenMsg{Val: triple}
		msg := <-g.SendCh
		if msg.Err != nil {
			return
		}
		for _, d := range dirs {
			sub := filepath.Join(root, d.(*objects.Unicode).Value())
			walkDir(g, sub, topdown, followlinks)
		}
	} else {
		// Recurse first, then yield this directory.
		for _, d := range dirs {
			sub := filepath.Join(root, d.(*objects.Unicode).Value())
			walkDir(g, sub, topdown, followlinks)
		}
		g.YieldCh <- objects.GenMsg{Val: triple}
		msg := <-g.SendCh
		if msg.Err != nil {
			return
		}
	}
}

// devNull returns the null device path for the current OS.
func devNull() string {
	if runtime.GOOS == "windows" {
		return "nul"
	}
	return "/dev/null"
}
