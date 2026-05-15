"""concurrent.futures: gopy stub for the parts asyncio.futures touches.

The CPython package re-exports Future, Executor, ThreadPoolExecutor,
ProcessPoolExecutor, CancelledError, InvalidStateError, TimeoutError,
plus the as_completed/wait/wrap helpers. asyncio.futures only needs
the Future / CancelledError / InvalidStateError class objects for
isinstance checks; the executor surface fails on first use until the
real port lands.

CPython: Lib/concurrent/futures/__init__.py
         Lib/concurrent/futures/_base.py
"""

# CPython 3.8+ moved CancelledError to builtins and re-exported it
# here. gopy's builtins module does not surface it yet, so define the
# class locally; identity is what asyncio.futures._convert_future_exc
# checks against.
#
# CPython: Lib/concurrent/futures/_base.py CancelledError
class CancelledError(BaseException):
    """The Future was cancelled."""


class InvalidStateError(Exception):
    """The operation is not allowed in this state."""


class BrokenExecutor(RuntimeError):
    """Raised when a executor has become non-functional."""


PENDING = 'PENDING'
RUNNING = 'RUNNING'
CANCELLED = 'CANCELLED'
CANCELLED_AND_NOTIFIED = 'CANCELLED_AND_NOTIFIED'
FINISHED = 'FINISHED'

_FUTURE_STATES = [PENDING, RUNNING, CANCELLED, CANCELLED_AND_NOTIFIED, FINISHED]


class Future:
    """A future encapsulates the result of an asynchronous computation.

    gopy stub: the class object is what asyncio.futures.wrap_future and
    isfuture() check against. Actual scheduling lands with the full
    concurrent.futures port.

    CPython: Lib/concurrent/futures/_base.py Future
    """

    def __init__(self):
        self._state = PENDING
        self._result = None
        self._exception = None

    def cancel(self):
        raise NotImplementedError("concurrent.futures.Future is unavailable in gopy")

    def cancelled(self):
        return False

    def running(self):
        return False

    def done(self):
        return self._state in (CANCELLED, CANCELLED_AND_NOTIFIED, FINISHED)

    def result(self, timeout=None):
        raise NotImplementedError("concurrent.futures.Future is unavailable in gopy")

    def exception(self, timeout=None):
        raise NotImplementedError("concurrent.futures.Future is unavailable in gopy")

    def add_done_callback(self, fn):
        raise NotImplementedError("concurrent.futures.Future is unavailable in gopy")

    def set_running_or_notify_cancel(self):
        raise NotImplementedError("concurrent.futures.Future is unavailable in gopy")

    def set_result(self, result):
        raise NotImplementedError("concurrent.futures.Future is unavailable in gopy")

    def set_exception(self, exception):
        raise NotImplementedError("concurrent.futures.Future is unavailable in gopy")


class Executor:
    """gopy stub. See class docstring above."""

    def submit(self, fn, /, *args, **kwargs):
        raise NotImplementedError("concurrent.futures.Executor is unavailable in gopy")

    def map(self, fn, *iterables, timeout=None, chunksize=1):
        raise NotImplementedError("concurrent.futures.Executor is unavailable in gopy")

    def shutdown(self, wait=True, *, cancel_futures=False):
        pass

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.shutdown(wait=True)
        return False


class ThreadPoolExecutor(Executor):
    def __init__(self, max_workers=None, thread_name_prefix='', initializer=None, initargs=()):
        raise NotImplementedError("concurrent.futures.ThreadPoolExecutor is unavailable in gopy")


class ProcessPoolExecutor(Executor):
    def __init__(self, max_workers=None, mp_context=None, initializer=None, initargs=()):
        raise NotImplementedError("concurrent.futures.ProcessPoolExecutor is unavailable in gopy")


def wait(fs, timeout=None, return_when='ALL_COMPLETED'):
    raise NotImplementedError("concurrent.futures.wait is unavailable in gopy")


def as_completed(fs, timeout=None):
    raise NotImplementedError("concurrent.futures.as_completed is unavailable in gopy")


FIRST_COMPLETED = 'FIRST_COMPLETED'
FIRST_EXCEPTION = 'FIRST_EXCEPTION'
ALL_COMPLETED = 'ALL_COMPLETED'

__all__ = (
    'FIRST_COMPLETED',
    'FIRST_EXCEPTION',
    'ALL_COMPLETED',
    'CancelledError',
    'BrokenExecutor',
    'InvalidStateError',
    'Future',
    'Executor',
    'wait',
    'as_completed',
    'ProcessPoolExecutor',
    'ThreadPoolExecutor',
)
