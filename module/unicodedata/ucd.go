// The UCD type and its ucd_3_2_0 singleton. CPython exposes UCD as a
// non-instantiable type whose instances carry a frozen Unicode
// snapshot: a getrecord pointer that returns a change_record overlay
// for each codepoint, plus a normalization pointer that rolls back
// the five 3.2-era NFD edits. The module-level functions and the UCD
// methods share the same C bodies, dispatching through UCD_Check to
// decide whether to apply the overlay.
//
// CPython: Modules/unicodedata.c:45 change_record
// CPython: Modules/unicodedata.c:73 PreviousDBVersion
// CPython: Modules/unicodedata.c:97 new_previous_version
// CPython: Modules/unicodedata_db.h:8336 get_change_3_2_0
// CPython: Modules/unicodedata_db.h:8347 normalization_3_2_0

package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// changeRecord mirrors CPython's change_record struct. Each field is
// the per-codepoint override (or sentinel 0xFF == 255 for "no
// change"; 0 in category_changed means "unassigned in this old
// version").
//
// CPython: Modules/unicodedata.c:45 change_record
type changeRecord struct {
	BidirChanged          uint8
	CategoryChanged       uint8
	DecimalChanged        uint8
	MirroredChanged       uint8
	EastAsianWidthChanged uint8
	NumericChanged        float64
}

// UCD is the gopy port of CPython's PreviousDBVersion. Each instance
// carries the name string exposed via the unidata_version member plus
// the two function pointers (getrecord, normalization) that pick out
// the snapshot-specific overlays.
//
// CPython: Modules/unicodedata.c:73 PreviousDBVersion
type UCD struct {
	objects.Header
	name          string
	getRecord     func(rune) changeRecord
	normalization func(rune) rune
}

// UCDType is the type singleton for unicodedata.UCD. CPython marks it
// with Py_TPFLAGS_DISALLOW_INSTANTIATION; gopy leaves TpNew unset, so
// `UCD()` from Python raises TypeError the same way.
//
// CPython: Modules/unicodedata.c:1667 ucd_type_spec
var UCDType = objects.NewType("unicodedata.UCD", []*objects.Type{objects.ObjectType()})

func init() {
	objects.SetTypeDescr(UCDType, "unidata_version", objects.NewGetSetDescr("unidata_version",
		func(o objects.Object) (objects.Object, error) {
			u, ok := o.(*UCD)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor 'unidata_version' for 'unicodedata.UCD' objects doesn't apply to a '%s' object", o.Type().Name)
			}
			return objects.NewStr(u.name), nil
		},
		nil,
	))

	objects.SetTypeDescr(UCDType, "decimal", objects.NewMethodDescr(UCDType, "decimal", ucdDecimal))
	objects.SetTypeDescr(UCDType, "digit", objects.NewMethodDescr(UCDType, "digit", ucdDigit))
	objects.SetTypeDescr(UCDType, "numeric", objects.NewMethodDescr(UCDType, "numeric", ucdNumeric))
	objects.SetTypeDescr(UCDType, "category", objects.NewMethodDescr(UCDType, "category", ucdCategory))
	objects.SetTypeDescr(UCDType, "bidirectional", objects.NewMethodDescr(UCDType, "bidirectional", ucdBidirectional))
	objects.SetTypeDescr(UCDType, "combining", objects.NewMethodDescr(UCDType, "combining", ucdCombining))
	objects.SetTypeDescr(UCDType, "mirrored", objects.NewMethodDescr(UCDType, "mirrored", ucdMirrored))
	objects.SetTypeDescr(UCDType, "east_asian_width", objects.NewMethodDescr(UCDType, "east_asian_width", ucdEastAsianWidth))
	objects.SetTypeDescr(UCDType, "decomposition", objects.NewMethodDescr(UCDType, "decomposition", ucdDecomposition))
	objects.SetTypeDescr(UCDType, "name", objects.NewMethodDescr(UCDType, "name", ucdName))
	objects.SetTypeDescr(UCDType, "lookup", objects.NewMethodDescr(UCDType, "lookup", ucdLookup))
	objects.SetTypeDescr(UCDType, "is_normalized", objects.NewMethodDescr(UCDType, "is_normalized", ucdIsNormalized))
	objects.SetTypeDescr(UCDType, "normalize", objects.NewMethodDescr(UCDType, "normalize", ucdNormalize))
}

