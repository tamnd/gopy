// Token type. Records the binding cv had in ctx before the matching
// cv.Set() call. Reset replays the recorded binding (or removes the
// key when the variable was previously unset). Token also doubles as a
// context manager: __enter__ returns the token and __exit__ resets the
// variable, so `with cv.set(v):` restores the prior value on exit.
//
// CPython: Python/context.c:1115 Token

package contextvars

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

func init() {
	// Register var / old_value as read-only getsets and the context
	// manager protocol methods, then expose Token.MISSING as a class
	// attribute. PyObject_GenericGetAttr (the inherited slot) resolves
	// them. Token is unhashable and not subclassable, matching the C
	// type's PyObject_HashNotImplemented and missing Py_TPFLAGS_BASETYPE.
	//
	// CPython: Python/context.c:1215 PyContextTokenType_getsetlist
	// CPython: Python/context.c:1257 PyContextTokenType_methods
	// CPython: Python/context.c:1278 .tp_hash = PyObject_HashNotImplemented
	objects.SetTypeDescr(TokenType, "var",
		objects.NewGetSetDescr("var", tokenGetVar, nil))
	objects.SetTypeDescr(TokenType, "old_value",
		objects.NewGetSetDescr("old_value", tokenGetOldValue, nil))
	objects.SetTypeDescr(TokenType, "__enter__",
		objects.NewMethodDescr(TokenType, "__enter__", tokenEnter))
	objects.SetTypeDescr(TokenType, "__exit__",
		objects.NewMethodDescr(TokenType, "__exit__", tokenExit))
	objects.SetTypeDescr(TokenType, "MISSING", Missing)
	TokenType.Repr = tokenReprSlot
	TokenType.Str = tokenReprSlot
	TokenType.Hash = func(objects.Object) (int64, error) {
		return 0, fmt.Errorf("TypeError: unhashable type: 'Token'")
	}
	TokenType.Dealloc = tokenDealloc
	TokenType.TpTraverse = tokenTraverse
	TokenType.TpFlags &^= objects.TpFlagBasetype
}

// newToken records the binding to undo. The token owns +1 on the
// context, the variable, and the prior value, mirroring CPython's
// Py_NewRef(ctx) / Py_NewRef(var) / Py_XNewRef(val). The reference on
// oldVal is essential: the cv.Set that mints the token replaces oldVal
// in the HAMT immediately, so without this hold the old value could
// drop to refcount 0 before Reset replays it.
//
// CPython: Python/context.c:1135 token_new
func newToken(ctx *Context, cv *ContextVar, oldVal objects.Object, hadOld bool) *Token {
	t := &Token{ctx: ctx, cv: cv, oldVal: oldVal, hadOld: hadOld}
	t.Init(TokenType)
	objects.Incref(ctx)
	objects.Incref(cv)
	if oldVal != nil {
		objects.Incref(oldVal)
	}
	return t
}

// tokenDealloc releases the references the token holds.
//
// CPython: Python/context.c:1147 token_tp_dealloc
func tokenDealloc(o objects.Object) {
	t := o.(*Token)
	if t.ctx != nil {
		objects.Decref(t.ctx)
		t.ctx = nil
	}
	if t.cv != nil {
		objects.Decref(t.cv)
		t.cv = nil
	}
	if t.oldVal != nil {
		objects.Decref(t.oldVal)
		t.oldVal = nil
	}
}

// tokenTraverse visits the token's held references for the cyclic
// collector.
//
// CPython: Python/context.c:1153 token_tp_traverse
func tokenTraverse(o objects.Object, visit objects.Visitor) error {
	t := o.(*Token)
	if t.ctx != nil {
		if err := visit(t.ctx); err != nil {
			return err
		}
	}
	if t.cv != nil {
		if err := visit(t.cv); err != nil {
			return err
		}
	}
	if t.oldVal != nil {
		return visit(t.oldVal)
	}
	return nil
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

func tokenSelf(name string, args []objects.Object) (*Token, []objects.Object, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' of 'Token' object needs an argument", name)
	}
	t, ok := args[0].(*Token)
	if !ok {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'Token' object", name)
	}
	return t, args[1:], nil
}

// tokenGetVar backs the read-only `var` getset.
//
// CPython: Python/context.c:1198 token_get_var
func tokenGetVar(o objects.Object) (objects.Object, error) {
	t, ok := o.(*Token)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'var' requires a 'Token' object")
	}
	return t.cv, nil
}

// tokenGetOldValue backs the read-only `old_value` getset.
//
// CPython: Python/context.c:1204 token_get_old_value
func tokenGetOldValue(o objects.Object) (objects.Object, error) {
	t, ok := o.(*Token)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'old_value' requires a 'Token' object")
	}
	return t.OldValue(), nil
}

// tokenEnter implements Token.__enter__: it returns the token itself so
// `with cv.set(v) as tok:` binds the token.
//
// CPython: Python/context.c:1227 token_enter_impl
func tokenEnter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t, _, err := tokenSelf("__enter__", args)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// tokenExit implements Token.__exit__(type, val, tb): it resets the
// linked ContextVar to the value it held before the set that minted
// the token, restoring the prior binding when the block exits.
//
// CPython: Python/context.c:1246 token_exit_impl
func tokenExit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	t, rest, err := tokenSelf("__exit__", args)
	if err != nil {
		return nil, err
	}
	if len(rest) != 3 {
		return nil, fmt.Errorf("TypeError: __exit__() takes exactly 3 arguments (%d given)", len(rest))
	}
	ts, err := currentTS()
	if err != nil {
		return nil, err
	}
	if rerr := t.cv.Reset(ts, t); rerr != nil {
		return nil, rerr
	}
	return objects.None(), nil
}

// tokenReprSlot is the tp_repr slot wrapper.
func tokenReprSlot(o objects.Object) (string, error) {
	t, ok := o.(*Token)
	if !ok {
		return "", fmt.Errorf("TypeError: expected Token")
	}
	return tokenRepr(t), nil
}

// tokenRepr renders "<Token var=<ContextVar ...> at 0x...>", inserting
// " used" after "<Token" once Reset has consumed the token. The var
// clause embeds the ContextVar's own repr so callers that compare
// repr(token) against repr(var) see a byte-identical substring.
//
// CPython: Python/context.c:1166 token_tp_repr
func tokenRepr(t *Token) string {
	used := ""
	if t.used {
		used = " used"
	}
	varRepr, err := contextVarRepr(t.cv)
	if err != nil {
		varRepr = "<ContextVar>"
	}
	return fmt.Sprintf("<Token%s var=%s at %p>", used, varRepr, t)
}
