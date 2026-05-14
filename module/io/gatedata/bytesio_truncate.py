import _io

b = _io.BytesIO(b"abcdef")
b.truncate(3)
print(b.getvalue())
b.seek(0)
print(b.read())

b = _io.BytesIO(b"ab")
b.seek(2)
b.write(b"cd")
b.truncate(6)
print(b.getvalue())
