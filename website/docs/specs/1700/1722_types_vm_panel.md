---
id: "1722"
slug: 1722
title: "1722: types / VM panel — int, complex, slice, iter, test infrastructure"
sidebar_label: "1722 types / VM panel"
description: "Close all failing ready-tests in the spec 1700 panel that regress on int(), complex(), slice, test_iter, and missing test-infrastructure modules (fractions, string_tests, mapping_tests, symtable). Every gap is traced to the exact CPython 3.14 source function."
---

## Status

Active. Branch `feat/v0.12.7-vm-audit`.

## Goal

Advance every failing `ready` row in spec 1700 to `done` by porting the
specific CPython functions identified below. No test patching — fix gopy until
CPython's unmodified test file passes.

Shipping criterion: test_slice, test_int, test_long, test_complex, test_float,
test_builtin, test_compare, test_str, test_bytes, test_dict, test_frame,
test_symtable, test_iter, and test_py_compile all pass end-to-end.

## Sources of truth

CPython 3.14 at `$HOME/cpython-314/`. Every cited function was read from that
tree. Do not diverge from CPython's logic without a comment explaining why.

---

## P1 — Missing test-infrastructure modules

Four modules that many `ready` tests import at the top of the file are absent
from gopy's stdlib or test corpus. All four cause import-time crashes before
any test method runs.

### P1.1 — `fractions` module

**CPython file:** `$HOME/cpython-314/Lib/fractions.py` (965 lines, pure Python)

**Blocked tests:** test_float, test_builtin, test_compare, test_numeric_tower
all crash with `ModuleNotFoundError: No module named 'fractions'`.

**Fix:** copy `Lib/fractions.py` verbatim into `stdlib/fractions.py`. The
module imports `math`, `numbers`, `operator`, `re`, and `sys`, all present in
gopy stdlib. Verify with `go run ./cmd/gopy -c "import fractions; print(fractions.Fraction(1,3))"`.

### P1.2 — `test.string_tests`

**CPython file:** `$HOME/cpython-314/Lib/test/string_tests.py` (1598 lines)

**Blocked tests:** test_bytes, test_str, test_userstring all crash with
`ModuleNotFoundError: No module named 'test.string_tests'`.

**Fix:** vendor `Lib/test/string_tests.py` into `test/cpython/string_tests.py`
and add a shim `test/cpython/test/__init__.py` + `test/cpython/test/string_tests.py`
so `from test import string_tests` resolves relative to the test runner's
working directory. Alternatively, adjust `sys.path` in the test runner before
dispatching.

### P1.3 — `test.mapping_tests`

**CPython file:** `$HOME/cpython-314/Lib/test/mapping_tests.py` (671 lines)

**Blocked tests:** test_dict, test_frame (both crash at import).

**Fix:** same vendoring approach as P1.2. Place at
`test/cpython/test/mapping_tests.py`.

### P1.4 — `symtable` module

**CPython file:** `$HOME/cpython-314/Lib/symtable.py` (312 lines, pure Python)

The pure-Python `symtable` module wraps the `_symtable` C extension. In gopy
`_symtable` is already ported (the symbol-table builder returns Python objects).

**Blocked test:** test_symtable crashes with `ModuleNotFoundError: No module named 'symtable'`.

**Fix:** copy `Lib/symtable.py` verbatim into `stdlib/symtable.py`. Verify any
`_symtable` API differences against gopy's compiler/symtable.go.

---

## P2 — `int` type gaps (`Objects/longobject.c`, `Objects/unicodeobject.c`)

### P2.1 — Unicode whitespace and decimal-digit normalization

**CPython functions:**
- `Objects/unicodeobject.c:9612 _PyUnicode_TransformDecimalAndSpaceToASCII`
- `Objects/longobject.c:3104 PyLong_FromUnicodeObject`

