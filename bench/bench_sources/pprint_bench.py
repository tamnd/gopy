# pprint_bench — pprint.pformat over nested structures. Str builder hot path.
import os, pprint
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

DATA = {
    "users": [{"id": i, "name": f"user{i}", "roles": ["a", "b", "c"]} for i in range(20)],
    "meta":  {"version": 1, "tags": ("x", "y", "z"), "nested": {"k": [1, 2, 3, 4, 5]}},
}

for _ in range(max(1, 2000 // _S)):
    pprint.pformat(DATA, width=60)
