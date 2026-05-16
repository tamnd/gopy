# Exercise BINARY_OP fast-path arms.
# Parity gate for spec 1712 P1.4b: cpython 3.14 and gopy must produce
# byte-equal stdout. Each block runs its operator enough times to
# trip the adaptive counter and pin the matching specialized variant:
#   BINARY_OP_{ADD,SUBTRACT,MULTIPLY}_{INT,FLOAT}
#   BINARY_OP_{ADD,INPLACE_ADD}_UNICODE
#   BINARY_OP_SUBSCR_{LIST_INT,TUPLE_INT,STR_INT,DICT,LIST_SLICE}


def arith_int(n):
    total = 0
    for i in range(n):
        total = total + i
        total = total - 1
        total = total * 1
    return total


def arith_float(n):
    total = 0.0
    for i in range(n):
        total = total + 0.5
        total = total - 0.25
        total = total * 1.0
    return total


def concat_unicode(n):
    s = "a"
    for _ in range(n):
        s = s + "b"
    out = ""
    for _ in range(n):
        out += "c"
    return len(s), len(out)


def subscr(n):
    lst = [10, 20, 30, 40, 50]
    tup = (1, 2, 3, 4, 5)
    txt = "abcdef"
    d = {"k": 7, "j": 9}
    total = 0
    for i in range(n):
        total += lst[i % 5]
        total += tup[i % 5]
        total += ord(txt[i % 6])
        total += d["k"]
    sl = lst[1:4]
    return total, sl


print(arith_int(1000))
print(arith_float(1000))
print(concat_unicode(50))
print(subscr(1000))
