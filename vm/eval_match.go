// Pattern-matching bytecode arms: MATCH_MAPPING, MATCH_SEQUENCE,
// MATCH_KEYS, MATCH_CLASS. Ports Python/bytecodes.c match section.
//
// CPython: Python/bytecodes.c:3043 MATCH_CLASS et al.

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 5: MATCH_* bodies migrate to typed op<NAME> functions invoked from vm/eval_dispatch_gen.go.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// tryMatch handles MATCH_* opcodes. Returns ok=false when op is not in
// this panel. err is a dispatch-level error (not retDone).
func (e *evalState) tryMatch(op compile.Opcode, oparg uint32) (next int, ok bool, err error) {
	switch op {
	case compile.MATCH_MAPPING:
		return e.execMatchMapping()

	case compile.MATCH_SEQUENCE:
		return e.execMatchSequence()

	case compile.MATCH_KEYS:
		return e.execMatchKeys()

	case compile.MATCH_CLASS:
		return e.execMatchClass(oparg)
	}
	return 0, false, nil
}

// execMatchMapping ports MATCH_MAPPING: push True iff the subject has
// the Py_TPFLAGS_MAPPING bit (i.e. dict or dict-subclass). The subject
// stays on the stack.
//
// CPython: Python/bytecodes.c:3062 MATCH_MAPPING
func (e *evalState) execMatchMapping() (next int, handled bool, err error) {
	subject := e.peek(0).AsObject()
	match := subject.Type().TpFlags&objects.TpFlagMapping != 0
	e.pushObject(objects.NewBool(match))
	return e.advance(), true, nil
}

// execMatchSequence ports MATCH_SEQUENCE: push True iff the subject has
// the Py_TPFLAGS_SEQUENCE bit (list, tuple, range, but NOT str/bytes).
// The subject stays on the stack.
//
// CPython: Python/bytecodes.c:3067 MATCH_SEQUENCE
func (e *evalState) execMatchSequence() (next int, handled bool, err error) {
	subject := e.peek(0).AsObject()
	match := subject.Type().TpFlags&objects.TpFlagSequence != 0
	e.pushObject(objects.NewBool(match))
	return e.advance(), true, nil
}

// execMatchKeys ports MATCH_KEYS: given a mapping subject and a keys
// tuple on the stack, extract the values for each key. Pushes a tuple
// of matched values on success, or None on any missing key.
//
// Stack before: [..., subject, keys]
// Stack after:  [..., subject, keys, values_or_None]
//
// CPython: Python/bytecodes.c:3072 MATCH_KEYS
// CPython: Python/ceval.c:L5052 _PyEval_MatchKeys
func (e *evalState) execMatchKeys() (next int, handled bool, err error) {
	keys, ok := e.peek(0).AsObject().(*objects.Tuple)
	if !ok {
		return 0, true, fmt.Errorf("MATCH_KEYS: keys not a tuple")
	}
	subject := e.peek(1).AsObject()

	n := keys.Len()
	if n == 0 {
		e.pushObject(objects.NewTuple(nil))
		return e.advance(), true, nil
	}

	values := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		k := keys.Item(i)
		v, gerr := matchKeysGet(subject, k)
		if errors.Is(gerr, errKeyMissing) {
			e.pushObject(objects.None())
			return e.advance(), true, nil
		}
		if gerr != nil {
			return 0, true, gerr
		}
		values[i] = v
	}
	e.pushObject(objects.NewTuple(values))
	return e.advance(), true, nil
}

var errKeyMissing = fmt.Errorf("key missing")

// matchKeysGet extracts one key from the subject. Returns errKeyMissing
// when the key is absent (not an error, just "no match").
//
// CPython: Python/ceval.c _PyEval_MatchKeys (inner loop)
func matchKeysGet(subject, key objects.Object) (objects.Object, error) {
	if d, ok := subject.(*objects.Dict); ok {
		v, gerr := d.GetItem(key)
		if gerr != nil || v == nil {
			return nil, errKeyMissing
		}
		return v, nil
	}
	// Generic mapping path via __getitem__.
	t := subject.Type()
	if t.Mapping == nil || t.Mapping.GetItem == nil {
		return nil, fmt.Errorf("MATCH_KEYS: subject is not a mapping")
	}
	v, gerr := t.Mapping.GetItem(subject, key)
	if gerr != nil {
		return nil, errKeyMissing
	}
	return v, nil
}

