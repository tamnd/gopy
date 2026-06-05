// PickleBuffer is the gopy port of CPython's PyPickleBuffer object: a thin
// wrapper around a buffer-protocol exporter used to carry out-of-band data
// through protocol 5 pickling. It holds the wrapped object's buffer and
// redirects its own buffer requests to it, exposing raw() and release().
//
// CPython: Objects/picklebufobject.c PyPickleBuffer_Type
package _pickle

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// pickleBufferObject mirrors PyPickleBufferObject. base is the exporter the
// view was taken from (the Py_buffer's obj); nil once released. readonly
// follows the exporter so raw()/memoryview report the same writability.
//
// CPython: Objects/picklebufobject.c:7 PyPickleBufferObject
type pickleBufferObject struct {
	objects.Header
	base     objects.Object
	readonly bool
}

// PickleBufferType is the type singleton for pickle.PickleBuffer.
//
// CPython: Objects/picklebufobject.c:211 PyPickleBuffer_Type
var PickleBufferType = objects.NewType("pickle.PickleBuffer", []*objects.Type{objects.ObjectType()})

func init() {
	PickleBufferType.TpNew = pickleBufferNew
	// CPython: Objects/picklebufobject.c:95 picklebuf_traverse (Py_VISIT(view.obj))
	PickleBufferType.TpTraverse = pickleBufferTraverse
	PickleBufferType.Dealloc = pickleBufferDealloc
	objects.SetTypeDescr(PickleBufferType, "raw",
		objects.NewMethodDescr(PickleBufferType, "raw", pickleBufferRaw))
	objects.SetTypeDescr(PickleBufferType, "release",
		objects.NewMethodDescr(PickleBufferType, "release", pickleBufferRelease))

	// bf_getbuffer redirects to the wrapped exporter, so memoryview(pb) and
	// pb.raw() resolve to the original buffer.
	//
	// CPython: Objects/picklebufobject.c:123 picklebuf_getbuf
	objects.RegisterBufferHook(func(o objects.Object) (objects.BufferInfo, bool) {
		pb, ok := o.(*pickleBufferObject)
		if !ok || pb.base == nil {
			return objects.BufferInfo{}, false
		}
		buf, ok := objects.AsBytesLike(pb.base)
		if !ok {
			return objects.BufferInfo{}, false
		}
		return objects.BufferInfo{Buf: buf, Readonly: pb.readonly, Format: "B", Itemsize: 1}, true
	})
}

// pickleBufferNew implements PickleBuffer(base). It requires base to expose
// the buffer protocol (PyObject_GetBuffer with PyBUF_FULL_RO) and tracks the
// new wrapper with the collector so a cycle through it is reclaimable.
//
// CPython: Objects/picklebufobject.c:69 picklebuf_new
func pickleBufferNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: PickleBuffer() takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: PickleBuffer() takes exactly 1 argument (%d given)", len(args))
	}
	base := args[0]
	if _, ok := objects.AsBytesLike(base); !ok {
		return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", base.Type().Name)
	}
	objects.Incref(base)
	pb := &pickleBufferObject{base: base, readonly: bufferIsReadonly(base)}
	pb.Init(cls)
	if h := objects.GCTrackHook; h != nil {
		h(pb)
	}
	return pb, nil
}

// bufferIsReadonly reports whether base exports a read-only buffer (bytes,
// read-only memoryview). Writable exporters (bytearray, array.array) keep
// the wrapper writable.
func bufferIsReadonly(base objects.Object) bool {
	_, writable := objects.AsWritableBuffer(base)
	return !writable
}

// pickleBufferTraverse visits the wrapped exporter so the cycle collector
// can trace a reference loop that runs through a PickleBuffer.
//
// CPython: Objects/picklebufobject.c:95 picklebuf_traverse
func pickleBufferTraverse(o objects.Object, visit objects.Visitor) error {
	pb, ok := o.(*pickleBufferObject)
	if !ok || pb.base == nil {
		return nil
	}
	return visit(pb.base)
}

// pickleBufferDealloc releases the wrapped exporter, mirroring
// picklebuf_dealloc / PyBuffer_Release.
//
// CPython: Objects/picklebufobject.c:110 picklebuf_dealloc
func pickleBufferDealloc(o objects.Object) {
	pb, ok := o.(*pickleBufferObject)
	if !ok {
		return
	}
	if pb.base != nil {
		objects.Decref(pb.base)
		pb.base = nil
	}
}

// pickleBufferRaw implements PickleBuffer.raw(): a contiguous one-byte-format
// memoryview over the wrapped memory.
//
// CPython: Objects/picklebufobject.c:151 picklebuf_raw
func pickleBufferRaw(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: raw() takes no arguments (%d given)", len(args)-1)
	}
	pb, ok := args[0].(*pickleBufferObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'raw' requires a 'pickle.PickleBuffer' object")
	}
	if pb.base == nil {
		return nil, fmt.Errorf("ValueError: operation forbidden on released PickleBuffer object")
	}
	return objects.NewMemoryView(pb)
}

// pickleBufferRelease implements PickleBuffer.release(): drop the exporter.
//
// CPython: Objects/picklebufobject.c:192 picklebuf_release
func pickleBufferRelease(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: release() takes no arguments (%d given)", len(args)-1)
	}
	pb, ok := args[0].(*pickleBufferObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'release' requires a 'pickle.PickleBuffer' object")
	}
	if pb.base != nil {
		objects.Decref(pb.base)
		pb.base = nil
	}
	return objects.None(), nil
}
