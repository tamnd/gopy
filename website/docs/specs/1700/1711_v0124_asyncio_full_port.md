---
format: md
id: 1711_v0124_asyncio_full_port
title: "1711. v0.12.4 asyncio full port"
sidebar_label: "1711. v0.12.4 asyncio"
sidebar_position: 1711
slug: /specs/1711-v0124-asyncio
description: "Port the CPython 3.14 asyncio package (Lib/asyncio/*.py + Modules/_asynciomodule.c) into gopy under module/asyncio + module/_asyncio so import asyncio, asyncio.coroutines._is_coroutine, and asyncio.Runner work end-to-end. Closes the T6 BLOCKED row in spec 1710 and unblocks test_tabnanny.py."
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

Python sources to vendor / port (CPython 3.14, `Lib/asyncio/`):

- [ ] `__init__.py` (74 lines) → `stdlib/asyncio/__init__.py`
- [ ] `base_events.py` (2082 lines) → `stdlib/asyncio/base_events.py`
- [ ] `base_futures.py` (67 lines) → `stdlib/asyncio/base_futures.py`
- [ ] `base_subprocess.py` (319 lines) → `stdlib/asyncio/base_subprocess.py`
- [ ] `base_tasks.py` (94 lines) → `stdlib/asyncio/base_tasks.py`
- [ ] `constants.py` (41 lines) → `stdlib/asyncio/constants.py`
- [ ] `coroutines.py` (118 lines) → `stdlib/asyncio/coroutines.py`
- [ ] `events.py` (878 lines) → `stdlib/asyncio/events.py`
- [ ] `exceptions.py` (62 lines) → `stdlib/asyncio/exceptions.py`
- [ ] `format_helpers.py` (84 lines) → `stdlib/asyncio/format_helpers.py`
- [ ] `futures.py` (481 lines) → `stdlib/asyncio/futures.py`
- [ ] `graph.py` (276 lines) → `stdlib/asyncio/graph.py`
- [ ] `locks.py` (617 lines) → `stdlib/asyncio/locks.py`
- [ ] `log.py` (7 lines) → `stdlib/asyncio/log.py`
- [ ] `mixins.py` (21 lines) → `stdlib/asyncio/mixins.py`
- [ ] `proactor_events.py` (896 lines) → `stdlib/asyncio/proactor_events.py`
- [ ] `protocols.py` (216 lines) → `stdlib/asyncio/protocols.py`
- [ ] `queues.py` (307 lines) → `stdlib/asyncio/queues.py`
- [ ] `runners.py` (225 lines) → `stdlib/asyncio/runners.py`
- [ ] `selector_events.py` (1326 lines) → `stdlib/asyncio/selector_events.py`
- [ ] `sslproto.py` (929 lines) → `stdlib/asyncio/sslproto.py`
- [ ] `staggered.py` (179 lines) → `stdlib/asyncio/staggered.py`
- [ ] `streams.py` (787 lines) → `stdlib/asyncio/streams.py`
- [ ] `subprocess.py` (229 lines) → `stdlib/asyncio/subprocess.py`
- [ ] `taskgroups.py` (286 lines) → `stdlib/asyncio/taskgroups.py`
- [ ] `tasks.py` (1141 lines) → `stdlib/asyncio/tasks.py`
- [ ] `threads.py` (24 lines) → `stdlib/asyncio/threads.py`
- [ ] `timeouts.py` (174 lines) → `stdlib/asyncio/timeouts.py`
- [ ] `tools.py` (291 lines) → `stdlib/asyncio/tools.py`
- [ ] `transports.py` (337 lines) → `stdlib/asyncio/transports.py`
- [ ] `trsock.py` (98 lines) → `stdlib/asyncio/trsock.py`
- [ ] `unix_events.py` (972 lines) → `stdlib/asyncio/unix_events.py`
- [ ] `windows_events.py` (903 lines) → `stdlib/asyncio/windows_events.py`
- [ ] `windows_utils.py` (181 lines) → `stdlib/asyncio/windows_utils.py`
- [ ] `__main__.py` (245 lines) → `stdlib/asyncio/__main__.py`

C sources to port (CPython 3.14):

- [ ] `Modules/_asynciomodule.c` (4429 lines) → `module/_asyncio/module.go`

Gate tests:

- [ ] `test_tabnanny.py` — vendored under `test/cpython/test_tabnanny.py`, unblocked once `import asyncio` succeeds (the `unittest.mock` → `asyncio.coroutines._is_coroutine` path is the only asyncio surface this test touches).
- [ ] `test_asyncio/` directory (41 files) — not in scope for 1711; the v0.12.4 asyncio panel is `test_tabnanny.py` only. A future spec stands up the full `test_asyncio/` corpus.

Downstream consumers that unlock when 1711 lands:

