import _io, sys

path = sys.argv[1]
f = _io.FileIO(path, "rb")
print(f.read())
f.close()
print(f.closed)
