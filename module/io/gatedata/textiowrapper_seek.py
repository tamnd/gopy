import _io, sys

path = sys.argv[1]

f = _io.FileIO(path, "wb")
f.write(b"line one\nline two\nline three\n")
f.close()

def fresh():
    raw = _io.FileIO(path, "rb")
    buf = _io.BufferedReader(raw)
    return _io.TextIOWrapper(buf, encoding="utf-8")

# tell at start, after read-all, after seek(0).
t = fresh()
print("start", t.tell())
data = t.read()
print("len", len(data), "tell", t.tell())
t.seek(0)
print("after-seek0", t.tell())
print("read-again", len(t.read()))
t.close()

# seek to 0 then read first line.
t = fresh()
t.seek(0)
line = t.readline()
print("line1", [ord(c) for c in line])
t.close()
