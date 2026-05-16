---
format: md
id: 1707_cpython_3145_sync
title: "1707. CPython 3.14.5 sync"
sidebar_label: "1707. cpython_3145_sync"
sidebar_position: 1707
slug: /specs/1707-cpython_3145_sync
description: "Refresh upstream against the v3.14.5 release tag (2026-05-10). Catalogue every file that changed between v3.14.4 and v3.14.5 that intersects an existing gopy port, the user-visible bug it fixes upstream, and the work needed in gopy to keep parity. Each row is a separate follow-up task."
---

## Why this spec exists

Gopy's "behavioural compatibility with CPython 3.14" claim is only
as fresh as the upstream tree we last referenced. Two CPython
clones live on this machine:

- `$HOME/cpython-314` is the canonical read-only mirror used by the
  port specs (every `// CPython:` citation resolves against this
  tree).
- `$HOME/github/python/cpython` is the working clone used to
  cherry-pick patches and reproduce upstream tests.

Both were behind the v3.14.5 release tag (cut 2026-05-10). The
brew formula `python@3.14` was also pinned at 3.14.4. On 2026-05-14
all three were bumped to v3.14.5:

| Target | Before | After |
|--------|--------|-------|
| `/opt/homebrew/Cellar/python@3.14` | `3.14.4_1` | `3.14.5` |
| `$HOME/cpython-314` HEAD | `c235654cba2` (2026-04-20) | `5607950ef23` (v3.14.5) |
| `$HOME/github/python/cpython` HEAD | `ab2d84fe102` (2026-04-29) | `5607950ef23` (v3.14.5) |

The v3.14.4 → v3.14.5 window covers 56 first-parent commits and
touches 257 files. The Lib/, Modules/, Objects/, Python/ subtrees
account for 89 of those; the rest are docs, CI, installer, and
test files. This spec catalogues the 89 that gopy cares about and
breaks them down by whether gopy has a port that needs a refresh.

## Rule

Every CPython upstream change that affects a file gopy already
ships a port for (whether a Go translation under `module/` or a
byte-equal vendor under `stdlib/`) is treated as a refresh task on
this spec. We do not skip patches because they are "small" or
"cosmetic". A two-line upstream fix that changes user-visible
behaviour is still a divergence if we don't apply it.

A row gets ticked off when:

1. The gopy port has the upstream fix mirrored (Go code or
   re-vendored `.py`).
2. The accompanying CPython test (or a gopy equivalent) runs
   green under `gopy`.

## Sync workflow

```
# refresh both clones to the next 3.14.x tag
git -C $HOME/cpython-314 fetch --tags origin 3.14
git -C $HOME/cpython-314 checkout v3.14.<n>
git -C $HOME/github/python/cpython fetch --tags origin 3.14
git -C $HOME/github/python/cpython checkout v3.14.<n>

# brew (optional, only matters when running upstream's own tests)
brew upgrade python@3.14

# audit
git -C $HOME/github/python/cpython diff --stat v3.14.<n-1>..v3.14.<n> -- \
  'Lib/' 'Modules/' 'Objects/' 'Python/' 'Include/'

# for each changed file, decide: gopy has a port -> add a row below.
```

## Files in scope

The 89 runtime files that changed between v3.14.4 and v3.14.5 fall
into four buckets. Each table below lists the upstream file, the
gopy port (if any), the upstream bug + summary, and the work
required in gopy.

### A. Python stdlib refreshes (`stdlib/` vendor)

Gopy vendors most of these byte-equal. A refresh means
re-copying the upstream `.py` into `stdlib/` and re-running the
relevant gate test.

