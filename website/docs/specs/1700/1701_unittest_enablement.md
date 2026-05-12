---
format: md
id: 1701_unittest_enablement
title: "1701. gopy unittest enablement"
sidebar_label: "1701. unittest_enablement"
sidebar_position: 1701
slug: /specs/1701-unittest_enablement
description: "Make `import unittest` work in the gopy interpreter so the test/regrtest harness can run vendored CPython tests. Gates every panel task in spec 1700."
---


## Checklist

Status legend: `done` shipped and verified; `partial` landed with
known follow-up; `pending` not started.

### Top-level steps

| Step | Status | Description |
|------|--------|-------------|
| A | done | stdlibinit assembly (port of `Modules/config.c.in`). |
| C | done | Path-based imports off `sys.path`. |
| C+ | done | Submodule resolution via parent package's `__path__`. |
| D scaffold | done | `stdlib/` tree at module root with byte-equal `unittest/` mirror plus `MANIFEST.txt`. `cmd/gopy` resolves the directory at runtime via `$GOPY_STDLIB` or walk-up search. |
| D verification | done | `gopy -c 'import unittest; print(unittest.TestCase)'` prints `<class 'TestCase'>`. All four compile-front blockers cleared; Step B continues iteratively but the gate itself is green. |
| B | partial | `_functools`, `_io`, `errno`, `time`, `functools`, `io`, `dataclasses`, `fnmatch`, `types`, `contextvars`, `GenericAlias`/`UnionType` shipped. `_collections` and `_signal` still pending. Per-module table below; full subsystem detail in spec 1702. |
| E | pending | Vendor `Lib/test/support/` under `test/cpython/test/support/`. `from test.support import import_helper` still raises `ModuleNotFoundError`. |
| Final gate | done | `gopy -c 'import unittest; unittest.main(module=None, exit=False)'` runs without raising. With no tests in `__main__` it reports `NO TESTS RAN`; a script that builds a suite via `TestLoader.loadTestsFromTestCase` runs pass/fail tests end-to-end through `TextTestRunner`. |
| Regrtest smoke | partial | `TestRunnerRunPasses` in `test/regrtest/runner_test.go` writes a `test_smoke.py` and asserts `OutcomePass` through `Runner.Run`. The current fixture uses `print("ok")`; a follow-up should swap in a `unittest.TestCase`-based fixture so the pin exercises the loader path too. |

### Step D verification blockers

