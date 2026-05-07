// Port of cpython/Lib/ast.py:50 literal_eval. Reduces an AST or
// expression-string to its Python literal value. Supports the
// documented set: strings, bytes, numbers (int/float/complex),
// tuples, lists, dicts, sets, booleans, None, and signed-numeric
// /arithmetic-of-complex BinOps. Anything outside that grammar is a
// "malformed node" error, matching CPython.

package ast

import (
	"fmt"
	"math/big"
)

// LiteralEval ports literal_eval. Strings are first parsed; passing a
// *Module or *Expression unwraps the body before evaluating.
func LiteralEval(node any) (any, error) {
	switch n := node.(type) {
	case *Expression:
		return literalEvalConvert(n.Body)
	case *Module:
		if len(n.Body) != 1 {
			return nil, malformed(node)
		}
		es, ok := n.Body[0].(*ExprStmt)
		if !ok {
			return nil, malformed(node)
		}
		return literalEvalConvert(es.Value)
	}
	return literalEvalConvert(node)
}

func literalEvalConvert(node any) (any, error) {
	switch n := node.(type) {
	case *Constant:
		return n.Value, nil
	case *Tuple:
		out := make([]any, n.Elts.Len())
		for i := 0; i < n.Elts.Len(); i++ {
			v, err := literalEvalConvert(n.Elts.Get(i))
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case *List:
		out := make([]any, n.Elts.Len())
		for i := 0; i < n.Elts.Len(); i++ {
			v, err := literalEvalConvert(n.Elts.Get(i))
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case *Set:
		out := make(map[any]struct{}, n.Elts.Len())
		for i := 0; i < n.Elts.Len(); i++ {
			v, err := literalEvalConvert(n.Elts.Get(i))
			if err != nil {
				return nil, err
			}
			out[v] = struct{}{}
		}
		return out, nil
	case *Call:
		// Only `set()` (the empty-set call) is allowed by literal_eval.
		name, ok := n.Func.(*Name)
		if ok && name.Id == "set" && n.Args.Len() == 0 && n.Keywords.Len() == 0 {
			return map[any]struct{}{}, nil
		}
		return nil, malformed(node)
	case *Dict:
		if n.Keys.Len() != n.Values.Len() {
			return nil, malformed(node)
		}
		out := make(map[any]any, n.Keys.Len())
		for i := 0; i < n.Keys.Len(); i++ {
			k, err := literalEvalConvert(n.Keys.Get(i))
			if err != nil {
				return nil, err
			}
			v, err := literalEvalConvert(n.Values.Get(i))
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	case *BinOp:
		if n.Op != Add && n.Op != Sub {
			return nil, malformed(node)
		}
		left, err := literalEvalSignedNum(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := literalEvalNum(n.Right)
		if err != nil {
			return nil, err
		}
		if !isReal(left) || !isComplex(right) {
			return literalEvalSignedNum(node)
		}
		if n.Op == Add {
			return numAdd(left, right), nil
		}
		return numSub(left, right), nil
	}
	return literalEvalSignedNum(node)
}

func literalEvalNum(node any) (any, error) {
	c, ok := node.(*Constant)
	if !ok {
		return nil, malformed(node)
	}
	switch c.Value.(type) {
	case int, int64, *big.Int, float64, complex128:
		return c.Value, nil
	}
	return nil, malformed(node)
}

func literalEvalSignedNum(node any) (any, error) {
	if u, ok := node.(*UnaryOp); ok && (u.Op == UAdd || u.Op == USub) {
		operand, err := literalEvalNum(u.Operand)
		if err != nil {
			return nil, err
		}
		if u.Op == UAdd {
			return operand, nil
		}
		return numNeg(operand), nil
	}
	return literalEvalNum(node)
}

func malformed(node any) error {
	return fmt.Errorf("malformed node or string: %T", node)
}

func isReal(v any) bool {
	switch v.(type) {
	case int, int64, *big.Int, float64:
		return true
	}
	return false
}

func isComplex(v any) bool {
	_, ok := v.(complex128)
	return ok
}

func numNeg(v any) any {
	switch x := v.(type) {
	case int:
		return -x
	case int64:
		return -x
	case *big.Int:
		return new(big.Int).Neg(x)
	case float64:
		return -x
	case complex128:
		return -x
	}
	return v
}

func numAdd(left, right any) any {
	if l, ok := left.(int); ok {
		if r, ok := right.(complex128); ok {
			return complex(float64(l), 0) + r
		}
	}
	if l, ok := left.(float64); ok {
		if r, ok := right.(complex128); ok {
			return complex(l, 0) + r
		}
	}
	return left
}

func numSub(left, right any) any {
	if l, ok := left.(int); ok {
		if r, ok := right.(complex128); ok {
			return complex(float64(l), 0) - r
		}
	}
	if l, ok := left.(float64); ok {
		if r, ok := right.(complex128); ok {
			return complex(l, 0) - r
		}
	}
	return left
}