- [ ] `stdlib/unittest/mock.py:284` — `mock._is_coroutine = asyncio.coroutines._is_coroutine`
- [ ] `stdlib/unittest/async_case.py:137` — `asyncio.Runner(debug=True, loop_factory=self.loop_factory)`

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

## Sources of truth

Python sources (3.14):

| CPython file | Lines | gopy destination |
|--------------|------:|------------------|
| Lib/asyncio/__init__.py | 74 | stdlib/asyncio/__init__.py |
| Lib/asyncio/base_events.py | 2082 | stdlib/asyncio/base_events.py |
| Lib/asyncio/base_futures.py | 67 | stdlib/asyncio/base_futures.py |
| Lib/asyncio/base_subprocess.py | 319 | stdlib/asyncio/base_subprocess.py |
| Lib/asyncio/base_tasks.py | 94 | stdlib/asyncio/base_tasks.py |
| Lib/asyncio/constants.py | 41 | stdlib/asyncio/constants.py |
| Lib/asyncio/coroutines.py | 118 | stdlib/asyncio/coroutines.py |
| Lib/asyncio/events.py | 878 | stdlib/asyncio/events.py |
| Lib/asyncio/exceptions.py | 62 | stdlib/asyncio/exceptions.py |
| Lib/asyncio/format_helpers.py | 84 | stdlib/asyncio/format_helpers.py |
| Lib/asyncio/futures.py | 481 | stdlib/asyncio/futures.py |
| Lib/asyncio/graph.py | 276 | stdlib/asyncio/graph.py |
| Lib/asyncio/locks.py | 617 | stdlib/asyncio/locks.py |
| Lib/asyncio/log.py | 7 | stdlib/asyncio/log.py |
| Lib/asyncio/mixins.py | 21 | stdlib/asyncio/mixins.py |
| Lib/asyncio/proactor_events.py | 896 | stdlib/asyncio/proactor_events.py |
| Lib/asyncio/protocols.py | 216 | stdlib/asyncio/protocols.py |
| Lib/asyncio/queues.py | 307 | stdlib/asyncio/queues.py |
| Lib/asyncio/runners.py | 225 | stdlib/asyncio/runners.py |
| Lib/asyncio/selector_events.py | 1326 | stdlib/asyncio/selector_events.py |
| Lib/asyncio/sslproto.py | 929 | stdlib/asyncio/sslproto.py |
| Lib/asyncio/staggered.py | 179 | stdlib/asyncio/staggered.py |
| Lib/asyncio/streams.py | 787 | stdlib/asyncio/streams.py |
| Lib/asyncio/subprocess.py | 229 | stdlib/asyncio/subprocess.py |
| Lib/asyncio/taskgroups.py | 286 | stdlib/asyncio/taskgroups.py |
| Lib/asyncio/tasks.py | 1141 | stdlib/asyncio/tasks.py |
| Lib/asyncio/threads.py | 24 | stdlib/asyncio/threads.py |
| Lib/asyncio/timeouts.py | 174 | stdlib/asyncio/timeouts.py |
| Lib/asyncio/tools.py | 291 | stdlib/asyncio/tools.py |
| Lib/asyncio/transports.py | 337 | stdlib/asyncio/transports.py |
| Lib/asyncio/trsock.py | 98 | stdlib/asyncio/trsock.py |
| Lib/asyncio/unix_events.py | 972 | stdlib/asyncio/unix_events.py |
| Lib/asyncio/windows_events.py | 903 | stdlib/asyncio/windows_events.py |
| Lib/asyncio/windows_utils.py | 181 | stdlib/asyncio/windows_utils.py |
| Lib/asyncio/__main__.py | 245 | stdlib/asyncio/__main__.py |

C sources (3.14):

| CPython file | Lines | gopy destination |
|--------------|------:|------------------|
| Modules/_asynciomodule.c | 4429 | module/_asyncio/module.go |

Gate test lives at `~/github/python/cpython/Lib/test/test_tabnanny.py`.

## Workflow

The spec follows the durable port-not-patch / full-subsystem rule.
Phases below are ordered smallest-fix-first so the very first commit
flips the immediate gate (`test_tabnanny.py`) and subsequent phases
fill out the surface the rest of asyncio needs.

### Phase 1: bottom-of-the-package vendor (zero-runtime files)

Vendor the leaf files that have no runtime dependencies and define
the package's constants and exception types. None of these execute
non-trivial code at import time, so they land as byte-equal copies.

