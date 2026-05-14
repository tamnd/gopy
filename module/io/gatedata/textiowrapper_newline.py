import _io, sys

path = sys.argv[1]

def write_bin(p, data):
    f = _io.FileIO(p, "wb")
    f.write(data)
    f.close()

def fresh(data, newline=None):
    write_bin(path, data)
    raw = _io.FileIO(path, "rb")
    buf = _io.BufferedReader(raw)
    return _io.TextIOWrapper(buf, encoding="utf-8", newline=newline)

def show(label, s):
    print(label, [ord(c) for c in s])

# newline=None: translate all to "\n".
t = fresh(b"abc\rdef\r\nghi\njkl", newline=None)
show("none-read", t.read())
t.close()

# newline="" : universal but preserve original terminators.
t = fresh(b"abc\rdef\r\nghi\njkl", newline="")
show("empty-read", t.read())
t.close()

# newline="\n": no translation.
t = fresh(b"abc\rdef\r\nghi\njkl", newline="\n")
show("lf-read", t.read())
t.close()

# readline with newline=None splits on \r, \n, and \r\n.
t = fresh(b"one\rtwo\r\nthree\n", newline=None)
show("none-line1", t.readline())
show("none-line2", t.readline())
show("none-line3", t.readline())
show("none-line4", t.readline())
t.close()

# write with newline="\r\n" expands "\n" on output.
out = path + ".out"
raw = _io.FileIO(out, "wb")
buf = _io.BufferedWriter(raw)
t = _io.TextIOWrapper(buf, encoding="utf-8", newline="\r\n")
t.write("a\nb\nc")
t.flush()
t.close()
f = _io.FileIO(out, "rb")
show("crlf-write", f.read().decode("latin-1"))
f.close()
