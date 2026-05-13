---
format: md
id: 1702_subsystem_port_log
title: "1702. Subsystem port log (v0.12.1 unittest enablement)"
sidebar_label: "1702. subsystem_port_log"
sidebar_position: 1702
slug: /specs/1702-subsystem_port_log
description: "Per-subsystem port detail for the unittest enablement push under spec 1701. A single status table tracks every subsystem; paragraphs below capture the structured detail (surface, location, deferred items, gate) for each one that has landed or is blocked. Read this when triaging downstream failures so you know which CPython source files have been brought across in full versus which are still identity-shim placeholders."
---


This document is the audit trail for spec 1701's "no partial stubs"
rule. The status table below names every subsystem the unittest
gate touches; entries marked **done** or **partial** have a
paragraph in the detail section. If a paragraph and the code ever
disagree, the code wins and both must be reconciled in the same
commit that fixes the drift.

Status legend:

| Symbol | Meaning |
|--------|---------|
| done | full port landed; every public name real and cited |
| partial | vendored or shim in place but the real load is blocked; see paragraph for the blocker |
| in-flight | a parallel agent is porting it right now |
| pending | not started |

## Status table

| Subsystem | Task | Status | CPython source | gopy destination | Wave | Notes |
|-----------|------|--------|----------------|------------------|------|-------|
| errno | #499 | done | `Modules/errnomodule.c` | `module/errno/` | 1 | C-extension; inittab is the final mechanism |
| _colorize | #520 | partial | `Lib/_colorize.py` | `stdlib/_colorize.py` + `module/colorize/` shim | 1 | Vendored; inittab shim still wins; blocked on `dataclasses` |
| fnmatch | #519 | done | `Lib/fnmatch.py` | `module/fnmatch/module.go` | 1 | Option B (Go port); vendor blocked on `itertools`, `posixpath`, `os.path.normcase`, `re` gaps |
| functools | #498 | partial | `Lib/functools.py` + `Modules/_functoolsmodule.c` | `stdlib/functools.py` + `module/functools/` + `module/_functools/` | 1 | `cmp_to_key` gate cleared. Vendor `.py` shadowed by inittab until `abc`, `operator`, `reprlib`, `_thread` land. `total_ordering` and `singledispatch` are partial. |
| types | #509 | partial | `Lib/types.py` + `Modules/_typesmodule.c` | `stdlib/types.py` + `module/types/` (registered as `_types`) | 1 | Vendored + `_types` builtin shipped; missing `ClassMethodDescriptorType`, `MethodWrapperType`, `WrapperDescriptorType`, `GenericAlias`, `UnionType` (no runtime equivalents yet) |
| collections | #497 | pending | `Lib/collections/__init__.py` + `Modules/_collectionsmodule.c` | `stdlib/collections/` + `module/_collections/` | 2 | Includes `OrderedDict`, `deque`, `defaultdict`, `Counter`, `ChainMap`, `namedtuple` |
| contextlib | #508 | done | `Lib/contextlib.py` | `stdlib/contextlib.py` | 2 | Vendored byte-equal; loads via PathFinder; gate cleared in PR #21 |
| abc | #533 | pending | `Lib/abc.py` + `Modules/_abc.c` | `stdlib/abc.py` + `module/_abc/` | 2 | Unblocks `_collections_abc` and the operator/reprlib/_thread chain that gates `functools` vendor flip |
| collections.abc | #531 | pending | `Lib/_collections_abc.py` | `stdlib/_collections_abc.py` + `stdlib/collections/abc.py` | 2 | Unblocks `_colorize` real load (its `from collections.abc import Callable, Iterator, Mapping`) |
| operator | #532 | pending | `Lib/operator.py` + `Modules/_operator.c` | `stdlib/operator.py` + `module/_operator/` | 2 | Required by `functools` vendor flip |
| warnings | #513 | pending | `Lib/warnings.py` + `Python/_warnings.c` | `stdlib/warnings.py` + `module/_warnings/` | 2 | |
| pprint | #511 | pending | `Lib/pprint.py` | `stdlib/pprint.py` | 2 | |
| traceback | #496 | pending | `Lib/traceback.py` | `stdlib/traceback.py` | 3 | Depends on collections, functools, `_colorize` (real load) |
| dataclasses | #522 | partial | `Lib/dataclasses.py` | `module/dataclasses/` (Go port, option B) | 3 | `_colorize` shim still wins: vendor blocked by missing `collections.abc`, subscriptable `Mapping[str, str]`, zero-arg `super()`. `make_dataclass`, `__hash__` gen, `order=True`, `slots=True`, recursive `asdict`/`astuple`, `InitVar` integration all deferred. |
| GenericAlias + UnionType | #523 | done | `Objects/genericaliasobject.c` + `Objects/unionobject.c` | `objects/generic_alias.go` + `objects/union_type.go` + `objects/class_getitem.go` | 3 | Clears `unittest.case` `types.GenericAlias` gate. `TypeVar` / `ParamSpec` substitution deferred (needs `typing` C accelerator). |
| time | #500 | pending | `Modules/timemodule.c` | `module/time/` | I | |
| re / _sre | #510 | done | `Lib/re/` + `Modules/_sre/` | `stdlib/re/` + `module/_sre/` | I | Full CPython-faithful bytecode interpreter; vendored Python layer drives it. Final gate pinned in `stdlibinit/re_match_smoke_test.go`. See spec 1703. |
| enum | #544 | done | `Lib/enum.py` | `stdlib/enum.py` | I | Vendored byte-equal; PEP 487 hooks (`__init_subclass__`, `__set_name__`) and the `@enum.global_enum` decorator land via the mappingproxy methodlist + `dict.update(keys() fast path)` fix. Pinned by `stdlibinit/enum_import_test.go` and `stdlibinit/re_match_smoke_test.go`. |
| difflib | #512 | pending | `Lib/difflib.py` | `stdlib/difflib.py` | I | Big |
| io / _io | #514 | pending | `Lib/io.py` + `Modules/_io/` | `stdlib/io.py` + `module/_io/` | I | `StringIO` already exists; need `TextIOWrapper`, `BufferedReader`, `FileIO` |
| argparse | #515 | pending | `Lib/argparse.py` | `stdlib/argparse.py` | I | Very large; needed by unittest's `__main__` |
| signal / _signal | #516 | pending | `Modules/signalmodule.c` | `module/_signal/` | I | |
| weakref / _weakref | #517 | pending | `Lib/weakref.py` + `Modules/_weakref.c` | `stdlib/weakref.py` + `module/_weakref/` | I | |
| os + posixpath + ntpath | #518 | pending | `Lib/os.py`, `Lib/posixpath.py`, `Lib/ntpath.py`, `Modules/posixmodule.c` slice | `stdlib/` + `module/posix/` | I | |
| VM / compile audit | #521 | pending | `Python/ceval.c`, `Python/bytecodes.c`, `Python/compile.c` | (in-tree) | A | Sweep after built-ins land; record findings as additional rows here |

