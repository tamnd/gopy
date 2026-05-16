"""GoWriter — Go-emitting sibling of cwriter.CWriter.

The cases_generator pipeline uses cwriter.CWriter to emit C with
brace-driven indent and token-position tracking. For spec 1714 we
need a Go-emitting sibling that the gopy-targeted generators
(Phase 2+) can plug into without reinventing the indent/scope
machinery.

This module is the Go-emission half of Phase 1. The matching
constant-macro binding table lives in go_generators_common.py.

Differences vs cwriter.CWriter:
  * No C preprocessor #line directives. Go has //line comments
    but we do not emit them: every generator output runs through
    gofmt anyway, which destroys precise column alignment.
  * No stack-pointer spill/reload helpers. CPython's bytecodes.c
    spills `stack_pointer` across host calls; gopy keeps that
    bookkeeping inside evalState methods so the macro layer does
    not need it.
  * No header_guard. Go has packages, not include guards. We
    expose write_go_header(out, generator, sources) in
    go_generators_common.py instead.
  * brace counting still drives indent (Go uses `{` `}` for
    blocks). Parenthesis-driven hanging indent is dropped: gofmt
    handles function-argument indent itself.

Identifier-stable point: GoWriter mirrors CWriter's public method
names (emit, emit_str, emit_token, emit_at, start_line) so a
generator written against either writer can swap by parameter.

Spec: website/docs/specs/1700/1714_bytecodes_dsl_codegen.md
"""

from io import StringIO
from typing import TextIO

from lexer import Token


class GoWriter:
    """A writer that understands tokens and emits Go source.

    The indent unit is the Go convention: one tab per scope. Output
    is meant to flow through gofmt before being checked in, so we
    only need indentation that is structurally correct, not visually
    perfect.
    """

    last_token: "Token | None"

    def __init__(self, out: TextIO, indent: int = 0):
        self.out = out
        # Indent stack in number of tabs. Each `{` pushes one tab;
        # each `}` pops one.
        self.indents = [i for i in range(indent + 1)]
        self.last_token = None
        self.newline = True

    @staticmethod
    def null() -> "GoWriter":
        return GoWriter(StringIO())

    # ------------------------------------------------------------------
    # Position + indent bookkeeping
    # ------------------------------------------------------------------

    def _indent_str(self) -> str:
        return "\t" * self.indents[-1]

    def maybe_dedent(self, txt: str) -> None:
        # Lines like "}" or "})" pop one scope per close brace.
        closes = txt.count("}") - txt.count("{")
        for _ in range(closes):
            if len(self.indents) > 1:
                self.indents.pop()
        # Go labels look the same as C labels (name:). Dedenting a
        # label one level matches gofmt's convention.
        if _is_label(txt) and len(self.indents) > 1:
            self.indents.pop()

    def maybe_indent(self, txt: str) -> None:
        opens = txt.count("{") - txt.count("}")
        for _ in range(opens):
            self.indents.append(self.indents[-1] + 1)
        if _is_label(txt):
            self.indents.append(self.indents[-1] + 1)

    def set_position(self, tkn: Token) -> None:
        if self.last_token is not None:
            if self.last_token.end_line < tkn.line:
                self.out.write("\n")
            if self.last_token.line < tkn.line:
                self.out.write(self._indent_str())
            else:
                gap = tkn.column - self.last_token.end_column
                if gap > 0:
                    self.out.write(" " * gap)
        elif self.newline:
            self.out.write(self._indent_str())
        self.last_token = tkn
        self.newline = False

    # ------------------------------------------------------------------
    # Emission
    # ------------------------------------------------------------------

    def emit_text(self, txt: str) -> None:
        self.out.write(txt)

    def emit_at(self, txt: str, where: Token) -> None:
        self.set_position(where)
        self.out.write(txt)

    def emit_token(self, tkn: Token) -> None:
        if tkn.kind == "COMMENT" and "\n" in tkn.text:
            return self._emit_multiline_comment(tkn)
        self.maybe_dedent(tkn.text)
        self.set_position(tkn)
        self.emit_text(tkn.text)
        self.maybe_indent(tkn.text)

    def _emit_multiline_comment(self, tkn: Token) -> None:
        # Go has /* */ block comments; reuse CPython's body but
        # never align with a leading '*' (Go convention does not).
        self.set_position(tkn)
        lines = tkn.text.splitlines(True)
        first = True
        for line in lines:
            text = line.lstrip()
            if first:
                spaces = 0
            else:
                spaces = self.indents[-1]
            first = False
            self.out.write("\t" * spaces)
            self.out.write(text)

    def emit_str(self, txt: str) -> None:
        self.maybe_dedent(txt)
        if self.newline and txt:
            if txt[0] != "\n":
                self.out.write(self._indent_str())
            self.newline = False
        self.emit_text(txt)
        if txt.endswith("\n"):
            self.newline = True
        self.maybe_indent(txt)
        self.last_token = None

    def emit(self, txt) -> None:
        if isinstance(txt, Token):
            self.emit_token(txt)
        elif isinstance(txt, str):
            self.emit_str(txt)
        else:
            raise TypeError(f"GoWriter.emit: unsupported type {type(txt).__name__}")

    def start_line(self) -> None:
        if not self.newline:
            self.out.write("\n")
        self.newline = True
        self.last_token = None


def _is_label(txt: str) -> bool:
    s = txt.strip()
    if not s or s.startswith("//") or s.startswith("/*"):
        return False
    # Distinguish `Name:` (label) from `case X:` / `default:` (switch)
    # and `key: value` map/struct literals. The cases_generator only
    # emits labels as standalone lines, so accept only short bare names.
    if not s.endswith(":"):
        return False
    head = s[:-1]
    if not head or " " in head:
        return False
    return head.replace("_", "").isalnum()
