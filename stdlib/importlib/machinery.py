"""importlib.machinery: gopy-side stub.

The CPython module re-exports loader / finder classes plus suffix
constants from ._bootstrap and ._bootstrap_external. gopy's import
system is implemented Go-side, so the loaders and finders aren't
needed at the Python boundary. Only the SUFFIXES constants and the
all_suffixes() helper are observable from stdlib consumers
(inspect.py being the main one for the v0.12.4 lexer/tokenizer gates).

When a future spec lands the full importlib bootstrap port, this file
becomes the byte-equal vendor of Lib/importlib/machinery.py.

CPython: Lib/importlib/machinery.py
"""

SOURCE_SUFFIXES = ['.py']
DEBUG_BYTECODE_SUFFIXES = ['.pyc']
OPTIMIZED_BYTECODE_SUFFIXES = ['.pyc']
BYTECODE_SUFFIXES = DEBUG_BYTECODE_SUFFIXES
EXTENSION_SUFFIXES = []


def all_suffixes():
    """Returns a list of all recognized module suffixes for this process."""
    return SOURCE_SUFFIXES + BYTECODE_SUFFIXES + EXTENSION_SUFFIXES


class ModuleSpec:
    """Minimal stand-in for importlib.machinery.ModuleSpec."""

    def __init__(self, name, loader, *, origin=None, loader_state=None,
                 is_package=None):
        self.name = name
        self.loader = loader
        self.origin = origin
        self.loader_state = loader_state
        self.submodule_search_locations = [] if is_package else None
        self.has_location = origin is not None
        self.cached = None


__all__ = [
    'BYTECODE_SUFFIXES',
    'DEBUG_BYTECODE_SUFFIXES',
    'EXTENSION_SUFFIXES',
    'ModuleSpec',
    'OPTIMIZED_BYTECODE_SUFFIXES',
    'SOURCE_SUFFIXES',
    'all_suffixes',
]
