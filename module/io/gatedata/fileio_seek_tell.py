import _io, sys

path = sys.argv[1]
f = _io.FileIO(path, "rb")
f.seek(4)
print(f.tell())
print(f.read(3))
f.seek(-2, 2)
print(f.tell())
print(f.read())
f.close()
