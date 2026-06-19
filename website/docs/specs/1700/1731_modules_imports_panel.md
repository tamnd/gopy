---
id: "1731"
slug: 1731
title: "1731: Modules / imports test panel — CPython 3.14 parity port"
sidebar_label: "1731 Modules imports panel"
description: "Audit and port of the Modules/imports test panel from spec 1700 (12 files plus test_import/, test_importlib/, test_module/) against CPython 3.14 under the spec 1726 zero-skip bridge."
---

## Status

Active. Branch `feat/v0.13.7-spec-1731-imports-reaudit` (re-audit of the
panel under the current build; original work on `feat/v0.13.5-spec-modules-imports`).

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
| `test_import/` (dir) | OK | **OK (118 tests, 4 skipped)** — 3 platform skips + `test_frozen_compat` (needs a frozen `_frozen_importlib`, P7) |
| `test_importlib/` (dir) | OK | 1346 tests; 0 failures, 0 errors, 63 skipped — threaded circular import and incomplete multi-phase init both closed; module-lock GC lifetime closed via tp_clear-from-delete_garbage. Run from a clean cwd: invoking `test_importlib.test_util` directly from the repo root puts `module/` (the Go module-port dir) on `sys.path[0]`, where `import module` then resolves as a PEP 420 namespace package and `test_find_submodule_in_module` no longer sees a `ModuleNotFoundError`. CPython fails identically from such a cwd; the canonical regrtest run uses a clean directory. |
| `test_modulefinder` | OK | **OK (17 tests)** |
| `test_pkg` | OK | **OK (8 tests)** |
| `test_pkgutil` | OK | **OK (21 tests)** |
| `test_pyclbr` | OK | **OK (6 tests)** |
| `test_pkgimport` | (covered by `test_import/`) | no flat file |
| `test_runpy` | OK | **OK (40 tests)** |
| `test_frozen` | OK | **OK (3/3)** — frozen test modules + override + `sys._stdlib_dir` shipped |
| `test_zipimport` | OK | **OK (91 tests, 4 skipped)** |
| `test_zipimport_support` | OK | **OK (4 tests)** — vendored `test.test_doctest`; pdb single-step under doctest now works after the opcode-tracing fix |
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
- [x] P5: `test_import/` runs all 118 tests without the threaded crash — `os.fstat`/`os.isatty` no longer borrow the fd in a finalizer-bearing `os.File`
- [x] P5: `test_import/` green — ported the single-phase extension cache (`_testsinglephase*` variants, `m_size` kinds, the extensions cache + `m_copy` reload), the gh-123950 circular import (`_testsinglephase_circular` via the `_gcd_import` import hook), and per-subinterpreter `sys.modules` isolation so the PEP 489 compat gate fires on re-import. 4 skips remain: 3 platform-specific, plus `test_frozen_compat`, which needs a frozen `_frozen_importlib` (P7)
- [x] P5: `test_module_with_large_stack` no longer flakes with `bad file descriptor` — `os.NewFile`/`os.OpenFile` arm the close finalizer on the unexported inner `*os.file`, so `SetFinalizer(f, nil)` on the outer handle was a no-op. A leaked borrowed-fd wrapper (subprocess pipes) would close a reused descriptor mid-write. `objects.ClearOSFileFinalizer` reaches the inner pointer; the `io` and `_posixsubprocess` borrows route through it
- [x] P5: re-audit `test_module/` — green (39 tests)
- [x] `test_doctest` green (71 tests) and `test_zipimport_support` green (4 tests): pdb single-stepping
  under doctest needed faithful opcode (INSTRUCTION) tracing. The bug was in the monitoring shadow walk:
  `add_tools`/`remove_tools` bailed when a slot's live byte was already `INSTRUMENTED_LINE` or
  `INSTRUMENTED_INSTRUCTION`, so installing the global `PY_RETURN` event the legacy `sys.settrace` bridge
  needs never reached the real opcode parked in the line / per-instruction side table. When `pdb`'s
  `step` toggled `f_trace_opcodes` off, the slot restored to the plain opcode and the return event was
  lost, so the debugger jumped back into `doctest.__run` instead of stopping at the function's
  `--Return--`. Ported `instrument()` and `de_instrument()` (Python/instrumentation.c) to walk the live
  byte through both side tables to the location CPython tracks as `opcode_ptr` and rewrite the
  (de)instrumented opcode there. The `module/` dir, the directory, and the ZIP archive each run as their
  own `__main__` via the new `pymain_get_importer` path in `cmd/gopy`.
