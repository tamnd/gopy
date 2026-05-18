# CPython debug patches for gopy

This directory holds patches that we apply to a local CPython checkout to make
1-to-1 debugging against the gopy port easier. The patches are not shipped in
any release: they exist purely to make divergences observable while we port
spec 1716 (`Python/flowgraph.c`, `Python/assemble.c`, and the
`optimize_and_assemble_code_unit` driver).

## Patches

- `0001-cfg-phase-dump.patch` — Re-enables the `#if 0`'d CFG dumpers in
  `Python/flowgraph.c`, replaces their `fprintf(stderr, ...)` calls with a
  `PyUnicodeWriter` so the dump comes back as a Python string, and inserts a
  per-pass hook call after every optimization step in
  `_PyCfg_OptimizeCodeUnit`. A new `_testinternalcapi.set_cfg_phase_hook(cb)`
  thunk installs the hook. The Python callback receives
  `(phase_name: str, dump_text: str)` once per pass per code unit.

## Applying

```
cd $HOME/github/python/cpython
git apply /Users/apple/github/tamnd/gopy/test/cpython/patches/0001-cfg-phase-dump.patch
./configure --prefix=/tmp/cpython-gopy-debug --with-pydebug --disable-test-modules
make -j8
```

The patched interpreter lives at `./python.exe` on macOS / `./python` on Linux.
Run the harness with `PYTHONHASHSEED=0` so const ordering matches between
runs.

## Phase names emitted

The hook fires after each of these passes, in order, for every code unit:

1. `entry` — graph immediately after `_PyCfgBuilder` hands it to
   `_PyCfg_OptimizeCodeUnit`.
2. `translate_jump_labels_to_targets`
3. `mark_except_handlers`
4. `label_exception_targets`
5. `optimize_cfg`
6. `remove_unused_consts`
7. `add_checks_for_loads_of_uninitialized_variables`
8. `insert_superinstructions`
9. `push_cold_blocks_to_end`
10. `resolve_line_numbers`

## Dump format

Each dump is a UTF-8 string with one block header followed by one line per
instruction:

```
B<label>: [EH=<n> CLD=<n> WRM=<n> NO_FT=<n>] used: <n>, depth: <n>, preds: <n> <return-marker>
  [<idx>] line: <lineno>, <opname> (<opcode>) <arg-or-target> <jump-marker>
```

The format is reproducible. No raw pointers, no addresses. Targets render as
`target: B<label> [<oparg>]`; non-target ops render as `arg: <oparg>`. This is
the exact format that gopy's `compile.DumpCfg` produces.
