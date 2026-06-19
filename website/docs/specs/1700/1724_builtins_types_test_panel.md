---
id: "1724"
slug: 1724
title: "1724: Builtins / types test panel — 28-file gap analysis and CPython parity port"
sidebar_label: "1724 Builtins types panel"
description: "Full audit and port of the 28 Builtins/types test files from spec 1700 against CPython 3.14. All 28 vendored files now pass. test_buffer stays out of scope (C Py_buffer struct API)."
---

## Status

Active. Branch `feat/v0.13.6-spec-1724-builtins-types-reaudit`.

Re-audited a second time under the [[1726]] bridge on 2026-06-19 (see the
re-audit section below). Twenty-eight of 29 files are fully green; only
`test_dict` carries residuals, and those are the two implementation-detail
divergences (PEP 412 split-table layout and refcount-exact `__del__` timing)
documented with CPython citations.

## Checklist

- [x] P0 panics cleared (`test_funcattrs`, `test_structseq`).
- [x] P1–P8 subsystem ports shipped (range, dict-views, property, memoryview,
  strtod, unicodedata/ucn, unicode-file, userdict/userlist).
- [x] First 1726 re-audit (2026-06-13): UCD instantiation refusal, `_pickle`
  `load_build` slots+dict, `WITH_DOC_STRINGS` sysconfig.
- [x] Second 1726 re-audit (2026-06-19): per-interpreter `int_max_str_digits`
  snapshot/restore (`test_int`), `getbuffer_with_null_view` (`test_bytes`),
  breakpoint `PyErr_Clear` before warn + type-creation namespace order
  (`test_builtin`), `_testinternalcapi.dict_getitem_knownhash` (`test_dict`).
- [x] Whole panel re-run for ground truth: 28/29 green.
- [ ] `test_dict` two residuals (`test_splittable_popitem`,
  `test_oob_indexing_dictiter_iternextitem`) — architectural, documented below,
  not planned to port (would reverse the borrow-model iterator / add PEP 412
  split tables for no Python-visible behaviour).

## Goal

Drive every test in the spec 1700 Builtins/types panel (28 files) to zero
failures via faithful CPython 3.14 ports. No shims, no partial slices: when a
gate lands on a subsystem, port every function in that subsystem from CPython
as the single source of truth.

Sources of truth: `$HOME/cpython-314/`. Every cited function is read from
that tree before porting.

---

## Zero-skip re-audit under the spec 1726 bridge (audit dates: 2026-06-13, 2026-06-19)

The table below was written before the [[1726]] bridge, which makes
`check_impl_detail()` report `cpython=True` so every `@cpython_only` test runs
on gopy instead of being skipped. Re-running the whole panel under the bridge
surfaced a handful of `@cpython_only` and missing-module cases the earlier
all-green pass never executed. The genuine gopy bugs are fixed in this branch;
the rest are behaviour that depends on a CPython implementation detail gopy does
not have, or a C subsystem gopy has never carried.

Reference interpreter for the skip/run decisions: brew `python@3.14` 3.14.5.

Panel result after the second re-audit (run from `test/cpython/`):

- 28 of 29 files fully green.
- `test_dict`: 2 failures, 1 skip (both failures are documented
  implementation-detail residuals).

### Fixed in the 2026-06-19 re-audit

- **`int_max_str_digits` leaked across subinterpreters.**
  `test_int_max_str_digits_is_per_interpreter` runs a child interpreter via
  `_testcapi.run_in_subinterp` and asserts the parent's limit is unchanged.
  CPython keeps the limit per-interpreter (`interp->long_state.max_str_digits`,
  `Include/internal/pycore_interp.h`); gopy parked it in a package global. Added
  a snapshot/restore hook (`imp.RegisterSubinterpSnapshot`) that captures the
  parent value on `PushSubinterp` and restores it on `PopSubinterp`, registered
  from `module/sys`. Fixes both `test_int` errors.
- **`getbuffer_with_null_view` was unported.** `test_bytes.test_obsolete_write_lock`
  calls a `_testcapi` helper that invokes `PyObject_GetBuffer` with a NULL
  `Py_buffer*` to force `BufferError`. Ported the helper. Fixes the `test_bytes`
  error.
