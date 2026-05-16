# comprehensions — list/dict/set comprehensions stress tier-2 uops + frame.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def run(n):
    lst = [i * i for i in range(n) if i % 3]
    dct = {i: i * 2 for i in range(n) if i & 1}
    st  = {i % 97 for i in range(n)}
    nested = [[j for j in range(20) if (i + j) % 5] for i in range(n // 50 + 1)]
    return len(lst), len(dct), len(st), sum(len(x) for x in nested)

for _ in range(max(1, 200 // _S)):
    run(2000)
