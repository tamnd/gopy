"""Pyc-writer slice of CPython's Lib/importlib/_bootstrap_external.py.

The vendored file ships only the parts py_compile needs: MAGIC_NUMBER,
_pack_uint32 / _unpack_uint*, _calc_mode, _write_atomic, _classify_pyc
plus the two pyc-data builders _code_to_timestamp_pyc /
_code_to_hash_pyc. The path / loader / spec scaffolding lives in the
companion util.py stub until spec 1711 wires the full module.

CPython: Lib/importlib/_bootstrap_external.py
"""

import _imp
import marshal
import os as _os
import sys


_MS_WINDOWS = (sys.platform == 'win32')
if _MS_WINDOWS:
    path_separators = ['\\', '/']
else:
    path_separators = ['/']
# Assumption made in _path_join()
assert all(len(sep) == 1 for sep in path_separators)
path_sep = path_separators[0]
path_sep_tuple = tuple(path_separators)
path_separators = ''.join(path_separators)


# CPython: Lib/importlib/_bootstrap_external.py:79 _pack_uint32
def _pack_uint32(x):
    """Convert a 32-bit integer to little-endian."""
    return (int(x) & 0xFFFFFFFF).to_bytes(4, 'little')


# CPython: Lib/importlib/_bootstrap_external.py:84 _unpack_uint64
def _unpack_uint64(data):
    """Convert 8 bytes in little-endian to an integer."""
    assert len(data) == 8
    return int.from_bytes(data, 'little')


# CPython: Lib/importlib/_bootstrap_external.py:89 _unpack_uint32
def _unpack_uint32(data):
    """Convert 4 bytes in little-endian to an integer."""
    assert len(data) == 4
    return int.from_bytes(data, 'little')


# CPython: Lib/importlib/_bootstrap_external.py:94 _unpack_uint16
def _unpack_uint16(data):
    """Convert 2 bytes in little-endian to an integer."""
    assert len(data) == 2
    return int.from_bytes(data, 'little')


# CPython: Lib/importlib/_bootstrap_external.py:200 _write_atomic
def _write_atomic(path, data, mode=0o666):
    """Best-effort function to write data to a path atomically.
    Be prepared to handle a FileExistsError if concurrent writing of the
    temporary file is attempted."""
    path_tmp = f'{path}.{id(path)}'
    fd = _os.open(path_tmp,
                  _os.O_EXCL | _os.O_CREAT | _os.O_WRONLY, mode & 0o666)
    try:
        with open(fd, 'wb') as file:
            file.write(data)
        _os.replace(path_tmp, path)
    except OSError:
        try:
            _os.unlink(path_tmp)
        except OSError:
            pass
        raise


# CPython: Lib/importlib/_bootstrap_external.py:224 MAGIC_NUMBER
MAGIC_NUMBER = _imp.pyc_magic_number_token.to_bytes(4, 'little')

_PYCACHE = '__pycache__'
_OPT = 'opt-'

SOURCE_SUFFIXES = ['.py']
BYTECODE_SUFFIXES = ['.pyc']


# CPython: Lib/importlib/_bootstrap_external.py:381 _calc_mode
def _calc_mode(path):
    """Calculate the mode permissions for a bytecode file."""
    try:
        mode = _os.stat(path).st_mode
    except OSError:
        mode = 0o666
    # We always ensure write access so we can update cached files
    # later even when the source files are read-only on Windows (#6074)
    mode |= 0o200
    return mode


# CPython: Lib/importlib/_bootstrap_external.py:424 _classify_pyc
def _classify_pyc(data, name, exc_details):
    """Perform basic validity checking of a pyc header and return the flags field,
    which determines how the pyc should be further validated against the source.

    *data* is the contents of the pyc file. (Only the first 16 bytes are
    required, though.)

    *name* is the name of the module being imported. It is used for logging.

    *exc_details* is a dictionary passed to ImportError if it raised for
    improved debugging.

    ImportError is raised when the magic number is incorrect or when the flags
    field is invalid. EOFError is raised when the data is found to be truncated.

    """
    magic = data[:4]
    if magic != MAGIC_NUMBER:
        message = f'bad magic number in {name!r}: {magic!r}'
        raise ImportError(message, **exc_details)
    if len(data) < 16:
        message = f'reached EOF while reading pyc header of {name!r}'
        raise EOFError(message)
    flags = _unpack_uint32(data[4:8])
    # Only the first two flags are defined.
    if flags & ~0b11:
        message = f'invalid flags {flags!r} in {name!r}'
        raise ImportError(message, **exc_details)
    return flags


