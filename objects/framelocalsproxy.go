// FrameLocalsProxy is the write-through mapping returned by
// frame.f_locals (PEP 667). Reads pull from the frame's LocalsPlus
// slots (unwrapping cells where appropriate); writes go straight back
// to the slot for known names, and to an extra-locals dict for
// unknown names so f_locals[name] = v sticks even when name is not
// part of the code object's LocalsplusNames.
//
// CPython: Objects/frameobject.c:24 PyFrameLocalsProxyObject
// CPython: PEP 667

package objects

import (
	"errors"
	"fmt"
)

// FrameLocalsProxy wraps a *Frame and implements the mutable mapping
// protocol on top of its fast locals.
//
// CPython: Objects/frameobject.c:24 PyFrameLocalsProxyObject
type FrameLocalsProxy struct {
	Header
	frame *Frame
}

// frameLocalsProxyType is the type singleton for FrameLocalsProxy.
//
// CPython: Objects/frameobject.c:786 PyFrameLocalsProxy_Type
var frameLocalsProxyType = NewType("FrameLocalsProxy", []*Type{objectType})

func init() {
	frameLocalsProxyType.Repr = frameLocalsProxyRepr
	frameLocalsProxyType.Iter = frameLocalsProxyIter
	frameLocalsProxyType.Mapping = &MappingMethods{
		Length:  frameLocalsProxyLen,
		GetItem: frameLocalsProxyGetItem,
		SetItem: frameLocalsProxySetItem,
		DelItem: frameLocalsProxyDelItem,
	}
	frameLocalsProxyType.Sequence = &SequenceMethods{
		Contains: frameLocalsProxyContains,
	}
	SetTypeDescr(frameLocalsProxyType, "__contains__",
		NewMethodDescr(frameLocalsProxyType, "__contains__", frameLocalsProxyContainsMethod))
	SetTypeDescr(frameLocalsProxyType, "__getitem__",
		NewMethodDescr(frameLocalsProxyType, "__getitem__", frameLocalsProxyGetItemMethod))
	SetTypeDescr(frameLocalsProxyType, "__setitem__",
		NewMethodDescr(frameLocalsProxyType, "__setitem__", frameLocalsProxySetItemMethod))
	SetTypeDescr(frameLocalsProxyType, "keys",
		NewMethodDescr(frameLocalsProxyType, "keys", frameLocalsProxyKeysMethod))
	SetTypeDescr(frameLocalsProxyType, "values",
		NewMethodDescr(frameLocalsProxyType, "values", frameLocalsProxyValuesMethod))
	SetTypeDescr(frameLocalsProxyType, "items",
		NewMethodDescr(frameLocalsProxyType, "items", frameLocalsProxyItemsMethod))
	SetTypeDescr(frameLocalsProxyType, "get",
		NewMethodDescr(frameLocalsProxyType, "get", frameLocalsProxyGetMethod))
	SetTypeDescr(frameLocalsProxyType, "setdefault",
		NewMethodDescr(frameLocalsProxyType, "setdefault", frameLocalsProxySetDefaultMethod))
	SetTypeDescr(frameLocalsProxyType, "pop",
		NewMethodDescr(frameLocalsProxyType, "pop", frameLocalsProxyPopMethod))
	SetTypeDescr(frameLocalsProxyType, "update",
		NewMethodDescr(frameLocalsProxyType, "update", frameLocalsProxyUpdateMethod))
	SetTypeDescr(frameLocalsProxyType, "copy",
		NewMethodDescr(frameLocalsProxyType, "copy", frameLocalsProxyCopyMethod))
	SetTypeDescr(frameLocalsProxyType, "__reversed__",
		NewMethodDescr(frameLocalsProxyType, "__reversed__", frameLocalsProxyReversedMethod))
	SetTypeDescr(frameLocalsProxyType, "__len__",
		NewMethodDescr(frameLocalsProxyType, "__len__", frameLocalsProxyLenMethod))
	SetTypeDescr(frameLocalsProxyType, "__iter__",
		NewMethodDescr(frameLocalsProxyType, "__iter__", frameLocalsProxyIterMethod))
}

// FrameLocalsProxyType returns the type singleton.
func FrameLocalsProxyType() *Type { return frameLocalsProxyType }

// NewFrameLocalsProxy wraps f in a proxy. PEP 667 makes f_locals a
// proxy so writes flow back to fast locals.
//
// CPython: Objects/frameobject.c:418 framelocalsproxy_new
func NewFrameLocalsProxy(f *Frame) *FrameLocalsProxy {
	p := &FrameLocalsProxy{frame: f}
	p.init(frameLocalsProxyType)
	return p
}

