// dataclasses module: Go port of Lib/dataclasses.py. CPython generates
// __init__, __repr__, __eq__, and __hash__ by exec()ing a textual
// function definition; this port builds the same surface directly in
// Go, so each generated method is a MethodDescr backed by a Go closure
// that captures the field list.
//
// Field discovery: collectFields reads cls.__annotations__ for the
// field name list and matches against the class descriptor table for
// defaults. SETUP_ANNOTATIONS populates __annotations__ at class scope
// so bare annotated fields (no default) participate too.
//
// CPython: Lib/dataclasses.py:1422 dataclass
// CPython: Lib/dataclasses.py:391 field
// CPython: Lib/dataclasses.py:279 Field
// CPython: Lib/dataclasses.py:185 MISSING
// CPython: Lib/dataclasses.py:191 KW_ONLY
// CPython: Lib/dataclasses.py:171 FrozenInstanceError
// CPython: Lib/dataclasses.py:252 InitVar
// CPython: Lib/dataclasses.py:1453 fields
// CPython: Lib/dataclasses.py:1476 is_dataclass
// CPython: Lib/dataclasses.py:1483 asdict
// CPython: Lib/dataclasses.py:1575 astuple
// CPython: Lib/dataclasses.py:1635 make_dataclass
// CPython: Lib/dataclasses.py:1762 replace

package dataclasses

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

const (
	fieldsAttr    = "__dataclass_fields__"
	paramsAttr    = "__dataclass_params__"
	postInitName  = "__post_init__"
	matchArgsAttr = "__match_args__"
)

// missingSingleton is the gopy stand-in for the MISSING sentinel.
//
// CPython: Lib/dataclasses.py:183 _MISSING_TYPE
type missingSingleton struct {
	objects.Header
}

var missingType = objects.NewType("_MISSING_TYPE", []*objects.Type{objects.ObjectType()})

func init() {
	missingType.Repr = func(_ objects.Object) (string, error) { return "MISSING", nil }
	missingType.Str = missingType.Repr
}

func newMissing() objects.Object {
	m := &missingSingleton{}
	m.Init(missingType)
	return m
}

var missingValue = newMissing()

// kwOnlySingleton backs the KW_ONLY marker.
//
// CPython: Lib/dataclasses.py:189 _KW_ONLY_TYPE
type kwOnlySingleton struct {
	objects.Header
}

var kwOnlyType = objects.NewType("_KW_ONLY_TYPE", []*objects.Type{objects.ObjectType()})

func init() {
	kwOnlyType.Repr = func(_ objects.Object) (string, error) { return "KW_ONLY", nil }
	kwOnlyType.Str = kwOnlyType.Repr
}

func newKWOnly() objects.Object {
	k := &kwOnlySingleton{}
	k.Init(kwOnlyType)
	return k
}

var kwOnlyValue = newKWOnly()

// fieldObj backs the public Field class. CPython exposes the same
// shape via __slots__; we use a plain Go struct since gopy has no
// stable __slots__ contract that survives subclass overrides yet.
//
// CPython: Lib/dataclasses.py:279 Field
type fieldObj struct {
	objects.Header
	name           string
	tp             objects.Object // type annotation, often None
	defaultVal     objects.Object
	defaultFactory objects.Object
	repr           bool
	hash           objects.Object // True, False, or None
	init           bool
	compare        bool
	metadata       objects.Object
	kwOnly         objects.Object // True, False, or MISSING
	doc            objects.Object
	fieldType      string // "_FIELD", "_FIELD_CLASSVAR", "_FIELD_INITVAR"
}

// FieldType is the type singleton for Field.
//
// CPython: Lib/dataclasses.py:279 Field
var FieldType = objects.NewType("Field", []*objects.Type{objects.ObjectType()})

func init() {
	FieldType.Repr = fieldRepr
	FieldType.Str = fieldRepr
	FieldType.Getattro = fieldGetattro
	FieldType.Setattro = fieldSetattro
}

func newField(defaultVal, defaultFactory objects.Object, init, repr bool,
	hash objects.Object, compare bool, metadata, kwOnly, doc objects.Object,
) *fieldObj {
	if metadata == nil {
		metadata = objects.NewDict()
	}
	f := &fieldObj{
		name:           "",
		tp:             objects.None(),
		defaultVal:     defaultVal,
		defaultFactory: defaultFactory,
		init:           init,
		repr:           repr,
		hash:           hash,
		compare:        compare,
		metadata:       metadata,
		kwOnly:         kwOnly,
		doc:            doc,
		fieldType:      "_FIELD",
	}
	f.Init(FieldType)
	return f
}

func fieldRepr(o objects.Object) (string, error) {
	f := o.(*fieldObj)
	return fmt.Sprintf("Field(name=%q,default=%s,default_factory=%s,init=%v,repr=%v,kw_only=%s)",
		f.name, mustRepr(f.defaultVal), mustRepr(f.defaultFactory), f.init, f.repr, mustRepr(f.kwOnly)), nil
}

func mustRepr(o objects.Object) string {
	if o == nil {
		return "None"
	}
	s, err := objects.Repr(o)
	if err != nil {
		return "<repr err>"
	}
	return s
}

