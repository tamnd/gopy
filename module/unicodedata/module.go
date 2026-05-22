// Package unicodedata is the gopy port of CPython's
// Modules/unicodedata.c. It exposes the Unicode Character Database
// query surface used by stdlib: normalize / is_normalized for the
// four normal forms, category / bidirectional / combining /
// mirrored / east_asian_width for per-character properties,
// decimal / digit / numeric for numeric values, name / lookup for
// character names, and the unidata_version constant.
//
// The data tables live in data_gen.go (re-generated from CPython's
// pre-built Modules/unicodedata_db.h via Tools/unicodedata_go).
// Parsing CPython's already-generated C output keeps the runtime
// tables byte-identical to CPython and lets the generator stay
// small.
//
// CPython: Modules/unicodedata.c
package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// BuildModule materializes the unicodedata module dict. Mirrors
// PyInit_unicodedata: the module-level functions dispatch through
// the default UCD instance; ucd_3_2_0 is the Unicode 3.2 snapshot
// the IDNA codec uses. Registration with imp.AppendInittab lives in
// stdlibinit/registry.go so this package stays free of the imp ->
// marshal -> specialize -> compile dependency chain. That lets
// parser/pegen import unicodedata for PEP 3131 NFKC identifier
// normalisation without creating a test-time import cycle through
// compile <- parser <- parser/pegen <- unicodedata.
//
// CPython: Modules/unicodedata.c:1734 PyInit_unicodedata
func BuildModule() (*objects.Module, error) {
	m := objects.NewModule("unicodedata")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"normalize", objects.NewBuiltinFunction("normalize", normalizeBuiltin)},
		{"is_normalized", objects.NewBuiltinFunction("is_normalized", isNormalizedBuiltin)},
		{"category", objects.NewBuiltinFunction("category", categoryBuiltin)},
		{"bidirectional", objects.NewBuiltinFunction("bidirectional", bidirectionalBuiltin)},
		{"combining", objects.NewBuiltinFunction("combining", combiningBuiltin)},
		{"mirrored", objects.NewBuiltinFunction("mirrored", mirroredBuiltin)},
		{"east_asian_width", objects.NewBuiltinFunction("east_asian_width", eastAsianWidthBuiltin)},
		{"decimal", objects.NewBuiltinFunction("decimal", decimalBuiltin)},
		{"digit", objects.NewBuiltinFunction("digit", digitBuiltin)},
		{"numeric", objects.NewBuiltinFunction("numeric", numericBuiltin)},
		{"decomposition", objects.NewBuiltinFunction("decomposition", decompositionBuiltin)},
		{"name", objects.NewBuiltinFunction("name", nameBuiltin)},
		{"lookup", objects.NewBuiltinFunction("lookup", lookupBuiltin)},
		{"unidata_version", objects.NewStr(unidataVersion)},
		{"UCD", UCDType},
		{"ucd_3_2_0", ucd320},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// argChar extracts the single-char argument the per-character
// query functions take. Mirrors CPython's `int` clinic converter
// that accepts a 1-character str and rejects anything else with
// TypeError. Returns the rune plus the original Object for use in
// error messages.
func argChar(fname string, args []objects.Object) (rune, error) {
	if len(args) < 1 || len(args) > 2 {
		return 0, fmt.Errorf("TypeError: %s() takes 1 or 2 arguments (%d given)", fname, len(args))
	}
	s, ok := args[0].(*objects.Unicode)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s() argument 1 must be a unicode character, not %s", fname, args[0].Type().Name)
	}
	r := s.Value()
	rs := []rune(r)
	if len(rs) != 1 {
		return 0, fmt.Errorf("TypeError: %s() argument 1 must be a unicode character, not str of length %d", fname, len(rs))
	}
	return rs[0], nil
}

// argCharWithDefault is argChar plus an optional default argument
// (for decimal / digit / numeric / name).
func argCharWithDefault(fname string, args []objects.Object) (rune, objects.Object, error) {
	r, err := argChar(fname, args)
	if err != nil {
		return 0, nil, err
	}
	var def objects.Object
	if len(args) == 2 {
		def = args[1]
	}
	return r, def, nil
}

// getRecord returns the _PyUnicode_DatabaseRecord for code. Mirrors
// CPython's _getrecord_ex with the shift-then-index2 walk.
//
// CPython: Modules/unicodedata.c:59 _getrecord_ex
func getRecord(code rune) dbRecord {
	if code < 0 || code >= 0x110000 {
		return dbRecords[0]
	}
	idx := int(index1[code>>recShift])
	idx = int(index2[(idx<<recShift)+(int(code)&((1<<recShift)-1))])
	return dbRecords[idx]
}