Wave column: `1` = first parallel batch, `2` = second batch
(queued after wave 1 merges), `3` = depends on earlier waves,
`I` = independent infrastructure (any order),
`A` = audit pass.

## Detail format

Each subsystem paragraph below follows the same structure so the
table and the prose stay in sync:

- **Surface.** What public names are now real.
- **Location.** Files added or modified, plus the inittab /
  PathFinder mechanism.
- **Deferred.** Anything intentionally left out, with the reason
  and the follow-up task.
- **Gate.** What this port unblocks (or, for partial entries, what
  is still blocking it).

## errno

**Surface.** Every per-platform `Exxx` constant CPython exposes is
present, plus the `errorcode` reverse-lookup dict. Linux aliases
(`EDEADLOCK` / `EDEADLK`, etc.) resolve to the canonical name
CPython picks because `addErrcode` writes only the first-seen
`code -> name` pair.

**Location.** `module/errno/` is the full port of CPython 3.14
`Modules/errnomodule.c`. `module.go` registers through
`imp.AppendInittab("errno", buildModule)` in `init()`. Per-platform
constants live in three build-tag files: `entries_darwin.go` (103
constants), `entries_linux.go` (132 constants), `entries_other.go`
(empty fallback under `//go:build !darwin && !linux`). Every Go
helper carries a `// CPython: Modules/errnomodule.c:NN` citation.
The inittab blank import was added to `stdlibinit/registry.go`
with the `config.c.in:46` citation.

