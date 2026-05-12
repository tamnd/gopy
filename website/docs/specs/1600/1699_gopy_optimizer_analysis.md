---
format: md
id: 1699_gopy_optimizer_analysis
title: "1699. gopy Tier-2 abstract interpretation"
sidebar_label: "1699. optimizer_analysis"
sidebar_position: 1699
slug: /specs/1699-optimizer_analysis
description: "v0.12 port of optimizer_analysis.c and optimizer_symbols.c. The abstract interpretation pass that runs over a freshly projected uop trace, the JitOptSymbol lattice it operates on, and the guard-elimination passes."
---


## Goal

Port the abstract interpreter that runs over a freshly projected
uop trace and prunes redundant guards / folds known constants.
Two pieces ship together:

1. The symbolic lattice (`optimizer_symbols.c`): the type that
   represents "what we know about a stack slot" through a trace.
   Five abstract states (top, type, type+version, type+const,
   bottom) plus the join / meet operations.
2. The analysis pass (`optimizer_analysis.c`): a forward
   abstract execution of every uop in the trace that updates the
   symbolic stack at each step and rewrites uops to lighter
   variants when the symbolic state proves a guard redundant or
   reveals a constant operand.

The output is the same `[]UopInstruction` buffer the projection
emitted (spec 1697), with some uops rewritten to `_NOP` and some
operands replaced with their constant value. Spec 1697's
executor-allocation step then copies this post-analysis buffer
into the executor's immutable trace.

## Sources of truth

| CPython file                                | Lines | Target                              |
|---------------------------------------------|------:|-------------------------------------|
| `Python/optimizer_analysis.c`               |   656 | `optimizer/analysis.go`             |
| `Python/optimizer_symbols.c`                |   880 | `optimizer/symbols.go`              |
| `Include/internal/pycore_optimizer.h`       |   313 | `optimizer/types.go` (shared with 1697) |
| `Lib/test/test_capi/test_opt_uops.py`       |  data | gate fixtures for analysis output   |

## Package layout

Two new files under `optimizer/`:

```
optimizer/
  symbols.go      JitOptSymbol type, sym_new, make_const,
                  set_type, set_const, set_null, get_const,
                  is_bottom, is_const, etc.
                  (optimizer_symbols.c:54-880)
  analysis.go     optimize_uops main loop, the per-uop
                  abstract-effect function, remove_globals,
                  remove_unneeded_uops, plus the
                  globals/type-watcher hooks
                  (optimizer_analysis.c:54-656)
```

Both files are pure ports of their CPython counterparts; the
public surface from `pycore_optimizer.h` is the `optimizer.Analyze(...)`
entry point that takes a uop buffer and a frame and returns the
rewritten buffer.

## The symbolic lattice

`JitOptSymbol` represents what is known about a single value at a
program point. Five states:

| State                | Means                                        |
|----------------------|----------------------------------------------|
| `top` / unknown      | Anything. Default at trace entry.            |
| `not null`           | Definitely not Python `None`.                |
| `type T`             | Definitely an instance of type `T`.          |
| `type T, version v`  | Type `T` whose `tp_version_tag` is `v`.      |
| `const C`            | The exact Python value `C` (a `*Object`).    |
| `null`               | Definitely Python `None`.                    |
| `bottom`             | Unreachable; trace ends here.                |

The Go type matches CPython byte for byte:

```go
type JitOptSymbol struct {
    Tag       uint8       // SYM_TYPE_* enum
    Bits      uint8       // flags: known not-null, known null, ...
    TypeVer   uint32      // tp_version_tag if Tag == SymTypeVersion
    Type      *objects.Type
    Const     objects.Object
}
```

The lattice operations are straightforward:

- `Top()`: the top element. Used for trace-entry stack slots.
- `Bottom(ctx)`: marks the symbol's owner unreachable. Triggers
  the analysis pass's "trace ends here" branch.
- `Const(ctx, val)`: the constant element.
- `Type(ctx, t)`: a type-only element.
- `Join(a, b)`: least upper bound. Joining `const 5` and
  `const 6` yields `type int`; joining different types yields
  top.
- `Meet(a, b)`: greatest lower bound. Used to apply guard
  results: `_GUARD_BOTH_INT` meets the input symbol with
  `type int`.

The `JitOptContext` carries the per-trace symbol arena. CPython
caps it at `TY_ARENA_SIZE = UOP_MAX_TRACE_LENGTH * 5`. gopy
mirrors that bound.

## The analysis pass

`optimize_uops` is the per-trace driver. Pseudo-code:

```go
func OptimizeUops(frame *frame.Frame, buf []UopInstruction) error {
    ctx := newAnalysisContext(frame, len(buf))
    for i := range buf {
        inst := &buf[i]
        switch inst.Opcode {
        case _LOAD_FAST:
            ctx.Stack = append(ctx.Stack, ctx.Locals[inst.Oparg])
        case _LOAD_CONST:
            sym := SymConst(ctx, frame.Code.Consts[inst.Oparg])
            ctx.Stack = append(ctx.Stack, sym)
        case _GUARD_BOTH_INT:
            // see "Guard elimination" below
        case _BINARY_OP_ADD_INT:
            // see "Constant folding" below
        // ... per-uop cases
        }
        if ctx.Bottom { return nil }   // unreachable; trace ends
    }
    removeUnneededUops(buf)
    return nil
}
```

