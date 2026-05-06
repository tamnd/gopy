// Token.MISSING singleton. Mirrors the static
// `_Py_SINGLETON(context_token_missing)` and the tiny tp_repr that
// returns the "<Token.MISSING>" string.
//
// CPython: Python/context.c:1303 Token.MISSING

package contextvar

import "github.com/tamnd/gopy/objects"

// tokenMissing is the singleton value Token.old_value returns when
// the matching cv.set() was made on a fresh ContextVar (the variable
// was previously unset).
//
// CPython: Python/context.c:1336 get_token_missing
type tokenMissing struct {
	objects.Header
}

// Missing is the public Token.MISSING sentinel. Two pointers compare
// equal only when they reference this singleton.
//
// CPython: Python/context.c:1338 _Py_SINGLETON(context_token_missing)
var Missing objects.Object = newTokenMissing()

func newTokenMissing() *tokenMissing {
	tokenMissingType.Repr = func(_ objects.Object) (string, error) {
		return "<Token.MISSING>", nil
	}
	tokenMissingType.Str = tokenMissingType.Repr
	m := &tokenMissing{}
	m.Init(tokenMissingType)
	return m
}
