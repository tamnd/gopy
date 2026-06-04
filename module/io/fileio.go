// FileIO is the raw file I/O type. It wraps an OS file descriptor and
// implements the _io.FileIO public surface: read, readall, readinto,
// write, seek, tell, truncate, close, fileno, capability predicates
// (readable/writable/seekable), repr, and the closed/closefd/mode/name
// attributes.
//
// CPython: Modules/_io/fileio.c:65 fileio (struct definition)
// CPython: Modules/_io/fileio.c:1248 fileio_methods

package io

import (
	"errors"
	"fmt"
	"io"
	stdos "os"

	"github.com/tamnd/gopy/objects"
)

// SMALLCHUNK / DEFAULT_BUFFER_SIZE / LARGE_BUFFER_CUTOFF_SIZE mirror the
// growth-policy constants used by readall() in CPython.
//
// CPython: Modules/_io/fileio.c (top of file)
const (
	smallChunk            = 8 * 1024
	defaultBufferSize     = 8 * 1024
	largeBufferCutoffSize = 64 * 1024 * 1024
	pyReadMax             = 1<<31 - 1
)

// FileIO wraps an *stdos.File plus the bookkeeping CPython carries on
// its fileio struct: capability flags, mode bits, statinfo collected at
// open, and the seekable tri-state probed lazily on first call.
//
// CPython: Modules/_io/fileio.c:65 fileio
type FileIO struct {
	objects.Header

	f         *stdos.File
	name      string // string form for repr/name; "" if opened from raw fd
	nameIsInt bool   // when true, the constructor was given an integer fd
	nameFd    int64

	readable  bool
	writable  bool
	appending bool
	created   bool

	closefd bool
	closed  bool

	// seekable: 0 = unknown, 1 = yes, -1 = no.
	// Probed lazily on first seekable() call.
	//
	// CPython: Modules/_io/fileio.c:653 _io_FileIO_seekable_impl
	seekable int

	statSize    int64
	statBlksize int64
	statValid   bool
}

// FileIOType is the type singleton for _io.FileIO.
//
// CPython: Modules/_io/fileio.c:1245 PyFileIO_Type
var FileIOType = objects.NewType("_io.FileIO", []*objects.Type{objects.ObjectType()})

func init() {
	FileIOType.Call = fileIOCall
	FileIOType.Repr = fileIORepr
	FileIOType.Str = fileIORepr
	FileIOType.Getattro = fileIOGetattr
	FileIOType.Setattro = fileIOSetattr
	// FileIO inherits object's identity-based __hash__ so it can be
	// used as a dict key (selectors and subprocess store fileobjs that way).
	// CPython: Objects/typeobject.c:7970 PyBaseObject_Type tp_hash
	FileIOType.Hash = objects.IdentityHash
	// LOAD_SPECIAL walks the type MRO for __enter__ / __exit__ rather
	// than going through instance Getattro, so `with f:` needs type-level
	// descriptors.
	//
	// CPython: Modules/_io/iobase.c:391 iobase_enter / :409 iobase_exit
	objects.SetTypeDescr(FileIOType, "__enter__", objects.NewBuiltinFunction("__enter__", fileIOEnterDescr))
	objects.SetTypeDescr(FileIOType, "__exit__", objects.NewBuiltinFunction("__exit__", fileIOExitDescr))
}

func fileIOEnterDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self argument")
	}
	fi, ok := args[0].(*FileIO)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__() expected _io.FileIO self")
	}
	if err := fi.checkClosed(); err != nil {
		return nil, err
	}
	return fi, nil
}

func fileIOExitDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self argument")
	}
	fi, ok := args[0].(*FileIO)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__() expected _io.FileIO self")
	}
	if err := fi.Close(); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// NewFileIO wraps an already-open *stdos.File. closefd defaults to true.
