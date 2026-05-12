// Package _itertools is the gopy port of CPython's itertools C
// module. The C source is Modules/itertoolsmodule.c; this Go package
// mirrors every iterator type and the module-level tee function.
//
// The Go directory carries an underscore prefix so it does not collide
// with a hypothetical Go stdlib name; the module name registered
// through imp.AppendInittab is the bare "itertools" matching CPython.
//
// Types ported (in CPython declaration order):
//   - batched, pairwise
//   - groupby, _grouper
//   - _tee_dataobject, _tee
//   - cycle, dropwhile, takewhile
//   - islice, starmap, chain
//   - product, combinations, combinations_with_replacement, permutations
//   - accumulate, compress, filterfalse
//   - count, repeat
//   - zip_longest
//
// Module-level callable: tee(iterable, n=2).
//
// CPython: Modules/itertoolsmodule.c

package _itertools

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("itertools", buildModule)
}

// buildModule is the moral equivalent of itertoolsmodule_exec: it
// publishes every iterator type and the tee module function.
//
// CPython: Modules/itertoolsmodule.c:4040 itertoolsmodule_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("itertools")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"accumulate", AccumulateType},
		{"batched", BatchedType},
		{"chain", ChainType},
		{"combinations", CombinationsType},
		{"combinations_with_replacement", CombinationsWithReplacementType},
		{"compress", CompressType},
		{"count", CountType},
		{"cycle", CycleType},
		{"dropwhile", DropwhileType},
		{"filterfalse", FilterfalseType},
		{"groupby", GroupbyType},
		{"_grouper", GrouperType},
		{"islice", IsliceType},
		{"pairwise", PairwiseType},
		{"permutations", PermutationsType},
		{"product", ProductType},
		{"repeat", RepeatType},
		{"starmap", StarmapType},
		{"takewhile", TakewhileType},
		{"_tee", TeeType},
		{"_tee_dataobject", TeedataType},
		{"zip_longest", ZipLongestType},
		{"tee", objects.NewBuiltinFunction("tee", teeFunc)},
		{"__doc__", objects.NewStr(moduleDoc)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

const moduleDoc = "Functional tools for creating and using iterators.\n\n" +
	"Infinite iterators:\n" +
	"count(start=0, step=1) --> start, start+step, start+2*step, ...\n" +
	"cycle(p) --> p0, p1, ... plast, p0, p1, ...\n" +
	"repeat(elem [,n]) --> elem, elem, elem, ... endlessly or up to n times\n\n" +
	"Iterators terminating on the shortest input sequence:\n" +
	"accumulate(p[, func]) --> p0, p0+p1, p0+p1+p2\n" +
	"batched(p, n) --> [p0, p1, ..., p_n-1], [p_n, p_n+1, ..., p_2n-1], ...\n" +
	"chain(p, q, ...) --> p0, p1, ... plast, q0, q1, ...\n" +
	"chain.from_iterable([p, q, ...]) --> p0, p1, ... plast, q0, q1, ...\n" +
	"compress(data, selectors) --> (d[0] if s[0]), (d[1] if s[1]), ...\n" +
	"dropwhile(predicate, seq) --> seq[n], seq[n+1], starting when predicate fails\n" +
	"groupby(iterable[, keyfunc]) --> sub-iterators grouped by value of keyfunc(v)\n" +
	"filterfalse(predicate, seq) --> elements of seq where predicate(elem) is False\n" +
	"islice(seq, [start,] stop [, step]) --> elements from seq[start:stop:step]\n" +
	"pairwise(s) --> (s[0],s[1]), (s[1],s[2]), (s[2], s[3]), ...\n" +
	"starmap(fun, seq) --> fun(*seq[0]), fun(*seq[1]), ...\n" +
	"tee(it, n=2) --> (it1, it2 , ... itn) splits one iterator into n\n" +
	"takewhile(predicate, seq) --> seq[0], seq[1], until predicate fails\n" +
	"zip_longest(p, q, ...) --> (p[0], q[0]), (p[1], q[1]), ...\n\n" +
	"Combinatoric generators:\n" +
	"product(p, q, ... [repeat=1]) --> cartesian product\n" +
	"permutations(p[, r])\n" +
	"combinations(p, r)\n" +
	"combinations_with_replacement(p, r)\n"

// ---------------------------------------------------------------------------
// Shared helpers.
// ---------------------------------------------------------------------------

// selfIter implements tp_iter for every iterator type below: return
// self.
//
// CPython: Objects/object.c PyObject_SelfIter (used via Py_tp_iter)
func selfIter(o objects.Object) (objects.Object, error) { return o, nil }

// noKwargs raises TypeError when kwargs is non-empty, mirroring the
// _PyArg_NoKeywords helper CPython uses for "function takes no keyword
// arguments" guards.
//
// CPython: Python/getargs.c:2820 _PyArg_NoKeywords
func noKwargs(name string, kwargs map[string]objects.Object) error {
	if len(kwargs) != 0 {
		return fmt.Errorf("TypeError: %s() takes no keyword arguments", name)
	}
	return nil
}

// asSsizeT converts a Python int-like to a Go int, matching
// PyNumber_AsSsize_t with PyExc_OverflowError clearing.
//
// CPython: Objects/abstract.c PyNumber_AsSsize_t
func asSsizeT(o objects.Object) (int, error) {
	idx, err := objects.NumberIndex(o)
	if err != nil {
		return 0, err
	}
	i, ok := idx.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
	}
	v, ok2 := i.Int64()
	if !ok2 {
		return 0, errors.New("OverflowError: Python int too large to convert to C ssize_t")
	}
	return int(v), nil
}

// isTrue mirrors PyObject_IsTrue and folds the (bool, error) result
// into a single value for the loops below.
//
// CPython: Objects/object.c:1671 PyObject_IsTrue
func isTrue(o objects.Object) (bool, error) {
	return objects.IsTruthy(o)
}

// iterOf returns PyObject_GetIter(o).
//
// CPython: Objects/abstract.c PyObject_GetIter
func iterOf(o objects.Object) (objects.Object, error) {
	return objects.Iter(o)
}

// nextOf returns PyIter_Next(it): a value plus an "exhausted" bool.
// The bool is true exactly when the iterator raised StopIteration; any
// other error short-circuits.
//
// CPython: Objects/abstract.c:3148 PyIter_Next
func nextOf(it objects.Object) (objects.Object, bool, error) {
	v, err := objects.IterNext(it)
	if err != nil {
		if errors.Is(err, objects.ErrStopIteration) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return v, false, nil
}

// callOneArg is PyObject_CallOneArg.
//
// CPython: Objects/call.c:412 PyObject_CallOneArg
func callOneArg(fn, arg objects.Object) (objects.Object, error) {
	return objects.CallOneArg(fn, arg)
}

// callObj is PyObject_Call(func, argsTuple, NULL).
//
// CPython: Objects/call.c:330 PyObject_Call
func callObj(fn objects.Object, args []objects.Object) (objects.Object, error) {
	return objects.Call(fn, objects.NewTuple(args), nil)
}

// finishType wires the slots shared by every itertools iterator: the
// module name is "itertools", tp_iter returns self, attribute access
// uses GenericGetAttr.
func finishType(t *objects.Type, next func(objects.Object) (objects.Object, error)) {
	t.Module = "itertools"
	t.Iter = selfIter
	t.IterNext = next
	t.Getattro = objects.GenericGetAttr
}

// ---------------------------------------------------------------------------
// batched.
// ---------------------------------------------------------------------------

// Batched is batchedobject: iterate over an underlying iterator and
// emit tuples of at most n consecutive values. With strict=True a
// trailing short batch raises ValueError.
//
// CPython: Modules/itertoolsmodule.c:100 batchedobject
type Batched struct {
	objects.Header
	it        objects.Object
	batchSize int
	strict    bool
}

// BatchedType is itertools.batched.
//
// CPython: Modules/itertoolsmodule.c:264 batched_spec
var BatchedType = objects.NewType("batched", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(BatchedType, batchedNext)
	BatchedType.TpNew = batchedNew
}

// batchedNew is itertools.batched.__new__.
//
// CPython: Modules/itertoolsmodule.c:136 batched_new_impl
func batchedNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var strict bool
	if v, ok := kwargs["strict"]; ok {
		b, err := isTrue(v)
		if err != nil {
			return nil, err
		}
		strict = b
		delete(kwargs, "strict")
	}
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: batched() got an unexpected keyword argument")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: batched expected 2 arguments, got %d", len(args))
	}
	n, err := asSsizeT(args[1])
	if err != nil {
		return nil, err
	}
	if n < 1 {
		return nil, errors.New("ValueError: n must be at least one")
	}
	it, err := iterOf(args[0])
	if err != nil {
		return nil, err
	}
	bo := &Batched{it: it, batchSize: n, strict: strict}
	bo.Init(cls)
	return bo, nil
}

// batchedNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:189 batched_next
func batchedNext(o objects.Object) (objects.Object, error) {
	bo := o.(*Batched)
	if bo.batchSize < 0 {
		return nil, objects.ErrStopIteration
	}
	n := bo.batchSize
	out := make([]objects.Object, 0, n)
	for i := 0; i < n; i++ {
		v, stop, err := nextOf(bo.it)
		if err != nil {
			bo.batchSize = -1
			return nil, err
		}
		if stop {
			if i == 0 {
				bo.batchSize = -1
				return nil, objects.ErrStopIteration
			}
			if bo.strict {
				bo.batchSize = -1
				return nil, errors.New("ValueError: batched(): incomplete batch")
			}
			return objects.NewTuple(out), nil
		}
		out = append(out, v)
	}
	return objects.NewTuple(out), nil
}

// ---------------------------------------------------------------------------
// pairwise.
// ---------------------------------------------------------------------------

// Pairwise yields overlapping pairs from the source iterator.
//
// CPython: Modules/itertoolsmodule.c:275 pairwiseobject
type Pairwise struct {
	objects.Header
	it      objects.Object
	old     objects.Object
	hasOld  bool
	stopped bool
}

// PairwiseType is itertools.pairwise.
//
// CPython: Modules/itertoolsmodule.c:417 pairwise_spec
var PairwiseType = objects.NewType("pairwise", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(PairwiseType, pairwiseNext)
	PairwiseType.TpNew = pairwiseNew
}

// pairwiseNew is itertools.pairwise.__new__.
//
// CPython: Modules/itertoolsmodule.c:295 pairwise_new_impl
func pairwiseNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("pairwise", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: pairwise expected 1 argument, got %d", len(args))
	}
	it, err := iterOf(args[0])
	if err != nil {
		return nil, err
	}
	po := &Pairwise{it: it}
	po.Init(cls)
	return po, nil
}

// pairwiseNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:345 pairwise_next
func pairwiseNext(o objects.Object) (objects.Object, error) {
	po := o.(*Pairwise)
	if po.stopped {
		return nil, objects.ErrStopIteration
	}
	if !po.hasOld {
		v, stop, err := nextOf(po.it)
		if err != nil {
			po.stopped = true
			return nil, err
		}
		if stop {
			po.stopped = true
			return nil, objects.ErrStopIteration
		}
		po.old = v
		po.hasOld = true
	}
	v, stop, err := nextOf(po.it)
	if err != nil {
		po.stopped = true
		return nil, err
	}
	if stop {
		po.stopped = true
		return nil, objects.ErrStopIteration
	}
	out := objects.NewTuple([]objects.Object{po.old, v})
	po.old = v
	return out, nil
}

// ---------------------------------------------------------------------------
// groupby.
// ---------------------------------------------------------------------------

// Groupby is groupbyobject: emit (key, _grouper) tuples for runs of
// adjacent equal keys.
//
// CPython: Modules/itertoolsmodule.c:428 groupbyobject
type Groupby struct {
	objects.Header
	it           objects.Object
	keyfunc      objects.Object // None means identity
	tgtkey       objects.Object
	hasTgtKey    bool
	currkey      objects.Object
	hasCurrKey   bool
	currvalue    objects.Object
	hasCurrValue bool
	currgrouper  *Grouper
}

// GroupbyType is itertools.groupby.
//
// CPython: Modules/itertoolsmodule.c:593 groupby_spec
var GroupbyType = objects.NewType("groupby", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(GroupbyType, groupbyNext)
	GroupbyType.TpNew = groupbyNew
}

// groupbyNew is itertools.groupby.__new__.
//
// CPython: Modules/itertoolsmodule.c:457 itertools_groupby_impl
func groupbyNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var iterable, keyfunc objects.Object
	keyfunc = objects.None()
	switch {
	case len(args) == 1 && len(kwargs) == 0:
		iterable = args[0]
	case len(args) == 2 && len(kwargs) == 0:
		iterable = args[0]
		keyfunc = args[1]
	default:
		// honor "iterable=" and "key=" keywords like the Argument Clinic spec.
		if v, ok := kwargs["iterable"]; ok && iterable == nil {
			iterable = v
			delete(kwargs, "iterable")
		}
		if v, ok := kwargs["key"]; ok {
			keyfunc = v
			delete(kwargs, "key")
		}
		if iterable == nil && len(args) >= 1 {
			iterable = args[0]
		}
		if len(args) >= 2 {
			keyfunc = args[1]
		}
		if iterable == nil || len(kwargs) != 0 {
			return nil, errors.New("TypeError: groupby() arguments are (iterable, key=None)")
		}
	}
	it, err := iterOf(iterable)
	if err != nil {
		return nil, err
	}
	gbo := &Groupby{it: it, keyfunc: keyfunc}
	gbo.Init(cls)
	return gbo, nil
}

// groupbyStep is the inline helper that advances the source iterator
// by one element, refreshing currkey + currvalue.
//
// CPython: Modules/itertoolsmodule.c:507 groupby_step
func groupbyStep(gbo *Groupby) (bool, error) {
	v, stop, err := nextOf(gbo.it)
	if err != nil {
		return false, err
	}
	if stop {
		return true, nil
	}
	var k objects.Object
	if objects.IsNone(gbo.keyfunc) {
		k = v
	} else {
		out, err := callOneArg(gbo.keyfunc, v)
		if err != nil {
			return false, err
		}
		k = out
	}
	gbo.currvalue = v
	gbo.hasCurrValue = true
	gbo.currkey = k
	gbo.hasCurrKey = true
	return false, nil
}

// groupbyNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:533 groupby_next
func groupbyNext(o objects.Object) (objects.Object, error) {
	gbo := o.(*Groupby)
	gbo.currgrouper = nil
	for {
		if !gbo.hasCurrKey {
			// fall through to step
		} else if !gbo.hasTgtKey {
			break
		} else {
			eq, err := objects.RichCmpBool(gbo.tgtkey, gbo.currkey, objects.CompareEQ)
			if err != nil {
				return nil, err
			}
			if !eq {
				break
			}
		}
		done, err := groupbyStep(gbo)
		if err != nil {
			return nil, err
		}
		if done {
			return nil, objects.ErrStopIteration
		}
	}
	gbo.tgtkey = gbo.currkey
	gbo.hasTgtKey = true
	g := newGrouper(gbo, gbo.tgtkey)
	return objects.NewTuple([]objects.Object{gbo.currkey, g}), nil
}

// ---------------------------------------------------------------------------
// _grouper (internal).
// ---------------------------------------------------------------------------

// Grouper is the sub-iterator handed back by groupby.
//
// CPython: Modules/itertoolsmodule.c:603 _grouperobject
type Grouper struct {
	objects.Header
	parent *Groupby
	tgtkey objects.Object
}

// GrouperType is itertools._grouper.
//
// CPython: Modules/itertoolsmodule.c:713 _grouper_spec
var GrouperType = objects.NewType("_grouper", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(GrouperType, grouperNext)
	GrouperType.TpNew = grouperNew
}

// newGrouper materializes a Grouper and wires it as the parent's
// active grouper, exactly the way _grouper_create does in C.
//
// CPython: Modules/itertoolsmodule.c:628 _grouper_create
func newGrouper(parent *Groupby, tgtkey objects.Object) *Grouper {
	g := &Grouper{parent: parent, tgtkey: tgtkey}
	g.Init(GrouperType)
	parent.currgrouper = g
	return g
}

