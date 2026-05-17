---
format: md
id: 1715_flowgraph_cfg_port
title: "1715. Structural CFG port of Python/flowgraph.c"
sidebar_label: "1715. flowgraph CFG port"
sidebar_position: 1715
slug: /specs/1715-flowgraph-cfg-port
description: "Replace gopy's flat-instruction-sequence substrate for flowgraph passes with a faithful port of CPython's basicblock / cfg_builder graph, then re-port every pass in Python/flowgraph.c to operate on that graph. Removes the structural impedance mismatch that has forced every optimization pass shipped under spec 1713 to be reimplemented against a different data model than its CPython source."
---

## Ground rule

Same rule as 1704 / 1705 / 1708 / 1712 / 1713. Full subsystem
port, function by function, every `// CPython:` citation carries
file plus line. No keeping the flat-sequence shim alongside the
graph "for now". The graph lands, the passes move onto it, the
old flat-sequence pass file gets deleted.

## Why this exists

Spec 1713 walks `Python/flowgraph.c` top to bottom and ports each
optimization pass into `compile/flowgraph_passes.go`. Every
ported function pays the same tax: CPython's pass reads and
rewrites a doubly-linked list of `basicblock` records (each block
holds an array of `cfg_instr`, plus `b_next`, `b_predecessors`,
`b_iused`, `b_startdepth`, etc.). gopy's pass instead walks a
flat `[]Instr` slice with embedded `LABEL` pseudo-ops and target
indices. The two data models disagree on:

- block boundaries (gopy: scan for LABEL; CPython: `b_next`)
- predecessor counts (CPython tracks `b_predecessors`; gopy
  recomputes from a linear scan on every pass)
- reachability (CPython: walk `b_next` from entry; gopy: dead-code
  pass marks instructions via a side `[]bool`)
- jump targets (CPython: `i_target *basicblock`; gopy: integer
  index into the flat sequence resolved via `ApplyLabelMap`)
- exit edges (CPython: every block has an explicit `b_next`
  fallthrough plus optional terminator; gopy: implicit via slice
  order)

Each ported pass therefore reads the CPython source, then
*translates* it onto the flat sequence. That translation is the
exact site where bugs creep in: `remove_unused_consts` already
needed a custom "are we inside a tuple body" heuristic that has
no analogue in CPython; `optimize_lists_and_sets` had to special
case the LABEL pseudo-op; the upcoming `inline_small_or_no_lineno_blocks`,
`optimize_load_fast`, and `resolve_line_numbers` ports are all
graph-shaped and will fight the flat representation harder than
the passes shipped so far.

The cost compounds. 1713 wants byte-equality with CPython; every
divergence becomes a forensic dig where the answer is "we ported
the algorithm but the substrate doesn't model what the algorithm
assumes". Better to fix the substrate once.

## Goal

Port `Python/flowgraph.c`'s data model and every pass that
operates on it, so the file maps 1:1 onto gopy:

```
Python/flowgraph.c        → compile/flowgraph.go
                            compile/flowgraph_cfg.go      (basicblock, cfg_builder)
                            compile/flowgraph_passes.go   (every pass, ported onto cfg_builder)
                            compile/flowgraph_stack.go    (stackdepth pass)
                            compile/flowgraph_locals.go   (optimize_load_fast + helpers)
                            compile/flowgraph_assemble.go (cfg → instruction sequence handoff)
```

Spec done = `compile/flowgraph_passes.go` no longer references
`[]Instr` directly; every pass takes `*cfgBuilder` and the file's
function list matches `Python/flowgraph.c`'s function list.

## Phases

