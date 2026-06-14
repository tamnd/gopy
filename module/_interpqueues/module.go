// Package _interpqueues is the Go port of CPython's _interpqueues C
// extension (Modules/_interpqueuesmodule.c), the low-level queue surface
// Lib/concurrent/interpreters/_queues.py wraps. As with channels, gopy
// is single-interpreter, so a queue is a bounded FIFO of object
// references guarded by a lock.
//
// CPython: Modules/_interpqueuesmodule.c
package _interpqueues

import (
	"fmt"
	"sync"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_interpqueues", buildModule)
}

// QueueError is the base for queue failures; CPython bases it on
// RuntimeError. QueueNotFoundError names a missing queue id.
//
// CPython: Modules/_interpqueuesmodule.c:1600 state->QueueError
var (
	QueueError         = pyerrors.NewExcType("QueueError", []*objects.Type{pyerrors.PyExc_RuntimeError})
	QueueNotFoundError = pyerrors.NewExcType("QueueNotFoundError", []*objects.Type{QueueError})
)

// queueEmptyType / queueFullType are the heap types the high-level
// wrapper hands us through _register_heap_types so get()/put() can raise
// the exact classes Lib/concurrent/interpreters/_queues.py defined.
//
// CPython: Modules/_interpqueuesmodule.c:1520 queuesmod_register_heap_types
var (
	queueEmptyType *objects.Type
	queueFullType  *objects.Type
)

type pqueue struct {
	id        int64
	maxsize   int64
	unboundop int64
	buf       []objects.Object
	bound     int64
}

var (
	mu     sync.Mutex
	queues = map[int64]*pqueue{}
	nextID int64
)

func raise(t *objects.Type, msg string) error {
	if t == nil {
		t = QueueError
	}
	exc := pyerrors.New(t, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
	return objects.NewRaisedError(exc, "")
}

func argInt(args []objects.Object, i int) (int64, bool) {
	if i >= len(args) {
		return 0, false
	}
	n, ok := args[i].(*objects.Int)
	if !ok {
		return 0, false
	}
	v, _ := n.Int64()
	return v, true
}

func lookup(id int64) (*pqueue, error) {
	q, ok := queues[id]
	if !ok {
		return nil, raise(QueueNotFoundError, fmt.Sprintf("queue %d not found", id))
	}
	return q, nil
}

// create allocates a queue and returns its id.
//
// CPython: Modules/_interpqueuesmodule.c:1180 queuesmod_create
func create(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	maxsize, _ := argInt(args, 0)
	unboundop, _ := argInt(args, 1)
	mu.Lock()
	defer mu.Unlock()
	id := nextID
	nextID++
	queues[id] = &pqueue{id: id, maxsize: maxsize, unboundop: unboundop, bound: 1}
	return objects.NewInt(id), nil
}

func listAll(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mu.Lock()
	defer mu.Unlock()
	var items []objects.Object
	for id := int64(0); id < nextID; id++ {
		q, ok := queues[id]
		if !ok {
			continue
		}
		items = append(items, objects.NewTuple([]objects.Object{
			objects.NewInt(q.id), objects.NewInt(q.unboundop), objects.None(),
		}))
	}
	return objects.NewList(items), nil
}

func bind(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: bind() requires a queue id")
	}
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	q.bound++
	return objects.None(), nil
}

func release(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: release() requires a queue id")
	}
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	q.bound--
	if q.bound <= 0 {
		delete(queues, id)
	}
	return objects.None(), nil
}

func getQueueDefaults(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: get_queue_defaults() requires a queue id")
	}
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{objects.NewInt(q.unboundop), objects.None()}), nil
}

func getMaxsize(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: get_maxsize() requires a queue id")
	}
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(q.maxsize), nil
}

func isFull(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: is_full() requires a queue id")
	}
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	full := q.maxsize > 0 && int64(len(q.buf)) >= q.maxsize
	return objects.NewBool(full), nil
}

func getCount(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: get_count() requires a queue id")
	}
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(len(q.buf))), nil
}

// put appends obj, raising the registered QueueFull when the bound is hit.
//
// CPython: Modules/_interpqueuesmodule.c:1300 queuesmod_put
func put(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: put() requires a queue id")
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: put() missing 'obj'")
	}
	obj := args[1]
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	if q.maxsize > 0 && int64(len(q.buf)) >= q.maxsize {
		return nil, raise(queueFullType, "queue is full")
	}
	q.buf = append(q.buf, obj)
	return objects.None(), nil
}

// get pops the next object, raising the registered QueueEmpty when empty.
// The second tuple element is the unbound replacement op, always None in
// a single-VM build.
//
// CPython: Modules/_interpqueuesmodule.c:1360 queuesmod_get
func get(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: get() requires a queue id")
	}
	mu.Lock()
	defer mu.Unlock()
	q, err := lookup(id)
	if err != nil {
		return nil, err
	}
	if len(q.buf) == 0 {
		return nil, raise(queueEmptyType, "queue is empty")
	}
	obj := q.buf[0]
	q.buf = q.buf[1:]
	return objects.NewTuple([]objects.Object{obj, objects.None()}), nil
}

// registerHeapTypes records the Queue/QueueEmpty/QueueFull classes the
// high-level wrapper defined so get()/put() raise the right exceptions.
//
// CPython: Modules/_interpqueuesmodule.c:1520 queuesmod_register_heap_types
func registerHeapTypes(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 2 {
		if t, ok := args[1].(*objects.Type); ok {
			queueEmptyType = t
		}
	}
	if len(args) >= 3 {
		if t, ok := args[2].(*objects.Type); ok {
			queueFullType = t
		}
	}
	return objects.None(), nil
}

func fn(name string, f func([]objects.Object, map[string]objects.Object) (objects.Object, error)) *objects.BuiltinFunction {
	return objects.NewBuiltinFunction(name, f)
}

// buildModule materializes the _interpqueues module dict.
//
// CPython: Modules/_interpqueuesmodule.c:1580 module_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_interpqueues")
	d := m.Dict()
	entries := []struct {
		name string
		v    objects.Object
	}{
		{"QueueError", QueueError},
		{"QueueNotFoundError", QueueNotFoundError},
		{"create", fn("create", create)},
		{"list_all", fn("list_all", listAll)},
		{"bind", fn("bind", bind)},
		{"release", fn("release", release)},
		{"get_queue_defaults", fn("get_queue_defaults", getQueueDefaults)},
		{"get_maxsize", fn("get_maxsize", getMaxsize)},
		{"is_full", fn("is_full", isFull)},
		{"get_count", fn("get_count", getCount)},
		{"put", fn("put", put)},
		{"get", fn("get", get)},
		{"_register_heap_types", fn("_register_heap_types", registerHeapTypes)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.v); err != nil {
			return nil, err
		}
	}
	return m, nil
}
