---
format: md
id: 1711_v0124_asyncio_full_port
title: "1711. v0.12.4 asyncio full port"
sidebar_label: "1711. v0.12.4 asyncio"
sidebar_position: 1711
slug: /specs/1711-v0124-asyncio
description: "Port the CPython 3.14 asyncio package (Lib/asyncio/*.py + Modules/_asynciomodule.c) into gopy under stdlib/asyncio + module/_asyncio so import asyncio, asyncio.coroutines._is_coroutine, and asyncio.Runner work end-to-end. Closes the T6 BLOCKED row in spec 1710 and unblocks test_tabnanny.py."
---

## Why this spec exists

Spec 1710's sub-system blocker table parks two rows on **T6** with the
note "port the asyncio package (event loop, transports, protocols,
futures, tasks, streams, subprocess, queues, locks) as its own spec".
That spec is this one.

The immediate need is small: `unittest.mock` does
`import asyncio` at module load and then reaches into
`asyncio.coroutines._is_coroutine`, which is a single sentinel object.
`stdlib/unittest/async_case.py` is heavier; it constructs an
`asyncio.Runner` and runs coroutines. Both consumers sit on the path
to `test_tabnanny.py` (via `unittest.mock`) and to anything that uses
`IsolatedAsyncioTestCase`.

A stub that defines `asyncio.coroutines._is_coroutine` only would
satisfy `unittest.mock`. The standing project rule
("port whole subsystem, never partial slices") rules that out: when a
gate hits a subsystem, every function in that subsystem gets a faithful
port. So this spec covers the full asyncio surface — Python + C —
even though only a sliver is exercised on day one.

## Checklist

Status legend: DONE = landed and verified, WIP = in progress,
TODO = not started, BLOCKED = waiting on a larger sub-system spec.

### Sources to fully port (CPython 3.14)

Python layer (`Lib/asyncio/`):

| File | Lines | gopy destination | Status | Commit |
|------|------:|------------------|--------|--------|
| `__init__.py` | 74 | `stdlib/asyncio/__init__.py` | TODO | — |
| `base_events.py` | 2082 | `stdlib/asyncio/base_events.py` | TODO | — |
| `base_futures.py` | 67 | `stdlib/asyncio/base_futures.py` | TODO | — |
| `base_subprocess.py` | 319 | `stdlib/asyncio/base_subprocess.py` | TODO | — |
| `base_tasks.py` | 94 | `stdlib/asyncio/base_tasks.py` | TODO | — |
| `constants.py` | 41 | `stdlib/asyncio/constants.py` | TODO | — |
| `coroutines.py` | 118 | `stdlib/asyncio/coroutines.py` | TODO | — |
| `events.py` | 878 | `stdlib/asyncio/events.py` | TODO | — |
| `exceptions.py` | 62 | `stdlib/asyncio/exceptions.py` | TODO | — |
| `format_helpers.py` | 84 | `stdlib/asyncio/format_helpers.py` | TODO | — |
| `futures.py` | 481 | `stdlib/asyncio/futures.py` | TODO | — |
| `graph.py` | 276 | `stdlib/asyncio/graph.py` | TODO | — |
| `locks.py` | 617 | `stdlib/asyncio/locks.py` | TODO | — |
| `log.py` | 7 | `stdlib/asyncio/log.py` | TODO | — |
| `mixins.py` | 21 | `stdlib/asyncio/mixins.py` | TODO | — |
| `proactor_events.py` | 896 | `stdlib/asyncio/proactor_events.py` | TODO | — |
| `protocols.py` | 216 | `stdlib/asyncio/protocols.py` | TODO | — |
| `queues.py` | 307 | `stdlib/asyncio/queues.py` | TODO | — |
| `runners.py` | 225 | `stdlib/asyncio/runners.py` | TODO | — |
| `selector_events.py` | 1326 | `stdlib/asyncio/selector_events.py` | TODO | — |
| `sslproto.py` | 929 | `stdlib/asyncio/sslproto.py` | TODO | — |
| `staggered.py` | 179 | `stdlib/asyncio/staggered.py` | TODO | — |
| `streams.py` | 787 | `stdlib/asyncio/streams.py` | TODO | — |
| `subprocess.py` | 229 | `stdlib/asyncio/subprocess.py` | TODO | — |
| `taskgroups.py` | 286 | `stdlib/asyncio/taskgroups.py` | TODO | — |
| `tasks.py` | 1141 | `stdlib/asyncio/tasks.py` | TODO | — |
| `threads.py` | 24 | `stdlib/asyncio/threads.py` | TODO | — |
| `timeouts.py` | 174 | `stdlib/asyncio/timeouts.py` | TODO | — |
| `tools.py` | 291 | `stdlib/asyncio/tools.py` | TODO | — |
| `transports.py` | 337 | `stdlib/asyncio/transports.py` | TODO | — |
| `trsock.py` | 98 | `stdlib/asyncio/trsock.py` | TODO | — |
| `unix_events.py` | 972 | `stdlib/asyncio/unix_events.py` | TODO | — |
| `windows_events.py` | 903 | `stdlib/asyncio/windows_events.py` | TODO | — |
| `windows_utils.py` | 181 | `stdlib/asyncio/windows_utils.py` | TODO | — |
| `__main__.py` | 245 | `stdlib/asyncio/__main__.py` | TODO | — |