// Used by the open() helper and external callers that already hold a file.
//
// CPython: Modules/_io/fileio.c:244 _io_FileIO___init___impl (analog)
func NewFileIO(f *stdos.File, name, mode string, readable, writable bool) *FileIO {
	fi := &FileIO{
		f:        f,
		name:     name,
		readable: readable,
		writable: writable,
		closefd:  true,
	}
	fi.populateMode(mode)
	fi.populateStat()
	fi.Init(FileIOType)
	return fi
}

// populateMode fills appending/created from a user-facing mode string.
// readable/writable are pre-set by the caller from fileIOModeFlags.
func (fi *FileIO) populateMode(mode string) {
	for i := 0; i < len(mode); i++ {
		switch mode[i] {
		case 'a':
			fi.appending = true
		case 'x':
			fi.created = true
		}
	}
}

// populateStat caches st_size and st_blksize. CPython keeps the whole
// struct stat in self->stat_atopen; we only need size (for readall
// pre-allocation) and blksize (for the _blksize getter).
//
// CPython: Modules/_io/fileio.c:464 PyMem_New struct _Py_stat_struct
func (fi *FileIO) populateStat() {
	if fi.f == nil {
		return
	}
	info, err := fi.f.Stat()
	if err != nil {
		return
	}
	fi.statSize = info.Size()
	fi.statValid = true
	// st_blksize is not portable across Go's per-platform syscall.Stat_t
	// (int32 on darwin, int64 on linux, absent on windows), so we leave
	// statBlksize at zero and let the _blksize getter fall back to
	// DEFAULT_BUFFER_SIZE.
	//
	// CPython: Modules/_io/fileio.c:1292 fileio_get_blksize
}

// fileIOCall is the type-call slot.
// FileIO(name, mode='r', closefd=True, opener=None)
//
// CPython: Modules/_io/fileio.c:244 _io_FileIO___init___impl
func fileIOCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	names := []string{"name", "mode", "closefd", "opener"}
	bound := make([]objects.Object, len(names))
	copy(bound, args)
	for k, v := range kwargs {
		idx := -1
		for i, n := range names {
			if n == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("TypeError: FileIO() got an unexpected keyword argument %q", k)
		}
		if bound[idx] != nil {
			return nil, fmt.Errorf("TypeError: FileIO() got multiple values for argument %q", k)
		}
		bound[idx] = v
	}
	if bound[0] == nil {
		return nil, fmt.Errorf("TypeError: FileIO() missing required argument 'name'")
	}

	mode := "r"
	if bound[1] != nil && !objects.IsNone(bound[1]) {
		s, ok := bound[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: invalid mode: %s", bound[1].Type().Name)
		}
		mode = s.Value()
	}

	closefd := true
	if bound[2] != nil {
		t, err := objects.IsTruthy(bound[2])
		if err != nil {
			return nil, err
		}
		closefd = t
	}

	flag, readable, writable, appending, created, err := fileIOModeFlags(mode)
	if err != nil {
		return nil, err
	}

	// Integer fd path: wrap an existing descriptor instead of opening a path.
	//
	// CPython: Modules/_io/fileio.c:291 PyLong_AsInt(nameobj)
	if fd, ok := asIntFd(bound[0]); ok {
		if fd < 0 {
			return nil, fmt.Errorf("ValueError: negative file descriptor")
		}
		f := stdos.NewFile(uintptr(fd), fmt.Sprintf("<fdopen:%d>", fd))
		if f == nil {
			return nil, fmt.Errorf("OSError: bad file descriptor")
		}
		fi := &FileIO{
			f:         f,
			nameIsInt: true,
			nameFd:    fd,
			readable:  readable,
			writable:  writable,
			appending: appending,
			created:   created,
			closefd:   closefd,
		}
		fi.populateStat()
		fi.Init(FileIOType)
		if appending {
			_, _ = fi.f.Seek(0, io.SeekEnd)
		}
		return fi, nil
	}

	name, err := fileIONameArg(bound[0])
	if err != nil {
		return nil, err
	}
	if !closefd {
		return nil, fmt.Errorf("ValueError: Cannot use closefd=False with file name")
	}

	opener := bound[3]
	var f *stdos.File
	if opener != nil && !objects.IsNone(opener) && objects.Callable(opener) {
		// Call opener(name, flags) to get the file descriptor.
		//
		// CPython: Modules/_io/fileio.c:399 _PyObject_CallMethodIdObjArgs opener
		nameObj := objects.NewStr(name)
		flagObj := objects.NewInt(int64(flag))
		fdObj, callErr := objects.Call(opener, objects.NewTuple([]objects.Object{nameObj, flagObj}), nil)
		if callErr != nil {
			return nil, callErr
		}
		fd, ok := fdObj.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected integer from opener, got %s", fdObj.Type().Name)
		}
		fdVal, fits := fd.Int64()
		if !fits || fdVal < 0 {
			return nil, fmt.Errorf("ValueError: opener returned invalid file descriptor")
		}
		f = stdos.NewFile(uintptr(fdVal), name)
		if f == nil {
			return nil, fmt.Errorf("OSError: bad file descriptor from opener")
		}
	} else {
		f, err = stdos.OpenFile(name, flag, 0o666)
		if err != nil {
			// Preserve the os.PathError chain (errno + filename) with %w
			// so the unwind path can build a FileNotFoundError /
			// PermissionError carrying exc.errno / exc.filename, the way
			// CPython's _io.FileIO open raises via
			// PyErr_SetFromErrnoWithFilenameObject.
			//
			// CPython: Modules/_io/fileio.c:451 _io_FileIO___init___impl
			return nil, fmt.Errorf("OSError: %w", err)
		}
	}
	fi := &FileIO{
		f:         f,
		name:      name,
		readable:  readable,
		writable:  writable,
		appending: appending,
		created:   created,
		closefd:   true,
	}
	fi.populateStat()
	fi.Init(FileIOType)
	if appending {
		_, _ = fi.f.Seek(0, io.SeekEnd)
	}
	return fi, nil
}

