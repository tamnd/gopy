// Symbolic-state lattice for the abstract interpreter. Every value
// the analysis pass tracks is one of nine tags: Unknown, NonNull,
// Null, Bottom, TypeVersion, KnownClass, KnownValue, Tuple,
// Truthiness. Lattice operations narrow a symbol toward Bottom on
// contradictions and toward concrete values on confirmations. The
// arena lives on JitOptContext.TArena and is reset at the start of
// every analysis pass.
//
// CPython: Python/optimizer_symbols.c:54-732

package optimizer

import (
	"unsafe"

	"github.com/tamnd/gopy/objects"
)

// arenaBase returns the symbol arena slice. CPython works in
// pointer arithmetic against the arena base; gopy keeps the indices
// directly and converts on read with this helper.
//
// CPython: Python/optimizer_symbols.c:53-57 allocation_base
func arenaBase(ctx *JitOptContext) []JitOptSymbol {
	return ctx.TArena.Arena[:]
}

// noSpaceSymbol is the sentinel returned by outOfSpace. Mutating it
// is harmless because the surrounding context is already poisoned.
//
// CPython: Python/optimizer_symbols.c:48-51 NO_SPACE_SYMBOL
var noSpaceSymbol = JitOptSymbol{Tag: uint8(JitSymBottom)}

// outOfSpace marks the context as done and returns the no-space
// sentinel. Callers propagate the sentinel up so the analysis pass
// can short-circuit cleanly.
//
// CPython: Python/optimizer_symbols.c:59-65 out_of_space
func outOfSpace(ctx *JitOptContext) *JitOptSymbol {
	ctx.Done = true
	ctx.OutOfSpace = true
	return &noSpaceSymbol
}

// symNew allocates a fresh Unknown symbol from the arena. Returns
// nil when the arena is exhausted; callers must wrap with
// outOfSpace.
//
// CPython: Python/optimizer_symbols.c:67-79 sym_new
func symNew(ctx *JitOptContext) *JitOptSymbol {
	if ctx.TArena.CurrNumber >= ctx.TArena.MaxNumber {
		return nil
	}
	self := &ctx.TArena.Arena[ctx.TArena.CurrNumber]
	ctx.TArena.CurrNumber++
	*self = JitOptSymbol{Tag: uint8(JitSymUnknown)}
	return self
}

// makeConst stamps sym to the KnownValue tag pointing at val. CPython
// adds a fresh strong reference; Go's GC keeps val alive as long as
// it stays in the union.
//
// CPython: Python/optimizer_symbols.c:81-85 make_const
func makeConst(sym *JitOptSymbol, val objects.Object) {
	sym.Tag = uint8(JitSymKnownValue)
	sym.Value = JitOptKnownValue{Value: val}
}

// symSetBottom forces sym to Bottom and trips the contradiction flag
// so the analysis pass can abandon the trace.
//
// CPython: Python/optimizer_symbols.c:87-93 sym_set_bottom
func symSetBottom(ctx *JitOptContext, sym *JitOptSymbol) {
	sym.Tag = uint8(JitSymBottom)
	ctx.Done = true
	ctx.Contradiction = true
}

// SymIsBottom reports whether sym is the bottom of the lattice.
//
// CPython: Python/optimizer_symbols.c:95-99 _Py_uop_sym_is_bottom
func SymIsBottom(sym *JitOptSymbol) bool {
	return sym.Tag == uint8(JitSymBottom)
}

// SymIsNotNull reports whether sym is known to not be NULL.
//
// CPython: Python/optimizer_symbols.c:101-104 _Py_uop_sym_is_not_null
func SymIsNotNull(sym *JitOptSymbol) bool {
	return sym.Tag == uint8(JitSymNonNull) || sym.Tag > uint8(JitSymBottom)
}

// SymIsConst reports whether sym is a known concrete value. A
// Truthiness symbol upgrades to a known bool when its underlying
// value's truthiness is known.
//
// CPython: Python/optimizer_symbols.c:106-122 _Py_uop_sym_is_const
func SymIsConst(ctx *JitOptContext, sym *JitOptSymbol) bool {
	if sym.Tag == uint8(JitSymKnownValue) {
		return true
	}
	if sym.Tag == uint8(JitSymTruthiness) {
		value := &arenaBase(ctx)[sym.Truthiness.Value]
		truthiness := SymTruthiness(ctx, value)
		if truthiness < 0 {
			return false
		}
		invert := boolToInt(sym.Truthiness.Invert)
		if (truthiness ^ invert) != 0 {
			makeConst(sym, objects.True())
		} else {
			makeConst(sym, objects.False())
		}
		return true
	}
	return false
}