**Deferred.** None. This is a built-in C-extension equivalent so
there is no Python-level vendor file and nothing for PathFinder to
load: inittab is the correct and final mechanism.

**Gate.** Acceptance run
`gopy -c 'import errno; print(errno.ENOENT, errno.errorcode[errno.ENOENT])'`
prints `2 ENOENT`; `go test ./...` is green; cross-compiles to
darwin, linux, and windows are all clean.

## _colorize

**Surface.** `stdlib/_colorize.py` is a byte-equal vendor of
CPython 3.14 `Lib/_colorize.py` (sha256
`15db41bec8e25fd1ba81cf234df2cc7446d0fbc10285fda73186176365efd730`),
recorded in `stdlib/MANIFEST.txt`. **The vendored file does not
load yet.** Runtime behaviour today is still the
`module/colorize/module.go` shim: `can_colorize` hard-wired to
`False`, `decolor` as `identity`, `set_theme` a no-op, and
`Theme.__getattr__` returning `""` for every attribute so callers
silently see empty strings instead of `AttributeError`. This is
exactly the identity-decorator shape spec 1701 forbids.

**Location.** Vendor file at `stdlib/_colorize.py`; shim still
registered through `module/colorize/module.go` via
`imp.AppendInittab`. Import order in `imp/import.go` is
frozen → inittab → PathFinder, so `import _colorize` short-circuits
on the shim before PathFinder ever reaches the `.py`.

**Deferred.** Two prerequisites are missing for the vendored file
to win: (1) a real `dataclasses` port: the vendored `_colorize.py`
opens with `from dataclasses import dataclass, field, Field` and
applies `@dataclass(frozen=True)` / `@dataclass(frozen=True, kw_only=True)`
to five classes, so the file cannot import until `dataclasses`
lands; (2) the inittab entry in `module/colorize/module.go` must
be removed (or guarded) so PathFinder takes the import. Follow-up:
queue a `dataclasses` port task (wave 3 in the table above), then
delete `module/colorize/`.

**Gate.** Blocked on `dataclasses`. Once dataclasses lands and the
shim is retired, the gate is whatever the next consumer needs
from `_colorize` (currently `traceback`, which imports it for
coloured output).

## fnmatch

**Surface.** Full public surface present and cited:
`fnmatch`, `fnmatchcase`, `filter`, `filterfalse`, `translate`.
`filterfalse` (missing from the prior shim but in CPython's
`__all__`) is now shipped.

**Location.** `module/fnmatch/module.go` rewritten as a Go port
(option B), with a companion `module/fnmatch/module_test.go`.
Every function carries a `// CPython: Lib/fnmatch.py:NN` citation.
`translate()` is a line-for-line port of CPython `_translate` +
`_join_translated_parts` and produces **byte-identical** regex
output for the `(?s:...)\z` framing, atomic groups around interior
`STAR fixed` pairings, set-operation escaping, range-collapse, and
the `[!]` → `.` / `[]` → `(?!)` edge cases. Matching uses a
parallel Go-side glob interpreter (`compileGlob`, `matchGlob`,
`parseClass`) that mirrors the same bracket grammar.
`stdlibinit/registry.go` already had the inittab entry; no change
there.

