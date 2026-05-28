// Package abcmod is the gopy port of CPython's _abc builtin module
// (Modules/_abc.c). It backs Lib/abc.py with the C-speed paths for
// ABC registration and subclass / instance checks. The 1:1 port keeps
// the module-level invalidation counter, the per-class _abc_data
// carrier, and the six step subclass check algorithm.
//
// CPython: Modules/_abc.c:1 _abc module
package abcmod

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_abc", buildModule)
}

// abcInvalidationCounter is the module-level counter bumped on every
// _abc_register call. It feeds get_cache_token and is compared against
// each _abc_data.negativeCacheVersion to decide when a cached negative
// answer has gone stale.
//
// CPython: Modules/_abc.c:24 abc_invalidation_counter
var abcInvalidationCounter atomic.Uint64

// abcData is the per-ABC state carrier. CPython exposes this as the
// _abc_data type stored under cls._abc_impl. The four fields mirror
// the C struct one-for-one. The lazy creation of the three weak sets
// matches Modules/_abc.c:60.
//
// CPython: Modules/_abc.c:58 _abc_data
type abcData struct {
	objects.Header

	mu sync.Mutex

	registry             *objects.Set
	cache                *objects.Set
	negativeCache        *objects.Set
	negativeCacheVersion uint64
}

// abcDataType is the type singleton for _abc_data.
//
// CPython: Modules/_abc.c:157 _abc_data_type_spec
var abcDataType = objects.NewType("_abc_data", []*objects.Type{objects.ObjectType()})

func init() {
	abcDataType.Repr = func(_ objects.Object) (string, error) { return "<_abc_data object>", nil }
	abcDataType.Str = abcDataType.Repr
}

// newAbcData builds a fresh carrier. Mirrors abc_data_new.
//
// CPython: Modules/_abc.c:123 abc_data_new
func newAbcData() *abcData {
	d := &abcData{negativeCacheVersion: abcInvalidationCounter.Load()}
	d.Init(abcDataType)
	return d
}

// getImpl reads cls._abc_impl and validates its type.
//
// CPython: Modules/_abc.c:164 _get_impl
func getImpl(self objects.Object) (*abcData, error) {
	v, err := objects.GetAttr(self, objects.NewStr("_abc_impl"))
	if err != nil {
		return nil, err
	}
	d, ok := v.(*abcData)
	if !ok {
		return nil, fmt.Errorf("TypeError: _abc_impl is set to a wrong type")
	}
	return d, nil
}

// inWeakSet reports whether obj appears in *pset. CPython wraps obj in
// a weakref because the underlying storage is a set of weak references;
// our set stores plain object pointers so a direct Contains call is
// equivalent. Lock so a concurrent _add_to_weak_set cannot grow the set
// out from under the read.
//
// CPython: Modules/_abc.c:180 _in_weak_set
func inWeakSet(d *abcData, set *objects.Set, obj objects.Object) (bool, error) {
	_ = d // signature mirrors _in_weak_set; the lock argument plugs in once we add concurrent prune
	if set == nil || set.Len() == 0 {
		return false, nil
	}
	ref, err := objects.PyWeakref_NewRef(obj, nil)
	if err != nil {
		// CPython swallows TypeError (unweakreferenceable types).
		return false, nil //nolint:nilerr // matches CPython's PyErr_ExceptionMatches(TypeError) path
	}
	return set.Contains(ref)
}

// addToWeakSet inserts obj into *pset, lazily creating the set. The
// stored entry is a weak reference whose destroy callback discards it
// when the underlying object dies; that is how dead virtual subclasses
// drop out of the registry without an explicit prune.
//
// CPython: Modules/_abc.c:222 _add_to_weak_set
func addToWeakSet(d *abcData, pset **objects.Set, obj objects.Object) error {
	d.mu.Lock()
	if *pset == nil {
		*pset = objects.NewSet()
	}
	set := *pset
	d.mu.Unlock()

	// The destroy callback discards the entry when obj dies. CPython
	// wraps the set itself in a weakref so the callback does not keep
	// the set alive; we close over set directly and accept the GC
	// pinning since user-defined ABCs typically outlive their members.
	destroy := objects.NewBuiltinFunction("_destroy", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return objects.None(), nil
		}
		_ = set.Discard(args[0])
		return objects.None(), nil
	})
	ref, err := objects.PyWeakref_NewRef(obj, destroy)
	if err != nil {
		return err
	}
	return set.Add(ref)
}