// grouperNew is itertools._grouper.__new__: build a Grouper given an
// existing groupby instance and a key.
//
// CPython: Modules/itertoolsmodule.c:620 itertools__grouper_impl
func grouperNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("_grouper", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _grouper expected 2 arguments, got %d", len(args))
	}
	parent, ok := args[0].(*Groupby)
	if !ok {
		return nil, errors.New("TypeError: _grouper expected a groupby object as parent")
	}
	return newGrouper(parent, args[1]), nil
}

// grouperNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:665 _grouper_next
func grouperNext(o objects.Object) (objects.Object, error) {
	g := o.(*Grouper)
	gbo := g.parent
	if gbo.currgrouper != g {
		return nil, objects.ErrStopIteration
	}
	if !gbo.hasCurrValue {
		done, err := groupbyStep(gbo)
		if err != nil {
			return nil, err
		}
		if done {
			return nil, objects.ErrStopIteration
		}
	}
	eq, err := objects.RichCmpBool(g.tgtkey, gbo.currkey, objects.CompareEQ)
	if err != nil {
		return nil, err
	}
	if !eq {
		return nil, objects.ErrStopIteration
	}
	r := gbo.currvalue
	gbo.currvalue = nil
	gbo.hasCurrValue = false
	gbo.currkey = nil
	gbo.hasCurrKey = false
	return r, nil
}

// ---------------------------------------------------------------------------
// tee / _tee / _tee_dataobject.
// ---------------------------------------------------------------------------

// linkCells matches CPython's LINKCELLS: each teedataobject buffers up
// to this many cached values from the source iterator before chaining
// a fresh node.
//
// CPython: Modules/itertoolsmodule.c:732 LINKCELLS
const linkCells = 57

// Teedata is the per-link cache shared by Tee siblings.
//
// CPython: Modules/itertoolsmodule.c:734 teedataobject
type Teedata struct {
	objects.Header
	it       objects.Object
	numread  int
	running  bool
	nextlink *Teedata
	values   [linkCells]objects.Object
}

// TeedataType is itertools._tee_dataobject.
//
// CPython: Modules/itertoolsmodule.c:924 teedataobject_spec
var TeedataType = objects.NewType("_tee_dataobject", []*objects.Type{objects.ObjectType()})

func init() {
	TeedataType.Module = "itertools"
	TeedataType.Getattro = objects.GenericGetAttr
	TeedataType.TpNew = teedataNew
}

// newTeedata is teedataobject_newinternal.
//
// CPython: Modules/itertoolsmodule.c:755 teedataobject_newinternal
func newTeedata(it objects.Object) *Teedata {
	tdo := &Teedata{it: it}
	tdo.Init(TeedataType)
	return tdo
}

// teedataJumplink advances to (or creates) the next link.
//
// CPython: Modules/itertoolsmodule.c:772 teedataobject_jumplink
func teedataJumplink(tdo *Teedata) *Teedata {
	if tdo.nextlink == nil {
		tdo.nextlink = newTeedata(tdo.it)
	}
	return tdo.nextlink
}

// teedataGetitem fetches values[i], pulling one more value from the
// source when this is the lead reader.
//
// CPython: Modules/itertoolsmodule.c:780 teedataobject_getitem
func teedataGetitem(tdo *Teedata, i int) (objects.Object, bool, error) {
	if i < tdo.numread {
		return tdo.values[i], false, nil
	}
	if tdo.running {
		return nil, false, errors.New("RuntimeError: cannot re-enter the tee iterator")
	}
	tdo.running = true
	v, stop, err := nextOf(tdo.it)
	tdo.running = false
	if err != nil {
		return nil, false, err
	}
	if stop {
		return nil, true, nil
	}
	tdo.numread++
	tdo.values[i] = v
	return v, false, nil
}

// teedataNew is itertools._tee_dataobject.__new__: reconstruct a
// teedataobject from its (iterable, values_list, next) reduce tuple.
//
// CPython: Modules/itertoolsmodule.c:869 itertools_teedataobject_impl
func teedataNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("_tee_dataobject", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: _tee_dataobject expected 3 arguments, got %d", len(args))
	}
	values, ok := args[1].(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: _tee_dataobject() values must be a list")
	}
	tdo := newTeedata(args[0])
	n := values.Len()
	if n > linkCells {
		return nil, errors.New("ValueError: Invalid arguments")
	}
	for i := 0; i < n; i++ {
		tdo.values[i] = values.Item(i)
	}
	tdo.numread = n
	if n == linkCells {
		if !objects.IsNone(args[2]) {
			next, ok := args[2].(*Teedata)
			if !ok {
				return nil, errors.New("ValueError: Invalid arguments")
			}
			tdo.nextlink = next
		}
	} else if !objects.IsNone(args[2]) {
		return nil, errors.New("ValueError: Invalid arguments")
	}
	return tdo, nil
}

// Tee is the user-visible iterator: a thin cursor into a Teedata
// chain.
//
// CPython: Modules/itertoolsmodule.c:745 teeobject
type Tee struct {
	objects.Header
	dataobj *Teedata
	index   int
}

// TeeType is itertools._tee.
//
// CPython: Modules/itertoolsmodule.c:1080 tee_spec
var TeeType = objects.NewType("_tee", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(TeeType, teeNext)
	TeeType.TpNew = teeNewSlot
	objects.SetTypeDescr(TeeType, "__copy__", objects.NewMethodDescr(TeeType, "__copy__", teeCopyMethod))
}

// teeCopyImpl mirrors tee_copy_impl: build a new cursor over the same
// data chain.
//
// CPython: Modules/itertoolsmodule.c:962 tee_copy_impl
func teeCopyImpl(to *Tee) *Tee {
	newto := &Tee{dataobj: to.dataobj, index: to.index}
	newto.Init(TeeType)
	return newto
}

// teeCopyMethod is tee.__copy__: hand back an independent cursor.
//
// CPython: Modules/itertoolsmodule.c:977 tee_copy
func teeCopyMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __copy__ expected 1 argument, got %d", len(args))
	}
	to, ok := args[0].(*Tee)
	if !ok {
		return nil, errors.New("TypeError: descriptor '__copy__' requires a '_tee' object")
	}
	return teeCopyImpl(to), nil
}

// teeFromIterable mirrors tee_fromiterable: build a fresh _tee over an
// iterable, fast-pathing the case where the input is already a _tee.
//
// CPython: Modules/itertoolsmodule.c:986 tee_fromiterable
func teeFromIterable(iterable objects.Object) (*Tee, error) {
	it, err := iterOf(iterable)
	if err != nil {
		return nil, err
	}
	if existing, ok := it.(*Tee); ok {
		return teeCopyImpl(existing), nil
	}
	dataobj := newTeedata(it)
	t := &Tee{dataobj: dataobj}
	t.Init(TeeType)
	return t, nil
}

// teeNewSlot is itertools._tee.__new__.
//
// CPython: Modules/itertoolsmodule.c:1028 itertools__tee_impl
func teeNewSlot(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("_tee", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _tee expected 1 argument, got %d", len(args))
	}
	return teeFromIterable(args[0])
}

// teeNext advances the cursor through the chain.
//
// CPython: Modules/itertoolsmodule.c:933 tee_next
func teeNext(o objects.Object) (objects.Object, error) {
	to := o.(*Tee)
	if to.index >= linkCells {
		to.dataobj = teedataJumplink(to.dataobj)
		to.index = 0
	}
	v, stop, err := teedataGetitem(to.dataobj, to.index)
	if err != nil {
		return nil, err
	}
	if stop {
		return nil, objects.ErrStopIteration
	}
	to.index++
	return v, nil
}

