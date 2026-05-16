"""Generate gopy's typed inline-cache accessors.

Reads Include/internal/pycore_code.h, finds every
`typedef struct { ... } _Py<Op>Cache;` block, and emits
specialize/cache_layouts_gen.go with one typed wrapper per struct:

    type LoadGlobalCache struct { code []uint16; instr int }
    func LoadGlobalCacheAt(code []uint16, instr int) LoadGlobalCache
    func (c LoadGlobalCache) Counter() uint16
    func (c LoadGlobalCache) SetCounter(v uint16)
    func (c LoadGlobalCache) Index() uint16
    func (c LoadGlobalCache) SetIndex(v uint16)
    ...

Field offsets are derived from struct member order. Each field
occupies one codeunit (uint16). The _Py_BackoffCounter type maps
to Counter()/SetCounter() that returns the raw uint16; the
specialize.BackoffCounter wrapper sits on top in hand-written code.

Unions (the LoadMethodCache `keys_version`/`dict_offset` alias)
emit two accessors at the same offset; callers pick the spelling
that matches the path they are on.

The generated file ships with //go:build ignore until Phase 3.3+
migrates every SetCacheCell/CacheCell caller. During that window
the size parity test reads both this file and the hand-rolled
cache.go as text.

Spec: website/docs/specs/1700/1714_bytecodes_dsl_codegen.md
"""

from __future__ import annotations

import argparse
import re
from pathlib import Path
from typing import TextIO

from go_generators_common import write_go_header


DEFAULT_INPUT = "Tools/cases_generator/inputs/Include/internal/pycore_code.h"
DEFAULT_OUTPUT = "specialize/cache_layouts_gen.go"


_STRUCT_RE = re.compile(
    r"typedef\s+struct\s*\{(.*?)\}\s*_Py(\w+)Cache\s*;",
    re.DOTALL,
)
_FIELD_RE = re.compile(
    r"(_Py_BackoffCounter|uint16_t)\s+(\w+)(?:\[(\d+)\])?\s*;"
)
_UNION_RE = re.compile(r"union\s*\{(.*?)\}\s*;", re.DOTALL)


def _to_camel(name: str) -> str:
    parts = name.split("_")
    return "".join(p.capitalize() for p in parts if p)


def _struct_fields(body: str) -> list[tuple[str, int, bool]]:
    """Return [(GoName, codeunit_offset, in_union), ...].

    Union members all share the offset they enter on. Array fields
    expand to GoName0..GoNameN-1, one codeunit each.
    """
    out: list[tuple[str, int, bool]] = []
    offset = 0

    # Find any union, extract its inner block, then strip it from
    # the body so the outer field walk does not double-count.
    union_match = _UNION_RE.search(body)
    union_inner = ""
    if union_match:
        union_inner = union_match.group(1)
        body = body[: union_match.start()] + body[union_match.end():]

    for m in _FIELD_RE.finditer(body):
        ctype, name, arr = m.group(1), m.group(2), m.group(3)
        size = int(arr) if arr else 1
        if name == "counter":
            out.append(("Counter", offset, False))
            offset += 1
            continue
        if size == 1:
            out.append((_to_camel(name), offset, False))
        else:
            for i in range(size):
                out.append((_to_camel(name) + str(i), offset + i, False))
        offset += size

    if union_inner:
        # Each union arm starts at the same offset; the union itself
        # consumes max(arm sizes) codeunits.
        union_start = offset
        max_size = 0
        for m in _FIELD_RE.finditer(union_inner):
            ctype, name, arr = m.group(1), m.group(2), m.group(3)
            size = int(arr) if arr else 1
            if size == 1:
                out.append((_to_camel(name), union_start, True))
            else:
                for i in range(size):
                    out.append((_to_camel(name) + str(i), union_start + i, True))
            max_size = max(max_size, size)
        offset = union_start + max_size

    return out, offset


def generate_cache_layouts(input_path: str, outfile: TextIO) -> None:
    src = Path(input_path).read_text()
    structs: list[tuple[str, list[tuple[str, int, bool]], int]] = []
    for m in _STRUCT_RE.finditer(src):
        body, op = m.group(1), m.group(2)
        fields, size = _struct_fields(body)
        structs.append((op, fields, size))

    write_go_header(
        outfile,
        "cache_struct_parser.py",
        ["Include/internal/pycore_code.h"],
        "specialize",
        build_tag="ignore",
    )

    outfile.write("// Typed inline-cache accessors. Each struct wraps a slice of\n")
    outfile.write("// codeunits (uint16) and an instruction offset; field accessors\n")
    outfile.write("// read or write the codeunit at the struct-relative offset. The\n")
    outfile.write("// offsets are derived from CPython's _Py<Op>Cache struct member\n")
    outfile.write("// order in Include/internal/pycore_code.h.\n\n")

    for op, fields, size in structs:
        type_name = op + "Cache"
        ctor = type_name + "At"
        outfile.write(f"// {type_name} wraps _Py{op}Cache (size {size} codeunits).\n")
        outfile.write(f"type {type_name} struct {{ code []uint16; instr int }}\n\n")
        outfile.write(f"func {ctor}(code []uint16, instr int) {type_name} {{ return {type_name}{{code, instr}} }}\n\n")
        # Codeunit size, accessible without reflection.
        outfile.write(f"func ({type_name}) Codeunits() int {{ return {size} }}\n\n")
        for go_name, off, _in_union in fields:
            outfile.write(f"func (c {type_name}) {go_name}() uint16 {{ return c.code[c.instr+1+{off}] }}\n")
            outfile.write(f"func (c {type_name}) Set{go_name}(v uint16) {{ c.code[c.instr+1+{off}] = v }}\n")
        outfile.write("\n")

    # Emit a name->size map so tests can cross-check against
    # compile.opcodeCachesGen without reflection.
    outfile.write("// CacheCodeunits names each cache struct by its CPython type and\n")
    outfile.write("// returns its codeunit footprint. Useful for parity tests.\n")
    outfile.write("var CacheCodeunits = map[string]int{\n")
    for op, _fields, size in structs:
        outfile.write(f"\t\"_Py{op}Cache\": {size},\n")
    outfile.write("}\n")


arg_parser = argparse.ArgumentParser(
    description="Generate specialize/cache_layouts_gen.go from pycore_code.h.",
    formatter_class=argparse.ArgumentDefaultsHelpFormatter,
)
arg_parser.add_argument("-o", "--output", default=DEFAULT_OUTPUT)
arg_parser.add_argument("input", nargs="?", default=DEFAULT_INPUT)


if __name__ == "__main__":
    args = arg_parser.parse_args()
    with open(args.output, "w") as outfile:
        generate_cache_layouts(args.input, outfile)