- [x] P5: `test_importlib/` residuals — 1346 tests run, 0 failures + 0 errors. `test_all_locks`
  passes: the collector grew a `tp_clear` slot that runs from `delete_garbage` (the cyclic-GC path,
  after the collector has proven unreachability) instead of the eager refcount-zero dealloc path, so an
  instance `__dict__` that pins a `_ModuleLock` is cleared at GC time and `_bootstrap._module_locks`
  drains to zero. The eager path deliberately no longer clears, so an object the VM under-counts to zero
  while still live is never cleared out from under its users. `test_incomplete_multi_phase_init_module`
  passes once the `_testmultiphase` incomplete-init path is wired. `test_circular_imports` passes by
  splitting the import entry points: the VM `IMPORT_NAME` opcode keeps the refcount-proven
  `_frozen_importlib.__import__` delegate route (it applies `DECREF_INPUTS` to the module it pushes),
  with the `import_ensure_initialized` still-initializing fast path prepended so a concurrent circular
  import waits via `_bootstrap._lock_unlock_module` (which catches `_DeadlockError`) instead of dying on
  an uncaught `_DeadlockError` inside `_find_and_load`'s `_ModuleLockManager`. The builtin `__import__`
  keeps the full C-faithful `PyImport_ImportModuleLevelObject` body (`_gcd_import` + the dotted-head
  `KeyError` for gh-134100), which a shared function could not satisfy without breaking one side or the
  other.
- [x] P5: `test_zipimport.testZip64LargeFile` runs under the bare-file harness (`use_resources is None`
  enables `largefile`). The reconstruction stitches the `>4 GiB` sparse zip back from
  `zipimport_data/sparse-zip64-c0-*.part`; CPython ships three parts (offsets `0`, `0x1_0000_0000`,
  `0x2_0000_0000`) and `test/cpython/zipimport_data/` carries all three, so the central directory near
  the 8 GiB mark is present and the file parses. `testAFakeZlib` self-skips with `'zlib is a builtin
  module'`: gopy statically links zlib, which is exactly the static-zlib build configuration CPython's
  own comment says to skip on, so the 4-vs-2 skip delta against a dynamic-zlib CPython is faithful, not
  a divergence.
- [x] P5: cyclic collector no longer reclaims `_frozen_importlib._blocking_on`
  (`'_WeakValueDictionary' object has no attribute 'data'`) under `testZip64`'s heap churn. `sys.modules`
  is held through a Go pointer the refcount pass cannot see, so `pin_roots` floated only the direct
  module entries and trusted `move_unreachable` to resurrect the rest. gopy containers do not incref what
  they store (instance `__dict__` among them), so `subtract_refs` over-decrements an interior node on the
  `module -> module __dict__ -> _WeakValueDictionary -> instance __dict__` chain and a partition order that
  fails to resurrect every hop drops a still-live object. `markReachableClosure` now walks the whole
  strongly-reachable closure from the static roots and floats each candidate to `refs >= 1`, recursing
  only through candidates so a young-generation collection stays as cheap as before.