// fieldGetattro exposes the slot attributes documented on Field.
//
// CPython: Lib/dataclasses.py:280 Field.__slots__
func fieldGetattro(o objects.Object, name objects.Object) (objects.Object, error) {
	f := o.(*fieldObj)
	ns, err := objects.Str(name)
	if err != nil {
		return nil, err
	}
	switch ns {
	case "name":
		return objects.NewStr(f.name), nil
	case "type":
		return f.tp, nil
	case "default":
		return f.defaultVal, nil
	case "default_factory":
		return f.defaultFactory, nil
	case "init":
		return objects.NewBool(f.init), nil
	case "repr":
		return objects.NewBool(f.repr), nil
	case "hash":
		return f.hash, nil
	case "compare":
		return objects.NewBool(f.compare), nil
	case "metadata":
		return f.metadata, nil
	case "kw_only":
		return f.kwOnly, nil
	case "doc":
		return f.doc, nil
	case "_field_type":
		return objects.NewStr(f.fieldType), nil
	}
	return objects.GenericGetAttr(o, name)
}

func fieldSetattro(o objects.Object, name objects.Object, value objects.Object) error {
	f := o.(*fieldObj)
	ns, err := objects.Str(name)
	if err != nil {
		return err
	}
	switch ns {
	case "name":
		s, err := objects.Str(value)
		if err != nil {
			return err
		}
		f.name = s
		return nil
	case "type":
		f.tp = value
		return nil
	}
	return fmt.Errorf("AttributeError: 'Field' object has no settable attribute %q", ns)
}

// field is the public constructor.
//
// CPython: Lib/dataclasses.py:391 field
func fieldBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("TypeError: field() takes 0 positional arguments but %d were given", len(args))
	}
	def := missingValue
	if v, ok := kwargs["default"]; ok {
		def = v
	}
	defFactory := missingValue
	if v, ok := kwargs["default_factory"]; ok {
		defFactory = v
	}
	if def != missingValue && defFactory != missingValue {
		return nil, fmt.Errorf("ValueError: cannot specify both default and default_factory")
	}
	init := true
	if v, ok := kwargs["init"]; ok {
		init = objects.IsTrue(v)
	}
	repr := true
	if v, ok := kwargs["repr"]; ok {
		repr = objects.IsTrue(v)
	}
	hash := objects.None()
	if v, ok := kwargs["hash"]; ok {
		hash = v
	}
	compare := true
	if v, ok := kwargs["compare"]; ok {
		compare = objects.IsTrue(v)
	}
	metadata := objects.None()
	if v, ok := kwargs["metadata"]; ok {
		metadata = v
	}
	kwOnly := missingValue
	if v, ok := kwargs["kw_only"]; ok {
		kwOnly = v
	}
	doc := objects.None()
	if v, ok := kwargs["doc"]; ok {
		doc = v
	}
	return newField(def, defFactory, init, repr, hash, compare, metadata, kwOnly, doc), nil
}

// initVarObj backs the InitVar marker. CPython exposes InitVar[T] via
// __class_getitem__; we approximate by treating InitVar(t) and
// InitVar[t] identically and storing the wrapped type for `fields`.
//
// CPython: Lib/dataclasses.py:252 InitVar
type initVarObj struct {
	objects.Header
	tp objects.Object
}

// InitVarType is the type singleton for InitVar instances.
var InitVarType = objects.NewType("InitVar", []*objects.Type{objects.ObjectType()})

func init() {
	InitVarType.Repr = func(o objects.Object) (string, error) {
		iv := o.(*initVarObj)
		return fmt.Sprintf("dataclasses.InitVar[%s]", mustRepr(iv.tp)), nil
	}
	InitVarType.Str = InitVarType.Repr
	InitVarType.TpNew = func(_ *objects.Type, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: InitVar() requires 1 argument")
		}
		iv := &initVarObj{tp: args[0]}
		iv.Init(InitVarType)
		return iv, nil
	}
}

// FrozenInstanceError is the exception type raised on writes to a
// frozen instance. CPython makes it a subclass of AttributeError; we
// mirror that lineage so `except AttributeError` catches frozen
// writes.
//
// CPython: Lib/dataclasses.py:171 FrozenInstanceError
var FrozenInstanceError = objects.NewType("FrozenInstanceError", []*objects.Type{errors.PyExc_AttributeError})

// dataclassParams stores @dataclass(**flags) for later introspection
// via __dataclass_params__.
//
// CPython: Lib/dataclasses.py:346 _DataclassParams
type dataclassParams struct {
	objects.Header
	init        bool
	repr        bool
	eq          bool
	order       bool
	unsafeHash  bool
	frozen      bool
	matchArgs   bool
	kwOnly      bool
	slots       bool
	weakrefSlot bool
}

var paramsType = objects.NewType("_DataclassParams", []*objects.Type{objects.ObjectType()})

func init() {
	paramsType.Repr = func(o objects.Object) (string, error) {
		p := o.(*dataclassParams)
		return fmt.Sprintf("_DataclassParams(init=%v,repr=%v,eq=%v,order=%v,unsafe_hash=%v,frozen=%v,match_args=%v,kw_only=%v,slots=%v,weakref_slot=%v)",
			p.init, p.repr, p.eq, p.order, p.unsafeHash, p.frozen, p.matchArgs, p.kwOnly, p.slots, p.weakrefSlot), nil
	}
	paramsType.Str = paramsType.Repr
	paramsType.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		p := o.(*dataclassParams)
		ns, err := objects.Str(name)
		if err != nil {
			return nil, err
		}
		switch ns {
		case "init":
			return objects.NewBool(p.init), nil
		case "repr":
			return objects.NewBool(p.repr), nil
		case "eq":
			return objects.NewBool(p.eq), nil
		case "order":
			return objects.NewBool(p.order), nil
		case "unsafe_hash":
			return objects.NewBool(p.unsafeHash), nil
		case "frozen":
			return objects.NewBool(p.frozen), nil
		case "match_args":
			return objects.NewBool(p.matchArgs), nil
		case "kw_only":
			return objects.NewBool(p.kwOnly), nil
		case "slots":
			return objects.NewBool(p.slots), nil
		case "weakref_slot":
			return objects.NewBool(p.weakrefSlot), nil
		}
		return objects.GenericGetAttr(o, name)
	}
}