// newUCD builds a UCD instance bound to the given snapshot helpers.
//
// CPython: Modules/unicodedata.c:97 new_previous_version
func newUCD(name string, getRecord func(rune) changeRecord, normalization func(rune) rune) *UCD {
	u := &UCD{name: name, getRecord: getRecord, normalization: normalization}
	u.Init(UCDType)
	return u
}

// getChange320 ports get_change_3_2_0. Two-stage page walk over the
// (changes320Index, changes320Data) tables returns the index into
// changeRecords320 for code.
//
// CPython: Modules/unicodedata_db.h:8336 get_change_3_2_0
func getChange320(code rune) changeRecord {
	if code < 0 || code >= 0x110000 {
		return changeRecords320[0]
	}
	idx := int(changes320Index[code>>8])
	idx = int(changes320Data[(idx<<8)+(int(code)&0xFF)])
	return changeRecords320[idx]
}

// normalize320Func ports normalization_3_2_0. The switch covers five
// codepoints whose NFD decomposition changed after Unicode 3.2; for
// anything else it returns 0 so the caller falls through to the
// modern table.
//
// CPython: Modules/unicodedata_db.h:8347 normalization_3_2_0
func normalize320Func(code rune) rune {
	if v, ok := normalize320[code]; ok {
		return v
	}
	return 0
}

// ucd320 is the module-level ucd_3_2_0 instance the IDNA codec uses.
// Built once at module construction; the UCD type's lack of TpNew
// means Python code cannot mint additional instances.
//
// CPython: Modules/unicodedata.c:1703 new_previous_version (unicodedata_exec)
var ucd320 = newUCD("3.2.0", getChange320, normalize320Func)

// ucdSelfAndArgs splits args[0] off as the UCD receiver and returns
// (self, rest). Used by every UCD.* method below: MethodDescr puts
// the receiver in args[0] and forwards the rest to the closure.
func ucdSelfAndArgs(fname string, args []objects.Object) (*UCD, []objects.Object, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("TypeError: %s() missing self argument", fname)
	}
	self, ok := args[0].(*UCD)
	if !ok {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' for 'unicodedata.UCD' objects doesn't apply to a '%s' object", fname, args[0].Type().Name)
	}
	return self, args[1:], nil
}

func ucdDecimal(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("decimal", args)
	if err != nil {
		return nil, err
	}
	return decimalImpl(self, rest, kwargs)
}

func ucdDigit(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	_, rest, err := ucdSelfAndArgs("digit", args)
	if err != nil {
		return nil, err
	}
	return digitImpl(rest, kwargs)
}

func ucdNumeric(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("numeric", args)
	if err != nil {
		return nil, err
	}
	return numericImpl(self, rest, kwargs)
}

func ucdCategory(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("category", args)
	if err != nil {
		return nil, err
	}
	return categoryImpl(self, rest, kwargs)
}

func ucdBidirectional(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("bidirectional", args)
	if err != nil {
		return nil, err
	}
	return bidirectionalImpl(self, rest, kwargs)
}

func ucdCombining(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("combining", args)
	if err != nil {
		return nil, err
	}
	return combiningImpl(self, rest, kwargs)
}

func ucdMirrored(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("mirrored", args)
	if err != nil {
		return nil, err
	}
	return mirroredImpl(self, rest, kwargs)
}

func ucdEastAsianWidth(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("east_asian_width", args)
	if err != nil {
		return nil, err
	}
	return eastAsianWidthImpl(self, rest, kwargs)
}

func ucdDecomposition(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("decomposition", args)
	if err != nil {
		return nil, err
	}
	return decompositionImpl(self, rest, kwargs)
}

func ucdName(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("name", args)
	if err != nil {
		return nil, err
	}
	return nameImpl(self, rest, kwargs)
}

func ucdLookup(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("lookup", args)
	if err != nil {
		return nil, err
	}
	return lookupImpl(self, rest, kwargs)
}

func ucdIsNormalized(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("is_normalized", args)
	if err != nil {
		return nil, err
	}
	return isNormalizedImpl(self, rest, kwargs)
}

func ucdNormalize(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	self, rest, err := ucdSelfAndArgs("normalize", args)
	if err != nil {
		return nil, err
	}
	return normalizeImpl(self, rest, kwargs)
}
