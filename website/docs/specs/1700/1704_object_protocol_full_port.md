---
format: md
id: 1704_object_protocol_full_port
title: "1704. gopy object / type / class-callable full-file ports"
sidebar_label: "1704. object_protocol_full_port"
sidebar_position: 1704
slug: /specs/1704-object_protocol_full_port
description: "One-time port of every CPython source file that defines the runtime object protocol: Objects/object.c, Objects/typeobject.c, Objects/classobject.c, Objects/funcobject.c, plus Python/ceval.c name ops. Every function in each file lands with a citation. After this spec we never revisit these files. Closes #544 enum import and unblocks #543 re, #542 fnmatch."
---


## Rule

Every CPython source file in scope is ported **in full**. No
function in those files may be left unported. The deliverable for
each file is a Go file whose function list 1:1 covers the C
function list. Once this spec lands we never come back to these
files for a missing slot.

## Files in scope

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Objects/object.c` | ~2,600 | `objects/object.go` + `objects/object_proto.go` | partial |
| B | `Objects/typeobject.c` | ~12,000 | `objects/type.go` + `objects/usertype.go` + `objects/type_call.go` + `objects/type_inherit.go` | partial |
| C | `Objects/classobject.c` | ~620 | `objects/method.go` (BoundMethod block) | partial |
| D | `Objects/funcobject.c` | ~1,260 | `objects/function.go` + `objects/method.go` (classmethod, staticmethod blocks) | partial |
| E | `Python/ceval.c` name ops | ~80 | `vm/eval_simple.go` (storeIn / lookupIn / deleteIn) | partial |
| F | `Include/cpython/classobject.h` | 30 | `objects/method.go` struct decl | done |
| G | `Include/cpython/funcobject.h` | various | `objects/function.go` + `objects/method.go` struct decls | done |

Sources of truth live under `/Users/apple/cpython-314/`.

## Phase index

Each phase ports one file (or one disjoint block of a file) end
to end. Phases are independent unless the **Blocks** column says
otherwise. Final gate closes #544.

| Phase | File | Block | Blocks | Status |
|-------|------|-------|--------|--------|
| 1 | A `object.c` | `object_methods` + `object_getsets` + `object_*` impls | - | done |
| 2 | D `funcobject.c` | PyClassMethod_Type (all `cm_*` functions) | - | done |
| 3 | D `funcobject.c` | PyStaticMethod_Type (all `sm_*` functions) | - | partial |
| 4 | D `funcobject.c` | PyFunction_Type (all `func_*` functions) | - | partial |
| 5 | C `classobject.c` | PyMethod_Type (all `method_*` functions) | - | partial |
| 6 | B `typeobject.c` | `type_new` pipeline (`type_new_*` functions) | 1,2,3,4,5 | partial |
| 7 | B `typeobject.c` | `inherit_slots` (every slot edge) | 6 | partial |
| 8 | E `ceval.c` | STORE_NAME / LOAD_NAME / DELETE_NAME | - | partial |
| Gate | - | enum + re + fnmatch smoke | all | pending |

## Phase 1 - `Objects/object.c` full port

### Functions to port

CPython lists these in `object_methods` (around line 6048) and
`object_getsets` (around line 6075), plus the supporting `object_*`
implementations earlier in the file. Every row must land.

| C function | Exposed as | gopy hook | Status |
|------------|-----------|-----------|--------|
| `object_new` | `object.__new__` | `SetTypeDescr(objectType, "__new__", ...)` | done |
| `object_init` | `object.__init__` | `SetTypeDescr(objectType, "__init__", ...)` | done |
| `object_repr` | `object.__repr__` | `SetTypeDescr(objectType, "__repr__", ...)` | done |
| `object_str` | `object.__str__` | `SetTypeDescr(objectType, "__str__", ...)` | done |
| `object_richcompare` | `__eq__`, `__ne__`, `__lt__`, `__le__`, `__gt__`, `__ge__` | six entries on `objectType` | done |
| `object_get_class` / `object_set_class` | `object.__class__` getset | `NewGetSetDescr("__class__", ...)` | done (setter rejects with TypeError; full compat check deferred) |
| `object_getattro` | `object.__getattribute__` | already wired as `GenericGetAttr` (verify) | done |
| `object_setattro` | `object.__setattr__` / `__delattr__` | already wired (verify) | done |
| `object_hash` | `object.__hash__` | `SetTypeDescr(objectType, "__hash__", ...)` | done |
| `object_format` | `object.__format__` | `SetTypeDescr(objectType, "__format__", ...)` | done |
| `object_sizeof` | `object.__sizeof__` | `SetTypeDescr(objectType, "__sizeof__", ...)` | done (uses reflect.Size; CPython tp_basicsize equivalent deferred) |
| `object___dir__` | `object.__dir__` | `SetTypeDescr(objectType, "__dir__", ...)` | done |
| `object___reduce__` | `object.__reduce__` | `SetTypeDescr(objectType, "__reduce__", ...)` | done (forwards to `__reduce_ex__(2)`; pickle deferred) |
| `object___reduce_ex__` | `object.__reduce_ex__` | `SetTypeDescr(objectType, "__reduce_ex__", ...)` | done (override detection + fallback TypeError; copyreg deferred) |
| `object___getstate__` | `object.__getstate__` | `SetTypeDescr(objectType, "__getstate__", ...)` | done (simple case; `__slotnames__` deferred) |
| `object___subclasshook__` | `object.__subclasshook__` (classmethod) | wrap in `NewClassMethod` | done |
| `object___init_subclass__` | `object.__init_subclass__` (classmethod) | wrap in `NewClassMethod` | done |
| `object_get_dict` / `object_set_dict` | `object.__dict__` getset | `NewGetSetDescr("__dict__", ...)` | done (getter; setter deferred to type_new dict layout work) |
| `object___class_getitem__` | `object.__class_getitem__` (classmethod) | `SetTypeDescr` + classmethod | deferred (not in CPython 3.14 `object_methods`; lives on subclasses) |

### Gates

| Gate | Command | Expected | Status |
|------|---------|----------|--------|
| 1.1 | `gopy -c 'print(object.__repr__)'` | repr-of-method-descriptor | pass |
| 1.2 | `gopy -c 'class C: pass; print(C.__class__ is type)'` | `True` | pass |
| 1.3 | `gopy -c 'class C: pass; print(object.__new__(C).__class__ is C)'` | `True` | pass |
| 1.4 | `gopy -c 'class C: pass; print(sorted(dir(C())) == EXPECTED_LIST)'` | `True` | pass (dir produces canonical set) |
| 1.5 | `gopy -c 'print(hash(object.__hash__))'` | runs without exception | pass |
| 1.6 | `gopy -c 'class C: pass; print(C().__class__)'` | `<class '__main__.C'>` | pass |
| 1.7 | `gopy -c 'object.__format__(42, "")'` | `'42'` | pass |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/object.c:6048` `object_methods` table |
| 2 | `Objects/object.c:6075` `object_getsets` table |
| 3 | `Objects/object.c:5800` `object___subclasshook__` |
| 4 | `Objects/object.c:5750` `object___init_subclass__` |
| 5 | `Objects/object.c:766` `object_format` |
| 6 | `Objects/object.c:884` `object_richcompare` |
| 7 | `Objects/object.c:1146` `object_get_class` / `object_set_class` |
| 8 | `Objects/object.c:1462` `object_get_dict` / `object_set_dict` |
| 9 | `Objects/object.c:5900` `object___getstate__` |

