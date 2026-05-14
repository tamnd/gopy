// _IOBase and _RawIOBase abstract base classes.
// Mirrors Modules/_io/iobase.c which defines exactly these two types.
//
// CPython: Modules/_io/iobase.c:887 iobase_spec (_IOBase)
// CPython: Modules/_io/iobase.c:1050 rawiobase_spec (_RawIOBase)
//
// Not ported (intentional):
//   - iobase_finalize / _PyIOBase_finalize / iobase_dealloc (CPython:294, :334, :369).
//     Go owns instance lifetime through its GC; we have no tp_finalize hook, and
//     the warn-if-not-closed ResourceWarning machinery has no counterpart in the
//     gopy runtime. Subclasses that need cleanup (FileIO) handle it inline.
//   - iobase_traverse / iobase_clear (CPython:350, :359).
//     Reachability tracking is the Go GC's job; nothing to wire here.
//   - __weaklistoffset__ / __dictoffset__ members (CPython:866).
//     gopy does not expose weak-ref offsets, and dict access goes through
//     iobaseGetDict directly rather than a member descriptor.

package io

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// IOBaseType is the type singleton for _io._IOBase.
//
// CPython: Modules/_io/iobase.c:887 iobase_spec
var IOBaseType = objects.NewType("_io._IOBase", []*objects.Type{objects.ObjectType()})

// RawIOBaseType is the type singleton for _io._RawIOBase.
//
// CPython: Modules/_io/iobase.c:1050 rawiobase_spec
var RawIOBaseType = objects.NewType("_io._RawIOBase", []*objects.Type{IOBaseType})

// closedKey is the instance-dict sentinel CPython uses to mark a closed IOBase.
//
// CPython: Modules/_io/iobase.c:83 iobase_is_closed (&_Py_ID(__IOBase_closed))
const closedKey = "__IOBase_closed"

func init() {
	setupIOBaseType()
	setupRawIOBaseType()
}

func setupIOBaseType() {
	IOBaseType.Call = iobaseCall
	IOBaseType.Getattro = iobaseGetattro
	IOBaseType.Setattro = iobaseSetattro
	IOBaseType.Iter = iobaseIter
	IOBaseType.IterNext = iobaseIternext
}

func setupRawIOBaseType() {
	RawIOBaseType.Call = rawiobaseCall
	RawIOBaseType.Getattro = rawiobaseGetattro
	RawIOBaseType.Setattro = iobaseSetattro
	RawIOBaseType.Iter = iobaseIter
	RawIOBaseType.IterNext = iobaseIternext
}

// IOBase is the runtime object for _io._IOBase instances.
//
// CPython: Modules/_io/iobase.c:33 iobase struct
type IOBase struct {
	objects.Header
	dict *objects.Dict
}

// RawIOBase is the runtime object for _io._RawIOBase instances.
//
// CPython: Modules/_io/iobase.c:1050 rawiobase_spec
type RawIOBase struct {
	IOBase
}

func iobaseCall(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	b := &IOBase{dict: objects.NewDict()}
	b.Init(IOBaseType)
	return b, nil
}

func rawiobaseCall(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	b := &RawIOBase{}
	b.dict = objects.NewDict()
	b.Init(RawIOBaseType)
	return b, nil
}

// iobaseGetDict returns the instance dict if the object is an IOBase or RawIOBase.
func iobaseGetDict(o objects.Object) *objects.Dict {
	switch v := o.(type) {
	case *IOBase:
		return v.dict
	case *RawIOBase:
		return v.dict
	}
	return nil
}

// iobaseUnsupported returns the canonical UnsupportedOperation error.
//
// CPython: Modules/_io/iobase.c:87 iobase_unsupported
func iobaseUnsupported(op string) error {
	return fmt.Errorf("io.UnsupportedOperation: %s", op)
}

// iobaseIsClosed checks whether the internal __IOBase_closed sentinel is set.
//
// CPython: Modules/_io/iobase.c:81 iobase_is_closed
func iobaseIsClosed(o objects.Object) bool {
	d := iobaseGetDict(o)
	if d == nil {
		return false
	}
	v, err := d.GetItem(objects.NewStr(closedKey))
	if err != nil {
		return false
	}
	return objects.IsTrue(v)
}

