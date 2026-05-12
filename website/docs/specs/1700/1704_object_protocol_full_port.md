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
| E | `Python/ceval.c` name ops | ~80 | `vm/eval_simple.go` (storeIn / lookupIn / deleteIn) | done |
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
| 3 | D `funcobject.c` | PyStaticMethod_Type (all `sm_*` functions) | - | done |
| 4 | D `funcobject.c` | PyFunction_Type (all `func_*` functions) | - | done |
| 5 | C `classobject.c` | PyMethod_Type (all `method_*` functions) | - | done |
| 6 | B `typeobject.c` | `type_new` pipeline (`type_new_*` functions) | 1,2,3,4,5 | done |
| 7 | B `typeobject.c` | `inherit_slots` (every slot edge) | 6 | done |
| 8 | E `ceval.c` | STORE_NAME / LOAD_NAME / DELETE_NAME | - | done |
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
| `sm_init` | `staticmethod(fn)` constructor + `functools_wraps` | done |
| `sm_descr_get` | `__get__(obj, type)` returns wrapped fn | done |
| `sm_traverse` | GC visit `sm_callable` and `sm_dict` | done |
| `sm_dealloc` / `sm_clear` | (Go GC, no-op) | n/a |
| `sm_repr` | `<staticmethod(REPR_OF_CALLABLE)>` text | done |
| `sm_call` | direct call forwards to wrapped fn (CPython 3.10+) | done |
| `sm_memberlist` | `__func__`, `__wrapped__` getsets | done |
| `sm_getsetlist` | `__isabstractmethod__`, `__dict__`, `__annotations__`, `__annotate__` | done |
| `sm_methodlist` | `__class_getitem__` (classmethod via Py_GenericAlias) | done |
| `__set_name__` forwarding | forward to wrapped fn | n/a (CPython 3.14 staticmethod has no `__set_name__` slot either) |

### Gates

| Gate | Command | Expected | Status |
|------|---------|----------|--------|
| 3.1 | `gopy -c 'class C:\n @staticmethod\n def f(): return 1\nprint(C.__dict__["f"].__func__())'` | `1` | pass |
| 3.2 | `gopy -c 'sm = staticmethod(lambda: 7); print(sm())'` | `7` (sm_call) | pass |
| 3.3 | `gopy -c 'class C:\n @staticmethod\n def f(): pass\nprint(C.__dict__["f"].__name__)'` | `f` | pass |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/funcobject.c:1731` `sm_init` |
| 2 | `Objects/funcobject.c:1705` `sm_descr_get` |
| 3 | `Objects/funcobject.c:1749` `sm_call` |
| 4 | `Objects/funcobject.c:1687` `sm_traverse` |
| 5 | `Objects/funcobject.c:1755` `sm_memberlist` |
| 6 | `Objects/funcobject.c:1762` `sm_get___isabstractmethod__` |
| 7 | `Objects/funcobject.c:1776` `sm_get___annotations__` / `sm_set___annotations__` |
| 8 | `Objects/funcobject.c:1789` `sm_get___annotate__` / `sm_set___annotate__` |
| 9 | `Objects/funcobject.c:1801` `sm_getsetlist` |
| 10 | `Objects/funcobject.c:1809` `sm_methodlist` |
| 11 | `Objects/funcobject.c:1815` `sm_repr` |
| 12 | `Objects/funcobject.c:1842` `PyStaticMethod_Type` |

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
| `func_get_module` / `func_set_module` | `__module__` getset | done |
| `func_repr` | `<function QUALNAME at 0xPTR>` | done |
| `func_call` / `func_vectorcall` | function call | done |
| `func_descr_get` | `__get__` returns BoundMethod | done |
| `func_traverse` | GC visit code, globals, defaults, etc. | done |
| `function_memberlist` | `__globals__`, `__closure__`, `__builtins__` | done (via getset) |
| `function___get_signature__` | (CPython internal) | n/a |
| `func_iternext` | (none) | n/a |

### Gates

| Gate | Command | Expected | Status |
|------|---------|----------|--------|
| 4.1 | `gopy -c 'def f(): pass\nprint(f.__globals__ is globals())'` | `True` | pass |
| 4.2 | `gopy -c 'def f(): pass\nprint(f.__module__)'` | `__main__` | pass |
| 4.3 | `gopy -c 'def f(): pass\nprint(repr(f).startswith("<function f at"))'` | `True` | pass |

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/funcobject.c:30` `PyFunction_NewWithQualName` |
| 2 | `Objects/funcobject.c:760` `func_getsetlist` |
| 3 | `Objects/funcobject.c:810` `func_memberlist` |
| 4 | `Objects/funcobject.c:920` `func_repr` |
| 5 | `Objects/funcobject.c:1093` `func_traverse` |
| 6 | `Objects/funcobject.c:1232` `PyFunction_Type` |

