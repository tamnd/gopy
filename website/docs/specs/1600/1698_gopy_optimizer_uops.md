---
format: md
id: 1698_gopy_optimizer_uops
title: "1698. gopy Tier-2 uop interpreter and DSL"
sidebar_label: "1698. optimizer_uops"
sidebar_position: 1698
slug: /specs/1698-optimizer_uops
description: "v0.12 port of optimizer_bytecodes.c, optimizer_cases.c.h, pycore_uop_ids.h, and pycore_uop_metadata.h. The micro-op opcode table, per-uop metadata, the uop interpreter, and the DSL generator that produces uops_cases_gen.go."
---


## Goal

Port the micro-op layer that sits underneath the Tier-2 optimizer.
Three pieces ship together:

1. The uop ID table (`pycore_uop_ids.h`): one ID per micro-op,
   plus the macro-expansion mapping that says which uops a Tier-1
   opcode projects to.
2. The uop metadata table (`pycore_uop_metadata.h`): per-uop
   operand count, stack effect, refcount effect, flags. Read by
   the optimizer (spec 1697), the analysis pass (spec 1699), and
   the disassembler.
3. The uop interpreter (`optimizer_bytecodes.c` plus the
   generated `optimizer_cases.c.h`): the `case` body for each
   uop ID. Driven by a switch over the uop opcode like the
   Tier-1 dispatch loop.

The DSL generator that produces these tables lives at
`tools/uops_gen/`. It mirrors the existing
`tools/bytecodes_gen/` for Tier-1 and shares the same parser for
the inst / op / family / replicate constructs.

## Sources of truth

| CPython file                                | Lines | Target                              |
|---------------------------------------------|------:|-------------------------------------|
| `Python/optimizer_bytecodes.c`              |  1107 | Input to `tools/uops_gen/`          |
| `Python/optimizer_cases.c.h`                |  gen  | Reference output for the generator  |
| `Include/internal/pycore_uop_ids.h`         |   ~80 | `optimizer/uop_ids_gen.go`          |
| `Include/internal/pycore_uop_metadata.h`    |  ~600 | `optimizer/uop_meta_gen.go`         |
| `Tools/cases_generator/`                    |    *  | Reference for the Go generator      |

The CPython generator under `Tools/cases_generator/` is the
canonical implementation. The gopy generator is a Go reimplementation
that emits Go-shaped output; it shares no code with the Python one,
just the input grammar.

## Package layout

```
optimizer/
  uop_ids_gen.go      Generated uop ID constants
                      (pycore_uop_ids.h)
  uop_meta_gen.go     Generated per-uop metadata table:
                      operand count, stack delta, flags
                      (pycore_uop_metadata.h)
  uops_cases_gen.go   Generated case bodies: one Go function or
                      switch arm per uop ID, ported from
                      optimizer_bytecodes.c
  uops.go             The uop interpreter loop. Walks an executor's
                      trace, dispatches per uop, handles exits and
                      deopts, returns control to Tier-1 with the
                      right bytecode offset and stack state.
  uops_print.go       _PyUOpName, _PyUOpPrint
                      (optimizer.c:286-329) for dis.dis output

tools/uops_gen/
  main.go             Entry point. Reads the four CPython sources
                      and emits the three generated files above.
  parser.go           Shared input parser (with bytecodes_gen)
  emit_ids.go         Emits uop_ids_gen.go
  emit_meta.go        Emits uop_meta_gen.go
  emit_cases.go       Emits uops_cases_gen.go
```

## The uop ID table

`pycore_uop_ids.h` is a list of `_PyOpcode_<NAME>` constants in a
fixed order. The Tier-1 ID space and the Tier-2 ID space share
the same `uint16` namespace. Real Tier-2 uops live above
`MAX_REAL_OPCODE` (currently 256 + headroom).

Three classes of uop:

| Class            | Examples                                    | Semantics              |
|------------------|---------------------------------------------|------------------------|
| Side-effect free | `_LOAD_FAST`, `_LOAD_CONST`, `_STORE_FAST`  | Stack manipulation     |
| Guards           | `_GUARD_BOTH_INT`, `_GUARD_TYPE_VERSION`    | Bail to Tier-1 on miss |
| Effects          | `_BINARY_OP_ADD_INT`, `_PUSH_FRAME`         | Real work              |

Plus four meta-uops:

- `_NOP`: pad after analysis pass eliminates a uop.
- `_DEOPT`: explicit fall-back to Tier-1.
- `_JUMP_TO_TOP`: loop close in a projected trace.
- `_EXIT_TRACE`: graceful return from the trace to the bytecode
  offset stored in `target`.

The Go header reads:

```go
package optimizer

const (
    _LOAD_FAST                 uint16 = 300
    _LOAD_CONST                uint16 = 301
    _STORE_FAST                uint16 = 302
    _GUARD_BOTH_INT            uint16 = 303
    _BINARY_OP_ADD_INT         uint16 = 304
    // ... up to ~150 entries
    _DEOPT                     uint16 = 400
    _JUMP_TO_TOP               uint16 = 401
    _EXIT_TRACE                uint16 = 402
)
```

The exact numbers come out of the generator; gopy does not pin
them, but the names round-trip with CPython's `_Py_uop_names[]`
table for `dis.dis` parity.

## The uop metadata table

Every uop has metadata describing its shape. The Go layout:

```go
package optimizer

type UopMeta struct {
    Name        string
    Flags       uint32   // HAS_DEOPT_FLAG, HAS_ESCAPES_FLAG, ...
    StackEffect int8     // net items pushed (negative = popped)
    OperandSize uint8    // 0, 1, 2, or 4 bytes for the trailing operand
}

var UopMetaTable = [...]UopMeta{
    _LOAD_FAST:        {Name: "_LOAD_FAST", Flags: 0, StackEffect: 1, OperandSize: 0},
    _GUARD_BOTH_INT:   {Name: "_GUARD_BOTH_INT", Flags: HasDeoptFlag, StackEffect: 0, OperandSize: 0},
    _BINARY_OP_ADD_INT:{Name: "_BINARY_OP_ADD_INT", Flags: HasErrorFlag, StackEffect: -1, OperandSize: 0},
    // ...
}
```

The flags follow CPython's `pycore_uop_metadata.h:OP_FLAGS_*`:

- `HasErrorFlag`: the uop can raise.
- `HasDeoptFlag`: the uop can deopt to Tier-1.
- `HasEscapesFlag`: the uop can call into Python code.
- `HasExitFlag`: the uop can exit the trace cleanly.
- `HasJumpFlag`: the uop alters the trace iterator (`_JUMP_TO_TOP`).
- `HasErrorNoPopFlag`: error path does not pop the operands.

Spec 1699's analysis pass reads `Flags` to decide whether a uop
is safely removable.

## The DSL

`optimizer_bytecodes.c` is in the same DSL the Tier-1 bytecodes
file uses. A uop is declared like this:

```c
op(_BINARY_OP_ADD_INT, (left, right -- res)) {
    STAT_INC(BINARY_OP, hit);
    PyObject *res_o = _PyLong_Add((PyLongObject *)left, (PyLongObject *)right);
    PyStackRef_CLOSE_SPECIALIZED(left, _PyLong_ExactDealloc);
    PyStackRef_CLOSE_SPECIALIZED(right, _PyLong_ExactDealloc);
    ERROR_IF(res_o == NULL);
    res = PyStackRef_FromPyObjectSteal(res_o);
}
```

The leading parenthesised stack signature `(left, right -- res)`
declares: pop two items named `left` and `right`, push one named
`res`. The body is C with macros for stack manipulation, error
propagation, and deopt. The generator's job is to translate the
body into a Go function or switch arm that:

- Pops the named operands from the trace stack.
- Runs the body verbatim, with Go-equivalent macros
  (`_PyLong_Add` becomes `objects.LongAdd`, `PyStackRef_*` becomes
  the gopy stackref helpers).
