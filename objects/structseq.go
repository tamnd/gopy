// StructSequence is the named-tuple flavor used by sys.flags,
// time.struct_time, os.stat_result, etc. Instances behave like tuples
// when iterated or indexed; the factory installs one getset descriptor
// per named field so attribute access maps onto positional slots, and
// leaves tp_getattro nil so generic getattr still reaches the inherited
// tuple methods (index, count, __getitem__, ...) through the MRO.
//
// A struct-sequence distinguishes three kinds of field, exactly like
// CPython: visible fields live in the tuple (indices < n_in_sequence and
// counted by len()); hidden fields sit past n_in_sequence and are reached
// only by attribute name; unnamed fields occupy a visible slot but carry
// no name (PyStructSequence_UnnamedField), so they are reachable by index
// but not by attribute.
//
// CPython: Objects/structseq.c

package objects

import (
	"errors"
	"fmt"
	"strings"
)

// UnnamedField marks a struct-sequence field that has a positional index
// but no name. Set StructSeqField.Name to this sentinel for such a slot.
//
// CPython: Objects/structseq.c:25 PyStructSequence_UnnamedField
const UnnamedField = "unnamed field"

// StructSeqField names one positional slot of a struct-sequence type.
// CPython carries an optional docstring per field; gopy stashes Doc on
// the Type rather than per-field, so this stays minimal.
//
// CPython: Include/structseq.h:21 PyStructSequence_Field
type StructSeqField struct {
	Name string
	Doc  string
}

// StructSeqDesc describes a struct-sequence type: its name, the full
// ordered field list (visible + hidden), and how many leading fields are
// part of the tuple sequence (NInSequence). Mirrors PyStructSequence_Desc.
//
// CPython: Include/structseq.h:27 PyStructSequence_Desc
type StructSeqDesc struct {
	Name        string
	Fields      []StructSeqField
	NInSequence int
}

// StructSeq is the per-instance value: a struct-sequence whose Type
// records the field layout and whose items slice holds every field,
// visible and hidden. The visible length (Py_SIZE) is the meta's
// nInSequence; hidden fields trail after and are reached by name only.
//
// CPython: Objects/structseq.c:30 PyStructSequence
type StructSeq struct {
	VarHeader
	items []Object
}

// structSeqMeta is the per-type layout record. CPython stashes the same
// data as n_sequence_fields / n_fields / n_unnamed_fields entries in the
// type dict plus the PyMemberDef table; gopy keeps a side map keyed by
// the Type pointer so the shared Type struct stays narrow.
//
// CPython: Objects/structseq.c:478 initialize_structseq_dict
type structSeqMeta struct {
	fields      []StructSeqField // every field, length == n_fields
	nInSequence int              // visible length (Py_SIZE), == n_sequence_fields
	nUnnamed    int              // count of UnnamedField entries
}

var structSeqMetas = map[*Type]*structSeqMeta{}

// NewStructSeqType builds a struct-sequence type whose fields are all
// visible (n_in_sequence == len(fields)). This is the common case for
// types with no hidden fields (sys.flags, os.terminal_size).
//
// CPython: Objects/structseq.c:431 PyStructSequence_NewType
func NewStructSeqType(name string, fields []StructSeqField) *Type {
	return NewStructSeqTypeDesc(StructSeqDesc{
		Name:        name,
		Fields:      fields,
		NInSequence: len(fields),
	})
}

