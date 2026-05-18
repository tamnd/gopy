---
format: md
id: 1716_full_compile_pipeline_port
title: "1716. Full compile-pipeline port roadmap (flowgraph + assemble + driver)"
sidebar_label: "1716. compile pipeline port"
sidebar_position: 1716
slug: /specs/1716-full-compile-pipeline-port
description: "Replace gopy's flat-instruction-sequence optimization pipeline with a 1:1 port of CPython 3.14's CFG-based pipeline. Each phase ports one full CPython file (Python/flowgraph.c, Python/assemble.c, the optimize_and_assemble_code_unit driver from Python/compile.c). Captures internal pipeline state at every phase boundary so per-phase compat tests can pin gopy to CPython without waiting for byte-equality."
---

## Ground rule

Same rule as 1704 / 1705 / 1708 / 1712 / 1713 / 1715. Port whole
CPython files, function by function, every `// CPython:` citation
carries filename plus line number. No keeping flat-sequence
shims alongside the graph "for now". The shim file gets deleted
in the same change that ports its last caller.

This spec subsumes the still-pending parts of 1715 (the original
graph-substrate port). 1715 landed phases 1 through 5; this spec
finishes phase 6 and extends the scope across `Python/assemble.c`
and the `optimize_and_assemble_code_unit` driver in
`Python/compile.c`, so the whole CPython compile back-end lands as
one coherent port instead of three half-overlapping ones.

## Why this exists

Spec 1713 has been driving disassembly-stream parity, and every
divergence so far has been fixed by patching
`compile/flowgraph_passes.go`: the flat-sequence pipeline that
predates the CFG substrate. Recent examples that all needed
custom flat-sequence logic with no analogue in CPython:

- `propagateLineNumbers` had to invent a block-boundary detector
  (computeBlockStartsResolved) because the flat sequence does not
  carry `b_next` / `b_predecessors`.
- `removeUnusedConsts` shipped a "are we inside a tuple body"
  heuristic that has no analogue in CPython.
- `foldConstSequences` had to special-case the LABEL pseudo-op
  that does not exist in CPython's graph.
- `optimizeLoadFastOnSequence` builds a temporary cfg, runs the
  real CPython pass on it, then copies opcode rewrites back into
  the sequence. It already exists 1:1 on the graph; the
  flat-sequence wrapper is a roundtrip.

Each patch reads the CPython source, then *translates* it onto
the flat sequence. Every translation is a fresh opportunity for
the substrate mismatch to produce a divergent edge case. The cost
compounds because spec 1713's gate is byte-equality: divergences
surface as opaque marshal-output differences, then need to be
traced back through the flat-sequence translation before they can
be compared against the CPython source the translation was
supposed to mirror.

A second pipeline already exists in `compile/flowgraph_cfg*.go`.
It is a 1:1 port of CPython's CFG passes, complete enough to drive
`_PyCfg_OptimizeCodeUnit` end to end. It is not wired into the
compile entry point. Every flat-sequence patch shipped under 1713
is therefore re-implementing logic that gopy already has, on a
substrate that does not match CPython.

This spec retires the flat-sequence track. The CFG track becomes
the only track.

## Goal

The compile back-end maps file for file onto CPython 3.14:

```
Python/flowgraph.c              -> compile/flowgraph.go               (public surface)
                                   compile/flowgraph_cfg.go           (basicblock, cfgBuilder, cfgInstr)
                                   compile/flowgraph_cfg_passes.go    (every optimization pass)
                                   compile/flowgraph_cfg_locals.go    (optimize_load_fast + ref_stack)
                                   compile/flowgraph_cfg_stackdepth.go
                                   compile/flowgraph_cfg_bridge.go    (from/to instruction sequence)

Python/assemble.c               -> compile/assemble.go                (public surface, _PyAssemble_MakeCodeObject)
                                   compile/assemble_locations.go      (PEP 657 location table writers)
                                   compile/assemble_exceptions.go     (exception table writers)
                                   compile/assemble_varint.go         (varint helpers)
                                   compile/assemble_jumps.go          (resolve_jump_offsets, resolve_unconditional_jumps)
                                   compile/assemble_makecode.go       (makecode, compute_localsplus_info)

Python/compile.c                -> compile/compiler.go
  optimize_and_assemble_code_unit   compile/compiler_assemble.go      (assembleUnit ported 1:1)
```

