// Errors local to the weakref package. Two distinct messages are
// needed: bare "key not found" for Remove(), and a specific phrasing
// for Pop() on an empty container that mirrors CPython.
//
// CPython: Lib/_weakrefset.py:59 raise KeyError('pop from empty WeakSet')

package weakref

import "errors"

var (
	errKeyNotFound    = errors.New("KeyError")
	errKeyNotFoundPop = errors.New("KeyError: pop from empty WeakSet")
)
