---
title: Specs
sidebar_label: Overview
slug: /specs
description: Design specifications for gopy. Each spec is a long-form plan covering one subsystem of the CPython port.
---

# Specs

This is the canonical specification set for gopy. Each spec is a
long-form plan for one part of the CPython port. We write the spec
before the code, keep it in sync as the port lands, and use it as
the source of truth when something behaves surprisingly.

Specs are split into two series:

- The **1600 series** covers the runtime port itself, organized by
  CPython subsystem. Each number maps to a slice of CPython's source
  tree: parser, compiler, VM, object protocol, GC, imports, codecs,
  and so on. If you want to know how a CPython function gets to Go,
  start here.
- The **1700 series** covers the test gate and porting workflow. It
  catalogs the end-to-end test corpus, tracks which subsystems have
  been audited for full ports, and records the lessons learned while
  vendoring CPython sources.

## 1600 series: runtime port

### Foundations
- [1600. Overview](./1600/1600_gopy_overview.md). Top-level goals,
  scoping, and what 100% behavioural compatibility means in practice.
- [1601. Naming conventions](./1600/1601_gopy_naming.md). The
  translation table from CPython C names to Go-idiomatic identifiers.
- [1602. File map](./1600/1602_gopy_filemap.md). How CPython source
  files map to gopy packages.
- [1603. Roadmap](./1600/1603_gopy_roadmap.md). Release-by-release
  porting plan.

### Runtime infrastructure
- [1604. Arena allocator](./1600/1604_gopy_arena.md)
- [1605. PyThread](./1600/1605_gopy_pythread.md)
- [1606. PySync primitives](./1600/1606_gopy_pysync.md)
- [1607. Hash secret](./1600/1607_gopy_hashsecret.md)
- [1611. Errors and exceptions infrastructure](./1600/1611_gopy_errors.md)
- [1613. Garbage collector](./1600/1613_gopy_gc.md)
- [1668. Runtime helpers](./1600/1668_gopy_runtime_helpers.md)

### Compile pipeline
- [1620. Compile pipeline overview](./1600/1620_gopy_compile_pipeline.md)
- [1621. Bytecodes DSL](./1600/1621_gopy_bytecodes_dsl.md)
- [1622. Lifecycle](./1600/1622_gopy_lifecycle.md)
- [1624. PythonRun](./1600/1624_gopy_pythonrun.md)
- [1625. Compile testing](./1600/1625_gopy_compile_testing.md)
- [1626. Codegen](./1600/1626_gopy_codegen.md)
- [1627. Flowgraph](./1600/1627_gopy_flowgraph.md)
- [1628. Assembler](./1600/1628_gopy_assemble.md)
- [1629. Compile goldens](./1600/1629_gopy_compile_goldens.md)

### Virtual machine
- [1630. VM overview](./1600/1630_gopy_vm_overview.md)
- [1635. Intrinsics](./1600/1635_gopy_intrinsics.md)
- [1636. Eval loop](./1600/1636_gopy_eval_loop.md)
- [1637. Frame](./1600/1637_gopy_frame.md)
- [1638. Stackref](./1600/1638_gopy_stackref.md)
- [1639. Eval / GIL](./1600/1639_gopy_eval_gil.md)
- [1693. VM remaining bytecodes](./1600/1693_gopy_vm_remaining.md)

### Parser
- [1640. Parser overview](./1600/1640_gopy_parser_overview.md)
- [1641. Lexer and tokenizer](./1600/1641_gopy_lexer_tokenizer.md)
- [1642. PEG generator](./1600/1642_gopy_pegen.md)
- [1643. Parser errors](./1600/1643_gopy_parser_errors.md)
- [1644. String parser](./1600/1644_gopy_string_parser.md)
- [1645. MyReadline](./1600/1645_gopy_myreadline.md)
- [1665. Tokenize module](./1600/1665_gopy_tokenize.md)

