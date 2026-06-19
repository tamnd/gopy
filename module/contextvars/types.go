// Type registrations for Context, ContextVar, Token, and the
// Token.MISSING singleton type. Mirrors the four PyTypeObject
// declarations in cpython/Python/context.c.
//
// CPython: Python/context.c:753 PyContext_Type / 1097 PyContextVar_Type / 1265 PyContextToken_Type / 1324 _PyContextTokenMissing_Type

package contextvars

import "github.com/tamnd/gopy/objects"

// ContextType, ContextVarType, TokenType are the runtime types for
// the three classes the _contextvars module exposes.
var (
	ContextType    = objects.NewType("Context", []*objects.Type{objects.ObjectType()})
	ContextVarType = objects.NewType("ContextVar", []*objects.Type{objects.ObjectType()})
	TokenType      = objects.NewType("Token", []*objects.Type{objects.ObjectType()})
)

// tokenMissingType is the singleton type for Token.MISSING. CPython
// keeps it private; we mirror that by leaving it unexported.
//
// CPython: Python/context.c:1324 _PyContextTokenMissing_Type
var tokenMissingType = objects.NewType("Token.MISSING", []*objects.Type{objects.ObjectType()})

func init() {
	// Context owns +1 on its HAMT; release it on dealloc and expose it
	// to the cyclic collector.
	//
	// CPython: Python/context.c:495 context_tp_dealloc / context_tp_traverse
	ContextType.Dealloc = contextDealloc
	ContextType.TpTraverse = contextTraverse

	// ContextVar and Token are subscriptable via __class_getitem__.
	// CPython: Python/context.c contextvar_methods / token_methods
	objects.BindClassGetitem(ContextVarType)
	objects.BindClassGetitem(TokenType)
	ContextVarType.Dealloc = contextVarDealloc
	ContextVarType.TpTraverse = contextVarTraverse
	ContextVarType.Hash = func(o objects.Object) (int64, error) {
		return o.(*ContextVar).hash, nil
	}
	ContextVarType.RichCmp = func(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
		bv, ok := b.(*ContextVar)
		if !ok {
			return objects.NotImplemented(), nil
		}
		av := a.(*ContextVar)
		switch op {
		case objects.CompareEQ:
			return objects.NewBool(av == bv), nil
		case objects.CompareNE:
			return objects.NewBool(av != bv), nil
		}
		return objects.NotImplemented(), nil
	}
}
