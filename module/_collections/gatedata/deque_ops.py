from _collections import deque

# Concat returns a new deque.
a = deque([1, 2])
b = deque([3, 4])
c = a + b
print("concat", list(c), len(a), len(b))

# Repeat returns a new deque.
d = deque([1, 2]) * 3
print("repeat", list(d))

# Inplace repeat mutates the deque.
e = deque([1, 2])
e *= 2
print("imul", list(e))

# Inplace repeat by 0 clears it.
f = deque([1, 2, 3])
f *= 0
print("imul-zero", list(f), len(f))

# __reduce__ returns a 4-tuple for unbounded deques.
r = deque([1, 2, 3]).__reduce__()
print("reduce-len", len(r), r[0].__name__, list(r[3]))
