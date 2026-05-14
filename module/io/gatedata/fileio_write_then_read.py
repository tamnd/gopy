import _io, sys

path = sys.argv[1]
f = _io.FileIO(path, "wb")
print(f.write(b"hello world"))
f.close()
f = _io.FileIO(path, "rb")
print(f.read())
f.close()