C layer (`Modules/`):

| File | Lines | gopy destination | Status | Commit |
|------|------:|------------------|--------|--------|
| `_asynciomodule.c` | 4429 | `module/_asyncio/module.go` | TODO | — |

### Gate tests

| Test | Lines | Status | Commit | Notes |
|------|------:|--------|--------|-------|
| `test_tabnanny.py` | 354 | TODO | — | Unblocked once `import asyncio` succeeds; the `unittest.mock` → `asyncio.coroutines._is_coroutine` path is the only asyncio surface this test touches. |
| `test_asyncio/` (41 files) | n/a | OUT OF SCOPE | — | Full asyncio test corpus is a separate panel. |

### Downstream consumers unlocked

| Consumer | Surface | Status |
|----------|---------|--------|
| `stdlib/unittest/mock.py:284` | `mock._is_coroutine = asyncio.coroutines._is_coroutine` | TODO |
| `stdlib/unittest/async_case.py:137` | `asyncio.Runner(debug=True, loop_factory=self.loop_factory)` | TODO |

## Goal

Replace the missing `asyncio` package with a one-to-one port of every
CPython 3.14 source file in the subsystem. The Python layer ships as
byte-equal vendoring under `stdlib/asyncio/`; the C layer ships as a
faithful Go port under `module/_asyncio/`. After this spec lands,
`import asyncio` succeeds, `asyncio.coroutines._is_coroutine` is the
documented sentinel object, and `asyncio.Runner(...).run(coro)` runs a
coroutine to completion using gopy's selector-loop port.

The success criterion is `test_tabnanny.py` running green under
`test/regrtest`. The wider `test_asyncio/` corpus (41 files) is a
separate panel and not gated here.

## Workflow

The spec follows the durable port-not-patch / full-subsystem rule.
Phases below are ordered smallest-fix-first so the very first commit
flips the immediate gate (`test_tabnanny.py`) and subsequent phases
fill out the surface the rest of asyncio needs. Every phase carries
its own tracking table; tick rows off as work lands.

### Phase 1: leaf files (zero-runtime vendor)

Byte-equal vendor of the leaf files that have no runtime dependencies
and define the package's constants and exception types. None execute
non-trivial code at import time.

| # | Task | File | Surface | Status | Commit |
|---|------|------|---------|--------|--------|
| 1 | P1.1 | `log.py` | `logger = logging.getLogger("asyncio")` (vendored; lazy-bound — `logging` still blocked on threading/itertools) | WIP | _pending_ |
| 2 | P1.2 | `constants.py` | numeric/string constants used across the package | DONE | _pending_ |
| 3 | P1.3 | `exceptions.py` | `CancelledError`, `InvalidStateError`, `TimeoutError`, `IncompleteReadError`, `LimitOverrunError`, `SendfileNotAvailableError`, `BrokenBarrierError` | DONE | _pending_ |
| 4 | P1.4 | `mixins.py` | `_LoopBoundMixin` (vendored; lazy-bound — pulls `threading`) | WIP | _pending_ |
| 5 | P1.5 | `format_helpers.py` | repr helpers | DONE | _pending_ |
| 6 | P1.6 | `base_futures.py` | shared future state constants + `_format_callbacks` | DONE | _pending_ |
| 7 | P1.7 | `base_tasks.py` | shared task helpers (vendored; lazy-bound — pulls `coroutines` which Phase 2 ships) | WIP | _pending_ |
| 8 | P1.8 | `__init__.py` (stub) | placeholder package marker; eagerly binds the leaves that import without runtime deps | DONE | _pending_ |
| 9 | P1.9 | `_thread` (3.14 lifecycle handle) | `_thread.start_joinable_thread`, `_thread._ThreadHandle`, `_thread._make_thread_handle`, `_thread.daemon_threads_allowed`, `_thread._shutdown`, `_thread._get_main_thread_ident`, `_thread._is_main_interpreter`, `_thread.LockType` attribute. Required because `asyncio.log` imports `logging` imports `threading`. | DONE | _pending_ |

