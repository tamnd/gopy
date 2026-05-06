// Token type. Records the binding cv had in ctx before the matching
// cv.Set() call. Reset replays the recorded binding (or removes the
// key when the variable was previously unset).
//
// CPython: Python/context.c:1115 Token

package contextvar

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// Token is the return value of ContextVar.Set; it carries enough
// state for ContextVar.Reset to undo the set. hadOld is the
// CPython-side `tok_oldval == NULL` distinction made explicit:
// hadOld==false means the variable was unset before, so Reset must
// delete the key rather than rebind it.
//
// CPython: Python/context.c:1115 PyContextToken
type Token struct {
	objects.Header
	ctx    *Context
	cv     *ContextVar
	oldVal objects.Object
	hadOld bool
	used   bool
}

func newToken(ctx *Context, cv *ContextVar, oldVal objects.Object, hadOld bool) *Token {
	t := &Token{ctx: ctx, cv: cv, oldVal: oldVal, hadOld: hadOld}
	t.Init(TokenType)
	return t
}

// Var returns the ContextVar this token was minted by.
//
// CPython: Python/context.c:1198 token_get_var
func (t *Token) Var() *ContextVar { return t.cv }

// OldValue returns the prior value of the variable, or the
// Token.MISSING singleton when the variable was previously unset.
//
// CPython: Python/context.c:1204 token_get_old_value
func (t *Token) OldValue() objects.Object {
	if !t.hadOld {
		return Missing
	}
	return t.oldVal
}

// Used reports whether Reset has consumed t. CPython exposes the
// same flag indirectly via the "<Token used ...>" repr.
//
// CPython: Python/context.c:1296 tok->tok_used
func (t *Token) Used() bool { return t.used }

func tokenRepr(t *Token) string {
	used := ""
	if t.used {
		used = " used"
	}
	return fmt.Sprintf("<Token%s var=<ContextVar name='%s' at %p> at %p>",
		used, t.cv.name, t.cv, t)
}
