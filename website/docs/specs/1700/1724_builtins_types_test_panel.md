---
id: "1724"
slug: 1724
title: "1724: Builtins / types test panel — 28-file gap analysis and CPython parity port"
sidebar_label: "1724 Builtins types panel"
description: "Full audit and port of the 28 Builtins/types test files from spec 1700 against CPython 3.14. Sixteen files are green today. Two panic, two crash on missing os surface, and eight fail on subsystem gaps (range getset attributes, dict-view set methods, property descriptor surface, memoryview buffer protocol, float.hex mantissa, hashlib blake2, UserDict/UserList edges). Every gap is traced to the exact CPython file and line with a concrete port plan."
---

## Status

Active. Branch `feat/v0.13.0-spec-1724-builtins-types-panel`.

## Goal

Drive every test in the spec 1700 Builtins/types panel (28 files) to zero
failures via faithful CPython 3.14 ports. No shims, no partial slices: when a
gate lands on a subsystem, port every function in that subsystem from CPython
as the single source of truth.

Sources of truth: `$HOME/cpython-314/`. Every cited function is read from
that tree before porting.

---

## Current test status (audit date: 2026-06-04)

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
| test_funcattrs | **PANIC** | getset setter nil-value (P0) | ready |
| test_structseq | **PANIC** | structseq-as-tuple cast (P0) | ready |
| test_range | FAIL (2f, 7e) | range getset + methods (P1) | ready |
| test_dictviews | FAIL (1f, 2e) | dict-view set ops (P2) | ready |
| test_property | FAIL (18f, 5e) | property descriptor surface (P3) | ready |
| test_memoryview | FAIL (49f, 36e) | memoryview buffer protocol (P4) | ready |
| test_strtod | FAIL (8f) | float.hex mantissa padding (P5) | ready |
| test_unicodedata | FAIL (4f, 1e) | hashlib blake2 + data file (P6) | ready |
| test_ucn | FAIL (2f) | hashlib blake2 + named sequences (P6) | ready |
| test_unicode_file | **CRASH** | os.supports_unicode_filenames (P7) | ready |
| test_unicode_file_functions | **CRASH** | os unicode path surface (P7) | ready |
| test_userdict | FAIL (1f, 1e) | UserDict `|` mappingproxy (P8) | ready |
| test_userlist | FAIL (1f) | UserList iterator free-after-iter (P8) | ready |
| test_buffer | not vendored | C-level buffer protocol API | out-of-scope |

Sixteen green, twelve to port. `test_buffer` stays out of scope (it drives the
C `Py_buffer` struct API directly, not Python-visible behaviour).

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

**Acceptance:** test_strtod green; test_float stays green.

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

**Blocked tests:** test_userdict (1 fail, 1 error), test_userlist (1 fail).

```
TypeError: unsupported operand type(s) for |: 'UserDict' and 'mappingproxy'
AssertionError: type(u) is not dict        (UserDict | mappingproxy)
AssertionError: False is not true          (UserList free_after_iterating)
```

1. **UserDict `|` mappingproxy** (`test_mixed_or` / `test_mixed_ior`):
   `UserDict.__or__` / `__ror__` must accept a `mappingproxy` (and any
   mapping) on the other side and produce a plain `dict`. This is in the
   vendored `collections/__init__.py` `UserDict` plus the `mappingproxy`
   `__ror__`; confirm `mappingproxy.__ror__` exists (port from
   `Objects/descrobject.c` mappingproxy if missing).
2. **UserList free_after_iterating** (`test_free_after_iterating`): the
   UserList iterator must drop its reference to the backing list when
   exhausted so a weakref to the list is reclaimed. Same class as the
   already-shipped list-iterator free-after-iterating fix (task #173); apply
   the equivalent to the UserList / list iterator path the test exercises.

**Acceptance:** test_userdict and test_userlist green.

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
- [ ] P1 range getset start/stop/step + count/index/richcompare/reduce/__index__ (test_range)
- [ ] P2 dict-view __contains__ + set ops + richcompare + isdisjoint + recursive repr (test_dictviews)
- [ ] P3 property __isabstractmethod__ + writable __doc__ + __name__/__set_name__ + subclass docstring rules (test_property)
- [ ] P4.a memoryview item/slice assignment + hash + context manager/release + count/index (test_memoryview bulk)
- [ ] P4.b memoryview cast edge cases + PEP 688 __buffer__ + PickleBuffer (test_memoryview tail)
- [ ] P5 float.hex full mantissa zero-padding (test_strtod)
- [ ] P6.a hashlib blake2b / blake2s (test_unicodedata, test_ucn checksums)
- [ ] P6.b vendor NormalizationTest-3.2.0.txt (test_unicodedata normalization)
- [ ] P6.c named-sequence resolver table (test_ucn named sequences)
- [ ] P7 os.supports_unicode_filenames + unicode path surface (test_unicode_file, test_unicode_file_functions)
- [ ] P8.a UserDict | mappingproxy -> dict (test_userdict)
- [ ] P8.b UserList iterator free-after-iterating weakref (test_userlist)