- Note: `test_namespace_pkgs.SeparatedNamespacePackagesCreatedWhileRunning.test_invalidate_caches` and
  `LoaderTests.test_path_indexable` fail when the file is run standalone (`gopy test_namespace_pkgs.py`),
  but CPython 3.14 fails identically standalone — `PathFinder.invalidate_caches` does
  `from importlib.metadata import MetadataPathFinder`, and the test's `sys.path` replacement strips the
  stdlib, so the first-time `import json` under that restricted path raises `ModuleNotFoundError`. Under
  the canonical package run (`python -m test test_importlib`) an earlier submodule imports
  `importlib.metadata` while the path is unrestricted, so it stays cached and both tests pass. This is a
  run-mode artifact, not a gopy defect; gopy matches CPython behavior in both modes.
- Note: `type_new` copies the class body into a transient dict, drains it onto the type, then drops the
  copy. That copy captures every class method, whose `__globals__` pins the defining module dict, so the
  drop has to release synchronously the way `dict_dealloc` does. `dropTransientDict` decrefs the copy and,
  if that takes it to zero, clears its contents and untracks it on the spot. A plain `Decref` left it at
  refcount zero but still tracked (gopy keeps refcount-zero non-finalizable containers tracked for the
  weakref pass), so the next collection counted it as an extra cycle member and the `module/gc` unit tests
  (`TestUserDelFiresDuringCycleCollect`) reported one too many reclaims.
- Note: `test_importlib.frozen.test_finder` compares the frozen loader's `filename` against
  `os.path.join(STDLIB_DIR, '__hello__.py')`, where `STDLIB_DIR` is derived from where `test/support`
  lives. gopy reports the live on-disk frozen path, matching CPython (which freezes `__hello__` with its
  `Lib/__hello__.py` filename). The two agree when the suite runs in its natural location under the
  stdlib (12/12 green); they only diverge if the corpus is relocated, which moves `STDLIB_DIR` away from
  the real stdlib. Run-mode artifact, not a gopy defect.
- [x] P5: the regrtest runner drives the three directory suites the way CPython's regrtest does, with
  `gopy -m unittest test.<name>`, instead of looking for a non-existent `<pkg>/<pkg>.py` entry point.
  The command runs from the corpus directory so the repo-root `module/` Go-port tree does not shadow
  stdlib imports on `sys.path[0]` (otherwise `find_spec('module.name')` resolves `module/` as a PEP 420
  namespace package and the two `test_find_submodule_in_module` rows stop raising `ModuleNotFoundError`).
  `TestModulesImportsPanelPackages` pins all three: `test_import` 118/118, `test_module` 39/39,
  `test_importlib` 1346/1346.
- [ ] P7: live importlib finders on `sys.meta_path` + `_imp` C functions (architectural)
- [ ] P6: `test__interpreters` / `test__interpchannels` parity with CPython skip/run

## Re-audit (2026-06-19)

Re-ran the whole panel against the current build, one entry per process the way the
gate drives it, to confirm ground truth. Every row is green with zero errors and zero
failures: `test_module` 39, `test_import` 118 (skip 4), `test_importlib` 1346 (skip
10), `test_frozen` 3, `test_modulefinder` 17, `test_pkg` 8, `test_pkgutil` 21,
`test_pyclbr` 6, `test_runpy` 40, `test_zipimport` 91 (skip 4), `test_zipimport_support`
4, `test_zipapp` 35. No code change was needed and the MANIFEST counts already match,
so this re-audit is documentation only.

One thing worth recording about `test_zipimport`: when several copies of the suite run
at once in the same working directory it can surface an intermittent
`EOFError('EOF read where not expected')` out of `zipimport.py`'s `_get_data`. That is a
harness collision, not a runtime bug. Every test in the file builds its archive under the
same fixed name (`TEMP_ZIP`, e.g. `junk95142.zip`) in the cwd, so two concurrent processes
truncate each other's file mid-read. CPython fails the same way under concurrent
same-directory execution. The gate runs each entry in its own process sequentially, where
the file passes 91/91 (12/12 test cases) every time.