- Pushes the named results.
- Routes `ERROR_IF` / `DEOPT_IF` to the right exit shape.

The generator under `tools/uops_gen/` mirrors
`tools/bytecodes_gen/` (existing Tier-1 generator) but emits Go
the optimizer expects, not the eval loop. The two generators
share `parser.go` and the macro-mapping table.

A `macro(...)` declaration combines several uops into one Tier-1
opcode expansion. Example:

```c
macro(BINARY_OP_ADD_INT) =
    _GUARD_BOTH_INT + _BINARY_OP_ADD_INT;
```

Trace projection (spec 1697) reads the `_PyOpcode_macro_expansion`
table the generator emits to know which uops to push for each
specialized Tier-1 opcode.

## The uop interpreter

`optimizer/uops.go` runs an `_PyExecutorObject`'s trace. The loop
shape is intentionally similar to Tier-1 dispatch:

```go
func RunTrace(ts *state.Thread, frame *frame.Frame, exec *ExecutorObject) (int, error) {
    ip := 0
    for {
        inst := exec.Trace[ip]
        switch inst.Opcode {
        case _LOAD_FAST:
            // generated body
        case _GUARD_BOTH_INT:
            // generated body, jumps to Deopt on miss
        // ... ~150 cases
        case _DEOPT:
            return DeoptToTier1(frame, inst.Target)
        case _EXIT_TRACE:
            return ExitTrace(frame, inst.Target)
        case _JUMP_TO_TOP:
            ip = 0
            continue
        }
        ip++
    }
}
```

Each `case` body is generated from the matching DSL block in
`optimizer_bytecodes.c`. The generator handles four idioms:

- `DEOPT_IF(cond)` becomes `if cond { return DeoptToTier1(...) }`.
- `ERROR_IF(cond)` becomes the Tier-1 error propagation path.
- `EXIT_IF(cond)` becomes a graceful trace exit.
- `STAT_INC(...)` becomes a no-op (gopy does not collect stats
  unless `-tags pystats` is on; the generator checks the tag).

The interpreter is *not* JIT'd. The Go runtime's compiler is the
only optimizer underneath us; we count on the switch to lower to
a jump table.

## Stack discipline

The uop interpreter shares the eval-loop stack with Tier-1. The
trace projection records absolute stack effects per uop; the
analysis pass (spec 1699) refines them with type info; the
interpreter reads `StackEffect` from `UopMetaTable` to keep the
stack pointer correct on entry and exit.

A subtlety: trace exits land in the middle of a Tier-1 opcode,
so the interpreter cannot simply jump back to the next bytecode
offset. The `target` field on each uop records the offset of the
*source* Tier-1 opcode. On `_DEOPT` / `_EXIT_TRACE` the
interpreter computes the resume offset from `target` plus the
matching deopt table entry.

## Gate

The uop interpreter has its own gate panel under
`v012test/uops_gate_test.go` independent of trace projection
(spec 1697 covers the projection gate):

1. **`_LOAD_FAST` / `_STORE_FAST` round-trip**. Hand-build a
   trace with two `_LOAD_FAST` uops feeding a `_STORE_FAST`,
   run on a frame with prepared locals, assert the destination
   slot has the right value.
2. **`_GUARD_BOTH_INT` deopt**. Hand-build a trace with the
   guard followed by `_BINARY_OP_ADD_INT`. Drive with int
   operands once (asserts `_BINARY_OP_ADD_INT` runs) and with
   a string operand once (asserts the guard takes the deopt
   branch).
3. **`_JUMP_TO_TOP`**. Hand-build a trace ending in `_JUMP_TO_TOP`
   and a counter that breaks out on the third iteration; assert
   the loop runs three times then exits cleanly.
4. **DSL generator regression**. `tools/uops_gen/` runs in
   `go generate` mode and the regenerated
   `optimizer/uops_cases_gen.go` is byte-equal to the checked-in
   copy.