// clearSet empties s in place. gopy's Set has no Clear method; we
// iterate Items() and Discard each. CPython calls PySet_Clear.
func clearSet(s *objects.Set) error {
	if s == nil {
		return nil
	}
	for _, k := range s.Items() {
		if err := s.Discard(k); err != nil {
			return err
		}
	}
	return nil
}

// isAbstract mirrors _PyObject_IsAbstract: read __isabstractmethod__ and
// return its truthiness, treating absence as false.
//
// CPython: Include/internal/pycore_object.h _PyObject_IsAbstract
//
//nolint:unparam // signature mirrors _PyObject_IsAbstract; future hash/error returns plug in here
func isAbstract(value objects.Object) (bool, error) {
	v, err := objects.GetAttr(value, objects.NewStr("__isabstractmethod__"))
	if err != nil {
		// AttributeError: not abstract.
		return false, nil //nolint:nilerr // matches CPython's swallow of AttributeError
	}
	return objects.IsTrue(v), nil
}

// computeAbstractMethods walks cls.__dict__ then base classes to build
// the frozenset of abstract method names and stores it as
// __abstractmethods__. Mirrors compute_abstract_methods.
//
// CPython: Modules/_abc.c:361 compute_abstract_methods
func computeAbstractMethods(self objects.Object) error {
	cls, ok := self.(*objects.Type)
	if !ok {
		return fmt.Errorf("TypeError: _abc_init expects a class")
	}

	names := map[string]bool{}

	// Stage 1: direct abstract methods. CPython iterates
	// PyMapping_Items(ns); gopy reaches the same entries through
	// TypeOwnDescrs which exposes the class's own descriptor table.
	for name, value := range objects.TypeOwnDescrs(cls) {
		abs, err := isAbstract(value)
		if err != nil {
			return err
		}
		if abs {
			names[name] = true
		}
	}

	// Stage 2: inherited abstract methods. For each base, pull its
	// __abstractmethods__ and re-check that the live attribute on self
	// is still abstract (an override in self may have de-abstracted it).
	for _, base := range cls.Bases {
		baseAbs, err := objects.GetAttr(base, objects.NewStr("__abstractmethods__"))
		if err != nil {
			continue
		}
		set, ok := baseAbs.(*objects.Set)
		if !ok {
			continue
		}
		for _, key := range set.Items() {
			s, ok := key.(*objects.Unicode)
			if !ok {
				continue
			}
			value, err := objects.GetAttr(self, objects.NewStr(s.Value()))
			if err != nil {
				continue
			}
			abs, err := isAbstract(value)
			if err != nil {
				return err
			}
			if abs {
				names[s.Value()] = true
			}
		}
	}

	items := make([]objects.Object, 0, len(names))
	for n := range names {
		items = append(items, objects.NewStr(n))
	}
	fs, err := objects.NewFrozenset(items)
	if err != nil {
		return err
	}
	objects.SetTypeDescr(cls, "__abstractmethods__", fs)
	return nil
}

// collectionFlags is the mask of TpFlags bits abc propagates through
// __abc_tpflags__ and _abc_register. Mirrors COLLECTION_FLAGS.
//
// CPython: Modules/_abc.c:484 COLLECTION_FLAGS
const collectionFlags = objects.TpFlagSequence | objects.TpFlagMapping

// abcInit installs __abstractmethods__ and a fresh _abc_data, then
// honors __abc_tpflags__ for sequence / mapping marking.
//
// CPython: Modules/_abc.c:495 _abc__abc_init
func abcInit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _abc_init() takes exactly 1 argument (%d given)", len(args))
	}
	self := args[0]
	if err := computeAbstractMethods(self); err != nil {
		return nil, err
	}
	data := newAbcData()
	cls, isType := self.(*objects.Type)
	if isType {
		objects.SetTypeDescr(cls, "_abc_impl", data)
	} else {
		if err := objects.SetAttr(self, objects.NewStr("_abc_impl"), data); err != nil {
			return nil, err
		}
	}

	if !isType {
		return objects.None(), nil
	}

	// __abc_tpflags__ piggybacks the sequence/mapping bits onto the
	// new class. CPython reads it out of the class dict with
	// PyDict_Pop; gopy reads the descriptor table and removes the
	// entry on success.
	own := objects.TypeOwnDescrs(cls)
	flagsObj, has := own["__abc_tpflags__"]
	if !has {
		return objects.None(), nil
	}
	objects.DelTypeDescr(cls, "__abc_tpflags__")
	flagsInt, ok := flagsObj.(*objects.Int)
	if !ok {
		return objects.None(), nil
	}
	val, _ := flagsInt.Int64()
	bits := uint64(val) & collectionFlags
	if bits == collectionFlags {
		return nil, fmt.Errorf("TypeError: __abc_tpflags__ cannot be both Py_TPFLAGS_SEQUENCE and Py_TPFLAGS_MAPPING")
	}
	cls.TpFlags |= bits
	return objects.None(), nil
}

