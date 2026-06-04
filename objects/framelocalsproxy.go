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
	// CPython stamps Py_TPFLAGS_MAPPING on PyFrameLocalsProxy_Type so the
	// MATCH_MAPPING opcode treats f_locals as a mapping subject. Without
	// this bit, `match d: case {...}` for a FrameLocalsProxy falls into
	// the default arm.
	//
	// CPython: Objects/frameobject.c:786 PyFrameLocalsProxy_Type
	// (Py_TPFLAGS_DEFAULT | Py_TPFLAGS_HAVE_GC | Py_TPFLAGS_MAPPING)
	frameLocalsProxyType.TpFlags |= TpFlagMapping
	frameLocalsProxyType.Repr = frameLocalsProxyRepr
	frameLocalsProxyType.RichCmp = frameLocalsProxyRichCompare
	frameLocalsProxyType.Iter = frameLocalsProxyIter
	frameLocalsProxyType.TpNew = frameLocalsProxyTpNew
	// __new__ slot wrapper so cls.__new__(cls, ...) routes through
	// TpNew rather than falling back to objectNewBuiltin (which would
	// allocate an *Instance and bypass the frame-argument check). This
	// is also how copy.copy / copy.deepcopy / pickle hit the no-arg
	// rejection.
	//
	// CPython: Objects/typeobject.c:9952 tp_new_wrapper / add_tp_new_wrapper
	SetTypeDescr(frameLocalsProxyType, "__new__", NewBuiltinFunction("FrameLocalsProxy.__new__", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: FrameLocalsProxy.__new__(): not enough arguments")
		}
		cls, ok := args[0].(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: FrameLocalsProxy.__new__(X): X is not a type object (%s)", typeNameOf(args[0]))
		}
		return frameLocalsProxyTpNew(cls, args[1:], kwargs)
	}))
	frameLocalsProxyType.Mapping = &MappingMethods{
		Length:  frameLocalsProxyLen,
		GetItem: frameLocalsProxyGetItem,
		SetItem: frameLocalsProxySetItem,
		DelItem: frameLocalsProxyDelItem,
	}
	frameLocalsProxyType.Number = &NumberMethods{
		Or:        frameLocalsProxyOr,
		InPlaceOr: frameLocalsProxyInPlaceOr,
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

// frameLocalsProxyTpNew is FrameLocalsProxy's tp_new. Builds a proxy
// from a single frame argument; rejects keyword args and non-frame
// positional args, mirroring CPython's framelocalsproxy_new. The
// no-arg form raises TypeError, which is what makes copy.copy /
// copy.deepcopy / pickle of an f_locals proxy fail with TypeError
// (the reduce machinery hits cls.__new__(cls) with no extras).
//
// CPython: Objects/frameobject.c:418 framelocalsproxy_new
func frameLocalsProxyTpNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected 1 argument, got %d", len(args))
	}
	frame, ok := args[0].(*Frame)
	if !ok {
		return nil, fmt.Errorf("TypeError: expect frame, not %s", args[0].Type().Name)
	}
	return NewFrameLocalsProxy(frame), nil
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
// Hashes the key first so an unhashable key surfaces as TypeError
// (CPython returns -2 with the exception set). For exact str keys we
// shortcut on Go string equality; otherwise we hash every local name
// and fall back to RichCompareBool so str subclasses and __hash__/
// __eq__ impostors land in the correct fast slot. Errors during
// hashing or comparison propagate to the caller.
// read controls whether the value or just the slot is required.
// Returns the index, or -1 when the key has no live matching slot.
// An error return signals an unhashable key or an __eq__ that raised,
// which the caller must surface (CPython distinguishes -2 with a
// pending exception from -1).
//
// CPython: Objects/frameobject.c:94 framelocalsproxy_getkeyindex
//
//nolint:gocognit,gocyclo // mirrors framelocalsproxy_getkeyindex: fast-name scan plus hidden/extra-slot fallbacks
func framelocalsproxyGetKeyIndex(f *Frame, key Object, read bool) (int, error) {
	if f == nil || f.interp == nil {
		return -1, nil
	}
	code := f.interp.FrameCode()
	if code == nil {
		return -1, nil
	}
	if _, err := Hash(key); err != nil {
		return -2, err
	}
	checkSlot := func(i int) (int, bool) {
		if read {
			if framelocalsproxyHasval(f.interp, code, i) {
				return i, true
			}
			return -1, false
		}
		if code.LocalsplusKinds[i]&CoFastHidden == 0 {
			return i, true
		}
		return -1, false
	}
	if keyStr, ok := key.(*Unicode); ok {
		keyName := keyStr.v
		found := false
		for i, name := range code.LocalsplusNames {
			if i >= len(code.LocalsplusKinds) {
				break
			}
			if name != keyName {
				continue
			}
			if idx, hit := checkSlot(i); hit {
				return idx, nil
			}
			found = true
		}
		if found {
			return -1, nil
		}
	}
	keyHash, err := Hash(key)
	if err != nil {
		return -2, err
	}
	for i, name := range code.LocalsplusNames {
		if i >= len(code.LocalsplusKinds) {
			break
		}
		nameObj := NewStr(name)
		nameHash, herr := Hash(nameObj)
		if herr != nil {
			return -2, herr
		}
		if nameHash != keyHash {
			continue
		}
		eq, cerr := RichCmpBool(nameObj, key, CompareEQ)
		if cerr != nil {
			return -2, cerr
		}
		if !eq {
			continue
		}
		if idx, hit := checkSlot(i); hit {
			return idx, nil
		}
	}
	return -1, nil
}

