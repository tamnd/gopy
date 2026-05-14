from _collections import defaultdict

# __or__ produces a new defaultdict that keeps the factory.
a = defaultdict(int, {"x": 1})
b = a | {"y": 2}
print("or-keys", sorted(b.keys()))
print("or-factory", b.default_factory.__name__)

# __ror__ when left side is a plain dict.
c = {"z": 3} | defaultdict(list, {"q": 4})
print("ror-keys", sorted(c.keys()))

# __ior__ updates in place.
d = defaultdict(int, {"a": 1})
d |= {"b": 2}
print("ior-keys", sorted(d.keys()))
print("ior-factory", d.default_factory.__name__)

# __reduce__ returns the 5-tuple expected by pickle.
r = defaultdict(int, {"k": 1}).__reduce__()
print("reduce-len", len(r))
print("reduce-class", r[0].__name__)
print("reduce-args", r[1])
print("reduce-items", sorted(list(r[4])))
