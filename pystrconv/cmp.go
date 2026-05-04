package pystrconv

// Cross-platform case-insensitive string compare. Ported from
// cpython/Python/pystrcmp.c. ASCII-only; the locale-dependent libc
// equivalents are deliberately not used.

// CompareInsensitive returns negative, zero, or positive depending on
// whether a is lexicographically less, equal, or greater than b under
// ASCII case folding. Mirrors PyOS_mystricmp.
//
// CPython: Python/pystrcmp.c:L21 PyOS_mystricmp
func CompareInsensitive(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		la := ToLower(a[i])
		lb := ToLower(b[i])
		if la != lb {
			return int(la) - int(lb)
		}
	}
	return len(a) - len(b)
}

// CompareInsensitiveN compares the first size bytes of a and b under
// ASCII case folding. Mirrors PyOS_mystrnicmp. CPython's
// implementation walks size-1 bytes and then compares the size'th
// pair, so passing size==0 returns 0 with no comparison.
//
// CPython: Python/pystrcmp.c:L7 PyOS_mystrnicmp
func CompareInsensitiveN(a, b string, size int) int {
	if size <= 0 {
		return 0
	}
	size = min(size, len(a), len(b))
	for i := range size {
		la := ToLower(a[i])
		lb := ToLower(b[i])
		if la != lb {
			return int(la) - int(lb)
		}
	}
	return 0
}