// iobaseCheckClosed returns an error if the stream is closed.
//
// CPython: Modules/_io/iobase.c:196 iobase_check_closed
func iobaseCheckClosed(o objects.Object) error {
	// Prefer the derived `closed` attribute, mirroring CPython which calls
	// PyObject_GetOptionalAttr(self, "closed") rather than the internal flag.
	if attr, err := objects.GetAttr(o, objects.NewStr("closed")); err == nil && attr != nil {
		if objects.IsTrue(attr) {
			return fmt.Errorf("ValueError: I/O operation on closed file")
		}
		return nil
	}
	if iobaseIsClosed(o) {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// IOBaseCannotPickle is exported so fileio / bufferedio / textio / stringio /
// bytesio can install it as __getstate__ / __reduce__ / __reduce_ex__.
//
// CPython: Modules/_io/iobase.c:248 _PyIOBase_cannot_pickle
func IOBaseCannotPickle(self objects.Object) (objects.Object, error) {
	name := "_io._IOBase"
	if t := self.Type(); t != nil {
		name = t.Name
	}
	return nil, fmt.Errorf("TypeError: cannot pickle '%s' instances", name)
}

// iobaseGetattro dispatches attribute lookup for _IOBase objects.
//
// CPython: Modules/_io/iobase.c:860 iobase_getset + iobase_methods
func iobaseGetattro(o objects.Object, name objects.Object) (objects.Object, error) {
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	return iobaseAttr(o, n.Value())
}

func rawiobaseGetattro(o objects.Object, name objects.Object) (objects.Object, error) {
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	// Instance dict shadows the type methods so subclasses (and tests) can
	// override read / readall / readinto / write via setattr, mirroring
	// CPython's PyObject_GenericGetAttr where the instance __dict__ wins
	// over non-data descriptors on the type.
	if d := iobaseGetDict(o); d != nil {
		if v, err := d.GetItem(objects.NewStr(n.Value())); err == nil {
			return v, nil
		}
	}
	if fn := rawiobaseMethod(o.(*RawIOBase), n.Value()); fn != nil {
		return fn, nil
	}
	return iobaseAttr(o, n.Value())
}

func iobaseAttr(o objects.Object, name string) (objects.Object, error) {
	// closed property
	if name == "closed" {
		return objects.NewBool(iobaseIsClosed(o)), nil
	}
	// instance dict
	if d := iobaseGetDict(o); d != nil {
		v, err := d.GetItem(objects.NewStr(name))
		if err == nil {
			return v, nil
		}
	}
	// methods
	if fn := iobaseMethod(o, name); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '_io._IOBase' object has no attribute %q", name)
}

// iobaseSetattro stores an attribute into the instance dict.
func iobaseSetattro(o objects.Object, name objects.Object, value objects.Object) error {
	n, ok := name.(*objects.Unicode)
	if !ok {
		return fmt.Errorf("TypeError: attribute name must be string")
	}
	d := iobaseGetDict(o)
	if d == nil {
		return fmt.Errorf("AttributeError: cannot set attribute on _IOBase without dict")
	}
	if value == nil {
		return d.DelItem(objects.NewStr(n.Value()))
	}
	return d.SetItem(objects.NewStr(n.Value()), value)
}

// iobaseIter checks closed and returns self.
//
// CPython: Modules/_io/iobase.c:677 iobase_iter
func iobaseIter(o objects.Object) (objects.Object, error) {
	if err := iobaseCheckClosed(o); err != nil {
		return nil, err
	}
	return o, nil
}

// iobaseIternext reads the next line; returns ErrStopIteration on EOF.
//
// CPython: Modules/_io/iobase.c:686 iobase_iternext
func iobaseIternext(o objects.Object) (objects.Object, error) {
	readlineFn, err := objects.GetAttr(o, objects.NewStr("readline"))
	if err != nil {
		return nil, err
	}
	line, err := objects.Call(readlineFn, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	n, err := objects.Length(line)
	if err != nil || n == 0 {
		return nil, objects.ErrStopIteration
	}
	return line, nil
}

// iobaseMethod returns a bound method for the given name, or nil if not found.
//
// CPython: Modules/_io/iobase.c:831 iobase_methods
func iobaseMethod(self objects.Object, name string) objects.Object {
	switch name {
	case "seek":
		// CPython: Modules/_io/iobase.c:117 _io__IOBase_seek_impl
		return objects.NewBuiltinFunction("seek", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, iobaseUnsupported("seek")
		})
	case "tell":
		// CPython: Modules/_io/iobase.c:132 _io__IOBase_tell_impl
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			seekFn, err := objects.GetAttr(self, objects.NewStr("seek"))
			if err != nil {
				return nil, err
			}
			return objects.Call(seekFn, objects.NewTuple([]objects.Object{objects.NewInt(0), objects.NewInt(1)}), nil)
		})
	case "truncate":
		// CPython: Modules/_io/iobase.c:151 _io__IOBase_truncate_impl
		return objects.NewBuiltinFunction("truncate", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, iobaseUnsupported("truncate")
		})
	case "flush":
		// CPython: Modules/_io/iobase.c:170 _io__IOBase_flush_impl
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if iobaseIsClosed(self) {
				return nil, fmt.Errorf("ValueError: I/O operation on closed file")
			}
			return objects.None(), nil
		})
	case "close":
		// CPython: Modules/_io/iobase.c:268 _io__IOBase_close_impl
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseCloseObj(self)
		})
	case "seekable":
		// CPython: Modules/_io/iobase.c:405 _io__IOBase_seekable_impl
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.False(), nil
		})
	case "readable":
		// CPython: Modules/_io/iobase.c:437 _io__IOBase_readable_impl
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.False(), nil
		})
	case "writable":
		// CPython: Modules/_io/iobase.c:470 _io__IOBase_writable_impl
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.False(), nil
		})
	case "fileno":
		// CPython: Modules/_io/iobase.c:526 _io__IOBase_fileno_impl
		return objects.NewBuiltinFunction("fileno", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, iobaseUnsupported("fileno")
		})
	case "isatty":
		// CPython: Modules/_io/iobase.c:542 _io__IOBase_isatty_impl
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := iobaseCheckClosed(self); err != nil {
				return nil, err
			}
			return objects.False(), nil
		})
	case "__enter__":
		// CPython: Modules/_io/iobase.c:497 iobase_enter
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := iobaseCheckClosed(self); err != nil {
				return nil, err
			}
			return self, nil
		})
	case "__exit__":
		// CPython: Modules/_io/iobase.c:506 iobase_exit
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			closeFn, err := objects.GetAttr(self, objects.NewStr("close"))
			if err != nil {
				return nil, err
			}
			return objects.Call(closeFn, objects.NewTuple(nil), nil)
		})
	case "readline":
		return objects.NewBuiltinFunction("readline", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseReadlineObj(self, args)
		})
	case "readlines":
		return objects.NewBuiltinFunction("readlines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseReadlinesObj(self, args)
		})
	case "writelines":
		return objects.NewBuiltinFunction("writelines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseWritelinesObj(self, args)
		})
	case "_checkClosed":
		// CPython: Modules/_io/iobase.c:215 _PyIOBase_check_closed
		return objects.NewBuiltinFunction("_checkClosed", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := iobaseCheckClosed(self); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "_checkSeekable":
		// CPython: Modules/_io/iobase.c:412 _PyIOBase_check_seekable
		return objects.NewBuiltinFunction("_checkSeekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseCheckCapability(self, "seekable", "File or stream is not seekable.")
		})
	case "_checkReadable":
		// CPython: Modules/_io/iobase.c:445 _PyIOBase_check_readable
		return objects.NewBuiltinFunction("_checkReadable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseCheckCapability(self, "readable", "File or stream is not readable.")
		})
	case "_checkWritable":
		// CPython: Modules/_io/iobase.c:478 _PyIOBase_check_writable
		return objects.NewBuiltinFunction("_checkWritable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseCheckCapability(self, "writable", "File or stream is not writable.")
		})
	}
	return nil
}

