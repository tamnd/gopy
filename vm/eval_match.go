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
	attrs := make([]objects.Object, 0, npos+nkw)
	seen := make(map[string]struct{}, npos+nkw)

	if npos > 0 {
		posAttrs, noMatch, mcErr := resolvePositionalAttrs(subject, typeObj, tp, npos, seen)
		if mcErr != nil {
			return 0, true, mcErr
		}
		if noMatch {
			return e.matchNoMatch(), true, nil
		}
		attrs = append(attrs, posAttrs...)
	}

	// Keyword patterns: look up each name in namesTup on the subject.
	for i := 0; i < nkw; i++ {
		name, isStr := namesTup.Item(i).(*objects.Unicode)
		if !isStr {
			return 0, true, fmt.Errorf("TypeError: keyword sub-pattern names must be strings (got %s)", namesTup.Item(i).Type().Name)
		}
		val, mcErr := matchClassAttr(subject, tp, name.Value(), seen)
		if mcErr != nil {
			return 0, true, mcErr
		}
		if val == nil {
			return e.matchNoMatch(), true, nil
		}
		attrs = append(attrs, val)
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

// resolvePositionalAttrs runs the __match_args__ + MATCH_SELF gating for
// MATCH_CLASS's positional sub-patterns. Returns the extracted attrs,
// or noMatch=true to make the caller emit the None / "no match" arm,
// or a real err for the TypeError cases (__match_args__ wrong type,
// arity overrun on a self-matching type).
//
// CPython: Python/ceval.c:836 _PyEval_MatchClass positional branch
func resolvePositionalAttrs(subject, typeObj objects.Object, tp *objects.Type, npos int, seen map[string]struct{}) (attrs []objects.Object, noMatch bool, err error) {
	matchArgs, agerr := objects.LookupAttrString(typeObj, "__match_args__")
	if agerr != nil {
		return nil, true, nil //nolint:nilerr // pattern-match swallows lookup errors as "no match"
	}
	matchSelf := false
	var maTup *objects.Tuple
	if matchArgs != nil {
		t, isTup := matchArgs.(*objects.Tuple)
		if !isTup {
			return nil, false, fmt.Errorf("TypeError: %s.__match_args__ must be a tuple (got %s)", tp.Name, matchArgs.Type().Name)
		}
		maTup = t
	} else {
		matchSelf = tp.TpFlags&objects.TpFlagMatchSelf != 0
	}
	allowed := 0
	if matchSelf {
		allowed = 1
	} else if maTup != nil {
		allowed = maTup.Len()
	}
	if allowed < npos {
		plural := "s"
		if allowed == 1 {
			plural = ""
		}
		return nil, false, fmt.Errorf("TypeError: %s() accepts %d positional sub-pattern%s (%d given)", tp.Name, allowed, plural, npos)
	}
	if matchSelf {
		return []objects.Object{subject}, false, nil
	}
	out := make([]objects.Object, 0, npos)
	for i := 0; i < npos; i++ {
		name, isStr := maTup.Item(i).(*objects.Unicode)
		if !isStr {
			return nil, false, fmt.Errorf("TypeError: __match_args__ elements must be strings (got %s)", maTup.Item(i).Type().Name)
		}
		val, mcErr := matchClassAttr(subject, tp, name.Value(), seen)
		if mcErr != nil {
			return nil, false, mcErr
		}
		if val == nil {
			return nil, true, nil
		}
		out = append(out, val)
	}
	return out, false, nil
}

// matchClassAttr fetches subject.<name> while tracking seen attribute
// names. Returns (nil, nil) on AttributeError (no-match signal),
// (val, nil) on success, or (nil, err) for a real TypeError such as a
// duplicate sub-pattern hit on the same attribute.
//
// CPython: Python/ceval.c:824 match_class_attr
func matchClassAttr(subject objects.Object, tp *objects.Type, name string, seen map[string]struct{}) (objects.Object, error) {
	if _, dup := seen[name]; dup {
		return nil, fmt.Errorf("TypeError: %s() got multiple sub-patterns for attribute %q", tp.Name, name)
	}
	seen[name] = struct{}{}
	v, err := objects.LookupAttrString(subject, name)
	if err != nil {
		return nil, nil //nolint:nilerr // pattern-match swallows lookup errors as "no match"
	}
	return v, nil
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