### Strings, numbers, hashing
- [1660. Strings and numbers](./1600/1660_gopy_strings_numbers.md)
- [1661. Hash](./1600/1661_gopy_hash.md)
- [1662. HAMT](./1600/1662_gopy_hamt.md)
- [1663. Context](./1600/1663_gopy_context.md)
- [1664. Time](./1600/1664_gopy_time.md)

### Objects
- [1670. Objects overview](./1600/1670_gopy_objects_overview.md)
- [1671. Object protocol](./1600/1671_gopy_object_protocol.md)
- [1672. Type object](./1600/1672_gopy_type.md)
- [1673. Long](./1600/1673_gopy_long.md)
- [1674. Float and complex](./1600/1674_gopy_float_complex.md)
- [1675. Bool and None](./1600/1675_gopy_bool_none.md)
- [1676. Bytes](./1600/1676_gopy_bytes.md)
- [1677. Unicode](./1600/1677_gopy_unicode.md)
- [1678. Tuple](./1600/1678_gopy_tuple.md)
- [1679. List](./1600/1679_gopy_list.md)
- [1680. Dict](./1600/1680_gopy_dict.md)
- [1681. Set](./1600/1681_gopy_set.md)
- [1682. Slice and range](./1600/1682_gopy_slice_range.md)
- [1683. Abstract object protocol](./1600/1683_gopy_abstract.md)
- [1684. Call protocol](./1600/1684_gopy_call.md)
- [1685. Descriptors and methods](./1600/1685_gopy_descr_method.md)
- [1686. Exceptions hierarchy](./1600/1686_gopy_exceptions.md)
- [1687. Code, frame, generator](./1600/1687_gopy_code_frame_gen.md)
- [1688. Module and misc](./1600/1688_gopy_module_misc.md)
- [1689. Object misc](./1600/1689_gopy_obj_misc.md)

### Imports, marshalling, codecs
- [1651. Modules](./1600/1651_gopy_modules.md)
- [1690. Marshal](./1600/1690_gopy_marshal.md)
- [1691. Import](./1600/1691_gopy_import.md)
- [1692. Codecs](./1600/1692_gopy_codecs.md)

### Optimizer and instrumentation
- [1694. Specialize](./1600/1694_gopy_specialize.md)
- [1695. Instrumentation](./1600/1695_gopy_instrumentation.md)
- [1696. Legacy tracing](./1600/1696_gopy_legacy_tracing.md)
- [1697. Optimizer overview](./1600/1697_gopy_optimizer_overview.md)
- [1698. Optimizer uops](./1600/1698_gopy_optimizer_uops.md)
- [1699. Optimizer analysis](./1600/1699_gopy_optimizer_analysis.md)

## 1700 series: test gate and porting workflow

- [1700. End-to-end test suite](./1700/1700_gopy_test_e2e.md).
  Master plan for porting CPython's `Lib/test/` corpus.
- [1701. Unittest enablement](./1700/1701_unittest_enablement.md).
  Bring up the `unittest` package end-to-end so test files can run.
- [1702. Subsystem port log](./1700/1702_subsystem_port_log.md).
  Per-module ledger of "ported in full" vs "still a shim".
- [1703. `re` and `_sre` full port](./1700/1703_re_sre_full_port.md).
  Replace the RE2 fallback with a CPython-faithful interpreter.
- [1704. Object protocol full port](./1700/1704_object_protocol_full_port.md).
  Eight-phase plan to bring `Objects/object.c` to parity.

## How to read a spec

Every spec follows the same shape:

1. **Goal**. What problem the spec is solving and the constraint
   (usually "match CPython 1:1").
2. **Sources**. The CPython files and line ranges the spec covers.
3. **Mapping**. The Go names, packages, and field layouts the port
   uses, with the CPython equivalents alongside.
4. **Checklist** (1700 series). The shipped vs. pending steps. We
   keep this current as work lands.
5. **Notes**. Edge cases, surprises from the CPython source, and
   anything that diverged and why.

When a spec disagrees with the code, the code is right and the spec
is the bug. We update the spec, not the runtime, unless the runtime
itself is wrong.