| Blocker | Status | Detail |
|---------|--------|--------|
| `import sys` from Python land | done | `sys/module.go` registers `sys` in the inittab; `stdlibinit` blank-imports it. |
| Builtins inherited by imported modules | done | `vm.EvalCode` reads `globals['__builtins__']`; `vm/eval_import.go` stamps it on imported module dicts before exec. |
| Closure / decorator codegen | done | Stack-underflow at STORE_FAST was three layered bugs: liftCode dropped Argcount, decorator codegen used CALL 1 instead of CALL 0 (3.14 self_or_null promotion), and MAKE_CELL never moved the parameter value into the cell. Fixed in PR #21 commit 2fdcbd2. `callPyFunction` also gained `*args` / `**kwargs` / kw-only support. |
| Chained-comparison compile bug | done | Two layered fixes: `compile/codegen_expr_op.go` `visitCompare` was missing the `COPY 1` + `TO_BOOL` pair before each rung's `POP_JUMP_IF_FALSE` (3.14 codegen.c:3552 `codegen_compare`); independently, `compile/flowgraph.go` `ApplyLabelMap` predicate failed to resolve `JUMP_NO_INTERRUPT` opargs because the pseudo opcode is absent from `opcodeFlags`. Both fixed. |
| Loader-error propagation | done | `imp.ImportModuleLevel` was treating any wrapped `ErrModuleNotFound` from `PathFinder.FindModule` as a finder miss and discarding it, folding transitive failures into a misleading "No module named 'unittest'". Introduced `errFinderMiss` (still wraps `ErrModuleNotFound` for external callers) and match it specifically in the import driver. |
| Cell-binding subsystem | done | Two layered fixes ported as one subsystem. `compile/codegen_stmt_funclike.go:emitMakeCellAndCopyFree` used to emit `MAKE_CELL` only for cell-bound parameters; ported the full CPython contract (`Python/flowgraph.c:3792 insert_prefix_instructions` cellvars block) so every cell var gets a `MAKE_CELL` at the prologue. That uncovered a latent operand bug in `compile/codegen_expr_name.go:emitDeref` where FREE-var `LOAD`/`STORE`/`DELETE_DEREF` emitted the raw FreeVars pool index instead of the deref-space offset `len(CellVars) + freeIdx`; ported the offset rewrite that `Python/flowgraph.c:3844 fix_cell_offsets` applies. Nested closures with non-parameter cell vars (the genexp inside `unittest.loader.shouldIncludeMethod` capturing `fullName`) now resolve correctly. |
| BaseException getset | done | `errors/exception_attrs.go` registers `args`, `__traceback__`, `__context__`, `__cause__`, `__suppress_context__` as data descriptors on `PyExc_BaseException` and wires `GenericGetAttr`/`GenericSetAttr` on the type. `unittest.result._clean_tracebacks` reads/writes these attributes; without them `addFailure` raised `AttributeError` and the failing-test path crashed. CPython: `Objects/exceptions.c:508 BaseException_getset`. |
| Exception str / repr | done | `excStr` / `excRepr` on `newExcType` port `BaseException_str` and `BaseException_repr` (`Objects/exceptions.c:171, 193`) so failing-test output shows `AssertionError: 1 != 2` instead of `<AssertionError object at 0x...>`. |
| Generator goroutine thread | done | `vm/eval_gen.go` calls `setActiveThread` inside the generator goroutine so `sys.exc_info()` works from within `@contextmanager` bodies (the goid-to-thread map used to miss the generator goroutine). |
| Handled-exception slot | done | `state.Thread.handled` (separate from `exc`) backs `sys.exc_info()` and the `PUSH_EXC_INFO` / `POP_EXCEPT` pair. The original combined-slot scheme tripped on the `with` codegen pattern `COPY 3 / POP_EXCEPT / RERAISE 1`. |

### Step B per-module status

`import unittest` now succeeds; the remaining work is opportunistic
(modules the test corpus reaches for that are not yet ported). Full
subsystem-by-subsystem detail and a comprehensive table covering
wave 2 / 3 / infrastructure modules lives in spec 1702.

| Module | Status | Notes |
|--------|--------|-------|
| `_collections` | pending | Still raises `ModuleNotFoundError`. Pure-Python `collections` falls through to it for `OrderedDict`, `deque`. |
| `_functools` | done | `cmp_to_key`, `reduce`, `partial`, `_lru_cache_wrapper` ported. |
| `_io` | done | `StringIO`, `BytesIO`, text/binary wrapper surface ported. `BlockingIOError` and the `BaseException` / `OSError` family wired as Python-level builtins. |
| `errno` | done | Full port from `Modules/errnomodule.c`; see spec 1702 § errno. |
| `time` | done | Module surface registered; resolves through `pytime/`. |
| `_signal` | pending | Deferred; unittest imports it lazily and runs fine without it. |
| `functools` (pure) | done | Stdlib import resolves through `stdlib/functools.py`. |
| `io` (pure) | done | Stdlib import resolves through `stdlib/io.py`. |
| `dataclasses` | done | Field discovery via `__annotations__` shipped. |
| `fnmatch` | done | Full port. |
| `types` | done | Full port. |
| `contextvars` | done | Tested. |
| `contextlib` | in progress | `@contextmanager` works end-to-end; helper-as-MethodDescr unresolved. |
| `traceback` | pending | Empty frames currently render in failing-test output; full subsystem port queued. |
| `_colorize`, `re`, `os`, `argparse`, `warnings`, `weakref`, `pprint`, `difflib`, `collections` (pure) | pending | Not yet on the unittest critical path. |

## Goal

