package errors

import (
	"github.com/tamnd/gopy/objects"
)

// BaseExceptionGroup wraps a sequence of leaf exceptions that all
// surfaced from a single try-block. PEP 654 splits it from
// ExceptionGroup so user code can match the BaseException-only form
// (KeyboardInterrupt, SystemExit) without losing the structural
// grouping. ExceptionGroup is the more common Exception-only form
// the BaseExceptionGroup constructor promotes to when every leaf is
// an Exception.
//
// CPython: Objects/exceptions.c:873 BaseExceptionGroup
// CPython: Objects/exceptions.c:874 ExceptionGroup
var (
	PyExc_BaseExceptionGroup = newExcType("BaseExceptionGroup", []*objects.Type{PyExc_BaseException})
	PyExc_ExceptionGroup     = newExcType("ExceptionGroup", []*objects.Type{PyExc_BaseExceptionGroup, PyExc_Exception})
)

// ExceptionGroupInfo carries the structured payload: the message
// string and the tuple of nested exceptions. Subgroup/Split use this
// to project a subset out of a group while preserving ordering.
//
// CPython: Objects/exceptions.c:886 BaseExceptionGroup_new
type ExceptionGroupInfo struct {
	Message    objects.Object
	Exceptions []*Exception
}

// IsExceptionGroup reports whether t inherits from BaseExceptionGroup.
// Used by the VM's CHECK_EG_MATCH to decide whether to recurse.
//
// CPython: Objects/exceptions.c _PyBaseExceptionGroup_Check
func IsExceptionGroup(t *objects.Type) bool {
	if t == nil {
		return false
	}
	return objects.IsSubtype(t, PyExc_BaseExceptionGroup)
}

// ExceptionGroupLeaves returns the leaf-exception list stored at
// args[1] for a BaseExceptionGroup instance. Returns nil when the
// exception is not a group or the slot is malformed; callers treat
// nil as "no leaves" and skip recursion.
//
// CPython: Objects/exceptions.c:886 BaseExceptionGroup_new (excs slot)
func ExceptionGroupLeaves(exc *Exception) []*Exception {
	if exc == nil || exc.Args == nil || exc.Args.Len() < 2 {
		return nil
	}
	raw := exc.Args.Item(1)
	tup, ok := raw.(*objects.Tuple)
	if ok {
		out := make([]*Exception, 0, tup.Len())
		for i := 0; i < tup.Len(); i++ {
			if e, eok := tup.Item(i).(*Exception); eok {
				out = append(out, e)
			}
		}
		return out
	}
	lst, ok := raw.(*objects.List)
	if !ok {
		return nil
	}
	out := make([]*Exception, 0, lst.Len())
	for i := 0; i < lst.Len(); i++ {
		if e, eok := lst.Item(i).(*Exception); eok {
			out = append(out, e)
		}
	}
	return out
}

// SplitExceptionGroup partitions group's leaves by matchType, recursing
// into nested ExceptionGroups. Returns (match, rest) where each is
// either a fresh ExceptionGroup mirroring the matching/non-matching
// subset, or nil when the corresponding partition is empty. The
// derived groups inherit message + group type from the input.
//
// CPython: Objects/exceptions.c:1326 exceptiongroup_split_recursive
// CPython: Objects/exceptions.c:1414 exceptiongroup_subset
func SplitExceptionGroup(group *Exception, matchType *objects.Type) (match, rest *Exception) {
	if group == nil {
		return nil, nil
	}
	// Whole-group match: the EG itself satisfies the predicate.
	if IsSubtype(group.ExcType, matchType) {
		return group, nil
	}
	leaves := ExceptionGroupLeaves(group)
	if leaves == nil {
		return nil, group
	}
	var matched, rested []*Exception
	for _, leaf := range leaves {
		if IsExceptionGroup(leaf.ExcType) {
			subMatch, subRest := SplitExceptionGroup(leaf, matchType)
			if subMatch != nil {
				matched = append(matched, subMatch)
			}
			if subRest != nil {
				rested = append(rested, subRest)
			}
			continue
		}
		if IsSubtype(leaf.ExcType, matchType) {
			matched = append(matched, leaf)
		} else {
			rested = append(rested, leaf)
		}
	}
	if len(matched) > 0 {
		match = deriveGroup(group, matched)
	}
	if len(rested) > 0 {
		rest = deriveGroup(group, rested)
	}
	return match, rest
}

// deriveGroup builds a fresh ExceptionGroup mirroring src's class and
// message, populated with subset. Mirrors CPython's exceptiongroup_subset.
//
// CPython: Objects/exceptions.c:1414 exceptiongroup_subset
func deriveGroup(src *Exception, subset []*Exception) *Exception {
	if src == nil || len(subset) == 0 {
		return nil
	}
	message := objects.NewStr("")
	if src.Args != nil && src.Args.Len() >= 1 {
		message = src.Args.Item(0)
	}
	items := make([]objects.Object, len(subset))
	for i, e := range subset {
		items[i] = e
	}
	leaves := objects.NewTuple(items)
	return New(src.ExcType, objects.NewTuple([]objects.Object{message, leaves}))
}