**Deferred.** Option A (vendor `Lib/fnmatch.py` to
`stdlib/fnmatch.py`) was attempted first and abandoned because
four upstream dependencies are missing or insufficient in gopy:
(1) `import itertools` returns `ModuleNotFoundError` (no
`module/itertools/`); (2) `import posixpath` returns
`ModuleNotFoundError` (`module/os/` registers `posixpath` only as
an alias of `os.path`, but `fnmatch.py` does a plain top-level
`import posixpath`); (3) `os.path.normcase` is not exposed
(the os port lists `basename`, `dirname`, etc. but not `normcase`);
(4) the current `re` port wraps Go's RE2 and rejects two regex
constructs CPython's `translate()` always emits: `\Z`
(`re.error: invalid escape sequence`) and `(?>...)` atomic groups
(`invalid or unsupported Perl syntax`). Spec calls out the re full
port as task #510, out of scope here. When #510 ships, the
Go-side glob matcher can be deleted and `fnmatchcase` can
delegate to `re.compile(translate(pat)).match`, completing the
option-A migration.

**Gate.** Acceptance run all green:
`fnmatch('test_one.py', 'test*.py')` → `True`,
`fnmatchcase('Test.py', 'test*.py')` → `False`,
`translate('*.py')` → `(?s:.*\.py)\z`,
`filter([...], '*.py')` → `['a.py', 'c.py']`;
`go test ./...` clean with no regressions.

## types

**Surface.** Vendored `Lib/types.py` is now the active code path
via `try: from _types import *`. `_types` exposes the canonical
type singletons that `types.py` re-binds (`FunctionType`,
`MethodType`, `ModuleType`, `CodeType`, `MappingProxyType`,
`SimpleNamespace`, etc.). Five upstream names are deliberately
absent because gopy has no runtime equivalent yet:
`ClassMethodDescriptorType`, `MethodWrapperType`,
`WrapperDescriptorType`, `GenericAlias`, `UnionType`. They are
not exposed under `types.X` until the matching runtime ports
land. The `_f.__code__` / `type.__dict__` / `sys.implementation`
fallback path in `types.py` is dead code in gopy (CPython solves
this at upstream the same way), so the architecture matches
CPython rather than diverging.

**Location.** `stdlib/types.py` is a byte-equal vendor of CPython
3.14 `Lib/types.py` (sha256 `fcfdd1c5…`), recorded in
`stdlib/MANIFEST.txt`. `module/types/module.go` is rebuilt to be
the `_types` builtin (mirrors `Modules/_typesmodule.c`),
registered through `imp.AppendInittab("_types", …)` from
`stdlibinit/registry.go`. The runtime support that this port
exposed came along the way:
`objects/mapping_proxy.go` (new port of CPython mappingproxy from
`Objects/descrobject.c:1034`, with read-only enforcement, repr,
iter, contains, hash, richcompare),
`objects/namespace.go` (`TpNew` so `SimpleNamespace(a=1, b=2)`
works), `objects/property.go` (`TpNew` so `@property` decorators
inside `types.py` instantiate), `objects/module.go`
(`module.__dict__` attribute access returns the module's dict,
needed for `from x import *`),
`pythonrun/runstring.go` (`RunSimpleString` now stamps
`__name__ = "__main__"` on `-c` globals),
`vm/eval_import.go` (promotes `ErrModuleNotFound` to a typed
`ModuleNotFoundError` on the thread state so
`except ImportError:` matches),
`vm/eval_simple.go` (`exceptionMatches` consults `IsSubtype` so
`ModuleNotFoundError` is recognised as `ImportError`), and
`vm/eval_unwind.go` (`synthesizeException` maps `TypeError:` /
`ValueError:` / etc. message prefixes to typed `PyExc_*` classes
when bare Go errors unwind).

**Deferred.** Five `types.py` re-exports are missing on purpose
(see Surface). Follow-up tasks needed: port the three descriptor
types as part of a broader descriptor pass; port `GenericAlias`
(`Objects/genericaliasobject.c`) and `UnionType`
(`Objects/unionobject.c`). Separately, the bare-Go-error →
typed-exception promotion in `vm/eval_unwind.go` is currently
prefix-based on the error message; the proper fix is to route
every call site through `errors.SetString`, which is a larger
refactor and is outside the scope of #509.

