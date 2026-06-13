---
id: "1730"
slug: 1730
title: "1730: Classes / descriptors / methods test panel — CPython 3.14 parity port"
sidebar_label: "1730 Classes descriptors panel"
description: "Audit and port of the 12 Classes/descriptors/methods test files from spec 1700 against CPython 3.14 under the spec 1726 bridge. SimpleNamespace, the metatype refcount, slot-state pickling and the type-level dunder lookups are done; test_descr/test_types/test_typing residuals remain."
---

## Status

Active. Branch `feat/v0.13.4-spec-classes-descriptors`.

Run under the [[1726]] bridge so every `@cpython_only` test executes on gopy
instead of being skipped. "No skip" means parity with CPython: if CPython
skips a test on this platform, gopy skips it too; everything else must pass.

## Goal

Drive every test in the spec 1700 Classes / descriptors / methods panel
(12 files) to zero failures via faithful CPython 3.14 ports. No shims, no
partial slices: when a gate lands on a subsystem, port every function in that
subsystem from CPython as the single source of truth.

Sources of truth: `$HOME/cpython-314/`. Every cited function is read from that
tree before porting.

---

## Panel state (run from `test/cpython/`, audit date 2026-06-14)

| File | Result |
| --- | --- |
| `test_abc` | OK (72) |
| `test_super` | OK (40) |
| `test_property` | OK (31, 2 skipped) |
| `test_descrtut` | OK (8) |
| `test_dynamicclassattribute` | OK (12, 1 skipped) |
| `test_genericalias` | OK |
| `test_class` | 1 failure (deferred, see below) |
| `test_metaclass` | 1 failure (harness `__module__` prefix, pre-existing) |
| `test_descr` | 3 failures, 4 errors (MroTest green) |
| `test_types` | 2 failures, 4 errors (CoroutineTests + ClassCreationTests green) |
| `test_typing` | 115 failures / 164 errors (no abort) |
| `test_enum` | error (missing `pydoc`) |

---

## Shipped in this branch

### types.SimpleNamespace ported from namespaceobject.c

The previous SimpleNamespace was a hand-rolled wrapper: it sorted repr keys
alphabetically, never exposed `__dict__`, rejected any positional argument and
implemented only EQ/NE. Replaced with a faithful port of
`Objects/namespaceobject.c`:

- `ns_dict` is the instance dict, reachable as a read-only `__dict__` getset
  (`namespace_members`), and attribute access routes through the generic
  getattr/setattr machinery (`*Namespace` implements `AttrDictHolder`,
  `HasDict = true`).
- `namespace_init` accepts an optional positional mapping (`dict(arg)` for a
  non-dict), validates that every key is a string, then merges the keyword
  arguments. Keyword order is preserved by threading the original `*Dict`
  through `TpNewWithDict`.
- `namespace_repr` renders `name(k=repr(v), ...)` in insertion order, guarded
  by `Py_ReprEnter`/`Py_ReprLeave`, with the `name(...)` form on recursion and
  the subclass `tp_name` for non-exact instances.
- `__new__`, `__reduce__` and `__replace__` are registered. `__replace__`
  builds the result by calling the (possibly overridden) type and rejects a
  non-namespace return with CPython's exact error.
- `__new__` is exposed so `super().__new__(cls)` from a subclass returns a
  `*Namespace` rather than falling through to `object.__new__`.

`test_types.SimpleNamespaceTests` is fully green (18 tests).

### A type holds a strong reference to its metatype

Every object references its `ob_type`, but a heap type was not counting its
metatype. A metaclass built in a temporary scope (`six.with_metaclass`) was
freed when its defining frame returned even though a class constructed from it
still named it as `ob_type`; `typeUserDealloc` then cleared the metaclass
descriptor table, so `__new__` lookups fell through to the builtin.
`stampMetaclass` now increfs the metatype and `typeUserDealloc` releases it
once through a latch, mirroring `type_dealloc`'s `Py_DECREF(Py_TYPE(type))`.
Fixes `test_abc.test_tricky_new_works`.

### _testinternalcapi specialization thresholds

