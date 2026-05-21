// Per-character property queries: category, bidirectional,
// combining, mirrored, east_asian_width.
//
// Each one looks up the _PyUnicode_DatabaseRecord for the character
// (getRecord, ports _getrecord_ex) and maps the integer index back
// to its string name via the *Names slices generated from CPython.
//
// CPython: Modules/unicodedata.c

package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// CPython: Modules/unicodedata.c:264 unicodedata_UCD_category_impl
func categoryBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("category", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).Category)
	if idx >= len(categoryNames) {
		return nil, fmt.Errorf("SystemError: category index out of range")
	}
	return objects.NewStr(categoryNames[idx]), nil
}

// CPython: Modules/unicodedata.c:291 unicodedata_UCD_bidirectional_impl
func bidirectionalBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("bidirectional", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).Bidirectional)
	if idx >= len(bidirectionalNames) {
		return nil, fmt.Errorf("SystemError: bidirectional index out of range")
	}
	return objects.NewStr(bidirectionalNames[idx]), nil
}

// CPython: Modules/unicodedata.c:320 unicodedata_UCD_combining_impl
func combiningBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("combining", args)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(getRecord(r).Combining)), nil
}

// CPython: Modules/unicodedata.c:348 unicodedata_UCD_mirrored_impl
func mirroredBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("mirrored", args)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(getRecord(r).Mirrored)), nil
}

// CPython: Modules/unicodedata.c:375 unicodedata_UCD_east_asian_width_impl
func eastAsianWidthBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, err := argChar("east_asian_width", args)
	if err != nil {
		return nil, err
	}
	idx := int(getRecord(r).EastAsianWidth)
	if idx >= len(eastAsianWidthNames) {
		return nil, fmt.Errorf("SystemError: east_asian_width index out of range")
	}
	return objects.NewStr(eastAsianWidthNames[idx]), nil
}
