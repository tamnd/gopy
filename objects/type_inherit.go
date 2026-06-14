// Port of CPython's inherit_slots / type_ready_inherit_as_structs.
// At type creation, every slot t left nil is filled from the closest
// MRO ancestor that defines it, so dispatch can be a single field read
// instead of a per-call MRO walk.
//
// CPython: Objects/typeobject.c:8227 inherit_slots /
// Objects/typeobject.c:8685 type_ready_inherit_as_structs /
// Objects/typeobject.c:8712 type_ready_inherit

package objects

// inheritPatmaFlagsAllMRO walks t's MRO and propagates the first
// COLLECTION_FLAGS bit it finds. Matches CPython's inherit_patma_flags
// guard ("if no flags set yet") so the FIRST base in MRO order with a
// SEQUENCE / MAPPING bit wins. Without this, `class M(UserDict,
// Sequence)` would OR both bits and turn match-class arms ambiguous.
//
// CPython: Objects/typeobject.c:8705 inherit_patma_flags /
// Objects/typeobject.c:8712 type_ready_inherit (per-MRO call)
func inheritPatmaFlagsAllMRO(t *Type) {
	const collFlags = TpFlagSequence | TpFlagMapping
	if t.TpFlags&collFlags != 0 {
		return
	}
	if len(t.MRO) < 2 {
		return
	}
	for _, anc := range t.MRO[1:] {
		if anc == nil {
			continue
		}
		bits := anc.TpFlags & collFlags
		if bits != 0 {
			t.TpFlags |= bits
			return
		}
	}
}

// inheritSlotsAllMRO is the gopy port of CPython's type_ready_inherit
// minus the parts that have no analog (tp_flags inheritance, GC
// flag handling, vectorcall flag).
//
// CPython walks the full MRO and uses SLOTDEFINED (base->SLOT !=
// basebase->SLOT) to know which ancestor owns a slot. gopy cannot
// replicate that ownership test because Go function values only
// compare to nil. The compromise:
//
//   - Protocol bundles (Number / Async / Sequence / Mapping) are
//     filled by walking the MRO and copying any nil field on t from
//     the corresponding bundle field on each ancestor. This is what
//     spec 1712 P7.2 cares about: numberSlot becomes a single field
//     load instead of a per-dispatch MRO walk.
//
//   - Scalar slots (Dealloc, Repr, Call, Hash, TpNew, ...) are NOT
//     touched here. Built-in types intentionally leave TpNew / Call
//     nil and route construction through dedicated entry points
//     (typeCall, builtin ctors). Pulling object.tp_new into every
//     subclass via inheritance would turn `class Meta(type)` and
//     every Exception subclass into "() takes no arguments". Scalar
//     inheritance happens inside NewUserType via
//     inheritDirectBaseScalars on the post-namespace fixup pass.
//
// CPython: Objects/typeobject.c:8712 type_ready_inherit
func inheritSlotsAllMRO(t *Type) {
	if len(t.MRO) < 2 {
		return
	}
	for _, anc := range t.MRO[1:] {
		if anc == nil {
			continue
		}
		inheritBundleSlotsFromAncestor(t, anc)
	}
	if len(t.Bases) > 0 && t.Bases[0] != nil {
		inheritProtocolPointers(t, t.Bases[0])
	}
	// Scalar slots like RichCmp are only copied from Bases[0] in
	// inheritProtocolPointers. When Bases[0] has nil RichCmp but a later MRO
	// entry (e.g. a mixin that defines __eq__) has it set, scan the full MRO.
	//
	// CPython: Objects/typeobject.c:8227 inherit_slots SLOTDEFINED check
	// walks all ancestors for each slot.
	if t.RichCmp == nil && !typeOverridesHash(t) {
		for _, anc := range t.MRO[1:] {
			if anc != nil && anc.RichCmp != nil {
				t.RichCmp = anc.RichCmp
				if t.Hash == nil {
					t.Hash = anc.Hash
				}
				break
			}
		}
	}
}