| # | Upstream file | gopy target | Upstream change | Status |
|---|---------------|-------------|----------------|--------|
| A.1 | `Lib/argparse.py` | `stdlib/argparse.py` | GH-130750: `_check_value` quotes each choice via `repr(str(choice))` instead of the bare comma-joined form. Error message went from `invalid choice: 'x' (choose from a, b)` to `invalid choice: 'x' (choose from 'a', 'b')`. | pending |
| A.2 | `Lib/annotationlib.py` | `stdlib/annotationlib.py` | gh-141388: handle non-function callables as annotate functions. Adds `__wrapped__` traversal and `_check_no_unevaluated_strings` accommodations for `partial`, `lambda`, etc. (36 lines). Sister to spec 1706. | pending |
| A.3 | `Lib/configparser.py` | `stdlib/configparser.py` | 17 lines of edge-case fixes. | pending |
| A.4 | `Lib/dataclasses.py` | `stdlib/dataclasses.py` *(or port at `module/dataclasses/`)* | 26 lines of bug fixes. Note: gopy ports dataclasses as Go (`module/dataclasses/`), not vendored. Audit + apply behaviour parity. | pending |
| A.5 | `Lib/inspect.py` | not vendored (audit only) | Docstring change removes `im_*` reference (gh-149096). No behavioural impact. | n/a |
| A.6 | `Lib/pickle.py` | not vendored (no gopy port) | gh-148914: fix PickleBuffer memoization of in-band buffers. Tracked under future pickle port. | n/a |
| A.7 | `Lib/random.py` | `stdlib/random.py` | gh-149221: sync `random.py` with main; gh-149276: fix `binomialvariate` for the `n=1` edge case. Re-vendor + add the new tests as a gopy gate. | pending |
| A.8 | `Lib/runpy.py` | not vendored | gh-149117: set `ImportError.name` from `runpy.run_module`/`run_path`. Tracked under future runpy port. | n/a |
| A.9 | `Lib/shutil.py` | `stdlib/shutil.py` | 24 lines. Re-vendor. | pending |
| A.10 | `Lib/typing.py` | not vendored (no gopy port) | gh-149221-adjacent: `get_type_hints` now detects `__wrapped__` loops and raises `ValueError`. Tracked under future typing port. | n/a |
| A.11 | `Lib/urllib/robotparser.py` | `stdlib/urllib/robotparser.py` *(if vendored)* | gh-138908: RFC 9309 support. 209 lines of new behaviour: wildcard handling, group precedence, longest-match. If gopy ships robotparser, re-vendor. | pending |
| A.12 | `Lib/uuid.py` | `stdlib/uuid.py` | 10 lines. Re-vendor. | pending |
| A.13 | `Lib/zipfile/__init__.py` | `stdlib/zipfile/__init__.py` | 21 lines. Re-vendor. | pending |
| A.14 | `Lib/timeit.py` | `stdlib/timeit.py` *(if vendored)* | 2 lines. | pending |
| A.15 | `Lib/webbrowser.py` | not vendored | 5 lines. | n/a |
| A.16 | `Lib/socket.py` | `stdlib/socket.py` *(if vendored)* | 28 lines. | pending |
| A.17 | `Lib/http/client.py`, `Lib/http/cookies.py` | `stdlib/http/` | 11 + 8 lines respectively. | pending |
| A.18 | `Lib/email/_header_value_parser.py`, `Lib/email/generator.py`, `Lib/email/quoprimime.py` | `stdlib/email/` | 15 + 2 + small. bpo-39100 `_header_value_parser` no longer treats `Group` as invalid mailbox. | pending |
| A.19 | `Lib/smtplib.py` | not vendored | 2 lines. | n/a |
| A.20 | `Lib/pdb.py` | not vendored | gh-148615: handle `--` separator in argument parsing. | n/a |
| A.21 | `Lib/json/tool.py` | not vendored | 3 lines. | n/a |
| A.22 | `Lib/multiprocessing/*` (`connection.py`, `popen_fork.py`, `resource_tracker.py`) | not vendored | small fixes. | n/a |
| A.23 | `Lib/asyncio/__main__.py`, `Lib/asyncio/windows_utils.py` | not vendored | gh-149388: `PipeHandle.close` idempotent. | n/a |
| A.24 | `Lib/ensurepip/__init__.py` + bundled wheel | not vendored | gh-149148: bump bundled `pip` from 26.0.1 to 26.1.1. | n/a |

