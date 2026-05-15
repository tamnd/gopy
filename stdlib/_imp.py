"""_imp: gopy stub.

CPython's _imp lives in Python/import.c and exposes low-level helpers
(lock_held, acquire_lock, release_lock, find_frozen, init_frozen,
_override_frozen_modules_for_tests, ...). gopy's import system has its
own runtime (imp package, Go side) so the Python surface is mostly
unreachable; test.support.import_helper only touches the two
_override_* setters at top-level imports, both no-ops.

CPython: Python/import.c _imp module
"""


def lock_held():
    return False


def acquire_lock():
    pass


def release_lock():
    pass


def is_builtin(name):
    return 0


def is_frozen(name):
    return False


def get_frozen_object(name, data=None):
    raise ImportError(f'No such frozen object: {name!r}')


def init_frozen(name):
    return None


def create_dynamic(spec, file=None):
    raise NotImplementedError("_imp.create_dynamic is unavailable in gopy")


def exec_dynamic(module):
    raise NotImplementedError("_imp.exec_dynamic is unavailable in gopy")


def exec_builtin(module):
    return 0


def source_hash(key, source):
    return b'\x00' * 8


def _fix_co_filename(code, filename):
    return code


_frozen_overridden = 0


def _override_frozen_modules_for_tests(value):
    global _frozen_overridden
    _frozen_overridden = value


_multi_interp_override = 0


def _override_multi_interp_extensions_check(value):
    global _multi_interp_override
    prev = _multi_interp_override
    _multi_interp_override = value
    return prev


check_hash_based_pycs = 'default'