// asIntFd extracts a small non-negative integer from an Int object.
// Returns ok=false when the input isn't an Int.
func asIntFd(o objects.Object) (int64, bool) {
	if _, isStr := o.(*objects.Unicode); isStr {
		return 0, false
	}
	if _, isBytes := o.(*objects.Bytes); isBytes {
		return 0, false
	}
	if v, ok := o.(*objects.Int); ok {
		n, fits := v.Int64()
		if !fits {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// fileIONameArg extracts a file-path string from the name argument.
// Accepts str, bytes, or any object implementing __fspath__ (os.PathLike).
//
// CPython: Modules/_io/fileio.c:303 PyUnicode_FSDecoder / PyUnicode_FSConverter
func fileIONameArg(o objects.Object) (string, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	}
	// os.PathLike: call __fspath__() and recurse.
	//
	// CPython: Modules/_io/fileio.c:303 PyUnicode_FSConverter calls
	// PyOS_FSPath which invokes __fspath__.
	if fspath, err := objects.GetAttr(o, objects.NewStr("__fspath__")); err == nil {
		result, err := objects.CallNoArgs(fspath)
		if err != nil {
			return "", err
		}
		return fileIONameArg(result)
	}
	return "", fmt.Errorf("TypeError: invalid file: %s", o.Type().Name)
}

// fileIOModeFlags maps the mode string to os flags plus capability bits
// and the appending/created bits. It enforces the "exactly one of
// create/read/write/append" rule and rejects unknown letters.
//
// CPython: Modules/_io/fileio.c:317 mode-parse loop
func fileIOModeFlags(mode string) (flag int, readable, writable, appending, created bool, err error) {
	var reading, writing, updating bool
	seen := map[byte]bool{}
	for i := 0; i < len(mode); i++ {
		c := mode[i]
		switch c {
		case 'x':
			created = true
			writable = true
		case 'r':
			reading = true
			readable = true
		case 'w':
			writing = true
			writable = true
		case 'a':
			appending = true
			writable = true
		case '+':
			if updating {
				return 0, false, false, false, false, fmt.Errorf("ValueError: invalid mode: %q", mode)
			}
			updating = true
			readable = true
			writable = true
		case 'b':
			// binary indicator, no-op for raw layer
		default:
			return 0, false, false, false, false, fmt.Errorf("ValueError: invalid mode: %q", mode)
		}
		if c != '+' && c != 'b' && seen[c] {
			return 0, false, false, false, false, fmt.Errorf("ValueError: invalid mode: %q", mode)
		}
		seen[c] = true
	}
	count := 0
	for _, v := range []bool{created, reading, writing, appending} {
		if v {
			count++
		}
	}
	if count != 1 {
		return 0, false, false, false, false, fmt.Errorf("ValueError: Must have exactly one of create/read/write/append mode and at most one plus")
	}
	switch {
	case reading:
		flag = stdos.O_RDONLY
	case writing:
		flag = stdos.O_WRONLY | stdos.O_CREATE | stdos.O_TRUNC
	case appending:
		flag = stdos.O_WRONLY | stdos.O_CREATE | stdos.O_APPEND
	case created:
		flag = stdos.O_WRONLY | stdos.O_CREATE | stdos.O_EXCL
	}
	if updating {
		flag = (flag &^ (stdos.O_RDONLY | stdos.O_WRONLY)) | stdos.O_RDWR
	}
	return flag, readable, writable, appending, created, nil
}

// modeString builds the canonical mode string surfaced via the `mode`
// attribute and the repr, e.g. "rb+", "wb", "xb".
//
// CPython: Modules/_io/fileio.c:1143 mode_string
func (fi *FileIO) modeString() string {
	switch {
	case fi.created:
		if fi.readable {
			return "xb+"
		}
		return "xb"
	case fi.appending:
		if fi.readable {
			return "ab+"
		}
		return "ab"
	case fi.readable:
		if fi.writable {
			return "rb+"
		}
		return "rb"
	default:
		return "wb"
	}
}

// fileIORepr matches CPython's `<_io.FileIO name='x' mode='rb' closefd=True>`.
//
// CPython: Modules/_io/fileio.c:1168 fileio_repr
func fileIORepr(o objects.Object) (string, error) {
	fi := o.(*FileIO)
	if fi.closed {
		return fmt.Sprintf("<%s [closed]>", FileIOType.Name), nil
	}
	cf := "True"
	if !fi.closefd {
		cf = "False"
	}
	if fi.nameIsInt {
		return fmt.Sprintf("<%s fd=%d mode='%s' closefd=%s>", FileIOType.Name, fi.nameFd, fi.modeString(), cf), nil
	}
	return fmt.Sprintf("<%s name=%q mode='%s' closefd=%s>", FileIOType.Name, fi.name, fi.modeString(), cf), nil
}

// errClosed mirrors err_closed(): "I/O operation on closed file".
//
// CPython: Modules/_io/fileio.c:583 err_closed
func errClosed() error {
	return errors.New("ValueError: I/O operation on closed file")
}

// errMode builds the "File not open for reading/writing" message.
//
// CPython: Modules/_io/fileio.c (err_mode helper)
func errMode(action string) error {
	return fmt.Errorf("io.UnsupportedOperation: File not open for %s", action)
}

func (fi *FileIO) checkClosed() error {
	if fi.closed || fi.f == nil {
		return errClosed()
	}
	return nil
}

// Read reads at most size bytes. size<0 delegates to ReadAll.
//
// CPython: Modules/_io/fileio.c:863 _io_FileIO_read_impl
func (fi *FileIO) Read(size int) ([]byte, error) {
	if err := fi.checkClosed(); err != nil {
		return nil, err
	}
	if !fi.readable {
		return nil, errMode("reading")
	}
	if size < 0 {
		return fi.ReadAll()
	}
	if size == 0 {
		return []byte{}, nil
	}
	if size > pyReadMax {
		size = pyReadMax
	}
	buf := make([]byte, size)
	n, err := fi.f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("OSError: %s", err.Error())
	}
	return buf[:n], nil
}

// ReadAll reads until EOF with stat-informed pre-allocation and the
// new_buffersize growth schedule.
//
// CPython: Modules/_io/fileio.c:736 _io_FileIO_readall_impl
func (fi *FileIO) ReadAll() ([]byte, error) {
	if err := fi.checkClosed(); err != nil {
		return nil, err
	}
	var end int64 = -1
	if fi.statValid && fi.statSize < pyReadMax {
		end = fi.statSize
	}
	var bufsize int
	if end <= 0 {
		bufsize = smallChunk
	} else if end > pyReadMax-1 {
		bufsize = pyReadMax
	} else {
		bufsize = int(end) + 1
	}
	// Adjust against current position for large reads, so a seek+readall
	// near EOF does not over-allocate.
	//
	// CPython: Modules/_io/fileio.c:779 lseek SEEK_CUR adjustment
	if bufsize > largeBufferCutoffSize && fi.f != nil {
		pos, err := fi.f.Seek(0, io.SeekCurrent)
		if err == nil && end >= pos && pos >= 0 && (end-pos) < int64(pyReadMax-1) {
			bufsize = int(end-pos) + 1
		}
	}

	result := make([]byte, bufsize)
	bytesRead := 0
	for {
		if bytesRead >= len(result) {
			grow := newBuffersize(bytesRead)
			if grow > pyReadMax || grow <= 0 {
				return nil, errors.New("OverflowError: unbounded read returned more bytes than a Python bytes object can hold")
			}
			if grow > len(result) {
				resized := make([]byte, grow)
				copy(resized, result[:bytesRead])
				result = resized
			}
		}
		n, err := fi.f.Read(result[bytesRead:])
		bytesRead += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("OSError: %s", err.Error())
		}
		if n == 0 {
			break
		}
	}
	return result[:bytesRead], nil
}

