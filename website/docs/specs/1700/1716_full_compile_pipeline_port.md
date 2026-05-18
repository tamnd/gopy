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

## State capture mechanics (Phase B deep dive)

The high-level Phase B summary above tells what to dump and when.
This subsection answers the operational question: *how* do we
actually pull the same state out of CPython, what does CPython
already expose, what do we have to patch in, and how does the
diff harness consume both sides? Without this section the port
keeps slipping back into "patch the symptom" mode because the
gopy side has no ground-truth oracle for the intermediate states.

### What CPython 3.14 already exposes

CPython ships three Python-callable entry points in
`Modules/_testinternalcapi.c` that drive the compile back-end
directly. They are the canonical handles for diffing intermediate
state without spinning up the full interpreter.

| Hook                                                                  | CPython source                                       | Returns                                                              |
| --------------------------------------------------------------------- | ---------------------------------------------------- | -------------------------------------------------------------------- |
| `_testinternalcapi.new_instruction_sequence()`                        | `Modules/_testinternalcapi.c:715`                    | empty `_PyInstructionSequence`                                       |
| `_testinternalcapi.compiler_codegen(ast, filename, optimize, mode=0)` | `Modules/_testinternalcapi.c:728`                    | `(InstructionSequence, metadata_dict)` straight out of `codegen.c`   |
| `_testinternalcapi.optimize_cfg(seq, consts, nlocals)`                | `Modules/_testinternalcapi.c:754` driving `flowgraph.c:4126` | optimized `InstructionSequence` (full pipeline runs internally)      |
| `_testinternalcapi.assemble_code_object(filename, insts, metadata)`   | `Modules/_testinternalcapi.c:785`                    | `types.CodeType` from `_PyCompile_Assemble`                          |

`InstructionSequence` itself is introspectable via
`get_instructions()` (returns a list of
`(opcode, oparg, lineno, end_lineno, col_offset, end_col_offset)`
tuples, with `-1` as the sentinel for unset locations) and
`get_nested()` (the list of nested sub-sequences for inner
scopes). Both are defined in `Python/instruction_sequence.c:333`
and `:353`. Labels appear inline as the relocated `oparg` after
`_PyInstructionSequence_ApplyLabelMap` runs, so a label-resolved
sequence dumps as flat tuples with integer targets.

These four hooks give us a stable, post-3.14-ABI surface for
three of the five comparison levels (L0, L1, L4 in the ladder
below). The two intermediate levels (L2 and L3) need a patched
build because CPython collapses every CFG pass into a single
`_PyCompile_OptimizeCfg` call.

### What CPython does *not* expose (and the patch we ship)

`_PyCfgBuilder_DumpGraph` is the closest thing CPython has to a
phase dumper, but it lives behind `#if 0` at
`Python/flowgraph.c:319` and only prints to stderr in CPython's
internal block format. To capture per-phase CFG snapshots, the
spec adds a single patch file:

```
test/cpython/patches/0001-cfg-phase-dump.patch
```

The patch is small and additive:

1. Re-enables `dump_basicblock` / `dump_instr` /
   `_PyCfgBuilder_DumpGraph` (remove the `#if 0` guard at
   `Python/flowgraph.c:277..328`).
2. Adds a `cfg_phase_dump` callback registry to `flowgraph.c`
   (one function pointer keyed by phase name) and arms it via a
   new `_testinternalcapi.set_cfg_phase_hook(callback)` thunk.
   `callback` is a Python callable that receives
   `(phase_name: str, dump_text: str)`.
3. Inserts `FIRE_PHASE("translate_jump_labels_to_targets")` etc.
   calls in `_PyCfg_OptimizeCodeUnit` at the same boundaries
   listed in **Hook points** above. The macro is a no-op when no
   hook is registered, so production builds are unaffected.
4. Replaces the `fprintf(stderr, ...)` in `dump_basicblock` with
   a `PyUnicodeWriter` so the dump goes back as a string instead
   of polluting stderr.

The patch is checked in under `test/cpython/patches/`, applied
during gate-builds of the bundled CPython, and never against the
system Python (`brew`'s interpreter is the reference for `.pyc`
parity only; the patched build is reference for CFG parity).
Rebasing the patch on top of new CPython tags is part of the
1707-style "pull CPython 3.14.x" routine.

### The comparison ladder

Each step traps regressions earlier than the next, so a
divergence lands at the highest level where it occurs and the
debug work points at one pass instead of the whole back-end.

