# Changelog

All notable changes to `gopy` are documented here.

The format is loosely based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each released version has a dated fragment in the [`changelog/`](changelog/)
folder; this file is the aggregated index.

## Unreleased

## v0.5.0 - 2026-05-04

See [`changelog/v0.5.0.md`](changelog/v0.5.0.md).

* feat(ast): port `Python-ast.c`, `ast.c`, `ast_preprocess.c`, and
  `ast_unparse.c`. Generated nodes from `Python.asdl`, validation,
  literal constant folding, and source round-trip.
* feat(future): port `future.c`. Detects `from __future__` imports
  and surfaces feature flags to the rest of the pipeline.
* feat(symtable): full port of `symtable.c`. Build, analyse, and the
  errors panel. Class name mangling included.
* feat(compile/instrseq): port `instruction_sequence.c`. Sequence,
  Instr, label model, ApplyLabelMap.
* feat(compile/codegen): port `codegen.c`. Every statement and
  expression visitor needed for the v0.5 gate, including the eight
  match patterns, four comprehension kinds, with / async with, try /
  try*, and the assignment-target panel.
* feat(compile/flowgraph): skeleton port of `flowgraph.c`. Public
  Optimize entry point, label resolution, forward stackdepth analysis
  on a hand-written effect table. The full optimisation panel lands
  incrementally per the 1627 spec.
* feat(compile/assemble): port of `assemble.c` for the code stream.
  EXTENDED_ARG widening and flat 3.11+ localsplus layout. Line and
  exception table writers land alongside the optimisation panel.
* feat(compile/compiler): top-level `Compile` entry point that walks
  symtable, codegen, flowgraph, and assemble.
* feat(compile/dis): port of `dis.dis`. Listings recombine
  EXTENDED_ARG prefixes and recurse into nested Code objects.
* feat(tokenize): skeleton wrapper around `Python-tokenize.c`. The
  public Iter / Token surface is stable; the lexer state machine
  arrives with the parser port.
* test(v05test): cross-cut gate that pins the end-to-end pipeline on
  empty modules, simple assignments, and binary operations.

## v0.4.0 - 2026-05-04

See [`changelog/v0.4.0.md`](changelog/v0.4.0.md).

* feat(pystrconv): port `cpython/Python/pyctype.c`, `pystrcmp.c`,
  `mystrtoul.c`, `pystrhex.c`, plus a Go-strconv-backed wrapper for
  `pystrtod.c` and `dtoa.c`. ASCII classification, case-insensitive
  compare, hex encoding with separators, integer parsing with base
  autodetect, IEEE-754 ParseFloat, and FormatFloat covering the
  `r/s/g/G/e/E/f/F/%` codes with FloatFormatFlag.
* feat(pymath): port `pymath.c` and `pyfpe.c`. CopySign, IsNaN, IsInf,
  IsFinite, Log1p, Hypot plus the FPE legacy sentinels.
* feat(hash): full port of `pyhash.c`. SipHash-1-3 Buffer, FNV-1a
  BufferFNV, KeyedHash, Pointer, Double, GetFuncDef, plus the numeric
  hash constants. Reference vectors against CPython under
  PYTHONHASHSEED=0.
* feat(format): port `formatter_unicode.c`. Spec parser plus
  FormatString, FormatInt, and FormatFloat covering the full
  `[[fill]align][sign][z][#][0][width][,_][.precision][type]`
  mini-language.
* test(v04test): cross-cut gate that pins hash.Buffer, ParseFloat,
  FormatFloat, and format.FormatInt to CPython 3.14 reference values.

## v0.3.0 - 2026-05-04

See [`changelog/v0.3.0.md`](changelog/v0.3.0.md).

* feat(errors): port `cpython/Python/errors.c` and the gating subset of
  `cpython/Objects/exceptions.c`. Set, SetString, Format, Occurred,
  Clear, Fetch, Restore, Raise, RaiseFrom, NormalizeException, Print,
  AttachTraceback. BaseException class hierarchy with KeyError __str__
  override.
* feat(traceback): port `cpython/Python/traceback.c` data shape and
  formatting. Entry, Push, Format, FormatException.
* feat(errors/suggest): port `cpython/Python/suggestions.c`. SuggestAttr,
  SuggestKey backed by bounded Levenshtein distance.
* feat(gc): refcount-only path of `cpython/Python/gc.c`. Track, Untrack,
  RegisterFinalizer, Finalize. Cycle collection deferred to v0.10.
* feat(brc): field layout from `cpython/Python/brc.c`. Operations are
  no-ops in the GIL build; the queue drains land in v0.14.
* feat(state): skeleton of `cpython/Python/pystate.c`. Runtime,
  Interpreter, Thread with the per-thread exception slot.
* feat(objects): v0.3 placeholder str so exception args round-trip
  through the protocol. Replaced by the unicodeobject port in v0.4.

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
