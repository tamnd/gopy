---
id: "1725"
slug: 1725
title: "1725: Funcstr arg-count sweep — centralize every method arity message through _PyObject_FunctionStr"
sidebar_label: "1725 Funcstr arg-count sweep"
description: "Route every method-descriptor arg-count TypeError through _PyObject_FunctionStr so each message renders the owner type qualname exactly like CPython 3.14, across objects/, builtins/, errors/ and the C-extension module ports."
---

## Status

Active. Branch `feat/v0.13.1-spec-1725-funcstr-argcount`.

## Goal

Across gopy, the method descriptors that take a fixed arity (METH_NOARGS /
METH_O) were spelling out their own `TypeError` text by hand, e.g.
`"__reduce__() takes no arguments"`. CPython never writes those messages
inline: `method_vectorcall_NOARGS` / `method_vectorcall_O` build them through
`_PyObject_FunctionStr(func)`, which reads the bound method's `__qualname__`
and prefixes `__module__` (skipped for `None` / `"builtins"`). The result is
that CPython's message carries the owner type qualname,
`timedelta.__reduce__() takes no arguments (1 given)`, while gopy's hand-rolled
text dropped the type and sometimes the `(N given)` count entirely.

This spec sweeps every such site to `NewMethodDescrConv(type, name, flag, fn)`
so the arity check flows through `methodDescrCheckArity` ->
`FunctionStr`, making every message byte-identical to CPython 3.14 in one pass.

Sources of truth: `$HOME/cpython-314/`. Citations: `Objects/object.c:973`
`_PyObject_FunctionStr`; `Objects/descrobject.c:330` `method_vectorcall_NOARGS`,
`:360` `method_vectorcall_O`, `:593` `calculate_qualname`.

---

## Approach

1. `descrQualnameGetter` (objects/descr.go) was returning
   `Owner().Name + "." + name` (tp_name based). CPython's `calculate_qualname`
   formats `"%S.%S"` from the owner type's `__qualname__`, which for a static
   type is the last dotted component (`timedelta`, not `datetime.timedelta`).
   Rewrote it to use `TypeGetQualName(owner)`. This single change makes every
   static-type descriptor message strip the module prefix exactly as CPython
   does.

2. For each method descriptor whose CPython convention is METH_NOARGS or
   METH_O, swap `NewMethodDescr` for `NewMethodDescrConv` with the verified
   flag. The inner closures keep their own length guards (now dead, harmless)
   so behaviour is unchanged; the arity *message* now comes from the shared
   path.

3. Leave bare (correctly) the descriptors that are METH_VARARGS / custom
   tp_getattro / slot wrappers in CPython: datetime/time `__reduce_ex__`
   (digit-style VARARGS), lru_cache `__copy__` / `__deepcopy__` (accept args),
   array `extend` / `index` / `insert` / `pop` / `__reduce_ex__`, the
   contextlib/contextvars/warnings `__enter__` (pure-Python classes upstream),
   and every nb_/sq_/tp_ slot wrapper.

Verification per batch: probe arity with an extra arg against `python3` (3.14)
and `/tmp/gopy`, diff. Regression: `git stash` -> build base -> compare ->
`git stash pop`, plus `go test` on each touched package.

---

## Drift fixed

- `slice.__getnewargs__`: gopy exposed a `__getnewargs__` descriptor that
  CPython 3.14's slice does not carry (`hasattr(slice(1), '__getnewargs__')`
  was `True` instead of `False`). copy/pickle reconstruct a slice through
  `slice.__reduce__` -> `(slice, (start, stop, step))`, so the extra descriptor
  was pure divergence. Removed it (task #229).

---

## Backlog opened during the sweep

Structural mismatches found while probing, filed as follow-up tasks rather than
patched inline:

- #234 `datetime.timezone` should inherit `tzinfo.__reduce__` (gopy subclasses
  ObjectType directly, so the message says `timezone.__reduce__` where CPython
  says `tzinfo.__reduce__`).
- #235 `functools.partial` custom `tp_getattro` returns instance-capturing
  closures that bypass `methodDescrCheckArity` entirely.
- #230 str_ascii_iterator vs str_iterator type split.

---

## Checklist

- [x] descrQualnameGetter: render owner `__qualname__` (strip module prefix for static types)
- [x] objects base: object `__reduce__`/`__reduce_ex__`/`__getstate__`/`__sizeof__`/`__dir__`/`__format__` arity via funcstr
- [x] objects slice: `__reduce__` NoArgs, `indices` O
- [x] objects iterators/helpers: bytes/bytearray/str/int/dict/set/tuple/list/map/filter/range_iter/dict_iter/enum/structseq/typevar/generic_alias `__reduce__`/`__length_hint__`/`__getnewargs__`/`__setstate__`/`__reduce_ex__`
- [x] objects proxies: mappingproxy + FrameLocalsProxy keys/values/items/copy/__reversed__/update; dict-view __reversed__
- [x] module ports: _datetime / _struct / _functools / _itertools / _operator / _collections arity via funcstr
- [x] module array: registerArrayMethods table given a conv column (NoArgs / O / bare-VARARGS)
- [x] errors: BaseException `__reduce__` NoArgs, `__setstate__` O
- [x] builtins iterators: seqIter / callIter / reversedIter / zip `__reduce__`/`__setstate__`/`__length_hint__`
- [x] drift: remove divergent `slice.__getnewargs__` (#229)
- [x] spec doc + PR #90 opened, CI green (lint, vet, cfg-phase-parity, macOS/ubuntu/windows test)