### Phase 2: coroutines + `_is_coroutine` sentinel

Vendor `coroutines.py` byte-equal. The single observable surface
`unittest.mock` requires is the module-level sentinel
`_is_coroutine = object()`. Audit that gopy's `inspect.iscoroutinefunction`
(vendored in spec 1710 T5.4) works against gopy's `CoroutineType`
so `_iscoroutinefunction` returns the expected answer.

This is the milestone that turns `test_tabnanny.py` green once
`__init__.py` (Phase 9) lands: `import asyncio` reaches the end of
the package init without raising, `unittest.mock` import succeeds,
and the test loads.

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P2.1 | stdlib vendor | `stdlib/asyncio/coroutines.py` (byte-equal) | TODO | — |
| 2 | P2.2 | inspect | verify `inspect.iscoroutinefunction` returns True for `async def` over gopy's `CoroutineType` (relies on spec 1710 T5.4) | TODO | — |
| 3 | P2.3 | itertools | gopy `itertools.count` exposes `__next__`; required transitively by `threading.py:853` (`_counter = _count(1).__next__`) | TODO | — |

### Phase 3: futures + events + protocols + transports

Vendor `futures.py`, `events.py`, `protocols.py`, `transports.py`,
`trsock.py`. These define the abstract interfaces
(`AbstractEventLoop`, `BaseProtocol`, `BaseTransport`) and `Future`
semantics that the rest of the package builds on. Pure Python, no OS
calls at import time.

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P3.1 | stdlib vendor | `stdlib/asyncio/futures.py` (byte-equal) | TODO | — |
| 2 | P3.2 | stdlib vendor | `stdlib/asyncio/events.py` (byte-equal) | TODO | — |
| 3 | P3.3 | stdlib vendor | `stdlib/asyncio/protocols.py` (byte-equal) | TODO | — |
| 4 | P3.4 | stdlib vendor | `stdlib/asyncio/transports.py` (byte-equal) | TODO | — |
| 5 | P3.5 | stdlib vendor | `stdlib/asyncio/trsock.py` (byte-equal) | TODO | — |
| 6 | P3.6 | contextvars | `Context.run(callable, *args, **kw)` returns the callable's return value, swapping the active context for the duration | TODO | — |
| 7 | P3.7 | weakref | `WeakSet`, `WeakValueDictionary` resolvable via `weakref` (audit, not new code) | TODO | — |

### Phase 4: locks + queues + streams

Vendor `locks.py`, `queues.py`, `streams.py`, `staggered.py`. These
build on the futures/events surface and don't reach into selectors or
sockets. `streams.py` references `socket.socket` indirectly through
`connect`/`open_connection`, but that path only fires when the loop is
used; import is safe.

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P4.1 | stdlib vendor | `stdlib/asyncio/locks.py` (byte-equal) | TODO | — |
| 2 | P4.2 | stdlib vendor | `stdlib/asyncio/queues.py` (byte-equal) | TODO | — |
| 3 | P4.3 | stdlib vendor | `stdlib/asyncio/streams.py` (byte-equal) | TODO | — |
| 4 | P4.4 | stdlib vendor | `stdlib/asyncio/staggered.py` (byte-equal) | TODO | — |
| 5 | P4.5 | collections | `collections.deque.appendleft` / `popleft` present (audit) | TODO | — |

### Phase 5: tasks + taskgroups + timeouts + runners

