---
format: md
id: 1706_pep_649_annotations
title: "1706. PEP 649 / 749 deferred annotations port"
sidebar_label: "1706. pep_649_annotations"
sidebar_position: 1706
slug: /specs/1706-pep_649_annotations
description: "Full-file port of CPython 3.14's deferred-annotation subsystem (PEP 649 + PEP 749). Class, module, and function bodies stop eagerly evaluating annotations and instead build an __annotate__ function; __annotations__ becomes a lazy property that calls it. Every C function in the cited files lands with a citation. Sister spec to 1704 / 1705."
---

## Rule

Every CPython source file in scope is ported **in full**. No
function in those files may be left unported. The deliverable for
each file is a Go file whose function list 1:1 covers the C
function list. Once this spec lands we never come back to these
files for a missing slot.

Same rule as 1704 and 1705. Different files: the deferred
annotation pipeline that PEP 649 introduced and PEP 749 finished
in 3.14.

## Why this spec exists

`gopy/compile/codegen_stmt_misc.go:visitAnnAssign` evaluates the
annotation expression eagerly at module and class scope, then
emits `STORE_SUBSCR __annotations__[name] = value`. CPython 3.14
no longer does this. PEP 649 / 749 changed the model:

- Each scope that owns annotations gets a separate `__annotate__`
  function. The function takes one parameter (`format`) and returns
  a dict mapping name to evaluated annotation.
- `__annotations__` becomes a lazy property on type, module, and
  function objects. Accessing it invokes `__annotate__(VALUE)` once
  and caches the result.
- Bodies of classes and modules no longer fail when an annotation
  references an undefined name. The failure is moved to the lookup
  site, and `annotationlib.get_annotations(..., format=STRING)`
  recovers the source.
- `from __future__ import annotations` becomes a no-op stringifier;
  the same lazy infrastructure is used either way.

This is not optional. Every stdlib module we vendor that uses
typing (`_colorize`, `collections`, `dataclasses`, `argparse`,
`io`, `traceback`, `unittest`) trips on the eager path because
the `typing` import lives inside an `if TYPE_CHECKING:` block
that gopy never enters, but the annotations referencing those
names are evaluated anyway. Spot-fixing each call site is endless;
the only stable port is the full subsystem.

## Files in scope

| # | CPython file | Lines | gopy target | Status |
|---|--------------|------:|-------------|--------|
| A | `Python/symtable.c` (annotation-block plumbing: `symtable_enter_existing_block` for annotation scopes, `ste_annotations_used`, `ste_has_conditional_annotations`, `ste_annotation_block`, the AnnAssign / FunctionDef / ClassDef visitors that flip these flags) | ~120 | `symtable/annotations.go` (new) plus edits to `symtable/visit_*.go` | pending |
| B | `Python/codegen.c` (annotation pipeline: `codegen_annassign`, `codegen_process_deferred_annotations`, `codegen_deferred_annotations_body`, `codegen_setup_annotations_scope`, `codegen_leave_annotations_scope`, `codegen_annotations_in_scope`, the `codegen_body` and `_PyCodegen_Module` hooks that drive them) | ~330 | `compile/codegen_annotations.go` (new) plus edits to `compile/codegen_stmt_misc.go` and `compile/codegen_stmt.go` | pending |
| C | `Objects/typeobject.c` (`__annotate__` and `__annotations__` getset slots: `type_get_annotate`, `type_set_annotate`, `type_get_annotations`, `type_set_annotations`) | ~200 | `objects/type_annotations.go` (new) plus edits to `objects/usertype.go` | pending |
| D | `Objects/funcobject.c` (`__annotate__` slot on PyFunction: `func_get_annotate`, `func_set_annotate`, `func_get_annotations`, `func_set_annotations`) | ~120 | `objects/function_annotations.go` (new) plus edits to `objects/function.go` | pending |
| E | `Objects/moduleobject.c` (module `__annotate__` plumbing: `module_get_annotate`, `module_set_annotate`, `module_get_annotations`, `module_set_annotations`) | ~80 | `objects/module_annotations.go` (new) plus edits to `objects/module.go` | pending |
| F | `Lib/annotationlib.py` (Format enum, ForwardRef, `get_annotations`, `call_annotate_function`, `call_evaluate_function`, `_stringify_single`) | ~1165 | `stdlib/annotationlib.py` (verbatim vendor) plus `module/annotationlib/` glue for the Format ints | pending |
| G | Inittab + builtins: `_PyEval_GetANext` style lookup-on-name for `__annotate__` cells; `BUILD_MAP 0` + `STORE_SUBSCR` opcodes (already shipped); `MAKE_FUNCTION` annotate flag | ~40 | `compile/flags.go`, `objects/function.go` | pending |