- **`breakpoint()` left a stale thread exception, and type creation ordered the
  `__dict__`/`__weakref__` descriptors before the class namespace.**
  `test_builtin.test_envar_unimportable` swallows a `ModuleNotFoundError` in the
  default breakpoint hook in favour of a warning; gopy did not clear the thread
  exception the way `sys_breakpointhook` calls `PyErr_Clear`
  (`Python/sysmodule.c:658`), so a later handler observed the stale error. And
  `test_namespace_order` requires the managed-dict descriptors to be installed
  after the class namespace is copied in, so a user-provided `__dict__` wins
  (`type_new_descriptors` runs `PyDict_SetDefaultRef` after `type_new_set_attrs`,
  `Objects/typeobject.c:8136`). Deferred the descriptor install behind an
  `nsHasName` guard. Fixes `test_builtin`.
- **`_testinternalcapi.dict_getitem_knownhash` was missing.**
  `test_dict`'s CAPI test calls it. Ported `dict_getitem_knownhash`
  (`Modules/_testinternalcapi.c:1562`) plus a `GetItemKnownHashOrKeyError`
  helper. Clears the CAPI failure.

### Fixed in the 2026-06-13 re-audit

- **`unicodedata.UCD.__new__(UCD)` did not refuse instantiation.** A type that
  carries `Py_TPFLAGS_DISALLOW_INSTANTIATION` leaves `tp_new` NULL, and
  `tp_new_wrapper` (`Objects/typeobject.c:9843`) walks to the most-derived
  static base and raises `cannot create '...' instances` when that base's
  `tp_new` is NULL. gopy's `object.__new__` skipped the staticbase walk, so
  `support.check_disallow_instantiation` (which also probes `tp.__new__(tp)`)
  failed. Ported the walk into `objectNewBuiltin`. Fixes
  `test_unicodedata.test_disallow_instantiation`.
- **Pickling an object with both `__dict__` and `__slots__` dropped its
  state.** The `_pickle` decoder's `load_build` (`Modules/_pickle.c:5752`) was a
  partial port: it never split the protocol 2 `(state, slots)` tuple, so a
  `bytearray` subclass that declares `__slots__` alongside `__dict__` came back
  from a dump/load round trip with none of its attributes. Ported the rest of
  `load_build`. Fixes `test_bytes.ByteArraySubclassWithSlotsTest.test_pickle`.
- **`requires_docstrings` tests were skipped.** `support.MISSING_C_DOCSTRINGS`
  is derived from `sysconfig.get_config_var('WITH_DOC_STRINGS')`, which gopy
  left unset (None). gopy compiles docstrings into its built-ins, matching a
  `--with-doc-strings` build, so `sysconfig` now reports `1` and the
  docstring-gated tests run. Fixes the extra skip in `test_property`.

### Remaining residuals: implementation-detail and unported-subsystem divergences

These are not "skips gopy chose"; they are cases where matching CPython would
mean either reversing gopy's native implementation or porting a C subsystem the
project has never carried. They run under the bridge and surface as
errors/failures rather than skips.

- **`test_int` — `_pylong` / C `_decimal` whitebox (7 skips).** These
  `@cpython_only` tests assert that huge-int `int`↔`str` and `divmod` delegate
  to the pure-Python `_pylong` module, and several also require the C `_decimal`
  module. gopy implements int arithmetic and base conversion natively in Go
  (`math/big`), so the `_pylong` delegation hook simply does not exist; vendoring
  `_pylong.py` would not make `int()` call it without reversing the native
  implementation, and the C `_decimal`/libmpdec subsystem is unported. The
  observable `int` behaviour (results, error messages, `int_max_str_digits`
  limit) is already covered and green; only the delegation mechanism differs.
- **`test_dict` — split-table layout + `__del__` timing (2 failures).**
  `test_splittable_popitem` is `@cpython_only` and inspects the PEP 412
  split-table dict layout through `sys.getsizeof`; gopy uses a single combined
  dict representation, so a `popitem()` does not grow `sys.getsizeof` the way a
  split-to-combined transition does. `test_oob_indexing_dictiter_iternextitem`
  depends on `dictiter_iternextitem` recycling one `di_result` tuple and
  decreffing the previously yielded value to refcount zero, firing `__del__`
  mid-iteration (`Objects/dictobject.c:5697`). gopy's `IterNext` slot returns a
  **borrowed** reference (spec [[1727]]) and allocates a fresh tuple per advance,
  so the spent value's finalizer fires under the Go GC rather than synchronously;
  attempting the in-place reuse instead frees a tuple a generic consumer still
  holds (it broke `sorted(d.items())`, see `objects/dict_iter.go`). Both are
  interpreter-internal memory layout / refcount-timing details, not
  Python-visible behaviour, and matching either would reverse a deliberate gopy
  design choice. Left as documented residuals.
