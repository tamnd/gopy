"""importlib: gopy-side stub.

CPython ships a multi-module package whose top-level __init__.py
re-exports a handful of names from ._bootstrap and ._bootstrap_external.
gopy's import system is the Go side of the runtime so the bootstrap
plumbing has no analogue here. Until the full importlib port lands,
this module exists so `import importlib` and `import importlib.machinery`
resolve for downstream consumers (notably inspect.py, which only reads
the SUFFIXES constants and all_suffixes()).

CPython: Lib/importlib/__init__.py
"""

from . import machinery  # bind importlib.machinery attribute eagerly

__all__ = ['import_module', 'invalidate_caches', 'machinery', 'reload']


def import_module(name, package=None):
    """Import a module by name. Mirrors importlib.import_module."""
    if package is not None and name.startswith('.'):
        name = _resolve_name(name, package)
    __import__(name)
    import sys
    return sys.modules[name]


def _resolve_name(name, package):
    level = 0
    while level < len(name) and name[level] == '.':
        level += 1
    if level == 0:
        return name
    bits = package.rsplit('.', level - 1)
    if len(bits) < level:
        raise ImportError("attempted relative import beyond top-level package")
    base = bits[0]
    return '{}.{}'.format(base, name[level:]) if name[level:] else base


def invalidate_caches():
    """No-op: gopy's import system has no path-importer cache to flush."""
    return None


def reload(module):
    """Reload a module. The actual machinery is wired up Go-side."""
    import _imp
    return _imp.reload(module)
