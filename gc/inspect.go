// Inspection helpers exposed by the gc module: get_objects, freeze
// and friends, get_referrers, get_referents. Each one walks the
// state under state.mu so callers see a consistent snapshot.
//
// CPython: Modules/gcmodule.c gc_get_objects
// CPython: Modules/gcmodule.c gc_get_referrers
// CPython: Modules/gcmodule.c gc_get_referents
// CPython: Python/gc.c _PyGC_Freeze / _PyGC_Unfreeze

package gc

import (
	"github.com/tamnd/gopy/objects"
)

// GetObjects returns every tracked object. When gen is in
// [0, NumGenerations) only that generation is reported; pass -1 for
// "all generations". Mirrors the optional generation argument on
// gc.get_objects.
//
// CPython: Modules/gcmodule.c gc_get_objects_impl
func GetObjects(gen int) []objects.Object {
	state.mu.Lock()
	defer state.mu.Unlock()
	out := make([]objects.Object, 0, len(state.tracked))
	if gen < 0 {
		for i := range state.generations {
			out = appendList(out, state.generations[i].head)
		}
		out = appendList(out, state.permanent)
		return out
	}
	if gen >= NumGenerations {
		return out
	}
	return appendList(out, state.generations[gen].head)
}

// appendList walks list and appends every contained object to dst.
func appendList(dst []objects.Object, list *gcHead) []objects.Object {
	for g := list.next; g != list; g = g.next {
		dst = append(dst, g.obj)
	}
	return dst
}

// Freeze moves every tracked object out of the regular generations
// into the permanent list. Frozen objects are skipped by Collect.
//
// CPython: Python/gc.c:1715 _PyGC_Freeze
func Freeze() {
	state.mu.Lock()
	defer state.mu.Unlock()
	for i := range state.generations {
		listMerge(state.generations[i].head, state.permanent)
		state.generations[i].count = 0
	}
}

// Unfreeze moves every permanent object back to gen 0.
//
// CPython: Python/gc.c:1727 _PyGC_Unfreeze
func Unfreeze() {
	state.mu.Lock()
	defer state.mu.Unlock()
	listMerge(state.permanent, state.generations[0].head)
}

// GetFreezeCount reports how many objects are currently frozen.
//
// CPython: Python/gc.c:1740 _PyGC_GetFreezeCount
func GetFreezeCount() int {
	state.mu.Lock()
	defer state.mu.Unlock()
	return listSize(state.permanent)
}

// GetReferents returns every direct target of any object in args.
// Untracked objects can still be passed in; we walk their tp_traverse
// regardless.
//
// CPython: Modules/gcmodule.c gc_get_referents
func GetReferents(args ...objects.Object) []objects.Object {
	out := make([]objects.Object, 0, len(args))
	for _, src := range args {
		if src == nil {
			continue
		}
		tr := src.Type().TpTraverse
		if tr == nil {
			continue
		}
		_ = tr(src, func(target objects.Object) error {
			if target != nil {
				out = append(out, target)
			}
			return nil
		})
	}
	return out
}

// GetReferrers returns every tracked object whose tp_traverse visits
// at least one of the args.
//
// CPython: Modules/gcmodule.c gc_get_referrers
func GetReferrers(args ...objects.Object) []objects.Object {
	wanted := make(map[objects.Object]struct{}, len(args))
	for _, a := range args {
		if a != nil {
			wanted[a] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	out := make([]objects.Object, 0)
	for src := range state.tracked {
		tr := src.Type().TpTraverse
		if tr == nil {
			continue
		}
		hit := false
		_ = tr(src, func(target objects.Object) error {
			if _, ok := wanted[target]; ok {
				hit = true
			}
			return nil
		})
		if hit {
			out = append(out, src)
		}
	}
	return out
}

// Garbage returns a snapshot of the gc.garbage list. gopy's unified
// finalizer model keeps this empty; the function exists so the gc
// module can publish gc.garbage as a list.
//
// CPython: Modules/gcmodule.c gc.garbage attribute
func Garbage() []objects.Object {
	state.mu.Lock()
	defer state.mu.Unlock()
	out := make([]objects.Object, len(state.garbage))
	copy(out, state.garbage)
	return out
}
