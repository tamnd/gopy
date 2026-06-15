// Bytecode-cache writing for the source loaders. After a .py file is
// compiled, SourceFileLoader.exec_module writes the resulting code
// object to a PEP 3147 __pycache__/<name>.<tag>.pyc file so the next
// import skips recompilation. gopy's import runs Go-side, so the write
// path is reimplemented here against the marshal .pyc writer.
//
// CPython: Lib/importlib/_bootstrap_external.py:1129 SourceFileLoader.get_code
// CPython: Lib/importlib/_bootstrap_external.py:1185 SourceFileLoader.set_data
package imp

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tamnd/gopy/marshal"
	"github.com/tamnd/gopy/objects"
)

// pycacheDir is the PEP 3147 cache subdirectory name.
//
// CPython: Lib/importlib/_bootstrap_external.py:60 _PYCACHE
const pycacheDir = "__pycache__"

// isFrozenBootstrapSource reports whether sourcePath is one of the two
// importlib bootstrap modules CPython freezes (importlib._bootstrap and
// importlib._bootstrap_external). Those are never byte-compiled to a .pyc
// in CPython, so gopy excludes them from the bytecode cache to keep their
// "<frozen importlib._bootstrap[_external]>" co_filename intact. A cached
// .pyc would be rewritten to the real disk path by fixCoFilename, leaving
// the import-machinery frames un-trimmable by remove_importlib_frames.
//
// CPython: Python/pylifecycle.c:1041 init_importlib (frozen modules)
func isFrozenBootstrapSource(sourcePath string) bool {
	return strings.HasSuffix(sourcePath, "importlib/_bootstrap.py") ||
		strings.HasSuffix(sourcePath, "importlib/_bootstrap_external.py")
}

// dontWriteBytecode reports sys.dont_write_bytecode. When True the
// source loaders skip the cache write entirely, exactly like CPython's
// SourceFileLoader.get_code (the `not sys.dont_write_bytecode` guard).
//
// CPython: Lib/importlib/_bootstrap_external.py:1167 source_to_code cache guard
func dontWriteBytecode() bool {
	sysMod, ok := GetModule("sys")
	if !ok {
		return true
	}
	v, err := objects.GetAttr(sysMod, objects.NewStr("dont_write_bytecode"))
	if err != nil {
		return true
	}
	return objects.IsTrue(v)
}

// cacheTag returns sys.implementation.cache_tag, the per-interpreter
// bytecode-cache discriminator (e.g. "gopy-3140"). The empty string
// signals a missing tag, in which case the caller skips caching the
// same way cache_from_source raises NotImplementedError.
//
// CPython: Lib/importlib/_bootstrap_external.py:480 cache_from_source (tag read)
func cacheTag() string {
	sysMod, ok := GetModule("sys")
	if !ok {
		return ""
	}
	impl, err := objects.GetAttr(sysMod, objects.NewStr("implementation"))
	if err != nil {
		return ""
	}
	tag, err := objects.GetAttr(impl, objects.NewStr("cache_tag"))
	if err != nil {
		return ""
	}
	t, ok := tag.(*objects.Unicode)
	if !ok {
		return ""
	}
	return t.Value()
}

// pycachePrefix returns sys.pycache_prefix as (value, set). When set,
// caches live under that root directory mirroring the source's absolute
// path instead of an adjacent __pycache__.
//
// CPython: Lib/importlib/_bootstrap_external.py:490 cache_from_source (prefix branch)
func pycachePrefix() (string, bool) {
	sysMod, ok := GetModule("sys")
	if !ok {
		return "", false
	}
	v, err := objects.GetAttr(sysMod, objects.NewStr("pycache_prefix"))
	if err != nil || objects.IsNone(v) {
		return "", false
	}
	p, ok := v.(*objects.Unicode)
	if !ok {
		return "", false
	}
	return p.Value(), true
}

// cacheFromSource computes the .pyc path for a source file, matching
// importlib.util.cache_from_source so the path gopy writes is the same
// one spec_from_file_location records as __cached__ and the loader reads
// back. Only the optimization=” (sys.flags.optimize == 0) case is
// produced; gopy never runs at -O.
//
// CPython: Lib/importlib/_bootstrap_external.py:466 cache_from_source
func cacheFromSource(sourcePath string) string {
	tag := cacheTag()
	if tag == "" {
		return ""
	}
	head, tail := filepath.Split(sourcePath)
	base := tail
	sep := ""
	if dot := strings.LastIndex(tail, "."); dot >= 0 {
		base, sep = tail[:dot], "."
		if base == "" {
			// A leading-dot name like ".pyc" keeps the whole tail as base.
			base = tail
			sep = ""
		}
	}
	almost := base + sep + tag
	filename := almost + ".pyc"
	if prefix, ok := pycachePrefix(); ok {
		// CPython rebuilds the source's absolute directory under the prefix,
		// dropping the volume separator so the tree nests cleanly.
		absHead, err := filepath.Abs(head)
		if err != nil {
			absHead = head
		}
		absHead = strings.TrimPrefix(absHead, string(filepath.Separator))
		return filepath.Join(prefix, absHead, filename)
	}
	return filepath.Join(filepath.Clean(head), pycacheDir, filename)
}

