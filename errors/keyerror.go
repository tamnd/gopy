package errors

import "github.com/tamnd/gopy/objects"

// KeyError formats its single arg via repr() rather than str(), so a
// missing string key prints as "'name'" instead of just "name". CPython
// installs this override on PyExc_KeyError.tp_str. Multi-arg KeyError
// falls back to BaseException's tuple repr.
//
// CPython: Objects/exceptions.c:L2867 KeyError_str
func keyErrorStr(o objects.Object) (string, error) {
	exc, ok := o.(*Exception)
	if !ok || exc.Args == nil || exc.Args.Len() != 1 {
		if exc != nil {
			return exc.Message(), nil
		}
		return "", nil
	}
	return objects.Repr(exc.Args.Item(0))
}

func init() {
	PyExc_KeyError.Str = keyErrorStr
}