// abcRegister registers subclass as a virtual subclass of self.
//
// CPython: Modules/_abc.c:555 _abc__abc_register_impl
func abcRegister(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _abc_register() takes exactly 2 arguments (%d given)", len(args))
	}
	self, subclass := args[0], args[1]
	subCls, ok := subclass.(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: Can only register classes")
	}
	selfCls, ok := self.(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: self must be a class")
	}
	if objects.IsSubtype(subCls, selfCls) {
		// Already a subclass: no-op, return subclass.
		return subclass, nil
	}
	if objects.IsSubtype(selfCls, subCls) {
		return nil, fmt.Errorf("RuntimeError: Refusing to create an inheritance cycle")
	}
	d, err := getImpl(self)
	if err != nil {
		return nil, err
	}
	if err := addToWeakSet(d, &d.registry, subclass); err != nil {
		return nil, err
	}
	abcInvalidationCounter.Add(1)

	// Propagate sequence / mapping flags to the registered subclass.
	if collFlag := selfCls.TpFlags & collectionFlags; collFlag != 0 {
		objects.SetFlagsRecursive(subCls, collectionFlags, collFlag)
	}
	return subclass, nil
}

// abcInstanceCheck inlines the cache hit for instance.__class__ ==
// type(instance) and falls back to __subclasscheck__.
//
// CPython: Modules/_abc.c:618 _abc__abc_instancecheck_impl
func abcInstanceCheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _abc_instancecheck() takes exactly 2 arguments (%d given)", len(args))
	}
	self, instance := args[0], args[1]
	d, err := getImpl(self)
	if err != nil {
		return nil, err
	}
	subclass, err := objects.GetAttr(instance, objects.NewStr("__class__"))
	if err != nil {
		return nil, err
	}

	// Inline cache check.
	hit, err := inWeakSet(d, d.cache, subclass)
	if err != nil {
		return nil, err
	}
	if hit {
		return objects.True(), nil
	}

	subtype := instance.Type()
	if subtype == subclass {
		// Same class: also try the negative cache before recursing.
		if d.negativeCacheVersion == abcInvalidationCounter.Load() {
			neg, err := inWeakSet(d, d.negativeCache, subclass)
			if err != nil {
				return nil, err
			}
			if neg {
				return objects.False(), nil
			}
		}
		return callSubclasscheck(self, subclass)
	}

	result, err := callSubclasscheck(self, subclass)
	if err != nil {
		return nil, err
	}
	if objects.IsTrue(result) {
		return result, nil
	}
	// Try the real type as well: an instance whose __class__ is lying
	// (e.g. a proxy) might still be a genuine subclass.
	return callSubclasscheck(self, subtype)
}

// callSubclasscheck invokes self.__subclasscheck__(arg). CPython spells
// this PyObject_CallMethodOneArg; the gopy equivalent walks the type's
// descriptor table, runs the descriptor protocol, then calls.
func callSubclasscheck(self objects.Object, arg objects.Object) (objects.Object, error) {
	descr, owner := objects.LookupDescriptor(self.Type(), "__subclasscheck__")
	if descr == nil {
		// Fall back to default subtype check.
		if a, ok := self.(*objects.Type); ok {
			if b, ok := arg.(*objects.Type); ok {
				return objects.NewBool(objects.IsSubtype(b, a)), nil
			}
		}
		return objects.False(), nil
	}
	bound, err := bindDescriptor(descr, self, owner)
	if err != nil {
		return nil, err
	}
	return objects.CallOneArg(bound, arg)
}

