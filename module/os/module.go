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
	"strings"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

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

	m := objects.NewModule("os")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("path"), pathMod); err != nil {
		return nil, err
	}
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"sep", objects.NewStr(string(filepath.Separator))},
		{"pardir", objects.NewStr("..")},
		{"curdir", objects.NewStr(".")},
		{"linesep", objects.NewStr("\n")},
		{"name", objects.NewStr("posix")},
		{"getcwd", objects.NewBuiltinFunction("getcwd", getcwd)},
		{"environ", objects.NewDict()},
		{"listdir", objects.NewBuiltinFunction("listdir", listdir)},
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

func getcwd(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cwd, err := goos.Getwd()
	if err != nil {
		return nil, fmt.Errorf("OSError: %w", err)
	}
	return objects.NewStr(cwd), nil
}

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
