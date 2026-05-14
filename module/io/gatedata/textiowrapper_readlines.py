import _io, sys

path = sys.argv[1]
raw = _io.FileIO(path, "rb")
buf = _io.BufferedReader(raw)
t = _io.TextIOWrapper(buf, encoding="utf-8")
print(t.read())
t.close()
