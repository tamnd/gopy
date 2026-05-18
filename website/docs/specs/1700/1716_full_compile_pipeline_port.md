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

## Phase summary

Top-level progress at a glance. Each phase has its own detail
table below; update both this table and the detail table in the
same commit that flips a row.

| Phase | Goal                                                                                                                        | Status      | Commit    |
| ----- | --------------------------------------------------------------------------------------------------------------------------- | ----------- | --------- |
| A     | Function-by-function audit: `Python/flowgraph.c`, `Python/assemble.c`, `optimize_and_assemble_code_unit`                    | done        | `95b53a4` |
| B.0   | gopy substrate (DumpCfg + CfgPhaseHook)                                                                                     | done        | `e94fcfa` |
| B.1   | CPython patch (per-phase dump + hook export)                                                                                | done        | `c9d380f` |
| B.2   | CPython oracle wrappers (L1 / L3 / L4)                                                                                      | done        | `f960caf`     |
| B.3   | Diff harness (`cfg_phase_parity_test.go` + corpus)                                                                          | done        | (this commit) |
| C.1   | Finish `Python/flowgraph.c` port (`convert_pseudo_conditional_jumps`, `prepare_localsplus`, `_PyCfg_OptimizedCfgToInstructionSequence`, helpers) | pending     | -         |
| C.2   | Finish `Python/assemble.c` port (split into `assemble_makecode.go` / `assemble_jumps.go` / `assemble.go`)                   | pending     | -         |
| C.3   | Port `optimize_and_assemble_code_unit` driver into `compile/compiler.go`                                                    | pending     | -         |
| D.1   | Flip `assembleUnit` to the cfg driver                                                                                       | pending     | -         |
| D.2   | Delete `compile/flowgraph_passes.go`, `flowgraph_jumps.go`, `flowgraph_except.go`, `flowgraph_locals.go`, `flowgraph_stackdepth.go` | pending     | -         |
| D.3   | Rename cfg files so the filename map in **Goal** holds                                                                       | pending     | -         |
| E     | All gates green; 1713 byte-equality work resumes on the new substrate                                                       | pending     | -         |

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

## Phase A: audit

Function-by-function map of the three CPython files against the
current gopy state. The audit is fixed: rows only change as C.1 /
C.2 / C.3 land the missing functions.

### A.1. `Python/flowgraph.c` (4165 lines)

| CPython function                                  | CPython line | gopy state                                                          | Status  | Commit    |
| ------------------------------------------------- | ------------ | ------------------------------------------------------------------- | ------- | --------- |
| `_PyCfg_OptimizeCodeUnit`                         | 3659         | ported 1:1 onto cfgBuilder                                          | done    | `6f49fd5` |
| `optimize_cfg`                                    | 3273         | ported 1:1                                                          | done    | `6f49fd5` |
| `optimize_load_const`                             | 2552         | ported 1:1                                                          | done    | `1265050` |
| `remove_unused_consts`                            | 2588         | ported 1:1                                                          | done    | `1265050` |
| `mark_warm` / `mark_cold` / `push_cold_blocks_to_end` | 3174 / 3273 / 3323 | ported 1:1                                                       | done    | `3b1fb50` |
| `insert_superinstructions`                        | 3404         | ported 1:1                                                          | done    | `caf4e13` |
| `add_checks_for_loads_of_uninitialized_variables` | 2843         | ported 1:1 (lives in `flowgraph_cfg_locals.go`)                     | done    | `e94fcfa` |
| `convert_pseudo_conditional_jumps`                | 3478         | partial, only flat-sequence `peepholeOpcodePairs` covers a subset   | partial | C.1       |
| `calculate_stackdepth` (graph version)            | 1352         | partial, flat-sequence in `flowgraph_stackdepth.go`                 | partial | C.1       |
| `prepare_localsplus`                              | 3768         | missing (flat-sequence `insertPrefixInstructions` covers prefix)    | missing | C.1       |
| `build_cellfixedoffsets`                          | 3711         | missing                                                             | missing | C.1       |
| `insert_prefix_instructions` (graph version)      | 3760         | partial, flat-sequence only                                         | partial | C.1       |
| `fix_cell_offsets`                                | 3729         | missing                                                             | missing | C.1       |
| `_PyCfg_OptimizedCfgToInstructionSequence`        | 4026         | missing as a single function (callers cobble it from pieces)        | missing | C.1       |

### A.2. `Python/assemble.c` (802 lines)