CPython normalizes the input string before parsing:
1. Every Unicode space character (`Py_UNICODE_ISSPACE`) → ASCII `' '`.
2. Every Unicode decimal digit (`Py_UNICODE_TODECIMAL`) → ASCII `'0'+decimal`.
3. Any other non-ASCII character → `'?'` (stops parsing, raises ValueError).

```c
// Objects/unicodeobject.c:9612
for (i = 0; i < len; ++i) {
    Py_UCS4 ch = PyUnicode_READ(kind, data, i);
    if (ch < 127)        { out[i] = ch;              }
    else if (Py_UNICODE_ISSPACE(ch)) { out[i] = ' '; }
    else {
        int decimal = Py_UNICODE_TODECIMAL(ch);
        out[i] = (decimal < 0) ? '?' : ('0' + decimal);
    }
}
```

**Status in gopy:** `builtins/ctor.go:157 trimSpace` uses a byte-level ASCII
space check. Unicode code points above 127 are not classified and `big.Int.SetString`
rejects them.

**Failing tests:**
- `int("\N{EM SPACE}-3\N{EN SPACE}")` → should be `-3`
- `int("१२३४५६७८९०1234567890")` → should be `1234567890...`
- `int("٣")` → should be `3` (Arabic-Indic digit three)

**Fix:** before passing the string to `big.Int.SetString`, iterate the Go
`[]rune` and apply the same transformation: `unicode.IsSpace(r)` → `' '`,
`unicode.ToLower('0' + int(r - '0'))` via `unicode.IsDigit` → decimal value.
Use `unicode.In(r, unicode.Nd)` and the `golang.org/x/text/unicode/norm`
package's decimal digit lookup, or the `unicodedata.Decimal()` function already
ported in gopy.

### P2.2 — `__index__` coercion for `int()` base argument

**CPython function:** `Objects/longobject.c:5895 long_new_impl`

```c
// Objects/longobject.c:5912
if (obase != NULL) {
    base = PyNumber_AsSsize_t(obase, NULL);
    if (base == -1 && PyErr_Occurred()) return NULL;
}
```

`PyNumber_AsSsize_t` calls `__index__` on the object before converting.

**Status in gopy:** `builtins/ctor.go:55`:
```go
baseInt, ok := args[1].(*objects.Int)
if !ok {
    return nil, fmt.Errorf("TypeError: int() base must be an integer")
}
```
Hard type-assert; objects with `__index__` are rejected.

**Failing test:** `int('101', base=MyIndexable(2))` where `MyIndexable.__index__` returns `2`.

**Fix:** call `objects.Index(args[1])` (the `__index__` protocol helper) to coerce
the base to an integer before asserting `*objects.Int`.

### P2.3 — `sys.int_info` structseq missing

**CPython function:** `Objects/longobject.c:6609 PyLong_GetInfo`

```c
// Objects/longobject.c:6593
static PyStructSequence_Field int_info_fields[] = {
    {"bits_per_digit",             ...},   // PyLong_SHIFT = 30
    {"sizeof_digit",               ...},   // sizeof(digit) = 4
    {"default_max_str_digits",     ...},   // 4300
    {"str_digits_check_threshold", ...},   // 640
    {NULL, NULL}
};
```

**Status in gopy:** `module/sys/module.go` exposes `int_max_str_digits`
get/set but has no `int_info` attribute at all.

**Failing test:** `test_long.py:11` → `AttributeError: module has no attribute 'int_info'`
causes the entire test_long module to crash before any test runs.

**Fix:** add a `structseq`-style named tuple to the `sys` module with fields
`bits_per_digit=30`, `sizeof_digit=4`, `default_max_str_digits=4300`,
`str_digits_check_threshold=640`. Bind it as `sys.int_info`.

### P2.4 — Runtime int-to-str / str-to-int digit limit not enforced

**CPython function:** `Objects/longobject.c:30 _MAX_STR_DIGITS_ERROR_FMT_TO_INT`

CPython checks `sys.int_max_str_digits` in both `PyLong_FromUnicodeObject` and
`PyLong_Format` (the str(int) path). gopy only enforces the limit in the
parser (compile time), not at runtime during `int(string)` or `str(big_int)`.