- **Environment-gated skips that match CPython under the CI locale/feature
  set:** `test_float.test_float_with_comma` (locale `decimal_point`, skipped
  under `LC_ALL=C` on both interpreters), `test_memoryview` ctypes cast (no
  ctypes C-FFI), `test_builtin` PtyTests (no pty+signal on the gopy build).
  These skip on both interpreters in the CI environment.

## Current test status (audit date: 2026-06-06, pre-bridge)

Run via `/tmp/gopy -m unittest <name>` from `test/cpython/`.

| Test | Result | Owning subsystem | Spec-1700 mark |
|------|--------|------------------|----------------|
| test_builtin | OK (133) | builtins module surface | done |
| test_int | OK (52) | int / long | done |
| test_long | OK (47) | int / long | done |
| test_float | OK (54) | float | done |
| test_complex | OK (37) | complex | done |
| test_bool | OK (31) | bool | done |
| test_bytes | OK (317) | bytes | done |
| test_str | OK (138) | str / unicode | done |
| test_list | OK (68) | list | done |
| test_tuple | OK (38) | tuple | done |
| test_dict | OK (120) | dict | done |
| test_set | OK (630) | set / frozenset | done |
| test_slice | OK (11) | slice | done |
| test_numeric_tower | OK (9) | numbers ABC | done |
| test_abstract_numbers | OK (7) | numbers module | done |
| test_userstring | OK (71) | UserString | done |
| test_funcattrs | OK | getset setter nil-value (P0, shipped) | done |
| test_structseq | OK | structseq-as-tuple cast (P0, shipped) | done |
| test_range | OK | range getset + methods + pickle compat (P1, shipped) | done |
| test_dictviews | OK | dict-view set ops + __contains__ + recursive repr (P2, shipped) | done |
| test_property | OK | property descriptor surface (P3, shipped) | done |
| test_memoryview | OK (171, 20 skipped) | gc traverse chain fix (P4.b, shipped) | done |
| test_strtod | OK | float.hex mantissa padding + long-mantissa parse (P5, shipped) | done |
| test_unicodedata | OK | hashlib blake2 + data file (P6, shipped) | done |
| test_ucn | OK | hashlib blake2 + named sequences (P6, shipped) | done |
| test_unicode_file | OK | os.supports_unicode_filenames (P7, shipped) | done |
| test_unicode_file_functions | OK | os unicode path surface (P7, shipped) | done |
| test_userdict | OK | `|`/`|=` + test_fromkeys (P8.a, shipped) | done |
| test_userlist | OK (54) | free-after-iter gc chain fix (P8.b, shipped) | done |
| test_buffer | not vendored | C-level buffer protocol API | out-of-scope |

Twenty-eight green (minus one out-of-scope), one out of scope. All 28 vendored
files pass. `test_buffer` stays out of scope (it drives the C `Py_buffer`
struct API directly, not Python-visible behaviour).

The two previously blocked tests (`test_memoryview` weakref/gc cases,
`test_userlist` free-after-iterating) are now green. The root cause was that
inline tracebacks created in `attachFrameTraceback` were never added to the
GC's tracked set, making the Exception→Traceback→Frame chain invisible to
`subtractRefs`. Added `gc.TrackSilent` for inline tracebacks, split dicts, and
exception objects, plus `TpTraverse` on Traceback and Exception types. Using
the silent variant avoids bumping gen0.count during execution, which would
otherwise trigger premature auto-collection that races with suspended
generators (the regression in `test_clear_executing_generator`).

---

## P0 — Panics (must never crash)

Two files abort the interpreter. These are the highest priority: a panic is a
hard correctness failure regardless of the assertion behind it.

### P0.1 — getset setter dereferences a nil value on attribute deletion

**Blocked test:** test_funcattrs.

```
panic: runtime error: invalid memory address or nil pointer dereference
  objects/object.go:624        objectSetClass: value.Type().Name on nil value
  objects/descr.go:187         getsetDescrSet: d.fset(owner, value)
  objects/generic_attr.go:102
  objects/protocol.go:504      SetAttr
  builtins/attrs.go:91
```