| CPython function                                  | CPython line | gopy state                                                | Status  | Commit    |
| ------------------------------------------------- | ------------ | --------------------------------------------------------- | ------- | --------- |
| `same_location`                                   | 30           | inline in `assembleLocationInfo`                          | partial | C.2       |
| `instr_size`                                      | 39           | `instrSize` in `assemble_locations.go`                    | done    | spec 1708 |
| `assemble_init` / `assemble_free`                 | 62 / 90      | replaced by `Assembler` struct lifecycle                  | done    | spec 1708 |
| `write_except_byte`                               | 98           | done                                                      | done    | spec 1708 |
| `assemble_emit_exception_table_item`              | 106          | done                                                      | done    | spec 1708 |
| `assemble_emit_exception_table_entry`             | 133          | done                                                      | done    | spec 1708 |
| `assemble_exception_table`                        | 158          | done (`assembleExceptionTable`)                           | done    | spec 1708 |
| `write_location_byte`                             | 196          | done                                                      | done    | spec 1708 |
| `write_location_first_byte`                       | 211          | done                                                      | done    | spec 1708 |
| `write_location_varint`                           | 218          | done                                                      | done    | spec 1708 |
| `write_location_signed_varint`                    | 226          | done                                                      | done    | spec 1708 |
| `write_location_info_short_form`                  | 233          | done                                                      | done    | spec 1708 |
| `write_location_info_oneline_form`                | 246          | done                                                      | done    | spec 1708 |
| `write_location_info_long_form`                   | 258          | done                                                      | done    | spec 1708 |
| `write_location_info_none`                        | 270          | done                                                      | done    | spec 1708 |
| `write_location_info_no_column`                   | 276          | done                                                      | done    | spec 1708 |
| `write_location_info_entry`                       | 286          | done                                                      | done    | spec 1708 |
| `assemble_emit_location`                          | 324          | done                                                      | done    | spec 1708 |
| `assemble_location_info`                          | 337          | done                                                      | done    | spec 1708 |
| `write_instr`                                     | 369          | done (inline in `emitInstr`)                              | done    | spec 1708 |
| `assemble_emit_instr`                             | 413          | inline in `Assemble`, no separate function                | partial | C.2       |
| `assemble_emit`                                   | 432          | inline loop in `Assemble`                                 | partial | C.2       |
| `dict_keys_inorder`                               | 458          | reads from `Unit` directly                                | missing | C.2       |
| `compute_localsplus_info`                         | 484          | `buildLocalsPlus`, not split out                          | partial | C.2       |
| `makecode`                                        | 575          | inline in `Assemble`                                      | partial | C.2       |
| `resolve_jump_offsets`                            | 675          | flat-sequence in `flowgraph_jumps.go`                     | partial | C.2       |
| `resolve_unconditional_jumps`                     | 749          | flat-sequence in `flowgraph_jumps.go`                     | partial | C.2       |
| `_PyAssemble_MakeCodeObject`                      | 779          | inline in `Assemble`                                      | partial | C.2       |

The location and exception writers are clean from 1708. The
remaining gap is structural: gopy collapses
`_PyAssemble_MakeCodeObject` -> `assemble_emit` ->
`assemble_emit_instr` into one inline loop and keeps the
jump-resolution helpers on the flat-sequence side. They have to
move onto the cfg side to match the CPython ordering inside
`_PyCfg_OptimizedCfgToInstructionSequence`.

### A.3. `Python/compile.c` driver

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
After C.3 lands, `assembleUnit` matches the four-call structure
above.

## Phase B: state capture

Per-phase compat tests need a way to dump the cfg between every
pipeline stage and diff against a CPython-side dump. Phase B
ships the format, the gopy hook, the matching CPython patch, and
the diff harness that consumes both sides.

### B.0. gopy substrate (DumpCfg + CfgPhaseHook)

| Item                                                                          | Status | Commit    |
| ----------------------------------------------------------------------------- | ------ | --------- |
| `compile.DumpCfg` reproducible dump format (`compile/flowgraph_cfg_dump.go`)   | done   | `e94fcfa` |
| `CfgPhaseHook` callback type + `cfgOptimizeCodeUnitWithHook` driver           | done   | `e94fcfa` |
| Unit tests covering the dump format and hook firing order                     | done   | `e94fcfa` |

### B.1. CPython patch (per-phase dump + hook export)