`test_super` reaches for `_testinternalcapi.SPECIALIZATION_THRESHOLD` to warm
an instruction past quickening. The module was importable but missing the
constants, so the tests errored. Publishes `TIER2_THRESHOLD`,
`SPECIALIZATION_THRESHOLD` and `SPECIALIZATION_COOLDOWN`
(`module_exec`, initial-counter-plus-one). Fixes 3 `test_super` errors.

### Slot-state pickling via copyreg._slotnames

`object_getstate_default` walked `__slots__` across the MRO directly; it now
reads the `__slotnames__` list `copyreg._slotnames` builds (cached on the
type), pulls each slot through normal getattr so a class `__getattr__`
participates, and rechecks the list length per iteration.
`__getnewargs_ex__`/`__getnewargs__` resolve through the type (`LookupSpecial`)
like `_PyObject_GetNewArguments`.

### math.ceil/floor/trunc via the type-level dunder

Ported `_PyObject_MaybeCallSpecialNoArgs` so `math.ceil/floor/trunc` consult
`__ceil__`/`__floor__`/`__trunc__` off the operand's type (bypassing the
instance `__getattribute__`), and dropped `trunc`'s float fallback so a type
without `__trunc__` raises.

### Optional dunder probes no longer leak AttributeError

`test_typing` aborted mid-run, not at import: `collect_parameters` and
`_generic_init_subclass` probe `__parameters__`/`__origin__` on bases that may
lack them. A typing `_SpecialGenericAlias` (`List`, `Mapping`, `ByteString`)
and `_SpecialForm` (`Concatenate`) raise `AttributeError` from their
`__getattr__`. The ports used `GetAttr` and ignored the error, but that left
the `AttributeError` pending on the thread state, so it surfaced spuriously on
the next operation. `iter(test)` inside the unittest suite's `_isnotsuite`
re-read that stale exception, which is why the abort pointed at
`__parameters__` raised through `__getattr__`. The original diagnosis (a
generic alias landing in `_tests`) was wrong: the suite was well-formed; the
exception state was not.

Switched these probes to `LookupAttr` (the `_PyObject_LookupAttr` port that
clears the suppressed `AttributeError`), matching CPython's own
`collect_parameters`/`_generic_init_subclass`. Same fix for `object.__dir__`
and `object.__getstate__`, which probe `__dict__` on `__slots__` objects.
`dict.clear()` now resolves its receiver through `DictBacking` so it operates
on a `defaultdict`'s backing storage instead of crashing on the type
assertion. test_typing now runs to completion (709 tests).

The residual failures / errors are dominated by PEP 646 `TypeVarTuple` and
PEP 612 `ParamSpec` substitution, which gopy's `subsParameters` does not yet
fully implement. That is a separate subsystem port.

### types.UnionType / TypeVar / GenericAlias operator parity

Four parity gaps in the `|` operator and GenericAlias subclassing:

- The union builder probed each arg with `PyObject_Hash` to decide the
  hashable vs unhashable bucket, but on failure it never cleared the pending
  exception the way `unionbuilder_add_single_unchecked` does with
  `PyErr_Clear`. With unhashable args alive the leaked `TypeError` surfaced
  later from an unrelated string-keyed dict lookup. `addSingleUnchecked` now
  clears it at the point of the swallow.
- `union_hash` re-hashes each unhashable arg so the error carries that arg's
  own `TypeError` rather than a generic "N unhashable elements".
- `TypeVar` and `ParamSpec` gained `__or__`/`__ror__` (building a
  `typing.Union`), so `T | None` no longer raises unsupported-operand.
- `GenericAlias` exposes `__new__`/`__repr__`/`__hash__` in its type dict and
  `ga_new` honours the passed `cls`, so a Python subclass of
  `types.GenericAlias` gets an instance of itself and inherits the repr.

`test_types.UnionTests` is green (29 tests, 1 CPython skip matched).

### int / float `__format__` method descriptors

Both types carried a working `PyObject_Format` slot (so f-strings and
`str.format` worked) but exposed no `__format__` in the type dict, so an
explicit `(123).__format__('d')` fell through to `object.__format__` and was
rejected. Installed method descriptors routing through the same renderers;
bool inherits int's through the MRO; empty spec returns `str(self)`.