# CPython: Lib/importlib/_bootstrap_external.py:457 _validate_timestamp_pyc
def _validate_timestamp_pyc(data, source_mtime, source_size, name,
                            exc_details):
    """Validate a pyc against the source last-modified time."""
    if _unpack_uint32(data[8:12]) != (source_mtime & 0xFFFFFFFF):
        message = f'bytecode is stale for {name!r}'
        raise ImportError(message, **exc_details)
    if (source_size is not None and
        _unpack_uint32(data[12:16]) != (source_size & 0xFFFFFFFF)):
        raise ImportError(f'bytecode is stale for {name!r}', **exc_details)


# CPython: Lib/importlib/_bootstrap_external.py:485 _validate_hash_pyc
def _validate_hash_pyc(data, source_hash, name, exc_details):
    """Validate a hash-based pyc by checking the real source hash against the one in
    the pyc header.
    """
    if data[8:16] != source_hash:
        raise ImportError(
            f'hash in bytecode doesn\'t match hash of source {name!r}',
            **exc_details,
        )


# CPython: Lib/importlib/_bootstrap_external.py:522 _code_to_timestamp_pyc
def _code_to_timestamp_pyc(code, mtime=0, source_size=0):
    "Produce the data for a timestamp-based pyc."
    data = bytearray(MAGIC_NUMBER)
    data.extend(_pack_uint32(0))
    data.extend(_pack_uint32(mtime))
    data.extend(_pack_uint32(source_size))
    data.extend(marshal.dumps(code))
    return data


# CPython: Lib/importlib/_bootstrap_external.py:532 _code_to_hash_pyc
def _code_to_hash_pyc(code, source_hash, checked=True):
    "Produce the data for a hash-based pyc."
    data = bytearray(MAGIC_NUMBER)
    flags = 0b1 | checked << 1
    data.extend(_pack_uint32(flags))
    assert len(source_hash) == 8
    data.extend(source_hash)
    data.extend(marshal.dumps(code))
    return data


# CPython exposes this as importlib.util.source_hash. We thread it
# through the same _imp builtin _bootstrap_external relies on.
#
# CPython: Lib/importlib/util.py source_hash (re-export of _imp.source_hash)
def source_hash(source_bytes):
    """Return the hash of *source_bytes* as bytes."""
    return _imp.source_hash(_RAW_MAGIC_NUMBER, source_bytes)


# _RAW_MAGIC_NUMBER mirrors CPython: the integer form of MAGIC_NUMBER is
# fed straight back into _imp.source_hash as the SipHash key. Keeping
# the conversion in one place avoids endian-swap mistakes at call sites.
#
# CPython: Lib/importlib/_bootstrap_external.py:223 _RAW_MAGIC_NUMBER
_RAW_MAGIC_NUMBER = _imp.pyc_magic_number_token


# CPython: Lib/importlib/_bootstrap_external.py:543 decode_source
def decode_source(source_bytes):
    """Decode bytes representing source code and return the string.

    Universal newline support is used in the decoding.
    """
    # gopy doesn't have tokenize.detect_encoding wired through this path
    # yet, so fall back to utf-8 (matching what test.support feeds in).
    if isinstance(source_bytes, str):
        return source_bytes
    return source_bytes.decode('utf-8')


# CPython: Lib/importlib/_bootstrap_external.py:912 FileLoader
class FileLoader:
    """Base file loader class.

    The gopy port skips the readers/finders machinery and keeps only the
    file-access shape py_compile needs.
    """

    def __init__(self, fullname, path):
        self.name = fullname
        self.path = path

    def get_filename(self, fullname=None):
        if fullname is not None and fullname != self.name:
            raise ImportError(
                f'loader for {self.name} cannot handle {fullname}',
                name=fullname,
            )
        return self.path

    def get_data(self, path):
        """Return the data from path as raw bytes."""
        with open(path, 'rb') as file:
            return file.read()


# CPython: Lib/importlib/_bootstrap_external.py:962 SourceFileLoader
class SourceFileLoader(FileLoader):
    """Concrete loader for source files. Implements the slice of the
    SourceLoader / FileLoader contract py_compile.compile() drives:
    get_data, get_filename, source_to_code, path_stats.
    """

    # CPython: Lib/importlib/_bootstrap_external.py:818 source_to_code
    def source_to_code(self, data, path, *, _optimize=-1):
        """Return the code object compiled from source."""
        return compile(data, path, 'exec',
                       dont_inherit=True, optimize=_optimize)

    # CPython: Lib/importlib/_bootstrap_external.py:966 path_stats
    def path_stats(self, path):
        st = _os.stat(path)
        return {'mtime': st.st_mtime, 'size': st.st_size}


# CPython: Lib/importlib/_bootstrap_external.py:101 _path_join
def _path_join(*path_parts):
    """Replacement for os.path.join()."""
    return path_sep.join([part.rstrip(path_separators)
                          for part in path_parts if part])


