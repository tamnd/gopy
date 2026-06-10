package errors

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// ExceptionGroupState carries the BaseExceptionGroup-specific payload that
// CPython keeps in separate struct fields rather than in args. msg and excs
// are independent of args: .message and .exceptions read these slots, while
// .args preserves the original (message, exceptions_object) the constructor
// was called with (exceptions_object may be a list, tuple, or any sequence).
// excs is always a frozen tuple snapshot taken at construction time, so
// mutating the original sequence afterwards does not change .exceptions or
// the repr. excs_str caches repr(exceptions_object) for custom (non
// list/tuple) sequences so the repr stays stable across mutation.
//
// CPython: Objects/exceptions.c:867 PyBaseExceptionGroupObject
type ExceptionGroupState struct {
	Msg     objects.Object
	Excs    *objects.Tuple
	ExcsStr objects.Object
}

// init wires up the Python-visible surface of BaseExceptionGroup and
// ExceptionGroup:
//   - message    (read-only getset → msg)
//   - exceptions (read-only getset → excs tuple)
//   - derive(excs)        → BaseExceptionGroup(self.message, excs)
//   - split(matcher)      → (match, rest)
//   - subgroup(matcher)   → match or None
//   - __new__             → egTpNew (so super().__new__ resolves)
//   - __class_getitem__   → types.GenericAlias
//   - __str__ / __repr__  → egStr / egRepr
//
// egTpNew is wired as tp_new on both group types so the constructor's
// validation and BaseExceptionGroup→ExceptionGroup promotion run regardless
// of which class is called, and user subclasses inherit it through Bases[0].
//
// CPython: Objects/exceptions.c:885  BaseExceptionGroup_new
// CPython: Objects/exceptions.c:1709 BaseExceptionGroup_getset
// CPython: Objects/exceptions.c:1718 BaseExceptionGroup_methods
func init() {
	for _, t := range []*objects.Type{PyExc_BaseExceptionGroup, PyExc_ExceptionGroup} {
		t.TpNew = egTpNew
		t.Str = egStr
		t.Repr = egRepr
		objects.SetTypeDescr(t, "message", objects.NewGetSetDescr("message", egMessageGet, nil))
		objects.SetTypeDescr(t, "exceptions", objects.NewGetSetDescr("exceptions", egExceptionsGet, nil))
		objects.SetTypeDescr(t, "derive", objects.NewMethodDescr(t, "derive", egDerive))
		objects.SetTypeDescr(t, "split", objects.NewMethodDescr(t, "split", egSplit))
		objects.SetTypeDescr(t, "subgroup", objects.NewMethodDescr(t, "subgroup", egSubgroup))
		// Re-register __str__ / __repr__ so they wrap the group slots
		// rather than the BaseException ones newExcType installed.
		objects.SetTypeDescr(t, "__str__", objects.NewMethodDescr(t, "__str__", egStrDescr))
		objects.SetTypeDescr(t, "__repr__", objects.NewMethodDescr(t, "__repr__", egReprDescr))
		// __new__ routes super().__new__(cls, msg, excs) back to egTpNew.
		// Stored as a plain builtin (not wrapped in staticmethod) the same
		// way type.__new__ is, so the MRO lookup hands the function back
		// unbound and slotTpNew can prepend cls.
		//
		// CPython: Objects/exceptions.c:1730 BaseExceptionGroup_Type (tp_new)
		objects.SetTypeDescr(t, "__new__", objects.NewBuiltinFunction(t.Name+".__new__", egNewBuiltin))
		bindGroupClassGetitem(t)
	}
}

// bindGroupClassGetitem installs a __class_getitem__ classmethod so
// ExceptionGroup[E] and BaseExceptionGroup[E] produce a types.GenericAlias.
// CPython exposes this through the METH_O|METH_CLASS row in
// BaseExceptionGroup_methods.
//
// CPython: Objects/exceptions.c:1718 BaseExceptionGroup_methods (__class_getitem__)
func bindGroupClassGetitem(t *objects.Type) {
	fn := objects.NewBuiltinFunction("__class_getitem__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: __class_getitem__() takes exactly one argument (%d given)", len(args)-1)
		}
		cls, ok := args[0].(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: __class_getitem__() requires a type, got %s", args[0].Type().Name)
		}
		return objects.NewGenericAlias(cls, args[1]), nil
	})
	objects.SetTypeDescr(t, "__class_getitem__", objects.NewClassMethod(fn))
}