`del obj.__class__` (and the same shape for any read/write getset attribute)
routes through `getsetDescrSet` with `value == nil` to signal deletion. The
setter `objectSetClass` immediately does `value.Type().Name`, dereferencing
nil. CPython's `object_set_class` guards this first:

```c
// Objects/typeobject.c:7208 object_set_class
if (value == NULL) {
    PyErr_SetString(PyExc_TypeError, "can't delete __class__ attribute");
    return -1;
}
```

**Port plan.** Add the `value == nil` guard to `objectSetClass` returning the
CPython message. Then audit every getset setter registered in `objects/` and
`builtins/` for a nil-value (deletion) path: any setter that dereferences
`value` before a nil check must port CPython's deletion guard (most raise
`TypeError: can't delete the X attribute` or `AttributeError`). The descriptor
layer (`getsetDescrSet`) already forwards nil correctly; the bug is purely in
the leaf setters.

**Acceptance:** `del f.__class__`, `del f.__name__`, and the rest of
test_funcattrs run without panic and match CPython's error text. No getset
setter dereferences a nil value.

### P0.2 — structseq passed to a tuple slot that casts to `*Tuple`

**Blocked test:** test_structseq.

```
panic: interface conversion: objects.Object is *objects.StructSeq, not *objects.Tuple
  objects/tuple.go:470   tupleConcat: at := a.(*Tuple)
  objects/tuple.go:287   tuple sq_concat
  objects/method_descr.go:167
  objects/abstract_number.go:686 / :222
```

`PyStructSequence` is a tuple subclass in CPython (`tp_base = &PyTuple_Type`),
so `structseq + x`, `structseq * n`, indexing, and comparison all run the
inherited tuple sequence slots, which read `ob_item` directly. gopy models
`StructSeq` as a separate Go struct, and the tuple slots cast their operand
with an unchecked `a.(*Tuple)`, which panics on a `*StructSeq`.

**Port plan.** Make the tuple sequence/number slots accept any tuple-subclass
instance by extracting the item slice through a helper rather than an unchecked
cast. Two faithful options, pick the one that matches the existing object
model:

1. Give `StructSeq` an accessor returning its `[]Object` items and route the
   tuple slots (`tupleConcat`, `tupleRepeat`, `tupleItem`, `tupleContains`,
   `tupleRichCompare`, `tupleSubscript`, `tupleHash`) through a
   `tupleItemsOf(Object) ([]Object, bool)` helper that handles both `*Tuple`
   and `*StructSeq`.
2. Embed a `*Tuple` view inside `StructSeq` so the inherited slots see real
   tuple storage.

Mirror CPython: `tupleconcat` (`Objects/tupleobject.c:625`) requires the right
operand to be an exact-or-subclass tuple, builds a fresh `tuple` (never a
structseq) of length `Py_SIZE(a)+Py_SIZE(b)`. The left operand may be a
subclass.

**Acceptance:** structseq arithmetic, indexing, slicing, `+`, `*`, `in`,
comparison, and hashing all run without panic and produce plain `tuple`
results where CPython does. test_structseq green.

---

## P1 — range type: missing getset attributes and sequence methods

**Blocked test:** test_range (2 fail, 7 error).

```
AttributeError: 'range' object has no attribute 'start'
```

The `range` object is missing the `start` / `stop` / `step` read-only getset
attributes, the `count` / `index` methods, rich comparison (`range == range`
by value), `__reduce__` for pickle, and the `__index__`-driven constructor /
membership path.

**Port plan.** Port the full `range` type surface from
`Objects/rangeobject.c`:

- `start` / `stop` / `step` getset (`range_getsets`, line ~700): each returns
  the stored PyObject field.
- `range.count(value)` (`range_count`, line ~600) and `range.index(value)`
  (`range_index`, line ~620): membership by value with the integer fast path
  and the general `PyObject_RichCompareBool` fallback.
- `range_richcompare` (line ~500): two ranges are equal iff they generate the
  same sequence (`range_equals` compares length, then start, then step when
  length > 1).
- `range_reduce` / `__reduce__` (line ~780) for pickle round-trip.
- `range.__contains__` integer fast path `range_contains` (line ~450) plus the
  `__index__` coercion of the constructor arguments (`range_from_array` /
  `compute_range_length`).
- `test_user_index_method`: `range(MyIndexClass())` must call `__index__`.

**Acceptance:** test_range green, including `test_attributes`,
`test_comparison`, `test_count`, `test_index`, `test_large_range`,
`test_user_index_method`, and the pickle compat tests.