**Failing tests:** `test_denial_of_service_prevented_str_to_int`,
`test_denial_of_service_prevented_int_to_str` in test_int.

**Fix:** in `builtins/ctor.go` (int-from-string path) and `objects/int.go` (int
repr/str path), read `sys.int_max_str_digits`, count decimal digits in the
string/repr, and raise `ValueError` if count exceeds the limit.

---

## P3 — `complex` type gaps (`Objects/complexobject.c`)

### P3.1 — `complex.__complex__` method missing

**CPython function:** `Objects/complexobject.c:912 complex___complex___impl`

```c
static PyObject *
complex___complex___impl(PyObject *self) {
    if (PyComplex_CheckExact(self))
        return Py_NewRef(self);
    return PyComplex_FromCComplex(((PyComplexObject *)self)->cval);
}
```

Listed in `complex_methods` at `Objects/complexobject.c:1371`.

**Status in gopy:** `ComplexType` has no `__complex__` method descriptor.

**Failing test:** `z.__complex__()` → `AttributeError: 'complex' object has no attribute '__complex__'`.

**Fix:** add a method descriptor on `ComplexType` with this logic: for exact
`*Complex` return self (with Incref); for subclasses return a new complex from
the value.

### P3.2 — `complex()` constructor does not try `__complex__` protocol

**CPython functions:**
- `Objects/complexobject.c:488 try_complex_special_method`
- `Objects/complexobject.c:522 PyComplex_AsCComplex`
- `Objects/complexobject.c:1182 complex_new_impl`

```c
// Objects/complexobject.c:488
static PyObject *
try_complex_special_method(PyObject *op) {
    PyObject *f = _PyObject_LookupSpecial(op, &_Py_ID(__complex__));
    if (f) {
        PyObject *res = PyObject_CallNoArgs(f);
        ...
        return res;
    }
    return NULL;
}
```

`complex_new_impl` calls `PyComplex_AsCComplex(r)` which first tries
`try_complex_special_method`, then falls back to `__float__`, then `__index__`.

**Status in gopy:** `builtins/ctor.go:682 toFloat` only checks
`Number.Float` slot; `__complex__` is never tried.

**Failing test:** `complex(WithComplex(4.25+0.5j))` where `WithComplex` defines
`__complex__` → `TypeError`.

**Fix:** in the `complex()` constructor path, before checking `Number.Float`,
call `objects.LookupSpecial(o, "__complex__")` and use its result if found.

### P3.3 — `complex.from_number` classmethod missing

**CPython function:** `Objects/complexobject.c:1301 complex_from_number_impl`

```c
static PyObject *
complex_from_number_impl(PyTypeObject *type, PyObject *number) {
    Py_complex cv = PyComplex_AsCComplex(number);
    if (PyErr_Occurred()) return NULL;
    return complex_subtype_from_doubles_impl(type, cv.real, cv.imag);
}
```

Listed as `METH_O | METH_CLASS` in `complex_methods`.

**Status in gopy:** absent from `objects/complex.go` and `ComplexType` descriptors.

**Failing test:** `complex.from_number(3.14)` → `AttributeError`.

**Fix:** add a classmethod descriptor `from_number` on `ComplexType` that calls
`PyComplex_AsCComplex` logic (try `__complex__`, `__float__`, `__index__`) and
returns a new complex.

### P3.4 — `complex.__getnewargs__` missing

**CPython function:** `Objects/complexobject.c:871 complex___getnewargs___impl`

```c
static PyObject *
complex___getnewargs___impl(PyComplexObject *self) {
    Py_complex c = self->cval;
    return Py_BuildValue("(dd)", c.real, c.imag);
}
```

**Status in gopy:** not present.

**Fix:** add `__getnewargs__` method descriptor returning a tuple `(real, imag)`.

### P3.5 — `complex.__truediv__` not exposed as dunder method