## Phase 2 - `Objects/funcobject.c` classmethod block

### Functions to port

| C function | Surface | Status |
|------------|---------|--------|
| `cm_init` | `classmethod(fn)` constructor + `functools_wraps` | done |
| `cm_descr_get` | `__get__(obj, type)` returning BoundMethod(fn, type) | done |
| `cm_traverse` | GC visit `cm_callable` and `cm_dict` | done |
| `cm_dealloc` | (Go has GC, no-op) | n/a |
| `cm_clear` | (Go has GC, no-op) | n/a |
| `cm_repr` | `<classmethod(REPR_OF_CALLABLE)>` text | done |
| `cm_memberlist` | `__func__`, `__wrapped__` getsets | done (gopy exposes the two CPython `T_OBJECT` members as read-only getsets) |
| `cm_getsetlist` | `__isabstractmethod__`, `__dict__`, `__annotations__`, `__annotate__` | done |
| `cm_methodlist` | `__class_getitem__` (classmethod via Py_GenericAlias) | done |
| `__set_name__` forwarding | call `cm.__func__.__set_name__(owner, name)` | n/a (CPython 3.14 classmethod has no `__set_name__` slot; wrapped descriptors only fire when assigned directly in the class body) |
| PEP 487 forwarding | classmethod inherits PEP 487 hooks from wrapped fn | done (the descriptor protocol on `cm` already binds the class as first argument, so `__init_subclass__`/`__class_getitem__` keep working when wrapped) |