**Shipped.** All 29 test_range cases pass. `start` / `stop` / `step` are
read-only getsets, `__hash__` hashes the (length, start_or_None,
step_or_None) tuple, `__bool__` reads the stored length directly,
`count` / `index` take the exact-int fast path and fall back to an
`__eq__` iterator search for subclasses, and subscripting computes the
item in arbitrary precision (so `x[sys.maxsize+1]` raises IndexError).
The constructor coerces every operand through `PyNumber_Index`.

`test_iterator_unpickle_compat` needed the `_compat_pickle` two-to-three
name mapping in the Go `_pickle` decoder: `find_class` now remaps GLOBAL
references through `NAME_MAPPING` then `IMPORT_MAPPING` for proto < 3 with
fix_imports on, and imports the target module through the vm hook instead
of only reading sys.modules.

---

## P2 — dict views: missing `__contains__` and set operations

**Blocked test:** test_dictviews (1 fail, 2 error).

```
AttributeError: 'dict_keys' object has no attribute '__contains__'
RecursionError: maximum recursion depth exceeded while getting the repr of an object
  frozenset(d3.keys()) & d4.keys()  -> not a set
```

The `dict_keys` / `dict_items` view objects lack the explicit `__contains__`
slot wrapper, the set-algebra operators (`&`, `|`, `-`, `^`, and their
reflected forms) that return a real `set`, the `isdisjoint` method, the
ordering comparisons (`<`, `<=`, `>`, `>=` as subset/superset), and the
recursive-repr guard.

**Port plan.** Port the dict-view set protocol from
`Objects/dictobject.c`:

- `dictview_contains` (`PyDictKeys`/`PyDictItems`, line ~5000) wired as the
  `__contains__` method and `sq_contains`.
- `dictviews_and` / `_or` / `_sub` / `_xor` (line ~5100) building a `set` via
  `set_copy` + `_PyObject_CallMethodId(result, &PyId_difference_update, ...)`
  etc. These must accept any iterable on the right and return `set`.
- `dictview_richcompare` (line ~5060): subset / superset / equality of two
  views via `all_contained_in`.
- `dictviews_isdisjoint` (line ~5200).
- `dictview_repr` (line ~5240) with `Py_ReprEnter`/`Py_ReprLeave` recursion
  guard so a self-referential dict view raises `RecursionError` instead of
  looping (the `test_recursive_repr` error).

**Acceptance:** test_dictviews green, including `test_keys_set_operations`,
`test_compare_error`, and `test_recursive_repr`.

**Shipped.** test_dictviews is green (16 tests). The set-algebra operators
already returned a set in most cases; the gap was that they cloned the
shape of the left operand, so `frozenset(d.keys()) & other` yielded a
frozenset. `dictViewBinop` now forces the left operand through
`mutableSetCopy` so the result is always a plain `set`, matching
`PySet_New`. Registered `__contains__` method descriptors on the keys and
items views forwarding to their `sq_contains` slot, and added the
`ReprEnter`/`ReprLeave` recursion guard to `dictViewRepr`. Richcompare and
`isdisjoint` were already present.

---

## P3 — property descriptor: missing attribute surface and docstring rules

**Blocked test:** test_property (18 fail, 5 error).

```
AttributeError: 'property' object has no attribute '__isabstractmethod__'
TypeError: 'property' object has no attributes (assign to .__doc__)
A.foo.__name__ == 'foo'   (missing __name__/__set_name__)
```

The `property` object is missing `__isabstractmethod__` (computed from the
abstractness of fget/fset/fdel), a writable `__doc__`, the `__name__` set by
`__set_name__`, and the subclass docstring-copy rules (a `property` subclass
without `__doc__` in `__slots__` copies the getter docstring; with the slot it
is silently dropped; `issue41287` and friends).

**Port plan.** Port from `Objects/descrobject.c` (property section, line
~1450):

- `property_get_isabstractmethod` getset: true when any of fget/fset/fdel has
  a true `__isabstractmethod__`.
- `property_init_impl` / `property_copy`: the `__doc__` handling, including
  the "getter docstring copy for subclasses, unless `__doc__` lives in
  `__slots__`" branch (the `getter_doc` flag).
- `property_set_name` (`__set_name__`) storing `__name__`.
- Writable `__doc__` (`property` has a settable `prop_doc`).

**Acceptance:** test_property green, all PropertySubclassTests and
PropertyTests.

---

## P4 — memoryview: buffer-protocol surface

