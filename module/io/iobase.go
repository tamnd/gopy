// _IOBase and _RawIOBase abstract base classes.
// Mirrors Modules/_io/iobase.c which defines exactly these two types.
//
// CPython: Modules/_io/iobase.c:887 iobase_spec (_IOBase)
// CPython: Modules/_io/iobase.c:1050 rawiobase_spec (_RawIOBase)

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
	if iobaseIsClosed(o) {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
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
		return objects.NewBuiltinFunction("seek", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: seek")
		})
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			seekFn, err := objects.GetAttr(self, objects.NewStr("seek"))
			if err != nil {
				return nil, err
			}
			return objects.Call(seekFn, objects.NewTuple([]objects.Object{objects.NewInt(0), objects.NewInt(1)}), nil)
		})
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: truncate")
		})
	case "flush":
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if iobaseIsClosed(self) {
				return nil, fmt.Errorf("ValueError: I/O operation on closed file")
			}
			return objects.None(), nil
		})
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseCloseObj(self)
		})
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.False(), nil
		})
	case "readable":
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.False(), nil
		})
	case "writable":
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.False(), nil
		})
	case "fileno":
		return objects.NewBuiltinFunction("fileno", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: fileno")
		})
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := iobaseCheckClosed(self); err != nil {
				return nil, err
			}
			return objects.False(), nil
		})
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := iobaseCheckClosed(self); err != nil {
				return nil, err
			}
			return self, nil
		})
	case "__exit__":
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
		return objects.NewBuiltinFunction("_checkClosed", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := iobaseCheckClosed(self); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "_checkSeekable":
		return objects.NewBuiltinFunction("_checkSeekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseCheckCapability(self, "seekable", "File or stream is not seekable.")
		})
	case "_checkReadable":
		return objects.NewBuiltinFunction("_checkReadable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return iobaseCheckCapability(self, "readable", "File or stream is not readable.")
		})
	case "_checkWritable":
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
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return rawiobaseReadObj(self, args)
		})
	case "readall":
		return objects.NewBuiltinFunction("readall", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return rawiobaseReadallObj(self)
		})
	case "readinto":
		return objects.NewBuiltinFunction("readinto", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("NotImplementedError: readinto")
		})
	case "write":
		return objects.NewBuiltinFunction("write", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("NotImplementedError: write")
		})
	}
	return nil
}

// iobaseCloseObj closes self: flush then set __IOBase_closed = True.
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
	// Set __IOBase_closed sentinel in instance dict.
	if d := iobaseGetDict(self); d != nil {
		_ = d.SetItem(objects.NewStr(closedKey), objects.True())
	}
	return objects.None(), flushErr
}

// iobaseCheckCapability calls the named boolean method and raises UnsupportedOperation if it returns False.
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
		return nil, fmt.Errorf("UnsupportedOperation: %s", msg)
	}
	return objects.None(), nil
}

// iobaseReadlineObj reads one line using read(1) byte-by-byte.
//
// CPython: Modules/_io/iobase.c:567 _io__IOBase_readline_impl
func iobaseReadlineObj(self objects.Object, args []objects.Object) (objects.Object, error) {
	limit := -1
	if len(args) >= 1 {
		if n, ok := args[0].(*objects.Int); ok {
			v, _ := n.Int64()
			limit = int(v)
		}
	}
	readFn, err := objects.GetAttr(self, objects.NewStr("read"))
	if err != nil {
		return nil, err
	}
	var buf []byte
	for limit < 0 || len(buf) < limit {
		b, rerr := objects.Call(readFn, objects.NewTuple([]objects.Object{objects.NewInt(1)}), nil)
		if rerr != nil {
			return nil, rerr
		}
		bobj, ok := b.(*objects.Bytes)
		if !ok || bobj.Len() == 0 {
			break
		}
		buf = append(buf, bobj.Bytes()...)
		if buf[len(buf)-1] == '\n' {
			break
		}
	}
	return objects.NewBytes(buf), nil
}

// iobaseReadlinesObj collects all lines into a list.
//
// CPython: Modules/_io/iobase.c:715 _io__IOBase_readlines_impl
func iobaseReadlinesObj(self objects.Object, args []objects.Object) (objects.Object, error) {
	hint := -1
	if len(args) >= 1 {
		if n, ok := args[0].(*objects.Int); ok {
			v, _ := n.Int64()
			hint = int(v)
		}
	}
	result := objects.NewList(nil)
	var total int
	for {
		readlineFn, err := objects.GetAttr(self, objects.NewStr("readline"))
		if err != nil {
			return nil, err
		}
		line, err := objects.Call(readlineFn, objects.NewTuple(nil), nil)
		if err != nil {
			return nil, err
		}
		n, lerr := objects.Length(line)
		if lerr != nil {
			return nil, lerr
		}
		if n == 0 {
			break
		}
		result.Append(line)
		total += n
		if hint >= 0 && total >= hint {
			break
		}
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
