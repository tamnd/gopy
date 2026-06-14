"""importlib.util: gopy stub for the parts pkgutil/unittest.mock need.

CPython's Lib/importlib/util.py re-exports symbols from the import
machinery's _bootstrap and _bootstrap_external modules, which gopy
doesn't fully ship. The pkgutil/unittest.mock load path only references
MAGIC_NUMBER at module load (inside a function body) plus find_spec
later; resolve_name doesn't touch util at all. Until spec 1711 Phase
9 wires the full importlib port this stub keeps the import chain
green.

CPython: Lib/importlib/util.py
"""

import os
import sys
import types

from importlib._bootstrap import module_from_spec
from importlib._bootstrap_external import (
    MAGIC_NUMBER,
    cache_from_source,
    decode_source,
    source_from_cache,
    source_hash,
)
from importlib.machinery import ModuleSpec


class _SourceFileLoader:
    """Minimal SourceFileLoader: reads the .py file and compiles it.

    CPython: Lib/importlib/_bootstrap_external.py:962 SourceFileLoader
    """

    def __init__(self, name, path):
        self.name = name
        self.path = path

    def get_filename(self, fullname=None):
        return self.path

    def get_source(self, fullname=None):
        with open(self.path, "rb") as f:
            data = f.read()
        try:
            return data.decode("utf-8")
        except UnicodeDecodeError:
            return data.decode("latin-1")

    def get_code(self, fullname):
        source = self.get_source(fullname)
        return compile(source, self.path, "exec")


def _resolve_search_paths(name):
    parent, _, _ = name.rpartition(".")
    if not parent:
        return sys.path
    pkg = sys.modules.get(parent)
    if pkg is None:
        try:
            __import__(parent)
        except ImportError:
            return None
        pkg = sys.modules.get(parent)
    if pkg is None:
        return None
    return getattr(pkg, "__path__", None)


def _spec_from_search(name, search):
    """Scan the directory list `search` for name's tail and build a spec.

    Shared by find_spec and _find_spec_from_path; mirrors the suffix
    loop FileFinder.find_spec runs against a single path entry list.

    CPython: Lib/importlib/_bootstrap_external.py:1357 FileFinder.find_spec
    """
    if search is None:
        return None
    tail = name.rpartition(".")[2]
    # PEP 420: every directory that matches the tail but lacks a regular
    # __init__.py / module file contributes a namespace portion. CPython's
    # PathFinder accumulates these across all path entries and, if no
    # concrete module is found, returns a namespace spec whose loader is
    # None and whose search locations are the collected portions.
    #
    # CPython: Lib/importlib/_bootstrap_external.py:1496 _fill_cache /
    # PathFinder._get_spec namespace_path accumulation
    namespace_portions = []
    for entry in search:
        directory = entry if entry else "."
        pkg_init = os.path.join(directory, tail, "__init__.py")
        if os.path.isfile(pkg_init):
            return spec_from_file_location(
                name, pkg_init,
                loader=_SourceFileLoader(name, pkg_init),
                submodule_search_locations=[os.path.join(directory, tail)])
        mod_file = os.path.join(directory, tail + ".py")
        if os.path.isfile(mod_file):
            return spec_from_file_location(
                name, mod_file, loader=_SourceFileLoader(name, mod_file))
        pkg_dir = os.path.join(directory, tail)
        if os.path.isdir(pkg_dir):
            namespace_portions.append(pkg_dir)
    if namespace_portions:
        spec = ModuleSpec(name, None, is_package=True)
        spec.submodule_search_locations = list(namespace_portions)
        return spec
    return None


def find_spec(name, package=None):
    """Locate name on sys.path (or the parent package's __path__) and
    return a ModuleSpec the caller can drive through .loader.get_code().

    CPython: Lib/importlib/util.py:90 find_spec
    """
    if name.startswith("."):
        if package is None:
            raise ValueError("relative module name requires package")
        name = resolve_name(name, package)
    if name in sys.modules:
        mod = sys.modules[name]
        spec = getattr(mod, "__spec__", None)
        if spec is not None:
            return spec
    return _spec_from_search(name, _resolve_search_paths(name))


def _find_spec_from_path(name, path=None):
    """Return the spec for name, searching `path` (or sys.path).

    First sys.modules is checked; if the module is already imported its
    __spec__ is returned (None __spec__ raises ValueError). Otherwise the
    given path is scanned. Dotted names do not implicitly import their
    parents, matching CPython.

    CPython: Lib/importlib/util.py:_find_spec_from_path
    """
    if name not in sys.modules:
        search = sys.path if path is None else path
        return _spec_from_search(name, search)
    module = sys.modules[name]
    if module is None:
        return None
    try:
        spec = module.__spec__
    except AttributeError:
        raise ValueError(f'{name}.__spec__ is not set') from None
    if spec is None:
        raise ValueError(f'{name}.__spec__ is None')
    return spec


def spec_from_loader(name, loader, *, origin=None, is_package=None):
    """Return a ModuleSpec based on a loader.

    CPython: Lib/importlib/util.py:44 spec_from_loader
    """
    if origin is None and hasattr(loader, 'get_filename'):
        try:
            origin = loader.get_filename(name)
        except (ImportError, AttributeError):
            pass
    if is_package is None:
        if hasattr(loader, 'is_package'):
            try:
                is_package = loader.is_package(name)
            except ImportError:
                is_package = False
        else:
            is_package = False
    return ModuleSpec(name, loader, origin=origin, is_package=bool(is_package))


def spec_from_file_location(name, location=None, *, loader=None,
                            submodule_search_locations=None):
    """Return a ModuleSpec for the specified module, using file location.

    CPython: Lib/importlib/_bootstrap_external.py:560 spec_from_file_location
    """
    if location is None and loader is None:
        return None
    if loader is None and location is not None:
        loader = _SourceFileLoader(name, str(location))
    if location is not None:
        location = os.fspath(location)
        try:
            location = os.path.abspath(location)
        except (OSError, AttributeError):
            # AttributeError: os.path (posixpath) can still be mid-import
            # when a deferred spec is flushed during interpreter bootstrap,
            # so abspath is not bound yet. The unnormalized path is fine.
            pass
    origin = location if location is not None else getattr(loader, 'path', None)
    spec = ModuleSpec(name, loader, origin=origin)
    spec._set_fileattr = True
    if submodule_search_locations is not None:
        spec.submodule_search_locations = list(submodule_search_locations)
    elif submodule_search_locations is None and hasattr(loader, 'is_package'):
        try:
            if loader.is_package(name):
                spec.submodule_search_locations = []
        except ImportError:
            pass
    if spec.submodule_search_locations == []:
        if origin:
            spec.submodule_search_locations.append(os.path.split(origin)[0])
    return spec


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