Vendor `tasks.py`, `taskgroups.py`, `timeouts.py`, `runners.py`,
`threads.py`. `runners.py` exposes `asyncio.Runner` (the surface
`stdlib/unittest/async_case.py` uses). At this point
`asyncio.Runner(...).run(coro)` should work for trivial coroutines
that do not touch I/O — drive scheduling via the gopy port of
`tasks._step` plus the base_events ready-queue (Phase 7).

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P5.1 | stdlib vendor | `stdlib/asyncio/tasks.py` (byte-equal) | TODO | — |
| 2 | P5.2 | stdlib vendor | `stdlib/asyncio/taskgroups.py` (byte-equal) | TODO | — |
| 3 | P5.3 | stdlib vendor | `stdlib/asyncio/timeouts.py` (byte-equal) | TODO | — |
| 4 | P5.4 | stdlib vendor | `stdlib/asyncio/runners.py` (byte-equal) | TODO | — |
| 5 | P5.5 | stdlib vendor | `stdlib/asyncio/threads.py` (byte-equal) | TODO | — |
| 6 | P5.6 | VM | coroutine `.send()` / `.throw()` / `.close()` slot wiring through to `genobject.c` semantics | TODO | — |

### Phase 6: graph + tools + subprocess

Vendor `graph.py`, `tools.py`, `subprocess.py`, `base_subprocess.py`.
These layer on top of tasks and have no fresh OS surface beyond
subprocess (which fails fast and clean on gopy until the subprocess
port lands separately).

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P6.1 | stdlib vendor | `stdlib/asyncio/graph.py` (byte-equal) | TODO | — |
| 2 | P6.2 | stdlib vendor | `stdlib/asyncio/tools.py` (byte-equal) | TODO | — |
| 3 | P6.3 | stdlib vendor | `stdlib/asyncio/subprocess.py` (byte-equal) | TODO | — |
| 4 | P6.4 | stdlib vendor | `stdlib/asyncio/base_subprocess.py` (byte-equal) | TODO | — |

### Phase 7: base_events (event loop core)

Vendor `base_events.py`. This is the largest file (2082 lines) and
defines `BaseEventLoop`, the canonical scheduler. The port is verbatim
Python; the runtime surface it depends on is large.

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P7.1 | stdlib vendor | `stdlib/asyncio/base_events.py` (byte-equal) | TODO | — |
| 2 | P7.2 | selectors | `selectors.DefaultSelector` resolves on darwin/linux/windows | TODO | — |
| 3 | P7.3 | signal | `signal.set_wakeup_fd`, `signal.valid_signals` on `module/_signal` | TODO | — |
| 4 | P7.4 | socket | `socket.socket` resolvable; `module/_socket` covers the surface base_events uses | TODO | — |
| 5 | P7.5 | threading | `threading.Event` wired (audit) | TODO | — |

### Phase 8: selector_events + proactor_events + sslproto

Vendor the concrete loop implementations and the SSL handshake state
machine. Imports execute cleanly; the actual `loop.run_forever()`
path is what exercises them and is tested in `test_asyncio/` (out of
scope for 1711).

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P8.1 | stdlib vendor | `stdlib/asyncio/selector_events.py` (byte-equal) | TODO | — |
| 2 | P8.2 | stdlib vendor | `stdlib/asyncio/proactor_events.py` (byte-equal) | TODO | — |
| 3 | P8.3 | stdlib vendor | `stdlib/asyncio/sslproto.py` (byte-equal) | TODO | — |
| 4 | P8.4 | ssl | `ssl.MemoryBIO` exists or `sslproto` import is guarded; verify on gopy `_ssl` | TODO | — |

### Phase 9: platform loops + full `__init__.py`

Vendor `unix_events.py`, `windows_events.py`, `windows_utils.py`, and
swap the Phase 1 stub `__init__.py` for the byte-equal CPython copy
that imports every submodule. `__init__.py:44` does
`if sys.platform == 'win32': from .windows_events import * else: from
.unix_events import *`, so one of them executes on every import.
Verify both build cleanly even on the opposite platform (unimported
branches must still parse).

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P9.1 | stdlib vendor | `stdlib/asyncio/unix_events.py` (byte-equal) | TODO | — |
| 2 | P9.2 | stdlib vendor | `stdlib/asyncio/windows_events.py` (byte-equal) | TODO | — |
| 3 | P9.3 | stdlib vendor | `stdlib/asyncio/windows_utils.py` (byte-equal) | TODO | — |
| 4 | P9.4 | stdlib vendor | replace Phase 1 stub `__init__.py` with the byte-equal CPython copy | TODO | — |
| 5 | P9.5 | stdlib vendor | `stdlib/asyncio/__main__.py` (byte-equal) | TODO | — |