// rawiobaseMethod returns bound methods for _RawIOBase (beyond IOBase ones).
//
// CPython: Modules/_io/iobase.c:1035 rawiobase_methods
func rawiobaseMethod(self *RawIOBase, name string) objects.Object {
	switch name {
	case "read":
		// CPython: Modules/_io/iobase.c:920 _io__RawIOBase_read_impl
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return rawiobaseReadObj(self, args)
		})
	case "readall":
		// CPython: Modules/_io/iobase.c:968 _io__RawIOBase_readall_impl
		return objects.NewBuiltinFunction("readall", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return rawiobaseReadallObj(self)
		})
	case "readinto":
		// CPython: Modules/_io/iobase.c:1022 rawiobase_readinto
		return objects.NewBuiltinFunction("readinto", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("NotImplementedError: readinto")
		})
	case "write":
		// CPython: Modules/_io/iobase.c:1029 rawiobase_write
		return objects.NewBuiltinFunction("write", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("NotImplementedError: write")
		})
	}
	return nil
}

// iobaseCloseObj closes self: flush then set __IOBase_closed = True. If flush
// raises, that exception still propagates but the close sentinel is set first.
//
// CPython: Modules/_io/iobase.c:268 _io__IOBase_close_impl
func iobaseCloseObj(self objects.Object) (objects.Object, error) {
	if iobaseIsClosed(self) {
		return objects.None(), nil
	}
	var flushErr error
	if flushFn, err := objects.GetAttr(self, objects.NewStr("flush")); err == nil {
		_, flushErr = objects.Call(flushFn, objects.NewTuple(nil), nil)
	}
	if d := iobaseGetDict(self); d != nil {
		_ = d.SetItem(objects.NewStr(closedKey), objects.True())
	}
	if flushErr != nil {
		return nil, flushErr
	}
	return objects.None(), nil
}