Spec done means: `assembleUnit` calls
`_PyCfg_FromInstructionSequence` -> `_PyCfg_OptimizeCodeUnit` ->
`_PyCfg_OptimizedCfgToInstructionSequence` ->
`_PyAssemble_MakeCodeObject` in the same order, with the same
arguments, as `optimize_and_assemble_code_unit` in
`Python/compile.c:1411`. `compile/flowgraph_passes.go`,
`compile/flowgraph_jumps.go`, `compile/flowgraph_except.go`,
`compile/flowgraph_locals.go`, and `compile/flowgraph_stackdepth.go`
no longer exist as flat-sequence files.

## Audit (Phase A)

The first phase is a function-by-function audit of the three
CPython files against the corresponding gopy state. The audit
produces a map of:

- functions that exist 1:1 (status: ported)
- functions that exist but on the flat sequence (status: partial,
  rewrite onto cfgBuilder)
- functions that do not exist (status: missing, port from scratch)

The audit goes in this spec; subsequent phases work off it.

### Python/flowgraph.c (4165 lines)

The current cfg port covers `_PyCfg_OptimizeCodeUnit` and every
helper it calls. The functions still missing or partial:

| CPython function                          | CPython line | gopy state                                                          |
| ----------------------------------------- | ------------ | ------------------------------------------------------------------- |
| `convert_pseudo_conditional_jumps`        | 3478         | partial, only flat-sequence `peepholeOpcodePairs` covers a subset   |
| `calculate_stackdepth`                    | 1352         | partial, flat-sequence in `flowgraph_stackdepth.go`                 |
| `prepare_localsplus`                      | 3768         | missing (flat-sequence `insertPrefixInstructions` covers prefix)    |
| `build_cellfixedoffsets`                  | 3711         | missing                                                             |
| `insert_prefix_instructions`              | 3760         | partial, flat-sequence only                                         |
| `fix_cell_offsets`                        | 3729         | missing                                                             |
| `_PyCfg_OptimizedCfgToInstructionSequence`| 4026         | missing as a single function (callers cobble it from pieces)        |
| `add_checks_for_loads_of_uninitialized_variables` | 2843 | done (lives in `flowgraph_cfg_locals.go`)                           |

Everything else (`_PyCfg_OptimizeCodeUnit` plus the optimization
passes it drives) is already on the graph from 1715. See
`compile/flowgraph_cfg_passes.go` for the function list.

### Python/assemble.c (802 lines)

| CPython function                          | CPython line | gopy state                                                |
| ----------------------------------------- | ------------ | --------------------------------------------------------- |
| `same_location`                           | 30           | inline in `assembleLocationInfo`                          |
| `instr_size`                              | 39           | done (`instrSize` in `assemble_locations.go`)             |
| `assemble_init` / `assemble_free`         | 62 / 90      | replaced by `Assembler` struct lifecycle                  |
| `write_except_byte`                       | 98           | done                                                      |
| `assemble_emit_exception_table_item`      | 106          | done                                                      |
| `assemble_emit_exception_table_entry`     | 133          | done                                                      |
| `assemble_exception_table`                | 158          | done (`assembleExceptionTable`)                           |
| `write_location_byte`                     | 196          | done                                                      |
| `write_location_first_byte`               | 211          | done                                                      |
| `write_location_varint`                   | 218          | done                                                      |
| `write_location_signed_varint`            | 226          | done                                                      |
| `write_location_info_short_form`          | 233          | done                                                      |
| `write_location_info_oneline_form`        | 246          | done                                                      |
| `write_location_info_long_form`           | 258          | done                                                      |
| `write_location_info_none`                | 270          | done                                                      |
| `write_location_info_no_column`           | 276          | done                                                      |
| `write_location_info_entry`               | 286          | done                                                      |
| `assemble_emit_location`                  | 324          | done                                                      |
| `assemble_location_info`                  | 337          | done                                                      |
| `write_instr`                             | 369          | done (inline in `emitInstr`)                              |
| `assemble_emit_instr`                     | 413          | partial, no separate function                             |
| `assemble_emit`                           | 432          | partial, inline loop in `Assemble`                        |
| `dict_keys_inorder`                       | 458          | missing, gopy reads from `Unit` directly                  |
| `compute_localsplus_info`                 | 484          | partial as `buildLocalsPlus`                              |
| `makecode`                                | 575          | partial, inline in `Assemble`                             |
| `resolve_jump_offsets`                    | 675          | partial, on flat sequence in `flowgraph_jumps.go`         |
| `resolve_unconditional_jumps`             | 749          | partial, on flat sequence in `flowgraph_jumps.go`         |
| `_PyAssemble_MakeCodeObject`              | 779          | partial, inline in `Assemble`                             |

