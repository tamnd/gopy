# json_dumps — serialize a moderately nested dict many times.
import json, os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

EMPTY = {}
SIMPLE = {"key1": 0, "key2": True, "key3": "value", "key4": "foo", "key5": "bar"}
NESTED = {"key": SIMPLE, "list": [SIMPLE] * 10}
HUGE = [NESTED] * 100

def run():
    for _ in range(max(1, 200 // _S)):
        json.dumps(EMPTY)
        json.dumps(SIMPLE)
        json.dumps(NESTED)
        json.dumps(HUGE)

run()