// inheritBundleSlotsFromAncestor walks one MRO entry and fills in
// per-bundle slot pointers (Number / Async / Sequence / Mapping) on t
// from base. Only protocol-bundle slots are MRO-walked; scalar slots
// stay on direct-base inheritance via inheritDirectBaseScalars to
// preserve gopy's Call-based construction fallback for types that
// intentionally leave TpNew / Call nil.
//
// CPython: Objects/typeobject.c:8227 inherit_slots (COPYNUM /
// COPYASYNC / COPYSEQ / COPYMAP blocks)
func inheritBundleSlotsFromAncestor(t, base *Type) {
	// CPython: Objects/typeobject.c:8227 COPYNUM — individual slots are
	// copied even if tp_as_number was NULL on the child; we lazily
	// allocate the bundle so a class that inherits __int__ (etc.) from a
	// grandparent across a mixed MRO picks up the slot.
	if base.Number != nil {
		if t.Number == nil {
			t.Number = &NumberMethods{}
		}
		copyNumberSlots(t.Number, base.Number)
	}
	// Sequence / Mapping / Async are lazily allocated for the same reason
	// as Number above: a class whose only protocol slot comes from a
	// non-primary base across a mixed MRO (e.g. IntFlag inheriting
	// Flag.__contains__ while int is the layout base) would otherwise
	// never pick the slot up, because inheritProtocolPointers only copies
	// the bundle from the single primary base.
	if base.Async != nil {
		if t.Async == nil {
			t.Async = &AsyncMethods{}
		}
		copyAsyncSlots(t.Async, base.Async)
	}
	if base.Sequence != nil {
		if t.Sequence == nil {
			t.Sequence = &SequenceMethods{}
		}
		copySequenceSlots(t.Sequence, base.Sequence)
	}
	if base.Mapping != nil {
		if t.Mapping == nil {
			t.Mapping = &MappingMethods{}
		}
		copyMappingSlots(t.Mapping, base.Mapping)
	}
}

// inheritDirectBaseScalars copies scalar (non-bundle) slots from the
// primary base. CPython runs the same COPYSLOT block across every MRO
// ancestor gated by SLOTDEFINED; gopy stops at the direct base
// because we cannot compare function values to determine ownership
// and because several built-in types (typeType, enumerateType,
// ReversedType, ...) deliberately leave TpNew / Call nil and rely on
// typeCall's fallback to route construction through their builtin
// entry points. A full MRO walk for TpNew would pull objectNew up
// through typeType into every metaclass and turn `class Meta(type)`
// into a non-functional class.
//
// CPython: Objects/typeobject.c:8227 inherit_slots (per-slot COPYSLOT
// macros, gated by SLOTDEFINED)
func inheritDirectBaseScalars(t, base *Type) {
	if t.Dealloc == nil {
		t.Dealloc = base.Dealloc
	}
	if t.Getattro == nil {
		t.Getattro = base.Getattro
	}
	if t.Setattro == nil {
		t.Setattro = base.Setattro
	}
	if t.Repr == nil {
		t.Repr = base.Repr
	}
	if t.Call == nil {
		t.Call = base.Call
	}
	if t.Str == nil {
		t.Str = base.Str
	}
	if t.Format == nil {
		t.Format = base.Format
	}
	// tp_richcompare and tp_hash come together: only inherit when t
	// has neither and the namespace did not override __hash__.
	// Otherwise we would either lose the user's compare semantics or
	// pair them with a hash that disagrees.
	//
	// CPython: Objects/typeobject.c:8366 (richcompare/hash dance)
	if t.RichCmp == nil && t.Hash == nil && !typeOverridesHash(t) {
		t.RichCmp = base.RichCmp
		t.Hash = base.Hash
	}
	if t.Iter == nil {
		t.Iter = base.Iter
	}
	if t.IterNext == nil {
		t.IterNext = base.IterNext
	}
	if t.DescrGet == nil {
		t.DescrGet = base.DescrGet
	}
	if t.DescrSet == nil {
		t.DescrSet = base.DescrSet
	}
	if t.Finalize == nil {
		t.Finalize = base.Finalize
	}
	if t.TpTraverse == nil {
		t.TpTraverse = base.TpTraverse
	}
	if t.TpNew == nil {
		t.TpNew = base.TpNew
	}
	if t.Vectorcall == nil {
		t.Vectorcall = base.Vectorcall
	}
}