The location and exception writers are clean from 1708. The
remaining gap is structural: gopy collapses
`_PyAssemble_MakeCodeObject` -> `assemble_emit` ->
`assemble_emit_instr` into one inline loop and keeps the
jump-resolution helpers on the flat-sequence side. They have to
move onto the cfg side to match the CPython ordering inside
`_PyCfg_OptimizedCfgToInstructionSequence`.

### Python/compile.c (optimize_and_assemble_code_unit)

```c
g = _PyCfg_FromInstructionSequence(u->u_instr_sequence);
_PyCfg_OptimizeCodeUnit(g, consts, ...);
_PyCfg_OptimizedCfgToInstructionSequence(g, &u->u_metadata, code_flags,
                                         &stackdepth, &nlocalsplus,
                                         &optimized_instrs);
co = _PyAssemble_MakeCodeObject(&u->u_metadata, const_cache, consts,
                                stackdepth, &optimized_instrs, nlocalsplus,
                                code_flags, filename);
```

`compile/compiler.go:assembleUnit` today calls
`OptimizeWithFlags` (the flat-sequence pipeline) then `Assemble`.
After this spec, `assembleUnit` matches the four-call structure
above.

## State capture (Phase B)

Per-phase compat tests need a way to dump the cfg between every
pipeline stage and diff against a CPython-side dump. This phase
adds the dump format and the hook points.

### Dump format

```
# cfg dump: <phase name>
firstlineno: <int>
nlocals: <int>
nparams: <int>
codeflags: 0x<hex>

block 0 (label=<id> preds=<n> startdepth=<n> warm=<bool> cold=<bool>):
  <i> <opname>(<oparg>) loc=<line:col-eline:ecol> target=block<id> except=block<id>
  ...
  -> next=block<id>
```

The format is deterministic (blocks in fallthrough order, no
pointers) so two dumps diff cleanly with `cmp`.

### Hook points

Inside `cfgOptimizeCodeUnit`:

```
after translate_jump_labels_to_targets
after mark_except_handlers
after label_exception_targets
after optimize_cfg
after remove_unused_consts
after add_checks_for_loads_of_uninitialized_variables
after insert_superinstructions
after push_cold_blocks_to_end
after resolve_line_numbers
```

Inside `cfgOptimizedCfgToInstructionSequence`:

```
after convert_pseudo_conditional_jumps
after calculate_stackdepth
after prepare_localsplus
after convert_pseudo_ops
after normalize_jumps
after optimize_load_fast
after _PyCfg_ToInstructionSequence
```

Inside `_PyAssemble_MakeCodeObject`:

```
after resolve_unconditional_jumps
after resolve_jump_offsets
after assemble_emit
after assemble_exception_table
after assemble_location_info
after makecode
```

### CPython side

CPython 3.14 already exposes `_testinternalcapi.optimize_cfg` and
`_testinternalcapi.assemble_code_object` for unit tests. The
existing hooks dump after the whole pipeline runs. The spec adds
a debug build flag (`-X dump-cfg-after=<phase>`) that calls the
same dump format gopy emits, so the two outputs diff line for
line.

### Test driver

`test/gate/cfg_phase_parity_test.go` walks
`test/cpython/Lib/` (currently `__future__.py` plus `keyword.py`,
to be grown) and for each file runs both interpreters with the
hook flag set at every phase boundary. Mismatches surface the
exact phase where gopy diverges.

## Phase C: port whole files

Each phase ports one CPython file in full. No file gets split
across phases.

### C.1. Finish `Python/flowgraph.c`

Port the functions still flagged partial or missing in the audit:

| Function                                       | CPython line | Lands in                            |
| ---------------------------------------------- | ------------ | ----------------------------------- |
| `convert_pseudo_conditional_jumps`             | 3478         | `flowgraph_cfg_passes.go`           |
| `calculate_stackdepth` (graph version)         | 1352         | `flowgraph_cfg_stackdepth.go` (new) |
| `build_cellfixedoffsets`                       | 3711         | `flowgraph_cfg_passes.go`           |
| `insert_prefix_instructions` (graph version)  | 3760         | `flowgraph_cfg_passes.go`           |
| `fix_cell_offsets`                             | 3729         | `flowgraph_cfg_passes.go`           |
| `prepare_localsplus`                           | 3768         | `flowgraph_cfg_passes.go`           |
| `_PyCfg_OptimizedCfgToInstructionSequence`     | 4026         | `flowgraph_cfg_bridge.go`           |