### locale.format_string and the float 'n' presentation type

`test_types` exercises `locale.format_string('%d'/'%g', x, grouping=True)`
against `format(x, 'n')`. Two gaps: gopy's `locale.py` was a stub without
`format_string`, and the float renderer never handled the `'n'` presentation
type. Vendored `format_string` plus its helpers (`_group`,
`_grouping_intervals`, `_strip_padding`, `_format`, `_localize`) from
`Lib/locale.py`, running off the existing `localeconv()`, and added `'n'` to
the float renderer (it is `'g'` plus LC_NUMERIC grouping).

### Stable generator names and await value extraction

`test_types.CoroutineTests` pins a generator/coroutine attribute at wrapper
construction (`types._GeneratorWrapper.__init__` captures `gen.__name__`) and
later asserts identity against a fresh read. The `__name__` / `__qualname__`
getsets rebuilt a `str` on every access, so the identity never held. They now
cache the PyObject form on the generator and return the same object, matching
`gen_get_name` / `gen_get_qualname`.

`test_duck_functional_gen` awaits a duck-typed coroutine (a plain object with
`send`/`throw`/`close` wrapped by `types.coroutine`). When its `send()` raises
`StopIteration(v)`, the `SEND` opcode must take the value out and resume the
awaiting frame. The generic (non-generator) `SEND` path extracted the value
from the sub-iterator's Python-level `StopIteration` but left the exception
live on the thread state, so the enclosing coroutine's own `RETURN` re-reported
the stale `StopIteration` instead of completing with `return value + 100`.
`SEND` now clears the indicator once it fetches the value, mirroring
`_PyGen_FetchStopIterationValue`.

### shape_differs walks the MRO-resolved size

Setting `object.__basicsize__` to its CPython value (16) made plain user
classes read `0` for their own `tp_basicsize` field while resolving `16`
through the MRO. `shape_differs` compared the raw fields, so a user class
looked different from `object` and became its own solid base, which made
`class D(B[str], float)` raise a spurious instance-layout conflict. It now
compares through `typeBasicSize` / `typeItemSize` (the same MRO walk the
`__basicsize__` getset uses), so a class that inherits its layout is no longer
treated as a distinct solid base. Fixes `ClassCreationTests.test_get_original_bases`
and `test_new_class_with_mro_entry_genericalias`.

### Reentrant `__bases__` recompute and the tp_base cycle guard

`type_set_bases` lets a custom metaclass `mro()` reassign `__bases__` again
while the hierarchy is still being recomputed, and gopy mishandled the
reentrance two ways. The recompute loop used to compute the C3 order,
install it, run the override separately, and on a raising override
unconditionally restore the previous MRO. That clobbered the value a
reentrant assignment had already installed deeper in the stack.
`mroInvoke` now computes the new MRO (dispatching to the override) without
installing it, then `mroHierarchy` does the reentrancy check by slice
identity, mirroring `mro_internal_unlocked`: on error it leaves `tp_mro`
as the reentrant call left it, and on reentrance it keeps that value and
stops instead of overwriting.

Separately, the cycle check in `validateNewBases` only scanned the
candidate base's MRO. During reentrance a custom `mro()` assigns the
primary base before the MRO is refreshed, so a freshly formed base cycle
slipped through and `solidBase` then looped until the stack overflowed.
The two-pronged check from `type_set_bases_unlocked` is now ported: MRO
scan plus a `tp_base` chain walk, with the chain walk folded into
`PyType_IsSubtype`. `__init_subclass__` also runs through `super(t, t)` so
an MRO that omits the type fails supercheck (gh-92112), with the bound
hook decref'd so a discarded subclass is not pinned past dealloc. Clears
all of `MroTest`, including `test_tp_subclasses_cycle_error_return_path`,
`test_reent_set_bases_tp_base_cycle`, and
`test_mutable_bases_with_failing_mro`.

---

## Checklist