**CPython mechanism:** `Objects/typeobject.c add_operators` + `slotdefs[]`

CPython's `add_operators` iterates `slotdefs` and for each slot wrapper
(e.g., `slot_nb_true_divide`) adds a `__truediv__` wrapper descriptor to the
type's dict. This makes `complex.__truediv__` accessible as an attribute.

**Status in gopy:** `ComplexType.Number.TrueDivide` is wired at the Go level
but there is no `__truediv__` descriptor in `ComplexType`'s descriptor table.
`complex.__truediv__(2+0j, 1+1j)` → `AttributeError`.

**Fix:** for each numeric slot wired in `ComplexType.Number`, add the
corresponding `__op__` method descriptor. This also applies to `__add__`,
`__sub__`, `__mul__`, `__abs__`, `__neg__`, `__pos__`, `__eq__`, `__ne__`,
`__bool__`, `__hash__`, `__repr__`, `__str__`. The existing `addDescriptorSlotWrappers`
machinery may handle some; audit which dunders are missing.

### P3.6 — Negative zero not preserved in complex mixed arithmetic

**CPython functions:**
- `Objects/complexobject.c:55 _Py_c_diff`
- `Objects/complexobject.c:72 _Py_rc_diff` (float - complex)

```c
// Objects/complexobject.c:72
Py_complex _Py_rc_diff(double a, Py_complex b) {
    Py_complex r;
    r.real = a - b.real;
    r.imag = -b.imag;   // negates: -0.0 - 0.0j → imag = -0.0
    return r;
}
```

For `float - complex`, CPython uses separate real/imag arithmetic that
preserves IEEE negative zero on the imaginary component.

**Status in gopy:** `complexSub` promotes the float to `complex128` and uses
Go's `-` operator, which does not produce `-0.0` for `-(+0.0)` in all cases.

**Failing test:** `assertComplexesAreIdentical(-0.0 - complex(0.0, 0.0), complex(-0.0, -0.0))`.

**Fix:** for mixed `float - complex` operations, implement the component-wise
arithmetic explicitly:
```go
r.real = float64(aReal) - bReal
r.imag = math.Copysign(0, -bImag) if bImag == 0 else -bImag
```
More generally: port `_Py_rc_diff`, `_Py_cr_diff`, `_Py_rc_sum` etc. as
separate helpers instead of relying on `complex128` arithmetic.

### P3.7 — `complex_abs` does not raise OverflowError on infinity

**CPython function:** `Objects/complexobject.c:368 _Py_c_abs`

```c
static double
_Py_c_abs(Py_complex z) {
    double result = hypot(z.real, z.imag);
    if (!Py_IS_FINITE(result)) { errno = ERANGE; }
    return result;
}
```

`complex_abs` at line 783 checks `errno == ERANGE` after calling `_Py_c_abs`
and raises `OverflowError` if set.

**Status in gopy:** `objects/complex.go complexAbs` uses `cmplx.Abs(v)`
without overflow check.

**Fix:** after `cmplx.Abs`, check `math.IsInf(result, 0)` and return
`OverflowError("math range error")`.

### P3.8 — Complex string parser incomplete

**CPython function:** `Objects/complexobject.c:935 complex_from_string_inner`

CPython's parser handles:
1. Leading/trailing whitespace stripped.
2. Optional outer parentheses `( ... )` with internal whitespace.
3. Real part optional (defaults to `0`).
4. `J` uppercase accepted alongside `j`.
5. Overflow in the mantissa maps to `inf` (via `PyOS_string_to_double`).

**Status in gopy:** `builtins/ctor.go:741 parseComplexString` only handles
simple `(a+bj)` without leading spaces before `(`, without spaces after `(`,
and does not handle `1e500` → `inf`.

**Failing tests:**
- `complex(" ( +4.25-J )")` → should be `(4.25-1j)`
- `complex("1e500")` → should be `complex(inf, 0.0)`