// framelocalsproxyGetval returns the value at LocalsPlus[i] with cell
// unwrap when the slot kind is CO_FAST_FREE or CO_FAST_CELL. Returns
// nil for unset slots.
//
// CPython: Objects/frameobject.c:42 framelocalsproxy_getval
func framelocalsproxyGetval(interp InterpreterFrame, code *Code, i int) Object {
	if interp == nil || code == nil {
		return nil
	}
	if i < 0 || i >= len(code.LocalsplusKinds) {
		return nil
	}
	kind := code.LocalsplusKinds[i]
	val := interp.FrameLocalsPlusItem(i)
	if val == nil {
		return nil
	}
	if kind&CoFastFree != 0 || kind&CoFastCell != 0 {
		if cell, ok := val.(*Cell); ok {
			if cell == nil || cell.Contents == nil {
				return nil
			}
			return cell.Contents
		}
	}
	return val
}

// framelocalsproxyHasval reports whether slot i carries a value.
//
// CPython: Objects/frameobject.c:84 framelocalsproxy_hasval
func framelocalsproxyHasval(interp InterpreterFrame, code *Code, i int) bool {
	return framelocalsproxyGetval(interp, code, i) != nil
}

// framelocalsproxyGetKeyIndex finds the LocalsPlus index for key.
// read controls whether the value or just the slot is required.
// Returns the index, or -1 when the key has no live matching slot.
//
// CPython: Objects/frameobject.c:94 framelocalsproxy_getkeyindex
func framelocalsproxyGetKeyIndex(f *Frame, key Object, read bool) int {
	if f == nil || f.interp == nil {
		return -1
	}
	code := f.interp.FrameCode()
	if code == nil {
		return -1
	}
	keyStr, ok := key.(*Unicode)
	if !ok {
		return -1
	}
	keyName := keyStr.v
	for i, name := range code.LocalsplusNames {
		if i >= len(code.LocalsplusKinds) {
			break
		}
		if name != keyName {
			continue
		}
		if read {
			if framelocalsproxyHasval(f.interp, code, i) {
				return i
			}
		} else {
			if code.LocalsplusKinds[i]&CoFastHidden == 0 {
				return i
			}
		}
	}
	return -1
}

// frameLocalsProxyGetItem implements proxy[key].
//
// CPython: Objects/frameobject.c:187 framelocalsproxy_getitem
func frameLocalsProxyGetItem(self, key Object) (Object, error) {
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	i := framelocalsproxyGetKeyIndex(p.frame, key, true)
	if i >= 0 {
		code := p.frame.interp.FrameCode()
		if val := framelocalsproxyGetval(p.frame.interp, code, i); val != nil {
			return val, nil
		}
	}
	if p.frame.extraLocals != nil {
		if v, err := p.frame.extraLocals.GetItem(key); err == nil {
			return v, nil
		} else if !errors.Is(err, errKeyNotFound) {
			return nil, err
		}
	}
	s, _ := Repr(key)
	return nil, fmt.Errorf("KeyError: %s", s)
}

// frameLocalsProxySetItem implements proxy[key] = value (and del on
// nil value).
//
// CPython: Objects/frameobject.c:246 framelocalsproxy_setitem
func frameLocalsProxySetItem(self, key, value Object) error {
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	i := framelocalsproxyGetKeyIndex(p.frame, key, false)
	if i >= 0 {
		if value == nil {
			return fmt.Errorf("ValueError: cannot remove local variables from FrameLocalsProxy")
		}
		code := p.frame.interp.FrameCode()
		kind := code.LocalsplusKinds[i]
		oldVal := p.frame.interp.FrameLocalsPlusItem(i)
		var cell *Cell
		switch {
		case kind&CoFastFree != 0:
			if c, ok2 := oldVal.(*Cell); ok2 {
				cell = c
			}
		case kind&CoFastCell != 0 && oldVal != nil:
			if c, ok2 := oldVal.(*Cell); ok2 {
				cell = c
			}
		}
		if cell != nil {
			cell.Contents = value
			return nil
		}
		p.frame.interp.FrameSetLocalsPlusItem(i, value)
		return nil
	}
	if value == nil {
		if p.frame.extraLocals != nil {
			return p.frame.extraLocals.DelItem(key)
		}
		s, _ := Repr(key)
		return fmt.Errorf("KeyError: %s", s)
	}
	if p.frame.extraLocals == nil {
		p.frame.extraLocals = NewDict()
	}
	return p.frame.extraLocals.SetItem(key, value)
}

// frameLocalsProxyDelItem implements del proxy[key].
func frameLocalsProxyDelItem(self, key Object) error {
	return frameLocalsProxySetItem(self, key, nil)
}

