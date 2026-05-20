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

- `CALL_INTRINSIC_*` family. Wraps gopy's intrinsics table.
- `GETITEM` / `SETITEM` family. Gopy spells these via
  `objects.SequenceGetItem`, which has subtle error-path
  semantics that the translator cannot infer.
- Frame-resume helpers. Generator/coroutine bookkeeping that
  gopy handles via `evalState.resumeGenerator`.
- Anything in `_PyFrame_*`. Gopy's frame layout differs in cell
  allocation and `__class__` slot handling, so direct
  translations would silently miscompile.

## Tier-2 emitter audit (spec 1713 / spec 1714 Phase 8 bucket B1)

Inventory of every uppercase token currently surviving into the
`optimizer/uops_stubs_gen.go` body comments after Tier2Emitter
runs (271 viable uops, 179 distinct uppercase tokens). Each token
is classified by who emits it and what the Go-target binding
should be once Phase 8 lights up.

### Class A. Generator-rewritten (never appears in body output)

These are recognized by `Tier2Emitter._replacers` or its
inherited base `Emitter._replacers` (see
`tools/cases_generator/generators_common.py:107-217` and
`tools/cases_generator/tier2_generator.py:61-133`). The C target
already swaps them for the rewrite shown in the third column;
the Go target needs the Go equivalent in the fourth column. Rows
with a `gopy mapping` are READY; rows with `pending` need a new
entry in `MACRO_BINDINGS` before B3 (Tier2GoEmitter draft) can
land.

