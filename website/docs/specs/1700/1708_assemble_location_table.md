---
format: md
id: 1708_assemble_location_table
title: "1708. Python/assemble.c location-emission full port"
sidebar_label: "1708. assemble_location_table"
sidebar_position: 1708
slug: /specs/1708-assemble_location_table
description: "Port Python/assemble.c's PEP 657 location-emission pipeline in full so every emitted instruction carries its own source-line entry. Removes the lineForOffset walk-back shim that vm/eval_unwind.go uses to recover linenos for traceback rendering."
---


## Rule

Every CPython source file in scope is ported **in full**. No
function in those files may be left unported. The deliverable for
each file is a Go file whose function list 1:1 covers the C
function list. Once this spec lands we delete the
`vm/eval_unwind.go:lineForOffset` shim and never look at the
location table from the unwind side again.

Same rule as 1704 / 1705. Different file: the assembler's
location-emission pipeline.

## Why this spec exists

Task #608 wired the VM to populate `exc.__traceback__` so the
stdlib `traceback` module can render frames. That work surfaced a
real gap, on the *compile* side, not the VM side: gopy's assembler
only emits one PEP 657 location entry per source line. Every
instruction after the first in the same line gets folded into a
single trailing "locNone" run.

CPython's assembler does the opposite. `write_location_info_entry`
runs once per instruction, so the location table records the
source position of every codeunit. `PyCode_Addr2Location` is
therefore a direct lookup, never a "walk back to find a real line"
sweep.

Because gopy's table is incomplete, `PyCode_Addr2Location` returns
`-1` for any op past the first one in a source line. That means
the unwind path (`vm/eval_unwind.go:attachFrameTraceback`) cannot
get a real lineno from a raise-site bytecode offset. We worked
around it in PR #26 (task #608) by adding `lineForOffset` which
walks the table and remembers the last positive line seen. That
ships traceback rendering today but is wrong under PEP 657: it
artificially carries a line forward across regions CPython would
mark as "no source line", and it costs an O(n) sweep on every
exception unwind frame.

Spec 1708 replaces the shim with a faithful port of the assembler
location-emission pipeline. After this lands, the unwind path can
go back to a one-shot `PyCode_Addr2Location` lookup that returns
the correct line on the first hit.

## Files in scope

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Python/assemble.c` (location-emission slice: `write_location_info_entry`, `write_location_info_short_form`, `write_location_info_oneline_form`, `write_location_info_long_form`, `write_location_info_no_column`, `write_location_info_none`, plus the loop in `assemble_location_info` that drives them) | ~240 | `compile/assemble.go` (or a new `compile/assemble_location.go` if cleaner) | pending |
| B | `Python/instruction_sequence.c` (per-instruction location storage that feeds A: `_PyInstructionSequence_AddLocation`, `_PyInstructionSequence_Insert`, the `i_loc` field on `instr`) | ~120 | `compile/instruction_sequence.go` | pending audit (some plumbing already exists; needs a one-to-one function audit) |
| C | `Python/compile.c` (the call sites that feed locations into B: `compiler_addop_*`, `_PyCompile_InstrSize`, anything that builds a `location` struct from an `expr_ty`/`stmt_ty`) | varies | `compile/codegen.go` and friends | partial (locations are computed; need to audit every emit site against CPython for "did we drop a location anywhere") |

Sources of truth live under `/Users/apple/cpython-314/`.

## Phase index

Each phase ports one block end to end. The final gate is "the
location table for any compiled function decodes to one
PositionEntry per instruction, with `Line > 0` for every op that
has one in CPython."

| Phase | File | Block | Blocks | Status |
|-------|------|-------|--------|--------|
| 1 | A `assemble.c` | `write_location_info_entry` and the five form-specific writers | - | pending |
| 2 | A `assemble.c` | `assemble_location_info` driver loop (one entry per instruction, with run-length compression matching CPython's short-form rules) | 1 | pending |
| 3 | B `instruction_sequence.c` | audit + finish: every instruction carries an `i_loc` and every insertion path propagates it | - | pending |
| 4 | C `compile.c` | audit emit sites: every `addop_*` call must take a `location`; no synthesized op may default to "unknown" silently | - | pending |
| Gate | - | Decoded position table has `Line > 0` for every non-synthetic codeunit; `lineForOffset` shim deleted; `attachFrameTraceback` calls `CoAddr2Location` directly; existing `TestTracebackFormatExc` still green | 1,2,3,4 | pending |

## Phase 1 - `Python/assemble.c` per-instruction writers

CPython encodes one PEP 657 entry per instruction. Each entry
picks one of six forms based on how the new location compares to
the running cursor.

| C function | gopy hook | Status |
|------------|-----------|--------|
| `write_location_info_entry` | new dispatcher in `compile/assemble.go` | pending |
| `write_location_info_short_form` | helper | pending |
| `write_location_info_oneline_form` | helper | pending |
| `write_location_info_long_form` | helper | pending |
| `write_location_info_no_column` | helper | pending |
| `write_location_info_none` | helper | pending |

### Gate

Round-trip test: pick a known instruction sequence with N ops,
encode it through the new writers, decode through
`objects.CoPositions`, assert exactly N entries come back and each
matches the source location we fed in.

## Phase 2 - `Python/assemble.c` driver

`assemble_location_info` walks the instruction sequence and calls
`write_location_info_entry` for each op, using the previous op's
line as the cursor so deltas stay small. Port the loop, including
the run-length compression CPython's short-form uses.

### Gate

Compile a multi-line function. Decode its position table. Every
codeunit (0, 2, 4, ...) up to the end of the function has a
distinct `PositionEntry` whose `Line` matches the source line of
the op at that offset.

## Phase 3 - `Python/instruction_sequence.c` audit

Walk every function in `instruction_sequence.c` and confirm a
gopy equivalent exists. The risk here is not the missing
functions (most are ported) but missing fields: if `i_loc` is not
propagated through every insertion / fixup path, phase 2 has
nothing to write.

## Phase 4 - `Python/compile.c` emit-site audit

Sweep every `compiler_addop_*` call. Each must pass through a
`location`. Synthetic ops (e.g. exception-table prologues, RESUME)
get an explicit "no location" marker, not the default.

## Gate

After all four phases land:

1. Delete `vm/eval_unwind.go:lineForOffset`.
2. Replace its use in `attachFrameTraceback` with the original
   single-call `objects.CoAddr2Location(co, off)`. No fallback
   chain to `monitor.LineForOffset` or `co.Firstlineno`.
3. Re-run `go test ./stdlibinit/ -run TestTracebackFormatExc` and
   `go test ./...`. All green.
4. Re-run the multi-frame TB probe (or its committed equivalent).
   `tb_lineno` for top/middle/inner matches the source line of
   the raising/calling instruction.

## Checklist

- [ ] Phase 1: per-instruction location writers ported with citations
- [ ] Phase 2: `assemble_location_info` driver ported
- [ ] Phase 3: `instruction_sequence.c` audit complete, `i_loc` propagated end to end
- [ ] Phase 4: `compile.c` emit sites audited, every op carries a location
- [ ] Gate: shim deleted, unwind back to direct lookup, all tests green
