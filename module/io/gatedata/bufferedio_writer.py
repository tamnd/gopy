import _io, sys

path = sys.argv[1]
raw = _io.FileIO(path, "wb")
b = _io.BufferedWriter(raw, 4)
print(b.write(b"abc"))
print(b.write(b"defghi"))
b.flush()
b.close()

raw = _io.FileIO(path, "rb")
print(raw.read())
raw.close()
