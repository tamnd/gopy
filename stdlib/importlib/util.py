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

from importlib._bootstrap_external import (
    MAGIC_NUMBER,
    cache_from_source,
    decode_source,
    source_from_cache,
    source_hash,
)


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


class _ModuleSpec:
    """Stripped-down ModuleSpec mirroring importlib.machinery.ModuleSpec.

    CPython: Lib/importlib/_bootstrap.py:392 ModuleSpec
    """

    def __init__(self, name, loader, *, origin=None, is_package=False):
        self.name = name
        self.loader = loader
        self.origin = origin
        self.submodule_search_locations = [] if is_package else None
        self.has_location = origin is not None
        self.cached = None
        self.parent = name.rpartition(".")[0] if is_package else name.rpartition(".")[0]


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
    search = _resolve_search_paths(name)
    if search is None:
        return None
    tail = name.rpartition(".")[2]
    for entry in search:
        directory = entry if entry else "."
        pkg_init = os.path.join(directory, tail, "__init__.py")
        if os.path.isfile(pkg_init):
            loader = _SourceFileLoader(name, pkg_init)
            spec = _ModuleSpec(name, loader, origin=pkg_init, is_package=True)
            spec.submodule_search_locations = [os.path.join(directory, tail)]
            return spec
        mod_file = os.path.join(directory, tail + ".py")
        if os.path.isfile(mod_file):
            return _ModuleSpec(name, _SourceFileLoader(name, mod_file),
                               origin=mod_file)
    return None


def module_from_spec(spec):
    """Create a new module based on spec and spec.loader.create_module.

    CPython: Lib/importlib/_bootstrap.py:571 module_from_spec
    """
    import types
    module = None
    if hasattr(spec.loader, 'create_module'):
        module = spec.loader.create_module(spec)
    if module is None:
        module = types.ModuleType(spec.name)
    module.__loader__ = spec.loader
    module.__spec__ = spec
    module.__package__ = spec.name.rpartition('.')[0]
    if spec.origin is not None:
        module.__file__ = spec.origin
    if spec.submodule_search_locations is not None:
        module.__path__ = list(spec.submodule_search_locations)
    return module


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
    return _ModuleSpec(name, loader, origin=origin, is_package=bool(is_package))


def spec_from_file_location(name, location=None, *, loader=None,
                            submodule_search_locations=None):
    """Return a ModuleSpec for the specified module, using file location.

    CPython: Lib/importlib/util.py:132 spec_from_file_location
    """
    if location is None and loader is None:
        return None
    if loader is None and location is not None:
        loader = _SourceFileLoader(name, str(location))
    origin = str(location) if location is not None else getattr(loader, 'path', None)
    is_package = submodule_search_locations is not None
    spec = _ModuleSpec(name, loader, origin=origin, is_package=is_package)
    if submodule_search_locations is not None:
        spec.submodule_search_locations = list(submodule_search_locations)
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