Plus a sweep that diffs the cfg-side function list against
`Python/flowgraph.c`'s function list. Every CPython function is
either ported, intentionally elided (with a short note), or
listed as a follow-up.

### C.2. Finish `Python/assemble.c`

Split the current `compile/assemble.go` so each CPython function
gets its own gopy function (currently `Assemble` collapses
`_PyAssemble_MakeCodeObject` + `assemble_emit` + `makecode` +
`assemble_emit_instr` into one method).

- `assemble_makecode.go` for `makecode`, `compute_localsplus_info`,
  `dict_keys_inorder`, `_PyAssemble_MakeCodeObject` as separate
  functions.
- `assemble_jumps.go` for `resolve_jump_offsets` and
  `resolve_unconditional_jumps` rewritten to take a `cfgBuilder`
  (CPython's signature is `instr_sequence *`, gopy's flat-seq
  versions become the new file's input after C.1 lands).
- `assemble.go` shrinks to the public surface (`Assemble`) plus
  `assemble_emit_instr` / `assemble_emit`.

### C.3. Port the `optimize_and_assemble_code_unit` driver

Lands in `compile/compiler.go` (or a new `compile/compiler_assemble.go`
if the original file grows too far). Calls the four cfg / assemble
entry points in the same order as `Python/compile.c:1411`.

## Phase D: wire and retire

D.1. Flip `assembleUnit` to call the new driver from C.3 instead
of `OptimizeWithFlags`.

D.2. Delete the flat-sequence files. After C.3 the only callers of
`OptimizeWithFlags` are tests that walked a `Sequence` directly;
those move onto the cfg dump format from Phase B.

```
compile/flowgraph.go              (drops OptimizeWithFlags body, keeps Info / ExceptHandler)
compile/flowgraph_passes.go       deleted
compile/flowgraph_jumps.go        deleted
compile/flowgraph_except.go       deleted
compile/flowgraph_locals.go       deleted (replaced by flowgraph_cfg_locals.go)
compile/flowgraph_stackdepth.go   deleted (replaced by flowgraph_cfg_stackdepth.go)
```

D.3. The cfg-side files take over the CPython filename mapping
listed in **Goal** above.

## Phase E: gate

Each phase keeps existing gates green:

- `test/gate/TestDisParityLib` (CPython Lib subset disassembly)
- `test/gate/TestDisParity` (gopy disdata corpus)
- `compile/...` package tests

Per-phase compat tests (Phase B's `cfg_phase_parity_test.go`)
land alongside C.1 / C.2 / C.3 and are required green before any
flat-sequence file gets deleted.

Once D lands, the byte-equality work in 1713 can resume on a
substrate that matches CPython, so divergences localize to the
pass that produced them rather than to the substrate translation.

## Out of scope

- Marshal byte layout. Spec 1713.
- Codegen-side instruction sequence DSL. Spec 1714.
- Specializer / inline cache layout. Spec 1712.
- The assemble location table format itself (already 1:1 from
  1708; this spec only restructures the surrounding code so the
  file boundary matches CPython).

## Checklist

- [x] A. Audit: function-by-function map for `flowgraph.c`, `assemble.c`, `optimize_and_assemble_code_unit`
- [ ] B. State capture: cfg dump format + CPython side hook + `test/gate/cfg_phase_parity_test.go`
- [ ] C.1. Finish `Python/flowgraph.c` (convert_pseudo_conditional_jumps, calculate_stackdepth graph version, prepare_localsplus + helpers, `_PyCfg_OptimizedCfgToInstructionSequence`)
- [ ] C.2. Finish `Python/assemble.c` (split into makecode / jumps / emit files, every CPython function gets its own gopy function)
- [ ] C.3. Port `optimize_and_assemble_code_unit` driver into `compile/compiler.go`
- [ ] D.1. Flip `assembleUnit` to call the cfg driver
- [ ] D.2. Delete `compile/flowgraph_passes.go`, `flowgraph_jumps.go`, `flowgraph_except.go`, `flowgraph_locals.go`, `flowgraph_stackdepth.go`
- [ ] D.3. Rename / split the cfg files so the filename map in **Goal** holds
- [ ] E. All gates green; 1713 byte-equality work resumes on the new substrate