// inheritProtocolPointers ports type_ready_inherit_as_structs. When
// t did not allocate a bundle of a given kind, give it a copy of
// base's bundle. CPython shares the pointer (tp_as_number = base->
// tp_as_number) because its slot_tp_* dispatchers look up the
// dunder dynamically through the MRO and never write per-type into
// the bundle. gopy's fixup pass (slotNbBool, slotMpLength, etc.)
// writes per-type slot pointers into the bundle, so sharing would
// alias base's table and mutate it from a subclass's __bool__ or
// __len__ install. Copying keeps every type's bundle independent.
//
// The slot pointers that get copied here have already been filled
// in by inheritSlotsFromAncestor's per-slot walk on the bundles
// that were non-nil during that walk; the copy here is only for
// types whose bundle stayed nil. The copy depth is one struct (no
// nested heap state) so the cost is fixed.
//
// CPython: Objects/typeobject.c:8685 type_ready_inherit_as_structs
func inheritProtocolPointers(t, base *Type) {
	if t.Number == nil && base.Number != nil {
		cp := *base.Number
		t.Number = &cp
	}
	if t.Sequence == nil && base.Sequence != nil {
		cp := *base.Sequence
		t.Sequence = &cp
	}
	if t.Mapping == nil && base.Mapping != nil {
		cp := *base.Mapping
		t.Mapping = &cp
	}
	if t.Async == nil && base.Async != nil {
		cp := *base.Async
		t.Async = &cp
	}
}

// copyNumberSlots fills nil slots on dst from src.
//
// CPython: Objects/typeobject.c:8258 COPYNUM block
func copyNumberSlots(dst, src *NumberMethods) {
	copyNumberBinarySlots(dst, src)
	copyNumberInPlaceSlots(dst, src)
	copyNumberUnarySlots(dst, src)
}

// copyNumberBinarySlots fills nil binary-op slots on dst from src.
// Split out from copyNumberSlots so gocognit stays under 30.
func copyNumberBinarySlots(dst, src *NumberMethods) {
	if dst.Add == nil {
		dst.Add = src.Add
	}
	if dst.Subtract == nil {
		dst.Subtract = src.Subtract
	}
	if dst.Multiply == nil {
		dst.Multiply = src.Multiply
	}
	if dst.MatrixMultiply == nil {
		dst.MatrixMultiply = src.MatrixMultiply
	}
	if dst.TrueDivide == nil {
		dst.TrueDivide = src.TrueDivide
	}
	if dst.FloorDivide == nil {
		dst.FloorDivide = src.FloorDivide
	}
	if dst.Remainder == nil {
		dst.Remainder = src.Remainder
	}
	if dst.And == nil {
		dst.And = src.And
	}
	if dst.Or == nil {
		dst.Or = src.Or
	}
	if dst.Xor == nil {
		dst.Xor = src.Xor
	}
	if dst.Lshift == nil {
		dst.Lshift = src.Lshift
	}
	if dst.Rshift == nil {
		dst.Rshift = src.Rshift
	}
	if dst.Power == nil {
		dst.Power = src.Power
	}
	if dst.Divmod == nil {
		dst.Divmod = src.Divmod
	}
}

