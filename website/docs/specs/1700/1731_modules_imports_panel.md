---
id: "1731"
slug: 1731
title: "1731: Modules / imports test panel — CPython 3.14 parity port"
sidebar_label: "1731 Modules imports panel"
description: "Audit and port of the Modules/imports test panel from spec 1700 (12 files plus test_import/, test_importlib/, test_module/) against CPython 3.14 under the spec 1726 zero-skip bridge."
---

## Status

Active. Branch `feat/v0.13.5-spec-modules-imports`.

Run under the [[1726]] bridge so every `@cpython_only` test executes on gopy
instead of being skipped. "No skip" means parity with CPython: if CPython
skips a test on this platform, gopy skips it too; everything else must pass.

## Goal

Drive every test in the spec 1700 Modules / imports panel to CPython 3.14
parity via faithful CPython ports. No shims, no partial slices: when a gate
lands on a subsystem, port every function in that subsystem from CPython as the
single source of truth.

Sources of truth: `$HOME/cpython-314/`. Every cited function is read from that
tree before porting.

## Panel

The panel is the 12 flat files plus the three directory suites. CPython 3.14.5
runs all of the non-interpreter files green.

| Test | CPython 3.14.5 | gopy (audit 2026-06-16) |
| --- | --- | --- |
| `test_module/` (dir) | OK | **OK (39 tests)** |
| `test_import/` (dir) | OK | 118 tests; 5 errors (`_testsinglephase`/`_testmultiphase` C-ext subinterp, P7), 16 skipped |
| `test_importlib/` (dir) | OK | 1346 tests; 2 failures (module-lock GC lifetime, threaded circular import) + 1 error (incomplete multi-phase C-ext), 63 skipped |
| `test_modulefinder` | OK | **OK (17 tests)** |
| `test_pkg` | OK | **OK (8 tests)** |
| `test_pkgutil` | OK | **OK (21 tests)** |
| `test_pyclbr` | OK | **OK (6 tests)** |
| `test_pkgimport` | (covered by `test_import/`) | no flat file |
| `test_runpy` | OK | **OK (40 tests)** |
| `test_frozen` | OK | **OK (3/3)** — frozen test modules + override + `sys._stdlib_dir` shipped |
| `test_zipimport` | OK | **OK (91 tests, 4 skipped)** |
| `test_zipimport_support` | OK | needs vendored `test.test_doctest` (doctest) |
| `test_zipapp` | OK | **OK (35 tests)** |
| `test__interpchannels` | PEP 554 | deferred (see below) |
| `test__interpreters` | PEP 554 | deferred (`_interpreters.run_string` missing) |

## Plan

Phased, smallest-blast-radius first. Re-audit after each phase against
CPython 3.14.5 (counts and `-v` lists).

- **P1 — `os.altsep` and the module namespace surface.** `os.altsep` is `None`
  on POSIX; its absence blocks `test_pkgutil`, `test_zipimport`,
  `test_zipimport_support`. The `test_pkg` `dir()` gap is a module-object
  attribute surface issue (`__cached__`, `__doc__`, `__loader__`, `__spec__`).
  Port these first.
- **P2 — pure-Python stdlib modules.** `modulefinder`, `pyclbr`, `zipapp` are
  pure-Python `Lib/*.py`; vendor them and whatever import-machinery they lean
  on. Confirm they run under gopy's import system.
- **P3 — frozen modules.** `test_frozen` needs `__hello__` and the frozen
  module table. Port the frozen-module surface from CPython.
- **P4 — `test_runpy` residual.** Single ERROR in
  `test_run_package_init_exceptions`; port the package-init exception path.
- **P5 — directory suites.** Re-audit `test_import/`, `test_importlib/`,
  `test_module/` against CPython and close residuals.
