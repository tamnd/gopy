# spectral_norm — power-iteration on a kernel matrix. Float + list-heavy.
import os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

def A(i, j):
    return 1.0 / ((i + j) * (i + j + 1) // 2 + i + 1)

def Au(u):
    n = len(u)
    out = [0.0] * n
    for i in range(n):
        s = 0.0
        for j in range(n):
            s += A(i, j) * u[j]
        out[i] = s
    return out

def Atu(u):
    n = len(u)
    out = [0.0] * n
    for i in range(n):
        s = 0.0
        for j in range(n):
            s += A(j, i) * u[j]
        out[i] = s
    return out

def AtAu(u):
    return Atu(Au(u))

def run(n):
    u = [1.0] * n
    for _ in range(5):
        v = AtAu(u)
        u = AtAu(v)
    return u

run(max(2, 60 // _S))