// dataclassFlags captures the keyword arguments to @dataclass.
type dataclassFlags struct {
	init        bool
	repr        bool
	eq          bool
	order       bool
	unsafeHash  bool
	frozen      bool
	matchArgs   bool
	kwOnly      bool
	slots       bool
	weakrefSlot bool
}

func defaultFlags() dataclassFlags {
	return dataclassFlags{
		init:      true,
		repr:      true,
		eq:        true,
		order:     false,
		matchArgs: true,
		kwOnly:    false,
	}
}

func (f *dataclassFlags) applyKwargs(kwargs map[string]objects.Object) error {
	for k, v := range kwargs {
		b := objects.IsTrue(v)
		switch k {
		case "init":
			f.init = b
		case "repr":
			f.repr = b
		case "eq":
			f.eq = b
		case "order":
			f.order = b
		case "unsafe_hash":
			f.unsafeHash = b
		case "frozen":
			f.frozen = b
		case "match_args":
			f.matchArgs = b
		case "kw_only":
			f.kwOnly = b
		case "slots":
			f.slots = b
		case "weakref_slot":
			f.weakrefSlot = b
		default:
			return fmt.Errorf("TypeError: dataclass() got an unexpected keyword argument %q", k)
		}
	}
	return nil
}

// fieldInfo bundles a Field with its bookkeeping data for codegen.
type fieldInfo struct {
	name        string
	f           *fieldObj
	classDefVal objects.Object // value present on the class before decoration; nil if absent
}

// dataclassBuiltin handles both @dataclass and @dataclass(...) forms.
//
// CPython: Lib/dataclasses.py:1422 dataclass
func dataclassBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	flags := defaultFlags()
	if err := flags.applyKwargs(kwargs); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		// @dataclass(**kwargs): return a decorator that closes over flags.
		dec := objects.NewBuiltinFunction("dataclass", func(decArgs []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(decArgs) != 1 {
				return nil, fmt.Errorf("TypeError: dataclass() decorator expects exactly one class")
			}
			t, ok := decArgs[0].(*objects.Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: dataclass() expects a class, not %s", decArgs[0].Type().Name)
			}
			return processClass(t, flags)
		})
		return dec, nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: dataclass() takes 0 or 1 positional arguments")
	}
	t, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: dataclass() expects a class, not %s", args[0].Type().Name)
	}
	return processClass(t, flags)
}

// processClass is the heart of the decorator. Mirrors _process_class.
//
// CPython: Lib/dataclasses.py:986 _process_class
func processClass(cls *objects.Type, flags dataclassFlags) (*objects.Type, error) {
	fields := collectFields(cls, flags)

	if flags.init {
		initFn := makeInit(cls, fields, flags)
		objects.SetTypeDescr(cls, "__init__", initFn)
	}

	if flags.repr {
		reprFn := makeRepr(cls, fields)
		objects.SetTypeDescr(cls, "__repr__", reprFn)
		cls.Repr = func(o objects.Object) (string, error) {
			return instanceRepr(o, cls, fields)
		}
		cls.Str = cls.Repr
	}

	if flags.eq {
		eqFn := makeEq(cls, fields)
		objects.SetTypeDescr(cls, "__eq__", eqFn)
		cls.RichCmp = func(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
			if op != objects.CompareEQ && op != objects.CompareNE {
				return objects.NotImplemented(), nil
			}
			eq, err := instanceEq(a, b, fields)
			if err != nil {
				return nil, err
			}
			if op == objects.CompareNE {
				return objects.NewBool(!eq), nil
			}
			return objects.NewBool(eq), nil
		}
	}

	if flags.frozen {
		installFrozen(cls)
	}

	// Stash field metadata so fields() / is_dataclass / replace see it.
	fieldsDict := objects.NewDict()
	for _, fi := range fields {
		if err := fieldsDict.SetItem(objects.NewStr(fi.name), fi.f); err != nil {
			return nil, err
		}
	}
	objects.SetTypeDescr(cls, fieldsAttr, fieldsDict)

	// __dataclass_params__ for introspection.
	p := &dataclassParams{
		init:        flags.init,
		repr:        flags.repr,
		eq:          flags.eq,
		order:       flags.order,
		unsafeHash:  flags.unsafeHash,
		frozen:      flags.frozen,
		matchArgs:   flags.matchArgs,
		kwOnly:      flags.kwOnly,
		slots:       flags.slots,
		weakrefSlot: flags.weakrefSlot,
	}
	p.Init(paramsType)
	objects.SetTypeDescr(cls, paramsAttr, p)

	if flags.matchArgs {
		// Tuple of init-by-position field names.
		items := []objects.Object{}
		for _, fi := range fields {
			if fi.f.init && !objects.IsTrue(fi.f.kwOnly) {
				items = append(items, objects.NewStr(fi.name))
			}
		}
		objects.SetTypeDescr(cls, matchArgsAttr, objects.NewTuple(items))
	}

	// Install __replace__ so copy.replace(obj, **changes) works on this class.
	// CPython: Lib/dataclasses.py:1150 _set_new_attribute(cls, '__replace__', _replace)
	objects.SetTypeDescr(cls, "__replace__", objects.NewMethodDescr(cls, "__replace__", replaceBuiltin))

	return cls, nil
}

