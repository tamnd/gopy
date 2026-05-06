# Changelog

All notable changes to `gopy` are documented here.

The format is loosely based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each released version has a dated fragment in the [`changelog/`](changelog/)
folder; this file is the aggregated index.

## Unreleased

## v0.8.0 - 2026-05-06

See [`changelog/v0.8.0.md`](changelog/v0.8.0.md).

* feat(marshal): `TYPE_LONG` decoder, `FLAG_REF` back-references,
  interned-string tags, `TYPE_CODE` round-trip for the 3.11+ wire
  format, `TYPE_SET` / `TYPE_FROZENSET` / `TYPE_DICT` /
  `TYPE_BINARY_COMPLEX` encoding and decoding.
* feat(marshal): `WritePyc` / `ReadPyc` implement the PEP 552
  16-byte `.pyc` header (timestamp and hash variants).
* feat(codecs): codec registry (`Register` / `Lookup`), built-in
  utf-8, ascii, and latin-1 codecs with alias normalization, and
  `Encode` / `Decode` entry points.
* feat(imp): frozen module table, sys.modules registry, inittab,
  `InitImportlib` two-phase bootstrap sequence, `ExecCodeModule`,
  source and `.pyc` loaders, and `ImportModuleLevel` with the
  sys.modules → frozen → built-in lookup chain.
* feat(vm): `IMPORT_NAME` and `IMPORT_FROM` bytecode arms wired into
  the eval loop via `vmExecutor` implementing `imp.Executor`.
* feat(objects): `Module` type, `Set` and `Frozenset` types.
* feat(errors): `ImportError` / `ModuleNotFoundError` hierarchy.

## v0.7.0 - 2026-05-06

See [`changelog/v0.7.0.md`](changelog/v0.7.0.md).

* feat(initconfig): port `Python/preconfig.c`, `Python/getenv.c`, and
  the `PyConfig` subset of `Python/initconfig.c`. PyPreConfig and
  PyConfig with the three named initializers, env-var merge,
  `_PyOS_GetOpt` plus `config_parse_cmdline`, and `_PyConfig_Read`
  walking the precedence chain.
* feat(pathconfig): port `Python/getpath.c` for darwin and linux.
  Resolves prefix / exec_prefix / `sys.path` from the executable's
  location with the documented fallbacks.
* feat(lifecycle): port `pylifecycle.c` `pyinit_core` / `pyinit_main`
  / `Py_FinalizeEx`, plus `Modules/main.c` `Py_Main` as the unified
  entry that reads argv into PyConfig, inits, dispatches, finalizes.
* feat(pythonrun): port `pythonrun.c` `PyRun_SimpleStringFlags`,
  `PyRun_AnyFileExFlags`, `PyRun_InteractiveLoopFlags`, and the
  three-arm dispatch panel. The v0.6 `cmd/gopy -c` smoke fixtures
  re-root onto `pythonrun.RunString`.
* feat(errors): port `_PyErr_Print` with SystemExit short-circuit.
* feat(sys): port the `_PySys_Create` static attribute slice, the
  PyConfig-driven `sys.argv` / `sys.path` / `sys.flags` panel, the
  runtime helpers (`exit`, `getrecursionlimit`, `setrecursionlimit`,
  `getrefcount`, `intern`, `gettrace`, `settrace`,
  `getfilesystemencoding`), and `sys.implementation` with the gopy
  `cache_tag = "gopy-3140"`.
* feat(builtins): port the iteration panel (`iter`, `next`,
  `enumerate`, `zip`, `range`, `reversed`, `map`, `filter`), the
  reflection panel (`type`, `isinstance`, `issubclass`, `id`,
  `hash`, `repr`, `len`, `callable`), the attribute panel
  (`getattr`, `hasattr`, `setattr`, `delattr`), the aggregation
  panel (`sum`, `min`, `max`, `any`, `all`, `sorted`), the numeric
  / format panel (`abs`, `divmod`, `pow`, `chr`, `ord`, `bin`,
  `oct`, `hex`, `ascii`, `format`), and the constructor wrappers
  (`int`, `float`, `bool`, `list`, `tuple`, `dict`). PEP 515
  underscore stripping in `int(...)` parsing.