### B. C modules (`module/` Go port)

| # | Upstream file | gopy target | Upstream change | Status |
|---|---------------|-------------|----------------|--------|
| B.1 | `Modules/_csv.c` | `module/_csv/` | 6 lines. Audit for behaviour change. | pending |
| B.2 | `Modules/_struct.c` | `module/_struct/` | gh-148675: add `Zd`, `Zf`, `Zg`, `Ze` complex format codes (27 lines). Add to gopy's format table. Linked to `arraymodule.c` and `memoryobject.c`. | pending |
| B.3 | `Modules/arraymodule.c` | gopy `array` module (none yet) | gh-148675: `Zd`/`Zf` complex formats (10 lines). | pending |
| B.4 | `Modules/binascii.c` | gopy `binascii` (none) | gh-148093: `a2b_uu` raises `binascii.Error` on empty input (8 lines). | n/a |
| B.5 | `Modules/gcmodule.c` | `module/gc/` | 30 lines. Knock-on from `Python/gc.c` generational restoration: new `gc.get_stats` fields, `heap_size` in stats. Sync the gc module's getset table. | pending |
| B.6 | `Modules/zlibmodule.c` | `module/zlib/` | 1 line cleanup. | pending |
| B.7 | `Modules/_elementtree.c` | gopy XML (none) | 45 lines. | n/a |
| B.8 | `Modules/_asynciomodule.c` | gopy `asyncio` (none) | 6 lines. | n/a |
| B.9 | `Modules/_bz2module.c`, `Modules/_lzmamodule.c`, `Modules/_zstd/*` | gopy (none) | compression cleanups. | n/a |
| B.10 | `Modules/_ctypes/_ctypes.c`, `Modules/_ctypes/cfield.c` | gopy `ctypes` (none) | gh-148675 (Zd/Zf) + gh-148967 (C complex FFI fix). | n/a |
| B.11 | `Modules/_remote_debugging_module.c` | none | 3.14 debugger surface; not in gopy scope. | n/a |
| B.12 | `Modules/overlapped.c` | none | Windows IO. | n/a |
| B.13 | `Modules/expat/*` | gopy (none, uses Go's encoding/xml) | gh-149698: bundled expat bumped to 2.8.1. | n/a |

### C. Object protocol (`objects/` Go port)

| # | Upstream file | gopy target | Upstream change | Status |
|---|---------------|-------------|----------------|--------|
| C.1 | `Objects/codeobject.c` | `objects/code.go` and friends | 24 lines. Code-object internal cleanups; audit `co_*` field churn. | pending |
| C.2 | `Objects/dictobject.c` | `objects/dict.go` | 4 lines. Free-threading lock cleanup. | pending |
| C.3 | `Objects/genericaliasobject.c` | `objects/genericalias.go` | 3 lines. | pending |
| C.4 | `Objects/memoryobject.c` | `objects/memoryview.go` | gh-148675: `Zd`/`Zf` complex formats; gh-148390: fix UB in `memoryview(...).cast("?")`. 8 lines. | pending |
| C.5 | `Objects/object.c` | `objects/object.go` and `ports/` | 8 lines. Audit per the 1704 full-port rule. | pending |
| C.6 | `Objects/typevarobject.c` | `objects/typevar.go` | 9 lines. | pending |
| C.7 | `Objects/unicodeobject.c` | `objects/str.go` and friends | 56 lines. Three independent fixes: (a) gh-113956: thread-safe `intern_common` in free-threaded build; (b) gh-147856: `bytes.replace()` accepts `count` as a keyword; (c) GH-145668: `FOR_ITER` specialization for virtual iterators (also touches Python/bytecodes.c). Only (b) is observable from gopy. | pending |

### D. VM / compile (`vm/`, `compile/`, `module/builtins/`)

| # | Upstream file | gopy target | Upstream change | Status |
|---|---------------|-------------|----------------|--------|
| D.1 | `Python/bltinmodule.c` | `module/builtins/` | 10 lines. Audit for behaviour change. | pending |
| D.2 | `Python/bytecodes.c` | `vm/` opcodes | 20 lines. Two fixes: (a) gh-137030: `YIELD_VALUE` `assert` no longer crashes on `instr_ptr` reads when the executable is detached; (b) gh-137814: `MAKE_FUNCTION_ANNOTATE` rewrites the qualname of the `__annotate__` function to `<parent>.__annotate__`. The annotate-qualname fix is on the gopy hot path because spec 1706 just landed; verify our MAKE_FUNCTION_ANNOTATE op sets `func_qualname` the same way. | pending |
| D.3 | `Python/codegen.c` | `compile/codegen_*.go` | 9 lines. Two fixes: (a) gh-149122: refleak in codegen; (b) `maybe_optimize_function_call` now bails out when the generator expression is a coroutine (otherwise the genexp-to-fast-call fold would be wrong). The coroutine guard is observable: a `[x async for x in ...]` inside `list(...)` no longer mis-compiles. Re-check `compile/codegen_call_optim.go`. | pending |
| D.4 | `Python/compile.c` | `compile/` driver | 14 lines. | pending |
| D.5 | `Python/flowgraph.c` | `compile/flowgraph_*.go` | 146 lines. Cleanups and one bug fix in `inline_small_or_no_lineno_blocks`. Audit gopy's flowgraph. | pending |
| D.6 | `Python/gc.c` | `module/gc/` + Go runtime | gh-148726: **forward-port the generational GC** to 3.14 (1076 lines, 330 ins / 746 del, mostly a refactor that re-introduces young/middle/old generations after 3.13's "incremental GC" experiment). gopy's GC is Go's runtime, not a Python-side mark/sweep, so most of this is informational; however, `gc.get_stats()`, `gc.get_count()`, `gc.collect(generation=...)`, `gc.set_threshold`, `gc.get_threshold`, and `gc.freeze`/`gc.unfreeze` are observable from Python and need consistent return shapes. Audit `module/gc/`. | pending |
| D.7 | `Python/instrumentation.c` | `vm/instrumentation.go` *(if ported)* | 41 lines. Tracking gh-148178 (validate remote debug offset tables). Mostly debugger surface. | pending |
| D.8 | `Python/legacy_tracing.c` | not ported | 4 lines. | n/a |
| D.9 | `Python/marshal.c` | gopy marshal (none) | gh-148418: reference leak on corrupted `TYPE_CODE` stream (46 lines). Tracked when marshal lands. | n/a |
| D.10 | `Python/optimizer_analysis.c` | gopy tier-2 optimizer | 5 lines. | pending |
| D.11 | `Python/crossinterp.c`, `Python/parking_lot.c`, `Python/lock.c` | not ported | free-threading internals. | n/a |
| D.12 | `Python/executor_cases.c.h`, `Python/generated_cases.c.h` | derived | auto-generated; reflects bytecodes.c changes. | n/a |

## Highlights: patches gopy must land soon

Pulled out separately because the rest of the table is sortable
maintenance work; these are the items that change observable
behaviour for code gopy already runs:

1. **`MAKE_FUNCTION_ANNOTATE` qualname rewrite** (D.2 gh-137814).
   Spec 1706 just landed the deferred-annotation pipeline. The
   3.14.5 fix means `f.__annotate__.__qualname__` is now
   `<parent>.__annotate__` instead of inherited from the function
   slot. Without this fix, `inspect.signature` and
   `typing.get_type_hints` reporting will diverge.

2. **`argparse` choice quoting** (A.1 GH-130750). One-line
   re-vendor; the error message is part of unittest gates.

3. **`Lib/annotationlib.py` non-function callables** (A.2 gh-141388).
   Spec 1706 Phase 8 just shipped this file byte-equal; we need a
   re-vendor against v3.14.5 to keep the annotate-callable handling
   in sync (`partial`, `lambda`, decorator-wrapped callables).

4. **`bytes.replace(count=…)` keyword** (C.7 gh-147856).
   Tests in `test_bytes` will fail without this.

5. **`codegen.c` coroutine genexp guard** (D.3). If gopy ports
   `maybe_optimize_function_call`, the guard prevents a mis-compile
   of `list(x async for x in agen)`.

6. **`gcmodule.c` / `Python/gc.c` generational GC API** (B.5, D.6).
   `gc.get_stats()` schema changed (now includes `heap_size`,
   per-generation counts). gopy's `module/gc/` returns its own
   shape; align with 3.14.5 fields so vendored code (e.g.,
   `Lib/test/test_gc.py`) compiles.

7. **`struct`/`array`/`memoryview` complex format codes**
   (B.2, B.3, C.4 gh-148675). `Zd` (complex128), `Zf`
   (complex64), `Zg`, `Ze` join the existing format chars. gopy's
   `module/_struct/` table currently rejects them.

## Files explicitly out of scope

These changed upstream but gopy does not ship a port, so they
are tracked only for future reference:

- `Lib/pickle.py`, `Lib/typing.py`, `Lib/runpy.py`,
  `Lib/inspect.py`, `Lib/pdb.py`, `Lib/json/tool.py`,
  `Lib/multiprocessing/*`, `Lib/asyncio/*`, `Lib/ensurepip/*`
- `Modules/binascii.c`, `Modules/_elementtree.c`,
  `Modules/_asynciomodule.c`, `Modules/_bz2module.c`,
  `Modules/_lzmamodule.c`, `Modules/_zstd/*`,
  `Modules/_ctypes/*`, `Modules/_remote_debugging_module.c`,
  `Modules/overlapped.c`, `Modules/expat/*`
- `Python/marshal.c`, `Python/crossinterp.c`,
  `Python/parking_lot.c`, `Python/lock.c`,
  `Python/legacy_tracing.c`
- All Windows/Apple/Android installer files
- All `Lib/test/*` files (we maintain our own gate corpus
  under spec 1701)
- `Doc/`, `.github/workflows/`, `iOS/`, `Mac/`,
  `PCbuild/`, `Tools/`

## Gate

This spec is **done** when every row marked `pending` above is
either flipped to `done` (port refreshed + matching test green) or
moved to an explicit out-of-scope follow-up with a tracking issue.
The audit cadence is per-release: every new 3.14.x tag spawns
a fresh `1707_X` follow-up spec (e.g., `1707_3146.md`) with the
v3.14.5 → v3.14.6 delta. The structure above is the template.

## Checklist

- [x] Pull `$HOME/cpython-314` to `v3.14.5` tag.
- [x] Pull `$HOME/github/python/cpython` to `v3.14.5` tag.
- [x] Upgrade `brew python@3.14` from `3.14.4_1` to `3.14.5`.
- [x] Audit `git diff --stat v3.14.4..v3.14.5` for `Lib/`,
      `Modules/`, `Objects/`, `Python/`, `Include/`.
- [x] Bucket each touched file into Stdlib / Modules / Objects /
      VM and record gopy target + upstream change summary.
- [ ] File a tracking task per pending row above (one task per
      row, not one per bucket. Each row maps to a discrete port
      refresh).
- [ ] Land the seven "Highlights" patches before the next release.
- [ ] Flip remaining `pending` rows as their refreshes land.
- [ ] Open `1707_3146.md` when v3.14.6 ships.