**Gate.** Acceptance run green: `type(lambda: 0)` →
`<class 'function'>`; `SimpleNamespace(a=1, b=2)` → `1 2`;
`MappingProxyType({'a': 1})['a']` → `1`; `types.FunctionType`
resolves OK. `go test ./...` clean.

## functools

**Surface.** `_functools` ships the full C accelerator surface:
`PartialType` / `partial` with placeholder-aware merge,
`KeyObjectType` / `cmp_to_key` plus `keyobject_richcompare`,
`reduce` with seeded and unseeded paths and the empty-iterable
`TypeError`, `LruCacheWrapperType` with all three CPython wrapper
variants (uncached, infinite, bounded), the doubly-linked recency
list, `cache_info` / `cache_clear` / `__copy__` / `__deepcopy__`,
descriptor binding, and the `Placeholder` singleton. The Go-side
`module/functools/module.go` re-exports `_functools` and adds the
pure-Python wrappers: `wraps`, `update_wrapper`, `lru_cache`,
`cache`, `cached_property`, `total_ordering`, `singledispatch`,
`partialmethod`, plus a Go-side `_CacheInfo` named-tuple
stand-in.

**Location.** `module/_functools/module.go` is the 978-line port
of CPython 3.14 `Modules/_functoolsmodule.c`; every exported name
carries a `// CPython: Modules/_functoolsmodule.c:NN <func>`
citation. `module/functools/module.go` is the Go-native port of
`Lib/functools.py`. `stdlibinit/registry.go` blank-imports
`module/_functools` next to `module/functools`. `stdlib/functools.py`
is a byte-equal vendor of `Lib/functools.py` (1165 lines), recorded
in `stdlib/MANIFEST.txt`, but it is shadowed today because the
inittab entry wins over the path finder.

**Deferred.** Four follow-ups: (1) the vendored `stdlib/functools.py`
cannot load yet because `abc`, `operator`, `reprlib`, `_thread`
are not ported: once those land, delete the `module/functools`
inittab entry so the vendored `.py` takes over. (2) `total_ordering`
currently returns the class unchanged; the CPython behaviour of
filling in missing comparison methods needs a class-mutating
decorator path, which lands when gopy exposes the typeobject
mutation API. (3) `singledispatch` dispatches by exact type only;
the full MRO walk lands when `__mro__` traversal is wired into the
registry. (4) Test coverage relies on `go test ./...` which Go's
glob skips for `_`-prefixed directories, so `module/_functools` is
reached only via `module/functools`'s end-to-end tests.

**Gate.** All five primary acceptance gates green: `cmp_to_key`
returns a `KeyWrapper`; `sorted([3,1,2], key=cmp_to_key(...))` →
`[1, 2, 3]`; `partial(add, 3)(4)` → `7`;
`@lru_cache(maxsize=2)` decorated `f(3)` returns `6` with
`CacheInfo(hits=0, misses=1, maxsize=2, currsize=1)`;
`reduce(add, [1,2,3,4])` → `10`. Sixth gate (`gopy /tmp/test_simple.py`)
now advances past the `cmp_to_key` failure and lands in
`unittest.case`, where it hits the already-documented
`types.GenericAlias` gap from the types port.

## GenericAlias + UnionType

