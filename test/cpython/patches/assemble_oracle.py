"""L4 oracle: dump code-object fields per unit.

The unpatched `_testinternalcapi.assemble_code_object` takes a
metadata dict (with co_names / co_varnames / co_cellvars /
co_freevars / co_fasthidden as dicts) that compiler_codegen does
not expose. Re-building that dict by hand defeats the point of a
"diff CPython against gopy" oracle: any reconstruction logic that
the oracle gets wrong would silently shift the comparison.

Instead, the L4 oracle uses the regular `compile()` builtin (which
runs the same `_PyAssemble_MakeCodeObject` internally) and dumps
each nested code object's fields one per unit. The fields are the
ones that 1713 byte-equality compares, broken apart so a `diff -u`
between this oracle's tree and gopy's emitter localizes to one
field instead of an opaque marshal mismatch.

Usage:

    PYTHONHASHSEED=0 /path/to/python.exe \\
        test/cpython/patches/assemble_oracle.py \\
            --src example.py --out /tmp/oracle-assemble
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from oracle_common import format_const  # noqa: E402


def walk_code_objects(co):
    """Yield (qualname, code_object) for co and every nested code."""
    yield co
    for c in co.co_consts:
        if type(c).__name__ == "code":
            yield from walk_code_objects(c)


def format_tuple(name: str, values) -> list[str]:
    if not values:
        return [f"{name}: ()"]
    lines = [f"{name} ({len(values)}):"]
    for i, v in enumerate(values):
        lines.append(f"  [{i}] {format_const(v)}")
    return lines


def format_bytes(name: str, data: bytes) -> str:
    return f"{name} ({len(data)} bytes): {data.hex()}"


def dump_code(co) -> str:
    lines: list[str] = []
    lines.append(f"co_name: {co.co_name!r}")
    lines.append(f"co_qualname: {co.co_qualname!r}")
    lines.append(f"co_argcount: {co.co_argcount}")
    lines.append(f"co_posonlyargcount: {co.co_posonlyargcount}")
    lines.append(f"co_kwonlyargcount: {co.co_kwonlyargcount}")
    lines.append(f"co_nlocals: {co.co_nlocals}")
    lines.append(f"co_stacksize: {co.co_stacksize}")
    lines.append(f"co_flags: 0x{co.co_flags:x}")
    lines.append(f"co_firstlineno: {co.co_firstlineno}")
    lines.extend(format_tuple("co_names", co.co_names))
    lines.extend(format_tuple("co_varnames", co.co_varnames))
    lines.extend(format_tuple("co_cellvars", co.co_cellvars))
    lines.extend(format_tuple("co_freevars", co.co_freevars))
    lines.extend(format_tuple("co_consts", co.co_consts))
    lines.append(format_bytes("co_code", co.co_code))
    lines.append(format_bytes("co_linetable", co.co_linetable))
    lines.append(format_bytes("co_exceptiontable", co.co_exceptiontable))
    return "\n".join(lines) + "\n"


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--src", required=True, help="Python source file")
    ap.add_argument("--out", required=True, help="Output directory")
    ap.add_argument("--filename", default=None,
                    help="filename passed to compile()")
    ns = ap.parse_args(argv)

    src_path = Path(ns.src)
    source = src_path.read_text(encoding="utf-8")
    filename = ns.filename or str(src_path)

    co = compile(source, filename, "exec")

    out_dir = Path(ns.out)
    out_dir.mkdir(parents=True, exist_ok=True)
    count = 0
    for idx, child in enumerate(walk_code_objects(co)):
        out_path = out_dir / f"unit{idx:03d}.l4.txt"
        out_path.write_text(dump_code(child), encoding="utf-8")
        count += 1

    print(f"wrote {count} l4 dumps", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