| Level | What's compared                              | Source of truth                                   | Caught when                                              |
| ----- | -------------------------------------------- | ------------------------------------------------- | -------------------------------------------------------- |
| L0    | AST                                          | `ast.dump(tree, indent=2)` on both sides          | Parser / preprocess diverges (caught by spec 1710 gates) |
| L1    | Pre-optimize instruction sequence            | `compiler_codegen` -> `get_instructions()` tuples | Codegen-side AST emission diverges (spec 1714 surface)   |
| L2    | Per-phase CFG dump (entry, after every pass) | patched `_PyCfg_OptimizeCodeUnit` callback        | Any individual optimization pass diverges                |
| L3    | Post-optimize instruction sequence           | `optimize_cfg` -> `get_instructions()` tuples     | The CFG -> sequence bridge diverges                      |
| L4    | Code object byte-equality                    | `marshal.dumps(co)` (spec 1713's gate)            | Assemble-side metadata, location, or constant order diverges |

Every test under Phase E runs at the highest level its inputs
allow. Synthetic snippets in `test/gate/cfg_phase_parity_test.go`
target L2 directly. The `test/gate/dis_parity_test.go` corpus
runs at L1 and L3. The byte-equality gate from 1713 runs at L4.
L0 is owned by the parser specs.

### Capture mechanics on each side

Both sides must emit byte-identical strings for any given level,
otherwise the diff harness fights formatting drift instead of
real divergences. The dump rules (alphabetical for sets, integer
indices instead of pointers, lowercased boolean literals) are
enforced by:

- gopy: `compile.DumpCfg` (`compile/flowgraph_cfg_dump.go`) and
  `compile.DumpInstructionSequence` (to be added in C.1 alongside
  the bridge port).
- CPython patched build: the patch's `PyUnicodeWriter`-backed
  dumpers, which copy the gopy format verbatim. The patch's
  `dump_basicblock` is the canonical source. gopy's `DumpCfg`
  mirrors it character-for-character; any drift is a gopy bug
  and fails L2.

Reproducibility constraints:

- Both sides run with `PYTHONHASHSEED=0`. CPython interns string
  constants by content hash inside the const cache; the order
  bleeds through `co_consts` and `co_names`. Without a fixed
  hash seed the diff harness sees spurious reorderings.
- `co_consts` and `co_names` ordering is *not* hash-stable across
  CPython releases. The gate pins to one CPython tag (currently
  3.14.5) and the patch carries that tag in its header. Tag
  bumps go through spec 1707's audit so the patch and the gopy
  port move together.
- Location ordering: PEP 657 emits `i_loc` as
  `(lineno, end_lineno, col_offset, end_col_offset)`. gopy stores
  it as `ast.Pos`. The dump uses `lineno:col-end_lineno:end_col`,
  with `-1` rendered literally on either side so unset locations
  round-trip.

### The diff harness

`test/gate/cfg_phase_parity_test.go` is the single Go-side
driver. Operationally:

1. Read a `.py` file from `test/cpython/Lib/` (corpus grows from
   `__future__.py` plus `keyword.py` outward; the file list is
   captured in `test/gate/cfg_phase_corpus.txt`).
2. Subprocess to the patched CPython build with
   `python -X gopy-cfg-dump=<outdir>`. The `-X` flag is parsed
   by the patch's small `_testinternalcapi.set_cfg_phase_hook`
   bootstrap and writes one file per `(unit, phase)` pair under
   `outdir`.
3. Drive gopy through `compile.CompileWithCfgPhaseHook(src,
   filename, hook)` from the same Go test, writing the same file
   layout into a sibling `outdir`.
4. For each `(unit, phase)` pair, `os.ReadFile` both sides and
   `t.Errorf` on the first byte difference, with the surrounding
   ~10-line context.

The harness skips files whose AST-level parity (L0) already
fails: that divergence is a parser bug, not a back-end bug, and
reporting it here would only drown the real signal. Skipped
files go into `test/gate/cfg_phase_skip.txt` with a one-line
reason and a bug-tracker reference.

### How this section unblocks the rest of the spec

C.1, C.2 and C.3 each ship with a corresponding addition to the
phase-parity corpus:

- C.1 lands `convert_pseudo_conditional_jumps`,
  `calculate_stackdepth`, `prepare_localsplus`, and
  `_PyCfg_OptimizedCfgToInstructionSequence`. Their L2 hooks fire
  inside the section those functions own; the gate refuses to
  flip the C.1 row green until the dumps for each of those four
  new hook points match byte-for-byte.
- C.2 ships `makecode`, `resolve_jump_offsets`,
  `resolve_unconditional_jumps`, `assemble_emit`,
  `assemble_emit_instr` as separate functions. Each emits its own
  L3-equivalent dump (post-assemble bytecode + location table)
  for the diff harness.
- C.3's driver port lands the four-call sequence at the same
  call sites as `Python/compile.c:1411`. The driver row only
  flips after L4 (the byte-equality gate) goes green across the
  full corpus.

Because divergences localize to a phase, the work of fixing them
also localizes: the port that emitted the wrong dump is the port
to re-read against CPython, not the entire pipeline. That is the
"port functions, do not patch" rule operationalized on the
back-end.

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
- [x] B.0 gopy substrate: `DumpCfg` + `CfgPhaseHook` + tests
- [ ] B.1 CPython patch: `test/cpython/patches/0001-cfg-phase-dump.patch` (un-`#if 0` dumpers, add `set_cfg_phase_hook`, fire macro at every phase)
- [ ] B.2 CPython introspection wrappers: helpers around `_testinternalcapi.compiler_codegen` / `optimize_cfg` / `assemble_code_object` for L1/L3/L4 oracles
- [ ] B.3 Diff harness: `test/gate/cfg_phase_parity_test.go` with `cfg_phase_corpus.txt` + `cfg_phase_skip.txt`, wired into the existing gate workflow
- [ ] C.1. Finish `Python/flowgraph.c` (convert_pseudo_conditional_jumps, calculate_stackdepth graph version, prepare_localsplus + helpers, `_PyCfg_OptimizedCfgToInstructionSequence`)
- [ ] C.2. Finish `Python/assemble.c` (split into makecode / jumps / emit files, every CPython function gets its own gopy function)
- [ ] C.3. Port `optimize_and_assemble_code_unit` driver into `compile/compiler.go`
- [ ] D.1. Flip `assembleUnit` to call the cfg driver
- [ ] D.2. Delete `compile/flowgraph_passes.go`, `flowgraph_jumps.go`, `flowgraph_except.go`, `flowgraph_locals.go`, `flowgraph_stackdepth.go`
- [ ] D.3. Rename / split the cfg files so the filename map in **Goal** holds
- [ ] E. All gates green; 1713 byte-equality work resumes on the new substrate