// copyNumberInPlaceSlots fills nil in-place op slots on dst from src.
func copyNumberInPlaceSlots(dst, src *NumberMethods) {
	if dst.InPlaceAdd == nil {
		dst.InPlaceAdd = src.InPlaceAdd
	}
	if dst.InPlaceSubtract == nil {
		dst.InPlaceSubtract = src.InPlaceSubtract
	}
	if dst.InPlaceMultiply == nil {
		dst.InPlaceMultiply = src.InPlaceMultiply
	}
	if dst.InPlaceMatrixMultiply == nil {
		dst.InPlaceMatrixMultiply = src.InPlaceMatrixMultiply
	}
	if dst.InPlaceTrueDivide == nil {
		dst.InPlaceTrueDivide = src.InPlaceTrueDivide
	}
	if dst.InPlaceFloorDivide == nil {
		dst.InPlaceFloorDivide = src.InPlaceFloorDivide
	}
	if dst.InPlaceRemainder == nil {
		dst.InPlaceRemainder = src.InPlaceRemainder
	}
	if dst.InPlaceAnd == nil {
		dst.InPlaceAnd = src.InPlaceAnd
	}
	if dst.InPlaceOr == nil {
		dst.InPlaceOr = src.InPlaceOr
	}
	if dst.InPlaceXor == nil {
		dst.InPlaceXor = src.InPlaceXor
	}
	if dst.InPlaceLshift == nil {
		dst.InPlaceLshift = src.InPlaceLshift
	}
	if dst.InPlaceRshift == nil {
		dst.InPlaceRshift = src.InPlaceRshift
	}
	if dst.InPlacePower == nil {
		dst.InPlacePower = src.InPlacePower
	}
}

// copyNumberUnarySlots fills nil unary / coercion slots on dst from src.
func copyNumberUnarySlots(dst, src *NumberMethods) {
	if dst.Negative == nil {
		dst.Negative = src.Negative
	}
	if dst.Positive == nil {
		dst.Positive = src.Positive
	}
	if dst.Absolute == nil {
		dst.Absolute = src.Absolute
	}
	if dst.Invert == nil {
		dst.Invert = src.Invert
	}
	if dst.Bool == nil {
		dst.Bool = src.Bool
	}
	if dst.Int == nil {
		dst.Int = src.Int
	}
	if dst.Float == nil {
		dst.Float = src.Float
	}
	if dst.Index == nil {
		dst.Index = src.Index
	}
}

// copySequenceSlots fills nil slots on dst from src.
//
// CPython: Objects/typeobject.c:8308 COPYSEQ block
func copySequenceSlots(dst, src *SequenceMethods) {
	if dst.Length == nil {
		dst.Length = src.Length
	}
	if dst.Concat == nil {
		dst.Concat = src.Concat
	}
	if dst.Repeat == nil {
		dst.Repeat = src.Repeat
	}
	if dst.GetItem == nil {
		dst.GetItem = src.GetItem
	}
	if dst.SetItem == nil {
		dst.SetItem = src.SetItem
	}
	if dst.Contains == nil {
		dst.Contains = src.Contains
	}
	if dst.InPlaceConcat == nil {
		dst.InPlaceConcat = src.InPlaceConcat
	}
	if dst.InPlaceRepeat == nil {
		dst.InPlaceRepeat = src.InPlaceRepeat
	}
}

// copyMappingSlots fills nil slots on dst from src.
//
// CPython: Objects/typeobject.c:8322 COPYMAP block
func copyMappingSlots(dst, src *MappingMethods) {
	if dst.Length == nil {
		dst.Length = src.Length
	}
	if dst.GetItem == nil {
		dst.GetItem = src.GetItem
	}
	if dst.SetItem == nil {
		dst.SetItem = src.SetItem
	}
	if dst.DelItem == nil {
		dst.DelItem = src.DelItem
	}
}

// copyAsyncSlots fills nil slots on dst from src.
//
// CPython: Objects/typeobject.c:8299 COPYASYNC block
func copyAsyncSlots(dst, src *AsyncMethods) {
	if dst.Aiter == nil {
		dst.Aiter = src.Aiter
	}
	if dst.Anext == nil {
		dst.Anext = src.Anext
	}
	if dst.Await == nil {
		dst.Await = src.Await
	}
}

// typeOverridesHash reports whether t's namespace declared __hash__.
// CPython's overrides_hash walks tp_dict; gopy's namespace lands in
// typeDescrTable, so check there directly. A None entry counts as an
// override too: setting `__hash__ = None` makes the type unhashable
// and the hash/richcompare pair must not be inherited from base.
//
// CPython: Objects/typeobject.c:8205 overrides_hash
func typeOverridesHash(t *Type) bool {
	if descrs, ok := typeDescrTable[t]; ok {
		if _, present := descrs["__hash__"]; present {
			return true
		}
	}
	return false
}