// iobaseCheckCapability calls the named boolean method and raises
// UnsupportedOperation if it returns False.
//
// CPython: Modules/_io/iobase.c:411 _PyIOBase_check_seekable (and siblings)
func iobaseCheckCapability(self objects.Object, method, msg string) (objects.Object, error) {
	fn, err := objects.GetAttr(self, objects.NewStr(method))
	if err != nil {
		return nil, err
	}
	res, err := objects.Call(fn, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	if !objects.IsTrue(res) {
		return nil, iobaseUnsupported(msg)
	}
	return res, nil
}

// iobaseReadlineObj reads one line. If self has `peek`, use it to avoid the
// one-byte-at-a-time slow path; otherwise fall back to read(1).
//
// CPython: Modules/_io/iobase.c:567 _io__IOBase_readline_impl
func iobaseReadlineObj(self objects.Object, args []objects.Object) (objects.Object, error) {
	limit := -1
	if len(args) >= 1 && !objects.IsNone(args[0]) {
		if n, ok := args[0].(*objects.Int); ok {
			v, _ := n.Int64()
			limit = int(v)
		}
	}
	peekFn, _ := objects.GetAttr(self, objects.NewStr("peek"))
	readFn, err := objects.GetAttr(self, objects.NewStr("read"))
	if err != nil {
		return nil, err
	}
	var buf []byte
	for limit < 0 || len(buf) < limit {
		nreadahead := 1
		if peekFn != nil {
			peeked, perr := objects.Call(peekFn, objects.NewTuple([]objects.Object{objects.NewInt(1)}), nil)
			if perr != nil {
				return nil, perr
			}
			pb, ok := peeked.(*objects.Bytes)
			if !ok {
				return nil, fmt.Errorf("OSError: peek() should have returned a bytes object, not '%s'", peeked.Type().Name)
			}
			pbuf := pb.Bytes()
			if len(pbuf) > 0 {
				n := 0
				for n < len(pbuf) {
					if limit >= 0 && n >= limit {
						break
					}
					c := pbuf[n]
					n++
					if c == '\n' {
						break
					}
				}
				nreadahead = n
			}
		}
		b, rerr := objects.Call(readFn, objects.NewTuple([]objects.Object{objects.NewInt(int64(nreadahead))}), nil)
		if rerr != nil {
			return nil, rerr
		}
		bobj, ok := b.(*objects.Bytes)
		if !ok {
			return nil, fmt.Errorf("OSError: read() should have returned a bytes object, not '%s'", b.Type().Name)
		}
		if bobj.Len() == 0 {
			break
		}
		buf = append(buf, bobj.Bytes()...)
		if buf[len(buf)-1] == '\n' {
			break
		}
	}
	return objects.NewBytes(buf), nil
}

// iobaseReadlinesObj collects all lines into a list. With hint>0, stop once
// the next line would push the total past hint (matching CPython's
// `line_length > hint - length` break, which fires after the append).
//
// CPython: Modules/_io/iobase.c:715 _io__IOBase_readlines_impl
func iobaseReadlinesObj(self objects.Object, args []objects.Object) (objects.Object, error) {
	hint := -1
	if len(args) >= 1 && !objects.IsNone(args[0]) {
		if n, ok := args[0].(*objects.Int); ok {
			v, _ := n.Int64()
			hint = int(v)
		}
	}
	result := objects.NewList(nil)
	if hint <= 0 {
		iter, err := objects.Iter(self)
		if err != nil {
			return nil, err
		}
		for {
			line, ierr := objects.IterNext(iter)
			if ierr != nil {
				if errors.Is(ierr, objects.ErrStopIteration) {
					break
				}
				return nil, ierr
			}
			result.Append(line)
		}
		return result, nil
	}
	iter, err := objects.Iter(self)
	if err != nil {
		return nil, err
	}
	var length int
	for {
		line, ierr := objects.IterNext(iter)
		if ierr != nil {
			if errors.Is(ierr, objects.ErrStopIteration) {
				break
			}
			return nil, ierr
		}
		result.Append(line)
		lineLength, lerr := objects.Length(line)
		if lerr != nil {
			return nil, lerr
		}
		if lineLength > hint-length {
			break
		}
		length += lineLength
	}
	return result, nil
}

// iobaseWritelinesObj writes each item in lines by calling self.write(line).
//
// CPython: Modules/_io/iobase.c:789 _io__IOBase_writelines
func iobaseWritelinesObj(self objects.Object, args []objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: writelines() requires lines argument")
	}
	if err := iobaseCheckClosed(self); err != nil {
		return nil, err
	}
	writeFn, err := objects.GetAttr(self, objects.NewStr("write"))
	if err != nil {
		return nil, err
	}
	iter, err := objects.Iter(args[0])
	if err != nil {
		return nil, err
	}
	for {
		line, err := objects.IterNext(iter)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				break
			}
			return nil, err
		}
		if _, err = objects.Call(writeFn, objects.NewTuple([]objects.Object{line}), nil); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// rawiobaseReadObj calls readinto or readall.
