// Package _interpchannels is the Go port of CPython's _interpchannels C
// extension (Modules/_interpchannelsmodule.c), the low-level channel
// surface Lib/test/support/channels.py wraps. CPython moves data
// between isolated subinterpreters; gopy is single-interpreter, so a
// channel is simply a FIFO of object references guarded by a lock. The
// observable contract the standard library relies on (send then recv
// round-trips the value; recv on an empty channel either blocks,
// returns a default, or raises ChannelEmptyError) is preserved.
//
// CPython: Modules/_interpchannelsmodule.c
package _interpchannels

import (
	"fmt"
	"sync"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_interpchannels", buildModule)
}

// ChannelError is the base for every channel failure. CPython bases it
// on RuntimeError.
//
// CPython: Modules/_interpchannelsmodule.c:3500 state->ChannelError
var (
	ChannelError         = pyerrors.NewExcType("ChannelError", []*objects.Type{pyerrors.PyExc_RuntimeError})
	ChannelNotFoundError = pyerrors.NewExcType("ChannelNotFoundError", []*objects.Type{ChannelError})
	ChannelClosedError   = pyerrors.NewExcType("ChannelClosedError", []*objects.Type{ChannelError})
	ChannelEmptyError    = pyerrors.NewExcType("ChannelEmptyError", []*objects.Type{ChannelError})
	ChannelNotEmptyError = pyerrors.NewExcType("ChannelNotEmptyError", []*objects.Type{ChannelError})
)

// channel is one logical cross-interpreter channel: a buffer of object
// references plus the closed flags the high-level wrapper inspects.
//
// CPython: Modules/_interpchannelsmodule.c _channel_state
type channel struct {
	id        int64
	unboundop int64
	buf       []objects.Object
	closed    bool
	cond      *sync.Cond
}

var (
	mu       sync.Mutex
	channels = map[int64]*channel{}
	nextID   int64
)

func raise(t *objects.Type, msg string) error {
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

func lookup(id int64) (*channel, error) {
	ch, ok := channels[id]
	if !ok {
		return nil, raise(ChannelNotFoundError, fmt.Sprintf("channel %d not found", id))
	}
	return ch, nil
}

// create allocates a channel and returns its id.
//
// CPython: Modules/_interpchannelsmodule.c:2600 channel_create
func create(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	unboundop, _ := argInt(args, 0)
	mu.Lock()
	defer mu.Unlock()
	id := nextID
	nextID++
	ch := &channel{id: id, unboundop: unboundop}
	ch.cond = sync.NewCond(&mu)
	channels[id] = ch
	return objects.NewInt(id), nil
}

// channelID normalizes a channel id. CPython returns a ChannelID object
// tagged with a send/recv end; gopy keys everything on the bare integer,
// which still satisfies int()/hash()/== on the high-level wrapper.
//
// CPython: Modules/_interpchannelsmodule.c:2493 channelid_new
func channelID(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: _channel_id() requires an int id")
	}
	return objects.NewInt(id), nil
}

// send appends obj to the channel.
//
// CPython: Modules/_interpchannelsmodule.c:2680 channel_send
func send(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: send() requires a channel id")
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: send() missing 'obj'")
	}
	obj := args[1]
	mu.Lock()
	defer mu.Unlock()
	ch, err := lookup(id)
	if err != nil {
		return nil, err
	}
	if ch.closed {
		return nil, raise(ChannelClosedError, "channel closed")
	}
	ch.buf = append(ch.buf, obj)
	ch.cond.Signal()
	return objects.None(), nil
}