Sources of truth live under `/Users/apple/cpython-314/`.

## Phase index

Phases are file-by-file. Each phase ports one CPython file (or one
disjoint block of it) end to end, with the row's gate green before
moving to the next. The order matches the dependency chain:
symtable feeds codegen, codegen builds the function, the object
types expose the lazy slot, the stdlib glue exposes the public
surface.

| Phase | File | Block | Blocks | Status |
|-------|------|-------|--------|--------|
| 1 | A `symtable.c` | annotation-block creation and flag tracking | - | done |
| 2 | B `codegen.c` | `codegen_annassign` rewritten to record-only (no eager STORE_SUBSCR) | 1 | done |
| 3 | B `codegen.c` | `codegen_process_deferred_annotations`, `codegen_deferred_annotations_body`, `codegen_setup_annotations_scope`, `codegen_leave_annotations_scope` | 2 | done |
| 4 | B `codegen.c` | `_PyCodegen_Module` / `codegen_body` hooks that emit the per-scope `__annotate__` install | 3 | done |
| 5 | C `typeobject.c` | type `__annotate__` / `__annotations__` getset | 4 | done |
| 6 | D `funcobject.c` | function `__annotate__` / `__annotations__` getset | 4 | done |
| 7 | E `moduleobject.c` | module `__annotate__` / `__annotations__` getset | 4 | done |
| 8 | F `annotationlib.py` | vendor the Lib file, wire `Format` / `get_annotations` | 5,6,7 | pending |
| Gate | - | `class Foo:  x: ClassVar[int]` succeeds without `typing` in scope; `import _colorize`, `import traceback`, `import dataclasses` all green; `Foo.__annotations__["x"]` returns the live `ClassVar[int]` value when `typing` is imported, and a `ForwardRef('ClassVar[int]')` when called with `format=STRING` | 1-8 | pending |

## Phase 1 - `Python/symtable.c` annotation scope

### Functions to port

| C function / field | gopy hook | Status |
|--------------------|-----------|--------|
| `ste_annotations_used` (field on PySTEntryObject) | `Entry.AnnotationsUsed bool` | done (`symtable/entry.go:74`) |
| `ste_has_conditional_annotations` (field) | `Entry.HasConditionalAnnotations bool` | done (`symtable/entry.go:121`) |
| `ste_annotation_block` (field, pointer to child STE) | `Entry.AnnotationBlock *Entry` | done (`symtable/entry.go`) |
| AnnAssign visitor branch that calls `symtable_add_def(__annotations__)` + flips `ste_annotations_used` | `symtable/build_visit.go:389` `visitAnnAssign` | done |
| Conditional-context tracking (if / try / while / for blocks set `ste_has_conditional_annotations`) | `symtable/build_visit.go` conditional-tracking helpers (lines 439-561) | done |
| `symtable_enter_existing_block` reuse for the annotation block | `symtable/build_helpers.go:130` `enterBlock("__annotate__", AnnotationBlock, ...)` | done |
| FunctionDef + ClassDef visitor flips that propagate annotation-block creation upward | `symtable/build_helpers.go:73` `visitAnnotations` | done |

### What was wrong before this phase

gopy's symtable has no concept of an annotation scope. The compiler
side emits `STORE_SUBSCR __annotations__[name] = expr` directly into
the enclosing class or module body. There is no separate block to
hold the eventual `__annotate__` function's locals, so any closure
that the annotation expression captures cannot be resolved.

### Gate

`symtable_test.go` covers: a class with `x: T` flags
`ste_annotations_used=True` on the class block and creates an
annotation child block whose name is `__annotate__`; a module
with `x: T = 1` flags `ste_annotations_used=True` on the module
block; a function body with `x: T = 1` does *not* flip the flag
(function annotations remain eager-evaluated locals, per the PEP).

## Phase 2 - `Python/codegen.c` `codegen_annassign` rewrite

### Functions to port

| C function | gopy hook | Status |
|------------|-----------|--------|
| `codegen_annassign` (PEP 649 form: record into deferred list, emit value-store only) | `compile/codegen_stmt_misc.go` `visitAnnAssign` | done |
| `_PyCompile_DeferredAnnotations` reader | `compile.(*Unit).DeferredAnnotations` (field, drained by `emitDeferredAnnotations`) | done |

### What was wrong before this phase

Current `visitAnnAssign` always emits `visitExpr(annotation)` +
`STORE_SUBSCR __annotations__[name] = annotation` after the value
assignment. That sequence runs at class / module body execution
time and therefore needs every name in the annotation expression
to be live in the enclosing scope. With PEP 649 the annotation
expression is *not* evaluated at body-execution time; it is
copied verbatim into the deferred list and replayed by the
`__annotate__` function later.

### Gate