// newBuffersize matches CPython's amortized growth schedule.
//
// CPython: Modules/_io/fileio.c:705 new_buffersize
func newBuffersize(current int) int {
	var addend int
	if current > largeBufferCutoffSize {
		addend = current >> 3
	} else {
		addend = 256 + current
	}
	if addend < smallChunk {
		addend = smallChunk
	}
	return addend + current
}

// ReadInto fills dst, returning the number of bytes actually read.
//
// CPython: Modules/_io/fileio.c:676 _io_FileIO_readinto_impl
func (fi *FileIO) ReadInto(dst []byte) (int, error) {
	if err := fi.checkClosed(); err != nil {
		return 0, err
	}
	if !fi.readable {
		return 0, errMode("reading")
	}
	if len(dst) == 0 {
		return 0, nil
	}
	n, err := fi.f.Read(dst)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("OSError: %s", err.Error())
	}
	return n, nil
}

// Write writes data, returning bytes written.
//
// CPython: Modules/_io/fileio.c:925 _io_FileIO_write_impl
func (fi *FileIO) Write(data []byte) (int, error) {
	if err := fi.checkClosed(); err != nil {
		return 0, err
	}
	if !fi.writable {
		return 0, errMode("writing")
	}
	n, err := fi.f.Write(data)
	if err != nil {
		return 0, fmt.Errorf("OSError: %s", err.Error())
	}
	return n, nil
}