// NewStructSeqTypeDesc builds a struct-sequence type from a full
// descriptor, honoring the visible/hidden/unnamed field split. Bases is
// [tuple] so isinstance(x, tuple) holds, matching CPython.
//
// CPython: Objects/structseq.c:386 _PyStructSequence_InitBuiltinWithFlags
func NewStructSeqTypeDesc(desc StructSeqDesc) *Type {
	tp := NewType(desc.Name, []*Type{TupleType})
	tp.TpFlags |= TpFlagSequence
	// PyStructSequence_NewType builds a heap type, so Python code may set
	// attributes on it (test_reference_cycle does `type(t).x = t`). Clear the
	// immutable bit NewType stamps on static built-ins.
	//
	// CPython: Objects/structseq.c:386 _PyStructSequence_InitBuiltinWithFlags
	tp.TpFlags &^= TpFlagImmutable

	meta := &structSeqMeta{
		fields:      desc.Fields,
		nInSequence: desc.NInSequence,
	}
	for _, f := range desc.Fields {
		if f.Name == UnnamedField {
			meta.nUnnamed++
		}
	}
	structSeqMetas[tp] = meta

	tp.Repr = structSeqRepr
	tp.Str = structSeqRepr
	tp.Hash = structSeqHash
	tp.RichCmp = structSeqRichCmp
	tp.Iter = structSeqIter
	tp.TpNew = structSeqNew
	// Inherit Tuple's sq_concat / sq_repeat / sq_contains / sq_item and
	// mp_subscript from NewType's inheritProtocolPointers. Those slots read
	// their operand through tupleItemsOf, which returns the visible slice for
	// a *StructSeq, exactly as CPython's tuple slots see only Py_SIZE() items.
	// We leave tp_getattro nil so generic getattr walks the MRO: it finds the
	// field descriptors below and also reaches tuple's inherited __getitem__,
	// index, count, etc.
	//
	// CPython: Objects/structseq.c:534 initialize_members (PyMemberDef table)
	for i, f := range desc.Fields {
		if f.Name == UnnamedField {
			continue
		}
		idx := i
		fname := f.Name
		SetTypeDescr(tp, fname, NewGetSetDescr(fname,
			func(o Object) (Object, error) {
				s, ok := o.(*StructSeq)
				if !ok {
					return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", fname, desc.Name, typeNameOf(o))
				}
				return s.items[idx], nil
			},
			// CPython exposes struct-sequence fields as READONLY members, so
			// assigning to one raises AttributeError("readonly attribute") from
			// PyMember_SetOne rather than silently succeeding.
			//
			// CPython: Python/structmember.c:226 PyMember_SetOne (READONLY)
			func(_ Object, _ Object) error {
				return errors.New("AttributeError: readonly attribute")
			}))
	}
	// Route instance attribute writes through generic setattr so the field
	// descriptors above are consulted (their readonly setter fires) and
	// unknown names fall through to the no-__dict__ AttributeError.
	//
	// CPython: Objects/structseq.c uses the default tp_setattro (object's),
	// which is PyObject_GenericSetAttr.
	tp.Setattro = GenericSetAttr

	// Class attributes mirror CPython's type-dict entries so Python code can
	// read len-vs-real layout and structural pattern matching works.
	//
	// CPython: Objects/structseq.c:495 SET_DICT_FROM_SIZE + match_args
	SetTypeDescr(tp, "n_sequence_fields", NewInt(int64(meta.nInSequence)))
	SetTypeDescr(tp, "n_fields", NewInt(int64(len(meta.fields))))
	SetTypeDescr(tp, "n_unnamed_fields", NewInt(int64(meta.nUnnamed)))
	matchNames := make([]Object, 0, meta.nInSequence)
	for i := range meta.nInSequence {
		if meta.fields[i].Name == UnnamedField {
			continue
		}
		matchNames = append(matchNames, NewStr(meta.fields[i].Name))
	}
	SetTypeDescr(tp, "__match_args__", NewTuple(matchNames))

	SetTypeDescr(tp, "__reduce__", NewMethodDescr(tp, "__reduce__", structSeqReduceMethod))
	SetTypeDescr(tp, "__replace__", NewMethodDescr(tp, "__replace__", structSeqReplaceMethod))
	return tp
}

// NewStructSeq builds an instance of tp with values. Values must match
// the full field count (n_fields) declared at type-creation time; the
// visible length (Py_SIZE) is set from the type's n_in_sequence so hidden
// fields do not appear to len()/iteration. Mismatch is a programmer error
// and panics.
//
// CPython: Objects/structseq.c:60 PyStructSequence_New
func NewStructSeq(tp *Type, values []Object) *StructSeq {
	meta := structSeqMetas[tp]
	if meta == nil {
		panic(fmt.Sprintf("structseq %s: type not registered", tp.Name))
	}
	if len(values) != len(meta.fields) {
		panic(fmt.Sprintf("structseq %s: got %d values, want %d", tp.Name, len(values), len(meta.fields)))
	}
	s := &StructSeq{items: append([]Object(nil), values...)}
	s.init(tp)
	s.size = int64(meta.nInSequence)
	if h := GCTrackHook; h != nil {
		h(s)
	}
	return s
}

// Items returns the underlying slice (all fields). Callers must not
// mutate it.
func (s *StructSeq) Items() []Object { return s.items }

// visible returns the slice of fields that participate in the tuple
// sequence (indices < n_in_sequence). CPython exposes exactly these
// through Py_SIZE(); tuple protocol slots read them via tupleItemsOf.
func (s *StructSeq) visible() []Object {
	meta := structSeqMetas[s.Type()]
	if meta == nil {
		return s.items
	}
	return s.items[:meta.nInSequence]
}

