"""Generate the gopy Tier-2 dispatch + stubs files.

This is the Go-emitting sibling of upstream tier2_generator.py.
Where that script emits Python/executor_cases.c.h (one big switch
of Tier-2-rewritten C bodies), this one emits two Go files into
the optimizer/ package:

  * optimizer/uops_dispatch_gen.go - a switch on inst.Opcode() that
    delegates to the per-uop method on *Tier2State.
  * optimizer/uops_stubs_gen.go - one StatusDeopt stub per
    Tier-2-viable uop the optimizer/ tree has not yet hand-ported,
    each carrying the Tier-2-rewritten C body as a Go // comment
    block so a porter has the spec inline.

The classification logic (which uops are Tier-2-viable, how to
substitute oparg in the _0 / _1 variants, how to lower DEOPT_IF /
EXIT_IF) is reused verbatim by re-importing the upstream
Tier2Emitter and write_uop. The only Go-specific work is:

  1. Detecting which uop methods are already hand-ported in
     optimizer/uops_impl.go so we do not emit duplicate stubs.
     The Go tool used go/parser; from Python we do a regex sweep
     over each non-_gen.go file.
  2. Re-writing the per-uop C body capture into a Go // comment
     block, line by line, so the file remains valid Go.
  3. Emitting Go method headers + bodies (return s.unimplementedUop
     for stubs, switch fan-out for the dispatcher).

Phase 8 (body translation via MACRO_BINDINGS) will eventually
produce Go bodies directly; until that lands, the inline C body
serves as the porting spec.

Spec: website/docs/specs/1700/1714_bytecodes_dsl_codegen.md
"""

from __future__ import annotations

import argparse
import os
import re
from io import StringIO
from typing import TextIO

from analyzer import Analysis, Uop, analyze_files
from cwriter import CWriter
from generators_common import DEFAULT_INPUT, ROOT
from stack import Stack
from tier2_generator import (
    SKIPS,
    Tier2Emitter,
    declare_variables,
    write_uop,
)


DEFAULT_DISPATCH_OUTPUT = "optimizer/uops_dispatch_gen.go"
DEFAULT_STUBS_OUTPUT = "optimizer/uops_stubs_gen.go"
DEFAULT_OPTIMIZER_DIR = "optimizer"


def _short_source(path: str) -> str:
    """Reduce a generator input path to a CPython tree reference so the
    embedded source line stays reproducible regardless of checkout
    location. Strips the leading inputs/ shim if present so the
    embedded path reads as `Python/bytecodes.c` rather than
    `inputs/Python/bytecodes.c`."""
    marker = "Tools/cases_generator/inputs/"
    if marker in path:
        return path.split(marker, 1)[1]
    if path.startswith("inputs/"):
        return path[len("inputs/"):]
    return path


def _pascal(name: str) -> str:
    """Convert UPPER_SNAKE to UpperCamelCase, matching the Go tool's
    pascal() so generated method and ID names stay byte-compatible.
    """
    out = []
    for part in name.split("_"):
        if not part:
            continue
        out.append(part[0].upper() + part[1:].lower())
    return "".join(out)


def uop_method_name(name: str) -> str:
    """Return the *Tier2State method name a uop binds to.

    Examples: _LOAD_FAST -> uopLoadFast, BUILD_LIST -> uopBuildList.
    Leading underscores are stripped first so a uop ID and its
    Tier-1 alias map to the same method.
    """
    return "uop" + _pascal(name.lstrip("_"))


def uop_id_const(name: str) -> str:
    """Return the UopXxx constant name a uop emits to.

    Mirrors uop_method_name but capitalizes the first letter so the
    constant matches optimizer/uop_ids_gen.go entries.
    """
    return "Uop" + _pascal(name.lstrip("_"))


def is_viable_tier2_uop(uop: Uop) -> bool:
    """Mirror tier2_generator.generate_tier2's filter.

    A uop is Tier-2-viable iff it is not in the SKIPS list, is not
    a Tier-1-only inst, is not a super-instruction, and has no
    why_not_viable diagnostic.
    """
    if uop.name in SKIPS:
        return False
    if uop.properties.tier == 1:
        return False
    if uop.is_super():
        return False
    if uop.why_not_viable() is not None:
        return False
    return True