## Phase 5 - `Objects/classobject.c` PyMethod_Type full port

### Functions to port

| C function | Surface | Status |
|------------|---------|--------|
| `PyMethod_New` | constructor | done |
| `method_repr` | `<bound method QUALNAME of REPR>` | done |
| `method_call` / `method_vectorcall` | call with self prepended | done |
| `method_getattro` | attribute forward through to `im_func` | done |
| `method_traverse` | GC visit `im_func` and `im_self` | done |
| `method_richcompare` | `__eq__` / `__ne__` over (func, self) | done |
| `method_hash` | hash combining `im_func` + `im_self` | done |
| `method_memberlist` | `__func__` / `__self__` members | done (via getset) |
| `method_getset` | `__doc__`, `__name__`, `__module__`, `__qualname__` | done (forwarded via method_getattro) |
| `method___reduce__` | pickle hook | n/a (gopy has no pickle yet; will land with copyreg port) |
| `PyMethod_Function` / `PyMethod_Self` | C API accessors | done (as Go methods) |

### Gates

| Gate | Command | Expected | Status |
|------|---------|----------|--------|
| 5.1 | `gopy -c 'class C:\n def f(self): pass\nc = C()\nprint(c.f == c.f)'` | `True` | pass |
| 5.2 | `gopy -c 'class C:\n def f(self): pass\nprint(C().f != C().f)'` | `True` | pass |
| 5.3 | `gopy -c 'class C:\n def f(self): pass\nprint({C().f, C().f})'` | set with 2 entries | pass |
| 5.4 | `gopy -c 'class C:\n def f(self): pass\nprint(C().f.__name__)'` | `f` | pass |
| 5.5 | `gopy -c 'class C:\n def f(self): pass\nprint(repr(C().f).startswith("<bound method C.f of"))'` | `True` | pass |

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

### Side fixes

- `object.__repr__` / `object.__str__` slot wrappers were re-entering
  `Repr` / `Str` from inside the wrapper, which bounced back through
  `slot_tp_repr` for user types and blew the stack. The wrappers now
  delegate straight to `objectRepr` / `objectStr`, matching CPython's
  direct `PyUnicode_FromFormat` path. This was a pre-existing latent
  bug surfaced once `method_repr` started calling `Repr(self)`.

## Phase 6 - `Objects/typeobject.c` type_new pipeline

### Functions to port

| C function | Purpose | gopy hook | Status |
|------------|---------|-----------|--------|
| `type_new` | top-level dispatch | `NewUserType` / `NewUserTypeKwargs` | done |
| `type_new_get_bases` | resolve bases tuple | inline in `NewUserType` | done |
| `type_new_alloc` | allocate type object | `NewType` | done |
| `type_new_set_attrs` | stamp `__module__` / `__qualname__` / `__doc__`, classmethod-wrap PEP 487 hooks | `copyNamespaceToType` | done |
| `type_new_set_names` | PEP 487 `__set_name__` pass | `typeSetNames` | done |
| `type_init_subclass` | PEP 487 `__init_subclass__` pass with kwargs | `typeInitSubclass` | done |
| `type_new_descriptors` | `__slots__` -> MemberDescr table | `installSlots` | done |
| `type_new_impl` | mro computation | `NewType` | done |
| `type_new_set_doc` | docstring stamping | `emitInnerClassCode` (compile-time) | done |
| `type_call` | metaclass call -> tp_new + tp_init | `TypeType.Call` | done |
| `type_init` | type.__init__ | n/a (Go init) | done |
| `type_getattro` | type attribute lookup | `typeGetAttr` | done |
| `type_setattro` | type attribute set | `typeSetAttr` | done |
| `type_qualname` / `type_set_qualname` | `__qualname__` getset | `typeGetQualname` / `typeSetQualname` | done |
| `fixup_slot_dispatchers` | wire C slots from Python dunders | `fixupSlotDispatchers` | done (Phase 7 widened the inherit pass) |

