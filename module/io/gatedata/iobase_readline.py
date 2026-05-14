import _io

b = _io.BytesIO(b"ab\ncd\nef")
print(b.readline())
print(b.readline())
print(b.readline())
print(b.readline())

b = _io.BytesIO(b"one\ntwo\nthree\n")
print(b.readlines())

b = _io.BytesIO(b"aa\nbb\ncc\n")
print(b.readlines(3))
