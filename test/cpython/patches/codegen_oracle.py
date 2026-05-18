"""L1 oracle: dump the pre-optimize instruction sequence.

Drives `_testinternalcapi.compiler_codegen(ast, filename, optimize,
mode)` for a Python source file and writes the resulting
InstructionSequence as a stable text file. This is the L1 source
of truth for the gopy compare ladder.

Usage:

    PYTHONHASHSEED=0 /path/to/python.exe \\
        test/cpython/patches/codegen_oracle.py \\
            --src example.py --out /tmp/oracle-codegen

Writes `<out>/unit000.l1.txt` (top-level unit only; nested code
units are out of scope until the codegen entry exposes their
metadata).
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

import _testinternalcapi

sys.path.insert(0, str(Path(__file__).parent))
from oracle_common import dump_instruction_sequence, parse_source  # noqa: E402


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--src", required=True, help="Python source file")
    ap.add_argument("--out", required=True, help="Output directory")
    ap.add_argument("--optimize", type=int, default=0,
                    help="optimize level passed to compiler_codegen")
    ap.add_argument("--filename", default=None,
                    help="filename passed to compiler_codegen")
    ns = ap.parse_args(argv)

    src_path = Path(ns.src)
    source = src_path.read_text(encoding="utf-8")
    filename = ns.filename or str(src_path)

    tree = parse_source(source, filename)
    seq, metadata = _testinternalcapi.compiler_codegen(tree, filename, ns.optimize)

    out_dir = Path(ns.out)
    out_dir.mkdir(parents=True, exist_ok=True)
    dump = dump_instruction_sequence(seq, metadata)
    out_path = out_dir / "unit000.l1.txt"
    out_path.write_text(dump, encoding="utf-8")

    print(f"wrote {out_path}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