// Seek moves to a new offset and returns it.
//
// CPython: Modules/_io/fileio.c:1037 _io_FileIO_seek_impl via portable_lseek
func (fi *FileIO) Seek(pos int64, whence int) (int64, error) {
	if err := fi.checkClosed(); err != nil {
		return 0, err
	}
	out, err := fi.f.Seek(pos, whence)
	if err != nil {
		fi.seekable = -1
		return 0, fmt.Errorf("OSError: %s", err.Error())
	}
	fi.seekable = 1
	return out, nil
}

// Tell returns the current offset.
//
// CPython: Modules/_io/fileio.c:1055 _io_FileIO_tell_impl
func (fi *FileIO) Tell() (int64, error) {
	return fi.Seek(0, io.SeekCurrent)
}

// Truncate cuts the file to at most size bytes.
//
// CPython: Modules/_io/fileio.c:1078 _io_FileIO_truncate_impl
func (fi *FileIO) Truncate(size int64) (int64, error) {
	if err := fi.checkClosed(); err != nil {
		return 0, err
	}
	if !fi.writable {
		return 0, errMode("writing")
	}
	if err := fi.f.Truncate(size); err != nil {
		return 0, fmt.Errorf("OSError: %s", err.Error())
	}
	return size, nil
}

// Close releases the descriptor unless closefd is False.
//
// CPython: Modules/_io/fileio.c:159 _io_FileIO_close_impl
func (fi *FileIO) Close() error {
	if fi.closed {
		return nil
	}
	fi.closed = true
	if fi.f != nil && fi.closefd {
		if err := fi.f.Close(); err != nil {
			return fmt.Errorf("OSError: %s", err.Error())
		}
	}
	return nil
}