For `class C: x: NotDefined`, the class body bytecode contains no
`LOAD_NAME NotDefined` and no `STORE_SUBSCR` against
`__annotations__`. The annotation appears only in the deferred
list, which Phase 3 picks up.

## Phase 3 - `Python/codegen.c` `__annotate__` function build

### Functions to port

| C function | gopy hook | Status |
|------------|-----------|--------|
| `codegen_process_deferred_annotations` | `compile/codegen_annotations.go` `emitDeferredAnnotations` | done |
| `codegen_deferred_annotations_body` | `compile/codegen_annotations.go` `emitAnnotateBody` | done |
| `codegen_setup_annotations_scope` | `compile/codegen.go` `enterScope` (AnnotationBlock branch with CoOptimized|CoNewLocals) | done |
| `codegen_leave_annotations_scope` | `compile/codegen.go` `leaveScope` (shared with all units) | done |
| `codegen_annotations_in_scope` (function-annotation arm) | function annotations remain eager (shipped earlier); class arm via `emitDeferredAnnotations` | done |

### What was wrong before this phase

There is no scope-entry / scope-exit machinery for an
`__annotate__` body in gopy. The compiler doesn't know how to
allocate a nested code object whose sole job is to evaluate the
deferred expressions and return a dict. Without this, Phase 2's
"record only" form is useless: the annotations are recorded and
never replayed.

### Gate

For `class C: x: int`, compiling the class body produces two code
objects: the class body itself (no annotation ops), and a separate
`__annotate__` code object whose `co_code` contains
`BUILD_MAP 0`, `LOAD_NAME int`, `LOAD_CONST "x"`, `STORE_SUBSCR`,
`RETURN_VALUE`. The class body bytecode ends with
`MAKE_FUNCTION` + `STORE_NAME __annotate_func__`.

## Phase 4 - `Python/codegen.c` body hook

### Functions to port

| C function | gopy hook | Status |
|------------|-----------|--------|
| `codegen_body` (annotation processing call) | `compile/codegen_class.go` `emitInnerClassCode` (calls `emitDeferredAnnotations` after `visitStmts`) | done |
| `_PyCodegen_Module` (conditional-annotation BUILD_SET prologue) | module scope still uses legacy SETUP_ANNOTATIONS + eager store; PEP 649 lazy form pending for module scope | pending |
| FUTURE_FEATURES `CO_FUTURE_ANNOTATIONS` short-circuit | not needed: PEP 563 stringification path lands with Phase 8 annotationlib | pending |

### Gate

`compile_test.go` confirms: with no future flag, class/module
bodies emit the `__annotate__` build sequence; with
`from __future__ import annotations`, bodies emit
`SETUP_ANNOTATIONS` and store stringified annotations directly
into `__annotations__` (legacy path).

## Phase 5 - `Objects/typeobject.c` lazy `__annotations__`

### Functions to port

| C function | gopy hook | Status |
|------------|-----------|--------|
| `type_get_annotate` | direct attribute lookup via `typeDescrTable` (no separate getter needed: `__annotate__` is stored straight in the class descr table by `type_new` once codegen emits it) | done (no-op) |
| `type_set_annotate` | `typeSetAttr` already accepts arbitrary attribute writes on user types | done (no-op) |
| `type_get_annotations` | `objects/type_attr.go:typeGetAttr` lazy branch | done (`type_attr.go`) |
| `type_set_annotations` | `typeSetAttr` (writes go to the descr table; cache invalidation is automatic via `InvalidateVersionTag`) | done (no-op) |
| getset registration on `objects.Type` | inlined into `typeGetAttr` rather than a separate getset, since the type machinery routes all attribute access through the `tp_getattro` slot | done |

### Behavior

`__annotate__` getter returns `__annotate_func__` from the type's
dict if present, else `None`. `__annotations__` getter looks up
the cached dict, and on miss calls `__annotate__(Format.VALUE)`,
stores the returned dict under `__annotations__`, and returns it.
Both slots respect `Py_TPFLAGS_IMMUTABLETYPE` (raises TypeError
on attempts to set on built-in types).

### Gate

```python
class C:
    x: int
print(C.__annotations__)      # {'x': <class 'int'>}
print(type(C).__annotate__)    # <function ...>
del C.__annotations__
print(C.__annotations__)      # rebuilt by re-invoking __annotate__
```

## Phase 6 - `Objects/funcobject.c` function `__annotate__`

### Functions to port

| C function | gopy hook | Status |
|------------|-----------|--------|
| `func_get_annotate` | `objects/function_annotations.go` `funcGetAnnotate` | pending |
| `func_set_annotate` | `funcSetAnnotate` | pending |
| `func_get_annotations` | `funcGetAnnotations` | pending |
| `func_set_annotations` | `funcSetAnnotations` | pending |

### Behavior