// teeFunc is itertools.tee(iterable, n=2): return a tuple of n
// independent iterators.
//
// CPython: Modules/itertoolsmodule.c:1096 itertools_tee_impl
func teeFunc(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("tee", kwargs); err != nil {
		return nil, err
	}
	n := 2
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: tee expected 1 or 2 arguments, got %d", len(args))
	}
	if len(args) == 2 {
		v, err := asSsizeT(args[1])
		if err != nil {
			return nil, err
		}
		n = v
	}
	if n < 0 {
		return nil, errors.New("ValueError: n must be >= 0")
	}
	out := make([]objects.Object, n)
	if n == 0 {
		return objects.NewTuple(out), nil
	}
	first, err := teeFromIterable(args[0])
	if err != nil {
		return nil, err
	}
	out[0] = first
	for i := 1; i < n; i++ {
		out[i] = teeCopyImpl(first)
	}
	return objects.NewTuple(out), nil
}

// ---------------------------------------------------------------------------
// cycle.
// ---------------------------------------------------------------------------

// Cycle is cycleobject: replay the source iterator forever.
//
// CPython: Modules/itertoolsmodule.c:1141 cycleobject
type Cycle struct {
	objects.Header
	it        objects.Object // nil after first pass
	saved     []objects.Object
	index     int
	firstpass bool
}

// CycleType is itertools.cycle.
//
// CPython: Modules/itertoolsmodule.c:1258 cycle_spec
var CycleType = objects.NewType("cycle", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(CycleType, cycleNext)
	CycleType.TpNew = cycleNew
}

// cycleNew is itertools.cycle.__new__.
//
// CPython: Modules/itertoolsmodule.c:1159 itertools_cycle_impl
func cycleNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("cycle", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: cycle expected 1 argument, got %d", len(args))
	}
	it, err := iterOf(args[0])
	if err != nil {
		return nil, err
	}
	lz := &Cycle{it: it, saved: []objects.Object{}}
	lz.Init(cls)
	return lz, nil
}

// cycleNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:1215 cycle_next
func cycleNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Cycle)
	if lz.it != nil {
		v, stop, err := nextOf(lz.it)
		if err != nil {
			return nil, err
		}
		if !stop {
			if !lz.firstpass {
				lz.saved = append(lz.saved, v)
			}
			return v, nil
		}
		lz.it = nil
	}
	if len(lz.saved) == 0 {
		return nil, objects.ErrStopIteration
	}
	v := lz.saved[lz.index]
	lz.index++
	if lz.index >= len(lz.saved) {
		lz.index = 0
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// dropwhile.
// ---------------------------------------------------------------------------

// Dropwhile is dropwhileobject.
//
// CPython: Modules/itertoolsmodule.c:1269 dropwhileobject
type Dropwhile struct {
	objects.Header
	fn    objects.Object
	it    objects.Object
	start bool // true once the predicate has yielded false
}

// DropwhileType is itertools.dropwhile.
//
// CPython: Modules/itertoolsmodule.c:1382 dropwhile_spec
var DropwhileType = objects.NewType("dropwhile", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(DropwhileType, dropwhileNext)
	DropwhileType.TpNew = dropwhileNew
}

// dropwhileNew is itertools.dropwhile.__new__.
//
// CPython: Modules/itertoolsmodule.c:1289 itertools_dropwhile_impl
func dropwhileNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("dropwhile", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: dropwhile expected 2 arguments, got %d", len(args))
	}
	it, err := iterOf(args[1])
	if err != nil {
		return nil, err
	}
	lz := &Dropwhile{fn: args[0], it: it}
	lz.Init(cls)
	return lz, nil
}

// dropwhileNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:1336 dropwhile_next
func dropwhileNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Dropwhile)
	for {
		v, stop, err := nextOf(lz.it)
		if err != nil {
			return nil, err
		}
		if stop {
			return nil, objects.ErrStopIteration
		}
		if lz.start {
			return v, nil
		}
		good, err := callOneArg(lz.fn, v)
		if err != nil {
			return nil, err
		}
		ok, err := isTrue(good)
		if err != nil {
			return nil, err
		}
		if !ok {
			lz.start = true
			return v, nil
		}
	}
}

// ---------------------------------------------------------------------------
// takewhile.
// ---------------------------------------------------------------------------

// Takewhile is takewhileobject.
//
// CPython: Modules/itertoolsmodule.c:1393 takewhileobject
type Takewhile struct {
	objects.Header
	fn   objects.Object
	it   objects.Object
	stop bool
}

// TakewhileType is itertools.takewhile.
//
// CPython: Modules/itertoolsmodule.c:1500 takewhile_spec
var TakewhileType = objects.NewType("takewhile", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(TakewhileType, takewhileNext)
	TakewhileType.TpNew = takewhileNew
}

// takewhileNew is itertools.takewhile.__new__.
//
// CPython: Modules/itertoolsmodule.c:1411 itertools_takewhile_impl
func takewhileNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("takewhile", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: takewhile expected 2 arguments, got %d", len(args))
	}
	it, err := iterOf(args[1])
	if err != nil {
		return nil, err
	}
	lz := &Takewhile{fn: args[0], it: it}
	lz.Init(cls)
	return lz, nil
}

// takewhileNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:1458 takewhile_next
func takewhileNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Takewhile)
	if lz.stop {
		return nil, objects.ErrStopIteration
	}
	v, stop, err := nextOf(lz.it)
	if err != nil {
		return nil, err
	}
	if stop {
		return nil, objects.ErrStopIteration
	}
	good, err := callOneArg(lz.fn, v)
	if err != nil {
		return nil, err
	}
	ok, err := isTrue(good)
	if err != nil {
		return nil, err
	}
	if ok {
		return v, nil
	}
	lz.stop = true
	return nil, objects.ErrStopIteration
}

// ---------------------------------------------------------------------------
// islice.
// ---------------------------------------------------------------------------

// Islice is isliceobject: bounded random-access-style sub-iterator.
//
// CPython: Modules/itertoolsmodule.c:1511 isliceobject
type Islice struct {
	objects.Header
	it   objects.Object
	next int
	stop int // -1 means open-ended
	step int
	cnt  int
	done bool
}

// IsliceType is itertools.islice.
//
// CPython: Modules/itertoolsmodule.c:1692 islice_spec
var IsliceType = objects.NewType("islice", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(IsliceType, isliceNext)
	IsliceType.TpNew = isliceNew
}

// isliceNew is islice.__new__: parse (iterable, stop) /
// (iterable, start, stop[, step]).
//
// CPython: Modules/itertoolsmodule.c:1522 islice_new
func isliceNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("islice", kwargs); err != nil {
		return nil, err
	}
	if len(args) < 2 || len(args) > 4 {
		return nil, fmt.Errorf("TypeError: islice expected 2 to 4 arguments, got %d", len(args))
	}
	start, stop, step := 0, -1, 1
	parseStop := func(o objects.Object) error {
		if objects.IsNone(o) {
			return nil
		}
		v, err := asSsizeT(o)
		if err != nil {
			return errors.New("ValueError: Stop argument for islice() must be None or an integer: 0 <= x <= sys.maxsize.")
		}
		stop = v
		return nil
	}
	parseStart := func(o objects.Object) error {
		if objects.IsNone(o) {
			return nil
		}
		v, err := asSsizeT(o)
		if err != nil {
			return errors.New("ValueError: Indices for islice() must be None or an integer: 0 <= x <= sys.maxsize.")
		}
		start = v
		return nil
	}
	if len(args) == 2 {
		if err := parseStop(args[1]); err != nil {
			return nil, err
		}
	} else {
		if err := parseStart(args[1]); err != nil {
			return nil, err
		}
		if err := parseStop(args[2]); err != nil {
			return nil, err
		}
		if len(args) == 4 && !objects.IsNone(args[3]) {
			v, err := asSsizeT(args[3])
			if err != nil {
				return nil, errors.New("ValueError: Step for islice() must be a positive integer or None.")
			}
			step = v
		}
	}
	if start < 0 || stop < -1 {
		return nil, errors.New("ValueError: Indices for islice() must be None or an integer: 0 <= x <= sys.maxsize.")
	}
	if step < 1 {
		return nil, errors.New("ValueError: Step for islice() must be a positive integer or None.")
	}
	it, err := iterOf(args[0])
	if err != nil {
		return nil, err
	}
	lz := &Islice{it: it, next: start, stop: stop, step: step}
	lz.Init(cls)
	return lz, nil
}

// isliceNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:1629 islice_next
func isliceNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Islice)
	if lz.done {
		return nil, objects.ErrStopIteration
	}
	for lz.cnt < lz.next {
		_, stop, err := nextOf(lz.it)
		if err != nil {
			lz.done = true
			return nil, err
		}
		if stop {
			lz.done = true
			return nil, objects.ErrStopIteration
		}
		lz.cnt++
	}
	if lz.stop != -1 && lz.cnt >= lz.stop {
		lz.done = true
		return nil, objects.ErrStopIteration
	}
	v, stop, err := nextOf(lz.it)
	if err != nil {
		lz.done = true
		return nil, err
	}
	if stop {
		lz.done = true
		return nil, objects.ErrStopIteration
	}
	lz.cnt++
	oldnext := lz.next
	lz.next += lz.step
	if lz.next < oldnext || (lz.stop != -1 && lz.next > lz.stop) {
		lz.next = lz.stop
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// starmap.
// ---------------------------------------------------------------------------

// Starmap is starmapobject.
//
// CPython: Modules/itertoolsmodule.c:1703 starmapobject
type Starmap struct {
	objects.Header
	fn objects.Object
	it objects.Object
}

// StarmapType is itertools.starmap.
//
// CPython: Modules/itertoolsmodule.c:1801 starmap_spec
var StarmapType = objects.NewType("starmap", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(StarmapType, starmapNext)
	StarmapType.TpNew = starmapNew
}

// starmapNew is itertools.starmap.__new__.
//
// CPython: Modules/itertoolsmodule.c:1720 itertools_starmap_impl
func starmapNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("starmap", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: starmap expected 2 arguments, got %d", len(args))
	}
	it, err := iterOf(args[1])
	if err != nil {
		return nil, err
	}
	lz := &Starmap{fn: args[0], it: it}
	lz.Init(cls)
	return lz, nil
}

// starmapNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:1766 starmap_next
func starmapNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Starmap)
	args, stop, err := nextOf(lz.it)
	if err != nil {
		return nil, err
	}
	if stop {
		return nil, objects.ErrStopIteration
	}
	tup, ok := args.(*objects.Tuple)
	if !ok {
		t, err := objects.SequenceTuple(args)
		if err != nil {
			return nil, err
		}
		tup = t
	}
	flat := make([]objects.Object, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		flat[i] = tup.Item(i)
	}
	return callObj(lz.fn, flat)
}

// ---------------------------------------------------------------------------
// chain.
// ---------------------------------------------------------------------------

// Chain is chainobject: walk through a stream of iterables.
//
// CPython: Modules/itertoolsmodule.c:1812 chainobject
type Chain struct {
	objects.Header
	source objects.Object // iterator of iterables, nil when exhausted
	active objects.Object // currently consumed iterator, nil between
}

// ChainType is itertools.chain.
//
// CPython: Modules/itertoolsmodule.c:1964 chain_spec
var ChainType = objects.NewType("chain", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(ChainType, chainNext)
	ChainType.TpNew = chainNew
	objects.SetTypeDescr(ChainType, "from_iterable",
		objects.NewClassMethod(objects.NewBuiltinFunction("from_iterable", chainFromIterable)))
}

// chainNewInternal mirrors chain_new_internal.
//
// CPython: Modules/itertoolsmodule.c:1820 chain_new_internal
func chainNewInternal(cls *objects.Type, source objects.Object) *Chain {
	lz := &Chain{source: source}
	lz.Init(cls)
	return lz
}

// chainNew is itertools.chain.__new__: source is iter(args).
//
// CPython: Modules/itertoolsmodule.c:1836 chain_new
func chainNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("chain", kwargs); err != nil {
		return nil, err
	}
	src, err := iterOf(objects.NewTuple(args))
	if err != nil {
		return nil, err
	}
	return chainNewInternal(cls, src), nil
}

// chainFromIterable is chain.from_iterable(arg): source is iter(arg).
//
// CPython: Modules/itertoolsmodule.c:1862 itertools_chain_from_iterable_impl
func chainFromIterable(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("from_iterable", kwargs); err != nil {
		return nil, err
	}
	// classmethod: args[0] is the class.
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: from_iterable expected 1 argument, got %d", len(args)-1)
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		cls = ChainType
	}
	src, err := iterOf(args[1])
	if err != nil {
		return nil, err
	}
	return chainNewInternal(cls, src), nil
}

// chainNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:1897 chain_next
func chainNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Chain)
	for lz.source != nil {
		if lz.active == nil {
			iterable, stop, err := nextOf(lz.source)
			if err != nil {
				lz.source = nil
				return nil, err
			}
			if stop {
				lz.source = nil
				return nil, objects.ErrStopIteration
			}
			act, err := iterOf(iterable)
			if err != nil {
				lz.source = nil
				return nil, err
			}
			lz.active = act
		}
		v, stop, err := nextOf(lz.active)
		if err != nil {
			return nil, err
		}
		if !stop {
			return v, nil
		}
		lz.active = nil
	}
	return nil, objects.ErrStopIteration
}

// ---------------------------------------------------------------------------
// product.
// ---------------------------------------------------------------------------

// Product is productobject.
//
// CPython: Modules/itertoolsmodule.c:1975 productobject
type Product struct {
	objects.Header
	pools   []*objects.Tuple
	indices []int
	result  []objects.Object
	stopped bool
	started bool
}

// ProductType is itertools.product.
//
// CPython: Modules/itertoolsmodule.c:2222 product_spec
var ProductType = objects.NewType("product", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(ProductType, productNext)
	ProductType.TpNew = productNew
}

// productNew is itertools.product.__new__.
//
// CPython: Modules/itertoolsmodule.c:1985 product_new
func productNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	repeat := 1
	if v, ok := kwargs["repeat"]; ok {
		r, err := asSsizeT(v)
		if err != nil {
			return nil, err
		}
		if r < 0 {
			return nil, errors.New("ValueError: repeat argument cannot be negative")
		}
		repeat = r
		delete(kwargs, "repeat")
	}
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: product() got an unexpected keyword argument")
	}
	nargs := len(args)
	if repeat == 0 {
		nargs = 0
	}
	npools := nargs * repeat
	pools := make([]*objects.Tuple, npools)
	indices := make([]int, npools)
	for i := 0; i < nargs; i++ {
		p, err := objects.SequenceTuple(args[i])
		if err != nil {
			return nil, err
		}
		pools[i] = p
	}
	for i := nargs; i < npools; i++ {
		pools[i] = pools[i-nargs]
	}
	lz := &Product{pools: pools, indices: indices}
	lz.Init(cls)
	return lz, nil
}

// productNext is the tp_iternext slot. The odometer algorithm matches
// product_next byte for byte: rightmost index increments first, with
// rollovers cascading left.
//
// CPython: Modules/itertoolsmodule.c:2102 product_next
func productNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Product)
	if lz.stopped {
		return nil, objects.ErrStopIteration
	}
	npools := len(lz.pools)
	if !lz.started {
		lz.started = true
		lz.result = make([]objects.Object, npools)
		for i, p := range lz.pools {
			if p.Len() == 0 {
				lz.stopped = true
				return nil, objects.ErrStopIteration
			}
			lz.result[i] = p.Item(0)
		}
		out := make([]objects.Object, npools)
		copy(out, lz.result)
		return objects.NewTuple(out), nil
	}
	i := npools - 1
	for ; i >= 0; i-- {
		pool := lz.pools[i]
		lz.indices[i]++
		if lz.indices[i] == pool.Len() {
			lz.indices[i] = 0
			lz.result[i] = pool.Item(0)
			continue
		}
		lz.result[i] = pool.Item(lz.indices[i])
		break
	}
	if i < 0 {
		lz.stopped = true
		return nil, objects.ErrStopIteration
	}
	out := make([]objects.Object, npools)
	copy(out, lz.result)
	return objects.NewTuple(out), nil
}

// ---------------------------------------------------------------------------
// combinations.
// ---------------------------------------------------------------------------