// Fileno returns the underlying OS descriptor.
//
// CPython: Modules/_io/fileio.c:602 _io_FileIO_fileno_impl
func (fi *FileIO) Fileno() (int64, error) {
	if err := fi.checkClosed(); err != nil {
		return 0, err
	}
	if fi.nameIsInt {
		return fi.nameFd, nil
	}
	return int64(fi.f.Fd()), nil
}

// isSeekable lazily probes via lseek(0, SEEK_CUR), caching the result.
//
// CPython: Modules/_io/fileio.c:647 _io_FileIO_seekable_impl
func (fi *FileIO) isSeekable() bool {
	if fi.seekable != 0 {
		return fi.seekable > 0
	}
	if fi.f == nil {
		return false
	}
	if _, err := fi.f.Seek(0, io.SeekCurrent); err != nil {
		fi.seekable = -1
		return false
	}
	fi.seekable = 1
	return true
}

// fileIOGetattr exposes attributes and methods.
//
// CPython: Modules/_io/fileio.c:1303 fileio_getsetlist + :1248 fileio_methods
func fileIOGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	fi := o.(*FileIO)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "name":
		if fi.nameIsInt {
			return objects.NewInt(fi.nameFd), nil
		}
		return objects.NewStr(fi.name), nil
	case "mode":
		return objects.NewStr(fi.modeString()), nil
	case "closed":
		return objects.NewBool(fi.closed), nil
	case "closefd":
		return objects.NewBool(fi.closefd), nil
	case "_blksize":
		bs := fi.statBlksize
		if bs <= 1 {
			bs = defaultBufferSize
		}
		return objects.NewInt(bs), nil
	}
	if fn := fileIOMethod(fi, n.Value()); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '_io.FileIO' object has no attribute '%s'", n.Value())
}

// fileIOSetattr handles attribute assignment on FileIO. Only .name is
// writable; tempfile.py assigns raw.name after the opener returns.
//
// CPython: Modules/_io/fileio.c:1195 fileio_name (member T_OBJECT, writable)
func fileIOSetattr(o, name, value objects.Object) error {
	fi, ok := o.(*FileIO)
	if !ok {
		return fmt.Errorf("TypeError: expected _io.FileIO self")
	}
	n, ok := name.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "name":
		if value == nil || objects.IsNone(value) {
			fi.name = ""
			fi.nameIsInt = false
			return nil
		}
		if s, ok := value.(*objects.Unicode); ok {
			fi.name = s.Value()
			fi.nameIsInt = false
			return nil
		}
		if i, ok := value.(*objects.Int); ok {
			v, _ := i.Int64()
			fi.nameFd = v
			fi.nameIsInt = true
			return nil
		}
		return fmt.Errorf("TypeError: name must be a str or int")
	}
	return fmt.Errorf("AttributeError: '_io.FileIO' object attribute '%s' is read-only", n.Value())
}