// egStateOf returns the BaseExceptionGroup payload for e. When e was built
// through egTpNew the cached EG state is returned directly. Groups created
// through New (the VM's CHECK_EG_MATCH / except* paths) carry no EG state, so
// synthesize the view from args: msg from args[0], excs coerced to a tuple
// from args[1].
//
// CPython: Objects/exceptions.c:867 PyBaseExceptionGroupObject
func egStateOf(e *Exception) *ExceptionGroupState {
	if e.EG != nil {
		return e.EG
	}
	st := &ExceptionGroupState{Msg: objects.NewStr(""), Excs: objects.NewTuple(nil)}
	if e.Args != nil {
		if e.Args.Len() >= 1 {
			st.Msg = e.Args.Item(0)
		}
		if e.Args.Len() >= 2 {
			if tup, ok := e.Args.Item(1).(*objects.Tuple); ok {
				st.Excs = tup
			} else if tup, err := objects.SequenceTuple(e.Args.Item(1)); err == nil {
				st.Excs = tup
			}
		}
	}
	return st
}

// egTpNew is the tp_new slot for BaseExceptionGroup / ExceptionGroup. It
// validates the (message, exceptions) arguments, freezes the exceptions
// sequence into a tuple, and applies PEP 654 type promotion: a plain
// BaseExceptionGroup wrapping only Exception subclasses becomes an
// ExceptionGroup, an ExceptionGroup may not wrap BaseExceptions, and a user
// subclass that is also an Exception subclass may not wrap BaseExceptions.
//
// CPython: Objects/exceptions.c:885 BaseExceptionGroup_new
func egTpNew(cls *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: BaseExceptionGroup.__new__() takes exactly 2 arguments (%d given)", len(args))
	}
	message := args[0]
	exceptions := args[1]

	if !objects.IsSubtype(message.Type(), objects.StrType()) {
		return nil, fmt.Errorf("TypeError: argument 1 must be str, not %s", message.Type().Name)
	}

	if !objects.SequenceCheck(exceptions) {
		return nil, fmt.Errorf("TypeError: second argument (exceptions) must be a sequence")
	}

	// Save the initial exceptions sequence as a string for custom (non
	// list/tuple) sequences, in case the sequence is later mutated.
	//
	// CPython: Objects/exceptions.c:908 PyObject_Repr(exceptions)
	var excsStr objects.Object
	_, isList := exceptions.(*objects.List)
	_, isTuple := exceptions.(*objects.Tuple)
	if !isList && !isTuple {
		s, err := objects.ReprObject(exceptions)
		if err != nil {
			return nil, err
		}
		excsStr = s
	}

	excsTuple, err := objects.SequenceTuple(exceptions)
	if err != nil {
		return nil, err
	}
	numExcs := excsTuple.Len()
	if numExcs == 0 {
		return nil, fmt.Errorf("ValueError: second argument (exceptions) must be a non-empty sequence")
	}

	nestedBaseExceptions := false
	for i := 0; i < numExcs; i++ {
		item, ok := excsTuple.Item(i).(*Exception)
		if !ok {
			return nil, fmt.Errorf("ValueError: Item %d of second argument (exceptions) is not an exception", i)
		}
		if !objects.IsSubtype(item.ExcType, PyExc_Exception) {
			nestedBaseExceptions = true
		}
	}

	actualCls := cls
	switch cls {
	case PyExc_ExceptionGroup:
		if nestedBaseExceptions {
			return nil, fmt.Errorf("TypeError: Cannot nest BaseExceptions in an ExceptionGroup")
		}
	case PyExc_BaseExceptionGroup:
		if !nestedBaseExceptions {
			// All nested exceptions are Exception subclasses: promote.
			actualCls = PyExc_ExceptionGroup
		}
	default:
		// User-defined subclass.
		if nestedBaseExceptions && objects.IsSubtype(cls, PyExc_Exception) {
			return nil, fmt.Errorf("TypeError: Cannot nest BaseExceptions in '%s'", cls.Name)
		}
	}

	// BaseException_new stores the original args verbatim, so .args keeps
	// the message and the original exceptions object (which may be a list
	// the caller goes on to mutate).
	self := New(actualCls, objects.NewTuple(args))
	self.EG = &ExceptionGroupState{Msg: message, Excs: excsTuple, ExcsStr: excsStr}
	return self, nil
}