The CPython side does not expose intermediate CFG state. We ship
a small, additive patch to the bundled CPython checkout that
re-enables the `#if 0` dumpers, routes them through a
`PyUnicodeWriter`, and fires a Python-level hook between each
pass. `Modules/_testinternalcapi.c` grows a
`set_cfg_phase_hook(callback)` thunk so the harness can install a
Python callable.

| Item                                                                                                                       | Status | Commit            |
| -------------------------------------------------------------------------------------------------------------------------- | ------ | ----------------- |
| `test/cpython/patches/0001-cfg-phase-dump.patch` (re-enables dumpers, adds hook registry + `FIRE_PHASE` calls, exposes thunk) | done   | `c9d380f`         |
| `test/cpython/patches/dump_phases.py` harness driving the patched interpreter into a `<unit>.<phase>.cfg` tree              | done   | `c9d380f`         |
| `test/cpython/patches/README.md` (how to apply + rebuild + run the harness)                                                 | done   | `c9d380f`         |
| Patched build smoke-tested: `python.exe dump_phases.py --src foo.py --out /tmp/cfg` produces one file per `(unit, phase)`   | done   | `c9d380f`         |

Phase boundaries fired (each emits one dump per code unit):

| # | Phase name                                          | Where it fires (`Python/flowgraph.c`)                     |
| - | --------------------------------------------------- | ---------------------------------------------------------- |
| 0 | `entry`                                             | start of `_PyCfg_OptimizeCodeUnit` (line 3662)             |
| 1 | `translate_jump_labels_to_targets`                  | line 3665                                                  |
| 2 | `mark_except_handlers`                              | line 3666                                                  |
| 3 | `label_exception_targets`                           | line 3667                                                  |
| 4 | `optimize_cfg`                                      | line 3691                                                  |
| 5 | `remove_unused_consts`                              | line 3697                                                  |
| 6 | `add_checks_for_loads_of_uninitialized_variables`   | line 3700                                                  |
| 7 | `insert_superinstructions`                          | line 3701                                                  |
| 8 | `push_cold_blocks_to_end`                           | line 3703                                                  |
| 9 | `resolve_line_numbers`                              | line 3704                                                  |

The harness writes `unit<idx>.<phase>.cfg` per fire. Two `entry`
fires for the same `idx` means a bug: each unit fires `entry`
exactly once.

### B.2. CPython oracle wrappers (L1 / L3 / L4)

Thin Python wrappers around the three `_testinternalcapi` entry
points so the Go-side gate can compare instruction sequences and
code objects without re-implementing CPython's introspection.
`assemble_oracle.py` drives the regular `compile()` builtin instead
of `_testinternalcapi.assemble_code_object` because that entry
needs a full metadata dict (names / varnames / cellvars / freevars
/ fasthidden as dicts) that `compiler_codegen` does not expose, so
re-building it in Python would defeat the point of a comparison
oracle. `compile()` runs the same `_PyAssemble_MakeCodeObject`
internally.

| Item                                                                                                                                      | Status | Commit    |
| ----------------------------------------------------------------------------------------------------------------------------------------- | ------ | --------- |
| `test/cpython/patches/oracle_common.py`: shared `opname`, `format_const`, `dump_instruction_sequence`, `parse_source` helpers              | done   | `f960caf`     |
| `test/cpython/patches/codegen_oracle.py`: drives `_testinternalcapi.compiler_codegen(ast, filename, optimize)` -> `unit000.l1.txt`         | done   | `f960caf`     |
| `test/cpython/patches/optimize_oracle.py`: drives `compiler_codegen` then `_testinternalcapi.optimize_cfg(seq, consts, nlocals)` -> `unit000.l3.txt` | done   | `f960caf`     |
| `test/cpython/patches/assemble_oracle.py`: walks every nested code object from `compile(src, filename, "exec")` -> `unit<idx>.l4.txt`     | done   | `f960caf`     |
| Each oracle prints stable `InstructionSequence.get_instructions()` tuples (L1/L3) or code-object fields (L4) for diff                     | done   | `f960caf`     |
| Smoke-tested: `def f(x): return x+1; print(f(2))` shows the `LOAD_CONST 2` -> `LOAD_SMALL_INT 2` rewrite between L1 and L3                | done   | `f960caf`     |

### B.3. Diff harness (`cfg_phase_parity_test.go`)