// readBytecodeCache returns the cached code object for sourcePath when a
// fresh, valid .pyc exists under __pycache__. "Fresh" means the .pyc
// magic matches and its timestamp-mode header records exactly the
// source's current mtime and size, the same staleness test
// SourceFileLoader.get_code applies before trusting the cache. A hash-
// mode .pyc (PEP 552) is only trusted when its hash bit is unchecked;
// any other condition (missing, stale, unreadable, checked-hash) returns
// ok=false so the caller recompiles from source.
//
// CPython: Lib/importlib/_bootstrap_external.py:1129 SourceFileLoader.get_code
// CPython: Lib/importlib/_bootstrap_external.py:585 _validate_timestamp_pyc
func readBytecodeCache(sourcePath string) (*objects.Code, bool) {
	if isFrozenBootstrapSource(sourcePath) {
		// CPython freezes importlib._bootstrap[_external] and never loads
		// them from a .pyc, so their code objects keep the synthetic
		// "<frozen importlib._bootstrap[_external]>" co_filename for the
		// life of the process. gopy loads them from source instead; reading
		// a cached .pyc would route through fixCoFilename below and rewrite
		// that co_filename to the real disk path, leaving the import-machinery
		// frames un-trimmable by remove_importlib_frames. Skip the cache so
		// the source compiler stamps the frozen name every time.
		//
		// CPython: Python/import.c:3500 remove_importlib_frames (frozen names)
		return nil, false
	}
	dest := cacheFromSource(sourcePath)
	if dest == "" {
		return nil, false
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, false
	}
	f, err := os.Open(dest) //nolint:gosec // dest is cacheFromSource of a trusted source path.
	if err != nil {
		return nil, false
	}
	defer f.Close()
	code, hdr, err := marshal.ReadPyc(f)
	if err != nil {
		return nil, false
	}
	if hdr.Flags&0x1 != 0 {
		// Hash-based .pyc: an unchecked-hash cache is trusted unconditionally,
		// a checked-hash cache would need the source hash recomputed, which
		// the timestamp fast path does not do, so fall back to recompiling.
		//
		// CPython: Lib/importlib/_bootstrap_external.py:609 _validate_hash_pyc
		if hdr.Flags&0x2 != 0 {
			return code, true
		}
		return nil, false
	}
	mtime := uint32(info.ModTime().Unix())
	size := uint32(info.Size())
	if hdr.Mtime != mtime || hdr.SourceSize != size {
		return nil, false
	}
	// The cached code object carries whatever co_filename it was compiled
	// with (py_compile's dfile can differ from the real source). When the
	// source still exists the loader rewrites co_filename to the actual
	// path, recursing into nested code consts, exactly like _compile_bytecode
	// calling _imp._fix_co_filename.
	//
	// CPython: Lib/importlib/_bootstrap_external.py:809 _compile_bytecode
	// CPython: Python/import.c:1276 _imp__fix_co_filename_impl
	fixCoFilename(code, code.Filename, sourcePath)
	return code, true
}

// fixCoFilename rewrites co_filename on code and every nested code const
// whose filename matches oldname, mirroring CPython's recursive
// update_code_filenames. Only matching consts are touched so that a code
// object compiled against a different file is left alone.
//
// CPython: Python/import.c:1243 update_code_filenames
func fixCoFilename(code *objects.Code, oldname, newname string) {
	if code.Filename != oldname {
		return
	}
	code.Filename = newname
	for _, c := range code.Consts {
		if nested, ok := c.(*objects.Code); ok {
			fixCoFilename(nested, oldname, newname)
		}
	}
	code.SyncConstObjs()
}

// writeBytecodeCache writes code to the .pyc cache for sourcePath unless
// sys.dont_write_bytecode is set. The header records the source file's
// mtime and size so a stale cache is detected on the next import. A
// write failure is swallowed: CPython's set_data treats a NotADirectory
// or permission error as non-fatal (the import still succeeds from
// source), and so does gopy.
//
// CPython: Lib/importlib/_bootstrap_external.py:1167 get_code (cache write)
// CPython: Lib/importlib/_bootstrap_external.py:1185 set_data (atomic write)
func writeBytecodeCache(sourcePath string, code *objects.Code) {
	if dontWriteBytecode() || isFrozenBootstrapSource(sourcePath) {
		return
	}
	dest := cacheFromSource(sourcePath)
	if dest == "" {
		return
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return
	}
	mtime := uint32(info.ModTime().Unix())
	size := uint32(info.Size())

	var buf bytes.Buffer
	if err := marshal.WritePyc(&buf, code, mtime, size); err != nil {
		return
	}
	// 0o777 is CPython's makedirs mode for __pycache__; the umask narrows it.
	// CPython: Lib/importlib/_bootstrap_external.py source_to_cache makedirs.
	if err := os.MkdirAll(filepath.Dir(dest), 0o777); err != nil { //nolint:gosec // CPython __pycache__ mode, umask-narrowed
		return
	}
	// The cache inherits the source's permission bits plus write access, so a
	// read-only .py still yields a rewritable .pyc.
	//
	// CPython: Lib/importlib/_bootstrap_external.py:438 _calc_mode
	mode := info.Mode().Perm() | 0o200

	// Write atomically the way _write_atomic does: a uniquely-suffixed temp
	// file in the cache directory opened O_EXCL with the computed mode, then
	// rename over the target. The temp name is keyed off the pid so concurrent
	// writers do not collide.
	//
	// CPython: Lib/importlib/_bootstrap_external.py:184 _write_atomic
	tmp := dest + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	f, err := os.OpenFile(tmp, os.O_EXCL|os.O_CREATE|os.O_WRONLY, mode&0o666) //nolint:gosec // tmp derives from a trusted cache path.
	if err != nil {
		return
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
	}
}
