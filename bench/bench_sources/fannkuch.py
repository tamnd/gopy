# fannkuch-redux, BSD/CLBG style. Standalone.

def fannkuch(n):
    count = list(range(1, n+1))
    max_flips = 0
    m = n - 1
    r = n
    perm1 = list(range(n))
    perm = list(range(n))
    perm1_ins = perm1.insert
    perm1_pop = perm1.pop
    while True:
        while r != 1:
            count[r-1] = r
            r -= 1
        perm[:] = perm1
        flips_count = 0
        k = perm[0]
        while k:
            perm[:k+1] = perm[k::-1]
            flips_count += 1
            k = perm[0]
        if flips_count > max_flips:
            max_flips = flips_count
        while r != n:
            perm1_ins(r, perm1_pop(0))
            count[r] -= 1
            if count[r] > 0:
                break
            r += 1
        else:
            return max_flips

fannkuch(9)
