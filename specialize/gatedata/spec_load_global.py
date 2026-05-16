# Exercise LOAD_GLOBAL_MODULE + LOAD_GLOBAL_BUILTIN fast paths.
# Parity gate for spec 1712 P1.4b. The hot loop reads a module-level
# global (G) and a builtin (len) enough times to push the adaptive
# counter past the warmup threshold so the specializer kicks in.

G = 42


def exercise(n):
    total = 0
    seq = (1, 2, 3, 4, 5)
    for _ in range(n):
        total += G        # LOAD_GLOBAL_MODULE
        total += len(seq) # LOAD_GLOBAL_BUILTIN + CALL
    return total


print(exercise(1000))