// Combinations is combinationsobject.
//
// CPython: Modules/itertoolsmodule.c:2233 combinationsobject
type Combinations struct {
	objects.Header
	pool    *objects.Tuple
	indices []int
	result  []objects.Object
	r       int
	stopped bool
	started bool
}

// CombinationsType is itertools.combinations.
//
// CPython: Modules/itertoolsmodule.c:2439 combinations_spec
var CombinationsType = objects.NewType("combinations", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(CombinationsType, combinationsNext)
	CombinationsType.TpNew = combinationsNew
}

// combinationsNew is itertools.combinations.__new__.
//
// CPython: Modules/itertoolsmodule.c:2254 itertools_combinations_impl
func combinationsNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("combinations", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: combinations expected 2 arguments, got %d", len(args))
	}
	pool, err := objects.SequenceTuple(args[0])
	if err != nil {
		return nil, err
	}
	r, err := asSsizeT(args[1])
	if err != nil {
		return nil, err
	}
	if r < 0 {
		return nil, errors.New("ValueError: r must be non-negative")
	}
	indices := make([]int, r)
	for i := range indices {
		indices[i] = i
	}
	co := &Combinations{
		pool:    pool,
		indices: indices,
		r:       r,
		stopped: r > pool.Len(),
	}
	co.Init(cls)
	return co, nil
}

// combinationsNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:2335 combinations_next
func combinationsNext(o objects.Object) (objects.Object, error) {
	co := o.(*Combinations)
	if co.stopped {
		return nil, objects.ErrStopIteration
	}
	r := co.r
	n := co.pool.Len()
	if !co.started {
		co.started = true
		co.result = make([]objects.Object, r)
		for i := 0; i < r; i++ {
			co.result[i] = co.pool.Item(co.indices[i])
		}
		out := make([]objects.Object, r)
		copy(out, co.result)
		return objects.NewTuple(out), nil
	}
	i := r - 1
	for i >= 0 && co.indices[i] == i+n-r {
		i--
	}
	if i < 0 {
		co.stopped = true
		return nil, objects.ErrStopIteration
	}
	co.indices[i]++
	for j := i + 1; j < r; j++ {
		co.indices[j] = co.indices[j-1] + 1
	}
	for ; i < r; i++ {
		co.result[i] = co.pool.Item(co.indices[i])
	}
	out := make([]objects.Object, r)
	copy(out, co.result)
	return objects.NewTuple(out), nil
}

// ---------------------------------------------------------------------------
// combinations_with_replacement.
// ---------------------------------------------------------------------------

// Cwr is cwrobject.
//
// CPython: Modules/itertoolsmodule.c:2476 cwrobject
type Cwr struct {
	objects.Header
	pool    *objects.Tuple
	indices []int
	result  []objects.Object
	r       int
	stopped bool
	started bool
}

// CombinationsWithReplacementType is itertools.combinations_with_replacement.
//
// CPython: Modules/itertoolsmodule.c:2677 cwr_spec
var CombinationsWithReplacementType = objects.NewType("combinations_with_replacement",
	[]*objects.Type{objects.ObjectType()})

func init() {
	finishType(CombinationsWithReplacementType, cwrNext)
	CombinationsWithReplacementType.TpNew = cwrNew
}

// cwrNew is itertools.combinations_with_replacement.__new__.
//
// CPython: Modules/itertoolsmodule.c:2497 itertools_combinations_with_replacement_impl
func cwrNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("combinations_with_replacement", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: combinations_with_replacement expected 2 arguments, got %d", len(args))
	}
	pool, err := objects.SequenceTuple(args[0])
	if err != nil {
		return nil, err
	}
	r, err := asSsizeT(args[1])
	if err != nil {
		return nil, err
	}
	if r < 0 {
		return nil, errors.New("ValueError: r must be non-negative")
	}
	indices := make([]int, r)
	co := &Cwr{
		pool:    pool,
		indices: indices,
		r:       r,
		stopped: pool.Len() == 0 && r > 0,
	}
	co.Init(cls)
	return co, nil
}

// cwrNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:2579 cwr_next
func cwrNext(o objects.Object) (objects.Object, error) {
	co := o.(*Cwr)
	if co.stopped {
		return nil, objects.ErrStopIteration
	}
	r := co.r
	n := co.pool.Len()
	if !co.started {
		co.started = true
		co.result = make([]objects.Object, r)
		if n > 0 {
			elem := co.pool.Item(0)
			for i := 0; i < r; i++ {
				co.result[i] = elem
			}
		}
		out := make([]objects.Object, r)
		copy(out, co.result)
		return objects.NewTuple(out), nil
	}
	i := r - 1
	for i >= 0 && co.indices[i] == n-1 {
		i--
	}
	if i < 0 {
		co.stopped = true
		return nil, objects.ErrStopIteration
	}
	index := co.indices[i] + 1
	elem := co.pool.Item(index)
	for ; i < r; i++ {
		co.indices[i] = index
		co.result[i] = elem
	}
	out := make([]objects.Object, r)
	copy(out, co.result)
	return objects.NewTuple(out), nil
}

// ---------------------------------------------------------------------------
// permutations.
// ---------------------------------------------------------------------------

// Permutations is permutationsobject.
//
// CPython: Modules/itertoolsmodule.c:2714 permutationsobject
type Permutations struct {
	objects.Header
	pool    *objects.Tuple
	indices []int
	cycles  []int
	result  []objects.Object
	r       int
	stopped bool
	started bool
}

// PermutationsType is itertools.permutations.
//
// CPython: Modules/itertoolsmodule.c:2947 permutations_spec
var PermutationsType = objects.NewType("permutations", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(PermutationsType, permutationsNext)
	PermutationsType.TpNew = permutationsNew
}

// permutationsNew is itertools.permutations.__new__.
//
// CPython: Modules/itertoolsmodule.c:2736 itertools_permutations_impl
func permutationsNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("permutations", kwargs); err != nil {
		return nil, err
	}
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: permutations expected 1 or 2 arguments, got %d", len(args))
	}
	pool, err := objects.SequenceTuple(args[0])
	if err != nil {
		return nil, err
	}
	n := pool.Len()
	r := n
	if len(args) == 2 && !objects.IsNone(args[1]) {
		_, isInt := args[1].(*objects.Int)
		if !isInt {
			return nil, errors.New("TypeError: Expected int as r")
		}
		v, err := asSsizeT(args[1])
		if err != nil {
			return nil, err
		}
		r = v
	}
	if r < 0 {
		return nil, errors.New("ValueError: r must be non-negative")
	}
	indices := make([]int, n)
	for i := 0; i < n; i++ {
		indices[i] = i
	}
	cycles := make([]int, r)
	for i := 0; i < r; i++ {
		cycles[i] = n - i
	}
	po := &Permutations{
		pool:    pool,
		indices: indices,
		cycles:  cycles,
		r:       r,
		stopped: r > n,
	}
	po.Init(cls)
	return po, nil
}

// permutationsNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:2838 permutations_next
func permutationsNext(o objects.Object) (objects.Object, error) {
	po := o.(*Permutations)
	if po.stopped {
		return nil, objects.ErrStopIteration
	}
	r := po.r
	n := po.pool.Len()
	if !po.started {
		po.started = true
		po.result = make([]objects.Object, r)
		for i := 0; i < r; i++ {
			po.result[i] = po.pool.Item(po.indices[i])
		}
		out := make([]objects.Object, r)
		copy(out, po.result)
		return objects.NewTuple(out), nil
	}
	if n == 0 {
		po.stopped = true
		return nil, objects.ErrStopIteration
	}
	i := r - 1
	for ; i >= 0; i-- {
		po.cycles[i]--
		if po.cycles[i] == 0 {
			// rotation: indices[i:] = indices[i+1:] + indices[i:i+1]
			index := po.indices[i]
			for j := i; j < n-1; j++ {
				po.indices[j] = po.indices[j+1]
			}
			po.indices[n-1] = index
			po.cycles[i] = n - i
		} else {
			j := po.cycles[i]
			po.indices[i], po.indices[n-j] = po.indices[n-j], po.indices[i]
			for k := i; k < r; k++ {
				po.result[k] = po.pool.Item(po.indices[k])
			}
			break
		}
	}
	if i < 0 {
		po.stopped = true
		return nil, objects.ErrStopIteration
	}
	out := make([]objects.Object, r)
	copy(out, po.result)
	return objects.NewTuple(out), nil
}

