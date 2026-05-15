"""importlib.util: gopy stub for the parts pkgutil/unittest.mock need.

CPython's Lib/importlib/util.py re-exports symbols from the import
machinery's _bootstrap and _bootstrap_external modules, which gopy
doesn't ship. The pkgutil/unittest.mock load path only references
MAGIC_NUMBER at module load (inside a function body) plus find_spec
later; resolve_name doesn't touch util at all. Until spec 1711 Phase
9 wires the full importlib port this stub keeps the import chain
green.

CPython: Lib/importlib/util.py
"""

import sys
import types

# 16-bit magic + 0x0a0d, identical to CPython 3.14's pyc magic. Used
# only by pkgutil.read_code in code paths gopy doesn't currently hit.
#
# CPython: Lib/importlib/_bootstrap_external.py MAGIC_NUMBER
MAGIC_NUMBER = b'\x74\x0e\r\n'


def find_spec(name, package=None):
    """Stub: gopy's PathFinder doesn't expose ModuleSpec yet."""
    raise NotImplementedError("importlib.util.find_spec is unavailable in gopy")


def module_from_spec(spec):
    raise NotImplementedError("importlib.util.module_from_spec is unavailable in gopy")


def spec_from_loader(name, loader, *, origin=None, is_package=None):
    raise NotImplementedError("importlib.util.spec_from_loader is unavailable in gopy")


def spec_from_file_location(name, location=None, *, loader=None,
                            submodule_search_locations=None):
    raise NotImplementedError("importlib.util.spec_from_file_location is unavailable in gopy")


def source_hash(source_bytes):
    raise NotImplementedError("importlib.util.source_hash is unavailable in gopy")


def resolve_name(name, package):
    """Resolve a relative module name to an absolute one."""
    if not name.startswith('.'):
        return name
    if not package:
        raise ImportError(f'no package specified for {name!r} '
                          '(required for relative module names)')
    level = 0
    for character in name:
        if character != '.':
            break
        level += 1
    return _resolve_name(name[level:], package, level)


def _resolve_name(name, package, level):
    bits = package.rsplit('.', level - 1)
    if len(bits) < level:
        raise ImportError('attempted relative import beyond top-level package')
    base = bits[0]
    return f'{base}.{name}' if name else base


class LazyLoader:
    """Stub: not used by the unittest.mock import chain."""

    @classmethod
    def factory(cls, loader):
        raise NotImplementedError("importlib.util.LazyLoader is unavailable in gopy")
