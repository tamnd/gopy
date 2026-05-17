# typing_runtime — exercises typing.runtime_checkable Protocol + isinstance.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

from typing import Protocol, runtime_checkable

@runtime_checkable
class Quacks(Protocol):
    def quack(self) -> str: ...

class Duck:
    def quack(self) -> str: return "quack"

class Cow:
    def moo(self) -> str: return "moo"

class Parrot:
    def quack(self) -> str: return "polly"

def run(objs, n):
    hits = 0
    for _ in range(n):
        for o in objs:
            if isinstance(o, Quacks):
                hits += 1
    return hits

run([Duck(), Cow(), Parrot(), Duck()], max(1, 20000 // _S))
