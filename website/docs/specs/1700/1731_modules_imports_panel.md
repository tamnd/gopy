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

| Test | CPython 3.14.5 | gopy baseline (audit 2026-06-14) |
| --- | --- | --- |
| `test_module/` (dir) | OK | to re-audit |
| `test_import/` (dir) | OK | to re-audit |
| `test_importlib/` (dir) | OK | to re-audit |
| `test_modulefinder` | OK | `ModuleNotFoundError: modulefinder` |
| `test_pkg` | OK | AssertionError: module `dir()` missing `__cached__`/`__doc__`/`__loader__`/`__spec__` |
| `test_pkgutil` | OK | `AttributeError: os has no attribute altsep` |
| `test_pyclbr` | OK | `ModuleNotFoundError: pyclbr` |
| `test_pkgimport` | (covered by `test_import/`) | no flat file |
| `test_runpy` | OK | 1 ERROR (`test_run_package_init_exceptions`) |
| `test_frozen` | OK | `ModuleNotFoundError: __hello__` (frozen module) |
| `test_zipimport` | OK | `AttributeError: os has no attribute altsep` |
| `test_zipimport_support` | OK | `AttributeError: os has no attribute altsep` |
| `test_zipapp` | OK | `ModuleNotFoundError: zipapp` |
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
- **P6 — interpreters.** `test__interpreters` / `test__interpchannels` are
  PEP 554 subinterpreters. Match CPython's behaviour: if CPython skips on this
  build, gopy skips; otherwise port the `_interpreters` surface the tests reach.

## Checklist

- [ ] P1: `os.altsep`
- [ ] P1: module-object `dir()` surface (`__cached__`, `__doc__`, `__loader__`, `__spec__`) for `test_pkg`
- [ ] P2: vendor `modulefinder`
- [ ] P2: vendor `pyclbr`
- [ ] P2: vendor `zipapp`
- [ ] P3: frozen `__hello__` + frozen module table for `test_frozen`
- [ ] P4: `test_runpy` package-init exception path
- [ ] P5: re-audit `test_import/`, `test_importlib/`, `test_module/`
- [ ] P6: `test__interpreters` / `test__interpchannels` parity with CPython skip/run
