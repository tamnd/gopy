"""Generate the gopy opcode metadata tables.

Go-emitting sibling of upstream opcode_metadata_generator.py.
Phase 2.2 scope is intentionally narrow: cache widths and the
16-bit flag word per opcode. Stack effects, expansion tables,
deopt/extra-cases tables, and pseudo targets stay out until later
phases (tier1/tier2 work pulls those in).

Flag bit assignment mirrors the order of FLAGS in upstream
opcode_metadata_generator.py and matches the bit positions in
compile/opcodes_gen.go (flagArg ... flagNoSaveIp).

Spec: website/docs/specs/1700/1714_bytecodes_dsl_codegen.md
"""

from __future__ import annotations

import argparse
from typing import TextIO

from analyzer import Analysis, Instruction, analyze_files
from generators_common import DEFAULT_INPUT
from go_generators_common import write_go_header


DEFAULT_OUTPUT = "compile/opcode_metadata_gen.go"

# Mirrors FLAGS in opcode_metadata_generator.py. Order is load-bearing:
# bit position N here must match (1 << N) in compile/opcodes_gen.go.
_FLAG_ORDER = [
    "ARG",
    "CONST",
    "NAME",
    "JUMP",
    "FREE",
    "LOCAL",
    "EVAL_BREAK",
    "DEOPT",
    "ERROR",
    "ESCAPES",
    "EXIT",
    "PURE",
    "PASSTHROUGH",
    "OPARG_AND_1",
    "ERROR_NO_POP",
    "NO_SAVE_IP",
]
_BIT = {name: 1 << i for i, name in enumerate(_FLAG_ORDER)}


def _flag_word(inst: Instruction) -> int:
    p = inst.properties
    v = 0
    if p.oparg:           v |= _BIT["ARG"]
    if p.uses_co_consts:  v |= _BIT["CONST"]
    if p.uses_co_names:   v |= _BIT["NAME"]
    if p.jumps:           v |= _BIT["JUMP"]
    if p.has_free:        v |= _BIT["FREE"]
    if p.uses_locals:     v |= _BIT["LOCAL"]
    if p.eval_breaker:    v |= _BIT["EVAL_BREAK"]
    if p.deopts:          v |= _BIT["DEOPT"]
    if not p.infallible:  v |= _BIT["ERROR"]
    if p.escapes:         v |= _BIT["ESCAPES"]
    if p.side_exit:       v |= _BIT["EXIT"]
    if p.pure:            v |= _BIT["PURE"]
    if p.error_without_pop: v |= _BIT["ERROR_NO_POP"]
    if p.no_save_ip:      v |= _BIT["NO_SAVE_IP"]
    return v


def _short_source(path: str) -> str:
    marker = "Tools/cases_generator/inputs/"
    if marker in path:
        return path.split(marker, 1)[1]
    return path


def generate_opcode_metadata(
    filenames: list[str], analysis: Analysis, outfile: TextIO
) -> None:
    write_go_header(
        outfile,
        "gopy_opcode_metadata_generator.py",
        [_short_source(f) for f in filenames],
        "compile",
        build_tag="ignore",
    )

    # Cache widths: same shape as _PyOpcode_Caches[256] but keyed
    # by Opcode constants so a future ergonomic switch to map[Opcode]uint8
    # is mechanical. Values are codeunits (1 cache = 1 _Py_CODEUNIT).
    outfile.write("// Cache widths per opcode (codeunits).\n")
    outfile.write("//\n")
    outfile.write("// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_Caches.\n")
    outfile.write("var opcodeCachesGen = [256]uint8{\n")
    cache_rows: list[tuple[str, int]] = []
    for inst in analysis.instructions.values():
        if inst.family and inst.family.name != inst.name:
            continue
        if inst.name.startswith("INSTRUMENTED"):
            continue
        if inst.size > 1:
            cache_rows.append((inst.name, inst.size - 1))
    name_width = max((len(n) for n, _ in cache_rows), default=1)
    for name, width in sorted(cache_rows):
        outfile.write(f"\t{name+':':<{name_width+1}} {width},\n")
    outfile.write("}\n\n")

    # 16-bit flag word per opcode. Bit positions match
    # compile/opcodes_gen.go flagArg ... flagNoSaveIp.
    outfile.write("// Per-opcode 16-bit flag word.\n")
    outfile.write("//\n")
    outfile.write("// Bit positions match flagArg ... flagNoSaveIp in compile/opcodes_gen.go,\n")
    outfile.write("// which in turn matches FLAGS order in CPython's opcode_metadata_generator.py.\n")
    outfile.write("//\n")
    outfile.write("// CPython: Include/internal/pycore_opcode_metadata.h _PyOpcode_opcode_metadata.flags.\n")
    outfile.write("var opcodeFlagsGen = [256]uint16{\n")
    flag_rows = [(inst.name, _flag_word(inst))
                 for inst in analysis.instructions.values()
                 if _flag_word(inst) != 0]
    name_width = max((len(n) for n, _ in flag_rows), default=1)
    for name, v in sorted(flag_rows):
        outfile.write(f"\t{name+':':<{name_width+1}} 0x{v:04x},\n")
    outfile.write("}\n")


arg_parser = argparse.ArgumentParser(
    description="Generate compile/opcode_metadata_gen.go from CPython DSL inputs.",
    formatter_class=argparse.ArgumentDefaultsHelpFormatter,
)

arg_parser.add_argument(
    "-o", "--output", type=str, help="Generated Go file", default=DEFAULT_OUTPUT
)

arg_parser.add_argument(
    "input", nargs=argparse.REMAINDER, help="Instruction definition file(s)"
)


if __name__ == "__main__":
    args = arg_parser.parse_args()
    if len(args.input) == 0:
        args.input.append(str(DEFAULT_INPUT))
    data = analyze_files(args.input)
    with open(args.output, "w") as outfile:
        generate_opcode_metadata(args.input, data, outfile)
