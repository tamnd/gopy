import _io, sys

path = sys.argv[1]
raw = _io.FileIO(path, "wb")
buf = _io.BufferedWriter(raw)
t = _io.TextIOWrapper(buf, encoding="utf-8")
print(t.write("hello world"))
t.flush()
t.close()

raw = _io.FileIO(path, "rb")
print(raw.read())
raw.close()
