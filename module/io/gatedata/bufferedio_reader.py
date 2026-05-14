import _io, sys

path = sys.argv[1]
raw = _io.FileIO(path, "rb")
b = _io.BufferedReader(raw, 4)
print(b.read(3))
print(b.read(3))
print(b.read())
print(b.read())
b.close()
