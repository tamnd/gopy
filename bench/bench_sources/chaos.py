# chaos — chaos-game point iteration. Float + small-list-heavy.
import os, math
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def run(iters):
    seeds = [(0.0, 0.0), (1.0, 0.0), (0.5, math.sqrt(0.75))]
    x, y = 0.5, 0.5
    state = 1
    for i in range(iters):
        # Linear-congruential PRNG (avoids stdlib random for stable timing).
        state = (state * 1103515245 + 12345) & 0x7fffffff
        sx, sy = seeds[state % 3]
        x = (x + sx) * 0.5
        y = (y + sy) * 0.5
    return x, y

run(max(100, 200000 // _S))