# CPython: Lib/importlib/_bootstrap_external.py:107 _path_split
def _path_split(path):
    """Replacement for os.path.split()."""
    i = max(path.rfind(p) for p in path_separators)
    if i < 0:
        return '', path
    return path[:i], path[i + 1:]


# CPython: Lib/importlib/_bootstrap_external.py:202 _path_isabs
def _path_isabs(path):
    """Replacement for os.path.isabs."""
    if not path:
        return False
    return path[0] in path_separators


# CPython: Lib/importlib/_bootstrap_external.py:217 _path_abspath
def _path_abspath(path):
    """Replacement for os.path.abspath."""
    if not _path_isabs(path):
        for sep in path_separators:
            path = path.removeprefix(f".{sep}")
        return _path_join(_os.getcwd(), path)
    else:
        return path


# CPython: Lib/importlib/_bootstrap_external.py:239 cache_from_source
def cache_from_source(path, debug_override=None, *, optimization=None):
    """Given the path to a .py file, return the path to its .pyc file.

    The .py file does not need to exist; this simply returns the path to the
    .pyc file calculated as if the .py file were imported.

    The 'optimization' parameter controls the presumed optimization level of
    the bytecode file. If 'optimization' is not None, the string representation
    of the argument is taken and verified to be alphanumeric (else ValueError
    is raised).

    The debug_override parameter is deprecated. If debug_override is not None,
    a True value is the same as setting 'optimization' to the empty string
    while a False value is equivalent to setting 'optimization' to '1'.

    If sys.implementation.cache_tag is None then NotImplementedError is raised.
    """
    if debug_override is not None:
        if optimization is not None:
            message = 'debug_override or optimization must be set to None'
            raise TypeError(message)
        optimization = '' if debug_override else 1
    path = _os.fspath(path)
    head, tail = _path_split(path)
    base, sep, rest = tail.rpartition('.')
    tag = sys.implementation.cache_tag
    if tag is None:
        raise NotImplementedError('sys.implementation.cache_tag is None')
    almost_filename = ''.join([(base if base else rest), sep, tag])
    if optimization is None:
        if sys.flags.optimize == 0:
            optimization = ''
        else:
            optimization = sys.flags.optimize
    optimization = str(optimization)
    if optimization != '':
        if not optimization.isalnum():
            raise ValueError(f'{optimization!r} is not alphanumeric')
        almost_filename = f'{almost_filename}.{_OPT}{optimization}'
    filename = almost_filename + BYTECODE_SUFFIXES[0]
    if getattr(sys, 'pycache_prefix', None) is not None:
        head = _path_abspath(head)
        if head[1:2] == ':' and head[0:1] not in path_separators:
            head = head[2:]
        return _path_join(
            sys.pycache_prefix,
            head.lstrip(path_separators),
            filename,
        )
    return _path_join(head, _PYCACHE, filename)


# CPython: Lib/importlib/_bootstrap_external.py:310 source_from_cache
def source_from_cache(path):
    """Given the path to a .pyc. file, return the path to its .py file.

    The .pyc file does not need to exist; this simply returns the path to
    the .py file calculated to correspond to the .pyc file.  If path does
    not conform to PEP 3147/488 format, ValueError will be raised. If
    sys.implementation.cache_tag is None then NotImplementedError is raised.
    """
    if sys.implementation.cache_tag is None:
        raise NotImplementedError('sys.implementation.cache_tag is None')
    path = _os.fspath(path)
    head, pycache_filename = _path_split(path)
    found_in_pycache_prefix = False
    if getattr(sys, 'pycache_prefix', None) is not None:
        stripped_path = sys.pycache_prefix.rstrip(path_separators)
        if head.startswith(stripped_path + path_sep):
            head = head[len(stripped_path):]
            found_in_pycache_prefix = True
    if not found_in_pycache_prefix:
        head, pycache = _path_split(head)
        if pycache != _PYCACHE:
            raise ValueError(f'{_PYCACHE} not bottom-level directory in '
                             f'{path!r}')
    dot_count = pycache_filename.count('.')
    if dot_count not in {2, 3}:
        raise ValueError(f'expected only 2 or 3 dots in {pycache_filename!r}')
    elif dot_count == 3:
        optimization = pycache_filename.rsplit('.', 2)[-2]
        if not optimization.startswith(_OPT):
            raise ValueError("optimization portion of filename does not start "
                             f"with {_OPT!r}")
        opt_level = optimization[len(_OPT):]
        if not opt_level.isalnum():
            raise ValueError(f"optimization level {opt_level!r} is not an "
                             "alphanumeric value")
    base_filename = pycache_filename.partition('.')[0]
    return _path_join(head, base_filename + SOURCE_SUFFIXES[0])
