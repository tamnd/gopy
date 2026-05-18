"""Shared helpers for the codegen / optimize / assemble oracles.

These oracles drive the unpatched _testinternalcapi entry points
(compiler_codegen, optimize_cfg, assemble_code_object) and write
stable text dumps that the gopy gate diffs against gopy's own
emitters. The format is deliberately verbose and ordered so a
`diff -u` between the two trees flags the first divergence.

Run with the patched (or unpatched) interpreter, but always with
PYTHONHASHSEED=0 so co_consts ordering is reproducible.
"""

from __future__ import annotations

import ast as _ast
import opcode as _opcode
from typing import Any

# CPython 3.14: opcode.opname is indexed by numeric opcode.
_OPNAME = _opcode.opname


def opname(op: int) -> str:
    """Symbolic name for a numeric opcode."""
    if 0 <= op < len(_OPNAME):
        name = _OPNAME[op]
        if name and not name.startswith("<"):
            return name
    return f"OP{op}"


def format_const(value: Any) -> str:
    """Stable repr for a co_consts entry.

    Uses repr() for primitives; for code objects we emit a marker
    so nested-unit ordering is comparable without dumping the
    nested object's full text on this line.
    """
    if type(value).__name__ == "code":
        return f"<code {value.co_qualname!r} firstlineno={value.co_firstlineno}>"
    return repr(value)


def format_consts(consts) -> list[str]:
    return [f"  [{i}] {format_const(c)}" for i, c in enumerate(consts)]


def format_instruction(idx: int, inst) -> str:
    """Render one InstructionSequence.get_instructions() tuple.

    Each tuple is (opcode, oparg_or_None, lineno, end_lineno,
    col_offset, end_col_offset). Labels are integers, not tuples.
    """
    if isinstance(inst, int):
        return f"  [{idx}] LABEL {inst}"
    op, arg, lineno, end_lineno, col, end_col = inst
    arg_str = "None" if arg is None else str(arg)
    return (
        f"  [{idx}] {opname(op)} ({op}) arg: {arg_str} "
        f"loc: {lineno}:{end_lineno}:{col}:{end_col}"
    )


def dump_instruction_sequence(seq, metadata: dict | None = None) -> str:
    """Pretty-print an InstructionSequence + optional metadata."""
    lines: list[str] = []
    if metadata is not None:
        lines.append(f"argcount: {metadata.get('argcount', 0)}")
        lines.append(f"posonlyargcount: {metadata.get('posonlyargcount', 0)}")
        lines.append(f"kwonlyargcount: {metadata.get('kwonlyargcount', 0)}")
        consts = metadata.get("consts", [])
        lines.append(f"consts ({len(consts)}):")
        lines.extend(format_consts(consts))
    instructions = seq.get_instructions()
    lines.append(f"instructions ({len(instructions)}):")
    for i, inst in enumerate(instructions):
        lines.append(format_instruction(i, inst))
    return "\n".join(lines) + "\n"


def parse_source(src: str, filename: str) -> _ast.AST:
    """Parse Python source into an AST module."""
    return _ast.parse(src, filename=filename, mode="exec")