// SymIsNull reports whether sym is known to be NULL.
//
// CPython: Python/optimizer_symbols.c:124-128 _Py_uop_sym_is_null
func SymIsNull(sym *JitOptSymbol) bool {
	return sym.Tag == uint8(JitSymNull)
}

// SymGetConst returns the constant value sym proves, or nil. Resolves
// Truthiness symbols to True/False on the fly when their underlying
// value's truthiness is known.
//
// CPython: Python/optimizer_symbols.c:131-148 _Py_uop_sym_get_const
func SymGetConst(ctx *JitOptContext, sym *JitOptSymbol) objects.Object {
	if sym.Tag == uint8(JitSymKnownValue) {
		return sym.Value.Value
	}
	if sym.Tag == uint8(JitSymTruthiness) {
		value := &arenaBase(ctx)[sym.Truthiness.Value]
		truthiness := SymTruthiness(ctx, value)
		if truthiness < 0 {
			return nil
		}
		invert := boolToInt(sym.Truthiness.Invert)
		var res objects.Object
		if (truthiness ^ invert) != 0 {
			res = objects.True()
		} else {
			res = objects.False()
		}
		makeConst(sym, res)
		return res
	}
	return nil
}

// SymSetType narrows sym to instances of typ. Contradictions on the
// existing tag drop sym to Bottom.
//
// CPython: Python/optimizer_symbols.c:150-198 _Py_uop_sym_set_type
func SymSetType(ctx *JitOptContext, sym *JitOptSymbol, typ *objects.Type) {
	switch JitSymType(sym.Tag) {
	case JitSymNull:
		symSetBottom(ctx, sym)
	case JitSymKnownClass:
		if sym.Class.Type != typ {
			symSetBottom(ctx, sym)
		}
	case JitSymTypeVersion:
		if sym.Version.Version == typ.VersionTag() {
			sym.Tag = uint8(JitSymKnownClass)
			sym.Class.Type = typ
			sym.Class.Version = typ.VersionTag()
		} else {
			symSetBottom(ctx, sym)
		}
	case JitSymKnownValue:
		if objects.ExactType(sym.Value.Value) != typ {
			sym.Value.Value = nil
			symSetBottom(ctx, sym)
		}
	case JitSymTuple:
		if typ != objects.TupleType {
			symSetBottom(ctx, sym)
		}
	case JitSymBottom:
		// Already bottom.
	case JitSymNonNull, JitSymUnknown:
		sym.Tag = uint8(JitSymKnownClass)
		sym.Class.Version = 0
		sym.Class.Type = typ
	case JitSymTruthiness:
		if typ != objects.BoolType {
			symSetBottom(ctx, sym)
		}
	}
}

// SymSetTypeVersion narrows sym to a specific tp_version_tag. Returns
// false on contradiction.
//
// CPython: Python/optimizer_symbols.c:200-245 _Py_uop_sym_set_type_version
func SymSetTypeVersion(ctx *JitOptContext, sym *JitOptSymbol, version uint32) bool {
	switch JitSymType(sym.Tag) {
	case JitSymNull:
		symSetBottom(ctx, sym)
		return false
	case JitSymKnownClass:
		if sym.Class.Type.VersionTag() != version {
			symSetBottom(ctx, sym)
			return false
		}
		sym.Class.Version = version
		return true
	case JitSymKnownValue:
		sym.Value.Value = nil
		symSetBottom(ctx, sym)
		return false
	case JitSymTuple:
		symSetBottom(ctx, sym)
		return false
	case JitSymTypeVersion:
		if sym.Version.Version == version {
			return true
		}
		symSetBottom(ctx, sym)
		return false
	case JitSymBottom:
		return false
	case JitSymNonNull, JitSymUnknown:
		sym.Tag = uint8(JitSymTypeVersion)
		sym.Version.Version = version
		return true
	case JitSymTruthiness:
		if version != objects.BoolType.VersionTag() {
			symSetBottom(ctx, sym)
			return false
		}
		return true
	}
	return false
}