# Match `func (s *Tier2State) uopXxx(` so the stubs writer can omit
# any uop whose method is already hand-ported. The Go tool walked
# the AST; the regex form is good enough because the method shape is
# fixed by convention and never appears inside a string literal.
_METHOD_RE = re.compile(
    r"^func\s+\(\s*[A-Za-z_]\w*\s*\*\s*Tier2State\s*\)\s+(uop\w+)\s*\(",
    re.MULTILINE,
)


def detect_ported_uops(go_dir: str) -> set[str]:
    """Scan non-_gen.go Go files in go_dir for `func (s *Tier2State)
    uopXxx(` and return the set of method names already defined."""
    ported: set[str] = set()
    if not os.path.isdir(go_dir):
        return ported
    for entry in os.listdir(go_dir):
        if not entry.endswith(".go"):
            continue
        if entry.endswith("_gen.go") or entry.endswith("_test.go"):
            continue
        path = os.path.join(go_dir, entry)
        try:
            with open(path, "r", encoding="utf-8") as fh:
                source = fh.read()
        except OSError:
            continue
        for m in _METHOD_RE.finditer(source):
            ported.add(m.group(1))
    return ported


def render_uop_body(analysis: Analysis, uop: Uop) -> str:
    """Capture a single uop's Tier-2-rewritten C body as text.

    Runs the upstream Tier2Emitter into a StringIO so we can comment-
    prefix every line for the Go output. The output matches what
    upstream tier2_generator.generate_tier2 would write for this uop
    (minus the surrounding `case .. break;` framing rules), which is
    intentional: the porter who lowers this body to Go must see the
    same spec the C target sees.
    """
    buf = StringIO()
    out = CWriter(buf, 2, False)
    emitter = Tier2Emitter(out, analysis.labels)
    out.emit(f"case {uop.name}: {{\n")
    declare_variables(uop, out)
    stack = Stack()
    write_uop(uop, emitter, stack)
    out.start_line()
    if not uop.properties.always_exits:
        out.emit("break;\n")
    out.start_line()
    out.emit("}\n")
    return buf.getvalue()


def comment_prefix(body: str) -> str:
    """Turn a captured C-flavored body into a Go // comment block.

    Empty lines become bare `//`, content lines become `// <text>`,
    and trailing empty comments are trimmed so the block ends on a
    content line (followed by a newline). Matches the Go tool's
    commentPrefix shape so diffs against the previous output stay
    minimal.
    """
    if not body:
        return ""
    lines = body.split("\n")
    out: list[str] = []
    for line in lines:
        stripped = line.rstrip("\r ")
        if stripped == "":
            out.append("//")
        else:
            out.append("// " + stripped)
    while out and out[-1] == "//":
        out.pop()
    return "\n".join(out) + "\n"


def _write_header(out: TextIO, filenames: list[str]) -> None:
    """Stamp the generator banner above each emitted Go file.

    Matches the Go tool's banner shape (`// This file is generated by
    ...`) so the gopy lint / drift checkers do not need a special-
    case for the new generator.
    """
    out.write("// This file is generated by Tools/cases_generator/gopy_tier2_generator.py\n")
    out.write("// from:\n")
    out.write(f"//   {', '.join(_short_source(f) for f in filenames)}\n")
    out.write("// Do not edit!\n")
    out.write("// Code generated by Tools/cases_generator; DO NOT EDIT.\n\n")
    out.write("package optimizer\n\n")


_DISPATCH_DOC = """// executeUop is the dispatch fan-out. Generated from the analyzer
// AST so adding a uop upstream auto-extends the switch on regen;
// hand-ported bodies live in uops_impl.go and the un-ported tail
// lives as StatusDeopt stubs in uops_stubs_gen.go.
//
// CPython: Python/ceval.c:1291-1356 tier2_dispatch switch
"""