// egNewBuiltin backs the __new__ descriptor on the group types. The MRO
// lookup hands it cls as the first positional argument (CPython's
// staticmethod-wrapped tp_new), so peel that off and forward to egTpNew.
//
// CPython: Objects/exceptions.c:885 BaseExceptionGroup_new
func egNewBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: BaseExceptionGroup.__new__(): not enough arguments")
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: BaseExceptionGroup.__new__(X): X is not a type object (%s)", args[0].Type().Name)
	}
	return egTpNew(cls, args[1:], kwargs)
}

// egMessageGet returns the group's message slot.
//
// CPython: Objects/exceptions.c:1709 BaseExceptionGroup_getset (message)
func egMessageGet(owner objects.Object) (objects.Object, error) {
	return egStateOf(owner.(*Exception)).Msg, nil
}

// egExceptionsGet returns the group's frozen exceptions tuple.
//
// CPython: Objects/exceptions.c:1709 BaseExceptionGroup_getset (exceptions)
func egExceptionsGet(owner objects.Object) (objects.Object, error) {
	return egStateOf(owner.(*Exception)).Excs, nil
}

// egStr ports BaseExceptionGroup_str: "<msg> (N sub-exception[s])".
//
// CPython: Objects/exceptions.c:1071 BaseExceptionGroup_str
func egStr(o objects.Object) (string, error) {
	st := egStateOf(o.(*Exception))
	msg, err := objects.Str(st.Msg)
	if err != nil {
		return "", err
	}
	num := st.Excs.Len()
	suffix := ""
	if num > 1 {
		suffix = "s"
	}
	return fmt.Sprintf("%s (%d sub-exception%s)", msg, num, suffix), nil
}

// egRepr ports BaseExceptionGroup_repr: "Name(repr(msg), exceptions_str)".
// The exceptions string is the cached repr for custom sequences, the repr of
// list(excs) when args[1] was a list, or the repr of the excs tuple otherwise.
//
// CPython: Objects/exceptions.c:1085 BaseExceptionGroup_repr
func egRepr(o objects.Object) (string, error) {
	e := o.(*Exception)
	st := egStateOf(e)

	var exceptionsStr string
	switch {
	case st.ExcsStr != nil:
		s, err := objects.Str(st.ExcsStr)
		if err != nil {
			return "", err
		}
		exceptionsStr = s
	case e.Args != nil && e.Args.Len() >= 2 && isListObject(e.Args.Item(1)):
		// Render as list(excs) so the repr looks like args[1] for
		// backwards compatibility, while staying in sync with .exceptions.
		lst, err := objects.SequenceList(st.Excs)
		if err != nil {
			return "", err
		}
		s, err := objects.Repr(lst)
		if err != nil {
			return "", err
		}
		exceptionsStr = s
	default:
		s, err := objects.Repr(st.Excs)
		if err != nil {
			return "", err
		}
		exceptionsStr = s
	}

	msgRepr, err := objects.Repr(st.Msg)
	if err != nil {
		return "", err
	}
	return e.TypeName() + "(" + msgRepr + ", " + exceptionsStr + ")", nil
}

func isListObject(o objects.Object) bool {
	_, ok := o.(*objects.List)
	return ok
}

// egStrDescr / egReprDescr adapt egStr / egRepr to the __str__ / __repr__
// method-descriptor calling convention.
func egStrDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __str__ expected 1 argument")
	}
	s, err := egStr(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

func egReprDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __repr__ expected 1 argument")
	}
	s, err := egRepr(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s), nil
}

