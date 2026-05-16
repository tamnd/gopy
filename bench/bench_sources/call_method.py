# call_method — repeated bound-method dispatch.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

class Counter(object):
    def __init__(self):
        self.n = 0
    def tick(self):
        self.n += 1
    def reset(self):
        self.n = 0

def run():
    c = Counter()
    for _ in range(max(1, 100000 // _S)):
        c.tick()
        c.tick()
        c.tick()
        c.tick()
        c.tick()

run()
