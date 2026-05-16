"""Generate the gopy opcode-ID table.

This is the Go-emitting sibling of upstream
opcode_id_generator.py. Where that script emits
Include/opcode_ids.h with `#define <NAME> <N>` rows, this one
emits compile/opcode_ids_gen.go with `<NAME> Opcode = <N>` rows
plus the HAVE_ARGUMENT / MIN_SPECIALIZED_OPCODE /
MIN_INSTRUMENTED_OPCODE constants.

Spec 1714 phase 2 makes this the single source of truth for
opcode IDs in gopy; compile/opcodes_gen.go (hand-rolled) will be
deleted once the parity test goes green.

Spec: website/docs/specs/1700/1714_bytecodes_dsl_codegen.md
"""

from __future__ import annotations

import argparse
from typing import TextIO

from analyzer import Analysis, analyze_files
from generators_common import DEFAULT_INPUT, ROOT
from go_generators_common import write_go_header


DEFAULT_OUTPUT = "compile/opcode_ids_gen.go"


def _short_source(path: str) -> str:
    """Reduce an inputs/-relative absolute path to a CPython tree
    reference so the embedded source line stays reproducible
    regardless of the checkout location."""
    marker = "Tools/cases_generator/inputs/"
    if marker in path:
        return path.split(marker, 1)[1]
    return path


def generate_opcode_ids(
    filenames: list[str], analysis: Analysis, outfile: TextIO
) -> None:
    # Build-tag the output until Phase 2.4 deletes the hand-rolled
    # compile/opcodes_gen.go. Without the tag, every opcode here
    # would redeclare the constant in opcodes_gen.go.
    # Build-tag the output until Phase 2.4 deletes the hand-rolled
    # compile/opcodes_gen.go. Without the tag, every opcode here
    # would redeclare the constant in opcodes_gen.go.
    write_go_header(
        outfile,
        "gopy_opcode_id_generator.py",
        [_short_source(f) for f in filenames],
        "compile",
        build_tag="ignore",
    )
    outfile.write("// Opcode constants. Numeric values match CPython 3.14 opmap.\n")
    outfile.write("//\n")
    outfile.write("// CPython: Include/opcode_ids.h (regenerate via Tools/cases_generator).\n")
    outfile.write("const (\n")

    rows = sorted(analysis.opmap.items(), key=lambda kv: (kv[1], kv[0]))
    name_width = max(len(name) for name, _ in rows)
    for name, op in rows:
        outfile.write(f"\t{name:<{name_width}} Opcode = {op}\n")
    outfile.write(")\n\n")

    outfile.write("// Boundaries published by CPython for the dispatch loop.\n")
    outfile.write("const (\n")
    outfile.write(f"\tHAVE_ARGUMENT          Opcode = {analysis.have_arg}\n")
    outfile.write(f"\tMIN_SPECIALIZED_OPCODE Opcode = {analysis.opmap['RESUME'] + 1}\n")
    outfile.write(f"\tMIN_INSTRUMENTED_OPCODE Opcode = {analysis.min_instrumented}\n")
    outfile.write(")\n")


arg_parser = argparse.ArgumentParser(
    description="Generate compile/opcode_ids_gen.go from CPython DSL inputs.",
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
        generate_opcode_ids(args.input, data, outfile)