**Surface.** Full runtime representation of `list[int]`, `dict[str, int]`,
`tuple[int, ...]`, and PEP 604 `int | str`. `types.GenericAlias` and
`types.UnionType` are now real types accessible from Python. The
`__class_getitem__` hook is bound on the six canonical generic
builtins (`list`, `dict`, `tuple`, `set`, `frozenset`, `type`), so
subscripting them produces a `GenericAlias`. The `|` operator on
`type` produces a `UnionType`; `__or__` and `__ror__` are wired on
both `GenericAlias` and `UnionType` so chained constructions
(`int | str | bytes`) compose correctly. `union_repr`, `union_hash`,
`union_richcompare` (set-equality on args with order preserved),
`isinstance(x, int | str)` / `issubclass`, `__args__`, `__parameters__`,
`__origin__`, `__mro_entries__`, `__copy__`, `__deepcopy__`,
`__reduce__` all behave per CPython.

**Location.** `objects/generic_alias.go` (~440 lines, full port of
`Objects/genericaliasobject.c`), `objects/union_type.go` (~430 lines,
full port of `Objects/unionobject.c`), `objects/class_getitem.go`
(~65 lines, the `__class_getitem__` and `Number.Or` slot wiring).
Two entries added to `module/types/module.go` exposing the new
types under `_types`. `vm/eval_simple.go` extended with a
`typeSubscript` fallback at the tail of `getItem` so `cls[arg]`
reaches `__class_getitem__` when no direct `mp_subscript` slot
exists. Every Go helper carries a
`// CPython: Objects/<file>.c:NN <func>` citation. New public API:
`objects.GenericAliasType`, `objects.NewGenericAlias(origin, args)`,
`objects.UnionTypeType`, `objects.NewUnionType(args)`. Helpers
`unionTypeOr` and `typingTypeRepr` are package-private and shared
between the two files.

**Deferred.** Three items, all justified: (1) `TypeVar`, `ParamSpec`,
`TypeVarTuple` substitution short-circuits with CPython's "is not
a generic class" `TypeError` when the parameters tuple is empty.
Real substitution requires the `typing` C accelerator types, which
gopy does not yet ship; this matches the CPython behaviour for
`list[int][str]`-style re-parameterization on non-typing builtins.
(2) `__class_getitem__` is bound on the six canonical builtins
only; generator, coroutine, contextvar, etc. are not wired. Each
is a one-line `bindClassGetitem(T)` call when needed. (3) Union
flattening when one arg is itself a `UnionType` from a different
construction path is not special-cased; the common left-associative
`int | str | bytes` is covered by the existing `union_or` path.

**Gate.** All five primary gates green: `types.GenericAlias` resolves
to the class; `list[int]` prints `list[int]`; `int | str` prints
`int | str`; `x: list[int] = [1,2,3]; print(x)` prints `[1, 2, 3]`;
`go test ./...` clean. Sixth gate (`gopy /tmp/test_simple.py`)
advances past the `types.GenericAlias` failure and now fails at
`AttributeError: 'method' object has no attribute '__doc__'` inside
the unittest import chain: a separate bound-method descriptor
gap, outside the generics subsystem.

## dataclasses

**Surface.** Full public surface of `Lib/dataclasses.py` exposed
through a Go-native port: `dataclass`, `field`, `Field`, `fields`,
`asdict`, `astuple`, `replace`, `is_dataclass`, `MISSING`,
`KW_ONLY`, `InitVar`, `FrozenInstanceError`. The `@dataclass`
decorator handles `frozen=True` (and `frozen=True, kw_only=True`,
which is exactly the shape `_colorize` uses on five classes),
generates `__init__` from field declarations with defaults and
`field(default_factory=...)`, sets `Repr` and `Eq` on the resulting
class, and enforces frozen attribute writes through
`FrozenInstanceError`.

**Location.** `module/dataclasses/module.go` (~1161 lines).
`objects/descr.go` was extended with `TypeOwnDescrs` and
`DelTypeDescr` helpers so dataclass field discovery can introspect
a class's own descriptor table. `stdlibinit/registry.go` blank-imports
`module/dataclasses`. Every Go helper carries a
`// CPython: Lib/dataclasses.py:NN <func>` citation.