// bindDescriptor runs the descriptor protocol if available; otherwise
// returns the raw descriptor (which must already be callable).
func bindDescriptor(descr, owner objects.Object, ownerType *objects.Type) (objects.Object, error) {
	dt := descr.Type()
	if dt.DescrGet != nil {
		return dt.DescrGet(descr, owner, ownerType)
	}
	return descr, nil
}

// abcSubclasscheck runs the six step virtual subclass check.
//
// CPython: Modules/_abc.c:704 _abc__abc_subclasscheck_impl
//
//nolint:gocognit,gocyclo // mirrors CPython's monolithic body
func abcSubclasscheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _abc_subclasscheck() takes exactly 2 arguments (%d given)", len(args))
	}
	self, subclass := args[0], args[1]
	subCls, ok := subclass.(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: issubclass() arg 1 must be a class")
	}
	d, err := getImpl(self)
	if err != nil {
		return nil, err
	}

	// Step 1: cache.
	hit, err := inWeakSet(d, d.cache, subclass)
	if err != nil {
		return nil, err
	}
	if hit {
		return objects.True(), nil
	}

	// Step 2: negative cache, with version-based invalidation.
	current := abcInvalidationCounter.Load()
	if d.negativeCacheVersion < current {
		if err := clearSet(d.negativeCache); err != nil {
			return nil, err
		}
		d.negativeCacheVersion = current
	} else {
		neg, err := inWeakSet(d, d.negativeCache, subclass)
		if err != nil {
			return nil, err
		}
		if neg {
			return objects.False(), nil
		}
	}

	// Step 3: __subclasshook__.
	ok2, err := callSubclasshook(self, subclass)
	if err != nil {
		return nil, err
	}
	if ok2 == objects.True() {
		if err := addToWeakSet(d, &d.cache, subclass); err != nil {
			return nil, err
		}
		return objects.True(), nil
	}
	if ok2 == objects.False() {
		if err := addToWeakSet(d, &d.negativeCache, subclass); err != nil {
			return nil, err
		}
		return objects.False(), nil
	}
	if ok2 != objects.NotImplemented() {
		return nil, fmt.Errorf("AssertionError: __subclasshook__ must return either False, True, or NotImplemented")
	}

	// Step 4: real subtype.
	if selfCls, ok := self.(*objects.Type); ok {
		if objects.IsSubtype(subCls, selfCls) {
			if err := addToWeakSet(d, &d.cache, subclass); err != nil {
				return nil, err
			}
			return objects.True(), nil
		}
	}

	// Step 5: registered subclasses (recursive).
	r, found, err := checkRegistry(d, subclass)
	if err != nil {
		return nil, err
	}
	if found {
		return r, nil
	}

	// Step 6: walk self.__subclasses__() recursively. Each direct
	// subclass might have registered subclass as a virtual subclass, so
	// recursing through _abc_subclasscheck honors those registrations.
	// The default fallback for non-ABC subclasses is a real subtype check.
	if selfCls, ok := self.(*objects.Type); ok {
		for _, sub := range selfCls.Subclasses() {
			var res objects.Object
			if impl, _ := objects.LookupDescriptor(sub, "_abc_impl"); impl != nil {
				r2, err := abcSubclasscheck([]objects.Object{sub, subclass}, nil)
				if err != nil {
					return nil, err
				}
				res = r2
			} else {
				res = objects.NewBool(objects.IsSubtype(subCls, sub))
			}
			if objects.IsTrue(res) {
				if err := addToWeakSet(d, &d.cache, subclass); err != nil {
					return nil, err
				}
				return objects.True(), nil
			}
		}
	}

	// Negative cache update.
	if err := addToWeakSet(d, &d.negativeCache, subclass); err != nil {
		return nil, err
	}
	return objects.False(), nil
}