### Gates

| Gate | Command | Expected | Status |
|------|---------|----------|--------|
| 2.1 | `gopy -c 'class C:\n @classmethod\n def f(cls): pass\nprint(C.__dict__["f"].__func__.__name__)'` | `f` | pass |
| 2.2 | `gopy -c 'class C:\n @classmethod\n def f(cls): pass\nprint(C.__dict__["f"].__wrapped__ is C.__dict__["f"].__func__)'` | `True` | pass |
| 2.3 | `gopy -c 'class C:\n @classmethod\n def f(cls): pass\nprint(C.__dict__["f"].__isabstractmethod__)'` | `False` | pass |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/funcobject.c:1487` `cm_init` |
| 2 | `Objects/funcobject.c:1459` `cm_descr_get` |
| 3 | `Objects/funcobject.c:1440` `cm_traverse` |
| 4 | `Objects/funcobject.c:1504` `cm_memberlist` |
| 5 | `Objects/funcobject.c:1511` `cm_get___isabstractmethod__` |
| 6 | `Objects/funcobject.c:1525` `cm_get___annotations__` / `cm_set___annotations__` |
| 7 | `Objects/funcobject.c:1538` `cm_get___annotate__` / `cm_set___annotate__` |
| 8 | `Objects/funcobject.c:1551` `cm_getsetlist` |
| 9 | `Objects/funcobject.c:1559` `cm_methodlist` |
| 10 | `Objects/funcobject.c:1565` `cm_repr` |
| 11 | `Objects/funcobject.c:1594` `PyClassMethod_Type` |
| 12 | `Objects/funcobject.c:1316` `functools_wraps` |
| 13 | `Objects/funcobject.c:1337` `descriptor_get_wrapped_attribute` |
| 14 | `Objects/funcobject.c:1367` `descriptor_set_wrapped_attribute` |
| 15 | `Objects/object.c:1235` `_PyObject_IsAbstract` |

## Phase 3 - `Objects/funcobject.c` staticmethod block

### Functions to port

| C function | Surface | Status |
|------------|---------|--------|
| `sm_init` | `staticmethod(fn)` constructor | done |
| `sm_descr_get` | `__get__(obj, type)` returns wrapped fn | done |
| `sm_traverse` | GC visit `sm_callable` and `sm_dict` | done (callable only) |
| `sm_dealloc` / `sm_clear` | (Go GC, no-op) | n/a |
| `sm_repr` | `<staticmethod object>` text | done |
| `sm_call` | direct call forwards to wrapped fn (CPython 3.10+) | pending |
| `sm_memberlist` | `__func__` member | pending |
| `sm_getsetlist` | `__wrapped__`, `__isabstractmethod__`, `__dict__`, `__name__`, `__qualname__`, `__module__` | pending |
| `__set_name__` forwarding | forward to wrapped fn | pending |

### Gates

| Gate | Command | Expected |
|------|---------|----------|
| 3.1 | `gopy -c 'class C:\n @staticmethod\n def f(): return 1\nprint(C.__dict__["f"].__func__())'` | `1` |
| 3.2 | `gopy -c 'sm = staticmethod(lambda: 7); print(sm())'` | `7` (sm_call) |
| 3.3 | `gopy -c 'class C:\n @staticmethod\n def f(): pass\nprint(C.__dict__["f"].__name__)'` | `f` |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/funcobject.c:1184` `sm_init` |
| 2 | `Objects/funcobject.c:1167` `sm_descr_get` |
| 3 | `Objects/funcobject.c:1148` `sm_call` |
| 4 | `Objects/funcobject.c:1211` `sm_memberlist` |
| 5 | `Objects/funcobject.c:1220` `sm_traverse` |
| 6 | `Objects/funcobject.c:1233` `PyStaticMethod_Type` |

## Phase 4 - `Objects/funcobject.c` PyFunction_Type block

### Functions to port