// execMatchClass ports MATCH_CLASS: pop subject, type, names; test
// isinstance(subject, type) and extract positional/keyword attributes.
// Pushes a tuple of extracted values, or None on no match.
//
// oparg is the number of positional patterns.
//
// CPython: Python/bytecodes.c:3043 MATCH_CLASS
// CPython: Python/ceval.c _PyEval_MatchClass
func (e *evalState) execMatchClass(oparg uint32) (next int, handled bool, err error) {
	names := e.popObject()
	typeObj := e.popObject()
	subject := e.popObject()

	namesTup, ok := names.(*objects.Tuple)
	if !ok {
		return 0, true, fmt.Errorf("MATCH_CLASS: names not a tuple")
	}
	tp, ok := typeObj.(*objects.Type)
	if !ok {
		return 0, true, fmt.Errorf("MATCH_CLASS: type operand not a type, got %T", typeObj)
	}

	// isinstance check: subject's type must be tp or a subtype.
	if !isInstance(subject, tp) {
		e.pushObject(objects.None())
		return e.advance(), true, nil
	}

	npos := int(oparg)
	nkw := namesTup.Len()
	attrs := make([]objects.Object, npos+nkw)

	// Positional patterns: look up __match_args__ on the type.
	if npos > 0 {
		matchArgs, agerr := objects.GetAttr(typeObj, objects.NewStr("__match_args__"))
		if agerr != nil {
			return e.matchNoMatch(), true, nil //nolint:nilerr // pattern-match swallows lookup errors as "no match"
		}
		maTup, isTup := matchArgs.(*objects.Tuple)
		if !isTup || maTup.Len() < npos {
			e.pushObject(objects.None())
			return e.advance(), true, nil
		}
		for i := 0; i < npos; i++ {
			attrNameStr, serr := objects.Str(maTup.Item(i))
			if serr != nil {
				return e.matchNoMatch(), true, nil //nolint:nilerr // pattern-match swallows lookup errors as "no match"
			}
			val, verr := objects.GetAttr(subject, objects.NewStr(attrNameStr))
			if verr != nil {
				return e.matchNoMatch(), true, nil //nolint:nilerr // pattern-match swallows lookup errors as "no match"
			}
			attrs[i] = val
		}
	}

	// Keyword patterns: look up each name in namesTup on the subject.
	for i := 0; i < nkw; i++ {
		nameStr, serr := objects.Str(namesTup.Item(i))
		if serr != nil {
			return e.matchNoMatch(), true, nil //nolint:nilerr // pattern-match swallows lookup errors as "no match"
		}
		val, verr := objects.GetAttr(subject, objects.NewStr(nameStr))
		if verr != nil {
			return e.matchNoMatch(), true, nil //nolint:nilerr // pattern-match swallows lookup errors as "no match"
		}
		attrs[npos+i] = val
	}

	e.pushObject(objects.NewTuple(attrs))
	return e.advance(), true, nil
}

// matchNoMatch pushes None on the stack and returns the advanced
// instruction pointer. Used by MATCH_CLASS arms that swallow a lookup
// error and report "no match" rather than propagating it; named so
// the swallow is explicit at the call site.
//
// CPython: Python/ceval.c _PyEval_MatchClass error → None branch
func (e *evalState) matchNoMatch() int {
	e.pushObject(objects.None())
	return e.advance()
}

// isInstance returns true if o is an instance of tp. v0.9 checks
// type identity and traverses the Bases slice one level deep to
// handle simple single-inheritance; the full MRO walk arrives with
// the complete type system port.
//
// CPython: Objects/abstract.c PyObject_IsInstance
func isInstance(o objects.Object, tp *objects.Type) bool {
	got := o.Type()
	if got == tp {
		return true
	}
	for _, base := range got.Bases {
		if base == tp {
			return true
		}
	}
	return false
}
