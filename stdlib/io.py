"""The io module provides the Python interfaces to stream handling.

This is a gopy stop-gap. CPython's Lib/io.py subclasses ``_io._IOBase``
through ``abc.ABCMeta`` so the ABC hierarchy can be registered
explicitly in Python land. gopy has not ported ``abc`` yet, so the
full vendor cannot run; this file re-exports the names from the
built-in ``_io`` module that callers actually reach for. As more of
the io family lands (BytesIO, FileIO, the Buffered* family,
TextIOWrapper) the names below get the same treatment, and once
``abc`` and ``_collections_abc`` are available this file is replaced
by the byte-identical CPython copy.

CPython: Lib/io.py
"""

from _io import (
    DEFAULT_BUFFER_SIZE,
    StringIO,
    BytesIO,
    FileIO,
    BufferedReader,
    BufferedWriter,
    BufferedRWPair,
    BufferedRandom,
    TextIOWrapper,
    IncrementalNewlineDecoder,
    UnsupportedOperation,
    open,
    open_code,
    text_encoding,
)

# CPython: Lib/io.py:64 SEEK_SET / SEEK_CUR / SEEK_END
SEEK_SET = 0
SEEK_CUR = 1
SEEK_END = 2

BlockingIOError = BlockingIOError

__all__ = [
    "BlockingIOError",
    "open",
    "open_code",
    "FileIO",
    "BytesIO",
    "StringIO",
    "BufferedReader",
    "BufferedWriter",
    "BufferedRWPair",
    "BufferedRandom",
    "TextIOWrapper",
    "UnsupportedOperation",
    "SEEK_SET",
    "SEEK_CUR",
    "SEEK_END",
    "DEFAULT_BUFFER_SIZE",
    "text_encoding",
    "IncrementalNewlineDecoder",
]
