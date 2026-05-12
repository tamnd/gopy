---
format: md
id: 1602_gopy_filemap
title: "1602. gopy file map"
sidebar_label: "1602. filemap"
sidebar_position: 1602
slug: /specs/1602-filemap
description: "One-line per CPython source file, with the target Go package path and a short purpose note. Canonical lookup table for porters."
---


The mapping is **one-to-many**: a CPython file may split across several Go
files (or vice-versa). Where multiple C files collapse into one Go package,
the package's `doc.go` lists the source files it derives from.

Conventions:
- All Go packages live at the **module root** (`gopy/<pkg>`). We do **not**
  use Go's `internal/` directory convention. See 1601 for rationale.
- Files named like `dynload_*.c` and `emscripten_*.c` are platform glue we
  do not port. Go's runtime handles the equivalents. They are listed here
  for completeness with a `--` Go target.
- Generated headers (`generated_cases.c.h`, `executor_cases.c.h`,
  `optimizer_cases.c.h`, `opcode_targets.h`) are regenerated from the
  CPython DSL via our own Go-emitting code generator. See 1621.

## Coverage as of v0.12.0

The single source of truth is `$HOME/github/python/cpython/Python/*.c`,
audited at v0.12.0 against the gopy module root.

```
Total CPython Python/*.c files .........  100
  Ported (file-level present) ..........   74  (74%)
  Dropped (intentionally not ported) ...   16  (16%)
  Missing (port pending) ...............   10  (10%)

In-scope coverage (excludes dropped) ...  88.1%
```