// recv pops the next object. recv(id) on an empty channel raises
// ChannelEmptyError; recv(id, default) returns (default, None) instead.
// The second tuple element is the "unbound replacement op", always None
// here because items never lose their interpreter in a single-VM build.
//
// CPython: Modules/_interpchannelsmodule.c:2740 channel_recv
func recv(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: recv() requires a channel id")
	}
	hasDefault := len(args) >= 2
	mu.Lock()
	defer mu.Unlock()
	ch, err := lookup(id)
	if err != nil {
		return nil, err
	}
	if len(ch.buf) == 0 {
		if hasDefault {
			return objects.NewTuple([]objects.Object{args[1], objects.None()}), nil
		}
		return nil, raise(ChannelEmptyError, "channel is empty")
	}
	obj := ch.buf[0]
	ch.buf = ch.buf[1:]
	return objects.NewTuple([]objects.Object{obj, objects.None()}), nil
}

// getInfo reports the channel's open/closed state and pending count.
//
// CPython: Modules/_interpchannelsmodule.c:2860 channel_get_info
func getInfo(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: get_info() requires a channel id")
	}
	mu.Lock()
	defer mu.Unlock()
	ch, err := lookup(id)
	if err != nil {
		return nil, err
	}
	ns := objects.NewNamespace()
	_ = objects.SetAttr(ns, objects.NewStr("open"), objects.NewBool(!ch.closed))
	_ = objects.SetAttr(ns, objects.NewStr("closing"), objects.NewBool(false))
	_ = objects.SetAttr(ns, objects.NewStr("closed"), objects.NewBool(ch.closed))
	_ = objects.SetAttr(ns, objects.NewStr("count"), objects.NewInt(int64(len(ch.buf))))
	return ns, nil
}

// closeChannel marks the channel closed.
//
// CPython: Modules/_interpchannelsmodule.c:2920 channel_close
func closeChannel(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	id, ok := argInt(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: close() requires a channel id")
	}
	mu.Lock()
	defer mu.Unlock()
	ch, err := lookup(id)
	if err != nil {
		return nil, err
	}
	ch.closed = true
	ch.cond.Broadcast()
	return objects.None(), nil
}

// listAll returns (cid, unboundop, None) for every open channel.
//
// CPython: Modules/_interpchannelsmodule.c:2640 channel_list_all
func listAll(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mu.Lock()
	defer mu.Unlock()
	var items []objects.Object
	for id := int64(0); id < nextID; id++ {
		ch, ok := channels[id]
		if !ok {
			continue
		}
		items = append(items, objects.NewTuple([]objects.Object{
			objects.NewInt(ch.id), objects.NewInt(ch.unboundop), objects.None(),
		}))
	}
	return objects.NewList(items), nil
}

// registerEndTypes is a no-op. CPython caches the SendChannel/RecvChannel
// classes so it can build end objects; gopy keys on bare ids and never
// needs them.
//
// CPython: Modules/_interpchannelsmodule.c:3360 _channels_register_end_types
func registerEndTypes(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

func fn(name string, f func([]objects.Object, map[string]objects.Object) (objects.Object, error)) *objects.BuiltinFunction {
	return objects.NewBuiltinFunction(name, f)
}

// buildModule materializes the _interpchannels module dict.
//
// CPython: Modules/_interpchannelsmodule.c:3470 module_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_interpchannels")
	d := m.Dict()
	entries := []struct {
		name string
		v    objects.Object
	}{
		{"ChannelError", ChannelError},
		{"ChannelNotFoundError", ChannelNotFoundError},
		{"ChannelClosedError", ChannelClosedError},
		{"ChannelEmptyError", ChannelEmptyError},
		{"ChannelNotEmptyError", ChannelNotEmptyError},
		{"create", fn("create", create)},
		{"_channel_id", fn("_channel_id", channelID)},
		{"send", fn("send", send)},
		{"recv", fn("recv", recv)},
		{"get_info", fn("get_info", getInfo)},
		{"close", fn("close", closeChannel)},
		{"list_all", fn("list_all", listAll)},
		{"_register_end_types", fn("_register_end_types", registerEndTypes)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.v); err != nil {
			return nil, err
		}
	}
	return m, nil
}
