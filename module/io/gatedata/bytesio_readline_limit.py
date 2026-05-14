import _io

b = _io.BytesIO(b"abcdef\nghi\njk")
print(b.readline(3))
print(b.readline())
print(b.readline(100))
print(b.readline())
print(b.readline())