### Gates

| Gate | Command | Expected | Status |
|------|---------|----------|--------|
| 6.1 | `gopy -c 'class C:\n """doc"""\nprint(C.__doc__)'` | `doc` | pass |
| 6.2 | `gopy -c 'class C: pass\nprint(C.__module__)'` | `__main__` | pass |
| 6.3 | `gopy -c 'class C:\n class D: pass\nprint(C.D.__qualname__)'` | `C.D` | pass |
| 6.4 | `gopy -c 'class B:\n def __init_subclass__(cls, **kw): cls.x = kw["x"]\nclass C(B, x=1): pass\nprint(C.x)'` | `1` | pass |

### Side fixes shipped under Phase 6

- `Type.Qualname` field added. `__qualname__` no longer shadows `__name__`,
  so nested classes report the dotted path (`C.D`) instead of the bare
  `D`. `typeSetQualname` honours the heap-type check the way CPython's
  `type_set_qualname` does.
- `copyNamespaceToType` now pulls `__qualname__` out of the namespace
  and stamps it on `t.Qualname` directly. The raw key is skipped from
  the descr table so the getset (not a stale descriptor) wins on
  lookup.
- `emitInnerClassCode` extracts the body's leading bare-string and
  emits `LOAD_CONST docstring` + `STORE_NAME __doc__` after the
  qualname store. Mirrors CPython's class-body docstring branch
  without disturbing the existing `consumeDocstring` flow (which
  pins to `consts[0]`, conflicting with the qualname const slot here).