// collectFields walks the class namespace to identify dataclass fields.
// Field names come from cls.__annotations__ (populated by
// SETUP_ANNOTATIONS at class scope); defaults are read from the class
// dict, with explicit Field() instances taking precedence over bare
// values. CPython's _process_class does the same iteration over
// cls.__dict__.get('__annotations__', {}).
//
// CPython: Lib/dataclasses.py:1041 _process_class field collection
//
//nolint:gocognit,gocyclo // mirrors CPython's monolithic _process_class
func collectFields(cls *objects.Type, flags dataclassFlags) []*fieldInfo {
	own := objects.TypeOwnDescrs(cls)
	// Inherit fields from base dataclasses, in MRO reverse order.
	inherited := map[string]*fieldObj{}
	inheritedOrder := []string{}
	for i := len(cls.MRO) - 1; i >= 0; i-- {
		base := cls.MRO[i]
		if base == cls {
			continue
		}
		bf, _ := objects.LookupDescriptor(base, fieldsAttr)
		if bf == nil {
			continue
		}
		bd, ok := bf.(*objects.Dict)
		if !ok {
			continue
		}
		for _, key := range bd.Keys() {
			nameStr, ok := key.(*objects.Unicode)
			if !ok {
				continue
			}
			val, err := bd.GetItem(key)
			if err != nil {
				continue
			}
			fo, ok := val.(*fieldObj)
			if !ok {
				continue
			}
			if _, seen := inherited[nameStr.Value()]; !seen {
				inheritedOrder = append(inheritedOrder, nameStr.Value())
			}
			inherited[nameStr.Value()] = fo
		}
	}

	// Own fields: keys of cls.__annotations__, in source order. Bare
	// annotated names without defaults appear here too; the class dict
	// only carries a value for names that had an "= default" clause.
	ownFieldNames := []string{}
	ownFieldDefaults := map[string]objects.Object{}
	var annObj objects.Object
	if v, ok := own["__annotations__"]; ok {
		annObj = v
	} else if v, err := objects.GetAttr(cls, objects.NewStr("__annotations__")); err == nil {
		// PEP 649: class annotations live behind __annotate__. typeGetAttr
		// materializes the dict on first read and caches it back on the
		// type, so subsequent collectFields calls hit the own[] branch.
		annObj = v
	}
	if annObj != nil {
		if annDict, ok := annObj.(*objects.Dict); ok {
			for _, key := range annDict.Keys() {
				nameStr, ok := key.(*objects.Unicode)
				if !ok {
					continue
				}
				name := nameStr.Value()
				ownFieldNames = append(ownFieldNames, name)
				if val, present := own[name]; present {
					ownFieldDefaults[name] = val
				}
			}
		}
	}
	// Explicit Field() instances assigned without an annotation still
	// count, matching CPython's check for `_FIELDS`-typed values on
	// class scan. Sort these for deterministic placement after the
	// annotation-driven names.
	extraFieldOnly := []string{}
	for name, val := range own {
		if isDunder(name) {
			continue
		}
		if _, already := ownFieldDefaults[name]; already {
			continue
		}
		if _, seen := indexOfName(ownFieldNames, name); seen {
			continue
		}
		if _, isField := val.(*fieldObj); isField {
			extraFieldOnly = append(extraFieldOnly, name)
			ownFieldDefaults[name] = val
		}
	}
	sort.Strings(extraFieldOnly)
	ownFieldNames = append(ownFieldNames, extraFieldOnly...)

	// Merge: inherited fields first (preserving order), then own fields
	// not already in inherited (sorted for determinism).
	allNames := []string{}
	seen := map[string]bool{}
	for _, n := range inheritedOrder {
		if !seen[n] {
			seen[n] = true
			allNames = append(allNames, n)
		}
	}
	for _, n := range ownFieldNames {
		if !seen[n] {
			seen[n] = true
			allNames = append(allNames, n)
		}
	}

	// ownNamesSet tracks names that came from cls.__annotations__ so we
	// can promote bare annotations (no default) into a field with
	// MISSING defaults.
	ownNamesSet := map[string]bool{}
	for _, n := range ownFieldNames {
		ownNamesSet[n] = true
	}

	out := []*fieldInfo{}
	for _, name := range allNames {
		var fo *fieldObj
		var classDef objects.Object
		if ownDef, ok := ownFieldDefaults[name]; ok {
			if existing, isField := ownDef.(*fieldObj); isField {
				fo = existing
				classDef = nil
			} else {
				fo = newField(ownDef, missingValue, true, true, objects.None(), true, nil, missingValue, objects.None())
				classDef = ownDef
			}
		} else if base, ok := inherited[name]; ok {
			fo = base
			classDef = nil
		} else if ownNamesSet[name] {
			// Bare annotation: no default, no inherited field.
			fo = newField(missingValue, missingValue, true, true, objects.None(), true, nil, missingValue, objects.None())
			classDef = nil
		} else {
			continue
		}
		fo.name = name
		// Resolve kw_only: explicit field setting beats class-level
		// flags.kwOnly default.
		if fo.kwOnly == missingValue {
			fo.kwOnly = objects.NewBool(flags.kwOnly)
		}
		// Drop the class-level slot value: instances should not see a
		// dangling class-level default that masks the per-instance
		// attribute, and Field() sentinels must never resolve as the
		// final value.
		if classDef != nil {
			// Keep simple non-mutable defaults on the class as
			// CPython does (after rebinding them). Field() sentinels
			// are handled by the own-name branch below.
			_ = classDef
		} else if _, hasOwn := own[name]; hasOwn {
			if _, isField := own[name].(*fieldObj); isField {
				objects.DelTypeDescr(cls, name)
			}
		}
		out = append(out, &fieldInfo{name: name, f: fo, classDefVal: classDef})
	}

	return out
}

