// decimal / digit / numeric. Each one looks up the
// _PyUnicode_TypeRecord for the character (gettyperecord, the
// unicodetype_db.h walk) and returns the matching field, falling
// back to the caller's default (or ValueError) when the character
// does not carry that property. When called as a UCD-instance method
// decimal and numeric also consult the change_record overlay; CPython
// leaves digit on the modern table because the digit field is not
// versioned.
//
// CPython: Objects/unicodectype.c:43 gettyperecord
// CPython: Objects/unicodectype.c:104 _PyUnicode_ToDecimalDigit
// CPython: Objects/unicodectype.c:121 _PyUnicode_ToDigit
// CPython: Objects/unicodetype_db.h:4513 _PyUnicode_ToNumeric

package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/unicodetype"
)

// CPython: Modules/unicodedata.c:132 unicodedata_UCD_decimal_impl
func decimalImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, def, err := argCharWithDefault("decimal", args)
	if err != nil {
		return nil, err
	}
	rc := int64(-1)
	haveOld := false
	if self != nil {
		old := self.getRecord(r)
		switch {
		case old.CategoryChanged == 0:
			haveOld = true
			rc = -1
		case old.DecimalChanged != 0xFF:
			haveOld = true
			rc = int64(old.DecimalChanged)
		}
	}
	if !haveOld {
		if d := unicodetype.ToDecimalDigit(r); d >= 0 {
			rc = int64(d)
		}
	}
	if rc < 0 {
		if def != nil {
			return def, nil
		}
		return nil, fmt.Errorf("ValueError: not a decimal")
	}
	return objects.NewInt(rc), nil
}

// CPython: Modules/unicodedata.c:184 unicodedata_UCD_digit_impl
func digitImpl(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, def, err := argCharWithDefault("digit", args)
	if err != nil {
		return nil, err
	}
	d := unicodetype.ToDigit(r)
	if d < 0 {
		if def != nil {
			return def, nil
		}
		return nil, fmt.Errorf("ValueError: not a digit")
	}
	return objects.NewInt(int64(d)), nil
}

// CPython: Modules/unicodedata.c:218 unicodedata_UCD_numeric_impl
// CPython: Objects/unicodetype_db.h:4513 _PyUnicode_ToNumeric
func numericImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, def, err := argCharWithDefault("numeric", args)
	if err != nil {
		return nil, err
	}
	rc := -1.0
	haveOld := false
	if self != nil {
		old := self.getRecord(r)
		switch {
		case old.CategoryChanged == 0:
			haveOld = true
			rc = -1.0
		case old.NumericChanged != 0.0:
			haveOld = true
			rc = old.NumericChanged
		}
	}
	if !haveOld {
		if v, ok := unicodetype.ToNumeric(r); ok {
			rc = v
		}
	}
	if rc == -1.0 {
		if def != nil {
			return def, nil
		}
		return nil, fmt.Errorf("ValueError: not a numeric character")
	}
	return objects.NewFloat(rc), nil
}

func decimalBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return decimalImpl(nil, args, kwargs)
}

func digitBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return digitImpl(args, kwargs)
}

func numericBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return numericImpl(nil, args, kwargs)
}