**Blocked test:** test_memoryview (49 fail, 36 error). Largest single gap.

Failure histogram:

```
  7  'memoryview' object does not support item assignment   (mv[i] = x)
  6  unhashable type: 'memoryview'                            (hash of read-only)
  6  does not support the context manager protocol           (__enter__/__exit__/release)
  6  'memoryview' object has no attribute 'index'
  6  'memoryview' object has no attribute 'count'
  6  BufferError not raised by hex / hash                     (release semantics)
  3  'B' != 'i' / format / cast edge cases
  1  module has no attribute "PickleBuffer"
  1  'memoryview' object has no attribute '__buffer__'        (PEP 688)
```

**Port plan.** Port the memoryview surface from `Objects/memoryobject.c`:

- `memory_ass_sub` (item / slice assignment, line ~1300) for writable views,
  raising `TypeError` for read-only.
- `memory_hash` (line ~3100): hash a read-only view of a hashable format,
  raising `ValueError` for non-contiguous / writable, caching the result.
- `memory_enter` / `memory_exit` / `memory_release` (line ~3300) context
  manager and `release()`, flipping the view to released and raising
  `BufferError` on any later access of an exported view.
- `memory_count` / `memory_index` (line ~3000) sequence methods.
- `memory_cast` (line ~2600) remaining format/shape edge cases
  (`test_cast_*`).
- PEP 688 `__buffer__` / `__release_buffer__` and `pickle.PickleBuffer` (defer
  PickleBuffer to a sub-task if it pulls in the pickle buffer machinery; if it
  does, log the scope cut in the checklist rather than silently skipping).

This phase may split into P4.a (assignment + hash + context manager +
count/index, the bulk of the failures) and P4.b (cast edge cases + PEP 688
buffer dunders) if one PR gets large.

**Acceptance:** test_memoryview green, or, if PickleBuffer is cut, every
non-PickleBuffer test green with the cut logged here.

**Status (in progress).** Shipped the writable/read-only assignment surface,
the count/index/context-manager/release methods, `memory_hash` with the
gh-142664 use-after-free guard (export-pin around hashing the underlying
object, BufferError on re-entrant release, `__hash__` descriptor wired to the
slot), and default-pickle rejection (TypeError, with reduce errors propagated
unchanged). test_memoryview went from 49f/36e to 17f/2e.