**Deferred.** Seven items, all reported with upstream line numbers
so follow-ups are precise. (1) `make_dataclass` raises
`NotImplementedError`; requires runtime synthesis of a new `Type`
from a field list (CPython `dataclasses.py:1635`). Not used by
`_colorize` or `unittest.mock`. (2) `__hash__` generation under
`eq=True frozen=True` (CPython `dataclasses.py:936-984`
`_hash_*`) and `unsafe_hash=True` not wired; the `Repr` / `Eq`
slots are set but `Hash` falls through. (3) Order comparisons
under `order=True` (CPython `dataclasses.py:1149` `_cmp_fn`) not
emitted. (4) `slots=True` and `weakref_slot=True` (CPython
`dataclasses.py:1337` `_add_slots`) parsed but the class is not
rebuilt with `__slots__`. (5) `asdict` / `astuple` do a shallow
`dict()` over top-level attrs; the recursive walk for nested
dataclasses (CPython `dataclasses.py:1507` `_asdict_inner`) is
not implemented. `_colorize` and `mock` do not nest. (6)
`dict_factory` / `tuple_factory` keyword arguments parsed but
ignored. (7) `InitVar` is exposed but `_get_field` integration
(CPython `dataclasses.py:825`) is not, so InitVar-typed fields
would be treated as regular fields.

**Gate.** All four primary gates green: `import dataclasses; print(dataclasses.dataclass)`
prints the decorator; `@dataclass class C: x:int=1; C(x=5).x` →
`5`; `@dataclass(frozen=True)` variant prints `5`; `go test ./...`
clean. The fifth gate ("delete `module/colorize/` and let the
vendored `stdlib/_colorize.py` win") **was not done** by design ;
the vendored file fails on its second line
(`from collections.abc import Callable, Iterator, Mapping`)
because `collections.abc` is not exposed, and would further need
subscriptable `Mapping[str, str]` and zero-arg `super()` to
complete its class definitions. The shim therefore stays in place
until those three runtime gaps land. The dataclasses port itself
was verified to handle the same `_colorize`-shape classes by
inlining the definitions and confirming construct, repr, eq, and
frozen-guard all work.

### Runtime gaps surfaced by this port

The dataclasses agent identified six runtime gaps that block a
clean vendor of `Lib/dataclasses.py` and adjacent stdlib modules.
These are separate follow-ups, not part of #522, but listed here
because they will keep coming up as later subsystems land:

1. **`__annotations__` not populated on class bodies.** The
   compiler records annotations in
   `compile/codegen_stmt_misc.go:151` (`visitAnnAssign`) into
   `DeferredAnnotations` but never emits the runtime store.
   `SETUP_ANNOTATIONS` exists as an opcode but is never emitted.
   This is why field discovery in `module/dataclasses` had to walk
   the descriptor table heuristically instead of reading
   `cls.__annotations__`. Fixing this unblocks a future vendor of
   `Lib/dataclasses.py`.
2. **Multi-line `def` inside `exec()` from a function-scope frame
   fails.** Top-level `exec("def f(x):\n  return x", g)` works;
   the same call from inside `def make(): ...` raises
   `parser: generated rule bodies not yet emitted`. This blocks
   the CPython `_FuncBuilder` codegen path that dataclasses,
   functools, and others rely on.
3. **`collections.abc` not exposed.** Required by the vendored
   `_colorize.py`. `module/collections` is a partial shim that
   publishes the dotted `collections` name only; it should either
   register a separate `collections.abc` inittab entry or attach
   an `abc` submodule attribute.
4. **Zero-arg `super()`** raises `RuntimeError: super(): no
   arguments (zero-arg form not yet supported)`. Required by
   `_colorize.ThemeSection.__post_init__`.
5. **`object.__setattr__` not exposed.** The documented workaround
   for writing through a frozen class is not available.
6. **`repr(c)` does not dispatch to user `__repr__`** on plain
   user classes (only when a class sets `cls.Repr` directly). The
   dataclass port works around this by setting `cls.Repr` from
   Go; user classes with a Python-level `__repr__` method still
   print the default `<C object at 0x...>` form.