- `typeInitSubclass` now takes the class-creation kwargs. The path
  from `__build_class__` -> `typeMetaCall` -> `NewUserTypeKwargs`
  threads them so `__init_subclass__(cls, **kw)` actually sees the
  PEP 487 keyword args.

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/typeobject.c:4153` `type_new` |
| 2 | `Objects/typeobject.c:4350` `type_new_set_attrs` |
| 3 | `Objects/typeobject.c:4419` `type_new_set_attrs` (classmethod wrap branch) |
| 4 | `Objects/typeobject.c:4549` `type_new_set_names` |
| 5 | `Objects/typeobject.c:4595` `type_init_subclass` |
| 6 | `Objects/typeobject.c:9874` `fixup_slot_dispatchers` |
| 7 | `Objects/typeobject.c:984` `type_qualname` |
| 8 | `Objects/typeobject.c:1003` `type_set_qualname` |
| 9 | `Python/codegen.c` `codegen_class_body` (docstring branch) |

## Phase 7 - `Objects/typeobject.c` inherit_slots

### Slots to verify propagation for

Every row from CPython's slotdefs table must propagate from the
base type when the subclass does not override it. `inheritSlotsFromBases`
runs before `fixupSlotDispatchers` so the subclass picks up the
base's slot table first, then a user-supplied dunder can override
individual slot fields. The `Number` / `Sequence` / `Mapping` / `Async`
tables are copied by value (`*cp := *base.X`) so the per-subclass
fixup writes never mutate the base.

| Slot group | Slots | Status |
|------------|-------|--------|
| Basic | `Repr`, `Str`, `Hash`, `Call`, `TpNew`, `Iter`, `IterNext`, `RichCmp`, `DescrGet`, `DescrSet`, `Format`, `TpTraverse` | done |
| Basic (handled outside `inherit_slots`) | `Getattro`, `Setattro` | done (selected per instance shape in `NewUserTypeKwargs`) |
| Number | `Add`, `Subtract`, `Multiply`, `Remainder`, `Divmod`, `Power`, `Negative`, `Positive`, `Absolute`, `Bool`, `Invert`, `Lshift`, `Rshift`, `And`, `Xor`, `Or`, `Int`, `Float`, `InPlace*`, `FloorDivide`, `TrueDivide`, `Index`, `MatrixMultiply` | done (table copied as a unit) |
| Mapping | `Length`, `GetItem`, `SetItem`, `DelItem` | done (table copied as a unit) |
| Sequence | `Length`, `Concat`, `Repeat`, `GetItem`, `SetItem`, `Contains`, `InPlaceConcat`, `InPlaceRepeat` | done (table copied as a unit) |
| Async | `Await`, `Aiter`, `Anext` | done (table copied as a unit) |
| Buffer | `Getbuffer`, `Releasebuffer` | pending (no Buffer subsystem ported yet) |

### Gates

| Gate | Command | Expected | Status |
|------|---------|----------|--------|
| 7.1 | `gopy -c 'class L(list): pass\nl=L([1,2,3])\nprint(len(l), l[1], list(reversed(l)))'` | `3 2 [3, 2, 1]` | pass |
| 7.2 | `gopy -c 'class D(dict): pass\nd=D({"a":1})\nprint(d["a"], len(d), "a" in d)'` | `1 1 True` | pass |
| 7.3 | `gopy -c 'class I(int): pass\nprint(I(3)+I(4), I(5)*I(2))'` | `7 10` | pass |

### Side fixes shipped under Phase 7

- `objectNew` now runs the abstract-method guard. Before this phase the
  guard sat in `typeCall` and only ran when `TpNew` was nil. Now that
  every user class inherits `TpNew` from `object`, the check has to
  live where CPython parks it - inside `object_new`.
- The fixup pass order is `inherit -> fixup` so user dunders override
  the inherited slot fields. Previously fixup ran first; that worked
  for the Basic four because they were the only inherited slots, but
  it would have masked user overrides once the slot tables started
  propagating.
- A subclass's slot tables are deep copies of the base's table. Without
  the copy, any `fixupSubscriptSlots` write through `ensureSequenceMethods`
  on the subclass would smash the base type's table.

### CPython citations

| # | Reference |
|---|-----------|
| 1 | `Objects/typeobject.c:7521` `inherit_slots` |
| 2 | `Objects/typeobject.c:9770` slotdefs table header |
| 3 | `Objects/typeobject.c:6854` `object_new` (Py_TPFLAGS_IS_ABSTRACT branch) |

## Phase 8 - `Python/ceval.c` name ops

### Functions to port

| Opcode | gopy hook | CPython semantics | Status |
|--------|-----------|-------------------|--------|
| `STORE_NAME` | `storeIn` | `PyObject_SetItem(locals, name, v)`; fast-path when locals is exact dict | done |
| `LOAD_NAME` | `lookupIn` | `PyMapping_GetOptionalItem(locals, name)`, then globals, then builtins | done (exact-dict guard gates the fast path so subclass __getitem__ fires) |
| `DELETE_NAME` | `deleteIn` | `PyObject_DelItem(locals, name)` | done (exact-dict guard plus `objects.DelItem` fallback for subclasses) |
| `LOAD_CLASSDEREF` (in class scope) | `lookupClassDeref` | same protocol as LOAD_NAME for the class namespace | done (reuses the widened `lookupIn` exact-dict guard) |

### Gates

Each gate exercises one of LOAD_NAME / STORE_NAME / DELETE_NAME inside a
class body whose namespace is a `dict` subclass returned by a metaclass
`__prepare__`. Tracing get/set/del lets the gate watch the slot fire.

| Gate | Opcode | Expected | Status |
|------|--------|----------|--------|
| 8.1 | LOAD_NAME routes through subclass `__getitem__` | log records the `X` lookup; class attribute computed from override-decorated read | pass |
| 8.2 | STORE_NAME routes through subclass `__setitem__` | log records every name binding inside the class body | pass |
| 8.3 | DELETE_NAME routes through subclass `__delitem__` | log records the `del`, and the attribute is gone from the class | pass |

Pinned form: `stdlibinit/name_ops_dict_subclass_test.go` (one Go test
per gate, each running the Python script under `pythonrun.RunString`
and asserting on stdout).

### Side fixes shipped under Phase 8

- `lookupIn` (LOAD_NAME) gates its `*objects.Dict` fast path on
  `scope.Type() == objects.DictType`. Dict subclasses fall through to
  `objects.GetItem`, which honours the subclass `__getitem__` slot.
  The previous type assertion succeeded on subclasses too, silently
  bypassing the override.
- `deleteIn` (DELETE_NAME) does the same exact-type gate, then falls
  back to `objects.DelItem` instead of returning the old
  `"unsupported scope type"` error. That branch used to panic when the
  class body's namespace was anything but a plain Dict.
- `storeIn` (STORE_NAME) already routed through `objects.SetItem` for
  non-Dict scopes; Phase 8 leaves the fast path in place but documents
  the symmetric exact-type guard so the three opcodes track each other.

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