- [x] Port `types.SimpleNamespace` from `namespaceobject.c` (test_types SimpleNamespaceTests green)
- [x] Type holds a strong reference to its metatype (test_abc green)
- [x] `_testinternalcapi` specialization thresholds (test_super green)
- [x] Slot-state pickling through `copyreg._slotnames`
- [x] `math.ceil/floor/trunc` route through the type-level dunder
- [x] `TpFlagAbstract` wired to `__abstractmethods__`
- [x] test_abc, test_super, test_property, test_descrtut, test_dynamicclassattribute, test_genericalias green
- [x] test_types: UnionTests green (union exc-clear + re-hash, TypeVar/ParamSpec `__or__`, GenericAlias subclass `__new__`/`__repr__`)
- [x] test_types: int/float `__format__` descriptors + `locale.format_string` + float `'n'` presentation type
- [x] test_types: MappingProxy dunders, ClassCreationTests (type.__new__ arity, __prepare__ mapping check, tuple-subclass __bases__ gh-132176), internal_sizes (object.__basicsize__)
- [x] test_types: CoroutineTests green (stable gi_name/gi_qualname identity + await StopIteration-value extraction)
- [x] shape_differs walks the MRO-resolved size so user classes are not spurious solid bases after object gained a nonzero basicsize
- [ ] test_types: bound builtin method type (`''.join` should be `builtin_function_or_method`, not Python `method`) and `test_names` (`type(len) is type([].append)`) both need the full descrobject wrapper taxonomy, tracked as task #250. gopy folds three CPython distinctions: (a) two separate type singletons both named `builtin_function_or_method` (`BuiltinFunctionType` for `len`, `CFunctionType` for `MethodDef` methods) that must unify, (b) slot dunders (`object.__init__`, `__str__`) are plain `MethodDescr` rather than `wrapper_descriptor`, so they have no `method-wrapper` bound form. A `method_get` -> `CFunction` shim was tried and reverted because it wrongly turned `object().__init__` into `builtin_function_or_method` (regressing `test_method_wrapper_types`); the real `PyWrapperDescr_Type` / `PyMethodWrapper_Type` subsystem is required instead. Also noted: `NewCFunction` does not take a reference on `self`/`module` (no traverse/dealloc), unlike `PyCMethod_New`'s `Py_XINCREF` — fix as part of #250.
- [ ] test_types: residual errors need C-extension support (`_testcapi` capsule/dunder-get-signature, `_queue`, subinterpreters)
- [x] test_descr: `MroTest` green. Ported the reentrant `__bases__` recompute semantics (mro_invoke / mro_internal reentrancy check) and the `tp_base` cycle guard; weakref `__slots__` rejection; `__mro__` returns None mid-creation
- [ ] test_descr: remaining 3 failures / 4 errors are out of scope. `test_slots` and `test_metaclass` doctests differ only by the `test.` module prefix (harness vendoring artifact). `test_subclasses` needs PEP 412 split-key dicts. `test_type_lookup_mro_reference` needs `assert_python_ok` subprocess. `test_bpo25750` / `test_testcapi_no_segfault` need `_testcapi`; `test_descrdoc` / `test_method_get_meth_method_invalid_type` are io getset gaps
- [x] test_typing: runner no longer aborts. Root cause was optional dunder probes leaking a pending `AttributeError` (see above), not a malformed suite.
- [ ] test_typing: port PEP 646 `TypeVarTuple` / PEP 612 `ParamSpec` substitution (`subsParameters`) — dominates the remaining errors
- [ ] test_enum: vendor `pydoc`
- [ ] test_class: `test_detach_materialized_dict_no_memory` needs `_testcapi.set_nomemory` (allocator fault injection, infeasible on the Go runtime; deferred)
- [ ] test_metaclass: `__module__` prefix differs under the unittest harness (`test_metaclass` vs `test.test_metaclass`); confirm it is harness-only and not a gopy divergence

## Deferred / out of scope

- `test_class.test_detach_materialized_dict_no_memory` drives
  `_testcapi.set_nomemory`, which faults the C allocator. Go's runtime has no
  equivalent injection point; this is an environmental gap, not a behaviour
  divergence.