func isDunder(name string) bool {
	return strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__")
}

// indexOfName returns (i, true) when name is in names, else (-1, false).
func indexOfName(names []string, name string) (int, bool) {
	for i, n := range names {
		if n == name {
			return i, true
		}
	}
	return -1, false
}

// makeInit returns a MethodDescr that, when called as a bound method,
// installs each field on the instance.
//
// CPython: Lib/dataclasses.py:670 _init_fn
//
//nolint:gocognit,gocyclo,gocritic // mirrors CPython's _init_fn body
func makeInit(cls *objects.Type, fields []*fieldInfo, flags dataclassFlags) *objects.MethodDescr {
	posFields := []*fieldInfo{}
	kwOnlyFields := []*fieldInfo{}
	for _, fi := range fields {
		if !fi.f.init {
			continue
		}
		if objects.IsTrue(fi.f.kwOnly) {
			kwOnlyFields = append(kwOnlyFields, fi)
		} else {
			posFields = append(posFields, fi)
		}
	}
	// Sentinel: a non-default field must not follow a defaulted one.
	hasDefault := false
	for _, fi := range posFields {
		hasOwn := fi.f.defaultVal != missingValue || fi.f.defaultFactory != missingValue
		if hasOwn {
			hasDefault = true
		} else if hasDefault {
			return objects.NewMethodDescr(cls, "__init__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
				return nil, fmt.Errorf("TypeError: non-default argument %q follows default argument", fi.name)
			})
		}
	}

	init := func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __init__() missing 1 required positional argument: 'self'")
		}
		self := args[0]
		posArgs := args[1:]
		if len(posArgs) > len(posFields) {
			return nil, fmt.Errorf("TypeError: __init__() takes %d positional arguments but %d were given", len(posFields)+1, len(args))
		}
		// Track which fields are assigned by positional argument.
		consumed := map[string]bool{}
		// Positional fields.
		for i, fi := range posFields {
			var val objects.Object
			if i < len(posArgs) {
				val = posArgs[i]
				consumed[fi.name] = true
			} else if kv, ok := kwargs[fi.name]; ok {
				val = kv
				consumed[fi.name] = true
				delete(kwargs, fi.name)
			} else if fi.f.defaultVal != missingValue {
				val = fi.f.defaultVal
			} else if fi.f.defaultFactory != missingValue {
				v, err := objects.CallNoArgs(fi.f.defaultFactory)
				if err != nil {
					return nil, err
				}
				val = v
			} else {
				return nil, fmt.Errorf("TypeError: __init__() missing required argument %q", fi.name)
			}
			if err := setField(self, fi.name, val, flags.frozen); err != nil {
				return nil, err
			}
		}
		// Reject extra positional that didn't match a field name.
		if len(posArgs) > len(posFields) {
			return nil, fmt.Errorf("TypeError: __init__() takes %d positional arguments but %d were given", len(posFields)+1, len(args))
		}
		// kw-only fields.
		for _, fi := range kwOnlyFields {
			var val objects.Object
			if kv, ok := kwargs[fi.name]; ok {
				val = kv
				delete(kwargs, fi.name)
			} else if fi.f.defaultVal != missingValue {
				val = fi.f.defaultVal
			} else if fi.f.defaultFactory != missingValue {
				v, err := objects.CallNoArgs(fi.f.defaultFactory)
				if err != nil {
					return nil, err
				}
				val = v
			} else {
				return nil, fmt.Errorf("TypeError: __init__() missing required keyword argument %q", fi.name)
			}
			if err := setField(self, fi.name, val, flags.frozen); err != nil {
				return nil, err
			}
		}
		if len(kwargs) > 0 {
			for k := range kwargs {
				return nil, fmt.Errorf("TypeError: __init__() got an unexpected keyword argument %q", k)
			}
		}
		// Call __post_init__ if present.
		if pi, _ := objects.LookupDescriptor(cls, postInitName); pi != nil {
			bound, err := bindMethod(pi, self, cls)
			if err != nil {
				return nil, err
			}
			if _, err := objects.CallNoArgs(bound); err != nil {
				return nil, err
			}
		}
		return objects.None(), nil
	}
	return objects.NewMethodDescr(cls, "__init__", init)
}

// setField writes value into self.name, going around the frozen guard
// when flags.frozen is set. CPython's _field_init uses
// object.__setattr__ for the same reason.
//
// CPython: Lib/dataclasses.py:579 _field_assign
func setField(self objects.Object, name string, value objects.Object, frozen bool) error {
	if frozen {
		// Frozen guard hooks Setattro on the type, so we have to write
		// straight into the instance __dict__. user types created via
		// NewUserType always carry HasDict, so EnsureDict materializes
		// the LAZY_DICT shape on first store and returns the existing
		// dict in the INLINE_VALUES shape.
		if inst, ok := self.(*objects.Instance); ok {
			if d := inst.EnsureDict(); d != nil {
				// The LOAD_ATTR specializer's NONDESCRIPTOR_WITH_VALUES
				// arm gates on tp.HasCachedKey(name): if the name has
				// never been stored on any instance, it caches the class
				// descriptor as the answer. Direct dict writes skip the
				// instanceSetAttr / GenericSetAttr hooks that normally
				// grow cachedKeys, so we have to call AddCachedKey here
				// too. Otherwise frozen dataclass fields with class-level
				// defaults (e.g. _colorize.Traceback) read back the
				// default after the first specialized hit.
				//
				// CPython: Objects/dictobject.c:5132 insert_split_key
				inst.Type().AddCachedKey(name)
				return d.SetItem(objects.NewStr(name), value)
			}
		}
	}
	return objects.SetAttr(self, objects.NewStr(name), value)
}