// egDerive implements BaseExceptionGroup.derive(excs): construct a fresh
// group with self's message and the given excs sequence. It always calls the
// BaseExceptionGroup constructor (not self's type), so the promotion rules
// apply and subset() relies on subclasses overriding derive() to preserve
// extra state.
//
// CPython: Objects/exceptions.c:1124 BaseExceptionGroup_derive_impl
func egDerive(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: derive() takes exactly one argument (%d given)", len(args)-1)
	}
	self, ok := args[0].(*Exception)
	if !ok {
		return nil, fmt.Errorf("TypeError: derive() requires a BaseExceptionGroup")
	}
	msg := egStateOf(self).Msg
	initArgs := objects.NewTuple([]objects.Object{msg, args[1]})
	return objects.Call(PyExc_BaseExceptionGroup, initArgs, nil)
}

// egSplit implements BaseExceptionGroup.split(matcher) → (match, rest).
//
// CPython: Objects/exceptions.c:1444 BaseExceptionGroup_split_impl
func egSplit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: split() takes exactly one argument (%d given)", len(args)-1)
	}
	self, ok := args[0].(*Exception)
	if !ok {
		return nil, fmt.Errorf("TypeError: split() requires a BaseExceptionGroup")
	}
	matchFn, err := egMakeMatcher(args[1])
	if err != nil {
		return nil, err
	}
	match, rest, err := egSplitRecursive(self, matchFn, true)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{orNone(match), orNone(rest)}), nil
}

// egSubgroup implements BaseExceptionGroup.subgroup(matcher) → match or None.
//
// CPython: Objects/exceptions.c:1479 BaseExceptionGroup_subgroup_impl
func egSubgroup(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: subgroup() takes exactly one argument (%d given)", len(args)-1)
	}
	self, ok := args[0].(*Exception)
	if !ok {
		return nil, fmt.Errorf("TypeError: subgroup() requires a BaseExceptionGroup")
	}
	matchFn, err := egMakeMatcher(args[1])
	if err != nil {
		return nil, err
	}
	match, _, err := egSplitRecursive(self, matchFn, false)
	if err != nil {
		return nil, err
	}
	return orNone(match), nil
}

func orNone(e *Exception) objects.Object {
	if e == nil {
		return objects.None()
	}
	return e
}

// egMatcher tests whether a single exception (leaf or group node) matches.
type egMatcher func(*Exception) (bool, error)

// egMakeMatcher ports get_matcher_type: a non-type callable becomes a
// predicate; an exception class or an exact tuple of exception classes
// matches by type; anything else is a TypeError.
//
// CPython: Objects/exceptions.c:1250 get_matcher_type
func egMakeMatcher(value objects.Object) (egMatcher, error) {
	_, isType := value.(*objects.Type)
	if objects.Callable(value) && !isType {
		return func(exc *Exception) (bool, error) {
			result, err := objects.CallOneArg(value, exc)
			if err != nil {
				return false, err
			}
			return objects.IsTrue(result), nil
		}, nil
	}

	if t, ok := value.(*objects.Type); ok {
		if !objects.IsSubtype(t, PyExc_BaseException) {
			return nil, egMatcherTypeError()
		}
		return matchByTypes([]*objects.Type{t}), nil
	}

	if tup, ok := value.(*objects.Tuple); ok {
		types := make([]*objects.Type, 0, tup.Len())
		for i := 0; i < tup.Len(); i++ {
			t, ok := tup.Item(i).(*objects.Type)
			if !ok || !objects.IsSubtype(t, PyExc_BaseException) {
				return nil, egMatcherTypeError()
			}
			types = append(types, t)
		}
		return matchByTypes(types), nil
	}

	return nil, egMatcherTypeError()
}

func egMatcherTypeError() error {
	return fmt.Errorf("TypeError: expected an exception type, a tuple of exception types, or a callable (other than a class)")
}

func matchByTypes(types []*objects.Type) egMatcher {
	return func(exc *Exception) (bool, error) {
		for _, t := range types {
			if objects.IsSubtype(exc.ExcType, t) {
				return true, nil
			}
		}
		return false, nil
	}
}

