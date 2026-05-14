import _io

b = _io.BytesIO()
print(b.write(b"hello"))
print(b.write(b" world"))
print(b.getvalue())
print(b.tell())
b.seek(0)
print(b.read())
print(b.read())

b = _io.BytesIO(b"abcdef")
print(b.read(3))
print(b.read())
print(b.read())

b = _io.BytesIO(b"abcdef")
b.seek(2)
print(b.read(-1))

b = _io.BytesIO(b"abcdef")
b.seek(2, 1)
print(b.tell())
b.seek(-2, 2)
print(b.tell())
print(b.read())