| Macro | Arity | C rewrite (Tier2Emitter) | Go-target rewrite |
|-------|-------|--------------------------|-------------------|
| `DEOPT_IF(cond, _)` | 2 | `if (cond) { UOP_STAT_INC(uopcode, miss); JUMP_TO_JUMP_TARGET(); }` | `if cond { return s.jumpToJumpTarget(inst) }` (pending; current MACRO_BINDINGS spelling is `e.deoptTo(parent)`. Phase 5 spelling carries a `parent` opcode that Tier-2 has erased: the Tier2GoEmitter must drop the second arg and route to the Tier-2 dispatcher's existing `jumpToJumpTarget` helper.) |
| `EXIT_IF(cond)` | 1 | identical to `DEOPT_IF` | identical to `DEOPT_IF` (Tier2Emitter aliases the two; Tier2GoEmitter should match.) |
| `ERROR_IF(cond, label)` | 2 | `if (cond) { JUMP_TO_ERROR(); }` | `if cond { return StatusError }` (pending; current binding emits `goto label` which is Tier-1 vocabulary. Tier-2 already collapses every label to the `JUMP_TO_ERROR()` sentinel.) |
| `ERROR_NO_POP()` | 0 | `JUMP_TO_ERROR();` | `return StatusError` (pending) |
| `oparg & 1` in `_FOO_0` / `_FOO_1` super-variants | n/a | rewritten to literal `0` / `1` | identical (no Go binding needed; Tier2GoEmitter inherits this from the base Tier2Emitter once subclassed.) |
| `DECREF_INPUTS()` | 0 | input stackref close + flush | `_ = 0` (GC no-op; binding entry: `DECREF_INPUTS`. pending) |
| `INPUTS_DEAD()` | 0 | mark inputs dead in storage | no-op (Go GC) (pending binding) |
| `DEAD(name)` | 1 | mark `name` dead in storage | no-op (pending binding) |
| `SYNC_SP()` | 0 | flush + clear stack | `e.f.StackTop = e.sp()` (pending; gopy keeps the stack pointer in `Frame.StackTop`, so a write-back is enough) |
| `SAVE_STACK()` | 0 | spill stackref locals to memory | no-op for gopy (Go stack already lives in `frame.Frame`; pending binding) |
| `RELOAD_STACK()` | 0 | reload spilled locals | no-op (pending binding) |
| `PyStackRef_CLOSE_SPECIALIZED(r, dealloc)` | 2 | conditional `PyStackRef_CLOSE` + dealloc dispatch | `_ = 0` (GC no-op; gopy never specializes the close path) |
| `PyStackRef_AsPyObjectSteal(r)` | 1 | borrow + null-out source | `r.AsObject()` (gopy: borrowing and stealing are indistinguishable under GC; pending binding) |
| `DISPATCH()` | 0 | yield to outer dispatch loop | unreachable for Tier-2 (Tier-1 only; emitter currently passes it through. Tier2GoEmitter should reject with "macro X out of scope for Tier-2".) |
| `INSTRUCTION_SIZE` | 0 | replaced with `uop.instruction_size` literal | identical (literal; no binding needed) |
| `stack_pointer` | identifier | spill-state validation only | `e.sp()` accessor (READY in MACRO_BINDINGS as `stack_pointer` not present; pending) |

Action items implied:
- `MACRO_BINDINGS` must grow rows for `DECREF_INPUTS`, `INPUTS_DEAD`,
  `DEAD`, `SYNC_SP`, `SAVE_STACK`, `RELOAD_STACK`,
  `PyStackRef_CLOSE_SPECIALIZED`, `PyStackRef_AsPyObjectSteal`,
  `stack_pointer`. Today only `DEOPT_IF` / `ERROR_IF` from this
  list are present, and their spellings target Tier-1 not Tier-2.
- The Tier-2 spellings of `DEOPT_IF` and `ERROR_IF` differ from
  the Tier-1 spellings already in `MACRO_BINDINGS`; Tier2GoEmitter
  must override these two before reusing the table.

### Class B. Body-surviving constants (need direct Go bindings)

These survive Tier2Emitter unchanged and appear in the generated
output, so the body translator (Phase 8) must lower them. Today
the comment block reproduces them verbatim; once Tier2GoEmitter
takes over, each row is a `MACRO_BINDINGS` entry.

| Macro | Arity | Where it comes from | Go target |
|-------|-------|---------------------|-----------|
| `CURRENT_OPARG()` | 0 | `Python/ceval_macros.h` | `s.Oparg` (already populated by the dispatcher; see `optimizer/uops.go:99-101`) |
| `CURRENT_OPERAND0()` | 0 | `Python/ceval_macros.h` | `inst.Operand0` |
| `CURRENT_OPERAND1()` | 0 | `Python/ceval_macros.h` | `inst.Operand1` |
| `JUMP_TO_ERROR()` | 0 | `Python/ceval.c:1335` jump_to_error_target | `return StatusError` |
| `JUMP_TO_JUMP_TARGET()` | 0 | `Python/ceval.c:1350` jump_to_jump_target | `return s.jumpToJumpTarget(inst)` (already exists; `optimizer/uops.go:141-144`) |
| `WITHIN_STACK_BOUNDS()` | 0 | `Python/ceval_macros.h` debug assert | always `true` for gopy (frame stack is Go-slice; no overflow possible at this layer) |
| `STACK_LEVEL()` | 0 | `Python/ceval_macros.h` | `e.sp()` |
| `LOAD_IP(off)` | 1 | `Python/ceval_macros.h` | `e.f.InstrPtr = e.f.InstrPtr + off` (pending; only appears in `_SET_IP` and unwinds) |
| `LOAD_SP()` | 0 | `Python/ceval_macros.h` | already implicit in gopy (`e.f.StackTop`) |
| `SKIP_OVER(n)` | 1 | `Python/ceval_macros.h` | `e.f.InstrPtr += n` |
| `STAT_INC(opcode, kind)` | 2 | `Include/internal/pycore_stats.h` | `_ = 0` (gopy has no Tier-2 stats counter today) |
| `UOP_STAT_INC(uop, kind)` | 2 | `Include/internal/pycore_stats.h` | `_ = 0` |
| `CALL_STAT_INC(kind)` | 1 | `Include/internal/pycore_stats.h` | `_ = 0` |
| `LLTRACE_RESUME_FRAME()` | 0 | LLTRACE debug only | `_ = 0` |
| `LOCK_OBJECT(o)` / `UNLOCK_OBJECT(o)` | 1 | free-threaded build only | `_ = 0` (gopy has no free-threaded build mode) |
| `QSBR_QUIESCENT_STATE(...)` | 1 | free-threaded build only | `_ = 0` |
| `FT_ATOMIC_LOAD_*` / `FT_ATOMIC_STORE_*` | n | free-threaded build only | direct load/store (no atomicity needed in gopy GC model) |
| `GETLOCAL(i)` | 1 | `Python/ceval_macros.h` | `e.localAt(i)` (already READY in `MACRO_BINDINGS`) |
| `SETLOCAL(i, v)` | 2 | `Python/ceval_macros.h` | `e.setLocal(i, v)` (already READY) |
| `GETITEM(co, i)` | 2 | `Python/ceval_macros.h` | `(co)[i]` for co_consts / co_names; flagged OUT OF SCOPE in this file's earlier section. Tier2GoEmitter must keep the rejection path. |

### Class C. Family / opcode enum identifiers (not macros)

The remaining uppercase tokens are CPython opcode / family enum
constants (`BINARY_OP`, `CALL`, `LOAD_FAST`, ...). They appear in
two places only:

- `UOP_STAT_INC(uopcode, ...)` and `CALL_STAT_INC(...)` arguments.
  Tier2Emitter rewrites these as no-ops on the Go side (Class A),
  so the embedded family name disappears with the call.
- The `inst.family` argument the base `deopt_if` passes when it
  emits `UPDATE_MISS_STATS(family)`. Tier2Emitter overrides
  `deopt_if` to drop the family argument entirely (it has no
  meaning in a Tier-2 trace), so the family name does not survive
  to the output.

Conclusion: Class C tokens are not part of the body translator's
input alphabet for Tier-2. Tier2GoEmitter does not need bindings
for them.

### Class D. Tokens that gate "stays hand-rolled" (~16 uops)

Per `tools/cases_generator/SUBSET.md` policy, a uop whose body
references any macro family in the "Out of scope" section above
is opted out of automatic translation. The Tier-2 body audit
identifies these distinct-uop counts (verified by `awk` over
`optimizer/uops_stubs_gen.go`):

- `CALL_INTRINSIC_1` / `CALL_INTRINSIC_2`: 3 uops
  (`_CALL_INTRINSIC_1`, `_CALL_INTRINSIC_2`, `_CALL_ISINSTANCE`).
- `STACKREFS_TO_PYOBJECTS` / `STACKREFS_TO_PYOBJECTS_CLEANUP` /
  `CONVERSION_FAILED`: 9 uops, the BUILD / CALL_BUILTIN /
  CALL_KW_NON_PY / CALL_METHOD_DESCRIPTOR_FAST family that
  re-materializes a `PyObject*` array from stackrefs before
  handing off to a C helper.
- Generator-bound helpers (`RESUME_AFTER_YIELD_FROM`,
  `RETURN_GENERATOR`, `END_SEND`, `YIELD_VALUE`): 4 uops; gopy
  spells these via `evalState.resumeGenerator`.

`_PyFrame_GetStackPointer` and `_PyFrame_SetStackPointer` show up
in 152 distinct uops, but these are **not** Class D. They are
the emitter's escaping-call save/reload pattern (emitted by
`Emitter._emit_stmt` via `emit_save` / `emit_reload`, see
`tools/cases_generator/generators_common.py:401-435`). The Go
emitter overrides those two methods to elide the spill (gopy's
stack pointer lives in `frame.Frame.StackTop` directly), so the
sequence vanishes from the Go output. Treat them as Class A.