// frameLocalsProxyLen reports the number of live name/value entries.
//
// CPython: Objects/frameobject.c:682 framelocalsproxy_length
func frameLocalsProxyLen(self Object) (int, error) {
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return 0, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	code := p.frame.interp.FrameCode()
	n := 0
	if code != nil {
		for i := 0; i < len(code.LocalsplusKinds); i++ {
			if framelocalsproxyHasval(p.frame.interp, code, i) {
				n++
			}
		}
	}
	if p.frame.extraLocals != nil {
		n += p.frame.extraLocals.Len()
	}
	return n, nil
}

// frameLocalsProxyContains implements `key in proxy`.
//
// CPython: Objects/frameobject.c:702 framelocalsproxy_contains
func frameLocalsProxyContains(self, key Object) (bool, error) {
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return false, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	if i := framelocalsproxyGetKeyIndex(p.frame, key, true); i >= 0 {
		return true, nil
	}
	if p.frame.extraLocals != nil {
		return p.frame.extraLocals.Contains(key)
	}
	return false, nil
}

// frameLocalsProxyKeysList returns a list of names with live values.
//
// CPython: Objects/frameobject.c:370 framelocalsproxy_keys
func frameLocalsProxyKeysList(p *FrameLocalsProxy) *List {
	out := NewList(nil)
	code := p.frame.interp.FrameCode()
	if code != nil {
		for i, name := range code.LocalsplusNames {
			if i >= len(code.LocalsplusKinds) {
				break
			}
			if framelocalsproxyHasval(p.frame.interp, code, i) {
				out.Append(NewStr(name))
			}
		}
	}
	if p.frame.extraLocals != nil {
		for _, k := range p.frame.extraLocals.Keys() {
			out.Append(k)
		}
	}
	return out
}

// frameLocalsProxyValuesList walks live slots and returns their values.
//
// CPython: Objects/frameobject.c:580 framelocalsproxy_values
func frameLocalsProxyValuesList(p *FrameLocalsProxy) *List {
	out := NewList(nil)
	code := p.frame.interp.FrameCode()
	if code != nil {
		for i := 0; i < len(code.LocalsplusKinds); i++ {
			if v := framelocalsproxyGetval(p.frame.interp, code, i); v != nil {
				out.Append(v)
			}
		}
	}
	if p.frame.extraLocals != nil {
		for _, k := range p.frame.extraLocals.Keys() {
			if v, err := p.frame.extraLocals.GetItem(k); err == nil {
				out.Append(v)
			}
		}
	}
	return out
}

// frameLocalsProxyItemsList returns (name, value) tuples for live slots.
//
// CPython: Objects/frameobject.c:619 framelocalsproxy_items
func frameLocalsProxyItemsList(p *FrameLocalsProxy) *List {
	out := NewList(nil)
	code := p.frame.interp.FrameCode()
	if code != nil {
		for i, name := range code.LocalsplusNames {
			if i >= len(code.LocalsplusKinds) {
				break
			}
			if v := framelocalsproxyGetval(p.frame.interp, code, i); v != nil {
				out.Append(NewTuple([]Object{NewStr(name), v}))
			}
		}
	}
	if p.frame.extraLocals != nil {
		for _, k := range p.frame.extraLocals.Keys() {
			if v, err := p.frame.extraLocals.GetItem(k); err == nil {
				out.Append(NewTuple([]Object{k, v}))
			}
		}
	}
	return out
}

// frameLocalsProxyIter returns iter(keys()).
//
// CPython: Objects/frameobject.c:467 framelocalsproxy_iter
func frameLocalsProxyIter(self Object) (Object, error) {
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	keys := frameLocalsProxyKeysList(p)
	return Iter(keys)
}

// frameLocalsProxyRepr renders the proxy as the dict view of its keys.
//
// CPython: Objects/frameobject.c:512 framelocalsproxy_repr
func frameLocalsProxyRepr(o Object) (string, error) {
	p, ok := o.(*FrameLocalsProxy)
	if !ok {
		return "", fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	d := frameLocalsProxyAsDict(p)
	return Repr(d)
}

// frameLocalsProxyAsDict materializes the proxy into a plain dict.
// Used by repr() and copy().
//
// CPython: Objects/frameobject.c:512 framelocalsproxy_repr
// CPython: Objects/frameobject.c:849 framelocalsproxy_copy
func frameLocalsProxyAsDict(p *FrameLocalsProxy) *Dict {
	d := NewDict()
	code := p.frame.interp.FrameCode()
	if code != nil {
		for i, name := range code.LocalsplusNames {
			if i >= len(code.LocalsplusKinds) {
				break
			}
			if v := framelocalsproxyGetval(p.frame.interp, code, i); v != nil {
				_ = d.SetItem(NewStr(name), v)
			}
		}
	}
	if p.frame.extraLocals != nil {
		for _, k := range p.frame.extraLocals.Keys() {
			if v, err := p.frame.extraLocals.GetItem(k); err == nil {
				_ = d.SetItem(k, v)
			}
		}
	}
	return d
}

// Method-descriptor wrappers below.

func frameLocalsProxyContainsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __contains__ expects 1 argument")
	}
	ok, err := frameLocalsProxyContains(args[0], args[1])
	if err != nil {
		return nil, err
	}
	if ok {
		return True(), nil
	}
	return False(), nil
}

func frameLocalsProxyGetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __getitem__ expects 1 argument")
	}
	return frameLocalsProxyGetItem(args[0], args[1])
}

func frameLocalsProxySetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: __setitem__ expects 2 arguments")
	}
	if err := frameLocalsProxySetItem(args[0], args[1], args[2]); err != nil {
		return nil, err
	}
	return None(), nil
}

func frameLocalsProxyKeysMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: keys() missing self")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	return frameLocalsProxyKeysList(p), nil
}

func frameLocalsProxyValuesMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: values() missing self")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	return frameLocalsProxyValuesList(p), nil
}

func frameLocalsProxyItemsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: items() missing self")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	return frameLocalsProxyItemsList(p), nil
}

func frameLocalsProxyGetMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: get() expects 1 or 2 arguments")
	}
	def := None()
	if len(args) == 3 {
		def = args[2]
	}
	v, err := frameLocalsProxyGetItem(args[0], args[1])
	if err != nil {
		//nolint:nilerr // mirrors dict.get: missing key returns default without error.
		return def, nil
	}
	return v, nil
}

func frameLocalsProxySetDefaultMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: setdefault() expects 1 or 2 arguments")
	}
	def := None()
	if len(args) == 3 {
		def = args[2]
	}
	if v, err := frameLocalsProxyGetItem(args[0], args[1]); err == nil {
		return v, nil
	}
	if err := frameLocalsProxySetItem(args[0], args[1], def); err != nil {
		return nil, err
	}
	return def, nil
}

func frameLocalsProxyPopMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: pop() expects 1 or 2 arguments")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	key := args[1]
	if i := framelocalsproxyGetKeyIndex(p.frame, key, false); i >= 0 {
		return nil, fmt.Errorf("ValueError: cannot remove local variables from FrameLocalsProxy")
	}
	if p.frame.extraLocals != nil {
		if v, err := p.frame.extraLocals.GetItem(key); err == nil {
			if err := p.frame.extraLocals.DelItem(key); err != nil {
				return nil, err
			}
			return v, nil
		} else if !errors.Is(err, errKeyNotFound) {
			return nil, err
		}
	}
	if len(args) == 3 {
		return args[2], nil
	}
	s, _ := Repr(key)
	return nil, fmt.Errorf("KeyError: %s", s)
}

func frameLocalsProxyUpdateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: update() expects 1 argument")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	if err := frameLocalsProxyMerge(p, args[1]); err != nil {
		return nil, err
	}
	return None(), nil
}

// frameLocalsProxyMerge writes every (key, value) from other into self.
//
// CPython: Objects/frameobject.c:321 framelocalsproxy_merge
func frameLocalsProxyMerge(p *FrameLocalsProxy, other Object) error {
	it, err := Iter(other)
	if err != nil {
		return err
	}
	for {
		k, err := IterNext(it)
		if err != nil {
			return err
		}
		if k == nil {
			break
		}
		v, err := GetItem(other, k)
		if err != nil {
			return err
		}
		if err := frameLocalsProxySetItem(p, k, v); err != nil {
			return err
		}
	}
	return nil
}

func frameLocalsProxyCopyMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: copy() missing self")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	return frameLocalsProxyAsDict(p), nil
}

func frameLocalsProxyReversedMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __reversed__() missing self")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	keys := frameLocalsProxyKeysList(p)
	rev := NewList(nil)
	n := keys.Len()
	for i := n - 1; i >= 0; i-- {
		rev.Append(keys.Item(i))
	}
	return Iter(rev)
}

func frameLocalsProxyLenMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __len__ missing self")
	}
	n, err := frameLocalsProxyLen(args[0])
	if err != nil {
		return nil, err
	}
	return NewInt(int64(n)), nil
}

func frameLocalsProxyIterMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __iter__ missing self")
	}
	return frameLocalsProxyIter(args[0])
}