// callSubclasshook fires cls.__subclasshook__(subclass). Returns
// NotImplemented when the class did not define one (the default in
// abc.ABC).
//
// CPython: Modules/_abc.c:589 _call_subclasshook uses
// _PyObject_GetAttrWithoutError(cls, "__subclasshook__") which resolves
// the classmethod descriptor via cls's MRO, not the metaclass's MRO.
func callSubclasshook(self, subclass objects.Object) (objects.Object, error) {
	checker, err := objects.GetAttr(self, objects.NewStr("__subclasshook__"))
	if err != nil || checker == nil {
		return objects.NotImplemented(), nil //nolint:nilerr // AttributeError → NotImplemented
	}
	result, err := objects.CallOneArg(checker, subclass)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// checkRegistry implements the fast path plus the recursive walk over
// every still-live registered class. Returns (result, found, err) where
// found==true means *result* should be returned to the caller.
//
// CPython: Modules/_abc.c:844 subclasscheck_check_registry
func checkRegistry(d *abcData, subclass objects.Object) (objects.Object, bool, error) {
	hit, err := inWeakSet(d, d.registry, subclass)
	if err != nil {
		return nil, false, err
	}
	if hit {
		return objects.True(), true, nil
	}
	if d.registry == nil {
		return nil, false, nil
	}
	// Snapshot the registry so concurrent mutation does not bite us
	// during the recursion.
	snapshot := d.registry.Items()
	for _, entry := range snapshot {
		w, ok := entry.(*objects.Weakref)
		if !ok {
			continue
		}
		rkey, alive := objects.PyWeakref_GetRef(w)
		if !alive {
			continue
		}
		// Recurse via subclasscheck so registry-of-registry chains
		// resolve. CPython calls PyObject_IsSubclass here; for gopy a
		// straight IsSubtype suffices for type targets.
		subCls, isType := subclass.(*objects.Type)
		rkeyCls, rIsType := rkey.(*objects.Type)
		if isType && rIsType && objects.IsSubtype(subCls, rkeyCls) {
			if err := addToWeakSet(d, &d.cache, subclass); err != nil {
				return nil, false, err
			}
			return objects.True(), true, nil
		}
	}
	return nil, false, nil
}

// abcResetRegistry empties cls._abc_impl.registry.
//
// CPython: Modules/_abc.c:270 _abc__reset_registry
func abcResetRegistry(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _reset_registry() takes exactly 1 argument (%d given)", len(args))
	}
	d, err := getImpl(args[0])
	if err != nil {
		return nil, err
	}
	if err := clearSet(d.registry); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// abcResetCaches empties both caches.
//
// CPython: Modules/_abc.c:301 _abc__reset_caches
func abcResetCaches(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _reset_caches() takes exactly 1 argument (%d given)", len(args))
	}
	d, err := getImpl(args[0])
	if err != nil {
		return nil, err
	}
	if err := clearSet(d.cache); err != nil {
		return nil, err
	}
	if err := clearSet(d.negativeCache); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// abcGetDump returns (registry, cache, negative_cache, version) as a
// 4-tuple. Each set is a shallow copy.
//
// CPython: Modules/_abc.c:340 _abc__get_dump
func abcGetDump(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _get_dump() takes exactly 1 argument (%d given)", len(args))
	}
	d, err := getImpl(args[0])
	if err != nil {
		return nil, err
	}
	mk := func(s *objects.Set) *objects.Set {
		out := objects.NewSet()
		if s != nil {
			for _, k := range s.Items() {
				_ = out.Add(k)
			}
		}
		return out
	}
	return objects.NewTuple([]objects.Object{
		mk(d.registry),
		mk(d.cache),
		mk(d.negativeCache),
		objects.NewInt(int64(d.negativeCacheVersion)),
	}), nil
}

// getCacheToken returns the current module-level invalidation counter.
//
// CPython: Modules/_abc.c:919 _abc_get_cache_token_impl
func getCacheToken(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("TypeError: get_cache_token() takes no arguments (%d given)", len(args))
	}
	return objects.NewInt(int64(abcInvalidationCounter.Load())), nil
}

// buildModule materializes the _abc module dict. Mirrors
// _abcmodule_methods plus _abcmodule_exec.
//
// CPython: Modules/_abc.c:927 _abcmodule_methods
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_abc")
	dd := m.Dict()
	entries := []struct {
		name string
		fn   func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"get_cache_token", getCacheToken},
		{"_abc_init", abcInit},
		{"_reset_registry", abcResetRegistry},
		{"_reset_caches", abcResetCaches},
		{"_get_dump", abcGetDump},
		{"_abc_register", abcRegister},
		{"_abc_instancecheck", abcInstanceCheck},
		{"_abc_subclasscheck", abcSubclasscheck},
	}
	for _, e := range entries {
		bf := objects.NewBuiltinFunction(e.name, e.fn)
		if err := dd.SetItem(objects.NewStr(e.name), bf); err != nil {
			return nil, err
		}
	}
	if err := dd.SetItem(objects.NewStr("_abc_data"), abcDataType); err != nil {
		return nil, err
	}
	return m, nil
}
