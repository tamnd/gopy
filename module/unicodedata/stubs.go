// Stubs for the unicodedata APIs that still need their own data
// tables (name / lookup draw from Modules/unicodename_db.h). Each
// function parses its arguments like CPython and returns the
// caller-supplied default when present; otherwise it raises
// NotImplementedError.

package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

func nameBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if _, def, err := argCharWithDefault("name", args); err != nil {
		return nil, err
	} else if def != nil {
		return def, nil
	}
	return nil, fmt.Errorf("NotImplementedError: unicodedata.name not implemented yet")
}

func lookupBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: lookup() takes exactly 1 argument (%d given)", len(args))
	}
	if _, ok := args[0].(*objects.Unicode); !ok {
		return nil, fmt.Errorf("TypeError: lookup() argument must be str, not %s", args[0].Type().Name)
	}
	return nil, fmt.Errorf("NotImplementedError: unicodedata.lookup not implemented yet")
}