// fileIOMethod maps method names to BuiltinFunctions.
//
// CPython: Modules/_io/fileio.c:1248 fileio_methods
func fileIOMethod(fi *FileIO, name string) objects.Object {
	switch name {
	case "read":
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			size, err := optionalSize(args, "read")
			if err != nil {
				return nil, err
			}
			data, err := fi.Read(size)
			if err != nil {
				return nil, err
			}
			return objects.NewBytes(data), nil
		})
	case "readall":
		return objects.NewBuiltinFunction("readall", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			data, err := fi.ReadAll()
			if err != nil {
				return nil, err
			}
			return objects.NewBytes(data), nil
		})
	case "readinto":
		return objects.NewBuiltinFunction("readinto", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: readinto() takes exactly 1 argument (%d given)", len(args))
			}
			ba, ok := args[0].(*objects.ByteArray)
			if !ok {
				return nil, fmt.Errorf("TypeError: readinto() argument must be a writable bytes-like object, not '%s'", args[0].Type().Name)
			}
			n, err := fi.ReadInto(ba.Bytes())
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(n)), nil
		})
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: write() takes exactly 1 argument (%d given)", len(args))
			}
			var data []byte
			switch v := args[0].(type) {
			case *objects.Bytes:
				data = v.Bytes()
			case *objects.ByteArray:
				data = v.Bytes()
			default:
				return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", args[0].Type().Name)
			}
			n, err := fi.Write(data)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(n)), nil
		})
	case "seek":
		return objects.NewBuiltinFunction("seek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("TypeError: seek() takes 1 or 2 arguments (%d given)", len(args))
			}
			pos, err := intArg(args[0], "seek")
			if err != nil {
				return nil, err
			}
			whence := 0
			if len(args) == 2 {
				w, err := intArg(args[1], "seek")
				if err != nil {
					return nil, err
				}
				whence = w
			}
			out, err := fi.Seek(int64(pos), whence)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(out), nil
		})
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			pos, err := fi.Tell()
			if err != nil {
				return nil, err
			}
			return objects.NewInt(pos), nil
		})
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkClosed(); err != nil {
				return nil, err
			}
			if !fi.writable {
				return nil, errMode("writing")
			}
			cur, err := fi.Tell()
			if err != nil {
				return nil, err
			}
			size := cur
			if len(args) >= 1 && !objects.IsNone(args[0]) {
				v, err := intArg(args[0], "truncate")
				if err != nil {
					return nil, err
				}
				size = int64(v)
			}
			out, err := fi.Truncate(size)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(out), nil
		})
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.Close(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "flush":
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkClosed(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "fileno":
		return objects.NewBuiltinFunction("fileno", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			fd, err := fi.Fileno()
			if err != nil {
				return nil, err
			}
			return objects.NewInt(fd), nil
		})
	case "readable":
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkClosed(); err != nil {
				return nil, err
			}
			return objects.NewBool(fi.readable), nil
		})
	case "writable":
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkClosed(); err != nil {
				return nil, err
			}
			return objects.NewBool(fi.writable), nil
		})
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkClosed(); err != nil {
				return nil, err
			}
			return objects.NewBool(fi.isSeekable()), nil
		})
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkClosed(); err != nil {
				return nil, err
			}
			return objects.NewBool(false), nil
		})
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkClosed(); err != nil {
				return nil, err
			}
			return fi, nil
		})
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.Close(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	}
	return nil
}