// SymSetConst narrows sym to a single concrete value. Contradictions
// on the type or existing const drop sym to Bottom.
//
// CPython: Python/optimizer_symbols.c:247-314 _Py_uop_sym_set_const
func SymSetConst(ctx *JitOptContext, sym *JitOptSymbol, constVal objects.Object) {
	switch JitSymType(sym.Tag) {
	case JitSymNull:
		symSetBottom(ctx, sym)
	case JitSymKnownClass:
		if sym.Class.Type != objects.ExactType(constVal) {
			symSetBottom(ctx, sym)
			return
		}
		makeConst(sym, constVal)
	case JitSymKnownValue:
		if sym.Value.Value != constVal {
			sym.Value.Value = nil
			symSetBottom(ctx, sym)
		}
	case JitSymTuple:
		symSetBottom(ctx, sym)
	case JitSymTypeVersion:
		if sym.Version.Version != objects.ExactType(constVal).VersionTag() {
			symSetBottom(ctx, sym)
			return
		}
		makeConst(sym, constVal)
	case JitSymBottom:
		// Already bottom.
	case JitSymNonNull, JitSymUnknown:
		makeConst(sym, constVal)
	case JitSymTruthiness:
		_, isBool := constVal.(*objects.Bool)
		alreadyConst := SymIsConst(ctx, sym)
		if !isBool || (alreadyConst && SymGetConst(ctx, sym) != constVal) {
			symSetBottom(ctx, sym)
			return
		}
		value := &arenaBase(ctx)[sym.Truthiness.Value]
		typ := SymGetType(value)
		var truthMatch objects.Object
		if sym.Truthiness.Invert {
			truthMatch = objects.False()
		} else {
			truthMatch = objects.True()
		}
		if constVal == truthMatch {
			if typ == objects.BoolType {
				SymSetConst(ctx, value, objects.True())
			}
		} else if typ == objects.BoolType {
			SymSetConst(ctx, value, objects.False())
		} else if typ == objects.IntType {
			SymSetConst(ctx, value, objects.NewInt(0))
		} else if typ == objects.StrType() {
			SymSetConst(ctx, value, objects.NewStr(""))
		}
		makeConst(sym, constVal)
	}
}

// SymSetNull narrows sym to NULL. Promotes Unknown directly; any
// non-NULL state drops to Bottom.
//
// CPython: Python/optimizer_symbols.c:316-325 _Py_uop_sym_set_null
func SymSetNull(ctx *JitOptContext, sym *JitOptSymbol) {
	if sym.Tag == uint8(JitSymUnknown) {
		sym.Tag = uint8(JitSymNull)
	} else if sym.Tag > uint8(JitSymNull) {
		symSetBottom(ctx, sym)
	}
}

// SymSetNonNull narrows sym to non-NULL. Unknown promotes; Null drops
// to Bottom.
//
// CPython: Python/optimizer_symbols.c:327-336 _Py_uop_sym_set_non_null
func SymSetNonNull(ctx *JitOptContext, sym *JitOptSymbol) {
	if sym.Tag == uint8(JitSymUnknown) {
		sym.Tag = uint8(JitSymNonNull)
	} else if sym.Tag == uint8(JitSymNull) {
		symSetBottom(ctx, sym)
	}
}

// SymNewUnknown allocates a fresh Unknown symbol.
//
// CPython: Python/optimizer_symbols.c:339-347 _Py_uop_sym_new_unknown
func SymNewUnknown(ctx *JitOptContext) *JitOptSymbol {
	res := symNew(ctx)
	if res == nil {
		return outOfSpace(ctx)
	}
	return res
}

// SymNewNotNull allocates a fresh NonNull symbol.
//
// CPython: Python/optimizer_symbols.c:349-358 _Py_uop_sym_new_not_null
func SymNewNotNull(ctx *JitOptContext) *JitOptSymbol {
	res := symNew(ctx)
	if res == nil {
		return outOfSpace(ctx)
	}
	res.Tag = uint8(JitSymNonNull)
	return res
}

// SymNewType allocates a fresh symbol narrowed to instances of typ.
//
// CPython: Python/optimizer_symbols.c:360-369 _Py_uop_sym_new_type
func SymNewType(ctx *JitOptContext, typ *objects.Type) *JitOptSymbol {
	res := symNew(ctx)
	if res == nil {
		return outOfSpace(ctx)
	}
	SymSetType(ctx, res, typ)
	return res
}

// SymNewConst allocates a fresh symbol pinned to constVal.
//
// CPython: Python/optimizer_symbols.c:372-382 _Py_uop_sym_new_const
func SymNewConst(ctx *JitOptContext, constVal objects.Object) *JitOptSymbol {
	res := symNew(ctx)
	if res == nil {
		return outOfSpace(ctx)
	}
	SymSetConst(ctx, res, constVal)
	return res
}

