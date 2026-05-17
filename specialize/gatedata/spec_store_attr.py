# Exercise STORE_ATTR_SLOT fast path.
# Parity gate for spec 1712 P1.4b. A class with __slots__ has no
# __dict__, so the specializer pins STORE_ATTR_SLOT and the VM arm
# writes the value directly to the cached slot index without walking
# the descriptor protocol. Output is the final accumulated sum that
# both cpython 3.14 and gopy must agree on.


class Point:
    __slots__ = ("x", "y")

    def __init__(self):
        self.x = 0
        self.y = 0


def exercise(n):
    p = Point()
    total = 0
    for i in range(n):
        p.x = i        # STORE_ATTR_SLOT
        p.y = i * 2    # STORE_ATTR_SLOT
        total += p.x + p.y
    return total


print(exercise(1000))
