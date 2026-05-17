# go_bench — minimal Go-board liberty-counting kernel. Dict + tuple heavy.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

SIZE = 9

def neighbors(p):
    x, y = p
    out = []
    if x > 0: out.append((x - 1, y))
    if x < SIZE - 1: out.append((x + 1, y))
    if y > 0: out.append((x, y - 1))
    if y < SIZE - 1: out.append((x, y + 1))
    return out

def liberties(board, p):
    color = board.get(p)
    if color is None:
        return 0
    seen = {p}
    stack = [p]
    libs = set()
    while stack:
        q = stack.pop()
        for n in neighbors(q):
            if n in seen:
                continue
            c = board.get(n)
            if c is None:
                libs.add(n)
            elif c == color:
                seen.add(n)
                stack.append(n)
    return len(libs)

def run():
    board = {}
    state = 1
    for _ in range(40):
        state = (state * 1103515245 + 12345) & 0x7fffffff
        x = state % SIZE
        state = (state * 1103515245 + 12345) & 0x7fffffff
        y = state % SIZE
        board[(x, y)] = "B" if (x + y) & 1 else "W"
    total = 0
    for p in board:
        total += liberties(board, p)
    return total

for _ in range(max(1, 5000 // _S)):
    run()
