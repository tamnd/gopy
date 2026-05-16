# float — tight float arithmetic loop. Exercises BINARY_OP_*_FLOAT.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def run(n):
    a, b, c = 1.1, 2.2, 3.3
    for i in range(n):
        a = a + b * 0.5
        b = b - c * 0.25
        c = c + a * 0.125
        a = a * 0.9999
    return a + b + c

run(max(1, 200000 // _S))