| C function | Surface | Status |
|------------|---------|--------|
| `PyFunction_New` | constructor | done |
| `PyFunction_NewWithQualName` | constructor with qualname | done |
| `func_get_code` / `func_set_code` | `__code__` getset | done |
| `func_get_name` / `func_set_name` | `__name__` getset | done |
| `func_get_qualname` / `func_set_qualname` | `__qualname__` getset | done |
| `func_get_defaults` / `func_set_defaults` | `__defaults__` getset | done |
| `func_get_kwdefaults` / `func_set_kwdefaults` | `__kwdefaults__` getset | done |
| `func_get_annotations` / `func_set_annotations` | `__annotations__` getset | done |
| `func_get_dict` / `func_set_dict` | `__dict__` getset | done |
| `func_get_module` / `func_set_module` | `__module__` getset | partial (verify) |
| `func_repr` | `<function name at 0x...>` | partial (verify exact format) |
| `func_call` / `func_vectorcall` | function call | done |
| `func_descr_get` | `__get__` returns BoundMethod | done |
| `func_traverse` | GC visit globals, defaults, etc. | partial |
| `function_memberlist` | `__globals__`, `__closure__`, `__builtins__` | partial |
| `function___get_signature__` | (CPython internal) | n/a |
| `func_iternext` | (none) | n/a |

### Gates

| Gate | Command | Expected |
|------|---------|----------|
| 4.1 | `gopy -c 'def f(): pass\nprint(f.__globals__ is globals())'` | `True` |
| 4.2 | `gopy -c 'def f(): pass\nprint(f.__module__)'` | `__main__` |
| 4.3 | `gopy -c 'def f(): pass\nprint(repr(f).startswith("<function f at"))'` | `True` |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/funcobject.c:30` `PyFunction_NewWithQualName` |
| 2 | `Objects/funcobject.c:760` `func_getsetlist` |
| 3 | `Objects/funcobject.c:810` `func_memberlist` |
| 4 | `Objects/funcobject.c:920` `func_repr` |
| 5 | `Objects/funcobject.c:1232` `PyFunction_Type` |

## Phase 5 - `Objects/classobject.c` PyMethod_Type full port

### Functions to port

| C function | Surface | Status |
|------------|---------|--------|
| `PyMethod_New` | constructor | done |
| `method_repr` | `<bound method ClassName.fn of <obj>>` | partial (wrong text) |
| `method_call` / `method_vectorcall` | call with self prepended | done |
| `method_getattro` | attribute forward through to `im_func` | done |
| `method_traverse` | GC visit `im_func` and `im_self` | done |
| `method_richcompare` | `__eq__` / `__ne__` over (func, self) | pending |
| `method_hash` | hash combining `im_func` + `im_self` | pending |
| `method_memberlist` | `__func__` / `__self__` members | done (via getset) |
| `method_getset` | `__doc__`, `__name__`, `__module__`, `__qualname__`, `__self_class__` | pending |
| `method___reduce__` | pickle hook | pending |
| `PyMethod_Function` / `PyMethod_Self` | C API accessors | done (as Go methods) |

### Gates

| Gate | Command | Expected |
|------|---------|----------|
| 5.1 | `gopy -c 'class C:\n def f(self): pass\nc = C()\nprint(c.f == c.f)'` | `True` |
| 5.2 | `gopy -c 'class C:\n def f(self): pass\nprint(C().f != C().f)'` | `True` |
| 5.3 | `gopy -c 'class C:\n def f(self): pass\nprint({C().f, C().f})'` | set with 2 entries |
| 5.4 | `gopy -c 'class C:\n def f(self): pass\nprint(C().f.__name__)'` | `f` |
| 5.5 | `gopy -c 'class C:\n def f(self): pass\nprint(repr(C().f).startswith("<bound method C.f of"))'` | `True` |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/classobject.c:38` `PyMethod_New` |
| 2 | `Objects/classobject.c:75` `method_getattro` |
| 3 | `Objects/classobject.c:135` `method_call` / `method_vectorcall` |
| 4 | `Objects/classobject.c:206` `method_richcompare` |
| 5 | `Objects/classobject.c:230` `method_hash` |
| 6 | `Objects/classobject.c:262` `method_traverse` |
| 7 | `Objects/classobject.c:268` `PyMethod_Type` |
| 8 | `Objects/classobject.c:280` `method_repr` |

## Phase 6 - `Objects/typeobject.c` type_new pipeline

### Functions to port

