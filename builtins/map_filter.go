// Map and Filter wrap the iterator types in objects/. The split is
// deliberate: gopy keeps the iterator state with the rest of the
// builtin types (objects.Map, objects.Filter), and the builtin entry
// points stay here next to the other iteration helpers.
//
// CPython: Python/bltinmodule.c:1361 map_new
// CPython: Python/bltinmodule.c:501 filter_new

package builtins

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// Map ports the map() entry point. Positional args are
// (function, *iterables); the only keyword honored is strict=, which
// forces the iterators to agree on length.
//
// CPython: Python/bltinmodule.c:1361 map_new
func Map(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: map() must have at least two arguments")
	}
	strict := false
	for k, v := range kwargs {
		if k != "strict" {
			return nil, fmt.Errorf("TypeError: map() got an unexpected keyword argument '%s'", k)
		}
		b, err := objects.IsTruthy(v)
		if err != nil {
			return nil, err
		}
		strict = b
	}
	return objects.NewMap(args[0], args[1:], strict)
}

// Filter ports the filter() entry point: filter(function, iterable).
// function may be None to mean "use the truth protocol on each item".
//
// CPython: Python/bltinmodule.c:501 filter_new
func Filter(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: filter expected 2 arguments, got %d", len(args))
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: filter() takes no keyword arguments")
	}
	return objects.NewFilter(args[0], args[1])
}