| Phase | Scope                                                                                  | Status      | Commit    |
| ----- | -------------------------------------------------------------------------------------- | ----------- | --------- |
| 1     | `basicblock` + `cfgBuilder` types and constructors                                     | done        | `ebae0b1` |
| 2     | `_PyCfg_FromInstructionSequence` bridge                                                | done        | `c5485af` |
| 3     | Re-port every pass in `flowgraph_passes.go` onto `*cfgBuilder`                         | in progress | `fb9d104` |
| 4     | `stackdepth` and `optimize_load_fast` onto the graph                                   | pending     | —         |
| 5     | `_PyCfg_ToInstructionSequence` bridge                                                  | pending     | —         |
| 6     | Delete flat-sequence pass shim; `flowgraph_passes.go` matches `Python/flowgraph.c` 1:1 | pending     | —         |

### Phase 1. Graph substrate

Port the data model from `Python/flowgraph.c:60-220` (`cfgInstr`,
`basicblock`, `cfgBuilder`) plus the constructors / mutators
`cfg_builder_init`, `cfg_builder_check`, `cfg_builder_new_block`,
`cfg_builder_use_label`, `cfg_builder_use_next_block`,
`basicblock_addop`, `basicblock_add_jump`.

### Phase 2. Build the graph from the instruction sequence

Port `_PyCfg_FromInstructionSequence` (flowgraph.c:3923). gopy's
codegen still emits an `instruction_sequence`; the flowgraph
passes start by folding that sequence into a `cfgBuilder`. Mirrors
CPython's boundary between codegen and flowgraph optimization.

### Phase 3. Port every pass onto the graph

Re-port every function already shipped under 1713 to take
`*cfgBuilder` instead of `[]Instr`, in source order. Drop the
flat-sequence versions as each ported pass lands.

| Pass                               | CPython              | Status  | Commit    |
| ---------------------------------- | -------------------- | ------- | --------- |
| `remove_redundant_nops`            | `flowgraph.c:1104`   | done    | `2d4f82a` |
| `normalize_jumps`                  | `flowgraph.c:590`    | done    | `58075c7` |
| `remove_redundant_jumps`           | `flowgraph.c:1159`   | done    | `fb9d104` |
| `remove_unreachable`               | `flowgraph.c:996`    | done    | `fb9d104` |
| `inline_small_or_no_lineno_blocks` | `flowgraph.c:1210`   | pending | —         |
| `optimize_basic_block`             | `flowgraph.c:1718`   | pending | —         |
| `resolve_line_numbers`             | `flowgraph.c:2823`   | pending | —         |
| `duplicate_exits_without_lineno`   | `flowgraph.c:3563`   | done    | `2f40735` |
| `propagate_line_numbers`           | `flowgraph.c:3616`   | done    | `c653483` |
| `convert_pseudo_ops`               | `flowgraph.c:2911`   | pending | —         |
| `_PyCfg_OptimizeCodeUnit`          | `flowgraph.c:2997`   | pending | —         |

### Phase 4. Stackdepth + locals

- `stackdepth` (flowgraph.c:1352) onto the graph; replaces gopy's
  current single-pass scan.
- `optimize_load_fast` (flowgraph.c:2546) plus all of `ref_stack_*`,
  `kill_local`, `maybe_push`, `scan_block_for_locals`. Land in
  `compile/flowgraph_locals.go`.

### Phase 5. Assemble bridge

Port `_PyCfg_ToInstructionSequence` (flowgraph.c:3988) so the
optimized graph flattens back into the assembler's expected
input. This is the seam with spec 1708 (assemble location table).

### Phase 6. Retire the flat-sequence shim

Delete every `func ...(seq []Instr, ...)` pass left in
`compile/flowgraph_passes.go`. `OptimizeWithFlags` becomes a
direct port of `_PyCfg_OptimizeCodeUnit`.

## Out of scope

- Specializer / inline cache layout. Lives under spec 1712.
- Marshal byte layout. Lives under spec 1713.
- Codegen-side instruction sequence DSL. Lives under spec 1714.
- Assemble location table. Lives under spec 1708.

This spec is purely the flowgraph subsystem's data model and the
passes that run inside it.