The 17 remaining failures and 2 errors are all getrefcount / GC-cycle parity:
test_getbuffer / test_refs / test_setitem_writable assert
`sys.getrefcount` returns to its pre-op value, and test_weakref / test_gc /
the two reference_loop errors expect deterministic cycle reclaim. The root
cause is that the eval loop omits CPython's DECREF_INPUTS on
BINARY_OP / COMPARE_OP / BINARY_SUBSCR, so every comparison and arithmetic
op leaks a reference on its operands. This cannot be fixed in isolation:
adding the operand release to COMPARE_OP drives frozensets stored as dict /
set keys (which the container never incref'd) to refcount zero and corrupts
their later lookup (test_set.TestGraphs.test_cuboctahedron). Closing these
out requires first auditing every container storage path to incref what it
retains. Tracked separately from this panel.

---

## P5 — float.hex mantissa not zero-padded

**Blocked test:** test_strtod (8 fail).

```
AssertionError: '0x0.0p+0' != '0x0p+0'
AssertionError: '0x1.0000000000000p+0' != '0x1p+0'
```

gopy's `float.hex()` drops the fractional mantissa digits. CPython always
emits the full 13-hex-digit mantissa with the `0x1.` prefix (`float_hex` →
`_Py_dg_dtoa` path in `Objects/floatobject.c:float___getformat___impl`
neighbour `float_hex`, line ~1600). The format is
`[sign]0x{lead}.{13 hex digits}p{+/-}{exp}` where `lead` is 0 for zero/subnormal
and 1 otherwise, trailing mantissa zeros retained.

**Port plan.** Port `float_hex` (`Objects/floatobject.c:1600`) faithfully,
including the `0x0.0p+0` form for zero and the full mantissa for normals.
Confirm `float.fromhex` round-trips (already green via test_float, keep it
green).

**Port plan (parsing).** test_strtod also exercises the decimal-to-double
boundary. Go's `strconv.ParseFloat` caps the mantissa digits it reads, so a
literal with a long digit run can round to the wrong binade (values near the
overflow boundary become inf, tiny values spelled with thousands of zeros
underflow to 0). When the mantissa carries more than 17 significant digits,
recompute the value exactly via `big.Rat` and take its correctly rounded
`float64`, matching `_Py_dg_strtod`. The fix lives in `pystrconv.ParseFloat`,
the path `float()` actually uses (not `objects.FloatFromString`).

**Acceptance:** test_strtod green and stable across runs; test_float stays
green.

**Shipped.** float.hex ported in objects/float_methods.go; long-mantissa
exact recompute added to pystrconv/strtod.go. test_strtod OK across 3 runs.

---

## P6 — hashlib blake2 + unicode data files

**Blocked tests:** test_unicodedata (4 fail, 1 error), test_ucn (2 fail).

```
ValueError: unsupported hash type blake2b / blake2s
OSError: open NormalizationTest-3.2.0.txt: no such file or directory
AssertionError: '#️⃣' != None    (named sequence resolver)
```

Three independent gaps:

1. **blake2b / blake2s** are missing from `hashlib`. The checksum tests build a
   `hashlib.blake2b()` over the full Unicode property table. Port the BLAKE2
   subsystem into `module/hashlib` (or wherever the other digests live),
   mirroring `Modules/blake2module.c` and the reference `Modules/_blake2/`.
   Go's `golang.org/x/crypto/blake2b` and `blake2s` give the primitives; the
   port wires them to the `hashlib` constructor surface with the CPython
   parameter names (digest_size, key, salt, person, ...).
2. **NormalizationTest-3.2.0.txt** must be vendored next to test_unicodedata
   (the harness already loads the current `NormalizationTest.txt`); add the
   3.2.0 data file the same way (per the spec-1721 "vendor adjacent data
   files" rule).
3. **Named-sequence resolver** (`test_named_sequences_sample`): `\N{KEYCAP}`
   style multi-codepoint named sequences resolve to `None` instead of the
   sequence. Port the named-sequence table lookup from `Modules/unicodedata.c`
   (`_getucname` / the `named_sequences` table) so `unicodedata.lookup` returns
   the multi-character sequence.

**Port plan.** blake2 first (it unblocks both checksum tests), then the data
file, then the named-sequence table. Each is an independent commit.

**Acceptance:** test_unicodedata and test_ucn green.

---

## P7 — os unicode-filename surface

**Blocked tests:** test_unicode_file, test_unicode_file_functions (both crash
at import).

```
AttributeError: module has no attribute "supports_unicode_filenames"
```

`os.supports_unicode_filenames` is missing, aborting both modules at import.
Beyond the flag, these files exercise unicode round-trips through `os.stat`,
`os.rename`, `os.listdir`, `os.path` on non-ASCII paths.

**Port plan.** Add `os.supports_unicode_filenames` (True on darwin/win,
filesystem-encoding dependent on posix, mirroring `Modules/posixmodule.c` /
`os.py`). Then run both files and port any remaining os/path surface they hit
(filesystem encoding, `os.fsencode`/`os.fsdecode` on the path arguments). Keep
the port faithful to CPython's `os` and `posixpath`/`ntpath`.

**Acceptance:** both files import and run; green on darwin (the CI matrix also
runs ubuntu + windows, so guard platform-specific expectations the way CPython
does with `requires_unicode_filenames`-style skips).

---

## P8 — UserDict / UserList edges

**Status (in progress).** The `|` / `|=` half is shipped; the two
remaining failures are both GC-reclaim, blocked on the refcount audit
(P8.b, see below).

**P8.a — UserDict `|` mappingproxy (shipped).** The vendored
`collections/__init__.py` `UserDict.__or__` / `__ror__` already match
CPython 3.14 byte for byte: they only pair with `UserDict` / `dict` and
return `NotImplemented` otherwise. The real gap was on the operand side.
`mappingproxy` carried no number slots, and `dict` exposed `__ior__` only
as a method descriptor, never as `nb_inplace_or`. So:
- `UserDict | mappingproxy` raised `TypeError`, because neither operand's
  slot accepted it (`mappingproxy` had no `nb_or` to re-dispatch
  `UserDict | dict`).
- `dict |= UserDict` fell through the in-place path (no `nb_inplace_or`)
  to `UserDict.__ror__`, rebuilding the value as a `UserDict` instead of
  updating the `dict` in place.

Fix: port `dict_or` / `dict_ior` (`Objects/dictobject.c`) and
`mappingproxy_or` / `mappingproxy_ior` (`Objects/descrobject.c`) onto the
`Number` slot of each type. `dict |= mapping` now keeps `dict` identity;
`UserDict | mappingproxy` produces a `UserDict`. `test_mixed_or` and
`test_mixed_ior` pass.

**P8.b — GC reclaim (blocked).** Two failures remain and both are the
refcount/GC drift tracked in the eval-loop audit:
- test_userdict `test_fromkeys`: passes in isolation and in pairs, fails
  only in the full module. `d.fromkeys(g())` over a generator returns
  `{}` once enough prior comparisons have inflated refcounts.
- test_userlist `test_free_after_iterating`: the backing instance is held
  by a Python-level `Sequence.__iter__` generator and is never
  reclaimed, so the subclass `__del__` does not fire. Same class as the
  C list-iterator fix (task #173), but reached through a generator frame.

Both wait on the container-incref audit; see the eval-loop DECREF_INPUTS
follow-up task.

**Acceptance:** P8.a green (test_mixed_or, test_mixed_ior); P8.b deferred
with P4.b to the refcount audit.

---

## Phase order and PR plan

Land in priority order, each phase its own commit (or PR when it stands
alone), CI green before moving on:

1. P0 panics (funcattrs, structseq) — crashes first.
2. P5 float.hex (small, self-contained).
3. P1 range, P2 dict-views, P3 property (independent type surfaces).
4. P8 UserDict / UserList edges.
5. P7 os unicode filenames.
6. P6 hashlib blake2 + data files + named sequences.
7. P4 memoryview (largest; may split P4.a / P4.b).

`test_buffer` stays out-of-scope.

---

## Checklist

- [x] P0.1 getset setter nil-value deletion guard (test_funcattrs)
- [x] P0.2 structseq routes through tuple slots without unchecked cast (test_structseq)
- [x] P1 range getset start/stop/step + count/index/richcompare/reduce/__index__ + _compat_pickle two-to-three mapping (test_range)
- [x] P2 dict-view __contains__ + set ops + richcompare + isdisjoint + recursive repr (test_dictviews)
- [x] P3 property __isabstractmethod__ + writable __doc__ + __name__/__set_name__ + subclass docstring rules (test_property)
- [x] P4.a memoryview item/slice assignment + hash (gh-142664 use-after-free guard) + context manager/release + count/index + default-pickle rejection (test_memoryview 49f/36e -> 17f/2e)
- [x] P4.b memoryview weakref / GC-cycle reclaim parity: shipped via GC chain fix (gc.TrackSilent on inline tracebacks + TpTraverse on Traceback/Exception). test_memoryview now OK (171, 20 skipped).
- [x] P5 float.hex full mantissa zero-padding + exact long-mantissa decimal parsing (test_strtod)
- [x] P6.a hashlib blake2b / blake2s (test_unicodedata, test_ucn checksums)
- [x] P6.b vendor NormalizationTest-3.2.0.txt (test_unicodedata normalization)
- [x] P6.c named-sequence resolver table (test_ucn named sequences)
- [x] P7 os.supports_unicode_filenames + unicode path surface (test_unicode_file, test_unicode_file_functions)
- [x] P8.a dict / mappingproxy nb_or + nb_inplace_or: `dict |= mapping` keeps dict identity, `UserDict | mappingproxy` -> UserDict; test_fromkeys now green (test_userdict OK; test_mixed_or, test_mixed_ior)
- [x] P8.b free-after-iterating reclaim: shipped via GC chain fix (same batch as P4.b). test_userlist now OK (54).
- [x] P9.a spec 1726 bridge re-audit: object.__new__ staticbase disallow-instantiation walk (test_unicodedata.test_disallow_instantiation)
- [x] P9.b spec 1726 bridge re-audit: _pickle load_build slot+dict state restore (test_bytes.ByteArraySubclassWithSlotsTest.test_pickle)
- [x] P9.c spec 1726 bridge re-audit: sysconfig WITH_DOC_STRINGS so requires_docstrings tests run (test_property)
- [ ] P9.d documented divergence (no port): test_int subinterpreters (PEP 684) + _pylong/_decimal whitebox delegation (gopy int is native Go big.Int)
- [ ] P9.e documented divergence (no port): test_bytes getbuffer_with_null_view (C-API NULL Py_buffer contract)
- [ ] P9.f documented divergence (no port): test_dict PEP 412 split-table layout + __del__ refcount-drop timing
