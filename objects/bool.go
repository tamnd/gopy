package objects

// Bool is the Python bool. CPython makes bool a subclass of int with
// just two singleton instances; gopy follows the same pattern.
//
// CPython: Objects/boolobject.c:L24 PyBool_Type
type Bool struct {
	Int
}

// BoolType is the type singleton for bool. Mirrors PyBool_Type.
//
// CPython: Objects/boolobject.c:L222 PyBool_Type
var BoolType = NewType("bool", []*Type{IntType})

var (
	trueSingleton  *Bool
	falseSingleton *Bool
)

func init() {
	BoolType.Repr = boolRepr
	BoolType.Str = boolRepr
	// bool inherits int's ordering but overrides hash (True→1, False→0).
	// intRichCmp is assigned to IntType.RichCmp in int.go's init, which
	// may run after NewType("bool") already ran inheritSlotsAllMRO, so we
	// must set it explicitly here rather than relying on inheritance.
	//
	// CPython: Objects/boolobject.c:L222 PyBool_Type (no tp_richcompare)
	BoolType.RichCmp = intRichCmp
	BoolType.Hash = func(o Object) (int64, error) {
		if o == trueSingleton {
			return 1, nil
		}
		return 0, nil
	}
	BoolType.TpFlags |= TpFlagMatchSelf
	trueSingleton = newBool(1)
	falseSingleton = newBool(0)
}

func newBool(x int64) *Bool {
	b := &Bool{}
	b.init(BoolType)
	b.v.SetInt64(x)
	b.MakeImmortal()
	return b
}

// True returns the True singleton. Mirrors Py_True.
//
// CPython: Include/object.h:L860 Py_True
func True() Object { return trueSingleton }

// False returns the False singleton. Mirrors Py_False.
//
// CPython: Include/object.h:L857 Py_False
func False() Object { return falseSingleton }

// NewBool returns the True or False singleton. Mirrors PyBool_FromLong.
//
// CPython: Objects/boolobject.c:L33 PyBool_FromLong
func NewBool(b bool) Object {
	if b {
		return trueSingleton
	}
	return falseSingleton
}

func boolRepr(o Object) (string, error) {
	if o == trueSingleton {
		return "True", nil
	}
	return "False", nil
}
