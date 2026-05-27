---
format: md
id: 1700_gopy_test_e2e
title: "1700. gopy end-to-end test suite porting plan"
sidebar_label: "1700. test_e2e"
sidebar_position: 1700
slug: /specs/1700-test_e2e
description: "Port CPython 3.14's Lib/test/ behaviour suite into gopy. v0.0-v0.12 left the test gate undertest. This spec catalogs every test_*.py file, assigns each to the gopy version that owns it, and tracks current port status."
---


## Checklist

Infrastructure (gates the panel work below):
- [x] MANIFEST: `test/cpython/MANIFEST.txt` lists 447 entries
      (248 ready, 125 deferred, 66 out-of-scope, 8 done).
- [x] Harness: `test/regrtest/` walks the manifest and shells out to
      the gopy binary per entry. Outcome buckets: pass / fail / skip
      / missing / timeout / error.
- [ ] Unittest enablement (spec 1701). Required before any panel
      task can move past `pending`. See 1701 checklist for status.

Per-version panel tasks (each lands its slice of `test/cpython/` and
fixes the underlying gopy package on red):
- [ ] v0.2.0 type panel (#479)
- [x] v0.3.0 exceptions panel (#485) — spec [1721](1721_exceptions_traceback_test_panel.md) shipped (all 7 phases green)
- [ ] v0.4.0 number / string panel (#487)
- [x] v0.5.0 compile panel (#475) — spec [1720](1720_compile_codegen_test_panel.md) shipped (all 9 phases green)
- [x] v0.5.5 lexer panel (#484) — full port under [spec 1710](1710_v0124_lexer_tokenizer_full_port.md) (closed by [spec 1718](1718_lexer_gate_closers.md), PR #76, `bd1e9cce`)
- [ ] v0.7.0 lifecycle panel (#482)
- [ ] v0.8.0 import panel (#481)
- [ ] v0.9.0 vm tail panel (#483)
- [ ] v0.10.0 gc panel (#474)
- [ ] v0.10.1 language / stdlib panel (#486)
- [x] v0.10.2 parser panel (#476) — spec [1719](1719_parser_ast_full_port.md) shipped (PR #78, `9d0a3199`); test_asdl_parser + test_ast/ + test_patma pending
- [ ] v0.11.0 specializer / monitor panel (#477)
- [ ] v0.12.0 optimizer panel (#478)

## Goal

Make CPython's behaviour-test panel the gopy gate. v0.0 through v0.12
shipped feature work and small per-package gates (`v04test/`,
`v05test/`, `v012test/`), but the canonical CPython suite at
`/Users/apple/github/python/cpython/Lib/test/test_*.py` was never wired up. This
spec catalogs the full corpus (398 top-level `test_*.py` files plus 36
`test_*/` packages), assigns each to the gopy release that owns the
underlying feature, and tracks where the port currently stands.

The work this spec drives is mechanical: for every in-scope test
module, run it through gopy, fix any divergence in the gopy package
that owns the divergence, and pin the green run in CI. The test files
themselves are imported verbatim from CPython 3.14 into a new
`test/cpython/` tree under the gopy module so future CPython upgrades
can rebase via `git diff`.

The matching feature work has already shipped (v0.1 through v0.12).
The remaining gap is observability: every shipped feature should be
gated by the upstream test it was specified against, not just the
package-local gate.

## Sources of truth

| CPython location                    | Count | Notes                                |
|-------------------------------------|------:|--------------------------------------|
| `Lib/test/test_*.py`                |   398 | Flat module per test                 |
| `Lib/test/test_*/`                  |    36 | Package per test (sub-suites)        |
| `Lib/test/support/`                 |     1 | Shared harness helpers               |
| `Lib/test/regrtest.py` (driver)     |     1 | Entry point; ported as `test/regrtest/` |

Total: 434 test entry points to triage.

## Package layout

```
test/
  cpython/                   verbatim Lib/test/ subset, one file or dir per entry
    test_grammar.py          imported from cpython unchanged
    test_dis.py
    ...
    support/                 Lib/test/support/ helpers gopy needs
  regrtest/                  Lib/test/regrtest.py port; entry point
    main.go                  picks up tests under test/cpython/, runs them via gopy
  goldens/                   reference outputs (dis listings, ast.dump, etc.)
```

The flat `test/cpython/` mirror is intentional: test files stay
byte-equal to CPython so `diff` against `~/github/python/cpython/Lib/test/`
shows zero noise across CPython point releases.

`test/regrtest/` ports the harness only enough to drive the test
files we have ported. The gopy `test/regrtest/run.go` produces a
report shape that lines up with CPython's so divergence is visible
in CI.

## Categorization

Every `test_*.py` file is assigned to one of the following
categories. The category determines the gopy package that owns the
work, the gopy release that ships the feature, and which existing
package gate the file replaces.

| Category | gopy package(s) | Owning release |
|----------|----------------|---------------:|
| lexer/tokenizer | `parser/lexer/`, `tokenize/` | v0.5.5, v0.9 |
| parser/grammar/ast | `parser/`, `ast/` | v0.5.5, v0.10.2 |
| compile/codegen | `compile/`, `ast/` | v0.5, v0.10.2 |
| bytecode/dis | `compile/`, `dis/` | v0.5 |
| vm/eval loop | `vm/`, `frame/`, `stackref/`, `eval/` | v0.6, v0.9 |
| builtins/types | `builtins/`, `objects/` | v0.2, v0.4, v0.7 |
| classes/descr/methods | `objects/type.go`, `objects/descr*` | v0.2, v0.10.1 |
| iterators/generators | `objects/iter*`, `vm/eval_gen.go` | v0.9 |
| exceptions/traceback | `errors/`, `traceback/` | v0.3 |
| modules/imports | `imp/`, `importlib/` | v0.8, v0.10.1 |
| gc | `gc/` | v0.3, v0.10 |
| codecs/encodings | `codecs/` | v0.8 |
| io/buffer | `io/`, `objects/file.go` | v0.10.1 |
| collections/containers | `objects/`, `collections/` | v0.2, v0.4 |
| math/numbers | `pystrconv/`, `pymath/`, `objects/long.go` | v0.4 |
| typing | `typing/` | v0.10.1 |
| context/contextvars | `context/`, `hamt/` | v0.9 |
| logging/warnings | `warnings/`, `logging/` | v0.7, v0.10.1 |
| cli/REPL | `cmd/gopy`, `pythonrun/`, `myreadline/` | v0.7, v0.10.1 |
| optimizer/specializer | `specialize/`, `optimizer/`, `monitoring/` | v0.11, v0.12 |
| stdlib (data formats) | `marshal/`, `json/`, `pickle/` | v0.8, deferred |
| stdlib (utilities) | various | deferred (post-1.0) |
| out-of-scope | n/a | n/a (CPython-impl-specific) |

## Status legend

| Mark | Meaning |
|------|---------|
| done | Already runs green against the gopy package; needs to be wired into `test/cpython/` and CI |
| ready | Underlying feature has shipped; test file just needs to be imported and pinned |
| pending | Feature has shipped but test file has known divergences that need fixing |
| deferred | Feature has not shipped yet (post-v0.12 work); test stays out-of-tree until the feature ships |
| out-of-scope | CPython-implementation-specific or platform-specific; will not port |

The triage table below sets each test's initial mark. The mark moves
to `done` only after the file lives under `test/cpython/`, runs green
in `go test ./test/...`, and the relevant CI workflow lights up.

## Test file triage

The table below covers every file directly under
`/Users/apple/github/python/cpython/Lib/test/`. The `Owner` column names the
gopy release that owns the feature and therefore owns the port; for
features that span multiple releases (e.g. `test_compile.py` covers
v0.5 codegen plus v0.10.1 `__build_class__`) the latest covering
release is named.

### Lexer / tokenizer (5 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_tokenize | v0.9 | done | Closed by spec 1718 P29..P59 (PR #76, `bd1e9cce`) |
| test_tabnanny | v0.9 | done | Rides on `_tokenize` iterator; greens with spec 1718 (PR #76, `bd1e9cce`) |
| test_source_encoding | v0.5.5 | done | Closed by spec 1718 P6..P29 cookie/BOM + P60 long-cookie line (PR #76, `bd1e9cce`) |
| test_utf8source | v0.5.5 | done | parser/lexer utf-8 default; greens with spec 1718 (PR #76, `bd1e9cce`) |
| test_keyword | v0.5.5 | done | `keyword` module is a generated table; pinned alongside spec 1718 (PR #76, `bd1e9cce`) |

### Parser / grammar / AST (27 files + test_ast/, test_future_stmt/, test_peg_generator/)

Spec [1719](1719_parser_ast_full_port.md) shipped (all phases green, PR #78, `9d0a3199`).

| Test | Owner | Mark | Commit | Notes |
|------|-------|------|--------|-------|
| test_grammar | v0.10.2 | done | `9d0a3199` | 75/75 green — spec 1719 P7 (PR #78) |
| test_syntax | v0.12.5 | done | `9d0a3199` | 45/45 green (7 skipped) — spec 1719 (PR #78) |
| test_eof | v0.12.5 | done | `9d0a3199` | 6/6 green — spec 1719 (PR #78) |
| test_fstring | v0.12.5 | done | `9d0a3199` | 90/90 green — spec 1719 (PR #78) |
| test_tstring | v0.12.5 | done | `9d0a3199` | 12/12 green — spec 1719 (PR #78) |
| test_named_expressions | v0.12.5 | done | `9d0a3199` | 74/74 green — spec 1719 (PR #78) |
| test_unparse | v0.12.5 | done | `9d0a3199` | 79/79 green — spec 1719 (PR #78) |
| test_asdl_parser | v0.5 | ready | | ast/asdl.go |
| test_pep646_syntax | v0.12.5 | done | `9d0a3199` | 1/1 green — spec 1719 (PR #78) |
| test_type_params | v0.12.5 | done | `9d0a3199` | 108/108 green — spec 1719 (PR #78) |
| test_type_aliases | v0.12.5 | done | `9d0a3199` | 30/30 green — spec 1719 (PR #78) |
| test_type_annotations | v0.12.5 | done | `9d0a3199` | 47/47 green (1 skip) — spec 1719 (PR #78) |
| test_type_comments | v0.12.5 | done | `9d0a3199` | 18/18 green (1 skip) — spec 1719 (PR #78) |
| test_annotationlib | v0.12.5 | done | `9d0a3199` | 117/117 green (1 skip) — spec 1719 (PR #78) |
| test_string_literals | v0.12.5 | done | `9d0a3199` | 20/20 green — spec 1719 (PR #78) |
| test_int_literal | v0.12.5 | done | `9d0a3199` | 6/6 green — spec 1719 (PR #78) |
| test_unicode_identifiers | v0.12.5 | done | `9d0a3199` | 5/5 green — spec 1719 (PR #78) |
| test_patma | v0.9 | ready | | 322/328 partial (6 TestTracing need sys.settrace) |
| test_decorators | v0.12.5 | done | `9d0a3199` | 16/16 green — spec 1719 (PR #78) |
| test_global | v0.12.5 | done | `9d0a3199` | 20/20 green — spec 1719 (PR #78) |
| test_scope | v0.12.5 | done | `9d0a3199` | 41/41 green (3 skipped) — spec 1719 (PR #78) |
| test_keywordonlyarg | v0.12.5 | done | `9d0a3199` | 11/11 green — spec 1719 (PR #78) |
| test_positional_only_arg | v0.12.5 | done | `9d0a3199` | 28/28 green — spec 1719 (PR #78) |
| test_unpack | v0.12.5 | done | `9d0a3199` | 2/2 green — spec 1719 (PR #78) |
| test_unpack_ex | v0.12.5 | done | `9d0a3199` | 1/1 green — spec 1719 (PR #78) |
| test_metaclass | v0.12.5 | done | `9d0a3199` | 1/1 green — spec 1719 (PR #78) |
| test_subclassinit | v0.12.5 | done | `9d0a3199` | 17/17 green — spec 1719 (PR #78) |
| test_ast/ (dir) | v0.10.2 | ready | | blocked on `_ast_unparse` (task #74) |
| test_future_stmt/ (dir) | v0.12.5 | done | `9d0a3199` | 7/7 green (AnnotationsFutureTestCase); 7 FutureTest rows need spawn_python — spec 1719 (PR #78) |
| test_peg_generator/ (dir) | v0.5.5 | deferred | | tools/parser_gen still WIP |

### Compile / codegen (36 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_compile | v0.10.1 | done | 185/185 — spec 1720 (PR #79, `9cb0f44d`) |
| test_compileall | v0.10.1 | done | 145/145 — spec 1720 (PR #79, `9cb0f44d`) |
| test_compiler_assemble | v0.5 | done | 145/145 — spec 1720 (PR #79, `9cb0f44d`) |
| test_compiler_codegen | v0.5 | done | 5/5 — spec 1720 (PR #79, `9cb0f44d`) |
| test_codeop | v0.10.1 | done | 9/9 — spec 1720 (PR #79, `9cb0f44d`) |
| test_peepholer | v0.5 | done | 130/130 — spec 1720 (PR #79, `9cb0f44d`) |
| test_dis | v0.5 | done | 145/145 — spec 1720 (PR #79, `9cb0f44d`) |
| test_opcodes | v0.5 | done | 8/8 — spec 1720 (PR #79, `9cb0f44d`) |
| test_generated_cases | v0.12 | out-of-scope | CPython-internal Tier1/Tier2 JIT generator toolchain |
| test__opcode | v0.5 | done | 7/7 — spec 1720 (PR #79, `9cb0f44d`) |
| test_code | v0.5 | done | 37/37 (25 skipped) — spec 1720 (PR #79, `9cb0f44d`) |
| test_code_module | v0.10.1 | done | 17/17 — spec 1720 (PR #79, `9cb0f44d`) |
| test_codeccallbacks | v0.8 | done | 43/43 — spec 1720 (PR #79, `9cb0f44d`) |
| test_codecencodings_cn | v0.8 | deferred | CJK codecs not shipped |
| test_codecencodings_hk | v0.8 | deferred | CJK codecs not shipped |
| test_codecencodings_iso2022 | v0.8 | deferred | iso2022 codecs not shipped |
| test_codecencodings_jp | v0.8 | deferred | CJK codecs not shipped |
| test_codecencodings_kr | v0.8 | deferred | CJK codecs not shipped |
| test_codecencodings_tw | v0.8 | deferred | CJK codecs not shipped |
| test_codecmaps_cn | v0.8 | deferred | CJK codec maps |
| test_codecmaps_hk | v0.8 | deferred | CJK codec maps |
| test_codecmaps_jp | v0.8 | deferred | CJK codec maps |
| test_codecmaps_kr | v0.8 | deferred | CJK codec maps |
| test_codecmaps_tw | v0.8 | deferred | CJK codec maps |
| test_codecs | v0.8 | done | 287/287 (13 skipped) — spec 1720 (PR #79, `9cb0f44d`) |
| test_charmapcodec | v0.8 | deferred | charmap codec not shipped |
| test_multibytecodec | v0.8 | deferred | multibyte codec base class |
| test_clinic | v0.5 | out-of-scope | CPython tooling, not a runtime feature |
| test_dictcomps | v0.5 | done | 10/10 — spec 1720 (PR #79, `9cb0f44d`) |
| test_listcomps | v0.5 | done | 61/61 (1 skipped PEP 667) — spec 1720 (PR #79, `9cb0f44d`) |
| test_setcomps | v0.5 | done | 2/2 — spec 1720 (PR #79, `9cb0f44d`) |
| test_genexps | v0.9 | done | doctest green — spec 1720 (PR #79, `9cb0f44d`) |
| test_lltrace | v0.6 | out-of-scope | low-level dispatch trace, build-time |
| test_thread_local_bytecode | v0.11 | deferred | per-thread quickening (post-v0.12) |
| test_print | v0.7 | done | 9/9 — spec 1720 (PR #79, `9cb0f44d`) |
| test_format | v0.4 | done | green — spec 1720 (PR #79, `9cb0f44d`) |

### VM / eval loop (22 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_call | v0.9 | ready | CALL / KW_NAMES family |
| test_extcall | v0.9 | ready | extended call (\* and \*\* expansion) |
| test_frame | v0.9 | ready | frame object surface |
| test_eval | v0.10.1 | ready | eval() builtin |
| test_dynamic | v0.7 | ready | exec/eval against frame.f_locals |
| test_richcmp | v0.2 | ready | rich comparison protocol |
| test_compare | v0.2 | ready | comparison fallbacks |
| test_unary | v0.4 | ready | unary number ops |
| test_pow | v0.4 | ready | builtin pow() |
| test_augassign | v0.5 | ready | augassign codegen |
| test_with | v0.9 | ready | context manager; with statement |
| test_yield_from | v0.9 | ready | yield from semantics |
| test_coroutines | v0.9 | ready | async def + await codegen |
| test_asyncgen | v0.9 | ready | async generators |
| test_generator_stop | v0.9 | ready | PEP 479 |
| test_generators | v0.9 | ready | generator protocol panel |
| test_iter | v0.2 | ready | iter()/next() builtins |
| test_iterlen | v0.2 | ready | __length_hint__ |
| test_contains | v0.2 | ready | __contains__ protocol |
| test_index | v0.4 | ready | __index__ protocol |
| test_isinstance | v0.2 | ready | isinstance() panel |
| test_typechecks | v0.2 | ready | type-check fast paths |

### Exceptions / traceback (8 files)

Spec [1721](1721_exceptions_traceback_test_panel.md) shipped (all 7 phases green).

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_exceptions | v0.12.7 | done | 107/107 green (2 skipped: _testcapi) |
| test_baseexception | v0.12.7 | done | 11/11 green |
| test_exception_hierarchy | v0.12.7 | done | 16/16 green (1 skip: WindowsError) |
| test_exception_variations | v0.12.7 | done | 30/30 green |
| test_exception_group | v0.12.7 | done | 52/52 green |
| test_except_star | v0.12.7 | done | 60/60 green |
| test_traceback | v0.12.7 | done | 370/370 green |
| test_raise | v0.12.7 | done | 37/37 green |

### Builtins / types (28 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_builtin | v0.7 | ready | full builtins module surface |
| test_int | v0.2 | ready | int type + small-int cache |
| test_long | v0.2 | ready | unbounded int (long) panel |
| test_float | v0.4 | ready | float parse / format / arithmetic |
| test_complex | v0.4 | ready | complex literal + arithmetic |
| test_bool | v0.2 | ready | bool subclass of int |
| test_bytes | v0.4 | ready | bytes immutable buffer |
| test_str | v0.4 | ready | str (PEP 393 unicode) |
| test_unicodedata | v0.4 | ready | unicodedata data tables |
| test_ucn | v0.4 | ready | \\N{NAME} resolver |
| test_unicode_file | v0.10.1 | ready | filesystem unicode path |
| test_unicode_file_functions | v0.10.1 | ready | os path unicode |
| test_list | v0.2 | ready | list type panel |
| test_tuple | v0.2 | ready | tuple type panel |
| test_dict | v0.2 | ready | dict type panel |
| test_dictviews | v0.2 | ready | dict_keys / dict_values / dict_items |
| test_set | v0.4 | ready | set / frozenset panel |
| test_range | v0.2 | ready | range type |
| test_slice | v0.2 | ready | slice type |
| test_memoryview | v0.4 | ready | memoryview surface |
| test_buffer | v0.4 | out-of-scope | C-level buffer protocol API |
| test_funcattrs | v0.5 | ready | function attribute panel |
| test_property | v0.10.1 | ready | property descriptor |
| test_strtod | v0.4 | done | gated implicitly via test_float |
| test_numeric_tower | v0.4 | ready | numbers ABC tower |
| test_abstract_numbers | v0.4 | ready | numbers module |
| test_userdict | v0.10.1 | ready | UserDict subclassing |
| test_userlist | v0.10.1 | ready | UserList subclassing |
| test_userstring | v0.10.1 | ready | UserString subclassing |
| test_structseq | v0.7 | ready | named-tuple style structseq |

### Classes / descriptors / methods (12 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_class | v0.10.1 | ready | __build_class__ panel |
| test_descr | v0.10.1 | ready | descriptors / slots / methods |
| test_descrtut | v0.10.1 | ready | descriptor tutorial doctest |
| test_super | v0.10.1 | ready | super() resolution |
| test_dynamicclassattribute | v0.10.1 | ready | DynamicClassAttribute |
| test_genericalias | v0.10.1 | ready | builtin __class_getitem__ |
| test_genericclass | v0.10.1 | ready | typing.Generic plumbing |
| test_type_cache | v0.11 | ready | type version cache (specializer) |
| test_types | v0.2 | ready | types module |
| test_abc | v0.10.1 | ready | abc module + ABCMeta |
| test_enum | v0.10.1 | ready | enum module |
| test_typing | v0.10.1 | ready | typing module surface |

### Iterators / generators / coroutines (covered above under VM)

### Modules / imports (12 files + test_import/, test_importlib/, test_module/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test__interpchannels | v0.7 | deferred | PEP 554 interpreters - post-v0.12 |
| test__interpreters | v0.7 | deferred | PEP 554 interpreters - post-v0.12 |
| test_module | v0.7 | ready | module type |
| test_modulefinder | v0.8 | ready | modulefinder utility |
| test_pkg | v0.8 | ready | package-style imports |
| test_pkgutil | v0.8 | ready | pkgutil walk_packages etc |
| test_pyclbr | v0.8 | ready | pyclbr class browser |
| test_pkgimport | v0.8 | ready | (covered by test_import/) |
| test_runpy | v0.10.1 | ready | runpy for `gopy -m` |
| test_zipimport | v0.8 | deferred | zipimport not shipped |
| test_zipimport_support | v0.8 | deferred | zipimport not shipped |
| test_zipapp | v0.8 | deferred | zipapp not shipped |
| test_frozen | v0.8 | ready | frozen module flag in marshal |
| test_import/ (dir) | v0.8 | ready | full import system panel |
| test_importlib/ (dir) | v0.8 | ready | importlib package |
| test_module/ (dir) | v0.7 | ready | module-level fixtures |

### GC (3 files + test_free_threading/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_gc | v0.10 | ready | full cycle collector panel |
| test_weakref | v0.10 | ready | weakref + finalizer queue |
| test_finalization | v0.10 | ready | PEP 442 finalizers |
| test_free_threading/ (dir) | v0.10 | deferred | PEP 703 free threading - post-v0.12 |

### Math / numbers (7 files + test_decimal/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_math | v0.4 | ready | math module |
| test_math_property | v0.4 | ready | math hypothesis-style |
| test_cmath | v0.4 | ready | cmath module |
| test_decimal | v0.4 | deferred | decimal not shipped |
| test_fractions | v0.4 | ready | fractions module |
| test_random | v0.7 | ready | random module |
| test_statistics | v0.4 | ready | statistics module |
| test_binop | v0.4 | ready | binary number ops |

### Datetime / time (5 files + test_zoneinfo/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_datetime | v0.9 | ready | datetime module on top of pytime |
| test_time | v0.9 | ready | time module |
| test_calendar | v0.9 | ready | calendar module |
| test_strftime | v0.9 | ready | strftime parity |
| test_strptime | v0.9 | ready | strptime parity |
| test_zoneinfo/ (dir) | v0.9 | deferred | zoneinfo TZ db, post-v0.12 |

### Collections / containers (11 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_collections | v0.10.1 | ready | collections module |
| test_array | v0.10.1 | ready | array module |
| test_heapq | v0.10.1 | ready | heapq module |
| test_bisect | v0.10.1 | ready | bisect module |
| test_queue | v0.10.1 | ready | queue module |
| test_deque | v0.10.1 | ready | collections.deque |
| test_ordered_dict | v0.2 | ready | dict insertion order |
| test_defaultdict | v0.10.1 | ready | collections.defaultdict |
| test_weakset | v0.10 | ready | WeakSet on weakref |
| test_graphlib | v0.10.1 | ready | graphlib topological sort |
| test_enumerate | v0.7 | ready | enumerate() builtin |

### Itertools / functools (6 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_itertools | v0.10.1 | ready | itertools module |
| test_functools | v0.10.1 | ready | functools module |
| test_operator | v0.10.1 | ready | operator module |
| test_contextlib | v0.10.1 | ready | contextlib module |
| test_contextlib_async | v0.10.1 | ready | contextlib async helpers |
| test_reprlib | v0.10.1 | ready | reprlib module |

### Codecs / encodings (covered above under compile/codegen)

### IO / buffer (8 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_io | v0.10.1 | ready | io module on top of objects.File |
| test_file | v0.10.1 | ready | file builtin |
| test_fileio | v0.10.1 | ready | FileIO low-level |
| test_file_eintr | v0.10.1 | ready | EINTR retry |
| test_memoryio | v0.10.1 | ready | BytesIO / StringIO |
| test_bufio | v0.10.1 | ready | BufferedReader |
| test_largefile | v0.10.1 | deferred | needs > 4 GiB filesystem |
| test_univnewlines | v0.10.1 | ready | universal newline mode |

### Filesystem / path (15 files + test_pathlib/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_pathlib/ (dir) | v0.10.1 | ready | pathlib module |
| test_os | v0.10.1 | ready | os module |
| test_posix | v0.10.1 | ready | posix module |
| test_posixpath | v0.10.1 | ready | posixpath module |
| test_ntpath | v0.10.1 | ready | ntpath module |
| test_genericpath | v0.10.1 | ready | shared os.path helpers |
| test_glob | v0.10.1 | ready | glob module |
| test_fnmatch | v0.10.1 | ready | fnmatch module |
| test_shutil | v0.10.1 | ready | shutil module |
| test_tempfile | v0.10.1 | ready | tempfile module |
| test_filecmp | v0.10.1 | ready | filecmp module |
| test_fileinput | v0.10.1 | ready | fileinput module |
| test_fileutils | v0.10.1 | deferred | C-level filesystem path helpers |
| test_stat | v0.10.1 | ready | stat module |
| test_mmap | v0.10.1 | deferred | mmap module not shipped |
| test_nturl2path | v0.10.1 | deferred | windows-only |

### Hash / random (3 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_hash | v0.4 | ready | hash() builtin under SipHash |
| test_hashlib | v0.4 | deferred | hashlib not shipped |
| test_hmac | v0.4 | deferred | hmac not shipped |
| test_secrets | v0.7 | deferred | secrets not shipped |

### Profiling / tracing (12 files)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_sys_setprofile | v0.11 | ready | sys.setprofile under monitor |
| test_sys_settrace | v0.11 | ready | sys.settrace under monitor |
| test_monitoring | v0.11 | done | gated indirectly via v012test |
| test_lltrace | v0.6 | out-of-scope | low-level dispatch trace |
| test_perf_profiler | v0.11 | out-of-scope | perf integration not portable |
| test_tracemalloc | v0.11 | deferred | tracemalloc not shipped |
| test_trace | v0.11 | ready | trace module on top of monitor |
| test_profile | v0.11 | ready | profile module |
| test_cprofile | v0.11 | deferred | cProfile (C accelerator) |
| test_pstats | v0.11 | ready | pstats reader |
| test_optimizer | v0.12 | done | gated via v012test |
| test_opcache | v0.11 | done | gated via v012test |

### Logging / warnings (2 files + test_warnings/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_logging | v0.10.1 | ready | logging module |
| test_warnings/ (dir) | v0.7 | ready | _warnings module |
| test_flufl | v0.7 | ready | __future__ barry_as_FLUFL |

### Cli / REPL (1 file + test_pyrepl/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_repl | v0.7 | ready | basic REPL gate |
| test_pyrepl/ (dir) | v0.10.1 | deferred | pyrepl line editor |
| test_cmd | v0.7 | ready | cmd module (REPL framework) |
| test_cmd_line | v0.7 | ready | cmd-line argument panel |
| test_cmd_line_script | v0.7 | ready | gopy script.py invocation |
| test_code_module | v0.10.1 | ready | code module |
| test_codeop | v0.10.1 | ready | codeop module |
| test_rlcompleter | v0.10.1 | deferred | readline integration |
| test_readline | v0.10.1 | deferred | readline integration |
| test_runpy | v0.10.1 | ready | runpy module (gopy -m) |
| test_argparse | v0.10.1 | ready | argparse module |
| test_optparse | v0.10.1 | ready | optparse module (deprecated but tested) |
| test_getopt | v0.9 | ready | getopt module |
| test_keyword | v0.5.5 | ready | keyword module |
| test_token | v0.9 | ready | token module |

### Distutils / packaging / venv (3 files + test_tools/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_venv | v0.10.1 | deferred | venv depends on filesystem layout |
| test_ensurepip | v0.10.1 | deferred | pip bundling |
| test_sysconfig | v0.10.1 | ready | sysconfig (gopy build flags) |
| test_tools/ (dir) | n/a | out-of-scope | tools shipped with CPython only |

### Stdlib data formats (32 files + test_email/, test_json/, test_zipfile/, test_tomllib/)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_marshal | v0.8 | ready | marshal module (PEP 552) |
| test_pickle | v0.8 | deferred | pickle not shipped |
| test_picklebuffer | v0.8 | deferred | pickle not shipped |
| test_pickletools | v0.8 | deferred | pickle not shipped |
| test_xpickle | v0.8 | deferred | pickle cross-version |
| test_copyreg | v0.8 | deferred | needs pickle |
| test_copy | v0.8 | ready | copy / deepcopy on top of __copy__ slot |
| test_struct | v0.8 | ready | struct module |
| test_json/ (dir) | v0.8 | deferred | json not shipped |
| test_csv | v0.10.1 | deferred | csv not shipped |
| test_configparser | v0.10.1 | deferred | configparser not shipped |
| test_plistlib | v0.10.1 | deferred | plistlib not shipped |
| test_xml_etree | v0.10.1 | deferred | xml.etree not shipped |
| test_xml_etree_c | v0.10.1 | deferred | xml.etree.cElementTree |
| test_xml_dom_minicompat | v0.10.1 | deferred | xml.dom not shipped |
| test_xml_dom_xmlbuilder | v0.10.1 | deferred | xml.dom not shipped |
| test_minidom | v0.10.1 | deferred | xml.dom not shipped |
| test_pulldom | v0.10.1 | deferred | xml.dom not shipped |
| test_sax | v0.10.1 | deferred | xml.sax not shipped |
| test_pyexpat | v0.10.1 | deferred | needs libexpat binding |
| test_xmlrpc | v0.10.1 | deferred | xmlrpc not shipped |
| test_docxmlrpc | v0.10.1 | deferred | xmlrpc not shipped |
| test_email/ (dir) | v0.10.1 | deferred | email not shipped |
| test_zipfile/ (dir) | v0.10.1 | deferred | zipfile not shipped |
| test_zipfile64 | v0.10.1 | deferred | zip64 not shipped |
| test_tarfile | v0.10.1 | deferred | tarfile not shipped |
| test_zlib | v0.10.1 | deferred | zlib not shipped |
| test_gzip | v0.10.1 | deferred | gzip not shipped |
| test_bz2 | v0.10.1 | deferred | bz2 not shipped |
| test_lzma | v0.10.1 | deferred | lzma not shipped |
| test_zstd | v0.10.1 | deferred | zstd new in 3.14 |
| test_html | v0.10.1 | deferred | html module |
| test_htmlparser | v0.10.1 | deferred | html.parser |
| test_quopri | v0.10.1 | deferred | quopri encoder |
| test_base64 | v0.10.1 | ready | base64 module |
| test_binascii | v0.10.1 | ready | binascii module |
| test_uuid | v0.10.1 | ready | uuid module |
| test_mimetypes | v0.10.1 | ready | mimetypes module |
| test_tomllib/ (dir) | v0.10.1 | deferred | tomllib not shipped |
| test_shelve | v0.10.1 | deferred | shelve depends on dbm |
| test_dbm | v0.10.1 | deferred | dbm module |
| test_dbm_dumb | v0.10.1 | deferred | dbm.dumb |
| test_dbm_gnu | v0.10.1 | deferred | dbm.gnu |
| test_dbm_ndbm | v0.10.1 | deferred | dbm.ndbm |
| test_dbm_sqlite3 | v0.10.1 | deferred | sqlite3-backed dbm |
| test_sqlite3/ (dir) | v0.10.1 | deferred | sqlite3 not shipped |
| test_textwrap | v0.10.1 | ready | textwrap module |
| test_string/ (dir) | v0.10.1 | ready | string module |
| test_stringprep | v0.10.1 | ready | stringprep module |
| test_pprint | v0.10.1 | ready | pprint module |
| test_difflib | v0.10.1 | ready | difflib module |
| test_shlex | v0.10.1 | ready | shlex module |
| test_dataclasses/ (dir) | v0.10.1 | ready | dataclasses module |
| test_doctest/ (dir) | v0.10.1 | ready | doctest module |
| test_re | v0.4 | deferred | re module not shipped |
| test_ipaddress | v0.10.1 | ready | ipaddress module |
| test_locale | v0.7 | deferred | locale module |
| test__locale | v0.7 | deferred | _locale module |
| test_colorsys | v0.10.1 | ready | colorsys module |
| test_typing/ (dir) | v0.10.1 | ready | (covered above) |
| test_inspect/ (dir) | v0.10.1 | ready | inspect module |
| test_linecache | v0.7 | ready | linecache module (used by traceback) |
| test_atexit | v0.7 | ready | atexit module |
| test_audit | v0.7 | ready | sys.audit hook |
| test_sundry | v0.7 | ready | sundry stdlib smoke |
| test_support | v0.7 | ready | test.support smoke |
| test_unittest/ (dir) | v0.10.1 | ready | unittest framework |
| test_regrtest | n/a | out-of-scope | regrtest is the harness itself |
| test_script_helper | v0.7 | ready | test.support.script_helper |

### Networking (27 files - all deferred)

All networking tests defer to a future stdlib release. Listed for
inventory completeness:

test_socket, test_socketserver, test_ssl, test_select, test_selectors,
test_poll, test_devpoll, test_epoll, test_kqueue, test_smtplib,
test_smtpnet, test_poplib, test_imaplib, test_ftplib, test_nntplib,
test_httplib, test_httpservers, test_http_cookiejar, test_http_cookies,
test_urllib, test_urllib2, test_urllib_response, test_urllib2_localnet,
test_urllib2net, test_urllibnet, test_urlparse, test_robotparser,
test_webbrowser, test_wsgiref, test_telnetlib, test_netrc.

Owner: post-v0.12. Mark: deferred.

### Subprocess / multiprocessing / threading (8 + 4 dirs)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_threading | v0.7 | ready | threading on top of pythread |
| test_thread | v0.7 | ready | _thread module |
| test_threading_local | v0.7 | ready | thread-local |
| test_threadedtempfile | v0.10.1 | ready | tempfile under threads |
| test_threadsignals | v0.10.1 | ready | signal-from-thread |
| test_thread_local_bytecode | v0.11 | deferred | per-thread quickening |
| test_subprocess | v0.10.1 | deferred | subprocess module |
| test_concurrent_futures/ (dir) | v0.10.1 | deferred | concurrent.futures |
| test_multiprocessing_main_handling | v0.10.1 | deferred | multiprocessing not shipped |
| test_multiprocessing_fork/ (dir) | v0.10.1 | deferred | multiprocessing fork start method |
| test_multiprocessing_forkserver/ (dir) | v0.10.1 | deferred | multiprocessing forkserver |
| test_multiprocessing_spawn/ (dir) | v0.10.1 | deferred | multiprocessing spawn |
| test_fork1 | v0.10.1 | deferred | os.fork tests |
| test_wait3 | v0.10.1 | deferred | os.wait3 |
| test_wait4 | v0.10.1 | deferred | os.wait4 |
| test_popen | v0.10.1 | deferred | os.popen |
| test_signal | v0.10.1 | ready | signal module |
| test_eintr | v0.10.1 | ready | EINTR handling |

### asyncio (1 dir)

| Test | Owner | Mark | Notes |
|------|-------|------|-------|
| test_asyncio/ (dir) | v0.9 | deferred | asyncio package on top of v0.9 coroutines |

### Out-of-scope (CPython-implementation-specific, 21 files + test_capi/, test_cext/, test_cppext/, test_ctypes/, test_gdb/)

These tests target CPython internals (C-API, ctypes, gdb integration,
free-threading build, embedding) or platform-specific shells that
gopy will never carry. They are listed for completeness but stay
out-of-tree.

| Test | Reason |
|------|--------|
| test_capi/ (dir) | C-API surface; gopy is pure Go |
| test_cext/ (dir) | C extensions; gopy uses Go cgo or pure Go |
| test_cppext/ (dir) | C++ extensions |
| test_ctypes/ (dir) | ctypes; defer to gopy/ctypes if ever |
| test_gdb/ (dir) | gdb integration; not portable |
| test_dtrace | dtrace probes |
| test_embed | C embedding API |
| test_external_inspection | gdb-based |
| test_faulthandler | C signal handler |
| test_gettext | gettext locale strings |
| test_msvcrt | windows-only |
| test_winapi | windows-only |
| test_winconsoleio | windows-only |
| test_winreg | windows-only |
| test_winsound | windows-only |
| test_startfile | windows-only |
| test_android | android-only |
| test_apple | apple-only |
| test_curses | curses (terminal UI) |
| test_tcl | tk/tcl GUI |
| test_tkinter/ (dir) | tk/tcl GUI |
| test_ttk_textonly | tk/tcl GUI |
| test_ttk/ (dir) | tk/tcl GUI |
| test_idle | idle (tk-based IDE) |
| test_turtle | turtle (tk-based) |
| test_pdb | pdb debugger |
| test_remote_pdb | pdb remote |
| test_bdb | bdb (pdb dependency) |
| test_perfmaps | perf integration |
| test_utf8_mode | -X utf8 mode (build flag) |
| test_c_locale_coercion | C-locale coercion (build flag) |
| test_osx_env | apple-only |
| test_build_details | CPython build manifest |
| test_stable_abi_ctypes | C ABI surface |
| test_xxlimited | limited C-API smoke |
| test_xxtestfuzz | C fuzz harness |
| test__osx_support | apple-only |
| test_resource | rlimit (POSIX-only, CPython-internal) |
| test_grp | grp module (POSIX-only, niche) |
| test_pwd | pwd module (POSIX-only, niche) |
| test_pty | pty module (POSIX-only, niche) |
| test_tty | tty module (POSIX-only, niche) |
| test_termios | termios (POSIX-only, niche) |
| test_fcntl | fcntl (POSIX-only) |
| test_ioctl | ioctl (POSIX-only) |
| test_openpty | openpty (POSIX-only) |
| test_resource | rlimit (POSIX-only) |
| test_syslog | syslog (POSIX-only, niche) |
| test_getpath | C-level getpath |
| test_getpass | getpass module |
| test_imghdr | imghdr (3.13 deprecated; removed 3.14?) |
| test_mailbox | mailbox module |
| test_audioop | audioop (deprecated 3.13) |
| test_sndhdr | sndhdr (deprecated 3.13) |
| test_aifc | aifc (deprecated 3.13) |
| test_wave | wave module (deprecated 3.13) |
| test_chunk | chunk module (deprecated 3.13) |
| test_cgi | cgi (deprecated 3.13) |
| test_cgitb | cgitb (deprecated 3.13) |
| test_gettext | gettext locale strings |
| test_audit | sys.audit (kept; listed earlier) |
| test_sched | sched module |
| test_timeit | timeit module |
| test_timeout | socket timeout (with networking) |
| test_platform | platform module |
| test_platform_support | platform support |
| test_site | site module |
| test_sysconfig | sysconfig (kept; listed earlier) |
| test_sys | sys module (kept under v0.7) |
| test_typing | typing (kept under v0.10.1) |
| test__colorize | _colorize internal |
| test_rlcompleter | readline (kept; listed earlier) |
| test_doctest | doctest (kept; listed earlier) |
| test_difflib | difflib (kept; listed earlier) |
| test_pprint | pprint (kept; listed earlier) |
| test_textwrap | textwrap (kept; listed earlier) |
| test_string | string (kept; listed earlier) |
| test_stringprep | stringprep (kept; listed earlier) |
| test_xxlimited | limited C-API smoke |
| test_clinic | argument clinic (build tool) |

(The "out-of-scope" list above also lists tests that are kept under
other categories. The kept-under-x line is the authoritative one.)

## Per-version porting tasks

The follow-up tasks are tracked in the project's task list. One task
per gopy version groups every `ready` test file for that version.
The version task is "done" when every `ready` file under it lives
under `test/cpython/` and runs green; deferred and out-of-scope files
are explicitly excluded.

```
test e2e v0.2.0: type panel: int, long, float, bool, list, tuple,
                  dict, dictviews, set, range, slice, iter, contains,
                  isinstance, ordered_dict, types, richcmp, compare
test e2e v0.3.0: exceptions panel: exceptions, baseexception,
                  exception_hierarchy, exception_variations,
                  exception_group, except_star, traceback, raise
test e2e v0.4.0: number / string panel: int_literal, hash, format,
                  float, complex, bytes, str, unicodedata, ucn,
                  memoryview, math, math_property, cmath, fractions,
                  statistics, binop, numeric_tower, abstract_numbers,
                  unary, pow, index, strtod
test e2e v0.5.0: compile panel: compiler_assemble, compiler_codegen,
                  dis, opcodes, _opcode, code, peepholer,
                  decorators, global, scope, keywordonlyarg,
                  positional_only_arg, unpack, unpack_ex, augassign,
                  dictcomps, listcomps, setcomps, asdl_parser,
                  unparse, funcattrs, future_stmt
test e2e v0.5.5: lexer panel: source_encoding, utf8source, keyword
test e2e v0.7.0: lifecycle panel: builtin, repl, cmd, cmd_line,
                  cmd_line_script, atexit, audit, dynamic, enumerate,
                  flufl, linecache, module, print, structseq, sundry,
                  support, sys, warnings, threading, thread,
                  threading_local, script_helper
test e2e v0.8.0: import panel: import, importlib, marshal, copy,
                  struct, modulefinder, pkg, pkgutil, pyclbr,
                  pkgimport, codecs, codeccallbacks, frozen
test e2e v0.9.0: vm tail panel: with, yield_from, generators,
                  generator_stop, genexps, coroutines, asyncgen, call,
                  extcall, frame, patma, datetime, time, calendar,
                  strftime, strptime, getopt, token, tokenize,
                  tabnanny
test e2e v0.10.0: gc panel: gc, weakref, finalization, weakset
test e2e v0.10.1: language / stdlib panel: class, descr, descrtut,
                   super, dynamicclassattribute, genericalias,
                   genericclass, abc, enum, typing, metaclass,
                   subclassinit, property, userdict, userlist,
                   userstring, compile, compileall, codeop,
                   code_module, eval, runpy, io, file, fileio,
                   file_eintr, memoryio, bufio, univnewlines,
                   pathlib, os, posix, posixpath, ntpath,
                   genericpath, glob, fnmatch, shutil, tempfile,
                   filecmp, fileinput, stat, sysconfig, signal,
                   eintr, threadedtempfile, threadsignals,
                   collections, array, heapq, bisect, queue,
                   deque, defaultdict, graphlib, itertools,
                   functools, operator, contextlib,
                   contextlib_async, reprlib, base64, binascii,
                   uuid, mimetypes, textwrap, string,
                   stringprep, pprint, difflib, shlex,
                   dataclasses, doctest, ipaddress, colorsys,
                   inspect, logging, argparse, optparse,
                   annotationlib, type_aliases,
                   type_annotations, type_comments, type_params,
                   pep646_syntax, named_expressions,
                   unicode_identifiers, fstring, tstring,
                   string_literals, unicode_file,
                   unicode_file_functions, unittest
test e2e v0.10.2: parser panel: grammar (done), syntax, eof, ast,
                   peg_generator (deferred until tools/parser_gen)
test e2e v0.11.0: specializer / monitor panel: monitoring,
                   sys_setprofile, sys_settrace, trace, profile,
                   pstats, type_cache, opcache
test e2e v0.12.0: optimizer panel: optimizer, generated_cases
```

## Out-of-tree mirror

`test/cpython/` is meant to be a verbatim git mirror of the relevant
`Lib/test/` files. To keep that contract:

- Each ported file lands at the same relative path it has in CPython
  (e.g. `Lib/test/test_grammar.py` becomes
  `test/cpython/test_grammar.py`).
- The file content is byte-identical to the upstream copy at the
  CPython 3.14 tag we target. Local edits go in a sibling
  `test/cpython/_overrides/` file that the test runner applies before
  invoking the test.
- A vendored copy of `Lib/test/support/` lands under
  `test/cpython/support/` with the same byte-equality contract.
- A small `test/regrtest/run.go` walks `test/cpython/` and invokes
  each test under a gopy-driven `unittest` runner. Skips, deferrals,
  and out-of-scope markers are read from `test/cpython/MANIFEST.txt`,
  the single source of truth for the triage status above.

## Workflow

1. Pick the smallest `ready` test for the active gopy version.
2. Copy the test file from `~/github/python/cpython/Lib/test/` into
   `test/cpython/`.
3. Run `go test ./test/regrtest/... -run TheTest`.
4. If green, mark the manifest entry `done`, commit, move on.
5. If red, fix the underlying gopy package; never edit the test file.
6. If the test depends on a stdlib module that has not shipped, mark
   `deferred` in the manifest with a one-line reason and skip.

The work is intentionally repetitive; each landed test is one extra
gate against future regressions.

## Out of scope for this spec

- Performance benchmarks. CPython's `Tools/scripts/perf*` and
  `pyperformance` integration are tracked separately.
- The stdlib gap (json / pickle / re / hashlib / etc.). The triage
  above marks every test depending on a missing stdlib module as
  `deferred`. Closing that gap is a multi-release stdlib effort
  tracked under specs in the 1700-1799 series.
- The test runner's own internal tests (`test_regrtest.py`,
  `test_support.py`). gopy has its own port of the harness; these
  files cover the CPython implementation we did not port.