The status column in each table below uses:
- `[x]` ported. The named Go target exists in the gopy tree and the
  corresponding spec is implemented (modulo deferred sub-features
  tracked under that spec's own checklist).
- `[~]` partial. Skeleton or partial port shipped; remaining CPython
  functions are tracked as dedicated tasks. v0.12 examples:
  `optimizer_analysis.c` (orchestrator + cleanup landed; per-opcode
  cases and watcher arm pending).
- `[ ]` missing. Port not yet started.
- `[-]` dropped. Intentionally not ported per filemap conventions
  (platform glue, native dynamic-loading, JIT, perf trampolines).

Per-spec checklists may surface sub-file gaps inside `[x]` files.
Those are tracked against the relevant 1620 / 1630 / 1660 / 1680 /
1690 series spec, not here.

## Compiler pipeline

| Status | C file                          | Go target                                     | Purpose                                          |
|--------|---------------------------------|-----------------------------------------------|--------------------------------------------------|
| `[x]`  | `Python/asdl.c`                 | `ast/asdl.go`                                 | ASDL sequence helpers (`asdl_seq_*`)             |
| `[x]`  | `Python/Python-ast.c`           | `ast/nodes_gen.go` (generated)                | Generated AST node constructors                  |
| `[x]`  | `Python/ast.c`                  | `ast/validate.go`                             | `_PyAST_Validate`                                |
| `[x]`  | `Python/ast_preprocess.c`       | `ast/preprocess.go`                           | Constant fold, PEP 765 control-flow checks       |
| `[x]`  | `Python/ast_unparse.c`          | `ast/unparse.go`                              | AST → source (annotations)                       |
| `[x]`  | `Python/symtable.c`             | `symtable/`                                   | Two-pass symbol table builder                    |
| `[x]`  | `Python/future.c`               | `future/future.go`                            | `__future__` extraction                          |
| `[x]`  | `Python/compile.c`              | `compile/compiler.go`                         | Pipeline orchestration                           |
| `[x]`  | `Python/codegen.c`              | `compile/codegen.go` plus `codegen_*.go` panel | AST → instruction sequence (split per stmt/expr family) |
| `[x]`  | `Python/instruction_sequence.c` | `compile/instrseq.go`                         | Labeled instruction sequence                     |
| `[x]`  | `Python/flowgraph.c`            | `compile/flowgraph.go`, `flowgraph_passes.go`, `flowgraph_stackdepth.go` | CFG build + optimization panel + stackdepth |
| `[x]`  | `Python/assemble.c`             | `compile/assemble.go`, `assemble_locations.go`, `assemble_exceptions.go`, `assemble_varint.go` | Bytecode + PEP 626 line table + PEP 657 exception table |
| `[x]`  | `Lib/dis.py`                    | `compile/dis.go`                              | Disassembler used by the v05test gate            |
| `[x]`  | (none)                          | `compile/code.go`                             | `Code` value type, mirrors `PyCodeObject`        |
| `[x]`  | (generator output)              | `compile/opcodes_gen.go`                      | Opcode constants and metadata (generated)        |

## Bytecode interpreter & frame

| Status | C file                                | Go target                                                  | Purpose                                  |
|--------|---------------------------------------|------------------------------------------------------------|------------------------------------------|
| `[x]`  | `Python/ceval.c`                      | `vm/eval.go`, `vm/eval_simple.go`, `vm/eval_*.go` panel    | Eval loop entry & unwind                 |
| `[x]`  | `Python/ceval_gil.c`                  | `gil/gil.go`, `vm/eval_gil.go`                             | GIL acquire/release, eval breaker        |
| `[x]`  | `Python/ceval_macros.h`               | `vm/dispatch.go`                                           | Dispatch helpers                         |
| `[x]`  | `Python/bytecodes.c`                  | (input to generator)                                       | Source-of-truth ISA                      |
| `[x]`  | `Python/generated_cases.c.h`          | `vm/opcodes_gen.go` (generated)                            | Tier-1 dispatch handlers                 |
| `[x]`  | `Python/opcode_targets.h`             | `vm/opcode_targets_gen.go` (generated)                     | Computed-goto table replaced by switch   |
| `[x]`  | `Python/frame.c`                      | `frame/frame.go`                                           | `PyFrameObject`                          |
| `[x]`  | `Python/stackrefs.c`                  | `stackref/stackref.go`                                     | Tagged stack references                  |

## Specializer / optimizer / JIT / instrumentation

| Status | C file                          | Go target                                | Purpose                                      |
|--------|---------------------------------|------------------------------------------|----------------------------------------------|
| `[x]`  | `Python/specialize.c`           | `specialize/`                            | PEP 659 adaptive specialization              |
| `[x]`  | `Python/optimizer.c`            | `optimizer/optimize.go`, `executor.go`, `trace.go`, `side_table.go`, `pyobject.go` | Trace projection, executor build |
| `[~]`  | `Python/optimizer_analysis.c`   | `optimizer/analysis.go`                  | Orchestrator + cleanup pass landed; per-opcode arms and `removeGlobals` body pending |
| `[~]`  | `Python/optimizer_bytecodes.c`  | `tools/uops_gen/` (input to generator)   | Tier-2 abstract-interp DSL; parser scaffold landed, case-body emitter pending |
| `[x]`  | `Python/optimizer_symbols.c`    | `optimizer/symbols.go`                   | Symbol lattice                               |
| `[ ]`  | `Python/optimizer_cases.c.h`    | `optimizer/uops_cases_gen.go` (generated) | Abstract interp cases (pending v0.13)        |
| `[ ]`  | `Python/executor_cases.c.h`     | `optimizer/executor_cases_gen.go` (generated) | Tier-2 interpreter cases (pending v0.13) |
| `[-]`  | `Python/jit.c`                  | `--`                                     | Copy-and-patch JIT, deferred indefinitely    |
| `[x]`  | `Python/instrumentation.c`      | `monitor/`                               | PEP 669 monitoring                           |
| `[x]`  | `Python/legacy_tracing.c`       | `monitor/sysmonitoring.go`, `vm/legacy_tracing.go` | sys.settrace / sys.setprofile      |
| `[x]`  | `Python/intrinsics.c`           | `intrinsics/intrinsics.go`               | INTRINSIC_1 / INTRINSIC_2                    |

## State, lifecycle, init, run

| Status | C file                          | Go target                                | Purpose                                      |
|--------|---------------------------------|------------------------------------------|----------------------------------------------|
| `[x]`  | `Python/pystate.c`              | `state/`                                 | Runtime / Interpreter / Thread               |
| `[x]`  | `Python/pylifecycle.c`          | `lifecycle/init.go`, `finalize.go`, `main.go` | Initialize / Finalize phases            |
| `[x]`  | `Python/initconfig.c`           | `initconfig/config.go`                   | `PyConfig`                                   |
| `[x]`  | `Python/preconfig.c`            | `initconfig/preconfig.go`                | `PyPreConfig`                                |
| `[x]`  | `Python/interpconfig.c`         | `initconfig/interpconfig.go`             | Per-interpreter config                       |
| `[x]`  | `Python/pathconfig.c`           | `pathconfig/pathconfig.go`               | sys.path, prefix, exec_prefix                |
| `[x]`  | `Python/bootstrap_hash.c`       | `hash/secret.go`                         | PYTHONHASHSEED bootstrap                     |
| `[x]`  | `Python/pythonrun.c`            | `pythonrun/`                             | REPL, file/string eval                       |
| `[x]`  | `Python/frozen.c`               | `imp/frozen.go`                          | Frozen module table                          |
| `[x]`  | `Python/frozenmain.c`           | `cmd/gopy-frozen/`                       | Embedded entry point                         |
| `[x]`  | `Python/import.c`               | `imp/import.go`                          | importlib bootstrap, sys.modules             |
| `[-]`  | `Python/importdl.c`             | `--`                                     | Native .so/.pyd loading, dropped (Go has no dlopen path for CPython extensions) |
| `[x]`  | `Python/marshal.c`              | `marshal/marshal.go`                     | .pyc wire format                             |
| `[ ]`  | `Python/crossinterp.c`          | `crossinterp/` (pending)                 | XIData, sub-interpreter handoff              |

## Errors, modules, codecs

| Status | C file                          | Go target                                | Purpose                                      |
|--------|---------------------------------|------------------------------------------|----------------------------------------------|
| `[x]`  | `Python/errors.c`               | `errors/errors.go`                       | `PyErr_*` exception protocol                 |
| `[x]`  | `Python/traceback.c`            | `traceback/traceback.go`                 | Traceback objects, formatting                |
| `[x]`  | `Python/suggestions.c`          | `errors/suggest.go`                      | "Did you mean…?" hints                       |
| `[x]`  | `Python/bltinmodule.c`          | `builtins/`                              | `__builtins__`                               |
| `[x]`  | `Python/sysmodule.c`            | `sys/`                                   | `sys`                                        |
| `[x]`  | `Python/_warnings.c`            | `warnings/warnings.go`                   | `warnings`                                   |
| `[x]`  | `Python/_contextvars.c`         | `contextvar/module.go`                   | `_contextvars` C module                      |
| `[x]`  | `Python/codecs.c`               | `codecs/codecs.go`                       | Codec registry, error handlers               |
| `[-]`  | `Python/modsupport.c`           | `objects/module.go` + per-call-site Go constructors | Drop. `Py_BuildValue` is varargs + format-string construction with no Go equivalent (call sites use `NewTuple` / `NewDict` / `NewLong` directly). `PyModule_AddObject` family is replaced by `module.Dict().SetItemString(...)`. |
| `[-]`  | `Python/structmember.c`         | `objects/member.go` (semantic equivalent)| `tp_members` descriptors. Drop. PyMember_Get/SetOne pun a `char* + offset` plus a type tag into typed loads/stores; Go has no runtime equivalent. gopy realises the same descriptor semantics via `objects.MemberDescr` using slot indices. |
| `[-]`  | `Python/getargs.c`              | per-call-site Go function signatures     | Drop. `PyArg_ParseTuple` / `PyArg_ParseTupleAndKeywords` are varargs + format-string parsers used only by C extension modules. gopy has no C extension surface; built-in Go functions take Go-typed args directly. |

## Memory, GC, concurrency

| Status | C file                          | Go target                                | Purpose                                      |
|--------|---------------------------------|------------------------------------------|----------------------------------------------|
| `[x]`  | `Python/gc.c`                   | `gc/collector.go`                        | Generational cycle collector (GIL)           |
| `[x]`  | `Python/gc_gil.c`               | `gc/gil.go`                              | GIL-build helpers                            |
| `[ ]`  | `Python/gc_free_threading.c`    | `gc/freethreading.go` (pending)          | nogil cycle collector                        |
| `[x]`  | `Python/brc.c`                  | `brc/brc.go`                             | Biased reference counting                    |
| `[x]`  | `Python/qsbr.c`                 | `gc/qsbr.go`                             | Quiescent-state-based reclamation            |
| `[~]`  | `Python/uniqueid.c`             | `gc/uniqueid.go`                         | Pool half ported; per-thread refcount-merge half deferred (see task #465) |
| `[x]`  | `Python/index_pool.c`           | `gc/indexpool.go`                        | Free index allocator                         |
| `[x]`  | `Python/object_stack.c`         | `gc/objstack.go`                         | GC mark stack                                |
| `[x]`  | `Python/pyarena.c`              | `arena/arena.go`                         | Bump arena (compiler scratch)                |
| `[x]`  | `Python/thread.c`               | `pythread/thread.go`                     | Thread create/join                           |
| `[x]`  | `Python/lock.c`                 | `pysync/lock.go`                         | `PyMutex`, `_PyOnceFlag`                     |
| `[x]`  | `Python/parking_lot.c`          | `pysync/parkinglot.go`                   | WebKit-style park/unpark                     |
| `[x]`  | `Python/critical_section.c`     | `pysync/criticalsection.go`              | Per-object critical sections (nogil)         |

> **Note on `pysync`**: we use the package name `pysync` (not `sync`) to
> avoid shadowing Go's standard library `sync` package inside the gopy
> module, since these CPython primitives have semantics distinct from
> `sync.Mutex` / `sync.Cond`.

## Strings, numbers, hashing, time, hamt, context

| Status | C file                          | Go target                                | Purpose                                      |
|--------|---------------------------------|------------------------------------------|----------------------------------------------|
| `[x]`  | `Python/formatter_unicode.c`    | `format/format.go`                       | `__format__` mini-language                   |
| `[x]`  | `Python/pystrtod.c`             | `pystrconv/strtod.go`                    | strtod wrapper (calls dtoa)                  |
| `[x]`  | `Python/dtoa.c`                 | `pystrconv/dtoa.go`                      | David Gay shortest-roundtrip dtoa            |
| `[x]`  | `Python/pystrhex.c`             | `pystrconv/hex.go`                       | bytes-to-hex helpers                         |
| `[x]`  | `Python/pystrcmp.c`             | `pystrconv/cmp.go`                       | locale-independent strcmp                    |
| `[x]`  | `Python/mystrtoul.c`            | `pystrconv/strtoul.go`                   | int parsing                                  |
| `[-]`  | `Python/mysnprintf.c`           | `--` (use `fmt.Sprintf`)                 | n/a in Go                                    |
| `[x]`  | `Python/pyhash.c`               | `hash/hash.go`                           | SipHash-1-3, FNV, x86_aes                    |
| `[x]`  | `Python/pyctype.c`              | `pystrconv/ctype.go`                     | ASCII isalpha/isdigit (locale-independent)   |
| `[x]`  | `Python/pymath.c`               | `pymath/pymath.go`                       | math primitives, NaN/Inf                     |
| `[x]`  | `Python/pyfpe.c`                | `pymath/fpe.go`                          | float-point exception handling               |
| `[x]`  | `Python/hamt.c`                 | `hamt/hamt.go`                           | Hash array mapped trie (contextvars)         |
| `[x]`  | `Python/hashtable.c`            | `hashtable/hashtable.go`                 | Internal hash table for caches               |
| `[x]`  | `Python/context.c`              | `contextvar/context.go`                  | PEP 567 contextvars                          |
| `[x]`  | `Python/pytime.c`               | `pytime/pytime.go`                       | monotonic, perf_counter, FromSeconds         |
| `[x]`  | `Python/Python-tokenize.c`      | `tokenize/tokenize.go`                   | tokenizer C-API hooks                        |
| `[x]`  | `Grammar/Tokens` plus `Include/internal/pycore_token.h` plus `Lib/token.py` | `tokenize/types_gen.go` (generated by `tools/tokens_go`), `tokenize/types.go` | Token kind constants and `Type.String` |
| `[x]`  | `Python/getopt.c`               | `getopt/getopt.go`                       | argv parsing for python.exe                  |
| `[ ]`  | `Python/tracemalloc.c`          | `tracemalloc/` (pending)                 | allocation tracing                           |
| `[-]`  | `Python/remote_debugging.c`     | `--`                                     | Remote debug hooks; relies on Linux ptrace + Mach syscalls; deferred indefinitely |

> **Note on `pystrconv`**: same reasoning as `pysync`. Avoids colliding
> with Go's standard library `strconv`. `pystrconv` holds the
> Python-specific string/number routines (dtoa, locale-independent
> strcmp, mystrtoul). Go's `strconv` is used for plain Go conversions.

## Misc / drop list

| Status | C file                          | Go target / disposition       | Note                                         |
|--------|---------------------------------|-------------------------------|----------------------------------------------|
| `[x]`  | `Python/getversion.c`           | `build/version.go`            | Hard-coded version string                    |
| `[x]`  | `Python/getplatform.c`          | `build/platform.go`           | `runtime.GOOS`/`runtime.GOARCH` based        |
| `[x]`  | `Python/getcompiler.c`          | `build/compiler.go`           | "gopy 0.x using go1.X" string                |
| `[x]`  | `Python/getcopyright.c`         | `build/copyright.go`          | static copyright string                      |
| `[x]`  | `Python/getopt.c`               | `getopt/`                     | Already listed above                         |
| `[-]`  | `Python/dynload_shlib.c`        | `--`                          | Drop. Go does not load `.so` extensions      |
| `[-]`  | `Python/dynload_win.c`          | `--`                          | Drop                                         |
| `[-]`  | `Python/dynload_hpux.c`         | `--`                          | Drop                                         |
| `[-]`  | `Python/dynload_stub.c`         | `--`                          | Drop                                         |
| `[-]`  | `Python/dup2.c`                 | `--`                          | Use Go's `os` package                        |
| `[-]`  | `Python/dynamic_annotations.c`  | `--`                          | TSAN annotations; drop                       |
| `[-]`  | `Python/perf_jit_trampoline.c`  | `--`                          | perf-jit trampoline; drop                    |
| `[-]`  | `Python/perf_trampoline.c`      | `--`                          | perf trampoline; drop                        |
| `[-]`  | `Python/asm_trampoline.S`       | `--`                          | asm; drop                                    |
| `[-]`  | `Python/emscripten_*.c`         | `--`                          | Emscripten platform; drop                    |
| `[ ]`  | `Python/fileutils.c`            | `fileutils/` (pending)        | Path helpers (only the bits not in `os`/`filepath`) |
| `[-]`  | `Python/condvar.h`              | `--`                          | Use `sync.Cond`                              |
| `[-]`  | `Python/config_common.h`        | `--`                          | autoconf glue                                |
| `[-]`  | `Python/thread_pthread.h`       | `--`                          | Use Go runtime                               |
| `[-]`  | `Python/thread_pthread_stubs.h` | `--`                          | Drop                                         |
| `[-]`  | `Python/thread_nt.h`            | `--`                          | Use Go runtime                               |
| `[ ]`  | `Python/crossinterp_*.h`        | `crossinterp/types.go` (pending) | XIData type registry                      |
| `[-]`  | `Python/remote_debug.h`         | `--`                          | header for dropped `remote_debugging.c`      |
| `[x]`  | `Python/stdlib_module_names.h`  | `imp/stdlib_names_gen.go`     | Generated list of stdlib module names        |

## Pending ports

The remaining in-scope CPython files that still need a Go target as
of v0.12.0. Each entry has its own task; see the v0.13+ spec backlog.
Dropped files (jit, importdl, remote_debugging, dynload_*,
emscripten_*, structmember, etc.) are **not** listed here because they
will not be ported.

| C file                          | Target Go path (planned)      | Needed for                                   |
|---------------------------------|-------------------------------|----------------------------------------------|
| `Python/crossinterp.c`          | `crossinterp/`                | sub-interpreter object handoff (PEP 734)     |
| `Python/fileutils.c`            | `fileutils/`                  | `_Py_wgetcwd`, encoded-path helpers stdlib relies on |
| `Python/gc_free_threading.c`    | `gc/freethreading.go`         | nogil cycle collector (build-tag gated)      |
| `Python/tracemalloc.c`          | `tracemalloc/`                | `tracemalloc` allocation tracing             |
| `Python/uniqueid.c` (refcount half) | `gc/uniqueid.go`          | per-thread refcount merge / disable; needs nogil refcount infra (task #465) |

The nogil index-pool (`index_pool.c`), QSBR (`qsbr.c`), and the unique-id
pool half (`uniqueid.c`) landed in v0.12.0 as inert infrastructure under
`gc/`. The C-extension host surface (`getargs`, `modsupport`) stays
pending until a real consumer lands. `crossinterp`, `fileutils`, and
`tracemalloc` are sequenced behind their consuming subsystems.

## Go top-level layout

The actual layout as of v0.12.0. Where it differs from the per-file map
above (e.g. `frame/` not `vm/frame.go`), the per-file map is the
authoritative target.

```
gopy/
├── go.mod                        # module tamnd/gopy
├── cmd/
│   ├── gopy/                     # main interpreter entry (mirror of python.c)
│   └── gopy-frozen/              # embedded-only entry (mirror of frozenmain.c)
├── abstract/                     # PyNumber / PySequence / PyMapping
├── arena/
├── ast/
├── brc/                          # biased reference counting (split from gc/)
├── build/                        # version/platform/compiler/copyright strings
├── builtins/                     # __builtins__ module
├── changelog/                    # per-release fragments
├── codecs/
├── compile/                      # codegen, flowgraph, assemble, instrseq
├── contextvar/
├── errors/
├── format/
├── frame/                        # PyFrameObject (split from vm/)
├── future/
├── gc/
├── getopt/
├── gil/                          # ceval_gil split
├── hamt/
├── hash/
├── hashtable/
├── imp/                          # import + frozen + stdlib_names; see 1612
├── initconfig/
├── intrinsics/
├── lifecycle/
├── marshal/
├── monitor/                      # PEP 669 + sys.settrace bridge
├── myreadline/                   # readline stub
├── objects/                      # NB: cpython/Objects/* lives in a separate spec series
├── optimizer/
├── parser/                       # PEG parser
├── pathconfig/
├── pymath/
├── pystrconv/                    # Python-specific str/number conversion
├── pysync/                       # Python-specific sync primitives
├── pythonrun/
├── pythread/
├── pytime/
├── specialize/
├── stackref/                     # tagged stack references (split from vm/)
├── state/
├── symtable/
├── sys/                          # sysmodule
├── token/
├── tokenize/
├── tools/                        # code generators (asdl_go, opcodes_go, parser_gen, tokens_go, uops_gen, bytecodes_gen)
├── traceback/
├── vm/
├── warnings/
├── weakref/
├── v04test/, v05test/, v012test/ # per-release end-to-end gates
├── objtest/, partest/, vmtest/   # per-subsystem gates
└── compat/                       # cross-runtime golden tests (planned)
    ├── bytecode/
    ├── marshal/
    └── hash/
```

### Pending package dirs

These directories are listed in the per-file map but not yet present in
the gopy tree. Each is a target for one of the ten pending ports above:

```
gopy/
├── crossinterp/        # Python/crossinterp.c
├── fileutils/          # Python/fileutils.c
├── getargs/            # Python/getargs.c
├── modsupport/         # Python/modsupport.c
├── structmember/       # Python/structmember.c
└── tracemalloc/        # Python/tracemalloc.c
```

## On flat layout vs `internal/`

Go's `internal/` directory convention restricts imports to packages within
the same module subtree. We deliberately **do not** use it for gopy, for
three reasons:

1. **Embedders**: third-party Go programs that embed gopy as a Python
   runtime (think `python -c` style execution from Go code) need to import
   `vm`, `state`, `lifecycle`, `compile` etc. directly. Burying them under
   `internal/` would block that.
2. **Stdlib re-implementations**: the Go-native ports of CPython's
   `Modules/*` and `Lib/*` live in companion modules (`tamnd/gopy-stdlib`,
   etc.). Those need to import from this module's runtime packages.
3. **Tooling and tests**: `compat/` and external golden-test harnesses
   import the same packages the runtime uses. `internal/` would force
   awkward indirections.

Discoverability is preserved by the package name itself. There is no
ambiguity that `compile`, `vm`, `gc`, `frame` etc. belong to the gopy
runtime.
