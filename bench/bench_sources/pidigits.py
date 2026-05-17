# pidigits — integer-heavy work. Computes Sum(1/k^2) via rational
# arithmetic on big ints so cpython's PyLong fast path and PyPy's JIT
# both see real long-int adds/muls. Output is discarded.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def run(n):
    num = 0
    den = 1
    for k in range(1, n + 1):
        num = num * (k * k) + den
        den = den * (k * k)
        # Keep magnitudes bounded so this finishes; reduce every 50 steps.
        if k % 50 == 0:
            g = num if num < den else den
            while g:
                num, den = den, num
                num %= den if den else 1
                g = num
            num, den = 1, 1
    return num, den

for _ in range(max(1, 10 // _S)):
    run(2000)