// ---------------------------------------------------------------------------
// accumulate.
// ---------------------------------------------------------------------------

// Accumulate is accumulateobject.
//
// CPython: Modules/itertoolsmodule.c:2958 accumulateobject
type Accumulate struct {
	objects.Header
	total      objects.Object
	hasTotal   bool
	it         objects.Object
	binop      objects.Object // nil for default add
	initial    objects.Object
	hasInitial bool
}

// AccumulateType is itertools.accumulate.
//
// CPython: Modules/itertoolsmodule.c:3080 accumulate_spec
var AccumulateType = objects.NewType("accumulate", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(AccumulateType, accumulateNext)
	AccumulateType.TpNew = accumulateNew
}

// accumulateNew is itertools.accumulate.__new__.
//
// CPython: Modules/itertoolsmodule.c:2979 itertools_accumulate_impl
func accumulateNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var initial objects.Object = objects.None()
	if v, ok := kwargs["initial"]; ok {
		initial = v
		delete(kwargs, "initial")
	}
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: accumulate() got an unexpected keyword argument")
	}
	var iterable, binop objects.Object
	switch len(args) {
	case 1:
		iterable = args[0]
		binop = objects.None()
	case 2:
		iterable, binop = args[0], args[1]
	default:
		return nil, fmt.Errorf("TypeError: accumulate expected 1 or 2 positional arguments, got %d", len(args))
	}
	it, err := iterOf(iterable)
	if err != nil {
		return nil, err
	}
	lz := &Accumulate{it: it}
	if !objects.IsNone(binop) {
		lz.binop = binop
	}
	if !objects.IsNone(initial) {
		lz.initial = initial
		lz.hasInitial = true
	}
	lz.Init(cls)
	return lz, nil
}

// accumulateNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:3035 accumulate_next
func accumulateNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Accumulate)
	if lz.hasInitial {
		lz.total = lz.initial
		lz.hasTotal = true
		lz.hasInitial = false
		lz.initial = nil
		return lz.total, nil
	}
	v, stop, err := nextOf(lz.it)
	if err != nil {
		return nil, err
	}
	if stop {
		return nil, objects.ErrStopIteration
	}
	if !lz.hasTotal {
		lz.total = v
		lz.hasTotal = true
		return v, nil
	}
	var newtotal objects.Object
	if lz.binop == nil {
		newtotal, err = objects.NumberAdd(lz.total, v)
	} else {
		newtotal, err = callObj(lz.binop, []objects.Object{lz.total, v})
	}
	if err != nil {
		return nil, err
	}
	lz.total = newtotal
	return newtotal, nil
}

// ---------------------------------------------------------------------------
// compress.
// ---------------------------------------------------------------------------

// Compress is compressobject.
//
// CPython: Modules/itertoolsmodule.c:3098 compressobject
type Compress struct {
	objects.Header
	data      objects.Object
	selectors objects.Object
}

// CompressType is itertools.compress.
//
// CPython: Modules/itertoolsmodule.c:3216 compress_spec
var CompressType = objects.NewType("compress", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(CompressType, compressNext)
	CompressType.TpNew = compressNew
}

// compressNew is itertools.compress.__new__.
//
// CPython: Modules/itertoolsmodule.c:3117 itertools_compress_impl
func compressNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("compress", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: compress expected 2 arguments, got %d", len(args))
	}
	data, err := iterOf(args[0])
	if err != nil {
		return nil, err
	}
	sels, err := iterOf(args[1])
	if err != nil {
		return nil, err
	}
	lz := &Compress{data: data, selectors: sels}
	lz.Init(cls)
	return lz, nil
}

// compressNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:3167 compress_next
func compressNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Compress)
	for {
		datum, stop, err := nextOf(lz.data)
		if err != nil {
			return nil, err
		}
		if stop {
			return nil, objects.ErrStopIteration
		}
		sel, stop, err := nextOf(lz.selectors)
		if err != nil {
			return nil, err
		}
		if stop {
			return nil, objects.ErrStopIteration
		}
		ok, err := isTrue(sel)
		if err != nil {
			return nil, err
		}
		if ok {
			return datum, nil
		}
	}
}

// ---------------------------------------------------------------------------
// filterfalse.
// ---------------------------------------------------------------------------

// Filterfalse is filterfalseobject.
//
// CPython: Modules/itertoolsmodule.c:3227 filterfalseobject
type Filterfalse struct {
	objects.Header
	fn objects.Object
	it objects.Object
}

// FilterfalseType is itertools.filterfalse.
//
// CPython: Modules/itertoolsmodule.c:3339 filterfalse_spec
var FilterfalseType = objects.NewType("filterfalse", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(FilterfalseType, filterfalseNext)
	FilterfalseType.TpNew = filterfalseNew
}

// filterfalseNew is itertools.filterfalse.__new__.
//
// CPython: Modules/itertoolsmodule.c:3246 itertools_filterfalse_impl
func filterfalseNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if err := noKwargs("filterfalse", kwargs); err != nil {
		return nil, err
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: filterfalse expected 2 arguments, got %d", len(args))
	}
	it, err := iterOf(args[1])
	if err != nil {
		return nil, err
	}
	lz := &Filterfalse{fn: args[0], it: it}
	lz.Init(cls)
	return lz, nil
}

// filterfalseNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:3292 filterfalse_next
func filterfalseNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Filterfalse)
	for {
		v, stop, err := nextOf(lz.it)
		if err != nil {
			return nil, err
		}
		if stop {
			return nil, objects.ErrStopIteration
		}
		var ok bool
		if objects.IsNone(lz.fn) || lz.fn == objects.BoolType {
			ok, err = isTrue(v)
		} else {
			good, callErr := callOneArg(lz.fn, v)
			if callErr != nil {
				return nil, callErr
			}
			ok, err = isTrue(good)
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return v, nil
		}
	}
}

// ---------------------------------------------------------------------------
// count.
// ---------------------------------------------------------------------------

// Count is countobject. The two operating modes match CPython's
// fast/slow split: when both start and step are simple ints with
// step==1 we increment a Go int; otherwise everything goes through
// NumberAdd.
//
// CPython: Modules/itertoolsmodule.c:3350 countobject
type Count struct {
	objects.Header
	fastCnt int64 // valid only when slow == false
	slow    bool
	cnt     objects.Object // valid only when slow == true
	step    objects.Object
}

// CountType is itertools.count.
//
// CPython: Modules/itertoolsmodule.c:3581 count_spec
var CountType = objects.NewType("count", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(CountType, countNext)
	CountType.TpNew = countNew
	CountType.Repr = countRepr
	CountType.Str = countRepr
}

// numberCheck approximates PyNumber_Check: a value is "a number" when
// its type carries number methods.
//
// CPython: Objects/abstract.c PyNumber_Check
func numberCheck(o objects.Object) bool {
	return o.Type().Number != nil
}

// countNew is itertools.count.__new__.
//
// CPython: Modules/itertoolsmodule.c:3391 itertools_count_impl
func countNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var startObj, stepObj objects.Object
	switch len(args) {
	case 0:
	case 1:
		startObj = args[0]
	case 2:
		startObj, stepObj = args[0], args[1]
	default:
		return nil, fmt.Errorf("TypeError: count expected 0, 1, or 2 arguments, got %d", len(args))
	}
	if v, ok := kwargs["start"]; ok && startObj == nil {
		startObj = v
		delete(kwargs, "start")
	}
	if v, ok := kwargs["step"]; ok && stepObj == nil {
		stepObj = v
		delete(kwargs, "step")
	}
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: count() got an unexpected keyword argument")
	}
	if startObj != nil && !numberCheck(startObj) {
		return nil, errors.New("TypeError: a number is required")
	}
	if stepObj != nil && !numberCheck(stepObj) {
		return nil, errors.New("TypeError: a number is required")
	}
	if startObj == nil {
		startObj = objects.NewInt(0)
	}
	if stepObj == nil {
		stepObj = objects.NewInt(1)
	}
	lz := &Count{step: stepObj}
	// fast mode requires both start and step to be ints and step == 1.
	startInt, startIsInt := startObj.(*objects.Int)
	stepInt, stepIsInt := stepObj.(*objects.Int)
	if startIsInt && stepIsInt {
		if sv, ok := startInt.Int64(); ok {
			if tv, ok := stepInt.Int64(); ok && tv == 1 {
				lz.fastCnt = sv
				lz.cnt = nil
				lz.Init(cls)
				return lz, nil
			}
		}
	}
	lz.slow = true
	lz.cnt = startObj
	lz.Init(cls)
	return lz, nil
}

// countNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:3513 count_next
func countNext(o objects.Object) (objects.Object, error) {
	lz := o.(*Count)
	if !lz.slow {
		v := objects.NewInt(lz.fastCnt)
		lz.fastCnt++
		return v, nil
	}
	cur := lz.cnt
	next, err := objects.NumberAdd(cur, lz.step)
	if err != nil {
		return nil, err
	}
	lz.cnt = next
	return cur, nil
}

// countRepr mirrors count_repr: `count(start)` with step omitted when
// step is an int equal to 1.
//
// CPython: Modules/itertoolsmodule.c:3543 count_repr
func countRepr(o objects.Object) (string, error) {
	lz := o.(*Count)
	if !lz.slow {
		return fmt.Sprintf("count(%d)", lz.fastCnt), nil
	}
	cntRepr, err := objects.Repr(lz.cnt)
	if err != nil {
		return "", err
	}
	if si, ok := lz.step.(*objects.Int); ok {
		if v, ok := si.Int64(); ok && v == 1 {
			return "count(" + cntRepr + ")", nil
		}
	}
	stepRepr, err := objects.Repr(lz.step)
	if err != nil {
		return "", err
	}
	return "count(" + cntRepr + ", " + stepRepr + ")", nil
}

// ---------------------------------------------------------------------------
// repeat.
// ---------------------------------------------------------------------------

// Repeat is repeatobject.
//
// CPython: Modules/itertoolsmodule.c:3592 repeatobject
type Repeat struct {
	objects.Header
	element objects.Object
	cnt     int64 // -1 means infinite
}

// RepeatType is itertools.repeat.
//
// CPython: Modules/itertoolsmodule.c:3711 repeat_spec
var RepeatType = objects.NewType("repeat", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(RepeatType, repeatNext)
	RepeatType.TpNew = repeatNew
	RepeatType.Repr = repeatRepr
	RepeatType.Str = repeatRepr
	objects.SetTypeDescr(RepeatType, "__length_hint__",
		objects.NewMethodDescr(RepeatType, "__length_hint__", repeatLengthHint))
}

// repeatNew is itertools.repeat.__new__.
//
// CPython: Modules/itertoolsmodule.c:3600 repeat_new
func repeatNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var element objects.Object
	cnt := int64(-1)
	timesGiven := false
	if v, ok := kwargs["object"]; ok && element == nil {
		element = v
		delete(kwargs, "object")
	}
	if v, ok := kwargs["times"]; ok {
		n, err := asSsizeT(v)
		if err != nil {
			return nil, err
		}
		cnt = int64(n)
		timesGiven = true
		delete(kwargs, "times")
	}
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: repeat() got an unexpected keyword argument")
	}
	switch len(args) {
	case 0:
		if element == nil {
			return nil, errors.New("TypeError: repeat() missing 1 required positional argument: 'object'")
		}
	case 1:
		if element == nil {
			element = args[0]
		} else {
			return nil, errors.New("TypeError: repeat() got multiple values for argument 'object'")
		}
	case 2:
		element = args[0]
		n, err := asSsizeT(args[1])
		if err != nil {
			return nil, err
		}
		cnt = int64(n)
		timesGiven = true
	default:
		return nil, fmt.Errorf("TypeError: repeat expected 1 or 2 arguments, got %d", len(args))
	}
	if timesGiven && cnt < 0 {
		cnt = 0
	}
	ro := &Repeat{element: element, cnt: cnt}
	ro.Init(cls)
	return ro, nil
}

// repeatNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:3646 repeat_next
func repeatNext(o objects.Object) (objects.Object, error) {
	ro := o.(*Repeat)
	if ro.cnt == 0 {
		return nil, objects.ErrStopIteration
	}
	if ro.cnt > 0 {
		ro.cnt--
	}
	return ro.element, nil
}

// repeatRepr mirrors repeat_repr.
//
// CPython: Modules/itertoolsmodule.c:3661 repeat_repr
func repeatRepr(o objects.Object) (string, error) {
	ro := o.(*Repeat)
	elemRepr, err := objects.Repr(ro.element)
	if err != nil {
		return "", err
	}
	if ro.cnt == -1 {
		return "repeat(" + elemRepr + ")", nil
	}
	return fmt.Sprintf("repeat(%s, %d)", elemRepr, ro.cnt), nil
}

// repeatLengthHint is repeat.__length_hint__.
//
// CPython: Modules/itertoolsmodule.c:3674 repeat_len
func repeatLengthHint(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __length_hint__ expected 1 argument, got %d", len(args))
	}
	ro, ok := args[0].(*Repeat)
	if !ok {
		return nil, errors.New("TypeError: descriptor '__length_hint__' requires a 'repeat' object")
	}
	if ro.cnt == -1 {
		return nil, errors.New("TypeError: len() of unsized object")
	}
	return objects.NewInt(ro.cnt), nil
}

// ---------------------------------------------------------------------------
// zip_longest.
// ---------------------------------------------------------------------------

// ZipLongest is ziplongestobject.
//
// CPython: Modules/itertoolsmodule.c:3722 ziplongestobject
type ZipLongest struct {
	objects.Header
	tuplesize int
	numactive int
	itTuple   []objects.Object // entries become nil after the matching iterator is exhausted
	fillvalue objects.Object
}

// ZipLongestType is itertools.zip_longest.
//
// CPython: Modules/itertoolsmodule.c:3921 ziplongest_spec
var ZipLongestType = objects.NewType("zip_longest", []*objects.Type{objects.ObjectType()})

func init() {
	finishType(ZipLongestType, zipLongestNext)
	ZipLongestType.TpNew = zipLongestNew
}

// zipLongestNew is itertools.zip_longest.__new__.
//
// CPython: Modules/itertoolsmodule.c:3733 zip_longest_new
func zipLongestNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	fillvalue := objects.None()
	if v, ok := kwargs["fillvalue"]; ok {
		fillvalue = v
		delete(kwargs, "fillvalue")
	}
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: zip_longest() got an unexpected keyword argument")
	}
	its := make([]objects.Object, len(args))
	for i, a := range args {
		it, err := iterOf(a)
		if err != nil {
			return nil, err
		}
		its[i] = it
	}
	lz := &ZipLongest{
		tuplesize: len(args),
		numactive: len(args),
		itTuple:   its,
		fillvalue: fillvalue,
	}
	lz.Init(cls)
	return lz, nil
}

// zipLongestNext is the tp_iternext slot.
//
// CPython: Modules/itertoolsmodule.c:3825 zip_longest_next
func zipLongestNext(o objects.Object) (objects.Object, error) {
	lz := o.(*ZipLongest)
	if lz.tuplesize == 0 || lz.numactive == 0 {
		return nil, objects.ErrStopIteration
	}
	out := make([]objects.Object, lz.tuplesize)
	for i := 0; i < lz.tuplesize; i++ {
		it := lz.itTuple[i]
		if it == nil {
			out[i] = lz.fillvalue
			continue
		}
		v, stop, err := nextOf(it)
		if err != nil {
			lz.numactive = 0
			return nil, err
		}
		if stop {
			lz.numactive--
			if lz.numactive == 0 {
				return nil, objects.ErrStopIteration
			}
			lz.itTuple[i] = nil
			out[i] = lz.fillvalue
			continue
		}
		out[i] = v
	}
	return objects.NewTuple(out), nil
}
