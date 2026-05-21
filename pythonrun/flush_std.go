package pythonrun

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// FlushStdFiles ports CPython's flush_std_files. It reads sys.stdout
// and sys.stderr via the sys module dict and pushes any buffered
// writes through. Errors are swallowed (CPython routes them through
// PyErr_FormatUnraisable / PyErr_Clear), so the function is safe to
// call from the gopy entry points right before they return.
//
// CPython: Python/pylifecycle.c:1813 flush_std_files
func FlushStdFiles() {
	flushSysAttr("stdout")
	flushSysAttr("stderr")
}

func flushSysAttr(name string) {
	mod, ok := imp.GetModule("sys")
	if !ok || mod == nil {
		return
	}
	d := mod.Dict()
	if d == nil {
		return
	}
	v, err := d.GetItem(objects.NewStr(name))
	if err != nil || v == nil || objects.IsNone(v) {
		return
	}
	if f, ok := v.(*objects.File); ok {
		_ = f.Flush()
		return
	}
	// Duck-typed flush(): mirrors _PyFile_Flush which calls the
	// `flush` method on whatever sys.<name> points at.
	//
	// CPython: Python/fileutils.c:2814 _PyFile_Flush
	flushAttr, err := objects.GetAttr(v, objects.NewStr("flush"))
	if err != nil {
		return
	}
	_, _ = objects.Call(flushAttr, objects.NewTuple(nil), nil)
}