// instanceRepr renders an instance as ClassName(field1=val1, field2=val2).
//
// CPython: Lib/dataclasses.py:943 _hash_add / repr generation in _process_class
func instanceRepr(self objects.Object, cls *objects.Type, fields []*fieldInfo) (string, error) {
	var b strings.Builder
	b.WriteString(cls.Name)
	b.WriteString("(")
	first := true
	for _, fi := range fields {
		if !fi.f.repr {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(fi.name)
		b.WriteString("=")
		val, err := objects.GetAttr(self, objects.NewStr(fi.name))
		if err != nil {
			return "", err
		}
		r, err := objects.Repr(val)
		if err != nil {
			return "", err
		}
		b.WriteString(r)
	}
	b.WriteString(")")
	return b.String(), nil
}

func makeRepr(cls *objects.Type, fields []*fieldInfo) *objects.MethodDescr {
	return objects.NewMethodDescr(cls, "__repr__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __repr__() missing self")
		}
		s, err := instanceRepr(args[0], cls, fields)
		if err != nil {
			return nil, err
		}
		return objects.NewStr(s), nil
	})
}

func instanceEq(a, b objects.Object, fields []*fieldInfo) (bool, error) {
	if a.Type() != b.Type() {
		return false, nil
	}
	for _, fi := range fields {
		if !fi.f.compare {
			continue
		}
		va, err := objects.GetAttr(a, objects.NewStr(fi.name))
		if err != nil {
			return false, err
		}
		vb, err := objects.GetAttr(b, objects.NewStr(fi.name))
		if err != nil {
			return false, err
		}
		eq, err := objects.RichCmp(va, vb, objects.CompareEQ)
		if err != nil {
			return false, err
		}
		if !objects.IsTrue(eq) {
			return false, nil
		}
	}
	return true, nil
}

func makeEq(cls *objects.Type, fields []*fieldInfo) *objects.MethodDescr {
	return objects.NewMethodDescr(cls, "__eq__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 2 {
			return objects.NotImplemented(), nil
		}
		eq, err := instanceEq(args[0], args[1], fields)
		if err != nil {
			return nil, err
		}
		return objects.NewBool(eq), nil
	})
}

// installFrozen wraps the type's Setattro / DelAttro to raise
// FrozenInstanceError on any post-construction write.
//
// CPython: Lib/dataclasses.py:728 _frozen_set_del_attr
func installFrozen(cls *objects.Type) {
	prevSet := cls.Setattro
	cls.Setattro = func(o objects.Object, name objects.Object, value objects.Object) error {
		ns, err := objects.Str(name)
		if err != nil {
			return err
		}
		return fmt.Errorf("FrozenInstanceError: cannot assign to field %q", ns)
	}
	_ = prevSet // retained for documentation; instance __init__ writes through the dict directly.
}

// bindMethod runs the descriptor protocol so we can call a method
// looked up on the class with the instance as the receiver.
func bindMethod(descr objects.Object, owner objects.Object, ownerType *objects.Type) (objects.Object, error) {
	dt := descr.Type()
	if dt.DescrGet != nil {
		return dt.DescrGet(descr, owner, ownerType)
	}
	return descr, nil
}

// fields returns the tuple of Field instances for a dataclass.
//
// CPython: Lib/dataclasses.py:1453 fields
func fieldsBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: fields() takes 1 positional argument but %d were given", len(args))
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		cls = args[0].Type()
	}
	d, _ := objects.LookupDescriptor(cls, fieldsAttr)
	if d == nil {
		return nil, fmt.Errorf("TypeError: %s is not a dataclass", cls.Name)
	}
	dd, ok := d.(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: %s.__dataclass_fields__ is not a dict", cls.Name)
	}
	items := []objects.Object{}
	for _, key := range dd.Keys() {
		v, err := dd.GetItem(key)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return objects.NewTuple(items), nil
}

// is_dataclass checks for the __dataclass_fields__ marker.
//
// CPython: Lib/dataclasses.py:1476 is_dataclass
func isDataclassBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: is_dataclass() takes 1 positional argument but %d were given", len(args))
	}
	cls, ok := args[0].(*objects.Type)
	if !ok {
		cls = args[0].Type()
	}
	d, _ := objects.LookupDescriptor(cls, fieldsAttr)
	return objects.NewBool(d != nil), nil
}

// asdict converts a dataclass instance to a dict.
//
// CPython: Lib/dataclasses.py:1483 asdict
func asdictBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: asdict() takes 1 positional argument")
	}
	obj := args[0]
	cls := obj.Type()
	d, _ := objects.LookupDescriptor(cls, fieldsAttr)
	if d == nil {
		return nil, fmt.Errorf("TypeError: asdict() should be called on dataclass instances")
	}
	dd := d.(*objects.Dict)
	out := objects.NewDict()
	for _, key := range dd.Keys() {
		s := key.(*objects.Unicode)
		v, err := objects.GetAttr(obj, objects.NewStr(s.Value()))
		if err != nil {
			return nil, err
		}
		if err := out.SetItem(key, v); err != nil {
			return nil, err
		}
	}
	_ = kwargs // dict_factory not supported; matches the documented surface for the common case.
	return out, nil
}

