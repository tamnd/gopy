"""py_compile: gopy stub.

The full CPython port writes .pyc cache files via
importlib._bootstrap_external. gopy doesn't have that subsystem ported
yet, but test.support.script_helper imports the module at top level so
the stub keeps that import alive. compile() / main() raise on first
use; PycInvalidationMode and PyCompileError stay reachable for callers
that only reference them at module scope.

CPython: Lib/py_compile.py
"""

import enum
import os
import os.path
import sys
import traceback


__all__ = ["compile", "main", "PycInvalidationMode", "PyCompileError"]


class PycInvalidationMode(enum.Enum):
    TIMESTAMP = 1
    CHECKED_HASH = 2
    UNCHECKED_HASH = 3


def _get_default_invalidation_mode():
    if os.environ.get("SOURCE_DATE_EPOCH"):
        return PycInvalidationMode.CHECKED_HASH
    return PycInvalidationMode.TIMESTAMP


class PyCompileError(Exception):
    def __init__(self, exc_type, exc_value, file, msg=""):
        exc_type_name = exc_type.__name__
        if exc_type is SyntaxError:
            tbtext = "".join(traceback.format_exception_only(exc_type, exc_value)).rstrip("\n")
            errmsg = msg or f"Sorry: {exc_type_name}: {tbtext}"
        else:
            errmsg = msg or f"Sorry: {exc_type_name}: {exc_value}"
        Exception.__init__(self, errmsg, exc_type_name, exc_value, file)
        self.exc_type_name = exc_type_name
        self.exc_value = exc_value
        self.file = file
        self.msg = errmsg

    def __str__(self):
        return self.msg


def compile(file, cfile=None, dfile=None, doraise=False, optimize=-1,
            invalidation_mode=None, quiet=0):
    raise NotImplementedError(
        "py_compile.compile is unavailable in gopy (importlib._bootstrap_external missing)"
    )


def main():
    raise NotImplementedError("py_compile.main is unavailable in gopy")