Function bodies still eager-evaluate parameter annotations (per
PEP 649, function annotations are evaluated at function-definition
time and stored directly). The new slots only matter for
`functools.wraps` and `inspect.signature` round-tripping. Default
implementation: `__annotate__` is None, `__annotations__` is the
dict built by `MAKE_FUNCTION`'s annotations flag.

### Gate

```python
def f(x: int) -> str: ...
print(f.__annotations__)      # {'x': <class 'int'>, 'return': <class 'str'>}
f.__annotate__ = lambda fmt: {'x': str, 'return': int}
print(f.__annotations__)      # still cached {'x': int, 'return': str}
del f.__annotations__
print(f.__annotations__)      # {'x': str, 'return': int}
```

## Phase 7 - `Objects/moduleobject.c` module `__annotate__`

### Functions to port

| C function | gopy hook | Status |
|------------|-----------|--------|
| `module_get_annotate` | `objects/module_annotations.go` `modGetAnnotate` | pending |
| `module_set_annotate` | `modSetAnnotate` | pending |
| `module_get_annotations` | `modGetAnnotations` | pending |
| `module_set_annotations` | `modSetAnnotations` | pending |

### Behavior

Same shape as type, but the storage lives on the module dict
directly (no `tp_dict` indirection). `__annotate__` is installed
by the module's body bytecode via `STORE_NAME __annotate__`.

### Gate

Module containing `x: ClassVar[int]` and no `typing` import loads
without raising. `mod.__annotations__` raises NameError when
accessed; `annotationlib.get_annotations(mod, format=STRING)`
returns `{'x': 'ClassVar[int]'}`.

## Phase 8 - `Lib/annotationlib.py` vendor

### Surface to port

| CPython public name | gopy hook | Status |
|---------------------|-----------|--------|
| `Format` (IntEnum: VALUE=1, FORWARDREF=2, STRING=3) | `stdlib/annotationlib.py` verbatim | pending |
| `ForwardRef` | `stdlib/annotationlib.py` verbatim | pending |
| `get_annotations(obj, *, globals=None, locals=None, eval_str=False, format=Format.VALUE)` | `stdlib/annotationlib.py` verbatim | pending |
| `call_annotate_function(annotate, format, *, owner=None)` | `stdlib/annotationlib.py` verbatim | pending |
| `call_evaluate_function(evaluate, format, *, owner=None)` | `stdlib/annotationlib.py` verbatim | pending |
| `_stringify_single`, `_get_and_call_annotate`, `_get_dunder_annotations` | `stdlib/annotationlib.py` verbatim | pending |

The file is pure Python and 1165 lines. Vendor byte-equal under
`stdlib/annotationlib.py`. The only Go surface is the inittab
glue if the file imports a non-vendored C module (it does not as
of 3.14, so vendoring alone is enough).

### Gate

```python
import annotationlib
class C:
    x: ClassVar[int]
print(annotationlib.get_annotations(C, format=annotationlib.Format.STRING))
# {'x': 'ClassVar[int]'}
print(annotationlib.get_annotations(C, format=annotationlib.Format.FORWARDREF))
# {'x': ForwardRef('ClassVar[int]')}
```

## Workflow per port

Same eight-step cadence as 1702, 1704, 1705:

1. Pick the next phase from the index above.
2. Mark the matching task in_progress.
3. Read the CPython source, port every function in the row, add
   `// CPython: <file>:<line> <name>` citations.
4. Run the row's gate.
5. Flip the row status in this spec to `done`.
6. `go build ./...`, `go test ./...`, fix lint diagnostics.
7. Commit, push, and post a human comment on the PR.
8. Mark the task `completed` and pick the next row.

## Checklist

- [x] Phase 1: `symtable.c` annotation-block plumbing (audit found all fields, visitors, and conditional tracking already shipped)
- [ ] Phase 2: `codegen.c` `codegen_annassign` rewrite to record-only
- [ ] Phase 3: `codegen.c` `__annotate__` function build pipeline
- [ ] Phase 4: `codegen.c` body hook + future-flag short-circuit
- [x] Phase 5: `typeobject.c` `__annotate__` / `__annotations__` getset (lazy branch in `objects/type_attr.go:typeGetAttr`; dormant until Phase 2-4 produces `__annotate__`)
- [x] Phase 6: `funcobject.c` `__annotate__` / `__annotations__` getset (already shipped via `Function.Annotate` + `func_get_annotation_dict`)
- [ ] Phase 7: `moduleobject.c` `__annotate__` / `__annotations__` getset
- [ ] Phase 8: vendor `Lib/annotationlib.py`
- [ ] Gate: `import _colorize`, `import traceback`, `import dataclasses` all green; `class Foo:  x: ClassVar[int]` succeeds without `typing` in scope