// AsTuple snapshots the visible fields into a plain tuple. CPython uses
// this for str/repr/hash via tp_as_sequence delegation; here we expose it
// so consumers that want a "real" tuple can drop the attribute facade.
//
// CPython: Objects/structseq.c:356 structseq_reduce (tuple build)
func (s *StructSeq) AsTuple() *Tuple {
	return NewTuple(s.visible())
}

func structSeqIter(o Object) (Object, error) {
	return tupleIter(o.(*StructSeq).AsTuple())
}

// structSeqRepr renders only the visible named fields, "Name(a=1, b=2)".
// CPython iterates VISIBLE_SIZE(obj), so hidden fields like struct_time's
// tm_zone never appear.
//
// CPython: Objects/structseq.c:270 structseq_repr
func structSeqRepr(o Object) (string, error) {
	s := o.(*StructSeq)
	meta := structSeqMetas[o.Type()]
	parts := make([]string, 0, meta.nInSequence)
	for i := range meta.nInSequence {
		r, err := Repr(s.items[i])
		if err != nil {
			return "", err
		}
		parts = append(parts, meta.fields[i].Name+"="+r)
	}
	return o.Type().Name + "(" + strings.Join(parts, ", ") + ")", nil
}

func structSeqHash(o Object) (int64, error) {
	return tupleHash(o.(*StructSeq).AsTuple())
}

func structSeqRichCmp(a, b Object, op CompareOp) (Object, error) {
	at, ok := a.(*StructSeq)
	if !ok {
		return NotImplemented(), nil
	}
	switch other := b.(type) {
	case *StructSeq:
		return tupleRichCmp(at.AsTuple(), other.AsTuple(), op)
	case *Tuple:
		return tupleRichCmp(at.AsTuple(), other, op)
	}
	return NotImplemented(), nil
}

// structSeqParseNewArgs resolves the clinic signature
// structseq.__new__(sequence, dict={}): the sequence and optional dict
// may be passed positionally or by keyword, and a name given both ways
// is rejected. Returns the sequence (required) and dict (may be nil).
//
// CPython: Objects/structseq.c:157 structseq.__new__
func structSeqParseNewArgs(cls *Type, args []Object, kwargs map[string]Object) (Object, Object, error) {
	var seqArg, dictArg Object
	switch len(args) {
	case 0:
		if v, ok := kwargs["sequence"]; ok {
			seqArg = v
		}
	case 1:
		seqArg = args[0]
	case 2:
		seqArg, dictArg = args[0], args[1]
	default:
		return nil, nil, fmt.Errorf("TypeError: %s() takes at most 2 arguments (%d given)", cls.Name, len(args))
	}
	for k := range kwargs {
		switch k {
		case "sequence":
			if len(args) >= 1 {
				return nil, nil, fmt.Errorf("TypeError: argument for %s() given by name ('sequence') and position (1)", cls.Name)
			}
		case "dict":
			if len(args) == 2 {
				return nil, nil, fmt.Errorf("TypeError: argument for %s() given by name ('dict') and position (2)", cls.Name)
			}
			dictArg = kwargs["dict"]
		default:
			return nil, nil, fmt.Errorf("TypeError: %s() got an unexpected keyword argument '%s'", cls.Name, k)
		}
	}
	if seqArg == nil {
		return nil, nil, fmt.Errorf("TypeError: %s() missing required argument 'sequence' (pos 1)", cls.Name)
	}
	return seqArg, dictArg, nil
}

// structSeqBuildItems validates the sequence length against the type's
// visible/total field counts, copies the visible items, then fills the
// hidden fields from the override dict (defaulting absent ones to None).
// A dict carrying names that no hidden field consumed is rejected.
//
// CPython: Objects/structseq.c:186 structseq_new_impl (length + fill loop)
func structSeqBuildItems(cls *Type, meta *structSeqMeta, seq []Object, dict *Dict) ([]Object, error) {
	minLen := meta.nInSequence
	maxLen := len(meta.fields)
	n := len(seq)
	if minLen != maxLen {
		if n < minLen {
			return nil, fmt.Errorf("TypeError: %s() takes an at least %d-sequence (%d-sequence given)", cls.Name, minLen, n)
		}
		if n > maxLen {
			return nil, fmt.Errorf("TypeError: %s() takes an at most %d-sequence (%d-sequence given)", cls.Name, maxLen, n)
		}
	} else if n != minLen {
		return nil, fmt.Errorf("TypeError: %s() takes a %d-sequence (%d-sequence given)", cls.Name, minLen, n)
	}

	items := make([]Object, maxLen)
	for i := range n {
		items[i] = seq[i]
	}
	foundKeys := 0
	for i := n; i < maxLen; i++ {
		name := meta.fields[i].Name
		var ob Object
		if dict != nil && name != UnnamedField {
			v, err := dict.GetItem(NewStr(name))
			if err == nil {
				ob = v
				foundKeys++
			} else if !errors.Is(err, errKeyNotFound) {
				return nil, err
			}
		}
		if ob == nil {
			ob = None()
		}
		items[i] = ob
	}
	if dict != nil && dict.Len() > foundKeys {
		return nil, fmt.Errorf("TypeError: %s() got duplicate or unexpected field name(s)", cls.Name)
	}
	return items, nil
}

