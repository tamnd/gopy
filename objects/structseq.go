// StructSequence is the named-tuple flavor used by sys.flags,
// sys.version_info, os.stat_result, etc. Instances behave like tuples
// when iterated or indexed; the factory wires Getattro to map field
// names onto positional slots so attribute access lines up with
// CPython.
//
// CPython: Objects/structseq.c:48 PyStructSequence_NewType

package objects

import (
	"fmt"
	"strings"
)

// StructSeqField names one positional slot of a struct-sequence type.
// CPython carries an optional docstring per field; gopy stashes Doc on
// the Type rather than per-field, so this stays minimal.
//
// CPython: Include/structseq.h:21 PyStructSequence_Field
type StructSeqField struct {
	Name string
	Doc  string
}

// StructSeq is the per-instance value: a struct-sequence whose Type
// records the field layout and whose data is a flat slice of Objects
// the same length as Type.fieldNames.
//
// CPython: Objects/structseq.c:30 PyStructSequence
type StructSeq struct {
	VarHeader
	items []Object
}

// fieldNames hangs off the Type so Getattro and Setattro can find the
// index for an attribute name without re-walking the descriptor table.
// Stored on the Type pointer via a side map to avoid widening the
// shared Type struct for one specialized factory.
//
// CPython: Objects/structseq.c:42 PyStructSequence_Desc.fields
var structSeqFields = map[*Type][]string{}

// NewStructSeqType builds a struct-sequence type with the given name
// and ordered fields. Bases is [tuple] so isinstance(x, tuple) holds,
// matching CPython.
//
// CPython: Objects/structseq.c:431 PyStructSequence_NewType
func NewStructSeqType(name string, fields []StructSeqField) *Type {
	tp := NewType(name, []*Type{TupleType})
	tp.TpFlags = TpFlagSequence
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	structSeqFields[tp] = names

	tp.Repr = structSeqRepr
	tp.Str = structSeqRepr
	tp.Hash = structSeqHash
	tp.RichCmp = structSeqRichCmp
	tp.Iter = structSeqIter
	// Wholesale-replace the bundle (do not preserve Concat / Repeat /
	// Contains that inheritProtocolPointers copied from Tuple in
	// NewType). Those inherited slots assume `a.(*Tuple)` at the Go
	// level, which panics on a *StructSeq instance because gopy's
	// structseq has its own representation rather than embedding
	// PyVarObject the way CPython does. Concat / Repeat for structseq
	// would need to be re-ported against *StructSeq before they can
	// be inherited safely; that is out of P7.4's scope.
	tp.Sequence = &SequenceMethods{
		Length:  structSeqLen,
		GetItem: structSeqGetItem,
	}
	tp.Getattro = structSeqGetattro
	return tp
}

// NewStructSeq builds an instance of tp with values. Values must
// match the field count declared at NewStructSeqType time; mismatch
// is a programmer error and panics.
//
// CPython: Objects/structseq.c:113 PyStructSequence_New
func NewStructSeq(tp *Type, values []Object) *StructSeq {
	names := structSeqFields[tp]
	if len(values) != len(names) {
		panic(fmt.Sprintf("structseq %s: got %d values, want %d", tp.Name, len(values), len(names)))
	}
	s := &StructSeq{items: append([]Object(nil), values...)}
	s.init(tp)
	s.size = int64(len(values))
	return s
}

// Items returns the underlying slice. Callers must not mutate it.
func (s *StructSeq) Items() []Object { return s.items }

// AsTuple snapshots the struct-sequence into a plain tuple. CPython
// uses this for str/repr/hash via tp_as_sequence delegation; here we
// expose it so consumers that want a "real" tuple can drop the
// attribute facade.
//
// CPython: Objects/structseq.c:78 structseq_reduce (tuple build)
func (s *StructSeq) AsTuple() *Tuple {
	return NewTuple(s.items)
}

func structSeqLen(o Object) (int, error) {
	return len(o.(*StructSeq).items), nil
}

func structSeqGetItem(o Object, i int) (Object, error) {
	s := o.(*StructSeq)
	n := len(s.items)
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return nil, fmt.Errorf("IndexError: %s index out of range", o.Type().Name)
	}
	return s.items[i], nil
}

func structSeqIter(o Object) (Object, error) {
	return tupleIter(o.(*StructSeq).AsTuple())
}

func structSeqRepr(o Object) (string, error) {
	s := o.(*StructSeq)
	names := structSeqFields[o.Type()]
	parts := make([]string, len(s.items))
	for i, v := range s.items {
		r, err := Repr(v)
		if err != nil {
			return "", err
		}
		parts[i] = names[i] + "=" + r
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

func structSeqGetattro(o Object, name Object) (Object, error) {
	n, ok := name.(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	s := o.(*StructSeq)
	for i, fn := range structSeqFields[o.Type()] {
		if fn == n.v {
			return s.items[i], nil
		}
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", o.Type().Name, n.v)
}