The pass is forward, single-pass, no fixpoint. CPython relies on
the trace being acyclic (`_JUMP_TO_TOP` terminates the trace; the
loop body is unrolled once per iteration in projection).

### Guard elimination

`_GUARD_BOTH_INT` is the canonical guard. Pre-analysis it always
runs. Post-analysis the pass deletes the guard if the symbolic
state already proves both operands are int:

```go
case _GUARD_BOTH_INT:
    a := ctx.Stack[len(ctx.Stack)-2]
    b := ctx.Stack[len(ctx.Stack)-1]
    if symIsType(a, intType) && symIsType(b, intType) {
        inst.Opcode = _NOP                  // delete the guard
        continue
    }
    a = SymMeetType(ctx, a, intType)
    b = SymMeetType(ctx, b, intType)
    ctx.Stack[len(ctx.Stack)-2] = a
    ctx.Stack[len(ctx.Stack)-1] = b
```

`_NOP` is later compacted out by `removeUnneededUops`. The same
pattern applies to `_GUARD_TYPE_VERSION`, `_GUARD_NOS_INT`,
`_GUARD_KEYS_VERSION`, etc.

### Constant folding

`_BINARY_OP_ADD_INT` folds when both operands are
const-symbolic:

```go
case _BINARY_OP_ADD_INT:
    a := ctx.Stack[len(ctx.Stack)-2]
    b := ctx.Stack[len(ctx.Stack)-1]
    if symIsConst(a) && symIsConst(b) {
        sum := objects.LongAdd(symGetConst(a), symGetConst(b))
        inst.Opcode = _LOAD_CONST_INLINE_BORROW
        inst.Operand = uint64(uintptr(unsafe.Pointer(sum)))
        ctx.Stack = ctx.Stack[:len(ctx.Stack)-1]
        ctx.Stack[len(ctx.Stack)-1] = SymConst(ctx, sum)
        continue
    }
    // unfolded: pop two, push not-null int
    ctx.Stack = ctx.Stack[:len(ctx.Stack)-1]
    ctx.Stack[len(ctx.Stack)-1] = SymType(ctx, intType)
```

The `_LOAD_CONST_INLINE_BORROW` is a uop that holds a pre-allocated
constant directly in the trace. It carries no refcount churn.

### Globals optimization

`remove_globals` is the analysis pass's special case for
`LOAD_GLOBAL_*`. When it can prove that the globals dict has not
mutated since trace projection (the globals watcher is armed),
the pass rewrites `LOAD_GLOBAL_MODULE` to `_LOAD_CONST_INLINE_BORROW`
with the resolved value baked in.

The watcher state lives on `monitor.InterpState` (already there
from v0.11) and is keyed by the dict pointer. The analysis pass
queries `get_mutations(globals)`; if it equals the trace's
recorded snapshot, the globals load can be folded.

## Watchers

Two watchers feed the analysis pass and the executor invalidation
path (spec 1697):

- **Globals watcher**. `globals_watcher_callback` (port
  `optimizer_analysis.c:75`). Bumps the per-dict mutation
  counter on every set / delete. The analysis pass reads this
  counter. The invalidation path walks executors whose bloom
  filter matches the dict and unlinks them.
- **Type watcher**. `type_watcher_callback` (port
  `optimizer_analysis.c:87`). Fires on type or MRO mutation.
  Same fan-out shape as the globals watcher, keyed by type.

Both watchers register through the existing v0.11 watcher
infrastructure (`monitor` + `objects/version.go`); the new code
adds the callback bodies, not new infrastructure.

## Pass ordering

`_Py_uop_analyze_and_optimize` (port `optimizer_analysis.c:626`)
sequences:

1. `remove_globals(...)` if the globals watcher is armed.
2. `optimize_uops(...)`: the main forward abstract-execution
   pass.
3. `remove_unneeded_uops(...)`: compact `_NOP` rows out of the
   buffer.

Each pass mutates the buffer in place. The executor
(spec 1697) copies the post-pass buffer when it allocates.

## Gate

`v012test/analysis_gate_test.go` covers four scenarios:

1. **Const fold**. Hand-build a trace `_LOAD_CONST(2)
   _LOAD_CONST(3) _BINARY_OP_ADD_INT`, run the analysis pass,
   assert the resulting buffer holds a single
   `_LOAD_CONST_INLINE_BORROW` with operand pointing at
   `int(5)`.
2. **Guard removal**. Trace `_LOAD_CONST(int 2) _LOAD_CONST(int 3)
   _GUARD_BOTH_INT _BINARY_OP_ADD_INT`. After analysis the
   guard is `_NOP` and `_BINARY_OP_ADD_INT` survives.
3. **Type narrowing**. Trace `_LOAD_FAST(0) _LOAD_FAST(1)
   _GUARD_BOTH_INT _BINARY_OP_ADD_INT _GUARD_BOTH_INT`. Locals
   are top at entry; the first guard narrows them to int; the
   second guard becomes `_NOP`.
4. **Globals folding**. Build a frame with `globals = {"x": 5}`,
   trace `_LOAD_GLOBAL_MODULE("x")`, run analysis with the
   globals watcher armed and the mutation counter unchanged;
   assert the load was rewritten to `_LOAD_CONST_INLINE_BORROW`
   pointing at `5`.