Tier2GoEmitter does not need bindings for Class D tokens; it
needs a rejection path that returns "unbound macro" and falls
through to the existing hand-rolled stub.

### Sizing the work for B2 / B3

- Class A bindings to add to `MACRO_BINDINGS`: 9 new rows
  (`DECREF_INPUTS`, `INPUTS_DEAD`, `DEAD`, `SYNC_SP`, `SAVE_STACK`,
  `RELOAD_STACK`, `PyStackRef_CLOSE_SPECIALIZED`,
  `PyStackRef_AsPyObjectSteal`, `stack_pointer`).
- Class A bindings to override for Tier-2 spelling: 2 rows
  (`DEOPT_IF`, `ERROR_IF`).
- Class A emitter-method overrides (not table rows):
  `emit_save` / `emit_reload` collapse to no-ops; `goto_error`
  collapses to `return StatusError`.
- Class B bindings to add: 16 new rows (the `CURRENT_*`, `JUMP_*`,
  `STAT_*`, `LOCK_*`, `FT_*`, `LL*` families).
- Class C: 0 (handled by Tier2Emitter's rewrites).
- Class D: rejection-path coverage in Tier2GoEmitter; ~16 uops
  stay hand-rolled.

Expected Tier-2 auto-translation reach after B2 / B3 land:
271 viable uops minus 16 Class D minus the existing 5 hand-ported
(`_DEOPT`, `_EXIT_TRACE`, `_ERROR_POP_N`, `_START_EXECUTOR`,
`_JUMP_TO_TOP`) leaves ~250 uops in scope for the body
translator, against the spec 1714 Phase 9 target of "~270 of ~285
uops fully generated". The gap is consistent with Phase 9.
