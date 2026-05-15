// functools: redirected to stdlib/functools.py.
// Intentionally no inittab registration so PathFinder serves
// Lib/functools.py instead. The C accelerator names (partial,
// reduce, cmp_to_key, _lru_cache_wrapper, Placeholder) live in
// module/_functools; the vendored .py imports them via
// `from _functools import ...`.
//
// CPython: Lib/functools.py
package functools