* feat(warnings): port `_warnings.c`. Action / Category / Filter
  model, `_PyWarnings_InitState` seeded defaults, `_Py_Warn`
  dispatch, the per-module `__warningregistry__` dedup with the
  `__once__` bucket for `ActionOnce`, and the user-facing
  `simplefilter` / `filterwarnings` / `resetwarnings` from
  `Lib/_py_warnings.py`.

## v0.5.5 - 2026-05-05

See [`changelog/v0.5.5.md`](changelog/v0.5.5.md).

* feat(parser/lexer): port `Parser/lexer/state.[ch]`,
  `Parser/lexer/buffer.[ch]`, and `Parser/lexer/lexer.c`. Regular
  mode FSM, INDENT / DEDENT, line continuation, PEP 701 f-strings,
  PEP 750 t-strings, COMMENT / NL / type-comment emission, plus
  string / file / readline drivers.
* feat(parser/pegen): scaffold of `Parser/pegen.[ch]`. Parser
  struct, fillToken, mark / reset / peek, Expect / ExpectName.
  Action helper shape ops (sequence, dotted-name, expr-context,
  expr-name table) from `Parser/action_helpers.c`.
* feat(parser/errors): port of the SyntaxError text panel from
  `Parser/pegen_errors.c` plus a small Build / Raise / RaiseRange
  surface and the lexer-errcode dispatch.
* feat(parser/string): port of `Parser/string_parser.c`. Prefix
  and quote stripping, escape decoding for unicode and bytes
  literals, raw / no-backslash fast paths.

## v0.5.0 - 2026-05-05

See [`changelog/v0.5.0.md`](changelog/v0.5.0.md).

* feat(ast): port `Python-ast.c`, `ast.c`, `ast_preprocess.c`, and
  `ast_unparse.c`. Generated nodes from `Python.asdl`, the full
  Validate panel (forbidden names, comprehension shape, expr_context,
  Starred placement, match-pattern shape, PEP 695 type-param), the
  3.14 preprocess folds, and source round-trip.
* feat(future): port `future.c`. Detects `from __future__` imports
  and surfaces feature flags to the rest of the pipeline.
* feat(symtable): full port of `symtable.c`. Build, analyse, errors
  panel, class name mangling.
* feat(compile/instrseq): port `instruction_sequence.c`. Sequence,
  Instr, label model, ApplyLabelMap.
* feat(compile/codegen): port `codegen.c`. Every statement and
  expression visitor needed for the v0.5 gate, including the eight
  match patterns, four comprehension kinds, with / async with, try /
  try*, the assignment-target panel, and TypeAlias via
  INTRINSIC_TYPEALIAS.
* feat(compile/flowgraph): port of `flowgraph.c`. CFG-driven pass
  driver, int-int BINARY_OP folding, jump threading, conditional-jump
  propagation, unreachable-block elimination, post-terminator dead
  code, redundant-NOP compaction. CFG-based stackdepth, swaptimize,
  super-instructions, LOAD_FAST ref-stack, and cold-block hoist
  remain for follow-on.
* feat(compile/assemble): port of `assemble.c`. EXTENDED_ARG widening,
  PEP 626 line table, PEP 657 exception table, type-keyed const
  dedup, `co_qualname` walk, full `co_flags` assembly.
* feat(compile/compiler): top-level `Compile` entry point that walks
  symtable, codegen, flowgraph, and assemble per scope.
* feat(compile/dis): port of `dis.dis`. Listings recombine
  EXTENDED_ARG prefixes and recurse into nested Code objects.
* feat(marshal): version-5 wire format skeleton plus roundtrip test.
  Code-object marshal arm and CPython byte-parity land in v0.8.
* feat(tokenize): skeleton wrapper around `Python-tokenize.c`. Type
  table generated from `Grammar/Tokens` and `pycore_token.h`. The
  Iter / Token surface plus the lexer state machine arrive in v0.9.
* test(v05test): cross-cut gate. Structural panel (empty, assign,
  binary fold, load-after-store, if/while, def, async def). Ten
  disassembly goldens (1629) under `testdata/golden/` covering
  `empty_module`, `simple_assign`, `binary_add`, `load_after_store`,
  `if_pass`, `while_pass`, `def_add_one`, `async_def_pass`,
  `class_pass`, `type_alias`. Refresh via `go test -update`.

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
