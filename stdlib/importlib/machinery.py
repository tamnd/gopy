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
    NamespaceLoader,
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
    """File-based finder for a single directory.

    gopy's import statement is resolved Go-side, so FileFinder is not on
    the meta-path. It still has to exist as a sys.path_hooks entry:
    pkgutil.get_importer / runpy.run_path call the hook on a path item
    and treat a non-None result as "this is an importable directory".
    The find_spec scan mirrors CPython closely enough for pkgutil's
    iter_modules / walk_packages to enumerate a directory's contents.

    CPython: Lib/importlib/_bootstrap_external.py:1322 FileFinder
    """

    def __init__(self, path, *loader_details):
        self.path = path or '.'
        self._loaders = loader_details

    def find_spec(self, name, target=None):
        """Scan self.path for name's tail and build a spec, or None.

        CPython: Lib/importlib/_bootstrap_external.py:1403 FileFinder.find_spec
        """
        import importlib.util as _util
        return _util._spec_from_search(name, [self.path])

    @classmethod
    def path_hook(cls, *loader_details):
        """Return a closure that builds a FileFinder for a directory.

        Raises ImportError for non-directory path items so get_importer
        falls through to the next hook (or None), exactly like CPython.

        CPython: Lib/importlib/_bootstrap_external.py:1467 FileFinder.path_hook
        """
        def path_hook_for_FileFinder(path):
            import os
            if not os.path.isdir(path):
                raise ImportError('only directories are supported', path=path)
            return cls(path, *loader_details)
        return path_hook_for_FileFinder


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


# Install the file-finder path hook so pkgutil.get_importer and
# runpy.run_path recognise directories on sys.path. CPython does this in
# _bootstrap_external._install; gopy's bootstrap is Go-side, so the hook
# is registered when machinery is first imported.
#
# CPython: Lib/importlib/_bootstrap_external.py:1648 _install (path_hooks)
def _install_path_hooks():
    import sys
    if getattr(sys, '_gopy_file_finder_installed', False):
        return
    # CPython orders the path hooks zipimport.zipimporter first, then the
    # FileFinder hook, so a sys.path entry pointing at a zip archive is
    # claimed by zipimport before the directory finder rejects it.
    #
    # CPython: Lib/importlib/_bootstrap_external.py:1648 _install (path_hooks)
    try:
        import zipimport
        sys.path_hooks.append(zipimport.zipimporter)
    except ImportError:
        pass
    _loader_details = (SourceFileLoader, SOURCE_SUFFIXES)
    sys.path_hooks.append(FileFinder.path_hook(_loader_details))
    sys._gopy_file_finder_installed = True


_install_path_hooks()


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