// astuple converts a dataclass instance to a tuple in field order.
//
// CPython: Lib/dataclasses.py:1575 astuple
func astupleBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: astuple() takes 1 positional argument")
	}
	obj := args[0]
	cls := obj.Type()
	d, _ := objects.LookupDescriptor(cls, fieldsAttr)
	if d == nil {
		return nil, fmt.Errorf("TypeError: astuple() should be called on dataclass instances")
	}
	dd := d.(*objects.Dict)
	items := []objects.Object{}
	for _, key := range dd.Keys() {
		s := key.(*objects.Unicode)
		v, err := objects.GetAttr(obj, objects.NewStr(s.Value()))
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	_ = kwargs
	return objects.NewTuple(items), nil
}

// replace produces a copy of obj with the supplied keyword overrides.
//
// CPython: Lib/dataclasses.py:1762 replace
func replaceBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: replace() takes 1 positional argument")
	}
	obj := args[0]
	cls := obj.Type()
	d, _ := objects.LookupDescriptor(cls, fieldsAttr)
	if d == nil {
		return nil, fmt.Errorf("TypeError: replace() should be called on dataclass instances")
	}
	dd := d.(*objects.Dict)
	// Fill missing fields from obj.
	callKwargs := map[string]objects.Object{}
	for _, key := range dd.Keys() {
		s := key.(*objects.Unicode)
		name := s.Value()
		fv, err := dd.GetItem(key)
		if err != nil {
			return nil, err
		}
		fo := fv.(*fieldObj)
		if !fo.init {
			continue
		}
		if v, ok := kwargs[name]; ok {
			callKwargs[name] = v
			delete(kwargs, name)
			continue
		}
		cv, err := objects.GetAttr(obj, objects.NewStr(name))
		if err != nil {
			return nil, err
		}
		callKwargs[name] = cv
	}
	if len(kwargs) > 0 {
		for k := range kwargs {
			return nil, fmt.Errorf("TypeError: replace() got an unexpected keyword argument %q", k)
		}
	}
	return objects.Call(cls, objects.NewTuple(nil), kwargsAsDict(callKwargs))
}

func kwargsAsDict(kwargs map[string]objects.Object) *objects.Dict {
	if len(kwargs) == 0 {
		return nil
	}
	d := objects.NewDict()
	for k, v := range kwargs {
		_ = d.SetItem(objects.NewStr(k), v)
	}
	return d
}

// makeDataclassBuiltin dynamically constructs a dataclass from a name,
// a field list, and the same flag kwargs accepted by @dataclass(). The
// fields argument is iterable of (name) | (name, type) | (name, type,
// Field()) items; bare-name entries get the string 'typing.Any' as the
// annotation. Class body is composed from the optional namespace dict
// plus the per-field defaults, the __annotations__ dict is built in
// field order, and an optional module kwarg sets __module__ for
// pickling. The resulting class is then run through the normal
// dataclass decorator with the remaining flag kwargs.
//
// CPython: Lib/dataclasses.py:1635 make_dataclass
func makeDataclassBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: make_dataclass() missing required arguments: 'cls_name' and 'fields'")
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: make_dataclass() takes 2 positional arguments but %d were given", len(args))
	}
	nameObj, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: make_dataclass() cls_name must be str, not %s", args[0].Type().Name)
	}
	clsName := nameObj.Value()

	// Pull non-dataclass kwargs out before forwarding the rest to the
	// decorator. Order matches CPython's signature.
	bases, err := makeDataclassBases(kwargs)
	if err != nil {
		return nil, err
	}
	userNS, err := makeDataclassNamespace(kwargs)
	if err != nil {
		return nil, err
	}
	moduleName, hasModule, err := makeDataclassModule(kwargs)
	if err != nil {
		return nil, err
	}
	decorator, hasDecorator := kwargs["decorator"]
	delete(kwargs, "decorator")

	entries, err := makeDataclassParseFields(args[1])
	if err != nil {
		return nil, err
	}

	ns, err := makeDataclassBuildNS(userNS, entries, moduleName, hasModule)
	if err != nil {
		return nil, err
	}
	cls := objects.NewUserTypeKwargs(clsName, bases, ns, nil)
	if hasDecorator && !objects.IsNone(decorator) {
		return objects.Call(decorator, objects.NewTuple([]objects.Object{cls}), kwargsAsDict(kwargs))
	}
	return dataclassBuiltin([]objects.Object{cls}, kwargs)
}

func makeDataclassBuildNS(userNS *objects.Dict, entries []makeDataclassField, moduleName string, hasModule bool) (*objects.Dict, error) {
	ns := objects.NewDict()
	if userNS != nil {
		for _, k := range userNS.Keys() {
			v, err := userNS.GetItem(k)
			if err != nil {
				return nil, err
			}
			if err := ns.SetItem(k, v); err != nil {
				return nil, err
			}
		}
	}
	annotations := objects.NewDict()
	for _, e := range entries {
		if err := annotations.SetItem(objects.NewStr(e.name), e.tp); err != nil {
			return nil, err
		}
		if e.hasDef {
			if err := ns.SetItem(objects.NewStr(e.name), e.defVal); err != nil {
				return nil, err
			}
		}
	}
	if err := ns.SetItem(objects.NewStr("__annotations__"), annotations); err != nil {
		return nil, err
	}
	if hasModule {
		if err := ns.SetItem(objects.NewStr("__module__"), objects.NewStr(moduleName)); err != nil {
			return nil, err
		}
	}
	return ns, nil
}

type makeDataclassField struct {
	name   string
	tp     objects.Object
	hasDef bool
	defVal objects.Object
}