| File | Surface |
|------|---------|
| `log.py` | `logger = logging.getLogger("asyncio")` |
| `constants.py` | numeric/string constants used across the package |
| `exceptions.py` | `CancelledError`, `InvalidStateError`, `TimeoutError`, `IncompleteReadError`, `LimitOverrunError`, `SendfileNotAvailableError`, `BrokenBarrierError` |
| `mixins.py` | `_LoopBoundMixin` |
| `format_helpers.py` | repr helpers |
| `base_futures.py` | shared future state constants + `_format_callbacks` |
| `base_tasks.py` | shared task helpers |

### Phase 2: coroutines + `_is_coroutine` sentinel

Vendor `coroutines.py` byte-equal. The single observable surface
`unittest.mock` requires is the module-level sentinel
`_is_coroutine = object()`. Audit that gopy's `inspect.iscoroutinefunction`
(already vendored by spec 1710 T5.4) works against gopy's
`CoroutineType` so `_iscoroutinefunction` returns the expected answer.

This is the milestone that turns `test_tabnanny.py` green: once
`import asyncio` reaches the end of `__init__.py` without raising,
`unittest.mock` import succeeds, and the test loads.

### Phase 3: futures + events + protocols + transports

Vendor `futures.py`, `events.py`, `protocols.py`, `transports.py`,
`trsock.py`. These define the abstract interfaces (`AbstractEventLoop`,
`BaseProtocol`, `BaseTransport`) and `Future` semantics that the rest
of the package builds on. Pure Python, no OS calls at import time.

Runtime gaps to expect:
- `contextvars.copy_context()` — verify gopy `contextvars` returns a
  `Context` with `.run(callable, ...)` semantics.
- `threading.get_ident()` — already exposed by `_thread`.
- `weakref.WeakSet` — already in `stdlib/_weakrefset.py`.

### Phase 4: locks + queues + streams

Vendor `locks.py`, `queues.py`, `streams.py`, `staggered.py`.
These build on the futures/events surface and don't reach into
selectors or sockets. `streams.py` does reference `socket.socket`
indirectly through `connect`/`open_connection`, but that path only
fires when the loop is actually used; import is safe.

### Phase 5: tasks + taskgroups + timeouts + runners

Vendor `tasks.py`, `taskgroups.py`, `timeouts.py`, `runners.py`,
`threads.py`. `runners.py` exposes `asyncio.Runner` (the surface
`stdlib/unittest/async_case.py` uses). At this point
`asyncio.Runner(...).run(coro)` should work for trivial coroutines
that do not touch I/O — drive scheduling via the gopy port of
`tasks._step` plus the base_events ready-queue (Phase 7).

### Phase 6: graph + tools

Vendor `graph.py`, `tools.py`, `subprocess.py`, `base_subprocess.py`.
These layer on top of tasks and have no fresh OS surface beyond
subprocess (which fails fast and clean on gopy until the subprocess
port lands separately).

### Phase 7: base_events (event loop core)

Vendor `base_events.py`. This is the largest file (2082 lines) and
defines `BaseEventLoop`, the canonical scheduler. The port is verbatim
Python; the runtime surface it depends on is:

- `selectors.DefaultSelector` — gopy already ships `stdlib/selectors.py`,
  audit that `KqueueSelector` / `EpollSelector` / `PollSelector` resolve.
- `socket` module — gopy ships `module/_socket` + `stdlib/socket.py`.
- `signal.set_wakeup_fd` — needs verification on gopy `module/_signal`.
- `threading.Event` — already wired.

Document any runtime gap that surfaces in this phase as a follow-up
task on this spec.

### Phase 8: selector_events + proactor_events + sslproto

Vendor `selector_events.py`, `proactor_events.py`, `sslproto.py`.
These implement the concrete loops and the SSL handshake state
machine. Imports execute cleanly; the actual `loop.run_forever()`
path is what exercises them and is tested in `test_asyncio/`
(out of scope for 1711).

### Phase 9: platform loops

Vendor `unix_events.py` plus `windows_events.py` and
`windows_utils.py`. `__init__.py:44` does
`if sys.platform == 'win32': from .windows_events import *
else: from .unix_events import *`, so one of them executes on every
import. Verify both build cleanly even on the opposite platform
(unimported branches must still parse).

### Phase 10: C-level `_asyncio` module

Port `Modules/_asynciomodule.c` (4429 lines) into
`module/_asyncio/module.go`. The Python layer falls back to pure-Python
implementations when `_asyncio` is missing (see e.g. `futures.py`'s
`try: import _asyncio` blocks), so the full port can be staged after
Phase 9. The surface to cover:

- `Future` C type (fast path for `Future`)
- `Task` C type and the `_PyAsyncioState` task-context plumbing
- `_swap_current_task` / `_register_task` / `_unregister_task`
- `_enter_task` / `_leave_task`
- `_get_event_loop` C-side fast path
- The `_set_event_loop` / `set_running_loop` accessors
- Module-level constants exposed back to Python

Until this phase lands, the Python fallback paths are exercised; that
is the correct behaviour, not a divergence.

