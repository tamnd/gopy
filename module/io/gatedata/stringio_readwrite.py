import _io

s = _io.StringIO()
print(s.write("hello"))
print(s.write(" world"))
print(s.getvalue())
print(s.tell())
s.seek(0)
print(s.read())

s = _io.StringIO("abcdef")
print(s.read(3))
print(s.read())
print(s.read())

s = _io.StringIO("abcdef")
s.seek(2, 1)
print(s.tell())
s.seek(0, 2)
print(s.tell())
