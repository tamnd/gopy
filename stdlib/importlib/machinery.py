"""importlib.machinery: gopy-side stub.

The CPython module re-exports loader / finder classes plus suffix
constants from ._bootstrap and ._bootstrap_external. gopy's import
system is implemented Go-side, so most loaders and finders aren't
needed at the Python boundary; the SourceFileLoader re-export is
necessary because py_compile.compile() drives it directly.

When a future spec lands the full importlib bootstrap port, this file
becomes the byte-equal vendor of Lib/importlib/machinery.py.

CPython: Lib/importlib/machinery.py
"""

from importlib._bootstrap_external import (
    FileLoader,
    SourceFileLoader,
)

SOURCE_SUFFIXES = ['.py']
DEBUG_BYTECODE_SUFFIXES = ['.pyc']
OPTIMIZED_BYTECODE_SUFFIXES = ['.pyc']
BYTECODE_SUFFIXES = DEBUG_BYTECODE_SUFFIXES
EXTENSION_SUFFIXES = []


def all_suffixes():
    """Returns a list of all recognized module suffixes for this process."""
    return SOURCE_SUFFIXES + BYTECODE_SUFFIXES + EXTENSION_SUFFIXES


class FileFinder:
    """Stub: gopy's import system is Go-side; pkgutil registers an
    iterator against FileFinder but it's only consulted when the
    user walks a package, which the spec 1711 test path doesn't.
    """

    def __init__(self, path, *loader_details):
        self.path = path
        self._loaders = loader_details


class ModuleSpec:
    """The specification for a module, used for loading.

    A faithful port of importlib._bootstrap.ModuleSpec. CPython defines
    the class in _bootstrap and re-exports it through machinery; gopy's
    bootstrap is Go-side, so the class lives here and importlib.util
    imports it from machinery.

    CPython: Lib/importlib/_bootstrap.py:565 ModuleSpec
    """

    def __init__(self, name, loader, *, origin=None, loader_state=None,
                 is_package=None):
        self.name = name
        self.loader = loader
        self.origin = origin
        self.loader_state = loader_state
        self.submodule_search_locations = [] if is_package else None
        self._uninitialized_submodules = []

        # file-location attributes
        self._set_fileattr = False
        self._cached = None

    def __repr__(self):
        args = [f'name={self.name!r}', f'loader={self.loader!r}']
        if self.origin is not None:
            args.append(f'origin={self.origin!r}')
        if self.submodule_search_locations is not None:
            args.append(f'submodule_search_locations={self.submodule_search_locations}')
        return f'{self.__class__.__name__}({", ".join(args)})'

    def __eq__(self, other):
        smsl = self.submodule_search_locations
        try:
            return (self.name == other.name and
                    self.loader == other.loader and
                    self.origin == other.origin and
                    smsl == other.submodule_search_locations and
                    self.cached == other.cached and
                    self.has_location == other.has_location)
        except AttributeError:
            return NotImplemented

    @property
    def cached(self):
        if self._cached is None:
            if self.origin is not None and self._set_fileattr:
                from importlib import _bootstrap_external
                self._cached = _bootstrap_external._get_cached(self.origin)
        return self._cached

    @cached.setter
    def cached(self, cached):
        self._cached = cached

    @property
    def parent(self):
        """The name of the module's parent."""
        if self.submodule_search_locations is None:
            return self.name.rpartition('.')[0]
        else:
            return self.name

    @property
    def has_location(self):
        return self._set_fileattr

    @has_location.setter
    def has_location(self, value):
        self._set_fileattr = bool(value)


__all__ = [
    'BYTECODE_SUFFIXES',
    'DEBUG_BYTECODE_SUFFIXES',
    'EXTENSION_SUFFIXES',
    'FileLoader',
    'ModuleSpec',
    'OPTIMIZED_BYTECODE_SUFFIXES',
    'SOURCE_SUFFIXES',
    'SourceFileLoader',
    'all_suffixes',
]