// SymNewNull allocates a fresh NULL-state symbol.
//
// CPython: Python/optimizer_symbols.c:384-393 _Py_uop_sym_new_null
func SymNewNull(ctx *JitOptContext) *JitOptSymbol {
	res := symNew(ctx)
	if res == nil {
		return outOfSpace(ctx)
	}
	SymSetNull(ctx, res)
	return res
}

// SymGetType returns the type sym is known to be, or nil if the
// type is unconstrained.
//
// CPython: Python/optimizer_symbols.c:395-416 _Py_uop_sym_get_type
func SymGetType(sym *JitOptSymbol) *objects.Type {
	switch JitSymType(sym.Tag) {
	case JitSymNull, JitSymTypeVersion, JitSymBottom, JitSymNonNull, JitSymUnknown:
		return nil
	case JitSymKnownClass:
		return sym.Class.Type
	case JitSymKnownValue:
		return objects.ExactType(sym.Value.Value)
	case JitSymTuple:
		return objects.TupleType
	case JitSymTruthiness:
		return objects.BoolType
	}
	return nil
}

// SymGetTypeVersion returns the tp_version_tag sym proves, or 0 when
// unconstrained.
//
// CPython: Python/optimizer_symbols.c:418-440 _Py_uop_sym_get_type_version
func SymGetTypeVersion(sym *JitOptSymbol) uint32 {
	switch JitSymType(sym.Tag) {
	case JitSymNull, JitSymBottom, JitSymNonNull, JitSymUnknown:
		return 0
	case JitSymTypeVersion:
		return sym.Version.Version
	case JitSymKnownClass:
		return sym.Class.Version
	case JitSymKnownValue:
		return objects.ExactType(sym.Value.Value).VersionTag()
	case JitSymTuple:
		return objects.TupleType.VersionTag()
	case JitSymTruthiness:
		return objects.BoolType.VersionTag()
	}
	return 0
}

// SymHasType reports whether sym carries a definite type.
//
// CPython: Python/optimizer_symbols.c:442-460 _Py_uop_sym_has_type
func SymHasType(sym *JitOptSymbol) bool {
	switch JitSymType(sym.Tag) {
	case JitSymKnownClass, JitSymKnownValue, JitSymTuple, JitSymTruthiness:
		return true
	}
	return false
}

// SymMatchesType reports whether sym's known type equals typ.
//
// CPython: Python/optimizer_symbols.c:462-467 _Py_uop_sym_matches_type
func SymMatchesType(sym *JitOptSymbol, typ *objects.Type) bool {
	return SymGetType(sym) == typ
}

// SymMatchesTypeVersion reports whether sym's known type version
// equals version.
//
// CPython: Python/optimizer_symbols.c:469-473 _Py_uop_sym_matches_type_version
func SymMatchesTypeVersion(sym *JitOptSymbol, version uint32) bool {
	return SymGetTypeVersion(sym) == version
}

// SymTruthiness returns 1 for truthy, 0 for falsey, -1 for unknown.
// Resolves Truthiness symbols recursively and folds to KnownValue
// when the underlying truth is known.
//
// CPython: Python/optimizer_symbols.c:475-521 _Py_uop_sym_truthiness
func SymTruthiness(ctx *JitOptContext, sym *JitOptSymbol) int {
	switch JitSymType(sym.Tag) {
	case JitSymNull, JitSymTypeVersion, JitSymBottom, JitSymNonNull, JitSymUnknown:
		return -1
	case JitSymKnownClass:
		return -1
	case JitSymKnownValue:
		// Fall through to const-handling below.
	case JitSymTuple:
		if sym.Tuple.Length != 0 {
			return 1
		}
		return 0
	case JitSymTruthiness:
		value := &arenaBase(ctx)[sym.Truthiness.Value]
		truthiness := SymTruthiness(ctx, value)
		if truthiness < 0 {
			return truthiness
		}
		truthiness ^= boolToInt(sym.Truthiness.Invert)
		if truthiness != 0 {
			makeConst(sym, objects.True())
		} else {
			makeConst(sym, objects.False())
		}
		return truthiness
	default:
		return -1
	}
	value := sym.Value.Value
	if objects.IsNone(value) {
		return 0
	}
	tp := objects.ExactType(value)
	switch tp {
	case objects.IntType:
		i := value.(*objects.Int)
		if v, ok := i.Int64(); ok {
			if v == 0 {
				return 0
			}
			return 1
		}
		if i.BigInt().Sign() == 0 {
			return 0
		}
		return 1
	case objects.StrType():
		s := value.(*objects.Unicode)
		if s.Value() == "" {
			return 0
		}
		return 1
	case objects.BoolType:
		if value == objects.True() {
			return 1
		}
		return 0
	}
	return -1
}

