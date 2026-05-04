# Changelog

All notable changes to `gopy` are documented here.

The format is loosely based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each released version has a dated fragment in the [`changelog/`](changelog/)
folder; this file is the aggregated index.

## Unreleased

* build: bump minimum Go to 1.26.
* feat(arena): port `cpython/Python/pyarena.c` to the `arena` package.
  Bump allocator with linked fixed-size blocks for the compiler
  pipeline.
* feat(pythread): port the cross-platform half of
  `cpython/Python/thread.c` to the `pythread` package. Goroutine-backed
  Start/Join handles, ident allocation, stacksize stubs, and the
  `TimeoutMax` constant.
* feat(pysync): port `cpython/Python/lock.c`,
  `cpython/Python/parking_lot.c`, and `cpython/Python/critical_section.c`
  to the `pysync` package. Address-keyed parking lot, byte-flag
  `Mutex`, `Event`, `OnceFlag`, `RecursiveMutex`, `SeqLock`, and
  PEP 703 `CriticalSection` stack.
* feat(pysync): add `RWMutex` and `RawMutex` so the `lock.c` port is
  complete.
* feat(hash): port the seed-init half of `cpython/Python/bootstrap_hash.c`.
  PYTHONHASHSEED parsing, the LCG matching `lcg_urandom`, and an OS
  entropy fallback through `crypto/rand`.

## v0.0.0 - 2026-05-04

See [`changelog/v0.0.0.md`](changelog/v0.0.0.md).

* Initial public scaffolding: Go module layout, `cmd/gopy` entry point,
  static `build` package, license, contribution guide, security policy,
  CI and release workflows.
