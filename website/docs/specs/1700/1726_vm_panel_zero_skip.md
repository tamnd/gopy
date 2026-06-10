---
id: "1726"
slug: 1726
title: "1726: VM panel zero-skip conformance (run exactly what CPython runs)"
sidebar_label: "1726 VM panel zero-skip"
description: "Bring the spec 1700 VM/eval-loop panel to zero gopy-specific skips: gopy must reproduce CPython 3.14's own run/skip decisions on the same vendored test files, port the test C-extension dependencies, and fix every behavioural gap the skips were hiding."
---

## Status

Active. Branch `feat/v0.13.2-vm-zero-skip-conformance`. Stacked on spec 1725
(funcstr arg-count sweep, PR #90), which the message fixes here depend on.

## Goal

A skip is only correct when CPython skips the same test. Running the vendored
panel files under CPython 3.14 itself is the reference: the only skips CPython
emits across `test_call` / `test_frame` / `test_iter` / `test_generators` /
`test_coroutines` are the 8 unconditional FrameLocalsProxy design skips in
`test_frame` ("Unlike a mapping: no proxy.update", etc.). gopy must match that
set exactly. Every other gopy skip is a gap to close, not a license to skip.

Sources of truth: `$HOME/cpython-314/`.

## Skip taxonomy (measured 2026-06-10)

Per panel file, `gopy skip count` vs `CPython skip count` on the same file:

- `test_call`: gopy 182, CPython 0. 131 `requires _testcapi`, 51
  `@cpython_only`.
- `test_frame`: gopy 12, CPython 8. The 8 match (proxy design). Extra: 3
  `@cpython_only`, 1 `ctypes`, plus 1 `_testinternalcapi`.
- `test_iter`: gopy 2, CPython 0. Both `@cpython_only`.
- `test_generators`: gopy 1, CPython 0. `_testcapi.raise_SIGINT_then_send_None`.
- `test_coroutines`: gopy 3, CPython 0. All `requires _testcapi`.

## Approach

1. Conformance bridge: gopy commits to CPython implementation-detail parity, so
   `test.support.check_impl_detail` treats `sys.implementation.name == 'gopy'`
   as cpython for impl-detail gating. `@cpython_only` tests now run on gopy
   exactly as on CPython. This is the harness recognising gopy's contract, not
   a skip waiver: anything gopy still cannot match is a real gap below.

2. Fix the arg-count message drift the `@cpython_only` tests expose. Same shape
   as spec 1725: route fixed-arity descriptors through METH flags so the
   message comes from `_PyObject_FunctionStr`. ~29 failures in `test_call`
   (hasattr/getattr/staticmethod/classmethod/struct/deque/min/print kwargs,
   `get` min/max arg messages, keyword-suggestion text).

3. Port the test C-extension modules the panel imports, 1-to-1 from CPython,
   into `module/_testcapi/` and extend `module/_testlimitedcapi/`:
   `Modules/_testcapi/vectorcall.c` (pyobject_vectorcall, pyobject_fastcalldict,
   MethInstance/MethClass/MethStatic, MethodDescriptorBase/Derived/NopGet/
   MethodDescriptor2), the SIGINT helper, and the `_testinternalcapi` sizeof
   hook. No stubs: each function mirrors its C source.

4. Port `ctypes` enough for `test_frame.TestFrameCApi.test_basic` (or confirm it
   is a genuine CPython-skip on this platform and matches).

5. Refcount discipline (P11 from spec 1723): make the failed-unpack path decref
   abandoned stack temporaries and the iterator so deterministic destruction
   matches CPython. Gates `test_iter.test_ref_counting_behavior` and the
   `_testcapi` getrefcount tests.

## Checklist

- [x] Conformance bridge: `check_impl_detail` treats gopy as cpython for impl-detail gating
- [x] `dict.__contains__` rebind to METH_O (kwargs rejected before arity)
- [x] `PySeqIter` index overflow guard -> `test_iter.test_iter_overflow` green
- [x] `test_call` `@cpython_only` message drift: hasattr/getattr/staticmethod/classmethod/struct/deque/min/print + get min/max + keyword suggestions (down to the 2 failures + 1 error that need the `_testcapi`/`_testinternalcapi` ports below)
- [ ] Port `module/_testcapi/` vectorcall.c (pyobject_vectorcall/fastcalldict + Meth* + MethodDescriptor* heap types)
- [ ] Port `_testcapi.raise_SIGINT_then_send_None` for `test_generators`
- [ ] Port `_testinternalcapi` sizeof hook for `test_frame.test_sizeof`
- [ ] `ctypes` for `test_frame.TestFrameCApi.test_basic` (or confirm CPython-skip parity)
- [ ] P11: failed-unpack decref so `test_iter.test_ref_counting_behavior` is green
- [ ] Panel re-run: gopy skip set == CPython skip set (only the 8 proxy skips remain)
- [ ] spec 1700 panel rows updated; spec 1723 closed against this