// SymNewTuple allocates a Tuple symbol with element-wise tracking.
// Tuples longer than MaxSymbolicTupleSize degrade to KnownClass.
//
// CPython: Python/optimizer_symbols.c:523-542 _Py_uop_sym_new_tuple
func SymNewTuple(ctx *JitOptContext, size int, args []*JitOptSymbol) *JitOptSymbol {
	res := symNew(ctx)
	if res == nil {
		return outOfSpace(ctx)
	}
	if size > MaxSymbolicTupleSize {
		res.Tag = uint8(JitSymKnownClass)
		res.Class.Type = objects.TupleType
		return res
	}
	res.Tag = uint8(JitSymTuple)
	res.Tuple.Length = uint8(size)
	base := &ctx.TArena.Arena[0]
	for i := 0; i < size; i++ {
		res.Tuple.Items[i] = uint16(symIndex(base, args[i]))
	}
	return res
}

// SymTupleGetitem returns the symbol at index item of a Tuple-tagged
// sym, or a fresh Unknown otherwise.
//
// CPython: Python/optimizer_symbols.c:544-558 _Py_uop_sym_tuple_getitem
func SymTupleGetitem(ctx *JitOptContext, sym *JitOptSymbol, item int) *JitOptSymbol {
	if sym.Tag == uint8(JitSymKnownValue) {
		if t, ok := sym.Value.Value.(*objects.Tuple); ok {
			if item < t.Len() {
				return SymNewConst(ctx, t.Item(item))
			}
		}
	} else if sym.Tag == uint8(JitSymTuple) && item < int(sym.Tuple.Length) {
		return &arenaBase(ctx)[sym.Tuple.Items[item]]
	}
	return SymNewUnknown(ctx)
}

// SymTupleLength returns the proven length of sym, or -1 if unknown.
//
// CPython: Python/optimizer_symbols.c:560-573 _Py_uop_sym_tuple_length
func SymTupleLength(sym *JitOptSymbol) int {
	if sym.Tag == uint8(JitSymKnownValue) {
		if t, ok := sym.Value.Value.(*objects.Tuple); ok {
			return t.Len()
		}
	} else if sym.Tag == uint8(JitSymTuple) {
		return int(sym.Tuple.Length)
	}
	return -1
}

// SymIsImmortal reports whether sym is known to point at an immortal
// object. CPython mints int(-5..256), the empty string, True/False,
// and None as immortal. gopy treats bool / None / interned singletons
// the same way.
//
// CPython: Python/optimizer_symbols.c:576-589 _Py_uop_sym_is_immortal
func SymIsImmortal(sym *JitOptSymbol) bool {
	if sym.Tag == uint8(JitSymKnownValue) {
		v := sym.Value.Value
		if objects.IsNone(v) {
			return true
		}
		if v == objects.True() || v == objects.False() {
			return true
		}
		return false
	}
	if sym.Tag == uint8(JitSymKnownClass) {
		return sym.Class.Type == objects.BoolType
	}
	if sym.Tag == uint8(JitSymTruthiness) {
		return true
	}
	return false
}

// SymNewTruthiness allocates a Truthiness-tagged symbol that mirrors
// the truthiness of value. truthy=true makes "is truthy", false
// inverts.
//
// CPython: Python/optimizer_symbols.c:591-613 _Py_uop_sym_new_truthiness
func SymNewTruthiness(ctx *JitOptContext, value *JitOptSymbol, truthy bool) *JitOptSymbol {
	invert := !truthy
	if value.Tag == uint8(JitSymTruthiness) && value.Truthiness.Invert == invert {
		return value
	}
	res := symNew(ctx)
	if res == nil {
		return outOfSpace(ctx)
	}
	truthiness := SymTruthiness(ctx, value)
	if truthiness < 0 {
		res.Tag = uint8(JitSymTruthiness)
		res.Truthiness.Invert = invert
		base := &ctx.TArena.Arena[0]
		res.Truthiness.Value = uint16(symIndex(base, value))
	} else {
		var v objects.Object
		if (truthiness ^ boolToInt(invert)) != 0 {
			v = objects.True()
		} else {
			v = objects.False()
		}
		makeConst(res, v)
	}
	return res
}