func makeDataclassBases(kwargs map[string]objects.Object) ([]*objects.Type, error) {
	v, ok := kwargs["bases"]
	if !ok {
		return nil, nil
	}
	delete(kwargs, "bases")
	tup, ok := v.(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: make_dataclass() bases must be tuple, not %s", v.Type().Name)
	}
	out := make([]*objects.Type, 0, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		bt, ok := tup.Item(i).(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: make_dataclass() bases must be a tuple of types")
		}
		out = append(out, bt)
	}
	return out, nil
}

func makeDataclassNamespace(kwargs map[string]objects.Object) (*objects.Dict, error) {
	v, ok := kwargs["namespace"]
	if !ok {
		return nil, nil
	}
	delete(kwargs, "namespace")
	if objects.IsNone(v) {
		return nil, nil
	}
	d, ok := v.(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: make_dataclass() namespace must be dict, not %s", v.Type().Name)
	}
	return d, nil
}

func makeDataclassModule(kwargs map[string]objects.Object) (string, bool, error) {
	v, ok := kwargs["module"]
	if !ok {
		return "", false, nil
	}
	delete(kwargs, "module")
	if objects.IsNone(v) {
		return "", false, nil
	}
	s, ok := v.(*objects.Unicode)
	if !ok {
		return "", false, fmt.Errorf("TypeError: make_dataclass() module must be str, not %s", v.Type().Name)
	}
	return s.Value(), true, nil
}

func makeDataclassParseFields(seq objects.Object) ([]makeDataclassField, error) {
	items, err := objects.SequenceList(seq)
	if err != nil {
		return nil, err
	}
	out := make([]makeDataclassField, 0, items.Len())
	seen := map[string]bool{}
	for i := 0; i < items.Len(); i++ {
		entry, err := makeDataclassParseOne(items.Item(i))
		if err != nil {
			return nil, err
		}
		if !objects.StrIsIdentifier(entry.name) {
			return nil, fmt.Errorf("TypeError: Field names must be valid identifiers: %q", entry.name)
		}
		if isPythonKeyword(entry.name) {
			return nil, fmt.Errorf("TypeError: Field names must not be keywords: %q", entry.name)
		}
		if seen[entry.name] {
			return nil, fmt.Errorf("TypeError: Field name duplicated: %q", entry.name)
		}
		seen[entry.name] = true
		out = append(out, entry)
	}
	return out, nil
}

func makeDataclassParseOne(item objects.Object) (makeDataclassField, error) {
	if s, ok := item.(*objects.Unicode); ok {
		return makeDataclassField{name: s.Value(), tp: objects.NewStr("typing.Any")}, nil
	}
	tup, ok := item.(*objects.Tuple)
	if !ok {
		if list, isList := item.(*objects.List); isList {
			coll := make([]objects.Object, 0, list.Len())
			for j := 0; j < list.Len(); j++ {
				coll = append(coll, list.Item(j))
			}
			tup = objects.NewTuple(coll)
		} else {
			return makeDataclassField{}, fmt.Errorf("TypeError: Invalid field: %s", item.Type().Name)
		}
	}
	switch tup.Len() {
	case 2:
		ns, ok := tup.Item(0).(*objects.Unicode)
		if !ok {
			return makeDataclassField{}, fmt.Errorf("TypeError: Field names must be valid identifiers: %s", tup.Item(0).Type().Name)
		}
		return makeDataclassField{name: ns.Value(), tp: tup.Item(1)}, nil
	case 3:
		ns, ok := tup.Item(0).(*objects.Unicode)
		if !ok {
			return makeDataclassField{}, fmt.Errorf("TypeError: Field names must be valid identifiers: %s", tup.Item(0).Type().Name)
		}
		return makeDataclassField{name: ns.Value(), tp: tup.Item(1), hasDef: true, defVal: tup.Item(2)}, nil
	default:
		return makeDataclassField{}, fmt.Errorf("TypeError: Invalid field tuple length: %d", tup.Len())
	}
}

// isPythonKeyword matches keyword.iskeyword for the Python keyword set
// (Lib/keyword.py kwlist). CPython rejects keywords as dataclass field
// names because they would be syntactically invalid as identifiers in
// the generated __init__ signature.
//
// CPython: Lib/keyword.py kwlist (regenerated from Grammar/python.gram)
func isPythonKeyword(name string) bool {
	switch name {
	case "False", "None", "True",
		"and", "as", "assert", "async", "await",
		"break", "class", "continue", "def", "del",
		"elif", "else", "except", "finally", "for", "from",
		"global", "if", "import", "in", "is",
		"lambda", "nonlocal", "not", "or", "pass",
		"raise", "return", "try", "while", "with", "yield":
		return true
	}
	return false
}

func init() {
	_ = imp.AppendInittab("dataclasses", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("dataclasses")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"dataclass", objects.NewBuiltinFunction("dataclass", dataclassBuiltin)},
		{"field", objects.NewBuiltinFunction("field", fieldBuiltin)},
		{"Field", FieldType},
		{"fields", objects.NewBuiltinFunction("fields", fieldsBuiltin)},
		{"is_dataclass", objects.NewBuiltinFunction("is_dataclass", isDataclassBuiltin)},
		{"asdict", objects.NewBuiltinFunction("asdict", asdictBuiltin)},
		{"astuple", objects.NewBuiltinFunction("astuple", astupleBuiltin)},
		{"replace", objects.NewBuiltinFunction("replace", replaceBuiltin)},
		{"make_dataclass", objects.NewBuiltinFunction("make_dataclass", makeDataclassBuiltin)},
		{"MISSING", missingValue},
		{"KW_ONLY", kwOnlyValue},
		{"InitVar", InitVarType},
		{"FrozenInstanceError", FrozenInstanceError},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}
