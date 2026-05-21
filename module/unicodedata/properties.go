// Per-character property queries: category, bidirectional,
// combining, mirrored, east_asian_width.
//
// Each one looks up the _PyUnicode_DatabaseRecord for the character
// (getRecord, ports _getrecord_ex) and maps the integer index back
// to its string name via the *Names slices generated from CPython.
// When called as a UCD-instance method the impl checks the matching
// change_record overlay first, mirroring UCD_Check in the C bodies.
//
// CPython: Modules/unicodedata.c

package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// CPython: Modules/unicodedata.c:264 unicodedata_UCD_category_impl
func categoryImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("category", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).Category)
	if self != nil {
		old := self.getRecord(r)
		if old.CategoryChanged != 0xFF {
			idx = int(old.CategoryChanged)
		}
	}
	if idx >= len(categoryNames) {
		return nil, fmt.Errorf("SystemError: category index out of range")
	}
	return objects.NewStr(categoryNames[idx]), nil
}

// CPython: Modules/unicodedata.c:291 unicodedata_UCD_bidirectional_impl
func bidirectionalImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("bidirectional", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).Bidirectional)
	if self != nil {
		old := self.getRecord(r)
		switch {
		case old.CategoryChanged == 0:
			idx = 0
		case old.BidirChanged != 0xFF:
			idx = int(old.BidirChanged)
		}
	}
	if idx >= len(bidirectionalNames) {
		return nil, fmt.Errorf("SystemError: bidirectional index out of range")
	}
	return objects.NewStr(bidirectionalNames[idx]), nil
}

// CPython: Modules/unicodedata.c:320 unicodedata_UCD_combining_impl
func combiningImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("combining", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).Combining)
	if self != nil && self.getRecord(r).CategoryChanged == 0 {
		idx = 0
	}
	return objects.NewInt(int64(idx)), nil
}

// CPython: Modules/unicodedata.c:348 unicodedata_UCD_mirrored_impl
func mirroredImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("mirrored", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).Mirrored)
	if self != nil {
		old := self.getRecord(r)
		switch {
		case old.CategoryChanged == 0:
			idx = 0
		case old.MirroredChanged != 0xFF:
			idx = int(old.MirroredChanged)
		}
	}
	return objects.NewInt(int64(idx)), nil
}

// CPython: Modules/unicodedata.c:375 unicodedata_UCD_east_asian_width_impl
func eastAsianWidthImpl(self *UCD, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("east_asian_width", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).EastAsianWidth)
	if self != nil {
		old := self.getRecord(r)
		switch {
		case old.CategoryChanged == 0:
			idx = 0
		case old.EastAsianWidthChanged != 0xFF:
			idx = int(old.EastAsianWidthChanged)
		}
	}
	if idx >= len(eastAsianWidthNames) {
		return nil, fmt.Errorf("SystemError: east_asian_width index out of range")
	}
	return objects.NewStr(eastAsianWidthNames[idx]), nil
}

func categoryBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return categoryImpl(nil, args, kwargs)
}

func bidirectionalBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return bidirectionalImpl(nil, args, kwargs)
}

func combiningBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return combiningImpl(nil, args, kwargs)
}

func mirroredBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return mirroredImpl(nil, args, kwargs)
}

func eastAsianWidthBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return eastAsianWidthImpl(nil, args, kwargs)
}