- **P7 — live importlib finders (architectural).** gopy dispatches imports
  Go-side: `sys.meta_path` is empty where CPython has
  `[BuiltinImporter, FrozenImporter, PathFinder]`, and `importlib.machinery`
  is a stub that does not re-export `PathFinder` / `FrozenImporter` /
  `BuiltinImporter`. The Python finder classes in `_bootstrap.py` exist but
  are not wired into `sys.meta_path`, and `_imp` is missing the functions the
  full bootstrap drives (`extension_suffixes`, `find_frozen`,
  `get_frozen_object`, `is_frozen_package`, `create_builtin`, `exec_builtin`,
  `create_dynamic`, `exec_dynamic`, `_fix_co_filename`). This is the root of
  the `test_import/`, `test_importlib/`, `test_modulefinder`, and `test_runpy`
  residuals. Closing it means making the Python finders the real dispatch path
  (populate `sys.meta_path`, port the `_imp` C functions, vendor the full
  `_bootstrap_external.py` with `PathFinder`) instead of the Go-side shim.
  This is a subsystem port on the scale of its own spec.

## Notable fixes

- `func_getattro` now increfs `__dict__` attribute reads
  (`Objects/funcobject.c` Py_XINCREF). A list stored on a function (mock keeps
  its `patchings` list this way) was emptied by `list_dealloc` after the first
  read, so a shared decorator silently stopped patching across test classes.
  This fixed `test_zipimport.test_checked_hash_based_change_pyc` in the
  cross-class run.
- `_testcapi.config_get` / `config_getint` / `config_names` ported over a
  `PyConfig_Get` spec table (`Python/initconfig.c`), fixing the two
  `testTraceback` errors.
- **P6 — interpreters.** `test__interpreters` / `test__interpchannels` are
  PEP 554 subinterpreters. Match CPython's behaviour: if CPython skips on this
  build, gopy skips; otherwise port the `_interpreters` surface the tests reach.

## Checklist

- [x] P1: `os.altsep`
- [x] P1: module-object `dir()` surface (`__cached__`, `__doc__`, `__loader__`, `__spec__`) for `test_pkg` — `test_pkg` green (8 tests)
- [x] P2: vendor `modulefinder` — `test_modulefinder` green (17 tests)
- [x] P2: vendor `pyclbr` — `test_pyclbr` green (6 tests)
- [x] P2: vendor `zipapp` — `test_zipapp` green (35 tests)
- [x] P2: `test_pkgutil` green (21 tests)
- [x] `test_zipimport` green (91 tests): `func_getattro` incref + `config_get` port
- [x] `test_module/` green (39 tests)
- [x] P3: frozen `__hello__`/`__phello__` + aliases, frozen override, `sys._stdlib_dir` — `test_frozen` green (3/3)
- [x] P4: `test_runpy` green (40 tests) — package-init exception path closed
- [x] P5: `test_import/` runs all 118 tests without the threaded crash — `os.fstat`/`os.isatty` no longer borrow the fd in a finalizer-bearing `os.File`; remaining 5 errors are the `_testmultiphase`/`_testsinglephase` C-extension subinterpreter tests (P7)
- [x] P5: `test_module_with_large_stack` no longer flakes with `bad file descriptor` — `os.NewFile`/`os.OpenFile` arm the close finalizer on the unexported inner `*os.file`, so `SetFinalizer(f, nil)` on the outer handle was a no-op. A leaked borrowed-fd wrapper (subprocess pipes) would close a reused descriptor mid-write. `objects.ClearOSFileFinalizer` reaches the inner pointer; the `io` and `_posixsubprocess` borrows route through it
- [x] P5: re-audit `test_module/` — green (39 tests)
- [ ] P5: `test_importlib/` residuals — 1346 tests run, down to 2 failures + 1 error. The error
  (`test_incomplete_multi_phase_init_module`) is the `_testmultiphase` C-ext path (P7). The two
  failures are GC/threading edge cases: `test_all_locks` expects `_bootstrap._module_locks` to drain
  to zero after `gc_collect()` (gopy's collector leaves the no-longer-referenced `_ModuleLock`
  weakref entries live across the full import sweep, though the isolated `test_lock_lifetime` passes),
  and `test_circular_imports` is a threaded-import determinism case. Both overlap the broader
  weakref/GC work (see the weakref/gc panel tasks).
- [ ] P7: live importlib finders on `sys.meta_path` + `_imp` C functions (architectural)
- [ ] P6: `test__interpreters` / `test__interpchannels` parity with CPython skip/run