### Phase 11: register on the inittab + path-finder

`module/_asyncio/module.go` calls
`imp.AppendInittab("_asyncio", buildModule)` in its `init()`, and the
blank import lands in `stdlibinit/registry.go` next to `_opcode`. The
Python package at `stdlib/asyncio/` is picked up automatically by the
existing `PathFinder` walk; no extra wiring is needed.

### Phase 12: land the `test_tabnanny.py` gate

Run `test/regrtest test_tabnanny.py`. Fix any divergence in the
vendored Python files or the gopy runtime; never edit the test.
Flip the 1710 T6 row to DONE with this commit's SHA. Mirror the
test into `stdtest/` and extend `TestStdtestCorpus`.

## Sub-system blockers (DFS)

Each phase has its own runtime-gap surface; the table below records
the ones we already know will trip the port before the work starts.
New entries get added here as the DFS surfaces them.

### Phase 2 chain (coroutines)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A1 | stdlib vendor | `stdlib/asyncio/coroutines.py` (byte-equal) | TODO | — |
| 2 | A2 | inspect | verify `inspect.iscoroutinefunction` returns True for `async def` over gopy's CoroutineType (relies on spec 1710 T5.4) | TODO | — |

### Phase 3 chain (futures + events)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A3 | contextvars | `Context.run(callable, *args, **kw)` returns the callable's return value, swapping the active context for the duration | TODO | — |
| 2 | A4 | stdlib vendor | `futures.py`, `events.py`, `protocols.py`, `transports.py`, `trsock.py` (all byte-equal) | TODO | — |
| 3 | A5 | weakref | `WeakSet`, `WeakValueDictionary` resolvable via `weakref` (audit, not new code) | TODO | — |

### Phase 4 chain (locks/queues/streams)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A6 | stdlib vendor | `locks.py`, `queues.py`, `streams.py`, `staggered.py` (byte-equal) | TODO | — |
| 2 | A7 | collections | `collections.deque.appendleft` / `popleft` already present (audit) | TODO | — |

### Phase 5 chain (tasks/runners)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A8 | stdlib vendor | `tasks.py`, `taskgroups.py`, `timeouts.py`, `runners.py`, `threads.py` (byte-equal) | TODO | — |
| 2 | A9 | VM | coroutine `.send()` / `.throw()` / `.close()` slot wiring through to `genobject.c` semantics | TODO | — |

### Phase 6 chain (graph/tools/subprocess)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A10 | stdlib vendor | `graph.py`, `tools.py`, `subprocess.py`, `base_subprocess.py` (byte-equal) | TODO | — |

### Phase 7 chain (base_events)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A11 | selectors | `selectors.DefaultSelector` resolves on darwin/linux/windows | TODO | — |
| 2 | A12 | signal | `signal.set_wakeup_fd`, `signal.valid_signals` on `module/_signal` | TODO | — |
| 3 | A13 | stdlib vendor | `base_events.py` (byte-equal) | TODO | — |

### Phase 8 chain (selector/proactor/ssl)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A14 | stdlib vendor | `selector_events.py`, `proactor_events.py`, `sslproto.py` (byte-equal) | TODO | — |
| 2 | A15 | ssl | `ssl.MemoryBIO` exists or `sslproto` import is guarded; verify on gopy `_ssl` | TODO | — |

### Phase 9 chain (platform loops)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A16 | stdlib vendor | `unix_events.py`, `windows_events.py`, `windows_utils.py` (byte-equal) | TODO | — |

### Phase 10 chain (_asyncio C module)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A17 | module port | `module/_asyncio/module.go` — `Future`, `Task`, `_swap_current_task`, `_register_task`, `_unregister_task`, `_enter_task`, `_leave_task`, `_get_event_loop` | TODO | — |
| 2 | A18 | inittab | bind `_asyncio` in `stdlibinit/registry.go` | TODO | — |

### Phase 12 chain (gate)

| # | Task | Sub-system | Surface | Status | Commit |
|---|------|------------|---------|--------|--------|
| 1 | A19 | gate test | `test/cpython/test_tabnanny.py` green under `test/regrtest`; also mirrored at `stdtest/test_tabnanny.py` and added to `TestStdtestCorpus` | TODO | — |
| 2 | A20 | spec 1710 | flip 1710 T6 rows in both `test_tabnanny.py` and `test_tokenize.py` chains to DONE with this spec's tip commit | TODO | — |

DFS execution order: Phase 1 → Phase 2 (closes the immediate
`unittest.mock` import gap) → Phase 12 short-circuit to flip the gate
→ Phases 3–11 fill the rest of the surface. Each phase ships as a
self-contained commit; large phases (Phase 7, Phase 8, Phase 10) may
need to split further.

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