**Fix:** rewrite `parseComplexString` to faithfully port
`complex_from_string_inner`. Key steps:
1. Strip leading/trailing whitespace (`unicode.IsSpace`).
2. If first non-space is `(`, strip it and expect matching `)`.
3. Strip whitespace after `(`.
4. Parse optional real part (float string).
5. Parse optional imaginary part with sign.
6. Normalize `J` → `j`.
7. Use `strconv.ParseFloat` with special handling: `Inf` for overflow.

### P3.9 — Complex subtype not preserved in constructor

**CPython function:** `Objects/complexobject.c:424 complex_subtype_from_doubles_impl`

```c
static PyObject *
complex_subtype_from_doubles_impl(PyTypeObject *type, double real, double imag) {
    PyComplexObject *op = (PyComplexObject*)type->tp_alloc(type, 0);
    op->cval.real = real;
    op->cval.imag = imag;
    return (PyObject *)op;
}
```

`complex_new_impl` passes `type` down to this function so that subclasses
get an instance of the correct type.

**Status in gopy:** `ComplexType.TpNew` is absent; `builtins/ctor.go` always
calls `objects.NewComplex(re, im)` which hardcodes `*Complex`.

**Fix:** add `ComplexType.TpNew = complexTpNew` that allocates an `*Instance`
(or `*Complex`) of the correct subtype, analogous to how `int`, `set`, and
`frozenset` have subtype-aware `TpNew`.

### P3.10 — `complex.__format__` incomplete

**CPython function:** `Objects/complexobject.c:887 complex___format___impl`