// AbstractContextInit zeroes ctx and seeds the arena and locals
// pool. Must run before any frame is pushed.
//
// CPython: Python/optimizer_symbols.c:677-701 _Py_uop_abstractcontext_init
func AbstractContextInit(ctx *JitOptContext) {
	ctx.Limit = MaxAbstractInterpSize
	ctx.NConsumed = 0
	ctx.TArena.CurrNumber = 0
	ctx.TArena.MaxNumber = TyArenaSize
	ctx.CurrFrameDepth = 0
	ctx.Done = false
	ctx.OutOfSpace = false
	ctx.Contradiction = false
}

// AbstractContextFini releases the per-trace arena. Const-tagged
// symbols hold object references; clearing them lets the GC reclaim
// values that were live only inside the analyzer.
//
// CPython: Python/optimizer_symbols.c:660-674 _Py_uop_abstractcontext_fini
func AbstractContextFini(ctx *JitOptContext) {
	if ctx == nil {
		return
	}
	ctx.CurrFrameDepth = 0
	tys := ctx.TArena.CurrNumber
	for i := 0; i < tys; i++ {
		sym := &ctx.TArena.Arena[i]
		if sym.Tag == uint8(JitSymKnownValue) {
			sym.Value.Value = nil
		}
	}
}

// FrameNew pushes a new abstract frame onto the context. arg-len
// symbols seed the locals; the rest are Unknown. Returns nil and
// sets OutOfSpace when the locals+stack pool is exhausted.
//
// CPython: Python/optimizer_symbols.c:617-658 _Py_uop_frame_new
func FrameNew(ctx *JitOptContext, co *objects.Code, currStackEntries int, args []*JitOptSymbol, argLen int) *AbstractFrame {
	frame := &ctx.Frames[ctx.CurrFrameDepth]
	nlocalsplus := nlocalsplusFor(co)
	frame.StackLen = co.Stacksize
	frame.LocalsLen = nlocalsplus
	frame.Locals = ctx.NConsumed
	frame.Stack = frame.Locals + nlocalsplus
	frame.StackPointer = frame.Stack + currStackEntries
	ctx.NConsumed += nlocalsplus + co.Stacksize
	if ctx.NConsumed >= ctx.Limit {
		ctx.Done = true
		ctx.OutOfSpace = true
		return nil
	}
	for i := 0; i < argLen; i++ {
		ctx.LocalsAndStack[frame.Locals+i] = args[i]
	}
	for i := argLen; i < nlocalsplus; i++ {
		ctx.LocalsAndStack[frame.Locals+i] = SymNewUnknown(ctx)
	}
	for i := 0; i < currStackEntries; i++ {
		ctx.LocalsAndStack[frame.Stack+i] = SymNewUnknown(ctx)
	}
	return frame
}

// FramePop drops the current abstract frame. The locals/stack window
// is recycled for the parent.
//
// CPython: Python/optimizer_symbols.c:703-713 _Py_uop_frame_pop
func FramePop(ctx *JitOptContext) {
	frame := ctx.Frame
	ctx.NConsumed = frame.Locals
	ctx.CurrFrameDepth--
	ctx.Frame = &ctx.Frames[ctx.CurrFrameDepth-1]
}

// nlocalsplusFor mirrors co_nlocalsplus. CPython packs locals,
// cellvars, and freevars into one count; gopy stores them in
// separate slices, so the sum reproduces the C field.
//
// CPython: Include/internal/pycore_code.h:163 co_nlocalsplus
func nlocalsplusFor(co *objects.Code) int {
	return len(co.Varnames) + len(co.Cellvars) + len(co.Freevars)
}

// symIndex returns the arena index of sym. Used when packing tuple
// element references and truthiness back-references into uint16.
//
// CPython: optimizer_symbols.c:538 (uint16_t)(args[i] - allocation_base(ctx))
func symIndex(base, sym *JitOptSymbol) int {
	delta := uintptr(unsafe.Pointer(sym)) - uintptr(unsafe.Pointer(base))
	return int(delta / unsafe.Sizeof(JitOptSymbol{}))
}

// boolToInt collapses bool -> 0/1. Used for the XOR-with-invert
// pattern the truthiness lattice runs through.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