| C function | Purpose | gopy hook | Status |
|------------|---------|-----------|--------|
| `type_new` | top-level dispatch | `NewUserType` | partial |
| `type_new_get_bases` | resolve bases tuple | inline in `NewUserType` | done |
| `type_new_alloc` | allocate type object | `NewType` | done |
| `type_new_set_attrs` | stamp `__module__` / `__qualname__` / `__doc__`, classmethod-wrap PEP 487 hooks | partial (classmethod wrap done; defaults missing) | partial |
| `type_new_set_names` | PEP 487 `__set_name__` pass | `typeSetNames` | done |
| `type_init_subclass` | PEP 487 `__init_subclass__` pass | `typeInitSubclass` | done |
| `type_new_descriptors` | `__slots__` -> MemberDescr table | `installSlots` | done |
| `type_new_impl` | mro computation | `NewType` | done |
| `type_new_set_doc` | docstring stamping | inline | pending |
| `type_call` | metaclass call -> tp_new + tp_init | `TypeType.Call` | partial |
| `type_init` | type.__init__ | n/a (Go init) | done |
| `type_getattro` | type attribute lookup | `typeGetAttr` | done |
| `type_setattro` | type attribute set | `typeSetAttr` | done |
| `fixup_slot_dispatchers` | wire C slots from Python dunders | `fixupSlotDispatchers` | partial |

### Gates

| Gate | Command | Expected |
|------|---------|----------|
| 6.1 | `gopy -c 'class C: """doc"""\nprint(C.__doc__)'` | `doc` |
| 6.2 | `gopy -c 'class C: pass\nprint(C.__module__)'` | `__main__` |
| 6.3 | `gopy -c 'class C:\n class D: pass\nprint(C.D.__qualname__)'` | `C.D` |
| 6.4 | `gopy -c 'class M(type):\n  def __init_subclass__(cls, **kw): cls.x=kw["x"]\nclass C(metaclass=M, x=1): pass\nprint(C.x)'` | `1` |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/typeobject.c:4153` `type_new` |
| 2 | `Objects/typeobject.c:4350` `type_new_set_attrs` |
| 3 | `Objects/typeobject.c:4419` `type_new_set_attrs` (classmethod wrap branch) |
| 4 | `Objects/typeobject.c:4549` `type_new_set_names` |
| 5 | `Objects/typeobject.c:4595` `type_init_subclass` |
| 6 | `Objects/typeobject.c:9874` `fixup_slot_dispatchers` |

## Phase 7 - `Objects/typeobject.c` inherit_slots

### Slots to verify propagation for

Every row from CPython's slotdefs table must propagate from the
base type when the subclass does not override it.

| Slot group | Slots | Status |
|------------|-------|--------|
| Basic | `Repr`, `Str`, `Hash`, `Call`, `Getattro`, `Setattro`, `Iter`, `IterNext`, `RichCmp`, `DescrGet`, `DescrSet` | partial (4 of 11 propagate) |
| Number | `Add`, `Subtract`, `Multiply`, `Remainder`, `Divmod`, `Power`, `Negative`, `Positive`, `Absolute`, `Bool`, `Invert`, `Lshift`, `Rshift`, `And`, `Xor`, `Or`, `Int`, `Float`, `InPlace*`, `FloorDivide`, `TrueDivide`, `Index`, `MatrixMultiply` | pending |
| Mapping | `Length`, `GetItem`, `SetItem`, `DelItem` | pending |
| Sequence | `Length`, `Concat`, `Repeat`, `GetItem`, `SetItem`, `Contains`, `InPlaceConcat`, `InPlaceRepeat` | pending |
| Async | `Await`, `Aiter`, `Anext` | pending |
| Buffer | `Getbuffer`, `Releasebuffer` | pending |

### Gates

| Gate | Command | Expected |
|------|---------|----------|
| 7.1 | `gopy -c 'class L(list): pass\nl=L([1,2,3])\nprint(len(l), l[1], list(reversed(l)))'` | `3 2 [3, 2, 1]` |
| 7.2 | `gopy -c 'class D(dict): pass\nd=D({"a":1})\nprint(d["a"], len(d), "a" in d)'` | `1 1 True` |
| 7.3 | `gopy -c 'class I(int): pass\nprint(I(3)+I(4), I(5)*I(2))'` | `7 10` |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/typeobject.c:7521` `inherit_slots` |
| 2 | `Objects/typeobject.c:9770` slotdefs table header |

## Phase 8 - `Python/ceval.c` name ops

### Functions to port

