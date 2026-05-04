# Changelog

All notable changes to `gopy` are documented here.

The format is loosely based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each released version has a dated fragment in the [`changelog/`](changelog/)
folder; this file is the aggregated index.

## Unreleased

## v0.2.0 - 2026-05-04

See [`changelog/v0.2.0.md`](changelog/v0.2.0.md).

* feat(objects): land the v0.2 object protocol foundation. Header,
  VarHeader, Object interface, atomic refcount, Type with the v0.2
  slot subset, C3 MRO.
* feat(objects): concrete builtins for the gate. int (with -5..256
  cache), float, bool, None, NotImplemented, tuple (empty-tuple
  singleton, CPython-compatible tuplehash), list, dict (open-addressed
  with the CPython probing sequence), slice, range.
* feat(abstract): subset of `cpython/Objects/abstract.c`. Length,
  GetItem, SetItem, Add, Subtract, Multiply, Iter, IterNext.
* test(objtest): v0.2 gate harness. Build a dict, hash a tuple,
  iterate a list, plus smoke tests for caching, MRO, repr, range
  iteration.

## v0.1.0 - 2026-05-04

See [`changelog/v0.1.0.md`](changelog/v0.1.0.md).

* build: bump minimum Go to 1.26.
* feat(arena): port `cpython/Python/pyarena.c` to the `arena` package.
* feat(pythread): port the cross-platform half of
  `cpython/Python/thread.c` to the `pythread` package.
* feat(pysync): port `cpython/Python/lock.c`,
  `cpython/Python/parking_lot.c`, and `cpython/Python/critical_section.c`
  to the `pysync` package.
* feat(hash): port the seed-init half of `cpython/Python/bootstrap_hash.c`
  to the `hash` package.

## v0.0.0 - 2026-05-04

See [`changelog/v0.0.0.md`](changelog/v0.0.0.md).

* Initial public scaffolding: Go module layout, `cmd/gopy` entry point,
  static `build` package, license, contribution guide, security policy,
  CI and release workflows.