CPython delegates to `_PyComplex_FormatAdvancedWriter` which handles the
standard format mini-language for complex (e.g., `format(3.2+0j, '-')` is
valid and strips the `+` before the imaginary part's sign).

**Status in gopy:** complex format either rejects all non-empty format specs or
is absent.

**Fix:** port `_PyComplex_FormatAdvancedWriter` from
`Objects/complexobject.c:847 _PyComplex_FormatAdvancedWriter`. Key behaviour:
- Format spec applies to each component separately.
- Sign character `-`, `+`, ` ` applies to both parts.
- The imaginary part always has a sign and always ends with `j`.

---

## P4 — `slice` type gaps (`Objects/sliceobject.c`)

### P4.1 — `slice_hash` not implemented

**CPython function:** `Objects/sliceobject.c:646 slice_hash`

```c
static Py_hash_t
slice_hash(PyObject *op) {
    PySliceObject *v = _PySlice_CAST(op);
    Py_uhash_t acc = _PyHASH_XXPRIME_5;
    for each of (v->start, v->stop, v->step) {
        Py_uhash_t item_hash = PyObject_Hash(item);
        acc += item_hash * _PyHASH_XXPRIME_2;
        acc = _PyHASH_XXROTATE(acc);
        acc *= _PyHASH_XXPRIME_1;
    }
    acc += 3 * _PyHASH_XXPRIME_5 ^ (3 * 0x345678UL);
    return acc == (Py_uhash_t)-1 ? -2 : acc;
}
```

**Status in gopy:** `SliceType.Hash` is nil; `hash(slice(5))` → `TypeError: unhashable type: 'slice'`.

**Fix:** port `slice_hash` using the same xxHash3 accumulation. The constants
`_PyHASH_XXPRIME_1/2/5` and `_PyHASH_XXROTATE` are defined in
`Include/cpython/pyhash.h:36`. Bind as `SliceType.Hash = sliceHash`.

### P4.2 — `slice.__reduce__` missing (pickle broken)

**CPython function:** `Objects/sliceobject.c:561 slice_reduce`

```c
static PyObject *
slice_reduce(PyObject *op, PyObject *Py_UNUSED(ignored)) {
    PySliceObject *self = _PySlice_CAST(op);
    return Py_BuildValue("O(OOO)", Py_TYPE(self), self->start, self->stop, self->step);
}
```

Returns `(slice, (start, stop, step))` for pickle/copy.

**Status in gopy:** `objects/slice.go` has `__getnewargs__` but no `__reduce__`.
`pickle.dumps(slice(1,10,2))` then `pickle.loads(...)` returns a
`<slice object at 0x…>` that doesn't reconstruct correctly.

**Fix:** add `sliceReduceMethod` that returns `(type(self), (start, stop, step))`.
Register as `SetTypeDescr(SliceType, "__reduce__", ...)`.

### P4.3 — Slice not enrolled in GC cyclic collector

**CPython:** `Objects/sliceobject.c:619 slice_traverse`,
`Objects/sliceobject.c:683 Py_TPFLAGS_DEFAULT | Py_TPFLAGS_HAVE_GC`

CPython marks slices with `Py_TPFLAGS_HAVE_GC` and provides
`slice_traverse` to visit `start`, `stop`, `step`. Without this, a cycle
involving a slice is never collected.

**Status in gopy:** `SliceType` does not set `TpFlagHaveGC` or equivalent; the
GC does not traverse slice fields.

**Fix:**
1. Set `SliceType.TpFlags |= TpFlagHaveGC` (or the gopy equivalent).
2. Implement `sliceTraverse` visiting `s.Start`, `s.Stop`, `s.Step`.
3. Call `GCTrack(s)` in `NewSlice`.
These mirror `slice_traverse` at `Objects/sliceobject.c:619` and the
`PyObject_GC_New` + `PyObject_GC_Track` calls in `Objects/sliceobject.c:184`.

### P4.4 — `slice.indices()` accepts only `*Int`; should call `__index__`

**CPython function:** `Objects/sliceobject.c:201 slice_indices_impl`

```c
// length is coerced via PyLong_AsSsize_t which calls __index__
static PyObject *
slice_indices_impl(PySliceObject *self, PyObject *len) {
    Py_ssize_t ilen, start, stop, step, slicelength;
    ilen = PyLong_AsSsize_t(len);
    ...
}
```

The `length` argument is passed through `PyLong_AsSsize_t` which calls
`__index__` on non-int types.

**Status in gopy:** `objects/slice_indices.go sliceIndicesMethod` does a direct
`*Int` type switch; objects with `__index__` are rejected.

**Fix:** after the `*Int` case, add an `__index__` coercion path using
`objects.Index(lenObj)`.

---

## P5 — `test_iter`: `StopIteration` escapes `str.join`

**CPython function:** `Objects/unicodeobject.c:10253 PyUnicode_Join`

CPython materializes the iterable with `PySequence_Fast` before joining:
```c
// Objects/unicodeobject.c:10253
fseq = PySequence_Fast(iterable, "");
```
`PySequence_Fast` calls `list(iterable)` which CONSUMES the full iterator
and stores the results in a list. Any `StopIteration` raised inside the
iterator's `__next__` propagates out of `list()`, not silently swallowed.

But in the test case (`OhPhooey.__next__` calls `next(self.it)` on an
exhausted file), after a first iteration produces all strings, the second
call to `next(self.it)` raises `StopIteration`. In CPython >= 3.7, PEP 479
converts this `StopIteration` into a `RuntimeError` inside a generator, but
outside a generator it propagates normally.

The actual bug: `test_unicode_join_endcase` at `test_iter.py:717` constructs
`"".join(OhPhooey())`. The `str.join` implementation in gopy (`objects/str_bind.go`)
iterates the sequence lazily instead of calling `PySequence_Fast` first. When
the second element raises `StopIteration`, gopy's for-loop exit path swallows
it as end-of-iteration, silently producing a truncated string instead of the
complete result.

Alternatively the test crash may be a different `StopIteration` escaping the
unittest runner; deeper investigation needed during P5 work.

**CPython citation:** `Objects/unicodeobject.c:10253 PyUnicode_Join`,
`Objects/abstract.c PySequence_Fast`.

**Fix:** change `strJoinMethod` in `objects/str_bind.go` to materialise the
sequence fully first (via `objects.SequenceFast` or equivalent), then join the
resulting list. Matches CPython's `PyUnicode_Join` exactly.

---

## P6 — `test_py_compile`: `os.write` rejects `memoryview`

**CPython function:** `Modules/posixmodule.c posix_write`

```c
// accepts any buffer-protocol object via Py_buffer
static PyObject *
posix_write(PyObject *self, PyObject *args) {
    int fd;
    Py_buffer pbuf;
    if (!PyArg_ParseTuple(args, "iy*:write", &fd, &pbuf)) return NULL;
    ...
}
```

The `y*` format accepts `bytes`, `bytearray`, and `memoryview`.

**Status in gopy:** `stdlib/subprocess.py:2164` calls `os.write(key.fd, chunk)`
where `chunk` is a `memoryview` slice. gopy's `os.write` requires `bytes`.

**Fix:** in the gopy `os.write` implementation, accept `*objects.MemoryView`
in addition to `*objects.Bytes`/`*objects.ByteArray`. Extract the underlying
bytes via `memoryview.ToBytes()`.

---

## Checklist

### P1 — Test infrastructure

- [x] P1.1 vendor `fractions.py` into `stdlib/`
- [x] P1.2 vendor `Lib/test/string_tests.py` for test harness
- [x] P1.3 vendor `Lib/test/mapping_tests.py` for test harness
- [x] P1.4 vendor `symtable.py` into `stdlib/`

### P2 — `int` type

- [x] P2.1 `int()` Unicode whitespace + decimal digit normalisation
- [x] P2.2 `int()` base `__index__` coercion
- [x] P2.3 `sys.int_info` structseq
- [x] P2.4 runtime int digit-limit enforcement

### P3 — `complex` type

- [x] P3.1 `complex.__complex__` method descriptor
- [x] P3.2 `complex()` constructor `__complex__` protocol
- [x] P3.3 `complex.from_number` classmethod
- [x] P3.4 `complex.__getnewargs__`
- [x] P3.5 expose numeric dunder descriptors on `ComplexType`
- [x] P3.6 negative-zero preservation in mixed float/complex arithmetic
- [x] P3.7 `complex_abs` OverflowError on infinite result
- [x] P3.8 complex string parser: spaces, parentheses, uppercase J, overflow→inf
- [x] P3.9 complex subtype preserved in constructor (`TpNew`)
- [x] P3.10 `complex.__format__` full format-spec support

### P4 — `slice` type

- [x] P4.1 `slice_hash` (xxHash3 over start/stop/step)
- [x] P4.2 `slice.__reduce__` for pickle
- [x] P4.3 slice GC traversal + `TpFlagHaveGC`
- [x] P4.4 `slice.indices()` length `__index__` coercion

### P5 — VM / eval loop

- [x] P5.1 `str.join` materialise sequence before iterating (CPython `PyUnicode_Join` model)

### P6 — OS module

- [x] P6.1 `os.write` accept `memoryview`

### Spec 1700 rows to advance

| Test | Current state (2026-05-29) | Unblocked by |
|------|---------------------------|--------------|
| test_float | crash (fractions) | P1.1 |
| test_builtin | crash (fractions) | P1.1 |
| test_compare | done (1722 audit) | — |
| test_numeric_tower | crash (fractions) | P1.1 |
| test_bytes | crash (string_tests) | P1.2 |
| test_str | 18 fail / 1 error | P1.2 + str fixes |
| test_userstring | crash (string_tests) | P1.2 |
| test_dict | crash (mapping_tests) | P1.3 |
| test_frame | 17 fail / 9 errors | spec 1723 P4 |
| test_symtable | crash (symtable) | P1.4 |
| test_long | crash (int_info) | P2.3 |
| test_int | 17 fail / 8 error | P2.1 P2.2 P2.4 |
| test_complex | done (P3.1–P3.10 landed) | — |
| test_slice | done (P4.3 landed via type_call orphan-tuple fix) | — |
| test_iter | 1 fail | P5.1 |
| test_py_compile | 1 error | P6.1 |
