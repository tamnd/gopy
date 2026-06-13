"""sysconfig: gopy stub.

CPython's sysconfig reads the build-time Makefile and pyconfig.h to
expose configuration values (CFLAGS, CC, prefix, installation
schemes, ...). gopy is a Go interpreter with no C build artifacts,
so every `get_config_var(name)` returns None and the schemes resolve
to sys.prefix-anchored defaults. test.support's module-level pulls
(Py_GIL_DISABLED, TEST_MODULES_ENABLED, ...) all tolerate None.

When the full sysconfig port lands this file becomes the byte-equal
vendor of Lib/sysconfig/__init__.py.

CPython: Lib/sysconfig/__init__.py
"""

import os
import sys


_CONFIG_VARS = None


def _init_config_vars():
    global _CONFIG_VARS
    _CONFIG_VARS = {
        'prefix': sys.prefix,
        'exec_prefix': sys.exec_prefix,
        'py_version': sys.version.split()[0] if sys.version else '3.14',
        'py_version_short': '3.14',
        'py_version_nodot': '314',
        'VERSION': '3.14',
        'SOABI': '',
        'EXT_SUFFIX': '.so',
        'SHLIB_SUFFIX': '.so',
        'platlibdir': 'lib',
        'platbase': sys.exec_prefix,
        'base': sys.prefix,
        'userbase': os.path.expanduser('~/.local'),
        'srcdir': sys.prefix,
        'projectbase': sys.prefix,
        'abiflags': '',
        # gopy compiles docstrings into its built-in types and functions,
        # so it matches a CPython built with --with-doc-strings (the
        # default). test.support keys MISSING_C_DOCSTRINGS off this, so
        # reporting it truthfully lets the requires_docstrings tests run.
        'WITH_DOC_STRINGS': 1,
    }


def get_config_var(name):
    """Return a single configuration variable, or None if unknown."""
    if _CONFIG_VARS is None:
        _init_config_vars()
    return _CONFIG_VARS.get(name)


def get_config_vars(*args):
    """Return all (or selected) configuration variables."""
    if _CONFIG_VARS is None:
        _init_config_vars()
    if not args:
        return _CONFIG_VARS
    return [_CONFIG_VARS.get(name) for name in args]


def get_platform():
    """Return a platform string."""
    return sys.platform


def get_python_version():
    return get_config_var('py_version_short')


def is_python_build(check_home=None):
    # Return True when Parser/asdl.py lives alongside the stdlib directory,
    # which is the case for the gopy source tree (Parser/ is vendored there).
    # Mirrors CPython's Modules/Setup probe in Lib/sysconfig/__init__.py:70.
    _f = os.path.abspath(__file__)
    _root = os.path.dirname(os.path.dirname(os.path.dirname(_f)))
    return os.path.isfile(os.path.join(_root, 'Parser', 'asdl.py'))


def get_paths(scheme=None, vars=None, expand=True):
    """Return installation paths corresponding to the scheme."""
    prefix = sys.prefix
    return {
        'stdlib': os.path.join(prefix, 'lib', 'python3.14'),
        'platstdlib': os.path.join(prefix, 'lib', 'python3.14'),
        'purelib': os.path.join(prefix, 'lib', 'python3.14', 'site-packages'),
        'platlib': os.path.join(prefix, 'lib', 'python3.14', 'site-packages'),
        'include': os.path.join(prefix, 'include', 'python3.14'),
        'platinclude': os.path.join(prefix, 'include', 'python3.14'),
        'scripts': os.path.join(prefix, 'bin'),
        'data': prefix,
    }


def get_path(name, scheme=None, vars=None, expand=True):
    return get_paths(scheme, vars, expand).get(name)


def get_path_names():
    return ('stdlib', 'platstdlib', 'purelib', 'platlib', 'include',
            'platinclude', 'scripts', 'data')


def get_scheme_names():
    return ('posix_prefix', 'posix_home', 'posix_user', 'osx_framework_user',
            'nt', 'nt_user')


def get_default_scheme():
    if os.name == 'nt':
        return 'nt'
    return 'posix_prefix'


def get_preferred_scheme(key):
    return get_default_scheme()


def get_makefile_filename():
    return os.path.join(sys.prefix, 'lib', 'python3.14', 'config', 'Makefile')


def parse_config_h(fp, vars=None):
    if vars is None:
        vars = {}
    return vars


def get_config_h_filename():
    return os.path.join(sys.prefix, 'include', 'python3.14', 'pyconfig.h')


def customize_compiler(compiler):
    """No-op: gopy has no C build path."""
    pass


def _print_dict(title, data):
    for k, v in sorted(data.items()):
        print(f'{title}["{k}"] = {v!r}')


__all__ = [
    'get_config_h_filename',
    'get_config_var',
    'get_config_vars',
    'get_default_scheme',
    'get_makefile_filename',
    'get_path',
    'get_path_names',
    'get_paths',
    'get_platform',
    'get_preferred_scheme',
    'get_python_version',
    'get_scheme_names',
    'is_python_build',
    'parse_config_h',
]
