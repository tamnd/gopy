"""Drive the patched CPython interpreter and capture per-phase CFG dumps.

Reads a Python source file or stdin, compiles it under a phase hook
installed via ``_testinternalcapi.set_cfg_phase_hook``, and writes one
``<unit>.<phase>.cfg`` file per (code unit, phase) pair into the output
directory.

Run with the patched interpreter:

    PYTHONHASHSEED=0 /path/to/patched/python.exe \\
        test/cpython/patches/dump_phases.py \\
            --src example.py --out /tmp/cfg-cpython

Each unit is identified by its qualified name from the surrounding code
object. Top-level module is written as ``<module>``. The output is the
exact format ``compile.DumpCfg`` produces in gopy, so a `diff -u` between
the two trees flags the first divergence.
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

import _testinternalcapi


def install_hook(out_dir: Path):
    out_dir.mkdir(parents=True, exist_ok=True)
    state = {"unit_idx": -1, "files": 0}

    def hook(phase_name: str, dump_text: str) -> None:
        # Each unit fires every phase exactly once, in the same order, with
        # "entry" first. Bump the unit counter on each "entry" boundary so the
        # filenames are unit-major and stable across runs.
        if phase_name == "entry":
            state["unit_idx"] += 1
        idx = state["unit_idx"]
        path = out_dir / f"unit{idx:03d}.{phase_name}.cfg"
        path.write_text(dump_text, encoding="utf-8")
        state["files"] += 1

    _testinternalcapi.set_cfg_phase_hook(hook)
    return state


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--src", required=True, help="Python source file")
    ap.add_argument("--out", required=True, help="Output directory for CFG dumps")
    ap.add_argument("--filename", default=None, help="Filename to compile() with")
    ns = ap.parse_args(argv)

    src_path = Path(ns.src)
    source = src_path.read_text(encoding="utf-8")
    filename = ns.filename or str(src_path)

    counters = install_hook(Path(ns.out))
    try:
        compile(source, filename, "exec")
    finally:
        _testinternalcapi.set_cfg_phase_hook(None)

    total = sum(counters.values())
    print(f"wrote {total} dump files for {len(counters)} phases", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