### Phase 10: C-level `_asyncio` module

Port `Modules/_asynciomodule.c` (4429 lines) into
`module/_asyncio/module.go`. The Python layer falls back to pure-Python
implementations when `_asyncio` is missing (see e.g. `futures.py`'s
`try: import _asyncio` blocks), so the full port can be staged after
Phase 9. Until this phase lands the Python fallback paths are
exercised; that is the correct behaviour, not a divergence.

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P10.1 | module port | `Future` C type (fast path for `Future`) | TODO | — |
| 2 | P10.2 | module port | `Task` C type and `_PyAsyncioState` task-context plumbing | TODO | — |
| 3 | P10.3 | module port | `_swap_current_task`, `_register_task`, `_unregister_task`, `_enter_task`, `_leave_task` | TODO | — |
| 4 | P10.4 | module port | `_get_event_loop` C-side fast path, `_set_event_loop` / `set_running_loop` accessors | TODO | — |
| 5 | P10.5 | module port | module-level constants exposed back to Python | TODO | — |

### Phase 11: inittab + path-finder wiring

`module/_asyncio/module.go` calls
`imp.AppendInittab("_asyncio", buildModule)` in its `init()`, and the
blank import lands in `stdlibinit/registry.go` next to `_opcode`. The
Python package at `stdlib/asyncio/` is picked up automatically by the
existing `PathFinder` walk; no extra wiring is needed beyond a smoke
test.

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P11.1 | inittab | bind `_asyncio` in `stdlibinit/registry.go` | TODO | — |
| 2 | P11.2 | smoke | `import asyncio; asyncio.Runner` resolves; `import _asyncio; _asyncio.Future` resolves | TODO | — |

### Phase 12: land the `test_tabnanny.py` gate

Run `test/regrtest test_tabnanny.py`. Fix any divergence in the
vendored Python files or the gopy runtime; never edit the test.
`test/cpython/` is the canonical gate location.

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | P12.1 | gate test | `test/cpython/test_tabnanny.py` green under `test/regrtest` | TODO | — |
| 2 | P12.2 | spec 1710 | flip 1710 T6 rows in both `test_tabnanny.py` and `test_tokenize.py` chains to DONE with this spec's tip commit | TODO | — |

DFS execution order: Phase 1 → Phase 2 (closes the immediate
`unittest.mock` import gap) → short-circuit to Phase 12 to flip the
gate as soon as it goes green → Phases 3–11 fill the rest of the
surface. Each row in each phase ships as its own commit; large rows
(P7.1, P8.1, P8.2, P10.1, P10.2) may split further as runtime gaps
surface.

## Out of scope

- `Lib/test/test_asyncio/` (41 files) — the full asyncio test corpus
  is a separate v0.12.4 panel; 1711 only gates `test_tabnanny.py`.
- `IocpProactor` / `_overlapped` / Windows-only IO primitives — vendored
  for byte equivalence but not exercised on darwin/linux.
- `asyncio` integration with the gopy GIL / scheduler — the port
  preserves CPython's single-threaded event-loop model; any future
  goroutine-backed loop is a new spec.
- The `Modules/_overlapped.c` Windows extension and the
  `Modules/socketmodule.c` proactor surface — both belong to their own
  full-port specs (socket / Windows IO) and only matter when the
  proactor loop is actually run.

## Cross-references

- Spec 1710 (`1710_v0124_lexer_tokenizer_full_port.md`) T6 rows in
  the `test_tabnanny.py` and `test_tokenize.py` chains, both BLOCKED
  on this spec.
- Spec 1700 (v0.12.4 panel) task #484 — flips to DONE only after
  both 1710 and 1711 are green.
- Standing rules: port-not-patch, full-subsystem,
  CPython citations with line numbers, specs live under
  `website/docs/specs/<group>/`.
