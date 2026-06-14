// Package _queue is the Go port of CPython's _queue C extension. It
// provides SimpleQueue, an unbounded, reentrant FIFO queue, and the
// Empty exception. Lib/queue.py imports both from here.
//
// CPython: Modules/_queuemodule.c
package _queue

import (
	"fmt"
	"sync"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_queue", buildModule)
}

// EmptyError is _queue.Empty, raised by get/get_nowait when no item is
// available.
//
// CPython: Modules/_queuemodule.c:594 state->EmptyError = PyErr_NewExceptionWithDoc("_queue.Empty", ...)
var EmptyError = pyerrors.NewExcType("Empty", []*objects.Type{pyerrors.PyExc_Exception})

// SimpleQueueType is _queue.SimpleQueue.
//
// CPython: Modules/_queuemodule.c:559 simplequeue_spec
var SimpleQueueType = newSimpleQueueType()

// SimpleQueue is the concrete Go shape backing a SimpleQueue instance.
// The C source stores items in a RingBuf; the Go port uses a slice
// guarded by a mutex plus a condition variable for blocking get.
//
// CPython: Modules/_queuemodule.c:30 simplequeueobject
type SimpleQueue struct {
	objects.Header
	mu   sync.Mutex
	cond *sync.Cond
	buf  []objects.Object
}

func newSimpleQueueType() *objects.Type {
	t := objects.NewType("SimpleQueue", []*objects.Type{objects.ObjectType()})
	// CPython: Modules/_queuemodule.c:559 simplequeue_spec name "_queue.SimpleQueue"
	t.Module = "_queue"
	t.TpNew = simpleQueueNew

	// CPython: Modules/_queuemodule.c:531 simplequeue_methods
	objects.SetTypeDescr(t, "put", objects.NewMethodDescr(t, "put", simpleQueuePut))
	objects.SetTypeDescr(t, "put_nowait", objects.NewMethodDescr(t, "put_nowait", simpleQueuePutNowait))
	objects.SetTypeDescr(t, "get", objects.NewMethodDescr(t, "get", simpleQueueGet))
	objects.SetTypeDescr(t, "get_nowait", objects.NewMethodDescr(t, "get_nowait", simpleQueueGetNowait))
	objects.SetTypeDescr(t, "empty", objects.NewMethodDescr(t, "empty", simpleQueueEmpty))
	objects.SetTypeDescr(t, "qsize", objects.NewMethodDescr(t, "qsize", simpleQueueQsize))
	objects.SetTypeDescr(t, "__class_getitem__", objects.NewClassMethod(
		objects.NewBuiltinFunction("__class_getitem__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __class_getitem__() takes exactly one argument")
			}
			cls, ok := args[0].(*objects.Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: __class_getitem__ requires a type")
			}
			return objects.NewGenericAlias(cls, args[1]), nil
		}),
	))
	return t
}

// simpleQueueNew is the tp_new slot.
//
// CPython: Modules/_queuemodule.c:249 simplequeue_new_impl
func simpleQueueNew(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	q := &SimpleQueue{}
	q.cond = sync.NewCond(&q.mu)
	q.Init(cls)
	return q, nil
}

func asSimpleQueue(name string, args []objects.Object) (*SimpleQueue, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of 'SimpleQueue' object needs an argument", name)
	}
	q, ok := args[0].(*SimpleQueue)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' requires a '_queue.SimpleQueue' object", name)
	}
	return q, nil
}

// simpleQueuePut implements SimpleQueue.put(item, block=True, timeout=None).
// The queue is unbounded, so put never blocks and the block/timeout
// arguments are ignored.
//
// CPython: Modules/_queuemodule.c:306 _queue_SimpleQueue_put_impl
func simpleQueuePut(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	q, err := asSimpleQueue("put", args)
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: put() missing 1 required positional argument: 'item'")
	}
	item := args[1]
	q.mu.Lock()
	q.buf = append(q.buf, item)
	q.cond.Signal()
	q.mu.Unlock()
	return objects.None(), nil
}

// simpleQueuePutNowait implements SimpleQueue.put_nowait(item).
//
// CPython: Modules/_queuemodule.c:341 _queue_SimpleQueue_put_nowait_impl
func simpleQueuePutNowait(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if _, err := asSimpleQueue("put_nowait", args); err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: put_nowait() missing 1 required positional argument: 'item'")
	}
	return simpleQueuePut(args[:2], kwargs)
}

// simpleQueueGet implements SimpleQueue.get(block=True, timeout=None).
// It returns the next item or raises Empty when the queue is empty and
// block is false.
//
// CPython: Modules/_queuemodule.c:379 _queue_SimpleQueue_get_impl
func simpleQueueGet(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	q, err := asSimpleQueue("get", args)
	if err != nil {
		return nil, err
	}
	block := true
	if len(args) >= 2 {
		b, err := objects.IsTruthy(args[1])
		if err != nil {
			return nil, err
		}
		block = b
	}
	q.mu.Lock()
	for len(q.buf) == 0 {
		if !block {
			q.mu.Unlock()
			return nil, emptyError()
		}
		q.cond.Wait()
	}
	item := q.buf[0]
	q.buf = q.buf[1:]
	q.mu.Unlock()
	return item, nil
}

// simpleQueueGetNowait implements SimpleQueue.get_nowait().
//
// CPython: Modules/_queuemodule.c:469 _queue_SimpleQueue_get_nowait_impl
func simpleQueueGetNowait(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	q, err := asSimpleQueue("get_nowait", args)
	if err != nil {
		return nil, err
	}
	return simpleQueueGet([]objects.Object{q, objects.False(), objects.None()}, nil)
}

// simpleQueueEmpty implements SimpleQueue.empty().
//
// CPython: Modules/_queuemodule.c:484 _queue_SimpleQueue_empty_impl
func simpleQueueEmpty(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	q, err := asSimpleQueue("empty", args)
	if err != nil {
		return nil, err
	}
	q.mu.Lock()
	empty := len(q.buf) == 0
	q.mu.Unlock()
	return objects.NewBool(empty), nil
}

// simpleQueueQsize implements SimpleQueue.qsize().
//
// CPython: Modules/_queuemodule.c:498 _queue_SimpleQueue_qsize_impl
func simpleQueueQsize(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	q, err := asSimpleQueue("qsize", args)
	if err != nil {
		return nil, err
	}
	q.mu.Lock()
	n := len(q.buf)
	q.mu.Unlock()
	return objects.NewInt(int64(n)), nil
}

// emptyError builds the Empty exception raised by get/get_nowait.
//
// CPython: Modules/_queuemodule.c:348 empty_error
func emptyError() error {
	exc := pyerrors.New(EmptyError, objects.NewTuple(nil))
	return objects.NewRaisedError(exc, "")
}

// buildModule materializes the _queue module dict.
//
// CPython: Modules/_queuemodule.c:600 queuemodule_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_queue")
	d := m.Dict()
	if err := d.SetItem(objects.NewStr("SimpleQueue"), SimpleQueueType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("Empty"), EmptyError); err != nil {
		return nil, err
	}
	return m, nil
}
