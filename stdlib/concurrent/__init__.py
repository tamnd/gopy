"""concurrent: gopy spec-1711 stub package.

The CPython package only exposes the `futures` subpackage. gopy does
not yet ship the executor implementations (those need a thread pool);
this stub keeps `import concurrent.futures` working for the asyncio
phase-3 vendor, which only references the class objects.

CPython: Lib/concurrent/__init__.py
"""