| Item                                                                                                                                 | Status  | Commit |
| ------------------------------------------------------------------------------------------------------------------------------------ | ------- | ------ |
| Align gopy `JumpTargetLabel` numbering with CPython: 0-based `NewLabel`, `-1` for no-label (today gopy starts at 1, uses 0 for none) | done    | `e11370a` |
| `compile.CompileWithCfgPhaseHook` public entry that drives the cfg pipeline with a hook                                              | done    | `266f73e` |
| `test/gate/cfg_phase_parity_test.go` (subprocess to patched CPython, run gopy with `CompileWithCfgPhaseHook`, diff per `(unit, phase)`) | done    | (this commit) |
| `test/gate/cfg_phase_corpus.txt` (file list, grows from `__future__.py` outward)                                                     | done    | (this commit) |
| `test/gate/cfg_phase_skip.txt` (per-file reasons for L0 failures, linked to bug tracker)                                              | done    | (this commit) |
| Workflow wiring: gate workflow builds the patched CPython once, caches it, then runs the test                                        | done    | (this commit) |

### B Reference: what CPython exposes vs what we add

| Hook                                                                  | CPython source                              | What it returns                                                | Level |
| --------------------------------------------------------------------- | ------------------------------------------- | -------------------------------------------------------------- | ----- |
| `_testinternalcapi.new_instruction_sequence()`                        | `Modules/_testinternalcapi.c:715`           | empty `_PyInstructionSequence`                                 | -     |
| `_testinternalcapi.compiler_codegen(ast, filename, optimize, mode=0)` | `Modules/_testinternalcapi.c:728`           | `(InstructionSequence, metadata_dict)` from `codegen.c`        | L1    |
| `_testinternalcapi.optimize_cfg(seq, consts, nlocals)`                | `Modules/_testinternalcapi.c:754`           | optimized `InstructionSequence` (full pipeline runs internally) | L3    |
| `_testinternalcapi.assemble_code_object(filename, insts, metadata)`   | `Modules/_testinternalcapi.c:785`           | `types.CodeType` from `_PyCompile_Assemble`                    | L4    |
| `_testinternalcapi.set_cfg_phase_hook(callback)` *(B.1 patch)*        | `test/cpython/patches/0001-cfg-phase-dump.patch` | install a Python callback that receives every per-pass dump   | L2    |

### B Reference: the comparison ladder

Each level traps regressions earlier than the next so a
divergence lands at the highest level where it occurs and the
debug work points at one pass instead of the whole back-end.

