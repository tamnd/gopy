---
name: project-dict-lock-thread-key
description: Per-dict critical section must key reentrancy on the Python thread, not the goroutine
metadata:
  type: project
---

gopy runs every generator/coroutine/async-generator body on its own goroutine,
but that body shares its driver's `*state.Thread` (it inherits `savedTS` via the
active-thread map), exactly as CPython runs a generator on the driver's
PyThreadState. So any goroutine-reentrant lock (the per-dict critical section
from spec 1728, `objects/dict_lock.go`) MUST key its owner on the Python thread
id, not the raw goroutine id. Keying on the goroutine deadlocks: a driver that
holds a dict lock and resumes a body touching the same dict (any globals lookup)
sees the body block on a lock its own thread already owns.

Fix shape: `objects.CriticalSectionOwnerHook` returns `currentThread().ID()`
(wired in `vm/thread_hook.go`), tagged out of the goroutine number space, with a
goid fallback for goroutines doing dict ops outside the eval loop. Distinct real
Python threads still get distinct ids and serialize, preserving the no-GIL dict
safety. See [[project_spec_1722]] panel work and spec 1729.

**Why:** the deadlock only appears once the lock is load-bearing (owned-store,
1727) AND a generator drives a dict op, so it is easy to miss in single-threaded
repros. **How to apply:** any future per-object lock that can be re-entered
through a generator/coroutine resume must use the thread identity, never goid.