// egSplitRecursive partitions exc by matchFn. It first tests the match on the
// node itself: a matching node (leaf or group) is returned by identity, so
// eg.subgroup(BaseException) returns eg unchanged. A non-matching leaf goes to
// rest. A non-matching group recurses into its excs, then rebuilds the match
// and rest subtrees through exceptiongroup_subset (which calls derive()).
//
// CPython: Objects/exceptions.c:1325 exceptiongroup_split_recursive
func egSplitRecursive(exc *Exception, matchFn egMatcher, constructRest bool) (match, rest *Exception, err error) {
	isMatch, err := matchFn(exc)
	if err != nil {
		return nil, nil, err
	}
	if isMatch {
		// Full match: identity passthrough.
		return exc, nil, nil
	}
	if !IsExceptionGroup(exc.ExcType) {
		// Leaf exception and no match.
		if constructRest {
			return nil, exc, nil
		}
		return nil, nil, nil
	}

	// Partial match: recurse into the nested exceptions.
	excs := egStateOf(exc).Excs
	var matchList, restList []*Exception
	for i := 0; i < excs.Len(); i++ {
		child, ok := excs.Item(i).(*Exception)
		if !ok {
			continue
		}
		if rerr := objects.EnterRecursiveCall(" in exceptiongroup_split_recursive"); rerr != nil {
			return nil, nil, rerr
		}
		subMatch, subRest, rerr := egSplitRecursive(child, matchFn, constructRest)
		objects.LeaveRecursiveCall()
		if rerr != nil {
			return nil, nil, rerr
		}
		if subMatch != nil {
			matchList = append(matchList, subMatch)
		}
		if subRest != nil {
			restList = append(restList, subRest)
		}
	}

	match, err = egSubset(exc, matchList)
	if err != nil {
		return nil, nil, err
	}
	if constructRest {
		rest, err = egSubset(exc, restList)
		if err != nil {
			return nil, nil, err
		}
	}
	return match, rest, nil
}

// egSubset builds a group wrapping excs with metadata copied from orig. It
// calls orig.derive(excs) through attribute lookup so subclass overrides run,
// verifies the result is a BaseExceptionGroup, then copies the traceback,
// context, cause, and (shallow-copied) notes. An empty excs yields nil.
//
// CPython: Objects/exceptions.c:1149 exceptiongroup_subset
func egSubset(orig *Exception, excs []*Exception) (*Exception, error) {
	if len(excs) == 0 {
		return nil, nil
	}
	items := make([]objects.Object, len(excs))
	for i, e := range excs {
		items[i] = e
	}
	deriveFn, err := objects.GetAttr(orig, objects.NewStr("derive"))
	if err != nil {
		return nil, err
	}
	derived, err := objects.Call(deriveFn, objects.NewTuple([]objects.Object{objects.NewList(items)}), nil)
	if err != nil {
		return nil, err
	}
	eg, ok := derived.(*Exception)
	if !ok || !IsExceptionGroup(eg.ExcType) {
		return nil, fmt.Errorf("TypeError: derive must return an instance of BaseExceptionGroup")
	}

	// Copy the metadata that split() preserves across the partition.
	//
	// CPython: Objects/exceptions.c:1180 PyException_SetTraceback / SetContext / SetCause
	eg.TB = orig.TB
	eg.Context = orig.Context
	eg.ContextSet = orig.ContextSet
	eg.Cause = orig.Cause
	// PyException_SetCause unconditionally marks the context suppressed.
	eg.Suppress = true

	// __notes__ is shallow-copied so the parts get independent lists; a
	// non-sequence __notes__ is ignored (split is not the place to report
	// an earlier user error).
	//
	// CPython: Objects/exceptions.c:1199 exceptiongroup_subset (__notes__)
	notes, err := notesGet(orig)
	if err == nil && notes != nil {
		if objects.SequenceCheck(notes) {
			notesCopy, lerr := objects.SequenceList(notes)
			if lerr != nil {
				return nil, lerr
			}
			eg.Notes = notesCopy
			eg.NotesObj = nil
		}
	}
	return eg, nil
}