_STUBS_DOC = """// One StatusDeopt method per Tier-2-viable uop the optimizer/ tree
// has not yet hand-ported. Each method's // comment block carries
// the Tier-2-rewritten C body so a porter has the spec inline.
//
// To port: copy the function over into uops_impl.go, translate the
// commented C body into Go, and regenerate. The generator detects
// the new method on *Tier2State and stops emitting a stub for it.
//
// CPython: Python/executor_cases.c.h (per-uop case bodies)
"""


def generate_dispatch(
    filenames: list[str], analysis: Analysis, outfile: TextIO
) -> None:
    """Write the executeUop switch into outfile."""
    _write_header(outfile, filenames)
    outfile.write(_DISPATCH_DOC)
    outfile.write(
        "func (s *Tier2State) executeUop(inst *UOPInstruction) Tier2Status {\n"
    )
    outfile.write("\tswitch inst.Opcode() {\n")
    for name in sorted(analysis.uops):
        uop = analysis.uops[name]
        if not is_viable_tier2_uop(uop):
            continue
        outfile.write(f"\tcase {uop_id_const(name)}:\n")
        outfile.write(f"\t\treturn s.{uop_method_name(name)}(inst)\n")
    outfile.write("\t}\n")
    outfile.write("\treturn StatusDeopt\n}\n")


def generate_stubs(
    filenames: list[str],
    analysis: Analysis,
    ported: set[str],
    outfile: TextIO,
) -> None:
    """Write StatusDeopt stubs for every Tier-2-viable uop missing
    from ported."""
    _write_header(outfile, filenames)
    outfile.write(_STUBS_DOC)
    for name in sorted(analysis.uops):
        uop = analysis.uops[name]
        if not is_viable_tier2_uop(uop):
            continue
        method = uop_method_name(name)
        if method in ported:
            continue
        body = render_uop_body(analysis, uop)
        outfile.write(f"// {uop.name} body (Tier-2 rewrite, awaiting hand-port):\n")
        outfile.write(comment_prefix(body))
        outfile.write(f"func (s *Tier2State) {method}(_ *UOPInstruction) Tier2Status {{\n")
        outfile.write(f"\treturn s.unimplementedUop(\"{name}\")\n")
        outfile.write("}\n\n")


def generate_tier2(
    filenames: list[str],
    analysis: Analysis,
    dispatch_out: TextIO,
    stubs_out: TextIO,
    optimizer_dir: str,
) -> None:
    """End-to-end entrypoint matching upstream's generate_tier2 shape.

    Detects already-ported methods from optimizer_dir, then writes
    dispatch + stubs files in that order. Caller owns the file
    handles so test code can stream into StringIO.
    """
    ported = detect_ported_uops(optimizer_dir)
    generate_dispatch(filenames, analysis, dispatch_out)
    generate_stubs(filenames, analysis, ported, stubs_out)


arg_parser = argparse.ArgumentParser(
    description="Generate optimizer/uops_dispatch_gen.go + uops_stubs_gen.go "
    "from CPython DSL inputs.",
    formatter_class=argparse.ArgumentDefaultsHelpFormatter,
)

arg_parser.add_argument(
    "--dispatch-output",
    type=str,
    help="Generated dispatch file",
    default=DEFAULT_DISPATCH_OUTPUT,
)

arg_parser.add_argument(
    "--stubs-output",
    type=str,
    help="Generated stubs file",
    default=DEFAULT_STUBS_OUTPUT,
)

arg_parser.add_argument(
    "--optimizer-dir",
    type=str,
    help="Directory scanned for already-ported Tier2State methods",
    default=DEFAULT_OPTIMIZER_DIR,
)

arg_parser.add_argument(
    "input", nargs=argparse.REMAINDER, help="Instruction definition file(s)"
)


if __name__ == "__main__":
    args = arg_parser.parse_args()
    if len(args.input) == 0:
        args.input.append(str(DEFAULT_INPUT))
    data = analyze_files(args.input)
    with open(args.dispatch_output, "w") as dispatch_fh, open(
        args.stubs_output, "w"
    ) as stubs_fh:
        generate_tier2(
            args.input, data, dispatch_fh, stubs_fh, args.optimizer_dir
        )