// structSeqNew is the struct-sequence tp_new: build an instance from a
// sequence plus an optional dict of hidden-field overrides.
//
// CPython: Objects/structseq.c:162 structseq_new_impl
func structSeqNew(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	meta := structSeqMetas[cls]
	if meta == nil {
		return nil, fmt.Errorf("TypeError: cannot create '%s' instances", cls.Name)
	}
	seqArg, dictArg, err := structSeqParseNewArgs(cls, args, kwargs)
	if err != nil {
		return nil, err
	}

	fast, err := SequenceFast(seqArg, "constructor requires a sequence")
	if err != nil {
		return nil, err
	}
	seq, _ := tupleItemsOf(fast)
	if seq == nil {
		if l, ok := fast.(*List); ok {
			seq = l.items
		}
	}
	// CPython's clinic default for dict is NULL (absent), not {}. An
	// explicitly supplied non-dict (including None) is rejected.
	var dict *Dict
	if dictArg != nil {
		d, ok := dictArg.(*Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: %s() takes a dict as second arg, if any", cls.Name)
		}
		dict = d
	}

	items, err := structSeqBuildItems(cls, meta, seq, dict)
	if err != nil {
		return nil, err
	}

	s := &StructSeq{items: items}
	s.init(cls)
	s.size = int64(meta.nInSequence)
	if h := GCTrackHook; h != nil {
		h(s)
	}
	return s, nil
}

// structSeqReduceMethod implements structseq.__reduce__ for pickling: it
// returns (type, (visible_tuple, hidden_dict)) so unpickling reconstructs
// the instance through structSeqNew.
//
// CPython: Objects/structseq.c:339 structseq_reduce
func structSeqReduceMethod(args []Object, _ map[string]Object) (Object, error) {
	self, ok := args[0].(*StructSeq)
	if !ok {
		return nil, fmt.Errorf("TypeError: __reduce__() requires a struct-sequence object")
	}
	meta := structSeqMetas[self.Type()]
	tup := NewTuple(self.items[:meta.nInSequence])
	dict := NewDict()
	for i := meta.nInSequence; i < len(meta.fields); i++ {
		name := meta.fields[i].Name
		if name == UnnamedField {
			continue
		}
		if err := dict.SetItem(NewStr(name), self.items[i]); err != nil {
			return nil, err
		}
	}
	inner := NewTuple([]Object{tup, dict})
	return NewTuple([]Object{self.Type(), inner}), nil
}

// structSeqReplaceMethod implements structseq.__replace__: return a copy
// with the named fields overridden by keyword arguments. Refused for
// types that carry unnamed fields.
//
// CPython: Objects/structseq.c:384 structseq_replace
func structSeqReplaceMethod(args []Object, kwargs map[string]Object) (Object, error) {
	self, ok := args[0].(*StructSeq)
	if !ok {
		return nil, fmt.Errorf("TypeError: __replace__() requires a struct-sequence object")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: __replace__() takes no positional arguments")
	}
	meta := structSeqMetas[self.Type()]
	if meta.nUnnamed > 0 {
		return nil, fmt.Errorf("TypeError: __replace__() is not supported for %s because it has unnamed field(s)", self.Type().Name)
	}

	items := append([]Object(nil), self.items...)
	used := 0
	for i := range meta.fields {
		if v, ok := kwargs[meta.fields[i].Name]; ok {
			items[i] = v
			used++
		}
	}
	if used < len(kwargs) {
		extra := make([]string, 0, len(kwargs))
		known := map[string]bool{}
		for _, f := range meta.fields {
			known[f.Name] = true
		}
		for k := range kwargs {
			if !known[k] {
				extra = append(extra, "'"+k+"'")
			}
		}
		return nil, fmt.Errorf("TypeError: Got unexpected field name(s): [%s]", strings.Join(extra, ", "))
	}

	s := &StructSeq{items: items}
	s.init(self.Type())
	s.size = int64(meta.nInSequence)
	if h := GCTrackHook; h != nil {
		h(s)
	}
	return s, nil
}
