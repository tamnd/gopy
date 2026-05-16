# hexiom — small constraint-counting kernel inspired by the hexiom puzzle.
# Tight integer + list-of-list workload.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def neighbors(i, j, n):
    out = []
    for di, dj in ((-1, 0), (1, 0), (0, -1), (0, 1), (-1, -1), (1, 1)):
        ni, nj = i + di, j + dj
        if 0 <= ni < n and 0 <= nj < n:
            out.append((ni, nj))
    return out

def solve(n):
    grid = [[(i * 7 + j * 3) % 7 for j in range(n)] for i in range(n)]
    score = 0
    for i in range(n):
        for j in range(n):
            v = grid[i][j]
            adj = sum(grid[ni][nj] for ni, nj in neighbors(i, j, n))
            if adj == v:
                score += 1
            score = (score * 31 + adj) & 0xffff
    return score

for _ in range(max(1, 200 // _S)):
    solve(12)
