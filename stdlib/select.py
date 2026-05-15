"""select: gopy stub.

The real Modules/selectmodule.c wraps select/poll/epoll/kqueue and is
the foundation subprocess.py + asyncio sit on. gopy doesn't host child
processes yet, so the stub only carries the constants and the public
class names so `import select` and `getattr(select, 'PIPE_BUF', ...)`
both keep working at module load. First use of select/poll/epoll
raises NotImplementedError.

CPython: Modules/selectmodule.c
"""


PIPE_BUF = 512


class error(OSError):
    """select.error alias kept for legacy callers."""


def select(rlist, wlist, xlist, timeout=None):
    raise NotImplementedError("select.select is unavailable in gopy")


# poll / epoll / kqueue intentionally absent: selectors._can_use uses
# getattr(select, name, None) and treats None as "not on this platform"
# without raising. Falling back to selectors.SelectSelector is fine
# because subprocess.py only calls select() once children exist, and
# gopy doesn't run children yet.

# Module-level POLL* constants for code that imports them directly.
POLLIN = 0x001
POLLPRI = 0x002
POLLOUT = 0x004
POLLERR = 0x008
POLLHUP = 0x010
POLLNVAL = 0x020