| Opcode | gopy hook | CPython semantics | Status |
|--------|-----------|-------------------|--------|
| `STORE_NAME` | `storeIn` | `PyObject_SetItem(locals, name, v)`; fast-path when locals is exact dict | done |
| `LOAD_NAME` | `lookupIn` | `PyMapping_GetOptionalItem(locals, name)`, then globals, then builtins | partial (exact-dict fast path bypasses subclass __getitem__) |
| `DELETE_NAME` | `deleteIn` | `PyObject_DelItem(locals, name)` | partial (panics on non-Dict) |
| `LOAD_CLASSDEREF` (in class scope) | `lookupClassDeref` | same protocol as LOAD_NAME for the class namespace | verify |

### Gates

| Gate | Command | Expected |
|------|---------|----------|
| 8.1 | `gopy -c 'class D(dict):\n def __setitem__(self,k,v): super().__setitem__(k,v*2)\nclass M(type):\n @classmethod\n def __prepare__(mcs,n,b): return D()\nclass C(metaclass=M): x=10\nprint(C.x)'` | `20` |
| 8.2 | `gopy -c 'class D(dict):\n def __getitem__(self,k): return super().__getitem__(k)+1\nclass M(type):\n @classmethod\n def __prepare__(mcs,n,b): return D()\nclass C(metaclass=M):\n x=10\n y=x\nprint(C.y)'` | `11` |
| 8.3 | `gopy -c 'class D(dict):\n def __delitem__(self,k): super().__delitem__(k)\nclass M(type):\n @classmethod\n def __prepare__(mcs,n,b): return D()\nclass C(metaclass=M):\n x=10\n del x\nprint(hasattr(C,"x"))'` | `False` |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Python/bytecodes.c STORE_NAME` |
| 2 | `Python/bytecodes.c LOAD_NAME` |
| 3 | `Python/bytecodes.c DELETE_NAME` |

## Final gate

| # | Command | Expected |
|---|---------|----------|
| F.1 | `gopy -c 'import enum; print(enum.FlagBoundary.STRICT)'` | `FlagBoundary.STRICT` |
| F.2 | `gopy -c 'import enum; print(list(enum.FlagBoundary))'` | `[STRICT, CONFORM, EJECT, KEEP]` (or members in spec order) |
| F.3 | `gopy -c 'import re; print(re.match(r"(\d+)-(\d+)", "12-34").groups())'` | `('12', '34')` |
| F.4 | `gopy -c 'import fnmatch; print(fnmatch.fnmatch("foo.py", "*.py"))'` | `True` |
| F.5 | spec 1703 phase 7 row | flips to `done` |
| F.6 | spec 1702 `enum` row | flips to `done` |

## Out of scope

| Topic | Reason |
|-------|--------|
| Full pickle (`__reduce__`, `copyreg`) | Phase 1 wires the descriptor; real reduce_newobj follows in a pickle spec |
| `__class__` reassignment validation | Phase 1 wires the getset; large compat rules deferred |
| `inherit_slots` async + buffer rows | No async / buffer subsystem ports landed |
| Locale-specific `__hash__` for strings | string subsystem owns its hash |
| GC `tp_clear` / `tp_dealloc` | Go has GC; explicit clear/dealloc only ported where they hold non-Go state |

## Today's already-landed pieces (do not redo)

| File | Hunk | Phase |
|------|------|-------|
| `objects/usertype.go` | `typeSetNames` PEP 487 pass | 6 |
| `objects/usertype.go` | `typeInitSubclass` | 6 |
| `vm/eval_simple.go` | `storeIn` dispatches through `PyObject_SetItem` for dict subclasses | 8 |
| `objects/function.go` | `FunctionType.Hash = identityHash` | 4 |
| `objects/function_builtin.go` | `BuiltinFunctionType.Hash = identityHash` | (CPython inheritance) |
| `objects/method_descr.go` | `MethodDescrType.Hash = identityHash` | (CPython inheritance) |

## Tasks

One task per phase, all blocking #544.

| Task | Phase | Title |
|------|-------|-------|
| TBD | 1 | Port Objects/object.c methodlist + getsetlist in full |
| TBD | 2 | Port Objects/funcobject.c classmethod block in full |
| TBD | 3 | Port Objects/funcobject.c staticmethod block in full |
| TBD | 4 | Port Objects/funcobject.c PyFunction_Type block in full |
| TBD | 5 | Port Objects/classobject.c PyMethod_Type in full |
| TBD | 6 | Port Objects/typeobject.c type_new pipeline in full |
| TBD | 7 | Audit Objects/typeobject.c inherit_slots: every slot edge |
| TBD | 8 | Port Python/ceval.c STORE_NAME / LOAD_NAME / DELETE_NAME in full |