| Level | What's compared                              | Source of truth                                   | Caught when                                                  |
| ----- | -------------------------------------------- | ------------------------------------------------- | ------------------------------------------------------------ |
| L0    | AST                                          | `ast.dump(tree, indent=2)` on both sides          | Parser / preprocess diverges (caught by spec 1710 gates)     |
| L1    | Pre-optimize instruction sequence            | `compiler_codegen` -> `get_instructions()` tuples | Codegen-side AST emission diverges (spec 1714 surface)       |
| L2    | Per-phase CFG dump (entry, after every pass) | patched `_PyCfg_OptimizeCodeUnit` callback        | Any individual optimization pass diverges                    |
| L3    | Post-optimize instruction sequence           | `optimize_cfg` -> `get_instructions()` tuples     | The CFG -> sequence bridge diverges                          |
| L4    | Code object byte-equality                    | `marshal.dumps(co)` (spec 1713's gate)            | Assemble-side metadata, location, or constant order diverges |

Reproducibility constraints: both sides run with
`PYTHONHASHSEED=0` (CPython interns string constants by content
hash; without a fixed seed `co_consts` ordering drifts); the gate
pins to one CPython tag (currently `v3.14.5`); tag bumps go
through the spec 1707 audit so the patch and the gopy port move
together.

### B Reference: dump format

Each phase dump is just the block listing CPython's `cfg_dump_to_string`
returns, with no per-unit header. The harness on both sides composes
the header `# cfg dump: <phase>` outside the dump itself.

Per-block:

```
B<label.id>: [EH=<eh> CLD=<cold> WRM=<warm> NO_FT=<no_ft>] used: <iused>, depth: <startdepth>, preds: <predecessors> <return-marker>
  [<idx:02d>] line: <lineno>, <opname> (<opcode>) <arg-or-target><jump-marker>
```

- `<return-marker>` is `"return "` when the last instruction is `RETURN_VALUE`, else empty.
- `<arg-or-target>` is `"target: B<tgt.label.id> [<oparg>] "` for HAS_TARGET opcodes (`-1` when target is nil), `"arg: <oparg> "` for opcodes with HAS_ARG but no target, else empty.
- `<jump-marker>` is `"jump "` when the opcode is a jump, else empty.
- Numeric fields use the same `%d` width as CPython (no padding). Boolean fields render as `0` or `1`.

No raw pointers, no addresses. gopy's `compile.DumpCfg` and the patch's
`cfg_dump_to_string` must produce byte-identical strings. Any drift is
a gopy bug and fails L2.

## Phase C: port whole files

Each phase ports one CPython file in full. No file gets split
across phases.

### C.1. Finish `Python/flowgraph.c`

| Function                                       | CPython line | Lands in                            | Status  | Commit |
| ---------------------------------------------- | ------------ | ----------------------------------- | ------- | ------ |
| `convert_pseudo_conditional_jumps`             | 3478         | `flowgraph_cfg_passes.go`           | pending | -      |
| `calculate_stackdepth` (graph version)         | 1352         | `flowgraph_cfg_stackdepth.go` (new) | pending | -      |
| `build_cellfixedoffsets`                       | 3711         | `flowgraph_cfg_passes.go`           | pending | -      |
| `insert_prefix_instructions` (graph version)   | 3760         | `flowgraph_cfg_passes.go`           | pending | -      |
| `fix_cell_offsets`                             | 3729         | `flowgraph_cfg_passes.go`           | pending | -      |
| `prepare_localsplus`                           | 3768         | `flowgraph_cfg_passes.go`           | pending | -      |
| `_PyCfg_OptimizedCfgToInstructionSequence`     | 4026         | `flowgraph_cfg_bridge.go`           | pending | -      |
| Sweep: diff cfg-side function list vs `Python/flowgraph.c`; flag elisions and follow-ups | -    | -                                   | pending | -      |

### C.2. Finish `Python/assemble.c`

| Split                                                                                                           | Lands in                  | Status  | Commit |
| --------------------------------------------------------------------------------------------------------------- | ------------------------- | ------- | ------ |
| `makecode`, `compute_localsplus_info`, `dict_keys_inorder`, `_PyAssemble_MakeCodeObject` as separate functions   | `assemble_makecode.go`    | pending | -      |
| `resolve_jump_offsets`, `resolve_unconditional_jumps` rewritten over `cfgBuilder`                                | `assemble_jumps.go`       | pending | -      |
| Public surface (`Assemble`) plus `assemble_emit_instr` / `assemble_emit`                                         | `assemble.go`             | pending | -      |

### C.3. Driver port

| Item                                                                                                                     | Lands in                                          | Status  | Commit |
| ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------- | ------- | ------ |
| `optimize_and_assemble_code_unit` four-call sequence ported 1:1 from `Python/compile.c:1411`                              | `compile/compiler_assemble.go`                    | pending | -      |
| `compile/compiler.go:assembleUnit` rewired to delegate to the new function                                                | `compile/compiler.go`                             | pending | -      |

## Phase D: wire and retire

| Step | What                                                                                                                                                                                                  | Status  | Commit |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ |
| D.1  | Flip `assembleUnit` to call the C.3 driver instead of `OptimizeWithFlags`                                                                                                                              | pending | -      |
| D.2  | Delete `compile/flowgraph_passes.go`, `flowgraph_jumps.go`, `flowgraph_except.go`, `flowgraph_locals.go`, `flowgraph_stackdepth.go` (`flowgraph.go` shrinks to `Info` + `ExceptHandler`)                  | pending | -      |
| D.3  | Rename / split cfg files so the filename map in **Goal** holds (`flowgraph_cfg.go` becomes `flowgraph_cfg.go` + `flowgraph_cfg_passes.go` + `flowgraph_cfg_locals.go` + `flowgraph_cfg_stackdepth.go`)   | pending | -      |

## Phase E: gates

Each phase keeps existing gates green and adds its own:

| Gate                                          | Owner                                | Status  | Commit |
| --------------------------------------------- | ------------------------------------ | ------- | ------ |
| `test/gate/TestDisParityLib`                  | spec 1713                            | green   | -      |
| `test/gate/TestDisParity`                     | spec 1713                            | green   | -      |
| `compile/...` package tests                   | spec 1716 (existing)                 | green   | -      |
| `test/gate/cfg_phase_parity_test.go` (L2)     | this spec, B.3                       | green   | (this commit) |
| `test/gate/codegen_parity_test.go` (L1)       | this spec, B.2                       | pending | -      |
| `test/gate/assemble_parity_test.go` (L3 / L4) | this spec, B.2 + spec 1713 byte-eq   | pending | -      |

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
