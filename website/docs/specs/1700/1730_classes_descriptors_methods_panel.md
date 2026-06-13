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

## Panel state (run from `test/cpython/`, audit date 2026-06-13)

| File | Result |
| --- | --- |
| `test_abc` | OK (72) |
| `test_super` | OK (40) |
| `test_property` | OK (31, 2 skipped) |
| `test_descrtut` | OK (8) |
| `test_dynamicclassattribute` | OK (12, 1 skipped) |
| `test_class` | 1 failure (deferred, see below) |
| `test_metaclass` | 1 failure (harness `__module__` prefix, pre-existing) |
| `test_descr` | 21 failures, 14 errors |
| `test_types` | 11 failures, 20 errors |
| `test_typing` | 709 run, 141 failures / 293 errors (no abort) |
| `test_genericalias` | error (missing `concurrent.futures.thread`) |
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

The residual 141 failures / 293 errors are dominated (~236) by PEP 646
`TypeVarTuple` and PEP 612 `ParamSpec` substitution, which gopy's
`subsParameters` does not yet implement. That is a separate subsystem port.

---

## Checklist

- [x] Port `types.SimpleNamespace` from `namespaceobject.c` (test_types SimpleNamespaceTests green)
- [x] Type holds a strong reference to its metatype (test_abc green)
- [x] `_testinternalcapi` specialization thresholds (test_super green)
- [x] Slot-state pickling through `copyreg._slotnames`
- [x] `math.ceil/floor/trunc` route through the type-level dunder
- [x] `TpFlagAbstract` wired to `__abstractmethods__`
- [x] test_abc, test_super, test_property, test_descrtut, test_dynamicclassattribute green
- [ ] test_descr: clear the remaining 21 failures / 14 errors
- [ ] test_types: clear UnionTests / MappingProxy / float-int `__format__` clusters
- [x] test_typing: runner no longer aborts. Root cause was optional dunder probes leaking a pending `AttributeError` (see above), not a malformed suite. Now runs all 709 tests.
- [ ] test_typing: port PEP 646 `TypeVarTuple` / PEP 612 `ParamSpec` substitution (`subsParameters`) — accounts for ~236 of the remaining errors
- [ ] test_genericalias: vendor `concurrent.futures` as a package (`_base`/`thread`/`process`)
- [ ] test_enum: vendor `pydoc`
- [ ] test_class: `test_detach_materialized_dict_no_memory` needs `_testcapi.set_nomemory` (allocator fault injection, infeasible on the Go runtime; deferred)
- [ ] test_metaclass: `__module__` prefix differs under the unittest harness (`test_metaclass` vs `test.test_metaclass`); confirm it is harness-only and not a gopy divergence

## Deferred / out of scope

- `test_class.test_detach_materialized_dict_no_memory` drives
  `_testcapi.set_nomemory`, which faults the C allocator. Go's runtime has no
  equivalent injection point; this is an environmental gap, not a behaviour
  divergence.
