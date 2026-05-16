# unpack_sequence — measure tuple/list unpacking through STORE_FAST.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def do_tuple(t):
    a, b, c, d, e, f, g, h, i, j = t

def do_list(t):
    a, b, c, d, e, f, g, h, i, j = t

T = (1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
L = list(T)

for _ in range(max(1, 50000 // _S)):
    do_tuple(T)
    do_list(L)
