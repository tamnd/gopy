# deepcopy — copy.deepcopy on a nested dict/list structure.
import os, copy
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

LEAF = {"a": 1, "b": [1, 2, 3], "c": (4, 5, 6)}
NEST = {"k": LEAF, "lst": [LEAF, LEAF, LEAF], "deep": [[LEAF] * 5 for _ in range(5)]}

for _ in range(max(1, 1000 // _S)):
    copy.deepcopy(NEST)