//
// CPython: Modules/_io/iobase.c:920 _io__RawIOBase_read_impl
func rawiobaseReadObj(self objects.Object, args []objects.Object) (objects.Object, error) {
	n := -1
	if len(args) >= 1 {
		if iv, ok := args[0].(*objects.Int); ok {
			v, _ := iv.Int64()
			n = int(v)
		}
	}
	if n < 0 {
		readallFn, err := objects.GetAttr(self, objects.NewStr("readall"))
		if err != nil {
			return nil, err
		}
		return objects.Call(readallFn, objects.NewTuple(nil), nil)
	}
	readintoFn, err := objects.GetAttr(self, objects.NewStr("readinto"))
	if err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	bobj := objects.NewByteArray(buf)
	res, err := objects.Call(readintoFn, objects.NewTuple([]objects.Object{bobj}), nil)
	if err != nil || objects.IsNone(res) {
		return res, err
	}
	filled, ok := res.(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("ValueError: readinto must return int")
	}
	count, _ := filled.Int64()
	if count < 0 || int(count) > n {
		return nil, fmt.Errorf("ValueError: readinto returned %d outside buffer size %d", count, n)
	}
	return objects.NewBytes(buf[:count]), nil
}

// rawiobaseReadallObj reads until EOF using successive read() calls.
//
// CPython: Modules/_io/iobase.c:968 _io__RawIOBase_readall_impl
func rawiobaseReadallObj(self objects.Object) (objects.Object, error) {
	readFn, err := objects.GetAttr(self, objects.NewStr("read"))
	if err != nil {
		return nil, err
	}
	var chunks []byte
	for {
		data, err := objects.Call(readFn, objects.NewTuple([]objects.Object{objects.NewInt(DefaultBufferSize)}), nil)
		if err != nil {
			return nil, err
		}
		if objects.IsNone(data) {
			if len(chunks) == 0 {
				return data, nil
			}
			break
		}
		b, ok := data.(*objects.Bytes)
		if !ok {
			return nil, fmt.Errorf("TypeError: read() should return bytes")
		}
		if b.Len() == 0 {
			break
		}
		chunks = append(chunks, b.Bytes()...)
	}
	return objects.NewBytes(chunks), nil
}