// frameLocalsProxyGetItem implements proxy[key].
//
// CPython: Objects/frameobject.c:187 framelocalsproxy_getitem
func frameLocalsProxyGetItem(self, key Object) (Object, error) {
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	i, err := framelocalsproxyGetKeyIndex(p.frame, key, true)
	if err != nil {
		return nil, err
	}
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
	i, err := framelocalsproxyGetKeyIndex(p.frame, key, false)
	if err != nil {
		return err
	}
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
			// CPython: Py_XINCREF(value); PyCell_SetTakeRef(cell, value).
			// The cell takes a new owning reference to value and drops the
			// reference it held on the previous contents. Without the incref
			// the proxy would adopt the caller's stack reference, which
			// STORE_SUBSCR then closes, leaving the cell pointing at a value
			// whose only reference just went away.
			Incref(value)
			old := cell.Contents
			cell.Contents = value
			if old != nil {
				Decref(old)
			}
			return nil
		}
		// CPython: fast[i] = PyStackRef_FromPyObjectNew(value) after closing
		// the old slot. The identity guard mirrors CPython skipping the
		// store when value is already the slot's object.
		if value == oldVal {
			return nil
		}
		Incref(value)
		p.frame.interp.FrameSetLocalsPlusItem(i, value)
		if oldVal != nil {
			Decref(oldVal)
		}
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
	i, err := framelocalsproxyGetKeyIndex(p.frame, key, true)
	if err != nil {
		return false, err
	}
	if i >= 0 {
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

// CPython: Objects/frameobject.c:481 framelocalsproxy_richcompare
func frameLocalsProxyRichCompare(a, b Object, op CompareOp) (Object, error) {
	pa, ok := a.(*FrameLocalsProxy)
	if !ok {
		return NotImplemented(), nil
	}
	if pb, ok := b.(*FrameLocalsProxy); ok {
		if op != CompareEQ && op != CompareNE {
			return NotImplemented(), nil
		}
		eq := pa.frame == pb.frame
		if op == CompareNE {
			eq = !eq
		}
		return NewBool(eq), nil
	}
	if _, ok := asDictBacking(b); ok {
		return RichCmp(frameLocalsProxyAsDict(pa), b, op)
	}
	return NotImplemented(), nil
}

// frameLocalsProxyRepr renders the proxy as the dict view of its keys.
// Py_ReprEnter / Py_ReprLeave guard cycles so a proxy that stores
// itself under one of its keys renders as "{...}" instead of recursing
// forever when the materialized dict tries to repr the same proxy.
//
// CPython: Objects/frameobject.c:512 framelocalsproxy_repr
func frameLocalsProxyRepr(o Object) (string, error) {
	p, ok := o.(*FrameLocalsProxy)
	if !ok {
		return "", fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	if ReprEnter(p) {
		return "{...}", nil
	}
	defer ReprLeave(p)
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

func frameLocalsProxyKeysMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: keys() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: keys() takes no keyword arguments")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	return frameLocalsProxyKeysList(p), nil
}

func frameLocalsProxyValuesMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: values() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: values() takes no keyword arguments")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	return frameLocalsProxyValuesList(p), nil
}

func frameLocalsProxyItemsMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: items() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: items() takes no keyword arguments")
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
	i, err := framelocalsproxyGetKeyIndex(p.frame, key, false)
	if err != nil {
		return nil, err
	}
	if i >= 0 {
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
// CPython restricts other to dict or FrameLocalsProxy; anything else
// returns -1 and the caller converts that to TypeError. We mirror that
// rejection here so update([1, 2]) raises TypeError rather than
// IndexError from a downstream GetItem.
//
// CPython: Objects/frameobject.c:321 framelocalsproxy_merge
func frameLocalsProxyMerge(p *FrameLocalsProxy, other Object) error {
	if _, isDict := other.(*Dict); !isDict {
		if _, isProxy := other.(*FrameLocalsProxy); !isProxy {
			return errMergeUnsupported
		}
	}
	it, err := Iter(other)
	if err != nil {
		return err
	}
	for {
		k, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				break
			}
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

// errMergeUnsupported is the sentinel update() / |= raise when handed a
// non-dict, non-FrameLocalsProxy operand. Matches CPython's update()
// error message; |= masks it to its own TypeError.
//
// CPython: Objects/frameobject.c:729 framelocalsproxy_update
var errMergeUnsupported = fmt.Errorf("TypeError: update() argument must be dict or another FrameLocalsProxy")

// frameLocalsProxyOr implements __or__: builds a fresh dict from self
// and updates it from other. Returns NotImplemented when other is not
// a dict / FrameLocalsProxy so the binary-op machinery can try the
// reflected operand.
//
// CPython: Objects/frameobject.c:540 framelocalsproxy_or
func frameLocalsProxyOr(self, other Object) (Object, error) {
	if _, isDict := other.(*Dict); !isDict {
		if _, isProxy := other.(*FrameLocalsProxy); !isProxy {
			return NotImplemented(), nil
		}
	}
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return NotImplemented(), nil
	}
	result := frameLocalsProxyAsDict(p)
	if err := dictUpdateFrom(result, other); err != nil {
		return nil, err
	}
	return result, nil
}

// frameLocalsProxyInPlaceOr implements __ior__: merges other into self
// in place. Returns NotImplemented on type mismatch so |= falls back.
//
// CPython: Objects/frameobject.c:565 framelocalsproxy_inplace_or
func frameLocalsProxyInPlaceOr(self, other Object) (Object, error) {
	if _, isDict := other.(*Dict); !isDict {
		if _, isProxy := other.(*FrameLocalsProxy); !isProxy {
			return NotImplemented(), nil
		}
	}
	p, ok := self.(*FrameLocalsProxy)
	if !ok {
		return NotImplemented(), nil
	}
	if err := frameLocalsProxyMerge(p, other); err != nil {
		return NotImplemented(), nil //nolint:nilerr // framelocalsproxy_inplace_or returns NotImplemented when the merge fails
	}
	return p, nil
}

// dictUpdateFrom copies every (key, value) from src into dst. Used by
// frameLocalsProxyOr to mirror PyDict_Update on the freshly-allocated
// result dict.
//
// CPython: Objects/dictobject.c PyDict_Update
func dictUpdateFrom(dst *Dict, src Object) error {
	if d, ok := src.(*Dict); ok {
		for _, k := range d.Keys() {
			v, err := d.GetItem(k)
			if err != nil {
				return err
			}
			if v == nil {
				continue
			}
			if err := dst.SetItem(k, v); err != nil {
				return err
			}
		}
		return nil
	}
	if p, ok := src.(*FrameLocalsProxy); ok {
		keys := frameLocalsProxyKeysList(p)
		for i := 0; i < keys.Len(); i++ {
			k := keys.Item(i)
			v, err := frameLocalsProxyGetItem(p, k)
			if err != nil {
				return err
			}
			if err := dst.SetItem(k, v); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("TypeError: dict update from non-mapping")
}

func frameLocalsProxyCopyMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: copy() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: copy() takes no keyword arguments")
	}
	p, ok := args[0].(*FrameLocalsProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: FrameLocalsProxy expected")
	}
	return frameLocalsProxyAsDict(p), nil
}

// frameLocalsProxyReversedMethod returns the keys list reversed in
// place. Returns a list (not an iterator) because PyList_Reverse on
// the keys result is what CPython does.
//
// CPython: Objects/frameobject.c:866 framelocalsproxy_reversed
func frameLocalsProxyReversedMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reversed__() takes no arguments (%d given)", len(args)-1)
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: __reversed__() takes no keyword arguments")
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
	return rev, nil
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
