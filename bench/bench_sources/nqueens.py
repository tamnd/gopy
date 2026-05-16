# nqueens — backtracking solver. Pure integer + list, hits BINARY_OP, COMPARE_OP.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def solve(n):
    cols = [0] * n
    count = 0
    def place(row):
        nonlocal count
        if row == n:
            count += 1
            return
        for c in range(n):
            ok = True
            for r in range(row):
                d = c - cols[r]
                if cols[r] == c or d == row - r or d == -(row - r):
                    ok = False
                    break
            if ok:
                cols[row] = c
                place(row + 1)
    place(0)
    return count

for _ in range(max(1, 3 // _S)):
    solve(9)
