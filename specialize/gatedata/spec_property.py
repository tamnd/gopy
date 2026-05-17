# Exercise LOAD_ATTR_PROPERTY + LOAD_ATTR_METHOD_NO_DICT fast paths.
# This script is the parity gate for spec 1712 P1.4b: cpython 3.14 and
# gopy must produce byte-equal stdout. The hot loop runs each access
# enough times to push the adaptive counter past the warmup threshold
# so the specializer kicks in. Output is the accumulated sum; the
# numeric answer is what we diff against cpython.


class WithProperty:
    __slots__ = ("_x",)

    def __init__(self, x):
        self._x = x

    @property
    def x(self):
        return self._x


class WithMethod:
    __slots__ = ("offset",)

    def __init__(self, offset):
        self.offset = offset

    def shifted(self, n):
        return self.offset + n


def exercise(n):
    p = WithProperty(7)
    m = WithMethod(3)
    total = 0
    for i in range(n):
        # LOAD_ATTR_PROPERTY: descriptor + __get__
        total += p.x
        # LOAD_ATTR_METHOD_NO_DICT: bound method on a slotted class
        total += m.shifted(i)
    return total


print(exercise(1000))
