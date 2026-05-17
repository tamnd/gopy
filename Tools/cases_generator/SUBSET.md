# Go body translator subset

This file tracks which constructs the Phase 8 body translator
will accept, and which it will reject (forcing a hand-written
opcode body). It is updated every time `MACRO_BINDINGS` in
`go_generators_common.py` grows or shrinks.

## In scope (READY bindings — bound to gopy spellings)

| Macro | Arity | Go spelling |
|-------|-------|-------------|
| `PEEK(n)` | 1 | `e.peek(n)` |
| `POKE(n, v)` | 2 | `e.poke(n, v)` |
| `TOP()` | 0 | `e.peek(0)` |
| `SECOND()` | 0 | `e.peek(1)` |
| `STACK_GROW(n)` | 1 | `e.f.StackTop += n` |
| `STACK_SHRINK(n)` | 1 | `e.f.StackTop -= n` |
| `GETLOCAL(i)` | 1 | `e.localAt(i)` |
| `SETLOCAL(i, v)` | 2 | `e.setLocal(i, v)` |
| `JUMPBY(n)` | 1 | `e.jumpBy(n)` |
| `JUMPTO(t)` | 1 | `e.f.InstrPtr = t` |
| `PyStackRef_FromPyObjectNew(o)` | 1 | `stackref.FromObjectNew(o)` |
| `PyStackRef_AsPyObjectBorrow(r)` | 1 | `r.AsObject()` |
| `INSTRUCTION_SIZE` | 0 | `2` (codeunits per opcode) |
| `NEXTOPARG()` | 0 | `e.fetch()` |

## In scope but staged (spelling firms up in Phase 5)

| Macro | Arity | Provisional Go spelling | Notes |
|-------|-------|-------------------------|-------|
| `DEOPT_IF(cond, parent)` | 2 | `if cond { return e.deoptTo(parent) }` | Phase 5 will replace the `return` with the dispatch shim; the cond itself stays as-is. |
| `ERROR_IF(cond, label)` | 2 | `if cond { goto label }` | Phase 8 rewrites label targets into structured `return ..., err`. |

## GC no-ops (Go has no refcount)

`Py_INCREF`, `Py_DECREF`, `Py_XDECREF`, `PyStackRef_CLOSE` emit
an unobtrusive `_ = 0` comment statement so the body translator's
line counter stays accurate. gofmt later collapses these into
blank lines.

## Out of scope (forces hand-written op body)

Any macro not listed above. The translator must error out with
"unbound macro: X" so the operator stays hand-rolled and Phase 9
gets a real picture of how many opcodes resist translation.

The most common offenders, expected to stay hand-rolled for the
forseeable future:

- `CALL_INTRINSIC_*` family — wraps gopy's intrinsics table.
- `GETITEM` / `SETITEM` family — gopy spells these via
  `objects.SequenceGetItem`, which has subtle error-path
  semantics that the translator cannot infer.
- Frame-resume helpers — generator/coroutine bookkeeping that
  gopy handles via `evalState.resumeGenerator`.
- Anything in `_PyFrame_*` — gopy's frame layout differs in cell
  allocation and `__class__` slot handling, so direct
  translations would silently miscompile.
