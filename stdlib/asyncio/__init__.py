"""asyncio: gopy spec-1711 staged vendor.

The byte-equal vendor from CPython Lib/asyncio/__init__.py imports
every submodule eagerly (base_events, events, futures, ...). Spec 1711
ships those incrementally; this file binds the submodules that have
already landed so `import asyncio; asyncio.exceptions` works without
the consumer reaching for `import asyncio.exceptions` separately.
gopy doesn't auto-attach sub-imports to the parent package, so the
explicit `from . import X` mirrors what `collections/__init__.py`
already does for `abc`.

When Phase 9 lands the full byte-equal vendor, this file gets replaced
with the upstream copy.

CPython: Lib/asyncio/__init__.py
"""

from . import exceptions
from . import constants
from . import format_helpers
from . import base_futures
from . import log
from . import coroutines
from . import protocols
from . import transports
from . import trsock
# futures + events ship in Phase 3 once subprocess and selectors are
# vendored; their .py files sit alongside but aren't auto-bound here
# yet so the leaf import chain stays green.