Make `gopy -c 'import unittest'` succeed and let `unittest.main()`
discover and run a vendored `Lib/test/test_*.py` file end to end.
Once that works, the panel tasks (#474-#487 in spec 1700) unblock
in version order: pick the smallest "ready" entry, copy it into
`test/cpython/`, run it under the gopy harness, and pin it green
(or fix the underlying gopy package on red).

## Strategy: port by subsystem, not by bug

Each new gate failure tends to surface one missing CPython
subsystem, not a one-line bug. Three confirmations from this branch:

1. **Exception unwind.** PUSH_EXC_INFO crashed because
   `handleException` was a stub. The fix wasn't one line; it was
   porting the full set: `label_exception_targets` (flowgraph.c:885),
   `exception_unwind` (ceval.c), the PUSH_EXC_INFO dispatch arm
   (bytecodes.c). Patching only `handleException` would have left
   the depth / lasti accounting broken on the next try/except.
2. **Generator prefix.** The first `async def` blew up because the
   compiler never emitted RETURN_GENERATOR + POP_TOP. The fix was
   to port `insert_prefix_instructions` (flowgraph.c:3760) plus
   the matching `RETURN_GENERATOR` dispatch arm. Each piece on its
   own (only the prefix, or only the dispatch) would have left the
   other tripping the next test.
3. **Cell binding.** The current LOAD_DEREF blocker on
   `shouldIncludeMethod` is the same shape: `emitMakeCellAndCopyFree`
   in `codegen_stmt_funclike.go` only emits MAKE_CELL for cell-bound
   parameters, but CPython's `insert_prefix_instructions`
   (flowgraph.c:3792) emits MAKE_CELL for *every* cell var.
   `build_cellfixedoffsets` (flowgraph.c:3711) + `fix_cell_offsets`
   (flowgraph.c:3844) rewrite the operands together. Porting only
   the parameter case left non-parameter cells (like `fullName`,
   captured by a nested generator expression) unallocated.

The pattern: a subsystem spans `Python/flowgraph.c` + `Python/ceval.c`
+ `Python/bytecodes.c`. Bug-by-bug patching re-enters the same
files repeatedly and misses the structural invariants.

**The process going forward:**

1. When a gate fails, identify the CPython subsystem the failure
   belongs to (exception, generator, cell binding, frame setup,
   import machinery, a built-in or pure-Python stdlib module, etc.).
2. Enumerate every function in that subsystem across all CPython
   source files. For VM/compile subsystems this means `Python/*.c`;
   for a stdlib module it means the entire `Lib/<name>.py` or
   `Modules/<name>module.c` (plus any `_<name>` C accelerator) -
   not just the slice unittest reaches for. Use `grep -n` and record
   file:line citations.
3. Audit gopy for each citation. List what's ported, what's stub,
   what's missing. Resist the urge to patch only the function that
   triggered the crash.
4. Port the **whole subsystem in one pass** with `// CPython:`
   citations on every function. Then run the existing test suite
   and re-test the gate that originally failed. Subsystem ports
   include the full public API of the module, not just the names
   unittest happens to call - so the next consumer of the module
   (regrtest, the panel tests, another stdlib import chain) does
   not surface another partial-port gap.
5. Only after the subsystem is whole, move on to the next gate.

**No partial stubs.** A stubbed `functools.partial` that satisfies
import-time use but returns wrong results under reduce/lru_cache
trades one gate failure today for a confusing test failure later.
Either port the full CPython function with citation, or omit the
name entirely so the import fails loudly. Names like `wraps`,
`partial`, `reduce`, `lru_cache` that currently exist as no-op
identity decorators must be replaced by real ports under this
rule.

Bug-by-bug fixes are evidence that a subsystem audit was skipped.
Take the audit hit up front; the gate moves forward in larger
strides and the citations make later rebases cheap.

This work landed nowhere across v0.0-v0.12: the import lookup walks
inittab and the frozen table, but the inittab is empty at runtime
because no central registration package blank-imports the gopy module
packages. The importlib bootstrap is also stubbed (`ErrBootstrapNotReady`)
so file-based imports off `sys.path` do not work yet. Both gaps have
to close before any `Lib/test/test_*.py` module can run.

## Sources of truth

- CPython 3.14 source tree at `/Users/apple/github/python/cpython/`. Reference
  paths in this spec use that root.
- `gopy/imp/` is the existing port of CPython's `Python/import.c`.
  `imp.AppendInittab` (`Python/import.c:2243 PyImport_AppendInittab`)
  and `imp.ImportModule` (`Python/import.c:1450 PyImport_ImportModule`)
  are already in place.
- `gopy/imp/bootstrap.go` ports `Python/pylifecycle.c:987 init_importlib`
  but returns `ErrBootstrapNotReady` because the frozen importlib code
  objects are not embedded.

## Why it is broken today

Two independent root causes:

1. **No central inittab assembly.** CPython's `Modules/config.c.in:26
   _PyImport_Inittab[]` is a static array that the linker materializes
   at startup. Each `PyInit_*` symbol comes from a separate `.c` file;
   `Modules/config.c` is generated by `Modules/makesetup` and gives
   the import machinery its complete picture of compiled-in modules.

   gopy uses Go `init()` blocks that call `imp.AppendInittab(name, fn)`
   (see `gc/module.go:19` and `contextvar/module.go:26`). Those run
   only when their package is imported. `cmd/gopy/main.go` does not
   blank-import them, so at startup the inittab is empty. `import gc`
   raises `ModuleNotFoundError` even though the Go code that registers
   `gc` exists.

   **Fix:** add a dedicated `stdlibinit/` package whose only job is
   to blank-import every module package and force its `init()` to run.
   `cmd/gopy/main.go` blank-imports `stdlibinit`. This is the gopy
   equivalent of `Modules/config.c.in`.

2. **No file-based imports.** `imp.ImportModule` (`imp/import.go:42`)
   walks: sys.modules, frozen, inittab, error. There is no path-based
   finder; nothing ever opens a `.py` file. CPython delegates that to
   `_frozen_importlib_external` whose code object lives in
   `Python/Lib/importlib/_bootstrap_external.py` and is frozen at build
   time into `Python/frozen_modules/importlib._bootstrap_external.h`.

   gopy/imp/bootstrap.go has the call structure (`InitImportlib`,
   `InitImportlibExternal`) but bails with `ErrBootstrapNotReady`
   because the frozen code objects do not exist. Until that lands,
   no `.py` file gets executed off disk; only inittab/frozen entries
   work.

   **Fix:** implement a Go-native path finder and loader (port of
   `_bootstrap_external.FileFinder`, `SourceFileLoader`,
   `_path_importer_cache`) and wire it as the default tail of the
   import lookup chain. The frozen-bytecode embedding is a separate,
   later step; the path-based loader is what unittest needs.

## Plan

Five steps. Each lands as one or more commits with its own gate, so
the PR stays green at every checkpoint.

**Order in practice:** A → C → D → B (iterative) → E. Step B
("port missing built-ins") is most productive once C and D let us
actually run `import unittest` and watch which built-in fails first.
Doing B speculatively before C/D risks porting modules unittest
never reaches. The numbering below reflects the conceptual buckets,
not the execution order.

### Step A. stdlibinit assembly

Port `Modules/config.c.in`.

- New package `stdlibinit/` at the module root.
- File `stdlibinit/registry.go` does nothing but blank-import every
  gopy module package whose `init()` calls `imp.AppendInittab`.
  Initial set (everything currently registered):
  - `_ "github.com/tamnd/gopy/gc"` (`gc/module.go:19`)
  - `_ "github.com/tamnd/gopy/contextvar"` (`contextvar/module.go:26`)
- Each new built-in module added in later steps appends a line here.
- `cmd/gopy/main.go` adds `_ "github.com/tamnd/gopy/stdlibinit"` to
  its import list. That is the only call site that needs to change;
  everything else triggers automatically.
- Test: `stdlibinit/registry_test.go` calls `imp.InittabSnapshot()`
  and asserts `gc` and `_contextvars` are present.
- Gate: `go run ./cmd/gopy -c 'import gc; print(gc.isenabled())'`
  prints `True`.

CPython references:
- `Modules/config.c.in:26 _PyImport_Inittab[]`: the static array.
- `Modules/makesetup:1 makesetup`: the script that generates
  `Modules/config.c` from `Modules/Setup` files.
- `Python/import.c:2403 _PyImport_FindBuiltin`: the lookup that
  walks `INITTAB`.

### Step B. Built-in modules unittest depends on (audit and fill)

Walk `Lib/unittest/__init__.py` and the modules it imports
transitively. Bucket each into:

- **Already shipped in gopy** (e.g. `builtins`, `sys` partial,
  `warnings` partial, `gc`).
- **Built-in module needed, not yet wired** (e.g. `_collections`,
  `_functools`, `_io`, `_thread`, `_signal`, `errno`, `time`,
  `_socket` if any).
- **Pure-Python module needed** (e.g. `os`, `os.path`, `re`, `io`,
  `traceback`, `copy`). These come for free once Step C lands and
  the stdlib tree is on `sys.path`.

For each "built-in module needed, not yet wired", port from CPython
following the `gc/module.go` pattern: a Go package that mirrors the
CPython C module, registers via `imp.AppendInittab`, and gets blank-
imported from `stdlibinit/`. Citations are mandatory; pure-Go shims
that only return placeholder values are not acceptable.

Estimated touch list (tighten this as Step B starts):
- `_collections` -> port of `Modules/_collectionsmodule.c`. Provides
  `deque`, `OrderedDict` C accelerators.
- `_functools` -> `Modules/_functoolsmodule.c` (`reduce`, `partial`,
  `_lru_cache_wrapper`).
- `_io` -> `Modules/_io/` (text and binary stream classes; `open`
  in builtins ultimately delegates here).
- `errno` -> `Modules/errnomodule.c` (constant table).
- `time` -> `Modules/timemodule.c` (already partly shipped under
  `pytime/`; the user-facing module surface is the gap).
- `_signal` -> `Modules/signalmodule.c` (likely deferred for
  v0.12.1; unittest imports it lazily).

If any module turns out to need stdlib state we have not built (e.g.
sqlite3, network), mark the dependent unittest path `deferred` in
the manifest with a note and skip it.

### Step C. Path-based imports off sys.path (DONE)

Port the FileFinder / SourceFileLoader chain so `.py` files on
disk become importable.

Status: shipped as `imp/pathfinder.go` + the splice in `imp/import.go`
(step 4 of `ImportModuleLevel`). `cmd/gopy/main.go` installs the
finder before each entry (`-c`, script, REPL); the directory of the
script (or `""` for `-c`/REPL) is the first entry, `PYTHONPATH` is
appended. Gate verified: a hand-written `foo.py` containing
`value = 42` resolves through `gopy -c 'import foo; print(foo.value)'`
when run from its parent directory. `imp/pathfinder_test.go` pins
the top-level / package / not-found / SetPathFinder paths.

CPython references:
- `Lib/importlib/_bootstrap_external.py:1531 FileFinder`: the
  finder that walks one directory entry on `sys.path`.
- `Lib/importlib/_bootstrap_external.py:1080 SourceFileLoader`
 : loads a `.py` file, compiles it, executes it as a module.
- `Lib/importlib/_bootstrap_external.py:1190 SourcelessFileLoader`
 : loads a `.pyc` file. Out of scope for v0.12.1; sources only.
- `Lib/importlib/_bootstrap.py:1184 _bootstrap._find_and_load`: the
  top-level lookup that consults `sys.meta_path` and then the path
  finders.

gopy approach:
- New file `imp/pathfinder.go`. Defines `PathFinder` mirroring the
  meta-path entry, and `FileFinder` mirroring the per-directory
  entry. Both are Go structs implementing the same surface their
  Python counterparts publish (`find_spec`, `_fill_cache`).
- `imp/import.go` adds a tail step: after inittab lookup misses,
  walk `sys.path` via `PathFinder` and execute the source file
  through `pythonrun.RunSource` (the existing `RunFile` is wrong
  shape; we need to compile in module-scope).
- `sys.path` gets a default first entry pointing at the vendored
  stdlib (Step D) and any user-provided `-c <dir>` argument.
- Test: `imp/pathfinder_test.go`. Build a tmp dir with a `foo.py`
  that defines `value = 42`, point `sys.path` at it, call
  `imp.ImportModule(exec, "foo")`, assert the module's dict has
  `value == 42`.
- Gate: `gopy -c 'import sys; sys.path.insert(0, "."); import foo'`
  works against a hand-written `foo.py`.

### Step D. Vendor the stdlib slice unittest needs

Set up the gopy stdlib mirror: a tree under
`stdlib/` (top-level) holding byte-equal copies of the CPython
`Lib/` files we ship. The directory is added to the default
`sys.path` so Step C's path finder picks it up.

Layout:

```
stdlib/
  __init__.py            (empty; marks the root)
  unittest/              copied from cpython/Lib/unittest/
  os.py                  (or os/ if a package; copied verbatim)
  posixpath.py
  ntpath.py
  io.py
  re/                    (re is deferred per spec 1700; included
                         only if a unittest path needs it)
  ...
```

Rules (mirror spec 1700's vendor contract):
- Files are byte-identical to CPython 3.14 at the tag we target.
  No local edits. If a file needs a tweak, it goes into a sibling
  override mechanism, not the vendored copy.
- The vendored tree is pulled from `~/github/python/cpython/Lib/`.
- `stdlib/MANIFEST.txt` records each vendored file with its
  upstream path and SHA-256, so a CPython rebase shows zero diff
  noise.

Gate: `gopy -c 'import unittest; print(unittest.TestCase)'` prints
the class object.

### Step E. test.support helpers

`Lib/test/support/__init__.py` and friends are imported by every
`Lib/test/test_*.py`. Vendor them under
`test/cpython/test/support/` so the import path resolves to
`test.support` (the existing `test/cpython/` mirror is the package
root once it has an `__init__.py`).

CPython references:
- `Lib/test/support/__init__.py:1`: the support module surface.
- `Lib/test/support/import_helper.py`: used by test_grammar.py
  among many.
- `Lib/test/support/warnings_helper.py`: used by test_grammar.py.

Add `test/cpython/__init__.py` and `test/cpython/test/__init__.py`
(empty files) so `import test.support` resolves through the
existing corpus directory. The harness's runner already sets the
working directory to `test/cpython/`; sys.path picks it up.

Gate: `gopy -c 'from test.support import import_helper; print(import_helper)'`
runs.

## Verification

After all five steps:

1. `go test ./test/regrtest/...` stays green.
2. New gate: `gopy -c 'import unittest; unittest.main(module=None)'`
   does not raise on import.
3. New regrtest gate test: `TestRunSmokeTest` writes a tiny
   `test_smoke.py` that uses `unittest.TestCase` and asserts a
   trivial truth, runs it through `Runner.Run`, expects
   `OutcomePass`. Pins the harness end-to-end.
4. The first panel task (smallest of #474-#487) starts running
   real CPython test files. The unblocking order is opportunistic;
   probably v0.4.0 number/string panel (no special imports) or
   v0.5.5 lexer panel.

## Out of scope

- Frozen-bytecode embedding for `_frozen_importlib`. Step C's
  Go-native path finder is enough for unittest; a full freeze of
  the importlib bootstrap is a separate effort.
- `.pyc` cache files. Source loading only.
- C-extension modules under `Lib/` that ship as `.so`. Pure-Python
  fallbacks (the `_py*.py` siblings) are used where CPython has
  them; otherwise the dependent test goes deferred.
- `setUpModule` / `tearDownModule` async paths. `unittest.async_case`
  is loaded lazily and its dependencies (asyncio) stay deferred.
- `unittest.mock`. 3204 lines, drags in `inspect`, `pickle`. Mark
  any test that imports `unittest.mock` deferred until a follow-up
  spec.

## Tasks

The work splits into one task per step, blocking the unittest
umbrella task #489 and through it the panel tasks #474-#487. Step
order is strict: A blocks B, B blocks C, C blocks D, D blocks E.
